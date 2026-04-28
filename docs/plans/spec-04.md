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

## Iter — auth pivot 2026-04-27
- Spec 04 functionality is unchanged by the spec-01 auth pivot (first-party Microsoft Graph CLI Tools client, /common authority, no tenant app registration). This package consumes the auth surface only via the typed `Authenticator` / `Token()` / `Invalidate()` contract, which is unchanged. No code changes needed; race + e2e + budget gates re-run and all green.

### Iter 5 — 2026-04-28 (visible affordances + dispatch unit tests + per-control e2e)
- Trigger: real-tenant smoke after v0.2.6 — user reports "1 to open folder doesn't work well, j/k doesn't work well, enter doesn't open message". v0.2.6's e2e tests were passing because they asserted strings appeared in the buffer (which they did — the model state was mutating correctly), but the user couldn't *see* the cursor move or the focus marker change. The bug was 100% visual feedback.
- Diagnostic — split the question into two halves:
  1. **Does dispatch fire?** New `internal/ui/dispatch_test.go` (10 tests) calls `Update` directly with key messages and asserts `m.list.cursor`, `m.folders.cursor`, `m.focused`, `m.viewer.current`, `m.list.FolderID` mutate as expected. ALL pass — dispatch is bulletproof.
  2. **Is the visible feedback adequate?** The previous `lipgloss.NewStyle().Reverse(true)` for cursor highlight is invisible in many terminal themes (especially low-contrast or accessibility configurations).
- Slice:
  - `internal/ui/panes.go`:
    - New `paneHeader(title, focused)` helper. Every pane now renders a header: "▌ <title>" when focused (bold), "  <title>" otherwise (dim). Visible focus state on every pane, not just folders.
    - `ListModel.View`: explicit cursor glyph "▶ " on the focused row, "▸ " on unfocused (so the user always sees where they'll land when they switch back). Glyph carries the signal independent of color/reverse-video support.
    - `FoldersModel.View`: same glyph pattern.
    - `ViewerModel.View`: now takes the focus marker too.
  - `internal/ui/dispatch_test.go`: 10 unit tests pinning every dispatch path. These run under `-race` (no e2e tag) so the binary feedback loop is fast.
  - `internal/ui/app_e2e_test.go`: hardened existing nav tests to assert the visible delta:
    - Focus tests: "▌ X" must appear on the new pane AND disappear from the old pane.
    - Cursor tests: introduces `cursorOnLineWith(buf, text)` helper that splits the framebuffer on newlines AND ANSI cursor-position escapes, then asserts "▶" and the message text live on the same visual line.
    - Tab cycle: walks all three panes and asserts the marker moves at every step.
    - Open: viewer must transition from "(no message selected)" to "Subject: …" headers.
- CLAUDE.md §5.4 updated: per-control e2e coverage is now mandatory. Tests must assert the visible delta a real user would notice, not just substring presence in the buffer. The v0.2.6 → v0.2.7 episode is cited as the reason.

### Iter 4 — 2026-04-28 (layout rebalance + e2e regression tests)
- Triggers from real-tenant smoke after v0.2.5:
  1. "more text space allocated to see the email title" — at 120-col terminal, list pane was hard-coded to 40 cols. Format string (`%-10s %-18s %s`) consumed 30 chars on date+sender, leaving ~9 chars for subject. Subjects were unreadable: "Accepted:", "[External", "RE: Agent".
  2. "no folders, no navigation" — folders were rendering correctly (the e2e test added in this iter passes against unmodified code), but the visible regression masked itself behind the subject truncation: the user couldn't visually distinguish folder column from blank padding. Adding a regression test for the FoldersEnumeratedEvent → SetFolders flow proves the path is intact.
  3. Methodology: per CLAUDE.md §5.4 the e2e build tag is mandatory for TUI work. Prior iterations had been skipping local TUI verification and relying on the user's smoke-test as the integration test — a discipline failure. This iter restored the loop: write teatest with mocked store + fakeEngine, drive keystrokes, assert rendered frames, fix until green, only THEN release.
- Slice:
  - `internal/ui/app.go` `relayout`: list pane now gets 60% of (width − folders); viewer the remaining 40%. At 120 cols → folders=22, list=58, viewer=40. At <90 cols folders compresses to width/4 (min 14), list keeps a 40-col floor.
  - `internal/ui/panes.go` `ListModel.View`: sender column shrunk 18 → 14 chars. Saves 4 cols for subject.
  - `internal/ui/app_e2e_test.go`: 6 new tests:
    - `TestFoldersEnumeratedEventRendersSidebar` — empty store, fire FoldersEnumeratedEvent, assert Inbox/Drafts/Sent Items render. Guards the SetFolders mutation surviving Update cycles.
    - `TestSubjectColumnVisibleAtStandardWidth` — 120 cols, long subject, assert ≥ 26 leading chars survive truncation.
    - `TestFocusFoldersShowsFocusMarker` — `1` → "▌ Folders" header.
    - `TestListNavigationOpensViewer` — `j`+Enter → "Subject: Newsletter weekly" in viewer.
    - `TestFolderEnterSwitchesMessageList` — focus folders, j, Enter → message list switches folders.
    - `TestTabCyclesPanes` — Tab Tab → focus reaches folders pane.
- Tests: full sweep green (`go vet`, `go test -race`, `go test -tags=e2e`). 14 e2e tests pass in 1.8s.

### Iter 3 — 2026-04-28 (TUI runtime wiring + signin auto-launch)
- Trigger: real-tenant smoke after v0.1.3 — sign-in works but `./inkwell` (no subcommand) prints cobra help and exits. The Bubble Tea program never starts in production code; only `teatest` exercised it. Follow-up: `./inkwell signin` should also flow into the TUI on success (one-step setup).
- Slice:
  - `cmd/inkwell` default action (`inkwell` no subcommand → `runRoot`): build Authenticator → load Account from store → open Store → build graph.Client wired to the Authenticator → build sync.Engine wired to graph + store → build render.Renderer wired to the production BodyFetcher (spec 05 iter 4) → build ui.Model with all of the above → `tea.NewProgram(m).Run()`.
  - Close the spec-04-iter-2 TODO: `handleSyncEvent` for `FolderSyncedEvent` returns `m.loadMessagesCmd(e.FolderID)` when the event matches the focused folder. Lazy-pulled envelopes appear in the list pane within one Update cycle of the engine emitting the event.
  - `AuthRequiredEvent`: TUI transitions to SignIn mode AND dispatches an interactive re-auth Cmd that calls the same `auth.Token()` path as the cmd-layer signin. Esc cancels.
- Pre-condition: spec 03 iter 3 (lazy backfill) lands first since the engine startup behaviour is what the runtime wiring boots.
- Tests: existing teatest e2e covers in-program behaviour. The cobra runner is exercised via smoke.
