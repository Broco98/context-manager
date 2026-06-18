package cli

import (
	"os"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

type currentOut struct {
	Task     *string `json:"task"`
	Project  *string `json:"project,omitempty"`
	Spec     string  `json:"spec,omitempty"`
	Worktree string  `json:"worktree,omitempty"`
	Status   string  `json:"status,omitempty"`
}

func runCurrent(home, cwd string) (currentOut, error) {
	loc, err := store.Locate(home, cwd)
	if err != nil {
		return currentOut{}, err
	}
	if loc.Task == "" {
		return currentOut{Task: nil}, nil
	}
	out := currentOut{Task: &loc.Task}
	if loc.Project != "" {
		p := loc.Project
		out.Project = &p
		if tk, lerr := task.Load(loc.TaskDir); lerr == nil {
			if pr := tk.Project(loc.Project); pr != nil {
				out.Spec, out.Worktree, out.Status = pr.Spec, pr.Worktree, string(pr.Status)
			}
		}
	}
	return out, nil
}

func init() {
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Show the task/project detected from the current directory",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			out, err := runCurrent(home, cwd)
			if err != nil {
				return err
			}
			human := "Not inside a ctx task"
			if out.Task != nil {
				human = "task=" + *out.Task
				if out.Project != nil {
					human += " project=" + *out.Project
				}
			}
			return output.Emit(jsonOut, human, out)
		},
	}
	RootCmd.AddCommand(cmd)
}
