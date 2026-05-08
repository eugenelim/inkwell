# Spec 23 — Routing destinations (Imbox / Feed / Paper Trail / Screener)

## Status
done — **Shipped v0.51.0** (2026-05-07)

## DoD checklist

Mirrors `docs/specs/23-routing-destinations.md` §12. Tick as
implementation lands.

- [ ] Migration `011_sender_routing.sql` creates `sender_routing`
      table (composite PK `(email_address, account_id)`, CHECK on
      destination, `CHECK(length(email_address) > 0)`),
      `idx_sender_routing_account_dest` index, expression index
      `idx_messages_from_lower ON messages(account_id,
      lower(trim(from_address)))`, and `UPDATE schema_meta SET
      value = '11'`.
- [ ] `store.Store` interface: `SetSenderRouting(ctx, accID, addr,
      dest) (prior string, err error)`, `ClearSenderRouting(ctx,
      accID, addr) (prior string, err error)`,
      `GetSenderRouting(ctx, accID, addr) (string, error)`,
      `ListSenderRoutings(ctx, accID, dest) ([]SenderRouting, error)`,
      `ListMessagesByRouting(ctx, accID, dest, limit, excludeMuted)
      ([]Message, error)`,
      `CountMessagesByRouting(ctx, accID, dest, excludeMuted) (int,
      error)`,
      `CountMessagesByRoutingAll(ctx, accID, excludeMuted)
      (map[string]int, error)`. Errors:
      `ErrInvalidDestination`, `ErrInvalidAddress`.
- [ ] `SetSenderRouting` is read-then-write internally (no SQL on
      no-op; `added_at` not bumped when prior == destination).
      Code comment forbids the `INSERT ON CONFLICT DO UPDATE`
      simplification.
- [ ] `NormalizeEmail(s string) string` exported from
      `internal/store/sender_routing.go` (lowercase + trim;
      ASCII-equivalent only — see §8 IDN limit).
- [ ] `internal/pattern/`: `~o <destination>` operator wired
      through `lexer.go::isOpLetter` ('o'), `lexer.go::fieldForOp`
      → `FieldRouting`, `ast.go` adds `FieldRouting`,
      `parser.go::parseRoutingValue` validates against
      `{imbox, feed, paper_trail, screener, none}` (rejects
      `paper-trail`), `compile.go` / `eval_local.go` emits
      EXISTS / NOT EXISTS with unqualified outer column references
      and `lower(trim(from_address))`, `eval_filter.go` and
      `eval_search.go` return `ErrUnsupportedFilter` for
      `FieldRouting`.
- [ ] `KeyMap.StreamChord` + `BindingOverrides.StreamChord`;
      wired through `ApplyBindingOverrides` and
      `findDuplicateBinding`; default `S`.
- [ ] `streamChordPending bool` + `streamChordToken uint64` model
      fields. `S` sets pending and starts `streamChordTimeout`
      Cmd. Stale-token timeout messages are no-ops.
- [ ] Cross-chord cancel: `T` while stream-pending cancels stream
      chord (no thread chord starts); `S` while thread-pending
      cancels thread chord (no stream chord starts). Matches
      existing spec 20 self-cancel discipline.
- [ ] `S i` / `S f` / `S p` / `S k` / `S c` dispatch `routeCmd`.
      `routedMsg` reloads list + status toast; `routeNoopMsg`
      skips the list reload.
- [ ] `routeErrMsg`, `routedMsg`, `routeNoopMsg` typed messages
      with no `String()` / `Error()` (toast-vs-log boundary).
- [ ] Sidebar gains `Streams` section with four entries:
      `__imbox__`, `__feed__`, `__paper_trail__`, `__screener__`.
      `folderItem.isStream bool` + `streamDestination string`
      flag, parallel to existing `isMuted`. Always rendered (count
      may be 0). Counts via `CountMessagesByRoutingAll(account,
      excludeMuted=true)`.
- [ ] Sidebar refresh wired on routing change, `FolderSyncedEvent`,
      and the spec 11 background-refresh tick.
- [ ] List-pane indicator slot per §5.5; priority `📅 > 🔕 > ⚑ >
      📥/📰/🧾/🚪 > ' '`. Off in regular folders by default; always
      on inside routing virtual folders.
- [ ] CLI `cmd/inkwell/cmd_route.go`: `assign|clear|list|show`,
      bare-address validation, exit 2 on bad input.
- [ ] Cmd-bar parity: `:route assign|clear|list|show`.
- [ ] Command-palette rows (spec 22 integration): five static rows
      `route_imbox`, `route_feed`, `route_paper_trail`,
      `route_screener`, `route_clear` in
      `internal/ui/palette_commands.go`, plus `route_show`
      (NeedsArg → cmd-bar). Availability gated on
      `from_address != ""`.
- [ ] `docs/user/reference.md` adds `S` chord rows, `~o` operator
      row, `:route …` rows, `inkwell route` CLI table.
