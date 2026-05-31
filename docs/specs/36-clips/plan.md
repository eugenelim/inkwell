# Plan: Clips (knowledge fragments)

- **Spec:** [`spec.md`](spec.md)
- **Status:** Drafting <!-- Drafting | Executing | Done -->

> **Plan contract:** this is the implementation strategy. Unlike the spec, this
> document is allowed to change as you learn. When it changes substantially
> note why in the changelog at the bottom.

## Approach

Build bottom-up: schema → store API → config, then the two UI surfaces
(visual selection mode, `:clips` overlay), then the CLI, then the
cross-cutting privacy/redaction/docs pass. The store and config layers
(T1, T2, T3) are independent of the UI and of each other except T2→T1,
so the plan is shaped to let the supervisor dispatch the leaf tasks
(T1, T3, T4) in parallel.

The riskiest part is **T4 — visual line-selection mode** in the viewer.
The viewer has no existing per-line cursor or selection concept (only a
scroll offset), so this introduces genuinely new interaction state and a
new render path (reverse-video range). It is gated entirely behind a
`selectActive` flag so that, when off, every existing viewer gesture is
byte-for-byte unchanged — the regression risk is "did we silently change
normal-mode `y`/`j`/`k`," which a dedicated test pins down. The second
subtlety is the `y` keybinding collision: bare `y` is already URL-yank,
so clip-save is bound to `y` *only inside* select mode; the disambiguation
is mode state, not a new key.

Everything else follows shipped precedent: the FTS5 table mirrors
`messages_fts` (migration 001) and the body-index table (spec 35,
migration 015); the overlay mirrors `urlpicker.go` / `ListModel`; the CLI
mirrors `inkwell index` (spec 35).

## Constraints

- `docs/CONVENTIONS.md` §7 invariant 4 — no `Mail.Send`; clips make no
  Graph calls (`scripts/check-no-mail-send.sh` stays green).
- Spec 17 — new on-disk content surface + clipboard egress; THREAT_MODEL
  + PRIVACY updated in this PR; redaction tests at every new log site.
- `docs/CONVENTIONS.md` §16 — schema-version regression sites
  (tabs_test.go, rules_test.go, sender_routing_test.go) must move 15→16.
- Pure-Go / modernc SQLite invariant (`docs/CONVENTIONS.md` §1) — FTS5
  uses only the built-in `unicode61` tokenizer already in use; no CGO,
  no extensions.

## Construction tests

Most tests live under their task's `Tests:` subsection. Cross-cutting:

**Integration tests:**
- TUI e2e: `v` → extend → `y` saves a clip that then appears in the
  `:clips` overlay; `Enter` jumps to source and scrolls to the line.
- TUI e2e: source-message permanent-delete → clip survives, overlay
  shows "source unavailable", `Enter` does not crash.
- CLI: `clips list` after a TUI save reflects the new clip;
  `export --format md` round-trips.

**Manual verification:** `make ai-fuzz` smoke + Claude-oracle pass over
`.context/ai-fuzz/run-*/REVIEW.md` (TUI surface, spec §10 + §12).

## Tasks

### T1: migration 016 + schema-version sites green

**Depends on:** none
**Touches:** internal/store/migrations/016_clips.sql, internal/store/store.go, internal/store/tabs_test.go, internal/store/rules_test.go, internal/store/sender_routing_test.go

