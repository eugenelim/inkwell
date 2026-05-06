# Spec 21 — Cross-folder bulk operations

**Status:** Ready for implementation.
**Depends on:** Specs 08 (pattern compile), 09 (batch executor), 10
(filter UX), 19 (mute — muted-message interaction documented §5).
**Blocks:** Custom actions framework (reuses `--all` / cross-folder
semantics).
**Estimated effort:** 1 day.

---

## 1. Goal

The `:filter` command and the `inkwell filter` CLI already query **all
folders** for the account — `store.SearchByPredicate` has no folder
scope by default. However, the UX does not surface this: the status
bar says "matched 47" without indicating which folders were touched,
the confirm modal says "Delete 247 messages?" with no folder context,
and the list pane renders no folder column so the user cannot tell at
a glance where a result message lives.

This spec adds the **visibility layer** for the already-existing cross-
folder capability:

1. Parse `--all` / `-a` from the `:filter` cmd-bar input to explicitly
   mark a filter as cross-folder (enabling folder-count tracking).
2. Show folder count in the status bar hint and confirm modal.
3. Show a FOLDER column in the list pane when results span >1 folder.
4. Wire `--all` in the CLI so `inkwell messages --filter ... --all`
   overrides `--folder` scoping.

### 1.1 What does NOT change

- `SearchByPredicate` is not changed — it remains cross-folder by
  default (account-scoped, trash/spam excluded).
- `CompileOptions.DefaultFolderID` is defined in the compile package
  but is unused in any production path; this spec does not implement
  it. Single-folder scoping for filter is not the goal here.
- The TUI `runFilterCmd` is not changed to add folder scoping.
- The `--action` / `--apply` pattern in the existing `inkwell filter`
  CLI is kept as-is — it is the established CLI contract.

## 2. Prior art

### 2.1 Terminal clients

- **mutt / neomutt** — cross-folder is achieved with virtual-mailboxes
  (notmuch) or `T~A` (tag-all) + `;d` (delete tagged). No built-in
  folder column; no confirmation count. Users rely on visual tag
  highlighting.
- **aerc** — `:filter` is per-mailbox; cross-folder requires notmuch
  backend via `:query`. No folder column in results.
- **alot (notmuch)** — natively cross-folder; the thread buffer shows
  the originating folder (tag) on each thread row. The tag/folder
  context is always visible.

### 2.2 Desktop / web clients

- **Gmail** — search is cross-label by default. Results list has no
  label column but the label badge appears on each row (if the message
  carries a non-inbox label). Confirm dialog on bulk delete shows
  total count only — no folder breakdown.
- **Thunderbird** — "Search Messages" (Ctrl+Shift+F) is cross-folder.
  The results list has an explicit Location/Folder column that always
  shows which folder the message is in. Bulk delete from results works
  without a folder-count warning.
- **Apple Mail** — "All Mailboxes" search has an optional Mailbox
  column (added via header right-click). Bulk delete has no
  cross-mailbox confirmation.
- **Outlook web** — default search scope is "Current mailbox" (all
  folders). Results have a Folder column. Bulk select/delete shows
  total count; no per-folder breakdown.
- **Superhuman** — search is cross-label; results show the label badge
  on each row. No folder column per se (label badge serves that role).

### 2.3 Design decision

Follow Thunderbird / Outlook: show a FOLDER column when results span
more than one folder. The column auto-hides for single-folder results
to avoid visual clutter. The confirm modal includes "across N folders"
when N > 1 — a safety cue that the user is touching more than one
location, without requiring them to count.

## 3. UI

### 3.1 Cmd-bar `:filter` — `--all` prefix token

```
:filter ~f newsletter@*           — runs as today (cross-folder, no
                                    folder-count tracking)
:filter --all ~f newsletter@*     — same query, but explicitly marks
:filter -a ~f newsletter@*          the filter as cross-folder, enabling
                                    folder-count display
```

The `--all` / `-a` token is stripped in the `:filter` cmd-bar handler
**before** the remaining pattern string is passed to `runFilterCmd`.
Without the prefix, `filterAllFolders` stays `false` and the hint
shows the message count only. With the prefix, `filterAllFolders` is
set to `true` and the folder count is shown.

