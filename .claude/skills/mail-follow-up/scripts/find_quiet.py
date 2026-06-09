#!/usr/bin/env python3
"""Deterministic candidate finder for the mail-follow-up skill.

Mode auto (default): if the mail-triage workflow folder `@Waiting`
exists, scan the conversations filed there (triage already decided
the ball is in someone else's court); otherwise fall back to scanning
recent Sent Items. Either way, a conversation is a candidate when the
USER sent the last real message, nothing newer arrived, and it's been
quiet ≥ the threshold; `has_draft` flags threads already chased.
Deciding *whether* a thread deserves a nudge stays with the model.

Read-only: every inkwell call runs with --no-sync. Run `inkwell sync`
once before this script if you want fresh server state.

Usage:
  find_quiet.py [--mode auto|waiting|sent] [--days 30] [--quiet-days 3]
                [--max-threads 30] [--binary PATH]
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
from datetime import date, datetime, timedelta, timezone


def resolve_binary(explicit):
    """--binary flag > $INKWELL_BIN > PATH > repo-local bin/inkwell."""
    return (explicit or os.environ.get("INKWELL_BIN")
            or shutil.which("inkwell") or "bin/inkwell")


def run_json(binary, args, timeout=120):
    try:
        p = subprocess.run([binary, *args, "--no-sync", "-q"],
                           capture_output=True, text=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as e:
        sys.exit(f"find_quiet: {' '.join(args)}: {e}")
    if p.returncode != 0:
        sys.exit(f"find_quiet: {' '.join(args)}: "
                 f"{p.stderr.strip() or p.returncode}")
    try:
        return json.loads(p.stdout)
    except ValueError:
        sys.exit(f"find_quiet: {' '.join(args)}: non-JSON output")


def own_address(binary):
    p = subprocess.run([binary, "whoami", "--no-sync", "-q"],
                       capture_output=True, text=True)
    if p.returncode != 0 or "@" not in p.stdout:
        sys.exit("find_quiet: cannot determine own address "
                 "(run scripts/preflight.py)")
    return p.stdout.split()[0].strip().lower()


def when(m):
    """Recency key: max of ReceivedAt / SentAt (RFC3339 strings sort
    lexicographically; a zero time marshals as 0001-01-01… and sorts
    low). Sent items are most reliably ordered by SentAt."""
    return max(m.get("ReceivedAt") or "", m.get("SentAt") or "")


def parse_when(s):
    try:
        return datetime.fromisoformat((s or "").replace("Z", "+00:00"))
    except ValueError:
        return None


def business_days_since(d, today):
    """Weekdays strictly between d.date() and today (approximation —
    no holiday awareness)."""
    n, cur = 0, d.date() + timedelta(days=1)
    while cur <= today:
        if cur.weekday() < 5:
            n += 1
        cur += timedelta(days=1)
    return n


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--days", type=int, default=30,
                    help="how far back to scan Sent Items (default 30)")
    ap.add_argument("--quiet-days", type=int, default=3,
                    help="minimum quiet business days to report (default 3)")
    ap.add_argument("--max-threads", type=int, default=30,
                    help="cap on conversations to inspect (default 30)")
    ap.add_argument("--mode", choices=("auto", "waiting", "sent"),
                    default="auto",
                    help="auto: use @Waiting if mail-triage maintains it, "
                         "else scan Sent Items")
    ap.add_argument("--sent-folder", default="Sent Items",
                    help='Sent folder display name (localized mailboxes: '
                         'pass the localized name, e.g. "Éléments envoyés")')
    ap.add_argument("--binary", default="",
                    help="inkwell binary (default: $INKWELL_BIN, PATH, bin/inkwell)")
    opts = ap.parse_args()
    binary = resolve_binary(opts.binary)
    own = own_address(binary)
    today = datetime.now(timezone.utc).date()

    mode = opts.mode
    if mode == "auto":
        folders = run_json(binary, ["folders", "--output", "json"])
        has_waiting = any(f.get("displayName") == "@Waiting"
                          for f in folders or [])
        mode = "waiting" if has_waiting else "sent"
    if mode == "waiting":
        # Triage already filed these as ball-in-their-court; the
        # thread check below still verifies nothing arrived since.
        sent = run_json(binary, ["messages", "--folder", "@Waiting",
                                 "--output", "json", "--limit", "200"])
    else:
        sent = run_json(binary, ["messages", "--folder", opts.sent_folder,
                                 "--output", "json", "--limit", "200",
                                 "--filter", f"~d <{opts.days}d"])

    # latest sent message per conversation, newest first
    convs = {}
    for m in sent or []:
        cid = m.get("ConversationID")
        if not cid:
            continue
        cur = convs.get(cid)
        if cur is None or when(m) > when(cur):
            convs[cid] = m
    ordered = sorted(convs.values(), key=when, reverse=True)
    ordered = ordered[:opts.max_threads]

    candidates = []
    for sent_msg in ordered:
        cid = sent_msg["ConversationID"]
        thread = run_json(binary, ["messages", "--all", "--output", "json",
                                   "--limit", "50", "--filter", f"~v {cid}"])
        if not thread:
            continue
        # Drafts are unsent — they don't count as thread activity and
        # must not become the nudge's reply target.
        real = [m for m in thread if not m.get("IsDraft")]
        if not real:
            continue
        latest = max(real, key=when)
        if (latest.get("FromAddress") or "").lower() != own:
            continue  # someone replied after the user — not quiet
        last_dt = parse_when(when(latest))
        if last_dt is None:
            continue
        bdays = business_days_since(last_dt, today)
        if bdays < opts.quiet_days:
            continue
        candidates.append({
            "conversation_id": cid,
            "last_message_id": latest.get("ID"),
            "subject": latest.get("Subject"),
            # EmailAddress marshals with lowercase tags
            "to": [a.get("address") for a in latest.get("ToAddresses") or []],
            "last_sent": when(latest),
            "quiet_days": (today - last_dt.date()).days,
            "quiet_business_days": bdays,
            "thread_msgs": len(thread),
            "has_draft": any(m.get("IsDraft") for m in thread),
            "snippet": (latest.get("BodyPreview") or "")[:200],
        })

    json.dump({
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "binary": binary,
        "own_address": own,
        "mode": mode,
        "window_days": opts.days,
        "min_quiet_business_days": opts.quiet_days,
        "conversations_inspected": len(ordered),
        "candidates": candidates,
    }, sys.stdout, indent=2)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
