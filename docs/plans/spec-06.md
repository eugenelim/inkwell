# Spec 06 — Hybrid Search

## Status
in-progress (CI scope, local-FTS subset shipped in v0.4.0; Graph $search merge + streaming Stream + highlighting + saved searches deferred to v0.5.x).

## DoD checklist (mirrored from spec)
- [x] Local FTS5 query layer — store.Search() over messages_fts (already in spec 02).
- [x] `/` activates SearchMode in the TUI; buffer captures the query.
- [x] Enter on `/` prompt fires the query against store.Search and replaces the list pane with results, keyed off a sentinel folder ID `search:<query>`.
- [x] Esc clears the search and restores the prior folder.
- [x] Help bar advertises `/ search` in the list pane.
- [ ] Server `$search` branch — deferred. v0.4.0 ships local-only.
- [ ] Streaming Stream API (Updates / Done / Err / Cancel) — deferred. v0.4.0 dispatches a single Cmd that returns the full result set.
- [ ] Highlighting / snippet generation — deferred. v0.4.0 shows the matching message envelope unchanged in the list.
- [ ] Saved searches — spec 11.
- [ ] Cross-folder match — already implicit (the FTS5 query has no folder scope unless we set FolderID).

## Iteration log

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
