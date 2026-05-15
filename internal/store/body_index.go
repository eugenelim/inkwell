package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// IndexBody persists decoded plaintext for messageID and lets the
// FTS5 triggers fan the change out to body_fts + body_trigram.
// Idempotent: re-indexing identical content is a no-op for the FTS
// tables because the AFTER UPDATE OF content trigger only fires
// when content actually changes. Spec 35 §6.1.
func (s *store) IndexBody(ctx context.Context, e BodyIndexEntry) error {
	if e.MessageID == "" {
		return fmt.Errorf("store: IndexBody: empty MessageID")
	}
	if e.AccountID == 0 {
		return fmt.Errorf("store: IndexBody: zero AccountID")
	}
	if e.FolderID == "" {
		return fmt.Errorf("store: IndexBody: empty FolderID")
	}
	now := time.Now().Unix()
	truncated := 0
	if e.Truncated {
		truncated = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO body_text (message_id, account_id, folder_id, content, content_size, indexed_at, last_accessed_at, truncated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			account_id       = excluded.account_id,
			folder_id        = excluded.folder_id,
			content          = excluded.content,
			content_size     = excluded.content_size,
			indexed_at       = excluded.indexed_at,
			last_accessed_at = excluded.last_accessed_at,
			truncated        = excluded.truncated`,
		e.MessageID, e.AccountID, e.FolderID, e.Content, int64(len(e.Content)), now, now, truncated)
	return err
}

// UnindexBody removes a single message's row from body_text. The
// `ON DELETE CASCADE` from `messages` handles the permanent-delete
// hot path automatically; this method covers the explicit
// `inkwell index evict --message-id=…` path. Spec 35 §6.1.
func (s *store) UnindexBody(ctx context.Context, messageID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM body_text WHERE message_id = ?`, messageID)
	return err
}

// BodyIndexStats returns aggregate stats for `inkwell index status`
// and the maintenance loop. Spec 35 §6.1.
func (s *store) BodyIndexStats(ctx context.Context) (BodyIndexStats, error) {
	var st BodyIndexStats
	var oldest, newest sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(content_size), 0),
			COALESCE(SUM(truncated), 0),
			MIN(indexed_at),
			MAX(indexed_at)
		FROM body_text`).Scan(&st.Rows, &st.Bytes, &st.Truncated, &oldest, &newest)
	if err != nil {
		return BodyIndexStats{}, err
	}
	if oldest.Valid {
		st.OldestIndexedAt = time.Unix(oldest.Int64, 0)
	}
	if newest.Valid {
		st.NewestIndexedAt = time.Unix(newest.Int64, 0)
	}
	return st, nil
}

// PurgeBodyIndex drops every body_text row. Triggers cascade to
// body_fts + body_trigram. Used by `inkwell index disable` and by
// the startup detector when [body_index].enabled has flipped to
// false. Spec 35 §6.1 / §12.
func (s *store) PurgeBodyIndex(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM body_text`)
	return err
}

