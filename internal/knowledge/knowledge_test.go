package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAddCreatesTopicPageAndIndex(t *testing.T) {
	home := t.TempDir()
	p, err := Add(home, PageInput{
		Projects: []string{"front"}, Topic: "Payment Form", Category: "gotcha",
		Body: "returns 200 early", SourceTask: "add-payment", When: "2026-06-18", Tags: []string{"payment"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "payment-form.md" || !fileContains(t, p, "returns 200 early") {
		t.Errorf("bad page path/body: %s", p)
	}
	idx := filepath.Join(home, "_knowledge", "index.md")
	if !fileContains(t, idx, "payment-form") {
		t.Error("index.md missing the page")
	}
	if !fileContains(t, filepath.Join(home, "_knowledge", "log.md"), "consolidate | Payment Form") {
		t.Error("log.md missing consolidation line")
	}
}

func TestAddMergesAndPromotesToShared(t *testing.T) {
	home := t.TempDir()
	// First, a per-project page for "front".
	if _, err := Add(home, PageInput{Projects: []string{"front"}, Topic: "pay", Body: "a", SourceTask: "t1", When: "2026-06-18"}); err != nil {
		t.Fatal(err)
	}
	// An EXPLICIT multi-project add (front+back) merges into the existing front
	// page and promotes it to _shared, because the resulting facet now spans 2
	// repos. (Adding a separate single-project "back" page would NOT merge — see
	// TestAddDoesNotMergeIndependentSingleProjectPages.)
	p, err := Add(home, PageInput{Projects: []string{"front", "back"}, Topic: "pay", Body: "b", SourceTask: "t2", When: "2026-06-19"})
	if err != nil {
		t.Fatal(err)
	}
	// now spans front+back -> _shared
	if filepath.Dir(p) != filepath.Join(home, "_knowledge", "_shared") {
		t.Errorf("expected promotion to _shared, got %s", p)
	}
	if _, err := os.Stat(filepath.Join(home, "_knowledge", "front", "pay.md")); !os.IsNotExist(err) {
		t.Error("old front/pay.md should be removed after promotion")
	}
	if !fileContains(t, p, "## Update") || !fileContains(t, p, "a") || !fileContains(t, p, "b") {
		t.Error("merged body should contain both contributions")
	}
}

// TestAddDoesNotMergeIndependentSingleProjectPages proves per-project isolation:
// adding topic "pay" to "front" and separately to "back" yields TWO distinct
// per-project pages, never an accidental _shared merge across projects.
func TestAddDoesNotMergeIndependentSingleProjectPages(t *testing.T) {
	home := t.TempDir()
	pf, err := Add(home, PageInput{Projects: []string{"front"}, Topic: "pay", Body: "a", SourceTask: "t1", When: "2026-06-18"})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := Add(home, PageInput{Projects: []string{"back"}, Topic: "pay", Body: "b", SourceTask: "t2", When: "2026-06-19"})
	if err != nil {
		t.Fatal(err)
	}
	if pf != filepath.Join(home, "_knowledge", "front", "pay.md") {
		t.Errorf("front page misplaced: %s", pf)
	}
	if pb != filepath.Join(home, "_knowledge", "back", "pay.md") {
		t.Errorf("back page misplaced: %s", pb)
	}
	if _, err := os.Stat(filepath.Join(home, "_knowledge", "_shared", "pay.md")); !os.IsNotExist(err) {
		t.Error("independent single-project pages must NOT be promoted to _shared")
	}
}

// TestAddDoesNotMergeUnrelatedProjectIntoSharedPage proves shared-page scope is
// respected: with an existing _shared/pay.md owned only by front+back, adding the
// same topic for the UNRELATED project "cli" must NOT merge into that shared page.
// Instead "cli" gets its own independent cli/pay.md, and the shared page's facet
// and body are left untouched (SPEC §9 per-project isolation / shared-page scope).
func TestAddDoesNotMergeUnrelatedProjectIntoSharedPage(t *testing.T) {
	home := t.TempDir()
	// Build a _shared/pay.md owned by front+back.
	if _, err := Add(home, PageInput{Projects: []string{"front"}, Topic: "pay", Body: "front-body", SourceTask: "t1", When: "2026-06-18"}); err != nil {
		t.Fatal(err)
	}
	shared, err := Add(home, PageInput{Projects: []string{"front", "back"}, Topic: "pay", Body: "back-body", SourceTask: "t2", When: "2026-06-19"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(shared) != filepath.Join(home, "_knowledge", "_shared") {
		t.Fatalf("setup: expected _shared/pay.md, got %s", shared)
	}
	// Now add topic "pay" for the unrelated project "cli".
	pc, err := Add(home, PageInput{Projects: []string{"cli"}, Topic: "pay", Body: "cli-body", SourceTask: "t3", When: "2026-06-20"})
	if err != nil {
		t.Fatal(err)
	}
	if pc != filepath.Join(home, "_knowledge", "cli", "pay.md") {
		t.Errorf("cli page must be independent at cli/pay.md, got %s", pc)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Error("shared front+back page must survive an unrelated add")
	}
	if fileContains(t, shared, "cli-body") {
		t.Error("unrelated cli body must NOT leak into the front+back shared page")
	}
	if fileContains(t, shared, "cli") && !fileContains(t, shared, "front") {
		t.Error("shared page facet must not gain the unrelated project cli")
	}
}

// TestAddRejectsInvalidCategory proves the controlled vocabulary is enforced: a
// nonempty category outside architecture|decision|gotcha|pattern is rejected and
// no page is written.
func TestAddRejectsInvalidCategory(t *testing.T) {
	home := t.TempDir()
	if _, err := Add(home, PageInput{
		Projects: []string{"front"}, Topic: "pay", Category: "bogus",
		Body: "x", SourceTask: "t", When: "2026-06-18",
	}); err == nil {
		t.Error("expected an error for an invalid category")
	}
	if _, err := os.Stat(filepath.Join(home, "_knowledge", "front", "pay.md")); !os.IsNotExist(err) {
		t.Error("no page should be written when the category is invalid")
	}
}

// TestAddAcceptsValidAndEmptyCategory proves every allowed value (and the empty
// optional value) is accepted.
func TestAddAcceptsValidAndEmptyCategory(t *testing.T) {
	for _, cat := range []string{"", "architecture", "decision", "gotcha", "pattern"} {
		home := t.TempDir()
		if _, err := Add(home, PageInput{
			Projects: []string{"front"}, Topic: "pay", Category: cat,
			Body: "x", SourceTask: "t", When: "2026-06-18",
		}); err != nil {
			t.Errorf("category %q should be accepted: %v", cat, err)
		}
	}
}

// TestAddMergesAllScopedPagesOnPromotion proves an explicit multi-project add
// supersedes EVERY pre-existing per-project page for the topic, not just the
// first. With both front/pay.md and back/pay.md already present, promoting to
// _shared must leave neither per-project page behind (no duplicate search hits)
// and the shared page must contain all contributions.
func TestAddMergesAllScopedPagesOnPromotion(t *testing.T) {
	home := t.TempDir()
	if _, err := Add(home, PageInput{Projects: []string{"front"}, Topic: "pay", Body: "front-body", SourceTask: "t1", When: "2026-06-18"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(home, PageInput{Projects: []string{"back"}, Topic: "pay", Body: "back-body", SourceTask: "t2", When: "2026-06-19"}); err != nil {
		t.Fatal(err)
	}
	// Explicit front+back add: merges BOTH existing pages and promotes to _shared.
	p, err := Add(home, PageInput{Projects: []string{"front", "back"}, Topic: "pay", Body: "shared-body", SourceTask: "t3", When: "2026-06-20"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != filepath.Join(home, "_knowledge", "_shared") {
		t.Fatalf("expected promotion to _shared, got %s", p)
	}
	if _, err := os.Stat(filepath.Join(home, "_knowledge", "front", "pay.md")); !os.IsNotExist(err) {
		t.Error("front/pay.md must be removed after promotion")
	}
	if _, err := os.Stat(filepath.Join(home, "_knowledge", "back", "pay.md")); !os.IsNotExist(err) {
		t.Error("back/pay.md must be removed after promotion (no duplicate left behind)")
	}
	for _, want := range []string{"front-body", "back-body", "shared-body"} {
		if !fileContains(t, p, want) {
			t.Errorf("shared page missing %q", want)
		}
	}
}

// TestHasProvenanceRequiresProjectOverlap proves the consolidation gate checks the
// page's project facet, not just the source task. A page sourced from "tk" but
// scoped to the unrelated project "back" must NOT satisfy provenance for a task
// whose registered project is "front"; the same task DOES satisfy it once a page
// scoped to "front" exists. This is the regression guard for `ctx done` accepting
// any same-task page regardless of project (SPEC §7/§9).
func TestHasProvenanceRequiresProjectOverlap(t *testing.T) {
	home := t.TempDir()
	// A page sourced from task "tk" but for the UNRELATED project "back".
	if _, err := Add(home, PageInput{
		Projects: []string{"back"}, Topic: "pay", Body: "x",
		SourceTask: "tk", When: "2026-06-18",
	}); err != nil {
		t.Fatal(err)
	}
	// The task registered only "front": the "back" page must not satisfy the gate.
	ok, err := HasProvenance(home, "tk", []string{"front"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("a page for an unrelated project must NOT satisfy provenance for the task's projects")
	}
	// Now consolidate a finding for the task's actual project "front".
	if _, err := Add(home, PageInput{
		Projects: []string{"front"}, Topic: "wrap-up", Body: "y",
		SourceTask: "tk", When: "2026-06-19",
	}); err != nil {
		t.Fatal(err)
	}
	ok, err = HasProvenance(home, "tk", []string{"front"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("a page scoped to the task's registered project must satisfy provenance")
	}
}

// TestHasProvenanceEmptyProjectsAcceptsAnySameTaskPage proves the degenerate case:
// a task that registered no projects has no per-project findings to require, so any
// page sourced from it satisfies the gate.
func TestHasProvenanceEmptyProjectsAcceptsAnySameTaskPage(t *testing.T) {
	home := t.TempDir()
	if _, err := Add(home, PageInput{
		Projects: []string{"back"}, Topic: "pay", Body: "x",
		SourceTask: "tk", When: "2026-06-18",
	}); err != nil {
		t.Fatal(err)
	}
	ok, err := HasProvenance(home, "tk", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("with no registered projects, any same-task page must satisfy provenance")
	}
}

// TestFirstLineSnippetKeepsCJKValidUTF8 proves a long CJK topic body is truncated
// by runes, not bytes, so the index/search snippet stays valid UTF-8 (a byte
// slice at 80 would cut inside a multibyte Korean rune).
func TestFirstLineSnippetKeepsCJKValidUTF8(t *testing.T) {
	long := strings.Repeat("결제", 100) // 200 runes, 600 bytes
	got := firstLine(long)
	if !utf8.ValidString(got) {
		t.Errorf("snippet is not valid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 80 {
		t.Errorf("snippet rune count: got %d want 80", n)
	}
}

// TestAddRejectsPunctuationOnlyTopic proves a topic whose slug is empty (e.g.
// "...") is rejected with an error mentioning "slug-able" and no dotfile is
// created.
func TestAddRejectsPunctuationOnlyTopic(t *testing.T) {
	home := t.TempDir()
	_, err := Add(home, PageInput{
		Projects: []string{"front"}, Topic: "...", Body: "x",
		SourceTask: "t", When: "2026-06-18",
	})
	if err == nil {
		t.Fatal("expected an error for a punctuation-only topic")
	}
	if !strings.Contains(err.Error(), "slug-able") {
		t.Errorf("error message should mention \"slug-able\", got: %v", err)
	}
	// No dotfile (.md) should be created.
	if _, statErr := os.Stat(filepath.Join(home, "_knowledge", "front", ".md")); !os.IsNotExist(statErr) {
		t.Error("no .md dotfile should be written for an invalid topic")
	}
}

// TestAddDefaultsEmptyWhen proves When: "" is accepted and the written page
// contains neither "## Update ()" (empty date) nor a blank last_reviewed/when.
func TestAddDefaultsEmptyWhen(t *testing.T) {
	home := t.TempDir()
	p, err := Add(home, PageInput{
		Projects: []string{"front"}, Topic: "defaults", Body: "body text",
		SourceTask: "t", When: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fileContains(t, p, "## Update ()") {
		t.Error("page must not contain \"## Update ()\" when When is defaulted")
	}
	data, readErr := os.ReadFile(p)
	if readErr != nil {
		t.Fatalf("read page: %v", readErr)
	}
	content := string(data)
	if strings.Contains(content, "last_reviewed: \"\"") || strings.Contains(content, "last_reviewed: ''") {
		t.Error("last_reviewed must not be empty when When is defaulted")
	}
	if strings.Contains(content, "when: \"\"") || strings.Contains(content, "when: ''") {
		t.Error("when must not be empty in sources when When is defaulted")
	}
}

func fileContains(t *testing.T, path, sub string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	s := string(data)
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
