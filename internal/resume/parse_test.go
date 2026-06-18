package resume

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleMD = `# add-payment

## Background
users want pay

## Plan

## Decisions
- chose stripe

## Journal
- [2026-06-18] started
- [2026-06-18] front done
`

func TestParseSectionsAndMissing(t *testing.T) {
	m := ParseSections(sampleMD)
	if Section(m, "Background") != "users want pay" {
		t.Errorf("background=%q", Section(m, "Background"))
	}
	if Section(m, "Plan") != "MISSING" {
		t.Errorf("empty Plan should be MISSING, got %q", Section(m, "Plan"))
	}
	if Section(m, "Nonexistent") != "MISSING" {
		t.Error("absent section should be MISSING")
	}
}

func TestJournalEntries(t *testing.T) {
	got := JournalEntries(sampleMD)
	if len(got) != 2 || got[1] != "- [2026-06-18] front done" {
		t.Errorf("journal entries: %v", got)
	}
}

// docWithTrailingSection has a section AFTER Journal, so a naive end-of-file
// append would put the entry under the wrong heading.
const docWithTrailingSection = `# add-payment

## Journal
- [2026-06-18] started

## References
- https://example.com
`

func TestAppendJournalInsertsUnderJournalNotAtEOF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "context.md")
	if err := os.WriteFile(p, []byte(docWithTrailingSection), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendJournal(p, "2026-06-19", "did the thing"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	got := string(data)

	// The new entry must appear in the Journal section, BEFORE "## References".
	entry := "- [2026-06-19] did the thing"
	ji := strings.Index(got, entry)
	ri := strings.Index(got, "## References")
	if ji < 0 {
		t.Fatalf("entry not written:\n%s", got)
	}
	if ri >= 0 && ji > ri {
		t.Errorf("entry landed under the wrong section:\n%s", got)
	}
	// The trailing section must be preserved.
	if !strings.Contains(got, "https://example.com") {
		t.Errorf("trailing section lost:\n%s", got)
	}
}

func TestAppendJournalReturnsErrorOnUnwritablePath(t *testing.T) {
	// Reading a non-existent context.md must surface an error (simulated failure),
	// never silently succeed.
	if err := AppendJournal(filepath.Join(t.TempDir(), "missing", "context.md"), "2026-06-19", "x"); err == nil {
		t.Error("expected an error when context.md cannot be read")
	}
}
