package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme groups the lipgloss styles used by every pane. All styling
// flows through here; no inline ANSI escapes anywhere else (CLAUDE.md
// §4).
type Theme struct {
	Status     lipgloss.Style
	Folders    lipgloss.Style
	FoldersSel lipgloss.Style
	List       lipgloss.Style
	ListSel    lipgloss.Style
	ListUnread lipgloss.Style
	Viewer     lipgloss.Style
	CommandBar lipgloss.Style
	Help       lipgloss.Style // body of the help bar (descriptions)
	HelpKey    lipgloss.Style // key glyphs in the help bar (j/k, ⏎, etc.)
	HelpSep    lipgloss.Style // separator dots between hints
	Modal      lipgloss.Style
	ErrorBar   lipgloss.Style
	Throttled  lipgloss.Style
	Dim        lipgloss.Style
	Bold       lipgloss.Style
}

// palette is the small set of semantic colors a theme builder picks
// from. Keeping the color tokens centralised means a new theme is one
// palette literal plus the bordered-pane assembly in [paletteToTheme].
type palette struct {
	fg       string // primary foreground
	muted    string // dim / faint
	accent   string // selected row foreground
	selectBG string // selected row background
	unread   string // unread row foreground
	warn     string // throttle / waiting
	err      string // error bar
	border   string // pane borders
	cmd      string // command-bar prompt
	// Help-bar palette. helpKey is the bright accent on key glyphs
	// (j/k, ⏎, etc.), helpDesc is the readable-but-secondary
	// description text, helpSep is the muted separator dot. Inspired
	// by Claude Code's warm-amber-on-charcoal hint style.
	helpKey  string
	helpDesc string
	helpSep  string
}

// presetPalettes maps a config name to a [palette]. Names are fixed
// constants — adding a new one is a code change reviewed against the
// per-spec PR checklist.
var presetPalettes = map[string]palette{
	// "default" is Claude-Code-inspired: warm amber accents on a
	// charcoal-friendly muted base. Help bar uses the same accent so
	// keys jump out without screaming.
	"default": {
		fg: "252", muted: "245", accent: "229", selectBG: "24",
		unread: "231", warn: "214", err: "203", border: "240", cmd: "215",
		helpKey: "215", helpDesc: "250", helpSep: "240",
	},
	"dark": {
		fg: "252", muted: "243", accent: "159", selectBG: "24",
		unread: "231", warn: "214", err: "203", border: "238", cmd: "117",
		helpKey: "215", helpDesc: "252", helpSep: "240",
	},
	"light": {
		fg: "232", muted: "247", accent: "232", selectBG: "153",
		unread: "16", warn: "130", err: "124", border: "250", cmd: "26",
		helpKey: "166", helpDesc: "238", helpSep: "248",
	},
	"solarized-dark": {
		fg: "230", muted: "240", accent: "254", selectBG: "23",
		unread: "230", warn: "136", err: "160", border: "239", cmd: "37",
		helpKey: "136", helpDesc: "245", helpSep: "240",
	},
	"solarized-light": {
		fg: "235", muted: "245", accent: "235", selectBG: "230",
		unread: "234", warn: "136", err: "160", border: "250", cmd: "33",
		helpKey: "166", helpDesc: "240", helpSep: "248",
	},
	"high-contrast": {
		fg: "15", muted: "15", accent: "0", selectBG: "11",
		unread: "15", warn: "11", err: "9", border: "15", cmd: "14",
		helpKey: "11", helpDesc: "15", helpSep: "8",
	},
}

// ThemeByName returns the named theme, falling back to "default" when
// the name is unknown. Caller is responsible for logging the fallback.
func ThemeByName(name string) (Theme, bool) {
	p, ok := presetPalettes[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return paletteToTheme(presetPalettes["default"]), false
	}
	return paletteToTheme(p), true
}

// DefaultTheme returns the "default" preset.
func DefaultTheme() Theme { return paletteToTheme(presetPalettes["default"]) }

func paletteToTheme(p palette) Theme {
	border := lipgloss.NormalBorder()
	return Theme{
		Status:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.fg)),
		Folders:    lipgloss.NewStyle().Border(border, false, true, false, false).BorderForeground(lipgloss.Color(p.border)),
		FoldersSel: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.accent)).Background(lipgloss.Color(p.selectBG)),
		List:       lipgloss.NewStyle().Border(border, false, true, false, false).BorderForeground(lipgloss.Color(p.border)),
		ListSel:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.accent)).Background(lipgloss.Color(p.selectBG)),
		ListUnread: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.unread)),
		Viewer:     lipgloss.NewStyle(),
		CommandBar: lipgloss.NewStyle().Foreground(lipgloss.Color(p.cmd)),
		Help:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.helpDesc)),
		HelpKey:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.helpKey)),
		HelpSep:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.helpSep)),
		Modal:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(p.border)).Padding(1, 2),
		ErrorBar:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.err)).Bold(true),
		Throttled:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.warn)),
		Dim:        lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.muted)),
		Bold:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.fg)),
	}
}
