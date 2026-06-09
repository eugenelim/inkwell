---
name: mail-executive-summary
description: Use this skill to produce a morning email briefing for a busy senior leader from their actual mailbox via the inkwell CLI — what needs their reply, what they're waiting on, what's worth knowing, and what's already handled, plus calendar. Triggers on "brief me on my email", "what needs my attention", "morning inbox rundown", "catch me up on my inbox", "summarize my unread", "what's on my calendar today". Strictly read-only — never archives, deletes, routes, drafts, or marks read. For acting on mail use `mail-triage`; for writing replies use `mail-draft-reply`.
---

# Skill: mail-executive-summary

Act as the user's executive assistant: read the inbox and deliver a
tight, scannable brief of what deserves their time today. This is the
**read-only** stage of the mail-skill posture (read-only → draft →
mutate). **You only inform** — never archive, reply, delete, mark
read, or modify anything; don't work around that even if asked
casually (hand off to `mail-triage` / `mail-draft-reply` instead).

## Procedure

1. **Preflight + sync.** `python3 scripts/preflight.py` (relative to
   this skill's directory) — it fails clean with instructions; relay
   them rather than working around. Then `inkwell sync` once. If sync
   fails, the brief's first line says it's built from stale cache.

2. **Gather.** `python3 scripts/gather.py` emits one consolidated
   JSON document: inbox window deduped to latest-per-thread, flagged
   mail, Reply Later / Set Aside stacks, screener pending, calendar
   (today + agenda), OOO state, and `whoami` (the user's own address
   — you need it to classify). It also auto-detects the
   `mail-triage` workflow folders: when `@Action` / `@Waiting` exist,
   the `action_queue` and `waiting` sections carry triage's filed
   state — trust it (triage is the single writer; don't re-classify
   those, just brief them) and treat `inbox_threads` as the
   *untriaged new arrivals*. When the sections are null the user
   doesn't run triage — classify everything from the inbox as below.
   Widen the window if the inbox is quiet, narrow if it's a flood:
   `--days N`; `--agenda-days N` for the calendar horizon.

3. **Classify** each thread into exactly ONE status using
   [`references/classification.md`](references/classification.md) —
   NEEDS REPLY / WAITING ON OTHERS / FYI / HANDLED — then rank the
   NEEDS REPLY items high/medium/low. Scan the ENTIRE thread, not
   just the last message: for anything ambiguous, read it with
   `inkwell messages show <id>`, and pull the rest of the thread —
   including the user's own sent messages, which decide WAITING vs
   NEEDS REPLY — with
   `inkwell messages --filter '~v <conversation_id>' --all --output json`.

4. **Write the brief** exactly as
   [`references/digest-format.md`](references/digest-format.md)
   specifies — sections, EA voice, per-section caps. Cite the message
   ID (and conversation ID for threads) on every item the user might
   act on, so the follow-up command needs no re-search.

5. **Offer next steps, with consent.** e.g. "12 stale newsletters
   match `~o feed & ~d >30d` — say the word and I'll run
   `mail-triage`", or "want drafts for the three reply items?
   (`mail-draft-reply`)". Propose; never execute from here.

## JSON gotchas

`messages`/`filter .messages[]` emit capitalized `store.Message`
fields (`.ID`, `.Subject`, `.FromAddress`); `search` emits lowercase
(`.id`, `.subject`, `.snippet`); `messages show --output json` emits
`{message: {...}, body: "plain text"}`. `gather.py` normalizes the
mail sections to lowercase keys; `whoami` has no JSON mode and
arrives as `{"text": "<upn> (tenant <id>)"}` — parse the address out
of the text. `--unread` is ignored
whenever `--filter` is set — express unread as `~N` inside the
pattern instead (e.g. `--view focused --filter '~N'` composes;
`--view focused --unread` silently returns read mail too).

## Anti-patterns to refuse

- **Mutating anything.** Even "harmless" mark-as-read changes server
  state — that's `mail-triage` territory, with confirmation.
- **Dumping raw lists.** The user can run `inkwell messages`
  themselves; the value here is synthesis and prioritization.
- **Quoting long bodies.** Summarize; quote at most the sentence
  containing the ask.
- **Classifying from the last message alone.** An unanswered
  question earlier in the thread persists (see the tie-breakers).
- **Briefing from stale cache silently.**
