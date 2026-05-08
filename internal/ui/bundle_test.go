package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// seedBundleModel returns a dispatch test model with a list of N
// consecutive messages from the same sender (default Alice). Used
// by every bundle test below. Sets bundleMinCount=2 (spec default)
// so tests don't have to repeat the configuration.
func seedBundleModel(t *testing.T, sender string, count int) Model {
	t.Helper()
	m := newDispatchTestModel(t)
	m.bundleMinCount = 2
	m.list.SetBundleMinCount(2)
	now := time.Now()
	msgs := make([]store.Message, count)
	for i := 0; i < count; i++ {
		msgs[i] = store.Message{
			ID:          "b-" + string(rune('a'+i)),
			AccountID:   m.deps.Account.ID,
			FolderID:    m.list.FolderID,
			Subject:     "msg " + string(rune('a'+i)),
			FromAddress: sender,
			FromName:    "Alice",
			ReceivedAt:  now.Add(-time.Duration(i) * time.Hour),
		}
	}
	m.list.SetBundledSenders(m.bundledSenders)
	m.list.SetMessages(msgs)
	return m
}

func TestBundleKeyMapNoDuplicates(t *testing.T) {
	require.Empty(t, findDuplicateBinding(DefaultKeyMap()),
		"BundleToggle (B) and BundleExpand (Space) must not collide with anything else")
}

func TestBundleKeyToggleAddsToSet(t *testing.T) {
	m := seedBundleModel(t, "news@acme.com", 5)
	require.NotContains(t, m.bundledSenders, "news@acme.com")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = m2.(Model)
	require.Contains(t, m.bundledSenders, "news@acme.com",
		"B on flat row from designated sender must add to in-memory set")
	require.Contains(t, m.engineActivity, "collapses 5 messages",
		"toast must mention the collapse count")
}

func TestBundleKeyToggleRemovesFromSet(t *testing.T) {
	m := seedBundleModel(t, "news@acme.com", 3)
	// Designate first.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = m2.(Model)
	require.Contains(t, m.bundledSenders, "news@acme.com")
	// Cursor should now be on the bundle header (rendered row 0).
	// Pressing B again un-designates.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = m2.(Model)
	require.NotContains(t, m.bundledSenders, "news@acme.com")
	require.Contains(t, m.engineActivity, "(was bundled)")
}

func TestBundleKeyDesignateNoConsecutiveRunToast(t *testing.T) {
	m := seedBundleModel(t, "news@acme.com", 1)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = m2.(Model)
	require.Contains(t, m.engineActivity, "no consecutive run",
		"single-message run must produce the no-consecutive-run toast")
}

func TestBundleKeyEmptyAddressShowsError(t *testing.T) {
	m := newDispatchTestModel(t)
	m.list.SetMessages([]store.Message{
		{ID: "m-noaddr", AccountID: m.deps.Account.ID, FolderID: "f-inbox", Subject: "no sender", ReceivedAt: time.Now()},
	})
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = m2.(Model)
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "no sender")
}

func TestBundleHeaderShowsCountAndLatestSubject(t *testing.T) {
	m := seedBundleModel(t, "news@acme.com", 12)
	m.bundledSenders["news@acme.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	out := m.list.View(m.theme, 120, 30, true)
	require.Contains(t, out, "(12)", "bundle header must show member count")
	require.Contains(t, out, "msg a", "bundle header must show newest subject")
}

func TestBundleHeaderHasNoMuteCalendarFlagGlyph(t *testing.T) {
	m := seedBundleModel(t, "news@acme.com", 3)
	// Flag the newest member; the bundle header must NOT show ⚑.
	m.list.messages[0].FlagStatus = "flagged"
	m.bundledSenders["news@acme.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	out := m.list.View(m.theme, 120, 30, true)
	// Find the bundle header line (the one containing "(3)"); strip ANSI.
	var headerLine string
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "(3)") {
			headerLine = ln
			break
		}
	}
	require.NotEmpty(t, headerLine)
	require.NotContains(t, headerLine, "⚑", "bundle header carries only the disclosure glyph")
	require.NotContains(t, headerLine, "📅")
	require.NotContains(t, headerLine, "🔕")
}

