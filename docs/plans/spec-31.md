# Spec 31 — Focused / Other tab

## Status
in-progress

## DoD checklist

### Configuration
- [ ] `internal/config/config.go` — new `InboxConfig` struct
      with `Split string` (TOML `split`),
      `SplitShowZeroCount bool` (TOML `split_show_zero_count`),
      `SplitDefaultSegment string` (TOML `split_default_segment`).
      Embedded as `Inbox InboxConfig` on the top-level config.
- [ ] `internal/config/defaults.go` `Defaults()` — sets
      `Inbox.Split = "off"`,
      `Inbox.SplitShowZeroCount = false`,
      `Inbox.SplitDefaultSegment = "focused"`.
- [ ] `internal/config/validate.go` — rejects
      `inbox.split` outside `{"off", "focused_other"}` (incl.
      empty); rejects `inbox.split_default_segment` outside
      `{"focused", "other", "none"}` (incl. empty). Friendly error
      strings per spec §10.

### Store
- [ ] `internal/store/messages_inference.go` (new file) —
      `Store.ListMessagesByInferenceClass(ctx, accountID, folderID,
      cls, limit, excludeMuted, excludeScreenedOut) ([]Message,
      error)` and `Store.CountUnreadByInferenceClass(ctx,
      accountID, folderID, cls, excludeMuted, excludeScreenedOut)
      (int, error)` with the SQL from spec §4.2.
- [ ] `cls` validated against `{"focused", "other"}`; reject empty,
      `"both"`, `"none"`, etc., with new typed sentinel
      `ErrInvalidInferenceClass` exposed in
      `internal/store/errors.go`.
- [ ] No new schema migration. Re-confirm
      `ls internal/store/migrations/` immediately before merge —
      latest at design time is `013_bundled_senders.sql`. The
      contingency index `idx_messages_inference_inbox` (spec §3.2)
      is added in a follow-up only if `BenchmarkInboxSubTabList100k`
      regresses.

