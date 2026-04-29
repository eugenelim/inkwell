package sync

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// TestBackfillUsesLessThanForOlderMessages is the regression for the
// scroll-to-load-more bug shipped in v0.10.0: backfillFolder used
// `receivedDateTime ge <until>` (NEWER-than), so when the UI passed
// the oldest cached message's date as the cutoff, Graph returned
// what we already had. The fix uses `lt` and orders desc.
func TestBackfillUsesLessThanForOlderMessages(t *testing.T) {
	eng, srv, st, accID := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: accID, DisplayName: "Inbox", WellKnownName: "inbox",
		LastSyncedAt: time.Now(),
	}))

	var capturedFilter, capturedOrder atomic.Pointer[string]
	srv.Handle("/me/mailFolders/f-inbox/messages", func(w http.ResponseWriter, r *http.Request) {
		q, _ := url.ParseQuery(r.URL.RawQuery)
		f := q.Get("$filter")
		o := q.Get("$orderby")
		capturedFilter.Store(&f)
		capturedOrder.Store(&o)
		writeJSON(w, graph.ListMessagesResponse{
			Value: []graph.Message{
				{ID: "m-old-1", ReceivedDateTime: time.Now().Add(-48 * time.Hour),
					From: &graph.Recipient{EmailAddress: graph.EmailAddress{Address: "x@x"}}},
			},
		})
	})

	cutoff := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	require.NoError(t, eng.Backfill(context.Background(), "f-inbox", cutoff))

	gotFilter := capturedFilter.Load()
	gotOrder := capturedOrder.Load()
	require.NotNil(t, gotFilter)
	require.Contains(t, *gotFilter, "lt", "must filter by lt (older than), not ge (newer than)")
	require.Contains(t, *gotFilter, "2026-04-29T12:00:00Z")
	require.NotNil(t, gotOrder)
	require.Equal(t, "receivedDateTime desc", *gotOrder,
		"newest-of-older first matches the scroll direction")
}

// TestBackfillSinglePageNoFollowingNextLink confirms the function
// fetches exactly ONE page even if Graph returns nextLink. The UI's
// scroll-to-wall fires once per cache state; if the user scrolls
// further, the next trigger pulls the next page. We never recurse
// to completion — a years-old account would otherwise burn thousands
// of HTTP calls per scroll.
func TestBackfillSinglePageNoFollowingNextLink(t *testing.T) {
	eng, srv, st, accID := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: accID, DisplayName: "Inbox", WellKnownName: "inbox",
		LastSyncedAt: time.Now(),
	}))

	var calls atomic.Int32
	srv.Handle("/me/mailFolders/f-inbox/messages", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, graph.ListMessagesResponse{
			Value: []graph.Message{
				{ID: fmt.Sprintf("m-%d", calls.Load()), ReceivedDateTime: time.Now(),
					From: &graph.Recipient{EmailAddress: graph.EmailAddress{Address: "x@x"}}},
			},
			NextLink: srv.URL() + "/me/mailFolders/f-inbox/messages?page=2",
		})
	})

	require.NoError(t, eng.Backfill(context.Background(), "f-inbox", time.Now()))
	require.Equal(t, int32(1), calls.Load(),
		"backfill must fetch ONE page; nextLink is for the next scroll trigger")
}

// TestBackfillZeroTimeFallsBackToNewest covers the no-pre-cache case:
// the UI may pass the zero time when nothing's loaded yet (e.g. a
// brand-new folder). Graph then returns the newest page.
func TestBackfillZeroTimeFallsBackToNewest(t *testing.T) {
	eng, srv, st, accID := newSyncTest(t)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: accID, DisplayName: "Inbox", WellKnownName: "inbox",
		LastSyncedAt: time.Now(),
	}))

	var capturedFilter atomic.Pointer[string]
	srv.Handle("/me/mailFolders/f-inbox/messages", func(w http.ResponseWriter, r *http.Request) {
		q, _ := url.ParseQuery(r.URL.RawQuery)
		f := q.Get("$filter")
		capturedFilter.Store(&f)
		writeJSON(w, graph.ListMessagesResponse{Value: nil})
	})

	require.NoError(t, eng.Backfill(context.Background(), "f-inbox", time.Time{}))
	got := capturedFilter.Load()
	require.NotNil(t, got)
	require.Empty(t, *got, "zero time → no filter (Graph returns newest by default)")
}
