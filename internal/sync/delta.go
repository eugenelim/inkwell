package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// syncFolder runs the per-folder delta loop (spec §6). Initial call
// uses the /me/mailFolders/{id}/messages/delta endpoint; subsequent
// calls follow the persisted deltaLink.
func (e *engine) syncFolder(ctx context.Context, folderID string) error {
	tok, err := e.st.GetDeltaToken(ctx, e.opts.AccountID, folderID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("get delta token: %w", err)
	}

	url := deltaURL(tok, folderID)
	var added, updated, deleted int

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := e.gc.GetDelta(ctx, url, graph.DeltaOpts{
			Select:      graph.EnvelopeSelectFields,
			MaxPageSize: 100,
		})
		if err != nil {
			if graph.IsSyncStateNotFound(err) {
				e.logger.Info("sync: delta token aged out, re-initialising",
					slog.String("folder_id", folderID))
				if err := e.st.ClearDeltaToken(ctx, e.opts.AccountID, folderID); err != nil {
					return err
				}
				return e.syncFolderFresh(ctx, folderID)
			}
			return err
		}

		for _, item := range resp.Value {
			if item.Removed != nil {
				if err := e.st.DeleteMessage(ctx, item.ID); err != nil {
					return err
				}
				deleted++
				continue
			}
			isNew, err := e.applyMessage(ctx, folderID, item)
			if err != nil {
				return err
			}
			if isNew {
				added++
			} else {
				updated++
			}
		}

		if resp.NextLink != "" {
			url = resp.NextLink
			continue
		}
		if resp.DeltaLink != "" {
			if err := e.st.PutDeltaToken(ctx, store.DeltaToken{
				AccountID:   e.opts.AccountID,
				FolderID:    folderID,
				DeltaLink:   resp.DeltaLink,
				LastDeltaAt: time.Now(),
			}); err != nil {
				return err
			}
			break
		}
		return errors.New("graph delta: response missing both nextLink and deltaLink")
	}

	e.emit(FolderSyncedEvent{
		FolderID: folderID,
		Added:    added,
		Updated:  updated,
		Deleted:  deleted,
		At:       time.Now(),
	})
	return nil
}

// syncFolderFresh re-initialises the delta cursor by hitting the bare
// /messages/delta endpoint. The next deltaLink we get becomes the
// resume cursor.
func (e *engine) syncFolderFresh(ctx context.Context, folderID string) error {
	url := "/me/mailFolders/" + folderID + "/messages/delta"
	for {
		resp, err := e.gc.GetDelta(ctx, url, graph.DeltaOpts{
			Select:      graph.EnvelopeSelectFields,
			MaxPageSize: 100,
		})
		if err != nil {
			return err
		}
		for _, item := range resp.Value {
			if item.Removed != nil {
				_ = e.st.DeleteMessage(ctx, item.ID)
				continue
			}
			if _, err := e.applyMessage(ctx, folderID, item); err != nil {
				return err
			}
		}
		if resp.NextLink != "" {
			url = resp.NextLink
			continue
		}
		if resp.DeltaLink != "" {
			return e.st.PutDeltaToken(ctx, store.DeltaToken{
				AccountID:   e.opts.AccountID,
				FolderID:    folderID,
				DeltaLink:   resp.DeltaLink,
				LastDeltaAt: time.Now(),
			})
		}
		return errors.New("graph delta init: missing both nextLink and deltaLink")
	}
}

// applyMessage upserts a delta-returned message into the store. Returns
// true when the row did not exist locally.
func (e *engine) applyMessage(ctx context.Context, folderID string, m graph.Message) (bool, error) {
	existing, _ := e.st.GetMessage(ctx, m.ID)
	isNew := existing == nil
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
		sm.FromAddress = strings.ToLower(m.From.EmailAddress.Address)
		sm.FromName = m.From.EmailAddress.Name
	}
	sm.ToAddresses = recipientsToStore(m.ToRecipients)
	sm.CcAddresses = recipientsToStore(m.CcRecipients)
	sm.BccAddresses = recipientsToStore(m.BccRecipients)
	if m.Flag != nil {
		sm.FlagStatus = m.Flag.FlagStatus
	}
	if existing != nil {
		sm.CachedAt = existing.CachedAt
	}
	return isNew, e.st.UpsertMessage(ctx, sm)
}

func recipientsToStore(rs []graph.Recipient) []store.EmailAddress {
	out := make([]store.EmailAddress, len(rs))
	for i, r := range rs {
		out[i] = store.EmailAddress{Name: r.EmailAddress.Name, Address: strings.ToLower(r.EmailAddress.Address)}
	}
	return out
}

// deltaURL returns the URL the next delta call should hit. When the
// cursor is empty, the caller falls back to the bare /messages/delta
// endpoint.
func deltaURL(t *store.DeltaToken, folderID string) string {
	if t == nil || t.DeltaLink == "" {
		return "/me/mailFolders/" + folderID + "/messages/delta"
	}
	return t.DeltaLink
}
