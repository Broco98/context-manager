package goal

import "github.com/kimhyoyeon/context-manager/internal/task"

// Handoff builds deterministic, model-facing text that re-arms native /goal.
func Handoff(tk *task.Task) string {
	return "Goal handoff for task " + tk.Name + ":\n" +
		"Objective: " + tk.Goal.Objective + "\n" +
		"Run this in-session to re-arm the native goal:\n" +
		"  /goal " + tk.Goal.DoneWhen + "\n"
}