**Why not change the default?** `:filter` without `--all` behaves
identically to today — it runs cross-folder, but without surfacing
folder metadata in the UI. This avoids a UX shock for users with
existing filter-heavy workflows while making cross-folder awareness
opt-in.

### 3.2 Status bar hint

| State | Hint |
|-------|------|
| Single-folder filter (no `--all`) | `filter: ~f newsletter@* · matched 47 · ;d delete · :unfilter` |
| Cross-folder `--all`, single folder in results | `filter: ~f newsletter@* · matched 47 (Inbox) · ;d delete · :unfilter` |
| Cross-folder `--all`, multiple folders | `filter: ~f newsletter@* · matched 247 (5 folders) · ;d delete · :unfilter` |

The folder name for the single-folder case is looked up by iterating
`m.folders.raw` (a `[]store.Folder` slice on `FoldersModel`) or via a
`foldersByID map[string]store.Folder` field added to `Model` in this
change and populated in the `FoldersLoadedMsg` handler (see §4.3).

### 3.3 Confirm modal — folder context

When `filterAllFolders && filterFolderCount > 1`:

```
Delete 247 messages across 5 folders?

Filter: ~f newsletter@*

Sample:
  Q4 forecast slides
  Re: investor deck
  … and 242 more
```

Single-folder confirm is unchanged (no "across N folders" suffix).

### 3.4 FOLDER column in list pane

When `filterAllFolders && filterFolderCount > 1`, the list pane adds
a FOLDER column between FROM and SUBJECT:

```
RECEIVED       FROM             FOLDER       SUBJECT
Mon 14:30      Alice            Inbox        Q4 deck
Mon 13:55      Bob              Clients      Re: deck
Fri 09:10      newsletter@*     Promotions   Your weekly digest
```

The column uses a fixed 12-character width (`%-12s`). FROM is trimmed
to 12 characters (down from 14) to accommodate. The header row adapts
automatically. No FOLDER column for single-folder results.

The folder display name is looked up from the same `foldersByID` map.
Unknown folder IDs (e.g., a folder that was deleted since the message
was cached) render as `"???"`.

## 4. Implementation

### 4.1 Model fields — new

```go
// Model — add:
filterAllFolders  bool              // set when --all / -a prefix used
filterFolderCount int               // distinct folders in current filter result
filterFolderName  string            // display name when filterFolderCount == 1
```

These are cleared alongside `filterActive` / `filterPattern` in
`clearFilter()`.

### 4.2 Cmd-bar parsing — strip `--all` / `-a`

In `dispatchCommand`, the `case "filter":` branch (around line 2406)
currently does:

```go
patternSrc := strings.TrimSpace(line after "filter")
return m, m.runFilterCmd(patternSrc)
```

Change to:

```go
patternSrc := strings.TrimSpace(line after "filter")
allFolders := false
if strings.HasPrefix(patternSrc, "--all") {
    patternSrc = strings.TrimSpace(patternSrc[5:])
    allFolders = true
} else if patternSrc == "-a" || strings.HasPrefix(patternSrc, "-a ") {
    patternSrc = strings.TrimSpace(patternSrc[2:])
    allFolders = true
}
if patternSrc == "" {
    m.lastError = fmt.Errorf("filter: usage :filter [--all] <pattern>")
    return m, nil
}
m.filterAllFolders = allFolders
return m, m.runFilterCmd(patternSrc)
```

`runFilterCmd` is unchanged — it already runs cross-folder.

### 4.3 `filterAppliedMsg` handler — compute folder metadata

When `filterAppliedMsg` is received and `filterAllFolders == true`,
compute from the message slice:

```go
// Count distinct folder IDs.
seen := make(map[string]struct{}, len(msg.messages))
for _, m := range msg.messages {
    seen[m.FolderID] = struct{}{}
}
m.filterFolderCount = len(seen)
if m.filterFolderCount == 1 {
    // Look up display name from the sidebar folder map.
    for id := range seen {
        if f, ok := m.foldersByID[id]; ok {
            m.filterFolderName = f.DisplayName
        } else {
            m.filterFolderName = "???"
        }
    }
}
```

