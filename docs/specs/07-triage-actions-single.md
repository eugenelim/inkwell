# Spec 07 — Single-Message Triage Actions

**Status:** Shipped (CI scope, v0.3.x → v0.13.x). All 13 action types operational: mark-read/unread, flag/unflag, soft-delete, archive, move (with folder picker — PR 4c), permanent-delete (PR 4a), add/remove category (PR 4b), and PR 7-i/7-iii's four draft-creation kinds. Undo wired (PR 1) with computeInverse covering every reversible type. Residual: replay-on-startup of mid-flight non-draft actions; lifecycle InFlight transition is currently fast-forwarded through; the `/move` path doesn't update the local row's primary key on Graph's id reassignment (heals on next delta).
**Depends on:** Specs 02 (action queue, undo tables), 03 (action draining in sync engine), 04 (TUI keymap).
**Blocks:** Specs 09 (batch executor reuses action types), 10 (bulk ops UX builds on these bindings).
**Estimated effort:** 2 days.

---

## 1. Goal

Implement single-message write operations: mark read/unread, flag/unflag, move, soft-delete, permanent-delete, archive, categorize, and create draft replies. Each operation:

1. Applies optimistically to the local store (instant UI feedback).
2. Enqueues a Graph mutation through the action executor.
3. Reconciles on response (rolls back on failure).
4. Pushes onto the undo stack (where reversible).

This spec establishes the **action plumbing pattern** that bulk operations (spec 10) will reuse with `$batch`. Get this right and bulk is a thin layer on top.

## 2. Module layout

```
internal/action/
├── action.go         # Action types, Params, status enums
├── executor.go       # Single-action and batch dispatchers
├── apply_local.go    # Optimistic local store mutations
├── inverse.go        # Compute inverse for undo
├── replay.go         # On-startup recovery from in-flight actions
└── types.go          # Per-action-type Graph translation

internal/ui/triage.go # Keybinding handlers wiring UI → action.Executor
```

## 3. Action types

```go
package action

type Type string

const (
    TypeMarkRead         Type = "mark_read"
    TypeMarkUnread       Type = "mark_unread"
    TypeFlag             Type = "flag"
    TypeUnflag           Type = "unflag"
    TypeMove             Type = "move"
    TypeSoftDelete       Type = "soft_delete"
    TypePermanentDelete  Type = "permanent_delete"
    TypeArchive          Type = "archive"           // alias for Move to archive folder
    TypeAddCategory      Type = "add_category"
    TypeRemoveCategory   Type = "remove_category"
    // Draft creation types (CreateDraft, CreateDraftReply, CreateDraftReplyAll,
    // CreateDraftForward, DiscardDraft) are owned by spec 15. They share this
    // Type enum because they flow through the same executor; their semantics
    // and Params live there.
)

type Action struct {
    ID         string         // local UUIDv4
    Type       Type
    MessageIDs []string       // single-message specs use len() == 1; bulk reuses with len() ≥ 1
    Params     map[string]any // type-specific parameters as JSON-friendly values
    Status     Status
    CreatedAt  time.Time
}

type Status string
const (
    StatusPending  Status = "pending"
    StatusInFlight Status = "in_flight"
    StatusDone     Status = "done"
    StatusFailed   Status = "failed"
)
```

The `Params` map carries action-specific data:

| Type | Params keys |
| --- | --- |
| `move`, `archive` | `destination_folder_id` (string) |
| `add_category`, `remove_category` | `category` (string) |
| `flag` | `due_date` (RFC3339 string, optional) |
| draft creation types | see spec 15 (compose / reply) |
| (others) | — |

## 4. Executor API

```go
package action

type Executor interface {
    // Execute applies an action: local mutation immediately, server mutation
    // dispatched asynchronously, undo entry pushed if reversible.
    Execute(ctx context.Context, a Action) error

    // ExecuteBulk batches multiple actions (used by spec 10).
    ExecuteBulk(ctx context.Context, as []Action) error

    // Undo pops the top undo entry and applies its inverse.
    Undo(ctx context.Context) error

    // PeekUndo returns the label of the top undo entry, or empty string if none.
    PeekUndo(ctx context.Context) (string, error)

    // ReplayPending re-runs actions that were Pending or InFlight at last shutdown.
    // Called once at startup.
    ReplayPending(ctx context.Context) error
}

func New(store store.Store, graph graph.Client, cfg *config.Config, events chan<- Event) Executor
```

