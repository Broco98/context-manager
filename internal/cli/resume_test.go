package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/goal"
	"github.com/kimhyoyeon/context-manager/internal/resume"
	"github.com/kimhyoyeon/context-manager/internal/task"
)

// TestRunResumeResolvesNamedTask confirms runResume targets the named task even
// when the process is not inside that task's directory (the positional path).
func TestRunResumeResolvesNamedTask(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "alpha")
	seedTask(t, home, "beta")
	b, err := runResume(home, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if b.Task != "beta" {
		t.Errorf("positional task ignored: got %q want beta", b.Task)
	}
}

// TestResumeLiveGitState exercises the real-git path: it registers a project,
// then verifies the resume bundle reports the live branch, dirty flag, ahead,
// and behind counts derived from the worktree.
func TestResumeLiveGitState(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo}); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(home, "tk", "front", "wt")

	// Advance "main" in the source repo so the worktree branch is BEHIND by 1.
	_ = os.WriteFile(filepath.Join(repo, "ahead.txt"), []byte("a"), 0o644)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "advance main")

	// Add a committed change in the worktree so it is AHEAD by 1...
	_ = os.WriteFile(filepath.Join(wt, "feat.txt"), []byte("b"), 0o644)
	runGit(t, wt, "add", "feat.txt")
	runGit(t, wt, "commit", "-m", "wip")
	// ...and an uncommitted change so it is DIRTY.
	_ = os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("c"), 0o644)

	b, err := runResume(home, "tk")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Projects) != 1 {
		t.Fatalf("expected 1 project state, got %+v", b.Projects)
	}
	ps := b.Projects[0]
	if !ps.Available {
		t.Errorf("expected Available=true for a live worktree: %+v", ps)
	}
	if ps.Branch != "feat/tk" {
		t.Errorf("branch: got %q want feat/tk", ps.Branch)
	}
	if !ps.Dirty {
		t.Error("expected dirty worktree")
	}
	if ps.Ahead != 1 {
		t.Errorf("ahead: got %d want 1", ps.Ahead)
	}
	if ps.Behind != 1 {
		t.Errorf("behind: got %d want 1", ps.Behind)
	}
}

// TestResumeMissingWorktreeReportsUnavailable proves resume does NOT fabricate
// live state for a missing/unreadable worktree: it must mark the project
// Available=false with a non-empty Error and leave Dirty/Ahead/Behind at zero,
// rather than silently reporting a clean worktree.
func TestResumeMissingWorktreeReportsUnavailable(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo}); err != nil {
		t.Fatal(err)
	}
	// Remove the worktree directory so live status can no longer be read.
	if err := os.RemoveAll(filepath.Join(home, "tk", "front", "wt")); err != nil {
		t.Fatal(err)
	}
	b, err := runResume(home, "tk")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Projects) != 1 {
		t.Fatalf("expected 1 project state, got %+v", b.Projects)
	}
	ps := b.Projects[0]
	if ps.Available {
		t.Error("expected Available=false for a missing worktree")
	}
	if ps.Error == "" {
		t.Error("expected a non-empty Error explaining the missing worktree")
	}
	if ps.Dirty || ps.Ahead != 0 || ps.Behind != 0 {
		t.Errorf("missing worktree must not fabricate live state: %+v", ps)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestResumeHumanRendersCompleteBundle proves text mode (not just --json) reloads
// the FULL context: goal, Background, Plan, worklist+progress, next, recent
// journal, and per-project live state — including an explicit "unavailable" line
// for a project whose git state could not be read (never fabricated clean state).
func TestResumeHumanRendersCompleteBundle(t *testing.T) {
	b := &resume.Bundle{
		Task: "add-payment", Status: "active",
		Goal:       task.Goal{Objective: "add pay", DoneWhen: "e2e pass"},
		Background: "users want pay", Plan: "wire the form",
		Worklist: []task.WorkItem{
			{ID: 1, Text: "ui", Status: "done"},
			{ID: 2, Text: "api", Status: "doing"},
		},
		Next:          "api",
		RecentJournal: []string{"- did X", "- did Y"},
		Projects: []resume.ProjState{
			{Name: "front", Status: "in_progress", Branch: "feat/x", Base: "develop", Dirty: true, Ahead: 2, Behind: 1, Available: true},
			{Name: "back", Status: "planned", Branch: "feat/x", Base: "develop", Available: false, Error: "worktree missing"},
		},
	}
	out := resumeHuman(b)
	for _, want := range []string{
		"add-payment", "add pay", "e2e pass",
		"## Background", "users want pay",
		"## Plan", "wire the form",
		"## Worklist (1/2 done)", "#2 [doing] api",
		"Next: api",
		"## Recent Journal", "did X", "did Y",
		"## Projects", "front", "ahead=2", "behind=1",
		"back", "git state unavailable: worktree missing",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text resume missing %q in:\n%s", want, out)
		}
	}
}

// TestResumeJSONGolden locks the resume bundle schema using a project-free task
// (no live git state) so the contract is deterministic.
func TestResumeJSONGolden(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	b, err := runResume(home, "add-payment")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "resume.json", b)
}

// TestGoalJSONGolden locks the `ctx goal` JSON schema (task.Goal).
func TestGoalJSONGolden(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	_, tk, err := loadTask(home, "add-payment")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "goal.json", tk.Goal)
}

// TestGoalHandoffTextGolden locks the `ctx goal --handoff` text contract.
func TestGoalHandoffTextGolden(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	_, tk, err := loadTask(home, "add-payment")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "goal_handoff.txt", goal.Handoff(tk))
}
