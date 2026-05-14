package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EventMessage is the eventMessage subtype of a Graph message. Only
// the fields the spec 34 InviteCard needs are decoded. Event is the
// navigation-expanded event resource and may be nil for response-type
// messages (meetingAccepted / meetingTenativelyAccepted /
// meetingDeclined) where no event-expand occurs — those messages
// are responses the user sent from another client, and the responder
// does not own the underlying event.
type EventMessage struct {
	MessageID          string
	MeetingMessageType string
	Event              *EventMessageEvent
}

// EventMessageEvent is the navigation-expanded event resource on an
// eventMessage. Field set is a strict subset of [EventDetail] in
// calendar.go plus a pre-computed Recurrence summary line.
type EventMessageEvent struct {
	ID               string
	Subject          string
	Start            time.Time
	End              time.Time
	IsAllDay         bool
	Location         string
	OnlineJoinURL    string
	OrganizerName    string
	OrganizerAddress string
	ResponseStatus   string // accepted | tentativelyAccepted | declined | notResponded | none | organizer
	WebLink          string
	Recurrence       string // human-readable summary; empty for non-recurring
	Required         []EventAttendee
	Optional         []EventAttendee
}

// rawEventMessage decodes
// GET /me/messages/{id}?$select=id,meetingMessageType&$expand=microsoft.graph.eventMessage/event(...).
// The expanded event arrives under the `event` JSON key on the
// eventMessage derived type.
type rawEventMessage struct {
	ID                 string                `json:"id"`
	MeetingMessageType string                `json:"meetingMessageType"`
	Event              *rawEventMessageEvent `json:"event"`
}

type rawEventMessageEvent struct {
	ID        string `json:"id"`
	Subject   string `json:"subject"`
	Organizer struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"organizer"`
	Start struct {
		DateTime string `json:"dateTime"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
	} `json:"end"`
	IsAllDay bool `json:"isAllDay"`
	Location struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	OnlineMeeting struct {
		JoinURL string `json:"joinUrl"`
	} `json:"onlineMeeting"`
	ResponseStatus struct {
		Response string `json:"response"`
	} `json:"responseStatus"`
	WebLink    string             `json:"webLink"`
	Recurrence *rawRecurrence     `json:"recurrence"`
	Attendees  []rawEventAttendee `json:"attendees"`
}

