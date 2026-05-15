# Spec 09 — Batch Engine

## Status
done. A-2 (PR audit-drain 2026-05-02) shipped: bounded concurrency,
per-sub-request 429 retry, composite undo, add/remove category,
permanent-delete sub-request, hard cap, [batch] config.
Throughput bench explicitly deferred (no 100k+ match UI flow yet).

## DoD checklist (mirrored from spec)
- [x] `graph.BatchBuilder` builds Graph $batch payloads with the 20-per-batch hard limit.
- [x] `graph.Client.ExecuteBatch` posts /$batch and returns parsed sub-responses correlated by id.
- [x] Sub-response error envelope parsed into `*GraphError` per item.
- [x] `action.Executor.BatchExecute(ctx, accountID, type, ids)` applies optimistically, dispatches via $batch in 20-per-chunk groups, rolls back per-message on Graph failure.
- [x] Action types covered: mark_read, mark_unread, flag, unflag, soft_delete, archive (move), permanent_delete, add_category, remove_category.
- [x] Outer batch error rolls back the whole chunk; per-sub-response error rolls back only that message.
- [x] Bounded concurrency (`ExecuteAllOpts.Concurrency`) — `graph.ExecuteAll` fans out up to `[batch].batch_concurrency` chunks in parallel.
- [x] Per-sub-request 429 retry with Retry-After honored — `executeChunkWithRetry` retries throttled sub-requests; outer-call 429 retries the whole chunk.
- [x] Composite undo entry (one undo per bulk op) — pushed for reversible types (mark_read↔unread, flag↔unflag, add_category↔remove_category); `Undo()` routes bulk entries to `batchExecute`.
- [x] `config.BatchConfig` struct with `[batch]` TOML section and defaults; `SetBatchConfig` on Executor; wired in cmd_run.go.
- [x] Hard cap (`[batch].bulk_size_hard_max`) — batchExecute rejects before starting.
- [x] New typed wrappers: BulkMarkUnread, BulkFlag, BulkUnflag, BulkPermanentDelete, BulkAddCategory, BulkRemoveCategory.
- [ ] Throughput bench — deferred until spec 10 has a UI flow that drives 100k+ matches.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| Bulk throughput bench | TBD (spec 10 drives it) | — | — | deferred |

## Iteration log

### Iter 1 — 2026-04-29 (chunked synchronous batch)
- Slice: graph + action both at once. The graph layer just translates BatchBuilder → JSON → /$batch HTTP. The action layer wraps that with optimistic local-apply + per-message rollback.
- Files: internal/graph/batch.go, internal/action/batch.go, internal/action/batch_test.go (~370 LOC).
- Commands: `go test -race ./internal/action/...` green in 3.2s.
- Critique:
  - Outer-batch error rolls back the whole chunk. That's coarser than the spec calls for; future iter can split a 20-batch into smaller retries on 503.
  - actionToSubRequest duplicates the URL/body shape from executor.dispatch. They're both deriving from the same action type. A future refactor could share the Graph translation; not done now because dispatch returns an error from the http.Response while ToSubRequest only builds the request — the divergence is real.

### Iter 2 — 2026-05-02 (PR A-2: concurrency + retry + composite undo + config)
- Slice: batch, bench, retry, undo, config.
- Files added:
  - `internal/graph/batch_retry.go` — `executeChunkWithRetry`, `retryAfterFromHeaders`, `retryAfterFromErr`
  - `internal/graph/batch_chunk.go` — `ExecuteAll` with semaphore-based fan-out, `ExecuteAllOpts`, `toGraphError`
  - `internal/graph/batch_retry_test.go` — 5 tests (outer 429 retry, sub-request 429 retry, input order preserved, OnProgress callback)
  - `internal/action/batch_spec09_test.go` — 8 tests (hard cap, sub-429 retry success, sub-429 exhausted, perm_delete, add_category, composite undo, bulk undo round-trip, concurrent chunks)
- Files modified:
  - `internal/action/batch.go` — rewrote around `batchExecute(extraParams, skipUndo)`, added `ExecuteAll` fan-out, hard cap, composite undo push, new action types (perm_delete, add_category, remove_category), new typed wrappers
  - `internal/action/executor.go` — added `batchCfg config.BatchConfig`, `SetBatchConfig`, bulk-undo path in `Undo()`
  - `internal/action/batch_test.go` — fixed race on `sizes` slice; made chunking assertion order-independent
  - `internal/config/config.go` — added `BatchConfig` struct, `Batch BatchConfig` in Config
  - `internal/config/defaults.go` — defaults for `[batch]`
  - `cmd/inkwell/cmd_run.go` — `exec.SetBatchConfig(cfg.Batch)`
- Commands: `go vet ./...` clean; `go test -race ./internal/graph/... ./internal/action/... ./internal/config/...` green.
- Critique:
  - Bulk undo is best-effort on partial failure (some messages succeed, some fail) — we don't re-push a partial undo. Acceptable for v1.
  - actionToSubRequest and dispatch() both encode the same Graph URL/body shapes. Real divergence (one returns SubRequest, one fires HTTP), so not abstracted.
  - Throughput bench deferred: no UI flow yet to drive 100k+ scenario.
  - Next: commit + push, update audit-drain plan.

## Cross-cutting checklist (`docs/CONVENTIONS.md` §11)
- [x] Scopes used: Mail.ReadWrite (already in PRD §3.1).
- [x] Store reads/writes: messages (optimistic apply + rollback per item), actions (Enqueue + UpdateActionStatus per item), undo stack (PushUndo for bulk composite).
- [x] Graph endpoints: POST /$batch (with retry and concurrent fan-out).
- [x] Offline behaviour: per-chunk outer errors converted to per-response GraphErrors; engine drain retries Pending on next online cycle.
- [x] Undo: composite undo for reversible action types; `Undo()` dispatches bulk entries via batchExecute.
- [x] User errors: hard cap returns error before starting; per-message failures surface in BatchResult.Err.
- [x] Latency budget: not benched; spec 10 UI flow will drive the budget.
- [x] Logs: graph transport stack logs request/response; redaction applies.
- [x] CLI mode: spec 14.
- [x] Tests: 12 action tests + 5+ graph tests covering all new paths.
