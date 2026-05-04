# Spec 15 — Compose / Reply (drafts only)

## Status
in-progress. v1: viewer-pane reply via `$EDITOR` shipped
post-v0.10.0. v0.13.x: drafts flow through the action queue with
two-stage idempotent dispatch (PR 7-i). **v0.13.x spec rewrite:**
in-modal compose pane replaces the editor-driven flow (real-tenant
"select Exit command first" friction) — see iter 3. Reply-all /
forward / new message skeletons (PR 7-iii), `compose_sessions`
crash recovery (PR 7-ii) shipped. **v0.30.0 (F-1 audit-drain):**
discard DELETE + webLink TTL + `Mail.Send` CI guard + attachments
infrastructure shipped — see iter 6.

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
- [x] Reply-all (R) — **closed by PR 7-iii (v0.13.x).** Viewer-pane R fires startComposeReplyAll; ApplyReplyAllSkeleton fills To with src.From + remaining To recipients (deduped against userUPN), Cc with src.Cc; subject prefixed Re: with normalisation; saveComposeCmd routes via DraftCreator.CreateDraftReplyAll → graph.CreateReplyAll → PATCH.
- [x] Forward (f in viewer) — **closed by PR 7-iii (v0.13.x).** Viewer-pane f fires startComposeForward; ApplyForwardSkeleton clears To/Cc, prefixes Subject Fwd: with Fw:/Fwd: normalisation, body opens with the canonical Forwarded message header block; saveComposeCmd routes via DraftCreator.CreateDraftForward → graph.CreateForward → PATCH. When Drafts isn't wired the binding falls back to the legacy ToggleFlag for graceful degradation.
- [x] New message (m) — **closed by PR 7-iii (v0.13.x).** Folders-pane and viewer-pane m (when Drafts wired) fire startComposeNew; ApplyNewSkeleton blanks the form and focuses the To field; saveComposeCmd routes via DraftCreator.CreateNewDraft → graph.CreateNewDraft (single-stage POST /me/messages with the full payload — no createX/PATCH dance). List-pane m keeps move-with-folder-picker.
- [x] `compose_sessions` table for crash recovery — **closed by PR 7-ii (v0.13.x).** Migration 005 adds the table per spec §7; SchemaVersion bumped to 5. `internal/ui/ComposeModel` gains a SessionID; entry / focus changes persist a JSON-encoded snapshot via `store.PutComposeSession`; save (Ctrl+S/Esc) and discard (Ctrl+D) stamp `confirmed_at` so the resume scan ignores the row. Init runs `scanComposeSessionsCmd` which GCs confirmed sessions older than 24h then offers the most-recent unconfirmed row via a confirm modal. Y restores into ComposeMode preserving SessionID; n inline-confirms. Corrupt snapshots are confirmed-and-skipped to avoid resume loops.
- [ ] Confirm pane after editor exit (`s` save / `e` re-edit / `d` discard) — deferred. v0.11.0 saves immediately on non-empty body.
- [x] Attachment infrastructure — `AttachmentRef` / `safeReadFile` path-traversal guard / `graph.AddDraftAttachment` / `uploadAttachments` pipeline wired through all four create methods. UI `AttachmentSnapshotRef` + snapshot round-trip + `snapshotRefsToAction` conversion. **Closed F-1 (v0.30.0).**
- [ ] Attachment UI picker — deferred. Infrastructure is wired; the file-picker overlay lands in a follow-up spec.
- [ ] HTML drafts — deferred (PRD §6 — plain text in v1).
- [x] Lint guard for `Mail.Send` strings — `scripts/check-no-mail-send.sh` (greps for `"Mail.Send"` literal in Go files outside scopes.go) + CI `permissions-check` job already covers this. **Closed F-1 (v0.30.0).**
- [x] Server-side discard (DELETE /me/messages/{id}) — viewer-pane `D` key, when `lastDraftID` is set, opens a confirm modal and fires `graph.DeleteDraft` (404 = success). Status bar hint updated to `✓ draft saved · s open · D discard`. **Closed F-1 (v0.30.0).**
- [x] WebLink TTL auto-clear — `[compose].web_link_ttl` (default 30s) fires `draftWebLinkExpiredMsg` which clears `lastDraftWebLink` + `lastDraftID`. **Closed F-1 (v0.30.0).**

## Iteration log

