# Spec 22 — Command palette

## Status
done

## DoD checklist
- [x] **Mode**: `PaletteMode` constant added to
      `internal/ui/messages.go`.
- [x] **Keymap**: `KeyMap.Palette` field, `BindingOverrides.Palette`
      field, default binding `ctrl+k`, override plumbing in
      `ApplyBindingOverrides`, duplicate detection includes palette.
- [x] **Trigger**: `updateNormal` in `app.go` matches
      `m.keymap.Palette` and transitions to `PaletteMode`. Other
      modes' update handlers do not match the palette key (no-op).
- [x] **Files**: `internal/ui/palette.go` (model + view +
      Open/Up/Down/Backspace/AppendRunes/Selected/recordRecent),
      `internal/ui/palette_commands.go`
      (`buildStaticPaletteRows(km KeyMap)` + row literals),
      `internal/ui/palette_match.go` (scoring + sort).
- [x] **Sigils**: `#` folders, `@` saved searches, `>` commands-only,
      no-sigil = mixed. `/` is a literal rune.
- [x] **Tab vs Enter**: Tab on `NeedsArg` rows defers to `ArgFn`
      (folder picker / category input / cmd-bar pre-fill /
      folder-name input). Enter on `NeedsArg` rows behaves the same
      since these have no zero-arg form.
- [x] **Availability**: dimmed rows render but Enter shows a toast
      with the cached `Why` string instead of dispatching.
- [x] **Recents**: in-process MRU, cap 8. Selection records into
      recents before the RunFn fires.
- [x] **Render**: centered modal via `lipgloss.Place(... t.Modal ...)`.
      Width = `min(max(60, w/2), 80)`. Right-aligned binding column.
      Section badges. Status footer with result count + key hints.
- [x] **Help overlay**: `buildHelpSections` in `help.go` gains a
      `command palette` row in `Modes & meta`.
- [x] **Tests**: scoring (subsequence, prefix bonus, word-boundary,
      uppercase, consecutive, in-title boundary, frecency boost,
      exclusion); sigil routing + cursor reset; recordRecent MRU
      semantics; collectPaletteRows availability resolution; width
      clamp; e2e suite covering every binding + sigil with visible-
      delta; redaction sniff test; bench well under 22ms p95 on 5000
      rows.
- [x] **Config**: `[bindings] palette` documented in `docs/CONFIG.md`.
- [x] **User docs**: `docs/user/reference.md` Command palette
      section; `docs/user/how-to.md` "Discover bindings" recipe.
- [x] **Project docs**: `docs/PRD.md` §10 row 22; `docs/ROADMAP.md`
      §0 Bucket 2 row 1 + §1.6 narrative cite spec 22; this plan
      file maintained per CLAUDE.md §13.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| Open palette (rebuild rows from snapshot) | <5ms p95 | 53µs (200-row fixture, M5) | `BenchmarkPaletteOpen` | green |
| Per-keystroke filter+rerank, ~200 rows | <2ms p95 | 24µs (M5) | `BenchmarkPaletteFilter`/200 | green |
| Per-keystroke filter+rerank, 5000 rows | <15ms p95 (fail >22ms) | 347µs (M5) | `BenchmarkPaletteFilter`/5000 | green |

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

### Iter 2 — 2026-05-07 (implementation)
- Slice: full implementation in one pass — keymap field,
  `PaletteMode`, three new files (palette.go, palette_commands.go,
  palette_match.go), wiring in app.go, help-overlay row, unit +
  dispatch + e2e tests, bench, redaction guard.
- Commands run: `gofmt -s`, `go vet`, `go test -race ./...`,
  `go test -tags=integration ./...`, `go test -tags=e2e ./...`,
  `go test -bench=. -benchmem -run=^$ ./...`, `bash scripts/regress.sh`.
- Result: all six gates green. Bench (M5): 24µs / 347µs filter+rerank
  for 200 / 5000 rows; 53µs open. All within the spec 22 §6 budgets
  by orders of magnitude.
- Key implementation choices:
  - **Sort tie-breakers** added beyond what spec §4.4 lists: when
    scores tie, prefer `sectionOrder` (commands > folders >
    saved-searches), then shorter title, then alphabetical. Without
    this, query "filter" returned the longer "Filter (all
    folders)…" row first; spec wants the shorter "Filter…" row.
    Same hit when query "archive" was matching both the static
    Archive command and a top-level "Archive" folder. The section
    bias keeps the action-oriented row first.
  - **`m.cmd.buf` direct write** in the cmd-bar pre-fill helper
    (`prefillCmdBar`) — same idiom the existing Filter binding uses
    in updateNormal:2284. Stays inside the package so no new
    accessor is needed.
  - **Mute row title resolution** does a 200ms-bounded
    `IsConversationMuted` call at row-collection time to flip
    "Mute thread" / "Unmute thread" per spec §3.7. Bounded so a
    slow store can't stall palette open.
  - **Help-overlay test bumped** from 40 to 50 rows; the new
    `ctrl+k command palette` line pushed the help modal past the
    test terminal height, which `lipgloss.Place` clipped silently.
- Critique: no layering violations (UI → store via deps; no Graph
  call from palette path); palette emits no logs; redaction test
  guards future regressions; bench numbers are well within budget;
  spec 17 review confirmed nothing for threat model / privacy doc.
- Next: ship.
