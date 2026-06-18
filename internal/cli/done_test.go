package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/knowledge"
	"github.com/kimhyoyeon/context-manager/internal/task"
)

// seedProvenance records a knowledge page sourced from taskName whose project
// facet covers projects, so the done consolidation prerequisite (phase 1) is
// satisfied without --force. Callers pass every project whose findings the test
// needs treated as consolidated; with no projects given it defaults to "front".
// A multi-project task that drops projects across a resumed done (e.g. front is
// removed before a retry) must seed ALL of them, so HasProvenance still overlaps
// the projects that remain in task.yaml on the retry.
func seedProvenance(t *testing.T, home, taskName string, projects ...string) {
	t.Helper()
	if len(projects) == 0 {
		projects = []string{"front"}
	}
	if _, err := knowledge.Add(home, knowledge.PageInput{
		Projects: projects, Topic: "wrap-up", Body: "consolidated",
		SourceTask: taskName, When: "2026-06-18",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunDoneRemovesWorktreesAndTask(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo}); err != nil {
		t.Fatal(err)
	}
	seedProvenance(t, home, "tk") // satisfy consolidation prerequisite
	// clean + not ahead + provenance recorded → done succeeds
	res, err := runDone(home, "tk", false)
	if err != nil {
		t.Fatalf("done failed: %v", err)
	}
	if len(res.RemovedWorktrees) != 1 {
		t.Errorf("expected 1 removed worktree, got %v", res.RemovedWorktrees)
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); !os.IsNotExist(err) {
		t.Error("task folder should be removed")
	}
}

// TestRunDoneRefusesWithoutConsolidation proves phase 1 blocks deletion when no
// knowledge provenance exists for the task (and that the task survives).
func TestRunDoneRefusesWithoutConsolidation(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	_, _ = runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"})
	_, _ = runAdd(home, "tk", addOpts{Project: "front", Repo: repo})
	if _, err := runDone(home, "tk", false); err == nil {
		t.Error("expected refusal when no knowledge was consolidated")
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); err != nil {
		t.Error("task folder must survive a refused done")
	}
}

// TestRunDoneForceStillRequiresConsolidation proves phase 1 is NOT bypassable by
// --force: with no knowledge provenance recorded, even `ctx done --force` refuses
// and the task folder survives. --force only overrides the dirty/unmerged worktree
// safety check in phase 2 (SPEC §11), never the consolidation prerequisite
// (SPEC §7/§9).
func TestRunDoneForceStillRequiresConsolidation(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	_, _ = runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"})
	_, _ = runAdd(home, "tk", addOpts{Project: "front", Repo: repo})
	// No seedProvenance: there is no consolidated knowledge for this task.
	if _, err := runDone(home, "tk", true); err == nil {
		t.Error("expected --force to STILL refuse a task with no consolidation provenance")
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); err != nil {
		t.Error("task folder must survive a refused force-done")
	}
}

func TestRunDoneRefusesUnmergedUnlessForce(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	_, _ = runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"})
	_, _ = runAdd(home, "tk", addOpts{Project: "front", Repo: repo})
	seedProvenance(t, home, "tk") // isolate the unmerged check from phase 1
	wt := filepath.Join(home, "tk", "front", "wt")
	// create an unmerged commit in the worktree
	_ = os.WriteFile(filepath.Join(wt, "f.txt"), []byte("z"), 0o644)
	for _, a := range [][]string{{"add", "."}, {"commit", "-m", "wip"}} {
		c := exec.Command("git", a...)
		c.Dir = wt
		_, _ = c.CombinedOutput()
	}
	if _, err := runDone(home, "tk", false); err == nil {
		t.Error("expected refusal for unmerged commit")
	}
	if _, err := runDone(home, "tk", true); err != nil {
		t.Errorf("--force should override: %v", err)
	}
}

// TestRunDoneVerifiesRegisteredBranchNotHead proves merge status is checked
// against the REGISTERED branch (refs/heads/p.Branch), not the worktree's current
// HEAD. With an unmerged commit on the registered branch, switching the worktree
// to a clean, merged branch (HEAD now reports Ahead == 0) must NOT let done
// delete the task: the registered branch is still unmerged (SPEC §7/§11).
func TestRunDoneVerifiesRegisteredBranchNotHead(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	_, _ = runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"})
	_, _ = runAdd(home, "tk", addOpts{Project: "front", Repo: repo})
	seedProvenance(t, home, "tk") // isolate the branch check from phase 1
	wt := filepath.Join(home, "tk", "front", "wt")
	gitIn := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = wt
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Put an unmerged commit on the registered branch (feat/tk).
	_ = os.WriteFile(filepath.Join(wt, "f.txt"), []byte("z"), 0o644)
	gitIn("add", ".")
	gitIn("commit", "-m", "wip on registered branch")
	// Now switch the worktree HEAD onto the merged base branch so HEAD looks clean.
	// (Find the base branch the worktree was created from.)
	base := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	base.Dir = repo
	baseName, berr := base.CombinedOutput()
	if berr != nil {
		t.Fatalf("rev-parse base: %v\n%s", berr, baseName)
	}
	gitIn("checkout", "-b", "side", strings.TrimSpace(string(baseName)))
	// HEAD is now Ahead == 0, but feat/tk is still unmerged → done must refuse.
	if _, err := runDone(home, "tk", false); err == nil {
		t.Error("expected refusal: registered branch feat/tk is unmerged even though HEAD looks clean")
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); err != nil {
		t.Error("task folder must survive a refused done")
	}
}

// TestRunDoneResumesAfterPartialWorktreeRemoval proves phase 3 is resumable: with
// two projects, when the SECOND worktree removal fails, the first removal is
// already persisted to task.yaml, the task folder survives, and a retry (after the
// blocker is cleared) finishes by removing only the still-present worktree. The
// second removal is forced to fail deterministically by locking its worktree
// (`git worktree remove` refuses a locked worktree); unlocking it makes the retry
// succeed. Project iteration follows task.yaml registration order: "front" first,
// "back" second.
func TestRunDoneResumesAfterPartialWorktreeRemoval(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	// Distinct branches per project: two worktrees of one repo cannot share a
	// branch, so the default feat/tk would collide on the second add.
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo, Branch: "feat/tk-front"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "back", Repo: repo, Branch: "feat/tk-back"}); err != nil {
		t.Fatal(err)
	}
	// Seed provenance for BOTH projects: the first done drops "front" from task.yaml,
	// so on retry HasProvenance is recomputed against the remaining ["back"]. A page
	// whose facet covers both keeps the prerequisite satisfied across the resume.
	seedProvenance(t, home, "tk", "front", "back") // satisfy phase 1 (survives the retry)

	backWt := filepath.Join(home, "tk", "back", "wt")
	frontWt := filepath.Join(home, "tk", "front", "wt")
	gitRepo := func(args ...string) ([]byte, error) {
		c := exec.Command("git", args...)
		c.Dir = repo
		return c.CombinedOutput()
	}
	// Lock the second worktree so its removal fails on the first attempt.
	if out, err := gitRepo("worktree", "lock", backWt); err != nil {
		t.Fatalf("git worktree lock: %v\n%s", err, out)
	}

	// First attempt: front is removed and persisted, back fails → error returned,
	// task folder must survive for a retry.
	if _, err := runDone(home, "tk", false); err == nil {
		t.Fatal("expected first done to fail on the locked second worktree")
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); err != nil {
		t.Fatal("task folder must survive a partial removal so it can be retried")
	}
	// task.yaml must now list ONLY the still-present "back" project; "front" was
	// removed and the progress persisted.
	tk, err := task.Load(filepath.Join(home, "tk"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tk.Projects) != 1 || tk.Projects[0].Name != "back" {
		t.Fatalf("expected only 'back' to remain after partial removal, got %+v", tk.Projects)
	}
	if tk.Project("front") != nil {
		t.Error("front must be dropped from task.yaml after its worktree was removed")
	}
	if _, err := os.Stat(frontWt); !os.IsNotExist(err) {
		t.Error("front worktree directory should be gone after the first attempt")
	}

	// Clear the blocker and retry: the resume must remove only "back" and then
	// delete the task folder.
	if out, err := gitRepo("worktree", "unlock", backWt); err != nil {
		t.Fatalf("git worktree unlock: %v\n%s", err, out)
	}
	res, err := runDone(home, "tk", false)
	if err != nil {
		t.Fatalf("retry done failed: %v", err)
	}
	if len(res.RemovedWorktrees) != 1 || res.RemovedWorktrees[0] != "back" {
		t.Errorf("retry should remove only the remaining 'back' worktree, got %v", res.RemovedWorktrees)
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); !os.IsNotExist(err) {
		t.Error("task folder should be removed after the retry finishes")
	}
}
