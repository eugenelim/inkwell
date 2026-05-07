# Spec 23 — Routing destinations (Imbox / Feed / Paper Trail / Screener)

**Status:** Ready for implementation.
**Depends on:** Specs 02 (store), 04 (TUI sidebar), 08 (pattern
language — adds `~o` operator), 10 (filter UX — `;` chord parity),
11 (saved searches — TOML mirror conventions), 19 (mute — both the
hardcoded-virtual-folder precedent in §5.4 *and* the `ExcludeMuted`
semantics in routing views), 22 (command palette — routing surfaces
as static palette rows, see §13).
**Blocks:** Screener (roadmap §1.16) — depends on the
`sender_routing.destination = 'screener'` rows shipped here. Custom
actions framework (roadmap §2) — exposes `set_sender_routing` as an
op primitive. Split-inbox tabs (roadmap §1.7) — routing virtual
folders are the natural seed for tabbed splits.
**Estimated effort:** 1–2 days.

---

## 1. Goal

Divide incoming mail into intent-based streams instead of urgency-
based: real correspondence in **Imbox**, newsletters / digests / list
traffic in **Feed**, receipts and transactional in **Paper Trail**,
and unknown-or-screened-out senders in **Screener**. Routing is per-
sender — once you assign `news@example.com` to Feed, every past and
future message from that address surfaces in Feed and disappears
from the Imbox view. The pattern is HEY's "Imbox / Feed / Paper
Trail / Screener" model, adapted to a TUI mail client backed by
Microsoft Graph (which has no equivalent server-side concept).

The user sees four virtual folders in the sidebar; pressing a chord
on a focused message moves that sender into a destination; reassign
is one keystroke and is **retroactive** — past mail follows the
sender. The actual inbox folder still works the way it always has;
the routing buckets are an additional, parallel view.

### 1.1 What does NOT change

- The user's actual mail folders (Inbox, Sent Items, Archive,
  user-created folders) are untouched. Nothing is moved server-side.
- The cached `inference_class` column and the `~y focused` / `~y
  other` pattern operator are unchanged. Routing reads the
  `from_address` field exclusively; the Focused/Other read path is
  orthogonal.
- The PATCH endpoint
  `/me/inferenceClassificationOverrides` is **not** called. v1 does
  not write any per-sender preference to Graph (rationale in §2.2).
- The Screener gating UX ("new senders sit in a queue until
  accepted; mail from screened-out senders is hidden from default
  views") is **not** in this spec — it is a separate roadmap item
  (Roadmap §1.16) that will reuse the `sender_routing` table shipped
  here. v1 of routing ships the `screener` destination value and a
  Screener virtual folder so the user can manually park mail there
  for review, but mail routed to Screener still appears in the
  user's actual Inbox folder. The user-visible value of the v1
  Screener bucket is "all senders I've marked as 'deal with later'
  in one place"; the automatic hiding behaviour comes with the
  follow-up spec.
- Bulk re-routing across many senders at once (`;S i`) is out of
  scope. Only the focused message's sender is reassigned per chord
  invocation.

## 2. Prior art

### 2.1 The sender-routed model — HEY

HEY (Basecamp, 2020) is the canonical implementation of intent-based
streams. Senders are bucketed once via The Screener — a one-time
"Yes / No, this sender goes to Imbox / Feed / Paper Trail" decision
on first contact. Reassign is **retroactive**: past mail follows the
sender. There is no automatic ML classification; routing is entirely
user-curated. The Screener gates new senders ("Screened Out" silently
catches mail you've rejected; rejected senders' mail still arrives but
is hidden from default views). Reply-to-a-sender does NOT auto-promote
to Imbox; the user must explicitly accept them.

Inkwell adopts HEY's data model (per-sender routing table, retroactive
on reassign) but defers the new-sender Screener gate to spec 1.16. v1
ships the four buckets as opt-in virtual folders that augment, rather
than replace, the existing folder UX.

### 2.2 Server-side classification — Gmail / Outlook / Apple Mail

- **Gmail Categories** (Primary / Promotions / Social / Updates /
  Forums) are server-side ML, with per-sender override via "Move to
  tab → Always do this for messages from this sender." Reassignment is
  prospective by default with a one-time prompt to retroactively
  re-bucket past mail. Override is a stable per-sender rule.
- **Outlook Focused / Other** uses
  `inferenceClassification` (server-side ML) with explicit per-sender
  override via the
  `/me/inferenceClassificationOverrides` Graph resource — a
  `(senderEmailAddress, classifyAs)` mapping. Override is **prospective
  only**; past messages keep their previous classification. This is
  the Microsoft-native equivalent and is documented in
  `learn.microsoft.com/graph/api/resources/inferenceclassificationoverride`.
- **Apple Mail Categories** (macOS 15 / iOS 18, 2024): Primary /
  Transactions / Updates / Promotions, on-device classification with
  user override via "Categorize Sender". Override is retroactive
  within the local view. Categories are Mail-app-local — they don't
  sync as IMAP labels.

Inkwell deliberately ignores `inferenceClassificationOverride`.
Reasons: (a) it's prospective-only, which contradicts the HEY
"reassign moves past mail too" semantics; (b) it only captures the
binary Focused / Other distinction, not Imbox / Feed / Paper Trail /
Screener; (c) the local table is faster, fully offline, and not
gated on a Graph round-trip per assign.

### 2.3 Query-based splits — Superhuman / Spark / Fastmail

- **Superhuman Split Inbox** is **query-based** (each split is a
  saved search), not sender-keyed. Switch with ⌘1..9. No first-contact
  gate.
- **Spark Smart Inbox** auto-segments into Personal / Newsletters /
  Notifications / Pinned / Seen, server-side classified on Readdle's
  infrastructure. Per-sender override exists and is retroactive within
  Spark's view; classifier internals are not publicly documented.
- **Fastmail** has no native bucket UX. Routing is via Sieve scripts
  (RFC 5228) that `fileinto` folders or apply keywords. Sieve runs
  at delivery — prospective only by default.

Inkwell's routing virtual folders are **query-based** in the same
sense as Superhuman's splits, but the queries are **fixed and
sender-keyed** rather than user-defined. Saved searches (spec 11)
remain available for arbitrary queries; routing buckets are the four
hardcoded streams.

### 2.4 Terminal clients

- **notmuch** (and frontends `alot`, `astroid`): tags are the
  routing primitive. `notmuch tag +newsletter -inbox from:foo@`
  (run as a `pre-new` / `post-new` hook or manually) buckets a
  sender. Retroactive tagging is one command. Saved searches become
  buckets. Closest precedent for the inkwell model.
- **mutt / neomutt**: `save-hook ~f foo@bar =Feed` files mail from a
  sender into a folder at delivery time (procmail / maildrop / Sieve
  upstream). No screener; routing is delivery-time and folder-based.
- **aerc**: filter rules in `accounts.conf` and the `:filter` command;
  virtual folders by query. Less developed than notmuch's tag model.

### 2.5 Heuristic header signals

These RFC-standardised headers are commonly used to seed routing
heuristics:

| Header | Signal | RFC |
|--------|--------|-----|
| `List-Id` | Strong Feed (mailing list) | RFC 2919 |
| `List-Unsubscribe` + `List-Unsubscribe-Post` | Bulk / marketing → Feed | RFC 2369, RFC 8058 |
| `Auto-Submitted: auto-generated` | Paper Trail (transactional) | RFC 3834 |
| `Precedence: bulk \| list \| junk` | Feed | informal, widely deployed |
| `Feedback-ID` | Bulk sender ID → Feed | Google Postmaster |

v1 does **not** auto-route on these headers. The roadmap (§1.27) calls
out heuristic auto-categorisation as research; for spec 23 we ship
the routing primitive (per-sender table + virtual folders + chord
UX), and a separate spec can add a "suggest routing" command later.
Inkwell's existing `unsubscribe_url` column (spec 16) is a useful
suggestion-time signal: a sender whose last 10 messages all carry an
unsubscribe link is plausibly a Feed candidate. Out of scope for v1.

### 2.6 Design decision

Inkwell follows HEY for the **data model** (per-sender routing
table; retroactive on reassign) and notmuch for the **execution
model** (virtual folders backed by queries against a tag-like local
table). Server-side overrides
(`inferenceClassificationOverride`) are intentionally not used. The
four buckets are hardcoded virtual folders following the spec 19
§5.4 "Muted Threads" precedent — sentinel folder ID, not user-
creatable, not deletable from the sidebar (the spec 19 protection
on `__muted__` is the only existing precedent; spec 18's `X` is the
first binding that has to learn about routing sentinels). The one
deliberate divergence from spec 19 §5.4: routing virtual folders are
**always rendered**, even at zero count. Rationale at the end of
§5.4.

## 3. Schema

Migration **`011_sender_routing.sql`** is the next available number:
migrations 001–010 are applied on disk under
`internal/store/migrations/`; the inventory has no gap (spec 22 in
the roadmap is the command palette, which is UI-only and ships no
migration). Verify pre-merge with
`ls internal/store/migrations/`.

