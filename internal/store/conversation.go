package store

import (
	"context"
	"fmt"
)

// MessageIDsInConversation returns the IDs of all messages with the given
// conversationID for the account. By default it excludes messages in
// well-known Drafts, Deleted Items, and Junk folders. When includeAllFolders
// is true the folder exclusion is skipped — used by CLI.
func (s *store) MessageIDsInConversation(ctx context.Context, accountID int64, conversationID string, includeAllFolders bool) ([]string, error) {
	if conversationID == "" {
		return nil, nil
	}
	var q string
	if includeAllFolders {
		q = `SELECT m.id
FROM messages m
WHERE m.account_id = ?
  AND m.conversation_id = ?
ORDER BY m.received_at DESC`
	} else {
		q = `SELECT m.id
FROM messages m
JOIN folders f ON f.id = m.folder_id
WHERE m.account_id = ?
  AND m.conversation_id = ?
  AND (f.well_known_name IS NULL
       OR f.well_known_name NOT IN ('drafts', 'deleteditems', 'junkemail'))
ORDER BY m.received_at DESC`
	}
	rows, err := s.db.QueryContext(ctx, q, accountID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("MessageIDsInConversation: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("MessageIDsInConversation scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
