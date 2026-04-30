package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// HelpModel is the spec 04 §12 full help overlay. Opened via `?` or
// `:help`; closed via Esc or `q`. Renders every binding in the
// active KeyMap, grouped by section so the user sees Movement →
// Triage → Filter → Compose etc. in one panel.
//
// Pane-scoped meanings (e.g. `r` = reply in viewer, mark-read in
// list) are surfaced as a single row with both labels — the
// rendering reflects the actual dispatch resolution rule from
// CLAUDE.md §4.
type HelpModel struct{}

// NewHelp returns the empty help overlay.
func NewHelp() HelpModel { return HelpModel{} }

// helpSection groups related bindings under a header.
type helpSection struct {
	title string
	rows  []helpRow
}

type helpRow struct {
	keys string
	desc string
}

// View renders the overlay. The model is stateless — it pulls the
// current bindings off the supplied KeyMap so user overrides
// surface immediately.
func (m HelpModel) View(t Theme, km KeyMap, width, height int) string {
	sections := buildHelpSections(km)
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(t.HelpKey.Render(s.title))
		b.WriteString("\n")
		for _, r := range s.rows {
			fmt.Fprintf(&b, "  %-14s  %s\n",
				t.HelpKey.Render(r.keys),
				t.Help.Render(r.desc))
		}
	}
	b.WriteString("\n")
	b.WriteString(t.Dim.Render("Esc / q  close"))
	box := t.Modal.Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// buildHelpSections assembles the canonical help layout from a
// KeyMap. The order matches the user-facing reference doc so what
// the user sees in `?` matches docs/user/reference.md.
func buildHelpSections(km KeyMap) []helpSection {
	return []helpSection{
		{
			title: "Pane focus & movement",
			rows: []helpRow{
				{keysOf(km.FocusFolders, km.FocusList, km.FocusViewer), "focus folders / list / viewer"},
				{keysOf(km.NextPane, km.PrevPane), "cycle panes"},
				{keysOf(km.Up, km.Down), "cursor up / down"},
				{keysOf(km.PageUp, km.PageDown), "page up / down"},
				{keysOf(km.Home, km.End), "first / last"},
				{keysOf(km.Open), "open / activate"},
			},
		},
		{
			title: "Triage (list & viewer)",
			rows: []helpRow{
				{keysOf(km.MarkRead), "mark read (list) / reply (viewer)"},
				{keysOf(km.MarkUnread), "mark unread"},
				{keysOf(km.ToggleFlag), "toggle flag"},
				{keysOf(km.Delete), "soft-delete"},
				{keysOf(km.PermanentDelete), "permanent delete (with confirm)"},
				{keysOf(km.Archive), "archive"},
				{keysOf(km.Move), "move to folder"},
				{keysOf(km.AddCategory, km.RemoveCategory), "add / remove category"},
				{keysOf(km.Undo), "undo last triage"},
				{keysOf(km.Unsubscribe), "unsubscribe (RFC 8058)"},
			},
		},
		{
			title: "Filter & bulk",
			rows: []helpRow{
				{keysOf(km.Filter), "open :filter prompt"},
				{keysOf(km.ClearFilter), "clear filter / search"},
				{keysOf(km.ApplyToFiltered), "begin bulk chord (then d / a)"},
			},
		},
		{
			title: "Viewer extras",
			rows: []helpRow{
				{keysOf(km.OpenURL), "URL picker (extracted links)"},
				{keysOf(km.Yank), "yank URL to clipboard"},
				{keysOf(km.FullscreenBody), "fullscreen body (drag-select)"},
			},
		},
		{
			title: "Modes & meta",
			rows: []helpRow{
				{keysOf(km.Cmd), "command mode"},
				{keysOf(km.Search), "search mode"},
				{keysOf(km.Refresh), "force sync now"},
				{keysOf(km.Help), "this help"},
				{keysOf(km.Quit), "quit"},
			},
		},
	}
}

// keysOf returns the user-facing string for one or more bindings.
// Multiple bindings concatenate with ` / ` (e.g. focus 1 / 2 / 3).
// Within a single binding, alternates concatenate with `,` (e.g.
// `q, ctrl+c`).
func keysOf(bs ...key.Binding) string {
	parts := make([]string, 0, len(bs))
	for _, b := range bs {
		ks := b.Keys()
		if len(ks) == 0 {
			continue
		}
		parts = append(parts, strings.Join(ks, ","))
	}
	return strings.Join(parts, " / ")
}
