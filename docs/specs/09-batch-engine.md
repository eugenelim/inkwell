# Spec 09 — $batch Execution Engine

**Status:** In progress (CI scope, v0.6.x). Synchronous chunked $batch shipped: `internal/graph/batch.go::ExecuteBatch` + `internal/action/batch.go` chunks at MaxBatchSize=20 and serially executes. Residual: per-sub-request 429 retry loop, concurrent batch fan-out at `[batch].batch_concurrency`, composite undo entries, BulkActionCompletedEvent over the engine notification channel, soft cap at 5,000 messages — all tracked under audit-drain queue.
**Depends on:** Specs 03 (graph client transport stack), 07 (action types).
**Blocks:** Spec 10 (bulk operations consume the batch executor).
**Estimated effort:** 2 days.

---

## 1. Goal

Build the engine that takes a list of actions affecting many messages, packs them into Graph `$batch` HTTP requests, runs them with bounded parallelism, handles partial failures, and reports per-action results back to the caller. This is what makes "delete 247 messages in 5 seconds" possible.

This spec is purely the **execution layer**. The decision about *what to do* (what messages, what action type) comes from spec 10 (bulk ops UX) and spec 08 (pattern selection).

## 2. Module layout

```
internal/graph/
├── batch.go            # Builder, Executor, Response types
├── batch_chunk.go      # Chunking long action lists into 20-per-batch groups
└── batch_retry.go      # Per-sub-request 429 / retry handling

internal/action/
└── executor.go         # Already exists from spec 07; extended here for ExecuteBulk()
```

## 3. Why a dedicated batch engine

A naive bulk operation would loop and call `PATCH /me/messages/{id}` 247 times. That:

- Burns 247 round-trips, each ~150ms, ~37 seconds total.
- Hits the 4-concurrent-Outlook-resource limit immediately.
- Spams the throttle counters.

With $batch:

- 247 actions ÷ 20 per batch = 13 batches.
- 13 batches × 3 in flight = ~5 round-trips at network level.
- Each round-trip is ~300ms (slightly slower than single because of payload size).
- Total: ~5 seconds.

The 7x improvement is real and matters for UX.

## 4. Public API

```go
package graph

type BatchBuilder struct {
    requests []SubRequest
}

type SubRequest struct {
    ID      string                 // local correlation ID, becomes the request id in $batch
    Method  string                 // GET, POST, PATCH, DELETE
    URL     string                 // Graph-relative URL, e.g., "/me/messages/{id}/move"
    Headers map[string]string
    Body    any                    // marshaled to JSON
}

func NewBatch() *BatchBuilder

func (b *BatchBuilder) Add(req SubRequest) *BatchBuilder

func (b *BatchBuilder) Len() int

// Build returns the JSON payload for POST /v1.0/$batch.
// Limits to 20 per spec; callers should chunk before calling.
func (b *BatchBuilder) Build() ([]byte, error)
```

```go
type BatchExecutor interface {
    // Execute runs a single $batch HTTP call. Returns per-sub-request results
    // in the same order as input requests (correlated via SubRequest.ID).
    Execute(ctx context.Context, reqs []SubRequest) ([]SubResponse, error)

    // ExecuteAll splits a long request list into 20-per-batch chunks, runs
    // them with bounded concurrency, and returns all results aggregated.
    // Per-sub-request 429s are retried internally up to retry budget.
    ExecuteAll(ctx context.Context, reqs []SubRequest, opts ExecuteAllOpts) (*ExecuteAllResult, error)
}

type ExecuteAllOpts struct {
    Concurrency int                          // default from [batch].batch_concurrency
    OnProgress  func(done, total int)        // called after each batch completes
    OnRetry     func(req SubRequest, attempt int, retryAfter time.Duration)
}

type SubResponse struct {
    ID         string                       // correlation
    Status     int                          // HTTP status
    Headers    map[string]string
    Body       json.RawMessage              // unparsed; caller decodes
    GraphError *GraphError                  // populated for status >= 400
}

type ExecuteAllResult struct {
    Results       []SubResponse              // in input order
    SuccessCount  int
    FailureCount  int
    RetryCount    int
    DurationMS    int64
}

func NewBatchExecutor(client *Client, cfg *config.Config) BatchExecutor
```

