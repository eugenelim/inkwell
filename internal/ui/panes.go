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
// its children, etc.) and a flag for whether it has any children
// (used to render the disclosure glyph).
//
// Saved searches piggy-back on this type with isSaved=true; the
// displayed name comes from savedName/savedPattern, and the row
// renders with a leading ☆ glyph.
type displayedFolder struct {
	f        store.Folder
	depth    int
	hasKids  bool
	expanded bool
	// Saved-search fields (used when isSaved). The dispatcher reads
	// savedPattern when Enter fires.
	isSaved      bool
	savedName    string
	savedPattern string
	// isSavedHeader marks the synthetic "Saved Searches" section
	// divider — non-selectable, not a saved search itself.
	isSavedHeader bool
}

// FoldersModel is the sidebar pane. It stores the raw folders + per-id
// expansion state, then computes the displayed tree on demand. The
// cursor is an index into the currently-visible items.
type FoldersModel struct {
	raw      []store.Folder
	saved    []SavedSearch
	expanded map[string]bool // folder ID → is-expanded
	items    []displayedFolder
	cursor   int
}

// NewFolders returns an empty folders pane. The default expansion
// rule (applied per-folder on first sight): Inbox is expanded so the
// user sees their nested project folders immediately; everything else
// starts collapsed to keep the sidebar tidy in big mailboxes.
func NewFolders() FoldersModel {
	return FoldersModel{expanded: map[string]bool{}}
}

// SetFolders replaces the displayed list (called from FoldersLoadedMsg).
// Tops are ordered Inbox → Sent → Drafts → Archive → user (alpha) →
// Junk / Deleted / etc. Children of any folder are sorted alphabetically
// regardless of well-known status. Folders with children render a
// disclosure glyph; the user toggles with `o`/space (KeyMap.Expand).
func (m *FoldersModel) SetFolders(fs []store.Folder) {
	if m.expanded == nil {
		m.expanded = map[string]bool{}
	}
	// Default-expand the Inbox so first-launch users see something
	// useful even if their account has only nested user folders.
	for _, f := range fs {
		if f.WellKnownName == "inbox" {
			if _, set := m.expanded[f.ID]; !set {
				m.expanded[f.ID] = true
			}
		}
	}
	m.raw = fs
	m.rebuild()
	if m.cursor >= len(m.items) {
		m.cursor = 0
	}
}

// SetSavedSearches replaces the saved-search list and rebuilds the
// displayed tree. Called once at Init from the [[saved_searches]]
// config block; the runtime list isn't mutable in v0.7.0.
func (m *FoldersModel) SetSavedSearches(s []SavedSearch) {
	m.saved = s
	m.rebuild()
}

// rebuild recomputes m.items from m.raw + m.expanded + m.saved.
func (m *FoldersModel) rebuild() {
	m.items = flattenFolderTree(m.raw, m.expanded)
	if len(m.saved) > 0 {
		m.items = append(m.items, displayedFolder{isSavedHeader: true})
		for _, s := range m.saved {
			m.items = append(m.items, displayedFolder{
				isSaved:      true,
				savedName:    s.Name,
				savedPattern: s.Pattern,
			})
		}
	}
}

// ToggleExpand flips the expansion state of the folder under the
// cursor. No-op if the folder has no children.
func (m *FoldersModel) ToggleExpand() {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return
	}
	cur := m.items[m.cursor]
	if !cur.hasKids {
		return
	}
	id := cur.f.ID
	m.expanded[id] = !m.expanded[id]
	m.rebuild()
	// Keep the cursor on the same folder after rebuild.
	for i, it := range m.items {
		if it.f.ID == id {
			m.cursor = i
			break
		}
	}
}

