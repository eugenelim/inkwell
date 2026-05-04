package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	stdsync "sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/graph"
	ilog "github.com/eugenelim/inkwell/internal/log"
	"github.com/eugenelim/inkwell/internal/store"
)

// openSyncTestStore opens a fresh store for sync tests. Replicated
// inline rather than imported because internal/store helpers live in
// store_test.go (cycle-free this way).
func openSyncTestStore(t *testing.T) store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	s, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedSyncAccount(t *testing.T, s store.Store) int64 {
	t.Helper()
	id, err := s.PutAccount(context.Background(), store.Account{
		TenantID: "T", ClientID: "C", UPN: "tester@example.invalid",
	})
	require.NoError(t, err)
	return id
}

// fakeAuth satisfies graph.Authenticator for sync tests.
type fakeAuth struct{ invalidates atomic.Int32 }

func (f *fakeAuth) Token(_ context.Context) (string, error) { return "tok", nil }
func (f *fakeAuth) Invalidate()                             { f.invalidates.Add(1) }

func newSyncTest(t *testing.T) (Engine, *fakeServer, store.Store, int64) {
	t.Helper()
	st := openSyncTestStore(t)
	acc := seedSyncAccount(t, st)

	srv := newFakeServer()
	t.Cleanup(srv.Close)

	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	gc, err := graph.NewClient(&fakeAuth{}, graph.Options{
		BaseURL: srv.URL(),
		Logger:  logger,
	})
	require.NoError(t, err)

	eng, err := New(gc, st, nil, Options{
		AccountID:          acc,
		Logger:             logger,
		ForegroundInterval: 50 * time.Millisecond,
		BackgroundInterval: time.Second,
	})
	require.NoError(t, err)
	return eng, srv, st, acc
}

// fakeServer is a programmable Graph stand-in. Each test wires a
// handler onto a path via Handle.
type fakeServer struct {
	mux    *http.ServeMux
	server *httptest.Server
}

func newFakeServer() *fakeServer {
	f := &fakeServer{mux: http.NewServeMux()}
	f.server = httptest.NewServer(f.mux)
	return f
}

func (f *fakeServer) Close()      { f.server.Close() }
func (f *fakeServer) URL() string { return f.server.URL }
func (f *fakeServer) Handle(path string, h http.HandlerFunc) {
	f.mux.HandleFunc(path, h)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestSyncFolderEnumerationNullsOutUntrackedParents(t *testing.T) {
	// Regression: v0.2.0/v0.2.1/v0.2.2 hit
	// "FOREIGN KEY constraint failed (787)" because Graph's
	// /me/mailFolders response returned each top-level folder with
	// `parentFolderId = msgfolderroot`, a folder we don't track.
	// Inserting that violated folders.parent_folder_id → folders.id.
	// Fix (spec 03 §iter-4): NULL out parent_folder_id when the
	// referenced folder isn't in the response set.
	eng, srv, st, acc := newSyncTest(t)
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.FolderListResponse{
			Value: []graph.MailFolder{
				{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox", ParentFolderID: "msgfolderroot"},
				{ID: "f-archive", DisplayName: "Archive", WellKnownName: "archive", ParentFolderID: "msgfolderroot"},
				{ID: "f-child", DisplayName: "Child", ParentFolderID: "f-inbox"}, // tracked parent — preserved
			},
		})
	})
	require.NoError(t, eng.(*engine).syncFolders(context.Background()))

	got, err := st.ListFolders(context.Background(), acc)
	require.NoError(t, err)
	require.Len(t, got, 3, "all three folders inserted despite untracked msgfolderroot parent")

	// f-child's parent IS in the response, so it should be preserved.
	for _, f := range got {
		if f.ID == "f-child" {
			require.Equal(t, "f-inbox", f.ParentFolderID, "tracked parent must be preserved")
		}
		if f.ID == "f-inbox" {
			require.Empty(t, f.ParentFolderID, "untracked parent (msgfolderroot) must be NULLed out")
		}
	}
}

func TestSyncFolderEnumerationUpsertsAndDeletes(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.FolderListResponse{
			Value: []graph.MailFolder{
				{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
				{ID: "f-archive", DisplayName: "Archive", WellKnownName: "archive"},
				{ID: "f-clients", DisplayName: "Clients"},
			},
		})
	})

	require.NoError(t, eng.(*engine).syncFolders(context.Background()))

	got, err := st.ListFolders(context.Background(), acc)
	require.NoError(t, err)
	require.Len(t, got, 3)

	// Now drop "Clients" server-side; sync must delete it locally.
	srv.mux = http.NewServeMux()
	srv.mux.HandleFunc("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.FolderListResponse{
			Value: []graph.MailFolder{
				{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
				{ID: "f-archive", DisplayName: "Archive", WellKnownName: "archive"},
			},
		})
	})
	srv.server.Config.Handler = srv.mux
	require.NoError(t, eng.(*engine).syncFolders(context.Background()))

	got2, err := st.ListFolders(context.Background(), acc)
	require.NoError(t, err)
	require.Len(t, got2, 2)
}

