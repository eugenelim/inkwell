package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// withInboxSplitOn enables the spec-31 sub-strip and pre-seeds Inbox
// rows with both inference classes so cycle / cmd-bar paths have data.
func withInboxSplitOn(t *testing.T, m Model) Model {
	t.Helper()
	m.inboxSplit = InboxSplitFocusedOther
	m.inboxSplitDefaultSegment = "focused"
	// Replace the standard Inbox content with a tagged set so the
	// segment-specific list loads exercise the actual SQL.
	ctx := context.Background()
	acc := m.deps.Account.ID
	base := time.Now()
	rows := []store.Message{
		{ID: "msg-foc-1", AccountID: acc, FolderID: "f-inbox", Subject: "Focused-1", FromAddress: "f1@example.invalid", ReceivedAt: base.Add(-time.Hour), InferenceClass: store.InferenceClassFocused},
		{ID: "msg-foc-2", AccountID: acc, FolderID: "f-inbox", Subject: "Focused-2", FromAddress: "f2@example.invalid", ReceivedAt: base.Add(-2 * time.Hour), InferenceClass: store.InferenceClassFocused},
		{ID: "msg-oth-1", AccountID: acc, FolderID: "f-inbox", Subject: "Other-1", FromAddress: "o1@example.invalid", ReceivedAt: base.Add(-3 * time.Hour), InferenceClass: store.InferenceClassOther},
	}
	for _, row := range rows {
		require.NoError(t, m.deps.Store.UpsertMessage(ctx, row))
	}
	return m
}

func TestSpec31ColonFocusedActivatesFocusedSegment(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	mm, _ := m.dispatchCommand("focused")
	got := mm.(Model)
	require.Equal(t, inboxSubTabFocused, got.activeInboxSubTab)
}

func TestSpec31ColonOtherActivatesOtherSegment(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	mm, _ := m.dispatchCommand("other")
	got := mm.(Model)
	require.Equal(t, inboxSubTabOther, got.activeInboxSubTab)
}

func TestSpec31ColonFocusedFromNonInboxNavigatesToInbox(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	// Pretend the user is on Archive.
	m.list.FolderID = "f-archive"
	mm, _ := m.dispatchCommand("focused")
	got := mm.(Model)
	require.Equal(t, "f-inbox", got.list.FolderID)
	require.Equal(t, inboxSubTabFocused, got.activeInboxSubTab)
}

func TestSpec31ColonFocusedWhenSplitOffShowsError(t *testing.T) {
	m := newDispatchTestModel(t)
	require.Equal(t, InboxSplitOff, m.inboxSplit)
	mm, _ := m.dispatchCommand("focused")
	got := mm.(Model)
	require.NotNil(t, got.lastError)
	require.Contains(t, got.lastError.Error(), "inbox split is off")
}

func TestSpec31ColonOtherWhenSplitOffShowsError(t *testing.T) {
	m := newDispatchTestModel(t)
	mm, _ := m.dispatchCommand("other")
	got := mm.(Model)
	require.NotNil(t, got.lastError)
	require.Contains(t, got.lastError.Error(), "inbox split is off")
}

func TestSpec31NextTabPressCyclesInboxSubStripWhenNoSpec24Tabs(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	require.Empty(t, m.tabs)
	mm, _ := m.cycleInboxSubTab(+1)
	require.Equal(t, inboxSubTabFocused, mm.activeInboxSubTab)
}

func TestSpec31NextTabPressCyclesSpec24WhenTabsConfigured(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	zero := 0
	m.tabs = []SavedSearch{{ID: 1, Name: "t1", Pattern: "~F", TabOrder: &zero}}
	m.tabState = make([]listSnapshot, 1)
	m.tabUnread = map[int64]int{1: 0}
	m.tabLastFocused = map[int64]time.Time{}

	mm, _ := m.cycleTab(+1)
	require.Equal(t, 0, mm.activeTab)
	require.Equal(t, -1, mm.activeInboxSubTab, "spec-24 cycle must NOT touch the inbox sub-strip")
}

func TestSpec31PrevTabFromMinusOneActivatesDefaultSegmentFocused(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	// "focused" default.
	mm, _ := m.cycleInboxSubTab(-1)
	require.Equal(t, inboxSubTabFocused, mm.activeInboxSubTab, "[ from -1 lands on the configured default segment (\"focused\")")
}

func TestSpec31CycleFromMinusOneRespectsSplitDefaultSegmentOther(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	m.inboxSplitDefaultSegment = "other"
	mm, _ := m.cycleInboxSubTab(+1)
	require.Equal(t, inboxSubTabOther, mm.activeInboxSubTab)
}

