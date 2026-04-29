package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// CalendarModel is the state for the `:cal` modal: today's events,
// a loading flag, and the most recent error if the fetch failed.
type CalendarModel struct {
	events  []CalendarEvent
	loading bool
	err     error
}

// NewCalendar returns an empty calendar modal.
func NewCalendar() CalendarModel { return CalendarModel{} }

// SetLoading marks the modal as fetching. Cleared when SetEvents or
// SetError fires.
func (m *CalendarModel) SetLoading() { m.loading = true; m.err = nil; m.events = nil }

// SetEvents replaces the displayed events.
func (m *CalendarModel) SetEvents(es []CalendarEvent) {
	m.events = es
	m.loading = false
	m.err = nil
}

// SetError records a fetch failure to surface in the modal.
func (m *CalendarModel) SetError(err error) {
	m.err = err
	m.loading = false
}

// Reset clears the modal back to empty.
func (m *CalendarModel) Reset() { *m = CalendarModel{} }

// View renders the calendar modal centred on the screen.
func (m CalendarModel) View(t Theme, width, height int) string {
	now := time.Now()
	header := t.Bold.Render("📅 Today — " + now.Format("Mon 2006-01-02"))

	body := ""
	switch {
	case m.loading:
		body = t.Dim.Render("loading…")
	case m.err != nil:
		body = t.ErrorBar.Render("error: " + m.err.Error())
	case len(m.events) == 0:
		body = t.Dim.Render("nothing on the calendar today.")
	default:
		var lines []string
		for _, e := range m.events {
			lines = append(lines, formatEvent(t, e))
		}
		body = strings.Join(lines, "\n\n")
	}

	footer := t.Dim.Render("esc to close")
	box := t.Modal.Render(strings.Join([]string{header, "", body, "", footer}, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// formatEvent renders one event as two lines: time range + subject,
// then a faint location/online-meeting line if either is set.
func formatEvent(t Theme, e CalendarEvent) string {
	timeRange := e.Start.Local().Format("15:04") + " – " + e.End.Local().Format("15:04")
	if e.IsAllDay {
		timeRange = "all day"
	}
	first := t.Bold.Render(timeRange) + "  " + e.Subject
	var meta []string
	if e.Location != "" {
		meta = append(meta, "📍 "+e.Location)
	}
	if e.OnlineMeetingURL != "" {
		meta = append(meta, "🔗 join link available")
	}
	if e.OrganizerName != "" {
		meta = append(meta, "by "+e.OrganizerName)
	}
	if len(meta) == 0 {
		return first
	}
	return first + "\n  " + t.Dim.Render(strings.Join(meta, " · "))
}