## 5. Building a batch payload

The Graph `$batch` request body shape:

```json
{
  "requests": [
    {
      "id": "1",
      "method": "POST",
      "url": "/me/messages/AAMkADk.../move",
      "headers": { "Content-Type": "application/json" },
      "body": { "destinationId": "deleteditems" }
    },
    {
      "id": "2",
      "method": "PATCH",
      "url": "/me/messages/AAMkADk.../",
      "headers": { "Content-Type": "application/json" },
      "body": { "isRead": true }
    }
    // up to 20 entries
  ]
}
```

Notes:
- `id` must be unique within the batch. We use stringified UUIDs internally; the user-facing correlation is by `SubRequest.ID`.
- URLs are Graph-relative (no scheme, no `https://graph.microsoft.com/v1.0`).
- Body objects MUST include `Content-Type: application/json` in headers when sending JSON. Forgetting this is a common bug; the builder enforces it.
- Methods: only POST, PATCH, GET, DELETE in our use cases.
- We do NOT use the `dependsOn` field. v1 has no dependent sub-requests; if we need ordering, the action queue level handles it (e.g., move-then-PATCH must run as two sequential actions, not within one batch).

### 5.1 Validation

`Build` rejects:
- More than 20 sub-requests.
- Duplicate IDs.
- Empty URL or method.
- POST/PATCH without body (or without Content-Type set).

These are programmer errors, surfaced as panics in development and errors in release builds.

## 6. Chunking

A bulk operation might involve 500 actions. We chunk into 20-per-batch:

```go
func chunkRequests(reqs []SubRequest, size int) [][]SubRequest {
    var chunks [][]SubRequest
    for i := 0; i < len(reqs); i += size {
        end := i + size
        if end > len(reqs) { end = len(reqs) }
        chunks = append(chunks, reqs[i:end])
    }
    return chunks
}
```

Chunk size from `[batch].max_per_batch` (default 20, max 20). Code path supports lower for testing.

## 7. ExecuteAll: orchestration

```go
func (e *batchExecutor) ExecuteAll(ctx context.Context, reqs []SubRequest, opts ExecuteAllOpts) (*ExecuteAllResult, error) {
    chunks := chunkRequests(reqs, e.cfg.MaxPerBatch)

    concurrency := opts.Concurrency
    if concurrency == 0 { concurrency = e.cfg.BatchConcurrency }

    sem := make(chan struct{}, concurrency)
    results := make([]SubResponse, len(reqs))
    var mu sync.Mutex
    var wg sync.WaitGroup

    var firstErr error
    var errOnce sync.Once

    started := time.Now()

    for chunkIdx, chunk := range chunks {
        select {
        case sem <- struct{}{}:
        case <-ctx.Done():
            return nil, ctx.Err()
        }

        wg.Add(1)
        go func(chunkIdx int, chunk []SubRequest) {
            defer wg.Done()
            defer func() { <-sem }()

            chunkResults, err := e.executeChunkWithRetry(ctx, chunk, opts.OnRetry)
            if err != nil {
                errOnce.Do(func() { firstErr = err })
                return
            }

            // Place results back into the canonical order.
            mu.Lock()
            baseIdx := chunkIdx * e.cfg.MaxPerBatch
            for i, r := range chunkResults {
                results[baseIdx + i] = r
            }
            mu.Unlock()

            if opts.OnProgress != nil {
                completed := atomic.AddInt32(&e.completed, 1)
                opts.OnProgress(int(completed) * e.cfg.MaxPerBatch, len(reqs))
            }
        }(chunkIdx, chunk)
    }

    wg.Wait()
    if firstErr != nil { return nil, firstErr }

    res := &ExecuteAllResult{Results: results, DurationMS: time.Since(started).Milliseconds()}
    for _, r := range results {
        if r.Status >= 200 && r.Status < 300 {
            res.SuccessCount++
        } else {
            res.FailureCount++
        }
    }
    return res, nil
}
```

