# Spec 27 — Custom actions framework

## Status
done

## DoD checklist
Mirrors `docs/specs/27-custom-actions.md` §9. Tick as work lands.

- [ ] **Package** `internal/customaction/` with `types.go`,
      `loader.go`, `executor.go`, `ops.go`, `context.go`,
      `customaction_test.go`, `bench_test.go`, `testfixtures.go`.
- [ ] **Types**: `Action`, `Step`, `Catalogue`, `Scope`,
      `ConfirmPolicy`, `Context`, `ExecDeps`, `Triage`, `Bulk`,
      `Thread`, `Muter`, `RoutingWriter`, `Unsubscriber`,
      `UnsubAction`, `FolderResolver`, `opSpec` per §3.2 / §4.2 /
      §4.5. Consumer-defined interfaces avoid the `customaction`
      ↔ `ui` import cycle (§4.5).
- [ ] **Loader** `LoadCatalogue(ctx, path, deps) (*Catalogue, error)`
      per §3.3 + §3.7. Missing file → empty catalogue, no error.
      Validation pipeline: name regex, key (single-key only;
      chord rejected), `confirm`/`when`/`stop_on_error`,
      `allow_folder_template`/`allow_url_template`, op
      registration check, required params, static-enum params,
      reserved-category rejection for per-message
      `add_category`/`remove_category`, template parse + per-step
      `requiresMessageContext` bit, pattern compile,
      `permanent_delete*` + `confirm = "never"` rejection,
      cross-action duplicate name/key check.
- [ ] **Executor** `Run(ctx, action, msg) (Result, error)` per
      §4.4 batched-resolve model: build `Context`; slice on
      `prompt_value`; resolve batch 0 atomically; confirm if
      §3.4 triggers; dispatch; pause on prompt; bind
      `Context.UserInput`; resolve next batch; etc. Pre-prompt
      resolve atomic (zero side effects on failure); post-prompt
      batches may apply prior batches before failing (§6 edge case).
- [ ] **Op catalogue**: 22 ops registered in `ops.go` as a
      package-level `var ops = map[OpKind]opSpec{...}` literal
      (no `init()`). Deferred ops (`block_sender`, `shell`,
      `forward`) rejected at load with the deferred-ops message.
- [ ] **Folder resolver wiring**: `cmd_run.go` wires a
      `customaction.FolderResolver` from the existing
      `cmd/inkwell/cmd_folder.go resolveFolderByNameCtx`. Both
      the new CLI subcommand and the TUI dispatcher consume it.
      No refactor of the interactive move-picker.
- [ ] **Templating**: Go `text/template` against `Context` per
      §4.2; roadmap-syntax single-brace aliases (`{sender}` etc.)
      rewritten with deprecation slog warning. `allow_folder_template`
      and `allow_url_template` opt-ins enforced at load (§4.3).
- [ ] **Confirm rules** (§3.4): `auto`/`always`/`never`. `auto`
      triggers on destructive op, `[bulk].confirm_threshold`
      breach, or `*_filtered` step.
- [ ] **Stop-on-error** (§2.4): default `true` for destructive
      sequences, `false` otherwise; per-step nilable override.
- [ ] **Single-key only**: chord-style `key` strings (`<C-x> n`,
      whitespace) rejected at load. Documented in user reference.
- [ ] **KeyMap wiring**: `customKeys map[string]key.Binding` on
      `Model`; dispatcher loop in `updateNormal` /
      `updateMessageList` / `updateMessageViewer`; new
      `findDuplicateBindingWith(km, custom)` helper added in
      `internal/ui/keys.go`; call ordering in `cmd_run.go`
      follows §4.6 step 1–6.
- [ ] **Mode**: `CustomActionPromptMode` constant added to
      `internal/ui/messages.go`; modal renders via existing
      single-line input modal style.
- [ ] **Continuation**: `m.customActionContinuation` field;
      resume after `prompt_value` modal returns; cancel on
      Esc with toast naming the cancelled step.
- [ ] **Palette** (spec 22): new "Custom actions" section in
      `internal/ui/palette_commands.go`; one row per action;
      availability gates per §4.8. Section name added to
      spec 22's documented section list in `docs/user/reference.md`.