```sql
CREATE TABLE sender_routing (
    email_address TEXT    NOT NULL CHECK(length(email_address) > 0),
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    destination   TEXT    NOT NULL CHECK(destination IN ('imbox', 'feed', 'paper_trail', 'screener')),
    added_at      INTEGER NOT NULL,   -- unix epoch seconds
    PRIMARY KEY (email_address, account_id)
);
CREATE INDEX idx_sender_routing_account_dest ON sender_routing(account_id, destination);

-- Expression index on lower(trim(from_address)) so the routing JOIN
-- (lower(trim(m.from_address)) = sr.email_address) does not full-scan
-- messages on a 100k-message store. Without this the call to lower()
-- defeats idx_messages_from (which is on the raw column). See §4.2.
-- NOT a partial index — partial-index predicates (WHERE …) only
-- engage when the JOIN's WHERE clause syntactically matches; keeping
-- the index general-purpose lets the planner pick it without
-- predicate-side gymnastics. The expression-index entries for rows
-- whose from_address is NULL or empty are themselves NULL/'' and
-- trivial; the storage cost is negligible.
CREATE INDEX idx_messages_from_lower
    ON messages(account_id, lower(trim(from_address)));

UPDATE schema_meta SET value = '11' WHERE key = 'version';
```

**Design rationale:**

- **Composite PK `(email_address, account_id)`**: same column order
  as `muted_conversations` (spec 19 §3): the "thing being routed"
  first, the account second. The PK index covers the JOIN-time
  lookup `WHERE sr.email_address = ? AND sr.account_id = ?` because
  both columns are bound in equality. The PK index does NOT cover an
  account-only scan (e.g., `ListSenderRoutings(account_id, "")`);
  `idx_sender_routing_account_dest` is the fallback for that path.
- **Multi-account isolation**: a user with two accounts may
  legitimately route the same sender to different destinations on
  each (e.g., `aws-billing@amazon.com` → Paper Trail on personal,
  → Imbox on work). Composite PK enforces uniqueness per account.
- **`email_address` stored lowercase + trimmed**: callers MUST
  normalise via `NormalizeEmail` before insert and lookup. The
  default index collation is BINARY (case-sensitive); we normalise
  at the call site rather than apply `COLLATE NOCASE` because (a)
  `COLLATE NOCASE` is ASCII-only — any future IDN handling would
  need a code-side normaliser anyway, so doing it once at the
  boundary is simpler; (b) the matching `messages.from_address`
  column is NOT normalised at write time (verified — see §4.2 SQL),
  so `lower(trim(from_address))` already appears in the JOIN
  predicate; matching that with a plain BINARY column on
  `sender_routing` is the simplest invariant.
- **`messages.from_address` asymmetry**: existing rows on `messages`
  carry whatever Graph returned (mixed case, occasional leading
  whitespace). This spec does NOT modify `messages` rows; the JOIN
  uses `lower(trim(...))` so both case and whitespace differences
  are tolerated. Callers writing to `sender_routing` always
  normalise via `NormalizeEmail`. Document the asymmetry in
  `internal/store/sender_routing.go`.
- **`destination` as a TEXT CHECK constraint**: the four values are
  a fixed contract with the UI and CLI. An enum table would add a
  JOIN for every list query. CHECK keeps it cheap and rejects bad
  inserts at the SQLite layer.
- **`CHECK(length(email_address) > 0)`**: defence in depth — the Go
  layer rejects empty strings before insert (see §4.1
  `ErrInvalidAddress`), but the constraint catches a buggy caller.
- **No `sender_name`**: display names are volatile (senders edit
  their From: name freely). Routing is keyed on the immutable
  address.
- **`added_at`**: enables `inkwell route list --sort added` and the
  "recently routed" hint. Note: `SetSenderRouting` does NOT update
  `added_at` on a no-op upsert (same destination); see §5.7.
- **Index `idx_sender_routing_account_dest`** covers the bucket-
  listing query for `(account_id, destination)`. It also serves as
  the `account_id`-only prefix index when callers do not pin
  destination.
- **Expression index `idx_messages_from_lower`**: required for the
  routing JOIN's case- and whitespace-insensitive equality on
  `messages.from_address`. The partial-index predicate
  (`from_address IS NOT NULL AND length > 0`) keeps the index small
  by skipping the rare drafts and synthesised rows that have no
  sender. Adds ~5% write cost per `UpsertMessages` call (verified
  in `BenchmarkUpsertMessagesBatch`); the budget headroom from spec
  02 (UpsertMessagesBatch <50ms p95) absorbs it.

## 4. Store API

### 4.1 New store methods

```go
// SetSenderRouting upserts a (account, sender) → destination row.
// emailAddress is normalised via NormalizeEmail. destination must be
// one of "imbox", "feed", "paper_trail", "screener" — any other value
// is rejected with ErrInvalidDestination.
//
// Read-then-write: the implementation does GetSenderRouting first
// and short-circuits when prior == destination (no SQL write, no
// added_at bump — see §5.7). Returns the prior destination ("" if
// the sender was unrouted). The "no-op" case is reported via
// (prior == destination), NOT via a sentinel error — callers
// inspect the returned prior to choose between the routed and
// already-routed UI toasts.
SetSenderRouting(ctx context.Context, accountID int64,
    emailAddress, destination string) (prior string, err error)

// ClearSenderRouting removes the row. Returns the prior destination
// ("" if the sender was unrouted; in that case the call is a no-op).
// Mirrors UnmuteConversation (spec 19 §4.1) but extends the return
// shape so the caller can distinguish "cleared" from "was already
// unrouted".
ClearSenderRouting(ctx context.Context, accountID int64,
    emailAddress string) (prior string, err error)

// GetSenderRouting returns the destination for a sender, or "" if
// the sender is unrouted. emailAddress is normalised before lookup.
GetSenderRouting(ctx context.Context, accountID int64,
    emailAddress string) (string, error)

// ListSenderRoutings returns all rows for the account, optionally
// filtered to a single destination. Empty destination = all.
// Ordered by destination then email_address.
ListSenderRoutings(ctx context.Context, accountID int64,
    destination string) ([]SenderRouting, error)

// ListMessagesByRouting returns messages whose sender appears in
// sender_routing for the given destination, ordered by received_at
// DESC. excludeMuted is honoured (callers pass true for the routing
// virtual folders to keep muted threads out of the bucket views,
// matching spec 19 §5.3 default-folder behaviour).
//
// Argument shape note: spec 19 puts ExcludeMuted on a MessageQuery
// struct because that struct already carries 6+ fields. This
// function has only one toggle, so the positional bool is in
// keeping with the rest of the store package's narrow per-call
// signatures (e.g., ListMutedMessages takes accountID + limit).
// If a second toggle is added (e.g., excludeRead), promote both to
// a RoutingQuery struct.
ListMessagesByRouting(ctx context.Context, accountID int64,
    destination string, limit int, excludeMuted bool) ([]Message, error)

// CountMessagesByRouting returns the count of messages whose sender
// is routed to destination. Used for one-off lookups (CLI,
// `inkwell route show`); the sidebar uses CountMessagesByRoutingAll
// instead. Honours excludeMuted identically to ListMessagesByRouting.
CountMessagesByRouting(ctx context.Context, accountID int64,
    destination string, excludeMuted bool) (int, error)

// CountMessagesByRoutingAll returns the count of messages per routing
// destination, batched into one GROUP BY query. The map keys are the
// four destination values; missing keys mean zero. Used by the
// sidebar refresh path (§9 BenchmarkSidebarBucketRefresh).
CountMessagesByRoutingAll(ctx context.Context, accountID int64,
    excludeMuted bool) (map[string]int, error)
```

The `SenderRouting` struct:

```go
type SenderRouting struct {
    EmailAddress string
    AccountID    int64
    Destination  string
    AddedAt      time.Time
}
```

### 4.2 `ListMessagesByRouting` SQL

```sql
SELECT m.<columns>
FROM messages m
JOIN sender_routing sr
  ON  sr.account_id    = m.account_id
  AND sr.email_address = lower(trim(m.from_address))
WHERE m.account_id    = :account_id
  AND sr.destination  = :destination
  -- optional anti-join when excludeMuted = true (spec 19 §4.2):
  AND (
    m.conversation_id IS NULL
    OR m.conversation_id = ''
    OR NOT EXISTS (
        SELECT 1 FROM muted_conversations mc
        WHERE mc.conversation_id = m.conversation_id
          AND mc.account_id = :account_id
    )
  )
ORDER BY m.received_at DESC
LIMIT :limit
```

`messages.from_address` is stored as Graph returns it (mixed case;
occasional leading whitespace from malformed responses).
`sender_routing.email_address` is always lowercase + trimmed.
`lower(trim(m.from_address))` normalises the messages side to match.
This expression matches the partial expression index
`idx_messages_from_lower` from §3, so the planner can use the index
for the JOIN probe. Verify with `EXPLAIN QUERY PLAN` in the
benchmark setup (§9): the expected plan is `SCAN sr USING INDEX
idx_sender_routing_account_dest` driving `SEARCH m USING INDEX
idx_messages_from_lower`.

