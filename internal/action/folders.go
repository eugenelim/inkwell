package action

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eugenelim/inkwell/internal/store"
)

// FolderResult is what the UI gets back after a folder round-trips
// to Graph. ID is the server-assigned folder id (replaces any
// optimistic placeholder); DisplayName mirrors the requested name
// after Graph normalises (whitespace trim, etc.).
type FolderResult struct {
	ID             string
	DisplayName    string
	ParentFolderID string
}

// CreateFolder creates a folder via Graph and upserts it locally.
// Spec 18 §5.3 calls for an optimistic placeholder, but a freshly-
// created folder has no incoming messages and the user already
// expects a quick round-trip — we keep this synchronous and skip
// the placeholder dance. Failures surface to the caller; the UI
// shows them and the local store is unchanged.
//
// parentID may be "" for top-level folders.
func (e *Executor) CreateFolder(ctx context.Context, accountID int64, parentID, displayName string) (*FolderResult, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return nil, fmt.Errorf("create_folder: empty name")
	}
	f, err := e.gc.CreateFolder(ctx, parentID, displayName)
	if err != nil {
		return nil, fmt.Errorf("create_folder: %w", err)
	}
	// Upsert locally so the sidebar reflects the new folder before
	// the next sync cycle. Subsequent /me/mailFolders enumerations
	// idempotently overwrite this row with the canonical envelope.
	if err := e.st.UpsertFolder(ctx, store.Folder{
		ID:             f.ID,
		AccountID:      accountID,
		DisplayName:    f.DisplayName,
		ParentFolderID: f.ParentFolderID,
		WellKnownName:  f.WellKnownName,
		LastSyncedAt:   time.Now(),
	}); err != nil {
		// Persist failure isn't fatal — Graph created the folder; the
		// user's view will catch up on the next sync. Surface as a
		// warning but return the canonical result so the caller can
		// continue.
		e.logger.Warn("create_folder: local upsert failed",
			"folder_id", f.ID,
			"err", err.Error())
	}
	return &FolderResult{
		ID:             f.ID,
		DisplayName:    f.DisplayName,
		ParentFolderID: f.ParentFolderID,
	}, nil
}

// RenameFolder PATCHes the folder's displayName via Graph and
// updates the local row. The folder ID stays the same. Graph
// rejects rename of well-known folders with 403 — surfaced
// unchanged so the UI status bar can show "cannot rename a
// system folder".
func (e *Executor) RenameFolder(ctx context.Context, folderID, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return fmt.Errorf("rename_folder: empty name")
	}
	if folderID == "" {
		return fmt.Errorf("rename_folder: empty folder id")
	}
	if err := e.gc.RenameFolder(ctx, folderID, displayName); err != nil {
		return fmt.Errorf("rename_folder: %w", err)
	}
	// Local update — a single FK-safe field flip; no rollback path
	// because the Graph success means the rename landed and a local
	// failure just delays the visual update by one sync cycle.
	if err := e.st.UpdateFolderDisplayName(ctx, folderID, displayName); err != nil {
		e.logger.Warn("rename_folder: local update failed",
			"folder_id", folderID,
			"err", err.Error())
	}
	return nil
}

// DeleteFolder removes a folder via Graph (cascading children +
// messages to Deleted Items server-side) and drops the local row.
// The store's FK cascade removes child folder rows + messages
// automatically. 404 is treated as success per `docs/CONVENTIONS.md` §3.
func (e *Executor) DeleteFolder(ctx context.Context, folderID string) error {
	if folderID == "" {
		return fmt.Errorf("delete_folder: empty folder id")
	}
	if err := e.gc.DeleteFolder(ctx, folderID); err != nil {
		return fmt.Errorf("delete_folder: %w", err)
	}
	if err := e.st.DeleteFolder(ctx, folderID); err != nil {
		// Local delete failure is rare (FK cascade is well-tested);
		// log and let the next sync reconcile by NOT seeing the
		// folder in /me/mailFolders.
		e.logger.Warn("delete_folder: local delete failed",
			"folder_id", folderID,
			"err", err.Error())
	}
	return nil
}
