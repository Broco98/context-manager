package cli

import (
	"os"
	"strings"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
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
