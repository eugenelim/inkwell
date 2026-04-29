package action

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/graph"
	ilog "github.com/eugenelim/inkwell/internal/log"
	"github.com/eugenelim/inkwell/internal/store"
)

type fakeAuth struct{}

func (fakeAuth) Token(_ context.Context) (string, error) { return "tok", nil }
func (fakeAuth) Invalidate()                             {}

// newTestExec spins up an httptest server, an in-tmp SQLite store, and
// returns a wired Executor plus a handle to the server for handler
// installation.
func newTestExec(t *testing.T) (*Executor, store.Store, int64, *httptest.Server) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	accID, err := st.PutAccount(context.Background(), store.Account{TenantID: "T", ClientID: "C", UPN: "tester@example.invalid"})
	require.NoError(t, err)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: accID, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	// Seed destination folders the move-style tests use as targets.
	for _, dest := range []string{"deleteditems", "archive"} {
		require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
			ID: dest, AccountID: accID, DisplayName: dest, WellKnownName: dest, LastSyncedAt: time.Now(),
		}))
	}
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "m-1", AccountID: accID, FolderID: "f-inbox", Subject: "x",
		FromAddress: "a@example.invalid", IsRead: false, FlagStatus: "notFlagged",
	}))
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	// MaxRetries=1 keeps the throttle/backoff loop short in tests;
	// production wires the configured value via cmd_run.go.
	gc, err := graph.NewClient(fakeAuth{}, graph.Options{BaseURL: srv.URL, Logger: logger, MaxRetries: 1})
	require.NoError(t, err)
	exec := New(st, gc, nil)
	t.Cleanup(func() {})
	// Stash mux on the server so tests can register handlers.
	t.Cleanup(func() { srv.Close() })
	srv.Config.Handler = mux
	return exec, st, accID, srv
}

// installPatchHandler wires PATCH /me/messages/{id} to capture the
// payload and respond 200.
func installPatchHandler(t *testing.T, srv *httptest.Server, captured *atomic.Pointer[map[string]any]) {
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		captured.Store(&body)
		w.WriteHeader(http.StatusOK)
	})
}

func TestExecutorMarkReadMutatesLocalAndCallsGraph(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	var captured atomic.Pointer[map[string]any]
	installPatchHandler(t, srv, &captured)

	require.NoError(t, exec.MarkRead(context.Background(), accID, "m-1"))

	// Local message reflects the new state.
	got, err := st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	require.True(t, got.IsRead, "local IsRead must be true")

	// Graph received the canonical payload.
	body := captured.Load()
	require.NotNil(t, body)
	require.Equal(t, true, (*body)["isRead"])

	// Action persisted as Done.
	pending, err := st.PendingActions(context.Background())
	require.NoError(t, err)
	require.Len(t, pending, 0, "Done actions are not Pending")
}

func TestExecutorToggleFlagFlipsState(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	var captured atomic.Pointer[map[string]any]
	installPatchHandler(t, srv, &captured)

	require.NoError(t, exec.ToggleFlag(context.Background(), accID, "m-1", false))

	got, err := st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	require.Equal(t, "flagged", got.FlagStatus)

	body := captured.Load()
	require.NotNil(t, body)
	flag, ok := (*body)["flag"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "flagged", flag["flagStatus"])
}

func TestExecutorSoftDeleteMovesMessage(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	var moveCalls atomic.Int32
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1/move", func(w http.ResponseWriter, r *http.Request) {
		moveCalls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		// Graph accepts the well-known alias.
		require.Equal(t, "deleteditems", body["destinationId"])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "m-1-moved"})
	})

	require.NoError(t, exec.SoftDelete(context.Background(), accID, "m-1"))

	require.Equal(t, int32(1), moveCalls.Load())
	got, err := st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	// Local folder uses the REAL folder ID resolved from the well-
	// known alias, not the alias literal — otherwise the FK
	// constraint on messages.folder_id rejects the update.
	require.Equal(t, "deleteditems", got.FolderID, "test seeded folder id == alias name")
}

// TestExecutorSoftDeleteWhenDestinationIDDiffersFromAlias guards the
// real-tenant case: the user's Deleted Items folder has a real Graph
// folder ID (e.g. "AAMkA..."), the alias "deleteditems" is just a
// shortcut. Local apply must use the real ID for the FK; Graph dispatch
// can use the alias.
func TestExecutorSoftDeleteWhenDestinationIDDiffersFromAlias(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	// Replace the alias-named seed with a realistic one whose ID
	// differs from the well-known alias.
	require.NoError(t, st.DeleteFolder(context.Background(), "deleteditems"))
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "real-deleted-id-AAMkA", AccountID: accID, DisplayName: "Deleted Items",
		WellKnownName: "deleteditems", LastSyncedAt: time.Now(),
	}))

	var capturedDest atomic.Pointer[string]
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1/move", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		dest := body["destinationId"]
		capturedDest.Store(&dest)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "m-1-moved"})
	})

	require.NoError(t, exec.SoftDelete(context.Background(), accID, "m-1"))

	got, err := st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	require.Equal(t, "real-deleted-id-AAMkA", got.FolderID,
		"local folder MUST be the real folder ID resolved from well-known alias (FK)")

	// Graph received the alias (durable across mailbox lifetimes).
	dest := capturedDest.Load()
	require.NotNil(t, dest)
	require.Equal(t, "deleteditems", *dest)
}

func TestExecutorRollsBackOnGraphFailure(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"nope"}}`))
	})

	err := exec.MarkRead(context.Background(), accID, "m-1")
	require.Error(t, err, "executor must surface Graph error")

	// Local state must be reverted to is_read=false.
	got, err := st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	require.False(t, got.IsRead, "local rolled back after Graph rejection")
}

func TestExecutorDrainRetriesPending(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	var ok atomic.Bool
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1", func(w http.ResponseWriter, r *http.Request) {
		if !ok.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// First call fails with 503 → action should remain Pending.
	require.Error(t, exec.MarkRead(context.Background(), accID, "m-1"))

	// Now the server says yes; Drain should pick up the pending action.
	ok.Store(true)
	require.NoError(t, exec.Drain(context.Background()))

	pending, err := st.PendingActions(context.Background())
	require.NoError(t, err)
	require.Empty(t, pending, "drain marks action Done")
}
