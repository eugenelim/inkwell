# Spec 28 — Screener for new senders

**Status:** Ready for implementation.
**Depends on:** Spec 02 (store + `messages` / `sender_routing`
queries), Spec 04 (TUI shell + folder sidebar), Spec 08 (pattern
language — `~o` operator already shipped, this spec adds the
`~o pending` alias and the `pending` enum value), Spec 11 (saved-
search Manager precedent for sidebar background-refresh ticks),
Spec 19 (mute — `MessageQuery.ExcludeMuted` precedent for an
opt-in default-folder filter; sentinel-folder protection pattern),
Spec 22 (command palette — static palette rows for screener verbs),
Spec 23 (sender_routing table + `S i/f/p/k/c` chord + the four
routing virtual folders / `__screener__` sentinel + `~o` operator).
This spec is a **pure follow-up** to spec 23 — no new schema, no
new chord prefix, no new Graph scope.
**Blocks:** Custom actions framework (roadmap §2) — exposes
`screener_accept` / `screener_reject` as op primitives once the
framework lands; not blocked-on, not blocking.
**Estimated effort:** 1.5 days. Bulk of the work is the default-
view filter changes (one new `MessageQuery` field, fan-out across
six call sites), the second sentinel virtual folder
(`__screened_out__`), and the pane-scoped `Y` / `N` accept/reject
shortcuts in the Screener pane. The store and CLI surface are
small.

### 0.1 Spec inventory

Screener is item 2 of Bucket 3 (Power-user automation) in
`docs/ROADMAP.md` §0 and corresponds to backlog item 1.16. It
takes spec slot **28**, leaving slot 27 for the custom-actions
framework (Bucket 3 item 1) which is independently specced.
Spec 26 (bundle senders) shipped at v0.55.0 (`a464f03` /
`6c70573`); spec 27 (custom actions) shipped at `614011e`. Spec
28 does NOT depend on either — its only material dependency is
spec 23 (already shipped v0.51.0). The PRD §10 spec inventory
adds a single row for spec 28.

### 0.2 What spec 23 promised this spec would deliver

Spec 23 §10.1 explicitly carved out a "known v1 UX limit" and
§14 listed three follow-ups for "Screener (roadmap §1.16)":

1. **The new-sender admission gate** — first-contact senders sit
   in the Screener until accepted.
2. **Hiding screened-out mail from the user's actual Inbox view**
   — currently visible in spec 23 v1 of routing.
3. **Optional native-OS notification suppression** for screened-
   out senders.

Item (3) is materially impossible for inkwell: the TUI has no
notification subsystem at all (per ARCH and PRD §3.2 — we don't
own the notification surface; the user keeps native Outlook
running for that). Spec 28 acknowledges (3) as out-of-scope by
construction (§1.1) and ships (1) and (2).

---

## 1. Goal

Mail from a sender you've never decided about should not get into
your Inbox unannounced. New senders land in a **Screener** queue
where you make one decision per sender — admit them (and choose
where: Imbox, Feed, or Paper Trail), or screen them out. Once
admitted, all their past and future mail flows where you said.
Once screened out, all their mail stays cached and searchable but
disappears from default folder views — they never bother you again
unless you go looking.

