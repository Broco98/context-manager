package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/kimhyoyeon/context-manager/internal/worktree"
)

func gitRepoOrSkip(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", a, out)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644)
	for _, a := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		_, _ = c.CombinedOutput()
	}
	return dir
}

func TestRunAddCreatesWorktreeAndSpec(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "add-payment", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	p, err := runAdd(home, "add-payment", addOpts{Project: "front", Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if p.Branch != "feat/add-payment" || p.Base != "main" {
		t.Errorf("bad defaults: %+v", p)
	}
	taskDir := filepath.Join(home, "add-payment")
	if _, err := os.Stat(filepath.Join(taskDir, "front", "wt", "README")); err != nil {
		t.Error("worktree not created")
	}
	if _, err := os.Stat(filepath.Join(taskDir, "front", "spec.md")); err != nil {
		t.Error("spec.md not scaffolded")
	}
	tk, _ := task.Load(taskDir)
	if tk.Project("front") == nil {
		t.Error("project not registered in task.yaml")
	}
}

func TestRunAddRejectsDuplicateProject(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	_, _ = runNew(home, newOpts{Name: "t", Objective: "o", DoneWhen: "d"})
	if _, err := runAdd(home, "t", addOpts{Project: "front", Repo: repo}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "t", addOpts{Project: "front", Repo: repo}); err == nil {
		t.Error("expected CONFLICT on duplicate project")
	}
}

// TestRunAddRollsBackWorktreeOnSpecWriteFailure proves that if spec.md cannot be
// written AFTER the git worktree was created, runAdd removes the worktree and the
// project directory instead of leaving an orphaned worktree that ctx cannot
// discover (it was never recorded in task.yaml). The spec write is forced to fail
// deterministically by pre-creating a non-empty DIRECTORY at the spec.md path, so
// AtomicWrite's final rename onto that path fails.
func TestRunAddRollsBackWorktreeOnSpecWriteFailure(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(home, "tk")
	// Make the spec.md target a non-empty directory so the spec write fails.
	specDir := filepath.Join(taskDir, "front", "spec.md")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "block"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo}); err == nil {
		t.Fatal("expected runAdd to fail when spec.md cannot be written")
	}
	// The worktree must be rolled back: neither the git worktree nor a registered
	// project may survive.
	wtAbs := filepath.Join(taskDir, "front", "wt")
	if _, err := os.Stat(wtAbs); !os.IsNotExist(err) {
		t.Error("worktree directory must be removed after a failed spec write")
	}
	if _, ok, _ := worktree.Find(repo, wtAbs); ok {
		t.Error("git must not still list the rolled-back worktree")
	}
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Project("front") != nil {
		t.Error("project must not be registered in task.yaml after rollback")
	}
}

// TestRunAddRollsBackWorktreeOnTaskSaveFailure proves the full rollback runs when
// the FINAL task.yaml save fails AFTER the worktree and spec were created. Earlier
// failure injection (a read-only task dir) cannot reach this path because
// store.EnsureDir(projDir) fails first, so the save is forced to fail via the
// saveTask seam — a sentinel error returned only after worktree.Add and the spec
// write have already succeeded. The assertions then confirm every artifact runAdd
// created is gone: the git worktree, the project directory runAdd created, and the
// (never persisted) project registration in task.yaml.
func TestRunAddRollsBackWorktreeOnTaskSaveFailure(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(home, "tk")
	// Force the FINAL save to fail with a sentinel, only after the worktree and spec
	// have been written, so this exercises the task.Save rollback rather than an
	// earlier EnsureDir/worktree/spec failure. Restore the real seam afterward.
	errSave := errors.New("injected task.Save failure")
	orig := saveTask
	saveTask = func(string, *task.Task) error { return errSave }
	defer func() { saveTask = orig }()
	_, addErr := runAdd(home, "tk", addOpts{Project: "front", Repo: repo})
	if !errors.Is(addErr, errSave) {
		t.Fatalf("expected the injected task.Save error, got %v", addErr)
	}
	// The git worktree must be rolled back: both the directory and git's worktree list.
	wtAbs := filepath.Join(taskDir, "front", "wt")
	if _, err := os.Stat(wtAbs); !os.IsNotExist(err) {
		t.Error("worktree directory must be removed after a failed task.yaml save")
	}
	if _, ok, _ := worktree.Find(repo, wtAbs); ok {
		t.Error("git must not still list the rolled-back worktree")
	}
	// The project directory runAdd created must be removed (it did not pre-exist).
	if _, err := os.Stat(filepath.Join(taskDir, "front")); !os.IsNotExist(err) {
		t.Error("the created project directory must be removed after a failed task.yaml save")
	}
	// The project must never be registered, since the save that would record it failed.
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Project("front") != nil {
		t.Error("project must not be registered in task.yaml after rollback")
	}
}
