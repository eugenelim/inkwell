#!/usr/bin/env python3
"""Deterministic space-hog finder for the mail-reclaim-storage skill.

The local cache stores no per-message size, so storage ranking uses
the dominant term: attachment sizes. Scans messages with attachments
older than the age floor (across ALL subscribed folders, archives
included — archived junk still counts against quota), sums each
message's attachment sizes, annotates mechanical skip-rails, and
ranks by size. Per-sender totals come along for the back-catalog
pass. Disposition judgment stays with the model and the user.

Read-only: every inkwell call runs with --no-sync. Run `inkwell sync`
once before this script if you want fresh server state.

Usage:
  big_items.py [--age-days 30] [--scan 150] [--min-mb 1] [--top 40]
               [--binary PATH] [--format json|table]
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
from datetime import datetime, timezone

FINANCIAL = re.compile(
    r"(receipt|invoice|billing)@|invoice #|payment receipt"
    r"|your receipt from|order confirmation|billing statement",
    re.I,
)
BULK_SENDER = re.compile(
    r"(newsletter|no-?reply|do-?not-?reply|notifications?@|marketing@"
    r"|news@|digest@|updates?@|mailer-daemon)",
    re.I,
)


def resolve_binary(explicit):
    """--binary flag > $INKWELL_BIN > PATH > repo-local bin/inkwell."""
    return (explicit or os.environ.get("INKWELL_BIN")
            or shutil.which("inkwell") or "bin/inkwell")


def run_json(binary, args, timeout=120):
    try:
        p = subprocess.run([binary, *args, "--no-sync", "-q"],
                           capture_output=True, text=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as e:
        return {"error": str(e)}
    if p.returncode != 0:
        return {"error": (p.stderr.strip() or f"exit {p.returncode}")[:500]}
    try:
        return json.loads(p.stdout)
    except ValueError:
        return {"error": "non-JSON output"}


def own_address(binary):
    p = subprocess.run([binary, "whoami", "--no-sync", "-q"],
                       capture_output=True, text=True)
    if p.returncode == 0 and "@" in p.stdout:
        return p.stdout.split()[0].strip().lower()
    return ""


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--age-days", type=int, default=30,
                    help="age floor — never report anything newer (default 30)")
    ap.add_argument("--scan", type=int, default=150,
                    help="max messages to size (one attachments call each)")
    ap.add_argument("--min-mb", type=float, default=1.0,
                    help="ignore messages below this attachment total")
    ap.add_argument("--top", type=int, default=40)
    ap.add_argument("--format", choices=("json", "table"), default="json")
    ap.add_argument("--binary", default="",
                    help="inkwell binary (default: $INKWELL_BIN, PATH, bin/inkwell)")
    opts = ap.parse_args()
    binary = resolve_binary(opts.binary)
    own = own_address(binary)

    scan = run_json(binary, ["filter", f"~A & ~d >{opts.age_days}d", "--all",
                             "--output", "json", "--limit", str(opts.scan)])
    if "error" in scan:
        sys.exit(f"big_items: scan failed: {scan['error']}")
    msgs = scan.get("messages") or []

    items, senders = [], {}
    for m in msgs:
        mid = m.get("ID")
        atts = run_json(binary, ["messages", "attachments", mid,
                                 "--output", "json"])
        if isinstance(atts, dict):  # error — skip but stay deterministic
            continue
        size = sum(a.get("Size") or 0 for a in atts
                   if not a.get("IsInline"))
        if size < opts.min_mb * 1024 * 1024:
            continue
        sender = (m.get("FromAddress") or "").lower()
        subject = m.get("Subject") or ""
        rails = []
        if m.get("FlagStatus") == "flagged":
            rails.append("flagged")
        if sender == own:
            rails.append("own-message")
        if FINANCIAL.search(sender) or FINANCIAL.search(subject):
            rails.append("financial")
        if m.get("MeetingMessageType") == "meetingRequest":
            rails.append("meeting-request")
        items.append({
            "id": mid,
            "conversation_id": m.get("ConversationID"),
            "received": m.get("ReceivedAt"),
            "from": sender,
            "subject": subject,
            "size_mb": round(size / (1024 * 1024), 1),
            "attachments": len(atts),
            "is_bulk_sender": bool(BULK_SENDER.search(sender)),
            "skip_rails": rails,
        })
        agg = senders.setdefault(sender, {"sender": sender, "messages": 0,
                                          "size_mb": 0.0})
        agg["messages"] += 1
        agg["size_mb"] = round(agg["size_mb"] + size / (1024 * 1024), 1)

    items.sort(key=lambda i: -i["size_mb"])
    sized = len(items)  # cleared --min-mb, before the --top cut
    items = items[:opts.top]
    by_sender = sorted(senders.values(), key=lambda s: -s["size_mb"])

    doc = {
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "binary": binary,
        "age_floor_days": opts.age_days,
        "messages_scanned": len(msgs),
        # how many cleared --min-mb — distinguishes "nothing big
        # found" from "ran out of scan budget" when truncated
        "sized": sized,
        "scan_truncated": len(msgs) >= opts.scan,
        "note": "sizes = attachment totals from the local cache; "
                "body sizes are not tracked",
        "items": items,
        "by_sender": by_sender,
    }

    if opts.format == "json":
        json.dump(doc, sys.stdout, indent=2)
        sys.stdout.write("\n")
        return

    for i in items:
        date = (i["received"] or "")[:10]
        rails = ",".join(i["skip_rails"]) or "-"
        bulk = "bulk" if i["is_bulk_sender"] else "person?"
        print(f"{i['size_mb']:>7.1f}M  {date}  {i['from']:<36.36}  "
              f"{i['subject']:<42.42}  {bulk:<7}  rails:{rails}")
    if doc["scan_truncated"]:
        print(f"(scan hit the {opts.scan}-message cap — "
              "rerun with --scan higher or a tighter --age-days)")


if __name__ == "__main__":
    main()
