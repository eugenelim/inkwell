package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// AttachmentRef identifies a local file to attach to the draft.
// LocalPath must be an absolute, clean path to a regular file.
// The path-traversal guard in safeReadFile enforces this contract.
// Spec 15 §5 / spec 17 §4.4.
type AttachmentRef struct {
	LocalPath string `json:"local_path"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
}

// safeReadFile reads a local file for attachment upload. It enforces
// spec 17 §4.4 path-traversal invariants: the path must be absolute,
// clean (no ".." components), and point to a regular file (not a
// symlink or directory). Files exceeding maxBytes are rejected to
// prevent accidentally uploading large binaries.
func safeReadFile(path string, maxBytes int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("attachment: path must be absolute: %q", path)
	}
	if clean := filepath.Clean(path); clean != path {
		return nil, fmt.Errorf("attachment: path contains traversal components: %q", path)
	}
	// Lstat does not follow symlinks — reject symlinks so a
	// crafted path can't point outside the intended tree.
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("attachment: stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("attachment: %q is not a regular file (mode %s)", path, info.Mode())
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("attachment: %q is %.1f MB, exceeds %.1f MB limit",
			path, float64(info.Size())/(1<<20), float64(maxBytes)/(1<<20))
	}
	// #nosec G304 — path is validated above: absolute, clean (no ".."),
	// Lstat-confirmed regular file. The caller provides the path from
	// a UI attachment-ref that the user explicitly selected; not from
	// untrusted user input directly.
	return os.ReadFile(path)
}

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
func (e *Executor) CreateDraftReply(ctx context.Context, accountID int64, sourceMessageID, body string, to, cc, bcc []string, subject string, attachments []AttachmentRef) (*DraftResult, error) {
	return e.createDraftFromSource(ctx, accountID, sourceMessageID, body, to, cc, bcc, subject, attachments,
		store.ActionCreateDraftReply, e.gc.CreateReply)
}

// CreateDraftReplyAll mirrors CreateDraftReply but stage 1 calls
// `/me/messages/{id}/createReplyAll`. The full audience comes from
// Graph's server-side dedup; the UI still passes its own to/cc/bcc
// to PATCH because the user may have curated the recipients
// (removed someone, added a Bcc) before pressing save. Spec 15 §5 /
// PR 7-iii.
func (e *Executor) CreateDraftReplyAll(ctx context.Context, accountID int64, sourceMessageID, body string, to, cc, bcc []string, subject string, attachments []AttachmentRef) (*DraftResult, error) {
	return e.createDraftFromSource(ctx, accountID, sourceMessageID, body, to, cc, bcc, subject, attachments,
		store.ActionCreateDraftReplyAll, e.gc.CreateReplyAll)
}

// CreateDraftForward mirrors CreateDraftReply but stage 1 calls
// `/me/messages/{id}/createForward`. Graph generates the
// "Forwarded message" header block + quote chain; the UI's
// to/cc/bcc / body / subject overlay onto that via the stage 2
// PATCH. Spec 15 §5 / PR 7-iii.
func (e *Executor) CreateDraftForward(ctx context.Context, accountID int64, sourceMessageID, body string, to, cc, bcc []string, subject string, attachments []AttachmentRef) (*DraftResult, error) {
	return e.createDraftFromSource(ctx, accountID, sourceMessageID, body, to, cc, bcc, subject, attachments,
		store.ActionCreateDraftForward, e.gc.CreateForward)
}

// CreateNewDraft enqueues a brand-new (no source) draft. Single-
// stage: POST /me/messages carries the full payload in one shot
// so there's no createX→PATCH dance. Spec 15 §5 / PR 7-iii.
//
// Drain still skips this type because the POST is non-idempotent
// (a retry produces a duplicate draft). PR 7-ii's crash-recovery
// resume path knows from the absence of `draft_id` in Params that
// stage 1 never landed and is the right place to either re-fire
// or surface to the user.
func (e *Executor) CreateNewDraft(ctx context.Context, accountID int64, body string, to, cc, bcc []string, subject string, attachments []AttachmentRef) (*DraftResult, error) {
	a := store.Action{
		ID:        newActionID(),
		AccountID: accountID,
		Type:      store.ActionCreateDraft,
		Status:    store.StatusPending,
		CreatedAt: time.Now(),
		Params: map[string]any{
			"body":    body,
			"to":      stringSliceParam(to),
			"cc":      stringSliceParam(cc),
			"bcc":     stringSliceParam(bcc),
			"subject": subject,
		},
		SkipUndo: true,
	}
	if err := e.st.EnqueueAction(ctx, a); err != nil {
		return nil, fmt.Errorf("draft: enqueue: %w", err)
	}
	ref, err := e.gc.CreateNewDraft(ctx, subject, body, to, cc, bcc)
	if err != nil {
		_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusFailed, err.Error())
		return nil, fmt.Errorf("createNewDraft: %w", err)
	}
	a.Params["draft_id"] = ref.ID
	a.Params["web_link"] = ref.WebLink
	if err := e.st.UpdateActionParams(ctx, a.ID, a.Params); err != nil {
		e.logger.Warn("draft: persist params failed", "action_id", a.ID, "err", err.Error())
	}
	out := &DraftResult{ID: ref.ID, WebLink: ref.WebLink}
	if err := e.uploadAttachments(ctx, ref.ID, attachments); err != nil {
		_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusFailed, err.Error())
		return out, fmt.Errorf("draft created, attachment upload failed: %w", err)
	}
	if err := e.st.UpdateActionStatus(ctx, a.ID, store.StatusDone, ""); err != nil {
		e.logger.Warn("draft: status update failed", "action_id", a.ID, "err", err.Error())
	}
	return out, nil
}

// DiscardDraft deletes a server-side draft created by this session.
// Issues DELETE /me/messages/{draftID}; 404 is treated as success
// so the UI can call this idempotently. Spec 15 §6.3 / F-1.
func (e *Executor) DiscardDraft(ctx context.Context, accountID int64, draftID string) error {
	if draftID == "" {
		return fmt.Errorf("discard_draft: empty draft id")
	}
	a := store.Action{
		ID:        newActionID(),
		AccountID: accountID,
		Type:      store.ActionDiscardDraft,
		Status:    store.StatusPending,
		CreatedAt: time.Now(),
		Params:    map[string]any{"draft_id": draftID},
		SkipUndo:  true,
	}
	if err := e.st.EnqueueAction(ctx, a); err != nil {
		return fmt.Errorf("discard_draft: enqueue: %w", err)
	}
	if err := e.gc.DeleteDraft(ctx, draftID); err != nil {
		_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusFailed, err.Error())
		return fmt.Errorf("discard_draft: %w", err)
	}
	if err := e.st.UpdateActionStatus(ctx, a.ID, store.StatusDone, ""); err != nil {
		e.logger.Warn("discard_draft: status update failed", "action_id", a.ID, "err", err.Error())
	}
	return nil
}

// createDraftFromSource is the two-stage Reply/ReplyAll/Forward
// shared executor body. The kind + stage1 fn parameterise the
// three flavours; everything else (action enqueue, params persist,
// stage 2 PATCH, attachment upload, status transitions) is identical.
func (e *Executor) createDraftFromSource(ctx context.Context, accountID int64, sourceMessageID, body string, to, cc, bcc []string, subject string, attachments []AttachmentRef, kind store.ActionType, stage1 func(context.Context, string) (*graph.DraftRef, error)) (*DraftResult, error) {
	if sourceMessageID == "" {
		return nil, fmt.Errorf("draft: empty source message id")
	}
	a := store.Action{
		ID:        newActionID(),
		AccountID: accountID,
		Type:      kind,
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
		SkipUndo: true,
	}
	if err := e.st.EnqueueAction(ctx, a); err != nil {
		return nil, fmt.Errorf("draft: enqueue: %w", err)
	}

	ref, err := stage1(ctx, sourceMessageID)
	if err != nil {
		_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusFailed, err.Error())
		return nil, fmt.Errorf("%s: %w", kind, err)
	}
	a.Params["draft_id"] = ref.ID
	a.Params["web_link"] = ref.WebLink
	if err := e.st.UpdateActionParams(ctx, a.ID, a.Params); err != nil {
		e.logger.Warn("draft: persist params failed", "action_id", a.ID, "err", err.Error())
	}

	out := &DraftResult{ID: ref.ID, WebLink: ref.WebLink}
	if err := e.gc.PatchMessageBody(ctx, ref.ID, body, to, cc, bcc, subject); err != nil {
		_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusFailed, err.Error())
		return out, fmt.Errorf("draft created, body update failed: %w", err)
	}
	if err := e.uploadAttachments(ctx, ref.ID, attachments); err != nil {
		_ = e.st.UpdateActionStatus(ctx, a.ID, store.StatusFailed, err.Error())
		return out, fmt.Errorf("draft created, attachment upload failed: %w", err)
	}
	if err := e.st.UpdateActionStatus(ctx, a.ID, store.StatusDone, ""); err != nil {
		e.logger.Warn("draft: status update failed", "action_id", a.ID, "err", err.Error())
	}
	return out, nil
}

// uploadAttachments reads each attachment from disk (path-traversal
// guard applied) and POSTs it to Graph. Fails fast on the first
// error; the caller marks the action Failed and returns the draft
// result (with WebLink) so the user can finish in Outlook.
func (e *Executor) uploadAttachments(ctx context.Context, draftID string, attachments []AttachmentRef) error {
	const defaultMaxBytes = 25 * 1024 * 1024 // 25 MB matches compose default
	maxBytes := int64(e.composeCfg.AttachmentMaxSizeMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	for _, att := range attachments {
		name := att.Name
		if name == "" {
			name = filepath.Base(att.LocalPath)
		}
		data, err := safeReadFile(att.LocalPath, maxBytes)
		if err != nil {
			return fmt.Errorf("attachment %q: %w", name, err)
		}
		if err := e.gc.AddDraftAttachment(ctx, draftID, name, data); err != nil {
			return fmt.Errorf("attachment %q: graph: %w", name, err)
		}
	}
	return nil
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
