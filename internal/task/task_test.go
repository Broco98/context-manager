package task

import (
	"path/filepath"
	"testing"
)

func sample() *Task {
	return &Task{
		Name: "add-payment", Status: "active", Created: "2026-06-18T09:00:00Z",
		Goal:     Goal{Objective: "add payment", DoneWhen: "e2e pass"},
		Projects: []Project{{Name: "front", Repo: "/p/front", Status: "in_progress"}},
		Worklist: []WorkItem{{ID: 1, Text: "ui", Status: "done"}, {ID: 2, Text: "api", Status: "doing"}},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, sample()); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "add-payment" || got.Goal.DoneWhen != "e2e pass" || len(got.Projects) != 1 {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if _, err := filepath.Abs(filepath.Join(dir, "task.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestProgressAndNext(t *testing.T) {
	tk := sample()
	done, total := tk.Progress()
	if done != 1 || total != 2 {
		t.Errorf("progress got %d/%d want 1/2", done, total)
	}
	if got := tk.NextWork(); got != "api" {
		t.Errorf("next got %q want api", got)
	}
}

func TestAddAndSetWork(t *testing.T) {
	tk := sample()
	id := tk.AddWork("docs")
	if id != 3 {
		t.Errorf("new id got %d want 3", id)
	}
	if !tk.SetWork(3, "done") || tk.Worklist[2].Status != "done" {
		t.Errorf("SetWork failed")
	}
	if tk.SetWork(99, "done") {
		t.Errorf("SetWork on missing id should return false")
	}
}

func TestProjectLookup(t *testing.T) {
	tk := sample()
	if p := tk.Project("front"); p == nil || p.Repo != "/p/front" {
		t.Errorf("Project(front) failed: %+v", p)
	}
	if tk.Project("missing") != nil {
		t.Errorf("Project(missing) should be nil")
	}
}
