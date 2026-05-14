package render

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/graph"
)

// inviteFixture returns a meetingRequest invite with one of every
// attendee status the breakdown line counts.
func inviteFixture() *Invite {
	return &Invite{
		MessageID:          "msg-1",
		MeetingMessageType: "meetingRequest",
		Event: &InviteEvent{
			ID:               "ev-1",
			Subject:          "Sprint planning",
			Start:            time.Date(2026, 5, 20, 22, 0, 0, 0, time.UTC), // 15:00 PDT
			End:              time.Date(2026, 5, 20, 23, 0, 0, 0, time.UTC), // 16:00 PDT
			IsAllDay:         false,
			Location:         "Conference room B",
			OnlineJoinURL:    "https://teams.example.invalid/abc",
			OrganizerName:    "Bob",
			OrganizerAddress: "bob@example.invalid",
			ResponseStatus:   "notResponded",
			WebLink:          "https://outlook.example.invalid/event/ev-1",
			Recurrence:       "Weekly on Monday",
			Required: []InviteAttendee{
				{Name: "Carol", Status: "accepted"},
				{Name: "Dan", Status: "tentativelyAccepted"},
				{Name: "Eve", Status: "declined"},
				{Name: "Frank", Status: "notResponded"},
				{Name: "Grace", Status: "notResponded"},
			},
			Optional: []InviteAttendee{
				{Name: "Heidi", Status: "accepted"},
				{Name: "Ivan", Status: "notResponded"},
			},
		},
	}
}

func TestRenderInviteCardNilReturnsEmpty(t *testing.T) {
	require.Equal(t, "", RenderInviteCard(nil, time.Time{}, time.UTC, 80))
}

func TestRenderInviteCardWidthTooSmallReturnsEmpty(t *testing.T) {
	require.Equal(t, "", RenderInviteCard(inviteFixture(), time.Time{}, time.UTC, 10))
}

func TestRenderInviteCardMeetingRequestContainsKeyLines(t *testing.T) {
	out := RenderInviteCard(inviteFixture(), time.Time{}, time.UTC, 80)
	require.NotEmpty(t, out)

	wantSubstrings := []string{
		"📅 Meeting invite",
		"⚪",
		"not responded",
		"When:",
		"2026-05-20",
		"22:00–23:00", // UTC
		"Where:",
		"Conference room B",
		"💻 join",
		"Recurs:",
		"Weekly on Monday",
		"Organizer:",
		"Bob <bob@example.invalid>",
		"Required:",
		"5 (1 accepted · 1 tentative · 1 declined · 2 pending)",
		"Optional:",
		"2",
		"Press o to open in Outlook web",
	}
	for _, s := range wantSubstrings {
		require.Contains(t, out, s, "want substring %q in:\n%s", s, out)
	}
}

func TestRenderInviteCardMeetingCancelledAdjustsHeaderAndWhen(t *testing.T) {
	inv := inviteFixture()
	inv.MeetingMessageType = "meetingCancelled"
	out := RenderInviteCard(inv, time.Time{}, time.UTC, 80)
	require.Contains(t, out, "🚫 Meeting cancelled")
	require.Contains(t, out, "(cancelled)")
	require.Contains(t, out, "Press o to open in Outlook web")
}

func TestRenderInviteCardEmptyLocationOmitsWhereLine(t *testing.T) {
	inv := inviteFixture()
	inv.Event.Location = ""
	inv.Event.OnlineJoinURL = ""
	out := RenderInviteCard(inv, time.Time{}, time.UTC, 80)
	require.NotContains(t, out, "Where:")
}

func TestRenderInviteCardOnlineOnlyShowsJoinOnly(t *testing.T) {
	inv := inviteFixture()
	inv.Event.Location = ""
	out := RenderInviteCard(inv, time.Time{}, time.UTC, 80)
	require.Contains(t, out, "Where: 💻 join")
}

func TestRenderInviteCardAllDayOmitsTimes(t *testing.T) {
	inv := inviteFixture()
	inv.Event.IsAllDay = true
	out := RenderInviteCard(inv, time.Time{}, time.UTC, 80)
	require.Contains(t, out, "· all day")
	require.NotContains(t, out, "22:00")
}

func TestRenderInviteCardWidthCollapsesBreakdown(t *testing.T) {
	inv := inviteFixture()
	out := RenderInviteCard(inv, time.Time{}, time.UTC, 38)
	// Narrow card: required line collapses to bare count.
	require.NotContains(t, out, "accepted ·",
		"narrow card must NOT contain the parenthetical breakdown:\n%s", out)
	require.Contains(t, out, "Required:")
}

func TestRenderInviteCardResponseTypeAccepted(t *testing.T) {
	inv := &Invite{
		MeetingMessageType: "meetingAccepted",
	}
	sentAt := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	out := RenderInviteCard(inv, sentAt, time.UTC, 80)
	require.Contains(t, out, "✅ Response: accepted")
	require.Contains(t, out, "sent Wed 2026-05-20")
	require.NotContains(t, out, "Press o to open",
		"response-type cards must NOT show the hand-off hint")
}

func TestRenderInviteCardResponseTypeTentativeMatchesGraphTypo(t *testing.T) {
	// Microsoft's wire-format value is `meetingTenativelyAccepted`
	// (Tenatively, sic). Spec 34 §4 preserves it verbatim.
	inv := &Invite{MeetingMessageType: "meetingTenativelyAccepted"}
	out := RenderInviteCard(inv, time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC), time.UTC, 80)
	require.Contains(t, out, "🟡 Response: tentative")
}

