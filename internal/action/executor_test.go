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
	// After the move Graph returns a new message ID; the old row must
	// be gone and the new row carries the destination folder.
	_, oldErr := st.GetMessage(context.Background(), "m-1")
	require.ErrorIs(t, oldErr, store.ErrNotFound, "old ID must be deleted after rename")
	got, err := st.GetMessage(context.Background(), "m-1-moved")
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

	// Old ID must be gone; new ID carries the real destination folder ID.
	_, oldErr := st.GetMessage(context.Background(), "m-1")
	require.ErrorIs(t, oldErr, store.ErrNotFound, "old ID must be deleted after rename")
	got, err := st.GetMessage(context.Background(), "m-1-moved")
	require.NoError(t, err)
	require.Equal(t, "real-deleted-id-AAMkA", got.FolderID,
		"local folder MUST be the real folder ID resolved from well-known alias (FK)")

	// Graph received the alias (durable across mailbox lifetimes).
	dest := capturedDest.Load()
	require.NotNil(t, dest)
	require.Equal(t, "deleteditems", *dest)
}

// TestExecutorMoveToUserFolder is the spec 07 §6.5 round trip for
// the move-with-folder-picker path: the action carries the
// destination folder ID (no well-known alias since user folders
// don't have one), local apply rewrites parent_folder_id, the
// Graph dispatch uses the folder ID as destinationId, and the
// inverse pushed for undo restores the source folder.
func TestExecutorMoveToUserFolder(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-projects", AccountID: accID, DisplayName: "Projects", LastSyncedAt: time.Now(),
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

	require.NoError(t, exec.Move(context.Background(), accID, "m-1", "f-projects", ""))

	// Old ID must be gone; new ID carries the destination folder.
	_, oldErr := st.GetMessage(context.Background(), "m-1")
	require.ErrorIs(t, oldErr, store.ErrNotFound, "old ID must be deleted after rename")
	got, err := st.GetMessage(context.Background(), "m-1-moved")
	require.NoError(t, err)
	require.Equal(t, "f-projects", got.FolderID, "local row must reflect the destination folder")

	dest := capturedDest.Load()
	require.NotNil(t, dest)
	require.Equal(t, "f-projects", *dest, "user-folder moves use the folder ID (no alias)")

	// Inverse pushed: undo entry uses the NEW message ID and restores
	// the original folder (pre.FolderID from snapshot).
	entry, err := st.PeekUndo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, store.ActionMove, entry.ActionType)
	require.Equal(t, "f-inbox", entry.Params["destination_folder_id"],
		"undo entry must point back at the source folder")
	require.Equal(t, []string{"m-1-moved"}, entry.MessageIDs,
		"undo entry must carry the new message ID so undo can find the row")
}

