package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ErrFolderNotFound is returned by [GetFolderByPath] when a path
// segment fails to resolve. Distinct from [ErrNotFound] so callers
// (notably the rules apply pipeline) can surface a folder-specific
// toast (spec 32 §6.5 step 3).
var ErrFolderNotFound = errors.New("store: folder not found")

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

// GetFolderByPath walks the cached folder tree by display_name, one
// segment at a time, separated by `/`. Names are NFC-normalised
// before comparison (macOS HFS+/APFS can return NFD; Graph returns
// NFC). The match on each level is case-sensitive. Returns
// [ErrFolderNotFound] when any segment fails to resolve, or the
// supplied path is empty. Comparison uses the cached `folders` rows
// only; this helper does not call Graph (spec 32 §6.5 step 3).
func (s *store) GetFolderByPath(ctx context.Context, accountID int64, slashPath string) (*Folder, error) {
	slashPath = strings.TrimSpace(slashPath)
	if slashPath == "" {
		return nil, ErrFolderNotFound
	}
	segments := splitFolderPath(slashPath)
	if len(segments) == 0 {
		return nil, ErrFolderNotFound
	}
	folders, err := s.ListFolders(ctx, accountID)
	if err != nil {
		return nil, err
	}
	parent := ""
	var match *Folder
	for _, seg := range segments {
		want := norm.NFC.String(seg)
		match = nil
		for i := range folders {
			f := &folders[i]
			if f.ParentFolderID != parent {
				continue
			}
			if norm.NFC.String(f.DisplayName) == want {
				match = f
				break
			}
		}
		if match == nil {
			return nil, ErrFolderNotFound
		}
		parent = match.ID
	}
	// Return a copy so callers can't mutate our slice header.
	out := *match
	return &out, nil
}

// splitFolderPath splits a `/`-separated folder path, discarding
// empty segments produced by leading / trailing / repeated slashes
// (`/Folders/A/` → ["Folders", "A"]).
func splitFolderPath(p string) []string {
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
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

// AdjustFolderCounts applies signed deltas to folders.total_count
// and folders.unread_count atomically. Counts are clamped at 0 so
// optimistic decrements can't drive the row negative when the
// pre-state was already stale (e.g. sync hadn't run yet).
//
// No-op for unknown folder IDs — the underlying UPDATE matches 0
// rows and returns no error. Callers can fire this for both
// source and destination folders without checking which exist
// locally.
//
// Eventually-consistent contract: every sync cycle's
// `syncFolders` rewrites total_count / unread_count from Graph's
// authoritative values, so any drift between optimistic and
// server state heals on the next cycle (~30s in foreground
// mode).
func (s *store) AdjustFolderCounts(ctx context.Context, folderID string, totalDelta, unreadDelta int) error {
	if folderID == "" || (totalDelta == 0 && unreadDelta == 0) {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE folders
		SET total_count = MAX(0, total_count + ?),
		    unread_count = MAX(0, unread_count + ?)
		WHERE id = ?`,
		totalDelta, unreadDelta, folderID)
	return err
}
