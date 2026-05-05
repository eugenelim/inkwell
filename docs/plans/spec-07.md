# Spec 07 — Single-Message Triage Actions

## Status
done. All DoD bullets ticked. A-1 (PR audit-drain 2026-05-02):
replay-on-startup, InFlight state transition, move-id rename,
[triage] config additions. Earlier iters shipped undo (PR 1),
permanent_delete (PR 4a), categories (PR 4b), move-with-picker (PR 4c).

## DoD checklist (mirrored from spec)
- [x] `internal/action/` package compiles with Executor + Drain.
- [x] Action types implemented: mark_read, mark_unread, flag, unflag, soft_delete, archive (move), permanent_delete, add_category, remove_category, move (user-folder picker).
- [x] Replay-on-startup — `Executor.ReplayPending()` resets InFlight non-draft actions to Pending at startup; called from cmd_run.go before engine.Start. Draft-creation InFlight rows are left for the spec-15 stage-aware resume path.
- [x] Optimistic apply: local store mutates first, Graph dispatched second, rollback on failure.
- [x] Pre-mutation snapshot per action (read message before mutation, used to compute rollback fields).
- [x] Action queue persists rows; Drain re-dispatches Pending on each engine cycle.
- [x] Undo stack — shipped v0.13.x (PR 1 of audit-drain). `internal/action/inverse.go` computes the inverse per type; `Executor.run` pushes on success; `Executor.Undo` pops + applies with `SkipUndo` so the inverse-of-the-inverse doesn't recurse. UI binds `u` in list + viewer dispatch; e2e visible-delta verifies the status bar paints `↶ undid: <label>`.
- [x] Replay-on-startup with stale-snapshot semantics — shipped PR A-1 2026-05-02.
- [x] UI keybindings wired in dispatchList: r (mark_read), R (mark_unread), f (toggle_flag), d (soft_delete), a (archive).
- [x] Status bar displays the most recent triage error.
- [x] List pane reloads after every successful triage (so the optimistic state is reflected immediately).
- [x] Tests: executor unit tests over httptest Graph, covering mark_read PATCH payload, toggle_flag flip, soft_delete move endpoint, rollback on Graph failure, drain-retries-pending, InFlight state transition, move-id rename, ReplayPending, Drain-rename.
- [x] `[triage]` config additions: `archive_folder`, `confirm_threshold`, `optimistic_ui` added to TriageConfig with defaults (archive, 10, true).

## Iteration log

### Iter 6 — 2026-05-02 (PR A-1 of Phase 2 audit-drain)
- Slice: replay-on-startup + InFlight state transition + move-id
  stale fix + [triage] config additions.
- Files modified:
  - `internal/action/types.go`: `dispatch()` return signature
    changed from `error` to `(string, error)`; Move/SoftDelete case
    now captures and returns the new message ID from Graph; all other
    cases return `"", err`.
  - `internal/action/executor.go`:
    - `run()`: adds `UpdateActionStatus(InFlight)` before dispatch;
      captures `newID`; renames local row (UpsertMessage + DeleteMessage)
      when Graph returns a new ID; passes `effectiveID` to PushUndo so
      the undo entry targets the live ID not the stale one.
    - `Drain()`: same InFlight mark + rename; retryable failures now
      explicitly reset to Pending (so InFlight doesn't leak on
      transient error).
    - `ReplayPending()`: new method; iterates PendingActions (which
      includes InFlight), skips draft-creation types, resets rest to
      Pending.
  - `cmd/inkwell/cmd_run.go`: calls `exec.ReplayPending(ctx)` before
    `engine.Start()`.
  - `internal/config/config.go`: `TriageConfig` gains `ArchiveFolder`,
    `ConfirmThreshold`, `OptimisticUI`.
  - `internal/config/defaults.go`: defaults `archive`, `10`, `true`.
- Tests added:
  - `TestRunSetsActionInFlightBeforeDispatch`: samples action status
    from inside the httptest handler; asserts InFlight at dispatch time.
  - `TestReplayPendingResetsInFlight`: seeds InFlight MarkRead action,
    ReplayPending → Pending.
  - `TestReplayPendingSkipsDraftCreation`: seeds InFlight
    CreateDraftReply, ReplayPending → still InFlight.
  - `TestDrainRenamesRowToNewIDOnMove`: manually seeded Pending move
    action; Drain re-dispatch → row renamed to ID returned by Graph.