// TestSyncFolderEnumerationPersistsNestedChildren is the spec 03
// real-tenant regression for the v0.13.x sub-folder bug: pressing
// `o` on Inbox showed no children because /me/mailFolders is
// non-recursive — only top-level folders were ever synced. Fix
// switches the sync helper to /me/mailFolders/delta which returns
// every folder regardless of depth in one paginated response.
//
// Fixture: 4-level hierarchy (Inbox > Projects > Q4 > Decks). All
// four rows must land in the local store with parent_folder_id
// chains intact, so flattenFolderTree can render the tree and
// ToggleExpand finds the children for `o` / Enter.
func TestSyncFolderEnumerationPersistsNestedChildren(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.FolderListResponse{
			Value: []graph.MailFolder{
				{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox", ParentFolderID: "msgfolderroot"},
				{ID: "f-projects", DisplayName: "Projects", ParentFolderID: "f-inbox"},
				{ID: "f-q4", DisplayName: "Q4", ParentFolderID: "f-projects"},
				{ID: "f-decks", DisplayName: "Decks", ParentFolderID: "f-q4"},
			},
		})
	})

	require.NoError(t, eng.(*engine).syncFolders(context.Background()))

	got, err := st.ListFolders(context.Background(), acc)
	require.NoError(t, err)
	require.Len(t, got, 4, "all four folders (Inbox + 3 nested levels) must persist")

	byID := make(map[string]store.Folder, len(got))
	for _, f := range got {
		byID[f.ID] = f
	}
	require.Empty(t, byID["f-inbox"].ParentFolderID, "untracked msgfolderroot NULLed out")
	require.Equal(t, "f-inbox", byID["f-projects"].ParentFolderID)
	require.Equal(t, "f-projects", byID["f-q4"].ParentFolderID)
	require.Equal(t, "f-q4", byID["f-decks"].ParentFolderID, "deepest child preserves chain")
}

// TestSyncFolderEnumerationPersistsArchiveChildren is the
// real-tenant regression for "Archive shows 'no subfolders to
// expand here' even when Outlook for Mac shows children". Mirrors
// the Inbox case but rooted at Archive (well-known name
// "archive") to prove the sync path doesn't special-case Inbox —
// every parent + child chain is persisted regardless of root.
//
// Fixture: msgfolderroot > Inbox + Archive (top-level), Archive >
// 2024 > Q4 (nested under Archive). All four rows must land with
// parent chains intact so the UI tree renders Archive as a
// parent and `o` expands it.
func TestSyncFolderEnumerationPersistsArchiveChildren(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.FolderListResponse{
			Value: []graph.MailFolder{
				{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox", ParentFolderID: "msgfolderroot"},
				{ID: "f-archive", DisplayName: "Archive", WellKnownName: "archive", ParentFolderID: "msgfolderroot"},
				{ID: "f-archive-2024", DisplayName: "2024", ParentFolderID: "f-archive"},
				{ID: "f-archive-2024-q4", DisplayName: "Q4", ParentFolderID: "f-archive-2024"},
			},
		})
	})
	require.NoError(t, eng.(*engine).syncFolders(context.Background()))

	got, err := st.ListFolders(context.Background(), acc)
	require.NoError(t, err)
	require.Len(t, got, 4, "Archive's children must persist alongside Inbox's")
	byID := map[string]store.Folder{}
	for _, f := range got {
		byID[f.ID] = f
	}
	require.Empty(t, byID["f-archive"].ParentFolderID, "Archive's msgfolderroot parent NULLed")
	require.Equal(t, "f-archive", byID["f-archive-2024"].ParentFolderID,
		"Archive's child preserves the parent link")
	require.Equal(t, "f-archive-2024", byID["f-archive-2024-q4"].ParentFolderID,
		"Archive's grandchild preserves the parent chain")
}

