# Spec 26 — Bundle senders

**Shipped:** v0.54.0
**Status:** Shipped.
**Depends on:** Spec 02 (store + new `bundled_senders` table), Spec 04
(TUI), Spec 07 (action queue — bulk-pending path only; bundle is local-
only and does NOT route through the action queue), Spec 10 (filter +
bulk-pending interaction documented §5.7), Spec 19 (mute — interaction
documented §5.8 / §10), Spec 21 (cross-folder filter — FOLDER column
interaction documented §5.2 / §10).
**Enables:** A future `~B` / `is:bundled` pattern predicate once the
custom-actions framework lands; not blocked-on, not blocking.
**Estimated effort:** 1.5–2 days. The cursor-model split (§5.5) and
its consumer audit (`ShouldLoadMore` / `AtCacheWall` / `PageDown` /
`OldestReceivedAt` / `Selected`) is the bulk of the work; the store /
CLI / migration surface is comparatively small.

### 0.1 Spec inventory

Bundle senders is item 5 of Bucket 2 in `docs/ROADMAP.md` §0 and
takes spec slot 26 to keep the spec number aligned with the
ROADMAP bullet number (1.11). Specs 22 (command palette), 23
(routing destinations), 24 (split inbox tabs), and 25 (reply-later
/ set-aside) are all already written. Spec 26 does NOT depend on
any of them. The PRD §10 spec inventory adds a single row for
spec 26.

---

## 1. Goal

For senders who send a deluge — newsletters, recruiter spam, build
notifications, automated CI updates — collapse consecutive messages
from the same sender in the list view into a single row. Expanding
shows the bundle inline; collapsing returns to the bundle row.

The feature is **per-sender opt-in**. The user designates a sender
once; from then on, every list view in the app collapses runs of that
sender's mail. There is no auto-bundling, no ML classifier, and no
category heuristic. If the user has not designated a sender, the
sender's mail renders as flat rows exactly as today.

Bundling is a pure render-pass over the already-sorted message slice.
No Graph scope is required. No new server state. The only persisted
state is the per-account list of bundled sender addresses (so the
designation survives restart).

### 1.1 What does NOT change

- Threading / `conversation_id` grouping is untouched. A bundle row
  may contain messages belonging to many threads; expanding does not
  expand threads. Bundle and thread are orthogonal axes.
- Folder views, filter results, and search results all share the same
  bundling pass — there is no per-view toggle.
- Spec 07 action queue, undo stack, and Graph $batch path are all
  unchanged. Bundling does not insert into `actions`.
- The existing list-pane sort (received_at DESC) is unchanged. Bundles
  appear wherever a run of the designated sender occurs in date
  order; they are NOT a re-sort.
- Mute (spec 19) hides a thread on **normal folder views** by
  applying `ExcludeMuted` before the bundle pass. On filter and
  search paths (where `ExcludeMuted` is intentionally false per
  spec 19 §4.3 / §4.4), muted messages do appear inside bundles
  with their `🔕` indicator. See §5.8 for the precise contract.
- No pattern-language predicate is added. A future spec may add
  `~B` / `is:bundled` once the `bundled_senders` table has settled.

## 2. Prior art

### 2.1 Terminal clients

- **mutt / neomutt** — no first-class sender bundling. Closest
  approximations: `l ~f addr` limits the view to a sender (filter,
  not collapse); `Esc-V` collapses *threads* (not senders). Out-of-
  tree patches add `%g` group expressions; not standard.
- **aerc** — threading only (`THREAD=REFS`). No sender bundling. A
  filter view via `:filter` is transient and does not collapse.
- **alot (notmuch)** — tag-and-search; bundling is achieved by saved
  searches (`from:newsletter@acme.com` becomes a virtual folder).
  No inline collapse — the saved search *is* the bundle.

### 2.2 Web / desktop clients

- **Inbox by Gmail (2014–2019)** — the canonical bundle UX. Auto-
  grouped by category (Promos, Updates, Forums, Travel, Purchases,
  Finance) plus user-defined bundles tied to filter rules. Bundles
  collapsed into a single row showing category icon, sender excerpt
  list ("Acme, Bob, Carol"), and item count. Tap expanded inline.
  An "archive bundle" gesture archived all members at once.
  **Not consecutive-only** — Inbox grouped anywhere in the list,
  which broke date order. We do NOT copy that.
- **Gmail (today)** — Promotions/Updates *tabs* survive but bundles
  do not. The `(N)` count next to a subject is conversation-thread
  count, not a sender bundle. Distinct.
- **HEY (37signals)** — "The Feed" is a per-sender opt-in stream for
  newsletter senders, screened on first contact ("Yes / No / Feed /
  Paper Trail"). Membership is per-email-address. Presentation is a
  separate pane, not inline collapse — closest in *intent* (per-
  sender opt-in) but different UX.
- **Superhuman "Splits"** — saved-filter views with badge counts. A
  split is a tab, not a collapsible row. Per-sender, per-domain, or
  per-list-id all expressible via filter syntax.
- **Outlook desktop "Sweep"** — `Always move messages from <sender>`
  is a per-sender rule that *moves* rather than collapses. Same
  granularity (per-address) but different action.
- **Thunderbird** — `View → Sort by → Sender` then `View → Group by
  Sort` produces collapsible sender groups with `▾`/`▸` chevrons.
  Always-on view mode (not per-sender opt-in); applies to whole
  folder; reorders by sender (loses date order). Closest existing
  TUI/desktop analogue but global rather than per-sender.
- **Apple Mail / Spark / Spike / Newton** — none implement sender
  bundling. Apple Mail does threading by Subject + References only.
  Spark categorises by ML; not the same shape.

### 2.3 Design decision

inkwell follows a deliberately narrower model than any of the above:

- **Per-sender opt-in.** A sender does not bundle until the user
  designates it. No ML, no category heuristic, no auto-detection.
  Closest to HEY's screening; lighter-weight (no first-contact
  modal — designation is on demand).
- **Consecutive-only.** A run of ≥2 adjacent messages from a
  designated sender collapses. Anywhere-in-list collapse breaks
  date ordering and was the main UX complaint about Inbox bundles.
- **Membership key = lowercased `from_address`, exact-match.** No
  plus-tag normalisation (`a+x@c` and `a+y@c` are distinct keys),
  no domain match, no list-id match, no display-name match. Two
  reasons: (1) plus-tag-stripping helps for one shape of
  newsletter (per-recipient unique tags) but breaks for another
  (separate purposes routed to `+billing` vs `+ops`); we don't
  have data to pick the right default. (2) Domain matching mixes
  unrelated tenants on shared infra (`*@substack.com`,
  `*@mailchimpapp.com`). Exact-match per-address is the only
  unambiguous v1 default. Roadmap note: a follow-up spec can add
  optional plus-tag stripping behind `[ui].bundle_strip_plus_tag`
  once we see real-tenant requests for it.
- **Distinct from threading.** Threads collapse by `conversation_id`;
  bundles collapse by sender designation. A bundle row may contain
  messages from many threads; expanding shows them as ordinary list
  rows (which themselves remain individual messages — there is no
  thread collapse layered on top).
- **Pure render pass.** No SQL changes; the bundle pass walks the
  already-sorted message slice in-memory and coalesces consecutive
  runs. Expansion state is session-local (cleared on folder switch).

## 3. Schema

Migration **`013_bundled_senders.sql`**. Migrations 001–010 are
applied on disk; spec 23 (sender routing) claims **011**, spec 24
(split inbox tabs) claims **012**, neither yet on disk. Spec 26
takes the next slot, **013**. If specs 23 / 24 land in a different
order, the loser of any race renumbers — no production DB has
≥011 applied. The implementation PR for spec 26 should
`ls internal/store/migrations/` immediately before the migration
is added to confirm 013 is still free.

```sql
-- 013_bundled_senders.sql
--
-- Spec 26: local-only per-sender bundling opt-in. No Graph API call;
-- the bundled state lives entirely in this table.
-- Composite PK (account_id, address) — every other table keys on
-- (account_id, ...). The address is stored lowercased; the UI
-- normalises before INSERT/SELECT.

CREATE TABLE bundled_senders (
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    address    TEXT    NOT NULL,             -- lowercased email address
    added_at   INTEGER NOT NULL,             -- unix epoch seconds
    PRIMARY KEY (account_id, address)
);
CREATE INDEX idx_bundled_senders_account ON bundled_senders(account_id);

UPDATE schema_meta SET value = '13' WHERE key = 'version';
```

