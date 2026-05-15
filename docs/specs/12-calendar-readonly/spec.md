# Spec 12 — Calendar (Read-Only)

**Status:** In progress (CI scope, v0.8.x → v0.13.x). `:cal` modal with Graph fetch + cache-first reads (PR 6a) + detail modal with `j`/`k`/`Enter` navigation + GetEvent($expand=attendees) (PR 6b-i). Residual: third sync-engine state for periodic calendar pull, midnight window-slide goroutine, sidebar-pane vs modal layout decision, day/week navigation (`]` / `[` / `}` / `{` / `t` / `c`), timezone resolution from `mailboxSettings.timeZone` — all tracked under PR 6b-ii.
**Depends on:** Specs 02 (store), 03 (sync engine reused for calendar deltas), 04 (TUI for sidebar pane).
**Blocks:** Nothing.
**Estimated effort:** 1–2 days.

---

## 1. Goal

Display the user's calendar in the TUI as a read-only sidebar pane. Show today's events, allow week navigation, render event details, and reflect changes within sync intervals. **No write operations** — `Calendars.ReadWrite` is denied (PRD §3.2). Acceptance, declination, modification, and creation all happen in native Outlook.

This is a "useful adjacent context" feature. A senior professional triaging email also needs to know what's on the calendar today; this pane prevents an alt-tab to Outlook for that question.

## 2. Module layout

```
internal/graph/
└── calendar.go        # /me/calendar, /me/events, /me/calendarView REST helpers

internal/store/
└── events.go          # events table CRUD (separate from messages)

internal/sync/
└── calendar_sync.go   # calendar delta sync, parallel to mail sync

internal/ui/
├── calendar_pane.go   # sidebar calendar view
└── calendar_modal.go  # full-screen calendar (week/agenda view)
```

## 3. Schema additions

A new `events` table (extends spec 02 schema, requires migration `002_calendar.sql`):

```sql
CREATE TABLE events (
    id                   TEXT PRIMARY KEY,         -- Graph event id
    account_id           INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    calendar_id          TEXT,                     -- if multiple calendars; NULL for default
    ical_uid             TEXT,                     -- iCalUId
    subject              TEXT,
    body_preview         TEXT,
    organizer_address    TEXT,
    organizer_name       TEXT,
    start_at             INTEGER NOT NULL,         -- unix epoch seconds (UTC)
    end_at               INTEGER NOT NULL,
    is_all_day           INTEGER NOT NULL DEFAULT 0,
    location             TEXT,
    online_meeting_url   TEXT,
    online_meeting_provider TEXT,                  -- 'teamsForBusiness' | 'skypeForBusiness' | NULL
    response_status      TEXT,                     -- 'accepted' | 'tentativelyAccepted' | 'declined' | 'notResponded' | 'organizer'
    show_as              TEXT,                     -- 'free' | 'busy' | 'tentative' | 'oof' | 'workingElsewhere'
    sensitivity          TEXT,
    is_recurring         INTEGER NOT NULL DEFAULT 0,
    series_master_id     TEXT,                     -- for instances of a recurring series
    type                 TEXT,                     -- 'singleInstance' | 'occurrence' | 'exception' | 'seriesMaster'
    web_link             TEXT,
    last_modified_at     INTEGER,
    cached_at            INTEGER NOT NULL
);

CREATE INDEX idx_events_start ON events(start_at);
CREATE INDEX idx_events_account_start ON events(account_id, start_at);
CREATE INDEX idx_events_series ON events(series_master_id);

CREATE TABLE event_attendees (
    event_id     TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    address      TEXT NOT NULL,
    name         TEXT,
    type         TEXT,                             -- 'required' | 'optional' | 'resource'
    status       TEXT,                             -- their response
    PRIMARY KEY (event_id, address)
);

CREATE INDEX idx_attendees_event ON event_attendees(event_id);
```

Attendees in a separate table because we may want to query "events where Alice is attending" (e.g., for the "Did the call cover Mike's ticket" use case from training data).

The events table replaces, for calendar items, the role that `messages` plays for mail. Calendar bodies are rare and short; we cache them inline in `body_preview` and don't bother with a separate `bodies`-equivalent.

## 4. Graph endpoints used

### 4.1 Listing events

For the sidebar's "today" view, use **calendarView** rather than `events`:

```
GET /me/calendarView?startDateTime=2026-04-27T00:00:00&endDateTime=2026-04-27T23:59:59&$top=50&$select=...
```

`calendarView` expands recurring series into individual occurrences, which is exactly what we want for display. The `events` endpoint returns recurring series as single rows with recurrence rules attached, requiring client-side expansion.

### 4.2 Delta sync

Calendar supports delta on `/me/calendarView/delta`:

```
GET /me/calendarView/delta?startDateTime=...&endDateTime=...
```

The window is bounded; we sync `[lookback_days]` days back through `[lookahead_days]` days forward. Out-of-window events are not delta-tracked; we re-fetch on demand.