func TestRenderInviteCardResponseTypeDeclined(t *testing.T) {
	inv := &Invite{MeetingMessageType: "meetingDeclined"}
	out := RenderInviteCard(inv, time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC), time.UTC, 80)
	require.Contains(t, out, "❌ Response: declined")
}

func TestRenderInviteCardUnrecognisedMeetingTypeReturnsEmpty(t *testing.T) {
	inv := &Invite{MeetingMessageType: "meetingFutureNewType"}
	out := RenderInviteCard(inv, time.Now(), time.UTC, 80)
	require.Equal(t, "", out)
}

func TestRenderInviteCardOrganizerVariants(t *testing.T) {
	inv := inviteFixture()
	inv.Event.OrganizerName = ""
	out := RenderInviteCard(inv, time.Time{}, time.UTC, 80)
	require.Contains(t, out, "Organizer: bob@example.invalid")

	inv.Event.OrganizerAddress = ""
	inv.Event.OrganizerName = "Bob"
	out = RenderInviteCard(inv, time.Time{}, time.UTC, 80)
	require.Contains(t, out, "Organizer: Bob")
}

func TestRenderInviteCardStatusPipsAllVariants(t *testing.T) {
	cases := []struct {
		status   string
		wantPip  string
		wantText string
	}{
		{"accepted", "🟢", "accepted"},
		{"tentativelyAccepted", "🟡", "tentative"},
		{"declined", "🔴", "declined"},
		{"notResponded", "⚪", "not responded"},
		{"organizer", "◆", "you are the organizer"},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			inv := inviteFixture()
			inv.Event.ResponseStatus = tc.status
			out := RenderInviteCard(inv, time.Time{}, time.UTC, 80)
			require.Contains(t, out, tc.wantPip)
			require.Contains(t, out, tc.wantText)
		})
	}
}

func TestRenderInviteCardNoEventStillHandlesGracefully(t *testing.T) {
	// Spec 34 §6.2: response handling allows Event = nil on a 200.
	inv := &Invite{MeetingMessageType: "meetingRequest", Event: nil}
	out := RenderInviteCard(inv, time.Time{}, time.UTC, 80)
	// Header + hand-off hint still render even without body details.
	require.Contains(t, out, "📅 Meeting invite")
	require.Contains(t, out, "Press o to open in Outlook web")
}

func TestHasExpandableEvent(t *testing.T) {
	require.True(t, HasExpandableEvent("meetingRequest"))
	require.True(t, HasExpandableEvent("meetingCancelled"))
	require.False(t, HasExpandableEvent("meetingAccepted"))
	require.False(t, HasExpandableEvent("meetingTenativelyAccepted"))
	require.False(t, HasExpandableEvent("meetingDeclined"))
	require.False(t, HasExpandableEvent(""))
	require.False(t, HasExpandableEvent("none"))
}

func TestInviteFromGraphCopiesFields(t *testing.T) {
	g := &graph.EventMessage{
		MessageID:          "m1",
		MeetingMessageType: "meetingRequest",
		Event: &graph.EventMessageEvent{
			Subject:        "X",
			ResponseStatus: "accepted",
			Recurrence:     "Daily",
			Required:       []graph.EventAttendee{{Name: "A", Type: "required"}},
			Optional:       []graph.EventAttendee{{Name: "B", Type: "optional"}},
		},
	}
	inv := InviteFromGraph(g)
	require.NotNil(t, inv)
	require.Equal(t, "m1", inv.MessageID)
	require.Equal(t, "meetingRequest", inv.MeetingMessageType)
	require.NotNil(t, inv.Event)
	require.Equal(t, "Daily", inv.Event.Recurrence)
	require.Len(t, inv.Event.Required, 1)
	require.Equal(t, "A", inv.Event.Required[0].Name)
	require.Len(t, inv.Event.Optional, 1)
	require.Equal(t, "B", inv.Event.Optional[0].Name)
}

func TestInviteFromGraphNilSafe(t *testing.T) {
	require.Nil(t, InviteFromGraph(nil))
	inv := InviteFromGraph(&graph.EventMessage{MeetingMessageType: "meetingAccepted"})
	require.NotNil(t, inv)
	require.Nil(t, inv.Event)
}

// BenchmarkRenderInviteCard covers spec 34 §9 perf budget:
// <500µs for a 50-attendee event.
func BenchmarkRenderInviteCard(b *testing.B) {
	inv := inviteFixture()
	// Pad required to 50 attendees with a mix of statuses.
	statuses := []string{"accepted", "tentativelyAccepted", "declined", "notResponded"}
	inv.Event.Required = inv.Event.Required[:0]
	for i := 0; i < 50; i++ {
		inv.Event.Required = append(inv.Event.Required, InviteAttendee{
			Name:    fmt.Sprintf("Attendee %d", i),
			Address: fmt.Sprintf("a%d@example.invalid", i),
			Type:    "required",
			Status:  statuses[i%len(statuses)],
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RenderInviteCard(inv, time.Time{}, time.UTC, 80)
	}
}

func TestRenderInviteCardLineCountStable(t *testing.T) {
	// Quick smoke that the card structure doesn't include spurious
	// blank lines. Boxed contents are between the top/bottom rules.
	out := RenderInviteCard(inviteFixture(), time.Time{}, time.UTC, 80)
	lines := strings.Split(out, "\n")
	require.GreaterOrEqual(t, len(lines), 8, "card should have at least 8 lines\n%s", out)
}
