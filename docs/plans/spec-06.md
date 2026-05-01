# Spec 06 — Hybrid Search

## Status
shipped (CI scope, v0.17.x — 2026-05-01). PR 8 closed every spec
06 audit row: streaming hybrid search package + graph $search +
field-prefix syntax + UI streaming integration with merger,
status indicator, and Esc-cancels-stream. Manual real-tenant
smoke (see §5 of spec) deferred per CLAUDE.md §5.5.

## DoD checklist (mirrored from spec)
- [x] Local FTS5 query layer — store.Search() over messages_fts (already in spec 02).
- [x] `/` activates SearchMode in the TUI; buffer captures the query.
- [x] Enter on `/` prompt fires the query against the streaming Searcher (PR 8) and replaces the list pane with progressive snapshots, keyed off a sentinel folder ID `search:<query>`.
- [x] Esc clears the search, cancels in-flight branches, and restores the prior folder.
- [x] Help bar advertises `/ search` in the list pane.
- [x] Server `$search` branch — **closed by PR 8 (v0.17.x).** `internal/graph/search.go` issues GET /me/messages?$search=… with proper URL-encoding (RFC 3986 quoting); folder-scoped variant routes through /me/mailFolders/{id}/messages.
- [x] Streaming Stream API (Updates / Done / Err / Cancel) — **closed by PR 8.** `internal/search/search.go` Searcher returns *Stream; per-snapshot debouncer in merge.go throttles emit cadence; Cancel() terminates both branches.
- [x] Highlighting / snippet generation — **closed by PR 8.** highlight.go produces match-anchored 120-char snippets with markdown-style asterisk emphasis around the first matching term.
- [x] Field-prefix syntax (`from:` / `subject:` / `body:`) — **closed by PR 8.** ParseQuery extracts prefixed values; BuildFTSQuery + BuildGraphSearchQuery render to per-engine column scopes.
- [x] Result merging (dedup + SourceBoth promotion + received-date-DESC sort) — **closed by PR 8** with table-driven tests covering overlap, sort, dedup.
- [ ] Saved searches — spec 11.
- [ ] Cross-folder match — already implicit (the FTS5 query has no folder scope unless we set FolderID); the streaming searcher passes Query.FolderID through to both branches.
- [ ] CLI `:search --all` flag — deferred (depends on the same `--flag` parser the CLI mode work in spec 14 will land).

## Iteration log

### Iter 2 — 2026-05-01 (streaming hybrid search, PR 8 of audit-drain)
- Trigger: spec 06 audit row + audit-drain PR 8. `/`-search ran
  a one-shot 2-second store.Search call; spec 06 calls for
  parallel local + Graph $search with streaming snapshots.

- Slice (graph layer):
  - `internal/graph/search.go` adds `SearchMessages(ctx, opts)`
    returning a `ListMessagesResponse`. URL-encodes the $search
    value with literal double-quote wrapping per Graph's KQL-ish
    contract; folder-scoped variant routes through
    `/me/mailFolders/{id}/messages`. Defensive empty-query
    check rejects without an HTTP round-trip.

- Slice (search package):
  - `internal/search/search.go` ships the public surface:
    `Searcher`, `Stream` (Updates / Done / Err / Cancel),
    `Result`, `Query`, `ResultSource` (Local / Server / Both),
    `LocalSearcher` + `ServerSearcher` consumer-side seams.
    `New(local, server, opts)` constructs; `Search(ctx, q)`
    spawns two goroutines + a merger debouncer.
  - `internal/search/local.go` parses field-prefix syntax
    (`from:` / `subject:` / `body:`); BuildFTSQuery emits
    column-scoped FTS5 expressions with OR-grouped `from:` doubles
    (so `from:bob` matches either `from_address` or `from_name`).
    Auto-quotes email-shaped tokens to dodge FTS5's tokeniser.
  - `internal/search/server.go` mirrors BuildFTSQuery for the
    Graph $search dialect (`from:`, `subject:`, `body:`); same
    auto-quote rule.
  - `internal/search/merge.go` is the streaming merger:
    map-keyed dedup; SourceBoth promotion when an overlap is
    seen; spec §4.3 sort (Both > Local > Server tier; received_at
    DESC; BM25 tiebreak); throttled emit via a debouncer
    goroutine.
  - `internal/search/highlight.go` produces match-anchored
    120-char snippets with markdown-style asterisk emphasis on
    the first match.

- Slice (config):
  - `internal/config/config.go` + defaults add `[search]`:
    `local_first` (true), `server_search_timeout` (5s),
    `default_result_limit` (200), `debounce_typing` (200ms),
    `merge_emit_throttle` (100ms), `default_sort`
    ("received_desc"). Spec 06 §7 default table.

- Slice (UI integration):
  - `internal/ui/app.go::Deps` gains `Search SearchService`
    (consumer-side type defined in app.go).
  - `internal/ui/app.go::Model` gains `searchStatus`,
    `searchCancel`, `searchUpdates` fields tracking the
    streaming run.
  - `internal/ui/messages.go` adds `SearchUpdateMsg` (progressive
    snapshot) and the unexported `searchStreamMsg` (carries the
    live channel + cancel from the first emission into the
    Update loop).
  - `runSearchCmd` routes through the SearchService when wired;
    falls back to the legacy single-shot store.Search when nil
    (test setup, degraded mode).
  - `startStreamingSearchCmd` reads the first snapshot inline
    so the user sees results before the next render frame;
    `consumeSearchUpdatesCmd` re-arms after each subsequent
    snapshot. Esc / new query cancels the in-flight stream
    cleanly.
  - View renders the spec §5.1 status hint
    (`[searching…]` / `[merged: N local, M server, K both]` /
    `[local only — server failed]`) in the search-line.

