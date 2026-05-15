# Spec 35 — Body regex & local body indexing

## Status
in-progress — slices 1-4 + slice 6 + slice 7 (this doc) shipped to main. Slice 5 (saved-search Manager threading + UI sidebar grey-out + `/regex:` prefix + palette rows + `:index` cmd-bar verb) and slice 8 (integration / e2e / bench) pending before tagging v0.64.0.

## DoD checklist
Copied from `docs/specs/35-body-regex-local-indexing/spec.md` §16. Tick
as each slice lands.

**Spec content (`docs/CONVENTIONS.md` §11)**
- [ ] Graph scopes confirmed unchanged: `Mail.Read` only (PRD §3.1).
- [ ] State surfaces named: reads `bodies`; writes `body_text` +
      `body_fts` + `body_trigram`.
- [ ] Graph endpoints called by this spec: **none** (indexer is
      offline; reuses existing body fetches).
- [ ] Offline behaviour: fully functional once bodies are decoded.
- [ ] Undo behaviour: indexing is non-mutating w.r.t. mail state;
      no user-visible undo. CLI destructive ops gated by the
      confirmation prompt (§8.4).
- [ ] Error states wired to the status bar / cmd-bar / `:filter`:
      `ErrRegexUnboundedScan`, `ErrRegexRequiresLocalIndex`,
      `ErrRegexNotSupportedOnHeader`, `ErrTooManyCandidates`,
      `ErrRegexTimeout`.
- [ ] CLI-mode equivalent: `inkwell index {status,rebuild,evict,
      disable}` + regex via `inkwell messages --filter '~b /.../'`.

**Schema + store**
- [ ] `internal/store/migrations/015_body_index.sql` lands; bumps
      `SchemaVersion` from 14 → 15.
- [ ] `internal/store/tabs_test.go:40` updated to assert `"15"`.
- [ ] `internal/store/sender_routing_test.go:320` OR-list extended
      to include `"15"`.
- [ ] `internal/store/AGENTS.md` invariant 1 amended per spec §6.2
      (cache-management write carve-out).

**Indexer**
- [ ] `internal/store/body_index.go` lands with `IndexBody`,
      `UnindexBody`, `BodyIndexStats`, `EvictBodyIndex(opts)`,
      `PurgeBodyIndex`, `SearchBodyText`,
      `SearchBodyTrigramCandidates` (§6.1).
- [ ] `internal/render/render.go`'s `Options` gains
      `OnBodyDecoded` callback; renderer exports
      `DecodeForIndex(rawHTML) (string, error)` that skips
      `normalisePlain` (§6.3).
- [ ] `internal/sync/body_index_hook.go` wires the callback to
      `engine.maybeIndexBody`; engine reads `cfg.BodyIndex.*`.
- [ ] `internal/sync/maintenance.go` `maintenancePass` calls
      `EvictBodyIndex` after `EvictBodies` (§6.4).
- [ ] Cross-folder move keeps `body_text.folder_id` in sync
      (`UpdateMessageFields` patch + regression test, §7.2 + §12).

**Pattern integration**
- [ ] `internal/pattern/ast.go` gains `RegexValue` implementing
      `PredicateValue` (§9.1).
- [ ] `internal/pattern/lexer.go` recognises `/.../` delimiter with
      `\/` escape; compiles via `regexp.Compile` at lex time.
- [ ] `internal/pattern/eval_local_regex.go` lands with
      mandatory-literal extraction (`regexp/syntax`-walk) and the
      `StrategyLocalRegex` SQL emitter (§9.4).
- [ ] `internal/pattern/eval_local.go` `emitStringPredicate` plus
      `emitLocal` / `emitPredicate` / `CompileLocal` gain
      `opts CompileOptions` trailing parameter; body-field arms
      flip to the `body_text` JOIN when
      `opts.BodyIndexEnabled` (§9.2).