// flattenFolderTree returns folders in the order they should appear in
// the sidebar. Roots ranked by [folderRank], children indented under
// their parent and sorted alphabetically. Children of a collapsed
// parent are skipped.
func flattenFolderTree(fs []store.Folder, expanded map[string]bool) []displayedFolder {
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
		kids := childrenOf[parent.ID]
		hasKids := len(kids) > 0
		isExpanded := hasKids && expanded[parent.ID]
		out = append(out, displayedFolder{
			f:        parent,
			depth:    depth,
			hasKids:  hasKids,
			expanded: isExpanded,
		})
		if !isExpanded {
			return
		}
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

// Up moves the cursor toward the top, skipping non-selectable
// section-header rows.
func (m *FoldersModel) Up() {
	for i := m.cursor - 1; i >= 0; i-- {
		if !m.items[i].isSavedHeader {
			m.cursor = i
			return
		}
	}
}

// Down moves the cursor toward the bottom, skipping headers.
func (m *FoldersModel) Down() {
	for i := m.cursor + 1; i < len(m.items); i++ {
		if !m.items[i].isSavedHeader {
			m.cursor = i
			return
		}
	}
}

// Selected returns the highlighted folder, if any. Returns ok=false
// when the cursor is on a saved-search row (callers should test
// SelectedSavedSearch first) or a section header.
func (m FoldersModel) Selected() (store.Folder, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return store.Folder{}, false
	}
	it := m.items[m.cursor]
	if it.isSaved || it.isSavedHeader {
		return store.Folder{}, false
	}
	return it.f, true
}

// SelectedSavedSearch returns the highlighted saved search, if the
// cursor is on one.
func (m FoldersModel) SelectedSavedSearch() (SavedSearch, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return SavedSearch{}, false
	}
	it := m.items[m.cursor]
	if !it.isSaved {
		return SavedSearch{}, false
	}
	return SavedSearch{Name: it.savedName, Pattern: it.savedPattern}, true
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
		// Saved-searches section header — non-selectable divider.
		if it.isSavedHeader {
			rows = append(rows, t.Dim.Render("  Saved Searches"))
			continue
		}
		var line string
		if it.isSaved {
			marker := "  "
			if i == m.cursor && focused {
				marker = "▶ "
			} else if i == m.cursor {
				marker = "· "
			}
			line = marker + "☆ " + it.savedName
			styled := truncate(line, width-1)
			if i == m.cursor && focused {
				styled = t.FoldersSel.Render(styled)
			}
			rows = append(rows, styled)
			continue
		}
		f := it.f
		indent := strings.Repeat("  ", it.depth)
		// Disclosure glyph for folders with children: ▾ open, ▸ closed.
		// Leaf folders get a 2-space gap so names align.
		disclosure := "  "
		if it.hasKids {
			if it.expanded {
				disclosure = "▾ "
			} else {
				disclosure = "▸ "
			}
		}
		line = indent + disclosure + f.DisplayName
		if f.UnreadCount > 0 {
			line = fmt.Sprintf("%s  %d", line, f.UnreadCount)
		}
		marker := "  "
		if i == m.cursor && focused {
			marker = "▶ "
		} else if i == m.cursor {
			marker = "· "
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
	// loadLimit is the current store.ListMessages limit. Starts at the
	// initial-load default and grows as the user scrolls down. The UI
	// fires a "load more" Cmd when the cursor approaches the bottom
	// of the currently-loaded slice.
	loadLimit int
	// loading is true while a load-more Cmd is in flight; prevents
	// duplicate requests from rapid j-presses.
	loading bool
	// cacheExhausted is true when the last load returned fewer rows
	// than loadLimit — the local store has nothing more to give at
	// this limit. ShouldLoadMore returns false in that state to stop
	// flapping reloads. Cleared on folder switch (ResetLimit) and
	// rechecked on every SetMessages.
	cacheExhausted bool
	// wallSyncRequested is the debounce flag: true after we've
	// already kicked a sync for the current cache-exhausted state.
	// Cleared on the next SetMessages so the next time the user
	// arrives at the wall we kick again. Without this, every j
	// press at the wall fired SyncAll → real-tenant log showed 3
	// cycles in 2.5s.
	wallSyncRequested bool
}

