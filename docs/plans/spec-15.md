# Spec 15 — Compose / Reply (drafts only)

## Status
in-progress. v1: viewer-pane reply via `$EDITOR` shipped
post-v0.10.0. v0.13.x: drafts flow through the action queue with
two-stage idempotent dispatch (PR 7-i). **v0.13.x spec rewrite:**
in-modal compose pane replaces the editor-driven flow (real-tenant
"select Exit command first" friction) — see iter 3. Reply-all /
forward / new message skeletons (PR 7-iii), `compose_sessions`
crash recovery (PR 7-ii), and `Ctrl+E` `$EDITOR` drop-out remain
deferred.

## DoD checklist (mirrored from spec)
- [x] `internal/compose/`: template (reply skeleton), parse (RFC2822-style headers), editor (tempfile + `$INKWELL_EDITOR` / `$EDITOR` / nano fallback).
- [x] `internal/graph/drafts.go`: `CreateReply` (POST /me/messages/{id}/createReply) + `PatchMessageBody` (PATCH /me/messages/{id}) with To / Cc / Bcc / Subject / body update.
- [x] `internal/action/draft.go`: `CreateDraftReply` orchestrates the two Graph calls and returns `{ID, WebLink}`. **PR 7-i (v0.13.x)** — orchestration now flows through the action queue: enqueue with full Params (source_id, body, recipients, subject), call createReply, persist the returned draft_id+web_link via `UpdateActionParams`, then PATCH. Failed status persists with the recorded draft_id so PR 7-ii's resume path can re-PATCH idempotently rather than fire a duplicate createReply. Drain skips the type so the engine doesn't blindly retry a non-idempotent stage 1.
- [x] UI: viewer-pane `r` triggers `startReplyCmd` → `composeStartedMsg` → `tea.ExecProcess(editor)` → `composeEditedMsg` → `saveDraftCmd` → `draftSavedMsg`.
- [x] Outlook hand-off: status bar shows `✓ draft saved · press s to open in Outlook`. `s` runs `open <webLink>` (macOS) or `xdg-open <webLink>` (Linux).
- [x] Tempfile cleanup on save success or parse failure.
- [x] Friendly errors: `ErrEmpty` (blanked-out body discards the draft), `ErrNoRecipients` (To: line empty); both surface to the status bar without a Graph round-trip.
- [x] DraftCreator interface defined at the consumer site (ui doesn't import internal/action). cmd_run.go provides a draftAdapter.
- [x] Tests: 6 in compose (skeleton headers / quote chain / blank-body / re-prefix; parse round-trip / no-recipients / empty); UI dispatch tests for the `r` keybinding + happy/no-deps paths + draft-saved-msg.
- [ ] Reply-all (R) — deferred. Same flow with cc-recipient prefill from source.
- [ ] Forward (f in viewer) — deferred. Different skeleton (forward header block instead of quote).
- [ ] New message (m) — deferred. Skeleton has empty headers + empty body.
- [ ] `compose_sessions` table for crash recovery — deferred. v0.11.0 cleans the tempfile on save; if the app crashes mid-edit, the file is orphaned in `~/Library/Caches/inkwell/drafts/` (mode 0600).
- [ ] Confirm pane after editor exit (`s` save / `e` re-edit / `d` discard) — deferred. v0.11.0 saves immediately on non-empty body.
- [ ] Attachment staging — deferred. Outlook handles attachments in the post-save webLink session.
- [ ] HTML drafts — deferred (PRD §6 — plain text in v1).
- [ ] Lint guard for `Mail.Send` strings — deferred. Spec invariant remains: no code path asks for or uses `Mail.Send`. Belt-and-suspenders CI script lands when convenient.

## Iteration log

### Iter 3 — 2026-04-30 (in-modal compose redesign, spec-15 v2)
- Trigger: real-tenant complaint — "the bottom should just have
  the ability for me to save draft or discard directly, not
  selecting the Exit command first". Spec 15 v1 inherited mutt's
  $EDITOR-driven convention; `tea.ExecProcess` suspends the
  Bubble Tea program while the editor runs, so save / discard
  hints couldn't appear in a footer until the user had already
  exited the editor. The pivot: keep inkwell's UI on screen the
  whole time via an in-modal compose pane.
- Slice:
  - `internal/ui/compose_model.go` (new): `ComposeModel`
    backed by `bubbles/textinput` for headers (To/Cc/Subject)
    and `bubbles/textarea` for body. Focus tracking blurs all
    fields except the focused one so only that component
    receives keystrokes. `ApplyReplySkeleton` reuses the
    existing `internal/compose/template.go::ReplySkeleton` and
    strips the leading header block (the v1 tempfile shape's
    redundant header section).
  - `internal/ui/messages.go`: new `ComposeMode` constant.
    Removed the `ComposeConfirmMode` constant — the
    post-edit modal it gated is gone.
  - `internal/ui/app.go::startCompose` enters ComposeMode with
    the reply skeleton pre-filled; `updateCompose` handles
    Tab / Shift+Tab / Ctrl+S / Esc / Ctrl+D and forwards
    everything else to the focused field's component.
    Ctrl+S / Esc both dispatch `saveComposeCmd` so "I'm done"
    works either way (matches the user's mental model trained
    by the v1 post-edit Enter alias).
  - `internal/ui/compose.go`: replaced `startReplyCmd` /
    `runEditorCmd` / `saveDraftCmd` (tempfile flow) with
    `saveComposeCmd(snap ComposeSnapshot)`. Recipient
    recovery (empty To → source.FromAddress) preserved.
    `composeStartedMsg` / `composeEditedMsg` removed since the
    editor flow is gone for now.
  - `r` in the viewer pane (`internal/ui/app.go::dispatchViewer`)
    now calls `startCompose` instead of `startReplyCmd`.
  - Added bubbles/textarea + textinput as direct deps in
    go.mod (transitively pulled atotto/clipboard,
    MakeNowJust/heredoc).
- Tests:
  - 9 new compose_model_test.go unit tests:
    NewComposeReturnsEmptyState, ApplyReplySkeleton-
    PopulatesFromSource / HandlesRePrefix / EmptyFromAddress-
    LeavesToEmpty, NextField/PrevField cycle, Snapshot+Restore
    round-trip, BodyAcceptsTextEdits, ViewRendersAllFieldsAnd-
    Footer (footer hint visibility — the structural fix for
    the user complaint).
  - 6 new dispatch tests:
    ReplyKeyEntersComposeMode, ComposeTabCyclesFields,
    ComposeCtrlSSavesAndExitsMode, ComposeEscIsSaveAlias,
    ComposeCtrlDDiscards, ComposeRecipientRecoveryFromSource-
    FromAddress, ComposeSaveErrorsWithoutFallback.
  - Removed v1 dispatch tests that referenced gone symbols
    (composeStartedMsg, composeEditedMsg, ComposeConfirmMode,
    saveDraftCmd, composeTempfile, composeSourceID).
  - e2e visible-delta deferred (same teatest issue from the
    earlier `Enter`-as-save attempt).
- Decisions:
  - Esc saves rather than cancels. Standard modal convention
    is Esc-cancels, but losing typed content silently is what
    the v1 post-edit modal explicitly avoided. `Ctrl+D` is the
    explicit discard path.
  - Body field has focus by default after skeleton apply — the
    user's primary editing target is the body; headers are
    pre-filled.
  - Reply only this iter. Reply-all / forward / new message
    are PR 7-iii (alongside the action types they need).
  - $EDITOR drop-out via `Ctrl+E` deferred to a follow-up. The
    `internal/compose/{editor,parse}.go` helpers will retarget
    for that path; kept in tree for now.
  - JSON snapshot replaces the tempfile shape in
    compose_sessions (spec §7) — PR 7-ii implements.
- Result: full -race + -tags=e2e UI suite green; 15 new tests
  pass; 5 v1 tests removed cleanly; spec 15 §6 / §6.1-6.3 / §7
  / §9 / §11 updated to match.

  **Deferred to PR 7-ii:** compose_sessions JSON-snapshot
  migration, resume-on-startup prompt that Restore()s the
  form.

  **Deferred to PR 7-iii:** ReplyAll / Forward / NewMessage
  action types + skeletons + R/F/m keybindings.

  **Deferred to a follow-up:** `Ctrl+E` drop-out for power
  users who want $EDITOR for body editing.

### Iter 2 — 2026-04-30 (drafts via action queue, PR 7-i of audit-drain)
- Trigger: spec 15 §5 / §8 audit row — drafts bypassed the action
  queue entirely. A network blip mid-compose lost the draft
  silently; the actions table had no row to surface in `:filter`
  / debug; crash recovery (PR 7-ii) had no audit trail to read.
- Slice:
  - `internal/store/types.go`: new `ActionCreateDraftReply` enum
    constant with a comment naming the spec rationale.
  - `internal/store/store.go` + `actions.go`: new
    `UpdateActionParams(ctx, id, params)` (mid-flight params
    rewrite for two-stage dispatch) and `ListActionsByType(ctx,
    type)` (terminal-state inspection PR 7-ii's resume path
    needs — `PendingActions` excludes Done/Failed).
  - `internal/action/draft.go`: full rewrite of
    `CreateDraftReply`. Now signature takes `accountID` (FK
    requirement). Flow: Enqueue(Pending) → graph.CreateReply →
    UpdateActionParams(draft_id, web_link) → graph.PatchMessageBody
    → UpdateActionStatus(Done|Failed). The PATCH-failure path
    still returns DraftResult{ID, WebLink} so the caller can
    paint "press s to open in Outlook" — existing UX contract
    preserved.
  - `internal/action/types.go::applyLocal`: ActionCreateDraftReply
    branch returns nil (no local row to mutate; drafts only
    materialize after Drafts-folder delta).
  - `internal/action/executor.go::Drain`: skips
    ActionCreateDraftReply rows. Createreply is non-idempotent;
    blind retry produces duplicate drafts. PR 7-ii's startup
    resume path is the right place for stage-aware retry logic.
  - `internal/ui/app.go::DraftCreator`: interface signature gains
    `accountID int64`.
  - `cmd/inkwell/cmd_run.go::draftAdapter`: signature update.
  - `internal/ui/compose.go::saveDraftCmd`: pulls accountID from
    `m.deps.Account` and threads it through.
- Tests:
  - `executor_test.go`:
    - `TestCreateDraftReplyEnqueuesActionAndPersistsDraftID`
      (happy path: action transitions Pending → Done; draft_id
      + web_link round-trip).
    - `TestCreateDraftReplyKeepsDraftIDOnPATCHFailure` (the
      crash-recovery shape: stage 1 succeeds, stage 2 fails;
      action is Failed BUT params still carry draft_id + web_link
      so PR 7-ii can resume).
    - `TestCreateDraftReplyMarksFailedOnCreateReplyFailure` (pure
      stage-1 failure: no draft_id persisted, action Failed).
    - `TestDrainSkipsCreateDraftReply` (engine drain doesn't
      re-fire stage 1; action stays Pending in the table for
      startup resume).
- Decisions:
  - Two-stage dispatch with mid-flight params persistence is the
    cleanest path to idempotent resume. Alternative considered:
    pre-allocate the draft id client-side. Rejected — Graph
    generates the id; we can't bypass that.
  - SkipUndo set to true on the action because drafts aren't
    reversible from the undo stack — the user finishes the draft
    (or discards) in Outlook. Without this, `u` after a save
    would find the draft action and try to invert it.
  - PATCH failure with draft_id recorded still returns the
    DraftResult so the caller can paint "press s to open in
    Outlook" — the user's body didn't apply but the draft IS on
    the server with Graph's auto-generated headers; better than
    a hard error that loses access to the partially-saved
    draft.
  - `accountID` propagation through DraftCreator: the actions
    table FKs account_id to accounts. The other executor
    methods (MarkRead, SoftDelete, etc.) all take accountID
    explicitly; matching that pattern keeps the surface
    consistent and avoids an Executor-side store lookup.
  - Did not add a "draft local row" optimistic insert. Spec §8
    suggests one, but real-world drafts are immediately
    overwritten by the Drafts-folder delta sync that runs on
    the next cycle. Adding a temp row would require ID
    rewriting on delta arrival, which we don't do for any
    other surface.
- Result: full -race + -tags=e2e suite green; 4 new tests pass;
  no existing tests broken by the signature change.

  **Deferred to PR 7-ii:** compose_sessions table migration,
  startup-time scan of Pending CreateDraftReply rows, resume
  prompt that re-PATCHes when draft_id is set or re-fires
  createReply when not.

  **Deferred to PR 7-iii:** ActionCreateDraft / ActionCreateReplyAll
  / ActionCreateForward / ActionDiscardDraft enum constants;
  `R` (reply-all) / `f` (forward, viewer pane) / `m` (new
  message) keybindings; ReplyAllSkeleton / ForwardSkeleton /
  NewSkeleton template functions.

### Iter 1 — 2026-04-29 (reply via $EDITOR)
- Slice: foundation packages (compose / graph drafts / executor) + UI wiring + cmd_run adapter, all in one cut.
- Files added:
  - internal/compose/{template,parse,editor}.go + 2 test files (~200 LOC + 7 tests).
  - internal/graph/drafts.go (~80 LOC).
  - internal/action/draft.go (~40 LOC).
  - internal/ui/compose.go (Cmds + msgs + openInBrowser, ~100 LOC).
  - internal/ui/app.go: DraftCreator interface, Deps.Drafts, viewer-pane `r` + `s` handlers, Update arms for composeStarted/Edited/draftSaved.
  - cmd/inkwell/cmd_run.go: draftAdapter wires action → ui.
  - 3 dispatch tests for the UI flow.
- Decisions:
  - Two-stage Cmd flow: startReplyCmd builds + writes the tempfile and returns composeStartedMsg with the editor *exec.Cmd already prepared. Update sees the started msg and dispatches tea.ExecProcess. Splits the failure path (skeleton/tempfile errors) from the suspend-resume dance — cleaner Bubble Tea code.
  - The body in the skeleton comes from the cached body_preview rather than the rendered body. Reasonable for v0.11.0 (the user can scroll back into the original message before pressing `r` if they need the full body in their reply); a future iter can fetch + render the full body so the quote chain is complete.
  - Press `s` to open in Outlook via the OS handler (`open` on macOS, `xdg-open` on Linux). Best-effort; failure is silent because the user has the URL on the status bar and can copy it.
  - `r` in the viewer maps to KeyMap.MarkRead binding (which is `r`). The pane-scoped resolution per CLAUDE.md handles this: list-pane `r` = mark-read, viewer-pane `r` = reply. Both code paths consult `m.deps.Drafts` to decide; nil-Drafts means we surface a friendly error rather than crashing.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: `Mail.ReadWrite` (already in PRD §3.1). `Mail.Send` REMAINS DENIED — inkwell never sends.
- [x] Store reads/writes: messages (read for skeleton). The created draft is NOT inserted locally; the next sync cycle pulls it back via the Drafts folder's delta.
- [x] Graph endpoints: `POST /me/messages/{id}/createReply`, `PATCH /me/messages/{id}`.
- [x] Offline behaviour: `r` in offline mode produces a friendly `createReply` error after the editor exits. The tempfile is preserved on a Graph failure so the user doesn't lose work; `compose_sessions` resume lands in a follow-up.
- [x] Undo: discard via blank-body editor exit; explicit DELETE of the saved draft from Outlook covers the post-save case.
- [x] User errors: `ErrEmpty`, `ErrNoRecipients`, editor-not-found, Graph 4xx all surface to the status bar with the spec's friendly wording.
- [x] Latency budget: not gated; the editor session dominates wall-clock. Graph round-trip is two sequential calls (~200-500ms).
- [x] Logs: the graph layer logs request/response via the existing transport stack with redaction.
- [x] CLI mode: `inkwell message reply <id>` deferred (would mirror this flow with `--body-from-file` for non-interactive paths).
- [x] User docs: docs/user/reference.md viewer-pane table gains `r` + `s`; docs/user/how-to.md gains "Reply to a message" recipe.
