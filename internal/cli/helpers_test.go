package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

// writeProjectForTest appends a minimal project to an existing task.yaml.
func writeProjectForTest(t *testing.T, taskDir, name string) {
	t.Helper()
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	tk.Projects = append(tk.Projects, task.Project{
		Name: name, Repo: "/repo/" + name, Base: "develop",
		Branch: "feat/x", Worktree: filepath.Join(name, "wt"),
		Spec: filepath.Join(name, "spec.md"), Status: "in_progress",
	})
	if err := task.Save(taskDir, tk); err != nil {
		t.Fatal(err)
	}
}

// writeRepolessProjectForTest appends a project with an EMPTY Repo so the
// git-truth lookup in runStatusDetail is skipped entirely (it only queries git
// when both Repo and Worktree are set). Used by the status_detail.json golden so
// the locked contract is deterministic: worktree_exists=false, live_branch="",
// and worktree_error is omitted — no machine-specific git error text leaks in.
func writeRepolessProjectForTest(t *testing.T, taskDir, name string) {
	t.Helper()
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	tk.Projects = append(tk.Projects, task.Project{
		Name: name, Repo: "", Base: "develop",
		Branch: "feat/x", Worktree: filepath.Join(name, "wt"),
		Spec: filepath.Join(name, "spec.md"), Status: "in_progress",
	})
	if err := task.Save(taskDir, tk); err != nil {
		t.Fatal(err)
	}
}

// writeSpecFileForTest writes a project's spec.md to its registered path
// (used by Task 15's ctx spec tests).
func writeSpecFileForTest(home, taskName, project, body string) error {
	taskDir := filepath.Join(home, taskName)
	tk, err := task.Load(taskDir)
	if err != nil {
		return err
	}
	p := tk.Project(project)
	if p == nil {
		return os.ErrNotExist
	}
	dst := filepath.Join(taskDir, p.Spec)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(body), 0o644)
}
