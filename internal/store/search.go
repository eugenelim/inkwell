package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Search runs an FTS5 query against messages_fts and returns hits in
// rank order (lower bm25 = better). The Query string is passed through
// to FTS5 so callers can use FTS5 operators (AND, OR, NEAR, prefix*).
// Empty Query returns no results.
func (s *store) Search(ctx context.Context, q SearchQuery) ([]MessageMatch, error) {
	if strings.TrimSpace(q.Query) == "" {
		return nil, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	args := []any{q.Query}
	stmt := `
		SELECT ` + messageColumnsPrefixed("m") + `, bm25(messages_fts) AS rank
		FROM messages_fts JOIN messages m ON m.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ?`
	if q.AccountID != 0 {
		stmt += " AND m.account_id = ?"
		args = append(args, q.AccountID)
	}
	if q.FolderID != "" {
		stmt += " AND m.folder_id = ?"
		args = append(args, q.FolderID)
	}
	stmt += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MessageMatch
	for rows.Next() {
		var (
			m                                                          Message
			toJSON, ccJSON, bccJSON, catsJSON                          string
			recvAt, sentAt, flagDueAt, flagCompAt, lastModAt, cachedAt int64
			isRead, isDraft, hasAtt                                    int
			rank                                                       float64
		)
		if err := rows.Scan(
			&m.ID, &m.AccountID, &m.FolderID,
			&m.InternetMessageID, &m.ConversationID, &m.ConversationIndex,
			&m.Subject, &m.BodyPreview, &m.FromAddress, &m.FromName,
			&toJSON, &ccJSON, &bccJSON,
			&recvAt, &sentAt, &isRead, &isDraft,
			&m.FlagStatus, &flagDueAt, &flagCompAt,
			&m.Importance, &m.InferenceClass, &hasAtt,
			&catsJSON, &m.WebLink, &lastModAt,
			&cachedAt, &m.EnvelopeETag,
			&rank,
		); err != nil {
			return nil, err
		}
		if toJSON != "" {
			_ = json.Unmarshal([]byte(toJSON), &m.ToAddresses)
		}
		if ccJSON != "" {
			_ = json.Unmarshal([]byte(ccJSON), &m.CcAddresses)
		}
		if bccJSON != "" {
			_ = json.Unmarshal([]byte(bccJSON), &m.BccAddresses)
		}
		if catsJSON != "" {
			_ = json.Unmarshal([]byte(catsJSON), &m.Categories)
		}
		m.ReceivedAt = unixToTime(recvAt)
		m.SentAt = unixToTime(sentAt)
		m.FlagDueAt = unixToTime(flagDueAt)
		m.FlagCompletedAt = unixToTime(flagCompAt)
		m.LastModifiedAt = unixToTime(lastModAt)
		m.CachedAt = time.Unix(cachedAt, 0)
		m.IsRead = isRead != 0
		m.IsDraft = isDraft != 0
		m.HasAttachments = hasAtt != 0
		out = append(out, MessageMatch{Message: m, Rank: rank})
	}
	return out, rows.Err()
}

// messageColumnsPrefixed returns the message columns with the given table alias.
func messageColumnsPrefixed(alias string) string {
	a := alias + "."
	return a + "id, " + a + "account_id, " + a + "folder_id, " +
		"COALESCE(" + a + "internet_message_id, ''), " +
		"COALESCE(" + a + "conversation_id, ''), " +
		"COALESCE(" + a + "conversation_index, X''), " +
		"COALESCE(" + a + "subject, ''), " +
		"COALESCE(" + a + "body_preview, ''), " +
		"COALESCE(" + a + "from_address, ''), " +
		"COALESCE(" + a + "from_name, ''), " +
		"COALESCE(" + a + "to_addresses, '[]'), " +
		"COALESCE(" + a + "cc_addresses, '[]'), " +
		"COALESCE(" + a + "bcc_addresses, '[]'), " +
		"COALESCE(" + a + "received_at, 0), " +
		"COALESCE(" + a + "sent_at, 0), " +
		a + "is_read, " + a + "is_draft, " +
		"COALESCE(" + a + "flag_status, ''), " +
		"COALESCE(" + a + "flag_due_at, 0), " +
		"COALESCE(" + a + "flag_completed_at, 0), " +
		"COALESCE(" + a + "importance, ''), " +
		"COALESCE(" + a + "inference_class, ''), " +
		a + "has_attachments, " +
		"COALESCE(" + a + "categories, '[]'), " +
		"COALESCE(" + a + "web_link, ''), " +
		"COALESCE(" + a + "last_modified_at, 0), " +
		a + "cached_at, " +
		"COALESCE(" + a + "envelope_etag, '')"
}
