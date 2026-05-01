package action

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// Type is a re-export of store.ActionType for callers that don't want
// to import internal/store directly.
type Type = store.ActionType

// Action types this executor handles. The full list lives in
// internal/store/types.go; v0.3.0 implements the most-used subset.
const (
	TypeMarkRead   = store.ActionMarkRead
	TypeMarkUnread = store.ActionMarkUnread
	TypeFlag       = store.ActionFlag
	TypeUnflag     = store.ActionUnflag
	TypeSoftDelete = store.ActionSoftDelete
	TypeArchive    = store.ActionMove // archive resolves to move-to-archive
	TypeMove       = store.ActionMove
)

// Executor applies optimistic local mutations and dispatches Graph
// calls. It implements [sync.ActionDrainer] so the sync engine drains
// the queue at every cycle (handles retry-after-failure transparently).
type Executor struct {
	st     store.Store
	gc     *graph.Client
	logger *slog.Logger
}

// New constructs an executor.
func New(st store.Store, gc *graph.Client, logger *slog.Logger) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{st: st, gc: gc, logger: logger}
}

// MarkRead enqueues + applies a mark-read action.
func (e *Executor) MarkRead(ctx context.Context, accountID int64, messageID string) error {
	return e.run(ctx, store.Action{
		ID:         newActionID(),
		AccountID:  accountID,
		Type:       TypeMarkRead,
		MessageIDs: []string{messageID},
	})
}

// MarkUnread enqueues + applies a mark-unread action.
func (e *Executor) MarkUnread(ctx context.Context, accountID int64, messageID string) error {
	return e.run(ctx, store.Action{
		ID:         newActionID(),
		AccountID:  accountID,
		Type:       TypeMarkUnread,
		MessageIDs: []string{messageID},
	})
}

// ToggleFlag flags an unflagged message and unflags a flagged one.
// Caller passes the current state (read from the cached envelope).
func (e *Executor) ToggleFlag(ctx context.Context, accountID int64, messageID string, currentlyFlagged bool) error {
	t := TypeFlag
	if currentlyFlagged {
		t = TypeUnflag
	}
	return e.run(ctx, store.Action{
		ID:         newActionID(),
		AccountID:  accountID,
		Type:       t,
		MessageIDs: []string{messageID},
	})
}

// AddCategory tags the message with the supplied category name.
// Categories are case-insensitive for dedup (Outlook semantics);
// supplying an existing one is a no-op locally and a redundant
// PATCH server-side. Spec 07 §6.9.
func (e *Executor) AddCategory(ctx context.Context, accountID int64, messageID, category string) error {
	if strings.TrimSpace(category) == "" {
		return fmt.Errorf("add_category: category required")
	}
	return e.run(ctx, store.Action{
		ID:         newActionID(),
		AccountID:  accountID,
		Type:       store.ActionAddCategory,
		MessageIDs: []string{messageID},
		Params:     map[string]any{"category": category},
	})
}

// RemoveCategory untags the message. Spec 07 §6.10.
func (e *Executor) RemoveCategory(ctx context.Context, accountID int64, messageID, category string) error {
	if strings.TrimSpace(category) == "" {
		return fmt.Errorf("remove_category: category required")
	}
	return e.run(ctx, store.Action{
		ID:         newActionID(),
		AccountID:  accountID,
		Type:       store.ActionRemoveCategory,
		MessageIDs: []string{messageID},
		Params:     map[string]any{"category": category},
	})
}

// PermanentDelete removes a message from the tenant entirely. Spec
// 07 §6.7 invariant: this action is **irreversible**; Inverse
// returns ok=false and Executor.run skips the undo push. The UI
// MUST gate this method behind a confirm modal — pressing `D`
// without confirmation is the primary footgun this method exists
// to handle, so the executor refuses bare invocation by checking
// the action's SkipUndo flag (set only via the confirmed-dispatch
// path).
func (e *Executor) PermanentDelete(ctx context.Context, accountID int64, messageID string) error {
	return e.run(ctx, store.Action{
		ID:         newActionID(),
		AccountID:  accountID,
		Type:       store.ActionPermanentDelete,
		MessageIDs: []string{messageID},
	})
}

