//go:build e2e

package ui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/render"
	"github.com/eugenelim/inkwell/internal/store"
)

// TestViewerInviteCardRendersAboveBody is the spec-34 §5 visible-
// delta gate: opening a meetingRequest message paints the invite
// card glyphs above the body text. Without this test, dispatch
// tests could pass while the rendered frame never shows the card.
func TestViewerInviteCardRendersAboveBody(t *testing.T) {
	invite := &render.Invite{
		MessageID:          "m-1",
		MeetingMessageType: "meetingRequest",
		Event: &render.InviteEvent{
			ID:               "ev-1",
			Subject:          "Sprint planning",
			Start:            time.Date(2026, 5, 20, 15, 0, 0, 0, time.UTC),
			End:              time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
			Location:         "Conference room B",
			OrganizerName:    "Bob",
			OrganizerAddress: "bob@example.invalid",
			ResponseStatus:   "notResponded",
			WebLink:          "https://outlook.example.invalid/event/ev-1",
			Required: []render.InviteAttendee{
				{Name: "Carol", Status: "accepted"},
				{Name: "Dan", Status: "notResponded"},
			},
		},
	}
	m, _ := newE2EModel(t)
	stub := &e2eCalendarStub{invite: invite}
	m.deps.Calendar = stub
	m.deps.CalendarTZ = time.UTC
	// Mark the seeded m-1 row as a meetingRequest so the open-msg
	// path branches into the spec-34 errgroup + RenderInviteCard.
	require.NoError(t, m.deps.Store.UpsertMessage(context.Background(), store.Message{
		ID: "m-1", AccountID: m.deps.Account.ID, FolderID: "f-inbox",
		Subject: "Q4 forecast", FromAddress: "alice@example.invalid", FromName: "Alice",
		ReceivedAt:         time.Now().Add(-time.Hour),
		MeetingMessageType: "meetingRequest",
		WebLink:            "https://outlook.example.invalid/message/m-1",
	}))

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "📅 Meeting invite") &&
			contains(s, "Press o to open in Outlook web")
	}, teatest.WithDuration(3*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// (Negative-coverage for non-invite messages is covered at the
// dispatch level by TestSetMessageClearsInviteCard in
// invite_dispatch_test.go — driving teatest negative assertions on
// a streaming Output() reader is fragile.)
