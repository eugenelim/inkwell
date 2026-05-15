#!/usr/bin/env bash
#
# scripts/lint-agents-md.sh — drift detection across AGENTS.md files.
#
# Inkwell has one canonical entry-point AGENTS.md at the repo root
# (symlinked from CLAUDE.md) plus per-package AGENTS.md under
# internal/. This script catches the most common drift modes:
#
#   1. Root AGENTS.md exceeds the slim-file budget (~200 lines —
#      docs/CONVENTIONS.md §14 keeps the entry point navigational).
#   2. CLAUDE.md is no longer a symlink to AGENTS.md (the sed
#      pass that clobbered it during step 1 + step 3a; this is
#      institutional memory now).
#   3. Per-package AGENTS.md cites "root §N" instead of pointing at
#      docs/CONVENTIONS.md §N (the load-bearing anchor file).
#   4. Any AGENTS.md cites CLAUDE.md §N (those §-anchors live in
#      docs/CONVENTIONS.md now, not in AGENTS.md or CLAUDE.md).
#   5. Per-package AGENTS.md references a package directory that
#      no longer exists (rename detection).
#
# Exit 0 = clean. Non-zero = at least one finding.
#
# Designed to run locally and in CI (always-checks.yml).

set -euo pipefail

cd "$(dirname "$0")/.."

findings=0
finding() { printf "  ✗ %s\n" "$*"; findings=$((findings + 1)); }
ok()      { printf "  ✓ %s\n" "$*"; }
step()    { printf "\n== %s ==\n" "$*"; }

ROOT_BUDGET=220   # generous over the ~150-line target; bumps as the slim file grows

# ----------------------------------------------------------------
# Check 1: root AGENTS.md slim-file budget
# ----------------------------------------------------------------
step "1/5 root AGENTS.md slim-file budget"
lines=$(wc -l < AGENTS.md | tr -d ' ')
if [ "$lines" -gt "$ROOT_BUDGET" ]; then
  finding "AGENTS.md has $lines lines, budget is $ROOT_BUDGET. Move detail into docs/CONVENTIONS.md."
else
  ok "AGENTS.md $lines lines (budget $ROOT_BUDGET)"
fi

# ----------------------------------------------------------------
# Check 2: CLAUDE.md symlink intact
# ----------------------------------------------------------------
step "2/5 CLAUDE.md → AGENTS.md symlink"
if [ ! -L CLAUDE.md ]; then
  finding "CLAUDE.md is not a symlink (was clobbered by perl -i in steps 1 and 3a)."
elif [ "$(readlink CLAUDE.md)" != "AGENTS.md" ]; then
  finding "CLAUDE.md symlink points to '$(readlink CLAUDE.md)', expected 'AGENTS.md'."
else
  ok "CLAUDE.md → AGENTS.md"
fi

# ----------------------------------------------------------------
# Check 3: per-package AGENTS.md "root §N" → docs/CONVENTIONS.md §N
# ----------------------------------------------------------------
step "3/5 per-package AGENTS.md uses absolute §N cites"
stale=$(grep -rnE 'root §[0-9]' internal/*/AGENTS.md 2>/dev/null || true)
if [ -n "$stale" ]; then
  while IFS= read -r line; do
    finding "$line   → rewrite to \`docs/CONVENTIONS.md\` §N"
  done <<< "$stale"
else
  ok "no \"root §N\" cites in per-package AGENTS.md"
fi

# ----------------------------------------------------------------
# Check 4: no CLAUDE.md §N anchors anywhere
# (anchors live in docs/CONVENTIONS.md now)
# ----------------------------------------------------------------
step "4/5 no \`CLAUDE.md §N\` cites in repo"
stale=$(grep -rnE 'CLAUDE\.md\s*(//|#)?\s*§[0-9]' --include='*.md' --include='*.go' --include='*.sh' --include='*.yml' --include='*.yaml' --include='*.toml' --include='Makefile' . 2>/dev/null || true)
# Filter out the line in docs/CONVENTIONS.md that documents the symlink
stale=$(echo "$stale" | grep -v 'docs/CONVENTIONS.md:.*CLAUDE\.md' || true)
if [ -n "$stale" ]; then
  echo "$stale" | while IFS= read -r line; do
    finding "$line   → rewrite to \`docs/CONVENTIONS.md\` §N"
  done
else
  ok "no \`CLAUDE.md §N\` cites"
fi

# ----------------------------------------------------------------
# Check 5: per-package AGENTS.md targets exist
# ----------------------------------------------------------------
step "5/5 per-package AGENTS.md packages exist"
missing=0
for f in internal/*/AGENTS.md; do
  pkg=$(dirname "$f")
  if [ ! -d "$pkg" ]; then
    finding "$f targets non-existent package $pkg"
    missing=$((missing + 1))
  fi
done
if [ "$missing" -eq 0 ]; then
  ok "$(ls internal/*/AGENTS.md 2>/dev/null | wc -l | tr -d ' ') per-package AGENTS.md files, all live"
fi

# ----------------------------------------------------------------
echo
if [ "$findings" -eq 0 ]; then
  echo "lint-agents-md: clean"
  exit 0
else
  echo "lint-agents-md: $findings finding(s)"
  exit 1
fi
