package action

import (
	"context"
	"fmt"
	"strings"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// BatchResult is the per-message outcome of a bulk batch operation.
// Err is nil on success.
type BatchResult struct {
	MessageID string
	Err       error
}

// BulkSoftDelete moves N messages to Deleted Items via /$batch.
// Implements the ui.BulkExecutor interface (spec 10).
func (e *Executor) BulkSoftDelete(ctx context.Context, accountID int64, messageIDs []string) ([]BatchResult, error) {
	return e.batchExecute(ctx, accountID, store.ActionSoftDelete, messageIDs, nil, false)
}

// BulkArchive moves N messages to the Archive folder via /$batch.
func (e *Executor) BulkArchive(ctx context.Context, accountID int64, messageIDs []string) ([]BatchResult, error) {
	return e.batchExecute(ctx, accountID, store.ActionMove, messageIDs, nil, false)
}

// BulkMarkRead marks N messages read via /$batch.
func (e *Executor) BulkMarkRead(ctx context.Context, accountID int64, messageIDs []string) ([]BatchResult, error) {
	return e.batchExecute(ctx, accountID, store.ActionMarkRead, messageIDs, nil, false)
}

// BulkMarkUnread marks N messages unread via /$batch.
func (e *Executor) BulkMarkUnread(ctx context.Context, accountID int64, messageIDs []string) ([]BatchResult, error) {
	return e.batchExecute(ctx, accountID, store.ActionMarkUnread, messageIDs, nil, false)
}

// BulkFlag flags N messages via /$batch.
func (e *Executor) BulkFlag(ctx context.Context, accountID int64, messageIDs []string) ([]BatchResult, error) {
	return e.batchExecute(ctx, accountID, store.ActionFlag, messageIDs, nil, false)
}

// BulkUnflag unflags N messages via /$batch.
func (e *Executor) BulkUnflag(ctx context.Context, accountID int64, messageIDs []string) ([]BatchResult, error) {
	return e.batchExecute(ctx, accountID, store.ActionUnflag, messageIDs, nil, false)
}

// BulkPermanentDelete permanently removes N messages via /$batch.
// Irreversible — callers MUST gate this behind a confirm modal.
func (e *Executor) BulkPermanentDelete(ctx context.Context, accountID int64, messageIDs []string) ([]BatchResult, error) {
	return e.batchExecute(ctx, accountID, store.ActionPermanentDelete, messageIDs, nil, false)
}

// BulkAddCategory tags N messages with the given category via /$batch.
func (e *Executor) BulkAddCategory(ctx context.Context, accountID int64, messageIDs []string, category string) ([]BatchResult, error) {
	if strings.TrimSpace(category) == "" {
		return nil, fmt.Errorf("bulk_add_category: category required")
	}
	return e.batchExecute(ctx, accountID, store.ActionAddCategory, messageIDs, map[string]any{"category": category}, false)
}

// BulkRemoveCategory untags N messages from the given category via /$batch.
func (e *Executor) BulkRemoveCategory(ctx context.Context, accountID int64, messageIDs []string, category string) ([]BatchResult, error) {
	if strings.TrimSpace(category) == "" {
		return nil, fmt.Errorf("bulk_remove_category: category required")
	}
	return e.batchExecute(ctx, accountID, store.ActionRemoveCategory, messageIDs, map[string]any{"category": category}, false)
}

// BulkMove moves N messages to the user-specified destination folder via /$batch.
// destFolderID is the Graph folder ID; destAlias is the well-known name if known
// (e.g. "archive", "deleteditems") — the Graph API accepts either.
func (e *Executor) BulkMove(ctx context.Context, accountID int64, messageIDs []string, destFolderID, destAlias string) ([]BatchResult, error) {
	if destFolderID == "" && destAlias == "" {
		return nil, fmt.Errorf("bulk_move: destination required")
	}
	dest := destFolderID
	if dest == "" {
		dest = destAlias
	}
	return e.batchExecute(ctx, accountID, store.ActionMove, messageIDs, map[string]any{
		"destination_folder_id":    dest,
		"destination_folder_alias": destAlias,
	}, false)
}

// BatchExecute applies a single action type to many messages via /$batch.
// Local mutations apply optimistically; per-message Graph failures roll back
// only that message. Successful messages are marked Done and get a composite
// undo entry for reversible action types.
func (e *Executor) BatchExecute(ctx context.Context, accountID int64, actionType store.ActionType, messageIDs []string) ([]BatchResult, error) {
	return e.batchExecute(ctx, accountID, actionType, messageIDs, nil, false)
}

// BatchExecuteWithParams is BatchExecute with caller-supplied extra
// per-message Params merged into each enqueued action. Spec 25
// uses this to pass `category` for `add_category` /
// `remove_category` thread / bulk ops without round-tripping
// through a wider public surface.
func (e *Executor) BatchExecuteWithParams(ctx context.Context, accountID int64, actionType store.ActionType, messageIDs []string, params map[string]any) ([]BatchResult, error) {
	return e.batchExecute(ctx, accountID, actionType, messageIDs, params, false)
}

// batchExecute is the shared implementation. extraParams are merged into
// each per-message action's Params (used for category operations).
// skipUndo suppresses the composite undo push (used by the Undo path).
func (e *Executor) batchExecute(ctx context.Context, accountID int64, actionType store.ActionType, messageIDs []string, extraParams map[string]any, skipUndo bool) ([]BatchResult, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	if !isBatchableAction(actionType) {
		return nil, fmt.Errorf("batchExecute: unsupported action type %q", actionType)
	}

	hardMax := e.batchCfg.HardMax
	if hardMax <= 0 {
		hardMax = 5000
	}
	if len(messageIDs) > hardMax {
		return nil, fmt.Errorf("batchExecute: %d messages exceeds hard max %d", len(messageIDs), hardMax)
	}

	// Resolve well-known destinations once for move-like actions,
	// unless the caller already provided a destination in extraParams
	// (e.g. BulkMove to a user-chosen folder).
	destID, destAlias := "", ""
	switch actionType {
	case store.ActionSoftDelete:
		var err error
		destID, destAlias, err = e.resolveWellKnownDestination(ctx, accountID, "deleteditems")
		if err != nil {
			return nil, err
		}
	case store.ActionMove:
		if _, hasExplicit := extraParams["destination_folder_id"]; !hasExplicit {
			var err error
			destID, destAlias, err = e.resolveWellKnownDestination(ctx, accountID, "archive")
			if err != nil {
				return nil, err
			}
		}
	}

	type entry struct {
		id     string
		action store.Action
		pre    *store.Message
	}

	results := make([]BatchResult, 0, len(messageIDs))
	live := make([]entry, 0, len(messageIDs))

	for _, id := range messageIDs {
		pre, err := e.st.GetMessage(ctx, id)
		if err != nil || pre == nil {
			results = append(results, BatchResult{MessageID: id, Err: fmt.Errorf("snapshot: message not found")})
			continue
		}
		params := make(map[string]any, len(extraParams)+2)
		for k, v := range extraParams {
			params[k] = v
		}
		if destID != "" {
			params["destination_folder_id"] = destID
			params["destination_folder_alias"] = destAlias
		}
		en := entry{
			id: id,
			action: store.Action{
				ID:         newActionID(),
				AccountID:  accountID,
				Type:       actionType,
				MessageIDs: []string{id},
				Params:     params,
			},
			pre: pre,
		}
		if err := applyLocal(ctx, e.st, en.action, pre); err != nil {
			results = append(results, BatchResult{MessageID: id, Err: fmt.Errorf("apply local: %w", err)})
			continue
		}
		// Capture post-apply categories for the $batch PATCH body.
		if actionType == store.ActionAddCategory || actionType == store.ActionRemoveCategory {
			if row, rErr := e.st.GetMessage(ctx, id); rErr == nil && row != nil {
				en.action.Params["post_apply_categories"] = row.Categories
			}
		}
		if err := e.st.EnqueueAction(ctx, en.action); err != nil {
			_ = rollbackLocal(ctx, e.st, en.action, pre)
			results = append(results, BatchResult{MessageID: id, Err: fmt.Errorf("enqueue: %w", err)})
			continue
		}
		live = append(live, en)
	}

	if len(live) == 0 {
		return results, nil
	}

	// Build sub-requests. reqToEntry maps sub-request ID → entry.
	allReqs := make([]graph.SubRequest, 0, len(live))
	reqToEntry := make(map[string]entry, len(live))
	for i, en := range live {
		reqID := fmt.Sprintf("%d", i)
		req, err := actionToSubRequest(en.action, reqID)
		if err != nil {
			_ = rollbackLocal(ctx, e.st, en.action, en.pre)
			_ = e.st.UpdateActionStatus(ctx, en.action.ID, store.StatusFailed, err.Error())
			results = append(results, BatchResult{MessageID: en.id, Err: err})
			continue
		}
		allReqs = append(allReqs, req)
		reqToEntry[reqID] = en
	}

	if len(allReqs) == 0 {
		return results, nil
	}

	concurrency := e.batchCfg.Concurrency
	if concurrency <= 0 {
		concurrency = 3
	}
	maxRetries := e.batchCfg.MaxRetriesPerSubrequest
	if maxRetries <= 0 {
		maxRetries = 5
	}

	// ExecuteAll converts per-chunk outer errors to per-response GraphErrors;
	// the returned error is always nil.
	responses, _ := e.gc.ExecuteAll(ctx, allReqs, graph.ExecuteAllOpts{
		Concurrency: concurrency,
		MaxRetries:  maxRetries,
	})

	successIDs := make([]string, 0, len(allReqs))
	for _, sr := range responses {
		en, ok := reqToEntry[sr.ID]
		if !ok {
			continue
		}
		if sr.GraphError != nil || sr.Status >= 400 {
			_ = rollbackLocal(ctx, e.st, en.action, en.pre)
			// Avoid the typed-nil-interface gotcha (staticcheck SA4023):
			// assign the concrete *GraphError to error only when non-nil.
			var graphErr error
			if sr.GraphError != nil {
				graphErr = sr.GraphError
			} else {
				graphErr = fmt.Errorf("status %d", sr.Status)
			}
			_ = e.st.UpdateActionStatus(ctx, en.action.ID, store.StatusFailed, graphErr.Error())
			results = append(results, BatchResult{MessageID: en.id, Err: graphErr})
		} else {
			_ = e.st.UpdateActionStatus(ctx, en.action.ID, store.StatusDone, "")
			results = append(results, BatchResult{MessageID: en.id})
			successIDs = append(successIDs, en.id)
		}
	}

	// Push composite undo entry for reversible action types.
	if !skipUndo && len(successIDs) > 0 {
		if invType, ok := bulkInverseType(actionType); ok {
			undo := store.UndoEntry{
				ActionType: invType,
				MessageIDs: successIDs,
				Params:     bulkInverseParams(actionType, extraParams),
				Label:      bulkUndoLabel(actionType, len(successIDs)),
			}
			if err := e.st.PushUndo(ctx, undo); err != nil {
				e.logger.Warn("batchExecute: undo push failed", "action_type", actionType, "err", err)
			}
		}
	}

	return results, nil
}

// isBatchableAction gates which action types batchExecute understands.
func isBatchableAction(t store.ActionType) bool {
	switch t {
	case store.ActionMarkRead, store.ActionMarkUnread,
		store.ActionFlag, store.ActionUnflag,
		store.ActionSoftDelete, store.ActionMove,
		store.ActionPermanentDelete,
		store.ActionAddCategory, store.ActionRemoveCategory:
		return true
	}
	return false
}

// actionToSubRequest renders one queued action as a Graph $batch sub-request.
func actionToSubRequest(a store.Action, id string) (graph.SubRequest, error) {
	if len(a.MessageIDs) != 1 {
		return graph.SubRequest{}, fmt.Errorf("actionToSubRequest: expected single message id, got %d", len(a.MessageIDs))
	}
	mid := a.MessageIDs[0]
	switch a.Type {
	case store.ActionMarkRead:
		return graph.SubRequest{
			ID: id, Method: "PATCH", URL: "/me/messages/" + mid,
			Body: map[string]any{"isRead": true},
		}, nil
	case store.ActionMarkUnread:
		return graph.SubRequest{
			ID: id, Method: "PATCH", URL: "/me/messages/" + mid,
			Body: map[string]any{"isRead": false},
		}, nil
	case store.ActionFlag:
		return graph.SubRequest{
			ID: id, Method: "PATCH", URL: "/me/messages/" + mid,
			Body: map[string]any{"flag": map[string]any{"flagStatus": "flagged"}},
		}, nil
	case store.ActionUnflag:
		return graph.SubRequest{
			ID: id, Method: "PATCH", URL: "/me/messages/" + mid,
			Body: map[string]any{"flag": map[string]any{"flagStatus": "notFlagged"}},
		}, nil
	case store.ActionSoftDelete, store.ActionMove:
		dest := paramString(a.Params, "destination_folder_alias")
		if dest == "" {
			dest = paramString(a.Params, "destination_folder_id")
		}
		if dest == "" {
			return graph.SubRequest{}, fmt.Errorf("move: missing destination")
		}
		return graph.SubRequest{
			ID: id, Method: "POST", URL: "/me/messages/" + mid + "/move",
			Body: map[string]string{"destinationId": dest},
		}, nil
	case store.ActionPermanentDelete:
		return graph.SubRequest{
			ID: id, Method: "POST", URL: "/me/messages/" + mid + "/permanentDelete",
			Body: map[string]any{},
		}, nil
	case store.ActionAddCategory, store.ActionRemoveCategory:
		// Graph requires the full post-state categories list. We capture
		// it after applyLocal and store it in Params so this function stays
		// free of store access.
		cats, _ := a.Params["post_apply_categories"].([]string)
		if cats == nil {
			cats = []string{}
		}
		return graph.SubRequest{
			ID: id, Method: "PATCH", URL: "/me/messages/" + mid,
			Body: map[string]any{"categories": cats},
		}, nil
	}
	return graph.SubRequest{}, fmt.Errorf("actionToSubRequest: unsupported action %q", a.Type)
}

// bulkInverseType returns the inverse action type for undo, plus whether
// the operation is reversible. Move/softdelete/permanent_delete are not
// reversible for bulk.
func bulkInverseType(t store.ActionType) (store.ActionType, bool) {
	switch t {
	case store.ActionMarkRead:
		return store.ActionMarkUnread, true
	case store.ActionMarkUnread:
		return store.ActionMarkRead, true
	case store.ActionFlag:
		return store.ActionUnflag, true
	case store.ActionUnflag:
		return store.ActionFlag, true
	case store.ActionAddCategory:
		return store.ActionRemoveCategory, true
	case store.ActionRemoveCategory:
		return store.ActionAddCategory, true
	}
	return "", false
}

// bulkInverseParams extracts the params needed for the undo action.
func bulkInverseParams(t store.ActionType, params map[string]any) map[string]any {
	switch t {
	case store.ActionAddCategory, store.ActionRemoveCategory:
		cat, _ := params["category"].(string)
		return map[string]any{"category": cat}
	}
	return nil
}

// bulkUndoLabel returns a human-readable description for the undo stack entry.
func bulkUndoLabel(t store.ActionType, n int) string {
	switch t {
	case store.ActionMarkRead:
		return fmt.Sprintf("marked %d messages read", n)
	case store.ActionMarkUnread:
		return fmt.Sprintf("marked %d messages unread", n)
	case store.ActionFlag:
		return fmt.Sprintf("flagged %d messages", n)
	case store.ActionUnflag:
		return fmt.Sprintf("unflagged %d messages", n)
	case store.ActionAddCategory:
		return fmt.Sprintf("added category to %d messages", n)
	case store.ActionRemoveCategory:
		return fmt.Sprintf("removed category from %d messages", n)
	}
	return fmt.Sprintf("bulk %s on %d messages", t, n)
}
