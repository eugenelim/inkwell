package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func putTab(t *testing.T, s Store, accountID int64, name string) int64 {
	t.Helper()
	require.NoError(t, s.PutSavedSearch(context.Background(), SavedSearch{
		AccountID: accountID, Name: name, Pattern: "~A",
	}))
	rows, err := s.ListSavedSearches(context.Background(), accountID)
	require.NoError(t, err)
	for _, r := range rows {
		if r.Name == name {
			return r.ID
		}
	}
	t.Fatalf("saved search %q not found", name)
	return 0
}

func TestMigration012AppliesCleanly(t *testing.T) {
	s := OpenTestStore(t)
	st := s.(*store)
	ctx := context.Background()
	var ver string
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT value FROM schema_meta WHERE key = 'version'`).Scan(&ver))
	require.Equal(t, "12", strings.TrimSpace(ver))

	// tab_order column exists and is NULL for new rows.
	acc := SeedAccount(t, s)
	id := putTab(t, s, acc, "x")
	var tabOrder *int
	err := st.db.QueryRowContext(ctx,
		`SELECT tab_order FROM saved_searches WHERE id = ?`, id).Scan(&tabOrder)
	require.NoError(t, err)
	require.Nil(t, tabOrder, "fresh row's tab_order must be NULL")

	// Partial UNIQUE index is present.
	var idx string
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_saved_searches_tab_order'`).Scan(&idx))
}

func TestPutSavedSearchDoesNotTouchTabOrder(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	id := putTab(t, s, acc, "n1")
	zero := 0
	require.NoError(t, s.SetTabOrder(ctx, id, &zero))

	// Re-upsert via PutSavedSearch — tab_order must NOT be reset.
	require.NoError(t, s.PutSavedSearch(ctx, SavedSearch{
		AccountID: acc, Name: "n1", Pattern: "~F",
	}))
	rows, err := s.ListTabs(ctx, acc)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].TabOrder)
	require.Equal(t, 0, *rows[0].TabOrder)
}

func TestListTabsOrdered(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	a := putTab(t, s, acc, "a")
	b := putTab(t, s, acc, "b")
	c := putTab(t, s, acc, "c")
	require.NoError(t, s.ApplyTabOrder(ctx, acc, []int64{c, a, b}))
	rows, err := s.ListTabs(ctx, acc)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, "c", rows[0].Name)
	require.Equal(t, "a", rows[1].Name)
	require.Equal(t, "b", rows[2].Name)
	require.Equal(t, 0, *rows[0].TabOrder)
	require.Equal(t, 1, *rows[1].TabOrder)
	require.Equal(t, 2, *rows[2].TabOrder)
}

func TestSetTabOrderNullDemotes(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	id := putTab(t, s, acc, "x")
	zero := 0
	require.NoError(t, s.SetTabOrder(ctx, id, &zero))
	require.NoError(t, s.SetTabOrder(ctx, id, nil))
	rows, err := s.ListTabs(ctx, acc)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestReindexTabsDense(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	a := putTab(t, s, acc, "a")
	b := putTab(t, s, acc, "b")
	c := putTab(t, s, acc, "c")
	// Apply a sparse layout via direct SetTabOrder.
	o0, o2, o5 := 0, 2, 5
	require.NoError(t, s.SetTabOrder(ctx, a, &o0))
	require.NoError(t, s.SetTabOrder(ctx, b, &o2))
	require.NoError(t, s.SetTabOrder(ctx, c, &o5))
	require.NoError(t, s.ReindexTabs(ctx, acc))
	rows, err := s.ListTabs(ctx, acc)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, 0, *rows[0].TabOrder)
	require.Equal(t, 1, *rows[1].TabOrder)
	require.Equal(t, 2, *rows[2].TabOrder)
	require.Equal(t, "a", rows[0].Name)
	require.Equal(t, "b", rows[1].Name)
	require.Equal(t, "c", rows[2].Name)
}

func TestApplyTabOrderDemotesOmitted(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	a := putTab(t, s, acc, "a")
	b := putTab(t, s, acc, "b")
	c := putTab(t, s, acc, "c")
	require.NoError(t, s.ApplyTabOrder(ctx, acc, []int64{a, b, c}))
	// Now drop b.
	require.NoError(t, s.ApplyTabOrder(ctx, acc, []int64{a, c}))
	rows, err := s.ListTabs(ctx, acc)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "a", rows[0].Name)
	require.Equal(t, "c", rows[1].Name)
}

func TestApplyTabOrderRejectsForeignAccount(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	id := putTab(t, s, acc, "x")
	err := s.ApplyTabOrder(ctx, acc+99, []int64{id})
	require.Error(t, err)
}

func TestPromoteUniquePartialIndex(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	a := putTab(t, s, acc, "a")
	b := putTab(t, s, acc, "b")
	zero := 0
	require.NoError(t, s.SetTabOrder(ctx, a, &zero))
	// Direct duplicate write must hit the partial UNIQUE.
	err := s.SetTabOrder(ctx, b, &zero)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "unique")
}

func TestSavedSearchDeleteDropsTab(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	a := putTab(t, s, acc, "a")
	b := putTab(t, s, acc, "b")
	require.NoError(t, s.ApplyTabOrder(ctx, acc, []int64{a, b}))
	require.NoError(t, s.DeleteSavedSearch(ctx, a))
	require.NoError(t, s.ReindexTabs(ctx, acc))
	rows, err := s.ListTabs(ctx, acc)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "b", rows[0].Name)
	require.Equal(t, 0, *rows[0].TabOrder)
}

func TestCountUnreadByIDs(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		m := SyntheticMessage(acc, folder.ID, i, time.Now())
		m.IsRead = i%2 == 0
		require.NoError(t, s.UpsertMessage(ctx, m))
	}
	ids := []string{"msg-0", "msg-1", "msg-2", "msg-3", "msg-4"}
	n, err := s.CountUnreadByIDs(ctx, acc, ids)
	require.NoError(t, err)
	// is_read = false for offsets 1, 3 (odd) → 2 unread
	require.Equal(t, 2, n)

	// Empty set returns 0.
	n, err = s.CountUnreadByIDs(ctx, acc, nil)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}
