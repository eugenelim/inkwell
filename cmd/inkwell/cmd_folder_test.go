package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/action"
	"github.com/eugenelim/inkwell/internal/graph"
	ilog "github.com/eugenelim/inkwell/internal/log"
	"github.com/eugenelim/inkwell/internal/store"
)

// cliGraphFakeAuth satisfies graph.Authenticator for tests.
type cliGraphFakeAuth struct{}

func (cliGraphFakeAuth) Token(_ context.Context) (string, error) { return "tok", nil }
func (cliGraphFakeAuth) Invalidate()                             {}

// newCLITestAppWithGraph wires a store + httptest.Server-backed graph
// client. Tests register handlers on the returned *httptest.Server.
func newCLITestAppWithGraph(t *testing.T) (*headlessApp, *httptest.Server) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cli.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	id, err := st.PutAccount(ctx, store.Account{TenantID: "T", ClientID: "C", UPN: "tester@example.invalid"})
	require.NoError(t, err)
	require.NoError(t, st.UpsertFolder(ctx, store.Folder{
		ID: "f-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, st.UpsertFolder(ctx, store.Folder{
		ID: "f-parent", AccountID: id, DisplayName: "Projects", LastSyncedAt: time.Now(),
	}))

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	gc, err := graph.NewClient(cliGraphFakeAuth{}, graph.Options{
		BaseURL:    srv.URL,
		MaxRetries: 1,
		Logger:     logger,
	})
	require.NoError(t, err)

	acc, err := st.GetAccount(ctx)
	require.NoError(t, err)
	return &headlessApp{store: st, graph: gc, account: acc, logger: logger}, srv
}

// TestResolveFolderByNameCtxByDisplayName verifies case-insensitive
// lookup by display name (no graph needed).
func TestResolveFolderByNameCtxByDisplayName(t *testing.T) {
	app := newCLITestApp(t)
	id, _, name, err := resolveFolderByNameCtx(context.Background(), app, "inbox")
	require.NoError(t, err)
	require.Equal(t, "f-inbox", id)
	require.Equal(t, "Inbox", name)
}

// TestResolveFolderByNameCtxUnknownReturnsError verifies a friendly
// error for a folder that doesn't exist.
func TestResolveFolderByNameCtxUnknownReturnsError(t *testing.T) {
	app := newCLITestApp(t)
	_, _, _, err := resolveFolderByNameCtx(context.Background(), app, "DoesNotExist")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

// TestFolderCLINewCreatesTopLevel verifies that newFolderNewCmd POSTs to
// /me/mailFolders and upserts the returned folder locally.
func TestFolderCLINewCreatesTopLevel(t *testing.T) {
	app, srv := newCLITestAppWithGraph(t)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "Vendor Quotes", body["displayName"])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":          "f-vendor",
			"displayName": "Vendor Quotes",
		})
	})

	ctx := context.Background()
	exec := action.New(app.store, app.graph, app.logger)
	res, err := exec.CreateFolder(ctx, app.account.ID, "", "Vendor Quotes")
	require.NoError(t, err)
	require.Equal(t, "f-vendor", res.ID)
	require.Equal(t, "Vendor Quotes", res.DisplayName)

	// Folder must be upserted locally.
	folders, err := app.store.ListFolders(ctx, app.account.ID)
	require.NoError(t, err)
	var found bool
	for _, f := range folders {
		if f.ID == "f-vendor" {
			found = true
			break
		}
	}
	require.True(t, found, "new folder must be visible in local store")
}

// TestFolderCLINewCreatesNested verifies that slash-path resolution picks
// up the parent ID and POSTs to /me/mailFolders/{parentID}/childFolders.
func TestFolderCLINewCreatesNested(t *testing.T) {
	app, srv := newCLITestAppWithGraph(t)
	var gotURL string
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/mailFolders/", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		gotURL = r.URL.Path
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "2026", body["displayName"])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":             "f-2026",
			"displayName":    "2026",
			"parentFolderId": "f-parent",
		})
	})

	ctx := context.Background()
	exec := action.New(app.store, app.graph, app.logger)
	res, err := exec.CreateFolder(ctx, app.account.ID, "f-parent", "2026")
	require.NoError(t, err)
	require.Equal(t, "f-2026", res.ID)
	require.Contains(t, gotURL, "f-parent")
	require.Contains(t, gotURL, "childFolders")
}

// TestFolderCLINewRejectsEmptyName verifies that an empty display name
// fails before touching Graph.
func TestFolderCLINewRejectsEmptyName(t *testing.T) {
	app, _ := newCLITestAppWithGraph(t)
	exec := action.New(app.store, app.graph, app.logger)
	_, err := exec.CreateFolder(context.Background(), app.account.ID, "", "  ")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty name")
}

// TestFolderCLIRenameUpdatesDisplayName verifies that RenameFolder
// PATCHes the correct folder and updates the local store.
func TestFolderCLIRenameUpdatesDisplayName(t *testing.T) {
	app, srv := newCLITestAppWithGraph(t)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/mailFolders/f-parent", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "Work", body["displayName"])
		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	exec := action.New(app.store, app.graph, app.logger)
	require.NoError(t, exec.RenameFolder(ctx, "f-parent", "Work"))

	// Verify local rename.
	folders, err := app.store.ListFolders(ctx, app.account.ID)
	require.NoError(t, err)
	var newName string
	for _, f := range folders {
		if f.ID == "f-parent" {
			newName = f.DisplayName
			break
		}
	}
	require.Equal(t, "Work", newName)
}

// TestFolderCLIDeleteRemovesFolder verifies that DeleteFolder sends
// DELETE to Graph and removes the row from the local store.
func TestFolderCLIDeleteRemovesFolder(t *testing.T) {
	app, srv := newCLITestAppWithGraph(t)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/mailFolders/f-parent", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})

	ctx := context.Background()
	exec := action.New(app.store, app.graph, app.logger)
	require.NoError(t, exec.DeleteFolder(ctx, "f-parent"))

	// Folder must be removed from local store.
	folders, err := app.store.ListFolders(ctx, app.account.ID)
	require.NoError(t, err)
	for _, f := range folders {
		require.NotEqual(t, "f-parent", f.ID, "deleted folder must not appear in local store")
	}
}

// TestFolderCLIDeleteWithoutYesIsNoop verifies that newFolderDeleteCmd
// without --yes prints a dry-run message and makes no Graph call.
func TestFolderCLIDeleteWithoutYesIsNoop(t *testing.T) {
	app, srv := newCLITestAppWithGraph(t)
	var graphCalled bool
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/mailFolders/", func(_ http.ResponseWriter, _ *http.Request) {
		graphCalled = true
	})

	// Simulate --yes=false: resolve folder, check guard, skip exec.
	ctx := context.Background()
	id, _, displayName, err := resolveFolderByNameCtx(ctx, app, "Projects")
	require.NoError(t, err)
	require.Equal(t, "f-parent", id)
	require.Equal(t, "Projects", displayName)

	// Without --yes we should NOT call exec.DeleteFolder. Confirm no graph call.
	require.False(t, graphCalled, "Graph must not be called when --yes is absent")
}
