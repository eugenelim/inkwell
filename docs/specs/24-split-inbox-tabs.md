# Spec 24 — Split inbox tabs

**Status:** Ready for implementation.
**Depends on:** Specs 02 (saved_searches table), 04 (TUI shell), 07
(triage actions — archive verb), 08 (pattern compile/execute), 11
(saved-search Manager), 19 (mute — `ExcludeMuted` filter), 20
(conversation ops — thread chord), 21 (cross-folder filter — `--all`
convention).
**Blocks:** Custom actions framework (will reuse the tab-scoped
default for bulk semantics).
**Estimated effort:** 2 days.

---

## 1. Goal

Today, the user picks one folder OR one saved search at a time. The
list pane shows that one query's results. Power users with high inbox
volume need to flip rapidly between several views — "VIP", "Newsletters",
"Calendar invites", "Clients" — without round-tripping through the
sidebar each time.

This spec adds a **tab strip** at the top of the list pane. Each tab
references an existing saved search by name; cycling tabs runs the
referenced pattern and renders results, exactly as if the user had
selected the saved search in the sidebar — except each tab keeps its
own cursor and scroll position across cycles.

The data model already supports it: `saved_searches` (spec 02 / spec
11) is the source of truth; `pattern.Compile` / `Manager.Evaluate`
(spec 08 / spec 11) is the execution path; `displayedFolder.savedID`
(spec 04) is how the sidebar selects one. This spec is a UI overlay
on existing primitives, plus a per-account ordering field.

### 1.1 What does NOT change

- Saved searches remain the source of truth. A tab is a *reference*
  to a saved search, not a new entity. Editing the saved search
  (rename, repattern) updates the tab automatically.
- Folder selection in the sidebar is unchanged. Selecting "Sent" or
  any folder hides the tab strip (tabs slice saved searches, not
  arbitrary folders) and renders that folder normally.
- The action queue (spec 07) is untouched. Archive remains `a`;
  permanent delete remains `D`. Tabs do not introduce new verbs.
- The "Saved Searches" sidebar section (spec 11 §5.1) continues to
  render pinned searches with their `☆` glyph and counts. A saved
  search may be both sidebar-pinned and promoted to a tab; the two
  states are independent.
- The `:filter` command (spec 10) and its `--all` cross-folder
  variant (spec 21) keep working as today; behaviour while a tab is
  active is defined explicitly in §5.3.
- Thread chord (`T`, spec 20) and mute (`M`, spec 19) are unchanged.
- No "done" verb / `E` keybinding is introduced. The roadmap §1.23
  "rebrand archive as done" is a separate, deferred item; this spec
  is binding-neutral. The "messages disappear from all tabs on
  archive" property in the roadmap is *emergent*, not a new verb
  (see §5.4).