- Tests updated:
  - `TestExecutorSoftDeleteMovesMessage`: asserts old ID gone, new ID
    "m-1-moved" carries correct FolderID.
  - `TestExecutorSoftDeleteWhenDestinationIDDiffersFromAlias`: same.
  - `TestExecutorMoveToUserFolder`: same; also asserts undo entry
    MessageIDs == ["m-1-moved"] (new ID, not stale ID).
- Critique: No layering violations. New public method `ReplayPending`
  required by the cmd layer. No PII in logs. InFlight→Done window is
  minimal (synchronous path). Drain's explicit Pending reset on
  retryable error is a behaviour change from the original (was
  effectively a no-op since the action was already InFlight) — now
  correctly leaves the action retryable on the next cycle.
- Next: PR A-2 (spec 09/10 finish).

### Iter 6 — 2026-05-04 (flag due_date persistence, PR H-2)
- Slice: wire `due_date` param through `ActionFlag` → `MessageFields.FlagDueAt`
  → `UpdateMessageFields` → SQLite `flag_due_at` column; same for Graph PATCH.
- Files modified:
  - `internal/action/types.go`: `applyLocal` reads `due_date` param and sets
    `FlagDueAt` in `MessageFields`; `dispatch` includes `dueDateTime` in PATCH;
    `parseDueDate` helper (RFC 3339 + date-only).
- Tests: `TestFlagWithDueDatePersists` in `executor_test.go`.
- Commands: `go test -race ./internal/action/...` ✓.

### Iter 5 — 2026-04-30 (move-with-folder-picker, PR 4c of audit-drain)
- Slice: spec 07 §6.5 / §12.1 — `m` keybind opens a typed-input
  filterable folder picker; Enter dispatches `Triage.Move`;
  recently-used destinations rank first.
- Files added:
  - `internal/ui/folder_picker.go` — FolderPickerModel with
    typed-filter + arrow-only navigation; rows built from
    FoldersModel.raw + session MRU; Drafts is filtered out
    (Graph rejects move-into-Drafts for non-draft items);
    cursor-windowed view caps at 12 visible rows.
- Files modified:
  - `internal/action/executor.go`: new `Move(ctx, accID, msgID,
    destID, alias)` method (mirrors Archive shape; rejects empty
    destination).
  - `internal/ui/app.go`: TriageExecutor gains `Move`;
    Deps.RecentFoldersCount carries the config knob; Model fields
    `folderPicker`, `pendingMoveMsg`, `recentFolderIDs`;
    `startMove` opens the picker; `updateFolderPicker` handles
    filter / arrows / Enter / Esc; View routes FolderPickerMode;
    `bumpRecentFolder` maintains MRU; `m` handler in dispatchList
    + dispatchViewer.
  - `internal/ui/messages.go`: new FolderPickerMode constant.
  - `cmd/inkwell/cmd_run.go`: triageAdapter forwards Move; Deps
    wires `cfg.Triage.RecentFoldersCount`.
  - `internal/config/config.go` + `defaults.go`: TriageConfig gains
    RecentFoldersCount (default 5).
- Tests:
  - executor_test: `TestExecutorMoveToUserFolder` (round-trip
    with destFolderID + undo entry restores source);
    `TestExecutorMoveRejectsEmptyDestination`.
  - dispatch_test: m opens FolderPickerMode + seeds rows;
    typed-input filters; Enter dispatches Triage.Move with the
    highlighted row's id+alias and bumps MRU; Esc cancels;
    arrow keys don't leak into the filter buffer; Enter on empty
    filter is safe and stays in picker mode; pre-sync (no
    folders) surfaces a useful error rather than an empty
    picker; bumpRecentFolder unit test covers MRU promotion +
    cap; row builder skips Drafts; recents rank first.
  - app_e2e_test: `TestFolderPickerMOpensModalAndDispatchesMove`
    visible-delta — `m` paints "Move to:" + filter label +
    Inbox + Archive; typing "Arc" updates the buffer + keeps
    Archive visible; Enter clears the modal.
- Decisions:
  - Arrow-only navigation (tea.KeyUp / tea.KeyDown) so j/k flow
    into the typed filter, matching aerc / mutt picker UX. List
    pane keymap (which binds j/k) is intentionally NOT consulted
    in FolderPickerMode.
  - MRU is session-scoped (in-memory) rather than persisted —
    spec 07 §12.1 says "recently used" not "across sessions",
    and persistence would tangle with folder-id rewrites that
    Graph performs on user moves. Keeping it session-scoped
    sidesteps the staleness class.
  - Drafts filtered from destinations because moving non-drafts
    into the Drafts well-known folder generates a Graph 400 the
    user can't act on. Better to hide the row than serve a
    confusing failure.
  - Path-style labels ("Parent / Child / Grandchild") so
    duplicate child names in different parent chains stay
    disambiguated. Built once at picker open via
    `buildFolderPaths`.
  - Cursor-windowed view (12 visible rows max) so big mailboxes
    don't blow past the modal height; the typed filter narrows
    most cases below the cap anyway.
