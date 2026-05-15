# Spec 10 — Bulk Operations UX

**Status:** In progress (CI scope, v0.6.x). `:filter <pattern>` + `;d` / `;a` chord + confirm modal + bulk dispatch via BulkExecutor all wired. Residual: 6 of 10 bulk verbs (`;D` / `;m` / `;r` / `;R` / `;f` / `;F` / `;c` / `;C`); preview screen with toggleable checkboxes; progress modal with cancel; result modal with partial-failure breakdown; composite undo; dry-run with `!` suffix; saved-search promotion via `:rule save NAME` — tracked under audit-drain PR 10 + spec 11 work.
**Depends on:** Specs 04 (TUI shell, command line, modals), 07 (action types and executor), 08 (pattern compile/execute), 09 (batch engine).
**Blocks:** Spec 11 (saved searches reuse the filter UI).
**Estimated effort:** 2–3 days.

---

## 1. Goal

Implement the user-facing bulk-triage workflow: filter a folder by pattern, preview the matched set, apply an action to all matches with a single keystroke, get a single composite undo entry, and have the operation complete in a few seconds.

This is the **conceptual core of why we're building this app rather than using existing TUIs**. Mutt has the verbs but not the server-side awareness; aerc has filter mode but only against IMAP. We get pattern-based bulk ops against the full Microsoft 365 mailbox with optimistic UI and proper undo.

## 2. The user flow

```
User wants to delete all newsletters older than 30 days.

1. User presses :     → command mode
2. User types: filter ~f newsletter@* & ~d <30d
3. Press Enter        → list pane narrows to matches
                         status: "247 matched"
                         (server still loading more, count climbs)
4. User reviews top of list (sanity check)
5. User presses ;d    → "Delete 247 messages? [y/N]"
6. User presses y     → progress: "Deleting... 47/247"
                         (~5 seconds)
                       → "✓ Deleted 247 messages. Press u to undo."
7. (Optional) User presses u → messages restored
```

Total elapsed time from "I want to clean these up" to "they're gone": about 15 seconds. The same task in Outlook for Mac requires creating a search folder, multi-selecting, hitting delete, confirming. ~2 minutes.

## 3. Module layout

```
internal/ui/
├── filter.go         # Filter mode: parse, run, narrow list
├── bulk.go           # ; prefix handling, action-on-filtered dispatch
├── preview.go        # Match preview / dry-run modal
├── progress.go       # Bulk progress modal
└── undo_overlay.go   # Undo stack visualizer (extension of spec 07)
```

The pattern parsing/execution lives in `internal/pattern` (spec 08). The batch execution lives in `internal/graph/batch.go` and `internal/action/executor.go` (spec 09). This spec is the glue and the UX.

## 4. Filter mode

### 4.1 Entering filter mode

Three entry points:

1. **`:filter <pattern>`** — typed in command mode. Most discoverable.
2. **`F` (capital)** — keybinding shortcut that opens command mode pre-filled with `filter `, ready for the pattern.
3. **`/<pattern>`** — search mode (spec 06) automatically becomes a filter when the user presses Enter and the result count is reasonable. Distinction: `/` searches but does not narrow; `:filter` narrows. After a `/` search, pressing `F` converts the search query into an equivalent filter.

Patterns can be plain text (treated as `~B <text>`) or use the operator language from spec 08:

```
:filter newsletter                       → ~B newsletter
:filter ~f newsletter@*                  → operator pattern
:filter ~f newsletter@* & ~d <30d        → composed pattern
```

### 4.2 Running the filter

```go
func (m *Model) runFilter(src string) tea.Cmd {
    return func() tea.Msg {
        compiled, err := pattern.Compile(src, pattern.CompileOptions{
            DefaultFolderID: m.list.folderID,
            PreferLocal:     m.cfg.Pattern.PreferLocalWhenOffline && !m.network.online(),
        })
        if err != nil {
            return filterErrorMsg{err: err}
        }
        ids, err := pattern.Execute(m.ctx, compiled, m.store, m.graph)
        if err != nil {
            return filterErrorMsg{err: err}
        }
        return filterAppliedMsg{compiled: compiled, ids: ids}
    }
}
```