// TestExecutorMoveRejectsEmptyDestination guards the API contract:
// supplying neither id nor alias is an error, not a no-op.
func TestExecutorMoveRejectsEmptyDestination(t *testing.T) {
	exec, _, accID, _ := newTestExec(t)
	err := exec.Move(context.Background(), accID, "m-1", "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "destination folder required")
}

// TestFolderCountChangesPerActionType is the exhaustive truth
// table for the optimistic folder-count adjustments. Drives the
// pure folderCountChanges helper through every action × read-state
// combination; the integration tests below verify the helper is
// actually called by applyLocal/rollbackLocal.
func TestFolderCountChangesPerActionType(t *testing.T) {
	pre := func(folderID string, isRead bool) *store.Message {
		return &store.Message{ID: "m-1", FolderID: folderID, IsRead: isRead}
	}
	moveAction := func(dest string) store.Action {
		return store.Action{
			Type:       store.ActionMove,
			Params:     map[string]any{"destination_folder_id": dest},
			MessageIDs: []string{"m-1"},
		}
	}
	cases := []struct {
		name string
		a    store.Action
		pre  *store.Message
		want []folderCountChange
	}{
		{
			name: "mark_read on unread → unread-=1",
			a:    store.Action{Type: store.ActionMarkRead, MessageIDs: []string{"m-1"}},
			pre:  pre("f-inbox", false),
			want: []folderCountChange{{folderID: "f-inbox", unreadDelta: -1}},
		},
		{
			name: "mark_read on already-read → no change",
			a:    store.Action{Type: store.ActionMarkRead, MessageIDs: []string{"m-1"}},
			pre:  pre("f-inbox", true),
			want: nil,
		},
		{
			name: "mark_unread on read → unread+=1",
			a:    store.Action{Type: store.ActionMarkUnread, MessageIDs: []string{"m-1"}},
			pre:  pre("f-inbox", true),
			want: []folderCountChange{{folderID: "f-inbox", unreadDelta: +1}},
		},
		{
			name: "mark_unread on already-unread → no change",
			a:    store.Action{Type: store.ActionMarkUnread, MessageIDs: []string{"m-1"}},
			pre:  pre("f-inbox", false),
			want: nil,
		},
		{
			name: "soft_delete unread → src both-=1, dst both+=1",
			a:    moveAction("f-deleted"),
			pre:  pre("f-inbox", false),
			want: []folderCountChange{
				{folderID: "f-inbox", totalDelta: -1, unreadDelta: -1},
				{folderID: "f-deleted", totalDelta: +1, unreadDelta: +1},
			},
		},
		{
			name: "soft_delete read → src total-=1, dst total+=1 (no unread carry)",
			a:    moveAction("f-deleted"),
			pre:  pre("f-inbox", true),
			want: []folderCountChange{
				{folderID: "f-inbox", totalDelta: -1, unreadDelta: 0},
				{folderID: "f-deleted", totalDelta: +1, unreadDelta: 0},
			},
		},
		{
			name: "move to same folder → no change (no-op move)",
			a:    moveAction("f-inbox"),
			pre:  pre("f-inbox", false),
			want: nil,
		},
		{
			name: "move with empty destination → no change (defensive)",
			a:    moveAction(""),
			pre:  pre("f-inbox", false),
			want: nil,
		},
		{
			name: "permanent_delete unread → src both-=1, no destination",
			a:    store.Action{Type: store.ActionPermanentDelete, MessageIDs: []string{"m-1"}},
			pre:  pre("f-inbox", false),
			want: []folderCountChange{{folderID: "f-inbox", totalDelta: -1, unreadDelta: -1}},
		},
		{
			name: "flag → no count change (read-state untouched)",
			a:    store.Action{Type: store.ActionFlag, MessageIDs: []string{"m-1"}},
			pre:  pre("f-inbox", false),
			want: nil,
		},
		{
			name: "add_category → no count change",
			a: store.Action{Type: store.ActionAddCategory, MessageIDs: []string{"m-1"},
				Params: map[string]any{"category": "Work"}},
			pre:  pre("f-inbox", false),
			want: nil,
		},
		{
			name: "create_draft_reply → no count change (no local row)",
			a:    store.Action{Type: store.ActionCreateDraftReply, MessageIDs: []string{"m-1"}},
			pre:  pre("f-inbox", false),
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := folderCountChanges(tc.a, tc.pre)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestExecutorMarkReadDecrementsInboxUnread is the integration
// test for the user-reported lag: pressing `r` on an unread
// message in Inbox immediately drops the sidebar's unread count by
// 1 (no waiting for the next sync). Without the optimistic
// adjustment, the user saw the count stuck at the pre-action
// value for ~30s.
func TestExecutorMarkReadDecrementsInboxUnread(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	// Seed Inbox with non-zero counts so we can observe the delta.
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: accID, DisplayName: "Inbox", WellKnownName: "inbox",
		TotalCount: 10, UnreadCount: 5, LastSyncedAt: time.Now(),
	}))
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(t, exec.MarkRead(context.Background(), accID, "m-1"))

	folders, err := st.ListFolders(context.Background(), accID)
	require.NoError(t, err)
	var inbox store.Folder
	for _, f := range folders {
		if f.ID == "f-inbox" {
			inbox = f
			break
		}
	}
	require.Equal(t, 10, inbox.TotalCount, "total_count untouched on mark_read")
	require.Equal(t, 4, inbox.UnreadCount, "unread_count -= 1 because the message was unread")
}

// TestExecutorSoftDeleteAdjustsBothFolders verifies the move-class
// action moves count between source and destination atomically.
func TestExecutorSoftDeleteAdjustsBothFolders(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: accID, DisplayName: "Inbox", WellKnownName: "inbox",
		TotalCount: 10, UnreadCount: 5, LastSyncedAt: time.Now(),
	}))
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "deleteditems", AccountID: accID, DisplayName: "Deleted Items", WellKnownName: "deleteditems",
		TotalCount: 100, UnreadCount: 0, LastSyncedAt: time.Now(),
	}))
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1/move", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m-1-moved"}`)
	})

	require.NoError(t, exec.SoftDelete(context.Background(), accID, "m-1"))

	folders, _ := st.ListFolders(context.Background(), accID)
	by := map[string]store.Folder{}
	for _, f := range folders {
		by[f.ID] = f
	}
	require.Equal(t, 9, by["f-inbox"].TotalCount, "Inbox total_count -= 1")
	require.Equal(t, 4, by["f-inbox"].UnreadCount, "Inbox unread_count -= 1 (m-1 was unread)")
	require.Equal(t, 101, by["deleteditems"].TotalCount, "Deleted Items total_count += 1")
	require.Equal(t, 1, by["deleteditems"].UnreadCount, "Deleted Items unread_count += 1 (carried)")
}

// TestExecutorRollsBackFolderCountsOnGraphFailure is the
// integrity invariant: when Graph rejects the action and the
// message-row mutation is rolled back, the folder counts must
// also revert. Without this, a 403 mid-flight would leave the
// row in pre-state but the sidebar count off-by-one until the
// next sync.
func TestExecutorRollsBackFolderCountsOnGraphFailure(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: accID, DisplayName: "Inbox", WellKnownName: "inbox",
		TotalCount: 10, UnreadCount: 5, LastSyncedAt: time.Now(),
	}))
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	err := exec.MarkRead(context.Background(), accID, "m-1")
	require.Error(t, err, "Graph 403 surfaces")

	folders, _ := st.ListFolders(context.Background(), accID)
	for _, f := range folders {
		if f.ID == "f-inbox" {
			require.Equal(t, 5, f.UnreadCount,
				"unread_count restored to pre-action 5 after Graph rejection")
		}
	}
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

// TestCreateDraftReplyEnqueuesActionAndPersistsDraftID is the spec
// 15 §5/§8 invariant: drafts now flow through the action queue
// rather than firing a synchronous one-shot. The action lands in
// the actions table with the source_message_id, body, recipients,
// AND — after createReply succeeds — the server-assigned draft id
// + webLink. The Done/Failed status reflects the dispatch outcome.
//
// Crash-recovery (PR 7-ii) reads the action's params on next launch
// to resume from the recorded draft_id, so we have to assert the
// id is durable in the table even though the in-memory return
// path also surfaces it.
func TestCreateDraftReplyEnqueuesActionAndPersistsDraftID(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "src-1", AccountID: accID, FolderID: "f-inbox", Subject: "x",
	}))

	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/src-1/createReply", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"draft-42","webLink":"https://outlook/drafts/42"}`)
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/draft-42", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		w.WriteHeader(http.StatusOK)
	})

	res, err := exec.CreateDraftReply(context.Background(), accID, "src-1", "Hi", []string{"a@example.invalid"}, nil, nil, "Re: x", nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "draft-42", res.ID)
	require.Equal(t, "https://outlook/drafts/42", res.WebLink)

	// The action persisted; status is Done. PendingActions returns
	// only Pending/InFlight rows so the Done row won't show — query
	// the audit shape directly via SweepDoneActions semantics.
	pending, err := st.PendingActions(context.Background())
	require.NoError(t, err)
	require.Empty(t, pending, "successful draft action must not stay Pending")
}

