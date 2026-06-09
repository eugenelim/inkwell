#!/usr/bin/env python3
"""Deterministic gather pass for the mail-executive-summary skill.

Collects everything the briefing needs in one consolidated JSON
document on stdout: inbox window (deduped to latest-per-thread),
flagged mail, the Reply Later / Set Aside stacks, screener pending
count, calendar, and OOO state. All judgment (classification,
ranking, voice) stays with the model.

Read-only: every inkwell call runs with --no-sync. Mail/stack reads
come from the local cache; note that `calendar` and `ooo` are live
Graph GETs regardless of --no-sync (still read-only). Run
`inkwell sync` once before this script if you want fresh mail state.

Usage:
  gather.py [--days 2] [--limit 200] [--agenda-days 7] [--binary PATH]
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
from datetime import datetime, timezone


def resolve_binary(explicit):
    """--binary flag > $INKWELL_BIN > PATH > repo-local bin/inkwell."""
    return (explicit or os.environ.get("INKWELL_BIN")
            or shutil.which("inkwell") or "bin/inkwell")


def run(binary, args, timeout=120):
    """Run an inkwell subcommand read-only.

    Returns {"json": …} on parseable output, {"text": …} when the
    command has no JSON mode for this verb, {"error": …} on failure —
    the briefing degrades per-section instead of dying whole.
    """
    try:
        p = subprocess.run(
            [binary, *args, "--no-sync", "-q"],
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except (OSError, subprocess.TimeoutExpired) as e:
        return {"error": str(e)}
    if p.returncode != 0:
        return {"error": (p.stderr.strip() or f"exit {p.returncode}")[:500]}
    try:
        return {"json": json.loads(p.stdout)}
    except ValueError:
        return {"text": p.stdout.strip()}


def dedupe_threads(msgs):
    """Latest message per ConversationID, newest first, with depth."""
    threads = {}
    for m in msgs:
        key = m.get("ConversationID") or m.get("ID")
        cur = threads.get(key)
        if cur is None:
            threads[key] = {"latest": m, "count": 1}
        else:
            cur["count"] += 1
            if (m.get("ReceivedAt") or "") > (cur["latest"].get("ReceivedAt") or ""):
                cur["latest"] = m
    out = []
    for t in sorted(threads.values(),
                    key=lambda t: t["latest"].get("ReceivedAt") or "",
                    reverse=True):
        m = t["latest"]
        out.append({
            "id": m.get("ID"),
            "conversation_id": m.get("ConversationID"),
            "received": m.get("ReceivedAt"),
            "from": m.get("FromAddress"),
            "from_name": m.get("FromName"),
            "subject": m.get("Subject"),
            "snippet": (m.get("BodyPreview") or "")[:200],
            "unread": not m.get("IsRead", False),
            "flagged": m.get("FlagStatus") == "flagged",
            "importance": m.get("Importance"),
            "meeting": m.get("MeetingMessageType") or "",
            "thread_msgs": t["count"],
        })
    return out


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--days", type=int, default=2,
                    help="inbox window in days (widen if quiet, narrow if flooded)")
    ap.add_argument("--limit", type=int, default=200)
    ap.add_argument("--agenda-days", type=int, default=7)
    ap.add_argument("--binary", default="",
                    help="inkwell binary (default: $INKWELL_BIN, PATH, bin/inkwell)")
    opts = ap.parse_args()
    binary = resolve_binary(opts.binary)

    inbox = run(binary, ["messages", "--folder", "Inbox", "--output", "json",
                         "--limit", str(opts.limit),
                         "--filter", f"~d <{opts.days}d"])
    flagged = run(binary, ["messages", "--all", "--output", "json",
                           "--limit", "50", "--filter", "~F"])

    # Workflow-folder detection: when mail-triage maintains @Action /
    # @Waiting, read its filed state instead of re-deriving it. When
    # the folders don't exist (the user doesn't run triage), the
    # sections are null and the brief classifies from the inbox alone.
    folders = run(binary, ["folders", "--output", "json"])
    names = {f.get("displayName") for f in folders.get("json") or []} \
        if "json" in folders else set()
    action_queue = waiting = None
    if "@Action" in names:
        r = run(binary, ["messages", "--folder", "@Action",
                         "--output", "json", "--limit", "50"])
        action_queue = dedupe_threads(r["json"]) if "json" in r else r
    if "@Waiting" in names:
        r = run(binary, ["messages", "--folder", "@Waiting",
                         "--output", "json", "--limit", "50"])
        waiting = dedupe_threads(r["json"]) if "json" in r else r

    doc = {
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "binary": binary,
        "window_days": opts.days,
        # whoami has no JSON mode — arrives as {"text": "<upn> (tenant <id>)"}
        "whoami": run(binary, ["whoami"]),
        "workflow_folders": sorted(names & {"@Action", "@Waiting",
                                            "Reference", "Newsletters"}),
        "action_queue": action_queue,   # null = folder absent
        "waiting": waiting,             # null = folder absent
        "inbox_threads": (dedupe_threads(inbox["json"])
                          if "json" in inbox else inbox),
        "flagged": (dedupe_threads(flagged["json"])
                    if "json" in flagged else flagged),
        "reply_later": run(binary, ["later", "list", "--output", "json"]),
        "set_aside": run(binary, ["aside", "list", "--output", "json"]),
        "screener_pending": run(binary, ["screener", "list", "--output", "json"]),
        "calendar_today": run(binary, ["calendar", "today", "--output", "json"]),
        "calendar_agenda": run(binary, ["calendar", "agenda",
                                        "--days", str(opts.agenda_days),
                                        "--output", "json"]),
        "ooo": run(binary, ["ooo", "--output", "json"]),
    }
    json.dump(doc, sys.stdout, indent=2)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