### Iter 6 — 2026-05-04 (discard DELETE + webLink TTL + Mail.Send guard + attachments, PR F-1 of audit-drain)
- Trigger: `docs/audits/spec-1-15-impl-gaps.md` F-1 row. Four
  open gaps from the audit: (a) discard after save doesn't call
  Graph DELETE, (b) webLink hint never expires, (c) no CI guard
  for `Mail.Send` scope, (d) attachment infrastructure missing.
- Slice (`internal/store`): Added `ActionDiscardDraft` action type.
- Slice (`internal/config`): Added `ComposeConfig{AttachmentMaxSizeMB, MaxAttachments, WebLinkTTL}` under `[compose]`; defaults 25 MB / 20 / 30s. `docs/CONFIG.md` updated.
- Slice (`internal/graph`): Added `DeleteDraft` (DELETE /me/messages/{id}, 404=success) and `AddDraftAttachment` (POST /me/messages/{id}/attachments, base64 contentBytes). Tests in `drafts_test.go`.
- Slice (`internal/action`): Added `AttachmentRef` struct, `safeReadFile` path-traversal guard (spec 17 §4.4 — absolute path, clean, Lstat, size limit), `uploadAttachments`, `DiscardDraft`. All four create methods accept `attachments []AttachmentRef`. `Executor.composeCfg` + `SetComposeConfig`. Tests in `draft_test.go`.
- Slice (`internal/ui`): `ComposeSnapshot.Attachments` + `AttachmentSnapshotRef` + `snapshotRefsToAction`. `DraftCreator` interface gains `attachments []DraftAttachmentRef` param on all four create methods and new `DiscardDraft` method. `Deps.DraftWebLinkTTL`. Model gains `lastDraftID`, `pendingDiscardDraftID`. `draftSavedMsg` stores both ID and webLink, fires TTL timer. New handlers for `draftWebLinkExpiredMsg`, `draftDiscardDoneMsg`. `discard_draft` topic wired in `ConfirmResultMsg`. Viewer `D` intercepts when `lastDraftID != ""`. Status: `✓ draft saved · s open · D discard`.
- Slice (`cmd/inkwell`): `draftAdapter` updated (4 create + 1 discard). `convertAttachmentRefs`. `DraftWebLinkTTL` + `SetComposeConfig` wired.
- Slice (CI): `scripts/check-no-mail-send.sh` + `scripts/regress.sh` step 0.
- Commands: `bash scripts/regress.sh` — all 6 gates green.
- Critique: no layering violations; no new PII log sites; all error paths covered; no context.Background in request paths (only in tea.Cmd goroutines); idempotent (404 = success on DELETE).
- Next: `Ctrl+E` $EDITOR drop-out (deferred), attachment picker (deferred).

### Iter 5 — 2026-05-01 (ReplyAll / Forward / NewMessage, PR 7-iii of audit-drain)
- Trigger: spec 15 §5 / §6.2 / §9 audit row + iter 3's deferral
  list. Reply-only was the v0.13.x MVP shape; the spec calls for
  R / f / m bindings with their corresponding action types and
  skeletons.

- Slice (action types):
  - `internal/store/types.go` adds `ActionCreateDraftReplyAll`,
    `ActionCreateDraftForward`, `ActionCreateDraft` enum
    constants alongside the existing `ActionCreateDraftReply`.
    Each has a doc comment naming the spec section + dispatch
    shape.

- Slice (graph layer):
  - `internal/graph/drafts.go` adds `CreateReplyAll`,
    `CreateForward`, and `CreateNewDraft`. The first two delegate
    to a new shared `createDraftFromSource` helper that
    parameterises the verb (`createReply` / `createReplyAll` /
    `createForward`) — they share the response shape and
    error-handling path. CreateNewDraft is single-stage: it
    POSTs to `/me/messages` with the full body+recipients
    payload in one shot.

- Slice (action executor):
  - `internal/action/draft.go` adds `CreateDraftReplyAll`,
    `CreateDraftForward` (shared two-stage body via the new
    private `createDraftFromSource` helper that takes the action
    Type + stage1 closure as parameters — compresses three nearly
    identical functions into one). `CreateNewDraft` is its own
    function (single-stage; no source-message gating).
  - `internal/action/types.go` extends `applyLocal` to no-op for
    all four draft kinds (drafts only materialise after the
    Drafts-folder delta sync; consistent with the Reply-only
    behaviour).
  - `internal/action/executor.go::Drain` swapped the
    `a.Type == ActionCreateDraftReply` skip for a generalised
    `isDraftCreationAction(a.Type)` helper that covers all four
    kinds.

