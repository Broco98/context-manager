# Context Manager CLI (`ctx`) — Design

- **Date**: 2026-06-18
- **Status**: Approved (design phase)
- **Author**: brainstormed with user

## 1. Problem

A single logical task often spans multiple repositories (e.g. `front`, `back`,
`cli`). Today each repo gets its own git worktree, which scatters the work:

- The **context** of "what is this task and why" lives nowhere — it is implicit,
  spread across several worktrees.
- **Plan / spec files** scatter across repos.
- Temporary spec files sometimes land **inside the repo working tree** and get
  committed to `develop`, causing contention and confusion ("why is this spec on
  develop, whose task is this?").

The user wants to manage the context of a cross-repo task in **one place**.

## 2. Goals

1. Manage **project-and-feature context as a first-class, task-centric unit**.
2. When starting a feature: create the per-project worktrees, persist and manage
   their state, and keep spec/related files alongside — not inside the repo.
3. When a feature is done: **persist** the worthwhile artifacts durably.
4. A **local folder structure of at most 3 depth** for v1.
5. A **CLI that an AI can use easily** (structured `--json` output), plus a
   companion **skill**.
6. **Session continuity**: a new session (or one resumed after context
   compaction) must rehydrate the full intent — goal, background, plan, and
   progress — from a single command, **never re-inferring the goal from the
   code**.
7. **Goal is the north star**: the single most important artifact to preserve is
   the **goal** — an `objective` plus an explicit, verifiable `done_when`
   completion condition. It is stored as a first-class, structured field (not
   buried in prose), and `ctx` can emit it as native Claude Code `/goal` handoff
   text so a fresh session re-arms `/goal` from `ctx`'s durable copy.

## 3. Non-Goals (v1)

- No central index / cache, no embedded database (filesystem is the source of
  truth).
- No write coupling to external knowledge systems (e.g. OMC wiki). `ctx` is
  self-contained.
- No remote/cloud sync, no multi-user concurrency model.
- No automatic merging or PR creation (out of scope; existing tools handle that).
- No embeddings / vector search, no `ctx know lint`, no auto-surfacing of related
  knowledge in v1. Knowledge retrieval is grep + frontmatter facets over markdown
  (deferred items are listed in §14).

## 4. Key Decisions

| Axis | Decision | Rationale |
|------|----------|-----------|
| Organizing model | **Task-centric** (`Task → Project → Files`) | The cross-repo *task* is the unit that has no home today; making it a first-class citizen is the core fix. |
| Worktree location | **Inside the ctx folder** (`<task>/<project>/wt/`); `spec.md` is a *sibling* of `wt/` | Everything in one place; `spec.md` sitting outside `wt/` is structurally invisible to `git status`, eliminating the "spec on develop" problem by construction. |
| Persistence (knowledge) | On `done`, **consolidate** the task's durable findings into **per-project topic pages** under `~/.ctx/_knowledge/<project>/` (cross-cutting → `_shared/`); the task survives only as `sources:` provenance, never as a frozen task folder. Self-contained (no OMC wiki). | Folders are the right *home* (per-project), topic pages are where reuse *compounds*, and tasks are *provenance not a storage axis* — the consensus of agent-memory, docs-engineering, and PKM research. Avoids the write-only "task snapshot graveyard". |
| Runtime | **Go**, single static binary | Zero runtime deps; fast startup for AI shell calls; stdlib covers fs + `git` exec + JSON; portable `~/.ctx`. |
| Context detection | **CWD auto-detect** via walk-up; `ctx current --json` | Same pattern as git finding `.git`; stateless-recoverable, no flags to memorize — robust for AI agents that lose positional state. |
| Source of truth | **Filesystem** (`task.yaml` per task) + **`git worktree list`** | No index → no drift (the worst failure mode for an AI tool); transparent to humans and AI; backup = copy the folder. |
| CLI framework | **cobra** | De-facto Go standard; subcommands + auto `--help` enable AI self-discovery; pure Go, single binary preserved. |
| Session continuity | **`context.md` as a templated narrative** + **`worklist` in `task.yaml`** + a single **`ctx resume`** entry point | Fixed storage schema makes rehydration reliable (parse, not guess); one command reloads intent+progress+live git state so a fresh session never re-infers the goal from code. |
| Goal model | **Single north-star goal per task**, stored as a structured `task.yaml` field (`objective` + verifiable `done_when`); `ctx goal --handoff` emits native `/goal` text | The goal is the one artifact that can't be reliably re-derived from code; a structured field + completion condition makes it durable and testable, and the handoff bridges to native `/goal`'s in-session enforcement (which is otherwise volatile across compaction). |

## 5. Folder Structure (exactly 3 depth)

```
~/.ctx/                          # CTX_HOME (default; override via $CTX_HOME)
├── add-payment/                 # depth 1: Task
│   ├── task.yaml                #   source of truth (meta)
│   ├── context.md               #   narrative: Background/Plan/Decisions/Journal (goal lives in task.yaml)
│   ├── front/                   # depth 2: Project
│   │   ├── spec.md              # depth 3: ctx-managed spec (OUTSIDE the repo)
│   │   └── wt/                  # depth 3: git worktree of ~/Proj/front
│   └── back/
│       ├── spec.md
│       └── wt/
└── _knowledge/                  # PER-PROJECT persistent knowledge (compounds across tasks)
    ├── index.md                 #   always-read catalog (feed-forward entry point)
    ├── log.md                   #   append-only provenance ledger (1 line / consolidation)
    ├── _shared/                 #   cross-cutting knowledge spanning 2+ projects
    │   └── payment-contract.md
    ├── front/                   #   one folder per project (the "home")
    │   ├── routing.md           #   topic page (frontmatter: project[], category, sources[])
    │   └── gotchas.md
    └── back/
        └── pay-endpoint.md
```

On `ctx done` the task's durable findings are **consolidated into per-project
topic pages** under `_knowledge/<project>/` (or `_knowledge/_shared/` when a page
spans 2+ projects), each stamped with the originating task in its `sources:`
frontmatter; `log.md` gets one line and the active task folder is removed. The
*task* is an active-phase organizing unit; the *knowledge* lives per project and
compounds across tasks (see §9).

The contents of `wt/` are git's domain, not part of the ctx structure, so the
ctx-managed structure stays within 3 depth. `task.yaml` and `context.md` live at
the task level (depth 1).

## 6. Data Model — `task.yaml` (single source of truth)

```yaml
name: add-payment
status: active                   # active | done
created: 2026-06-18T09:00:00Z
description: 결제 기능 추가 (front/back/cli 동시 수정)
goal:                              # the north star — first-class, preserved across sessions
  objective: "결제 기능을 front/back/cli에 일관되게 추가"
  done_when: "결제 E2E 통과 + /pay 200 + lint clean"   # verifiable completion condition
projects:
  - name: front
    repo: /Users/kim/Proj/front
    base: develop                # branch the worktree was created from
    branch: feat/add-payment
    worktree: front/wt           # relative to the task directory
    spec: front/spec.md
    status: in_progress          # planned | in_progress | review | done
  - name: back
    repo: /Users/kim/Proj/back
    base: develop
    branch: feat/add-payment
    worktree: back/wt
    spec: back/spec.md
    status: planned
worklist:                          # task-level checklist (machine-readable progress)
  - id: 1
    text: "front: 결제 폼 UI"
    status: done                   # todo | doing | done
  - id: 2
    text: "back: /pay 엔드포인트"
    status: doing
```

The **existence and current branch** of a worktree are queried from git
(`git worktree list`). `task.yaml` holds the **meta** (status, spec path,
description) plus the **`worklist`** (so `ctx status`/`ctx resume` report precise
progress, e.g. 1/2 done); worktree existence is never duplicated, to avoid drift.

### 6.1 `context.md` — the narrative companion (fixed template)

The **canonical goal lives in `task.yaml`** (§6: structured `objective` +
`done_when`). `context.md` carries the prose around it. `ctx new` scaffolds it
with required headings; `ctx resume` parses by heading and reports a missing or
empty section as `MISSING` rather than silently inferring it — so lost intent is
surfaced, never guessed:

```markdown
# add-payment
## Background    <!-- 맥락, 제약 (왜 이 일이 필요한가) -->
## Plan          <!-- 접근 방식 -->
## Decisions     <!-- append-only: 왜 그렇게 결정했나 -->
## Journal       <!-- `ctx log` 가 날짜와 함께 append -->
```

## 7. Command Surface (v1)

```
# Task lifecycle
ctx new <task> --objective "..." --done-when "..." [-d "desc"] [--background "..."]
    Scaffold task dir + task.yaml (with the goal as a first-class field) +
    context.md. objective/done-when define the task's north-star goal; prompted
    interactively if omitted.

ctx add <project> --repo <path> [--branch <b>] [--base <b>]
    Register a project under the current (or --task) task:
    run `git worktree add <task>/<project>/wt -b <branch> <base>` in the repo,
    scaffold <project>/spec.md, append to task.yaml projects.
    Defaults: branch = feat/<task>; base = repo default branch (override --base).

ctx ls
    List all tasks (status, #projects).

ctx status [--json]
    Inside a task: detailed view. Outside: overview of all tasks.

ctx current [--json]
    CWD-detected {task, project, spec, worktree, status}. Returns {task:null}
    (not an error) when outside any task.

ctx set-status <planned|in_progress|review|done> [--project <p>]
    Update a project's status.

ctx done [<task>] [--force]
    Finalize: for each project verify branch merged / worktree clean
    (refuse by default; --force overrides) → CONSOLIDATE the task's findings into
    per-project topic pages under _knowledge/<project>/ (cross-cutting →
    _shared/), stamping each with sources:{task,when} and appending one line to
    _knowledge/log.md → `git worktree remove` each wt/ → remove the task folder.
    The AI does the distillation (guided by the skill); `ctx know add` is the
    primitive it calls.

# Goal (the north star — the primary thing preserved across sessions)
ctx goal [--json]
    Show the task's goal: objective + done_when completion condition.

ctx goal set [--objective "..."] [--done-when "..."]
    Set or edit the goal (stored as a structured field in task.yaml).

ctx goal --handoff
    Emit native Claude Code /goal handoff text built from the stored goal, so a
    new session can re-arm `/goal <done_when>` itself. ctx = durable home,
    native /goal = in-session enforcement; ctx only prints text and never
    mutates /goal state from the shell.

# Session continuity (the fix for lost intent across sessions)
ctx resume [<task>] [--json]
    Single rehydration entry point. Leads with the GOAL (objective + done_when),
    then context.md Background/Plan, the task.yaml worklist, the derived `next`
    item, recent journal entries, and LIVE git state per project (branch, base,
    dirty, ahead/behind). Run FIRST by a new session; pair with
    `ctx goal --handoff` to re-arm native /goal.

ctx log <message>
    Append a dated entry under context.md `## Journal` (append-only worklog).

ctx task add <text>            Add a worklist item.
ctx task <todo|doing|done> <id>  Set a worklist item's status.
ctx task ls [--json]           List the worklist with progress.

# Knowledge (per-project, compounds across tasks — see §9)
ctx know add --project <p> --topic <t> [--category <c>] [--tags a,b] [--from <file>]
    Create or MERGE into topic page _knowledge/<p>/<t>.md (or _shared/ when
    --project lists 2+). Writes frontmatter facets + a sources: stamp and
    updates index.md + log.md.

ctx know search <query> [--project <p>] [--tag <t>] [--category <c>] [--json]
    Rank topic pages by keyword + facet match (grep + frontmatter; Korean/CJK
    bigram tokenization). Returns snippets + paths; the AI reads + synthesizes.

ctx know index [--json]
    Print _knowledge/index.md (the always-read catalog). Read this FIRST when
    starting related work (feed-forward).

# Helpers
ctx spec [--project <p>]
    Print the spec path/content for the current (or named) project — for AI
    to read/edit.

ctx where
    Print CTX_HOME and the current task path.
```

All **read** commands support `--json` for AI consumption. The `--json` schema is
treated as a **stable contract** and locked with golden tests.

## 8. Data Flow (lifecycle)

```
ctx new add-payment --objective "..." --done-when "..."
   └─ create ~/.ctx/add-payment/{task.yaml (goal = north star), context.md}

ctx add front --repo ~/Proj/front --base develop
   ├─ (in repo) git worktree add ~/.ctx/add-payment/front/wt -b feat/add-payment develop
   ├─ scaffold front/spec.md
   └─ append project to task.yaml

[AI/developer works in wt/, edits spec.md; records progress via
 `ctx task done <id>` and decisions via `ctx log "..."`]

ctx status                 # all projects' state + worklist progress on one screen

# --- new session / after context compaction ---
ctx resume add-payment --json     # leads with GOAL (objective + done_when)
   └─ reload goal + Background/Plan + worklist + `next` + recent journal
      + live git state per project — never re-infer the goal from code
ctx goal --handoff                # re-arm native /goal from ctx's durable copy

ctx done add-payment
   ├─ per project: verify branch merged / worktree clean (refuse unless --force)
   ├─ consolidate findings → _knowledge/<project>/<topic>.md (cross-cutting → _shared/)
   │    each stamped sources:{task: add-payment, when}; append 1 line to log.md
   ├─ git worktree remove each wt/
   └─ remove the active task folder (knowledge now lives per-project)
```

## 9. Persistent Knowledge (`_knowledge/`)

The persistent layer answers a future question: *"what do we already know about
project X (or topic Y)?"* It is organized by the consensus pattern of agent
memory, docs engineering, and PKM research: **folders are the home, facets are
the classifier, tasks are provenance.**

- **Per-project folders = the home.** `_knowledge/<project>/` matches the dominant
  query ("about repo X") and isolates one project's conventions from another's.
- **Topic pages = the unit of persistence.** Within a project, one durable page
  per stable subject (`routing.md`, `gotchas.md`). Repeated work on the same
  subject **merges into the same page** (reconcile + flag contradictions), so
  knowledge compounds instead of piling up as per-task dumps.
- **`_shared/` = cross-cutting home.** A page whose `project:` facet lists 2+
  repos lives in `_knowledge/_shared/` — knowledge sits at the level it applies
  to, never copied downward.
- **Frontmatter facets + provenance** (grep-able, controlled vocab):
  `project: []`, `category: architecture|decision|gotcha|pattern`, `topic`,
  `status: active|superseded`, `sources: [{task, when, pr}]`, `last_reviewed`.
- **Two special files:** `index.md` (a regenerated catalog — the always-read
  feed-forward entry point) and `log.md` (append-only, one line per
  consolidation as `## [date] consolidate | <topic>`, so `grep '^## \[' log.md`
  works).
