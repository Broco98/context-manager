package resume

import (
	"os"
	"path/filepath"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

type ProjState struct {
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Base   string `json:"base"`
	Dirty  bool   `json:"dirty"`
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
	Status string `json:"status"`
	// Available is true only when LIVE git state was read for this worktree.
	// When false, the worktree is missing/unreadable and Dirty/Ahead/Behind are
	// left zero — they must NOT be presented as real live state. Error explains why.
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

type Bundle struct {
	Task          string          `json:"task"`
	Status        string          `json:"status"`
	Goal          task.Goal       `json:"goal"`
	Background    string          `json:"background"`
	Plan          string          `json:"plan"`
	Worklist      []task.WorkItem `json:"worklist"`
	Next          string          `json:"next"`
	RecentJournal []string        `json:"recent_journal"`
	Projects      []ProjState     `json:"projects"`
}

func Build(taskDir string, tk *task.Task, gitState func(task.Project) ProjState) (*Bundle, error) {
	md := ""
	// A missing/unreadable context.md is intentionally treated as empty: all
	// sections then report MISSING via Section, and the (*Bundle, error) signature
	// is preserved so callers handle it uniformly.
	if data, err := os.ReadFile(filepath.Join(taskDir, "context.md")); err == nil {
		md = string(data)
	}
	sections := ParseSections(md)
	journal := JournalEntries(md)
	if len(journal) > 5 {
		journal = journal[len(journal)-5:]
	}
	// Normalize empty slices for a stable JSON contract: RecentJournal and Worklist
	// must serialize as [] not null (Projects is already []ProjState{} below).
	if journal == nil {
		journal = []string{}
	}
	worklist := tk.Worklist
	if worklist == nil {
		worklist = []task.WorkItem{}
	}
	b := &Bundle{
		Task: tk.Name, Status: string(tk.Status), Goal: tk.Goal,
		Background: Section(sections, "Background"), Plan: Section(sections, "Plan"),
		Worklist: worklist, Next: tk.NextWork(), RecentJournal: journal,
		Projects: []ProjState{},
	}
	for _, p := range tk.Projects {
		b.Projects = append(b.Projects, gitState(p))
	}
	return b, nil
}
