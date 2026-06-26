package cli

import (
	"os"
	"path/filepath"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/kimhyoyeon/context-manager/internal/worktree"
	"github.com/spf13/cobra"
)

type addOpts struct {
	Project, Repo, Branch, Base string
}

// saveTask is the seam runAdd uses for the FINAL task.yaml write. It exists so a
// test can force the save to fail AFTER the worktree and spec are created, exercising
// the rollback path that real read-only-directory failures cannot reach reliably.
var saveTask = task.Save

func runAdd(home, taskName string, o addOpts) (*task.Project, error) {
	if o.Repo == "" {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "--repo is required")
	}
	// The project name becomes a directory component under the task dir (and the
	// worktree/spec relative paths), so it must be a safe single component.
	if err := store.ValidName(o.Project); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "invalid project name: %v", err)
	}
	taskDir, err := checkedTaskDir(home, taskName)
	if err != nil {
		return nil, err
	}
	if !dirExists(taskDir) {
		return nil, output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", taskName)
	}
	// Hold the per-task lock across the whole conflict-check -> worktree-create ->
	// save critical section (and the rollback defer below): two concurrent `ctx add`
	// on the same task must serialize, or both pass the "already registered" check on
	// a stale snapshot and the second save clobbers the first's project (the very race
	// that orphaned a worktree while only one project survived in task.yaml).
	unlock, err := store.Lock(store.TaskLockPath(taskDir))
	if err != nil {
		return nil, err
	}
	defer unlock()
	tk, err := task.Load(taskDir)
	if err != nil {
		return nil, output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", taskName)
	}
	if tk.Project(o.Project) != nil {
		return nil, output.Errorf(jsonOut, output.ErrConflict, "project %q already registered", o.Project)
	}
	branch := o.Branch
	if branch == "" {
		branch = "feat/" + taskName
	}
	base := o.Base
	if base == "" {
		b, derr := worktree.DefaultBranch(o.Repo)
		if derr != nil {
			// DefaultBranch refuses to guess when no default can be resolved;
			// surface its actionable message so the user passes --base explicitly
			// rather than branching off an unrelated feature checkout.
			return nil, output.Errorf(jsonOut, output.ErrUsage, "%v", derr)
		}
		base = b
	}
	wtRel := filepath.Join(o.Project, "wt")
	specRel := filepath.Join(o.Project, "spec.md")
	projDir := filepath.Join(taskDir, o.Project)
	wtAbs := filepath.Join(taskDir, wtRel)
	specAbs := filepath.Join(taskDir, specRel)
	// Guard the project dir, worktree, and spec targets against the task dir BEFORE
	// any worktree creation, write, or rollback deletion. ValidRelPath canonicalizes
	// both ends via symlink resolution, so a symlinked task/project component (one
	// redirecting outside CTX_HOME) is rejected here; the lexical paths above still
	// address the same real inodes for every legitimate (non-escaping) mutation.
	if _, err := store.ValidRelPath(taskDir, o.Project); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "invalid project path: %v", err)
	}
	if _, err := store.ValidRelPath(taskDir, wtRel); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "invalid worktree path: %v", err)
	}
	if _, err := store.ValidRelPath(taskDir, specRel); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "invalid spec path: %v", err)
	}

	// Snapshot any pre-existing regular spec.md so rollback can RESTORE it rather
	// than delete it. The project facet is not yet registered in task.yaml (checked
	// above), but a user could still have hand-written a spec at this path; AtomicWrite
	// would overwrite it and the rollback below would then os.Remove it, destroying
	// user content. priorSpec holds its bytes when it existed as a regular file;
	// hadPriorSpec records that something was there (file or otherwise) so rollback
	// never deletes a path runAdd did not create.
	var priorSpec []byte
	hadPriorSpec := false
	if info, statErr := os.Stat(specAbs); statErr == nil {
		hadPriorSpec = true
		if info.Mode().IsRegular() {
			b, rerr := os.ReadFile(specAbs)
			if rerr != nil {
				return nil, rerr
			}
			priorSpec = b
		}
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	// Track whether WE created the project dir so rollback removes only artifacts
	// runAdd created. EnsureDir accepts a pre-existing dir, so a later failure must
	// not os.RemoveAll a directory (and any pre-existing files) the user already had.
	createdProjDir := false
	if _, statErr := os.Stat(projDir); os.IsNotExist(statErr) {
		createdProjDir = true
	}
	if err := store.EnsureDir(projDir); err != nil {
		return nil, err
	}
	if err := worktree.Add(o.Repo, wtAbs, branch, base); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrGit, "worktree add failed: %v", err)
	}
	// The git worktree now exists but is NOT yet recorded in task.yaml, so ctx can
	// neither discover nor clean it up. Roll back ONLY what runAdd created on any
	// later failure; `committed` is set true only once task.Save records the
	// project, after which the worktree is owned by task.yaml and must survive.
	committed := false
	defer func() {
		if committed {
			return
		}
		// Always undo the worktree (runAdd created it just above). For the spec:
		// restore a pre-existing regular file's bytes, leave any other pre-existing
		// path untouched, and only os.Remove a spec runAdd itself created. Remove the
		// project dir only if runAdd created it — never delete a pre-existing project
		// directory or its prior contents.
		_ = worktree.Remove(o.Repo, wtAbs, true)
		_ = os.RemoveAll(wtAbs)
		switch {
		case priorSpec != nil:
			_ = store.AtomicWrite(specAbs, priorSpec)
		case !hadPriorSpec:
			_ = os.Remove(specAbs)
		}
		if createdProjDir {
			_ = os.RemoveAll(projDir)
		}
	}()
	specBody := "# " + taskName + " — " + o.Project + " spec\n\n(Write the spec for this project's slice of the task here.)\n"
	if err := store.AtomicWrite(specAbs, []byte(specBody)); err != nil {
		return nil, err
	}
	p := task.Project{
		Name: o.Project, Repo: o.Repo, Base: base, Branch: branch,
		Worktree: wtRel, Spec: specRel, Status: "planned",
	}
	tk.Projects = append(tk.Projects, p)
	if err := saveTask(taskDir, tk); err != nil {
		return nil, err
	}
	committed = true
	return &p, nil
}

func init() {
	o := addOpts{}
	var taskFlag string
	cmd := &cobra.Command{
		Use:   "add <project>",
		Short: "Register a project: create its worktree + spec under the current task",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			taskName, err := resolveTask(home, taskFlag)
			if err != nil {
				return err
			}
			o.Project = args[0]
			p, err := runAdd(home, taskName, o)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, "Added project "+p.Name+" (branch "+p.Branch+" from "+p.Base+")", p)
		},
	}
	cmd.Flags().StringVar(&o.Repo, "repo", "", "path to the project's git repo (required)")
	cmd.Flags().StringVar(&o.Branch, "branch", "", "branch name (default feat/<task>)")
	cmd.Flags().StringVar(&o.Base, "base", "", "base branch (default: repo's default branch; required if it cannot be determined)")
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	RootCmd.AddCommand(cmd)
}