- **Retrieval = grep + facets** (Korean/CJK bigram tokenization); no database, no
  embeddings in v1.

**`ctx done` consolidates, it does not archive.** It distills the finished task's
durable findings into the relevant topic pages (the AI does the judgment, guided
by the skill; `ctx know add` is the primitive), stamps each with the task as
`sources:` provenance, and appends to `log.md`. The active phase stays
task-centric (correct for in-flight work); only the `done` boundary converts
task → topic.

Deferred to a later version (§14): `ctx know lint` (orphans / stale via
`last_reviewed` cadence / contradiction + supersede), embeddings / hybrid search
past a few hundred pages, and auto-surfacing related knowledge at `ctx new`.

## 10. Companion Skill & Discovery (Goal 5)

How an AI learns to use `ctx` is **layered** — each layer has one job, and the
command reference is never duplicated, so it can't drift from the code:

1. **Global `~/.claude/CLAUDE.md` pointer (discovery)** — 1–2 lines: "`ctx` is a
   cross-repo context manager; run `ctx --help`; use it for multi-repo tasks."
   Always loaded, negligible cost, works from any worktree.
2. **`ctx --help` / `ctx <cmd> --help` (reference, single source of truth)** —
   cobra auto-generates it from the code, so the *how* never drifts; zero context
   cost until the agent looks.
