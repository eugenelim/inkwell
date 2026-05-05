package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// CalendarModel is the state for the `:cal` modal: today's events,
// a loading flag, the most recent error if the fetch failed, the
// cursor index for spec-12 §6.2 j/k navigation, the currently
// viewed date for day navigation (]/[/{/}/t spec 12 §6.2), and a
// week-mode flag toggled by `w`.
type CalendarModel struct {
	events   []CalendarEvent
	loading  bool
	err      error
	cursor   int
	viewDate time.Time // date whose events are displayed; zero = today
	weekMode bool      // true = week grid view; false = agenda (default)
}

// NewCalendar returns an empty calendar modal with viewDate = today (UTC).
func NewCalendar() CalendarModel {
	y, m, d := time.Now().UTC().Date()
	return CalendarModel{viewDate: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// ViewDate returns the currently displayed day (midnight UTC).
// Callers use it to fetch the right day's events.
func (m CalendarModel) ViewDate() time.Time { return m.viewDate }

// NavNextDay advances the view by one day.
func (m *CalendarModel) NavNextDay() { m.viewDate = m.viewDate.AddDate(0, 0, 1); m.cursor = 0 }

// NavPrevDay moves the view back one day.
func (m *CalendarModel) NavPrevDay() { m.viewDate = m.viewDate.AddDate(0, 0, -1); m.cursor = 0 }

// NavNextWeek advances the view by seven days.
func (m *CalendarModel) NavNextWeek() { m.viewDate = m.viewDate.AddDate(0, 0, 7); m.cursor = 0 }

// NavPrevWeek moves the view back seven days.
func (m *CalendarModel) NavPrevWeek() { m.viewDate = m.viewDate.AddDate(0, 0, -7); m.cursor = 0 }

// GotoToday resets the view to today (UTC).
func (m *CalendarModel) GotoToday() {
	y, mo, d := time.Now().UTC().Date()
	m.viewDate = time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
	m.cursor = 0
}

// SetLoading marks the modal as fetching. Cleared when SetEvents or
// SetError fires.
func (m *CalendarModel) SetLoading() { m.loading = true; m.err = nil; m.events = nil }

// SetEvents replaces the displayed events and resets the cursor to
// the top so a previous run's cursor doesn't carry into a new
// event list.
func (m *CalendarModel) SetEvents(es []CalendarEvent) {
	m.events = es
	m.loading = false
	m.err = nil
	m.cursor = 0
}

// SetError records a fetch failure to surface in the modal.
func (m *CalendarModel) SetError(err error) {
	m.err = err
	m.loading = false
}

// Reset clears the modal back to today.
func (m *CalendarModel) Reset() { *m = NewCalendar() }

// ToggleWeekMode flips between agenda (false) and week (true) view.
// Returns the new mode value.
func (m *CalendarModel) ToggleWeekMode() bool {
	m.weekMode = !m.weekMode
	return m.weekMode
}

// IsWeekMode reports whether week-grid view is active.
func (m CalendarModel) IsWeekMode() bool { return m.weekMode }

// Up / Down move the cursor inside the events list. No-op at the
// edges (no wrap-around — matches list-pane semantics).
func (m *CalendarModel) Up() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *CalendarModel) Down() {
	if m.cursor < len(m.events)-1 {
		m.cursor++
	}
}

// Selected returns the currently-highlighted event, or nil when the
// list is empty / loading / errored.
func (m CalendarModel) Selected() *CalendarEvent {
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return nil
	}
	e := m.events[m.cursor]
	return &e
}

