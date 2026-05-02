-- 006_event_attendees.sql
--
-- Spec 12 §3: separate table for event attendees so we can later
-- query "events where Alice is attending." Deferred from 004 because
-- there was no detail modal yet; now that PR 6b-i landed the detail
-- view, PR 6b-ii adds persistence so GetEvent results are cached.

CREATE TABLE event_attendees (
    event_id    TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    address     TEXT NOT NULL,
    name        TEXT,
    type        TEXT,    -- required / optional / resource
    status      TEXT,    -- accepted / tentativelyAccepted / declined / notResponded / none / organizer
    PRIMARY KEY (event_id, address)
);

CREATE INDEX idx_event_attendees_event ON event_attendees(event_id);

UPDATE schema_meta SET value = '6' WHERE key = 'version';
