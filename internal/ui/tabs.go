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

// listSnapshot captures one tab's per-tab state so cycling between
// tabs is instant. Spec 24 §5.6: cursor + scroll + the message slice
// header (sharing the backing array — ListModel.SetMessages replaces
// the slice rather than mutating in place, so this is safe).
type listSnapshot struct {
	cursor       int
	scrollOffset int
	cacheKey     string // "savedsearch:<id>" — used to detect drift
	messages     []store.Message
	capturedAt   time.Time
}

// TabsConfig is the consumer-side mirror of [config.TabsConfig]. The
// UI doesn't import internal/config (`docs/CONVENTIONS.md` §2 layering); cmd_run
// converts the typed config into this shape and threads it through
// Deps.
type TabsConfig struct {
	Enabled       bool
	ShowZeroCount bool
	MaxNameWidth  int
	CycleWraps    bool
}

// tabsLoadedMsg arrives when the SavedSearchService.Tabs reload
// completes (initial load, or after a promote/demote/reorder).
type tabsLoadedMsg struct {
	tabs []SavedSearch
	err  error
}

// tabCountsUpdatedMsg arrives when RefreshTabCounts completes.
type tabCountsUpdatedMsg struct {
	counts map[int64]int
}

// tabSwitchMsg arrives when the user has cycled or jumped to a tab.
// It carries the activated saved search so the dispatch handler can
// load its messages (cache-hit path skips this; cache-miss falls
// through to runFilterCmd).
type tabSwitchMsg struct {
	tab SavedSearch
}

// tabSentinelID returns the synthetic FolderID used by the list
// pane when a tab is active. Distinct from the saved-search-only
// "filter:" prefix so the dispatcher and renderer can recognise the
// tab context.
func tabSentinelID(tabID int64) string {
	return fmt.Sprintf("tab:%d", tabID)
}

// loadTabsCmd queries the SavedSearchService for the current tab list
// (called on app boot and after any tab mutation).
func (m Model) loadTabsCmd() tea.Cmd {
	if m.deps.SavedSearchSvc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		tabs, err := m.deps.SavedSearchSvc.Tabs(ctx)
		return tabsLoadedMsg{tabs: tabs, err: err}
	}
}

// refreshTabCountsCmd queries the SavedSearchService for per-tab
// unread counts (sync-event hook, also used after a tab mutation).
func (m Model) refreshTabCountsCmd() tea.Cmd {
	if m.deps.SavedSearchSvc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		counts, err := m.deps.SavedSearchSvc.RefreshTabCounts(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return tabCountsUpdatedMsg{counts: counts}
	}
}

// activateTab is the workhorse for ] / [ / `:tab <name>` / sidebar-
// click flows. Saves the current tab snapshot, switches, and
// returns a Cmd that loads the new tab's messages (either from
// cache or via runFilterCmd).
func (m Model) activateTab(idx int) (Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.tabs) {
		return m, nil
	}
	// Snapshot current tab state if we were already in a tab.
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		m.tabState[m.activeTab] = listSnapshot{
			cursor:       m.list.cursor,
			scrollOffset: 0,
			cacheKey:     fmt.Sprintf("savedsearch:%d", m.tabs[m.activeTab].ID),
			messages:     m.list.messages,
			capturedAt:   time.Now(),
		}
	}
	m.activeTab = idx
	m.tabLastFocused[m.tabs[idx].ID] = time.Now()
	// Clear any active filter — the tab pattern itself is the new view.
	m.filterActive = false
	m.filterPattern = ""
	m.filterIDs = nil
	m.filterAllFolders = false
	m.filterFolderCount = 0
	m.filterFolderName = ""
	m.list.folderNameByID = nil
	m.searchActive = false

	tab := m.tabs[idx]
	m.list.FolderID = tabSentinelID(tab.ID)

	// Cache hit?
	cacheTTL := 60 * time.Second
	if m.deps.SavedSearchCacheTTL > 0 {
		cacheTTL = m.deps.SavedSearchCacheTTL
	}
	if idx < len(m.tabState) {
		snap := m.tabState[idx]
		if len(snap.messages) > 0 && time.Since(snap.capturedAt) < cacheTTL {
			m.list.SetMessages(snap.messages)
			if snap.cursor < len(snap.messages) {
				m.list.cursor = snap.cursor
			}
			return m, nil
		}
	}

	m.focused = ListPane
	m.list.ResetLimit()
	return m, m.runFilterCmd(tab.Pattern)
}

