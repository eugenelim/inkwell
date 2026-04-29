package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// DraftRef is the minimal Graph response we capture after a draft is
// saved. ID is the new server-side message id; WebLink opens the
// draft in Outlook web (and via deep link, Outlook desktop on macOS
// if installed) — that's the spec 15 hand-off path.
type DraftRef struct {
	ID      string
	WebLink string
}

// CreateReply posts to /me/messages/{id}/createReply, which produces
// a draft pre-populated by Graph (subject prefixed with "Re:",
// recipients copied, quote chain inserted). Returns the new draft's
// id + webLink.
//
// Inkwell then replaces the body via [PatchMessageBody] using the
// user-edited content from the tempfile.
func (c *Client) CreateReply(ctx context.Context, sourceMessageID string) (*DraftRef, error) {
	url := "/me/messages/" + sourceMessageID + "/createReply"
	resp, err := c.Do(ctx, http.MethodPost, url, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseError(resp)
	}
	var raw struct {
		ID      string `json:"id"`
		WebLink string `json:"webLink"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("graph: decode createReply: %w", err)
	}
	return &DraftRef{ID: raw.ID, WebLink: raw.WebLink}, nil
}

// PatchMessageBody sets the body of an existing message (typically a
// draft we just created via createReply). Body is plain text in
// v0.11.0; rich HTML lifts when spec 15 graduates beyond minimum
// viable.
//
// Also updates To / Cc / Bcc / Subject if any of the slices are
// non-nil, so the user's edits to the tempfile headers round-trip.
func (c *Client) PatchMessageBody(ctx context.Context, id, body string, to, cc, bcc []string, subject string) error {
	payload := map[string]any{
		"body": map[string]string{
			"contentType": "text",
			"content":     body,
		},
	}
	if subject != "" {
		payload["subject"] = subject
	}
	if to != nil {
		payload["toRecipients"] = recipientList(to)
	}
	if cc != nil {
		payload["ccRecipients"] = recipientList(cc)
	}
	if bcc != nil {
		payload["bccRecipients"] = recipientList(bcc)
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("graph: marshal patch body: %w", err)
	}
	url := "/me/messages/" + id
	resp, err := c.Do(ctx, http.MethodPatch, url, bytes.NewReader(buf), http.Header{
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

// recipientList wraps each address in the Graph recipient envelope
// shape: {"emailAddress": {"address": "..."}}.
func recipientList(addrs []string) []map[string]any {
	out := make([]map[string]any, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, map[string]any{
			"emailAddress": map[string]string{"address": a},
		})
	}
	return out
}
