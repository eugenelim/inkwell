package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGetMailboxSettingsDecodesAllFields verifies that GetMailboxSettings
// populates TimeZone, Language, WorkingHoursDisplay, DateFormat, and TimeFormat.
func TestGetMailboxSettingsDecodesAllFields(t *testing.T) {
	payload := `{
		"timeZone": "Pacific Standard Time",
		"language": {"locale": "en-US"},
		"dateFormat": "M/d/yyyy",
		"timeFormat": "h:mm tt",
		"workingHours": {
			"daysOfWeek": ["monday","tuesday","wednesday","thursday","friday"],
			"startTime": "09:00:00",
			"endTime": "17:00:00"
		},
		"automaticRepliesSetting": {
			"status": "disabled",
			"internalReplyMessage": "",
			"externalReplyMessage": "",
			"externalAudience": "none"
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.URL.Path, "/me/mailboxSettings")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	got, err := c.GetMailboxSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Pacific Standard Time", got.TimeZone)
	require.Equal(t, "en-US", got.Language)
	require.Equal(t, "M/d/yyyy", got.DateFormat)
	require.Equal(t, "h:mm tt", got.TimeFormat)
	require.Equal(t, "Mon–Fri 09:00–17:00", got.WorkingHoursDisplay)
}

// TestUpdateAutoRepliesScheduledIncludesSchedule verifies that when status is
// "scheduled" and both ScheduledStart/End are set, the PATCH body includes them.
func TestUpdateAutoRepliesScheduledIncludesSchedule(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	err = c.UpdateAutoReplies(context.Background(), AutoRepliesSetting{
		Status:               AutoReplyScheduled,
		InternalReplyMessage: "OOO internal",
		ExternalReplyMessage: "OOO external",
		ExternalAudience:     "all",
		ScheduledStart:       &DateTimeTimeZone{DateTime: "2026-04-28T09:00:00", TimeZone: "UTC"},
		ScheduledEnd:         &DateTimeTimeZone{DateTime: "2026-05-05T17:00:00", TimeZone: "UTC"},
	})
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(capturedBody, &body))
	inner := body["automaticRepliesSetting"].(map[string]any)
	require.Equal(t, "scheduled", inner["status"])
	require.NotNil(t, inner["scheduledStartDateTime"], "scheduledStartDateTime should be present")
	require.NotNil(t, inner["scheduledEndDateTime"], "scheduledEndDateTime should be present")
}

// TestUpdateAutoRepliesDisabledOmitsSchedule verifies that when status is
// "disabled", schedule fields are absent from the PATCH body.
func TestUpdateAutoRepliesDisabledOmitsSchedule(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	err = c.UpdateAutoReplies(context.Background(), AutoRepliesSetting{
		Status:           AutoReplyDisabled,
		ExternalAudience: "all",
		ScheduledStart:   &DateTimeTimeZone{DateTime: "2026-04-28T09:00:00", TimeZone: "UTC"},
		ScheduledEnd:     &DateTimeTimeZone{DateTime: "2026-05-05T17:00:00", TimeZone: "UTC"},
	})
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(capturedBody, &body))
	inner := body["automaticRepliesSetting"].(map[string]any)
	require.Equal(t, "disabled", inner["status"])
	require.Nil(t, inner["scheduledStartDateTime"], "scheduledStartDateTime should be absent")
	require.Nil(t, inner["scheduledEndDateTime"], "scheduledEndDateTime should be absent")
}

// TestBuildWorkingHoursDisplay verifies the human-readable formatting.
func TestBuildWorkingHoursDisplay(t *testing.T) {
	cases := []struct {
		days  []string
		start string
		end   string
		want  string
	}{
		{
			days:  []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
			start: "09:00:00",
			end:   "17:00:00",
			want:  "Mon–Fri 09:00–17:00",
		},
		{
			days:  []string{"monday", "wednesday", "friday"},
			start: "08:30:00",
			end:   "16:00:00",
			want:  "Mon, Wed, Fri 08:30–16:00",
		},
		{
			days:  []string{},
			start: "09:00:00",
			end:   "17:00:00",
			want:  "",
		},
	}
	for _, tc := range cases {
		got := buildWorkingHoursDisplay(tc.days, tc.start, tc.end)
		if got != tc.want {
			t.Errorf("buildWorkingHoursDisplay(%v, %q, %q) = %q, want %q",
				tc.days, tc.start, tc.end, got, tc.want)
		}
	}
}

// TestDateTimeTimeZoneToTime verifies the ToTime helper used by the
// flag due_date / completed_date round-trip (H-2).
func TestDateTimeTimeZoneToTime(t *testing.T) {
	nyc, _ := time.LoadLocation("America/New_York")

	cases := []struct {
		name     string
		d        *DateTimeTimeZone
		wantZero bool
		wantHour int // local hour in the named tz
	}{
		{"nil", nil, true, 0},
		{"empty strings", &DateTimeTimeZone{}, true, 0},
		{"UTC noon", &DateTimeTimeZone{DateTime: "2026-05-10T12:00:00", TimeZone: "UTC"}, false, 12},
		{"NYC noon", &DateTimeTimeZone{DateTime: "2026-05-10T12:00:00", TimeZone: "America/New_York"}, false, 12},
		{"with fractional seconds", &DateTimeTimeZone{DateTime: "2026-05-10T12:00:00.000", TimeZone: "UTC"}, false, 12},
		{"bad timezone falls back to UTC", &DateTimeTimeZone{DateTime: "2026-05-10T12:00:00", TimeZone: "Invalid/Zone"}, false, 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.d.ToTime()
			if tc.wantZero {
				require.True(t, got.IsZero(), "expected zero time")
				return
			}
			require.False(t, got.IsZero())
			if tc.d != nil && tc.d.TimeZone == "America/New_York" {
				require.Equal(t, tc.wantHour, got.In(nyc).Hour())
			} else {
				require.Equal(t, tc.wantHour, got.UTC().Hour())
			}
		})
	}
}
