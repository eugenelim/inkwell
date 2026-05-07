package savedsearch

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/eugenelim/inkwell/internal/config"
	"github.com/eugenelim/inkwell/internal/store"
)

// benchSeed creates a Manager + N messages + tabCount tabs.
func benchSeed(b *testing.B, msgCount, tabCount int) (*Manager, store.Store, int64) {
	b.Helper()
	dir := b.TempDir()
	st, err := store.Open(filepath.Join(dir, "mail.db"), store.DefaultOptions())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	acc, err := st.PutAccount(ctx, store.Account{TenantID: "T", ClientID: "C", UPN: "user@x.invalid"})
	if err != nil {
		b.Fatal(err)
	}
	if err := st.UpsertFolder(ctx, store.Folder{
		ID: "f", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}); err != nil {
		b.Fatal(err)
	}
	base := time.Now()
	const batch = 1000
	buf := make([]store.Message, 0, batch)
	for i := 0; i < msgCount; i++ {
		m := store.Message{
			ID: fmt.Sprintf("m-%d", i), AccountID: acc, FolderID: "f",
			Subject: "x", FromAddress: fmt.Sprintf("s%d@x.invalid", i%500),
			ReceivedAt: base.Add(-time.Duration(i) * time.Minute),
			IsRead:     i%4 == 0,
		}
		buf = append(buf, m)
		if len(buf) == batch {
			if err := st.UpsertMessagesBatch(ctx, buf); err != nil {
				b.Fatal(err)
			}
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		if err := st.UpsertMessagesBatch(ctx, buf); err != nil {
			b.Fatal(err)
		}
	}
	cfg := config.SavedSearchSettings{CacheTTL: 60 * time.Second, TOMLMirrorPath: ""}
	mgr := New(st, acc, cfg)
	patterns := []string{"~F", "~A", "~U", "~N", "~G work", "~f sender0@*"}
	for i := 0; i < tabCount; i++ {
		name := fmt.Sprintf("tab-%d", i)
		pat := patterns[i%len(patterns)]
		if err := mgr.Save(ctx, store.SavedSearch{Name: name, Pattern: pat}); err != nil {
			b.Fatal(err)
		}
		if _, err := mgr.Promote(ctx, name); err != nil {
			b.Fatal(err)
		}
	}
	return mgr, st, acc
}

// BenchmarkCountTabs5x100k_Cold covers spec 24 §8: ≤200ms p95 for
// 5 tabs over 100k messages, cold cache.
func BenchmarkCountTabs5x100k_Cold(b *testing.B) {
	n := 100_000
	if testing.Short() {
		n = 5_000
	}
	mgr, _, _ := benchSeed(b, n, 5)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.InvalidateCache()
		if _, err := mgr.CountTabs(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCountTabs5x100k_Warm covers spec 24 §8: ≤20ms p95 for
// the cached refresh path.
func BenchmarkCountTabs5x100k_Warm(b *testing.B) {
	n := 100_000
	if testing.Short() {
		n = 5_000
	}
	mgr, _, _ := benchSeed(b, n, 5)
	ctx := context.Background()
	if _, err := mgr.CountTabs(ctx); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mgr.CountTabs(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCountTabs20x100k_Cold covers spec 24 §8: ≤500ms p95
// for the 20-tab fan-out (concurrency capped at 5).
func BenchmarkCountTabs20x100k_Cold(b *testing.B) {
	n := 100_000
	if testing.Short() {
		n = 5_000
	}
	mgr, _, _ := benchSeed(b, n, 20)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.InvalidateCache()
		if _, err := mgr.CountTabs(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