// TestSyncFolderEnumerationHandlesNestedFolderCalledInbox is the
// v0.16.0 real-tenant regression: a user with a nested folder
// literally named "Inbox" (very common — old-mail archives, year-
// indexed organisation, shared-mailbox mounts) hit
// `UNIQUE constraint failed: folders.account_id,
// folders.well_known_name` on first sync. RT-1 switched to
// /me/mailFolders/delta which returns nested children; the legacy
// `inferWellKnownName(displayName)` heuristic fired on the child
// "Inbox" too, conflicting with the real top-level one.
//
// Fix: only apply the heuristic to top-level folders (parent is
// empty after the NULL-out). Children keep wellKnownName empty
// when Graph didn't set one.
func TestSyncFolderEnumerationHandlesNestedFolderCalledInbox(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.FolderListResponse{
			Value: []graph.MailFolder{
				// Real top-level Inbox with wellKnownName populated by Graph.
				{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox", ParentFolderID: "msgfolderroot"},
				// User-created folder coincidentally named "Inbox" inside Archive —
				// the heuristic would otherwise infer wellKnownName="inbox" and
				// conflict with the real one.
				{ID: "f-archive", DisplayName: "Archive", WellKnownName: "archive", ParentFolderID: "msgfolderroot"},
				{ID: "f-archive-inbox", DisplayName: "Inbox", ParentFolderID: "f-archive"},
				// Another collision: user has a folder called "Sent Items"
				// inside their Archive.
				{ID: "f-archive-sent", DisplayName: "Sent Items", ParentFolderID: "f-archive"},
			},
		})
	})

	// Without the fix, this errors with the unique-constraint failure.
	require.NoError(t, eng.(*engine).syncFolders(context.Background()),
		"sync must NOT error when a child folder shares a display name with a well-known one")

	got, err := st.ListFolders(context.Background(), acc)
	require.NoError(t, err)
	require.Len(t, got, 4)

	byID := map[string]store.Folder{}
	for _, f := range got {
		byID[f.ID] = f
	}
	require.Equal(t, "inbox", byID["f-inbox"].WellKnownName,
		"real Inbox keeps its wellKnownName")
	require.Empty(t, byID["f-archive-inbox"].WellKnownName,
		"nested 'Inbox' folder has no inferred wellKnownName (would conflict)")
	require.Empty(t, byID["f-archive-sent"].WellKnownName,
		"nested 'Sent Items' folder has no inferred wellKnownName")
}

// TestSyncFolderEnumerationDedupesGraphReturnedWellKnownClash is
// the defensive secondary: even if Graph itself returns two
// folders with the same wellKnownName (unlikely but possible for
// shared-mailbox mounts, search folders, etc.), the sync layer
// must not error out on the unique-index conflict. The first
// folder with a given wellKnownName wins; later rows in the same
// response keep their own ID but get wellKnownName cleared.
func TestSyncFolderEnumerationDedupesGraphReturnedWellKnownClash(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.FolderListResponse{
			Value: []graph.MailFolder{
				{ID: "f-inbox-1", DisplayName: "Inbox", WellKnownName: "inbox", ParentFolderID: "msgfolderroot"},
				// Pathological case: Graph returns a second folder also
				// claiming to be the inbox. Sync must not crash.
				{ID: "f-inbox-2", DisplayName: "Inbox (shared)", WellKnownName: "inbox", ParentFolderID: "msgfolderroot"},
			},
		})
	})
	require.NoError(t, eng.(*engine).syncFolders(context.Background()))
	got, _ := st.ListFolders(context.Background(), acc)
	require.Len(t, got, 2)
	byID := map[string]store.Folder{}
	for _, f := range got {
		byID[f.ID] = f
	}
	require.Equal(t, "inbox", byID["f-inbox-1"].WellKnownName, "first wins")
	require.Empty(t, byID["f-inbox-2"].WellKnownName, "second has wellKnownName cleared")
}

