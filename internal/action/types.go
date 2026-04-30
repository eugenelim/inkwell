package action

import (
	"context"
	"fmt"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// applyLocal applies the optimistic mutation to the local store. The
// pre-state snapshot lets [rollbackLocal] reverse it if Graph rejects.
func applyLocal(ctx context.Context, st store.Store, a store.Action, pre *store.Message) error {
	if pre == nil {
		return fmt.Errorf("apply local: missing pre-state")
	}
	id := pre.ID
	switch a.Type {
	case store.ActionMarkRead:
		t := true
		return st.UpdateMessageFields(ctx, id, store.MessageFields{IsRead: &t})
	case store.ActionMarkUnread:
		f := false
		return st.UpdateMessageFields(ctx, id, store.MessageFields{IsRead: &f})
	case store.ActionFlag:
		flagged := "flagged"
		return st.UpdateMessageFields(ctx, id, store.MessageFields{FlagStatus: &flagged})
	case store.ActionUnflag:
		notFlagged := "notFlagged"
		return st.UpdateMessageFields(ctx, id, store.MessageFields{FlagStatus: &notFlagged})
	case store.ActionSoftDelete, store.ActionMove:
		dest := paramString(a.Params, "destination_folder_id")
		if dest == "" {
			return fmt.Errorf("apply local: move missing destination")
		}
		return st.UpdateMessageFields(ctx, id, store.MessageFields{FolderID: &dest})
	case store.ActionPermanentDelete:
		// Optimistic local delete; if Graph rejects, rollbackLocal
		// re-inserts from the snapshot.
		return st.DeleteMessage(ctx, id)
	default:
		return fmt.Errorf("apply local: unsupported action type %q", a.Type)
	}
}

// rollbackLocal reverses applyLocal using the pre-mutation snapshot.
// Errors are logged but not returned — rollback is best-effort.
func rollbackLocal(ctx context.Context, st store.Store, a store.Action, pre *store.Message) error {
	if pre == nil {
		return nil
	}
	switch a.Type {
	case store.ActionMarkRead, store.ActionMarkUnread:
		return st.UpdateMessageFields(ctx, pre.ID, store.MessageFields{IsRead: &pre.IsRead})
	case store.ActionFlag, store.ActionUnflag:
		fs := pre.FlagStatus
		return st.UpdateMessageFields(ctx, pre.ID, store.MessageFields{FlagStatus: &fs})
	case store.ActionSoftDelete, store.ActionMove:
		fid := pre.FolderID
		return st.UpdateMessageFields(ctx, pre.ID, store.MessageFields{FolderID: &fid})
	case store.ActionPermanentDelete:
		// Re-insert the snapshot so the user's view returns to the
		// pre-action state when Graph rejects the destructive call.
		return st.UpsertMessage(ctx, *pre)
	}
	return nil
}

// dispatch issues the Graph call corresponding to the action.
func (e *Executor) dispatch(ctx context.Context, a store.Action) error {
	if len(a.MessageIDs) != 1 {
		return fmt.Errorf("dispatch: expected single message ID, got %d", len(a.MessageIDs))
	}
	id := a.MessageIDs[0]
	switch a.Type {
	case store.ActionMarkRead:
		return e.gc.PatchMessage(ctx, id, map[string]any{"isRead": true})
	case store.ActionMarkUnread:
		return e.gc.PatchMessage(ctx, id, map[string]any{"isRead": false})
	case store.ActionFlag:
		return e.gc.PatchMessage(ctx, id, map[string]any{
			"flag": map[string]any{
				"flagStatus": "flagged",
			},
		})
	case store.ActionUnflag:
		return e.gc.PatchMessage(ctx, id, map[string]any{
			"flag": map[string]any{
				"flagStatus": "notFlagged",
			},
		})
	case store.ActionSoftDelete, store.ActionMove:
		// Prefer the well-known alias if present (more durable —
		// Graph accepts "deleteditems" / "archive" without
		// resolving to a tenant-specific ID). Fall back to the
		// stored real folder ID for user-folder moves.
		dest := paramString(a.Params, "destination_folder_alias")
		if dest == "" {
			dest = paramString(a.Params, "destination_folder_id")
		}
		if dest == "" {
			return fmt.Errorf("dispatch: move missing destination")
		}
		_, err := e.gc.MoveMessage(ctx, id, dest)
		// Graph 404 means the message is already where we wanted it
		// (or removed entirely). Treat as success per CLAUDE.md §3.
		if graph.IsNotFound(err) {
			return nil
		}
		return err
	case store.ActionPermanentDelete:
		// Spec 07 §6.7: POST /me/messages/{id}/permanentDelete.
		// Irreversible from the tenant; the UI must guard with a
		// confirm modal before reaching this method.
		return e.gc.PermanentDelete(ctx, id)
	default:
		return fmt.Errorf("dispatch: unsupported action type %q", a.Type)
	}
}

func paramString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Compile-time check: Executor satisfies sync.ActionDrainer.
var _ interface {
	Drain(ctx context.Context) error
} = (*Executor)(nil)
