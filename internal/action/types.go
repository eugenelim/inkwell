package action

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// applyLocal applies the optimistic mutation to the local store. The
// pre-state snapshot lets [rollbackLocal] reverse it if Graph rejects.
//
// Two-step contract: (1) mutate the message row, (2) adjust the
// affected folders' total_count / unread_count so the sidebar
// reflects the change at TUI speed instead of waiting for the next
// sync cycle. The folder-count step is best-effort — failures are
// swallowed because the next sync overwrites the columns from
// Graph's authoritative numbers anyway.
func applyLocal(ctx context.Context, st store.Store, a store.Action, pre *store.Message) error {
	if pre == nil {
		return fmt.Errorf("apply local: missing pre-state")
	}
	if err := applyLocalMessage(ctx, st, a, pre); err != nil {
		return err
	}
	applyFolderCountChanges(ctx, st, a, pre, +1)
	return nil
}

// applyLocalMessage mutates the message row only. Split out from
// applyLocal so the folder-count step lives in one place.
func applyLocalMessage(ctx context.Context, st store.Store, a store.Action, pre *store.Message) error {
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
		mf := store.MessageFields{FlagStatus: &flagged}
		if raw := paramString(a.Params, "due_date"); raw != "" {
			if t := parseDueDate(raw); !t.IsZero() {
				mf.FlagDueAt = &t
			}
		}
		return st.UpdateMessageFields(ctx, id, mf)
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
	case store.ActionCreateDraftReply,
		store.ActionCreateDraftReplyAll,
		store.ActionCreateDraftForward,
		store.ActionCreateDraft:
		// Drafts have no local row to mutate — they only appear in
		// the messages table after the next Drafts-folder delta sync.
		// Spec 15 §5: the local apply for drafts is a no-op; the
		// action's record in the actions table IS the local state.
		return nil
	case store.ActionDiscardDraft:
		// Discard is a server-only operation; the draft local row (if
		// it exists) is already gone via the session close path. The
		// next Drafts-folder delta sync reconciles anything that
		// remains. No local mutation needed.
		return nil
	default:
		return fmt.Errorf("apply local: unsupported action type %q", a.Type)
	}
}

// isDraftCreationAction reports whether t is one of the spec 15
// non-idempotent draft-creation kinds. Drain skips these so a
// retry doesn't fire createReply / createReplyAll / createForward
// / POST /me/messages a second time and produce a duplicate draft;
// PR 7-ii's crash-recovery resume path is the right place for
// stage-aware retry.
func isDraftCreationAction(t store.ActionType) bool {
	switch t {
	case store.ActionCreateDraftReply,
		store.ActionCreateDraftReplyAll,
		store.ActionCreateDraftForward,
		store.ActionCreateDraft:
		return true
	}
	return false
}

// folderCountChange is one folder's total_count / unread_count
// adjustment. Applied with +1 sign on apply and -1 on rollback so
// a single helper drives both directions.
type folderCountChange struct {
	folderID    string
	totalDelta  int
	unreadDelta int
}

// folderCountChanges returns the count adjustments to apply for the
// supplied action + pre-snapshot. The adjustments use the snapshot's
// IsRead / FolderID to know:
//   - whether `mark_read` actually transitions an unread row (and
//     thus decrements the source folder's unread_count) or is a
//     redundant write on an already-read row (no count change),
//   - what the SOURCE folder of a move/delete was (the destination
//     is in the action's Params),
//   - whether the moved/deleted row contributed to unread_count
//     (and thus needs to follow it across the move).
//
// Returns an empty slice for actions that don't affect counts
// (flag / unflag / categories / draft creation).
func folderCountChanges(a store.Action, pre *store.Message) []folderCountChange {
	if pre == nil {
		return nil
	}
	// unreadCarry is +1 if the row was UNread before the action;
	// it follows the row across a move and matches what gets
	// decremented when the row leaves a folder.
	unreadCarry := 0
	if !pre.IsRead {
		unreadCarry = 1
	}
	switch a.Type {
	case store.ActionMarkRead:
		if !pre.IsRead {
			return []folderCountChange{{folderID: pre.FolderID, unreadDelta: -1}}
		}
	case store.ActionMarkUnread:
		if pre.IsRead {
			return []folderCountChange{{folderID: pre.FolderID, unreadDelta: +1}}
		}
	case store.ActionMove, store.ActionSoftDelete:
		dest := paramString(a.Params, "destination_folder_id")
		if dest == "" || dest == pre.FolderID {
			return nil
		}
		return []folderCountChange{
			{folderID: pre.FolderID, totalDelta: -1, unreadDelta: -unreadCarry},
			{folderID: dest, totalDelta: +1, unreadDelta: +unreadCarry},
		}
	case store.ActionPermanentDelete:
		return []folderCountChange{
			{folderID: pre.FolderID, totalDelta: -1, unreadDelta: -unreadCarry},
		}
	}
	return nil
}

