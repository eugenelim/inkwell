package ui

import (
	"testing"
	"time"

	"github.com/eugenelim/inkwell/internal/store"
)

// inboxBenchModel returns a minimal Model satisfying the §5.2
// preconditions so the renderer paints inside the bench loop. Used by
// the spec 31 §9 row-1 budget benchmark.
func inboxBenchModel() Model {
	var fm FoldersModel
	fm.SetFolders([]store.Folder{{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now()}})
	return Model{
		theme:               DefaultTheme(),
		inboxSplit:          InboxSplitFocusedOther,
		activeInboxSubTab:   inboxSubTabFocused,
		inboxSubTabUnread:   [2]int{12, 47},
		inboxSubTabUnreadOK: [2]bool{true, true},
		list:                ListModel{FolderID: "f-inbox"},
		folders:             fm,
	}
}

// BenchmarkRenderInboxSubStrip covers spec 31 §9 row 1: sub-strip
// render budget < 2ms p95. Lipgloss layout over two short strings.
func BenchmarkRenderInboxSubStrip(b *testing.B) {
	m := inboxBenchModel()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.renderInboxSubStrip(m.theme, 80)
	}
}

// BenchmarkInboxSubTabCycleCached covers spec 31 §9 row 2: cycle
// between segments when a cache snapshot is fresh. Pure model
// rotation; no DB hit.
func BenchmarkInboxSubTabCycleCached(b *testing.B) {
	m := inboxBenchModel()
	now := time.Now()
	m.inboxSubTabState[0] = listSnapshot{messages: []store.Message{{ID: "x"}}, capturedAt: now}
	m.inboxSubTabState[1] = listSnapshot{messages: []store.Message{{ID: "y"}}, capturedAt: now}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, _ = m.cycleInboxSubTab(+1)
	}
}
