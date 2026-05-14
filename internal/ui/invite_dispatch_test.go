package ui

import (
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// browserSpy is the synchronised recorder for openInBrowser. The
// production call site is `go openInBrowser(...)` (fire-and-forget),
// so the test goroutine must wait for the side effect before
// asserting; done sits at zero until exactly one URL is captured.
type browserSpy struct {
	mu       sync.Mutex
	captured []string
	done     chan struct{}
}

func (s *browserSpy) record(url string) {
	s.mu.Lock()
	s.captured = append(s.captured, url)
	s.mu.Unlock()
	select {
	case s.done <- struct{}{}:
	default:
	}
}

// urls returns a copy of the captured URLs.
func (s *browserSpy) urls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.captured...)
}

// waitOne blocks until at least one openInBrowser call lands or
// times out. Fail-fast on timeout — the test would otherwise hang.
func (s *browserSpy) waitOne(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("openInBrowser was not called within 2s")
	}
}

// withOpenInBrowserSpy swaps the package-level openInBrowser var for
// a recorder so dispatch tests can assert WHICH URL got opened. The
// restore runs via t.Cleanup so individual tests don't defer.
func withOpenInBrowserSpy(t *testing.T) *browserSpy {
	t.Helper()
	spy := &browserSpy{done: make(chan struct{}, 4)}
	prev := openInBrowser
	openInBrowser = spy.record
	t.Cleanup(func() { openInBrowser = prev })
	return spy
}

// setInviteForTest installs an invite snapshot on the viewer + sets
// the focused message; SetMessage clears prior invite state so we
// must SetMessage first.
func setInviteForTest(m *Model, msg store.Message, snap *InviteSnapshot) {
	m.viewer.SetMessage(msg)
	m.viewer.SetInvite(snap)
}

// TestViewerOOpensEventWebLinkOnInvite asserts the spec-34 routing:
// when the focused message is a meetingRequest with a valid event
// webLink, `o` opens the event webLink (not the message webLink).
func TestViewerOOpensEventWebLinkOnInvite(t *testing.T) {
	spy := withOpenInBrowserSpy(t)

	m := newDispatchTestModel(t)
	m.focused = ViewerPane
	m.mode = NormalMode

	msg := store.Message{ID: "msg-1", WebLink: "https://outlook.example.invalid/message/1"}
	setInviteForTest(&m, msg, &InviteSnapshot{
		MeetingMessageType: "meetingRequest",
		EventWebLink:       "https://outlook.example.invalid/event/ev-1",
		Card:               "📅 Meeting invite",
	})

	updated, _ := m.dispatchViewer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	mm := updated.(Model)
	require.Contains(t, mm.engineActivity, "invite",
		"status hint must reflect invite routing")

	spy.waitOne(t)
	urls := spy.urls()
	require.Len(t, urls, 1, "openInBrowser must be called exactly once")
	require.Equal(t, "https://outlook.example.invalid/event/ev-1", urls[0],
		"o on an invite must open event.WebLink, not message.WebLink")
}

func TestViewerOOpensEventWebLinkOnMeetingCancelled(t *testing.T) {
	spy := withOpenInBrowserSpy(t)

	m := newDispatchTestModel(t)
	m.focused = ViewerPane
	m.mode = NormalMode

	msg := store.Message{ID: "msg-c", WebLink: "https://outlook.example.invalid/message/c"}
	setInviteForTest(&m, msg, &InviteSnapshot{
		MeetingMessageType: "meetingCancelled",
		EventWebLink:       "https://outlook.example.invalid/event/ev-c",
		Card:               "🚫 Meeting cancelled",
	})

	_, _ = m.dispatchViewer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	spy.waitOne(t)
	urls := spy.urls()
	require.Len(t, urls, 1)
	require.Equal(t, "https://outlook.example.invalid/event/ev-c", urls[0])
}

// TestViewerOFallsThroughOnNonInvite is the spec-05 regression:
// without an invite snapshot, `o` opens the message webLink.
func TestViewerOFallsThroughOnNonInvite(t *testing.T) {
	spy := withOpenInBrowserSpy(t)

	m := newDispatchTestModel(t)
	m.focused = ViewerPane
	m.mode = NormalMode

	msg := store.Message{ID: "msg-n", WebLink: "https://outlook.example.invalid/message/n"}
	m.viewer.SetMessage(msg)
	require.Nil(t, m.viewer.InviteRouting(), "fixture starts with no invite")

	updated, _ := m.dispatchViewer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	mm := updated.(Model)
	require.NotContains(t, mm.engineActivity, "invite",
		"non-invite path must not use the invite-routing hint")

	spy.waitOne(t)
	urls := spy.urls()
	require.Len(t, urls, 1)
	require.Equal(t, "https://outlook.example.invalid/message/n", urls[0])
}

