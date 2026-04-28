package action

import (
	"context"
	"fmt"
	"time"

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
		dest := paramString(a.Params, "destination_folder_id")
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

// touch keeps time imported even if we drop the explicit usage.
var _ = time.Now
