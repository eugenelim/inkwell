package store

import (
	"context"
	"database/sql"
	"errors"
)

// GetDeltaToken returns the per-folder cursor or [ErrNotFound].
func (s *store) GetDeltaToken(ctx context.Context, accountID int64, folderID string) (*DeltaToken, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT account_id, folder_id, COALESCE(delta_link, ''), COALESCE(next_link, ''),
		       COALESCE(last_full_sync, 0), COALESCE(last_delta_at, 0)
		FROM delta_tokens WHERE account_id = ? AND folder_id = ?`, accountID, folderID)
	var t DeltaToken
	var lastFull, lastDelta int64
	if err := row.Scan(&t.AccountID, &t.FolderID, &t.DeltaLink, &t.NextLink, &lastFull, &lastDelta); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.LastFullSync = unixToTime(lastFull)
	t.LastDeltaAt = unixToTime(lastDelta)
	return &t, nil
}

// PutDeltaToken upserts the per-folder cursor.
func (s *store) PutDeltaToken(ctx context.Context, t DeltaToken) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO delta_tokens (account_id, folder_id, delta_link, next_link, last_full_sync, last_delta_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, folder_id) DO UPDATE SET
			delta_link = excluded.delta_link,
			next_link = excluded.next_link,
			last_full_sync = excluded.last_full_sync,
			last_delta_at = excluded.last_delta_at`,
		t.AccountID, t.FolderID, nullStr(t.DeltaLink), nullStr(t.NextLink),
		nullTime(t.LastFullSync), nullTime(t.LastDeltaAt))
	return err
}

// ClearDeltaToken removes the per-folder cursor (called on syncStateNotFound).
func (s *store) ClearDeltaToken(ctx context.Context, accountID int64, folderID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM delta_tokens WHERE account_id = ? AND folder_id = ?", accountID, folderID)
	return err
}
