# Ralph-loop implementation plan — Specs 01 → 05

**Status:** Planned. Driven by the assistant in dynamic-pacing mode (`ScheduleWakeup`).
**Owner:** AI assistant (per CLAUDE.md §12).
**Order is sequential.** 01 must reach Done before 02 starts. 02 before 03, etc. ARCH §2 layering forbids parallelism here — every later spec depends on the lower layer being solid.

This file is the **outer loop**. Each spec gets its own tracking note at `docs/plans/spec-NN.md` (per CLAUDE.md §13) that the assistant maintains during that spec's loop.

---

## 0. Pre-flight (one-time, before spec 01)

Run once. Idempotent.

- [ ] `go mod init github.com/<owner>/inkwell` (placeholder owner, fix at rename).
- [ ] Add the locked dependencies from CLAUDE.md §1:
  - `github.com/charmbracelet/bubbletea bubbles lipgloss`
  - `github.com/charmbracelet/x/exp/teatest`
  - `github.com/AzureAD/microsoft-authentication-library-for-go`
  - `modernc.org/sqlite`
  - `github.com/zalando/go-keyring`
  - `github.com/jaytaylor/html2text`
  - `github.com/BurntSushi/toml`
  - `github.com/spf13/cobra`
  - `github.com/stretchr/testify`
