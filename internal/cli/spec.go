package cli

import (
	"os"
	"path/filepath"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

type specOut struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func runSpec(home, cwd, taskFlag, projectFlag string) (specOut, error) {
	loc, _ := store.Locate(home, cwd)
	name := taskFlag
	if name == "" && loc != nil {
		name = loc.Task
	}
	if name == "" {
		return specOut{}, output.Errorf(jsonOut, output.ErrUsage, "not inside a task; pass --task")
	}
	proj := projectFlag
	if proj == "" && loc != nil {
		proj = loc.Project
	}
	if proj == "" {
		return specOut{}, output.Errorf(jsonOut, output.ErrUsage, "no project resolved; pass --project")
	}
	dir, err := checkedTaskDir(home, name)
	if err != nil {
		return specOut{}, err
	}
	tk, err := task.Load(dir)
	if err != nil {
		return specOut{}, output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", name)
	}
	p := tk.Project(proj)
	if p == nil {
		return specOut{}, output.Errorf(jsonOut, output.ErrNotFound, "project %q not registered", proj)
	}
	path := filepath.Join(dir, p.Spec)
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return specOut{}, output.Errorf(jsonOut, output.ErrNotFound, "spec for %q not found at %s", proj, path)
	}
	return specOut{Project: proj, Path: path, Content: string(data)}, nil
}

func init() {
	var taskFlag, project string
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "Print the spec path/content for the current (or named) project",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			out, err := runSpec(home, cwd, taskFlag, project)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, out.Content, out)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project name (default: detect from CWD)")
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	RootCmd.AddCommand(cmd)
}
