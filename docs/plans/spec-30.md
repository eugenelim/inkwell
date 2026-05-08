# Spec 30 — "Done" alias for archive

## Status
not-started

## DoD checklist

### Keymap and pane-scoping
- [ ] `internal/ui/keys.go:209` — `Archive` default updated to
      `key.NewBinding(key.WithKeys("a", "e"))`. No `WithHelp` to
      remove.
- [ ] `internal/config/defaults.go:90` — `Archive: "a"` updated to
      `Archive: "a,e"` (mirrors the `Up: "k,up"` comma-separated
      pattern at lines 76-79). Without this change the bootstrap
      decode silently overwrites the new keymap default.
- [ ] `internal/ui/app.go:5563-5566` — viewer-pane `e`
      quote-toggle alternative deleted; `Q` at `:5559-5562`
      remains canonical.
- [ ] `internal/ui/dispatch_test.go` — `TestEKeyTogglesQuoteExpansion`
      (around line 5022) deleted in same commit.
- [ ] `internal/ui/app.go:3927-3930` — folders-pane `e` rule-edit
      preserved (no code change). Pane-scoping rule documented in
      spec §3.1.

### Branding helper
- [ ] `internal/ui/labels.go` (new file): `ArchiveLabel` typed
      string, `ArchiveLabelArchive` / `ArchiveLabelDone` constants;
      `archiveVerbLower(label)`, `archiveVerbTitle(label)`,
      `archiveVerbForName(name, label)`,
      `archiveVerbTitleForName(name, label)`.
- [ ] `internal/config/config.go` `UIConfig` gains
      `ArchiveLabel string` (TOML `archive_label`).
- [ ] `internal/config/defaults.go` `Defaults()` sets
      `UI.ArchiveLabel = "archive"`.
- [ ] `internal/config/validate.go` rejects values other than
      `"archive"` / `"done"` (and rejects empty).
- [ ] `Model.archiveLabel ArchiveLabel` field; threaded once at
      `ui.New(deps, cfg)`.
- [ ] No `WithArchiveLabel` mutator on `KeyMap`. Label flows
      through `m.archiveLabel` to format-time helpers.

### Surface updates
- [ ] `triageDoneMsg` success format at `app.go:1974` and `:1986`
      → `archiveVerbForName(msg.name, m.archiveLabel)`.
- [ ] `triageDoneMsg` failure format at `app.go:1950` → same.
- [ ] `bulkResultMsg` and `threadResultMsg` toasts route through
      `archiveVerbForName` / `archiveVerbTitleForName`.
- [ ] `confirmBulk` modal text (`app.go:3713-3776`) uses
      `archiveVerbTitle` for the verb segment when verb ==
      `"archive"`. Cross-folder suffix / pluralisation preserved.
- [ ] Filter status bar `app.go:6224` — `;a archive` segment
      becomes `;a <archiveVerbLower(label)>`.
- [ ] Bulk pending hint `app.go:6226` — `a (archive)` becomes
      `a (<archiveVerbLower(label)>)`.
- [ ] `palette_commands.go:436` AND `palette_commands.go:441` —
      same bulk hint string updated.
- [ ] List pane key hint `app.go:6286` — `{"a", "archive"}` →
      `{"a", archiveVerbLower(label)}`.
- [ ] Viewer pane key hint `app.go:6288` — same.
- [ ] Fullscreen body hint `app.go:6112` — `a  archive` →
      `a  <archiveVerbLower(label)>`.
- [ ] Palette single-message row title `palette_commands.go:109`
      becomes dynamic: `Archive message` ↔ `Mark done`.
- [ ] Palette thread row title `palette_commands.go:269` becomes
      dynamic: `Archive thread` ↔ `Mark thread done`.
- [ ] Palette single-message synonyms expand from
      `["done","file"]` to `["done","file","archive"]`.
- [ ] Palette thread row gains
      `Synonyms: []string{"done","file","archive"}`.
- [ ] Palette `Available.Why` for both rows uses
      `archiveVerbLower(m.archiveLabel)`.
- [ ] Existing `internal/ui/palette_test.go` fixture updated to
      match new defaults.
- [ ] Help overlay (`internal/ui/help.go:84`) —
      `buildHelpSections(km, archiveLabel)` gains a parameter; the
      Archive row's description becomes
      `archiveVerbLower(archiveLabel)`. Every existing caller
      updated in same commit.