`m.foldersByID` does not yet exist on `Model`. Add it in this change:

```go
// Model — add alongside filterAllFolders:
foldersByID map[string]store.Folder  // populated in FoldersLoadedMsg handler
```

Populate in the `FoldersLoadedMsg` handler (wherever `m.folders` is
updated from a `ListFolders` result):

```go
m.foldersByID = make(map[string]store.Folder, len(msg.folders))
for _, f := range msg.folders {
    m.foldersByID[f.ID] = f
}
```

### 4.4 Confirm modal — add folder suffix

`confirmBulk`'s signature is unchanged. In the body, after the verb
string is assembled, add a folder-count suffix before the `?`:

```go
// In confirmBulk body, replacing the existing first-line construction:
line := fmt.Sprintf("%s %d messages", titleCase(verb), count)
if m.filterAllFolders && m.filterFolderCount > 1 {
    line += fmt.Sprintf(" across %d folders", m.filterFolderCount)
}
sb.WriteString(line + "?")
// rest of confirmBulk unchanged
```

### 4.5 FOLDER column in list pane

`ListModel` needs a `folderNameByID map[string]string` field:

```go
// ListModel — add:
folderNameByID map[string]string  // populated when cross-folder filter is active
```

In the `filterAppliedMsg` handler, after computing `filterFolderCount`,
populate the map from `m.foldersByID`:

```go
if m.filterAllFolders && m.filterFolderCount > 1 {
    nameMap := make(map[string]string, len(seen))
    for id := range seen {
        if f, ok := m.foldersByID[id]; ok {
            nameMap[id] = f.DisplayName
        } else {
            nameMap[id] = "???"
        }
    }
    m.list.folderNameByID = nameMap
} else {
    m.list.folderNameByID = nil
}
```

In `ListModel.View()`, when `folderNameByID != nil` (non-empty):

- Add FOLDER column header (12 chars) between FROM and SUBJECT.
- Trim FROM field from 14 chars to 12 chars to fit within the
  terminal width budget.
- Each message row: look up `folderNameByID[msg.FolderID]`, truncate
  to 12 chars.

Clear `m.list.folderNameByID = nil` in `clearFilter()`.

## 5. Muted-message interaction (spec 19 consistency)

`SearchByPredicate` does not apply `ExcludeMuted`. This is intentional
and consistent with spec 19 §4.4:

> "The CLI `inkwell filter` command should pass `ExcludeMuted: false`
> (same reasoning as search: explicit filter is intentional)."

Cross-folder filter results **will** include muted messages. This
matches the search path (FTS5 / hybrid search also includes muted
messages). Users who want to act on muted messages can use `:filter
--all` to surface them. Document in the status bar tip if a muted
indicator appears (the list row already renders the `🔕` glyph per
spec 19 §5.2).

Add an edge-case row (§6) explicitly noting muted behaviour.

## 6. Edge cases

| Case | Behaviour |
|------|-----------|
| `:filter --all` with no pattern after the prefix | Friendly error: `filter: usage :filter [--all] <pattern>`. Pattern empty check is in the dispatcher. |
| `:filter` (no `--all`) with results spanning multiple folders | Runs cross-folder silently (current behaviour). No FOLDER column; hint shows message count only. `filterAllFolders = false`. |
| Pattern is folder-scoped (`~m Inbox`) AND `--all` | `--all` has no effect on query execution (SearchByPredicate is always cross-folder). The `~m Inbox` predicate in the compiled SQL narrows results to Inbox rows. Folder column shows only "Inbox". |
| Result includes muted threads | Shown — SearchByPredicate does not apply ExcludeMuted. Muted rows carry the `🔕` indicator. Consistent with search path (spec 19 §4.3). |
| Cross-folder result includes Junk folder | Junk is excluded by SearchByPredicate's default trash/spam filter. Results will NOT include Junk or Deleted Items messages. |
| `:unfilter` clears cross-folder state | `clearFilter()` resets `filterAllFolders`, `filterFolderCount`, `filterFolderName`, `m.list.folderNameByID`. |
| Result is empty | Same as today: status bar shows `0 matched`. |

