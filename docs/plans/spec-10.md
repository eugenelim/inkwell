# Spec 10 — Bulk Operations UX

## Status
in-progress (CI scope: filter mode + ;d/;a chord + confirm modal + bulk dispatch shipped in v0.6.0; capital-F shortcut, search→filter conversion, progress modal, undo overlay deferred to v0.6.x).

## DoD checklist (mirrored from spec)
- [x] `:filter <pattern>` parses via spec 08 (Parse → CompileLocal), runs against the local store via SearchByPredicate, and replaces the list pane with matches.
- [x] Plain text without `~` operator desugars to `~B <text>` (subject-or-body contains).
- [x] `:unfilter` clears the active filter.
- [x] Cmd-bar reminder while filter active: "filter: <pattern> · matched N · ;d delete · ;a archive · :unfilter".
- [x] `;` chord arms the bulk-pending state (only when a filter is active).
- [x] `;d` opens confirm modal "Delete N messages? [y/N]" — destructive default-No (CLAUDE.md §7 #9).
- [x] `;a` opens confirm modal "Archive N messages?".
- [x] On Confirm-yes, BulkExecutor dispatches via spec 09's BatchExecute. Status bar shows "✓ <action> N" or "⚠ <action> X/Y" partial.
- [x] Filter clears after a successful bulk; list reloads from the prior folder.
- [x] Tests: 5 dispatch cases (filter activates, plain text wraps in ~B, ; without filter is no-op, ;d opens confirm, confirm-yes fires bulk Cmd) + reused stub BulkExecutor.
- [ ] Capital `F` shortcut that pre-fills `:filter ` — deferred. `:filter` is the discoverable form.
- [ ] Search → filter conversion (post-`/`-search press F) — deferred.
- [ ] Progress modal during long bulk ops — deferred. Status bar suffices.
- [ ] Composite undo (single undo entry for the whole bulk op) — deferred to spec 11 + undo overlay.
- [ ] Cross-folder filter — deferred per PRD §6 (single-folder only in v1).

## Iteration log

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
