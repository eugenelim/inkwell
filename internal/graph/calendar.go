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
