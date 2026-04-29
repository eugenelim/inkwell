# Spec 13 — Mailbox Settings

## Status
in-progress (CI scope: `:ooo` modal with view + toggle shipped in v0.9.0; status-bar OOO indicator, custom-message editing, schedule, audience options, working-hours / locale / timezone display deferred).

## DoD checklist (mirrored from spec)
- [x] `internal/graph/mailbox.go` — `GetMailboxSettings` + `UpdateAutoReplies` against `/me/mailboxSettings`.
- [x] AutoReplyStatus enum maps to Graph's automaticRepliesSetting.status (disabled / alwaysEnabled / scheduled).
- [x] `:ooo` (also `:oof` / `:outofoffice`) opens a modal showing current auto-reply state.
- [x] Modal renders status (On/Off) + the configured internal-reply message preview.
- [x] `t` key toggles enabled flag; PATCH preserves the existing internal/external messages.
- [x] Esc / `q` closes the modal.
- [x] MailboxClient interface defined at the consumer site (ui doesn't import internal/graph). cmd_run.go provides a mailboxAdapter.
- [x] Tests: dispatch tests covering `:ooo` flow exercised via the OOFMode path.
- [ ] Status-bar OOO indicator (e.g., `🌴 OOO`) when enabled — deferred.
- [ ] `:settings` view aggregating timezone / locale / working hours — deferred.
- [ ] Edit custom internal/external messages — deferred. v0.9.0 preserves whatever the user has set in Outlook.
- [ ] Schedule editing (start/end dates) — deferred.
- [ ] External audience picker (all / contactsOnly / none) — deferred. The UI assumes "all".

## Iteration log

### Iter 1 — 2026-04-29 (`:ooo` toggle modal)
- Slice: graph + ui + cmd_run in one cut.
- Files:
  - internal/graph/mailbox.go: AutoReplyStatus, MailboxSettings, AutoRepliesSetting, GetMailboxSettings, UpdateAutoReplies.
  - internal/ui/oof.go: OOFModel + View renders header / status / preview / footer with [t] toggle / [esc] close.
  - internal/ui/messages.go: OOFMode added to the Mode enum.
  - internal/ui/app.go: MailboxClient interface + MailboxSettings type at consumer site; updateOOF handles t/Esc/q; dispatchCommand handles :ooo/:oof/:outofoffice; oofLoadedMsg + oofToggledMsg + Cmds.
  - cmd/inkwell/cmd_run.go: mailboxAdapter wraps graph client into ui.MailboxClient.
- Critique:
  - No status-bar indicator yet. The spec calls for one ("🌴 OOO" in the top bar). Adds layout work to status.go; defer to v0.9.x.
  - Toggle preserves the message, but doesn't surface the schedule (if the user has "scheduled" set in Outlook, our toggle flips status to "alwaysEnabled" or "disabled", losing the schedule). The spec calls this out — accept for v0.9.0; richer edits land later.
  - External audience hard-coded to "all" on the SET path. Outlook's default is also "all", so this is rarely wrong.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: MailboxSettings.Read + MailboxSettings.ReadWrite (already in PRD §3.1).
- [x] Store reads/writes: none — mailbox settings are not cached. Refetch on every `:ooo` invocation.
- [x] Graph endpoints: GET /me/mailboxSettings, PATCH /me/mailboxSettings.
- [x] Offline behaviour: `:ooo` errors with the network failure surfaced in the modal.
- [x] Undo: N/A. Toggle is reversible via another toggle.
- [x] User errors: GET / PATCH failures land in modal as "error: <message>". Friendly "ooo: not wired" if Mailbox dep is nil.
- [x] Latency budget: not measured; the Graph round-trip dominates (~100-300ms).
- [x] Logs: graph package logs request/response; redaction applies to the body of the auto-reply message (not currently logged but defensive).
- [x] CLI mode: spec 14 will surface `inkwell ooo on/off` as a non-interactive toggle.
- [x] User docs: docs/user/reference.md (`:ooo` + OOO mode rows) + docs/user/how-to.md ("Toggle out-of-office") updated.
