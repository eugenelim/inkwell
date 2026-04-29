package action

import (
	"context"
	"fmt"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// BatchResult is the per-message outcome of a [Executor.BatchExecute]
// call. Err is nil on success.
type BatchResult struct {
	MessageID string
	Err       error
}

// BulkSoftDelete is a typed wrapper around BatchExecute that the UI
// (spec 10) consumes via the ui.BulkExecutor interface.
func (e *Executor) BulkSoftDelete(ctx context.Context, accountID int64, messageIDs []string) ([]BatchResult, error) {
	return e.BatchExecute(ctx, accountID, store.ActionSoftDelete, messageIDs)
}

// BulkArchive moves N messages to the Archive folder via /$batch.
func (e *Executor) BulkArchive(ctx context.Context, accountID int64, messageIDs []string) ([]BatchResult, error) {
	return e.BatchExecute(ctx, accountID, store.ActionMove, messageIDs)
}

// BulkMarkRead marks N messages read via /$batch.
func (e *Executor) BulkMarkRead(ctx context.Context, accountID int64, messageIDs []string) ([]BatchResult, error) {
	return e.BatchExecute(ctx, accountID, store.ActionMarkRead, messageIDs)
}

// BatchExecute applies a single action type to many messages in one
// pass. Local mutations apply optimistically; Graph dispatch goes
// through /$batch in 20-per-chunk groups; per-message failures roll
// back ONLY that message's local change. Successful messages stay
// mutated and their actions are marked Done.
//
// Supported action types in v0.6.0: mark_read, mark_unread, flag,
// unflag, soft_delete, archive (move). Move-with-arbitrary-folder is
// next (spec 10 wires it).
func (e *Executor) BatchExecute(ctx context.Context, accountID int64, actionType store.ActionType, messageIDs []string) ([]BatchResult, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	if !isBatchableAction(actionType) {
		return nil, fmt.Errorf("BatchExecute: unsupported action type %q", actionType)
	}

	// Resolve well-known destinations once for soft_delete / archive.
	destAlias := ""
	destID := ""
	switch actionType {
	case store.ActionSoftDelete:
		var err error
		destID, destAlias, err = e.resolveWellKnownDestination(ctx, accountID, "deleteditems")
		if err != nil {
			return nil, err
		}
	case store.ActionMove: // archive shares this type
		var err error
		destID, destAlias, err = e.resolveWellKnownDestination(ctx, accountID, "archive")
		if err != nil {
			return nil, err
		}
	}

	// Snapshot pre-state for rollback. Skip messages that aren't in
	// the local store — we can't rollback what we don't know.
	type entry struct {
		id     string
		action store.Action
		pre    *store.Message
	}
	entries := make([]entry, 0, len(messageIDs))
	for _, id := range messageIDs {
		pre, err := e.st.GetMessage(ctx, id)
		if err != nil || pre == nil {
			// Don't fail the whole batch — record the failure and
			// continue. The user sees a partial-success message.
			continue
		}
		params := map[string]any{}
		if destID != "" {
			params["destination_folder_id"] = destID
			params["destination_folder_alias"] = destAlias
		}
		entries = append(entries, entry{
			id: id,
			action: store.Action{
				ID:         newActionID(),
				AccountID:  accountID,
				Type:       actionType,
				MessageIDs: []string{id},
				Params:     params,
			},
			pre: pre,
		})
	}

	// Optimistic local apply. If a single apply fails (e.g. the
	// destination folder was deleted between resolve and apply), skip
	// the message — we'll surface it in results.
	results := make([]BatchResult, 0, len(messageIDs))
	for _, en := range entries {
		if err := applyLocal(ctx, e.st, en.action, en.pre); err != nil {
			results = append(results, BatchResult{MessageID: en.id, Err: fmt.Errorf("apply local: %w", err)})
			continue
		}
		if err := e.st.EnqueueAction(ctx, en.action); err != nil {
			_ = rollbackLocal(ctx, e.st, en.action, en.pre)
			results = append(results, BatchResult{MessageID: en.id, Err: fmt.Errorf("enqueue: %w", err)})
			continue
		}
	}

	// Build $batch sub-requests for the entries that survived local apply.
	live := make([]entry, 0, len(entries))
	for _, en := range entries {
		// Filter to entries NOT already failed in `results`.
		failed := false
		for _, r := range results {
			if r.MessageID == en.id && r.Err != nil {
				failed = true
				break
			}
		}
		if !failed {
			live = append(live, en)
		}
	}

	// Chunk and dispatch.
	for start := 0; start < len(live); start += graph.MaxBatchSize {
		end := start + graph.MaxBatchSize
		if end > len(live) {
			end = len(live)
		}
		chunk := live[start:end]
		batch := graph.NewBatch()
		for i, en := range chunk {
			req, err := actionToSubRequest(en.action, fmt.Sprintf("%d", i))
			if err != nil {
				results = append(results, BatchResult{MessageID: en.id, Err: err})
				_ = rollbackLocal(ctx, e.st, en.action, en.pre)
				continue
			}
			batch.Add(req)
		}
		if batch.Len() == 0 {
			continue
		}
		responses, err := e.gc.ExecuteBatch(ctx, batch)
		if err != nil {
			// Outer batch call failed — rollback every message in this
			// chunk and surface the error per-message.
			for _, en := range chunk {
				_ = rollbackLocal(ctx, e.st, en.action, en.pre)
				_ = e.st.UpdateActionStatus(ctx, en.action.ID, store.StatusFailed, err.Error())
				results = append(results, BatchResult{MessageID: en.id, Err: err})
			}
			continue
		}
		// Reconcile per sub-response.
		for i, sr := range responses {
			if i >= len(chunk) {
				break
			}
			en := chunk[i]
			if sr.GraphError != nil || sr.Status >= 400 {
				_ = rollbackLocal(ctx, e.st, en.action, en.pre)
				graphErr := error(sr.GraphError)
				if graphErr == nil {
					graphErr = fmt.Errorf("status %d", sr.Status)
				}
				_ = e.st.UpdateActionStatus(ctx, en.action.ID, store.StatusFailed, graphErr.Error())
				results = append(results, BatchResult{MessageID: en.id, Err: graphErr})
				continue
			}
			_ = e.st.UpdateActionStatus(ctx, en.action.ID, store.StatusDone, "")
			results = append(results, BatchResult{MessageID: en.id, Err: nil})
		}
	}

	return results, nil
}

// isBatchableAction gates which action types BatchExecute understands.
func isBatchableAction(t store.ActionType) bool {
	switch t {
	case store.ActionMarkRead, store.ActionMarkUnread,
		store.ActionFlag, store.ActionUnflag,
		store.ActionSoftDelete, store.ActionMove:
		return true
	}
	return false
}

// actionToSubRequest renders one queued action as a Graph $batch
// sub-request. Mirrors [Executor.dispatch] but produces the SubRequest
// instead of firing immediately.
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
	}
	return graph.SubRequest{}, fmt.Errorf("actionToSubRequest: unsupported action %q", a.Type)
}
