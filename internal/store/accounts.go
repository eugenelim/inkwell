package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// GetAccount returns the single v1 account, or [ErrNotFound].
func (s *store) GetAccount(ctx context.Context) (*Account, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, client_id, upn, COALESCE(display_name, ''), COALESCE(object_id, ''), COALESCE(last_signin, 0) FROM accounts ORDER BY id LIMIT 1`)
	var a Account
	var last int64
	if err := row.Scan(&a.ID, &a.TenantID, &a.ClientID, &a.UPN, &a.DisplayName, &a.ObjectID, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if last > 0 {
		a.LastSignin = time.Unix(last, 0)
	}
	return &a, nil
}

// PutAccount upserts by (tenant_id, upn) and returns the local id.
func (s *store) PutAccount(ctx context.Context, a Account) (int64, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO accounts (tenant_id, client_id, upn, display_name, object_id, last_signin)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, upn) DO UPDATE SET
			client_id = excluded.client_id,
			display_name = excluded.display_name,
			object_id = excluded.object_id,
			last_signin = excluded.last_signin`,
		a.TenantID, a.ClientID, a.UPN, nullStr(a.DisplayName), nullStr(a.ObjectID), nullTime(a.LastSignin),
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		// Conflict path returns 0 from LastInsertId on some drivers; look up.
		row := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE tenant_id = ? AND upn = ?`, a.TenantID, a.UPN)
		if err := row.Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}
