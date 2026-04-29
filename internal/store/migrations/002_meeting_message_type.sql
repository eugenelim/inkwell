-- 002_meeting_message_type.sql
--
-- Adds Graph's `meetingMessageType` field to the messages row so the
-- list pane can render the calendar-invite indicator from the
-- canonical signal instead of a subject-prefix heuristic. Real-tenant
-- bug: invites whose subject didn't start with "Accepted:" / "Meeting:"
-- / etc. silently lost their 📅 glyph (panes.go isLikelyMeeting).
--
-- Values mirror Graph's enum: "" (empty string for non-meetings),
-- "meetingRequest", "meetingCancellation", "meetingResponse",
-- "meetingForwardNotification", "none". NULL is also valid for rows
-- synced before this migration; the renderer treats NULL identically
-- to "none".

ALTER TABLE messages ADD COLUMN meeting_message_type TEXT;

UPDATE schema_meta SET value = '2' WHERE key = 'version';