type rawEventAttendee struct {
	Type   string `json:"type"`
	Status struct {
		Response string `json:"response"`
	} `json:"status"`
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

type rawRecurrence struct {
	Pattern struct {
		// Six documented values per
		// learn.microsoft.com/.../recurrencepattern: daily, weekly,
		// absoluteMonthly, relativeMonthly, absoluteYearly,
		// relativeYearly. Unknown values fall through to an empty
		// summary (forward-compatible).
		Type       string   `json:"type"`
		Interval   int      `json:"interval"`
		DaysOfWeek []string `json:"daysOfWeek"`
		DayOfMonth int      `json:"dayOfMonth"`
		Month      int      `json:"month"` // 1-12 for absoluteYearly / relativeYearly
		Index      string   `json:"index"` // first|second|third|fourth|last (relative*)
	} `json:"pattern"`
}

// eventMessageExpandFields is the $select list applied inside the
// $expand=microsoft.graph.eventMessage/event(...) cast.
const eventMessageExpandFields = "id,subject,start,end,isAllDay,location,onlineMeeting,organizer,attendees,responseStatus,recurrence,webLink"

// GetEventMessage fetches the eventMessage cast of a message id with
// its event navigation property expanded. Returns *EventMessage with
// Event=nil when the message is a response type (meetingAccepted /
// meetingTenativelyAccepted / meetingDeclined) or when Graph elides
// the event field for any other reason — callers must tolerate nil.
//
// A 404 surfaces a typed *GraphError so spec 34 §6.1's soft-fail
// path can branch on it.
//
// Scope: Mail.ReadWrite (already granted; same scope as
// GetMessageBody). Calendars.Read is NOT required — the read routes
// via the messages endpoint, not /me/events/{id}.
func (c *Client) GetEventMessage(ctx context.Context, messageID string) (*EventMessage, error) {
	if messageID == "" {
		return nil, fmt.Errorf("graph: GetEventMessage: messageID required")
	}
	q := url.Values{}
	q.Set("$select", "id,meetingMessageType")
	q.Set("$expand", "microsoft.graph.eventMessage/event($select="+eventMessageExpandFields+")")

	resp, err := c.Do(ctx, http.MethodGet, "/me/messages/"+url.PathEscape(messageID)+"?"+q.Encode(), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var raw rawEventMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("graph: decode eventMessage: %w", err)
	}
	em := &EventMessage{
		MessageID:          raw.ID,
		MeetingMessageType: raw.MeetingMessageType,
	}
	if raw.Event != nil {
		em.Event = decodeEventMessageEvent(raw.Event)
	}
	return em, nil
}

// decodeEventMessageEvent translates the raw JSON shape into the
// public EventMessageEvent. Times that fail to parse default to the
// zero value (caller-visible — RenderInviteCard treats zero as a
// degraded but non-crashing render).
func decodeEventMessageEvent(e *rawEventMessageEvent) *EventMessageEvent {
	out := &EventMessageEvent{
		ID:               e.ID,
		Subject:          e.Subject,
		IsAllDay:         e.IsAllDay,
		Location:         e.Location.DisplayName,
		OnlineJoinURL:    e.OnlineMeeting.JoinURL,
		OrganizerName:    e.Organizer.EmailAddress.Name,
		OrganizerAddress: e.Organizer.EmailAddress.Address,
		ResponseStatus:   e.ResponseStatus.Response,
		WebLink:          e.WebLink,
	}
	out.Start, _ = time.Parse("2006-01-02T15:04:05.0000000", e.Start.DateTime)
	out.End, _ = time.Parse("2006-01-02T15:04:05.0000000", e.End.DateTime)
	for _, a := range e.Attendees {
		att := EventAttendee{
			Name:    a.EmailAddress.Name,
			Address: a.EmailAddress.Address,
			Type:    a.Type,
			Status:  a.Status.Response,
		}
		switch a.Type {
		case "optional":
			out.Optional = append(out.Optional, att)
		case "resource":
			// Resources (rooms) aren't broken out in the spec-34
			// card; treat them as optional so the count line stays
			// honest about who's invited.
			out.Optional = append(out.Optional, att)
		default:
			out.Required = append(out.Required, att)
		}
	}
	if e.Recurrence != nil {
		out.Recurrence = summarizeRecurrence(e.Recurrence)
	}
	return out
}

// summarizeRecurrence reduces Graph's structured recurrence pattern
// to a single human-readable line per spec 34 §6.2. Returns "" for
// unrecognised pattern types so future Graph additions don't crash
// the renderer.
func summarizeRecurrence(r *rawRecurrence) string {
	switch r.Pattern.Type {
	case "daily":
		return "Daily"
	case "weekly":
		if days := joinDayList(r.Pattern.DaysOfWeek); days != "" {
			return "Weekly on " + days
		}
		return "Weekly"
	case "absoluteMonthly":
		if r.Pattern.DayOfMonth > 0 {
			return "Monthly on the " + ordinal(r.Pattern.DayOfMonth)
		}
		return "Monthly"
	case "relativeMonthly":
		if day := relativeDayPhrase(r.Pattern.Index, r.Pattern.DaysOfWeek); day != "" {
			return "Monthly on the " + day
		}
		return "Monthly"
	case "absoluteYearly":
		if r.Pattern.Month >= 1 && r.Pattern.Month <= 12 && r.Pattern.DayOfMonth > 0 {
			return fmt.Sprintf("Yearly on %s %d",
				time.Month(r.Pattern.Month).String(), r.Pattern.DayOfMonth)
		}
		return "Yearly"
	case "relativeYearly":
		if r.Pattern.Month < 1 || r.Pattern.Month > 12 {
			return "Yearly"
		}
		if day := relativeDayPhrase(r.Pattern.Index, r.Pattern.DaysOfWeek); day != "" {
			return fmt.Sprintf("Yearly on the %s of %s",
				day, time.Month(r.Pattern.Month).String())
		}
		return "Yearly"
	}
	return ""
}

// relativeDayPhrase composes "second Tuesday" from index+daysOfWeek.
// Returns "" when either is missing — caller falls through to the
// bare-frequency word per spec 34 §6.2.
func relativeDayPhrase(index string, days []string) string {
	if index == "" || len(days) == 0 {
		return ""
	}
	day := strings.ToLower(days[0])
	if day == "" {
		return ""
	}
	return index + " " + strings.Title(day) //nolint:staticcheck // strings.Title fine for ASCII day names
}

func joinDayList(days []string) string {
	if len(days) == 0 {
		return ""
	}
	parts := make([]string, 0, len(days))
	for _, d := range days {
		d = strings.ToLower(d)
		if d == "" {
			continue
		}
		parts = append(parts, strings.ToUpper(d[:1])+d[1:])
	}
	return strings.Join(parts, ", ")
}

// ordinal returns "1st", "2nd", "3rd", "4th"… for n>=0. Matches
// English ordinal rules including the 11th/12th/13th teen exception.
func ordinal(n int) string {
	if n < 0 {
		return fmt.Sprintf("%d", n)
	}
	suf := "th"
	switch n % 100 {
	case 11, 12, 13:
		// teens always use "th"
	default:
		switch n % 10 {
		case 1:
			suf = "st"
		case 2:
			suf = "nd"
		case 3:
			suf = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suf)
}
