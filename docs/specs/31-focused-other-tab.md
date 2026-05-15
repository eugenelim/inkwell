# Spec 31 — Focused / Other tab

**Status:** Shipped.
**Shipped:** v0.60.0.
**Depends on:** Spec 02 (`messages.inference_class` column from
migration `001_initial.sql`; existing sync write-path), Spec 03
(`InferenceClassification` field already populated by
`internal/sync/delta.go:254` and `internal/sync/backfill.go:152`),
Spec 04 (TUI shell — list-pane header layout, mode dispatch,
`BindingOverrides` + `findDuplicateBinding`), Spec 07 (existing
triage actions; this spec calls none of them), Spec 08 (`~y focused`
/ `~y other` pattern operator already shipped — `FieldInferenceCls`
in `internal/pattern/ast.go`, `internal/pattern/eval_local.go:135`,
`internal/pattern/eval_filter.go:113`), Spec 10 (filter UX — `:filter`
hint composition under an active sub-tab), Spec 11 (saved-search
Manager precedent for sidebar refresh ticks), Spec 14 (CLI mode —
`inkwell messages` flag plumbing), Spec 19 (mute — `ExcludeMuted`
default-folder filter precedent), Spec 22 (command palette — static
rows for `:focused` / `:other` verbs), Spec 24 (split-inbox tabs —
this spec coordinates strip-render precedence with the spec-24 tab
strip; the inbox sub-strip is a SEPARATE UI surface, not a tab strip
extension).
**Blocks:** None. A future spec may add server-side reclassification
(`PATCH /me/messages/{id}` with `inferenceClassification` field) so
the user can move messages between Focused and Other; that is a
write surface and a separate spec.
**Estimated effort:** Half a day.

### 0.1 Spec inventory

Focused / Other is item 1 of Bucket 4 (Mailbox parity) in
`docs/ROADMAP.md` §0 and corresponds to backlog item §1.15. Buckets
1, 1.5, 2, and 3 are all shipped (specs 16, 17, 18, 19, 20, 21, 22,
23, 24, 25, 26, 27, 28, 29, 30); spec 31 is the first Bucket-4 spec
and takes spec slot **31**. The PRD §10 spec inventory adds a single
row for spec 31.

The roadmap text for §1.15 is short and explicit:

> Microsoft Graph already provides `inferenceClassification`
> (Focused / Other). Surface it as a tab in the list pane and
> filter accordingly. Cheap, immediate value. Richer
> auto-categorisation (Promotions, Updates, Forums) is research-
> grade — see §3 and §1.21.

The implementation budget is consistent: this spec ships **no new
schema**, **no new Graph endpoint**, **no new pattern operator**,
**no new action type**, **no new chord**, **no new keymap field**.
Every primitive it consumes is already in the codebase (§1.1).

---

## 1. Goal

The user's Inbox folder gains an opt-in **two-segment sub-strip**
above the message list, splitting the Inbox view into **Focused**
and **Other** by reading the existing `messages.inference_class`
column that Microsoft Graph populates on incoming mail. Cycling
between the two segments is a single keystroke; the strip badges
show unread counts per sub-tab; cursor / scroll state is preserved
across sub-tab switches.

The split is **read-only in v1.** The user **views** Microsoft's
classification but does not override it from inkwell — neither
per-message (`PATCH /me/messages/{id}` with `inferenceClassification`)
nor per-sender (`POST /me/inferenceClassificationOverrides`). Per-
sender intent already has a first-class home in inkwell via spec 23
routing (Imbox / Feed / Paper Trail / Screener); the spec 31
sub-strip is purely a viewing convenience that surfaces the signal
Graph already gives us.

The split is **opt-in** behind a master config key
`[inbox].split = "off" | "focused_other"`, default `"off"`. Existing
users upgrading see no behaviour change until they flip the key.

### 1.1 What does NOT change

- **No schema migration.** All sub-strip behaviour reads the existing
  `messages.inference_class` column shipped in
  `internal/store/migrations/001_initial.sql`. The most recent
  migration on disk at spec-31 design time is
  `013_bundled_senders.sql` (spec 26); spec 31 claims no migration
  slot. Re-confirm `ls internal/store/migrations/` immediately
  before merge.
- **No new Graph scope.** `inferenceClassification` is already
  fetched by the existing `Mail.Read` / `Mail.ReadBasic` $select
  list (`internal/graph/types.go:138`).
- **No new pattern operator.** `~y focused` / `~y other` already
  compile to `inference_class = ?` in
  `internal/pattern/eval_local.go:135` and to
  `inferenceClassification eq ?` in
  `internal/pattern/eval_filter.go:113`. The sub-strip's queries are
  the existing operator.
- **`inferenceClassificationOverride` (per-sender server preference)
  is NOT called.** Spec 23 §2.2 already documented why; spec 31
  inherits that decision unchanged. No write to the per-sender
  classifier; spec 23 routing is the per-sender mechanism.
- **`PATCH /me/messages/{id}` with `inferenceClassification`
  (per-message server reclassification) is NOT called.** v1 of this
  spec is read-only. A future spec may add a write surface; it is
  out of scope here. See §14.
- **No new chord prefix.** No `F`, `O`, or sub-tab chord. Cycling
  is via the existing `]` / `[` keys (precedence rules in §5.5)
  and via two new cmd-bar verbs `:focused` / `:other` (§6).
- **No new sentinel sidebar folder.** Unlike spec 23 (which adds
  four routing virtual folders) and spec 28 (which adds a
  `__screened_out__` sentinel), spec 31 keeps the **Inbox folder
  unchanged** in the sidebar — the sub-strip is a list-pane control,
  not a sidebar entry. Rationale at §5.1.
- **Spec 24 tab strip is unchanged.** The user-defined tab strip
  shipped in spec 24 (`internal/ui/tabs.go`) renders and cycles as
  today. Spec 31's inbox sub-strip is a SEPARATE UI surface with its
  own model fields (§5.4). When a spec-24 tab is active, the inbox
  sub-strip is hidden (§5.5).
- **Mute (spec 19), routing (spec 23), screener (spec 28),
  bundles (spec 26)** are orthogonal data axes. A message can
  simultaneously be `inference_class = 'focused'`, routed to Feed,
  muted, and inside a sender bundle. Sub-tab membership reads
  `inference_class` only; spec 31 does not redefine any of those
  behaviours and does not suppress muted messages from the sub-tab
  unless `ExcludeMuted` is in effect (which it is, by spec 19 §5.3
  default-folder behaviour — Inbox is a normal folder view).
- **Confirmation gates, undo, action queue** — none touched.

## 2. Prior art

### 2.1 The Microsoft model — Outlook desktop / web

