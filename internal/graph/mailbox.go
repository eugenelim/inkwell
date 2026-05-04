package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AutoReplyStatus mirrors Graph's automaticRepliesSetting.status enum.
type AutoReplyStatus string

const (
	AutoReplyDisabled      AutoReplyStatus = "disabled"
	AutoReplyAlwaysEnabled AutoReplyStatus = "alwaysEnabled"
	AutoReplyScheduled     AutoReplyStatus = "scheduled"
)

// DateTimeTimeZone holds a local datetime string (RFC 3339 without timezone,
// e.g. "2026-04-28T09:00:00") and its associated IANA timezone name.
type DateTimeTimeZone struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// ToTime parses the DateTime string in the named timezone. Returns the zero
// time on any parse failure (malformed string or unknown timezone).
func (d *DateTimeTimeZone) ToTime() time.Time {
	if d == nil {
		return time.Time{}
	}
	loc, err := time.LoadLocation(d.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05.999999999", d.DateTime, loc)
	if err != nil {
		// Graph sometimes omits the fractional seconds; try without.
		t, err = time.ParseInLocation("2006-01-02T15:04:05", d.DateTime, loc)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

// MailboxSettings is the subset of /me/mailboxSettings we use for the
// out-of-office flow.
type MailboxSettings struct {
	AutoReplies         AutoRepliesSetting
	TimeZone            string
	Language            string
	WorkingHoursDisplay string
	DateFormat          string
	TimeFormat          string
}

// AutoRepliesSetting maps to Graph's automaticRepliesSetting object.
type AutoRepliesSetting struct {
	Status               AutoReplyStatus
	InternalReplyMessage string
	ExternalReplyMessage string
	ExternalAudience     string
	ScheduledStart       *DateTimeTimeZone
	ScheduledEnd         *DateTimeTimeZone
}

// rawMailboxSettings is the wire shape we decode into.
type rawMailboxSettings struct {
	AutomaticRepliesSetting struct {
		Status                 AutoReplyStatus   `json:"status"`
		InternalReplyMessage   string            `json:"internalReplyMessage"`
		ExternalReplyMessage   string            `json:"externalReplyMessage"`
		ExternalAudience       string            `json:"externalAudience"`
		ScheduledStartDateTime *DateTimeTimeZone `json:"scheduledStartDateTime"`
		ScheduledEndDateTime   *DateTimeTimeZone `json:"scheduledEndDateTime"`
	} `json:"automaticRepliesSetting"`
	TimeZone string `json:"timeZone"`
	Language struct {
		Locale string `json:"locale"`
	} `json:"language"`
	WorkingHours *struct {
		DaysOfWeek []string `json:"daysOfWeek"`
		StartTime  string   `json:"startTime"`
		EndTime    string   `json:"endTime"`
	} `json:"workingHours"`
	DateFormat string `json:"dateFormat"`
	TimeFormat string `json:"timeFormat"`
}

// GetMailboxSettings fetches /me/mailboxSettings.
func (c *Client) GetMailboxSettings(ctx context.Context) (*MailboxSettings, error) {
	resp, err := c.Do(ctx, http.MethodGet, "/me/mailboxSettings", nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var raw rawMailboxSettings
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("graph: decode mailboxSettings: %w", err)
	}
	s := &MailboxSettings{
		AutoReplies: AutoRepliesSetting{
			Status:               raw.AutomaticRepliesSetting.Status,
			InternalReplyMessage: raw.AutomaticRepliesSetting.InternalReplyMessage,
			ExternalReplyMessage: raw.AutomaticRepliesSetting.ExternalReplyMessage,
			ExternalAudience:     raw.AutomaticRepliesSetting.ExternalAudience,
			ScheduledStart:       raw.AutomaticRepliesSetting.ScheduledStartDateTime,
			ScheduledEnd:         raw.AutomaticRepliesSetting.ScheduledEndDateTime,
		},
		TimeZone:   raw.TimeZone,
		Language:   raw.Language.Locale,
		DateFormat: raw.DateFormat,
		TimeFormat: raw.TimeFormat,
	}
	if raw.WorkingHours != nil {
		s.WorkingHoursDisplay = buildWorkingHoursDisplay(raw.WorkingHours.DaysOfWeek, raw.WorkingHours.StartTime, raw.WorkingHours.EndTime)
	}
	return s, nil
}

// buildWorkingHoursDisplay converts a days slice + start/end time strings
// into a human-readable summary like "Mon–Fri 09:00–17:00".
func buildWorkingHoursDisplay(days []string, startTime, endTime string) string {
	if len(days) == 0 {
		return ""
	}
	// Normalize day names to 3-letter abbreviations.
	abbrev := map[string]string{
		"monday": "Mon", "tuesday": "Tue", "wednesday": "Wed",
		"thursday": "Thu", "friday": "Fri", "saturday": "Sat", "sunday": "Sun",
	}
	// Order for weekday sequence detection.
	order := []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	orderIdx := map[string]int{}
	for i, d := range order {
		orderIdx[d] = i
	}

	normalized := make([]string, 0, len(days))
	for _, d := range days {
		key := strings.ToLower(d)
		if a, ok := abbrev[key]; ok {
			normalized = append(normalized, a)
		} else {
			normalized = append(normalized, d)
		}
	}

	// Try to detect if the days are a contiguous weekday range.
	type dayEntry struct {
		name string
		idx  int
	}
	entries := make([]dayEntry, 0, len(days))
	valid := true
	for _, d := range days {
		key := strings.ToLower(d)
		idx, ok := orderIdx[key]
		if !ok {
			valid = false
			break
		}
		entries = append(entries, dayEntry{abbrev[key], idx})
	}

	dayStr := strings.Join(normalized, ", ")
	if valid && len(entries) >= 2 {
		// Sort by index.
		for i := 1; i < len(entries); i++ {
			for j := i; j > 0 && entries[j].idx < entries[j-1].idx; j-- {
				entries[j], entries[j-1] = entries[j-1], entries[j]
			}
		}
		// Check contiguous.
		contiguous := true
		for i := 1; i < len(entries); i++ {
			if entries[i].idx != entries[i-1].idx+1 {
				contiguous = false
				break
			}
		}
		if contiguous {
			dayStr = entries[0].name + "–" + entries[len(entries)-1].name
		}
	}

	// Trim seconds from time strings like "09:00:00" → "09:00".
	trim := func(t string) string {
		if len(t) > 5 {
			return t[:5]
		}
		return t
	}

	return dayStr + " " + trim(startTime) + "–" + trim(endTime)
}

// UpdateAutoReplies PATCHes /me/mailboxSettings. When s.Status is
// AutoReplyScheduled and both ScheduledStart/End are non-nil, the
// schedule fields are included in the payload.
func (c *Client) UpdateAutoReplies(ctx context.Context, s AutoRepliesSetting) error {
	inner := map[string]any{
		"status":               s.Status,
		"internalReplyMessage": s.InternalReplyMessage,
		"externalReplyMessage": s.ExternalReplyMessage,
		"externalAudience":     s.ExternalAudience,
	}
	if s.Status == AutoReplyScheduled && s.ScheduledStart != nil && s.ScheduledEnd != nil {
		inner["scheduledStartDateTime"] = s.ScheduledStart
		inner["scheduledEndDateTime"] = s.ScheduledEnd
	}
	payload := map[string]any{"automaticRepliesSetting": inner}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("graph: marshal auto-replies: %w", err)
	}
	resp, err := c.Do(ctx, http.MethodPatch, "/me/mailboxSettings", bytes.NewReader(body), http.Header{
		"Content-Type": []string{"application/json"},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp)
	}
	return nil
}
