package store

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestFTS5ProbeTokenizers is spec 35 §3.6's canary. modernc.org/sqlite
// v1.50.0 embeds SQLite 3.53.0 with the `unicode61`, `porter`,
// `ascii`, and `trigram` tokenizers plus external-content FTS5 — and
// the body index relies on every one of them. If a future modernc
// release regresses any tokenizer, this test fails and release blocks
// before the body index can silently break.
func TestFTS5ProbeTokenizers(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// 1. unicode61 with diacritic folding — the default for messages_fts
	// and body_fts.
	_, err = db.Exec(`CREATE VIRTUAL TABLE t_u USING fts5(x, tokenize='unicode61 remove_diacritics 2')`)
	require.NoError(t, err, "unicode61 remove_diacritics 2 tokenizer must be available")

	// 2. porter stemming wrapping unicode61 — reserved for spec 35 v2.
	_, err = db.Exec(`CREATE VIRTUAL TABLE t_p USING fts5(x, tokenize="porter unicode61 remove_diacritics 2")`)
	require.NoError(t, err, "porter unicode61 tokenizer combination must be available")

	// 3. ascii tokenizer — sanity check (covers minimal config path).
	_, err = db.Exec(`CREATE VIRTUAL TABLE t_a USING fts5(x, tokenize='ascii')`)
	require.NoError(t, err, "ascii tokenizer must be available")

	// 4. trigram tokenizer — the load-bearing one for body_trigram. Added
	// to SQLite in 3.34; modernc v1.50.0 embeds 3.53.0. Without trigram,
	// the regex narrowing path in spec 35 §3.3 cannot work.
	_, err = db.Exec(`CREATE VIRTUAL TABLE t_t USING fts5(x, tokenize='trigram')`)
	require.NoError(t, err, "trigram tokenizer must be available")

	// 5. trigram + detail=none — the exact shape body_trigram uses.
	_, err = db.Exec(`CREATE VIRTUAL TABLE t_tn USING fts5(x, tokenize='trigram', detail=none)`)
	require.NoError(t, err, "trigram + detail=none must be available")

	// 6. external-content FTS5 over a parent table — the shape body_fts
	// and body_trigram both use against body_text.
	_, err = db.Exec(`CREATE TABLE src(id INTEGER PRIMARY KEY, body TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE VIRTUAL TABLE ext USING fts5(body, content='src', content_rowid='id', tokenize='unicode61 remove_diacritics 2')`)
	require.NoError(t, err, "external-content FTS5 must be available")

	// 7. LIKE accelerated by trigram. The single most important capability
	// — without it, body regex narrowing falls back to a full scan and
	// spec 35's perf budgets fail.
	_, err = db.Exec(`INSERT INTO t_t(rowid, x) VALUES (1, 'Please reset your password by clicking here.')`)
	require.NoError(t, err)
	row := db.QueryRow(`SELECT x FROM t_t WHERE x LIKE '%reset%'`)
	var got string
	require.NoError(t, row.Scan(&got))
	require.Contains(t, got, "reset")
}
