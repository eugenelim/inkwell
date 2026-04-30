package action

import (
	"context"
	"fmt"
	"time"

	"github.com/eugenelim/inkwell/internal/store"
)

// DraftResult is what the UI gets back after a draft round-trips to
// Graph. WebLink opens the draft in Outlook (the spec 15 hand-off);
// ID is the server-assigned message id.
type DraftResult struct {
	ID      string
	WebLink string
}

// CreateDraftReply enqueues a draft-reply action and dispatches it
// in two stages:
//
//  1. POST /me/messages/{srcID}/createReply  → returns a server-
//     assigned draft id + webLink. We persist these into the
//     action's Params before stage 2 so a crash mid-flight can
//     resume from the recorded id rather than fire createReply
//     a second time and generate a duplicate draft (spec 15 §8 —
//     the v0.13.x audit row "drafts bypass action queue").
//  2. PATCH /me/messages/{draftID}            → body + headers.
//
// Failure handling:
//   - Stage 1 fails: action → Failed, no draft exists, surface
//     error to caller.
//   - Stage 2 fails after stage 1 succeeded: action → Failed with
//     draft_id recorded. Caller still receives DraftResult{ID,
//     WebLink} so the user can finish in Outlook (existing
//     contract). PR 7-ii's resume path will re-PATCH idempotently
//     on next launch.
//
// Drain (engine retry-on-cycle) explicitly skips ActionCreateDraftReply:
// stage 1's createReply is non-idempotent, so blind retry would
// produce duplicate drafts. PR 7-ii adds proper resume logic on
// startup that inspects the Params for a draft_id and stage-aware
// behaviour.
func (e *Executor) CreateDraftReply(ctx context.Context, accountID int64, sourceMessageID, body string, to, cc, bcc []string, subject string) (*DraftResult, error) {
	if sourceMessageID == "" {
		return nil, fmt.Errorf("draft: empty source message id")
	}
	a := store.Action{
		ID:        newActionID(),
		AccountID: accountID,
		Type:      store.ActionCreateDraftReply,
		Status:    store.StatusPending,
		CreatedAt: time.Now(),
		Params: map[string]any{
			"source_message_id": sourceMessageID,
			"body":              body,
			"to":                stringSliceParam(to),
			"cc":                stringSliceParam(cc),
			"bcc":               stringSliceParam(bcc),
			"subject":           subject,
		},
		// Drafts are not reversible from the undo stack — the user
		// finishes the draft (or discards) in Outlook. Skip-undo
		// keeps `u` from finding draft actions on the stack.
		SkipUndo: true,
	}
	if err := e.st.EnqueueAction(ctx, a); err != nil {
		return nil, fmt.Errorf("draft: enqueue: %w", err)
	}

	// Stage 1: createReply.
	ref, err := e.gc.CreateReply(ctx, sourceMessageID)
	if err != nil {
		_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusFailed, err.Error())
		return nil, fmt.Errorf("createReply: %w", err)
	}
	a.Params["draft_id"] = ref.ID
	a.Params["web_link"] = ref.WebLink
	if err := e.st.UpdateActionParams(ctx, a.ID, a.Params); err != nil {
		// The draft IS created server-side; we just couldn't persist
		// the id locally. Log + carry on — stage 2 still runs, and
		// the user can finish in Outlook either way. If we crash
		// before stage 2 lands the body, PR 7-ii's resume path
		// won't have a draft_id to find; it falls back to "skip
		// resume; rely on Drafts folder delta sync to surface the
		// stranded draft for the user to clean up in Outlook".
		e.logger.Warn("draft: persist params failed", "action_id", a.ID, "err", err.Error())
	}

	// Stage 2: PATCH body + headers.
	out := &DraftResult{ID: ref.ID, WebLink: ref.WebLink}
	if err := e.gc.PatchMessageBody(ctx, ref.ID, body, to, cc, bcc, subject); err != nil {
		_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusFailed, err.Error())
		// Existing contract: surface the error AND return DraftResult
		// so the caller can paint "press s to open in Outlook" with
		// a webLink to a draft that has Graph's auto-generated body.
		return out, fmt.Errorf("draft created, body update failed: %w", err)
	}
	if err := e.st.UpdateActionStatus(ctx, a.ID, store.StatusDone, ""); err != nil {
		e.logger.Warn("draft: status update failed", "action_id", a.ID, "err", err.Error())
	}
	return out, nil
}

// stringSliceParam normalises a []string for JSON storage in the
// action's Params blob. The encoding round-trip turns []string into
// []any (interface slice) on retrieval; storing as []any here keeps
// the resume path reading consistent shapes.
func stringSliceParam(s []string) []any {
	if len(s) == 0 {
		return nil
	}
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
