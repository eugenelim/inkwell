package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// PushUndo appends an inverse-action descriptor to the session stack.
func (s *store) PushUndo(ctx context.Context, e UndoEntry) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	ids, _ := json.Marshal(e.MessageIDs)
	params, _ := json.Marshal(e.Params)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO undo (action_type, message_ids, params, label, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		string(e.ActionType), string(ids), string(params), e.Label, e.CreatedAt.Unix())
	return err
}

// PopUndo removes and returns the most recent entry, or [ErrNotFound].
func (s *store) PopUndo(ctx context.Context) (*UndoEntry, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		SELECT id, action_type, message_ids, COALESCE(params, '{}'), label, created_at
		FROM undo ORDER BY id DESC LIMIT 1`)
	e, err := scanUndo(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM undo WHERE id = ?", e.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return e, nil
}

// PeekUndo returns the most recent entry without removing it.
func (s *store) PeekUndo(ctx context.Context) (*UndoEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, action_type, message_ids, COALESCE(params, '{}'), label, created_at
		FROM undo ORDER BY id DESC LIMIT 1`)
	e, err := scanUndo(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return e, nil
}

// ClearUndo empties the stack. Called on app start.
func (s *store) ClearUndo(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM undo")
	return err
}

func scanUndo(row rowScanner) (*UndoEntry, error) {
	var e UndoEntry
	var typeStr, idsJSON, paramsJSON string
	var createdAt int64
	if err := row.Scan(&e.ID, &typeStr, &idsJSON, &paramsJSON, &e.Label, &createdAt); err != nil {
		return nil, err
	}
	e.ActionType = ActionType(typeStr)
	_ = json.Unmarshal([]byte(idsJSON), &e.MessageIDs)
	_ = json.Unmarshal([]byte(paramsJSON), &e.Params)
	e.CreatedAt = time.Unix(createdAt, 0)
	return &e, nil
}
