package resume

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

func TestBuildAssemblesGoalSectionsAndState(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "context.md"), []byte(sampleMD), 0o644)
	tk := &task.Task{
		Name: "add-payment", Status: "active",
		Goal:     task.Goal{Objective: "add pay", DoneWhen: "e2e"},
		Projects: []task.Project{{Name: "front", Base: "develop", Status: "in_progress"}},
		Worklist: []task.WorkItem{{ID: 1, Text: "api", Status: "doing"}},
	}
	stub := func(p task.Project) ProjState {
		return ProjState{Name: p.Name, Branch: "feat/x", Base: p.Base, Dirty: true, Ahead: 2, Status: string(p.Status)}
	}
	b, err := Build(dir, tk, stub)
	if err != nil {
		t.Fatal(err)
	}
	if b.Goal.DoneWhen != "e2e" || b.Background != "users want pay" || b.Plan != "MISSING" {
		t.Errorf("bad bundle: %+v", b)
	}
	if b.Next != "api" || len(b.Projects) != 1 || !b.Projects[0].Dirty {
		t.Errorf("bad next/projects: %+v", b)
	}
	if len(b.RecentJournal) != 2 {
		t.Errorf("journal: %v", b.RecentJournal)
	}
}
