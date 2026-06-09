# Thread classification

Decide each thread's status from the USER's perspective. Scan the
ENTIRE thread, not just the last message. Exactly one status per
thread.

**Shortcut when mail-triage runs:** the gather document's
`action_queue` (= `@Action`) maps to NEEDS REPLY and `waiting`
(= `@Waiting`) to WAITING ON OTHERS — triage already decided whose
turn it is; don't re-litigate, just rank and write. Apply the full
classification below only to `inbox_threads` (untriaged arrivals) —
or to everything when those sections are null.

## Statuses

- **NEEDS REPLY** — the user has to respond. Use when: someone asks
  them a direct question; requests info/action from them; there's any
  unanswered question/request they haven't addressed; OR they
  promised a follow-up/deliverable and haven't sent it. In
  multi-person threads, track only the user's own commitments.
- **WAITING ON OTHERS** — the ball is in someone else's court: the
  user asked a question or requested something and hasn't received
  it, or someone else owes a deliverable. Note how long it's been
  quiet.
- **FYI** — information the user RECEIVED and should know, with no
  question or request anywhere in the thread. (If the user SENT the
  last message, it is NOT FYI.)
- **HANDLED** — concluded; all questions answered / requests
  fulfilled, nothing pending.

## Tie-breakers (these catch the common mistakes)

- An unanswered question earlier in the thread persists even if the
  latest message is just informational → still NEEDS REPLY.
- Someone else promised something → WAITING. The user promised
  something → NEEDS REPLY.
- If the user's last reply takes ownership ("I'll handle it") →
  HANDLED, unless it promised a further update/deliverable.
- A clarifying question that got answered does NOT cancel a pending
  commitment → still NEEDS REPLY.
- Use FYI only when absolutely nothing is pending anywhere in the
  thread.

## Ranking within NEEDS REPLY

Mark each **high / medium / low** by: direct ask from a real person
> time-sensitive or deadline-bearing > senior/important sender or
known contact > everything else. Newsletters, marketing, cold emails,
and automated notifications are never high.

## Guards

- The user's own address comes from the gather document's `whoami`
  section (a text blob, `"<upn> (tenant <id>)"` — parse the address
  out) — classify against it, not against guesses.
- Never infer urgency from a sender's title alone — only from content
  and whether someone is actually blocked.
- This pass is read-only: reading bodies is allowed; changing read
  state or anything else is not.
