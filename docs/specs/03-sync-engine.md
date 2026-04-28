# Spec 03 — Sync Engine

**Status:** Ready for implementation.
**Depends on:** Specs 01 (auth), 02 (store). ARCH §4 (state machine), §5 (graph client), §6 (body strategy).
**Blocks:** Specs 04 (TUI shell), 06 (search), 07 (triage), 09 (batch), 10 (bulk).
**Estimated effort:** 3–4 days. The most complex spec in the set.

---

## 1. Goal

Build the engine that keeps the local SQLite cache in sync with Microsoft Graph. The engine owns:

1. Initial backfill (first-run, 90 days deep).
2. Incremental delta sync (per-folder, polling cadence).
3. Action queue draining (writes → Graph).
4. Reconciliation when Graph state diverges from local optimistic state.
5. Throttling and retry for all Graph traffic.

The engine is **the only thing that talks to Graph** for mail data (the auth module talks to AAD; everything else goes through here).

## 2. Module layout

```
internal/sync/
├── engine.go        # main Engine struct, public API, state machine
├── backfill.go      # initial 90-day pull
├── delta.go         # per-folder delta query loop
├── actions.go       # action queue draining
├── folders.go       # folder enumeration sync
├── reconcile.go     # divergence handling
└── tick.go          # foreground/background ticker
```

```
internal/graph/
├── client.go        # base HTTP client, transport stack
├── messages.go      # /me/messages REST helpers
├── folders.go       # /me/mailFolders REST helpers
├── delta.go         # delta query response parsing
├── batch.go         # $batch builder/executor (spec 09)
└── errors.go        # Graph error parsing, retry classification
```

The graph client is a separate concern from the sync engine. The sync engine USES the graph client; the client is a low-level REST helper.

## 3. Public Engine API

```go
package sync

type Engine interface {
    // Start begins the sync loop. Idempotent.
    Start(ctx context.Context) error

    // Stop signals the engine to shut down. Drains in-flight calls before returning.
    Stop(ctx context.Context) error

    // SetActive informs the engine that the user is foregrounded (true) or
    // backgrounded (false). Affects polling cadence.
    SetActive(active bool)

    // Sync triggers an immediate sync of the named folder, bypassing the tick.
    // Returns when sync of that folder completes.
    Sync(ctx context.Context, folderID string) error

    // SyncAll triggers immediate sync of all subscribed folders.
    SyncAll(ctx context.Context) error

    // Backfill triggers a backward-in-time fetch on the named folder, going
    // back beyond the 90-day initial window.
    Backfill(ctx context.Context, folderID string, until time.Time) error

    // ResetDelta clears the delta token for a folder, forcing a re-sync.
    // Used when syncStateNotFound is encountered.
    ResetDelta(ctx context.Context, folderID string) error

    // Notifications returns a channel that emits sync events for the UI to consume.
    Notifications() <-chan Event
}

type Event interface {
    isEvent()
}

type FolderSyncedEvent struct {
    FolderID string
    Added    int
    Updated  int
    Deleted  int
    At       time.Time
}

type SyncStartedEvent struct{ At time.Time }
type SyncCompletedEvent struct{ At time.Time; FoldersSynced int }
type SyncFailedEvent    struct{ At time.Time; Err error }
type ActionResultEvent  struct{ ActionID string; Status string; Err error }
type ThrottledEvent     struct{ RetryAfter time.Duration }
type AuthRequiredEvent  struct{ At time.Time }
```

## 4. Lifecycle and state machine

States as in ARCH §4:

```
   ┌─────────────┐
   │  idle       │◄───────────────────┐
   └──────┬──────┘                    │
          │ tick / wake / explicit    │
          ▼                           │
   ┌─────────────┐                    │
   │  draining   │ ── flush done ─►   │
   │  actions    │                    │
   └──────┬──────┘                    │
          │                           │
          ▼                           │
   ┌─────────────┐                    │
   │  syncing    │ ── all done ───────┘
   │  folders    │
   └─────────────┘
```

