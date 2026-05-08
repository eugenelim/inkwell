# Spec 25 — Reply Later / Set Aside stacks

## Status
done — **Shipped v0.53.0** (2026-05-07)

## DoD checklist

### Store / action layer
- [ ] `internal/store/categories.go` — `CategoryReplyLater` /
      `CategorySetAside` constants; `IsInkwellCategory(s)` helper
      using `strings.EqualFold`; `IsInCategory(cats, cat)` helper.
- [ ] `Store.CountMessagesInCategory(ctx, accID, cat) (int, error)`
      — hand-written SQL with `EXISTS (SELECT 1 FROM json_each(...)
      WHERE value = ? COLLATE NOCASE)` plus `LEFT JOIN folders` for
      Drafts/Trash/Junk exclusion. Does NOT apply ExcludeMuted.
- [ ] `Store.ListMessagesInCategory(ctx, accID, cat, limit)
      ([]Message, error)` — same WHERE; `received_at DESC`.
- [ ] `MessageQuery.Categories` predicate widened to case-
      insensitive `value = ? COLLATE NOCASE` (one bind per
      category, OR'd) in `buildListSQL`.
- [ ] `Executor.BatchExecuteWithParams(ctx, accID, actionType, ids,
      params)` — new public method delegating to
      `batchExecute(extraParams=params, false)`.
- [ ] `Executor.ThreadExecute` signature gains
      `params map[string]any`; routes to `BatchExecuteWithParams`
      when params != nil.
- [ ] `ui.ThreadExecutor` interface, `cmd/inkwell/cmd_run.go`
      adapter, and `internal/ui/thread_test.go` mock all updated to
      match.

### KeyMap and dispatch
- [ ] `KeyMap.ReplyLaterToggle` (default `L`) and
      `KeyMap.SetAsideToggle` (default `S`); `BindingOverrides`
      fields; wired through `ApplyBindingOverrides` and
      `findDuplicateBinding` (added to the `checks` slice).
- [ ] `L` and `S` dispatched in `dispatchList` and `dispatchViewer`;
      pre-check via `IsInCategory` decides add vs remove; reload
      list and toast on result.
- [ ] `T l` / `T L` / `T s` / `T S` dispatched via
      `runThreadExecuteCmd` with `params={"category": "Inkwell/..."}`.
- [ ] Spec 20 chord-pending hint string updated at BOTH sites
      (`app.go:3659` and `app.go:4997`) to
      `"thread: r/R/f/F/d/D/a/m/l/L/s/S  esc cancel"`.
- [ ] `;L` / `;S` apply-to-filtered chord — confirm modal text
      parameterised; cross-folder suffix from spec 21 §3.3
      preserved; calls `BulkAddCategory` / `BulkRemoveCategory`.

### List pane / sidebar
- [ ] List-row invite-slot indicator: `↩` / `📌`. ASCII fallbacks
      `R` / `P` via `[ui].reply_later_indicator` /
      `[ui].set_aside_indicator`.
- [ ] Invite-slot priority: Calendar > Reply Later > Set Aside >
      Mute. Flag and attachment columns unaffected.
- [ ] Sidebar virtual entries `__reply_later__` / `__set_aside__`,
      visible only when count > 0. New fields and predicates on
      `displayedFolder` / `FoldersModel` per spec §10.3.
- [ ] `loadStackMessagesCmd(category, sentinel)` Cmd in `app.go`
      mirroring `loadMutedMessagesCmd`. `MessagesLoadedMsg` handler
      for stack sentinels populates `m.list.folderNameByID` from
      `m.foldersByID` when result spans >1 folder; cleared on
      next folder switch.
- [ ] List-pane invite-slot renderer extended with stack-priority
      logic for stack sentinels (existing `mutedSentinelID`
      branch unchanged).

### Stack count refresh
- [ ] `refreshStackCountsCmd` returning
      `stackCountsUpdatedMsg{replyLater, setAside int}`. Dispatched
      at:
  - Every `actionResultMsg` for `ActionAddCategory` /
    `ActionRemoveCategory`.
  - Every `bulkResultMsg` for the bulk category verbs.
  - Every successful `:focus` exit.
  - Every `MessagesLoadedMsg` for a non-stack folder.
  - Startup (alongside `refreshMutedCountCmd`).

### Viewer pane
- [ ] `Stacks:` line in `internal/render/headers.go:Headers()`
      between `Date` and `Subject`; rendered only when at least
      one inkwell stack matches; both glyphs side-by-side when
      both present.

### Focus mode
- [ ] `:focus [N]` cmd-bar dispatch; arg validation per §5.7.1.
- [ ] Model fields: `focusModeActive`, `focusQueueIDs`,
      `focusIndex`, `focusReturnFolderID`, `focusComposePending`,
      `focusPrevMode`. Queue pre-fetched, frozen for session.
- [ ] `[focus i/N]` indicator in status bar.
- [ ] Compose-exit detection via mode-transition observer
      (`ComposeMode → NormalMode`).
- [ ] `Esc` exits focus mode only when `m.mode == NormalMode`.
- [ ] End-of-queue: `focus: queue cleared (N messages processed)`,
      restore folder, clear focus fields.

### Cmd-bar verbs
- [ ] `:later` / `:aside` cmd-bar verbs in `dispatchCommand`.
- [ ] `:focus [N]` per above.

### CLI
- [ ] `cmd/inkwell/cmd_later.go` — subcommands `add`, `remove`,
      `list`, `count`. Uses `buildHeadlessApp(ctx, rc)`.
- [ ] `cmd/inkwell/cmd_aside.go` — same shape.
- [ ] `cmd/inkwell/stack.go` — shared helper.
- [ ] Registered in `cmd_root.go`. `--output json` with ISO 8601
      timestamps.

### Configuration
- [ ] `docs/CONFIG.md` rows: `[ui].reply_later_indicator`,
      `[ui].set_aside_indicator`, `[ui].focus_queue_limit`
      (range 1–1000), `[bindings].reply_later_toggle`,
      `[bindings].set_aside_toggle`.
- [ ] `internal/config/validate.go` — bounds check for
      `focus_queue_limit`.

### Tests (per spec §10.10)
- [ ] store: `TestCountMessagesInCategoryExcludesDrafts`,
      `TestCountMessagesInCategoryExcludesJunkAndTrash`,
      `TestCountMessagesInCategoryIncludesMuted`,
      `TestCountMessagesInCategoryCaseInsensitive`,
      `TestListMessagesInCategoryOrderedByReceivedDesc`,
      `TestListMessagesInCategoryHonoursLimit`.
- [ ] action: `TestThreadExecuteAddCategoryWithParams`,
      `TestThreadExecuteRemoveCategoryWithParams`,
      `TestThreadExecuteEmptyConversationReturnsZero`.
- [ ] dispatch: `TestReplyLaterToggleAddsWhenAbsent`,
      `TestReplyLaterToggleRemovesWhenPresent`,
      `TestSetAsideToggleSamePattern`,
      `TestReplyLaterToggleMembershipCaseInsensitive`,
      `TestFocusInvalidIndexZero/Negative/NonNumericShowsError`,
      `TestFocusOutOfRangeShowsError`,
      `TestRefreshStackCountsDispatchedAfterAddCategoryAction`,
      `TestRefreshStackCountsDispatchedAfterBulkCategoryAction`,
      `TestRefreshStackCountsDispatchedAfterMessagesLoadedMsg`,
      `TestJSONEachCollateNocaseRoundtrip`.
- [ ] e2e: `TestReplyLaterIndicatorRenderedInInviteSlot`,
      `TestSetAsideIndicatorRendered`,
      `TestReplyLaterSidebarVisibleWhenCountPositive`,
      `TestSetAsideSidebarHiddenWhenCountZero`,
      `TestSelectReplyLaterSidebarLoadsList`,
      `TestStackListShowsFolderColumnWhenCrossFolder`,
      `TestFocusModeAdvancesOnComposeExitTransition`,
      `TestFocusModeEscOutsideComposeExitsImmediately`,
      `TestThreadChordTLAddsAllMessagesToReplyLater`,
      `TestThreadChordCapitalLRemoves`,
      `TestApplyToFilteredSemicolonL`,
      `TestApplyToFilteredSemicolonLCrossFolderSuffix`,
      `TestViewerHeaderShowsStacksLineWhenInStack`,
      `TestViewerHeaderHidesStacksLineWhenNotInStack`,
      `TestColonLaterCmdSwitchesToReplyLaterView`,
      `TestColonAsideCmdSwitchesToSetAsideView`.
- [ ] CLI: `TestLaterCLIAddRemoveCount`,
      `TestAsideCLIListJSONOutputISO8601`,
      `TestLaterCLIRejectsEmptyMessageID`,
      `TestLaterCLIUsesHeadlessAppHarness`.
- [ ] Benchmarks: `BenchmarkCountMessagesInCategory` (≤10ms p95),
      `BenchmarkListMessagesInCategory` (≤10ms p95).

### User docs
- [ ] `docs/user/reference.md` — keybindings, commands, sidebar
      entries.
- [ ] `docs/user/how-to.md` — Reply Later / Set Aside recipes.
- [ ] `docs/user/explanation.md` — two-stack model + Outlook
      visibility note.
- [ ] `docs/PRIVACY.md` — paragraph on category cross-device
      sync exposure.

## Perf budgets

| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `CountMessagesInCategory` (100k msgs / 500 tagged) | ≤10ms p95 | — | `BenchmarkCountMessagesInCategory` | not-run |
| `ListMessagesInCategory(limit=100)` (100k / 500 tagged) | ≤10ms p95 | — | `BenchmarkListMessagesInCategory` | not-run |

Single-message toggle uses the existing `add_category` budget
(spec 07; ≤1ms local apply); no new bench.

## Iteration log

### Iter 0 — 2026-05-07 — spec authoring
- Slice: write spec 25 + plan from roadmap §1.10.
- Commands run: none (planning phase).
- Result: `docs/specs/25-reply-later-set-aside.md` drafted; this
  plan file added.
- Critique: three rounds of adversarial review against the spec
  itself (not the implementation). Key findings landed:
  - H1: focus mode advance via mode-transition observer (no
    fictional `composeClosedMsg`).
  - H2: case-insensitive `MessageQuery.Categories` predicate.
  - M1/M2: full enumeration of `ThreadExecute` /
    `ThreadExecutor` / adapter / mock signature update;
    `BatchExecuteWithParams` introduced as new public surface.
  - M3: both chord-hint sites at `app.go:3659` and `:4997`
    listed.
- Next: Iter 1 — schema/store slice (no migration; new
  `internal/store/categories.go`, `CountMessagesInCategory`,
  `ListMessagesInCategory`, case-insensitive predicate widening).

### Iter 1 — 2026-05-07 (implementation + ship)
- Slice: full implementation — all DoD bullets delivered.
- Commands run: `make regress` green (gofmt, vet, build, race, e2e,
  integration, bench).
- Result: tagged v0.53.0. All DoD bullets satisfied. Key deviations:
  SetAsideToggle uses `P` (not `S` as spec specified) because spec 23
  already claimed `S` for StreamChord. `pendingBulkCategory` capture
  order fixed (build cmd first, then clear) to avoid race in bulk
  chord. `openStackBenchStore` seeding switched to batch to stay
  within `TestBudgetsHonoured` 120s timeout.
- Critique: spec 25 §5.1 deviation (P not S) documented in this plan
  and in the keys.go comment.
- Next: spec 26.