// EvictBodyIndex enforces [body_index] caps (count + bytes), and
// optionally evicts by age / folder / message id. A zero value
// evicts nothing. Spec 35 §6.1 / §6.4 / §11.
func (s *store) EvictBodyIndex(ctx context.Context, opts EvictBodyIndexOpts) (int, error) {
	if opts.MessageID != "" {
		res, err := s.db.ExecContext(ctx, `DELETE FROM body_text WHERE message_id = ?`, opts.MessageID)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		return int(n), nil
	}

	evicted := 0

	// Folder-scoped + age-based eviction in one pass when either is set.
	if opts.FolderID != "" || !opts.OlderThan.IsZero() {
		where := []string{}
		args := []any{}
		if opts.FolderID != "" {
			where = append(where, "folder_id = ?")
			args = append(args, opts.FolderID)
		}
		if !opts.OlderThan.IsZero() {
			where = append(where, "last_accessed_at < ?")
			args = append(args, opts.OlderThan.Unix())
		}
		// #nosec G202 — `where` is built from two fixed string literals above (`folder_id = ?` and `last_accessed_at < ?`); user-supplied values bind via `?` placeholders, never concatenated.
		q := `DELETE FROM body_text WHERE ` + strings.Join(where, " AND ")
		res, err := s.db.ExecContext(ctx, q, args...) //nolint:gosec
		if err != nil {
			return evicted, err
		}
		n, _ := res.RowsAffected()
		evicted += int(n)
	}

	// Cap-driven eviction: drop oldest-by-last_accessed_at until both
	// caps are satisfied. Mirrors [EvictBodies] (spec 02 §3.5).
	if opts.MaxCount <= 0 && opts.MaxBytes <= 0 {
		return evicted, nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return evicted, err
	}
	defer func() { _ = tx.Rollback() }()
	var totalCount int
	var totalBytes int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(content_size), 0) FROM body_text`).Scan(&totalCount, &totalBytes); err != nil {
		return evicted, err
	}
	if (opts.MaxCount <= 0 || totalCount <= opts.MaxCount) && (opts.MaxBytes <= 0 || totalBytes <= opts.MaxBytes) {
		return evicted, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT message_id, content_size FROM body_text ORDER BY last_accessed_at ASC`)
	if err != nil {
		return evicted, err
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
			return evicted, err
		}
		victims = append(victims, v)
	}
	_ = rows.Close()
	for _, v := range victims {
		overCount := opts.MaxCount > 0 && totalCount > opts.MaxCount
		overBytes := opts.MaxBytes > 0 && totalBytes > opts.MaxBytes
		if !overCount && !overBytes {
			break
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM body_text WHERE message_id = ?`, v.id); err != nil {
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

// SearchBodyText runs an FTS5 MATCH against body_fts and returns hits
// in BM25 order (lowest score = best match in SQLite's bm25 sign
// convention; we negate so callers can sort descending). Bumps the
// matched rows' last_accessed_at as a side effect. Spec 35 §6.3.
func (s *store) SearchBodyText(ctx context.Context, q BodyTextQuery) ([]BodyTextHit, error) {
	if q.AccountID == 0 {
		return nil, fmt.Errorf("store: SearchBodyText: zero AccountID")
	}
	if q.Query == "" {
		return nil, fmt.Errorf("store: SearchBodyText: empty Query")
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	sqlText := `
		SELECT b.message_id,
		       bm25(body_fts) AS score,
		       snippet(body_fts, 0, '«', '»', '…', 32) AS snip
		FROM body_text b
		JOIN body_fts ON body_fts.rowid = b.rowid
		WHERE b.account_id = ?
		  AND (? = '' OR b.folder_id = ?)
		  AND body_fts MATCH ?
		ORDER BY score
		LIMIT ?`
	rows, err := s.db.QueryContext(ctx, sqlText, q.AccountID, q.FolderID, q.FolderID, q.Query, q.Limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []BodyTextHit
	var ids []string
	for rows.Next() {
		var h BodyTextHit
		if err := rows.Scan(&h.MessageID, &h.Score, &h.Snippet); err != nil {
			return nil, err
		}
		// SQLite's bm25 returns negative numbers for better matches;
		// negate so the caller's descending sort is consistent.
		h.Score = -h.Score
		out = append(out, h)
		ids = append(ids, h.MessageID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		s.bumpAccess(ctx, ids)
	}
	return out, nil
}

// SearchBodyTrigramCandidates returns candidate (message_id, content)
// rows whose decoded body matches every literal in q.Literals via
// LIKE '%lit%' (index-accelerated by the trigram tokenizer). Each
// literal must be at least 3 characters; shorter literals would
// require a non-indexed scan. Spec 35 §6.3 + §3.3.
func (s *store) SearchBodyTrigramCandidates(ctx context.Context, q BodyTrigramQuery) ([]BodyCandidate, error) {
	if q.AccountID == 0 {
		return nil, fmt.Errorf("store: SearchBodyTrigramCandidates: zero AccountID")
	}
	if len(q.Literals) == 0 {
		return nil, fmt.Errorf("store: SearchBodyTrigramCandidates: empty Literals")
	}
	for _, lit := range q.Literals {
		if len(lit) < 3 {
			return nil, fmt.Errorf("store: SearchBodyTrigramCandidates: literal %q is shorter than 3 chars", lit)
		}
	}
	if q.Limit <= 0 {
		q.Limit = 2000
	}

	var (
		clauses []string
		args    []any
	)
	args = append(args, q.AccountID)
	clauses = append(clauses, "b.account_id = ?")

	if q.FolderID != "" {
		clauses = append(clauses, "b.folder_id = ?")
		args = append(args, q.FolderID)
	}

	for _, lit := range q.Literals {
		clauses = append(clauses, "b.content LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLikeLiteral(lit)+"%")
	}

	join := ""
	if q.StructuralWhere != "" {
		join = " JOIN messages m ON m.id = b.message_id "
		clauses = append(clauses, "("+q.StructuralWhere+")")
		args = append(args, q.StructuralArgs...)
	}

	// #nosec G202 — `clauses` is built from three fixed string literals above (`b.account_id = ?`, `b.folder_id = ?`, `b.content LIKE ? ESCAPE '\'`) plus the caller-supplied `q.StructuralWhere` which the caller (eval_local) builds from a closed set of column-name string literals via the same likeArgs / likeOne helpers as the rest of pattern. All user-supplied values bind via `?`.
	sqlText := `SELECT b.message_id, b.content FROM body_text b` + join +
		` JOIN body_trigram t ON t.rowid = b.rowid WHERE ` +
		strings.Join(clauses, " AND ") + ` LIMIT ?`
	args = append(args, q.Limit)

	rows, err := s.db.QueryContext(ctx, sqlText, args...) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []BodyCandidate
	var ids []string
	for rows.Next() {
		var c BodyCandidate
		if err := rows.Scan(&c.MessageID, &c.Content); err != nil {
			return nil, err
		}
		out = append(out, c)
		ids = append(ids, c.MessageID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		s.bumpAccess(ctx, ids)
	}
	return out, nil
}

// GetBodyText returns the decoded plaintext for one indexed message.
// Returns [ErrNotFound] when the message is not in the index.
func (s *store) GetBodyText(ctx context.Context, messageID string) (string, error) {
	var content string
	err := s.db.QueryRowContext(ctx, `SELECT content FROM body_text WHERE message_id = ?`, messageID).Scan(&content)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return content, nil
}

// UpdateBodyTextFolder relocates a body_text row when its message
// moves to a different folder. No-op when the message is not indexed.
// Called by UpdateMessageFields when the folder_id field changes —
// keeps `~m Folder & ~b foo` consistent after a move.
func (s *store) UpdateBodyTextFolder(ctx context.Context, messageID, folderID string) error {
	if messageID == "" || folderID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE body_text SET folder_id = ? WHERE message_id = ?`, folderID, messageID)
	return err
}

// bumpAccess updates last_accessed_at for the supplied ids. Errors
// are silently dropped — LRU bookkeeping is best-effort and must not
// fail the query that produced the hits.
func (s *store) bumpAccess(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(ids)+1)
	args = append(args, time.Now().Unix())
	for _, id := range ids {
		args = append(args, id)
	}
	// #nosec G202 — `placeholders` is a generated `?,?,?,…` string from the local `ids` slice length; only the placeholder count is concatenated, never any user value. Every id binds through `args...`.
	_, _ = s.db.ExecContext(ctx, `UPDATE body_text SET last_accessed_at = ? WHERE message_id IN (`+placeholders+`)`, args...) //nolint:gosec
}

// escapeLikeLiteral escapes the SQL LIKE wildcard metacharacters in
// lit (`\`, `%`, `_`) so the caller's `'%lit%'` template only
// triggers wildcards at the outer ends. Matches the existing
// `likeArgs` helper in eval_local.go for consistency.
func escapeLikeLiteral(lit string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(lit)
}