- [ ] Skeleton package directories under `internal/` per ARCH §2 module layout (empty `doc.go` per package).
- [ ] `cmd/inkwell/main.go` skeleton wired to `cobra` root command, no subcommands yet.
- [ ] `internal/log/redact.go` skeleton + redaction unit tests (the redaction layer is required by every spec; build it first so logging-aware code can land cleanly).
- [ ] `internal/config/config.go` + `defaults.go` + `validate.go` skeletons. The first spec needs `[account]` config; later specs add `[cache]`, `[sync]`, `[bindings]`, `[ui]`, `[rendering]`.
- [ ] CI workflow at `.github/workflows/ci.yml` running:
  - `go vet ./...`
  - `go test -race ./...`
  - `go test -tags=integration ./...`
  - `go test -tags=e2e ./...`
  - `go test -bench=. -benchmem -run=^$ ./...` (informational, doesn't gate)
  - `staticcheck ./...`
- [ ] Commit at this point with conventional `chore: bootstrap module skeleton`.
- [ ] `go build ./...` succeeds on a clean clone.

Exit pre-flight when `go build ./...` and `go test -race ./...` are both green on an empty test base.

---

## Spec 01 — Authentication via Device Code Flow

**Source of truth:** `docs/specs/01-auth-device-code.md`.
**Tracking note:** `docs/plans/spec-01.md` (assistant creates on first iteration).

### Scope reminder
- MSAL Go device code flow.
- Keychain-backed cache via `cache.ExportReplace` adapter.
- Concurrent-safe `Authenticator.Token` with proactive refresh.
- `Invalidate()` method (added later by Spec 03 §10.2; declare it now to avoid churn).
- CLI: `inkwell signin`, `inkwell signout`, `inkwell whoami`.
- Privacy: tokens in Keychain only; no plaintext on disk; redaction tests for any code path that could log a token.

### DoD checklist (mirror spec §12)
- [ ] `internal/auth/` compiles, unit tests pass.
- [ ] `inkwell signin/signout/whoami` work end-to-end against a real tenant **(manual smoke; documented, not CI)**.
- [ ] Tokens persist across restarts (manual smoke).
- [ ] Silent refresh happens without UI prompt (manual smoke + log inspection).
- [ ] Re-auth triggers when refresh token is invalidated (manual smoke).
- [ ] Grep of `~/Library/Logs/inkwell/` returns no token-shaped strings.

### Loop slices (in suggested order)
1. **api** — define `auth.Authenticator` interface + `Config`, `PromptFn`, `DeviceCodePrompt` types per spec §4. Add `Invalidate()` per ARCH §5.1 / Spec 03 §10.2.
2. **wire** — wire MSAL `public.Client` construction with authority + scopes from §6.
3. **schema** — implement `keychainCache` adapter (`Replace` / `Export`) with mocked `keyring`.
4. **api** — implement `Token()` flow: `AcquireTokenSilent` → device code fallback. Mutex around refresh; in-memory token cache with 5-minute proactive window.
5. **api** — implement `SignOut()` clearing accounts + Keychain item.
6. **wire** — wire `cobra` subcommands `signin`, `signout`, `whoami`. The CLI's `PromptFn` writes to stderr in a grep-friendly format.
7. **redact** — log assertions: every error path that could surface MSAL detail goes through the redactor. Add a focused test that sets a token-shaped string and asserts the captured log line redacts it.
8. **test** — unit tests:
   - `keychainCache.Replace/Export` round-trip with mocked `keyring`.
   - Concurrent `Token()` serialises refresh (counter-asserted with a fake MSAL client).
   - Cached token returned when expiry is far.
   - Refresh triggered when within 5-minute window.
   - `Invalidate()` forces next `Token()` to reacquire.
9. **integration** — replay-style test with a fake MSAL client returning canned `AuthenticationResult` values; assert call sequences (silent → fallback → success → silent next call).
10. **polish** — CLI golden tests for `whoami` (signed-in / not-signed-in stdout) using `os.Pipe`.

### Test commands
```sh
go vet ./internal/auth/... ./cmd/inkwell/...
go test -race ./internal/auth/...
go test -tags=integration ./internal/auth/...
go test -race ./cmd/inkwell/...
```

### Exit criteria for the loop
All §12 DoD items ticked. Above commands green. Manual smoke documented in `docs/qa-checklist.md` under "Spec 01 — Auth."

### Privacy gates
- Redaction test asserting a synthetic bearer token does not appear in any captured slog output.
- No `t.Log`/`fmt.Println` of any MSAL `AuthenticationResult` field other than `Account.Username`.
- `keychainCache` writes happen via `keyring.Set` only; no `os.WriteFile` paths in `internal/auth`.

### Performance gates
None required by the spec. Assert that `Token()` returns in <1ms when cached (sanity bench, non-gating).

---

## Spec 02 — Local Cache Schema

**Source of truth:** `docs/specs/02-local-cache-schema.md`.
**Tracking note:** `docs/plans/spec-02.md`.

### Scope reminder
- SQLite via `modernc.org/sqlite`. WAL + the PRAGMAs from §2.
- Schema version 1 (every table + every index + FTS5 virtual table + triggers from §3).
- Embedded SQL migrations under `internal/store/migrations/`, applied transactionally.
- Public API `store.Store` per §5; sole owner of `mail.db`.
- Concurrency rules from §6 (`BEGIN IMMEDIATE`, batch-tx upserts, single mutex only at open).
- Performance budgets from §7 — every one has a benchmark.
- Body LRU eviction by both row count and byte size.

### DoD checklist (mirror spec §10)
- [ ] All tables, indexes, FTS triggers from §3 created by `001_initial.sql`.
- [ ] Public API in §5 implemented and tested.
- [ ] Every perf budget in §7 verified by benchmark.
- [ ] Coverage ≥ 80% on `internal/store`.
- [ ] Concurrent-access stress test passes (N=8 goroutines, mixed RW, 60s, no errors).
- [ ] DB file mode is 0600 on creation (integration test on macOS).

### Loop slices
1. **schema** — write `migrations/001_initial.sql` from spec §3. Verify every CREATE statement compiles with a temp DB.
2. **api** — `store.Open(path)`: opens file with `0600`, applies PRAGMAs, runs migrations. Single `sync.Mutex` only around the migration pass.
3. **api** — typed structs (`Account`, `Folder`, `Message`, `Body`, `Attachment`, `DeltaToken`, `Action`, `UndoEntry`, `SavedSearch`, `MessageQuery`, `SearchQuery`).
4. **api** — implement `MessageQuery`-driven `ListMessages` with the indexes in §3.4. Validate index usage via `EXPLAIN QUERY PLAN` in a test.
5. **api** — `UpsertMessagesBatch` wraps a single `BEGIN IMMEDIATE` tx.
6. **api** — bodies: `GetBody`, `PutBody`, `TouchBody`, `EvictBodies`. Eviction respects both caps; runs as a callable function (background goroutine wiring is the consumer's job).
7. **api** — actions (`EnqueueAction`, `PendingActions`, `UpdateActionStatus`) and undo stack (`PushUndo`, `PopUndo`, `PeekUndo`, `ClearUndo`). `ClearUndo` is called at app start.
8. **api** — saved searches CRUD.
9. **api** — `Search(SearchQuery)` over `messages_fts`.
10. **api** — `Vacuum`, `Close`, periodic-maintenance helpers.
11. **test** — unit per public method (round-trip insert → read → delete).
12. **test** — FTS triggers fire on insert/update/delete.
13. **test** — concurrent stress: N=8 goroutines, 60s, mix of upserts/reads/searches; assert no SQLite errors and final state consistent (deterministic seed).
14. **bench** — `internal/store/testfixtures.go` synthesises a 100k-message dataset deterministically. One `Benchmark*` per row of §7. Each fails the test if the budget is missed by >50%.
15. **integration** — open → close → reopen confirms persistence; mid-tx kill (subprocess-style, `os.Exit(1)` inside tx) confirms partial writes do not persist.

### Test commands
```sh
go vet ./internal/store/...
go test -race ./internal/store/...
go test -tags=integration ./internal/store/...
go test -bench=. -benchmem -run=^$ ./internal/store/...
go test -coverprofile=cover.out ./internal/store/... && go tool cover -func=cover.out | tail -5
```

### Exit criteria
- Coverage ≥ 80%.
- Stress test passes 60s without errors.
- Every benchmark from §7 within budget on dev machine. Budget numbers logged into the per-spec note.
- DB file mode `0600` asserted in integration test.

### Privacy gates
- DB path asserted to live under `~/Library/Application Support/inkwell/`.
- No plaintext mail body written outside the `bodies` table (asserted by code search test using `go/ast` looking for `os.WriteFile` in `internal/store` outside `Vacuum`).

### Performance gates
| Surface | Budget | Bench |
| --- | --- | --- |
| `GetMessage(id)` cached | <1ms p95 | `BenchmarkGetMessageCached` |
| `ListMessages(folder=Inbox, limit=100)` over 100k | <10ms p95 | `BenchmarkListMessagesInbox100kLimit100` |
| `UpsertMessage(single)` | <5ms p95 | `BenchmarkUpsertMessageSingle` |
| `UpsertMessagesBatch(100)` | <50ms p95 | `BenchmarkUpsertMessagesBatch100` |
| `Search(q="meeting", limit=50)` over 100k | <100ms p95 | `BenchmarkSearchMeeting100kLimit50` |
| App-start migration check on existing DB | <50ms | `BenchmarkOpenExistingDB` |
| Body fetch from `bodies` | <5ms | `BenchmarkGetBodyCached` |

---

## Spec 03 — Sync Engine

**Source of truth:** `docs/specs/03-sync-engine.md`.
**Tracking note:** `docs/plans/spec-03.md`.

### Scope reminder
- `internal/graph/` HTTP client first (transport stack: auth → throttle → logging).
- Then `internal/sync/`: state machine (idle → drain actions → sync folders → idle), tickers, backfill, delta loop, reconciliation.
- Concurrency budget = 4 (semaphore in throttle transport). Priority scheduler (body-fetch > action-drain > delta > backfill).
- 429 / Retry-After honoured. Exponential-backoff fallback up to 30s. Retry budget bounded.
- Crash safety: in-flight actions resumed on start; idempotent re-runs.
- `auth.Authenticator.Invalidate()` invoked on 401 then retry-once.

### DoD checklist (mirror spec §15 / §14 — the spec uses different DoD wording; assistant should re-read on iteration 1 to capture)
- [ ] Engine starts, ticks, drains, syncs, idles. State transitions covered by tests.
- [ ] First-launch backfill bounded to 90 days.
- [ ] Per-folder delta token persistence; resume on restart.
- [ ] `syncStateNotFound` triggers fresh init transparently.
- [ ] 429 handling honoured via `Retry-After`.
- [ ] Foreground 30s / background 5min cadence configurable.
- [ ] Notifications channel emits typed events for the UI.
- [ ] Action queue draining ordered before delta sync (reasoning in §4).

### Loop slices
1. **api** — `internal/graph/client.go` HTTP transport stack. Order: auth (outer) → throttle → logging (inner) per spec §10.3. Custom `RoundTripper`s with race-clean state.
2. **api** — `internal/graph/errors.go` parsing + classification (`isSyncStateNotFound`, `isThrottled`, `isAuthError`).
3. **api** — `internal/graph/messages.go` + `folders.go` + `delta.go` — typed wrappers around REST endpoints used by sync.
4. **api** — `internal/graph/scheduler.go` priority scheduler (priority queue feeding into the semaphore).
5. **api** — `internal/sync/engine.go` public `Engine` interface, `Event` types, `Start/Stop/SetActive/Sync/SyncAll/Backfill/ResetDelta/Notifications`.
6. **wire** — `internal/sync/folders.go` enumeration sync (folders before messages, see §7).
7. **wire** — `internal/sync/backfill.go` 90-day bounded initial pull.
8. **wire** — `internal/sync/delta.go` per-folder delta loop with nextLink pagination + deltaLink persistence + `syncStateNotFound` recovery.
9. **wire** — `internal/sync/reconcile.go` server-authoritative reconciliation rules from §12.
10. **wire** — `internal/sync/tick.go` single-`time.Timer` cadence.
11. **wire** — drain-actions hook (calls into spec 09's executor; for now a stub interface that the executor satisfies later).
12. **redact** — logging transport scrubs bearer tokens; structured per-cycle log line per §13. Test asserting a stub bearer never appears in captured slog.
13. **test** — unit:
    - Throttle transport respects `Retry-After` (numeric + HTTP-date).
    - Auth transport refreshes on 401 once, fails on second 401 in a row.
    - Concurrent `RoundTrip` calls obey semaphore capacity 4 (counter test).
    - Delta loop applies adds/updates/deletes correctly into a fake `store`.
    - `syncStateNotFound` triggers `ClearDeltaToken` and a fresh init.
    - Folder enumeration deletes folders no longer in Graph.
14. **integration** — `httptest.Server` replaying canned Graph responses from `internal/graph/testdata/`. Scenarios:
    - Cold start → backfill → first delta link persists.
    - Subsequent start → resume from delta link, picks up new messages.
    - 429 with `Retry-After: 2` → second attempt succeeds.
    - 410 `syncStateNotFound` → reset → re-init.
    - Long-running pagination (3 nextLinks then deltaLink).
15. **bench** — sustained delta apply rate target: ≥1000 envelopes/sec through the engine into a fresh store. Document the measured number; do not gate unless it regresses >50%.

### Test commands
```sh
go vet ./internal/graph/... ./internal/sync/...
go test -race ./internal/graph/... ./internal/sync/...
go test -tags=integration ./internal/graph/... ./internal/sync/...
go test -bench=. -benchmem -run=^$ ./internal/sync/...
```

### Exit criteria
All DoD ticked. Integration scenarios above all green. No live-tenant calls in CI. `internal/graph/testdata/` contains scrubbed fixtures only.

### Privacy gates
- Logging transport always present in stack; without it, the engine refuses to construct (constructor error).
- Test fixtures use `example.invalid` domain only; grep for real domains fails the redaction CI step.
- No message bodies fetched during delta sync (only envelope `$select`). Asserted by recording outgoing requests in integration test and inspecting the URL.

### Performance gates
- Cold delta sync of 1000 envelopes: <2s end-to-end on dev machine (recorded; informational).
- Action drain loop for 50 actions over batch executor stub: <1s.
- Single `tick` cycle when nothing changed: <50ms.

---

## Spec 04 — TUI Shell

**Source of truth:** `docs/specs/04-tui-shell.md`.
**Tracking note:** `docs/plans/spec-04.md`.

### Scope reminder
- Bubble Tea root `Model` + sub-models: folders / list / viewer (stub) / command / status / signin / confirm.
- Modes: Normal, Command, Search, SignIn, Confirm. Mode-dispatched root `Update`.
- Keymap with pane-scoped overrides. User-overridable via `[bindings]`.
- Three-pane layout with configurable widths. `WindowSizeMsg` re-layout.
- Sync events surface via the engine notification channel (consumed as a `tea.Cmd`).
- Sub-models are value types (no pointer aliasing across update cycles).
- No I/O in `Update`; everything via `tea.Cmd`.

### DoD checklist (assistant captures from spec on iteration 1)
- [ ] App launches, three panes render, status line shows account + sync state.
- [ ] Keybindings move focus, navigate folders, scroll list.
- [ ] `:` opens command mode; `q` quits in normal mode; `Ctrl+C` quits anywhere.
- [ ] `Ctrl+R` triggers `engine.SyncAll`; status line updates from event stream.
- [ ] Sign-in modal appears when `auth.Token` would block; transitions to main UI on success.
- [ ] Confirm modal appears for any registered destructive action (foundation, even if no actions wired yet).
- [ ] Resizes re-layout panes per `[ui]` widths.

### Loop slices
1. **schema** — `Model`, `PaneWidths`, `KeyMap`, `Theme`, `Mode`, `Pane` types in `internal/ui/`.
2. **wire** — root `Update` dispatching by `Mode` per spec §4. Sub-model updates take values, return values.
3. **wire** — folders pane: subscribes to `store.ListFolders`, renders, supports up/down + collapse/expand.
4. **wire** — list pane: subscribes to `store.ListMessages` for the focused folder.
5. **wire** — viewer pane: stub. Shows headers from selected message; body section says "Spec 05 will render this here." Rationale: prevents creep into spec 05.
6. **wire** — command bar (`:` prompt) with stubbed `:sync`, `:signin`, `:signout`, `:quit`.
7. **wire** — status line: account UPN, last sync timestamp, throttled banner when `ThrottledEvent` arrives.
8. **wire** — sign-in modal consuming `PromptFn` per spec 01 §9. Submit when MSAL completes (Cmd returns success).
9. **wire** — confirm modal for destructive action stub (used real in spec 07).
10. **wire** — pane-scoped key dispatch. Add tests that `r` does different things in list vs viewer (per CLAUDE.md §4).
11. **wire** — `[bindings]` config layer overrides. `[ui]` widths layer.
12. **test** — `teatest` e2e: scripted keystrokes (`j j j Enter`), assert rendered final frame contains expected line. Scenarios:
    - App boots → folders render → focus list with `2` → arrow down → status line shows selection count.
    - `:quit` exits.
    - `Ctrl+R` triggers `SyncAll` (assert via fake engine call counter).
    - Resize triggers re-layout (compare width slices).
13. **bench** — measure cold-start-to-first-frame: TUI program init → first `View()` < 500ms with a pre-populated store of 100k messages. Bench gates on this budget per PRD §7.

### Test commands
```sh
go vet ./internal/ui/...
go test -race ./internal/ui/...
go test -tags=e2e ./internal/ui/...
go test -bench=. -benchmem -run=^$ ./internal/ui/...
```

### Exit criteria
All DoD ticked. e2e scripts cover the eight scenarios above. Cold-start benchmark within budget.

### Privacy gates
- Status line never renders raw email addresses; addresses go through the redactor's display formatter (which keeps the user's own UPN visible but does not leak third-party addresses to logs).
- The Bubble Tea program's debug log (when `BUBBLETEA_LOG` set) is wired through the redaction handler.

### Performance gates
| Surface | Budget | Bench |
| --- | --- | --- |
| Cold start → first paint (100k cached) | <500ms | `BenchmarkColdStartFirstFrame` |
| Folder switch | <100ms | `BenchmarkFolderSwitch` |
| List pane scroll 100 messages | <100ms total | `BenchmarkListScrollPage` |

---

## Spec 05 — Message Rendering

**Source of truth:** `docs/specs/05-message-rendering.md`.
**Tracking note:** `docs/plans/spec-05.md`.

### Scope reminder
- `internal/render/` package. Stateless `Renderer` interface.
- Header rendering with focused/other styling, configurable full-headers toggle.
- Body fetch flow: cache hit → render; cache miss → placeholder + async `graph.GetMessageBody` → `BodyFetchedMsg` → re-render.
- HTML→text via `jaytaylor/html2text`. Plain text path normalises CRLFs and quoting. Browser fallback via `open(1)`.
- Attachment list. `:save <path>`, `:open` actions.
- Link extraction with numbered references; `o N` opens link N.
- Theming via lipgloss in `internal/render/theme.go`.

### DoD checklist (assistant captures from spec on iteration 1)
- [ ] Headers render with proper truncation (3 visible recipients + "(N more)").
- [ ] Body cache hit renders inline; miss shows "Loading…" then re-renders on completion.
- [ ] HTML body converted to readable text within budget.
- [ ] Plain-text body normalised (line breaks, quote depth via `>` markers).
- [ ] Attachments listed with size + content-type.
- [ ] Links extracted, numbered, openable via `o N`.
- [ ] `[rendering].show_full_headers` toggle works.
- [ ] No body content ever logged.

### Loop slices
1. **schema** — types: `Renderer`, `BodyOpts`, `BodyView`, `BodyState`, `ExtractedLink`, `Theme` per spec §3.
2. **api** — `render.New(store, graph, cfg) Renderer`. Stateless beyond holds.
3. **wire** — `headers.go` formatting: From/To/Cc/Date/Subject. Address-list truncation. Date formatting per `[ui].timezone` and `[ui].relative_dates_within`.
4. **wire** — `plain.go`: normalise EOLs, soft-wrap to viewer width, render quoted text dimmed.
5. **wire** — `html.go`: pipe through `html2text`; if empty/garbled, surface "Open in browser?" affordance with the `webLink`. Strip tracker pixels (`<img width=1 height=1>`) before conversion.
6. **wire** — `attachments.go`: list rendering, `:save <path>` writes to disk via `graph.GetAttachment`, `:open` writes to `os.TempDir()` under `0600` and shells out to `open`.
7. **wire** — `links.go`: extract from rendered text + raw HTML hrefs; numbered references; `o N` resolves.
8. **wire** — body fetch flow as `tea.Cmd`. `BodyFetchedMsg` reaches the viewer pane and triggers re-render.
9. **redact** — any error path (HTML conversion failure, fetch failure) goes through the redactor before logging.
10. **test** — unit:
    - Header formatter: empty Cc, single recipient, 3+ recipients, missing display name.
    - Plain text: CRLF, mixed line endings, quote depth, trailing whitespace.
    - HTML: golden-file conversions of representative messages stored under `internal/render/testdata/` (synthetic only).
    - Link extractor: numbered list deterministic; URL deduplication.
    - Attachments: byte size pretty-printed; inline vs non-inline.
11. **integration** — viewer pane in a `teatest` flow: open a message, body shows "Loading…", `BodyFetchedMsg` arrives (driven by a fake graph client), final frame contains the expected text.
12. **bench** — HTML→text conversion: 50KB HTML body < 30ms p95. Cold body open from cache < 5ms (re-uses `store.GetBody`).

### Test commands
```sh
go vet ./internal/render/...
go test -race ./internal/render/...
go test -tags=integration ./internal/render/...
go test -tags=e2e ./internal/ui/...   # viewer pane e2e
go test -bench=. -benchmem -run=^$ ./internal/render/...
```

### Exit criteria
All DoD ticked. Golden HTML conversions stable across runs. Bench within budget.

### Privacy gates
- All testdata HTML/text fixtures are synthetic, scrubbed; CI grep step asserts no real domains slip into `internal/render/testdata/`.
- The renderer never calls `slog.*` with body content. Lint test enforces this by `go/ast` walking `internal/render/` and failing on any `slog` call site that takes a `body` / `content` field.
- Temp files created by `:open` are mode `0600` and live under `os.TempDir()`. Asserted by integration test.

### Performance gates
| Surface | Budget | Bench |
| --- | --- | --- |
| HTML→text 50KB | <30ms p95 | `BenchmarkHTMLToText50KB` |
| Body open cache hit | <5ms p95 | covered by spec 02 bench, re-asserted here |
| Header render | <1ms p95 | `BenchmarkHeaders` |
| Link extract 200 links | <5ms p95 | `BenchmarkExtractLinks` |

---

## Cross-spec invariants the loop must re-check on every iteration

These are the things that are easiest to drift on. The self-critique phase (CLAUDE.md §12.2 step 5) must explicitly answer each one:

1. **Layering.** No `ui` import of `graph`. No package opens `mail.db` except `store`. No package talks to AAD/Keychain except `auth`.
2. **Optimistic UI.** Writes apply locally first, dispatch to Graph second, reconcile on response. No write path that hits Graph synchronously from `Update`.
3. **Idempotency.** Every action is safe to re-run. 404 on delete = success.
4. **Context propagation.** Every I/O function takes `ctx` as first arg. No `context.Background()` inside a request path.
5. **Privacy.** Tokens, bodies, addresses (outside the user's own UPN) never reach logs. Redaction tests cover every new log site.
6. **Performance.** Every spec budget has a benchmark; benchmark fails the test on >50% regression.
7. **Tests.** Unit + integration + (if relevant) e2e + bench all green before declaring DoD.
8. **Config.** Every new key in `docs/CONFIG.md` in the same change.
9. **Scope.** No reach for a Graph permission outside PRD §3.1.

If any answer is "drifted," the next iteration's slice fixes the drift before any new feature work.

---

## Inter-spec dependencies (do not violate)

```
01 (auth)  ──►  03 (sync, via auth.Authenticator.Token + Invalidate)
02 (store) ──►  03 (sync, via store.Store)
                 │
                 ▼
              04 (TUI shell, consumes engine + store + auth)
                 │
                 ▼
              05 (rendering, consumes store + graph + viewer pane scaffold from 04)
```

- Do not start spec 02 until spec 01's loop exits.
- Do not start spec 03 until both 01 and 02 are at Done. The graph client needs `auth.Authenticator`; the sync engine needs `store.Store`.
- Spec 04 introduces a viewer-pane stub that spec 05 fills in. The stub must exist before 05 begins, but the loop for 04 doesn't wait on 05.
- Spec 05's e2e tests live under `internal/ui/` (the viewer pane is an `ui` concern using the `render` package).

---

## When the assistant pauses

The assistant must stop and ask the user when:

1. A spec's wording is ambiguous in a way that materially changes design.
2. A perf budget is unattainable on the chosen approach (decide: relax budget or change approach).
3. A required Graph endpoint behaves unexpectedly vs the spec.
4. Anything would require a Graph scope outside PRD §3.1.
5. **Eight consecutive iterations without DoD progress** on the current spec (CLAUDE.md §12.1).

The pause is a regular text message to the user, not a `ScheduleWakeup`. The user resumes the loop manually.

---

## Sequence for the assistant

1. Run **Pre-flight** (§0) once, commit, smoke-test `go build ./...` and `go test -race ./...`.
2. Open `docs/plans/spec-01.md` (assistant creates from the template in CLAUDE.md §13). Begin spec-01 ralph loop until exit criteria fire.
3. Open PR for spec 01. Wait for user merge if required.
4. Repeat for specs 02, 03, 04, 05 in order.
5. After spec 05 lands, this outer plan is complete. Subsequent specs (06–14) get their own plan documents.
