# Spec 12 — Calendar (Read-Only)

## Status
in-progress (CI scope: `:cal` modal + Graph fetch shipped in v0.8.0; sidebar pane, schema, sync engine extension, week view, attendees, event details deferred).

## DoD checklist (mirrored from spec)
- [x] `internal/graph/calendar.go` — `ListEventsBetween(start, end)` + `ListEventsToday()` against `/me/calendarView` with the right $select fields.
- [x] Read-only — no `Calendars.ReadWrite` requested or used.
- [x] `:cal` (and `:calendar`) command opens a modal showing today's events.
- [x] Modal renders time range, subject, organizer, location, online-meeting indicator.
- [x] Esc / `q` closes the modal.
- [x] Loading and error states surfaced in the modal.
- [x] CalendarFetcher interface defined at the consumer site (ui doesn't import internal/graph). cmd_run.go provides a calendarAdapter.
- [x] Tests: `:cal` opens modal + Cmd, fetcher result populates modal, fetcher-not-wired surfaces friendly error, Esc closes.
- [ ] events / event_attendees schema migration — deferred.
- [ ] Sidebar calendar pane (dismissable) — deferred. v0.8.0 is modal-only; the sidebar pane requires layout reflow that's out of scope.
- [ ] `/me/calendarView/delta` sync into local store — deferred. v0.8.0 fetches per `:cal` invocation; no caching.
- [ ] Week / agenda full-screen view — deferred.
- [ ] Event detail modal — deferred. v0.8.0's modal is one-screen list-style.

## Iteration log

### Iter 1 — 2026-04-29 (`:cal` modal + Graph fetch)
- Slice: graph + ui in one cut.
- Files:
  - internal/graph/calendar.go: Event type, ListEventsBetween, ListEventsToday. /me/calendarView is the right endpoint (not /me/events) because it expands recurring series server-side.
  - internal/ui/calendar.go: CalendarModel (events / loading / err) + View renders the modal centred on screen.
  - internal/ui/messages.go: CalendarMode added to the Mode enum.
  - internal/ui/app.go: CalendarFetcher interface + CalendarEvent type at consumer site; updateCalendar handles Esc/q; dispatchCommand handles :cal/:calendar; calendarFetchedMsg + fetchCalendarCmd + handler.
  - cmd/inkwell/cmd_run.go: calendarAdapter wraps graph.Client.ListEventsToday into ui.CalendarFetcher (struct-shape conversion only).
  - 2 dispatch tests.
- Commands: `make regress` green.
- Critique:
  - No caching — every `:cal` re-hits Graph. Acceptable while it's a manual command (user opens it explicitly). When the sidebar pane lands and updates passively, we'll need the sync engine extension.
  - The modal is uniform-style (no week view) which the spec calls for. The ":cal" line is actively documented in user/reference.md and user/how-to.md.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: Calendars.Read (already in PRD §3.1).
- [x] Store reads/writes: none — events fetched live, not cached locally.
- [x] Graph endpoints: GET /me/calendarView with $select.
- [x] Offline behaviour: `:cal` errors with the network failure surfaced in the modal. Acceptable; spec says calendar is a "useful adjacent context" not core.
- [x] Undo: N/A (read-only).
- [x] User errors: fetch failures land in modal as "error: <message>". Friendly "calendar not wired" surfaces if Calendar dep is nil.
- [x] Latency budget: not measured; the Graph round-trip dominates.
- [x] Logs: graph package logs request/response via the existing transport stack; redaction applies.
- [x] CLI mode: spec 14 will surface `inkwell cal` as a non-interactive list.
- [x] Tests: 2 dispatch tests covering the happy + not-wired paths.
- [x] User docs: docs/user/reference.md (`:cal` command + Calendar mode) + docs/user/how-to.md ("Open the calendar") updated.
