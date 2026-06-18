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