### UI model
- [ ] `internal/ui/types.go` — `InboxSplit` typed string with
      constants `InboxSplitOff = "off"`,
      `InboxSplitFocusedOther = "focused_other"` (co-located with
      spec 30's `ArchiveLabel`).
- [ ] `internal/ui/app.go` Model fields per spec §5.4:
      `inboxSplit InboxSplit`, `activeInboxSubTab int` (default
      `-1`), `inboxSubTabState [2]listSnapshot`,
      `inboxSubTabUnread [2]int`,
      `inboxSubTabLastFocused [2]time.Time`,
      `inboxTenantHintShown bool`. Multi-account refactor cost
      acknowledged in spec §5.4 / §7 — single-account scaffolding
      for v1.
- [ ] `Model.inboxSplit` threaded once at `ui.New(deps, cfg)` from
      `cfg.Inbox.Split` (typed conversion); never mutated over the
      session (no hot reload, CLAUDE.md §9).

### UI render
- [ ] New file `internal/ui/inbox_split.go` (parallel to spec
      24's `internal/ui/tabs.go`): `renderInboxSubStrip(m Model)
      string`, `cycleInboxSubTab(m Model, dir int) (Model,
      tea.Cmd)`, `loadInboxSubTabCmd(folderID string, segment
      int) tea.Cmd`, `refreshInboxSubTabUnreadCmd(folderID string)
      tea.Cmd`, plus message types `inboxSubTabLoadedMsg` /
      `inboxSubTabUnreadMsg`.
- [ ] List-pane render path enforces preconditions §5.2:
      `cfg.Inbox.Split == "focused_other"` AND Inbox folder
      selected AND no saved-search row selected AND
      `m.activeTab < 0` AND `m.searchActive == false` AND
      `m.filterAllFolders == false`. Folder-scoped `:filter` does
      NOT hide the strip (§5.7).
- [ ] Strip layout per §5.3: 1 row, 2 segments, active /
      inactive Lipgloss styling matching spec 24 §5.1; `•` glyph
      for new mail (segment count rose since
      `inboxSubTabLastFocused[seg]`); `⚠` glyph for query error;
      zero-count rendering governed by
      `cfg.Inbox.SplitShowZeroCount`.

### Keybindings & dispatch
- [ ] Cycle precedence rule §5.5: in `dispatchList` the
      `key.Matches(msg, m.keymap.NextTab)` / `PrevTab` arms check
      `len(m.tabs) > 0` first (spec-24 path); else fall to the
      inbox sub-strip cycle path; else no-op (DEBUG log).
- [ ] Cycle behaviour from `activeInboxSubTab == -1`: the first
      `]` / `[` selects the segment named by
      `cfg.Inbox.SplitDefaultSegment` (`"focused"` → 0, `"other"`
      → 1, `"none"` → no-op). Subsequent presses toggle.
- [ ] Per-segment state preserved across cycles via
      `inboxSubTabState[2]listSnapshot` (cursor / scroll / message
      slice header). Cache TTL reads existing
      `[saved_search].cache_ttl` (no new TTL config key).
- [ ] No new keymap field; no new chord prefix.

### Cmd-bar verbs
- [ ] `:focused` and `:other` cases in `dispatchCommand`
      (`internal/ui/app.go` cmd-bar dispatcher). Both call shared
      `m.activateInboxSubTab(seg int) (Model, tea.Cmd)` helper
      that runs §5.8 steps 1–5 (clear `activeTab` to -1; clear
      saved-search selection; clear filter / search; navigate to
      Inbox folder; activate sub-tab and dispatch
      `loadInboxSubTabCmd`).
- [ ] Off-state error when `cfg.Inbox.Split == "off"`:
      `focused: inbox split is off — set [inbox].split =
      "focused_other" first` (and matching for `:other`).

### Palette
- [ ] Two static rows in `internal/ui/palette_commands.go` under
      a new "Inbox" section heading: `focused_view`
      (Title `Show Focused`, synonyms `["focused", "focus",
      "important"]`, binding `:focused`); `other_view` (Title
      `Show Other`, synonyms `["other", "unimportant",
      "unfocused"]`, binding `:other`). `"clutter"` deliberately
      excluded (§5.9).
- [ ] Both rows expose `Available.Why = "inbox split is off"`
      when `cfg.Inbox.Split == "off"`; rows still rendered
      (greyed) — discoverability over hiding.

### Status bar / one-time hints
- [ ] List-pane status segment appends sub-tab marker per §5.7
      when the strip is rendering (`· sub: Focused · ] / [ to
      cycle`, or `· sub: all (press ] for Focused, [ for Other)`
      from -1, or `· sub: Focused · :focused / :other to switch`
      when spec-24 tabs steal `]` / `[`).
- [ ] One-time tenant-detection hint (§6.2): when the strip first
      renders for the session AND Inbox has ≥ 50 messages AND
      both sub-tab unread counts are zero AND `Folder.UnreadCount`
      for Inbox is non-zero, render the hint
      `focused/other looks empty — your tenant may have Focused
      Inbox off (see [inbox].split docs)`. Dismiss via Esc;
      `Model.inboxTenantHintShown` flag prevents repeat.

### Filter integration
- [ ] `:filter <pattern>` (folder-scoped) AND's the user pattern
      with `(~y focused & ~m Inbox)` or `(~y other & ~m Inbox)` —
      synthesised at the dispatcher; pattern engine handles the
      rest.
- [ ] `:filter --all` suppresses the strip (precondition
      `m.filterAllFolders == false`); sub-tab snapshot preserved
      and restored on `:unfilter`.
- [ ] Spec 28 screener divergence documented: `:filter` paths
      always set `ApplyScreenerFilter = false` (spec 28 §5.4
      contract); sub-tab direct-helper path sets
      `excludeScreenedOut = cfg.Screener.Enabled`. The two paths
      deliberately diverge when screener is enabled — covered by
      `TestFilterOverSubTabBypassesScreenerWhenEnabled`.

### Sync integration
- [ ] Sub-strip badge refresh hooks the existing
      `FolderSyncedEvent` (the same site that drives spec 24's
      `RefreshTabCounts`) and runs both segment counts
      sequentially inside one Cmd. No errgroup fan-out
      (§6.1 rationale).

### CLI
- [ ] `cmd/inkwell/cmd_messages.go` — new `--view <focused|other>`
      flag on `inkwell messages` (§8.1). Plumbs through to the
      existing query path with `~y focused` / `~y other` AND'd
      into the user's pattern. Rejects unknown values (exit 2);
      rejects combination with non-Inbox `--folder` (exit 2).
      `--quiet` semantic for the empty-result tenant warning.

### Tests (per spec §12)

- [ ] **store unit:** `TestListMessagesByInferenceClassFocused`,
      `TestListMessagesByInferenceClassOther`,
      `TestListMessagesByInferenceClassEmptyClassExcluded`,
      `TestListMessagesByInferenceClassExcludeMuted`,
      `TestListMessagesByInferenceClassRejectsInvalidCls`,
      `TestListMessagesByInferenceClassExcludeScreenedOut`,
      `TestCountUnreadByInferenceClassFocused`,
      `TestCountUnreadByInferenceClassOther`,
      `TestCountUnreadByInferenceClassRespectsMute`,
      `TestCountUnreadByInferenceClassRespectsScreener`.
- [ ] **config unit:** `TestInboxSplitDefaultIsOff`,
      `TestInboxSplitAcceptsFocusedOther`,
      `TestInboxSplitRejectsUnknownValue`,
      `TestInboxSplitRejectsEmptyString`,
      `TestInboxSplitDefaultSegmentDefault`,
      `TestInboxSplitDefaultSegmentRejectsUnknown`.
- [ ] **dispatch unit:**
      `TestColonFocusedActivatesFocusedSegment`,
      `TestColonOtherActivatesOtherSegment`,
      `TestColonFocusedFromNonInboxNavigatesToInbox`,
      `TestColonFocusedWhenSplitOffShowsError`,
      `TestColonOtherWhenSplitOffShowsError`,
      `TestNextTabPressCyclesInboxSubStripWhenNoSpec24Tabs`,
      `TestNextTabPressCyclesSpec24WhenTabsConfigured`,
      `TestPrevTabPressOnInboxSubStripFromMinusOneActivatesDefaultSegment`,
      `TestCycleFromMinusOneRespectsSplitDefaultSegmentOther`,
      `TestCycleFromMinusOneRespectsSplitDefaultSegmentNone`,
      `TestNextTabPressInComposeModeDoesNotCycle`,
      `TestNextTabPressInSearchModeDoesNotCycle`,
      `TestSubStripHiddenOnNonInboxFolder`,
      `TestSubStripHiddenWhenSpec24TabActive`,
      `TestSubStripHiddenWhenFilterAllActive`.
- [ ] **dispatch e2e:**
      `TestInboxSubStripRendersWhenSplitFocusedOther`,
      `TestInboxSubStripHiddenWhenSplitOff`,
      `TestInboxSubStripHiddenOnSentFolder`,
      `TestInboxSubStripCycleSelectsFocused`,
      `TestInboxSubStripCycleSelectsOtherThenFocused`,
      `TestInboxSubStripBadgeShowsUnreadCount`,
      `TestInboxSubStripNewMailGlyphAppearsOnInactiveSegment`,
      `TestInboxSubStripStatusBarHintShowsCycleKeys`,
      `TestInboxSubStripStatusBarHintShowsCmdBarVerbsWhenSpec24Active`,
      `TestColonFocusedAndColonOtherE2ENavigates`,
      `TestSubStripPreservesCursorAcrossCycle`,
      `TestSubStripBadgeUpdatesAfterArchive`,
      `TestSubStripBadgeUpdatesAfterFolderSyncedEvent`,
      `TestSubStripWarningGlyphOnCountQueryError`,
      `TestPaletteShowsInboxSection`,
      `TestPaletteSynonymUnfocusedMatchesOther`,
      `TestPaletteSynonymClutterDoesNotMatch`,
      `TestSubStripDisabledHintRendersWhenSplitOff`,
      `TestSubStripFilterAndsWithUserPattern`,
      `TestSubStripFilterAllSuppressesStrip`,
      `TestSubStripDirectAndFilterPathsAgreeOnTrivialPatternNoScreener`,
      `TestFilterOverSubTabBypassesScreenerWhenEnabled`,
      `TestTenantDetectionHintRendersWhenAllZeroAndUnreadNonZero`,
      `TestTenantDetectionHintNotRenderedOnFreshSignIn`,
      `TestTenantDetectionHintDismissedViaEscDoesNotRepeat`,
      `TestSubStripExcludesScreenedOutWhenScreenerEnabled`,
      `TestSubStripIncludesScreenedOutWhenScreenerDisabled`.
- [ ] **bench:** `BenchmarkRenderInboxSubStrip`,
      `BenchmarkInboxSubTabCycleCached`,
      `BenchmarkInboxSubTabList100k`,
      `BenchmarkInboxSubTabCountUnreadCold`,
      `BenchmarkInboxSubTabCountUnreadWarm`.
- [ ] **redaction:**
      `TestInboxSubTabLogsContainNoSubjectOrSender` in
      `internal/ui/inbox_split_redact_test.go`.
- [ ] **CLI:** `TestMessagesViewFocused`,
      `TestMessagesViewOther`,
      `TestMessagesViewRejectsUnknownValue`,
      `TestMessagesViewWithNonInboxFolderErrors`,
      `TestMessagesViewCombinesWithFilter`.

### User docs
- [ ] `docs/CONFIG.md` — new `[inbox]` section (between `[help]`
      and `[keychain]` alphabetical placement) with the three keys
      from spec §10.
- [ ] `docs/user/reference.md` — rows for `:focused`, `:other`,
      the "Inbox" palette section, the three `[inbox]` config
      keys, and the `inkwell messages --view` flag. Existing
      `]` / `[` rows annotated.
- [ ] `docs/user/how-to.md` — new "Show Focused / Other in the
      Inbox" recipe (opt-in, cycle keys, cmd-bar verbs,
      precedence with spec-24, tenant-disabled edge case,
      `:filter` + screener divergence).
- [ ] `docs/user/explanation.md` — paragraph on the read-only
      design choice.
- [ ] `docs/PRD.md` §10 inventory row.
- [ ] `docs/ROADMAP.md` §0 Bucket 4 row 1 status updated at ship
      time; §1.15 backlog heading updated likewise.
- [ ] `README.md` status table row at ship time.
- [ ] PR checklist (CLAUDE.md §11) ticked.

## Perf budgets

| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| Inbox sub-strip render | <2ms p95 | — | `BenchmarkRenderInboxSubStrip` | not run |
| Sub-tab cycle, cached | <16ms p95 | — | `BenchmarkInboxSubTabCycleCached` | not run |
| Sub-tab cycle, cache miss | <100ms p95 | — | `BenchmarkInboxSubTabList100k` | not run |
| Badge refresh, both segments cold | <200ms p95 (combined) | — | `BenchmarkInboxSubTabCountUnreadCold` | not run |
| Badge refresh, warm | <10ms p95 | — | `BenchmarkInboxSubTabCountUnreadWarm` | not run |

If `BenchmarkInboxSubTabList100k` exceeds 100ms p95, the
contingency index `idx_messages_inference_inbox(folder_id,
inference_class, received_at DESC)` is added in a follow-up
migration and re-baselined.

## Iteration log

### Iter 0 — 2026-05-09 (spec authored)

- Slice: spec authoring + adversarial review loops.
- Output: `docs/specs/31-focused-other-tab.md`,
  `docs/plans/spec-31.md`.
- Adversarial review pass 1 found 20 findings (1 BLOCKER:
  `:filter` + sub-strip precondition contradiction; 5 MAJOR:
  saved-search transition undefined, cold-start cycle behaviour
  inconsistent across 3 sites, screener interaction unspecified,
  pattern-engine vs direct-helper claim, "two strips co-exist"
  prose ambiguity; 14 MINOR / NIT). All addressed.
- Adversarial review pass 2 found 4 findings (3 BLOCKER:
  DoD-vs-§5.4 model-fields drift, false equivalence claim
  against shipped spec 28 §5.4 ApplyScreenerFilter contract,
  three-way `clutter` synonym contradiction; 1 MAJOR: test name
  contradicting §5.9). All addressed; the screener-divergence
  was corrected (sub-tab direct helper applies
  `excludeScreenedOut = cfg.Screener.Enabled`; `:filter` path
  always sets `ApplyScreenerFilter = false` per spec 28 §5.4
  contract; the divergence is a documented design choice and
  covered by `TestFilterOverSubTabBypassesScreenerWhenEnabled`).
- Adversarial review pass 3 found 1 BLOCKER: fictional index
  name `idx_messages_inbox_unread_received` cited in §3.2 / §4.2
  / §6.1 / §9. The actual indexes (per `001_initial.sql:66-71`)
  are `idx_messages_folder_received` and the partial
  `idx_messages_unread`. Spec corrected to cite the real indexes
  with the planner reasoning grounded in their actual column
  shape; contingency index renamed and column order corrected to
  lead on `folder_id` matching the existing convention.
- Result: spec ready for implementation; no further blockers.
- Next: implementation slice when scheduled.
