package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomeUsesEnvOverride(t *testing.T) {
	t.Setenv("CTX_HOME", "/tmp/custom-ctx")
	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/custom-ctx" {
		t.Errorf("got %q, want /tmp/custom-ctx", got)
	}
}

func TestHomeDefaultsToHomeDotCtx(t *testing.T) {
	t.Setenv("CTX_HOME", "")
	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if got != filepath.Join(home, ".ctx") {
		t.Errorf("got %q, want %q", got, filepath.Join(home, ".ctx"))
	}
}

func TestAtomicWriteCreatesParentsAndContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a", "b", "task.yaml")
	if err := AtomicWrite(p, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "hello" {
		t.Errorf("got %q", string(got))
	}
}

func TestListTasksExcludesUnderscoreAndSortsByName(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "_knowledge"} {
		d := filepath.Join(home, name)
		_ = os.MkdirAll(d, 0o755)
		if name != "_knowledge" {
			_ = os.WriteFile(filepath.Join(d, "task.yaml"), []byte("name: "+name), 0o644)
		}
	}
	got, err := ListTasks(home)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "zeta"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}
