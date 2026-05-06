# Spec 20 — Conversation-level operations

## Status
not-started

## DoD checklist
- [ ] Migration `010_conv_account_idx.sql`: composite index
      `(account_id, conversation_id)` on messages table.
- [ ] `store.Store` interface gains `MessageIDsInConversation(ctx,
      accountID, conversationID, includeAllFolders) ([]string, error)`.
      Excludes Drafts/Trash/Junk by default.
- [ ] `action.Executor.ThreadExecute(ctx, accID, verb, focusedMsgID)
      (int, []BatchResult, error)` — all verbs except ActionMove.
- [ ] `action.Executor.ThreadMove(ctx, accID, focusedMsgID,
      destFolderID, destAlias) (int, []BatchResult, error)`.
- [ ] `KeyMap` gains `ThreadChord key.Binding`; `BindingOverrides` gains
      `ThreadChord string`; default binding `T`.
- [ ] Model fields: `threadChordPending bool`, `threadChordToken uint64`,
      `pendingThreadMove bool`, `pendingThreadIDs []string`.
- [ ] Chord timeout: `threadChordTimeoutMsg{token}` type; 3-second
      one-shot Cmd; stale-token timeouts are no-ops.
- [ ] `T r`/`R`/`f`/`F` dispatch `ThreadExecute`; no confirm modal.
- [ ] `T a` dispatches `ThreadMove("", "archive")`; no confirm modal.
- [ ] `T d`: pre-fetches IDs into `pendingThreadIDs`, confirm modal
      (default N), then `BatchExecute(ActionSoftDelete, pendingThreadIDs)`.
- [ ] `T D`: same flow, `ActionPermanentDelete`, stronger warning.
- [ ] `T m`: sets `pendingThreadMove`, activates `FolderPickerMode`;
      folder selection dispatches `ThreadMove`; picker Esc clears flag.
- [ ] `updateFolderPicker` Enter handler: three-way dispatch with nil-
      guard on `pendingMoveMsg` in the single-message fallthrough branch.
- [ ] Status bar feedback: success count and ⚠ partial-failure format.
- [ ] CLI `cmd/inkwell/cmd_thread.go`: archive, delete, permanent-delete,
      mark-read, mark-unread, flag, unflag, move subcommands.
- [ ] Tests: `TestMessageIDsInConversationExcludesDraftTrash`,
      `TestMessageIDsInConversationIncludeAllFolders`,
      `TestMessageIDsInConversationEmptyConvID`,
      `TestThreadExecuteMarkRead`, `TestThreadExecuteRejectsMove`,
      `TestThreadMoveCallsBulkMove`, `TestThreadExecuteNoConvID`,
      `TestThreadChordTPendingState`, `TestThreadChordEscCancels`,
      `TestThreadChordTimeoutNoop`, `TestThreadChordArArchivesThread`,
      `TestThreadChordDdOpensConfirm`, `TestThreadChordTmOpensFolderPicker`,
      `TestThreadCLIArchive`, `TestThreadCLIDeleteWithoutYesIsNoop`,
      `TestThreadCLIMoveResolvesFolder`,
      `BenchmarkMessageIDsInConversation`.
- [ ] User docs: reference.md `T` chord table; how-to.md "Triage an
      entire thread" recipe.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `MessageIDsInConversation` over 100k msgs, avg 20/conv | ≤5ms p95 | — | `BenchmarkMessageIDsInConversation` | pending |

## Iteration log
### Iter 1 — 2026-05-06 (spec written + adversarial review)
- Slice: spec document written, two rounds of adversarial review, all
  6 review findings fixed.
- Key decisions:
  - `ThreadExecute` rejects `ActionMove`; `T a` routes through
    `ThreadMove("", "archive")` to match `BulkArchive` behaviour.
  - `pendingThreadIDs` model field stores pre-flight IDs for T d/D
    confirm flow to avoid double-fetch race.
  - Token-based chord timeout prevents stale Cmds from clearing active
    pending state.
  - Migration is 010 (spec 19 claims 009 for mute table).
  - Excludes Drafts/Trash/Junk from `MessageIDsInConversation` by
    default; `includeAllFolders=true` for CLI.
- Implementation not yet started.
