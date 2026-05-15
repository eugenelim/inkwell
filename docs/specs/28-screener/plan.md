# Spec 28 — Screener for new senders

## Status
done

## DoD checklist

Mirrors `docs/specs/28-screener/spec.md` §10. Tick as work lands.

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
- [ ] **Gate-flip confirmation modal (§5.3.1)**: at launch,
      when `cfg.Screener.Enabled = true` AND
      `[ui].screener_last_seen_enabled = false` AND
      `M (CountMessagesFromPendingSenders) > 0`, render a
      Confirm-mode modal *before* the first list-pane render.
      `Y` writes marker `true` and proceeds; `N`/`Esc` keeps
      gate off for the session, leaves marker `false`, modal
      re-fires next launch. Skip path on `M == 0`. Disable
      path resets marker.
- [ ] `store.Store.CountMessagesFromPendingSenders` added
      (only used by the §5.3.1 modal).
- [ ] `[ui].screener_last_seen_enabled` config key (default
      `false`).
- [ ] **`config.WriteUIFlag(path, key, value)` writer** added
      (load-bearing for §5.3.1 + §5.3.2; specs 11/23 latent).
      Atomic temp-file + rename, mode `0600`, preserves other
      sections.
- [ ] **Concurrent-decision semantics (§5.4)**: `Y`/`N` capture
      focused address synchronously; `routeCmd` writes
      `sender_routing` directly (spec 23 §6 — bypasses action
      queue); SQLite write lock + `(account_id, email_address)`
      PK conflict-target serialise concurrent upserts.
- [ ] One-time Screener-on hint after gate enable (§5.3.2 —
      copy explicitly names the sidebar swap); dismissed via
      `Esc`; persisted as `[ui].screener_hint_dismissed = true`;
      never re-fires.
- [ ] Empty-queue helper text `(no pending senders — all caught
      up)`; ASCII fallback gated on `[ui].ascii_fallback`.
- [ ] CLI `cmd/inkwell/cmd_screener.go`: `list`, `accept`,
      `reject`, `history`, `pre-approve`, `status`. Bare-address
      validation; exit 2 on bad input; `--to` accepts `imbox` /
      `feed` / `paper_trail` (rejects `screener`); `pre-approve`
      accepts `--from-stdin` OR `--from-file <path>` (mutually
      exclusive); TTY-stdin guard; CRLF / BOM / `#` comment /
      blank-line handling per §7. Registered in `cmd_root.go`.
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
- [ ] User docs: `docs/user/how-to.md` adds three recipes —
      "Turn on the Screener," "Pre-approve senders from a
      contacts dump," and "Recover from a wrong screener
      decision" (HEY Screener History parity via the
      `__screened_out__` virtual folder + `S c`).
- [ ] User docs: `docs/user/explanation.md` adds the local-only
      filter / no-notifications-suppression invariant note
      (§9.1).
- [ ] `docs/CONFIG.md` adds `[screener].*` (4 keys),
      `[bindings].screener_accept` / `screener_reject`,
      `[ui].screener_hint_dismissed`,
      `[ui].screener_last_seen_enabled`.
- [ ] `docs/ARCH.md` §"action queue" mentions spec 28 reuses
      spec 23 `routeCmd`.
- [ ] `docs/PRD.md` §10 spec inventory adds spec 28.
- [ ] `docs/ROADMAP.md` Bucket 3 row + §1.16 backlog heading
      reference spec 28.
- [ ] `docs/specs/23-routing-destinations/spec.md` §10.1 / §14
      forward-link to spec 28 (closes the spec 23 follow-up
      loop).
- [ ] `docs/PRIVACY.md` one-line note that the gate is local-
      only filter logic; no new persisted state beyond what
      spec 23 already shipped.
- [ ] `README.md` status table row.
- [ ] Tests per §10 (store, pattern, UI dispatch / e2e, CLI,
      redaction, benchmarks). All four layers green per
      `docs/CONVENTIONS.md` §5.6.

## Perf budgets

Mirrors `docs/specs/28-screener/spec.md` §8.

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

### Iter 2 — 2026-05-09 — implementation shipped as v0.57.0

- Slice: full implementation per §10 DoD across 11 files (config
  layer, store layer, pattern alias, UI sentinel + sidebar +
  KeyMap + dispatch, modal + hint, palette, cmd-bar, CLI).
- Files added: `internal/config/write_ui_flag.go` (+ test);
  `internal/store/screener.go` (+ test); `internal/ui/screener.go`
  (+ test); `cmd/inkwell/cmd_screener.go` (+ test). Files
  modified: `internal/config/{config,defaults}.go`,
  `internal/store/{types,messages,store}.go`,
  `internal/pattern/{parser.go,routing_test.go}`,
  `internal/ui/{keys,panes,palette_commands,app}.go`,
  `cmd/inkwell/{cmd_run,cmd_root}.go`,
  `internal/store/bench_test.go`.
