package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// OOFModel is the state for the `:ooo` modal: current settings, a
// loading flag while fetching/patching, and the most recent error.
// v0.9.0 supports view + toggle; richer edits (custom message,
// schedule, audience) are deferred.
type OOFModel struct {
	settings *MailboxSettings
	loading  bool
	saving   bool
	err      error
}

// NewOOF returns an empty OOF modal.
func NewOOF() OOFModel { return OOFModel{} }

// SetLoading marks the modal as fetching from Graph.
func (m *OOFModel) SetLoading() {
	m.loading = true
	m.saving = false
	m.err = nil
}

// SetSettings replaces the displayed settings. Clears loading/saving.
func (m *OOFModel) SetSettings(s *MailboxSettings) {
	m.settings = s
	m.loading = false
	m.saving = false
	m.err = nil
}

// SetSaving marks the modal as PATCHing.
func (m *OOFModel) SetSaving() {
	m.saving = true
	m.err = nil
}

// SetError records a fetch / patch failure to surface in the modal.
func (m *OOFModel) SetError(err error) {
	m.err = err
	m.loading = false
	m.saving = false
}

// Reset clears the modal back to empty.
func (m *OOFModel) Reset() { *m = OOFModel{} }

// View renders the modal centred on the screen.
func (m OOFModel) View(t Theme, width, height int) string {
	header := t.Bold.Render("Out of Office")
	body := ""
	switch {
	case m.loading:
		body = t.Dim.Render("loading…")
	case m.saving:
		body = t.Dim.Render("saving…")
	case m.err != nil:
		body = t.ErrorBar.Render("error: " + m.err.Error())
	case m.settings == nil:
		body = t.Dim.Render("no settings loaded.")
	default:
		state := "Off"
		if m.settings.AutoReplyEnabled {
			state = "On"
		}
		preview := m.settings.InternalReplyMessage
		if preview == "" {
			preview = t.Dim.Render("(no internal message configured)")
		} else if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		body = strings.Join([]string{
			"Status:   " + t.Bold.Render(state),
			"",
			"Reply:",
			"  " + preview,
			"",
			t.Dim.Render("(custom message, schedule, audience: edit in Outlook for now)"),
		}, "\n")
	}
	footer := t.Dim.Render("[t] toggle on/off · [esc] close")
	if m.settings == nil || m.loading || m.saving || m.err != nil {
		footer = t.Dim.Render("[esc] close")
	}
	box := t.Modal.Render(strings.Join([]string{header, "", body, "", footer}, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
