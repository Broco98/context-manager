# Context Manager CLI (`ctx`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `ctx`, a Go CLI that manages cross-repo task context — worktrees, specs, a north-star goal, session rehydration, and per-project persistent knowledge — all under a single `~/.ctx/` store.

**Architecture:** A thin cobra command layer wires together pure `internal/` packages: `store` (path resolution + walk-up), `task` (task.yaml model), `worktree` (git wrappers), `resume` (context.md parsing + rehydration), `goal` (/goal handoff), `knowledge` (per-project topic pages), and `output` (json/text + error envelope). The filesystem is the source of truth; git owns worktree truth. Inner packages are unit-tested as pure functions; commands are thin wiring.

**Tech Stack:** Go 1.22+, `github.com/spf13/cobra` (CLI), `gopkg.in/yaml.v3` (task.yaml). Standard library for everything else (`os`, `os/exec`, `path/filepath`, `encoding/json`, `bufio`, `strings`). Real `git` invoked via `os/exec`.

## Global Constraints

- **Go module path:** `github.com/kimhyoyeon/context-manager`; binary name `ctx` from `cmd/ctx`.
- **Only two external deps:** `cobra` and `yaml.v3`. No other modules. Pure-Go, single static binary (`CGO_ENABLED=0`).
- **Folder depth:** the ctx-managed structure stays within **3 depth** (`task/project/{spec.md,wt}`); `wt/` internals are git's domain.
- **`CTX_HOME`:** store root resolves to `$CTX_HOME` if set, else `~/.ctx`. Every command resolves this once via `store.Home()`.
- **`--json` is a stable contract:** read commands (`current`, `status`, `resume`, `goal`, `know search`, `task ls`) support `--json`; output schemas are locked with golden tests. Errors in `--json` mode print `{"error": "<msg>", "code": "<CODE>"}` to stdout and exit non-zero.
- **Atomic writes:** every write to `task.yaml`, `context.md`, `index.md`, knowledge pages goes through `store.AtomicWrite` (temp file + `os.Rename`).
- **Destructive ops refuse by default:** `ctx done` refuses unmerged/dirty worktrees unless `--force`.
- **Timestamps:** ISO-8601 UTC (`time.Now().UTC().Format(time.RFC3339)`); date-only fields use `2006-01-02`.
- **Controlled vocab:** project status `planned|in_progress|review|done`; worklist status `todo|doing|done`; task status `active|done`; knowledge category `architecture|decision|gotcha|pattern`; page status `active|superseded`.

---

## Shared Data Model (defined in Task 3, referenced everywhere)

These types live in `internal/task/task.go`. Later tasks consume them by the exact names below.

```go
type Status string         // "active" | "done"
type ProjStatus string     // "planned" | "in_progress" | "review" | "done"

type Goal struct {
    Objective string `yaml:"objective" json:"objective"`
    DoneWhen  string `yaml:"done_when" json:"done_when"`
}
type Project struct {
    Name     string     `yaml:"name" json:"name"`
    Repo     string     `yaml:"repo" json:"repo"`
    Base     string     `yaml:"base" json:"base"`
    Branch   string     `yaml:"branch" json:"branch"`
    Worktree string     `yaml:"worktree" json:"worktree"` // relative to task dir, e.g. "front/wt"
    Spec     string     `yaml:"spec" json:"spec"`         // relative, e.g. "front/spec.md"
    Status   ProjStatus `yaml:"status" json:"status"`
}
type WorkItem struct {
    ID     int    `yaml:"id" json:"id"`
    Text   string `yaml:"text" json:"text"`
    Status string `yaml:"status" json:"status"` // "todo" | "doing" | "done"
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
```

## File Structure

| Path | Responsibility |
|------|----------------|
| `cmd/ctx/main.go` | Entry point; calls `cli.Execute()`. |
| `internal/cli/root.go` | cobra root command, `--json` persistent flag, `Execute()`. |
| `internal/cli/*.go` | One file per command group (`new.go`, `add.go`, `status.go`, `current.go`, `task.go`, `log.go`, `resume.go`, `goal.go`, `done.go`, `know.go`, `where.go`). Thin wiring only. |
| `internal/store/store.go` | `Home()`, path helpers, `AtomicWrite`, `EnsureDir`, `ListTasks`, `Locate` (walk-up). |
| `internal/task/task.go` | Data model (above) + `Load`/`Save` + helpers. |
| `internal/task/context.go` | `context.md` template scaffold. |
| `internal/worktree/worktree.go` | `Add`/`Remove`/`Status`/`DefaultBranch` git wrappers. |
| `internal/resume/resume.go` | `ParseSections` (context.md) + `Build` (rehydration bundle). |
| `internal/goal/goal.go` | `Handoff` (/goal text builder). |
| `internal/knowledge/knowledge.go` | topic-page merge, `_shared/` promotion, `index.md`/`log.md`. |
| `internal/knowledge/search.go` | `tokenize` (CJK bigrams) + `Search`. |
| `internal/output/output.go` | `Emit`, `Errorf`, error codes. |

---

## Task 1: Project scaffold + cobra root + output package

**Files:**
- Create: `go.mod`, `cmd/ctx/main.go`, `internal/cli/root.go`, `internal/output/output.go`
- Test: `internal/output/output_test.go`

**Interfaces:**
- Produces: `output.Emit(jsonMode bool, human string, data any) error`; `output.Errorf(jsonMode bool, code, format string, a ...any) error` (prints envelope, returns an `error` so commands can `return`); error codes `output.ErrNotFound="NOT_FOUND"`, `output.ErrConflict="CONFLICT"`, `output.ErrUsage="USAGE"`, `output.ErrGit="GIT"`, `output.ErrState="STATE"`. `cli.Execute() error`; `cli.RootCmd` exposes persistent `--json` bool.

- [ ] **Step 1: Initialize the module and dependencies**

```bash
cd /Users/kimhyoyeon/MyProjects/context-manager
go mod init github.com/kimhyoyeon/context-manager
go get github.com/spf13/cobra@latest
go get gopkg.in/yaml.v3@latest
```
Expected: `go.mod` and `go.sum` created listing cobra and yaml.v3.

- [ ] **Step 2: Write the failing test for `output.Emit`**

Create `internal/output/output_test.go`:

```go
package output

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestEmitJSONWritesData(t *testing.T) {
	var buf bytes.Buffer
	out = &buf // package-level writer override for tests
	if err := Emit(true, "ignored human text", map[string]any{"task": "add-payment"}); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got["task"] != "add-payment" {
		t.Errorf("got task=%v, want add-payment", got["task"])
	}
}

func TestEmitTextWritesHuman(t *testing.T) {
	var buf bytes.Buffer
	out = &buf
	if err := Emit(false, "hello human", map[string]any{"task": "x"}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello human\n" {
		t.Errorf("got %q, want %q", buf.String(), "hello human\n")
	}
}

func TestErrorfJSONEnvelope(t *testing.T) {
	var buf bytes.Buffer
	out = &buf
	err := Errorf(true, ErrNotFound, "task %q not found", "x")
	if err == nil {
		t.Fatal("Errorf should return a non-nil error")
	}
	var got map[string]string
	if jerr := json.Unmarshal(buf.Bytes(), &got); jerr != nil {
		t.Fatalf("not JSON: %v\n%s", jerr, buf.String())
	}
	if got["code"] != "NOT_FOUND" || got["error"] == "" {
		t.Errorf("bad envelope: %v", got)
	}
}

func TestErrorfMarksRenderedAndRenderSkipsDouble(t *testing.T) {
	var buf bytes.Buffer
	out = &buf
	err := Errorf(true, ErrUsage, "bad input")
	if !IsRendered(err) {
		t.Fatal("Errorf result should be marked rendered")
	}
	buf.Reset()
	if got := Render(true, err); got == nil {
		t.Fatal("Render should pass the error through")
	}
	if buf.Len() != 0 {
		t.Errorf("Render should not re-emit an already-rendered error, got %q", buf.String())
	}
}

func TestRenderEmitsEnvelopeForRawError(t *testing.T) {
	var buf bytes.Buffer
	out = &buf
	_ = Render(true, os.ErrNotExist)
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if got["code"] != "NOT_FOUND" || got["error"] == "" {
		t.Errorf("bad raw envelope: %v", got)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/output/`
Expected: FAIL — `undefined: out`, `Emit`, `Errorf`, `ErrNotFound`, `IsRendered`, `Render`.

- [ ] **Step 4: Implement `internal/output/output.go`**

This file is written **complete** here (including the `rendered` marker,
`IsRendered`, `Render`, and `classify`) so the package compiles at Step 5: the
`"errors"` and `"os"` imports are used by `IsRendered`/`classify`/`Errorf` from
the start. Step 6 only adds the cobra root and entry point — it does **not**
re-edit this file.

```go
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// out is the destination writer; overridable in tests.
var out io.Writer = os.Stdout

const (
	ErrNotFound = "NOT_FOUND"
	ErrConflict = "CONFLICT"
	ErrUsage    = "USAGE"
	ErrGit      = "GIT"
	ErrState    = "STATE"
)

// Emit prints data as JSON when jsonMode, else prints the human string.
func Emit(jsonMode bool, human string, data any) error {
	if jsonMode {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}
	_, err := fmt.Fprintln(out, human)
	return err
}

// Errorf prints an error (JSON envelope when jsonMode, else "Error: ..." to stderr)
// and returns a rendered-marked error so callers can `return output.Errorf(...)`
// and the top-level Execute wrapper will not print a second envelope.
func Errorf(jsonMode bool, code, format string, a ...any) error {
	msg := fmt.Sprintf(format, a...)
	if jsonMode {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]string{"error": msg, "code": code})
	} else {
		fmt.Fprintf(os.Stderr, "Error [%s]: %s\n", code, msg)
	}
	return rendered{err: fmt.Errorf("%s: %s", code, msg)}
}

// rendered marks errors already printed by Errorf so the top-level
// Execute wrapper does not print a second envelope for them.
type rendered struct{ err error }

func (r rendered) Error() string { return r.err.Error() }
func (r rendered) Unwrap() error { return r.err }

// IsRendered reports whether err was already emitted by Errorf/Render.
func IsRendered(err error) bool {
	var r rendered
	return errors.As(err, &r)
}

// Render emits a JSON/text envelope for an error that was NOT produced by
// Errorf (raw filesystem/parsing/Cobra errors), mapping it to a fallback code,
// and returns a rendered-marked error so callers print exactly once.
func Render(jsonMode bool, err error) error {
	if err == nil {
		return nil
	}
	if IsRendered(err) {
		return err // already emitted by Errorf; do not double-print
	}
	code := classify(err)
	msg := err.Error()
	if jsonMode {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]string{"error": msg, "code": code})
	} else {
		fmt.Fprintf(os.Stderr, "Error [%s]: %s\n", code, msg)
	}
	return rendered{err: fmt.Errorf("%s: %s", code, msg)}
}

// classify maps a raw error to a coarse error code for the envelope.
func classify(err error) string {
	switch {
	case os.IsNotExist(err):
		return ErrNotFound
	case os.IsExist(err):
		return ErrConflict
	default:
		return ErrState
	}
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/output/`
Expected: PASS (5 tests).

- [ ] **Step 6: Implement the cobra root and entry point**

The `output.Errorf` helper already renders a typed envelope and returns a
sentinel-wrapped error. To honor the JSON error contract for *every* failure
(filesystem, YAML parsing, raw Cobra argument errors), `Execute` centralizes
error rendering: it renders any error that has **not** already been rendered by
`Errorf`, mapping it to a code and emitting exactly one envelope. The
rendered-marker (`rendered`/`IsRendered`) plus the catch-all `Render`/`classify`
already live in `internal/output/output.go` (written complete in Step 4), so this
step adds **only** the cobra root and entry point — no further edits to
`output.go` or `output_test.go`.

Create `internal/cli/root.go`:

```go
package cli

import (
	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/spf13/cobra"
)

var jsonOut bool

var RootCmd = &cobra.Command{
	Use:           "ctx",
	Short:         "ctx — cross-repo task context manager",
	Long:          "ctx manages cross-repo task context: worktrees, specs, a north-star goal, session rehydration, and per-project persistent knowledge under ~/.ctx.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	RootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
}

// Execute runs the root command and guarantees exactly one error envelope:
// errors already rendered by output.Errorf pass through untouched; every other
// error (filesystem, YAML parsing, Cobra argument errors) is rendered here so no
// failure bypasses the {error,code} contract.
func Execute() error {
	if err := RootCmd.Execute(); err != nil {
		return output.Render(jsonOut, err)
	}
	return nil
}
```

Create `cmd/ctx/main.go`:

```go
package main

import (
	"os"

	"github.com/kimhyoyeon/context-manager/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 7: Verify the binary builds and help works**

Run: `go build -o /tmp/ctx ./cmd/ctx && /tmp/ctx --help`
Expected: builds; help shows `ctx — cross-repo task context manager` and the `--json` flag.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum cmd/ internal/
git commit -m "feat: scaffold ctx CLI with cobra root and output package"
```

---

## Task 2: `store` — CTX_HOME resolution, atomic write, layout, ListTasks

**Files:**
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces:
  - `store.Home() (string, error)` — `$CTX_HOME` or `~/.ctx` (does not create it).
  - `store.TaskDir(home, name string) string`, `store.KnowledgeDir(home string) string`, `store.SharedDir(home string) string`.
  - `store.EnsureDir(path string) error`.
  - `store.AtomicWrite(path string, data []byte) error` — creates parent dirs, writes temp + renames.
  - `store.ListTasks(home string) ([]string, error)` — task dir names (those containing `task.yaml`), sorted, excluding names starting with `_`.

- [ ] **Step 1: Write the failing test**

Create `internal/store/store_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomeUsesEnvOverride(t *testing.T) {
	t.Setenv("CTX_HOME", "/tmp/custom-ctx")
	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/custom-ctx" {
		t.Errorf("got %q, want /tmp/custom-ctx", got)
	}
}

func TestHomeDefaultsToHomeDotCtx(t *testing.T) {
	t.Setenv("CTX_HOME", "")
	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if got != filepath.Join(home, ".ctx") {
		t.Errorf("got %q, want %q", got, filepath.Join(home, ".ctx"))
	}
}

func TestAtomicWriteCreatesParentsAndContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a", "b", "task.yaml")
	if err := AtomicWrite(p, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "hello" {
		t.Errorf("got %q", string(got))
	}
}

func TestListTasksExcludesUnderscoreAndSortsByName(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "_knowledge"} {
		d := filepath.Join(home, name)
		_ = os.MkdirAll(d, 0o755)
		if name != "_knowledge" {
			_ = os.WriteFile(filepath.Join(d, "task.yaml"), []byte("name: "+name), 0o644)
		}
	}
	got, err := ListTasks(home)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "zeta"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/`
Expected: FAIL — undefined `Home`, `AtomicWrite`, `ListTasks`.

- [ ] **Step 3: Implement `internal/store/store.go`**

```go
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ValidName validates that name is a safe single path component usable as a
// task or project directory name directly under CTX_HOME (or a task dir). It
// rejects empty names, "." / "..", names containing a path separator or NUL,
// and reserved underscore-prefixed names (which collide with the reserved
// `_knowledge`/`_shared` directories and ListTasks' "_" exclusion). This stops
// values like "../other" or "_knowledge" from escaping or shadowing managed dirs.
func ValidName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name %q is not allowed", name)
	}
	if strings.HasPrefix(name, "_") {
		return fmt.Errorf("name %q is reserved (underscore-prefixed names are not allowed)", name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("name %q contains an invalid NUL byte", name)
	}
	if strings.ContainsAny(name, "/\\") || name != filepath.Base(name) {
		return fmt.Errorf("name %q must be a single path component (no slashes)", name)
	}
	return nil
}

// resolveExisting returns the canonical (symlink-resolved) form of path. Because
// path may not exist yet (e.g. a spec.md or wt about to be created), it resolves
// the LONGEST existing ancestor with filepath.EvalSymlinks and re-appends the
// not-yet-existing trailing components, so a symlinked ancestor is followed to its
// real location while a path whose leaf does not exist still canonicalizes.
func resolveExisting(path string) (string, error) {
	clean := filepath.Clean(path)
	rest := ""
	for {
		if resolved, err := filepath.EvalSymlinks(clean); err == nil {
			return filepath.Join(resolved, rest), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(clean)
		if parent == clean {
			// Reached the filesystem root without an existing ancestor; nothing to
			// resolve, so the cleaned input is already its own canonical form.
			return filepath.Join(clean, rest), nil
		}
		rest = filepath.Join(filepath.Base(clean), rest)
		clean = parent
	}
}

// ValidRelPath validates that rel is a relative path contained beneath base and
// returns its CANONICAL absolute form. It rejects absolute paths and any path that
// escapes base via "..", so a stored Spec/Worktree value can never point outside
// its task directory before a read, write, Git operation, or deletion. Both base
// and the joined target are canonicalized via resolveExisting BEFORE the
// containment check, so a symlinked managed component (a task, project, wt, or
// _knowledge dir that redirects elsewhere) is followed to its real location and
// rejected when that location is no longer beneath the canonical base — lexical
// "../" checks alone would miss a symlink that points outside the store.
func ValidRelPath(base, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q must be relative", rel)
	}
	canonBase, err := resolveExisting(base)
	if err != nil {
		return "", err
	}
	abs, err := resolveExisting(filepath.Join(canonBase, rel))
	if err != nil {
		return "", err
	}
	if abs != canonBase && !strings.HasPrefix(abs, canonBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes its task directory", rel)
	}
	return abs, nil
}

func Home() (string, error) {
	if v := os.Getenv("CTX_HOME"); v != "" {
		return v, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".ctx"), nil
}

func TaskDir(home, name string) string  { return filepath.Join(home, name) }
func KnowledgeDir(home string) string   { return filepath.Join(home, "_knowledge") }
func SharedDir(home string) string      { return filepath.Join(KnowledgeDir(home), "_shared") }

func EnsureDir(path string) error { return os.MkdirAll(path, 0o755) }

// AtomicWrite writes data to path via a temp file + rename, creating parents.
func AtomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// ListTasks returns names of directories under home that contain a task.yaml,
// excluding names beginning with "_", sorted ascending.
func ListTasks(home string) ([]string, error) {
	entries, err := os.ReadDir(home)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || (len(e.Name()) > 0 && e.Name()[0] == '_') {
			continue
		}
		if _, err := os.Stat(filepath.Join(home, e.Name(), "task.yaml")); err == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/store/`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat: add store package (CTX_HOME, atomic write, task listing)"
```

---

## Task 3: `task` — data model, Load/Save, helpers

**Files:**
- Create: `internal/task/task.go`
- Test: `internal/task/task_test.go`

**Interfaces:**
- Produces (types as in **Shared Data Model** above) plus:
  - `task.Load(taskDir string) (*Task, error)` — reads `<taskDir>/task.yaml`.
  - `task.Save(taskDir string, t *Task) error` — atomic YAML write.
  - `(*Task).Project(name string) *Project` — pointer into slice, or nil.
  - `(*Task).Progress() (done, total int)` — over worklist.
  - `(*Task).AddWork(text string) int` — appends, returns new id (max+1).
  - `(*Task).SetWork(id int, status string) bool` — returns false if id missing.
  - `(*Task).NextWork() string` — text of first `doing`, else first `todo`, else "".

- [ ] **Step 1: Write the failing test**

Create `internal/task/task_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/task/`
Expected: FAIL — undefined types/functions.

- [ ] **Step 3: Implement `internal/task/task.go`**

```go
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/task/`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/task/task.go internal/task/task_test.go
git commit -m "feat: add task.yaml data model with load/save and worklist helpers"
```

---

## Task 4: `context.md` template + `ctx new`

**Files:**
- Create: `internal/task/context.go`, `internal/cli/new.go`
- Test: `internal/task/context_test.go`, `internal/cli/new_test.go`

**Interfaces:**
- Produces:
  - `task.ContextTemplate(name, background string) string` — markdown with `# <name>` and `## Background/## Plan/## Decisions/## Journal`; Background prefilled if non-empty.
  - `task.ContextPath(taskDir) string`.
  - `cli.runNew(home string, o newOpts) (*task.Task, error)` where `newOpts{Name, Objective, DoneWhen, Description, Background string}`. Errors `CONFLICT` if the task dir exists, `USAGE` if Name/Objective/DoneWhen empty. (Pure: no prompting — the command layer fills missing goal fields before calling it.)
  - The `new` command **prompts interactively** for any missing `--objective`/`--done-when` when stdin is a terminal; when stdin is non-interactive it returns a `USAGE` error instead.

