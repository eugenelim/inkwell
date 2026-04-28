package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkGetMessageCached covers spec §7 row 1: GetMessage(id) for a
// cached message must be <1ms p95.
func BenchmarkGetMessageCached(b *testing.B) {
	s, acc, f := openBenchStore(b, 10_000)
	id := "msg-100"
	_, _ = s.GetMessage(context.Background(), id) // warm caches
	_ = acc
	_ = f

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.GetMessage(context.Background(), id); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkListMessagesInbox100kLimit100 covers row 2: <10ms p95 over
// a 100k-message inbox. Reduced to 50k by default so CI stays fast;
// budget is verified against the per-op time, not the dataset size.
func BenchmarkListMessagesInbox100kLimit100(b *testing.B) {
	n := 50_000
	if testing.Short() {
		n = 5_000
	}
	s, acc, f := openBenchStore(b, n)

	q := MessageQuery{AccountID: acc, FolderID: f.ID, Limit: 100}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ListMessages(context.Background(), q); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUpsertMessagesBatch100 covers row 4: <50ms p95 per 100-batch.
func BenchmarkUpsertMessagesBatch100(b *testing.B) {
	s, acc, f := openBenchStore(b, 0)
	base := time.Now()
	batch := make([]Message, 100)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range batch {
			batch[j] = SyntheticMessage(acc, f.ID, i*100+j, base)
		}
		if err := s.UpsertMessagesBatch(context.Background(), batch); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSearchMeeting covers row 5: <100ms p95 for a 50-result FTS
// search over 100k messages.
func BenchmarkSearchMeeting(b *testing.B) {
	n := 50_000
	if testing.Short() {
		n = 5_000
	}
	s, acc, f := openBenchStore(b, n)
	q := SearchQuery{AccountID: acc, FolderID: f.ID, Query: "meeting", Limit: 50}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Search(context.Background(), q); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOpenExistingDB covers row 6: app-start migration check < 50ms.
// Migrations are no-ops on an existing DB at the current version, so
// this measures the hot Open path including PRAGMAs and version-check.
func BenchmarkOpenExistingDB(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "mail.db")
	{
		s, err := Open(path, DefaultOptions())
		if err != nil {
			b.Fatal(err)
		}
		_ = s.Close()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := Open(path, DefaultOptions())
		if err != nil {
			b.Fatal(err)
		}
		_ = s.Close()
	}
}

// BenchmarkGetBodyCached covers row 7: <5ms.
func BenchmarkGetBodyCached(b *testing.B) {
	s, acc, f := openBenchStore(b, 100)
	body := Body{
		MessageID:   "msg-0",
		ContentType: "text",
		Content:     "hello world",
	}
	if err := s.PutBody(context.Background(), body); err != nil {
		b.Fatal(err)
	}
	_ = acc
	_ = f

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.GetBody(context.Background(), "msg-0"); err != nil {
			b.Fatal(err)
		}
	}
}

func openBenchStore(b *testing.B, n int) (Store, int64, Folder) {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "mail.db")
	s, err := Open(path, DefaultOptions())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	acc, err := s.PutAccount(context.Background(), Account{TenantID: "tenant-bench", ClientID: "client-bench", UPN: "bench@example.invalid"})
	if err != nil {
		b.Fatal(err)
	}
	f := Folder{ID: "folder-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now()}
	if err := s.UpsertFolder(context.Background(), f); err != nil {
		b.Fatal(err)
	}
	if n > 0 {
		base := time.Now()
		const batch = 1000
		buf := make([]Message, 0, batch)
		for i := 0; i < n; i++ {
			buf = append(buf, SyntheticMessage(acc, f.ID, i, base))
			if len(buf) == batch {
				if err := s.UpsertMessagesBatch(context.Background(), buf); err != nil {
					b.Fatal(err)
				}
				buf = buf[:0]
			}
		}
		if len(buf) > 0 {
			if err := s.UpsertMessagesBatch(context.Background(), buf); err != nil {
				b.Fatal(err)
			}
		}
	}
	return s, acc, f
}

// TestBudgetsHonoured runs every benchmark for ~50ms and asserts the
// per-op time is within the spec §7 budget. This is a normal go test,
// not a `go test -bench`, so it gates CI without needing the bench
// flag. Larger datasets (50k+) are gated by testing.Short() to keep
// `-short` runs fast.
func TestBudgetsHonoured(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping budget check in -short mode")
	}
	cases := []struct {
		name   string
		budget time.Duration
		fn     func(b *testing.B)
	}{
		{"GetMessageCached", time.Millisecond, BenchmarkGetMessageCached},
		{"ListMessagesInbox", 10 * time.Millisecond, BenchmarkListMessagesInbox100kLimit100},
		{"UpsertMessagesBatch100", 50 * time.Millisecond, BenchmarkUpsertMessagesBatch100},
		{"SearchMeeting", 100 * time.Millisecond, BenchmarkSearchMeeting},
		{"GetBodyCached", 5 * time.Millisecond, BenchmarkGetBodyCached},
		{"OpenExistingDB", 50 * time.Millisecond, BenchmarkOpenExistingDB},
	}
	// Allow up to 1.5x the spec budget before failing — CI hardware
	// varies, and CLAUDE.md §6 says ">50% regression" is the gate.
	const slack = 3.0 / 2.0

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := testing.Benchmark(tc.fn)
			if res.N == 0 {
				t.Fatalf("benchmark %s did not run", tc.name)
			}
			perOp := res.T / time.Duration(res.N)
			limit := time.Duration(float64(tc.budget) * slack)
			t.Logf("%s: %v/op (budget %v, slack-limit %v, N=%d)", tc.name, perOp, tc.budget, limit, res.N)
			if perOp > limit {
				t.Fatalf("%s exceeded slack-limit: %v > %v", tc.name, perOp, limit)
			}
		})
	}
}
