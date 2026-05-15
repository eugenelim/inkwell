package store

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// makeBody generates n bytes of pseudo-English prose seeded so the
// corpus is deterministic across benchmark runs.
func makeBody(seed int64, n int) string {
	r := rand.New(rand.NewSource(seed))
	words := []string{
		"the", "quick", "brown", "fox", "jumps", "over", "lazy",
		"dog", "auth", "token", "password", "reset", "review",
		"budget", "meeting", "deadline", "ship", "test", "code",
		"alpha", "bravo", "charlie", "delta", "echo", "foxtrot",
		"please", "click", "here", "secret", "confidential",
	}
	var b strings.Builder
	b.Grow(n)
	for b.Len() < n {
		b.WriteString(words[r.Intn(len(words))])
		b.WriteByte(' ')
	}
	return b.String()[:n]
}

// seedCorpus inserts n synthetic messages + indexed bodies.
func seedCorpus(b *testing.B, s Store, acc int64, fld, prefix string, n int, bytesPerBody int) {
	ctx := context.Background()
	now := time.Now()
	msgs := make([]Message, 0, n)
	for i := 0; i < n; i++ {
		msgs = append(msgs, Message{
			ID:          fmt.Sprintf("%s-%d", prefix, i),
			AccountID:   acc,
			FolderID:    fld,
			Subject:     fmt.Sprintf("Subject %d", i),
			BodyPreview: fmt.Sprintf("Preview %d", i),
			ReceivedAt:  now.Add(-time.Duration(i) * time.Minute),
		})
		if len(msgs) == 500 {
			if err := s.UpsertMessagesBatch(ctx, msgs); err != nil {
				b.Fatalf("batch upsert: %v", err)
			}
			msgs = msgs[:0]
		}
	}
	if len(msgs) > 0 {
		if err := s.UpsertMessagesBatch(ctx, msgs); err != nil {
			b.Fatalf("batch upsert: %v", err)
		}
	}
	for i := 0; i < n; i++ {
		mid := fmt.Sprintf("%s-%d", prefix, i)
		body := makeBody(int64(i), bytesPerBody)
		if err := s.IndexBody(ctx, BodyIndexEntry{
			MessageID: mid, AccountID: acc, FolderID: fld,
			Content: body,
		}); err != nil {
			b.Fatalf("IndexBody: %v", err)
		}
	}
}

// BenchmarkIndexBody_1KB covers spec 35 §14 row 1 (target <3 ms p95).
func BenchmarkIndexBody_1KB(b *testing.B) {
	s := openBodyIndexBenchStore(b)
	acc, fld := benchSeedRoot(b, s)
	ctx := context.Background()
	body := makeBody(42, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mid := fmt.Sprintf("ix-1k-%d", i)
		// IndexBody upserts the body_text row. The first call inserts;
		// subsequent calls with the same id update + re-fire FTS
		// triggers. To exercise the insert path we mint unique ids.
		_ = s.UpsertMessage(ctx, Message{
			ID: mid, AccountID: acc, FolderID: fld, ReceivedAt: time.Now(),
		})
		if err := s.IndexBody(ctx, BodyIndexEntry{
			MessageID: mid, AccountID: acc, FolderID: fld, Content: body,
		}); err != nil {
			b.Fatalf("IndexBody: %v", err)
		}
	}
}

// BenchmarkIndexBody_10KB covers spec 35 §14 row 2 (target <8 ms p95).
func BenchmarkIndexBody_10KB(b *testing.B) {
	s := openBodyIndexBenchStore(b)
	acc, fld := benchSeedRoot(b, s)
	ctx := context.Background()
	body := makeBody(43, 10*1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mid := fmt.Sprintf("ix-10k-%d", i)
		_ = s.UpsertMessage(ctx, Message{
			ID: mid, AccountID: acc, FolderID: fld, ReceivedAt: time.Now(),
		})
		if err := s.IndexBody(ctx, BodyIndexEntry{
			MessageID: mid, AccountID: acc, FolderID: fld, Content: body,
		}); err != nil {
			b.Fatalf("IndexBody: %v", err)
		}
	}
}

