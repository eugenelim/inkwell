package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ListSavedSearches returns saved searches for accountID, pinned-first then sort_order.
func (s *store) ListSavedSearches(ctx context.Context, accountID int64) ([]SavedSearch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, account_id, name, pattern, pinned, sort_order, tab_order, created_at
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
		var tabOrder sql.NullInt64
		var created int64
		if err := rows.Scan(&ss.ID, &ss.AccountID, &ss.Name, &ss.Pattern, &pinned, &ss.SortOrder, &tabOrder, &created); err != nil {
			return nil, err
		}
		ss.Pinned = pinned != 0
		if tabOrder.Valid {
			n := int(tabOrder.Int64)
			ss.TabOrder = &n
		}
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

// ListTabs returns saved searches promoted to the spec 24 tab strip
// (rows with non-NULL tab_order), ordered ascending by tab_order.
func (s *store) ListTabs(ctx context.Context, accountID int64) ([]SavedSearch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, account_id, name, pattern, pinned, sort_order, tab_order, created_at
		FROM saved_searches
		WHERE account_id = ? AND tab_order IS NOT NULL
		ORDER BY tab_order ASC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedSearch
	for rows.Next() {
		var ss SavedSearch
		var pinned int
		var tabOrder sql.NullInt64
		var created int64
		if err := rows.Scan(&ss.ID, &ss.AccountID, &ss.Name, &ss.Pattern, &pinned, &ss.SortOrder, &tabOrder, &created); err != nil {
			return nil, err
		}
		ss.Pinned = pinned != 0
		if tabOrder.Valid {
			n := int(tabOrder.Int64)
			ss.TabOrder = &n
		}
		ss.CreatedAt = time.Unix(created, 0)
		out = append(out, ss)
	}
	return out, rows.Err()
}

// SetTabOrder writes tab_order for one saved search. Pass nil to
// demote (clear tab status). Idempotent. Does not renumber siblings —
// callers writing multi-row mutations use [ApplyTabOrder] instead.
func (s *store) SetTabOrder(ctx context.Context, id int64, order *int) error {
	if order == nil {
		_, err := s.db.ExecContext(ctx,
			`UPDATE saved_searches SET tab_order = NULL WHERE id = ?`, id)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE saved_searches SET tab_order = ? WHERE id = ?`, *order, id)
	return err
}

// ReindexTabs renumbers tab_order for the given account so values are
// dense (0..N-1) preserving relative order. Two-pass NULL-then-renumber
// inside one transaction so the partial UNIQUE index in migration 012
// is satisfied at every visible state. Called by Manager after
// add/remove/reorder/delete mutations.
func (s *store) ReindexTabs(ctx context.Context, accountID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ReindexTabs: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM saved_searches
		WHERE account_id = ? AND tab_order IS NOT NULL
		ORDER BY tab_order ASC, id ASC`, accountID)
	if err != nil {
		return fmt.Errorf("ReindexTabs: select: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("ReindexTabs: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ReindexTabs: rows: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE saved_searches SET tab_order = NULL
		 WHERE account_id = ? AND tab_order IS NOT NULL`, accountID); err != nil {
		return fmt.Errorf("ReindexTabs: clear: %w", err)
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE saved_searches SET tab_order = ? WHERE id = ?`, i, id); err != nil {
			return fmt.Errorf("ReindexTabs: renumber: %w", err)
		}
	}
	return tx.Commit()
}

// ApplyTabOrder writes a full ordered slice of saved-search IDs as the
// account's new tab strip in one transaction. Each ID present is set
// to its index in the slice; any tabbed row whose ID is absent from
// the slice is demoted (tab_order = NULL). Two-pass NULL-then-renumber
// keeps the partial UNIQUE index satisfied throughout. Spec 24 §3.4.
func (s *store) ApplyTabOrder(ctx context.Context, accountID int64, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ApplyTabOrder: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Step 1: clear all tab_order values for the account.
	if _, err := tx.ExecContext(ctx,
		`UPDATE saved_searches SET tab_order = NULL
		 WHERE account_id = ? AND tab_order IS NOT NULL`, accountID); err != nil {
		return fmt.Errorf("ApplyTabOrder: clear: %w", err)
	}

	// Step 2: write the new dense ordering. Each row must belong to
	// the supplied account; a mismatched id is rejected so a caller
	// passing the wrong account doesn't promote someone else's row.
	for i, id := range ids {
		res, err := tx.ExecContext(ctx,
			`UPDATE saved_searches SET tab_order = ?
			 WHERE id = ? AND account_id = ?`, i, id, accountID)
		if err != nil {
			return fmt.Errorf("ApplyTabOrder: assign: %w", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("ApplyTabOrder: id %d not found for account %d", id, accountID)
		}
	}
	return tx.Commit()
}

// CountUnreadByIDs returns the count of unread messages whose id is
// in the supplied set, scoped to accountID. Spec 24 §4: used by
// Manager.CountTabs to compute per-tab unread badges without
// re-running the user's pattern with an `~U` rider (which would
// semantically drift if the pattern itself references read state).
func (s *store) CountUnreadByIDs(ctx context.Context, accountID int64, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// Chunk into batches of 500 — SQLITE_MAX_VARIABLE_NUMBER defaults
	// to 999 in modernc/sqlite; 500 leaves headroom for the two
	// fixed parameters (accountID, plus per-id placeholders).
	const chunk = 500
	total := 0
	for i := 0; i < len(ids); i += chunk {
		end := i + chunk
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		placeholders := strings.Repeat("?,", len(batch))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, 0, len(batch)+1)
		args = append(args, accountID)
		for _, id := range batch {
			args = append(args, id)
		}
		var n int
		err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM messages
			 WHERE account_id = ? AND is_read = 0 AND id IN (`+placeholders+`)`,
			args...).Scan(&n)
		if err != nil {
			return 0, fmt.Errorf("CountUnreadByIDs: %w", err)
		}
		total += n
	}
	return total, nil
}