// TestSyncFolderEnumerationTombstoneDeletesExistingFolder verifies
// that @removed markers from the delta endpoint delete the matching
// folder from the local store. Spec 03 §6.2 tombstone propagation.
func TestSyncFolderEnumerationTombstoneDeletesExistingFolder(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	// Pre-seed a folder that the server will mark as deleted.
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-gone", AccountID: acc, DisplayName: "To Be Deleted", LastSyncedAt: time.Now(),
	}))
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"value": [
				{"id": "f-inbox", "displayName": "Inbox", "wellKnownName": "inbox"},
				{"id": "f-gone", "@removed": {"reason": "deleted"}}
			]
		}`))
	})
	require.NoError(t, eng.(*engine).syncFolders(context.Background()))
	got, err := st.ListFolders(context.Background(), acc)
	require.NoError(t, err)
	require.Len(t, got, 1, "@removed entry must be deleted, not left in store")
	require.Equal(t, "f-inbox", got[0].ID)
}

func TestSyncFolderQuickStartPersistsMessages(t *testing.T) {
	// v0.2.4: first-launch path is /messages, not /messages/delta.
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))

	srv.Handle("/me/mailFolders/f-inbox/messages", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.ListMessagesResponse{
			Value: []graph.Message{
				{
					ID:               "m-1",
					Subject:          "First",
					BodyPreview:      "Hello",
					ReceivedDateTime: time.Now().Add(-time.Hour),
					From:             &graph.Recipient{EmailAddress: graph.EmailAddress{Name: "Alice", Address: "alice@example.invalid"}},
				},
				{
					ID:               "m-2",
					Subject:          "Second",
					ReceivedDateTime: time.Now().Add(-30 * time.Minute),
					From:             &graph.Recipient{EmailAddress: graph.EmailAddress{Name: "Bob", Address: "bob@example.invalid"}},
				},
			},
		})
	})

	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))

	msgs, err := st.ListMessages(context.Background(), store.MessageQuery{AccountID: acc, FolderID: "f-inbox", Limit: 50})
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	tok, err := st.GetDeltaToken(context.Background(), acc, "f-inbox")
	require.NoError(t, err)
	require.False(t, tok.LastDeltaAt.IsZero(), "LastDeltaAt set so next tick takes pullSince")
	require.Empty(t, tok.DeltaLink, "no delta cursor seeded yet")
}

func TestSyncFolderDeltaResumesFromCursor(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))

	cursor := srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=cursor-1"
	require.NoError(t, st.PutDeltaToken(context.Background(), store.DeltaToken{
		AccountID: acc, FolderID: "f-inbox", DeltaLink: cursor,
	}))

	var hits atomic.Int32
	srv.Handle("/me/mailFolders/f-inbox/messages/delta", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		require.Equal(t, "cursor-1", r.URL.Query().Get("$deltatoken"), "must use persisted cursor")
		writeJSON(w, graph.DeltaResponse{
			Value:     []graph.Message{{ID: "m-3", Subject: "Third"}},
			DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=cursor-2",
		})
	})

	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
	require.Equal(t, int32(1), hits.Load())

	tok, err := st.GetDeltaToken(context.Background(), acc, "f-inbox")
	require.NoError(t, err)
	require.Contains(t, tok.DeltaLink, "deltatoken=cursor-2")
}

func TestSyncFolderHandlesSyncStateNotFound(t *testing.T) {
	// On 410 Gone (syncStateNotFound), the engine clears the token
	// and re-runs syncFolder. v0.2.4: that re-run takes the quickStart
	// path (/messages, not /messages/delta) since there's no cursor
	// left.
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))
	require.NoError(t, st.PutDeltaToken(context.Background(), store.DeltaToken{
		AccountID: acc, FolderID: "f-inbox", DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=stale",
	}))

	var deltaHits atomic.Int32
	srv.Handle("/me/mailFolders/f-inbox/messages/delta", func(w http.ResponseWriter, r *http.Request) {
		deltaHits.Add(1)
		require.Equal(t, "stale", r.URL.Query().Get("$deltatoken"))
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":{"code":"syncStateNotFound","message":"resync"}}`))
	})
	var quickStartHits atomic.Int32
	srv.Handle("/me/mailFolders/f-inbox/messages", func(w http.ResponseWriter, _ *http.Request) {
		quickStartHits.Add(1)
		writeJSON(w, graph.ListMessagesResponse{
			Value: []graph.Message{{ID: "m-fresh"}},
		})
	})

	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
	require.Equal(t, int32(1), deltaHits.Load(), "delta call returned 410")
	require.Equal(t, int32(1), quickStartHits.Load(), "recovery falls through to quickStart")

	tok, err := st.GetDeltaToken(context.Background(), acc, "f-inbox")
	require.NoError(t, err)
	require.False(t, tok.LastDeltaAt.IsZero(), "quickStart sets LastDeltaAt on the recovered row")
}