While the filter runs, the status line shows `[filtering…]`. Local strategies return immediately (<100ms); server strategies stream results — see §4.4.

### 4.3 Filter applied state

Once `filterAppliedMsg` arrives:

```go
m.list.filter = compiled
m.list.filteredIDs = ids
m.list.refresh()  // re-renders with only the filtered subset
m.status.transient = fmt.Sprintf("%d matched · :filter clear to exit", len(ids))
```

The list pane displays only matched messages. The folders pane is unchanged (filters are scoped to the current folder by default). The status line shows the match count and a hint.

A small `[filter: ~f newsletter@* & ~d <30d]` badge appears at the top of the list pane so the user always knows what's filtering.

### 4.4 Streaming server results

For server-strategy patterns, the executor returns IDs in batches as Graph paginates. The UI updates progressively:

```
14:32  newsletter@a.com    Deal of the day...     ← appears immediately (local)
13:18  newsletter@b.com    Weekly digest...
...
[filter: ~f newsletter@* & ~d <30d] · 47 matched (loading more...)

⋮ a few seconds later ⋮

[filter: ~f newsletter@* & ~d <30d] · 247 matched (complete)
```

Implementation: spec 08's `Execute` returns a `<-chan []string` for streaming strategies; `runFilter` consumes it and emits `filterPartialMsg` updates.

### 4.5 Exiting filter mode

Three ways:

- `Esc` (in normal mode with filter active) → clear filter, restore full list.
- `:filter clear` or `:filter` (no argument) → same.
- `:filter <new pattern>` → replace the current filter.

### 4.6 Single-message triage on a filtered list

Pressing a single-message triage key (`d`, `r`, `R`, `f`, `a`, `D`)
on a filtered list **acts on the focused row only**, not on the
whole filtered set. The bulk verbs are armed via the `;` prefix
(spec §5.1); plain `d` is the same single-message dispatch as in a
normal folder view.

After the dispatch lands, the list pane re-runs the active filter
(NOT the underlying folder) so the post-state is reflected against
the same pattern. The list pane's `m.list.FolderID` while filtered
is a sentinel string `filter:<pattern>`; reloading via the normal
`loadMessagesCmd(folderID)` path against that sentinel returns
zero rows and makes the user think every filtered message
disappeared. This was the v0.13.x real-tenant bug (`d` on a
filtered list looked like it deleted everything; only the focused
row was actually mutated).

Status bar after a successful single-message triage on a filtered
list: `✓ <action> · u to undo`. The `u` key (spec 07 §11) reverses
the most recent triage regardless of filter state — undoing a
soft-delete brings the message back to its source folder.

| Action            | What it does on a filtered list                          |
| ----------------- | -------------------------------------------------------- |
| `d`               | Soft-delete the focused row; filter re-runs.             |
| `;d`              | Soft-delete the entire filtered set (with confirm).      |
| `D`               | Permanent-delete the focused row (with confirm; not undoable). |
| `r` / `R`         | Mark read / unread the focused row.                      |
| `f`               | Toggle flag on the focused row.                          |
| `a`               | Archive the focused row; filter re-runs.                 |
| `;a`              | Archive the entire filtered set (with confirm).          |
| `u`               | Undo the most recent triage (works regardless of filter state). |

After clearing: the list pane returns to showing the folder's full message list, sorted by `received_at DESC`.

## 5. The `;` prefix: action-on-filtered

When a filter is active and the user presses an action key with `;` prefix, the action applies to the entire filtered set, not just the focused message.

### 5.1 Bindings

| Sequence | Action |
| --- | --- |
| `;d` | Soft-delete all filtered |
| `;D` | Permanent-delete all filtered (always confirms) |
| `;a` | Archive all filtered |
| `;m` | Move all filtered (opens folder picker) |
| `;r` | Mark all filtered as read |
| `;R` | Mark all filtered as unread |
| `;f` | Flag all filtered |
| `;F` | Unflag all filtered |
| `;c` | Add category to all filtered (opens category picker) |
| `;C` | Remove category from all filtered |

