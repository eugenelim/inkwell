---
name: mail-reclaim-storage
description: Use this skill to free up mailbox storage via the inkwell CLI — soft-delete (move to Deleted Items) the junk that takes the most space, large attachments first, then old bulk-sender back-catalogs and stale low-value mail. Triggers on "free up storage", "my mailbox is full", "clear space", "trash junk to reduce storage". It never permanently deletes and never empties Deleted Items — the user does that. Confirm every batch. For volume (not size) cleanup use `mail-triage`; to stop the junk arriving use `mail-reduce-noise`.
---

# Skill: mail-reclaim-storage

Recover mailbox space by moving space-hungry junk to **Deleted
Items** (soft delete). The user empties Deleted Items themselves
(Outlook, or the tenant's retention auto-purge) — you never
permanently delete and never empty it. **Confirm-every-batch,
interactive — never run unattended.** This is the **mutate** stage of
the mail-skill posture.

Order passes by space freed per action: biggest wins first. Scan the
**entire mailbox including archived mail** (`filter --all` covers all
subscribed folders) — archived junk still counts against quota.
Storage is dominated by **size**, so lead with size, not count.

## Hard exclusions — never trash these (skip-rails)

- **Flagged** (`~F` / `skip_rails: flagged`).
- **Threads the user participated in** (anything they sent into —
  check `~v <conv-id> & ~f <own-addr>` when in doubt).
- **Receipts, invoices, financial/tax/legal records**
  (`skip_rails: financial`).
- **Calendar invites for future events**
  (`skip_rails: meeting-request`; verify the date).
- **Attachments from real people** (colleagues, clients, known
  contacts) — only target bulk/automated/promotional senders
  (`is_bulk_sender`; a `person?` mark means *check*, not *take*).
- **Anything newer than the age floor** (default 30 days — the
  script enforces it; raise it if unsure).
- Anything the user marks keep during review.

When unsure whether something is junk, **keep it** — Deleted Items is
recoverable, but keep the user's review burden small.

## Procedure

1. **Preflight + sync.** `python3 scripts/preflight.py` (relative to
   this skill's directory; fails clean), then `inkwell sync` once.

2. **Pass 1 — large attachments (highest ROI).**
   `python3 scripts/big_items.py --min-mb 5` ranks messages by
   attachment size (the cache tracks no body sizes — attachment
   totals are the honest dominant term), with mechanical
   `skip_rails` and an `is_bulk_sender` hint; `--format table` for
   presentation. Apply the exclusions, group by sender, present the
   batch with **total MB it would free**, get explicit confirmation,
   then soft-delete: `inkwell messages delete <id>` per item (or a
   per-sender `inkwell filter '~f <addr> & ~A & ~d >30d' --action
   delete --apply --yes` after its dry-run list was shown).

3. **Pass 2 — bulk-sender back-catalogs.** Use `big_items.py`'s
   `by_sender` totals plus `mail-reduce-noise`'s ranking signals;
   prefer senders the user already unsubscribed from or routed away.
   Per offender: dry-run the full old history —
   `inkwell filter '~f <addr> & ~d >1y'` — show count (+ MB where
   known), confirm, then re-run with `--action delete --apply --yes`.

4. **Pass 3 — stale low-value categories.** Old feed/paper-trail
   mail, past meeting traffic, and — when the `mail-triage` workflow
   folders exist — the aged contents of the `Newsletters` folder:
   `~o feed & ~d >1y`, `~m Newsletters & ~d >1y`, etc. Same
   discipline: dry-run list → exclusions → confirm →
   `--action delete --apply --yes`.

5. **After each pass / at the end.** Report **estimated MB freed**
   and the running total (attachment-sum based — say so). Remind the
   user the space is reclaimed only once they **empty Deleted
   Items** in Outlook (or the auto-purge window passes) — offer to
   explain how, but never do it. Suggest `mail-reduce-noise` so the
   junk doesn't refill.

## Anti-patterns to refuse

- **`permanent-delete` or "emptying trash" by any means.** The only
  removal verb here is soft delete (`messages delete` /
  `--action delete`).
- **Batches without the dry-run list + size estimate shown.** Same
  presentation bar as `mail-triage` hard rule 3.
- **Trashing real-person attachments** without the user explicitly
  saying so, per item.
- **Running unattended** (cron, "just clean it while I'm out").
- **Pausing the rails because the mailbox is "really full".** A full
  mailbox with its tax records intact beats a roomy one without.