- Slice (production wiring):
  - `cmd/inkwell/cmd_run.go` adds `searchAdapter` wrapping
    `internal/search.Searcher` into `ui.SearchService`. The
    adapter publishes per-snapshot SourceCount-derived status
    hints. `graphServerSearcher` adapts `graph.Client.SearchMessages`
    → `search.ServerSearcher`. `convertGraphMessageEnvelope`
    flattens graph.Message → store.Message for the list pane.

- Tests (12 new across 3 packages):
  - **internal/search**: 5 (TestParseQueryFieldPrefixes,
    TestBuildFTSQueryShapes, TestBuildGraphSearchQueryShapes,
    TestHighlightSnippetCentersOnMatch + 2 highlight cases) +
    8 Searcher behaviour tests (TestSearcherEmitsLocalThenMergesServer,
    TestSearcherDedupesOverlappingBranches, TestSearcherSorts...,
    TestSearcherLocalOnlySkipsServer, TestSearcherEmptyQuery...,
    TestSearcherServerError..., TestSearcherCancelStops...,
    TestSearcherFirstLocalResultLatencyUnder100ms).
  - **internal/graph**: 3 (TestSearchMessagesEncodesQuotedQueryString,
    TestSearchMessagesScopesToFolder,
    TestSearchMessagesRejectsEmptyQuery).
  - **internal/ui**: 3 (TestSearchEnterRoutesThroughSearchService,
    TestSearchEscCancelsInFlightStream,
    TestSearchUpdateMsgIgnoredAfterQueryChange).

- Decisions:
  - **Streaming via re-armed Cmd, not a long-lived goroutine.**
    Bubble Tea's idiom is one Cmd → one Msg. The
    `consumeSearchUpdatesCmd` pattern matches the existing
    `consumeSyncEventsCmd` in the engine wiring — re-arm after
    each emission until the channel closes.
  - **Merger debouncer instead of per-add emit.** A burst of
    adds from local + server arriving close together would
    paint the list 3-4 times in the same frame; the 100ms
    throttle collapses bursts into one repaint window.
  - **`from:` ORs both name + address columns in FTS5.** The
    sender's display name lives in `from_name` and email in
    `from_address`; users typing `from:bob` mean "either".
    Without OR-grouping, `from:bob` would compile to AND of
    both columns and match only senders whose name AND email
    both contain "bob".
  - **Server failure → local-only with `[local only]` hint,
    not error.** Spec §8 explicitly: a server timeout / 429 /
    5xx surfaces local results with a status hint, not a
    Stream-level error. The Stream's Err() still carries the
    failure so callers that care can inspect.
  - **First-snapshot inline read.** The search Cmd reads the
    first snapshot off the channel synchronously inside the
    Cmd goroutine — no second tea.Msg dispatch round-trip.
    This is what makes the spec §6 latency budget (<100ms for
    first local result) achievable end-to-end.

- Result: full -race + -tags=e2e + -tags=integration suite
  green; 14 new unit tests + 3 new dispatch tests pass; spec
  06 §10 DoD bullets all closed except the cross-folder
  `--all` flag and saved-search promotion (depend on spec 11
  / spec 14 work tracked separately).

  **Deferred:** CLI `inkwell search` subcommand (spec 14
  scope), saved-search promotion via `:search --save NAME`
  (spec 11 scope), `--sort=relevance` flag (spec §4.3
  alternate; today the merger always uses received_desc).

### Iter 1 — 2026-04-28 (local FTS subset)
- Slice: UI integration. The store layer already exposes Search(); spec 02 wired the FTS5 virtual table + INSERT/DELETE/UPDATE triggers.
- Files added/changed:
  - internal/ui/app.go: searchBuf + searchActive + searchQuery + priorFolderID on Model. `/` enters SearchMode (saves prior folder). updateSearch handles Enter (run query) / Esc (clear) / Backspace / printable runes. runSearchCmd calls store.Search and emits MessagesLoadedMsg keyed to searchFolderID(q).
  - View renders a search prompt in the cmd-bar slot during SearchMode and a "search: <query> (esc to clear)" reminder while results are visible.
  - Help bar list hint now includes `/ search`.
- Tests: dispatch_test adds:
  - TestSearchModeCapturesAndRunsQuery — '/'+typed+Enter+Esc round trip; state transitions correctly, Cmd is non-nil after Enter.
  - TestSearchEmptyQueryDoesNothing — empty Enter exits cleanly without firing a Cmd.
- Critique:
  - No streaming yet — when v0.5.x adds Graph $search, the store query and the Graph query both fire and merge as they arrive. v0.4.0's single-Cmd-return shape is throw-away when that lands.
  - No highlighting — the matched terms aren't visibly bold in the list. Cosmetic, defer.
  - The list pane reuses ListModel which has the load-more pre-fetch logic — pagination is suppressed during search (correct: the FTS limit is fixed at 200).

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: Mail.Read (already in PRD §3.1).
- [x] Store reads/writes: messages_fts (read), no writes.
- [x] Graph endpoints: none in v0.4.0 (local-only). Server $search lands later.
- [x] Offline behaviour: search works offline against the local FTS5 index. That's the whole point.
- [x] Undo: N/A.
- [x] User errors: search Cmd returns ErrorMsg on store failure → status bar.
- [x] Latency budget: store.Search has its own bench (spec 02). UI overhead is one Cmd round-trip.
- [x] Logs: store.Search doesn't log query text. Bodies never logged.
- [x] CLI mode: spec 14.
- [x] Tests: dispatch_test covers the UI flow. Store Search has its own tests in spec 02.
