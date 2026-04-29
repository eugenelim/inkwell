package action

import (
	"context"
	"fmt"
)

// DraftResult is what the UI gets back after a draft round-trips to
// Graph. WebLink opens the draft in Outlook (the spec 15 hand-off);
// ID is the server-assigned message id.
type DraftResult struct {
	ID      string
	WebLink string
}

// CreateDraftReply posts to /me/messages/{srcID}/createReply, then
// PATCHes the new draft's body + headers with the user-edited
// content. Two Graph round-trips; returns the webLink so the UI can
// surface "press s to open in Outlook".
//
// Failures: if createReply returns 4xx, no draft was created and we
// surface the error. If PATCH returns 4xx, the draft exists with
// Graph's auto-generated headers but the user's edits weren't
// applied — we still surface the error and the (incomplete) webLink
// so the user can finish in Outlook rather than lose their work.
func (e *Executor) CreateDraftReply(ctx context.Context, sourceMessageID, body string, to, cc, bcc []string, subject string) (*DraftResult, error) {
	if sourceMessageID == "" {
		return nil, fmt.Errorf("draft: empty source message id")
	}
	ref, err := e.gc.CreateReply(ctx, sourceMessageID)
	if err != nil {
		return nil, fmt.Errorf("createReply: %w", err)
	}
	patchErr := e.gc.PatchMessageBody(ctx, ref.ID, body, to, cc, bcc, subject)
	out := &DraftResult{ID: ref.ID, WebLink: ref.WebLink}
	if patchErr != nil {
		// Draft exists but our edits didn't land. Surface the error;
		// the webLink still works — user can finish in Outlook.
		return out, fmt.Errorf("draft created, body update failed: %w", patchErr)
	}
	return out, nil
}