- [ ] **Step 1: Write the failing test for the template**

Create `internal/task/context_test.go`:

```go
package task

import "testing"

func TestContextTemplateHasRequiredHeadings(t *testing.T) {
	out := ContextTemplate("add-payment", "needs payment")
	for _, h := range []string{"# add-payment", "## Background", "## Plan", "## Decisions", "## Journal"} {
		if !contains(out, h) {
			t.Errorf("template missing %q\n%s", h, out)
		}
	}
	if !contains(out, "needs payment") {
		t.Errorf("background not prefilled:\n%s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/task/ -run TestContextTemplate`
Expected: FAIL — undefined `ContextTemplate`.

- [ ] **Step 3: Implement `internal/task/context.go`**

```go
package task

import "path/filepath"

func ContextPath(taskDir string) string { return filepath.Join(taskDir, "context.md") }

func ContextTemplate(name, background string) string {
	return "# " + name + "\n\n" +
		"## Background\n" + background + "\n\n" +
		"## Plan\n\n" +
		"## Decisions\n\n" +
		"## Journal\n"
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/task/ -run TestContextTemplate`
Expected: PASS.

- [ ] **Step 5: Write the failing test for `runNew`**

Create `internal/cli/new_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

func TestRunNewCreatesTaskAndContext(t *testing.T) {
	home := t.TempDir()
	got, err := runNew(home, newOpts{
		Name: "add-payment", Objective: "add payment", DoneWhen: "e2e pass",
		Description: "cross-repo", Background: "users want pay",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "active" || got.Goal.Objective != "add payment" || got.Created == "" {
		t.Errorf("bad task: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(home, "add-payment", "task.yaml")); err != nil {
		t.Error("task.yaml not written")
	}
	ctx, _ := os.ReadFile(task.ContextPath(filepath.Join(home, "add-payment")))
	if !contains(string(ctx), "users want pay") {
		t.Error("context.md missing background")
	}
}

func TestRunNewRejectsDuplicate(t *testing.T) {
	home := t.TempDir()
	opts := newOpts{Name: "dup", Objective: "o", DoneWhen: "d"}
	if _, err := runNew(home, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := runNew(home, opts); err == nil {
		t.Error("expected CONFLICT on duplicate task")
	}
}

func TestRunNewRequiresGoal(t *testing.T) {
	home := t.TempDir()
	if _, err := runNew(home, newOpts{Name: "x"}); err == nil {
		t.Error("expected USAGE error when objective/done_when empty")
	}
}

// TestPromptMissingGoalInteractiveReadsStdin drives the interactive path: the
// objective and done-when are read line-by-line from stdin.
func TestPromptMissingGoalInteractiveReadsStdin(t *testing.T) {
	in := strings.NewReader("add payment\ne2e pass\n")
	var prompt bytes.Buffer
	got, err := promptMissingGoal(newOpts{Name: "x"}, in, &prompt, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Objective != "add payment" || got.DoneWhen != "e2e pass" {
		t.Errorf("bad prompted opts: %+v", got)
	}
	if !strings.Contains(prompt.String(), "--objective") {
		t.Errorf("expected a prompt for --objective, got %q", prompt.String())
	}
}

// TestPromptMissingGoalNonInteractiveErrors confirms a clear USAGE error when a
// field is missing and stdin is non-interactive.
func TestPromptMissingGoalNonInteractiveErrors(t *testing.T) {
	if _, err := promptMissingGoal(newOpts{Name: "x"}, strings.NewReader(""), &bytes.Buffer{}, false); err == nil {
		t.Error("expected USAGE error when non-interactive and objective is missing")
	}
}

// TestPromptMissingGoalKeepsProvidedFlags leaves already-set fields untouched.
func TestPromptMissingGoalKeepsProvidedFlags(t *testing.T) {
	got, err := promptMissingGoal(
		newOpts{Name: "x", Objective: "o", DoneWhen: "d"},
		strings.NewReader(""), &bytes.Buffer{}, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Objective != "o" || got.DoneWhen != "d" {
		t.Errorf("provided flags should be preserved: %+v", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestRunNew`
Expected: FAIL — undefined `runNew`, `newOpts`.

- [ ] **Step 7: Implement `internal/cli/new.go`**

```go
package cli

import (
	"bufio"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

type newOpts struct {
	Name, Objective, DoneWhen, Description, Background string
}

func runNew(home string, o newOpts) (*task.Task, error) {
	if o.Name == "" || o.Objective == "" || o.DoneWhen == "" {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "name, --objective and --done-when are required")
	}
	// Reject names that are not a safe single path component (e.g. "../other") or
	// reserved underscore names (e.g. "_knowledge"), so the task dir cannot escape
	// CTX_HOME or collide with the reserved knowledge directory.
	if err := store.ValidName(o.Name); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "invalid task name: %v", err)
	}
	dir := store.TaskDir(home, o.Name)
	if dirExists(dir) {
		return nil, output.Errorf(jsonOut, output.ErrConflict, "task %q already exists", o.Name)
	}
	t := &task.Task{
		Name: o.Name, Status: "active",
		Created:     time.Now().UTC().Format(time.RFC3339),
		Description: o.Description,
		Goal:        task.Goal{Objective: o.Objective, DoneWhen: o.DoneWhen},
	}
	if err := task.Save(dir, t); err != nil {
		return nil, err
	}
	if err := store.AtomicWrite(task.ContextPath(dir), []byte(task.ContextTemplate(o.Name, o.Background))); err != nil {
		return nil, err
	}
	return t, nil
}

// promptMissingGoal fills empty Objective/DoneWhen by prompting on `in` when the
// session is interactive. When non-interactive and a goal field is still empty,
// it returns a USAGE error (spec §7: prompt interactively, else clear usage error).
func promptMissingGoal(o newOpts, in io.Reader, prompt io.Writer, interactive bool) (newOpts, error) {
	r := bufio.NewReader(in)
	ask := func(label string) (string, error) {
		if !interactive {
			return "", output.Errorf(jsonOut, output.ErrUsage, "%s is required (pass the flag or run interactively)", label)
		}
		_, _ = io.WriteString(prompt, label+": ")
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return "", output.Errorf(jsonOut, output.ErrUsage, "%s is required", label)
		}
		return strings.TrimSpace(line), nil
	}
	if o.Objective == "" {
		v, err := ask("--objective")
		if err != nil {
			return o, err
		}
		o.Objective = v
	}
	if o.DoneWhen == "" {
		v, err := ask("--done-when")
		if err != nil {
			return o, err
		}
		o.DoneWhen = v
	}
	if o.Objective == "" || o.DoneWhen == "" {
		return o, output.Errorf(jsonOut, output.ErrUsage, "objective and done-when are required")
	}
	return o, nil
}

// isInteractive reports whether stdin is a terminal (not a pipe/redirect).
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func init() {
	o := newOpts{}
	cmd := &cobra.Command{
		Use:   "new <task>",
		Short: "Create a task (task.yaml with goal + context.md)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			o.Name = args[0]
			filled, err := promptMissingGoal(o, os.Stdin, os.Stderr, isInteractive())
			if err != nil {
				return err
			}
			t, err := runNew(home, filled)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, "Created task "+t.Name, t)
		},
	}
	cmd.Flags().StringVar(&o.Objective, "objective", "", "goal objective (what/why); prompted if omitted")
	cmd.Flags().StringVar(&o.DoneWhen, "done-when", "", "verifiable completion condition; prompted if omitted")
	cmd.Flags().StringVarP(&o.Description, "description", "d", "", "short description")
	cmd.Flags().StringVar(&o.Background, "background", "", "context.md Background prefill")
	RootCmd.AddCommand(cmd)
}
```

Also add the `dirExists` helper in a shared file `internal/cli/util.go`:

```go
package cli

import "os"

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./internal/cli/ ./internal/task/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/task/context.go internal/task/context_test.go internal/cli/new.go internal/cli/new_test.go internal/cli/util.go
git commit -m "feat: ctx new — scaffold task.yaml goal + context.md template"
```

---

## Task 5: `store.Locate` (walk-up) + `ctx current`

**Files:**
- Create: `internal/cli/current.go`
- Modify: `internal/store/store.go` (add `Location` + `Locate`)
- Test: `internal/store/locate_test.go`, `internal/cli/current_test.go`

**Interfaces:**
- Produces:
  - `store.Location{Home, Task, Project, TaskDir string}` and `store.Locate(home, cwd string) (*Location, error)`. `Task == ""` when cwd is not inside any task.
  - `cli.runCurrent(home, cwd string) (currentOut, error)` where `currentOut{Task *string; Project *string; Spec, Worktree, Status string}` marshals to `{"task":null}` when outside a task.

- [ ] **Step 1: Write the failing test for `Locate`**

Create `internal/store/locate_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateFindsTaskAndProjectFromDeepCwd(t *testing.T) {
	home := t.TempDir()
	taskDir := filepath.Join(home, "add-payment")
	deep := filepath.Join(taskDir, "front", "wt", "src")
	_ = os.MkdirAll(deep, 0o755)
	_ = os.WriteFile(filepath.Join(taskDir, "task.yaml"), []byte("name: add-payment"), 0o644)

	loc, err := Locate(home, deep)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Task != "add-payment" || loc.Project != "front" {
		t.Errorf("got task=%q project=%q", loc.Task, loc.Project)
	}
}

func TestLocateOutsideTaskReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	loc, err := Locate(home, home)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Task != "" {
		t.Errorf("expected empty task, got %q", loc.Task)
	}
}

// TestLocateIgnoresTaskYamlOutsideHome proves a task.yaml that lives OUTSIDE
// CTX_HOME is never reported as a ctx task: Locate must not walk past the home
// boundary, so {task:""} is returned even though an ancestor has a task.yaml.
func TestLocateIgnoresTaskYamlOutsideHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "ctx-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stray task.yaml in an unrelated tree OUTSIDE home.
	external := filepath.Join(root, "some-repo")
	deep := filepath.Join(external, "pkg", "sub")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "task.yaml"), []byte("name: not-a-ctx-task"), 0o644); err != nil {
		t.Fatal(err)
	}
	loc, err := Locate(home, deep)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Task != "" {
		t.Errorf("external task.yaml outside home must not be detected, got task=%q", loc.Task)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/store/ -run TestLocate`
Expected: FAIL — undefined `Locate`.

- [ ] **Step 3: Implement `Locate` (append to `internal/store/store.go`)**

```go
// Note: "fmt" and "strings" are already in the import block from Task 3 Step 3
// (added for ValidName/ValidRelPath), so the Locate code below needs no new imports.

type Location struct {
	Home    string
	Task    string
	Project string
	TaskDir string
}

// Locate walks up from cwd to the first ancestor containing task.yaml, but never
// past the CTX_HOME boundary. It returns {Home} (Task == "") when cwd is outside
// home or when no managed task is found within home, so a stray task.yaml outside
// CTX_HOME is never mistaken for a ctx task. The walk stops once it reaches the
// cleaned home dir: tasks live directly under home, so home itself is the last
// directory inspected.
func Locate(home, cwd string) (*Location, error) {
	cleanHome := filepath.Clean(home)
	dir := filepath.Clean(cwd)

	// cwd must be within home (home itself counts). Use the boundary-safe prefix
	// check so "/a/.ctx-other" is not treated as inside "/a/.ctx".
	if dir != cleanHome && !strings.HasPrefix(dir, cleanHome+string(filepath.Separator)) {
		return &Location{Home: home}, nil
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "task.yaml")); err == nil {
			loc := &Location{Home: home, TaskDir: dir, Task: filepath.Base(dir)}
			if rel, rerr := filepath.Rel(dir, filepath.Clean(cwd)); rerr == nil && rel != "." {
				parts := strings.Split(rel, string(filepath.Separator))
				if len(parts) > 0 && parts[0] != ".." && parts[0] != "" {
					loc.Project = parts[0]
				}
			}
			return loc, nil
		}
		// Stop at the home boundary: tasks live directly under home, so home is
		// the last directory we inspect. Never walk to the filesystem root.
		if dir == cleanHome {
			return &Location{Home: home}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return &Location{Home: home}, nil
		}
		dir = parent
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/store/ -run TestLocate`
Expected: PASS.

- [ ] **Step 5: Write the failing test for `runCurrent`**

Create `internal/cli/current_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCurrentInsideTask(t *testing.T) {
	home := t.TempDir()
	if _, err := runNew(home, newOpts{Name: "add-payment", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	// simulate a registered front project so status/spec resolve
	taskDir := filepath.Join(home, "add-payment")
	_ = os.MkdirAll(filepath.Join(taskDir, "front", "wt"), 0o755)
	writeProjectForTest(t, taskDir, "front")

	out, err := runCurrent(home, filepath.Join(taskDir, "front", "wt"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Task == nil || *out.Task != "add-payment" || out.Project == nil || *out.Project != "front" {
		t.Errorf("bad current: %+v", out)
	}
}

func TestRunCurrentOutsideTaskIsNull(t *testing.T) {
	home := t.TempDir()
	out, err := runCurrent(home, home)
	if err != nil {
		t.Fatal(err)
	}
	if out.Task != nil {
		t.Errorf("expected nil task, got %v", *out.Task)
	}
}
```

Add the shared test helper `internal/cli/helpers_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

// writeProjectForTest appends a minimal project to an existing task.yaml.
func writeProjectForTest(t *testing.T, taskDir, name string) {
	t.Helper()
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	tk.Projects = append(tk.Projects, task.Project{
		Name: name, Repo: "/repo/" + name, Base: "develop",
		Branch: "feat/x", Worktree: filepath.Join(name, "wt"),
		Spec: filepath.Join(name, "spec.md"), Status: "in_progress",
	})
	if err := task.Save(taskDir, tk); err != nil {
		t.Fatal(err)
	}
}

// writeRepolessProjectForTest appends a project with an EMPTY Repo so the
// git-truth lookup in runStatusDetail is skipped entirely (it only queries git
// when both Repo and Worktree are set). Used by the status_detail.json golden so
// the locked contract is deterministic: worktree_exists=false, live_branch="",
// and worktree_error is omitted — no machine-specific git error text leaks in.
func writeRepolessProjectForTest(t *testing.T, taskDir, name string) {
	t.Helper()
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	tk.Projects = append(tk.Projects, task.Project{
		Name: name, Repo: "", Base: "develop",
		Branch: "feat/x", Worktree: filepath.Join(name, "wt"),
		Spec: filepath.Join(name, "spec.md"), Status: "in_progress",
	})
	if err := task.Save(taskDir, tk); err != nil {
		t.Fatal(err)
	}
}

// writeSpecFileForTest writes a project's spec.md to its registered path
// (used by Task 15's ctx spec tests).
func writeSpecFileForTest(home, taskName, project, body string) error {
	taskDir := filepath.Join(home, taskName)
	tk, err := task.Load(taskDir)
	if err != nil {
		return err
	}
	p := tk.Project(project)
	if p == nil {
		return os.ErrNotExist
	}
	dst := filepath.Join(taskDir, p.Spec)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(body), 0o644)
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestRunCurrent`
Expected: FAIL — undefined `runCurrent`.

- [ ] **Step 7: Implement `internal/cli/current.go`**

```go
package cli

import (
	"os"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

type currentOut struct {
	Task     *string `json:"task"`
	Project  *string `json:"project,omitempty"`
	Spec     string  `json:"spec,omitempty"`
	Worktree string  `json:"worktree,omitempty"`
	Status   string  `json:"status,omitempty"`
}

func runCurrent(home, cwd string) (currentOut, error) {
	loc, err := store.Locate(home, cwd)
	if err != nil {
		return currentOut{}, err
	}
	if loc.Task == "" {
		return currentOut{Task: nil}, nil
	}
	out := currentOut{Task: &loc.Task}
	if loc.Project != "" {
		p := loc.Project
		out.Project = &p
		if tk, lerr := task.Load(loc.TaskDir); lerr == nil {
			if pr := tk.Project(loc.Project); pr != nil {
				out.Spec, out.Worktree, out.Status = pr.Spec, pr.Worktree, string(pr.Status)
			}
		}
	}
	return out, nil
}

func init() {
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Show the task/project detected from the current directory",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			out, err := runCurrent(home, cwd)
			if err != nil {
				return err
			}
			human := "Not inside a ctx task"
			if out.Task != nil {
				human = "task=" + *out.Task
				if out.Project != nil {
					human += " project=" + *out.Project
				}
			}
			return output.Emit(jsonOut, human, out)
		},
	}
	RootCmd.AddCommand(cmd)
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./internal/cli/ ./internal/store/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/store/store.go internal/store/locate_test.go internal/cli/current.go internal/cli/current_test.go internal/cli/helpers_test.go
git commit -m "feat: walk-up Locate + ctx current (CWD context detection)"
```

---

## Task 6: `worktree` package (git wrappers)

**Files:**
- Create: `internal/worktree/worktree.go`
- Test: `internal/worktree/worktree_test.go`

**Interfaces:**
- Produces:
  - `worktree.Add(repo, worktreePath, branch, base string) error` — `git -C repo worktree add <abs path> -b <branch> <base>`.
  - `worktree.Remove(repo, worktreePath string, force bool) error`.
  - `worktree.DefaultBranch(repo string) (string, error)` — the repository's *default* branch (from `origin/HEAD`, falling back to a local `main`/`master`/`develop`), **not** the currently checked-out branch.
  - `worktree.State{Branch string; Dirty bool; Ahead, Behind int}` and `worktree.Status(worktreePath, base string) (*State, error)`.
  - `worktree.BranchStatus(worktreePath, branch, base string) (*State, error)` — like `Status` but measures ahead/behind for the **registered branch ref** (`refs/heads/<branch>`) against `base`, independent of the worktree's current HEAD. `ctx done` uses it so a switched/detached worktree cannot hide unmerged commits on the task's registered branch; a missing/unreadable branch ref is an error (fail closed).
  - `worktree.Entry{Path, Branch, Head string; Detached, Bare bool}` and `worktree.List(repo string) ([]Entry, error)` — parses `git worktree list --porcelain` so callers can derive *git truth* (which worktrees actually exist and their current branch), not just task.yaml metadata.
  - `worktree.Find(repo, worktreePath string) (*Entry, bool, error)` — looks up the registered worktree whose path matches `worktreePath` (compared as cleaned absolute paths); the bool is false when git has no such worktree (stale/missing metadata).

- [ ] **Step 1: Write the failing integration test**

Create `internal/worktree/worktree_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/worktree/`
Expected: FAIL — undefined `Add`, `Status`, `List`, `Find`, etc.

- [ ] **Step 3: Implement `internal/worktree/worktree.go`**

```go
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

func Add(repo, worktreePath, branch, base string) error {
	abs, err := filepath.Abs(worktreePath)
	if err != nil {
		return err
	}
	_, err = git(repo, "worktree", "add", abs, "-b", branch, base)
	return err
}

func Remove(repo, worktreePath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	abs, _ := filepath.Abs(worktreePath)
	args = append(args, abs)
	_, err := git(repo, args...)
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
		out, err = git(repo, "worktree", "list", "--porcelain")
		if err != nil {
			return nil, err
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
```

