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
// Three transformations on the Graph response:
//
//  1. parent_folder_id NULL-out: Graph returns top-level folders with
//     parentFolderId pointing to the mailbox root (msgfolderroot), a
//     folder we don't track. The folders.parent_folder_id → folders.id
//     FK would reject this. We collect the response's ID set and NULL
//     any parent that isn't in it.
//
//  2. wellKnownName inference (TOP-LEVEL ONLY): Graph 400's some
//     tenants when we $select wellKnownName on the LIST endpoint, so
//     we don't request it. We infer from the DisplayName via
//     [inferWellKnownName] which has the canonical English mapping.
//     **Critical: only applied to top-level folders.** Real-tenant
//     v0.16.0 regression: a user with a nested folder literally
//     named "Inbox" (very common — old-mail archives, year-indexed
//     organisation, shared-mailbox mounts) would otherwise have the
//     heuristic infer wellKnownName="inbox" for the child too,
//     conflicting with the real top-level Inbox on the
//     `(account_id, well_known_name)` unique index.
//
//  3. wellKnownName dedup: even on top-level folders, if Graph returns
//     two rows claiming the same wellKnownName (shared-mailbox mounts,
//     search folders, tenant quirks), only the first wins. Subsequent
//     rows keep their own ID but lose the wellKnownName — better than
//     aborting the whole sync on the unique-index conflict.
func (e *engine) syncFolders(ctx context.Context) error {
	remote, err := e.gc.ListFoldersDelta(ctx)
	if err != nil {
		return err
	}
	// Build a set of non-tombstone IDs for parent-FK resolution and
	// the diff-from-stored-set delete pass.
	known := make(map[string]bool, len(remote))
	for _, f := range remote {
		if f.Removed == nil {
			known[f.ID] = true
		}
	}
	// Track wellKnownName values we've already used in this sync so a
	// second clashing row drops its wellKnownName instead of failing
	// the unique constraint.
	usedWellKnown := make(map[string]bool, len(remote))
	seen := make(map[string]bool, len(remote))
	for _, f := range remote {
		// Tombstone: propagate as an explicit delete. This path fires
		// when the caller uses an incremental deltaLink; on a fresh
		// full-scan Graph emits no tombstones and the diff-from-stored
		// pass below handles any deletions instead.
		if f.Removed != nil {
			if err := e.st.DeleteFolder(ctx, f.ID); err != nil {
				return err
			}
			continue
		}
		seen[f.ID] = true
		parent := f.ParentFolderID
		if !known[parent] {
			parent = ""
		}
		wkn := f.WellKnownName
		// Only top-level folders can carry an inferred wellKnownName.
		// A nested folder named "Inbox" is the user's filing label,
		// not Outlook's mailbox-root inbox.
		if wkn == "" && parent == "" {
			wkn = inferWellKnownName(f.DisplayName)
		}
		// Dedup: first wins, later rows lose the wellKnownName.
		if wkn != "" {
			if usedWellKnown[wkn] {
				wkn = ""
			} else {
				usedWellKnown[wkn] = true
			}
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
