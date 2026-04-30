package action

import (
	"context"
	"fmt"
	"strings"

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
	case store.ActionAddCategory:
		cat := paramString(a.Params, "category")
		if cat == "" {
			return fmt.Errorf("apply local: add_category missing category param")
		}
		next := appendCategory(pre.Categories, cat)
		return st.UpdateMessageFields(ctx, id, store.MessageFields{Categories: &next})
	case store.ActionRemoveCategory:
		cat := paramString(a.Params, "category")
		if cat == "" {
			return fmt.Errorf("apply local: remove_category missing category param")
		}
		next := removeCategory(pre.Categories, cat)
		return st.UpdateMessageFields(ctx, id, store.MessageFields{Categories: &next})
	default:
		return fmt.Errorf("apply local: unsupported action type %q", a.Type)
	}
}

// appendCategory adds cat to existing if not already present.
// Categories are case-insensitive for dedup but preserve the
// user-supplied casing on insert. Mirrors Outlook's behaviour.
func appendCategory(existing []string, cat string) []string {
	for _, e := range existing {
		if strings.EqualFold(e, cat) {
			return append([]string(nil), existing...) // copy unchanged
		}
	}
	out := append([]string(nil), existing...)
	return append(out, cat)
}

// removeCategory drops cat from existing (case-insensitive).
func removeCategory(existing []string, cat string) []string {
	out := make([]string, 0, len(existing))
	for _, e := range existing {
		if !strings.EqualFold(e, cat) {
			out = append(out, e)
		}
	}
	return out
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
	case store.ActionAddCategory, store.ActionRemoveCategory:
		// Restore the snapshot's category list verbatim — Graph
		// rejection means the optimistic addition / removal didn't
		// stick.
		cats := pre.Categories
		return st.UpdateMessageFields(ctx, pre.ID, store.MessageFields{Categories: &cats})
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
	case store.ActionAddCategory, store.ActionRemoveCategory:
		// Spec 07 §6.9 / §6.10: PATCH the full categories array.
		// Graph requires the post-state list (no append / remove
		// primitive); we recompute from the snapshot + the param.
		cat := paramString(a.Params, "category")
		if cat == "" {
			return fmt.Errorf("dispatch: %s missing category param", a.Type)
		}
		// Re-fetch the post-apply local row so the dispatch payload
		// matches the optimistic state.
		row, err := e.st.GetMessage(ctx, id)
		if err != nil {
			return fmt.Errorf("dispatch %s: read row: %w", a.Type, err)
		}
		return e.gc.PatchMessage(ctx, id, map[string]any{
			"categories": row.Categories,
		})
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
