package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

func TestRunNewCreatesTaskAndContext(t *testing.T) {
	home := t.TempDir()
	got, err := runNew(home, newOpts{
		Name: "add-payment", Objective: "add payment", DoneWhen: "e2e pass",
		Description: "cross-repo", Background: "users want pay",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "active" || got.Goal.Objective != "add payment" || got.Created == "" {
		t.Errorf("bad task: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(home, "add-payment", "task.yaml")); err != nil {
		t.Error("task.yaml not written")
	}
	ctx, _ := os.ReadFile(task.ContextPath(filepath.Join(home, "add-payment")))
	if !contains(string(ctx), "users want pay") {
		t.Error("context.md missing background")
	}
}

func TestRunNewRejectsDuplicate(t *testing.T) {
	home := t.TempDir()
	opts := newOpts{Name: "dup", Objective: "o", DoneWhen: "d"}
	if _, err := runNew(home, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := runNew(home, opts); err == nil {
		t.Error("expected CONFLICT on duplicate task")
	}
}

func TestRunNewRequiresGoal(t *testing.T) {
	home := t.TempDir()
	if _, err := runNew(home, newOpts{Name: "x"}); err == nil {
		t.Error("expected USAGE error when objective/done_when empty")
	}
}

// TestPromptMissingGoalInteractiveReadsStdin drives the interactive path: the
// objective and done-when are read line-by-line from stdin.
func TestPromptMissingGoalInteractiveReadsStdin(t *testing.T) {
	in := strings.NewReader("add payment\ne2e pass\n")
	var prompt bytes.Buffer
	got, err := promptMissingGoal(newOpts{Name: "x"}, in, &prompt, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Objective != "add payment" || got.DoneWhen != "e2e pass" {
		t.Errorf("bad prompted opts: %+v", got)
	}
	if !strings.Contains(prompt.String(), "--objective") {
		t.Errorf("expected a prompt for --objective, got %q", prompt.String())
	}
}

// TestPromptMissingGoalNonInteractiveErrors confirms a clear USAGE error when a
// field is missing and stdin is non-interactive.
func TestPromptMissingGoalNonInteractiveErrors(t *testing.T) {
	if _, err := promptMissingGoal(newOpts{Name: "x"}, strings.NewReader(""), &bytes.Buffer{}, false); err == nil {
		t.Error("expected USAGE error when non-interactive and objective is missing")
	}
}

// TestPromptMissingGoalKeepsProvidedFlags leaves already-set fields untouched.
func TestPromptMissingGoalKeepsProvidedFlags(t *testing.T) {
	got, err := promptMissingGoal(
		newOpts{Name: "x", Objective: "o", DoneWhen: "d"},
		strings.NewReader(""), &bytes.Buffer{}, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Objective != "o" || got.DoneWhen != "d" {
		t.Errorf("provided flags should be preserved: %+v", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
