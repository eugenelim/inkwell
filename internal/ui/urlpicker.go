package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// URLPickerModel is the spec 05 §10 / v0.15.x URL extractor pane.
// Opened by `o` from the viewer pane; lists every URL the renderer
// pulled out of the body so the user picks one to open or copy.
//
// The model is stateless beyond cursor position — the data lives
// on ViewerModel.links so re-opening the picker after the user
// scrolls or switches messages always reflects the current body.
//
// Keystrokes (active in URLPickerMode):
//
//	j / k / ↓ / ↑   move cursor
//	Enter / o       open in browser
//	y               yank URL to clipboard (OSC 52 + pbcopy on macOS)
//	Esc / q         close
//
// Why a picker instead of relying on terminal click? Because the
// viewer pane sits in a side-by-side three-column layout — a URL
// that wraps across rows can't be drag-selected (terminal selection
// is rectangular and crosses pane borders) and OSC 8 hyperlinks
// only cover the single visible row. urlview / urlscan in mutt /
// neomutt converge on this pattern; aerc has `:open-link`. We follow
// the convention. (Research: TUI mail client URL handling, 2026-04.)
type URLPickerModel struct {
	cursor int
}

// NewURLPicker returns the empty model.
func NewURLPicker() URLPickerModel { return URLPickerModel{} }

// Reset returns the cursor to the top. Called when the picker
// opens so a previously-positioned cursor doesn't carry over to a
// different message's URL list.
func (m *URLPickerModel) Reset() { m.cursor = 0 }

// Up / Down move the cursor within the bounds of `links`. No-op
// at the edges (matches list-pane semantics — no wrap-around so
// j-mash doesn't accidentally cycle).
func (m *URLPickerModel) Up() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *URLPickerModel) Down(maxIdx int) {
	if m.cursor < maxIdx {
		m.cursor++
	}
}

// Selected returns the currently-highlighted link, or nil if the
// list is empty.
func (m URLPickerModel) Selected(links []BodyLink) *BodyLink {
	if len(links) == 0 || m.cursor < 0 || m.cursor >= len(links) {
		return nil
	}
	return &links[m.cursor]
}

// View renders the picker as a centered modal. Each row shows the
// numbered index, the URL, and (when the renderer recorded one
// distinct from the URL) the surrounding text snippet — this is
// the urlscan-killer-feature: disambiguate `[1]` vs `[2]` by
// context, not memory.
func (m URLPickerModel) View(t Theme, links []BodyLink, width, height int) string {
	if len(links) == 0 {
		body := "No URLs in this message.\n\n" + t.Dim.Render("Esc / q  close")
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			t.Modal.Render(body))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", t.HelpKey.Render(fmt.Sprintf("URLs (%d)", len(links))))
	for i, l := range links {
		marker := "  "
		if i == m.cursor {
			marker = "▶ "
		}
		// Show the URL on the first line; if the renderer recorded
		// a distinct anchor text, show it dim on the same line.
		row := fmt.Sprintf("%s[%d] %s", marker, l.Index, l.URL)
		if l.Text != "" && l.Text != l.URL {
			row += "  " + t.Dim.Render("("+truncateForModal(l.Text, 40)+")")
		}
		if i == m.cursor {
			row = t.HelpKey.Render(row)
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(t.Dim.Render("Enter / o  open  ·  y  yank to clipboard  ·  Esc / q  close"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		t.Modal.Render(b.String()))
}