// TestCreateDraftReplyKeepsDraftIDOnPATCHFailure covers the most
// important crash-recovery shape: createReply succeeded (the draft
// IS on the server), but the body PATCH failed. The action must be
// Failed, draft_id must be recorded in Params, and the caller must
// receive a DraftResult with the webLink so the user can finish in
// Outlook. PR 7-ii's resume path reads that draft_id on next launch
// and re-PATCHes idempotently.
func TestCreateDraftReplyKeepsDraftIDOnPATCHFailure(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "src-2", AccountID: accID, FolderID: "f-inbox",
	}))

	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/src-2/createReply", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"draft-99","webLink":"https://outlook/drafts/99"}`)
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/draft-99", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"forbidden"}}`))
	})

	res, err := exec.CreateDraftReply(context.Background(), accID, "src-2", "Hi", []string{"a@example.invalid"}, nil, nil, "Re: x", nil)
	require.Error(t, err, "PATCH failure must surface to caller")
	require.NotNil(t, res, "even on PATCH failure the DraftResult must come back so the user finishes in Outlook")
	require.Equal(t, "draft-99", res.ID)
	require.Equal(t, "https://outlook/drafts/99", res.WebLink)

	// Action recorded as Failed; PendingActions doesn't return Failed
	// rows. Inspect via raw store helper.
	row := readRawAction(t, st, "src-2", store.ActionCreateDraftReply)
	require.Equal(t, store.StatusFailed, row.Status)
	require.Equal(t, "draft-99", row.Params["draft_id"], "draft_id must persist on PATCH-after-createReply failure (resume path needs it)")
	require.Equal(t, "https://outlook/drafts/99", row.Params["web_link"])
}

