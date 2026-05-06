package ui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// newCrossfolderTestModel returns a dispatch-test model pre-seeded with
// messages in two folders (f-inbox and f-sent) so that a cross-folder
// filter can return results from both.
func newCrossfolderTestModel(t *testing.T) Model {
	t.Helper()
	m := newDispatchTestModel(t)
	// Seed f-sent folder and two messages in it.
	acc := m.deps.Account.ID
	require.NoError(t, m.deps.Store.UpsertFolder(context.Background(), store.Folder{
		ID:            "f-sent",
		AccountID:     acc,
		DisplayName:   "Sent Items",
		WellKnownName: "sentitems",
		LastSyncedAt:  time.Now(),
	}))
	for i, subj := range []string{"Sent reply A", "Sent reply B"} {
		require.NoError(t, m.deps.Store.UpsertMessage(context.Background(), store.Message{
			ID:          "ms-" + string(rune('1'+i)),
			AccountID:   acc,
			FolderID:    "f-sent",
			Subject:     subj,
			FromAddress: "alice@example.invalid",
			FromName:    "Alice",
			ReceivedAt:  time.Now().Add(-time.Duration(i+10) * time.Hour),
		}))
	}
	// Reload folders so foldersByID is populated.
	loadCmd := m.loadFoldersCmd()
	msg := loadCmd()
	m2, _ := m.Update(msg)
	m = m2.(Model)
	return m
}

// TestFilterAllFlagSetsModelField confirms that `:filter --all ~f x`
// sets filterAllFolders=true and passes `~f x` to runFilterCmd.
func TestFilterAllFlagSetsModelField(t *testing.T) {
	m := newDispatchTestModel(t)

	// Enter command mode.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "filter --all ~f x" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)

	require.True(t, m.filterAllFolders, "filterAllFolders must be true after --all prefix")
	require.NotNil(t, cmd, ":filter must return a runFilterCmd")
}

// TestFilterNoPrefixLeavesFieldFalse confirms that `:filter ~f x` (without
// --all) leaves filterAllFolders=false.
func TestFilterNoPrefixLeavesFieldFalse(t *testing.T) {
	m := newDispatchTestModel(t)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "filter ~f *alice*" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)

	require.False(t, m.filterAllFolders, "filterAllFolders must remain false without --all prefix")
	require.NotNil(t, cmd)
}

// TestFilterAllEmptyPatternError confirms that `:filter --all` with no
// pattern after the flag returns a friendly error.
func TestFilterAllEmptyPatternError(t *testing.T) {
	m := newDispatchTestModel(t)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "filter --all" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)

	require.NotNil(t, m.lastError, "empty pattern after --all must set lastError")
	require.Contains(t, m.lastError.Error(), "filter", "error must mention filter")
	require.Nil(t, cmd, "no cmd should be dispatched on empty-pattern error")
}

// TestFilterAllFolderHintShowsFolderCount verifies that when filterAllFolders
// is true and results span >1 folder, the status bar shows "(N folders)".
func TestFilterAllFolderHintShowsFolderCount(t *testing.T) {
	m := newCrossfolderTestModel(t)

	// Apply the filter with --all via dispatchCommand directly.
	m.filterAllFolders = true
	cmd := m.runFilterCmd("~f *alice*")
	require.NotNil(t, cmd)
	msg := cmd()
	applied, ok := msg.(filterAppliedMsg)
	require.True(t, ok, "runFilterCmd must return filterAppliedMsg, got %T", msg)

	// Messages span 2 folders (f-inbox and f-sent).
	m2, _ := m.Update(applied)
	m = m2.(Model)

	require.True(t, m.filterActive)
	require.True(t, m.filterAllFolders)
	require.GreaterOrEqual(t, m.filterFolderCount, 2, "should see ≥2 folders in results")

	// Render the full view and check the cmd bar contains the folder count hint.
	view := m.View()
	require.Contains(t, view, "folders)", "status bar must show folder count hint")
}

// TestFilterAllFolderColumnRendered verifies that the list pane renders a
// FOLDER column header when folderNameByID is non-nil (cross-folder result).
func TestFilterAllFolderColumnRendered(t *testing.T) {
	m := newCrossfolderTestModel(t)

	m.filterAllFolders = true
	cmd := m.runFilterCmd("~f *alice*")
	msg := cmd()
	applied, ok := msg.(filterAppliedMsg)
	require.True(t, ok)
	m2, _ := m.Update(applied)
	m = m2.(Model)

	require.NotNil(t, m.list.folderNameByID, "folderNameByID must be populated")

	// Render the list pane directly.
	listView := m.list.View(m.theme, m.paneWidths.List, 20, true)
	require.Contains(t, listView, "FOLDER", "FOLDER column header must appear in list pane")
}

