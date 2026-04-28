package sync

import (
	"context"
	"time"

	"github.com/eugenelim/inkwell/internal/store"
)

// syncFolders enumerates all mailFolders, upserts them, and deletes any
// folders that no longer exist server-side (cascades messages).
//
// Graph's /me/mailFolders response carries `parentFolderId` referring
// to folders we don't track — typically the well-known msgfolderroot
// ID, the user's mailbox root. Inserting that value would violate the
// folders.parent_folder_id → folders.id FK. We collect the ID set of
// the response and NULL out any parent reference that isn't in the
// set. This means the sidebar shows a flat folder list for now;
// hierarchical childFolders enumeration is deferred.
func (e *engine) syncFolders(ctx context.Context) error {
	remote, err := e.gc.ListFolders(ctx)
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
		if err := e.st.UpsertFolder(ctx, store.Folder{
			ID:             f.ID,
			AccountID:      e.opts.AccountID,
			ParentFolderID: parent,
			DisplayName:    f.DisplayName,
			WellKnownName:  f.WellKnownName,
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
