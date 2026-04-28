package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// EnqueueAction inserts a new action with status 'pending'.
func (s *store) EnqueueAction(ctx context.Context, a Action) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	if a.Status == "" {
		a.Status = StatusPending
	}
	ids, _ := json.Marshal(a.MessageIDs)
	params, _ := json.Marshal(a.Params)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO actions (id, account_id, type, message_ids, params, status, failure_reason, created_at, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.AccountID, string(a.Type), string(ids), string(params),
		string(a.Status), nullStr(a.FailureReason),
		a.CreatedAt.Unix(), nullTime(a.StartedAt), nullTime(a.CompletedAt))
	return err
}

// PendingActions returns actions in 'pending' or 'in_flight' state, oldest first.
func (s *store) PendingActions(ctx context.Context) ([]Action, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, account_id, type, message_ids, COALESCE(params, '{}'), status,
		       COALESCE(failure_reason, ''), created_at, COALESCE(started_at, 0), COALESCE(completed_at, 0)
		FROM actions WHERE status IN ('pending', 'in_flight') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		var a Action
		var typeStr, statusStr, idsJSON, paramsJSON string
		var created, started, completed int64
		if err := rows.Scan(&a.ID, &a.AccountID, &typeStr, &idsJSON, &paramsJSON, &statusStr,
			&a.FailureReason, &created, &started, &completed); err != nil {
			return nil, err
		}
		a.Type = ActionType(typeStr)
		a.Status = ActionStatus(statusStr)
		_ = json.Unmarshal([]byte(idsJSON), &a.MessageIDs)
		_ = json.Unmarshal([]byte(paramsJSON), &a.Params)
		a.CreatedAt = time.Unix(created, 0)
		a.StartedAt = unixToTime(started)
		a.CompletedAt = unixToTime(completed)
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateActionStatus moves an action through its lifecycle.
func (s *store) UpdateActionStatus(ctx context.Context, id string, status ActionStatus, reason string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now()
	switch status {
	case StatusInFlight:
		_, err = tx.ExecContext(ctx, "UPDATE actions SET status = ?, started_at = ?, failure_reason = NULL WHERE id = ?", string(status), now.Unix(), id)
	case StatusDone:
		_, err = tx.ExecContext(ctx, "UPDATE actions SET status = ?, completed_at = ?, failure_reason = NULL WHERE id = ?", string(status), now.Unix(), id)
	case StatusFailed:
		_, err = tx.ExecContext(ctx, "UPDATE actions SET status = ?, completed_at = ?, failure_reason = ? WHERE id = ?", string(status), now.Unix(), nullStr(reason), id)
	default:
		_, err = tx.ExecContext(ctx, "UPDATE actions SET status = ? WHERE id = ?", string(status), id)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
