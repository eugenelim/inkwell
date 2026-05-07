package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Palette section identifiers. Used both for the per-row Section
// label (rendered as a dimmed badge in mixed scope) and for empty-
// buffer ordering.
const (
	sectionCommands      = "Commands"
	sectionFolders       = "Folders"
	sectionSavedSearches = "Saved searches"
)

// paletteScope identifies the active sigil scope. Mixed = no sigil
// (commands + folders + saved searches in one ranked list); folders
// / savedSearches / commands restrict the index to a single source.
type paletteScope int

const (
	scopeMixed paletteScope = iota
	scopeFolders
	scopeSavedSearches
	scopeCommands
)

// paletteRecentsCap is the MRU cap on the in-process recents list.
// Spec 22 §3.7.
const paletteRecentsCap = 8

// PaletteRow is one entry the palette can dispatch. Pure data — the
// RunFn / ArgFn close over small identifiers (folder ID, saved-search
// name) at row-collection time, never the whole Model. At Enter time
// the dispatcher hands the *current* Model into the closure.
type PaletteRow struct {
	ID        string
	Title     string
	Subtitle  string
	Binding   string
	Section   string
	Synonyms  []string
	NeedsArg  bool
	Available Availability
	RunFn     func(m Model) (tea.Model, tea.Cmd)
	ArgFn     func(m Model) (tea.Model, tea.Cmd)
}

// Availability is resolved once per palette Open against the live
// Model snapshot and stored on the row. Renderer + dispatcher both
// read from it without re-evaluating per keystroke.
type Availability struct {
	OK  bool
	Why string
}

// PaletteModel is the spec 22 Ctrl+K palette overlay. Stateless
// beyond cursor + buffer + recents; the row index is rebuilt on
// each Open so live state (focused message, deps) is fresh.
type PaletteModel struct {
	cursor   int
	buf      string
	rows     []PaletteRow
	caches   []paletteRowCache
	scope    paletteScope
	filtered []scoredRow
	recents  []string
}

// NewPalette returns the empty palette model.
func NewPalette() PaletteModel { return PaletteModel{} }

// Open seeds the row list from a Model snapshot. Called whenever the
// palette is entered so live state (focused message, deps) is fresh.
// The snapshot is frozen for the open session; FoldersLoadedMsg /
// savedSearchesUpdatedMsg arriving while the palette is open does not
// re-collect rows.
func (p *PaletteModel) Open(m *Model) {
	p.cursor = 0
	p.buf = ""
	p.rows = collectPaletteRows(m)
	p.caches = buildRowCaches(p.rows)
	p.refilter()
}

// Buffer returns the current input buffer (including any leading
// sigil). Used by tests to assert typed-input behaviour without
// poking unexported state.
func (p PaletteModel) Buffer() string { return p.buf }

// Scope returns the active sigil scope. Used by tests + the View to
// render the right-side header glyph.
func (p PaletteModel) Scope() paletteScope { return p.scope }

// Recents returns the in-process MRU list, MRU first. Used by tests.
func (p PaletteModel) Recents() []string { return append([]string(nil), p.recents...) }

// Filtered returns the current filtered+ranked rows. Used by tests +
// the View.
func (p PaletteModel) Filtered() []scoredRow { return p.filtered }

// Cursor returns the selected row index. Used by tests + the View.
func (p PaletteModel) Cursor() int { return p.cursor }

// Up moves the cursor toward the top of the filtered list.
func (p *PaletteModel) Up() {
	if p.cursor > 0 {
		p.cursor--
	}
}

// Down moves the cursor toward the bottom of the filtered list.
func (p *PaletteModel) Down() {
	if p.cursor < len(p.filtered)-1 {
		p.cursor++
	}
}

// AppendRunes extends the buffer and refilters. The cursor resets to
// row 0 so a freshly typed query doesn't leave the cursor past the
// now-shorter result list.
func (p *PaletteModel) AppendRunes(rs []rune) {
	prevScope := p.detectScope(p.buf)
	p.buf += string(rs)
	if p.detectScope(p.buf) != prevScope {
		p.cursor = 0
	}
	p.cursor = 0
	p.refilter()
}