- No ML / auto-classification. Hard-coded category tabs (Gmail-style
  Promotions/Social, Spark's Notifications) are explicitly out of
  scope; the user defines tabs by writing patterns, full stop.

## 2. Prior art

### 2.1 Terminal clients

- **mutt / neomutt + notmuch virtual mailboxes.** A virtual mailbox
  is a saved notmuch query rendered as an entry in the sidebar.
  `c` (change-folder) cycles between them. Cursor state is preserved
  per virtual mailbox. No top-of-pane tab strip — the sidebar carries
  the affordance. Users on r/commandline routinely complain that
  cycling N virtual mailboxes by name in the sidebar is slower than
  flipping numbered tabs.
- **aerc.** Tabs are first-class. Every account / folder / search
  result opens in its own tab. `gt` / `gT` cycle (vim convention);
  `Alt+N` jumps to tab N. Each tab has its own buffer + cursor.
  Tabs are session-local — lost on restart, the #1 complaint on the
  aerc issue tracker.
- **alot (notmuch).** Each search opens a buffer; `Tab` / `Shift+Tab`
  cycle. Buffer model is unfamiliar to non-Emacs users; no top tab
  strip.
- **Mailcap-style TUIs (mblaze, mbsync + custom scripts).** No tab
  abstraction; users compose with shell.

### 2.2 Desktop / web clients

- **Hey (Imbox / Feed / Paper Trail).** Three hard-coded buckets
  defined by per-sender routing, not patterns. New senders go through
  a Screener. Well-loved in concept; in practice users complain that
  routing is per-sender forever, so a colleague who occasionally
  sends a newsletter lands wrong. **We do not copy this model** —
  Inkwell tabs are query-defined, not sender-defined, which composes
  with the rest of the app (`:filter`, patterns, bulk delete).
- **Superhuman Split Inbox.** Splits are saved filters (from / to /
  domain / label / calendar). Auto-Split creates News, Marketing,
  Calendar, Travel splits automatically. Tab strip across the top of
  the inbox; `Cmd+K` then split name to jump. Each split has an
  independent cursor. Bulk operations scope to the active split.
  **This is the closest analogue to what we want.**
- **Gmail Tabs** (Primary / Social / Promotions / Updates / Forums).
  Hard-coded ML categories; user can drag a message between tabs to
  retrain. Most-complained-about feature: misclassification is
  opaque, no override per-pattern. **We don't copy this** — tabs
  must be deterministic.
- **Gmail Multiple Inboxes.** User-supplied search queries (up to
  five panels), e.g. `is:starred`, `from:boss@`. No first-class
  keyboard cycling; settings-buried; no per-panel cursor. The right
  *concept*, wrong *UX*.
- **Outlook Focused / Other.** Two-tab server-side ML split. Opaque
  routing, no user-defined queries. We already deny
  `inferenceClassification` writes (PRD §3.1) — Focused/Other is a
  separate spec (1.15).
- **Thunderbird "open folder in new tab".** Each tab is a folder
  view; `Ctrl+Tab` cycles. Tabs are not restored on restart unless
  the user opts in.
- **Mimestream Smart Folders.** Saved Gmail queries shown as
  sidebar entries; no top tab strip. `Cmd+1`–`9` jumps to sidebar
  position N.
- **Spark Smart Inbox.** Hard-coded categories (Personal,
  Notifications, Newsletters, Pins, Seen). Same complaints as
  Gmail Tabs. Out of model for us.
- **Twitter / X Lists.** Pinned curated feeds rendered as a top tab
  strip on the home timeline. The relevant lesson: users churn on
  curation cost — a starter set matters.

### 2.3 Design decision

Follow Superhuman / aerc:

- **Tabs are saved searches.** No new entity, no new DSL, no
  per-sender routing. Spec 11's saved searches are already the
  primitive; tabs reuse them.
- **Tab strip lives at the top of the list pane.** Cycling stays
  in muscle memory once the user is in the list — no sidebar round
  trip. Each tab has its own cursor + scroll, restored on switch.
- **Cycle keys are pane-scoped.** `]` (next tab) / `[` (prev tab)
  bind only when the list pane is focused. These keys are already
  pane-scoped to other meanings: viewer pane uses them for
  `NavPrevInThread` / `NavNextInThread` (spec 05 §12,
  `app.go:4939-4949`); calendar pane uses them for prev/next day
  navigation (spec 12, `app.go:2124-2131`). Adding a third
  pane-scoped meaning (list pane = next/prev tab) is consistent
  with the existing pattern — each pane interprets the bracket
  keys as "next/prev within this pane's logical axis."
  We deliberately do NOT overload `Tab` / `Shift+Tab` (which mean
  "cycle pane focus" globally, spec 04 §5 and `keys.go:156-157`)
  — overloading the most-used navigation binding has burned aerc
  users for years.
- **Counts are unread-only,** updated on `FolderSyncedEvent`.
  Total counts are noisy; unread is what the user scans.
- **No implicit "Other" tab.** Tabs do not partition a parent
  folder. A message can match zero tabs and still be reachable via
  the regular Inbox folder selection. This avoids the "where did
  my message go?" failure mode of partitioned-tab models.
- **Bulk operations scope to the active tab.** `:filter` while a
  tab is active runs the filter pattern AND'd with the tab pattern.
  `:filter --all` (spec 21) widens to the whole mailbox, ignoring
  the tab. Same convention as cross-folder bulk.
- **Tabs persist across restart.** Stored in SQLite via a new
  `tab_order` column on `saved_searches`. Lost-tabs-on-restart is
  the #1 aerc complaint; we don't repeat it.

## 3. Storage

### 3.1 Schema migration `011_tab_order.sql`

```sql
ALTER TABLE saved_searches
    ADD COLUMN tab_order INTEGER;

CREATE UNIQUE INDEX idx_saved_searches_tab_order
    ON saved_searches(account_id, tab_order)
    WHERE tab_order IS NOT NULL;

UPDATE schema_meta SET value = '11' WHERE key = 'version';
```

`tab_order` is `NULL` (not a tab) by default. A non-NULL integer
denotes the saved search is promoted to the tab strip; the integer is
the strip position. Order is dense (0, 1, 2, …) and **uniqueness is
enforced** per account by the partial UNIQUE index above. `Promote`
and `Reorder` (§4) take a row-level transaction and write all
shifted tab_order values in one statement to avoid transient
collisions.

The column is per-account because `saved_searches` is per-account;
each account has its own tab strip.

Partial indexes are used widely in this repo already
(`idx_folders_well_known`, `idx_messages_unread`,
`idx_messages_unsubscribe`, `idx_compose_sessions_unconfirmed`)
and supported on every SQLite build the project compiles against
(modernc.org/sqlite, since SQLite 3.8.0 / 2013).

The `schema_meta` UPDATE is the convention used by every prior
migration (`002_meeting_message_type.sql` through
`010_conv_account_idx.sql`).

### 3.2 TOML mirror

The saved-searches TOML mirror (spec 11 §4,
`~/.config/inkwell/saved_searches.toml`) gains an optional `tab_order`
field per `[[search]]` block:

```toml
[[search]]
name = "Newsletters"
pattern = '~f newsletter@* | ~f noreply@*'
pinned = true
sort_order = 1
tab_order = 0          # tab strip position 0 (leftmost)

[[search]]
name = "VIP"
pattern = '~f boss@example.invalid | ~f spouse@'
pinned = true
sort_order = 2
tab_order = 1          # tab strip position 1
```

Absent `tab_order` ⇒ not a tab (NULL in DB). Spec 11's divergence
prompt continues to govern user-edited mirrors.

### 3.3 `store.SavedSearch` struct

Add a single nullable field:

```go
// internal/store/types.go
type SavedSearch struct {
    ID         int64
    AccountID  int64
    Name       string
    Pattern    string
    Pinned     bool
    SortOrder  int
    TabOrder   *int       // nil = not a tab; *int = strip position
    CreatedAt  time.Time
}
```

`*int` (pointer) rather than `int` so that "no tab" is unambiguously
distinct from `tab_order = 0` (leftmost tab).

### 3.4 Store API additions

```go
// internal/store/saved_searches.go
// ListTabs returns the saved searches that are promoted to the tab
// strip, ordered by tab_order ASC. Result excludes NULL tab_order
// rows.
func (s *Store) ListTabs(ctx context.Context, accountID int64) ([]SavedSearch, error)

// SetTabOrder writes tab_order for one saved search. Pass nil to
// demote (clear tab status). Idempotent. Does not renumber siblings.
func (s *Store) SetTabOrder(ctx context.Context, id int64, order *int) error

// ReindexTabs renumbers tab_order for the given account so values
// are dense (0..N-1) preserving relative order. Called by Manager
// after add/remove/reorder mutations. Atomic via single transaction.
// Implementation: inside one tx, set every tabbed row's tab_order to
// NULL via one UPDATE, then set them back to dense values via a
// second UPDATE keyed by (CASE WHEN id = ? THEN N END) — or
// equivalently a temp-table renumber. The two-pass form keeps the
// partial UNIQUE index in §3.1 satisfied at every point.
func (s *Store) ReindexTabs(ctx context.Context, accountID int64) error

// ApplyTabOrder writes a full ordered slice of saved-search IDs as
// the new tab strip in one transaction. Any ID present is set to
// its index in the slice; any tabbed row whose ID is absent from
// the slice is demoted (tab_order = NULL). This is the single
// transactional helper that Promote / Demote / Reorder use to
// implement the "shift many rows in one tx" path; it makes the
// partial UNIQUE in §3.1 trivially satisfied via the same
// two-pass NULL-then-renumber strategy as ReindexTabs.
func (s *Store) ApplyTabOrder(ctx context.Context, accountID int64, ids []int64) error
```

`PutSavedSearch` is unchanged — promotion to a tab goes through
`ApplyTabOrder` / `SetTabOrder`, not through `Save`. This keeps the
saved-search create path narrow and avoids the "Manager.Save
renumbered everyone's tabs" surprise.

`SetTabOrder` is retained as a single-row helper used by tests and
by the (rare) "demote one specific tab" edge cases. The
`ApplyTabOrder` helper is the workhorse for any multi-row mutation
and is what Manager calls.

## 4. Manager API additions

```go
// internal/savedsearch/manager.go

// Tabs returns the saved searches promoted to the tab strip for the
// active account, in display order.
func (m *Manager) Tabs(ctx context.Context) ([]store.SavedSearch, error)

// Promote attaches a saved search (by name) to the tab strip,
// appending at the end. Idempotent: if already a tab, returns the
// current tab order unchanged. Returns the assigned tab_order.
// Re-writes TOML mirror. Errors if name does not resolve.
func (m *Manager) Promote(ctx context.Context, name string) (int, error)

// Demote removes a saved search from the tab strip. Idempotent for
// non-tabs (no-op). Reindexes remaining tabs to keep dense ordering.
// Re-writes TOML mirror.
func (m *Manager) Demote(ctx context.Context, name string) error

// Reorder moves the tab at position `from` to position `to`. Both
// indices are 0-based against the current tab list. Returns an
// error if either is out of bounds. Re-writes TOML mirror.
func (m *Manager) Reorder(ctx context.Context, from, to int) error

// CountTabs returns the unread match count for each tab keyed by
// saved-search ID. Used by the UI for tab badges. Errors per-tab
// are swallowed (tab badge falls back to "?" — see §5.5); the call
// itself errors only on infrastructure faults (e.g., DB closed).
func (m *Manager) CountTabs(ctx context.Context) (map[int64]int, error)
```

Notes:

- `Promote` / `Demote` / `Reorder` compute the desired new ordered
  list of saved-search IDs in memory, then call `store.ApplyTabOrder`
  (§3.4) once. `ApplyTabOrder` runs the two-pass NULL-then-renumber
  inside one transaction so the partial UNIQUE index in §3.1 is
  satisfied at every visible state. There is no caller-driven
  multi-statement orchestration; the store owns the transaction.
- **`Manager.Delete` and `Manager.DeleteByName` are modified** to
  call `store.ReindexTabs(accountID)` after a successful delete.
  Without this, deleting a tabbed saved search (`:rule delete <name>`,
  spec 11 §5.5) leaves a hole in the dense ordering. The TOML mirror
  is rewritten as part of the existing delete path.
- `CountTabs` is the unread-only sibling of `CountPinned` (spec 11
  §6.2). Implementation: for each tab `t`, run
  `Evaluate(ctx, t.Name, force=false)` to obtain the matched ID set
  (cached by spec 11's TTL), then call a new helper
  `store.CountUnreadByIDs(ctx, accountID, ids)` — one indexed query
  over the primary-key set returning the unread count. The
  user-supplied tab pattern is **never modified**; this avoids the
  semantic-drift trap of AND'ing `~U` into a pattern that may itself
  reference read state (e.g., `~U | ~F`). Total cost per refresh:
  O(tabs) cached pattern evaluations + O(tabs) point queries. With
  the spec 11 default 60s cache TTL, refreshes within a sync window
  are nearly free.
- For tabs ≥ 5 the per-tab evaluations run **in parallel** through a
  bounded `errgroup` (concurrency = min(tabs, 5)) to keep the cold
  refresh within budget (§8). Each goroutine reads only `Manager`'s
  cache and the store; the store is safe under concurrent reads
  (spec 02 §3 WAL invariant). This introduces
  `golang.org/x/sync/errgroup` as a new direct import in
  `internal/savedsearch/` — `golang.org/x/sync` is already pulled
  transitively by Bubble Tea's deps (`go.sum` lists it), so no new
  module is added; only a new direct import. No CLAUDE.md §1 stack
  invariant is affected.
- `CountTabs` does NOT apply `ExcludeMuted`. Muted threads are
  hidden in normal folder views (spec 19 §5.3) but the badge counts
  what the underlying pattern matches; if the user's tab pattern
  selects muted threads, the badge reflects them, consistent with
  `:filter` / search (spec 21 §5).
- The UI-side `SavedSearchService` interface in `internal/ui/app.go`
  (declared at line ~339; the `Deps.SavedSearchSvc` field at line
  ~197 holds an instance) gains corresponding methods:
  `Tabs(ctx) ([]SavedSearch, error)`, `PromoteTab(ctx, name) (int, error)`,
  `DemoteTab(ctx, name) error`, `ReorderTab(ctx, from, to int) error`,
  `RefreshTabCounts(ctx) (map[int64]int, error)`. The interface uses
  the UI-local `SavedSearch` type (value-mapped from
  `store.SavedSearch`) consistent with existing methods on that
  interface (`Reload`, `RefreshCounts`).

## 5. UI

### 5.1 Tab strip layout

Rendered above the list-pane header line whenever at least one tab
is configured for the active account. The strip is **always
visible** when tabs exist — including immediately after restart
(when `activeTab == -1` and Inbox or another regular folder is
selected). The visible cue is which tab segment, if any, carries
the active-bracket styling.

Rendering states:

- **Tabs configured, no tab active** (cold start, or while a
  regular folder is selected, or while `:filter --all` is active):
  every segment renders in inactive style; the strip is a quick
  visual reminder that tabs exist and that `]` will enter them.
- **Tabs configured, tab active**: the active segment carries the
  `theme.AccentEmphasis` bracket pair; selecting that tab focuses
  its content in the list pane.
- **No tabs configured**: strip is hidden, list pane unchanged
  from spec 04 / spec 21 layout.

Selecting a regular folder (Sent, Archive, etc.) does NOT hide the
strip; the active-tab highlight clears, but the strip remains as a
discoverable affordance. This matches Superhuman's "splits visible
across the inbox header even when viewing other folders" behaviour
and avoids the surprise of a strip that pops in and out as the user
clicks around.

Width = list pane width. Each tab segment:

```
 [Newsletters 12] [VIP 3] [Calendar] [Clients 47] 
```

Per-tab segment grammar:

```
 <space> [<name> <count>?] <space>
```

- `<name>` — saved search name. Truncated to 16 chars with `…` if
  longer. Original name preserved on hover/help.
- `<count>` — unread count. Hidden when zero. When the per-tab
  evaluation errored (e.g., compile failure on a stale pattern), the
  segment is rendered with the **`⚠` glyph** in place of the count —
  matching spec 11 §10's compile-error rendering in the sidebar. The
  glyph is consistent across both surfaces; users learn one symbol
  for one condition.
- Active tab: bracket pair rendered in `theme.AccentEmphasis`
  (Lip Gloss style, `internal/ui/theme.go`); name in
  `theme.TextEmphasis`.
- Inactive tabs: bracket pair `theme.TextSubtle`; name `theme.Text`.
- Tab with new mail since last focus: name prefixed with `•`
  (existing unread glyph from `[ui.indicator_unread]`).

The tab strip is **single-line and non-wrapping**. If the strip would
exceed the list-pane width, it scrolls horizontally to keep the
active tab visible (active tab is always the focal point). Overflow
on the right is rendered as a trailing `›`; on the left, `‹`. Spec 04
window-resize handler applies.

The strip does NOT add a separator row; the list-pane header
(`RECEIVED FROM SUBJECT` / spec 21 `RECEIVED FROM FOLDER SUBJECT`)
follows immediately below. Total vertical cost: 1 row.

### 5.2 Cycling and selection

Pane-scoped bindings, active **only when the list pane is focused**:

| Binding | Action |
|---------|--------|
| `]` | Cycle to next tab. Wraps at end. |
| `[` | Cycle to previous tab. Wraps at start. |
| `:tab <name>` | Jump to tab by name (cmd-bar, §6). |

`Tab` / `Shift+Tab` are NOT rebound — they remain `NextPane` /
`PrevPane` (spec 04 §5, `keys.go:156-157`) regardless of focus. We do
not overload them; the rationale is documented in §2.3.

`]` / `[` are list-pane-only. Viewer pane retains thread navigation
(`NavPrevInThread` / `NavNextInThread`, spec 05 §12) and calendar
pane retains day navigation (spec 12); both are pane-scoped already
and untouched by this spec. The new bindings register in the
list-pane dispatch branch only.

In `CommandMode` and `SearchMode` the cmd-bar / search input
consumes runes directly; `]` / `[` typed there are part of the
input string and do NOT cycle tabs. Cycle bindings fire only in
`NormalMode` with the list pane focused.

`1`–`3` are NOT rebound. They remain `FocusFolders` / `FocusList` /
`FocusViewer` (`keys.go:153-155`). Number-jump-to-tab-N is deferred —
introducing it requires reclaiming `1`–`3` away from pane focus,
which is a breaking change. A leader-chord (`g` then digit) may ship
in a follow-up.

When the user clicks (or `Enter`s) a saved search in the sidebar that
is also a tab, the UI focuses the corresponding tab in the strip.
This is the same code path as cycling and shares the per-tab state
restore (§5.6).

### 5.3 Interaction with `:filter`

When a tab is active and the user runs `:filter <pattern>`:

- The filter pattern is AND'd with the tab pattern. The compiled
  source becomes `(<tab pattern>) & (<filter pattern>)`. Implemented
  by passing `m.activeTabPattern` to `runFilterCmd` as a prefix that
  the dispatcher wraps in parentheses before the user's pattern.
- The status-bar hint says
  `filter: <user pattern> · in tab: <name> · matched N · ;d delete · :unfilter`.
- `:unfilter` (or `Esc`) restores the tab without the filter.
- `:filter --all <pattern>` (spec 21) ignores the tab — the filter
  runs cross-folder over the entire account. The list pane then
  shows the cross-folder result; tab strip remains visible but no
  tab is highlighted as active until the user clears the filter.
  Status hint:
  `filter: <pattern> --all · matched N (K folders) · ;d delete · :unfilter`.

This matches spec 21's `--all` widening convention exactly: tab is
the "default scope," `--all` overrides.

When a regular folder is selected (no tab active), `:filter` behaves
unchanged from spec 10 / spec 21.

### 5.4 Archive ⇒ message disappears from tab (emergent)

The roadmap §1.7 asserts: "When the user marks it done (E), it
disappears from all splits." This property is **emergent**, not a
new verb:

- Archive (`a`, spec 07) moves the message out of Inbox into the
  Archive folder.
- Tabs whose pattern includes `~m Inbox` (or excludes `~m Archive`,
  or implicitly is scoped via the user's pattern choices) no longer
  match the message after the archive completes.
- The next tab refresh (sync event, manual reload, or tab cycle)
  re-evaluates and the message is gone.

For the property to hold reliably, the spec **does not** mandate any
particular `~m` constraint — that's the user's choice. Documented in
the user-facing how-to recipe (§9) as a starter pattern; a tab
defined as `~f boss@` without `~m Inbox` will keep the message
visible after archive (which is the user's explicit choice). No
silent rewriting of patterns.

### 5.5 New-mail signal across non-active tabs

Each tab tracks `lastFocusedAt time.Time`. When `Manager.CountTabs`
runs after a `FolderSyncedEvent`, any tab whose unread count
increased since the last `lastFocusedAt` snapshot is flagged as
"new". The tab strip renders the `•` prefix on those tabs.

Flag clears on tab focus (`lastFocusedAt = now`).

Per-tab evaluation errors render the segment with `⚠` (consistent
with spec 11 §10) rather than crashing the strip. Error details are
in DEBUG logs only (subject data is never logged; see §11).

### 5.6 Per-tab state preservation

`ListModel` is single-instance (spec 04 §5). Per-tab state lives on
the parent `Model` as a slice indexed by tab position:

```go
// Model — add:
tabs           []store.SavedSearch         // ordered, from Manager.Tabs
activeTab      int                         // -1 = none, else 0..len(tabs)-1
tabState       []listSnapshot              // parallel to tabs
tabUnread      map[int64]int               // saved-search ID → unread count
tabLastFocused map[int64]time.Time         // for the • new-mail glyph
```

```go
type listSnapshot struct {
    cursor       int
    scrollOffset int
    cacheKey     string  // "savedsearch:<id>" — used to detect drift
    messages     []store.Message
    capturedAt   time.Time
}
```

On tab change (cycle, click, `:tab` jump):

1. Snapshot the current `ListModel` into `tabState[activeTab]`
   (cursor, offset, message slice).
2. Set `activeTab = newIndex`.
3. If `tabState[newIndex].messages` is non-empty AND
   `time.Since(capturedAt) < cacheTTL` (default 60s, reuses
   `[saved_search].cache_ttl`), restore those into the ListModel
   without a re-evaluation.
4. Otherwise dispatch `loadSavedSearchCmd(tab.Pattern)` (existing
   spec 11 path) and let the result populate the ListModel; on
   completion, re-snapshot `tabState[newIndex]`.

The `tabState` slice is **not persisted**; cursor positions reset on
restart. Persisting cursor positions across restart is out of scope
(no clear semantic for stale message IDs).

`activeTab` is also not persisted; startup defaults to `-1`. The
tab strip renders but no segment is highlighted as active until the
user cycles in (`]`) or jumps (`:tab <name>`).

Each `tabState[i].messages` is a slice header sharing its backing
array with whatever slice `loadSavedSearchCmd` produced for that
tab. When a tab is re-evaluated (TTL expiry or manual refresh), the
new ListModel slice points at a fresh backing array; the old
`tabState[i].messages` is replaced atomically inside the Bubble Tea
Update step. This bounds memory: at any time there is one backing
array per tab, never two. With 5 tabs × ~5000 messages × ~1KB envelope
≈ 25MB pinned, well within PRD §7's 200MB RSS budget.

When a saved search is edited (spec 11 §5.3), all `tabState` slots
referencing that saved search ID are invalidated (cleared messages,
forced reload on next focus). This avoids stale cursor on a pattern
that no longer matches.

### 5.7 Sidebar interaction

A saved search's sidebar entry is unchanged from spec 11 §5.1. A
small additional glyph `▦` (tab indicator) renders to the right of
the `☆` glyph when the saved search is also a tab:

```
▾ Saved Searches
  ☆▦ Newsletters    247
  ☆▦ VIP             3
  ☆ From CEO         12        # pinned but not a tab
  ▦ Notifications    8         # tab but not pinned (rendered without ☆)
```

This is purely informational; no new interaction. Selecting the
saved search from the sidebar focuses the tab if one exists, else
behaves as today.

## 6. Cmd-bar commands

| Command | Effect |
|---------|--------|
| `:tab` | Lists all tabs in cmd-bar status (e.g. `tabs: Newsletters, VIP, Calendar`). Read-only. |
| `:tab list` | Same as `:tab`. |
| `:tab add <name>` | Promote saved search `<name>` to the tab strip (append). Errors if `<name>` does not resolve to a saved search; suggests `:rule save <name>` in the error. |
| `:tab remove <name>` | Demote. Idempotent (no-op if not a tab). |
| `:tab move <name> <position>` | Reorder. `<position>` is 0-based. Errors out of range. |
| `:tab <name>` | Focus the tab named `<name>`. Errors if `<name>` is not a current tab. Tab-completion on names. |
| `:tab close` | Demote the currently active tab. Convenience for `:tab remove <active>`. Active tab becomes the next one to the right (or wraps). |

The dispatcher routes `:tab` based on the second token:
`add | remove | move | close | list`. Tokens that don't match are
treated as a tab name for jump (so `:tab Newsletters` works).

Why `:tab` and not `:tabs`? Singular matches `:rule`, `:filter`,
`:folder` (existing spec 11 / spec 18 conventions). Aliases not
needed in v1.

## 7. Edge cases

| Case | Behaviour |
|------|-----------|
| User has zero tabs configured | Tab strip is hidden. List pane renders normally (no extra row consumed). |
| User configures one tab | Strip shows the single tab. `]` and `[` no-op (logged at DEBUG, no error toast). |
| Saved search referenced by a tab is deleted | The saved-search row carries the `tab_order` value, so deleting the row removes the tab implicitly. `Manager.Delete` / `Manager.DeleteByName` then call `ReindexTabs` (§4) to keep the remaining tabs dense. `tabState` for the missing index is dropped; `activeTab` clamps to `min(activeTab, len(tabs)-1)` and falls back to `-1` when the strip becomes empty. |
| Saved search deleted, then a new one created with the same name | Tabs are bound by saved-search ID, not name. The new row gets a fresh ID with `tab_order = NULL`; the user must re-promote it explicitly. No silent re-tabbing. |
| Two `tab_order` values collide (theoretical, prevented by the partial UNIQUE index) | The migration's UNIQUE constraint rejects the write. `Promote` / `Reorder` write all shifted values within one transaction, so no transient duplicate exists. A would-be conflict surfaces as a constraint error from the store layer; the Manager wraps and returns it; UI shows a status-bar toast. |
| Saved search pattern is edited | `tabState[i].messages` for the matching ID is cleared on next render; tab badge re-evaluates on next sync. Active tab forces an immediate reload. |
| Pattern compile fails (stale pattern after edit error) | Tab segment renders with `⚠` in place of the count (matching spec 11 §10's compile-error glyph); selecting the tab shows the spec 11 §10 error UI (red status: "tab pattern: <error>") and the list pane shows the prior tab's content if any. |
| Tab name longer than 16 chars | Truncated with `…` in the strip. The user-facing reference (§9) recommends short tab names. |
| Tab strip wider than list pane | Horizontal scroll to keep active tab visible; `‹` / `›` overflow markers per §5.1. |
| User has 20+ tabs | No hard cap, but only 16 chars × 20 = 320 cols of strip — overflow markers handle it. Practical limit is screen width; we don't error. |
| User cycles with `]` while filter is active | Filter clears on tab change (same semantics as switching folders today). The tab's saved pattern resumes; status bar acknowledges "(cleared filter)" for 1.5s. |
| Tab refers to a saved search whose pattern uses `~m <foldername>` and the folder is renamed | Spec 18 (folder management) renames in-place; the pattern still resolves. Tab works. If the folder is deleted, the pattern compile errors at evaluate time → `⚠` segment → spec 11 §10 error UI on focus. |
| Muted threads in tab pattern | Included in counts and content (matches `:filter` / saved-search behaviour, spec 19 §4.4). The `🔕` glyph on the row indicates muted state. The tab is NOT a "normal folder view" in spec 19's sense; explicit pattern-defined views see muted by default. Documented in user reference. |
| `:filter` with tab active, then `:tab close` while filter is showing | Filter clears; the *next* tab to the right becomes active and reloads. |
| Sync arrives mid-cycle (user pressed `]`) | `loadSavedSearchCmd` returns the result for the new tab; sync update arrives separately and triggers a count refresh. No race because both are serialised through the Bubble Tea Update loop. |
| User has tabs but selects a regular folder | Tab strip remains visible (§5.1) but no segment carries the active-tab highlight. `]` activates tab 0 (and replaces the folder view with the tab's content); `[` activates tab 0 too from this state (per the cold-start rule below). Sidebar shows the saved-search entries normally. |
| First launch (post-migration), no tabs configured | Tab strip hidden. The seed defaults from spec 11 §7.3 (Unread, Flagged, From me) are still pinned-only, NOT tabbed. Users opt in via `:tab add`. |
| Restart with tabs configured | `activeTab` is **not** persisted across restart. Startup defaults to `activeTab = -1`. Tab strip renders (§5.1) with every segment in inactive style. The user explicitly cycles into the strip with `]` (which selects tab 0) or jumps with `:tab <name>`. Per-tab cursor / scroll state is also not persisted (§5.6). |
| Offline at startup with tabs configured | Tab strip renders; pattern evaluation runs against the local store (spec 02). No Graph fallback. Counts may be stale relative to the server but are consistent with what the user sees in folders (folders also draw from local cache offline, spec 03). When the network returns and `FolderSyncedEvent` fires, counts refresh per §5.5. |
| Tab pattern uses `~m <folder>` and that folder is not yet locally synced | Pattern compiles; match count reflects whatever the local store has (often zero on cold start). Badge updates on the next `FolderSyncedEvent` for the folder. Same behaviour as a regular saved search (spec 11 §10). |
| `:filter --all` active, then user presses `]` / `[` | Cycle clears the `--all` filter (same semantics as switching folders today, spec 21 §3.2) and activates the next/prev tab. Status hint flashes "(cleared filter)" for 1.5s. |
| `]` / `[` pressed when `activeTab == -1` (cold start, no tab focused yet) | Both keys activate **tab 0** (leftmost). The "wrap to last" interpretation is rejected — from no-active-tab, the user wants the strip to "start"; landing on the rightmost tab is surprising. Coded as: `if activeTab == -1 { activeTab = 0 } else if next { activeTab = (activeTab + 1) % N } else { activeTab = (activeTab - 1 + N) % N }`. |
| User typing in `:filter` cmd-bar (CommandMode) and types `]` or `[` | The cmd-bar input field consumes the rune as part of the pattern. Cycle bindings do NOT fire — they are NormalMode-only (§5.2). |

## 8. Performance

Budget: tab cycle (`]` / `[`) MUST feel instantaneous. The cached path
(§5.6) is in-memory state restoration only — no DB or Graph call.

| Surface | Budget | How met |
|---------|--------|---------|
| Tab cycle, cached state | <16ms p95 | In-memory snapshot/restore. The snapshot stores a *shallow copy of the slice header* — same backing array, fresh len/cap — so neither snapshot nor restore copies messages. ListModel never mutates message rows in place (it replaces the slice via `SetMessages`), so sharing the backing array is safe. Bench: `BenchmarkTabCycleCached` in `internal/ui/`. |
| Tab cycle, cache miss (60s TTL exceeded) | <100ms p95 | Reuses the `loadSavedSearchCmd` path, which is the existing saved-search evaluate path. Same budget as folder switch (PRD §7). Bench: `BenchmarkTabCycleEvaluate` in `internal/savedsearch/`. |
| `Manager.CountTabs` for 5 tabs over 100k messages, **cold (cache empty)** | <200ms p95 | Five `Evaluate` calls run in parallel via a bounded errgroup (§4) at concurrency 5. Per-call budget is the spec 02 `Search(q, limit=50)` <100ms p95; the `errgroup.Wait()` total is bounded by the slowest call plus a small fan-out cost. Bench: `BenchmarkCountTabs5x100k_Cold` in `internal/savedsearch/`. |
| `Manager.CountTabs` for 5 tabs, **warm (TTL hit)** | <20ms p95 | Cache returns precomputed ID sets; only `store.CountUnreadByIDs` runs (one indexed query per tab). Bench: `BenchmarkCountTabs5x100k_Warm`. |
| `Manager.CountTabs` for 20 tabs, cold | <500ms p95 | Concurrency capped at 5 (the same errgroup); 4 sequential rounds at <100ms each. Bench: `BenchmarkCountTabs20x100k_Cold`. |
| Tab strip render | <2ms p95 | Lipgloss layout over ≤20 short strings. Bench: `BenchmarkRenderTabStrip`. |

If `BenchmarkCountTabs5x100k` regresses >50% in CI, the benchmark
fails and blocks merge (§5.6 of CLAUDE.md gates).

No new database query path is hot beyond what spec 11 already
exercises. The new partial index `idx_saved_searches_tab_order` is
narrow (account_id, tab_order) and only touched on `Manager.Tabs`
reads, which are O(tabs) and called once per UI tick when a tab is
active.

## 9. CLI mode

```sh
# List tabs.
inkwell tab list
inkwell tab list --output json

# Promote / demote.
inkwell tab add Newsletters
inkwell tab add 'VIP'                  # quoted name
inkwell tab remove Newsletters

# Reorder.
inkwell tab move Newsletters 0         # to leftmost
```

The CLI does NOT introduce `inkwell tab eval` — `inkwell rule eval`
(spec 11 §8) already evaluates a saved search by name; whether it is
a tab or not adds nothing to that surface. `inkwell tab list` JSON
output includes both `matched` and `unread` per row so a script can
read counts without a separate command:

```json
{
  "tabs": [
    {"name": "Newsletters", "tab_order": 0, "matched": 247, "unread": 12},
    {"name": "VIP",         "tab_order": 1, "matched": 18,  "unread": 3}
  ]
}
```

The CLI does NOT introduce a new DB code path — it shells through
`Manager.Tabs` / `Promote` / `Demote` / `Reorder` / `CountTabs`.

## 10. Configuration

This spec adds a `[tabs]` section to `docs/CONFIG.md`.

| Key | Default | Used in § |
|-----|---------|-----------|
| `tabs.enabled` | `true` | If `false`, the tab strip is forcibly hidden even when rows exist (escape hatch for users who configured tabs and then changed their mind). Tabs persist; this is rendering-only. |
| `tabs.show_zero_count` | `false` | When `true`, render `[Name 0]` instead of `[Name]` for tabs with no unread. |
| `tabs.max_name_width` | `16` | Per-tab name truncation width. Min 4. |
| `tabs.cycle_wraps` | `true` | If `false`, `]` at last tab and `[` at first tab no-op instead of wrapping. |

`bindings.next_tab` and `bindings.prev_tab` are added to the
`BindingOverrides` struct in `internal/ui/keys.go` (lines 14-50) with
defaults `]` / `[`. Validation rejects empty strings (spec 04 §17
invariant). The TOML keys (`bindings.next_tab`, `bindings.prev_tab`)
decode to Go fields `NextTab`, `PrevTab` via the existing burntsushi
case mapping; this matches the convention used by every other
binding in the struct.

No config keys move; no defaults change for any existing key.

## 11. Logging and redaction

Tab cycle / promote / demote events are logged at `INFO` with the
saved-search **ID** only (e.g., `tab.focused id=42 order=2`). Tab
**names** are NOT included as INFO-level slog attributes: a
user-chosen name may carry PII (`Boss emails`, `Recruiter spam from
<person>`, `Health stuff`). When a name is needed in the log line at
all, it is logged at `DEBUG` only.

This is enforced **at the call site**, not by `internal/log/redact.go`.
The current redactor (`SensitiveKeys` in `redact.go:21-41`) does NOT
treat saved-search names, folder names, or tab names as sensitive —
it covers tokens, bodies, content, and (via `SubjectIsSensitiveAtLevel`)
subjects. Adding a new "name = sensitive at INFO" rule to the
redactor would require a flag day across every existing log site that
emits a folder or saved-search name; that change is out of scope for
this spec. Instead, this spec's new log sites simply do not pass the
name as an INFO attribute — same approach taken by the spec 11
saved-search log sites today.

Tab patterns are NOT logged at any level (consistent with saved-search
patterns, spec 11). A pattern eval failure logs the compile error
class but not the source.

A regression test for the new log sites lives in
`internal/savedsearch/manager_test.go` —
`TestPromoteDoesNotLogName` — checking that `INFO`-level captured
output contains the ID and order but not the raw name. A parallel
test `TestDemoteDoesNotLogName` covers the demote path.

## 12. Definition of done

- [ ] Migration `011_tab_order.sql` applies cleanly on a fresh DB and
      on a v0.49.x DB (the spec-21 release line). `tab_order` is
      `NULL` for all pre-migration rows.
- [ ] `store.SavedSearch.TabOrder *int` field exists; serialised as
      NULL/integer in SQLite. `PutSavedSearch` does NOT touch
      `tab_order`. `ListSavedSearches` includes the field.
- [ ] `store.ListTabs`, `store.SetTabOrder`, `store.ReindexTabs`
      implemented with unit tests covering: empty list, single tab,
      reorder, demote, dense reindex after gaps.
- [ ] `Manager.Tabs`, `Manager.Promote`, `Manager.Demote`,
      `Manager.Reorder`, `Manager.CountTabs` implemented; TOML mirror
      written on every mutation; `tab_order` field round-trips
      through TOML.
- [ ] `:tab` cmd-bar dispatcher: `add`, `remove`, `move`, `close`,
      `list`, and `<name>` (jump). Tab-completion on names where the
      cmd-bar already supports completion (spec 04 §11). Unknown name
      surfaces a friendly error: `tab: no saved search named "<x>";
      run :rule save <x> first`.
- [ ] Pane-scoped key bindings `]` / `[` for next / prev tab; bind
      only when list pane is focused; documented in `BindingOverrides`
      with defaults; config validation rejects empty overrides.
- [ ] Tab strip renders above the list pane when any tabs exist and
      a tab is active (or the list pane has no folder selected).
      Hidden when a regular folder is selected.
- [ ] Active-tab styling, inactive-tab styling, count rendering,
      `•` new-mail glyph, `⚠` error glyph (matching spec 11 §10),
      horizontal-scroll overflow
      with `‹` / `›`.
- [ ] Per-tab state preserved across cycles (cursor, scroll, message
      slice) via `tabState []listSnapshot`. Cache TTL reuses
      `[saved_search].cache_ttl`. Edit-saved-search invalidates the
      relevant snapshot.
- [ ] Sidebar saved-search entries that are also tabs render with the
      `▦` indicator. Selecting a sidebar saved search that is a tab
      focuses the tab.
- [ ] `:filter` while a tab is active AND's the user pattern with the
      tab pattern; `:filter --all` ignores the tab (spec 21
      consistency). Status bar hint reflects which scope is active.
- [ ] `Manager.CountTabs` integrates with the existing sync-event
      hook (the same site as `RefreshCounts` / `CountPinned` for
      pinned saved searches). Counts refresh on `FolderSyncedEvent`.
- [ ] CLI `inkwell tab list / add / remove / move` per §9. (No
      `tab eval` — `inkwell rule eval` already covers single-name
      evaluation; spec 24 does not duplicate it.) `tab list --output
      json` returns the envelope shape from §9 with `matched` and
      `unread` per row.
- [ ] `Manager.Delete` and `Manager.DeleteByName` modified to call
      `store.ReindexTabs` after a successful row delete; TOML mirror
      rewritten as part of the existing delete path.
- [ ] `tabs.max_name_width` validated with min = 4 in
      `internal/config/validate.go`; values below the minimum surface
      a typed config error at startup.
- [ ] `tabs.enabled = false` forcibly hides the strip even when tabs
      exist (rendering-only escape hatch); `tabs.show_zero_count =
      true` renders `[Name 0]` for tabs with zero unread;
      `tabs.cycle_wraps = false` makes `]` at the last tab and `[`
      at the first tab no-op (no wrap).
- [ ] `:tab close` demotes the active tab and selects the
      next-to-the-right (or wraps to leftmost; if the strip becomes
      empty, `activeTab = -1` and the strip hides).
- [ ] The `SavedSearchService` interface in `internal/ui/app.go`
      gains the methods listed in §4 ("UI-side `SavedSearchService`
      interface"); the existing `refreshSavedSearchCountsCmd` site
      in `internal/ui/app.go` is extended (or a sibling
      `refreshTabCountsCmd` added) to invoke `RefreshTabCounts` on
      `FolderSyncedEvent`.
- [ ] `THREAT_MODEL.md` row added: "Tab name as PII vector — mitigated
      by call-site DEBUG-only logging policy (§11) and
      `TestPromoteDoesNotLogName` regression."
- [ ] Tests:
  - store unit: `TestListTabsOrdered`, `TestSetTabOrderNullDemotes`,
    `TestReindexTabsDense`, `TestSavedSearchDeleteReindexesTabs`,
    `TestPromoteUniquePartialIndex` (partial UNIQUE rejects duplicate
    `tab_order` per account; uses direct `store.SetTabOrder` calls
    since the `Promote` path through `ApplyTabOrder` would never
    naturally produce a duplicate), `TestSeedDefaultsLeavesTabOrderNull`
    (spec 11 seeds set `pinned`, never `tab_order`).
  - manager unit: `TestPromoteIdempotent`, `TestDemoteUnknownNoOp`,
    `TestReorderOutOfRangeError`, `TestCountTabsUnreadOnly`
    (verifies user pattern is NOT mutated; unread is filtered via
    `CountUnreadByIDs`), `TestCountTabsParallelBoundedConcurrency`
    (with 20 tabs, ≤5 in-flight goroutines), `TestPromoteDoesNotLogName`
    (redaction), `TestDemoteDoesNotLogName`,
    `TestEditSavedSearchInvalidatesTabState` (invalidation hook fires
    on rename or repattern).
  - dispatch unit: `TestTabAddCmd`, `TestTabRemoveCmd`,
    `TestTabMoveCmd`, `TestTabCloseCmd`, `TestTabJumpByNameCmd`,
    `TestTabUnknownName`, `TestTabAddCompletionOnNames` (cmd-bar
    completion offers saved-search names).
  - dispatch e2e (`*_e2e_test.go`):
    `TestTabStripRendersWhenTabsExist` (visible bracket pair),
    `TestTabStripHiddenWhenNoTabs`, `TestTabStripHiddenOnRegularFolder`,
    `TestNextTabBindingCyclesActive` (active-tab style moves
    visibly), `TestPrevTabBindingCyclesBackward`,
    `TestBracketKeysInactiveInCommandMode` (typing `]` in `:filter`
    inserts the rune, does NOT cycle),
    `TestBracketKeysInactiveInViewerPane` (viewer thread nav
    unaffected), `TestTabSwitchPreservesCursor` (cursor on row 5 in
    tab A, switch to B, switch back, cursor still on row 5),
    `TestFilterWhileTabActiveScopesToTab` (status hint shows
    "in tab: <name>"), `TestFilterAllWhileTabActiveWidens`
    (status hint shows "(N folders)" not "in tab"),
    `TestCycleTabClearsActiveFilter` (`]` while `:filter --all`
    active → filter clears, next tab activates),
    `TestTabBadgeShowsUnreadCount` (badge increments after
    a synthetic `FolderSyncedEvent`),
    `TestTabBadgeNewMailGlyph` (`•` appears on non-active
    tab when count rises),
    `TestTabBadgeWarningGlyphOnCompileError`,
    `TestTabsDisabledHidesStrip` (config `tabs.enabled = false`,
    even with tabs configured, strip not rendered),
    `TestShowZeroCountRendersZero` (config `tabs.show_zero_count =
    true`, tab with 0 unread renders `[Name 0]`),
    `TestCycleWrapsFalseNoOpsAtEnd` (config `tabs.cycle_wraps =
    false`, `]` on last tab leaves `activeTab` unchanged),
    `TestTabListCmdShowsAllTabs` (`:tab` with no args lists names
    in the cmd-bar status line),
    `TestArchiveDropsMessageFromInboxScopedTab` (tab pattern
    `~m Inbox & ~f x@y`; archive a matching message; next refresh,
    message gone from tab — emergent property §5.4),
    `TestSidebarTabGlyphRendered` (`▦` appears next to `☆` for tabs),
    `TestSidebarSelectFocusesActiveTab`,
    `TestActiveTabResetsToMinusOneOnRestart` (model reload starts
    at `activeTab = -1`),
    `TestActiveTabClampsAfterDelete` (active tab is index 2; saved
    search at index 1 deleted; `activeTab` clamps to new index 1),
    `TestSyncAndCycleProduceCoherentState` (interleaved
    `FolderSyncedEvent` and `]` keypress sequence through `teatest`;
    asserts the final rendered frame shows both the cycle (active
    tab moved) and the sync (badge updated). Bubble Tea serialises
    Update calls so a true race is impossible — this test asserts
    the *visible ordering*, not the absence of a race.).
  - bench: `BenchmarkTabCycleCached`,
    `BenchmarkTabCycleEvaluate`, `BenchmarkCountTabs5x100k_Cold`,
    `BenchmarkCountTabs5x100k_Warm`, `BenchmarkCountTabs20x100k_Cold`,
    `BenchmarkRenderTabStrip` — all gated within budget per §8.
- [ ] User docs: `docs/user/reference.md` adds the `]` / `[`
      bindings and the `:tab` command family. `docs/user/how-to.md`
      gains a "Set up split inbox tabs" recipe with a starter
      pattern set (Newsletters, VIP, Receipts) and the explicit
      note that tab patterns including `~m Inbox` make archive
      remove the message from the tab.
- [ ] `docs/CONFIG.md` adds the `[tabs]` section with the four
      keys from §10 and mentions the two new
      `bindings.next_tab` / `bindings.prev_tab` overrides.

## 13. Cross-cutting checklist

- [ ] **Scopes:** none new. Reads from `saved_searches` (already
      Mail.Read scope reach via the underlying patterns); the tab
      strip itself never calls Graph.
- [ ] **Store reads/writes:** new column `saved_searches.tab_order`
      via migration 011; new helpers `ListTabs`, `SetTabOrder`,
      `ReindexTabs`. No new mutations on the messages or actions
      tables.
- [ ] **Graph endpoints:** none new. Tab strip is local. Pattern
      evaluation may hit `$search` per the existing spec 08 strategy
      logic, unchanged.
- [ ] **Offline:** fully functional offline against the local store —
      tabs, cycling, counts, promote/demote all run without network.
      Same as saved searches.
- [ ] **Undo:** Tab promote / demote / reorder are not in the action
      queue (they're config-like, not mailbox mutations). They are
      idempotent; users redo by reverse cmd. No `u` undo coverage,
      consistent with spec 11's saved-search CRUD.
- [ ] **User errors:** §7 edge-case table. `:tab add` with unknown
      name surfaces `tab: no saved search named "<x>"`; `:tab move`
      out of range surfaces `tab: position N out of range (have K)`.
- [ ] **Latency:** §8 perf budgets. New benchmarks gate CI.
- [ ] **Logs:** no new log sites that touch raw mail content. Tab
      mutations log saved-search **ID and order** at INFO; **name**
      is DEBUG-only (PII-adjacent — see §11). Patterns never logged.
      Redaction test: `TestPromoteDoesNotLogName`.
- [ ] **CLI:** §9 `inkwell tab` subcommands.
- [ ] **Tests:** §12 list.
- [ ] **Spec 17 review (security testing + CASA evidence):** No new
      external HTTP surface (tabs are local). No new SQL composition
      (existing saved-search SQL plus a new column read; the
      ReindexTabs UPDATE uses a literal column list, no
      user-controlled string interpolation). No token handling, no
      subprocess, no cryptographic primitive, no new third-party
      data flow. New persisted state: `saved_searches.tab_order`
      column — already inside the local cache file `mail.db`
      (mode 0600). One threat-model row to add to
      `docs/THREAT_MODEL.md`: "user promotes a saved search whose
      name carries PII; the tab strip must not leak it through
      INFO logs" → mitigated by the DEBUG-only name logging policy
      in §11 and the `TestPromoteDoesNotLogName` regression in §12.
      `docs/PRIVACY.md` does not change (no new data leaves the
      device).
- [ ] **Spec 11 consistency:** Saved searches remain the source of
      truth. `tab_order` is additive; the existing CRUD path
      (`Save`, `Delete`, `Edit`) is unchanged. TOML mirror gains
      one optional field; spec 11's divergence prompt continues to
      govern.
- [ ] **Spec 19 consistency:** Muted threads appear in tab counts
      and content (tabs are explicit pattern-defined views; spec 19
      hides muted only from "normal folder views"). Muted glyph
      `🔕` renders as today.
- [ ] **Spec 20 consistency:** Thread chord (`T`) operates on the
      focused message in the active tab. After a thread archive,
      the conversation drops out of the tab on next refresh —
      same emergent property as single-message archive (§5.4).
- [ ] **Spec 21 consistency:** `:filter` AND's with the active tab
      by default; `:filter --all` widens to the whole mailbox,
      ignoring the tab. Same `--all` convention as cross-folder
      bulk. Folder column from spec 21 still renders when a
      tab's pattern spans >1 folder (`m.list.folderNameByID` is
      populated by the existing `filterAppliedMsg` /
      saved-search-loaded handler).
- [ ] **Docs consistency sweep:** `docs/CONFIG.md` `[tabs]` section
      added — placement is between `[saved_search]` and `[bulk]`,
      keeping the saved-search-related sections adjacent;
      `docs/user/reference.md` `]` / `[` and `:tab` rows added
      (with the explicit note that `]` / `[` are list-pane-scoped
      and the existing viewer / calendar bindings are unchanged);
      `docs/user/how-to.md` "Split inbox tabs" recipe added;
      `docs/PRD.md` §10 inventory row updated;
      `docs/ROADMAP.md` §1.7 row updated to "shipped as spec 24"
      and the `Tab` / `Shift+Tab` claim in §1.7 prose corrected to
      `]` / `[` (matches what we shipped); this spec file written;
      `docs/plans/spec-24.md` created at ship time (CLAUDE.md §13
      mandatory). Optional follow-up: spec 11's status line says
      "Stub" but the Manager API is shipped — flag in plan note,
      side-PR to fix.
