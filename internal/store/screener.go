package store

import (
	"context"
	"fmt"
)

// ListPendingSenders returns one row per pending sender — i.e.,
// senders with at least one message in the local store and no
// sender_routing row. Each row carries the most recent message's
// envelope so the UI can render a representative subject /
// received_at. Used by the Screener virtual folder when
// [screener].grouping = "sender".
//
// Result rows are ordered by newest received_at DESC, with
// address ASC as the deterministic tie-break (spec 28 §4.4).
//
// capPerSender bounds the per-sender MessageCount via a SQL-side
// LIMIT inside the correlated subquery so a single noisy sender
// (e.g. 50k newsletter messages) does not dominate the budget. A
// caller-side cap of 999 means counts >= 1000 surface as "999+";
// the actual returned MessageCount equals min(real, cap+1) so the
// Go layer can detect saturation. capPerSender <= 0 falls back to
// 999.
func (s *store) ListPendingSenders(ctx context.Context, accountID int64, limit, capPerSender int, excludeMuted bool) ([]PendingSender, error) {
	if limit <= 0 {
		limit = 200
	}
	if capPerSender <= 0 {
		capPerSender = 999
	}
	capPlusOne := capPerSender + 1

	mutedClause := ""
	if excludeMuted {
		mutedClause = `
			  AND (
			      m.conversation_id IS NULL
			      OR m.conversation_id = ''
			      OR NOT EXISTS (
			          SELECT 1 FROM muted_conversations mc
			          WHERE mc.conversation_id = m.conversation_id
			            AND mc.account_id = m.account_id
			      )
			  )`
	}
	mutedCountClause := ""
	if excludeMuted {
		mutedCountClause = `
			      AND (
			          m2.conversation_id IS NULL
			          OR m2.conversation_id = ''
			          OR NOT EXISTS (
			              SELECT 1 FROM muted_conversations mc
			              WHERE mc.conversation_id = m2.conversation_id
			                AND mc.account_id = m2.account_id
			          )
			      )`
	}

	q := fmt.Sprintf(`
		WITH ranked AS (
		    SELECT
		        lower(trim(m.from_address)) AS address,
		        m.from_name      AS display_name,
		        m.subject        AS subject,
		        m.received_at    AS received_at,
		        m.id             AS message_id,
		        ROW_NUMBER() OVER (
		            PARTITION BY lower(trim(m.from_address))
		            ORDER BY m.received_at DESC, m.id ASC
		        ) AS rn
		    FROM messages m
		    WHERE m.account_id = ?
		      AND m.from_address IS NOT NULL
		      AND m.from_address != ''
		      AND NOT EXISTS (
		          SELECT 1 FROM sender_routing sr
		          WHERE sr.account_id    = m.account_id
		            AND sr.email_address = lower(trim(m.from_address))
		      )%s
		)
		SELECT r.address, COALESCE(r.display_name, ''), COALESCE(r.subject, ''),
		       r.received_at, r.message_id,
		       (SELECT COUNT(*) FROM (
		            SELECT 1 FROM messages m2
		            WHERE m2.account_id = ?
		              AND lower(trim(m2.from_address)) = r.address%s
		            LIMIT ?
		       )) AS message_count
		FROM ranked r
		WHERE r.rn = 1
		ORDER BY r.received_at DESC, r.address ASC
		LIMIT ?
	`, mutedClause, mutedCountClause)

	rows, err := s.db.QueryContext(ctx, q, accountID, accountID, capPlusOne, limit)
	if err != nil {
		return nil, fmt.Errorf("ListPendingSenders: %w", err)
	}
	defer rows.Close()
	var out []PendingSender
	for rows.Next() {
		var ps PendingSender
		var receivedAt int64
		if err := rows.Scan(&ps.EmailAddress, &ps.DisplayName, &ps.LatestSubject,
			&receivedAt, &ps.LatestMessageID, &ps.MessageCount); err != nil {
			return nil, err
		}
		ps.LatestReceived = unixToTime(receivedAt)
		out = append(out, ps)
	}
	return out, rows.Err()
}