- [ ] `internal/pattern/compile.go` `CompileOptions` gains
      `BodyIndexEnabled bool`; selector at lines 276-396 gains
      step 0 (regex routing) and step 1 amendment (body→local).
- [ ] `internal/pattern/eval_memory.go` `EvalEnv` gains
      `BodyTextFor` callback; regex on body fields routes through
      it; missing-from-index drops counted + surfaced (§9.5).

**Saved-search interaction**
- [ ] `config.SavedSearchSettings` gains `BodyIndexEnabled bool`
      (populated at `savedsearch.New` site in
      `cmd/inkwell/cmd_run.go`).
- [ ] `internal/savedsearch/manager.go` threads
      `BodyIndexEnabled` into all four `pattern.Compile` calls
      (`Save`, `Evaluate`, `Edit`, `EvaluatePattern`).
- [ ] `store.SavedSearch` (`internal/store/types.go:278`) gains a
      transient `LastCompileError string` field populated by
      `Manager.List` (§9.6).
- [ ] `ui.SavedSearch` (`internal/ui/app.go:413`) gains the matching
      field; `cmd_run.go` adapter sites carry it across.
- [ ] Sidebar render path greys out rows with non-empty
      `LastCompileError` and shows `!` indicator + status-bar
      help line.

**UI**
- [ ] `internal/ui/app.go` recognises `regex:` prefix in
      `/`-mode; sets `Regex bool` on `search.Query`.
- [ ] Search Stream status indicator gains `[regex local-only]`
      and `[regex needs index]` states (§10.1).
- [ ] Command palette gains four rows: `Index — Status / Rebuild /
      Evict / Disable` (§10.3).
- [ ] Cmd-bar verbs `:index status | rebuild | evict | disable`
      land in `internal/ui/palette_commands.go`.
- [ ] `:filter --explain` SQL output matches the §10.2 literal
      string (asserted by test).

**CLI**
- [ ] `cmd/inkwell/cmd_index.go` adds the four subverbs
      (`status`, `rebuild`, `evict`, `disable`) with `--json`,
      `--folder`, `--limit`, `--force`, `--older-than`,
      `--message-id`, `--yes` flags as enumerated in §11.

**Logging / privacy**
- [ ] `internal/log/redact.go` adds `HashMessageID(id) string`
      (truncated SHA-256 hex); test in
      `internal/log/redact_test.go`.
- [ ] Indexer site never logs message IDs / folder IDs / content
      at INFO+; DEBUG uses `HashMessageID`.
- [ ] CLI surface never enumerates folder names when
      `folder_allowlist` is empty; never echoes snippets.

**Config**
- [ ] `internal/config/` adds the `[body_index]` struct + parser
      + validation rules (§7.1).
- [ ] `docs/CONFIG.md` gains the `[body_index]` section.

**Tests + benchmarks**
- [ ] `go vet ./...`
- [ ] `go test -race ./...`
- [ ] `go test -tags=integration ./...`
- [ ] `go test -tags=e2e ./...`
- [ ] `internal/store/fts_probe_test.go` passes (modernc tokenizer
      probe).
- [ ] All §13 unit / redaction / integration / e2e / bench /
      property tests land.
- [ ] Every perf budget in §14 has a benchmark; passes within
      budget on dev machine.

**Security (spec 17)**
- [ ] `docs/THREAT_MODEL.md` "Threats and mitigations" + "Accepted
      residual risks" updated (§8.1).
- [ ] `docs/PRIVACY.md` "Where data is stored" + "What data
      inkwell accesses" updated (§8.2).
- [ ] PR description carries spec-17 impact note.
- [ ] gosec, semgrep, govulncheck green. Zero new `// #nosec`.

**Docs**
- [ ] `docs/CONFIG.md` `[body_index]` section.
- [ ] `docs/ARCH.md` §6 Tier 3 row + §7 schema-table `body_text`
      row.