- Implementation notes:
  - Config writer (`config.WriteUIFlag`) atomic temp-file +
    rename, mode 0600, preserves other sections. Closes the
    spec 11/23 latent hint-dismissal claim.
  - Store screener.go: 6 methods (`ListPendingSenders`,
    `ListPendingMessages`, `ListScreenedOutMessages`,
    `CountPendingSenders`, `CountScreenedOutMessages`,
    `CountMessagesFromPendingSenders`). `ListPendingSenders`
    SQL caps `MessageCount` via `LIMIT cap+1` so a noisy sender
    with 50k messages doesn't dominate. `MessageQuery.ApplyScreenerFilter`
    extends `buildListSQL` with the EXISTS-IN-approved fragment;
    NULL/empty `from_address` exempted.
  - Pattern parser: `~o pending` canonicalises to `none` at
    parse time so eval_local / eval_filter / eval_search /
    eval_memory stay unchanged.
  - UI: sentinel `__screened_out__` added; `FoldersModel`
    gains gate-aware sidebar rendering (`SetScreenerSidebarState`).
    `Y` / `N` pane-scoped to `__screener__` while
    `m.screenerEnabled`; ScreenerReject excluded from
    `findDuplicateBinding` scan due to N overlap with NewFolder
    (pinned by `TestKeymapScreenerRejectExcludedFromDuplicateScan`).
    Default folder views pass `ApplyScreenerFilter = m.screenerEnabled`.
  - Gate-flip detection at boot via `detectScreenerGateFlipCmd`:
    when transitioning false→true with M>0 pending messages,
    `ConfirmMode` modal renders before the first list-pane
    refresh. Y advances `[ui].screener_last_seen_enabled` and
    arms the §5.3.2 hint; N keeps the gate off this session and
    re-issues `loadMessagesCmd` so the user sees the gate-off
    view immediately.
  - Cmd-bar `:screener accept|reject|list|history|status` and
    palette rows (`screener_accept` / `screener_reject` /
    `screener_open` / `screener_history`) wired with title swaps
    when the gate is off.
  - CLI: `cmd_screener.go` registers the six subcommands. Pure-
    stdlib TTY detection (`os.Stdin.Stat() & os.ModeCharDevice`)
    avoids adding `golang.org/x/term` as a dependency.
    `pre-approve` accepts `--from-stdin` OR `--from-file <path>`
    (mutually exclusive); CRLF / BOM / `#` comment / blank-line
    handling per §7.
- Tests added: 12 store unit tests (filter clauses + 6 method
  paths + ordering / cap / mute interactions); 4 pattern alias
  tests (pending→none canonicalisation, LocalOnly compile,
  filter/search rejection); 11 config writer tests (fresh
  file, append, replace, preserves sections, mode 0600, no-op
  when equal, invalid keys); 16 UI tests (Y/N dispatch, pane
  scoping, sidebar gate-on/off rendering, gate-flip modal,
  decline keeps gate off, palette rows + title swap,
  cmd-bar verbs, history-gating); 8 CLI tests (preApproveStream
  parsing rules, mutual exclusion, registration); 1 keymap
  duplicate-scan exclusion test. 7 new benchmarks under §8
  budgets on M5: ListMessagesScreenerFilter ≈640µs (gate 15ms),
  ListPendingSenders ≈10ms, ListPendingMessages ≈1.5ms,
  ListScreenedOutMessages ≈2.4ms, CountPendingSenders ≈3.9ms,
  CountScreenedOutMessages ≈1.3ms, SidebarStreamsRefreshWithScreener
  ≈18ms cumulative (gate 25ms).
- Self-review: ran a final adversarial pass that surfaced 2
  CRITICAL (no UI tests, missing `TestKeymapScreenerRejectExcludedFromDuplicateScan`)
  + 5 MAJOR (missing sidebar bench, modal-ordering race,
  list-pane label, doc sweep undone). All addressed: 16 UI
  tests landed, the duplicate-scan-exclusion test added, the
  sidebar bench added, the gate-flip race fixed via re-issuing
  `loadMessagesCmd` on the N branch, and the doc sweep
  completed (§12.6 table).
