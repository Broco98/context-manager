package cli

import (
	"os"
	"strings"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
)

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// checkedTaskDir validates name as a safe single path component (rejecting
// "../victim", "_knowledge", slashes, etc.) BEFORE joining it under CTX_HOME, so
// an explicit --task value can never escape the store or shadow a reserved dir.
// It then canonicalizes the joined task dir and rejects it unless the real
// (symlink-resolved) target is contained beneath canonical CTX_HOME: a lexical
// name check alone cannot catch a task directory that exists as a symlink
// redirecting outside the store, so we resolve both ends before trusting the path
// for any read, write, Git operation, or deletion. Every code path that turns a
// task name into a directory (resolveTask, loadTask, runAdd, runStatusDetail,
// runResume, runDone, runSpec) goes through this helper, which is the single
// checked replacement for raw store.TaskDir(home, name).
func checkedTaskDir(home, name string) (string, error) {
	if err := store.ValidName(name); err != nil {
		return "", output.Errorf(jsonOut, output.ErrUsage, "invalid task name %q: %v", name, err)
	}
	if _, err := store.ValidRelPath(home, name); err != nil {
		return "", output.Errorf(jsonOut, output.ErrUsage, "invalid task name %q: %v", name, err)
	}
	return store.TaskDir(home, name), nil
}

// resolveTask returns the explicit flag if set, else the task detected from CWD.
// An explicit flag is validated here so a caller that only resolves a name (and
// then hands it to checkedTaskDir) still rejects an unsafe --task value early.
func resolveTask(home, flag string) (string, error) {
	if flag != "" {
		if err := store.ValidName(flag); err != nil {
			return "", output.Errorf(jsonOut, output.ErrUsage, "invalid task name %q: %v", flag, err)
		}
		return flag, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	loc, err := store.Locate(home, cwd)
	if err != nil {
		return "", err
	}
	if loc.Task == "" {
		return "", output.Errorf(jsonOut, output.ErrUsage, "not inside a task; pass --task")
	}
	return loc.Task, nil
}

// classifyLoadErr maps a task.Load error to the correct output error code.
// os.IsNotExist means the task.yaml is absent (NOT_FOUND); any other error
// means it is present but unreadable (STATE).
func classifyLoadErr(name string, err error) error {
	if os.IsNotExist(err) {
		return output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", name)
	}
	return output.Errorf(jsonOut, output.ErrState, "task %q is unreadable: %v", name, err)
}

// lockedLoadTask is the write-path counterpart to loadTask: it checks the task dir,
// takes the per-task exclusive lock, THEN loads. The caller MUST defer the returned
// unlock; holding it across the subsequent task.Save makes the whole
// load->modify->save sequence atomic against other ctx mutations, preventing the
// last-writer-wins loss that concurrent edits would otherwise cause. Read-only
// callers keep loadTask (no lock): AtomicWrite's rename gives readers a consistent
// snapshot without serializing them. A missing task is reported as NOT_FOUND before
// the lock is taken, so neither the task dir nor its lock file is created for it.
func lockedLoadTask(home, name string) (string, *task.Task, func(), error) {
	dir, err := checkedTaskDir(home, name)
	if err != nil {
		return "", nil, nil, err
	}
	if !dirExists(dir) {
		return "", nil, nil, classifyLoadErr(name, os.ErrNotExist)
	}
	unlock, err := store.Lock(store.TaskLockPath(dir))
	if err != nil {
		return "", nil, nil, err
	}
	tk, err := task.Load(dir)
	if err != nil {
		unlock()
		return "", nil, nil, classifyLoadErr(name, err)
	}
	return dir, tk, unlock, nil
}

func joinArgs(a []string) string { return strings.Join(a, " ") }

// homeAndTask resolves CTX_HOME and the active task (flag or CWD).
func homeAndTask(taskFlag string) (string, string, error) {
	home, err := store.Home()
	if err != nil {
		return "", "", err
	}
	name, err := resolveTask(home, taskFlag)
	return home, name, err
}
