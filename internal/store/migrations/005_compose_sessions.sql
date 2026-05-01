-- 005_compose_sessions.sql
--
-- Spec 15 §7 — crash-recovery snapshot table for the in-modal
-- compose pane (PR 7-ii). The `internal/ui/ComposeModel` snapshots
-- its form state (kind / source_id / to / cc / subject / body) into
-- a JSON blob persisted here on entry and on focus change. On
-- startup, the resume scan reads the most recent unconfirmed row
-- and offers to restore the user's draft via a confirm modal.
--
-- Why a JSON blob (vs columnar fields): the snapshot payload IS
-- the form state — a single Restore() call hydrates the UI without
-- per-field unmarshal logic. Schema evolution stays cheap (add a
-- field on `ComposeSnapshot`, no migration needed). The trade-off
-- (no SQL filtering on body content) is a non-cost: we only ever
-- look these up by `confirmed_at IS NULL` on launch.
--
-- Lifecycle:
--   * Created at compose entry with the skeleton snapshot.
--   * Updated on focus-change so each Tab captures the field the
--     user just left.
--   * `confirmed_at = now()` on save (Ctrl+S / Esc) or discard
--     (Ctrl+D). Confirmed sessions older than 24h are deleted on
--     next launch (GC pass).
--
-- The FK on `source_id` uses ON DELETE SET NULL: if the source
-- message is deleted between the crash and the resume, we still
-- want the body content but lose the reply-source linkage (the
-- resume modal can warn the user and let them fix it manually).
--
-- The partial index `idx_compose_sessions_unconfirmed` accelerates
-- the launch-time "any unconfirmed sessions?" query — the only
-- real query path against this table.

CREATE TABLE compose_sessions (
    session_id    TEXT PRIMARY KEY,
    kind          TEXT NOT NULL,
    source_id     TEXT REFERENCES messages(id) ON DELETE SET NULL,
    snapshot      TEXT NOT NULL,            -- JSON-encoded ComposeSnapshot
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    confirmed_at  INTEGER                    -- NULL while in flight
);

CREATE INDEX idx_compose_sessions_unconfirmed
  ON compose_sessions(created_at)
  WHERE confirmed_at IS NULL;

UPDATE schema_meta SET value = '5' WHERE key = 'version';
