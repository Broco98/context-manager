package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateFindsTaskAndProjectFromDeepCwd(t *testing.T) {
	home := t.TempDir()
	taskDir := filepath.Join(home, "add-payment")
	deep := filepath.Join(taskDir, "front", "wt", "src")
	_ = os.MkdirAll(deep, 0o755)
	_ = os.WriteFile(filepath.Join(taskDir, "task.yaml"), []byte("name: add-payment"), 0o644)

	loc, err := Locate(home, deep)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Task != "add-payment" || loc.Project != "front" {
		t.Errorf("got task=%q project=%q", loc.Task, loc.Project)
	}
}

func TestLocateOutsideTaskReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	loc, err := Locate(home, home)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Task != "" {
		t.Errorf("expected empty task, got %q", loc.Task)
	}
}

// TestLocateIgnoresTaskYamlOutsideHome proves a task.yaml that lives OUTSIDE
// CTX_HOME is never reported as a ctx task: Locate must not walk past the home
// boundary, so {task:""} is returned even though an ancestor has a task.yaml.
func TestLocateIgnoresTaskYamlOutsideHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "ctx-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stray task.yaml in an unrelated tree OUTSIDE home.
	external := filepath.Join(root, "some-repo")
	deep := filepath.Join(external, "pkg", "sub")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "task.yaml"), []byte("name: not-a-ctx-task"), 0o644); err != nil {
		t.Fatal(err)
	}
	loc, err := Locate(home, deep)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Task != "" {
		t.Errorf("external task.yaml outside home must not be detected, got task=%q", loc.Task)
	}
}
