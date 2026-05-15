#!/usr/bin/env bash
#
# scripts/doc-sweep.sh — automate the mechanical checks from
# `docs/CONVENTIONS.md` §12.6 ("Doc sweep at ship time") and §13 ("Per-spec
# tracking notes").
#
# The §12.6 table is the source of truth; this script only covers
# the checks that can be done mechanically without reading prose.
# It is NOT a substitute for the human walk-through at ship time.
#
# Exit codes:
#   0  — all checks passed (or all findings were advisory under --warn)
#   1  — at least one strict check failed
#
# Modes:
#   --strict   (default)  Fail on any finding from the always-on checks.
#   --warn                Print findings, exit 0.
#   --all                 Also run advisory checks (palette ID coverage).
#                         Combine with --strict to fail on those too.
#
# Always-on checks:
#   1. Plan-file existence  — every docs/specs/NN-<title>/spec.md has a
#      docs/specs/NN-<title>/plan.md (`docs/CONVENTIONS.md` §13, the v0.12.0 lesson).
#   2. Shipped consistency  — every spec with a `**Shipped:** vX.Y.Z`
#      line has its plan's `## Status` block start with `done`.
#
# Advisory checks (--all):
#   3. Palette ID coverage  — every `ID: "<name>"` literal in
#      internal/ui/palette_commands.go appears verbatim in
#      docs/user/reference.md. Reference.md uses user-facing labels
#      that may not match the ID; treat misses as nudges, not bugs.
#
# Future work (not yet implemented):
#   - Diff-based key binding coverage (new KeyMap field → reference.md).
#   - CLI subcommand coverage (cmd/inkwell/ → reference.md).
#   - PRD §10 row updated for each shipped spec's version.
#   - ROADMAP.md status cell updated for each shipped spec.

set -uo pipefail

cd "$(dirname "$0")/.."

mode_strict=1
mode_all=0
for arg in "$@"; do
  case "$arg" in
    --strict) mode_strict=1 ;;
    --warn)   mode_strict=0 ;;
    --all)    mode_all=1 ;;
    -h|--help)
      sed -n '3,40p' "$0"
      exit 0
      ;;
    *)
      echo "unknown flag: $arg" >&2
      exit 2
      ;;
  esac
done

bold=$(tput bold 2>/dev/null || echo "")
red=$(tput setaf 1 2>/dev/null || echo "")
yellow=$(tput setaf 3 2>/dev/null || echo "")
green=$(tput setaf 2 2>/dev/null || echo "")
reset=$(tput sgr0 2>/dev/null || echo "")

findings=0
advisories=0

step()    { printf "\n${bold}== %s ==${reset}\n" "$*"; }
finding() { printf "${red}✗${reset} %s\n" "$*"; findings=$((findings+1)); }
advise()  { printf "${yellow}!${reset} %s\n" "$*"; advisories=$((advisories+1)); }
ok()      { printf "${green}✓${reset} %s\n" "$*"; }

# ----------------------------------------------------------------
# Check 1: plan-file existence
# ----------------------------------------------------------------
step "1/3 plan-file existence (§13)"
missing_plans=0
for spec in docs/specs/[0-9]*/spec.md; do
  dir=$(dirname "$spec")
  num=$(basename "$dir" | cut -d- -f1)
  plan="$dir/plan.md"
  if [ ! -f "$plan" ]; then
    finding "spec ${num} (${spec}) has no $plan"
    missing_plans=$((missing_plans+1))
  fi
done
if [ "$missing_plans" -eq 0 ]; then
  ok "every spec has a plan file"
fi

# ----------------------------------------------------------------
# Check 2: shipped consistency
# ----------------------------------------------------------------
step "2/3 shipped consistency (§12.6)"
inconsistent=0
for spec in docs/specs/[0-9]*/spec.md; do
  dir=$(dirname "$spec")
  num=$(basename "$dir" | cut -d- -f1)
  plan="$dir/plan.md"
  if ! grep -q '^\*\*Shipped:\*\*' "$spec"; then
    continue
  fi
  if [ ! -f "$plan" ]; then
    continue   # already flagged by check 1
  fi
  # The line immediately after `## Status` should start with `done`.
  status_line=$(awk '/^## Status[[:space:]]*$/{getline; print; exit}' "$plan")
  if ! echo "$status_line" | grep -qiE '^done\b'; then
    finding "spec ${num}: shipped per spec but plan status is: ${status_line:-<empty>}"
    inconsistent=$((inconsistent+1))
  fi
done
if [ "$inconsistent" -eq 0 ]; then
  ok "every shipped spec has plan status 'done'"
fi

# ----------------------------------------------------------------
# Check 3: palette ID coverage (advisory)
# ----------------------------------------------------------------
step "3/3 palette ID coverage in reference.md (--all only)"
if [ "$mode_all" -eq 1 ]; then
  palette_src="internal/ui/palette_commands.go"
  ref="docs/user/reference.md"
  if [ ! -f "$palette_src" ] || [ ! -f "$ref" ]; then
    advise "skipping — $palette_src or $ref missing"
  else
    missing_ids=0
    grep -oE 'ID: "[a-z_]+"' "$palette_src" \
      | sed -E 's/ID: "(.*)"/\1/' | sort -u \
      | while IFS= read -r id; do
          if ! grep -qw "$id" "$ref"; then
            echo "$id"
          fi
        done > /tmp/doc-sweep-missing-ids.$$
    if [ -s /tmp/doc-sweep-missing-ids.$$ ]; then
      while IFS= read -r id; do
        if [ "$mode_strict" -eq 1 ]; then
          finding "palette ID '${id}' not in $ref"
        else
          advise "palette ID '${id}' not in $ref"
        fi
      done < /tmp/doc-sweep-missing-ids.$$
    else
      ok "every palette ID appears in $ref"
    fi
    rm -f /tmp/doc-sweep-missing-ids.$$
  fi
else
  ok "skipped (run with --all to enable)"
fi

# ----------------------------------------------------------------
# Summary
# ----------------------------------------------------------------
echo
if [ "$findings" -eq 0 ] && [ "$advisories" -eq 0 ]; then
  printf "${green}${bold}doc-sweep: clean${reset}\n"
  exit 0
fi
if [ "$findings" -eq 0 ]; then
  printf "${yellow}${bold}doc-sweep: %d advisory finding(s) — exit 0${reset}\n" "$advisories"
  exit 0
fi
if [ "$mode_strict" -eq 1 ]; then
  printf "${red}${bold}doc-sweep: %d finding(s), %d advisory — exit 1${reset}\n" "$findings" "$advisories"
  exit 1
fi
printf "${yellow}${bold}doc-sweep: %d finding(s), %d advisory — exit 0 (--warn)${reset}\n" "$findings" "$advisories"
exit 0
