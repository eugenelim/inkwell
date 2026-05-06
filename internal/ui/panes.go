package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/eugenelim/inkwell/internal/render"
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
//
// Calendar events piggy-back with isCalEvent=true (spec 12 sidebar).
type displayedFolder struct {
	f        store.Folder
	depth    int
	hasKids  bool
	expanded bool
	// Saved-search fields (used when isSaved). The dispatcher reads
	// savedPattern when Enter fires; savedID + savedCount drive the
	// sidebar count badge (spec 11 §5.1).
	isSaved      bool
	savedID      int64
	savedName    string
	savedPattern string
	savedPinned  bool
	savedCount   int // -1 = not yet evaluated; ≥0 = match count from last refresh
	// isSavedHeader marks the synthetic "Saved Searches" section
	// divider — non-selectable, not a saved search itself.
	isSavedHeader bool
	// Calendar event fields (spec 12 sidebar). isCalHeader marks
	// a day-divider row (non-selectable); isCalEvent marks an event row.
	isCalHeader bool
	calDayLabel string // e.g. "Today · Mon 27" or "Tue 28"
	isCalEvent  bool
	calEvent    CalendarEvent
	// isMuted marks the "Muted Threads" virtual sidebar entry (spec 19).
	// Selectable; selecting it loads ListMutedMessages into the list pane.
	isMuted    bool
	mutedCount int // distinct muted-conversation count for the badge
}

// mutedSentinelID is the virtual folder ID used for the "Muted Threads"
// sidebar entry (spec 19). Prefixed with double-underscores to avoid
// collision with Graph folder IDs, which are base64url strings.
const mutedSentinelID = "__muted__"

// FoldersModel is the sidebar pane. It stores the raw folders + per-id
// expansion state, then computes the displayed tree on demand. The
// cursor is an index into the currently-visible items.
type FoldersModel struct {
	raw      []store.Folder
	saved    []SavedSearch
	expanded map[string]bool // folder ID → is-expanded
	items    []displayedFolder
	cursor   int
	// Calendar sidebar section (spec 12).
	calendarEvents  []CalendarEvent
	sidebarShowDays int
	calendarTZ      *time.Location
	// mutedConvCount is the count of distinct muted conversations (spec 19).
	// When > 0, the "Muted Threads" virtual entry is shown in the sidebar.
	mutedConvCount int
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

// SetMutedCount updates the muted-conversation count and rebuilds the
// sidebar. When count > 0 the "Muted Threads" virtual entry appears;
// when 0 it is hidden. Called after every mute/unmute operation.
func (m *FoldersModel) SetMutedCount(count int) {
	m.mutedConvCount = count
	m.rebuild()
}

// SetCalendarEvents replaces the sidebar calendar section (spec 12).
func (m *FoldersModel) SetCalendarEvents(events []CalendarEvent, showDays int, tz *time.Location) {
	m.calendarEvents = append([]CalendarEvent(nil), events...)
	m.sidebarShowDays = showDays
	m.calendarTZ = tz
	m.rebuild()
}

// SelectedCalendarEvent returns the currently-highlighted calendar event,
// or nil when the cursor is not on a calendar event row.
func (m FoldersModel) SelectedCalendarEvent() (*CalendarEvent, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil, false
	}
	it := m.items[m.cursor]
	if !it.isCalEvent {
		return nil, false
	}
	e := it.calEvent
	return &e, true
}