- [ ] `docs/PRD.md` §10 inventory row.
- [ ] `docs/product/roadmap.md` Bucket 5 row + §1.13 status flip.
- [ ] `docs/specs/06-search-hybrid/spec.md` §11 OOS bullet struck +
      back-reference to spec 35.
- [ ] `docs/user/reference.md` regex form, `/regex:` prefix,
      `:index` cmd-bar verbs, CLI verbs, viewer status states.
- [ ] `docs/user/how-to.md` recipe: "Search inside bodies (and
      with regex)."
- [ ] `README.md` status-table row at ship.

## Perf budgets

Captured from spec §14. Measured numbers go in at implementation
time. All warm-buffer-cache p95 on dev machine.

| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `IndexBody(1 KB body)` | <3ms p95 | — | `BenchmarkIndexBody1KB` | not measured |
| `IndexBody(10 KB body)` | <8ms p95 | — | `BenchmarkIndexBody10KB` | not measured |
| `IndexBody(1 MB body)` | <60ms p95 | — | `BenchmarkIndexBody1MB` | not measured |
| `SearchBodyText(q, limit=50)` over 50 k indexed bodies | <80ms p95 | — | `BenchmarkSearchBodyText` | not measured |
| `SearchBodyTrigramCandidates(["auth","token="], limit=2000)` over 50 k indexed bodies | <100ms p95 | — | `BenchmarkSearchBodyTrigramCandidates` | not measured |
| Regex post-filter: 200 candidate × 5 KB body × moderate regex | <120ms p95 | — | `BenchmarkRegexPostFilter` | not measured |
| Full regex search end-to-end, 50 k bodies | <300ms p95 | — | `BenchmarkRegexSearchEndToEnd` | not measured |
| `EvictBodyIndex` reducing 5 000 → 4 500 rows | <500ms p95 | — | `BenchmarkEvictBodyIndex` | not measured |
| `PurgeBodyIndex` from 5 000 rows | <1 s p95 | — | `BenchmarkPurgeBodyIndex` | not measured |
| `inkwell index rebuild` for 500 cached bodies | <5 s end-to-end | — | manual smoke | not measured |
| Cold-start overhead of `[body_index].enabled = true` | <50ms added | — | `BenchmarkColdStartWithBodyIndex` | not measured |

## Iteration log

### Iter 1 — 2026-05-14 (spec + plan)
- Slice: research, draft, two-agent adversarial review, multi-pass
  fix loop, plan file
- Verifier: docs review (`docs/CONVENTIONS.md` §12.0 spec-verification
  discipline)
- Commands run: codebase grep verification (`grep` over
  `internal/store`, `internal/pattern`, `internal/render`,
  `internal/sync`, `internal/savedsearch`, `internal/ui`,
  `internal/log`, `cmd/inkwell`); modernc.org/sqlite tokenizer
  probe (`go run` against `:memory:` confirmed `trigram` +
  `external-content` + `unicode61 remove_diacritics 2` +
  `porter unicode61` + `ascii` all OK on v1.50.0 / SQLite 3.53.0)
- Result: spec converged after four adversarial-review passes.
  Pass 1 found 5 Critical, 9 Major, 9 Minor, 4 Nit; pass 2 found 2
  Major + 4 Minor; pass 3 found 5 Concerns + 2 Nits; pass 4 found
  2 Concerns + 5 Nits; pass 5 returned "Clean — ready to commit."