func TestSpec31CycleFromMinusOneRespectsSplitDefaultSegmentNone(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	m.inboxSplitDefaultSegment = "none"
	mm, _ := m.cycleInboxSubTab(+1)
	require.Equal(t, -1, mm.activeInboxSubTab, "split_default_segment=none makes ] a no-op from -1")
	mm2, _ := m.cycleInboxSubTab(-1)
	require.Equal(t, -1, mm2.activeInboxSubTab, "split_default_segment=none makes [ a no-op too")
}

func TestSpec31CycleAfterActivationToggles(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	mm, _ := m.cycleInboxSubTab(+1)
	require.Equal(t, inboxSubTabFocused, mm.activeInboxSubTab)
	mm, _ = mm.cycleInboxSubTab(+1)
	require.Equal(t, inboxSubTabOther, mm.activeInboxSubTab)
	mm, _ = mm.cycleInboxSubTab(+1)
	require.Equal(t, inboxSubTabFocused, mm.activeInboxSubTab)
	mm, _ = mm.cycleInboxSubTab(-1)
	require.Equal(t, inboxSubTabOther, mm.activeInboxSubTab)
}

func TestSpec31SubStripHiddenOnNonInboxFolder(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	m.list.FolderID = "f-archive"
	require.False(t, m.inboxSubStripShouldRender(), "sub-strip hidden outside Inbox")
	require.Empty(t, m.renderInboxSubStrip(m.theme, 80))
}

func TestSpec31SubStripHiddenWhenSpec24TabActive(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	m.activeTab = 0
	require.False(t, m.inboxSubStripShouldRender())
}

func TestSpec31SubStripHiddenWhenFilterAllActive(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	m.filterAllFolders = true
	require.False(t, m.inboxSubStripShouldRender())
}

func TestSpec31SubStripHiddenWhenSplitOff(t *testing.T) {
	m := newDispatchTestModel(t)
	// inboxSplit = off by default.
	require.False(t, m.inboxSubStripShouldRender())
}

func TestSpec31SubStripRenderShowsBothSegments(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	out := m.renderInboxSubStrip(m.theme, 80)
	require.Contains(t, out, "Focused")
	require.Contains(t, out, "Other")
}

func TestSpec31ListMessagesByInferenceClassFromUIPath(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	// Exercise the load Cmd directly to ensure the wiring is correct.
	cmd := m.loadInboxSubTabCmd("f-inbox", inboxSubTabFocused)
	require.NotNil(t, cmd)
	msg := cmd()
	loaded, ok := msg.(inboxSubTabLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)
	require.Equal(t, inboxSubTabFocused, loaded.segment)
	require.GreaterOrEqual(t, len(loaded.messages), 2)
	for _, mm := range loaded.messages {
		require.Equal(t, store.InferenceClassFocused, mm.InferenceClass)
	}
}

func TestSpec31PaletteShowsInboxSection(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	rows := buildInboxPaletteRows(&m)
	require.Len(t, rows, 2)
	for _, r := range rows {
		require.Equal(t, sectionInbox, r.Section)
		require.True(t, r.Available.OK)
	}
}

func TestSpec31PaletteSynonymUnfocusedMatchesOther(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	rows := buildInboxPaletteRows(&m)
	otherRow := rows[1]
	found := false
	for _, syn := range otherRow.Synonyms {
		if syn == "unfocused" {
			found = true
		}
	}
	require.True(t, found, "Other row must have \"unfocused\" as a synonym")
}

func TestSpec31PaletteSynonymClutterDoesNotMatch(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	rows := buildInboxPaletteRows(&m)
	for _, r := range rows {
		for _, syn := range r.Synonyms {
			require.NotEqualf(t, "clutter", strings.ToLower(syn), "row %s must not contain \"clutter\" synonym", r.ID)
		}
	}
}

func TestSpec31SubStripDisabledHintRendersWhenSplitOff(t *testing.T) {
	m := newDispatchTestModel(t)
	rows := buildInboxPaletteRows(&m)
	require.Len(t, rows, 2)
	for _, r := range rows {
		require.False(t, r.Available.OK)
		require.Contains(t, r.Available.Why, "inbox split is off")
	}
}

func TestSpec31SubStripExcludesScreenedOutWhenScreenerEnabled(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	m.screenerEnabled = true
	ctx := context.Background()
	// Route the second focused sender to screener.
	_, err := m.deps.Store.SetSenderRouting(ctx, m.deps.Account.ID, "f2@example.invalid", store.RoutingScreener)
	require.NoError(t, err)
	cmd := m.loadInboxSubTabCmd("f-inbox", inboxSubTabFocused)
	loaded := cmd().(inboxSubTabLoadedMsg)
	require.NoError(t, loaded.err)
	for _, mm := range loaded.messages {
		require.NotEqual(t, "f2@example.invalid", mm.FromAddress, "screened-out sender must be excluded when screener enabled")
	}
}

