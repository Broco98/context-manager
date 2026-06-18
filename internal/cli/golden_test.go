package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// assertGolden marshals v (or uses raw text when v is a string) and compares it
// to testdata/<name>; UPDATE_GOLDEN=1 regenerates the file. All locked --json /
// text contracts in §13 go through this one helper.
func assertGolden(t *testing.T, name string, v any) {
	t.Helper()
	var got []byte
	if s, ok := v.(string); ok {
		got = []byte(s)
	} else {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		got = b
	}
	golden := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("missing golden %s (run UPDATE_GOLDEN=1): %v", name, err)
	}
	if string(got) != string(want) {
		t.Errorf("contract drift for %s.\n got: %s\nwant: %s", name, got, want)
	}
}

func TestStatusDetailJSONGolden(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	// Register a repo-less project so the git-truth lookup is skipped entirely
	// (runStatusDetail only queries git when both Repo and Worktree are set). This
	// keeps the golden deterministic: worktree_exists is false, live_branch is "",
	// and worktree_error is omitted — no machine-specific git error text leaks into
	// the locked contract.
	writeRepolessProjectForTest(t, filepath.Join(home, "add-payment"), "front")
	d, _ := runStatusDetail(home, "add-payment")
	assertGolden(t, "status_detail.json", d)
}

func TestCurrentJSONGolden(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	taskDir := filepath.Join(home, "add-payment")
	writeProjectForTest(t, taskDir, "front")
	out, err := runCurrent(home, filepath.Join(taskDir, "front", "wt"))
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "current.json", out)
}