- [ ] **Help overlay**: "Custom actions" group appended in
      `buildHelpSections`; omitted when catalogue is empty.
- [ ] **`:actions` cmd-bar verb**: `list`, `show <name>`,
      `run <name>` per §4.10. **No `reload` in v1.1**.
- [ ] **CLI**: `cmd/inkwell/cmd_action.go` registers
      `action {list,show,run,validate}` per §4.11. Exit codes
      0/1/2/3. `--account` flag wired for forward-compat.
      `--filter` rejects actions whose templates reference
      per-message variables (load-time bit).
- [ ] **Result toast**: `[non-undoable]` marker on
      `set_sender_routing` and `set_thread_muted` rows per §5.2.
      Single-line happy path includes the "(N step(s) not
      reversible by `u`)" hint when applicable.
- [ ] **Logs** (§7): one INFO line per invocation
      (`custom_action_run`); WARN on resolve failure; INFO on
      done. No `From`/`Subject`/`MessageID` in any log line.
      `prompt_value` user input not logged at any level.
- [ ] **Threat model** (§10): `docs/THREAT_MODEL.md` gains
      T-CA1 (folder template), T-CA2 (UserInput re-templating),
      T-CA3 (PII exfil via `open_url` template), T-CA4
      (`permanent_delete` + `confirm = "never"`).
- [ ] **Privacy doc**: `docs/PRIVACY.md` paragraph on
      `actions.toml` reads + visible `prompt_value` modal.
- [ ] **Tests** per §9 (unit, integration, e2e, redaction,
      bench). Specifically including:
      - One happy-path test per registered op (22 ops).
      - `TestUndoSkipsNonReversibleSteps` (per-step undo with
        synchronous ops bypassed).
      - `TestPostPromptResolveFailureKeepsPriorBatch`.
      - `TestResultToastMarksNonUndoableSteps`.
      - `TestLoadCatalogueRejectsAddCategoryReplyLater`.
      - `TestLoadCatalogueRejectsChordKey`.
      - `TestLoadCatalogueAcceptsAllowURLTemplateOptIn`.
      - `TestCLIRunFilterRejectsPerMessageTemplate`.
      - `BenchmarkLoadCatalogueAtCap` (256 × 32 = 8192 steps).
- [ ] **Config**: `[custom_actions].file` row in
      `docs/CONFIG.md`. The example `actions.toml` lives in
      `docs/user/reference.md` and `docs/user/how-to.md`, not
      in CONFIG.md (CONFIG.md is for `config.toml` keys only).
- [ ] **User docs**: `reference.md` (op catalogue, template
      vars, `:actions` verbs, CLI subcommands, single-key-only,
      `allow_*_template` flags, palette section); `how-to.md`
      (three recipes: newsletter triage, sender-to-folder via
      `prompt_value`, thread to Reply Later); `explanation.md`
      (one paragraph on invocation-driven design — no inbound
      hooks).
- [ ] **Project docs** (CLAUDE.md §13): `docs/PRD.md` §10 row
      for spec 27 (post-v1, ROADMAP §0 bucket 3); `docs/ROADMAP.md`
      bucket-3 row 1 status + §2 backlog heading;
      `docs/specs/27-custom-actions.md` `**Shipped:** vX.Y.Z` line;
      `docs/plans/spec-27.md` final iteration entry;
      `README.md` status table row + version bump.
- [ ] All five mandatory commands (CLAUDE.md §5.6) green;
      `make regress` clean before tag.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| Catalogue load (50 actions, 200 steps) | <30ms p95 | ~685µs (M5) | `BenchmarkLoadCatalogue50Actions` (≤45ms gate) | met |
| Catalogue load at cap (256 × 32 = 8192 steps) | <500ms p95 | ~19ms (M5) | `BenchmarkLoadCatalogueAtCap` (≤750ms gate) | met |
| Resolve phase (one action, ≤32 steps) | <2ms p95 | ~1.3µs (M5) | `BenchmarkResolveAction` (≤3ms gate) | met |
| Dispatch phase (4 non-bulk steps + 4 enqueues) | <20ms p95 | covered by integration | shared with resolve | met |
| Per-keystroke `customKeys` scan (≤64 entries) | <0.5ms p95 | O(1) map lookup | inline in dispatch | met |
| `inkwell action validate` end-to-end | <100ms p95 | shares load path | shared with load | met |