### Cmd-bar verbs
- [ ] `:archive` and `:done` cases in `dispatchCommand`. Both
      call shared `m.runArchiveOnFocused()` helper.
- [ ] Empty-list error path: `<verb>: no message focused`.

### Thread chord
- [ ] `app.go:4152-4153` (list) `case "a":` → `case "a", "e":`.
- [ ] `app.go:5618-5619` (viewer) `case "a":` → `case "a", "e":`.
- [ ] Chord-pending hint at `app.go:4129` and `app.go:5595`:
      `r/R/f/F/d/D/a/m/l/L/s/S` → `r/R/f/F/d/D/a/e/m/l/L/s/S`.

### Apply-to-filtered chord
- [ ] `app.go:4035-4036` `case "a":` → `case "a", "e":`. Dispatch
      payload (`m.confirmBulk("archive", len(m.filterIDs))`)
      unchanged.
- [ ] Confirm modal verb passes through `archiveVerbTitle`.

### CLI
- [ ] `cmd/inkwell/cmd_thread.go:newThreadArchiveCmd` gains
      `Aliases: []string{"done"}` and `Short` updated.

### Configuration
- [ ] `docs/CONFIG.md` `[ui]` row for `archive_label`.
- [ ] `docs/CONFIG.md` `[bindings].archive` row updated to mention
      `"a,e"` default and the label-config interaction.

### Tests
- [ ] **unit (config)**: `TestArchiveLabelDefaultIsArchive`,
      `TestArchiveLabelAcceptsDone`,
      `TestArchiveLabelRejectsUnknownValue`,
      `TestArchiveLabelEmptyStringRejected`.
- [ ] **unit (ui/labels)**: `TestArchiveVerbLowerArchive`,
      `TestArchiveVerbLowerDone`,
      `TestArchiveVerbTitleArchive`, `TestArchiveVerbTitleDone`,
      `TestArchiveVerbForNameOnlyTouchesArchive`.
- [ ] **unit (keys)**: `TestDefaultArchiveBindsAandE`,
      `TestDefaultsBootstrapPreservesAandE`,
      `TestArchiveOverrideAOnlyDropsE`,
      `TestArchiveOverrideEOnlyDropsA`,
      `TestFindDuplicateBindingDetectsArchiveCollision`.
- [ ] **unit (help)**:
      `TestHelpOverlayArchiveRowReadsArchiveByDefault`,
      `TestHelpOverlayArchiveRowReadsDoneWhenLabelDone`.
- [ ] **dispatch**: `TestKeyEArchivesFromList`,
      `TestKeyEArchivesFromViewer`,
      `TestKeyEDoesNothingInComposeMode`,
      `TestFoldersPaneEStillEditsSavedSearchRule`,
      `TestViewerEDoesNotToggleQuotes`,
      `TestThreadChordTeArchivesThread`,
      `TestSemicolonEArchivesFiltered`,
      `TestColonDoneArchivesFocused`,
      `TestColonArchiveSamePathAsColonDone`,
      `TestColonDoneOnEmptyListShowsError`.
- [ ] **dispatch (branding)**:
      `TestArchiveToastReadsArchiveWhenLabelArchive`,
      `TestArchiveToastReadsDoneWhenLabelDone`,
      `TestArchiveFailureToastReadsDoneWhenLabelDone`,
      `TestBulkConfirmModalUsesConfiguredVerb`,
      `TestThreadConfirmReadsMarkThreadDoneWhenLabelDone`,
      `TestPaletteArchiveRowTitleSwitchesOnLabel`,
      `TestPaletteArchiveSynonymMatchesArchiveAndDoneRegardlessOfLabel`.
- [ ] **e2e**: `TestPressingEArchivesFocusedMessage`,
      `TestColonDoneArchivesFocusedMessage`,
      `TestArchiveToastBrandedDoneWithDoneLabel`,
      `TestThreadChordTEArchivesThread`,
      `TestChordPendingHintShowsAEGlyphs`,
      `TestPaletteShowsBindingAandE`,
      `TestPaletteThreadArchiveSynonymsIncludeArchive`,
      `TestPaletteBulkPendingHintBranded`,
      `TestHelpOverlayShowsDoneLabelWhenConfigured`.
