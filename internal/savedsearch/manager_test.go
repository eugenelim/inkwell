package savedsearch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/config"
	"github.com/eugenelim/inkwell/internal/store"
)

func testManager(t *testing.T) (*Manager, store.Store, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	accID, err := st.PutAccount(ctx, store.Account{TenantID: "T", ClientID: "C", UPN: "user@example.invalid"})
	require.NoError(t, err)
	require.NoError(t, st.UpsertFolder(ctx, store.Folder{
		ID: "f-inbox", AccountID: accID, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	cfg := config.SavedSearchSettings{
		CacheTTL:               30 * time.Second,
		SeedDefaults:           true,
		BackgroundRefreshInterval: 2 * time.Minute,
		TOMLMirrorPath:         "",
	}
	mgr := New(st, accID, cfg)
	return mgr, st, accID
}

// TestManagerCRUDRoundTrip verifies Save → List → Get → Delete.
func TestManagerCRUDRoundTrip(t *testing.T) {
	mgr, _, accID := testManager(t)
	ctx := context.Background()

	require.NoError(t, mgr.Save(ctx, store.SavedSearch{AccountID: accID, Name: "Newsletters", Pattern: "~N", Pinned: true, SortOrder: 1}))

	all, err := mgr.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "Newsletters", all[0].Name)
	require.True(t, all[0].Pinned)

	got, err := mgr.Get(ctx, "Newsletters")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "Newsletters", got.Name)

	require.NoError(t, mgr.Delete(ctx, all[0].ID))
	all2, err := mgr.List(ctx)
	require.NoError(t, err)
	require.Len(t, all2, 0)
}

// TestManagerGetMiss returns nil without error for an unknown name.
func TestManagerGetMiss(t *testing.T) {
	mgr, _, _ := testManager(t)
	got, err := mgr.Get(context.Background(), "nonexistent")
	require.NoError(t, err)
	require.Nil(t, got)
}

// TestManagerDeleteByName removes by name and errors on missing.
func TestManagerDeleteByName(t *testing.T) {
	mgr, _, accID := testManager(t)
	ctx := context.Background()
	require.NoError(t, mgr.Save(ctx, store.SavedSearch{AccountID: accID, Name: "Unread", Pattern: "~N", Pinned: true}))

	require.NoError(t, mgr.DeleteByName(ctx, "Unread"))
	require.Error(t, mgr.DeleteByName(ctx, "Unread"), "deleting missing name must error")
}

// TestManagerInvalidPatternRejected verifies Save validates syntax.
func TestManagerInvalidPatternRejected(t *testing.T) {
	mgr, _, accID := testManager(t)
	err := mgr.Save(context.Background(), store.SavedSearch{AccountID: accID, Name: "Bad", Pattern: "~?"})
	require.Error(t, err, "invalid pattern must be rejected")
}

// TestManagerEvaluateCacheHit verifies that a second Evaluate within
// CacheTTL uses the cached result.
func TestManagerEvaluateCacheHit(t *testing.T) {
	mgr, st, accID := testManager(t)
	ctx := context.Background()

	// Seed a message so the pattern matches.
	require.NoError(t, st.UpsertMessage(ctx, store.Message{
		ID: "m-1", AccountID: accID, FolderID: "f-inbox",
		Subject: "hello", IsRead: false,
	}))
	require.NoError(t, mgr.Save(ctx, store.SavedSearch{AccountID: accID, Name: "Unread", Pattern: "~N", Pinned: true}))

	r1, err := mgr.Evaluate(ctx, "Unread", false)
	require.NoError(t, err)
	require.False(t, r1.FromCache)
	require.Equal(t, 1, r1.Count)

	r2, err := mgr.Evaluate(ctx, "Unread", false)
	require.NoError(t, err)
	require.True(t, r2.FromCache, "second call within TTL must be from cache")
	require.Equal(t, r1.Count, r2.Count)
}

// TestManagerEvaluateForceBypasesCache verifies force=true re-evaluates.
func TestManagerEvaluateForceBypassesCache(t *testing.T) {
	mgr, _, accID := testManager(t)
	ctx := context.Background()
	require.NoError(t, mgr.Save(ctx, store.SavedSearch{AccountID: accID, Name: "Flagged", Pattern: "~F", Pinned: true}))

	r1, err := mgr.Evaluate(ctx, "Flagged", false)
	require.NoError(t, err)
	require.False(t, r1.FromCache)

	r2, err := mgr.Evaluate(ctx, "Flagged", true)
	require.NoError(t, err)
	require.False(t, r2.FromCache, "force=true must bypass cache")
}

// TestManagerSeedDefaultsNoOpWhenNotEmpty verifies SeedDefaults
// doesn't overwrite existing data.
func TestManagerSeedDefaultsNoOpWhenNotEmpty(t *testing.T) {
	mgr, _, accID := testManager(t)
	ctx := context.Background()
	require.NoError(t, mgr.Save(ctx, store.SavedSearch{AccountID: accID, Name: "Custom", Pattern: "~B foo", Pinned: false}))

	require.NoError(t, mgr.SeedDefaults(ctx, "user@example.invalid"))

	all, err := mgr.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "SeedDefaults must not add rows when table is non-empty")
	require.Equal(t, "Custom", all[0].Name)
}

