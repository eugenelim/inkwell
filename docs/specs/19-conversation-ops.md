# Spec 19 — Conversation-level operations

**Status:** Ready for implementation.
**Depends on:** Specs 02 (`messages.conversation_id` exists), 07
(action queue), 09 (Graph $batch).
**Blocks:** Custom actions (§2 — many ops want a `--thread` flag),
Split inbox tabs (1.7 — auto-archive on thread events).
**Estimated effort:** 1 day.

---

## 1. Goal

Promote the conversation (thread) to a first-class unit of action.
Today every triage verb operates on one message. "Archive entire
thread", "delete this whole conversation history", "mark thread
read" require N separate actions. The data model already has
`conversation_id` — we just don't act on it as a unit.

## 2. Prior art

- **mutt / neomutt** — `T` tags entire thread; tagged ops apply
  to all tagged messages. Thread-aware navigation (`Esc t` to
  thread root). No first-class "archive thread" verb; users wire
  macros: `<tag-thread><tag-prefix><save-message>=Archive`.
- **aerc** — `:archive thread` and `:delete thread` natively. Same
  for `read`, `flag`. Documented as "messages from the entire
  thread."
- **alot (notmuch)** — fundamentally thread-oriented. `tag +archive`
  applies to all messages in the focused thread. The default unit
  is the thread; the message is the exception.
- **Gmail / Outlook web** — "Archive conversation" is the visible
  button. The single-message archive isn't even surfaced in
  Outlook web's main UI.

We follow aerc + alot's convention: a Shift-modified key applies
the action to the entire thread; the bare key stays single-
message. So `D` is permanent-delete (single, spec 07) and `D`
already taken — we use a `T` chord prefix (mutt convention) so the
user types `T d` to "delete the thread" or `T a` to "archive the
thread".

## 3. The chord

A two-keystroke binding `T<verb>` where `<verb>` is one of the
existing single-message verbs:

| Chord | Action                                                    |
| ----- | --------------------------------------------------------- |
| `T r` | Mark whole thread read                                    |
| `T R` | Mark whole thread unread                                  |
| `T f` | Toggle flag on every message in the thread                |
| `T d` | Soft-delete the entire thread                             |
| `T a` | Archive the entire thread                                 |
| `T m` | Move whole thread (mute conversations from spec 18 use M) |

`T` enters a "thread chord pending" state (3-second timeout, Esc to
cancel). The status bar shows `thread: r/R/f/d/a/m  esc cancel`.

Why not just shift-modify each verb? `D` is already permanent-
delete. `R` already means mark-unread. The chord is the only way
to keep all single-message verbs available alongside their thread
counterparts. mutt does the same with `T`-tags.

## 4. Implementation

The existing action.Executor already supports `BatchExecute(type,
ids)` from spec 09. Thread ops are just batches with a different
ID-collection step:

1. Read the focused message's `conversation_id`.
2. Query the local store for all messages with that conversation_id
   (account-scoped, all folders).
3. Hand the IDs to the existing BatchExecute path for the chosen verb.

```go
func (e *Executor) ThreadExecute(ctx context.Context, accID int64,
    verb store.ActionType, focusedMessageID string) ([]BatchResult, error) {
    msg, _ := e.st.GetMessage(ctx, focusedMessageID)
    if msg == nil || msg.ConversationID == "" {
        return nil, fmt.Errorf("thread: no conversation id on message %q", focusedMessageID)
    }
    ids, _ := e.st.MessageIDsInConversation(ctx, accID, msg.ConversationID)
    return e.BatchExecute(ctx, accID, verb, ids)
}
```

New store query: `MessageIDsInConversation(accountID, conversationID) ([]string, error)`.

## 5. Schema

No schema change needed. The `messages.conversation_id` column +
`idx_messages_conversation` index from spec 02 are sufficient.

If we observe slow conversation queries on heavy mailboxes, an
explicit composite `(account_id, conversation_id)` index can land
in a follow-up.

## 6. Confirmation gates

| Verb               | Confirm modal?                                           |
| ------------------ | -------------------------------------------------------- |
| `T r` / `T R` / `T f` | No — non-destructive, one-keystroke reversal.          |
| `T a` / `T m`      | No on archive (recoverable). Yes on move (spec 07's m).  |
| `T d`              | Yes — "Delete entire thread (N messages)? [y/N]" with default-N.|

Confirmation policy mirrors single-message: destructive bulk gets
the confirm pane; reversible ops don't.

## 7. UI feedback

Status bar after a thread op:

```
✓ archived thread (12 messages)
✓ marked thread read (12 messages)
⚠ delete thread: 11/12 succeeded — 1 already gone
```

The list pane reloads automatically; the user immediately sees
the cleared rows.

## 8. Optimistic apply

Thread ops route through the existing BatchExecute pipeline (spec
09), which applies optimistically per message and rolls back per-
message on Graph failure. A partial failure on a thread op leaves
the thread in a mixed state — some messages archived, some not.
Status bar shows the partial count.

This matches the established behaviour for `;a` / `;d` bulk on a
filter set (spec 10). The user has the existing recovery path:
`:filter ~v <conversation_id>` re-finds the still-in-place
messages, retry.

## 9. CLI

```sh
inkwell thread <conversation_id> --action archive --apply
inkwell thread <conversation_id> --action delete --apply --yes
inkwell thread <conversation_id> --action mark-read --apply
```

Same shape as `inkwell filter` from spec 14.

## 10. Definition of done

- [ ] `store.MessageIDsInConversation(accID, convID) ([]string, error)` added.
- [ ] `action.Executor.ThreadExecute(verb, focusedID)` orchestrates
      lookup + BatchExecute.
- [ ] `T<verb>` chord wired in dispatchList + dispatchViewer.
- [ ] Chord pending state with 3s timeout + Esc cancel.
- [ ] `T d` opens confirm modal; default N.
- [ ] Status bar shows post-op result with thread message count.
- [ ] CLI `inkwell thread <id> --action <verb> [--apply] [--yes]`.
- [ ] Tests: ThreadExecute resolves IDs correctly; BatchExecute
      stub records the right verb + IDs; chord-pending state
      expires after 3s and Esc cancels; confirm modal blocks `T d`.
- [ ] User docs: reference (T-chord row), how-to ("Triage an entire
      thread").

## 11. Cross-cutting checklist

- [ ] Scopes: same as spec 09 (Mail.ReadWrite).
- [ ] Store reads/writes: messages (read for IDs); the per-message
      mutations route through spec 07's existing apply paths.
- [ ] Graph endpoints: spec 09's $batch.
- [ ] Offline: queues for later replay.
- [ ] Undo: per-message undo from spec 07. A future "thread undo"
      would group all per-message inverse ops as one entry.
- [ ] User errors: §7's "partial" status; "no conversation id" for
      messages without one (rare).
- [ ] Tests: §10.
