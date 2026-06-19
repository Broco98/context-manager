# `ctx skill` — Agent Integration Installer — Design

- **Date**: 2026-06-19
- **Status**: Approved (design phase)
- **Author**: brainstormed with user

## 1. Problem

The README defines a 3-layer agent integration:

- **Discovery** — a snippet in the agent's global memory so it knows `ctx` exists.
- **Reference** — `ctx --help`, generated from code, always present.
- **Behavior** — the companion skill teaching *when/why* (`skill/context-manager/SKILL.md`).

Today **only the Reference layer is automatic.** Discovery and Behavior require
manual `cp` / `ln` / `cat` steps from the source repo. Consequences:

- A user who installs `ctx` via `go install` has the binary on PATH but **no skill
  and no discovery pointer** — the agent never proactively uses `ctx`. (Observed in
  this very environment: the binary was on PATH, but `~/.claude/skills/context-manager`
  and the global-memory snippet were both absent.)
- The manual steps reference repo-relative paths (`skill/context-manager`,
  `docs/global-claude-md-snippet.md`) that a `go install`ed binary **cannot see** —
  the source tree may not even be present on the machine.
- Hand-editing the agent's global memory file is error-prone (duplicate appends on
  re-run).

We want a first-class `ctx` command that installs the Discovery + Behavior layers
itself, idempotently, with **no dependence on the source tree**.

## 2. Goals

1. **One command** sets up the skill + discovery snippet for detected coding agents.
2. **Self-contained**: works from a `go install`ed binary with no source tree present
   (assets embedded in the binary).
3. **Idempotent & safe**: re-running never duplicates; never silently clobbers a
   non-`ctx` file; atomic writes; default-refuse on conflict (matches `ctx`'s
   fail-closed philosophy).
4. **Multi-agent**: auto-detect Claude Code and Codex homes; `--target` to override.
5. **Inspectable**: `ctx skill status` reports what is installed and whether it
   matches the binary's embedded version (drift is visible, never hidden).
6. **Consistent with the tool**: `--json` on read paths, golden-tested output, the
   same `{error, code}` envelope.

## 3. Non-Goals (v1)

- No shell-completion installation. `ctx completion` already exists; wiring it into
  rc files is a separate concern, deferrable later as `ctx skill install --completion`.
- No `ctx skill uninstall`. The skill dir is `ctx`-owned; manual `rm` is trivial.
- No agents beyond Claude Code and Codex (the design is extensible by adding a
  descriptor row — see §6).
- No editing of project-local `.claude/` / `CLAUDE.md`. Global homes only; project
  scope is the user's per-repo call.
- No network, no version negotiation. The embedded snapshot is the source.

## 4. Key Decisions

| Axis | Decision | Rationale |
|------|----------|-----------|
| Asset delivery | `go:embed` snapshot compiled into the binary | Self-contained; works wherever `go install` puts the binary; no source-tree dependency. Snapshot staleness is acceptable and made visible by `status`. |
| Embed origin | A **root-level** package (`assets`, file `/embed.go`, import path = module root) embedding `skill/...` and `docs/...` | `go:embed` cannot reference `../`; the embedding file must sit at or above the asset dirs. Keeps `skill/` + `docs/` as the **single canonical source** (no duplicated copies → no drift). |
| Command shape | `ctx skill` group with `install` and `status` | Matches existing groups (`task`, `know`, `goal`); leaves room to grow (`uninstall`, `--completion`). Chosen over a flat `ctx skill-set`. |
| Target resolution | Auto-detect homes that exist; `--target claude\|codex` (repeatable) overrides | "Just set it up" intent; explicit override for scripts/CI. |
| Skill file write | Always (re)write from embed via `store.AtomicWrite` | `ctx` owns the path; re-install after a binary upgrade refreshes the skill. |
| Snippet write | Marker-delimited block; replace if present, append if absent, create file if missing | Idempotent; lets snippet edits propagate; never duplicates; never rewrites unrelated user content. |
| Conflict policy | Refuse (non-zero, structured error) if the skill path exists **as a symlink**, unless `--force` | Fail-closed; protects a user's deliberate `ln -snf` live-tracking link. A regular file is the same logical asset and is safely refreshed. |
| Home resolution | `os.UserHomeDir()` (= `$HOME`), **not** `CTX_HOME` | Agent homes are independent of `ctx`'s store; `$HOME` is injectable for tests via `t.Setenv`. |

## 5. Command Surface

```
ctx skill install [--target claude|codex]... [--force] [--json]
ctx skill status  [--target claude|codex]... [--json]
```

- `install`: for each resolved target, write the skill file and upsert the snippet;
  report per-target actions.
- `status`: for each resolved target, report skill presence + match-vs-embedded, and
  snippet presence.
- Default target set = the subset of `{claude, codex}` whose home dir exists.
- If `--target` is given, use exactly those, and **error** if a named target's home is
  absent (explicit request → explicit failure).
- If no target resolves (neither home exists, no `--target`), exit with a clear
  `STATE` error suggesting `--target`.

## 6. Target Descriptor (the one extension seam)

A small internal table drives everything; adding an agent = adding a row.

```go
type target struct {
    name        string // "claude" | "codex"
    homeRel     string // ".claude"  | ".codex"   (joined onto $HOME)
    snippetFile string // "CLAUDE.md" | "AGENTS.md"
}
```

- Skill file: `<$HOME>/<homeRel>/skills/context-manager/SKILL.md`
- Snippet file: `<$HOME>/<homeRel>/<snippetFile>`

(Claude → `.claude` + `CLAUDE.md`; Codex → `.codex` + `AGENTS.md`.)

