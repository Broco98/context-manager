package cli

import (
	"os"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/spf13/cobra"
)

type whereOut struct {
	Home    string `json:"home"`
	TaskDir string `json:"task_dir,omitempty"`
}

func runWhere(home, cwd string) (whereOut, error) {
	loc, err := store.Locate(home, cwd)
	if err != nil {
		return whereOut{}, err
	}
	return whereOut{Home: home, TaskDir: loc.TaskDir}, nil
}

func init() {
	cmd := &cobra.Command{
		Use: "where", Short: "Print CTX_HOME and the current task path",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			w, err := runWhere(home, cwd)
			if err != nil {
				return err
			}
			human := "CTX_HOME=" + w.Home
			if w.TaskDir != "" {
				human += "\ntask=" + w.TaskDir
			}
			return output.Emit(jsonOut, human, w)
		},
	}
	RootCmd.AddCommand(cmd)
}
