package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddBundledSenderIdempotent(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	require.NoError(t, s.AddBundledSender(ctx, acc, "news@acme.com"))
	// Second add must not error and must not produce a duplicate row.
	require.NoError(t, s.AddBundledSender(ctx, acc, "news@acme.com"))

	rows, err := s.ListBundledSenders(ctx, acc)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "news@acme.com", rows[0].Address)
}

func TestRemoveBundledSenderNoop(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	// Remove of a non-existent row must not error.
	require.NoError(t, s.RemoveBundledSender(ctx, acc, "ghost@example.invalid"))

	require.NoError(t, s.AddBundledSender(ctx, acc, "x@y.com"))
	require.NoError(t, s.RemoveBundledSender(ctx, acc, "x@y.com"))

	bundled, err := s.IsSenderBundled(ctx, acc, "x@y.com")
	require.NoError(t, err)
	require.False(t, bundled)
}

func TestListBundledSendersOrder(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	// Direct INSERT with explicit added_at so the order test is
	// independent of wall-clock granularity (Add uses time.Now().Unix(),
	// which collapses fast-back-to-back inserts to the same second).
	concrete, ok := s.(*store)
	require.True(t, ok)
	_, err := concrete.db.ExecContext(ctx,
		`INSERT INTO bundled_senders (account_id, address, added_at) VALUES
		 (?, 'older@a.com', 100),
		 (?, 'newer@a.com', 200),
		 (?, 'amid@a.com',  200)`,
		acc, acc, acc)
	require.NoError(t, err)

	rows, err := s.ListBundledSenders(ctx, acc)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	// added_at DESC: 200, 200, 100. Within the 200 group, address ASC:
	// amid → newer. older@a.com (added_at=100) comes last.
	require.Equal(t, "amid@a.com", rows[0].Address)
	require.Equal(t, "newer@a.com", rows[1].Address)
	require.Equal(t, "older@a.com", rows[2].Address)
}

func TestIsSenderBundledMixedCaseInput(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	require.NoError(t, s.AddBundledSender(ctx, acc, "BOB@A.COM"))

	// Stored as lowercase.
	rows, err := s.ListBundledSenders(ctx, acc)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "bob@a.com", rows[0].Address)

	// Lookup with mixed-case input still finds the row.
	got, err := s.IsSenderBundled(ctx, acc, "Bob@A.com")
	require.NoError(t, err)
	require.True(t, got)
}

func TestBundledSendersAccountFKCascade(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	require.NoError(t, s.AddBundledSender(ctx, acc, "n@a.com"))

	// Delete the account row (FK cascade should clear bundled_senders).
	concrete, ok := s.(*store)
	require.True(t, ok)
	_, err := concrete.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, acc)
	require.NoError(t, err)

	rows, err := s.ListBundledSenders(ctx, acc)
	require.NoError(t, err)
	require.Empty(t, rows, "FK cascade must clear bundled_senders on account delete")
}

func BenchmarkBundleAddRemove(b *testing.B) {
	s := OpenTestStore(b)
	acc := SeedAccount(b, s)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.AddBundledSender(ctx, acc, "bench@a.com"); err != nil {
			b.Fatal(err)
		}
		if err := s.RemoveBundledSender(ctx, acc, "bench@a.com"); err != nil {
			b.Fatal(err)
		}
	}

	elapsed := b.Elapsed()
	avgMs := float64(elapsed.Microseconds()) / float64(b.N) / 1000.0
	const budgetMs = 1
	if avgMs > budgetMs {
		b.Errorf("BenchmarkBundleAddRemove: avg %.3fms exceeds %dms budget", avgMs, budgetMs)
	}
}

func BenchmarkListBundledSenders(b *testing.B) {
	s := OpenTestStore(b)
	acc := SeedAccount(b, s)
	ctx := context.Background()

	for i := 0; i < 500; i++ {
		addr := "bench" + addrSuffix(i) + "@example.invalid"
		if err := s.AddBundledSender(ctx, acc, addr); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := s.ListBundledSenders(ctx, acc)
		if err != nil {
			b.Fatal(err)
		}
		_ = rows
	}

	elapsed := b.Elapsed()
	avgMs := float64(elapsed.Microseconds()) / float64(b.N) / 1000.0
	const budgetMs = 2
	if avgMs > budgetMs {
		b.Errorf("BenchmarkListBundledSenders: avg %.3fms exceeds %dms budget", avgMs, budgetMs)
	}
}

// addrSuffix turns an int into a short stable suffix without
// pulling in fmt/strconv allocations on the bench hot path.
func addrSuffix(i int) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if i == 0 {
		return "0"
	}
	var buf [16]byte
	n := 0
	for i > 0 {
		buf[n] = digits[i%36]
		i /= 36
		n++
	}
	out := make([]byte, n)
	for k := 0; k < n; k++ {
		out[k] = buf[n-1-k]
	}
	return string(out)
}