func TestSpec31SubStripIncludesScreenedOutWhenScreenerDisabled(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	require.False(t, m.screenerEnabled)
	ctx := context.Background()
	_, err := m.deps.Store.SetSenderRouting(ctx, m.deps.Account.ID, "f2@example.invalid", store.RoutingScreener)
	require.NoError(t, err)
	cmd := m.loadInboxSubTabCmd("f-inbox", inboxSubTabFocused)
	loaded := cmd().(inboxSubTabLoadedMsg)
	addrs := make([]string, 0, len(loaded.messages))
	for _, mm := range loaded.messages {
		addrs = append(addrs, mm.FromAddress)
	}
	require.Contains(t, addrs, "f2@example.invalid", "screener off must NOT hide screener-routed senders")
}

func TestSpec31FilterOverSubTabBypassesScreenerWhenEnabled(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	m.screenerEnabled = true
	ctx := context.Background()
	_, err := m.deps.Store.SetSenderRouting(ctx, m.deps.Account.ID, "f2@example.invalid", store.RoutingScreener)
	require.NoError(t, err)

	// Direct helper: should EXCLUDE f2.
	directCmd := m.loadInboxSubTabCmd("f-inbox", inboxSubTabFocused)
	direct := directCmd().(inboxSubTabLoadedMsg)
	directAddrs := map[string]bool{}
	for _, mm := range direct.messages {
		directAddrs[mm.FromAddress] = true
	}
	require.False(t, directAddrs["f2@example.invalid"])

	// :filter path: ApplyScreenerFilter is false for all :filter
	// executions per spec 28 §5.4. f2 should appear.
	// Drive the dispatch path: set activeInboxSubTab first so the
	// dispatcher AND's `~y focused & ~m Inbox` into the user pattern.
	m.activeInboxSubTab = inboxSubTabFocused
	_, cmd := m.dispatchCommand("filter ~y focused")
	require.NotNil(t, cmd)
	msg := cmd()
	if errMsg, ok := msg.(ErrorMsg); ok {
		t.Fatalf("filter error: %v", errMsg.Err)
	}
	applied, ok := msg.(filterAppliedMsg)
	require.True(t, ok, "expected filterAppliedMsg, got %T", msg)
	filterAddrs := map[string]bool{}
	for _, mm := range applied.messages {
		filterAddrs[mm.FromAddress] = true
	}
	require.True(t, filterAddrs["f2@example.invalid"], ":filter path includes screener-routed senders; got %v", applied.messages)
}

func TestSpec31SubStripErrorOnInvalidClassPropagates(t *testing.T) {
	ctx := context.Background()
	m := newDispatchTestModel(t)
	_, err := m.deps.Store.ListMessagesByInferenceClass(ctx, m.deps.Account.ID, "f-inbox", "junk", 50, true, false)
	require.Error(t, err)
	require.True(t, errors.Is(err, store.ErrInvalidInferenceClass))
}

func TestSpec31InboxSubTabUnreadMsgUpdatesBadges(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	cmd := m.refreshInboxSubTabUnreadCmd()
	require.NotNil(t, cmd)
	msg := cmd()
	unread := msg.(inboxSubTabUnreadMsg)
	require.True(t, unread.focusedOK)
	require.True(t, unread.otherOK)
	require.Equal(t, "f-inbox", unread.folderID)

	mm, _ := m.Update(unread)
	got := mm.(Model)
	require.Equal(t, unread.focused, got.inboxSubTabUnread[inboxSubTabFocused])
	require.Equal(t, unread.other, got.inboxSubTabUnread[inboxSubTabOther])
	require.True(t, got.inboxSubTabUnreadOK[inboxSubTabFocused])
	require.True(t, got.inboxSubTabUnreadOK[inboxSubTabOther])
}

// Ensure search / compose modes don't cycle the sub-strip — `]`/`[`
// typed in those modes are part of the user's input, not cycle keys.
func TestSpec31NextTabPressInSearchModeDoesNotCycle(t *testing.T) {
	m := newDispatchTestModel(t)
	m = withInboxSplitOn(t, m)
	m.mode = SearchMode
	// The mode-dispatch guard short-circuits before dispatchList runs
	// the cycle case, so simulate by calling the precondition.
	require.True(t, m.inboxSubStripShouldRender(), "sub-strip would render in normal mode")
	// Search-mode input never reaches cycleInboxSubTab — assert
	// directly that the search-mode dispatch doesn't go through the
	// Normal-mode list-dispatch.
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}
	mm, _ := m.Update(msg)
	got := mm.(Model)
	require.Equal(t, -1, got.activeInboxSubTab, "]-keystroke in SearchMode must not cycle the sub-strip")
}