// rebuild recomputes m.items from m.raw + m.expanded + m.saved + calendar events.
func (m *FoldersModel) rebuild() {
	items := flattenFolderTree(m.raw, m.expanded)
	if len(m.saved) > 0 {
		items = append(items, displayedFolder{isSavedHeader: true})
		for _, s := range m.saved {
			items = append(items, displayedFolder{
				isSaved:      true,
				savedID:      s.ID,
				savedName:    s.Name,
				savedPattern: s.Pattern,
				savedPinned:  s.Pinned,
				savedCount:   s.Count,
			})
		}
	}
	// Muted Threads virtual entry — spec 19 §5.4.
	if m.mutedConvCount > 0 {
		items = append(items, displayedFolder{
			isMuted:    true,
			mutedCount: m.mutedConvCount,
		})
	}
	// Calendar section — spec 12 §6 sidebar pane.
	if len(m.calendarEvents) > 0 {
		tz := m.calendarTZ
		if tz == nil {
			tz = time.Local
		}
		days := m.sidebarShowDays
		if days < 1 {
			days = 1
		}
		now := time.Now().In(tz)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)
		for d := 0; d < days; d++ {
			day := today.AddDate(0, 0, d)
			dayEnd := day.AddDate(0, 0, 1)
			var dayEvents []CalendarEvent
			for _, e := range m.calendarEvents {
				t := e.Start.In(tz)
				if !t.Before(day) && t.Before(dayEnd) {
					dayEvents = append(dayEvents, e)
				}
			}
			var label string
			switch d {
			case 0:
				label = "Today · " + day.Format("Mon 2")
			case 1:
				label = "Tomorrow · " + day.Format("Mon 2")
			default:
				label = day.Format("Mon Jan 2")
			}
			items = append(items, displayedFolder{isCalHeader: true, calDayLabel: label})
			if len(dayEvents) == 0 {
				items = append(items, displayedFolder{isCalHeader: true, calDayLabel: "  (no events)"})
			} else {
				for _, e := range dayEvents {
					items = append(items, displayedFolder{isCalEvent: true, calEvent: e})
				}
			}
		}
	}
	m.items = items
}

// ToggleExpand flips the expansion state of the folder under the
// cursor. Returns true if the folder had children and the state
// flipped; false on no-op (cursor on a leaf folder, saved-search
// row, or out of bounds). The caller paints a status hint on
// false so the keypress isn't visually silent — real-tenant
// regression v0.15.0 where users pressed `o` on top-level
// folders that had children on the server but only top-level
// folders had been synced locally (Graph /me/mailFolders is
// non-recursive).
func (m *FoldersModel) ToggleExpand() bool {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return false
	}
	cur := m.items[m.cursor]
	if !cur.hasKids {
		return false
	}
	id := cur.f.ID
	m.expanded[id] = !m.expanded[id]
	m.rebuild()
	for i, it := range m.items {
		if it.f.ID == id {
			m.cursor = i
			break
		}
	}
	return true
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

// isSelectableRow reports whether a sidebar row can receive cursor focus.
func isSelectableRow(it displayedFolder) bool {
	return !it.isSavedHeader && !it.isCalHeader
}

// Up moves the cursor toward the top, skipping non-selectable rows.
func (m *FoldersModel) Up() {
	for i := m.cursor - 1; i >= 0; i-- {
		if isSelectableRow(m.items[i]) {
			m.cursor = i
			return
		}
	}
}

// Down moves the cursor toward the bottom, skipping non-selectable rows.
func (m *FoldersModel) Down() {
	for i := m.cursor + 1; i < len(m.items); i++ {
		if isSelectableRow(m.items[i]) {
			m.cursor = i
			return
		}
	}
}

// PageUp / PageDown jump the cursor by foldersPageStep rows
// (skipping non-selectable rows). Home / End jump to the first /
// last selectable folder.
const foldersPageStep = 10

func (m *FoldersModel) PageUp() {
	for n := 0; n < foldersPageStep; n++ {
		prev := m.cursor
		m.Up()
		if m.cursor == prev {
			return
		}
	}
}

func (m *FoldersModel) PageDown() {
	for n := 0; n < foldersPageStep; n++ {
		prev := m.cursor
		m.Down()
		if m.cursor == prev {
			return
		}
	}
}

func (m *FoldersModel) JumpTop() {
	for i := 0; i < len(m.items); i++ {
		if isSelectableRow(m.items[i]) {
			m.cursor = i
			return
		}
	}
}

