package store

import (
	"context"
	"database/sql"
	"errors"
)

// ListFolders returns folders for accountID ordered by display_name.
func (s *store) ListFolders(ctx context.Context, accountID int64) ([]Folder, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, account_id, COALESCE(parent_folder_id, ''), display_name, COALESCE(well_known_name, ''),
		       total_count, unread_count, is_hidden, COALESCE(last_synced_at, 0)
		FROM folders WHERE account_id = ? ORDER BY display_name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		var f Folder
		var hidden int
		var lastSync int64
		if err := rows.Scan(&f.ID, &f.AccountID, &f.ParentFolderID, &f.DisplayName, &f.WellKnownName,
			&f.TotalCount, &f.UnreadCount, &hidden, &lastSync); err != nil {
			return nil, err
		}
		f.IsHidden = hidden != 0
		f.LastSyncedAt = unixToTime(lastSync)
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFolderByWellKnown returns the folder with the matching well-known
// name (e.g. "inbox") or [ErrNotFound].
func (s *store) GetFolderByWellKnown(ctx context.Context, accountID int64, name string) (*Folder, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, account_id, COALESCE(parent_folder_id, ''), display_name, well_known_name,
		       total_count, unread_count, is_hidden, COALESCE(last_synced_at, 0)
		FROM folders WHERE account_id = ? AND well_known_name = ? LIMIT 1`, accountID, name)
	var f Folder
	var hidden int
	var lastSync int64
	if err := row.Scan(&f.ID, &f.AccountID, &f.ParentFolderID, &f.DisplayName, &f.WellKnownName,
		&f.TotalCount, &f.UnreadCount, &hidden, &lastSync); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	f.IsHidden = hidden != 0
	f.LastSyncedAt = unixToTime(lastSync)
	return &f, nil
}

// UpsertFolder writes (or replaces) a single folder row.
func (s *store) UpsertFolder(ctx context.Context, f Folder) error {
	hidden := 0
	if f.IsHidden {
		hidden = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO folders (id, account_id, parent_folder_id, display_name, well_known_name,
			total_count, unread_count, is_hidden, last_synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			account_id = excluded.account_id,
			parent_folder_id = excluded.parent_folder_id,
			display_name = excluded.display_name,
			well_known_name = excluded.well_known_name,
			total_count = excluded.total_count,
			unread_count = excluded.unread_count,
			is_hidden = excluded.is_hidden,
			last_synced_at = excluded.last_synced_at`,
		f.ID, f.AccountID, nullStr(f.ParentFolderID), f.DisplayName, nullStr(f.WellKnownName),
		f.TotalCount, f.UnreadCount, hidden, nullTime(f.LastSyncedAt),
	)
	return err
}

// DeleteFolder removes the folder and cascades to messages.
func (s *store) DeleteFolder(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM folders WHERE id = ?", id)
	return err
}

// UpdateFolderDisplayName mutates the display_name field on a single
// folder row. Used by the spec 18 rename path so the sidebar
// reflects the renamed folder before the next sync cycle. The
// folder ID stays the same — Graph keeps it across renames.
func (s *store) UpdateFolderDisplayName(ctx context.Context, id, displayName string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE folders SET display_name = ? WHERE id = ?",
		displayName, id)
	return err
}