// Backspace drops one rune from the buffer and refilters. Cursor
// resets to row 0. No-op at empty buffer (spec 22 §5: do not close
// the palette — Esc is the close key).
func (p *PaletteModel) Backspace() {
	if len(p.buf) == 0 {
		return
	}
	prevScope := p.detectScope(p.buf)
	rs := []rune(p.buf)
	p.buf = string(rs[:len(rs)-1])
	if p.detectScope(p.buf) != prevScope {
		p.cursor = 0
	}
	p.cursor = 0
	p.refilter()
}

// Selected returns the currently-highlighted row, or nil when the
// filtered list is empty.
func (p PaletteModel) Selected() *PaletteRow {
	if p.cursor < 0 || p.cursor >= len(p.filtered) {
		return nil
	}
	r := p.filtered[p.cursor].row
	return &r
}

// recordRecent moves id to the front of the recents list, deduping
// and capping at paletteRecentsCap. Called by the dispatcher before
// running RunFn so even verbs that change mode still record their
// row in the palette MRU.
func (p *PaletteModel) recordRecent(id string) {
	if id == "" {
		return
	}
	out := make([]string, 0, paletteRecentsCap)
	out = append(out, id)
	for _, r := range p.recents {
		if r == id {
			continue
		}
		out = append(out, r)
		if len(out) >= paletteRecentsCap {
			break
		}
	}
	p.recents = out
}

// detectScope reads the leading sigil rune from buf and returns the
// implied scope.
func (p PaletteModel) detectScope(buf string) paletteScope {
	if buf == "" {
		return scopeMixed
	}
	rs := []rune(buf)
	switch rs[0] {
	case '#':
		return scopeFolders
	case '@':
		return scopeSavedSearches
	case '>':
		return scopeCommands
	}
	return scopeMixed
}

// refilter recomputes the filtered list from the current buffer.
func (p *PaletteModel) refilter() {
	p.scope = p.detectScope(p.buf)
	q := p.buf
	if p.scope != scopeMixed && len(q) > 0 {
		rs := []rune(q)
		q = string(rs[1:])
	}
	rows := p.rowsForScope(p.scope)
	caches := p.cachesForScope(p.scope)
	p.filtered = matchAndScore(rows, caches, q, p.recents)
	if p.cursor >= len(p.filtered) {
		p.cursor = 0
	}
}

// rowsForScope returns the row subset for the active scope. The
// caches slice in cachesForScope must be filtered the same way.
func (p PaletteModel) rowsForScope(scope paletteScope) []PaletteRow {
	if scope == scopeMixed {
		return p.rows
	}
	out := make([]PaletteRow, 0, len(p.rows))
	for _, r := range p.rows {
		if matchesScope(r.Section, scope) {
			out = append(out, r)
		}
	}
	return out
}

// cachesForScope is the parallel filter for the row caches.
func (p PaletteModel) cachesForScope(scope paletteScope) []paletteRowCache {
	if scope == scopeMixed {
		return p.caches
	}
	out := make([]paletteRowCache, 0, len(p.caches))
	for i, r := range p.rows {
		if matchesScope(r.Section, scope) {
			out = append(out, p.caches[i])
		}
	}
	return out
}

func matchesScope(section string, scope paletteScope) bool {
	switch scope {
	case scopeFolders:
		return section == sectionFolders
	case scopeSavedSearches:
		return section == sectionSavedSearches
	case scopeCommands:
		return section == sectionCommands
	}
	return true
}

// scopeLabel returns the right-side header glyph for the scope.
func scopeLabel(s paletteScope) string {
	switch s {
	case scopeFolders:
		return "(folders)"
	case scopeSavedSearches:
		return "(saved searches)"
	case scopeCommands:
		return "(commands)"
	}
	return "(mixed)"
}