- Slice (compose templates):
  - `internal/compose/template.go` adds `ReplyAllSkeleton`,
    `ForwardSkeleton`, `NewSkeleton`. ReplyAll dedups the user
    out of the audience (userUPN passed in); Forward normalises
    `Fwd:` / `Fw:` to canonical `Fwd:` and emits the
    `---------- Forwarded message ----------` header block with
    From / Date / Subject / To from the source. New is a blank
    canvas. Helper `dedupAddresses` + `joinAddrs` keep the
    rendering consistent with what the in-modal pane does in
    its form fields.

- Slice (UI compose model):
  - `internal/ui/compose_model.go` adds `ApplyReplyAllSkeleton`,
    `ApplyForwardSkeleton`, `ApplyNewSkeleton` alongside the
    existing reply applier. Reply-all populates To+Cc from the
    source (userUPN-filtered + deduped); Forward clears To/Cc;
    New blanks everything and shifts focus to To (since there's
    no source-sender to pre-fill from, recipients are the
    user's first task).
  - Subject normalisation lives in two helpers (`replyPrefix`,
    `forwardPrefix`) so the UI form and the compose package
    template emit the same prefix logic.

- Slice (DraftCreator interface + adapter):
  - `internal/ui/app.go::DraftCreator` adds three methods
    (`CreateDraftReplyAll`, `CreateDraftForward`, `CreateNewDraft`)
    so the UI can route by Kind without reaching into
    internal/action.
  - `cmd/inkwell/cmd_run.go::draftAdapter` implements all four;
    a new private helper `convertDraftResult` collapses the
    `(*action.DraftResult, error) → (*ui.DraftRef, error)` dance
    that the four methods share.

- Slice (UI dispatch):
  - `internal/ui/app.go` factors `startCompose` into a shared
    `startComposeOfKind(kind, *src)` that handles all four
    flavours; per-kind starters (`startCompose`,
    `startComposeReplyAll`, `startComposeForward`,
    `startComposeNew`) wrap it.
  - Viewer pane:
    - `r` (MarkRead binding) → Reply (existing).
    - `R` (MarkUnread binding, viewer-pane scope) → ReplyAll.
    - `f` (ToggleFlag binding, viewer-pane scope) → Forward
      when Drafts is wired; falls back to ToggleFlag when not
      so test setups + degraded modes still work.
    - `m` (Move binding, viewer-pane scope) → NewMessage when
      Drafts is wired; falls back to startMove when not.
  - Folders pane:
    - `m` → NewMessage (when Drafts wired). Previously a no-op
      because Move had no list of messages to act on.
  - List pane retains all four bindings as their original
    triage verbs (mark-read, mark-unread, toggle-flag, move).
  - `internal/ui/compose.go::saveComposeCmd` routes by
    `snap.Kind` to the matching DraftCreator method. Recipient
    recovery (empty-To fallback to source.FromAddress) skips
    the `ComposeKindNew` case — there's no source. Stage-2
    failure preserves the existing spec-15 contract (return
    DraftResult + err so the caller paints "press s to open in
    Outlook").

- Tests:
  - **compose** (5): TestReplyAllSkeletonPopulatesAllRecipients,
    TestReplyAllSkeletonDedupesAddresses,
    TestForwardSkeletonHasForwardHeaderBlock,
    TestForwardSkeletonPreservesExistingFwdPrefix,
    TestNewSkeletonIsBlank.
  - **action executor** (4): TestCreateDraftReplyAllRoutesTo
    ReplyAllEndpoint, TestCreateDraftForwardRoutesToForward
    Endpoint, TestCreateNewDraftSinglePost,
    TestDrainSkipsAllDraftCreationKinds.
  - **ui dispatch** (10): TestViewerCapitalRStartsReplyAll,
    TestViewerLowerFStartsForward,
    TestViewerLowerFFlagsWhenNoDraftsWired,
    TestViewerLowerMStartsNewWhenDraftsWired,
    TestFolderPaneMStartsNew,
    TestSaveComposeRoutesByKind (table-driven, 4 sub-cases),
    TestSaveComposeNewDraftSkipsRecipientFallback,
    TestApplyReplyAllSkeletonFiltersUserUPN,
    TestApplyForwardSkeletonClearsRecipients,
    TestApplyNewSkeletonFocusesTo.

- Decisions:
  - **Pane-scoped `f` and `m` rebindings.** Viewer-pane `f`
    and `m` previously did flag-toggle and move; spec 15 §9
    explicitly reassigns them to Forward and NewMessage in the
    viewer. Pane scope keeps the list-pane meanings intact so
    users still flag / move from the list. The Drafts-not-wired
    fallback ensures degraded-mode UX (test harnesses; future
    "no auth" demo mode) still has working flag/move.
  - **`R` is reply-all in viewer; mark-unread in list; rename-
    folder in folders.** Three-way pane scope on a single
    keymap field — pre-existing pattern with pane-scoped `r` /
    `f` / `m`, now formalised across more bindings.
  - **Single-stage POST /me/messages for new drafts.** The
    Graph endpoint accepts the full body in one shot; no need
    for a createX / PATCH dance. This means the resume path
    can't distinguish "stage 1 succeeded but not persisted"
    from "stage 1 never happened" — but for new drafts that
    distinction doesn't matter (no draft on the server vs draft
    on the server are both fine; user can re-send from the
    resumed snapshot).
  - **Subject normalisation matches across compose package
    and UI form.** `replyPrefix` and `forwardPrefix` collapse
    `Re: Re:` / `Fw:` / `Fwd:` to canonical forms in both
    layers so the textarea body and the form's Subject field
    don't drift.
  - **`createDraftFromSource` helper compresses three near-
    identical executor methods into one.** Stage 1 endpoint
    differs; everything else is the same. The closure-passed
    stage1 fn lets the helper stay typed without an interface
    box.

- Result: full -race + -tags=e2e + -tags=integration suite
  green. 5 new compose tests + 4 new action tests + 10 new ui
  tests pass; spec 15 §5 / §6.2 / §9 DoD bullets all closed.

  **Remaining deferred (post-MVP):**
  - `Ctrl+E` $EDITOR drop-out for power users who want
    external-editor body composition.
  - Discard flow that DELETEs the server-side draft when the
    user cancels AFTER save (today: cancel before save = Ctrl+D,
    no server roundtrip; cancel after save = open in Outlook
    and discard there).
  - CI lint guard for `Mail.Send` literal outside auth/scopes.go
    + docs/PRD.md.

### Iter 4 — 2026-05-01 (compose_sessions migration + crash-recovery resume, PR 7-ii of audit-drain)
- Trigger: spec 15 §7 audit row + iter 3's deferral note. The
  in-modal compose pane held form state in memory only; a crash
  while typing lost everything. Spec §7 calls for a JSON-snapshot
  table that the launch path scans on next start and offers as a
  resume prompt.
- Slice (schema):
  - `internal/store/migrations/005_compose_sessions.sql` adds
    `compose_sessions(session_id, kind, source_id, snapshot,
    created_at, updated_at, confirmed_at)` plus a partial index
    `idx_compose_sessions_unconfirmed` on `created_at` where
    `confirmed_at IS NULL` to keep the launch-time resume scan
    cheap. `source_id` FKs to `messages(id)` ON DELETE SET NULL
    so the session row survives a source-message delete (the
    resume modal warns rather than crashes).
  - `store.SchemaVersion` bumped to 5.
- Slice (store API):
  - `internal/store/compose_sessions.go` adds
    `PutComposeSession` (idempotent upsert by SessionID),
    `ConfirmComposeSession` (stamps confirmed_at),
    `ListUnconfirmedComposeSessions` (newest first; resume scan
    consumer), `GCConfirmedComposeSessions` (delete confirmed-
    older-than-cutoff; launch-time pass uses now-24h).
  - `internal/store/types.go` adds the `ComposeSession` struct.
  - `Store` interface in `internal/store/store.go` extended with
    the 4 new methods + spec-rationale comments.
- Slice (UI wiring):
  - `internal/ui/compose_model.go::ComposeModel` gains
    `SessionID string` field (set on entry; preserved across
    Restore so subsequent saves hit the same row).
  - `internal/ui/compose.go` adds:
    - `newComposeSessionID()` (crypto/rand → `cs-<8-byte-hex>`).
    - `composeKindToString()` (text-column mapping).
    - `persistComposeSnapshotCmd()` — async tea.Cmd that writes
      via `store.PutComposeSession`.
    - `confirmComposeSessionInline()` — synchronous SQLite WAL
      write (sub-ms; used by Ctrl+D / decline-resume so the
      test contract "no Cmd returned" stays clean).
    - `scanComposeSessionsCmd()` — launch-time scan: GC pass +
      list-unconfirmed → returns `composeResumeMsg` /
      `composeResumeNoneMsg`.
    - `composeResumePrompt()` + `humanAge()` — user-facing
      modal text with "5 min ago" / "2 h ago" / "1 day ago"
      formatting.
    - `resumeCompose()` — hydrates ComposeModel from a stored
      snapshot; preserves SessionID; falls through to a
      friendly "snapshot corrupt; discarded" status when the
      JSON decode fails (and inline-confirms the row to break
      resume loops).
  - `internal/ui/app.go::Init` — adds `scanComposeSessionsCmd()`
    to the startup batch.
  - `internal/ui/app.go::Update` — `composeResumeMsg` opens the
    confirm modal (`Topic: "compose_resume"`); the
    `ConfirmResultMsg` branch routes y/n to `resumeCompose()`
    or inline-confirm.
  - `internal/ui/app.go::startCompose` — assigns SessionID,
    persists initial skeleton snapshot.
  - `internal/ui/app.go::updateCompose` — Tab / Shift+Tab
    re-persist on focus change; Ctrl+S/Esc folds the confirm
    write into `saveComposeCmd`'s goroutine; Ctrl+D
    inline-confirms.
  - `Model.pendingComposeResume` field tracks the row across
    the confirm modal lifetime.

- Tests:
  - **store** (5): TestComposeSessionRoundTrip,
    TestComposeSessionUpsertRewritesSnapshot,
    TestComposeSessionConfirmHidesFromUnconfirmedScan,
    TestComposeSessionListUnconfirmedNewestFirst,
    TestComposeSessionGCRemovesOldConfirmed,
    TestComposeSessionForeignKeySetNullOnSourceDelete.
  - **ui dispatch** (9): TestComposeSessionPersistsOnEntry,
    TestComposeSessionConfirmedOnSave,
    TestComposeSessionConfirmedOnDiscard,
    TestComposeSessionPersistsOnTab,
    TestComposeResumeMsgOpensConfirmModal,
    TestComposeResumeYesRestoresIntoComposeMode,
    TestComposeResumeNoConfirmsAndDiscards,
    TestComposeResumeCorruptSnapshotDoesNotCrash,
    TestScanComposeSessionsCmdReturnsNoneWhenEmpty,
    TestScanComposeSessionsCmdReturnsResumeWhenPresent,
    TestScanComposeSessionsCmdGCsOldConfirmed.

- Decisions:
  - **Persist on focus change, not per-keystroke.** Per-keystroke
    persistence would write 100s of rows/min for a typical
    compose; per-Tab captures every field-completion the user
    makes and is the natural "I've finished thinking about this
    field" boundary. Worst-case loss on crash: the partial
    sentence in the currently-focused field. Acceptable.
  - **Inline confirm on Ctrl+D / decline-resume.** SQLite WAL
    locally is sub-millisecond; an inline write keeps the
    test contract "Ctrl+D returns nil Cmd" intact and means
    the user's next keystroke never races persistence.
  - **saveComposeCmd folds confirm into its goroutine.** The
    Graph round-trip is already async; piggy-backing the
    confirm write on the same goroutine means the save returns
    a single Cmd (existing tests stay green) AND confirmed_at
    is stamped regardless of save success/failure (the user
    explicitly chose save, so the resume scan should not
    re-offer this row).
  - **Corrupt snapshot → confirm + skip.** A row whose JSON
    blob can't decode would resume-loop forever; we confirm
    it (with a friendly status message) so it never resurfaces.
  - **Init batch always emits a deterministic message.** The
    scan-Cmd returns either `composeResumeMsg` or
    `composeResumeNoneMsg`; tests can assert the launch path
    completes end-to-end without a long teatest dance.

- Result: full -race + -tags=e2e + -tags=integration suite
  green; 9 new ui tests + 6 new store tests pass; spec 15 §7
  closed in the DoD checklist. Ralph-loop critique:
  - Layering check: ui → store stays through `Deps.Store`; no
    new layering inversions.
  - Comment audit: each new helper has a one-paragraph
    doc-comment naming the spec section + the rationale; no
    "what the code does" restating.
  - Privacy check: `store.ComposeSession.Snapshot` is opaque
    JSON with body content; the redaction layer scrubs body
    content from logs by default; we never log the snapshot
    blob from the UI layer, only the SessionID + error.
  - Idempotency: PutComposeSession is upsert; Confirm is
    no-op when the row's already confirmed; GC sweeps the
    same rows multiple times without harm.
  - Crash-safety: every write is single-statement on a WAL DB
    so a crash at any point either persists the row or doesn't
    (no half-states).

  **Deferred to PR 7-iii:** ReplyAll / Forward / NewMessage
  action types + skeletons + R/F/m keybindings.

  **Deferred to a follow-up:** `Ctrl+E` drop-out for power
  users who want $EDITOR for body editing.

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
