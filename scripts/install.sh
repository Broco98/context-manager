#!/usr/bin/env bash
set -euo pipefail

# Install ctx and make it runnable from any shell.
# Idempotent: safe to re-run; the PATH line is written at most once.

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# 1. Preconditions. ctx is built with the Go toolchain and shells out to git at
#    runtime, so both must be present.
command -v go  >/dev/null || { echo "error: 'go' not found on PATH" >&2; exit 1; }
command -v git >/dev/null || { echo "error: 'git' not found on PATH (ctx needs it at runtime)" >&2; exit 1; }

# 2. Install. `go install` is the canonical way to place a Go CLI on disk: it
#    lands in GOBIN, or GOPATH/bin when GOBIN is unset. CGO_ENABLED=0 enforces
#    the pure-Go static build this project requires (global constraint). `go -C`
#    runs from the module root so the relative ./cmd/ctx package resolves.
BINDIR="$(go env GOBIN)"
[ -n "$BINDIR" ] || BINDIR="$(go env GOPATH)/bin"
CGO_ENABLED=0 go -C "$ROOT" install ./cmd/ctx
echo "installed: $BINDIR/ctx"

# 3. Ensure BINDIR is on PATH via the shell's interactive rc file, chosen by the
#    user's shell. Anything exotic falls back to a manual hint.
case "$(basename "${SHELL:-}")" in
  zsh)  PROFILE="$HOME/.zshrc" ;;
  bash) PROFILE="$HOME/.bashrc" ;;
  *)    PROFILE="" ;;
esac

if printf '%s' ":$PATH:" | grep -qF ":$BINDIR:"; then
  echo "PATH: already includes $BINDIR"
elif [ -z "$PROFILE" ]; then
  echo "note: add this to your shell profile manually:"
  echo "  export PATH=\"$BINDIR:\$PATH\""
elif grep -qsF "# Added by ctx installer" "$PROFILE"; then
  echo "PATH: $PROFILE already updated (restart shell or 'source $PROFILE')"
else
  printf '\n# Added by ctx installer\nexport PATH="%s:$PATH"\n' "$BINDIR" >> "$PROFILE"
  echo "PATH: added $BINDIR to $PROFILE (restart shell or 'source $PROFILE')"
fi

# 4. Prove the installed binary actually runs.
"$BINDIR/ctx" --help >/dev/null
echo "OK — open a new terminal (or source the profile above), then: ctx --help"

# 5. Point the user at the agent-integration step. We do NOT auto-run it: writing
#    into ~/.claude / ~/.codex is an explicit user choice, not an install side effect.
echo
echo "next: run 'ctx skill install' to set up the companion skill + discovery"
echo "      pointer for your coding agents (Claude Code / Codex)."