The `;` is the **tag-prefix**, borrowed verbatim from Mutt. Mutt-trained users will recognize it instantly.

### 5.2 The dispatch

```go
func (m *Model) handleBulkAction(action ActionType, params map[string]any) tea.Cmd {
    if m.list.filter == nil {
        // Without a filter, ; falls back to the focused message
        return m.handleSingleAction(action, params)
    }
    ids := m.list.filteredIDs
    return m.confirmBulk(action, ids, params)
}
```

### 5.3 Confirmation modal

Before any bulk operation runs, a confirmation modal appears showing the count, action description, and a sample of affected messages.

```
   ╭─────────────────────────────────────────────────────────────╮
   │  Delete 247 messages?                                        │
   │                                                              │
   │  Filter: ~f newsletter@* & ~d <30d                           │
   │  Action: Move to Deleted Items (recoverable for 30 days)     │
   │                                                              │
   │  Sample of affected messages:                                │
   │    Sun 14:32  newsletter@vendor.com  Deal of the day         │
   │    Sat 09:18  newsletter@news.com    Weekly digest #4521     │
   │    Fri 12:04  newsletter@blog.com    New posts from your...  │
   │    Thu 16:45  newsletter@vendor.com  Limited time offer      │
   │    Wed 11:22  newsletter@analytics.. Your weekly report      │
   │    ... and 242 more                                          │
   │                                                              │
   │  [y] Yes, proceed     [n] Cancel     [p] Preview all         │
   ╰─────────────────────────────────────────────────────────────╯
```

The sample is the first 5 messages of the filtered set (already in `m.list.filteredIDs[:5]`). The "Preview all" option opens a dedicated preview screen.

#### 5.3.1 When confirmation is required

Confirmation is shown when ANY of:

- Filtered set size > `[triage].confirm_threshold` (default 10).
- Action type is `permanent_delete` (always confirms regardless of count).
- The user has set `[ui].confirm_destructive_default = "yes"` and the action is destructive.

For small bulk operations (fewer than threshold) of non-permanent actions, no confirmation is needed; the action proceeds immediately. This is consistent with Mutt's flow where `;d` on 3 messages is just three deletes.

#### 5.3.2 Confirm prompt defaults

The default selection in the confirm modal is **No** — pressing Enter without a selection cancels. The user must explicitly press `y` to proceed. Configurable to default-yes via `[ui].confirm_destructive_default = "yes"`, but discouraged.

For permanent delete, the default is always No regardless of config.

### 5.4 Preview screen

`p` from the confirm modal (or `:preview` standalone) opens a full-pane preview:

```
Preview · 247 messages affected by ;d (soft delete)

Filter: ~f newsletter@* & ~d <30d

j/k scroll · u uncheck · y confirm · n cancel · / search within preview

[✓] Sun 14:32  newsletter@vendor.com    Deal of the day - up to 50% off...
[✓] Sat 09:18  newsletter@news.com      Weekly digest #4521 - this week...
[✓] Fri 12:04  newsletter@blog.com      New posts from your subscriptions
[✓] Thu 16:45  newsletter@vendor.com    Limited time offer expires soon
[✓] Wed 11:22  newsletter@analytics...  Your weekly report is ready
[✓] Wed 09:08  newsletter@vendor.com    Today's newsletter
[✓] Tue 15:30  newsletter@news.com      Daily briefing
... 240 more
```

Each row has a checkbox. Pressing `u` toggles the checkbox on the current row, removing it from the action set. The user can run `j`/`k` to scroll, `Space` to page through, `/` to incrementally narrow.

Pressing `y` confirms with the *checked* set; pressing `n` cancels entirely.

The preview is also where the user sees the EXACT messages affected, removing any ambiguity from a pattern that might over-match.

## 6. Dry-run mode