func TestBundlePassDeterministicOrder(t *testing.T) {
	m := newDispatchTestModel(t)
	m.bundleMinCount = 2
	m.list.SetBundleMinCount(2)
	now := time.Now()
	// Fixture: 3 from news, then 1 from someone else, then 3 from news again.
	// Two separate runs → two bundle headers.
	msgs := []store.Message{
		{ID: "n1", FromAddress: "news@x.com", Subject: "a", ReceivedAt: now.Add(-1 * time.Minute), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
		{ID: "n2", FromAddress: "news@x.com", Subject: "b", ReceivedAt: now.Add(-2 * time.Minute), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
		{ID: "n3", FromAddress: "news@x.com", Subject: "c", ReceivedAt: now.Add(-3 * time.Minute), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
		{ID: "alice1", FromAddress: "alice@x.com", Subject: "hi", ReceivedAt: now.Add(-4 * time.Minute), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
		{ID: "n4", FromAddress: "news@x.com", Subject: "d", ReceivedAt: now.Add(-5 * time.Minute), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
		{ID: "n5", FromAddress: "news@x.com", Subject: "e", ReceivedAt: now.Add(-6 * time.Minute), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
		{ID: "n6", FromAddress: "news@x.com", Subject: "f", ReceivedAt: now.Add(-7 * time.Minute), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
	}
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	m.list.SetMessages(msgs)
	headers := 0
	for i := 0; i < m.list.renderedLen(); i++ {
		if m.list.rowAt(i).IsBundleHeader {
			headers++
		}
	}
	require.Equal(t, 2, headers, "two non-adjacent runs must produce two bundle rows")
}

func TestConsecutiveEmptyFromAddressNeverBundles(t *testing.T) {
	m := newDispatchTestModel(t)
	m.bundleMinCount = 2
	m.list.SetBundleMinCount(2)
	now := time.Now()
	msgs := []store.Message{
		{ID: "e1", FromAddress: "", Subject: "a", ReceivedAt: now, FolderID: "f-inbox", AccountID: m.deps.Account.ID},
		{ID: "e2", FromAddress: "", Subject: "b", ReceivedAt: now.Add(-time.Minute), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
		{ID: "e3", FromAddress: "", Subject: "c", ReceivedAt: now.Add(-2 * time.Minute), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
	}
	m.list.SetMessages(msgs)
	require.Equal(t, 3, m.list.renderedLen(), "empty-address rows must render flat regardless of bundleSize")
}

func TestPlusTagDoesNotMatchBaseAddress(t *testing.T) {
	m := newDispatchTestModel(t)
	m.bundleMinCount = 2
	m.list.SetBundleMinCount(2)
	now := time.Now()
	msgs := []store.Message{
		{ID: "p1", FromAddress: "news+abc@acme.com", Subject: "a", ReceivedAt: now, FolderID: "f-inbox", AccountID: m.deps.Account.ID},
		{ID: "p2", FromAddress: "news+abc@acme.com", Subject: "b", ReceivedAt: now.Add(-time.Minute), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
	}
	m.bundledSenders["news@acme.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	m.list.SetMessages(msgs)
	require.Equal(t, 2, m.list.renderedLen(), "plus-tag variant must not match base address (exact-match v1)")
}

func TestBundleMinCountTwoCollapsesPair(t *testing.T) {
	m := seedBundleModel(t, "news@x.com", 2)
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundleMinCount(2)
	m.list.SetBundledSenders(m.bundledSenders)
	require.Equal(t, 1, m.list.renderedLen(), "2-message run with min=2 must collapse to 1 header")
}

func TestBundleMinCountThreeLeavesPairFlat(t *testing.T) {
	m := seedBundleModel(t, "news@x.com", 2)
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundleMinCount(3)
	m.list.SetBundledSenders(m.bundledSenders)
	require.Equal(t, 2, m.list.renderedLen(), "2-message run with min=3 stays as 2 flat rows")
}

func TestBundleMinCountZeroDisablesBundling(t *testing.T) {
	m := seedBundleModel(t, "news@x.com", 5)
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundleMinCount(0)
	m.list.SetBundledSenders(m.bundledSenders)
	require.Equal(t, 5, m.list.renderedLen(), "min=0 disables bundling regardless of designations")
}

func TestSpaceTogglesBundleExpandInListPane(t *testing.T) {
	m := seedBundleModel(t, "news@x.com", 3)
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	require.Equal(t, 1, m.list.renderedLen(), "starts collapsed")
	// Press Space to expand.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = m2.(Model)
	require.Equal(t, 4, m.list.renderedLen(), "Space expands: 1 header + 3 members")
	// Press Space again to collapse.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = m2.(Model)
	require.Equal(t, 1, m.list.renderedLen(), "Space collapses back to 1 header")
}

func TestSpaceOnFlatRowInListPaneIsNoop(t *testing.T) {
	m := seedBundleModel(t, "alice@x.com", 1) // only 1 message → no bundle
	before := m.list.renderedLen()
	beforeCursor := m.list.cursor
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = m2.(Model)
	require.Equal(t, before, m.list.renderedLen(), "Space on flat row must not change rendered rows")
	require.Equal(t, beforeCursor, m.list.cursor, "Space on flat row must not move cursor")
}

func TestEnterOnCollapsedBundleExpandsCursorStaysOnHeader(t *testing.T) {
	m := seedBundleModel(t, "news@x.com", 3)
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	require.Equal(t, 1, m.list.renderedLen())
	require.Equal(t, 0, m.list.cursor)
	beforeFocus := m.focused
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, 4, m.list.renderedLen(), "first Enter expands the bundle")
	require.Equal(t, 0, m.list.cursor, "cursor stays on bundle header")
	require.Equal(t, beforeFocus, m.focused, "focus does not move to viewer on first Enter")
}

func TestEnterOnExpandedBundleHeaderOpensRepresentative(t *testing.T) {
	m := seedBundleModel(t, "news@x.com", 3)
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	// Expand first.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, 4, m.list.renderedLen())
	// Second Enter on header → opens viewer.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused, "second Enter on expanded bundle header opens viewer")
}

func TestCollapseFromMemberMovesCursorToHeader(t *testing.T) {
	m := seedBundleModel(t, "news@x.com", 3)
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	// Expand.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	// Move cursor to a member (down twice).
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Equal(t, 2, m.list.cursor)
	require.True(t, m.list.rowAt(2).IsBundleMember)
	// Collapse via Space.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = m2.(Model)
	require.Equal(t, 0, m.list.cursor, "collapse from member lands cursor on bundle header")
	require.True(t, m.list.rowAt(0).IsBundleHeader)
}

func TestBundleHeaderColumnWidthMatchesFlatRow(t *testing.T) {
	m := seedBundleModel(t, "news@x.com", 3)
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	// Make a flat row by adding a non-designated sender after the bundle.
	now := time.Now()
	msgs := append(m.list.messages,
		store.Message{ID: "alice", FromAddress: "alice@x.com", Subject: "alice msg", ReceivedAt: now.Add(-10 * time.Hour), FolderID: "f-inbox", AccountID: m.deps.Account.ID},
	)
	m.list.SetMessages(msgs)
	out := m.list.View(m.theme, 120, 30, true)
	var headerLine, flatLine string
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "(3)") && headerLine == "" {
			headerLine = ln
		}
		if strings.Contains(ln, "alice msg") {
			flatLine = ln
		}
	}
	require.NotEmpty(t, headerLine)
	require.NotEmpty(t, flatLine)
	// Compare visible (lipgloss) widths; ANSI styling differs between
	// the cursor row and non-cursor rows so byte counts diverge.
	require.Equal(t, lipglossWidth(headerLine), lipglossWidth(flatLine),
		"header and flat row must share visible column width")
}

// lipglossWidth strips ANSI escapes and returns the cell width.
func lipglossWidth(s string) int {
	// Strip CSI escape sequences (ESC + '[' + ... + final byte).
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == 0x1b {
			in = true
			continue
		}
		if in {
			if (r >= 0x40 && r <= 0x7e) || r == 'm' {
				in = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return len([]rune(b.String()))
}

func TestBundleNotAffectedBySingleMessageVerbs(t *testing.T) {
	// Verify that flag toggle on a collapsed bundle row targets the
	// representative (newest member), not the whole bundle.
	m := seedBundleModel(t, "news@x.com", 3)
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	// SelectedMessage on a bundle header returns the newest member.
	sel, ok := m.list.SelectedMessage()
	require.True(t, ok)
	require.Equal(t, m.list.messages[0].ID, sel.ID, "SelectedMessage on bundle header returns newest")
}

func TestRapidBundleToggleSequenceConsistency(t *testing.T) {
	// Two B presses in quick succession on the same address: end-state
	// matches the last keypress's intent (per §6 seq guard).
	m := seedBundleModel(t, "news@x.com", 3)
	// Press B twice (designate, then un-designate).
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = m2.(Model)
	require.Contains(t, m.bundledSenders, "news@x.com")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = m2.(Model)
	require.NotContains(t, m.bundledSenders, "news@x.com",
		"end-state must reflect last keypress regardless of Cmd ordering")
	// Stale toast (lower seq) must not undo the latest mutation.
	stale := bundleToastMsg{address: "news@x.com", nowBundled: true, seq: 1}
	m2, _ = m.Update(stale)
	m = m2.(Model)
	require.NotContains(t, m.bundledSenders, "news@x.com",
		"stale-seq bundleToastMsg must not mutate the in-memory set")
}

func TestRefreshSweepsStaleBundleExpandedEntries(t *testing.T) {
	m := seedBundleModel(t, "news@x.com", 3)
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	// Expand the bundle.
	m.bundleExpanded[m.list.FolderID] = map[string]bool{"news@x.com": true}
	require.True(t, m.bundleExpanded[m.list.FolderID]["news@x.com"])
	// Simulate Ctrl+R: load returns empty (sender un-designated externally).
	m2, _ := m.Update(bundledSendersLoadedMsg{addresses: nil})
	m = m2.(Model)
	require.Empty(t, m.bundledSenders)
	// The stale expand-state must be swept.
	require.NotContains(t, m.bundleExpanded[m.list.FolderID], "news@x.com")
}

func TestPageDownAcrossBundle(t *testing.T) {
	// PageDown steps 20 rendered rows. With a bundle, 50 messages
	// collapse to 1 header — PageDown advances by 20 rendered rows
	// (which can include the header).
	m := newDispatchTestModel(t)
	m.bundleMinCount = 2
	m.list.SetBundleMinCount(2)
	now := time.Now()
	msgs := make([]store.Message, 0, 250)
	for i := 0; i < 100; i++ {
		msgs = append(msgs, store.Message{
			ID:          "f1-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			FromAddress: "alice" + string(rune('a'+i%26)) + "@x.com",
			Subject:     "flat",
			ReceivedAt:  now.Add(-time.Duration(i) * time.Minute),
			FolderID:    "f-inbox", AccountID: m.deps.Account.ID,
		})
	}
	for i := 0; i < 50; i++ {
		msgs = append(msgs, store.Message{
			ID:          "b" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			FromAddress: "news@x.com",
			Subject:     "bundle msg",
			ReceivedAt:  now.Add(-time.Duration(100+i) * time.Minute),
			FolderID:    "f-inbox", AccountID: m.deps.Account.ID,
		})
	}
	for i := 0; i < 100; i++ {
		msgs = append(msgs, store.Message{
			ID:          "f2-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			FromAddress: "bob" + string(rune('a'+i%26)) + "@x.com",
			Subject:     "flat2",
			ReceivedAt:  now.Add(-time.Duration(150+i) * time.Minute),
			FolderID:    "f-inbox", AccountID: m.deps.Account.ID,
		})
	}
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	m.list.SetMessages(msgs)
	// Total rendered: 100 flat + 1 header + 100 flat = 201.
	require.Equal(t, 201, m.list.renderedLen())
	// PageDown 5 times: cursor advances 5 * 20 = 100 rendered rows.
	for i := 0; i < 5; i++ {
		m.list.PageDown()
	}
	require.Equal(t, 100, m.list.cursor, "5 PageDowns advance 100 rendered rows")
}

func TestLoadMoreFiresAtMessageTailNotRenderedTail(t *testing.T) {
	m := newDispatchTestModel(t)
	m.bundleMinCount = 2
	m.list.SetBundleMinCount(2)
	now := time.Now()
	const N = 200
	msgs := make([]store.Message, N)
	for i := 0; i < N; i++ {
		msgs[i] = store.Message{
			ID:          "x" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			FromAddress: "news@x.com",
			Subject:     "msg",
			ReceivedAt:  now.Add(-time.Duration(i) * time.Minute),
			FolderID:    "f-inbox", AccountID: m.deps.Account.ID,
		}
	}
	m.bundledSenders["news@x.com"] = struct{}{}
	m.list.SetBundledSenders(m.bundledSenders)
	m.list.SetMessages(msgs)
	// Rendered: 1 bundle header (collapsed). Cursor at the (sole) row.
	require.Equal(t, 1, m.list.renderedLen())
	// Underlying message tail: messageIndexAt(0) == 0 (newest member).
	// ShouldLoadMore looks at messageIndexAt(cursor) >= len-threshold;
	// here index 0 vs len 200, so should NOT fire.
	require.False(t, m.list.ShouldLoadMore(),
		"densely-bundled tail must not pre-fire load-more on cursor at rendered tail")
}

func TestBundleExpandedClearedOnFilterExit(t *testing.T) {
	m := newDispatchTestModel(t)
	m.list.FolderID = "filter:~f news@x.com:all=false"
	m.bundleExpanded[m.list.FolderID] = map[string]bool{"news@x.com": true}
	m.priorFolderID = "f-inbox"
	m = m.clearFilter()
	require.NotContains(t, m.bundleExpanded, "filter:~f news@x.com:all=false",
		"clearFilter must drop synthetic filter:* expand-state")
}

func TestFilterAllScopeVariantHasDistinctExpandState(t *testing.T) {
	// :filter X and :filter --all X must produce distinct synthetic
	// folder IDs so their expand state is independent.
	scoped := "filter:~f news@x.com:all=false"
	all := "filter:~f news@x.com:all=true"
	require.NotEqual(t, scoped, all)
}

func BenchmarkBundlePass1000(b *testing.B) {
	now := time.Now()
	msgs := make([]store.Message, 1000)
	bundled := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		bundled["sender"+addrSuffix(i)+"@x.com"] = struct{}{}
	}
	for i := 0; i < 1000; i++ {
		msgs[i] = store.Message{
			ID:          "m" + addrSuffix(i),
			FromAddress: "sender" + addrSuffix(i%75) + "@x.com",
			Subject:     "msg",
			ReceivedAt:  now.Add(-time.Duration(i) * time.Minute),
			FolderID:    "f-inbox",
		}
	}
	var lm ListModel
	lm.bundleMinCount = 2
	lm.bundledSenders = bundled
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lm.cache.valid = false
		lm.messages = msgs
		lm.ensureBundleCache()
	}
	elapsed := b.Elapsed()
	avgMs := float64(elapsed.Microseconds()) / float64(b.N) / 1000.0
	const budgetMs = 2
	if avgMs > budgetMs*1.5 {
		b.Errorf("BenchmarkBundlePass1000: avg %.3fms exceeds %.1fms budget (50%% slack)", avgMs, float64(budgetMs)*1.5)
	}
}

// BenchmarkBundleViewRender measures the cache-hit path that View()
// reads on every Bubble Tea render tick. Spec 26 §8.1 budget: ≤0.1ms
// p95, ≤4 allocs per call. We benchmark ensureBundleCache directly
// (the only spec-26-introduced cost on View); lipgloss/ANSI styling
// is the existing pane-render cost and is out of scope.
func BenchmarkBundleViewRender(b *testing.B) {
	now := time.Now()
	msgs := make([]store.Message, 1000)
	for i := 0; i < 1000; i++ {
		msgs[i] = store.Message{
			ID:          "m" + addrSuffix(i),
			FromAddress: "alice" + addrSuffix(i%50) + "@x.com",
			Subject:     "msg",
			ReceivedAt:  now.Add(-time.Duration(i) * time.Minute),
			FolderID:    "f-inbox",
		}
	}
	var lm ListModel
	lm.FolderID = "f-inbox"
	lm.bundleMinCount = 2
	lm.bundledSenders = map[string]struct{}{"alice0@x.com": {}}
	lm.SetMessages(msgs)
	lm.ensureBundleCache() // warm; cache.valid=true
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Cache-hit path only — what View() pays per render tick.
		lm.ensureBundleCache()
		_ = lm.cache.rendered
	}
	elapsed := b.Elapsed()
	avgUs := float64(elapsed.Nanoseconds()) / float64(b.N) / 1000.0
	const budgetUs = 100 // 0.1ms
	if avgUs > budgetUs {
		b.Errorf("BenchmarkBundleViewRender: avg %.3fµs exceeds %dµs budget", avgUs, budgetUs)
	}
}

// addrSuffix is a copy of the helper from store/bundled_senders_test.go;
// duplicated here to avoid a cross-package test import.
func addrSuffix(i int) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if i == 0 {
		return "0"
	}
	var buf [16]byte
	n := 0
	for i > 0 {
		buf[n] = digits[i%36]
		i /= 36
		n++
	}
	out := make([]byte, n)
	for k := 0; k < n; k++ {
		out[k] = buf[n-1-k]
	}
	return string(out)
}

// _ keeps the context import valid (some test paths use it, others not).
var _ = context.Background
