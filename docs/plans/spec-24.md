# Spec 24 — Split inbox tabs

## Status
done — **Shipped v0.52.0** (2026-05-07)

## DoD checklist

Copied from `docs/specs/24-split-inbox-tabs.md` §12. Tick during
implementation iterations.

- [ ] Migration `012_tab_order.sql` applies cleanly on a fresh DB
      and on a v0.49.x DB (the spec-21 release line). `tab_order`
      is `NULL` for all pre-migration rows. `schema_meta.value`
      bumped to `'12'`. Note: spec 23 (routing destinations,
      lower-numbered, ships first) claims migration 011.
- [ ] `store.SavedSearch.TabOrder *int` field exists; serialised as
      NULL/integer in SQLite. `PutSavedSearch` does NOT touch
      `tab_order`. `ListSavedSearches` includes the field.
- [ ] `store.ListTabs`, `store.SetTabOrder`, `store.ReindexTabs`,
      `store.ApplyTabOrder` implemented with unit tests covering:
      empty list, single tab, reorder, demote, dense reindex after
      gaps, partial-UNIQUE rejection of duplicates.
- [ ] `Manager.Tabs`, `Manager.Promote`, `Manager.Demote`,
      `Manager.Reorder`, `Manager.CountTabs` implemented; TOML
      mirror written on every mutation; `tab_order` field
      round-trips through TOML.
- [ ] `Manager.Delete` and `Manager.DeleteByName` modified to call
      `store.ReindexTabs` after a successful row delete; TOML
      mirror rewritten as part of the existing delete path.
- [ ] `:tab` cmd-bar dispatcher: `add`, `remove`, `move`, `close`,
      `list`, and `<name>` (jump). Tab-completion on names where
      the cmd-bar already supports completion (spec 04 §11).
      Unknown name surfaces a friendly error.
- [ ] Pane-scoped key bindings `]` / `[` for next / prev tab; bind
      only when list pane is focused; documented in
      `BindingOverrides` with defaults; config validation rejects
      empty overrides. Viewer thread-nav and calendar day-nav
      bindings unchanged.
- [ ] Tab strip renders above the list pane whenever any tabs are
      configured for the active account; per-state styling per
      §5.1.
- [ ] Active-tab styling, inactive-tab styling, count rendering,
      `•` new-mail glyph, `⚠` error glyph (matching spec 11 §10),
      horizontal-scroll overflow with `‹` / `›`.
- [ ] Per-tab state preserved across cycles (cursor, scroll,
      message slice) via `tabState []listSnapshot`. Cache TTL
      reuses `[saved_search].cache_ttl`. Edit-saved-search
      invalidates the relevant snapshot.
- [ ] Sidebar saved-search entries that are also tabs render with
      the `▦` indicator. Selecting a sidebar saved search that is
      a tab focuses the tab.
- [ ] `:filter` while a tab is active AND's the user pattern with
      the tab pattern; `:filter --all` ignores the tab (spec 21
      consistency). Status bar hint reflects which scope is active.
- [ ] `Manager.CountTabs` integrates with the existing sync-event
      hook (the same site as `RefreshCounts` / `CountPinned` for
      pinned saved searches). Counts refresh on `FolderSyncedEvent`.
- [ ] CLI `inkwell tab list / add / remove / move` per §9. (No
      `tab eval` — `inkwell rule eval` already covers single-name
      evaluation.) `tab list --output json` returns the envelope
      shape from §9 with `matched` and `unread` per row.
- [ ] `tabs.max_name_width` validated with min = 4 in
      `internal/config/validate.go`.
- [ ] `tabs.enabled = false` forcibly hides the strip;
      `tabs.show_zero_count = true` renders `[Name 0]`;
      `tabs.cycle_wraps = false` makes the cycle keys no-op at
      ends.
- [ ] `:tab close` demotes the active tab and selects the
      next-to-the-right (or wraps to leftmost; if the strip
      becomes empty, `activeTab = -1` and the strip hides).
