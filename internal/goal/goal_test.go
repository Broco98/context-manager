package goal

import (
	"strings"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

func TestHandoffEndsWithGoalCommand(t *testing.T) {
	tk := &task.Task{Name: "add-payment", Goal: task.Goal{Objective: "add pay", DoneWhen: "e2e pass + lint clean"}}
	out := Handoff(tk)
	if !strings.Contains(out, "add pay") {
		t.Errorf("missing objective:\n%s", out)
	}
	if !strings.Contains(out, "/goal e2e pass + lint clean") {
		t.Errorf("missing /goal line:\n%s", out)
	}
}