> Note: `List`/`Find` use `filepath` (already imported in Step 3 for `Add`/`Remove`) and `os` (for `Find`'s on-disk existence check; added to the import block above).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/worktree/`
Expected: PASS (skips if git absent).

- [ ] **Step 5: Commit**

```bash
git add internal/worktree/
git commit -m "feat: worktree package (git add/remove/status/default-branch/list)"
```

---

## Task 7: `ctx add` — register project + create worktree + scaffold spec

**Files:**
- Create: `internal/cli/add.go`
- Test: `internal/cli/add_test.go`

**Interfaces:**
- Consumes: `task.Load/Save`, `store.Locate`, `worktree.Add`, `worktree.DefaultBranch`.
- Produces: `cli.runAdd(home, taskName string, o addOpts) (*task.Project, error)` where `addOpts{Project, Repo, Branch, Base string}`. Errors `CONFLICT` if project already registered, `USAGE` if repo missing, `NOT_FOUND` if task missing. Defaults: `Branch = "feat/"+taskName`; `Base = DefaultBranch(repo)`, but if the default branch cannot be determined `runAdd` errors `USAGE` (requiring an explicit `--base`) rather than guessing the current checkout.

- [ ] **Step 1: Write the failing integration test**

Create `internal/cli/add_test.go`:

```go
package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/kimhyoyeon/context-manager/internal/worktree"
)

func gitRepoOrSkip(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", a, out)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644)
	for _, a := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		_, _ = c.CombinedOutput()
	}
	return dir
}

func TestRunAddCreatesWorktreeAndSpec(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "add-payment", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	p, err := runAdd(home, "add-payment", addOpts{Project: "front", Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if p.Branch != "feat/add-payment" || p.Base != "main" {
		t.Errorf("bad defaults: %+v", p)
	}
	taskDir := filepath.Join(home, "add-payment")
	if _, err := os.Stat(filepath.Join(taskDir, "front", "wt", "README")); err != nil {
		t.Error("worktree not created")
	}
	if _, err := os.Stat(filepath.Join(taskDir, "front", "spec.md")); err != nil {
		t.Error("spec.md not scaffolded")
	}
	tk, _ := task.Load(taskDir)
	if tk.Project("front") == nil {
		t.Error("project not registered in task.yaml")
	}
}

func TestRunAddRejectsDuplicateProject(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	_, _ = runNew(home, newOpts{Name: "t", Objective: "o", DoneWhen: "d"})
	if _, err := runAdd(home, "t", addOpts{Project: "front", Repo: repo}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "t", addOpts{Project: "front", Repo: repo}); err == nil {
		t.Error("expected CONFLICT on duplicate project")
	}
}

// TestRunAddRollsBackWorktreeOnSpecWriteFailure proves that if spec.md cannot be
// written AFTER the git worktree was created, runAdd removes the worktree and the
// project directory instead of leaving an orphaned worktree that ctx cannot
// discover (it was never recorded in task.yaml). The spec write is forced to fail
// deterministically by pre-creating a non-empty DIRECTORY at the spec.md path, so
// AtomicWrite's final rename onto that path fails.
func TestRunAddRollsBackWorktreeOnSpecWriteFailure(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(home, "tk")
	// Make the spec.md target a non-empty directory so the spec write fails.
	specDir := filepath.Join(taskDir, "front", "spec.md")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "block"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo}); err == nil {
		t.Fatal("expected runAdd to fail when spec.md cannot be written")
	}
	// The worktree must be rolled back: neither the git worktree nor a registered
	// project may survive.
	wtAbs := filepath.Join(taskDir, "front", "wt")
	if _, err := os.Stat(wtAbs); !os.IsNotExist(err) {
		t.Error("worktree directory must be removed after a failed spec write")
	}
	if _, ok, _ := worktree.Find(repo, wtAbs); ok {
		t.Error("git must not still list the rolled-back worktree")
	}
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Project("front") != nil {
		t.Error("project must not be registered in task.yaml after rollback")
	}
}

// TestRunAddRollsBackWorktreeOnTaskSaveFailure proves the full rollback runs when
// the FINAL task.yaml save fails AFTER the worktree and spec were created. Earlier
// failure injection (a read-only task dir) cannot reach this path because
// store.EnsureDir(projDir) fails first, so the save is forced to fail via the
// saveTask seam — a sentinel error returned only after worktree.Add and the spec
// write have already succeeded. The assertions then confirm every artifact runAdd
// created is gone: the git worktree, the project directory runAdd created, and the
// (never persisted) project registration in task.yaml.
func TestRunAddRollsBackWorktreeOnTaskSaveFailure(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(home, "tk")
	// Force the FINAL save to fail with a sentinel, only after the worktree and spec
	// have been written, so this exercises the task.Save rollback rather than an
	// earlier EnsureDir/worktree/spec failure. Restore the real seam afterward.
	errSave := errors.New("injected task.Save failure")
	orig := saveTask
	saveTask = func(string, *task.Task) error { return errSave }
	defer func() { saveTask = orig }()
	_, addErr := runAdd(home, "tk", addOpts{Project: "front", Repo: repo})
	if !errors.Is(addErr, errSave) {
		t.Fatalf("expected the injected task.Save error, got %v", addErr)
	}
	// The git worktree must be rolled back: both the directory and git's worktree list.
	wtAbs := filepath.Join(taskDir, "front", "wt")
	if _, err := os.Stat(wtAbs); !os.IsNotExist(err) {
		t.Error("worktree directory must be removed after a failed task.yaml save")
	}
	if _, ok, _ := worktree.Find(repo, wtAbs); ok {
		t.Error("git must not still list the rolled-back worktree")
	}
	// The project directory runAdd created must be removed (it did not pre-exist).
	if _, err := os.Stat(filepath.Join(taskDir, "front")); !os.IsNotExist(err) {
		t.Error("the created project directory must be removed after a failed task.yaml save")
	}
	// The project must never be registered, since the save that would record it failed.
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Project("front") != nil {
		t.Error("project must not be registered in task.yaml after rollback")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestRunAdd`
Expected: FAIL — undefined `runAdd`, `addOpts`.

- [ ] **Step 3: Implement `internal/cli/add.go`**

```go
package cli

import (
	"os"
	"path/filepath"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/kimhyoyeon/context-manager/internal/worktree"
	"github.com/spf13/cobra"
)

type addOpts struct {
	Project, Repo, Branch, Base string
}

// saveTask is the seam runAdd uses for the FINAL task.yaml write. It exists so a
// test can force the save to fail AFTER the worktree and spec are created, exercising
// the rollback path that real read-only-directory failures cannot reach reliably.
var saveTask = task.Save

func runAdd(home, taskName string, o addOpts) (*task.Project, error) {
	if o.Repo == "" {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "--repo is required")
	}
	// The project name becomes a directory component under the task dir (and the
	// worktree/spec relative paths), so it must be a safe single component.
	if err := store.ValidName(o.Project); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "invalid project name: %v", err)
	}
	taskDir, err := checkedTaskDir(home, taskName)
	if err != nil {
		return nil, err
	}
	tk, err := task.Load(taskDir)
	if err != nil {
		return nil, output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", taskName)
	}
	if tk.Project(o.Project) != nil {
		return nil, output.Errorf(jsonOut, output.ErrConflict, "project %q already registered", o.Project)
	}
	branch := o.Branch
	if branch == "" {
		branch = "feat/" + taskName
	}
	base := o.Base
	if base == "" {
		b, derr := worktree.DefaultBranch(o.Repo)
		if derr != nil {
			// DefaultBranch refuses to guess when no default can be resolved;
			// surface its actionable message so the user passes --base explicitly
			// rather than branching off an unrelated feature checkout.
			return nil, output.Errorf(jsonOut, output.ErrUsage, "%v", derr)
		}
		base = b
	}
	wtRel := filepath.Join(o.Project, "wt")
	specRel := filepath.Join(o.Project, "spec.md")
	projDir := filepath.Join(taskDir, o.Project)
	wtAbs := filepath.Join(taskDir, wtRel)
	specAbs := filepath.Join(taskDir, specRel)
	// Guard the project dir, worktree, and spec targets against the task dir BEFORE
	// any worktree creation, write, or rollback deletion. ValidRelPath canonicalizes
	// both ends via symlink resolution, so a symlinked task/project component (one
	// redirecting outside CTX_HOME) is rejected here; the lexical paths above still
	// address the same real inodes for every legitimate (non-escaping) mutation.
	if _, err := store.ValidRelPath(taskDir, o.Project); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "invalid project path: %v", err)
	}
	if _, err := store.ValidRelPath(taskDir, wtRel); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "invalid worktree path: %v", err)
	}
	if _, err := store.ValidRelPath(taskDir, specRel); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrUsage, "invalid spec path: %v", err)
	}

	// Snapshot any pre-existing regular spec.md so rollback can RESTORE it rather
	// than delete it. The project facet is not yet registered in task.yaml (checked
	// above), but a user could still have hand-written a spec at this path; AtomicWrite
	// would overwrite it and the rollback below would then os.Remove it, destroying
	// user content. priorSpec holds its bytes when it existed as a regular file;
	// hadPriorSpec records that something was there (file or otherwise) so rollback
	// never deletes a path runAdd did not create.
	var priorSpec []byte
	hadPriorSpec := false
	if info, statErr := os.Stat(specAbs); statErr == nil {
		hadPriorSpec = true
		if info.Mode().IsRegular() {
			b, rerr := os.ReadFile(specAbs)
			if rerr != nil {
				return nil, rerr
			}
			priorSpec = b
		}
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	// Track whether WE created the project dir so rollback removes only artifacts
	// runAdd created. EnsureDir accepts a pre-existing dir, so a later failure must
	// not os.RemoveAll a directory (and any pre-existing files) the user already had.
	createdProjDir := false
	if _, statErr := os.Stat(projDir); os.IsNotExist(statErr) {
		createdProjDir = true
	}
	if err := store.EnsureDir(projDir); err != nil {
		return nil, err
	}
	if err := worktree.Add(o.Repo, wtAbs, branch, base); err != nil {
		return nil, output.Errorf(jsonOut, output.ErrGit, "worktree add failed: %v", err)
	}
	// The git worktree now exists but is NOT yet recorded in task.yaml, so ctx can
	// neither discover nor clean it up. Roll back ONLY what runAdd created on any
	// later failure; `committed` is set true only once task.Save records the
	// project, after which the worktree is owned by task.yaml and must survive.
	committed := false
	defer func() {
		if committed {
			return
		}
		// Always undo the worktree (runAdd created it just above). For the spec:
		// restore a pre-existing regular file's bytes, leave any other pre-existing
		// path untouched, and only os.Remove a spec runAdd itself created. Remove the
		// project dir only if runAdd created it — never delete a pre-existing project
		// directory or its prior contents.
		_ = worktree.Remove(o.Repo, wtAbs, true)
		_ = os.RemoveAll(wtAbs)
		switch {
		case priorSpec != nil:
			_ = store.AtomicWrite(specAbs, priorSpec)
		case !hadPriorSpec:
			_ = os.Remove(specAbs)
		}
		if createdProjDir {
			_ = os.RemoveAll(projDir)
		}
	}()
	specBody := "# " + taskName + " — " + o.Project + " spec\n\n(Write the spec for this project's slice of the task here.)\n"
	if err := store.AtomicWrite(specAbs, []byte(specBody)); err != nil {
		return nil, err
	}
	p := task.Project{
		Name: o.Project, Repo: o.Repo, Base: base, Branch: branch,
		Worktree: wtRel, Spec: specRel, Status: "planned",
	}
	tk.Projects = append(tk.Projects, p)
	if err := saveTask(taskDir, tk); err != nil {
		return nil, err
	}
	committed = true
	return &p, nil
}

func init() {
	o := addOpts{}
	var taskFlag string
	cmd := &cobra.Command{
		Use:   "add <project>",
		Short: "Register a project: create its worktree + spec under the current task",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			taskName, err := resolveTask(home, taskFlag)
			if err != nil {
				return err
			}
			o.Project = args[0]
			p, err := runAdd(home, taskName, o)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, "Added project "+p.Name+" (branch "+p.Branch+" from "+p.Base+")", p)
		},
	}
	cmd.Flags().StringVar(&o.Repo, "repo", "", "path to the project's git repo (required)")
	cmd.Flags().StringVar(&o.Branch, "branch", "", "branch name (default feat/<task>)")
	cmd.Flags().StringVar(&o.Base, "base", "", "base branch (default: repo's default branch; required if it cannot be determined)")
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	RootCmd.AddCommand(cmd)
}
```

Add `resolveTask` and `checkedTaskDir` to `internal/cli/util.go`:

```go
import (
	"os"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
)

// checkedTaskDir validates name as a safe single path component (rejecting
// "../victim", "_knowledge", slashes, etc.) BEFORE joining it under CTX_HOME, so
// an explicit --task value can never escape the store or shadow a reserved dir.
// It then canonicalizes the joined task dir and rejects it unless the real
// (symlink-resolved) target is contained beneath canonical CTX_HOME: a lexical
// name check alone cannot catch a task directory that exists as a symlink
// redirecting outside the store, so we resolve both ends before trusting the path
// for any read, write, Git operation, or deletion. Every code path that turns a
// task name into a directory (resolveTask, loadTask, runAdd, runStatusDetail,
// runResume, runDone, runSpec) goes through this helper, which is the single
// checked replacement for raw store.TaskDir(home, name).
func checkedTaskDir(home, name string) (string, error) {
	if err := store.ValidName(name); err != nil {
		return "", output.Errorf(jsonOut, output.ErrUsage, "invalid task name %q: %v", name, err)
	}
	if _, err := store.ValidRelPath(home, name); err != nil {
		return "", output.Errorf(jsonOut, output.ErrUsage, "invalid task name %q: %v", name, err)
	}
	return store.TaskDir(home, name), nil
}

// resolveTask returns the explicit flag if set, else the task detected from CWD.
// An explicit flag is validated here so a caller that only resolves a name (and
// then hands it to checkedTaskDir) still rejects an unsafe --task value early.
func resolveTask(home, flag string) (string, error) {
	if flag != "" {
		if err := store.ValidName(flag); err != nil {
			return "", output.Errorf(jsonOut, output.ErrUsage, "invalid task name %q: %v", flag, err)
		}
		return flag, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	loc, err := store.Locate(home, cwd)
	if err != nil {
		return "", err
	}
	if loc.Task == "" {
		return "", output.Errorf(jsonOut, output.ErrUsage, "not inside a task; pass --task")
	}
	return loc.Task, nil
}
```

> Merge these imports/functions into the existing `util.go` from Task 4 (keep the single `package cli` clause and one import block).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run TestRunAdd`
Expected: PASS (skips if git absent).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/add.go internal/cli/add_test.go internal/cli/util.go
git commit -m "feat: ctx add — register project, create worktree, scaffold spec"
```

---

## Task 8: `ctx ls` + `ctx status`

**Files:**
- Create: `internal/cli/status.go`
- Test: `internal/cli/status_test.go`, `internal/cli/golden_test.go`, and committed goldens under `internal/cli/testdata/` for every `--json` contract in §13: `status_detail.json`, `current.json`, `resume.json`, `goal.json`, `know_search.json`, plus the `goal --handoff` text golden `goal_handoff.txt`.

**Interfaces:**
- Produces:
  - `cli.runLs(home string) (overviewOut, error)` — `overviewOut{Tasks []taskRow}`, `taskRow{Name, Status string; Projects int; Progress string}` (Progress is `"done/total"`).
  - `cli.runStatusDetail(home, taskName string) (detailOut, error)` — `detailOut{Task, Status string; Goal task.Goal; Projects []projectDetail; Worklist []task.WorkItem; Progress progressOut}`, `progressOut{Done, Total int}`. Each `projectDetail` embeds the stored `task.Project` and adds **git-truth** fields derived from `git worktree list --porcelain` (via `worktree.Find`): `WorktreeExists bool` (true only when git **confirms** the worktree exists), `LiveBranch string` (the branch git reports for that worktree, blank when the worktree is missing or unreadable), and `WorktreeError string` (non-empty when the git query itself failed, e.g. an invalid/unreadable repository). A git failure is reported as `worktree_exists:false` **with** a populated `worktree_error`, so an *unreadable* git state is never silently reported as a *confirmed absent* worktree. Status therefore reflects reality, not just `task.yaml`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/status_test.go`:

```go
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

func seedTask(t *testing.T, home, name string) {
	t.Helper()
	if _, err := runNew(home, newOpts{Name: name, Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunLsListsTasksWithProgress(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "alpha")
	seedTask(t, home, "beta")
	ov, err := runLs(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Tasks) != 2 || ov.Tasks[0].Name != "alpha" {
		t.Errorf("bad overview: %+v", ov.Tasks)
	}
	if ov.Tasks[0].Progress != "0/0" {
		t.Errorf("progress got %q want 0/0", ov.Tasks[0].Progress)
	}
}

func TestRunStatusDetailHasGoalAndProgress(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	taskDir := home + "/add-payment"
	writeProjectForTest(t, taskDir, "front")
	d, err := runStatusDetail(home, "add-payment")
	if err != nil {
		t.Fatal(err)
	}
	if d.Goal.DoneWhen != "d" || len(d.Projects) != 1 || d.Progress.Total != 0 {
		t.Errorf("bad detail: %+v", d)
	}
}

// TestRunStatusDetailReportsLiveWorktree exercises git truth: a project with a
// real worktree reports WorktreeExists=true and the live branch from
// `git worktree list`, not just the task.yaml branch field.
func TestRunStatusDetailReportsLiveWorktree(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo}); err != nil {
		t.Fatal(err)
	}
	d, err := runStatusDetail(home, "tk")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %+v", d.Projects)
	}
	if !d.Projects[0].WorktreeExists {
		t.Error("expected WorktreeExists=true for a live worktree")
	}
	if d.Projects[0].LiveBranch != "feat/tk" {
		t.Errorf("live branch: got %q want feat/tk", d.Projects[0].LiveBranch)
	}
}

// TestRunStatusDetailReportsStaleWorktree proves stale/missing metadata is
// reflected: after the worktree directory is removed, git no longer tracks it,
// so WorktreeExists is false and LiveBranch is blank even though task.yaml still
// lists the project.
func TestRunStatusDetailReportsStaleWorktree(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo}); err != nil {
		t.Fatal(err)
	}
	// Delete the worktree directory out-of-band so the task.yaml metadata is now
	// stale relative to git truth.
	if err := os.RemoveAll(filepath.Join(home, "tk", "front", "wt")); err != nil {
		t.Fatal(err)
	}
	d, err := runStatusDetail(home, "tk")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %+v", d.Projects)
	}
	if d.Projects[0].WorktreeExists {
		t.Error("expected WorktreeExists=false for a stale/missing worktree")
	}
	if d.Projects[0].LiveBranch != "" {
		t.Errorf("missing worktree should have blank LiveBranch, got %q", d.Projects[0].LiveBranch)
	}
}

// TestRunStatusDetailReportsUnreadableRepo proves a git FAILURE is not silently
// reported as a confirmed-absent worktree. The project's Repo points at a
// directory that is NOT a git repository, so `git -C <repo> worktree list` fails;
// status must surface that as WorktreeError (non-empty) with WorktreeExists=false,
// never as a clean "worktree confirmed gone" result.
func TestRunStatusDetailReportsUnreadableRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	// A real directory that is NOT a git repo => git worktree list errors out.
	badRepo := t.TempDir()
	taskDir := filepath.Join(home, "tk")
	tk, err := task.Load(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	tk.Projects = append(tk.Projects, task.Project{
		Name: "front", Repo: badRepo, Base: "main",
		Branch: "feat/tk", Worktree: filepath.Join("front", "wt"),
		Spec: filepath.Join("front", "spec.md"), Status: "in_progress",
	})
	if err := task.Save(taskDir, tk); err != nil {
		t.Fatal(err)
	}
	d, err := runStatusDetail(home, "tk")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %+v", d.Projects)
	}
	if d.Projects[0].WorktreeError == "" {
		t.Error("expected a non-empty WorktreeError when the repo is unreadable")
	}
	if d.Projects[0].WorktreeExists {
		t.Error("unreadable git state must not be reported as a confirmed-present worktree")
	}
	if d.Projects[0].LiveBranch != "" {
		t.Errorf("unreadable git state must leave LiveBranch blank, got %q", d.Projects[0].LiveBranch)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestRunLs|TestRunStatus'`
Expected: FAIL — undefined `runLs`, `runStatusDetail`.

- [ ] **Step 3: Implement `internal/cli/status.go`**

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/kimhyoyeon/context-manager/internal/worktree"
	"github.com/spf13/cobra"
)

type taskRow struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Projects int    `json:"projects"`
	Progress string `json:"progress"`
}
type overviewOut struct {
	Tasks []taskRow `json:"tasks"`
}
type progressOut struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// projectDetail is the stored project metadata enriched with git truth derived
// from `git worktree list --porcelain`. WorktreeExists is true only when git
// CONFIRMS the worktree exists; LiveBranch is the branch git currently reports
// for it (blank when missing or unreadable). WorktreeError distinguishes an
// unreadable git state (the query itself failed — e.g. an invalid/unreadable
// repo) from a confirmed-absent worktree: when it is non-empty, WorktreeExists is
// false because git state could not be determined, NOT because the worktree was
// confirmed gone. This keeps `status` from reporting unknown git state as
// confirmed absence.
type projectDetail struct {
	task.Project
	WorktreeExists bool   `json:"worktree_exists"`
	LiveBranch     string `json:"live_branch"`
	WorktreeError  string `json:"worktree_error,omitempty"`
}

