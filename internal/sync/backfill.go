package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// Backfill pulls a folder's messages back to `until`, paginating until
// exhausted. Spec §5.4 calls this for the older-than-90-days case.
// On first launch (spec §5), [InitialBackfill] is the entry point.
func (e *engine) backfillFolder(ctx context.Context, folderID string, until time.Time) error {
	filter := fmt.Sprintf("receivedDateTime ge %s", until.UTC().Format(time.RFC3339))
	page, err := e.gc.ListMessagesInFolder(ctx, folderID, graph.ListMessagesOpts{
		Top:    100,
		Filter: filter,
	})
	if err != nil {
		return err
	}
	for {
		var batch []store.Message
		for _, m := range page.Value {
			sm, _ := e.toStoreMessage(folderID, m)
			batch = append(batch, sm)
		}
		if len(batch) > 0 {
			if err := e.st.UpsertMessagesBatch(ctx, batch); err != nil {
				return err
			}
			e.emit(FolderSyncedEvent{
				FolderID: folderID,
				Added:    len(batch),
				At:       time.Now(),
			})
		}
		if page.NextLink == "" {
			break
		}
		page, err = e.gc.FollowNext(ctx, page.NextLink)
		if err != nil {
			return err
		}
	}
	return nil
}

// InitialBackfill runs the spec §5 90-day pull for every subscribed
// folder. The caller (typically cmd/inkwell signin or the engine's
// first-launch detection) drives this.
func (e *engine) InitialBackfill(ctx context.Context) error {
	if err := e.syncFolders(ctx); err != nil {
		return err
	}
	folders, err := e.st.ListFolders(ctx, e.opts.AccountID)
	if err != nil {
		return err
	}
	since := time.Now().AddDate(0, 0, -e.opts.BackfillDays)
	for _, f := range filterSubscribed(folders, e.opts.SubscribedFolders) {
		if err := e.backfillFolder(ctx, f.ID, since); err != nil {
			return fmt.Errorf("backfill folder %s: %w", f.ID, err)
		}
		// After backfill, fire one delta call to obtain the resume cursor.
		if err := e.syncFolderFresh(ctx, f.ID); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("delta seed folder %s: %w", f.ID, err)
		}
	}
	return nil
}

// toStoreMessage applies the mapping used by both delta and backfill
// paths. Returns true iff the message did not already exist locally.
func (e *engine) toStoreMessage(folderID string, m graph.Message) (store.Message, bool) {
	parent := m.ParentFolderID
	if parent == "" {
		parent = folderID
	}
	sm := store.Message{
		ID:                m.ID,
		AccountID:         e.opts.AccountID,
		FolderID:          parent,
		InternetMessageID: m.InternetMessageID,
		ConversationID:    m.ConversationID,
		ConversationIndex: m.ConversationIndex,
		Subject:           m.Subject,
		BodyPreview:       m.BodyPreview,
		ReceivedAt:        m.ReceivedDateTime,
		SentAt:            m.SentDateTime,
		IsRead:            m.IsRead,
		IsDraft:           m.IsDraft,
		Importance:        m.Importance,
		InferenceClass:    m.InferenceClassification,
		HasAttachments:    m.HasAttachments,
		Categories:        m.Categories,
		WebLink:           m.WebLink,
		LastModifiedAt:    m.LastModifiedDateTime,
	}
	if m.From != nil {
		sm.FromAddress = m.From.EmailAddress.Address
		sm.FromName = m.From.EmailAddress.Name
	}
	sm.ToAddresses = recipientsToStore(m.ToRecipients)
	sm.CcAddresses = recipientsToStore(m.CcRecipients)
	sm.BccAddresses = recipientsToStore(m.BccRecipients)
	if m.Flag != nil {
		sm.FlagStatus = m.Flag.FlagStatus
	}
	return sm, true
}
