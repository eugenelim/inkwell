package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/eugenelim/inkwell/internal/graph"
)

// Invite is the render-package mirror of [graph.EventMessage]. The
// UI cannot import internal/graph (`docs/CONVENTIONS.md` §2 layering), so the
// CalendarFetcher interface returns *Invite and cmd/inkwell's
// calendarAdapter does the graph→render conversion (see
// [InviteFromGraph]).
//
// Spec 34's prose names *graph.EventMessage as the renderer input;
// this type is behaviourally identical — same fields, same
// nil-tolerant Event subfield.
type Invite struct {
	MessageID          string
	MeetingMessageType string
	Event              *InviteEvent
}

// InviteEvent mirrors [graph.EventMessageEvent].
type InviteEvent struct {
	ID               string
	Subject          string
	Start            time.Time
	End              time.Time
	IsAllDay         bool
	Location         string
	OnlineJoinURL    string
	OrganizerName    string
	OrganizerAddress string
	ResponseStatus   string
	WebLink          string
	Recurrence       string
	Required         []InviteAttendee
	Optional         []InviteAttendee
}

// InviteAttendee mirrors [graph.EventAttendee].
type InviteAttendee struct {
	Name    string
	Address string
	Type    string
	Status  string
}

// InviteFromGraph converts a *graph.EventMessage to a *render.Invite.
// Returns nil when em is nil. Used by the CLI's calendarAdapter to
// satisfy the UI's CalendarFetcher.GetEventMessage interface without
// forcing ui → graph.
func InviteFromGraph(em *graph.EventMessage) *Invite {
	if em == nil {
		return nil
	}
	out := &Invite{
		MessageID:          em.MessageID,
		MeetingMessageType: em.MeetingMessageType,
	}
	if em.Event != nil {
		out.Event = &InviteEvent{
			ID:               em.Event.ID,
			Subject:          em.Event.Subject,
			Start:            em.Event.Start,
			End:              em.Event.End,
			IsAllDay:         em.Event.IsAllDay,
			Location:         em.Event.Location,
			OnlineJoinURL:    em.Event.OnlineJoinURL,
			OrganizerName:    em.Event.OrganizerName,
			OrganizerAddress: em.Event.OrganizerAddress,
			ResponseStatus:   em.Event.ResponseStatus,
			WebLink:          em.Event.WebLink,
			Recurrence:       em.Event.Recurrence,
			Required:         attendeesFromGraph(em.Event.Required),
			Optional:         attendeesFromGraph(em.Event.Optional),
		}
	}
	return out
}

func attendeesFromGraph(in []graph.EventAttendee) []InviteAttendee {
	if len(in) == 0 {
		return nil
	}
	out := make([]InviteAttendee, len(in))
	for i, a := range in {
		out[i] = InviteAttendee{
			Name:    a.Name,
			Address: a.Address,
			Type:    a.Type,
			Status:  a.Status,
		}
	}
	return out
}

// HasExpandableEvent reports whether mt is one of the meeting types
// that carry an expandable event navigation property (meetingRequest
// or meetingCancelled). Response types do not.
func HasExpandableEvent(mt string) bool {
	switch mt {
	case "meetingRequest", "meetingCancelled":
		return true
	}
	return false
}

// RenderInviteCard returns the spec-34 invite metadata card as a
// string ready to paint above the body. Returns "" for em==nil and
// for meetingMessageType values that aren't documented.
//
// sentAt is the parent message's SentDateTime (used for response-
// type headers); tz is the user's resolved IANA timezone (nil →
// UTC); width is the viewer-content cell-width. The card hard-wraps
// at min(width-2, 80).
//
// Width<40 collapses the required-breakdown line to a bare count.
// Width<20 returns "" (no room to render a meaningful card).
func RenderInviteCard(em *Invite, sentAt time.Time, tz *time.Location, width int) string {
	if em == nil {
		return ""
	}
	if width < 20 {
		return ""
	}
	if tz == nil {
		tz = time.UTC
	}
	cardWidth := width - 2
	if cardWidth > 80 {
		cardWidth = 80
	}

	switch em.MeetingMessageType {
	case "meetingRequest", "meetingCancelled":
		return renderFullCard(em, tz, cardWidth)
	case "meetingAccepted", "meetingTenativelyAccepted", "meetingDeclined":
		return renderResponseCard(em.MeetingMessageType, sentAt, tz, cardWidth)
	}
	return ""
}