- [ ] `docs/user/how-to.md` adds "Set up Imbox / Feed / Paper
      Trail" recipe.
- [ ] `docs/CONFIG.md` adds `[ui].show_routing_indicator`,
      `[ui.stream_indicators]` (inline-table form),
      `[ui].stream_ascii_fallback`, `[bindings].stream_chord`.
- [ ] `docs/ARCH.md` §"action queue" notes routing as the second
      explicit local-only mutation surface (after mute).
- [ ] `docs/PRD.md` §10 spec inventory adds spec 23.
- [ ] `docs/PRIVACY.md` (when it lands per spec 17) adds
      `sender_routing` to the local-storage list.
- [ ] Tests:
  - migration: `TestMigration011AppliesCleanly`.
  - store: `TestSetSenderRoutingUpsertsAndNormalizes`,
    `TestSetSenderRoutingNoOpReturnsErrAlreadyRouted` (no `added_at`
    bump),
    `TestSetSenderRoutingReassignBumpsAddedAt`,
    `TestSetSenderRoutingRejectsInvalidDestination`,
    `TestSetSenderRoutingRejectsEmptyAddress`,
    `TestClearSenderRoutingNoop`,
    `TestListMessagesByRoutingExcludesMuted`,
    `TestListMessagesByRoutingNormalizesCaseAndWhitespace`,
    `TestListMessagesByRoutingUsesIndex`
    (EXPLAIN QUERY PLAN check),
    `TestCountMessagesByRouting`,
    `TestCountMessagesByRoutingAllReturnsAllFour`,
    `TestSenderRoutingFKCascadeOnAccountDelete`.
  - pattern: `TestParseRoutingOperator` (incl. paper-trail hyphen
    rejection),
    `TestCompileRoutingOperatorLocalOnly`,
    `TestCompileRoutingOperatorTwoStage`,
    `TestCompileRoutingOperatorRejectedByFilterAndSearch`,
    `TestRoutingOperatorNegationVsNone`,
    `TestExecuteRoutingOperatorIntegration`. Fuzz seed:
    `~o feed`, `~o none`, `~o paper_trail`.
  - UI dispatch (e2e): `TestStreamChordSPendingState`,
    `TestStreamChordEscCancels`, `TestStreamChordTimeoutNoop`,
    `TestStreamChordSiRoutesToImbox`,
    `TestStreamChordSkRoutesToScreener`,
    `TestStreamChordScClearsRouting`,
    `TestStreamChordReassignShowsPriorInToast`,
    `TestStreamChordSiOnAlreadyImboxIsNoop`,
    `TestStreamChordNoFromAddressShowsError`,
    `TestStreamChordTPressCancelsStreamChord`,
    `TestThreadChordSPressCancelsThreadChord`,
    `TestStreamChordSSPressIsCancelNotStart`,
    `TestStreamVirtualFoldersRenderInSidebar`,
    `TestStreamVirtualFoldersAlwaysVisibleAtZero`,
    `TestStreamVirtualFolderSelectLoadsByRouting`,
    `TestStreamSentinelFolderRefusesNRX`.
  - CLI: `TestRouteCLIAssignAndShow`,
    `TestRouteCLIListByDestination`,
    `TestRouteCLIRejectsUnknownDestination`,
    `TestRouteCLIRejectsDisplayNameAddress`,
    `TestRouteCLINormalisesCase`.

## Perf budgets

| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `SetSenderRouting` / `ClearSenderRouting` | ≤1ms p95 | — | `BenchmarkSetSenderRouting` | pending |
| `GetSenderRouting` (single sender) | ≤1ms p95 | — | `BenchmarkGetSenderRouting` | pending |
| `ListMessagesByRouting(dest, limit=100)` over 100k msgs + 500 routed senders | ≤10ms p95 | — | `BenchmarkListMessagesByRouting` | pending |
| `CountMessagesByRouting(dest)` over 100k msgs + 500 routed senders | ≤5ms p95 | — | `BenchmarkCountMessagesByRouting` | pending |
| Pattern compile + execute for `~o feed` over 100k msgs | ≤10ms p95 | — | `BenchmarkPatternRoutingOperator` | pending |
| Sidebar refresh of all four bucket counts (batched `CountMessagesByRoutingAll`) | ≤20ms p95 | — | `BenchmarkSidebarBucketRefresh` | pending |

## Iteration log

(To be filled by the implementing ralph loop.)

### Iter 1 — 2026-05-07 (implementation + ship)
- Slice: full implementation — all DoD bullets delivered.
- Commands run: `make regress` green (gofmt, vet, build, race, e2e,
  integration, bench).
- Result: tagged v0.51.0. All DoD bullets satisfied. Key deviation:
  `ListMessagesByRouting` uses EXISTS IN-subquery (not JOIN) to
  avoid column ambiguity with `messageColumns`. Partial index
  `idx_messages_account_received` scoped to non-empty `from_address`
  to avoid `MessageIDsInConversation` regression.
- Critique: none outstanding.
- Next: spec 24.
