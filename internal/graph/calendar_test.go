package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestListCalendarDeltaFirstCall verifies that an empty deltaLink triggers
// a fresh /me/calendarView/delta?startDateTime=...&endDateTime=... request,
// returns events, and surfaces the deltaLink for the next call.
func TestListCalendarDeltaFirstCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		require.Contains(t, r.URL.Path, "/me/calendarView/delta")
		require.Contains(t, r.URL.RawQuery, "startDateTime")
		require.Contains(t, r.URL.RawQuery, "endDateTime")

		resp := map[string]any{
			"value": []map[string]any{{
				"id":      "ev-1",
				"subject": "Standup",
				"start":   map[string]any{"dateTime": "2026-05-01T09:00:00.0000000"},
				"end":     map[string]any{"dateTime": "2026-05-01T09:30:00.0000000"},
				"isAllDay": false,
				"organizer": map[string]any{"emailAddress": map[string]any{
					"name": "Alice", "address": "alice@example.invalid",
				}},
				"showAs":  "busy",
				"webLink": "https://outlook.example.invalid/event/1",
			}},
			"@odata.deltaLink": "https://graph.microsoft.com/v1.0/me/calendarView/delta?$deltatoken=abc123",
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)

	result, err := c.ListCalendarDelta(context.Background(), start, end, "")
	require.NoError(t, err)
	require.True(t, called, "must hit the server")
	require.Len(t, result.Events, 1)
	require.Equal(t, "ev-1", result.Events[0].ID)
	require.Equal(t, "Standup", result.Events[0].Subject)
	require.Equal(t, "alice@example.invalid", result.Events[0].OrganizerAddress)
	require.Equal(t, "Alice", result.Events[0].OrganizerName)
	require.Equal(t, "https://graph.microsoft.com/v1.0/me/calendarView/delta?$deltatoken=abc123", result.DeltaLink)
	require.Empty(t, result.Removed)
}

// TestListCalendarDeltaRemovedEntries verifies that items with @removed
// go to result.Removed and are excluded from result.Events.
func TestListCalendarDeltaRemovedEntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"value": []map[string]any{
				{
					"id":      "ev-gone",
					"@removed": map[string]any{"reason": "deleted"},
				},
				{
					"id":      "ev-2",
					"subject": "Still here",
					"start":   map[string]any{"dateTime": "2026-05-02T10:00:00.0000000"},
					"end":     map[string]any{"dateTime": "2026-05-02T11:00:00.0000000"},
					"isAllDay": false,
					"organizer": map[string]any{"emailAddress": map[string]any{"name": "", "address": ""}},
				},
			},
			"@odata.deltaLink": "https://example.invalid/delta?tok=xyz",
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	result, err := c.ListCalendarDelta(context.Background(),
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC),
		"",
	)
	require.NoError(t, err)
	require.Len(t, result.Events, 1, "only non-removed events go to Events")
	require.Equal(t, "ev-2", result.Events[0].ID)
	require.Equal(t, []string{"ev-gone"}, result.Removed)
}

// TestListCalendarDeltaWithDeltaLink confirms that a non-empty deltaLink
// is used as the endpoint directly (the full URL is forwarded as-is).
func TestListCalendarDeltaWithDeltaLink(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		resp := map[string]any{
			"value":            []map[string]any{},
			"@odata.deltaLink": srv.URL + "/me/calendarView/delta?$deltatoken=tok99",
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	testDeltaLink := srv.URL + "/me/calendarView/delta?$deltatoken=tok99"

	result, err := c.ListCalendarDelta(context.Background(),
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC),
		testDeltaLink,
	)
	require.NoError(t, err)
	require.Contains(t, gotQuery, "deltatoken=tok99",
		"deltaLink must be used verbatim; query must contain the deltatoken")
	// When the delta link is supplied, startDateTime must NOT appear — the
	// server builds the window from the token, not fresh params.
	require.False(t, strings.Contains(gotQuery, "startDateTime"),
		"startDateTime must not be sent when deltaLink is provided")
	require.Empty(t, result.Events)
	require.Empty(t, result.Removed)
}

// TestListCalendarDeltaSurfaces410 verifies that a 410 response is returned
// as an error that IsSyncStateNotFound can classify. The sync engine handles
// the reset; the graph layer just surfaces the error faithfully.
func TestListCalendarDeltaSurfaces410(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":{"code":"syncStateNotFound","message":"delta expired"}}`))
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	_, err = c.ListCalendarDelta(context.Background(),
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC),
		"",
	)
	require.Error(t, err)
	require.True(t, IsSyncStateNotFound(err), "410 syncStateNotFound must be classifiable by IsSyncStateNotFound")
}
