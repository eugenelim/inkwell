package ui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// newMuteTestModel builds a dispatch-test model seeded with a message
// that has a conversation ID set (required for mute).
func newMuteTestModel(t *testing.T) (Model, store.Store) {
	t.Helper()
	m := newDispatchTestModel(t)
	// The base messages from newDispatchTestModel have no ConversationID.
	// Update the first message to have one so M can operate on it.
	var acc int64
	if m.deps.Account != nil {
		acc = m.deps.Account.ID
	}
	err := m.deps.Store.UpsertMessage(context.Background(), store.Message{
		ID:             "m-1",
		AccountID:      acc,
		FolderID:       "f-inbox",
		Subject:        "Q4 forecast",
		ConversationID: "conv-q4",
		FromAddress:    "alice@example.invalid",
		FromName:       "Alice",
		ReceivedAt:     time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)
	// Reload messages so the in-memory list reflects the updated row.
	loadCmd := m.loadMessagesCmd(m.list.FolderID)
	msg := loadCmd()
	m2, _ := m.Update(msg)
	m = m2.(Model)
	return m, m.deps.Store
}

// TestMuteKeyMutesThread verifies that pressing M on a focused message
// with a ConversationID emits a muteCmd that eventually returns
// mutedToastMsg{nowMuted: true}.
func TestMuteKeyMutesThread(t *testing.T) {
	m, st := newMuteTestModel(t)
	require.Equal(t, ListPane, m.focused)

	// Confirm the focused message has a conversation ID.
	sel, ok := m.list.Selected()
	require.True(t, ok)
	require.NotEmpty(t, sel.ConversationID, "test setup: first message must have a conversationID")

	// Press M — should return a muteCmd.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})
	require.NotNil(t, cmd, "M must return a muteCmd")

	// Execute the cmd to get the result.
	result := cmd()
	toast, ok := result.(mutedToastMsg)
	require.True(t, ok, "muteCmd result must be mutedToastMsg, got %T", result)
	require.NoError(t, toast.err)
	require.True(t, toast.nowMuted, "first press of M must mute the thread")

	// Confirm the store now has the conversation muted.
	var acc int64
	if m.deps.Account != nil {
		acc = m.deps.Account.ID
	}
	muted, err := st.IsConversationMuted(context.Background(), acc, sel.ConversationID)
	require.NoError(t, err)
	require.True(t, muted)
}

// TestMuteKeyUnmutesThread verifies that pressing M a second time on a
// muted conversation un-mutes it (toggle behaviour).
func TestMuteKeyUnmutesThread(t *testing.T) {
	m, st := newMuteTestModel(t)
	sel, ok := m.list.Selected()
	require.True(t, ok)

	var acc int64
	if m.deps.Account != nil {
		acc = m.deps.Account.ID
	}

	// Pre-mute the conversation.
	require.NoError(t, st.MuteConversation(context.Background(), acc, sel.ConversationID))

	// Press M — should now un-mute.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})
	require.NotNil(t, cmd)

	result := cmd()
	toast, ok := result.(mutedToastMsg)
	require.True(t, ok)
	require.NoError(t, toast.err)
	require.False(t, toast.nowMuted, "second M press must unmute the thread")

	muted, err := st.IsConversationMuted(context.Background(), acc, sel.ConversationID)
	require.NoError(t, err)
	require.False(t, muted)
}

// TestMuteKeyNoConvIDShowsError verifies that pressing M on a message
// that has no ConversationID surfaces an error in m.lastError and does
// not return a muteCmd.
func TestMuteKeyNoConvIDShowsError(t *testing.T) {
	m := newDispatchTestModel(t)
	// The base messages have no ConversationID (empty string).
	sel, ok := m.list.Selected()
	require.True(t, ok)
	require.Empty(t, sel.ConversationID, "precondition: test message must have empty ConversationID")

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})
	m = m2.(Model)

	require.Nil(t, cmd, "M on no-conv-id message must not emit a Cmd")
	require.ErrorContains(t, m.lastError, "conversation ID", "must surface a friendly error")
}