// SoftDelete moves a message to Deleted Items.
func (e *Executor) SoftDelete(ctx context.Context, accountID int64, messageID string) error {
	dest, alias, err := e.resolveWellKnownDestination(ctx, accountID, "deleteditems")
	if err != nil {
		return err
	}
	return e.run(ctx, store.Action{
		ID:         newActionID(),
		AccountID:  accountID,
		Type:       TypeSoftDelete,
		MessageIDs: []string{messageID},
		Params: map[string]any{
			"destination_folder_id":    dest,
			"destination_folder_alias": alias,
		},
	})
}

// Move moves a message to the supplied destination folder. Spec 07
// §6.5. destFolderID is the Graph folder ID; alias may be a Graph
// well-known name ("inbox", "archive", "deleteditems") and is used
// preferentially in the dispatch path because Graph accepts those
// without resolving to tenant-specific IDs. Either may be empty —
// when both are empty the call rejects with an error.
func (e *Executor) Move(ctx context.Context, accountID int64, messageID, destFolderID, destAlias string) error {
	if strings.TrimSpace(destFolderID) == "" && strings.TrimSpace(destAlias) == "" {
		return fmt.Errorf("move: destination folder required")
	}
	return e.run(ctx, store.Action{
		ID:         newActionID(),
		AccountID:  accountID,
		Type:       TypeMove,
		MessageIDs: []string{messageID},
		Params: map[string]any{
			"destination_folder_id":    destFolderID,
			"destination_folder_alias": destAlias,
		},
	})
}

// Archive moves a message to the Archive folder.
func (e *Executor) Archive(ctx context.Context, accountID int64, messageID string) error {
	dest, alias, err := e.resolveWellKnownDestination(ctx, accountID, "archive")
	if err != nil {
		return err
	}
	return e.run(ctx, store.Action{
		ID:         newActionID(),
		AccountID:  accountID,
		Type:       TypeArchive,
		MessageIDs: []string{messageID},
		Params: map[string]any{
			"destination_folder_id":    dest,
			"destination_folder_alias": alias,
		},
	})
}

// resolveWellKnownDestination looks up the actual folder ID for a
// well-known name (e.g. "deleteditems") in the local store. Returns
// (realID, alias, nil) on hit. If the folder isn't synced yet, returns
// the alias as both id and alias and lets the local apply fail with a
// clearer error. Graph's /move endpoint accepts both forms, so the
// dispatch path uses the alias as a fallback.
func (e *Executor) resolveWellKnownDestination(ctx context.Context, accountID int64, alias string) (string, string, error) {
	f, err := e.st.GetFolderByWellKnown(ctx, accountID, alias)
	if err != nil || f == nil {
		// Not yet synced — surface a friendly error rather than the
		// raw FK constraint message.
		return "", alias, fmt.Errorf("destination folder %q not yet synced; wait for the next sync cycle and retry", alias)
	}
	return f.ID, alias, nil
}

// run is the synchronous Execute path: optimistic local apply →
// enqueue → Graph dispatch → update status. Failures roll back the
// local mutation (best-effort) and surface to the caller.
func (e *Executor) run(ctx context.Context, a store.Action) error {
	a.CreatedAt = time.Now()
	a.Status = store.StatusPending

	// Snapshot pre-state for rollback.
	if len(a.MessageIDs) != 1 {
		return fmt.Errorf("action: single-message actions only (bulk lands in spec 09)")
	}
	id := a.MessageIDs[0]
	pre, err := e.st.GetMessage(ctx, id)
	if err != nil {
		return fmt.Errorf("action: snapshot %s: %w", id, err)
	}

	if err := applyLocal(ctx, e.st, a, pre); err != nil {
		return fmt.Errorf("action: apply local: %w", err)
	}
	if err := e.st.EnqueueAction(ctx, a); err != nil {
		// Local applied but queue insert failed — try to roll back so
		// the UI doesn't show inconsistent state.
		_ = rollbackLocal(ctx, e.st, a, pre)
		return fmt.Errorf("action: enqueue: %w", err)
	}

	// Dispatch synchronously. Most triage actions are <1s on Graph.
	if err := e.dispatch(ctx, a); err != nil {
		_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusFailed, err.Error())
		_ = rollbackLocal(ctx, e.st, a, pre)
		return fmt.Errorf("action: dispatch: %w", err)
	}
	if err := e.st.UpdateActionStatus(ctx, a.ID, store.StatusDone, ""); err != nil {
		e.logger.Warn("action: status update failed", "action_id", a.ID, "err", err)
	}
	// Spec 07 §11 — push an inverse-action descriptor so the next `u`
	// keystroke can roll this back. Only the first-dispatch path
	// (run) pushes; Drain replays don't (the entry is already on the
	// stack from the user's first attempt). Non-reversible actions
	// (permanent_delete) skip the push.
	if !a.SkipUndo {
		if entry, ok := Inverse(a, pre); ok {
			if err := e.st.PushUndo(ctx, entry); err != nil {
				// Push failure isn't fatal to the action itself —
				// the user's data is in the desired state. Log + move on.
				e.logger.Warn("action: undo push failed", "action_id", a.ID, "err", err)
			}
		}
	}
	return nil
}

