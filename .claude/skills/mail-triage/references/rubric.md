# Triage rubric

The classification logic, kept out of `SKILL.md` so the main file
stays short. Tune verdicts and thresholds here without touching the
workflow.

## Data contracts

The only assumptions this rubric makes about the data:

**`scripts/survey.py`** emits one JSON document with a `threads`
array (latest message per conversation) and a `folder_setup` map
(which workflow folders exist):

```json
{"id":"<opaque>","conversation_id":"<opaque>","received":"<ISO-8601>",
 "from":"addr","from_name":"Name","subject":"...","snippet":"first ~200 chars",
 "unread":true,"importance":"normal","thread_msgs":2,
 "keep_rails":["flagged","attachments","meeting-request","financial-record"],
 "archive_hints":["routed-feed","routed-paper_trail","inference-other",
                  "bulk-sender","automated-platform"]}
```

`keep_rails` / `archive_hints` are *mechanical* signals,
pre-computed deterministically; everything below is judgment.

**`inkwell messages show <id>`** prints the full plaintext body
(HTML already converted by the CLI).
**`inkwell messages --filter '~v <conversation_id> & ~f <own-addr>' --all`**
answers "is the user a participant in this thread?".

## Verdicts (assign exactly ONE per thread)

Decide whose turn it is across the WHOLE thread, not just the last
message:

- **To Reply** — the user needs to respond: a direct question or
  request to them, an unanswered question anywhere in the thread, or
  a follow-up/deliverable they promised and haven't sent. If they
  sent the last message but still owe something, it's still To Reply.
- **Awaiting Reply** — the user asked or delegated and is waiting;
  the ball is in the other court. If the request was already
  fulfilled, it's no longer awaiting.
- **Calendar** — meeting invites and scheduling the user must see or
  RSVP to.
- **FYI** — information received, no question or request anywhere in
  the thread. If the user sent the last message, it cannot be FYI.
- **Done** — concluded; nothing pending for anyone.
- **Receipt** — purchase confirmations, payment receipts, invoices,
  transaction/financial/legal/tax records. (Payment *reminders*,
  *overdue* and *renewal* notices are NOT records — they're usually
  To Reply or a worthless notification.)
- **Newsletter** — regular content from subscriptions.
- **Marketing** — promotional mail about products, sales, offers.
- **Notification** — alerts, status updates, system messages.
- **Cold Email** — unsolicited pitches: selling, recruiting,
  partnership requests. NOT cold: investors, friends/colleagues,
  people the user has corresponded with, intros, customers, password
  resets, welcome emails, receipts, calendar invites.

## Placement (verdict → destination)

Workflow folders encode whose turn it is; **soft delete is the
default for bulk** — filing bulk is the exception, not the rule:

| Verdict | Destination |
| --- | --- |
| To Reply | `@Action` (offer `mail-draft-reply`) |
| Awaiting Reply | `@Waiting` (where `mail-follow-up` looks) |
| Calendar | `@Action` if an RSVP/decision is pending; `Archive` once handled or past |
| FYI | surface it in the plan, then `Archive` (it's done once seen) |
| Done | `Archive` |
| Receipt | `Reference` |
| Newsletter / Marketing / Notification / Cold Email | **soft delete** (→ Deleted Items, recoverable) — UNLESS the user has said to keep that sender/class, then `Newsletters` |

Degraded mode (user declined the folder setup): To Reply / Awaiting
/ pending-Calendar stay in the Inbox; everything else files or
deletes as above.

For recurring bulk senders, also propose the durable fix —
`route assign <addr> feed`, `screener reject`, or a server-side
rule — so next week's triage is smaller.

## Keep-rails (never soft-delete or archive if ANY holds)

Checked before any removal decision — mechanical ones arrive in
`keep_rails`; the judgment ones are yours:

- Flagged by the user (`flagged`).
- Has attachments (`attachments`) — file, don't delete.
- Meeting request for a **future** event (`meeting-request`).
- Real financial record (`financial-record`) → `Reference`.
- The user is a sender in the thread (participant check above).
- Anything verdicted To Reply / Awaiting Reply / pending Calendar.

## Tie-breakers and guards

- **Person vs. system.** A human sender outranks an automated one at
  the same signal strength; no-reply / do-not-reply addresses are
  system.
- **Direct vs. CC.** Addressed directly → at least To Reply
  consideration; CC-only with no direct ask → at most FYI.
- **Ambiguity rule.** Can't tell To Reply from FYI? Choose To Reply
  (`@Action`) and say "unclear if a reply is expected." Unsure
  between filing and deleting → file. Prefer leaving something in
  `@Action` over misfiling it.
- **An unanswered question earlier in the thread persists** even if
  the latest message is informational → still To Reply.
- **Never infer urgency from a sender's title or seniority** — only
  from content and whether someone is actually blocked.
- **No mailbox writes during classification.** Reading bodies is
  allowed; moving, deleting, or categorizing is not (that's the
  execute step, after confirmation).

## Project tagging (optional, closed-set, abstain-first)

Topics never get folders — they get additive **categories**, and
only if the user maintains an approved project list
(`name + one-line definition`):

- Mechanism: a one-time custom action per project in
  `~/.config/inkwell/actions.toml` —
  `steps = [{ op = "add_category", params = { name = "<Project>" } }]`
  — then `inkwell action run tag_<project> --message <id>`.
- Apply a project category ONLY from the approved list, ONLY when
  the email clearly fits one project's definition with high
  confidence. Otherwise apply none — prefer NOT tagging over
  mis-tagging.
- Never create a project category. If a new topic seems to be
  emerging, don't act — collect examples and **suggest** it for the
  user to approve.
- A project tag is additive on top of placement; it never changes
  where the message is filed.
- When the user corrects a tag, record the sender/pattern in the
  plan so it's deterministic next time.
- Never touch the `Inkwell/*` categories (stack storage).

## What a good plan line contains

For To Reply / Awaiting Reply only:

- One line, plain language, the user's domain terms.
- The single decision or action requested, if any ("wants a yes/no
  on the vendor by Thu").
- Omit greetings, signatures, quoted history, opaque IDs and
  tracking tokens. Never invent facts not in the body; if the body
  is empty or unreadable, say so rather than guessing.
