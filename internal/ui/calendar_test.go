package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCalendarModelViewDateIsToday verifies NewCalendar initialises
// viewDate to today midnight UTC.
func TestCalendarModelViewDateIsToday(t *testing.T) {
	m := NewCalendar()
	now := time.Now().UTC()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
	require.Equal(t, today, m.ViewDate(), "NewCalendar must start at today midnight UTC")
}

// TestCalendarModelNavNextDayAdvancesOneDay confirms ] navigation.
func TestCalendarModelNavNextDayAdvancesOneDay(t *testing.T) {
	m := NewCalendar()
	base := m.ViewDate()
	m.NavNextDay()
	require.Equal(t, base.AddDate(0, 0, 1), m.ViewDate())
	require.Equal(t, 0, m.cursor, "NavNextDay must reset cursor to top")
}

// TestCalendarModelNavPrevDayRetreat confirms [ navigation.
func TestCalendarModelNavPrevDayRetreat(t *testing.T) {
	m := NewCalendar()
	base := m.ViewDate()
	m.NavPrevDay()
	require.Equal(t, base.AddDate(0, 0, -1), m.ViewDate())
	require.Equal(t, 0, m.cursor, "NavPrevDay must reset cursor to top")
}

// TestCalendarModelNavNextWeekAdvancesSeven confirms } navigation.
func TestCalendarModelNavNextWeekAdvancesSeven(t *testing.T) {
	m := NewCalendar()
	base := m.ViewDate()
	m.NavNextWeek()
	require.Equal(t, base.AddDate(0, 0, 7), m.ViewDate())
	require.Equal(t, 0, m.cursor, "NavNextWeek must reset cursor to top")
}

// TestCalendarModelNavPrevWeekRetreatsSeven confirms { navigation.
func TestCalendarModelNavPrevWeekRetreatsSeven(t *testing.T) {
	m := NewCalendar()
	base := m.ViewDate()
	m.NavPrevWeek()
	require.Equal(t, base.AddDate(0, 0, -7), m.ViewDate())
	require.Equal(t, 0, m.cursor, "NavPrevWeek must reset cursor to top")
}

// TestCalendarModelGotoTodayResetsAfterNavigation confirms t key logic.
func TestCalendarModelGotoTodayResetsAfterNavigation(t *testing.T) {
	m := NewCalendar()
	m.NavNextDay()
	m.NavNextDay()
	// Manually move the cursor so we can verify it resets.
	m.cursor = 3

	m.GotoToday()

	now := time.Now().UTC()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
	require.Equal(t, today, m.ViewDate(), "GotoToday must reset viewDate to today midnight UTC")
	require.Equal(t, 0, m.cursor, "GotoToday must reset cursor to top")
}

// TestCalendarModelNavResetsEvents verifies that NavNextDay resets the
// event list and loading flag so the previous day's events don't flicker.
func TestCalendarModelNavResetsEvents(t *testing.T) {
	m := NewCalendar()
	events := []CalendarEvent{{Subject: "old event"}}
	m.SetEvents(events)
	require.Len(t, m.events, 1)

	// Navigation flips loading; SetLoading clears events.
	m.NavNextDay()
	m.SetLoading()
	require.Nil(t, m.events, "SetLoading must clear the event list")
	require.True(t, m.loading)
}

// TestSameDayTrueForSameDay exercises the sameDay helper.
func TestSameDayTrueForSameDay(t *testing.T) {
	a := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	b := time.Date(2026, 5, 1, 23, 59, 59, 0, time.UTC)
	require.True(t, sameDay(a, b))
}

// TestSameDayFalseForDifferentDay verifies midnight boundary.
func TestSameDayFalseForDifferentDay(t *testing.T) {
	a := time.Date(2026, 5, 1, 23, 59, 59, 0, time.UTC)
	b := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	require.False(t, sameDay(a, b))
}

// TestSameDayFalseForDifferentYears is a boundary sanity check.
func TestSameDayFalseForDifferentYears(t *testing.T) {
	a := time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC)
	b := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.False(t, sameDay(a, b))
}

// TestCalendarModelWeekModeToggle verifies that ToggleWeekMode flips the
// flag and that repeated calls toggle back.
func TestCalendarModelWeekModeToggle(t *testing.T) {
	m := NewCalendar()
	require.False(t, m.IsWeekMode(), "new calendar must start in agenda mode")

	got := m.ToggleWeekMode()
	require.True(t, got, "first toggle must return true")
	require.True(t, m.IsWeekMode())

	got = m.ToggleWeekMode()
	require.False(t, got, "second toggle must return false")
	require.False(t, m.IsWeekMode())
}

// TestCalendarModelIsWeekModeDefault confirms the zero-value is agenda
// mode (weekMode==false) — consistent with NewCalendar.
func TestCalendarModelIsWeekModeDefault(t *testing.T) {
	var m CalendarModel
	require.False(t, m.IsWeekMode(), "zero-value CalendarModel must be in agenda mode")
}

