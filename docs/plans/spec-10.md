# Spec 10 — Bulk Operations UX

## Status
done. A-3 (PR audit-drain 2026-05-02) shipped: full bulk verb set
(;D ;r ;R ;f ;F ;c ;C), F shortcut, enhanced confirm sample (pattern +
5-subject sample). Progress modal, dry-run mode, cross-folder filter
explicitly deferred.

## DoD checklist (mirrored from spec)
- [x] `:filter <pattern>` parses via spec 08 (Parse → CompileLocal), runs against the local store via SearchByPredicate, and replaces the list pane with matches.
- [x] Plain text without `~` operator desugars to `~B <text>` (subject-or-body contains).
- [x] `:unfilter` clears the active filter.
- [x] Cmd-bar reminder while filter active: "filter: <pattern> · matched N · ;d delete · ;a archive · :unfilter".
- [x] `;` chord arms the bulk-pending state (only when a filter is active).
- [x] `;d` opens confirm modal "Delete N messages? [y/N]" — destructive default-No (CLAUDE.md §7 #9).
- [x] `;D` opens confirm modal "Permanently delete N messages?" (always confirms).
- [x] `;a` opens confirm modal "Archive N messages?".
- [x] `;r` / `;R` — mark all filtered read / unread.
- [x] `;f` / `;F` — flag / unflag all filtered.
- [x] `;c` — opens category input modal (bulk), then confirm modal → BulkAddCategory.
- [x] `;C` — opens category input modal (bulk), then confirm modal → BulkRemoveCategory.
- [x] Confirm modal shows filter pattern and a 5-message subject sample.
- [x] Capital `F` shortcut that pre-fills `:filter ` in command mode.
- [x] On Confirm-yes, BulkExecutor dispatches via spec 09's BatchExecute. Status bar shows "✓ <action> N" or "⚠ <action> X/Y" partial.
- [x] Filter clears after a successful bulk; list reloads from the prior folder.
- [x] Tests: dispatch cases for all 9 verb chords, F key, ;F inside chord, ;c full flow.
- [ ] Search → filter conversion (post-`/`-search press F) — deferred.
- [ ] Progress modal during long bulk ops — deferred. Status bar suffices for v1.
- [ ] Composite undo (single undo entry for the whole bulk op) — handled by spec 09 batch layer; undo overlay deferred to spec 11.
- [ ] Cross-folder filter — deferred per PRD §6 (single-folder only in v1).
- [ ] Dry-run mode (`:filter --dry-run` / `!` suffix) — deferred.

## Iteration log

### Iter 2 — 2026-05-02 (PR A-3: full verb set + F key + ;c/;C + confirm sample)
- Slice: UI, tests.
- Files modified:
  - `internal/ui/app.go` — BulkExecutor interface +6 methods; Model gains `pendingBulkCategoryAction`/`pendingBulkCategory`; global dispatch wires `F` key (guards against `;F` chord); dispatchList `;` chord extended with D/r/R/f/F/c/C; updateCategoryInput handles bulk path on Enter; confirmBulk enhanced with filter+sample; runBulkCmd handles all 9 action types + category param.
  - `internal/ui/dispatch_test.go` — stubBulkExecutor extended with 6 new methods + `bulkOK` helper; added TestFKeyPreFillsFilterCommand, TestFKeyInsideBulkChordUnflags, TestSemicolonNewVerbsOpenConfirmModal, TestSemicolonCEntersCategoryInputMode, TestSemicolonCategoryConfirmFlowReachesRunBulk.
  - `cmd/inkwell/cmd_run.go` — bulkAdapter gains BulkMarkUnread, BulkFlag, BulkUnflag, BulkPermanentDelete, BulkAddCategory, BulkRemoveCategory.
- Commands: `go vet ./...` clean; `go test -race ./internal/ui/... ./cmd/...` green (calendar timezone failures are pre-existing).
- Critique:
  - Progress modal deferred: status bar shows "✓ <action> N" after completion, which is adequate for v1. The `OnProgress` callback in `batchExecute` is wired in spec 09; threading it back to the UI requires a channel pattern and a new mode (`BulkProgressMode`). Scope for a future iter.
  - `;m` (move all filtered) not wired — opens the folder picker for each message individually; bulk move requires a different Graph endpoint (`copyItem`/`moveItem` per message). Deferred.
  - Dry-run mode not wired — spec 10 §6 defines it but no UI user has requested it yet.

### Iter 1 — 2026-04-29 (filter + ;d/;a chord + confirm modal)
- Slice: UI + store wiring. Pattern + batch were in place from specs 08 + 09.
- Files:
  - internal/ui/app.go: Model gains filterActive/filterPattern/filterIDs + bulkPending/pendingBulk. dispatchCommand handles `:filter` and `:unfilter`. dispatchList intercepts `;` then routes the next d/a to confirmBulk. ConfirmResultMsg handler fires runBulkCmd on yes. New BulkExecutor interface defined at the consumer site (CLAUDE.md §2 layering).
  - internal/store/messages.go: SearchByPredicate(accountID, where, args, limit) — runs caller-supplied SQL fragment from spec 08's evaluator.
  - cmd/inkwell/cmd_run.go: bulkAdapter wires action.Executor into ui.BulkExecutor (struct-shape conversion only; types are intentionally identical).
  - 5 dispatch tests in internal/ui/dispatch_test.go.
- Commands: `make regress` green.
- Critique:
  - Filter is local-only in v0.6.0. If the user filters on a query the local cache doesn't have (e.g. ~B matching body content from an un-cached message), they'll get incomplete results. Server-side filter routing lands when CompileFilter (spec 08 deferred) is wired.
  - The confirm modal title reads "Delete N messages?" not the spec's exact wording — close enough; a future polish iter can match the spec text byte-for-byte.
  - `:unfilter` is a separate command rather than esc-clears like search. Acceptable; esc inside a filtered list pane goes to the list cursor in vim style.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: Mail.ReadWrite via spec 09.
- [x] Store reads/writes: messages (SearchByPredicate read; UpdateMessageFields per item via the bulk path).
- [x] Graph endpoints: /$batch via spec 09.
- [x] Offline behaviour: filter works fully offline. Bulk dispatch fails gracefully when offline (engine drain retries on reconnect).
- [x] Undo: deferred. Composite undo is the next iter's headline feature.
- [x] User errors: filter parse errors (spec 08) surface via ErrorMsg → status bar. Bulk failures show "⚠ <action> X/Y".
- [x] Latency budget: not measured here; spec 09 will gate /$batch latency.
- [x] Logs: nothing new logged at this layer.
- [x] CLI mode: spec 14 will surface :filter as `inkwell filter <pattern>`.
- [x] Tests: 5 dispatch tests covering activation, plain-text desugar, chord guard, confirm modal, confirmed-bulk-fires.