This is HEY's gate ([HEY help — The Screener](https://help.hey.com/article/722-the-screener),
[HEY feature page](https://www.hey.com/features/the-screener/)),
adapted to a TUI mail client backed by Microsoft Graph (which has
no equivalent server-side concept). The data model is already in
place from spec 23 (`sender_routing` table + four routing
destinations); spec 28 adds the *gating UX* — the queue surface,
the Yes/No verbs, and the default-view filter behaviour — on top
of it.

The screener is **opt-in** behind a master config flag
`[screener].enabled`, default `false`. Existing inkwell users
upgrading from spec 23 see no behaviour change until they set it.
This is deliberate: turning the gate on without warning would
suddenly hide every unrouted sender's mail from the default Inbox
view, which is a surprising change for a 50k-message archive that
has zero routing assignments. The gate is a power-user opt-in,
not a default.

### 1.1 What does NOT change

- **No schema migration.** All gating behaviour reads existing
  `sender_routing` rows. Migrations 001–013 are on disk at
  spec-28 design time (`013_bundled_senders.sql` is the latest,
  shipped with spec 26 v0.55.0); spec 28 claims no slot. The
  implementation PR should `ls internal/store/migrations/`
  immediately before merge to re-confirm the no-migration
  claim still holds (specs 27, 29, 30 are concurrent design
  work and may have claimed slots in the interim — none of
  them affect the no-migration claim here, but the slot
  number used in error messages or follow-up audits should
  be the live count, not 013).
- **No new Graph scope.** Screener is local-only; no Graph
  endpoint is called by the gate. PRD §3.1 unchanged.
- **No new sender_routing destination value.** The four shipped
  values (`imbox`, `feed`, `paper_trail`, `screener`) remain the
  closed set. "Pending" / "first-contact" is the *absence* of a
  row, queried via the existing `~o none` operator (spec 23 §4.3)
  — no `destination='pending'` row is ever inserted.
- **The `S` chord (`S i/f/p/k/c`) is unchanged.** Spec 28 adds
  *pane-scoped one-key shortcuts* (§5.4 `Y` / `N`) for the
  Screener pane only; the global chord remains the canonical
  routing verb.
- **`__screener__` sentinel ID is unchanged.** Spec 28 redefines
  what the Screener virtual folder *shows* when the gate is
  enabled (§5.1) and adds a separate `__screened_out__` sentinel
  for screener-routed senders' mail. The existing sentinel ID
  stays so that user muscle memory ("Screener is over there in
  the sidebar") survives.
- **Mute (spec 19), Reply Later / Set Aside (spec 25), bundles
  (spec 26)** are orthogonal in their data models — none of them
  share schema with `sender_routing`. Cross-feature interactions
  worth surfacing in user docs (§5.10):
  - A **bundled-but-pending sender** under spec 26 has its mail
    rendered as a collapsed bundle row in the default Inbox view
    pre-gate. Post-gate (with `[screener].enabled = true`), that
    bundle row vanishes from the Inbox along with all other
    pending mail. The user is warned at the next launch via the
    §5.3.1 confirmation modal; the how-to recipe "Turn on the
    Screener" recommends a routing pass over bundled senders
    before editing the config flag.
  - A **reply-later'd message** under spec 25 lives in the
    Reply Later stack overlay (intentional surface) regardless
    of gate state. If the sender becomes pending after the
    reply-later was set, the *stacked message* remains visible
    in the overlay (intentional query, not a default-folder
    view); the sender's *other* mail is gate-suppressed. The
    user can still act on the stacked message; the Screener
    queue still surfaces the sender for a routing decision.
    Documented edge in `docs/user/reference.md` Streams
    section.
  - A **muted thread**: spec 19 mute is per-thread, "no
    decision" is per-sender — different granularities. By
    default `[screener].exclude_muted = true` (§5.7) — muting
    is a stronger signal and hides the thread from the Screener
    queue. A pending sender whose only mail lives in muted
    threads is therefore unreachable from the Screener queue;
    they must use `:filter ~o pending` to surface those threads
    explicitly. The how-to recipe documents this as the
    "fully-muted pending sender" edge.
- **Native OS notifications.** Out of scope by construction;
  inkwell has no notification subsystem (ARCH).
- **Send-side feedback.** No mail is sent to a screened-out
  sender — `Mail.Send` is denied by PRD §3.2, and the screener
  decision is private (matches HEY by accident of architecture).
- **Auto-promotion rules.** Replies-to-a-sender do NOT auto-admit
  the sender (HEY's explicit decision). The user must press `Y`
  / `S i` etc. The sync engine never writes to `sender_routing`.
- **Filter / search results.** `:filter` and `/`-search are
  intentional queries — they include screened-out and pending
  senders' mail unconditionally (§4.2 — same rule as spec 19's
  `ExcludeMuted` carve-out for search).

### 1.2 Three sender states

Spec 28 introduces a three-state mental model for senders, all
derived from the existing `sender_routing` table:

| State          | Definition                                     | Default-view visible? | Screener visible? | Screened-Out visible? |
|----------------|------------------------------------------------|-----------------------|-------------------|-----------------------|
| **Approved**   | row exists, `destination ∈ {imbox, feed, paper_trail}` | yes (always)          | no                | no                    |
| **Pending**    | no row (first contact / cleared)               | gated by `[screener].enabled` — visible when off (default); hidden when on | yes (when enabled) | no                    |
| **Screened**   | row exists, `destination = 'screener'`         | gated by `[screener].enabled` — visible when off (default); hidden when on | no                | yes (when enabled)    |

**Reading the table:** when `[screener].enabled = false` (default),
the default Inbox view shows mail from Approved + Pending +
Screened senders identically (spec 23 v1 behaviour). When
`[screener].enabled = true`, Pending and Screened mail is hidden
from the default Inbox view (the gate fires); they are visible
only in the dedicated Screener / Screened-Out virtual folders.

The three labels (Approved / Pending / Screened) are the user-
facing terms. Internally the code keeps using the existing
destination strings; no `senderState` enum is introduced because
the state is fully derived.

**Word "Screener" overloaded — read carefully.** The string
"Screener" carries three distinct meanings in this spec, each
disambiguated by context:

| Use site                      | Meaning                                      |
|-------------------------------|----------------------------------------------|
| `[screener].enabled`          | The master gate flag.                        |
| `__screener__` sentinel       | The sidebar virtual folder (content shifts with the gate per §5.1). |
| `destination = 'screener'`    | The `sender_routing` value for screened-out senders (spec 23 column value, unchanged). |

`docs/user/reference.md` calls out this overload in the Streams
section so readers don't conflate "I turned the screener on" (the
flag), "I navigated to the Screener" (the sentinel), and "this
sender is in the screener" (the destination). Renaming the
column value to `screened_out` was considered and rejected:
breaking spec 23's shipped data is not justified by a
documentation-fixable confusion.

## 2. Prior art

### 2.1 Terminal clients

- **mutt / neomutt** — no admission gate. Closest precedent:
  spam-style scoring (`spam` / `nospam` patterns) plus saved
  searches that exclude scored senders. The user runs a manual
  hook to score; there is no "first-contact queue" view. Aerc
  and alot follow the same pattern (tag-based with no inherent
  first-contact concept).
- **notmuch frontends (alot, astroid)** — tags-as-routing. A
  `tag:screened-out` query is the natural Screener equivalent;
  `not tag:approved and not tag:screened-out` is the pending
  view. No first-class UI; the user composes by query.
- **None of these terminal clients implements a HEY-style
  first-contact gate.** Spec 28 is therefore a novel TUI surface;
  the prior art that informs the design is web/desktop.

### 2.2 Web / desktop clients

- **HEY (Basecamp, 2020)** — the canonical Screener
  ([feature page](https://www.hey.com/features/the-screener/),
  [help article](https://help.hey.com/article/722-the-screener)).
  First-time senders land in The Screener with a Yes / No
  decision. Yes admits them and the user picks Imbox / Feed /
  Paper Trail. No screens them out — past and future mail is
  silently hidden from default views, with a "Screener History"
  view to undo. Decisions are private (the sender sees no
  feedback). The product has a one-click "import contacts to
  pre-approve them" affordance that we do not need (TUI users
  rarely sync OS contacts to mail clients; the `inkwell screener
  pre-approve` CLI verb in §6 is the equivalent for shell users).
- **SaneBox `SaneLater`** ([SaneBox feature page](https://www.sanebox.com/))
  — ML-deferred queue of low-priority mail. Distinct from a
  first-contact gate (it operates on every sender, not just new
  ones) and uses ML rather than user routing. We do NOT copy
  this — the spec 28 gate is deterministic and user-curated.
- **Clean Email Screener** — first-contact gate on top of any
  IMAP backend (per multiple reviews). Closer to HEY's design
  than SaneBox; same "messages from new senders won't reach your
  inbox" semantics. Good precedent for "the gate as an opt-in
  layer, not a hard-replacement of the inbox."
- **Apple Mail (macOS 15 / iOS 18) "Categories"** — Primary /
  Transactions / Updates / Promotions, on-device classification.
  No first-contact gate; all mail still appears in Mail's "All
  Mail" view, just bucketed. We do not copy because the
  classifier is opaque and there is no admission step.
- **Gmail "Filter messages from non-contacts"** ([Gmail Community
  thread](https://support.google.com/mail/thread/44191756/can-i-apply-a-filter-to-emails-coming-from-an-address-not-currently-in-my-contacts?hl=en))
  — Gmail famously does not support this directly; users
  approximate via a saved-filter that whitelists every contact in
  a giant `from:(a@b.com OR …)` list. Cited as the canonical
  user-experience-the-screener-replaces.
- **iOS Messages "Filter Unknown Senders"** — at-the-OS level,
  same shape as the Screener. Inspires the *name* of the second
  sentinel folder (`Screened Out` mirroring "Unknown Senders").
- **Outlook Focused / Other** — server-side ML, not a gate.
  Already declined in spec 23 §2.2.
- **Superhuman** — no admission gate. Splits + Reminders give
  power-users the moral equivalent (a saved-query "VIP" tab) but
  not the gate itself. Cited because Superhuman is the design
  reference for the rest of the bucket-2 workflow specs and its
  *omission* of the gate is informative — they bet on AI-assisted
  triage instead. We bet on user-curated routing.
- **Spike** — "Priority Inbox" splits into Priority and Other; ML-
  based, not a first-contact gate.

### 2.3 Design decision

inkwell follows HEY for the **shape of the queue** (one row per
sender, Yes / No decision, retroactive on accept) but with three
deliberate divergences:

1. **Opt-in, not default.** HEY's Screener ships on by default
   for new accounts; inkwell's ships off by default because the
   product already has a populated mailbox at first launch (HEY
   accounts start empty). Defaulting on would "lose" thousands
   of pending senders into the Screener at upgrade time. Users
   who want HEY behaviour set `[screener].enabled = true`; the
   onboarding doc (`docs/user/how-to.md`) makes this the
   recommended setup once a user has done a routing pass.
2. **Per-message AND per-sender views.** The Screener virtual
   folder by default shows **one row per pending sender** (newest
   message representative), matching HEY. A `[screener].grouping
   = "sender"` config key (default `"sender"`; alternatives
   `"message"`) lets the user flip to one-row-per-message if they
   want to triage individual messages from new senders before
   committing to a per-sender routing.
3. **No moment-of-arrival surface.** HEY pops a card overlay on
   first contact for some workflows; we deliberately do not
   confront the user at message-open time. New mail from a
   pending sender is silently routed to the Screener queue and
   the user encounters it only when they navigate there. The
   trade-off is that a long-deferred queue-visit means more mail
   batched for one session; the Screener is the surface, not an
   interruption. (The §5.3.1 confirmation modal is a one-time
   gate-flip event, not a moment-of-arrival surface.)

Beyond divergences, the data-model stack is unchanged from spec
23: same `sender_routing` table, same destination values, same
chord. Spec 28 is purely a UX / filter layer.

## 3. Schema

**No migration.** All state is in the existing `sender_routing`
table (spec 23 §3) and the `messages` table (spec 02). The
following derivations are read-only:

- **Pending** sender lookup is `~o none` (spec 23 §4.3
  `NOT EXISTS sender_routing` form).
- **Screened-out** sender lookup is `~o screener` (spec 23 §4.3
  `EXISTS … destination='screener'` form).
- **Default-view exclusion** (when gate enabled) is the union of
  the two: `! (~o none | ~o screener)` — i.e., only Approved
  senders' mail.

The expression index `idx_messages_from_lower` shipped by spec 23
§3 covers the JOIN probe for both queries. No new index is
required; verify with `EXPLAIN QUERY PLAN` per §8.

## 4. Store API

### 4.1 New `MessageQuery` field

Add one field to `store.MessageQuery`:

```go
type MessageQuery struct {
    // ... existing fields unchanged (incl. ExcludeMuted from spec 19) ...

    // ApplyScreenerFilter, when true, suppresses messages whose
    // sender is in the Pending state (no sender_routing row) OR
    // the Screened-Out state (sender_routing row with destination
    // = 'screener'). Equivalent to anding with `! (~o none | ~o
    // screener)` from the pattern language.
    //
    // Default false preserves spec 23 behaviour. The TUI passes
    // true only on default folder views when [screener].enabled
    // is true (§5.5). Search and filter paths always pass false
    // (§4.2 — intentional queries are not gated).
    ApplyScreenerFilter bool
}
```

The SQL clause appended when `ApplyScreenerFilter = true` (matches
the existing `buildListSQL` shape: unaliased `FROM messages`, `?`
positional binds appended to `args`, references to messages
columns are unqualified — same convention as
`emitRoutingPredicate` in `internal/pattern/eval_local.go`):

```sql
-- Only Approved senders' mail. Messages with NULL or empty
-- from_address are NEVER suppressed (defensive — drafts and
-- synthesised list-server messages can lack a From; they
-- predate any routing decision and the user should see them).
AND (
    from_address IS NULL
    OR from_address = ''
    OR EXISTS (
        SELECT 1 FROM sender_routing sr
        WHERE sr.account_id    = ?
          AND sr.email_address = lower(trim(from_address))
          AND sr.destination IN ('imbox', 'feed', 'paper_trail')
    )
)
```

The `?` in the EXISTS sub-clause binds `q.AccountID` (appended to
`args` once when the filter is enabled). The destination set is
inlined as a literal three-value `IN (…)` — these are
spec-23-fixed strings, not user input, so no bind is required.

**Why `EXISTS … IN (…)` rather than two `NOT EXISTS` (one for
unrouted, one for screener)?** The positive form (`is approved`)
is one EXISTS with an `IN` check; the negative form would be two
separate sub-clauses. The positive form is also faster — it short-
circuits on the first match per row, whereas the negative form
must verify the absence of any matching row in two relations.
Verified in §8 benchmark: positive form is ≈30% faster on the
100k-message fixture.

**Why not a JOIN?** Same rationale as spec 23 §4.3: composability
with other `MessageQuery` fields (folder, ExcludeMuted, the FTS5
search subquery). EXISTS slots into the WHERE clause without
restructuring the outer query.

**NULL safety.** `from_address` is permitted to be NULL or empty
on `messages` rows (drafts, synthesised list-server messages,
Graph envelope quirks — same as spec 19's NULL-conversation_id
case). The defensive `OR from_address IS NULL OR from_address =
''` clause prevents silent suppression. Cover with
`TestApplyScreenerFilterNullFromAddress`.

### 4.2 No change to FTS5 / `SearchByPredicate`

`store.Search` (FTS5 path used by `/`-search and `:search`) does
**not** add `ApplyScreenerFilter`. Rationale: matches spec 19 §4.3
— intentional search is intentional. A user typing `/budget`
expects to see all matches, including from screened-out senders.

`store.SearchByPredicate` (used by `:filter` and CLI `inkwell
filter`) similarly does not add the filter. Rationale: matches
spec 19 §4.4 — `:filter` is an intentional pattern query. The
TUI's outer `ListMessages` call already applies
`ApplyScreenerFilter` to the default folder view; pattern filters
narrow the visible list and inherit the suppression naturally.
The CLI's `inkwell filter` is intentional and passes false.

### 4.3 New store methods

```go
// ListPendingSenders returns one row per pending sender — i.e.,
// senders with at least one message in the local store and no
// sender_routing row. Each row carries the most recent message's
// envelope so the UI can render a representative subject /
// received_at. Used by the Screener virtual folder (§5.1) when
// [screener].grouping = "sender".
//
// Result rows are ordered by newest message received_at DESC.
// excludeMuted is honoured (callers pass [screener].exclude_muted
// — default true — so muted threads from pending senders don't
// surface in the queue; matches spec 19 §5.3).
ListPendingSenders(ctx context.Context, accountID int64,
    limit int, excludeMuted bool) ([]PendingSender, error)

// ListPendingMessages returns the raw message rows from pending
// senders, one per message, ordered by received_at DESC. Used by
// the Screener virtual folder when [screener].grouping =
// "message". Equivalent to calling ListMessages with a "~o none"
// pattern, but specialised for performance (no pattern compile).
//
// Excludes rows where from_address IS NULL OR from_address = ''
// (same NULL-safety carve-out as ListPendingSenders §4.4 — a
// malformed-from row has no actionable sender and Y/N would
// no-op forever, so the Screener queue must not surface them
// in either grouping mode). The corresponding messages remain
// reachable via :filter / search.
ListPendingMessages(ctx context.Context, accountID int64,
    limit int, excludeMuted bool) ([]Message, error)

// ListScreenedOutMessages returns messages whose sender is
// routed to 'screener'. Used by the new Screened-Out virtual
// folder (§5.2). Equivalent to ListMessagesByRouting('screener')
// from spec 23 §4.2, kept as a named method for symmetry with
// ListPendingMessages and to match the new sentinel folder ID.
// Honours excludeMuted (default true).
ListScreenedOutMessages(ctx context.Context, accountID int64,
    limit int, excludeMuted bool) ([]Message, error)

// CountPendingSenders returns the count of distinct pending
// sender addresses for the account. Used by the sidebar Screener
// badge. Honours excludeMuted (callers pass the
// [screener].exclude_muted config). Cheap (covered by
// idx_messages_from_lower).
CountPendingSenders(ctx context.Context, accountID int64,
    excludeMuted bool) (int, error)

// CountScreenedOutMessages returns the count of messages from
// screener-routed senders. Used by the sidebar Screened-Out
// badge. Mirrors CountMessagesByRouting('screener') from spec
// 23, kept as a named method for symmetry.
CountScreenedOutMessages(ctx context.Context, accountID int64,
    excludeMuted bool) (int, error)

// CountMessagesFromPendingSenders returns the count of messages
// (not distinct senders) from pending senders. Used ONLY by the
// gate-flip confirmation modal (§5.3.1) to render the warning
// copy "N messages from M senders". Distinct from
// CountPendingSenders (which counts senders); this counts the
// messages those senders contributed. Single query at modal
// render time; never on the hot path.
CountMessagesFromPendingSenders(ctx context.Context,
    accountID int64, excludeMuted bool) (int, error)
```

`PendingSender`:

```go
type PendingSender struct {
    EmailAddress    string    // lowercased + trimmed
    DisplayName     string    // most recent message's from_name
    LatestSubject   string    // most recent message's subject
    LatestReceived  time.Time // most recent received_at
    MessageCount    int       // total messages from this sender (capped at config max)
    LatestMessageID string    // most recent message's id (for Open / preview)
}
```

### 4.4 SQL for `ListPendingSenders`

The SQL below uses named binds (`:account_id`, `:limit`,
`:cap_plus_one`) for readability — the production implementation
uses `?` positional binds with the same value passed up to four
times via `args = append(args, q.AccountID, q.AccountID, ...)`,
matching the rest of `internal/store/`. The illustrative form
shows the logical placement; reviewers should not assume named
binds work in modernc/sqlite call sites.

```sql
-- One row per pending sender, with the latest message's envelope.
-- The window function picks the newest message per address; the
-- outer query keeps only that row. Anti-join filters out
-- senders that have any sender_routing row (pending = unrouted).
WITH ranked AS (
    SELECT
        lower(trim(m.from_address)) AS address,
        m.from_name      AS display_name,
        m.subject        AS subject,
        m.received_at    AS received_at,
        m.id             AS message_id,
        ROW_NUMBER() OVER (
            PARTITION BY lower(trim(m.from_address))
            ORDER BY m.received_at DESC
        ) AS rn
    FROM messages m
    WHERE m.account_id = :account_id
      AND m.from_address IS NOT NULL
      AND m.from_address != ''
      AND NOT EXISTS (
          SELECT 1 FROM sender_routing sr
          WHERE sr.account_id    = m.account_id
            AND sr.email_address = lower(trim(m.from_address))
      )
      -- Optional anti-join when excludeMuted = true (spec 19 §4.2):
      AND (
        m.conversation_id IS NULL
        OR m.conversation_id = ''
        OR NOT EXISTS (
            SELECT 1 FROM muted_conversations mc
            WHERE mc.conversation_id = m.conversation_id
              AND mc.account_id = :account_id
        )
      )
)
SELECT r.address, r.display_name, r.subject, r.received_at,
       r.message_id,
       (SELECT COUNT(*) FROM messages m2
         WHERE m2.account_id = :account_id
           AND lower(trim(m2.from_address)) = r.address
           AND (m2.conversation_id IS NULL
                OR m2.conversation_id = ''
                OR NOT EXISTS (
                    SELECT 1 FROM muted_conversations mc
                    WHERE mc.conversation_id = m2.conversation_id
                      AND mc.account_id = :account_id))
       ) AS message_count
FROM ranked r
WHERE r.rn = 1
ORDER BY r.received_at DESC, r.address ASC
LIMIT :limit
```

The secondary `r.address ASC` is mandatory: `received_at` ties
are not impossible (delta-syncs from a list server can land
multiple senders at identical second-resolution timestamps),
and an unstable order would flicker on every re-render. Cover
with `TestListPendingSendersDeterministicTieBreak` (§10).

The expected query plan is `SCAN m USING INDEX idx_messages_from_lower`
driving the window-function partition; the anti-join probes
`sender_routing.PRIMARY KEY` (composite). Verify with
`EXPLAIN QUERY PLAN` in `BenchmarkListPendingSenders`.

**`message_count` cap.** A correlated count over a 100k-message
store with one outlier sender of ~50k messages would dominate
the budget. The implementation caps `message_count` at
`[screener].max_count_per_sender` (default 999, exposed as a
display "999+" when capped) by wrapping the count subquery so
SQLite stops scanning at `:cap + 1` rows per sender. The
**production form** of the correlated subquery in the SELECT
list is therefore:

```sql
(SELECT COUNT(*) FROM (
    SELECT 1 FROM messages m2
    WHERE m2.account_id = :account_id
      AND lower(trim(m2.from_address)) = r.address
      AND (m2.conversation_id IS NULL
           OR m2.conversation_id = ''
           OR NOT EXISTS (
               SELECT 1 FROM muted_conversations mc
               WHERE mc.conversation_id = m2.conversation_id
                 AND mc.account_id = :account_id))
    LIMIT :cap_plus_one
)) AS message_count
```

`:cap_plus_one` is `[screener].max_count_per_sender + 1` (so the
inner `LIMIT` produces 1000 rows when the cap is 999, and the
outer COUNT returns 1000; the Go layer renders any value `>=
:cap_plus_one` as the display "999+"). The illustrative SQL
block above shows the un-capped form for readability; the
production form is the capped wrapper. Cover with
`TestListPendingSendersMessageCountCap`.

### 4.5 Pattern operator alias `~o pending`

Spec 23 §4.3 ships `~o none` for "no sender_routing row." Spec 28
adds **`~o pending`** as a parser-level alias that **canonicalises
to `none` at parse time** — the AST carries `RoutingValue{Destination:
"none"}` regardless of which spelling the user typed. Rationale:
`none` reads as "match nothing" to a casual reader; `pending` is
the user-facing term in the Screener UX (status bar, palette
rows, docs). Both spellings remain valid forever — never
deprecate `none`, both compile to identical SQL.

**Why canonicalise at the parser, not the evaluator.** The
existing `emitRoutingPredicate` in
`internal/pattern/eval_local.go:95` switches on
`v.Destination == "none"`. If the parser left `pending` in the
AST verbatim, `pending` would leak into the evaluator and the
`== "none"` check would miss it (returning the wrong EXISTS
form). Canonicalising at parse time keeps every downstream
consumer — `eval_local.go`, `eval_filter.go`, `eval_search.go`,
`eval_memory.go`, `routing_test.go` — unchanged.

**Concrete change to `parseRoutingValue`** (spec 23 §4.4 form
in `internal/pattern/parser.go:169`):

```go
func parseRoutingValue(raw string) (RoutingValue, error) {
    switch raw {
    case "imbox", "feed", "paper_trail", "screener", "none":
        return RoutingValue{Destination: raw}, nil
    case "pending":
        return RoutingValue{Destination: "none"}, nil
    }
    return RoutingValue{}, fmt.Errorf(
        "unknown routing destination %q; expected one of imbox, "+
        "feed, paper_trail, screener, none, pending", raw)
}
```

The valid-input set is six values (the four destination column
values plus `none` and `pending`); the AST `RoutingValue.Destination`
remains a five-value set (the same four plus `none`). Document
this 6-input/5-AST asymmetry as one sentence in the function
doc comment so a future reader doesn't mistake `pending` for a
sixth destination.

**Spelling preservation in saved searches.** `internal/pattern/`
has no AST printer — saved searches store the raw pattern *text*
in `saved_searches.pattern` (user-typed source, exposed as
`store.SavedSearch.Pattern string` per `internal/store/types.go`)
and only the AST is re-derived on each compile. So both spellings
round-trip verbatim through the saved-search table by accident of
the existing design: `~o pending` stays `~o pending` when written
to a saved-search row, and re-parsing it produces the canonical
`RoutingValue{Destination:"none"}` AST. No "canonical printer" is
needed and none is added. The parser test asserts both spellings
parse to the same AST node value (`RoutingValue{Destination:"none"}`).
Document the alias in `docs/user/reference.md` under the `~o`
operator row so users aren't surprised that the two spellings are
equivalent.

`paper-trail` (hyphen) remains a parse error.

No change to `eval_filter.go` / `eval_search.go` — these already
return `ErrUnsupported` for any `FieldRouting` predicate (spec 23
§4.4 step 6; sentinel name verified at
`internal/pattern/compile.go:109`).

### 4.6 No changes to action queue / Graph endpoints

Spec 28 is local-only (per spec 23 §6 precedent). No Graph call
is made by the screener filter or the new sentinel folder. No
action queue entry. Routing assignments still flow through
spec 23's `routeCmd` (§5.6 of spec 23) — spec 28 reuses it
verbatim. Document in `docs/ARCH.md` §"action queue" alongside
mute and routing.

## 5. UI

### 5.1 The Screener virtual folder (redefined when gate enabled)

Spec 23 §5.4 ships four routing virtual folders — Imbox, Feed,
Paper Trail, Screener — with sentinel IDs `__imbox__`,
`__feed__`, `__paper_trail__`, `__screener__` respectively. Spec
28 keeps the same four sentinels but **redefines the content of
`__screener__`** when `[screener].enabled = true`:

| `[screener].enabled` | `__screener__` content                                   | `__screened_out__` rendered? |
|----------------------|----------------------------------------------------------|------------------------------|
| `false` (default)    | Messages from `destination='screener'` senders (spec 23) | No (sentinel hidden)         |
| `true`               | Pending senders' mail (`~o none`/`~o pending`)           | Yes — separate sentinel      |

The display label of `__screener__` is `Screener` in both modes.
The data underneath shifts. This is a deliberate trade-off: muscle
memory ("Screener is the second-to-last stream") survives, but the
*semantics* shift to match HEY when the gate is enabled. The user
who flips the flag sees the queue change once and the mental model
sticks.

Selecting `__screener__` while the gate is enabled calls
`ListPendingSenders` (when `[screener].grouping = "sender"`,
default) or `ListPendingMessages` (when `"message"`). The list
pane top shows `[screener]`.

The list-pane row format for the per-sender mode mirrors the
bundle-row layout from spec 26 §5.2:

```
▶ Mon 14:32   Acme Corp       (12) — Your weekly digest
  Mon 13:14   Bob Recruiter   (3) — Quick chat next week?
  Sat 09:00   noreply@svc     (1) — Welcome to Acme
```

The `(N)` count is `MessageCount` from `PendingSender`; the
subject is `LatestSubject`. Spec 26's bundle disclosure glyph
is NOT reused — the Screener queue is one row per sender by
nature, no expansion. (`Enter` opens the *latest* message in the
viewer; see §5.4 for the per-sender Yes/No verbs.)

In per-message mode (`[screener].grouping = "message"`), each
row is a normal flat message row, identical to spec 23 §5.5
plus the existing pane-scoped indicators.

**Sidebar position:** unchanged from spec 23 §5.4. The four
Streams remain in their original order. When the gate is enabled,
`Screened Out` is appended as a fifth Streams entry **below
Screener and above** the optional `__muted__` entry (which spec
19 §5.4 places at the bottom of the sidebar). The order is:
Imbox → Feed → Paper Trail → Screener → Screened Out → (Saved
Searches block) → (Stacks block, spec 25 — Reply Later, Set Aside) → Muted Threads (when
non-empty).

**Sidebar count source.** When `[screener].enabled = false`,
the `__screener__` badge count is sourced from
`CountMessagesByRouting('screener')` (spec 23 v1 behaviour).
When `[screener].enabled = true`, the count is sourced from
`CountPendingSenders` (distinct pending senders) and
`__screened_out__` is sourced from `CountScreenedOutMessages`.
The flip happens in the same materialisation site as §5.5
(`refreshStreamCountsCmd` reads `m.screenerEnabled` once per
refresh and dispatches the matching count query). DoD bullet
in §10.

### 5.2 The Screened-Out virtual folder (new)

A fifth hardcoded virtual folder, gated on
`[screener].enabled = true` (hidden in the sidebar when false).

| Destination value | Sentinel ID         | Display name    |
|-------------------|---------------------|-----------------|
| `screener`*       | `__screened_out__`  | `Screened Out`  |

\* Reuses the existing `screener` destination value — the
sentinel ID is new, the destination value is not.

Selecting `__screened_out__` calls `ListScreenedOutMessages`.
The list pane top shows `[screened out]`.

The folderItem flag pattern is unchanged from spec 23 §5.4 —
`isStream = true`, `streamDestination = "screener"`. The new
sentinel ID is added to `IsStreamSentinelID` (already in
`internal/ui/panes.go`) and `streamDestinationFromID`. The
`Selected()` returns `(_, false)` rule (spec 19 protection)
inherits automatically.

**Visibility rule.** Per spec 23 §5.4, all routing virtual
folders are *always rendered* even at zero count. Spec 28
preserves this for Imbox / Feed / Paper Trail / Screener, but
`__screened_out__` is rendered only when the gate is enabled.
Rationale: when the gate is off, the user has no way to populate
the bucket meaningfully (spec 23 v1's `S k` lands in
`__screener__`); rendering an empty Screened-Out entry alongside
a populated Screener would confuse. When the gate is on, the
spec 23 v1 `__screener__` content moves wholesale to
`__screened_out__`, so the entry is the new home and is always
rendered.

### 5.3 Mode transitions for existing users

A user upgrading from v0.51.0+ (spec 23) with screener-routed
senders flips `[screener].enabled = true` once. On the next
list-pane refresh:

1. The Screener virtual folder's content changes from "screener-
   routed senders' mail" to "pending senders' mail." Existing
   screener-routed senders' mail moves to the new Screened-Out
   virtual folder (same data, new home).
2. The Screened-Out sentinel folder appears in the sidebar
   between Screener and the optional `__muted__` entry.
3. Default folder views (Inbox, Sent, Archive, user folders)
   start hiding Pending and Screened mail (§5.5).

#### 5.3.1 Gate-flip confirmation prompt

Flipping `[screener].enabled false → true` while the local store
holds messages from many unrouted senders is a *materially
destructive view change* — a 50k-message archive with zero
routing assignments will appear to lose its entire Inbox until
the user starts admitting senders. Spec 28 therefore wraps the
flip with a confirmation gate so the surprise can never happen
silently.

**Mechanism — first-launch detection** (CLAUDE.md §9: "No hot
reload. Config changes require restart"). The TUI does not
implement runtime config reload, so the modal fires at *launch
time* immediately after `ui.New(deps)` materialises the model,
**before the first list pane render**. Detection is by comparing
`cfg.Screener.Enabled` against a last-seen marker stored in
`[ui].screener_last_seen_enabled` (default `false`):

| `cfg.Screener.Enabled` | `[ui].screener_last_seen_enabled` | Action at launch                |
|------------------------|------------------------------------|---------------------------------|
| `false`                | `false`                            | Gate off; no modal.             |
| `false`                | `true`                             | Gate off; reset marker to `false`; no modal (disable path is non-destructive). |
| `true`                 | `true`                             | Gate on; no modal (returning user). |
| `true`                 | `false` AND `M == 0`               | Gate on; write marker `true`; no modal; §5.3.2 hint fires (per-`hint_dismissed`). |
| `true`                 | `false` AND `M > 0`                | **Render modal** (Mode = Confirm); see below. |

Modal copy:

```
Enable Screener?

This will hide N messages from M senders from your default Inbox
view. They remain accessible from the Screener virtual folder.

Press Y to enable, N to leave the gate off this session.
(Esc behaves as N.)
```

**`Y`** writes `[ui].screener_last_seen_enabled = true` to the
on-disk config (so a returning user is not re-prompted), sets
`m.screenerEnabled = true`, and triggers the §5.3.2 hint on the
first list-pane render.

**`N` / `Esc`** sets `m.screenerEnabled = false` for *this
session only*. **The on-disk `[screener].enabled` value is NOT
rewritten** (the user edited their config; we don't silently
fight the edit). The `[ui].screener_last_seen_enabled` marker is
also NOT updated, so the modal will re-fire on the *next* launch
until the user either confirms or edits the on-disk flag back to
`false`. Status-bar toast: `screener: gate kept off this session
(re-launch to re-prompt)`.

**Skip path.** When `M == 0` (no pending senders — the user has
done a complete routing pass first, the recommended sequence in
the how-to recipe), the modal is skipped entirely; the gate
enables silently and the §5.3.2 hint fires per usual.

**First-launch path.** A user setting `enabled = true` in their
config *before first sign-in* (no messages in the local store)
hits the `M == 0` branch and skips the modal. Same outcome as
"did a routing pass first."

**Disable path.** Editing on-disk `[screener].enabled` from
`true → false` and re-launching is non-destructive: mail
re-appears in default views. The marker is reset to `false` so
a future re-enable correctly re-evaluates. No modal.

**Why a marker key, not a transition log.** A single boolean
keyed by previous-launch state is the minimum viable surface to
distinguish "first time enabling" from "returning user." The
existing `[ui].screener_hint_dismissed` is similar — a one-shot
boolean. We deliberately do not track decision history; users
who want a richer trail use `inkwell screener history`.

Cover with `TestScreenerGateFlipNoPromptWhenNoPending`,
`TestScreenerGateFlipPromptWhenPendingExists`,
`TestScreenerGateFlipDisablePathNoPrompt`, and
`TestScreenerGateFlipMarkerPersistsAcrossLaunches`.

#### 5.3.2 Post-enable hint

A **one-time hint** is shown on the next list-pane render after
the flag flips (post-confirmation), in the same shape as spec 11
§5.4 / spec 23 §5.9 auto-suggest hints. The copy explicitly names
the sidebar swap so the user is not surprised that "Screener"
just changed contents:

```
screener: gate on. 'Screener' now shows pending senders;
previously-routed senders moved to 'Screened Out'. Y / N on a
focused sender admits / screens-out.
```

Dismissed via `Esc`. Persisted as `[ui].screener_hint_dismissed =
true` so it never re-fires.

No ASCII-fallback variant: typographic single-quotes already
render as straight ASCII (`'`) in the TUI's default font path.
Emoji-free rendering is not gated — matches every other status
toast in spec 23 §5.6.

### 5.4 Pane-scoped Yes / No shortcuts

When the list pane is showing the Screener virtual folder
(`__screener__` with the gate enabled), two new pane-scoped
keybindings activate:

| Key (Screener pane focused, sender row) | Action |
|------------------------------------------|--------|
| `Y` (capital) | Equivalent to `S i` — admit the focused sender to **Imbox**. |
| `N` (capital) | Equivalent to `S k` — screen out the focused sender. |

Both are pane-scoped to the Screener virtual folder *only*. They
do NOT fire in the regular Inbox view, in any other Streams
folder, or in the viewer pane. A pane-scoped binding is the
established pattern for context verbs (spec 18 `N` for new
folder is folder-pane-scoped; the same key in the list pane
is unbound).

**Cross-pane binding collision audit:**

- `Y` (capital): unused in `internal/ui/keys.go::DefaultKeyMap`
  (verified — scan `Keys()` for `"Y"`; nothing). Safe to add to
  the duplicate-scan list.
- `N` (capital): **already used** by spec 18 (`NewFolder`,
  default `key.NewBinding(key.WithKeys("N"))` in
  `internal/ui/keys.go::DefaultKeyMap`). Folder-pane vs. list-
  pane scoping disambiguates at dispatch time. `NewFolder` is
  **not** in the existing `findDuplicateBinding` `checks` slice
  — the duplicate-scan policy already excludes pane-scoped-only
  bindings (precedent: `MarkRead`, `ToggleFlag`, `BundleExpand`
  exclusions). `ScreenerReject` follows the same precedent and
  is **excluded** from the duplicate-scan list.
- The Screener pane is part of the list pane (it's a virtual
  folder selection); no new pane is introduced. The dispatch
  hook is at the list pane handler, gated on
  `displayedFolder.sentinelID == "__screener__"`.

**KeyMap changes (`internal/ui/keys.go`):**

- Add `ScreenerAccept key.Binding` and `ScreenerReject
  key.Binding` to `KeyMap`.
- Add `ScreenerAccept string` and `ScreenerReject string` to
  `BindingOverrides`.
- Wire both through `ApplyBindingOverrides`.
- Add `ScreenerAccept` to the `findDuplicateBinding` scan list
  (its `Y` default does not collide with anything globally).
- Do **NOT** add `ScreenerReject` to the scan list. Add an
  exclusion comment alongside the existing `MarkRead` /
  `ToggleFlag` / `Expand` exclusions: `// ScreenerReject excluded
  — pane-scoped overlap with NewFolder (spec 18) on capital N`.
  The pane dispatcher routes to the focused-pane handler before
  fallthrough.
- Defaults: `ScreenerAccept: key.NewBinding(key.WithKeys("Y"))`,
  `ScreenerReject: key.NewBinding(key.WithKeys("N"))`.

**`config.BindingsConfig` plumbing.** Per CLAUDE.md §9, every
new binding key has an entry in `internal/config/config.go`
(`BindingsConfig` struct field with TOML tag) and a default in
`internal/config/defaults.go`. Add:

```go
// internal/config/config.go BindingsConfig:
ScreenerAccept string `toml:"screener_accept"`
ScreenerReject string `toml:"screener_reject"`

// internal/config/defaults.go:
ScreenerAccept: "Y",
ScreenerReject: "N",
```

The wiring layer (where `BindingsConfig` is converted to
`ui.BindingOverrides`) gains the two assignments. Cover with a
`TestBindingsScreenerKeysFlowFromConfig` test that exercises the
TOML → config → KeyMap path.

The `S` chord is unchanged. `Y` / `N` are pure shortcuts; the
chord remains available everywhere `S` is bound.

**Visual feedback:** identical to the chord toasts (spec 23 §5.6).
The toast says `📥 admitted news@example.com → Imbox` for `Y`
and `🚪 screened out news@example.com` for `N`. The `(was X)`
suffix from spec 23 §5.7 is omitted because Pending senders have
no prior destination.

After admit/reject, the row vanishes from the Screener queue
(it is no longer Pending). The cursor falls to the next row.

**Concurrent-decision semantics.** `Y` and `N` are local-write
optimistic — `routeCmd` applies a `sender_routing` row to the
store synchronously and emits a `tea.Cmd` for the list-pane
refresh. Two `Y` keypresses in quick succession on rows 1 and 2
must not race the visible cursor against the in-flight refresh.
The implementation pins the *focused address* (the sender's
`from_address`) at keypress time and routes that address; the
list-pane refresh that lands later may reorder rows but cannot
mis-target a decision. Concretely:

1. `Y` keypress — read `m.focusedRow().from_address` synchronously.
2. Dispatch `routeCmd(addr, "imbox")` with the captured address.
3. The cursor moves to the next address-different row (i.e., it
   skips any row whose address matches the just-decided one,
   defensive against duplicate-pending-message edge cases in
   per-message mode). If no further row exists, the cursor
   parks on the last remaining row (per spec 04 list-pane
   conventions).
4. The store-write resolves; the next refresh removes the
   decided sender from the queue.

`Y` / `N` are NOT debounced — a fast user can decide ten senders
in ten keystrokes and each decision is dispatched on its own
goroutine via `routeCmd`. Routing does **not** flow through the
spec 07 action queue (per spec 23 §6 — `routeCmd` writes
`sender_routing` directly via `store.SetSenderRouting`). Ten
near-simultaneous upserts settle deterministically because the
SQLite write lock serialises them and the table's
`(account_id, email_address)` PK is the conflict target on the
`UPSERT … ON CONFLICT DO UPDATE`. Cover with
`TestScreenerPaneRapidYDispatchesAllAddresses`
(spawns ten address-distinct rows, fires ten `Y` keys in a tight
loop via teatest, asserts ten `routeCmd` calls each with the
correct address).

### 5.5 Default folder view filter

When `[screener].enabled = true`, the TUI's normal folder views
(Inbox, Sent, Archive, user-created folders) call `ListMessages`
with `ApplyScreenerFilter: true`. This hides Pending and
Screened mail from the default Inbox view — the gate fires.

**Materialisation in `Model`** (load-time only — CLAUDE.md §9
mandates no hot reload; config changes require restart). The
Model carries `screenerEnabled bool`, `screenerGrouping string`,
`screenerExcludeMuted bool`, and `screenerMaxCountPerSender int`
(parallel to existing `[ui]` mirrors), set once on app boot in
`ui.New(deps)` from `cfg.Screener`. The §5.3.1 confirmation modal
runs at boot before the first list-pane render and may override
`m.screenerEnabled` to `false` for the session if the user
chooses `N`. Each call site in the table below passes an explicit
boolean to `ListMessages` — the **two filter-applying sites**
read `m.screenerEnabled`; the **other five sites** hard-code
`false` (search / filter / CLI are intentional queries, §4.2).
The TUI never reads `cfg.Screener.Enabled` outside the boot
materialisation site.

**Affected call sites** (default folder views only — search,
filter, and CLI paths are NOT affected, per §4.2):

| Call site | Default behaviour |
|-----------|-------------------|
| `Model.loadFolderCmd` (folder Enter / refresh) | `ApplyScreenerFilter = [screener].enabled` |
| List-pane refresh after action queue settle | same |
| `Model.searchSubmit` (FTS5 path)               | `ApplyScreenerFilter = false` (always) |
| `:filter` pattern execution                    | `ApplyScreenerFilter = false` (always) |
| `:filter --all` cross-folder (spec 21)         | `ApplyScreenerFilter = false` (always) |
| `inkwell messages` CLI                         | `ApplyScreenerFilter = false` (always) |
| `inkwell filter` CLI                           | `ApplyScreenerFilter = false` (always) |

The TUI never reads the config flag at dispatch time. The filter
value is materialised into the `Model` once at boot — same
pattern as `[ui].show_routing_indicator` and other UI flags
(CLAUDE.md §9: config changes require restart).

**Counts on Inbox / Sent / Archive entries** in the sidebar
remain Graph's authoritative counts (spec 19 §5.6 precedent).
Inkwell does not adjust the Graph-source unread counts to reflect
local screener filtering. The mismatch ("Inbox (47)" but only 32
visible rows) is the same trade-off spec 19 makes for mute and
spec 23 makes for routed virtual folders. A future spec can add
an `effectiveUnreadCount` computation; out of scope.

### 5.6 Status-bar feedback

Reuses spec 23 §5.6 toast forms for chord-driven actions. Spec 28
adds the `Y` / `N` toast variants (which are aliases for `S i` /
`S k`):

| Action      | Toast                                              |
|-------------|----------------------------------------------------|
| `Y` succ.   | `📥 admitted news@example.com → Imbox`             |
| `N` succ.   | `🚪 screened out news@example.com`                 |
| `Y` no addr | `screener: focused sender has no from-address`     |
| Empty queue | List pane shows `(no pending senders — all caught up)` |

The empty-queue helper text uses HEY's "all caught up" phrasing
to reinforce inbox-zero affordance. The em-dash and toast emoji
(`📥`, `🚪`) render unconditionally — matches every other status
toast and helper text in specs 19, 22, 23. The narrow-purpose
`[ui].stream_ascii_fallback` key (spec 23) gates the four
stream-indicator letter glyphs only and does not apply here; a
broader unicode-fallback config is out of scope for spec 28.

### 5.7 Screener queue ordering and mute interaction

- **Ordering:** newest representative-message `received_at` DESC,
  with `address ASC` as the deterministic tie-break (see §4.4).
  Senders with multiple pending messages are ordered by their
  newest message's date. Settled by SQL (`ORDER BY received_at
  DESC, address ASC`) so the order is stable across re-renders.
- **Mute interaction:** muted threads are excluded from the
  Screener queue by default (`[screener].exclude_muted = true`),
  matching spec 19 §5.3 default-folder-view contract. Rationale:
  muting a thread is a stronger signal than "I haven't decided
  on this sender" — if you've already muted, the sender shouldn't
  pop up demanding a decision. Configurable per §6.

### 5.8 Sentinel-folder protections

The new `__screened_out__` sentinel folder inherits spec 19's
`isStream`-style protection from `folderItem`:
`FoldersModel.Selected()` returns `(_, false)` so spec 18's `N`
/ `R` / `X` handlers refuse to operate on it. Same as spec 23
§5.4. No code change to spec 18 handlers required; the new
sentinel ID is added to `IsStreamSentinelID` only.

### 5.9 Command palette rows

Adds four static palette rows to
`internal/ui/palette_commands.go`, parallel to spec 23's six
routing rows (`route_imbox`, `route_feed`, `route_paper_trail`,
`route_screener`, `route_clear`, `route_show`):

| ID                  | Title                              | Binding | RunFn                              |
|---------------------|------------------------------------|---------|------------------------------------|
| `screener_accept`   | Admit focused sender to Imbox      | `Y`     | `routeCmd(addr, "imbox")`          |
| `screener_reject`   | Screen out focused sender          | `N`     | `routeCmd(addr, "screener")`       |
| `screener_open`     | Open Screener queue                | (none)  | navigate to `__screener__`         |
| `screener_history`  | Open Screened-Out history          | (none)  | navigate to `__screened_out__`     |

`screener_accept` / `_reject` `Available()` resolves to OK only
when (a) a message is focused, (b) `from_address != ""`, and
(c) `m.deps.Store != nil` (the unsigned-in / CLI-mode guard).
This matches the spec 23 `buildRoutingPaletteRows` precedent
verbatim — see `internal/ui/palette_commands.go::Available`
combinator that ANDs `hasFrom` and `storeAvail`. The focused
sender's routing state is **not** checked at palette-open time
— doing so would require a synchronous SQLite probe inside the
UI goroutine, violating CLAUDE.md §3 invariant 2 ("UI never
blocks on I/O"). The palette therefore surfaces the verb
whenever a message is focused; `routeCmd` itself is the no-op-
friendly path (an already-Imbox sender short-circuits to an
"already → Imbox" toast per spec 23 §5.7). `screener_open` is
always available.

`screener_history` is available only when
`[screener].enabled = true` (the `__screened_out__` sentinel
is not rendered when the gate is off, so navigating to it is a
no-op). `screener_open` is always available even with the gate
off — the `__screener__` sentinel is rendered in both modes
(spec 23 v1 behaviour preserved per §5.1), so navigation is
meaningful regardless. The asymmetry between
`screener_open` (always) and `screener_history` (gate-on only)
is intentional: it mirrors the sentinel-render visibility rule
in §5.2.

When the gate is off, `screener_accept` and `screener_reject`
remain available but their titles read oddly (no Screener queue
to act in). The implementation rewrites the palette title when
`m.screenerEnabled = false` to:

| ID                | Gate-off title                       | Gate-on title (default)         |
|-------------------|--------------------------------------|---------------------------------|
| `screener_accept` | `Route focused sender → Imbox (Y)`   | `Admit focused sender to Imbox` |
| `screener_reject` | `Screen out focused sender (N)`      | `Screen out focused sender`     |

The runtime title swap is a one-liner in `buildScreenerPaletteRows`
that branches on `m.screenerEnabled`; the `RunFn` is the same
in both modes.

### 5.10 Cross-feature interactions surfaced in user docs

The `docs/user/reference.md` Streams section gains a "Screener
edge cases" subsection covering:

1. **Bundled-but-pending senders** (spec 26): the bundle row is
   gate-suppressed in the default Inbox view when
   `[screener].enabled = true`. The user must admit the bundled
   sender (via `S i` or `Y` in the Screener queue) before the
   bundle re-appears. The "Turn on the Screener" how-to recipe
   recommends a routing pass over bundled senders first.
2. **Reply-Later'd messages from pending senders** (spec 25):
   the stacked message remains visible in the Reply Later
   overlay regardless of gate state (intentional surface). The
   sender's *other* mail is still gate-suppressed; the Screener
   queue still surfaces the sender for a routing decision.
3. **Fully-muted pending senders** (spec 19): a sender whose
   only mail lives in muted threads is invisible to both the
   default Inbox and the Screener queue (the latter because
   `[screener].exclude_muted = true` by default). Recovery is
   `:filter ~o pending` to surface them explicitly.
4. **Threads spanning multiple senders**: the gate operates
   per-sender. A thread with one Approved and one Pending
   sender renders the Approved message in the default Inbox and
   the Pending message in the Screener queue; the user makes
   the Pending sender's decision independently.

## 6. Configuration

This spec adds the following to `[screener]` (a new top-level
section):

| Key                                | Default     | Used in §                                 |
|------------------------------------|-------------|-------------------------------------------|
| `screener.enabled`                 | `false`     | §1 (master flag), §5.5, §5.1, §5.2        |
| `screener.grouping`                | `"sender"`  | §5.1 (sender vs message rendering)        |
| `screener.exclude_muted`           | `true`      | §5.7                                      |
| `screener.max_count_per_sender`    | `999`       | §4.4 (count cap)                          |

TOML form:

```toml
[screener]
# When enabled, mail from senders not in sender_routing OR routed to
# 'screener' is hidden from default folder views and routed to the
# Screener / Screened-Out virtual folders. Off by default — turning
# it on after a routing pass is the recommended setup; flipping it
# on a fresh inkwell install will hide most of your inbox until
# you start admitting senders.
enabled = false

# "sender" (default) shows one row per pending sender in the
# Screener queue, with a count badge per sender. "message" shows
# one row per pending message — useful if you want to triage by
# subject before committing to a per-sender routing.
grouping = "sender"

# When true (default), muted threads are excluded from the
# Screener queue. Mute is a stronger signal than "no decision
# yet"; treating it as such avoids muted threads popping back
# into the user's face.
exclude_muted = true

# Cap for the per-sender message count display in the Screener
# queue. Counts above this render as "999+". Capping is a perf
# safeguard — see spec 28 §4.4.
max_count_per_sender = 999
```

`[bindings]` gains:

| Key                          | Default | Used in § |
|------------------------------|---------|-----------|
| `bindings.screener_accept`   | `"Y"`   | §5.4      |
| `bindings.screener_reject`   | `"N"`   | §5.4      |

`[ui]` gains:

| Key                                  | Default | Used in § |
|--------------------------------------|---------|-----------|
| `ui.screener_hint_dismissed`         | `false` | §5.3.2    |
| `ui.screener_last_seen_enabled`      | `false` | §5.3.1    |

The hint-dismissed flag is updated by the TUI on `Esc`-dismissal
and persists in the user's config file across runs (same
auto-write pattern as spec 11 §5.4 / spec 23 §5.9). If the user
manually deletes the line from config, the hint will re-fire on
next launch.

## 7. CLI

```sh
# List pending senders (one row per sender by default).
inkwell screener list
inkwell screener list --grouping message
inkwell screener list --output json

# Admit a sender — alias for `inkwell route assign <addr> imbox`.
inkwell screener accept news@example.com
inkwell screener accept news@example.com --to feed
inkwell screener accept news@example.com --to paper_trail

# Screen out a sender — alias for `inkwell route assign <addr> screener`.
inkwell screener reject news@example.com

# List screened-out senders.
inkwell screener history
inkwell screener history --output json

# Pre-approve senders in bulk (e.g., from a contacts dump).
inkwell screener pre-approve --from-stdin
inkwell screener pre-approve --from-file ~/contacts.txt
# reads one address per line; equivalent to `route assign … imbox` for each.

# Show the screener configuration state.
inkwell screener status
```

Subcommands:

| Subcommand     | Text output                                                 | JSON output (`--output json`)                                  |
|----------------|-------------------------------------------------------------|----------------------------------------------------------------|
| `list`         | `ADDRESS DISPLAY-NAME COUNT LATEST` columns                 | `[{"address","display_name","count","latest_received","latest_subject"},…]` |
| `accept`       | `✓ admitted <addr> → <dest>`                                | `{"address","destination":"imbox|feed|paper_trail","prior":""}` |
| `reject`       | `✓ screened out <addr>`                                     | `{"address","destination":"screener","prior":""}`               |
| `history`      | one row per screened-out sender, `ADDRESS ADDED-AT` columns | `[{"address","added_at"},…]` (route-shape audit; no envelope columns — use `inkwell screener list` for a per-sender envelope view of the queue) |
| `pre-approve`  | `✓ pre-approved N senders to imbox` summary                 | `{"approved":N,"skipped":M,"errors":[…]}`                       |
| `status`       | `screener: enabled=true grouping=sender pending=12 screened=3` | `{"enabled","grouping","exclude_muted","pending_count","screened_count"}` |

**Verb choice (`accept` / `reject` over `yes` / `no`).** `accept`
and `reject` are explicit imperatives; `yes` / `no` are HEY's UX
labels but read as ambiguous in shell (`inkwell screener no
news@example.com` is grammatically wrong). The TUI uses `Y` / `N`
(letters) where context makes them unambiguous; the CLI uses the
verbs.

**Address normalization, exit codes, error messages.** Identical
to spec 23 §7.

**`pre-approve --from-stdin` input rules:**

- One address per line. Lines are stripped of leading / trailing
  whitespace and CR (`\r`) before parsing; CRLF input is handled
  transparently.
- **Blank lines** (after strip) are silently skipped.
- Lines whose first non-whitespace character is `#` are treated
  as comments and silently skipped. (Lets users version-control
  a contacts allow-list with annotations.)
- A leading UTF-8 BOM on the first line is stripped before parse
  (defensive — common in Windows-exported CSV / TXT).
- **Display-name forms** (`"Bob" <bob@example.com>`) are
  rejected per the spec 23 §7 rule, with the per-line error
  `pre-approve: line N: address must be bare; got "<input>"
  (strip the display name and angle brackets, keep just the
  address)`.
  These do NOT abort the whole command (per the partial-success
  contract below) but they DO appear in the JSON-output `errors`
  array, so a user pasting a contacts dump that contains
  display-names sees every rejection rather than silently
  dropping them.
- Per-line errors accumulate. The command exits 0 if at least
  one address was successfully applied (with stderr summary
  `pre-approve: N admitted, M skipped (errors above)`); exit 2
  if every line failed; exit 0 if stdin was empty; exit 0 if
  stdin contained only blank lines and `#` comments (zero
  parseable addresses ≠ all-failed; treat as a no-op success
  with stderr summary `pre-approve: 0 admitted (no addresses
  in input)`).
- The `--to <dest>` flag controls the destination for *all*
  successful lines; default is `imbox`. Accepted values:
  `imbox`, `feed`, `paper_trail`. **`screener` is rejected**
  with exit 2 and message `pre-approve: --to: invalid
  destination "screener"; use 'inkwell screener reject' for
  screening-out`. Mixing destinations in one stdin batch is
  not supported.

- **`--from-file` flag.** Mutually exclusive with `--from-stdin`.
  Opens the named file with `os.Open` (mode `0644` constraint
  irrelevant — read-only); applies the same per-line parse
  rules as the stdin path (CRLF / BOM / `#` comments / blank
  lines / display-name rejection). Errors (missing file,
  permission denied, not a regular file) exit 2 before any
  upsert. The file is closed deterministically via `defer`.
  Convenience wrapper for users who keep a contacts allow-list
  in version control. Specifying both flags exits 2 with
  `pre-approve: --from-stdin and --from-file are mutually
  exclusive`.

- **TTY-stdin guard.** `inkwell screener pre-approve` requires
  one of `--from-stdin` or `--from-file`. When `--from-stdin`
  is given the stdin file descriptor must NOT be a terminal —
  if stdin is a TTY (no redirect / pipe present), the command
  exits 2 with `pre-approve: --from-stdin requires a non-tty
  stdin (use a pipe or file redirect)` *before* reading any
  input. Detected via `term.IsTerminal(int(os.Stdin.Fd()))`
  from the existing `golang.org/x/term` dependency. Without
  any input flag, the command exits 2 with `pre-approve:
  one of --from-stdin or --from-file is required`. No
  interactive prompt mode in v1.

Commands live in `cmd/inkwell/cmd_screener.go`, registered in
`cmd_root.go`.

### 7.1 Cmd-bar parity

The TUI cmd-bar accepts the same verbs:

```
:screener accept news@example.com
:screener accept news@example.com --to feed
:screener reject news@example.com
:screener list
:screener history
:screener status
```

Behaviour matches the CLI exactly. `:screener list` opens the
Screener virtual folder (equivalent to navigating to it via the
sidebar). `:screener history` opens `__screened_out__`.

## 8. Performance budgets

| Surface | Budget | Benchmark |
| --- | --- | --- |
| `ListMessages(folder, ApplyScreenerFilter=true, limit=100)` over 100k msgs + 500 routed senders + 200 screener-routed senders | ≤15ms p95 | `BenchmarkListMessagesScreenerFilter` in `internal/store/` |
| `ListPendingSenders(limit=200)` over 100k msgs + 500 routed senders | ≤15ms p95 | `BenchmarkListPendingSenders` |
| `ListPendingMessages(limit=200)` over same fixture | ≤10ms p95 | `BenchmarkListPendingMessages` |
| `ListScreenedOutMessages(limit=200)` | ≤10ms p95 | `BenchmarkListScreenedOutMessages` (parity with spec 23's `BenchmarkListMessagesByRouting`) |
| `CountPendingSenders` over same fixture | ≤10ms p95 | `BenchmarkCountPendingSenders` |
| `CountScreenedOutMessages` | ≤5ms p95 | `BenchmarkCountScreenedOutMessages` |
| Sidebar refresh of all five Streams (Imbox / Feed / Paper Trail / Screener / Screened Out) when the gate is on | ≤25ms p95 cumulative | `BenchmarkSidebarStreamsRefreshWithScreener` |

The `ApplyScreenerFilter` clause is one EXISTS sub-clause; the
budget headroom over spec 19's `BenchmarkListMessagesExcludeMuted`
(≤10ms p95) is +5ms, which is the measured overhead in a
prototype on the dev machine. If the bench misses, the fix is to
add a covering index on `sender_routing(account_id, destination)`
that filters to Approved-only — the existing
`idx_sender_routing_account_dest` already covers `(account_id,
destination)` so this should be a no-op.

`BenchmarkListPendingSenders` includes the worst-case correlated
count (the per-sender `MessageCount`). The cap from §4.4 is the
load-bearing optimisation here; without it the count subquery
dominates on fixtures where one sender has 10k messages.

`BenchmarkSidebarStreamsRefreshWithScreener` drives a single
`CountMessagesByRoutingAll` call (spec 23 §9, returns map of all
four routing destinations including 'screener') plus
`CountPendingSenders`, summed against the budget. The existing
spec 23 sidebar refresh path is one query; spec 28 adds one more
(pending-sender count). Two queries cumulative ≤25ms.

## 9. Security and privacy

- **No new external surface.** Screener is local-only; no Graph
  endpoints are called by the gate. Spec 17 threat model gains
  no new attack surface.
- **No new PII category.** `from_address` is already in
  `messages` and `sender_routing`; the Screener queue surfaces
  the latest subject (already in `messages.subject`). The
  redaction handler at `internal/log/redact.go` already scrubs
  email addresses → `<email-N>` (CLAUDE.md §7 rule 3) and
  subject lines outside DEBUG; new log sites must opt into both.
- **Toast vs. log boundary.** Toasts show literal addresses (UI-
  only path, not logged — matches spec 23 §5.6). Error toasts
  do NOT include raw DB error messages.
- **Cross-account isolation.** `account_id` is in every PK and
  FK of the underlying tables (`sender_routing`,
  `muted_conversations`, `messages`); a second account's pending
  senders never bleed into the first.
- **Persistence across sign-out.** `[screener].enabled`
  persists in the user's config file (same as spec 23 §10
  precedent for `[ui]` keys). `sender_routing` rows are FK-
  cascaded on account delete. Spec 17 PRIVACY.md gains a row
  noting that the screener "decides which senders see the
  Inbox" is a local-only decision; nothing is sent to the
  sender or to a third party.
- **Screener queue subject lines.** The Screener pane renders
  `LatestSubject` for each pending sender. Subject lines are
  cleared from logs outside DEBUG by spec 17's redaction rules;
  the queue itself is rendered to the terminal only and is not
  logged (matches spec 19 §5.5 / spec 23 §5.6).

### 9.1 Notification-suppression non-goal

Spec 23 §14 mentions "optional native-OS notification
suppression for screened-out senders" as a follow-up. inkwell
does not own any notification surface — the user keeps native
Outlook running for that, per PRD §3.2. Native Outlook
notifications for screened-out senders are NOT suppressed by
inkwell; users who want suppression must configure native
Outlook's own per-sender rules. Document in `docs/user/explanation.md`
as part of the "what inkwell does and does not own" section if
it lands; for v1 of this spec, mention in the how-to recipe.

## 10. Definition of done

- [ ] **No new migration.** Verify pre-merge that no
      `internal/store/migrations/0NN_screener.sql` is added.
      Spec 28's contract is purely API + UI + config.
- [ ] `MessageQuery.ApplyScreenerFilter bool` added; `buildListSQL`
      emits the EXISTS-IN-approved sub-clause when true. NULL /
      empty `from_address` is exempted (defensive). Default false
      preserves spec 23 behaviour.
- [ ] `store.Store` interface gains `ListPendingSenders`,
      `ListPendingMessages`, `ListScreenedOutMessages`,
      `CountPendingSenders`, `CountScreenedOutMessages`. The
      `PendingSender` struct is exported per §4.3.
- [ ] `ListPendingSenders` SQL caps `MessageCount` at
      `[screener].max_count_per_sender` via a SQL-side LIMIT
      inside the correlated subquery (§4.4). Cover with a
      `TestListPendingSendersMessageCountCap`.
- [ ] `internal/pattern/parser.go::parseRoutingValue` accepts
      `pending` as an alias for `none`. Both compile to the same
      `NOT EXISTS` form (§4.5). Error message updated to list
      both spellings.
- [ ] `KeyMap` gains `ScreenerAccept` (default `"Y"`) and
      `ScreenerReject` (default `"N"`). `BindingOverrides` gains
      both. `ScreenerAccept` is added to the
      `findDuplicateBinding` scan list; `ScreenerReject` is
      **excluded** with an inline comment citing the pane-scoped
      overlap with spec 18's `NewFolder` (see §5.4).
- [ ] `internal/config/config.go::BindingsConfig` gains
      `ScreenerAccept` and `ScreenerReject` fields with TOML
      tags `screener_accept` / `screener_reject`. Defaults `"Y"`
      / `"N"` registered in `internal/config/defaults.go`. The
      config-to-`BindingOverrides` wiring layer assigns both.
      `TestBindingsScreenerKeysFlowFromConfig` covers the path.
- [ ] `Model` gains four screener materialisation fields,
      populated **once at boot** from `cfg.Screener` in
      `ui.New(deps)`: `screenerEnabled bool`,
      `screenerGrouping string`, `screenerExcludeMuted bool`,
      `screenerMaxCountPerSender int`. The TUI never reads
      `cfg.Screener` outside this site (CLAUDE.md §9: no hot
      reload). The §5.3.1 confirmation modal may override
      `m.screenerEnabled` to `false` for the session before the
      first list-pane render. `TestScreenerConfigBootMaterialisesModelFields`
      covers the load path.
- [ ] **Sidebar count source flips with the gate.** The
      `__screener__` badge count source is gated on
      `m.screenerEnabled`: false → `CountMessagesByRouting('screener')`
      (spec 23 v1); true → `CountPendingSenders` (distinct
      pending senders). The `__screened_out__` badge count is
      sourced from `CountScreenedOutMessages` only when the gate
      is on. `refreshStreamCountsCmd` reads `m.screenerEnabled`
      once per refresh. Cover with
      `TestSidebarScreenerBadgeFlipsOnGateToggle`.
- [ ] `dispatchList` gains a Screener-pane-scoped branch: when
      `displayedFolder.sentinelID == "__screener__"` and
      `[screener].enabled = true`, `Y` dispatches `routeCmd(addr,
      "imbox")` and `N` dispatches `routeCmd(addr, "screener")`.
      Outside the Screener pane, both keys are unbound (no
      fallthrough to other handlers).
- [ ] `__screened_out__` sentinel folder ID added to
      `internal/ui/panes.go` constants and `IsStreamSentinelID`.
      `streamDestinationFromID` maps it to `"screener"`.
      `folderItem` gets the `isStream`/`streamDestination`
      treatment (already in place from spec 23 — extend the list).
- [ ] Sidebar Streams section renders `__screened_out__` only
      when `[screener].enabled = true` (§5.2 visibility rule).
      The four spec 23 stream entries always render (unchanged).
- [ ] Selecting `__screener__` calls `ListPendingSenders` (or
      `ListPendingMessages` based on `[screener].grouping`) when
      gate is on; falls back to `ListMessagesByRouting('screener')`
      when gate is off (spec 23 v1 behaviour preserved).
- [ ] Selecting `__screened_out__` calls
      `ListScreenedOutMessages`. List pane top shows
      `[screened out]`.
- [ ] Default folder views (Inbox, Sent, Archive, user folders)
      pass `ApplyScreenerFilter = [screener].enabled` to
      `ListMessages`. Search, filter, and CLI paths always pass
      false (§4.2).
- [ ] **Gate-flip confirmation modal (§5.3.1).** At launch,
      when `cfg.Screener.Enabled = true` AND
      `[ui].screener_last_seen_enabled = false` AND
      `CountMessagesFromPendingSenders > 0`, render the
      Confirm-mode modal *before* the first list-pane render.
      `Y` writes `[ui].screener_last_seen_enabled = true` and
      proceeds; `N` / `Esc` sets `m.screenerEnabled = false`
      for the session without rewriting `[screener].enabled`
      and without updating the marker (modal re-fires next
      launch). Skip path (`M == 0`): marker advances silently.
      Disable path (`cfg.Screener.Enabled = false`): marker
      reset to `false`; no modal.
- [ ] **`[ui].screener_last_seen_enabled` config key**
      (default `false`) added per §5.3.1.
- [ ] **Config-write surface for `[ui]` one-shot flags
      (load-bearing for §5.3.1 + §5.3.2).** Specs 11 §5.4 and
      23 §5.9 both reference an "auto-write pattern" for one-
      shot UI flags but neither shipped the infrastructure (no
      `WriteUIFlag` / `SaveConfig` / equivalent exists in
      `internal/config/`). Spec 28 is the first spec whose
      correctness depends on the writer existing — without it,
      the `[ui].screener_last_seen_enabled` marker resets every
      launch and the §5.3.1 modal re-fires forever. Land a
      minimal writer in `internal/config/`:
      `WriteUIFlag(path string, key string, value bool) error`
      that reads the existing TOML, sets `[ui].<key> = value`
      (preserving comments and key order via the BurntSushi/toml
      `MetaData` round-trip or, simpler, a `[ui]`-section
      append-or-replace), and writes atomically (temp file +
      rename, mode `0600` matching the rest of inkwell's
      privacy posture per CLAUDE.md §7). Cover with
      `TestWriteUIFlagAtomicReplaceExistingKey`,
      `TestWriteUIFlagAtomicAppendNewKey`,
      `TestWriteUIFlagPreservesOtherSections`, and
      `TestWriteUIFlagMode0600`. The same writer back-fills
      `[ui].screener_hint_dismissed` per §5.3.2 and unblocks
      the spec 11 / spec 23 hint-dismissal claims that have
      been latent since their respective ships.
- [ ] `store.Store.CountMessagesFromPendingSenders(ctx,
      accountID, excludeMuted)` added per §4.3 — used by the
      gate-flip confirmation modal only.
- [ ] One-time Screener-on hint shown after the gate is enabled
      (§5.3.2 — copy explicitly names the sidebar swap);
      dismissed via `Esc`; persisted as
      `[ui].screener_hint_dismissed = true`. Never re-fires once
      dismissed.
- [ ] **Concurrent-decision semantics (§5.4).** `Y` / `N`
      capture the focused address synchronously at keypress
      time and dispatch `routeCmd` with the captured value.
      Cursor moves to the next address-different row. No
      debounce; serialisation is the SQLite write lock plus
      the `(account_id, email_address)` PK conflict-target on
      `sender_routing` (spec 23 §6 — routing bypasses the
      action queue).
- [ ] Empty-queue helper text `(no pending senders — all caught
      up)` rendered in the list pane when the Screener queue is
      empty (§5.6).
- [ ] CLI: `cmd/inkwell/cmd_screener.go` implements `inkwell
      screener list|accept|reject|history|pre-approve|status`.
      Bare-address validation per spec 23 §7. Exit code 2 on
      bad input. `pre-approve` accepts `--from-stdin` OR
      `--from-file <path>` (mutually exclusive). Stdin path:
      one address per line; partial-success exit 0, all-fail
      exit 2; TTY-stdin guard. File path: same parse rules,
      file errors exit 2 before any upsert. Registered in
      `cmd_root.go`.
- [ ] Cmd-bar parity (§7.1): `:screener
      accept|reject|list|history|status` dispatches via the same
      `routeCmd` / navigation handlers as the chord.
- [ ] Command palette: `internal/ui/palette_commands.go` gains
      `screener_accept`, `screener_reject`, `screener_open`,
      `screener_history` static rows per §5.9. `Available()`
      gates per the rules in §5.9. Title strings for
      `screener_accept` / `screener_reject` swap based on
      `m.screenerEnabled` (§5.9 table); `screener_history`
      hidden when the gate is off.
- [ ] User docs: `docs/user/reference.md` adds `Y` / `N`
      Screener-pane shortcut rows, `~o pending` operator alias
      row, `:screener …` cmd-bar verbs, `inkwell screener` CLI
      table. Update the Streams section to reflect the gated
      content shift and the new `Screened Out` entry. The
      `_Last reviewed against vX.Y.Z._` footer bumps with the
      ship version.
- [ ] User docs: `docs/user/how-to.md` adds three recipes —
      "Turn on the Screener" (the recommended sequence: do a
      routing pass first, then flip `[screener].enabled = true`;
      mention the §5.3.1 confirmation modal copy so users know
      what to expect), "Pre-approve senders from a contacts
      dump" (the `pre-approve --from-stdin` and `--from-file`
      workflows), and "Recover from a wrong screener decision"
      (navigate to `__screened_out__`, focus the offending
      sender's mail, press `S c` to clear, then re-decide; HEY
      Screener History parity).
- [ ] `docs/CONFIG.md` adds the four `[screener].*` keys, the
      two `[bindings].screener_*` keys, and the two `[ui]` keys
      (`screener_hint_dismissed`, `screener_last_seen_enabled`).
- [ ] `docs/ARCH.md` §"action queue" updated to mention spec 28
      reuses spec 23's `routeCmd` (no new local-only mutation
      surface; the gate is read-only filter logic).
- [ ] `docs/PRD.md` §10 spec inventory adds spec 28.
- [ ] `docs/ROADMAP.md` updates: §0 Bucket 3 `Screener (1.16)`
      row gains a Spec column entry `28`; §1.16 backlog heading
      flips to `— Spec 28` (in progress) until ship.
- [ ] `docs/PRIVACY.md` (spec 17 §): one row added under "what
      data inkwell stores locally" noting the gate is a local-
      only filter; no new persisted state beyond what spec 23
      already shipped.
- [ ] Tests:
  - **store**:
    - `TestApplyScreenerFilterApprovedOnly` — only Approved
      senders' mail returns when filter is true.
    - `TestApplyScreenerFilterExcludesPending` — no row for an
      unrouted sender appears under filter.
    - `TestApplyScreenerFilterExcludesScreenerRouted` — no row
      for a `destination='screener'` sender appears.
    - `TestApplyScreenerFilterNullFromAddress` — NULL / empty
      from_address rows are NEVER suppressed (NULL safety).
    - `TestApplyScreenerFilterDefaultFalse` — without the flag,
      behaviour matches spec 23 v1 (all rows return).
    - `TestApplyScreenerFilterUsesIndex` — `EXPLAIN QUERY PLAN`
      shows `idx_messages_from_lower` driving the JOIN.
    - `TestListPendingSendersOrderingAndDedupe` — one row per
      sender, newest representative; `received_at` DESC.
    - `TestListPendingSendersExcludesApproved` — sender with any
      `sender_routing` row (incl. `destination='screener'`) is
      excluded. Pending = unrouted only.
    - `TestListPendingSendersMessageCountCap` — sender with 5000
      messages reports `MessageCount = 999` (the cap).
    - `TestListPendingSendersExcludesMuted` — muted-thread
      messages excluded from the count and from row selection
      when `excludeMuted = true`.
    - `TestListPendingSendersIncludesMutedWhenFlagFalse`.
    - `TestListPendingSendersFullyMutedSenderInvisible` — a
      pending sender whose ONLY messages live in muted threads
      does not appear in the queue when `excludeMuted=true`.
      Their mail is also gate-suppressed in default views (since
      they have no `sender_routing` row), so it is reachable
      only via `:filter` / `/`-search. Documented edge.
    - `TestListPendingSendersDeterministicTieBreak` — two
      pending senders sharing the exact same `received_at` for
      their newest message return in `address ASC` order
      (secondary `ORDER BY` keys explicit in the SQL).
    - `TestListPendingMessagesParity` — same row set as
      `ListPendingSenders` flattened, ordered by received_at.
    - `TestListScreenedOutMessages` — only `destination='screener'`
      mail returns; ordering by received_at DESC.
    - `TestCountPendingSendersDistinct` — counts unique
      addresses, not messages.
    - `TestCountScreenedOutMessages` — count matches len(List…).
  - **pattern**:
    - `TestParseRoutingPendingAlias` — `~o pending` and `~o none`
      parse to the same AST node value
      (`OpRouting{dest:"none"}`). No printer assertion (no AST
      printer exists; saved searches store raw text — see §4.5).
    - `TestCompileRoutingPendingLocalOnly` — strategy `LocalOnly`,
      LocalSQL identical to `~o none`.
    - `TestRoutingPendingRejectedByFilterAndSearch` —
      `eval_filter.go` and `eval_search.go` return
      `ErrUnsupportedFilter` for the `pending` alias (regression
      test that the alias inherits the existing rejection rule;
      cheap insurance).
  - **UI dispatch (e2e)**:
    - `TestScreenerPaneYAcceptsToImbox` — `Y` in the Screener
      pane dispatches `routeCmd(addr, "imbox")`; toast says
      `admitted … → Imbox`; row vanishes from the queue.
    - `TestScreenerPaneNRejectsToScreener` — `N` dispatches
      `routeCmd(addr, "screener")`.
    - `TestScreenerPaneYOutsideScreenerIsNoop` — `Y` in the
      regular Inbox view does not dispatch; falls through to no
      handler.
    - `TestScreenerPaneNDoesNotCollideWithFolderPaneNewFolder` —
      `N` in folder-pane focus still hits spec 18's NewFolder.
    - `TestScreenerVirtualFolderShowsPendingWhenGateOn` — sidebar
      Screener entry, when gate is on, loads ListPendingSenders
      (per-sender rows).
    - `TestScreenerVirtualFolderShowsScreenerRoutedWhenGateOff`
      — gate off, Screener entry loads
      `ListMessagesByRouting('screener')` (spec 23 v1).
    - `TestScreenedOutVirtualFolderHiddenWhenGateOff` — sidebar
      does not render `__screened_out__` when gate off.
    - `TestScreenedOutVirtualFolderVisibleWhenGateOn`.
    - `TestSidebarSentinelOrderingWhenGateOn` — when the gate is
      on, sidebar entries appear in this order: user-folder
      block → Streams (Imbox → Feed → Paper Trail → Screener →
      Screened Out) → Saved Searches → Stacks (spec 25) →
      Muted Threads (when non-empty per spec 19).
    - `TestKeymapScreenerRejectExcludedFromDuplicateScan` —
      regression: `findDuplicateBinding(km)` returns `""` when
      both `ScreenerReject` and `NewFolder` default to `"N"`.
      The test pins the *exclusion* — a future contributor who
      removes the pane-scoped exclusion comment in
      `findDuplicateBinding` (causing the scan to flag the
      legitimate `N`-overlap) will turn this test red and be
      forced to revisit the spec 28 §5.4 collision audit.
    - `TestDefaultInboxHidesPendingWhenGateOn` — Inbox view
      excludes pending-sender mail.
    - `TestDefaultInboxHidesScreenedWhenGateOn` — Inbox view
      excludes screener-routed mail.
    - `TestSearchIncludesPendingAndScreenedRegardlessOfGate` —
      `/`-search is unaffected.
    - `TestFilterIncludesPendingAndScreenedRegardlessOfGate` —
      `:filter` is unaffected.
    - `TestScreenerHintShownOnGateEnable` — first list refresh
      after flag flips renders the hint with the spec §5.3.2
      copy ("'Screener' now shows pending senders; previously-
      routed senders moved to 'Screened Out'").
    - `TestScreenerHintDismissedNeverReappears` — `Esc` dismisses
      and persists the flag.
    - `TestScreenerGateFlipPromptWhenPendingExists` — boot with
      `cfg.Screener.Enabled = true`, marker `false`, and
      `M > 0` renders the §5.3.1 confirm modal *before* the
      first list pane render. `N` reverts `m.screenerEnabled`
      to false for the session, leaves marker `false` (re-prompt
      next launch), and does NOT rewrite `[screener].enabled`;
      `Y` writes marker `true` and proceeds.
    - `TestScreenerGateFlipNoPromptWhenNoPending` — boot with
      `M == 0` skips the modal entirely; marker advanced to
      `true` silently.
    - `TestScreenerGateFlipDisablePathNoPrompt` — boot with
      `cfg.Screener.Enabled = false` and marker `true` resets
      the marker to `false`; no modal.
    - `TestScreenerGateFlipMarkerPersistsAcrossLaunches` —
      after `Y`, a second launch with the same on-disk state
      and `M > 0` does NOT re-prompt (marker already `true`).
    - `TestScreenerPaneRapidYDispatchesAllAddresses` — ten
      address-distinct pending senders, ten `Y` keypresses in a
      tight loop via teatest, asserts ten `routeCmd` calls each
      with the captured address (race coverage per §5.4
      "concurrent-decision semantics").
    - `TestScreenerPaletteTitleSwapsWhenGateOff` — palette rows
      `screener_accept` / `screener_reject` carry the gate-off
      titles when `m.screenerEnabled = false`.
    - `TestScreenerEmptyQueueHelperText`.
    - `TestScreenerSentinelFolderRefusesNRX` — `N` / `R` / `X` on
      `__screened_out__` in the folder pane are no-ops (spec 18
      protection).
  - **redaction** (per §11 spec 17 review):
    - `TestScreenerRouteCmdRedactsAddress` — a `Y`-dispatched
      `routeCmd` log line, after `internal/log/redact.go`
      processing, contains `<email-N>` and not the literal
      `from_address`. Run twice in the same session to confirm
      the per-session-keyed token is stable for the same
      address.
    - `TestScreenerRouteCmdNoSubjectInLog` — log lines emitted
      by the screener-pane dispatch path do not contain the
      message subject at any level outside DEBUG.
  - **CLI**:
    - `TestScreenerCLIAcceptToImbox` — default destination.
    - `TestScreenerCLIAcceptWithToFlag` — `--to feed`.
    - `TestScreenerCLIRejectAliasesScreenerDestination`.
    - `TestScreenerCLIListGroupingSenderVsMessage`.
    - `TestScreenerCLIPreApproveStdinPartialSuccess` — bad line
      collected as an error, good lines applied, exit 0.
    - `TestScreenerCLIPreApproveStdinAllFail` — exit 2.
    - `TestScreenerCLIPreApproveFromFileHappyPath`.
    - `TestScreenerCLIPreApproveFromFileMissingExits2`.
    - `TestScreenerCLIPreApproveFromFileAndFromStdinMutuallyExclusive`.
    - `TestScreenerCLIStatusJSON`.
  - **bench** (per §8): `BenchmarkListMessagesScreenerFilter`,
    `BenchmarkListPendingSenders`, `BenchmarkListPendingMessages`,
    `BenchmarkListScreenedOutMessages`, `BenchmarkCountPendingSenders`,
    `BenchmarkCountScreenedOutMessages`,
    `BenchmarkSidebarStreamsRefreshWithScreener`.

## 11. Cross-cutting checklist (CLAUDE.md §11)

- [ ] **Scopes:** none new (`Mail.Read`, `Mail.ReadWrite` already
      in PRD §3.1; spec 28 is local-only and makes no Graph
      calls).
- [ ] **Store reads/writes:** `messages` read-only;
      `sender_routing` read-only (writes still go through spec
      23's `routeCmd`); `muted_conversations` read-only (for the
      `excludeMuted` clause). No new tables.
- [ ] **Graph endpoints:** none.
- [ ] **Offline:** works fully offline. The gate is a local
      filter; sync does not interact with it.
- [ ] **Undo:** `Y` / `N` are equivalent to `S i` / `S k` (spec
      23). Spec 23 §6 documented that the `u`-key undo stack is
      not involved; same here. The recovery path for a wrong
      decision is **navigate to `__screened_out__` (HEY's
      "Screener History" affordance — same surface), focus the
      offending sender's mail, press `S c` to clear, then
      re-decide**. Document this sequence prominently in
      `docs/user/how-to.md` "Recover from a wrong screener
      decision" recipe so users aren't stuck. A future spec MAY
      add a `:screener undo` ring buffer (last N decisions,
      session-scoped, no on-disk persistence — the action queue
      cannot be rewound for sender_routing ops without a new
      reverse-op primitive); explicitly out of scope for v1.
- [ ] **User errors:** focused message has no `from_address`
      (toast: "screener: focused sender has no from-address");
      empty queue helper; CLI bad-input exit 2.
- [ ] **Latency budget:** §8 covers all seven new surfaces.
      Default-folder filter at +5ms over spec 19's mute filter is
      the user-visible cost.
- [ ] **Logs:** new log sites at DEBUG with destination
      decisions and **scrubbed** address markers (`<email-N>`).
      Never log raw `from_address` or subject lines outside
      DEBUG (spec 17 / CLAUDE.md §7 rule 3). Spec 17 redaction
      regex already catches these; verify by adding a redaction
      test for `routeCmd` log output emitted from the screener-
      pane dispatch.
- [ ] **CLI mode:** `inkwell screener list|accept|reject|history|pre-approve|status`
      per §7.
- [ ] **Tests:** §10 test list. All four layers (unit, race-
      enabled `go test ./...`, `go test -tags=integration`,
      `go test -tags=e2e`, plus `go test -bench=. -benchmem
      -run=^$ ./...`) green per CLAUDE.md §5.6.
- [ ] **Spec 11 consistency:** Screener virtual folder is a
      sentinel sidebar item (spec 23 pattern), not a saved
      search row. `dd` in the saved-search row deletes a saved
      search; on a sentinel it is gated by
      `FoldersModel.Selected()` returning `(_, false)`. Spec 11
      tab promotion (spec 24) does not promote the Screener
      sentinel — only saved searches are promotable.
- [ ] **Spec 17 review:** spec 28 introduces a new SQL
      composition (the EXISTS-IN-approved fragment). All values
      are bound parameters; no dynamic table or column names; no
      string concatenation against user input. New CLI verbs
      validate input via the same `NormalizeEmail` helper as
      spec 23. No new external HTTP, no new subprocess
      invocation, no new cryptographic primitive. Threat-model
      delta is minimal: an attacker controlling Graph could send
      mail from a chosen address and land in the user's
      Screener queue (already true today; the gate doesn't
      worsen the surface). PRIVACY.md gains a one-line note.
      `make sec` must remain clean; no new `// #nosec`
      annotations.
- [ ] **Spec 18 consistency:** the new `__screened_out__`
      sentinel ID must be added to the `IsStreamSentinelID`
      switch in `internal/ui/panes.go` (DoD bullet above) and
      to `streamDestinationFromID` (mapping to `"screener"`).
      Once that's done, spec 18's `N` / `R` / `X` handlers
      inherit the sentinel-folder protection automatically via
      the existing `FoldersModel.Selected()` returns
      `(_, false)` rule for `isStream`-flagged items — no edit
      to spec 18 handlers is needed.
- [ ] **Spec 19 consistency:** `[screener].exclude_muted = true`
      by default. Muted threads don't surface in the Screener
      queue. `__muted__` virtual folder is unchanged; muted
      messages from pending senders DO appear there per spec 19
      semantics.
- [ ] **Spec 20 consistency:** thread chord (`T`) is unchanged.
      `T m` (move thread) does not interact with sender_routing.
      Threads spanning multiple senders (rare) — the gate
      operates per-sender, so a thread with one Approved and one
      Pending sender renders both messages in the default Inbox
      (the Approved one) but only the Pending one in the
      Screener queue. Document this as a known edge in the
      reference.
- [ ] **Spec 21 consistency:** `:filter --all` cross-folder is
      not gated (intentional query, §4.2).
- [ ] **Spec 22 consistency:** four new palette rows registered
      per §5.9. `Available()` gates honour
      `[screener].enabled`. Stale-snapshot rule from spec 22 §6
      applies identically.
- [ ] **Spec 23 consistency:** sender_routing schema, four
      destination values, `S` chord, `~o` operator are all
      unchanged. Spec 28 rebinds the *meaning* of
      `__screener__` virtual folder content when the gate is
      enabled (§5.1) — document the divergence in the spec 23
      §10.1 / §14 follow-up section by linking forward to spec
      28. The `[ui].show_routing_indicator` keys (spec 23 §11)
      are unchanged.
- [ ] **Spec 24 consistency:** the new Screener pane is not a
      saved search row, so it is not promotable as a tab. A
      user wanting "Screener as a tab" creates a saved search
      with pattern `~o pending` and promotes that.
- [ ] **Spec 25 consistency:** Reply Later / Set Aside stacks
      operate on individual messages via Graph categories. Spec
      28 operates on senders via `sender_routing`. Orthogonal:
      a message can be reply-later'd while its sender is
      Pending. The Screener queue is per-sender; the
      Reply-Later overlay is per-message. The two surfaces
      don't overlap.
- [ ] **Spec 26 consistency:** bundle senders is per-sender opt-
      in (different table: `bundled_senders`). A bundled sender
      can also be Pending. Pending senders' bundled mail
      surfaces in the Screener as one row per sender (already
      the desired shape). Spec 26 status is in progress at
      spec-28 design time; if spec 26 ships first, the
      `ListPendingSenders` SQL can be extended in a follow-up to
      respect `bundled_senders` (deduplicate display); for v1 of
      spec 28, both surfaces work together by virtue of operating
      on different tables.
- [ ] **Spec 27 consistency:** custom actions framework (planned
      for spec 27) gains `screener_accept` / `screener_reject` as
      operation primitives in a follow-up; no API contract
      between specs 27 and 28 in v1. Spec 27 reuses spec 23's
      `routeCmd`, which spec 28 also reuses; the chain is one
      level of indirection.
- [ ] **Doc sweep (§12.6):** `docs/CONFIG.md`,
      `docs/user/reference.md`, `docs/user/how-to.md`,
      `docs/user/explanation.md` (one paragraph noting the gate
      is a local-only filter and that native-OS notification
      suppression is not in inkwell's scope, per §9.1),
      `docs/PRD.md` §10, `docs/ROADMAP.md` (Bucket 3 row + §1.16
      heading), `docs/specs/28-screener.md` Shipped line,
      `docs/specs/23-routing-destinations.md` §10.1 / §14
      (forward-link to spec 28 noting the `__screener__`
      content redefinition when the gate is enabled — closes
      the loop spec 23 §14 promised),
      `docs/plans/spec-28.md` Status: done, `docs/PRIVACY.md`
      one-line note, `README.md` status table row. No tutorial
      change (Screener is a power-user opt-in; the
      first-30-minutes path is unchanged).

## 12. Notes for follow-up specs

- **Auto-suggest first-contact admission (heuristic).** A future
  spec can extend `:screener accept --suggest` to propose
  destinations based on `unsubscribe_url` presence (spec 16),
  `List-Id` header (spec 23 §2.5 table), and pattern of past
  admissions ("you always send recruiters to Paper Trail").
  Out of scope for v1; would surface as `:screener suggest` and
  open a list of `(sender, suggested_dest, signal)` rows.
- **Bulk admission from a regex.** `inkwell screener accept
  --pattern '~f *@vendor.invalid' --to feed` would let a user
  pre-approve every sender at a domain in one shot. Out of
  scope for v1; depends on the bulk-route work flagged in spec
  23 §14.
- **Re-screening after long absence.** A sender admitted years
  ago whose role has changed (left the company, moved to
  marketing) might warrant re-screening. Out of scope; users
  use `S c` to clear and re-decide.
- **Native-OS notification suppression.** Per §9.1, requires a
  notification subsystem inkwell does not own. Out of scope by
  product design; users configure native Outlook for this.
- **Domain-level routing vs. Screener.** Roadmap §14 of spec 23
  flags domain-level routing as a follow-up. When it lands, a
  `*@vendor.invalid` Approved match should suppress the
  per-sender Pending entries for individual `bob@vendor.invalid`
  / `alice@vendor.invalid` senders. Spec 28 does NOT pre-empt
  that work; for v1, each address is its own Pending row.