The semaphore caps concurrency at `[batch].batch_concurrency` (default 3). Combined with `[batch].max_per_batch=20`, that's effectively 60 sub-requests in flight at peak — but only 3 actual HTTP connections, well within the 4-concurrent-Outlook-resource limit.

Note: the batch HTTP calls themselves still go through the throttle transport (spec 03 §10.1), so they share the global semaphore (`[sync].max_concurrent`). Batch concurrency × 1 batch HTTP call each = up to 3 of the 4 concurrent slots. The remaining slot is reserved for delta sync, on-demand body fetch, and other non-batch traffic.

## 8. Per-chunk execution with sub-request retry

```go
func (e *batchExecutor) executeChunkWithRetry(ctx context.Context, chunk []SubRequest, onRetry func(SubRequest, int, time.Duration)) ([]SubResponse, error) {
    pending := chunk
    results := make(map[string]SubResponse)

    for attempt := 0; attempt < e.cfg.MaxRetries; attempt++ {
        if len(pending) == 0 { break }

        responses, err := e.executeChunk(ctx, pending)
        if err != nil {
            // Whole-batch error (network, auth). Bubble up.
            return nil, err
        }

        var stillPending []SubRequest
        var maxRetryAfter time.Duration

        for i, r := range responses {
            req := pending[i]
            if r.Status == 429 {
                ra := parseRetryAfter(r.Headers["Retry-After"])
                if ra > maxRetryAfter { maxRetryAfter = ra }
                stillPending = append(stillPending, req)
                if onRetry != nil { onRetry(req, attempt+1, ra) }
            } else {
                results[req.ID] = r
            }
        }

        if len(stillPending) == 0 { break }

        // Wait the longest retry-after before re-issuing the throttled subset
        if maxRetryAfter == 0 { maxRetryAfter = time.Duration(1<<attempt) * time.Second }
        select {
        case <-time.After(maxRetryAfter):
        case <-ctx.Done():
            return nil, ctx.Err()
        }

        pending = stillPending
    }

    // Anything still pending after retry budget exhausted is recorded as 429
    for _, req := range pending {
        results[req.ID] = SubResponse{ID: req.ID, Status: 429, GraphError: &GraphError{Code: "throttled", Message: "retry budget exhausted"}}
    }

    // Order results to match input
    ordered := make([]SubResponse, len(chunk))
    for i, req := range chunk {
        ordered[i] = results[req.ID]
    }
    return ordered, nil
}
```

Key behaviors:
- The whole batch HTTP call returns 200 even when sub-requests fail. We don't retry the whole batch on 200.
- We retry **only the 429'd sub-requests** in a fresh batch. Microsoft's docs are explicit: SDKs that retry batches automatically don't retry sub-requests; we do this manually.
- We wait the **longest Retry-After** across the throttled sub-requests, then re-issue. Waiting per-sub-request would defeat batching.
- We do NOT retry on 5xx within a sub-request — those are application-level errors that the action layer (spec 07) handles via its classification.

## 9. Parsing the $batch response

The response shape:

```json
{
  "responses": [
    {
      "id": "1",
      "status": 200,
      "headers": { "Cache-Control": "private", ... },
      "body": { "@odata.context": "...", ... }
    },
    {
      "id": "2",
      "status": 429,
      "headers": { "Retry-After": "10" },
      "body": { "error": { "code": "TooManyRequests", "message": "..." } }
    }
  ]
}
```

Parser:

```go
type batchResponse struct {
    Responses []rawSubResponse `json:"responses"`
}

type rawSubResponse struct {
    ID      string            `json:"id"`
    Status  int               `json:"status"`
    Headers map[string]string `json:"headers"`
    Body    json.RawMessage   `json:"body"`
}
```

We pass `body` through as `json.RawMessage` so callers can decode the appropriate payload type (move endpoints return the moved message; PATCH endpoints return the updated message; DELETE returns 204 with no body).

