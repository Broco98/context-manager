package cli

import (
	"os"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

func runSetStatus(home, taskName, project, status string) error {
	switch status {
	case "planned", "in_progress", "review", "done":
	default:
		return output.Errorf(jsonOut, output.ErrUsage, "status must be planned|in_progress|review|done")
	}
	dir, tk, err := loadTask(home, taskName)
	if err != nil {
		return err
	}
	p := tk.Project(project)
	if p == nil {
		return output.Errorf(jsonOut, output.ErrNotFound, "project %q not found", project)
	}
	p.Status = task.ProjStatus(status)
	return task.Save(dir, tk)
}

// resolveTaskProject derives the task and project for set-status: explicit
// flags win, otherwise both fall back to the CWD-detected location. It returns a
// USAGE error only when neither a flag nor the CWD can supply the value.
func resolveTaskProject(home, cwd, taskFlag, projectFlag string) (string, string, error) {
	loc, _ := store.Locate(home, cwd)
	name := taskFlag
	if name == "" && loc != nil {
		name = loc.Task
	}
	if name == "" {
		return "", "", output.Errorf(jsonOut, output.ErrUsage, "not inside a task; pass --task")
	}
	proj := projectFlag
	if proj == "" && loc != nil {
		proj = loc.Project
	}
	if proj == "" {
		return "", "", output.Errorf(jsonOut, output.ErrUsage, "no project resolved; pass --project")
	}
	return name, proj, nil
}

func init() {
	var taskFlag, project string
	cmd := &cobra.Command{
		Use: "set-status <planned|in_progress|review|done>", Short: "Set a project's status",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			name, proj, err := resolveTaskProject(home, cwd, taskFlag, project)
			if err != nil {
				return err
			}
			if err := runSetStatus(home, name, proj, args[0]); err != nil {
				return err
			}
			return output.Emit(jsonOut, proj+" -> "+args[0], map[string]string{"project": proj, "status": args[0]})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project name (default: detect from CWD)")
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	RootCmd.AddCommand(cmd)
}
