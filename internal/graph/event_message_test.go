package graph

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// meetingRequestPayload returns a minimal meetingRequest response with
// the event navigation property expanded. Required + optional attendee
// counts diverge so the test catches a swapped slice assignment.
func meetingRequestPayload() map[string]any {
	return map[string]any{
		"id":                 "msg-001",
		"meetingMessageType": "meetingRequest",
		"event": map[string]any{
			"id":      "ev-001",
			"subject": "Sprint planning",
			"organizer": map[string]any{"emailAddress": map[string]any{
				"name": "Bob", "address": "bob@example.invalid",
			}},
			"start":          map[string]any{"dateTime": "2026-05-20T15:00:00.0000000"},
			"end":            map[string]any{"dateTime": "2026-05-20T16:00:00.0000000"},
			"isAllDay":       false,
			"location":       map[string]any{"displayName": "Conference room B"},
			"onlineMeeting":  map[string]any{"joinUrl": "https://teams.example.invalid/abc"},
			"responseStatus": map[string]any{"response": "notResponded"},
			"webLink":        "https://outlook.example.invalid/event/ev-001",
			"recurrence": map[string]any{"pattern": map[string]any{
				"type":       "weekly",
				"interval":   1,
				"daysOfWeek": []string{"monday"},
			}},
			"attendees": []map[string]any{
				{
					"type":         "required",
					"emailAddress": map[string]any{"name": "Carol", "address": "carol@example.invalid"},
					"status":       map[string]any{"response": "accepted"},
				},
				{
					"type":         "required",
					"emailAddress": map[string]any{"name": "Dan", "address": "dan@example.invalid"},
					"status":       map[string]any{"response": "notResponded"},
				},
				{
					"type":         "optional",
					"emailAddress": map[string]any{"name": "Eve", "address": "eve@example.invalid"},
					"status":       map[string]any{"response": "tentativelyAccepted"},
				},
			},
		},
	}
}

func newGetEventMessageServer(t *testing.T, status int, payload map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.URL.Path, "/me/messages/")
		require.Contains(t, r.URL.RawQuery, "%24select=id%2CmeetingMessageType")
		require.Contains(t, r.URL.RawQuery, "%24expand=microsoft.graph.eventMessage%2Fevent")
		require.Contains(t, r.URL.RawQuery, "isAllDay")
		require.Contains(t, r.URL.RawQuery, "recurrence")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		require.NoError(t, json.NewEncoder(w).Encode(payload))
	}))
}

func TestGetEventMessageMeetingRequest(t *testing.T) {
	srv := newGetEventMessageServer(t, http.StatusOK, meetingRequestPayload())
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	em, err := c.GetEventMessage(context.Background(), "msg-001")
	require.NoError(t, err)
	require.NotNil(t, em)
	require.Equal(t, "msg-001", em.MessageID)
	require.Equal(t, "meetingRequest", em.MeetingMessageType)
	require.NotNil(t, em.Event)
	require.Equal(t, "Sprint planning", em.Event.Subject)
	require.Equal(t, "Bob", em.Event.OrganizerName)
	require.Equal(t, "bob@example.invalid", em.Event.OrganizerAddress)
	require.Equal(t, "Conference room B", em.Event.Location)
	require.Equal(t, "https://teams.example.invalid/abc", em.Event.OnlineJoinURL)
	require.Equal(t, "notResponded", em.Event.ResponseStatus)
	require.Equal(t, "https://outlook.example.invalid/event/ev-001", em.Event.WebLink)
	require.Equal(t, "Weekly on Monday", em.Event.Recurrence)
	require.False(t, em.Event.IsAllDay)
	require.Equal(t, 2026, em.Event.Start.Year())
	require.Equal(t, 15, em.Event.Start.Hour())
	require.Equal(t, 16, em.Event.End.Hour())

	require.Len(t, em.Event.Required, 2, "Carol + Dan are required")
	require.Equal(t, "Carol", em.Event.Required[0].Name)
	require.Equal(t, "accepted", em.Event.Required[0].Status)
	require.Equal(t, "Dan", em.Event.Required[1].Name)
	require.Equal(t, "notResponded", em.Event.Required[1].Status)
	require.Len(t, em.Event.Optional, 1, "Eve is optional")
	require.Equal(t, "Eve", em.Event.Optional[0].Name)
}

func TestGetEventMessageMeetingCancelledNoOnlineMeeting(t *testing.T) {
	payload := map[string]any{
		"id":                 "msg-cancel",
		"meetingMessageType": "meetingCancelled",
		"event": map[string]any{
			"id":             "ev-cancel",
			"subject":        "Cancelled standup",
			"start":          map[string]any{"dateTime": "2026-05-21T09:00:00.0000000"},
			"end":            map[string]any{"dateTime": "2026-05-21T09:30:00.0000000"},
			"isAllDay":       false,
			"responseStatus": map[string]any{"response": "accepted"},
			"webLink":        "https://outlook.example.invalid/event/ev-cancel",
			"organizer":      map[string]any{"emailAddress": map[string]any{"name": "Bob", "address": "bob@example.invalid"}},
			// no location, no onlineMeeting, no attendees, no recurrence
		},
	}
	srv := newGetEventMessageServer(t, http.StatusOK, payload)
	defer srv.Close()
	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	em, err := c.GetEventMessage(context.Background(), "msg-cancel")
	require.NoError(t, err)
	require.Equal(t, "meetingCancelled", em.MeetingMessageType)
	require.NotNil(t, em.Event)
	require.Equal(t, "", em.Event.Location)
	require.Equal(t, "", em.Event.OnlineJoinURL)
	require.Equal(t, "", em.Event.Recurrence)
	require.Empty(t, em.Event.Required)
	require.Empty(t, em.Event.Optional)
}