type detailOut struct {
	Task     string          `json:"task"`
	Status   string          `json:"status"`
	Goal     task.Goal       `json:"goal"`
	Projects []projectDetail `json:"projects"`
	Worklist []task.WorkItem `json:"worklist"`
	Progress progressOut     `json:"progress"`
}

func runLs(home string) (overviewOut, error) {
	names, err := store.ListTasks(home)
	if err != nil {
		return overviewOut{}, err
	}
	ov := overviewOut{Tasks: []taskRow{}}
	for _, n := range names {
		tk, err := task.Load(store.TaskDir(home, n))
		if err != nil {
			continue
		}
		done, total := tk.Progress()
		ov.Tasks = append(ov.Tasks, taskRow{
			Name: n, Status: string(tk.Status), Projects: len(tk.Projects),
			Progress: fmt.Sprintf("%d/%d", done, total),
		})
	}
	return ov, nil
}

func runStatusDetail(home, taskName string) (detailOut, error) {
	dir, err := checkedTaskDir(home, taskName)
	if err != nil {
		return detailOut{}, err
	}
	tk, err := task.Load(dir)
	if err != nil {
		return detailOut{}, output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", taskName)
	}
	done, total := tk.Progress()
	projects := make([]projectDetail, 0, len(tk.Projects))
	for _, p := range tk.Projects {
		pd := projectDetail{Project: p}
		// Derive git truth: does the registered worktree still exist, and what
		// branch is it actually on? A stale/missing worktree => WorktreeExists
		// false and a blank LiveBranch. A git FAILURE (unreadable/invalid repo) is
		// recorded in WorktreeError and must NOT be collapsed into a confirmed
		// "absent" result, so callers can tell unknown git state apart from a
		// worktree git positively reports as gone.
		if p.Repo != "" && p.Worktree != "" {
			e, ok, ferr := worktree.Find(p.Repo, filepath.Join(dir, p.Worktree))
			switch {
			case ferr != nil:
				pd.WorktreeError = ferr.Error()
			case ok:
				pd.WorktreeExists = true
				pd.LiveBranch = e.Branch
			}
		}
		projects = append(projects, pd)
	}
	return detailOut{
		Task: tk.Name, Status: string(tk.Status), Goal: tk.Goal,
		Projects: projects, Worklist: tk.Worklist,
		Progress: progressOut{Done: done, Total: total},
	}, nil
}

func init() {
	lsCmd := &cobra.Command{
		Use: "ls", Short: "List all tasks",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			ov, err := runLs(home)
			if err != nil {
				return err
			}
			human := fmt.Sprintf("%d task(s)", len(ov.Tasks))
			for _, r := range ov.Tasks {
				human += fmt.Sprintf("\n  %-20s %-7s %d proj  %s", r.Name, r.Status, r.Projects, r.Progress)
			}
			return output.Emit(jsonOut, human, ov)
		},
	}
	var taskFlag string
	statusCmd := &cobra.Command{
		Use: "status", Short: "Detail for the current task, or overview of all tasks",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			name := taskFlag
			if name == "" {
				cwd, _ := os.Getwd()
				if loc, _ := store.Locate(home, cwd); loc != nil {
					name = loc.Task
				}
			}
			if name == "" { // overview
				ov, err := runLs(home)
				if err != nil {
					return err
				}
				return output.Emit(jsonOut, fmt.Sprintf("%d task(s)", len(ov.Tasks)), ov)
			}
			d, err := runStatusDetail(home, name)
			if err != nil {
				return err
			}
			human := fmt.Sprintf("%s [%s]  goal: %s  (%d/%d)", d.Task, d.Status, d.Goal.Objective, d.Progress.Done, d.Progress.Total)
			return output.Emit(jsonOut, human, d)
		},
	}
	statusCmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	RootCmd.AddCommand(lsCmd, statusCmd)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run 'TestRunLs|TestRunStatus'`
Expected: PASS.

- [ ] **Step 5: Add a shared golden harness + the `status`/`current` goldens**

§13 locks the `--json` schema for `status`, `current`, `resume`, `goal`, and
`know search`, plus the `goal --handoff` text. The contracts whose producers
exist by this task (`status`, `current`) are locked here; the rest are locked in
their producing tasks (Task 11: `resume`, `goal`, `goal --handoff`; Task 13:
`know search`) using the **same** `assertGolden` harness defined below.

Create `internal/cli/golden_test.go`:

```go
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// assertGolden marshals v (or uses raw text when v is a string) and compares it
// to testdata/<name>; UPDATE_GOLDEN=1 regenerates the file. All locked --json /
// text contracts in §13 go through this one helper.
func assertGolden(t *testing.T, name string, v any) {
	t.Helper()
	var got []byte
	if s, ok := v.(string); ok {
		got = []byte(s)
	} else {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		got = b
	}
	golden := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("missing golden %s (run UPDATE_GOLDEN=1): %v", name, err)
	}
	if string(got) != string(want) {
		t.Errorf("contract drift for %s.\n got: %s\nwant: %s", name, got, want)
	}
}

func TestStatusDetailJSONGolden(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	// Register a repo-less project so the git-truth lookup is skipped entirely
	// (runStatusDetail only queries git when both Repo and Worktree are set). This
	// keeps the golden deterministic: worktree_exists is false, live_branch is "",
	// and worktree_error is omitted — no machine-specific git error text leaks into
	// the locked contract.
	writeRepolessProjectForTest(t, filepath.Join(home, "add-payment"), "front")
	d, _ := runStatusDetail(home, "add-payment")
	assertGolden(t, "status_detail.json", d)
}

func TestCurrentJSONGolden(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	taskDir := filepath.Join(home, "add-payment")
	writeProjectForTest(t, taskDir, "front")
	out, err := runCurrent(home, filepath.Join(taskDir, "front", "wt"))
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "current.json", out)
}
```

Generate both goldens once:

Run: `UPDATE_GOLDEN=1 go test ./internal/cli/ -run 'TestStatusDetailJSONGolden|TestCurrentJSONGolden'`
Then inspect `internal/cli/testdata/status_detail.json` (contains `task`,
`status`, `goal{objective,done_when}`, `projects[]` — each project embeds the
stored fields plus the git-truth fields `worktree_exists` and `live_branch`,
which are `false`/`""` here because the fixture project is repo-less so the git
query is skipped; `worktree_error` is omitted for the same reason —,
`worklist`, `progress{done,total}`) and `current.json` (contains `task`,
`project`, `spec`, `worktree`, `status`).

- [ ] **Step 6: Run to verify it passes**

Run: `go test ./internal/cli/ -run 'TestStatusDetailJSONGolden|TestCurrentJSONGolden'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/status.go internal/cli/status_test.go internal/cli/golden_test.go internal/cli/testdata/
git commit -m "feat: ctx ls and ctx status (+ JSON goldens for status detail and current)"
```

---

## Task 9: worklist commands (`ctx task ...`) + `ctx set-status`

**Files:**
- Create: `internal/cli/task.go`, `internal/cli/setstatus.go`
- Test: `internal/cli/task_test.go`, plus the committed `task ls` JSON golden `internal/cli/testdata/task_ls.json` (§Global Constraints lists `task ls` as a stable `--json` contract). The golden uses the shared `assertGolden` harness from Task 8.

**Interfaces:**
- Produces:
  - `cli.runTaskAdd(home, taskName, text string) (task.WorkItem, error)`
  - `cli.runTaskSet(home, taskName string, id int, status string) error` (status in `todo|doing|done`; `NOT_FOUND` if id absent)
  - `cli.runTaskLs(home, taskName string) (worklistOut, error)` — `worklistOut{Items []task.WorkItem; Progress progressOut}`
  - `cli.runSetStatus(home, taskName, project, status string) error` (project status vocab)

- [ ] **Step 1: Write the failing test**

Create `internal/cli/task_test.go`:

```go
package cli

