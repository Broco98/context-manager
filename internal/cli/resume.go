package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/resume"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/kimhyoyeon/context-manager/internal/worktree"
	"github.com/spf13/cobra"
)

func runResume(home, taskName string) (*resume.Bundle, error) {
	dir, err := checkedTaskDir(home, taskName)
	if err != nil {
		return nil, err
	}
	tk, err := task.Load(dir)
	if err != nil {
		return nil, output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", taskName)
	}
	gitState := func(p task.Project) resume.ProjState {
		// Start from stored metadata; mark live state UNAVAILABLE until proven.
		ps := resume.ProjState{Name: p.Name, Base: p.Base, Branch: p.Branch, Status: string(p.Status)}
		st, err := worktree.Status(filepath.Join(dir, p.Worktree), p.Base)
		if err != nil {
			// Spec requires LIVE git state: a missing/unreadable worktree must be
			// reported as an explicit error, NOT fabricated as clean (dirty=false,
			// ahead/behind=0). Leave those fields zero and surface the reason.
			ps.Available = false
			ps.Error = err.Error()
			return ps
		}
		ps.Available = true
		ps.Branch, ps.Dirty, ps.Ahead, ps.Behind = st.Branch, st.Dirty, st.Ahead, st.Behind
		return ps
	}
	return resume.Build(dir, tk, gitState)
}

func init() {
	var taskFlag string
	cmd := &cobra.Command{
		Use: "resume [task]", Short: "Rehydrate full task context (goal + state) for a new session",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			// Resolution order: positional <task>, then --task, then CWD.
			name := taskFlag
			if len(args) == 1 {
				name = args[0]
			}
			if name == "" {
				if name, err = resolveTask(home, ""); err != nil {
					return err
				}
			}
			b, err := runResume(home, name)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, resumeHuman(b), b)
		},
	}
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: positional arg, then CWD)")
	RootCmd.AddCommand(cmd)
}

// resumeHuman renders the COMPLETE rehydration bundle for text mode. SPEC §7
// requires `ctx resume` (not only `--json`) to reload the full context and live
// project state, so every field of the Bundle is printed: goal, Background, Plan,
// worklist with progress, the next item, recent journal entries, and each
// project's live git state (or an explicit "unavailable: <reason>" when the
// worktree could not be read, never fabricated clean state).
func resumeHuman(b *resume.Bundle) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "TASK: %s [%s]\n", b.Task, b.Status)
	fmt.Fprintf(&sb, "GOAL: %s\n  done_when: %s\n", b.Goal.Objective, b.Goal.DoneWhen)
	fmt.Fprintf(&sb, "\n## Background\n%s\n", b.Background)
	fmt.Fprintf(&sb, "\n## Plan\n%s\n", b.Plan)

	done, total := 0, len(b.Worklist)
	for _, w := range b.Worklist {
		if w.Status == "done" {
			done++
		}
	}
	fmt.Fprintf(&sb, "\n## Worklist (%d/%d done)\n", done, total)
	for _, w := range b.Worklist {
		fmt.Fprintf(&sb, "  #%d [%s] %s\n", w.ID, w.Status, w.Text)
	}

	next := b.Next
	if next == "" {
		next = "(none)"
	}
	fmt.Fprintf(&sb, "\nNext: %s\n", next)

	fmt.Fprintf(&sb, "\n## Recent Journal\n")
	if len(b.RecentJournal) == 0 {
		fmt.Fprintf(&sb, "  (none)\n")
	}
	for _, j := range b.RecentJournal {
		fmt.Fprintf(&sb, "  %s\n", j)
	}

	fmt.Fprintf(&sb, "\n## Projects\n")
	if len(b.Projects) == 0 {
		fmt.Fprintf(&sb, "  (none)\n")
	}
	for _, p := range b.Projects {
		if p.Available {
			dirty := "clean"
			if p.Dirty {
				dirty = "dirty"
			}
			fmt.Fprintf(&sb, "  %-12s %-10s branch=%s base=%s %s ahead=%d behind=%d\n",
				p.Name, p.Status, p.Branch, p.Base, dirty, p.Ahead, p.Behind)
		} else {
			fmt.Fprintf(&sb, "  %-12s %-10s branch=%s base=%s git state unavailable: %s\n",
				p.Name, p.Status, p.Branch, p.Base, p.Error)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
