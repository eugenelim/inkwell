package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// OOFModel is the state for the `:ooo` modal: current settings, loading
// state, and all editing fields.
type OOFModel struct {
	settings *MailboxSettings
	loading  bool
	saving   bool
	err      error

	// cursor indexes the focused field:
	// 0=status, 1=startDate, 2=startTime, 3=endDate, 4=endTime,
	// 5=audience, 6=internalMsg, 7=externalMsg
	cursor int

	editStatus   string // "disabled" | "alwaysEnabled" | "scheduled"
	startDate    string // "YYYY-MM-DD"
	startTime    string // "HH:MM"
	endDate      string
	endTime      string
	editAudience string // "all" | "contactsOnly" | "none"
	internalMsg  string
	externalMsg  string
	validErr     string
}

// NewOOF returns an empty OOF modal.
func NewOOF() OOFModel { return OOFModel{} }

// SetLoading marks the modal as fetching from Graph.
func (m *OOFModel) SetLoading() {
	m.loading = true
	m.saving = false
	m.err = nil
}

// SetSettings replaces the displayed settings and pre-fills the editing fields.
func (m *OOFModel) SetSettings(s *MailboxSettings) {
	m.settings = s
	m.loading = false
	m.saving = false
	m.err = nil
	if s == nil {
		return
	}
	m.editStatus = s.AutoReplyStatus
	if m.editStatus == "" {
		m.editStatus = "disabled"
	}
	m.editAudience = s.ExternalAudience
	if m.editAudience == "" {
		m.editAudience = "all"
	}
	m.internalMsg = s.InternalReplyMessage
	m.externalMsg = s.ExternalReplyMessage
	if s.ScheduledStart != nil {
		m.startDate = s.ScheduledStart.Format("2006-01-02")
		m.startTime = s.ScheduledStart.Format("15:04")
	}
	if s.ScheduledEnd != nil {
		m.endDate = s.ScheduledEnd.Format("2006-01-02")
		m.endTime = s.ScheduledEnd.Format("15:04")
	}
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

// ToggleStatus cycles through disabled → alwaysEnabled → scheduled → disabled.
func (m *OOFModel) ToggleStatus() {
	switch m.editStatus {
	case "disabled", "":
		m.editStatus = "alwaysEnabled"
	case "alwaysEnabled":
		m.editStatus = "scheduled"
	default:
		m.editStatus = "disabled"
	}
}

// ToggleAudience cycles through all → contactsOnly → none → all.
func (m *OOFModel) ToggleAudience() {
	switch m.editAudience {
	case "all", "":
		m.editAudience = "contactsOnly"
	case "contactsOnly":
		m.editAudience = "none"
	default:
		m.editAudience = "all"
	}
}

// NextField advances the cursor to the next editable field (wrapping).
func (m *OOFModel) NextField() {
	max := 7
	if m.editStatus != "scheduled" {
		// Skip date/time fields when not in scheduled mode.
		if m.cursor == 0 {
			m.cursor = 5
			return
		}
		m.cursor++
		if m.cursor > max {
			m.cursor = 0
		}
		return
	}
	m.cursor++
	if m.cursor > max {
		m.cursor = 0
	}
}

// PrevField moves the cursor to the previous editable field (wrapping).
func (m *OOFModel) PrevField() {
	max := 7
	if m.editStatus != "scheduled" {
		if m.cursor == 0 {
			m.cursor = max
			return
		}
		if m.cursor <= 5 {
			m.cursor = 0
			return
		}
		m.cursor--
		return
	}
	m.cursor--
	if m.cursor < 0 {
		m.cursor = max
	}
}

// CurrentStatus returns the current editStatus string.
func (m *OOFModel) CurrentStatus() string { return m.editStatus }

// ToMailboxSettings builds a MailboxSettings from the current editing state.
// For scheduled mode, startDate+startTime and endDate+endTime are parsed into
// *time.Time; fields that fail to parse leave the pointer nil (caught by Validate).
func (m *OOFModel) ToMailboxSettings() MailboxSettings {
	s := MailboxSettings{
		AutoReplyStatus:      m.editStatus,
		InternalReplyMessage: m.internalMsg,
		ExternalReplyMessage: m.externalMsg,
		ExternalAudience:     m.editAudience,
	}
	if m.settings != nil {
		s.TimeZone = m.settings.TimeZone
		s.Language = m.settings.Language
	}
	if m.editStatus == "scheduled" {
		if t, err := time.Parse("2006-01-02 15:04", m.startDate+" "+m.startTime); err == nil {
			s.ScheduledStart = &t
		}
		if t, err := time.Parse("2006-01-02 15:04", m.endDate+" "+m.endTime); err == nil {
			s.ScheduledEnd = &t
		}
	}
	return s
}

// Validate checks the editing state and returns a user-facing error string,
// or "" if valid. Called before save.
func (m *OOFModel) Validate() string {
	if m.editStatus != "scheduled" {
		return ""
	}
	s := m.ToMailboxSettings()
	if s.ScheduledStart == nil {
		return "scheduled mode requires a valid start date/time (YYYY-MM-DD HH:MM)"
	}
	if s.ScheduledEnd == nil {
		return "scheduled mode requires a valid end date/time (YYYY-MM-DD HH:MM)"
	}
	if !s.ScheduledEnd.After(*s.ScheduledStart) {
		return "end must be after start"
	}
	return ""
}

// View renders the modal centred on the screen.
func (m OOFModel) View(t Theme, width, height int) string {
	header := t.Bold.Render("Out of Office")
	var body string
	switch {
	case m.loading:
		body = t.Dim.Render("loading…")
	case m.saving:
		body = t.Dim.Render("saving…")
	case m.err != nil:
		body = t.ErrorBar.Render("error: " + m.err.Error())
	default:
		var sb strings.Builder

		// Status row.
		radio := func(label, val string) string {
			if m.editStatus == val {
				return "(•) " + label
			}
			return "( ) " + label
		}
		statusLine := radio("Off", "disabled") + "  " +
			radio("On", "alwaysEnabled") + "  " +
			radio("On with schedule", "scheduled")
		if m.cursor == 0 {
			statusLine = t.Bold.Render(statusLine)
		}
		sb.WriteString("Status:  " + statusLine + "\n")

		// Date/time fields (only when scheduled).
		if m.editStatus == "scheduled" {
			sb.WriteString("\n")
			renderField := func(label, val string, cur int) string {
				s := label + ": " + val
				if m.cursor == cur {
					s = t.Bold.Render("> " + s)
				} else {
					s = "  " + s
				}
				return s
			}
			sb.WriteString(renderField("Start date", m.startDate, 1) + "\n")
			sb.WriteString(renderField("Start time", m.startTime, 2) + "\n")
			sb.WriteString(renderField("End date", m.endDate, 3) + "\n")
			sb.WriteString(renderField("End time", m.endTime, 4) + "\n")
		}

		// Audience (only when not disabled).
		if m.editStatus != "disabled" {
			sb.WriteString("\n")
			audRadio := func(label, val string) string {
				if m.editAudience == val {
					return "(•) " + label
				}
				return "( ) " + label
			}
			audLine := audRadio("All", "all") + "  " +
				audRadio("Contacts only", "contactsOnly") + "  " +
				audRadio("None", "none")
			if m.cursor == 5 {
				audLine = t.Bold.Render(audLine)
			}
			sb.WriteString("Audience: " + audLine + "\n")
		}

		// Internal message preview.
		sb.WriteString("\n")
		intPreview := truncateLines(m.internalMsg, 3)
		if intPreview == "" {
			intPreview = t.Dim.Render("(no internal message)")
		}
		intLabel := "Internal:"
		if m.cursor == 6 {
			intLabel = t.Bold.Render("> " + intLabel)
		} else {
			intLabel = "  " + intLabel
		}
		sb.WriteString(intLabel + "\n  " + strings.ReplaceAll(intPreview, "\n", "\n  ") + "\n")

		// External message preview.
		sb.WriteString("\n")
		extPreview := truncateLines(m.externalMsg, 3)
		if extPreview == "" {
			extPreview = t.Dim.Render("(no external message)")
		}
		extLabel := "External:"
		if m.cursor == 7 {
			extLabel = t.Bold.Render("> " + extLabel)
		} else {
			extLabel = "  " + extLabel
		}
		sb.WriteString(extLabel + "\n  " + strings.ReplaceAll(extPreview, "\n", "\n  "))

		if m.validErr != "" {
			sb.WriteString("\n" + t.ErrorBar.Render(m.validErr))
		}

		body = sb.String()
	}

	footer := t.Dim.Render("[Tab] next  [Space] toggle  [Enter] save  [Esc] cancel")
	box := t.Modal.Render(strings.Join([]string{header, "", body, "", footer}, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// truncateLines returns at most n lines from s.
func truncateLines(s string, n int) string {
	if s == "" {
		return ""
	}
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
		return strings.Join(lines, "\n") + "\n…"
	}
	return strings.Join(lines, "\n")
}
