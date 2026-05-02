package sync

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// TestSyncCalendarUpsertsAndDeletesEvents verifies the core delta-sync
// contract: new events are upserted, @removed IDs are deleted from the
// cache, and the deltaLink is persisted for the next pass.
func TestSyncCalendarUpsertsAndDeletesEvents(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)

	// Pre-seed an event that the delta response will @remove.
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, st.PutEvent(context.Background(), store.Event{
		ID: "ev-stale", AccountID: acc, Start: now, End: now.Add(time.Hour),
	}))

	srv.Handle("/me/calendarView/delta", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"value": []map[string]any{
				{
					"id": "ev-new", "subject": "Fresh event",
					"start":    map[string]any{"dateTime": "2026-05-01T10:00:00.0000000"},
					"end":      map[string]any{"dateTime": "2026-05-01T11:00:00.0000000"},
					"isAllDay": false,
					"organizer": map[string]any{
						"emailAddress": map[string]any{"name": "Bob", "address": "bob@example.invalid"},
					},
				},
				{"id": "ev-stale", "@removed": map[string]any{"reason": "deleted"}},
			},
			"@odata.deltaLink": "https://example.invalid/delta?tok=v1",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	require.NoError(t, eng.(*engine).syncCalendar(context.Background()))

	// ev-stale must be gone; ev-new must be upserted.
	got, err := st.ListEvents(context.Background(), store.EventQuery{AccountID: acc})
	require.NoError(t, err)
	require.Len(t, got, 1, "ev-stale must be deleted; ev-new must be upserted")
	require.Equal(t, "ev-new", got[0].ID)
	require.Equal(t, "Fresh event", got[0].Subject)
	require.Equal(t, "bob@example.invalid", got[0].OrganizerAddress)

	// Delta token must be persisted for the next call.
	tok, err := st.GetDeltaToken(context.Background(), acc, "__calendar__")
	require.NoError(t, err)
	require.NotNil(t, tok, "delta token must be stored after sync")
	require.Equal(t, "https://example.invalid/delta?tok=v1", tok.DeltaLink)
}

// TestSyncCalendar410ResetsAndReFetches exercises the syncStateNotFound
// path: a 410 response must cause the engine to clear the stored delta
// token and retry with a fresh full-window request.
func TestSyncCalendar410ResetsAndReFetches(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)

	callCount := 0
	srv.Handle("/me/calendarView/delta", func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusGone)
			_, _ = io.WriteString(w, `{"error":{"code":"syncStateNotFound","message":"delta token expired"}}`)
			return
		}
		// Second call: fresh full-window response.
		resp := map[string]any{
			"value": []map[string]any{{
				"id": "ev-fresh", "subject": "Refetched",
				"start":    map[string]any{"dateTime": "2026-05-01T10:00:00.0000000"},
				"end":      map[string]any{"dateTime": "2026-05-01T11:00:00.0000000"},
				"isAllDay": false,
				"organizer": map[string]any{
					"emailAddress": map[string]any{"name": "", "address": ""},
				},
			}},
			"@odata.deltaLink": "https://example.invalid/delta?tok=fresh",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	require.NoError(t, eng.(*engine).syncCalendar(context.Background()))

	require.Equal(t, 2, callCount, "engine must retry after 410 SyncStateNotFound")

	got, err := st.ListEvents(context.Background(), store.EventQuery{AccountID: acc})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "ev-fresh", got[0].ID)
}

// TestSyncCalendarPrunesOldEvents confirms that events whose start_at
// predates (now - lookback) are removed after a sync pass.
func TestSyncCalendarPrunesOldEvents(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)

	// Seed a very old event that should be pruned after the window slide.
	ancient := time.Now().UTC().Add(-365 * 24 * time.Hour) // ~1 year ago
	require.NoError(t, st.PutEvent(context.Background(), store.Event{
		ID: "ev-ancient", AccountID: acc, Start: ancient, End: ancient.Add(time.Hour),
	}))

	srv.Handle("/me/calendarView/delta", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"value":            []map[string]any{},
			"@odata.deltaLink": "https://example.invalid/delta?tok=prune",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	require.NoError(t, eng.(*engine).syncCalendar(context.Background()))

	got, err := st.ListEvents(context.Background(), store.EventQuery{AccountID: acc})
	require.NoError(t, err)
	require.Empty(t, got, "events outside the sync window must be pruned")
}

// TestSyncCalendarUsesStoredDeltaLink confirms that when a delta token
// exists in the store, it is passed to the Graph call verbatim (the full
// URL is forwarded rather than rebuilt from scratch).
func TestSyncCalendarUsesStoredDeltaLink(t *testing.T) {
	eng, srv, st, acc := newSyncTest(t)

	var gotQuery string
	srv.Handle("/me/calendarView/delta", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		resp := map[string]any{
			"value":            []map[string]any{},
			"@odata.deltaLink": srv.URL() + "/me/calendarView/delta?$deltatoken=existing",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Pre-seed a delta token pointing at the test server so the engine
	// can follow the URL in tests.
	storedLink := srv.URL() + "/me/calendarView/delta?$deltatoken=existing"
	require.NoError(t, st.PutDeltaToken(context.Background(), store.DeltaToken{
		AccountID:   acc,
		FolderID:    "__calendar__",
		DeltaLink:   storedLink,
		LastDeltaAt: time.Now(),
	}))

	require.NoError(t, eng.(*engine).syncCalendar(context.Background()))

	require.Contains(t, gotQuery, "deltatoken=existing",
		"stored delta link must be used verbatim as the request URL")
	// When the delta link is used, startDateTime must not appear — the
	// server controls the window via the token.
	require.NotContains(t, gotQuery, "startDateTime",
		"startDateTime must not appear when delta token is used")
}
