package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// newCLITestApp sets up a headlessApp pointed at an in-memory store
// (no auth probe, no Graph client). Used only by the helper tests
// that don't need the network.
func newCLITestApp(t *testing.T) *headlessApp {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cli.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	id, err := st.PutAccount(context.Background(), store.Account{TenantID: "T", ClientID: "C", UPN: "tester@example.invalid"})
	require.NoError(t, err)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-projects", AccountID: id, DisplayName: "Projects", LastSyncedAt: time.Now(),
	}))
	acc, err := st.GetAccount(context.Background())
	require.NoError(t, err)
	return &headlessApp{store: st, account: acc}
}

func TestResolveFolderByWellKnownName(t *testing.T) {
	app := newCLITestApp(t)
	id, err := resolveFolder(context.Background(), app, "inbox")
	require.NoError(t, err)
	require.Equal(t, "f-inbox", id)
}

func TestResolveFolderByDisplayNameCaseInsensitive(t *testing.T) {
	app := newCLITestApp(t)
	id, err := resolveFolder(context.Background(), app, "PROJECTS")
	require.NoError(t, err)
	require.Equal(t, "f-projects", id)
}

func TestResolveFolderEmptyMeansAll(t *testing.T) {
	app := newCLITestApp(t)
	id, err := resolveFolder(context.Background(), app, "")
	require.NoError(t, err)
	require.Empty(t, id, "empty input → empty folder ID (no scope)")
}

func TestResolveFolderUnknownIsAFriendlyError(t *testing.T) {
	app := newCLITestApp(t)
	_, err := resolveFolder(context.Background(), app, "DoesNotExist")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
	require.Contains(t, err.Error(), "inkwell folders")
}

func TestRunFilterListingDesugarsPlainText(t *testing.T) {
	app := newCLITestApp(t)
	require.NoError(t, app.store.UpsertMessage(context.Background(), store.Message{
		ID: "m-1", AccountID: app.account.ID, FolderID: "f-inbox",
		Subject: "Q4 forecast", FromAddress: "alice@example.invalid",
		ReceivedAt: time.Now(),
	}))
	got, err := runFilterListing(context.Background(), app, "*forecast*", "", 50)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "m-1", got[0].ID)
}

func TestRunFilterListingScopesToFolder(t *testing.T) {
	app := newCLITestApp(t)
	require.NoError(t, app.store.UpsertMessage(context.Background(), store.Message{
		ID: "m-inbox", AccountID: app.account.ID, FolderID: "f-inbox",
		Subject: "Hi", FromAddress: "x@example.invalid", ReceivedAt: time.Now(),
	}))
	require.NoError(t, app.store.UpsertMessage(context.Background(), store.Message{
		ID: "m-other", AccountID: app.account.ID, FolderID: "f-projects",
		Subject: "Hi", FromAddress: "x@example.invalid", ReceivedAt: time.Now(),
	}))
	got, err := runFilterListing(context.Background(), app, "~B Hi", "f-inbox", 50)
	require.NoError(t, err)
	require.Len(t, got, 1, "folder scope must drop messages outside f-inbox")
	require.Equal(t, "m-inbox", got[0].ID)
}

func TestTruncCLI(t *testing.T) {
	require.Equal(t, "abc", truncCLI("abc", 10))
	require.Equal(t, "abc…", truncCLI("abcdef", 4))
}