// View renders the palette as a centered modal. width / height are
// the full terminal dimensions; the modal sizes itself from them.
func (p PaletteModel) View(t Theme, _ KeyMap, width, height int) string {
	modalW := width / 2
	if modalW < 60 {
		modalW = 60
	}
	if modalW > 80 {
		modalW = 80
	}
	if modalW > width-2 {
		modalW = width - 2
	}
	if modalW < 30 {
		modalW = width - 2
		if modalW < 1 {
			modalW = 1
		}
	}
	dropBindings := modalW < 30
	contentW := modalW - 4
	if contentW < 10 {
		contentW = 10
	}
	modalH := 20
	if modalH > height-4 {
		modalH = height - 4
	}
	if modalH < 8 {
		modalH = 8
	}

	var b strings.Builder
	if dropBindings {
		b.WriteString(t.Dim.Render("(palette: terminal too narrow for binding hints)"))
		b.WriteString("\n")
	}
	header := t.HelpKey.Render("Command palette")
	scopeHint := t.Dim.Render(scopeLabel(p.scope))
	b.WriteString(padBetween(header, scopeHint, contentW))
	b.WriteString("\n")
	b.WriteString("> " + p.buf + "▎")
	b.WriteString("\n\n")

	// Empty-buffer view: surface "Recent" header when we have any
	// recents matched against the current row index.
	showRecentHeader := p.buf == "" && p.scope == scopeMixed && p.hasRecentMatches()
	if showRecentHeader {
		b.WriteString(t.Dim.Render("Recent"))
		b.WriteString("\n")
	}

	maxRows := modalH - 6
	if maxRows < 3 {
		maxRows = 3
	}
	start, end := paletteWindow(p.cursor, len(p.filtered), maxRows)
	if len(p.filtered) == 0 {
		switch p.scope {
		case scopeFolders:
			b.WriteString(t.Dim.Render("  no folders match — Esc to close, Backspace for commands"))
		case scopeSavedSearches:
			b.WriteString(t.Dim.Render("  no saved searches match — Esc to close, Backspace for commands"))
		case scopeCommands:
			b.WriteString(t.Dim.Render("  no commands match — Esc to close"))
		default:
			b.WriteString(t.Dim.Render("  no matches"))
		}
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		sr := p.filtered[i]
		row := sr.row
		marker := "  "
		if i == p.cursor {
			marker = "▶ "
		}
		// Section badge in mixed scope only.
		title := row.Title
		if p.scope == scopeMixed && row.Section != sectionCommands {
			title = t.Dim.Render("["+sectionTag(row.Section)+"]") + " " + title
		}
		left := marker + title
		bindCol := ""
		if !dropBindings {
			bindCol = row.Binding
		}
		line := padBetween(left, bindCol, contentW)
		if !row.Available.OK {
			line = t.Dim.Render(line)
		} else if i == p.cursor {
			line = t.HelpKey.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	footer := paletteFooter(p)
	b.WriteString(t.Dim.Render(footer))

	box := t.Modal.Width(modalW).Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// hasRecentMatches reports whether any recents id resolves to a row
// in the current row list.
func (p PaletteModel) hasRecentMatches() bool {
	if len(p.recents) == 0 {
		return false
	}
	ids := make(map[string]bool, len(p.rows))
	for _, r := range p.rows {
		ids[r.ID] = true
	}
	for _, id := range p.recents {
		if ids[id] {
			return true
		}
	}
	return false
}

// sectionTag is the short badge label for a Section.
func sectionTag(s string) string {
	switch s {
	case sectionFolders:
		return "Folders"
	case sectionSavedSearches:
		return "Saved"
	}
	return "Cmd"
}

// paletteFooter is the status line at the bottom of the modal.
func paletteFooter(p PaletteModel) string {
	count := len(p.filtered)
	total := len(p.rowsForScope(p.scope))
	var s string
	if p.buf == "" {
		s = fmt.Sprintf("%d commands available  ·  type to search  ·  ⎋ close", total)
	} else {
		s = fmt.Sprintf("%d of %d rows  ·  ↑/↓ navigate  ·  ⏎ run  ·  ⎋ close", count, total)
	}
	return s
}

// padBetween renders "left" left-aligned and "right" right-aligned
// inside a string of total width width. If the two collide, "right"
// is dropped.
func padBetween(left, right string, width int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	if lw+rw+1 > width {
		// Truncate left, drop right.
		return truncateToWidth(left, width)
	}
	pad := width - lw - rw
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

// truncateToWidth shortens s with an ellipsis when it exceeds width.
// Respects multi-byte runes and ANSI styling at coarse granularity.
func truncateToWidth(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return strings.Repeat(".", width)
	}
	rs := []rune(s)
	for i := len(rs) - 1; i >= 0; i-- {
		cand := string(rs[:i]) + "…"
		if lipgloss.Width(cand) <= width {
			return cand
		}
	}
	return "…"
}

// paletteWindow returns (start, end) so the cursor stays visible
// within max rendered rows.
func paletteWindow(cursor, total, max int) (int, int) {
	if total <= max {
		return 0, total
	}
	start := 0
	if cursor >= max {
		start = cursor - max + 1
	}
	end := start + max
	if end > total {
		end = total
		start = end - max
	}
	if start < 0 {
		start = 0
	}
	return start, end
}
