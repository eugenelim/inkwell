package store

import (
	"context"
	"database/sql"
)

// ListAttachments returns the metadata rows for messageID.
func (s *store) ListAttachments(ctx context.Context, messageID string) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, message_id, name, COALESCE(content_type, ''), size, is_inline, COALESCE(content_id, '')
		FROM attachments WHERE message_id = ? ORDER BY name`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		var inline int
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Name, &a.ContentType, &a.Size, &inline, &a.ContentID); err != nil {
			return nil, err
		}
		a.IsInline = inline != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertAttachments writes (or replaces) the metadata rows.
func (s *store) UpsertAttachments(ctx context.Context, atts []Attachment) error {
	if len(atts) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO attachments (id, message_id, name, content_type, size, is_inline, content_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			message_id = excluded.message_id,
			name = excluded.name,
			content_type = excluded.content_type,
			size = excluded.size,
			is_inline = excluded.is_inline,
			content_id = excluded.content_id`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, a := range atts {
		inline := 0
		if a.IsInline {
			inline = 1
		}
		if _, err := stmt.ExecContext(ctx, a.ID, a.MessageID, a.Name, nullStr(a.ContentType), a.Size, inline, nullStr(a.ContentID)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