// cycleTab advances activeTab by ±1, with config-driven wrap. From
// `activeTab == -1` (cold start), both ] and [ activate tab 0
// (spec 24 §7 cold-start row).
func (m Model) cycleTab(delta int) (Model, tea.Cmd) {
	n := len(m.tabs)
	if n == 0 {
		return m, nil
	}
	if m.activeTab == -1 {
		return m.activateTab(0)
	}
	next := m.activeTab + delta
	if !m.tabsCfg.CycleWraps {
		if next < 0 || next >= n {
			return m, nil
		}
	} else {
		next = ((next % n) + n) % n
	}
	return m.activateTab(next)
}

// invalidateTabSnapshot drops the snapshot for tabs that reference
// the supplied saved-search ID. Called when an Edit re-saves the
// pattern (spec 24 §5.6).
func (m *Model) invalidateTabSnapshot(savedSearchID int64) {
	for i, t := range m.tabs {
		if t.ID == savedSearchID && i < len(m.tabState) {
			m.tabState[i] = listSnapshot{}
		}
	}
}

// applyTabsLoaded reconciles a fresh tab list with the model state.
// Preserves activeTab when the same saved-search ID is still
// present; clamps to len-1 if shorter; falls back to -1 if empty.
func (m Model) applyTabsLoaded(newTabs []SavedSearch) Model {
	prevActiveID := int64(-1)
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		prevActiveID = m.tabs[m.activeTab].ID
	}
	m.tabs = newTabs
	m.tabState = make([]listSnapshot, len(newTabs))
	if m.tabUnread == nil {
		m.tabUnread = make(map[int64]int)
	}
	if m.tabLastFocused == nil {
		m.tabLastFocused = make(map[int64]time.Time)
	}
	if prevActiveID < 0 {
		m.activeTab = -1
		return m
	}
	for i, t := range newTabs {
		if t.ID == prevActiveID {
			m.activeTab = i
			return m
		}
	}
	// Previous active tab is no longer in the strip — fall back to
	// "no active" rather than picking some arbitrary survivor (spec
	// 24 §7: "active id no longer present → -1"). Empty strip also
	// lands here.
	m.activeTab = -1
	return m
}

// renderTabStrip paints the spec 24 §5.1 tab strip. Returns "" when
// no strip should render (no tabs configured, or tabs.enabled =
// false). Width is the list-pane content width.
func (m Model) renderTabStrip(t Theme, width int) string {
	if !m.tabsCfg.Enabled || len(m.tabs) == 0 {
		return ""
	}
	maxName := m.tabsCfg.MaxNameWidth
	if maxName < 4 {
		maxName = 4
	}
	segments := make([]string, 0, len(m.tabs))
	for i, tab := range m.tabs {
		name := truncateName(tab.Name, maxName)
		count, hasCount := m.tabUnread[tab.ID]
		var seg string
		switch {
		case hasCount && count < 0:
			// Sentinel for compile error; render warning glyph.
			seg = name + " ⚠"
		case hasCount && count == 0 && !m.tabsCfg.ShowZeroCount:
			seg = name
		case hasCount:
			seg = fmt.Sprintf("%s %d", name, count)
		default:
			seg = name
		}
		// New-mail glyph: tab has unread > 0 AND user hasn't focused
		// it since the last bump. Skipped on the active tab itself
		// (its lastFocusedAt was just set).
		if i != m.activeTab && hasCount && count > 0 {
			lastFocus := m.tabLastFocused[tab.ID]
			if lastFocus.IsZero() || lastFocus.Before(m.lastTabRefreshAt) {
				seg = "•" + seg
			}
		}
		open, close := "[", "]"
		styled := open + seg + close
		if i == m.activeTab {
			styled = t.HelpKey.Render(styled)
		} else {
			styled = t.Dim.Render(styled)
		}
		segments = append(segments, styled)
	}
	strip := " " + strings.Join(segments, " ") + " "
	// Horizontal-scroll truncation when the strip exceeds width.
	if lipgloss.Width(strip) > width {
		strip = truncateStrip(strip, width)
	}
	return strip
}

// truncateName clips name to max chars, appending "…" if clipped.
func truncateName(name string, max int) string {
	if max <= 0 {
		return name
	}
	if len([]rune(name)) <= max {
		return name
	}
	rs := []rune(name)
	if max <= 1 {
		return "…"
	}
	return string(rs[:max-1]) + "…"
}

