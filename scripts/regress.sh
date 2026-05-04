#!/usr/bin/env bash
#
# scripts/regress.sh — full local regression suite.
#
# Runs every gate from CLAUDE.md §5.6 in order. Exits non-zero on any
# failure. CLAUDE.md §5.8 mandates running this after every substantial
# change AND before tagging a release.
#
# Why a script and not just `make test test-e2e test-bench`? Because the
# bench step has its own quirks (skips under -race per the test-bench
# Makefile target's comment) and the budget gate is gated separately.
# A single command keeps the discipline simple to follow.

set -euo pipefail

cd "$(dirname "$0")/.."

bold=$(tput bold 2>/dev/null || echo "")
green=$(tput setaf 2 2>/dev/null || echo "")
red=$(tput setaf 1 2>/dev/null || echo "")
reset=$(tput sgr0 2>/dev/null || echo "")

step() { printf "\n${bold}== %s ==${reset}\n" "$*"; }
ok()   { printf "${green}✓${reset} %s\n" "$*"; }
fail() { printf "${red}✗ %s${reset}\n" "$*"; exit 1; }

step "0/6 Mail.Send scope guard"
bash "$(dirname "$0")/check-no-mail-send.sh" || fail "Mail.Send scope guard"
ok "Mail.Send guard clean"

step "1/6 gofmt -s (formatting)"
diff_files=$(gofmt -s -l .)
if [ -n "$diff_files" ]; then
  echo "$diff_files"
  fail "gofmt -s found unformatted files; run 'make fmt'"
fi
ok "all files formatted"

step "2/6 go vet ./..."
go vet ./... || fail "go vet"
ok "vet clean"

step "3/6 go build ./..."
go build ./... || fail "build"
ok "build clean"

step "4/6 go test -race ./... (unit + dispatch)"
go test -race -timeout 120s ./... || fail "race tests"
ok "race tests green"

step "5/6 go test -tags=e2e ./... (TUI e2e)"
go test -tags=e2e -timeout 120s ./... || fail "e2e tests"
ok "e2e tests green"

# Integration tag is reserved for tests that need recorded fixtures.
# Run if any *_test.go uses the integration tag.
if grep -rl "//go:build integration" --include="*.go" . >/dev/null 2>&1; then
  step "6a/6 go test -tags=integration ./..."
  go test -tags=integration -timeout 120s ./... || fail "integration tests"
  ok "integration tests green"
fi

step "6/6 go test -bench=. -benchmem -run=^$ ./... (benches)"
go test -bench=. -benchmem -run='^$' -timeout 600s ./... || fail "benchmarks"
ok "benchmarks within budget"

printf "\n${bold}${green}All regression gates green.${reset}\n"