import (
	"path/filepath"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

func TestWorklistAddSetLs(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	it, err := runTaskAdd(home, "tk", "build api")
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != 1 || it.Status != "todo" {
		t.Errorf("bad item: %+v", it)
	}
	if err := runTaskSet(home, "tk", 1, "doing"); err != nil {
		t.Fatal(err)
	}
	out, _ := runTaskLs(home, "tk")
	if out.Items[0].Status != "doing" || out.Progress.Total != 1 {
		t.Errorf("bad worklist: %+v", out)
	}
	if err := runTaskSet(home, "tk", 99, "done"); err == nil {
		t.Error("expected NOT_FOUND for missing id")
	}
}

func TestSetStatusUpdatesProject(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	writeProjectForTest(t, filepath.Join(home, "tk"), "front")
	if err := runSetStatus(home, "tk", "front", "review"); err != nil {
		t.Fatal(err)
	}
	tk, _ := task.Load(filepath.Join(home, "tk"))
	if tk.Project("front").Status != "review" {
		t.Errorf("status not updated: %v", tk.Project("front").Status)
	}
}

// TestResolveTaskProjectFromCWD covers the optional --project path: with no
// flags, both task and project come from the CWD-detected location.
func TestResolveTaskProjectFromCWD(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	taskDir := filepath.Join(home, "tk")
	writeProjectForTest(t, taskDir, "front")
	cwd := filepath.Join(taskDir, "front", "wt")

	name, proj, err := resolveTaskProject(home, cwd, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "tk" || proj != "front" {
		t.Errorf("CWD resolution failed: task=%q project=%q", name, proj)
	}

	// Explicit flags still override the CWD.
	name, proj, err = resolveTaskProject(home, cwd, "tk", "back")
	if err != nil || name != "tk" || proj != "back" {
		t.Errorf("flag override failed: task=%q project=%q err=%v", name, proj, err)
	}

	// Outside any task and with no flags -> USAGE error.
	if _, _, err := resolveTaskProject(home, home, "", ""); err == nil {
		t.Error("expected USAGE error when no task can be resolved")
	}
}

// TestTaskLsJSONGolden locks the `task ls` --json contract (§Global Constraints
// lists `task ls` among the stable JSON contracts). A fixed two-item worklist in
// known states makes worklistOut{items,progress} deterministic. Uses the shared
// assertGolden harness from Task 8.
func TestTaskLsJSONGolden(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "add-payment")
	if _, err := runTaskAdd(home, "add-payment", "build ui"); err != nil {
		t.Fatal(err)
	}
	if _, err := runTaskAdd(home, "add-payment", "build api"); err != nil {
		t.Fatal(err)
	}
	if err := runTaskSet(home, "add-payment", 1, "done"); err != nil {
		t.Fatal(err)
	}
	if err := runTaskSet(home, "add-payment", 2, "doing"); err != nil {
		t.Fatal(err)
	}
	out, err := runTaskLs(home, "add-payment")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "task_ls.json", out)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestWorklist|TestSetStatus'`
Expected: FAIL — undefined run functions.

- [ ] **Step 3: Implement `internal/cli/task.go`**

```go
package cli

import (
	"fmt"
	"strconv"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

type worklistOut struct {
	Items    []task.WorkItem `json:"items"`
	Progress progressOut     `json:"progress"`
}

func loadTask(home, name string) (string, *task.Task, error) {
	dir, err := checkedTaskDir(home, name)
	if err != nil {
		return "", nil, err
	}
	tk, err := task.Load(dir)
	if err != nil {
		return "", nil, output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", name)
	}
	return dir, tk, nil
}

func runTaskAdd(home, taskName, text string) (task.WorkItem, error) {
	dir, tk, err := loadTask(home, taskName)
	if err != nil {
		return task.WorkItem{}, err
	}
	id := tk.AddWork(text)
	if err := task.Save(dir, tk); err != nil {
		return task.WorkItem{}, err
	}
	return task.WorkItem{ID: id, Text: text, Status: "todo"}, nil
}

func runTaskSet(home, taskName string, id int, status string) error {
	if status != "todo" && status != "doing" && status != "done" {
		return output.Errorf(jsonOut, output.ErrUsage, "status must be todo|doing|done")
	}
	dir, tk, err := loadTask(home, taskName)
	if err != nil {
		return err
	}
	if !tk.SetWork(id, status) {
		return output.Errorf(jsonOut, output.ErrNotFound, "worklist item %d not found", id)
	}
	return task.Save(dir, tk)
}

func runTaskLs(home, taskName string) (worklistOut, error) {
	_, tk, err := loadTask(home, taskName)
	if err != nil {
		return worklistOut{}, err
	}
	done, total := tk.Progress()
	items := tk.Worklist
	if items == nil {
		items = []task.WorkItem{}
	}
	return worklistOut{Items: items, Progress: progressOut{Done: done, Total: total}}, nil
}

func init() {
	var taskFlag string
	taskCmd := &cobra.Command{Use: "task", Short: "Manage the worklist (todo/doing/done items)"}
	taskCmd.PersistentFlags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")

	add := &cobra.Command{
		Use: "add <text>", Short: "Add a worklist item", Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, name, err := homeAndTask(taskFlag)
			if err != nil {
				return err
			}
			it, err := runTaskAdd(home, name, joinArgs(args))
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, fmt.Sprintf("#%d %s", it.ID, it.Text), it)
		},
	}
	setCmd := func(status string) *cobra.Command {
		return &cobra.Command{
			Use: status + " <id>", Short: "Mark a worklist item " + status, Args: cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				home, name, err := homeAndTask(taskFlag)
				if err != nil {
					return err
				}
				id, cerr := strconv.Atoi(args[0])
				if cerr != nil {
					return output.Errorf(jsonOut, output.ErrUsage, "id must be a number")
				}
				if err := runTaskSet(home, name, id, status); err != nil {
					return err
				}
				return output.Emit(jsonOut, fmt.Sprintf("#%d -> %s", id, status), map[string]any{"id": id, "status": status})
			},
		}
	}
	lsCmd := &cobra.Command{
		Use: "ls", Short: "List worklist items",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, name, err := homeAndTask(taskFlag)
			if err != nil {
				return err
			}
			out, err := runTaskLs(home, name)
			if err != nil {
				return err
			}
			human := fmt.Sprintf("%d/%d done", out.Progress.Done, out.Progress.Total)
			for _, it := range out.Items {
				human += fmt.Sprintf("\n  #%d [%s] %s", it.ID, it.Status, it.Text)
			}
			return output.Emit(jsonOut, human, out)
		},
	}
	taskCmd.AddCommand(add, setCmd("todo"), setCmd("doing"), setCmd("done"), lsCmd)
	RootCmd.AddCommand(taskCmd)
}
```

Add helpers to `internal/cli/util.go`:

```go
import "strings" // add to util.go import block

func joinArgs(a []string) string { return strings.Join(a, " ") }

// homeAndTask resolves CTX_HOME and the active task (flag or CWD).
func homeAndTask(taskFlag string) (string, string, error) {
	home, err := store.Home()
	if err != nil {
		return "", "", err
	}
	name, err := resolveTask(home, taskFlag)
	return home, name, err
}
```

- [ ] **Step 4: Implement `internal/cli/setstatus.go`**

```go
package cli

import (
	"os"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

func runSetStatus(home, taskName, project, status string) error {
	switch status {
	case "planned", "in_progress", "review", "done":
	default:
		return output.Errorf(jsonOut, output.ErrUsage, "status must be planned|in_progress|review|done")
	}
	dir, tk, err := loadTask(home, taskName)
	if err != nil {
		return err
	}
	p := tk.Project(project)
	if p == nil {
		return output.Errorf(jsonOut, output.ErrNotFound, "project %q not found", project)
	}
	p.Status = task.ProjStatus(status)
	return task.Save(dir, tk)
}

// resolveTaskProject derives the task and project for set-status: explicit
// flags win, otherwise both fall back to the CWD-detected location. It returns a
// USAGE error only when neither a flag nor the CWD can supply the value.
func resolveTaskProject(home, cwd, taskFlag, projectFlag string) (string, string, error) {
	loc, _ := store.Locate(home, cwd)
	name := taskFlag
	if name == "" && loc != nil {
		name = loc.Task
	}
	if name == "" {
		return "", "", output.Errorf(jsonOut, output.ErrUsage, "not inside a task; pass --task")
	}
	proj := projectFlag
	if proj == "" && loc != nil {
		proj = loc.Project
	}
	if proj == "" {
		return "", "", output.Errorf(jsonOut, output.ErrUsage, "no project resolved; pass --project")
	}
	return name, proj, nil
}

func init() {
	var taskFlag, project string
	cmd := &cobra.Command{
		Use: "set-status <planned|in_progress|review|done>", Short: "Set a project's status",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			name, proj, err := resolveTaskProject(home, cwd, taskFlag, project)
			if err != nil {
				return err
			}
			if err := runSetStatus(home, name, proj, args[0]); err != nil {
				return err
			}
			return output.Emit(jsonOut, proj+" -> "+args[0], map[string]string{"project": proj, "status": args[0]})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project name (default: detect from CWD)")
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	RootCmd.AddCommand(cmd)
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/cli/ -run 'TestWorklist|TestSetStatus|TestResolveTaskProject'`
Expected: PASS.

- [ ] **Step 6: Generate the `task ls` JSON golden**

`task ls` is a stable `--json` contract (§Global Constraints), so lock its
schema with the same `assertGolden` harness defined in Task 8. Generate the
golden once:

Run: `UPDATE_GOLDEN=1 go test ./internal/cli/ -run TestTaskLsJSONGolden`
Then inspect `internal/cli/testdata/task_ls.json` — it contains `items[]` (each
`{id,text,status}`) and `progress{done,total}` for the fixed two-item worklist
(`#1 done`, `#2 doing` → `progress 1/2`).

- [ ] **Step 7: Re-run to verify the golden passes**

Run: `go test ./internal/cli/ -run TestTaskLsJSONGolden`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/task.go internal/cli/setstatus.go internal/cli/task_test.go internal/cli/util.go internal/cli/testdata/task_ls.json
git commit -m "feat: ctx task worklist commands + ctx set-status (+ task ls JSON golden)"
```

---

## Task 10: `resume.ParseSections` + `ctx log`

**Files:**
- Create: `internal/resume/resume.go`, `internal/cli/log.go`
- Test: `internal/resume/parse_test.go`, `internal/cli/log_test.go`

**Interfaces:**
- Produces:
  - `resume.ParseSections(md string) map[string]string` — heading (without `## `) → trimmed body; absent sections simply not in the map.
  - `resume.Section(m map[string]string, name string) string` — body or `"MISSING"` if absent/empty.
  - `resume.JournalEntries(md string) []string` — bullet lines under `## Journal`.
  - `resume.AppendJournal(contextPath, dateOnly, msg string) error` — reads and parses context.md, inserts `- [date] msg` inside the `## Journal` section (works even when other sections follow Journal), and atomically rewrites the file via `store.AtomicWrite`.
  - `cli.runLog(home, taskName, msg string) error`.

- [ ] **Step 1: Write the failing test for parsing**

Create `internal/resume/parse_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/resume/ -run 'TestParse|TestJournal'`
Expected: FAIL — undefined functions.

- [ ] **Step 3: Implement parsing in `internal/resume/resume.go`**

```go
package resume

import (
	"os"
	"strings"

	"github.com/kimhyoyeon/context-manager/internal/store"
)

// ParseSections splits markdown by "## Heading" into heading->body (trimmed).
func ParseSections(md string) map[string]string {
	m := map[string]string{}
	var cur string
	var buf []string
	flush := func() {
		if cur != "" {
			m[cur] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
		buf = nil
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			cur = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if cur != "" {
			buf = append(buf, line)
		}
	}
	flush()
	return m
}

func Section(m map[string]string, name string) string {
	if v, ok := m[name]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return "MISSING"
}

func JournalEntries(md string) []string {
	body := ParseSections(md)["Journal"]
	var out []string
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "- ") {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

// AppendJournal inserts "- [date] msg" at the end of the ## Journal section and
// rewrites context.md atomically (store.AtomicWrite). It does NOT assume Journal
// is the final section: it locates the Journal heading, appends the entry after
// the last existing content line of that section, and preserves any sections
// that follow Journal. If no Journal section exists, one is appended at the end.
func AppendJournal(contextPath, dateOnly, msg string) error {
	data, err := os.ReadFile(contextPath)
	if err != nil {
		return err
	}
	entry := "- [" + dateOnly + "] " + msg
	lines := strings.Split(string(data), "\n")

	// Find the Journal heading and the start of the next "## " heading (if any).
	journalIdx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "## Journal" {
			journalIdx = i
			break
		}
	}
	if journalIdx < 0 {
		// No Journal section: append one at the end.
		out := strings.TrimRight(string(data), "\n")
		out += "\n\n## Journal\n" + entry + "\n"
		return store.AtomicWrite(contextPath, []byte(out))
	}
	nextIdx := len(lines)
	for i := journalIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			nextIdx = i
			break
		}
	}
	// Insertion point: just after the last non-blank line within the section,
	// so the entry lands under Journal even when other sections follow.
	insertAt := journalIdx + 1
	for i := journalIdx + 1; i < nextIdx; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			insertAt = i + 1
		}
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, entry)
	out = append(out, lines[insertAt:]...)
	return store.AtomicWrite(contextPath, []byte(strings.Join(out, "\n")))
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/resume/ -run 'TestParse|TestJournal'`
Expected: PASS.

- [ ] **Step 5: Write the failing test for `ctx log`**

Create `internal/cli/log_test.go`:

```go
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
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestRunLog`
Expected: FAIL — undefined `runLog`.

- [ ] **Step 7: Implement `internal/cli/log.go`**

```go
package cli

import (
	"time"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/resume"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

func runLog(home, taskName, msg string) error {
	dir, err := checkedTaskDir(home, taskName)
	if err != nil {
		return err
	}
	if _, err := task.Load(dir); err != nil {
		return output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", taskName)
	}
	date := time.Now().UTC().Format("2006-01-02")
	return resume.AppendJournal(task.ContextPath(dir), date, msg)
}

func init() {
	var taskFlag string
	cmd := &cobra.Command{
		Use: "log <message>", Short: "Append a dated entry to context.md Journal",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, name, err := homeAndTask(taskFlag)
			if err != nil {
				return err
			}
			if err := runLog(home, name, joinArgs(args)); err != nil {
				return err
			}
			return output.Emit(jsonOut, "logged", map[string]string{"task": name})
		},
	}
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	RootCmd.AddCommand(cmd)
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./internal/cli/ -run TestRunLog ./internal/resume/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/resume/resume.go internal/resume/parse_test.go internal/cli/log.go internal/cli/log_test.go
git commit -m "feat: context.md section parsing + ctx log journal append"
```

---

## Task 11: `ctx resume` (rehydration bundle) + `ctx goal`

**Files:**
- Create: `internal/resume/bundle.go`, `internal/goal/goal.go`, `internal/cli/resume.go`, `internal/cli/goal.go`
- Test: `internal/resume/bundle_test.go`, `internal/goal/goal_test.go`, `internal/cli/goal_test.go`

**Interfaces:**
- Produces:
  - `resume.ProjState{Name, Branch, Base string; Dirty bool; Ahead, Behind int; Status string; Available bool; Error string}` — `Available` is true only when **live** git state was actually read; when the worktree is missing/unreadable, `Available` is false, `Error` carries the reason, and the `Dirty`/`Ahead`/`Behind` fields are left zero (they MUST NOT be presented as real live state).
  - `resume.Bundle{Task, Status string; Goal task.Goal; Background, Plan string; Worklist []task.WorkItem; Next string; RecentJournal []string; Projects []ProjState}`
  - `resume.Build(taskDir string, tk *task.Task, gitState func(task.Project) resume.ProjState) (*resume.Bundle, error)` — reads context.md, fills Background/Plan via `Section` (so empties become `"MISSING"`), last 5 journal entries, `Next = tk.NextWork()`, and one ProjState per project via the injected `gitState`. The `gitState` callback signature is unchanged; the unavailable/error state is carried inside the returned `ProjState`.
  - `cli.resumeHuman(b *resume.Bundle) string` — renders the COMPLETE bundle for **text mode** (not just `--json`): goal, Background, Plan, worklist with progress, next item, recent journal, and each project's live git state (or an explicit "git state unavailable: <reason>" when the worktree could not be read). SPEC §7 requires `ctx resume` itself to reload the full live context.
  - `goal.Handoff(tk *task.Task) string` — deterministic handoff text ending with `  /goal <done_when>`.
  - `cli.runGoalSet(home, taskName, objective, doneWhen string) (task.Goal, error)`.

- [ ] **Step 1: Write the failing test for `resume.Build`**

Create `internal/resume/bundle_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/resume/ -run TestBuild`
Expected: FAIL — undefined `Build`, `ProjState`, `Bundle`.

- [ ] **Step 3: Implement `internal/resume/bundle.go`**

```go
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
	if data, err := os.ReadFile(filepath.Join(taskDir, "context.md")); err == nil {
		md = string(data)
	}
	sections := ParseSections(md)
	journal := JournalEntries(md)
	if len(journal) > 5 {
		journal = journal[len(journal)-5:]
	}
	b := &Bundle{
		Task: tk.Name, Status: string(tk.Status), Goal: tk.Goal,
		Background: Section(sections, "Background"), Plan: Section(sections, "Plan"),
		Worklist: tk.Worklist, Next: tk.NextWork(), RecentJournal: journal,
		Projects: []ProjState{},
	}
	for _, p := range tk.Projects {
		b.Projects = append(b.Projects, gitState(p))
	}
	return b, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/resume/ -run TestBuild`
Expected: PASS.

- [ ] **Step 5: Write the failing test for `goal.Handoff`**

Create `internal/goal/goal_test.go`:

```go
package goal

import (
	"strings"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

func TestHandoffEndsWithGoalCommand(t *testing.T) {
	tk := &task.Task{Name: "add-payment", Goal: task.Goal{Objective: "add pay", DoneWhen: "e2e pass + lint clean"}}
	out := Handoff(tk)
	if !strings.Contains(out, "add pay") {
		t.Errorf("missing objective:\n%s", out)
	}
	if !strings.Contains(out, "/goal e2e pass + lint clean") {
		t.Errorf("missing /goal line:\n%s", out)
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/goal/`
Expected: FAIL — undefined `Handoff`.

- [ ] **Step 7: Implement `internal/goal/goal.go`**

```go
package goal

import "github.com/kimhyoyeon/context-manager/internal/task"

// Handoff builds deterministic, model-facing text that re-arms native /goal.
func Handoff(tk *task.Task) string {
	return "Goal handoff for task " + tk.Name + ":\n" +
		"Objective: " + tk.Goal.Objective + "\n" +
		"Run this in-session to re-arm the native goal:\n" +
		"  /goal " + tk.Goal.DoneWhen + "\n"
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./internal/goal/`
Expected: PASS.

- [ ] **Step 9: Implement `internal/cli/resume.go` and `internal/cli/goal.go`**

`internal/cli/resume.go`:

```go
package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/resume"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/kimhyoyeon/context-manager/internal/worktree"
	"github.com/spf13/cobra"
)

func runResume(home, taskName string) (*resume.Bundle, error) {
	dir, err := checkedTaskDir(home, taskName)
	if err != nil {
		return nil, err
	}
	tk, err := task.Load(dir)
	if err != nil {
		return nil, output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", taskName)
	}
	gitState := func(p task.Project) resume.ProjState {
		// Start from stored metadata; mark live state UNAVAILABLE until proven.
		ps := resume.ProjState{Name: p.Name, Base: p.Base, Branch: p.Branch, Status: string(p.Status)}
		st, err := worktree.Status(filepath.Join(dir, p.Worktree), p.Base)
		if err != nil {
			// Spec requires LIVE git state: a missing/unreadable worktree must be
			// reported as an explicit error, NOT fabricated as clean (dirty=false,
			// ahead/behind=0). Leave those fields zero and surface the reason.
			ps.Available = false
			ps.Error = err.Error()
			return ps
		}
		ps.Available = true
		ps.Branch, ps.Dirty, ps.Ahead, ps.Behind = st.Branch, st.Dirty, st.Ahead, st.Behind
		return ps
	}
	return resume.Build(dir, tk, gitState)
}

func init() {
	var taskFlag string
	cmd := &cobra.Command{
		Use: "resume [task]", Short: "Rehydrate full task context (goal + state) for a new session",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			// Resolution order: positional <task>, then --task, then CWD.
			name := taskFlag
			if len(args) == 1 {
				name = args[0]
			}
			if name == "" {
				if name, err = resolveTask(home, ""); err != nil {
					return err
				}
			}
			b, err := runResume(home, name)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, resumeHuman(b), b)
		},
	}
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: positional arg, then CWD)")
	RootCmd.AddCommand(cmd)
}

// resumeHuman renders the COMPLETE rehydration bundle for text mode. SPEC §7
// requires `ctx resume` (not only `--json`) to reload the full context and live
// project state, so every field of the Bundle is printed: goal, Background, Plan,
// worklist with progress, the next item, recent journal entries, and each
// project's live git state (or an explicit "unavailable: <reason>" when the
// worktree could not be read, never fabricated clean state).
func resumeHuman(b *resume.Bundle) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "TASK: %s [%s]\n", b.Task, b.Status)
	fmt.Fprintf(&sb, "GOAL: %s\n  done_when: %s\n", b.Goal.Objective, b.Goal.DoneWhen)
	fmt.Fprintf(&sb, "\n## Background\n%s\n", b.Background)
	fmt.Fprintf(&sb, "\n## Plan\n%s\n", b.Plan)

	done, total := 0, len(b.Worklist)
	for _, w := range b.Worklist {
		if w.Status == "done" {
			done++
		}
	}
	fmt.Fprintf(&sb, "\n## Worklist (%d/%d done)\n", done, total)
	for _, w := range b.Worklist {
		fmt.Fprintf(&sb, "  #%d [%s] %s\n", w.ID, w.Status, w.Text)
	}

	next := b.Next
	if next == "" {
		next = "(none)"
	}
	fmt.Fprintf(&sb, "\nNext: %s\n", next)

	fmt.Fprintf(&sb, "\n## Recent Journal\n")
	if len(b.RecentJournal) == 0 {
		fmt.Fprintf(&sb, "  (none)\n")
	}
	for _, j := range b.RecentJournal {
		fmt.Fprintf(&sb, "  %s\n", j)
	}

	fmt.Fprintf(&sb, "\n## Projects\n")
	if len(b.Projects) == 0 {
		fmt.Fprintf(&sb, "  (none)\n")
	}
	for _, p := range b.Projects {
		if p.Available {
			dirty := "clean"
			if p.Dirty {
				dirty = "dirty"
			}
			fmt.Fprintf(&sb, "  %-12s %-10s branch=%s base=%s %s ahead=%d behind=%d\n",
				p.Name, p.Status, p.Branch, p.Base, dirty, p.Ahead, p.Behind)
		} else {
			fmt.Fprintf(&sb, "  %-12s %-10s branch=%s base=%s git state unavailable: %s\n",
				p.Name, p.Status, p.Branch, p.Base, p.Error)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
```

`internal/cli/goal.go`:

```go
package cli

import (
	"github.com/kimhyoyeon/context-manager/internal/goal"
	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

func runGoalSet(home, taskName, objective, doneWhen string) (task.Goal, error) {
	dir, tk, err := loadTask(home, taskName)
	if err != nil {
		return task.Goal{}, err
	}
	if objective != "" {
		tk.Goal.Objective = objective
	}
	if doneWhen != "" {
		tk.Goal.DoneWhen = doneWhen
	}
	if err := task.Save(dir, tk); err != nil {
		return task.Goal{}, err
	}
	return tk.Goal, nil
}

func init() {
	var taskFlag string
	var handoff bool
	var objective, doneWhen string
	cmd := &cobra.Command{
		Use: "goal", Short: "Show, set, or hand off the task's north-star goal",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, name, err := homeAndTask(taskFlag)
			if err != nil {
				return err
			}
			_, tk, err := loadTask(home, name)
			if err != nil {
				return err
			}
			if handoff {
				return output.Emit(jsonOut, goal.Handoff(tk), map[string]string{"handoff": goal.Handoff(tk)})
			}
			return output.Emit(jsonOut, "objective: "+tk.Goal.Objective+"\ndone_when: "+tk.Goal.DoneWhen, tk.Goal)
		},
	}
	setCmd := &cobra.Command{
		Use: "set", Short: "Set/edit the goal (objective and/or done_when)",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, name, err := homeAndTask(taskFlag)
			if err != nil {
				return err
			}
			g, err := runGoalSet(home, name, objective, doneWhen)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, "goal updated", g)
		},
	}
	cmd.PersistentFlags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	cmd.Flags().BoolVar(&handoff, "handoff", false, "emit native /goal handoff text")
	setCmd.Flags().StringVar(&objective, "objective", "", "new objective")
	setCmd.Flags().StringVar(&doneWhen, "done-when", "", "new completion condition")
	cmd.AddCommand(setCmd)
	RootCmd.AddCommand(cmd)
}
```

Add the `ctx goal` test `internal/cli/goal_test.go`:

```go
package cli

import "testing"

func TestRunGoalSetUpdatesDoneWhen(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	g, err := runGoalSet(home, "tk", "", "all tests pass")
	if err != nil {
		t.Fatal(err)
	}
	if g.DoneWhen != "all tests pass" || g.Objective != "o" {
		t.Errorf("bad goal: %+v", g)
	}
}
```

Add the positional + live-git resume tests `internal/cli/resume_test.go`:

```go
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
```

Generate these goldens once:

Run: `UPDATE_GOLDEN=1 go test ./internal/cli/ -run 'TestResumeJSONGolden|TestGoalJSONGolden|TestGoalHandoffTextGolden'`
Then verify `internal/cli/testdata/{resume.json,goal.json,goal_handoff.txt}` exist.

- [ ] **Step 10: Run to verify everything passes**

Run: `go test ./internal/resume/ ./internal/goal/ ./internal/cli/ -run 'TestBuild|TestHandoff|TestRunGoal|TestRunResume|TestResumeLiveGitState|TestResumeHuman'`
Expected: PASS (the live-git test skips if git is absent).

- [ ] **Step 11: Commit**

```bash
git add internal/resume/bundle.go internal/resume/bundle_test.go internal/goal/ internal/cli/resume.go internal/cli/resume_test.go internal/cli/goal.go internal/cli/goal_test.go internal/cli/testdata/
git commit -m "feat: ctx resume rehydration bundle + ctx goal (+/goal handoff, JSON+text goldens)"
```

---

## Task 12: `knowledge` — topic-page model, merge, `_shared/` promotion, index/log

**Files:**
- Create: `internal/knowledge/knowledge.go`
- Test: `internal/knowledge/knowledge_test.go`

**Interfaces:**
- Produces:
  - `knowledge.Source{Task, When, PR string}`
  - `knowledge.Page{Project []string; Category, Topic, Status string; Tags []string; Sources []Source; LastReviewed string; Body string}` (Body has `yaml:"-"`).
  - `knowledge.PageInput{Projects []string; Topic, Category, Body, SourceTask, When string; Tags []string}`
  - `knowledge.Slug(s string) string`, `knowledge.Render(p *Page) []byte`, `knowledge.ParsePage(data []byte) (*Page, error)`.
  - `knowledge.ValidCategory(c string) bool` — true when `c` is empty (category optional) or one of the controlled vocabulary `architecture|decision|gotcha|pattern`. `Add` rejects any other nonempty category with an error.
  - `knowledge.HasProvenance(home, taskName string, taskProjects []string) (bool, error)` — true when a page records `taskName` as a `sources:` entry **and** that page's project facet overlaps `taskProjects` (used by `ctx done` to require the task's relevant per-project findings were consolidated before deletion). When `taskProjects` is empty, any same-task page qualifies.
  - `knowledge.Add(home string, in PageInput) (path string, err error)` — create or merge **within the project scope declared by `in.Projects`**: a per-project page is found only under the requested `<project>/` directories, so the same topic slug in two *different* single-project folders stays isolated. An existing `_shared/<topic>.md` is merged into **only when its `Project` facet overlaps `in.Projects`** — an unrelated project (e.g. `cli`) never merges into a shared page owned by other repos (e.g. `front`+`back`); it gets its own independent `<project>/` page instead. Union `Project`/`Tags`, append `Sources`, append body as a dated `## Update` block. **Target dir = `_shared/` only when the resulting `Project` facet lists ≥ 2 repos** — i.e. when `in.Projects` itself names ≥ 2, or when this add merges into an existing page already spanning other projects. When an explicit multi-project add finds **several** in-scope per-project pages for the topic (e.g. `front/pay.md` AND `back/pay.md`), it merges **all** of them (union facets/tags/sources, concatenate bodies). Promotion writes the new (`_shared/`) page first and removes **every** superseded per-project page only after the write succeeds, so no duplicate of the topic is left behind. `in.SourceTask` must be non-empty (the originating task — durable provenance). Then regenerate `index.md` and append to `log.md`, propagating any log error.

- [ ] **Step 1: Write the failing test**

Create `internal/knowledge/knowledge_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/knowledge/`
Expected: FAIL — undefined `Add`, `PageInput`.

- [ ] **Step 3: Implement `internal/knowledge/knowledge.go`**

