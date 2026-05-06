package ui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// mockThreadExecutor captures calls to ThreadExecute and ThreadMove.
type mockThreadExecutor struct {
	executeVerb   store.ActionType
	executeMsgID  string
	moveMsgID     string
	moveDestAlias string
}

func (m *mockThreadExecutor) ThreadExecute(_ context.Context, _ int64, verb store.ActionType, focusedMsgID string) (int, []BulkResult, error) {
	m.executeVerb = verb
	m.executeMsgID = focusedMsgID
	return 1, []BulkResult{{MessageID: focusedMsgID}}, nil
}

func (m *mockThreadExecutor) ThreadMove(_ context.Context, _ int64, focusedMsgID, _ string, destAlias string) (int, []BulkResult, error) {
	m.moveMsgID = focusedMsgID
	m.moveDestAlias = destAlias
	return 1, []BulkResult{{MessageID: focusedMsgID}}, nil
}

// newThreadTestModel returns a dispatch-test model with a message that
// has a ConversationID (required for thread chord ops).
func newThreadTestModel(t *testing.T) (Model, *mockThreadExecutor) {
	t.Helper()
	mock := &mockThreadExecutor{}
	base := newDispatchTestModel(t)

	// Wire the mock thread executor.
	base.deps.Thread = mock

	// Update message m-1 to have a ConversationID so T chord can operate.
	var acc int64
	if base.deps.Account != nil {
		acc = base.deps.Account.ID
	}
	err := base.deps.Store.UpsertMessage(context.Background(), store.Message{
		ID:             "m-1",
		AccountID:      acc,
		FolderID:       "f-inbox",
		Subject:        "Q4 forecast",
		ConversationID: "conv-q4",
		FromAddress:    "alice@example.invalid",
		ReceivedAt:     time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)

	// Reload so the in-memory list reflects the updated row.
	loadCmd := base.loadMessagesCmd(base.list.FolderID)
	msg := loadCmd()
	m2, _ := base.Update(msg)
	m := m2.(Model)
	return m, mock
}

// TestThreadChordTPendingState verifies that pressing T in the list pane
// sets threadChordPending and shows the chord hint in the status bar.
func TestThreadChordTPendingState(t *testing.T) {
	m, _ := newThreadTestModel(t)
	require.Equal(t, ListPane, m.focused)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	nm := updated.(Model)
	require.True(t, nm.threadChordPending, "T press must set threadChordPending=true")
	require.Contains(t, nm.engineActivity, "thread:", "status bar must show chord hint")
}

// TestThreadChordEscCancels verifies that Esc while threadChordPending
// clears the pending state.
func TestThreadChordEscCancels(t *testing.T) {
	m, _ := newThreadTestModel(t)
	m.focused = ListPane
	m.threadChordPending = true
	m.threadChordToken = 1

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	nm := updated.(Model)
	require.False(t, nm.threadChordPending, "Esc must clear threadChordPending")
}

// TestThreadChordTimeoutNoop verifies that a stale timeout (old token)
// does not clear an active chord pending state.
func TestThreadChordTimeoutNoop(t *testing.T) {
	m, _ := newThreadTestModel(t)
	m.threadChordPending = true
	m.threadChordToken = 2 // token 2 is active; token 1 is stale

	updated, _ := m.Update(threadChordTimeoutMsg{token: 1})
	nm := updated.(Model)
	require.True(t, nm.threadChordPending, "stale timeout must not clear active pending state")
}

// TestThreadChordArArchivesThread verifies that T+a in the list pane
// dispatches a cmd that calls ThreadMove with destAlias="archive".
func TestThreadChordArArchivesThread(t *testing.T) {
	m, mock := newThreadTestModel(t)
	m.focused = ListPane
	m.threadChordPending = true
	m.threadChordToken = 1

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	require.NotNil(t, cmd, "T+a must dispatch a cmd")
	// Execute the cmd synchronously.
	result := cmd()
	_ = result
	require.Equal(t, "m-1", mock.moveMsgID, "ThreadMove must be called with the focused message ID")
	require.Equal(t, "archive", mock.moveDestAlias, "T+a must archive (destAlias=archive)")
}

// TestThreadChordDdOpensConfirm verifies that T+d dispatches a
// pre-fetch cmd (not nil).
func TestThreadChordDdOpensConfirm(t *testing.T) {
	m, _ := newThreadTestModel(t)
	m.focused = ListPane
	m.threadChordPending = true
	m.threadChordToken = 1

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.NotNil(t, cmd, "T+d must dispatch a pre-fetch cmd")
}

// TestThreadChordTmOpensFolderPicker verifies that T+m activates
// FolderPickerMode and sets pendingThreadMove.
func TestThreadChordTmOpensFolderPicker(t *testing.T) {
	m, _ := newThreadTestModel(t)
	m.focused = ListPane
	m.threadChordPending = true
	m.threadChordToken = 1
	// Seed folders so the picker has something to show.
	m.folders.raw = []store.Folder{
		{ID: "f-inbox", DisplayName: "Inbox"},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	nm := updated.(Model)
	require.True(t, nm.pendingThreadMove, "T+m must set pendingThreadMove=true")
	require.Equal(t, FolderPickerMode, nm.mode, "T+m must activate FolderPickerMode")
}