### 4.3 Pattern operator `~o <destination>`

Spec 08 gains a new operator:

```
~o imbox        — match messages whose sender is routed to Imbox
~o feed         — match messages whose sender is routed to Feed
~o paper_trail  — match messages whose sender is routed to Paper Trail
~o screener     — match messages whose sender is routed to Screener
~o none         — match messages whose sender is NOT in sender_routing
                   (i.e., unrouted)
```

**Strategy:** `LocalOnly`. Microsoft Graph has no equivalent
server-side concept — `inferenceClassificationOverride` only covers
Focused / Other. When `~o` appears with server-side operators
(`~b`, `~B`, `~h`), the planner falls into `TwoStage` execution: run
the server query, then locally JOIN against `sender_routing`. This
matches the existing handling of `~y` (inference) and `~F` (flag) —
local-only refinements over a server result set.

**Compilation (LocalSQL fragment).** `eval_local.go` emits
predicates against unqualified `messages` columns (the outer query
in `SearchByPredicate` does not alias `messages`). The EXISTS
sub-clause MUST use unqualified outer references too:

```sql
-- ~o imbox compiles to:
EXISTS (
    SELECT 1 FROM sender_routing sr
    WHERE sr.account_id    = account_id
      AND sr.email_address = lower(trim(from_address))
      AND sr.destination   = 'imbox'
)

-- ~o none compiles to:
NOT EXISTS (
    SELECT 1 FROM sender_routing sr
    WHERE sr.account_id    = account_id
      AND sr.email_address = lower(trim(from_address))
)
```

(`§4.2`'s `ListMessagesByRouting` uses an explicit `m` alias
because that query is freshly written, not emitted from
`eval_local`. The two SQL surfaces can use different alias
conventions.)

**Why `EXISTS` not a JOIN:** the predicate is composable with other
local operators (`~o feed & ~U`, `~o feed | ~o paper_trail`); a JOIN
in `SearchByPredicate` would require restructuring the existing
predicate composition. `EXISTS` slots into the `WHERE` clause as a
sub-clause without changing the outer query shape.

**Why a new `~o`, not a new `~G` value:** categories (`~G`) are a
Graph concept (mirrored from `messages.categories`); routing is a
local concept. Conflating them confuses users who would expect
`~G Feed` to round-trip with `add_category Feed` (spec 07).

**Negation vs `~o none` — distinct semantics.** `! ~o feed` and
`~o none` both compile to a `NOT EXISTS` form, but the predicates
differ:

- `! ~o feed` → "the sender is NOT routed to Feed" — matches
  unrouted senders AND senders routed to Imbox / Paper Trail /
  Screener.
- `~o none` → "the sender has no row in `sender_routing`" — matches
  unrouted senders only.

The parser must compile `! ~o feed` as the *negation of the EXISTS
form for the specified destination*, NOT as a substitution to
`~o none`. Document this in the operator's reference and add a
parser test (§12). A user who wants "everything except Feed,
including unrouted" types `! ~o feed`; a user who wants "unrouted
only" types `~o none`.

### 4.4 Pattern operator parser

The new operator requires changes in three pattern-package files:

1. **`lexer.go::isOpLetter`** — extend the switch to accept `'o'`.
   Without this, `~o feed` lexes as `~` + identifier `o feed`.
2. **`lexer.go::fieldForOp`** (or equivalent dispatch) — map `'o'`
   to the new `FieldRouting` constant.
3. **`ast.go`** — add `FieldRouting` to the field-tag enum.
4. **`parser.go`** — add a `parseRoutingValue` helper that consumes
   the next token and validates it against the allowed set
   (`imbox` / `feed` / `paper_trail` / `screener` / `none`). Invalid
   values raise: `unknown routing destination "foo"; expected one of
   imbox, feed, paper_trail, screener, none`. `paper-trail` (hyphen)
   is rejected with the same message — only the underscore form is
   accepted.
5. **`compile.go`** (or `eval_local.go`) — add the `EXISTS` /
   `NOT EXISTS` SQL emitter for `FieldRouting` predicates.
6. **`eval_filter.go`** (Graph `$filter` builder) and
   **`eval_search.go`** (Graph `$search` builder) — both must
   return `ErrUnsupportedFilter` when the AST contains a
   `FieldRouting` node, mirroring the existing `~h` (raw header)
   handling. The compile.go strategy selector then forces
   `LocalOnly` (or `TwoStage` if combined with server-only
   operators).

## 5. UI

### 5.1 The chord — `S` (Stream/Sort-to)

`S` is chosen because:
- `S` (Shift+s) is unused in `internal/ui/keys.go::DefaultKeyMap`
  (verified — the only globals using `S` glyphs are
  `PrevPane` = `shift+tab` and `End` = `G`, neither conflicting).
- Lowercase `s` is unused at the global level. (Some pane-scoped
  modes — compose draft, command-bar text input — pass `s` through
  to the embedded text widget; routing chord activation only fires
  at the list / viewer dispatch layer, before any text-mode
  consumes the key.)
- Capital letters are the convention for "this affects more than
  the single message" (per spec 19 §5.1 rationale: `D` permanent-
  delete, `N/R/X` folder ops). Routing assigns the entire sender —
  past and future — so it qualifies.
- `T` (thread chord, spec 20) and `S` (stream chord, this spec) are
  symmetric uppercase chord prefixes that disambiguate multi-target
  verbs.

Pressing `S` in the list pane or viewer pane enters
**stream-chord-pending** state. The status bar shows:

```
stream: i/f/p/k/c  esc cancel
```

A 3-second timeout starts (same shape as spec 20 §3.1). A second
keypress completes the chord:

| Chord | Action                                                | Confirm modal? |
|-------|-------------------------------------------------------|----------------|
| `S i` | Route the focused message's sender to **Imbox**       | No |
| `S f` | Route the focused message's sender to **Feed**        | No |
| `S p` | Route the focused message's sender to **Paper Trail** | No |
| `S k` | Route the focused message's sender to **Screener**    | No |
| `S c` | **Clear** routing for the focused message's sender    | No |

**Mnemonics:** `i` = imbox, `f` = feed, `p` = paper trail, `k` =
s**k**reener (`s` is reserved against `S S` double-press confusion
— users hitting Shift twice instead of releasing it would otherwise
silently route to Screener), `c` = **c**lear.

`Esc` or any unrecognised key while in chord-pending state cancels
and clears the status bar. A 3-second timeout with no second key
also cancels.

**Pane scope:** `S` is active in the list pane and viewer pane. It
is NOT active in the folder sidebar pane.

**Chord-pending discipline (per spec 19 §5.1 / spec 20 §3 pattern).**
While `streamChordPending == true`, every keypress is consumed by
the chord handler — global bindings (e.g., the lowercase `c` =
`AddCategory`, `f` = `ToggleFlag`) are NOT triggered. Pressing any
unrecognised key clears pending and the original action does NOT
fire (the user must press the key again after cancel).

**Cross-chord interaction with `T` (spec 20).** Spec 20's chord-
pending discipline matches spec 19: any unrecognised second key
clears pending without firing an action. `T` is not a valid second
key for `T`-chord (only `r/R/f/F/d/D/a/m`), so a second `T` press
clears the thread chord without entering a new one. Symmetric for
`S`: a second `S` press while `streamChordPending` clears the
stream chord without entering a new one (the "double-Shift safety"
from the §5.1 mnemonic rationale).

When chord A is pending and the user presses chord B's prefix
(e.g., `S` while `threadChordPending`): the keypress is treated as
an unrecognised second key for chord A, clearing chord A's pending
state. The user must press chord B again to enter chord B. This
matches the existing spec 20 self-cancel behaviour (`T` while
`threadChordPending` cancels and does not start a fresh chord).
Net effect: chord prefixes don't auto-switch — one cancel, one
start. Cover with `TestStreamChordTPressCancelsStreamChord` (T
while stream-pending → cancel; subsequent `S T r` → no action) and
the symmetric `TestThreadChordSPressCancelsThreadChord`.

### 5.2 KeyMap changes

Add to `internal/ui/keys.go`:

```go
// KeyMap struct:
StreamChord key.Binding

// BindingOverrides struct:
StreamChord string
```

Default: `key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "stream chord"))`.

Wire through `ApplyBindingOverrides` (one new line) and include in
`findDuplicateBinding` scan (one new entry) — same pattern as
`ThreadChord` in spec 20.

### 5.3 UI model fields

```go
// Model — add:
streamChordPending bool
streamChordToken   uint64
```

Implementation parallels spec 20 §3.1 / §4.5 exactly. The token field
is incremented each time `S` is pressed; stale `streamChordTimeoutMsg`
deliveries (token mismatch) are no-ops.

