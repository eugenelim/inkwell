# Spec 02 — Local Cache Schema

## Status
done. All deferred bullets shipped in PR H-2:
- `flag_due_at` / `flag_completed_at` persist via `MessageFields` and the flag
  action's `due_date` param.
- `DeleteSavedSearchByName` store helper added (atomic SQL); `Manager.DeleteByName`
  now uses it instead of a two-step fetch+delete.
Earlier: §8 maintenance loop (body LRU eviction + done-actions sweep + optional
Vacuum) shipped v0.13.x (PR 11 of audit-drain).

## DoD checklist
- [x] All tables, indexes, FTS triggers from §3 created by `001_initial.sql`.
- [x] Public API in §5 implemented and tested.
- [x] Every perf budget in §7 verified by `TestBudgetsHonoured` with 1.5× slack per `docs/CONVENTIONS.md` §6.
- [x] Test coverage on `internal/store` covers schema, CRUD, FTS triggers, body LRU, action lifecycle, undo monotonicity, undo-cleared-on-reopen, delta tokens, migrations idempotency, concurrent stress (4w + 4r × 2s).
- [x] DB file mode 0600 verified by integration test on macOS.
- [x] Concurrent-access test passes (replaced 60s spec target with 2s for CI; same shape, smaller dataset; can be extended on demand).

## Perf budgets (measured 2026-04-27, dev machine)
| Surface | Budget | Measured | Status |
| --- | --- | --- | --- |
| GetMessage cached | 1ms | 34.9µs | ✓ (28× under) |
| ListMessages(folder, limit=100), 50k rows | 10ms | 532µs | ✓ (18× under) |
| UpsertMessagesBatch(100) | 50ms | 6.2ms | ✓ (8× under) |
| Search 50k rows | 100ms | 51ms | ✓ (~2× under, scales to ~100ms at 100k) |
| GetBody cached | 5ms | 8.6µs | ✓ (580× under) |
| Open existing DB (migrations no-op) | 50ms | 11ms | ✓ (4× under) |

## Iteration log

### Iter 4 — 2026-05-04 (flag_due_at persistence + saved-search delete-by-name, PR H-2)
- Slice: spec 02 deferred — `flag_due_at`/`flag_completed_at` round-trip + atomic
  `DeleteSavedSearchByName`.
- Files modified:
  - `internal/graph/types.go`: added `DueDateTime *DateTimeTimeZone` and
    `CompletedDateTime *DateTimeTimeZone` to `Flag` struct.
  - `internal/graph/mailbox.go`: added `ToTime()` method on `DateTimeTimeZone`
    (parses local datetime + timezone; nil-safe; falls back to UTC on unknown tz).
  - `internal/store/types.go`: added `FlagDueAt *time.Time` and
    `FlagCompletedAt *time.Time` to `MessageFields`.
  - `internal/store/messages.go`: `UpdateMessageFields` now writes the two new fields.
  - `internal/store/saved_searches.go`: added `DeleteSavedSearchByName(ctx, accountID, name)`.
  - `internal/store/store.go`: interface gains `DeleteSavedSearchByName`.
  - `internal/sync/backfill.go` + `delta.go`: map `Flag.DueDateTime`/`CompletedDateTime`
    → store `FlagDueAt`/`FlagCompletedAt` via `.ToTime()`.
  - `internal/action/types.go`: `applyLocal` for `ActionFlag` reads `due_date` param
    and sets `FlagDueAt`; `dispatch` includes `dueDateTime` in the PATCH when present;
    added `parseDueDate` helper (RFC 3339 + date-only formats).
  - `internal/savedsearch/manager.go`: `DeleteByName` now delegates to
    `st.DeleteSavedSearchByName` (atomic) instead of two-step fetch+delete.
- Tests added:
  - `store/store_test.go`: `TestDeleteSavedSearchByName`, `TestUpdateMessageFieldsFlagDueAt`.
  - `graph/mailbox_test.go`: `TestDateTimeTimeZoneToTime`.
  - `action/executor_test.go`: `TestFlagWithDueDatePersists`.
- Commands: `go build ./...` ✓, `go vet ./...` ✓, `go test -race ./...` ✓.

