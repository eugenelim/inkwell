package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GetMessage returns the cached envelope for id, or [ErrNotFound].
func (s *store) GetMessage(ctx context.Context, id string) (*Message, error) {
	row := s.db.QueryRowContext(ctx, selectMessageByID, id)
	m, err := scanMessage(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return m, nil
}

// ListMessages returns envelopes matching q.
func (s *store) ListMessages(ctx context.Context, q MessageQuery) ([]Message, error) {
	sql, args := buildListSQL(q)
	rows, err := s.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
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

// UpsertMessage writes a single envelope.
func (s *store) UpsertMessage(ctx context.Context, m Message) error {
	return s.UpsertMessagesBatch(ctx, []Message{m})
}

// UpsertMessagesBatch wraps a single transaction to upsert N envelopes.
func (s *store) UpsertMessagesBatch(ctx context.Context, ms []Message) error {
	if len(ms) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, upsertMessageSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now()
	for _, m := range ms {
		if m.CachedAt.IsZero() {
			m.CachedAt = now
		}
		if err := bindUpsert(ctx, stmt, m); err != nil {
			return fmt.Errorf("upsert message %s: %w", m.ID, err)
		}
	}
	return tx.Commit()
}

// DeleteMessage removes a single message.
func (s *store) DeleteMessage(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM messages WHERE id = ?", id)
	return err
}

// DeleteMessages removes the listed message ids in one transaction.
func (s *store) DeleteMessages(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, "DELETE FROM messages WHERE id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpdateMessageFields applies a partial update. Only non-nil fields are set.
func (s *store) UpdateMessageFields(ctx context.Context, id string, f MessageFields) error {
	var sets []string
	var args []any
	if f.IsRead != nil {
		v := 0
		if *f.IsRead {
			v = 1
		}
		sets = append(sets, "is_read = ?")
		args = append(args, v)
	}
	if f.FlagStatus != nil {
		sets = append(sets, "flag_status = ?")
		args = append(args, nullStr(*f.FlagStatus))
	}
	if f.FolderID != nil {
		sets = append(sets, "folder_id = ?")
		args = append(args, *f.FolderID)
	}
	if f.Categories != nil {
		b, _ := json.Marshal(*f.Categories)
		sets = append(sets, "categories = ?")
		args = append(args, string(b))
	}
	if f.LastModifiedAt != nil {
		sets = append(sets, "last_modified_at = ?")
		args = append(args, nullTime(*f.LastModifiedAt))
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := s.db.ExecContext(ctx, "UPDATE messages SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	return err
}

const upsertMessageSQL = `
INSERT INTO messages (
	id, account_id, folder_id, internet_message_id, conversation_id, conversation_index,
	subject, body_preview, from_address, from_name, to_addresses, cc_addresses, bcc_addresses,
	received_at, sent_at, is_read, is_draft, flag_status, flag_due_at, flag_completed_at,
	importance, inference_class, has_attachments, categories, web_link, last_modified_at,
	cached_at, envelope_etag
) VALUES (?,?,?,?,?,?, ?,?,?,?,?,?,?, ?,?,?,?,?,?,?, ?,?,?,?,?,?, ?,?)
ON CONFLICT(id) DO UPDATE SET
	account_id = excluded.account_id,
	folder_id = excluded.folder_id,
	internet_message_id = excluded.internet_message_id,
	conversation_id = excluded.conversation_id,
	conversation_index = excluded.conversation_index,
	subject = excluded.subject,
	body_preview = excluded.body_preview,
	from_address = excluded.from_address,
	from_name = excluded.from_name,
	to_addresses = excluded.to_addresses,
	cc_addresses = excluded.cc_addresses,
	bcc_addresses = excluded.bcc_addresses,
	received_at = excluded.received_at,
	sent_at = excluded.sent_at,
	is_read = excluded.is_read,
	is_draft = excluded.is_draft,
	flag_status = excluded.flag_status,
	flag_due_at = excluded.flag_due_at,
	flag_completed_at = excluded.flag_completed_at,
	importance = excluded.importance,
	inference_class = excluded.inference_class,
	has_attachments = excluded.has_attachments,
	categories = excluded.categories,
	web_link = excluded.web_link,
	last_modified_at = excluded.last_modified_at,
	envelope_etag = excluded.envelope_etag
`

func bindUpsert(ctx context.Context, stmt *sql.Stmt, m Message) error {
	to, _ := json.Marshal(m.ToAddresses)
	cc, _ := json.Marshal(m.CcAddresses)
	bcc, _ := json.Marshal(m.BccAddresses)
	cats, _ := json.Marshal(m.Categories)
	isRead, isDraft, hasAtt := boolToInt(m.IsRead), boolToInt(m.IsDraft), boolToInt(m.HasAttachments)
	_, err := stmt.ExecContext(ctx,
		m.ID, m.AccountID, m.FolderID,
		nullStr(m.InternetMessageID), nullStr(m.ConversationID), m.ConversationIndex,
		nullStr(m.Subject), nullStr(m.BodyPreview), nullStr(m.FromAddress), nullStr(m.FromName),
		string(to), string(cc), string(bcc),
		nullTime(m.ReceivedAt), nullTime(m.SentAt), isRead, isDraft,
		nullStr(m.FlagStatus), nullTime(m.FlagDueAt), nullTime(m.FlagCompletedAt),
		nullStr(m.Importance), nullStr(m.InferenceClass), hasAtt,
		string(cats), nullStr(m.WebLink), nullTime(m.LastModifiedAt),
		m.CachedAt.Unix(), nullStr(m.EnvelopeETag),
	)
	return err
}

const messageColumns = `
	id, account_id, folder_id, COALESCE(internet_message_id, ''), COALESCE(conversation_id, ''),
	COALESCE(conversation_index, X''), COALESCE(subject, ''), COALESCE(body_preview, ''),
	COALESCE(from_address, ''), COALESCE(from_name, ''),
	COALESCE(to_addresses, '[]'), COALESCE(cc_addresses, '[]'), COALESCE(bcc_addresses, '[]'),
	COALESCE(received_at, 0), COALESCE(sent_at, 0), is_read, is_draft,
	COALESCE(flag_status, ''), COALESCE(flag_due_at, 0), COALESCE(flag_completed_at, 0),
	COALESCE(importance, ''), COALESCE(inference_class, ''), has_attachments,
	COALESCE(categories, '[]'), COALESCE(web_link, ''), COALESCE(last_modified_at, 0),
	cached_at, COALESCE(envelope_etag, '')
`

const selectMessageByID = `SELECT ` + messageColumns + ` FROM messages WHERE id = ?`

// rowScanner abstracts *sql.Row and *sql.Rows so scanMessage works for both.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanMessage(r rowScanner) (*Message, error) {
	var (
		m                                                          Message
		toJSON, ccJSON, bccJSON, catsJSON                          string
		recvAt, sentAt, flagDueAt, flagCompAt, lastModAt, cachedAt int64
		isRead, isDraft, hasAtt                                    int
	)
	err := r.Scan(
		&m.ID, &m.AccountID, &m.FolderID,
		&m.InternetMessageID, &m.ConversationID, &m.ConversationIndex,
		&m.Subject, &m.BodyPreview, &m.FromAddress, &m.FromName,
		&toJSON, &ccJSON, &bccJSON,
		&recvAt, &sentAt, &isRead, &isDraft,
		&m.FlagStatus, &flagDueAt, &flagCompAt,
		&m.Importance, &m.InferenceClass, &hasAtt,
		&catsJSON, &m.WebLink, &lastModAt,
		&cachedAt, &m.EnvelopeETag,
	)
	if err != nil {
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
	return &m, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func buildListSQL(q MessageQuery) (string, []any) {
	var where []string
	var args []any
	if q.AccountID != 0 {
		where = append(where, "account_id = ?")
		args = append(args, q.AccountID)
	}
	if q.FolderID != "" {
		where = append(where, "folder_id = ?")
		args = append(args, q.FolderID)
	}
	if q.ConversationID != "" {
		where = append(where, "conversation_id = ?")
		args = append(args, q.ConversationID)
	}
	if q.From != "" {
		where = append(where, "from_address = ?")
		args = append(args, strings.ToLower(q.From))
	}
	if q.UnreadOnly {
		where = append(where, "is_read = 0")
	}
	if q.FlaggedOnly {
		where = append(where, "flag_status = 'flagged'")
	}
	if q.HasAttachments != nil {
		v := 0
		if *q.HasAttachments {
			v = 1
		}
		where = append(where, "has_attachments = ?")
		args = append(args, v)
	}
	if q.ReceivedAfter != nil {
		where = append(where, "received_at >= ?")
		args = append(args, q.ReceivedAfter.Unix())
	}
	if q.ReceivedBefore != nil {
		where = append(where, "received_at < ?")
		args = append(args, q.ReceivedBefore.Unix())
	}
	if len(q.Categories) > 0 {
		// JSON1: any-of match.
		// EXISTS (SELECT 1 FROM json_each(categories) WHERE value IN (...))
		ph := strings.Repeat(",?", len(q.Categories))[1:]
		where = append(where, "EXISTS (SELECT 1 FROM json_each(categories) WHERE value IN ("+ph+"))")
		for _, c := range q.Categories {
			args = append(args, c)
		}
	}

	order := "received_at DESC"
	switch q.OrderBy {
	case OrderReceivedAsc:
		order = "received_at ASC"
	case OrderSubjectAsc:
		order = "subject COLLATE NOCASE ASC"
	case OrderFromAsc:
		order = "from_address COLLATE NOCASE ASC"
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit, q.Offset)

	stmt := "SELECT " + messageColumns + " FROM messages"
	if len(where) > 0 {
		stmt += " WHERE " + strings.Join(where, " AND ")
	}
	stmt += " ORDER BY " + order + " LIMIT ? OFFSET ?"
	return stmt, args
}
