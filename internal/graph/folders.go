package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ListFoldersDelta enumerates every mail folder — including
// nested children — in a single paginated response. Uses
// /me/mailFolders/delta which returns the full folder tree flat
// regardless of depth, sidestepping the v0.x non-recursive
// limitation of /me/mailFolders.
//
// The delta endpoint also returns a deltaLink the caller can
// pass back on a subsequent call for incremental updates
// (server-side deletions arrive as @removed markers). v0.13.x
// doesn't persist the deltaLink yet — every sync cycle calls
// fresh, which is fine because folder lists are small (typically
// <100 folders) and one extra GET per cycle is unmeasurable
// against the per-folder message-delta calls. A future iter
// adds a meta column for the deltaLink and switches to the
// incremental path.
//
// @removed tombstones from the delta endpoint are included in the
// returned slice with Removed != nil so callers can propagate deletes
// directly instead of relying solely on the diff-from-stored-set pass.
// On a fresh delta scan (no persisted deltaLink) Graph returns no
// tombstones, so the change is transparent to the current code path.
func (c *Client) ListFoldersDelta(ctx context.Context) ([]MailFolder, error) {
	url := "/me/mailFolders/delta"
	var out []MailFolder
	for url != "" {
		resp, err := c.Do(ctx, http.MethodGet, url, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, parseError(resp)
		}
		var page struct {
			Value     []MailFolder `json:"value"`
			NextLink  string       `json:"@odata.nextLink,omitempty"`
			DeltaLink string       `json:"@odata.deltaLink,omitempty"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("graph: decode folders delta: %w", err)
		}
		_ = resp.Body.Close()
		out = append(out, page.Value...)
		url = page.NextLink
	}
	return out, nil
}

// CreateFolder creates a new mail folder. parentID is the Graph
// folder ID to nest under, or "" for a top-level folder. Returns
// the new MailFolder envelope (including the assigned ID) which
// callers upsert locally to replace any optimistic placeholder.
//
// Spec 18 §4. Body: {"displayName": "<name>"}.
func (c *Client) CreateFolder(ctx context.Context, parentID, displayName string) (*MailFolder, error) {
	body, err := json.Marshal(map[string]string{"displayName": displayName})
	if err != nil {
		return nil, fmt.Errorf("graph: marshal create-folder: %w", err)
	}
	url := "/me/mailFolders"
	if parentID != "" {
		url = "/me/mailFolders/" + parentID + "/childFolders"
	}
	resp, err := c.Do(ctx, http.MethodPost, url, bytes.NewReader(body), http.Header{
		"Content-Type": []string{"application/json"},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseError(resp)
	}
	var f MailFolder
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return nil, fmt.Errorf("graph: decode create-folder: %w", err)
	}
	return &f, nil
}

// RenameFolder PATCHes a folder's displayName. The folder ID stays
// the same. Spec 18 §4. Graph rejects rename of well-known folders
// (Inbox / Drafts / etc.) with 403 — surfaced unchanged.
func (c *Client) RenameFolder(ctx context.Context, folderID, displayName string) error {
	body, err := json.Marshal(map[string]string{"displayName": displayName})
	if err != nil {
		return fmt.Errorf("graph: marshal rename-folder: %w", err)
	}
	resp, err := c.Do(ctx, http.MethodPatch, "/me/mailFolders/"+folderID,
		bytes.NewReader(body), http.Header{
			"Content-Type": []string{"application/json"},
		})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp)
	}
	return nil
}

// DeleteFolder removes a folder server-side. Children + messages
// cascade into Deleted Items recursively (Graph contract). Spec 18
// §4. 404 is treated as success per the `docs/CONVENTIONS.md` §3 idempotency
// invariant: the user wanted the folder gone; Graph confirms it's
// gone.
func (c *Client) DeleteFolder(ctx context.Context, folderID string) error {
	resp, err := c.Do(ctx, http.MethodDelete, "/me/mailFolders/"+folderID, nil, nil)
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
