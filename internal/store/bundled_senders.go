package store

import (
	"context"
	"strings"
	"time"
)

// AddBundledSender inserts a bundled_senders row. Idempotent
// (INSERT OR IGNORE). The store lowercases the address as
// defense-in-depth; callers (UI, CLI) lowercase too.
func (s *store) AddBundledSender(ctx context.Context, accountID int64, address string) error {
	addr := strings.ToLower(strings.TrimSpace(address))
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO bundled_senders (account_id, address, added_at)
		 VALUES (?, ?, ?)`,
		accountID, addr, time.Now().Unix())
	return err
}

// RemoveBundledSender deletes a bundled_senders row. No-op when not
// present (idempotent). Lowercases the address as defense-in-depth.
func (s *store) RemoveBundledSender(ctx context.Context, accountID int64, address string) error {
	addr := strings.ToLower(strings.TrimSpace(address))
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM bundled_senders WHERE account_id = ? AND address = ?`,
		accountID, addr)
	return err
}

// ListBundledSenders returns all bundled-sender rows for accountID
// ordered by added_at DESC then address ASC. Used by the UI to build
// the in-memory bundle set after sign-in and on Ctrl+R refresh.
func (s *store) ListBundledSenders(ctx context.Context, accountID int64) ([]BundledSender, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT account_id, address, added_at FROM bundled_senders
		 WHERE account_id = ?
		 ORDER BY added_at DESC, address ASC`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []BundledSender
	for rows.Next() {
		var b BundledSender
		var added int64
		if err := rows.Scan(&b.AccountID, &b.Address, &added); err != nil {
			return nil, err
		}
		b.AddedAt = time.Unix(added, 0)
		out = append(out, b)
	}
	return out, rows.Err()
}

// IsSenderBundled returns true when (accountID, address) has a row.
// Lowercases the address before SELECT. Hot-path keypress dispatch
// MUST use the in-memory set on the UI Model instead — this method
// is for CLI / tests / reconciliation paths.
func (s *store) IsSenderBundled(ctx context.Context, accountID int64, address string) (bool, error) {
	addr := strings.ToLower(strings.TrimSpace(address))
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM bundled_senders WHERE account_id = ? AND address = ?`,
		accountID, addr).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
