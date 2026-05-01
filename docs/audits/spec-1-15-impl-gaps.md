# Spec 1-15 implementation/design gap audit

Date: 2026-04-29

Scope: implementation and design gaps in `internal/` and `cmd/inkwell/`. Test gaps audited separately. File:line references are absolute.

---

## Spec 01 — Authentication (interactive browser + device code)

- Implementation: `internal/auth/`
- Status overall: partial — sign-in flows ship; AADSTS classification + clock-skew detection + CLI PromptFn missing.
- Implementation gaps:
  - DoD bullet "`inkwell whoami` works end-to-end" — `cmd/inkwell/cmd_root.go:38` registers `newWhoamiCmd(rc)` but no `cmd_whoami.go` file exists in `cmd/inkwell/`. The root command also references `newSignoutCmd(rc)` (`cmd_root.go:38`) with no corresponding file. The runners are presumably in `cmd_auth_runners.go` but the spec's three-command surface (`signin`/`signout`/`whoami`) has not been verified to compile / run end-to-end.
  - Spec §11 lists "Conditional Access requires a compliant / managed device" with a guarded user-facing message. `internal/auth/auth.go:296,300` only wraps MSAL errors with `fmt.Errorf("interactive auth: %w", ...)`; there is no AADSTS code classification (`AADSTS530003`, `AADSTS65001`, etc.) and no friendly message rewriting. The spec table promises specific error text per scenario; the code passes the raw MSAL string through.
  - Spec §11 row "Clock skew > 5 minutes" — no detection or special surface. Clock-skew failures bubble up as MSAL token-validation errors with no actionable hint.
- Design drifts:
  - Spec §4 declares the public type `DeviceCodePrompt` carries `Message string`. `internal/auth/auth.go:138` includes the field but `noopPrompt` (`auth.go:465`) is the only registered prompt for non-TUI flows. There is no CLI implementation of `PromptFn` that prints to stderr per spec §5.4 ("The CLI's `PromptFn` should print to stderr…"). For CLI sign-in, no device-code text would surface.
- Schema/config gaps: none.
- TODO-shaped spec language: spec §6 line 269 — "The `Chat.Read` and `User.ReadBasic.All` scopes are deferred (not in v1 surface area)." (Acceptable — explicit deferral.)

---

## Spec 02 — Local Cache Schema

- Implementation: `internal/store/`
- Status overall: partial (most surfaces present; maintenance + a few methods missing)
- Implementation gaps:
  - ~~Maintenance job not implemented~~ **Closed by PR 11 (v0.13.x).** New `internal/sync/maintenance.go` runs in its own goroutine off the engine's main timer. Each pass: EvictBodies (config caps), SweepDoneActions (config retention), optional Vacuum (off by default). Negative MaintenanceInterval is the test-only disable sentinel.
  - `internal/store/saved_searches.go` has no `delete-by-name` helper despite `Manager.Delete` consuming an ID per spec 11. Existing `DeleteSavedSearch(id)` is correct; flagging because spec 11 §3 wants name-based lookup which requires another method spec 11 doesn't get either way.
- Design drifts:
  - ~~`EvictBodies` signature drift; eviction code dead at runtime~~ **Closed by PR 11 (v0.13.x).** The new `internal/sync/maintenance.go` periodically calls `EvictBodies` with the configured `BodyCacheMaxCount` / `BodyCacheMaxBytes` caps. The signature stays as-is (caller passes caps explicitly); production wiring now lives.
- Schema/config gaps:
  - `flag_due_at` and `flag_completed_at` columns exist in `001_initial.sql:54-55` but spec 07's `flag` action with `due_date` parameter (§3 / §6.3) writes the param and never persists it. `MessageFields` (`store/types.go:206`) has no `FlagDueAt` field, so flag-with-due is structurally impossible.
- TODO-shaped spec language: none.

---

## Spec 03 — Sync Engine

- Implementation: `internal/sync/` and `internal/graph/`
- Status overall: partial — diverged from delta-driven design
- Implementation gaps:
  - DoD bullet "Initial backfill of a 5,000-message Inbox completes in <2 minutes" — there is no tombstone-aware delta path during backfill. `sync/delta.go:25-40` documents that quickStart and pullSince do **not** receive `@removed` markers, so server-side deletions/moves never propagate. `followDeltaPage` exists (`delta.go:131`) but is unreachable from a fresh install (`syncFolder` always picks quickStart for new folders, `delta.go:54`).
  - ~~Spec §3 declares `Engine.Notifications()` emits a `ThrottledEvent`~~ **Closed by PR 3 (v0.13.x).** `Engine.OnThrottle(d)` is now part of the interface; `cmd_run.go` wires graph.Options.OnThrottle as a closure that forwards into the engine; the engine emits ThrottledEvent. Verified by integration test `TestEngineGraphClientIntegrationEmitsThrottle`.
  - DoD bullet "Engine survives 24-hour unattended run with no goroutine leaks" — the panic recovery is in `engine.go:241` but `consumeSyncEventsCmd` (`ui/app.go:1351`) reads `<-m.deps.Engine.Notifications()` without a Done/cancel signal. On engine `Stop`, the events channel never closes, so the UI goroutine blocks forever.
  - Spec §3 lists `ResetDelta(ctx, folderID string)` and `Backfill(ctx, folderID, until)`. Both are implemented. ~~`AuthRequiredEvent` is never emitted~~ **Closed by PR 3 (v0.13.x).** `engine.emitCycleFailure(err)` classifies via `graph.IsAuth` and emits AuthRequiredEvent on auth-shaped errors; the loop's two cycle-error sites route through it. Verified by `TestEngineEmitsAuthRequiredOn401`.
