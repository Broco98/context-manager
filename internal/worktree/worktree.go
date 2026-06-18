package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// checkBaseRef rejects a base ref beginning with '-', which git would misparse
// as a flag in the rev-list revspec (e.g. base+"...HEAD").
func checkBaseRef(base string) error {
	if strings.HasPrefix(base, "-") {
		return fmt.Errorf("invalid base ref %q", base)
	}
	return nil
}

func Add(repo, worktreePath, branch, base string) error {
	if branch == "" || base == "" {
		return fmt.Errorf("branch and base are required")
	}
	// A branch passed as `-b <branch>` sits before the `--` separator, so git
	// sees it before the separator takes effect and would misparse it as a flag.
	// The `--` separator only protects `base`; an explicit guard is required for
	// `branch`.
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch %q", branch)
	}
	abs, err := filepath.Abs(worktreePath)
	if err != nil {
		return err
	}
	// The "--" separator stops git from misparsing `base` as a flag.
	_, err = git(repo, "worktree", "add", abs, "-b", branch, "--", base)
	return err
}

func Remove(repo, worktreePath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	abs, err := filepath.Abs(worktreePath)
	if err != nil {
		return err
	}
	args = append(args, abs)
	_, err = git(repo, args...)
	return err
}

// DefaultBranch resolves the repository's default branch, NOT the currently
// checked-out branch. It prefers the remote's published default (origin/HEAD),
// then falls back to a local main/master/develop. When none of those resolve it
// returns an actionable error rather than the current HEAD: a worktree created
// from an unrelated feature checkout would diverge from the true integration
// branch (SPEC §7), so the caller must pass an explicit --base instead.
func DefaultBranch(repo string) (string, error) {
	// 1) Remote default: refs/remotes/origin/HEAD -> "origin/main", or
	// "origin/release/1.0" for a slash-containing default. Strip ONLY the
	// "origin/" prefix and keep the full remaining branch name; LastIndex would
	// wrongly truncate "origin/release/1.0" to "1.0", picking a nonexistent base.
	if ref, err := git(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		if b := strings.TrimPrefix(ref, "origin/"); b != "" {
			return b, nil
		}
		return ref, nil
	}
	// 2) Common local defaults, in priority order.
	for _, cand := range []string{"main", "master", "develop"} {
		if _, err := git(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+cand); err == nil {
			return cand, nil
		}
	}
	// 3) Refuse to guess. Returning the current HEAD here would let `ctx add`
	// branch a new worktree off an unrelated feature checkout; require --base.
	return "", fmt.Errorf("cannot determine default branch for %s: no origin/HEAD and no local main/master/develop; pass --base explicitly", repo)
}

type State struct {
	Branch string `json:"branch"`
	Dirty  bool   `json:"dirty"`
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
}

func Status(worktreePath, base string) (*State, error) {
	branch, err := git(worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, err
	}
	porcelain, err := git(worktreePath, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	st := &State{Branch: branch, Dirty: strings.TrimSpace(porcelain) != ""}
	if base != "" {
		if err := checkBaseRef(base); err != nil {
			return nil, err
		}
		// left = commits in base not HEAD (behind), right = in HEAD not base (ahead).
		// A failed rev-list, an unexpected field count, or an unparseable count
		// MUST be an error: ahead/behind drive `ctx resume` (live-state availability)
		// and `ctx done` (merge verification), so silently reporting zero would let
		// done delete unmerged work and resume claim live state it never read.
		counts, err := git(worktreePath, "rev-list", "--left-right", "--count", base+"...HEAD")
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(counts)
		if len(fields) != 2 {
			return nil, fmt.Errorf("rev-list --left-right --count %s...HEAD: expected 2 fields, got %q", base, counts)
		}
		behind, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, fmt.Errorf("rev-list behind count %q: %w", fields[0], err)
		}
		ahead, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("rev-list ahead count %q: %w", fields[1], err)
		}
		st.Behind, st.Ahead = behind, ahead
	}
	return st, nil
}

