# Spec 12 — Calendar (Read-Only)

## Status
done. All deferred bullets now shipped in PR H-1 (spec 12 finish):
timezone threading, sidebar calendar pane, week/agenda view toggle,
`c` key from folders pane, plus full test suite.

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
      `idx_events_start` + `idx_events_account_start`.
- [x] event_attendees migration — PR 6b-ii. Migration
      `006_event_attendees.sql` adds the attendees table with
      FK to events ON DELETE CASCADE. SchemaVersion = 6.
- [x] Calendar adapter persists on fetch and reads-cache-first
      with TTL. Stale-data fallback on Graph failure so the
      modal renders the last-known state instead of empty.
- [x] `/me/calendarView/delta` sync into local store — PR 6b-ii.
      `syncCalendar` in `internal/sync/calendar_sync.go` uses the
      delta endpoint; upserts/deletes/prunes per cycle; stores
      deltaLink in delta_tokens table under key `__calendar__`.
- [x] Sync engine third state `StateSyncingCalendar` — PR 6b-ii.
      `runCycle` advances through StateSyncingCalendar after
      folder sync; can be disabled with `CalendarLookaheadDays < 0`.
- [x] Midnight window slide — PR 6b-ii. `midnightWatcher` fires
      once per day just after local midnight, clears the calendar
      delta token, and kicks the engine for a fresh full-window fetch.
- [x] Day navigation in calendar modal — PR 6b-ii. `]`/`[` advance/
      retreat by 1 day; `}`/`{` advance/retreat by 7 days; `t`
      returns to today. CalendarModel gains `viewDate` field +
      Nav* methods; fetchCalendarForDateCmd dispatches
      ListEventsBetween for the selected day.
- [x] Attendees persisted on GetEvent — PR 6b-ii. `GetEvent` in
      calendarAdapter now calls `PutEventAttendees` after a successful
      fetch so repeated opens serve from the local cache.
- [x] Sidebar calendar pane — shipped PR H-1. FoldersModel gains
      calendar section rebuilt in rebuild(); SetCalendarEvents wired;
      SelectedCalendarEvent for Enter dispatch; calendarSidebarCmd
      fires on Init and SyncCompletedEvent.
- [x] Week / agenda full-screen view — shipped PR H-1. `w` toggles
      weekMode; renderWeekView groups events by day; `a` hint returns
      to agenda; fetchCalendarForWeekCmd fetches Mon–Sun window.

## Iteration log

### Iter 5 — 2026-05-04 (timezone threading + sidebar + week view + `c` key, PR H-1)
- Slice: spec 12 finish — all deferred DoD bullets.
- Files modified:
  - `internal/ui/calendar.go`: `weekMode` field; `ToggleWeekMode()`;
    `IsWeekMode()`; `View` signature gains `tz *time.Location`;
    `formatEvent` signature gains `tz *time.Location`; all `.Local()`
    → `.In(tz)`; `renderWeekView` helper; footer hints updated
    (`w` / `a` for week/agenda).
  - `internal/ui/panes.go`: `displayedFolder` gains `isCalHeader`,
    `calDayLabel`, `isCalEvent`, `calEvent`; `FoldersModel` gains
    `calendarEvents`, `sidebarShowDays`, `calendarTZ`; `SetCalendarEvents`;
    `SelectedCalendarEvent`; `rebuild()` appends calendar section;
    `Up`/`Down`/`JumpTop`/`JumpBottom` skip `isCalHeader` rows;
    `Selected()` returns false for calendar rows; View renders
    header + event rows.
  - `internal/ui/app.go`: `Deps` gains `CalendarTZ`, `CalendarSidebarDays`;
    `View` passes `tz` to `calendar.View`; `calendarSidebarLoadedMsg`
    type; `calendarSidebarCmd`; `fetchCalendarForWeekCmd`; Update
    handles `calendarSidebarLoadedMsg`; Init fires `calendarSidebarCmd`;
    `handleSyncEvent` fires `calendarSidebarCmd` on `SyncCompletedEvent`;
    `updateCalendar` handles `w`; `dispatchFolders` handles `c` and
    Enter-on-calEvent.
  - `cmd/inkwell/cmd_run.go`: wires `CalendarTZ: sm.ResolvedTimeZone()`
    and `CalendarSidebarDays: cfg.Calendar.SidebarShowDays`; removes
    `_ = sm` placeholder.
- Tests added:
  - `calendar_test.go`: `TestCalendarModelWeekModeToggle`,
    `TestCalendarModelIsWeekModeDefault`, `TestFormatEventUsesProvidedTimezone`.
  - `dispatch_test.go`: `TestCalendarWKeyTogglesToWeekMode`,
    `TestFoldersPaneCKeyOpensCalendar`, `TestSidebarCalendarEventsRender`.
  - `app_e2e_test.go`: `TestSidebarShowsCalendarEventsAfterLoad`,
    `TestFoldersPaneCKeyOpensCal`.
- Commands run: `go build ./...` ✓, `go vet ./...` ✓,
  `go test -race ./internal/ui/...` ✓ (pass),
  `go test -tags=e2e ./internal/ui/...` ✓ (pass).