// TestManagerSeedDefaultsOnFirstLaunch verifies the three seed rows
// are created when the table is empty.
func TestManagerSeedDefaultsOnFirstLaunch(t *testing.T) {
	mgr, _, _ := testManager(t)
	ctx := context.Background()

	require.NoError(t, mgr.SeedDefaults(ctx, "me@example.invalid"))

	all, err := mgr.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 3)

	names := make(map[string]bool)
	for _, s := range all {
		names[s.Name] = true
	}
	require.True(t, names["Unread"])
	require.True(t, names["Flagged"])
	require.True(t, names["From me"])
}

// TestManagerTOMLMirrorWrites verifies a non-empty mirror file is
// created after Save and its content round-trips.
func TestManagerTOMLMirrorWrites(t *testing.T) {
	mgr, _, accID := testManager(t)
	mirrorPath := filepath.Join(t.TempDir(), "saved_searches.toml")
	mgr.cfg.TOMLMirrorPath = mirrorPath
	ctx := context.Background()

	require.NoError(t, mgr.Save(ctx, store.SavedSearch{AccountID: accID, Name: "Newsletters", Pattern: "~f newsletter@*", Pinned: true, SortOrder: 1}))

	data, err := os.ReadFile(mirrorPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "[[search]]")
	require.Contains(t, string(data), "Newsletters")
	require.Contains(t, string(data), "newsletter@*")
}

// TestManagerCountPinnedSkipsUnpinned verifies CountPinned only evaluates
// pinned searches.
func TestManagerCountPinnedSkipsUnpinned(t *testing.T) {
	mgr, _, accID := testManager(t)
	ctx := context.Background()
	require.NoError(t, mgr.Save(ctx, store.SavedSearch{AccountID: accID, Name: "Pinned", Pattern: "~N", Pinned: true}))
	require.NoError(t, mgr.Save(ctx, store.SavedSearch{AccountID: accID, Name: "Unpinned", Pattern: "~F", Pinned: false}))

	counts, err := mgr.CountPinned(ctx)
	require.NoError(t, err)

	all, _ := mgr.List(ctx)
	var pinnedID, unpinnedID int64
	for _, s := range all {
		if s.Name == "Pinned" {
			pinnedID = s.ID
		} else {
			unpinnedID = s.ID
		}
	}
	_, hasPinned := counts[pinnedID]
	_, hasUnpinned := counts[unpinnedID]
	require.True(t, hasPinned, "pinned search must appear in CountPinned result")
	require.False(t, hasUnpinned, "unpinned search must NOT appear in CountPinned result")
}
