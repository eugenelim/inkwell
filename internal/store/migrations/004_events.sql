-- 004_events.sql
--
-- Spec 12: persist calendar events locally so the :cal modal can
-- render offline and the next-event peek is instant. Live fetches
-- against /me/calendarView populate this table; the modal reads
-- from here first and only network-fetches on a window miss.
--
-- Schema scope (spec 12 §3):
--   events: one row per calendar entry within the maintained window.
--   No event_attendees table yet — the modal renders organizer +
--   showAs + location only. Attendee expansion lands with the
--   detail modal (PR 6b).
--
-- Instances of recurring series come pre-expanded by Graph's
-- /me/calendarView endpoint, so we don't need a separate `series`
-- table. Each occurrence has its own row keyed by Graph's instance
-- ID.

CREATE TABLE events (
    id                  TEXT PRIMARY KEY,
    account_id          INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    subject             TEXT,
    organizer_name      TEXT,
    organizer_address   TEXT,
    start_at            INTEGER NOT NULL,  -- unix seconds, UTC
    end_at              INTEGER NOT NULL,  -- unix seconds, UTC
    is_all_day          INTEGER NOT NULL DEFAULT 0,
    location            TEXT,
    online_meeting_url  TEXT,
    show_as             TEXT,              -- free / busy / tentative / oof / workingElsewhere
    web_link            TEXT,
    cached_at           INTEGER NOT NULL
);

CREATE INDEX idx_events_account_start ON events(account_id, start_at);
CREATE INDEX idx_events_start         ON events(start_at);

UPDATE schema_meta SET value = '4' WHERE key = 'version';