`Event`s flow back to the UI:

```go
type ActionAppliedEvent  struct{ ActionID string; Type Type; MessageIDs []string }
type ActionConfirmedEvent struct{ ActionID string }
type ActionFailedEvent    struct{ ActionID string; Reason string; RolledBack bool }
type UndoAppliedEvent     struct{ Label string }
```

## 5. The single-action lifecycle (canonical flow)

```
User presses 'd' on a message
    │
    ▼
ui.handleDelete(msg) ──► Executor.Execute(Action{Type: SoftDelete, MessageIDs: [id]})
    │
    ▼
[1] Persist Action to actions table (status=Pending)
[2] Optimistically apply to store:
        msg.parent_folder_id = deletedItemsFolder.id
        store.UpdateMessageFields(...)
[3] Push inverse onto undo stack:
        UndoEntry{label="Restore message X", action=Move(originalFolderID)}
[4] Emit ActionAppliedEvent → UI re-renders without the message
[5] Mark action InFlight; dispatch Graph call:
        POST /me/messages/{id}/move {"destinationId":"deleteditems"}
[6a] On 2xx: action → Done; emit ActionConfirmedEvent
[6b] On 4xx/5xx: rollback local store (move msg back); emit ActionFailedEvent
       - Action stays Pending if retryable (5xx, 429, network)
       - Action → Failed if non-retryable (404, 403)
       - Pop the undo entry (it was an optimistic apply that failed)
```

## 6. Per-action-type implementations

For each type, three things are defined: (1) local mutation, (2) Graph translation, (3) inverse for undo.

### 6.1 `mark_read`

- **Local:** `UpdateMessageFields(id, {is_read: true})`.
- **Graph:** `PATCH /me/messages/{id} {"isRead": true}`.
- **Inverse:** `mark_unread`.
- **Idempotent:** yes. Re-applying after crash is safe.

### 6.2 `mark_unread`

- **Local:** `is_read: false`.
- **Graph:** `PATCH /me/messages/{id} {"isRead": false}`.
- **Inverse:** `mark_read`.

### 6.3 `flag`

- **Local:** `flag_status: "flagged"`, `flag_due_at: <param>` if provided.
- **Graph:** `PATCH /me/messages/{id} {"flag":{"flagStatus":"flagged","dueDateTime":...}}`.
- **Inverse:** `unflag`.

### 6.4 `unflag`

- **Local:** `flag_status: "notFlagged"`, clear `flag_due_at`, `flag_completed_at`.
- **Graph:** `PATCH /me/messages/{id} {"flag":{"flagStatus":"notFlagged"}}`.
- **Inverse:** `flag` with the original due date (captured pre-mutation).

### 6.5 `move`

- **Local:** `parent_folder_id: <destination_folder_id>`. Also: messages move out of the current folder view.
- **Graph:** `POST /me/messages/{id}/move {"destinationId": "<id>"}`.
  - Note: `destinationId` accepts either a folder ID or a well-known name (`"inbox"`, `"deleteditems"`, `"archive"`, `"junkemail"`, etc.).
- **Inverse:** `move` back to the original `parent_folder_id` (captured pre-mutation).
- **Subtle:** Graph's `/move` returns a NEW message ID for the moved item (the original ID is invalidated). Our store update needs to update the row's primary key. Since SQLite primary keys aren't easily updated, we delete the original row and insert with the new ID, preserving all other fields.

### 6.6 `soft_delete`

- **Local:** equivalent to `move` to Deleted Items.
- **Graph:** `POST /me/messages/{id}/move {"destinationId":"deleteditems"}`.
- **Inverse:** `move` back to original folder.
- Same ID-change behavior as `move`.

### 6.7 `permanent_delete`

