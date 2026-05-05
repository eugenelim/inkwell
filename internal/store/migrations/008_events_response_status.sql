-- 008_events_response_status.sql
--
-- Spec 12 §3: add response_status column to events so the UI can
-- filter declined events when calendar.show_declined = false.
-- Values match the Graph responseStatus.response field:
--   "accepted" | "tentativelyAccepted" | "declined" |
--   "notResponded" | "none" | "organizer"
-- NULL means "not fetched" (e.g. events ingested before this migration).

ALTER TABLE events ADD COLUMN response_status TEXT;

UPDATE schema_meta SET value = '8' WHERE key = 'version';
