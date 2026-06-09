#!/usr/bin/env python3
"""Deterministic survey pass for the mail-triage skill.

Pulls a batch of inbox messages through the inkwell CLI, dedupes to
the latest message per conversation, joins per-sender routing, and
annotates each thread with the *mechanical* triage signals from
references/rubric.md (keep-rails and archive-hints that need no
judgment). Classification judgment stays with the model.

Read-only: every inkwell call runs with --no-sync. Run `inkwell sync`
once before this script if you want fresh server state.

Usage:
  survey.py [--folder Inbox] [--limit 100] [--days N]
            [--include-read] [--format json|table] [--binary PATH]
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
from datetime import datetime, timezone

FINANCIAL_SENDER = re.compile(r"(receipt|invoice|billing)@", re.I)
FINANCIAL_SUBJECT = re.compile(
    r"(invoice #|payment receipt|your receipt from|order confirmation"
    r"|billing statement)",
    re.I,
)
# Reminders/renewals are NOT financial records (rubric: they fall
# through to judgment) — used to suppress the rail, not to archive.
FINANCIAL_NOT_RECORD = re.compile(r"(reminder|overdue|renew)", re.I)
BULK_SENDER = re.compile(
    r"(newsletter|no-?reply|do-?not-?reply|notifications?@|marketing@"
    r"|news@|digest@|updates?@|mailer-daemon)",
    re.I,
)
AUTOMATED_DOMAIN = re.compile(
    r"@(.*\.)?(github\.com|gitlab\.com|linkedin\.com|facebookmail\.com"
    r"|twitter\.com|x\.com|atlassian\.(com|net)|slack\.com)$",
    re.I,
)


def resolve_binary(explicit):
    """--binary flag > $INKWELL_BIN > PATH > repo-local bin/inkwell."""
    return (explicit or os.environ.get("INKWELL_BIN")
            or shutil.which("inkwell") or "bin/inkwell")


def run(binary, args, timeout=120):
    """Run an inkwell subcommand; return (parsed-json-or-None, raw, err)."""
    try:
        p = subprocess.run(
            [binary, *args, "--no-sync", "-q"],
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except (OSError, subprocess.TimeoutExpired) as e:
        return None, "", str(e)
    if p.returncode != 0:
        return None, p.stdout, p.stderr.strip() or f"exit {p.returncode}"
    try:
        return json.loads(p.stdout), p.stdout, None
    except ValueError:
        return None, p.stdout, None


def pull_messages(binary, folder, limit, days, include_read):
    """Build the right messages query.

    NOTE: --unread is ignored whenever --filter is set (the filter
    branch never consults it), so unread+window must be expressed as
    a single pattern.
    """
    args = ["messages", "--folder", folder, "--output", "json",
            "--limit", str(limit)]
    atoms = []
    if not include_read:
        atoms.append("~N")
    if days:
        atoms.append(f"~d <{days}d")
    if atoms:
        args += ["--filter", " & ".join(atoms)]
    parsed, _, err = run(binary, args)
    if err:
        sys.exit(f"survey: messages pull failed: {err}")
    return parsed or []


def keep_rails(msg):
    rails = []
    if msg.get("FlagStatus") == "flagged":
        rails.append("flagged")
    if msg.get("HasAttachments"):
        rails.append("attachments")
    if msg.get("MeetingMessageType") == "meetingRequest":
        rails.append("meeting-request")
    sender = msg.get("FromAddress") or ""
    subject = msg.get("Subject") or ""
    if (FINANCIAL_SENDER.search(sender) or FINANCIAL_SUBJECT.search(subject)) \
            and not FINANCIAL_NOT_RECORD.search(subject):
        rails.append("financial-record")
    return rails


def archive_hints(msg, route_by_sender):
    hints = []
    sender = (msg.get("FromAddress") or "").lower()
    dest = route_by_sender.get(sender)
    if dest in ("feed", "paper_trail"):
        hints.append(f"routed-{dest}")
    if msg.get("InferenceClass") == "other":
        hints.append("inference-other")
    if BULK_SENDER.search(sender):
        hints.append("bulk-sender")
    if AUTOMATED_DOMAIN.search(sender):
        hints.append("automated-platform")
    return hints


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
    out = list(threads.values())
    out.sort(key=lambda t: t["latest"].get("ReceivedAt") or "", reverse=True)
    return out


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--folder", default="Inbox")
    ap.add_argument("--limit", type=int, default=100)
    ap.add_argument("--days", type=int, default=0,
                    help="only messages received in the last N days")
    ap.add_argument("--include-read", action="store_true")
    ap.add_argument("--format", choices=("json", "table"), default="json")
    ap.add_argument("--binary", default="",
                    help="inkwell binary (default: PATH, then bin/inkwell)")
    opts = ap.parse_args()

    binary = resolve_binary(opts.binary)
    msgs = pull_messages(binary, opts.folder, opts.limit, opts.days,
                         opts.include_read)

    routes, _, _ = run(binary, ["route", "list", "--output", "json"])
    route_by_sender = {}
    for r in routes or []:
        if isinstance(r, dict) and r.get("address"):
            route_by_sender[r["address"].lower()] = r.get("destination")

    # Workflow-folder setup (docs/user/howto/agent-skills.md): which
    # of the fixed set exist. Archive is built-in (well-known).
    folders, _, _ = run(binary, ["folders", "--output", "json"])
    names = {f.get("displayName") for f in folders or []}
    wellknown = {f.get("wellKnownName") for f in folders or []}
    folder_setup = {
        "@Action": "@Action" in names,
        "@Waiting": "@Waiting" in names,
        "Archive": "archive" in wellknown or "Archive" in names,
        "Reference": "Reference" in names,
        "Newsletters": "Newsletters" in names,
    }

    rows = []
    for t in dedupe_threads(msgs):
        m = t["latest"]
        rows.append({
            "id": m.get("ID"),
            "conversation_id": m.get("ConversationID"),
            "received": m.get("ReceivedAt"),
            "from": m.get("FromAddress"),
            "from_name": m.get("FromName"),
            "subject": m.get("Subject"),
            "snippet": (m.get("BodyPreview") or "")[:200],
            "unread": not m.get("IsRead", False),
            "importance": m.get("Importance"),
            "thread_msgs": t["count"],
            "keep_rails": keep_rails(m),
            "archive_hints": archive_hints(m, route_by_sender),
        })

    doc = {
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "binary": binary,
        "folder": opts.folder,
        "folder_setup": folder_setup,
        "messages_scanned": len(msgs),
        "threads": rows,
    }

    if opts.format == "json":
        json.dump(doc, sys.stdout, indent=2)
        sys.stdout.write("\n")
        return

    # table: the presentation shape hard rule 3 requires (date · from · subject)
    for r in rows:
        date = (r["received"] or "")[:16].replace("T", " ")
        rails = ",".join(r["keep_rails"]) or "-"
        hints = ",".join(r["archive_hints"]) or "-"
        print(f"{date}  {(r['from'] or ''):<38.38}  "
              f"{(r['subject'] or ''):<50.50}  rails:{rails}  hints:{hints}")


if __name__ == "__main__":
    main()
