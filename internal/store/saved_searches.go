package store

import (
	"context"
	"time"
)

// ListSavedSearches returns saved searches for accountID, pinned-first then sort_order.
func (s *store) ListSavedSearches(ctx context.Context, accountID int64) ([]SavedSearch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, account_id, name, pattern, pinned, sort_order, created_at
		FROM saved_searches WHERE account_id = ?
		ORDER BY pinned DESC, sort_order ASC, name ASC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedSearch
	for rows.Next() {
		var ss SavedSearch
		var pinned int
		var created int64
		if err := rows.Scan(&ss.ID, &ss.AccountID, &ss.Name, &ss.Pattern, &pinned, &ss.SortOrder, &created); err != nil {
			return nil, err
		}
		ss.Pinned = pinned != 0
		ss.CreatedAt = time.Unix(created, 0)
		out = append(out, ss)
	}
	return out, rows.Err()
}

// PutSavedSearch upserts by (account_id, name).
func (s *store) PutSavedSearch(ctx context.Context, ss SavedSearch) error {
	if ss.CreatedAt.IsZero() {
		ss.CreatedAt = time.Now()
	}
	pinned := 0
	if ss.Pinned {
		pinned = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO saved_searches (account_id, name, pattern, pinned, sort_order, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, name) DO UPDATE SET
			pattern = excluded.pattern,
			pinned = excluded.pinned,
			sort_order = excluded.sort_order`,
		ss.AccountID, ss.Name, ss.Pattern, pinned, ss.SortOrder, ss.CreatedAt.Unix())
	return err
}

// DeleteSavedSearch removes by id.
func (s *store) DeleteSavedSearch(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM saved_searches WHERE id = ?", id)
	return err
}

// DeleteSavedSearchByName removes the saved search matching (accountID, name)
// in a single atomic DELETE. Returns nil if no matching row existed.
func (s *store) DeleteSavedSearchByName(ctx context.Context, accountID int64, name string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM saved_searches WHERE account_id = ? AND name = ?", accountID, name)
	return err
}
