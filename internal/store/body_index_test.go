package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// indexEntry produces a BodyIndexEntry tied to acc + fld.
func indexEntry(messageID, content string, acc int64, fld string) BodyIndexEntry {
	return BodyIndexEntry{
		MessageID: messageID,
		AccountID: acc,
		FolderID:  fld,
		Content:   content,
	}
}

// seedIndexed seeds a message + body_text row for messageID. Returns
// the seeded body content.
func seedIndexed(t *testing.T, s Store, acc int64, fld, messageID, body string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ID: messageID, AccountID: acc, FolderID: fld,
		Subject: "subject for " + messageID, ReceivedAt: time.Now(),
	}))
	require.NoError(t, s.IndexBody(ctx, indexEntry(messageID, body, acc, fld)))
}

func TestIndexBody_RoundTrip(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	seedIndexed(t, s, acc, fld.ID, "m1", "the quick brown fox jumps over the lazy dog")

	st := s.(*store)
	ctx := context.Background()

	// body_text row.
	var content string
	var size int64
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT content, content_size FROM body_text WHERE message_id = ?`, "m1").Scan(&content, &size))
	require.Equal(t, "the quick brown fox jumps over the lazy dog", content)
	require.Equal(t, int64(len(content)), size)

	// body_fts row exists for the same rowid.
	var ftsRows int
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_fts WHERE body_fts MATCH 'quick'`).Scan(&ftsRows))
	require.Equal(t, 1, ftsRows)

	// body_trigram row exists for the same rowid.
	var triRows int
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_trigram WHERE content LIKE '%fox%'`).Scan(&triRows))
	require.Equal(t, 1, triRows)
}

func TestIndexBody_Idempotent(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "alpha bravo charlie")

	st := s.(*store)
	var indexed1 int64
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT indexed_at FROM body_text WHERE message_id = ?`, "m1").Scan(&indexed1))

	// Re-index with the same content — should replace timestamps but
	// not double the FTS rows.
	time.Sleep(1100 * time.Millisecond)
	require.NoError(t, s.IndexBody(ctx, indexEntry("m1", "alpha bravo charlie", acc, fld.ID)))
	var indexed2 int64
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT indexed_at FROM body_text WHERE message_id = ?`, "m1").Scan(&indexed2))
	require.GreaterOrEqual(t, indexed2, indexed1)

	var ftsRows int
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_fts WHERE body_fts MATCH 'alpha'`).Scan(&ftsRows))
	require.Equal(t, 1, ftsRows)
}

func TestIndexBody_UpdateContentRefreshesFTS(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()

	seedIndexed(t, s, acc, fld.ID, "m1", "the original body talks about apples")

	// Re-index with new content. AFTER UPDATE OF content trigger
	// rebuilds FTS rows.
	require.NoError(t, s.IndexBody(ctx, indexEntry("m1", "completely different body about oranges", acc, fld.ID)))

	st := s.(*store)
	var apples, oranges int
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_fts WHERE body_fts MATCH 'apples'`).Scan(&apples))
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_fts WHERE body_fts MATCH 'oranges'`).Scan(&oranges))
	require.Equal(t, 0, apples)
	require.Equal(t, 1, oranges)
}

