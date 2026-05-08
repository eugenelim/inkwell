package savedsearch

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

func filepath_join(parts ...string) string { return filepath.Join(parts...) }
func readFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func seedSearch(t *testing.T, mgr *Manager, name, pat string) {
	t.Helper()
	require.NoError(t, mgr.Save(context.Background(), store.SavedSearch{
		Name: name, Pattern: pat, Pinned: true,
	}))
}

func TestPromoteAppendsAtEnd(t *testing.T) {
	mgr, _, _ := testManager(t)
	seedSearch(t, mgr, "Newsletters", "~F")
	seedSearch(t, mgr, "VIP", "~A")
	ctx := context.Background()
	o, err := mgr.Promote(ctx, "Newsletters")
	require.NoError(t, err)
	require.Equal(t, 0, o)
	o, err = mgr.Promote(ctx, "VIP")
	require.NoError(t, err)
	require.Equal(t, 1, o)
	tabs, err := mgr.Tabs(ctx)
	require.NoError(t, err)
	require.Len(t, tabs, 2)
	require.Equal(t, "Newsletters", tabs[0].Name)
	require.Equal(t, "VIP", tabs[1].Name)
}

func TestPromoteIdempotent(t *testing.T) {
	mgr, _, _ := testManager(t)
	seedSearch(t, mgr, "Newsletters", "~F")
	ctx := context.Background()
	o, err := mgr.Promote(ctx, "Newsletters")
	require.NoError(t, err)
	require.Equal(t, 0, o)
	o2, err := mgr.Promote(ctx, "Newsletters")
	require.NoError(t, err)
	require.Equal(t, 0, o2, "second promote of same tab returns same order")
}

func TestPromoteUnknownErrors(t *testing.T) {
	mgr, _, _ := testManager(t)
	_, err := mgr.Promote(context.Background(), "DoesNotExist")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no saved search named")
}

func TestDemote(t *testing.T) {
	mgr, _, _ := testManager(t)
	seedSearch(t, mgr, "a", "~F")
	seedSearch(t, mgr, "b", "~A")
	seedSearch(t, mgr, "c", "~U")
	ctx := context.Background()
	for _, n := range []string{"a", "b", "c"} {
		_, err := mgr.Promote(ctx, n)
		require.NoError(t, err)
	}
	require.NoError(t, mgr.Demote(ctx, "b"))
	tabs, err := mgr.Tabs(ctx)
	require.NoError(t, err)
	require.Len(t, tabs, 2)
	require.Equal(t, "a", tabs[0].Name)
	require.Equal(t, "c", tabs[1].Name)
	require.Equal(t, 0, *tabs[0].TabOrder)
	require.Equal(t, 1, *tabs[1].TabOrder)
}

func TestDemoteUnknownNoOp(t *testing.T) {
	mgr, _, _ := testManager(t)
	seedSearch(t, mgr, "a", "~F")
	ctx := context.Background()
	require.NoError(t, mgr.Demote(ctx, "a"), "demote unrouted is no-op")
}

func TestReorder(t *testing.T) {
	mgr, _, _ := testManager(t)
	for _, n := range []string{"a", "b", "c"} {
		seedSearch(t, mgr, n, "~F")
	}
	ctx := context.Background()
	for _, n := range []string{"a", "b", "c"} {
		_, _ = mgr.Promote(ctx, n)
	}
	require.NoError(t, mgr.Reorder(ctx, 0, 2)) // move a to position 2 → [b, c, a]
	tabs, err := mgr.Tabs(ctx)
	require.NoError(t, err)
	require.Equal(t, "b", tabs[0].Name)
	require.Equal(t, "c", tabs[1].Name)
	require.Equal(t, "a", tabs[2].Name)
}

func TestReorderOutOfRangeError(t *testing.T) {
	mgr, _, _ := testManager(t)
	seedSearch(t, mgr, "a", "~F")
	ctx := context.Background()
	_, _ = mgr.Promote(ctx, "a")
	require.Error(t, mgr.Reorder(ctx, 5, 0))
	require.Error(t, mgr.Reorder(ctx, 0, 5))
}

