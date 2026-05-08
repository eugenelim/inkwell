# Spec 28 — Screener for new senders

## Status
not-started

## DoD checklist

Mirrors `docs/specs/28-screener.md` §10. Tick as work lands.

- [ ] **No new migration.** `ls internal/store/migrations/`
      pre-merge to confirm slot 013 is still the next free
      slot (claimed by spec 26 in flight; spec 28 claims none).
- [ ] `MessageQuery.ApplyScreenerFilter bool` added;
      `buildListSQL` emits the EXISTS-IN-approved sub-clause
      when true; NULL / empty `from_address` exempted.
- [ ] `store.Store` gains `ListPendingSenders`,
      `ListPendingMessages`, `ListScreenedOutMessages`,
      `CountPendingSenders`, `CountScreenedOutMessages`.
      `PendingSender` struct exported.
- [ ] `ListPendingSenders` SQL: `ORDER BY received_at DESC,
      address ASC` deterministic tie-break. `message_count` cap
      via `LIMIT :cap_plus_one` wrapper subquery (production
      form per §4.4).
- [ ] `internal/pattern/parser.go::parseRoutingValue` accepts
      `pending` as alias for `none`; both compile to
      `OpRouting{dest:"none"}` AST node.
- [ ] `KeyMap` gains `ScreenerAccept` (default `Y`) and
      `ScreenerReject` (default `N`). `BindingOverrides` mirror.
      `ScreenerAccept` added to `findDuplicateBinding` scan;
      `ScreenerReject` excluded with inline comment citing the
      pane-scoped overlap with spec 18 `NewFolder`.
- [ ] `internal/config/config.go::BindingsConfig` gains
      `ScreenerAccept` / `ScreenerReject` fields with TOML tags;
      defaults registered in `internal/config/defaults.go`;
      config-to-`BindingOverrides` wiring assigns both.
- [ ] `Model` gains `screenerEnabled`, `screenerGrouping`,
      `screenerExcludeMuted`, `screenerMaxCountPerSender`
      fields. Materialised in `ui.New(deps)` and re-set in
      `configReloadedMsg` handler. TUI reads only these mirrors.
- [ ] `dispatchList` Screener-pane branch: when
      `displayedFolder.sentinelID == "__screener__"` AND
      `m.screenerEnabled`, `Y` → `routeCmd(addr, "imbox")` and
      `N` → `routeCmd(addr, "screener")`. Outside the Screener
      pane, both keys are unbound (no fallthrough).
- [ ] `__screened_out__` sentinel ID added to
      `internal/ui/panes.go` constants, `IsStreamSentinelID`
      switch, and `streamDestinationFromID` mapping (→
      `"screener"`).
- [ ] Sidebar Streams renders `__screened_out__` only when
      gate is on; the four spec 23 stream entries always render
      (unchanged).
- [ ] Sidebar count source flips with the gate:
      `__screener__` → `CountMessagesByRouting('screener')`
      (gate off) or `CountPendingSenders` (gate on).
      `__screened_out__` → `CountScreenedOutMessages` (gate on
      only). `refreshStreamCountsCmd` reads `m.screenerEnabled`
      once per refresh.
- [ ] Selecting `__screener__` calls `ListPendingSenders` (or
      `ListPendingMessages` per `[screener].grouping`) when gate
      on; falls back to `ListMessagesByRouting('screener')`
      when off.
- [ ] Selecting `__screened_out__` calls
      `ListScreenedOutMessages`. List-pane top shows
      `[screened out]`.
- [ ] Default folder views pass `ApplyScreenerFilter =
      m.screenerEnabled` to `ListMessages`. Search, filter, and
      CLI paths always pass false.
- [ ] One-time Screener-on hint after gate enable; dismissed
      via `Esc`; persisted as `[ui].screener_hint_dismissed =
      true`; never re-fires.
- [ ] Empty-queue helper text `(no pending senders — all caught
      up)`; ASCII fallback gated on `[ui].ascii_fallback`.
- [ ] CLI `cmd/inkwell/cmd_screener.go`: `list`, `accept`,
      `reject`, `history`, `pre-approve`, `status`. Bare-address
      validation; exit 2 on bad input; `--to` accepts `imbox` /
      `feed` / `paper_trail` (rejects `screener`); TTY-stdin
      guard; CRLF / BOM / `#` comment / blank-line handling per
      §7. Registered in `cmd_root.go`.
