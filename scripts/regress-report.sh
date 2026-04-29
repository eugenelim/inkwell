#!/usr/bin/env bash
#
# scripts/regress-report.sh — per-feature regression report.
#
# Runs each Go package's tests in isolation, records pass/fail and
# elapsed time, and writes a Markdown report under ./reports/. The
# reports/ directory is .gitignored — these reports are local-only
# evidence of "I ran the suite before tagging," NOT documentation
# that gets shipped.
#
# Usage:
#   make regress-report
#   ./scripts/regress-report.sh [--tags=e2e,integration]
#
# Output: reports/regression-YYYY-MM-DD-HHMMSS.md
#
# Why a per-feature report and not just `go test ./...`? Because when
# the suite goes red on a release-prep run, "internal/store failed in
# 0.2s" tells the reader far more than 300 lines of -v output. The
# report groups packages under spec/feature labels (CLAUDE.md §14) so
# a regression points at the right spec immediately.

set -uo pipefail

cd "$(dirname "$0")/.."

mkdir -p reports

stamp=$(date -u +%Y-%m-%d-%H%M%S)
out="reports/regression-${stamp}.md"

bold=$(tput bold 2>/dev/null || echo "")
green=$(tput setaf 2 2>/dev/null || echo "")
red=$(tput setaf 1 2>/dev/null || echo "")
yellow=$(tput setaf 3 2>/dev/null || echo "")
reset=$(tput sgr0 2>/dev/null || echo "")

# package → feature label. Update when a new spec lands; an entry
# missing from this table falls back to the bare package path.
feature_of() {
  case "$1" in
    github.com/eugenelim/inkwell/internal/auth)         echo "Spec 01 — Authentication" ;;
    github.com/eugenelim/inkwell/internal/store)        echo "Spec 02 — Local cache schema" ;;
    github.com/eugenelim/inkwell/internal/sync)         echo "Spec 03 — Sync engine" ;;
    github.com/eugenelim/inkwell/internal/ui)           echo "Spec 04 — TUI shell" ;;
    github.com/eugenelim/inkwell/internal/render)       echo "Spec 05 — Message rendering" ;;
    github.com/eugenelim/inkwell/internal/search)       echo "Spec 06 — Hybrid search" ;;
    github.com/eugenelim/inkwell/internal/action)       echo "Spec 07/09 — Triage actions / batch" ;;
    github.com/eugenelim/inkwell/internal/pattern)      echo "Spec 08 — Pattern language" ;;
    github.com/eugenelim/inkwell/internal/graph)        echo "Spec 03/09 — Graph client / batch" ;;
    github.com/eugenelim/inkwell/internal/savedsearch)  echo "Spec 11 — Saved searches" ;;
    github.com/eugenelim/inkwell/internal/settings)     echo "Spec 13 — Mailbox settings" ;;
    github.com/eugenelim/inkwell/internal/cli)          echo "Spec 14 — CLI mode" ;;
    github.com/eugenelim/inkwell/internal/compose)      echo "Spec 15 — Compose / reply" ;;
    github.com/eugenelim/inkwell/internal/log)          echo "Cross-cutting — privacy / redaction" ;;
    github.com/eugenelim/inkwell/internal/config)       echo "Cross-cutting — configuration" ;;
    github.com/eugenelim/inkwell/internal/savedsearch)  echo "Spec 11 — Saved searches" ;;
    github.com/eugenelim/inkwell/cmd/inkwell)           echo "CLI binary integration" ;;
    *) echo "$1" ;;
  esac
}

# Build the package list (skip the ones with no tests).
pkgs=$(go list ./... 2>/dev/null)
if [ -z "$pkgs" ]; then
  echo "${red}go list returned no packages${reset}" >&2
  exit 1
fi

# Aggregate results in a temp dir; we render the markdown in one pass
# at the end so a single terminal summary lines up with the file.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

total_pass=0
total_fail=0
total_skip=0

printf "${bold}Per-feature regression report → %s${reset}\n" "$out"
printf "%-58s %-8s %s\n" "PACKAGE" "STATUS" "DURATION"
printf "%-58s %-8s %s\n" "-------" "------" "--------"

for pkg in $pkgs; do
  start=$(date +%s)
  log_file="$tmp/$(echo "$pkg" | tr '/' '_').log"
  # -count=1 disables the build cache so each invocation actually runs.
  # Failing here is recorded but does not abort the loop — the report
  # is meant to capture every package's status in one pass.
  if go test -race -count=1 -timeout 120s "$pkg" >"$log_file" 2>&1; then
    rc=0
  else
    rc=$?
  fi
  end=$(date +%s)
  dur=$(( end - start ))

  status="?"
  color="$reset"
  if grep -q "^?   .* \[no test files\]" "$log_file"; then
    status="SKIP"
    color="$yellow"
    total_skip=$((total_skip + 1))
  elif [ $rc -eq 0 ]; then
    status="PASS"
    color="$green"
    total_pass=$((total_pass + 1))
  else
    status="FAIL"
    color="$red"
    total_fail=$((total_fail + 1))
  fi

  printf "%-58s ${color}%-8s${reset} %ds\n" "${pkg#github.com/eugenelim/inkwell/}" "$status" "$dur"

  # Stash a per-package record for the markdown render.
  echo "$pkg|$status|$dur|$log_file" >>"$tmp/results"
done

# Render the markdown report grouped by feature.
{
  echo "# Regression report"
  echo
  echo "- Stamp: \`${stamp}\`"
  echo "- Host: \`$(uname -srm)\`"
  echo "- Go: \`$(go version | awk '{print $3}')\`"
  echo "- Branch: \`$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)\`"
  echo "- HEAD: \`$(git rev-parse --short HEAD 2>/dev/null || echo unknown)\`"
  echo
  echo "## Summary"
  echo
  echo "| Pass | Fail | Skip |"
  echo "| --- | --- | --- |"
  echo "| ${total_pass} | ${total_fail} | ${total_skip} |"
  echo
  echo "## By feature"
  echo

  # Group by feature label, sorted feature-wise.
  while IFS='|' read -r pkg status dur log; do
    label=$(feature_of "$pkg")
    echo "${label}|${pkg}|${status}|${dur}|${log}"
  done <"$tmp/results" | sort -t'|' -k1,1 >"$tmp/grouped"

  current=""
  while IFS='|' read -r label pkg status dur log; do
    if [ "$label" != "$current" ]; then
      printf "\n### %s\n\n" "$label"
      printf "| Package | Status | Duration |\n"
      printf "| --- | --- | --- |\n"
      current="$label"
    fi
    printf "| \`%s\` | %s | %ds |\n" "${pkg#github.com/eugenelim/inkwell/}" "$status" "$dur"
  done <"$tmp/grouped"

  echo
  if [ "$total_fail" -gt 0 ]; then
    echo "## Failure logs"
    echo
    while IFS='|' read -r pkg status dur log; do
      if [ "$status" = "FAIL" ]; then
        echo "### \`${pkg#github.com/eugenelim/inkwell/}\`"
        echo
        echo '```'
        cat "$log"
        echo '```'
        echo
      fi
    done <"$tmp/results"
  fi
} >"$out"

printf "\n${bold}Report:${reset} %s\n" "$out"
printf "${bold}Pass:${reset} %d  ${bold}Fail:${reset} %d  ${bold}Skip:${reset} %d\n" "$total_pass" "$total_fail" "$total_skip"

if [ "$total_fail" -gt 0 ]; then
  exit 1
fi
exit 0
