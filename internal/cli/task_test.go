package cli

import (
	"path/filepath"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

func TestWorklistAddSetLs(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	it, err := runTaskAdd(home, "tk", "build api")
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != 1 || it.Status != "todo" {
		t.Errorf("bad item: %+v", it)
	}
	if err := runTaskSet(home, "tk", 1, "doing"); err != nil {
		t.Fatal(err)
	}
	out, _ := runTaskLs(home, "tk")
	if out.Items[0].Status != "doing" || out.Progress.Total != 1 {
		t.Errorf("bad worklist: %+v", out)
	}
	if err := runTaskSet(home, "tk", 99, "done"); err == nil {
		t.Error("expected NOT_FOUND for missing id")
	}
}

func TestSetStatusUpdatesProject(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	writeProjectForTest(t, filepath.Join(home, "tk"), "front")
	if err := runSetStatus(home, "tk", "front", "review"); err != nil {
		t.Fatal(err)
	}
	tk, _ := task.Load(filepath.Join(home, "tk"))
	if tk.Project("front").Status != "review" {
		t.Errorf("status not updated: %v", tk.Project("front").Status)
	}
}

// TestResolveTaskProjectFromCWD covers the optional --project path: with no
// flags, both task and project come from the CWD-detected location.
func TestResolveTaskProjectFromCWD(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	taskDir := filepath.Join(home, "tk")
	writeProjectForTest(t, taskDir, "front")
	cwd := filepath.Join(taskDir, "front", "wt")

	name, proj, err := resolveTaskProject(home, cwd, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "tk" || proj != "front" {
		t.Errorf("CWD resolution failed: task=%q project=%q", name, proj)
	}

	// Explicit flags still override the CWD.
	name, proj, err = resolveTaskProject(home, cwd, "tk", "back")
	if err != nil || name != "tk" || proj != "back" {
		t.Errorf("flag override failed: task=%q project=%q err=%v", name, proj, err)
	}

	// Outside any task and with no flags -> USAGE error.
	if _, _, err := resolveTaskProject(home, home, "", ""); err == nil {
		t.Error("expected USAGE error when no task can be resolved")
	}
}

// TestTaskLsJSONGolden locks the `task ls` --json contract (§Global Constraints
// lists `task ls` among the stable JSON contracts). A fixed two-item worklist in
// known states makes worklistOut{items,progress} deterministic. Uses the shared
// assertGolden harness from Task 8.
func TestTaskLsJSONGolden(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	if _, err := runTaskAdd(home, "add-payment", "build ui"); err != nil {
		t.Fatal(err)
	}
	if _, err := runTaskAdd(home, "add-payment", "build api"); err != nil {
		t.Fatal(err)
	}
	if err := runTaskSet(home, "add-payment", 1, "done"); err != nil {
		t.Fatal(err)
	}
	if err := runTaskSet(home, "add-payment", 2, "doing"); err != nil {
		t.Fatal(err)
	}
	out, err := runTaskLs(home, "add-payment")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "task_ls.json", out)
}