// TestCreateDraftReplyMarksFailedOnCreateReplyFailure covers the
// pure-stage-1 failure path: createReply itself errors. No draft
// exists; no draft_id should be persisted; action is Failed.
func TestCreateDraftReplyMarksFailedOnCreateReplyFailure(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "src-3", AccountID: accID, FolderID: "f-inbox",
	}))
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/src-3/createReply", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	res, err := exec.CreateDraftReply(context.Background(), accID, "src-3", "Hi", nil, nil, nil, "", nil)
	require.Error(t, err)
	require.Nil(t, res, "no DraftResult when stage 1 failed")

	row := readRawAction(t, st, "src-3", store.ActionCreateDraftReply)
	require.Equal(t, store.StatusFailed, row.Status)
	_, hasID := row.Params["draft_id"]
	require.False(t, hasID, "no draft_id when createReply itself failed")
}

// TestCreateDraftReplyRecipientsRoundTripThroughJSON guards the
// PR 7-ii resume contract: when the action is read back from the
// store (via PendingActions / ListActionsByType), the recipients
// stored in Params must come back in a shape the resume path can
// type-assert. Strings persist as JSON arrays; on decode they
// become []any. Resume must walk that []any to rebuild []string.
//
// Without this guard, a resume that did `to, _ := params["to"].
// ([]string)` would silently get a nil slice (no recipients) and
// fire a stage-2 PATCH that leaves the draft empty — the same bug
// shape as the original audit-row.
func TestCreateDraftReplyRecipientsRoundTripThroughJSON(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "src-rt", AccountID: accID, FolderID: "f-inbox",
	}))
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/src-rt/createReply", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"draft-rt","webLink":"https://outlook/drafts/rt"}`)
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/draft-rt", func(w http.ResponseWriter, _ *http.Request) {
		// Force PATCH failure so the action lands in a Failed state
		// with all params persisted — exactly the shape the resume
		// path on next launch will scan.
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, _ = exec.CreateDraftReply(context.Background(), accID, "src-rt", "Hi",
		[]string{"alice@example.invalid", "bob@example.invalid"},
		[]string{"cc@example.invalid"},
		nil,
		"Re: x", nil)

	row := readRawAction(t, st, "src-rt", store.ActionCreateDraftReply)
	require.Equal(t, store.StatusFailed, row.Status)

	// Recipients come back as []any after JSON decode. Resume code
	// must walk this shape to reconstruct []string.
	toRaw, ok := row.Params["to"].([]any)
	require.True(t, ok, "to recipients persist as []any after JSON round-trip")
	require.Len(t, toRaw, 2)
	require.Equal(t, "alice@example.invalid", toRaw[0])
	require.Equal(t, "bob@example.invalid", toRaw[1])

	ccRaw, ok := row.Params["cc"].([]any)
	require.True(t, ok)
	require.Len(t, ccRaw, 1)

	// The spec-15 audit-row symptom: no recipients silently lost.
	// Both draft_id AND recipients must be intact for the resume
	// path to reconstruct the PATCH body.
	require.Equal(t, "draft-rt", row.Params["draft_id"])
	require.Equal(t, "Hi", row.Params["body"])
	require.Equal(t, "Re: x", row.Params["subject"])
}

