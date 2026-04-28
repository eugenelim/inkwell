package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// FolderSelectFields is the locked $select for the /me/mailFolders
// listing. CRITICAL: wellKnownName is NOT returned by default; Graph
// only emits it when explicitly $select-ed. Without this, every
// folder comes back with wellKnownName="" and filterSubscribed
// can't tell Inbox from Sync Issues — the symptom reported in real-
// tenant smoke after v0.2.3 (Junk E-mail and Sync Issues both being
// synced; the Inbox-default picker falling back to alphabetical first).
const FolderSelectFields = "id,displayName,parentFolderId,wellKnownName,totalItemCount,unreadItemCount,isHidden"

// ListFolders enumerates all mailFolders for the signed-in user. It
// follows nextLinks until exhausted. Spec §7 calls this on every sync
// cycle; <100 folders is typical.
func (c *Client) ListFolders(ctx context.Context) ([]MailFolder, error) {
	url := "/me/mailFolders?$top=100&$select=" + FolderSelectFields
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
			resp.Body.Close()
			return nil, fmt.Errorf("graph: decode folders: %w", err)
		}
		resp.Body.Close()
		out = append(out, page.Value...)
		url = page.NextLink
	}
	return out, nil
}