// renderFullCard produces the meetingRequest / meetingCancelled
// shape: header + multi-line body + hand-off hint.
func renderFullCard(em *Invite, tz *time.Location, width int) string {
	bannerEmoji := "📅"
	bannerLabel := "Meeting invite"
	if em.MeetingMessageType == "meetingCancelled" {
		bannerEmoji = "🚫"
		bannerLabel = "Meeting cancelled"
	}

	header := bannerEmoji + " " + bannerLabel
	if em.Event != nil {
		if pip, label := pipAndLabel(em.Event.ResponseStatus); pip != "" {
			header += " · " + pip + " " + label
		}
	}

	lines := []string{header}

	if em.Event != nil {
		lines = append(lines, "")
		ev := em.Event
		if w := whenLine(ev, em.MeetingMessageType, tz); w != "" {
			lines = append(lines, fieldLine("When:", w))
		}
		if w := whereLine(ev); w != "" {
			lines = append(lines, fieldLine("Where:", w))
		}
		if ev.Recurrence != "" {
			lines = append(lines, fieldLine("Recurs:", ev.Recurrence))
		}
		if org := organizerLine(ev); org != "" {
			lines = append(lines, fieldLine("Organizer:", org))
		}
		req := requiredLine(ev, width)
		if req != "" {
			lines = append(lines, fieldLine("Required:", req))
		}
		if n := len(ev.Optional); n > 0 {
			lines = append(lines, fieldLine("Optional:", fmt.Sprintf("%d", n)))
		}
	}

	// Hand-off hint is the last line for full cards only.
	lines = append(lines, "")
	lines = append(lines, "Press o to open in Outlook web (RSVP there)")

	return boxed(lines, width)
}

// renderResponseCard produces the single-line response-type card.
// emoji + label come from the meetingMessageType; sentAt is the
// parent message's SentDateTime.
func renderResponseCard(mt string, sentAt time.Time, tz *time.Location, width int) string {
	var emoji, label string
	switch mt {
	case "meetingAccepted":
		emoji, label = "✅", "accepted"
	case "meetingTenativelyAccepted":
		emoji, label = "🟡", "tentative"
	case "meetingDeclined":
		emoji, label = "❌", "declined"
	default:
		return ""
	}
	line := fmt.Sprintf("%s Response: %s", emoji, label)
	if !sentAt.IsZero() {
		ts := sentAt.In(tz).Format("Mon 2006-01-02")
		line += " · sent " + ts
	}
	return boxed([]string{line}, width)
}

// fieldLine returns "label   value" with a two-space gap. label is
// not padded — callers can let the box rule absorb misalignment in
// favour of natural-feeling whitespace.
func fieldLine(label, value string) string {
	return label + " " + value
}

func whenLine(ev *InviteEvent, mt string, tz *time.Location) string {
	if ev.Start.IsZero() {
		return ""
	}
	day := ev.Start.In(tz).Format("Mon 2006-01-02")
	if ev.IsAllDay {
		// All-day events are timezone-independent at display time
		// (Graph contract). Omit times and TZ abbrev.
		out := day + " · all day"
		if mt == "meetingCancelled" {
			out += " (cancelled)"
		}
		return out
	}
	startTime := ev.Start.In(tz).Format("15:04")
	endTime := ev.End.In(tz).Format("15:04")
	tzAbbrev, _ := ev.Start.In(tz).Zone()
	out := fmt.Sprintf("%s · %s–%s %s", day, startTime, endTime, tzAbbrev)
	if mt == "meetingCancelled" {
		out += " (cancelled)"
	}
	return out
}

func whereLine(ev *InviteEvent) string {
	loc := strings.TrimSpace(ev.Location)
	if loc != "" && ev.OnlineJoinURL != "" {
		return loc + "  ·  💻 join"
	}
	if loc != "" {
		return loc
	}
	if ev.OnlineJoinURL != "" {
		return "💻 join"
	}
	return ""
}

func organizerLine(ev *InviteEvent) string {
	name := strings.TrimSpace(ev.OrganizerName)
	addr := strings.TrimSpace(ev.OrganizerAddress)
	switch {
	case name != "" && addr != "":
		return name + " <" + addr + ">"
	case addr != "":
		return addr
	case name != "":
		return name
	}
	return ""
}

// requiredLine returns the "5 (1 accepted · 0 tentative · …)" line
// or, at width<40, the bare "5". Empty when there are no required
// attendees.
func requiredLine(ev *InviteEvent, width int) string {
	n := len(ev.Required)
	if n == 0 {
		return ""
	}
	if width < 40 {
		return fmt.Sprintf("%d", n)
	}
	var acc, tent, dec, pend int
	for _, a := range ev.Required {
		switch a.Status {
		case "accepted":
			acc++
		case "tentativelyAccepted":
			tent++
		case "declined":
			dec++
		default:
			// notResponded / none / organizer / "" all count as pending
			// for the purposes of the breakdown.
			pend++
		}
	}
	return fmt.Sprintf("%d (%d accepted · %d tentative · %d declined · %d pending)",
		n, acc, tent, dec, pend)
}

// pipAndLabel returns the status pip + label for the responseStatus
// row. Empty strings for unrecognised / empty values (no pip drawn).
func pipAndLabel(rs string) (pip, label string) {
	switch rs {
	case "accepted":
		return "🟢", "accepted"
	case "tentativelyAccepted":
		return "🟡", "tentative"
	case "declined":
		return "🔴", "declined"
	case "notResponded":
		// ⚪ (white circle), NOT 🟡 — semantically distinct from
		// "tentative" per spec 34 §6.3.
		return "⚪", "not responded"
	case "organizer":
		return "◆", "you are the organizer"
	}
	return "", ""
}

// boxed wraps a list of lines in a lipgloss rounded border at the
// supplied cell-width. Terminals that lack the unicode box-drawing
// glyphs fall back to ASCII via lipgloss's own degradation.
func boxed(lines []string, width int) string {
	body := strings.Join(lines, "\n")
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(width - 2) // -2 for the left/right border columns
	return style.Render(body)
}
