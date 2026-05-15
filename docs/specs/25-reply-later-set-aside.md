# Spec 25 — Reply Later / Set Aside stacks

**Status:** Shipped.
**Shipped:** v0.53.0 (2026-05-07)
**Depends on:** Spec 02 (`messages.categories` JSON column +
`MessageQuery.Categories` filter — predicate widened to
case-insensitive in this spec), Spec 07 (`add_category` /
`remove_category` action types + bulk variants — §6.9, §6.10),
Spec 09 (`/$batch` executor; `batchExecute(extraParams=...)`
internal path already supports a category Param — this spec
exposes it via a new `BatchExecuteWithParams` public method),
Spec 11 (sidebar saved-searches adjacent block — visual precedent
for between-block placement),
Spec 15 (compose-reply, used by Focus & Reply mode), Spec 19 (virtual
sidebar entry precedent — `__muted__` sentinel ID; `MuteThread`
`KeyMap` precedent; `refreshMutedCountCmd` precedent), Spec 20 (thread
chord — `T <verb>` binding; `MessageIDsInConversation` helper), Spec 21
(cross-folder list rendering — `folderNameByID` map; confirm-modal
`across N folders` suffix).
**Blocks:** None. Inbox-philosophy pack (1.9 sender routing, 1.11
bundles) is independent.
**Estimated effort:** 1.5 days.

---

## 1. Goal

Two adjacent verbs that the native Outlook clients handle poorly and
that HEY proved are first-class workflow primitives:

- **Reply Later** — "I owe this person a reply, but not now." A queue
  of writing-debt I can serialise through later.
- **Set Aside** — "I want to keep this handy without committing to a
  reply." A reference shelf for door codes, addresses, hotel
  confirmations, the SKU I might re-buy, the line item from a contract.

Each is a **stack**: a virtual queue surfaced as a sidebar entry, a
toggle keybinding to add/remove the focused message, and (for Reply
Later) a "Focus & Reply" batch mode that walks the queue one message
at a time. Stacks are stored as Microsoft Graph **categories** on the
message resource, so state round-trips through the server and across
clients (the user can see "Inkwell/ReplyLater" in Outlook web; that's
intentional cross-device sync).

