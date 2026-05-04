package ui

import (
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
