package ui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// stubTriageArchive embeds the spec 04 full-surface stub and
// records archive calls so tests can assert `e` /
// `:archive` / `:done` all reach the same Triage.Archive path.
type stubTriageArchive struct {
	*stubTriageWithUndo
	called *[]string
}

func (s stubTriageArchive) Archive(_ context.Context, _ int64, id string) error {
	*s.called = append(*s.called, id)
	return nil
}

// TestKeyEArchivesFromList — pressing `e` while the list pane is
// focused on a message dispatches `runTriage("archive", …)`. The
// fakeTriage records the message ID so we can assert the path
// without taking a Graph round-trip.
func TestKeyEArchivesFromList(t *testing.T) {
	m := newDispatchTestModel(t)
	calls := []string{}
	m.deps.Triage = stubTriageArchive{stubTriageWithUndo: &stubTriageWithUndo{}, called: &calls}
	m.list.SetMessages([]store.Message{{
		ID: "m-archive-list", FromAddress: "alice@example.invalid", Subject: "x", ReceivedAt: time.Now(),
	}})
	m.focused = ListPane

	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = out.(Model)
	require.NotNil(t, cmd, "e must dispatch a routeCmd-style triage cmd")
	res := cmd()
	if msg, ok := res.(triageDoneMsg); ok {
		_, _ = msg, ok
	}
	require.Equal(t, []string{"m-archive-list"}, calls)
}

// TestKeyEArchivesFromViewer — pressing `e` in the viewer pane
// archives the focused viewer message.
func TestKeyEArchivesFromViewer(t *testing.T) {
	m := newDispatchTestModel(t)
	calls := []string{}
	m.deps.Triage = stubTriageArchive{stubTriageWithUndo: &stubTriageWithUndo{}, called: &calls}
	msg := store.Message{ID: "m-archive-viewer", FromAddress: "x@example.invalid", Subject: "y", ReceivedAt: time.Now()}
	m.viewer.SetMessage(msg)
	m.viewer.current = &msg
	m.focused = ViewerPane

	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = out.(Model)
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, []string{"m-archive-viewer"}, calls)
}

// TestColonDoneArchivesFocused — `:done` reaches the same archive
// path as the keybinding.
func TestColonDoneArchivesFocused(t *testing.T) {
	m := newDispatchTestModel(t)
	calls := []string{}
	m.deps.Triage = stubTriageArchive{stubTriageWithUndo: &stubTriageWithUndo{}, called: &calls}
	m.list.SetMessages([]store.Message{{
		ID: "m-done", FromAddress: "x@example.invalid", Subject: "y", ReceivedAt: time.Now(),
	}})
	m.focused = ListPane

	out, cmd := m.dispatchCommand("done")
	m = out.(Model)
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, []string{"m-done"}, calls)
}

// TestColonArchiveSamePathAsColonDone — both verbs land on the
// same dispatch path.
func TestColonArchiveSamePathAsColonDone(t *testing.T) {
	m := newDispatchTestModel(t)
	calls := []string{}
	m.deps.Triage = stubTriageArchive{stubTriageWithUndo: &stubTriageWithUndo{}, called: &calls}
	m.list.SetMessages([]store.Message{{
		ID: "m-archive-cmd", FromAddress: "x@example.invalid", Subject: "y", ReceivedAt: time.Now(),
	}})
	m.focused = ListPane

	_, cmd := m.dispatchCommand("archive")
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, []string{"m-archive-cmd"}, calls)
}

// TestColonDoneOnEmptyListShowsError — empty list → `<verb>: no
// message focused`.
func TestColonDoneOnEmptyListShowsError(t *testing.T) {
	m := newDispatchTestModel(t)
	m.list.SetMessages(nil) // empty list
	m.viewer.current = nil
	m.focused = ListPane

	out, _ := m.dispatchCommand("done")
	m = out.(Model)
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "done: no message focused")
}

// TestColonArchiveOnEmptyListShowsError — same shape; the typed
// verb is what shows up in the error.
func TestColonArchiveOnEmptyListShowsError(t *testing.T) {
	m := newDispatchTestModel(t)
	m.list.SetMessages(nil)
	m.viewer.current = nil
	m.focused = ListPane

	out, _ := m.dispatchCommand("archive")
	m = out.(Model)
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "archive: no message focused")
}