**Tests:**
- Migration runner applies 016 cleanly on a fresh DB and on a DB at
  version 15; `PRAGMA user_version` (or the store's version query) reads 16.
- `clips` + `clips_fts` + the three triggers exist; an INSERT into `clips`
  populates `clips_fts` (probe via a MATCH after insert).
- The three schema-version assertion sites (spec §4.1) read 16 and pass.
  Re-grep the sites first — line numbers may have drifted from §4.1.

**Approach:**
- Write `016_clips.sql` exactly as spec §4 (table, indexes, FTS5 vtab,
  ai/ad/au triggers; `ON DELETE SET NULL` on `source_message_id`,
  `ON DELETE CASCADE` on `account_id`). **Final statement must be**
  `UPDATE schema_meta SET value = '16' WHERE key = 'version';` — without
  it the store refuses to open (spec §4 / §4.1).
- Bump `SchemaVersion` 15 → 16 at `internal/store/store.go:22`.
- Update tabs_test.go:40, rules_test.go:21, and the sender_routing_test.go:320
  OR-list.

**Done when:** `go test ./internal/store/...` passes with the new
migration applied and all three version assertions green.

### T2: store clips API

**Depends on:** T1
**Touches:** internal/store/clips.go, internal/store/clips_test.go, internal/store/clips_integration_test.go, internal/store/store.go

**Tests:**
- CRUD round-trip: SaveClip → GetClip → ListClips (newest-first) →
  UpdateClipLabel → DeleteClip; each verified.
- `SaveClip` returns a usable id; `saved_at` set; `source_message_id`
  NULL ↔ `""` mapping correct on read.
- `SearchClips` matches on text, label, subject, and sender; BM25 order;
  empty query and no-match return empty, not error.
- **Injection probe**: a query of `'; DROP TABLE clips; --` and a hostile
  FTS operator string leave the table intact and return no rows/clean
  error (spec §6.2). Asserts zero `// #nosec`.
- `ON DELETE SET NULL`: delete the referenced `messages` row →
  `GetClip` shows `SourceMessageID == ""`; `ClipStats.Orphaned` increments.
- `CountClips` accurate; `ClipStats` aggregates (Count/Bytes/Oldest/Newest/Orphaned).
- Verifies Acceptance Criteria: schema, ON DELETE SET NULL, FTS search,
  cap-supporting count (spec §10).

**Approach:**
- Implement the 8 methods from spec §6.1 with parameterised SQL only.
- `SourceMessageID` via `sql.NullString` on read; NULL on write when "".
- FTS MATCH via a store-local `clipsMatchQuery` escaper beside clips.go
  (double-quote terms); do NOT import `internal/search` (upward cycle —
  spec §6.2). User query bound as `?`, never concatenated.

**Done when:** `go test -race ./internal/store/...` green including the
injection probe and the SET-NULL test.

### T3: `[clips]` config + validation

**Depends on:** none
**Touches:** internal/config/config.go, internal/config/validate.go, docs/CONFIG.md

**Tests:**
- Defaults load as spec §7 (enabled true, copy_to_clipboard true,
  prompt_for_label false, max_clips 10000, max_clip_bytes 65536).
- Validation rejects max_clip_bytes < 256 or > 1 MB and max_clips < 100
  with the standard validation error.

**Approach:**
- Add the `[clips]` struct mirroring the `[body_index]` section shape.
- Add the two validation rules to `validate.go`.
- Document the section in CONFIG.md (table from spec §7).

**Done when:** config round-trip + validation tests green; CONFIG.md has
the `[clips]` section.

### T4: viewer visual line-selection mode

**Depends on:** none
**Touches:** internal/ui/viewer_select.go, internal/ui/viewer_select_test.go, internal/ui/panes.go, internal/ui/keys.go, internal/ui/app.go

**Tests:**
- State transitions: `v` sets selectActive, anchor=cursor; `j`/`k` move
  cursor within bounds; `G`/`gg` extend to ends; `Esc` clears.
- Selected range = `[min(anchor,cursor), max(anchor,cursor)]`; render
  marks those lines (assert on rendered frame, visible-delta).
- **Regression: normal-mode gestures unchanged** — with selectActive
  false, `y` still URL-yanks, `j`/`k` still scroll (spec §10 criterion).
- Blank-line trimming + empty-selection → "nothing to clip" guard.

**Approach:**
- Add selectActive/selectAnchor/selectCursor to `ViewerModel` (panes.go).
- `viewer_select.go`: enter/extend/cancel ops + range-highlight render
  helper; selection text extractor (join + trim blanks).
- `keys.go`: add `VisualSelect` ('v'); reuse Yank ('y') inside select mode.
- `app.go` `dispatchViewer`: route `v` and the select-mode key table
  (spec §5.2). Clip-save wiring itself lands in T5.

**Done when:** `viewer_select_test.go` green; normal-mode regression test
green; visible-delta frame test shows the highlighted range.

### T5: yank-saves-clip wiring

**Depends on:** T2, T3, T4
**Touches:** internal/ui/app.go, internal/ui/viewer_select.go

**Tests:**
- TUI e2e: `v` → `j` → `y` calls SaveClip with joined text, source id,
  provenance, line range; status shows line count; mode exits.
- `copy_to_clipboard=true` also invokes yanker; a forced yanker error
  leaves the clip saved and surfaces "(clipboard unavailable)".
- At `max_clips`, save refused with the ceiling message; no insert.
- `prompt_for_label=true` opens the label input; Enter saves with label,
  Esc saves empty.

**Approach:**
- In select-mode `y`: build text (T4 extractor), enforce max_clip_bytes
  truncation + max_clips ceiling (CountClips), SaveClip, optional yanker
  copy (best-effort), optional label prompt, status string.
- Pull provenance (subject/sender/received-at) from the open message model.

**Done when:** the e2e gesture test green; clipboard-failure and ceiling
paths covered.

### T6: `:clips` overlay

**Depends on:** T2
**Touches:** internal/ui/clips_overlay.go, internal/ui/clips_overlay_test.go, internal/ui/app.go

**Tests:**
- TUI e2e: `:clips` opens overlay listing clips newest-first; typing
  filters via SearchClips; rows show label-or-first-line + provenance.
- `Enter` on a clip with a live source loads that message and scrolls to
  line_start; with a gone source shows "source unavailable", no crash.
- `dd` deletes (confirm); `e` edits label and persists.

**Approach:**
- `clips_overlay.go`: model mirroring urlpicker.go / ListModel — search
  input + scrollable result list + key table (Enter/dd/e/y/Esc).
- `app.go` `dispatchCommand`: add the `:clips` case routing to the overlay
  (mirror the `:index` → `dispatchIndexCmdBar` pattern).
- Jump-back: resolve source via GetMessage; on miss, status + keep open.

**Done when:** overlay e2e tests green including the source-gone path.

### T7: command palette rows

**Depends on:** T6
**Touches:** internal/ui/palette_commands.go

**Tests:**
- Palette includes an "Open clips" row (always available) and a
  "Save clip from selection" row available only when the viewer is in
  select mode; RunFn dispatches correctly.

**Approach:**
- Add a `buildClipsPaletteRows(m)` builder; append in `collectPaletteRows`.
- Set `Available` predicates to match viewer/select state.

**Done when:** palette test green; rows appear with correct availability.

### T8: CLI `inkwell clips` family

**Depends on:** T2
**Touches:** cmd/inkwell/cmd_clips.go, cmd/inkwell/cmd_clips_test.go, cmd/inkwell/cmd_root.go

**Tests:**
- `clips list` / `search` / `show` emit valid JSON under `--output json`
  and a table otherwise; `delete` confirms unless `--yes`; `export
  --format md|json|txt` round-trips a known clip set.
- `show <missing-id>` exits non-zero with "clip <id> not found".

**Approach:**
- `newClipsCmd(rc)` parent + subcommands, mirroring `newIndexCmd`
  (cmd_index.go). Register in cmd_root.go alongside `newIndexCmd`.
- `export --format md`: one `##` section per clip with provenance + fenced
  text.

**Done when:** `go test ./cmd/inkwell/...` green; subcommands behave per
spec §9.

### T9: redaction audit + tests

**Depends on:** T2, T5, T6, T8
**Touches:** internal/store/clips_redact_test.go, cmd/inkwell/cmd_clips_redact_test.go, internal/store/clips.go, internal/ui/app.go, cmd/inkwell/cmd_clips.go

**Tests:**
- Capture logs at every clips log site (store, UI status path, CLI);
  assert no clip text, label, subject, sender, or raw message ID appears
  at INFO+; DEBUG correlation uses HashMessageID only.

**Approach:**
- Audit each new log call; route any message-id correlation through
  `redact.HashMessageID`; drop text/label from logs entirely.

**Done when:** redaction tests green at store + CLI sites (spec §8.5).

### T10: privacy / threat-model / user docs

**Depends on:** T1, T2, T3, T4, T5, T6, T8
**Touches:** docs/THREAT_MODEL.md, docs/PRIVACY.md, docs/architecture/overview.md, docs/user/reference.md, docs/user/how-to.md, docs/backlog.md

**Tests:**
- Goal-based: `make doc-sweep` clean; `make regress` doc checks pass;
  THREAT_MODEL + PRIVACY contain the new rows; PR body has the spec-17
  impact line; the two deferred anchors (`#clips-cli-add`,
  `#clips-bulk-delete`) resolve to headings in `docs/backlog.md`.

**Approach:**
- Add the THREAT_MODEL rows + residual-risk entry (spec §8.1), PRIVACY row
  (§8.2), overview.md schema-table + module-tree rows, reference.md
  entries (`v`, `:clips`, `inkwell clips`), how-to recipe.
- Create the two `docs/backlog.md` headings the spec §10 deferred markers
  point at (`#clips-cli-add`, `#clips-bulk-delete`) so the deferrals
  don't dangle.

**Done when:** docs updated; doc-sweep clean.

### T11: perf benchmarks meet budgets

**Depends on:** T2, T6
**Touches:** internal/store/clips_bench_test.go, internal/ui/clips_overlay_test.go

**Tests:**
- BenchmarkSaveClip, BenchmarkSearchClips_10k, BenchmarkListClips_10k,
  BenchmarkClipsOverlayOpen — assert the spec §11 budgets.

**Approach:**
- Seed 10k clips; benchmark save/search/list; UI bench for overlay open.

**Done when:** benchmarks recorded and within budget; numbers noted in
this plan's changelog.

### T12: ai-fuzz closing ritual

**Depends on:** T4, T5, T6, T7
**Touches:** (none — verification only)

**Tests:**
- `make ai-fuzz` smoke completes; Claude-oracle pass over
  `.context/ai-fuzz/run-*/REVIEW.md` finds no clip-related regression.

**Approach:**
- Run `STEPS=30 make ai-fuzz`; review REVIEW.md; file any findings.

**Done when:** REVIEW.md clean of clip regressions (spec §10 final
criterion).

## Rollout

Ships behind `[clips].enabled` (default **true** — opt-out). Nothing is
auto-populated, so "on by default" carries no silent-storage cost: the
table stays empty until the user yanks. Reversible: set `enabled = false`
to hide the feature without destroying saved clips. Fully removable: drop
the `clips` table (no other feature depends on it).

## Risks

- **Visual-selection mode regressing existing viewer gestures (T4).** The
  whole interaction is new viewer state; the mitigation is the
  `selectActive` gate + a dedicated normal-mode regression test asserting
  `y`/`j`/`k` are unchanged when the flag is off.
- **`y` keybinding ambiguity.** Resolved by mode (bare `y` = URL-yank;
  select-mode `y` = save clip). If user testing finds the mode confusing,
  the fallback is a distinct save key (`Y`) — note in changelog if taken.
- **Body line indexing drift.** `line_start`/`line_end` are captured
  against the rendered body at save time; a later re-render at a different
  width could shift line numbers, so jump-back scroll is best-effort
  ("scroll near", not "exact line"). Documented as a known limitation in
  reference.md.
- **Clipboard egress surprise.** `copy_to_clipboard` default-true sends
  clip text to OSC 52 / pbcopy; called out in THREAT_MODEL (§8.1) and
  user docs so SSH/multiplexer users can disable it.

## Changelog

- 2026-05-31: initial plan.
