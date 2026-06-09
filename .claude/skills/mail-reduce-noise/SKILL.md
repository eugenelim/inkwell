---
name: mail-reduce-noise
description: Use this skill to cut recurring inbox clutter at the source via the inkwell CLI — find high-volume, rarely-read senders and apply a standing decision (unsubscribe, auto-route past the inbox, or keep) so their future mail never piles up. Triggers on "reduce email noise", "unsubscribe from junk", "clean up my subscriptions", "stop newsletters cluttering my inbox", "which senders are noisiest". Different from `mail-triage` (one-off processing of what's already there) — this changes what arrives. For a one-off cleanup use `mail-triage`.
---

# Skill: mail-reduce-noise

Attack clutter at the source. Unlike one-off archiving, this finds
*senders* that repeatedly fill the inbox and applies a standing
decision so future mail never piles up. Every disposition is one of
three: **unsubscribe**, **auto-route**, or **keep**.

## Hard rules (non-negotiable)

1. **Unsubscribe execution belongs to the human.** inkwell's
   unsubscribe is a TUI flow (`U` / `:unsub`, spec 16) with a
   phishing-safe confirm modal that shows the exact URL before the
   one-click POST — there is deliberately no CLI verb, and you never
   substitute raw HTTP calls or emails to mailto: addresses. You
   rank candidates and hand the user the keystroke list.
2. **Standing decisions are durable — confirm each one in chat.**
   Routing, screener rejections, and server-side rules affect all
   future mail from a sender.
3. **Unsubscribe is outward-facing and hard to undo; auto-route is
   reversible.** When unsure between the two, prefer auto-route.
4. **Never target people** — only bulk/automated senders, never an
   individual the user corresponds with.
5. **inkwell cannot send mail** (PRD §3.1) — nothing here sends
   anything, including unsubscribe emails.

## Procedure

1. **Preflight + sync.** `python3 scripts/preflight.py` (relative to
   this skill's directory; fails clean with instructions), then
   `inkwell sync` once.

2. **Rank senders by noise.** If `mail-triage` maintains the
   workflow folders, its `Newsletters` folder and soft-delete plans
   are a ready-made candidate list — senders repeatedly filed or
   deleted there are exactly what this skill should silence at the
   source. Then `python3 scripts/sender_stats.py`
   aggregates the last `--days 90` per sender: total volume, read
   rate, still-in-inbox count, flagged count, cached unsubscribe
   hint, and existing routing — ranked by noise score
   `volume × (1 − read rate)`. A sender you get 40 emails from and
   open 2 of is prime; one you get 40 from and read 35 is not.
   `--format table` for the user-facing view. Note: `has_unsubscribe`
   is lazily cached — `?` means *unknown*, not absent.

3. **Safety rails — exclude before proposing:**
   - **High read rate** — if they open it, it's not noise.
   - **Flagged count > 0** — the user has marked some of it.
   - **Transactional / important senders** — receipts, invoices,
     security/2FA codes, password resets, banking, payroll, the
     user's employer, known work contacts.
   - **People** (hard rule 4). When the sender's nature is unclear
     from the stats, sample a message body
     (`inkwell messages show <id>`) before proposing.

4. **Decide a disposition per candidate:**
   - **UNSUBSCRIBE** — high volume, low read rate, promotional
     blasts / newsletters the user never reads and would never want
     back. Executed by the user in the TUI (see step 6).
   - **AUTO-ROUTE (keep the subscription, skip the inbox)** —
     recurring mail rarely read in the moment but worth *searching
     later*, or with no reliable unsubscribe path: receipts,
     automated notifications, low-priority updates. Pick the
     lightest standing fix:
     | Fix | Effect |
     | --- | --- |
     | `inkwell route assign <addr> feed\|paper_trail` | local-only stream routing (this client) |
     | `inkwell screener reject <addr>` | screened out at first contact (local) |
     | `inkwell bundle add <addr>` | stays in inbox, collapsed to one row |
     | server-side rule (`rules.toml`: `fromAddresses` + `moveToFolder = "Archive"`, then `inkwell rules apply --dry-run` → confirm → `apply`) | skips the inbox across ALL clients |
   - **KEEP** — leave alone; whitelist anything that survived the
     rails.

5. **Present the plan, grouped by disposition** — "unsubscribe from
   these 8 / auto-route these 5 / keeping the rest" — showing volume
   and read rate per sender so the user can sanity-check. Get
   explicit confirmation before applying anything (hard rule 2).

6. **Apply.**
   - Auto-route: run the approved commands; report per-sender
     results.
   - Unsubscribe: hand the user the TUI list — for each sender,
     "open any message from `<addr>`, press `U`, confirm" (see
     `docs/user/how-to.md` § "Get off a mailing list"). Don't do it
     for them (hard rule 1).
   - Offer the follow-up sweep for past mail via `mail-triage`
     (e.g. `inkwell filter '~f <addr>' --action delete` — irrelevant
     mail is soft-deleted per triage hard rule 2; use `archive` only
     if the back-catalog has search-later value. Triage's
     confirmation discipline applies).

7. **Report.** What was routed/ruled, the unsubscribe checklist
   handed over, anything skipped by the rails and why, and any
   `scan_truncated` caveat from the stats run.

## Anti-patterns to refuse

- **Unsubscribing on the user's behalf** by fetching unsubscribe
  URLs or composing mailto: mail — hard rule 1; the TUI modal exists
  to let a human spot phishing.
- **Standing decisions from a truncated scan.** If
  `scan_truncated` is true, narrow `--days` or raise `--limit`
  before ranking.
- **Bundling the decision.** One disposition per sender, separately
  confirmable — not "yes to all" unless the user says exactly that.
- **Treating `has_unsubscribe: ?` as "no path exists".** It usually
  means the headers were never fetched; the TUI `U` flow resolves it
  live.
- **Touching what the rails excluded** because it "looks like junk
  anyway."
