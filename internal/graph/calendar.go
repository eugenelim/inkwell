package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Event is a single calendar entry. Mirrors the subset of
// /me/calendarView fields we use for display. Times are UTC.
type Event struct {
	ID               string
	Subject          string
	OrganizerName    string
	OrganizerAddress string
	Start            time.Time
	End              time.Time
	IsAllDay         bool
	Location         string
	OnlineMeetingURL string
	ShowAs           string // "free" | "busy" | "tentative" | "oof" | "workingElsewhere"
	WebLink          string
}

// EventDetail extends Event with the data the spec 12 §7 detail
// modal renders: full attendee list with response status + the
// body preview. Returned by GetEvent($expand=attendees).
type EventDetail struct {
	Event
	BodyPreview string
	Attendees   []EventAttendee
}

// EventAttendee mirrors a Graph attendee row. Type is "required" /
// "optional" / "resource"; Status is "accepted" / "declined" /
// "tentativelyAccepted" / "notResponded" / "none".
type EventAttendee struct {
	Name    string
	Address string
	Type    string
	Status  string
}

// rawEventDetail is the decode shape for GET /me/events/{id}?$expand=attendees.
type rawEventDetail struct {
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
		TimeZone string `json:"timeZone"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone"`
	} `json:"end"`
	IsAllDay bool `json:"isAllDay"`
	Location struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	OnlineMeeting struct {
		JoinURL string `json:"joinUrl"`
	} `json:"onlineMeeting"`
	ShowAs      string `json:"showAs"`
	WebLink     string `json:"webLink"`
	BodyPreview string `json:"bodyPreview"`
	Attendees   []struct {
		Type   string `json:"type"`
		Status struct {
			Response string `json:"response"`
		} `json:"status"`
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"attendees"`
}

// rawCalendarView is the Graph response shape we decode into.
type rawCalendarView struct {
	Value []struct {
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
			TimeZone string `json:"timeZone"`
		} `json:"start"`
		End struct {
			DateTime string `json:"dateTime"`
			TimeZone string `json:"timeZone"`
		} `json:"end"`
		IsAllDay bool `json:"isAllDay"`
		Location struct {
			DisplayName string `json:"displayName"`
		} `json:"location"`
		OnlineMeeting struct {
			JoinURL string `json:"joinUrl"`
		} `json:"onlineMeeting"`
		ShowAs  string `json:"showAs"`
		WebLink string `json:"webLink"`
	} `json:"value"`
}

// ListEventsBetween fetches /me/calendarView for the supplied half-open
// [start, end) window. calendarView is the right endpoint here (not
// /me/events) because it expands recurring series into individual
// occurrences server-side — exactly what we want for display.
func (c *Client) ListEventsBetween(ctx context.Context, start, end time.Time) ([]Event, error) {
	q := url.Values{}
	q.Set("startDateTime", start.UTC().Format("2006-01-02T15:04:05"))
	q.Set("endDateTime", end.UTC().Format("2006-01-02T15:04:05"))
	q.Set("$top", "100")
	q.Set("$orderby", "start/dateTime asc")
	q.Set("$select", "id,subject,organizer,start,end,isAllDay,location,onlineMeeting,showAs,webLink")

	resp, err := c.Do(ctx, http.MethodGet, "/me/calendarView?"+q.Encode(), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var raw rawCalendarView
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("graph: decode calendarView: %w", err)
	}
	out := make([]Event, 0, len(raw.Value))
	for _, e := range raw.Value {
		startT, _ := time.Parse("2006-01-02T15:04:05.0000000", e.Start.DateTime)
		endT, _ := time.Parse("2006-01-02T15:04:05.0000000", e.End.DateTime)
		out = append(out, Event{
			ID:               e.ID,
			Subject:          e.Subject,
			OrganizerName:    e.Organizer.EmailAddress.Name,
			OrganizerAddress: e.Organizer.EmailAddress.Address,
			Start:            startT,
			End:              endT,
			IsAllDay:         e.IsAllDay,
			Location:         e.Location.DisplayName,
			OnlineMeetingURL: e.OnlineMeeting.JoinURL,
			ShowAs:           e.ShowAs,
			WebLink:          e.WebLink,
		})
	}
	return out, nil
}

// CalendarDeltaResult holds the page of events returned by a single
// /me/calendarView/delta call plus the delta link for the next call.
// When an ID appears in Removed, the caller should delete that event
// from the local cache.
type CalendarDeltaResult struct {
	Events    []Event
	Removed   []string // event IDs that Graph returned as @removed
	DeltaLink string   // full @odata.deltaLink URL; persist and pass on the next call
}

// ListCalendarDelta fetches one page of the calendarView delta stream.
// Pass an empty deltaLink for the first call (Graph starts a new delta
// query from scratch); pass the DeltaLink returned in the previous
// result to get only changes since then. Spec 12 §4.2.
func (c *Client) ListCalendarDelta(ctx context.Context, start, end time.Time, deltaLink string) (CalendarDeltaResult, error) {
	var endpoint string
	if deltaLink == "" {
		q := url.Values{}
		q.Set("startDateTime", start.UTC().Format("2006-01-02T15:04:05"))
		q.Set("endDateTime", end.UTC().Format("2006-01-02T15:04:05"))
		q.Set("$top", "100")
		q.Set("$select", "id,subject,organizer,start,end,isAllDay,location,onlineMeeting,showAs,webLink")
		endpoint = "/me/calendarView/delta?" + q.Encode()
	} else {
		endpoint = deltaLink
	}

	resp, err := c.Do(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return CalendarDeltaResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CalendarDeltaResult{}, parseError(resp)
	}

	var raw struct {
		Value []struct {
			ID      string `json:"id"`
			Removed *struct {
				Reason string `json:"reason"`
			} `json:"@removed"`
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
			ShowAs  string `json:"showAs"`
			WebLink string `json:"webLink"`
		} `json:"value"`
		DeltaLink string `json:"@odata.deltaLink"`
		NextLink  string `json:"@odata.nextLink"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return CalendarDeltaResult{}, fmt.Errorf("graph: decode calendarView delta: %w", err)
	}

	var result CalendarDeltaResult
	for _, e := range raw.Value {
		if e.Removed != nil {
			result.Removed = append(result.Removed, e.ID)
			continue
		}
		startT, _ := time.Parse("2006-01-02T15:04:05.0000000", e.Start.DateTime)
		endT, _ := time.Parse("2006-01-02T15:04:05.0000000", e.End.DateTime)
		result.Events = append(result.Events, Event{
			ID:               e.ID,
			Subject:          e.Subject,
			OrganizerName:    e.Organizer.EmailAddress.Name,
			OrganizerAddress: e.Organizer.EmailAddress.Address,
			Start:            startT,
			End:              endT,
			IsAllDay:         e.IsAllDay,
			Location:         e.Location.DisplayName,
			OnlineMeetingURL: e.OnlineMeeting.JoinURL,
			ShowAs:           e.ShowAs,
			WebLink:          e.WebLink,
		})
	}
	result.DeltaLink = raw.DeltaLink
	return result, nil
}

