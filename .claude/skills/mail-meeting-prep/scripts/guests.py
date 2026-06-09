#!/usr/bin/env python3
"""Deterministic guest-context gatherer for the mail-meeting-prep skill.

Pulls the day's calendar view from Microsoft Graph (live read — the
calendar is not cached locally), fetches each event's attendee list,
classifies attendees as external (email domain differs from the
user's), and collects each external guest's recent mail history from
the local cache. All judgment (who matters, what to say) stays with
the model.

Read-only throughout: calendar GETs + cached mail reads. Run
`inkwell sync` once before this script for fresh mail state.

Usage:
  guests.py [--days 1] [--mail-limit 10] [--max-guests 15]
            [--binary PATH]
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


def own_identity(binary):
    p = subprocess.run([binary, "whoami", "--no-sync", "-q"],
                       capture_output=True, text=True)
    if p.returncode != 0 or "@" not in p.stdout:
        sys.exit("guests: cannot determine own address "
                 "(run scripts/preflight.py)")
    addr = p.stdout.split()[0].strip().lower()
    return addr, addr.split("@", 1)[1]


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--days", type=int, default=1,
                    help="1 = today (default); N = agenda horizon")
    ap.add_argument("--mail-limit", type=int, default=10)
    ap.add_argument("--max-guests", type=int, default=15,
                    help="cap on distinct guests to research (default 15)")
    ap.add_argument("--binary", default="",
                    help="inkwell binary (default: $INKWELL_BIN, PATH, bin/inkwell)")
    opts = ap.parse_args()
    binary = resolve_binary(opts.binary)
    own_addr, own_domain = own_identity(binary)

    if opts.days <= 1:
        events = run_json(binary, ["calendar", "today", "--output", "json"])
    else:
        events = run_json(binary, ["calendar", "agenda",
                                   "--days", str(opts.days),
                                   "--output", "json"])
    if isinstance(events, dict) and "error" in events:
        sys.exit(f"guests: calendar pull failed: {events['error']}")

    out_events, guests = [], {}
    for ev in events or []:
        detail = run_json(binary, ["calendar", "show", ev.get("ID", ""),
                                   "--output", "json"])
        external = []
        for att in (detail.get("Attendees") or []
                    if isinstance(detail, dict) else []):
            addr = (att.get("Address") or "").lower()
            if not addr or addr == own_addr:
                continue
            if addr.split("@", 1)[-1] == own_domain:
                continue  # internal colleague
            if att.get("Type") == "resource":
                continue  # rooms, equipment
            external.append(addr)
            guests.setdefault(addr, {
                "name": att.get("Name") or "",
                "domain": addr.split("@", 1)[-1],
                "response": att.get("Status") or "",
            })
        out_events.append({
            "id": ev.get("ID"),
            "subject": ev.get("Subject"),
            "start": ev.get("Start"),
            "end": ev.get("End"),
            "location": ev.get("Location"),
            "organizer": ev.get("OrganizerAddress"),
            "online_meeting": bool(ev.get("OnlineMeetingURL")),
            "external_guests": external,
            "detail_error": detail.get("error")
            if isinstance(detail, dict) else None,
        })

    # Recent cached mail per guest (most recent first), capped.
    for addr in list(guests)[:opts.max_guests]:
        msgs = run_json(binary, ["messages", "--all", "--output", "json",
                                 "--limit", str(opts.mail_limit),
                                 "--filter", f"~f {addr} | ~r {addr}"])
        if isinstance(msgs, dict):
            guests[addr]["mail_error"] = msgs.get("error")
            guests[addr]["recent_mail"] = []
        else:
            guests[addr]["recent_mail"] = [{
                "id": m.get("ID"),
                "received": m.get("ReceivedAt"),
                "from": m.get("FromAddress"),
                "subject": m.get("Subject"),
                "snippet": (m.get("BodyPreview") or "")[:150],
            } for m in msgs]
        guests[addr]["no_history"] = not guests[addr]["recent_mail"]

    json.dump({
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "binary": binary,
        "own_address": own_addr,
        "own_domain": own_domain,
        "horizon_days": opts.days,
        "events": out_events,
        "guests": guests,
    }, sys.stdout, indent=2)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
