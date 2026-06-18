package cli

import (
	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/spf13/cobra"
)

var jsonOut bool

var RootCmd = &cobra.Command{
	Use:           "ctx",
	Short:         "ctx — cross-repo task context manager",
	Long:          "ctx manages cross-repo task context: worktrees, specs, a north-star goal, session rehydration, and per-project persistent knowledge under ~/.ctx.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	RootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
}

// Execute runs the root command and guarantees exactly one error envelope:
// errors already rendered by output.Errorf pass through untouched; every other
// error (filesystem, YAML parsing, Cobra argument errors) is rendered here so no
// failure bypasses the {error,code} contract.
func Execute() error {
	if err := RootCmd.Execute(); err != nil {
		return output.Render(jsonOut, err)
	}
	return nil
}
