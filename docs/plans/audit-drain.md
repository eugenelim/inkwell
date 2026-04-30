# Audit drain plan — retiring `docs/audits/spec-1-15-impl-gaps.md`

## Why this file exists

The spec 1–15 impl/design audit dropped a punch list of 113 gaps
across 15 specs (plus a top-10 leverage list). The user direction:
progressively address findings in PR-sized chunks ordered by
leverage, and as each gap is verified, fold it into the relevant
**spec** or **plan** file so the audit doc shrinks. When every row
has either been resolved or explicitly deferred in a spec plan
file, the audit file (and the `docs/audits/` directory) get
deleted.

This file is the tracker for that drain. Update on every audit-
draining PR.

## Movement rules — what "drained" means per row

For each audit row, one of three things happens:

1. **Implemented and verified.** The finding is resolved by code +
   tests in the PR. The audit row is **deleted**. The spec's plan
   file (`docs/plans/spec-NN.md`) gains an iteration-log entry
   noting the closed gap.

2. **Explicitly deferred.** The finding is moved (verbatim or
   refined) into the relevant `docs/plans/spec-NN.md` under a
   "Deferred" section, with a one-line WHY. The audit row is
   **deleted**.

3. **Reclassified.** The finding is wrong, out-of-scope, or
   superseded by a later spec decision. Replaced in the plan file
   with the rationale; audit row deleted.

The audit doc is **append-only-deletes-only** during the drain —
no edits to existing rows. This keeps `git log -p
docs/audits/spec-1-15-impl-gaps.md` an honest record of what got
addressed and when.

## Sequence — 12 PRs ordered by leverage

Numbering matches the audit's top-10 ranking where possible.
Effort estimates are calendar-day proxies; real PR count may
expand or contract as the work surfaces.

### PR 1 — Action queue undo (spec 07 §11) — `feat(spec-07): undo`
**Leverage:** #1. Users can't recover from a misclick. The store
table + helpers (`PushUndo` / `PopUndo` / `PeekUndo`) already
exist; the gap is wiring.

**Closes audit rows:**
- spec 07 §11 "undo unimplemented; `u` keybinding unhandled"
- spec 07 §7.1 `computeInverse` absent

**Slice:**
- `internal/action/inverse.go` — typed `Inverse(action) Action`.
- `internal/action/executor.go::run` — push undo entry on success.
- `internal/ui/app.go` — `u` handler in list + viewer dispatch.
- Tests: unit for `Inverse` per action type; dispatch test for
  `u`; e2e visible-delta (mark-read → u → message reverts to
  unread visibly).

### PR 2 — Bindings config + Help overlay (spec 04 §17, §12) — `feat(spec-04): bindings + help`
**Leverage:** #2 + partial #5. Right now `[bindings]` decodes from
TOML but is silently ignored.

**Closes:**
- spec 04 §17 "[bindings] silently ignored"
- spec 04 §12 "no `?` help overlay"
- spec 04 §6.4 "`:help` not registered"

**Slice:**
- `internal/ui/keys.go` — `applyBindingsOverrides(BindingsConfig)`
  with unknown-name validation (startup error with line number).
- `internal/ui/help.go` — full overlay model with section
  headers (Movement / Triage / Filter / Compose / etc.).
- `?` keybind handler; `:help` command.
- Tests: dispatch for `?`; dispatch for `:help`; config
  invalid-name produces typed error.

### PR 3 — Engine event emission (spec 03 §3) — `feat(spec-03): emit ThrottledEvent + AuthRequiredEvent`
**Leverage:** #3. UI handlers exist but the engine never sends
the events.

**Closes:**
- spec 03 §3 "ThrottledEvent never emitted (`OnThrottle` not
  forwarded)"
- spec 03 §3 "AuthRequiredEvent never emitted (auth retry
  doesn't propagate failure)"

**Slice:**
- `internal/sync/engine.go` — `OnThrottle` callback wires to
  `e.events <- ThrottledEvent{...}`.
- `internal/graph/client.go::authTransport` — surface a
  401-after-refresh as a typed error the engine can catch and
  emit `AuthRequiredEvent` for.
- Tests: integration via httptest — 429 → ThrottledEvent on
  channel; 401-after-refresh → AuthRequiredEvent.

### PR 4 — Triage verbs: D / m / c / C (spec 07) — `feat(spec-07): permanent-delete + categories + move`
**Leverage:** #4. `D`/`m`/`c`/`C` keybindings are declared but
unbound; the underlying executor branches don't exist.

**Closes:**
- spec 07 §6.7 "permanent_delete unimplemented end-to-end"
- spec 07 §6.9 / §6.10 "add_category / remove_category not in
  applyLocal or dispatch"
- spec 07 §12.1 "move-with-folder-picker absent"

**Slice:**
- `internal/graph/triage.go` — `PermanentDelete`,
  category PATCH (read-current-then-write-full-list).