**Order matters:** drain actions BEFORE running deltas. Reasoning: if the user moved a message from Inbox to Archive locally, then delta runs against Inbox before action drains, the delta will tell us "the message was moved" (because we already moved it), but it won't know it's our doing. Worse, if the action fails mid-batch, we'd have to undo the delta side effect. Drain first means the server is the truth before we ask "what changed?".

State transitions are driven by:
- `tick` from the cadence ticker (foreground 30s, background 5min)
- `wake` from `Sync`/`SyncAll`/`Backfill` calls
- internal completion signals

State is stored in memory in the engine; not persisted. On restart, the engine starts in `idle` and a tick is scheduled immediately.

## 5. First-launch detection and lazy progressive backfill

The first-launch path is optimised for **time to first paint** rather than completeness. On a heavy mailbox we don't want the user staring at an empty TUI for several minutes; on a light mailbox we don't want gratuitous "loading" spinners.

The strategy is **last-50-per-folder, drained progressively** using the existing `delta_tokens.next_link` column from spec 02 §3.7. The schema was deliberately designed for this — a `next_link` value means "we're mid-pagination, resume from here on the next tick."

On `Start()`:

1. Run **folder enumeration** once (cheap: <100 folders for typical users; one paginated `GET /me/mailFolders` call).
2. Detect first-launch per folder by `delta_tokens` row absence (spec 02 §3.7).
3. For first-launch folders, **kick the quick-start path** (§5.2). Inbox first, then the rest of the subscribed set sequentially.
4. On each subsequent sync tick, the engine looks for any folder whose `delta_tokens` row has a non-empty `next_link`. If found, it follows ONE more page (50 envelopes), persists the new `next_link` (or the final `delta_link` if pagination exhausted), and emits a `FolderSyncedEvent` so the UI re-loads.
5. Once `delta_link` is set on a folder, that folder shifts to the standard incremental delta loop (§6) — already implemented.

### 5.1 Subscribed folders

By default, subscribed = `Inbox` + `Sent Items` + `Drafts` + `Archive` + every user-created folder (NOT `Deleted Items`, NOT `Junk Email`, NOT `Conversation History`, NOT `Sync Issues`, NOT search folders).

The first-launch quick-start ordering is:

1. **Inbox first.** Enables interactive use within seconds.
2. Then `Sent Items`, `Drafts`, `Archive`, in that order.
3. Then user-created folders, alphabetically.

This order is sequential, not parallel — the per-mailbox concurrency cap of 4 (ARCH §5.2) is shared with body fetches and action drains, and we want the user's first inbox view to land before the engine starts grinding through 30 user folders.

The user can override per-folder subscription via `:folder subscribe <name>` / `:folder unsubscribe <name>`. Subscription state lives in `folders.last_synced_at` (NULL = unsubscribed; non-NULL = subscribed and last synced).

### 5.2 Quick-start: last 50 per folder, newest-by-receivedDateTime

For each first-launch folder, call the **non-delta** `/messages` endpoint with `$top=50` and explicit `$orderby=receivedDateTime desc`:

```
GET /me/mailFolders/{id}/messages?$select={envelope_fields}&$top=50&$orderby=receivedDateTime desc
```

Why not `/messages/delta`? Graph v1.0's delta endpoint **does not support `$orderby`**. With `$top=50` it returns the first 50 messages in Graph's internal ordering (typically `lastModifiedDateTime`, not `receivedDateTime`), so users see whichever 50 messages Graph happens to surface, not the most recent ones. Real-tenant smoke after v0.2.3 surfaced this — the user reported "it's not the most recent emails." The non-delta endpoint accepts `$orderby` and gives us the guarantee we want.

Trade-off: we forfeit Graph's delta tombstones (`@removed` markers for deletions and moves). For the v0.2.x read-only flow this is acceptable. A future iter can add a background "drain delta to seed the cursor" pass for full incremental sync.

Three possible responses:

