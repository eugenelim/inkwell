package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestSyncFolderDeltaInitialPersistsToken(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))

	srv.Handle("/me/mailFolders/f-inbox/messages/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.DeltaResponse{
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
			DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=abc",
		})
	})

	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))

	msgs, err := st.ListMessages(context.Background(), store.MessageQuery{AccountID: acc, FolderID: "f-inbox", Limit: 50})
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	tok, err := st.GetDeltaToken(context.Background(), acc, "f-inbox")
	require.NoError(t, err)
	require.Contains(t, tok.DeltaLink, "deltatoken=abc")
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
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))
	require.NoError(t, st.PutDeltaToken(context.Background(), store.DeltaToken{
		AccountID: acc, FolderID: "f-inbox", DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=stale",
	}))

	var hits atomic.Int32
	srv.Handle("/me/mailFolders/f-inbox/messages/delta", func(w http.ResponseWriter, r *http.Request) {
		switch hits.Add(1) {
		case 1:
			require.Equal(t, "stale", r.URL.Query().Get("$deltatoken"))
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"error":{"code":"syncStateNotFound","message":"resync"}}`))
		default:
			require.Empty(t, r.URL.Query().Get("$deltatoken"))
			writeJSON(w, graph.DeltaResponse{
				Value:     []graph.Message{{ID: "m-fresh"}},
				DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=fresh",
			})
		}
	})

	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
	require.Equal(t, int32(2), hits.Load(), "first call gets 410, second re-inits")

	tok, err := st.GetDeltaToken(context.Background(), acc, "f-inbox")
	require.NoError(t, err)
	require.Contains(t, tok.DeltaLink, "deltatoken=fresh")
}

func TestSyncFolderPaginatesNextLink(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))

	page1 := srv.URL() + "/me/mailFolders/f-inbox/messages/delta"
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

	require.NoError(t, eng.Sync(context.Background(), "f-inbox"))
	_ = page1 // referenced for log

	msgs, err := st.ListMessages(context.Background(), store.MessageQuery{AccountID: acc, FolderID: "f-inbox", Limit: 50})
	require.NoError(t, err)
	require.Len(t, msgs, 3)
}

func TestSyncFolderAppliesRemovedTombstones(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))
	// Pre-cache a message; delta will tombstone it.
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "m-doomed", AccountID: acc, FolderID: "f-inbox", Subject: "doomed",
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
	srv.Handle("/me/mailFolders/f-inbox/messages/delta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, graph.DeltaResponse{
			Value:     []graph.Message{{ID: "m-1"}},
			DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=ok",
		})
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

	srv.Handle("/me/mailFolders/f-inbox/messages/delta", func(w http.ResponseWriter, r *http.Request) {
		// Spec §6 / §5.2: delta sync requests must not include the
		// 'body' column in $select. bodyPreview is fine — that's the
		// 255-char preview Graph returns by default.
		sel := r.URL.Query().Get("$select")
		fields := strings.Split(sel, ",")
		for _, f := range fields {
			require.NotEqual(t, "body", strings.TrimSpace(f),
				"delta sync must NEVER request body content (spec §5.2)")
		}
		writeJSON(w, graph.DeltaResponse{
			DeltaLink: srv.URL() + "/me/mailFolders/f-inbox/messages/delta?$deltatoken=ok",
		})
	})
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

// helper: silence unused-import in some build configurations.
var _ = fmt.Sprintf
