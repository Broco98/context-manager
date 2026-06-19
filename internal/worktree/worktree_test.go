package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// newRepo creates a repo with one commit on branch "main".
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644)
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestAddStatusRemove(t *testing.T) {
	gitOrSkip(t)
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")

	if err := Add(repo, wt, "feat/x", "main"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "README")); err != nil {
		t.Fatalf("worktree not checked out: %v", err)
	}

	st, err := Status(wt, "main")
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "feat/x" || st.Dirty || st.Ahead != 0 {
		t.Errorf("unexpected clean state: %+v", st)
	}

	// dirty + one commit ahead
	_ = os.WriteFile(filepath.Join(wt, "new.txt"), []byte("y"), 0o644)
	st, _ = Status(wt, "main")
	if !st.Dirty {
		t.Error("expected dirty after new file")
	}

	if err := Remove(repo, wt, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Error("worktree dir should be gone")
	}
}

func TestDefaultBranch(t *testing.T) {
	gitOrSkip(t)
	repo := newRepo(t)
	b, err := DefaultBranch(repo)
	if err != nil || b != "main" {
		t.Errorf("got %q err=%v want main", b, err)
	}
}

// TestDefaultBranchIgnoresCurrentCheckout proves DefaultBranch returns the
// repository default ("main") even when HEAD is on a different feature branch.
func TestDefaultBranchOnFeatureBranchStillReturnsDefault(t *testing.T) {
	gitOrSkip(t)
	repo := newRepo(t) // default + current is "main"
	// Switch the checkout to a feature branch so HEAD != default.
	cmd := exec.Command("git", "checkout", "-b", "feature/x")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout: %v\n%s", err, out)
	}
	b, err := DefaultBranch(repo)
	if err != nil || b != "main" {
		t.Errorf("got %q err=%v want main (must ignore current feature checkout)", b, err)
	}
}