## 7. Embedded Assets

New root package `assets` (`/embed.go`):

```go
package assets

import _ "embed"

//go:embed skill/context-manager/SKILL.md
var SkillMD string

//go:embed docs/global-claude-md-snippet.md
var Snippet string
```

Consumed by `internal/cli/skill.go`. Note: a content-equality test against the
on-disk file would be **tautological** (embed reads that file at build time). The
meaningful guards are: (a) the **build breaks** if the embed path stops resolving
(file moved/renamed), and (b) a sanity test that `SkillMD` is non-empty and contains
`name: context-manager`, and `Snippet` is non-empty and contains `cross-repo`.

## 8. Snippet Upsert Algorithm

```
BEGIN = "<!-- ctx:begin -->"
END   = "<!-- ctx:end -->"
block = BEGIN + "\n" + trim(assets.Snippet) + "\n" + END + "\n"

content = read(snippetFile)            // "" if file missing
if content contains BEGIN and END:
    region = content[BEGIN line .. END line inclusive]
    new    = content with region replaced by block
    action = (region == block) ? "unchanged" : "updated"
else:
    new    = content (+ trailing blank line if non-empty) + block
    action = (content == "") ? "created" : "appended"
AtomicWrite(snippetFile, new)
```

- The marker scan is a literal match on the two marker lines; the region runs from the
  BEGIN line through the END line inclusive.
- Only the delimited region is ever touched — user content around the block is
  preserved verbatim.

## 9. Skill File Write

```
skillPath = <$HOME>/<homeRel>/skills/context-manager/SKILL.md
fi, statErr = lstat(skillDir or skillPath)
if exists and is symlink and not --force:
    refuse (error STATE: "manually installed symlink; pass --force to replace")
existing = read(skillPath)             // "" if missing
action   = classify(existing vs assets.SkillMD): created|updated|unchanged
AtomicWrite(skillPath, assets.SkillMD)
```

Conflict detection is deliberately minimal for v1: a **symlink** at the skill path is
the only case treated as "not ours" (a user's `ln -snf` live link). A regular file/dir
is the same logical asset and is refreshed in place. `--force` replaces the symlink
with a real file.

## 10. Output Contract

`install --json`:

```json
{
  "installed": [
    { "target": "claude",
      "skillPath": "/Users/x/.claude/skills/context-manager/SKILL.md",
      "skillAction": "created",
      "snippetFile": "/Users/x/.claude/CLAUDE.md",
      "snippetAction": "appended" }
  ]
}
```

`status --json`:

```json
{ "targets": [
  { "target": "claude", "skillInstalled": true, "skillMatchesEmbedded": true, "snippetInstalled": true }
] }
```

- Text mode: one human-readable line per target.
- Both shapes pinned with golden tests.
- Errors flow through the existing `output.Render` `{error, code}` envelope.

## 11. Error Handling

- No home resolves → `STATE`: "no agent home found; pass --target claude|codex".
- `--target X` but `<$HOME>/.X` absent → `STATE`: "...home not found at <path>".
- Symlinked skill path without `--force` → `STATE` (see §9).
- All filesystem errors bubble to `root.go`'s single top-level envelope.

## 12. Testing

All tests run over a temp `$HOME` (`t.Setenv("HOME", tmp)`; `CTX_HOME` is untouched
and irrelevant to this command):

1. **Fresh install** (claude home present): skill file written and equals embedded;
   snippet file created with exactly one marker block.
2. **Idempotent re-install**: second run → `snippetAction == "unchanged"`, exactly one
   BEGIN/END pair; `skillAction == "unchanged"`.
3. **Snippet edit propagation**: hand-edit the block, re-install → `"updated"`, block
   restored to embedded content.
4. **User content preserved**: pre-write `CLAUDE.md` with unrelated lines → after
   install both the unrelated lines and the block are present; re-run does not grow the
   file.
5. **Conflict**: pre-create `skills/context-manager` as a symlink → install refuses
   without `--force`; succeeds (replaces) with `--force`.
6. **Auto-detect**: only `.codex` exists → codex only; both exist → both; neither and
   no `--target` → error.
7. **Explicit-target miss**: `--target claude` with no `.claude` → error.
8. **status**: reflects installed/not; `skillMatchesEmbedded` flips false when the
   on-disk skill is altered.
9. **Embedded-asset sanity**: `assets.SkillMD` non-empty + contains
   `name: context-manager`; `assets.Snippet` non-empty + contains `cross-repo`.
10. **Golden**: `install --json` and `status --json` shapes pinned.

Coverage target: the new command + assets package at high coverage from day one (no
legacy gaps).

## 13. File-by-file change map

- `/embed.go` *(new)* — `package assets`; embed `SkillMD` + `Snippet`.
- `internal/cli/skill.go` *(new)* — `skillCmd` group + `install` / `status`; target
  table; upsert + write logic; output structs; `init()` registers on `RootCmd`.
- `internal/cli/skill_test.go` *(new)* — the tests in §12.
- `internal/cli/testdata/` — golden files for the two JSON shapes.
- `README.md` — replace the manual `cp` / `ln` / `cat` block in "For AI assistants"
  with `ctx skill install`, keeping the manual steps as a fallback note.
- `scripts/install.sh` — optional: after installing the binary, print a hint to run
  `ctx skill install`. Do **not** auto-run it — writing into agent homes must stay an
  explicit user action.

## 14. Deferred

- `ctx skill install --completion` (shell completion into rc files).
- `ctx skill uninstall`.
- More targets (Cursor, Gemini, Copilot) via new descriptor rows.
- Per-project (`.claude/`) install scope.