- **Local:** `store.DeleteMessage(id)`.
- **Graph:** `POST /me/messages/{id}/permanentDelete` (Graph endpoint added April 2025; bypasses Recoverable Items).
- **Inverse:** **none.** This is the only non-reversible action. Undo stack is NOT pushed.
- **Confirmation:** mandatory unless `[triage].confirm_permanent_delete = false` (which we discourage; default `true`).

### 6.8 `archive`

- Alias for `move` with `destination_folder_id` resolved from `[triage].archive_folder` config (default well-known `archive`).
- Otherwise identical to `move`.

### 6.9 `add_category`

- **Local:** append `<category>` to `categories` JSON array if not already present.
- **Graph:** `PATCH /me/messages/{id} {"categories": [...all current categories plus new...]}`.
  - Graph requires the FULL categories array; you cannot add one. The executor reads current categories from local cache and constructs the full list.
- **Inverse:** `remove_category` with the same value.

### 6.10 `remove_category`

- **Local:** remove `<category>` from `categories` JSON array.
- **Graph:** `PATCH /me/messages/{id} {"categories": [...current minus removed...]}`.
- **Inverse:** `add_category`.

### 6.11 Draft creation (`create_draft`, `create_draft_reply`, `create_draft_reply_all`, `create_draft_forward`)

Moved to **spec 15** (compose / reply). They share this executor's optimistic-apply + queue + replay machinery but the editor lifecycle, body templating, header parsing, and Outlook hand-off are substantial enough to live in their own spec. See `docs/specs/15-compose-reply.md`.

## 7. Optimistic UI rules

The "apply local first, then dispatch" pattern relies on a few invariants:

1. **Local apply must be cheap and synchronous.** A single SQLite write per action is the budget.
2. **Local apply must be reversible.** For every local mutation, we know how to undo it (using the pre-mutation snapshot captured at `Execute` time).
3. **The UI re-renders from the store.** It does not hold its own copy of message state. Optimistic apply → store update → UI reads new state → re-render. This means optimistic and confirmed states are indistinguishable from the UI's perspective; they only diverge if a rollback fires.
4. **Rollback is rare and visible.** When rollback happens, the user sees a status-line warning and the message visibly "comes back." This is jarring on purpose: the user notices and can react.

### 7.1 Pre-mutation snapshot

Before applying optimistically, the executor captures the relevant fields for inverse computation:

```go
func (e *executor) Execute(ctx context.Context, a Action) error {
    // Read current state for snapshot
    snapshots, err := e.snapshotMessages(ctx, a.MessageIDs)
    if err != nil { return err }

    // Compute inverse
    inverse := computeInverse(a, snapshots)

    // Persist action + push undo (in single transaction)
    tx := e.store.BeginTx()
    tx.EnqueueAction(a)
    if inverse != nil {
        tx.PushUndo(UndoEntry{
            Label: humanLabel(a),
            Type:  inverse.Type,
            MessageIDs: a.MessageIDs,
            Params: inverse.Params,
        })
    }

    // Apply local mutation
    if err := applyLocal(tx, a, snapshots); err != nil {
        tx.Rollback()
        return err
    }
    tx.Commit()

    // Emit event for UI re-render
    e.events <- ActionAppliedEvent{...}

    // Dispatch Graph call asynchronously
    go e.dispatch(ctx, a, snapshots)
    return nil
}
```

The snapshot is what enables both inverse-undo and rollback-on-failure with the same data.

## 8. Server dispatch and error handling

