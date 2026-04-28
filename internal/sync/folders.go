package sync

import (
	"context"
	"time"

	"github.com/eu-gene-lim/inkwell/internal/store"
)

// syncFolders enumerates all mailFolders, upserts them, and deletes any
// folders that no longer exist server-side (cascades messages).
func (e *engine) syncFolders(ctx context.Context) error {
	remote, err := e.gc.ListFolders(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(remote))
	for _, f := range remote {
		seen[f.ID] = true
		if err := e.st.UpsertFolder(ctx, store.Folder{
			ID:             f.ID,
			AccountID:      e.opts.AccountID,
			ParentFolderID: f.ParentFolderID,
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