- `internal/action/executor.go` — branches for the new types.
- `internal/ui/folder_picker.go` — modal for `m`.
- `internal/ui/categories.go` — picker for `c` / `C`.
- Tests: dispatch + e2e visible-delta for each verb;
  permanent-delete confirm modal default-No.

### PR 5 — Missing `:` commands (spec 04 §6.4) — `feat(spec-04): :refresh / :folder / :open / :backfill / :search / :rule / :save`
**Leverage:** #5. 8 of 15 commands are dead.

**Closes:**
- spec 04 §6.4 "8 commands unimplemented"
- spec 03 `:backfill` (referenced cross-spec; needs the engine's
  `Backfill(ctx, folderID, until)` already implemented).

**Slice:**
- `dispatchCommand` in `internal/ui/app.go` — register handlers
  for each. `:refresh` calls `Engine.Wake`; `:folder <name>`
  jumps via the folder list; `:open` opens the current
  message's webLink; `:save <name>` persists current filter as
  saved search; `:backfill` calls `Engine.Backfill`.
- `:rule` dispatches into PR 9's saved-search Manager (or stub
  until then).
- Tests: dispatch for each; e2e for the visible ones
  (`:refresh` shows `engineActivity`; `:folder Inbox` switches
  the list pane).

### PR 6 — Calendar schema + persistence + delta (spec 12) — `feat(spec-12): events table + delta sync`
**Leverage:** #6. Calendar is fetched live; no offline support.

**Closes:**
- spec 12 §3 "events / event_attendees tables never migrated"
- spec 12 §4.2 "calendar delta sync absent"
- spec 12 §5.1 "window slide at midnight absent"
- spec 12 §6 "calendar rendered as modal not pane (mismatch)"
- spec 12 §6.2 "j/k/Enter/]/[ keybindings absent"

**Slice:**
- Migration `004_calendar.sql` — events, event_attendees,
  indexes per spec §3.
- `internal/store/events.go` — CRUD.
- `internal/sync/calendar_sync.go` — third state in the engine
  loop; consumes `/me/calendarView/delta`.
- Window-slide goroutine.
- `internal/ui/calendar_pane.go` — sidebar pane (replaces the
  modal in §6) OR keep modal + add the missing keybindings;
  decide in the PR after re-reading spec 12.
- Tests.

### PR 7 — Drafts via action queue + crash recovery (spec 15) — `feat(spec-15): draft action types + compose_sessions`
**Leverage:** #7.

**Closes:**
- spec 15 §5 / §8 "drafts bypass action queue"
- spec 15 §7 "compose_sessions migration absent; no
  crash-recovery"
- spec 15 §6.2 "no ReplyAllSkeleton / ForwardSkeleton /
  NewSkeleton"
- spec 15 §10 "App crash mid-edit → resume prompt unimplemented"

**Slice:**
- Migration `005_compose_sessions.sql`.
- Add 4 typed actions to `store.ActionType` enum.
- Refactor `internal/action/draft.go` — enqueue + apply via the
  executor's optimistic + replay path.
- `internal/ui/compose.go` — startup checks for
  in-flight sessions and surfaces resume modal.
- ReplyAll / Forward / NewMessage skeleton functions.
- Tests.

### PR 8 — Hybrid search streaming (spec 06) — `feat(spec-06): Searcher / Stream / merge`
**Leverage:** #8. Whole spec is a stub; current `/` is a
single-shot 2s call.

**Closes:** every spec 06 row.

**Slice:**
- `internal/search/searcher.go` — `Searcher` interface,
  `Stream`, `Result` types.
- Local-first + server-second merge with debounce.
- Field-prefix parsing (`from:`, `subject:`).
- `:search` command dispatcher.
- UI status line streaming `[searching local]` →
  `[merged: N local, M server]`.
- Tests + bench (first-result <100ms).

### PR 9 — Pattern Compile/Execute + server evaluators (spec 08) — `feat(spec-08): server $filter and $search`
**Leverage:** #9.

**Closes:**
- spec 08 §6 "Compile/Execute API absent"
- spec 08 §3 "server-side evaluators missing"
- spec 08 §11 "two-stage execution absent"

**Slice:**
- `internal/pattern/compile.go` — strategy selection over
  existing `CompileLocal` + new `CompileFilter` /
  `CompileSearch`.
- `internal/pattern/execute.go` — `Execute(ctx, c, store, gc)`
  driving the strategy.
- Wire `~h` server-only path (currently rejects).
- Tests: ≥30 patterns through strategy table; explain output
  human-readable.

### PR 10 — Body fetch + attachments + viewer keybindings (spec 05) — `feat(spec-05): full headers + attachments + viewer keys`
**Leverage:** #10.

