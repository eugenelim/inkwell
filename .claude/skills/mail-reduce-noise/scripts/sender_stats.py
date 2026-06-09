#!/usr/bin/env python3
"""Deterministic sender-noise ranking for the mail-reduce-noise skill.

Aggregates per-sender stats from the local cache over a recent
window: total volume, read rate, how much still sits in the Inbox,
whether a cached List-Unsubscribe action exists, and any existing
routing. Ranks by noise score = volume × (1 − read rate). All
dispositions (unsubscribe / auto-route / keep) stay with the model
and the user.

Read-only: every inkwell call runs with --no-sync. Run `inkwell sync`
once before this script if you want fresh server state.

Usage:
  sender_stats.py [--days 90] [--limit 1000] [--min-total 5]
                  [--top 25] [--binary PATH] [--format json|table]
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


def run_json(binary, args, timeout=180):
    try:
        p = subprocess.run([binary, *args, "--no-sync", "-q"],
                           capture_output=True, text=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as e:
        sys.exit(f"sender_stats: {' '.join(args)}: {e}")
    if p.returncode != 0:
        sys.exit(f"sender_stats: {' '.join(args)}: "
                 f"{p.stderr.strip() or p.returncode}")
    try:
        return json.loads(p.stdout)
    except ValueError:
        sys.exit(f"sender_stats: {' '.join(args)}: non-JSON output")


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--days", type=int, default=90,
                    help="lookback window (default 90)")
    ap.add_argument("--limit", type=int, default=1000,
                    help="max messages to scan (filter cap, default 1000)")
    ap.add_argument("--min-total", type=int, default=5,
                    help="ignore senders below this volume (default 5)")
    ap.add_argument("--top", type=int, default=25,
                    help="report the N noisiest senders (default 25)")
    ap.add_argument("--format", choices=("json", "table"), default="json")
    ap.add_argument("--binary", default="",
                    help="inkwell binary (default: $INKWELL_BIN, PATH, bin/inkwell)")
    opts = ap.parse_args()
    binary = resolve_binary(opts.binary)

    # Window scan across all subscribed folders; Inbox id for the
    # still-in-inbox count.
    scan = run_json(binary, ["filter", f"~d <{opts.days}d", "--all",
                             "--output", "json", "--limit", str(opts.limit)])
    msgs = scan.get("messages") or []
    folders = run_json(binary, ["folders", "--output", "json"])
    inbox_ids = {f["id"] for f in folders
                 if f.get("wellKnownName") == "inbox"}

    own = ""
    whoami = subprocess.run([binary, "whoami", "--no-sync", "-q"],
                            capture_output=True, text=True)
    if whoami.returncode == 0 and "@" in whoami.stdout:
        own = whoami.stdout.split()[0].strip().lower()

    routes = {}
    for r in run_json(binary, ["route", "list", "--output", "json"]) or []:
        if isinstance(r, dict) and r.get("address"):
            routes[r["address"].lower()] = r.get("destination")

    stats = {}
    for m in msgs:
        addr = (m.get("FromAddress") or "").lower()
        if not addr or addr == own:
            continue
        s = stats.setdefault(addr, {
            "sender": addr,
            "name": m.get("FromName") or "",
            "total": 0, "read": 0, "inbox": 0, "flagged": 0,
            "has_unsubscribe": False,
        })
        s["total"] += 1
        s["read"] += 1 if m.get("IsRead") else 0
        s["inbox"] += 1 if m.get("FolderID") in inbox_ids else 0
        s["flagged"] += 1 if m.get("FlagStatus") == "flagged" else 0
        # Lazily cached (spec 16): True is reliable; False means
        # "unknown or none" — the headers may simply never have been
        # fetched.
        if m.get("UnsubscribeURL"):
            s["has_unsubscribe"] = True

    rows = []
    for s in stats.values():
        if s["total"] < opts.min_total:
            continue
        s["read_rate"] = round(s["read"] / s["total"], 2)
        s["noise_score"] = round(s["total"] * (1 - s["read_rate"]), 1)
        s["routed"] = routes.get(s["sender"], "")
        rows.append(s)
    rows.sort(key=lambda s: (-s["noise_score"], -s["total"], s["sender"]))
    rows = rows[:opts.top]

    doc = {
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "binary": binary,
        "window_days": opts.days,
        "messages_scanned": len(msgs),
        "scan_truncated": len(msgs) >= opts.limit,
        "min_total": opts.min_total,
        "senders": rows,
    }

    if opts.format == "json":
        json.dump(doc, sys.stdout, indent=2)
        sys.stdout.write("\n")
        return

    print(f"{'SENDER':<42} {'TOTAL':>5} {'READ%':>6} {'INBOX':>5} "
          f"{'FLAG':>4} {'UNSUB':>5} {'ROUTED':<11} {'NOISE':>6}")
    for s in rows:
        print(f"{s['sender']:<42.42} {s['total']:>5} "
              f"{int(s['read_rate'] * 100):>5}% {s['inbox']:>5} "
              f"{s['flagged']:>4} {'yes' if s['has_unsubscribe'] else '?':>5} "
              f"{s['routed'] or '-':<11} {s['noise_score']:>6}")
    if doc["scan_truncated"]:
        print(f"(scan hit the {opts.limit}-message cap — "
              "narrow --days or raise --limit)")


if __name__ == "__main__":
    main()