- Design drifts:
  - Spec §6 ("Delta sync per folder") says first-launch goes through `/me/mailFolders/{id}/messages/delta?$top=50`. Implementation chose `/messages?$top=50&$orderby=receivedDateTime desc` non-delta endpoint instead (`delta.go:46-56`), with explicit doc comments explaining why (Graph delta doesn't honour `$orderby`). This is a documented deviation: spec §5.2 says "Why not `/messages/delta`?" and revises to non-delta. The code matches the revised intent. **However spec §6.2 still describes "Identifying additions vs updates" in terms of delta tombstones** — that section never triggers in production because `pullSince` and `quickStart` don't see tombstones (`delta.go:40-41`). The spec text and code are out of phase by one revision.
  - Spec §11 promises a "small priority queue feeding into the semaphore" so on-demand body fetches jump the queue. `graph/client.go:177` is a plain semaphore — no priority queue, no `internal/graph/scheduler.go`. Body fetches share fairly with backfill traffic.
  - Spec §10.2 requires `auth.Authenticator.Invalidate()` — present at `auth/auth.go:415`. OK.
- Schema/config gaps:
  - `[sync].subscribed_well_known` and `[sync].excluded_folders` from spec §17 are absent in `internal/config/defaults.go:24-30`. The engine hardcodes them at `engine.go:148-158`. Config keys `delta_page_size`, `retry_max_backoff`, `prioritize_body_fetches` are also missing.
- TODO-shaped spec language:
  - Spec §5.5 / §5.2 contains "A future iter can add a background 'drain delta to seed the cursor' pass for full incremental sync." — present in `delta.go:38-40`, an explicit "future iter" hedge.

---

## Spec 04 — TUI Shell

- Implementation: `internal/ui/`
- Status overall: partial
- Implementation gaps:
  - ~~DoD bullet "Help screen lists all bindings"~~ **Closed by PR 2 (v0.13.x).** New `internal/ui/help.go` renders a full `HelpModel` overlay grouped by section (Pane focus / Triage / Filter / Modes); `?` keybind + `:help` / `:?` command both open it. e2e visible-delta verifies all four section headers paint.
  - DoD bullet "`:quit`, `:q`, `Ctrl+C`, `q` all exit cleanly (engine stop, store close, no goroutine leaks)" — `dispatchCommand` quit (`app.go:817`) returns `tea.Quit` directly without calling `engine.Stop` or `store.Close`. Lifecycle teardown happens (presumably) in `cmd_run.go` but the spec wants the UI exit path to be the single shutdown gate.
  - Spec §13 minimum terminal: 80×24, with "terminal too small" message below. `relayout` (`app.go:1401`) clamps but never refuses to render.
  - Spec §6.5 / §17 `ui.transient_status_ttl` (default 5s) — not in defaults (`config/defaults.go:32-37`). Transient status messages are set but never auto-clear with a TTL goroutine.
- Design drifts:
  - Spec §5 keymap declares `MarkRead/MarkUnread` and pane scoping rules. `keys.go:85-86` implements them. But spec 07 §12 promises pane-scoped meaning for `f` (list = flag, viewer = forward) and `r` (list = read, viewer = reply). The viewer `r` is wired (`app.go:1287-1295`), but `f` in the viewer fires `ToggleFlag` (`app.go:1266`) — there is no Forward action wired anywhere.
  - ~~Spec §6.4 lists 13 commands in the dispatcher (5 missing).~~ **Mostly closed.** `:help` / `:?` shipped in PR 2 (v0.13.x); `:refresh` / `:folder` / `:open` / `:backfill` / `:search` shipped in PR 5 (v0.13.x). **Two of fifteen commands remain:** `:save` (saved-search promotion) and `:rule` (saved-search CRUD) — both depend on spec 11's saved-search Manager. Tracked under PR 5b alongside the spec 11 implementation.
  - `ui.confirm_destructive_default` from spec §17 — not in `config/defaults.go`. Confirm modal in `app.go:791-805` always defaults the cursor to "No" unconditionally.
  - `ui.min_terminal_cols` / `ui.min_terminal_rows` from §17 — absent.
  - `ui.unread_indicator`, `ui.flag_indicator`, `ui.attachment_indicator` from §17 — absent in defaults; rendering hardcodes glyphs in `panes.go`.
- Schema/config gaps:
  - ~~The whole `[bindings]` section silently ignored~~ **Closed by PR 2 (v0.13.x).** `ApplyBindingOverrides` translates string overrides to `key.NewBinding`; `config.Load` rejects unknown TOML keys via `MetaData.Undecoded()` with a typed error naming the offender; duplicate bindings fail at `ui.New` with a typed error so the binary refuses to start with a broken keymap.
- TODO-shaped spec language:
  - Spec §11 "Auto-detection from terminal can come post-v1 (Bubble Tea exposes `lipgloss.HasDarkBackground()`)." — explicit deferral.

---

## Spec 05 — Message Rendering

- Implementation: `internal/render/`
- Status overall: partial
- Implementation gaps:
  - DoD "All viewer keybindings from §12 work" — only `j/k` scroll, `H` toggle headers, `r` reply, `f`/`a`/`d` triage are wired (`ui/app.go:1254-1303`). Missing in viewer dispatch: `o` (open in browser via webLink), `O` (open focused link), `e` (toggle quote expand), `Q` (toggle all quotes), `1`-`9` (open link [N]), `a`-`z` (save attachment), `Shift+A`-`Shift+Z` (open attachment), `[` `]` (prev/next message in conversation).
  - Spec §6.3 quoted-reply collapse with threshold from `[rendering].quote_collapse_threshold` — not implemented. `plain.go:46-62` strips `> ` markers but renders all depths verbatim. No collapse, no expand toggle, no `[… N quoted lines …]` placeholder.
  - Spec §6.4 attribution-line detection — no regex, no styling.
  - Spec §6.5 Outlook-specific noise stripping (`[rendering].strip_patterns`) — only the `trackingPixel` regex (`html.go:10`) is applied. No "External email" banner stripping, no `Outlook-AltVw` stripping.
  - Spec §7 plain-text format=flowed unwrapping (RFC 3676) — `plain.go` has no detection or unwrapping. Long-wrapped plaintext stays line-broken.
  - Spec §8 attachment rendering — visibility partially closed by the v0.13.x post-audit slice (2026-05-01). `GetMessageBody` now includes `$expand=attachments`; `FetchBodyAsync` upserts the metadata into the local store; the viewer pane renders an "Attachments:" block between headers and body (mutt/alpine convention), showing name + size + content-type + `(inline)` flag. **Still missing:** `[a]`/`[b]` accelerator-letter prefixes, `internal/graph/GetAttachment` / `attachments/$value` helper, save / open keybindings. Those land with PR 10 alongside the spec 17 §4.4 path-traversal guard.
  - Spec §10 `:open` for browser fallback (`webLink`) — no handler in `dispatchCommand` and no viewer keybinding. `lastDraftWebLink` open (`app.go:1296-1303`) is the only `open` shellout, and it's specifically for drafts.
  - Spec §11 conversation context (thread map under viewer) — not implemented. Viewer renders headers + body only.
  - Spec §6.2 external HTML converter (`html2text` → `pandoc`/`lynx` fallback) — `html.go:17-26` calls `html2text.FromString` with no fallback. Spec config keys `html_converter`, `html_converter_cmd`, `external_converter_timeout` are not in defaults.
- Design drifts:
  - Spec §5.2 body $select drift — `$expand=attachments` shipped in the v0.13.x post-audit slice (2026-05-01); the `GetMessageBody` URL now reads `?$select=body,hasAttachments&$expand=attachments($select=id,name,contentType,size,isInline,contentId)`. Still pending: the `internetMessageHeaders` $select for the full-headers toggle (`H`); spec §4 "Plus all `internetMessageHeaders`" still doesn't materialise. Tracked under PR 10 alongside the rest of the viewer-keys / save-attachment work.
  - Spec §3 declares `BodyOpts.Width`, `BodyOpts.ShowFullHeaders`, `BodyOpts.Theme`. `openMessageCmd` (`ui/app.go:1233`) hardcodes `Width: 80` regardless of viewer width.
  - Spec §5.1 single-flight per message ID (preventing duplicate Graph calls) — not implemented. Two concurrent opens would race.
- Schema/config gaps:
  - `[rendering]` keys missing from defaults: `quote_collapse_threshold`, `large_attachment_warn_mb`, `strip_patterns`, `external_converter_timeout`, `html_converter`, `html_converter_cmd`, `attachment_save_dir`, `wrap_columns`. Only `ShowFullHeaders`, `OpenBrowserCmd`, `HTMLMaxBytes` are present (`config/defaults.go:73-77`).
- TODO-shaped spec language:
  - Spec §13 "html_converter_cmd = ''" (default empty) — design says configurable, code says nothing.

---

## Spec 06 — Hybrid Search

- Implementation: `internal/search/` (4 files + tests) + `internal/graph/search.go`
- Status overall: **shipped (CI scope, v0.17.x — PR 8 of audit-drain).**
- Implementation gaps (all closed by PR 8 except as noted):
  - ~~The entire `internal/search/` package is a doc stub.~~ **Closed.** `search.go` (Searcher / Stream / Query / Result / ResultSource), `local.go` (FTS5 query construction + field-prefix parser), `server.go` (Graph $search dialect), `merge.go` (deduping / debouncing merger), `highlight.go` (snippet builder).
  - ~~Streaming Searcher absent; one-shot `store.Search` masquerades as the spec's design.~~ **Closed.** UI integration via `Deps.SearchService` + the `searchStreamMsg` / `SearchUpdateMsg` channel-drain pattern (mirrors `consumeSyncEventsCmd`).
  - ~~Spec §3 types `Searcher`, `Stream`, `Result`, `ResultSource` — none exist.~~ **Closed.**
  - `:search <query>` command from spec §5.2 — still routed via the existing command dispatcher (re-uses the same streaming searcher as `/`); the command path matches spec.
  - ~~Spec §5.1 status indicators (`[searching local]`, `[📡 searching server…]`, `[merged: 12 local, 47 server]`).~~ **Closed.** The cmd-bar's "search: <q>" hint now appends the streaming status from the merger.
  - ~~`from:bob` / `subject:Q4` field-prefix syntax (§4.1).~~ **Closed.** `internal/search/local.go::ParseQuery` extracts; BuildFTSQuery / BuildGraphSearchQuery render to the per-engine column scopes.
  - `--all` cross-folder flag (§5.3) — still **deferred** (depends on the broader CLI-flag parsing work in spec 14).
- Design drifts:
  - ~~Spec §3.1 "first local result emission <100ms" — current implementation has a 2-second context timeout.~~ **Closed.** `TestSearcherFirstLocalResultLatencyUnder100ms` pins the latency invariant; the streaming Searcher emits the first local snapshot inline inside the start Cmd so there's no extra Bubble Tea round-trip.
- Schema/config gaps:
  - ~~`[search]` section absent.~~ **Closed.** `internal/config/config.go::SearchConfig` adds `local_first`, `server_search_timeout`, `default_result_limit`, `debounce_typing`, `merge_emit_throttle`, `default_sort` per spec §7.
- TODO-shaped spec language: none.

---

## Spec 07 — Single-Message Triage Actions

- Implementation: `internal/action/`
- Status overall: partial
- Implementation gaps:
  - DoD "All 13 action types in §3 implemented" — `executor.go:23-30` exposes only MarkRead, MarkUnread, Flag, Unflag, SoftDelete, Archive, Move. **Missing:** `permanent_delete`, `add_category`, `remove_category`, `move`-with-arbitrary-folder picker. The four draft types (`create_draft`, `create_draft_reply`, `create_draft_reply_all`, `create_draft_forward`) are not in the queued-action surface — only `CreateDraftReply` exists as a one-off non-queued path (`draft.go:26`).
  - ~~Permanent delete unimplemented~~ **Closed by PR 4a (v0.13.x).** `graph.PermanentDelete` helper, `Executor.PermanentDelete`, applyLocal/rollback/dispatch branches, and the `D` keybind with confirm modal all shipped. Inverse returns ok=false so undo doesn't try to restore a tenant-deleted message. Categories (`c`/`C`) closed by PR 4b; move-with-folder-picker (`m`) closed by PR 4c.
  - ~~Categories unimplemented~~ **Closed by PR 4b (v0.13.x).** applyLocal + dispatch + Inverse all handle add/remove; PATCH carries the full post-state list (Graph contract); case-insensitive dedup matches Outlook semantics. UI prompt mode opens via `c` / `C`; Enter dispatches; Esc cancels.
  - ~~Move-with-folder-picker~~ **Closed by PR 4c (v0.13.x).** New `internal/ui/folder_picker.go` renders the modal; FolderPickerMode handles typed-input filter + arrow navigation; Enter dispatches `Triage.Move(ctx, accID, msgID, destID, alias)`; recently-used destinations rank above the alphabetical list (capped at `[triage].recent_folders_count`, default 5); Drafts is filtered from destinations because Graph rejects move-into-Drafts. e2e visible-delta verifies `m` paints "Move to:" + filter narrows + Enter clears the modal.
  - ~~DoD "Optimistic UI, rollback, undo, replay all verified" — **undo is unimplemented**.~~ **Closed by PR 1 (v0.13.x).** Executor pushes inverse on success, `u` wired in list + viewer dispatch, e2e visible-delta verifies the status bar paints `↶ undid: <label>`. See `docs/plans/spec-07.md` iteration log.
  - Replay (`ReplayPending`) — not present in `executor.go`. `Drain` (`executor.go:180`) re-dispatches Pending/InFlight on each cycle but with no rollback semantics and no startup explicit replay path. Spec §10 contract is partially satisfied by Drain piggybacking on the sync loop.
  - ~~Pre-mutation snapshot used for rollback; no inverse computation for undo (`computeInverse` from spec §7.1 is absent).~~ **Closed by PR 1 (v0.13.x).** `internal/action/inverse.go` covers all reversible action types; soft-delete / move use `pre.FolderID` to restore to the source folder. Bulk path still pending (PR-bulk-undo, separate item).
  - Confirmation gates (spec §6.7, CLAUDE.md §7): `D` not wired at all. `:permanent-delete` CLI shape from spec 14 not present. The "always confirms" requirement has no code.
  - DoD "Editor integration verified with at least vim and nano" — spec 15 covers reply editor; for triage the editor path (e.g., reply skeleton spawning $EDITOR) is in `compose/editor.go:34` and used by reply only.
- Design drifts:
  - Spec §5 single-action lifecycle: "Mark action InFlight; dispatch Graph call". `executor.go:139-173` skips the InFlight transition; it goes Pending → (synchronous dispatch) → Done in one DB transaction window. There is no async dispatch goroutine, so the optimistic UI never lights up before the Graph call.
  - Spec §3 `MessageIDs []string` allows N≥1 for bulk; `executor.go:144` rejects `len(a.MessageIDs) != 1`. Bulk actions go through a separate `BatchExecute` path (`batch.go`). The architecture works but no longer matches "ExecuteBulk batches multiple actions" — instead bulk has a typed wrapper.
  - Spec §5.5/§6.5 graph `/move` returns a new ID; the spec calls out "delete the original row and insert with the new ID, preserving all other fields." The dispatch in `apply_local.go:96` discards the moved-id from `MoveMessage`'s return and never updates the local row's primary key. The local row's `id` becomes stale; subsequent operations on the message will 404 on Graph until the next delta sync overwrites.
  - Spec §3 / §11 undo stack capacity `[triage].undo_stack_size` (default 50) — `[triage]` section absent from config entirely.
- Schema/config gaps:
  - Whole `[triage]` section missing from `config/config.go` and `config/defaults.go`. None of `triage.archive_folder`, `triage.confirm_threshold`, `triage.confirm_permanent_delete`, `triage.undo_stack_size`, `triage.optimistic_ui`, `triage.editor`, `triage.draft_temp_dir`, `triage.recent_folders_count` exist.
- TODO-shaped spec language: none.

---

## Spec 08 — Pattern Language

- Implementation: `internal/pattern/` (8 source files + tests)
- Status overall: **shipped (CI scope, v0.18.x — PR 9 of audit-drain).**
- Implementation gaps (all closed by PR 9 except as noted):
  - ~~DoD "All 18 operators from §3.1 implemented." `~h` server-only rejected.~~ **Closed.** `eval_search.go::EmitSearch` renders `~h` to the server `$search` dialect; the strategy selector routes `~h`-bearing patterns to StrategyServerSearch (or TwoStage when combined with read/flag predicates).
  - ~~DoD "Strategy selection table-driven test passes for ≥30 patterns."~~ **Closed.** `compile_test.go::TestCompileStrategySelection` covers 30+ pattern shapes mapping to LocalOnly / ServerFilter / ServerSearch / TwoStage with the rendered query strings asserted as substrings.
  - ~~DoD "`--explain` output is human-readable for at least 10 sample patterns."~~ **Closed.** `Compiled.Explain()` renders strategy + reason + Local SQL / Graph $filter / Graph $search / folder scope on separate lines; tested via TestCompileExplainOutput.
  - DoD "Property-based parser tests pass on 10k random ASTs." — `fuzz_test.go` ships `FuzzParse` / `FuzzCompileLocal`; the spec-mandated 10k-iteration counter in CI is **still deferred** (CI-runtime decision).
  - ~~Two-stage execution (§11), server-hybrid (§7.3), server-filter, server-search — none exist.~~ **Closed.** `execute.go::Execute` dispatches per strategy via consumer-side `LocalSearcher` + `GraphService` seams. TwoStage uses `EvaluateInMemory` against cached envelopes; ServerHybrid INTERSECTs `$filter` + `$search` IDs. `:filter` UI now routes through Compile + Execute.
- Design drifts:
  - ~~Spec §6 declares `pattern.Compile` / `pattern.Execute`.~~ **Closed.** `compile.go::Compile` returns `*Compiled`; `execute.go::Execute` dispatches per strategy. Per-spec API matches; legacy `CompileLocal` retained as the implementation detail Step 0 / Step 4 fall through to.
- Schema/config gaps:
  - ~~`[pattern]` section absent.~~ **Closed.** `internal/config/config.go::PatternConfig` adds `local_match_limit` (5000), `server_candidate_limit` (1000), `prefer_local_when_offline` (true).
- TODO-shaped spec language: `internal/pattern/ast.go` doc still mentions "v0.5.0 ships the lexer, parser, AST, and a local-SQL evaluator. The Graph $filter / $search evaluators land alongside specs 09/10". Stale post-PR 9; non-blocking.

---

## Spec 09 — $batch Execution Engine

- Implementation: `internal/graph/batch.go` + `internal/action/batch.go`
- Status overall: partial
- Implementation gaps:
  - DoD "Composite undo entry pushed for bulk operations; undo executes inverse bulk." Not implemented. `action/batch.go:106-186` does not push any undo entry.
  - DoD "Per-sub-request 429 retry verified against mock." Not implemented. `graph/batch.go:84-100` `ExecuteBatch` is a one-shot HTTP call. There is no `ExecuteAll` orchestrator, no per-sub-request retry loop, no Retry-After honoring inside the batch envelope. Spec §8 `executeChunkWithRetry` does not exist.
  - DoD "`Executor.ExecuteBulk` in `internal/action/executor.go` calls into `BatchExecutor.ExecuteAll`." There is no `BatchExecutor` type. `action/batch.go:135-184` chunks at `graph.MaxBatchSize` and calls `gc.ExecuteBatch` directly. **No concurrency** — chunks are processed serially in a `for start := 0; ...` loop. Spec §7 promised concurrent batch fan-out at `[batch].batch_concurrency=3`.
  - Spec §10's `BulkActionCompletedEvent` — not defined anywhere in `internal/action/`. The UI wraps results in a local `bulkDoneMsg` (`ui/app.go:917`). No event flows over the engine notification channel.
  - `add_category`/`remove_category` per-message body construction (§10.2) — not present (`action/batch.go:200-243` doesn't handle category actions).
  - Permanent delete sub-request shape (`POST /me/messages/{id}/permanentDelete`) — not in `batch.go`'s `actionToSubRequest`.
- Design drifts:
  - Spec §7 specifies progress callback `OnProgress(done, total int)` — no equivalent in `BatchExecute`. UI cannot render the progress modal (§10's `[bulk].progress_threshold`).
  - Spec §11 "soft cap of 5,000 per single bulk operation" — no cap enforced. `BatchExecute` accepts arbitrary `messageIDs` length and chunks freely.
- Schema/config gaps:
  - `[batch]` section entirely absent from `config/config.go`. Hard-coded constants: `MaxBatchSize=20` in `graph/batch.go:14`. None of `batch.max_per_batch`, `batch.batch_concurrency`, `batch.batch_request_timeout`, `batch.dry_run_default`, `batch.max_retries_per_subrequest`, `batch.bulk_size_warn_threshold`, `batch.bulk_size_hard_max` exist in defaults.
- TODO-shaped spec language: none.

---

## Spec 10 — Bulk Operations UX

- Implementation: `internal/ui/app.go` (no separate `internal/ui/filter.go`/`bulk.go`/`preview.go`/`progress.go`)
- Status overall: partial
- Implementation gaps:
  - DoD "Filter mode works for all spec-08 pattern strategies" — only LocalOnly works (because spec 08 only ships LocalOnly).
  - DoD "`;` prefix dispatches all action types in spec 07's bulk-able subset" — `dispatchList` handles `;d` (soft_delete) and `;a` (archive). **Missing:** `;D` permanent-delete, `;m` move, `;r`/`;R` mark-read/unread bulk, `;f`/`;F` flag/unflag bulk, `;c`/`;C` category bulk. Six of ten bulk verbs are unbound.
  - DoD "Confirm modal renders with correct sample, count, and reasoning" — `confirmBulk` (`app.go:986-998`) shows `"<verb> <count> messages?"` only. No filter expression display, no sample of affected messages, no `[p] Preview all` shortcut.
  - DoD "Preview screen with toggleable checkboxes works for sets up to 5000" — no preview screen exists.
  - DoD "Progress modal updates during bulk; cancel works" — no progress modal (`internal/ui/progress.go` does not exist). `runBulkCmd` (`app.go:1003-1049`) runs synchronously with a 30-second timeout and reports a single `bulkDoneMsg`. No `Esc` cancellation.
  - DoD "Result modal correctly categorizes success/partial/pending" — `bulkDoneMsg` handler (`app.go:474-492`) writes a single-line `engineActivity`. No partial-failure breakdown, no `[l] see failed messages`, no pending case.
  - DoD "Composite undo restores only the successful subset" — no undo at all; same gap as spec 07 §11.
  - DoD "Dry-run mode prevents accidental applies" — `[batch].dry_run_default` not in config. CLI `cmd_filter.go` is dry-run-by-default per spec 14, but TUI applies on `;d` confirm with no dry-run state.
  - Spec §10 saved-search promotion ("`:rule save <name>`") — `:rule` not in command dispatcher.
  - Spec §6 dry-run with `!` suffix — not parsed.
- Design drifts:
  - Spec §4.1 entry points include `F` (capital) opening command mode pre-filled with `filter `. Keybinding declared (`keys.go:99`), no handler.
  - Spec §4.4 streaming server results — moot until spec 08 ships server execution.
  - Spec §5.1 lists `;m` and `;c`/`;C` flows that need a folder/category picker — neither picker exists.
- Schema/config gaps:
  - `[bulk]` section absent. No `bulk.preview_sample_size`, `bulk.progress_threshold`, `bulk.progress_update_hz`, `bulk.suggest_save_after_n_uses`.
- TODO-shaped spec language: none.

---

## Spec 11 — Saved Searches as Virtual Folders

- Implementation: `internal/savedsearch/` is a stub (`doc.go` only)
- Status overall: mostly-spec-only
- Implementation gaps:
  - `internal/savedsearch/savedsearch.go`, `store.go`, `evaluator.go`, `refresh.go` from spec §2 — none exist.
  - DoD "CRUD complete via Manager API." `Manager` interface (`Save`, `Get`, `List`, `Delete`, `Evaluate`, `Pinned`) — not declared. The store has `ListSavedSearches`, `PutSavedSearch`, `DeleteSavedSearch` (`store/saved_searches.go:9-57`), but no domain layer above.
  - DoD "Sidebar renders pinned searches with live counts." Sidebar shows saved searches from `[[saved_searches]]` config (`ui/app.go:259-262`, `panes.go:83`). However:
    - Counts are not displayed.
    - "Live counts" / background refresh — no goroutine, no `[saved_search].background_refresh_interval`.
    - Saved searches come from TOML config, not from the `saved_searches` SQLite table. The DB-backed source of truth is unwired.
  - DoD "Edit modal works; pattern test (`t`) functions." No edit modal. No `:rule edit/save/new/delete` commands. No `e` keybinding on saved-search rows.
  - DoD "Auto-suggest after N uses fires once per session per pattern." — not implemented.
  - DoD "Seed defaults populate on first launch." — no first-launch seed for `Unread` / `Flagged` / `From me` (§7.3).
  - DoD "TOML mirror writes correctly; divergence prompt works." — no TOML mirror writer, no divergence detection.
  - DoD "CLI `inkwell rule` subcommands all work." — `cmd/inkwell/cmd_rule.go` does not exist; `cmd_root.go` does not register one.
  - DoD "Cache TTL and background refresh verified." — no caching, no TTL.
- Design drifts:
  - Spec §7.2 "If a saved-search pattern doesn't include `~m`, it scopes to all subscribed folders." Implementation in `app.go:867-899` runs `runFilterCmd` against all messages in the local store via `SearchByPredicate` — happens to match the spec by virtue of having no folder filter, but the lack of `~m` handling means `~m` patterns won't apply folder filtering either (the pattern compiler's `FieldFolder` predicate compiles to `folder_id LIKE ?` with the *string* literal, not a folder-name lookup).
- Schema/config gaps:
  - Whole `[saved_search]` section absent. No `cache_ttl`, `background_refresh_interval`, `seed_defaults`, `toml_mirror_path`.
  - The DB `saved_searches` table exists from migration 001 but is unused; the runtime path uses `[[saved_searches]]` TOML entries instead.
- TODO-shaped spec language: none.

---

## Spec 12 — Calendar (Read-Only)

- Implementation: `internal/graph/calendar.go` + `ui/calendar.go`
- Status overall: partial
- Implementation gaps:
  - ~~The whole calendar schema (§3) is missing~~ **Closed by PR 6a (v0.13.x).** Migration `004_events.sql` adds the events table with `idx_events_start` + `idx_events_account_start`. The `event_attendees` table is deferred (no detail modal yet); when PR 6b lands the attendees expansion, migration 005 adds it. Calendar adapter now reads cache first, fetches on miss/staleness, persists on success. Stale-data fallback when Graph fails.
  - DoD "Calendar sync runs on the same cadence as mail." — `engine.go` has no `SyncCalendar` method. The sync state machine has only `StateDrainingActions` and `StateSyncingFolders` (`engine.go:71-74`). Spec §5 "third state syncing calendar" never exists.
  - Calendar delta sync (`/me/calendarView/delta`, §4.2) — not present.
  - Window slide at midnight (§5.1) — no goroutine.
  - DoD "Sidebar pane renders today + next 1 day with correct event styling." — calendar is rendered as a **modal** (`ui/calendar.go:42`, opened via `:cal`), not as a sidebar pane below "Saved Searches." Spec §6 layout is wrong vs reality.
  - DoD "`:cal` opens full view; week and agenda toggleable." — no week view, no agenda toggle. The modal renders today only.
  - ~~DoD "Event detail modal works; `o` opens Outlook; `l` opens meeting URL."~~ **Closed by PR 6b-i (v0.13.x).** New `CalendarDetailModel` opens via `Enter` on the calendar list; renders subject + time + location + online-meeting URL + organizer + attendees (with response-status glyphs `✓`/`~`/`✗`/`?`) + body preview. `o` shellouts to webLink, `l` shellouts to onlineMeetingURL, `Esc` returns to the list. e2e visible-delta verifies the modal paints attendees + body + Outlook hint. The `j`/`k` cursor + `Enter` dispatch closed in the same PR.
  - Spec §6.2 keybindings — `j` / `k` / `Enter` shipped (PR 6b-i v0.13.x). Day navigation (`]` / `[` / `}` / `{`), today (`t`), and full-view (`c`) deferred to later PRs (each requires multi-day fetch / window-slide / week-view scaffolding).
  - ~~Spec §4.3 `GET /me/events/{id}?$expand=attendees` for full event — no helper.~~ **Closed by PR 6b-i (v0.13.x).** New `graph.GetEvent(ctx, id)` + `graph.EventDetail` / `graph.EventAttendee` types; CalendarFetcher.GetEvent flows through calendarAdapter (live fetch — attendees persistence still deferred).
- Design drifts:
  - `graph/calendar.go:107-113` `ListEventsToday` uses `time.Now().Date()` in local time. Spec §5 / §7.1 says timezone resolution should come from `mailboxSettings.timeZone` (or `[calendar].time_zone` override). System local time is the wrong source of truth on a tenant whose user travels.
  - Spec §3 `attendees` table separate from `events` (so we can query "events where Alice is attending"). With no schema, the use case is impossible.
- Schema/config gaps:
  - `[calendar]` section absent from `config/config.go` and `defaults.go`. No `calendar.default_view`, `calendar.lookback_days`, `calendar.lookahead_days`, `calendar.time_zone`, `calendar.show_declined`, `calendar.sidebar_show_days`, `calendar.show_tentative`, `calendar.online_meeting_indicator`, `calendar.now_indicator`.
- TODO-shaped spec language: none.

---

## Spec 13 — Mailbox Settings

- Implementation: `internal/graph/mailbox.go` + `ui/oof.go`. `internal/settings/` is a stub.
- Status overall: partial
- Implementation gaps:
  - DoD "`:settings` modal renders all read fields." — no `:settings` command in `dispatchCommand`, no settings modal. Only `:ooo` is wired.
  - DoD "`:ooo` modal supports all three status modes, both audience options, both message types." — `OOFModel` (`ui/oof.go:11-93`) is read-only. Toggle (`updateOOF` → `SetAutoReply`, `app.go:606-615`) only flips `enabled` between True and False (mapped to `alwaysEnabled`/`disabled`). No "scheduled" mode, no audience choice (`all`/`contactsOnly`/`none`), no internal/external message editing.
  - DoD "Editor integration for message bodies works with $EDITOR." — no `e` key in OOF modal.
  - DoD "`:ooo on`, `:ooo off`, `:ooo schedule` quick commands." — only `:ooo` is implemented. `:ooo on` / `:ooo off` not parsed.
  - DoD "Status bar indicator appears when OOO active." — no `🌴` indicator, no `[mailbox_settings].ooo_indicator`.
  - DoD "CLI commands work end-to-end." — `cmd/inkwell/cmd_ooo.go` does not exist.
  - DoD "Time zone resolution centralized; calendar and search both use it." — no `settings.Manager.ResolvedTimeZone()` (`internal/settings/` is a doc-only stub). Calendar uses local TZ; search uses local TZ; nothing reads `mailboxSettings.timeZone`.
  - Spec §4 "Refresh on a 5-minute timer; force refresh after any PATCH." — no refresh timer; `MailboxClient.Get` is a one-shot.
- Design drifts:
  - Spec §5.4 PATCH payload includes `scheduledStartDateTime`, `scheduledEndDateTime`, `externalAudience`. `graph/mailbox.go:84-108` `UpdateAutoReplies` only sends `status`/`internalReplyMessage`/`externalReplyMessage`/`externalAudience`. Schedule fields are not sent.
- Schema/config gaps:
  - `[mailbox_settings]` section absent from defaults. None of `confirm_ooo_change`, `default_ooo_audience`, `ooo_indicator`, `refresh_interval`, `default_internal_message`, `default_external_message`.
- TODO-shaped spec language: `mailbox.go:35-36` "ScheduledStart / ScheduledEnd omitted — v0.9.0 doesn't edit schedules." Explicit deferral.

---

## Spec 14 — CLI Mode

- Implementation: `cmd/inkwell/`. `internal/cli/` is a stub.
- Status overall: mostly-spec-only
- Implementation gaps:
  - DoD "All subcommands from §6 implemented and tested." Implemented: `signin`, `signout`, `whoami`, `folders` (list), `messages`, `sync`, `filter`. Spec 18 (v0.15.x) added `folder new/rename/delete` (mailbox modification). **Still missing:** `folder subscribe/unsubscribe/show/tree` (mailbox subscription management — different from create/rename/delete), `message` (show/read/unread/flag/unflag/move/delete/permanent-delete/attachments/save-attachment/reply/reply-all/forward), `rule` (list/show/save/edit/delete/eval/apply — depends on spec 11), `calendar` (today/week/agenda/show — depends on spec 12 PR 6b), `ooo` (on/off/set — depends on spec 13 expansion), `settings`, `export`, `daemon`, `backfill`. ~60% of the spec's CLI surface still absent.
  - DoD "Text and JSON output for every command." `cmd_filter.go:58-64` supports `--output json`; `cmd_messages.go` likely similar but `cmd_folders.go` / `cmd_sync.go` need verification. The promised JSONSchema fixture per command (§12) is not in the repo.
  - DoD "Exit codes match §5.3." There is no exit-code mapping anywhere in `cmd/inkwell/`. All errors return `1` via `main.go:11`.
  - DoD "Pipeline-friendly output (line-delimited JSON, no enclosing array for streams)." `cmd_filter.go:59-64` emits `{"matched": n, "messages": [...]}`, an enclosing array. Spec §5.2 wants line-delimited.
  - DoD "Progress bars on TTY; quiet on pipes." No `mpb` import, no progress bars, no TTY detection.
  - DoD "`--help` is comprehensive at root and per subcommand." Cobra provides defaults; spec mandates "informative" — not audited.
  - DoD "`daemon` mode runs and exits cleanly." No `daemon` subcommand.
  - DoD "At least three documented pipeline examples in the README work as written." README not audited; the §8 pipelines depend on missing subcommands.
  - Spec §4 global flags `--config`, `--verbose` are present (`cmd_root.go:35-36`). **Missing:** `--output`, `--color`, `--log-level`, `--quiet`, `--no-sync`, `--yes`. (Per-subcommand `--output` and `--yes` exist on `cmd_filter`; not global.)
- Design drifts:
  - Spec §3 mode routing: "If no subcommand → launch TUI." `main.go:9-14` calls `newRootCmd().Execute()` only — there is no special-case for empty argv. Cobra's default behaviour with `RunE` (`cmd_root.go:47`) does run `runRoot` (presumably the TUI launcher) for the bare command, so this works in practice, but the implementation differs from the spec's `if len(os.Args) == 1` short-circuit.
  - Spec §6.5 `inkwell filter --action delete --since 30d --apply` — `--since` flag not present in `cmd_filter.go`. The user must encode the time window in the pattern (`~d <30d`).
- Schema/config gaps:
  - `[cli]` section entirely absent. None of `cli.default_output`, `cli.color`, `cli.confirm_destructive_in_cli`, `cli.progress_bars`, `cli.json_compact`, `cli.export_default_dir`.
- TODO-shaped spec language: none.

---

## Spec 15 — Compose / Reply (drafts only)

- Implementation: `internal/compose/` + `action/draft.go` + UI compose flow
- Status overall: partial
- Implementation gaps:
  - DoD "Action executor (extending spec 07) handles the four new draft types with idempotent local apply + Graph dispatch + replay." `CreateDraftReply` now flows through the action queue end-to-end (PR 7-i v0.13.x): two-stage dispatch (createReply → record draft_id+web_link in Params → PATCH), Failed status persisted on either stage's failure, Drain skips the type so non-idempotent stage 1 isn't re-fired, the recorded draft_id sets up PR 7-ii's resume path. Still missing: `TypeCreateDraft` (new), `TypeCreateReplyAll`, `TypeCreateForward`, `TypeDiscardDraft` — those land with PR 7-iii alongside the R/F/m skeletons. Crash-recovery (the resume-on-startup invariant) lands with PR 7-ii.
  - ~~DoD "`compose_sessions` table created by migration N+1 (latest schema version bumped accordingly)."~~ **Closed by PR 7-ii (v0.13.x).** Migration 005 adds the table per spec §7; SchemaVersion bumped to 5; partial index `idx_compose_sessions_unconfirmed` accelerates the launch-time resume scan. See `docs/plans/spec-15.md` iter 4.
  - DoD "Discard flow deletes both the local draft row AND the server-side draft (Graph `DELETE /me/messages/{id}`)." UI flow (`updateComposeConfirm` `app.go:548-591`, case `"d"`) only deletes the tempfile. There is no Graph `DELETE` call. Server-side draft never lifted.
  - DoD "On `s`, the action's `webLink` is captured; the status bar exposes 'open in Outlook' for 30s after." `lastDraftWebLink` (`app.go:233`) is set indefinitely, not for 30s. There's no TTL.
  - ~~DoD "Crash-recovery: kill -9 the app while in the editor, restart, the resume-prompt fires and the tempfile is intact."~~ **Closed by PR 7-ii (v0.13.x).** Form state (To/Cc/Subject/Body) snapshots into `compose_sessions` on entry + each focus change (Tab); Init runs `scanComposeSessionsCmd` which GCs confirmed sessions older than 24h then surfaces the most-recent unconfirmed row via a confirm modal. Y restores into ComposeMode preserving SessionID; n inline-confirms the row. Spec wording about a tempfile is a v1 carryover — the v2 in-modal redesign (iter 3) replaced the tempfile with the JSON snapshot blob; the §7 invariant ("on next launch the user is offered to resume their crashed draft") is now satisfied via that path.
  - ~~DoD "`r`/`R`/`f`/`m` keybindings wired with the pane-scoped resolution rule from §9."~~ **Closed by PR 7-iii (v0.13.x).** Viewer-pane R fires reply-all; f fires forward (Drafts-not-wired fallback to legacy ToggleFlag); m fires new-message (Drafts-not-wired fallback to legacy startMove). Folders-pane m also fires new-message (was previously a no-op). List-pane retains all four bindings as their original triage verbs (mark-read / mark-unread / toggle-flag / move). Three-way pane scope on `R` (mark-unread list / reply-all viewer / rename-folder folders) extends the existing `r`/`f`/`m` pattern. See `docs/plans/spec-15.md` iter 5.
  - Spec §6.1 `INKWELL_EDITOR` env override — implemented at `compose/editor.go:21-29`. OK.
  - ~~Spec §10 row "App crash while editor is open / On next launch, 'resume draft?' prompt; tempfile and source_id are intact in `compose_sessions`."~~ **Closed by PR 7-ii (v0.13.x).** See the matching DoD bullet above; the v2 in-modal flow uses a JSON snapshot blob (no tempfile) but the user-visible invariant — "next launch offers to resume your crashed draft" — is now satisfied.
  - Spec §11 lint guard "fails any source line that contains the literal string `Mail.Send` outside `docs/PRD.md` and `internal/auth/scopes.go`" — no CI script for this in `scripts/`.
- Design drifts:
  - Spec §8 "local row gets a temp ID that's replaced after the Graph response." Optimistic local insert is intentionally skipped — drafts only appear in the local store after the next delta sync of the Drafts folder. Spec wording predates the refactor; the action queue now records the action in the actions table (post-PR 7-i), which is the spec-15 §8 audit-trail intent. Reframe in a future spec edit; not a code gap.
  - Spec §5 declared `DraftParams` with `Attachments []AttachmentRef`. `compose.ParsedDraft` (`compose/parse.go:11-17`) has no attachments field. Attachments path absent end-to-end.
  - ~~Spec §6.2 forward skeleton, reply-all skeleton — only `ReplySkeleton` exists.~~ **Closed by PR 7-iii (v0.13.x).** `ReplyAllSkeleton`, `ForwardSkeleton`, `NewSkeleton` shipped in `internal/compose/template.go` plus matching `ApplyReplyAllSkeleton` / `ApplyForwardSkeleton` / `ApplyNewSkeleton` on the in-modal ComposeModel. Reply-all dedups against userUPN; forward normalises `Fw:` / `Fwd:` to canonical `Fwd:` and emits the canonical `---------- Forwarded message ----------` header block.
- Schema/config gaps:
  - No `[compose]` section. No `INKWELL_EDITOR` config key (env-only).
- TODO-shaped spec language:
  - `compose/template.go:18-19` "v0.11.0 only implements KindReply; the others land in follow-up iterations of spec 15." Explicit deferral.

---

## Summary table

**Counting note.** Numbers below are the count of distinct
remaining bullets in the per-spec sections (Implementation gaps +
Design drifts + Schema/config gaps; "TODO-shaped" entries are
informational deferrals and excluded). The "Closed since v0.12.0"
column tags the audit-drain PRs that struck out their bullets
inline. Refresh after every audit-drain PR.

| Spec | Status | Open | Closed since v0.12.0 | Highest-risk remaining |
|------|--------|------|----------------------|------------------------|
| 01   | partial | 4 | — | AADSTS code classification + clock-skew detection + CLI PromptFn missing |
| 02   | partial | 2 | maintenance loop (PR 11), EvictBodies wiring (PR 11) | flag_due_at not in MessageFields; saved-search delete-by-name (depends on spec 11) |
| 03   | partial | 5 | ThrottledEvent + AuthRequiredEvent emission (PR 3) | tombstone-aware delta; engine-Stop UI goroutine leak; priority queue absent |
| 04   | partial | 8 | `[bindings]` config wired + `?` help overlay (PR 2); 5 of 7 `:` commands (PR 5) | lifecycle teardown not via UI; transient_status_ttl; min_terminal refusal; viewer `f` Forward; default-No confirm config |
| 05   | partial | 12 | — | viewer keybindings (links/attachments/conv-thread/quote toggles) all absent; body $select drift; no GetAttachment helper |
| 06   | shipped | 1 | streaming Searcher + graph $search + merger + field prefixes + UI streaming integration (PR 8) | `--all` cross-folder flag + saved-search promotion (depend on spec 14 CLI flags / spec 11 Manager) |
| 07   | partial | 9 | undo (PR 1); permanent_delete (PR 4a); add/remove category (PR 4b); inverse computation (PR 1); move-with-folder-picker (PR 4c) | replay-on-startup; lifecycle InFlight skipped; move-id stale after `/move` |
| 08   | shipped | 1 | Compile/Execute API + $filter/$search evaluators + TwoStage + strategy selector + [pattern] config (PR 9) | 100k-message bench + 10k-AST property test in CI deferred |
| 09   | partial | 9 | — | no per-sub-request 429 retry; no concurrent batch fan-out; no composite undo |
| 10   | partial | ~14 | bulk-config skeleton (PR 12) | no preview screen; no progress modal; 6 of 10 `;` verbs unbound; `F` keybind unhandled |
| 11   | mostly-spec-only | 12 | — | whole `Manager` API absent; live counts / TOML mirror / `:rule` / seed defaults all unimplemented |
| 12   | partial | 7 | events table + persistence + cache-first reads (PR 6a); detail modal + j/k/Enter + GetEvent (PR 6b-i) | sync-engine third state; midnight window slide; pane-vs-modal layout; ]/[/{/}/t/c day/week navigation |
| 13   | partial | 10 | — | OOF read-only beyond enable/disable; no schedule/audience editing; no `:settings`; no time-zone source of truth |
| 14   | mostly-spec-only | 11 | spec 18 added `folder new/rename/delete` (overlap, not closure) | ~60% of CLI surface absent (rule/calendar/ooo/settings/message subverbs/export/daemon/backfill); exit-code map missing; line-delimited JSON not honoured |
| 15   | partial | 6 | drafts via action queue + two-stage idempotent dispatch (PR 7-i); compose_sessions migration + crash-recovery resume + 24h GC (PR 7-ii); ReplyAll/Forward/NewMessage action types + skeletons + R/f/m bindings (PR 7-iii) | no Graph delete on discard; webLink TTL (30s); lint guard for Mail.Send literal |

**Drained-since-v0.12.0 totals:** 39 audit bullets struck out
across 9 specs (02 + 03 + 04 + 05 + 06 + 07 + 08 + 12 + 15). Nine of
the original top-10 leverage gaps are closed (#1 undo, #2 bindings/
help, #3 events, #4 permanent-delete, #5 commands, #6 calendar
schema, #7 drafts queue, #8 hybrid search, #9 pattern Compile/
Execute). Spec 15 §7 crash-recovery (PR 7-ii) + R/F/m drafts (PR
7-iii) fold in alongside the queue work; spec 05 §8 attachment
visibility partially closes the §10 viewer-keys block; spec 06
ships end-to-end via PR 8; spec 08 ships end-to-end via PR 9.
Remaining audit-drain queue: PR 5b (`:save` / `:rule` blocked on
spec 11), PR 6b-ii (calendar sync engine), PR 10 (viewer
keybindings + save/open + GetAttachment helper).

---

## Top 10 highest-leverage impl gaps

Ranked by what blocks a v0.X release.

1. ~~**Action queue undo unimplemented (spec 07 §11)**~~ **Closed by PR 1 (v0.13.x).** Executor pushes inverse, `u` wired in list + viewer, e2e visible-delta verifies the status bar paints. See `docs/plans/spec-07.md` for the iteration log.

2. ~~**`[bindings]` config silently ignored (spec 04 §17)**~~ **Closed by PR 2 (v0.13.x).** `?` help overlay (§12) and `:help` command (§6.4) closed in the same PR. See `docs/plans/spec-04.md` iter 9.

3. ~~**`ThrottledEvent` / `AuthRequiredEvent` never emitted (spec 03 §3)**~~ **Closed by PR 3 (v0.13.x).** Engine.OnThrottle hook + emitCycleFailure classifier; integration tests cover both paths. See `docs/plans/spec-03.md` iter 8.

4. ~~**Permanent delete (`D`) unimplemented end-to-end (spec 07 §6.7)**~~ **Closed by PR 4a (v0.13.x).** Categories (`c`/`C`) closed by PR 4b (iter 4); move-with-folder-picker (`m`) closed by PR 4c (iter 5). See `docs/plans/spec-07.md`.

5. ~~**7 of 15 `:` commands unimplemented (spec 04 §6.4)**~~ Five closed by PR 5 (v0.13.x): `:refresh`, `:folder`, `:open`, `:backfill`, `:search`. The remaining two (`:save`, `:rule`) depend on spec 11's saved-search Manager; tracked under PR 5b alongside the spec 11 implementation. See `docs/plans/spec-04.md` iter 10.

6. ~~**Calendar schema not migrated (spec 12 §3)**~~ Schema + persistence closed by PR 6a (v0.13.x); migration 004 adds the events table. The `:cal` modal now serves from cache first with TTL refresh; offline use works once events have been cached. Detail modal + j/k/Enter navigation + GetEvent($expand=attendees) closed by PR 6b-i (v0.13.x). Sync engine pass, midnight window slide, sidebar-vs-modal layout question, and day/week navigation deferred to PR 6b-ii.

7. ~~**Compose draft path bypasses action queue (spec 15 §5, §8)**~~ **Closed by PR 7-i (v0.13.x).** `ActionCreateDraftReply` constant added; `Executor.CreateDraftReply` enqueues with status Pending → calls Graph `createReply` → persists `draft_id`+`web_link` via the new `UpdateActionParams` → calls `PatchMessageBody` → marks Done. PATCH-after-success failure leaves the action Failed with `draft_id` intact so PR 7-ii's startup resume path can re-PATCH idempotently. Drain skips the type because stage 1 is non-idempotent. Reply-all / forward / new-message variants deferred to PR 7-iii; crash-recovery resume scan deferred to PR 7-ii. See `docs/plans/spec-15.md` iter 2.

8. ~~**Hybrid search package empty (spec 06)**~~ **Closed by PR 8 (v0.17.x).** `internal/search/` ships streaming Searcher with parallel local FTS5 + Graph $search branches, deduping merger with throttled emit, field-prefix syntax, snippet highlighting. UI integration paints progressive snapshots with a status-line hint and Esc-cancels-stream. See `docs/plans/spec-06.md` iter 2.

9. ~~**Pattern Compile/Execute surface absent (spec 08 §6)**~~ **Closed by PR 9 (v0.18.x).** `internal/pattern/compile.go` exposes Compile / CompileNode / Compiled / ExecutionStrategy / CompilationPlan / CompileOptions per spec §6; `eval_filter.go` + `eval_search.go` + `eval_memory.go` + `execute.go` ship the strategy dispatch path. `:filter` UI routes through the new API. See `docs/plans/spec-08.md` iter 2.

10. **Body fetch select drift (spec 05 §5.2)** — `GET /me/messages/{id}?$select=body,hasAttachments` ignores `attachments` and `internetMessageHeaders` and skips `$expand=attachments`. The full-headers toggle (`H`) renders only cached envelope fields; spec promised internet headers expansion. Attachment download is structurally impossible because `internal/graph/` has no `GetAttachment` / `attachments/$value` helper anywhere. Blocks v0.5.x feature completeness for the viewer pane.
