package action

import (
	"context"
	"encoding/json"
	"io"
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

// TestExecutorCreateFolderUpsertsLocally verifies the spec 18
// happy path: graph CreateFolder is called with the right body,
// the response is upserted into the local store so the sidebar
// reflects the new folder before the next sync.
func TestExecutorCreateFolderUpsertsLocally(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"f-vendors","displayName":"Vendors","parentFolderId":""}`)
	})

	res, err := exec.CreateFolder(context.Background(), accID, "", "Vendors")
	require.NoError(t, err)
	require.Equal(t, "f-vendors", res.ID)
	require.Equal(t, "Vendors", res.DisplayName)

	// Local upsert ran.
	folders, err := st.ListFolders(context.Background(), accID)
	require.NoError(t, err)
	var found bool
	for _, f := range folders {
		if f.ID == "f-vendors" {
			found = true
			require.Equal(t, "Vendors", f.DisplayName)
		}
	}
	require.True(t, found, "create_folder must upsert the new row locally")
}

// TestExecutorRenameFolderUpdatesLocally verifies the rename round-trip.
func TestExecutorRenameFolderUpdatesLocally(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	// Seed a folder to rename.
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-old", AccountID: accID, DisplayName: "OldName",
	}))
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/mailFolders/f-old", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(t, exec.RenameFolder(context.Background(), "f-old", "NewName"))
	folders, _ := st.ListFolders(context.Background(), accID)
	for _, f := range folders {
		if f.ID == "f-old" {
			require.Equal(t, "NewName", f.DisplayName)
		}
	}
}

// TestExecutorDeleteFolderRemovesLocally verifies delete cascades.
func TestExecutorDeleteFolderRemovesLocally(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-doomed", AccountID: accID, DisplayName: "Doomed",
	}))
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/mailFolders/f-doomed", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})
	require.NoError(t, exec.DeleteFolder(context.Background(), "f-doomed"))
	folders, _ := st.ListFolders(context.Background(), accID)
	for _, f := range folders {
		require.NotEqual(t, "f-doomed", f.ID, "deleted folder must be gone from store")
	}
}

// TestExecutorAddCategoryAppendsAndPatchesGraph verifies the spec
// 07 §6.9 round trip: a new category is appended to the local row
// (case-insensitive dedup), Graph receives a PATCH with the full
// post-state list, and the inverse pushed onto the undo stack is
// remove_category for the same name.
func TestExecutorAddCategoryAppendsAndPatchesGraph(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	// Seed the message with an existing category so we can verify
	// dedup + append-not-replace.
	require.NoError(t, st.UpdateMessageFields(context.Background(), "m-1", store.MessageFields{
		Categories: &[]string{"Existing"},
	}))

	var got []string
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		raw, _ := body["categories"].([]any)
		got = make([]string, len(raw))
		for i, v := range raw {
			got[i], _ = v.(string)
		}
		w.WriteHeader(http.StatusOK)
	})

	require.NoError(t, exec.AddCategory(context.Background(), accID, "m-1", "Q4"))
	require.Equal(t, []string{"Existing", "Q4"}, got,
		"PATCH must carry the full post-state list, not a delta")

	// Local row reflects the append.
	row, err := st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	require.Equal(t, []string{"Existing", "Q4"}, row.Categories)

	// Undo entry is the inverse (remove_category for the same name).
	entry, err := st.PeekUndo(context.Background())
	require.NoError(t, err)
	require.Equal(t, store.ActionRemoveCategory, entry.ActionType)
	require.Equal(t, "Q4", entry.Params["category"])
}

// TestExecutorAddCategoryDedupsCaseInsensitively verifies the
// Outlook semantic: tagging with an existing category (regardless
// of case) is a no-op locally.
func TestExecutorAddCategoryDedupsCaseInsensitively(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpdateMessageFields(context.Background(), "m-1", store.MessageFields{
		Categories: &[]string{"q4"},
	}))
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(t, exec.AddCategory(context.Background(), accID, "m-1", "Q4"))
	row, err := st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	require.Equal(t, []string{"q4"}, row.Categories,
		"existing case-insensitive match must NOT be appended")
}

// TestExecutorRemoveCategoryDropsFromList covers spec 07 §6.10.
func TestExecutorRemoveCategoryDropsFromList(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpdateMessageFields(context.Background(), "m-1", store.MessageFields{
		Categories: &[]string{"Q4", "Important"},
	}))
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(t, exec.RemoveCategory(context.Background(), accID, "m-1", "Q4"))
	row, err := st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	require.Equal(t, []string{"Important"}, row.Categories)
}

// TestExecutorPermanentDeleteHitsGraphAndRemovesLocally is the spec
// 07 §6.7 invariant: the executor calls POST
// /me/messages/{id}/permanentDelete, the local row is removed, and
// no undo entry is pushed (the action is intentionally
// non-reversible).
func TestExecutorPermanentDeleteHitsGraphAndRemovesLocally(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	var hits atomic.Int32
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1/permanentDelete", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})

	require.NoError(t, exec.PermanentDelete(context.Background(), accID, "m-1"))
	require.Equal(t, int32(1), hits.Load(), "Graph permanentDelete must be called exactly once")

	// Local row gone — GetMessage returns ErrNotFound.
	_, err := st.GetMessage(context.Background(), "m-1")
	require.ErrorIs(t, err, store.ErrNotFound,
		"permanent_delete must remove the local row")

	// No undo entry pushed (Inverse returns ok=false).
	_, err = st.PeekUndo(context.Background())
	require.ErrorIs(t, err, store.ErrNotFound,
		"permanent_delete must NOT push to the undo stack — irreversible by design")
}

// TestExecutorPermanentDeleteRollsBackOnGraphFailure verifies the
// snapshot-restore path: if Graph rejects the destructive call,
// the local row gets re-inserted from the pre-action snapshot so
// the user's view returns to consistency.
func TestExecutorPermanentDeleteRollsBackOnGraphFailure(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1/permanentDelete", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"Forbidden","message":"no"}}`)
	})

	err := exec.PermanentDelete(context.Background(), accID, "m-1")
	require.Error(t, err)

	// Local row restored.
	got, err := st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	require.NotNil(t, got, "rollback must re-insert the message after Graph failure")
	require.Equal(t, "m-1", got.ID)
}

