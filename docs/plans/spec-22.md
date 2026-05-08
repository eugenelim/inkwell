# Spec 22 — Command palette

## Status
done — **Shipped v0.50.0** (2026-05-07)

## DoD checklist
- [ ] **Mode**: `PaletteMode` constant added to
      `internal/ui/messages.go`.
- [ ] **Keymap**: `KeyMap.Palette` field, `BindingOverrides.Palette`
      field, default binding `ctrl+k`, override plumbing in
      `ApplyBindingOverrides`, duplicate detection includes palette.
- [ ] **Trigger**: `updateNormal` in `app.go` matches
      `m.keymap.Palette` and transitions to `PaletteMode`. Other
      modes' update handlers do not match the palette key (no-op).
- [ ] **Files**: `internal/ui/palette.go` (model + view +
      Open/Up/Down/Backspace/AppendRunes/Selected/recordRecent),
      `internal/ui/palette_commands.go`
      (`buildStaticPaletteRows(km KeyMap)` + row literals),
      `internal/ui/palette_match.go` (scoring + sort).
- [ ] **Sigils**: `#` folders, `@` saved searches, `>` commands-only,
      no-sigil = mixed. `/` is a literal rune.
- [ ] **Tab vs Enter**: Tab on `NeedsArg` rows defers to `ArgFn`
      (folder picker / category input / cmd-bar pre-fill /
      folder-name input). Enter on `NeedsArg` rows behaves the same
      since these have no zero-arg form.
- [ ] **Availability**: dimmed rows render but Enter shows a toast
      with the cached `Why` string instead of dispatching.
- [ ] **Recents**: in-process MRU, cap 8. Selection records into
      recents before the RunFn fires.
- [ ] **Render**: centered modal via `lipgloss.Place(... t.Modal ...)`.
      Width = `min(max(60, w/2), 80)`. Right-aligned binding column.
      Section badges. Status footer with result count + key hints.
- [ ] **Help overlay**: `buildHelpSections` in `help.go` gains a
      `command palette` row in `Modes & meta`.
- [ ] **Tests**: scoring (subsequence, prefix bonus, word-boundary,
      uppercase, consecutive, in-title boundary, frecency boost,
      exclusion); sigil routing + cursor reset; recordRecent MRU
      semantics; collectPaletteRows availability resolution; width
      clamp; e2e suite covering every binding + sigil with visible-
      delta; redaction sniff test; bench under 22ms p95 on 5000 rows.
- [ ] **Config**: `[bindings] palette` documented in `docs/CONFIG.md`.
- [ ] **User docs**: `docs/user/reference.md` Command palette
      section; `docs/user/how-to.md` "Discover bindings" recipe.
- [ ] **Project docs**: `docs/PRD.md` §10 row 22; `docs/ROADMAP.md`
      §0 Bucket 2 row 1 + §1.6 narrative cite spec 22; this plan
      file maintained per CLAUDE.md §13.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| Open palette (rebuild rows from snapshot) | <5ms p95 | — | `BenchmarkPaletteOpen` | pending |
| Per-keystroke filter+rerank, ~200 rows | <2ms p95 | — | `BenchmarkPaletteFilter`/200 | pending |
| Per-keystroke filter+rerank, 5000 rows | <15ms p95 (fail >22ms) | — | `BenchmarkPaletteFilter`/5000 | pending |

## Iteration log
### Iter 1 — 2026-05-06 (spec written + adversarial review)
- Slice: spec document written; three rounds of adversarial review
  (9 findings → 6 → 3); all addressed.
- Key decisions:
  - **`Ctrl+K` is a parallel entry point, not a replacement** for
    `:` cmd-bar or `?` help. Cmd-bar stays for muscle-memory power-
    use; help stays for categorised reference. Palette is for
    discovery + passive shortcut learning.
  - **Right-aligned binding column** (Superhuman ancestry) is the
    single biggest UX lever — palette doubles as cheatsheet.
  - **fzf-style subsequence scoring** with a sentinel-rune title/
    synonym boundary so title hits genuinely outrank synonym hits.
    Frecency boost (in-process MRU) ranks recent commands first
    without separate MRU bucket carve-out.
  - **Sigil scopes**: `#` folders, `@` saved searches, `>` commands.
    Mixed when no sigil. `/` is literal — rejected the
    "open SearchMode" auto-close as mode-teleporting.
  - **No preview pane** in v1. Palette is for *acting*, not
    browsing. Single column keeps the modal under 50 cols and
    well within the local-action latency budget.
  - **`Available` resolved at row-collection time** and stored on
    the row as a struct (`OK bool, Why string`). Renderer + Enter
    both read the cached value; no per-keystroke re-evaluation.
  - **`RunFn` / `ArgFn` are value-typed closures**; closures
    capture small identifiers (folder ID, saved-search name), never
    Model pointers. Stale-snapshot edge case is benign — folder
    closures dispatch IDs, the existing 404 path handles deletion.
  - **Spec 20 chord refactor explicitly rejected** as scope creep.
    Palette RunFns copy the 1–3 line dispatch from each chord
    verb; no `tea.KeyMsg` synthesis (chord state spans two Update
    cycles, not reliably reproducible inline).
  - **Spec 21 `--all` composition**: `Filter (all folders)` row
    pre-fills the cmd-bar with `filter --all ` so the existing
    dispatcher strips `--all` and sets `filterAllFolders` as today.
  - **Tests**: 18 e2e tests cover every binding (Ctrl+K, Esc,
    Enter, ↑/↓, Ctrl+P/N, Tab, Backspace at empty + past sigil)
    and every sigil (`#`, `@`, `>`, no-sigil) with visible-delta
    assertions per CLAUDE.md §5.4. Bench gates 22ms p95 over
    5000 rows.
- Implementation not yet started.

### Iter 2 — 2026-05-07 (implementation + ship)
- Slice: full implementation — all DoD bullets delivered.
- Commands run: `make regress` green (gofmt, vet, build, race, e2e,
  integration, bench).
- Result: tagged v0.50.0. All DoD bullets satisfied. Key deviation:
  shorter-title tiebreak added to sort to fix "Filter (all folders)"
  ranking above "Filter…"; `TestHelpOverlayShowsAllSections` bumped
  to terminal height 50 after help overlay grew one row.
- Critique: none outstanding.
- Next: spec 23.
