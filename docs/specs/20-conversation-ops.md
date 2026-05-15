# Spec 20 — Conversation-level operations

**Status:** Ready for implementation.
**Depends on:** Specs 02 (`messages.conversation_id` exists), 07 (action
queue), 09 (Graph $batch), 19 (mute — key assignment coordination).
**Blocks:** Custom actions (spec 21 — many ops want a `--thread` flag),
split-inbox tabs (spec 1.7 — auto-archive on thread events).
**Estimated effort:** 1 day.

---

## 1. Goal

Promote the conversation (thread) to a first-class unit of action.
Today every triage verb operates on one message. "Archive the entire
thread", "mark thread read", "move all replies elsewhere" require N
separate actions. The data model already has `conversation_id` — we
just don't act on it as a unit.

## 2. Prior art

### 2.1 Terminal clients

- **mutt / neomutt** — `t` tags a single message; `T<pattern>` tags
  all messages matching a pattern. Tagged messages share actions via
  `;` prefix (e.g. `;d` = delete all tagged). The `T` prefix itself
  is a thread-tag operation in NeoMutt: `T` then a pattern string
  tags the entire thread matching that pattern. Macros approximate
  "archive thread" by chaining tag + save. No built-in chord timeout —
  tag state is sticky, not pending-input. Permanent-delete is `\D`
  (backslash-modifier), not a chord.
- **aerc** — commands like `:archive`, `:delete`, `:flag`, `:read` act
  on the message selection (highlighted rows). To act on a whole
  thread the user selects all thread members first. No native
  "archive thread in one binding" — users compose commands. Key
  system: single character in `[messages]` section of `binds.conf`.
  No chord timeout concept.
- **alot (notmuch)** — thread-first by design. In the thread buffer,
  `a` archives the thread (adds `archive` tag, removes `inbox`). The
  thread is the default unit; message-level operations require entering
  the message buffer. No chord system — each key is a direct binding
  at the thread level.

### 2.2 Desktop / web clients

- **Gmail** — `e` archives the conversation, `#` deletes (moves to
  Trash), `Shift+u` marks unread, `Shift+i` marks read, `v` moves
  (opens an autocomplete folder selector in a modal). All operations
  apply to the focused conversation; individual-message ops require
  opening the message first. No chord; each letter is a direct binding
  at the conversation-list level.
- **Fastmail** — `y` archives, `M` opens a folder autocomplete popup
  (keyboard-navigable with arrow keys and type-ahead filtering),
  `Shift+Del` is permanent delete. Thread-level operations are the
  default. No chord.
- **Superhuman** — `e` archives, `#` deletes, `v` opens a
  spotlight-style folder picker with autocomplete and arrow-key
  navigation (full overlay). `H` flags/unflag, no separate chord.
  Thread is the default unit.
- **Thunderbird** — `a` archives thread (when threaded view is on),
  `Del` moves to Trash, `Shift+Del` permanent-deletes. `r` marks
  read. Modifier-key distinction: `Del` vs `Shift+Del` for soft vs.
  hard delete. No chord timeout.
- **Apple Mail** — `Cmd+Del` archives, `Del` moves to Trash. Flagging
  is per-message. Thread-level operations work when the thread is
  collapsed.

### 2.3 Design decision

The chord `T<verb>` (mutt-lineage convention) is the right model for
inkwell:

- The single-message verbs (`a`, `d`, `r`, `f`) are already taken at
  the list level.
- Shift-modifying each conflicts: `R` is mark-unread, `D` is
  permanent-delete.
- A chord prefix keeps both the single-message verbs and their thread
  counterparts unambiguous without requiring modifier keys.
- The 3-second timeout means the user always sees the chord status in
  the status bar and knows what's pending. Pressing `Esc` cancels.

`M` is reserved for mute (spec 19). `T m` (move thread) does not
collide because it is a chord member, not a standalone binding.

## 3. The chord

Pressing `T` in the list pane or viewer pane enters
**thread-chord-pending** state. The status bar shows:

```
thread: r/R/f/F/d/D/a/m  esc cancel
```

A 3-second timeout starts. A second keypress completes the chord:

| Chord | Action                                  | Confirm modal? |
|-------|-----------------------------------------|----------------|
| `T r` | Mark whole thread read                  | No             |
| `T R` | Mark whole thread unread                | No             |
| `T f` | Flag every message in the thread        | No             |
| `T F` | Unflag every message in the thread      | No             |
| `T d` | Soft-delete the entire thread           | Yes — default N |
| `T D` | Permanently delete the entire thread    | Yes — default N, stronger warning |
| `T a` | Archive the entire thread (via `ThreadMove("","archive")`) | No |
| `T m` | Move whole thread (opens folder picker) | No (picker is the confirmation UX) |