// TestExecutorMarkReadPushesUndoEntry is the spec 07 §11 invariant:
// a successful action pushes its inverse onto the undo stack so the
// next `u` keystroke can roll it back.
func TestExecutorMarkReadPushesUndoEntry(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	var captured atomic.Pointer[map[string]any]
	installPatchHandler(t, srv, &captured)

	require.NoError(t, exec.MarkRead(context.Background(), accID, "m-1"))

	entry, err := st.PeekUndo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, store.ActionMarkUnread, entry.ActionType,
		"undo of mark_read must be mark_unread")
	require.Equal(t, []string{"m-1"}, entry.MessageIDs)
}

// TestExecutorUndoRollsBackMarkRead drives the full round-trip:
// MarkRead → undo → message back to unread, both locally and via
// Graph. The visible-delta is local IsRead flipping false again
// after the undo dispatch lands.
func TestExecutorUndoRollsBackMarkRead(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	var captured atomic.Pointer[map[string]any]
	installPatchHandler(t, srv, &captured)

	require.NoError(t, exec.MarkRead(context.Background(), accID, "m-1"))
	got, err := st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	require.True(t, got.IsRead)

	entry, err := exec.Undo(context.Background(), accID)
	require.NoError(t, err)
	require.Equal(t, store.ActionMarkUnread, entry.ActionType)

	got, err = st.GetMessage(context.Background(), "m-1")
	require.NoError(t, err)
	require.False(t, got.IsRead, "undo must flip IsRead back to false")

	// Stack is now empty — pressing u again must surface ErrNotFound,
	// which the UI translates into "nothing to undo".
	_, err = exec.Undo(context.Background(), accID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestExecutorUndoDoesNotRecursivelyPush is the spec 07 §11.2
// invariant: applying an undo entry sets SkipUndo so the executor
// doesn't push the inverse-of-the-inverse. Without this, pressing
// `u` would toggle infinitely instead of stepping back.
func TestExecutorUndoDoesNotRecursivelyPush(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	var captured atomic.Pointer[map[string]any]
	installPatchHandler(t, srv, &captured)

	require.NoError(t, exec.MarkRead(context.Background(), accID, "m-1"))
	_, err := exec.Undo(context.Background(), accID)
	require.NoError(t, err)

	// After the undo, the stack must be empty — no inverse-of-the-
	// inverse landed on top.
	_, err = st.PeekUndo(context.Background())
	require.ErrorIs(t, err, store.ErrNotFound)
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
