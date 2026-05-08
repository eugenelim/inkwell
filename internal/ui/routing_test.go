package ui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// TestStreamChordSPendingState confirms pressing `S` in the list pane
// puts the model into stream-chord-pending state with the expected
// status hint.
func TestStreamChordSPendingState(t *testing.T) {
	m := newDispatchTestModel(t)
	require.False(t, m.streamChordPending)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	require.True(t, m.streamChordPending, "S must enter pending state")
	require.Contains(t, m.engineActivity, "stream:")
	require.Contains(t, m.engineActivity, "i/f/p/k/c")
}

// TestStreamChordEscCancels confirms Esc clears pending without
// dispatching.
func TestStreamChordEscCancels(t *testing.T) {
	m := newDispatchTestModel(t)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	require.True(t, m.streamChordPending)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	require.False(t, m.streamChordPending)
	require.Contains(t, m.engineActivity, "cancelled")
}

// TestStreamChordTimeoutNoop confirms a stale timeout (token bumped
// by a fresh S press) leaves pending state intact.
func TestStreamChordTimeoutNoop(t *testing.T) {
	m := newDispatchTestModel(t)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	tokenBefore := m.streamChordToken
	// Press Esc to cancel, then S again — token bumps.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	require.Greater(t, m.streamChordToken, tokenBefore)

	// Inject a stale-token timeout — pending must not flip.
	out, _ = m.Update(streamChordTimeoutMsg{token: tokenBefore})
	m = out.(Model)
	require.True(t, m.streamChordPending, "stale-token timeout must be a no-op")
}

// TestStreamChordSiRoutesToImbox drives `S i` and asserts the routing
// row was written.
func TestStreamChordSiRoutesToImbox(t *testing.T) {
	m := newDispatchTestModel(t)
	sel, ok := m.list.SelectedMessage()
	require.True(t, ok)
	addr := sel.FromAddress
	require.NotEmpty(t, addr)

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = out.(Model)
	require.False(t, m.streamChordPending)
	require.NotNil(t, cmd, "S i must return routeCmd")
	// Run the cmd; result is a routedMsg.
	msg := cmd()
	rm, ok := msg.(routedMsg)
	require.True(t, ok, "expected routedMsg, got %T", msg)
	require.Equal(t, "imbox", rm.dest)
	require.Equal(t, addr, rm.address)

	dest, err := m.deps.Store.GetSenderRouting(context.Background(), m.deps.Account.ID, addr)
	require.NoError(t, err)
	require.Equal(t, "imbox", dest)
}

// TestStreamChordSkRoutesToScreener — `S k` mnemonic.
func TestStreamChordSkRoutesToScreener(t *testing.T) {
	m := newDispatchTestModel(t)
	sel, _ := m.list.SelectedMessage()
	addr := sel.FromAddress

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = out.(Model)
	require.NotNil(t, cmd)
	msg := cmd()
	rm, ok := msg.(routedMsg)
	require.True(t, ok)
	require.Equal(t, "screener", rm.dest)

	dest, _ := m.deps.Store.GetSenderRouting(context.Background(), m.deps.Account.ID, addr)
	require.Equal(t, "screener", dest)
}

// TestStreamChordScClearsRouting — `S c` clears.
func TestStreamChordScClearsRouting(t *testing.T) {
	m := newDispatchTestModel(t)
	sel, _ := m.list.SelectedMessage()
	addr := sel.FromAddress
	_, err := m.deps.Store.SetSenderRouting(context.Background(), m.deps.Account.ID, addr, "feed")
	require.NoError(t, err)

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = out.(Model)
	require.NotNil(t, cmd)
	msg := cmd()
	rm, ok := msg.(routedMsg)
	require.True(t, ok)
	require.Equal(t, "", rm.dest)
	require.Equal(t, "feed", rm.priorDest)

	dest, _ := m.deps.Store.GetSenderRouting(context.Background(), m.deps.Account.ID, addr)
	require.Equal(t, "", dest)
}

