package ui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// TestScreenerYAcceptsToImbox drives `Y` against a Screener-pane
// row and asserts routeCmd dispatches the address to imbox.
// Spec 28 §5.4 / §10 (TestScreenerPaneYAcceptsToImbox).
func TestScreenerYAcceptsToImbox(t *testing.T) {
	m := newDispatchTestModel(t)
	m.screenerEnabled = true
	m.list.FolderID = screenerSentinelID
	// Ensure list focus is on a pending sender.
	require.NoError(t, m.deps.Store.UpsertMessage(context.Background(), store.Message{
		ID: "m-pending", AccountID: m.deps.Account.ID, FolderID: "f-inbox",
		FromAddress: "news@example.invalid", FromName: "News",
		Subject: "hi", ReceivedAt: time.Now(),
	}))
	m.list.SetMessages([]store.Message{{
		ID: "m-pending", AccountID: m.deps.Account.ID, FromAddress: "news@example.invalid",
		Subject: "hi", ReceivedAt: time.Now(),
	}})

	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
	m = out.(Model)
	require.NotNil(t, cmd, "Y must dispatch routeCmd")
	require.Empty(t, m.engineActivity, "Y on a focused row must not surface the no-from-address toast")
}

// TestScreenerNRejectsToScreener mirrors the above for `N`.
func TestScreenerNRejectsToScreener(t *testing.T) {
	m := newDispatchTestModel(t)
	m.screenerEnabled = true
	m.list.FolderID = screenerSentinelID
	m.list.SetMessages([]store.Message{{
		ID: "m-pending", AccountID: m.deps.Account.ID, FromAddress: "news@example.invalid",
		Subject: "hi", ReceivedAt: time.Now(),
	}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	require.NotNil(t, cmd, "N must dispatch routeCmd")
}

// TestScreenerYOutsideScreenerIsNoop asserts pane scoping: pressing
// Y in the regular Inbox view does NOT call routeCmd. The dispatch
// returns no command and no toast.
func TestScreenerYOutsideScreenerIsNoop(t *testing.T) {
	m := newDispatchTestModel(t)
	m.screenerEnabled = true
	m.list.FolderID = "f-inbox" // NOT screenerSentinelID
	m.list.SetMessages([]store.Message{{
		ID: "m-1", AccountID: m.deps.Account.ID, FromAddress: "alice@example.invalid",
		Subject: "x", ReceivedAt: time.Now(),
	}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
	require.Nil(t, cmd, "Y outside Screener pane must not dispatch")
}

// TestScreenerYWhenGateOffIsNoop confirms that when the gate is
// disabled the pane-scoped Y/N do not fire even on the screener
// sentinel folder (the gate-off content is the spec 23 v1 routed
// senders' mail, where Y/N would be wrong).
func TestScreenerYWhenGateOffIsNoop(t *testing.T) {
	m := newDispatchTestModel(t)
	m.screenerEnabled = false
	m.list.FolderID = screenerSentinelID
	m.list.SetMessages([]store.Message{{
		ID: "m-1", AccountID: m.deps.Account.ID, FromAddress: "alice@example.invalid",
		Subject: "x", ReceivedAt: time.Now(),
	}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
	require.Nil(t, cmd)
}

// TestScreenerYNoFromAddressToasts confirms the §5.6 friendly-error
// path when the focused row has no from-address.
func TestScreenerYNoFromAddressToasts(t *testing.T) {
	m := newDispatchTestModel(t)
	m.screenerEnabled = true
	m.list.FolderID = screenerSentinelID
	m.list.SetMessages([]store.Message{{
		ID: "m-empty", AccountID: m.deps.Account.ID, FromAddress: "",
		Subject: "x", ReceivedAt: time.Now(),
	}})
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
	m = out.(Model)
	require.Nil(t, cmd)
	require.Contains(t, m.engineActivity, "no from-address")
}

// TestScreenerCmdBarListNavigates verifies `:screener list` parks
// the list pane on the screener sentinel folder.
func TestScreenerCmdBarListNavigates(t *testing.T) {
	m := newDispatchTestModel(t)
	out, _ := m.dispatchScreener([]string{"list"})
	m = out.(Model)
	require.Equal(t, screenerSentinelID, m.list.FolderID)
}

// TestScreenerCmdBarStatusToasts verifies `:screener status` writes
// an engineActivity hint containing the gate state.
func TestScreenerCmdBarStatusToasts(t *testing.T) {
	m := newDispatchTestModel(t)
	m.screenerEnabled = true
	m.screenerGrouping = "sender"
	out, _ := m.dispatchScreener([]string{"status"})
	m = out.(Model)
	require.Contains(t, m.engineActivity, "screener:")
	require.Contains(t, m.engineActivity, "enabled=true")
	require.Contains(t, m.engineActivity, "grouping=sender")
}

// TestScreenerCmdBarAcceptDispatches verifies `:screener accept
// <addr>` dispatches a routeCmd to imbox with the bare address.
func TestScreenerCmdBarAcceptDispatches(t *testing.T) {
	m := newDispatchTestModel(t)
	_, cmd := m.dispatchScreener([]string{"accept", "news@example.invalid"})
	require.NotNil(t, cmd)
	// The cmd produces a routedMsg or routeNoopMsg via the live
	// store. Run it inline; we only care that it didn't error.
	res := cmd()
	switch res.(type) {
	case routedMsg, routeNoopMsg:
		// expected
	default:
		t.Fatalf("unexpected msg type %T", res)
	}
	dest, err := m.deps.Store.GetSenderRouting(context.Background(), m.deps.Account.ID, "news@example.invalid")
	require.NoError(t, err)
	require.Equal(t, "imbox", dest)
}

// TestScreenerCmdBarAcceptRejectsScreenerDest verifies the --to=
// screener guard: spec 28 §7.1 routes that to `:screener reject`.
func TestScreenerCmdBarAcceptRejectsScreenerDest(t *testing.T) {
	m := newDispatchTestModel(t)
	out, _ := m.dispatchScreener([]string{"accept", "news@example.invalid", "--to", "screener"})
	m = out.(Model)
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "use `:screener reject`")
}

// TestScreenerCmdBarRejectDispatches mirrors the accept path for the
// reject verb.
func TestScreenerCmdBarRejectDispatches(t *testing.T) {
	m := newDispatchTestModel(t)
	_, cmd := m.dispatchScreener([]string{"reject", "news@example.invalid"})
	require.NotNil(t, cmd)
	cmd() // resolve to apply the routing
	dest, err := m.deps.Store.GetSenderRouting(context.Background(), m.deps.Account.ID, "news@example.invalid")
	require.NoError(t, err)
	require.Equal(t, "screener", dest)
}

// TestScreenerCmdBarHistoryGatedOnEnabled covers the gate-off path
// of `:screener history` — refuses navigation with a friendly toast.
func TestScreenerCmdBarHistoryGatedOnEnabled(t *testing.T) {
	m := newDispatchTestModel(t)
	m.screenerEnabled = false
	out, _ := m.dispatchScreener([]string{"history"})
	m = out.(Model)
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "[screener].enabled = true")
}

// TestScreenerSidebarStateGateOff verifies that when the gate is off
// the sidebar renders only the four spec 23 streams.
func TestScreenerSidebarStateGateOff(t *testing.T) {
	var fm FoldersModel
	fm.SetFolders([]store.Folder{{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"}})
	fm.SetStreamCounts(map[string]int{"imbox": 0, "feed": 0, "paper_trail": 0, "screener": 5})
	fm.SetScreenerSidebarState(false, 0, 0)
	streams := 0
	hasScreenedOut := false
	for _, it := range fm.items {
		if it.isStream && !it.streamIsHeader {
			streams++
			if it.streamSentinel == screenedOutSentinelID {
				hasScreenedOut = true
			}
		}
	}
	require.Equal(t, 4, streams, "gate off → exactly four spec 23 streams")
	require.False(t, hasScreenedOut, "gate off → no __screened_out__ entry")
}

// TestScreenerSidebarStateGateOnRendersScreenedOut covers the spec 28
// §5.2 rendering rule: __screened_out__ appears only when the gate
// is on.
func TestScreenerSidebarStateGateOnRendersScreenedOut(t *testing.T) {
	var fm FoldersModel
	fm.SetFolders([]store.Folder{{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"}})
	fm.SetStreamCounts(map[string]int{"imbox": 1, "feed": 2, "paper_trail": 3, "screener": 4})
	fm.SetScreenerSidebarState(true, 12, 8)
	hasScreenedOut := false
	var screenerCount int
	for _, it := range fm.items {
		if it.isStream && !it.streamIsHeader {
			if it.streamSentinel == screenedOutSentinelID {
				hasScreenedOut = true
				require.Equal(t, 8, it.streamCount, "Screened-Out count")
			}
			if it.streamDestination == "screener" && it.streamSentinel == "" {
				screenerCount = it.streamCount
			}
		}
	}
	require.True(t, hasScreenedOut, "gate on → __screened_out__ entry rendered")
	require.Equal(t, 12, screenerCount, "Screener count source flips to pendingSenderCount when gate on")
}

// TestScreenerGateFlipModalRendersConfirm verifies the
// screenerGateFlipModalMsg handler enters ConfirmMode with the
// expected modal copy.
func TestScreenerGateFlipModalRendersConfirm(t *testing.T) {
	m := newDispatchTestModel(t)
	out, _ := m.Update(screenerGateFlipModalMsg{MessagesFromPending: 17, PendingSenders: 5})
	m = out.(Model)
	require.Equal(t, ConfirmMode, m.mode)
	require.Contains(t, m.confirm.Message, "Enable Screener?")
	require.Contains(t, m.confirm.Message, "17 messages from 5 senders")
	require.Equal(t, "screener_gate_flip", m.confirm.Topic)
}

// TestScreenerGateFlipDeclineKeepsGateOff covers the N branch:
// m.screenerEnabled goes false, marker stays unchanged so the modal
// re-fires next launch.
func TestScreenerGateFlipDeclineKeepsGateOff(t *testing.T) {
	m := newDispatchTestModel(t)
	m.screenerEnabled = true
	out, _ := m.Update(ConfirmResultMsg{Topic: "screener_gate_flip", Confirm: false})
	m = out.(Model)
	require.False(t, m.screenerEnabled, "N must flip the gate off for the session")
	require.False(t, m.screenerLastSeenEnabled, "marker stays false; modal re-fires next launch")
}

// TestPaletteScreenerRowsRegistered verifies the palette includes
// the spec 28 §5.9 rows.
func TestPaletteScreenerRowsRegistered(t *testing.T) {
	m := newDispatchTestModel(t)
	rows := buildScreenerPaletteRows(&m, &store.Message{FromAddress: "alice@example.invalid"})
	have := map[string]bool{}
	for _, r := range rows {
		have[r.ID] = true
	}
	require.True(t, have["screener_accept"])
	require.True(t, have["screener_reject"])
	require.True(t, have["screener_open"])
	require.False(t, have["screener_history"], "screener_history hidden when gate off")
}

// TestPaletteScreenerHistoryAppearsWhenGateOn verifies the
// gate-conditioned history row.
func TestPaletteScreenerHistoryAppearsWhenGateOn(t *testing.T) {
	m := newDispatchTestModel(t)
	m.screenerEnabled = true
	rows := buildScreenerPaletteRows(&m, &store.Message{FromAddress: "alice@example.invalid"})
	have := map[string]bool{}
	for _, r := range rows {
		have[r.ID] = true
	}
	require.True(t, have["screener_history"])
}

// TestPaletteScreenerTitleSwapsWhenGateOff confirms the gate-off
// title rewrite per spec 28 §5.9.
func TestPaletteScreenerTitleSwapsWhenGateOff(t *testing.T) {
	m := newDispatchTestModel(t)
	m.screenerEnabled = false
	rows := buildScreenerPaletteRows(&m, &store.Message{FromAddress: "alice@example.invalid"})
	for _, r := range rows {
		if r.ID == "screener_accept" {
			require.Contains(t, r.Title, "Route focused sender")
		}
	}
}