Microsoft introduced **Focused Inbox** in Outlook for Windows / Mac
/ Web / iOS / Android in 2016, replacing the earlier Clutter folder.
The signal is a server-side ML score on each incoming Inbox message,
exposed through Graph as the
[`inferenceClassification`](https://learn.microsoft.com/en-us/graph/api/resources/inferenceclassification?view=graph-rest-1.0)
property with two values, `focused` and `other`. The classifier
trains per-mailbox and adapts to user feedback. Outlook clients
render the split as **two tabs above the message list**, only when
the Inbox folder is selected — every other folder shows a flat list.

Outlook supports two write surfaces that v1 of this spec does NOT
adopt:

- **Per-message reclassify** — a "Move to Focused / Other" command
  in Outlook desktop and web that issues a `PATCH /me/messages/{id}`
  with `{ "inferenceClassification": "focused" | "other" }`. The
  classifier learns from the override.
- **Per-sender override** — the
  [`POST /me/inferenceClassificationOverrides`](https://learn.microsoft.com/en-us/graph/api/inferenceclassification-post-overrides?view=graph-rest-1.0)
  endpoint pins a sender to one classification permanently. Spec 23
  §2.2 already explains why we don't use this — it's prospective-
  only, contradicts inkwell's HEY-style retroactive routing, and
  duplicates what spec 23 already provides locally.

The classifier only acts on Inbox; messages in other folders are
"by default Focused" per
[the Graph docs](https://learn.microsoft.com/en-us/graph/api/resources/manage-focused-inbox?view=graph-rest-1.0).
This is why spec 31's sub-strip is **Inbox-folder-scoped** — outside
Inbox the signal is undefined.

Outlook desktop and web tabs are mouse-driven; there is no first-
class keyboard shortcut for switching Focused / Other. (The
groovyPost forum thread on the topic stays unanswered as of 2026.)
Spec 31 ships a keyboard binding (`]` / `[`) and cmd-bar verbs that
Outlook itself does not provide — a TUI advantage.

### 2.2 The Apple model — macOS 15 / iOS 18 Categories

Apple Mail (Sequoia, 2024) ships **Categories** (Primary,
Transactions, Updates, Promotions) as on-device ML. The split is
folder-scoped to the Inbox view; user override per-sender ("Categorize
Sender") is retroactive within the local view. Categories don't
sync as IMAP labels — they are mail-app-local. The number of
categories (4) and richness of the ML are out of scope for spec 31;
the on-device user-override pattern is interesting but maps cleanly
to spec 23 routing, not to a per-message Inbox split.

### 2.3 The Gmail model — Tabs (Primary / Social / Promotions / Updates / Forums)

Gmail's tabs are a five-way ML split with per-message drag-to-move
retraining the classifier. Most-complained-about feature:
misclassification is opaque, no override per-sender pattern. Spec 31
deliberately stays at Microsoft's binary `focused / other` and does
not invent a richer split — the Graph signal is what we surface.

### 2.4 Terminal clients

- **mutt / neomutt** — no Focused/Other equivalent. Users emulate
  via `score` files or `procmail` rules at delivery time. No tab UX.
- **aerc** — no Focused/Other; saved searches are the routing
  primitive (spec 24 §2.1). Aerc users wanting "important first"
  build a saved search like `flag:Important` and bind a keystroke.
- **alot / astroid (notmuch)** — `notmuch tag +imp` plus a
  per-thread classifier hook is the closest precedent. Tag-based;
  no Inbox-only sub-tab.
- **Mutt + IMAP-IDLE + `inferenceClassification`** — hypothetically
  possible (the Microsoft IMAP gateway exposes the signal as a
  custom IMAP keyword), but no published mutt config does it. Not
  a precedent worth copying.

### 2.5 Web / desktop — third-party clients

- **Spark Smart Inbox**, **Hey Imbox / Feed / Paper Trail / Screener**,
  **Superhuman Split Inbox** — all reviewed in spec 23 §2 and spec
  24 §2.2. None map directly to Microsoft's binary
  `inferenceClassification`; all build on either ML categories or
  user-defined queries. Their lessons (per-sender override
  retroactive, query-defined > ML-defined) inform spec 23 / spec 24
  but not the read-only Microsoft-signal split spec 31 ships.

### 2.6 Design decision

Inkwell follows **Outlook** for the read path:

- **Two segments only** — `Focused` and `Other`. No third "All"
  segment; the user disables the split via `[inbox].split = "off"`
  to see the unsplit Inbox.
- **Inbox-folder-scoped** — strip renders only when the Inbox
  folder is selected. Outside Inbox the `inferenceClassification`
  signal is undefined per Microsoft, so we don't render a split that
  would only show one populated segment.
- **Read-only in v1** — surface the signal Graph gives us; do not
  override. Per-sender override is already a first-class spec 23
  feature; per-message override is a follow-up spec.
- **Strip lives in the list pane, not the sidebar** — Outlook's
  precedent is "tabs above the list", and a sidebar-folder split
  would compete with the routing virtual folders (spec 23) for the
  same conceptual real estate ("alternate views of Inbox").
- **Reuses the spec-24 cycle keys (`]` / `[`) with a precedence
  rule, not new keys.** Adding a third pair of cycle keys would
  expand the keymap surface for a feature most users either keep
  off or use only when no spec-24 tab is active. The precedence rule
  in §5.5 keeps key behaviour predictable: at most one strip is
  cycle-active per render.
- **Cmd-bar verbs `:focused` / `:other` are always available,**
  regardless of whether Inbox is currently selected — they navigate
  to Inbox and select the sub-tab. Mirrors spec 22 / spec 25's
  navigation-verb pattern.

## 3. Storage

**No schema migration.** All sub-strip queries read the existing
`messages.inference_class TEXT` column (migration 001) populated by
the sync engine on every delta and backfill (`internal/sync/delta.go`,
`internal/sync/backfill.go`).

### 3.1 Existing column semantics

`inference_class` is one of:

| Value | Meaning |
|-------|---------|
| `focused`   | Graph classified the message as Focused. |
| `other`     | Graph classified the message as Other. |
| `''` (empty) or `NULL` | The message was never classified. Tenants with Focused Inbox disabled, drafts, sent items, and pre-Outlook-2016 archived messages all carry empty/NULL. |

Sync persists whatever Graph returned, including `''` (empty
string — see `internal/store/messages.go:300`'s
`COALESCE(inference_class, '')`). The pattern operator
`~y focused` compiles to `inference_class = 'focused'`; `~y other`
to `inference_class = 'other'`. Neither matches the `''` / `NULL`
case — the empty class is **invisible to the sub-strip** in both
segments. §7 covers the empty-classification edge case.

### 3.2 Index audit

Existing `messages` indexes (verified against
`internal/store/migrations/001_initial.sql:66-71`):

| Index | Columns | Notes |
|-------|---------|-------|
| `idx_messages_folder_received` | `(folder_id, received_at DESC)` | Drives the folder-list ORDER BY path. |
| `idx_messages_conversation`    | `(conversation_id)` | Thread lookups. |
| `idx_messages_from`            | `(from_address)` | Sender pattern matches. |
| `idx_messages_received`        | `(received_at DESC)` | Cross-folder time order. |
| `idx_messages_flag`            | `(flag_status) WHERE flag_status = 'flagged'` | Partial; flagged subset. |
| `idx_messages_unread`          | `(folder_id, is_read) WHERE is_read = 0` | Partial; unread subset. |

The sub-strip's queries are served by these existing indexes:

- **List query** (`folder_id = <inbox> AND inference_class = ? ORDER
  BY received_at DESC LIMIT 100`): planner uses
  `idx_messages_folder_received` for the index-ordered scan, then
  the `inference_class` equality is an in-row filter. Selectivity
  is favourable on a 100k-message inbox: the dominant cost is the
  `LIMIT 100` early termination, not the per-row inference filter.
- **Unread-count query** (`folder_id = <inbox> AND is_read = 0 AND
  inference_class = ?`): planner picks the partial
  `idx_messages_unread` (filters to the unread subset, typically
  10–30% of the inbox), then `inference_class` is an in-row filter.

Verify with `EXPLAIN QUERY PLAN` in the §9 benchmark setup; the
expected plans are `SEARCH messages USING INDEX
idx_messages_folder_received` for the list and `SEARCH messages
USING INDEX idx_messages_unread` for the count.

Spec 31 does **not** add a new index. The benchmark (§9) gates this
claim — if `BenchmarkInboxSubTabList100k` regresses past the
100ms p95 budget on the synthesised 100k fixture, the implementer
adds `CREATE INDEX idx_messages_inference_inbox ON messages(folder_id,
inference_class, received_at DESC)` in a separate spec-31 migration
(next available slot at merge time) and re-baselines. The benchmark
is the gate; the index is the contingency. Note: the contingency
index leads on `folder_id` (not `account_id`) to match the column
order of the existing `idx_messages_folder_received` and the
single-account v1 model.

## 4. Store API

### 4.1 New helpers

Two narrow helpers on `internal/store`:

```go
// ListMessagesByInferenceClass returns messages in the given folder
// whose inference_class matches `cls` ("focused" | "other"),
// ordered by received_at DESC. Limit caps the page size; pass <=0
// for the default 100 (spec 02 page-size convention).
//
// excludeMuted applies the same anti-join shape as
// ListMessagesByRouting (spec 23 §4.2) — when true, muted threads
// are excluded; when false, all matches are returned.
//
// excludeScreenedOut applies the spec 28 default-view filter — when
// true, messages whose sender is routed to "screener" are excluded
// (the spec 28 §5.4 "hide from default views" behaviour). The
// caller passes true for the default Inbox sub-strip view so the
// sub-tab inherits the same default-view filter as the unsplit
// Inbox folder; passes false for an explicit "show me everything
// classified Focused, including screened-out senders" path (none
// in v1).
func (s *Store) ListMessagesByInferenceClass(
    ctx context.Context, accountID int64, folderID, cls string,
    limit int, excludeMuted, excludeScreenedOut bool,
) ([]Message, error)

// CountUnreadByInferenceClass returns the unread message count for
// a (folder, inference_class) pair. excludeMuted and
// excludeScreenedOut are honoured (same shape as the List variant).
// Used for the sub-strip badges.
func (s *Store) CountUnreadByInferenceClass(
    ctx context.Context, accountID int64, folderID, cls string,
    excludeMuted, excludeScreenedOut bool,
) (int, error)
```

`cls` MUST be `"focused"` or `"other"`. Any other value (including
`""`, `"both"`, or `"none"`) returns
`ErrInvalidInferenceClass` — a typed sentinel exposed alongside
`ErrInvalidDestination` (spec 23 §4.1) in `internal/store/errors.go`.
Defence in depth: callers normalise to the closed set before invoking;
the store layer rejects bad inputs at the SQL boundary so a buggy
caller can't silently produce a no-match query.

### 4.2 SQL

```sql
-- ListMessagesByInferenceClass
SELECT m.<columns>
FROM messages m
WHERE m.account_id        = :account_id
  AND m.folder_id         = :folder_id
  AND m.inference_class   = :cls
  AND (
    NOT :exclude_muted
    OR m.conversation_id IS NULL
    OR m.conversation_id  = ''
    OR NOT EXISTS (
        SELECT 1 FROM muted_conversations mc
        WHERE mc.conversation_id = m.conversation_id
          AND mc.account_id      = :account_id
    )
  )
  AND (
    NOT :exclude_screened_out
    OR NOT EXISTS (
        SELECT 1 FROM sender_routing sr
        WHERE sr.account_id    = m.account_id
          AND sr.email_address = lower(trim(m.from_address))
          AND sr.destination   = 'screener'
    )
  )
ORDER BY m.received_at DESC
LIMIT :limit
```

```sql
-- CountUnreadByInferenceClass
SELECT COUNT(*)
FROM messages m
WHERE m.account_id        = :account_id
  AND m.folder_id         = :folder_id
  AND m.inference_class   = :cls
  AND m.is_read           = 0
  AND (
    NOT :exclude_muted
    OR m.conversation_id IS NULL
    OR m.conversation_id  = ''
    OR NOT EXISTS (
        SELECT 1 FROM muted_conversations mc
        WHERE mc.conversation_id = m.conversation_id
          AND mc.account_id      = :account_id
    )
  )
  AND (
    NOT :exclude_screened_out
    OR NOT EXISTS (
        SELECT 1 FROM sender_routing sr
        WHERE sr.account_id    = m.account_id
          AND sr.email_address = lower(trim(m.from_address))
          AND sr.destination   = 'screener'
    )
  )
```

The mute anti-join shape matches spec 19 §4.2 / spec 23 §4.2
exactly. The screener anti-join matches spec 28 §5.4's default-
view filter shape. Both anti-joins are gated by the boolean
parameter so callers can opt out for diagnostic / CLI surfaces.
The planner uses `idx_messages_folder_received` for the outer
list-query probe (and `idx_messages_unread` for the partial
unread-count probe); the `inference_class` equality narrows in-row;
the two anti-join `EXISTS` sub-queries hit
`muted_conversations.PRIMARY KEY` and the
`sender_routing.PRIMARY KEY` plus the
`idx_messages_from_lower` expression index from spec 23 §3.

The caller passes `excludeScreenedOut = cfg.Screener.Enabled` —
when the screener gate is off, no rows are filtered (spec 28
§1.1's "the gate is opt-in" stance is preserved); when on, the
sub-tab inherits the same default-view filter as the unsplit
Inbox.

### 4.3 No pattern-operator changes

`~y focused` and `~y other` already exist (spec 08). The sub-strip
queries do **not** route through the pattern engine — they use the
direct store helpers above for predictability and to avoid invoking
the saved-search compile path on every cycle. The `~y` operator
remains the user-typed pattern surface (e.g., `:filter ~y focused &
~d <7d`); the sub-strip's helpers and the operator are two
independent consumers of the same column, sharing nothing but the
column name.

## 5. UI

### 5.1 Why a list-pane sub-strip, not a sidebar folder pair

Two design alternatives were considered and rejected:

1. **Two sentinel virtual folders** in the sidebar
   (`__inbox_focused__`, `__inbox_other__`), modelled on spec 23's
   routing folders. **Rejected** because:
   - It competes with the routing virtual folders (spec 23) and the
     Muted Threads sentinel (spec 19) for the same conceptual real
     estate ("alternate views of mailbox state"), confusing first-
     time users.
   - It hides the existing Inbox folder selection from the user's
     muscle memory — the user's "Inbox" sidebar entry would no
     longer be the primary surface.
   - Outlook itself models Focused/Other as sub-tabs of the Inbox
     view, not as separate folders.

2. **Spec 24-style tab strip with two seed saved searches**
   (`Focused: ~y focused & ~m Inbox`, `Other: ~y other & ~m Inbox`).
   **Rejected** because:
   - It would consume two slots in the user's tab strip
     unconditionally, conflicting with the user-defined splits spec
     24 already supports.
   - The strip would render even when the user is browsing other
     folders or saved searches, which doesn't match Microsoft's
     "Inbox-only" semantic.
   - It locks the implementation into spec 24's `tab_order` schema
     for what is a fixed pair (no reorder, no demote).

The chosen design is a **list-pane sub-strip** rendered above the
list contents, **only when the Inbox folder is selected**. It is a
pure UI control; there is no sentinel folder, no `tab_order` row,
no saved-search row.

### 5.2 Render conditions

The sub-strip renders if and only if **all** of the following hold:

| Condition | Source |
|-----------|--------|
| `cfg.Inbox.Split == "focused_other"` | New config key, §10 |
| The folders pane has the Inbox folder selected (i.e., the user clicked / Entered on the well-known Inbox folder; `Folder.WellKnownName == "inbox"`) AND no saved-search row is currently selected (spec 11 saved-search selection is its own state, distinct from folder selection). | `internal/ui/panes.go` selection state |
| `m.activeTab < 0` (no spec-24 user-defined tab is currently focused). | `internal/ui/app.go:812` |
| `m.searchActive == false` (no `:search` query active). | `internal/ui/app.go:657` |
| `m.filterAllFolders == false` (no `:filter --all` cross-folder filter active). Plain `:filter <pattern>` (folder-scoped) is **compatible** with the sub-strip and keeps it visible — see §5.7 for the AND'd-pattern dispatch. | `internal/ui/app.go:684` |

When any precondition flips off, the sub-strip is hidden and the
list pane reverts to whatever the base view dictates (regular Inbox,
spec-24 tab, search results, cross-folder filter results). Switching
folders or toggling the config key takes effect immediately on the
next render tick (no hot reload of the config key value, but mode /
selection state changes are picked up on the next Update).

A user with `[inbox].split = "focused_other"` who selects a
non-Inbox folder (Sent, Archive, custom) or a saved-search row sees
no sub-strip — the unsplit folder / saved-search list, exactly as
today.

### 5.3 Layout

The strip renders one row above the list-pane header
(`RECEIVED FROM SUBJECT` / spec 21 cross-folder header). Total
vertical cost: **1 row**, identical to spec 24's tab strip (which is
hidden in this state by precondition `m.activeTab < 0`, so the rows
do not stack).

```
 [Focused 12] [Other 47]                                   
 RECEIVED  FROM                       SUBJECT
 14:32     boss@example.invalid       Q3 numbers
 ▶ 12:11   newsletter@vendor.invalid  Weekly digest
 …
```

Per-segment grammar (verbatim spec-24 conventions, §5.1):

```
 <space> [<name> <count>?] <space>
```

- `<name>` — `Focused` or `Other`. Always two segments; no
  truncation needed (six and five characters, well under the spec-24
  default `tabs.max_name_width = 16`).
- `<count>` — unread count from `CountUnreadByInferenceClass`. Hidden
  when zero **unless** `[inbox].split_show_zero_count = true` (spec-31
  config key, §10), in which case rendered as `0`.
- Active segment: bracket pair in `theme.AccentEmphasis`, name in
  `theme.TextEmphasis`. Inactive: bracket pair `theme.TextSubtle`,
  name `theme.Text`. Identical Lipgloss styles to spec 24 §5.1.
- New-mail glyph `•` prefixes the segment **name** when its
  unread count rose since `lastInboxSubTabFocusedAt[seg]`. Same
  semantics as spec 24 §5.5.
- A `⚠` glyph in place of the count signals a per-segment count-
  query error (rare; DB closed mid-render, etc.). Spec 24 §10 /
  spec 11 §10 precedent.

The strip is single-line, non-wrapping. With two segments (six and
five characters plus padding and counts) the strip fits trivially
into any list-pane width inkwell renders at — no horizontal scroll
needed.

### 5.4 Model fields

```go
// internal/ui/app.go — add to Model struct, adjacent to the spec
// 24 tab fields at lines 805-817:
inboxSplit             InboxSplit       // typed enum, see below
activeInboxSubTab      int              // -1 = none, 0 = focused, 1 = other
inboxSubTabState       [2]listSnapshot  // per-segment cursor + slice
inboxSubTabUnread      [2]int           // [focused, other]
inboxSubTabLastFocused [2]time.Time     // for the • glyph
inboxTenantHintShown   bool             // §6.2 one-time tenant hint
```

The fields are scalar (single-account) by design. When multi-account
(roadmap §1.2) ships, every field above must become per-account
(typed `map[int64]…` keyed on the active account ID, or a
`Model.byAccount` sub-struct). The `Store.ListMessagesByInferenceClass`
/ `CountUnreadByInferenceClass` API already takes `accountID` so the
**store boundary** is forward-compatible; the model fields and
dispatch logic are NOT, and the multi-account refactor will need to
revisit this section. This is the same scaffolding cost spec 24
will incur (its `tabs` / `tabState` / `tabUnread` are also scalar);
spec 31 adopts the same posture rather than pre-paying refactor
cost on a single-account v1.

```go
// internal/ui/types.go (adjacent to ArchiveLabel from spec 30):
type InboxSplit string

const (
    InboxSplitOff           InboxSplit = "off"
    InboxSplitFocusedOther  InboxSplit = "focused_other"
)

const (
    inboxSubTabFocused = 0
    inboxSubTabOther   = 1
)
```

`activeInboxSubTab` is `-1` on cold start — even when the strip is
visible, the list pane shows the unsplit Inbox until the user cycles
in (`]`) or invokes a cmd-bar verb. From the `-1` state, the first
`]` (or `[`) press selects the segment named by
`[inbox].split_default_segment` (§10): `"focused"` → segment 0,
`"other"` → segment 1, `"none"` → no-op (cycle keys do nothing
until the user invokes `:focused` or `:other` explicitly). The
default config value is `"focused"` so the out-of-the-box behaviour
matches Outlook's "Focused first" reading order. Subsequent `]` /
`[` presses toggle between the two segments (the strip never wraps
to `-1`).

The `[2]listSnapshot` array reuses spec 24's `listSnapshot` type
(`internal/ui/tabs.go:19`) — same cursor / scroll / message-slice
shape. The array is fixed-length two; sub-tab count never grows.

`activeInboxSubTab` is **not persisted across restart** (spec 24 §7
precedent — startup defaults to `-1`). Per-session cursor / scroll
state is also not persisted.

### 5.5 Cycle keys and precedence

`]` and `[` are the existing spec-24 `NextTab` / `PrevTab` bindings
(`internal/ui/keys.go:170-171`). Spec 31 reuses them; the dispatch
selects which strip cycles based on a precedence rule:

```
1. spec-24 user-defined tab strip configured (len(m.tabs) > 0)
   → ] / [ cycle the spec-24 strip per spec 24 §5.2 (unchanged).

2. else if inbox sub-strip is rendering (§5.2 conditions)
   → ] / [ cycle the inbox sub-strip (this spec).

3. else
   → ] / [ no-op (DEBUG log, no toast — spec 24 §7 precedent).
```

Implementation: `dispatchList`'s `key.Matches(msg, m.keymap.NextTab)`
branch checks `len(m.tabs) > 0` first (spec 24 path); else falls to
the inbox sub-strip cycle path.

`Tab` / `Shift+Tab` (pane focus, spec 04) are NOT rebound. Spec 24
§2.3's overload rationale applies symmetrically — the inbox sub-strip
does not steal pane-cycling keys.

The cycle keys are **list-pane-scoped**: viewer-pane `]`/`[` retains
`NavPrevInThread` / `NavNextInThread` (spec 05); folders-pane has no
`]`/`[` binding today and does not gain one (the user navigates
folders with `j`/`k`/Enter and only sees the strip in the list pane).

In `CommandMode` and `SearchMode` the cmd-bar / search input
consumes runes directly; `]` / `[` typed there are part of the
input string and do NOT cycle. `NormalMode` + list-pane focus is
required for cycling, identical to spec 24 §5.2.

When **both** spec-24 tabs are configured AND the inbox sub-strip is
enabled AND the user has selected the Inbox folder, the user MUST
use `:focused` / `:other` to navigate the sub-tabs — the cycle keys
default to spec-24. This is documented in `docs/user/reference.md`
and called out in the inbox-split status hint (§5.7) so the user is
not surprised. We do not invent a third pair of cycle keys; the
config-flag-and-cmd-bar combination is the escape hatch.

### 5.6 List-pane reload on sub-tab change

On a sub-tab change (cycle, cmd-bar verb), the dispatch path runs:

1. Snapshot the current `ListModel` into
   `inboxSubTabState[oldActive]` if `oldActive >= 0` (cursor +
   scroll + message slice header). Snapshot is shallow; backing
   array is shared because `ListModel.SetMessages` replaces the
   slice rather than mutating it (spec 24 §5.6 precedent).
2. Set `activeInboxSubTab = newSegment`.
3. If `inboxSubTabState[newSegment].messages` is non-empty AND
   `time.Since(capturedAt) < inboxSubTabCacheTTL` (default 60s,
   reading the existing `[saved_search].cache_ttl` config value;
   spec 31 introduces no new TTL config key), restore the snapshot
   without a reload — no DB round-trip, no flicker.
4. Otherwise dispatch a fresh
   `loadInboxSubTabCmd(folderID, segment)` that calls
   `Store.ListMessagesByInferenceClass`. On completion, populate
   the ListModel and re-snapshot the active segment slot.

Each `inboxSubTabState[i].messages` is a slice header sharing the
backing array allocated by `ListMessagesByInferenceClass`. When a
segment is re-evaluated (TTL expiry or `FolderSyncedEvent` while the
segment is active), the new slice points at a fresh backing array;
the old `inboxSubTabState[i].messages` is replaced atomically inside
the Bubble Tea Update step. With at most two backing arrays of
~5000 inbox messages × ~1 KB envelope ≈ 10 MB pinned, well within
PRD §7's 200 MB RSS budget.

### 5.6.1 Two query surfaces — direct helpers vs pattern engine

The sub-strip has **two** distinct query paths into the local store:

| State | Query path | Screener filter |
|-------|-----------|-----------------|
| Sub-tab active, no `:filter` | `Store.ListMessagesByInferenceClass(folderID=Inbox, cls=focused\|other, excludeMuted=true, excludeScreenedOut=cfg.Screener.Enabled)` — direct helpers (§4.1, §4.2). Bypasses the pattern engine for predictability. | Applied per `[screener].enabled`, mirroring the unsplit Inbox folder view (spec 28 §5.4 row "default folder view"). |
| Sub-tab active, `:filter <pattern>` (folder-scoped) | Pattern engine: dispatcher synthesises `(~y focused & ~m Inbox) & (<user pattern>)` (or Other), runs through `pattern.Compile` + `Manager.Evaluate`. | **Not applied** — spec 28 §5.4 explicitly sets `ApplyScreenerFilter = false` for `:filter` pattern execution (always). The user has typed an explicit pattern and is opting in to the full result set. |

**The two paths therefore deliberately produce different row sets
when `[screener].enabled = true`.** This is consistent with spec
28 §5.4's mailbox-wide design: default folder views hide screened-
out senders; pattern-driven views never do. A user who enters
`:filter` over a sub-tab will see screened-out messages they did
not see in the bare sub-tab view; this is the same behaviour as
applying `:filter` over the unsplit Inbox folder.

The two paths **are** observationally equivalent when
`[screener].enabled = false` AND `<user pattern>` is a tautology —
the trivial-pattern equivalence test
`TestSubStripDirectAndFilterPathsAgreeOnTrivialPatternNoScreener`
covers that condition. The complementary test
`TestFilterOverSubTabBypassesScreenerWhenEnabled` covers the
divergence: load Focused via direct helper with screener on,
then load Focused via `:filter ~U | ! ~U` with screener on,
assert the filter-path row set is a superset (it includes any
screened-out focused-classified messages that the direct helper
hid).

`docs/user/how-to.md`'s sub-strip recipe documents this divergence
in one paragraph so users with the screener gate enabled aren't
surprised when `:filter` "reveals" more rows than the bare
sub-tab.

### 5.7 Status-bar hints

When the sub-strip is rendering AND `activeInboxSubTab >= 0`, the
existing list-pane status-bar segment (which today shows
`folder: Inbox · 47 unread`) appends a sub-tab marker:

```
folder: Inbox · 47 unread · sub: Focused · ] / [ to cycle
folder: Inbox · 47 unread · sub: Other · ] / [ to cycle
```

When the sub-strip is rendering but `activeInboxSubTab == -1` (cold
start, or user has just switched into Inbox without cycling yet),
the marker shows:

```
folder: Inbox · 47 unread · sub: all (press ] for Focused, [ for Other)
```

When **both** spec-24 tabs are configured AND the inbox sub-strip is
enabled, `]`/`[` cycle spec-24 (§5.5), so the inbox-sub-strip
marker substitutes `:focused / :other` for the cycle hint:

```
folder: Inbox · sub: Focused · :focused / :other to switch
```

These hints fit in the existing status-bar slot without adding a
new line. The text format passes through the existing
`m.statusBar` formatter, not a new helper.

`:filter` while a sub-tab is active scopes per spec 10 / spec 21
conventions, mirrored from spec 24 §5.3:

- `:filter <pattern>` (folder-scoped) AND's with the sub-tab
  pattern. Compiled source becomes
  `(~y focused & ~m Inbox) & (<user pattern>)` for the Focused
  segment (similarly Other). The sub-strip **stays visible** with
  the active segment highlighted (the §5.2 precondition is
  `m.filterAllFolders == false`, not `m.filterActive == false`, so
  folder-scoped filter is compatible). Status hint:
  `filter: <user pattern> · in sub: Focused · matched N · ;d delete · :unfilter`.
- `:filter --all <pattern>` (spec 21) widens cross-folder; the
  inbox sub-strip is suppressed (precondition `m.filterAllFolders
  == false` from §5.2) because `--all` no longer makes sense as an
  Inbox-only sub-tab refinement. The sub-tab snapshot is preserved
  on the model and restored on `:unfilter`.

### 5.8 Cmd-bar verbs

| Command | Effect |
|---------|--------|
| `:focused` | Navigate to Inbox (if not already), set `activeInboxSubTab = inboxSubTabFocused`, render the strip and load Focused. Errors with `focused: inbox split is off — set [inbox].split = "focused_other" first` if `cfg.Inbox.Split == "off"`. |
| `:other`   | Same as `:focused` but for the Other segment, with the matching error message. |

Both verbs work from any focused pane and any current selection
state (regular folder, saved-search row, spec-24 tab, search
results, filter results). The dispatcher resolves the transition in
this order:

1. Clear `activeTab` to `-1` (deselect any spec-24 user-defined tab).
2. Clear any active saved-search selection (the sidebar reverts
   focus to the folder list).
3. Clear any active `:search` / `:filter` state.
4. Navigate to the well-known Inbox folder (existing folder-
   selection code path used by sidebar `Enter` on the Inbox row).
5. Activate the requested sub-tab (`activeInboxSubTab = 0` or `1`)
   and dispatch `loadInboxSubTabCmd`.

Implementation: two cases in `dispatchCommand`'s switch
(`internal/ui/app.go` cmd-bar dispatcher), calling a shared
`m.activateInboxSubTab(seg int)` helper that runs steps 1–5 in
order.

These verbs are also the **escape hatch** when spec-24 tabs are
configured and consume the `]` / `[` keys (§5.5).

The verbs do NOT take an argument (e.g., `:focused on`, `:focused
off`). The config key controls visibility; the verbs only navigate.
Activating the strip with the config off would be inconsistent with
the "config drives surface" rule (`docs/CONVENTIONS.md` §9 / spec 28 §1
opt-in pattern).

### 5.9 Palette rows

The command palette (spec 22) gains **two** static rows under a new
section heading **"Inbox"**:

| Row ID | Title | Synonyms | Binding column | Available.Why |
|--------|-------|----------|----------------|---------------|
| `focused_view`   | `Show Focused`   | `["focused","focus","important"]` | `:focused` | shown when `cfg.Inbox.Split == "off"`: `inbox split is off` |
| `other_view`     | `Show Other`     | `["other","unimportant","unfocused"]` | `:other`   | same Why text when off |

The Other synonyms cover the natural antonyms a user might type;
`"clutter"` is deliberately NOT a synonym because Clutter (2014–
2017) was a separate Outlook folder feature distinct from
Focused / Other (2016–) and including it would surface this row
to users who are actually looking for the deprecated Clutter
folder.

When `cfg.Inbox.Split == "off"`, both rows are still shown but
`Available.Why` greys them out and renders the disabled hint above.
Selecting a greyed row prints the same status-bar error as the
cmd-bar verb. We do not hide the rows, because the user is
discovering the feature through the palette in the first place;
silently omitting them defeats the purpose.

### 5.10 List-row indicators

**No new list-row indicator.** Sub-tab membership is implicit in the
filter that produced the row — there is no need for a glyph, and
adding one would clutter the existing flag-slot priority order
(spec 23 §5.5). Outlook does not render a per-row Focused/Other
glyph either; the strip is the sufficient surface.

### 5.11 Folders-pane rendering

The Inbox folder entry in the sidebar is unchanged. Its unread count
(rendered today via the existing folder-list path) continues to
reflect the **total** unread count for the Inbox folder, summing
both Focused and Other. Splitting the sidebar count into two would
shift the sidebar layout for users with the strip disabled.

## 6. Sub-strip activation flow

### 6.1 First-time activation

The user sets `[inbox].split = "focused_other"` and restarts (no hot
reload — `docs/CONVENTIONS.md` §9). On the next launch:

1. Folders pane loads as usual.
2. List pane resolves the selected folder (defaulting to Inbox per
   spec 04 startup behaviour).
3. The inbox sub-strip render preconditions evaluate; because Inbox
   is selected and split is on and no spec-24 tab is active, the
   strip renders with both segments inactive (`activeInboxSubTab ==
   -1`).
4. Initial badge counts populate via two **sequential**
   `Store.CountUnreadByInferenceClass` calls (one per segment),
   issued back-to-back inside one Bubble Tea Cmd. Each call hits the
   partial `idx_messages_unread` index (folder + is_read filter)
   and is bound by §9's <100ms p95 cold budget; the total <200ms
   p95 cold latency for both segments is below the perceptual
   threshold. We do NOT use an `errgroup` fan-out for two ~10ms
   warm queries — the goroutine spawn cost outweighs the
   parallelism benefit at this scale.
5. The list pane shows the unsplit Inbox until the user cycles or
   invokes a cmd-bar verb.

### 6.2 Tenant detection — Focused Inbox disabled

A tenant admin can disable Focused Inbox at the org or per-mailbox
level via the
[`POST /me/mailboxSettings`](https://learn.microsoft.com/en-us/graph/api/resources/mailboxsettings?view=graph-rest-1.0)
or via the cmdlet
`Set-FocusedInbox -Identity <user> -State Off`. When Focused Inbox
is off, incoming Inbox messages have `inferenceClassification`
**unset** (`""` / `NULL` after sync). Both segments will therefore
show zero rows.

Spec 31 ships a one-time **detection hint** at first sub-strip
render, modelled on spec 23 §5.9's auto-suggest hint:

```
focused/other looks empty — your tenant may have Focused Inbox off
(see [inbox].split docs)
```

Detection rule: at the moment the strip first renders for the
session AND the Inbox folder has ≥ 50 messages locally AND BOTH
sub-tab unread counts are zero AND the **unsplit Inbox unread
count** (read from the cached `Folder.UnreadCount` field already
maintained by the folder-list path; no extra query) is non-zero,
emit the one-time hint. The hint is dismissable via `Esc` and never
repeats in the same session (`Model.inboxTenantHintShown` flag,
§5.4). The 50-message floor avoids false positives on a fresh
sign-in where the inbox has not yet backfilled. Reading
`Folder.UnreadCount` over running a third SQL query keeps the
detection cost at zero additional reads; the cached value can lag
the per-class counts by one sync tick at worst, and the rule's
"BOTH zero AND total non-zero" condition is robust to that lag
(in the lag window the rule is just delayed by one tick, not
falsified).

The hint does NOT auto-disable the config key — the user may still
want the strip rendered for the few classified messages (e.g., a
mixed-tenant setup) and we don't override their explicit config
choice. Mute / suggestions overriding user config is a `docs/CONVENTIONS.md`
§9 anti-pattern.

### 6.3 Reading messages, marking read, archiving

When a message in the Focused segment is opened, the existing
`mark_read` action (spec 07) fires and the unread count decrements.
The sub-tab badge updates on the next refresh tick (§5.6 cache TTL
+ explicit `tabRefreshAfterTriageMsg` hook reused from spec 24).

When the user archives a message from the Focused segment (`a`,
`e`, `:archive`, `:done`, `T a`, `T e`, `;a`, `;e` per spec 30),
the action queue moves the message out of Inbox into Archive. The
archived message no longer matches `folder_id = <inbox>` and drops
out of both segments on the next refresh. Identical semantics to
spec 24 §5.4 — emergent, not a new verb.

### 6.4 Routing assignments while a sub-tab is active

The user pressing `S i` / `S f` / `S p` / `S k` / `S c` (spec 23)
routes the focused message's sender. The action does not change the
message's `inference_class` — routing and inference are independent
axes. The list pane reloads after the routing change (spec 23
§5.7); cursor returns to the same row if the message is still in
the segment, else moves to the next row.

A user who systematically routes Other-classified senders to Feed
ends up with a curated Feed view (per spec 23) and a thinning Other
sub-tab — both behaviours flow from existing primitives without
spec 31 doing any cross-feature work.

## 7. Edge cases

| Case | Behaviour |
|------|-----------|
| `cfg.Inbox.Split == "off"` (default) | Sub-strip never renders; cmd-bar `:focused` / `:other` print the off-state error. Existing Inbox view unchanged. |
| Inbox folder not yet locally synced (cold start) | Both badges show 0; strip renders. After the first `FolderSyncedEvent`, badges update. Same shape as spec 24 §7 row "Tab pattern uses `~m <folder>` and that folder is not yet locally synced". |
| Tenant has Focused Inbox disabled | Both badges show 0 even when Inbox has messages. One-time hint per §6.2. Strip stays rendered (user's explicit config). |
| User has spec-24 tabs configured AND `[inbox].split = "focused_other"` AND a spec-24 tab is currently active | The spec-24 strip is the active surface; `]`/`[` cycle it (precedence §5.5). The inbox sub-strip is hidden because precondition `m.activeTab < 0` is false. The user can invoke `:focused` / `:other` at any time to leave the spec-24 tab and land on the Inbox sub-tab — the verb's dispatch (§5.8) clears `activeTab` to `-1`, navigates to Inbox, and activates the requested sub-tab. |
| User has spec-24 tabs configured AND `[inbox].split = "focused_other"` AND has navigated to the Inbox folder via the sidebar (no spec-24 tab active, `m.activeTab == -1`) | Both strips render (spec-24 strip with no segment highlighted per spec 24 §5.1, plus the inbox sub-strip below it). Total vertical cost: 2 rows above the list-pane header. `]`/`[` cycle the spec-24 strip per the §5.5 precedence rule (which moves the user back into a spec-24 tab and hides the inbox sub-strip on the next render). To navigate the inbox sub-strip in this state, the user invokes `:focused` / `:other`. The two-row layout is documented in `docs/user/how-to.md` as a known state for users who enable both surfaces. |
| `:filter <pattern>` (folder-scoped) active while sub-tab active | Sub-strip stays visible; the user pattern AND's with `(~y focused & ~m Inbox)` or `(~y other & ~m Inbox)`; status hint shows `· in sub: <name>` (§5.7). `:unfilter` clears the filter and the sub-tab content reloads. |
| `:filter --all` (cross-folder) active while sub-tab active | Sub-strip suppressed (precondition `m.filterAllFolders == false` from §5.2); the sub-tab snapshot is preserved on the model and restored on `:unfilter`. |
| `:search` active while sub-tab active | Sub-strip suppressed (precondition `m.searchActive == false`); restored on Esc / clear. |
| User opens a Calendar invite in Focused segment | Calendar invite renders normally (spec 12); no spec 31 interaction. Archiving the invite removes it from the segment. |
| User marks a message muted (spec 19, `M`) while in a sub-tab | The conversation enters `muted_conversations`; the sub-tab list excludes muted threads on next refresh (spec 19 §5.3 default-folder behaviour applies — `excludeMuted=true` for sub-tab queries). The cursor moves to the next row. Reversible via `M` again. |
| Sender is bundled (spec 26) and the bundle's underlying messages span both classifications | Bundling is a list-render collapsing pass that runs over the rows produced by the SQL. The Focused-segment SQL returns only `inference_class = 'focused'` rows; spec 26's bundling collapses the subset of those rows that come from designated bundle senders. The Other segment performs the symmetric collapse over `inference_class = 'other'` rows. A sender whose mail spans both classifications produces TWO bundle rows (one per segment), each summarising only that segment's subset. This is consistent with the bundle being a list-render artefact, not a logical entity, and matches the spec 26 §5 behaviour for any sub-set of rows produced by a different filter. |
| User toggles `[inbox].split` between `"off"` and `"focused_other"` | Takes effect on next launch (`docs/CONVENTIONS.md` §9: no hot reload). All sub-strip state, cmd-bar verb availability, and palette row availability flip on next launch. |
| `inference_class` column is `''` (empty string) for some messages | Neither `~y focused` nor `~y other` matches the empty class. Those messages are invisible to both sub-tabs but still appear in the unsplit Inbox view. Documented in §3.1. |
| User tries `[inbox].split = "complete"` (unknown value) | Validation rejects with `config <path>: inbox.split must be one of "off" or "focused_other"`. App refuses to start (`docs/CONVENTIONS.md` §9). |
| User tries `[inbox].split = ""` (empty) | Same — validation rejects empty explicitly to avoid silent default-fallback ambiguity (spec 30 §9.1 precedent). |
| User opens compose / draft from sub-tab | Compose mode (spec 15) takes over; sub-tab state preserved on Model; restored on compose exit. The cmd-bar (`:`) is unavailable in compose-input mode, so `:focused` / `:other` cannot be invoked from inside compose — this state is unreachable, not a defensive dispatch path. |
| Sub-tab badge query returns an error mid-render (DB closed, etc.) | Segment renders with `⚠` glyph in place of the count (§5.3). Error logged at DEBUG; no toast (matches spec 24 §10). |
| New mail arrives in the Other segment via `FolderSyncedEvent` | Segment's `inboxSubTabUnread` updates; `•` glyph appears on the Other segment if the user is currently on Focused. Cleared when user cycles to Other. |
| User has `bindings.next_tab = ""` (empty override) | Spec 04 §17 / spec 24 §10 invariant: empty strings on binding overrides are **rejected** at config-load validation; the app refuses to start with `config <path>: bindings.next_tab cannot be empty`. The user changes the value to a real key (or removes the line entirely to inherit the default `]`). Spec 31 inherits this rule unchanged — the inbox sub-strip cycle hooks off the same binding, so the validation error covers both spec-24 and spec-31 surfaces. |
| Multi-account future (out of scope today) | `Store.ListMessagesByInferenceClass` / `CountUnreadByInferenceClass` take `accountID` as the first argument (spec 02 convention) — the **store boundary** is forward-compatible. The Model fields (§5.4) are scalar and will need a per-account refactor when multi-account ships (roadmap §1.2). The same refactor cost applies to spec 24 and is tracked there; spec 31 does not pre-pay it. |
| `inkwell messages --view focused` CLI run on a tenant with Focused Inbox off | Returns the empty result set (no matches; no error). Stderr prints a one-line warning `inkwell: no messages in Focused; tenant may have Focused Inbox off` when stdout would otherwise be empty AND `--quiet` is not set. Mirrors the §6.2 hint at the CLI surface. |

## 8. CLI

### 8.1 `inkwell messages --view <focused|other>`

The existing `inkwell messages` subcommand (spec 14) gains a single
new flag:

```sh
# Show the Focused subset of the Inbox.
inkwell messages --view focused

# Show the Other subset.
inkwell messages --view other

# Combined with --filter (AND'd):
inkwell messages --view focused --filter '~d <7d'

# Combined with --output json:
inkwell messages --view other --output json
```

Behaviour:

- `--view focused` is sugar for `--filter '~y focused' --folder Inbox`,
  enforced even when the user passes a different `--folder`. If the
  user explicitly combines `--view` with a non-Inbox `--folder`, the
  CLI exits with code 2 and `messages: --view requires --folder
  Inbox (or no --folder); got "<other>"`. We refuse to silently
  re-scope the user's explicit folder choice.
- `--view <unknown>` exits with code 2 and `messages: --view must
  be one of "focused", "other"`.
- The flag does NOT introduce a new store helper at the CLI layer;
  it threads through to the existing `messages` query path with
  `~y focused` / `~y other` AND'd into the user's pattern.
- The flag is **CLI-only** (no environment variable, no profile
  default — keeping the v1 CLI surface tight per spec 14 §"flag
  parsimony").

`--view` works regardless of `[inbox].split` — the config key
controls the TUI sub-strip rendering, not the CLI surface. A user
who keeps the TUI strip off can still invoke
`inkwell messages --view focused` for one-off scripted use.

### 8.2 No new `inkwell focused` / `inkwell other` subcommand

Spec 31 explicitly does NOT introduce a top-level
`inkwell focused` subcommand. Single-flag-on-existing-command is
the smaller surface and matches the `inkwell messages --filter` /
`--folder` precedent (spec 14). A future spec adding write-side
reclassification (PATCH endpoint) may introduce
`inkwell focused move <message-id>` as a verb-form subcommand;
v1 of this spec is read-only.

## 9. Performance budgets

| Surface | Budget | How met |
|---------|--------|---------|
| Inbox sub-strip render (1 row, 2 segments) | <2ms p95 | Lipgloss layout over two short strings. Bench: `BenchmarkRenderInboxSubStrip` in `internal/ui/`. |
| Sub-tab cycle, cached state | <16ms p95 | In-memory snapshot/restore. Identical shape to spec 24 §8 row 1; the `[2]listSnapshot` array is a fixed-size struct on the Model. Bench: `BenchmarkInboxSubTabCycleCached`. |
| Sub-tab cycle, cache miss (60s TTL exceeded) | <100ms p95 | Reuses `Store.ListMessagesByInferenceClass`, which is a single indexed query served by `idx_messages_folder_received` with an in-row `inference_class` filter (§3.2). Same budget as folder switch (PRD §7). Bench: `BenchmarkInboxSubTabList100k`. |
| Sub-tab badge refresh, both segments, cold | <200ms p95 (combined) | Two sequential `CountUnreadByInferenceClass` calls back-to-back inside one Cmd; per-call budget <100ms p95 by the spec 02 `Search(q, limit=50)` budget. Bench: `BenchmarkInboxSubTabCountUnreadCold`. |
| Sub-tab badge refresh, warm (no message change since last call) | <10ms p95 | The unread-count query reads the same indexed rows; warm-cache path is bound by SQLite's page cache. Bench: `BenchmarkInboxSubTabCountUnreadWarm`. |

If `BenchmarkInboxSubTabList100k` regresses past the 100ms p95
budget, the implementer adds the optional
`idx_messages_inference_inbox` index per §3.2 in a follow-up
migration; the benchmark gates that contingency.

The fixture for the 100k benchmarks reuses
`internal/store/testfixtures.go` — synthesised messages with
`inference_class` randomly distributed 60% focused / 35% other / 5%
empty (matches Microsoft's reported population distribution within
~5% on enterprise tenants).

## 10. Configuration

Spec 31 adds a `[inbox]` section to `docs/CONFIG.md`. Placement: in
alphabetical order, between `[help]` and `[keychain]`.

| Key | Default | Range / values | Description |
|-----|---------|----------------|-------------|
| `inbox.split` | `"off"` | `"off"`, `"focused_other"` | Whether the Inbox folder renders a Focused/Other sub-strip above the message list. Default `"off"` (existing users see no change on upgrade). Spec 31 §1.1, §5.2. |
| `inbox.split_show_zero_count` | `false` | bool | When `true`, render `[Focused 0]` instead of `[Focused]` for a sub-tab with no unread. Mirrors `tabs.show_zero_count` (spec 24 §10). |
| `inbox.split_default_segment` | `"focused"` | `"focused"`, `"other"`, `"none"` | Which sub-tab is selected on first cycle from the `-1` cold-start state. `"focused"` (default) makes `]` and `[` both land on Focused first; `"other"` makes them both land on Other; `"none"` keeps `activeInboxSubTab` at `-1` until the user explicitly invokes `:focused` / `:other` (no `]`/`[` activation). |

Validation in `internal/config/validate.go`:

- `inbox.split`: reject any value other than the two literals (incl.
  empty string) with `config <path>: inbox.split must be one of
  "off" or "focused_other"`.
- `inbox.split_default_segment`: reject any value other than the
  three literals (incl. empty string) with `config <path>:
  inbox.split_default_segment must be one of "focused", "other",
  "none"`.

App refuses to start on either invalid value (`docs/CONVENTIONS.md` §9).

**No new keybinding key is added.** Spec 31 reuses
`bindings.next_tab` / `bindings.prev_tab` (spec 24); the precedence
rule in §5.5 selects which strip cycles. Adding
`bindings.next_inbox_sub_tab` would expand the keymap surface for
no semantic gain — at most one strip cycles per render, so one pair
of keys suffices.

The `[inbox]` section name is reserved for future Inbox-scoped
configuration. If a future spec adds, e.g., `[inbox].auto_categorise`
or `[inbox].pin_unread_top`, those keys join this section. The
existing `[triage]`, `[ui]`, `[tabs]`, etc., sections do not absorb
Inbox-only keys.

## 11. Logging and redaction

The sub-strip emits at most three new INFO log sites, all at the
list-pane dispatcher:

```
inbox.sub_tab.cycled  segment=focused  unread=12
inbox.sub_tab.cycled  segment=other    unread=47
inbox.sub_tab.activated_via_cmd_bar  segment=focused
```

No subject lines, sender addresses, or message bodies are logged.
The unread integer is non-PII. The `segment` field is one of two
hardcoded enum strings (`focused` / `other`).

DEBUG-level logs may include the folder ID (`folder_id=<base64
inbox id>`) for debugging, consistent with existing folder-switch
DEBUG logs. Inbox folder IDs are non-PII (they are stable Graph
folder identifiers).

A new redaction test
`TestInboxSubTabLogsContainNoSubjectOrSender` asserts that no log
emission from the sub-strip dispatcher contains substrings matching
the test fixture's subject lines or sender addresses. Lives in
`internal/ui/inbox_split_redact_test.go`. Pattern matches spec 24
§11 / spec 11 §11 redaction-test convention.

The `redact.go` `SensitiveKeys` set (`internal/log/redact.go:21-41`)
does not require expansion — `segment`, `unread`, `folder_id` are
all non-PII and the log sites do not introduce any subject /
sender / body fields.

## 12. Definition of done

- [ ] `[inbox].split` config key with default `"off"`, validated to
      `{"off", "focused_other"}`.
- [ ] `[inbox].split_show_zero_count` config key with default
      `false`.
- [ ] `[inbox].split_default_segment` config key with default
      `"focused"`, validated to `{"focused", "other", "none"}`.
- [ ] `internal/config/config.go` `InboxConfig` struct with
      `Split`, `SplitShowZeroCount`, `SplitDefaultSegment` fields.
- [ ] `internal/config/defaults.go` `Defaults()` factory sets the
      three defaults.
- [ ] `internal/config/validate.go` rejects invalid values with the
      friendly error per §10. Empty string explicitly rejected.
- [ ] `Store.ListMessagesByInferenceClass(ctx, accountID, folderID,
      cls, limit, excludeMuted, excludeScreenedOut)` implemented;
      rejects `cls` not in `{"focused", "other"}` with
      `ErrInvalidInferenceClass`. The screener anti-join SQL matches
      §4.2.
- [ ] `Store.CountUnreadByInferenceClass(ctx, accountID, folderID,
      cls, excludeMuted, excludeScreenedOut)` implemented; same
      `cls` validation; same screener anti-join.
- [ ] `ErrInvalidInferenceClass` typed sentinel exposed in
      `internal/store/errors.go`.
- [ ] `Model.inboxSplit`, `Model.activeInboxSubTab`,
      `Model.inboxSubTabState`, `Model.inboxSubTabUnread`,
      `Model.inboxSubTabLastFocused`, `Model.inboxTenantHintShown`
      fields added per §5.4.
- [ ] `InboxSplit` typed string in `internal/ui/types.go` with
      `InboxSplitOff` / `InboxSplitFocusedOther` constants.
- [ ] Render preconditions §5.2 enforced in the list-pane render
      path; the strip only paints when all five conditions hold.
- [ ] Strip layout per §5.3: 1 row, 2 segments, active /
      inactive styling matching spec 24 §5.1; `•` glyph for new-
      mail; `⚠` glyph for query error; zero-count hiding
      governed by `[inbox].split_show_zero_count`.
- [ ] Cycle precedence rule §5.5: `]` / `[` cycle spec-24 strip
      when `len(m.tabs) > 0`; else cycle the inbox sub-strip if
      rendering; else no-op.
- [ ] Cycle key behaviour when only the inbox sub-strip is active:
      `]` / `[` from `activeInboxSubTab == -1` activates
      `inboxSubTabFocused` (or whatever
      `[inbox].split_default_segment` says); subsequent presses
      toggle between segments. The two-segment strip wraps trivially.
- [ ] Per-segment state preserved across cycles (cursor / scroll /
      message slice) via `[2]listSnapshot`. Cache TTL reuses the
      `[saved_search].cache_ttl` value (no new TTL key).
- [ ] Cmd-bar verbs `:focused` and `:other` per §5.8. Both
      navigate to Inbox first, then activate the sub-tab. Off-state
      error per §5.8.
- [ ] Palette rows `focused_view` and `other_view` under a new
      "Inbox" section per §5.9; synonyms include `"focused"`,
      `"focus"`, `"important"` for Focused and `"other"`,
      `"unimportant"`, `"unfocused"` for Other. `"clutter"` is
      deliberately excluded — see §5.9 rationale.
- [ ] Status-bar hint per §5.7 appended to the existing list-pane
      status segment when the sub-strip is rendering.
- [ ] One-time tenant-detection hint per §6.2: rendered when Inbox
      has ≥ 50 messages locally AND both sub-tab unread counts are
      zero AND total Inbox unread is non-zero. Dismissed via Esc;
      never repeats in a session.
- [ ] `:filter` while a sub-tab is active AND's the user pattern
      with `~y focused & ~m Inbox` (or Other); status hint reflects
      `in sub: <name>` (§5.7).
- [ ] `:filter --all` widens cross-folder; sub-strip is suppressed;
      sub-tab state preserved and restored on `:unfilter`.
- [ ] `inkwell messages --view focused | other` CLI flag per §8.1;
      rejects unknown values with exit 2; rejects combination with
      non-Inbox `--folder` with exit 2.
- [ ] Sub-strip badge refresh hooks the existing
      `FolderSyncedEvent` (the same site that drives spec 24's
      `RefreshTabCounts`) and runs both segment counts sequentially
      inside one Bubble Tea Cmd. No errgroup fan-out — see §6.1
      rationale.
- [ ] Caller passes `excludeMuted = true` AND
      `excludeScreenedOut = cfg.Screener.Enabled` for the default
      sub-strip view, so the sub-tab inherits the same default-
      view filters as the unsplit Inbox folder. Test:
      `TestSubStripExcludesScreenedOutWhenScreenerEnabled`,
      `TestSubStripIncludesScreenedOutWhenScreenerDisabled`.
- [ ] Tests:
  - **store unit:** `TestListMessagesByInferenceClassFocused`,
    `TestListMessagesByInferenceClassOther`,
    `TestListMessagesByInferenceClassEmptyClassExcluded`
    (rows with `inference_class = ''` do not match either segment),
    `TestListMessagesByInferenceClassExcludeMuted`,
    `TestListMessagesByInferenceClassRejectsInvalidCls`,
    `TestListMessagesByInferenceClassExcludeScreenedOut`
    (with screener routing rows present, `excludeScreenedOut=true`
    drops them; `=false` keeps them),
    `TestCountUnreadByInferenceClassFocused`,
    `TestCountUnreadByInferenceClassOther`,
    `TestCountUnreadByInferenceClassRespectsMute`,
    `TestCountUnreadByInferenceClassRespectsScreener`.
  - **config unit:** `TestInboxSplitDefaultIsOff`,
    `TestInboxSplitAcceptsFocusedOther`,
    `TestInboxSplitRejectsUnknownValue`,
    `TestInboxSplitRejectsEmptyString`,
    `TestInboxSplitDefaultSegmentDefault`,
    `TestInboxSplitDefaultSegmentRejectsUnknown`.
  - **dispatch unit:** `TestColonFocusedActivatesFocusedSegment`,
    `TestColonOtherActivatesOtherSegment`,
    `TestColonFocusedFromNonInboxNavigatesToInbox`,
    `TestColonFocusedWhenSplitOffShowsError`,
    `TestColonOtherWhenSplitOffShowsError`,
    `TestNextTabPressCyclesInboxSubStripWhenNoSpec24Tabs`,
    `TestNextTabPressCyclesSpec24WhenTabsConfigured` (precedence),
    `TestPrevTabPressOnInboxSubStripFromMinusOneActivatesDefaultSegment`
    (default config `split_default_segment = "focused"` lands on
    Focused regardless of `]` or `[` from `-1`),
    `TestCycleFromMinusOneRespectsSplitDefaultSegmentOther`
    (`split_default_segment = "other"` lands on Other),
    `TestCycleFromMinusOneRespectsSplitDefaultSegmentNone`
    (`split_default_segment = "none"` makes `]` / `[` no-op
    until cmd-bar verb invoked),
    `TestNextTabPressInComposeModeDoesNotCycle`,
    `TestNextTabPressInSearchModeDoesNotCycle`,
    `TestSubStripHiddenOnNonInboxFolder`,
    `TestSubStripHiddenWhenSpec24TabActive`,
    `TestSubStripHiddenWhenFilterAllActive`.
  - **dispatch e2e (`*_e2e_test.go`):**
    `TestInboxSubStripRendersWhenSplitFocusedOther`
    (visible bracket pair, Focused and Other segments rendered),
    `TestInboxSubStripHiddenWhenSplitOff`,
    `TestInboxSubStripHiddenOnSentFolder`,
    `TestInboxSubStripCycleSelectsFocused` (active-segment
    style moves visibly to Focused),
    `TestInboxSubStripCycleSelectsOtherThenFocused`,
    `TestInboxSubStripBadgeShowsUnreadCount`,
    `TestInboxSubStripNewMailGlyphAppearsOnInactiveSegment`,
    `TestInboxSubStripStatusBarHintShowsCycleKeys`,
    `TestInboxSubStripStatusBarHintShowsCmdBarVerbsWhenSpec24Active`,
    `TestColonFocusedAndColonOtherE2ENavigates`,
    `TestSubStripPreservesCursorAcrossCycle`,
    `TestSubStripBadgeUpdatesAfterArchive`
    (archive a focused-segment message; badge decrements next refresh),
    `TestSubStripBadgeUpdatesAfterFolderSyncedEvent`,
    `TestSubStripWarningGlyphOnCountQueryError`,
    `TestPaletteShowsInboxSection` (Focused / Other rows under
    "Inbox" heading),
    `TestPaletteSynonymUnfocusedMatchesOther`
    (palette filter `unfocused` matches the Other row),
    `TestPaletteSynonymClutterDoesNotMatch`
    (palette filter `clutter` does NOT match either Inbox row;
    spec 31 §5.9 deliberately excludes the term),
    `TestSubStripDisabledHintRendersWhenSplitOff` (palette row
    `Available.Why` says "inbox split is off"),
    `TestSubStripFilterAndsWithUserPattern`
    (`:filter ~d <7d` while on Focused sub-tab; status reads
    `filter: ~d <7d · in sub: Focused · matched N`),
    `TestSubStripFilterAllSuppressesStrip` (`:filter --all`
    hides the strip; `:unfilter` restores it),
    `TestSubStripDirectAndFilterPathsAgreeOnTrivialPatternNoScreener`
    (with `[screener].enabled = false`, load Focused via direct
    helper then via `:filter ~U | ! ~U` — assert identical row
    IDs; verifies §5.6.1 equivalence in the no-screener case),
    `TestFilterOverSubTabBypassesScreenerWhenEnabled` (with
    `[screener].enabled = true`, the `:filter`-path row set is a
    superset of the direct-helper row set by exactly the screened-
    out messages — verifies §5.6.1 deliberate divergence),
    `TestTenantDetectionHintRendersWhenAllZeroAndUnreadNonZero`,
    `TestTenantDetectionHintNotRenderedOnFreshSignIn`
    (Inbox has < 50 messages → no hint),
    `TestTenantDetectionHintDismissedViaEscDoesNotRepeat`,
    `TestSubStripExcludesScreenedOutWhenScreenerEnabled`
    (with `[screener].enabled = true`, screened-out senders'
    focused-classified mail is hidden from the sub-tab — matches
    spec 28 §5.4 default-folder-view behaviour),
    `TestSubStripIncludesScreenedOutWhenScreenerDisabled`
    (with `[screener].enabled = false`, all classified mail is
    shown regardless of routing).
  - **bench:** `BenchmarkRenderInboxSubStrip`,
    `BenchmarkInboxSubTabCycleCached`,
    `BenchmarkInboxSubTabList100k`,
    `BenchmarkInboxSubTabCountUnreadCold`,
    `BenchmarkInboxSubTabCountUnreadWarm` — all gated within
    budget per §9.
  - **redaction:**
    `TestInboxSubTabLogsContainNoSubjectOrSender` (§11).
  - **CLI:** `TestMessagesViewFocused`,
    `TestMessagesViewOther`,
    `TestMessagesViewRejectsUnknownValue`
    (exit code 2),
    `TestMessagesViewWithNonInboxFolderErrors`
    (`--view focused --folder Sent` → exit 2),
    `TestMessagesViewCombinesWithFilter`
    (`--view focused --filter '~d <7d'`).
- [ ] **User docs:**
  - `docs/user/reference.md` — rows for `:focused`, `:other`,
    the `Inbox` palette section, the `[inbox].split` /
    `[inbox].split_show_zero_count` /
    `[inbox].split_default_segment` config keys, the
    `inkwell messages --view focused | other` flag. The
    existing `]` / `[` rows gain a "(also cycles inbox
    sub-strip when no spec-24 tabs configured)" note.
  - `docs/user/how-to.md` — new "Show Focused / Other in the
    Inbox" recipe explaining the opt-in flag, the cycle keys, the
    cmd-bar verbs, the precedence with spec-24 tabs, and the
    tenant-disabled-Focused-Inbox edge case.
  - `docs/user/explanation.md` — one-paragraph note on the
    "we surface Microsoft's signal but don't override it"
    design choice; references spec 23 routing as the per-sender
    mechanism.
- [ ] `docs/CONFIG.md` — new `[inbox]` section with the three
      keys per §10.
- [ ] `docs/PRD.md` §10 spec inventory adds a row for spec 31.
- [ ] `docs/ROADMAP.md` §0 Bucket 4 row 1 status updated when
      shipped; §1.15 backlog heading updated likewise.
- [ ] `README.md` status table row for spec 31 once shipped (per
      `docs/CONVENTIONS.md` §12.6).
- [ ] **`docs/plans/spec-31.md`** exists with `Status: done` at
      ship time, including the final iteration entry, measured perf
      numbers, and any noted deviations. Per `docs/CONVENTIONS.md` §13 the plan
      file is mandatory at ship time; an in-progress version may be
      maintained during the loop but the §13 obligation is the ship
      gate.
- [ ] PR checklist (`docs/CONVENTIONS.md` §11) fully ticked.

## 13. Cross-cutting checklist

- [ ] **Scopes:** none new. `inferenceClassification` is fetched
      under the existing `Mail.Read` / `Mail.ReadBasic` $select list
      (`internal/graph/types.go:138`). PRD §3.1 unchanged.
- [ ] **Store reads/writes:** two new read-only helpers
      (`ListMessagesByInferenceClass`,
      `CountUnreadByInferenceClass`); no new mutations; no new
      schema; no new index (contingent on §3.2 / §9 benchmark).
- [ ] **Graph endpoints:** none new. `inferenceClassification` is
      already a field on the existing `GET /messages`
      $select clause. The
      `inferenceClassificationOverrides` collection (`POST
      /me/inferenceClassificationOverrides`) and per-message
      reclassification (`PATCH /me/messages/{id}` with
      `inferenceClassification`) are explicitly NOT called (§1.1,
      §2.6, §14).
- [ ] **Offline:** fully functional offline against the local store
      — sub-strip rendering, cycling, badge counts, cmd-bar verbs,
      and the CLI flag all run without network. Same as routing
      virtual folders (spec 23) and saved searches (spec 11).
- [ ] **Undo:** Sub-tab activation / cycling are pure UI navigation;
      no action queue, no `u`-key undo. Consistent with spec 24
      tab-switching (no undo) and spec 23 sidebar selection (no
      undo).
- [ ] **User errors:** §7 edge-case table. `:focused` / `:other`
      with split off prints the friendly off-state error.
      `inkwell messages --view <bad>` exits 2.
      `[inbox].split = "<bad>"` is a startup error.
- [ ] **Latency budget:** §9 perf budgets. New benchmarks gate CI.
- [ ] **Logs:** three new INFO sites (§11), all carrying
      non-PII enum strings + integers. Subject / sender / body never
      logged. Redaction test `TestInboxSubTabLogsContainNoSubjectOrSender`
      added.
- [ ] **CLI:** `--view focused | other` flag on `inkwell messages`
      (§8.1). No new top-level subcommand.
- [ ] **Tests:** §12 list.
- [ ] **Spec 17 review (security testing + CASA evidence):** No new
      external HTTP surface (sub-strip is local; CLI flag composes
      existing query path). No new SQL composition (the two new
      helpers use parameterised `?` placeholders; the `cls`
      argument is validated to a closed set at the Go layer and
      rejected at the SQL boundary by the explicit check). No
      token handling, no subprocess, no cryptographic primitive,
      no new third-party data flow, no new local persisted state
      (the `messages.inference_class` column already exists). No
      new threat-model row required; no `docs/THREAT_MODEL.md`
      change; no `docs/PRIVACY.md` change. **No spec 17 §4 update
      required.**
- [ ] **Spec 02 (store) consistency:** new helpers follow
      `(ctx, accountID, …)` argument shape; SQL parameterised;
      typed sentinel errors exposed in `errors.go`.
- [ ] **Spec 04 (TUI shell) consistency:** sub-strip is a
      list-pane control; no new mode; existing `Update` /
      `View` cycle drives it. `BindingOverrides` unchanged
      (no new keymap field).
- [ ] **Spec 08 (pattern language) consistency:** `~y focused` /
      `~y other` pattern operator unchanged; sub-strip queries
      use the direct store helpers, not the pattern engine.
- [ ] **Spec 14 (CLI mode) consistency:** new flag composes with
      existing `--filter` / `--folder` / `--output`; no new
      subcommand.
- [ ] **Spec 19 (mute) consistency:** sub-strip queries pass
      `excludeMuted=true` (matches default-folder-view semantics
      per spec 19 §5.3).
- [ ] **Spec 22 (palette) consistency:** two static rows added
      under a new "Inbox" section heading; ID, title, synonyms,
      and binding column follow the spec 22 row schema.
- [ ] **Spec 23 (routing) consistency:** routing virtual folders
      and the `S` chord are orthogonal; routing changes do not
      alter `inference_class` and vice versa. The user can
      systematically route Other senders to Feed without spec 31
      doing any cross-feature work.
- [ ] **Spec 24 (split-inbox tabs) consistency:** sub-strip is a
      SEPARATE UI surface from the spec-24 tab strip; precedence
      rule §5.5 determines which strip cycles `]` / `[`; both can
      co-exist visually when the user has both surfaces enabled
      and is on the Inbox folder (rare, documented in user docs).
- [ ] **Spec 30 ("Done" alias) consistency:** archive verb (`a`,
      `e`, `:archive`, `:done`) drops messages out of the
      sub-strip on next refresh — emergent property identical to
      spec 24 §5.4. No spec 30 change.
- [ ] **Docs consistency sweep:** CONFIG.md (1 new section, 3
      keys); reference.md (4 surface rows + amendments to `]` / `[`
      rows + `--view` flag); how-to.md (one new recipe);
      explanation.md (one paragraph on the read-only design
      choice); PRD §10 (1 inventory row); ROADMAP §0 Bucket 4 +
      §1.15 backlog heading; README status table; spec
      `**Shipped:**` line; plan file. Full §12.6 ship-time
      table enumerated in §12 above.

## 14. Out of scope (deferred)

These items are explicitly NOT in v1 of spec 31. Any of them is a
follow-up spec (or a roadmap re-prioritisation), not a stretch goal
inside the spec-31 PR.

1. **Per-message reclassify** (`PATCH /me/messages/{id}` with
   `inferenceClassification`). Outlook's "Move to Focused / Other"
   verb. Requires a new action type, undo-via-inverse path, and a
   confirmation gate decision. Likely spec 31.5 or a follow-up.
2. **Per-sender override** (`POST
   /me/inferenceClassificationOverrides`). Spec 23 §2.2 already
   covers why we don't use this; if a future user wants the
   prospective-only Outlook semantic instead of inkwell's
   retroactive routing, that is a separate spec with its own
   trade-offs.
3. **Richer auto-categorisation** (Promotions / Updates / Forums).
   Roadmap §1.27 / §3 — research-grade, separate spec, not blocked
   on spec 31.
4. **Tenant Focused-Inbox state in mailbox settings** (querying
   `mailboxSettings.focusedInboxOn` to suppress the strip auto-
   matically when the tenant has it off). Out of scope; the §6.2
   one-time hint is the v1 affordance and a future spec can deepen
   the integration.
5. **Sub-strip in the calendar pane.** Calendar invites have
   `inferenceClassification` semantics in some tenants but
   inkwell's calendar pane (spec 12) is read-only and folder-
   independent; no equivalent sub-strip UX.

These deferrals are documented to prevent scope creep inside the
spec-31 PR and to give future contributors a concrete starting
point if the user demand surfaces.