// truncateStrip clips s to width cells, marking overflow with `›`.
// Only the trailing-overflow case is implemented in v1; spec §5.1
// also allows leading overflow `‹` when scroll position is mid-strip,
// but this v1 always renders from the start (the strip is rebuilt
// each frame so progressive scroll is a follow-up).
func truncateStrip(s string, width int) string {
	if width <= 1 {
		return strings.Repeat(".", width)
	}
	rs := []rune(s)
	for i := len(rs); i >= 0; i-- {
		cand := string(rs[:i]) + "›"
		if lipgloss.Width(cand) <= width {
			return cand
		}
	}
	return "›"
}

// dispatchTabCmd routes `:tab` cmd-bar input. Spec 24 §6.
func (m Model) dispatchTabCmd(args []string) (tea.Model, tea.Cmd) {
	if m.deps.SavedSearchSvc == nil {
		m.lastError = fmt.Errorf("tab: not wired (CLI mode or unsigned)")
		return m, nil
	}
	if len(args) == 0 {
		return m.tabListSummary()
	}
	switch args[0] {
	case "list":
		return m.tabListSummary()
	case "add":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("tab add: usage :tab add <name>")
			return m, nil
		}
		name := strings.Join(args[1:], " ")
		return m, tabPromoteCmd(m.deps.SavedSearchSvc, name)
	case "remove":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("tab remove: usage :tab remove <name>")
			return m, nil
		}
		name := strings.Join(args[1:], " ")
		return m, tabDemoteCmd(m.deps.SavedSearchSvc, name)
	case "move":
		if len(args) < 3 {
			m.lastError = fmt.Errorf("tab move: usage :tab move <name> <position>")
			return m, nil
		}
		name := args[1]
		pos := 0
		if _, err := fmt.Sscanf(args[2], "%d", &pos); err != nil {
			m.lastError = fmt.Errorf("tab move: position %q is not an integer", args[2])
			return m, nil
		}
		return m, tabReorderByNameCmd(m.deps.SavedSearchSvc, name, pos)
	case "close":
		if m.activeTab < 0 || m.activeTab >= len(m.tabs) {
			m.lastError = fmt.Errorf("tab close: no active tab")
			return m, nil
		}
		name := m.tabs[m.activeTab].Name
		return m, tabDemoteCmd(m.deps.SavedSearchSvc, name)
	}
	// Treat as a tab name jump.
	name := strings.Join(args, " ")
	for i, t := range m.tabs {
		if t.Name == name {
			mm, cmd := m.activateTab(i)
			return mm, cmd
		}
	}
	m.lastError = fmt.Errorf("tab: no tab named %q (use :tab list to see all)", name)
	return m, nil
}

func (m Model) tabListSummary() (tea.Model, tea.Cmd) {
	if len(m.tabs) == 0 {
		m.engineActivity = "tabs: (none configured)"
		return m, m.clearTransientCmd()
	}
	names := make([]string, 0, len(m.tabs))
	for _, t := range m.tabs {
		names = append(names, t.Name)
	}
	m.engineActivity = "tabs: " + strings.Join(names, ", ")
	return m, m.clearTransientCmd()
}

// tabMutationDoneMsg fires after a successful Promote/Demote/Reorder
// so the dispatch loop can reload the tab list.
type tabMutationDoneMsg struct {
	verb string
	err  error
}

func tabPromoteCmd(svc SavedSearchService, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := svc.PromoteTab(ctx, name)
		return tabMutationDoneMsg{verb: "promote", err: err}
	}
}

func tabDemoteCmd(svc SavedSearchService, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := svc.DemoteTab(ctx, name)
		return tabMutationDoneMsg{verb: "demote", err: err}
	}
}

func tabReorderByNameCmd(svc SavedSearchService, name string, to int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Resolve the current position via Tabs.
		tabs, err := svc.Tabs(ctx)
		if err != nil {
			return tabMutationDoneMsg{verb: "reorder", err: err}
		}
		from := -1
		for i, t := range tabs {
			if t.Name == name {
				from = i
				break
			}
		}
		if from < 0 {
			return tabMutationDoneMsg{verb: "reorder", err: fmt.Errorf("tab: no tab named %q", name)}
		}
		if to < 0 || to >= len(tabs) {
			return tabMutationDoneMsg{verb: "reorder", err: fmt.Errorf("tab: position %d out of range (have %d)", to, len(tabs))}
		}
		err = svc.ReorderTab(ctx, from, to)
		return tabMutationDoneMsg{verb: "reorder", err: err}
	}
}