For status >= 400, we attempt to decode the standard Graph error envelope:

```go
if r.Status >= 400 {
    var ee struct {
        Error struct {
            Code    string `json:"code"`
            Message string `json:"message"`
        } `json:"error"`
    }
    if err := json.Unmarshal(r.Body, &ee); err == nil && ee.Error.Code != "" {
        sub.GraphError = &GraphError{Code: ee.Error.Code, Message: ee.Error.Message}
    }
}
```

## 10. Action executor's bulk path (extending spec 07)

`Executor.ExecuteBulk(ctx, actions)` is the entry point bulk operations call. It:

1. Validates all actions are of the SAME type. (Mixed-type bulk is rejected; spec 10 ensures uniformity.)
2. For each action, applies optimistic local state + pushes a single composite undo entry covering all actions.
3. Calls `e.actionToSubRequest(action)` for each action to translate to a `SubRequest`.
4. Invokes `BatchExecutor.ExecuteAll`.
5. Walks the per-sub-request results:
   - On success: action `Done`.
   - On 404: treated as success (idempotency).
   - On 4xx (non-404): action `Failed`, rollback that specific message.
   - On 429-after-retry-exhausted: action stays `Pending`, will retry on next sync engine drain.
6. Emits a single `BulkActionCompletedEvent` summarizing the result:
   ```go
   type BulkActionCompletedEvent struct {
       BulkID      string
       Type        ActionType
       Total       int
       Succeeded   int
       Failed      int
       Pending     int   // throttled, will retry
       DurationMS  int64
   }
   ```

### 10.1 Composite undo

A single bulk operation pushes ONE undo entry that covers all affected messages:

```go
type BulkUndoEntry struct {
    Label      string             // "Move 247 messages back to Inbox"
    Type       ActionType
    MessageIDs []string
    Params     map[string]any
}
```

`Undo` for a bulk entry runs another `ExecuteBulk` with the inverse action over the same messages.