| Response | Action |
|---|---|
| Has `@odata.deltaLink` (tiny folder, all in one page) | Persist the deltaLink. Folder shifts to incremental sync immediately. |
| Has `@odata.nextLink` (more to drain) | Persist the nextLink. The next sync tick will follow ONE more page. |
| Has both (rare; spec-compliant Graph never does) | Prefer deltaLink. |

In all cases, parse the page's messages and `UpsertMessagesBatch` them. Emit one `FolderSyncedEvent` per page so the UI re-loads.

The envelope `$select` is unchanged from the previous design:

```
$select=id,internetMessageId,conversationId,conversationIndex,subject,bodyPreview,
        from,toRecipients,ccRecipients,bccRecipients,
        receivedDateTime,sentDateTime,
        isRead,isDraft,flag,importance,inferenceClassification,
        hasAttachments,categories,webLink,
        parentFolderId,lastModifiedDateTime
```

### 5.3 Drain `next_link` across sync ticks

The delta loop in §6 is updated to consult `delta_tokens` in this order on every call:

1. If `next_link` is non-empty: follow it (mid-pagination). On response, store the new `next_link` (still draining) or `delta_link` (drained). Done for this tick.
2. Else if `delta_link` is non-empty: standard incremental delta call. Process the page; if the response carries another `nextLink` (delta result was paginated), persist it as `next_link` and drain on subsequent ticks.
3. Else (first-launch with no row at all): quick-start (§5.2).

The result: the user's inbox is filled progressively. After the first tick they see the newest 50; after the second, 100; and so on. No magic 90-day cliff.

### 5.4 Older mail on demand

The 90-day cliff is gone from the auto-backfill. To pull older mail explicitly, the user runs:

```
:backfill <folder> <duration>
```

…where `<duration>` is e.g. `30d`, `6m`, `2y`. Implementation: `GET /me/mailFolders/{id}/messages?$filter=receivedDateTime ge {now-duration}&$top=100&$select={envelope_fields}` with full pagination (this *is* foreground-blocking and the user is asked to wait).

The TUI command-bar plumbing for `:backfill` lands with spec 07 (which adds argument-taking commands generally). For v0.2.0 the engine method exists (`engine.Backfill(ctx, folderID, until time.Time)`) but is only callable via the existing CLI interface or programmatically — that's fine; the CLI use-case is spec 14.

### 5.5 Why this is better than the previous "90-day" design

- **Time to first paint** drops from "tens of seconds to several minutes" to "~2 seconds" on heavy mailboxes — bounded by 50 × N folders, not by mailbox depth.
- **Predictable bandwidth**: the engine never grabs more than 50 envelopes per folder per tick (~25 KB/folder/tick at typical envelope sizes). On a slow connection the user still gets useful data quickly.
- **Schema reuse**: the `next_link` column was specced in spec 02 §3.7 from the start. We're using something that was always there.
- **Progressive disclosure**: identical to the UX modern mail clients use (Apple Mail, Outlook web). Newest-first; older mail trickles in automatically; explicit "load older" gesture is the escape hatch.
- **Fewer magic numbers**: no "90 days" appears in user-visible semantics. The user only sees "last 50."

## 6. Delta sync per folder

The core incremental loop. For each subscribed folder:

```go
func (e *engine) syncFolder(ctx context.Context, folderID string) error {
    token, err := e.store.GetDeltaToken(ctx, e.accountID, folderID)
    if err != nil { return err }

    // Cursor selection (§5.3):
    //   - non-empty next_link → mid-pagination resume; follow exactly one page
    //   - else non-empty delta_link → standard incremental
    //   - else first-launch → quick-start with $top=50
    var url string
    drainOnePageOnly := false
    switch {
    case token != nil && token.NextLink != "":
        url = token.NextLink
        drainOnePageOnly = true
    case token != nil && token.DeltaLink != "":
        url = token.DeltaLink
    default:
        url = fmt.Sprintf("/me/mailFolders/%s/messages/delta?$top=50", folderID)
        drainOnePageOnly = true
    }

    var added, updated, deleted int
    for {
        resp, err := e.graph.GetDelta(ctx, url, &graph.DeltaOpts{
            Select: envelopeSelect,
            MaxPageSize: 100,
        })
        if err != nil {
            if isSyncStateNotFound(err) {
                // Token expired or invalidated. Reset and re-init.
                if err := e.store.ClearDeltaToken(ctx, e.accountID, folderID); err != nil { return err }
                return e.syncFolder(ctx, folderID) // retry once with fresh init
            }
            return err
        }

        // Apply changes
        for _, item := range resp.Value {
            if item.Removed != nil {
                e.store.DeleteMessage(ctx, item.ID)
                deleted++
            } else {
                msg := convertGraphToStore(item)
                e.store.UpsertMessage(ctx, msg)
                if item.IsNew { added++ } else { updated++ }
            }
        }

        if resp.NextLink != "" {
            if drainOnePageOnly {
                // Quick-start or mid-pagination resume: persist the
                // nextLink and yield. The next sync tick continues
                // the drain.
                e.store.PutDeltaToken(ctx, store.DeltaToken{
                    AccountID:   e.accountID,
                    FolderID:    folderID,
                    NextLink:    resp.NextLink,
                    LastDeltaAt: time.Now().Unix(),
                })
                break
            }
            url = resp.NextLink
            continue
        }
        if resp.DeltaLink != "" {
            // Pagination drained. Persist the deltaLink and clear
            // any lingering next_link so subsequent ticks use the
            // standard incremental path.
            e.store.PutDeltaToken(ctx, store.DeltaToken{
                AccountID:   e.accountID,
                FolderID:    folderID,
                DeltaLink:   resp.DeltaLink,
                NextLink:    "",
                LastDeltaAt: time.Now().Unix(),
            })
            break
        }
        // Neither nextLink nor deltaLink? Anomalous.
        return errors.New("delta response missing both nextLink and deltaLink")
    }

    e.events <- FolderSyncedEvent{FolderID: folderID, Added: added, Updated: updated, Deleted: deleted, At: time.Now()}
    return nil
}
```

### 6.1 Detecting `syncStateNotFound`

Graph returns HTTP 410 Gone with error code `syncStateNotFound`. Implement detection in `internal/graph/errors.go`:

```go
func isSyncStateNotFound(err error) bool {
    var ge *GraphError
    if !errors.As(err, &ge) { return false }
    return ge.Code == "syncStateNotFound" || ge.Code == "SyncStateNotFound"
}
```

On `syncStateNotFound`: clear the delta token, trigger a fresh init. The user does not see anything other than a status-line note "Resyncing Inbox after token expiry."

### 6.2 Identifying additions vs updates

Graph's delta response does not directly distinguish "new" from "updated." Two approaches:

- **Optimistic:** if the local store has no row for `id`, treat as added; otherwise updated. This is correct except in the rare case where a message was inserted, deleted locally (e.g., an action failure), then we see it again.
- **Practical:** we don't actually need this distinction for correctness — only for the event count for UI feedback. Use the optimistic approach and accept that the count is approximate.

### 6.3 Conversation index updates

`conversationIndex` changes when a thread gets new replies. Delta will return updated entries with new indices. The store's `UpsertMessage` overwrites with the new value. Conversation grouping (UI concern) reads the latest indices.

## 7. Folder enumeration sync

Run on every sync cycle. Cheap: one paginated call to `GET /me/mailFolders?$top=100`, expecting <100 folders for typical users.

```go
func (e *engine) syncFolders(ctx context.Context) error {
    folders, err := e.graph.ListFolders(ctx)
    if err != nil { return err }

    seen := make(map[string]bool)
    for _, f := range folders {
        e.store.UpsertFolder(ctx, convertGraphFolder(f))
        seen[f.ID] = true
    }

    // Detect deleted folders
    existing, _ := e.store.ListFolders(ctx, e.accountID)
    for _, f := range existing {
        if !seen[f.ID] {
            e.store.DeleteFolder(ctx, f.ID) // cascades delete messages
        }
    }
    return nil
}
```

Folder sync runs BEFORE message delta sync. Reason: if a folder was renamed or moved, we want the up-to-date metadata before we render messages from it.