func TestSyncFolderPaginatesNextLink(t *testing.T) {
	// Tests followDeltaPage with a pre-seeded NextLink (mid-pagination
	// resume). v0.2.4 splits first-launch (quickStart) from cursor
	// resumption (followDeltaPage); this test exercises the latter.
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))
	// Pre-seed a NextLink so the first call goes through followDeltaPage.
	require.NoError(t, st.PutDeltaToken(context.Background(), store.DeltaToken{
		AccountID: acc, FolderID: "f-inbox",
		NextLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta",
	}))

	page2 := srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$skiptoken=p2"

	srv.Handle("/me/mailFolders/f-inbox/messages/delta", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("$skiptoken") {
		case "":
			writeJSON(w, graph.DeltaResponse{
				Value:    []graph.Message{{ID: "m-1"}},
				NextLink: page2,
			})
		case "p2":
			writeJSON(w, graph.DeltaResponse{
				Value:     []graph.Message{{ID: "m-2"}, {ID: "m-3"}},
				DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=final",
			})
		}
	})

	// Spec §5.3: each Sync call yields after one page during lazy
	// progressive backfill. The first tick stores m-1 and persists
	// the nextLink as next_link.
	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
	msgs, err := st.ListMessages(context.Background(), store.MessageQuery{AccountID: acc, FolderID: "f-inbox", Limit: 50})
	require.NoError(t, err)
	require.Len(t, msgs, 1, "tick 1 drains one page only")
	tok, err := st.GetDeltaToken(context.Background(), acc, "f-inbox")
	require.NoError(t, err)
	require.Contains(t, tok.NextLink, "skiptoken=p2")
	require.Empty(t, tok.DeltaLink, "no deltaLink yet — still mid-pagination")

	// Second tick consumes the persisted nextLink, drains the final
	// page, and seeds the deltaLink.
	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
	msgs, err = st.ListMessages(context.Background(), store.MessageQuery{AccountID: acc, FolderID: "f-inbox", Limit: 50})
	require.NoError(t, err)
	require.Len(t, msgs, 3, "tick 2 drains the second page")
	tok, err = st.GetDeltaToken(context.Background(), acc, "f-inbox")
	require.NoError(t, err)
	require.Empty(t, tok.NextLink, "next_link cleared once deltaLink is set")
	require.Contains(t, tok.DeltaLink, "deltatoken=final")
}

func TestSyncFolderAppliesRemovedTombstones(t *testing.T) {
	// @removed tombstones only come through the delta endpoint, which
	// is now followDeltaPage. Pre-seed a DeltaLink to exercise that
	// path; v0.2.4's quickStart / pullSince paths don't track deletes.
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "m-doomed", AccountID: acc, FolderID: "f-inbox", Subject: "doomed",
	}))
	require.NoError(t, st.PutDeltaToken(context.Background(), store.DeltaToken{
		AccountID: acc, FolderID: "f-inbox",
		DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=cur",
	}))

	srv.Handle("/me/mailFolders/f-inbox/messages/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.DeltaResponse{
			Value:     []graph.Message{{ID: "m-doomed", Removed: &graph.RemovedMarker{Reason: "deleted"}}},
			DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=x",
		})
	})

	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
	_, err := st.GetMessage(context.Background(), "m-doomed")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestRunCycleEmitsCompletedEvent(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.FolderListResponse{
			Value: []graph.MailFolder{{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"}},
		})
	})
	srv.Handle("/me/mailFolders/f-inbox/messages", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.ListMessagesResponse{Value: []graph.Message{{ID: "m-1"}}})
	})

	require.NoError(t, eng.SyncAll(context.Background()))

	got, err := st.ListMessages(context.Background(), store.MessageQuery{AccountID: acc, FolderID: "f-inbox"})
	require.NoError(t, err)
	require.Len(t, got, 1)

	// Drain events until SyncCompletedEvent appears.
	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-eng.Notifications():
			if c, ok := ev.(SyncCompletedEvent); ok {
				require.Equal(t, 1, c.FoldersSynced)
				return
			}
		case <-deadline:
			t.Fatalf("never received SyncCompletedEvent")
		}
	}
}

func TestFilterSubscribedExcludesJunkAndDeleted(t *testing.T) {
	all := []store.Folder{
		{ID: "1", WellKnownName: "inbox"},
		{ID: "2", WellKnownName: "archive"},
		{ID: "3", WellKnownName: "deleteditems"},
		{ID: "4", WellKnownName: "junkemail"},
		{ID: "5", DisplayName: "User Folder"},
	}
	got := filterSubscribed(all, DefaultSubscribedFolders(), nil)
	gotIDs := make([]string, len(got))
	for i, f := range got {
		gotIDs[i] = f.ID
	}
	require.ElementsMatch(t, []string{"1", "2", "5"}, gotIDs)
}

func TestFilterSubscribedExcludesByDisplayName(t *testing.T) {
	all := []store.Folder{
		{ID: "1", WellKnownName: "inbox"},
		{ID: "2", DisplayName: "User Folder"},
		{ID: "3", DisplayName: "Junk Email"}, // excluded by display name
		{ID: "4", DisplayName: "JUNK EMAIL"}, // case-insensitive
	}
	got := filterSubscribed(all, DefaultSubscribedFolders(), []string{"Junk Email"})
	gotIDs := make([]string, len(got))
	for i, f := range got {
		gotIDs[i] = f.ID
	}
	require.ElementsMatch(t, []string{"1", "2"}, gotIDs)
}

func TestEngineDoneUnblocksConsumer(t *testing.T) {
	eng, _, _, _ := newSyncTest(t)
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopCancel()
	_ = eng.Stop(stopCtx)

	// Done() channel must be closed after Stop().
	select {
	case <-eng.Done():
		// pass
	case <-time.After(time.Second):
		t.Fatal("Done() was not closed after Stop()")
	}
}