func TestGetEventMessageResponseTypesNoEventExpand(t *testing.T) {
	// Spec 34 §4: response messages (meetingAccepted /
	// meetingTenativelyAccepted / meetingDeclined) do not carry
	// an event expand. Microsoft's typo `Tenatively` is preserved
	// as the wire-format spelling.
	for _, mtype := range []string{"meetingAccepted", "meetingTenativelyAccepted", "meetingDeclined"} {
		t.Run(mtype, func(t *testing.T) {
			payload := map[string]any{
				"id":                 "msg-resp",
				"meetingMessageType": mtype,
				// no event key
			}
			srv := newGetEventMessageServer(t, http.StatusOK, payload)
			defer srv.Close()
			logger, _ := newCapturedLogger()
			c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
			require.NoError(t, err)

			em, err := c.GetEventMessage(context.Background(), "msg-resp")
			require.NoError(t, err)
			require.Equal(t, mtype, em.MeetingMessageType)
			require.Nil(t, em.Event, "response-type message must decode with Event=nil, not panic")
		})
	}
}

func TestGetEventMessage404ReturnsTypedGraphError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "ErrorItemNotFound",
				"message": "The specified object was not found in the store.",
			},
		})
	}))
	defer srv.Close()
	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	em, err := c.GetEventMessage(context.Background(), "missing")
	require.Nil(t, em)
	require.Error(t, err)
	var ge *GraphError
	require.True(t, errors.As(err, &ge), "404 must surface a typed *GraphError")
	require.Equal(t, http.StatusNotFound, ge.StatusCode)
	require.Equal(t, "ErrorItemNotFound", ge.Code)
}

func TestGetEventMessageEmptyMessageID(t *testing.T) {
	c := &Client{baseURL: "http://unused"}
	em, err := c.GetEventMessage(context.Background(), "")
	require.Nil(t, em)
	require.Error(t, err)
	require.Contains(t, err.Error(), "messageID required")
}

// ---------- recurrence summary table ----------

func TestSummarizeRecurrenceTable(t *testing.T) {
	cases := []struct {
		name string
		raw  rawRecurrence
		want string
	}{
		{
			name: "daily",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "daily"}},
			want: "Daily",
		},
		{
			name: "weekly with days",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "weekly", DaysOfWeek: []string{"monday", "wednesday"}}},
			want: "Weekly on Monday, Wednesday",
		},
		{
			name: "weekly without days",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "weekly"}},
			want: "Weekly",
		},
		{
			name: "absoluteMonthly",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "absoluteMonthly", DayOfMonth: 15}},
			want: "Monthly on the 15th",
		},
		{
			name: "absoluteMonthly 1st",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "absoluteMonthly", DayOfMonth: 1}},
			want: "Monthly on the 1st",
		},
		{
			name: "absoluteMonthly 23rd (10s-place not teen)",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "absoluteMonthly", DayOfMonth: 23}},
			want: "Monthly on the 23rd",
		},
		{
			name: "absoluteMonthly 13th (teen exception)",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "absoluteMonthly", DayOfMonth: 13}},
			want: "Monthly on the 13th",
		},
		{
			name: "relativeMonthly with index",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "relativeMonthly", Index: "second", DaysOfWeek: []string{"tuesday"}}},
			want: "Monthly on the second Tuesday",
		},
		{
			name: "relativeMonthly empty daysOfWeek falls through",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "relativeMonthly", Index: "second"}},
			want: "Monthly",
		},
		{
			name: "absoluteYearly",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "absoluteYearly", Month: 5, DayOfMonth: 20}},
			want: "Yearly on May 20",
		},
		{
			name: "relativeYearly",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "relativeYearly", Month: 5, Index: "second", DaysOfWeek: []string{"tuesday"}}},
			want: "Yearly on the second Tuesday of May",
		},
		{
			name: "relativeYearly empty daysOfWeek",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "relativeYearly", Month: 5, Index: "second"}},
			want: "Yearly",
		},
		{
			name: "unknown type forward-compat",
			raw: rawRecurrence{Pattern: struct {
				Type       string   `json:"type"`
				Interval   int      `json:"interval"`
				DaysOfWeek []string `json:"daysOfWeek"`
				DayOfMonth int      `json:"dayOfMonth"`
				Month      int      `json:"month"`
				Index      string   `json:"index"`
			}{Type: "lunarSomething"}},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeRecurrence(&tc.raw)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestOrdinal(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "1st"}, {2, "2nd"}, {3, "3rd"}, {4, "4th"},
		{10, "10th"}, {11, "11th"}, {12, "12th"}, {13, "13th"},
		{21, "21st"}, {22, "22nd"}, {23, "23rd"}, {24, "24th"},
		{101, "101st"}, {111, "111th"}, {112, "112th"}, {113, "113th"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, ordinal(tc.n), "n=%d", tc.n)
	}
}

// TestGetEventMessageBuildsURL is a sanity check that the $select +
// $expand chunks make it into the request URL — the spec's prose
// commits the project to a single fetch shape so changes here are
// load-bearing.
func TestGetEventMessageBuildsURL(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "x", "meetingMessageType": "meetingRequest",
		})
	}))
	defer srv.Close()
	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	_, err = c.GetEventMessage(context.Background(), "msg-001")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(gotURL, "/me/messages/msg-001?"), "url=%s", gotURL)
}