If the original bulk had partial failures (some messages succeeded, some didn't), the undo only covers the successful ones. Failed messages weren't actually changed; nothing to undo.

### 10.2 Translating actions to SubRequests

For each action type, the translator emits a sub-request matching the single-message endpoint Graph would use:

| Action | SubRequest |
| --- | --- |
| `mark_read` | `PATCH /me/messages/{id}` body `{"isRead":true}` |
| `mark_unread` | `PATCH /me/messages/{id}` body `{"isRead":false}` |
| `flag` | `PATCH /me/messages/{id}` body `{"flag":{"flagStatus":"flagged"}}` |
| `unflag` | `PATCH /me/messages/{id}` body `{"flag":{"flagStatus":"notFlagged"}}` |
| `move`, `archive`, `soft_delete` | `POST /me/messages/{id}/move` body `{"destinationId":...}` |
| `permanent_delete` | `POST /me/messages/{id}/permanentDelete` (no body) |
| `add_category`, `remove_category` | `PATCH /me/messages/{id}` body `{"categories":[...]}` |

`add_category`/`remove_category` are special: each message has its own current category list. The batch builder, before issuing, snapshots each message's current categories from the local store and constructs the PATCH body per-message. This is one of the few cases where a bulk operation has different bodies per sub-request, but the framework handles it cleanly.

## 11. Throughput projections

For a typical user-facing bulk:

| Bulk size | Chunks | Concurrency | Estimated wall-clock |
| --- | --- | --- | --- |
| 20 messages | 1 | 1 | 0.5s |
| 100 messages | 5 | 3 | 1.5s |
| 247 messages | 13 | 3 | ~5s |
| 1,000 messages | 50 | 3 | ~17s |
| 5,000 messages | 250 | 3 | ~85s |

Beyond ~1,000 messages, the wall-clock time becomes user-noticeable. Spec 10 imposes a soft cap of 5,000 per single bulk operation, with a confirmation that explains the time estimate. Beyond 5,000, the user is told to refine their pattern or run multiple smaller bulks.

## 12. Configuration

This spec owns the `[batch]` section. Full reference in `CONFIG.md`.

| Key | Default | Used in § |
| --- | --- | --- |
| `batch.max_per_batch` | `20` | §6 (chunk size) |
| `batch.batch_concurrency` | `3` | §7 (semaphore) |
| `batch.batch_request_timeout` | `"60s"` | §8 (per-batch HTTP timeout) |
| `batch.dry_run_default` | `false` | spec 10 (UX); included here so spec 09 doesn't have to re-ask |

New keys this spec adds:

| Key | Default | Used in § |
| --- | --- | --- |
| `batch.max_retries_per_subrequest` | `5` | §8 |
| `batch.bulk_size_warn_threshold` | `1000` | §11 (warn user when bulk exceeds) |
| `batch.bulk_size_hard_max` | `5000` | §11 (refuse beyond) |

## 13. Failure modes

| Scenario | Behavior |
| --- | --- |
| Whole-batch HTTP fails (network) | `ExecuteAll` returns error; spec 10 treats as "all actions still pending"; sync engine drains on next tick. |
| Whole-batch returns 401 | Auth transport refreshes and retries the whole batch automatically. |
| Whole-batch returns 429 | Throttle transport waits Retry-After and retries the whole batch. |
| Sub-request returns 429 | Per-sub-request retry loop (§8). |
| Sub-request returns 404 | Treated as success (action layer interprets). |
| Sub-request returns 5xx | Reported as failure to action layer; that layer decides retry. |
| Sub-request body malformed | Reported as failure with code `parseError`. |
| Bulk exceeds `bulk_size_hard_max` | `ExecuteAll` refuses with `ErrBulkTooLarge`; spec 10 surfaces to user. |
| Concurrent ExecuteAll calls (multiple bulk ops queued) | Each holds its own semaphore portion; total in-flight respects `[sync].max_concurrent`. |

## 14. Test plan

### Unit tests

- `BatchBuilder.Build` produces correct JSON for various sub-request mixes.
- `BatchBuilder.Build` rejects >20 entries, duplicate IDs, missing fields.
- `chunkRequests` partitions correctly for various inputs.
- Per-sub-request retry: feed a mock that returns 429 for some IDs; assert correct re-issue behavior.
- Result ordering: results align to input order regardless of internal concurrency.

### Integration tests

- `httptest.Server` simulating Graph $batch endpoint. Test cases:
  - All success (20/20).
  - Half success, half 429 with Retry-After (assert re-issue and eventual success).
  - 429 budget exhaustion (assert reported as still-throttled).
  - Mix of 200, 404, 403, 429.
  - Very large bulk (200+ messages); assert correct chunking and ordering.
- Concurrent bulks: kick off two `ExecuteAll`s simultaneously; assert both complete correctly without interference.

### Performance benchmark

- Synthesized 1,000-message bulk against mock server with realistic latencies (200ms per batch). Assert wall-clock <20s.

## 15. Definition of done

- [ ] `internal/graph/batch.go` and `batch_chunk.go` and `batch_retry.go` implemented and tested.
- [ ] `Executor.ExecuteBulk` in `internal/action/executor.go` calls into `BatchExecutor.ExecuteAll`.
- [ ] Composite undo entry pushed for bulk operations; undo executes inverse bulk.
- [ ] Per-sub-request 429 retry verified against mock.
- [ ] 1000-message bulk completes within budget against mock with realistic latencies.
- [ ] Result ordering verified for batches of varying sizes.
- [ ] Real-tenant smoke: 50-message mark_read bulk completes in <2s; 200-message move completes in <5s.

## 16. Out of scope for this spec

- The pattern selection layer (spec 08).
- The bulk operations UX: filter mode, `;` prefix, dry-run, confirmation modals (spec 10).
- Inter-action dependencies (`dependsOn`). Not used in v1.
- Bulk-fetch GET batches (e.g., grabbing 100 message bodies in one batch). Possible future optimization; not in v1.
- Mixed-action-type bulks. v1 requires uniformity.