// ListEventsToday is the convenience wrapper for the "what's on my
// calendar today" view. Uses the local timezone to compute the day
// boundaries.
func (c *Client) ListEventsToday(ctx context.Context) ([]Event, error) {
	now := time.Now()
	y, m, d := now.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	end := start.Add(24 * time.Hour)
	return c.ListEventsBetween(ctx, start, end)
}

// GetEvent fetches the full detail for a single event including
// the attendee list and the body preview. Spec 12 §4.3. The
// attendee status comes from the user's perspective per Graph;
// "accepted" / "declined" / "tentativelyAccepted" / "notResponded"
// / "none" / "organizer" are the canonical values.
func (c *Client) GetEvent(ctx context.Context, id string) (EventDetail, error) {
	if id == "" {
		return EventDetail{}, fmt.Errorf("graph: GetEvent: id required")
	}
	q := url.Values{}
	q.Set("$expand", "attendees")
	q.Set("$select", "id,subject,organizer,start,end,isAllDay,location,onlineMeeting,showAs,webLink,bodyPreview,attendees")
	resp, err := c.Do(ctx, http.MethodGet, "/me/events/"+url.PathEscape(id)+"?"+q.Encode(), nil, nil)
	if err != nil {
		return EventDetail{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return EventDetail{}, parseError(resp)
	}
	var raw rawEventDetail
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return EventDetail{}, fmt.Errorf("graph: decode event: %w", err)
	}
	startT, _ := time.Parse("2006-01-02T15:04:05.0000000", raw.Start.DateTime)
	endT, _ := time.Parse("2006-01-02T15:04:05.0000000", raw.End.DateTime)
	det := EventDetail{
		Event: Event{
			ID:               raw.ID,
			Subject:          raw.Subject,
			OrganizerName:    raw.Organizer.EmailAddress.Name,
			OrganizerAddress: raw.Organizer.EmailAddress.Address,
			Start:            startT,
			End:              endT,
			IsAllDay:         raw.IsAllDay,
			Location:         raw.Location.DisplayName,
			OnlineMeetingURL: raw.OnlineMeeting.JoinURL,
			ShowAs:           raw.ShowAs,
			WebLink:          raw.WebLink,
		},
		BodyPreview: raw.BodyPreview,
	}
	for _, a := range raw.Attendees {
		det.Attendees = append(det.Attendees, EventAttendee{
			Name:    a.EmailAddress.Name,
			Address: a.EmailAddress.Address,
			Type:    a.Type,
			Status:  a.Status.Response,
		})
	}
	return det, nil
}
