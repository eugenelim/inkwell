#!/usr/bin/env python3
"""Deterministic draft-save helper (identical copy in mail-draft-reply
and mail-follow-up so each skill stays self-contained).

Reads the approved body from stdin (or --body-file), writes it to a
private tempfile, calls `inkwell messages reply|reply-all|forward`
(which creates a DRAFT in the mailbox Drafts folder — inkwell has no
send verb; Mail.Send is denied by construction, PRD §3.1), and always
removes the tempfile afterwards so confidential body text never
lingers on disk.

Usage:
  echo "$BODY" | save_draft.py <message-id> [--verb reply|reply-all|forward]
                 [--to addr]... [--subject "Re: ..."] [--binary PATH]
"""

import argparse
import os
import shutil
import subprocess
import sys
import tempfile


def resolve_binary(explicit):
    """--binary flag > $INKWELL_BIN > PATH > repo-local bin/inkwell."""
    return (explicit or os.environ.get("INKWELL_BIN")
            or shutil.which("inkwell") or "bin/inkwell")


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("message_id")
    ap.add_argument("--verb", choices=("reply", "reply-all", "forward"),
                    default="reply")
    ap.add_argument("--to", action="append", default=[],
                    help="recipient (forward only; may repeat)")
    ap.add_argument("--subject", default="")
    ap.add_argument("--body-file", default="",
                    help="read body from this file instead of stdin")
    ap.add_argument("--binary", default="",
                    help="inkwell binary (default: $INKWELL_BIN, PATH, bin/inkwell)")
    opts = ap.parse_args()

    if opts.verb == "forward" and not opts.to:
        sys.exit("save_draft: forward requires at least one --to")
    if opts.verb != "forward" and opts.to:
        sys.exit("save_draft: --to is only valid with --verb forward")

    if opts.body_file:
        with open(opts.body_file, "r", encoding="utf-8") as f:
            body = f.read()
    else:
        body = sys.stdin.read()
    if not body.strip():
        sys.exit("save_draft: refusing to save an empty draft body")

    binary = resolve_binary(opts.binary)
    fd, path = tempfile.mkstemp(prefix="inkwell-draft-", suffix=".txt")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write(body)
        args = [binary, "messages", opts.verb, opts.message_id,
                "--body-from-file", path]
        if opts.subject:
            args += ["--subject", opts.subject]
        for addr in opts.to:
            args += ["--to", addr]
        p = subprocess.run(args, capture_output=True, text=True, timeout=120)
        sys.stdout.write(p.stdout)
        sys.stderr.write(p.stderr)
        if p.returncode == 0:
            print("draft saved to Drafts — review and send from Outlook")
        return p.returncode
    finally:
        os.unlink(path)


if __name__ == "__main__":
    sys.exit(main())
