# Spec 09 — Batch Engine

## Status
in-progress (CI scope: synchronous chunked $batch shipped in v0.6.0; bounded concurrency, sub-request throttle retry, composite undo deferred to v0.6.x).

## DoD checklist (mirrored from spec)
- [x] `graph.BatchBuilder` builds Graph $batch payloads with the 20-per-batch hard limit.
- [x] `graph.Client.ExecuteBatch` posts /$batch and returns parsed sub-responses correlated by id.
- [x] Sub-response error envelope parsed into `*GraphError` per item.
- [x] `action.Executor.BatchExecute(ctx, accountID, type, ids)` applies optimistically, dispatches via $batch in 20-per-chunk groups, rolls back per-message on Graph failure.
- [x] Action types covered: mark_read, mark_unread, flag, unflag, soft_delete, archive (move).
- [x] Outer batch error rolls back the whole chunk; per-sub-response error rolls back only that message.
- [x] Tests: 4 cases — happy path (3 messages, single batch), partial failure (m-2 → 403), chunking at 20 (25 messages → 2 batches), soft_delete uses well-known alias.
- [ ] Bounded concurrency (`ExecuteAllOpts.Concurrency`) — deferred. v0.6.0 dispatches chunks serially.
- [ ] Per-sub-request 429 retry with Retry-After honored — deferred. v0.6.0 relies on the engine drainer for retry; failed actions sit Pending until the next sync cycle.
- [ ] Composite undo entry (one undo per bulk op) — deferred to spec 11/undo overlay.
- [ ] Throughput bench — deferred until spec 10 has a UI flow that drives 100k+ matches.

## Iteration log

### Iter 1 — 2026-04-29 (chunked synchronous batch)
- Slice: graph + action both at once. The graph layer just translates BatchBuilder → JSON → /$batch HTTP. The action layer wraps that with optimistic local-apply + per-message rollback.
- Files: internal/graph/batch.go, internal/action/batch.go, internal/action/batch_test.go (~370 LOC).
- Commands: `go test -race ./internal/action/...` green in 3.2s.
- Critique:
  - Outer-batch error rolls back the whole chunk. That's coarser than the spec calls for; future iter can split a 20-batch into smaller retries on 503.
  - actionToSubRequest duplicates the URL/body shape from executor.dispatch. They're both deriving from the same action type. A future refactor could share the Graph translation; not done now because dispatch returns an error from the http.Response while ToSubRequest only builds the request — the divergence is real.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: Mail.ReadWrite (already in PRD §3.1).
- [x] Store reads/writes: messages (UpdateMessageFields per item), actions (Enqueue + UpdateActionStatus per item).
- [x] Graph endpoints: POST /$batch.
- [x] Offline behaviour: BatchExecute returns the outer error per-message; engine drain retries Pending on next online cycle.
- [x] Undo: deferred (composite undo).
- [x] User errors: ExecuteBatch returns the outer GraphError; BatchExecute surfaces per-message errors via BatchResult.
- [x] Latency budget: not benched; spec 10's UI flow will drive the budget.
- [x] Logs: graph package logs request/response via the existing transport stack; redaction applies.
- [x] CLI mode: spec 14.
- [x] Tests: 4 unit tests covering happy / partial / chunking / soft-delete-alias paths.
