#!/usr/bin/env python3
"""Preflight for the mail-* mailbox-operation skills (identical copy
shipped in each skill's scripts/ so every skill stays self-contained).

Verifies the inkwell CLI is reachable and a session exists. Fails
clean with instructions when something is missing — it installs
nothing, builds nothing, and never uses sudo. Exit codes:
  0 ok · 1 binary missing · 2 not signed in · 3 CLI error.

Resolution order: $INKWELL_BIN > PATH > repo-local bin/inkwell.
"""

import os
import shutil
import subprocess
import sys

GUIDE = "docs/user/howto/agent-skills.md"


def main():
    binary = (os.environ.get("INKWELL_BIN") or shutil.which("inkwell")
              or ("bin/inkwell" if os.path.isfile("bin/inkwell") else None))
    if not binary:
        sys.stderr.write(
            "preflight: inkwell CLI not found.\n"
            f"Install it on your PATH (see {GUIDE}), or from this repo:\n"
            "  go build -o bin/inkwell ./cmd/inkwell\n"
            "or point $INKWELL_BIN at an existing binary.\n"
        )
        return 1

    try:
        p = subprocess.run([binary, "whoami", "-q"], capture_output=True,
                           text=True, timeout=60)
    except (OSError, subprocess.TimeoutExpired) as e:
        sys.stderr.write(f"preflight: {binary} failed to run: {e}\n")
        return 3
    if p.returncode != 0:
        sys.stderr.write(
            "preflight: no signed-in session.\n"
            "Ask the user to run `inkwell signin` — it opens a browser\n"
            "and needs a human; agents cannot complete it.\n"
            f"({p.stderr.strip()})\n"
        )
        return 2

    print(f"ok: {binary}")
    print(p.stdout.strip())
    return 0


if __name__ == "__main__":
    sys.exit(main())
