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
// any 256-colour terminal. Links render in cyan (universally understood
// as hyperlink); attachments render in amber to distinguish them from
// body text and links.
func DefaultTheme() Theme {
	return newTheme("45", "214")
}

// NewTheme returns a viewer theme with caller-specified link and
// attachment colours (256-colour ANSI codes or hex strings). Used by
// the UI layer to derive a theme that matches the active UI palette.
func NewTheme(linkColor, attachColor string) Theme {
	return newTheme(linkColor, attachColor)
}

func newTheme(linkColor, attachColor string) Theme {
	return Theme{
		HeaderLabel: lipgloss.NewStyle().Bold(true),
		HeaderValue: lipgloss.NewStyle(),
		Subject:     lipgloss.NewStyle().Bold(true),
		Quote:       lipgloss.NewStyle().Faint(true),
		Link:        lipgloss.NewStyle().Underline(true).Foreground(lipgloss.Color(linkColor)),
		Attachment:  lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color(attachColor)),
		Dim:         lipgloss.NewStyle().Faint(true),
		Error:       lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	}
}
