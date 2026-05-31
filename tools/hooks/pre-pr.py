#!/usr/bin/env python3
"""Pre-PR hook (inkwell): runs the project's mechanical gates against the
working tree and exits non-zero on the first failure, so a contributor
can't open a PR whose artifacts or specs are inconsistent with the
conventions.

Adapted from the agent-ready-repo `core` pack's pre-pr body. The upstream
version ran the catalogue's *own* artifact linters (`tools/lint-*.py`),
none of which ship into an adopter repo. This inkwell port instead drives
inkwell's real gates:

  - make doc-sweep   — docs/CONVENTIONS.md §12.6 mechanical checks
                       (plan files present, shipped-consistency, etc.)
  - make regress     — docs/CONVENTIONS.md §5.8 full regression suite
                       (skipped when PRE_PR_SKIP_REGRESS=1, for fast
                        artifact-only runs)
  - loop-cohort.py check — docs/CONVENTIONS.md §12.9 loop-termination gate,
                       run per docs/specs/*/ with a state.json in the
                       implement and review phases (iteration cap +
                       fingerprint stasis)

Wiring: not auto-wired. To gate PR creation, symlink or call this from a
git pre-push hook (`.git/hooks/pre-push`), or invoke it by hand before
`gh pr create`. (Claude Code has no "pre-pr" event; the session-start hook
is the only one wired in .claude/settings.local.json.)

Output shape mirrors the upstream body: `pre-pr: ✓ <label>` /
`pre-pr: ✖ <label> failed` / `pre-pr: all checks passed`.
"""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path


def _repo_root() -> Path:
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            capture_output=True, text=True, check=False,
        )
        if result.returncode == 0 and result.stdout.strip():
            return Path(result.stdout.strip())
    except FileNotFoundError:
        pass
    return Path.cwd()


def _run(label: str, argv: list[str]) -> None:
    """Run *argv*; on non-zero exit, surface the child output, print the
    failure line, and sys.exit(1). On success, print the success line."""
    result = subprocess.run(argv, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        if result.stdout:
            sys.stdout.write(result.stdout)
        if result.stderr:
            sys.stderr.write(result.stderr)
        print(f"pre-pr: ✖ {label} failed", file=sys.stderr)
        sys.exit(1)
    print(f"pre-pr: ✓ {label}")


def main() -> int:
    repo_root = _repo_root()
    os.chdir(repo_root)

    py = sys.executable  # use parent's interpreter for child checks

    _run("doc-sweep (§12.6)", ["make", "doc-sweep"])

    if os.environ.get("PRE_PR_SKIP_REGRESS") == "1":
        print("pre-pr: (PRE_PR_SKIP_REGRESS=1 — skipping make regress)")
    else:
        _run("regress (§5.8)", ["make", "regress"])

    loop_cohort = Path(".claude/skills/work-loop/scripts/loop-cohort.py")
    state_files = sorted(Path("docs/specs").glob("*/state.json"))
    if not state_files:
        print("pre-pr: (no active docs/specs/*/state.json — skipping loop gate)")
    elif not loop_cohort.is_file():
        print(
            "pre-pr: ✖ loop gate failed — "
            ".claude/skills/work-loop/scripts/loop-cohort.py missing",
            file=sys.stderr,
        )
        sys.exit(1)
    else:
        for state in state_files:
            spec_dir = state.parent
            for phase in ("implement", "review"):
                result = subprocess.run(
                    [py, str(loop_cohort), "check", str(spec_dir),
                     "--phase", phase],
                    capture_output=True, text=True, check=False,
                )
                if result.returncode != 0:
                    if result.stdout:
                        sys.stdout.write(result.stdout)
                    if result.stderr:
                        sys.stderr.write(result.stderr)
                    print(
                        f"pre-pr: ✖ loop-cohort check {spec_dir} "
                        f"--phase {phase} failed",
                        file=sys.stderr,
                    )
                    sys.exit(1)
                print(f"pre-pr: ✓ loop-cohort check {spec_dir} ({phase})")

    print("pre-pr: all checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