```go
func (e *executor) dispatch(ctx context.Context, a Action, snap []messageSnapshot) {
    e.store.UpdateActionStatus(ctx, a.ID, StatusInFlight, "")
    defer func() {
        // Always update status on exit, even on panic
    }()

    err := e.callGraph(ctx, a)
    if err == nil {
        e.store.UpdateActionStatus(ctx, a.ID, StatusDone, "")
        e.events <- ActionConfirmedEvent{ActionID: a.ID}
        return
    }

    classification := classifyError(err)
    switch classification {
    case errRetryable:
        // Stay in InFlight; sync engine will retry on next tick
        e.store.UpdateActionStatus(ctx, a.ID, StatusPending, err.Error())
    case errNotFound:
        // Server already in target state (e.g., deleting a deleted message); treat as success
        e.store.UpdateActionStatus(ctx, a.ID, StatusDone, "")
        e.events <- ActionConfirmedEvent{ActionID: a.ID}
    case errUnrecoverable:
        // Roll back local; surface to user
        e.rollback(ctx, a, snap)
        e.store.UpdateActionStatus(ctx, a.ID, StatusFailed, err.Error())
        e.events <- ActionFailedEvent{ActionID: a.ID, Reason: err.Error(), RolledBack: true}
    }
}
```

### 8.1 Error classification

| Graph response | Classification | Action |
| --- | --- | --- |
| 2xx | success | Done |
| 401 (Unauthorized) | retryable | Auth transport refreshes; retried |
| 403 (Forbidden) | unrecoverable | Roll back; surface "permission denied" |
| 404 (Not Found) | success-equivalent | Server is in target state |
| 409 (Conflict) | retryable once | Refresh local from delta, retry |
| 410 (Gone) | unrecoverable | Roll back; the resource changed shape |
| 423 (Locked) | retryable | Wait and retry |
| 429 (Throttled) | retryable | Throttle transport handles; UI shows "throttled" |
| 5xx | retryable | Throttle transport handles |
| Network error | retryable | Stay Pending; resume next tick |

### 8.2 Rollback

```go
func (e *executor) rollback(ctx context.Context, a Action, snap []messageSnapshot) {
    tx := e.store.BeginTx()
    for _, s := range snap {
        tx.UpdateMessageFromSnapshot(s)
    }
    // Remove the undo entry we pushed (this apply turned out to be a no-op)
    tx.PopUndoIfMatches(a.ID)
    tx.Commit()
}
```

The user sees the message reappear with its original state. A status-line warning explains why: `⚠ Could not flag "Q4 forecast": permission denied`.

## 9. Draft composition flow

Moved to **spec 15** (compose / reply). The editor lifecycle, body templating, header parsing, confirmation pane, and Outlook hand-off all live there. This spec only owns the executor / queue plumbing the draft actions ride on.

## 10. Replay on startup

`Executor.ReplayPending(ctx)` runs once at startup and re-dispatches any `Pending` or `InFlight` actions:

```go
func (e *executor) ReplayPending(ctx context.Context) error {
    pending, err := e.store.PendingActions(ctx)
    if err != nil { return err }
    for _, a := range pending {
        // Note: snapshot is no longer accurate (server may have already processed).
        // Re-running is safe because all actions are idempotent.
        // We do NOT rollback on failure during replay — the local state is
        // already the post-action state and matches the server (probably).
        go e.dispatch(ctx, a, nil) // no snapshot = no rollback
    }
    return nil
}
```

The trade-off: if replay fails, the local state may be inconsistent with the server. We accept this because the next delta sync on the affected folder will reconcile. Replay errors are logged but do not surface to the user as warnings (too noisy and usually self-healing).

## 11. Undo

```go
func (e *executor) Undo(ctx context.Context) error {
    entry, err := e.store.PopUndo(ctx)
    if err != nil { return err }
    if entry == nil {
        e.events <- ToastEvent{Msg: "Nothing to undo"}
        return nil
    }
    // The undo entry IS an action. Execute it as one — but DO NOT push a
    // "redo" entry. Undo is one-way in v1.
    inverseAction := Action{
        ID: uuid.New(),
        Type: entry.Type,
        MessageIDs: entry.MessageIDs,
        Params: entry.Params,
    }
    if err := e.executeWithoutUndoPush(ctx, inverseAction); err != nil {
        return err
    }
    e.events <- UndoAppliedEvent{Label: entry.Label}
    return nil
}
```

The `executeWithoutUndoPush` flag prevents the undo→redo loop. v1 has only undo, no redo.

### 11.1 What's in the undo stack