// BenchmarkIndexBody_1MB covers spec 35 §14 row 3 (target <60 ms p95).
func BenchmarkIndexBody_1MB(b *testing.B) {
	s := openBodyIndexBenchStore(b)
	acc, fld := benchSeedRoot(b, s)
	ctx := context.Background()
	body := makeBody(44, 1024*1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mid := fmt.Sprintf("ix-1m-%d", i)
		_ = s.UpsertMessage(ctx, Message{
			ID: mid, AccountID: acc, FolderID: fld, ReceivedAt: time.Now(),
		})
		if err := s.IndexBody(ctx, BodyIndexEntry{
			MessageID: mid, AccountID: acc, FolderID: fld, Content: body,
		}); err != nil {
			b.Fatalf("IndexBody: %v", err)
		}
	}
}

// BenchmarkPurgeBodyIndex_5kCorpus covers spec 35 §14 row 10
// (target <1 s p95 over 5 000 rows). The whole-table DELETE
// cascades through both FTS5 surfaces via the AFTER DELETE
// trigger; the trigger fan-out is the dominant cost.
func BenchmarkPurgeBodyIndex_5kCorpus(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s := openBodyIndexBenchStore(b)
		acc, fld := benchSeedRoot(b, s)
		seedCorpus(b, s, acc, fld, "pg", 5_000, 4*1024)
		ctx := context.Background()
		b.StartTimer()
		if err := s.PurgeBodyIndex(ctx); err != nil {
			b.Fatalf("PurgeBodyIndex: %v", err)
		}
	}
}

// BenchmarkSearchBodyText_5kCorpus covers spec 35 §14 row 4.
// 5 000 bodies × 4 KB; budget <80 ms p95 at 50 k bodies. 5 k is
// what the CI bench corpus tolerates without bloating bench time;
// the dev-machine measurement is the authoritative spec gate.
func BenchmarkSearchBodyText_5kCorpus(b *testing.B) {
	s := openBodyIndexBenchStore(b)
	acc, fld := benchSeedRoot(b, s)
	seedCorpus(b, s, acc, fld, "stx", 5_000, 4*1024)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.SearchBodyText(ctx, BodyTextQuery{
			AccountID: acc, Query: "budget", Limit: 50,
		})
		if err != nil {
			b.Fatalf("SearchBodyText: %v", err)
		}
	}
}

// BenchmarkSearchBodyTrigramCandidates_5kCorpus covers spec 35 §14
// row 5. budget <100 ms p95 at 50 k bodies; 5 k is the CI proxy.
func BenchmarkSearchBodyTrigramCandidates_5kCorpus(b *testing.B) {
	s := openBodyIndexBenchStore(b)
	acc, fld := benchSeedRoot(b, s)
	seedCorpus(b, s, acc, fld, "trg", 5_000, 4*1024)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.SearchBodyTrigramCandidates(ctx, BodyTrigramQuery{
			AccountID: acc,
			Literals:  []string{"auth", "token"},
			Limit:     2000,
		})
		if err != nil {
			b.Fatalf("SearchBodyTrigramCandidates: %v", err)
		}
	}
}

// BenchmarkEvictBodyIndex_5kCorpus covers spec 35 §14 row 9.
// budget <500 ms p95 reducing 5 000 → 4 500.
func BenchmarkEvictBodyIndex_5kCorpus(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s := openBodyIndexBenchStore(b)
		acc, fld := benchSeedRoot(b, s)
		seedCorpus(b, s, acc, fld, "ev", 5_000, 4*1024)
		ctx := context.Background()
		b.StartTimer()
		if _, err := s.EvictBodyIndex(ctx, EvictBodyIndexOpts{MaxCount: 4_500}); err != nil {
			b.Fatalf("EvictBodyIndex: %v", err)
		}
	}
}

// openBodyIndexBenchStore opens a tmpdir DB for a single bench function.
func openBodyIndexBenchStore(b *testing.B) Store {
	b.Helper()
	dir := b.TempDir()
	s, err := Open(dir+"/mail.db", DefaultOptions())
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = s.Close() })
	return s
}

// benchSeedRoot seeds the account + folder needed by every bench.
func benchSeedRoot(b *testing.B, s Store) (int64, string) {
	b.Helper()
	ctx := context.Background()
	id, err := s.PutAccount(ctx, Account{TenantID: "t", ClientID: "c", UPN: "tester@example.invalid"})
	if err != nil {
		b.Fatalf("PutAccount: %v", err)
	}
	f := Folder{ID: "folder-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now()}
	if err := s.UpsertFolder(ctx, f); err != nil {
		b.Fatalf("UpsertFolder: %v", err)
	}
	return id, f.ID
}
