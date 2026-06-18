package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

func seedTask(t *testing.T, home, name string) {
	t.Helper()
	if _, err := runNew(home, newOpts{Name: name, Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunLsListsTasksWithProgress(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "alpha")
	seedTask(t, home, "beta")
	ov, err := runLs(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Tasks) != 2 || ov.Tasks[0].Name != "alpha" {
		t.Errorf("bad overview: %+v", ov.Tasks)
	}
	if ov.Tasks[0].Progress != "0/0" {
		t.Errorf("progress got %q want 0/0", ov.Tasks[0].Progress)
	}
}

func TestRunStatusDetailHasGoalAndProgress(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	taskDir := home + "/add-payment"
	writeProjectForTest(t, taskDir, "front")
	d, err := runStatusDetail(home, "add-payment")
	if err != nil {
		t.Fatal(err)
	}
	if d.Goal.DoneWhen != "d" || len(d.Projects) != 1 || d.Progress.Total != 0 {
		t.Errorf("bad detail: %+v", d)
	}
}

// TestRunStatusDetailReportsLiveWorktree exercises git truth: a project with a
// real worktree reports WorktreeExists=true and the live branch from
// `git worktree list`, not just the task.yaml branch field.
func TestRunStatusDetailReportsLiveWorktree(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo}); err != nil {
		t.Fatal(err)
	}
	d, err := runStatusDetail(home, "tk")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %+v", d.Projects)
	}
	if !d.Projects[0].WorktreeExists {
		t.Error("expected WorktreeExists=true for a live worktree")
	}
	if d.Projects[0].LiveBranch != "feat/tk" {
		t.Errorf("live branch: got %q want feat/tk", d.Projects[0].LiveBranch)
	}
}

// TestRunStatusDetailReportsStaleWorktree proves stale/missing metadata is
// reflected: after the worktree directory is removed, git no longer tracks it,
// so WorktreeExists is false and LiveBranch is blank even though task.yaml still
// lists the project.
func TestRunStatusDetailReportsStaleWorktree(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo}); err != nil {
		t.Fatal(err)
	}
	// Delete the worktree directory out-of-band so the task.yaml metadata is now
	// stale relative to git truth.
	if err := os.RemoveAll(filepath.Join(home, "tk", "front", "wt")); err != nil {
		t.Fatal(err)
	}
	d, err := runStatusDetail(home, "tk")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %+v", d.Projects)
	}
	if d.Projects[0].WorktreeExists {
		t.Error("expected WorktreeExists=false for a stale/missing worktree")
	}
	if d.Projects[0].LiveBranch != "" {
		t.Errorf("missing worktree should have blank LiveBranch, got %q", d.Projects[0].LiveBranch)
	}
}

// TestRunStatusDetailReportsUnreadableRepo proves a git FAILURE is not silently
// reported as a confirmed-absent worktree. The project's Repo points at a
// directory that is NOT a git repository, so `git -C <repo> worktree list` fails;
// status must surface that as WorktreeError (non-empty) with WorktreeExists=false,
// never as a clean "worktree confirmed gone" result.
func TestRunStatusDetailReportsUnreadableRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	// A real directory that is NOT a git repo => git worktree list errors out.
	badRepo := t.TempDir()
	taskDir := filepath.Join(home, "tk")
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	tk.Projects = append(tk.Projects, task.Project{
		Name: "front", Repo: badRepo, Base: "main",
		Branch: "feat/tk", Worktree: filepath.Join("front", "wt"),
		Spec: filepath.Join("front", "spec.md"), Status: "in_progress",
	})
	if err := task.Save(taskDir, tk); err != nil {
		t.Fatal(err)
	}
	d, err := runStatusDetail(home, "tk")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %+v", d.Projects)
	}
	if d.Projects[0].WorktreeError == "" {
		t.Error("expected a non-empty WorktreeError when the repo is unreadable")
	}
	if d.Projects[0].WorktreeExists {
		t.Error("unreadable git state must not be reported as a confirmed-present worktree")
	}
	if d.Projects[0].LiveBranch != "" {
		t.Errorf("unreadable git state must leave LiveBranch blank, got %q", d.Projects[0].LiveBranch)
	}
}
