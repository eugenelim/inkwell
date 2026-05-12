package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eugenelim/inkwell/internal/store"
)

// inboxSubTabLoadedMsg arrives when a sub-tab's message list has been
// re-fetched from the store via Store.ListMessagesByInferenceClass.
type inboxSubTabLoadedMsg struct {
	segment  int
	folderID string
	messages []store.Message
	err      error
}

// inboxSubTabUnreadMsg arrives when both sub-tab badges have refreshed
// (sequential calls inside one Cmd, spec 31 §6.1).
type inboxSubTabUnreadMsg struct {
	folderID   string
	focused    int
	other      int
	focusedOK  bool
	otherOK    bool
	focusedErr error
	otherErr   error
}

// inboxFolderID returns the well-known Inbox folder ID for the current
// account, or "" when the folder list is not yet loaded.
func (m Model) inboxFolderID() string {
	if f, ok := m.folders.FindByName("inbox"); ok {
		return f.ID
	}
	return ""
}

// inboxSubStripShouldRender reports whether the sub-strip should paint
// this frame per spec 31 §5.2's five preconditions.
func (m Model) inboxSubStripShouldRender() bool {
	if m.inboxSplit != InboxSplitFocusedOther {
		return false
	}
	inboxID := m.inboxFolderID()
	if inboxID == "" {
		return false
	}
	if m.list.FolderID != inboxID {
		return false
	}
	// A spec-24 tab is the active surface — sub-strip hides.
	if m.activeTab >= 0 {
		return false
	}
	if m.searchActive {
		return false
	}
	if m.filterAllFolders {
		return false
	}
	return true
}

// renderInboxSubStrip paints the spec 31 §5.3 sub-strip. Returns "" when
// the strip should not render (precondition failure). One row, two
// segments, identical Lipgloss styling vocabulary to spec 24's tab strip.
func (m Model) renderInboxSubStrip(t Theme, width int) string {
	if !m.inboxSubStripShouldRender() {
		return ""
	}
	names := [2]string{"Focused", "Other"}
	segments := make([]string, 0, 2)
	showZero := m.deps.InboxSplitShowZeroCount
	for i := 0; i < 2; i++ {
		name := names[i]
		count := m.inboxSubTabUnread[i]
		ok := m.inboxSubTabUnreadOK[i]
		var seg string
		switch {
		case !ok:
			seg = name + " ⚠"
		case count == 0 && !showZero:
			seg = name
		default:
			seg = fmt.Sprintf("%s %d", name, count)
		}
		// New-mail glyph: inactive segment has unread > 0 AND user
		// hasn't focused it since the last refresh tick. Spec 24 §5.5
		// mirror.
		if i != m.activeInboxSubTab && ok && count > 0 {
			lastFocus := m.inboxSubTabLastFocused[i]
			if lastFocus.IsZero() || lastFocus.Before(m.inboxSubTabRefreshAt) {
				seg = "•" + seg
			}
		}
		open, close := "[", "]"
		styled := open + seg + close
		if i == m.activeInboxSubTab {
			styled = t.HelpKey.Render(styled)
		} else {
			styled = t.Dim.Render(styled)
		}
		segments = append(segments, styled)
	}
	strip := " " + strings.Join(segments, " ") + " "
	if lipgloss.Width(strip) > width {
		// Two short segments will essentially never exceed the list
		// pane width, but truncate defensively rather than overflow.
		strip = truncateStrip(strip, width)
	}
	return strip
}

// inboxSplitDefaultSegmentIndex maps the [inbox].split_default_segment
// config value to a segment index, or -1 when "none" (no `]`/`[`
// activation from the -1 state).
func (m Model) inboxSplitDefaultSegmentIndex() int {
	switch m.inboxSplitDefaultSegment {
	case "other":
		return inboxSubTabOther
	case "none":
		return -1
	default:
		return inboxSubTabFocused
	}
}

// cycleInboxSubTab handles a `]` / `[` press when the inbox sub-strip
// is the active cycle surface. From the -1 cold-start state the first
// press lands on the configured default segment ("focused" / "other" /
// "none"); subsequent presses toggle between the two segments.
//
// Returns (m, nil) when the press is a no-op
// ([inbox].split_default_segment = "none" from -1; sub-strip not
// rendering; etc.).
func (m Model) cycleInboxSubTab(delta int) (Model, tea.Cmd) {
	if !m.inboxSubStripShouldRender() {
		return m, nil
	}
	target := -1
	switch {
	case m.activeInboxSubTab < 0:
		target = m.inboxSplitDefaultSegmentIndex()
		if target < 0 {
			return m, nil
		}
	default:
		// Toggle between the two segments regardless of delta sign:
		// the two-segment strip wraps trivially (spec 31 §5.5).
		_ = delta
		if m.activeInboxSubTab == inboxSubTabFocused {
			target = inboxSubTabOther
		} else {
			target = inboxSubTabFocused
		}
	}
	return m.activateInboxSubTabIndex(target)
}