## 7. CLI

### 7.1 `inkwell filter` — wire the existing `--all` flag

`cmd_filter.go` already declares `allFolders bool` and registers
`--all`, but the variable is never read (the function always calls
`runFilterListing` with `folderID = ""`). Since `runFilterListing`
is already cross-folder by default, wiring `allFolders` requires no
query change. Wire it to produce proper output when set:

```sh
inkwell filter '~f newsletter@*' --all     # explicit cross-folder (same
                                            # query, but output includes
                                            # per-message folder name)
```

When `allFolders` is true, add a `folders` count map to JSON output
and a Folder column to text output. Building the display-name map
requires resolving Graph folder IDs to display names: call
`app.store.ListFolders(ctx, app.account.ID)` after `runFilterListing`
returns and build `map[string]int` keyed by `DisplayName`:

```go
// After runFilterListing, when allFolders:
folderRows, _ := app.store.ListFolders(ctx, app.account.ID)
nameByID := make(map[string]string, len(folderRows))
for _, f := range folderRows { nameByID[f.ID] = f.DisplayName }
folderCounts := make(map[string]int)
for _, m := range msgs { folderCounts[nameByID[m.FolderID]]++ }
```

JSON output:

```json
{
  "pattern": "~f newsletter@*",
  "all_folders": true,
  "matched": 247,
  "folders": {"Inbox": 120, "Clients": 80, "Promotions": 47},
  "messages": [...]
}
```

Text output adds a folder column between FROM and SUBJECT (matching
the TUI layout).

### 7.2 `inkwell messages --filter ... --all`

`cmd_messages.go` has no `--all` flag today. Add it:

```go
var allFolders bool
cmd.Flags().BoolVar(&allFolders, "all", false,
    "ignore --folder and search all folders")
cmd.MarkFlagsMutuallyExclusive("folder", "all")
```

When `allFolders` is true, pass `folderID = ""` to `runFilterListing`
regardless of `--folder`. Cobra's `MarkFlagsMutuallyExclusive` raises
a usage error if the user passes both.

```sh
inkwell messages --filter '~f newsletter@*' --all --limit 100
```

## 8. Performance

A pattern over the entire mailbox hits the same `SearchByPredicate`
path — account-scoped with no folder filter. The existing
`idx_messages_received` and `idx_messages_from` (spec 02) cover the
common predicates. The spec 02 benchmark gates `Search(q, limit=50)`
over 100k messages at <100ms p95; the predicate path is the same.

No new benchmark is required. If cross-folder queries over large
mailboxes show regressions, the fix is a covering index on
`(account_id, received_at)` — already implied by the existing
`idx_messages_received` index (account_id is not in that index today;
migration can add `idx_messages_account_received` in a follow-up).

## 9. Definition of done

- [ ] Model: `filterAllFolders bool`, `filterFolderCount int`,
      `filterFolderName string`, `foldersByID map[string]store.Folder`
      fields added. `filterAllFolders`/`filterFolderCount`/
      `filterFolderName` cleared in `clearFilter()`. `foldersByID`
      populated in `FoldersLoadedMsg` handler (alongside `m.folders`
      update).
- [ ] `dispatchCommand` `case "filter":` strips `--all` / `-a` prefix,
      sets `filterAllFolders`, guards empty-pattern with a friendly
      error, then calls `runFilterCmd` unchanged.
- [ ] `filterAppliedMsg` handler: when `filterAllFolders`, compute
      distinct folder count from message slice, look up display name(s)
      from `m.foldersByID`; populate `m.list.folderNameByID` when
      `filterFolderCount > 1`, nil otherwise.
- [ ] Status bar hint (§3.2): shows "(Inbox)" for single folder,
      "(N folders)" for multi-folder, nothing extra when
      `filterAllFolders == false`.
- [ ] `confirmBulk` appends "across N folders" to modal title when
      `filterAllFolders && filterFolderCount > 1`.
- [ ] `ListModel` gains `folderNameByID map[string]string`; `View()`
      renders FOLDER column (12 chars) when non-nil; FROM trimmed to
      12 chars in that case.