**Design rationale:**

- **Composite PK `(account_id, address)`**: matches the convention
  established by `muted_conversations` (spec 19 §3) — every per-
  account local-only metadata table keys on `(account_id, …)`. A
  single-column `address` PK would silently collapse the same
  designation across multiple signed-in accounts (a future spec).
- **Lowercased address**: RFC 5321 local-part is technically case-
  sensitive but virtually no real MTA preserves case; lowercasing
  on the way in eliminates a class of "I bundled `Bob@Acme.com`
  but new mail comes from `bob@acme.com`" misses. The TUI / CLI
  always lowercases before INSERT and before lookup.
- **No domain or list-id columns**: explicit non-goal for v1
  (§1.1). Adding them later is additive.
- **`added_at`**: surfaced by `inkwell bundle list --output json`
  for users who want to audit when a sender was designated.

## 4. Store API

### 4.1 New store methods

```go
// AddBundledSender inserts a bundled_senders row. Idempotent
// (ON CONFLICT DO NOTHING). The store ALSO lowercases the address
// before INSERT as defense-in-depth — callers (UI, CLI) lowercase
// already, but a future caller forgetting to do so would otherwise
// silently insert a mixed-case row that subsequent lowercased
// lookups would miss. The double-lowercase costs ~50ns and removes
// a class of regression.
AddBundledSender(ctx context.Context, accountID int64, address string) error

// RemoveBundledSender deletes a bundled_senders row. The store
// lowercases the address before DELETE (same rationale as Add).
// No-op if not present (idempotent per CLAUDE.md §3 invariant).
RemoveBundledSender(ctx context.Context, accountID int64, address string) error

// ListBundledSenders returns all bundled sender rows for the account,
// ordered by added_at DESC then address ASC. Used by the UI to
// build the in-memory bundle set after sign-in and after CLI mutations
// (the TUI subscribes to a change channel; see §4.3).
ListBundledSenders(ctx context.Context, accountID int64) ([]BundledSender, error)

// IsSenderBundled returns true if the (account_id, address) row
// exists. The store lowercases the address before SELECT. Hot-
// path callers (the keypress dispatch) MUST use the in-memory set
// in the UI Model instead — IsSenderBundled is for CLI / tests
// and reconciliation paths.
IsSenderBundled(ctx context.Context, accountID int64, address string) (bool, error)
```

`store.BundledSender`:

```go
type BundledSender struct {
    AccountID int64
    Address   string  // lowercased
    AddedAt   time.Time
}
```

### 4.2 No change to MessageQuery / ListMessages

Bundling is a **post-query render pass**, not a query filter. The
list pane already calls `ListMessages` with the existing
`MessageQuery` shape (spec 19 added `ExcludeMuted`); bundling does
not need a query field. The store returns the full sorted slice;
the UI groups it on the way to the screen.

Rationale: a SQL-side group-by would either (a) collapse senders
across the whole result set rather than consecutive runs (wrong
shape), or (b) require a window-function-aware GROUP BY that's
fragile and slow on 100k+ stores. The render-pass approach is
O(N) over the already-paginated visible slice (N ≈ 200 by
default — the `loadLimit` initial value).

### 4.3 CLI ↔ TUI synchronisation

When the CLI runs `inkwell bundle add <addr>` while the TUI is
running against the same DB, the TUI's in-memory bundle set goes
stale. Two options were considered:

1. **Polling** — TUI re-reads `ListBundledSenders` every N seconds.
2. **Reload on focus / refresh** — the TUI re-reads when the
   `Refresh` keybinding (`Ctrl+R`) fires or the user re-enters the
   list pane after a `:` command.

We do **(2)** for v1 — it's free (the existing refresh path already
fans out to other reload calls), and CLI-during-TUI is a corner
case for one-shot scripts, not steady-state usage. Document the
caveat: "CLI bundle changes apply on the next refresh
(`Ctrl+R`) inside the TUI." Future work can add a SQLite
`update_hook` watcher if real users hit it.

## 5. UI

### 5.1 Toggle keys

| Key (list pane focus, flat message row) | Action |
| ---------------------------------------- | ------ |
| `B` | Toggle bundle designation on the focused message's sender. |

| Key (list pane focus, **bundle header** row, collapsed or expanded) | Action |
| ------------------------------------------------------------------- | ------ |
| `Space` (new `BundleExpand` binding) | Toggle expand/collapse on the focused bundle. Cursor stays on the header. |
| `Enter` (`Open` binding) | If collapsed: expand and leave cursor on the bundle header (the user's second Enter resolves to the header → opens the representative). If already expanded: open the representative (newest member) in the viewer (the second Enter case). |
| `B` | **Un-designate** the bundle's sender — `bundled_senders` row deleted, bundle dissolves into flat rows in place. Same key as designation; toggle. No confirmation (mirrors mute spec 19's no-confirmation toggle — bundling is reversible at zero data-loss cost; presses on it accidentally are safe). |

| Key (list pane focus, **bundle member** row, only when bundle is expanded) | Action |
| -------------------------------------------------------------------------- | ------ |
| `Space` (`BundleExpand`) | **Collapse** the parent bundle. Cursor lands on the (now-collapsed) bundle header row. |
| `Enter` (`Open`) | Open this member in the viewer (existing flat-row behaviour). |
| `B` | Un-designate the member's sender (same as on bundle header — the member's `from_address` lowercased equals the bundle's address; both paths converge). |

`B` (Shift+b) is chosen because:
- It is unused in `DefaultKeyMap` (`internal/ui/keys.go:145–198`).
  Audit method: scan `DefaultKeyMap` for any binding whose `Keys()`
  contains `"B"`; none does.
- `b` (lowercase) is reserved for future use; keeping it free
  preserves a per-message verb slot.
- `B` (capital) follows the convention "this affects more than the
  single message" — same as `D` (permanent-delete), `M` (mute),
  `T` (thread chord), `X` (delete folder), `U` (unsubscribe).

**Why a new `BundleExpand` key, not `Expand`?** The existing
`Expand` binding (`internal/ui/keys.go:168`) binds **both** `o` and
Space. Reusing `Expand` pane-scoped would mean lowercase `o` also
toggles bundles in the list pane — surprising, and `o` is reserved
for the spec 05 / spec 16 lowercase webLink-open in the viewer
pane (and is thus a "viewer-scoped open" mnemonic globally). To
avoid the overload we add a dedicated `BundleExpand` key with only
`" "` (Space) bound by default. The list pane's Space is therefore
list-pane-only; the folders pane keeps its existing `Expand`
(`o` and Space) untouched.

**Pane scope:**
- `B` is active only in the **list pane**. It is NOT active in the
  folders sidebar or in the viewer pane. (The viewer shows one
  message at a time; bundling has no visible effect there until
  the user returns to the list.)
- `BundleExpand` (Space) is active only in the **list pane** when
  the focused row is a bundle header. On a flat row in the list
  pane, Space is a no-op (does NOT fall through to `Expand` —
  Space-as-folders-Expand only fires when the folders pane is
  focused, by virtue of pane dispatch order).

**KeyMap changes (`internal/ui/keys.go`):**
- Add `BundleToggle key.Binding` and `BundleExpand key.Binding` to
  `KeyMap` struct.
- Add `BundleToggle string` and `BundleExpand string` to
  `BindingOverrides` struct.
- Wire both through `ApplyBindingOverrides`.
- Add `BundleToggle` to the `findDuplicateBinding` scan list (it
  must not collide with anything else).
- **Do NOT add `BundleExpand` to the `findDuplicateBinding` scan**
  — it shares the Space key with `Expand` *intentionally*
  (pane-scoped, like the existing exclusion of `MarkRead` /
  `ToggleFlag` from the scan, see `keys.go:312–313`). Add a
  comment in the scan checks list documenting the exclusion. The
  `Expand` binding is also not in the scan today; that exclusion
  remains.
- Defaults: `BundleToggle: key.NewBinding(key.WithKeys("B"))`,
  `BundleExpand: key.NewBinding(key.WithKeys(" "))`.