// applyFolderCountChanges fires the deltas computed by
// folderCountChanges, multiplied by `sign`. Pass +1 from
// applyLocal, -1 from rollbackLocal. Failures are logged at the
// store layer (best-effort) — the next sync overwrites these
// columns from Graph anyway, so any drift heals within one cycle.
func applyFolderCountChanges(ctx context.Context, st store.Store, a store.Action, pre *store.Message, sign int) {
	for _, c := range folderCountChanges(a, pre) {
		_ = st.AdjustFolderCounts(ctx, c.folderID, c.totalDelta*sign, c.unreadDelta*sign)
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
// Two-step contract mirrors applyLocal: restore the message row,
// then apply the inverse folder-count deltas so the sidebar
// matches the user-visible rollback.
func rollbackLocal(ctx context.Context, st store.Store, a store.Action, pre *store.Message) error {
	if pre == nil {
		return nil
	}
	err := rollbackLocalMessage(ctx, st, a, pre)
	applyFolderCountChanges(ctx, st, a, pre, -1)
	return err
}

// rollbackLocalMessage restores the message row only.
func rollbackLocalMessage(ctx context.Context, st store.Store, a store.Action, pre *store.Message) error {
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

// dispatch issues the Graph call corresponding to the action. For
// Move/SoftDelete it returns the new message ID assigned by Graph at
// the destination (the original ID is invalidated by Graph after a
// successful move). All other action types return an empty string.
func (e *Executor) dispatch(ctx context.Context, a store.Action) (string, error) {
	if len(a.MessageIDs) != 1 {
		return "", fmt.Errorf("dispatch: expected single message ID, got %d", len(a.MessageIDs))
	}
	id := a.MessageIDs[0]
	switch a.Type {
	case store.ActionMarkRead:
		return "", e.gc.PatchMessage(ctx, id, map[string]any{"isRead": true})
	case store.ActionMarkUnread:
		return "", e.gc.PatchMessage(ctx, id, map[string]any{"isRead": false})
	case store.ActionFlag:
		flagPatch := map[string]any{"flagStatus": "flagged"}
		if raw := paramString(a.Params, "due_date"); raw != "" {
			if t := parseDueDate(raw); !t.IsZero() {
				// Graph expects DateTimeTimeZone format.
				flagPatch["dueDateTime"] = map[string]any{
					"dateTime": t.UTC().Format("2006-01-02T15:04:05"),
					"timeZone": "UTC",
				}
			}
		}
		return "", e.gc.PatchMessage(ctx, id, map[string]any{"flag": flagPatch})
	case store.ActionUnflag:
		return "", e.gc.PatchMessage(ctx, id, map[string]any{
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
			return "", fmt.Errorf("dispatch: move missing destination")
		}
		newID, err := e.gc.MoveMessage(ctx, id, dest)
		// Graph 404 means the message is already where we wanted it
		// (or removed entirely). Treat as success per `docs/CONVENTIONS.md` §3.
		if graph.IsNotFound(err) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return newID, nil
	case store.ActionPermanentDelete:
		// Spec 07 §6.7: POST /me/messages/{id}/permanentDelete.
		// Irreversible from the tenant; the UI must guard with a
		// confirm modal before reaching this method.
		return "", e.gc.PermanentDelete(ctx, id)
	case store.ActionAddCategory, store.ActionRemoveCategory:
		// Spec 07 §6.9 / §6.10: PATCH the full categories array.
		// Graph requires the post-state list (no append / remove
		// primitive); we recompute from the snapshot + the param.
		cat := paramString(a.Params, "category")
		if cat == "" {
			return "", fmt.Errorf("dispatch: %s missing category param", a.Type)
		}
		// Re-fetch the post-apply local row so the dispatch payload
		// matches the optimistic state.
		row, err := e.st.GetMessage(ctx, id)
		if err != nil {
			return "", fmt.Errorf("dispatch %s: read row: %w", a.Type, err)
		}
		return "", e.gc.PatchMessage(ctx, id, map[string]any{
			"categories": row.Categories,
		})
	default:
		return "", fmt.Errorf("dispatch: unsupported action type %q", a.Type)
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

// parseDueDate parses a due_date param string in RFC 3339 or date-only
// ("2006-01-02") format. Returns zero time on failure; callers must check.
func parseDueDate(raw string) time.Time {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t
	}
	return time.Time{}
}

// Compile-time check: Executor satisfies sync.ActionDrainer.
var _ interface {
	Drain(ctx context.Context) error
} = (*Executor)(nil)