## 8. Action queue draining

When the engine enters the `draining actions` state:

```go
func (e *engine) drainActions(ctx context.Context) error {
    pending, err := e.store.PendingActions(ctx)
    if err != nil { return err }
    if len(pending) == 0 { return nil }

    // Group by type for batching efficiency
    byType := groupActionsByType(pending)

    for _, group := range byType {
        if err := e.executor.ExecuteGroup(ctx, group); err != nil {
            // Per-action failures are recorded inside ExecuteGroup; only catastrophic
            // errors (network, auth) reach here.
            return err
        }
    }
    return nil
}
```

The actual `ExecuteGroup` lives in `internal/action/executor.go` and is the subject of spec 09. The sync engine just calls it.

## 9. Polling cadence

Two tickers:

```go
type cadence struct {
    foreground time.Duration  // default 30s
    background time.Duration  // default 5min
}
```

- `SetActive(true)` sets ticker to `foreground` interval.
- `SetActive(false)` sets ticker to `background` interval.

Implementation: a single `time.Timer` reset to the appropriate duration after each cycle. Avoids the goroutine-leak pattern of having two tickers running simultaneously.

`Active` is determined by the UI layer:
- TUI sets `true` when started, `false` on terminal-suspend (Ctrl-Z) or window-blur (if detectable; macOS terminal apps generally cannot detect this reliably, so we may always be `true` in TUI mode).
- CLI mode: never starts a sync engine in long-running mode; CLI commands fire one-shot syncs as needed.

## 10. Throttling and retry

In `internal/graph/client.go`, the HTTP transport stack:

### 10.1 ThrottleTransport

```go
type throttleTransport struct {
    base     http.RoundTripper
    sem      chan struct{}  // capacity = MaxConcurrent (default 4)
    onThrottle func(time.Duration)  // notify engine for status line
}

func (t *throttleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    select {
    case t.sem <- struct{}{}:
        defer func() { <-t.sem }()
    case <-req.Context().Done():
        return nil, req.Context().Err()
    }

    for attempt := 0; attempt < maxRetries; attempt++ {
        resp, err := t.base.RoundTrip(req)
        if err != nil { return nil, err }

        if resp.StatusCode != 429 && resp.StatusCode != 503 {
            return resp, nil
        }

        retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
        if retryAfter == 0 {
            // Exponential backoff fallback
            retryAfter = time.Duration(1<<attempt) * time.Second
            if retryAfter > 30*time.Second { retryAfter = 30 * time.Second }
        }
        t.onThrottle(retryAfter)
        resp.Body.Close()

        select {
        case <-time.After(retryAfter):
        case <-req.Context().Done():
            return nil, req.Context().Err()
        }
    }
    return nil, errors.New("retry budget exhausted")
}
```

### 10.2 AuthTransport

```go
type authTransport struct {
    base http.RoundTripper
    auth auth.Authenticator
}

func (a *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    token, err := a.auth.Token(req.Context())
    if err != nil { return nil, err }
    req.Header.Set("Authorization", "Bearer " + token)

    resp, err := a.base.RoundTrip(req)
    if err != nil { return nil, err }

    if resp.StatusCode == 401 {
        // Token may have been revoked. Force refresh and retry once.
        resp.Body.Close()
        // Trigger a forced refresh; auth.Token internally handles this if the
        // cached token is recent. Need an explicit Invalidate() method on auth.
        a.auth.Invalidate()
        token, err = a.auth.Token(req.Context())
        if err != nil { return nil, err }
        req.Header.Set("Authorization", "Bearer " + token)
        return a.base.RoundTrip(req)
    }
    return resp, nil
}
```

(Note: this requires adding `Invalidate()` to the `auth.Authenticator` interface in spec 01. Add it as part of this implementation.)

### 10.3 Order of transports

```go
client := &http.Client{
    Transport: &authTransport{
        base: &throttleTransport{
            base: &loggingTransport{
                base: http.DefaultTransport,
            },
        },
    },
}
```

