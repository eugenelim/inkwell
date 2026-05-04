#!/usr/bin/env bash
# check-no-mail-send.sh — CI lint guard for spec 15 / PRD §3.1 §8.
#
# Inkwell must never REQUEST Mail.Send. The app creates drafts only;
# the user sends from native Outlook. This script rejects any Go
# source that passes "Mail.Send" as a scope string (e.g., to an OAuth
# or Graph call), which would be a sign someone accidentally wired
# send permission.
#
# Allowed: comment-only mentions inside Go files (grep excludes those).
# Allow-list Go files:
#   internal/auth/scopes.go — lists GRANTED scopes (Mail.Send absent)
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

# Search only Go source files; ignore pure-comment lines (// ...).
# A scope string in real code would appear as a quoted string literal
# like "Mail.Send" or `Mail.Send` — match those patterns.
HITS=$(grep -rn \
  --include='*.go' \
  -E '"Mail\.Send"|`Mail\.Send`' \
  --exclude='scopes.go' \
  . 2>/dev/null || true)

if [ -n "$HITS" ]; then
  echo "ERROR: Mail.Send scope string found in Go source outside scopes.go:" >&2
  echo "$HITS" >&2
  echo "" >&2
  echo "inkwell never requests Mail.Send — drafts only (PRD §3.1 / spec 15 §8)." >&2
  echo "Remove the scope or add the file to the allow-list with a justification comment." >&2
  exit 1
fi

echo "check-no-mail-send: OK"