func TestEngineActionDrainCalledBeforeFolderSync(t *testing.T) {
	eng, srv, _, _ := newSyncTest(t)
	var folderHit atomic.Int32
	var drainHit atomic.Int32

	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		folderHit.Add(1)
		writeJSON(w, graph.FolderListResponse{Value: nil})
	})
	// Inject a recording drainer.
	e := eng.(*engine)
	e.drain = recordingDrainer{onDrain: func() {
		require.Equal(t, int32(0), folderHit.Load(), "drain MUST happen before folder sync (spec §4)")
		drainHit.Add(1)
	}}

	require.NoError(t, eng.SyncAll(context.Background()))
	require.Equal(t, int32(1), drainHit.Load())
}

type recordingDrainer struct{ onDrain func() }

func (r recordingDrainer) Drain(_ context.Context) error {
	if r.onDrain != nil {
		r.onDrain()
	}
	return nil
}

// TestRunCycleSerialisesConcurrentCallers is the regression for the
// real-tenant log that showed cycle 2 starting at 22:55:19.809 while
// cycle 1 (started 22:55:19.661) didn't end until 22:55:19.880 — two
// runCycle invocations overlapping. Without the cycleMu lock added in
// v0.9.x, parallel SyncAll() goroutines stacked HTTP fan-outs.
func TestRunCycleSerialisesConcurrentCallers(t *testing.T) {
	eng, srv, _, _ := newSyncTest(t)
	var inflight atomic.Int32
	var maxInflight atomic.Int32

	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		cur := inflight.Add(1)
		if cur > maxInflight.Load() {
			maxInflight.Store(cur)
		}
		// Hold the lock briefly so a concurrent caller would race
		// here if cycleMu weren't enforcing serialisation.
		time.Sleep(20 * time.Millisecond)
		inflight.Add(-1)
		writeJSON(w, graph.FolderListResponse{Value: nil})
	})

	// Fire 4 SyncAll calls in parallel.
	var wg stdsync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = eng.SyncAll(context.Background())
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), maxInflight.Load(),
		"runCycle must be serialised — only one in-flight at a time")
}

func TestPrivacyNoBodyContentReachedDuringDeltaSync(t *testing.T) {
	eng, srv, _, acc := newSyncTest(t)
	require.NoError(t, eng.(*engine).st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))

	// v0.2.4: first-launch is /messages, not /messages/delta. The
	// body-column privacy check applies to whichever endpoint is in
	// use, so we attach the same handler to both routes.
	check := func(w http.ResponseWriter, r *http.Request) {
		sel := r.URL.Query().Get("$select")
		fields := strings.Split(sel, ",")
		for _, f := range fields {
			require.NotEqual(t, "body", strings.TrimSpace(f),
				"delta sync must NEVER request body content (spec §5.2)")
		}
		writeJSON(w, graph.ListMessagesResponse{Value: nil})
	}
	srv.Handle("/me/mailFolders/f-inbox/messages", check)
	srv.Handle("/me/mailFolders/f-inbox/messages/delta", check)
	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
}

func TestParseRetryAfterIsExportedThroughGraphPackageContract(t *testing.T) {
	// Sanity: confirm the graph package classification helpers are
	// used by sync via the same contract we test in graph_test.
	_ = graph.IsThrottled
	_ = graph.IsAuth
	_ = graph.IsSyncStateNotFound
	_ = graph.IsNotFound
}

func TestQuickStartBackfillInboxFirst(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.FolderListResponse{
			Value: []graph.MailFolder{
				{ID: "f-archive", DisplayName: "Archive", WellKnownName: "archive"},
				{ID: "f-zebra", DisplayName: "ZebraTalk"},
				{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
				{ID: "f-aardvarks", DisplayName: "Aardvarks"},
			},
		})
	})

	// Track the ORDER folders are hit during quick-start. v0.2.4
	// switched from /messages/delta to /messages?$orderby for the
	// first-launch path; this test follows.
	var order []string
	var orderMu stdsync.Mutex
	for _, fid := range []string{"f-inbox", "f-archive", "f-zebra", "f-aardvarks"} {
		fid := fid
		srv.Handle("/me/mailFolders/"+fid+"/messages", func(w http.ResponseWriter, _ *http.Request) {
			orderMu.Lock()
			order = append(order, fid)
			orderMu.Unlock()
			writeJSON(w, graph.ListMessagesResponse{Value: nil})
		})
	}

	require.NoError(t, eng.(*engine).QuickStartBackfill(context.Background()))

	// Inbox first, then well-known (archive), then user folders alpha.
	orderMu.Lock()
	defer orderMu.Unlock()
	require.Equal(t, []string{"f-inbox", "f-archive", "f-aardvarks", "f-zebra"}, order,
		"quick-start must hit Inbox before Archive before user folders alpha")

	folders, err := st.ListFolders(context.Background(), acc)
	require.NoError(t, err)
	require.Len(t, folders, 4, "all enumerated folders persisted")
}