```go
package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/kimhyoyeon/context-manager/internal/store"
	"gopkg.in/yaml.v3"
)

type Source struct {
	Task string `yaml:"task" json:"task"`
	When string `yaml:"when" json:"when"`
	PR   string `yaml:"pr,omitempty" json:"pr,omitempty"`
}
type Page struct {
	Project      []string `yaml:"project" json:"project"`
	Category     string   `yaml:"category,omitempty" json:"category,omitempty"`
	Topic        string   `yaml:"topic" json:"topic"`
	Status       string   `yaml:"status" json:"status"`
	Tags         []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Sources      []Source `yaml:"sources,omitempty" json:"sources,omitempty"`
	LastReviewed string   `yaml:"last_reviewed,omitempty" json:"last_reviewed,omitempty"`
	Body         string   `yaml:"-" json:"-"`
}
type PageInput struct {
	Projects   []string
	Topic      string
	Category   string
	Body       string
	SourceTask string
	When       string
	Tags       []string
}

func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func Render(p *Page) []byte {
	fm, _ := yaml.Marshal(p)
	return []byte("---\n" + string(fm) + "---\n\n" + strings.TrimSpace(p.Body) + "\n")
}

func ParsePage(data []byte) (*Page, error) {
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		return &Page{Body: s}, nil
	}
	rest := s[4:]
	i := strings.Index(rest, "\n---")
	if i < 0 {
		return &Page{Body: s}, nil
	}
	var p Page
	if err := yaml.Unmarshal([]byte(rest[:i]), &p); err != nil {
		return nil, err
	}
	p.Body = strings.TrimLeft(rest[i+4:], "\n")
	return &p, nil
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func targetDir(home string, projects []string) string {
	if len(projects) >= 2 {
		return store.SharedDir(home)
	}
	return filepath.Join(store.KnowledgeDir(home), projects[0])
}

// pageAt loads a single topic page at a specific group directory, if present.
func pageAt(home, group, topicSlug string) (string, *Page) {
	cand := filepath.Join(store.KnowledgeDir(home), group, topicSlug+".md")
	if data, err := os.ReadFile(cand); err == nil {
		if p, perr := ParsePage(data); perr == nil {
			return cand, p
		}
	}
	return "", nil
}

// scopedMatch is one existing page found within the declared scope for a topic.
type scopedMatch struct {
	Path string
	Page *Page
}

// findScopedPages collects EVERY existing page for this topic within the scope
// declared by the input projects. It considers EACH named <project>/ directory,
// and _shared/ ONLY when the existing shared page's Project facet actually
// overlaps the requested projects. Without that overlap check, adding topic
// "pay" for project "cli" would wrongly merge an existing _shared/pay.md owned
// only by "front"+"back" (SPEC §9 per-project isolation / shared-page scope): a
// shared page belongs to the repos it lists, not to every project. When there is
// no overlap, _shared/ is skipped so the requested project gets (or merges into)
// its OWN independent <project>/ page instead. Returning all in-scope matches
// also lets an explicit multi-project add merge AND supersede every per-project
// page that already holds the topic — e.g. both front/pay.md and back/pay.md.
// Matches are returned _shared first (when in scope), then in the projects order,
// deduplicated by path.
func findScopedPages(home string, projects []string, topicSlug string) []scopedMatch {
	var matches []scopedMatch
	seen := map[string]bool{}
	add := func(group string) {
		if path, p := pageAt(home, group, topicSlug); p != nil && !seen[path] {
			seen[path] = true
			matches = append(matches, scopedMatch{Path: path, Page: p})
		}
	}
	// Include the shared page only when its facet overlaps the requested scope.
	if sharedPath, shared := pageAt(home, "_shared", topicSlug); shared != nil {
		want := map[string]bool{}
		for _, proj := range projects {
			want[proj] = true
		}
		overlaps := false
		for _, owner := range shared.Project {
			if want[owner] {
				overlaps = true
				break
			}
		}
		if overlaps && !seen[sharedPath] {
			seen[sharedPath] = true
			matches = append(matches, scopedMatch{Path: sharedPath, Page: shared})
		}
	}
	for _, proj := range projects {
		add(proj)
	}
	return matches
}

// ValidCategory reports whether c is empty (category is optional) or one of the
// controlled vocabulary values architecture|decision|gotcha|pattern.
func ValidCategory(c string) bool {
	switch c {
	case "", "architecture", "decision", "gotcha", "pattern":
		return true
	default:
		return false
	}
}

func Add(home string, in PageInput) (string, error) {
	if len(in.Projects) == 0 {
		return "", fmt.Errorf("at least one project is required")
	}
	if strings.TrimSpace(in.SourceTask) == "" {
		return "", fmt.Errorf("a non-empty source task is required for provenance")
	}
	if !ValidCategory(in.Category) {
		return "", fmt.Errorf("category must be one of architecture|decision|gotcha|pattern, got %q", in.Category)
	}
	// Each project name becomes a knowledge group directory, so reject names that
	// could escape _knowledge/ (e.g. "../front") or collide with the reserved
	// _shared/_knowledge dirs (underscore-prefixed names).
	for _, proj := range in.Projects {
		if err := store.ValidName(proj); err != nil {
			return "", fmt.Errorf("invalid project %q: %w", proj, err)
		}
	}
	topicSlug := Slug(in.Topic)
	matches := findScopedPages(home, in.Projects, topicSlug)

	var page *Page
	if len(matches) > 0 {
		// Merge EVERY in-scope page (e.g. front/pay.md AND back/pay.md) into one,
		// unioning facets/tags/sources and concatenating bodies, so an explicit
		// multi-project promotion never leaves a stale per-project duplicate.
		page = matches[0].Page
		for _, m := range matches[1:] {
			page.Project = union(page.Project, m.Page.Project)
			page.Tags = union(page.Tags, m.Page.Tags)
			page.Sources = append(page.Sources, m.Page.Sources...)
			if page.Category == "" && m.Page.Category != "" {
				page.Category = m.Page.Category
			}
			if body := strings.TrimSpace(m.Page.Body); body != "" {
				page.Body = strings.TrimSpace(page.Body) + "\n\n" + body
			}
			if m.Page.LastReviewed > page.LastReviewed {
				page.LastReviewed = m.Page.LastReviewed
			}
		}
		page.Project = union(page.Project, in.Projects)
		page.Tags = union(page.Tags, in.Tags)
		if in.Category != "" {
			page.Category = in.Category
		}
		page.Body = strings.TrimSpace(page.Body) + "\n\n## Update (" + in.When + ")\n" + strings.TrimSpace(in.Body)
		page.Sources = append(page.Sources, Source{Task: in.SourceTask, When: in.When})
		page.LastReviewed = in.When
	} else {
		page = &Page{
			Project: union(in.Projects, nil), Category: in.Category, Topic: in.Topic,
			Status: "active", Tags: union(in.Tags, nil),
			Sources: []Source{{Task: in.SourceTask, When: in.When}}, LastReviewed: in.When,
			Body: in.Body,
		}
	}

	// Promotion to _shared happens only when the resulting facet spans ≥2 repos.
	newPath := filepath.Join(targetDir(home, page.Project), topicSlug+".md")
	// Canonicalize the write target against CTX_HOME itself so a symlinked group
	// dir — or a symlinked _knowledge dir — redirecting outside CTX_HOME cannot
	// redirect the write or the supersede-deletions below to a real location off
	// the store. Anchoring to KnowledgeDir would resolve a symlinked _knowledge to
	// its real (possibly off-store) location and validate the target against that
	// wrong base, so we anchor to home and let ValidRelPath resolve both ends.
	homeRel, rerr := filepath.Rel(home, newPath)
	if rerr != nil {
		return "", rerr
	}
	if _, verr := store.ValidRelPath(home, homeRel); verr != nil {
		return "", verr
	}
	// Write the (possibly promoted) page FIRST so a failure never loses any source
	// page; only after a successful write do we remove EVERY superseded per-project
	// page so no duplicate of the topic survives.
	if err := store.AtomicWrite(newPath, Render(page)); err != nil {
		return "", err
	}
	for _, m := range matches {
		if m.Path == newPath {
			continue
		}
		// Validate EVERY deletion target against CTX_HOME immediately before the
		// os.Remove: a symlinked per-project group dir could otherwise make this
		// delete a file outside the store. Anchor to home (not KnowledgeDir) for the
		// same reason as newPath above, and let ValidRelPath resolve symlinks on both
		// ends so a redirected source page is rejected rather than removed off-store.
		mRel, mErr := filepath.Rel(home, m.Path)
		if mErr != nil {
			return "", mErr
		}
		if _, verr := store.ValidRelPath(home, mRel); verr != nil {
			return "", verr
		}
		if err := os.Remove(m.Path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	if err := updateIndex(home); err != nil {
		return "", err
	}
	if err := appendLog(home, "## ["+in.When+"] consolidate | "+page.Topic); err != nil {
		return "", err
	}
	return newPath, nil
}

// HasProvenance reports whether a knowledge page consolidates the given task's
// findings for one of its registered projects. A page qualifies only when it both
// (1) records taskName as a sources: entry AND (2) has a project facet that
// overlaps taskProjects. Requiring the overlap implements SPEC §7/§9: `ctx done`
// must verify the task's *relevant per-project* findings were consolidated, so a
// page written for an unrelated project (e.g. provenance for "back" when the task
// only registered "front") never satisfies the gate. When taskProjects is empty
// the task registered no projects, so there are no per-project findings to require
// and any same-task page qualifies.
func HasProvenance(home, taskName string, taskProjects []string) (bool, error) {
	if strings.TrimSpace(taskName) == "" {
		return false, fmt.Errorf("task name is required")
	}
	want := map[string]bool{}
	for _, p := range taskProjects {
		if p != "" {
			want[p] = true
		}
	}
	for _, ref := range listPages(home) {
		sourced := false
		for _, s := range ref.Page.Sources {
			if s.Task == taskName {
				sourced = true
				break
			}
		}
		if !sourced {
			continue
		}
		if len(want) == 0 {
			return true, nil
		}
		for _, proj := range ref.Page.Project {
			if want[proj] {
				return true, nil
			}
		}
	}
	return false, nil
}

type pageRef struct {
	Group string
	Path  string
	Page  *Page
}

func listPages(home string) []pageRef {
	base := store.KnowledgeDir(home)
	groups, _ := os.ReadDir(base)
	var refs []pageRef
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(base, g.Name()))
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			p := filepath.Join(base, g.Name(), f.Name())
			if data, err := os.ReadFile(p); err == nil {
				if pg, perr := ParsePage(data); perr == nil {
					refs = append(refs, pageRef{Group: g.Name(), Path: p, Page: pg})
				}
			}
		}
	}
	return refs
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(l, "#"))
		if l != "" {
			// Truncate by RUNES, not bytes: slicing a UTF-8 string at byte 80 can
			// cut inside a multibyte rune (Korean/Japanese/Chinese), producing
			// invalid UTF-8 in the index and search snippets.
			r := []rune(l)
			if len(r) > 80 {
				return string(r[:80])
			}
			return l
		}
	}
	return ""
}

func updateIndex(home string) error {
	refs := listPages(home)
	byGroup := map[string][]pageRef{}
	var groups []string
	for _, r := range refs {
		if _, ok := byGroup[r.Group]; !ok {
			groups = append(groups, r.Group)
		}
		byGroup[r.Group] = append(byGroup[r.Group], r)
	}
	sort.Strings(groups)
	var b strings.Builder
	fmt.Fprintf(&b, "# Knowledge Index\n\n> %d page(s)\n", len(refs))
	for _, g := range groups {
		fmt.Fprintf(&b, "\n## %s\n", g)
		rs := byGroup[g]
		sort.Slice(rs, func(i, j int) bool { return rs[i].Page.Topic < rs[j].Page.Topic })
		for _, r := range rs {
			rel := filepath.Join(g, filepath.Base(r.Path))
			fmt.Fprintf(&b, "- [%s](%s) — %s\n", r.Page.Topic, rel, firstLine(r.Page.Body))
		}
	}
	return store.AtomicWrite(filepath.Join(store.KnowledgeDir(home), "index.md"), []byte(b.String()))
}

func appendLog(home, line string) error {
	p := filepath.Join(store.KnowledgeDir(home), "log.md")
	// Validate log.md against CTX_HOME before touching it: an existing log.md symlink
	// could otherwise redirect the append (or the create below) to a file outside the
	// store. ValidRelPath resolves symlinks on both ends, so a redirected log is
	// rejected here rather than written off-store.
	if _, verr := store.ValidRelPath(home, filepath.Join("_knowledge", "log.md")); verr != nil {
		return verr
	}
	// Read-modify-write via AtomicWrite (temp file + rename) instead of O_APPEND on the
	// path: the rename replaces log.md atomically without ever following it as a symlink,
	// so the append cannot land outside CTX_HOME even if log.md is later re-symlinked.
	prior, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		prior = []byte("# Knowledge Log\n\n")
	}
	return store.AtomicWrite(p, append(prior, []byte(line+"\n")...))
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/knowledge/`
Expected: PASS (all knowledge tests, including the category-vocabulary checks).

- [ ] **Step 5: Commit**

```bash
git add internal/knowledge/knowledge.go internal/knowledge/knowledge_test.go
git commit -m "feat: knowledge topic-page store with merge, _shared promotion, index/log"
```

---

## Task 13: knowledge search (CJK tokenizer) + `ctx know` commands

**Files:**
- Create: `internal/knowledge/search.go`, `internal/cli/know.go`
- Test: `internal/knowledge/search_test.go`, `internal/cli/know_test.go`

**Interfaces:**
- Produces:
  - `knowledge.Query{Text, Project, Tag, Category string}`
  - `knowledge.Result{Path, Topic string; Score int; Snippet string; Project []string}`
  - `knowledge.Search(home string, q Query) ([]Result, error)` — facet filters are AND; ranking: tag +3/term, topic +2/term, body +2/occurrence; empty `Text` lists all matches (score 1). Sorted by score desc, then Topic.
  - `cli.runKnowAdd(home string, in knowledge.PageInput) (string, error)`, `cli.runKnowSearch(home string, q knowledge.Query) ([]knowledge.Result, error)`, `cli.runKnowIndex(home string) (string, error)`.

- [ ] **Step 1: Write the failing test for search**

Create `internal/knowledge/search_test.go`:

```go
package knowledge

import "testing"

func TestTokenizeKoreanProducesBigrams(t *testing.T) {
	toks := tokenize("결제처리")
	want := map[string]bool{"결": true, "제": true, "처": true, "리": true, "결제": true, "제처": true, "처리": true}
	for w := range want {
		if !hasTok(toks, w) {
			t.Errorf("missing token %q in %v", w, toks)
		}
	}
}

func TestSearchKoreanBigramMatch(t *testing.T) {
	home := t.TempDir()
	if _, err := Add(home, PageInput{Projects: []string{"back"}, Topic: "pay endpoint", Body: "결제 처리 시 200 먼저 반환", SourceTask: "t", When: "2026-06-18"}); err != nil {
		t.Fatal(err)
	}
	res, err := Search(home, Query{Text: "결제"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("expected a match for 결제")
	}
}

func TestSearchProjectFilter(t *testing.T) {
	home := t.TempDir()
	_, _ = Add(home, PageInput{Projects: []string{"front"}, Topic: "routing", Body: "uses next router", SourceTask: "t", When: "2026-06-18"})
	_, _ = Add(home, PageInput{Projects: []string{"back"}, Topic: "db", Body: "uses postgres", SourceTask: "t", When: "2026-06-18"})
	res, _ := Search(home, Query{Text: "uses", Project: "front"})
	if len(res) != 1 || res[0].Topic != "routing" {
		t.Errorf("project filter failed: %+v", res)
	}
}

func hasTok(toks []string, w string) bool {
	for _, t := range toks {
		if t == w {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/knowledge/ -run 'TestTokenize|TestSearch'`
Expected: FAIL — undefined `tokenize`, `Search`, `Query`.

- [ ] **Step 3: Implement `internal/knowledge/search.go`**

```go
package knowledge

import (
	"sort"
	"strings"
	"unicode"
)

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
}

// tokenize lowercases, splits latin/numeric on non-alnum, and emits CJK
// unigrams + adjacent bigrams (so "결제" matches inside "결제처리").
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var toks []string
	var latin, cjk []rune
	flushLatin := func() {
		if len(latin) > 0 {
			toks = append(toks, string(latin))
			latin = nil
		}
	}
	flushCJK := func() {
		for _, r := range cjk {
			toks = append(toks, string(r))
		}
		for i := 0; i+1 < len(cjk); i++ {
			toks = append(toks, string(cjk[i:i+2]))
		}
		cjk = nil
	}
	for _, r := range s {
		switch {
		case isCJK(r):
			flushLatin()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			latin = append(latin, r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return toks
}

type Query struct {
	Text, Project, Tag, Category string
}
type Result struct {
	Path    string   `json:"path"`
	Topic   string   `json:"topic"`
	Score   int      `json:"score"`
	Snippet string   `json:"snippet"`
	Project []string `json:"project"`
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func countTok(toks []string, term string) int {
	n := 0
	for _, t := range toks {
		if t == term {
			n++
		}
	}
	return n
}

func Search(home string, q Query) ([]Result, error) {
	qtokens := tokenize(q.Text)
	var results []Result
	for _, ref := range listPages(home) {
		p := ref.Page
		if q.Project != "" && !contains(p.Project, q.Project) {
			continue
		}
		if q.Category != "" && p.Category != q.Category {
			continue
		}
		if q.Tag != "" && !contains(p.Tags, q.Tag) {
			continue
		}
		score := 0
		if q.Text == "" {
			score = 1
		} else {
			titleToks := tokenize(p.Topic)
			bodyToks := tokenize(p.Body)
			for _, qt := range qtokens {
				if contains(p.Tags, qt) {
					score += 3
				}
				score += 2 * countTok(titleToks, qt)
				score += 2 * countTok(bodyToks, qt)
			}
		}
		if score > 0 {
			results = append(results, Result{
				Path: ref.Path, Topic: p.Topic, Score: score,
				Snippet: firstLine(p.Body), Project: p.Project,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Topic < results[j].Topic
	})
	return results, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/knowledge/`
Expected: PASS (all knowledge tests).

- [ ] **Step 5: Write the failing test for `ctx know` wiring**

Create `internal/cli/know_test.go`:

```go
package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/knowledge"
)

func TestRunKnowAddThenSearch(t *testing.T) {
	home := t.TempDir()
	if _, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "uses next router",
		SourceTask: "t", When: "2026-06-18",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := runKnowSearch(home, knowledge.Query{Text: "router"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Topic != "routing" {
		t.Errorf("search failed: %+v", res)
	}
	idx, err := runKnowIndex(home)
	if err != nil || !contains(idx, "routing") {
		t.Errorf("index missing page: %v %q", err, idx)
	}
}

// TestRunKnowAddRendersSourceTask asserts the originating task is written into
// the page's sources: provenance, not left empty.
func TestRunKnowAddRendersSourceTask(t *testing.T) {
	home := t.TempDir()
	path, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "uses next router",
		SourceTask: "add-payment", When: "2026-06-18",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "task: add-payment") {
		t.Errorf("rendered page missing source task provenance:\n%s", data)
	}
}

// TestRunKnowAddRequiresSourceTask asserts consolidation refuses empty provenance.
func TestRunKnowAddRequiresSourceTask(t *testing.T) {
	home := t.TempDir()
	if _, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "x", When: "2026-06-18",
	}); err == nil {
		t.Error("expected USAGE error when source task is empty")
	}
}

// TestRunKnowAddRejectsInvalidCategory asserts the controlled vocabulary is
// enforced at the CLI boundary (USAGE error), and a valid category is accepted.
func TestRunKnowAddRejectsInvalidCategory(t *testing.T) {
	home := t.TempDir()
	if _, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "x",
		SourceTask: "t", When: "2026-06-18", Category: "bogus",
	}); err == nil {
		t.Error("expected USAGE error for an invalid --category")
	}
	if _, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "x",
		SourceTask: "t", When: "2026-06-18", Category: "gotcha",
	}); err != nil {
		t.Errorf("valid category should be accepted: %v", err)
	}
}

// TestKnowSearchJSONGolden locks the `know search` result contract. The page
// path is made stable by stripping the temp-home prefix before comparison.
func TestKnowSearchJSONGolden(t *testing.T) {
	home := t.TempDir()
	if _, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "uses next router",
		SourceTask: "add-payment", When: "2026-06-18",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := runKnowSearch(home, knowledge.Query{Text: "router"})
	if err != nil {
		t.Fatal(err)
	}
	// Normalize the absolute path so the golden is reproducible across machines.
	for i := range res {
		res[i].Path = strings.TrimPrefix(res[i].Path, home)
	}
	assertGolden(t, "know_search.json", map[string]any{"results": res})
}
```

Generate the golden once:

Run: `UPDATE_GOLDEN=1 go test ./internal/cli/ -run TestKnowSearchJSONGolden`
Then verify `internal/cli/testdata/know_search.json` exists (contains a
`results` array with `path`, `topic`, `score`, `snippet`, `project`).

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestRunKnow`
Expected: FAIL — undefined run functions.

- [ ] **Step 7: Implement `internal/cli/know.go`**

```go
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kimhyoyeon/context-manager/internal/knowledge"
	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/spf13/cobra"
)

func runKnowAdd(home string, in knowledge.PageInput) (string, error) {
	if len(in.Projects) == 0 || in.Topic == "" {
		return "", output.Errorf(jsonOut, output.ErrUsage, "--project and --topic are required")
	}
	if in.SourceTask == "" {
		return "", output.Errorf(jsonOut, output.ErrUsage, "a source task is required for provenance (pass --source-task or run inside a task)")
	}
	if !knowledge.ValidCategory(in.Category) {
		return "", output.Errorf(jsonOut, output.ErrUsage, "--category must be one of architecture|decision|gotcha|pattern")
	}
	if in.When == "" {
		in.When = time.Now().UTC().Format("2006-01-02")
	}
	return knowledge.Add(home, in)
}

func runKnowSearch(home string, q knowledge.Query) ([]knowledge.Result, error) {
	return knowledge.Search(home, q)
}

func runKnowIndex(home string) (string, error) {
	data, err := os.ReadFile(store.KnowledgeDir(home) + "/index.md")
	if err != nil {
		if os.IsNotExist(err) {
			return "# Knowledge Index\n\n> 0 page(s)\n", nil
		}
		return "", err
	}
	return string(data), nil
}

