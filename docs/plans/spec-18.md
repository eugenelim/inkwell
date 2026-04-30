# Spec 18 — Folder management (create / rename / delete)

## Status
shipped (v0.15.x). All §8 DoD bullets ticked except CLI tests
(deferred — covered indirectly by the action-layer tests; CLI
plumbing is straightforward cobra wiring).

## DoD checklist (mirrored from spec)
- [x] `internal/graph/folders.go` adds CreateFolder /
      RenameFolder / DeleteFolder. 404 on delete = success
      (CLAUDE.md §3 idempotency).
- [x] `action.Executor` extends with the three folder methods.
      Synchronous (not queued) — folder ops are user-initiated
      with quick round-trips, no optimistic-apply value at scale.
      Local upsert/update/delete on success keeps the sidebar in
      sync before the next sync cycle.
- [x] Sidebar `N` / `R` / `X` wired in dispatchFolders. Capital
      letters so they don't collide with movement keys.
- [x] Name modal (`FolderNameInputMode`) for create + rename.
      Pre-seeds buffer on rename so user edits in place.
      Confirm modal for delete with a warning about Deleted
      Items recovery.
- [x] CLI `inkwell folder new|rename|delete` per spec 14
      patterns. Slash-path syntax for nested creates
      (`"Parent/Child"`). Delete requires `--yes`.
- [x] Tests:
      - graph: 5 httptest cases covering create top-level +
        nested, rename happy + 403, delete 204 + 404.
      - action: executor tests for create-upserts-locally,
        rename-updates-locally, delete-removes-locally.
      - dispatch: N opens FolderNameInputMode, Enter dispatches
        Create; R pre-seeds buffer + dispatches Rename; X opens
        confirm modal, y dispatches Delete.
      - e2e visible-delta: pressing N paints the modal, typing
        + Enter shows "✓ created folder" status.
- [x] User docs: reference (`N`/`R`/`X` rows), how-to
      ("Reorganise your mailbox" recipe).
- [ ] CLI tests deferred — `cmd_messages_test.go` is the only
      CLI unit test pattern in the repo; a folder-CLI test
      would mostly exercise cobra's argument parsing. The
      action-layer tests cover the call shapes; integration
      with real Graph is manual smoke (CLAUDE.md §5.5).

## Iteration log

### Iter 1 — 2026-04-30 (full ship)
- Slice: graph helpers + executor methods + sidebar dispatch +
  modals + CLI + tests + docs in one cut.
- Files added/modified:
  - `internal/graph/folders.go`: CreateFolder, RenameFolder,
    DeleteFolder.
  - `internal/action/folders.go` (new): Executor methods.
    Synchronous dispatch — no queued action types.
  - `internal/store/folders.go`: UpdateFolderDisplayName.
  - `internal/store/store.go`: interface gains the new method.
  - `internal/ui/keys.go`: NewFolder / RenameFolder /
    DeleteFolder key bindings, defaulting to N / R / X.
  - `internal/ui/messages.go`: FolderNameInputMode constant.
  - `internal/ui/app.go`: TriageExecutor interface gains
    CreateFolder / RenameFolder / DeleteFolder + CreatedFolder
    value type. Model fields for pendingFolderAction /
    pendingFolderID / pendingFolderParent / folderNameBuf /
    pendingFolderDelete. startFolderNameInput /
    updateFolderNameInput / startFolderDelete handlers.
    folderActionDoneMsg + handler in Update reloads sidebar
    on success, surfaces errors, clears the viewer pane when
    the user just deleted the folder they were viewing.
    Render branch in View() for FolderNameInputMode.
    dispatchFolders gains N/R/X handlers.
  - `cmd/inkwell/cmd_folder.go` (new): cobra commands for
    new/rename/delete with slash-path resolver.
  - `cmd/inkwell/cmd_run.go`: triageAdapter passes through
    the three new methods.
- Decisions:
  - Synchronous execution (not queued via spec 07's action
    queue). Folder ops are user-initiated with quick round-
    trips; the optimistic-apply pattern is for triage actions
    that mutate hundreds of messages. Folder ops mutate one
    folder per call.
  - Capital-letter keys for the destructive set. `X` matches
    the `D` permanent-delete naming convention: capital =
    destructive variant. Doesn't collide with `R` (mark-unread,
    list pane) because it's pane-scoped.
  - Spec 18's "optimistic placeholder" for create is skipped:
    a freshly-created folder has no incoming messages and the
    user already expects a quick round-trip. Synchronous-with-
    canonical-ID is simpler and avoids the temp-ID/swap dance.
  - Local update of folders happens after Graph success only;
    no rollback needed because the Graph confirmation IS the
    truth and a local-update failure just delays the UI by one
    sync cycle.
  - Slash-path syntax for nested creates: `"Parent/Child"` is
    resolved by looking up Parent in the local cache (any
    parent depth supported as long as the immediate parent is
    cached locally).
- Result: all packages green under -race + -tags=e2e; gosec
  10 nosec, 0 issues; govulncheck 0 vulnerabilities.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: `Mail.ReadWrite` (already in PRD §3.1).
- [x] Store reads/writes: folders (UpsertFolder for create,
      UpdateFolderDisplayName for rename, DeleteFolder for
      delete). FK cascade removes child messages on delete.
- [x] Graph endpoints: POST /me/mailFolders[/{parent}/childFolders],
      PATCH /me/mailFolders/{id}, DELETE /me/mailFolders/{id}.
- [x] Offline behaviour: synchronous dispatch surfaces a Graph
      error when offline; local store unchanged. User retries
      when connectivity returns.
- [x] Undo: rename and create are easily reversible (rename
      back, delete the new folder); delete is server-soft —
      recoverable from Outlook's Deleted Items per the docs/
      user/how-to.md "Reorganise your mailbox" recipe.
- [x] User errors: §7 table covered. Well-known folder rename
      / delete surfaces Graph's 403 unchanged; empty name
      surfaces a friendly error before hitting Graph.
- [x] Latency budget: not gated; folder ops are network-bound
      and rare.
- [x] Logs: action.Executor's existing logger captures
      create/rename/delete failures. No new redaction-relevant
      fields.
- [x] CLI mode: `inkwell folder new|rename|delete` shipped per
      spec 14 patterns.
- [x] Tests: graph + action + dispatch + e2e all present and
      green.
- [x] **Spec 17 review:** new external HTTP flow (3 calls);
      no PII egress beyond folder displayName which is
      sender-supplied; no new subprocess; no new crypto. No
      threat-model row needed beyond the existing Graph TLS
      coverage.