**Closes:**
- spec 05 §5.2 "body $select drift; no `attachments` /
  `internetMessageHeaders` / `$expand=attachments`"
- spec 05 §8 "no `GetAttachment` / save / open path"
- spec 05 §12 "viewer keybindings: o, O, e, Q, 1-9, a-z,
  Shift+A-Z, [, ] all absent"
- spec 05 §11 "thread map absent"

**Slice:**
- Fix `GetMessageBody` $select; add `GetAttachment`.
- `internal/render/attachments.go` — accelerator letters in
  rendering.
- Viewer dispatch handlers for each key.
- Path-traversal guard for attachment save (closes a deferred
  spec 17 §4.4 bullet too).
- Conversation-thread map under viewer.
- Tests including spec 17 path-traversal regression.

### PR 11 — Engine maintenance (spec 02 §8) — `feat(spec-02): periodic Vacuum + EvictBodies + action retention`
**Leverage:** moderate. The store's maintenance methods exist
but are never called.

**Closes:**
- spec 02 §8 "Vacuum never invoked"
- spec 02 §8 "EvictBodies dead at runtime"
- spec 02 §8 "actions retention sweep absent"

**Slice:**
- `internal/sync/maintenance.go` — periodic loop reading config
  caps; runs nightly.
- Wired into engine's run loop.
- Tests.

### PR 12 — Config defaults backfill (cross-cutting) — `feat(config): missing [triage] [batch] [bulk] [search] [calendar] [mailbox_settings] [cli] [pattern] [saved_search]`
**Leverage:** documentation+correctness. Most of these are
referenced by per-spec gaps above; this PR consolidates the
config surface.

**Closes:** every "Whole `[X]` section absent" row.

**Slice:**
- `internal/config/defaults.go` + `config.go` parsing.
- `docs/CONFIG.md` updates.
- Validation errors with line numbers (existing pattern).
- Tests for round-trip + invalid-key rejection.

## Execution rules

1. **One PR per slice above.** Don't bundle. Bundling makes the
   audit-drain bookkeeping unmanageable.
2. **Each PR's commit message lists the audit rows it closes** by
   the same format used here (`spec NN §X.Y "<row text>"`).
3. **Each PR deletes the closed rows from `docs/audits/spec-1-15-
   impl-gaps.md`** in the same commit. The diff makes the
   bookkeeping mechanical.
4. **Each PR appends an iteration-log entry to
   `docs/plans/spec-NN.md`** for whichever spec it touches. If a
   PR spans multiple specs (e.g. PR 5's `:rule` touches specs 04
   and 11), update both plan files.
5. **Deferred rows go into the spec plan file's "Deferred"
   section** with a one-line WHY, and the audit row gets deleted.
6. **When the audit doc has no rows left** (only headers /
   summary), delete `docs/audits/spec-1-15-impl-gaps.md`. Then
   delete the `docs/audits/` directory if no other audits live
   there. Update `CLAUDE.md` §14 "Where things live" to remove
   the `audits/` line if it's listed (it isn't currently).

## Status tracker

| PR | Spec(s) | Status | Branch | Audit rows closed | Plan file updated |
|----|---------|--------|--------|-------------------|-------------------|
| 1  | 07      | shipped (v0.13.x) | main | spec 07 §11 undo + §7.1 inverse | docs/plans/spec-07.md iter 2 |
| 2  | 04      | shipped (v0.13.x) | main | spec 04 §17 [bindings] + §12 help overlay + §6.4 :help | docs/plans/spec-04.md iter 9 |
| 3  | 03      | shipped (v0.13.x) | main | spec 03 §3 ThrottledEvent + AuthRequiredEvent | docs/plans/spec-03.md iter 8 |
| 4a | 07      | shipped (v0.13.x) | main | spec 07 §6.7 permanent_delete | docs/plans/spec-07.md iter 3 |
| 4b | 07      | not-started (categories + move-with-picker) | — | — | — |
| 5  | 04      | shipped (v0.13.x) | main | spec 04 §6.4 :refresh / :folder / :open / :backfill / :search | docs/plans/spec-04.md iter 10 |
| 5b | 04 (+11)| not-started (`:save` + `:rule` block on spec 11) | — | — | — |
| 6a | 12      | shipped (v0.13.x) | main | spec 12 §3 events schema + persistence | docs/plans/spec-12.md iter — |
| 6b | 12      | not-started (sync pass / window slide / detail modal / pane layout) | — | — | — |
| 7  | 15      | not-started | — | — | — |
| 8  | 06      | not-started | — | — | — |
| 9  | 08      | not-started | — | — | — |
| 10 | 05 (+17)| not-started | — | — | — |
| 11 | 02      | not-started | — | — | — |
| 12 | config  | not-started | — | — | — |

When all rows show "shipped" and the audit doc is empty, this
plan file (`audit-drain.md`) gets a final commit deleting it
along with the audit doc.
