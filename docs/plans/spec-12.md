# Spec 12 — Calendar (Read-Only)

## Status
in-progress. `:cal` modal + Graph fetch shipped v0.8.0. Events
schema + persistence + cache-first reads shipped v0.13.x (PR 6a
of audit-drain). Sidebar pane, sync-engine third state, midnight
window slide, week / agenda view, attendees + event-detail modal
remain deferred (PR 6b).

## DoD checklist (mirrored from spec)
- [x] `internal/graph/calendar.go` — `ListEventsBetween(start, end)` + `ListEventsToday()` against `/me/calendarView` with the right $select fields.
- [x] Read-only — no `Calendars.ReadWrite` requested or used.
- [x] `:cal` (and `:calendar`) command opens a modal showing today's events.
- [x] Modal renders time range, subject, organizer, location, online-meeting indicator.
- [x] Esc / `q` closes the modal.
- [x] Loading and error states surfaced in the modal.
- [x] CalendarFetcher interface defined at the consumer site (ui doesn't import internal/graph). cmd_run.go provides a calendarAdapter.
- [x] Tests: `:cal` opens modal + Cmd, fetcher result populates modal, fetcher-not-wired surfaces friendly error, Esc closes.
- [x] events schema migration — shipped v0.13.x (PR 6a). Migration
      `004_events.sql` adds the events table with
      `idx_events_start` + `idx_events_account_start`. The
      `event_attendees` table waits for the detail modal (PR 6b).
- [x] Calendar adapter persists on fetch and reads-cache-first
      with TTL. Stale-data fallback on Graph failure so the
      modal renders the last-known state instead of empty.
- [ ] Sidebar calendar pane (dismissable) — deferred. v0.8.0 is modal-only; the sidebar pane requires layout reflow that's out of scope.
- [ ] `/me/calendarView/delta` sync into local store — deferred. v0.8.0 fetches per `:cal` invocation; no caching.
- [ ] Week / agenda full-screen view — deferred.
- [ ] Event detail modal — deferred. v0.8.0's modal is one-screen list-style.

## Iteration log

### Iter 2 — 2026-04-30 (events schema + persistence, PR 6a of audit-drain)
- Slice: spec 12 §3 schema + cache-first read path. The biggest
  audit-row was "events table never migrated; calendar persisted
  nowhere" — closed structurally by adding the table + adapter
  changes; engine integration carved as PR 6b.
- Files added/modified:
  - `internal/store/migrations/004_events.sql` — events table
    with `idx_events_start` + `idx_events_account_start`. Schema
    version bumped to 4. event_attendees deferred.
  - `internal/store/types.go` — Event struct + EventQuery.
  - `internal/store/events.go` — PutEvent / PutEvents
    (transactional batch upsert), ListEvents (Start/End window
    + ASC sort), DeleteEventsBefore (window-slide hook).
  - `internal/store/store.go` — interface gains 4 methods;
    SchemaVersion = 4.
  - `cmd/inkwell/cmd_run.go` — calendarAdapter now holds
    `st store.Store` + `accountID`. ListEventsToday reads cache
    first; refetches on TTL miss (15 min); persists on success;
    falls back to stale cache on Graph failure.
- Tests:
  - `events_test.go` — round-trip (all fields preserved);
    [Start, End) window semantics (yesterday + tomorrow
    excluded; today's two ASC); DeleteEventsBefore;
    cascade-on-account-delete.
- Decisions:
  - Used a 15-minute TTL on cache freshness. Below 5 min the
    refetch storm dominates a typical day's :cal usage; above
    30 min the user gets stale state too often. 15 is the
    obvious midpoint and matches the spec's "refresh on a
    5-minute timer" wording for mailbox settings (we use a
    longer TTL because calendar churn is lower than mail).
  - Stale-data fallback when Graph fails: spec 12 §5 wants
    the modal to render the last-known state rather than an
    empty modal. The adapter returns cached rows on error
    when any are present; bubbles the error otherwise.
  - event_attendees deferred — there's no detail modal yet,
    so persisting the attendee list would be dead data. Add
    when PR 6b ships the detail view.
  - The calendarAdapter became a struct rather than the
    earlier value type because it holds the store + account
    id. Same pattern as unsubAdapter for spec 16.
- Result: 4 new tests green; full -race + -tags=e2e suite
  green; gosec 0 issues (10 nosec — added one for the events
  ListEvents WHERE composer mirroring the messages pattern);
  govulncheck 0 vulns.

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
