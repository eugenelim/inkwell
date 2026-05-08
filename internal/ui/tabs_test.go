package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// withTabsLoaded seeds the model with three tabs (a, b, c) so tests
// can drive the cycle / activate paths without going through the
// SavedSearchService loadTabsCmd round-trip.
func withTabsLoaded(t *testing.T, m Model) Model {
	t.Helper()
	zero, one, two := 0, 1, 2
	m.tabs = []SavedSearch{
		{ID: 1, Name: "a", Pattern: "~F", TabOrder: &zero},
		{ID: 2, Name: "b", Pattern: "~A", TabOrder: &one},
		{ID: 3, Name: "c", Pattern: "~U", TabOrder: &two},
	}
	m.tabState = make([]listSnapshot, 3)
	m.tabUnread = map[int64]int{1: 0, 2: 5, 3: 0}
	m.tabLastFocused = make(map[int64]time.Time)
	m.activeTab = -1
	return m
}

func TestCycleTabFromColdStart(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	require.Equal(t, -1, m.activeTab)
	mm, _ := m.cycleTab(+1)
	require.Equal(t, 0, mm.activeTab, "cold start ] activates leftmost")

	m2 := withTabsLoaded(t, m)
	mm2, _ := m2.cycleTab(-1)
	require.Equal(t, 0, mm2.activeTab, "cold start [ also activates leftmost (spec 24 §7)")
}

func TestCycleTabWraps(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	m, _ = m.cycleTab(+1) // 0
	m, _ = m.cycleTab(+1) // 1
	m, _ = m.cycleTab(+1) // 2
	m, _ = m.cycleTab(+1) // wrap → 0
	require.Equal(t, 0, m.activeTab)

	m, _ = m.cycleTab(-1) // wrap → 2
	require.Equal(t, 2, m.activeTab)
}

func TestCycleTabNoWrapWhenDisabled(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	m.tabsCfg.CycleWraps = false
	m, _ = m.cycleTab(+1) // 0
	m, _ = m.cycleTab(+1) // 1
	m, _ = m.cycleTab(+1) // 2
	mm, _ := m.cycleTab(+1)
	require.Equal(t, 2, mm.activeTab, "no-wrap: ] at last is no-op")
	mm2, _ := mm.cycleTab(-1)
	mm2, _ = mm2.cycleTab(-1)
	mm2, _ = mm2.cycleTab(-1)
	require.Equal(t, 0, mm2.activeTab)
	mm2, _ = mm2.cycleTab(-1)
	require.Equal(t, 0, mm2.activeTab, "no-wrap: [ at first is no-op")
}

func TestRenderTabStripHiddenWhenNoTabs(t *testing.T) {
	m := newDispatchTestModel(t)
	out := m.renderTabStrip(m.theme, 80)
	require.Empty(t, out)
}

func TestRenderTabStripHiddenWhenDisabled(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	m.tabsCfg.Enabled = false
	out := m.renderTabStrip(m.theme, 80)
	require.Empty(t, out)
}

func TestRenderTabStripShowsBracketedNames(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	out := m.renderTabStrip(m.theme, 80)
	require.Contains(t, out, "a]")
	require.Contains(t, out, "b 5]")
	require.Contains(t, out, "c]")
}

func TestRenderTabStripShowsCount(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	// b has unread 5
	out := m.renderTabStrip(m.theme, 80)
	require.Contains(t, out, "b 5")
}

func TestRenderTabStripShowZeroCount(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	m.tabsCfg.ShowZeroCount = true
	out := m.renderTabStrip(m.theme, 80)
	require.Contains(t, out, "a 0", "show_zero_count = true must render the zero")
}

func TestTabStripActiveStyleSwitchesOnCycle(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	require.Equal(t, -1, m.activeTab)
	m, _ = m.cycleTab(+1)
	require.Equal(t, 0, m.activeTab, "cycle from cold start lands on tab 0")
	m, _ = m.cycleTab(+1)
	require.Equal(t, 1, m.activeTab, "second cycle lands on tab 1")
}

func TestActiveTabResetsToMinusOneOnLoad(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	m, _ = m.cycleTab(+1)
	require.Equal(t, 0, m.activeTab)
	// Apply a fresh tab list — same shape, same IDs → activeTab
	// preserved.
	m = m.applyTabsLoaded(m.tabs)
	require.Equal(t, 0, m.activeTab)
	// Apply a list missing the active tab's ID — activeTab clamps.
	zero := 0
	smaller := []SavedSearch{{ID: 99, Name: "z", Pattern: "~A", TabOrder: &zero}}
	m = m.applyTabsLoaded(smaller)
	require.Equal(t, -1, m.activeTab, "active id no longer present → -1")
}