3. **`context-manager` skill (behavior / when / hard rules)** — lazy-loaded; it
   teaches *when and why*, not the full command list:
   - **Session start**: run `ctx resume --json` FIRST; re-arm the native goal via
     `ctx goal --handoff`. **Never re-infer the goal from code.**
   - **Goal capture first** at `ctx new` (`objective` + verifiable `done_when`).
   - **Keep it live**: update the worklist (`ctx task ...`) and journal (`ctx log`).
   - **Knowledge**: when starting work, read `ctx know index` / `ctx know search`
     first (feed-forward); at `ctx done`, consolidate findings into per-project
     topic pages via `ctx know add` — don't leave learnings in chat history.
   - For exact flags, defer to `ctx <cmd> --help`.

The skill ships in the repo under `skill/` with install guidance (symlink/copy).
`ctx schema --json` (machine-readable command/flag/JSON-contract dump) is a
future add for complex/MCP scenarios (§14).

## 11. Error Handling & Safety

- Worktree branch already exists → offer reuse or a clear, actionable error.
- `done` with uncommitted/unmerged changes → **refused by default**; requires
  `--force` (a direct safety net for the user's "contention" pain).
- CWD outside any task → `ctx current` returns `{task:null}`, never crashes.
- `task.yaml` writes are **atomic** (write temp + rename) to prevent corruption.
- In `--json` mode, all errors return a structured `{error, code}`.

## 12. Go Project Structure

```
context-manager/
├── cmd/ctx/main.go
├── internal/
│   ├── store/        # CTX_HOME resolution, walk-up detection, scan
│   ├── task/         # task.yaml model + load/save (atomic)
│   ├── worktree/     # git worktree wrappers (os/exec)
│   ├── knowledge/    # topic pages (frontmatter merge), index.md/log.md, search
│   ├── resume/       # context.md parsing + rehydration bundle assembly
│   ├── goal/         # goal field model + /goal handoff text builder
│   ├── cli/          # cobra command definitions
│   └── output/       # json/text renderers
├── skill/            # the context-manager skill
└── go.mod
```

## 13. Testing Strategy

- **Unit**: walk-up path resolution, `task.yaml` round-trip, status derivation,
  `context.md` heading parsing (incl. `MISSING`-section handling), worklist
  progress + `next` derivation.
- **Integration**: temp dirs + real `git` for worktree add/remove and for
  `ctx resume` live git state (branch / dirty / ahead-behind), table-driven.
- **Knowledge**: topic-page frontmatter merge (new vs. existing; `_shared/`
  promotion when `project:` lists ≥2), `index.md`/`log.md` generation, and
  `ctx know search` ranking incl. Korean/CJK bigram tokenization.
- **Golden**: `--json` schema stability for `status`, `current`, `resume`,
  `goal`, and `know search`; plus the `ctx goal --handoff` text format (AI
  depends on these contracts).

## 14. Future / Out of Scope (noted, not built in v1)

- `ctx know lint`: orphan pages, stale entries (`last_reviewed` cadence), and
  contradiction detection + `status: superseded` chains.
- Embeddings / hybrid search (BM25 + vector) once `_knowledge/` exceeds a few
  hundred pages; markdown + grep is the v1 path.
- Auto-surfacing related knowledge at `ctx new` / `ctx add` (feed-forward beyond
  "read `index.md` first").
- `ctx schema --json` for machine-readable command/flag discovery (MCP bridge).
- Cross-project status rollup views (`ctx status --by-project`).
- Config file for default base branch / branch naming per repo.