- Result: gosec 0 issues, govulncheck 0 vulns, all packages
  green under -race + -tags=e2e.

### Iter 4 — 2026-04-30 (categories, PR 4b of audit-drain)
- Slice: spec 07 §6.9 / §6.10 — add_category / remove_category
  end-to-end. Move-with-folder-picker carved out as PR 4c
  (needs a real folder picker UI, beyond this PR's scope).
- Files modified:
  - `internal/action/types.go`: applyLocal handles
    add/remove (appends/drops via case-insensitive dedup);
    rollbackLocal restores the snapshot's category list;
    dispatch reads the post-apply local row + PATCHes the
    full categories array (Graph contract — no append /
    remove primitive).
  - `internal/action/executor.go`: AddCategory + RemoveCategory
    methods (mirror MarkRead shape; reject empty category).
  - `internal/action/inverse.go`: already handled add↔remove
    pair from PR 1; no change.
  - `internal/ui/messages.go`: new CategoryInputMode constant.
  - `internal/ui/app.go`: pendingCategoryAction +
    pendingCategoryMsg + categoryBuf model fields;
    startCategoryInput opens the prompt; updateCategoryInput
    handles typing / Enter / Esc; render branch in View();
    `c` / `C` handlers in dispatchList + dispatchViewer.
  - `cmd/inkwell/cmd_run.go`: triageAdapter passes through.
- Tests:
  - executor_test: AddCategory appends + PATCHes the full
    list + pushes RemoveCategory inverse; case-insensitive
    dedup; RemoveCategory drops the named entry.
  - dispatch_test: `c` opens CategoryInputMode with
    action="add"; typing + Enter dispatches; Esc cancels.
- Decisions:
  - Category prompt is a typed-input modal (Enter / Esc)
    rather than a chord (`c X` would conflict with the bulk
    `;X` chord pattern). Spec 07 §6.9 left the UX open;
    typed-input is what aerc and mutt do.
  - PATCH carries the full post-apply list rather than a
    delta because Graph requires it. The dispatch path
    re-reads the local row after applyLocal so the payload
    matches the optimistic state exactly.
  - Inverse already handles the symmetric pair from PR 1;
    no inverse work needed in this PR.
  - `m` (move-with-folder-picker) deferred to PR 4c — the
    folder picker is a non-trivial filterable list with its
    own keybindings, beyond this PR's scope.
- Result: gosec 0 issues, govulncheck 0 vulns, all packages
  green under -race + -tags=e2e.

### Iter 3 — 2026-04-30 (permanent_delete, PR 4a of audit-drain)
- Slice: spec 07 §6.7 — `D` keybind + confirm modal + Graph
  helper + executor branch + Inverse non-reversible.
- Files modified:
  - `internal/graph/triage.go` — new `PermanentDelete(ctx, id)`
    helper. POST /me/messages/{id}/permanentDelete; 404 treated
    as success (idempotency); 204 No Content is canonical.
  - `internal/action/executor.go` — `PermanentDelete(ctx, accID,
    id)` method (mirrors SoftDelete shape).
  - `internal/action/types.go` — applyLocal does
    `st.DeleteMessage(id)` for ActionPermanentDelete; rollback
    re-inserts from snapshot via UpsertMessage; dispatch calls
    the new graph helper.
  - `internal/ui/app.go` — `pendingPermanentDelete *store.Message`
    on the model; new `startPermanentDelete(src)` opens the
    confirm modal with the irreversibility warning + subject +
    sender; ConfirmResultMsg branch fires runTriage on y; D
    handler in dispatchList + dispatchViewer.
  - cmd_run.go triageAdapter wires through.
- Tests:
  - `executor_test.go`: hits-Graph + removes-locally + no-undo;
    rollback-on-Graph-failure restores from snapshot.
  - `dispatch_test.go`: opens-confirm-modal; y fires; n cancels.
  - `app_e2e_test.go`: visible-delta — modal carries
    "PERMANENT DELETE" + "irreversible"; n shows
    "permanent delete cancelled".
- Decisions:
  - The Inverse `permanent_delete → ok=false` invariant means
    the executor's run() path doesn't push to the undo stack on
    success, so pressing `u` after a confirmed D produces
    "nothing to undo" rather than a deceptive restore attempt.
    Matched test in inverse_test.go iter 2.
  - Modal copy intentionally puts "PERMANENT DELETE" in caps and
    "irreversible" twice (once in the verb, once in the body).
    Heuristic: if the user breezes past these visual cues they
    probably meant to do it.
  - Confirm flow re-uses pendingPermanentDelete + ConfirmResultMsg
    pattern from spec 16 unsubscribe — the precedent for
    storing per-action context in a typed pointer field rather
    than overloading pendingBulk.
- Result: gosec 0 issues, govulncheck 0 vulns, all packages
  green under -race + -tags=e2e.

  **Deferred to PR 4b:** add_category / remove_category
  (needs picker), move-with-folder-picker (`m`).

### Iter 2 — 2026-04-30 (undo, PR 1 of audit-drain)
- Slice: `internal/action/inverse.go` (Inverse function + reversibility table); `Executor.run` pushes inverse on success; `Executor.Undo` pops + applies; `store.Action.SkipUndo` bool prevents recursion; UI exposes Undo via `TriageExecutor`; `triageAdapter` in `cmd_run.go` translates `store.UndoEntry → ui.UndoneAction` + `store.ErrNotFound → ui.UndoEmpty`.
- Tests:
  - `inverse_test.go`: 5 cases covering toggle pairs, move-with-snapshot, snapshot-required-for-move, permanent-delete-non-reversible, unknown-action-non-reversible.
  - `executor_test.go`: 3 new cases — push-on-success, full round-trip (mark_read → undo → unread; stack now empty surfaces ErrNotFound), no-recursive-push (the SkipUndo invariant).
  - `dispatch_test.go`: 2 new cases — `u` returns runUndo Cmd; empty-stack surfaces "nothing to undo" without lastError.
  - `app_e2e_test.go`: 1 visible-delta case — pressing `u` paints `↶ undid: marked read` on the status bar.
- Decisions:
  - Inverse of soft_delete / move uses `pre.FolderID` from snapshot (not the action's destination_folder_id) so undo restores to the source even if the user later moved the message to yet another folder.
  - Permanent_delete intentionally returns `ok=false` from Inverse — the message is gone from the tenant; pretending to undo it would be deceptive.
  - SkipUndo is a runtime-only field (json:"-"`) on store.Action so it doesn't get persisted to the actions table or replayed across restarts. The undo stack itself persists across actions but is cleared on app start (existing ClearUndo path).
  - Undo of the inverse pops the entry but doesn't re-push — symmetric pairs mean a second `u` would just restore the original, which is what the user pressed in the first place. The test `TestExecutorUndoDoesNotRecursivelyPush` is the regression for the alternative buggy implementation that would loop.
- Result: all packages green under -race; e2e green; gosec 0 issues; govulncheck 0 vulns.

### Iter 1 — 2026-04-28 (minimum-viable triage)
- Slice: action package (executor.go + types.go), graph PATCH/POST helpers, UI dispatch wiring.
- Files added: 3 in internal/action, 1 in internal/graph (triage.go), 4 method-pairs in internal/ui/app.go.
- Commands: `go test -race ./internal/action/...` green in 32s (the 503-retry test waits through graph client backoff).
- Wired action.Executor as sync.ActionDrainer in cmd_run.go so the engine retries pending actions every cycle.
- Critique:
  - Executor.run is synchronous-dispatch. Spec calls for async (goroutine). Synchronous is simpler for v0.3.0 and works because Graph latency is <1s typically; revisit when bulk lands.
  - No goroutine leak since dispatch is in-line.
  - Test execution time of 32s is dominated by graph client retry-backoff in the rollback test (403 with mocked retry). Acceptable for this iter; consider injecting a no-retry option for tests in a follow-up.
  - Categorize / move-with-picker / permanent-delete intentionally deferred — they each need a modal/picker UI surface that's out of scope for v0.3.0.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: `Mail.ReadWrite` (already in PRD §3.1; no new scope).
- [x] Store reads/writes: messages (UpdateMessageFields), actions (Enqueue/Update/PendingActions).
- [x] Graph endpoints: `PATCH /me/messages/{id}`, `POST /me/messages/{id}/move`.
- [x] Offline behaviour: action sits in Pending; engine drain retries on next online cycle.
- [x] Undo: deferred.
- [x] User errors: triage failure surfaces via Model.lastError → status bar.
- [x] Latency budget: not benched yet (spec doesn't list one for single-message); revisit with batch executor (spec 09).
- [x] Logs: executor takes a logger, all dispatch failures logged. No new redaction-relevant fields.
- [x] CLI mode: spec 14.
- [x] Tests: unit (executor_test.go) covering 5 action types + rollback + drain.
