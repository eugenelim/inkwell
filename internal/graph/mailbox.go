package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// AutoReplyStatus mirrors Graph's automaticRepliesSetting.status enum.
type AutoReplyStatus string

const (
	AutoReplyDisabled      AutoReplyStatus = "disabled"
	AutoReplyAlwaysEnabled AutoReplyStatus = "alwaysEnabled"
	AutoReplyScheduled     AutoReplyStatus = "scheduled"
)

// MailboxSettings is the subset of /me/mailboxSettings we use for the
// out-of-office flow. v0.9.0 ships only the auto-replies surface;
// working hours / locale / date format are deferred.
type MailboxSettings struct {
	AutoReplies AutoRepliesSetting
	TimeZone    string
	Language    string
}

// AutoRepliesSetting maps to Graph's automaticRepliesSetting object.
type AutoRepliesSetting struct {
	Status               AutoReplyStatus
	InternalReplyMessage string // body shown to in-tenant senders
	ExternalReplyMessage string // body shown to external senders
	ExternalAudience     string // "all" | "contactsOnly" | "none"
	// ScheduledStart / ScheduledEnd omitted — v0.9.0 doesn't edit schedules.
}

// rawMailboxSettings is the wire shape we decode into.
type rawMailboxSettings struct {
	AutomaticRepliesSetting struct {
		Status               AutoReplyStatus `json:"status"`
		InternalReplyMessage string          `json:"internalReplyMessage"`
		ExternalReplyMessage string          `json:"externalReplyMessage"`
		ExternalAudience     string          `json:"externalAudience"`
	} `json:"automaticRepliesSetting"`
	TimeZone string `json:"timeZone"`
	Language struct {
		Locale string `json:"locale"`
	} `json:"language"`
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
	return &MailboxSettings{
		AutoReplies: AutoRepliesSetting{
			Status:               raw.AutomaticRepliesSetting.Status,
			InternalReplyMessage: raw.AutomaticRepliesSetting.InternalReplyMessage,
			ExternalReplyMessage: raw.AutomaticRepliesSetting.ExternalReplyMessage,
			ExternalAudience:     raw.AutomaticRepliesSetting.ExternalAudience,
		},
		TimeZone: raw.TimeZone,
		Language: raw.Language.Locale,
	}, nil
}

// UpdateAutoReplies PATCHes /me/mailboxSettings to enable or disable
// automatic replies. Preserves the existing internal/external reply
// messages on the supplied AutoRepliesSetting (callers should fetch
// current state, mutate Status, and pass the whole struct back). The
// scheduled start/end fields are not edited in v0.9.0; if the user has
// a schedule configured in Outlook, it persists.
func (c *Client) UpdateAutoReplies(ctx context.Context, s AutoRepliesSetting) error {
	payload := map[string]any{
		"automaticRepliesSetting": map[string]any{
			"status":               s.Status,
			"internalReplyMessage": s.InternalReplyMessage,
			"externalReplyMessage": s.ExternalReplyMessage,
			"externalAudience":     s.ExternalAudience,
		},
	}
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
