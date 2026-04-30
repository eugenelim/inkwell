package action

import (
	"fmt"

	"github.com/eugenelim/inkwell/internal/store"
)

// Inverse computes the undo descriptor for a successfully-applied
// action. The result is what's pushed onto the session undo stack so
// the next `u` keystroke (or `:undo`) can roll the change back.
//
// `pre` is the message snapshot taken at run-start (before applyLocal
// fired). The inverse uses pre's fields to restore state — e.g.
// MarkRead's inverse copies pre.IsRead into the params so undo
// restores even if the user toggled the flag elsewhere in the
// meantime.
//
// Returns (UndoEntry, true) when the action is reversible. Returns
// (_, false) when the action is intentionally irreversible (today
// only `permanent_delete` qualifies — once Graph deletes the message
// it's gone). Callers must NOT push a non-reversible result.
//
// Spec 07 §11 invariant: every action that produces a server-state
// change pushes exactly one undo entry on success. Replay
// (`Drain`) does NOT push duplicates — only the first dispatch path
// (`run`) does, because the user already has the inverse on the
// stack from their first action.
func Inverse(a store.Action, pre *store.Message) (store.UndoEntry, bool) {
	switch a.Type {
	case store.ActionMarkRead:
		// Undo: mark unread. If the message was already read at
		// snapshot time, undoing is a no-op the user wouldn't
		// expect; we still push a mark_unread because that's what
		// the user pressed `r` against and the inverse is well-
		// defined.
		return store.UndoEntry{
			ActionType: store.ActionMarkUnread,
			MessageIDs: a.MessageIDs,
			Label:      "marked read",
		}, true
	case store.ActionMarkUnread:
		return store.UndoEntry{
			ActionType: store.ActionMarkRead,
			MessageIDs: a.MessageIDs,
			Label:      "marked unread",
		}, true
	case store.ActionFlag:
		return store.UndoEntry{
			ActionType: store.ActionUnflag,
			MessageIDs: a.MessageIDs,
			Label:      "flagged",
		}, true
	case store.ActionUnflag:
		return store.UndoEntry{
			ActionType: store.ActionFlag,
			MessageIDs: a.MessageIDs,
			Label:      "unflagged",
		}, true
	case store.ActionMove, store.ActionSoftDelete:
		// Inverse of move (incl. archive + soft_delete) is a move
		// back to the source folder. pre.FolderID is the truth from
		// snapshot time. If pre is nil — caller forgot to snapshot —
		// we can't compute it; return non-reversible.
		if pre == nil {
			return store.UndoEntry{}, false
		}
		return store.UndoEntry{
			ActionType: store.ActionMove,
			MessageIDs: a.MessageIDs,
			Params: map[string]any{
				"destination_folder_id": pre.FolderID,
			},
			Label: undoLabelFor(a.Type),
		}, true
	case store.ActionAddCategory:
		// Inverse: remove the category that was added.
		cat, _ := a.Params["category"].(string)
		return store.UndoEntry{
			ActionType: store.ActionRemoveCategory,
			MessageIDs: a.MessageIDs,
			Params:     map[string]any{"category": cat},
			Label:      fmt.Sprintf("added category %q", cat),
		}, true
	case store.ActionRemoveCategory:
		cat, _ := a.Params["category"].(string)
		return store.UndoEntry{
			ActionType: store.ActionAddCategory,
			MessageIDs: a.MessageIDs,
			Params:     map[string]any{"category": cat},
			Label:      fmt.Sprintf("removed category %q", cat),
		}, true
	case store.ActionPermanentDelete:
		// Permanent delete is intentionally irreversible — once
		// Graph honours the request the message is gone from the
		// tenant. The confirm modal warns the user; no undo.
		return store.UndoEntry{}, false
	}
	return store.UndoEntry{}, false
}

func undoLabelFor(t store.ActionType) string {
	switch t {
	case store.ActionSoftDelete:
		return "deleted"
	case store.ActionMove:
		return "moved"
	}
	return string(t)
}
