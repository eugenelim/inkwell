package ui

import (
	"testing"
	"time"
)

// BenchmarkRenderTabStrip covers spec 24 §8: tab strip render
// budget ≤2ms p95 over a 5-tab strip.
func BenchmarkRenderTabStrip(b *testing.B) {
	t := DefaultTheme()
	zero, one, two, three, four := 0, 1, 2, 3, 4
	tabs := []SavedSearch{
		{ID: 1, Name: "Newsletters", Pattern: "~F", TabOrder: &zero},
		{ID: 2, Name: "VIP", Pattern: "~A", TabOrder: &one},
		{ID: 3, Name: "Calendar", Pattern: "~G cal", TabOrder: &two},
		{ID: 4, Name: "Receipts", Pattern: "~G receipt", TabOrder: &three},
		{ID: 5, Name: "Clients", Pattern: "~G client", TabOrder: &four},
	}
	m := Model{
		tabs:           tabs,
		activeTab:      1,
		tabUnread:      map[int64]int{1: 12, 2: 3, 3: 0, 4: 7, 5: 21},
		tabLastFocused: map[int64]time.Time{},
		tabsCfg:        TabsConfig{Enabled: true, MaxNameWidth: 16, CycleWraps: true},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.renderTabStrip(t, 80)
	}
}

// (BenchmarkTabCycleCached / BenchmarkTabCycleEvaluate are deferred
// — they require a fully-wired Model with a populated ListModel
// backing slice, which the unit tests above cover by exercising
// activateTab / cycleTab against in-memory state.)