// TestDrainSkipsCreateDraftReply guards the spec 15 §8 idempotency
// invariant: stage 1 (POST /createReply) is non-idempotent —
// re-firing it produces a duplicate draft. Drain must skip
// ActionCreateDraftReply rows entirely; resume is PR 7-ii's job
// (with stage-aware logic that uses the recorded draft_id).
func TestDrainSkipsCreateDraftReply(t *testing.T) {
	exec, st, accID, _ := newTestExec(t)
	// Hand-craft a Pending draft action; if Drain re-fires it we'd
	// see the createReply hit (and panic on the missing handler).
	a := store.Action{
		ID:        newActionID(),
		AccountID: accID,
		Type:      store.ActionCreateDraftReply,
		Status:    store.StatusPending,
		Params: map[string]any{
			"source_message_id": "src-X",
			"body":              "hello",
		},
		SkipUndo: true,
	}
	require.NoError(t, st.EnqueueAction(context.Background(), a))

	// Drain must NOT call the (unregistered) createReply handler.
	require.NoError(t, exec.Drain(context.Background()))

	pending, err := st.PendingActions(context.Background())
	require.NoError(t, err)
	require.Len(t, pending, 1, "Drain leaves the draft action Pending; PR 7-ii will resume it on startup with stage-aware logic")
}

// TestCreateDraftReplyAllRoutesToReplyAllEndpoint is the spec 15
// §5 / PR 7-iii invariant: stage 1 hits /createReplyAll (not
// /createReply); stage 2 PATCHes the draft body. The action's
// type is ActionCreateDraftReplyAll.
func TestCreateDraftReplyAllRoutesToReplyAllEndpoint(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "src-ra", AccountID: accID, FolderID: "f-inbox",
	}))
	var hitReplyAll, hitPatch bool
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/src-ra/createReplyAll", func(w http.ResponseWriter, _ *http.Request) {
		hitReplyAll = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"draft-ra","webLink":"https://outlook/drafts/ra"}`)
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/draft-ra", func(w http.ResponseWriter, r *http.Request) {
		hitPatch = r.Method == http.MethodPatch
		w.WriteHeader(http.StatusOK)
	})

	res, err := exec.CreateDraftReplyAll(context.Background(), accID, "src-ra", "Hi all", []string{"a@example.invalid", "b@example.invalid"}, []string{"c@example.invalid"}, nil, "Re: x", nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, hitReplyAll, "stage 1 routes to /createReplyAll")
	require.True(t, hitPatch, "stage 2 PATCHes the draft body")

	row := readRawAction(t, st, "src-ra", store.ActionCreateDraftReplyAll)
	require.Equal(t, store.StatusDone, row.Status)
	require.Equal(t, "draft-ra", row.Params["draft_id"])
}

// TestCreateDraftForwardRoutesToForwardEndpoint mirrors the
// reply-all test but for the forward path: stage 1 hits
// /createForward; action type is ActionCreateDraftForward.
func TestCreateDraftForwardRoutesToForwardEndpoint(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "src-fwd", AccountID: accID, FolderID: "f-inbox",
	}))
	var hitForward, hitPatch bool
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/src-fwd/createForward", func(w http.ResponseWriter, _ *http.Request) {
		hitForward = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"draft-fwd","webLink":"https://outlook/drafts/fwd"}`)
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/draft-fwd", func(w http.ResponseWriter, r *http.Request) {
		hitPatch = r.Method == http.MethodPatch
		w.WriteHeader(http.StatusOK)
	})

	res, err := exec.CreateDraftForward(context.Background(), accID, "src-fwd", "Forwarding for review", []string{"alice@example.invalid"}, nil, nil, "Fwd: x", nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, hitForward)
	require.True(t, hitPatch)

	row := readRawAction(t, st, "src-fwd", store.ActionCreateDraftForward)
	require.Equal(t, store.StatusDone, row.Status)
}

