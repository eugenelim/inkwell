package graph

import (
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
