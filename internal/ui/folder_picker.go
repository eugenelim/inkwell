package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/eugenelim/inkwell/internal/store"
)

// FolderPickerModel is the spec 07 §12.1 modal opened by `m`. It
// renders a fuzzy-filtered list of move destinations; the selected
// row dispatches a move action via Triage.Move.
//
// The picker is stateless beyond cursor + filter buffer: the source
// folder list comes from the parent FoldersModel each time the
// picker opens (so renames / new folders surface without a refresh
// of the picker), and recently-used folder IDs come from a parent
// slice the move handler maintains as session-scoped MRU. The
// session scope is intentional — cross-session recency would mean
// the cache survives schema migrations and folder ID rewrites,
// which we'd rather not reason about for a UX nicety.
type FolderPickerModel struct {
	cursor int
	buf    string
	rows   []folderPickerRow
}

// folderPickerRow is one row in the picker. id is the Graph folder
// ID; alias is the well-known name when present (rendered dim);
// recent flags rows that came from the MRU list and rank above the
// alphabetical section.
type folderPickerRow struct {
	id     string
	label  string
	alias  string
	recent bool
}

// NewFolderPicker returns the empty model.
func NewFolderPicker() FolderPickerModel { return FolderPickerModel{} }

// Reset rebuilds the row list from the supplied folders + recent
// IDs and clears the cursor + buffer. Called when the picker opens
// so state from a previous open doesn't leak into this one.
func (m *FolderPickerModel) Reset(folders []store.Folder, recentIDs []string) {
	m.cursor = 0
	m.buf = ""
	m.rows = buildFolderPickerRows(folders, recentIDs)
}

// Up moves the cursor toward the top. No-op at the edge — no wrap-
// around so j-mash doesn't accidentally cycle past the alphabetical
// section into the recent section.
func (m *FolderPickerModel) Up() {
	if m.cursor > 0 {
		m.cursor--
	}
}

// Down moves the cursor toward the bottom of the filtered list.
func (m *FolderPickerModel) Down() {
	max := len(m.filtered()) - 1
	if m.cursor < max {
		m.cursor++
	}
}

// AppendRune extends the filter buffer and resets the cursor so a
// freshly typed query doesn't leave the cursor past the now-shorter
// result list.
func (m *FolderPickerModel) AppendRune(r rune) {
	m.buf += string(r)
	m.cursor = 0
}

// Backspace drops one rune from the filter and resets the cursor.
func (m *FolderPickerModel) Backspace() {
	if len(m.buf) == 0 {
		return
	}
	runes := []rune(m.buf)
	m.buf = string(runes[:len(runes)-1])
	m.cursor = 0
}

// Buffer returns the current filter string. Used by tests to assert
// typed-input behaviour without poking unexported state.
func (m FolderPickerModel) Buffer() string { return m.buf }

// Selected returns the currently-highlighted row, or nil when the
// filtered list is empty.
func (m FolderPickerModel) Selected() *folderPickerRow {
	rows := m.filtered()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return nil
	}
	r := rows[m.cursor]
	return &r
}

// filtered returns rows matching the buffer (substring, case-
// insensitive on both label and alias). When the buffer is empty
// the full list is returned in display order.
func (m FolderPickerModel) filtered() []folderPickerRow {
	if m.buf == "" {
		return m.rows
	}
	needle := strings.ToLower(m.buf)
	out := make([]folderPickerRow, 0, len(m.rows))
	for _, r := range m.rows {
		if strings.Contains(strings.ToLower(r.label), needle) ||
			strings.Contains(strings.ToLower(r.alias), needle) {
			out = append(out, r)
		}
	}
	return out
}

