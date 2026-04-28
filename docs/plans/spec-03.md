# Spec 03 — Sync Engine + Graph Client

## Status
done (CI scope) — full-tenant integration test deferred per CLAUDE.md §5.5.

## DoD checklist (mirrored from spec)
- [x] Graph HTTP client with auth → throttle → logging transport stack.
- [x] 401 triggers `auth.Invalidate()` + retry once.
- [x] 429 / 503 honour `Retry-After` (numeric + HTTP-date) up to MaxRetries with exponential fallback.
- [x] Concurrency cap enforced via semaphore (verified: 12 goroutines × cap 3 stays ≤ 3).
- [x] Context cancellation observed inside the throttle wait.
- [x] `GraphError` parses Graph error envelope; `Is*` classification helpers cover throttled/auth/syncStateNotFound/notFound.
- [x] Engine state machine: idle → drain actions → sync folders → idle (drain MUST run first; verified in test).
- [x] Per-folder delta loop persists `@odata.deltaLink`; resumes from cursor on next call (verified).
- [x] `syncStateNotFound` clears the persisted token and re-initialises transparently (verified).
- [x] `nextLink` pagination accumulates results across pages (verified).
- [x] `@removed` tombstones delete locally (verified).
- [x] Folder enumeration deletes server-removed folders, cascading messages (verified).
- [x] `SyncCompletedEvent` emitted with FoldersSynced count.
- [x] Polling cadence: single `time.Timer` reset per cycle; `SetActive(true/false)` switches between fg (30s) / bg (5min); `Sync*` calls kick the loop via wakeup channel.
- [x] Privacy: delta sync $select asserted **never** contains the literal `body` field (bodyPreview is fine; spec §5.2). Tested.
- [x] Logging redaction: bearer tokens scrubbed in captured slog output (carried over from internal/log tests).
- [ ] **Deferred (manual):** real-tenant 90-day backfill + repeated delta cycles + `:backfill <date>` foreground command.

## Iteration log

### Iter 1 — 2026-04-27
- Slice: graph/{errors,client,types,folders,delta,messages}.go + tests.
- Files added: 5 source files + 1 test file in graph.
- Commands: `go test -race ./internal/graph/...` — green in 2.7s.
- Critique: none (clean run).

### Iter 2 — 2026-04-27
- Slice: sync/{engine,folders,delta,backfill}.go + tests.
- Initial compile error: defined a method on `*store.DeltaToken` from the sync package — illegal cross-package method def. Fixed by extracting to a free function `deltaURL(t, folderID)`.
- Then test compile error: cross-package access to test helpers (OpenTestStore lived in store_test.go).
- First fix attempt: extracted `internal/store/storetest` subpackage. That created an import cycle (store_test.go → storetest → store) at test compile time.
- Final fix: inline minimal helpers in sync's `engine_test.go` (just the two it needs), and keep store's helpers as `store/testhelpers_test.go` for store-internal tests.
- TestBudgetsHonoured failed under -race because the race detector inflates per-op time and 50k seeding blows the 60s test timeout. Skipped under race via build-tagged `isRaceEnabled()`. Race-disabled run still gates all six budgets.
- Privacy test was too loose: `bodyPreview` contains the substring `body`. Tightened to comma-split + word equality.
- Final: `go test -race -timeout 90s ./...` green across the whole tree.

### Iter 5 — 2026-04-28 (FK constraint fix in syncFolders + visibility)
- Trigger: real-tenant log finally surfaced the actual root cause: `sync folders: constraint failed: FOREIGN KEY constraint failed (787)`. Every cycle since v0.2.0 had been hitting this, retrying every 30s, never succeeding. The Graph response for `/me/mailFolders` returns each top-level folder with `parentFolderId = msgfolderroot` (the well-known mailbox-root ID, not in our response). Inserting that violated `folders.parent_folder_id → folders.id`.
- Fix (spec §7 / sync/folders.go): build a `known` set of folder IDs from the response BEFORE the upsert loop. For each folder, if `parentFolderID` isn't in `known`, NULL it out. Folders with tracked parents preserve the relationship.
- Visibility additions in this iter (since the bug was invisible to the user — TUI showed "(select a folder)" while the engine retried every 30s):
  - New `FoldersEnumeratedEvent` emitted from `runCycle` immediately after the folder enumeration step. The TUI re-loads its sidebar BEFORE per-folder syncs complete (or even if a per-folder sync later errors out).
  - `engine.Start` wraps the loop goroutine in `defer recover` so panics surface as `SyncFailedEvent` instead of dying silently inside bubbletea's alt-screen.
  - Status bar now shows engine activity ("syncing folders…" / "syncing…" / "✓ synced HH:MM" / "ERR: …" / "waiting for sync…"). The "waiting for sync…" idle state replaces the previous unconditional "—" so a stuck-and-silent engine is more obvious. Last-error display in red.
  - cmd/inkwell prints `logs: <path>` to stderr at startup so the user sees the log path before alt-screen takes over.
