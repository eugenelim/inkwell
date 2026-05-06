# Spec 21 — Cross-folder bulk operations

## Status
not-started

## DoD checklist
- [ ] Model: `filterAllFolders bool`, `filterFolderCount int`,
      `filterFolderName string`, `foldersByID map[string]store.Folder`
      fields added. `filterAllFolders`/`filterFolderCount`/
      `filterFolderName` cleared in `clearFilter()`. `foldersByID`
      populated in `FoldersLoadedMsg` handler (alongside `m.folders`
      update).
- [ ] `dispatchCommand` `case "filter":` strips `--all` / `-a` prefix
      (using `HasPrefix("--all")` or `== "-a" || HasPrefix("-a ")` to
      avoid greedy match), sets `filterAllFolders`, guards empty-pattern
      with a friendly error, then calls `runFilterCmd` unchanged.
- [ ] `filterAppliedMsg` handler: when `filterAllFolders`, compute
      distinct folder count from message slice, look up display name(s)
      from `m.foldersByID`; populate `m.list.folderNameByID` when
      `filterFolderCount > 1`, nil otherwise.
- [ ] Status bar hint shows "(Inbox)" for single folder, "(N folders)"
      for multi-folder, nothing extra when `filterAllFolders == false`.
- [ ] `confirmBulk` body appends "across N folders" to modal title when
      `filterAllFolders && filterFolderCount > 1`. Signature unchanged.
- [ ] `ListModel` gains `folderNameByID map[string]string`; `View()`
      renders FOLDER column (12 chars, FROM trimmed to 12) when non-nil.
- [ ] `m.list.folderNameByID` cleared in `clearFilter()`.
- [ ] `inkwell filter --all`: wires `allFolders` variable in
      `cmd_filter.go`. When set: call `app.store.ListFolders` to build
      display-name map; add `folders` count map to JSON; add Folder
      column to text output. No query change needed.
- [ ] `inkwell messages --filter ... --all`: new `--all` flag; mutually
      exclusive with `--folder` via `MarkFlagsMutuallyExclusive`; when
      set passes `folderID = ""` to `runFilterListing`.
- [ ] Tests:
  - `TestFilterAllFlagSetsModelField` (`:filter --all ~f x` sets
    `filterAllFolders=true`, pattern passed to runFilterCmd is `~f x`)
  - `TestFilterNoPrefixLeavesFieldFalse`
  - `TestFilterAllEmptyPatternError`
  - `TestFilterAllFolderHintShowsFolderCount` (e2e: 2-folder fixture)
  - `TestFilterAllFolderColumnRendered` (e2e: FOLDER header visible)
  - `TestFilterAllConfirmModalIncludesFolderCount` (e2e: ";d" modal)
  - `TestFilterCLIAllFlagAddsFolderMetadata` (CLI JSON output)
  - `TestMessagesFilterAllOverridesFolder`
- [ ] User docs: `docs/user/reference.md` `:filter --all` row;
      `docs/user/how-to.md` cross-folder cleanup recipe updated.

## Perf budgets
No new benchmark required. Cross-folder uses the existing
`SearchByPredicate` path, gated by spec 02's <100ms p95 budget.

## Iteration log
### Iter 1 — 2026-05-06 (spec written + adversarial review)
- Slice: spec document written, two rounds of adversarial review
  (9 findings + 6 findings), all fixed.
- Key decisions:
  - **Correct premise**: filter is ALREADY cross-folder by default;
    spec adds UI visibility layer only, not a new query mechanism.
  - **`--all` / `-a` is opt-in visibility**: without it, filter runs
    cross-folder silently (preserving existing behaviour); with it,
    folder count appears in hint, confirm modal, and list column.
  - **`-a` stripping uses exact match or space-suffix check** to avoid
    greedy match on patterns starting with `-a`.
  - **`m.foldersByID`** is a new model field (does not exist today),
    populated in `FoldersLoadedMsg` handler.
  - **`confirmBulk` signature unchanged** — only body modified.
  - **`dispatchCommand`** is the correct function name (not
    `dispatchCmd`).
  - **`inkwell filter --all`**: already declared but dead — wired to
    produce folder metadata in output. No query change.
  - **`inkwell messages --all`**: genuinely new flag; mutually exclusive
    with `--folder` via cobra `MarkFlagsMutuallyExclusive`.
  - **Muted messages**: appear in cross-folder results (consistent with
    spec 19 §4.4 — intentional for explicit filter/search paths).
- Implementation not yet started.
