package sync

import (
	"context"
	"strings"
	"time"

	"github.com/eugenelim/inkwell/internal/store"
)

// englishDisplayNameToWellKnown maps the canonical English display names
// of standard Outlook folders to their well-known-name values. Used
// when Graph's /me/mailFolders endpoint won't return wellKnownName via
// $select (a real-tenant quirk caught in v0.2.4: "Could not find a
// property named 'wellKnownName' on type 'microsoft.graph.mailFolder'").
//
// Limitation: locale-sensitive. Non-English tenants will see empty
// well-known mappings. A future iter can switch to per-folder
// accessor calls (GET /me/mailFolders/{name}) for locale-agnostic
// resolution. For v0.2.x this is good enough for the target audience
// (English M365 mailboxes).
var englishDisplayNameToWellKnown = map[string]string{
	"inbox":                "inbox",
	"sent items":           "sentitems",
	"drafts":               "drafts",
	"archive":              "archive",
	"junk email":           "junkemail",
	"junk e-mail":          "junkemail",
	"deleted items":        "deleteditems",
	"conversation history": "conversationhistory",
	"sync issues":          "syncissues",
	"outbox":               "outbox",
}

// inferWellKnownName returns the well-known name for a given display
// name (case-insensitive), or empty string when no match.
func inferWellKnownName(displayName string) string {
	return englishDisplayNameToWellKnown[strings.ToLower(strings.TrimSpace(displayName))]
}

// syncFolders enumerates all mailFolders, upserts them, and deletes any
// folders that no longer exist server-side (cascades messages).
//
// Uses the delta endpoint (graph.ListFoldersDelta) so nested
// child folders are returned in one flat response — /me/mailFolders
// alone is non-recursive and surfaces only top-level folders. Real-
// tenant regression v0.13.x: users couldn't see Inbox children
// (Projects / Q4 / etc.) because the legacy ListFolders helper
// only walked the top level. The delta endpoint returns the whole
// tree regardless of depth.
//
// Two transformations on the Graph response:
//
//  1. parent_folder_id NULL-out: Graph returns top-level folders with
//     parentFolderId pointing to the mailbox root (msgfolderroot), a
//     folder we don't track. The folders.parent_folder_id → folders.id
//     FK would reject this. We collect the response's ID set and NULL
//     any parent that isn't in it.
//
//  2. wellKnownName inference: Graph 400's some tenants when we $select
//     wellKnownName on the LIST endpoint, so we don't request it. We
//     infer it from the DisplayName via [inferWellKnownName] which has
//     the canonical English mapping. Non-English tenants get empty
//     well-known names — they sort alphabetically and the Inbox-default
//     picker falls back to display-name match (case-insensitive).
func (e *engine) syncFolders(ctx context.Context) error {
	remote, err := e.gc.ListFoldersDelta(ctx)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(remote))
	for _, f := range remote {
		known[f.ID] = true
	}
	seen := make(map[string]bool, len(remote))
	for _, f := range remote {
		seen[f.ID] = true
		parent := f.ParentFolderID
		if !known[parent] {
			parent = ""
		}
		wkn := f.WellKnownName
		if wkn == "" {
			wkn = inferWellKnownName(f.DisplayName)
		}
		if err := e.st.UpsertFolder(ctx, store.Folder{
			ID:             f.ID,
			AccountID:      e.opts.AccountID,
			ParentFolderID: parent,
			DisplayName:    f.DisplayName,
			WellKnownName:  wkn,
			TotalCount:     f.TotalItemCount,
			UnreadCount:    f.UnreadItemCount,
			IsHidden:       f.IsHidden,
			LastSyncedAt:   time.Now(),
		}); err != nil {
			return err
		}
	}
	existing, err := e.st.ListFolders(ctx, e.opts.AccountID)
	if err != nil {
		return err
	}
	for _, f := range existing {
		if !seen[f.ID] {
			if err := e.st.DeleteFolder(ctx, f.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
