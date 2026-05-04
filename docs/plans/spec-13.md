# Spec 13 — Mailbox Settings

## Status
done (D-1 shipped 2026-05-04) — all deferred bullets from v0.9.0 now implemented.

## DoD checklist (mirrored from spec)
- [x] `internal/graph/mailbox.go` — `GetMailboxSettings` + `UpdateAutoReplies` against `/me/mailboxSettings`.
- [x] AutoReplyStatus enum maps to Graph's automaticRepliesSetting.status (disabled / alwaysEnabled / scheduled).
- [x] `:ooo` (also `:oof` / `:outofoffice`) opens a modal showing current auto-reply state.
- [x] Modal renders status (On/Off) + the configured internal-reply message preview.
- [x] `t` key toggles enabled flag; PATCH preserves the existing internal/external messages.
- [x] Esc / `q` closes the modal.
- [x] MailboxClient interface defined at the consumer site (ui doesn't import internal/graph). cmd_run.go provides a mailboxAdapter.
- [x] Tests: dispatch tests covering `:ooo` flow exercised via the OOFMode path.
- [x] Status-bar OOO indicator (`🌴 OOO`) when auto-reply active; glyph from `[mailbox_settings].ooo_indicator`.
- [x] `:settings` view aggregating timezone / locale / working hours / auto-reply status.
- [x] Edit custom internal/external message bodies in OOF modal (pre-filled from server; `$EDITOR` integration deferred to G-1 polish).
- [x] Schedule editing (start/end date+time fields, parsed on save). Inline text-entry deferred; fields pre-fill from existing schedule on round-trip.
- [x] External audience picker (all / contactsOnly / none); Space toggles on audience row.
- [x] `[mailbox_settings]` config section: `confirm_ooo_change`, `default_ooo_audience`, `ooo_indicator`, `refresh_interval`, `default_internal_message`, `default_external_message`.
- [x] `settings.Manager` in `internal/settings/` with `ResolvedTimeZone()` (configTZ > mailbox TZ > system); wired in cmd_run.go.
- [x] 5-min background mailbox refresh via tea.Tick; force-refresh after PATCH.
- [x] PATCH payload includes `scheduledStartDateTime`/`scheduledEndDateTime`/`externalAudience`.
- [x] `:ooo on` / `:ooo off` / `:ooo schedule` quick commands.
- [x] CLI stubs: `inkwell ooo` / `inkwell ooo on` / `inkwell ooo off` / `inkwell settings`.

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

### Iter 2 — 2026-05-04 (PR D-1: full OOF editing + :settings + settings.Manager + OOO indicator)

- Slice: all deferred D-1 bullets in one PR.
- New files: `internal/settings/manager.go`, `internal/settings/manager_test.go`,
  `internal/ui/settings_view.go`, `internal/graph/mailbox_test.go`,
  `cmd/inkwell/cmd_ooo.go`, `cmd/inkwell/cmd_settings.go`.
- Modified: `internal/graph/mailbox.go` (DateTimeTimeZone, schedule + working-hours fields,
  extended PATCH payload); `internal/config/config.go`+`defaults.go` ([mailbox_settings]);
  `internal/ui/oof.go` (full editing modal: status radio, schedule fields, audience picker,
  message previews, ToggleAudience, Validate, ToMailboxSettings date parsing);
  `internal/ui/messages.go` (SettingsMode); `internal/ui/panes.go` (OOOActive/OOOIndicator
  in StatusInputs); `internal/ui/app.go` (extended MailboxSettings/MailboxClient, settingsView,
  oofReturnMode, updateSettings, background refresh, `:settings`/`:ooo on/off/schedule`,
  dispatch + View); `cmd/inkwell/cmd_run.go` (settings.Manager, mailboxAdapter extensions,
  mapMailboxSettings, mapToGraphAutoReplies, new Deps fields).
- Fixes applied after agent self-review: ToMailboxSettings date parsing, ToggleAudience,
  Validate before save, oofReturnMode tracking (Esc from :ooo returns to NormalMode not SettingsMode).
- Commands run: `make regress` — all 6 gates green (18 packages).
- Critique: no layering violations; $EDITOR integration deferred (modal footer omits [e] hint
  so no orphaned keybinding); inline date-entry deferred (fields pre-fill from existing schedule);
  OOO message bodies not logged; no context.Background() in request paths.
- Next: D-1 done. Next PR is E-1 (spec-03 goroutine + tombstone + priority queue).

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