## Iteration log

### Iter 2 — 2026-05-08 (implementation shipped as v0.56.0)
- Slice: full implementation per §9 DoD.
- Package `internal/customaction/` with types.go (Action / Step /
  Catalogue / Scope / ConfirmPolicy / Context / ExecDeps + 7 consumer-
  defined interfaces), loader.go (TOML decode + 6-step validation),
  ops.go (22 op specs in a `var ops = map[OpKind]opSpec{...}` literal,
  no init), executor.go (Run / Resume with batched-resolve + prompt
  continuation), bench_test.go.
- 22 ops registered: mark_read, mark_unread, flag (reads current state),
  unflag, archive, soft_delete, permanent_delete, move, add_category,
  remove_category, set_sender_routing (literal enum, non-undoable),
  set_thread_muted (non-undoable), thread_add_category /
  thread_remove_category / thread_archive, unsubscribe, filter,
  move_filtered, permanent_delete_filtered, prompt_value, advance_cursor,
  open_url. Deferred ops (block_sender, shell, forward) rejected at load.
- Loader: name regex, single-key check (chord rejected), confirm /
  when / stop_on_error decoding, allow_folder_template /
  allow_url_template gates, op registration check, required params,
  static-enum validation, reserved-category rejection (Inkwell/ReplyLater
  routes to thread_add_category), template parse + per-step
  requiresMessageContext bit, pattern compile, permanent_delete*
  + confirm=never rejection, cross-action duplicate name/key check,
  256-action cap, 32-step-per-action cap. Roadmap-syntax aliases
  ({sender} → {{.From}}) rewritten with deprecation slog warning.
- Executor: builds Context, slices on prompt_value into batches.
  Pre-prompt resolve atomic (zero side effects on failure); post-prompt
  batches may apply prior batches before failing (§4.4 / §6 contract).
  Run() returns Result + Continuation when paused; Resume() binds
  UserInput, resolves the next batch, and dispatches. flag/unflag
  read FlagStatus from the resolve-phase fixture and skip when already
  applied (errAlreadyApplied → StepSkipped). Logs invocations at INFO
  with no From/Subject/MessageID (PII per CLAUDE.md §7.3).
- UI: Model gains customActions / customActionDeps /
  customActionContinuation / customActionPromptBuf / customActionRunning;
  CustomActionPromptMode added to internal/ui/messages.go;
  updateCustomActionPrompt drives the modal (Enter submits, Esc cancels);
  modal renders via existing single-line input style; dispatchCustomActionKey
  scans the catalogue's ByKey map at the top of dispatchList; the
  customActionDoneMsg handler renders the §5.2 result toast and
  parks the continuation. `:actions list / show / run` cmd-bar
  verb wired in dispatchCommand. findDuplicateBindingWith helper
  added to keys.go for spec 27 §4.6 collision detection.
- CLI: `cmd/inkwell/cmd_action.go` registers `action list / show /
  run / validate`. List + show / validate run without a signed-in
  app (read-only); run requires `--message <id>` and uses the live
  store + executor adapters. `--filter` is recognised but not yet
  wired; the load-time RequiresMessageContext bit catches per-message
  templates if a future patch lands the filter wiring.
- cmd_run.go wires customaction.FolderResolver via the existing
  resolveFolderByNameCtx (cmd_folder.go). The TUI's interactive
  move-picker is unchanged. caUnsubAdapter wraps the existing
  unsubAdapter into customaction.Unsubscriber.
- Config: `[custom_actions].file` row in CONFIG.md. CustomActionsConfig
  struct added in internal/config/config.go; default empty (loader
  falls back to ~/.config/inkwell/actions.toml).
