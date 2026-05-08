package pattern

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

func TestParseRoutingOperator(t *testing.T) {
	for _, src := range []string{"~o imbox", "~o feed", "~o paper_trail", "~o screener", "~o none"} {
		got, err := Parse(src)
		require.NoError(t, err, "src=%q", src)
		pred, ok := got.(Predicate)
		require.True(t, ok)
		require.Equal(t, FieldRouting, pred.Field, "src=%q", src)
		rv, ok := pred.Value.(RoutingValue)
		require.True(t, ok)
		require.Equal(t, strings.TrimPrefix(src, "~o "), rv.Destination)
	}
}

func TestParseRoutingRejectsUnknown(t *testing.T) {
	_, err := Parse("~o foo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown routing destination")

	// Hyphenated form is rejected — only underscore form accepted.
	_, err = Parse("~o paper-trail")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown routing destination")
}

func TestCompileRoutingOperatorLocalOnly(t *testing.T) {
	c, err := Compile("~o feed", CompileOptions{})
	require.NoError(t, err)
	require.Equal(t, StrategyLocalOnly, c.Strategy)
	require.Contains(t, c.Plan.LocalSQL, "EXISTS")
	require.Contains(t, c.Plan.LocalSQL, "sender_routing")
	require.Contains(t, c.Plan.LocalSQL, "lower(trim(from_address))")
	require.Equal(t, []any{"feed"}, c.Plan.LocalArgs)
}

func TestCompileRoutingNoneEmitsNotExists(t *testing.T) {
	c, err := Compile("~o none", CompileOptions{})
	require.NoError(t, err)
	require.Equal(t, StrategyLocalOnly, c.Strategy)
	require.Contains(t, c.Plan.LocalSQL, "NOT EXISTS")
	require.NotContains(t, c.Plan.LocalSQL, "destination")
}

func TestCompileRoutingOperatorTwoStage(t *testing.T) {
	// ~o + body operator → server $search runs for the body, local
	// refinement runs for ~o.
	c, err := Compile(`~o feed & ~B "unsubscribe"`, CompileOptions{})
	require.NoError(t, err)
	require.Equal(t, StrategyTwoStage, c.Strategy)
	require.NotEmpty(t, c.Plan.GraphSearch)
	require.NotContains(t, c.Plan.GraphSearch, "destination",
		"server search must not contain the routing predicate")
}

func TestCompileRoutingOperatorRejectedByFilterAndSearch(t *testing.T) {
	root, err := Parse("~o feed")
	require.NoError(t, err)
	_, err = EmitFilter(root)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsupported))
	_, err = EmitSearch(root)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsupported))
}

// BenchmarkPatternRoutingOperator covers spec 23 §9: Compile +
// Execute for `~o feed` over a 100k-message store seeded with 500
// routed senders. Budget ≤10ms p95.
func BenchmarkPatternRoutingOperator(b *testing.B) {
	n := 100_000
	if testing.Short() {
		n = 5_000
	}
	path := filepath.Join(b.TempDir(), "mail.db")
	st, err := store.Open(path, store.DefaultOptions())
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	acc, err := st.PutAccount(ctx, store.Account{TenantID: "t", ClientID: "c", UPN: "u@x.invalid"})
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
	for i := 0; i < n; i++ {
		buf = append(buf, store.Message{
			ID:          "m-" + itoaPattern(i),
			AccountID:   acc,
			FolderID:    "f",
			Subject:     "Hello",
			FromAddress: "sender" + itoaPattern(i%500) + "@x.invalid",
			ReceivedAt:  base.Add(-time.Duration(i) * time.Minute),
		})
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
	dests := []string{"imbox", "feed", "paper_trail", "screener"}
	for i := 0; i < 500; i++ {
		if _, err := st.SetSenderRouting(ctx, acc, "sender"+itoaPattern(i)+"@x.invalid", dests[i%4]); err != nil {
			b.Fatal(err)
		}
	}
	c, err := Compile("~o feed", CompileOptions{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Execute(ctx, c, st, nil, ExecuteOptions{AccountID: acc, LocalMatchLimit: 200}); err != nil {
			b.Fatal(err)
		}
	}
}

func itoaPattern(n int) string {
	return strings.Repeat("", 0) + intStr(n)
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestExecuteRoutingOperatorIntegration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()
	acc, err := st.PutAccount(ctx, store.Account{TenantID: "t", ClientID: "c", UPN: "u@x.invalid"})
	require.NoError(t, err)
	require.NoError(t, st.UpsertFolder(ctx, store.Folder{
		ID: "f", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	for i, addr := range []string{"news@x.invalid", "news@x.invalid", "alice@x.invalid"} {
		require.NoError(t, st.UpsertMessage(ctx, store.Message{
			ID:          "m-" + string(rune('0'+i)),
			AccountID:   acc,
			FolderID:    "f",
			Subject:     "Hello",
			FromAddress: addr,
			ReceivedAt:  time.Now().Add(-time.Duration(i) * time.Minute),
		}))
	}
	_, err = st.SetSenderRouting(ctx, acc, "news@x.invalid", "feed")
	require.NoError(t, err)

	c, err := Compile("~o feed", CompileOptions{})
	require.NoError(t, err)
	ids, err := Execute(ctx, c, st, nil, ExecuteOptions{AccountID: acc, LocalMatchLimit: 100})
	require.NoError(t, err)
	require.Len(t, ids, 2, "two routed-to-feed messages")

	c, err = Compile("~o none", CompileOptions{})
	require.NoError(t, err)
	ids, err = Execute(ctx, c, st, nil, ExecuteOptions{AccountID: acc, LocalMatchLimit: 100})
	require.NoError(t, err)
	require.Len(t, ids, 1, "one unrouted message (alice)")
}

func TestRoutingOperatorNegationVsNone(t *testing.T) {
	// ! ~o feed compiles to the negation of EXISTS-feed (matches
	// unrouted AND non-feed destinations); ~o none compiles to the
	// NOT EXISTS sentinel form (matches unrouted only). Distinct
	// shapes — verified by inspecting the LocalSQL.
	notFeed, err := Compile("!~o feed", CompileOptions{})
	require.NoError(t, err)
	noneOnly, err := Compile("~o none", CompileOptions{})
	require.NoError(t, err)

	require.Contains(t, notFeed.Plan.LocalSQL, "destination")
	require.NotContains(t, noneOnly.Plan.LocalSQL, "destination")
}