// ListPendingMessages returns the raw message rows from pending
// senders, one per message, ordered by received_at DESC. Equivalent
// to calling ListMessages with a "~o none" pattern, but specialised
// for performance. Excludes rows where from_address is NULL or
// empty (no actionable sender).
func (s *store) ListPendingMessages(ctx context.Context, accountID int64, limit int, excludeMuted bool) ([]Message, error) {
	if limit <= 0 {
		limit = 200
	}
	q := `SELECT ` + messageColumns + `
		  FROM messages
		  WHERE account_id = ?
		    AND from_address IS NOT NULL
		    AND length(trim(from_address)) > 0
		    AND NOT EXISTS (
		        SELECT 1 FROM sender_routing sr
		        WHERE sr.account_id    = messages.account_id
		          AND sr.email_address = lower(trim(from_address))
		    )`
	if excludeMuted {
		q += `
		    AND (
		        messages.conversation_id IS NULL
		        OR messages.conversation_id = ''
		        OR NOT EXISTS (
		            SELECT 1 FROM muted_conversations mc
		            WHERE mc.conversation_id = messages.conversation_id
		              AND mc.account_id = messages.account_id
		        )
		    )`
	}
	q += `
		  ORDER BY received_at DESC, id ASC
		  LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("ListPendingMessages: %w", err)
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

// ListScreenedOutMessages returns messages whose sender is routed
// to 'screener'. Mirror of ListMessagesByRouting('screener') with
// a fixed destination.
func (s *store) ListScreenedOutMessages(ctx context.Context, accountID int64, limit int, excludeMuted bool) ([]Message, error) {
	return s.ListMessagesByRouting(ctx, accountID, RoutingScreener, limit, excludeMuted)
}

// CountPendingSenders returns the count of distinct pending sender
// addresses for the account. Used by the sidebar Screener badge
// when the gate is enabled.
func (s *store) CountPendingSenders(ctx context.Context, accountID int64, excludeMuted bool) (int, error) {
	q := `SELECT COUNT(DISTINCT lower(trim(from_address)))
		  FROM messages
		  WHERE account_id = ?
		    AND from_address IS NOT NULL
		    AND length(trim(from_address)) > 0
		    AND NOT EXISTS (
		        SELECT 1 FROM sender_routing sr
		        WHERE sr.account_id    = messages.account_id
		          AND sr.email_address = lower(trim(from_address))
		    )`
	if excludeMuted {
		q += `
		    AND (
		        messages.conversation_id IS NULL
		        OR messages.conversation_id = ''
		        OR NOT EXISTS (
		            SELECT 1 FROM muted_conversations mc
		            WHERE mc.conversation_id = messages.conversation_id
		              AND mc.account_id = messages.account_id
		        )
		    )`
	}
	var n int
	err := s.db.QueryRowContext(ctx, q, accountID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("CountPendingSenders: %w", err)
	}
	return n, nil
}

// CountScreenedOutMessages mirrors CountMessagesByRouting('screener')
// kept as a named method for symmetry with the other §4.3 methods.
func (s *store) CountScreenedOutMessages(ctx context.Context, accountID int64, excludeMuted bool) (int, error) {
	return s.CountMessagesByRouting(ctx, accountID, RoutingScreener, excludeMuted)
}

// CountMessagesFromPendingSenders returns the total message count
// (not distinct senders) from pending senders. Used only by the
// gate-flip confirmation modal (spec 28 §5.3.1).
func (s *store) CountMessagesFromPendingSenders(ctx context.Context, accountID int64, excludeMuted bool) (int, error) {
	q := `SELECT COUNT(*)
		  FROM messages
		  WHERE account_id = ?
		    AND from_address IS NOT NULL
		    AND length(trim(from_address)) > 0
		    AND NOT EXISTS (
		        SELECT 1 FROM sender_routing sr
		        WHERE sr.account_id    = messages.account_id
		          AND sr.email_address = lower(trim(from_address))
		    )`
	if excludeMuted {
		q += `
		    AND (
		        messages.conversation_id IS NULL
		        OR messages.conversation_id = ''
		        OR NOT EXISTS (
		            SELECT 1 FROM muted_conversations mc
		            WHERE mc.conversation_id = messages.conversation_id
		              AND mc.account_id = messages.account_id
		        )
		    )`
	}
	var n int
	err := s.db.QueryRowContext(ctx, q, accountID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("CountMessagesFromPendingSenders: %w", err)
	}
	return n, nil
}
