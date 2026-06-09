---
name: mail-follow-up
description: Use this skill to find threads where the user is waiting on someone else and hasn't heard back, then draft polite nudges to chase a response — via the inkwell CLI. Triggers on "follow up on", "chase", "nudge", "what am I waiting on", "remind people who owe me a reply". Drafts for approval, saved to the Drafts folder — it never sends (Mail.Send is denied by construction, PRD §3.1; the user sends from Outlook). The action counterpart to `mail-executive-summary`'s "waiting on others" section.
---

# Skill: mail-follow-up

Surface conversations where the user sent the last message and hasn't
gotten a reply, then draft a brief, polite nudge for each. **Draft
for approval — never send** (there is no send verb; don't simulate
one). This is the **draft** stage of the mail-skill posture.

## Procedure

1. **Preflight + sync.** `python3 scripts/preflight.py` (relative to
   this skill's directory; fails clean with instructions), then
   `inkwell sync` once.

2. **Find what's gone quiet.** `python3 scripts/find_quiet.py`
   auto-detects the workflow setup: if `mail-triage` maintains an
   `@Waiting` folder it scans the conversations filed there (triage
   is the single writer of that state — this skill only reads it);
   otherwise it falls back to scanning recent Sent Items (the user
   chose not to run triage — that's fine). Force either with
   `--mode waiting|sent`; localized mailboxes: add
   `--sent-folder "<localized Sent Items name>"`. Either way it
   reports conversations where the user sent the last message,
   nothing newer arrived, and it's been quiet ≥ `--quiet-days 3`
   business days (adjust if the user gives a window). Each candidate
   carries `quiet_business_days`, recipients, a snippet, and
   `has_draft` — **skip any candidate with `has_draft: true`**; a
   pending draft in the thread means it was already chased (or a
   reply is mid-composition). Don't re-nudge the same thread twice
   in a short window.

3. **Filter out what doesn't need chasing.** Read the user's last
   message (`inkwell messages show <last_message_id>`) — and the
   thread (`inkwell messages --filter '~v <conversation_id>' --all`)
   when the snippet isn't enough. Exclude:
   - Purely informational closers (an FYI, a thank-you, a "sounds
     good") with no outstanding question, request, or pending answer.
   - Threads the user closed or took ownership of ("I'll handle it").
   - Bulk/automated recipients, newsletters, no-reply addresses.
   - Anything where a reply genuinely isn't expected.

   Only chase a real outstanding ask: an unanswered question, a
   deliverable owed to the user, or a request awaiting action.

4. **Draft a nudge per surviving candidate.** You're writing because
   the user sent the last message and hasn't received a reply; the
   purpose is to politely check in. Write from the user's
   perspective. Rules:
   - **1–3 sentences.** A check-in, not a new email.
   - Acknowledge you're following up; gently remind the recipient of
     the outstanding matter. Do NOT repeat the previous email.
   - Polite and friendly. **Don't be pushy.**
   - Don't mention that you're an AI. Body only. Same language as
     the thread. Match the user's known style; otherwise short,
     plainspoken, professional.
   - Natural openers (English): "Just checking in on this", "Wanted
     to follow up on my previous email", "Circling back on this."
   - Plain text only.

5. **Present, confirm, save.** Show the list grouped by staleness /
   importance — each item as *"{recipient} — {what's outstanding} —
   quiet {N} days"* plus the drafted nudge. Let the user edit, skip,
   or approve each. Then save each approved nudge as a
   **reply-all** draft on the user's own last message — `reply-all`
   is load-bearing: Graph's plain `createReply` on a self-sent
   message addresses its *sender* (the user!); `reply-all` addresses
   the original recipients, with the user's own address deduped
   server-side:

   ```sh
   python3 scripts/save_draft.py <last_message_id> --verb reply-all <<'EOF'
   ...approved nudge...
   EOF
   ```

   Sanity-check the saved draft's recipients
   (`inkwell messages show <draft-id> --headers`) against the
   candidate's `to` list before telling the user it's ready.

6. **Report.** Which threads got a nudge draft ("review and send
   from Outlook"), which were skipped and why (no outstanding ask /
   already drafted / automated sender).

## Anti-patterns to refuse

- **Nudging a thread with no genuine outstanding ask.**
- **Double-chasing** — `has_draft: true`, or the user nudged
  recently; the draft sitting unsent in Drafts is the tell.
- **Pushy escalation** ("this is my third email…") unless the user
  explicitly asks for a firmer tone.
- **Claiming anything was sent.** Drafts only; the human sends.
- **Chasing from a stale cache silently** — sync first, caveat
  otherwise.
