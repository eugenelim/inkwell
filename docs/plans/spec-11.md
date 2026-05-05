# Spec 11 — Saved Searches as Virtual Folders

## Status
in-progress (B-1 shipped: Manager API + sidebar counts + :rule CRUD + seed defaults; B-2 deferred: edit modal, auto-suggest, CLI subcommands, background refresh timer).

## DoD checklist (mirrored from spec)
- [x] `[[saved_searches]]` TOML config table with `name` + `pattern`.
- [x] Saved searches render in the folders pane below regular folders, under a "Saved Searches" section header (non-selectable).
- [x] ☆ glyph distinguishes saved-search rows from real folders.
- [x] Cursor j/k skips the section header row.
- [x] Enter (or `l`) on a saved-search row runs its pattern via existing `runFilterCmd` (spec 10's filter machinery).
- [x] Selection auto-focuses the list pane.
- [x] Filter cmd-bar reminder appears (`filter: <pattern> · matched N · …`).
- [x] Count badges: sidebar shows match count for pinned searches (spec 11 §5.1).
- [x] Manager API (`internal/savedsearch`) — CRUD, evaluation cache, CountPinned, SeedDefaults, TOML mirror.
- [x] DB-backed saved_searches table CRUD — save/delete/list/get all work via Manager.
- [x] TOML mirror to `~/.config/inkwell/saved_searches.toml` — written after every save/delete.
- [x] Evaluation cache + background count refresh on sync event — Init fires refreshSavedSearchCountsCmd; FolderSyncedEvent triggers re-count.
- [x] `:rule save <name>` — saves active filter as named saved search.
- [x] `:rule list` — shows saved search names in activity bar.
- [x] `:rule show <name>` — shows pattern in activity bar.
- [x] `:rule delete <name>` — confirm modal → delete.
- [x] Seed defaults (`Unread`, `Flagged`, `From me`) on first launch.
- [x] Tests: Manager unit tests (9 cases) + UI dispatch tests (rule + saved search + count badge).
- [ ] Edit modal (`e` keybinding / `:rule edit <name>`) — deferred B-2.
- [ ] Auto-suggest after N filter uses — deferred B-2.
- [x] CLI `inkwell rule list/save/edit/delete/eval/apply` — shipped by PR G-1 (spec-14, 2026-05-04).
- [ ] Background refresh timer (independent of sync event) — deferred B-2.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| pattern.Execute over 100k msgs | <100ms p95 | <10ms (SQLite local) | spec-08 bench | ok |

## Iteration log

### Iter 1 — 2026-04-29 (config-driven sidebar, no DB)
- Slice: config + UI wiring only.
- Files:
  - internal/config/config.go: Config gains `SavedSearches []SavedSearchConfig` with `name` + `pattern` fields.
  - internal/ui/app.go: ui.Deps gains `SavedSearches []SavedSearch`. New consumer-site `SavedSearch{Name, Pattern}` type. dispatchFolders Open path checks `SelectedSavedSearch()` first; on hit, runs `runFilterCmd(ss.Pattern)` and auto-focuses ListPane.
  - internal/ui/panes.go: displayedFolder grows `isSaved`, `savedName`, `savedPattern`, `isSavedHeader`. FoldersModel grows `saved []SavedSearch` + `SetSavedSearches`. rebuild appends a synthetic header row + one per saved search. Up/Down skip the header. Selected() returns ok=false on saved/header rows; new SelectedSavedSearch() handles the saved-search case.
  - cmd/inkwell/cmd_run.go: maps cfg.SavedSearches → []ui.SavedSearch and threads through Deps.
  - docs/CONFIG.md: `[[saved_searches]]` documented; deferred keys listed.
  - 3 dispatch tests.
- Commands: `make regress` green.
- Critique:
  - The cursor's "skip headers" logic in Up/Down assumes m.items has at least one selectable row before/after the header. With zero saved searches, the header is never appended, so this is fine. With zero folders + N saved searches, the cursor lands on the first saved search; Up clamps at 0. OK.
  - SelectByID matches `it.f.ID == id`. For saved-search rows, `it.f.ID` is the zero-value empty string. If a caller ever passes id="" to SelectByID, the cursor could land on the header (via the saved-search row whose f.ID happens to also be ""). Not exercised in v0.7.0; revisit if SelectByID gains new callers.

### Iter 2 — 2026-05-02 (B-1: Manager API + sidebar counts + :rule CRUD + seed defaults)
- Slice: DB-backed Manager, count badges, :rule commands, savedSearchAdapter wiring.
- Files:
  - internal/savedsearch/manager.go (NEW): Manager struct with Save/Delete/DeleteByName/Get/List/Evaluate/CountPinned/SeedDefaults/InvalidateCache + TOML mirror write.
  - internal/savedsearch/manager_test.go (NEW): 9 unit tests covering all Manager methods.
  - internal/config/config.go: Added SavedSearchSettings struct (CacheTTL, BackgroundRefreshInterval, SeedDefaults, TOMLMirrorPath).
  - internal/config/defaults.go: SavedSearch defaults seeded.
  - internal/ui/app.go: SavedSearch struct extended (ID, Pinned, Count). SavedSearchService interface added. Deps gains SavedSearchSvc. Model gains savedSearches + pendingRuleDelete. Init fires refreshSavedSearchCountsCmd. dispatchCommand routes :rule. dispatchRule helper + ruleSaveCmd/ruleDeleteCmd + savedSearchesUpdatedMsg/savedSearchSavedMsg handlers. Count refresh on FolderSyncedEvent.
  - internal/ui/messages.go: savedSearchesUpdatedMsg + savedSearchSavedMsg types.
  - internal/ui/panes.go: displayedFolder gains savedID/savedCount/savedPinned. rebuild populates them. SelectedSavedSearch returns full struct. View renders count badge when Count≥0.
  - cmd/inkwell/cmd_run.go: savedSearchAdapter (Save/DeleteByName/Reload/RefreshCounts). Manager wiring with SeedDefaults + initial list load + fallback to TOML config. convertSavedSearchList helper.
  - internal/ui/dispatch_test.go: stubSavedSearchService + 7 new tests (RuleSave, RuleSaveNoFilter, RuleList, RuleShow, RuleDelete, RuleDeleteConfirmYes, CountBadge).
- Commands: `go test -race -run "TestRule|TestSavedSearch" ./internal/ui/...` → ok; `go build ./...` → clean.
- Critique:
  - Background refresh timer (periodic, independent of sync events) not yet wired — deferred B-2.
  - Edit modal (`e` key / `:rule edit`) not yet implemented — deferred B-2.
  - Count badge shows "0" for a pinned search that matched nothing — correct per spec; user sees the search is active but empty.
  - TOML mirror writes best-effort; parse errors on next launch produce no divergence detection yet (spec §4 prompt) — deferred B-2.
- Next: B-2 (edit modal, auto-suggest, CLI rule subcommands, background refresh timer).

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: Mail.Read (existing).
- [x] Store reads/writes: messages (read via SearchByPredicate from the existing :filter path); saved_searches table (CRUD via Manager).
- [x] Graph endpoints: none directly. Pattern eval is local-only.
- [x] Offline behaviour: saved searches work fully offline against the FTS5 index.
- [x] Undo: N/A (no mutation to messages).
- [x] User errors: pattern parse errors surface via the existing ErrorMsg → status bar. :rule save without filter gives clear error.
- [x] Latency budget: pattern compile + store query both <100ms per spec 02 budgets.
- [x] Logs: nothing new logged beyond existing sync/action paths.
- [x] CLI mode: spec 14 / B-2 will surface saved searches via `inkwell rule` subcommands.
- [x] Tests: Manager unit tests (9) + UI dispatch tests (rule + saved search + count badge).
