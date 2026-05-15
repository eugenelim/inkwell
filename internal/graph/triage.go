package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// PatchMessage issues PATCH /me/messages/{id} with the supplied JSON
// payload. Used by the action executor for property mutations:
// {"isRead": true} for mark_read, {"flag": {...}} for flag/unflag.
func (c *Client) PatchMessage(ctx context.Context, id string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("graph: marshal patch: %w", err)
	}
	url := "/me/messages/" + id
	resp, err := c.Do(ctx, http.MethodPatch, url, bytes.NewReader(body), http.Header{
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

// PermanentDelete issues POST /me/messages/{id}/permanentDelete. Once
// Graph honours the request the message is gone from the tenant —
// this is irreversible by design and the UI must guard with a
// confirm modal before reaching this method (spec 07 §6.7).
//
// Returns nil on 204 No Content (the canonical success). 404 is
// also treated as success: the message is already gone, which is
// the user's intent (`docs/CONVENTIONS.md` §3 idempotency invariant).
func (c *Client) PermanentDelete(ctx context.Context, id string) error {
	url := "/me/messages/" + id + "/permanentDelete"
	resp, err := c.Do(ctx, http.MethodPost, url, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp)
	}
	return nil
}

// MoveMessage issues POST /me/messages/{id}/move to relocate a message
// to the given destination folder. destinationID accepts either a real
// folder ID or a well-known name (e.g. "deleteditems", "archive",
// "junkemail"). Returns the new message ID assigned by Graph at the
// destination (the original ID is invalidated).
func (c *Client) MoveMessage(ctx context.Context, id, destinationID string) (string, error) {
	body, err := json.Marshal(map[string]string{"destinationId": destinationID})
	if err != nil {
		return "", fmt.Errorf("graph: marshal move: %w", err)
	}
	url := "/me/messages/" + id + "/move"
	resp, err := c.Do(ctx, http.MethodPost, url, bytes.NewReader(body), http.Header{
		"Content-Type": []string{"application/json"},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", parseError(resp)
	}
	var moved struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&moved); err != nil {
		return "", fmt.Errorf("graph: decode move response: %w", err)
	}
	return moved.ID, nil
}