// View renders the calendar modal centred on the screen.
// tz controls time formatting; nil falls back to time.Local.
func (m CalendarModel) View(t Theme, tz *time.Location, width, height int) string {
	if tz == nil {
		tz = time.Local
	}
	if m.weekMode {
		body := renderWeekView(t, m.events, tz)
		footer := t.Dim.Render("j/k  navigate  ·  Enter  open  ·  ]/[  day  ·  }/{ week  ·  t  today  ·  a  agenda view  ·  esc  close")
		box := t.Modal.Render(strings.Join([]string{t.Bold.Render("📅 Week View"), "", body, "", footer}, "\n"))
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}
	vd := m.viewDate
	if vd.IsZero() {
		vd = time.Now().In(tz)
	}
	isToday := sameDay(vd, time.Now().In(tz))
	dayLabel := "Today — " + vd.In(tz).Format("Mon 2006-01-02")
	if !isToday {
		dayLabel = vd.In(tz).Format("Mon 2006-01-02")
	}
	header := t.Bold.Render("📅 " + dayLabel)

	body := ""
	switch {
	case m.loading:
		body = t.Dim.Render("loading…")
	case m.err != nil:
		body = t.ErrorBar.Render("error: " + m.err.Error())
	case len(m.events) == 0:
		body = t.Dim.Render("nothing on the calendar.")
	default:
		var lines []string
		for i, e := range m.events {
			lines = append(lines, formatEvent(t, e, i == m.cursor, tz))
		}
		body = strings.Join(lines, "\n\n")
	}

	footer := t.Dim.Render("j/k  navigate  ·  Enter  open  ·  ]/[  day  ·  }/{ week  ·  t  today  ·  w  week view  ·  esc  close")
	box := t.Modal.Render(strings.Join([]string{header, "", body, "", footer}, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// renderWeekView renders all events grouped by day for the week grid.
func renderWeekView(t Theme, events []CalendarEvent, tz *time.Location) string {
	if tz == nil {
		tz = time.Local
	}
	if len(events) == 0 {
		return t.Dim.Render("no events this week.")
	}
	type dayBucket struct {
		label  string
		events []CalendarEvent
	}
	byDay := map[string]*dayBucket{}
	var dayKeys []string
	for _, e := range events {
		d := e.Start.In(tz)
		key := d.Format("2006-01-02")
		if _, ok := byDay[key]; !ok {
			byDay[key] = &dayBucket{label: d.Format("Mon Jan 2")}
			dayKeys = append(dayKeys, key)
		}
		byDay[key].events = append(byDay[key].events, e)
	}
	// Sort days ascending.
	for i := 0; i < len(dayKeys)-1; i++ {
		for j := i + 1; j < len(dayKeys); j++ {
			if dayKeys[i] > dayKeys[j] {
				dayKeys[i], dayKeys[j] = dayKeys[j], dayKeys[i]
			}
		}
	}
	var sb strings.Builder
	for di, key := range dayKeys {
		if di > 0 {
			sb.WriteString("\n")
		}
		bucket := byDay[key]
		sb.WriteString(t.Bold.Render(bucket.label) + "\n")
		for _, e := range bucket.events {
			timeStr := e.Start.In(tz).Format("15:04") + " – " + e.End.In(tz).Format("15:04")
			if e.IsAllDay {
				timeStr = "all day"
			}
			sb.WriteString("  " + timeStr + "  " + e.Subject + "\n")
		}
	}
	return sb.String()
}

// sameDay reports whether two times fall on the same calendar day in
// UTC. Used by the calendar header to decide "Today" vs date label.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}

// formatEvent renders one event as two lines: time range + subject,
// then a faint location/online-meeting line if either is set. The
// `selected` flag paints a leading ▶ marker plus highlights the
// title row so the cursor position is visible at a glance.
// tz controls the time display; nil falls back to time.Local.
func formatEvent(t Theme, e CalendarEvent, selected bool, tz *time.Location) string {
	if tz == nil {
		tz = time.Local
	}
	marker := "  "
	if selected {
		marker = "▶ "
	}
	var first string
	if e.IsAllDay {
		// All-day events: 📅 prefix, no time range (spec 12 §6.1).
		first = marker + "📅 " + e.Subject
	} else {
		timeRange := e.Start.In(tz).Format("15:04") + " – " + e.End.In(tz).Format("15:04")
		first = marker + t.Bold.Render(timeRange) + "  " + e.Subject
	}
	if selected {
		first = t.HelpKey.Render(first)
	}
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
	return first + "\n    " + t.Dim.Render(strings.Join(meta, " · "))
}

// CalendarDetailModel is the spec 12 §7 detail-modal state. Holds
// the fetched detail (with attendees + body preview) plus the
// loading / error states for the GetEvent round-trip the parent
// model dispatches when the user presses Enter.
type CalendarDetailModel struct {
	detail  *CalendarEventDetail
	loading bool
	err     error
	scrollY int // line offset for j/k scrolling
}

// NewCalendarDetail returns the empty detail model.
func NewCalendarDetail() CalendarDetailModel { return CalendarDetailModel{} }

// SetLoading marks the modal as fetching.
func (m *CalendarDetailModel) SetLoading() {
	m.loading = true
	m.err = nil
	m.detail = nil
}

// SetDetail replaces the rendered detail and resets the scroll position.
func (m *CalendarDetailModel) SetDetail(d CalendarEventDetail) {
	m.detail = &d
	m.loading = false
	m.err = nil
	m.scrollY = 0
}

// ScrollDown advances the detail body by one line.
func (m *CalendarDetailModel) ScrollDown() { m.scrollY++ }

// ScrollUp moves the detail body up one line.
func (m *CalendarDetailModel) ScrollUp() {
	if m.scrollY > 0 {
		m.scrollY--
	}
}

// PageDown jumps the detail body down by half a screen.
func (m *CalendarDetailModel) PageDown() { m.scrollY += 10 }

// PageUp jumps the detail body up by half a screen.
func (m *CalendarDetailModel) PageUp() {
	m.scrollY -= 10
	if m.scrollY < 0 {
		m.scrollY = 0
	}
}

// SetError records a fetch failure.
func (m *CalendarDetailModel) SetError(err error) {
	m.err = err
	m.loading = false
}

// Reset clears the detail back to empty.
func (m *CalendarDetailModel) Reset() { *m = CalendarDetailModel{} }

// Detail returns the loaded event, if any. Used by the dispatch
// layer to extract the WebLink / OnlineMeetingURL for `o` / `l`.
func (m CalendarDetailModel) Detail() *CalendarEventDetail { return m.detail }

// View renders the detail modal centred on the screen. The body is
// scrollable via j/k when content exceeds the available height.
func (m CalendarDetailModel) View(t Theme, width, height int) string {
	if m.loading {
		body := t.Dim.Render("loading event…\n\n") + t.Dim.Render("esc  close")
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, t.Modal.Render(body))
	}
	if m.err != nil {
		body := t.ErrorBar.Render("error: "+m.err.Error()) + "\n\n" + t.Dim.Render("esc  close")
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, t.Modal.Render(body))
	}
	if m.detail == nil {
		body := t.Dim.Render("(no event)\n\n") + t.Dim.Render("esc  close")
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, t.Modal.Render(body))
	}
	d := m.detail

	// Build fixed header lines (always visible, not scrolled).
	var header strings.Builder
	header.WriteString(t.Bold.Render(d.Subject))
	header.WriteString("\n\n")
	timeRange := d.Start.Local().Format("Mon 02 Jan 2006, 15:04") + "–" + d.End.Local().Format("15:04")
	if d.IsAllDay {
		timeRange = d.Start.Local().Format("Mon 02 Jan 2006") + " (all day)"
	}
	fmt.Fprintf(&header, "📅 %s\n", timeRange)
	if d.Location != "" {
		fmt.Fprintf(&header, "📍 %s\n", d.Location)
	}
	if d.OnlineMeetingURL != "" {
		fmt.Fprintf(&header, "🔗 %s  %s\n", d.OnlineMeetingURL, t.Dim.Render("[press l]"))
	}

	// Build scrollable body lines.
	var bodyLines []string
	{
		who := d.OrganizerName
		if d.OrganizerAddress != "" {
			if who != "" {
				who += " <" + d.OrganizerAddress + ">"
			} else {
				who = d.OrganizerAddress
			}
		}
		if who == "" {
			who = "(no organizer)"
		}
		bodyLines = append(bodyLines, "")
		bodyLines = append(bodyLines, "Organizer: "+who)
	}
	if len(d.Attendees) > 0 {
		const attendeeCap = 10
		bodyLines = append(bodyLines, "Attendees:")
		shown := d.Attendees
		if len(shown) > attendeeCap {
			shown = shown[:attendeeCap]
		}
		for _, a := range shown {
			bodyLines = append(bodyLines, fmt.Sprintf("  %s %s", attendeeStatusGlyph(a.Status), formatAttendee(a)))
		}
		if extra := len(d.Attendees) - attendeeCap; extra > 0 {
			bodyLines = append(bodyLines, fmt.Sprintf("  … and %d more", extra))
		}
	}
	if d.BodyPreview != "" {
		bodyLines = append(bodyLines, "")
		bodyLines = append(bodyLines, "Body:")
		for _, line := range strings.Split(strings.TrimSpace(d.BodyPreview), "\n") {
			bodyLines = append(bodyLines, "  "+line)
		}
	}

	// Footer hint (always visible).
	hint := "j/k  scroll  ·  esc  back"
	if d.WebLink != "" {
		hint = "o  Outlook  ·  " + hint
	}
	if d.OnlineMeetingURL != "" {
		hint = "l  meeting  ·  " + hint
	}
	footer := t.Dim.Render(hint)

	// Modal padding: RoundedBorder (1px each side) + Padding(1,2) = 4 rows vertical, 5 cols horizontal.
	// Reserve rows for: header lines + 1 blank separator + 1 footer.
	headerLines := strings.Split(header.String(), "\n")
	modalVertPad := 4 // border + padding top+bottom
	modalHorizPad := 5
	modalWidth := width - 10
	if modalWidth < 40 {
		modalWidth = 40
	}
	bodyWidth := modalWidth - modalHorizPad
	_ = bodyWidth

	// Available rows for the scrollable body.
	available := height - modalVertPad - len(headerLines) - 2 // 2 = blank line + footer
	if available < 1 {
		available = 1
	}

	// Apply scroll offset.
	scrolled := bodyLines
	if m.scrollY > 0 {
		if m.scrollY >= len(scrolled) {
			scrolled = nil
		} else {
			scrolled = scrolled[m.scrollY:]
		}
	}
	if len(scrolled) > available {
		scrolled = scrolled[:available]
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(header.String(), "\n"))
	if len(scrolled) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(scrolled, "\n"))
	}
	b.WriteString("\n\n")
	b.WriteString(footer)

	box := t.Modal.Width(modalWidth).Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// attendeeStatusGlyph maps a Graph response status to a one-char
// glyph for the attendees block. Spec 12 §7: ✓ accepted, ?
// not-responded / tentative, ✗ declined.
func attendeeStatusGlyph(status string) string {
	switch status {
	case "accepted", "organizer":
		return "✓"
	case "declined":
		return "✗"
	case "tentativelyAccepted":
		return "~"
	default:
		return "?"
	}
}

// formatAttendee renders the attendee's name + address compactly.
func formatAttendee(a CalendarAttendee) string {
	switch {
	case a.Name != "" && a.Address != "":
		return a.Name + " <" + a.Address + ">"
	case a.Name != "":
		return a.Name
	default:
		return a.Address
	}
}
