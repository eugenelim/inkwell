---
name: mail-meeting-prep
description: Use this skill to prepare a concise briefing on the external people the user is about to meet — who they are, their company, recent discussions, and anything outstanding — via the inkwell CLI (Graph calendar view + cached mail), optionally with web research. Triggers on "prep me for my meetings", "who am I meeting today", "brief me before this call", "tell me about <person> before our meeting". Read-only: never sends, replies, or modifies the calendar. For inbox briefings use `mail-executive-summary`.
---

# Skill: mail-meeting-prep

Prepare a concise briefing about the **external guests** the user is
meeting. Read-only — gather and summarize only; this is the
**read-only** stage of the mail-skill posture. Focus on what's
actually useful in the minutes before a meeting.

## Procedure

1. **Preflight + sync.** `python3 scripts/preflight.py` (relative to
   this skill's directory; fails clean with instructions), then
   `inkwell sync` once (freshens the cached mail history; the
   calendar itself is read live from Graph).

2. **Find the meetings and their guests.**
   `python3 scripts/guests.py` pulls the calendar view (`--days 1` =
   today, default; more for a horizon — or target one meeting the
   user named via `inkwell calendar show <event-id>`), fetches each
   event's attendees, and classifies **external guests** by email
   domain vs the user's own (skipping the user, internal colleagues,
   and room/resource attendees). It also attaches each guest's
   recent cached mail (most recent ~10). Company hint = the email
   domain (`john@acme.com` → Acme).

3. **Deepen context per guest** where the snippets aren't enough:
   - Read the decision-bearing messages: `inkwell messages show <id>`.
   - More history: `inkwell messages --filter '~f <addr> | ~r <addr>' --all --output json`
     (cached mail only — the CLI has no calendar search; past
     *meeting* traffic shows up in this mail history as invite/
     response messages).
   - **Professional background — only if web search is available:**
     current role, company, LinkedIn. Use the email domain to
     identify the company and include it in queries to disambiguate
     common names; try more than one query if the first is weak. If
     results are uncertain (common name, conflicting info), say so
     rather than guessing.
   - A guest with **no mail history and no web result** is a **new
     contact** — say that; never invent background.

4. **Write the briefing.** Per guest, **fewer than 10 bullets, max
   ~10 words per bullet**, in priority order: role and company →
   recent discussions → **pending or outstanding items** (open
   questions, things owed either way, follow-ups — the
   highest-value part).

   Rules:
   - Do NOT repeat meeting logistics (time, date, location) — the
     user has those.
   - Only the specific guest — not other attendees, not the user's
     colleagues.
   - Note identity uncertainty explicitly ("couldn't confirm").
   - Ground everything in gathered context or cited research; never
     fabricate a role, company, or background.

## Output format

> **Meeting prep — {meeting title}, {time}**
>
> **{Guest name}** · {role} at {company} · *{email}*
> - {bullet}
> - {bullet}
> - Outstanding: {anything pending between you}
>
> **{Next guest}** …
>
> *{If any guest is a new contact or identity is uncertain, flag it here.}*

Keep the whole brief skimmable in a minute. Several meetings → lead
with the soonest. Purely internal meeting (no external guests) → say
so briefly and move on.

## Anti-patterns to refuse

- **Any mutation.** No replies, no calendar changes, no read-state
  changes — hand off to `mail-draft-reply` / `mail-triage` instead.
- **Briefing on internal colleagues** unless the user explicitly
  asks.
- **Fabricated identity.** A wrong "VP of Sales at Acme" is worse
  than "couldn't confirm role".
- **Dumping the mail history.** Distill; the user has one minute.
- **Stale-cache mail history without saying so** (sync failed →
  caveat).
