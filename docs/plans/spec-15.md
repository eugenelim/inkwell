# Spec 15 — Compose / Reply (drafts only)

## Status
in-progress (CI scope: viewer-pane reply via `$EDITOR` shipped post-v0.10.0; reply-all, forward, new message, attachments, compose-session crash recovery deferred).

## DoD checklist (mirrored from spec)
- [x] `internal/compose/`: template (reply skeleton), parse (RFC2822-style headers), editor (tempfile + `$INKWELL_EDITOR` / `$EDITOR` / nano fallback).
- [x] `internal/graph/drafts.go`: `CreateReply` (POST /me/messages/{id}/createReply) + `PatchMessageBody` (PATCH /me/messages/{id}) with To / Cc / Bcc / Subject / body update.
- [x] `internal/action/draft.go`: `CreateDraftReply` orchestrates the two Graph calls and returns `{ID, WebLink}`.
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
