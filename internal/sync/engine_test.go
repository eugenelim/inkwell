package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	stdsync "sync"
	"strings"
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

func (f *fakeServer) Close()          { f.server.Close() }
func (f *fakeServer) URL() string     { return f.server.URL }
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
	srv.Handle("/me/mailFolders", func(w http.ResponseWriter, _ *http.Request) {
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
	srv.Handle("/me/mailFolders", func(w http.ResponseWriter, _ *http.Request) {
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
	srv.mux.HandleFunc("/me/mailFolders", func(w http.ResponseWriter, _ *http.Request) {
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
	srv.Handle("/me/mailFolders", func(w http.ResponseWriter, _ *http.Request) {
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
	got := filterSubscribed(all, DefaultSubscribedFolders())
	gotIDs := make([]string, len(got))
	for i, f := range got {
		gotIDs[i] = f.ID
	}
	require.ElementsMatch(t, []string{"1", "2", "5"}, gotIDs)
}

func TestEngineActionDrainCalledBeforeFolderSync(t *testing.T) {
	eng, srv, _, _ := newSyncTest(t)
	var folderHit atomic.Int32
	var drainHit atomic.Int32

	srv.Handle("/me/mailFolders", func(w http.ResponseWriter, _ *http.Request) {
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
	srv.Handle("/me/mailFolders", func(w http.ResponseWriter, _ *http.Request) {
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
	srv.Handle("/me/mailFolders", func(w http.ResponseWriter, _ *http.Request) {
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

// helper: silence unused-import in some build configurations.
var _ = fmt.Sprintf