// TestArchiveToastReadsArchiveWhenLabelArchive pins the spec 30
// §5.2 success-toast format under the default label.
func TestArchiveToastReadsArchiveWhenLabelArchive(t *testing.T) {
	m := newDispatchTestModel(t)
	m.archiveLabel = ArchiveLabelArchive
	m.filterActive = true
	m.filterPattern = "~U"
	out, _ := m.Update(triageDoneMsg{name: "archive", msgID: "m-1"})
	m = out.(Model)
	require.Equal(t, "✓ archive · u to undo", m.engineActivity)
}

// TestArchiveToastReadsDoneWhenLabelDone — same shape with the
// configured label.
func TestArchiveToastReadsDoneWhenLabelDone(t *testing.T) {
	m := newDispatchTestModel(t)
	m.archiveLabel = ArchiveLabelDone
	m.filterActive = true
	m.filterPattern = "~U"
	out, _ := m.Update(triageDoneMsg{name: "archive", msgID: "m-1"})
	m = out.(Model)
	require.Equal(t, "✓ done · u to undo", m.engineActivity)
}

// TestArchiveFailureToastReadsDoneWhenLabelDone pins the failure
// branch.
func TestArchiveFailureToastReadsDoneWhenLabelDone(t *testing.T) {
	m := newDispatchTestModel(t)
	m.archiveLabel = ArchiveLabelDone
	out, _ := m.Update(triageDoneMsg{name: "archive", msgID: "m-1", err: errSentinel})
	m = out.(Model)
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "done:")
}

// TestNonArchiveToastUnaffectedByLabel — soft_delete and other
// names pass through the helper unchanged.
func TestNonArchiveToastUnaffectedByLabel(t *testing.T) {
	m := newDispatchTestModel(t)
	m.archiveLabel = ArchiveLabelDone
	m.filterActive = true
	m.filterPattern = "~U"
	out, _ := m.Update(triageDoneMsg{name: "soft_delete", msgID: "m-1"})
	m = out.(Model)
	require.Equal(t, "✓ soft_delete · u to undo", m.engineActivity)
}

// TestBulkConfirmModalUsesConfiguredVerb — the bulk archive
// confirmation modal text uses the title-cased configured verb.
func TestBulkConfirmModalUsesConfiguredVerb(t *testing.T) {
	m := newDispatchTestModel(t)
	m.archiveLabel = ArchiveLabelDone
	m.filterIDs = []string{"m-1", "m-2", "m-3"}
	m.filterPattern = "~U"
	m.deps.Bulk = stubBulkExecutor{}
	out, _ := m.confirmBulk("archive", 3)
	m = out.(Model)
	require.Contains(t, m.confirm.Message, "Done 3 messages")
}

// TestBulkArchiveSuccessToastBranded — bulk archive toast uses
// the configured verb.
func TestBulkArchiveSuccessToastBranded(t *testing.T) {
	m := newDispatchTestModel(t)
	m.archiveLabel = ArchiveLabelDone
	out, _ := m.Update(bulkDoneMsg{name: "archive", succeeded: 5})
	m = out.(Model)
	require.Contains(t, m.engineActivity, "done")
}

// TestPaletteArchiveRowTitleSwitchesOnLabel — palette title flips
// per label.
func TestPaletteArchiveRowTitleSwitchesOnLabel(t *testing.T) {
	m := newDispatchTestModel(t)
	m.archiveLabel = ArchiveLabelArchive
	rows := buildStaticPaletteRows(&m)
	require.Equal(t, "Archive message", findPaletteRow(rows, "archive").Title)

	m.archiveLabel = ArchiveLabelDone
	rows = buildStaticPaletteRows(&m)
	require.Equal(t, "Mark done", findPaletteRow(rows, "archive").Title)
}