func (m *FoldersModel) JumpBottom() {
	for i := len(m.items) - 1; i >= 0; i-- {
		if isSelectableRow(m.items[i]) {
			m.cursor = i
			return
		}
	}
}

// Selected returns the highlighted folder, if any. Returns ok=false
// when the cursor is on a saved-search row, a section header, a
// calendar event row, or the virtual muted-threads entry.
func (m FoldersModel) Selected() (store.Folder, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return store.Folder{}, false
	}
	it := m.items[m.cursor]
	if it.isSaved || it.isSavedHeader || it.isCalHeader || it.isCalEvent || it.isMuted {
		return store.Folder{}, false
	}
	return it.f, true
}

// SelectedMuted returns true when the cursor is on the virtual
// "Muted Threads" entry (spec 19 §5.4). The caller dispatches
// loadMutedMessagesCmd when this returns true.
func (m FoldersModel) SelectedMuted() bool {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return false
	}
	return m.items[m.cursor].isMuted
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
	return SavedSearch{ID: it.savedID, Name: it.savedName, Pattern: it.savedPattern, Pinned: it.savedPinned, Count: it.savedCount}, true
}

// FindByName returns the folder whose DisplayName or WellKnownName
// matches `name` case-insensitively. Used by `:folder <name>` to
// jump the list pane without requiring the user to know the
// tenant-specific folder ID. Sidebar order is preserved on
// duplicates: the first match wins.
func (m FoldersModel) FindByName(name string) (store.Folder, bool) {
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return store.Folder{}, false
	}
	for _, f := range m.raw {
		if strings.ToLower(f.DisplayName) == target ||
			strings.ToLower(f.WellKnownName) == target {
			return f, true
		}
	}
	return store.Folder{}, false
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
		// Muted Threads virtual folder (spec 19 §5.4).
		if it.isMuted {
			marker := "  "
			if i == m.cursor && focused {
				marker = "▶ "
			} else if i == m.cursor {
				marker = "· "
			}
			line := fmt.Sprintf("%s🔕 Muted  %d", marker, it.mutedCount)
			styled := truncate(line, width-1)
			if i == m.cursor && focused {
				styled = t.FoldersSel.Render(styled)
			}
			rows = append(rows, styled)
			continue
		}
		// Saved-searches section header — non-selectable divider.
		if it.isSavedHeader {
			rows = append(rows, t.Dim.Render("  Saved Searches"))
			continue
		}
		// Calendar day header — non-selectable divider.
		if it.isCalHeader {
			rows = append(rows, t.Dim.Render("  "+it.calDayLabel))
			continue
		}
		// Calendar event row.
		if it.isCalEvent {
			tz := m.calendarTZ
			if tz == nil {
				tz = time.Local
			}
			marker := "  "
			if i == m.cursor && focused {
				marker = "▶ "
			} else if i == m.cursor {
				marker = "· "
			}
			timeStr := it.calEvent.Start.In(tz).Format("15:04")
			if it.calEvent.IsAllDay {
				timeStr = "all day"
			}
			calLine := marker + timeStr + " " + it.calEvent.Subject
			styled := truncate(calLine, width-1)
			if i == m.cursor && focused {
				styled = t.FoldersSel.Render(styled)
			}
			rows = append(rows, styled)
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
			if it.savedCount >= 0 {
				line = fmt.Sprintf("%s  %d", line, it.savedCount)
			}
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
	// graphExhausted is sticky: set when a backfill round-trip
	// returned zero new messages. Tells the UI "this folder has no
	// more older mail on Graph, stop kicking Backfill". Cleared on
	// folder switch (ResetLimit). Without this flag, a user at the
	// genuine end of a mailbox kept hitting j and re-firing
	// no-op backfills (real-tenant regression v0.14.x).
	graphExhausted bool
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

// OldestReceivedAt returns the received_at of the last (oldest)
// message in the loaded slice. Used by the cache-wall flow to
// compute the upper bound for engine.Backfill — Graph returns
// messages older than this. Returns the zero Time when the list is
// empty, in which case Backfill falls back to its default window.
func (m ListModel) OldestReceivedAt() time.Time {
	if len(m.messages) == 0 {
		return time.Time{}
	}
	return m.messages[len(m.messages)-1].ReceivedAt
}

// AtCacheWall returns true when the cursor sits at the last row of a
// list that's exhausted the local store. Caller can use this to kick
// a sync so the engine pulls more from Graph.
func (m ListModel) AtCacheWall() bool {
	return m.cacheExhausted && len(m.messages) > 0 && m.cursor == len(m.messages)-1
}

// ShouldKickWallSync returns true when the cursor is at the cache
// wall AND we haven't already requested a sync for this state AND
// Graph hasn't told us the mailbox is truly exhausted. The caller
// flips wallSyncRequested via [MarkWallSyncRequested] after firing
// the Cmd, so subsequent j-presses don't re-fire until the next
// SetMessages.
func (m ListModel) ShouldKickWallSync() bool {
	return m.AtCacheWall() && !m.wallSyncRequested && !m.graphExhausted
}

// MarkGraphExhausted sets the sticky flag that stops further
// Backfill kicks. Caller invokes this when a FolderSyncedEvent
// arrives with Added=0 for the folder the list is on — Graph has
// confirmed there's no more older mail. Cleared on folder switch
// via ResetLimit.
func (m *ListModel) MarkGraphExhausted() { m.graphExhausted = true }

// GraphExhausted reports whether the user has hit the true end of
// the mailbox per Graph. Used by the status bar to paint a hint.
func (m ListModel) GraphExhausted() bool { return m.graphExhausted }

// MarkWallSyncRequested arms the wall-sync debounce flag.
func (m *ListModel) MarkWallSyncRequested() { m.wallSyncRequested = true }

// ClearWallSyncRequested releases the debounce flag. Called by the
// backfillDoneMsg handler on error so the user can retry by pressing
// j again instead of being permanently stuck after a transient
// network failure.
func (m *ListModel) ClearWallSyncRequested() { m.wallSyncRequested = false }

// ResetLimit collapses the load limit back to the initial page and
// clears the exhausted flags (used when the user switches folders —
// the new folder's cache state is unknown).
func (m *ListModel) ResetLimit() {
	m.loadLimit = initialListLimit
	m.loading = false
	m.graphExhausted = false
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

// listPageStep is the cursor jump distance for PgUp / PgDn in the
// list pane. ~20 rows is one screen on a typical 30-row terminal
// minus chrome — a meaningful skip without overshooting the user's
// mental position.
const listPageStep = 20

// PageDown jumps the cursor `listPageStep` rows toward the bottom,
// clamped at the last message. Used by PgDn / Ctrl+D in the list
// pane. Pre-fetch + wall-sync flow the same as a single Down() —
// the dispatch in dispatchList re-checks ShouldLoadMore /
// ShouldKickWallSync after the jump.
func (m *ListModel) PageDown() {
	target := m.cursor + listPageStep
	if target >= len(m.messages) {
		target = len(m.messages) - 1
	}
	if target < 0 {
		target = 0
	}
	m.cursor = target
}

// PageUp jumps the cursor `listPageStep` rows toward the top,
// clamped at row 0.
func (m *ListModel) PageUp() {
	target := m.cursor - listPageStep
	if target < 0 {
		target = 0
	}
	m.cursor = target
}

// JumpTop / JumpBottom move the cursor to the first / last
// message. Used by Home / End / g / G.
func (m *ListModel) JumpTop() { m.cursor = 0 }
func (m *ListModel) JumpBottom() {
	if len(m.messages) > 0 {
		m.cursor = len(m.messages) - 1
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
		// Meeting-invite / mute indicator. Calendar takes priority:
		// if the message is a meeting invite, show 📅 regardless of mute
		// state. Otherwise, if we're in the muted virtual view, show the
		// mute glyph. Normal folder views exclude muted messages
		// (ExcludeMuted: true) so 🔕 only ever appears in __muted__ view.
		// Spec 19 §5.2.
		invite := "  "
		if isMeetingMessage(msg) {
			invite = "📅 "
		} else if m.FolderID == mutedSentinelID {
			mi := t.MuteIndicator
			if mi == "" {
				mi = "🔕"
			}
			invite = mi + " "
		}
		flag := "  "
		if msg.FlagStatus == "flagged" {
			fi := t.FlagIndicator
			if fi == "" {
				fi = "⚑"
			}
			flag = fi + " "
		}
		attach := ""
		if msg.HasAttachments {
			ai := t.AttachmentIndicator
			if ai == "" {
				ai = "📎"
			}
			attach = " " + ai
		}
		line := fmt.Sprintf("%s%s%s%-10s %-14s %s%s", marker, flag, invite, when, truncate(from, 14), msg.Subject, attach)
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
	// links is the numbered URL table the renderer extracted.
	// Spec 05 §10 + the v0.15.x URL-picker work key off this.
	links []BodyLink
	// attachments is the metadata-only attachment list for the
	// current message (spec 05 §8). Populated by the body-fetch
	// path on each open. The viewer renders an "Attachments:" block
	// between headers and body. Save / open keybindings wired in PR 10.
	attachments []store.Attachment
	// conversationThread is the ordered list of sibling messages in
	// the same conversation (spec 05 §11). Sorted ReceivedAt ASC.
	// convIdx points at the currently-displayed message. Both survive
	// SetMessage so [/] navigation works without a new store query.
	conversationThread []store.Message
	convIdx            int
	// bodyExpanded is the fully un-collapsed body (quotes not folded).
	// body holds the collapsed version when quotesExpanded is false.
	bodyExpanded   string
	quotesExpanded bool
	// rawHeaders carries the RFC 822 headers from the last body fetch.
	rawHeaders []RawHeader
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
	m.attachments = nil
}

// SetAttachments records the attachment metadata loaded for the
// current message. The viewer renders an "Attachments:" block
// between headers and body when this is non-empty.
func (m *ViewerModel) SetAttachments(atts []store.Attachment) {
	m.attachments = atts
}

// Attachments returns the metadata for the current message. Tests
// + future save/open keybindings consume this.
func (m ViewerModel) Attachments() []store.Attachment {
	return m.attachments
}

// SetBody is invoked after a fetch completes (or the cache hits).
// collapsed is the displayed version (with quotes folded when threshold > 0);
// expanded is the fully un-collapsed body. When collapsed == expanded the
// toggle is a no-op. state mirrors render.BodyState.
func (m *ViewerModel) SetBody(collapsed, expanded string, state int) {
	m.body = collapsed
	m.bodyExpanded = expanded
	m.bodyState = state
	m.quotesExpanded = false
}

// ToggleQuotes swaps between the collapsed and expanded body views.
func (m *ViewerModel) ToggleQuotes() {
	if m.quotesExpanded {
		m.body, m.bodyExpanded = m.bodyExpanded, m.body
		m.quotesExpanded = false
	} else {
		m.body, m.bodyExpanded = m.bodyExpanded, m.body
		m.quotesExpanded = true
	}
}

// QuotesExpanded reports whether the body is currently in expanded (uncollapsed) state.
func (m ViewerModel) QuotesExpanded() bool {
	return m.quotesExpanded
}

// SetLinks records the renderer's extracted URL table. The URL
// picker overlay reads from here.
func (m *ViewerModel) SetLinks(links []BodyLink) {
	m.links = links
}

// SetRawHeaders stores the RFC 822 headers for the current message.
func (m *ViewerModel) SetRawHeaders(hdrs []RawHeader) {
	m.rawHeaders = hdrs
}

// RawHeaders returns the stored RFC 822 headers.
func (m ViewerModel) RawHeaders() []RawHeader {
	return m.rawHeaders
}

// Links returns the most recent extracted URL table (may be nil).
func (m ViewerModel) Links() []BodyLink {
	return m.links
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

// viewerPageStep is the body-scroll jump distance for PgUp / PgDn
// in the viewer pane. Half a screen is the mutt / less convention —
// keeps a few lines of context at the new cursor position.
const viewerPageStep = 10

// PageDown / PageUp jump the body viewport by viewerPageStep lines.
// PageUp clamps at 0; PageDown lets the existing render-clip logic
// in View() handle past-EOF gracefully (drawn as empty rows, no
// crash).
func (m *ViewerModel) PageDown() { m.scrollY += viewerPageStep }
func (m *ViewerModel) PageUp() {
	m.scrollY -= viewerPageStep
	if m.scrollY < 0 {
		m.scrollY = 0
	}
}

// JumpTop / JumpBottom reset / advance the body viewport.
// JumpBottom counts newlines in the current body; View() further
// clamps so we always paint a non-empty trailing slice (otherwise
// the user sees a blank pane and thinks the binding is broken).
func (m *ViewerModel) JumpTop() { m.scrollY = 0 }
func (m *ViewerModel) JumpBottom() {
	m.scrollY = strings.Count(m.body, "\n")
	if m.scrollY < 0 {
		m.scrollY = 0
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

// SetConversationThread replaces the conversation cache and updates convIdx
// to point at currentID. convIdx defaults to 0 when currentID is not found.
func (m *ViewerModel) SetConversationThread(msgs []store.Message, currentID string) {
	m.conversationThread = msgs
	m.convIdx = 0
	for i, msg := range msgs {
		if msg.ID == currentID {
			m.convIdx = i
			return
		}
	}
}

// ConversationThread returns the current thread cache (may be nil).
func (m ViewerModel) ConversationThread() []store.Message {
	return m.conversationThread
}

// NavPrevInThread moves to the chronologically older sibling in the
// conversation and returns it. Returns nil when already at the first.
func (m *ViewerModel) NavPrevInThread() *store.Message {
	if m.convIdx <= 0 || len(m.conversationThread) == 0 {
		return nil
	}
	m.convIdx--
	return &m.conversationThread[m.convIdx]
}

// NavNextInThread moves to the chronologically newer sibling in the
// conversation and returns it. Returns nil when already at the last.
func (m *ViewerModel) NavNextInThread() *store.Message {
	if m.convIdx >= len(m.conversationThread)-1 || len(m.conversationThread) == 0 {
		return nil
	}
	m.convIdx++
	return &m.conversationThread[m.convIdx]
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
		// Spec §4: extra structured fields shown in full-header mode.
		if m.current.Importance != "" {
			hdrs = append(hdrs, "Importance: "+m.current.Importance)
		}
		if len(m.current.Categories) > 0 {
			hdrs = append(hdrs, "Categories: "+strings.Join(m.current.Categories, ", "))
		}
		if s := m.current.FlagStatus; s != "" && s != "notFlagged" {
			hdrs = append(hdrs, "Flag:    "+s)
		}
		if m.current.HasAttachments {
			hdrs = append(hdrs, "Has-Attachments: yes")
		}
		if m.current.InternetMessageID != "" {
			hdrs = append(hdrs, "Message-ID: "+m.current.InternetMessageID)
		}
		// Raw RFC 822 headers from Graph's internetMessageHeaders field.
		for _, h := range m.rawHeaders {
			hdrs = append(hdrs, h.Name+": "+h.Value)
		}
	} else {
		hdrs = append(hdrs, "To:      "+compactAddrs(m.current.ToAddresses, m.current.CcAddresses, m.current.BccAddresses))
	}
	hdrs = append(hdrs, "")
	// Attachments block sits between headers and body. mutt and
	// alpine both surface attachments above the body so the reader
	// sees what's attached before scrolling. Real-tenant complaint
	// 2026-05-01: previously the only signal was the list-pane
	// `📎` glyph; the user couldn't see filenames at all.
	attLines := renderAttachmentLines(m.attachments, t.RenderTheme)
	hdrs = append(hdrs, attLines...)
	body := m.body
	if body == "" {
		body = t.Dim.Render("(loading…)")
	}
	// Append conversation thread map below the body so the user can
	// scroll down to navigate the thread context (spec 05 §11).
	if thread := renderConversationSection(m.conversationThread, m.convIdx); thread != "" {
		body = body + "\n\n" + thread
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

// renderAttachmentLines produces the compact "Attachments:" block
// shown above the body (spec 05 §8). One line per attachment with an
// accelerator letter prefix `[a]`, `[b]`, … so the user can press
// that letter to save, or Shift+letter to open (spec 05 §12 / PR 10).
func renderAttachmentLines(atts []store.Attachment, renderTheme render.Theme) []string {
	if len(atts) == 0 {
		return nil
	}
	out := make([]string, 0, len(atts)+2)
	out = append(out, renderTheme.Attachment.Render("Attach:  "+attachmentSummary(atts)))
	for i, a := range atts {
		letter := "?"
		if i < 26 {
			letter = string(rune('a' + i))
		}
		out = append(out, renderTheme.Attachment.Render("  ["+letter+"] "+attachmentLine(a)))
	}
	out = append(out, "")
	return out
}

// attachmentSummary renders a one-line header summary
// "3 files · 2.4 MB" so the user sees the count + total weight at
// a glance even if the per-file lines scroll off-screen.
func attachmentSummary(atts []store.Attachment) string {
	var total int64
	for _, a := range atts {
		total += a.Size
	}
	noun := "files"
	if len(atts) == 1 {
		noun = "file"
	}
	return fmt.Sprintf("%d %s · %s", len(atts), noun, humanByteSize(total))
}

// attachmentLine renders one attachment's name + size + content-type.
// Inline attachments are flagged so users understand they're embedded
// images rather than user-attached files.
func attachmentLine(a store.Attachment) string {
	suffix := ""
	if a.IsInline {
		suffix = " (inline)"
	}
	if a.ContentType != "" {
		return fmt.Sprintf("%s · %s · %s%s", a.Name, humanByteSize(a.Size), a.ContentType, suffix)
	}
	return fmt.Sprintf("%s · %s%s", a.Name, humanByteSize(a.Size), suffix)
}

// humanByteSize is a panes-local copy of render.humanBytes so the
// viewer doesn't import internal/render. Same conversion (KB == 1024
// bytes); kept in sync via a tiny test.
func humanByteSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
}

// renderConversationSection builds the "Thread (N messages)" block
// that appears at the bottom of the scrollable body (spec 05 §11).
// curIdx marks the currently-displayed message with a `*` glyph. The
// block is omitted when the thread has 0 or 1 entries (no context to
// show) or when msgs is nil (message has no ConversationID).
func renderConversationSection(msgs []store.Message, curIdx int) string {
	if len(msgs) <= 1 {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("──── Thread (%d messages) ────\n", len(msgs)))
	for i, m := range msgs {
		mark := "  "
		if i == curIdx {
			mark = "▶ "
		}
		date := m.ReceivedAt.Format("Jan 02 15:04")
		from := m.FromName
		if from == "" {
			from = m.FromAddress
		}
		if len(from) > 16 {
			from = from[:15] + "…"
		}
		subj := m.Subject
		if subj == "" {
			subj = "(no subject)"
		}
		if len(subj) > 38 {
			subj = subj[:37] + "…"
		}
		b.WriteString(fmt.Sprintf("  %s%s  %-16s  %s\n", mark, date, from, subj))
	}
	b.WriteString("  [ ← prev  ] → next\n")
	return b.String()
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
	LastSync     time.Time
	Throttled    time.Duration
	Activity     string // "syncing folders…" / "syncing…" / "" (idle)
	LastErr      error  // most recent SyncFailedEvent, if any
	OOOActive    bool
	OOOIndicator string // configurable glyph, default "🌴"
}

// View renders the status line.
func (m StatusModel) View(t Theme, width int, in StatusInputs) string {
	left := "☰ inkwell"
	if m.upn != "" {
		left += " · " + m.upn
	}
	if in.OOOActive {
		indicator := in.OOOIndicator
		if indicator == "" {
			indicator = "🌴"
		}
		left += " · " + indicator + " OOO"
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
// known invite/response forms. Heuristic — used as a fallback for
// messages that predate the meeting_message_type schema migration
// (spec 02 v2). New messages get the canonical signal via Graph's
// $select=meetingMessageType.
func isLikelyMeeting(subject string) bool {
	s := strings.ToLower(strings.TrimSpace(subject))
	for _, p := range meetingPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// isMeetingMessage decides whether to render the 📅 indicator.
// Three signals, in priority order:
//
//  1. Canonical Graph signal (`MeetingMessageType` non-empty). Today
//     this column is empty for every row because the field was
//     dropped from $select after a real-tenant 400 (see
//     `internal/graph/types.go::EnvelopeSelectFields`); kept here
//     so a future tenant-cast-form $select revives it without
//     code churn.
//  2. Subject prefix heuristic — catches meeting RESPONSES /
//     CANCELLATIONS (`Accepted:`, `Declined:`, `Canceled:`, etc.).
//     Misses meeting REQUESTS whose subject is just the meeting
//     title with no prefix.
//  3. BodyPreview shape — catches the meeting REQUEST case the
//     subject heuristic misses. Outlook auto-generates a
//     structured preview ("When: <date>\nWhere: <location>") on
//     every server-side invite. The When+Where pair within the
//     first ~200 chars is a high-precision signal.
//
// Limitation: English-locale only. Non-English Outlook deployments
// emit the same preview with localised labels ("Cuándo:" /
// "Quand :" / etc.); those fall through and miss the indicator.
// Same constraint as the existing subject heuristic. Locale-aware
// detection lifts with the type-cast $select in a future release.
func isMeetingMessage(msg store.Message) bool {
	switch strings.ToLower(strings.TrimSpace(msg.MeetingMessageType)) {
	case "":
		// No canonical signal — fall through to heuristics.
	case "none":
		// Graph explicitly says "not a meeting"; trust it over the
		// heuristics (which would otherwise false-positive on
		// any "Meeting: Q4 sync" plain mail).
		return false
	default:
		return true
	}
	if isLikelyMeeting(msg.Subject) {
		return true
	}
	return hasInviteBodyPreview(msg.BodyPreview)
}

// hasInviteBodyPreview detects Outlook's auto-generated meeting-
// invite body preview shape. Real invites always include both
// "When:" and "Where:" labels in close proximity within the
// preview block. Checking for both (not either) keeps false
// positives down — a regular email might mention "When I get
// back" or "Where do you want to meet" but rarely both as
// labelled headers.
//
// We scan the first ~200 chars only (the Outlook preview header
// block is short). Limiting the scan window keeps the heuristic
// from false-positiving on long emails that happen to contain
// both words elsewhere in the body.
func hasInviteBodyPreview(preview string) bool {
	if preview == "" {
		return false
	}
	head := preview
	if len(head) > 200 {
		head = head[:200]
	}
	lower := strings.ToLower(head)
	return strings.Contains(lower, "when:") && strings.Contains(lower, "where:")
}

// truncate cuts s to fit `width` terminal cells. Width is measured
// in cells, NOT runes — emoji (📅 = 2 cells) and CJK characters
// (e.g. 李 = 2 cells) make rune count and cell count diverge. The
// previous rune-slice implementation overshot for those inputs:
// taking N runes of an emoji-prefixed line consumed N+1 cells, the
// list pane's lipgloss Width(W) didn't clip back, and the right
// edge characters spilled off the right edge until the user
// resized the terminal (real-tenant Ghostty regression).
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if used+w > width {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
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