func TestQuickStartBackfillUsesTop50AndOrderByReceivedDateTime(t *testing.T) {
	// v0.2.4: first-launch hits /messages with $top=50 AND
	// $orderby=receivedDateTime desc. The Graph delta endpoint
	// doesn't support $orderby in v1.0, so we use the non-delta
	// endpoint to guarantee newest-first.
	eng, srv, _, _ := newSyncTest(t)
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.FolderListResponse{
			Value: []graph.MailFolder{{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"}},
		})
	})
	var topSeen, orderbySeen string
	srv.Handle("/me/mailFolders/f-inbox/messages", func(w http.ResponseWriter, r *http.Request) {
		topSeen = r.URL.Query().Get("$top")
		orderbySeen = r.URL.Query().Get("$orderby")
		writeJSON(w, graph.ListMessagesResponse{Value: nil})
	})
	require.NoError(t, eng.(*engine).QuickStartBackfill(context.Background()))
	require.Equal(t, "50", topSeen, "first-launch must pin $top=50")
	require.Equal(t, "receivedDateTime desc", orderbySeen,
		"first-launch must order by receivedDateTime desc so the user sees newest mail first")
}

func TestSyncFolderResumesPersistedNextLink(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))
	// Pre-seed a next_link as if a prior tick had drained one page.
	resumeURL := srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$skiptoken=mid"
	require.NoError(t, st.PutDeltaToken(context.Background(), store.DeltaToken{
		AccountID: acc, FolderID: "f-inbox", NextLink: resumeURL,
	}))

	var hits atomic.Int32
	srv.Handle("/me/mailFolders/f-inbox/messages/delta", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		require.Equal(t, "mid", r.URL.Query().Get("$skiptoken"), "next_link must be followed verbatim")
		writeJSON(w, graph.DeltaResponse{
			Value:     []graph.Message{{ID: "m-resumed"}},
			DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=done",
		})
	})

	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
	require.Equal(t, int32(1), hits.Load())

	tok, err := st.GetDeltaToken(context.Background(), acc, "f-inbox")
	require.NoError(t, err)
	require.Empty(t, tok.NextLink, "next_link cleared on completion")
	require.Contains(t, tok.DeltaLink, "deltatoken=done")
}

func TestSyncFolderQuickStartPersistsLastDeltaAt(t *testing.T) {
	// v0.2.4: first-launch persists LastDeltaAt only (no DeltaLink,
	// no NextLink). The next tick takes the pullSince path, which
	// uses $filter=receivedDateTime gt {LastDeltaAt}.
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))

	srv.Handle("/me/mailFolders/f-inbox/messages", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "50", r.URL.Query().Get("$top"))
		require.Equal(t, "receivedDateTime desc", r.URL.Query().Get("$orderby"))
		writeJSON(w, graph.ListMessagesResponse{Value: []graph.Message{{ID: "m-1"}}})
	})

	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
	tok, err := st.GetDeltaToken(context.Background(), acc, "f-inbox")
	require.NoError(t, err)
	require.Empty(t, tok.NextLink, "quick-start does not seed a delta cursor")
	require.Empty(t, tok.DeltaLink, "quick-start does not seed a delta cursor")
	require.False(t, tok.LastDeltaAt.IsZero(), "LastDeltaAt is set so the next tick takes pullSince")
}

func TestSyncFolderPullSinceUsesFilter(t *testing.T) {
	// After quick-start, subsequent ticks use /messages with a
	// $filter=receivedDateTime gt {last_delta_at} clause to fetch
	// any new messages received since.
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))
	prior := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, st.PutDeltaToken(context.Background(), store.DeltaToken{
		AccountID:   acc,
		FolderID:    "f-inbox",
		LastDeltaAt: prior,
	}))

	var filterSeen string
	srv.Handle("/me/mailFolders/f-inbox/messages", func(w http.ResponseWriter, r *http.Request) {
		filterSeen = r.URL.Query().Get("$filter")
		writeJSON(w, graph.ListMessagesResponse{Value: []graph.Message{{ID: "m-new"}}})
	})

	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
	require.Contains(t, filterSeen, "receivedDateTime gt ")
	require.Contains(t, filterSeen, prior.Format("2006-01-02"),
		"$filter must include the persisted last-delta timestamp")
}

