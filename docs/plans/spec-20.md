# Spec 20 — Conversation-level operations

## Status
done

## DoD checklist
- [x] Migration `010_conv_account_idx.sql`: composite index
      `(account_id, conversation_id)` on messages table.
- [x] `store.Store` interface gains `MessageIDsInConversation(ctx,
      accountID, conversationID, includeAllFolders) ([]string, error)`.
      Excludes Drafts/Trash/Junk by default.
- [x] `action.Executor.ThreadExecute(ctx, accID, verb, focusedMsgID)
      (int, []BatchResult, error)` — all verbs except ActionMove.
- [x] `action.Executor.ThreadMove(ctx, accID, focusedMsgID,
      destFolderID, destAlias) (int, []BatchResult, error)`.
- [x] `KeyMap` gains `ThreadChord key.Binding`; `BindingOverrides` gains
      `ThreadChord string`; default binding `T`.
- [x] Model fields: `threadChordPending bool`, `threadChordToken uint64`,
      `pendingThreadMove bool`, `pendingThreadIDs []string`.
- [x] Chord timeout: `threadChordTimeoutMsg{token}` type; 3-second
      one-shot Cmd; stale-token timeouts are no-ops.
- [x] `T r`/`R`/`f`/`F` dispatch `ThreadExecute`; no confirm modal.
- [x] `T a` dispatches `ThreadMove("", "archive")`; no confirm modal.
- [x] `T d`: pre-fetches IDs into `pendingThreadIDs`, confirm modal
      (default N), then `BatchExecute(ActionSoftDelete, pendingThreadIDs)`.
- [x] `T D`: same flow, `ActionPermanentDelete`, stronger warning.
- [x] `T m`: sets `pendingThreadMove`, activates `FolderPickerMode`;
      folder selection dispatches `ThreadMove`; picker Esc clears flag.
- [x] `updateFolderPicker` Enter handler: three-way dispatch with nil-
      guard on `pendingMoveMsg` in the single-message fallthrough branch.
- [x] Status bar feedback: success count and ⚠ partial-failure format.
- [x] CLI `cmd/inkwell/cmd_thread.go`: archive, delete, permanent-delete,
      mark-read, mark-unread, flag, unflag, move subcommands.
- [x] Tests: `TestMessageIDsInConversationExcludesDraftTrash`,
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
- [x] User docs: reference.md `T` chord table; how-to.md "Triage an
      entire thread" recipe; CONFIG.md binding keys.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `MessageIDsInConversation` over 100k msgs, avg 20/conv | ≤5ms p95 | ~0.018ms avg | `BenchmarkMessageIDsInConversation` | ✓ |

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

### Iter 2 — 2026-05-06 (implementation)
- Slice: full implementation in one pass (schema, store, action, UI, CLI, tests, docs).
- Commands run: gofmt, go vet, go test -race, go test -tags=integration, go test -tags=e2e, benchmarks.
- Result: all green. BenchmarkMessageIDsInConversation ~0.018ms avg.
- Critique:
  - CLI test referenced `fakeAuth` from action package — used `cliGraphFakeAuth` instead.
  - UI thread test needed mockThreadExecutor to implement ThreadExecutor interface.
  - gofmt field alignment in thread_test.go: fixed.
- Shipped: commit ad36b29, tag v0.48.0.