// initialListLimit is the first-page size for the list pane.
const initialListLimit = 200

// loadMoreThreshold is the number of unviewed rows below the cursor
// at which we trigger the next page. 20 rows ahead means the user
// scrolling at 1 row/100ms gets a fresh slice ~2s before they hit the
// edge — typically faster than a SQLite read of another 200 rows.
const loadMoreThreshold = 20

// pageIncrement is how much we extend the limit on each load-more.
const pageIncrement = 200

// NewList returns an empty list pane.
func NewList() ListModel { return ListModel{loadLimit: initialListLimit} }

// SetMessages replaces the displayed list. Keeps the cursor on the same
// message id when possible (so a load-more refresh doesn't yank the
// user's selection back to row 0). Marks cacheExhausted when the
// returned page is shorter than the requested limit — that signals
// the local store has nothing more to give until a sync delivers
// fresh messages.
func (m *ListModel) SetMessages(ms []store.Message) {
	prevID := ""
	if m.cursor < len(m.messages) {
		prevID = m.messages[m.cursor].ID
	}
	m.messages = ms
	m.loading = false
	m.cacheExhausted = len(ms) < m.LoadLimit()
	m.wallSyncRequested = false // fresh load → allow another wall-sync
	if prevID != "" {
		for i, msg := range ms {
			if msg.ID == prevID {
				m.cursor = i
				return
			}
		}
	}
	if m.cursor >= len(ms) {
		m.cursor = 0
	}
}

// LoadLimit reports the current page size for store.ListMessages.
func (m ListModel) LoadLimit() int {
	if m.loadLimit <= 0 {
		return initialListLimit
	}
	return m.loadLimit
}

// ShouldLoadMore returns true when the cursor is close enough to the
// bottom that we should pre-fetch the next page. False once a load is
// already in flight, or when the local store is exhausted at the
// current limit (no point asking SQLite for more rows it doesn't
// have; the engine's foreground sync will deliver more eventually).
func (m ListModel) ShouldLoadMore() bool {
	if m.loading || m.cacheExhausted {
		return false
	}
	if len(m.messages) == 0 {
		return false
	}
	return m.cursor >= len(m.messages)-loadMoreThreshold
}

// MarkLoading flips the loading flag and bumps the load limit by one
// page increment.
func (m *ListModel) MarkLoading() {
	m.loading = true
	m.loadLimit = m.LoadLimit() + pageIncrement
}

// AtCacheWall returns true when the cursor sits at the last row of a
// list that's exhausted the local store. Caller can use this to kick
// a sync so the engine pulls more from Graph.
func (m ListModel) AtCacheWall() bool {
	return m.cacheExhausted && len(m.messages) > 0 && m.cursor == len(m.messages)-1
}

// ShouldKickWallSync returns true when the cursor is at the cache
// wall AND we haven't already requested a sync for this state. The
// caller flips wallSyncRequested via [MarkWallSyncRequested] after
// firing the Cmd, so subsequent j-presses don't re-fire until the
// next SetMessages.
func (m ListModel) ShouldKickWallSync() bool {
	return m.AtCacheWall() && !m.wallSyncRequested
}

// MarkWallSyncRequested arms the wall-sync debounce flag.
func (m *ListModel) MarkWallSyncRequested() { m.wallSyncRequested = true }