`Esc` or any unrecognised key while in chord-pending state cancels and
clears the status bar. A 3-second timeout with no second key also
cancels.

### 3.1 Chord timeout implementation

The timeout is a one-shot tea.Cmd using a token to prevent stale
messages from interfering:

```go
type threadChordTimeoutMsg struct{ token uint64 }

func threadChordTimeout(token uint64) tea.Cmd {
    return func() tea.Msg {
        <-time.After(3 * time.Second)
        return threadChordTimeoutMsg{token: token}
    }
}
```

The model carries:

```go
threadChordPending bool
threadChordToken   uint64  // incremented each time chord is entered
```

On `T` pressed:
```go
m.threadChordToken++
m.threadChordPending = true
return m, threadChordTimeout(m.threadChordToken)
```

On `threadChordTimeoutMsg` received:
```go
// Stale timeouts (prior chord already consumed) are no-ops.
if msg.token == m.threadChordToken {
    m.threadChordPending = false
    m.status = ""
}
```

## 4. Implementation

### 4.1 Schema — new index

Migration **`010_conv_account_idx.sql`** (spec 19 claims `009_mute.sql`;
this spec depends on spec 19, so its migration is 010):

```sql
-- The existing idx_messages_conversation covers (conversation_id) only.
-- A composite index on (account_id, conversation_id) is required for
-- MessageIDsInConversation to avoid a full-table scan on account_id.
CREATE INDEX IF NOT EXISTS idx_messages_conv_account
    ON messages(account_id, conversation_id);
```

No other schema changes. `messages.conversation_id` and the new
composite index are sufficient.

### 4.2 New store method

```go
// MessageIDsInConversation returns the IDs of all messages with the
// given conversationID for the account. By default it excludes messages
// in well-known Drafts, Deleted Items, and Junk folders (soft-deleting
// or archiving a draft, or re-moving a trashed message, is surprising).
//
// When includeAllFolders is true the folder exclusion is skipped — used
// by CLI where the user explicitly targets a conversation.
MessageIDsInConversation(ctx context.Context, accountID int64,
    conversationID string, includeAllFolders bool) ([]string, error)
```

SQL (default, excluding Drafts / Trash / Junk):

```sql
SELECT m.id
FROM messages m
JOIN folders f ON f.id = m.folder_id
WHERE m.account_id = ?
  AND m.conversation_id = ?
  AND (f.well_known_name IS NULL
       OR f.well_known_name NOT IN ('drafts', 'deleteditems', 'junkemail'))
ORDER BY m.received_at DESC
```

When `includeAllFolders` is true, drop the `well_known_name` filter.

### 4.3 New Executor methods

```go
// ThreadExecute collects the conversation's message IDs (from the
// focused message's conversationID) and applies a batch action.
// Use ThreadMove for move/archive operations; this method rejects
// ActionMove.
// Returns (count of IDs collected, per-message results, error).
//
// Note: store.GetMessage never returns (nil, nil) — a missing message
// returns (nil, store.ErrNotFound). The err != nil guard below is the
// only nil-path; no subsequent msg == nil check is needed.
func (e *Executor) ThreadExecute(ctx context.Context, accID int64,
    verb store.ActionType, focusedMsgID string) (int, []BatchResult, error) {

    if verb == store.ActionMove {
        return 0, nil, fmt.Errorf("thread: use ThreadMove for move/archive operations")
    }
    msg, err := e.st.GetMessage(ctx, focusedMsgID)
    if err != nil {
        return 0, nil, fmt.Errorf("thread: get message: %w", err)
    }
    if msg.ConversationID == "" {
        return 0, nil, fmt.Errorf("thread: no conversation id on message %q", focusedMsgID)
    }
    ids, err := e.st.MessageIDsInConversation(ctx, accID, msg.ConversationID, false)
    if err != nil {
        return 0, nil, fmt.Errorf("thread: enumerate: %w", err)
    }
    if len(ids) == 0 {
        return 0, nil, nil
    }
    results, err := e.BatchExecute(ctx, accID, verb, ids)
    return len(ids), results, err
}

// ThreadMove collects the conversation's message IDs and bulk-moves
// them to the user-specified folder via BulkMove.
// For archive, pass destFolderID="" and destAlias="archive".
// Returns (count of IDs collected, per-message results, error).
//
// Note: store.GetMessage never returns (nil, nil); the err != nil
// guard is the only nil-path.
func (e *Executor) ThreadMove(ctx context.Context, accID int64,
    focusedMsgID, destFolderID, destAlias string) (int, []BatchResult, error) {

    msg, err := e.st.GetMessage(ctx, focusedMsgID)
    if err != nil {
        return 0, nil, fmt.Errorf("thread: get message: %w", err)
    }
    if msg.ConversationID == "" {
        return 0, nil, fmt.Errorf("thread: no conversation id on message %q", focusedMsgID)
    }
    ids, err := e.st.MessageIDsInConversation(ctx, accID, msg.ConversationID, false)
    if err != nil {
        return 0, nil, fmt.Errorf("thread: enumerate: %w", err)
    }
    if len(ids) == 0 {
        return 0, nil, nil
    }
    results, err := e.BulkMove(ctx, accID, ids, destFolderID, destAlias)
    return len(ids), results, err
}
```