When the window slides (new day, etc.), we adjust both `startDateTime` and `endDateTime` and reset the delta token. The window slide happens at midnight local time.

### 4.3 Event detail fetch

`GET /me/events/{id}?$expand=attendees` for the full event when the user opens it.

### 4.4 Free/busy

Out of scope for v1. Useful for "find a time" but requires write scope to act on the result.

## 5. Calendar sync engine extension

The existing `internal/sync` engine (spec 03) is extended:

```go
type Engine interface {
    // ... existing methods ...

    // SyncCalendar runs a calendar delta sync. Called on the same cadence as mail.
    SyncCalendar(ctx context.Context) error
}
```

The main sync loop (spec 03 §4) gets a third state: after `syncing folders`, run `syncing calendar` before returning to idle. Both share the concurrency budget and throttling transport.

The calendar window is initialized from `[calendar].lookback_days` and `[calendar].lookahead_days`. On first run, a full fetch of that window populates the table. Subsequent ticks use delta.

### 5.1 Window slide

A small goroutine wakes daily at local midnight. It:

1. Computes new window bounds from current local time.
2. Clears delta token.
3. Triggers a calendar full-window fetch.
4. Drops events outside the new window from the local cache.

This is a once-per-day operation; the cost is negligible.

## 6. UI: sidebar calendar pane

The folders pane gets a third section (after Saved Searches):

```
▾ Mail
  Inbox          47
  Drafts          3
  ...

▾ Saved Searches
  ☆ Newsletters    247
  ...

▾ Today · Mon Apr 27
  09:00–10:00  Daily standup
  10:30–11:00  ▶ Q4 review prep   (now)
  14:00–15:00  TIAA SME interview
  16:30–17:00  1:1 with manager

  ▾ Tomorrow
  09:00–09:30  Coffee with Alice
  ...
```

### 6.1 Visual conventions

- Times in 24-hour format (configurable; covered by mailbox locale via `[calendar].time_zone` or system locale).
- The currently-active event marked with `▶` and a "now" hint in muted color.
- All-day events listed with `📅 ` prefix and no time.
- Events with online meeting URLs marked with `🔗` (or `&` ASCII fallback).
- Tentatively accepted events rendered in muted color.
- Declined events not shown by default (configurable via `[calendar].show_declined`).

### 6.2 Interactions

- `j`/`k` while pane focused: navigate events.
- `Enter` on focused event: open detail modal (§7).
- `t` (today): scroll to today.
- `]`, `[`: next/previous day.
- `}`, `{`: next/previous week.
- `c` while sidebar focused: open full calendar view (§8).
- `:cal` command: same as `c`.

## 7. Event detail modal

```
   ╭───────────────────────────────────────────────────────────────────╮
   │  Q4 review prep                                                    │
   │                                                                    │
   │  📅 Mon 27 Apr 2026, 10:30–11:00 EDT (30 min)                      │
   │  📍 Conference Room 3                                              │
   │  🔗 https://teams.microsoft.com/l/meetup-join/...   [press l]      │
   │                                                                    │
   │  Organizer: Alice Smith <alice@example.invalid>                     │
   │  Attendees:                                                        │
   │    ✓ Eu Gene Eu <eu.gene@example.invalid>          (you)            │
   │    ✓ Bob Acme <bob.acme@vendor.com>                               │
   │    ? Charlie Lee <charlie@example.invalid>                          │
   │    ✗ Dana Wong <dana@example.invalid>                                │
   │                                                                    │
   │  Body:                                                             │
   │    Going through the final draft of the Q4 deck. Please review    │
   │    sections 3 and 5 before the call.                              │
   │                                                                    │
   │  [o] open in Outlook   [l] join meeting   [Esc] close              │
   ╰───────────────────────────────────────────────────────────────────╯
```

`o` opens `webLink` (Outlook web view). `l` opens the online meeting URL. **No accept/decline/edit buttons.** Attempting any modification opens Outlook.

Attendee status glyphs: `✓` accepted, `?` not responded / tentative, `✗` declined.

## 8. Full calendar view

`:cal` opens a full-screen calendar:

### 8.1 Default: today's agenda

```
Calendar · Today, Mon Apr 27 2026                     [a] agenda  [w] week  [Esc]

08:00 ┌───────────────────────────────────────────────────────────────┐
      │                                                                 │
09:00 │ ▌ Daily standup                          09:00–10:00            │
      │ ▌ team / online                                                 │
10:00 │                                                                 │
      │ ▌▌▌ Q4 review prep                       10:30–11:00            │
11:00 │ ▌▌▌ alice@example.invalid                                         │
      │                                                                 │
12:00 │                                                                 │
      │ (lunch — no events)                                             │
13:00 │                                                                 │
      │                                                                 │
14:00 │ ▌ TIAA SME interview                     14:00–15:00            │
      │ ▌ Conference Room 3                                             │
15:00 │                                                                 │
      │                                                                 │
16:00 │ ▌▌▌ Now: Free                                                  │
      │                                                                 │
16:30 │ ▌▌▌ 1:1 with manager                     16:30–17:00            │
17:00 │ ▌▌▌                                                            │
      └───────────────────────────────────────────────────────────────┘
```

