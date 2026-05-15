#!/usr/bin/env bash
# scripts/ai-fuzz.sh — produce an exploratory-fuzz corpus for Claude-Code review.
#
# This script does NOT call any LLM. It runs the Go harness that drives
# the TUI with random keystrokes, captures the rendered framebuffer after
# every action, and writes artifacts to .context/ai-fuzz/run-<unix-ts>/.
# A Claude Code session then reads REVIEW.md + the per-step files and
# acts as the oracle.
#
# Usage:
#   scripts/ai-fuzz.sh             # 8-step smoke (default)
#   scripts/ai-fuzz.sh 30          # 30-step run
#   STEPS=50 scripts/ai-fuzz.sh    # same, via env
#   SEED=42 scripts/ai-fuzz.sh     # reproducible run
#
# On success the run dir is printed to stdout (also captured in the test
# log) so the next agent step can `cat <run-dir>/REVIEW.md`.

set -euo pipefail

cd "$(dirname "$0")/.."

STEPS="${1:-${STEPS:-8}}"
SEED="${SEED:-}"

env_args=(INKWELL_FUZZ_STEPS="${STEPS}")
[[ -n "${SEED}" ]] && env_args+=(INKWELL_FUZZ_SEED="${SEED}")

LOG_FILE="$(mktemp -t ai-fuzz.XXXXXX.log)"
trap 'rm -f "${LOG_FILE}"' EXIT

set +e
env "${env_args[@]}" \
  go test -tags='e2e aifuzz' -run='^TestAIFuzzExplore$' -count=1 -v -timeout=5m \
  ./internal/ui 2>&1 | tee "${LOG_FILE}"
test_rc="${PIPESTATUS[0]}"
set -e

RUN_DIR="$(grep -oE 'ai-fuzz run dir: [^ ]+' "${LOG_FILE}" | head -1 | awk '{print $NF}' || true)"

echo
if [[ -z "${RUN_DIR}" || ! -d "${RUN_DIR}" ]]; then
  echo "ai-fuzz: run dir not found in test output (test rc=${test_rc})" >&2
  exit "${test_rc}"
fi

echo "ai-fuzz: ${STEPS} steps written to ${RUN_DIR}"
echo "next: read ${RUN_DIR}/REVIEW.md (and the diffs/frames it points at)"
exit "${test_rc}"
