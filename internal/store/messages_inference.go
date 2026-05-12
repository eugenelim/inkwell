package store

import (
	"context"
	"errors"
	"fmt"
)

// Inference-classification value strings. These are the two non-empty
// values Microsoft Graph populates on `inferenceClassification`; the
// third state, an empty string / NULL, is the "never classified" case
// (drafts, sent items, tenants with Focused Inbox off) and is invisible
// to both sub-tabs (spec 31 §3.1).
const (
	InferenceClassFocused = "focused"
	InferenceClassOther   = "other"
)

// ErrInvalidInferenceClass is returned when callers pass any string
// other than "focused" or "other" to the inference-class store helpers.
// Defence in depth: callers normalise to the closed set, the store
// rejects bad inputs at the SQL boundary so a buggy caller can't
// silently produce a no-match query (spec 31 §4.1).
var ErrInvalidInferenceClass = errors.New("store: invalid inference class")

func validInferenceClass(cls string) bool {
	switch cls {
	case InferenceClassFocused, InferenceClassOther:
		return true
	}
	return false
}

// ListMessagesByInferenceClass returns messages in the given folder
// whose inference_class matches cls ("focused" or "other"), ordered by
// received_at DESC. limit caps the page size; pass <=0 for the default
// 100 (spec 02 page-size convention).
//
// excludeMuted applies the same anti-join shape as ListMessagesByRouting
// (spec 23 §4.2) — when true, muted threads are excluded.
//
// excludeScreenedOut applies spec 28 §5.4's default-view filter — when
// true, messages whose sender is routed to "screener" are excluded.
// The default Inbox sub-strip view passes
// excludeScreenedOut = cfg.Screener.Enabled so the sub-tab inherits
// the same default-view filter as the unsplit Inbox.
func (s *store) ListMessagesByInferenceClass(
	ctx context.Context, accountID int64, folderID, cls string,
	limit int, excludeMuted, excludeScreenedOut bool,
) ([]Message, error) {
	if !validInferenceClass(cls) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidInferenceClass, cls)
	}
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT ` + messageColumns + `
		  FROM messages
		  WHERE account_id      = ?
		    AND folder_id       = ?
		    AND inference_class = ?`
	if excludeMuted {
		q += `
		    AND (
		        conversation_id IS NULL
		        OR conversation_id = ''
		        OR NOT EXISTS (
		            SELECT 1 FROM muted_conversations mc
		            WHERE mc.conversation_id = messages.conversation_id
		              AND mc.account_id      = messages.account_id
		        )
		    )`
	}
	if excludeScreenedOut {
		q += `
		    AND NOT EXISTS (
		        SELECT 1 FROM sender_routing sr
		        WHERE sr.account_id    = messages.account_id
		          AND sr.email_address = lower(trim(messages.from_address))
		          AND sr.destination   = 'screener'
		    )`
	}
	q += `
		  ORDER BY received_at DESC
		  LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, accountID, folderID, cls, limit)
	if err != nil {
		return nil, fmt.Errorf("ListMessagesByInferenceClass: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// CountUnreadByInferenceClass returns the unread message count for the
// (folder, inference_class) pair. excludeMuted and excludeScreenedOut
// follow the same semantics as ListMessagesByInferenceClass. Used for
// the sub-strip badges (spec 31 §5.3).
func (s *store) CountUnreadByInferenceClass(
	ctx context.Context, accountID int64, folderID, cls string,
	excludeMuted, excludeScreenedOut bool,
) (int, error) {
	if !validInferenceClass(cls) {
		return 0, fmt.Errorf("%w: %q", ErrInvalidInferenceClass, cls)
	}
	q := `SELECT COUNT(*)
		  FROM messages
		  WHERE account_id      = ?
		    AND folder_id       = ?
		    AND inference_class = ?
		    AND is_read         = 0`
	if excludeMuted {
		q += `
		    AND (
		        conversation_id IS NULL
		        OR conversation_id = ''
		        OR NOT EXISTS (
		            SELECT 1 FROM muted_conversations mc
		            WHERE mc.conversation_id = messages.conversation_id
		              AND mc.account_id      = messages.account_id
		        )
		    )`
	}
	if excludeScreenedOut {
		q += `
		    AND NOT EXISTS (
		        SELECT 1 FROM sender_routing sr
		        WHERE sr.account_id    = messages.account_id
		          AND sr.email_address = lower(trim(messages.from_address))
		          AND sr.destination   = 'screener'
		    )`
	}
	var n int
	if err := s.db.QueryRowContext(ctx, q, accountID, folderID, cls).Scan(&n); err != nil {
		return 0, fmt.Errorf("CountUnreadByInferenceClass: %w", err)
	}
	return n, nil
}