// TestEngineForwardsThrottleAsEvent is the spec-03-§3 invariant:
// the graph client's OnThrottle hook, wired through the engine,
// must surface as a ThrottledEvent on the Notifications channel.
// Without this the UI's `case isync.ThrottledEvent` is dead code
// and a 429-storm degrades sync silently.
func TestEngineForwardsThrottleAsEvent(t *testing.T) {
	eng, _, _, _ := newSyncTest(t)
	// The engine satisfies the OnThrottle hook contract; calling it
	// directly is what the cmd_run.go closure does at runtime.
	eng.OnThrottle(2 * time.Second)

	select {
	case ev := <-eng.Notifications():
		thr, ok := ev.(ThrottledEvent)
		require.True(t, ok, "expected ThrottledEvent, got %T", ev)
		require.Equal(t, 2*time.Second, thr.RetryAfter)
	case <-time.After(time.Second):
		t.Fatal("ThrottledEvent never emitted")
	}
}

// TestEngineGraphClientIntegrationEmitsThrottle is the wired-up
// version of the test above: a real graph.Client backed by a 429-
// returning httptest server, with OnThrottle pointed at the engine
// via the same closure pattern as cmd_run.go.
func TestEngineGraphClientIntegrationEmitsThrottle(t *testing.T) {
	st := openSyncTestStore(t)
	acc := seedSyncAccount(t, st)

	srv := newFakeServer()
	defer srv.Close()
	var hits atomic.Int32
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeJSON(w, graph.FolderListResponse{Value: []graph.MailFolder{
			{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
		}})
	})

	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	var eng Engine
	gc, err := graph.NewClient(&fakeAuth{}, graph.Options{
		BaseURL: srv.URL(),
		Logger:  logger,
		OnThrottle: func(d time.Duration) {
			if eng != nil {
				eng.OnThrottle(d)
			}
		},
	})
	require.NoError(t, err)
	eng, err = New(gc, st, nil, Options{
		AccountID: acc, Logger: logger,
		ForegroundInterval: 50 * time.Millisecond,
		BackgroundInterval: time.Second,
	})
	require.NoError(t, err)

	// Drive a folder-enumeration cycle; the 429 fires the hook.
	_ = eng.(*engine).syncFolders(context.Background())

	select {
	case ev := <-eng.Notifications():
		_, ok := ev.(ThrottledEvent)
		require.True(t, ok, "expected ThrottledEvent, got %T", ev)
	case <-time.After(2 * time.Second):
		t.Fatal("ThrottledEvent not emitted from graph.Client → engine path")
	}
}

// TestEngineEmitsAuthRequiredOn401 covers the spec-03-§3 invariant
// for the auth half: a cycle that returns IsAuth(err) must surface
// as AuthRequiredEvent (UI transitions to sign-in) rather than the
// generic SyncFailedEvent.
func TestEngineEmitsAuthRequiredOn401(t *testing.T) {
	st := openSyncTestStore(t)
	acc := seedSyncAccount(t, st)

	srv := newFakeServer()
	defer srv.Close()
	srv.Handle("/me/mailFolders/delta", func(w http.ResponseWriter, _ *http.Request) {
		// Always 401 so the auth retry also fails; this is the
		// "user's tokens were revoked at the tenant" case.
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":"InvalidAuthenticationToken","message":"token expired"}}`)
	})

	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	gc, err := graph.NewClient(&fakeAuth{}, graph.Options{
		BaseURL: srv.URL(),
		Logger:  logger,
	})
	require.NoError(t, err)
	eng, err := New(gc, st, nil, Options{
		AccountID: acc, Logger: logger,
		ForegroundInterval: 50 * time.Millisecond,
		BackgroundInterval: time.Second,
	})
	require.NoError(t, err)

	// Run one cycle; the 401 propagates through runCycle → loop
	// → emitCycleFailure → AuthRequiredEvent. Drive runCycle
	// directly so we don't have to start the goroutine loop.
	err = eng.(*engine).runCycle(context.Background())
	require.Error(t, err)
	require.True(t, graph.IsAuth(err), "test setup: cycle error must classify as auth")

	// emitCycleFailure is what the loop wraps runCycle errors with.
	eng.(*engine).emitCycleFailure(err)

	select {
	case ev := <-eng.Notifications():
		// Drain through any FoldersEnumeratedEvent / SyncStartedEvent
		// noise that runCycle may have emitted along the way.
		for {
			if _, ok := ev.(AuthRequiredEvent); ok {
				return
			}
			select {
			case ev = <-eng.Notifications():
			case <-time.After(time.Second):
				t.Fatal("AuthRequiredEvent never emitted after IsAuth error")
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no events emitted")
	}
}

// helper: silence unused-import in some build configurations.
var _ = fmt.Sprintf