func TestDeleteReindexesTabs(t *testing.T) {
	mgr, _, _ := testManager(t)
	for _, n := range []string{"a", "b", "c"} {
		seedSearch(t, mgr, n, "~F")
	}
	ctx := context.Background()
	for _, n := range []string{"a", "b", "c"} {
		_, _ = mgr.Promote(ctx, n)
	}
	require.NoError(t, mgr.DeleteByName(ctx, "b"))
	tabs, err := mgr.Tabs(ctx)
	require.NoError(t, err)
	require.Len(t, tabs, 2)
	require.Equal(t, 0, *tabs[0].TabOrder)
	require.Equal(t, 1, *tabs[1].TabOrder)
}

func TestCountTabsUnreadOnly(t *testing.T) {
	mgr, st, accID := testManager(t)
	ctx := context.Background()
	// Three messages from "alice", two unread.
	for i := 0; i < 3; i++ {
		require.NoError(t, st.UpsertMessage(ctx, store.Message{
			ID: "m-" + string(rune('1'+i)), AccountID: accID, FolderID: "f-inbox",
			FromAddress: "alice@example.invalid", Subject: "x",
			IsRead:     i == 0,
			ReceivedAt: time.Now().Add(-time.Duration(i) * time.Minute),
		}))
	}
	seedSearch(t, mgr, "From Alice", "~f alice@example.invalid")
	_, err := mgr.Promote(ctx, "From Alice")
	require.NoError(t, err)
	counts, err := mgr.CountTabs(ctx)
	require.NoError(t, err)
	tabs, _ := mgr.Tabs(ctx)
	require.Len(t, tabs, 1)
	require.Equal(t, 2, counts[tabs[0].ID])
}

func TestCountTabsParallelBoundedConcurrency(t *testing.T) {
	mgr, st, accID := testManager(t)
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		seedSearch(t, mgr, "tab-"+string(rune('a'+i)), "~A")
		_, err := mgr.Promote(ctx, "tab-"+string(rune('a'+i)))
		require.NoError(t, err)
	}
	require.NoError(t, st.UpsertMessage(ctx, store.Message{
		ID: "m-1", AccountID: accID, FolderID: "f-inbox",
		HasAttachments: true, IsRead: false, ReceivedAt: time.Now(),
	}))
	counts, err := mgr.CountTabs(ctx)
	require.NoError(t, err)
	require.Len(t, counts, 20)
}

func TestPromoteDoesNotLogName(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	mgr, _, _ := testManager(t)
	seedSearch(t, mgr, "BossEmails", "~f boss@example.invalid")
	_, err := mgr.Promote(context.Background(), "BossEmails")
	require.NoError(t, err)
	out := buf.String()
	require.NotContains(t, out, "BossEmails", "INFO logs must not include the saved-search name")
	require.Contains(t, out, "tab.promote", "promote event should still log")
}

func TestDemoteDoesNotLogName(t *testing.T) {
	mgr, _, _ := testManager(t)
	seedSearch(t, mgr, "BossEmails", "~f boss@")
	_, _ = mgr.Promote(context.Background(), "BossEmails")

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	require.NoError(t, mgr.Demote(context.Background(), "BossEmails"))
	out := buf.String()
	require.NotContains(t, out, "BossEmails")
	require.Contains(t, out, "tab.demote")
}

func TestEditSavedSearchInvalidatesEvalCache(t *testing.T) {
	mgr, _, _ := testManager(t)
	ctx := context.Background()
	seedSearch(t, mgr, "x", "~F")
	_, err := mgr.Evaluate(ctx, "x", false)
	require.NoError(t, err)
	require.NoError(t, mgr.Edit(ctx, "x", "x", "~A", true))
	mgr.mu.Lock()
	_, has := mgr.cache["x"]
	mgr.mu.Unlock()
	require.False(t, has, "Edit must drop the cache entry for the renamed/repatterned name")
}

func TestTomlMirrorRoundTripsTabOrder(t *testing.T) {
	mgr, _, _ := testManager(t)
	mgr.cfg.TOMLMirrorPath = filepath_join(t.TempDir(), "mirror.toml")
	seedSearch(t, mgr, "Newsletters", "~F")
	_, err := mgr.Promote(context.Background(), "Newsletters")
	require.NoError(t, err)
	body, err := readFile(mgr.cfg.TOMLMirrorPath)
	require.NoError(t, err)
	require.True(t, strings.Contains(body, "tab_order = 0"),
		"TOML mirror must contain tab_order = 0; got: %s", body)
}
