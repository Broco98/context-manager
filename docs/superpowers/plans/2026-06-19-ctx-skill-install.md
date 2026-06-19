# ctx skill-install Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `ctx skill install` / `ctx skill status` command that installs the
companion skill + global discovery snippet into detected agent homes, from a
self-contained binary.

**Architecture:** Embed `SKILL.md` + the discovery snippet into the binary via a
root-level `assets` package (`go:embed` cannot reference `../`, so it must live at the
module root where `skill/` and `docs/` are reachable). A new `internal/cli/skill.go`
resolves target agent homes under `$HOME`, writes the skill file atomically, and
upserts a marker-delimited snippet block. Read paths return data values so the
existing `assertGolden` harness pins the JSON shape.

**Tech Stack:** Go 1.25, cobra, `internal/store.AtomicWrite`, `internal/output`.

## Global Constraints

- Pure-Go static build (`CGO_ENABLED=0`); no new third-party deps.
- Filesystem is the source of truth; all managed writes go through `store.AtomicWrite`
  (temp + rename).
- Fail-closed: never clobber a user's manual symlink without `--force`.
- Every read command supports `--json`; errors use the `output.Errorf`/`Render`
  `{error, code}` envelope. Codes: `output.ErrUsage`, `output.ErrState`,
  `output.ErrConflict`.
- Agent homes resolve via `os.UserHomeDir()` (= `$HOME`), never `CTX_HOME`.
- Module path is `github.com/kimhyoyeon/context-manager`; the root assets package is
  imported as `assets "github.com/kimhyoyeon/context-manager"`.

---

## File Structure

- `embed.go` *(new, package `assets`, module root)* — embeds `SkillMD`, `Snippet`.
- `assets_test.go` *(new, package `assets`)* — embedded-content sanity.
- `internal/cli/skill.go` *(new)* — target table, `runSkillInstall`/`runSkillStatus`,
  `installSkill`, `upsertSnippet`, `skillStatus`, cobra wiring.
- `internal/cli/skill_test.go` *(new)* — install/status/idempotency/conflict tests.
- `internal/cli/testdata/skill_status.json` *(new)* — golden for the status shape.
- `README.md` *(modify)* — replace the manual cp/ln/cat block with `ctx skill install`.
- `scripts/install.sh` *(modify)* — print a hint to run `ctx skill install`.

---

### Task 1: Embed assets package

**Files:**
- Create: `embed.go`
- Test: `assets_test.go`

**Interfaces:**
- Produces: `assets.SkillMD string`, `assets.Snippet string`.

- [ ] **Step 1: Write the failing test** (`assets_test.go`)

```go
package assets

import (
	"strings"
	"testing"
)

func TestEmbeddedSkillNonEmpty(t *testing.T) {
	if !strings.Contains(SkillMD, "name: context-manager") {
		t.Errorf("SkillMD missing skill frontmatter; got %d bytes", len(SkillMD))
	}
}

func TestEmbeddedSnippetNonEmpty(t *testing.T) {
	if !strings.Contains(Snippet, "cross-repo") {
		t.Errorf("Snippet missing expected text; got %d bytes", len(Snippet))
	}
}
```

- [ ] **Step 2: Run test — expect FAIL** (`assets` package does not exist)

Run: `go test ./...`
Expected: build error — undefined `SkillMD`/`Snippet`.

- [ ] **Step 3: Create `embed.go`**

```go
package assets

import _ "embed"

//go:embed skill/context-manager/SKILL.md
var SkillMD string

//go:embed docs/global-claude-md-snippet.md
var Snippet string
```

- [ ] **Step 4: Run test — expect PASS**

Run: `go test . ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add embed.go assets_test.go
git commit -m "feat(skill): embed SKILL.md + discovery snippet via go:embed"
```

---

### Task 2: Target table + `skill status`

**Files:**
- Create: `internal/cli/skill.go`
- Test: `internal/cli/skill_test.go`

**Interfaces:**
- Consumes: `assets.SkillMD`, `assets.Snippet`, `output.Emit/Errorf`, `store.AtomicWrite`.
- Produces:
  - `type skillTarget struct{ name, homeRel, snippetFile string }`
  - `func resolveTargets(home string, names []string) ([]skillTarget, error)`
  - `func runSkillStatus(home string, names []string) ([]skillStatusOut, error)`
  - `type skillStatusOut struct{ Target string; SkillInstalled, SkillMatchesEmbedded, SnippetInstalled bool }`

