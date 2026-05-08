# Spec 26 — Bundle senders

## Status
done

## DoD checklist
Mirrors `docs/specs/26-bundle-senders.md` §9. Tick as work lands.

- [x] Migration `013_bundled_senders.sql` (`account_id`, `address`
      lowercased, `added_at`); `schema_meta.version = '13'`.
- [x] `store.Store` gains `AddBundledSender`, `RemoveBundledSender`,
      `ListBundledSenders`, `IsSenderBundled`; all four lowercase
      the address argument inside the store (defense-in-depth).
- [x] `KeyMap.BundleToggle` (default `B`) and `KeyMap.BundleExpand`
      (default `" "`); `BindingOverrides` mirror; both wired through
      `ApplyBindingOverrides`. `BundleToggle` added to
      `findDuplicateBinding`; `BundleExpand` deliberately excluded.
- [x] `Model.bundleExpanded` and `Model.bundledSenders` fields with
      §5.6 lifecycle (real folders persist; synthetic IDs deleted on
      exit; sign-out clears via `bundledSendersLoadedMsg` reload).
- [x] `B` dispatch (list pane only): synchronous in-memory toggle,
      `bundleToggleCmd(addr, nowBundled)`; per-address seq counter
      (`bundleInflight`) guards rapid-press race; toast count walked
      synchronously in `Update` via `countBundleCollapse`.
- [x] `BundleExpand` (Space) wired in `dispatchList` for bundle
      header AND bundle-member rows (member Space collapses parent,
      cursor lands on header). Flat-row Space is no-op.
- [x] `Enter` on collapsed bundle header expands and leaves cursor
      on the header; second Enter opens the representative.
- [x] `ListModel.cache` (rendered, messageIndex, valid) with §8.1
      invalidation triggers (SetMessages, SetBundledSenders,
      SetBundleExpanded, SetBundleMinCount, ResetLimit).
- [x] `rowAt`, `SelectedMessage() (store.Message, bool)`,
      `messageIndexAt`, `SelectedRow`, `renderedLen` helpers;
      existing `Selected()` renamed across all call sites.
- [x] Cursor-consumer audit per §5.5 table: `Up`/`Down`/`PageUp`/
      `PageDown`/`JumpTop`/`JumpBottom` step rendered rows;
      `ShouldLoadMore` and `AtCacheWall` use `messageIndexAt`;
      `OldestReceivedAt` unchanged.
- [x] List render pass: bundle headers per §5.2 (disclosure glyph,
      `(N) — <subject>`, address in FROM column, `truncateBundleFolder`
      helper for cross-folder bundles).
- [x] `[ui].bundle_min_count` (default 2; 0 = off);
      `[ui].bundle_indicator_collapsed` / `bundle_indicator_expanded`
      (≤2 display cells validated at load).
- [x] `Ctrl+R` fans out `loadBundledSendersCmd`; handler rebuilds
      set, sweeps stale `bundleExpanded` entries.
- [x] CLI `cmd/inkwell/cmd_bundle.go`: `add` / `remove` / `list`
      with `--output json`; addresses lowercased; uses the existing
      `buildHeadlessApp` no-account guard.
- [x] Tests: store (Add/Remove/List/IsSender + FK cascade),
      UI dispatch + visible-delta (B / Space / Enter / cursor /
      bundle render / column width / rapid toggle / refresh sweep /
      page-down / load-more), CLI (add/remove/list/JSON/idempotent),
      benchmarks (BundlePass1000, BundleViewRender, BundleAddRemove,
      ListBundledSenders) — all under budget.
- [x] User docs: `docs/user/reference.md` `B` + `Space` + bundle
      indicators + CLI rows; `docs/user/how-to.md` "Bundle a noisy
      newsletter sender" recipe (canonical workflow:
      `:filter ~f <addr>` then `;<verb>`).
