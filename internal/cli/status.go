package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/kimhyoyeon/context-manager/internal/worktree"
	"github.com/spf13/cobra"
)

type taskRow struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Projects int    `json:"projects"`
	Progress string `json:"progress"`
}
type overviewOut struct {
	Tasks []taskRow `json:"tasks"`
}
type progressOut struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// projectDetail is the stored project metadata enriched with git truth derived
// from `git worktree list --porcelain`. WorktreeExists is true only when git
// CONFIRMS the worktree exists; LiveBranch is the branch git currently reports
// for it (blank when missing or unreadable). WorktreeError distinguishes an
// unreadable git state (the query itself failed — e.g. an invalid/unreadable
// repo) from a confirmed-absent worktree: when it is non-empty, WorktreeExists is
// false because git state could not be determined, NOT because the worktree was
// confirmed gone. This keeps `status` from reporting unknown git state as
// confirmed absence.
type projectDetail struct {
	task.Project
	WorktreeExists bool   `json:"worktree_exists"`
	LiveBranch     string `json:"live_branch"`
	WorktreeError  string `json:"worktree_error,omitempty"`
}

type detailOut struct {
	Task     string          `json:"task"`
	Status   string          `json:"status"`
	Goal     task.Goal       `json:"goal"`
	Projects []projectDetail `json:"projects"`
	Worklist []task.WorkItem `json:"worklist"`
	Progress progressOut     `json:"progress"`
}

func runLs(home string) (overviewOut, error) {
	names, err := store.ListTasks(home)
	if err != nil {
		return overviewOut{}, err
	}
	ov := overviewOut{Tasks: []taskRow{}}
	for _, n := range names {
		tk, err := task.Load(store.TaskDir(home, n))
		if err != nil {
			continue
		}
		done, total := tk.Progress()
		ov.Tasks = append(ov.Tasks, taskRow{
			Name: n, Status: string(tk.Status), Projects: len(tk.Projects),
			Progress: fmt.Sprintf("%d/%d", done, total),
		})
	}
	return ov, nil
}

func runStatusDetail(home, taskName string) (detailOut, error) {
	dir, err := checkedTaskDir(home, taskName)
	if err != nil {
		return detailOut{}, err
	}
	tk, err := task.Load(dir)
	if err != nil {
		return detailOut{}, classifyLoadErr(taskName, err)
	}
	done, total := tk.Progress()
	projects := make([]projectDetail, 0, len(tk.Projects))
	for _, p := range tk.Projects {
		pd := projectDetail{Project: p}
		// Derive git truth: does the registered worktree still exist, and what
		// branch is it actually on? A stale/missing worktree => WorktreeExists
		// false and a blank LiveBranch. A git FAILURE (unreadable/invalid repo) is
		// recorded in WorktreeError and must NOT be collapsed into a confirmed
		// "absent" result, so callers can tell unknown git state apart from a
		// worktree git positively reports as gone.
		if p.Repo != "" && p.Worktree != "" {
			e, ok, ferr := worktree.Find(p.Repo, filepath.Join(dir, p.Worktree))
			switch {
			case ferr != nil:
				pd.WorktreeError = ferr.Error()
			case ok:
				pd.WorktreeExists = true
				pd.LiveBranch = e.Branch
			}
		}
		projects = append(projects, pd)
	}
	return detailOut{
		Task: tk.Name, Status: string(tk.Status), Goal: tk.Goal,
		Projects: projects, Worklist: tk.Worklist,
		Progress: progressOut{Done: done, Total: total},
	}, nil
}

func init() {
	lsCmd := &cobra.Command{
		Use: "ls", Short: "List all tasks",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			ov, err := runLs(home)
			if err != nil {
				return err
			}
			human := fmt.Sprintf("%d task(s)", len(ov.Tasks))
			for _, r := range ov.Tasks {
				human += fmt.Sprintf("\n  %-20s %-7s %d proj  %s", r.Name, r.Status, r.Projects, r.Progress)
			}
			return output.Emit(jsonOut, human, ov)
		},
	}
	var taskFlag string
	statusCmd := &cobra.Command{
		Use: "status", Short: "Detail for the current task, or overview of all tasks",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			name := taskFlag
			if name == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				if loc, _ := store.Locate(home, cwd); loc != nil {
					name = loc.Task
				}
			}
			if name == "" { // overview
				ov, err := runLs(home)
				if err != nil {
					return err
				}
				return output.Emit(jsonOut, fmt.Sprintf("%d task(s)", len(ov.Tasks)), ov)
			}
			d, err := runStatusDetail(home, name)
			if err != nil {
				return err
			}
			human := fmt.Sprintf("%s [%s]  goal: %s  (%d/%d)", d.Task, d.Status, d.Goal.Objective, d.Progress.Done, d.Progress.Total)
			return output.Emit(jsonOut, human, d)
		},
	}
	statusCmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	RootCmd.AddCommand(lsCmd, statusCmd)
}