// activateInboxSubTabIndex switches to the given segment, snapshotting
// the current state and either restoring a fresh-enough snapshot or
// dispatching a load Cmd. Segment must be 0 or 1.
func (m Model) activateInboxSubTabIndex(segment int) (Model, tea.Cmd) {
	if segment != inboxSubTabFocused && segment != inboxSubTabOther {
		return m, nil
	}
	inboxID := m.inboxFolderID()
	if inboxID == "" {
		return m, nil
	}
	// Snapshot the current sub-tab's list state when leaving an
	// already-active segment. Spec 31 §5.6 / spec 24 §5.6 precedent.
	if m.activeInboxSubTab >= 0 && m.activeInboxSubTab < 2 {
		m.inboxSubTabState[m.activeInboxSubTab] = listSnapshot{
			cursor:     m.list.cursor,
			messages:   m.list.messages,
			capturedAt: time.Now(),
		}
	}
	m.activeInboxSubTab = segment
	m.inboxSubTabLastFocused[segment] = time.Now()

	// Cache hit? Reuse the spec-24 SavedSearchCacheTTL value rather
	// than introduce a new TTL config key (spec 31 §5.6).
	cacheTTL := 60 * time.Second
	if m.deps.SavedSearchCacheTTL > 0 {
		cacheTTL = m.deps.SavedSearchCacheTTL
	}
	snap := m.inboxSubTabState[segment]
	if len(snap.messages) > 0 && time.Since(snap.capturedAt) < cacheTTL {
		m.list.SetMessages(snap.messages)
		if snap.cursor < len(snap.messages) {
			m.list.cursor = snap.cursor
		}
		return m, nil
	}

	m.focused = ListPane
	m.list.ResetLimit()
	return m, m.loadInboxSubTabCmd(inboxID, segment)
}

// loadInboxSubTabCmd fetches the message slice for the given segment
// from Store.ListMessagesByInferenceClass.
func (m Model) loadInboxSubTabCmd(folderID string, segment int) tea.Cmd {
	cls := inboxSubTabClass(segment)
	if cls == "" {
		return nil
	}
	limit := m.list.LoadLimit()
	excludeScreenedOut := m.screenerEnabled
	store := m.deps.Store
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		msgs, err := store.ListMessagesByInferenceClass(ctx, accountID, folderID, cls, limit, true, excludeScreenedOut)
		return inboxSubTabLoadedMsg{segment: segment, folderID: folderID, messages: msgs, err: err}
	}
}

// refreshInboxSubTabUnreadCmd queries both segments' unread counts
// sequentially inside one Cmd (spec 31 §6.1 — two queries, not an
// errgroup fan-out).
func (m Model) refreshInboxSubTabUnreadCmd() tea.Cmd {
	folderID := m.inboxFolderID()
	if folderID == "" || m.deps.Store == nil {
		return nil
	}
	excludeScreenedOut := m.screenerEnabled
	store := m.deps.Store
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		foc, fErr := store.CountUnreadByInferenceClass(ctx, accountID, folderID, "focused", true, excludeScreenedOut)
		oth, oErr := store.CountUnreadByInferenceClass(ctx, accountID, folderID, "other", true, excludeScreenedOut)
		return inboxSubTabUnreadMsg{
			folderID:   folderID,
			focused:    foc,
			other:      oth,
			focusedOK:  fErr == nil,
			otherOK:    oErr == nil,
			focusedErr: fErr,
			otherErr:   oErr,
		}
	}
}

// inboxSubTabClass maps a segment index to the inference_class string
// store helpers expect.
func inboxSubTabClass(segment int) string {
	switch segment {
	case inboxSubTabFocused:
		return store.InferenceClassFocused
	case inboxSubTabOther:
		return store.InferenceClassOther
	}
	return ""
}

// inboxSubTabName returns the user-visible name for a segment index.
func inboxSubTabName(segment int) string {
	switch segment {
	case inboxSubTabFocused:
		return "Focused"
	case inboxSubTabOther:
		return "Other"
	}
	return ""
}

// activateInboxSubTabFromCmdBar handles the `:focused` / `:other`
// cmd-bar verbs per spec 31 §5.8: clear spec-24 tab, clear filter /
// search, navigate to Inbox if needed, activate the segment. When
// [inbox].split == "off", surface the friendly off-state error and
// take no action.
func (m Model) activateInboxSubTabFromCmdBar(segment int) (Model, tea.Cmd) {
	if m.inboxSplit == InboxSplitOff {
		m.lastError = fmt.Errorf("%s: inbox split is off — set [inbox].split = \"focused_other\" first", inboxSubTabVerb(segment))
		return m, nil
	}
	inboxID := m.inboxFolderID()
	if inboxID == "" {
		m.lastError = fmt.Errorf("%s: inbox folder not yet loaded", inboxSubTabVerb(segment))
		return m, nil
	}
	// 1. Clear any active spec-24 tab.
	m.activeTab = -1
	// 2. Clear filter / search state (folders.SelectByID below moves
	//    the sidebar cursor off any saved-search row to the Inbox row).
	m.filterActive = false
	m.filterPattern = ""
	m.filterIDs = nil
	m.filterAllFolders = false
	m.filterFolderCount = 0
	m.filterFolderName = ""
	m.searchActive = false
	m.searchQuery = ""
	// 3. Navigate to Inbox.
	wasNotInbox := m.list.FolderID != inboxID
	m.list.FolderID = inboxID
	m.folders.SelectByID(inboxID)
	// 5. Activate the requested sub-tab.
	mm, cmd := m.activateInboxSubTabIndex(segment)
	if wasNotInbox && cmd == nil {
		// Force a load even if a fresh cache snapshot exists, because
		// the cmd-bar verb's contract is "always show the segment".
		cmd = mm.loadInboxSubTabCmd(inboxID, segment)
	}
	return mm, cmd
}

func inboxSubTabVerb(segment int) string {
	if segment == inboxSubTabOther {
		return "other"
	}
	return "focused"
}