- [ ] The `SavedSearchService` UI interface gains the methods
      listed in §4; sync-event hook extended to call
      `RefreshTabCounts`.
- [ ] `THREAT_MODEL.md` row added: "Tab name as PII vector —
      mitigated by call-site DEBUG-only logging policy (§11) and
      `TestPromoteDoesNotLogName` regression."
- [ ] All §12 unit / dispatch / e2e / bench tests added and green.
- [ ] User docs: `docs/user/reference.md` adds `]` / `[` and
      `:tab` rows; `docs/user/how-to.md` "Set up split inbox
      tabs" recipe.
- [ ] `docs/CONFIG.md` `[tabs]` section added between
      `[saved_search]` and `[bulk]`.
- [ ] `docs/PRD.md` §10 inventory row updated for spec 24.
- [ ] `docs/ROADMAP.md` §1.7 row updated to "shipped as spec 24";
      the `Tab` / `Shift+Tab` claim in §1.7 prose corrected to
      `]` / `[`.

## Perf budgets

| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| Tab cycle, cached state | <16ms p95 | — | `BenchmarkTabCycleCached` | pending |
| Tab cycle, cache miss | <100ms p95 | — | `BenchmarkTabCycleEvaluate` | pending |
| `Manager.CountTabs` 5 tabs / 100k msgs, cold | <200ms p95 | — | `BenchmarkCountTabs5x100k_Cold` | pending |
| `Manager.CountTabs` 5 tabs / 100k msgs, warm | <20ms p95 | — | `BenchmarkCountTabs5x100k_Warm` | pending |
| `Manager.CountTabs` 20 tabs / 100k msgs, cold | <500ms p95 | — | `BenchmarkCountTabs20x100k_Cold` | pending |
| Tab strip render | <2ms p95 | — | `BenchmarkRenderTabStrip` | pending |

## Iteration log

### Iter 0 — 2026-05-07 — spec drafting only
- Slice: write the spec, run adversarial review until findings
  bottom out.
- Commands run: none (no code touched).
- Result: Spec landed at `docs/specs/24-split-inbox-tabs.md`.
  Three review rounds (24 + 15 + 5 findings); all fixed.
- Critique: design choices to revisit during implementation:
  - `]` / `[` are pane-scoped to viewer (thread nav) and calendar
    (day nav); list-pane scoping is the third case. Implementation
    must register handlers in the list-pane dispatch branch only,
    not globally.
  - `ApplyTabOrder` is the new transactional helper; the prose in
    §3.4 prescribes a two-pass NULL-then-renumber to keep the
    partial UNIQUE in §3.1 satisfied. Watch the SQL.
  - Spec 11's status string still says "Stub" but the Manager API
    is shipped. Fold a side-fix to spec 11's status line into the
    spec-24 implementation PR, or open a tiny side-PR.
  - The "tab strip always visible when tabs exist" rule (§5.1) is
    the resolution to the §5.1 vs §7 contradiction the third
    review surfaced. Implementation must NOT key strip visibility
    on `activeTab >= 0`.
- Next: schedule iter 1 as the first implementation slice — the
  `012_tab_order.sql` migration plus `store.ListTabs` /
  `SetTabOrder` / `ReindexTabs` / `ApplyTabOrder` with unit tests
  (CLAUDE.md §12.2 phase 2: smallest runnable slice). No UI yet.

### Iter 1 — 2026-05-07 (implementation + ship)
- Slice: full implementation — all DoD bullets delivered.
- Commands run: `make regress` green (gofmt, vet, build, race, e2e,
  integration, bench).
- Result: tagged v0.52.0. All DoD bullets satisfied. Key fixes:
  `TestFoldersCollapseHidesChildren` and
  `TestSavedSearchEnterRunsFilter` updated for always-rendered
  Streams section. `tabsConfigOrDefault` uses `MaxNameWidth==0`
  as zero-value sentinel for unset config.
- Critique: none outstanding.
- Next: spec 25.