- Test: TestSyncFolderEnumerationNullsOutUntrackedParents asserts both behaviours (untracked parent → NULL; tracked parent → preserved).

### Iter 4 — 2026-04-28 (engine boots immediately; Inbox-first runCycle; visibility breadcrumbs)
- Trigger: real-tenant smoke after v0.2.1 — TUI launched but folders/messages never appeared. The cmd-layer had two goroutines (SyncAll + QuickStartBackfill) firing alongside `engine.Start()`. Both could fail silently to a `logger.Warn` while the TUI's status bar showed nothing. Meanwhile the engine's internal `loop` was sitting on its first `time.NewTimer(e.interval())` for the full foreground interval (default 30s) before doing any work.
- Slice:
  - `engine.loop` runs the FIRST cycle immediately, then enters the timer reset loop. Spec §5 already said "On Start():" — this aligns the code with the spec.
  - `runCycle` iterates folders via `orderForQuickStart(filterSubscribed(...))` so Inbox is always first regardless of which path triggered the cycle.
  - cmd-layer's duplicate `SyncAll` + `QuickStartBackfill` goroutines deleted. The engine handles its own first-launch.
  - Breadcrumb logs added: `engine: starting`, `sync: cycle starting`, `sync: enumerated folders (total=N, subscribed=M)`, per-folder `sync: folder begin`, `sync: cycle complete (folders=M, duration=…)`, plus `engine: first cycle failed (err=…)` at Error level. v0.2.0's Warn-only swallowing is replaced with Error so silent failures stop being silent.
- Tests: existing TestQuickStartBackfillInboxFirst, TestSyncFolderResumesPersistedNextLink, TestSyncFolderQuickStartYieldsAfterOnePage still pass — the engine semantics didn't change, only the activation timing and ordering.

## Notes for follow-up specs
- Spec 04 (TUI shell) consumes `sync.Engine` via the `Notifications()` channel. The status-line model dispatches typed events (`SyncCompletedEvent`, `ThrottledEvent`, `AuthRequiredEvent`).
- Spec 09 (batch executor) implements the `ActionDrainer` interface that this spec stubs with a noop. The engine constructor accepts any `ActionDrainer` so spec 09 can drop in cleanly.
- Spec 06 (search) does not interact with the engine; it reads the local store directly.
- The graph client's `OnThrottle` callback is wired into the engine in spec 04 (status-line consumer) — for spec 03 the engine emits `ThrottledEvent` itself only on cycle-level errors. Per-request throttle notifications go through the graph option.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: `Mail.Read` (delta + folders) — already in PRD §3.1.
- [x] Store reads/writes: folders (UpsertFolder/DeleteFolder), messages (UpsertMessagesBatch/DeleteMessage), delta_tokens (Get/Put/Clear).
- [x] Graph endpoints: `/me/mailFolders`, `/me/mailFolders/{id}/messages/delta`, `/me/mailFolders/{id}/messages` (backfill), `/me/messages/{id}` (body fetch via existing helper).
- [x] Offline behaviour: engine is the only writer to Graph; UI reads from store, so offline is transparent.
- [x] Undo: N/A for this spec (no triage actions yet).
- [x] User errors: `SyncFailedEvent` carries the wrapped error; per-folder failures log and continue.
- [x] Latency budget: not gated yet (the spec doesn't list one beyond "1000 envelopes/sec" target, which I'll measure in spec 09's batch executor work).
- [x] Logs: all auth/throttle/request lines go through the redacting slog handler.
- [x] CLI mode: spec 14 will add `inkwell sync` CLI.
- [x] Tests: unit + integration via httptest + race-clean.

## Iter — auth pivot 2026-04-27
- Spec 03 functionality is unchanged by the spec-01 auth pivot (first-party Microsoft Graph CLI Tools client, /common authority, no tenant app registration). This package consumes the auth surface only via the typed `Authenticator` / `Token()` / `Invalidate()` contract, which is unchanged. No code changes needed; race + e2e + budget gates re-run and all green.