- [ ] Cmd-bar parity: `:screener accept|reject|list|history|status`
      via the same handlers.
- [ ] Command palette: four static rows per §5.9 with
      `Available()` rule (focused message + non-empty
      `from_address` + `m.deps.Store != nil`).
- [ ] User docs: `docs/user/reference.md` adds `Y`/`N`
      Screener-pane shortcuts, `~o pending` operator alias,
      `:screener` cmd-bar verbs, `inkwell screener` CLI table.
      Streams section updated for the gated content shift +
      new Screened-Out entry. `_Last reviewed against vX.Y.Z._`
      footer bumped.
- [ ] User docs: `docs/user/how-to.md` adds two recipes —
      "Turn on the Screener" and "Pre-approve senders from a
      contacts dump."
- [ ] User docs: `docs/user/explanation.md` adds the local-only
      filter / no-notifications-suppression invariant note
      (§9.1).
- [ ] `docs/CONFIG.md` adds `[screener].*` (4 keys),
      `[bindings].screener_accept` / `screener_reject`,
      `[ui].screener_hint_dismissed`.
- [ ] `docs/ARCH.md` §"action queue" mentions spec 28 reuses
      spec 23 `routeCmd`.
- [ ] `docs/PRD.md` §10 spec inventory adds spec 28.
- [ ] `docs/ROADMAP.md` Bucket 3 row + §1.16 backlog heading
      reference spec 28.
- [ ] `docs/specs/23-routing-destinations.md` §10.1 / §14
      forward-link to spec 28 (closes the spec 23 follow-up
      loop).
- [ ] `docs/PRIVACY.md` one-line note that the gate is local-
      only filter logic; no new persisted state beyond what
      spec 23 already shipped.
- [ ] `README.md` status table row.
- [ ] Tests per §10 (store, pattern, UI dispatch / e2e, CLI,
      redaction, benchmarks). All four layers green per
      CLAUDE.md §5.6.

## Perf budgets

Mirrors `docs/specs/28-screener.md` §8.

| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `ListMessages(folder, ApplyScreenerFilter=true, limit=100)` over 100k+500+200 fixture | ≤15ms p95 | — | `BenchmarkListMessagesScreenerFilter` | not measured |
| `ListPendingSenders(limit=200)` over same fixture | ≤15ms p95 | — | `BenchmarkListPendingSenders` | not measured |
| `ListPendingMessages(limit=200)` | ≤10ms p95 | — | `BenchmarkListPendingMessages` | not measured |
| `ListScreenedOutMessages(limit=200)` | ≤10ms p95 | — | `BenchmarkListScreenedOutMessages` | not measured |
| `CountPendingSenders` | ≤10ms p95 | — | `BenchmarkCountPendingSenders` | not measured |
| `CountScreenedOutMessages` | ≤5ms p95 | — | `BenchmarkCountScreenedOutMessages` | not measured |
| Sidebar refresh of all five Streams (gate on) | ≤25ms p95 cumulative | — | `BenchmarkSidebarStreamsRefreshWithScreener` | not measured |

## Iteration log

### Iter 0 — 2026-05-08 — spec drafted

- Slice: write the spec from scratch following bucket-3 roadmap
  item 1.16; build on spec 23 and spec 19 patterns.
- Commands run: none (design-only iteration).
- Result: `docs/specs/28-screener.md` landed (≈1100 lines).
  Plan file (this file) created. PRD §10, ROADMAP §0 Bucket 3 +
  §1.16 backlog cross-refs updated in the same commit.
- Critique: ran four adversarial-review passes against the
  spec (general-purpose subagent). Pass 1 found 2 critical /
  7 major / 6 minor issues; Pass 2 found 0 critical / 3 major
  / 6 minor; Pass 3 found 0 critical / 0 major / 7 minor;
  Pass 4 found 0 critical / 0 major / 1 minor; final pass
  clean across all severities. Each pass's findings were
  applied in full before the next ran.
- Next: implementation — start with the store-layer slice
  (`ApplyScreenerFilter` field + the four new store methods +
  their tests), then the pattern-parser alias, then the UI
  / palette wiring, then the CLI. Each landing in a separate
  commit per CLAUDE.md §10.
