package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// MuteConversation records a muted conversation. Idempotent: if the row
// already exists the muted_at timestamp is left unchanged.
func (s *store) MuteConversation(ctx context.Context, accountID int64, conversationID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO muted_conversations (conversation_id, account_id, muted_at)
		 VALUES (?, ?, ?)`,
		conversationID, accountID, time.Now().Unix())
	return err
}

// UnmuteConversation removes a muted conversation row. No-op if not muted.
func (s *store) UnmuteConversation(ctx context.Context, accountID int64, conversationID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM muted_conversations WHERE conversation_id = ? AND account_id = ?`,
		conversationID, accountID)
	return err
}

// IsConversationMuted returns true when conversationID is muted for accountID.
func (s *store) IsConversationMuted(ctx context.Context, accountID int64, conversationID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM muted_conversations WHERE conversation_id = ? AND account_id = ?`,
		conversationID, accountID).Scan(&n)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return n > 0, nil
}

// ListMutedMessages returns all messages whose conversation_id is muted for
// accountID, ordered by muted_at DESC then received_at DESC within each
// conversation. Used by the "Muted Threads" virtual folder view.
//
// Uses a scalar subquery for the ORDER BY instead of a JOIN to avoid
// ambiguous column names with the messageColumns constant which doesn't
// carry table aliases.
func (s *store) ListMutedMessages(ctx context.Context, accountID int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT ` + messageColumns + `
		  FROM messages
		  WHERE account_id = ?
		    AND conversation_id IN (
		        SELECT conversation_id FROM muted_conversations WHERE account_id = ?
		    )
		  ORDER BY (
		      SELECT muted_at FROM muted_conversations mc
		      WHERE mc.conversation_id = messages.conversation_id
		        AND mc.account_id = ?
		  ) DESC, received_at DESC
		  LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, accountID, accountID, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *msg)
	}
	return out, rows.Err()
}

// CountMutedConversations returns the number of distinct muted conversations
// for accountID. Used by the sidebar to decide whether to render the virtual
// "Muted Threads" entry and its count badge.
func (s *store) CountMutedConversations(ctx context.Context, accountID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM muted_conversations WHERE account_id = ?`,
		accountID).Scan(&n)
	return n, err
}