- [ ] **Step 1: Write the failing test** (`skill_test.go`)

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunSkillStatusEmptyHome(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := runSkillStatus(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Target != "claude" || got[0].SkillInstalled {
		t.Errorf("want one uninstalled claude target, got %+v", got)
	}
}

func TestResolveTargetsNoHomeErrors(t *testing.T) {
	if _, err := resolveTargets(t.TempDir(), nil); err == nil {
		t.Error("want error when no agent home exists")
	}
}

func TestResolveTargetsExplicitMissingErrors(t *testing.T) {
	if _, err := resolveTargets(t.TempDir(), []string{"claude"}); err == nil {
		t.Error("want error when --target claude home is absent")
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`runSkillStatus`/`resolveTargets` undefined)

Run: `go test ./internal/cli/ -run TestRunSkillStatus -v`
Expected: build error.

- [ ] **Step 3: Implement `internal/cli/skill.go`** (status half)

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	assets "github.com/kimhyoyeon/context-manager"
	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/spf13/cobra"
)

const (
	skillSubdir  = "skills/context-manager"
	skillFile    = "SKILL.md"
	snippetBegin = "<!-- ctx:begin -->"
	snippetEnd   = "<!-- ctx:end -->"
)

type skillTarget struct {
	name, homeRel, snippetFile string
}

var skillTargets = []skillTarget{
	{name: "claude", homeRel: ".claude", snippetFile: "CLAUDE.md"},
	{name: "codex", homeRel: ".codex", snippetFile: "AGENTS.md"},
}

func lookupTarget(name string) (skillTarget, bool) {
	for _, t := range skillTargets {
		if t.name == name {
			return t, true
		}
	}
	return skillTarget{}, false
}

// resolveTargets returns the targets to act on: the named ones (erroring if a
// named target's home is absent), or every target whose home exists when none
// are named. No resolvable target is a STATE error.
func resolveTargets(home string, names []string) ([]skillTarget, error) {
	if len(names) > 0 {
		var out []skillTarget
		for _, n := range names {
			t, ok := lookupTarget(n)
			if !ok {
				return nil, output.Errorf(jsonOut, output.ErrUsage, "unknown target %q (want claude or codex)", n)
			}
			dir := filepath.Join(home, t.homeRel)
			if _, err := os.Stat(dir); err != nil {
				return nil, output.Errorf(jsonOut, output.ErrState, "%s home not found at %s", t.name, dir)
			}
			out = append(out, t)
		}
		return out, nil
	}
	var out []skillTarget
	for _, t := range skillTargets {
		if _, err := os.Stat(filepath.Join(home, t.homeRel)); err == nil {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, output.Errorf(jsonOut, output.ErrState, "no agent home found under %s; pass --target claude|codex", home)
	}
	return out, nil
}

type skillStatusOut struct {
	Target               string `json:"target"`
	SkillInstalled       bool   `json:"skillInstalled"`
	SkillMatchesEmbedded bool   `json:"skillMatchesEmbedded"`
	SnippetInstalled     bool   `json:"snippetInstalled"`
}

func runSkillStatus(home string, names []string) ([]skillStatusOut, error) {
	ts, err := resolveTargets(home, names)
	if err != nil {
		return nil, err
	}
	out := make([]skillStatusOut, 0, len(ts))
	for _, t := range ts {
		s := skillStatusOut{Target: t.name}
		if b, err := os.ReadFile(filepath.Join(home, t.homeRel, skillSubdir, skillFile)); err == nil {
			s.SkillInstalled = true
			s.SkillMatchesEmbedded = string(b) == assets.SkillMD
		}
		if b, err := os.ReadFile(filepath.Join(home, t.homeRel, t.snippetFile)); err == nil {
			s.SnippetInstalled = strings.Contains(string(b), snippetBegin)
		}
		out = append(out, s)
	}
	return out, nil
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/cli/ -run 'TestRunSkillStatus|TestResolveTargets' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/skill.go internal/cli/skill_test.go
git commit -m "feat(skill): target resolution + ctx skill status"
```

---

### Task 3: `installSkill` (skill file write + symlink conflict)

**Files:**
- Modify: `internal/cli/skill.go`
- Test: `internal/cli/skill_test.go`

**Interfaces:**
- Produces: `func installSkill(home string, t skillTarget, force bool) (action, path string, err error)`
  where `action ∈ {created, updated, unchanged}`.

- [ ] **Step 1: Write failing tests**

```go
func TestInstallSkillCreatesAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	ct, _ := lookupTarget("claude")
	a1, p, err := installSkill(home, ct, false)
	if err != nil || a1 != "created" {
		t.Fatalf("first install: action=%q err=%v", a1, err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != assetsSkillMDForTest() {
		t.Error("skill file content != embedded")
	}
	a2, _, _ := installSkill(home, ct, false)
	if a2 != "unchanged" {
		t.Errorf("second install action: got %q want unchanged", a2)
	}
}

func TestInstallSkillRefusesSymlinkWithoutForce(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "skills")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.Symlink(t.TempDir(), filepath.Join(dir, "context-manager"))
	ct, _ := lookupTarget("claude")
	if _, _, err := installSkill(home, ct, false); err == nil {
		t.Error("want refusal on symlinked skill dir without --force")
	}
	if _, _, err := installSkill(home, ct, true); err != nil {
		t.Errorf("--force should replace the symlink: %v", err)
	}
}
```

Add a tiny helper at the bottom of `skill_test.go`:

```go
func assetsSkillMDForTest() string {
	b, _ := os.ReadFile(filepath.Join("..", "..", "skill", "context-manager", "SKILL.md"))
	return string(b)
}
```

- [ ] **Step 2: Run — expect FAIL** (`installSkill` undefined)

Run: `go test ./internal/cli/ -run TestInstallSkill -v`
Expected: build error.

- [ ] **Step 3: Add `installSkill` to `skill.go`**

```go
func installSkill(home string, t skillTarget, force bool) (action, path string, err error) {
	dir := filepath.Join(home, t.homeRel, skillSubdir)
	path = filepath.Join(dir, skillFile)
	// A symlinked skill dir is a user's deliberate live-link (`ln -snf`); never
	// clobber it silently. Remove only the link (not its target) under --force.
	if fi, lerr := os.Lstat(dir); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		if !force {
			return "", path, output.Errorf(jsonOut, output.ErrConflict,
				"%s is a symlink (manually installed); pass --force to replace", dir)
		}
		if rmErr := os.Remove(dir); rmErr != nil {
			return "", path, rmErr
		}
	}
	switch existing, rerr := os.ReadFile(path); {
	case rerr == nil && string(existing) == assets.SkillMD:
		action = "unchanged"
	case rerr == nil:
		action = "updated"
	case os.IsNotExist(rerr):
		action = "created"
	default:
		return "", path, rerr
	}
	if err := store.AtomicWrite(path, []byte(assets.SkillMD)); err != nil {
		return "", path, err
	}
	return action, path, nil
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/cli/ -run TestInstallSkill -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/skill.go internal/cli/skill_test.go
git commit -m "feat(skill): write skill file with fail-closed symlink conflict"
```

---

### Task 4: `upsertSnippet` (idempotent marker block)

**Files:**
- Modify: `internal/cli/skill.go`
- Test: `internal/cli/skill_test.go`

**Interfaces:**
- Produces: `func upsertSnippet(home string, t skillTarget) (action, path string, err error)`
  where `action ∈ {created, appended, updated, unchanged}`.

- [ ] **Step 1: Write failing tests**

```go
func TestUpsertSnippetIdempotentAndPreservesContent(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	ct, _ := lookupTarget("claude")
	// Pre-existing unrelated content must survive.
	p := filepath.Join(home, ".claude", "CLAUDE.md")
	_ = os.WriteFile(p, []byte("# my prefs\nkeep me\n"), 0o644)

	a1, _, err := upsertSnippet(home, ct)
	if err != nil || a1 != "appended" {
		t.Fatalf("first upsert: action=%q err=%v", a1, err)
	}
	a2, _, _ := upsertSnippet(home, ct)
	if a2 != "unchanged" {
		t.Errorf("second upsert action: got %q want unchanged", a2)
	}
	b, _ := os.ReadFile(p)
	body := string(b)
	if !strings.Contains(body, "keep me") {
		t.Error("unrelated user content was lost")
	}
	if strings.Count(body, snippetBegin) != 1 {
		t.Errorf("want exactly one snippet block, got %d", strings.Count(body, snippetBegin))
	}
}

func TestUpsertSnippetReplacesEditedBlock(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	ct, _ := lookupTarget("claude")
	p := filepath.Join(home, ".claude", "CLAUDE.md")
	_ = os.WriteFile(p, []byte(snippetBegin+"\nstale\n"+snippetEnd+"\n"), 0o644)
	a, _, _ := upsertSnippet(home, ct)
	if a != "updated" {
		t.Errorf("action: got %q want updated", a)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "stale") {
		t.Error("stale block content was not replaced")
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`upsertSnippet` undefined)

Run: `go test ./internal/cli/ -run TestUpsertSnippet -v`
Expected: build error.

- [ ] **Step 3: Add `upsertSnippet` to `skill.go`**

```go
func upsertSnippet(home string, t skillTarget) (action, path string, err error) {
	path = filepath.Join(home, t.homeRel, t.snippetFile)
	block := snippetBegin + "\n" + strings.TrimSpace(assets.Snippet) + "\n" + snippetEnd + "\n"

	existing, rerr := os.ReadFile(path)
	if rerr != nil && !os.IsNotExist(rerr) {
		return "", path, rerr
	}
	content := string(existing) // "" when missing

	bi := strings.Index(content, snippetBegin)
	ei := strings.Index(content, snippetEnd)
	var updated string
	switch {
	case bi >= 0 && ei > bi:
		end := ei + len(snippetEnd)
		if end < len(content) && content[end] == '\n' {
			end++
		}
		if content[bi:end] == block {
			return "unchanged", path, nil
		}
		action, updated = "updated", content[:bi]+block+content[end:]
	case content == "":
		action, updated = "created", block
	default:
		sep := "\n"
		if !strings.HasSuffix(content, "\n") {
			sep = "\n\n"
		}
		action, updated = "appended", content+sep+block
	}
	if err := store.AtomicWrite(path, []byte(updated)); err != nil {
		return "", path, err
	}
	return action, path, nil
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/cli/ -run TestUpsertSnippet -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/skill.go internal/cli/skill_test.go
git commit -m "feat(skill): idempotent marker-delimited snippet upsert"
```

---

### Task 5: `runSkillInstall`, cobra wiring, golden status shape

**Files:**
- Modify: `internal/cli/skill.go`
- Test: `internal/cli/skill_test.go`
- Create: `internal/cli/testdata/skill_status.json`

**Interfaces:**
- Produces:
  - `type skillInstallOut struct{ Target, SkillPath, SkillAction, SnippetFile, SnippetAction string }`
  - `func runSkillInstall(home string, names []string, force bool) ([]skillInstallOut, error)`
  - cobra `skill` group with `install` + `status` registered on `RootCmd`.

- [ ] **Step 1: Write failing tests**

```go
func TestRunSkillInstallActionsAndPaths(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	got, err := runSkillInstall(home, nil, false)
	if err != nil || len(got) != 1 {
		t.Fatalf("install: got %+v err=%v", got, err)
	}
	r := got[0]
	if r.Target != "claude" || r.SkillAction != "created" || r.SnippetAction != "created" {
		t.Errorf("unexpected actions: %+v", r)
	}
	if !strings.HasSuffix(r.SkillPath, "/.claude/skills/context-manager/SKILL.md") {
		t.Errorf("skillPath suffix: %s", r.SkillPath)
	}
}

func TestSkillStatusGolden(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	_, _ = runSkillInstall(home, nil, false)
	got, err := runSkillStatus(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "skill_status.json", got)
}
```

- [ ] **Step 2: Run — expect FAIL** (`runSkillInstall` undefined; golden missing)

Run: `go test ./internal/cli/ -run 'TestRunSkillInstall|TestSkillStatusGolden' -v`
Expected: build error, then missing-golden error.

- [ ] **Step 3: Add `runSkillInstall` + wiring to `skill.go`**

```go
type skillInstallOut struct {
	Target        string `json:"target"`
	SkillPath     string `json:"skillPath"`
	SkillAction   string `json:"skillAction"`
	SnippetFile   string `json:"snippetFile"`
	SnippetAction string `json:"snippetAction"`
}

func runSkillInstall(home string, names []string, force bool) ([]skillInstallOut, error) {
	ts, err := resolveTargets(home, names)
	if err != nil {
		return nil, err
	}
	out := make([]skillInstallOut, 0, len(ts))
	for _, t := range ts {
		sa, sp, err := installSkill(home, t, force)
		if err != nil {
			return nil, err
		}
		na, np, err := upsertSnippet(home, t)
		if err != nil {
			return nil, err
		}
		out = append(out, skillInstallOut{t.name, sp, sa, np, na})
	}
	return out, nil
}

func init() {
	skillCmd := &cobra.Command{Use: "skill", Short: "Install the ctx companion skill + discovery snippet into agent homes"}

	var instTargets []string
	var force bool
	installCmd := &cobra.Command{
		Use: "install", Short: "Install the skill + discovery snippet for detected agents", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			res, err := runSkillInstall(home, instTargets, force)
			if err != nil {
				return err
			}
			human := ""
			for i, r := range res {
				if i > 0 {
					human += "\n"
				}
				human += fmt.Sprintf("%s: skill %s → %s; snippet %s → %s", r.Target, r.SkillAction, r.SkillPath, r.SnippetAction, r.SnippetFile)
			}
			return output.Emit(jsonOut, human, map[string]any{"installed": res})
		},
	}
	installCmd.Flags().StringSliceVar(&instTargets, "target", nil, "claude|codex (default: auto-detect)")
	installCmd.Flags().BoolVar(&force, "force", false, "replace a manually-symlinked skill dir")

	var statTargets []string
	statusCmd := &cobra.Command{
		Use: "status", Short: "Show skill + snippet install status per agent", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			res, err := runSkillStatus(home, statTargets)
			if err != nil {
				return err
			}
			human := ""
			for i, r := range res {
				if i > 0 {
					human += "\n"
				}
				human += fmt.Sprintf("%s: skill=%v (matches=%v) snippet=%v", r.Target, r.SkillInstalled, r.SkillMatchesEmbedded, r.SnippetInstalled)
			}
			return output.Emit(jsonOut, human, map[string]any{"targets": res})
		},
	}
	statusCmd.Flags().StringSliceVar(&statTargets, "target", nil, "claude|codex (default: auto-detect)")

	skillCmd.AddCommand(installCmd, statusCmd)
	RootCmd.AddCommand(skillCmd)
}
```

- [ ] **Step 4: Generate the golden, then run**

Run: `UPDATE_GOLDEN=1 go test ./internal/cli/ -run TestSkillStatusGolden` then
`go test ./internal/cli/ -run 'TestRunSkillInstall|TestSkillStatusGolden' -v`
Expected: PASS. Verify `testdata/skill_status.json` shows
`skillInstalled:true, skillMatchesEmbedded:true, snippetInstalled:true`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/skill.go internal/cli/skill_test.go internal/cli/testdata/skill_status.json
git commit -m "feat(skill): runSkillInstall + cobra wiring + status golden"
```

---

### Task 6: Docs — README + install hint

**Files:**
- Modify: `README.md` ("For AI assistants" section)
- Modify: `scripts/install.sh`

- [ ] **Step 1: Update README** — replace the manual `cp`/`ln`/`cat` block with:

```sh
# Install the companion skill + global discovery pointer for detected agents
ctx skill install            # auto-detects ~/.claude and ~/.codex
ctx skill status             # verify

# (fallback) manual install if you prefer:
#   cp -R skill/context-manager ~/.claude/skills/context-manager
#   cat docs/global-claude-md-snippet.md >> ~/.claude/CLAUDE.md
```

- [ ] **Step 2: Append a hint to `scripts/install.sh`** (after the PATH section, do NOT auto-run):

```sh
echo
echo "next: run 'ctx skill install' to set up the companion skill + discovery"
echo "      pointer for your coding agents (Claude Code / Codex)."
```

- [ ] **Step 3: Verify the whole suite**

Run: `go test ./...`
Expected: PASS (all packages).

- [ ] **Step 4: Commit**

```bash
git add README.md scripts/install.sh
git commit -m "docs(skill): document ctx skill install in README + install hint"
```

---

## Self-Review

- **Spec coverage:** §5 surface → Tasks 2/5; §6 descriptor → Task 2; §7 embed → Task 1;
  §8 snippet upsert → Task 4; §9 skill write + conflict → Task 3; §10 output/golden →
  Task 5 (status golden; install asserted by action+suffix because paths are
  machine-specific — deliberate deviation from "both golden"); §11 errors → Tasks 2/3;
  §12 tests → distributed; §13 change map → all tasks. No gaps.
- **Placeholder scan:** none — every step has real code/commands.
- **Type consistency:** `skillTarget`, `skillStatusOut`, `skillInstallOut`,
  `runSkillStatus`, `runSkillInstall`, `installSkill`, `upsertSnippet` used identically
  across tasks.

## Execution Handoff

Per the user's "A, C 진행" instruction (ultracode), execute **inline** in this session
(superpowers:executing-plans), interleaving Task A (security-validator tests) which is
independent. A final adversarial review workflow verifies both before completion.
