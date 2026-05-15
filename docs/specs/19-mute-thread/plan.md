# Spec 19 — Mute thread

## Status
done

## DoD checklist
- [x] Migration `009_mute.sql` created; `muted_conversations` table
      with composite PK `(conversation_id, account_id)` + index.
- [x] `store.Store` interface gains `MuteConversation`,
      `UnmuteConversation`, `IsConversationMuted`,
      `ListMutedMessages`, `CountMutedConversations`.
- [x] `MessageQuery.ExcludeMuted bool` added; `buildListSQL` emits the
      `NOT EXISTS` anti-join when true; normal folder views pass
      `ExcludeMuted: true`.
- [x] `KeyMap` gains `MuteThread key.Binding`; `BindingOverrides` gains
      `MuteThread string`; `ApplyBindingOverrides` wires it;
      `findDuplicateBinding` includes it; default `M`.
- [x] `M` wired in `dispatchList` and `dispatchViewer`; dispatches
      `muteCmd` Cmd; on `mutedToastMsg` reloads list + shows status
      toast.
- [x] `🔕` indicator in list-pane row for muted messages.
- [x] "Muted Threads" virtual sidebar entry (sentinel ID `__muted__`);
      visible only when ≥1 muted conversation exists; selecting it
      calls `ListMutedMessages`; count shows distinct muted-conversation
      count.
- [x] `[ui].mute_indicator` config key documented in `docs/CONFIG.md`
      (default `🔕`, ASCII fallback `m`).
- [x] CLI: `cmd/inkwell/cmd_mute.go` implementing `inkwell mute` and
      `inkwell unmute` with `--message` resolver; registered in
      `cmd_root.go`.
- [x] Tests: `TestMuteConversationIdempotent`,
      `TestUnmuteConversationNoop`, `TestListMessagesExcludesMuted`,
      `TestListMessagesNullConvIDNotFiltered`,
      `TestListMutedMessages`, `TestCountMutedConversations`,
      `TestMuteKeyMutesThread`, `TestMuteKeyUnmutesThread`,
      `TestMuteKeyNoConvIDShowsError`,
      `TestMuteCLIByConversationID`, `TestMuteCLIByMessageID`,
      `TestMuteCLIByMessageIDNoConvReturnsError`,
      `BenchmarkListMessagesExcludeMuted`,
      `BenchmarkMuteUnmute`.
- [x] User docs: `docs/user/reference.md` adds `M` row to list-pane
      keybindings table; `docs/user/how-to.md` adds "Mute a noisy
      thread" recipe.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `ListMessages(folder, limit=100, ExcludeMuted=true)` over 100k msgs + 500 muted | ≤10ms p95 | ~2.9ms avg | `BenchmarkListMessagesExcludeMuted` | ✓ |
| `MuteConversation` / `UnmuteConversation` | ≤1ms p95 | ~0.035ms avg | `BenchmarkMuteUnmute` | ✓ |

## Iteration log
### Iter 1 — 2026-05-06 (spec written)
- Slice: spec document written + adversarial review loop completed (2 rounds).
- Key decisions recorded in spec §2.3 and §6.
- Implementation not yet started.

### Iter 2 — 2026-05-06 (implementation)
- Slice: full implementation in one pass (schema, store, UI, CLI, tests, docs).
- Commands run: go vet, go test -race, go test -tags=integration, go test -tags=e2e, benchmarks.
- Result: all green. BenchmarkListMessagesExcludeMuted ~2.9ms, BenchmarkMuteUnmute ~0.035ms.
- Critique:
  - messageColumns ambiguity with JOIN: fixed using correlated subquery for ORDER BY.
  - SeedAccount/SeedFolder typed *testing.T, not testing.TB: fixed.
  - gofmt trailing blank line in mute_test.go: fixed.
  - gofmt field alignment in panes.go: fixed.
- Shipped: commit 9ac7f4b, tag v0.47.0.
