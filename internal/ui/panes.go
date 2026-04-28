package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eugenelim/inkwell/internal/store"
)

// displayedFolder is one row in the rendered sidebar tree: the source
// folder plus the depth at which it appears (0 for top-level, 1 for
// its children, etc.).
type displayedFolder struct {
	f     store.Folder
	depth int
}

// FoldersModel is the sidebar pane. It stores the folders pre-flattened
// into display order so the cursor is a simple index into a slice and
// View doesn't need to re-walk the tree on every render.
type FoldersModel struct {
	items  []displayedFolder
	cursor int
}

// NewFolders returns an empty folders pane.
func NewFolders() FoldersModel { return FoldersModel{} }

// SetFolders replaces the displayed list (called from FoldersLoadedMsg).
// Tops are ordered Inbox → Sent → Drafts → Archive → user (alpha) →
// Junk / Deleted / etc. Children of any folder are sorted alphabetically
// regardless of well-known status (well-known names don't typically
// nest under each other).
func (m *FoldersModel) SetFolders(fs []store.Folder) {
	m.items = flattenFolderTree(fs)
	if m.cursor >= len(m.items) {
		m.cursor = 0
	}
}

// flattenFolderTree returns folders in the order they should appear in
// the sidebar: top-level folders ranked by [folderRank], children
// indented under their parent and sorted alphabetically.
func flattenFolderTree(fs []store.Folder) []displayedFolder {
	if len(fs) == 0 {
		return nil
	}
	tracked := make(map[string]bool, len(fs))
	for _, f := range fs {
		tracked[f.ID] = true
	}
	childrenOf := make(map[string][]store.Folder)
	for _, f := range fs {
		// Top-level: parent is empty OR parent points to a folder we
		// don't track (msgfolderroot, etc.). syncFolders already NULLs
		// out untracked parents, but be defensive here too.
		key := f.ParentFolderID
		if key != "" && !tracked[key] {
			key = ""
		}
		childrenOf[key] = append(childrenOf[key], f)
	}
	roots := childrenOf[""]
	sortRootFolders(roots)
	out := make([]displayedFolder, 0, len(fs))
	var walk func(parent store.Folder, depth int)
	walk = func(parent store.Folder, depth int) {
		out = append(out, displayedFolder{f: parent, depth: depth})
		kids := childrenOf[parent.ID]
		sort.SliceStable(kids, func(i, j int) bool {
			return strings.ToLower(kids[i].DisplayName) < strings.ToLower(kids[j].DisplayName)
		})
		for _, k := range kids {
			walk(k, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out
}

// sortRootFolders sorts in place by [folderRank] then alphabetically.
func sortRootFolders(roots []store.Folder) {
	sort.SliceStable(roots, func(i, j int) bool {
		ri, rj := folderRank(roots[i]), folderRank(roots[j])
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(roots[i].DisplayName) < strings.ToLower(roots[j].DisplayName)
	})
}

// folderRank assigns a sort position to a top-level folder. Inbox first,
// then the other transactional folders, then user folders (alpha among
// themselves), then the rarely-visited well-known folders at the bottom.
func folderRank(f store.Folder) int {
	switch f.WellKnownName {
	case "inbox":
		return 0
	case "sentitems":
		return 1
	case "drafts":
		return 2
	case "archive":
		return 3
	case "junkemail":
		return 5
	case "deleteditems":
		return 6
	case "conversationhistory":
		return 7
	case "syncissues":
		return 8
	default:
		return 4
	}
}

// Up moves the cursor toward the top.
func (m *FoldersModel) Up() {
	if m.cursor > 0 {
		m.cursor--
	}
}

// Down moves the cursor toward the bottom.
func (m *FoldersModel) Down() {
	if m.cursor+1 < len(m.items) {
		m.cursor++
	}
}

// Selected returns the highlighted folder, if any.
func (m FoldersModel) Selected() (store.Folder, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return store.Folder{}, false
	}
	return m.items[m.cursor].f, true
}

// SelectByID moves the cursor onto the folder with the given id.
// No-op if not present.
func (m *FoldersModel) SelectByID(id string) {
	for i, it := range m.items {
		if it.f.ID == id {
			m.cursor = i
			return
		}
	}
}

// View renders the folders column.
func (m FoldersModel) View(t Theme, width, height int, focused bool) string {
	lines := []string{paneHeader(t, "Folders", focused)}
	if len(m.items) == 0 {
		lines = append(lines, t.Dim.Render("  (waiting…)"))
	}
	rows := make([]string, 0, len(m.items))
	for i, it := range m.items {
		f := it.f
		// Indentation: 2 spaces per depth level. Subfolders live to
		// the right of their parent, like a tree pane in any other
		// mail client.
		indent := strings.Repeat("  ", it.depth)
		line := indent + f.DisplayName
		if f.UnreadCount > 0 {
			line = fmt.Sprintf("%s  %d", line, f.UnreadCount)
		}
		marker := "  "
		if i == m.cursor && focused {
			marker = "▶ "
		} else if i == m.cursor {
			marker = "▸ "
		}
		styled := truncate(marker+line, width-1)
		if i == m.cursor && focused {
			styled = t.FoldersSel.Render(styled)
		}
		rows = append(rows, styled)
	}
	visible := clipToCursorViewport(rows, m.cursor, height-len(lines))
	lines = append(lines, visible...)
	return t.Folders.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

// ListModel is the message-list pane.
type ListModel struct {
	FolderID string
	messages []store.Message
	cursor   int
}

// NewList returns an empty list pane.
func NewList() ListModel { return ListModel{} }

// SetMessages replaces the displayed list.
func (m *ListModel) SetMessages(ms []store.Message) {
	m.messages = ms
	if m.cursor >= len(ms) {
		m.cursor = 0
	}
}

// Up / Down / Selected mirror the folders pane.
func (m *ListModel) Up() {
	if m.cursor > 0 {
		m.cursor--
	}
}

// Down moves the cursor toward newer messages.
func (m *ListModel) Down() {
	if m.cursor+1 < len(m.messages) {
		m.cursor++
	}
}

// Selected returns the highlighted message, if any.
func (m ListModel) Selected() (store.Message, bool) {
	if m.cursor < 0 || m.cursor >= len(m.messages) {
		return store.Message{}, false
	}
	return m.messages[m.cursor], true
}

// View renders the message column.
func (m ListModel) View(t Theme, width, height int, focused bool) string {
	header := paneHeader(t, "Messages", focused)
	if m.FolderID == "" {
		body := strings.Join([]string{header, t.Dim.Render("  (select a folder)")}, "\n")
		return t.List.Width(width).Height(height).Render(body)
	}
	rows := make([]string, 0, len(m.messages))
	for i, msg := range m.messages {
		when := relativeWhen(msg.ReceivedAt)
		from := msg.FromName
		if from == "" {
			from = msg.FromAddress
		}
		marker := "  "
		if i == m.cursor && focused {
			marker = "▶ "
		} else if i == m.cursor {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%-10s %-14s %s", marker, when, truncate(from, 14), msg.Subject)
		styled := truncate(line, width-1)
		if i == m.cursor && focused {
			styled = t.ListSel.Render(styled)
		} else if !msg.IsRead {
			styled = t.ListUnread.Render(styled)
		}
		rows = append(rows, styled)
	}
	visible := clipToCursorViewport(rows, m.cursor, height-1)
	out := append([]string{header}, visible...)
	return t.List.Width(width).Height(height).Render(strings.Join(out, "\n"))
}

// ViewerModel is the read pane. Headers + body + attachments routed
// through internal/render.
type ViewerModel struct {
	current     *store.Message
	body        string
	bodyState   int // mirrors render.BodyState; kept as int to avoid import cycle
	showFullHdr bool
	scrollY     int // body line offset for j/k scroll
}

// NewViewer returns an empty viewer.
func NewViewer() ViewerModel { return ViewerModel{} }

// SetMessage replaces the displayed message; clears any prior body
// and resets the scroll offset.
func (m *ViewerModel) SetMessage(msg store.Message) {
	m.current = &msg
	m.body = ""
	m.bodyState = 0
	m.scrollY = 0
}

// SetBody is invoked after a fetch completes (or the cache hits).
func (m *ViewerModel) SetBody(text string, state int) {
	m.body = text
	m.bodyState = state
}

// ScrollDown advances the body viewport by one line.
func (m *ViewerModel) ScrollDown() {
	m.scrollY++
}

// ScrollUp moves the body viewport up by one line.
func (m *ViewerModel) ScrollUp() {
	if m.scrollY > 0 {
		m.scrollY--
	}
}

// CurrentMessageID returns the id of the currently-displayed message,
// or empty if none.
func (m ViewerModel) CurrentMessageID() string {
	if m.current == nil {
		return ""
	}
	return m.current.ID
}

// View renders the viewer column.
func (m ViewerModel) View(t Theme, width, height int, focused bool) string {
	header := paneHeader(t, "Message", focused)
	if m.current == nil {
		body := strings.Join([]string{header, t.Dim.Render("  (no message selected)")}, "\n")
		return t.Viewer.Width(width).Height(height).Render(body)
	}
	from := m.current.FromName
	if from == "" {
		from = m.current.FromAddress
	}
	hdrs := []string{
		header,
		"From:    " + from,
		"To:      " + joinAddrs(m.current.ToAddresses),
		"Date:    " + m.current.ReceivedAt.Format(time.RFC1123),
		"Subject: " + m.current.Subject,
		"",
	}
	body := m.body
	if body == "" {
		body = t.Dim.Render("(loading…)")
	}
	bodyLines := strings.Split(body, "\n")
	// Apply scroll offset and clip to remaining height. The window
	// renders [scrollY, scrollY+room) of the body; scrolling past EOF
	// is harmless (we just show fewer rows).
	room := height - len(hdrs)
	if room < 1 {
		room = 1
	}
	if m.scrollY >= len(bodyLines) {
		bodyLines = nil
	} else {
		bodyLines = bodyLines[m.scrollY:]
	}
	if len(bodyLines) > room {
		bodyLines = bodyLines[:room]
	}
	out := append(hdrs, bodyLines...)
	return t.Viewer.Width(width).Height(height).Render(strings.Join(out, "\n"))
}

// joinAddrs renders a recipient list as "name <addr>, name2 <addr2>".
func joinAddrs(rs []store.EmailAddress) string {
	var parts []string
	for _, a := range rs {
		if a.Name != "" {
			parts = append(parts, a.Name+" <"+a.Address+">")
		} else {
			parts = append(parts, a.Address)
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

// CommandModel is the `:command` line.
type CommandModel struct {
	buf    string
	active bool
}

// NewCommand returns an empty command bar.
func NewCommand() CommandModel { return CommandModel{} }

// Activate clears the buffer and marks the bar as accepting input.
func (m *CommandModel) Activate() {
	m.active = true
	m.buf = ""
}

// Reset deactivates and clears the bar.
func (m *CommandModel) Reset() {
	m.active = false
	m.buf = ""
}

// Buffer returns the current entered text.
func (m CommandModel) Buffer() string { return m.buf }

// HandleKey appends or backspaces buffered text.
func (m *CommandModel) HandleKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyBackspace:
		if len(m.buf) > 0 {
			m.buf = m.buf[:len(m.buf)-1]
		}
	default:
		m.buf += msg.String()
	}
}

// View renders the command line.
func (m CommandModel) View(t Theme, width int, active bool) string {
	if !active && m.buf == "" {
		return strings.Repeat(" ", width)
	}
	return t.CommandBar.Render(":" + m.buf)
}

// StatusModel is the top status bar.
type StatusModel struct {
	upn    string
	tenant string
}

// NewStatus returns the top status bar prefilled with the signed-in account.
func NewStatus(upn, tenant string) StatusModel {
	return StatusModel{upn: upn, tenant: tenant}
}

// StatusInputs is the per-frame state the status bar consumes. Values
// are passed by value each render so the StatusModel itself stays a
// stable identity holder (UPN, tenant) — transient state lives in the
// root Model.
type StatusInputs struct {
	LastSync   time.Time
	Throttled  time.Duration
	Activity   string // "syncing folders…" / "syncing…" / "" (idle)
	LastErr    error  // most recent SyncFailedEvent, if any
}

// View renders the status line.
func (m StatusModel) View(t Theme, width int, in StatusInputs) string {
	left := "☰ inkwell"
	if m.upn != "" {
		left += " · " + m.upn
	}

	// Right side: errors > throttled > activity > last sync > idle.
	var right string
	switch {
	case in.LastErr != nil:
		errMsg := in.LastErr.Error()
		// Trim very long errors so they don't blow the line. The full
		// text is in the log file via the redactor.
		if len(errMsg) > 120 {
			errMsg = errMsg[:117] + "…"
		}
		right = t.ErrorBar.Render("ERR: " + errMsg)
	case in.Throttled > 0:
		right = t.Throttled.Render(fmt.Sprintf("⏳ throttled %ds", int(in.Throttled.Seconds())))
	case in.Activity != "":
		right = t.Throttled.Render(in.Activity)
	case !in.LastSync.IsZero():
		right = "✓ synced " + in.LastSync.Format("15:04")
	default:
		right = t.Dim.Render("waiting for sync…")
	}

	pad := width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return t.Status.Render(left + strings.Repeat(" ", pad) + right)
}

// SignInModel is the device-code prompt.
type SignInModel struct {
	UserCode        string
	VerificationURL string
}

// NewSignIn returns an empty sign-in modal.
func NewSignIn() SignInModel { return SignInModel{} }

// Set populates the modal with the prompt data delivered by the auth
// package.
func (m *SignInModel) Set(userCode, url string) {
	m.UserCode = userCode
	m.VerificationURL = url
}

// View renders the sign-in modal centered on the screen.
func (m SignInModel) View(t Theme, width, height int) string {
	body := "Sign in to Microsoft 365\n\n" +
		"Open: " + m.VerificationURL + "\n" +
		"Code: " + m.UserCode + "\n\n" +
		t.Dim.Render("(press Esc to cancel)")
	if m.VerificationURL == "" {
		body = "Waiting for sign-in…\n\n" + t.Dim.Render("(press Esc to cancel)")
	}
	box := t.Modal.Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// ConfirmModel is the y/N modal.
type ConfirmModel struct {
	Topic   string
	Message string
}

// NewConfirm returns an empty confirm modal.
func NewConfirm() ConfirmModel { return ConfirmModel{} }

// Ask returns a confirm modal seeded with topic + message.
func (m ConfirmModel) Ask(message, topic string) ConfirmModel {
	return ConfirmModel{Topic: topic, Message: message}
}

// View renders the y/N prompt.
func (m ConfirmModel) View(t Theme, width, height int) string {
	body := m.Message + "\n\n[y]es / [N]o"
	box := t.Modal.Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// paneHeader renders a pane title with a focus-state marker. Every
// pane uses this so the user always sees which pane has focus,
// independent of terminal color support: "▌ Messages" when focused,
// "  Messages" otherwise. Bold styling on top.
func paneHeader(t Theme, title string, focused bool) string {
	if focused {
		return t.Bold.Render("▌ " + title)
	}
	return t.Dim.Render("  " + title)
}

// clipToCursorViewport returns the slice of `rows` that fits in `room`
// rows while keeping `cursor` visible. If `rows` already fits, the
// whole list is returned. Otherwise we slide the window so the cursor
// stays inside the visible range.
func clipToCursorViewport(rows []string, cursor, room int) []string {
	if room <= 0 {
		return nil
	}
	if len(rows) <= room {
		return rows
	}
	// Center the cursor when possible. Top edge clamped to 0; bottom
	// edge clamped so we never overshoot the slice.
	top := cursor - room/2
	if top < 0 {
		top = 0
	}
	if top+room > len(rows) {
		top = len(rows) - room
	}
	return rows[top : top+room]
}

// truncate cuts s to width characters.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	if len(r) > width {
		return string(r[:width])
	}
	return s
}

// relativeWhen returns "Mon 14:32" or "2026-04-25".
func relativeWhen(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	if now.Sub(t) < 7*24*time.Hour {
		return t.Format("Mon 15:04")
	}
	return t.Format("2006-01-02")
}