- Doc sweep: spec Shipped line `v0.57.0`; PRD §10 row updated;
  ROADMAP Bucket-3 row 2 + §1.16 backlog heading flipped to
  Shipped v0.57.0; spec 23 §10.1 / §14 forward-link to spec 28
  added (closes the spec 23 §14 v1 UX limit); CONFIG.md gains
  `[screener]` section + `[bindings].screener_*` rows + `[ui]`
  marker keys; ARCH.md §"action queue" updated; PRIVACY.md row
  added; user/reference.md gains `Y`/`N` shortcuts row, `~o
  pending` operator alias mention, `:screener` cmd-bar verbs,
  `inkwell screener` CLI rows, footer bumped to v0.57.0;
  user/how-to.md gains three recipes ("Turn on the Screener,"
  "Pre-approve senders from a contacts dump," "Recover from a
  wrong Screener decision"), footer bumped; user/explanation.md
  gains "Why the Screener is local-only" with the
  notification-suppression non-goal, footer bumped to v0.57.0;
  README status table row + download example.
- Critique: the implementation is the minimum-viable wiring
  documented in §10 DoD. No new schema, no new Graph scope,
  no new threat-model surface (§9 §11). Two pre-existing
  calendar-sync tests unrelated to spec 28 were already fixed
  in v0.56.1. The `[ui].screener_hint_dismissed` and
  `[ui].screener_last_seen_enabled` markers are persisted via
  the new `config.WriteUIFlag` writer, which back-fills the
  spec 11 / spec 23 latent claim.
- Next: spec 28 is shipped. Spec 29 (Watch mode) and spec 30
  ("Done" alias) remain as the next Bucket 3 items.

### Iter 0 — 2026-05-08 — spec drafted

- Slice: write the spec from scratch following bucket-3 roadmap
  item 1.16; build on spec 23 and spec 19 patterns.
- Commands run: none (design-only iteration).
- Result: `docs/specs/28-screener/spec.md` landed (≈1100 lines).
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
  commit per `docs/CONVENTIONS.md` §10.

### Iter 1 — 2026-05-08 — additional adversarial review

- Slice: re-review the shipped spec with three parallel
  adversarial agents (consistency, implementability, UX) plus
  follow-up confirmation passes; apply every finding in place.
- Commands run: none (design-only iteration; no code touched).
- Result: spec edits applied across §0.1, §1.1, §1.2, §2.3,
  §4.1, §4.4, §4.5, §5.1, §5.3 (split into §5.3.1 confirm
  modal + §5.3.2 hint), §5.4 (concurrent-decision
  semantics), §5.5 (load-time-only materialisation), §5.6,
  §5.9 (palette title swap), §5.10 (cross-feature
  interactions surfaced in user docs), §6 (added
  `[ui].screener_last_seen_enabled` key), §7 (added
  `--from-file`), §10 DoD (gate-flip modal, race semantics,
  WriteUIFlag, palette title swap, --from-file, marker key,
  recovery recipe), §11 cross-cutting (recovery path
  documented).
- Critique: ran an additional four adversarial-review passes:
  - Pass A (consistency): found 2 MAJOR (migration drift —
    spec 26 shipped, 013 on disk; spec 26/27 in-flight stale)
    + 6 MINOR (AST type name, line numbers, sidebar Stacks
    naming, palette `screener_open` asymmetry, test name
    backwards). All applied.
  - Pass B (implementability vs. live code): found 2 MAJOR
    (§4.1 SQL `m.` alias + named binds wouldn't compile;
    §4.5 needed explicit `pending`→`none` parser
    canonicalisation) + 3 MINOR (ErrUnsupported sentinel
    name, §4.4 named-binds note, line drift). All applied.
  - Pass C (UX): found 2 CRITICAL (no gate-flip
    confirmation; undefined Y/N race semantics) + 4 MAJOR
    (per-message malformed-from no-op trap, bundle
    interaction, reply-later interaction, sidebar swap
    discoverability) + 5 MINOR (--from-file, undo recovery
    path, taxonomy clarification, first-contact framing,
    palette gate-off title). All applied.
  - Pass D (post-fix verification): found 1 CRITICAL — §5.3.1
    wired the modal to a fictional `:reload-config` /
    `configReloadedMsg` that violates `docs/CONVENTIONS.md` §9 ("no hot
    reload"). Reframed as first-launch detection using a new
    `[ui].screener_last_seen_enabled` marker; §5.5
    materialisation reduced to load-time only; tests
    renamed to assert boot-time semantics. Applied.
  - Pass E (post-fix verification): found 1 CRITICAL — §5.4
    incorrectly named the action queue as serialisation
    point for concurrent Y/N decisions, but spec 23 §6 is
    explicit that routing bypasses the action queue.
    Reattributed serialisation to the SQLite write lock +
    `(account_id, email_address)` PK conflict-target. Also
    found that the "auto-write pattern" referenced for
    `[ui]` flags has no infrastructure — added a DoD bullet
    landing a minimal `config.WriteUIFlag` writer with four
    test cases, unblocking spec 11 / spec 23 latent
    hint-dismissal claims.
  - Pass F (post-fix verification): found 1 MINOR —
    nonexistent `[ui].ascii_fallback` config key referenced
    in §5.3.2 + §5.6. Removed the parenthetical; emoji and
    em-dash render unconditionally per shipped behaviour in
    specs 19/22/23. The narrow `[ui].stream_ascii_fallback`
    (which does exist) is named honestly and scoped out of
    spec 28.
  - Pass G (final confirmation): clean — zero findings
    across all severities.
- Next: same as Iter 0 — implementation begins with the
  store-layer slice. The `WriteUIFlag` writer is now its own
  early slice (it unblocks §5.3.1 and the spec 11 / 23 hint
  flags); ordering: writer → store SQL → parser alias → UI →
  palette → CLI.