func init() {
	knowCmd := &cobra.Command{Use: "know", Short: "Per-project persistent knowledge"}

	var projects, tags []string
	var topic, category, from, sourceTask string
	addCmd := &cobra.Command{
		Use: "add", Short: "Create or merge a topic page",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			// Provenance: explicit --source-task wins, else resolve the active
			// task from CWD so consolidation always records a non-empty source.
			src := sourceTask
			if src == "" {
				if cwd, cerr := os.Getwd(); cerr == nil {
					if loc, lerr := store.Locate(home, cwd); lerr == nil {
						src = loc.Task
					}
				}
			}
			body := ""
			if from != "" {
				b, rerr := os.ReadFile(from)
				if rerr != nil {
					return rerr
				}
				body = string(b)
			} else {
				// Stat the stdin handle defensively: if stdin is closed or Stat
				// fails, fi is nil and fi.Mode() would panic, breaking the
				// structured-error contract. Surface the error for centralized
				// rendering instead.
				fi, serr := os.Stdin.Stat()
				if serr != nil {
					return output.Errorf(jsonOut, output.ErrUsage, "cannot inspect stdin: %v", serr)
				}
				if fi == nil {
					return output.Errorf(jsonOut, output.ErrUsage, "cannot inspect stdin: no file info")
				}
				if (fi.Mode() & os.ModeCharDevice) == 0 {
					b, rerr := io.ReadAll(os.Stdin)
					if rerr != nil {
						return output.Errorf(jsonOut, output.ErrUsage, "cannot read stdin: %v", rerr)
					}
					body = string(b)
				}
			}
			path, err := runKnowAdd(home, knowledge.PageInput{
				Projects: projects, Topic: topic, Category: category, Tags: tags,
				SourceTask: src, Body: strings.TrimSpace(body),
			})
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, "wrote "+path, map[string]string{"path": path})
		},
	}
	addCmd.Flags().StringSliceVar(&projects, "project", nil, "project(s) this knowledge belongs to (≥2 → _shared)")
	addCmd.Flags().StringVar(&topic, "topic", "", "topic name (the compounding key)")
	addCmd.Flags().StringVar(&category, "category", "", "architecture|decision|gotcha|pattern")
	addCmd.Flags().StringSliceVar(&tags, "tags", nil, "comma-separated tags")
	addCmd.Flags().StringVar(&from, "from", "", "read body from file (else stdin)")
	addCmd.Flags().StringVar(&sourceTask, "source-task", "", "originating task for provenance (default: detect from CWD)")

	var sProject, sTag, sCategory string
	searchCmd := &cobra.Command{
		Use: "search <query>", Short: "Search topic pages (keyword + facets)", Args: cobra.MinimumNArgs(0),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			res, err := runKnowSearch(home, knowledge.Query{
				Text: strings.Join(args, " "), Project: sProject, Tag: sTag, Category: sCategory,
			})
			if err != nil {
				return err
			}
			human := fmt.Sprintf("%d result(s)", len(res))
			for _, r := range res {
				human += fmt.Sprintf("\n  [%d] %s (%s) — %s", r.Score, r.Topic, strings.Join(r.Project, ","), r.Snippet)
			}
			return output.Emit(jsonOut, human, map[string]any{"results": res})
		},
	}
	searchCmd.Flags().StringVar(&sProject, "project", "", "filter by project")
	searchCmd.Flags().StringVar(&sTag, "tag", "", "filter by tag")
	searchCmd.Flags().StringVar(&sCategory, "category", "", "filter by category")

	indexCmd := &cobra.Command{
		Use: "index", Short: "Print the knowledge catalog (read this first)",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			idx, err := runKnowIndex(home)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, idx, map[string]string{"index": idx})
		},
	}

	knowCmd.AddCommand(addCmd, searchCmd, indexCmd)
	RootCmd.AddCommand(knowCmd)
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./internal/cli/ -run TestRunKnow ./internal/knowledge/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/knowledge/search.go internal/knowledge/search_test.go internal/cli/know.go internal/cli/know_test.go internal/cli/testdata/
git commit -m "feat: knowledge search (CJK bigram) + ctx know add/search/index (+ search golden)"
```

---

## Task 14: `ctx done` (verify → remove worktrees → remove task) + `ctx where`

**Files:**
- Create: `internal/cli/done.go`, `internal/cli/where.go`
- Test: `internal/cli/done_test.go`

**Interfaces:**
- Consumes: `worktree.Status` (live HEAD/branch check), `worktree.BranchStatus` (registered-branch merge check), `worktree.Remove`, `knowledge.HasProvenance`.
- Produces: `cli.runDone(home, taskName string, force bool) (doneResult, error)` where `doneResult{Task string; RemovedWorktrees []string}`. `done` is a finalization with ordered, fail-closed phases:
  1. **Consolidation prerequisite** — refuses (`STATE`) unless `knowledge.HasProvenance(home, taskName, taskProjectNames)` is true (i.e. a knowledge page records this task as a `sources:` provenance entry **and** that page's project facet overlaps one of the task's registered projects), so the task's *relevant per-project* findings are not lost and a page written for an unrelated project never satisfies the gate. **`force` does NOT override this** — per SPEC §7/§9 consolidation provenance is always required; SPEC §11 limits `force` to the dirty/unmerged worktree safety overrides in phase 2.
  2. **Verification** — refuses (`STATE`) when any worktree is **dirty**, has **unmerged commits on its registered branch** (`worktree.BranchStatus(refs/heads/p.Branch, p.Base).Ahead > 0`), is **on a different branch than the registered `p.Branch`** (a switched/detached worktree must not mask unmerged work), **or its status cannot be read** (status/branch-read errors are blockers, not skipped), each reported with project context. Merge status is computed against the registered branch ref, not the worktree's current HEAD. `force` overrides.
  3. **Worktree removal** — removes each worktree; a removal failure is a blocker (returns `GIT` with project context) and aborts before deleting the task folder, so registered git worktrees are never orphaned. Under `force`, removal still runs and a failure is still reported.
  4. **Task folder deletion** — deletes the task directory **only after every worktree was removed successfully**.
  `cli.runWhere(home, cwd string) (whereOut, error)` where `whereOut{Home, TaskDir string}`.

- [ ] **Step 1: Write the failing integration test**

Create `internal/cli/done_test.go`:

```go
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/knowledge"
	"github.com/kimhyoyeon/context-manager/internal/task"
)

