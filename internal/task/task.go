package task

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kimhyoyeon/context-manager/internal/store"
	"gopkg.in/yaml.v3"
)

type Status string
type ProjStatus string

type Goal struct {
	Objective string `yaml:"objective" json:"objective"`
	DoneWhen  string `yaml:"done_when" json:"done_when"`
}
type Project struct {
	Name     string     `yaml:"name" json:"name"`
	Repo     string     `yaml:"repo" json:"repo"`
	Base     string     `yaml:"base" json:"base"`
	Branch   string     `yaml:"branch" json:"branch"`
	Worktree string     `yaml:"worktree" json:"worktree"`
	Spec     string     `yaml:"spec" json:"spec"`
	Status   ProjStatus `yaml:"status" json:"status"`
}
type WorkItem struct {
	ID     int    `yaml:"id" json:"id"`
	Text   string `yaml:"text" json:"text"`
	Status string `yaml:"status" json:"status"`
}
type Task struct {
	Name        string     `yaml:"name" json:"name"`
	Status      Status     `yaml:"status" json:"status"`
	Created     string     `yaml:"created" json:"created"`
	Description string     `yaml:"description,omitempty" json:"description,omitempty"`
	Goal        Goal       `yaml:"goal" json:"goal"`
	Projects    []Project  `yaml:"projects" json:"projects"`
	Worklist    []WorkItem `yaml:"worklist" json:"worklist"`
}

func yamlPath(taskDir string) string { return filepath.Join(taskDir, "task.yaml") }

func Load(taskDir string) (*Task, error) {
	data, err := os.ReadFile(yamlPath(taskDir))
	if err != nil {
		return nil, err
	}
	var t Task
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	// Validate stored relative paths BEFORE any caller reads, writes, runs a Git
	// operation on, or deletes them. A tampered task.yaml with Worktree/Spec like
	// "../other" would otherwise let `ctx done` remove a directory outside the task.
	for i := range t.Projects {
		if t.Projects[i].Worktree != "" {
			if _, err := store.ValidRelPath(taskDir, t.Projects[i].Worktree); err != nil {
				return nil, fmt.Errorf("invalid worktree path for project %q: %w", t.Projects[i].Name, err)
			}
		}
		if t.Projects[i].Spec != "" {
			if _, err := store.ValidRelPath(taskDir, t.Projects[i].Spec); err != nil {
				return nil, fmt.Errorf("invalid spec path for project %q: %w", t.Projects[i].Name, err)
			}
		}
	}
	return &t, nil
}

func Save(taskDir string, t *Task) error {
	data, err := yaml.Marshal(t)
	if err != nil {
		return err
	}
	return store.AtomicWrite(yamlPath(taskDir), data)
}

func (t *Task) Project(name string) *Project {
	for i := range t.Projects {
		if t.Projects[i].Name == name {
			return &t.Projects[i]
		}
	}
	return nil
}

func (t *Task) Progress() (done, total int) {
	for _, w := range t.Worklist {
		if w.Status == "done" {
			done++
		}
	}
	return done, len(t.Worklist)
}

func (t *Task) AddWork(text string) int {
	max := 0
	for _, w := range t.Worklist {
		if w.ID > max {
			max = w.ID
		}
	}
	id := max + 1
	t.Worklist = append(t.Worklist, WorkItem{ID: id, Text: text, Status: "todo"})
	return id
}

func (t *Task) SetWork(id int, status string) bool {
	for i := range t.Worklist {
		if t.Worklist[i].ID == id {
			t.Worklist[i].Status = status
			return true
		}
	}
	return false
}

func (t *Task) NextWork() string {
	for _, w := range t.Worklist {
		if w.Status == "doing" {
			return w.Text
		}
	}
	for _, w := range t.Worklist {
		if w.Status == "todo" {
			return w.Text
		}
	}
	return ""
}
