package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// ValidName validates that name is a safe single path component usable as a
// task or project directory name directly under CTX_HOME (or a task dir). It
// rejects empty names, "." / "..", names containing a path separator or NUL,
// and reserved underscore-prefixed names (which collide with the reserved
// `_knowledge`/`_shared` directories and ListTasks' "_" exclusion). This stops
// values like "../other" or "_knowledge" from escaping or shadowing managed dirs.
func ValidName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name %q is not allowed", name)
	}
	if strings.HasPrefix(name, "_") {
		return fmt.Errorf("name %q is reserved (underscore-prefixed names are not allowed)", name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("name %q contains an invalid NUL byte", name)
	}
	if strings.ContainsAny(name, "/\\") || name != filepath.Base(name) {
		return fmt.Errorf("name %q must be a single path component (no slashes)", name)
	}
	return nil
}

// resolveExisting returns the canonical (symlink-resolved) form of path. Because
// path may not exist yet (e.g. a spec.md or wt about to be created), it resolves
// the LONGEST existing ancestor with filepath.EvalSymlinks and re-appends the
// not-yet-existing trailing components, so a symlinked ancestor is followed to its
// real location while a path whose leaf does not exist still canonicalizes.
func resolveExisting(path string) (string, error) {
	clean := filepath.Clean(path)
	rest := ""
	for {
		if resolved, err := filepath.EvalSymlinks(clean); err == nil {
			return filepath.Join(resolved, rest), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(clean)
		if parent == clean {
			// Reached the filesystem root without an existing ancestor; nothing to
			// resolve, so the cleaned input is already its own canonical form.
			return filepath.Join(clean, rest), nil
		}
		rest = filepath.Join(filepath.Base(clean), rest)
		clean = parent
	}
}

// ValidRelPath validates that rel is a relative path contained beneath base and
// returns its CANONICAL absolute form. It rejects absolute paths and any path that
// escapes base via "..", so a stored Spec/Worktree value can never point outside
// its task directory before a read, write, Git operation, or deletion. Both base
// and the joined target are canonicalized via resolveExisting BEFORE the
// containment check, so a symlinked managed component (a task, project, wt, or
// _knowledge dir that redirects elsewhere) is followed to its real location and
// rejected when that location is no longer beneath the canonical base — lexical
// "../" checks alone would miss a symlink that points outside the store.
func ValidRelPath(base, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q must be relative", rel)
	}
	canonBase, err := resolveExisting(base)
	if err != nil {
		return "", err
	}
	abs, err := resolveExisting(filepath.Join(canonBase, rel))
	if err != nil {
		return "", err
	}
	if abs != canonBase && !strings.HasPrefix(abs, canonBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes its task directory", rel)
	}
	return abs, nil
}

func Home() (string, error) {
	if v := os.Getenv("CTX_HOME"); v != "" {
		return v, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".ctx"), nil
}

func TaskDir(home, name string) string { return filepath.Join(home, name) }
func KnowledgeDir(home string) string  { return filepath.Join(home, "_knowledge") }
func SharedDir(home string) string     { return filepath.Join(KnowledgeDir(home), "_shared") }

func EnsureDir(path string) error { return os.MkdirAll(path, 0o755) }

// AtomicWrite writes data to path via a temp file + rename, creating parents.
func AtomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// TaskLockPath returns the per-task advisory-lock file path. It is a DEDICATED file,
// never the renamed task.yaml: AtomicWrite swaps task.yaml's inode via rename, so a
// Flock taken on task.yaml itself would survive on a now-orphaned inode. Locking a
// file that is never renamed keeps the lock meaningful across the whole save.
func TaskLockPath(taskDir string) string { return filepath.Join(taskDir, ".task.lock") }

// Lock takes a blocking exclusive advisory lock (flock) on lockPath and returns a
// release function that unlocks and closes it. It serializes the load->modify->save
// critical section across concurrent ctx processes so a slower writer cannot clobber
// a faster one's change (last-writer-wins). The lock is advisory and Unix-only
// (syscall.Flock); every ctx mutation cooperates by taking it. The lock lives on the
// open file description, so it auto-releases on process exit and stays valid even if
// the lock file is unlinked out from under it (e.g. `ctx done` deleting the task dir).
// The caller is responsible for ensuring lockPath's parent (the task dir) exists.
func Lock(lockPath string) (func(), error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// ListTasks returns names of directories under home that contain a task.yaml,
// excluding names beginning with "_", sorted ascending.
func ListTasks(home string) ([]string, error) {
	entries, err := os.ReadDir(home)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		if _, err := os.Stat(filepath.Join(home, e.Name(), "task.yaml")); err == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Note: "fmt" and "strings" are already in the import block from Task 3 Step 3
// (added for ValidName/ValidRelPath), so the Locate code below needs no new imports.

type Location struct {
	Home    string
	Task    string
	Project string
	TaskDir string
}

// Locate walks up from cwd to the first ancestor containing task.yaml, but never
// past the CTX_HOME boundary. It returns {Home} (Task == "") when cwd is outside
// home or when no managed task is found within home, so a stray task.yaml outside
// CTX_HOME is never mistaken for a ctx task. The walk stops once it reaches the
// cleaned home dir: tasks live directly under home, so home itself is the last
// directory inspected.
func Locate(home, cwd string) (*Location, error) {
	// Canonicalize BOTH home and cwd via symlink resolution before the boundary
	// check and walk: a symlinked CTX_HOME or a resolved cwd (e.g. macOS /tmp ->
	// /private/tmp) must not make Locate wrongly report "no task". resolveExisting
	// follows symlinks of existing ancestors with a Clean fallback for paths that
	// do not exist yet.
	cleanHome, err := resolveExisting(home)
	if err != nil {
		return nil, err
	}
	canonCwd, err := resolveExisting(cwd)
	if err != nil {
		return nil, err
	}
	dir := canonCwd

	// cwd must be within home (home itself counts). Use the boundary-safe prefix
	// check so "/a/.ctx-other" is not treated as inside "/a/.ctx".
	if dir != cleanHome && !strings.HasPrefix(dir, cleanHome+string(filepath.Separator)) {
		return &Location{Home: home}, nil
	}

	for {
		// Only accept a task.yaml whose directory is a DIRECT child of home: tasks
		// live directly under home by contract, so a task.yaml found deeper (e.g. a
		// nested repo inside a worktree) is NOT a ctx task — keep walking up.
		if filepath.Dir(dir) == cleanHome {
			if _, err := os.Stat(filepath.Join(dir, "task.yaml")); err == nil {
				loc := &Location{Home: home, TaskDir: dir, Task: filepath.Base(dir)}
				if rel, rerr := filepath.Rel(dir, canonCwd); rerr == nil && rel != "." {
					parts := strings.Split(rel, string(filepath.Separator))
					if len(parts) > 0 && parts[0] != ".." && parts[0] != "" {
						loc.Project = parts[0]
					}
				}
				return loc, nil
			}
		}
		// Stop at the home boundary: tasks live directly under home, so home is
		// the last directory we inspect. Never walk to the filesystem root.
		if dir == cleanHome {
			return &Location{Home: home}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return &Location{Home: home}, nil
		}
		dir = parent
	}
}