// seedProvenance records a knowledge page sourced from taskName whose project
// facet covers projects, so the done consolidation prerequisite (phase 1) is
// satisfied without --force. Callers pass every project whose findings the test
// needs treated as consolidated; with no projects given it defaults to "front".
// A multi-project task that drops projects across a resumed done (e.g. front is
// removed before a retry) must seed ALL of them, so HasProvenance still overlaps
// the projects that remain in task.yaml on the retry.
func seedProvenance(t *testing.T, home, taskName string, projects ...string) {
	t.Helper()
	if len(projects) == 0 {
		projects = []string{"front"}
	}
	if _, err := knowledge.Add(home, knowledge.PageInput{
		Projects: projects, Topic: "wrap-up", Body: "consolidated",
		SourceTask: taskName, When: "2026-06-18",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunDoneRemovesWorktreesAndTask(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo}); err != nil {
		t.Fatal(err)
	}
	seedProvenance(t, home, "tk") // satisfy consolidation prerequisite
	// clean + not ahead + provenance recorded → done succeeds
	res, err := runDone(home, "tk", false)
	if err != nil {
		t.Fatalf("done failed: %v", err)
	}
	if len(res.RemovedWorktrees) != 1 {
		t.Errorf("expected 1 removed worktree, got %v", res.RemovedWorktrees)
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); !os.IsNotExist(err) {
		t.Error("task folder should be removed")
	}
}

// TestRunDoneRefusesWithoutConsolidation proves phase 1 blocks deletion when no
// knowledge provenance exists for the task (and that the task survives).
func TestRunDoneRefusesWithoutConsolidation(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	_, _ = runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"})
	_, _ = runAdd(home, "tk", addOpts{Project: "front", Repo: repo})
	if _, err := runDone(home, "tk", false); err == nil {
		t.Error("expected refusal when no knowledge was consolidated")
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); err != nil {
		t.Error("task folder must survive a refused done")
	}
}

// TestRunDoneForceStillRequiresConsolidation proves phase 1 is NOT bypassable by
// --force: with no knowledge provenance recorded, even `ctx done --force` refuses
// and the task folder survives. --force only overrides the dirty/unmerged worktree
// safety check in phase 2 (SPEC §11), never the consolidation prerequisite
// (SPEC §7/§9).
func TestRunDoneForceStillRequiresConsolidation(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	_, _ = runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"})
	_, _ = runAdd(home, "tk", addOpts{Project: "front", Repo: repo})
	// No seedProvenance: there is no consolidated knowledge for this task.
	if _, err := runDone(home, "tk", true); err == nil {
		t.Error("expected --force to STILL refuse a task with no consolidation provenance")
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); err != nil {
		t.Error("task folder must survive a refused force-done")
	}
}

func TestRunDoneRefusesUnmergedUnlessForce(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	_, _ = runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"})
	_, _ = runAdd(home, "tk", addOpts{Project: "front", Repo: repo})
	seedProvenance(t, home, "tk") // isolate the unmerged check from phase 1
	wt := filepath.Join(home, "tk", "front", "wt")
	// create an unmerged commit in the worktree
	_ = os.WriteFile(filepath.Join(wt, "f.txt"), []byte("z"), 0o644)
	for _, a := range [][]string{{"add", "."}, {"commit", "-m", "wip"}} {
		c := exec.Command("git", a...)
		c.Dir = wt
		_, _ = c.CombinedOutput()
	}
	if _, err := runDone(home, "tk", false); err == nil {
		t.Error("expected refusal for unmerged commit")
	}
	if _, err := runDone(home, "tk", true); err != nil {
		t.Errorf("--force should override: %v", err)
	}
}

// TestRunDoneVerifiesRegisteredBranchNotHead proves merge status is checked
// against the REGISTERED branch (refs/heads/p.Branch), not the worktree's current
// HEAD. With an unmerged commit on the registered branch, switching the worktree
// to a clean, merged branch (HEAD now reports Ahead == 0) must NOT let done
// delete the task: the registered branch is still unmerged (SPEC §7/§11).
func TestRunDoneVerifiesRegisteredBranchNotHead(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	_, _ = runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"})
	_, _ = runAdd(home, "tk", addOpts{Project: "front", Repo: repo})
	seedProvenance(t, home, "tk") // isolate the branch check from phase 1
	wt := filepath.Join(home, "tk", "front", "wt")
	gitIn := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = wt
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Put an unmerged commit on the registered branch (feat/tk).
	_ = os.WriteFile(filepath.Join(wt, "f.txt"), []byte("z"), 0o644)
	gitIn("add", ".")
	gitIn("commit", "-m", "wip on registered branch")
	// Now switch the worktree HEAD onto the merged base branch so HEAD looks clean.
	// (Find the base branch the worktree was created from.)
	base := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	base.Dir = repo
	baseName, berr := base.CombinedOutput()
	if berr != nil {
		t.Fatalf("rev-parse base: %v\n%s", berr, baseName)
	}
	gitIn("checkout", "-b", "side", strings.TrimSpace(string(baseName)))
	// HEAD is now Ahead == 0, but feat/tk is still unmerged → done must refuse.
	if _, err := runDone(home, "tk", false); err == nil {
		t.Error("expected refusal: registered branch feat/tk is unmerged even though HEAD looks clean")
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); err != nil {
		t.Error("task folder must survive a refused done")
	}
}

// TestRunDoneResumesAfterPartialWorktreeRemoval proves phase 3 is resumable: with
// two projects, when the SECOND worktree removal fails, the first removal is
// already persisted to task.yaml, the task folder survives, and a retry (after the
// blocker is cleared) finishes by removing only the still-present worktree. The
// second removal is forced to fail deterministically by locking its worktree
// (`git worktree remove` refuses a locked worktree); unlocking it makes the retry
// succeed. Project iteration follows task.yaml registration order: "front" first,
// "back" second.
func TestRunDoneResumesAfterPartialWorktreeRemoval(t *testing.T) {
	home := t.TempDir()
	repo := gitRepoOrSkip(t)
	if _, err := runNew(home, newOpts{Name: "tk", Objective: "o", DoneWhen: "d"}); err != nil {
		t.Fatal(err)
	}
	// Distinct branches per project: two worktrees of one repo cannot share a
	// branch, so the default feat/tk would collide on the second add.
	if _, err := runAdd(home, "tk", addOpts{Project: "front", Repo: repo, Branch: "feat/tk-front"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdd(home, "tk", addOpts{Project: "back", Repo: repo, Branch: "feat/tk-back"}); err != nil {
		t.Fatal(err)
	}
	// Seed provenance for BOTH projects: the first done drops "front" from task.yaml,
	// so on retry HasProvenance is recomputed against the remaining ["back"]. A page
	// whose facet covers both keeps the prerequisite satisfied across the resume.
	seedProvenance(t, home, "tk", "front", "back") // satisfy phase 1 (survives the retry)

	backWt := filepath.Join(home, "tk", "back", "wt")
	frontWt := filepath.Join(home, "tk", "front", "wt")
	gitRepo := func(args ...string) ([]byte, error) {
		c := exec.Command("git", args...)
		c.Dir = repo
		return c.CombinedOutput()
	}
	// Lock the second worktree so its removal fails on the first attempt.
	if out, err := gitRepo("worktree", "lock", backWt); err != nil {
		t.Fatalf("git worktree lock: %v\n%s", err, out)
	}

	// First attempt: front is removed and persisted, back fails → error returned,
	// task folder must survive for a retry.
	if _, err := runDone(home, "tk", false); err == nil {
		t.Fatal("expected first done to fail on the locked second worktree")
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); err != nil {
		t.Fatal("task folder must survive a partial removal so it can be retried")
	}
	// task.yaml must now list ONLY the still-present "back" project; "front" was
	// removed and the progress persisted.
	tk, err := task.Load(filepath.Join(home, "tk"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tk.Projects) != 1 || tk.Projects[0].Name != "back" {
		t.Fatalf("expected only 'back' to remain after partial removal, got %+v", tk.Projects)
	}
	if tk.Project("front") != nil {
		t.Error("front must be dropped from task.yaml after its worktree was removed")
	}
	if _, err := os.Stat(frontWt); !os.IsNotExist(err) {
		t.Error("front worktree directory should be gone after the first attempt")
	}

	// Clear the blocker and retry: the resume must remove only "back" and then
	// delete the task folder.
	if out, err := gitRepo("worktree", "unlock", backWt); err != nil {
		t.Fatalf("git worktree unlock: %v\n%s", err, out)
	}
	res, err := runDone(home, "tk", false)
	if err != nil {
		t.Fatalf("retry done failed: %v", err)
	}
	if len(res.RemovedWorktrees) != 1 || res.RemovedWorktrees[0] != "back" {
		t.Errorf("retry should remove only the remaining 'back' worktree, got %v", res.RemovedWorktrees)
	}
	if _, err := os.Stat(filepath.Join(home, "tk")); !os.IsNotExist(err) {
		t.Error("task folder should be removed after the retry finishes")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestRunDone`
Expected: FAIL — undefined `runDone`.

- [ ] **Step 3: Implement `internal/cli/done.go`**

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kimhyoyeon/context-manager/internal/knowledge"
	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/kimhyoyeon/context-manager/internal/worktree"
	"github.com/spf13/cobra"
)

type doneResult struct {
	Task             string   `json:"task"`
	RemovedWorktrees []string `json:"removed_worktrees"`
}

// runDone finalizes a task as ordered, fail-closed phases:
//  1. consolidation prerequisite (knowledge provenance recorded),
//  2. verification (clean + merged + readable worktree status),
//  3. worktree removal (every worktree must be removed), then
//  4. task-folder deletion (only after all worktrees are gone).
// `force` overrides ONLY phase 2 (the dirty/unmerged worktree safety check and
// the status-read check), per SPEC §11. It does NOT bypass phase 1: consolidation
// provenance is ALWAYS required (SPEC §7/§9) so durable findings are never lost.
// `force` also still requires that worktree removal itself succeed before the task
// folder is deleted, so a registered worktree is never orphaned with its metadata
// destroyed.
func runDone(home, taskName string, force bool) (doneResult, error) {
	dir, tk, err := loadTask(home, taskName)
	if err != nil {
		return doneResult{}, err
	}

	// Phase 1: consolidation prerequisite. This is enforced even under --force:
	// SPEC §7/§9 require that a task's findings be consolidated (provenance
	// recorded) before the task is deleted, and SPEC §11 limits --force to the
	// dirty/unmerged worktree safety overrides in phase 2. Allowing --force to
	// skip this would let `ctx done --force` destroy a task with no provenance.
	// The page must consolidate one of THIS task's registered projects, so a page
	// written for an unrelated project never satisfies the gate (SPEC §7/§9).
	taskProjects := make([]string, 0, len(tk.Projects))
	for _, p := range tk.Projects {
		taskProjects = append(taskProjects, p.Name)
	}
	ok, perr := knowledge.HasProvenance(home, taskName, taskProjects)
	if perr != nil {
		return doneResult{}, output.Errorf(jsonOut, output.ErrState,
			"could not verify knowledge consolidation for %q: %v", taskName, perr)
	}
	if !ok {
		return doneResult{}, output.Errorf(jsonOut, output.ErrState,
			"refusing to finish %q: consolidate this task's project findings first with 'ctx know add' (no matching provenance recorded); --force does not override this", taskName)
	}

	// Phase 2: verification. Status-read failures are blockers, not skipped.
	// Merge status is measured for the REGISTERED branch (refs/heads/p.Branch)
	// against p.Base, NOT the worktree's current HEAD: a worktree that was switched
	// or detached must not let unmerged commits on p.Branch slip through as
	// Ahead == 0 (SPEC §7/§11). We additionally fail closed if the worktree's live
	// branch no longer matches the registered p.Branch, since dirty-tree state then
	// refers to a different branch than the one whose merge status we verified.
	var blockers []string
	for _, p := range tk.Projects {
		wtPath := filepath.Join(dir, p.Worktree)
		if p.Branch == "" {
			blockers = append(blockers, p.Name+": no registered branch to verify")
			continue
		}
		head, herr := worktree.Status(wtPath, "")
		if herr != nil {
			blockers = append(blockers, fmt.Sprintf("%s: cannot read worktree status: %v", p.Name, herr))
			continue
		}
		if head.Branch != p.Branch {
			blockers = append(blockers, fmt.Sprintf("%s: worktree is on %q, not the registered branch %q", p.Name, head.Branch, p.Branch))
		}
		bs, berr := worktree.BranchStatus(wtPath, p.Branch, p.Base)
		if berr != nil {
			blockers = append(blockers, fmt.Sprintf("%s: cannot read branch status: %v", p.Name, berr))
			continue
		}
		if bs.Dirty {
			blockers = append(blockers, p.Name+": uncommitted changes")
		}
		if bs.Ahead > 0 {
			blockers = append(blockers, fmt.Sprintf("%s: %d unmerged commit(s) on %s", p.Name, bs.Ahead, p.Branch))
		}
	}
	if len(blockers) > 0 && !force {
		return doneResult{}, output.Errorf(jsonOut, output.ErrState,
			"refusing to finish %q: %s (use --force)", taskName, strings.Join(blockers, "; "))
	}

	// Phase 3: remove every worktree, resumably. A removal failure aborts before
	// deleting the task folder so the worktree (and its recovery metadata)
	// survives. To make a retry reliable, every successful (or already-absent)
	// removal is persisted: the project is dropped from task.yaml immediately, so
	// a later failure leaves task.yaml listing ONLY the still-present worktrees. A
	// retry then never re-attempts an already-removed worktree (which `git worktree
	// remove` would reject) and never re-verifies a vanished one. A worktree git
	// already reports as absent (stale/missing metadata) is treated as already
	// removed rather than an error.
	res := doneResult{Task: taskName}
	// Iterate over a stable snapshot of the original projects: the loop body
	// reassigns tk.Projects (to persist progress) as it goes, so we must not range
	// over the field being mutated.
	originalProjects := make([]task.Project, len(tk.Projects))
	copy(originalProjects, tk.Projects)
	for _, p := range originalProjects {
		wtPath := filepath.Join(dir, p.Worktree)
		_, present, ferr := worktree.Find(p.Repo, wtPath)
		if ferr != nil || present {
			// Either git positively reports the worktree as present, or we could
			// not read git state (ferr != nil) — in the unreadable case we still
			// attempt the removal so the failure is surfaced, never silently
			// skipped. A confirmed-absent worktree (ferr == nil && !present) is
			// treated as already removed and falls through to the persist step.
			if rerr := worktree.Remove(p.Repo, wtPath, force); rerr != nil {
				return res, output.Errorf(jsonOut, output.ErrGit,
					"task %q: failed to remove worktree for project %q: %v", taskName, p.Name, rerr)
			}
		}
		// Persist this removal (real or already-absent) before touching the next
		// worktree: drop the project from task.yaml so a failure on a LATER project
		// leaves a retryable, consistent task.yaml that lists only still-present
		// worktrees. A retry then never re-attempts an already-removed worktree.
		tk.Projects = dropProject(tk.Projects, p.Name)
		if serr := task.Save(dir, tk); serr != nil {
			return res, output.Errorf(jsonOut, output.ErrState,
				"task %q: removed worktree for project %q but failed to persist progress: %v", taskName, p.Name, serr)
		}
		res.RemovedWorktrees = append(res.RemovedWorktrees, p.Name)
	}

	// Phase 4: only now (every worktree removed) is it safe to delete the folder.
	if err := os.RemoveAll(dir); err != nil {
		return res, output.Errorf(jsonOut, output.ErrState,
			"task %q: removed worktrees but failed to delete task folder: %v", taskName, err)
	}
	return res, nil
}

// dropProject returns ps without the project named name, preserving order. It is
// used by runDone to persist worktree-removal progress one project at a time so a
// failed multi-project finalization can be retried without re-removing or
// re-verifying an already-removed worktree.
func dropProject(ps []task.Project, name string) []task.Project {
	out := make([]task.Project, 0, len(ps))
	for _, p := range ps {
		if p.Name != name {
			out = append(out, p)
		}
	}
	return out
}

func init() {
	var taskFlag string
	var force bool
	cmd := &cobra.Command{
		Use: "done [task]", Short: "Finish a task: verify, remove worktrees, remove the task folder",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			name := taskFlag
			if len(args) == 1 {
				name = args[0]
			}
			if name == "" {
				if name, err = resolveTask(home, ""); err != nil {
					return err
				}
			}
			res, err := runDone(home, name, force)
			if err != nil {
				return err
			}
			human := "Finished " + res.Task + " (removed " + fmt.Sprint(len(res.RemovedWorktrees)) +
				" worktree(s)); knowledge consolidated."
			return output.Emit(jsonOut, human, res)
		},
	}
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	cmd.Flags().BoolVar(&force, "force", false, "override unmerged/dirty refusal")
	RootCmd.AddCommand(cmd)
}
```

- [ ] **Step 4: Implement `internal/cli/where.go`**

```go
package cli

import (
	"os"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/spf13/cobra"
)

type whereOut struct {
	Home    string `json:"home"`
	TaskDir string `json:"task_dir,omitempty"`
}

func runWhere(home, cwd string) (whereOut, error) {
	loc, err := store.Locate(home, cwd)
	if err != nil {
		return whereOut{}, err
	}
	return whereOut{Home: home, TaskDir: loc.TaskDir}, nil
}

func init() {
	cmd := &cobra.Command{
		Use: "where", Short: "Print CTX_HOME and the current task path",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			w, err := runWhere(home, cwd)
			if err != nil {
				return err
			}
			human := "CTX_HOME=" + w.Home
			if w.TaskDir != "" {
				human += "\ntask=" + w.TaskDir
			}
			return output.Emit(jsonOut, human, w)
		},
	}
	RootCmd.AddCommand(cmd)
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/cli/ -run TestRunDone`
Expected: PASS (skips if git absent).

- [ ] **Step 6: Run the whole suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/done.go internal/cli/where.go internal/cli/done_test.go
git commit -m "feat: ctx done (verify+cleanup) and ctx where"
```

---

## Task 15: `ctx spec` — print a project's spec path/content

**Files:**
- Create: `internal/cli/spec.go`
- Test: `internal/cli/spec_test.go`

**Interfaces:**
- Consumes: `store.Locate`, `task.Load`.
- Produces: `cli.runSpec(home, cwd, taskFlag, projectFlag string) (specOut, error)` where
  `specOut{Project, Path, Content string}`. Resolution: task from `--task` else CWD;
  project from `--project` else CWD; errors `USAGE` when no task/project can be
  resolved, `NOT_FOUND` when the project is unregistered or its `spec.md` is missing.
  In `--json` mode the full `{project, path, content}` is emitted; in text mode the
  spec content is printed (path on stderr is implied via the human string).

- [ ] **Step 1: Write the failing test**

Create `internal/cli/spec_test.go`:

```go
package cli

import (
	"path/filepath"
	"testing"
)

func TestRunSpecResolvesNamedProject(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	taskDir := filepath.Join(home, "tk")
	writeProjectForTest(t, taskDir, "front")
	// writeProjectForTest sets Spec to "front/spec.md"; create that file.
	if err := writeSpecFileForTest(home, "tk", "front", "# front spec\nbody here"); err != nil {
		t.Fatal(err)
	}
	// Resolve by explicit task + project flags (CWD is outside the task).
	out, err := runSpec(home, home, "tk", "front")
	if err != nil {
		t.Fatal(err)
	}
	if out.Project != "front" || out.Path != filepath.Join(taskDir, "front", "spec.md") {
		t.Errorf("bad spec out: %+v", out)
	}
	if !contains(out.Content, "front spec") {
		t.Errorf("spec content missing: %q", out.Content)
	}
}

func TestRunSpecResolvesProjectFromCWD(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	taskDir := filepath.Join(home, "tk")
	writeProjectForTest(t, taskDir, "front")
	if err := writeSpecFileForTest(home, "tk", "front", "# front spec"); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(taskDir, "front", "wt")
	out, err := runSpec(home, cwd, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Project != "front" || !contains(out.Content, "front spec") {
		t.Errorf("CWD resolution failed: %+v", out)
	}
}

func TestRunSpecErrorsWhenUnregistered(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	if _, err := runSpec(home, home, "tk", "ghost"); err == nil {
		t.Error("expected NOT_FOUND for unregistered project")
	}
}
```

The `writeSpecFileForTest` helper used above is defined once in
`internal/cli/helpers_test.go` (added in Task 5) — do not redeclare it here.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestRunSpec`
Expected: FAIL — undefined `runSpec`, `specOut`.

- [ ] **Step 3: Implement `internal/cli/spec.go`**

```go
package cli

import (
	"os"
	"path/filepath"

	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/kimhyoyeon/context-manager/internal/task"
	"github.com/spf13/cobra"
)

type specOut struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func runSpec(home, cwd, taskFlag, projectFlag string) (specOut, error) {
	loc, _ := store.Locate(home, cwd)
	name := taskFlag
	if name == "" && loc != nil {
		name = loc.Task
	}
	if name == "" {
		return specOut{}, output.Errorf(jsonOut, output.ErrUsage, "not inside a task; pass --task")
	}
	proj := projectFlag
	if proj == "" && loc != nil {
		proj = loc.Project
	}
	if proj == "" {
		return specOut{}, output.Errorf(jsonOut, output.ErrUsage, "no project resolved; pass --project")
	}
	dir, err := checkedTaskDir(home, name)
	if err != nil {
		return specOut{}, err
	}
	tk, err := task.Load(dir)
	if err != nil {
		return specOut{}, output.Errorf(jsonOut, output.ErrNotFound, "task %q not found", name)
	}
	p := tk.Project(proj)
	if p == nil {
		return specOut{}, output.Errorf(jsonOut, output.ErrNotFound, "project %q not registered", proj)
	}
	path := filepath.Join(dir, p.Spec)
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return specOut{}, output.Errorf(jsonOut, output.ErrNotFound, "spec for %q not found at %s", proj, path)
	}
	return specOut{Project: proj, Path: path, Content: string(data)}, nil
}

func init() {
	var taskFlag, project string
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "Print the spec path/content for the current (or named) project",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			out, err := runSpec(home, cwd, taskFlag, project)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, out.Content, out)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project name (default: detect from CWD)")
	cmd.Flags().StringVar(&taskFlag, "task", "", "task name (default: detect from CWD)")
	RootCmd.AddCommand(cmd)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run TestRunSpec`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/spec.go internal/cli/spec_test.go
git commit -m "feat: ctx spec — print a project's spec path/content"
```

---

## Task 16: Companion skill, discovery pointer, README, and end-to-end smoke test

**Files:**
- Create: `skill/context-manager/SKILL.md`, `README.md`, `docs/global-claude-md-snippet.md`, `scripts/smoke.sh`

**Interfaces:** none (docs + verification). The skill encodes the §10 behavior layer.

- [ ] **Step 1: Write the companion skill**

Create `skill/context-manager/SKILL.md`:

```markdown
---
name: context-manager
description: Use the `ctx` CLI to manage cross-repo task context — when a task spans multiple repos/worktrees, when resuming work in a new session, when capturing a goal, or when recording/looking up per-project knowledge. Triggers include "start a cross-repo task", "resume my work", "what was the goal", "where am I", "save what we learned", "ctx".
---

# Using `ctx` (cross-repo context manager)

`ctx` stores cross-repo task context under `~/.ctx`. Full command reference:
run `ctx --help` and `ctx <command> --help` (the binary is the source of truth —
do not memorize flags from here).

## Hard rules

1. **Session start — rehydrate first.** Before doing anything on an existing
   task, run `ctx resume --json` (or `ctx current --json` to locate yourself).
   **Never re-infer the goal from the code.** Then re-arm the native goal:
   run `ctx goal --handoff` and invoke the printed `/goal` line.
2. **Capture the goal first.** At `ctx new`, set `--objective` and a verifiable
   `--done-when` before any coding. The goal is the one thing that must not be lost.
3. **Keep state live.** Update the worklist (`ctx task add|doing|done`) and append
   decisions with `ctx log` as you work.
4. **Knowledge — read first, write at the end.** When starting work, read
   `ctx know index` and `ctx know search <topic>` first (feed-forward). When the
   task is finished, consolidate durable findings into per-project topic pages
   with `ctx know add --project <p> --topic <t>` BEFORE `ctx done` — don't leave
   learnings in chat history.
5. **Finish with `ctx done`.** It refuses dirty/unmerged worktrees unless `--force`.

## Typical flow

    ctx new add-payment --objective "..." --done-when "..." --background "..."
    ctx add front --repo ~/Proj/front
    ctx add back  --repo ~/Proj/back
    # work inside <task>/<project>/wt; ctx task ...; ctx log ...
    # new session: ctx resume --json ; ctx goal --handoff
    ctx know add --project back --topic pay-endpoint --category gotcha --from notes.md
    ctx done
```

- [ ] **Step 2: Write the global discovery pointer**

Create `docs/global-claude-md-snippet.md`:

```markdown
# Add this to ~/.claude/CLAUDE.md (global memory)

## ctx — cross-repo context manager
`ctx` manages cross-repo task context (worktrees, goal, session resume,
per-project knowledge) under ~/.ctx. For multi-repo tasks, start with
`ctx resume --json` to rehydrate; run `ctx --help` for commands.
```

- [ ] **Step 3: Write the README**

Create `README.md`:

```markdown
# ctx — cross-repo context manager

A single-binary Go CLI that keeps the context of a cross-repo task in one place:
worktrees, specs, a north-star goal, session rehydration, and per-project
persistent knowledge — all under `~/.ctx`.

## Install

    # CGO_ENABLED=0 forces the required pure-Go static binary (no libc linkage).
    CGO_ENABLED=0 go build -o ~/bin/ctx ./cmd/ctx   # ensure ~/bin is on PATH
    ctx --help                                      # verify it runs

## Quick start

    ctx new add-payment --objective "add payment" --done-when "e2e pass + lint clean"
    ctx add front --repo ~/Proj/front
    ctx resume --json          # rehydrate a new session
    ctx goal --handoff         # re-arm native /goal
    ctx know search "결제"      # look up prior knowledge

## For AI assistants

The companion skill lives in `skill/context-manager/`. Install it into your
assistant's skills directory by **copying** or **symlinking** it (symlink keeps
it in sync with this repo):

    # Claude Code (skills live under ~/.claude/skills)
    cp -R skill/context-manager ~/.claude/skills/context-manager
    # ...or symlink to track this repo:
    ln -snf "$(pwd)/skill/context-manager" ~/.claude/skills/context-manager

    # Codex (skills live under ~/.codex/skills)
    cp -R skill/context-manager ~/.codex/skills/context-manager
    # ...or symlink:
    ln -snf "$(pwd)/skill/context-manager" ~/.codex/skills/context-manager

Then install the global discovery pointer so the assistant finds `ctx` without
being told — append the snippet from `docs/global-claude-md-snippet.md` to your
global memory file:

    cat docs/global-claude-md-snippet.md >> ~/.claude/CLAUDE.md   # Claude
    cat docs/global-claude-md-snippet.md >> ~/.codex/AGENTS.md    # Codex

See `ctx --help` for the full command reference.

## Design

See `docs/superpowers/specs/2026-06-18-context-manager-cli-design.md`.
```

- [ ] **Step 4: Write the end-to-end smoke test**

Create `scripts/smoke.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$(mktemp -d)/ctx"
# Build from the module root with a RELATIVE package path: Go rejects an absolute
# directory ("$ROOT/cmd/ctx") as a package argument, so use `go -C` (Go 1.20+).
# CGO_ENABLED=0 enforces the required pure-Go static build (global constraint).
CGO_ENABLED=0 go -C "$ROOT" build -o "$BIN" ./cmd/ctx
# Prove the resulting binary actually executes.
"$BIN" --help >/dev/null

export CTX_HOME="$(mktemp -d)"
REPO="$(mktemp -d)"
git -C "$REPO" init -b main -q
git -C "$REPO" config user.email t@t
git -C "$REPO" config user.name t
echo x > "$REPO/README"
git -C "$REPO" add . && git -C "$REPO" commit -qm init

"$BIN" new add-payment --objective "add payment" --done-when "e2e pass"
"$BIN" add front --repo "$REPO" --task add-payment
"$BIN" --json resume add-payment | grep -q '"done_when": "e2e pass"'
"$BIN" goal --handoff --task add-payment | grep -q '/goal e2e pass'
# --source-task stamps provenance (required) and satisfies done's consolidation
# gate. The page must be scoped to the task's REGISTERED project ("front"): done
# now requires the consolidated page's project facet to overlap the task's
# projects, so a page for an unrelated project would not satisfy the gate.
"$BIN" know add --project front --topic pay-endpoint --category gotcha --source-task add-payment --from <(echo "결제 처리 시 200 먼저 반환")
"$BIN" --json know search "결제" | grep -q '"topic": "pay-endpoint"'
"$BIN" done add-payment

echo "SMOKE OK"
```

> Note: `resume` is invoked with the positional task (`resume add-payment`) to
> exercise the positional argument path; `know add` passes `--source-task` AND
> scopes the page to the registered project `front`, so the page records
> provenance whose project facet overlaps the task's projects and `ctx done` finds
> the required consolidation.

- [ ] **Step 5: Run the smoke test**

Run: `chmod +x scripts/smoke.sh && ./scripts/smoke.sh`
Expected: prints `SMOKE OK` (exit 0). If git is unavailable the worktree steps fail — run on a machine with git.

- [ ] **Step 6: Final full build + test**

Run: `CGO_ENABLED=0 go build -o /tmp/ctx ./cmd/ctx && /tmp/ctx --help && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...`
Expected: the pure-Go static `ctx` binary builds and runs (`--help` prints), the
whole module builds clean, all tests pass, and vet is clean. `CGO_ENABLED=0`
enforces the required static build (global constraint).

- [ ] **Step 7: Commit**

```bash
git add skill/ README.md docs/global-claude-md-snippet.md scripts/smoke.sh
git commit -m "docs: companion skill, global CLAUDE.md pointer, README, smoke test"
```

---

## Self-Review (completed during planning)

**Spec coverage** — every spec section maps to a task:

| Spec section | Task(s) |
|---|---|
| §4 task-centric, CTX_HOME, filesystem-truth | 2, 3 |
| §5 folder structure (3-depth, `_knowledge/`) | 4, 7, 12 |
| §6 `task.yaml` model + worklist | 3, 9 |
| §6.1 `context.md` template | 4 |
| §7 `new/add/ls/status/current/set-status/done` | 4, 5, 7, 8, 9, 14 |
| §7 `goal/resume/log/task` | 10, 11 |
| §7 `know add/search/index` | 12, 13 |
| §7 `spec` (print project spec path/content) | 15 |
| §7 `where` | 14 |
| §8 lifecycle / data flow | 4–14 (spec in 15, end-to-end smoke in 16) |
| §9 persistent knowledge (topic pages, per-project isolation, `_shared/`, provenance, index/log, CJK search) | 12, 13 |
| §10 skill & discovery (3 layers) | 16 |
| §11 error handling (`{error,code}`, central raw-error rendering, atomic writes, refuse-by-default) | 1 (envelope + central Render), 2 (atomic), 14 (refuse) |
| §13 testing (unit / integration / knowledge / golden) | every task; goldens for `status`+`current` in 8, `task ls` in 9, `resume`+`goal`+`goal --handoff` in 11, `know search` in 13; live-git resume integration in 11 |

**Placeholder scan:** no TBD/TODO; every code step shows complete, copy-ready code.

**Type consistency:** `Task`/`Project`/`Goal`/`WorkItem` defined once (Task 3) and referenced by exact names; `progressOut`/`detailOut`/`overviewOut` (Task 8) reused in Tasks 9/11; `resume.ProjState`/`resume.Bundle` (Task 11) consumed by `cli/resume.go`; `knowledge.PageInput`/`Query`/`Result` (Tasks 12–13) consumed by `cli/know.go`. `loadTask`, `homeAndTask`, `resolveTask`, `checkedTaskDir`, `joinArgs`, `dirExists` are shared helpers introduced in Tasks 4/7/9 — keep one definition each in `util.go`. `checkedTaskDir` (Task 7 `util.go`) is the single name-validated replacement for raw `store.TaskDir(home, name)` and is used by `resolveTask`, `loadTask`, `runAdd`, `runStatusDetail`, `runResume`, `runDone` (via `loadTask`), and `runSpec` so an explicit `--task` name can never escape CTX_HOME. Single-definition helpers introduced by the fixes: `output.Render`/`output.IsRendered`/`classify`/`rendered` (Task 1 `output.go`, written complete in Step 4 so the package compiles at Step 5); `resolveTaskProject` (Task 9 `setstatus.go`); `knowledge.HasProvenance`/`knowledge.ValidCategory` (Task 12 `knowledge.go`); `worktree.List`/`worktree.Find`/`worktree.Entry` (Task 6 `worktree.go`, the git-truth source consumed by `runStatusDetail` in Task 8 `status.go` via the `projectDetail` type — whose `WorktreeError` field distinguishes an unreadable git state from a confirmed-absent worktree, so a git failure is never reported as `worktree_exists:true`); `worktree.BranchStatus` (Task 6 `worktree.go`, the registered-branch merge check consumed by `runDone` in Task 14 `done.go` so a switched/detached worktree cannot mask unmerged work); `dropProject` (Task 14 `done.go`, the single helper `runDone` uses to persist worktree-removal progress one project at a time so a failed multi-project finalization is retryable); `resume.ProjState.Available`/`Error` (Task 11 `bundle.go`, set by the `cli/resume.go` gitState closure so a missing worktree is an explicit error, never fabricated clean state); `resumeHuman` (Task 11 `cli/resume.go`, the single text-mode formatter that prints the COMPLETE bundle including per-project live/unavailable git state); `promptMissingGoal`/`isInteractive` (Task 4 `new.go`); `assertGolden` (Task 8 `golden_test.go`); `writeSpecFileForTest`/`writeRepolessProjectForTest` (Task 5 `helpers_test.go`, the latter feeding the deterministic `status_detail.json` golden) — keep one definition each. `runDone`'s phase 1 consolidation prerequisite (`knowledge.HasProvenance`) is enforced even under `--force`; `--force` overrides only phase 2 (the dirty/unmerged/status-read safety check), per SPEC §7/§9/§11.

**Known cross-task assembly notes (call out to the implementer):**
- `internal/cli/util.go` accumulates helpers across Tasks 4, 7, 9 — merge into ONE file with a single `package cli` + one import block.
- `internal/store/store.go` imports `fmt` and `strings` from Task 3 Step 3 (for `ValidName`/`ValidRelPath`); Task 5's `Locate` reuses them and adds no new import.
- `store.ValidName`/`store.ValidRelPath` are defined once in Task 3 `store.go` and consumed by `task.Load` (Task 3), `runNew` (Task 4), `checkedTaskDir` (Task 7 `util.go`, which uses `ValidRelPath(home, name)` so a symlinked task dir redirecting outside CTX_HOME is rejected, not merely name-checked), `runAdd` (Task 7), `runDone` (Task 14), and `knowledge.Add` (Task 12, which anchors `ValidRelPath` to CTX_HOME itself so a symlinked `_knowledge`/group dir cannot redirect a write off-store) — keep one definition each.
- `internal/cli/status_test.go` (Task 8 Step 1) imports `os`, `os/exec`, `path/filepath`, `testing`, and `internal/task` (the last two for the unreadable-repo test) — keep them in one import block. The golden harness `assertGolden` lives in the separate `internal/cli/golden_test.go` (Task 8 Step 5), so it adds no imports to `status_test.go`.