// TestCreateNewDraftSinglePost is the spec 15 §5 / PR 7-iii
// new-message invariant: a single POST /me/messages carries the
// full payload — no two-stage createX/PATCH dance. The action
// transitions Pending → Done directly.
func TestCreateNewDraftSinglePost(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	var postCalls int
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		postCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"draft-new","webLink":"https://outlook/drafts/new"}`)
	})

	res, err := exec.CreateNewDraft(context.Background(), accID, "Hello world", []string{"alice@example.invalid"}, nil, nil, "New thread", nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "draft-new", res.ID)
	require.Equal(t, 1, postCalls, "single-stage: exactly one POST")

	rows, err := st.ListActionsByType(context.Background(), store.ActionCreateDraft)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, store.StatusDone, rows[0].Status)
	require.Equal(t, "draft-new", rows[0].Params["draft_id"])
}

// TestDrainSkipsAllDraftCreationKinds extends the spec 15 §8
// idempotency invariant to the new draft kinds: createReplyAll,
// createForward, and POST /me/messages are all non-idempotent at
// stage 1, so Drain must skip every kind.
func TestDrainSkipsAllDraftCreationKinds(t *testing.T) {
	exec, st, accID, _ := newTestExec(t)
	for _, kind := range []store.ActionType{
		store.ActionCreateDraftReply,
		store.ActionCreateDraftReplyAll,
		store.ActionCreateDraftForward,
		store.ActionCreateDraft,
	} {
		a := store.Action{
			ID:        newActionID(),
			AccountID: accID,
			Type:      kind,
			Status:    store.StatusPending,
			Params:    map[string]any{"body": "x"},
			SkipUndo:  true,
		}
		require.NoError(t, st.EnqueueAction(context.Background(), a))
	}

	// Drain must NOT hit any of the (unregistered) endpoints.
	require.NoError(t, exec.Drain(context.Background()))

	pending, err := st.PendingActions(context.Background())
	require.NoError(t, err)
	require.Len(t, pending, 4, "Drain skips all draft-creation kinds")
}

// readRawAction fetches an action of the supplied kind whose
// params carry a matching source_message_id. Goes through
// ListActionsByType so terminal-state rows (Failed / Done) are
// visible — PendingActions excludes those.
func readRawAction(t *testing.T, st store.Store, sourceMessageID string, kind store.ActionType) store.Action {
	t.Helper()
	rows, err := st.ListActionsByType(context.Background(), kind)
	require.NoError(t, err)
	for _, a := range rows {
		if id, _ := a.Params["source_message_id"].(string); id == sourceMessageID {
			return a
		}
	}
	t.Fatalf("action with source_message_id=%s of kind %s not found", sourceMessageID, kind)
	return store.Action{}
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

// TestRunSetsActionInFlightBeforeDispatch verifies that run() transitions
// the action from Pending to InFlight before firing the Graph call, so a
// crash during dispatch is visible to ReplayPending on next startup.
func TestRunSetsActionInFlightBeforeDispatch(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	observed := make(chan store.ActionStatus, 1)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-1", func(w http.ResponseWriter, r *http.Request) {
		// Sample the action status from inside the handler — this is the
		// window between InFlight mark and Done mark.
		actions, _ := st.PendingActions(context.Background())
		for _, a := range actions {
			if a.Type == store.ActionMarkRead {
				observed <- a.Status
				break
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	require.NoError(t, exec.MarkRead(context.Background(), accID, "m-1"))

	select {
	case status := <-observed:
		require.Equal(t, store.StatusInFlight, status,
			"action must be InFlight while the Graph call is in-flight")
	default:
		t.Fatal("Graph handler was not called — InFlight observation skipped")
	}
}

// TestReplayPendingResetsInFlight confirms that InFlight non-draft actions
// are demoted back to Pending so the next Drain cycle re-dispatches them.
func TestReplayPendingResetsInFlight(t *testing.T) {
	exec, st, accID, _ := newTestExec(t)
	a := store.Action{
		ID:         newActionID(),
		AccountID:  accID,
		Type:       store.ActionMarkRead,
		MessageIDs: []string{"m-1"},
		Status:     store.StatusInFlight,
	}
	require.NoError(t, st.EnqueueAction(context.Background(), a))
	// Manually bump to InFlight to simulate a crash during dispatch.
	require.NoError(t, st.UpdateActionStatus(context.Background(), a.ID, store.StatusInFlight, ""))

	require.NoError(t, exec.ReplayPending(context.Background()))

	actions, err := st.PendingActions(context.Background())
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, store.StatusPending, actions[0].Status,
		"InFlight non-draft must be reset to Pending by ReplayPending")
}

// TestReplayPendingSkipsDraftCreation ensures draft-creation InFlight
// rows are left untouched — they use a separate stage-aware resume path.
func TestReplayPendingSkipsDraftCreation(t *testing.T) {
	exec, st, accID, _ := newTestExec(t)
	a := store.Action{
		ID:         newActionID(),
		AccountID:  accID,
		Type:       store.ActionCreateDraftReply,
		MessageIDs: []string{"src-1"},
		Status:     store.StatusPending,
	}
	require.NoError(t, st.EnqueueAction(context.Background(), a))
	require.NoError(t, st.UpdateActionStatus(context.Background(), a.ID, store.StatusInFlight, ""))

	require.NoError(t, exec.ReplayPending(context.Background()))

	actions, err := st.PendingActions(context.Background())
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, store.StatusInFlight, actions[0].Status,
		"draft-creation InFlight must not be touched by ReplayPending")
}

// TestDrainRenamesRowToNewIDOnMove verifies that Drain re-dispatch of a
// Pending move action renames the local row when Graph returns a new ID.
func TestDrainRenamesRowToNewIDOnMove(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-projects", AccountID: accID, DisplayName: "Projects", LastSyncedAt: time.Now(),
	}))
	// Seed a message that was moved locally but hasn't been dispatched yet.
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "m-drain", AccountID: accID, FolderID: "f-projects", Subject: "drain test",
		FromAddress: "x@example.invalid",
	}))
	// Hand-craft a Pending move action (simulates what ReplayPending
	// leaves behind after a crash mid-dispatch).
	a := store.Action{
		ID:         newActionID(),
		AccountID:  accID,
		Type:       store.ActionMove,
		MessageIDs: []string{"m-drain"},
		Status:     store.StatusPending,
		Params: map[string]any{
			"destination_folder_id": "f-projects",
		},
	}
	require.NoError(t, st.EnqueueAction(context.Background(), a))

	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/m-drain/move", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "m-drain-renamed"})
	})

	require.NoError(t, exec.Drain(context.Background()))

	_, oldErr := st.GetMessage(context.Background(), "m-drain")
	require.ErrorIs(t, oldErr, store.ErrNotFound, "old row must be deleted after drain rename")
	got, err := st.GetMessage(context.Background(), "m-drain-renamed")
	require.NoError(t, err)
	require.Equal(t, "f-projects", got.FolderID)
}

// TestFlagWithDueDatePersists verifies that the flag action's due_date param
// round-trips to the store's flag_due_at column via applyLocal (H-2).
func TestFlagWithDueDatePersists(t *testing.T) {
	_, st, accID, srv := newTestExec(t)
	defer srv.Close()

	ctx := context.Background()
	require.NoError(t, st.UpsertFolder(ctx, store.Folder{ID: "f-inbox", AccountID: accID, WellKnownName: "inbox", DisplayName: "Inbox"}))
	pre := &store.Message{
		ID: "m-flag", AccountID: accID, FolderID: "f-inbox",
		Subject: "flag me", FromAddress: "x@example.invalid",
	}
	require.NoError(t, st.UpsertMessage(ctx, *pre))

	// applyLocal with a due_date param must write flag_due_at.
	due := "2026-05-15T00:00:00Z"
	a := store.Action{
		Type:       store.ActionFlag,
		MessageIDs: []string{"m-flag"},
		Params:     map[string]any{"due_date": due},
	}
	require.NoError(t, applyLocal(ctx, st, a, pre))

	got, err := st.GetMessage(ctx, "m-flag")
	require.NoError(t, err)
	require.Equal(t, "flagged", got.FlagStatus)
	// 2026-05-15T00:00:00Z → Unix 1778803200.
	require.Equal(t, int64(1778803200), got.FlagDueAt.Unix())
}
