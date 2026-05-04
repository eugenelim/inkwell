package graph

import (
	"bytes"
	"context"
	"encoding/base64"
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
	return c.createDraftFromSource(ctx, sourceMessageID, "createReply")
}

// CreateReplyAll posts to /me/messages/{id}/createReplyAll. Graph
// pre-populates the draft with the full audience (To = original
// From + remaining To recipients; Cc = original Cc; deduped against
// the user's own UPN server-side). Returns the draft's id + webLink
// for stage 2's PATCH. Spec 15 §5 / PR 7-iii.
func (c *Client) CreateReplyAll(ctx context.Context, sourceMessageID string) (*DraftRef, error) {
	return c.createDraftFromSource(ctx, sourceMessageID, "createReplyAll")
}

// CreateForward posts to /me/messages/{id}/createForward. Graph
// produces a draft with the source body wrapped in the canonical
// "Forwarded message" header block + quote chain; To/Cc start
// empty for the user to fill in. Spec 15 §5 / PR 7-iii.
func (c *Client) CreateForward(ctx context.Context, sourceMessageID string) (*DraftRef, error) {
	return c.createDraftFromSource(ctx, sourceMessageID, "createForward")
}

// createDraftFromSource is the shared two-stage stage-1 helper for
// the three Graph endpoints that all share the shape
// `/me/messages/{id}/<verb>`. Returns id + webLink so the caller's
// stage 2 can PATCH the body and headers.
func (c *Client) createDraftFromSource(ctx context.Context, sourceMessageID, verb string) (*DraftRef, error) {
	url := "/me/messages/" + sourceMessageID + "/" + verb
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
		return nil, fmt.Errorf("graph: decode %s: %w", verb, err)
	}
	return &DraftRef{ID: raw.ID, WebLink: raw.WebLink}, nil
}

// CreateNewDraft posts to /me/messages with the full body and
// recipients in one shot, returning a saved draft. Single-stage
// (no PATCH needed) because the API accepts the entire payload up
// front. Spec 15 §5 / PR 7-iii.
func (c *Client) CreateNewDraft(ctx context.Context, subject, body string, to, cc, bcc []string) (*DraftRef, error) {
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
		return nil, fmt.Errorf("graph: marshal new draft: %w", err)
	}
	resp, err := c.Do(ctx, http.MethodPost, "/me/messages",
		bytes.NewReader(buf),
		http.Header{"Content-Type": []string{"application/json"}})
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
		return nil, fmt.Errorf("graph: decode new draft: %w", err)
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

// DeleteDraft issues DELETE /me/messages/{id}, moving the draft to
// Deleted Items. Idempotent: 404 is treated as success (spec 15
// §6.3 / F-1). Use this from the discard flow AFTER the draft has
// been saved to the server; do NOT use for messages the user didn't
// draft in inkwell.
func (c *Client) DeleteDraft(ctx context.Context, id string) error {
	resp, err := c.Do(ctx, http.MethodDelete, "/me/messages/"+id, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil // idempotent: already gone
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp)
	}
	return nil
}

// AddDraftAttachment posts a file attachment to an existing draft via
// POST /me/messages/{messageID}/attachments. The Graph API expects
// a base64-encoded contentBytes field. Spec 15 §5 / F-1.
func (c *Client) AddDraftAttachment(ctx context.Context, messageID, name string, data []byte) error {
	payload := map[string]any{
		"@odata.type":  "#microsoft.graph.fileAttachment",
		"name":         name,
		"contentBytes": base64.StdEncoding.EncodeToString(data),
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("graph: marshal attachment: %w", err)
	}
	resp, err := c.Do(ctx, http.MethodPost,
		"/me/messages/"+messageID+"/attachments",
		bytes.NewReader(buf),
		http.Header{"Content-Type": []string{"application/json"}})
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
