# Spec 26 — Bundle senders

## Status
not-started

## DoD checklist
Mirrors `docs/specs/26-bundle-senders.md` §9. Tick as work lands.

- [ ] Migration `013_bundled_senders.sql` (`account_id`, `address`
      lowercased, `added_at`); `schema_meta.version = '13'`.
      (Specs 23 and 24 own 011 and 012 respectively.)
- [ ] `store.Store` gains `AddBundledSender`, `RemoveBundledSender`,
      `ListBundledSenders`, `IsSenderBundled`; all four lowercase
      the address argument inside the store (defense-in-depth).
- [ ] `KeyMap.BundleToggle` (default `B`) and `KeyMap.BundleExpand`
      (default `" "`); `BindingOverrides` mirror; both wired through
      `ApplyBindingOverrides`. `BundleToggle` added to
      `findDuplicateBinding`; `BundleExpand` deliberately excluded
      (pane-scoped Space share with `Expand` is intentional).
- [ ] `Model.bundleExpanded` and `Model.bundledSenders` fields with
      §5.6 lifecycle (real folders persist; synthetic IDs deleted on
      exit; sign-out clears).
- [ ] `B` dispatch (list pane only): synchronous in-memory toggle,
      `bundleToggleCmd(addr, nowBundled)`; per-address seq counter
      (`bundleInflight`) guards rapid-press race; toast count walked
      synchronously in `Update` post-`bundleToastMsg`.
- [ ] `BundleExpand` (Space) wired in `dispatchList` for bundle
      header AND bundle-member rows (member Space collapses parent,
      cursor lands on header). Flat-row Space is no-op.
- [ ] `Enter` on collapsed bundle header expands and leaves cursor
      on the header; second Enter opens the representative.
- [ ] `ListModel.bundleCache` (rendered, messageIndex, valid)
      with §8.1 invalidation triggers.
- [ ] `rowAt`, `SelectedMessage() (store.Message, bool)`,
      `messageIndexAt` helpers; existing `Selected()` renamed
      across all call sites; `(_, ok)` shape preserved.
- [ ] Cursor-consumer audit per §5.5 table: `Up`/`Down`/`PageUp`/
      `PageDown` step rendered rows; `ShouldLoadMore` uses
      `messageIndexAt`; `AtCacheWall` / `OldestReceivedAt`
      unchanged (operate on underlying message slice).
- [ ] List render pass: bundle headers per §5.2 (column width
      identical to flat rows; FOLDER `+N` truncation helper).
- [ ] `[ui].bundle_min_count` (default 2; range 0–9999, 0 = off);
      `[ui].bundle_indicator_collapsed` (default `▸`);
      `[ui].bundle_indicator_expanded` (default `▾`); ≤2 display
      cells (validated at load).
- [ ] `Ctrl+R` fans out `loadBundledSendersCmd`; handler rebuilds
      set, sweeps stale `bundleExpanded` entries, invalidates
      `bundleCache`.
- [ ] CLI `cmd/inkwell/cmd_bundle.go`: `add` / `remove` / `list`
      with `--output json`; addresses lowercased; no-account guard
      exits 1 with `inkwell: not signed in`.
- [ ] Audit `app_e2e_test.go` Enter-opens-viewer fixtures: pin
      first row sender or document no-bundle invariant.
- [ ] Tests per §9 (store, UI dispatch / e2e, CLI, redaction,
      benchmarks).
- [ ] User docs: `docs/user/reference.md` `B` + Space + indicators
      + CLI rows; `docs/user/how-to.md` "Bundle a noisy newsletter
      sender" recipe (canonical workflow: `:filter ~f <addr>` then
      `;<verb>` for whole-bundle ops).
- [ ] `docs/PRIVACY.md` "what data inkwell stores locally" gains
      `bundled_senders` row.
- [ ] `docs/PRD.md` §10 spec inventory: row for 26 added. Specs
      22–25 already exist on main with their own inventory rows.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| Bundle pass over 1000 msgs, 50 designations (`SetMessages` recompute) | ≤2ms p95 | — | `BenchmarkBundlePass1000` | not-measured |
| `View()` cache-hit overhead | ≤0.1ms p95, ≤4 allocs | — | `BenchmarkBundleViewRender` | not-measured |
| `AddBundledSender` / `RemoveBundledSender` | ≤1ms p95 | — | `BenchmarkBundleAddRemove` | not-measured |
| `ListBundledSenders` (≤500 rows) | ≤2ms p95 | — | `BenchmarkListBundledSenders` | not-measured |

## Iteration log

### Iter 1 — 2026-05-07 (spec drafted + adversarial review)
- Slice: spec written; three rounds of adversarial review.
- Rounds: round 1 produced ~30 findings; round 2 produced ~13
  (mostly issues introduced by round-1 fixes); round 3 produced
  2 substantive findings, both fixed.
- Key design decisions captured in the spec:
  - **Per-sender opt-in, exact-match lowercase** (no domain, no
    list-id, no plus-tag stripping) — the smallest unambiguous
    v1; future `[ui].bundle_strip_plus_tag` can opt in.
  - **Consecutive-only collapse** preserves date order; matches
    Thunderbird's group-by-sort, avoids Inbox-bundles' criticism.
  - **Cursor model split**: `m.cursor` indexes rendered rows;
    `m.messages` retains its semantics; `messageIndexAt` bridges.
    Audit table enumerates every consumer that must be updated.
  - **Two new keybindings**: `B` (toggle designation) and
    `BundleExpand` (Space, pane-scoped to list pane). The
    existing `Expand` (`o` / Space) stays the folders-pane key;
    Space is shared by intent, deliberately excluded from the
    duplicate-binding scan.
  - **No action queue**: bundle is local-only (parallels mute
    spec 19). No Graph call, no `actions` row.
  - **`bundleCache` on ListModel** with explicit invalidation
    triggers; bundle pass recomputes only on cache miss; `View()`
    cache-hit budget is ≤0.1ms / ≤4 allocs.
  - **Toast count walked synchronously** in `Update` after
    `bundleToastMsg`; per-address seq counter guards rapid-press
    races.
  - **CLI ↔ TUI sync** via `Ctrl+R` (no SQLite update_hook in v1);
    refresh sweeps stale `bundleExpanded` entries.
  - **Migration 013** is the next free slot (specs 23 / 24 own
    011 / 012); bare `CREATE TABLE` matches the 009/010
    precedent (idempotent under the gated migration runner).
  - **Spec 19 mute interaction** is precise: bundle pass groups
    whatever `SetMessages` is given. Normal folder views drop
    muted before bundling; filter / search / muted-threads views
    include muted (per spec 19 §4.3 / §4.4) and the muted glyph
    appears on member rows when expanded.
  - **Spec 20 thread-chord interaction**: `T a` on a bundle
    representative archives one thread; the bundle re-evaluates
    on reload. Spec 26 does NOT augment spec 20's toast text —
    visible row-count change is its own feedback.
  - **Spec 21 cross-folder interaction**: bundle headers in
    multi-folder bundles render `<folder> +N` with a truncation
    helper that preserves the `+N` suffix.
- Implementation not yet started.
