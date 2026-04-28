package render

import "github.com/charmbracelet/lipgloss"

// Theme holds rendering-specific styles for the viewer pane.
type Theme struct {
	HeaderLabel lipgloss.Style
	HeaderValue lipgloss.Style
	Subject     lipgloss.Style
	Quote       lipgloss.Style
	Link        lipgloss.Style
	Attachment  lipgloss.Style
	Dim         lipgloss.Style
	Error       lipgloss.Style
}

// DefaultTheme returns a high-contrast viewer theme. Compatible with
// any 256-colour terminal.
func DefaultTheme() Theme {
	return Theme{
		HeaderLabel: lipgloss.NewStyle().Bold(true),
		HeaderValue: lipgloss.NewStyle(),
		Subject:     lipgloss.NewStyle().Bold(true),
		Quote:       lipgloss.NewStyle().Faint(true),
		Link:        lipgloss.NewStyle().Underline(true),
		Attachment:  lipgloss.NewStyle().Italic(true),
		Dim:         lipgloss.NewStyle().Faint(true),
		Error:       lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	}
}
