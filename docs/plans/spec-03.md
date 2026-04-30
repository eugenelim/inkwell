# Spec 03 — Sync Engine + Graph Client

## Status
done (CI scope). ThrottledEvent + AuthRequiredEvent emission
shipped v0.13.x (PR 3 of audit-drain). Full-tenant integration
test still deferred per CLAUDE.md §5.5.

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

### Iter 8 — 2026-04-30 (event emission, PR 3 of audit-drain)
- Slice: spec 03 §3 invariants — ThrottledEvent never emitted +
  AuthRequiredEvent never emitted (UI handlers were dead code).
- Files modified:
  - `internal/sync/engine.go` — `Engine` interface gains
    `OnThrottle(retryAfter)`; `engine.OnThrottle` emits
    `ThrottledEvent`. New `emitCycleFailure(err)` helper
    classifies via `graph.IsAuth` and emits either
    `AuthRequiredEvent` or `SyncFailedEvent`. The two existing
    cycle-error sites in `loop()` route through the new helper.
  - `cmd/inkwell/cmd_run.go` — graph.Client constructed with
    an `OnThrottle` closure that captures the engine pointer;
    once the engine is built (a few lines below), the closure
    forwards 429 retries. The chicken-and-egg was the simplest
    place to land this without restructuring construction.
- Tests:
  - `engine_test.go` — three new cases:
    1. `TestEngineForwardsThrottleAsEvent` — direct OnThrottle
       call surfaces as ThrottledEvent on the channel.
    2. `TestEngineGraphClientIntegrationEmitsThrottle` — full
       wired path: 429 → graph.Client → closure → engine
       OnThrottle → ThrottledEvent. Mirrors cmd_run.go's
       wiring exactly.
    3. `TestEngineEmitsAuthRequiredOn401` — runCycle on a 401-
       returning server emits AuthRequiredEvent (not the
       generic SyncFailedEvent).
- Decisions:
  - Engine interface gains a method (`OnThrottle`); the UI's
    narrower Engine interface (in `internal/ui/app.go`) doesn't
    need it because the UI only consumes events. Test stubs
    that satisfy the narrower interface didn't need updates.
  - The closure-capture pattern in cmd_run.go intentionally
    handles the case where graph.Client is built before the
    engine — the only alternative was a setter on graph.Client
    that mutates `onThrottle` post-construction, which would
    expose the field publicly for no other reason.
- Result: gosec 0 issues, govulncheck 0 vulns, all packages
  green under -race + -tags=e2e.

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

### Iter 7 — 2026-04-27 (wellKnownName tenant-quirk workaround)
- Trigger: real-tenant smoke after v0.2.4. The `$select=...,wellKnownName,...` introduced in iter 6 caused Graph to 400 on at least one tenant: "Could not find a property named 'wellKnownName' on type 'microsoft.graph.mailFolder'." TUI showed "Folders (waiting)" and never recovered. The property IS on the resource but isn't returnable via `$select` on the LIST endpoint for every tenant.
- Slice:
  - `internal/graph/folders.go`: dropped the `$select` entirely. `ListFolders` now hits `/me/mailFolders?$top=100` and lets Graph return its default property set (which includes displayName, parentFolderId, totalItemCount, unreadItemCount, isHidden, but NOT wellKnownName).
  - `internal/sync/folders.go`: added `englishDisplayNameToWellKnown` map covering the canonical 10 standard folders (Inbox, Sent Items, Drafts, Archive, Junk Email + "Junk E-mail" variant, Deleted Items, Conversation History, Sync Issues, Outbox) and `inferWellKnownName(displayName) string`. `syncFolders` infers it from DisplayName when Graph returns it empty, before UpsertFolder.
  - `internal/ui/panes.go`: bumped status-bar error truncation from 60 → 120 chars so future Graph 400s (which are verbose) are at least partially readable in the bar; full text is in the log file.
- Limitation noted in code: locale-sensitive. Non-English tenants see empty well-known mappings and fall back to display-name match in the Inbox-default picker (already in place from iter 6). A future iter can switch to per-folder accessor calls (GET /me/mailFolders/{name}) for locale-agnostic resolution; left as low-prio since target audience is English M365 mailboxes.
- Tests: existing `TestSyncFolderEnumerationNullsOutUntrackedParents` still passes. No new test for the inference helper since it's a pure map lookup; behaviour is exercised by the existing folder-enum tests when `WellKnownName` is empty in the fixture.

### Iter 6 — 2026-04-28 (newest-by-receivedDateTime + wellKnownName fix + URL encoding fix)
- Triggers from real-tenant smoke after v0.2.3:
  1. "It's not the most recent emails." First-launch was hitting `/messages/delta?$top=50`, but Graph v1.0's delta endpoint doesn't support `$orderby`. The 50 returned were in Graph's internal order (typically by `lastModifiedDateTime`).
  2. "Junk E-mail" and "Sync Issues" being synced even though they're in `excludedWellKnown`. Cause: Graph's `/me/mailFolders` doesn't return `wellKnownName` by default — you have to `$select` it explicitly. Without it, every folder came back with empty `WellKnownName`, so `filterSubscribed` treated them all as user folders.
  3. Inbox-default picker falling back to alphabetical first folder (often Archive) for the same reason.
- Slice:
  - `internal/graph/folders.go`: add `FolderSelectFields` constant including `wellKnownName`, request it via `$select` in `ListFolders`. Single-line bug, biggest-impact fix.
  - `internal/sync/delta.go`: replace pickURL with explicit `quickStart` / `pullSince` / `followDeltaPage` paths. Quick-start hits `/messages?$top=50&$orderby=receivedDateTime desc`. Steady-state (LastDeltaAt set, no DeltaLink) hits `/messages?$filter=receivedDateTime gt {since}&$orderby=receivedDateTime desc`. The delta path remains as `followDeltaPage` for code that pre-seeds DeltaLink (future iter).
  - `internal/graph/messages.go`: switch URL building to `net/url.Values.Encode()`. Previous string concat broke on the space in `$orderby=receivedDateTime desc` (Graph 400'd).
  - `internal/ui/app.go`: Inbox-default picker now has a 3-step fallback (wellKnownName → display_name=Inbox → first folder).
  - `internal/ui/panes.go`: folder pane gets a "▌ Folders" header. Sidebar folders are sorted Inbox → Sent → Drafts → Archive → user (alpha) → Junk/Deleted/etc.
  - `internal/ui/app.go`: bottom help bar with pane-scoped key hints (`j/k: nav · ⏎: open · …`).
- Spec 03 §5.2 rewritten to document the non-delta-endpoint approach.
- Tests reshaped: existing delta-based tests pre-seed a DeltaLink to exercise `followDeltaPage`. New tests cover `/messages` with `$top=50` and `$orderby=receivedDateTime desc`, plus pullSince's `$filter` clause.

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
