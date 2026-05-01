package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PutComposeSession upserts a compose session row. The first call
// for a given SessionID inserts; subsequent calls update the
// snapshot blob and bump updated_at. Idempotent — the spec 15
// resume path may replay a row on launch and we don't want a
// duplicate-key error.
func (s *store) PutComposeSession(ctx context.Context, sess ComposeSession) error {
	if sess.SessionID == "" {
		return errors.New("store: compose session needs SessionID")
	}
	now := time.Now()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	if sess.UpdatedAt.IsZero() {
		sess.UpdatedAt = now
	}
	confirmedAt := sql.NullInt64{}
	if !sess.ConfirmedAt.IsZero() {
		confirmedAt = sql.NullInt64{Int64: sess.ConfirmedAt.Unix(), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO compose_sessions (
			session_id, kind, source_id, snapshot,
			created_at, updated_at, confirmed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			kind = excluded.kind,
			source_id = excluded.source_id,
			snapshot = excluded.snapshot,
			updated_at = excluded.updated_at,
			confirmed_at = excluded.confirmed_at`,
		sess.SessionID, sess.Kind, nullStr(sess.SourceID), sess.Snapshot,
		sess.CreatedAt.Unix(), sess.UpdatedAt.Unix(), confirmedAt,
	)
	return err
}

// ConfirmComposeSession stamps confirmed_at on the named row. Called
// when the user saves (Ctrl+S / Esc) or discards (Ctrl+D) a draft.
// No-op if the row doesn't exist (caller may double-confirm if a
// resume + immediate save races; idempotency keeps that safe).
func (s *store) ConfirmComposeSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return errors.New("store: ConfirmComposeSession needs sessionID")
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE compose_sessions SET confirmed_at = ? WHERE session_id = ?",
		time.Now().Unix(), sessionID)
	return err
}

// ListUnconfirmedComposeSessions returns every session whose
// confirmed_at is NULL, newest first. The launch-time resume scan
// uses this to offer the user the most-recently-crashed draft. In
// practice there's at most one row (the user can't be in two
// compose sessions at once), but the API takes the safer approach
// of returning a slice so a future multi-window mode doesn't need
// a contract break.
func (s *store) ListUnconfirmedComposeSessions(ctx context.Context) ([]ComposeSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, kind, COALESCE(source_id, ''), snapshot,
		       created_at, updated_at, confirmed_at
		FROM compose_sessions
		WHERE confirmed_at IS NULL
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ComposeSession
	for rows.Next() {
		var sess ComposeSession
		var createdAt, updatedAt int64
		var confirmedAt sql.NullInt64
		if err := rows.Scan(
			&sess.SessionID, &sess.Kind, &sess.SourceID, &sess.Snapshot,
			&createdAt, &updatedAt, &confirmedAt,
		); err != nil {
			return nil, err
		}
		sess.CreatedAt = time.Unix(createdAt, 0)
		sess.UpdatedAt = time.Unix(updatedAt, 0)
		if confirmedAt.Valid {
			sess.ConfirmedAt = time.Unix(confirmedAt.Int64, 0)
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// GCConfirmedComposeSessions deletes confirmed sessions whose
// confirmed_at is before `before`. Returns rowsAffected for
// telemetry. Spec 15 §7: confirmed sessions linger for ~24h so
// debugging can correlate "user pressed save" with "draft appeared
// in Drafts folder", then get GC'd to keep the table small.
func (s *store) GCConfirmedComposeSessions(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM compose_sessions WHERE confirmed_at IS NOT NULL AND confirmed_at < ?",
		before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
