package cli

import (
	"bufio"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

type newOpts struct {
	Name, Objective, DoneWhen, Description, Background string
}

func runNew(home string, o newOpts) (*task.Task, error) {
	if o.Name == "" || o.Objective == "" || o.DoneWhen == "" {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "name, --objective and --done-when are required")
	}
	// Reject names that are not a safe single path component (e.g. "../other") or
	// reserved underscore names (e.g. "_knowledge"), so the task dir cannot escape
	// CTX_HOME or collide with the reserved knowledge directory.
	if err := store.ValidName(o.Name); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "invalid task name: %v", err)
	}
	dir := store.TaskDir(home, o.Name)
	if dirExists(dir) {
		return nil, output.Errorf(jsonOut, output.ErrConflict, "task %q already exists", o.Name)
	}
	t := &task.Task{
		Name: o.Name, Status: "active",
		Created:     time.Now().UTC().Format(time.RFC3339),
		Description: o.Description,
		Goal:        task.Goal{Objective: o.Objective, DoneWhen: o.DoneWhen},
	}
	if err := task.Save(dir, t); err != nil {
		return nil, err
	}
	if err := store.AtomicWrite(task.ContextPath(dir), []byte(task.ContextTemplate(o.Name, o.Background))); err != nil {
		return nil, err
	}
	return t, nil
}

// promptMissingGoal fills empty Objective/DoneWhen by prompting on `in` when the
// session is interactive. When non-interactive and a goal field is still empty,
// it returns a USAGE error (spec §7: prompt interactively, else clear usage error).
func promptMissingGoal(o newOpts, in io.Reader, prompt io.Writer, interactive bool) (newOpts, error) {
	r := bufio.NewReader(in)
	ask := func(label string) (string, error) {
		if !interactive {
			return "", output.Errorf(jsonOut, output.ErrUsage, "%s is required (pass the flag or run interactively)", label)
		}
		_, _ = io.WriteString(prompt, label+": ")
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return "", output.Errorf(jsonOut, output.ErrUsage, "%s is required", label)
		}
		return strings.TrimSpace(line), nil
	}
	if o.Objective == "" {
		v, err := ask("--objective")
		if err != nil {
			return o, err
		}
		o.Objective = v
	}
	if o.DoneWhen == "" {
		v, err := ask("--done-when")
		if err != nil {
			return o, err
		}
		o.DoneWhen = v
	}
	if o.Objective == "" || o.DoneWhen == "" {
		return o, output.Errorf(jsonOut, output.ErrUsage, "objective and done-when are required")
	}
	return o, nil
}

// isInteractive reports whether stdin is a terminal (not a pipe/redirect).
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func init() {
	o := newOpts{}
	cmd := &cobra.Command{
		Use:   "new <task>",
		Short: "Create a task (task.yaml with goal + context.md)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			o.Name = args[0]
			filled, err := promptMissingGoal(o, os.Stdin, os.Stderr, isInteractive())
			if err != nil {
				return err
			}
			t, err := runNew(home, filled)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, "Created task "+t.Name, t)
		},
	}
	cmd.Flags().StringVar(&o.Objective, "objective", "", "goal objective (what/why); prompted if omitted")
	cmd.Flags().StringVar(&o.DoneWhen, "done-when", "", "verifiable completion condition; prompted if omitted")
	cmd.Flags().StringVarP(&o.Description, "description", "d", "", "short description")
	cmd.Flags().StringVar(&o.Background, "background", "", "context.md Background prefill")
	RootCmd.AddCommand(cmd)
}
