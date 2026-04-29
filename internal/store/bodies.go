package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// GetBody returns the cached body for messageID, or [ErrNotFound]. The
// caller is expected to call [TouchBody] asynchronously after a hit.
func (s *store) GetBody(ctx context.Context, messageID string) (*Body, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT message_id, content_type, content, content_size, fetched_at, last_accessed_at
		FROM bodies WHERE message_id = ?`, messageID)
	var b Body
	var fetched, accessed int64
	if err := row.Scan(&b.MessageID, &b.ContentType, &b.Content, &b.ContentSize, &fetched, &accessed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	b.FetchedAt = time.Unix(fetched, 0)
	b.LastAccessedAt = time.Unix(accessed, 0)
	return &b, nil
}

// PutBody stores or replaces a body. content_size is recomputed from the
// content length if omitted.
func (s *store) PutBody(ctx context.Context, b Body) error {
	if b.ContentSize == 0 {
		b.ContentSize = int64(len(b.Content))
	}
	if b.FetchedAt.IsZero() {
		b.FetchedAt = time.Now()
	}
	if b.LastAccessedAt.IsZero() {
		b.LastAccessedAt = b.FetchedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO bodies (message_id, content_type, content, content_size, fetched_at, last_accessed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			content_type = excluded.content_type,
			content = excluded.content,
			content_size = excluded.content_size,
			fetched_at = excluded.fetched_at,
			last_accessed_at = excluded.last_accessed_at`,
		b.MessageID, b.ContentType, b.Content, b.ContentSize, b.FetchedAt.Unix(), b.LastAccessedAt.Unix())
	return err
}

// TouchBody updates last_accessed_at = now for messageID.
func (s *store) TouchBody(ctx context.Context, messageID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE bodies SET last_accessed_at = ? WHERE message_id = ?", time.Now().Unix(), messageID)
	return err
}

// EvictBodies enforces the LRU caps from §3.5: at most maxCount rows AND
// at most maxBytes total content_size. Returns the number of rows
// evicted. Eviction is one transaction.
func (s *store) EvictBodies(ctx context.Context, maxCount int, maxBytes int64) (int, error) {
	if maxCount <= 0 && maxBytes <= 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var totalCount int
	var totalBytes int64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(content_size), 0) FROM bodies").Scan(&totalCount, &totalBytes); err != nil {
		return 0, err
	}
	if (maxCount <= 0 || totalCount <= maxCount) && (maxBytes <= 0 || totalBytes <= maxBytes) {
		return 0, tx.Commit()
	}

	rows, err := tx.QueryContext(ctx, "SELECT message_id, content_size FROM bodies ORDER BY last_accessed_at ASC")
	if err != nil {
		return 0, err
	}
	type victim struct {
		id   string
		size int64
	}
	var victims []victim
	for rows.Next() {
		var v victim
		if err := rows.Scan(&v.id, &v.size); err != nil {
			_ = rows.Close()
			return 0, err
		}
		victims = append(victims, v)
	}
	_ = rows.Close()

	evicted := 0
	for _, v := range victims {
		overCount := maxCount > 0 && totalCount > maxCount
		overBytes := maxBytes > 0 && totalBytes > maxBytes
		if !overCount && !overBytes {
			break
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM bodies WHERE message_id = ?", v.id); err != nil {
			return evicted, err
		}
		totalCount--
		totalBytes -= v.size
		evicted++
	}
	if err := tx.Commit(); err != nil {
		return evicted, err
	}
	return evicted, nil
}