// TestFilterAllConfirmModalIncludesFolderCount verifies that pressing `;d`
// after a cross-folder filter shows "across N folders" in the confirm modal.
func TestFilterAllConfirmModalIncludesFolderCount(t *testing.T) {
	m := newCrossfolderTestModel(t)
	m.deps.Bulk = stubBulkExecutor{}

	m.filterAllFolders = true
	cmd := m.runFilterCmd("~f *alice*")
	msg := cmd()
	applied, ok := msg.(filterAppliedMsg)
	require.True(t, ok)
	m2, _ := m.Update(applied)
	m = m2.(Model)
	require.GreaterOrEqual(t, m.filterFolderCount, 2)

	// Press ; to enter bulk-pending mode.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
	m = m2.(Model)
	require.True(t, m.bulkPending)

	// Press d to trigger confirm modal.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode, ";d must open ConfirmMode")

	// The confirm modal text must include "across N folders".
	promptText := m.confirm.Message
	require.Contains(t, promptText, "across", "confirm modal must mention cross-folder scope")
	require.Contains(t, promptText, "folders", "confirm modal must show folder count")
}

// TestClearFilterResetsAllFolderFields verifies that clearFilter() zeroes
// out all cross-folder fields including m.list.folderNameByID.
func TestClearFilterResetsAllFolderFields(t *testing.T) {
	m := newDispatchTestModel(t)

	// Simulate an active cross-folder filter.
	m.filterActive = true
	m.filterAllFolders = true
	m.filterFolderCount = 3
	m.filterFolderName = "Inbox"
	m.list.folderNameByID = map[string]string{"f-inbox": "Inbox"}

	m = m.clearFilter()

	require.False(t, m.filterAllFolders, "filterAllFolders must be cleared")
	require.Equal(t, 0, m.filterFolderCount, "filterFolderCount must be zeroed")
	require.Equal(t, "", m.filterFolderName, "filterFolderName must be cleared")
	require.Nil(t, m.list.folderNameByID, "folderNameByID must be nil after clearFilter")
}

// TestFilterHintSingleFolderShowsName verifies that when filterAllFolders is
// true and results land in exactly one folder, the status bar shows the
// folder display name, e.g. "(Inbox)".
func TestFilterHintSingleFolderShowsName(t *testing.T) {
	m := newDispatchTestModel(t)

	// Populate foldersByID manually.
	m.foldersByID = map[string]store.Folder{
		"f-inbox": {ID: "f-inbox", DisplayName: "Inbox"},
	}
	m.filterActive = true
	m.filterAllFolders = true
	m.filterPattern = "~f alice"
	m.filterFolderCount = 1
	m.filterFolderName = "Inbox"
	m.filterIDs = []string{"m-1"}

	view := m.View()
	require.Contains(t, view, "(Inbox)", "status bar must show folder name for single-folder cross-folder filter")
}

// TestConfirmBulkNoFolderSuffixWithoutAllFlag verifies that confirmBulk does
// NOT append "across N folders" when filterAllFolders is false.
func TestConfirmBulkNoFolderSuffixWithoutAllFlag(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Bulk = stubBulkExecutor{}
	m.filterActive = true
	m.filterPattern = "~f alice"
	m.filterIDs = []string{"m-1", "m-2"}
	m.filterAllFolders = false
	m.filterFolderCount = 2 // count is set but flag is false — must NOT appear

	m2, _ := m.confirmBulk("soft_delete", 2)
	nm := m2.(Model)
	require.NotContains(t, nm.confirm.Message, "across", "no folder suffix when filterAllFolders=false")
}

// TestListViewFolderColumnHidden verifies that View() does NOT emit a FOLDER
// column when folderNameByID is nil (normal single-folder view).
func TestListViewFolderColumnHidden(t *testing.T) {
	m := newDispatchTestModel(t)
	require.Nil(t, m.list.folderNameByID)

	listView := m.list.View(m.theme, m.paneWidths.List, 20, true)
	require.NotContains(t, listView, "FOLDER", "FOLDER column must be hidden in normal view")
}

// TestFilterShortFlagSetsAllFolders verifies that `:filter -a <pattern>`
// (short flag) also sets filterAllFolders=true.
func TestFilterShortFlagSetsAllFolders(t *testing.T) {
	m := newDispatchTestModel(t)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "filter -a ~f alice" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)

	require.True(t, m.filterAllFolders, "-a flag must set filterAllFolders=true")
	require.NotNil(t, cmd)
}
