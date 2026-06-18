package cli

import (
	"fmt"
	"strconv"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

type worklistOut struct {
	Items    []task.WorkItem `json:"items"`
	Progress progressOut     `json:"progress"`
}

func loadTask(home, name string) (string, *task.Task, error) {
	dir, err := checkedTaskDir(home, name)
	if err != nil {
		return "", nil, err
	}
	tk, err := task.Load(dir)
	if err != nil {
		return "", nil, output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", name)
	}
	return dir, tk, nil
}

func runTaskAdd(home, taskName, text string) (task.WorkItem, error) {
	dir, tk, err := loadTask(home, taskName)
	if err != nil {
		return task.WorkItem{}, err
	}
	id := tk.AddWork(text)
	if err := task.Save(dir, tk); err != nil {
		return task.WorkItem{}, err
	}
	return task.WorkItem{ID: id, Text: text, Status: "todo"}, nil
}

func runTaskSet(home, taskName string, id int, status string) error {
	if status != "todo" && status != "doing" && status != "done" {
		return output.Errorf(jsonOut, output.ErrUsage, "status must be todo|doing|done")
	}
	dir, tk, err := loadTask(home, taskName)
	if err != nil {
		return err
	}
	if !tk.SetWork(id, status) {
		return output.Errorf(jsonOut, output.ErrNotFound, "worklist item %d not found", id)
	}
	return task.Save(dir, tk)
}

func runTaskLs(home, taskName string) (worklistOut, error) {
	_, tk, err := loadTask(home, taskName)
	if err != nil {
		return worklistOut{}, err
	}
	done, total := tk.Progress()
	items := tk.Worklist
	if items == nil {
		items = []task.WorkItem{}
	}
	return worklistOut{Items: items, Progress: progressOut{Done: done, Total: total}}, nil
}

func init() {
	var taskFlag string
	taskCmd := &cobra.Command{Use: "task", Short: "Manage the worklist (todo/doing/done items)"}
	taskCmd.PersistentFlags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")

	add := &cobra.Command{
		Use: "add <text>", Short: "Add a worklist item", Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, name, err := homeAndTask(taskFlag)
			if err != nil {
				return err
			}
			it, err := runTaskAdd(home, name, joinArgs(args))
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, fmt.Sprintf("#%d %s", it.ID, it.Text), it)
		},
	}
	setCmd := func(status string) *cobra.Command {
		return &cobra.Command{
			Use: status + " <id>", Short: "Mark a worklist item " + status, Args: cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				home, name, err := homeAndTask(taskFlag)
				if err != nil {
					return err
				}
				id, cerr := strconv.Atoi(args[0])
				if cerr != nil {
					return output.Errorf(jsonOut, output.ErrUsage, "id must be a number")
				}
				if err := runTaskSet(home, name, id, status); err != nil {
					return err
				}
				return output.Emit(jsonOut, fmt.Sprintf("#%d -> %s", id, status), map[string]any{"id": id, "status": status})
			},
		}
	}
	lsCmd := &cobra.Command{
		Use: "ls", Short: "List worklist items",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, name, err := homeAndTask(taskFlag)
			if err != nil {
				return err
			}
			out, err := runTaskLs(home, name)
			if err != nil {
				return err
			}
			human := fmt.Sprintf("%d/%d done", out.Progress.Done, out.Progress.Total)
			for _, it := range out.Items {
				human += fmt.Sprintf("\n  #%d [%s] %s", it.ID, it.Status, it.Text)
			}
			return output.Emit(jsonOut, human, out)
		},
	}
	taskCmd.AddCommand(add, setCmd("todo"), setCmd("doing"), setCmd("done"), lsCmd)
	RootCmd.AddCommand(taskCmd)
}