### 4.4 KeyMap changes

Add to `internal/ui/keys.go`:

```go
// KeyMap struct:
ThreadChord key.Binding

// BindingOverrides struct:
ThreadChord string
```

Default: `key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "thread chord"))`.

Wire through `ApplyBindingOverrides` and include in `findDuplicateBinding`
scan — same pattern as `MuteThread` in spec 19.

### 4.5 UI model fields

```go
// Model — add:
threadChordPending bool
threadChordToken   uint64
pendingThreadMove  bool     // true while FolderPickerMode is active for T m
pendingThreadIDs   []string // pre-fetched IDs for T d / T D confirm flow
```

`pendingThreadMove` parallels `pendingBulkMove`. The folder picker's
`Enter` handler checks which pending flag is set:

```go
// In updateFolderPicker, on folder selected:
if m.pendingThreadMove {
    m.pendingThreadMove = false
    // dispatch ThreadMove Cmd
} else if m.pendingBulkMove {
    m.pendingBulkMove = false
    // dispatch BulkMove Cmd (existing behaviour)
} else {
    // Single-message move: guard against nil pendingMoveMsg before
    // dereferencing. If pendingMoveMsg is nil (state bug), return
    // without action rather than panicking.
    if m.pendingMoveMsg == nil {
        return m, nil
    }
    // dispatch single-message Move Cmd (existing behaviour)
}
```

On picker `Esc`, clear `pendingThreadMove`. The existing Esc branch
also nils `pendingMoveMsg`; this is fine because `pendingMoveMsg` is
nil (unused) during a thread-move flow.

### 4.6 T m folder picker flow

1. `T` pressed → `threadChordPending = true`, timeout Cmd started.
2. `m` pressed → `threadChordPending = false`, `pendingThreadMove = true`.
3. Activate `FolderPickerMode` (same `FolderPickerModel` as `m`).
4. User selects folder → `Enter` in picker fires `threadMoveResultMsg`
   via a `ThreadMove` Cmd.
5. On `threadMoveResultMsg`: clear `pendingThreadMove`, reload list,
   show status toast.

## 5. Confirmation gates

| Chord                     | Modal text                                                              |
|---------------------------|-------------------------------------------------------------------------|
| `T r`/`R`/`f`/`F`/`a`/`m`| No modal — reversible, or picker is the confirmation.                   |
| `T d`                     | `Soft-delete thread (N messages)? [y/N]` — default N. N is fetched via `MessageIDsInConversation` before the modal opens. |
| `T D`                     | `Permanently delete thread (N messages)?`<br>`This cannot be undone. [y/N]` — default N. Same pre-fetch. |

For `T d` and `T D` the count N is filled by a pre-flight
`MessageIDsInConversation` call issued as a `tea.Cmd` before the
modal is shown. The resulting ID slice is stored in a new model field:

```go
pendingThreadIDs []string  // populated on T d / T D before confirm modal
```

When the user confirms (`y`), the handler passes `pendingThreadIDs`
directly to `e.BatchExecute` (bypassing a second `MessageIDsInConversation`
call). This avoids a race where messages are moved between the count
call and the execute call. On cancel or Esc, `pendingThreadIDs` is
cleared.

Add `pendingThreadIDs []string` to the model field list in §4.5.

## 6. UI feedback

Status bar after a thread op:

```
✓ archived thread (12 messages)
✓ marked thread read (12 messages)
⚠ delete thread: 11/12 succeeded — 1 already deleted
⚠ move thread: 8/12 succeeded — 4 failed
```

Partial failure matches the `⚠` pattern from spec 10 bulk ops. If
all N messages are in excluded folders (Drafts/Trash/Junk), the status
bar shows `thread: 0 messages to act on`.

## 7. Optimistic apply

Thread ops route through `BatchExecute` / `BulkMove` from spec 09.
Each message applies optimistically and rolls back per-message on
Graph failure. A partial failure leaves the thread in a mixed state;
the status bar shows the partial count.