- [ ] **CLI**: `TestThreadDoneAliasInvokesArchive`,
      `TestThreadHelpListsDoneAlias`.

### User docs
- [ ] `docs/user/reference.md` — rows for `e`, `:archive`, `:done`,
      `T e`, `;e`, `inkwell thread done`. Existing rows annotated.
- [ ] `docs/user/how-to.md` — short triage paragraph.
- [ ] `docs/user/explanation.md` — "archive vs done" framing.
- [ ] `docs/PRD.md` §10 inventory row.
- [ ] `docs/ROADMAP.md` §0 Bucket 3 / §1.23 status updated at
      ship time.
- [ ] `README.md` status table row at ship time.
- [ ] PR checklist (CLAUDE.md §11) ticked.

## Perf budgets

No new perf budget. Spec 30 introduces no new SQL path, no new
Graph call, no new render-loop allocation. The branding helpers
(`archiveVerbLower` / `archiveVerbTitle` / `archiveVerbForName`) are
two-arm switches on a typed string with no heap allocation. The
single-message archive path's spec 07 budget (local apply ≤ 1ms;
batched dispatch ≤ 50ms p95 per 100-message batch) is unchanged.

| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| (no new budgets) | — | — | — | — |

## Iteration log

### Iter 0 — 2026-05-08 — spec authoring
- Slice: write spec 30 + plan from roadmap §1.23.
- Commands run: none (planning phase). Two adversarial review
  rounds against the spec itself.
- Result: `docs/specs/30-done-alias.md` drafted; this plan file
  added.
- Critique:
  - Round 1 (18 findings, 5 HIGH):
    - H1: `e` already bound in viewer (quote-toggle alt at
      `app.go:5563-5566`) — resolved by deleting the alt + the
      `TestEKeyTogglesQuoteExpansion` test.
    - H2: `e` already bound in folders pane (rule-edit at
      `app.go:3927-3930`) — preserved as new pane-scoping rule;
      regression test added.
    - H3: `defaults.go:90` `Archive: "a"` would silently
      overwrite the keymap default — fixed by updating to
      `Archive: "a,e"` and adding `TestDefaultsBootstrapPreservesAandE`.
    - H4: help overlay is hardcoded at `help.go:84`, not
      `key.Help()`-driven — fixed by plumbing `archiveLabel`
      into `buildHelpSections(km, archiveLabel)`.
    - H5: toast format is `"✓ <name> · u to undo"` and
      `"<name>: <err>"`, not the invented `"✓ Archived"` /
      `"failed to archive"` — fixed; spec preserves existing
      convention and only branches the `<verb>` token.
    - Plus 4 missed surface sites enumerated, line-number
      drift in chord dispatch arms vs entries split, palette
      synonym asymmetry recorded, drafts edge case added,
      Bucket 3 ordering claim corrected, plan-file DoD bullet
      added.
  - Round 2 (1 HIGH, 4 MED, 3 LOW, 4 NIT):
    - H1: `findDuplicateBinding` allowlist scope clarified.
    - M1: §4.3 row 1 stray "third write" claim removed.
    - M2: dead `internal/config/ui.go` reference removed.
    - M3: `WithArchiveLabel` helper removed; label is a model
      field only.
    - M4: `UIArchiveLabel` renamed to `ArchiveLabel`
      consistently.
    - L1: list/viewer site labels in §4.3 corrected.
    - L2: test name `TestEKeyTogglesQuoteExpansion` confirmed.
    - L3: palette fixture update bullet added.
    - N2: `archiveVerbTitleForName` added explicitly for
      bulk/thread title-cased toasts.
  - Final consistency sweep (round 3): minor tone/labelling
    fixes; spec passes section numbering, helper naming, DoD
    coverage, and cross-cutting checks.
- Next: Iter 1 — implementation. Suggested slice order:
  1. Branding helper + config (`internal/ui/labels.go`,
     `UIConfig.ArchiveLabel`, validation, `Model.archiveLabel`).
  2. Keymap + pane-scoping (`Archive` default `["a","e"]`,
     `defaults.go` update, viewer `e` removal, folders-pane
     regression test).
  3. Surface updates batched into one PR (toast central
     formatter, hints, palette titles + synonyms, help overlay).
  4. Cmd-bar verbs + thread/`;` chord arms.
  5. CLI alias.
  6. Test pass + ship-time doc sweep.