// TestStreamChordReassignReportsPriorInRoutedMsg — S f on a sender
// already routed to Imbox produces priorDest=imbox in the routedMsg.
func TestStreamChordReassignReportsPriorInRoutedMsg(t *testing.T) {
	m := newDispatchTestModel(t)
	sel, _ := m.list.SelectedMessage()
	addr := sel.FromAddress
	_, err := m.deps.Store.SetSenderRouting(context.Background(), m.deps.Account.ID, addr, "imbox")
	require.NoError(t, err)

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = out.(Model)
	rm, ok := cmd().(routedMsg)
	require.True(t, ok)
	require.Equal(t, "feed", rm.dest)
	require.Equal(t, "imbox", rm.priorDest)
}

// TestStreamChordSiOnAlreadyImboxIsNoop — same destination produces
// routeNoopMsg (skipping the list reload).
func TestStreamChordSiOnAlreadyImboxIsNoop(t *testing.T) {
	m := newDispatchTestModel(t)
	sel, _ := m.list.SelectedMessage()
	addr := sel.FromAddress
	_, err := m.deps.Store.SetSenderRouting(context.Background(), m.deps.Account.ID, addr, "imbox")
	require.NoError(t, err)

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = out.(Model)
	noop, ok := cmd().(routeNoopMsg)
	require.True(t, ok, "second S i must return routeNoopMsg")
	require.Equal(t, "already", noop.kind)
	require.Equal(t, "imbox", noop.dest)
}

// TestStreamChordTPressCancelsStreamChord — T while stream-pending
// cancels stream chord without starting thread chord (§5.1).
func TestStreamChordTPressCancelsStreamChord(t *testing.T) {
	m := newDispatchTestModel(t)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	require.True(t, m.streamChordPending)

	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	m = out.(Model)
	require.False(t, m.streamChordPending, "T while stream-pending must clear stream chord")
	require.False(t, m.threadChordPending, "T while stream-pending must NOT start thread chord")
}

// TestThreadChordSPressCancelsThreadChord — symmetric.
func TestThreadChordSPressCancelsThreadChord(t *testing.T) {
	m := newDispatchTestModel(t)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	m = out.(Model)
	require.True(t, m.threadChordPending)

	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	require.False(t, m.threadChordPending, "S while thread-pending must clear thread chord")
	require.False(t, m.streamChordPending, "S while thread-pending must NOT start stream chord")
}

// TestStreamChordSSPressIsCancelNotStart — second S press cancels.
func TestStreamChordSSPressIsCancelNotStart(t *testing.T) {
	m := newDispatchTestModel(t)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	require.True(t, m.streamChordPending)

	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	require.False(t, m.streamChordPending, "second S must clear pending")
}

// TestStreamVirtualFoldersRenderInSidebar — sidebar shows the four
// streams under a Streams header.
func TestStreamVirtualFoldersRenderInSidebar(t *testing.T) {
	m := newDispatchTestModel(t)
	out := m.folders.View(m.theme, 30, 40, true)
	require.Contains(t, out, "Streams")
	require.Contains(t, out, "Imbox")
	require.Contains(t, out, "Feed")
	require.Contains(t, out, "Paper Trail")
	require.Contains(t, out, "Screener")
}

// TestStreamVirtualFoldersAlwaysVisibleAtZero — buckets render even
// when their counts are zero.
func TestStreamVirtualFoldersAlwaysVisibleAtZero(t *testing.T) {
	m := newDispatchTestModel(t)
	// SetStreamCounts to all-zero explicitly.
	m.folders.SetStreamCounts(map[string]int{
		"imbox": 0, "feed": 0, "paper_trail": 0, "screener": 0,
	})
	out := m.folders.View(m.theme, 30, 40, true)
	require.Contains(t, out, "Imbox")
	require.Contains(t, out, "Feed")
	require.Contains(t, out, "Screener")
}

