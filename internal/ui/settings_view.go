package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SettingsModel renders the read-only :settings modal (spec 13 §5.2).
type SettingsModel struct {
	settings *MailboxSettings
	loading  bool
	err      error
}

// SetLoading marks the model as fetching from Graph.
func (m *SettingsModel) SetLoading() {
	m.loading = true
	m.err = nil
}

// SetSettings replaces the displayed settings.
func (m *SettingsModel) SetSettings(s *MailboxSettings) {
	m.settings = s
	m.loading = false
	m.err = nil
}

// SetError records a fetch failure.
func (m *SettingsModel) SetError(err error) {
	m.err = err
	m.loading = false
}

// Reset clears the model back to empty.
func (m *SettingsModel) Reset() { *m = SettingsModel{} }

// View renders the modal centred on the screen.
func (m SettingsModel) View(t Theme, width, height int) string {
	header := t.Bold.Render("Mailbox Settings")
	var body string
	switch {
	case m.loading:
		body = t.Dim.Render("loading…")
	case m.err != nil:
		body = t.ErrorBar.Render("error: " + m.err.Error())
	case m.settings == nil:
		body = t.Dim.Render("no settings loaded.")
	default:
		s := m.settings
		status := s.AutoReplyStatus
		if status == "" {
			status = "disabled"
		}
		var sb strings.Builder
		sb.WriteString("Automatic Replies: " + status + "  [o] edit\n")
		sb.WriteString("Time Zone:         " + s.TimeZone + "\n")
		sb.WriteString("Locale:            " + s.Language + "\n")
		if s.DateFormat != "" {
			sb.WriteString("Date Format:       " + s.DateFormat + "\n")
		}
		if s.TimeFormat != "" {
			sb.WriteString("Time Format:       " + s.TimeFormat + "\n")
		}
		if s.WorkingHoursDisplay != "" {
			sb.WriteString("Working Hours:     " + s.WorkingHoursDisplay + "\n")
		}
		body = strings.TrimRight(sb.String(), "\n")
	}
	footer := t.Dim.Render("[o] Edit OOO    [Esc] Close")
	box := t.Modal.Render(strings.Join([]string{header, "", body, "", footer}, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