After a `move`, the stack has `Move(toOriginalFolder)`. After two `flag`s, two entries. After a `permanent_delete`, **no entry** (irreversible).

Stack capacity bounded by `[triage].undo_stack_size` (default 50). Oldest entries drop off the bottom when full.

### 11.2 Cross-session

The `undo` table is cleared on app start (ARCH §9). Undo is session-scoped because:
- Inverse actions reference message IDs that may have changed across syncs.
- Mental model is "u undoes the thing I just did," not "u undoes something from yesterday."

## 12. Keybindings (extending spec 04 keymap)

`f`, `r`, `d` etc. operate on the focused message in list pane OR the displayed message in viewer pane (pane-scoped). Bindings that are present in BOTH panes have the same meaning unless noted otherwise.

| Key | Pane | Action |
| --- | --- | --- |
| `r` | list, viewer | mark_read on focused message |
| `Shift+r` (i.e. `R`) | list | mark_unread on focused message |
| `r` | viewer | reply (opens $EDITOR with quoted body) |
| `Shift+r` (i.e. `R`) | viewer | reply-all |
| `f` | list | toggle flag (flag if unflagged, unflag if flagged) |
| `f` | viewer | forward (opens $EDITOR with forwarded body) |
| `d` | list, viewer | soft_delete |
| `Shift+d` (i.e. `D`) | list, viewer | permanent_delete (always confirms) |
| `a` | list, viewer | archive |
| `m` | list, viewer | move (opens folder picker) |
| `c` | list, viewer | add_category (opens category input) |
| `Shift+c` (i.e. `C`) | list, viewer | remove_category (opens picker over current categories) |
| `Enter` | list | open focused message in viewer |
| `u` | any | undo last action |
| `Shift+u` (i.e. `U`) | any | show undo stack (overlay; press number to undo to that depth) |

**Notes on the resolution of binding conflicts:**

- **`f` is mode-overloaded by pane.** In list pane: toggle-flag. In viewer pane: forward. Pane-scoped bindings are a spec 04 §5 mechanism. This is the cleanest way to give forward a one-key shortcut despite `f` being needed for flagging in the list.
- **`F` (i.e. `Shift+f`) is reserved for bulk filter** (spec 10). It does NOT mean unflag here. Unflag is achieved by pressing `f` again on a flagged message (toggle behavior).
- **`R` (i.e. `Shift+r`) is mode-overloaded by pane.** In list pane: mark unread. In viewer pane: reply-all. Same pane-scoping mechanism.
- **No `Shift+F` binding.** That would be a no-op keystroke (Shift+Shift+f).

The Action types `Flag` and `Unflag` remain distinct in the action layer (§3) for explicit programmatic use via CLI mode and `:command` invocations like `:flag` and `:unflag`. The keybinding `f` is a toggle wrapper that dispatches whichever fits the current state.

### 12.1 Folder picker

Triggered by `m` and `c`. A modal overlay listing folders/categories with type-to-filter:

```
   ╭──────────────────────────────────╮
   │ Move to:                          │
   │ archive_                          │
   │                                   │
   │   ▸ Archive                       │
   │     Archived/2025                 │
   │     Clients/TIAA                  │
   │     Clients/ACME                  │
   ╰──────────────────────────────────╯
```

Recently-used folders appear at the top. Selection via Enter; Esc cancels.

For categories: same UI, but listing existing categories. The user can also type a new category name to create one.

## 13. Configuration

This spec owns the `[triage]` section. Full reference in `CONFIG.md`.