func TestUnindexBody_CascadesFTS(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "delete me please")

	require.NoError(t, s.UnindexBody(ctx, "m1"))

	st := s.(*store)
	var rows int
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_text WHERE message_id = ?`, "m1").Scan(&rows))
	require.Equal(t, 0, rows)

	var ftsRows int
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_fts WHERE body_fts MATCH 'delete'`).Scan(&ftsRows))
	require.Equal(t, 0, ftsRows)

	var triRows int
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_trigram WHERE content LIKE '%please%'`).Scan(&triRows))
	require.Equal(t, 0, triRows)
}

func TestPermanentDeleteCascades(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "this body about secrets")

	// Permanent-delete the message row. FK cascade removes body_text.
	require.NoError(t, s.DeleteMessage(ctx, "m1"))

	st := s.(*store)
	var rows int
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_text WHERE message_id = ?`, "m1").Scan(&rows))
	require.Equal(t, 0, rows)
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_fts WHERE body_fts MATCH 'secrets'`).Scan(&rows))
	require.Equal(t, 0, rows)
}

func TestBodyIndexStats(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	for i, body := range []string{"alpha", "bravo charlie", "delta echo foxtrot"} {
		seedIndexed(t, s, acc, fld.ID, "m"+string(rune('0'+i)), body)
	}

	// Mark one as truncated.
	require.NoError(t, s.IndexBody(ctx, BodyIndexEntry{
		MessageID: "m1", AccountID: acc, FolderID: fld.ID,
		Content: "bravo charlie", Truncated: true,
	}))

	stats, err := s.BodyIndexStats(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.Rows)
	require.Equal(t, int64(len("alpha")+len("bravo charlie")+len("delta echo foxtrot")), stats.Bytes)
	require.Equal(t, int64(1), stats.Truncated)
	require.False(t, stats.OldestIndexedAt.IsZero())
}

func TestEvictBodyIndex_CountCap(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		mid := "m" + string(rune('0'+i))
		seedIndexed(t, s, acc, fld.ID, mid, "body content "+mid)
		// Stagger last_accessed_at so eviction order is deterministic.
		_, err := s.(*store).db.ExecContext(ctx,
			`UPDATE body_text SET last_accessed_at = ? WHERE message_id = ?`,
			time.Now().Add(-time.Duration(5-i)*time.Hour).Unix(), mid)
		require.NoError(t, err)
	}

	evicted, err := s.EvictBodyIndex(ctx, EvictBodyIndexOpts{MaxCount: 3})
	require.NoError(t, err)
	require.Equal(t, 2, evicted)

	stats, err := s.BodyIndexStats(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.Rows)

	// The two oldest (m0, m1) should be gone.
	st := s.(*store)
	var leftover int
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_text WHERE message_id IN ('m0','m1')`).Scan(&leftover))
	require.Equal(t, 0, leftover)
}

func TestEvictBodyIndex_ByteCap(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	// Each body is 100 bytes; total 500.
	body := make([]byte, 100)
	for i := range body {
		body[i] = 'a'
	}
	for i := 0; i < 5; i++ {
		mid := "m" + string(rune('0'+i))
		seedIndexed(t, s, acc, fld.ID, mid, string(body))
		_, err := s.(*store).db.ExecContext(ctx,
			`UPDATE body_text SET last_accessed_at = ? WHERE message_id = ?`,
			time.Now().Add(-time.Duration(5-i)*time.Hour).Unix(), mid)
		require.NoError(t, err)
	}

	evicted, err := s.EvictBodyIndex(ctx, EvictBodyIndexOpts{MaxBytes: 250})
	require.NoError(t, err)
	require.Equal(t, 3, evicted)

	stats, err := s.BodyIndexStats(ctx)
	require.NoError(t, err)
	require.LessOrEqual(t, stats.Bytes, int64(250))
}

func TestEvictBodyIndex_OlderThan(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		mid := "m" + string(rune('0'+i))
		seedIndexed(t, s, acc, fld.ID, mid, "body for "+mid)
		_, err := s.(*store).db.ExecContext(ctx,
			`UPDATE body_text SET last_accessed_at = ? WHERE message_id = ?`,
			time.Now().Add(-time.Duration((4-i)*24)*time.Hour).Unix(), mid)
		require.NoError(t, err)
	}

	cutoff := time.Now().Add(-48 * time.Hour)
	evicted, err := s.EvictBodyIndex(ctx, EvictBodyIndexOpts{OlderThan: cutoff})
	require.NoError(t, err)
	require.Equal(t, 2, evicted)
}

