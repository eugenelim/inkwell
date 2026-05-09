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

	// Pre-seed an event that the delta response will @remove. Use a
	// time inside the default lookback window (now is the conservative
	// choice — the prune step deletes events strictly older than
	// truncateToDay(now) - lookbackDays).
	inWindow := time.Now().UTC().Add(time.Hour)
	require.NoError(t, st.PutEvent(context.Background(), store.Event{
		ID: "ev-stale", AccountID: acc, Start: inWindow, End: inWindow.Add(time.Hour),
	}))

	freshStart := inWindow.Format("2006-01-02T15:04:05.0000000")
	freshEnd := inWindow.Add(time.Hour).Format("2006-01-02T15:04:05.0000000")
	srv.Handle("/me/calendarView/delta", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"value": []map[string]any{
				{
					"id": "ev-new", "subject": "Fresh event",
					"start":    map[string]any{"dateTime": freshStart},
					"end":      map[string]any{"dateTime": freshEnd},
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

	// Use a time inside the default lookback window so the event is not
	// pruned right after upsert when the test runs on a date past the
	// hardcoded fixture date.
	inWindow := time.Now().UTC().Add(time.Hour)
	freshStart := inWindow.Format("2006-01-02T15:04:05.0000000")
	freshEnd := inWindow.Add(time.Hour).Format("2006-01-02T15:04:05.0000000")
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
				"start":    map[string]any{"dateTime": freshStart},
				"end":      map[string]any{"dateTime": freshEnd},
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

// TestTruncateToDayMidnightUTC verifies that truncateToDay always returns
// midnight UTC regardless of the input time component (spec 12 §11).
func TestTruncateToDayMidnightUTC(t *testing.T) {
	cases := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{
			name: "noon UTC",
			in:   time.Date(2026, 5, 1, 12, 30, 59, 0, time.UTC),
			want: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "already midnight",
			in:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			want: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "one nanosecond before midnight",
			in:   time.Date(2026, 5, 1, 23, 59, 59, 999999999, time.UTC),
			want: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, truncateToDay(tc.in))
		})
	}
}

// TestTruncateToDayDSTBoundary verifies correct UTC midnight for inputs
// near a DST boundary (spec 12 §11: "Window slide computes correct UTC
// bounds for various time zones").
func TestTruncateToDayDSTBoundary(t *testing.T) {
	nyc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	// 2026-03-08 is DST spring-forward day. 02:30 NYC = 07:30 UTC.
	// truncateToDay operates on UTC, so result must be 2026-03-08 00:00 UTC.
	nycTime := time.Date(2026, 3, 8, 2, 30, 0, 0, nyc).UTC()
	got := truncateToDay(nycTime)
	require.Equal(t, time.UTC, got.Location())
	y, m, d := got.Date()
	h, min, sec := got.Clock()
	require.Equal(t, 2026, y)
	require.Equal(t, time.March, m)
	require.Equal(t, 8, d)
	require.Equal(t, 0, h, "hour must be 0 (midnight UTC)")
	require.Equal(t, 0, min)
	require.Equal(t, 0, sec)
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
