package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ListFolders enumerates all mailFolders for the signed-in user. It
// follows nextLinks until exhausted. Spec §7 calls this on every sync
// cycle; <100 folders is typical.
//
// We do NOT $select wellKnownName despite it being documented on the
// mailFolder resource. v0.2.4's real-tenant smoke caught Graph 400'ing
// with "Could not find a property named 'wellKnownName' on type
// 'microsoft.graph.mailFolder'" on at least one tenant. The property
// IS available via the per-folder accessor (GET /me/mailFolders/{name})
// but not via $select on the list endpoint for every tenant.
//
// Workaround: ListFolders returns folders with WellKnownName empty;
// the sync layer infers it from DisplayName via a heuristic that
// works for English tenants. See internal/sync/folders.go.
// Localisation is a future iter — see spec 03 §7.
func (c *Client) ListFolders(ctx context.Context) ([]MailFolder, error) {
	url := "/me/mailFolders?$top=100"

	var out []MailFolder
	for url != "" {
		resp, err := c.Do(ctx, http.MethodGet, url, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}
		var page FolderListResponse
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("graph: decode folders: %w", err)
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
// §4. 404 is treated as success per the CLAUDE.md §3 idempotency
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