The two stacks are deliberately separate (HEY's design call): the
*verbs* are different. Conflating them into a generic "later" pile
loses the asymmetry between commitment-to-write and reference-shelf.

### 1.1 Terminology

- **Stack** — the user-facing term for either Reply Later or Set
  Aside as a whole (`Reply Later stack`, `Set Aside stack`). Used
  in toasts, sidebar labels, doc strings, and the spec title.
- **Queue** — used only in the context of the Focus & Reply
  mode, where messages are walked one at a time
  (`focusQueueIDs`, "queue cleared", "the queue"). Internal to
  the focus-mode loop; not a user-facing label for the stacks.
- **Pile** — HEY's visual term, kept only in §2 prior-art prose
  for accuracy.
- **Action queue** — the existing spec 07 action-dispatching
  machinery; unrelated to the "queue" sense above. Refer to as
  "the action queue" when there's any ambiguity.

### 1.2 What does NOT change

- Categories storage, sync, and PATCH path are untouched. This spec
  reuses `ActionAddCategory` / `ActionRemoveCategory` from spec 07
  and the bulk variants from spec 09 — no new action type, no new
  Graph endpoint, no new schema migration.
- The `~G` predicate in the pattern engine (spec 08) already matches
  category strings; users could roll a saved-search equivalent today.
  This spec adds the **first-class** UX layer: dedicated keybindings,
  sidebar entries, a Focus mode, and CLI subcommands.
- Categories are not removed from a message when it is deleted, moved,
  or muted — Reply-Later/Set-Aside state outlives folder transitions.
  This is the existing categories behaviour; we don't add any
  cleanup hook.
- Mute (spec 19) and Reply-Later/Set-Aside are orthogonal: a message
  can simultaneously be muted **and** in a stack. The stack views
  ignore mute (see §5.4 — "intentional view, like search").

## 2. Prior art

### 2.1 Terminal clients

- **mutt / neomutt** — no first-class "later" pile. Approximations:
  flag (`F`) for "look at again"; tag-based macros over notmuch
  (`+reply-later`) plus a virtual mailbox for the saved query. No
  serialised "go through the queue" mode; the user opens the virtual
  mailbox and works manually.
- **aerc** — `:tag +reply-later` against notmuch / Gmail labels;
  `:cf <query>` opens a buffer of tagged messages. No Focus mode; no
  built-in counter overlay.
- **alot (notmuch)** — first-class tag UI; the established convention
  is `tag:reply` or `tag:to-reply`. Threads are the unit; the queue
  is just "switch to the `tag:to-reply` virtual folder."

### 2.2 Web / desktop clients

- **HEY** — the canonical reference. Two persistent piles at the
  bottom of the screen ("Reply Later" and "Set Aside"); clicking a
  pile fans it out as a card overlay. The piles are first-class verbs
  alongside Reply and Archive — buttons in the message toolbar.
  **Focus & Reply** mode stitches every Reply-Later message into a
  single scrolling page with a reply box next to each — explicitly a
  batch-process mode for clearing the queue. The toggle key is
  context-menu / button only (no global keybinding); `I` returns a
  message to the Imbox (untoggle).
- **Superhuman** — no native "Reply Later" pile. Closest
  approximations: **Reminders** (timed snooze with `H`) and **Split
  Inbox** (top panels driven by saved queries). A user wanting a
  reply-later workflow rolls their own split with `is:starred` or a
  custom query.
- **Gmail** — Snooze (`b`) + Star (`s`) + Multiple Inboxes (any
  `is:starred` / custom-search-driven panel above the main list).
  Star + saved search is the closest to inkwell's design.
- **Fastmail** — "Pin" sticks a message to the top of its folder;
  Snooze removes from inbox until time. No cross-folder pin queue
  per se.
- **Outlook web** — Pin (per-folder, sticks to top of inbox), flag
  for follow-up (To Do app integration), categories. Pin is
  per-folder; categories are global. Outlook does **not** have a
  dedicated "queue" overlay for either.
- **Apple Mail (Ventura+)** — Remind Me Later: 1h / Tonight /
  Tomorrow / custom; reminded messages stay in inbox at the top with
  a yellow banner. Smart mailbox lists all reminded messages.
- **Thunderbird** — neither concept first-class. Users build saved
  searches over `is:starred` or per-tag queries.

### 2.3 Design decision

Inkwell follows the **HEY two-stack model** (the user-visible mental
model) on top of the **alot/notmuch tag-as-virtual-folder
implementation** (the data path) backed by **Microsoft Graph
categories** (the storage primitive).

Concretely:

- **Two reserved category names**: `Inkwell/ReplyLater` and
  `Inkwell/SetAside`. Slash-prefixed namespacing keeps them visually
  distinct from user-defined categories in Outlook web. They are
  treated as inkwell-managed; users editing them in Outlook directly
  is supported but documented as advanced.
- **One toggle keybinding per stack**: `L` (Reply Later) and `S` (Set
  Aside). Both unused capitals in `DefaultKeyMap`; both pane-scoped to
  list and viewer. Same UX shape as `M` (mute) — instant toggle, no
  confirmation. Note: `findDuplicateBinding`
  (`internal/ui/keys.go:289`) currently excludes movement keys
  (`j`/`k`/`h`/`l`) from its scan because lower/upper are distinct
  strings to Bubble Tea. `L` (capital) does not collide with `l`
  (lowercase Right). A user who rebinds `[bindings].right = "L"` would
  silently double-bind, but movement-key remapping is already
  unmonitored by spec 04; we don't expand the scan in this spec.
- **Two sidebar virtual entries**: `__reply_later__` and
  `__set_aside__`, mirroring spec 19's `__muted__` sentinel pattern.
  Visible only when the count is ≥1.
- **Focus & Reply mode** (HEY's flagship): a `:focus` cmd-bar command
  that walks the Reply Later queue, opening the compose-reply UI
  (spec 15) for each message in turn. `Esc` exits the mode at any
  point. `:focus` is the inkwell name for HEY's mode; the verb-form
  is kept short.
- **Thread variants via spec 20 chord**: `T l` adds the entire thread
  to Reply Later, `T s` to Set Aside. Removing the thread from a
  stack is `T L` / `T S` (capital chord-verb removes). The lower /
  upper symmetry mirrors `T r` (mark-read) / `T R` (mark-unread) in
  spec 20 §3 — lowercase is the doing verb, uppercase is the
  inverse. The hint string in spec 20's chord-pending status bar
  (`thread: r/R/f/F/d/D/a/m  esc cancel`) is updated to
  `thread: r/R/f/F/d/D/a/m/l/L/s/S  esc cancel`. The string remains a
  hardcoded constant (no generation refactor); the spec 20 hint stays
  hardcoded and is edited once.
- **Roadmap deviation — `L` for `R`**: roadmap 1.10 suggested
  capital `R` for "Reply Later"; `R` is already bound (MarkUnread in
  list, Reply-All in viewer). We deviate to `L` (mnemonic:
  **L**ater). The roadmap's `S` for "Set Aside" is preserved.
  Documented in §5.1.

## 3. Schema

**No migration required.** The `messages.categories` JSON column
(migration 001) and the index-free predicate path
(`buildListSQL` `EXISTS (SELECT 1 FROM json_each(categories)
WHERE value IN (?))` — `internal/store/messages.go`) already support
this spec end-to-end.

### 3.1 Reserved category names

```go
// internal/store/categories.go (new file — small, contains only the
// reserved-name constants and a helper).

// Reserved category names for inkwell-managed stacks. The "Inkwell/"
// prefix namespaces them away from user-defined categories. The
// strings round-trip to Microsoft Graph and are visible in Outlook
// web as ordinary categories — this is intentional (cross-device
// sync) and documented in docs/user/explanation.md.
const (
    CategoryReplyLater = "Inkwell/ReplyLater"
    CategorySetAside   = "Inkwell/SetAside"
)

// IsInkwellCategory reports whether s is one of the reserved names.
// Comparison is case-insensitive (strings.EqualFold) — the local
// dedup helper appendCategory in internal/action/types.go already
// uses EqualFold, so a category previously stored with non-canonical
// casing (e.g. user-tagged "inkwell/replylater" in Outlook web) is
// recognised as the same stack here. Used by the renderer to draw
// the stack glyph and by the toggle path to detect membership.
func IsInkwellCategory(s string) bool {
    return strings.EqualFold(s, CategoryReplyLater) ||
        strings.EqualFold(s, CategorySetAside)
}

// IsInCategory reports whether the message's Categories slice
// contains a case-insensitive match for cat. Single source of truth
// for membership checks in the toggle handler, the indicator
// renderer, the stack-view filter, and the focus-mode pre-fetch.
func IsInCategory(cats []string, cat string) bool {
    for _, c := range cats {
        if strings.EqualFold(c, cat) {
            return true
        }
    }
    return false
}
```

`CountMessagesInCategory` and `ListMessagesInCategory` (§4.2) match
the category column case-insensitively (`COLLATE NOCASE` on the
`json_each.value`). This lets a message tagged `inkwell/replylater`
in Outlook web still appear in the inkwell stack view. Worked
example: `EXISTS (SELECT 1 FROM json_each(categories) WHERE value =
? COLLATE NOCASE)` where the bind value is the canonical
`Inkwell/ReplyLater`.

**The existing `MessageQuery.Categories` predicate**
(`buildListSQL` in `internal/store/messages.go:408`) currently uses
case-sensitive `value IN (?,?)`. We update it to case-insensitive
`value = ? COLLATE NOCASE` (one bind per category, OR'd) in the
same change. Reasoning: the predicate is used by saved searches
and CLI `inkwell messages --category` (Outlook category names are
case-preserving but case-insensitive on the server; matching the
server semantics is correct). This is a small spec 02 / spec 11
adjacent change documented in DoD §10.1. Existing callers are
unchanged at the API level; semantics widen from case-sensitive to
case-insensitive (no caller relies on the strict semantics —
verified by reading the `Categories` field's call sites).

**`json_each` + `COLLATE NOCASE` viability** in
`modernc.org/sqlite`: the driver implements full SQLite semantics
including `COLLATE NOCASE` over text values, and `json_each.value`
is text for string entries. An integration test
(`TestJSONEachCollateNocaseRoundtrip`) confirms behaviour before
the perf benchmarks claim 10ms.

The constants live in `store` (not `ui`) so the action layer, CLI,
and pattern engine can all share them.

### 3.2 Why categories, not a new table

A local-only `reply_later_messages` table (mirroring
`muted_conversations`) was considered and rejected:

| Question                                 | Local table       | Graph categories (chosen) |
| ---------------------------------------- | ----------------- | ------------------------- |
| Survives device switch?                  | No                | **Yes**                   |
| Works while Graph is unreachable?        | Yes               | Yes (action queue)        |
| Uses an existing action-queue path?      | No (new actions)  | **Yes** (`add_category`)  |
| Visible in Outlook web?                  | No                | Yes (acceptable)          |
| Schema migration?                        | Yes               | **No**                    |
| Pattern-engine queryable today (`~G`)?   | No                | **Yes**                   |

Roadmap 1.10 explicitly calls out "Backed by Graph categories —
independent." We honour that.

## 4. Store / Action plumbing

### 4.1 Existing primitives that suffice

The following already exist and need no change:

- `MessageQuery.Categories []string` (any-of match) — used by the
  virtual-folder loaders in §5.4.
- `Executor.AddCategory(ctx, accID, msgID, cat string) error` —
  used by `L` / `S` toggle to add.
- `Executor.RemoveCategory(ctx, accID, msgID, cat string) error` —
  used by `L` / `S` toggle to remove.
- `Executor.BulkAddCategory(ctx, accID, msgIDs []string, cat string)
  ([]BatchResult, error)` — used by thread variants.
- `Executor.BulkRemoveCategory(ctx, accID, msgIDs []string, cat
  string) ([]BatchResult, error)` — used by thread variants.

Spec 07 §6.9/§6.10 documents these as idempotent at the local-apply
layer (case-insensitive dedup; supplying an existing category does
not mutate the stored array). However, `Executor.AddCategory`
unconditionally enqueues a row — the dedup happens inside
`applyLocalMessage`. A redundant `L L` rapid double-press will
therefore enqueue two actions and dispatch two PATCHes (the second
being a no-op for the categories array but still a network round
trip).

**Toggle pre-check:** to avoid the redundant PATCH, the `L` / `S`
toggle handler **reads the focused message's `Categories` slice
first** via `IsInCategory(...)`. The handler dispatches `AddCategory`
only when the membership check returns false; otherwise it dispatches
`RemoveCategory`. The pre-check is on the in-model slice — if the
slice is stale (sync arrived between read and dispatch), the action-
queue dedup still protects correctness, just with a redundant PATCH.
The single-press case is the only one we optimise. Document the
edge: rapid double-press will enqueue both actions; the user sees one
toast, the second action is dispatched but has no visible effect.

### 4.2 New store helpers

Two helpers in the new `internal/store/categories.go` next to the
reserved-name constants. Both helpers are **hand-written SQL** — they
do NOT call `ListMessages` / `buildListSQL`. Reason: `MessageQuery`
has no folder-exclusion field today (only `FolderID` for inclusion),
and we don't want to extend `MessageQuery` for two callers; mirroring
the spec 20 `MessageIDsInConversation` precedent (also a hand-written
SQL helper that joins `folders` for well-known exclusion) is
cheaper.

```go
// CountMessagesInCategory returns the count of distinct messages
// carrying the given category for the account. Used to drive the
// sidebar count badge — the stack entries are hidden when the count
// is zero, so the count is consulted on every list reload.
//
// Excludes muted threads? NO — same precedent as search and saved
// searches: stack views are intentional (the user opened them) and
// muted state is orthogonal to the verb. Spec 19 §4.3 ("intentional
// search includes muted") is the controlling precedent.
//
// Excludes well-known folders? YES — Drafts, Deleted Items, and Junk
// are excluded (matches MessageIDsInConversation default). Reasoning:
// a message dragged to Junk by a server-side rule should not pollute
// the queue count; a draft auto-saved in Drafts should not appear in
// Reply Later (Drafts is the user's outgoing-pending state, not the
// reply-later queue). Sent items are NOT excluded — a user who sent
// a follow-up may want to circle back.
CountMessagesInCategory(ctx context.Context, accountID int64,
    category string) (int, error)

// ListMessagesInCategory returns messages tagged with the given
// category for the account, ordered by received_at DESC, capped at
// limit. Excludes Drafts/Trash/Junk (same exclusion as the count
// helper); does NOT exclude muted threads.
ListMessagesInCategory(ctx context.Context, accountID int64,
    category string, limit int) ([]Message, error)
```

SQL shape (`COLLATE NOCASE` on the json-each match handles the
casing edge case — see §3.1):

```sql
-- CountMessagesInCategory
SELECT COUNT(*)
FROM messages m
LEFT JOIN folders f ON f.id = m.folder_id
WHERE m.account_id = ?
  AND EXISTS (
    SELECT 1 FROM json_each(m.categories)
    WHERE value = ? COLLATE NOCASE
  )
  AND (f.well_known_name IS NULL
       OR f.well_known_name NOT IN ('drafts', 'deleteditems', 'junkemail'));

-- ListMessagesInCategory: same WHERE; SELECT messageColumns;
-- ORDER BY m.received_at DESC LIMIT ?
```

### 4.3 Why no new action type

Both stacks are pure category mutations. Routing through the existing
`add_category` / `remove_category` actions:

- Inherits the action queue's optimistic-apply / rollback behaviour.
- Inherits the inverse (`add_category` ↔ `remove_category`), so `u`
  (undo) un-stacks a message.
- Inherits the existing `internal/log/redact.go` redaction discipline
  for category names (categories are not PII, but the redaction layer
  treats them as opaque strings — spec 17 alignment).

Adding a `reply_later_set` action type would duplicate the entire
queue/inverse/dispatch path for no semantic gain. Don't.

## 5. UI

### 5.1 Toggle keys

| Key (list pane or viewer pane focused) | Action                                      |
| --------------------------------------- | ------------------------------------------- |
| `L`                                     | Toggle `Inkwell/ReplyLater` on focused message |
| `S`                                     | Toggle `Inkwell/SetAside` on focused message   |

Both keys are currently unbound. `L`/`S` (capitals) follow the spec
19 convention that capitals signal "this puts the message into a
queue / outside the normal flow" (compare `M` mute, `U`
unsubscribe). `findDuplicateBinding` (`internal/ui/keys.go`) already
checks all distinct binding fields; both new fields are added to the
scan.

**Pane scope:** active in list pane and viewer pane. Not active in
the folders sidebar (no focused message).

**Dispatch:** the toggle reads the focused message's `Categories`
slice; if it contains the reserved name, dispatch
`Executor.RemoveCategory`; otherwise dispatch `Executor.AddCategory`.
The check is case-insensitive (matches `appendCategory` in
`internal/action/types.go`).

**KeyMap changes:**

```go
// internal/ui/keys.go

// BindingOverrides struct — add:
ReplyLaterToggle string
SetAsideToggle   string

// KeyMap struct — add:
ReplyLaterToggle key.Binding
SetAsideToggle   key.Binding
```

Defaults:

```go
ReplyLaterToggle: key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "reply later")),
SetAsideToggle:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "set aside")),
```

Wire through `ApplyBindingOverrides` and `findDuplicateBinding` —
mirror the `MuteThread` pattern from spec 19. Add entries to the
`checks` slice in `findDuplicateBinding`:

```go
{"reply_later_toggle", km.ReplyLaterToggle},
{"set_aside_toggle",   km.SetAsideToggle},
```

### 5.2 List-row indicator

The list row in `internal/ui/panes.go` renders three independent
glyph slots: **invite** (`📅` calendar / `🔕` mute), **flag** (`⚑`),
and **attachment** (`📎`). They occupy distinct columns and are
already orthogonal. Spec 25 adds Reply Later (`↩`) and Set Aside
(`📌`) into the **invite slot only** — they share that column with
calendar and mute, leaving flag and attachment unaffected.

| Glyph | Slot   | State                                                                | Config key                   | ASCII fallback |
| ----- | ------ | -------------------------------------------------------------------- | ---------------------------- | -------------- |
| `↩`   | invite | Message is in `Inkwell/ReplyLater` (case-insensitive match)          | `[ui].reply_later_indicator` | `R`            |
| `📌`  | invite | Message is in `Inkwell/SetAside`                                     | `[ui].set_aside_indicator`   | `P`            |
| `🔕`  | invite | Currently rendered only in `__muted__` view (spec 19 §5.2 unchanged) | `[ui].mute_indicator`        | `m`            |
| `📅`  | invite | Existing — meeting invite (spec 12)                                  | (none)                       | `D`            |
| `⚑`   | flag   | Existing — flagged (independent column)                              | `[ui].flag_indicator`        | `F`            |
| `📎`  | attach | Existing — has attachments (independent column)                      | `[ui].attachment_indicator`  | `@`            |

Priority within the invite slot when a message qualifies for
multiple (highest wins; single glyph rendered in the slot):

1. Calendar (`📅`) — meeting-invite messages.
2. Reply Later (`↩`) — actionable, write-debt outranks reference.
3. Set Aside (`📌`).
4. Mute (`🔕`) — only ever rendered in the `__muted__` virtual view
   today (spec 19 §5.2); all stack views are non-mute, so this slot
   contention only matters in `__muted__` itself, where Reply Later
   / Set Aside take priority over the mute glyph because the user
   navigated *into* the mute view and already knows everything in
   it is muted.

Order rationale: glyphs with **action commitment** outrank passive
indicators. A message in Reply Later **and** muted (in `__muted__`
view) renders as `↩`; the user is in the mute view so the mute
state is implicit. Flag and attachment columns continue rendering
independently — a Reply Later **and** flagged message shows
`⚑↩` in the row.

A message can carry both `Inkwell/ReplyLater` and `Inkwell/SetAside`
simultaneously (categories are multi-valued). The invite slot
renders the higher-priority glyph (`↩`); the lower stack still
claims the message, and both stacks appear in the viewer-pane
header (§5.5).

### 5.3 Sidebar virtual entries

Two new virtual sidebar entries, mirroring the `__muted__` pattern in
spec 19 §5.4:

| Sentinel folder ID  | Display name (default) | Visibility       |
| ------------------- | ---------------------- | ---------------- |
| `__reply_later__`   | `Reply Later`          | Count > 0        |
| `__set_aside__`     | `Set Aside`            | Count > 0        |

Render with the matching glyph: `↩ Reply Later  N` /
`📌 Set Aside  N`. Position: between saved searches and the
`__muted__` entry (when present). The badge `N` is the
`CountMessagesInCategory` result.

`displayedFolder` (in `internal/ui/panes.go`) gets two new boolean
fields and one count field; `FoldersModel` gets two new count
fields:

```go
// displayedFolder — add:
isReplyLater bool
isSetAside   bool
stackCount   int  // shared count field for either stack glyph

// FoldersModel — add:
replyLaterCount int
setAsideCount   int
```

`SetReplyLaterCount(int)` / `SetSetAsideCount(int)` mutator methods
mirror `SetMutedCount`. Each rebuilds the sidebar.

`rebuild()` appends the entries when the count is > 0, after the
saved-searches block and before the calendar block. Order between
the three virtual entries (when all > 0): Reply Later → Set Aside →
Muted. Reasoning: Reply Later is the most action-bearing (closest to
the user's working memory); Muted is the most passive.

`(FoldersModel) IsReplyLaterSelected() bool` and
`IsSetAsideSelected() bool` parallel `IsMutedSelected()`.

**Dispatcher branches** — three sites in `internal/ui/app.go` learn
about the new sentinels (mirrors the audit done for `__muted__`):

1. **Folder-selection handler** (the place that today calls
   `loadMutedMessagesCmd` near `app.go:1543`): add two new branches
   for `IsReplyLaterSelected()` / `IsSetAsideSelected()` that call
   `m.loadStackMessagesCmd(category, sentinel)`.
2. **Body-fetch path** (the per-message body load Cmd): no change —
   stack views surface real messages with real Graph IDs; existing
   body fetch works.
3. **List-pane invite-slot renderer** (`panes.go:838-852`): the
   existing branch `m.FolderID == mutedSentinelID` → `🔕`. Replace
   with a small switch over the active sentinel; the stack-view
   branches use the priority logic from §5.2 (read message
   categories, pick the highest-priority glyph). This is one site
   not three because the priority logic is centralised.

```go
// internal/ui/app.go — Cmd factory.
//
// loadStackMessagesCmd loads messages for a stack virtual folder.
// FolderID in the resulting MessagesLoadedMsg is the sentinel so
// the list pane can identify the view.
func (m Model) loadStackMessagesCmd(category, sentinel string) tea.Cmd {
    limit := m.list.LoadLimit()
    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()
        var accountID int64
        if m.deps.Account != nil {
            accountID = m.deps.Account.ID
        }
        msgs, err := m.deps.Store.ListMessagesInCategory(ctx, accountID, category, limit)
        if err != nil {
            return ErrorMsg{Err: err}
        }
        return MessagesLoadedMsg{FolderID: sentinel, Messages: msgs}
    }
}
```

The function is parameterised over `(category, sentinel)`; the two
call sites pass `(CategoryReplyLater, "__reply_later__")` and
`(CategorySetAside, "__set_aside__")`. Same Cmd shape as
`loadMutedMessagesCmd`; no Bubble Tea idiom violation (Bubble Tea
allows parameterised Cmd factories — see `loadFolderMessagesCmd`
which already takes a folder ID).

### 5.4 Stack views — list-pane behaviour

When the active "folder" is `__reply_later__` or `__set_aside__`:

- Sort: `received_at DESC` (newest first). Rationale: reply-debt is
  freshest at the top — newest messages are the ones still in working
  memory. (HEY uses time-of-set-aside; we use received-at as a
  simpler v1 approximation; revisit if users complain.)
- Mute filter: NOT applied (`ExcludeMuted: false`). Same precedent as
  search (spec 19 §4.3).
- Folder column: shown when results span >1 folder (reuses the
  spec 21 `folderNameByID` map and rendering branch in
  `internal/ui/panes.go`, around line 871). Stacks are inherently
  cross-folder — a Reply Later message moved to Archive still
  appears in the queue. The `MessagesLoadedMsg` handler for stack
  sentinels populates `m.list.folderNameByID` from the new map
  `m.foldersByID` (introduced in spec 21) for the distinct folders
  in the result; cleared back to `nil` on next folder switch.
- Triage actions still work (`r` mark-read, `d` delete, `a` archive,
  `f` flag, `M` mute). Removing the message from the stack is `L` /
  `S` (the toggle untags it).
- Empty state: when the user navigates to an empty stack (count
  dropped to 0 between sidebar render and selection), the list pane
  renders `Reply Later is empty` / `Set Aside is empty`. The sidebar
  entry hides on next rebuild.

### 5.5 Viewer-pane header

The viewer-pane header is rendered by
`internal/render/headers.go:Headers()`. Today it always emits
`From / To / Cc? / Date / Subject`, and (when `ShowFullHeaders` is
true) appends `Importance / Categories / Flag / Has Attachments /
Message-ID`. The full-headers `Categories` line lists every category
joined with `, ` — generic, including `Inkwell/*` reserved names.

Spec 25 changes the always-on header (not the full-headers expansion):
add a one-line `Stacks:` indicator immediately after `Date` and
before `Subject`, **only when the message is in at least one
inkwell stack**. The full-headers `Categories` line is unchanged
(still lists every category including `Inkwell/*`).

```
From:    alice@example.invalid
To:      eugene@example.invalid
Date:    Mon 14:30
Stacks:  ↩ Reply Later · 📌 Set Aside     ← only when present
Subject: Q4 forecast
```

Implementation: `Headers()` filters the message's `Categories` slice
for `IsInkwellCategory` matches, formats them with the theme's
`HeaderLabel` style, and writes one extra line via `writeHeader`.
Empty result emits no line and no leading whitespace.

### 5.6 Status-bar feedback

| Action                                                | Toast                                                              |
| ----------------------------------------------------- | ------------------------------------------------------------------ |
| `L` adds (single)                                     | `✓ ↩ added to Reply Later (subject: Q4 deck)`                      |
| `L` removes (single)                                  | `✓ ↩ removed from Reply Later`                                     |
| `S` adds / removes (single)                           | same shape with `📌` and `Set Aside`                              |
| `T l` adds entire thread, all succeed                 | `✓ ↩ added thread to Reply Later (12 messages)`                    |
| `T l` partial failure                                 | `⚠ added 11/12 to Reply Later — 1 failed`                          |
| `T l` zero-eligible (all in Drafts/Trash/Junk)        | `thread: 0 messages to act on`                                     |
| `T L` / `T S` removes (success / partial / zero)      | same shape, "removed" verb                                         |
| `;L` / `;S` apply-to-filtered (success)               | `✓ ↩ added 247 messages to Reply Later`                            |
| `;L` partial failure                                  | `⚠ added 240/247 to Reply Later — 7 failed`                        |
| Single-press on already-tagged message (stale slice)  | toast still emitted; second action is a redundant PATCH (see §4.3) |

The status-bar formatter for thread / bulk operations reuses the
spec 20 §6 helper that already emits the `✓` / `⚠ X/N succeeded —
Y failed` / `0 messages to act on` shapes. Spec 25 only supplies the
verb / category strings.

The subject in the single-message toast is the focused message's
subject. Per ARCH §12 / `docs/CONVENTIONS.md` §7.3, subject lines must NOT
appear in log output outside DEBUG. The toast is terminal UI only
and is not logged. Spec 19 §5.5 set this precedent.

### 5.7 Focus & Reply mode (`:focus`)

Inspired directly by HEY's "Focus & Reply" feature. Walks the Reply
Later queue, opening the compose-reply UI (spec 15) for each message
in turn.

**Cmd-bar invocation:**

```
:focus           — start at the top of the queue
:focus <N>       — start at the Nth message (1-indexed)
```

**Loop semantics:**

1. Load `ListMessagesInCategory(CategoryReplyLater, limit=focus_queue_limit)`.
2. If empty (or `:focus N` with `N > len(queue)` or `N <= 0` or
   non-numeric): status-bar shows the relevant error
   (see §5.7.1) and stays in normal mode.
3. Open the message at `focusIndex`: switch to viewer pane, render
   body, immediately enter compose-reply (spec 15 `r`).
4. **Compose lifecycle.** The compose layer (spec 15) owns its own
   `Esc` modal (the draft-discard-or-save prompt) and exits compose
   by setting `m.mode = NormalMode` directly — there is **no
   composeClosedMsg** in spec 15 today. Focus mode therefore tracks
   compose entry/exit by mode transition, not by a dedicated
   message:
   - When focus mode opens compose for queue index `i`, it sets
     `m.focusComposePending = true` (new model field).
   - The Update loop checks, on every iteration, whether `m.mode`
     transitioned from `ComposeMode` to `NormalMode` while
     `focusModeActive && focusComposePending` were both true. The
     check lives in a small helper at the top of `Update` (a
     single-line comparison against a `prevMode` field already
     on `Model`, or a new `focusPrevMode` field added in this
     spec).
   - On detected exit, `focusComposePending` is cleared and the
     queue-advance step (5) runs. The mode transition itself is
     unchanged — focus mode is purely observational.
5. On detected compose exit:
   - The message stays in the Reply Later stack regardless of
     compose outcome (send, save-draft, discard). Auto-clear-on-send
     is rejected: the user might have replied and also want the
     thread archived, or the reply might be partial. Stay manual.
   - Advance `focusIndex` and open the next message. If no next
     message, see step 7.
6. **Esc from focus mode** (outside compose): if `focusModeActive
   == true` AND `m.mode == NormalMode`, `Esc` exits focus mode
   immediately. Restores the previous folder
   (`focusReturnFolderID`) and clears the `[focus i/N]` indicator.
   The existing `Esc` handler (which clears filters etc.) takes
   precedence only when no focus session is active.
7. End of queue: status-bar `focus: queue cleared (N messages
   processed)`. Return to normal mode and the previous folder.

### 5.7.1 `:focus` argument validation

| Input             | Behaviour                                                          |
| ----------------- | ------------------------------------------------------------------ |
| `:focus`          | Start at index 1 (top of queue).                                   |
| `:focus 1`..`N`   | Start at the given 1-indexed position.                             |
| `:focus 0`        | Status: `focus: invalid index (must be ≥ 1)`. No mode entered.     |
| `:focus -3`       | Same friendly error.                                               |
| `:focus abc`      | Status: `focus: invalid index (must be a positive integer)`.       |
| `:focus 9999` (>N)| Status: `focus: queue has only N messages`. No mode entered.       |
| Empty queue       | Status: `focus: Reply Later is empty`. No mode entered.            |

**Mode indicator:** the status bar shows `[focus 3/12]` while the
mode is active. `Esc` outside compose exits the mode immediately
(sets `focusModeActive = false`, restores previous folder, clears
indicator).

**Model field:**

```go
// Model — add:
focusModeActive bool
focusQueueIDs   []string  // pre-fetched queue snapshot (no race)
focusIndex      int       // 0-based cursor into focusQueueIDs
focusReturnFolderID string  // folder to restore on exit
```

The queue is **frozen at start** — a message added/removed during
the session does not shift the cursor or the snapshot. This avoids
a race where the user replies to message 3, the compose-reply
handler mutates Categories, and the next-index logic skips the
following message. The user can re-issue `:focus` to get a fresh
snapshot.

**Why pre-fetched, not streaming:** queue length is bounded (default
limit 200; CONFIG `[ui].focus_queue_limit` overrides). Reply-later
sessions over 200 messages are rare and pre-fetching saves a query
per advance.

### 5.8 Thread chord (`T <verb>`) — spec 20 extension

Add to the `T` chord verb table (spec 20 §3):

| Chord  | Action                                                    | Confirm modal? |
| ------ | --------------------------------------------------------- | -------------- |
| `T l`  | Add entire thread to Reply Later                          | No             |
| `T L`  | Remove entire thread from Reply Later                     | No             |
| `T s`  | Add entire thread to Set Aside                            | No             |
| `T S`  | Remove entire thread from Set Aside                       | No             |

**Implementation extends spec 20's `ThreadExecute` signature plus
`BatchExecute`'s public signature** so a category Param can flow
through.

The path today:

- `BatchExecute(ctx, accID, actionType, messageIDs)` — `internal/action/batch.go:93` —
  forwards to the unexported `batchExecute(..., extraParams=nil, ...)`.
- `ThreadExecute(ctx, accID, verb, focusedMsgID)` — `internal/action/executor.go:209` —
  collects IDs and calls `BatchExecute`.
- `ui.ThreadExecutor` interface — `internal/ui/app.go:151` — wraps
  the two methods.
- `cmd/inkwell/cmd_run.go:855` `threadExecutorAdapter` — implements
  the interface for the headless harness.
- `internal/ui/thread_test.go:22` — mock implementing the interface.

To carry a `category` Param without losing it inside
`batchExecute`, the spec extends two signatures:

```go
// internal/action/batch.go — new public method (or extend
// existing). BatchExecute keeps its current signature for back-
// compat; BatchExecuteWithParams is the new entry point that
// passes through to batchExecute(extraParams, false).
func (e *Executor) BatchExecuteWithParams(ctx context.Context,
    accountID int64, actionType store.ActionType,
    messageIDs []string, params map[string]any) ([]BatchResult, error) {
    return e.batchExecute(ctx, accountID, actionType, messageIDs, params, false)
}

// internal/action/executor.go — extend signature. The existing
// caller in app.go is updated to pass nil.
func (e *Executor) ThreadExecute(ctx context.Context, accID int64,
    verb store.ActionType, focusedMsgID string,
    params map[string]any) (int, []BatchResult, error)
```

`ThreadExecute` calls `BatchExecuteWithParams` when `params != nil`,
else `BatchExecute` (cheap branch; both end up in `batchExecute`).

```go
// In dispatchThreadChord, on `l` after `T`:
return m, m.runThreadExecuteCmd("add to Reply Later",
    store.ActionAddCategory, sel.ID,
    map[string]any{"category": store.CategoryReplyLater})

// On `L`:
return m, m.runThreadExecuteCmd("remove from Reply Later",
    store.ActionRemoveCategory, sel.ID,
    map[string]any{"category": store.CategoryReplyLater})
```

`runThreadExecuteCmd` (the existing `app.go` Cmd factory near
line 4445) gains an extra `params` parameter; existing call sites
pass nil.

**All four call/implementation sites must be updated** (DoD §10.1
bullets cover each):

1. `internal/action/executor.go:209` — `ThreadExecute` signature.
2. `internal/action/batch.go` — new `BatchExecuteWithParams` method.
3. `internal/ui/app.go:151` — `ThreadExecutor` interface signature.
4. `cmd/inkwell/cmd_run.go:855` — adapter signature.
5. `internal/ui/thread_test.go:22` — mock signature.

**Hint string update.** Spec 20's chord-pending status string is a
hardcoded constant appearing **twice** in `app.go` (lines 3659 and
4997). Spec 25 edits both occurrences in place to
`"thread: r/R/f/F/d/D/a/m/l/L/s/S  esc cancel"`. No generation
refactor; the single-source-of-truth claim from a prior revision is
withdrawn — both sites are listed in the DoD.

### 5.9 Bulk via `;` chord

The existing `;` apply-to-filtered chord (spec 10) covers bulk add /
remove from a filter result. No new key needed.

```
:filter ~f newsletter@*
;          ← apply-to-filtered prefix, status: "apply: d/D/a/r/R/f/F/m/L/S"
L          ← apply: add Reply Later to all 247 matched
```

Add `L` and `S` to the `;<verb>` dispatch table. Confirm modal text
is parameterised over verb (add / remove) and category (Reply Later
/ Set Aside):

```
Add 247 messages to Reply Later? [y/N]
```

When the active filter used `:filter --all` and the result spans >1
folder, the spec 21 §3.3 suffix appends:

```
Add 247 messages across 3 folders to Reply Later? [y/N]
```

The cross-folder suffix is wired by reusing the existing
`filterAllFolders && filterFolderCount > 1` check from spec 21's
`confirmBulk`. Default response is N for both single-folder and
cross-folder cases.

Reuses `BulkAddCategory` / `BulkRemoveCategory` (spec 09).

### 5.10 Cmd-bar verbs

Three new commands in `dispatchCommand` (`internal/ui/app.go:2547`):

| Command   | Behaviour                                                                    |
| --------- | ---------------------------------------------------------------------------- |
| `:later`  | Switch sidebar+list to the `__reply_later__` virtual folder view.            |
| `:aside`  | Switch sidebar+list to the `__set_aside__` virtual folder view.              |
| `:focus`  | Enter Focus & Reply mode for the Reply Later queue (§5.7).                   |

`:later` and `:aside` are convenience verbs for users who don't want
to navigate the sidebar with the keyboard. They behave exactly as
selecting the corresponding sidebar entry would: dispatch
`m.loadStackMessagesCmd(category, sentinel)` and move the sidebar
cursor onto the entry. If the count is 0 (entry not visible), the
command still succeeds (the list pane shows the empty-state message;
the sidebar entry remains hidden until a message is added).

## 6. CLI

```sh
# Reply Later
inkwell later add <message-id>           # tag with Inkwell/ReplyLater
inkwell later remove <message-id>        # untag
inkwell later list [--limit N]           # show queued messages (text or --output json)
inkwell later count                      # print N

# Set Aside
inkwell aside add <message-id>           # tag with Inkwell/SetAside
inkwell aside remove <message-id>        # untag
inkwell aside list [--limit N]
inkwell aside count

# Focus mode is TUI-only — there is no headless CLI invocation.
```

`<message-id>` is the Graph message ID. Add resolves cleanly via
`Executor.AddCategory`; remove via `Executor.RemoveCategory`. List
calls `Store.ListMessagesInCategory`.

**Auth and store wiring.** `inkwell later add` and
`inkwell aside add` mutate categories, which dispatches a Graph
PATCH via the action queue. Both commands therefore use
`buildHeadlessApp(ctx, rc)` — the same helper as `cmd_mute.go`
(spec 19 §7) — to acquire an authenticated MSAL client, the local
store, and the resolved account ID. `list` and `count` are
read-only against the local store; they still call
`buildHeadlessApp` because the store path comes from the config
loaded in the same helper. Tests stub `buildHeadlessApp` with a
pre-built app harness; `cmd_later_test.go` reuses the same harness
constructor as `cmd_mute_test.go`.

JSON output for `list` (timestamps in ISO 8601, matching
`inkwell messages` and `inkwell folder list` JSON shapes):

```json
{
  "stack": "reply_later",
  "count": 3,
  "messages": [
    {"id": "AAQk...", "subject": "...", "from": "alice@example.invalid",
     "received_at": "2026-05-04T14:30:00Z", "folder": "Inbox"}
  ]
}
```

For `count`:

```json
{"stack": "reply_later", "count": 12}
```

Commands live in `cmd/inkwell/cmd_later.go` and `cmd/inkwell/cmd_aside.go`.
Both register in `cmd_root.go`. Subcommand structure mirrors
`cmd_mute.go` (spec 19 §7).

A shared helper in `cmd/inkwell/stack.go` factors the add/remove/list
plumbing — the only difference between `later` and `aside` is the
category constant. The shared helper is preferred over copy-paste
because the add/remove paths each touch the executor, the printer,
and the JSON shape; duplicating four functions is brittle.

## 7. Performance budgets

| Surface | Budget | Benchmark |
| --- | --- | --- |
| `CountMessagesInCategory` over 100k-message store with 500 tagged | ≤10ms p95 | `BenchmarkCountMessagesInCategory` in `internal/store/` |
| `ListMessagesInCategory(limit=100)` over 100k-message store with 500 tagged | ≤10ms p95 | `BenchmarkListMessagesInCategory` |

The `EXISTS (SELECT 1 FROM json_each(categories) WHERE value = ?
COLLATE NOCASE)` predicate is the same JSON1 path the
`MessageQuery.Categories` predicate uses today (`buildListSQL` in
`internal/store/messages.go:408`). With JSON1 + a typical mailbox
the per-row cost is constant; the outer `account_id` filter rides
the existing `idx_messages_account_received` (spec 02). The
`LEFT JOIN folders` for the well-known-folder exclusion is a single
indexed lookup per row — same shape as `MessageIDsInConversation`
(spec 20).

The 10ms budget is consistent with spec 19's
`BenchmarkListMessagesExcludeMuted` (also 10ms over the same 100k
fixture) — both queries do a similar one-pass scan with a
`LEFT JOIN` filter. We deliberately don't claim 5ms (which the
adversarial review correctly flagged as plausibly tight on slower
hardware without a partial index).

If a future benchmark ever misses the budget by >50%, the fix is a
partial index `CREATE INDEX idx_messages_inkwell_stacks ON
messages(account_id) WHERE categories LIKE '%Inkwell/%'`. Not
required for v1.

The single-message toggle path (`Executor.AddCategory` /
`Executor.RemoveCategory`) is unchanged from spec 07 — no new
benchmark required for that path.

## 8. Edge cases

| Case | Behaviour |
|------|-----------|
| User tags a message via Outlook web with `Inkwell/ReplyLater` directly | Sync delta brings it down; the message appears in the inkwell sidebar count and stack view on next refresh. No special handling. |
| User tags with a near-miss (e.g. `inkwell/replylater` lowercase) in Outlook web | Treated as the same stack — `IsInkwellCategory`, `IsInCategory`, and the SQL `COLLATE NOCASE` match are all case-insensitive (§3.1). The local stored casing is preserved (`appendCategory` uses EqualFold for dedup but keeps whichever variant arrived first); membership and counts are correct regardless of casing. |
| User tags with a different prefix entirely (`MyReplyLater`) | Not recognised. Different category, not an inkwell stack. The user's free-form categories are unaffected. |
| Message is in Drafts and tagged Reply Later | Excluded from `ListMessagesInCategory` (well-known-folder filter). The user is editing the draft — the queue would be confusing. Categories on a draft do round-trip on send. |
| Message is permanently deleted | The categories tag dies with the message (FK cascade on the messages row). Sidebar count drops on the next list reload. No staleness window beyond one tick. |
| Message is moved to Junk | Excluded from `ListMessagesInCategory`. Same rationale as Drafts. |
| Toggle dispatched on a message with no `id` (offline-queued and not yet on Graph) | `Executor.AddCategory` accepts the local ID (action queue uses the local `messages.id` PK; spec 07 §6). Resolves on Graph response. |
| Conversation has 100 messages; user `T l`'s | All 100 get Reply Later. The stack count reflects 100 distinct messages. Removing the conversation is `T L` (chord-verb capital). |
| User configures `[bindings].reply_later_toggle = "r"` | `ApplyBindingOverrides` rejects via `findDuplicateBinding` — `r` is bound to MarkRead. Config load fails with the spec 04 error path. |
| User mutes a thread that's also in Reply Later | Stack view ignores mute (§5.4). Mute view shows the message (mute filter is per-conversation; Reply Later is per-message). Both states are independent and both visible in the appropriate view. |
| `:focus` invoked with an empty queue | Status bar: `focus: Reply Later is empty`. No mode entered. |
| `:focus <N>` with N > queue length | Status bar: `focus: queue has only N messages`. Mode not entered. |
| User removes the focused message from the queue mid-`:focus` | The queue is frozen at start (§5.7); the cursor still advances through the original snapshot. The user sees the message they un-tagged still in the loop until they `Esc`. Documented as a known v1 quirk. |
| Two devices both run inkwell against the same account; both tag `Inkwell/ReplyLater` for the same message | Graph PATCH is idempotent with the full categories array (spec 07 §6.9). The first PATCH wins; the second is a no-op (the array already has the value). No conflict. |
| Sidebar count for a stack falls to 0 while the user has the stack view open | Sidebar entry hides on next rebuild; the list-pane view shows the empty-state message. The user can `2` to focus list and `Esc` / pick another folder. |
| User mutes a thread (`M`), the list reloads with `ExcludeMuted: true` (the focused thread vanishes), then immediately presses `T l` | The model still holds the focused message ID from before the reload; `T l` operates on it correctly via `MessageIDsInConversation`. The user sees the toast "added thread to Reply Later (12 messages)" even though the thread is no longer visible in the list — the action succeeded server-side. The Reply Later sidebar count increments. Acceptable v1 quirk; the alternative (re-resolve focus on every reload) adds latency for no semantic gain. |
| `:focus` while a stack message is being dispatched-but-not-yet-applied | The pre-fetched queue snapshot is from the local store at `:focus` invocation time; it includes the message even if its add-to-stack PATCH hasn't yet drained. Removing a message via the action queue between snapshot time and queue-advance does not break focus mode (the message is still in the snapshot; advance is by index, not predicate). |
| User edits `[ui].focus_queue_limit` to 0 or negative | Config validation rejects at load time (range 1–1000). App refuses to start (`docs/CONVENTIONS.md` §9). |

## 9. Logging

- `Executor.AddCategory` and `Executor.RemoveCategory` already log at
  DEBUG level with `category` and `message_id`. The reserved names
  `Inkwell/ReplyLater` / `Inkwell/SetAside` are not PII; the existing
  redaction layer (`internal/log/redact.go`) lets them through.
- The toast-rendering path in `app.go` does **not** log subject lines
  (UI-only; ARCH §12 / `docs/CONVENTIONS.md` §7 rule 3).
- Focus mode does not introduce a new log site. Each compose-reply
  open / close is logged by spec 15's existing path.
- No new redaction tests required; the `add_category` / `remove_category`
  paths are already covered.

## 10. Definition of done

**No schema migration.** This spec adds no SQL DDL — categories,
indices, and the muted_conversations table are reused or unchanged.

### 10.1 Store / action layer

- [ ] `internal/store/categories.go` (new file): `CategoryReplyLater`
      / `CategorySetAside` constants; `IsInkwellCategory(s)` helper
      using `strings.EqualFold`; `IsInCategory(cats, cat)`
      membership helper, also case-insensitive.
- [ ] `Store.CountMessagesInCategory(ctx, accID, cat) (int, error)`
      hand-written SQL per §4.2 — `EXISTS (SELECT 1 FROM
      json_each(...) WHERE value = ? COLLATE NOCASE)` plus
      `LEFT JOIN folders` for Drafts/Trash/Junk exclusion. Does NOT
      apply ExcludeMuted.
- [ ] `Store.ListMessagesInCategory(ctx, accID, cat, limit)
      ([]Message, error)` with the same WHERE clause; ordered by
      `received_at DESC`.
- [ ] **Existing `MessageQuery.Categories` predicate widened to
      case-insensitive.** `buildListSQL` in
      `internal/store/messages.go:408-415` currently emits
      `EXISTS (SELECT 1 FROM json_each(categories) WHERE value IN
      (?,?))`. Replace with one `value = ? COLLATE NOCASE` clause
      per category, OR'd: `EXISTS (SELECT 1 FROM
      json_each(categories) WHERE value = ? COLLATE NOCASE OR
      value = ? COLLATE NOCASE)`. Existing callers' API is
      unchanged; semantics widen from strict to case-insensitive.
- [ ] **`Executor.BatchExecuteWithParams`** new public method in
      `internal/action/batch.go` that delegates to
      `batchExecute(extraParams=params, skipUndo=false)`.
      `BatchExecute` (no params) keeps its existing signature
      for back-compat.
- [ ] **`Executor.ThreadExecute` signature extended** to accept
      `params map[string]any` as a new trailing argument. When
      `params != nil`, calls `BatchExecuteWithParams`; else calls
      `BatchExecute`. The existing single caller in
      `runThreadExecuteCmd` (`internal/ui/app.go:4445`) is updated
      to pass nil for the existing verbs and the new map for
      category verbs.
- [ ] **`ui.ThreadExecutor` interface** (`internal/ui/app.go:151`)
      gets the new params argument on `ThreadExecute`.
- [ ] **`threadExecutorAdapter`** (`cmd/inkwell/cmd_run.go:855`)
      updated to match.
- [ ] **`internal/ui/thread_test.go:22`** mock implementation
      updated to match. (Any other test mocks discovered during
      compile fail — silver-bullet check is `go build ./...`
      green after the signature change.)

### 10.2 KeyMap and dispatch

- [ ] `KeyMap.ReplyLaterToggle` (default `L`) and
      `KeyMap.SetAsideToggle` (default `S`); matching
      `BindingOverrides` fields; wired through
      `ApplyBindingOverrides` and `findDuplicateBinding` (added to
      the `checks` slice).
- [ ] `L` and `S` dispatched in `dispatchList` and `dispatchViewer`.
      Pre-check via `IsInCategory(focused.Categories, ...)` decides
      add vs remove (single dispatch path; stale-slice race
      acknowledged §4.3). On result, reload list and show toast.
- [ ] Spec 20 chord verbs `T l` / `T L` / `T s` / `T S` dispatched
      via `ThreadExecute` with the appropriate
      `ActionAddCategory`/`ActionRemoveCategory` verb and category
      Param.
- [ ] Spec 20 chord-pending hint string updated in `app.go` at
      **both** sites (lines 3659 AND 4997) to
      `"thread: r/R/f/F/d/D/a/m/l/L/s/S  esc cancel"`.
      Hardcoded; not generated.
- [ ] `;L` / `;S` apply-to-filtered chord — confirm modal text
      parameterised over verb (add/remove) and category (Reply
      Later/Set Aside); cross-folder suffix from spec 21 §3.3
      preserved when `filterAllFolders && filterFolderCount > 1`;
      calls `BulkAddCategory` / `BulkRemoveCategory`.

### 10.3 List pane / sidebar

- [ ] List-row invite-slot indicator: `↩` for `CategoryReplyLater`,
      `📌` for `CategorySetAside`. ASCII fallbacks `R` / `P` via
      `[ui].reply_later_indicator` and `[ui].set_aside_indicator`
      config keys.
- [ ] Invite-slot priority: Calendar > Reply Later > Set Aside >
      Mute. Flag and attachment columns continue rendering
      independently of stack state (§5.2).
- [ ] Sidebar virtual entries `__reply_later__` and `__set_aside__`,
      visible only when count > 0:
      - `displayedFolder.isReplyLater bool` / `isSetAside bool` /
        `stackCount int` fields in `internal/ui/panes.go`.
      - `FoldersModel.replyLaterCount` / `setAsideCount` fields plus
        `SetReplyLaterCount(int)` / `SetSetAsideCount(int)` mutators.
      - `IsReplyLaterSelected() bool` / `IsSetAsideSelected() bool`
        predicates parallel to `IsMutedSelected()`.
      - `rebuild()` appends entries between the saved-searches block
        and the calendar block. Order between virtual entries (when
        all > 0): Reply Later → Set Aside → Muted.
- [ ] `loadStackMessagesCmd(category, sentinel)` Cmd in `app.go`;
      `MessagesLoadedMsg{FolderID: sentinel, Messages: msgs}`
      handler populates `m.list.folderNameByID` from `m.foldersByID`
      when the stack result spans >1 folder (spec 21 reuse); cleared
      on next folder switch.
- [ ] List-pane invite-slot renderer (`panes.go:838-852`) extended:
      when `m.FolderID == "__reply_later__"` or `"__set_aside__"`,
      apply the invite-slot priority over the focused row's
      categories. The existing `mutedSentinelID` branch unchanged.

### 10.4 Stack count refresh

- [ ] `refreshStackCountsCmd` returning
      `stackCountsUpdatedMsg{replyLater, setAside int}`. Dispatched
      at every site that could mutate categories:
      - On every `actionResultMsg` (or equivalent) where
        `Action.Type` is `ActionAddCategory` or `ActionRemoveCategory`.
      - On every `bulkResultMsg` where the bulk verb was a category
        op (add or remove).
      - On every successful `:focus` exit (queue snapshot may have
        diverged from current state).
      - On every `MessagesLoadedMsg` for a non-stack folder, since
        a sync delta may have arrived with category changes from
        Outlook web. (Cheap — two SELECT COUNT calls per reload.)
      - On startup, alongside `refreshMutedCountCmd`.

### 10.5 Viewer pane

- [ ] `Stacks:` line in `internal/render/headers.go:Headers()`
      between `Date` and `Subject`, only when `IsInkwellCategory`
      matches at least one entry of the message's `Categories`.
      Both stacks render side-by-side when both present. Empty
      result emits no line.

### 10.6 Focus mode

- [ ] `:focus [N]` cmd-bar dispatch in `dispatchCommand`. Validates
      `N` per §5.7.1; emits friendly status errors for invalid
      input.
- [ ] Model fields: `focusModeActive bool`, `focusQueueIDs
      []string`, `focusIndex int`, `focusReturnFolderID string`,
      `focusComposePending bool`, `focusPrevMode Mode`. Queue
      pre-fetched at `:focus` invocation; immutable for the
      session.
- [ ] `[focus i/N]` indicator in the status bar while
      `focusModeActive`.
- [ ] **Compose-exit detection (no new compose message).** A small
      observer at the top of `Update` compares `m.mode` to
      `m.focusPrevMode`; when the transition is `ComposeMode →
      NormalMode` and `focusModeActive && focusComposePending` are
      both true, advance the queue. `focusPrevMode` is updated at
      the end of every Update tick. (Spec 15 emits no
      `composeClosedMsg`; compose exits by setting `m.mode =
      NormalMode` directly.) Test:
      `TestFocusModeAdvancesOnComposeExitTransition`.
- [ ] `Esc` exits focus mode only when `m.mode == NormalMode`
      (compose modals own their own `Esc`).
- [ ] End-of-queue: `focus: queue cleared (N messages processed)`,
      restore `focusReturnFolderID`, clear all focus-mode model
      fields.

### 10.7 Cmd-bar verbs

- [ ] `:later` / `:aside` cmd-bar verbs in `dispatchCommand` —
      switch sidebar+list to the corresponding sentinel; works
      even when count is 0 (list shows empty-state).
- [ ] `:focus [N]` per §10.6.

### 10.8 CLI

- [ ] `cmd/inkwell/cmd_later.go`: subcommands `add`, `remove`,
      `list`, `count`. Use `buildHeadlessApp(ctx, rc)`.
- [ ] `cmd/inkwell/cmd_aside.go`: same shape.
- [ ] `cmd/inkwell/stack.go`: shared helper for add/remove/list/count
      parameterised by category constant.
- [ ] Registered in `cmd_root.go`. Both support `--output json`
      with ISO 8601 timestamps.

### 10.9 Configuration

- [ ] `docs/CONFIG.md` rows for: `[ui].reply_later_indicator`
      (default `↩`, ASCII `R`); `[ui].set_aside_indicator`
      (default `📌`, ASCII `P`); `[ui].focus_queue_limit`
      (default 200, range 1–1000); `[bindings].reply_later_toggle`
      (default `L`); `[bindings].set_aside_toggle` (default `S`).
- [ ] Validation for `focus_queue_limit` in
      `internal/config/validate.go` — bounds 1–1000; out-of-range
      values fail config load (`docs/CONVENTIONS.md` §9).

### 10.10 Tests

- [ ] **store**: `TestCountMessagesInCategoryExcludesDrafts`,
      `TestCountMessagesInCategoryExcludesJunkAndTrash`,
      `TestCountMessagesInCategoryIncludesMuted` (regression — mute
      does not suppress stack membership),
      `TestCountMessagesInCategoryCaseInsensitive` (lowercase tag in
      Outlook web still counts),
      `TestListMessagesInCategoryOrderedByReceivedDesc`,
      `TestListMessagesInCategoryHonoursLimit`.
- [ ] **action**: `TestThreadExecuteAddCategoryWithParams` (verifies
      the new params arg routes through `BatchExecute` for every
      thread message); `TestThreadExecuteRemoveCategoryWithParams`;
      `TestThreadExecuteEmptyConversationReturnsZero`.
- [ ] **dispatch (unit)**: `TestReplyLaterToggleAddsWhenAbsent`
      (`L` on a message lacking the cat → enqueues `add_category`);
      `TestReplyLaterToggleRemovesWhenPresent`;
      `TestSetAsideToggleSamePattern`;
      `TestReplyLaterToggleMembershipCaseInsensitive` (lowercase
      `inkwell/replylater` already on the message — `L` removes it,
      not adds a duplicate);
      `TestFocusInvalidIndexZeroShowsError` (`:focus 0` →
      `focus: invalid index (must be ≥ 1)`);
      `TestFocusInvalidIndexNegativeShowsError`;
      `TestFocusInvalidIndexNonNumericShowsError`;
      `TestFocusOutOfRangeShowsError`
      (`:focus 9999` → `focus: queue has only N messages`);
      `TestRefreshStackCountsDispatchedAfterAddCategoryAction`;
      `TestRefreshStackCountsDispatchedAfterBulkCategoryAction`;
      `TestRefreshStackCountsDispatchedAfterMessagesLoadedMsg`;
      `TestJSONEachCollateNocaseRoundtrip` (integration — verifies
      `modernc.org/sqlite` honours `COLLATE NOCASE` over
      `json_each.value`).
- [ ] **e2e (TUI)**: `TestReplyLaterIndicatorRenderedInInviteSlot`
      (queue a message; assert `↩` in invite slot of row; assert
      `⚑`/`📎` columns unchanged); `TestSetAsideIndicatorRendered`;
      `TestReplyLaterSidebarVisibleWhenCountPositive`;
      `TestSetAsideSidebarHiddenWhenCountZero`;
      `TestSelectReplyLaterSidebarLoadsList` (cursor on entry,
      Enter → list pane shows queued messages with sentinel
      FolderID); `TestStackListShowsFolderColumnWhenCrossFolder`
      (queue messages in 3 folders → FOLDER column rendered);
      `TestFocusModeAdvancesOnComposeExitTransition` (script
      `:focus` → compose opens; simulate compose exit by setting
      `m.mode = NormalMode` (the same transition the real send /
      save-draft / discard paths produce); assert next message
      opens; on end-of-queue, status shows `focus: queue cleared
      (3 messages processed)`);
      `TestFocusModeEscOutsideComposeExitsImmediately`;
      `TestThreadChordTLAddsAllMessagesToReplyLater` (`T l` on
      2-message thread → both tagged via BatchExecute);
      `TestThreadChordCapitalLRemoves` (`T L`);
      `TestApplyToFilteredSemicolonL` (`;L` after `:filter` →
      confirm modal → all matched tagged);
      `TestApplyToFilteredSemicolonLCrossFolderSuffix`
      (`:filter --all`, then `;L` → confirm reads
      `Add 247 messages across 3 folders to Reply Later?`);
      `TestViewerHeaderShowsStacksLineWhenInStack`
      (open a message in both stacks → "Stacks:" line lists
      both glyphs and is positioned between Date and Subject);
      `TestViewerHeaderHidesStacksLineWhenNotInStack`;
      `TestColonLaterCmdSwitchesToReplyLaterView`;
      `TestColonAsideCmdSwitchesToSetAsideView`.
- [ ] **CLI**: `TestLaterCLIAddRemoveCount`;
      `TestAsideCLIListJSONOutputISO8601` (timestamp shape check);
      `TestLaterCLIRejectsEmptyMessageID`;
      `TestLaterCLIUsesHeadlessAppHarness` (auth wiring).
- [ ] **Benchmarks**: `BenchmarkCountMessagesInCategory` (≤10ms
      p95, 100k msgs / 500 tagged); `BenchmarkListMessagesInCategory`
      (≤10ms p95, same fixture).

### 10.11 User docs

- [ ] `docs/user/reference.md`: rows for `L`, `S`, `T l`, `T L`,
      `T s`, `T S`, `;L`, `;S`, `:focus`, `:later`, `:aside`, the
      `Reply Later` and `Set Aside` sidebar entries, and the
      invite-slot priority note.
- [ ] `docs/user/how-to.md`: "Build a Reply Later queue" recipe and
      "Set things aside for reference" recipe; one-paragraph note
      on Focus & Reply mode.
- [ ] `docs/user/explanation.md`: paragraph on the two-stack model
      (HEY origin), the categories storage choice, and the Outlook
      visibility implication ("you'll see Inkwell/ReplyLater
      categories when you open Outlook web — that's intentional;
      it's how the queue syncs to a second device").
- [ ] `docs/PRIVACY.md`: short paragraph noting that
      `Inkwell/ReplyLater` and `Inkwell/SetAside` categories are
      written to the user's Microsoft 365 mailbox and visible to
      anyone with delegated access (executive assistants,
      compliance reviewers). The behavioural metadata exposure is
      acknowledged and intentional (cross-device sync).
- [ ] PR checklist (`docs/CONVENTIONS.md` §11) fully ticked.

## 11. Cross-cutting checklist

- [ ] **Scopes:** none new. `Mail.ReadWrite` (PRD §3.1) covers
      category PATCH.
- [ ] **Store reads/writes:** read `messages` via two new
      hand-written SQL helpers `CountMessagesInCategory` /
      `ListMessagesInCategory` (NOT through `ListMessages` /
      `MessageQuery` — they need a folder-exclusion clause that
      `MessageQuery` does not expose). The existing
      `MessageQuery.Categories` predicate is widened from
      case-sensitive `IN` to case-insensitive `value = ? COLLATE
      NOCASE` (one bind per category, OR'd) in the same change.
      Writes route through existing `add_category` /
      `remove_category` action types — no new table, no new
      mutating SQL.
- [ ] **Graph endpoints:** `PATCH /me/messages/{id}` (existing
      categories path, spec 07 §6.9) and `/$batch` for thread/bulk
      variants (existing, spec 09).
- [ ] **Offline:** L / S queue locally; the action queue drains on
      next sync; categories appear instantly in the local view via
      optimistic apply. `:focus` mode opens compose UIs that the
      compose layer (spec 15) handles offline-first.
- [ ] **Undo:** `u` reverses the most recent toggle (uses spec 07's
      existing `add_category` ↔ `remove_category` inverse). Thread
      and bulk variants push a single grouped undo entry per spec 09.
- [ ] **User errors:** §8 edge-case table. Empty Focus queue and
      out-of-range index print friendly status-bar errors.
- [ ] **Latency budget:** §7 — count ≤10ms p95; list ≤10ms p95;
      single-message toggle uses the existing `add_category`
      budget (no new bench).
- [ ] **Logs:** existing `add_category` / `remove_category` log
      sites; no new log emissions. Subject not logged in toasts.
- [ ] **CLI:** `inkwell later …` and `inkwell aside …` per §6.
- [ ] **Tests:** §10 list.
- [ ] **Spec 17 review:** no new external HTTP surface (existing
      `PATCH /me/messages/{id}` categories path); SQL composition
      adds two hand-written queries with bound parameters and
      compile-time category constants — no user-input SQL injection
      surface; no token handling; no subprocess; no new cryptographic
      primitive; no new persisted state (categories already on
      `messages`). Reserved-category strings are bound at compile
      time. **Privacy implication acknowledged in PRIVACY.md** (DoD
      §10.11): the `Inkwell/ReplyLater` / `Inkwell/SetAside` strings
      sync to the server-side mailbox and are visible to anyone with
      delegated access (assistants, compliance reviewers). This is
      intentional behavioural metadata exposure for cross-device
      sync. No threat-model row required (the categories surface is
      pre-existing; we add two reserved strings to it).
- [ ] **Spec 19 (mute) consistency:** stack views ignore mute (§5.4);
      a muted thread can still be in a stack and vice versa. List-row
      priority puts Reply Later above Mute.
- [ ] **Spec 20 (thread chord) consistency:** four new chord verbs
      (`T l` / `T L` / `T s` / `T S`). The pending-status hint
      string is hardcoded in `app.go` at TWO sites (lines 3659,
      4997); this spec edits both. `ThreadExecute` /
      `ThreadExecutor` interface / `threadExecutorAdapter` / mock
      all carry a new `params map[string]any` argument; existing
      callers pass nil.
- [ ] **Spec 21 (cross-folder bulk) consistency:** stack views are
      inherently cross-folder; the FOLDER column appears in stack
      list views when results span >1 folder, reusing the spec 21
      column logic (the list-pane `folderNameByID` map is populated
      when the active sentinel folder is `__reply_later__` or
      `__set_aside__`).
- [ ] **Docs consistency sweep:** CONFIG.md (3 keys), reference.md
      (4 keybindings + 3 commands + 2 sidebar entries), how-to.md
      (2 recipes), explanation.md (one paragraph). No CHANGELOG-style
      file added.
