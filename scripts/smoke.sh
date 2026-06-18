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
