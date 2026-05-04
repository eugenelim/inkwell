package sync

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// backfillFolder pulls one page of messages OLDER than `before` from
// the non-delta /messages endpoint. The UI calls this when the user
// scrolls past the cache wall — read-ahead pattern: one batch per
// trigger, debounced by the UI's wallSyncRequested flag, repeat as
// the user scrolls deeper.
//
// Filter: receivedDateTime lt <before> — strictly older than the
// user's oldest currently-cached message. Order desc so the
// most-recent-of-older arrives first (matches the user's scroll
// direction). Top 100 is a single Graph page; we do NOT follow
// nextLink here — that would block the foreground for thousands
// of HTTP calls on a deep account. The next scroll-to-wall fires
// the next page.
//
// When `before` is the zero time, the call falls back to "newest
// 100" — useful for tests and as a sane no-op when the UI hasn't
// loaded anything yet.
func (e *engine) backfillFolder(ctx context.Context, folderID string, before time.Time) error {
	opts := graph.ListMessagesOpts{
		Top:     100,
		OrderBy: "receivedDateTime desc",
	}
	if !before.IsZero() {
		opts.Filter = fmt.Sprintf("receivedDateTime lt %s", before.UTC().Format(time.RFC3339))
	}
	page, err := e.gc.ListMessagesInFolder(ctx, folderID, opts)
	if err != nil {
		return err
	}
	// Always emit FolderSyncedEvent — even on zero results — so the
	// UI can react. Without this, a user who scrolls to the cache
	// wall on a truly-exhausted mailbox never receives an event,
	// the list pane's wallSyncRequested flag never clears, and j
	// presses appear to do nothing. Real-tenant regression
	// reported on v0.14.x.
	if len(page.Value) == 0 {
		e.emit(FolderSyncedEvent{
			FolderID: folderID,
			Added:    0,
			At:       time.Now(),
		})
		return nil
	}
	batch := make([]store.Message, 0, len(page.Value))
	for _, m := range page.Value {
		sm, _ := e.toStoreMessage(folderID, m)
		batch = append(batch, sm)
	}
	if err := e.st.UpsertMessagesBatch(ctx, batch); err != nil {
		return err
	}
	e.emit(FolderSyncedEvent{
		FolderID: folderID,
		Added:    len(batch),
		At:       time.Now(),
	})
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
	for _, f := range orderForQuickStart(filterSubscribed(folders, e.opts.SubscribedFolders, e.opts.ExcludedFolders)) {
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
		ID:                 m.ID,
		AccountID:          e.opts.AccountID,
		FolderID:           parent,
		InternetMessageID:  m.InternetMessageID,
		ConversationID:     m.ConversationID,
		ConversationIndex:  m.ConversationIndex,
		Subject:            m.Subject,
		BodyPreview:        m.BodyPreview,
		ReceivedAt:         m.ReceivedDateTime,
		SentAt:             m.SentDateTime,
		IsRead:             m.IsRead,
		IsDraft:            m.IsDraft,
		Importance:         m.Importance,
		InferenceClass:     m.InferenceClassification,
		HasAttachments:     m.HasAttachments,
		Categories:         m.Categories,
		WebLink:            m.WebLink,
		LastModifiedAt:     m.LastModifiedDateTime,
		MeetingMessageType: m.MeetingMessageType,
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
		if t := m.Flag.DueDateTime.ToTime(); !t.IsZero() {
			sm.FlagDueAt = t
		}
		if t := m.Flag.CompletedDateTime.ToTime(); !t.IsZero() {
			sm.FlagCompletedAt = t
		}
	}
	return sm, true
}
