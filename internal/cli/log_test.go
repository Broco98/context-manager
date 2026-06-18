package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/resume"
	"github.com/kimhyoyeon/context-manager/internal/task"
)

func TestRunLogAppendsJournal(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	if err := runLog(home, "tk", "did the thing"); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(task.ContextPath(filepath.Join(home, "tk")))
	entries := resume.JournalEntries(string(md))
	if len(entries) != 1 || !contains(entries[0], "did the thing") {
		t.Errorf("journal not appended: %v", entries)
	}
}