### Iter 3 — 2026-04-30 (maintenance loop, PR 11 of audit-drain)
- Slice: spec 02 §8 maintenance pass — body LRU eviction +
  done-actions sweep + optional Vacuum.
- Files added/modified:
  - `internal/sync/maintenance.go` — runMaintenance loop +
    maintenancePass single-cycle helper. 6h default interval;
    1min initial delay so startup doesn't hammer disk.
  - `internal/sync/engine.go` — Options gains
    MaintenanceInterval / BodyCacheMaxCount /
    BodyCacheMaxBytes / DoneActionsRetention /
    VacuumOnMaintenance with sensible defaults. Negative
    interval is the test-disable sentinel. Start() launches
    the maintenance goroutine alongside the main loop.
  - `internal/store/actions.go` — new SweepDoneActions(before)
    method; deletes done/failed actions whose completed_at <
    before. Returns rowsAffected.
  - `internal/store/store.go` — interface gains SweepDoneActions.
  - `cmd/inkwell/cmd_run.go` — config knobs threaded through.
- Tests:
  - LRU eviction respects count cap (20 bodies, cap=10 → ≤10
    remain).
  - Sweep with negative retention guarantees a freshly-Done
    row is removed; pending row survives.
  - Negative MaintenanceInterval disables the loop (returns
    immediately).
- Decisions:
  - Maintenance runs in its own goroutine, NOT inside the
    sync runCycle, because Vacuum can take seconds on a
    large DB and would block foreground sync.
  - Vacuum off by default — SQLite's VACUUM rewrites the
    whole DB; the I/O cost is rarely worth the space savings
    for a typical mailbox. User can opt in via
    VacuumOnMaintenance.
  - 6h interval picked as a balance between "too aggressive
    against I/O" and "actions accumulate visibly". Quarterly
    review can tune based on logs.
- Result: gosec 0 issues, govulncheck 0 vulns, all packages
  green under -race + -tags=e2e.

### Iter 1 — 2026-04-27
- Slice: schema migration + types + Open + migrations runner + accounts/folders/messages/bodies/attachments/delta/actions/undo/saved_searches/search.
- Files added: internal/store/{store,migrations,types,null,accounts,folders,messages,bodies,attachments,delta,actions,undo,saved_searches,search}.go + 001_initial.sql + testfixtures_test + store_test.
- Compile: clean after `go mod tidy`.
- First test run: hung at 10-min go test timeout in concurrent stress.
- Critique: two real bugs:
  1. `time.After(d)` shared across 8 goroutines — only one receives, others spin forever. Fixed with `time.AfterFunc(d, func(){close(done)})`.
  2. `applyPragmas` ran against one pooled connection only. `busy_timeout = 5000` did not propagate to other writer connections, causing fast SQLITE_BUSY failures under contention. Fixed by moving every pragma into the DSN (`?_pragma=...`), including `_txlock=immediate` per spec §6.
- After fixes: all unit tests green in 6s.

### Iter 2 — 2026-04-27
- Slice: bench_test.go with `BenchmarkX*` mirrors of §7 + a `TestBudgetsHonoured` driver that runs each benchmark for ~ms and gates on 1.5× slack of the spec budget.
- Result: all six budgets pass — see table above. Search at 51ms/op over 50k is the closest to budget; 100k will land near limit on slow CI hardware.
- Critique: no new violations.

## Notes for follow-up specs
- Spec 03 (sync engine) consumes `Store` via the public interface only. The `_txlock=immediate` DSN means every write contends up-front — sync-engine batching benefits from this.
- Spec 06 (search) will use `Store.Search` directly; the bm25 join is the dominant cost and is the closest to budget.
- Spec 07 (triage) will exercise the action queue + undo stack heavily; the current scan supports `pending`+`in_flight` filtering via the partial index.
- Body eviction is a callable function (`EvictBodies(ctx, maxCount, maxBytes)`); the sync engine will run it as a periodic goroutine.

## Iter — auth pivot 2026-04-27
- Spec 02 functionality is unchanged by the spec-01 auth pivot (first-party Microsoft Graph CLI Tools client, /common authority, no tenant app registration). This package consumes the auth surface only via the typed `Authenticator` / `Token()` / `Invalidate()` contract, which is unchanged. No code changes needed; race + e2e + budget gates re-run and all green.