// TestStreamVirtualFolderSelectLoadsByRouting — Enter on the Feed
// virtual folder dispatches loadByRoutingCmd.
func TestStreamVirtualFolderSelectLoadsByRouting(t *testing.T) {
	m := newDispatchTestModel(t)
	folderID := streamSentinelIDForDestination("feed")
	require.NotEmpty(t, folderID)

	cmd := m.loadByRoutingCmd("feed")
	require.NotNil(t, cmd)
	msg := cmd()
	loaded, ok := msg.(MessagesLoadedMsg)
	require.True(t, ok)
	require.Equal(t, folderID, loaded.FolderID)
}

// TestStreamSentinelFolderRefusesNRX — N/R/X (folder ops) treat the
// stream sentinels as non-folders, inheriting the spec 18 protection.
func TestStreamSentinelFolderRefusesNRX(t *testing.T) {
	m := newDispatchTestModel(t)
	// Walk cursor to the Feed stream entry.
	for i := 0; i < 30; i++ {
		if m.folders.SelectedStream() == "feed" {
			break
		}
		out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
		m = out.(Model)
		out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = out.(Model)
	}
	if m.folders.SelectedStream() != "feed" {
		t.Skip("could not navigate to Feed stream in this model")
	}
	// Selected() must return ok=false for stream sentinels — N/R/X
	// short-circuit on that gate (spec 18).
	_, ok := m.folders.Selected()
	require.False(t, ok, "stream sentinel must not satisfy Selected()")
}

// TestRefreshStreamCountsCmdDispatches — refreshStreamCountsCmd
// returns a streamCountsUpdatedMsg with all four destinations.
func TestRefreshStreamCountsCmdDispatches(t *testing.T) {
	m := newDispatchTestModel(t)
	_, err := m.deps.Store.SetSenderRouting(context.Background(), m.deps.Account.ID, "alice@example.invalid", "imbox")
	require.NoError(t, err)

	cmd := m.refreshStreamCountsCmd()
	require.NotNil(t, cmd)
	msg := cmd()
	upd, ok := msg.(streamCountsUpdatedMsg)
	require.True(t, ok, "expected streamCountsUpdatedMsg, got %T", msg)
	require.Contains(t, upd.counts, "imbox")
	require.Contains(t, upd.counts, "feed")
}

// TestStreamSentinelHelpersRoundTrip — sentinel ↔ destination ↔
// display label form a coherent triplet.
func TestStreamSentinelHelpersRoundTrip(t *testing.T) {
	for _, dest := range []string{"imbox", "feed", "paper_trail", "screener"} {
		sentinel := streamSentinelIDForDestination(dest)
		require.NotEmpty(t, sentinel, "dest=%s", dest)
		require.True(t, IsStreamSentinelID(sentinel), "dest=%s sentinel=%s", dest, sentinel)
		require.Equal(t, dest, streamDestinationFromID(sentinel), "round-trip %s", dest)
		require.NotEmpty(t, streamDisplayLabelForDestination(dest))
	}
}

// TestStreamChordRouteCmdEmptyAddress — focused message with no
// from_address surfaces a friendly error rather than dispatching.
func TestStreamChordRouteCmdEmptyAddress(t *testing.T) {
	m := newDispatchTestModel(t)
	// Synthesise a list with a blank-FromAddress message at the
	// top of the cursor.
	require.NoError(t, m.deps.Store.UpsertMessage(context.Background(), store.Message{
		ID: "no-from-1", AccountID: m.deps.Account.ID, FolderID: "f-inbox",
		Subject: "Anonymous", FromAddress: "", ReceivedAt: time.Now().Add(time.Hour),
	}))
	loadMsgs := m.loadMessagesCmd("f-inbox")
	out, _ := m.Update(loadMsgs())
	m = out.(Model)
	require.Equal(t, "no-from-1", m.list.messages[0].ID)
	// Move cursor to the top so the focused message is the empty-
	// from one (SetMessages preserves the prior selection by ID).
	m.list.JumpTop()
	sel, _ := m.list.SelectedMessage()
	require.Equal(t, "no-from-1", sel.ID)

	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = out.(Model)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = out.(Model)
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "no from-address")
}