Auth is outermost (it can re-issue requests after refresh). Throttle is in the middle (it applies the semaphore and Retry-After waits). Logging is innermost (logs the actual outgoing request, including the bearer token, which the redaction layer scrubs).

## 11. Concurrency budget

The semaphore capacity is the single tunable for "how aggressive should sync be?":

- **Default:** 4. Conservative; matches historic per-mailbox limits.
- **Configurable** via `[sync].max_concurrent` in config.toml.
- **Never exceed:** Microsoft has historically used 4 as the soft limit on Outlook resources. Going higher risks getting throttled enough to slow things down rather than speed them up.

The semaphore covers ALL Graph traffic: backfill, delta, action drains, on-demand body fetches. They share the budget. This means a heavy backfill can slow down interactive operations; that's acceptable because backfill is bounded and infrequent. To improve interactive responsiveness, the engine prioritizes by:

- On-demand body fetches: highest priority (jump the queue).
- Action drains: high priority.
- Delta polls: normal priority.
- Backfill: lowest priority; yields to other traffic.

Implementation: instead of a plain semaphore, use a small priority queue feeding into the semaphore. See `internal/graph/scheduler.go` (introduce as a sub-component).

## 12. Reconciliation

When does local state diverge from Graph?

1. **User flagged a message offline.** Action queued. Drain on reconnect. Graph confirms. No divergence.
2. **User flagged a message; action failed (server-side error).** Local state says flagged; server says not. Reconciliation: next delta will tell us the server state. Apply server state. Surface a status-line warning ("1 action failed: flag message X — undo to re-apply").
3. **User flagged a message; action succeeded but we crashed before recording 'done'.** On restart, action has status `in_flight`. Re-running it is idempotent for flag/unflag/move/categorize (Graph PATCH is idempotent on these fields). Re-running soft-delete is also idempotent (deleting an already-deleted message returns 404, which we treat as success). Re-running permanent-delete: same — idempotent. So: on restart, we re-run all `in_flight` actions and treat 404s as success.
4. **A message was moved server-side (e.g., a user used Outlook for Mac to organize).** Delta will tell us the move. Local store updates `parent_folder_id`. UI re-renders.
5. **A message was deleted server-side.** Delta returns it with `@removed`. Local store deletes it.

The general rule: **the server is authoritative.** When in doubt, fetch from Graph and overwrite local. The only exception is the optimistic-action window: between user action and Graph confirmation, local is allowed to lead the server.

## 13. Telemetry / observability

Every sync cycle emits structured logs:

```json
{
    "level": "info",
    "msg": "sync complete",
    "duration_ms": 1450,
    "folders_synced": 12,
    "added": 7,
    "updated": 3,
    "deleted": 1,
    "actions_drained": 0,
    "throttled_count": 0
}
```

The status line in the TUI consumes the engine's notification channel and displays a one-line summary:

```
✓ Synced 12 folders · 7 new · 3 updated · 1 deleted · 1.4s
```

Or on error:

```
⚠ Sync failed (will retry in 30s): network unreachable
```

## 14. Failure modes

| Scenario                                  | Engine behavior                                                |
| ----------------------------------------- | -------------------------------------------------------------- |
| Network down                              | Tick fires, all calls fail with network error, log+emit `SyncFailedEvent`, schedule next tick at backoff (max 5 min). User-facing reads still work from cache. |
| Auth token revoked / refresh fails       | `AuthTransport` 401 path triggers re-auth. If `auth.Token()` then fails (refresh expired), engine emits `AuthRequiredEvent`. UI shows re-auth modal. Engine pauses ticks until auth restored. |
| Single folder fails delta                 | Log error, mark that folder `last_synced_at` unchanged, continue with other folders. Re-attempt next tick. |
| `syncStateNotFound`                      | Clear token, re-init that folder, continue.                    |
| Graph 5xx persistent                      | Retry with backoff. After 5 attempts in a single cycle, emit `SyncFailedEvent` and yield to next tick. |
| Engine crash mid-sync                     | On restart: actions in `in_flight` are re-run (idempotent). Folders without `deltaLink` get re-init. No data loss. |
| Disk full                                 | Store writes fail; engine logs ERROR and emits `SyncFailedEvent` with a specific "out of disk" reason. UI shows actionable message. |