- Critique: the recurring failure mode across passes was
  **fabricated code references** — type names, function names,
  file paths, line numbers that read plausibly but didn't exist
  (`TextMatch`, `matchKind`, `Manager.EvaluateByName`,
  `internal/ui/search.go`, `internal/savedsearch/picker_test.go`,
  `redact.HashMessageID` referenced as pre-existing, `THREAT_MODEL
  §4.2`, `PRIVACY §3`, `*Engine` receiver where the interface
  doesn't have one). Each pass forced me to grep for the real
  symbols before re-writing. The §12.0 "verify every concrete
  claim" rule earned its place — the spec is now full of
  `file:line` cites whose accuracy was checked, not assumed. The
  width-wrap concern (renderer's `htmlToText` inserts newlines at
  display width; indexer needs pre-wrap text) was the most subtle
  finding and would have silently broken regex matches in
  production. `DecodeForIndex` is the resolution.
- Next: implementation when prioritised. The plan can be picked
  up cold from this file + the spec; the only state needed is the
  unchecked DoD boxes.

## Notes for the next implementer

- The `inkwell index rebuild` CLI walks `bodies`, not Graph. On
  default config (`[cache].body_cache_max_count = 500`) this means
  rebuild indexes ~500 messages. That is **expected**; the spec is
  explicit (§7.3). Do not silently widen the rebuild path to
  Graph round-trips — that would change the cost model the user
  consented to.
- The schema migration is opt-in-friendly: migration 015 creates
  empty tables. A user who never sets `[body_index].enabled =
  true` pays only the (negligible) schema-creation cost.
- `body_text.last_accessed_at` is bumped on both renderer
  `OnBodyDecoded` callbacks **and** on `SearchBody*` hits — the
  latter is a single UPDATE per query. Don't forget the
  per-query side effect; the LRU is meaningless without it.
- The `regex_post_filter_timeout` knob (§7) is a deliberate
  back-pressure mechanism for catastrophic-backtracking regexes.
  The implementation must use `context.WithTimeout` cancellation
  between iterations of the post-filter loop, not on the
  individual `regexp.MatchString` call (Go's `regexp` doesn't
  honor context).
- Adversarial-review pass 3 flagged the
  width-wrap-vs-indexing concern. The viewer pipeline is
  unchanged; the new `render.DecodeForIndex` is the only path
  that should feed `body_text`. Do not "reuse" the viewer's
  already-decoded string — it's wrapped.

### Iter 2 — 2026-05-15 (implementation slices 1-7)

- Slice 1: migration 015 + canary
  - Verifier: `internal/store/fts_probe_test.go` (trigram, porter,
    unicode61, ascii, external-content, detail=none, LIKE accel) red → green
  - `tabs_test.go:40` + `sender_routing_test.go:320` +
    `rules_test.go` schema-version asserts bumped to "15".
  - `store/AGENTS.md` invariant 1 amended with the
    cache-management write carve-out (EvictBodies +
    IndexBody/UnindexBody/EvictBodyIndex/PurgeBodyIndex).
- Slice 2: indexer (`internal/store/body_index.go`) — 9 new Store
  methods + 20 unit tests. SQL-injection probe (literal
  `'); DROP TABLE …`) red → green. `UpdateMessageFields` wired
  to propagate folder moves into `body_text.folder_id`.
- Slice 3: render hook + sync hook + config.
  - `render.DecodeForIndex` exports the pre-wrap html2text path;
    `decode_for_index_test.go` enforces the token-preservation
    invariant (no newlines mid-token).
  - `render.Options.OnBodyDecoded` callback; render package
    free of sync import.
  - `internal/sync/body_index_hook.go` bridges renderer →
    `store.IndexBody` with folder allow-list.
  - `maintenance.go` runs `EvictBodyIndex` after `EvictBodies`.
  - `[body_index]` config section + validation (max_body_bytes
    ≤ max_bytes/8; max_regex_candidates ≤ max_count×2).
- Slice 4: pattern integration.
  - `RegexValue` concrete type implementing `PredicateValue`.
  - `lexer.go` recognises `/.../` with `\/` escape.
  - `parser.go` admits regex on ~s / ~b / ~B; rejects on ~h and
    others; surfaces `regexp.Compile` errors with column position.
  - `CompileOptions.BodyIndexEnabled` + `MaxRegexCandidates`;
    selectStrategy step 0 → `StrategyLocalRegex`.
  - `CompileLocal` split into back-compat + `CompileLocalWithOpts`;
    body-field arms flip to `bt.content` JOIN when index on.
  - `eval_local_regex.go` extracts mandatory ≥3-char literals via
    `regexp/syntax` walk; refuses `ErrRegexUnboundedScan` when no
    predicate contributes a literal; `ExecuteAgainst` runs
    trigram narrow + Go regex post-filter with wall-clock cap.
  - `EvalEnv.BodyTextFor` enables lazy body lookup in the
    in-memory regex path (TwoStage refinement).
  - 16 pattern tests covering lexer / parser / strategy /
    literal extraction / structural-part carry / emit routing.
- Slice 6 (interleaved before docs sweep): CLI + logging.
  - `redact.HashMessageID` (truncated SHA-256, 16 hex chars).
  - `internal/sync/body_index_hook_test.go::TestMaybeIndexBody
    _RedactsNoSensitiveFields` enforces §8.5 (no msg id / folder
    id / content in the indexer error log line).
  - `Engine` interface gains `MaybeIndexBody` so the cmd layer
    wires the renderer callback without the unexported type.
  - `cmd/inkwell/cmd_run.go` threads the `[body_index].*` knobs
    into Engine.Options; renderer.OnBodyDecoded =
    engine.MaybeIndexBody. Startup auto-purge when
    `enabled = false` but rows exist (config flip detector).
  - `cmd/inkwell/cmd_index.go` — `status` / `rebuild` /
    `evict` / `disable` with --json, --folder, --limit, --force,
    --older-than, --message-id, --yes flags.
- Slice 7 (this iteration): docs.
  - `docs/CONFIG.md` `[body_index]` section.
  - `docs/ARCH.md` §6 Tier 3 + §7 schema-table rows.
  - `docs/PRD.md` §10 inventory row.
  - `docs/product/roadmap.md` Bucket 5 row updated.
  - `docs/THREAT_MODEL.md` new row + `internal/sync/
    body_index_hook_test.go` cite.
  - `docs/PRIVACY.md` "Where data is stored" row.
  - `docs/specs/06-search-hybrid/spec.md` §11 OOS bullet struck
    + back-reference to spec 35.

Result: spec 35 ships in stages (commits aaa65f0 + 5061ce2 + this
docs commit). The CLI + indexer + pattern integration are
end-to-end functional from a fresh inkwell binary. **Pending for
ship as v0.64.0:**
- Slice 5 (saved-search Manager BodyIndexEnabled threading;
  `LastCompileError` transient field; UI sidebar grey-out;
  `/regex:` search-mode prefix; status indicators; palette rows;
  `:index` cmd-bar verb) — wider UI surface, can land in a
  follow-up iteration.
- Slice 8 (integration test with 5k synthetic corpus; TUI e2e
  for regex search + :index; benchmarks for every §14 perf
  budget row; property-based literal extraction) — measurement
  gates the v0.64 tag.

Critique:
- The `ListBodyMessageIDs` shim in `cmd_index.go::rebuildFromBodyLRU`
  is a placeholder — the rebuild walks `ListMessages` and skips
  rows where `GetBody` misses. For the default 500-LRU config this
  is fine; for users with higher caps the iteration cost grows.
  Follow-up should add a dedicated `store.ListCachedBodies`
  helper.
- `bodyLRUSnapshot` in `cmd_index.go` returns zeroes; the
  `inkwell index status` output's "Body LRU" line is dormant
  until a dedicated stats helper lands. Cleanup deferred.
- The structural-carry in `CompileLocalRegex` re-uses
  `CompileLocalWithOpts` rather than emitting an inline WHERE —
  works, but produces fully-qualified `body_preview LIKE` /
  `bt.content LIKE` fragments depending on `opts.BodyIndexEnabled`.
  Acceptable for v1 since the regex path always has the index
  on, but worth tightening when the helper API is touched.