// TestDefaultBranchPreservesSlashInRemoteDefault proves a remote default whose
// name contains a slash (e.g. origin/HEAD -> "origin/release/1.0") is returned
// whole as "release/1.0", not truncated to "1.0" (which would be a nonexistent base).
func TestDefaultBranchPreservesSlashInRemoteDefault(t *testing.T) {
	gitOrSkip(t)
	origin := newRepo(t) // bare-ish origin with default "main"

	// Create the slash-containing default branch in the origin and make it HEAD.
	for _, args := range [][]string{
		{"branch", "release/1.0"},
		{"symbolic-ref", "HEAD", "refs/heads/release/1.0"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = origin
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Clone so the clone gets a proper refs/remotes/origin/HEAD pointing at
	// the origin's default (release/1.0).
	clone := filepath.Join(t.TempDir(), "clone")
	cmd := exec.Command("git", "clone", origin, clone)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}

	b, err := DefaultBranch(clone)
	if err != nil || b != "release/1.0" {
		t.Errorf("got %q err=%v want release/1.0 (full slash-containing default)", b, err)
	}
}

// TestDefaultBranchErrorsWhenUndeterminable proves DefaultBranch refuses to guess
// when there is no origin/HEAD and no local main/master/develop. A repo whose only
// branch is a nonstandard default ("trunk"), with HEAD switched to a feature
// branch, must yield an ERROR (requiring an explicit --base) rather than silently
// returning the current feature checkout as the base.
func TestDefaultBranchErrorsWhenUndeterminable(t *testing.T) {
	gitOrSkip(t)
	repo := newRepo(t) // default + current is "main"
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Rename the only branch to a nonstandard default, then add a feature branch
	// and check it out, so neither main/master/develop exists and HEAD != default.
	run("branch", "-m", "main", "trunk")
	run("checkout", "-b", "feature/x")
	if b, err := DefaultBranch(repo); err == nil {
		t.Errorf("expected an error when the default branch cannot be determined, got %q", b)
	}
}

// TestBranchStatusMeasuresRegisteredBranchNotHead proves BranchStatus reports
// ahead/behind for the registered branch ref (refs/heads/feat/x) against base,
// independent of the worktree's current HEAD. After committing on feat/x and then
// switching HEAD to a clean merged branch, Status (HEAD-based) sees Ahead == 0 but
// BranchStatus still reports the unmerged commit on feat/x.
func TestBranchStatusMeasuresRegisteredBranchNotHead(t *testing.T) {
	gitOrSkip(t)
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := Add(repo, wt, "feat/x", "main"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = wt
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// One unmerged commit on the registered branch feat/x.
	_ = os.WriteFile(filepath.Join(wt, "new.txt"), []byte("y"), 0o644)
	run("add", ".")
	run("commit", "-m", "wip")

	bs, err := BranchStatus(wt, "feat/x", "main")
	if err != nil {
		t.Fatal(err)
	}
	if bs.Ahead != 1 {
		t.Errorf("BranchStatus ahead: got %d want 1", bs.Ahead)
	}

	// Switch HEAD to a clean branch off main; HEAD-based Status now sees Ahead==0.
	run("checkout", "-b", "side", "main")
	head, err := Status(wt, "main")
	if err != nil {
		t.Fatal(err)
	}
	if head.Ahead != 0 {
		t.Errorf("HEAD Status ahead after switch: got %d want 0", head.Ahead)
	}
	// BranchStatus must STILL see feat/x as unmerged.
	bs2, err := BranchStatus(wt, "feat/x", "main")
	if err != nil {
		t.Fatal(err)
	}
	if bs2.Ahead != 1 {
		t.Errorf("BranchStatus must track feat/x regardless of HEAD: got %d want 1", bs2.Ahead)
	}

	// A nonexistent registered branch must be a fail-closed error.
	if _, err := BranchStatus(wt, "feat/missing", "main"); err == nil {
		t.Error("BranchStatus on a missing branch must return an error (fail closed)")
	}
}

// TestListAndFindReportGitTruth proves List/Find read worktree existence and the
// current branch from git itself: a registered worktree is found with its branch,
// and a path git does not track is reported missing (stale metadata).
func TestListAndFindReportGitTruth(t *testing.T) {
	gitOrSkip(t)
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := Add(repo, wt, "feat/x", "main"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries, err := List(repo)
	if err != nil {
		t.Fatal(err)
	}
	// Expect the main repo worktree plus the added one.
	var found *Entry
	for i := range entries {
		if entries[i].Branch == "feat/x" {
			found = &entries[i]
		}
	}
	if found == nil {
		t.Fatalf("added worktree not reported by List: %+v", entries)
	}

	e, ok, err := Find(repo, wt)
	if err != nil || !ok {
		t.Fatalf("Find should locate the live worktree: ok=%v err=%v", ok, err)
	}
	if e.Branch != "feat/x" {
		t.Errorf("Find branch: got %q want feat/x", e.Branch)
	}

	// A path git does not track must be reported as missing (stale metadata).
	if _, ok, err := Find(repo, filepath.Join(t.TempDir(), "ghost-wt")); err != nil || ok {
		t.Errorf("Find on an untracked path should be missing: ok=%v err=%v", ok, err)
	}

	// After removal, git truth says the worktree no longer exists.
	if err := Remove(repo, wt, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok, err := Find(repo, wt); err != nil || ok {
		t.Errorf("Find after Remove should be missing: ok=%v err=%v", ok, err)
	}
}

// TestCheckBaseRefRejectsLeadingDash fires the leading-dash guard's error branch
// directly (shipped without a regression test; error branch was never exercised).
func TestCheckBaseRefRejectsLeadingDash(t *testing.T) {
	if err := checkBaseRef("-rf"); err == nil {
		t.Error(`checkBaseRef("-rf") = nil, want error (git would misparse it as a flag)`)
	}
	if err := checkBaseRef("main"); err != nil {
		t.Errorf(`checkBaseRef("main") = %v, want nil`, err)
	}
}

// TestAddRejectsLeadingDashBranch covers Add's inline branch guard (the branch
// sits before git's "--" separator, so it needs its own flag-injection guard).
func TestAddRejectsLeadingDashBranch(t *testing.T) {
	gitOrSkip(t)
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := Add(repo, wt, "--evil", "main"); err == nil {
		t.Error(`Add branch "--evil" = nil, want error (flag-injection guard)`)
	}
}

// TestStatusAndBranchStatusRejectLeadingDashBase reaches checkBaseRef through the
// public API both callers use, proving a "-"-prefixed base is rejected before it
// reaches the rev-list revspec.
func TestStatusAndBranchStatusRejectLeadingDashBase(t *testing.T) {
	gitOrSkip(t)
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := Add(repo, wt, "feat/x", "main"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := Status(wt, "-rf"); err == nil {
		t.Error(`Status base "-rf" = nil, want error (checkBaseRef via public API)`)
	}
	if _, err := BranchStatus(wt, "feat/x", "-rf"); err == nil {
		t.Error(`BranchStatus base "-rf" = nil, want error`)
	}
}
