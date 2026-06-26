package cli

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

// TestConcurrentAddDistinctProjectsNoLostProject reproduces the user's exact
// incident: two concurrent `ctx add` of DIFFERENT projects onto the SAME task. The
// per-task lock must serialize them so BOTH survive — both registered in task.yaml
// AND both worktrees on disk. Without the lock, each runAdd reads the same task.yaml
// snapshot, appends its own project, and the slower atomic-rename Save clobbers the
// other's project (last-writer-wins), orphaning the lost project's already-created
// worktree (committed==true => no rollback). This guards the runAdd critical section
// (the literal production failure), not just the simpler runTaskAdd slice append.
func TestConcurrentAddDistinctProjectsNoLostProject(t *testing.T) {
	home := t.TempDir()
	repoA := gitRepoOrSkip(t)
	repoB := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "incident", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}

	specs := []addOpts{
		{Project: "web-api", Repo: repoA, Base: "main"},
		{Project: "fe", Repo: repoB, Base: "main"},
	}
	errs := make([]error, len(specs))
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(len(specs))
	for i := range specs {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = runAdd(home, "incident", specs[i])
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("runAdd[%s] returned error: %v", specs[i].Project, err)
		}
	}

	taskDir := filepath.Join(home, "incident")
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"web-api", "fe"} {
		if tk.Project(name) == nil {
			t.Errorf("project %q LOST from task.yaml (last-writer-wins clobber)", name)
		}
		if _, statErr := os.Stat(filepath.Join(taskDir, name, "wt", "README")); statErr != nil {
			t.Errorf("worktree for %q missing on disk: %v", name, statErr)
		}
	}
	if got := len(tk.Projects); got != len(specs) {
		t.Errorf("task.yaml has %d projects, want %d (a clobber dropped one)", got, len(specs))
	}
}

// TestConcurrentAddSameProjectExactlyOneWins covers two concurrent `ctx add` of the
// SAME project name onto one task. Under the lock, exactly one wins and the other
// hits the already-registered conflict check (which returns BEFORE worktree.Add, so
// it touches no git state); task.yaml ends with exactly one such project. Without the
// lock both pass the Project()==nil check on the same snapshot and then collide on
// git's index.lock at the shared worktree path, corrupting state instead of yielding
// a clean CONFLICT.
func TestConcurrentAddSameProjectExactlyOneWins(t *testing.T) {
	home := t.TempDir()
	repos := []string{gitRepoOrSkip(t), gitRepoOrSkip(t)}
	if _, err := runNew(home, newOpts{Name: "dup", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}

	errs := make([]error, len(repos))
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(len(repos))
	for i := range repos {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = runAdd(home, "dup", addOpts{Project: "front", Repo: repos[i], Base: "main"})
		}(i)
	}
	close(start)
	wg.Wait()

	successes, conflicts, others := 0, 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case strings.Contains(err.Error(), "CONFLICT"):
			conflicts++
		default:
			others++
			t.Logf("non-conflict error from a loser: %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("got %d successes, want exactly 1", successes)
	}
	if conflicts != 1 {
		t.Errorf("got %d CONFLICT-class losers, want exactly 1 (other errors=%d)", conflicts, others)
	}

	tk, err := task.Load(filepath.Join(home, "dup"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, p := range tk.Projects {
		if p.Name == "front" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("task.yaml has %d projects named front, want exactly 1 (duplicate registration)", count)
	}
}

// TestConcurrentMixedMutationsAllPersist runs two DIFFERENT mutating commands on the
// same task concurrently — runSetStatus on a pre-registered project plus several
// runTaskAdd worklist items. Because both go through the same per-task lock, every
// effect persists. An unlocked implementation lets one command's Save clobber the
// other's (a lost update ACROSS commands), dropping either the status change or the
// worklist items.
func TestConcurrentMixedMutationsAllPersist(t *testing.T) {
	home := t.TempDir()
	if _, err := runNew(home, newOpts{Name: "mixed", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(home, "mixed")
	// Pre-register a project (no git needed; runSetStatus only edits task.yaml).
	writeProjectForTest(t, taskDir, "svc")

	const nItems = 8
	addErrs := make([]error, nItems)
	var statusErr error
	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		statusErr = runSetStatus(home, "mixed", "svc", "done")
	}()
	for i := 0; i < nItems; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, addErrs[i] = runTaskAdd(home, "mixed", "work item")
		}(i)
	}
	close(start)
	wg.Wait()

	if statusErr != nil {
		t.Errorf("runSetStatus error: %v", statusErr)
	}
	for i, err := range addErrs {
		if err != nil {
			t.Errorf("runTaskAdd[%d] error: %v", i, err)
		}
	}

	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if p := tk.Project("svc"); p == nil {
		t.Fatal("project svc vanished")
	} else if string(p.Status) != "done" {
		t.Errorf("status change LOST: svc.Status=%q want done", p.Status)
	}
	if got := len(tk.Worklist); got != nItems {
		t.Errorf("worklist has %d items, want %d (lost worklist updates)", got, nItems)
	}
}