- [x] `docs/PRIVACY.md` local-data section gains `bundled_senders`.
- [x] `docs/PRD.md` §10 spec inventory: row for 26 marked shipped.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| Bundle pass over 1000 msgs, 50 designations (`SetMessages` recompute) | ≤2ms p95 | ~52µs (M5) | `BenchmarkBundlePass1000` | met |
| `View()` cache-hit overhead | ≤0.1ms p95, ≤4 allocs | ~1.4ns / 0 allocs (M5) | `BenchmarkBundleViewRender` | met |
| `AddBundledSender` / `RemoveBundledSender` | ≤1ms p95 | ~40µs (M5) | `BenchmarkBundleAddRemove` | met |
| `ListBundledSenders` (≤500 rows) | ≤2ms p95 | ~394µs (M5) | `BenchmarkListBundledSenders` | met |

## Iteration log

### Iter 2 — 2026-05-08 (implementation shipped as v0.55.0)
- Slice: full implementation per §9 DoD.
- Schema + store: migration 013 added (`bundled_senders` table, FK-
  cascade on accounts), four `Store` methods with defense-in-depth
  lowercase, `BundledSender` type. Five store unit tests + 2 benches.
- Config: `bundle_min_count` (0–9999), `bundle_indicator_collapsed`,
  `bundle_indicator_expanded` (≤2 cells, validated via
  `runewidth.StringWidth`). Defaults wired in `defaults.go`. Two
  validate tests added.
- KeyMap: `BundleToggle` (B) and `BundleExpand` (Space) added,
  `BindingOverrides` mirrored, `ApplyBindingOverrides` wired. Only
  `BundleToggle` enrolled in `findDuplicateBinding`; the Space
  share is documented in the duplicate-scan exclusion comment.
- ListModel: `renderedRow` type, `bundleCache` (rendered +
  messageIndex), `rowAt` / `SelectedMessage` / `SelectedRow` /
  `messageIndexAt` / `renderedLen`. `Selected()` renamed to
  `SelectedMessage()` across ~30 call sites; `(_, ok)` shape
  preserved. `Up`/`Down`/`PageUp`/`PageDown`/`JumpTop`/`JumpBottom`
  step rendered rows. `ShouldLoadMore`/`AtCacheWall` use
  `messageIndexAt`. `SetMessages` re-anchors on message ID.
- Render: `renderBundleHeader` writes `(N) — <subject>` with the
  disclosure glyph in the flag/invite slot; `truncateBundleFolder`
  formats `<folder> +N` for cross-folder bundles. Indicators
  precedence (calendar > mute > stream > stack > bundle disclosure)
  documented in code comments.
- Dispatch: B/Space/Enter wired in `dispatchList`. `B` is
  optimistic — synchronous in-memory mutation + `bundleToggleCmd`
  for the store write; per-address `bundleInflight` seq guards
  rapid-press race. Toast text walks `m.list.messages` via
  `countBundleCollapse` for an exact "collapses N" count. `Enter`
  on a collapsed bundle expands without opening the viewer.
  `Space` on a member collapses the parent and lands on the header
  via `moveCursorToBundleHeader`. `Refresh` (Ctrl+R) also kicks
  `loadBundledSendersCmd` so CLI mutations apply on next refresh.
  `clearFilter`/search-exit drop synthetic-folder `bundleExpanded`
  entries. `Init` loads the set on startup when an account exists.
- CLI: `cmd_bundle.go` — `add` / `remove` / `list` with
  `--output json`. Addresses lowercased before any store call.
  Uses the existing `buildHeadlessApp` not-signed-in guard. Six
  CLI tests cover the round-trip + JSON shape + idempotency.
- Tests: 27 UI tests (visible-delta for B / Space / Enter / cursor
  traversal / cross-folder render / rapid toggle / refresh sweep /
  page-down / load-more) + 2 UI benchmarks; 5 store unit tests + 2
  store benchmarks; 6 CLI tests. All four perf budgets measured
  well under target on M5.
- Doc sweep: PRD §10 inventory marked shipped, ROADMAP bucket and
  §1.11 backlog flipped to Shipped v0.55.0, README status table +
  download example bumped to v0.55.0, reference.md gains B / Space /
  bundle indicators + CLI rows, how-to.md gains the
  "Bundle a noisy newsletter sender" recipe with the
  `:filter ~f <addr>` / `;<verb>` workflow callout, CONFIG.md
  gains all three new keys + the two new bindings, PRIVACY.md
  notes the new local-only metadata table, spec sets
  `**Shipped:** v0.55.0`, plan flips to `done`.

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