- [ ] `m.list.folderNameByID` cleared in `clearFilter()`.
- [ ] `inkwell filter --all`: wires `allFolders` variable in
      `cmd_filter.go` to include folder metadata in output (JSON
      `folders` count map; text Folder column). No query change needed.
- [ ] `inkwell messages --filter ... --all`: new `--all` flag; mutually
      exclusive with `--folder` via `MarkFlagsMutuallyExclusive`; when
      set passes `folderID = ""` to `runFilterListing`.
- [ ] Tests:
  - store: `TestSearchByPredicateCrossFolder` (existing coverage, verify
    no folder_id clause in the SQL plan — use `EXPLAIN QUERY PLAN`).
  - dispatch (unit): `TestFilterAllFlagSetsModelField` (`:filter --all ~f x`
    sets `filterAllFolders=true`, pattern is `~f x`); `TestFilterNoPrefixLeavesFieldFalse`
    (`:filter ~f x` leaves `filterAllFolders=false`); `TestFilterAllEmptyPatternError`
    (`:filter --all` with nothing after returns error).
  - dispatch (e2e): `TestFilterAllFolderHintShowsFolderCount` (`:filter --all ~f x`
    against a fixture with messages in two folders → status bar shows
    "(2 folders)"); `TestFilterAllFolderColumnRendered` (list pane shows
    FOLDER header when cross-folder filter is active and result spans >1
    folder); `TestFilterAllConfirmModalIncludesFolderCount` (`;d` confirm
    modal says "across N folders").
  - CLI: `TestFilterCLIAllFlagAddsFolderMetadata` (JSON output includes
    `folders` map when `--all`); `TestMessagesFilterAllOverridesFolder`
    (`--filter x --all` ignores `--folder Inbox`).
- [ ] User docs: `docs/user/reference.md` adds `:filter --all` row to
      the command reference; `docs/user/how-to.md` updates the "Cross-
      folder cleanup" recipe with `--all` syntax.

## 10. Cross-cutting checklist

- [ ] Scopes: none new. `Mail.ReadWrite` already in PRD §3.1.
- [ ] Store reads/writes: `messages` read only (`SearchByPredicate`).
      No mutations beyond the existing filter→bulk-action path.
- [ ] Graph endpoints: spec 09's $batch on apply path; otherwise none.
- [ ] Offline: works fully offline against the local store.
- [ ] Undo: per-message via spec 07 (unchanged from single-folder
      bulk). No new undo entries.
- [ ] User errors: §6 edge-case table. Empty-pattern-after-flag error
      surfaced as status-bar `lastError`.
- [ ] Latency: no new store query path. Cross-folder is the existing
      `SearchByPredicate` default. No new benchmark required (§8).
- [ ] Logs: no new log sites. Existing redaction applies. `filterPattern`
      is not logged (subject lines are not logged; patterns may contain
      address fragments — confirmed not sensitive at DEBUG level per
      existing `runFilterCmd` logging policy).
- [ ] CLI: `inkwell filter --all` and `inkwell messages --filter --all`
      per §7.
- [ ] Tests: §9.
- [ ] **Spec 17 review:** no new external HTTP surface; no new SQL
      composition (same `SearchByPredicate`); no token handling; no
      subprocess; no cryptographic change. The only new surface is
      string-prefix parsing in `dispatchCommand` — no injection risk
      (stripped token is a fixed-string prefix, not user-controlled
      SQL). No threat-model row needed.
- [ ] **Spec 19 consistency:** muted messages appear in cross-folder
      filter results (ExcludeMuted is not applied by
      SearchByPredicate). Documented in §5. Consistent with spec 19
      §4.4 ("explicit filter is intentional").
- [ ] **Spec 20 consistency:** cross-folder filter does not interact
      with thread ops. After `:filter --all`, the `;` bulk-pending and
      `T`-chord operate on the filter result as a flat message list —
      no conversation grouping.
- [ ] **Docs consistency sweep:** `docs/user/reference.md` `:filter`
      row updated; how-to cross-folder recipe updated. No `CONFIG.md`
      change (no new config keys).
