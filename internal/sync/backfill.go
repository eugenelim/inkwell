package sync

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// backfillFolder pulls a folder's messages back to `until` from the
// non-delta /messages endpoint. Spec §5.4: this is the older-on-demand
// path triggered by `:backfill <folder> <duration>`. It paginates to
// completion (foreground-blocking) and is NOT used on first-launch —
// first-launch goes through the lazy progressive path in syncFolder
// (spec §5.2).
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

// QuickStartBackfill runs the spec §5 first-launch path: enumerate
// folders, then pull last-50 from Inbox first, then sequentially from
// the rest of the subscribed set. Each call to syncFolder yields after
// one page (drainOnePageOnly). Subsequent sync ticks drain
// `delta_tokens.next_link` for any folder still mid-pagination.
func (e *engine) QuickStartBackfill(ctx context.Context) error {
	if err := e.syncFolders(ctx); err != nil {
		return err
	}
	folders, err := e.st.ListFolders(ctx, e.opts.AccountID)
	if err != nil {
		return err
	}
	for _, f := range orderForQuickStart(filterSubscribed(folders, e.opts.SubscribedFolders)) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := e.syncFolder(ctx, f.ID); err != nil {
			// Per-folder failures are logged and skipped; the next
			// tick retries (next_link is preserved across runs).
			e.logger.Warn("sync: quick-start folder failed",
				"folder_id", f.ID,
				"err", err.Error(),
			)
		}
	}
	return nil
}

// orderForQuickStart returns folders in the §5.1 quick-start order:
// Inbox → Sent → Drafts → Archive → user folders (alphabetical).
func orderForQuickStart(in []store.Folder) []store.Folder {
	priority := map[string]int{
		"inbox":     0,
		"sentitems": 1,
		"drafts":    2,
		"archive":   3,
	}
	out := make([]store.Folder, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		pi, oki := priority[out[i].WellKnownName]
		pj, okj := priority[out[j].WellKnownName]
		switch {
		case oki && okj:
			return pi < pj
		case oki:
			return true
		case okj:
			return false
		default:
			return out[i].DisplayName < out[j].DisplayName
		}
	})
	return out
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