This matches the established behaviour for `;a` / `;d` bulk ops on a
filter set (spec 10). Recovery: re-filter with `~v <conversation_id>`
to locate remaining messages, then retry.

## 8. CLI

```sh
# Archive entire thread
inkwell thread archive <conversation-id>

# Soft-delete thread (--yes required for destructive ops)
inkwell thread delete <conversation-id> --yes

# Permanent delete thread
inkwell thread permanent-delete <conversation-id> --yes

# Mark read / unread
inkwell thread mark-read <conversation-id>
inkwell thread mark-unread <conversation-id>

# Flag / unflag
inkwell thread flag <conversation-id>
inkwell thread unflag <conversation-id>

# Move to a folder (resolved by display name or well-known name)
inkwell thread move <conversation-id> --folder <folder-name>
```

Without `--yes` on destructive operations, a dry-run message prints:
`would delete N messages — pass --yes to apply`.

`inkwell thread move` resolves `--folder` via `resolveFolderByNameCtx`
(the same helper as `inkwell folder`). The conversation ID is passed
directly; the CLI does not resolve it from a message ID (use
`inkwell messages <id>` to look up `conversation_id` if needed).

All subcommands support `--output json`:

```json
{"action":"archive","conversation_id":"AAQkADFi...","succeeded":12,"failed":0}
```

Commands live in `cmd/inkwell/cmd_thread.go`. Registered in
`cmd_root.go`.

## 9. Performance budgets

| Surface | Budget | Benchmark |
|---------|--------|-----------|
| `MessageIDsInConversation` over 100k-message store (avg 20 msgs/conversation) | ≤5ms p95 | `BenchmarkMessageIDsInConversation` in `internal/store/` |

Verify with `EXPLAIN QUERY PLAN` in the benchmark harness that the
query uses `idx_messages_conv_account`. If the plan shows a full-table
scan, the migration 010 index is required before shipping.

## 10. Definition of done

- [ ] Migration `010_conv_account_idx.sql`: `CREATE INDEX IF NOT EXISTS
      idx_messages_conv_account ON messages(account_id, conversation_id)`.
- [ ] `store.Store` interface gains `MessageIDsInConversation(ctx,
      accountID, conversationID, includeAllFolders) ([]string, error)`.
      Excludes Drafts/Trash/Junk by default; `includeAllFolders=true`
      disables exclusion.
- [ ] `action.Executor.ThreadExecute(ctx, accID, verb, focusedMsgID)
      (int, []BatchResult, error)` — all verbs except ActionMove; all
      errors propagated (no silent `_` discard).
- [ ] `action.Executor.ThreadMove(ctx, accID, focusedMsgID,
      destFolderID, destAlias) (int, []BatchResult, error)` — calls
      `BulkMove`.
- [ ] `KeyMap` gains `ThreadChord key.Binding`; `BindingOverrides` gains
      `ThreadChord string`; wired through `ApplyBindingOverrides` and
      `findDuplicateBinding`; default binding `T`.
- [ ] `threadChordPending bool` + `threadChordToken uint64` in model.
      `T` sets pending and starts `threadChordTimeout` Cmd. Any chord
      key, `Esc`, or expired token clears `threadChordPending`.
- [ ] `threadChordTimeoutMsg{token}` type; stale-token timeouts are
      no-ops.
- [ ] `T r` / `T R` / `T f` / `T F` dispatch `ThreadExecute` with the
      matching verb (MarkRead / MarkUnread / Flag / Unflag); no confirm
      modal.
- [ ] `T a` dispatches `ThreadMove(ctx, accID, focusedMsgID, "", "archive")`
      — archive is a move to the well-known `archive` destination, not a
      `BatchExecute` verb. No confirm modal.
- [ ] `T d` fetches `MessageIDsInConversation` into `pendingThreadIDs`,
      opens confirm modal showing count (default N); on `y` calls
      `e.BatchExecute(ActionSoftDelete, pendingThreadIDs)` directly
      (no second fetch); on cancel clears `pendingThreadIDs`.
- [ ] `T D` same flow as `T d` with stronger modal warning and
      `ActionPermanentDelete`.
- [ ] `T m` sets `pendingThreadMove = true`, activates `FolderPickerMode`;
      folder selection dispatches `ThreadMove`; picker `Esc` clears
      `pendingThreadMove`.
- [ ] Status bar: `✓ <verb> thread (N messages)` on success;
      `⚠ <verb> thread: X/N succeeded — Y failed` on partial failure;
      `thread: 0 messages to act on` when excluded folders cover all.