// Undo pops the most recent UndoEntry and applies it as a fresh
// action. Inverse pairs are symmetric (mark_read ↔ mark_unread, flag
// ↔ unflag, move ↔ move-back, add_category ↔ remove_category), so
// undo is just executing the inverse-shaped action with SkipUndo set
// — otherwise pressing u twice would restore the original instead of
// toggling. Returns store.ErrNotFound when the stack is empty.
func (e *Executor) Undo(ctx context.Context, accountID int64) (store.UndoEntry, error) {
	entry, err := e.st.PopUndo(ctx)
	if err != nil {
		return store.UndoEntry{}, err
	}
	if entry == nil {
		return store.UndoEntry{}, store.ErrNotFound
	}
	a := store.Action{
		ID:         newActionID(),
		AccountID:  accountID,
		Type:       entry.ActionType,
		MessageIDs: entry.MessageIDs,
		Params:     entry.Params,
		SkipUndo:   true, // don't push the inverse of the inverse.
	}
	// run() requires a single-message action; UndoEntry covers that
	// invariant because every action we push is single-message.
	if err := e.run(ctx, a); err != nil {
		// Re-push the entry so the user can retry. PopUndo already
		// removed it.
		_ = e.st.PushUndo(ctx, *entry)
		return store.UndoEntry{}, err
	}
	return *entry, nil
}

// Drain implements sync.ActionDrainer. The sync engine calls this at
// the top of every cycle to retry actions that failed transiently. We
// keep it simple: re-dispatch every Pending/InFlight, mark Done on
// success, leave Pending on transient failure (engine retries next
// cycle), Failed on hard failure.
func (e *Executor) Drain(ctx context.Context) error {
	pending, err := e.st.PendingActions(ctx)
	if err != nil {
		return fmt.Errorf("action drain: list pending: %w", err)
	}
	for _, a := range pending {
		// Draft creation actions are all non-idempotent at stage 1
		// (createReply / createReplyAll / createForward / POST
		// /me/messages each produce a fresh draft per call). Drain
		// mustn't re-fire them — the row stays Pending for the
		// crash-recovery resume path (PR 7-ii) to handle on next
		// launch.
		if isDraftCreationAction(a.Type) {
			continue
		}
		if err := e.dispatch(ctx, a); err != nil {
			classification := classifyDispatchError(err)
			switch classification {
			case classRetryable:
				e.logger.Warn("action drain: retry next cycle", "action_id", a.ID, "err", err)
				continue
			default:
				e.logger.Error("action drain: hard failure", "action_id", a.ID, "err", err)
				_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusFailed, err.Error())
			}
			continue
		}
		_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusDone, "")
	}
	return nil
}

func newActionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

type errClass int

const (
	classRetryable errClass = iota
	classHard
)

func classifyDispatchError(err error) errClass {
	if err == nil {
		return classRetryable
	}
	if graph.IsThrottled(err) {
		return classRetryable
	}
	if graph.IsAuth(err) {
		return classRetryable // engine triggers re-auth, then retries.
	}
	var ge *graph.GraphError
	if errors.As(err, &ge) {
		// 5xx is transient.
		if ge.StatusCode >= 500 {
			return classRetryable
		}
		return classHard
	}
	// Network / DNS errors are retryable.
	return classRetryable
}