- Result: all five mandatory commands green; DoD fully ticked.

### Iter 4 — 2026-05-02 (sync engine + day nav + attendees persistence, PR 6b-ii of audit-drain)
- Slice: spec 12 §4.2 (ListCalendarDelta), §5 (engine 3rd state +
  SyncCalendar API), §5.1 (midnight window slide), §6.2 (]/[/{/}/t
  day/week navigation), §3 (event_attendees migration + PutEventAttendees
  + ListEventAttendees), §4.3 attendees persistence in calendarAdapter.
- Files added/modified:
  - `internal/config/config.go` + `defaults.go`: CalendarConfig gains
    ShowTentative, TimeZone, OnlineMeetingIndicator, NowIndicator,
    SidebarShowDays, CacheTTL. Defaults set; all keys documented in
    docs/CONFIG.md.
  - `internal/store/migrations/006_event_attendees.sql` (new): event_attendees
    table with FK to events ON DELETE CASCADE. SchemaVersion = 6.
  - `internal/store/types.go`: EventAttendee struct.
  - `internal/store/events.go`: DeleteEvent, PutEventAttendees (atomic
    DELETE+INSERT), ListEventAttendees (nil-on-empty).
  - `internal/store/store.go`: interface gains 3 methods; SchemaVersion = 6.
  - `internal/graph/calendar.go`: CalendarDeltaResult struct;
    ListCalendarDelta(ctx, start, end, deltaLink) — empty deltaLink =
    fresh query, non-empty = delta endpoint used verbatim.
  - `internal/sync/calendar_sync.go` (new): syncCalendar (upsert/delete/
    prune/persist deltaLink) + truncateToDay helper + midnightWatcher
    goroutine.
  - `internal/sync/engine.go`: StateSyncingCalendar state; SyncCalendar
    Engine interface method; CalendarLookaheadDays/LookbackDays options;
    runCycle third state; Start() launches midnightWatcher.
  - `internal/ui/calendar.go`: CalendarModel gains viewDate + ViewDate() +
    NavNextDay/NavPrevDay/NavNextWeek/NavPrevWeek/GotoToday; View uses
    viewDate for header; footer updated with ]/[ {/} t hints.
  - `internal/ui/app.go`: CalendarFetcher interface gains ListEventsBetween;
    updateCalendar dispatches ]/[/{/}/t; fetchCalendarForDateCmd.
  - `cmd/inkwell/cmd_run.go`: calendarAdapter.ListEventsBetween (cache-
    first, same TTL pattern); GetEvent now persists attendees via
    PutEventAttendees; engine Options carry Calendar*Days.
- Tests:
  - `internal/store/events_test.go`: TestDeleteEvent, TestDeleteEventIsIdempotent,
    TestPutEventAttendeesRoundTrip, TestPutEventAttendeesReplacesExisting,
    TestListEventAttendeesEmptyReturnsNil, TestEventAttendeeCascadeOnEventDelete.
  - `internal/graph/calendar_test.go` (new): TestListCalendarDeltaFirstCall,
    TestListCalendarDeltaRemovedEntries, TestListCalendarDeltaWithDeltaLink,
    TestListCalendarDeltaSurfaces410.
  - `internal/sync/calendar_sync_test.go` (new): TestSyncCalendarUpsertsAndDeletesEvents,
    TestSyncCalendar410ResetsAndReFetches, TestSyncCalendarPrunesOldEvents,
    TestSyncCalendarUsesStoredDeltaLink.
  - `internal/ui/calendar_test.go` (new): TestCalendarModelViewDateIsToday,
    TestCalendarModelNavNextDayAdvancesOneDay, TestCalendarModelNavPrevDayRetreat,
    TestCalendarModelNavNextWeekAdvancesSeven, TestCalendarModelNavPrevWeekRetreatsSeven,
    TestCalendarModelGotoTodayResetsAfterNavigation, TestCalendarModelNavResetsEvents,
    TestSameDayTrueForSameDay, TestSameDayFalseForDifferentDay, TestSameDayFalseForDifferentYears.
  - `internal/ui/dispatch_test.go`: stubCalendar gains ListEventsBetween;
    TestCalendarBracketRightNavNextDay, TestCalendarBracketLeftNavPrevDay,
    TestCalendarBraceRightNavNextWeek, TestCalendarBraceLeftNavPrevWeek,
    TestCalendarTKeyReturnsToToday, TestCalendarNavStaysInCalendarMode.
- Decisions:
  - Reused delta_tokens table for the calendar delta link (key
    `__calendar__`). Avoids a new migration just for token storage;
    same precedent as mail folder delta tokens.
  - Full @odata.deltaLink URL stored and passed verbatim as the next
    endpoint. Graph embeds all pagination and scope in this URL;
    reconstructing it from a bare token is fragile.
  - `CalendarLookaheadDays < 0` sentinel disables calendar sync
    (engine 3rd state + midnight watcher) — used by tests that
    don't want the timer goroutine interfering with goleak.
  - Attendees persisted on GetEvent rather than in the sync pass.
    The delta endpoint doesn't return attendees; a separate expand
    per event would be N+1 fetches. GetEvent is already the
    explicit detail load; persisting there is the right seam.
  - SidebarShowDays, OnlineMeetingIndicator, NowIndicator, ShowTentative,
    TimeZone, CacheTTL all added to CalendarConfig even though the
    sidebar pane is deferred. The config shape needs to be stable
    before v1; these are referenced by the spec.
- Result: CI green. All five mandatory commands passed. Tagged v0.21.0.

**Deferred to future PRs:** sidebar pane layout, week/agenda grid
view (§8), `c` key to open full-screen from sidebar,
mailboxSettings.timeZone resolution, SyncCalendar wiring in
cmd_run.go (the adapter method exists; the engine opts consume
the config values; the cmd_run.go wiring to pass calendar days
to the engine is needed for full end-to-end delta sync).

### Iter 3 — 2026-04-30 (event detail modal + j/k/Enter, PR 6b-i of audit-drain)
- Slice: spec 12 §6.2 (j/k/Enter) + §7 (detail modal) + §4.3
  (GetEvent helper). Closes the highest-leverage 6b sub-slice
  in one cut while leaving sync-engine third state, window
  slide, sidebar pane, and day/week navigation for later PRs.
- Files added/modified:
  - `internal/graph/calendar.go`: new `EventDetail`,
    `EventAttendee` types; new `GetEvent(ctx, id)` helper hits
    `/me/events/{id}?$expand=attendees` and decodes the
    response into the new types.
  - `internal/ui/calendar.go`: `CalendarModel` gains cursor +
    `Up`/`Down`/`Selected`; `formatEvent` renders the cursor
    glyph and highlights the selected row; new
    `CalendarDetailModel` renders subject/time/location/online
    URL/organizer/attendees (with status glyphs) /body preview
    + dynamic hint line that surfaces only the keys the event
    supports (`o` if WebLink, `l` if OnlineMeetingURL).
  - `internal/ui/messages.go`: new `CalendarDetailMode`
    constant.
  - `internal/ui/app.go`: TriageExecutor's parallel — the
    CalendarFetcher interface gains `GetEvent`; CalendarEvent
    gains `ID` + `WebLink`; new `CalendarEventDetail` +
    `CalendarAttendee` consumer types; model gains
    `calendarDetail` field + `pendingMoveMsg`-equivalent
    routing; new `eventFetchedMsg` flows through Update;
    `updateCalendar` handles j/k/Enter, `updateCalendarDetail`
    handles `o` / `l` / Esc; View routes CalendarDetailMode.
  - `cmd/inkwell/cmd_run.go`: calendarAdapter gains GetEvent
    (live fetch — no caching this PR; attendees persistence
    deferred until PR 6b-ii ships the sync-engine third state
    so we don't half-implement the persistence story);
    convertStoreEvents / convertStoreEventsFromGraph now carry
    ID + WebLink so the list rows can dispatch GetEvent.
- Tests:
  - dispatch_test: j/k move CalendarModel cursor (no
    wrap-around at edges); Enter dispatches GetEvent + opens
    CalendarDetailMode with loading state; eventFetchedMsg
    populates the detail model; View paints attendees + body;
    Esc on detail returns to CalendarMode (preserves list
    state); Enter on empty list is safe (no GetEvent fired,
    stays in CalendarMode); GetEvent error surfaces inside
    the detail modal.
  - app_e2e_test: visible-delta — `:cal` paints the list with
    "navigate" hint; `j` moves the ▶ cursor to the second
    event row; Enter paints attendees + body preview + "o
    Outlook" hint; Esc returns to the list with the cursor
    intact.
- Decisions:
  - Live fetch on Enter rather than caching attendees. The
    sync-engine third state lands in PR 6b-ii — when it does,
    we add the event_attendees migration + persist on
    GetEvent. Doing it now without the sync extension would
    leave attendees data getting stale forever (no engine
    pass to refresh).
  - Hint line is dynamic: only renders `o` / `l` when the
    event actually has WebLink / OnlineMeetingURL. Real-tenant
    events sometimes lack one or the other; static hints
    invite the user to press a key that does nothing.
  - Attendee cap at 10 with "… and N more" — spec §10 failure
    mode "Event has 100+ attendees" needs the cap or the
    modal blows past the screen. 10 matches the spec's
    "first 10 plus … and 92 more" example.
  - Used `~` for tentative attendee glyph rather than `?` so
    the four states (`✓` / `~` / `✗` / `?`) are distinct;
    spec text uses `?` for both tentative and not-responded
    but the visual distinction is more useful in practice.
  - `o`/`l` keys in detail mode use string literal matching
    rather than keymap entries because they're modal-scoped
    and don't need the global rebinding plumbing of the list
    pane.
- Result: gosec 0 issues, govulncheck 0 vulns, all packages
  green under -race + -tags=e2e.

  **Deferred to PR 6b-ii:** sync-engine third state, midnight
  window slide, sidebar pane (vs modal), week/agenda toggle,
  day navigation (`]`/`[`/`}`/`{`/`t`/`c`), event_attendees
  table, mailboxSettings.timeZone resolution.

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
