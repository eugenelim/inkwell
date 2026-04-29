# Spec 11 — Saved Searches as Virtual Folders

## Status
in-progress (CI scope: config-driven sidebar entries shipped post-v0.6.0; DB-backed CRUD, TOML mirror, evaluation cache + background refresh, count badges deferred).

## DoD checklist (mirrored from spec)
- [x] `[[saved_searches]]` TOML config table with `name` + `pattern`.
- [x] Saved searches render in the folders pane below regular folders, under a "Saved Searches" section header (non-selectable).
- [x] ☆ glyph distinguishes saved-search rows from real folders.
- [x] Cursor j/k skips the section header row.
- [x] Enter (or `l`) on a saved-search row runs its pattern via existing `runFilterCmd` (spec 10's filter machinery).
- [x] Selection auto-focuses the list pane.
- [x] Filter cmd-bar reminder appears (`filter: <pattern> · matched N · …`).
- [x] Tests: 3 dispatch cases — saved searches render with ☆, Enter on one fires runFilterCmd + ListPane focus, header is not selectable.
- [ ] Manager API (`internal/savedsearch`) — deferred. v0.7.0 reads directly from config.
- [ ] DB-backed saved_searches table CRUD — deferred.
- [ ] TOML mirror to `~/.config/inkwell/saved_searches.toml` — deferred.
- [ ] Evaluation cache + background count refresh — deferred (sidebar shows no count badge yet).
- [ ] `:savedsearch new/edit/delete` commands — deferred (the user edits config.toml + restarts for v0.7.0).

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

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: Mail.Read (existing).
- [x] Store reads/writes: messages (read via SearchByPredicate from the existing :filter path).
- [x] Graph endpoints: none directly. Pattern eval is local-only in v0.7.0.
- [x] Offline behaviour: saved searches work fully offline against the FTS5 index.
- [x] Undo: N/A (no mutation).
- [x] User errors: pattern parse errors surface via the existing ErrorMsg → status bar.
- [x] Latency budget: pattern compile + store query both <100ms per spec 02 budgets.
- [x] Logs: nothing new logged.
- [x] CLI mode: spec 14 will surface saved searches via `inkwell saved-search list/run`.
- [x] Tests: 3 dispatch tests + the existing filter pipeline tests.