- [ ] List pane reloads after every thread op.
- [ ] CLI `cmd/inkwell/cmd_thread.go` with subcommands: `archive`,
      `delete`, `permanent-delete`, `mark-read`, `mark-unread`, `flag`,
      `unflag`, `move`. Registered in `cmd_root.go`.
- [ ] Tests:
  - store: `TestMessageIDsInConversationExcludesDraftTrash` (messages
    in Drafts/Trash excluded from default call), `TestMessageIDsInConversationIncludeAllFolders`
    (flag=true includes them), `TestMessageIDsInConversationEmptyConvID`
    (empty convID → empty slice, no error).
  - action: `TestThreadExecuteMarkRead` (BatchExecute called with
    correct IDs), `TestThreadExecuteRejectsMove` (returns error when
    verb=ActionMove), `TestThreadMoveCallsBulkMove` (verifies BulkMove
    called with correct folder params), `TestThreadExecuteNoConvID`
    (error path — message has no conversation_id).
  - dispatch (e2e): `TestThreadChordTPendingState` (T → status bar
    shows chord hint), `TestThreadChordEscCancels` (Esc clears pending),
    `TestThreadChordTimeoutNoop` (second `T` press increments token;
    first timeout fires with old token; `threadChordPending` remains
    `true` — stale timeouts must not clear active pending state),
    `TestThreadChordArArchivesThread` (T+a fires archive),
    `TestThreadChordDdOpensConfirm` (T+d shows confirm modal, default
    N), `TestThreadChordTmOpensFolderPicker` (T+m activates picker,
    `pendingThreadMove` set).
  - CLI: `TestThreadCLIArchive`, `TestThreadCLIDeleteWithoutYesIsNoop`,
    `TestThreadCLIMoveResolvesFolder`.
  - bench: `BenchmarkMessageIDsInConversation` (≤5ms p95 over 100k-
    message store, average 20 messages per conversation).
- [ ] User docs: `docs/user/reference.md` adds `T` chord table to the
      list-pane keybindings section; `docs/user/how-to.md` adds "Triage
      an entire thread" recipe.

## 11. Cross-cutting checklist

- [ ] Scopes: `Mail.ReadWrite` (already in PRD §3.1). No new scope.
- [ ] Store reads/writes: `messages` read (`MessageIDsInConversation`).
      All mutations route through existing spec 07 action queue
      (`BatchExecute` / `BulkMove`). No new tables beyond the index.
- [ ] Graph endpoints: spec 09's $batch (`/me/messages/{id}/move`,
      PATCH `/me/messages/{id}`, POST
      `/me/messages/{id}/permanentDelete`). No new endpoints.
- [ ] Offline: queues for later replay via spec 07's action drain.
      Optimistic local apply fires immediately; Graph dispatch retries
      on reconnect.
- [ ] Undo: spec 07's composite undo entry covers `T r`, `T R`, `T f`,
      `T F` (reversible batch types). `T d`, `T D`, `T a`, `T m` are
      not batch-undoable (matching spec 10 bulk-op behaviour). Per-
      message single undo (`u`) can reverse individual messages if
      needed.
- [ ] User errors: "no conversation id" for messages without one;
      partial-failure status with count; "0 messages to act on" when
      all messages are in excluded folders.
- [ ] Latency: `MessageIDsInConversation` ≤5ms p95 (§9 benchmark).
- [ ] Logs: `ThreadExecute` and `ThreadMove` log at DEBUG with
      `conversation_id` and `verb` — never log subject or body. No
      new redaction-relevant fields (conversation_id is not PII per
      spec 19 §10).
- [ ] CLI mode: `inkwell thread <subcommand>` per §8.
- [ ] Tests: §10.
- [ ] **Spec 17 review:** thread ops use existing Graph paths (no new
      external HTTP surface). `MessageIDsInConversation` is a pure
      parameterised store read (no SQL injection risk — no dynamic
      column or table name construction). `T D` (permanent delete) is
      irreversible — confirmation gate is mandatory per `docs/CONVENTIONS.md` §7
      rule 9. No new token handling, subprocess, or cryptographic
      primitive. Threat model: no new row needed beyond spec 09's $batch
      coverage. Privacy: `conversation_id` values appear transiently in
      the action log — already covered by spec 07's redaction policy.
      `ThreadExecute` does not log message bodies or subjects.
- [ ] **Docs consistency sweep:** `docs/user/reference.md` gains the
      `T` chord table; `docs/user/how-to.md` gains the recipe. No
      `docs/CONFIG.md` change (no new config keys). `docs/ARCH.md` does
      not need updating (no new architectural layer).
