package cli

import (
	"strings"
	"time"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/resume"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

func runLog(home, taskName, msg string) error {
	if strings.TrimSpace(msg) == "" {
		return output.Errorf(jsonOut, output.ErrUsage, "message is empty")
	}
	dir, err := checkedTaskDir(home, taskName)
	if err != nil {
		return err
	}
	if _, err := task.Load(dir); err != nil {
		return output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", taskName)
	}
	date := time.Now().UTC().Format("2006-01-02")
	return resume.AppendJournal(task.ContextPath(dir), date, msg)
}

func init() {
	var taskFlag string
	cmd := &cobra.Command{
		Use: "log <message>", Short: "Append a dated entry to context.md Journal",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, name, err := homeAndTask(taskFlag)
			if err != nil {
				return err
			}
			if err := runLog(home, name, joinArgs(args)); err != nil {
				return err
			}
			return output.Emit(jsonOut, "logged", map[string]string{"task": name})
		},
	}
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	RootCmd.AddCommand(cmd)
}
