package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kimhyoyeon/context-manager/internal/knowledge"
	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/kimhyoyeon/context-manager/internal/worktree"
	"github.com/spf13/cobra"
)

type doneResult struct {
	Task             string   `json:"task"`
	RemovedWorktrees []string `json:"removed_worktrees"`
}

// runDone finalizes a task as ordered, fail-closed phases:
//  1. consolidation prerequisite (knowledge provenance recorded),
//  2. verification (clean + merged + readable worktree status),
//  3. worktree removal (every worktree must be removed), then
//  4. task-folder deletion (only after all worktrees are gone).
//
// `force` overrides ONLY phase 2 (the dirty/unmerged worktree safety check and
// the status-read check), per SPEC §11. It does NOT bypass phase 1: consolidation
// provenance is ALWAYS required (SPEC §7/§9) so durable findings are never lost.
// `force` also still requires that worktree removal itself succeed before the task
// folder is deleted, so a registered worktree is never orphaned with its metadata
// destroyed.
func runDone(home, taskName string, force bool) (doneResult, error) {
	dir, tk, err := loadTask(home, taskName)
	if err != nil {
		return doneResult{}, err
	}

	// Phase 1: consolidation prerequisite. This is enforced even under --force:
	// SPEC §7/§9 require that a task's findings be consolidated (provenance
	// recorded) before the task is deleted, and SPEC §11 limits --force to the
	// dirty/unmerged worktree safety overrides in phase 2. Allowing --force to
	// skip this would let `ctx done --force` destroy a task with no provenance.
	// The page must consolidate one of THIS task's registered projects, so a page
	// written for an unrelated project never satisfies the gate (SPEC §7/§9).
	taskProjects := make([]string, 0, len(tk.Projects))
	for _, p := range tk.Projects {
		taskProjects = append(taskProjects, p.Name)
	}
	ok, perr := knowledge.HasProvenance(home, taskName, taskProjects)
	if perr != nil {
		return doneResult{}, output.Errorf(jsonOut, output.ErrState,
			"could not verify knowledge consolidation for %q: %v", taskName, perr)
	}
	if !ok {
		return doneResult{}, output.Errorf(jsonOut, output.ErrState,
			"refusing to finish %q: consolidate this task's project findings first with 'ctx know add' (no matching provenance recorded); --force does not override this", taskName)
	}

	// Phase 2: verification. Status-read failures are blockers, not skipped.
	// Merge status is measured for the REGISTERED branch (refs/heads/p.Branch)
	// against p.Base, NOT the worktree's current HEAD: a worktree that was switched
	// or detached must not let unmerged commits on p.Branch slip through as
	// Ahead == 0 (SPEC §7/§11). We additionally fail closed if the worktree's live
	// branch no longer matches the registered p.Branch, since dirty-tree state then
	// refers to a different branch than the one whose merge status we verified.
	var blockers []string
	for _, p := range tk.Projects {
		wtPath := filepath.Join(dir, p.Worktree)
		if p.Branch == "" {
			blockers = append(blockers, p.Name+": no registered branch to verify")
			continue
		}
		head, herr := worktree.Status(wtPath, "")
		if herr != nil {
			blockers = append(blockers, fmt.Sprintf("%s: cannot read worktree status: %v", p.Name, herr))
			continue
		}
		if head.Branch != p.Branch {
			blockers = append(blockers, fmt.Sprintf("%s: worktree is on %q, not the registered branch %q", p.Name, head.Branch, p.Branch))
		}
		bs, berr := worktree.BranchStatus(wtPath, p.Branch, p.Base)
		if berr != nil {
			blockers = append(blockers, fmt.Sprintf("%s: cannot read branch status: %v", p.Name, berr))
			continue
		}
		if bs.Dirty {
			blockers = append(blockers, p.Name+": uncommitted changes")
		}
		if bs.Ahead > 0 {
			blockers = append(blockers, fmt.Sprintf("%s: %d unmerged commit(s) on %s", p.Name, bs.Ahead, p.Branch))
		}
	}
	if len(blockers) > 0 && !force {
		return doneResult{}, output.Errorf(jsonOut, output.ErrState,
			"refusing to finish %q: %s (use --force)", taskName, strings.Join(blockers, "; "))
	}

	// Phase 3: remove every worktree, resumably. A removal failure aborts before
	// deleting the task folder so the worktree (and its recovery metadata)
	// survives. To make a retry reliable, every successful (or already-absent)
	// removal is persisted: the project is dropped from task.yaml immediately, so
	// a later failure leaves task.yaml listing ONLY the still-present worktrees. A
	// retry then never re-attempts an already-removed worktree (which `git worktree
	// remove` would reject) and never re-verifies a vanished one. A worktree git
	// already reports as absent (stale/missing metadata) is treated as already
	// removed rather than an error.
	res := doneResult{Task: taskName}
	// Iterate over a stable snapshot of the original projects: the loop body
	// reassigns tk.Projects (to persist progress) as it goes, so we must not range
	// over the field being mutated.
	originalProjects := make([]task.Project, len(tk.Projects))
	copy(originalProjects, tk.Projects)
	for _, p := range originalProjects {
		wtPath := filepath.Join(dir, p.Worktree)
		_, present, ferr := worktree.Find(p.Repo, wtPath)
		if ferr != nil || present {
			// Either git positively reports the worktree as present, or we could
			// not read git state (ferr != nil) — in the unreadable case we still
			// attempt the removal so the failure is surfaced, never silently
			// skipped. A confirmed-absent worktree (ferr == nil && !present) is
			// treated as already removed and falls through to the persist step.
			if rerr := worktree.Remove(p.Repo, wtPath, force); rerr != nil {
				return res, output.Errorf(jsonOut, output.ErrGit,
					"task %q: failed to remove worktree for project %q: %v", taskName, p.Name, rerr)
			}
		}
		// Persist this removal (real or already-absent) before touching the next
		// worktree: drop the project from task.yaml so a failure on a LATER project
		// leaves a retryable, consistent task.yaml that lists only still-present
		// worktrees. A retry then never re-attempts an already-removed worktree.
		tk.Projects = dropProject(tk.Projects, p.Name)
		if serr := task.Save(dir, tk); serr != nil {
			return res, output.Errorf(jsonOut, output.ErrState,
				"task %q: removed worktree for project %q but failed to persist progress: %v", taskName, p.Name, serr)
		}
		res.RemovedWorktrees = append(res.RemovedWorktrees, p.Name)
	}

	// Phase 4: only now (every worktree removed) is it safe to delete the folder.
	if err := os.RemoveAll(dir); err != nil {
		return res, output.Errorf(jsonOut, output.ErrState,
			"task %q: removed worktrees but failed to delete task folder: %v", taskName, err)
	}
	return res, nil
}

// dropProject returns ps without the project named name, preserving order. It is
// used by runDone to persist worktree-removal progress one project at a time so a
// failed multi-project finalization can be retried without re-removing or
// re-verifying an already-removed worktree.
func dropProject(ps []task.Project, name string) []task.Project {
	out := make([]task.Project, 0, len(ps))
	for _, p := range ps {
		if p.Name != name {
			out = append(out, p)
		}
	}
	return out
}

func init() {
	var taskFlag string
	var force bool
	cmd := &cobra.Command{
		Use: "done [task]", Short: "Finish a task: verify, remove worktrees, remove the task folder",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			name := taskFlag
			if len(args) == 1 {
				name = args[0]
			}
			if name == "" {
				if name, err = resolveTask(home, ""); err != nil {
					return err
				}
			}
			res, err := runDone(home, name, force)
			if err != nil {
				return err
			}
			human := "Finished " + res.Task + " (removed " + fmt.Sprint(len(res.RemovedWorktrees)) +
				" worktree(s)); knowledge consolidated."
			return output.Emit(jsonOut, human, res)
		},
	}
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	cmd.Flags().BoolVar(&force, "force", false, "override unmerged/dirty refusal")
	RootCmd.AddCommand(cmd)
}