func TestEvictBodyIndex_FolderScope(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	other := Folder{
		ID: "folder-archive", AccountID: acc, DisplayName: "Archive",
		WellKnownName: "archive", LastSyncedAt: time.Now(),
	}
	require.NoError(t, s.UpsertFolder(context.Background(), other))
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "inbox body")
	seedIndexed(t, s, acc, other.ID, "m2", "archive body")

	evicted, err := s.EvictBodyIndex(ctx, EvictBodyIndexOpts{FolderID: fld.ID})
	require.NoError(t, err)
	require.Equal(t, 1, evicted)

	stats, err := s.BodyIndexStats(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Rows)
}

func TestEvictBodyIndex_MessageID(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "first")
	seedIndexed(t, s, acc, fld.ID, "m2", "second")

	evicted, err := s.EvictBodyIndex(ctx, EvictBodyIndexOpts{MessageID: "m1"})
	require.NoError(t, err)
	require.Equal(t, 1, evicted)

	_, err = s.GetBodyText(ctx, "m1")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestPurgeBodyIndex_ClearsAllThreeTables(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		seedIndexed(t, s, acc, fld.ID, "m"+string(rune('0'+i)), "body "+string(rune('0'+i)))
	}

	require.NoError(t, s.PurgeBodyIndex(ctx))

	st := s.(*store)
	for _, tbl := range []string{"body_text", "body_fts", "body_trigram"} {
		var rows int
		require.NoError(t, st.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM `+tbl).Scan(&rows))
		require.Equal(t, 0, rows, "%s should be empty after purge", tbl)
	}
}

func TestSearchBodyText_BM25Ordering(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	// More occurrences of "budget" → higher BM25 score.
	seedIndexed(t, s, acc, fld.ID, "m1", "weekly note about meeting and lunch")
	seedIndexed(t, s, acc, fld.ID, "m2", "budget review minutes — budget allocation budget plan")
	seedIndexed(t, s, acc, fld.ID, "m3", "quick note mentioning budget once")

	hits, err := s.SearchBodyText(ctx, BodyTextQuery{
		AccountID: acc, Query: "budget", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, hits, 2)
	// m2 (3 occurrences) ranks ahead of m3 (1 occurrence).
	require.Equal(t, "m2", hits[0].MessageID)
	require.Equal(t, "m3", hits[1].MessageID)
	require.Contains(t, hits[0].Snippet, "budget")
	require.Greater(t, hits[0].Score, hits[1].Score)
}

func TestSearchBodyText_FolderScope(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	other := Folder{ID: "folder-other", AccountID: acc, DisplayName: "Other", LastSyncedAt: time.Now()}
	require.NoError(t, s.UpsertFolder(context.Background(), other))
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "inbox body about budget")
	seedIndexed(t, s, acc, other.ID, "m2", "other body about budget")

	hits, err := s.SearchBodyText(ctx, BodyTextQuery{
		AccountID: acc, FolderID: fld.ID, Query: "budget", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "m1", hits[0].MessageID)
}

func TestSearchBodyTrigramCandidates(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "please reset your password by clicking auth-token=ab12cd34")
	seedIndexed(t, s, acc, fld.ID, "m2", "weekly digest about news items")
	seedIndexed(t, s, acc, fld.ID, "m3", "another auth thread but no token")

	// Both literals → only m1 matches.
	out, err := s.SearchBodyTrigramCandidates(ctx, BodyTrigramQuery{
		AccountID: acc,
		Literals:  []string{"auth", "token="},
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "m1", out[0].MessageID)
	require.Contains(t, out[0].Content, "auth-token=ab12cd34")
}

func TestSearchBodyTrigramCandidates_LiteralIsParameterised(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "harmless body")

	// Injection probe: the SQL meta-characters in the literal must
	// not terminate the LIKE pattern or drop the table.
	out, err := s.SearchBodyTrigramCandidates(ctx, BodyTrigramQuery{
		AccountID: acc,
		Literals:  []string{"'); DROP TABLE body_text; --"},
		Limit:     10,
	})
	require.NoError(t, err)
	require.Empty(t, out, "injection probe should return no matches")

	// The table is still there.
	st := s.(*store)
	var rows int
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM body_text`).Scan(&rows))
	require.Equal(t, 1, rows)
}