For users who want to verify a pattern without any chance of applying it:

```
:filter --dry-run ~f newsletter@* & ~d <30d
```

Or, if `[batch].dry_run_default = true`:

```
:filter ~f newsletter@*           # dry-run by default
:filter ~f newsletter@* !          # ! suffix to actually apply
```

Dry-run mode:

- Filter applies normally.
- Any subsequent `;<action>` shows a "would affect 247 messages" toast and does NOT execute.
- The user explicitly types `:bulk apply` (or presses `!` after `;d`) to actually execute.

This adds a layer of safety for users who are wary of pattern mistakes. Off by default; for safety-conscious users, the `[batch].dry_run_default = true` config makes it the standard mode and they explicitly opt in to apply.

## 7. Progress display

For bulk operations >50 items, a progress modal appears:

```
   ╭─────────────────────────────────────────────────────────────╮
   │  Deleting 247 messages...                                    │
   │                                                              │
   │  ████████████████████░░░░░░░░░░░░░░░░░░░░░  47/247  19%      │
   │                                                              │
   │  Press Esc to cancel (operation already started)             │
   ╰─────────────────────────────────────────────────────────────╯
```

The progress bar updates from the `OnProgress` callback in `BatchExecutor.ExecuteAllOpts`. Updates are debounced to 10Hz max.

For small bulks (≤50 items), no modal — they finish so fast that opening and closing a modal is more disruptive than it's worth.

### 7.1 Cancellation

