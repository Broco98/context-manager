package cli

import (
	"path/filepath"
	"testing"
)

func TestRunSpecResolvesNamedProject(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	taskDir := filepath.Join(home, "tk")
	writeProjectForTest(t, taskDir, "front")
	// writeProjectForTest sets Spec to "front/spec.md"; create that file.
	if err := writeSpecFileForTest(home, "tk", "front", "# front spec\nbody here"); err != nil {
		t.Fatal(err)
	}
	// Resolve by explicit task + project flags (CWD is outside the task).
	out, err := runSpec(home, home, "tk", "front")
	if err != nil {
		t.Fatal(err)
	}
	if out.Project != "front" || out.Path != filepath.Join(taskDir, "front", "spec.md") {
		t.Errorf("bad spec out: %+v", out)
	}
	if !contains(out.Content, "front spec") {
		t.Errorf("spec content missing: %q", out.Content)
	}
}

func TestRunSpecResolvesProjectFromCWD(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	taskDir := filepath.Join(home, "tk")
	writeProjectForTest(t, taskDir, "front")
	if err := writeSpecFileForTest(home, "tk", "front", "# front spec"); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(taskDir, "front", "wt")
	out, err := runSpec(home, cwd, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Project != "front" || !contains(out.Content, "front spec") {
		t.Errorf("CWD resolution failed: %+v", out)
	}
}

func TestRunSpecErrorsWhenUnregistered(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	if _, err := runSpec(home, home, "tk", "ghost"); err == nil {
		t.Error("expected NOT_FOUND for unregistered project")
	}
}