// BranchStatus reports a branch's dirty/ahead/behind status computed against a
// REGISTERED ref (refs/heads/branch), not the worktree's current HEAD. `ctx done`
// uses this so a worktree that was switched or detached cannot hide unmerged work
// on the task's registered branch: ahead/behind are measured for the branch ref
// itself, and Dirty still reflects the worktree's tree (uncommitted changes block
// finalization regardless of which branch is checked out). The branch ref MUST
// exist; a missing/unreadable branch is an error (fail closed), never silent zero.
func BranchStatus(worktreePath, branch, base string) (*State, error) {
	if branch == "" {
		return nil, fmt.Errorf("branch must not be empty")
	}
	ref := "refs/heads/" + branch
	if _, err := git(worktreePath, "rev-parse", "--verify", "--quiet", ref); err != nil {
		return nil, fmt.Errorf("registered branch %q not found: %w", branch, err)
	}
	porcelain, err := git(worktreePath, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	st := &State{Branch: branch, Dirty: strings.TrimSpace(porcelain) != ""}
	if base != "" {
		if err := checkBaseRef(base); err != nil {
			return nil, err
		}
		// left = commits in base not branch (behind), right = in branch not base (ahead).
		counts, err := git(worktreePath, "rev-list", "--left-right", "--count", base+"..."+ref)
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(counts)
		if len(fields) != 2 {
			return nil, fmt.Errorf("rev-list --left-right --count %s...%s: expected 2 fields, got %q", base, ref, counts)
		}
		behind, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, fmt.Errorf("rev-list behind count %q: %w", fields[0], err)
		}
		ahead, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("rev-list ahead count %q: %w", fields[1], err)
		}
		st.Behind, st.Ahead = behind, ahead
	}
	return st, nil
}

// Entry is one worktree as reported by `git worktree list --porcelain`.
type Entry struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`   // short branch name, "" when detached/bare
	Head     string `json:"head"`     // commit SHA HEAD points at
	Detached bool   `json:"detached"` // HEAD detached (no branch)
	Bare     bool   `json:"bare"`     // the main bare worktree
	Prunable bool   `json:"prunable"` // git reports the entry as removable (stale)
}

// List parses `git worktree list --porcelain` into entries. This is the source
// of *git truth* for which worktrees exist and what branch each is on, so callers
// can detect stale or missing task.yaml worktree metadata. `--expire=now` makes
// git mark an entry whose directory is gone as `prunable` immediately, rather
// than waiting for the default grace period, so deletions are detected at once.
func List(repo string) ([]Entry, error) {
	out, err := git(repo, "worktree", "list", "--porcelain", "--expire=now")
	if err != nil {
		// Older git may not support --expire on `worktree list`; retry without it.
		expireErr := err
		out, err = git(repo, "worktree", "list", "--porcelain")
		if err != nil {
			return nil, fmt.Errorf("worktree list failed (with --expire=now: %v; without: %w)", expireErr, err)
		}
	}
	var entries []Entry
	var cur *Entry
	flush := func() {
		if cur != nil {
			entries = append(entries, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			p := strings.TrimPrefix(line, "worktree ")
			if abs, aerr := filepath.Abs(p); aerr == nil {
				p = abs
			}
			cur = &Entry{Path: filepath.Clean(p)}
		case cur == nil:
			// ignore stray lines before the first "worktree " header
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "bare":
			cur.Bare = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			// `prunable` (with an optional reason after a space) means git
			// considers this entry stale/removable, e.g. its directory is gone.
			cur.Prunable = true
		}
	}
	flush()
	return entries, nil
}

// Find returns the registered worktree whose path equals worktreePath (compared
// as cleaned absolute paths). The bool is false when git knows of no such
// worktree — i.e. the task.yaml metadata is stale or the worktree is missing.
// Removing a worktree directory out-of-band does NOT immediately drop git's
// administrative entry, so an entry that git reports as `prunable` or whose
// directory no longer exists on disk is treated as missing.
func Find(repo, worktreePath string) (*Entry, bool, error) {
	target := worktreePath
	if abs, err := filepath.Abs(worktreePath); err == nil {
		target = abs
	}
	// git reports worktree paths with symlinks resolved (e.g. on macOS /tmp and
	// /var/folders are symlinks under /private), but filepath.Abs does not resolve
	// them. Canonicalize the target the same way so the comparison matches git
	// truth. EvalSymlinks fails for nonexistent paths (a stale/ghost path), where
	// the cleaned absolute form is the right fallback and still won't match.
	if real, err := filepath.EvalSymlinks(target); err == nil {
		target = real
	}
	target = filepath.Clean(target)
	entries, err := List(repo)
	if err != nil {
		return nil, false, err
	}
	for i := range entries {
		if entries[i].Path != target {
			continue
		}
		if entries[i].Prunable {
			return nil, false, nil
		}
		if info, statErr := os.Stat(entries[i].Path); statErr != nil || !info.IsDir() {
			// Directory removed out-of-band: the metadata is stale.
			return nil, false, nil
		}
		return &entries[i], true, nil
	}
	return nil, false, nil
}