func TestDispatchTabAddCmd(t *testing.T) {
	m := newDispatchTestModel(t)
	svc := &stubSavedSearchService{}
	m.deps.SavedSearchSvc = svc
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = out.(Model)
	for _, r := range "tab add Newsletters" {
		out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = out.(Model)
	}
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	require.NotNil(t, cmd, ":tab add must return a Cmd")
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		// drive each
		for _, c := range batch {
			if c != nil {
				_ = c()
			}
		}
	}
	_ = m
}

func TestDispatchTabListEmpty(t *testing.T) {
	m := newDispatchTestModel(t)
	svc := &stubSavedSearchService{}
	m.deps.SavedSearchSvc = svc
	mm, _ := m.dispatchTabCmd([]string{"list"})
	model := mm.(Model)
	require.Contains(t, model.engineActivity, "(none configured)")
}

func TestDispatchTabUnknownNameError(t *testing.T) {
	m := newDispatchTestModel(t)
	svc := &stubSavedSearchService{}
	m.deps.SavedSearchSvc = svc
	mm, _ := m.dispatchTabCmd([]string{"NotATab"})
	model := mm.(Model)
	require.Error(t, model.lastError)
	require.Contains(t, model.lastError.Error(), "no tab named")
}

func TestTabsLoadedMsgPopulatesModel(t *testing.T) {
	m := newDispatchTestModel(t)
	zero := 0
	tabs := []SavedSearch{{ID: 7, Name: "x", Pattern: "~A", TabOrder: &zero}}
	out, _ := m.Update(tabsLoadedMsg{tabs: tabs})
	mm := out.(Model)
	require.Len(t, mm.tabs, 1)
	require.Equal(t, "x", mm.tabs[0].Name)
}

func TestTabCountsUpdatedMsgPopulatesUnread(t *testing.T) {
	m := newDispatchTestModel(t)
	out, _ := m.Update(tabCountsUpdatedMsg{counts: map[int64]int{42: 9}})
	mm := out.(Model)
	require.Equal(t, 9, mm.tabUnread[42])
}

func TestActivateTabClearsActiveFilter(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	m.filterActive = true
	m.filterPattern = "test"
	m.filterIDs = []string{"a"}
	m.filterAllFolders = true
	mm, _ := m.activateTab(1)
	require.False(t, mm.filterActive, "activating tab clears active filter")
	require.Empty(t, mm.filterIDs)
}

func TestRenderTabStripTruncatesNarrow(t *testing.T) {
	m := newDispatchTestModel(t)
	zero := 0
	m.tabs = []SavedSearch{
		{ID: 1, Name: "a-very-long-tab-name", Pattern: "~A", TabOrder: &zero},
	}
	m.tabUnread = map[int64]int{1: 0}
	m.tabsCfg = TabsConfig{Enabled: true, MaxNameWidth: 5, CycleWraps: true}
	out := m.renderTabStrip(m.theme, 80)
	// First 4 chars of name + ellipsis, NOT the full name.
	require.NotContains(t, out, "a-very-long-tab-name")
	require.Contains(t, out, "…")
}

func TestTabStripOverflowMarker(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	out := m.renderTabStrip(m.theme, 8) // very narrow
	require.Contains(t, strings.TrimSpace(out), "›")
}

// integration-flavoured test: dispatch sequence ] in the list pane
// activates the first tab (cycleTab via the dispatcher).
func TestNextTabBindingActivatesTab(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	require.Equal(t, ListPane, m.focused)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	mm := out.(Model)
	require.Equal(t, 0, mm.activeTab)
}

func TestPrevTabBindingActivatesTab(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	mm := out.(Model)
	require.Equal(t, 0, mm.activeTab)
}

func TestBracketKeysInactiveOnFoldersPane(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = out.(Model)
	require.Equal(t, FoldersPane, m.focused)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	mm := out.(Model)
	require.Equal(t, -1, mm.activeTab, "] on folders pane must not activate a tab")
}

func TestCloseDemotesActiveTab(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withTabsLoaded(t, m)
	svc := &stubSavedSearchService{
		saved: []SavedSearch{{ID: 1, Name: "a", Pattern: "~F"}},
	}
	m.deps.SavedSearchSvc = svc
	m, _ = m.cycleTab(+1)
	require.Equal(t, 0, m.activeTab)
	mm, cmd := m.dispatchTabCmd([]string{"close"})
	m = mm.(Model)
	require.NotNil(t, cmd)
	require.NoError(t, m.lastError)
	// Drive the cmd — should fire tabMutationDoneMsg.
	msg := cmd()
	d, ok := msg.(tabMutationDoneMsg)
	require.True(t, ok)
	require.Equal(t, "demote", d.verb)
}

func TestInvalidateTabSnapshotClears(t *testing.T) {
	m := withTabsLoaded(t, newDispatchTestModel(t))
	m.tabState[0].cacheKey = "savedsearch:1"
	m.tabState[0].capturedAt = time.Now()
	m.invalidateTabSnapshot(1)
	require.Empty(t, m.tabState[0].cacheKey, "invalidate must clear snapshot for matching id")
}

var _ = context.Background
