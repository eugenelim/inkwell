---
name: mail-draft-reply
description: Use this skill to draft an email reply (or reply-all / forward) in the user's voice for their review, saved to the mailbox Drafts folder via the inkwell CLI. Triggers on "draft a reply", "respond to this email", "write a response", "reply to <sender>", "answer my reply-later stack", "forward this to Bob with context", "set my out-of-office". Produces a draft for approval with a confidence label — it never sends (Mail.Send is denied by construction, PRD §3.1; the user sends from Outlook). For triaging use `mail-triage`; for read-only briefings use `mail-executive-summary`.
---

# Skill: mail-draft-reply

Draft replies to real messages in the user's voice, present them for
approval with a confidence label, and save the approved draft to the
mailbox **Drafts** folder. This is the **draft** stage of the
mail-skill posture (read-only → draft → mutate). **Never send. Only
draft.** There is no send verb to misuse — and don't simulate one by
other means even if asked; the human reviews and sends from Outlook.

## Hard rules (non-negotiable)

1. **Nothing is ever sent.** Never say "sent" — say "draft saved to
   Drafts; review and send from Outlook".
2. **Show the body in chat before saving**, unless the user has
   explicitly pre-approved a batch ("draft replies to all of these,
   I'll review in Outlook").
3. **Reply vs reply-all is a decision, not a default.** Default to
   `reply`; use `reply-all` only when the thread clearly warrants it
   or the user asked. Flag risks (big CC lists, external domains)
   before saving.
4. **Never use `--editor`** — it opens `$EDITOR` and hangs a
   non-interactive shell. Saving goes through `scripts/save_draft.py`
   (body on stdin → tempfile → `inkwell messages <verb>` → tempfile
   removed so confidential text never lingers).
5. **Do not invent information.** Ground facts, terms, statuses,
   dates, approvals, attachments, completed actions, and external
   changes in the thread or provided context.

## Procedure

1. **Preflight.** `python3 scripts/preflight.py` (relative to this
   skill's directory); it fails clean with instructions. Then
   `inkwell sync` once.

2. **Gather context.**
   - Locate the message: an ID already surfaced (by the user,
     `mail-executive-summary`, or `mail-triage`), or — when
     `mail-triage` maintains the workflow folders — the `@Action`
     queue is the natural worklist:
     `inkwell messages --folder "@Action" --output json` (absent
     folder = the user doesn't run triage; search the inbox
     instead). Also: `inkwell search 'from:alice budget' --output
     json` (lowercase fields: `.id`, `.subject`) or
     `inkwell later list`.
   - Read the FULL thread — it is the source of truth:
     `inkwell messages show <id> --headers`, then
     `inkwell messages --filter '~v <conversation-id>' --all --output json`
     for the rest, and `inkwell messages attachments <id>` so you
     know what was attached.
   - For tone, pull the user's past replies to this sender:
     `inkwell messages --folder "Sent Items" --filter '~t <addr>' --limit 3 --output json`,
     then `show` them. Use these for relationship, tone, brevity,
     and directness only — current thread facts always win.

3. **Draft** per the rules below; state the verb and recipients
   ("reply-all → Alice, Bob, +list@…").

4. **Present** the draft body in chat with a confidence label
   (below) and wait for approval (hard rule 2's batch exception
   aside). Offer to revise tone, length, or specifics.

5. **Save** the approved draft:

   ```sh
   python3 scripts/save_draft.py <message-id> <<'EOF'
   ...approved body...
   EOF
   # forwards: save_draft.py <id> --verb forward --to bob@example.com [--to ...]
   # reply-all: save_draft.py <id> --verb reply-all
   # optional:  --subject "Re: ..."
   ```

6. **Close the loop.** Confirm "draft saved — review and send from
   Outlook", and offer the bookkeeping (mutations — ask first):
   `inkwell later remove <id>` if it was in the reply-later stack;
   and if it came from `@Action`, offer
   `inkwell thread move <conversation-id> --to "@Waiting"` so that
   once the user sends the reply, `mail-follow-up` will watch it.

## Drafting rules (apply all)

- Write from the user's perspective, as the address `whoami` reports.
- Use prior-thread context to be relevant and accurate; don't ask for
  details already present there.
- Do NOT simply repeat or mirror what the last email said — continue
  the conversation or add new information. Nothing substantial to
  add → keep the reply minimal.
- Don't mention that you're an AI.
- Body only — the subject comes from the reply chain unless the user
  overrides it.
- When key context is missing, still draft the most useful reply you
  can, but lower the confidence label when it relies on assumptions
  or user-fillable details.
- Placeholders (`[TODO: …]`) sparingly — only where information is
  genuinely missing. Never a placeholder for the user's name.
- Match the sign-off habits observed in the user's sent mail; never
  add a title or contact block.
- Write in the same language as the latest message in the thread.
- No em dashes unless the user's writing style explicitly calls for
  them.
- Treat email dates as message metadata, not calendar context.
- Plain text only (that's what the compose path supports). Separate
  paragraphs with a blank line. A necessary link goes inline as a
  bare URL.

## Scheduling

- Don't name meeting times or claim availability without real
  calendar data — check `inkwell calendar agenda --days 14` (and
  `calendar show <event-id>` for specifics). With no calendar
  access, acknowledge the request and propose finding a time rather
  than naming slots.
- If the sender provides their own scheduling link or process, use
  that path instead of offering the user's.

## Writing style

Default (the user's own instructions override this):

> Concise, direct, friendly. Aim for 2 sentences unless briefly
> answering multiple questions needs more. Plainspoken, professional;
> short declarative sentences over polished phrasing. Don't be pushy.

## Confidence label (present with every draft)

- **HIGH** — complete, grounded, depends on no missing facts,
  calendar/business state, assumptions, or user-fillable details.
- **MEDIUM** — useful but relies on reasonable assumptions, missing
  facts, or details the user must fill in.
- **LOW** — highly uncertain, likely needs broader thread review, or
  mainly asks/checks/follows up.

## Out-of-office (the one true auto-reply)

Automatic replies are the only machine-sent mail in scope, and they
go through mailbox settings, not drafts:

```sh
inkwell ooo                       # show current state
inkwell ooo on --until 2026-06-16 # bare date or full RFC3339 (2026-06-16T09:00:00+08:00)
inkwell ooo set ...               # update text without changing status
inkwell ooo off
```

Show the exact internal/external message text in chat and get a yes
before enabling — OOO replies are outward-facing.

## Anti-patterns to refuse

- **Drafting from the envelope alone.** Always read the message and
  thread first; a reply that ignores half the thread is worse than
  no draft.
- **Reply-all by reflex** — hard rule 3.
- **Inventing the user's commitments** ("happy to join Thursday")
  without checking the calendar or asking.
- **Overwriting doubts with confidence.** Sensitive topic, angry
  thread, uncertain tone → label LOW, say so, and let the user steer
  before saving.
- **Bypassing `save_draft.py`** with ad-hoc tempfiles that might be
  left behind.
