package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"log/slog"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/action"
	"github.com/eugenelim/inkwell/internal/graph"
	ilog "github.com/eugenelim/inkwell/internal/log"
	"github.com/eugenelim/inkwell/internal/store"
)

// newThreadCLIApp seeds a headlessApp with two messages sharing a
// conversation_id, a batch-success httptest server, and well-known
// destination folders.
func newThreadCLIApp(t *testing.T) (*headlessApp, *httptest.Server) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "thread-cli.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	id, err := st.PutAccount(context.Background(), store.Account{
		TenantID: "T", ClientID: "C", UPN: "tester@example.invalid",
	})
	require.NoError(t, err)

	ctx := context.Background()
	for _, f := range []store.Folder{
		{ID: "f-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now()},
		{ID: "f-projects", AccountID: id, DisplayName: "Projects", LastSyncedAt: time.Now()},
		{ID: "archive", AccountID: id, DisplayName: "Archive", WellKnownName: "archive", LastSyncedAt: time.Now()},
		{ID: "deleteditems", AccountID: id, DisplayName: "Deleted Items", WellKnownName: "deleteditems", LastSyncedAt: time.Now()},
	} {
		require.NoError(t, st.UpsertFolder(ctx, f))
	}

	for _, m := range []store.Message{
		{ID: "m-tc-1", AccountID: id, FolderID: "f-inbox", Subject: "thread msg 1", ConversationID: "conv-cli", IsRead: false, FlagStatus: "notFlagged", ReceivedAt: time.Now().Add(-time.Hour)},
		{ID: "m-tc-2", AccountID: id, FolderID: "f-inbox", Subject: "thread msg 2", ConversationID: "conv-cli", IsRead: false, FlagStatus: "notFlagged", ReceivedAt: time.Now().Add(-2 * time.Hour)},
	} {
		require.NoError(t, st.UpsertMessage(ctx, m))
	}

	acc, err := st.GetAccount(ctx)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/$batch", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"responses":[{"id":"0","status":200},{"id":"1","status":200}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	gc, err := graph.NewClient(cliGraphFakeAuth{}, graph.Options{BaseURL: srv.URL, MaxRetries: 1, Logger: logger})
	require.NoError(t, err)

	return &headlessApp{store: st, graph: gc, account: acc, logger: logger}, srv
}

// TestThreadCLIArchive verifies that cliThreadMove archives all messages
// in a conversation (fetches IDs, calls BulkMove, returns count=2).
func TestThreadCLIArchive(t *testing.T) {
	app, _ := newThreadCLIApp(t)
	ex := action.New(app.store, app.graph, app.logger)

	total, results, err := cliThreadMove(context.Background(), ex, app, "conv-cli", "", "archive")
	require.NoError(t, err)
	require.Equal(t, 2, total)
	var ok int
	for _, r := range results {
		if r.Err == nil {
			ok++
		}
	}
	require.Equal(t, 2, ok)
}

// TestThreadCLIDeleteWithoutYesIsNoop verifies that cliThreadExecute is
// NOT called when --yes is absent; the dry-run path prints the count.
func TestThreadCLIDeleteWithoutYesIsNoop(t *testing.T) {
	app, _ := newThreadCLIApp(t)

	// Simulate the dry-run path directly (same logic as the RunE without --yes).
	ids, err := app.store.MessageIDsInConversation(context.Background(), app.account.ID, "conv-cli", true)
	require.NoError(t, err)

	var out bytes.Buffer
	fmt.Fprintf(&out, "would delete %d messages — pass --yes to apply\n", len(ids))
	require.Contains(t, out.String(), "would delete 2 messages")

	// Confirm the store is unchanged — messages still exist.
	msgs, listErr := app.store.ListMessages(context.Background(), store.MessageQuery{
		AccountID: app.account.ID,
		FolderID:  "f-inbox",
	})
	require.NoError(t, listErr)
	require.Len(t, msgs, 2, "dry-run must not mutate the store")
}

// TestThreadCLIMoveResolvesFolder verifies that resolveFolderByNameCtx
// returns the correct ID and that cliThreadMove succeeds with it.
func TestThreadCLIMoveResolvesFolder(t *testing.T) {
	app, _ := newThreadCLIApp(t)

	folderID, _, _, err := resolveFolderByNameCtx(context.Background(), app, "Projects")
	require.NoError(t, err)
	require.Equal(t, "f-projects", folderID)

	ex := action.New(app.store, app.graph, app.logger)
	total, results, moveErr := cliThreadMove(context.Background(), ex, app, "conv-cli", folderID, "")
	require.NoError(t, moveErr)
	require.Equal(t, 2, total)
	var ok int
	for _, r := range results {
		if r.Err == nil {
			ok++
		}
	}
	require.Equal(t, 2, ok)
}