```go
type streamChordTimeoutMsg struct{ token uint64 }

func streamChordTimeout(token uint64) tea.Cmd {
    return func() tea.Msg {
        <-time.After(3 * time.Second)
        return streamChordTimeoutMsg{token: token}
    }
}
```

### 5.4 Routing virtual folders in the sidebar

Four hardcoded virtual folders, modelled on spec 19 §5.4 ("Muted
Threads"). Sentinel folder IDs (double-underscore prefix avoids
collision with Graph's base64url folder IDs). The three names —
**internal destination value**, **sentinel folder ID**, and
**display name** — are distinct on purpose: the destination value
is the API contract (CLI, pattern operator, store rows), the
sentinel ID is the sidebar key, the display name is the human
label.

| Destination value | Sentinel ID       | Display name      |
|-------------------|-------------------|-------------------|
| `imbox`           | `__imbox__`       | `Imbox`           |
| `feed`            | `__feed__`        | `Feed`            |
| `paper_trail`     | `__paper_trail__` | `Paper Trail`     |
| `screener`        | `__screener__`    | `Screener`        |

**Sentinel-folder protections — implementation strategy.** The
existing muted sentinel (spec 19 `__muted__`) is implemented as a
non-`store.Folder` sidebar item carrying an `isMuted` flag on
`folderItem` (`internal/ui/panes.go`); `FoldersModel.Selected()`
returns `(_, false)` for items with `isMuted = true`, and the spec
18 `N`/`R`/`X` handlers short-circuit on that. Routing virtual
folders adopt the same pattern with an `isStream` flag (and a
`streamDestination string` field carrying `"imbox"` / `"feed"` /
`"paper_trail"` / `"screener"`). `Selected()` returns
`(_, false)` for stream items, so spec 18's `N`/`R`/`X` handlers
inherit the same protection without code changes.

The sentinel folder IDs (`__imbox__` etc.) exist for cmd-bar /
config / log identification only — they do not appear in the
`folders` table and are not passed to Graph. They are a wire-level
identifier in places that need a string handle (e.g., `folder_id`
log fields, the sidebar's selected-folder snapshot). The
`folderItem.isStream` flag is the runtime gate; the sentinel ID is
the durable handle. Both must be consistent.

**Sidebar position:** below the user's subscribed folders, in a new
section header `Streams` between regular folders and saved
searches:

```
▾ Mail
  Inbox          47
  Sent
  Archive
  …
▾ Streams
  📥 Imbox        12
  📰 Feed         84
  🧾 Paper Trail  31
  🚪 Screener      3
▾ Saved Searches
  ☆ Newsletters  247
  …
▾ Muted Threads (when present, spec 19 §5.4)
```

Glyphs are configurable via `[ui].stream_indicators` (table; see §11).
ASCII fallback uses single ASCII characters: `i`, `f`, `p`, `k`
(matching the chord sub-keys, so the visual indicator and the
chord mnemonic agree).

**Visibility:** all four virtual folders are always rendered, even
when their count is zero. This differs from spec 19's "Muted Threads"
(which hides at zero) — routing buckets are the user's primary triage
surface and need to be visible so the user can see "I haven't routed
anyone yet" rather than "where did Imbox go?".

**Selection:** Enter on a routing virtual folder loads the
corresponding `ListMessagesByRouting` result into the list pane. The
list pane top shows `[stream: Imbox]` (matching spec 11 §5.2's
`[saved: …]` convention).

**Count display:** distinct-message count from
`CountMessagesByRouting` with `excludeMuted=true`. Refreshed:
- on initial sidebar load,
- after every routing assignment (`S i`/`S f`/...),
- after every sync engine `FolderSyncedEvent` (any folder sync may
  add messages from routed senders),
- on the spec 11 background refresh tick (`saved_search.background_refresh_interval`,
  default 2m) — routing counts are refreshed alongside saved-search
  counts.

### 5.5 List-pane indicator

When the list is showing a routing virtual folder OR when a regular
folder view contains a mix of routed and unrouted messages, each
routed row carries a leading 1-character glyph in the "flags" slot
(same column as `🔕` mute / `📅` calendar / `⚑` flag from spec 19,
20):

| Destination  | Glyph (default) | ASCII fallback |
|--------------|-----------------|----------------|
| `imbox`      | `📥`            | `i`            |
| `feed`       | `📰`            | `f`            |
| `paper_trail`| `🧾`            | `p`            |
| `screener`   | `🚪`            | `k`            |

Off by default in regular folder views (clutter); always on in
routing virtual folders (redundant with the folder name, but signals
that the row's sender is routed and which destination matched). The
flag-slot priority order from spec 19 §5.2 is extended:

```
📅 (calendar, spec 12)
> 🔕 (mute, spec 19)
> ⚑  (flag, spec 07)
> 📥/📰/🧾/🚪 (routing, spec 23)
> ' ' (none)
```

In other words a calendar invite always wins; if the row is muted,
the mute glyph wins over routing; etc. Only one glyph at a time in
the slot.

Configurable via `[ui].show_routing_indicator = false` (default
false in regular folder views; routing virtual folders always show
the indicator regardless of this setting).

### 5.6 Status-bar feedback

| Action      | Toast                                              |
|-------------|----------------------------------------------------|
| `S i` succ. | `📥 routed news@example.com → Imbox`               |
| `S f` succ. | `📰 routed news@example.com → Feed`                |
| `S p` succ. | `🧾 routed news@example.com → Paper Trail`         |
| `S k` succ. | `🚪 routed news@example.com → Screener`            |
| `S c` succ. | `↩ cleared routing for news@example.com`           |
| Reassign    | `📰 routed news@example.com → Feed (was Imbox)`    |
| No-op `S i` on already-Imbox | `route: news@example.com already → Imbox` |
| No-op `S c` on unrouted | `route: news@example.com is not routed`         |
| No address  | `route: focused message has no from-address`       |
| DB error    | `route failed: database error (see logs)`          |

**Address vs name in the toast:** the toast displays the bare
`from_address` (lowercased to match the routing key — predictable
under reassign). The display name is intentionally NOT joined in:
adding a JOIN per toast costs measurable latency, and the address
is the actual routing key — showing it disambiguates senders who
share a display name. Address-form mail (`bob@vendor.invalid`) is
already public per RFC 5322 and visible in the list pane.

**Toast vs. log boundary.** Toasts are rendered to the terminal
only; the slog redaction handler does not see them. To prevent
panic-recovery accidentally routing toast text through slog,
`routedMsg` and `routeNoopMsg` types do NOT implement `String()` /
`Error()` — they are plain structs consumed only by the renderer.
Subject lines and message bodies are never included. The literal
`<error>` from the DB layer is NOT shown in the user-facing toast
(see DB-error row above): the wrapper at the dispatch boundary
substitutes the generic message and writes the underlying error
to the redacted slog stream.

### 5.7 Reassignment semantics

Pressing `S f` on a sender already routed to Imbox **updates** the
row's destination to Feed and refreshes `added_at`. The toast
includes `(was Imbox)` so the user sees the prior state — guarding
against accidental re-route in the wrong direction. The list pane
reloads after every routing change that wrote to the table.

Pressing `S c` on an unrouted sender is a no-op with the friendly
status from §5.6. Clearing is idempotent (matches
`ClearSenderRouting` semantics).

Pressing `S i` on a sender already routed to Imbox is also a no-op.

**No-op short-circuit (read-then-write protocol).** Both no-op
cases above MUST short-circuit before the SQL write to avoid
bumping `added_at` and to skip the list-pane reload (visible
flicker for no semantic change). Implementation:
`SetSenderRouting` does a `GetSenderRouting` first; if the prior
destination matches, it returns the no-op marker without touching
SQLite. Do **not** simplify to a single `INSERT … ON CONFLICT DO
UPDATE` — the read-first pattern is intentional. Document the
constraint as a code comment.

### 5.8 Routing while inside a routing virtual folder

When the list is loaded from `ListMessagesByRouting('feed', …)` and
the user presses `S i`, the focused message's sender is reassigned
to Imbox; the row vanishes from the Feed view (correct — the JOIN
no longer matches). The list reloads, the cursor stays at the same
position (or moves to the next row if the previous focused row
disappeared).

`S c` while inside any routing virtual folder removes the sender's
row from `sender_routing`. All messages from that sender vanish
from the current view (and from any other routing virtual folder).
Cursor falls to the next row; if the list is now empty, the empty-
bucket message from §8 is shown.

`S i` while viewing the Imbox virtual folder on a sender already
routed to Imbox is the no-op of §5.7 — no list reload, no toast
flicker. Same for the symmetric cases on Feed / Paper Trail /
Screener views.

### 5.9 Auto-suggest hint (deferred)

The roadmap notes that routing should benefit from heuristic
seeding ("a sender whose mail always carries an unsubscribe URL is
plausibly Feed"). v1 ships a manual flow only; an explicit
`:route suggest` command that proposes assignments based on
`unsubscribe_url` presence is a future enhancement. Mention in the
status bar tip on first launch:

```
press S then i/f/p/k on a focused message to route its sender
```

shown once on first sidebar render that includes a Stream section
with all-zero counts, dismissed via `Esc` (same one-time hint
pattern as spec 11 §5.4 auto-suggest).

## 6. Routing assignments — local-only, NOT via action queue

Routing assignments do NOT use the spec 07 `action.Executor` or the
`actions` table queue. Rationale identical to spec 19 §6 (mute):

- The action queue's `dispatch()` switch dispatches to Graph.
  Routing has no Graph call — `inferenceClassificationOverride` is
  intentionally not used (§2.2). Routing through the queue would
  require a `default` no-op branch that misleads future readers
  into thinking a Graph call is made.
- The data is per-account local state.

**Census of local-only state in the codebase.** Saved searches
(spec 11, table `saved_searches`) and muted conversations (spec 19,
table `muted_conversations`) are also local-only. Saved searches
predate the action queue concept and are managed via the
`savedsearch.Manager` interface, not via the action queue. Mute is
the first explicit local-only **mutation surface** added after the
queue existed; spec 19 §6 documents the precedent. Routing is the
second such surface. Update `docs/ARCH.md` §"action queue" so the
list of explicit local-only mutation surfaces is canonical (mute,
routing) — distinct from the broader category of local-only state
(which also includes saved searches and undo bookkeeping).

The UI dispatches directly:

```go
// dispatchList / dispatchViewer:
//
// IMPORTANT (per spec 19 §6 ctx-capture warning): tea.Cmd goroutines
// must not capture a context from the synchronous Update call —
// Update has no ambient ctx and capturing nil propagates nil-deref
// at the store boundary. The caller threads a cancellable
// context.Context (typically m.ctx, set up at app boot from the
// root context) explicitly into the Cmd factory:
func routeCmd(ctx context.Context, st store.Store, accountID int64,
    fromAddress, destination string) tea.Cmd {
    return func() tea.Msg {
        if destination == "" {  // S c (clear)
            prior, err := st.ClearSenderRouting(ctx, accountID, fromAddress)
            if err != nil {
                return routeErrMsg{err: err}
            }
            if prior == "" {
                return routeNoopMsg{address: fromAddress, kind: "unrouted"}
            }
            return routedMsg{address: fromAddress, dest: "", priorDest: prior}
        }
        prior, err := st.SetSenderRouting(ctx, accountID, fromAddress, destination)
        if err != nil {
            return routeErrMsg{err: err}
        }
        if prior == destination {
            return routeNoopMsg{address: fromAddress, kind: "already", dest: destination}
        }
        return routedMsg{address: fromAddress, dest: destination, priorDest: prior}
    }
}
```

`routeErrMsg{err}`, `routedMsg{address, dest, priorDest}`, and
`routeNoopMsg{address, kind, dest}` are new typed messages added
alongside the existing UI message types (`pollErrMsg`, etc.) in
the routing dispatch file. Each has no `String()` / `Error()`
method — the toast renderer reads the fields directly (per §5.6
toast-vs-log boundary).

**Undo model:** `S c` is the inverse of any `S <dest>`. The spec 07
`u`-key undo stack is **not** involved. Rationale matches mute
(spec 19 §6): undo-stack integration would require teaching the undo
runner to call a store method rather than a Graph call, with no
user-facing benefit since the toggle is instant and lossless.

## 7. CLI

```sh
# Assign a sender to a destination.
inkwell route assign news@example.com feed
inkwell route assign aws-billing@amazon.com paper_trail

# Clear routing for a sender (returns them to the unrouted state).
inkwell route clear news@example.com

# List all routings, optionally filtered by destination.
inkwell route list
inkwell route list --destination feed
inkwell route list --output json

# Show the routing for one sender.
inkwell route show news@example.com
inkwell route show news@example.com --output json
```

Subcommands:

| Subcommand | Text output                                                          | JSON output (`--output json`)                              |
|------------|----------------------------------------------------------------------|------------------------------------------------------------|
| `assign`   | `✓ routed <address> → <destination>` (or `(was <prior>)` on reassign) | `{"address":"…","destination":"…","prior":"…"}`           |
| `clear`    | `✓ cleared routing for <address>`                                     | `{"address":"…","cleared":true,"prior":"…"}`              |
| `list`     | One row per routing, columns `DESTINATION  ADDRESS  ADDED`            | `[{"address":"…","destination":"…","added_at":"…"}, …]`   |
| `show`     | `<address> → <destination> (added <relative time>)` or `<address> is not routed` | `{"address":"…","destination":"…"}` or `{"address":"…","destination":null}` |

Destination values accepted on the CLI: `imbox`, `feed`,
`paper_trail`, `screener`. Unknown values exit with code 2 and
`route: unknown destination "<x>"; expected one of imbox, feed,
paper_trail, screener`.

**Address normalization:** the CLI lowercases the input via the same
`NormalizeEmail` helper as the store. Addresses with a display-name
form (`"Bob Acme" <bob@acme.invalid>`) are rejected with
`route: address must be bare; got "<input>"`. Strict input is
preferable to silent extraction — the user typed it.

**Verb choice (`assign` vs `set`).** The CLI uses `assign`
(directional — sender to destination) over `set` (which is the
generic config verb in spec 13's `inkwell mailbox set …`). `assign`
matches HEY's "Assign to Imbox" UX label and reads naturally in
shell scripts: `inkwell route assign news@example.com feed`.

**Exit codes.** Unknown destination value or display-name address
exit with code **2** (usage error), per spec 14 §"exit codes" (`0`
= success, `2` = user error / bad input). The store layer's
`ErrInvalidDestination` and `ErrInvalidAddress` are translated to
exit 2 in the CLI wrapper.

**Bulk routing not in v1.** The CLI does not accept `--filter` or
`--all` flags on `route assign`; bulk re-routing of every sender
matching a pattern is deferred (see §14). Users wanting to seed
many senders run a shell loop:
`inkwell messages --filter '~f *@vendor.com' --output json | jq … | xargs -I{} inkwell route assign {} feed`.

Commands live in `cmd/inkwell/cmd_route.go`, registered in
`cmd_root.go`.

### 7.1 Cmd-bar parity

The TUI cmd-bar accepts the same verbs:

```
:route assign news@example.com feed
:route clear news@example.com
:route show news@example.com
:route list
```

Behaviour matches the CLI exactly. Output goes to the status bar
(success toast / error message). `:route list` opens a modal that
shows the routing table with the same columns as the text CLI
output; `Esc` closes. The cmd-bar parser dispatches via the same
internal handler as the chord (`routeCmd`), so the CLI, cmd-bar,
and chord paths share one validation funnel.

**Auto-suggest verb (`:route suggest`)** — deferred per §5.9. Not
a v1 verb; mention as a future surface in §14.

## 8. Edge cases

| Case                                          | Behaviour                                                            |
| --------------------------------------------- | -------------------------------------------------------------------- |
| Focused message has empty `from_address`      | Toast: `route: focused message has no from-address`. No DB write. (Defensive — every Graph message envelope has a From, but drafts and synthesised list-server messages can be missing one.) |
| `from_address` mixed-case (`Bob@Acme.io`)     | Normalised to `bob@acme.io` before insert. The JOIN's `lower(trim(...))` matches both casings; the toast displays the lowercased address. |
| Two messages from the same sender stored under different casings (`Bob@…` vs `BOB@…` from a delta resync) | Both rows match the same `sender_routing` entry via the JOIN's `lower(trim(...))` predicate. The expression index `idx_messages_from_lower` (§3) covers both. |
| `from_address` has leading or trailing whitespace (malformed Graph envelope) | Tolerated: `lower(trim(from_address))` normalises both at JOIN time and at index build. `NormalizeEmail` also trims at the routing-row write side. |
| IDN / Unicode-domain email (`user@münchen.de`) | v1 is ASCII-equivalent only — `NormalizeEmail` lowercases via Go `strings.ToLower` (handles ASCII; passes through non-ASCII unchanged). A user routing `user@münchen.de` will not match the punycode form `user@xn--mnchen-3ya.de` or vice-versa. Document as a known v1 limit; full IDNA / RFC 5895 normalization is a follow-up. |
| Sender has multiple addresses (alias, list)   | Routing is per-address. Aliases are not collapsed in v1; each address is routed independently. Future: a `[routing].aliases` table or a heuristic suggester. |
| Bulk-route via `;`-chord while filter active  | **Out of scope for v1.** `;S i` is unbound; mention as future. |
| Sender on a routing virtual folder loses all routed mail (e.g., user clears) | The bucket count drops to 0; sidebar still shows the bucket entry (per §5.4 always-visible rule). |
| `~o feed` matches in `:filter` while focused folder is Inbox | Cross-folder filter match — the filter result lists messages from any folder whose sender is routed to Feed. Same semantics as spec 21 cross-folder filter. |
| Pattern combines `~o feed & ~m Inbox`         | Local-AND, narrow to the intersection. Compiles to a single SQL query with both clauses. |
| Pattern combines `~o feed & ~B unsubscribe`   | TwoStage: server `$search` returns candidates with "unsubscribe" in body; local refinement keeps only those whose sender is in `sender_routing` with `destination='feed'`. |
| Pattern uses `! ~o feed` vs `~o none`         | Distinct semantics (§4.3): `! ~o feed` = "not in Feed" (matches unrouted + Imbox + Paper Trail + Screener); `~o none` = "no routing row at all". |
| Sign-out + sign-in to a different account     | Routing rows are FK-cascaded on account delete. Sign-out without delete leaves rows; sign-in to a new account uses a new `account_id`, so no cross-contamination. |
| Routing virtual folder empty for that destination | List pane shows `(no senders routed to <Destination> yet — press S then i/f/p/k on a focused message)`. |
| Sender routed but no messages from them (purely prospective) | `sender_routing` has a row; the `messages` table has no row matching the JOIN. Bucket count is 0 until the next sync brings a message. The CLI `inkwell route show <addr>` still reports the assignment. |
| User assigns to `screener` (v1 behaviour) | The sender's mail is visible both in the user's actual Inbox folder AND in the Screener virtual folder. v1 does NOT hide screened-out mail from default views; the Screener gating UX comes with Roadmap §1.16. |
| Migration applied on a populated DB           | New table is empty; all existing senders are unrouted. Routing virtual folders show count 0 until the user assigns. |

## 9. Performance budgets

| Surface | Budget | Benchmark |
| --- | --- | --- |
| `SetSenderRouting` / `ClearSenderRouting` | ≤1ms p95 | `BenchmarkSetSenderRouting` in `internal/store/` |
| `GetSenderRouting` (single sender lookup) | ≤1ms p95 | `BenchmarkGetSenderRouting` |
| `ListMessagesByRouting(dest, limit=100)` over 100k msgs + 500 routed senders | ≤10ms p95 | `BenchmarkListMessagesByRouting` |
| `CountMessagesByRouting(dest)` over 100k msgs + 500 routed senders | ≤5ms p95 | `BenchmarkCountMessagesByRouting` |
| Pattern compile + execute for `~o feed` over 100k msgs | ≤10ms p95 | `BenchmarkPatternRoutingOperator` in `internal/pattern/` |
| Sidebar refresh of all four bucket counts | ≤20ms p95 cumulative | `BenchmarkSidebarBucketRefresh` |

The expected query plan for `ListMessagesByRouting` is `SCAN sr
USING INDEX idx_sender_routing_account_dest` driving `SEARCH m
USING INDEX idx_messages_from_lower` (the partial expression index
from §3). Verify with `EXPLAIN QUERY PLAN` in the benchmark setup.
If the planner picks the wrong driver, force it with `INDEXED BY`
or restructure the query (last resort).

`BenchmarkPatternRoutingOperator` compiles the literal `~o feed`
source via `pattern.Compile`, asserts `Strategy == LocalOnly` and
the LocalSQL contains the EXISTS form, then runs
`SearchByPredicate` against a 100k-message store seeded with 500
routed senders. The bench separately gates the EXISTS path —
parity with `ListMessagesByRouting` is expected but not assumed.

`BenchmarkSidebarBucketRefresh` drives the batched form
**`CountMessagesByRoutingAll(ctx, accountID, excludeMuted bool)
(map[string]int, error)`** against a 100k-message store + 500
routed senders (≈125 senders per bucket). v1 ships the batched
form, not four serial calls — committing to it upfront avoids the
"add the benchmark next week" anti-pattern (CLAUDE.md §12.4). The
single `GROUP BY destination` query is the only sidebar refresh
path; the per-bucket `CountMessagesByRouting` (single destination)
remains for CLI `inkwell route show` and other one-off lookups.
Budget ≤20ms p95 over the 100k+500 fixture.

## 10. Security and privacy

- **No new external surface.** `sender_routing` is a local-only
  table; no Graph endpoints are called by routing assignments.
- **Address handling.** `email_address` is already in `messages.from_address`
  and `messages.from_name`; no new PII category. The redaction handler
  (`internal/log/redact.go`) already scrubs email addresses → `<email-N>`
  (CLAUDE.md §7 rule 3); routing-related log lines must use the same
  scrub. Specifically, log sites in `dispatchList` / `dispatchViewer`
  for routing must NOT log `from_address` directly — log the
  destination + scrubbed address marker only.
- **Toast vs. log.** The status bar toast (§5.6) shows the literal
  address. This is a UI-only path and not logged (matches spec 19 §5.5
  precedent). Error toasts (`route failed: <error>`) MUST NOT include
  raw DB error messages that could leak the address; the wrapper at
  the dispatch boundary scrubs.
- **Cross-account isolation.** `account_id` is in every PK and FK; a
  second account's routing never bleeds into the first. FK cascade on
  account delete handles cleanup.
- **Persistence across sign-out.** Routing rows persist locally if
  the user signs out without `--purge`. This is consistent with mute
  (spec 19 §10 threat-model row) and is not new threat surface. The
  privacy doc gains a row noting the table.

### 10.1 Known v1 UX limit — Screener

Routing a sender to `screener` in v1 leaves their mail visible in
the user's actual Inbox folder; only the dedicated Screener virtual
folder is added. A user who routes a sender to Screener sees that
sender's mail in **two** sidebar entries (Inbox + `__screener__`)
with no automatic hiding. The user-facing value of the v1 Screener
bucket is a **manually-curated parking lot** — "senders I've
flagged as 'review later, do not engage'" — not the new-sender
admission gate of HEY's original design. The Roadmap §1.16 follow-
up adds (a) the new-sender first-contact gate and (b) hiding
screened-out mail from the regular Inbox view; both build on the
`destination = 'screener'` rows shipped here.

The decision to ship the Screener bucket in v1 (rather than defer
the entire bucket to spec 1.16) is so that early adopters can
start curating screen-out senders with a forward-compatible data
model — when spec 1.16 ships, all existing screener-routed senders
gain the gating behaviour without re-routing.

## 11. Configuration

This spec adds the following to `[ui]`:

| Key | Default | Used in § |
| --- | --- | --- |
| `ui.show_routing_indicator` | `false` | §5.5 |
| `ui.stream_indicators.imbox` | `"📥"` | §5.4, §5.5 |
| `ui.stream_indicators.feed` | `"📰"` | §5.4, §5.5 |
| `ui.stream_indicators.paper_trail` | `"🧾"` | §5.4, §5.5 |
| `ui.stream_indicators.screener` | `"🚪"` | §5.4, §5.5 |
| `ui.stream_ascii_fallback` | `false` | §5.4 |

When `stream_ascii_fallback = true`, the four `stream_indicators.*`
values are replaced with `i` / `f` / `p` / `k` regardless of the
configured strings — for terminals that cannot render the emoji.

TOML form (inline-table style preferred for readability):

```toml
[ui]
show_routing_indicator = false
stream_ascii_fallback = false

[ui.stream_indicators]
imbox       = "📥"
feed        = "📰"
paper_trail = "🧾"
screener    = "🚪"
```

The 🚪 (door) glyph is the conventional "screened out" marker — a
mnemonic ("door to the waiting room") not borrowed from any prior
client. The `c` of `S c` is the **c**lear mnemonic; the chord
disambiguates it from the global `c` = `AddCategory` binding.

`[bindings]` gains:

| Key | Default | Used in § |
| --- | --- | --- |
| `bindings.stream_chord` | `"S"` | §5.2 |

No new `[routing]` section in v1 — there are no per-routing
preferences. A future spec for the Screener (§1.16) will own
`[screener]` if needed.

## 12. Definition of done

- [ ] Migration `011_sender_routing.sql` lands cleanly with: the
      `sender_routing` table (composite PK, CHECK on destination,
      CHECK on `length(email_address) > 0`), the
      `idx_sender_routing_account_dest` index, the partial
      expression index `idx_messages_from_lower`, and
      `UPDATE schema_meta SET value = '11'`.
- [ ] `store.Store` interface gains `SetSenderRouting`,
      `ClearSenderRouting`, `GetSenderRouting`, `ListSenderRoutings`,
      `ListMessagesByRouting`, `CountMessagesByRouting`,
      `CountMessagesByRoutingAll`. `SetSenderRouting` and
      `ClearSenderRouting` return `(prior string, err error)` per
      §4.1. Errors: `ErrInvalidDestination` for any value outside
      the four allowed strings; `ErrInvalidAddress` for empty /
      display-name forms after `NormalizeEmail`.
- [ ] `SetSenderRouting` does **read-then-write internally**: a
      `GetSenderRouting` first; if `prior == destination` it
      returns the prior unchanged with no SQL write (no `added_at`
      bump). The no-op signal flows through the returned `prior`
      value, NOT through a sentinel error. Code comment forbids
      simplifying to a single `INSERT ON CONFLICT` upsert
      (per §5.7). `routeCmd` (§6) does NOT do its own
      `GetSenderRouting` — the read-then-write protocol lives in
      one place.
- [ ] `NormalizeEmail(s string) string` exported from
      `internal/store/sender_routing.go` (lowercases via
      `strings.ToLower`, trims surrounding whitespace via
      `strings.TrimSpace`). Documents IDN limitation per §8.
- [ ] `internal/pattern/`:
  - `lexer.go::isOpLetter` accepts `'o'`.
  - `lexer.go::fieldForOp` (or equivalent) maps `'o'` to
    `FieldRouting`.
  - `ast.go` adds `FieldRouting` field-tag.
  - `parser.go` adds `parseRoutingValue`; rejects values outside
    `{imbox, feed, paper_trail, screener, none}` with the §4.4
    error message; rejects `paper-trail` (hyphen) explicitly.
  - `compile.go` / `eval_local.go` emits the `EXISTS` /
    `NOT EXISTS` SQL fragments per §4.3 (with `lower(trim(...))`).
  - `eval_filter.go` and `eval_search.go` return
    `ErrUnsupportedFilter` for `FieldRouting` predicates, forcing
    `LocalOnly` or `TwoStage` strategy.
- [ ] `KeyMap` gains `StreamChord key.Binding`; `BindingOverrides`
      gains `StreamChord string`; wired through
      `ApplyBindingOverrides` and `findDuplicateBinding`; default
      binding `S`.
- [ ] `streamChordPending bool` + `streamChordToken uint64` in
      model. `S` sets pending and starts `streamChordTimeout` Cmd.
      Any chord key, `Esc`, or expired token clears
      `streamChordPending`. `streamChordTimeoutMsg{token}` type;
      stale-token timeouts are no-ops.
- [ ] Cross-chord interaction with spec 20: pressing `T` while
      `streamChordPending` cancels stream chord (token bump);
      pressing `S` while `threadChordPending` cancels thread chord.
      Symmetric behaviour, covered by tests.
- [ ] `S i` / `S f` / `S p` / `S k` / `S c` dispatch `routeCmd`
      with the correct destination (or empty for clear). On
      `routedMsg` reload list + show status toast per §5.6. On
      `routeNoopMsg` skip the list reload (§5.7).
- [ ] Four hardcoded virtual sidebar entries (`__imbox__`,
      `__feed__`, `__paper_trail__`, `__screener__`) under a new
      `Streams` section header. Always rendered (count may be 0).
      Selecting one calls `ListMessagesByRouting`. Counts use
      `CountMessagesByRouting(excludeMuted=true)`.
- [ ] `folderItem` (or equivalent sidebar-item type) gains an
      `isStream bool` flag and a `streamDestination string`
      field, parallel to the existing `isMuted` flag for spec 19.
      `FoldersModel.Selected()` returns `(_, false)` for stream
      items, inheriting spec 18's existing N/R/X protection
      without code change to spec 18 handlers (per §5.4).
- [ ] List-pane indicator slot extended per §5.5; priority order
      `📅 > 🔕 > ⚑ > 📥/📰/🧾/🚪 > ' '`. Off by default in regular
      folder views (`show_routing_indicator=false`); always on in
      routing virtual folders.
- [ ] CLI: `cmd/inkwell/cmd_route.go` implementing `inkwell route
      assign|clear|list|show`. Bare-address validation per §7.
      Exit code 2 on bad input. Registered in `cmd_root.go`.
- [ ] Cmd-bar parity (§7.1): `:route assign|clear|show|list`
      dispatches via the same `routeCmd` handler as the chord. No
      new bubbletea component — `:route list` opens the existing
      modal-list pane.
- [ ] Command-palette rows (spec 22): `internal/ui/palette_commands.go`
      gains five static rows — `route_imbox`, `route_feed`,
      `route_paper_trail`, `route_screener`, `route_clear` — each
      carrying `Binding: "S i" / "S f" / "S p" / "S k" / "S c"` and
      a `RunFn` that calls `routeCmd` with the focused message's
      `from_address`. `Available` resolves to OK only when a
      message is focused and `from_address != ""` (mirrors spec 19's
      `mute_thread` availability gate). One additional row
      `route_show` (Title `"Show routing for sender…"`,
      `Binding: ":route show"`, NeedsArg) defers to the cmd-bar.
- [ ] User docs: `docs/user/reference.md` adds `S` chord rows to
      list-pane keybindings table; `~o` operator row in pattern
      operator table; `:route …` rows; `inkwell route` CLI table.
      `docs/user/how-to.md` adds "Set up Imbox / Feed / Paper Trail"
      recipe.
- [ ] `docs/CONFIG.md`: new `[ui].show_routing_indicator`,
      `[ui].stream_indicators.*` (inline-table form),
      `[ui].stream_ascii_fallback`, `[bindings].stream_chord` keys
      documented per §11.
- [ ] `docs/ARCH.md` §"action queue" updated to list routing as the
      second explicit local-only mutation surface (after mute);
      saved searches noted as predating-the-queue local state.
- [ ] `docs/PRD.md` §10 spec inventory adds spec 23.
- [ ] `docs/PRIVACY.md` (when it lands per spec 17): new row noting
      `sender_routing` table stored locally.
- [ ] Tests:
  - **migration**:
    - `TestMigration011AppliesCleanly` — opens a v10 DB, runs
      migration 011, asserts `schema_meta.version == '11'`,
      `sender_routing` table exists with the right columns,
      `idx_messages_from_lower` is present.
  - **store**:
    - `TestSetSenderRoutingUpsertsAndNormalizes` — mixed-case
      address is stored lowercase; whitespace trimmed.
    - `TestSetSenderRoutingNoOpReturnsErrAlreadyRouted` — same
      destination twice does NOT bump `added_at` (read-first
      protocol per §5.7).
    - `TestSetSenderRoutingReassignBumpsAddedAt` — different
      destination updates the row and `added_at`.
    - `TestSetSenderRoutingRejectsInvalidDestination` — values
      outside the four allowed strings return
      `ErrInvalidDestination`.
    - `TestSetSenderRoutingRejectsEmptyAddress` — empty / whitespace-
      only address returns `ErrInvalidAddress`.
    - `TestClearSenderRoutingNoop` — clearing an unrouted sender is
      success with no-op semantics.
    - `TestListMessagesByRoutingExcludesMuted` — muted-conversation
      messages from a routed sender are excluded when
      excludeMuted=true.
    - `TestListMessagesByRoutingNormalizesCaseAndWhitespace` —
      messages with `From: Bob@Acme.io` and `From: '  bob@acme.io  '`
      both match a routing row stored as `bob@acme.io`.
    - `TestListMessagesByRoutingUsesIndex` — `EXPLAIN QUERY PLAN`
      shows `idx_messages_from_lower` on the JOIN probe.
    - `TestCountMessagesByRouting` — count matches len(List…).
    - `TestSenderRoutingFKCascadeOnAccountDelete` — deleting an
      account drops its routing rows.
  - **pattern**:
    - `TestParseRoutingOperator` — `~o feed` parses to
      `OpRouting{dest:"feed"}`; `~o none` parses to the
      "unrouted" sentinel; `~o foo` is a parse error;
      `~o paper-trail` (hyphen form) is a parse error.
    - `TestCompileRoutingOperatorLocalOnly` — strategy is
      `LocalOnly`; LocalSQL contains the EXISTS fragment with
      `lower(trim(m.from_address))`.
    - `TestCompileRoutingOperatorTwoStage` — `~o feed & ~B "unsubscribe"`
      compiles to `TwoStage`.
    - `TestCompileRoutingOperatorRejectedByFilterAndSearch` —
      `eval_filter.go` and `eval_search.go` return
      `ErrUnsupportedFilter` for routing predicates.
    - `TestRoutingOperatorNegationVsNone` — `! ~o feed` matches
      unrouted senders AND senders routed to other destinations;
      `~o none` matches only unrouted senders. Distinct result sets.
    - `TestExecuteRoutingOperatorIntegration` — runs against a
      tmpdir SQLite store with seeded messages and routing rows;
      validates the result set.
    - **fuzz**: `~o feed`, `~o none`, `~o paper_trail` added to
      `internal/pattern/fuzz_test.go` seed corpus.
  - **UI dispatch (e2e)**:
    - `TestStreamChordSPendingState` — `S` shows the chord hint
      (`stream: i/f/p/k/c  esc cancel`).
    - `TestStreamChordEscCancels` — `Esc` clears pending.
    - `TestStreamChordTimeoutNoop` — second `S` press increments
      token; first timeout fires with old token; pending stays true.
    - `TestStreamChordSiRoutesToImbox` — `S i` calls
      `SetSenderRouting(account, addr, "imbox")` and shows the
      success toast.
    - `TestStreamChordSkRoutesToScreener` — `S k` (NOT `S s`)
      routes to Screener.
    - `TestStreamChordScClearsRouting` — `S c` clears.
    - `TestStreamChordReassignShowsPriorInToast` — `S f` on a sender
      already routed to Imbox shows `(was Imbox)`.
    - `TestStreamChordSiOnAlreadyImboxIsNoop` — toast says "already
      → Imbox", list does NOT reload.
    - `TestStreamChordNoFromAddressShowsError` — focused message with
      empty `from_address` produces the friendly error toast.
    - `TestStreamChordTPressCancelsStreamChord` — `T` while stream-
      pending cancels stream chord without starting thread chord
      (per §5.1 cross-chord protocol).
    - `TestThreadChordSPressCancelsThreadChord` — symmetric form.
    - `TestStreamChordSSPressIsCancelNotStart` — second `S` press
      while stream-pending cancels (any unrecognised second key
      clears chord-pending). Validates the "double-Shift safety"
      rationale in §5.1.
    - `TestStreamVirtualFoldersRenderInSidebar` — sidebar shows the
      four buckets under `Streams` after first load.
    - `TestStreamVirtualFoldersAlwaysVisibleAtZero` — buckets render
      with count 0 (divergence from spec 19's hide-at-zero rule).
    - `TestStreamVirtualFolderSelectLoadsByRouting` — Enter on the
      Feed bucket loads `ListMessagesByRouting('feed', …)` into the
      list.
    - `TestStreamSentinelFolderRefusesNRX` — `N` / `R` / `X` in the
      folder pane on a `__feed__` sentinel are no-ops with a
      friendly status (spec 18 binding integration).
  - **CLI**:
    - `TestRouteCLIAssignAndShow` — assign + show round-trip.
    - `TestRouteCLIListByDestination` — `--destination feed` filters.
    - `TestRouteCLIRejectsUnknownDestination` — exit 2 + helpful
      message.
    - `TestRouteCLIRejectsDisplayNameAddress` — bare-address
      validation; exit 2.
    - `TestRouteCLINormalisesCase` — `inkwell route assign Bob@Acme.IO feed`
      stores `bob@acme.io`.
  - **bench**: per §9 — `BenchmarkSetSenderRouting`,
    `BenchmarkGetSenderRouting`, `BenchmarkListMessagesByRouting`,
    `BenchmarkCountMessagesByRouting`,
    `BenchmarkPatternRoutingOperator`,
    `BenchmarkSidebarBucketRefresh`.

## 13. Cross-cutting checklist (CLAUDE.md §11)

- [ ] **Scopes:** none new (`Mail.Read`, `Mail.ReadWrite` already in
      PRD §3.1; routing is local-only and makes no Graph calls).
- [ ] **Store reads/writes:** `sender_routing` (INSERT / UPDATE /
      DELETE / SELECT); `messages` read-only via the new
      `ListMessagesByRouting` and pattern-operator JOIN. FK cascade
      on account delete handles cleanup automatically.
- [ ] **Graph endpoints:** none. (Deliberately not using
      `inferenceClassificationOverride` per §2.2.)
- [ ] **Offline:** works fully offline. Routing is local; sync does
      not overwrite it.
- [ ] **Undo:** `S c` is the inverse of any `S <dest>`. Does NOT
      push to spec 07 undo stack. `u` does not un-route.
- [ ] **User errors:** focused message has no `from_address`
      (§5.6); DB write failure surfaces as a status-bar error toast;
      invalid destination at CLI exits 2.
- [ ] **Latency budget:** §9 covers all six new surfaces. Sidebar
      bucket refresh at ≤20ms cumulative is the user-visible budget.
- [ ] **Logs:** routing log sites at DEBUG with destination and
      scrubbed address marker (`<email-N>`). Never log raw
      `from_address`. The redaction policy in
      `internal/log/redact.go` already covers email addresses; new
      log sites must opt into it (i.e., go through `slog` with the
      attributes the redact handler scrubs).
- [ ] **CLI mode:** `inkwell route assign|clear|list|show` per §7.
- [ ] **Tests:** §12 test list.
- [ ] **Spec 11 consistency:** routing virtual folders are
      hardcoded sentinel-ID entries (`__imbox__` etc.), distinct from
      `saved_searches` rows. The user cannot delete them via `dd` in
      the sidebar (the saved-search delete path is gated by sentinel
      check). Documented in §5.4.
- [ ] **Spec 17 review (security testing + CASA evidence):** routing
      adds a new local table and one new pattern operator (`~o`),
      with no new external HTTP, no token handling, no subprocess,
      no cryptographic primitive. SQL composition is parameterised
      (no dynamic table or column names). Threat-model row: routing
      data persists across sign-out (matches mute, spec 19 §10);
      cross-account isolation via `account_id` PK; FK cascade on
      account delete. Privacy doc impact: `sender_routing` table
      added to "what data inkwell stores locally".
- [ ] **Spec 18 consistency:** routing virtual folders sit in a
      separate `Streams` sidebar section; user-created folders
      (spec 18) are not affected. `N` / `R` / `X` (folder ops)
      already refuse to operate on items that
      `FoldersModel.Selected()` returns `(_, false)` for; stream
      items inherit this protection via the new `isStream` flag on
      `folderItem` (per §5.4), parallel to spec 19's `isMuted`.
      No code change to spec 18 handlers required.
- [ ] **Spec 19 consistency:** routing virtual folders default
      `excludeMuted=true` (spec 19 §5.3 default-folder-view
      contract). Muted threads from routed senders appear in the
      "Muted Threads" virtual folder, not in their routing bucket.
- [ ] **Spec 20 consistency:** routing affects the focused message's
      sender; the `T m` (move thread) chord still moves the entire
      conversation between Graph folders without touching routing.
      A muted thread in a routed bucket is hidden from the bucket
      view (per spec 19) but still routed; un-muting restores
      visibility. The two chord prefixes `T` (thread) and `S`
      (stream) do not collide.
- [ ] **Spec 21 consistency:** `:filter --all ~o feed` is a valid
      cross-folder filter — pattern operator is local, but
      `SearchByPredicate` is account-scoped and folder-agnostic by
      default, so the filter naturally spans folders. The folder
      column from spec 21 §3.4 renders correctly.
- [ ] **Spec 22 consistency:** routing surfaces in the command
      palette via five dedicated static rows in
      `palette_commands.go` (`route_imbox`, `route_feed`,
      `route_paper_trail`, `route_screener`, `route_clear`) — not
      via the `#` sigil. Rationale: routing virtual folders are
      sentinel sidebar items (`isStream` flag, §5.4); they are NOT
      members of `m.folders.raw`, which is the source the `#`
      sigil indexes (spec 22 §4.2). Surfacing routing destinations
      under `#` would require either polluting `m.folders.raw` (a
      cross-cutting change rejected here) or extending spec 22's
      sigil registry (out of scope). Static palette rows are the
      same pattern spec 22 already uses for spec 19 `mute_thread`
      and spec 20 `thread_*` rows. Each routing row's `RunFn`
      delegates to the same `routeCmd` as the `S`-chord and the
      cmd-bar, keeping one validation funnel.
      Edge case: spec 22's stale-snapshot rule (palette open while
      `FoldersLoadedMsg` arrives) applies identically to routing
      virtual folders — the user re-opens the palette to refresh.
      The new chord binding (`StreamChord`) and the existing
      `Palette` binding do not collide (different default keys, both
      go through `findDuplicateBinding`).
- [ ] **Docs consistency sweep:** `docs/CONFIG.md` updated for the
      five new keys. `docs/user/reference.md` updated for `S`
      chord, `~o` operator, `inkwell route` subcommands. `docs/user/how-to.md`
      adds the "Set up Imbox / Feed / Paper Trail" recipe. `docs/ARCH.md`
      §action-queue gains the routing local-only note. `docs/PRD.md` §10
      spec inventory adds spec 23. No `docs/user/tutorial.md` change
      (the first-30-minutes path doesn't include routing — it is a
      power-user feature).

## 14. Notes for follow-up specs

- **Screener (roadmap §1.16):** depends on the
  `sender_routing.destination = 'screener'` rows shipped here.
  Implements: (a) the new-sender admission gate ("first-contact
  senders sit in Screener until accepted"), (b) hiding screened-out
  mail from the user's actual Inbox view (currently visible in v1 of
  routing), (c) optional native-OS notification suppression for
  screened-out senders.
- **Custom actions (roadmap §2):** `set_sender_routing` is the
  obvious operation primitive. Argument: destination string. The
  template variables `{sender}` etc. (§2.4) interact naturally —
  `op = "set_sender_routing", destination = "feed"` routes the
  focused message's sender.
- **Heuristic auto-suggest (roadmap §1.27):** a future
  `:route suggest` command can propose assignments based on
  `unsubscribe_url` presence (spec 16) and `List-Id` header signals
  (§2.5). Surface as a list of `(sender, suggested_dest, signal)`
  rows the user can accept individually.
- **Bulk re-routing:** `;S i` to route all senders in the current
  filter set. Out of scope for v1; opens a confirm modal with the
  count of distinct senders.
- **Domain-level routing:** `*@vendor.invalid` matching all senders
  at a domain. Add a `is_domain INTEGER NOT NULL DEFAULT 0` column
  in a follow-up migration; the JOIN gains a CASE expression for
  domain-form rows.