The `Expand` binding (`o` / Space) is unchanged — it remains the
folders-pane disclosure key. There is no spec change to spec 18.

### 5.2 Bundle row rendering

A bundle row replaces consecutive same-sender rows whose count is
≥ `[ui].bundle_min_count` (default 2). The row format mirrors the
flat-row layout but uses a disclosure glyph in place of the
combined cursor / flag / invite indicator prefix.

The current ListModel.View row format (`internal/ui/panes.go`
around line 876–878) is roughly:

```
// without FOLDER column:
"%s%s%s%-10s %-14s %s%s"   cursor, flag, invite, received, from, subject, suffix

// with FOLDER column (spec 21, when folderNameByID != nil):
"%s%s%s%-10s %-12s %-12s %s%s"  cursor, flag, invite, received, from, folder, subject, suffix
```

For a bundle header row:
- The **cursor slot** carries the standard `▶ ` (focused) / `▸ ` /
  `· ` (unfocused) marker per spec 04.
- The **flag** and **invite** slots are replaced with a single
  disclosure glyph: `▸ ` (collapsed) or `▾ ` (expanded), occupying
  whichever of the two slots renders left in the existing layout
  (the implementer picks the leftmost so the glyph reads first).
  The other indicator slot is rendered as one space, preserving
  column alignment.
- The **received** slot shows the **newest member's** received time
  (matches the sort order — bundles appear at the position of
  their newest member).
- The **from** slot shows the bundled address (truncated to 14 / 12
  chars by the existing `truncate(from, n)` helper).
- The **subject** slot for a *collapsed* bundle is
  `(N) — <latest subject>` where N is the member count and
  `<latest subject>` is the subject of the newest member.
  Example: `(12) — Your weekly digest`.
- An *expanded* bundle renders the bundle-header row (with `▾`)
  followed by individual member rows in normal flat format,
  indented by two spaces in the SUBJECT column to make membership
  visible:
  ```
  ▾ newsletter@acme.com  (12) — Your weekly digest
      Mon 14:32   Acme            Your weekly digest
      Mon 14:30   Acme            Re: last week
      ...
  ```

**FOLDER column on bundle headers** (when `folderNameByID != nil`,
spec 21 active):
- Single-folder bundle: FOLDER slot shows the (truncated)
  display name of the only folder represented.
- Multi-folder bundle: FOLDER slot shows `<folder> +N` where N is
  the count of *other* distinct folders in the bundle.
- The 12-char column width is fixed (spec 21 §3.4). When
  `len("<folder> +N") > 12`, **truncate the folder name** and keep
  the `+N` suffix verbatim. Helper:
  ```go
  // truncateBundleFolder formats <folder> +N into width chars.
  // The +N suffix is preserved; the folder name is truncated and
  // suffixed with "…" if it had to be cut. Width is the column
  // budget (always 12 in current layouts).
  func truncateBundleFolder(folder string, others, width int) string {
      if others == 0 { return truncate(folder, width) }
      suffix := fmt.Sprintf(" +%d", others)
      // Reserve room for the suffix; one rune for the ellipsis if cut.
      head := width - len(suffix)
      if head < 1 { head = 1 }
      return truncate(folder, head) + suffix
  }
  ```
  `truncate` uses the same rune-aware logic as the existing
  `internal/ui/panes.go` truncation helper (the implementer should
  reuse it, not introduce a parallel function). Member rows (when
  expanded) restore per-row folder display via the existing
  `folderNameByID[msg.FolderID]` lookup.

**Indicator-glyph slot precedence on bundle headers**: the
disclosure glyph (`▸` / `▾`) replaces calendar / mute / flag
indicators on the bundle row itself. Member rows (when expanded)
render their individual indicators normally — including `🔕` mute
glyphs for muted members included in filter / search bundles
(§5.8).

**Column-width invariant**: the bundle header row consumes
**exactly the same total column width** as a flat row. The
existing flat-row prefix is `cursor (2) + flag (2) + invite (2) =
6 cols`. Bundle header replaces flag + invite with `disclosure
(2) + space (2) = 4 cols` and keeps cursor (2) → 6 cols total.
Glyph overrides via `[ui].bundle_indicator_collapsed` /
`bundle_indicator_expanded` are **clamped to display-width 2
cells** (not 2 bytes, not 2 runes — a CJK glyph is 2 cells in
one rune). The implementer reuses the same display-width helper
that `[ui].mute_indicator` validates against (per spec 19 §5.2).
A test (`TestBundleHeaderColumnWidthMatchesFlatRow`, §9 DoD)
asserts the total column width is identical.

### 5.3 Bundle minimum size

`[ui].bundle_min_count` (default 2; range 0–9999). A run of N
consecutive same-sender messages where N < bundle_min_count is
rendered as flat rows. This avoids the degenerate "1-message
bundle" that consumes a screen line for no compaction.