// TestAttendeeStatusGlyphAllCases verifies all spec 12 §7 status → glyph mappings.
func TestAttendeeStatusGlyphAllCases(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"accepted", "✓"},
		{"organizer", "✓"},
		{"declined", "✗"},
		{"tentativelyAccepted", "~"},
		{"notResponded", "?"},
		{"none", "?"},
		{"", "?"},
		{"unknown", "?"},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			require.Equal(t, tc.want, attendeeStatusGlyph(tc.status))
		})
	}
}

// TestFormatAttendeeFormats verifies name+addr, name-only, and addr-only rendering.
func TestFormatAttendeeFormats(t *testing.T) {
	require.Equal(t, "Alice <alice@example.invalid>",
		formatAttendee(CalendarAttendee{Name: "Alice", Address: "alice@example.invalid"}))
	require.Equal(t, "Bob",
		formatAttendee(CalendarAttendee{Name: "Bob", Address: ""}))
	require.Equal(t, "carol@example.invalid",
		formatAttendee(CalendarAttendee{Name: "", Address: "carol@example.invalid"}))
}

// TestFormatEventAllDay verifies that all-day events render with 📅 prefix and no time range.
func TestFormatEventAllDay(t *testing.T) {
	th := DefaultTheme()
	ev := CalendarEvent{Subject: "Company Holiday", IsAllDay: true}
	line := formatEvent(th, ev, false, time.UTC)
	require.Contains(t, line, "📅 Company Holiday")
	// No colon means no "HH:MM" time range rendered.
	require.NotContains(t, line, "00:00")
}

// TestFormatEventOnlineMeeting verifies that an event with a join URL shows 🔗 in meta line.
func TestFormatEventOnlineMeeting(t *testing.T) {
	th := DefaultTheme()
	start := time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC)
	ev := CalendarEvent{
		Subject:          "Planning",
		Start:            start,
		End:              start.Add(time.Hour),
		OnlineMeetingURL: "https://teams.example.invalid/join/x",
	}
	line := formatEvent(th, ev, false, time.UTC)
	require.Contains(t, line, "🔗")
}

// TestCalendarDetailViewAttendeeCap verifies that 11 attendees render the first 10
// plus "… and 1 more" (spec 12 §7 attendee cap).
func TestCalendarDetailViewAttendeeCap(t *testing.T) {
	attendees := make([]CalendarAttendee, 11)
	for i := range attendees {
		attendees[i] = CalendarAttendee{
			Name:    fmt.Sprintf("Person %d", i),
			Address: fmt.Sprintf("p%d@example.invalid", i),
			Status:  "accepted",
		}
	}
	detail := CalendarEventDetail{
		CalendarEvent: CalendarEvent{
			Subject: "Big Meeting",
			Start:   time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
			End:     time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC),
		},
		Attendees: attendees,
	}
	m := NewCalendarDetail()
	m.SetDetail(detail)
	rendered := m.View(DefaultTheme(), 120, 40)
	require.Contains(t, rendered, "… and 1 more", "overflow line must appear for 11 attendees")
	require.Contains(t, rendered, "Person 0", "first attendee must be shown")
	require.Contains(t, rendered, "Person 9", "tenth attendee must be shown")
	require.NotContains(t, rendered, "Person 10", "eleventh attendee must be hidden")
}

// TestCalendarDetailViewNoOrganizerPlaceholder verifies that an event with empty
// organizer fields shows "(no organizer)" rather than a blank line.
func TestCalendarDetailViewNoOrganizerPlaceholder(t *testing.T) {
	detail := CalendarEventDetail{
		CalendarEvent: CalendarEvent{
			Subject: "Orphaned Meeting",
			Start:   time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
			End:     time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC),
		},
	}
	m := NewCalendarDetail()
	m.SetDetail(detail)
	rendered := m.View(DefaultTheme(), 120, 40)
	require.Contains(t, rendered, "(no organizer)")
}

// TestFormatEventUsesProvidedTimezone verifies that formatEvent formats
// times using the supplied *time.Location, not the local zone.
func TestFormatEventUsesProvidedTimezone(t *testing.T) {
	nyc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	// Construct a UTC event at 15:00 UTC = 11:00 EDT (UTC-4 in summer).
	start := time.Date(2026, 5, 4, 15, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 4, 16, 0, 0, 0, time.UTC)
	ev := CalendarEvent{Subject: "Standup", Start: start, End: end}

	th := DefaultTheme()
	line := formatEvent(th, ev, false, nyc)
	require.Contains(t, line, "11:00", "time must display in the provided timezone (EDT)")
	require.NotContains(t, line, "15:00", "UTC time must not appear when tz is provided")
}