Pressing `Esc` during progress sends a context cancel to the bulk executor. The executor finishes any in-flight batch HTTP calls (we don't abort mid-HTTP), then stops issuing new ones. The result modal appears with partial results:

```
   ╭─────────────────────────────────────────────────────────────╮
   │  Cancelled.                                                  │
   │                                                              │
   │  47 of 247 messages deleted.                                 │
   │  200 messages were not affected.                             │
   │                                                              │
   │  Press u to undo the 47 deletions.                           │
   ╰─────────────────────────────────────────────────────────────╯
```

Important: the 47 already-deleted messages CAN be undone via the composite undo entry (which captures only the successful subset).

## 8. Result modal

After a bulk operation completes:

### Success case

```
   ╭─────────────────────────────────────────────────────────────╮
   │  ✓ Deleted 247 messages.                                     │
   │                                                              │
   │  Duration: 4.8s                                              │
   │  Press u to undo.                                            │
   ╰─────────────────────────────────────────────────────────────╯
```

This auto-dismisses after 5 seconds (configurable via `[ui].transient_status_ttl`). The user can press any key to dismiss earlier.

### Partial-failure case

```
   ╭─────────────────────────────────────────────────────────────╮
   │  ⚠ Deleted 240 of 247 messages.                              │
   │                                                              │
   │  7 messages failed:                                          │
   │    • 4 returned 403 (permission denied — likely shared       │
   │      mailbox messages outside delegated scope)               │
   │    • 3 returned 410 (message no longer exists)               │
   │                                                              │
   │  Press l to see the failed messages.                         │
   │  Press u to undo the 240 successful deletions.               │
   ╰─────────────────────────────────────────────────────────────╯
```

This does NOT auto-dismiss — the user should see and acknowledge it.

### Pending case (some throttled out)

```
   ╭─────────────────────────────────────────────────────────────╮
   │  ⏳ Deleted 200 of 247 messages.                              │
   │                                                              │
   │  47 messages are queued for retry due to throttling.         │
   │  They will complete in the background.                       │
   │                                                              │
   │  Press u to undo the 200 successful deletions.               │
   ╰─────────────────────────────────────────────────────────────╯
```

The pending 47 are now in the action queue (status: `Pending`) and will drain on the next sync engine cycle.

## 9. Bulk-specific keymap (additions to spec 04)

| Key | Mode | Action |
| --- | --- | --- |
| `F` | Normal | Open command mode pre-filled with `filter ` |
| `;` (prefix) | Normal, with filter active | Next action key applies to entire filtered set |
| `Esc` | During progress | Cancel ongoing bulk operation |
| `Esc` | Normal, with filter active | Clear filter |
| `p` | In confirm modal | Open preview screen |
| `u` (in preview) | Preview screen | Toggle current message's checkbox |
| `y` / `n` | In confirm modal or preview | Yes / No |
| `!` | After `;d`/`;D` etc. when in dry-run mode | Force-apply despite dry-run |

### 9.1 The `F` key — resolution with spec 07

`F` (i.e. `Shift+f`) belongs to bulk filter mode in the list pane. Spec 07 §12 documents the resolution:

- `f` (lowercase) toggles flag in list pane, opens forward in viewer pane (pane-scoped).
- `F` (Shift+f) opens filter command mode in list pane.
- There is no `Shift+F` binding (would be a no-op keystroke — terminals can't distinguish it from `Shift+f`).

This means individual flagging is a single-key toggle (`f` to flag, `f` again to unflag), which is the universal expectation. Bulk filter gets its own dedicated binding (`F`). Forward gets a one-key shortcut in the viewer pane where `f` is unambiguous.

The Action types `Flag` and `Unflag` remain distinct in spec 07 §3 for explicit programmatic use (CLI, `:flag` command).

## 10. Saved-search promotion

If the user runs the same `:filter` pattern repeatedly, the TUI suggests promoting it to a saved search:

```
Status: Filter applied 4 times this session — :rule save <name>?
```

`:rule save Newsletters ~f newsletter@*` creates a named saved search (spec 11). The next invocation can use `:filter Newsletters` (named) instead of re-typing the pattern.

This is a small but high-leverage UX touch — it pushes power users toward the persistent toolbox naturally.

## 11. Error handling

| Scenario | Behavior |
| --- | --- |
| Pattern parse error | Status line shows error verbatim; filter not applied. |
| Pattern compiles but execution fails (server unavailable) | Toast: "Filter failed: <reason>"; if local-strategy was viable as fallback, automatically retry local-only with status indicator. |
| Pattern matches more than `bulk_size_hard_max` (5000) | Confirm modal becomes a refusal: "Pattern matches 8,432 messages, exceeding the 5,000 limit. Refine your pattern or run `:backfill` and apply per-folder." |
| Filtered list opened in viewer, message disappears mid-bulk (because bulk deleted it) | Viewer pane shows "Message no longer exists"; cursor returns to list. |
| Bulk operation while sync engine is mid-cycle | OK — they share the concurrency budget; bulk just runs slightly slower. No correctness issue. |
| User attempts ; without an active filter | Falls back to single-message action on focused row (with a one-time hint: "; with no filter applies to the focused message"). |
| Network drops during bulk | All in-flight batches complete or fail; remaining queued actions are picked up by sync engine drain on reconnect. User sees pending case (§8). |
| User undoes a bulk that had failed messages | Only the successful subset is undone. The undo entry's `MessageIDs` is the successful list captured at execute time. |

## 12. Configuration

This spec owns no `[section]` of its own — it composes consumer keys from other specs.

Keys consumed:

| Key | Owner | Used in § |
| --- | --- | --- |
| `[triage].confirm_threshold` | 07 | §5.3.1 |
| `[triage].confirm_permanent_delete` | 07 | §5.3.1 |
| `[ui].confirm_destructive_default` | 04 | §5.3.2 |
| `[batch].dry_run_default` | 09 | §6 |
| `[batch].bulk_size_warn_threshold` | 09 | §11 (warn) |
| `[batch].bulk_size_hard_max` | 09 | §11 (refuse) |

New keys this spec adds:

| Key | Default | Used in § |
| --- | --- | --- |
| `bulk.preview_sample_size` | `5` | §5.3 |
| `bulk.progress_threshold` | `50` | §7 (when to show modal) |
| `bulk.progress_update_hz` | `10` | §7 (debounce) |
| `bulk.suggest_save_after_n_uses` | `4` | §10 |

Bindings (`F`, `;`, etc.) live in `[bindings]` per spec 04 conventions.

## 13. CLI mode equivalent

A bulk operation as a one-shot from the shell, for scripting:

```bash
# Dry-run by default in CLI mode
inkwell filter ~f newsletter@* --action delete --since 30d

# With explicit confirmation flag
inkwell filter ~f newsletter@* --action delete --since 30d --apply

# JSON output for piping
inkwell filter ~f newsletter@* --output json | jq '.matches'

# Save as a named rule
inkwell rule save Newsletters --pattern '~f newsletter@*'

# Apply a saved rule
inkwell rule apply Newsletters --action delete --apply
```

CLI mode disables interactive confirmations by default (it's noninteractive); `--apply` is the explicit "yes I mean it" flag. Without `--apply`, the command prints what would happen and exits.

Spec 14 documents the full CLI command set; this spec documents only the bulk subset.

## 14. Performance budgets

| Operation | Target |
| --- | --- |
| `:filter` parse + compile | <10ms |
| Filter execution, local strategy, 100k cache | <200ms |
| Filter execution, server strategy, first results | <2s |
| Filter execution, server strategy, complete | <5s for typical patterns |
| Confirm modal open after `;d` | <50ms (just rendering) |
| 100-message bulk soft-delete, end-to-end | <3s |
| 1000-message bulk soft-delete, end-to-end | <20s |
| Composite undo of 247-message bulk | same as forward bulk |

## 15. Test plan

### Unit tests

- Confirm threshold logic: action × count → confirm-or-not table.
- Bulk dispatcher: with no filter falls back to single; with filter applies to set.
- Result classification: success / partial-failure / pending categorization based on per-sub-request results.
- Composite undo: only successful IDs included.

### Integration tests with `teatest`

- Type `:filter ~f newsletter@*` → list narrows.
- Press `;d` → confirm modal appears.
- Press `y` → progress modal → result modal.
- Press `u` → messages restored; result modal updates.
- Press `Esc` during progress → cancellation flow.
- Open preview, uncheck a few, confirm; assert only checked subset acted on.
- Dry-run mode: `;d` shows toast, doesn't execute.

### Manual smoke tests

1. Filter inbox by `~f newsletter@*`, verify count matches Outlook server-side filter.
2. `;d` 50 newsletters; verify all 50 gone in Outlook within 30 seconds.
3. `u` to undo; verify all 50 restored.
4. Filter by complex pattern (`~f *@vendor.com & ~A & ~U & ~d <90d`); preview; uncheck 3; apply; verify N-3 affected.
5. Run a bulk that triggers throttling (artificially: very large bulk); verify pending state and eventual completion via sync drain.
6. Cancel a bulk mid-flight; verify partial completion and undo of partial.
7. Try a pattern that matches 6000 messages; verify hard-max refusal.

## 16. Definition of done

- [ ] Filter mode works for all spec-08 pattern strategies.
- [ ] List pane narrows to filtered IDs; badge shows filter; status shows count.
- [ ] `;` prefix dispatches all action types in spec 07's bulk-able subset.
- [ ] Confirm modal renders with correct sample, count, and reasoning.
- [ ] Preview screen with toggleable checkboxes works for sets up to 5000.
- [ ] Progress modal updates during bulk; cancel works.
- [ ] Result modal correctly categorizes success/partial/pending.
- [ ] Composite undo restores only the successful subset.
- [ ] Dry-run mode prevents accidental applies.
- [ ] All keybindings from §9 wired up.
- [ ] All performance budgets met.
- [ ] All manual smoke tests pass.

## 17. Out of scope for this spec

- Saved searches and virtual folders (spec 11).
- CLI mode `inkwell filter ...` flow (spec 14 covers in detail).
- Cross-folder bulk operations. v1 scopes filters to current folder; future enhancement.
- Conditional bulk (e.g., "delete if older than 30 days OR mark read otherwise" — branching). Out of scope; users compose two passes.
- Scheduled bulks ("delete this every Friday"). Not in v1.
- Undo of an undo (redo). v1 has one-way undo per spec 07.