// buildFolderPickerRows produces the picker's row list:
//   - recently-used folders first (in MRU order, tagged .recent)
//   - then the sidebar order from flattenFolderTree (Inbox, Sent,
//     Drafts, Archive, user folders alpha, then Junk / Deleted)
//
// Drafts is filtered out because moving a non-draft into Drafts via
// the picker is an error path Outlook itself rejects (it only
// accepts drafts created via /me/messages with isDraft=true).
func buildFolderPickerRows(folders []store.Folder, recentIDs []string) []folderPickerRow {
	if len(folders) == 0 {
		return nil
	}
	pathByID := buildFolderPaths(folders)
	folderByID := make(map[string]store.Folder, len(folders))
	for _, f := range folders {
		folderByID[f.ID] = f
	}
	expanded := make(map[string]bool, len(folders))
	for _, f := range folders {
		expanded[f.ID] = true
	}
	flat := flattenFolderTree(folders, expanded)
	rows := make([]folderPickerRow, 0, len(folders))
	seen := make(map[string]bool, len(recentIDs))
	for _, id := range recentIDs {
		f, ok := folderByID[id]
		if !ok {
			continue
		}
		if f.WellKnownName == "drafts" {
			continue
		}
		rows = append(rows, folderPickerRow{
			id:     id,
			label:  pathByID[id],
			alias:  f.WellKnownName,
			recent: true,
		})
		seen[id] = true
	}
	for _, df := range flat {
		if seen[df.f.ID] {
			continue
		}
		if df.f.WellKnownName == "drafts" {
			continue
		}
		rows = append(rows, folderPickerRow{
			id:    df.f.ID,
			label: pathByID[df.f.ID],
			alias: df.f.WellKnownName,
		})
	}
	return rows
}

// buildFolderPaths walks each folder's parent chain to build the
// path-style label ("Inbox / Project / 2025"). Used so duplicate
// child names ("Receipts" under multiple parents) stay
// disambiguated in the picker. Untracked parents (the synthetic
// msgfolderroot) terminate the walk; the spec doesn't surface them.
func buildFolderPaths(folders []store.Folder) map[string]string {
	byID := make(map[string]store.Folder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}
	out := make(map[string]string, len(folders))
	var resolve func(id string) string
	resolve = func(id string) string {
		if cached, ok := out[id]; ok {
			return cached
		}
		f, ok := byID[id]
		if !ok {
			return ""
		}
		if f.ParentFolderID == "" || f.ParentFolderID == id {
			out[id] = f.DisplayName
			return f.DisplayName
		}
		parent := resolve(f.ParentFolderID)
		if parent == "" {
			out[id] = f.DisplayName
			return f.DisplayName
		}
		path := parent + " / " + f.DisplayName
		out[id] = path
		return path
	}
	for id := range byID {
		resolve(id)
	}
	return out
}

// folderPickerVisibleRows is the soft cap on rows rendered at once.
// The cursor stays within the window via [folderPickerWindow]. The
// typed filter narrows the list quickly so most mailboxes never hit
// the cap.
const folderPickerVisibleRows = 12

// folderPickerWindow returns (start, end) indices into rows so the
// cursor stays visible. Window slides as the cursor moves rather
// than centering — keeps top-of-list rows pinned when the cursor is
// near the top, which matches list-pane scroll semantics.
func folderPickerWindow(cursor, total, max int) (int, int) {
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

// View renders the picker as a centered modal.
func (m FolderPickerModel) View(t Theme, width, height int) string {
	rows := m.filtered()
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", t.HelpKey.Render("Move to:"))
	fmt.Fprintf(&b, "%s %s▎\n\n", t.Dim.Render("filter:"), m.buf)
	if len(rows) == 0 {
		b.WriteString(t.Dim.Render("  (no folders match)\n"))
	}
	start, end := folderPickerWindow(m.cursor, len(rows), folderPickerVisibleRows)
	for i := start; i < end; i++ {
		r := rows[i]
		marker := "  "
		if i == m.cursor {
			marker = "▶ "
		}
		prefix := ""
		if r.recent {
			prefix = t.Dim.Render("[recent] ") + " "
		}
		line := marker + prefix + r.label
		if r.alias != "" && !strings.EqualFold(r.alias, r.label) {
			line += " " + t.Dim.Render("("+r.alias+")")
		}
		if i == m.cursor {
			line = t.HelpKey.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(rows) > folderPickerVisibleRows {
		fmt.Fprintf(&b, "%s\n", t.Dim.Render(fmt.Sprintf("  …(%d more, narrow with filter)", len(rows)-folderPickerVisibleRows)))
	}
	b.WriteString("\n")
	b.WriteString(t.Dim.Render("type to filter  ·  ↑/↓  navigate  ·  Enter  move  ·  Esc  cancel"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		t.Modal.Render(b.String()))
}