## 15. Test plan

### Unit tests

- State machine transitions for all valid state×event combos.
- Delta response parsing: a fixture set of recorded Graph responses (success, syncStateNotFound, throttled, page nextLink, terminal deltaLink, removed items).
- Retry-After parsing (HTTP date format AND seconds-integer format both supported).
- Concurrency limiter: assert at most N requests in flight under load.

### Integration tests

- Spin up an `httptest.Server` mocking Graph. Drive the engine through full backfill → delta → action drain cycles. Assert correct DB state at each step.
- Simulate `syncStateNotFound` mid-session; assert clean recovery.
- Simulate 429 with Retry-After; assert respected.
- Simulate auth revocation (mock returning 401); assert AuthRequiredEvent emitted.

### End-to-end smoke test (manual, against real tenant)

1. Fresh DB; sign in; observe initial backfill of last 90 days; verify message count roughly matches mailbox.
2. Send self a test email externally; within 60 seconds, message appears in TUI.
3. Mark a message read in Outlook for Mac; within 60 seconds, marked read in TUI.
4. Move a message in Outlook for Mac to another folder; within 60 seconds, reflected in TUI.
5. Quit TUI; in Outlook for Mac, delete a message; restart TUI; observe message gone after first sync cycle.

## 16. Definition of done

- [ ] `internal/sync/` and `internal/graph/` packages compile and pass unit + integration tests.
- [ ] Initial backfill of a 5,000-message Inbox completes in <2 minutes on a typical broadband connection.
- [ ] Steady-state delta cycle (no changes) completes in <500ms total across 10 folders.
- [ ] All five end-to-end smoke tests pass on a real tenant.
- [ ] Throttling tested: artificially trigger 429s; verify backoff and recovery.
- [ ] Engine survives 24-hour unattended run with no goroutine leaks (verified via `runtime.NumGoroutine` snapshot at start and end).

## 17. Configuration

This spec owns the `[sync]` section. Full reference in `CONFIG.md`.

| Key | Default | Used in §  |
| --- | --- | --- |
| `sync.foreground_interval` | `"30s"` | §9 (cadence) |
| `sync.background_interval` | `"5m"` | §9 (cadence) |
| `sync.backfill_days` | `90` | §5 (initial backfill bound) |
| `sync.max_concurrent` | `4` | §11 (semaphore capacity) |
| `sync.max_retries` | `5` | §10.1 (throttle transport retry budget) |
| `sync.retry_max_backoff` | `"30s"` | §10.1 (backoff cap) |
| `sync.delta_page_size` | `100` | §6 (`Prefer` header) |
| `sync.subscribed_well_known` | `["inbox","sentitems","drafts","archive"]` | §5.1 |
| `sync.excluded_folders` | `["Deleted Items","Junk Email","Conversation History","Sync Issues"]` | §5.1 |
| `sync.prioritize_body_fetches` | `true` | §11 (priority queue) |

**Hard-coded:**
- The `$select` field list in §5.2. Adding a column to the cache requires a coordinated migration (spec 02) AND a sync engine update.
- The state machine ordering (drain actions → sync folders → idle). Changing this risks the divergence problem in §4.
- Idempotency assumptions in §12. They depend on Graph behavior, not user config.

The engine accepts a `*config.Config` at construction time and re-reads only `subscribed_well_known` and `excluded_folders` on each cycle (so a config-file edit + restart applies cleanly without a cache rebuild).

## 18. Out of scope for this spec

- The action executor itself (spec 09).
- Body fetching on demand (covered in spec 05, message rendering).
- Search-index population (handled by FTS5 triggers in spec 02; sync engine doesn't touch FTS directly).
- Webhook subscriptions (PRD §6: deferred indefinitely).
- Calendar sync (spec 12 will reuse the engine's machinery for a calendar delta loop).
