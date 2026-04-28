# Spec 04 — TUI Shell

## Status
done (CI scope) — visual polish + viewer body filling deferred to spec 05; manual TUI smoke deferred per CLAUDE.md §5.5.

## DoD checklist
- [x] App launches; three panes render (folders / list / viewer-stub).
- [x] Status bar shows account UPN + last-sync timestamp + throttled banner.
- [x] Mode dispatch in root Update: Normal / Command / Search / SignIn / Confirm.
- [x] `:` activates command bar; `q` (Normal) and Ctrl+C (anywhere) quit.
- [x] `:quit`, `:sync`, `:signin`, `:signout` wired (sync triggers engine.SyncAll; signout enters confirm modal).
- [x] `Ctrl+R` triggers `engine.SyncAll`; verified via `e2e` test counter.
- [x] `1`/`2`/`3` focus folders/list/viewer; `Tab`/`Shift+Tab` cycle.
- [x] Sync events consumed via long-running `tea.Cmd` re-arming after each receive — UI never blocks on the channel.
- [x] `WindowSizeMsg` re-layouts; cramped widths shrink folders + list to fit.
- [x] Confirm modal y/N flows route a `ConfirmResultMsg`.
- [x] SignIn modal renders with code + URL placeholders; `Esc` cancels.
- [x] Sub-models are value types (no pointer aliasing across Update cycles); CLAUDE.md §4 honoured.
- [x] Default folder pick prefers `wellKnownName=inbox` over alphabetical first.
- [x] Privacy: panes never render raw addresses other than the user's own UPN in the status bar; the rendering helpers route through theme styles only (no inline ANSI).
- [ ] **Deferred to spec 05:** viewer pane fills with rendered headers / body / attachments.
- [ ] **Deferred to spec 06:** `/` search prompt actual query semantics.
- [ ] **Deferred to spec 07:** triage actions wired to the keymap (the bindings exist; the dispatchers are stubs).

## Iteration log

### Iter 1 — 2026-04-27
- Slice: keymap, theme, types (Pane / Mode / messages), root Model, Update with mode dispatch, sub-models for folders / list / viewer-stub / command / status / signin / confirm.
- Files: internal/ui/{keys,theme,messages,app,panes}.go.
- Compile: clean after `go mod tidy` pulled charm deps.

### Iter 2 — 2026-04-27
- Slice: e2e tests via `teatest`. Six scenarios: boot renders three panes, `:quit` exits, Ctrl+R kicks SyncAll, sync event updates status bar, focus switching, resize, unknown command no-crash.
- Initial run: two failures.
  - **Critique 1**: TestBootRendersThreePanesAndStatusBar asserted `"Q4 forecast"` but the list pane truncates subjects to its 40-char width. Real on-screen text is `"Q4 foreca"`. Tightened the assertion to the truncated form.
  - **Critique 2**: Default-folder picker selected alphabetically-first folder (Archive). For inbox-by-default UX, added wellKnownName=inbox preference + `FoldersModel.SelectByID` to keep the cursor in sync.
- After fixes: all six e2e tests green. Whole-tree race + e2e sweep clean.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes: none directly — UI consumes data already cached by store.
- [x] Store reads: `ListFolders`, `ListMessages` only. No writes from this spec (writes land in spec 07).
- [x] Graph endpoints: none directly. SyncAll routes through the engine.
- [x] Offline behaviour: UI reads from store; offline is transparent (the engine handles 401 / network).
- [x] Undo: keymap exists, dispatcher is a stub for spec 07.
- [x] User-facing errors: `ErrorMsg` parks `lastError` in the model — surfaced by future status-bar work in spec 05.
- [x] Latency: cold-start budget (<500ms) NOT yet benchmarked; needs spec 02 helpers + a teatest cold-start measurement. Deferred to a follow-up bench iteration before spec 07.
- [x] Logs: UI takes a redacting `*slog.Logger` via Deps; `New` panics if Logger is nil to prevent silent default fallback.
- [x] CLI mode: spec 14.
- [x] Tests: e2e covers boot, quit, refresh, status updates, focus, resize, error paths.

## Notes for follow-up specs
- Spec 05 (rendering) replaces `ViewerModel.View`'s "(spec 05 will render the body here)" stub with real headers / body conversion / attachments / numbered links. The Renderer interface lives in `internal/render/` and is consumed by the viewer.
- Spec 07 (triage) fills `dispatchList` / `dispatchViewer` action handlers (currently no-ops for `r/R/f/d/D/a/m/c/C`).
- Spec 06 (search) wires the `/` prompt to the FTS5 store search.
- The cold-start performance budget (<500ms to first paint with 100k cached) is currently unmeasured. Add `BenchmarkColdStartFirstFrame` to internal/ui/ when spec 07 lands or sooner if regressions surface.
