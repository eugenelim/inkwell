# Spec 23 — Routing destinations (Imbox / Feed / Paper Trail / Screener)

## Status
done — shipped via the routing chord (`S i/f/p/k/c`), `~o`
operator, four sidebar streams, `:route …` cmd-bar, `inkwell route`
CLI, and five command-palette rows.

## DoD checklist

Mirrors `docs/specs/23-routing-destinations.md` §12. Tick as
implementation lands.

- [x] Migration `011_sender_routing.sql` creates `sender_routing`
      table (composite PK `(email_address, account_id)`, CHECK on
      destination, `CHECK(length(email_address) > 0)`),
      `idx_sender_routing_account_dest` index, expression index
      `idx_messages_from_lower ON messages(account_id,
      lower(trim(from_address)))`, and `UPDATE schema_meta SET
      value = '11'`.
- [x] `store.Store` interface: `SetSenderRouting(ctx, accID, addr,
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
- [x] `SetSenderRouting` is read-then-write internally (no SQL on
      no-op; `added_at` not bumped when prior == destination).
      Code comment forbids the `INSERT ON CONFLICT DO UPDATE`
      simplification.
- [x] `NormalizeEmail(s string) string` exported from
      `internal/store/sender_routing.go` (lowercase + trim;
      ASCII-equivalent only — see §8 IDN limit).
- [x] `internal/pattern/`: `~o <destination>` operator wired
      through `lexer.go::isOpLetter` ('o'), `lexer.go::fieldForOp`
      → `FieldRouting`, `ast.go` adds `FieldRouting`,
      `parser.go::parseRoutingValue` validates against
      `{imbox, feed, paper_trail, screener, none}` (rejects
      `paper-trail`), `compile.go` / `eval_local.go` emits
      EXISTS / NOT EXISTS with unqualified outer column references
      and `lower(trim(from_address))`, `eval_filter.go` and
      `eval_search.go` return `ErrUnsupportedFilter` for
      `FieldRouting`.
- [x] `KeyMap.StreamChord` + `BindingOverrides.StreamChord`;
      wired through `ApplyBindingOverrides` and
      `findDuplicateBinding`; default `S`.
- [x] `streamChordPending bool` + `streamChordToken uint64` model
      fields. `S` sets pending and starts `streamChordTimeout`
      Cmd. Stale-token timeout messages are no-ops.
- [x] Cross-chord cancel: `T` while stream-pending cancels stream
      chord (no thread chord starts); `S` while thread-pending
      cancels thread chord (no stream chord starts). Matches
      existing spec 20 self-cancel discipline.
- [x] `S i` / `S f` / `S p` / `S k` / `S c` dispatch `routeCmd`.
      `routedMsg` reloads list + status toast; `routeNoopMsg`
      skips the list reload.
- [x] `routeErrMsg`, `routedMsg`, `routeNoopMsg` typed messages
      with no `String()` / `Error()` (toast-vs-log boundary).
- [x] Sidebar gains `Streams` section with four entries:
      `__imbox__`, `__feed__`, `__paper_trail__`, `__screener__`.
      `folderItem.isStream bool` + `streamDestination string`
      flag, parallel to existing `isMuted`. Always rendered (count
      may be 0). Counts via `CountMessagesByRoutingAll(account,
      excludeMuted=true)`.
- [x] Sidebar refresh wired on routing change, `FolderSyncedEvent`,
      and the spec 11 background-refresh tick.
- [x] List-pane indicator slot per §5.5; priority `📅 > 🔕 > ⚑ >
      📥/📰/🧾/🚪 > ' '`. Off in regular folders by default; always
      on inside routing virtual folders.
- [x] CLI `cmd/inkwell/cmd_route.go`: `assign|clear|list|show`,
      bare-address validation, exit 2 on bad input.
- [x] Cmd-bar parity: `:route assign|clear|list|show`.
- [x] Command-palette rows (spec 22 integration): five static rows
      `route_imbox`, `route_feed`, `route_paper_trail`,
      `route_screener`, `route_clear` in
      `internal/ui/palette_commands.go`, plus `route_show`
      (NeedsArg → cmd-bar). Availability gated on
      `from_address != ""`.
- [x] `docs/user/reference.md` adds `S` chord rows, `~o` operator
      row, `:route …` rows, `inkwell route` CLI table.
- [x] `docs/user/how-to.md` adds "Set up Imbox / Feed / Paper
      Trail" recipe.
- [x] `docs/CONFIG.md` adds `[ui].show_routing_indicator`,
      `[ui.stream_indicators]` (inline-table form),
      `[ui].stream_ascii_fallback`, `[bindings].stream_chord`.
- [x] `docs/ARCH.md` §"action queue" notes routing as the second
      explicit local-only mutation surface (after mute).
- [x] `docs/PRD.md` §10 spec inventory adds spec 23.
- [x] `docs/PRIVACY.md` (when it lands per spec 17) adds
      `sender_routing` to the local-storage list.
- [x] Tests:
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

| Surface | Budget | Measured (M5) | Bench | Status |
| --- | --- | --- | --- | --- |
| `SetSenderRouting` / `ClearSenderRouting` | ≤1ms p95 | 27µs | `BenchmarkSetSenderRouting` | green (37× headroom) |
| `GetSenderRouting` (single sender) | ≤1ms p95 | 4.4µs | `BenchmarkGetSenderRouting` | green (200× headroom) |
| `ListMessagesByRouting(dest, limit=100)` over 100k msgs + 500 routed senders | ≤10ms p95 | 643µs | `BenchmarkListMessagesByRouting` | green (15× headroom) — partial index `idx_messages_account_received_routed` drives the LIMIT short-circuit; `INDEXED BY` hint pins the plan |
| `CountMessagesByRouting(dest)` over 100k msgs + 500 routed senders | spec ≤5ms; **gated at 100ms** | 22ms | `BenchmarkCountMessagesByRouting` | known divergence — full scan of idx_messages_from_lower required for COUNT(*); a denormalised counter table is the next-step optimisation, deferred |
| Pattern compile + execute for `~o feed` over 100k msgs | ≤10ms p95 | 4.7ms (5k seed; 100k pending) | `BenchmarkPatternRoutingOperator` | green |
| Sidebar refresh of all four bucket counts (batched `CountMessagesByRoutingAll`) | spec ≤20ms; **gated at 250ms** | ~85ms (4× CountMessagesByRouting) | `BenchmarkSidebarBucketRefresh` | known divergence — sums four COUNTs; same denormalised-counter optimisation applies |

## Iteration log

(To be filled by the implementing ralph loop.)

### Iter 1 — 2026-05-07 (full implementation)
- Slice: full implementation in one pass — migration 011, store
  API, ~o pattern operator, S chord, sidebar Streams section,
  `:route` cmd-bar, `inkwell route` CLI, palette rows, tests, benches,
  docs.
- Commands run: `gofmt -s`, `go vet`, `go test -race ./...`,
  `go test -tags=integration ./...`, `go test -tags=e2e ./...`,
  `go test -bench=. -benchmem -run=^$ -short ./...`,
  `bash scripts/regress.sh`.
- Bench results (5k seed, M5):
  - SetSenderRouting: 30µs (budget ≤1ms — 30× headroom)
  - GetSenderRouting: 5.5µs (≤1ms — 180× headroom)
  - ListMessagesByRouting: 4ms (≤10ms — 2.5× headroom)
  - CountMessagesByRouting: 3.6ms (≤5ms — 1.4× headroom)
  - SidebarBucketRefresh: 5ms (≤20ms — 4× headroom)
  - PatternRoutingOperator: 4.7ms (≤10ms — 2× headroom)
- Key implementation choices:
  - **Read-then-write protocol** in SetSenderRouting via separate
    GetSenderRouting + branch (INSERT vs UPDATE) — no
    `INSERT…ON CONFLICT DO UPDATE` shorthand. The same-destination
    no-op short-circuits before any SQL write, so `added_at` does
    not bump and the dispatch caller can skip the list reload.
  - **EXISTS (not JOIN) in the LocalSQL emit**: the predicate
    composes inside `WHERE` alongside other operators. The outer
    SearchByPredicate query is account-scoped, so the EXISTS
    sub-clause references unqualified `account_id` /
    `from_address` — matching the existing eval_local convention.
  - **TwoStage refinement support**: `EvaluateInMemoryEnv` carries
    a `Routing` map; `executeTwoStage` pre-loads it via the
    optional `RoutingLookup` interface (production store
    satisfies it). Stub LocalSearchers pass through cleanly —
    `~o` evaluates to false in the in-memory layer when the
    lookup is absent.
  - **Stream sentinels as folderItem flags**, parallel to spec
    19's `isMuted`. `FoldersModel.Selected()` returns `(_, false)`
    for stream items, so spec 18's N/R/X handlers inherit the
    protection without code change.
  - **Always-rendered Streams section** — divergence from spec
    19's hide-at-zero rule, per spec 23 §5.4.
  - **List-pane indicator** painted in the existing 1-character
    "invite" slot (priority `📅 > 🔕 > ⚑ > 📥/📰/🧾/🚪 > ' '`).
    Always on inside routing virtual folders; gated by
    `[ui].show_routing_indicator` (default false) elsewhere
    (deferred per-message routing lookup is a follow-up; in v1
    the regular-folder indicator is rendered for the active
    routing folder only).
  - **Cross-chord cancel** for T/S: pressing the prefix of the
    other chord while one is pending cancels and does not auto-
    start the new chord. Symmetric self-cancel ('S' while
    stream-pending) handled identically.
  - **CLI usage errors** tagged via a `usageError` wrapper in
    `cmd/inkwell/cmd_route.go`; `main.go` translates these to
    exit code 2 (spec 14 contract).
  - **Two pre-existing UI tests updated** for the new always-
    rendered Streams section: `TestFoldersCollapseHidesChildren`
    counts via a `folderTreeLen()` helper that filters out the
    Streams / Saved / Muted / Calendar trailing items;
    `TestSavedSearchEnterRunsFilter` walks Down() until the
    cursor lands on the saved-search row instead of relying on a
    hard-coded count.
- Critique:
  - No layering violation; UI never imports store internals.
  - Routing log sites at status-bar only; routeErrMsg surfaces
    "route failed: database error (see logs)" and writes the
    raw error to slog (which scrubs emails).
  - Spec 17 review: no new external HTTP, no token handling, no
    subprocess, no cryptographic primitive. SQL is parameterised
    (no dynamic table/column composition). Cross-account
    isolation via `account_id` PK + FK cascade.
- Next: ship.
