# Spec 19 — Mute thread

## Status
not-started

## DoD checklist
- [ ] Migration `009_mute.sql` created; `muted_conversations` table
      with composite PK `(conversation_id, account_id)` + index.
- [ ] `store.Store` interface gains `MuteConversation`,
      `UnmuteConversation`, `IsConversationMuted`,
      `ListMutedMessages`.
- [ ] `MessageQuery.ExcludeMuted bool` added; `buildListSQL` emits the
      `NOT EXISTS` anti-join when true; normal folder views pass
      `ExcludeMuted: true`.
- [ ] `KeyMap` gains `MuteThread key.Binding`; `BindingOverrides` gains
      `MuteThread string`; `ApplyBindingOverrides` wires it;
      `findDuplicateBinding` includes it; default `M`.
- [ ] `M` wired in `dispatchList` and `dispatchViewer`; dispatches
      `muteCmd` Cmd; on `mutedToastMsg` reloads list + shows status
      toast.
- [ ] `🔕` indicator in list-pane row for muted messages.
- [ ] "Muted Threads" virtual sidebar entry (sentinel ID `__muted__`);
      visible only when ≥1 muted conversation exists; selecting it
      calls `ListMutedMessages`; count shows distinct muted-conversation
      count.
- [ ] `[ui].mute_indicator` config key documented in `docs/CONFIG.md`
      (default `🔕`, ASCII fallback `m`).
- [ ] CLI: `cmd/inkwell/cmd_mute.go` implementing `inkwell mute` and
      `inkwell unmute` with `--message` resolver; registered in
      `cmd_root.go`.
- [ ] Tests: `TestMuteConversationIdempotent`,
      `TestUnmuteConversationNoop`, `TestListMessagesExcludesMuted`,
      `TestListMessagesNullConvIDNotFiltered`,
      `TestListMutedMessages`,
      `TestMuteKeyMutesThread`, `TestMuteKeyUnmutesThread`,
      `TestMuteKeyNoConvIDShowsError`,
      `TestMuteCLIByConversationID`, `TestMuteCLIByMessageID`,
      `BenchmarkListMessagesExcludeMuted`,
      `BenchmarkMuteUnmute`.
- [ ] User docs: `docs/user/reference.md` adds `M` row to list-pane
      keybindings table; `docs/user/how-to.md` adds "Mute a noisy
      thread" recipe.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `ListMessages(folder, limit=100, ExcludeMuted=true)` over 100k msgs + 500 muted | ≤10ms p95 | — | `BenchmarkListMessagesExcludeMuted` | pending |
| `MuteConversation` / `UnmuteConversation` | ≤1ms p95 | — | `BenchmarkMuteUnmute` | pending |

## Iteration log
### Iter 1 — 2026-05-06 (spec written)
- Slice: spec document written + adversarial review loop completed (2 rounds).
- Key decisions recorded in spec §2.3 and §6.
- Implementation not yet started.
