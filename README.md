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
    ctx know add --project front --topic pay-endpoint --source-task add-payment
                               # consolidate findings (required before done)
    ctx done                   # finalize (refuses dirty/unmerged worktrees)

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