func TestViewerOFallsThroughOnResponseTypeInvite(t *testing.T) {
	spy := withOpenInBrowserSpy(t)

	m := newDispatchTestModel(t)
	m.focused = ViewerPane
	m.mode = NormalMode

	msg := store.Message{ID: "msg-r", WebLink: "https://outlook.example.invalid/message/r"}
	setInviteForTest(&m, msg, &InviteSnapshot{
		MeetingMessageType: "meetingAccepted",
		EventWebLink:       "",
		Card:               "✅ Response: accepted",
	})

	_, _ = m.dispatchViewer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	spy.waitOne(t)
	urls := spy.urls()
	require.Len(t, urls, 1)
	require.Equal(t, "https://outlook.example.invalid/message/r", urls[0],
		"response-type invites must fall through to message.WebLink")
}

func TestViewerOFallsThroughOnEmptyEventWebLink(t *testing.T) {
	spy := withOpenInBrowserSpy(t)

	m := newDispatchTestModel(t)
	m.focused = ViewerPane
	m.mode = NormalMode

	msg := store.Message{ID: "msg-e", WebLink: "https://outlook.example.invalid/message/e"}
	setInviteForTest(&m, msg, &InviteSnapshot{
		MeetingMessageType: "meetingRequest",
		EventWebLink:       "",
	})

	_, _ = m.dispatchViewer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	spy.waitOne(t)
	urls := spy.urls()
	require.Len(t, urls, 1)
	require.Equal(t, "https://outlook.example.invalid/message/e", urls[0])
}

// TestIsResponseTypeInvite asserts the helper recognises exactly
// the three documented response-type values (spec 34 §4) — and
// preserves Microsoft's wire-format typo `Tenatively`.
func TestIsResponseTypeInvite(t *testing.T) {
	require.True(t, isResponseTypeInvite("meetingAccepted"))
	require.True(t, isResponseTypeInvite("meetingTenativelyAccepted"))
	require.True(t, isResponseTypeInvite("meetingDeclined"))
	require.False(t, isResponseTypeInvite("meetingRequest"))
	require.False(t, isResponseTypeInvite("meetingCancelled"))
	require.False(t, isResponseTypeInvite(""))
	require.False(t, isResponseTypeInvite("none"))
}

// TestViewerSetMessageClearsInviteRouting is the adversarial-review
// blocker fix: pressing `o` on a new non-invite focused message
// after viewing an invite must NOT open the previous meeting's
// event webLink. The Model-side routing snapshot lives on the
// ViewerModel so SetMessage clears it atomically with inviteCard.
func TestViewerSetMessageClearsInviteRouting(t *testing.T) {
	spy := withOpenInBrowserSpy(t)

	m := newDispatchTestModel(t)
	m.focused = ViewerPane
	m.mode = NormalMode

	// Open an invite first.
	inviteMsg := store.Message{ID: "msg-i", WebLink: "https://outlook.example.invalid/message/i"}
	setInviteForTest(&m, inviteMsg, &InviteSnapshot{
		MeetingMessageType: "meetingRequest",
		EventWebLink:       "https://outlook.example.invalid/event/STALE",
	})
	require.NotNil(t, m.viewer.InviteRouting())

	// Navigate to a non-invite. SetMessage must wipe the routing.
	plainMsg := store.Message{ID: "msg-p", WebLink: "https://outlook.example.invalid/message/p"}
	m.viewer.SetMessage(plainMsg)
	require.Nil(t, m.viewer.InviteRouting(),
		"SetMessage must clear the invite-routing snapshot")

	// `o` on the new focused message must open ITS webLink, not
	// the stale event one.
	_, _ = m.dispatchViewer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	spy.waitOne(t)
	urls := spy.urls()
	require.Len(t, urls, 1)
	require.Equal(t, "https://outlook.example.invalid/message/p", urls[0],
		"stale invite routing must not leak across SetMessage")
}

// TestSetMessageClearsInviteCard is the spec-34 §6.1 cleanup
// guarantee: navigating to a new message in the viewer drops the
// previous invite card so the prior meeting's metadata doesn't
// leak onto the next message.
func TestSetMessageClearsInviteCard(t *testing.T) {
	v := NewViewer()
	v.SetMessage(store.Message{ID: "m1"})
	v.SetInvite(&InviteSnapshot{
		MeetingMessageType: "meetingRequest",
		EventWebLink:       "https://outlook.example.invalid/event/x",
		Card:               "📅 Meeting invite\n…",
	})
	require.NotEqual(t, "", v.InviteCard())
	require.NotNil(t, v.InviteRouting())

	v.SetMessage(store.Message{ID: "m2"})
	require.Equal(t, "", v.InviteCard(),
		"SetMessage must wipe the previous invite card")
	require.Nil(t, v.InviteRouting(),
		"SetMessage must wipe the previous invite routing snapshot")
}
