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

// FoldersModel is the sidebar pane.
type FoldersModel struct {
	folders []store.Folder
	cursor  int
}

// NewFolders returns an empty folders pane.
func NewFolders() FoldersModel { return FoldersModel{} }

// SetFolders replaces the displayed list (called from FoldersLoadedMsg).
// Folders are reordered Inbox-first for sidebar display.
func (m *FoldersModel) SetFolders(fs []store.Folder) {
	m.folders = sortFoldersForSidebar(fs)
	if m.cursor >= len(m.folders) {
		m.cursor = 0
	}
}

// sortFoldersForSidebar returns folders in the canonical sidebar
// order: Inbox → Sent Items → Drafts → Archive → user folders (alpha)
// → Junk Email / Deleted Items / Conversation History / Sync Issues
// (well-known but usually uninteresting; bottom of the list).
func sortFoldersForSidebar(in []store.Folder) []store.Folder {
	rank := func(f store.Folder) int {
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
			return 4 // user folders, alphabetically among themselves
		}
	}
	out := make([]store.Folder, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}

// Up moves the cursor toward the top.
func (m *FoldersModel) Up() {
	if m.cursor > 0 {
		m.cursor--
	}
}

// Down moves the cursor toward the bottom.
func (m *FoldersModel) Down() {
	if m.cursor+1 < len(m.folders) {
		m.cursor++
	}
}

// Selected returns the highlighted folder, if any.
func (m FoldersModel) Selected() (store.Folder, bool) {
	if m.cursor < 0 || m.cursor >= len(m.folders) {
		return store.Folder{}, false
	}
	return m.folders[m.cursor], true
}

// SelectByID moves the cursor onto the folder with the given id.
// No-op if not present.
func (m *FoldersModel) SelectByID(id string) {
	for i, f := range m.folders {
		if f.ID == id {
			m.cursor = i
			return
		}
	}
}

// View renders the folders column.
func (m FoldersModel) View(t Theme, width, height int, focused bool) string {
	var b strings.Builder
	header := "Folders"
	if focused {
		header = "▌ Folders"
	}
	b.WriteString(t.Bold.Render(header))
	b.WriteByte('\n')
	if len(m.folders) == 0 {
		b.WriteString(t.Dim.Render("  (waiting…)"))
		b.WriteByte('\n')
	}
	for i, f := range m.folders {
		line := f.DisplayName
		if f.UnreadCount > 0 {
			line = fmt.Sprintf("%s  %d", line, f.UnreadCount)
		}
		if i == m.cursor && focused {
			line = t.FoldersSel.Render("▸ " + line)
		} else if i == m.cursor {
			line = "▸ " + line
		} else {
			line = "  " + line
		}
		b.WriteString(truncate(line, width-1))
		b.WriteByte('\n')
	}
	return t.Folders.Width(width).Height(height).Render(b.String())
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
	if m.FolderID == "" {
		return t.List.Width(width).Height(height).Render("(select a folder)")
	}
	var b strings.Builder
	for i, msg := range m.messages {
		when := relativeWhen(msg.ReceivedAt)
		from := msg.FromName
		if from == "" {
			from = msg.FromAddress
		}
		line := fmt.Sprintf("%-10s %-18s %s", when, truncate(from, 18), msg.Subject)
		styled := truncate(line, width-1)
		if i == m.cursor && focused {
			styled = t.ListSel.Render(styled)
		} else if !msg.IsRead {
			styled = t.ListUnread.Render(styled)
		}
		b.WriteString(styled)
		b.WriteByte('\n')
	}
	return t.List.Width(width).Height(height).Render(b.String())
}

// ViewerModel is the read pane. Headers + body + attachments routed
// through internal/render.
type ViewerModel struct {
	current      *store.Message
	body         string
	bodyState    int // mirrors render.BodyState; kept as int to avoid import cycle
	showFullHdr  bool
}

// NewViewer returns an empty viewer.
func NewViewer() ViewerModel { return ViewerModel{} }

// SetMessage replaces the displayed message; clears any prior body.
func (m *ViewerModel) SetMessage(msg store.Message) {
	m.current = &msg
	m.body = ""
	m.bodyState = 0
}

// SetBody is invoked after a fetch completes (or the cache hits).
func (m *ViewerModel) SetBody(text string, state int) {
	m.body = text
	m.bodyState = state
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
func (m ViewerModel) View(t Theme, width, height int, _ bool) string {
	if m.current == nil {
		return t.Viewer.Width(width).Height(height).Render("(no message selected)")
	}
	from := m.current.FromName
	if from == "" {
		from = m.current.FromAddress
	}
	headers := lipgloss.JoinVertical(lipgloss.Left,
		"From:    "+from,
		"To:      "+joinAddrs(m.current.ToAddresses),
		"Date:    "+m.current.ReceivedAt.Format(time.RFC1123),
		"Subject: "+m.current.Subject,
		"",
	)
	body := m.body
	if body == "" {
		body = t.Dim.Render("(loading…)")
	}
	return t.Viewer.Width(width).Height(height).Render(headers + body)
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