func TestSearchBodyTrigramCandidates_RejectsShortLiteral(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	seedIndexed(t, s, acc, fld.ID, "m1", "body")
	_, err := s.SearchBodyTrigramCandidates(context.Background(), BodyTrigramQuery{
		AccountID: acc, Literals: []string{"ab"}, Limit: 10,
	})
	require.Error(t, err)
}

func TestSearchBodyTrigramCandidates_StructuralFilter(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	// Flag m1; m2 stays unflagged.
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ID: "m1", AccountID: acc, FolderID: fld.ID,
		FlagStatus: "flagged", ReceivedAt: time.Now(),
	}))
	require.NoError(t, s.IndexBody(ctx, indexEntry("m1", "auth token in body", acc, fld.ID)))
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ID: "m2", AccountID: acc, FolderID: fld.ID,
		ReceivedAt: time.Now(),
	}))
	require.NoError(t, s.IndexBody(ctx, indexEntry("m2", "auth token in another body", acc, fld.ID)))

	out, err := s.SearchBodyTrigramCandidates(ctx, BodyTrigramQuery{
		AccountID:       acc,
		Literals:        []string{"auth"},
		StructuralWhere: "m.flag_status = ?",
		StructuralArgs:  []any{"flagged"},
		Limit:           10,
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "m1", out[0].MessageID)
}

func TestGetBodyText_NotFound(t *testing.T) {
	s := OpenTestStore(t)
	_, err := s.GetBodyText(context.Background(), "missing")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestUpdateBodyTextFolder(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	other := Folder{ID: "folder-archive", AccountID: acc, DisplayName: "Archive", LastSyncedAt: time.Now()}
	require.NoError(t, s.UpsertFolder(context.Background(), other))
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "body content")

	require.NoError(t, s.UpdateBodyTextFolder(ctx, "m1", other.ID))

	st := s.(*store)
	var folderID string
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT folder_id FROM body_text WHERE message_id = ?`, "m1").Scan(&folderID))
	require.Equal(t, other.ID, folderID)
}

func TestUpdateMessageFields_KeepsBodyTextFolderInSync(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	other := Folder{ID: "folder-archive", AccountID: acc, DisplayName: "Archive", LastSyncedAt: time.Now()}
	require.NoError(t, s.UpsertFolder(context.Background(), other))
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "body content")

	newFolder := other.ID
	require.NoError(t, s.UpdateMessageFields(ctx, "m1", MessageFields{FolderID: &newFolder}))

	st := s.(*store)
	var folderID string
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT folder_id FROM body_text WHERE message_id = ?`, "m1").Scan(&folderID))
	require.Equal(t, other.ID, folderID)
}

func TestSearchBodyText_BumpsLastAccessed(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	fld := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedIndexed(t, s, acc, fld.ID, "m1", "the budget review minutes")

	st := s.(*store)
	// Force last_accessed_at backward.
	_, err := st.db.ExecContext(ctx,
		`UPDATE body_text SET last_accessed_at = ? WHERE message_id = ?`,
		time.Now().Add(-48*time.Hour).Unix(), "m1")
	require.NoError(t, err)
	var before int64
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT last_accessed_at FROM body_text WHERE message_id = ?`, "m1").Scan(&before))

	hits, err := s.SearchBodyText(ctx, BodyTextQuery{
		AccountID: acc, Query: "budget", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, hits, 1)

	var after int64
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT last_accessed_at FROM body_text WHERE message_id = ?`, "m1").Scan(&after))
	require.Greater(t, after, before)
}
