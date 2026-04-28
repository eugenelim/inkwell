package ui

import "github.com/charmbracelet/lipgloss"

// Theme groups the lipgloss styles used by every pane. All styling
// flows through here; no inline ANSI escapes anywhere else (CLAUDE.md
// §4).
type Theme struct {
	Status      lipgloss.Style
	Folders     lipgloss.Style
	FoldersSel  lipgloss.Style
	List        lipgloss.Style
	ListSel     lipgloss.Style
	ListUnread  lipgloss.Style
	Viewer      lipgloss.Style
	CommandBar  lipgloss.Style
	Help        lipgloss.Style
	Modal       lipgloss.Style
	ErrorBar    lipgloss.Style
	Throttled   lipgloss.Style
	Dim         lipgloss.Style
	Bold        lipgloss.Style
}

// DefaultTheme returns a high-contrast, terminal-safe theme.
func DefaultTheme() Theme {
	border := lipgloss.NormalBorder()
	return Theme{
		Status:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")),
		Folders:    lipgloss.NewStyle().Border(border, false, true, false, false),
		FoldersSel: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")),
		List:       lipgloss.NewStyle().Border(border, false, true, false, false),
		ListSel:    lipgloss.NewStyle().Reverse(true),
		ListUnread: lipgloss.NewStyle().Bold(true),
		Viewer:     lipgloss.NewStyle(),
		CommandBar: lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		Help:       lipgloss.NewStyle().Faint(true),
		Modal:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2),
		ErrorBar:   lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),
		Throttled:  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		Dim:        lipgloss.NewStyle().Faint(true),
		Bold:       lipgloss.NewStyle().Bold(true),
	}
}