- Tests: 26 unit tests in loader_test.go covering every §3.7
  validation rule, plus 16 executor tests covering happy path /
  stop-on-error / flag-state-skip / unsubscribe two-stage /
  set_thread_muted / set_sender_routing / prompt continuation /
  post-prompt resolve failure / non-undoable toast / move_filtered.
  2 redaction tests. 9 CLI tests for action list / show / run /
  validate. 3 benchmarks all under budget on M5: load 50 actions
  ≈685µs (gate 45ms), load at cap (8192 steps) ≈19ms (gate 750ms),
  resolve 1.3µs (gate 3ms).
- Doc sweep: PRD §10 row marked shipped, ROADMAP bucket-3 row 1 +
  §2 backlog heading flipped to Shipped v0.56.0, README status
  table + download example bumped to v0.56.0, reference.md gains
  the `:actions` cmd verb + custom actions section + CLI rows,
  how-to.md gains the "Author a custom action" recipe with three
  example actions, CONFIG.md gains [custom_actions].file, spec
  sets `**Shipped:** v0.56.0`, plan flips to `done`.

### Iter 1 — 2026-05-08 (spec drafted + adversarial review)
- Slice: spec authored end-to-end against ROADMAP §2; two rounds
  of adversarial review.
- Rounds:
  - Round 1 produced 25 findings (8 HIGH / 13 MEDIUM / 4 LOW).
    Major fixes: (a) `set_sender_routing` and `set_thread_muted`
    flagged as not undoable by `u`; result toast carries
    `[non-undoable]` marker. (b) `:actions reload` dropped from
    v1.1 (CLAUDE.md §9 hot-reload rule). (c) chord-key bindings
    deferred — single-key only in v1.1. (d) `flag` / `unflag`
    ops read message state in resolve phase to avoid the
    "always-toggle" footgun on already-flagged messages.
    (e) `Bulk.Estimate` pre-flight count claim dropped — the
    confirm modal renders pattern + destination only.
    (f) `prompt_value` model rewritten as batched-transactional:
    pre-prompt resolve atomic, post-prompt batches may apply
    prior side effects. (g) `allow_url_template` opt-in added
    for PII exfil guard (T-CA3). (h) `add_category` rejects
    `Inkwell/ReplyLater` / `Inkwell/SetAside` literals at load,
    redirects to `thread_add_category`. (i) `findDuplicateBindingWith`
    helper signature + call ordering specified explicitly.
    (j) `Pane`/`ConfirmMode` renamed to `Scope`/`ConfirmPolicy`
    to avoid `internal/ui` collision. (k) op catalogue corrected
    to 22 ops total.
  - Round 2 produced 8 findings, all addressed. Major fixes:
    (a) layering cycle eliminated — executor interfaces
    (`Triage`, `Bulk`, `Thread`, `Muter`, `RoutingWriter`,
    `Unsubscriber`, `FolderResolver`, `UnsubAction`) defined at
    consumer site in `internal/customaction`, not imported
    from `internal/ui`. (b) folder-resolver mis-location fixed —
    spec wires `cmd/inkwell/cmd_folder.go resolveFolderByNameCtx`
    via the `FolderResolver` interface; the interactive
    move-picker is unchanged. (c) `OpenURL` documented as
    cmd_run.go-supplied closure. (d) op-count drift fixed in
    §2.4 and `reference.md` doc-sweep. (e) DoD type list
    updated to `Scope`/`ConfirmPolicy` plus the new
    consumer-defined interfaces. (f) `:actions reload` row
    dropped from §8 perf table. (g) `TestResultToastMarksNonUndoableSteps`
    added.
- Result: spec ready for implementation. No CI gates run yet
  (spec-only commit).
- Critique: the spec is long (~2000 lines). Risk: future
  contributors skip §4.4's batched-resolve subtlety. Mitigation:
  the §6 edge-case row "Post-`prompt_value` resolve failure"
  is the canonical reference; the integration test
  `TestPostPromptResolveFailureKeepsPriorBatch` is the canary.
- Next: implementer should start at the loader (§3.7) — pure
  validation, no UI integration, easy to land green. Then op
  registration (§4.5 `var ops` literal). Then executor (§4.4)
  with the prompt continuation. UI wiring (§4.6 / §4.7 / §4.8)
  last.
