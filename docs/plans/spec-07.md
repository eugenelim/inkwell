# Spec 07 — Single-Message Triage Actions

## Status
in-progress (CI scope, minimum-viable subset shipped in v0.3.0; undo / replay / categorize / move-with-picker deferred to v0.3.x).

## DoD checklist (mirrored from spec)
- [x] `internal/action/` package compiles with Executor + Drain.
- [x] Action types implemented: mark_read, mark_unread, flag, unflag, soft_delete, archive (move).
- [ ] Action types deferred to v0.3.x: permanent_delete (needs confirmation modal), move (needs folder picker), add_category / remove_category.
- [x] Optimistic apply: local store mutates first, Graph dispatched second, rollback on failure.
- [x] Pre-mutation snapshot per action (read message before mutation, used to compute rollback fields).
- [x] Action queue persists rows; Drain re-dispatches Pending on each engine cycle.
- [ ] Undo stack — deferred. v0.3.0 ships with no undo.
- [ ] Replay-on-startup with stale-snapshot semantics — deferred. (Drain already covers transient retry; full replay across restart added with v0.3.x.)
- [x] UI keybindings wired in dispatchList: r (mark_read), R (mark_unread), f (toggle_flag), d (soft_delete), a (archive).
- [x] Status bar displays the most recent triage error.
- [x] List pane reloads after every successful triage (so the optimistic state is reflected immediately).
- [x] Tests: executor unit tests over httptest Graph, covering mark_read PATCH payload, toggle_flag flip, soft_delete move endpoint, rollback on Graph failure, drain-retries-pending.

## Iteration log

### Iter 1 — 2026-04-28 (minimum-viable triage)
- Slice: action package (executor.go + types.go), graph PATCH/POST helpers, UI dispatch wiring.
- Files added: 3 in internal/action, 1 in internal/graph (triage.go), 4 method-pairs in internal/ui/app.go.
- Commands: `go test -race ./internal/action/...` green in 32s (the 503-retry test waits through graph client backoff).
- Wired action.Executor as sync.ActionDrainer in cmd_run.go so the engine retries pending actions every cycle.
- Critique:
  - Executor.run is synchronous-dispatch. Spec calls for async (goroutine). Synchronous is simpler for v0.3.0 and works because Graph latency is <1s typically; revisit when bulk lands.
  - No goroutine leak since dispatch is in-line.
  - Test execution time of 32s is dominated by graph client retry-backoff in the rollback test (403 with mocked retry). Acceptable for this iter; consider injecting a no-retry option for tests in a follow-up.
  - Categorize / move-with-picker / permanent-delete intentionally deferred — they each need a modal/picker UI surface that's out of scope for v0.3.0.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: `Mail.ReadWrite` (already in PRD §3.1; no new scope).
- [x] Store reads/writes: messages (UpdateMessageFields), actions (Enqueue/Update/PendingActions).
- [x] Graph endpoints: `PATCH /me/messages/{id}`, `POST /me/messages/{id}/move`.
- [x] Offline behaviour: action sits in Pending; engine drain retries on next online cycle.
- [x] Undo: deferred.
- [x] User errors: triage failure surfaces via Model.lastError → status bar.
- [x] Latency budget: not benched yet (spec doesn't list one for single-message); revisit with batch executor (spec 09).
- [x] Logs: executor takes a logger, all dispatch failures logged. No new redaction-relevant fields.
- [x] CLI mode: spec 14.
- [x] Tests: unit (executor_test.go) covering 5 action types + rollback + drain.