The minimum is configurable so users on tiny terminals can set 5+
for less aggressive collapse, or 2 (default) for the most
aggressive. Setting `bundle_min_count = 0` is the off-switch:
no run is ever bundled regardless of the `bundled_senders` list.
This lets users keep their designations while temporarily
disabling rendering (e.g. when mirroring the buffer to a screen
reader or another tool that doesn't render the disclosure glyph).
The cleaner per-sender toggle is to remove the sender via `B` or
`inkwell bundle remove`.

### 5.4 Status-bar feedback

| Action | Toast |
| ------ | ----- |
| Designate sender, bundle visible in current view | `▸ bundled newsletter@acme.com — collapses 12 messages` |
| Designate sender, no run ≥ `bundle_min_count` in current view | `▸ bundled newsletter@acme.com — no consecutive run in current view; will collapse on next match` |
| Un-designate sender | `flat newsletter@acme.com (was bundled)` |
| Toggle expand / collapse | (no toast — visual change is immediate) |
| Empty address (focused message has no `from_address`) | `bundle: message has no sender` |
| DB error | `bundle failed: <error>` |

The "no consecutive run" branch matters because `B` is most useful
*proactively* — designating a sender after seeing one of their
messages, before the next deluge arrives. The toast confirms the
designation succeeded even when the visual change is delayed
until the sender's next batch.

The toast for designate uses the sender address only (no subject
line). Per ARCH §12 / CLAUDE.md §7 rule 3, addresses are redacted
in logs; the toast is terminal UI only and is not logged. Subject
lines never appear in either toast.

### 5.5 Cursor model

Bundling changes the visible-row count. The existing `cursor int`
on `ListModel` (`internal/ui/panes.go:581–617`) historically indexes
`m.messages` directly. After this spec, **`m.cursor` indexes the
rendered slice (`m.rendered`), and a separate resolver maps each
rendered row to its backing message(s)**.

```go
// renderedRow describes one visible row in the list pane.
// Either Message (flat row or bundle representative) or BundleIDs
// (bundle header row, listing all member message IDs in date-DESC
// order) is non-zero, never both empty.
type renderedRow struct {
    IsBundleHeader bool
    Message        store.Message   // flat row OR newest member of a bundle
    BundleAddress  string          // lowercased address, for bundle headers
    BundleIDs      []string        // member IDs in date-DESC order, len ≥ 2
}

// rowAt returns the row at rendered index i.
func (m *ListModel) rowAt(i int) renderedRow

// SelectedMessage returns the message that single-message verbs
// should target, plus an "ok" boolean (false when the list is
// empty or the cursor is out of bounds — preserves the existing
// Selected() (store.Message, bool) contract). For a bundle header
// (collapsed or expanded header row), returns the newest member
// (BundleIDs[0]). For a flat row or a bundle-member row, returns
// the message itself.
func (m *ListModel) SelectedMessage() (store.Message, bool)

// messageIndexAt returns the index in the underlying m.messages
// slice of the message backing rendered row i. For a bundle
// header, returns the index of the newest member (BundleIDs[0]).
// Used by ShouldLoadMore and other consumers that reason about
// position within the cached message slice rather than rendered
// rows.
func (m *ListModel) messageIndexAt(i int) int
```

`SelectedMessage()` replaces `Selected()` at every call site.
Callers MUST preserve the `(_, ok)` shape; the spec's `Selected()
→ SelectedMessage()` rename is signature-compatible. An empty
list / out-of-bounds cursor returns `(zero-Message, false)` and
the caller short-circuits exactly as today.

`messageIndexAt` and `rowAt` share the cached parallel slices
inside `bundleCache` (§8.1): `bundleCache.rendered[i]` returns the
row, `bundleCache.messageIndex[i]` returns the underlying index.
The cache is populated in a single O(N) pass; both lookups are
O(1) thereafter.

Every consumer of `m.cursor` and `m.messages` must be audited. The
table below records each call site and its required translation;
the implementation MUST update each one.

| Call site (today) | Today's semantic | After spec 26 |
| ----------------- | ---------------- | ------------- |
| `Selected() store.Message` (panes.go:807) | `m.messages[m.cursor]` | `SelectedMessage()` returns the resolved single-message representative. |
| `Up()/Down()` (panes.go) | `cursor ± 1` over messages | `cursor ± 1` over **rendered rows**. Wrapping/clamping unchanged. |
| `PageUp()/PageDown()` | `cursor ± listPageStep (=20)` | Same — but stepped over rendered rows. The scroll feel is now in render-row units; this is intentional. |
| `JumpTop()/JumpBottom()` | `cursor = 0 / len-1` | Over rendered rows. |
| `ShouldLoadMore() bool` (panes.go:682) | `m.cursor >= len(m.messages) - loadMoreThreshold` | `messageIndexAt(cursor) >= len(m.messages) - loadMoreThreshold`. The threshold is over the underlying message slice (the load-more trigger is about what's in the *cache*, not how many rendered rows are left). `messageIndexAt(i)` returns the underlying message index of the newest member at rendered row i (for a bundle, the newest member's index). |
| `AtCacheWall() bool` | derived from `len(m.messages)` and Graph state | Unchanged — operates on the underlying message slice, not rendered rows. |
| `OldestReceivedAt() time.Time` | min over `m.messages` | Unchanged — operates on the underlying message slice. |
| Wall-sync trigger | `cursor` near tail of `m.messages` | Use `messageIndexAt(cursor)` — rendered-row tail does NOT necessarily mean message-slice tail when bundles are dense at the bottom. |
| `SetMessages(ms)` | replaces `m.messages` | replaces `m.messages` AND recomputes `m.rendered` via the bundle pass; preserves cursor on the same MESSAGE ID where possible (not the rendered position). |
| `MoveCursorToMessage(id)` (helper) | scan `m.messages` for id | scan `m.rendered` for the row whose `Message.ID == id` OR whose `BundleIDs` contain `id`. Lands cursor on the bundle header (collapsed) or the member (expanded). |

Bundle expansion / collapse mutates `m.rendered` and adjusts
`m.cursor` to keep the same logical message in view:
- **Expand a bundle (cursor on header)**: cursor stays on the
  header row. New member rows appear below; rows beyond shift
  down. No cursor jump.
- **Collapse a bundle (cursor on the header or any member)**:
  cursor lands on the bundle header row. Rows beyond shift up.
- **Cursor on a flat row when an unrelated bundle expands /
  collapses elsewhere**: not user-triggered in practice (only
  user-triggered toggles fire the recompute), so this case does
  not arise. `SetMessages` recomputes wholesale and re-anchors on
  message ID per the table.

Single-message verbs (`r`, `R`, `f`, `d`, `m`, `M`, `T`, `U`, `c`,
`C`) operate on `SelectedMessage()`. On a *collapsed bundle row*
the verb targets the representative (the newest member, displayed
in the subject slot) — consistent with what the user sees. On an
*expanded bundle*, the cursor sits on either the bundle header
(verbs target the representative) or on a member row (verbs target
that member). The user's mental model is "the row I see is the row
I act on."

**The `Open` (`Enter`) special case:** when the focused row is a
collapsed bundle, `Enter` **expands** the bundle and **leaves the
cursor on the bundle header**. The user presses `Enter` again to
open the representative in the viewer (the second press resolves
to the header row, whose `SelectedMessage()` is the
representative). Rationale: opening the latest of a 12-message
digest is rarely what the user wants on first press — they want
to see the run. The two-press pattern is intuitive and reversible
(`Space` to collapse).

**Cursor traversal across bundles** (`j` / `k` contract):
- `j`/`k` advance by one *rendered* row, including across
  expanded-bundle members.
- Pressing `Space` to collapse a bundle while cursor is on a
  member moves cursor to the bundle header row (the member rows
  vanish; the header is the only valid landing spot).
- Pressing `Space` to collapse on the header itself keeps cursor
  on the header.
- `loadMore` triggers when the *underlying message index* (not the
  rendered row index) is within `loadMoreThreshold` of the message
  slice tail. This avoids a pre-fire when a long run of bundles
  collapses the tail visually.

### 5.6 Expansion state

Per-folder, session-local. `Model` gains:

```go
// bundleExpanded[folderID][address] = true when the bundle is open.
// Keyed by the resolved m.list.FolderID string at the moment the
// expansion state was set. Real folder IDs are Graph IDs; synthetic
// folder IDs include "filter:<src>" (spec 10), "search:<q>" (spec
// 06), and "__muted__" (spec 19 §5.4).
bundleExpanded map[string]map[string]bool

// bundledSenders is the in-memory set of designated addresses for
// the signed-in account, populated from store.ListBundledSenders
// at sign-in and re-read on Ctrl+R refresh. O(1) membership tests
// during the render pass.
bundledSenders map[string]struct{}
```

**Lifecycle of `bundleExpanded` entries**:
- Real folder IDs: entries persist across sidebar selections in the
  current session. Switching from Inbox → Drafts → Inbox restores
  the prior expansion state on the second visit. Cleared on
  sign-out.
- `filter:*` synthetic ID: deleted from `bundleExpanded` on
  filter exit (Esc / `ClearFilter` keybinding / `:unfilter` cmd —
  whichever path runs `clearFilter()`). A subsequent `:filter`
  with the same pattern produces the same `filter:` ID and starts
  with no expansion state — the user gets a clean slate. Note:
  the synthetic ID must include the `--all` scope variant (spec
  21) so that `:filter X` and `:filter --all X` do not share
  expand-state. The implementer composes the ID from the existing
  filter pattern source plus the `filterAllFolders` boolean (e.g.
  `"filter:" + src + ":all=" + allFlag`); reuse whatever helper
  exists today and assert the ID changes when scope flips.
- `search:*` synthetic ID: deleted on search exit (Esc out of
  search mode).
- `__muted__`: deleted on leaving the muted-threads virtual folder.
- Sign-out: all entries cleared (the `bundleExpanded` map is
  reinitialised to empty).

Both maps are bounded — bundles per folder rarely exceed dozens,
and the `bundledSenders` set is per-account (typical user: a few
dozen designated senders).

### 5.7 Bulk-pending interaction (spec 10)

When a `:filter` is active and the user presses `;d` (or any bulk
verb), the bulk path operates on the **filter result message
slice** (`m.filterIDs`), not on the rendered rows. Bundle collapse
is purely visual; a bundled run inside the filter result is still
N messages from the bulk-engine's perspective.

This is the right behaviour for the user: `:filter ~f newsletter@acme.com`
followed by `;d` deletes all 12 messages even if the bundle is
collapsed, because the user's intent is "delete the filter result".
The bundle is scenery.

The existing confirm-modal text (`Delete N messages?`) reflects the
filter-message count — i.e., the *true* count, not the rendered-
row count. No modal-text change is required by this spec; the user
sees `Delete 12 messages?` even when only 1 bundle row is visible.
The user-facing how-to recipe (§9 DoD) MUST call this out so users
don't mis-read the rendered-row count.

### 5.8 Mute interaction (spec 19) — precise contract

| List context | `ExcludeMuted` applied? | Bundle pass sees muted? |
| ------------ | ----------------------- | ----------------------- |
| Normal folder view (Inbox, etc.) | Yes (spec 19 §5.3) | No — muted messages are dropped before bundling. Bundle count reflects visible (non-muted) members. |
| `:filter` result | No (spec 19 §4.4) | Yes — muted messages appear inside bundles with their `🔕` indicator (visible only when bundle is expanded; bundle header carries no `🔕` per §5.2 indicator-precedence). |
| `/search` result | No (spec 19 §4.3) | Yes — same as filter. |
| Muted-threads virtual folder | False (the *whole point* is to show muted) | Yes — bundles in this view will, by construction, be entirely muted; the disclosure glyph wins on the header. |

The bundle pass operates on whatever message slice
`SetMessages(ms)` is given; it does not re-apply `ExcludeMuted`.
This makes the rule mechanically simple: "the bundle pass groups
whatever the store returned."

### 5.9 Thread chord on bundle representative (spec 20)

Pressing `T<verb>` on a focused collapsed bundle row uses the
bundle's representative (newest member) as the focused message and
acts on that message's *conversation*, not on the *bundle*. A
12-message bundle that spans 5 conversations: `T a` (archive
thread) archives one of the five conversations. After Graph
response and list reload, the bundle re-evaluates: if the archived
conversation contributed K messages, the bundle is now of size
12-K (or no longer a bundle if 12-K < `bundle_min_count`).

The status toast is whatever spec 20 emits today
(`✓ archived thread (N messages)`); spec 26 does NOT augment that
toast text — coupling spec 26's render-pass output to spec 20's
toast pipeline is unnecessary because the visible row-count change
on the next `View()` is its own feedback. The user sees the bundle
shrink (or vanish if it falls below `bundle_min_count`).

To act on the entire bundle as one unit, the user has two paths:
1. **Expand** the bundle, place cursor on a member, then use
   single-message verbs against each (slow, manual).
2. **Filter** to the bundle's sender (`:filter ~f <addr>`), then
   `;<verb>` against the filter result. This is the canonical
   "act on a whole bundle" workflow and is documented in the
   user-facing how-to.

### 5.10 Sidebar unread count (spec 19 §5.6 mirror)

Bundling does NOT hide messages — it collapses them visually. An
unread message inside a collapsed bundle is still represented in
the bundle's `(N)` summary. The folder sidebar's
`folders.unread_count` (sourced from Graph delta sync) remains
consistent with the user's perception: the unread is *visible*,
just *grouped*. No local adjustment is required, in contrast to
spec 19's noted gap where mute hides messages and the badge
count over-reports.

## 6. Designate / Un-designate flow (local-only, NOT via action queue)

Bundle designation does NOT use the spec 07 `action.Executor` or the
`actions` table. Same rationale as mute (spec 19 §6): the action
queue dispatches to Graph; bundle has no Graph call.

The desired post-state is decided **in the synchronous Update**
from the in-memory `bundledSenders` set (O(1) membership test);
the Cmd does the store write. This avoids a wasteful
`IsSenderBundled` SQL call on the keypress hot path and avoids a
TOCTOU race window between membership test and write — `Add` /
`Remove` are idempotent so a concurrent CLI write is harmless.

```go
// In Update, on B keypress:
addr := strings.ToLower(strings.TrimSpace(focused.FromAddress))
if addr == "" {
    m.lastError = errors.New("bundle: message has no sender")
    return m, nil
}
_, alreadyBundled := m.bundledSenders[addr]
nowBundled := !alreadyBundled
return m, bundleToggleCmd(m.ctx, m.deps.Store, m.account.ID, addr, nowBundled)

// The Cmd:
func bundleToggleCmd(ctx context.Context, st store.Store, accountID int64,
    address string, target bool) tea.Cmd {
    return func() tea.Msg {
        addr := strings.ToLower(strings.TrimSpace(address))
        if addr == "" {
            return errMsg{fmt.Errorf("bundle: empty sender address")}
        }
        var err error
        if target {
            err = st.AddBundledSender(ctx, accountID, addr)
        } else {
            err = st.RemoveBundledSender(ctx, accountID, addr)
        }
        if err != nil {
            return errMsg{err}
        }
        return bundleToastMsg{address: addr, nowBundled: target}
    }
}
```

After `bundleToastMsg` is received in `Update`:
- Update the in-memory `bundledSenders` set (add or delete) per
  `nowBundled`. Idempotent — if the CLI raced and the store row is
  the opposite of `nowBundled`, the next `Ctrl+R` (§6.1 below)
  reconciles. The toast shown to the user reflects the *intended*
  post-state of the keypress.
- Invalidate `m.list.bundleCache` (§8) so the next `View()` call
  recomputes the rendered rows.
- **Compute the toast count synchronously** by walking
  `m.list.messages` and counting consecutive runs of `addr` that
  are ≥ `bundle_min_count`. This walk is O(N), bounded by the
  same N as the bundle pass (≤2ms for N=1000). The total is the
  number of messages that will collapse in the current view. If
  the total is 0, the toast text uses the "no consecutive run"
  variant (§5.4); otherwise the "collapses N messages" variant.
  The walk reads the slice as it is in `Update`; no Cmd / Msg
  round-trip is needed.
- Show the §5.4 toast.

**Concurrency guard for rapid toggles.** Two `B` presses on the
same address in rapid succession produce two Cmds in flight; the
Cmds may complete out of order. To keep the in-memory set
consistent with the user's last expressed intent, `Model` carries
a `bundleInflight map[string]uint64` keyed by address: each `B`
keypress increments the counter for `addr` and the dispatched Cmd
carries the seq number. The `bundleToastMsg` handler ignores the
message if its seq is older than the current counter (a stale
toast for a superseded toggle). The set mutation only fires for
the most recent in-flight toggle per address.

```go
type bundleToastMsg struct {
    address    string
    nowBundled bool
    seq        uint64  // monotonic counter, per address
}
```

**Undo model:** `B` toggles in place. The spec 07 `u`-key undo
stack is **not** involved. Pressing `B` again reverses the
designation. Same rationale as mute (spec 19 §6).

### 6.1 Refresh-driven CLI sync

Per §4.3, the TUI's `bundledSenders` set is re-read from
`store.ListBundledSenders` on `Ctrl+R` (`Refresh` keybinding) and
on the post-sign-in init sequence. New plumbing:

```go
type bundledSendersLoadedMsg struct{ addresses []string }

func loadBundledSendersCmd(ctx context.Context, st store.Store,
    accountID int64) tea.Cmd {
    return func() tea.Msg {
        rows, err := st.ListBundledSenders(ctx, accountID)
        if err != nil {
            return errMsg{err}
        }
        addrs := make([]string, len(rows))
        for i, r := range rows {
            addrs[i] = r.Address
        }
        return bundledSendersLoadedMsg{addresses: addrs}
    }
}
```

`Refresh` (`Ctrl+R`) fans out to: `SyncAll` Cmd, `loadFoldersCmd`,
`loadMessagesCmd`, **and** `loadBundledSendersCmd`. The
`bundledSendersLoadedMsg` handler in `Update`:
1. Rebuilds the `bundledSenders` map from scratch (drop and
   replace, not merge — the store is authoritative).
2. Sweeps `bundleExpanded`: for every `(folderID, address)` pair,
   if `address` is no longer in `bundledSenders`, deletes the
   entry. This avoids leaking stale expand-state for senders that
   have been un-designated externally.
3. Invalidates `m.list.bundleCache`.

Empty-address guard: if a designated sender is removed externally
(SQL row deleted), the rebuilt set will not contain it; the next
render pass treats that sender's mail as flat. No crash, no
inconsistency.

### 6.2 No cmd-bar `:bundle` command in v1

Spec 26 deliberately does not add a `:bundle` cmd-bar command. The
`B` keybinding plus the `inkwell bundle` CLI cover the common
paths. The cmd-bar surface should grow only when there is a
demonstrated need (cf. spec 19 mute, which also ships without a
`:mute` cmd-bar). A future spec — most likely the command-palette
spec from ROADMAP §0 Bucket 2 item 1 — may add discoverable
cmd-bar entries for all per-sender designations together.

## 7. CLI

```sh
# Designate a sender as bundled
inkwell bundle add <email-address>

# Remove the designation
inkwell bundle remove <email-address>

# List currently bundled senders
inkwell bundle list
inkwell bundle list --output json
```

| Command | Text output | JSON output |
| ------- | ----------- | ----------- |
| `bundle add` | `✓ bundled <addr>` | `{"bundled": true, "address": "..."}` |
| `bundle remove` | `✓ unbundled <addr>` | `{"bundled": false, "address": "..."}` |
| `bundle list` | header `ADDRESS<TAB>ADDED` then one tab-separated row per sender; ADDED column is RFC3339 in the local timezone | `[{"address": "...", "added_at": "RFC3339"}, ...]` |

Address normalisation: every command lowercases its argument before
INSERT/DELETE/SELECT. `inkwell bundle add Bob@Acme.com` and
`inkwell bundle remove bob@acme.com` operate on the same row.

Subcommands live in `cmd/inkwell/cmd_bundle.go` and are registered in
`cmd_root.go`.

`inkwell bundle add` and `inkwell bundle remove` are fully
reversible local-only mutations and require no `--yes` flag.

**No-account guard**: every `inkwell bundle …` subcommand exits 1
with `inkwell: not signed in` if no `accounts` row exists, matching
the existing CLI convention (cf. `inkwell mute`, `inkwell thread`).

## 8. Performance budgets

| Surface | Budget | Benchmark |
| --- | --- | --- |
| Bundle pass over 1000 messages with 50 bundled senders (`SetMessages` recompute) | ≤2ms p95 | `BenchmarkBundlePass1000` in `internal/ui/` |
| `View()` cost overhead from bundling (cache hit) | ≤0.1ms p95, ≤4 allocations per call | `BenchmarkBundleViewRender` in `internal/ui/` |
| `AddBundledSender` / `RemoveBundledSender` | ≤1ms p95 | `BenchmarkBundleAddRemove` in `internal/store/` |
| `ListBundledSenders` (≤500 rows) | ≤2ms p95 | `BenchmarkListBundledSenders` |

### 8.1 Hot path — bundle cache

The bundle pass is O(N) over the rendered message slice. For
N=200 (the `loadLimit` default), 2ms is generous; for N=1000 (a
fully scrolled list), it stays well under the spec-02 list budget
of 10ms p95.

**Crucially, the bundle pass MUST NOT run on every `View()` call.**
Bubble Tea calls `View()` after every Msg (every cursor move,
every animation tick); recomputing the bundle structure each time
would either blow the latency budget or churn allocations.

The cache:

```go
// On ListModel:
type bundleCache struct {
    rendered      []renderedRow              // byte-for-byte the rendered rows
    messageIndex  []int                      // rendered row → underlying messages index
    valid         bool
}
```

`bundleCache` is **invalidated** (set `valid = false`) when:
- `SetMessages(ms)` is called (full recompute).
- `bundledSenders` is mutated (`B` toggle, `bundledSendersLoadedMsg`).
- `bundleExpanded[folderID][address]` flips for the current folder.
- Folder switch (cursor moves to a different `FolderID`).

`View()` checks `valid`; if false it runs the O(N) pass and sets
`valid = true`. Otherwise it iterates `rendered` directly with no
allocation. The benchmark `BenchmarkBundleViewRender` enforces
≤4 allocations per call after warm-up.

**Why not benchmark store-side bundling?** There is no store-side
path — bundling is rendered in the UI from an in-memory map. The
`bundled_senders` membership test is a Go `map[string]struct{}`
lookup; no SQL query per row.

## 9. Definition of done

- [ ] Migration `013_bundled_senders.sql` created; `bundled_senders`
      table with composite PK `(account_id, address)` + index on
      `account_id`. `schema_meta.version` bumped to '13'.
- [ ] `store.Store` interface gains `AddBundledSender`,
      `RemoveBundledSender`, `ListBundledSenders`, `IsSenderBundled`.
      All four lowercase the address argument inside the store
      (defense-in-depth); UI/CLI also lowercase before call.
      `BundledSender` struct exposed (Address lowercased; AddedAt
      `time.Time`).
- [ ] `KeyMap` gains `BundleToggle key.Binding` (default `B`) and
      `BundleExpand key.Binding` (default `" "`); `BindingOverrides`
      gains matching string fields; `ApplyBindingOverrides` wires
      both; `findDuplicateBinding` includes both. The existing
      `Expand` binding (`o` / Space) is unchanged.
- [ ] `Model` gains `bundleExpanded map[string]map[string]bool`
      (lifecycle per §5.6) and `bundledSenders map[string]struct{}`
      (in-memory hot-path set). Both initialised on sign-in
      (`bundledSenders` from `ListBundledSenders`); cleared on
      sign-out. `bundleExpanded[folderID]` entries deleted per the
      §5.6 lifecycle table (real folders persist; synthetic
      `filter:*` / `search:*` / `__muted__` entries deleted on
      exit).
- [ ] `B` wired in `dispatchList`; computes
      `nowBundled := !bundledSenders[addr]` synchronously, then
      dispatches `bundleToggleCmd(addr, nowBundled)`. On flat-row
      focus, address = `lowercase(message.FromAddress)`. On bundle-
      header focus, address = the bundle's address. On
      `bundleToastMsg`, mutate the in-memory set, invalidate
      `bundleCache`, show §5.4 toast. The toast distinguishes
      "collapses N messages" vs "no consecutive run in current
      view" per §5.4.
- [ ] `BundleExpand` (Space) wired in `dispatchList`: toggles
      `bundleExpanded[folderID][address]` for the focused bundle
      header; invalidates `bundleCache`. On a non-bundle row,
      Space is a no-op (no fall-through to folder Expand — that
      fires only when the folders pane is focused).
- [ ] `Enter` (`Open` binding) wired in `dispatchList`: if focused
      row is a *collapsed* bundle header, expand it and **leave
      cursor on the bundle header** (NOT the first member); do
      NOT open the viewer. Otherwise (flat row, expanded bundle
      header second-press, expanded bundle member) preserve
      existing open-in-viewer behaviour.
- [ ] `ListModel.bundleCache` field; invalidated per §8.1
      (`SetMessages`, `bundledSenders` mutation, `bundleExpanded`
      flip for current folder, folder switch). `View()` reads from
      cache; on miss, runs the O(N) bundle pass and populates.
- [ ] `ListModel.rowAt(i) renderedRow`,
      `ListModel.SelectedMessage() (store.Message, bool)`, and
      `ListModel.messageIndexAt(i) int` helpers implemented per
      §5.5. All three read from `bundleCache` after the cache is
      populated; the cache is built in a single O(N) pass that
      fills both `rendered` and `messageIndex`.
- [ ] **Cursor-consumer audit** (§5.5 table): every existing
      consumer of `m.cursor` updated. Specifically:
      - `Selected() (store.Message, bool)` is renamed to
        `SelectedMessage() (store.Message, bool)`. The
        `(_, ok)` shape is preserved; an empty list / out-of-
        bounds cursor returns `(zero-Message, false)` exactly as
        today. Every call site (≥22 in `app.go` and one in
        `panes_test.go` / `mute_test.go`) updated to the new
        name; the pattern `sel, ok := m.list.Selected()` becomes
        `sel, ok := m.list.SelectedMessage()`.
      - `Up()`/`Down()`/`PageUp()`/`PageDown()` step rendered rows.
      - `JumpTop()`/`JumpBottom()` use rendered-row bounds.
      - `ShouldLoadMore()` uses `messageIndexAt(cursor)`, NOT
        `m.cursor`, against `len(m.messages)`.
      - `AtCacheWall()` and `OldestReceivedAt()` continue to
        operate on the underlying `m.messages` slice.
      - `MoveCursorToMessage(id)` (or equivalent re-anchor on
        `SetMessages`) lands cursor on the matching rendered row
        (bundle header if collapsed, member if expanded).
- [ ] List pane render pass groups consecutive same-sender messages
      where the sender is in `bundledSenders` AND the run length
      ≥ `[ui].bundle_min_count`. Bundle header row format per §5.2;
      member rows indented when expanded; FOLDER column treatment
      per §5.2.
- [ ] `[ui].bundle_min_count` config key (default 2; range 0–9999;
      0 = bundling disabled while preserving designations) added
      to `internal/config/defaults.go` + `docs/CONFIG.md`.
- [ ] `[ui].bundle_indicator_collapsed` (default `▸`) and
      `[ui].bundle_indicator_expanded` (default `▾`) config keys
      added (range: any string ≤ 2 chars); ASCII fallbacks
      documented as `>` and `v`.
- [ ] `Ctrl+R` (`Refresh`) fans out to `loadBundledSendersCmd`
      alongside the existing reload Cmds (§6.1). The
      `bundledSendersLoadedMsg` handler replaces `bundledSenders`
      wholesale and invalidates `bundleCache`.
- [ ] CLI: `cmd/inkwell/cmd_bundle.go` implementing `inkwell
      bundle add`, `inkwell bundle remove`, `inkwell bundle list`
      with `--output json` support; registered in `cmd_root.go`.
      Address argument lowercased in the CLI helper before any
      store call (matching the store's defense-in-depth lowercase).
- [ ] **Audit existing Enter-opens-viewer e2e tests**
      (`internal/ui/app_e2e_test.go`): document that their fixtures
      contain no run of consecutive same-sender messages from a
      designated sender, OR pin the first row's sender so no
      bundle forms. Add a one-line comment in each affected test.
- [ ] Tests:
      - **store** (`internal/store/`):
        - `TestAddBundledSenderIdempotent` — second `Add` is no-op,
          no error, no duplicate row.
        - `TestRemoveBundledSenderNoop` — `Remove` of a non-existent
          row returns nil.
        - `TestListBundledSendersOrder` — order is `added_at DESC,
          address ASC`.
        - `TestIsSenderBundledMixedCaseInput` — `Add("BOB@A.COM")`
          stores `bob@a.com`; `IsSenderBundled("Bob@A.com")` returns
          true (defense-in-depth lowercase).
        - `TestBundledSendersAccountFKCascade` — deleting an
          account row cascades `bundled_senders` rows.
      - **UI dispatch / e2e** (`internal/ui/`):
        - `TestBundleKeyToggleAddsToSet` — `B` on flat row from a
          non-bundled sender → AddBundledSender called, in-memory
          set updated, toast says "collapses N messages".
        - `TestBundleKeyDesignateNoConsecutiveRunToast` — designate
          a sender that has only 1 message in the current view;
          toast text matches "no consecutive run in current view".
        - `TestBundleKeyToggleRemovesFromSet` — `B` on bundle-header
          row → RemoveBundledSender called, in-memory set cleared,
          bundle dissolves to flat rows in place
          (`TestBundleDissolvesOnUnDesignate`).
        - `TestBundleKeyEmptyAddressShowsError` — focused message
          with empty `from_address` → error toast, no store write.
        - `TestBundleKeyMapNoDuplicates` — runs
          `findDuplicateBinding(DefaultKeyMap())` and asserts
          empty result (B and Space don't collide with anything).
        - `TestSpaceTogglesBundleExpandInListPane` — visible-delta:
          before press, member rows hidden; after, visible; second
          press hides again. Cursor stays on bundle header.
        - `TestSpaceInFoldersPaneStillTogglesFolder` — pane-scoped
          regression: Space in folders pane toggles folder tree.
        - `TestSpaceOnFlatRowInListPaneIsNoop` — pane-scoped
          regression: Space on flat row neither expands a folder
          nor changes any state.
        - `TestEnterOnCollapsedBundleExpandsCursorStaysOnHeader` —
          visible-delta: glyph flips `▸` → `▾`, cursor stays on
          bundle header (not first member), viewer pane unchanged.
        - `TestEnterOnExpandedBundleHeaderOpensRepresentative` —
          second Enter on the (already expanded) bundle header
          opens the newest member in the viewer.
        - `TestEnterOnFlatRowOpensViewer` — regression: flat-row
          Enter still opens viewer.
        - `TestEnterOnExpandedMemberOpensThatMessage` — Enter on a
          bundle member opens that specific member.
        - `TestBundleHeaderShowsCountAndLatestSubject` — visible-
          delta: row contains `(12) — <subject>` for a 12-member
          bundle.
        - `TestBundleHeaderHasNoMuteCalendarFlagGlyph` — visible-
          delta: bundle header carries only the disclosure glyph;
          no `📅` / `🔕` / `⚑`. Member rows render those normally.
        - `TestBundlePassDeterministicOrder` — fixture with two
          separate runs of the same designated sender split by a
          different sender renders TWO bundle rows, not one.
        - `TestConsecutiveEmptyFromAddressNeverBundles` — fixture
          with two consecutive empty-from messages renders flat
          (the empty-string key is never inserted into
          `bundledSenders`).
        - `TestPlusTagDoesNotMatchBaseAddress` — designate
          `news@acme.com`; messages from `news+abc@acme.com`
          render as flat rows.
        - `TestBundleMinCountTwoCollapsesPair` — fixture with 2
          consecutive same-sender bundled messages → 1 bundle row.
        - `TestBundleMinCountThreeLeavesPairFlat` — same fixture
          with `bundle_min_count=3` → 2 flat rows.
        - `TestBundleMinCountZeroDisablesBundling` — `bundle_min_count=0`
          → all rows flat regardless of designations.
        - `TestBundleExpandedClearedOnFilterExit` — `:filter`,
          expand, `:unfilter`, re-`:filter` with same pattern →
          bundle starts collapsed.
        - `TestBundleExpandedPersistsOnRealFolderRoundTrip` —
          designate sender, expand bundle in Inbox, switch to
          Drafts, switch back to Inbox → bundle still expanded.
        - `TestBundleNotAffectedBySingleMessageVerbs` — `f` on
          collapsed bundle row flags ONLY the representative.
        - `TestThreadChordOnBundleArchivesOnlyOneThread` — fixture
          mixing 5 threads in a 12-message bundle; `T a` archives
          one thread (e.g. 3 messages); post-reload bundle is 9.
          Status toast text is whatever spec 20 emits today (plain
          `✓ archived thread (3 messages)`); spec 26 does NOT
          augment it. The visible row-count change (12 → 9 in the
          bundle header `(N)` slot) is the bundle-side feedback.
        - `TestBundlePersistsAcrossRestart` — designate, simulate
          reopen → `bundledSenders` re-populated, bundle visible.
        - `TestCrossFolderBundleShowsPlusNFolders` — `:filter --all`
          fixture with bundle members in 2 folders → header shows
          `Inbox +1`.
        - `TestBundleFolderColumnTruncatesFolderNameKeepsPlusN` —
          fixture with folder name `Promotions` (10 chars) + 2
          others; expected render `Promot…+2` (truncated head,
          intact suffix; total 12 chars).
        - `TestMutedMessagesExcludedFromBundleInNormalView` —
          normal folder view: 12-message bundle with 3 muted →
          bundle of 9 (bundle pass sees only the 9 non-muted).
        - `TestMutedMessagesIncludedInBundleOnFilterView` —
          `:filter ~f addr` including muted: bundle of 12 (members
          when expanded include muted with `🔕` glyph).
        - `TestBulkPendingDeletesAllBundleMembers` — `:filter` →
          bundle collapsed to 1 row → `;d` modal says
          `Delete 12 messages?`; on confirm, all 12 deleted.
        - `TestCtrlRRefreshesBundledSendersFromStore` — start TUI
          with empty bundle set, externally insert a row via store,
          send `Ctrl+R`, assert in-memory set now contains the
          address and a fixture run of that sender bundles.
        - `TestPageDownAcrossBundle` — fixture: 100 flat rows,
          a bundle of 50, 100 more flat rows. PageDown by 20s
          steps over rendered rows; at the bundle row, one
          PageDown jumps past the bundle (one rendered row); the
          *underlying* message index advances by ~50.
        - `TestLoadMoreFiresAtMessageTailNotRenderedTail` — fixture
          where rendered tail is densely bundled. Cursor near
          rendered-tail does NOT trigger load-more; cursor near
          message-tail (after expanding bundles) does.
        - `TestCollapseFromMemberMovesCursorToHeader` — expand
          bundle, j/k onto a member, press Space → cursor lands
          on bundle header (not before, not after).
        - `TestBundleHeaderColumnWidthMatchesFlatRow` — render
          two adjacent rows (one flat, one bundle header) and
          assert their total column width is byte-identical
          (after stripping the variable subject suffix). Guards
          the §5.2 column-width invariant.
        - `TestRapidBundleToggleSequenceConsistency` — fire two
          `B` presses in quick succession on the same address
          (delivering Cmds out of order via a test scheduler);
          assert the in-memory `bundledSenders` set matches the
          last keypress's intent (per §6 seq guard).
        - `TestRefreshSweepsStaleBundleExpandedEntries` —
          designate sender, expand bundle, externally remove the
          sender via store, send `Ctrl+R`; assert
          `bundleExpanded[folderID][addr]` is gone after the
          refresh.
        - `TestFilterAllScopeVariantHasDistinctExpandState` —
          `:filter X`, expand a bundle; `:unfilter`; `:filter
          --all X` → bundle starts collapsed (different synthetic
          ID due to scope difference).
        - `TestBundleIndicatorOverrideClampedToWidth2` — config
          `[ui].bundle_indicator_collapsed = "▶▶▶"` (3 cells) is
          rejected at config-load time (matches spec 19 §5.2
          `mute_indicator` precedent of ≤2-cell glyphs); a CJK
          single-rune override (e.g. `"中"`, 2 cells) is accepted.
      - **CLI** (`cmd/inkwell/`):
        - `TestBundleCLIAddLowercases` — `Bob@Acme.com` →
          `bob@acme.com` in store.
        - `TestBundleCLIRemoveByCanonicalAddr`.
        - `TestBundleCLIListJSONShape`.
        - `TestBundleCLIListTextShape` — header `ADDRESS<TAB>ADDED`
          + RFC3339-in-local-tz timestamps.
        - `TestBundleCLIAddIdempotent` — second add is a no-op
          and exits 0.
        - `TestBundleCLINoAccountErrors` — no account row in DB
          → all three subcommands exit 1 with
          `inkwell: not signed in`.
        - `TestCLIAddDoesNotApplyToRunningTUIUntilRefresh` —
          start TUI process / model fixture, run `bundle add`
          via direct store call, send j/k keys (no `Ctrl+R`),
          assert no bundling; send `Ctrl+R`, assert bundling now
          occurs.
      - **Logging / redaction**:
        - `TestBundleAddLogsRedactedAddress` — capture slog output
          via the existing `internal/log/` test helper; call
          `AddBundledSender` with `news@acme.com`; assert log line
          contains the `<email-` prefix and does NOT contain the
          raw address. Do NOT assert a specific N (per-process
          counter; not stable across tests).
        - `TestBundleRemoveLogsRedactedAddress` — same for Remove.
      - **Benchmarks** (`internal/store/`, `internal/ui/`):
        - `BenchmarkBundlePass1000` — 1000-msg slice, 50 bundled
          senders, ≤2ms p95.
        - `BenchmarkBundleViewRender` — cache-hit View() iteration
          over 1000 rendered rows, ≤0.1ms p95, ≤4 allocations
          per call (`b.ReportAllocs()`).
        - `BenchmarkBundleAddRemove` — single-row add/remove cycle,
          ≤1ms p95.
        - `BenchmarkListBundledSenders` — 500-row list, ≤2ms p95.
- [ ] User docs:
      - `docs/user/reference.md` adds: `B` row in list-pane
        keybindings; `Space` row in list-pane keybindings (note:
        bundle expand/collapse, distinct from folders-pane Space);
        bundle indicators (`▸` / `▾`) row in Indicators table;
        `bundle add` / `bundle remove` / `bundle list` rows in CLI
        subcommands.
      - `docs/user/how-to.md` adds "Bundle a noisy newsletter
        sender" recipe with the canonical "act on the whole
        bundle" workflow (`:filter ~f <addr>` then `;<verb>`),
        and a callout that the `;d` confirm modal shows the true
        message count, not the rendered-row count.
- [ ] `docs/plans/spec-26.md` created at start of implementation
      per CLAUDE.md §13 and updated each iteration.
- [ ] `docs/PRD.md` §10 spec inventory: row for 26 (bundle
      senders) added. Specs 22–25 already exist on main with
      their own inventory rows.

## 10. Cross-cutting checklist

- [ ] Scopes: none new (`Mail.ReadWrite` already in PRD §3.1; bundle
      is local-only and makes no Graph calls).
- [ ] Store reads/writes: `bundled_senders` (INSERT + DELETE +
      SELECT). `messages` read-only (no schema change). FK cascade
      on account delete handles cleanup automatically.
- [ ] Graph endpoints: none.
- [ ] Offline: works fully offline. Bundle state is local; sync does
      not touch it. Designation made offline persists; comes alive
      against new mail when sync runs.
- [ ] Undo: toggle (`B` again) is the undo. Does NOT push to spec 07
      undo stack. `u` does not un-designate.
- [ ] User errors: focused message with empty `from_address`
      (§5.4 toast); DB write failure surfaces as a status-bar error
      toast.
- [ ] Latency budget: ≤2ms p95 for bundle render pass over 1000
      messages with 50 designated senders (§8 benchmark). ≤1ms
      for store add/remove.
- [ ] Logs: `AddBundledSender` / `RemoveBundledSender` log at DEBUG
      with the **redacted** address (`<email-N>` per session, per
      `internal/log/redact.go`) — never the raw address. Toast
      address display is UI-only, not logged.
- [ ] CLI mode: `inkwell bundle add` / `remove` / `list` per §7.
- [ ] Tests: §9 test list.
- [ ] **Spec 17 review:** bundle adds a new store path (local CRUD)
      and a new log site (redacted address at DEBUG). No new
      external HTTP, no token handling, no subprocess, no
      cryptographic primitive, no new SQL composition (parameterised
      INSERT/SELECT/DELETE only). New persisted local state row:
      `bundled_senders` table → add to `docs/PRIVACY.md` "what data
      inkwell stores locally" section. Threat-model row: bundled
      sender list is per-account local; signing out + cache purge
      removes via FK cascade (covered by spec 19's existing model).
      No `// #nosec` annotation needed; no new security CI gate.
- [ ] **Spec 19 consistency:** the bundle pass groups whatever
      `SetMessages` is given. On normal folder views, that slice
      is post-`ExcludeMuted=true`, so muted messages do not appear
      in bundles (bundle of N - M_muted). On filter / search /
      muted-threads views, `ExcludeMuted` is intentionally false
      per spec 19 §4.3 / §4.4, so muted messages appear inside
      bundles with their `🔕` glyph (visible only when the bundle
      is expanded; bundle header carries no `🔕` per §5.2 indicator
      precedence). See §5.8 contract table.
- [ ] **Spec 20 consistency:** `T` chord on a focused collapsed
      bundle row uses the bundle's representative (newest member)
      as the focused message — `T a` archives the representative's
      conversation, NOT the entire bundle. The bundle re-evaluates
      after reload (§5.9). To act on the entire bundle as one
      unit, the user uses `:filter ~f <addr>` then `;<verb>`.
- [ ] **Spec 23 consistency** (routing destinations): bundling and
      routing are orthogonal axes. A sender can be both routed
      (e.g. to Feed) AND bundled — the routing changes which
      virtual folder the messages appear in; bundling collapses
      consecutive runs *within* whichever view the user is
      looking at. The `S` chord (spec 23) and `B` keybinding (this
      spec) are distinct: `S` opens an autocomplete picker for a
      destination, `B` toggles a boolean designation. The tables
      `sender_routing` (spec 23) and `bundled_senders` (this spec)
      are independent and may both contain the same address.
- [ ] **Spec 24 consistency** (split inbox tabs): bundling renders
      across whatever message slice the active tab provides. A
      saved-search-backed tab returns its result set; the bundle
      pass groups consecutive same-sender runs in that result.
      Tab switching is a folder-change event and clears the
      synthetic-folder branch of `bundleExpanded` per §5.6 (real
      folders persist; tab IDs are synthetic).
- [ ] **Spec 21 consistency:** cross-folder filter (`:filter --all`)
      results bundle by sender across folders; the bundle header's
      FOLDER slot shows `<folder> +N` when members span >1 folder
      (§5.2). When the formatted string exceeds the 12-char
      column, the helper truncates the folder name and preserves
      the `+N` suffix verbatim. The flat-row FOLDER column
      rendering for non-bundle rows is unchanged.
- [ ] **Docs consistency sweep:** `docs/CONFIG.md` updated for
      `[ui].bundle_min_count`, `[ui].bundle_indicator_collapsed`,
      `[ui].bundle_indicator_expanded`; `docs/user/reference.md`
      updated for `B` keybinding, `Space` in list pane, indicators,
      CLI commands; `docs/user/how-to.md` updated for the recipe;
      `docs/PRIVACY.md` updated for the new local table; `docs/PRD.md`
      §10 spec inventory adds spec 26 row.
