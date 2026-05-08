package store

import (
	"context"
	"fmt"
	"strings"
)

// Reserved category names for spec 25's inkwell-managed stacks. The
// `Inkwell/` prefix namespaces them away from user-defined categories
// in Outlook. The strings round-trip to Microsoft Graph and are
// visible in Outlook web as ordinary categories — intentional
// cross-device sync (documented in docs/user/explanation.md).
const (
	CategoryReplyLater = "Inkwell/ReplyLater"
	CategorySetAside   = "Inkwell/SetAside"
)

// IsInkwellCategory reports whether s is one of the reserved
// inkwell stack categories. Comparison is case-insensitive — a
// user who tagged `inkwell/replylater` in Outlook web is still
// recognised as the same stack here.
func IsInkwellCategory(s string) bool {
	return strings.EqualFold(s, CategoryReplyLater) ||
		strings.EqualFold(s, CategorySetAside)
}

// IsInCategory reports whether the message's Categories slice
// contains a case-insensitive match for cat. Single source of truth
// for membership checks in the toggle handler, the indicator
// renderer, the stack-view filter, and the focus-mode pre-fetch.
func IsInCategory(cats []string, cat string) bool {
	for _, c := range cats {
		if strings.EqualFold(c, cat) {
			return true
		}
	}
	return false
}

// CountMessagesInCategory returns the count of messages tagged with
// the given category for the account. Spec 25 §4.2.
//
// Excludes well-known folders Drafts, Deleted Items, and Junk
// (matches MessageIDsInConversation). Does NOT exclude muted
// threads — stack views are intentional, like search.
func (s *store) CountMessagesInCategory(ctx context.Context, accountID int64, category string) (int, error) {
	q := `SELECT COUNT(*)
	      FROM messages m
	      LEFT JOIN folders f ON f.id = m.folder_id
	      WHERE m.account_id = ?
	        AND EXISTS (
	            SELECT 1 FROM json_each(m.categories)
	            WHERE value = ? COLLATE NOCASE
	        )
	        AND (f.well_known_name IS NULL
	             OR f.well_known_name NOT IN ('drafts', 'deleteditems', 'junkemail'))`
	var n int
	if err := s.db.QueryRowContext(ctx, q, accountID, category).Scan(&n); err != nil {
		return 0, fmt.Errorf("CountMessagesInCategory: %w", err)
	}
	return n, nil
}

// ListMessagesInCategory returns messages tagged with the given
// category for the account, ordered by received_at DESC, capped at
// limit. Same exclusions as [CountMessagesInCategory].
func (s *store) ListMessagesInCategory(ctx context.Context, accountID int64, category string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT ` + messageColumns + `
	      FROM messages
	      WHERE account_id = ?
	        AND EXISTS (
	            SELECT 1 FROM json_each(messages.categories)
	            WHERE value = ? COLLATE NOCASE
	        )
	        AND folder_id NOT IN (
	            SELECT id FROM folders
	            WHERE well_known_name IN ('drafts', 'deleteditems', 'junkemail')
	        )
	      ORDER BY received_at DESC
	      LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, accountID, category, limit)
	if err != nil {
		return nil, fmt.Errorf("ListMessagesInCategory: %w", err)
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
