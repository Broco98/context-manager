package cli

import (
	"github.com/kimhyoyeon/context-manager/internal/goal"
	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

func runGoalSet(home, taskName, objective, doneWhen string) (task.Goal, error) {
	dir, tk, unlock, err := lockedLoadTask(home, taskName)
	if err != nil {
		return task.Goal{}, err
	}
	defer unlock()
	if objective != "" {
		tk.Goal.Objective = objective
	}
	if doneWhen != "" {
		tk.Goal.DoneWhen = doneWhen
	}
	if err := task.Save(dir, tk); err != nil {
		return task.Goal{}, err
	}
	return tk.Goal, nil
}

func init() {
	var taskFlag string
	var handoff bool
	var objective, doneWhen string
	cmd := &cobra.Command{
		Use: "goal", Short: "Show, set, or hand off the task's north-star goal",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, name, err := homeAndTask(taskFlag)
			if err != nil {
				return err
			}
			_, tk, err := loadTask(home, name)
			if err != nil {
				return err
			}
			if handoff {
				h := goal.Handoff(tk)
				return output.Emit(jsonOut, h, map[string]string{"handoff": h})
			}
			return output.Emit(jsonOut, "objective: "+tk.Goal.Objective+"\ndone_when: "+tk.Goal.DoneWhen, tk.Goal)
		},
	}
	setCmd := &cobra.Command{
		Use: "set", Short: "Set/edit the goal (objective and/or done_when)",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, name, err := homeAndTask(taskFlag)
			if err != nil {
				return err
			}
			g, err := runGoalSet(home, name, objective, doneWhen)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, "goal updated", g)
		},
	}
	cmd.PersistentFlags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	cmd.Flags().BoolVar(&handoff, "handoff", false, "emit native /goal handoff text")
	setCmd.Flags().StringVar(&objective, "objective", "", "new objective")
	setCmd.Flags().StringVar(&doneWhen, "done-when", "", "new completion condition")
	cmd.AddCommand(setCmd)
	RootCmd.AddCommand(cmd)
}