// ResetLimit collapses the load limit back to the initial page and
// clears the exhausted flag (used when the user switches folders —
// the new folder's cache state is unknown).
func (m *ListModel) ResetLimit() {
	m.loadLimit = initialListLimit
	m.loading = false
	m.cacheExhausted = false
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
		// Meeting-invite indicator: a leading 📅 glyph for messages that
		// are recognisable as calendar invites or invite responses
		// (Accepted: / Declined: / Updated: / etc.). Heuristic — see
		// isLikelyMeeting. v0.9 will read Graph's meetingMessageType
		// via $select for an exact signal.
		invite := "  "
		if isLikelyMeeting(msg.Subject) {
			invite = "📅 "
		}
		line := fmt.Sprintf("%s%s%-10s %-14s %s", marker, invite, when, truncate(from, 14), msg.Subject)
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

// ToggleHeaders flips between compact (default) and full header
// display. Compact shows From/Date/Subject + To collapsed; full
// shows every recipient on every line.
func (m *ViewerModel) ToggleHeaders() {
	m.showFullHdr = !m.showFullHdr
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
	// Header layout. Compact (default) shows From/Date/Subject + a
	// collapsed To line ("first 3 + N more"). Full (capital H) shows
	// every recipient on every line. Many-attendee emails would
	// otherwise eat the entire viewer pane and starve the body.
	hdrs := []string{
		header,
		"From:    " + from,
		"Date:    " + m.current.ReceivedAt.Format(time.RFC1123),
		"Subject: " + m.current.Subject,
	}
	if m.showFullHdr {
		hdrs = append(hdrs,
			"To:      "+joinAddrs(m.current.ToAddresses),
		)
		if len(m.current.CcAddresses) > 0 {
			hdrs = append(hdrs, "Cc:      "+joinAddrs(m.current.CcAddresses))
		}
		if len(m.current.BccAddresses) > 0 {
			hdrs = append(hdrs, "Bcc:     "+joinAddrs(m.current.BccAddresses))
		}
	} else {
		hdrs = append(hdrs, "To:      "+compactAddrs(m.current.ToAddresses, m.current.CcAddresses, m.current.BccAddresses))
	}
	hdrs = append(hdrs, "")
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

// compactAddrs renders a one-line summary of all recipients across
// To/Cc/Bcc: first three To names + " + N more" if there are more
// than three To addresses or any Cc/Bcc. Designed to fit on a single
// pane row regardless of attendee count. Press capital H in the
// viewer to expand to full To/Cc/Bcc lines.
func compactAddrs(to, cc, bcc []store.EmailAddress) string {
	total := len(to) + len(cc) + len(bcc)
	if total == 0 {
		return "—"
	}
	const showTo = 3
	var parts []string
	for i, a := range to {
		if i >= showTo {
			break
		}
		parts = append(parts, addrShort(a))
	}
	more := total - len(parts)
	out := strings.Join(parts, ", ")
	if more > 0 {
		out += fmt.Sprintf("  + %d more (press H to expand)", more)
	}
	return out
}

// addrShort prefers the display name; falls back to the address.
func addrShort(a store.EmailAddress) string {
	if a.Name != "" {
		return a.Name
	}
	return a.Address
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
	LastSync  time.Time
	Throttled time.Duration
	Activity  string // "syncing folders…" / "syncing…" / "" (idle)
	LastErr   error  // most recent SyncFailedEvent, if any
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

// meetingPrefixes is the set of subject-line prefixes that indicate a
// calendar-invite-style message. Detected case-insensitively. Covers
// the response messages Outlook generates when attendees act on an
// invite, plus common original-invite forms.
var meetingPrefixes = []string{
	"accepted:",
	"declined:",
	"tentative:",
	"tentatively accepted:",
	"canceled:",
	"cancelled:",
	"updated:",
	"meeting:",
	"invitation:",
	"new time proposed:",
	"forwarded invitation:",
}

// isLikelyMeeting reports whether subject's prefix matches one of the
// known invite/response forms. Heuristic — covers the common cases
// without a schema change. Future iter ($select meetingMessageType)
// will replace this with the canonical Graph signal.
func isLikelyMeeting(subject string) bool {
	s := strings.ToLower(strings.TrimSpace(subject))
	for _, p := range meetingPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
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
