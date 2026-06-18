package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCurrentInsideTask(t *testing.T) {
	home := t.TempDir()
	if _, err := runNew(home, newOpts{Name: "add-payment", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	// simulate a registered front project so status/spec resolve
	taskDir := filepath.Join(home, "add-payment")
	_ = os.MkdirAll(filepath.Join(taskDir, "front", "wt"), 0o755)
	writeProjectForTest(t, taskDir, "front")

	out, err := runCurrent(home, filepath.Join(taskDir, "front", "wt"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Task == nil || *out.Task != "add-payment" || out.Project == nil || *out.Project != "front" {
		t.Errorf("bad current: %+v", out)
	}
}

func TestRunCurrentOutsideTaskIsNull(t *testing.T) {
	home := t.TempDir()
	out, err := runCurrent(home, home)
	if err != nil {
		t.Fatal(err)
	}
	if out.Task != nil {
		t.Errorf("expected nil task, got %v", *out.Task)
	}
}