Block characters render approximate event durations vertically. Event detail shows on selection.

### 8.2 Week view

`w` toggles to week:

```
Week of Mon Apr 27                                       [a] agenda  [Esc]

       Mon 27        Tue 28        Wed 29        Thu 30        Fri  1
09:00  Standup       Standup       Standup       Standup       Standup
10:00
       Q4 review     [free]        Demo prep     Demo
11:00
12:00
13:00
14:00  TIAA SME      [free]        Architecture  Bilateral     [free]
15:00                              review        w/ Alice
16:00
       1:1 mgr                     [free]                       Beer
17:00
```

Compressed; only top-of-hour. Selecting a cell drills to event detail.

### 8.3 Agenda view

`a` shows a flat list:

```
Today, Mon Apr 27

  09:00–10:00  Daily standup                      ✓
  10:30–11:00  Q4 review prep                     ?  online: Teams
  14:00–15:00  TIAA SME interview                 ✓  Conf Rm 3
  16:30–17:00  1:1 with manager                   ✓

Tomorrow, Tue Apr 28

  09:00–09:30  Coffee with Alice                  ✓
  ...
```

This is the default for `:cal`; the day grid is entered via `[w]` toggle.

## 9. Configuration

This spec owns the `[calendar]` section. Full reference in `CONFIG.md`.

| Key | Default | Used in § |
| --- | --- | --- |
| `calendar.default_view` | `"agenda"` | §8 (initial) |
| `calendar.lookback_days` | `7` | §5 |
| `calendar.lookahead_days` | `30` | §5 |
| `calendar.time_zone` | `""` (mailbox setting) | §6.1 (display) |
| `calendar.show_declined` | `false` | §6.1 |

New keys this spec adds:

| Key | Default | Used in § |
| --- | --- | --- |
| `calendar.sidebar_show_days` | `2` | §6 (today + N more days) |
| `calendar.show_tentative` | `true` | §6.1 |
| `calendar.online_meeting_indicator` | `"🔗"` | §6.1 |
| `calendar.now_indicator` | `"▶"` | §6.1 |

## 10. Failure modes

| Scenario | Behavior |
| --- | --- |
| User has no events in window | Sidebar shows "No events today" / "No events this week"; not an error. |
| Calendar API throttles (429) | Same Retry-After handling as mail; sidebar shows last-known events with stale indicator. |
| Recurring event series modified upstream | Delta returns updated occurrences; we replace by id. |
| Time zone mismatch | We always store in UTC; display converts via `[calendar].time_zone` or system locale. |
| User in unusual time zone (non-IANA) | Fall back to system local time; log warning. |
| Event has no organizer (rare; some imported events) | Show `(no organizer)` placeholder. |
| Very long subject | Truncate to viewport width with ellipsis. |
| Event has 100+ attendees | Show first 10 plus `… and 92 more`. |

## 11. Test plan

### Unit tests

- Window slide computes correct UTC bounds for various time zones (DST transitions).
- Delta application: insert, update, delete handled.
- Event detail rendering: golden tests for various event shapes (recurring instance, all-day, online meeting, declined).

### Integration tests

- Mocked Graph calendar; assert event upsert into store.
- Assert calendarView vs events endpoint usage (we use calendarView for window).
- Assert delta token persistence and reuse.

### Manual smoke tests

1. Sign in; sidebar populates with today's events.
2. Add a meeting in Outlook; within sync interval, appears in sidebar.
3. Decline a meeting in Outlook; sidebar updates.
4. Cross midnight; window slides; today's events refresh.
5. `:cal`; week view; navigate.
6. Open event; click meeting URL; Teams opens.

## 12. Definition of done

- [ ] `events` and `event_attendees` tables created via migration `002`.
- [ ] Calendar sync runs on the same cadence as mail.
- [ ] Sidebar pane renders today + next 1 day with correct event styling.
- [ ] `:cal` opens full view; week and agenda toggleable.
- [ ] Event detail modal works; `o` opens Outlook; `l` opens meeting URL.
- [ ] Window slide at midnight verified.
- [ ] All failure modes handled cleanly.

## 13. Out of scope

- Any write operation. PRD §3.2 hard out-of-scope.
- Free/busy lookups for other users.
- Multi-calendar UI (the user's default calendar only in v1; the schema supports multiple, but the UI pretends there's one).
- Calendar invitations rendered as actionable in the mail viewer (currently they just appear as messages with `meetingMessageType` — we render them as plain mail).
- Reminder notifications. Not in v1; we don't have a notification subsystem.
- ICS file import/export.
- Time zone conversion UI ("show in PT" toggle). v1 uses one configured TZ.
