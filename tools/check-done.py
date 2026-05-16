#!/usr/bin/env python3
"""Mechanical termination gate for the work-loop (docs/CONVENTIONS.md §12.9).

Reads docs/specs/<feature>/state.json and exits non-zero when the loop
should stop:

  - iteration_count >= max_iterations          (§12.1: 8-iteration cap)
  - fingerprint stasis in REVIEW phase         (§12.8: same findings twice)

Usage:
    python3 tools/check-done.py <path-to-state.json> --phase <plan|execute|review>

Exits 0 to continue, 1 with a one-line reason to stop. The skill (and a
human reading the message) decides what "stop" means: re-plan, surface
to a human, ship.

Intentionally narrow. inkwell uses make targets / CI for the heavy
gates; this script only enforces the two non-prose loop-termination
rules that prose can't catch.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


def _fail(reason: str) -> int:
    print(reason, file=sys.stderr)
    return 1


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("state_path", type=Path)
    parser.add_argument(
        "--phase",
        required=True,
        choices=["plan", "execute", "review"],
        help="Loop phase the caller is in; some checks are phase-scoped.",
    )
    args = parser.parse_args(argv)

    if not args.state_path.exists():
        return _fail(f"state file not found: {args.state_path}")

    try:
        state = json.loads(args.state_path.read_text())
    except json.JSONDecodeError as exc:
        return _fail(f"state file is not valid JSON ({exc}): {args.state_path}")

    iteration = int(state.get("iteration_count", 0))
    max_iter = int(state.get("max_iterations", 8))

    if iteration >= max_iter:
        return _fail(
            f"iteration cap hit: {iteration} >= {max_iter} (§12.1). "
            "Stop and ask the user."
        )

    if args.phase == "review":
        prev = set(state.get("previous_finding_fingerprints") or [])
        cur = set(state.get("finding_fingerprints") or [])
        # Stasis = identical sets. A strict subset means one or more
        # findings were resolved this pass — that's progress, not
        # stasis, even if the survivors persist.
        if cur and prev and cur == prev:
            return _fail(
                "fingerprint stasis: every finding from this review pass "
                "also appeared in the previous one, with nothing resolved "
                "(§12.8). Stop and re-plan; spinning a third pass on the "
                "same findings is how loops burn money for no signal."
            )

    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