| Key | Default | Used in § |
| --- | --- | --- |
| `triage.archive_folder` | `"archive"` | §6.8 |
| `triage.confirm_threshold` | `10` | (used by spec 10; included here so spec 07 doesn't have to re-define) |
| `triage.confirm_permanent_delete` | `true` | §6.7 |
| `triage.undo_stack_size` | `50` | §11.1 |
| `triage.optimistic_ui` | `true` | §7 |

New keys this spec adds:

| Key | Default | Used in § |
| --- | --- | --- |
| `triage.editor` | `""` (uses `$EDITOR`, falls back to `nano`) | §9.1 |
| `triage.draft_temp_dir` | `"~/Library/Caches/inkwell/drafts"` | §9.1 |
| `triage.recent_folders_count` | `5` | §12.1 (folder picker top-N) |

Bindings (from §12) live in `[bindings]` per spec 04 conventions.

## 14. Performance budgets

| Operation | Target |
| --- | --- |
| Local apply (single message, any action) | <10ms |
| UI re-render after action | <50ms |
| Graph mutation round-trip (single message) | <500ms p95 |
| Undo round-trip | same as forward action |
| Replay on startup (10 pending actions) | <2s total |

## 15. Failure modes

| Scenario | Behavior |
| --- | --- |
| User flags message; Graph 403 (no permission) | Roll back; toast: "Could not flag: permission denied" |
| User deletes message; Graph 404 (already deleted by another client) | Treat as success; remove from local |
| User moves to a folder that was deleted server-side | Graph returns 404 on destination; roll back; toast: "Folder no longer exists" |
| User permanently deletes; succeeds on server but app crashes before recording done | On replay, the second permanent delete returns 404; treated as success |
| User adds a category that exceeds Graph's category limit | Graph 400; roll back; toast: "Category not added: too many categories" |
| User opens reply editor, $EDITOR not set, nano not installed | Toast: "No editor configured. Set $EDITOR or [triage].editor" |
| User saves draft body, network goes down before Graph creates the draft | Action stays Pending; retried on reconnect; user sees "Draft pending sync" |
| User undoes a move; original folder no longer exists | Same as above (404 on destination); toast: "Could not undo move: folder gone"; the redo of the failed undo doesn't push another undo |
| Action queue grows unbounded (network down for hours) | Soft cap: 1000 pending actions. Beyond that, refuse new actions with toast: "Too many pending actions; restore network connection" |

## 16. Test plan

### Unit tests

- Each action type's local apply: round-trip from snapshot to applied state.
- Each action type's inverse: applying then undoing returns to original state.
- Idempotency: applying same action twice has same final state as once.
- Error classification table: every Graph status code maps to expected behavior.
- Undo stack: push, pop, peek, capacity bound.

### Integration tests

- Mock Graph; execute each action type; assert local state and Graph call payloads.
- Simulate Graph 5xx; assert action remains Pending.
- Simulate Graph 403; assert rollback fires.
- Simulate disconnect mid-flight; assert action stays InFlight; assert replay recovers.
- Concurrent execute (two `r` keystrokes on different messages); assert no race.

### Manual smoke tests

1. Mark a message read; confirm in Outlook for Mac it's marked read within 30s.
2. Flag, unflag.
3. Move to a folder; verify in Outlook.
4. Move; press `u`; message comes back.
5. Soft-delete; press `u`; comes back.
6. Permanent-delete; confirm prompt; deletion is final.
7. Archive (`a`); message goes to Archive folder.
8. Add category; verify in Outlook.
9. Open reply editor; type response; save; verify draft in Outlook; send from there.
10. Disconnect network; do 5 actions; reconnect; observe queue drain.
11. Crash the app (kill -9) mid-action; restart; observe replay completes the action.

## 17. Definition of done

- [ ] `internal/action/` package compiles, passes unit tests.
- [ ] All 13 action types in §3 implemented and tested.
- [ ] Optimistic UI, rollback, undo, replay all verified.
- [ ] Bindings from §12 wired and functional.
- [ ] Folder/category picker UI works.
- [ ] Editor integration verified with at least vim and nano.
- [ ] All manual smoke tests in §16 pass.
- [ ] Performance budgets met.

## 18. Out of scope for this spec

- Bulk variants of these actions (spec 10).
- Automatic categorization (rules-based on incoming mail) — this is what saved searches in spec 11 partially address.
- Send mail (PRD §3.2: hard out-of-scope).
- Rich-text or HTML draft composition (drafts are plain text in v1; spec mentions in PRD §6).
- Snooze / defer (Outlook has it; we don't in v1).
- Multi-message select-and-act in the list (v1 single-message; spec 10 introduces filter-then-act).