// TestPaletteArchiveSynonymMatchesArchiveAndDoneRegardlessOfLabel
// — both vocabularies match regardless of the configured label.
func TestPaletteArchiveSynonymMatchesArchiveAndDoneRegardlessOfLabel(t *testing.T) {
	m := newDispatchTestModel(t)
	m.archiveLabel = ArchiveLabelDone
	rows := buildStaticPaletteRows(&m)
	row := findPaletteRow(rows, "archive")
	require.Contains(t, row.Synonyms, "archive")
	require.Contains(t, row.Synonyms, "done")
	require.Contains(t, row.Synonyms, "file")

	threadRow := findPaletteRow(rows, "thread_archive")
	require.Contains(t, threadRow.Synonyms, "archive")
	require.Contains(t, threadRow.Synonyms, "done")
}

// TestSemicolonEArchivesFiltered — `;e` follows the same path as
// `;a`; bulk archive confirm modal opens.
func TestSemicolonEArchivesFiltered(t *testing.T) {
	m := newDispatchTestModel(t)
	m.archiveLabel = ArchiveLabelArchive
	m.filterActive = true
	m.filterPattern = "~U"
	m.filterIDs = []string{"m-1", "m-2"}
	m.deps.Bulk = stubBulkExecutor{}
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
	m = out.(Model)
	require.True(t, m.bulkPending)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = out.(Model)
	require.Equal(t, ConfirmMode, m.mode, "; e must enter the bulk-archive confirm modal")
}

// TestThreadChordTeArchivesThread — `T e` reaches the same chord
// arm as `T a`. The dispatch test fixture has no Thread executor
// wired so runThreadMoveCmd returns nil; the assertion is
// behavioural — the chord state resets after the second key, which
// only happens if the `e` arm matched.
func TestThreadChordTeArchivesThread(t *testing.T) {
	m := newDispatchTestModel(t)
	m.list.SetMessages([]store.Message{{ID: "m-thread", ConversationID: "c-thread", Subject: "x"}})
	m.focused = ListPane

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	m = out.(Model)
	require.True(t, m.threadChordPending)
	require.Contains(t, m.engineActivity, "/a/e/")

	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = out.(Model)
	require.False(t, m.threadChordPending, "T e must consume the chord (same as T a)")
}

// TestChordPendingHintShowsAEGlyphs — the chord-pending status
// string includes both glyphs.
func TestChordPendingHintShowsAEGlyphs(t *testing.T) {
	m := newDispatchTestModel(t)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	m = out.(Model)
	require.Contains(t, m.engineActivity, "r/R/f/F/d/D/a/e/m/l/L/s/S")
}

// TestViewerEDoesNotToggleQuotes — regression for spec 30 §3.1
// removal: pressing `e` in the viewer no longer toggles quote
// expansion.
func TestViewerEDoesNotToggleQuotes(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Triage = stubTriageArchive{called: new([]string)}
	msg := store.Message{ID: "m-viewer", FromAddress: "x@example.invalid", Subject: "y"}
	m.viewer.SetMessage(msg)
	m.viewer.current = &msg
	m.focused = ViewerPane
	collapsed := "intro\n[… 2 quoted lines]\noutro\n"
	expanded := "intro\n> q1\n> q2\noutro\n"
	m.viewer.SetBody(collapsed, expanded, 1)
	require.False(t, m.viewer.QuotesExpanded())

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = out.(Model)
	require.False(t, m.viewer.QuotesExpanded(), "e is now archive in viewer pane; Q remains the quote-toggle key")
}

// TestViewerQTogglesQuotes — canonical Q binding still works.
func TestViewerQTogglesQuotes(t *testing.T) {
	m := newDispatchTestModel(t)
	msg := store.Message{ID: "m-q", FromAddress: "x@example.invalid", Subject: "y"}
	m.viewer.SetMessage(msg)
	m.viewer.current = &msg
	m.focused = ViewerPane
	m.viewer.SetBody("intro\n[…]\nout", "intro\n> q\nout", 1)
	require.False(t, m.viewer.QuotesExpanded())

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Q")})
	m = out.(Model)
	require.True(t, m.viewer.QuotesExpanded(), "Q remains the canonical quote-toggle key")
}

// helpers ---------------------------------------------------------

var errSentinel = sentinelErr("archive failed")

type sentinelErr string

func (s sentinelErr) Error() string { return string(s) }

// findPaletteRow looks up a row by its ID in a slice of palette
// rows. Returns the empty row on miss to avoid nil deref in
// assertion-style tests.
func findPaletteRow(rows []PaletteRow, id string) PaletteRow {
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	return PaletteRow{}
}
