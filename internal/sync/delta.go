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

// QuickStartPageSize is the per-folder envelope budget for the very
// first page of a folder's lazy backfill (spec §5.2).
const QuickStartPageSize = 50

// syncFolder syncs one folder, picking its strategy from the folder's
// delta_tokens row (spec §5.3, revised in iter 6):
//
//  1. NextLink set → mid-pagination resume of an in-flight delta. Follow
//     ONE page, persist the new cursor, yield.
//  2. DeltaLink set → standard incremental delta call. Drain to a new
//     deltaLink (typically zero-page on a quiet folder).
//  3. LastDeltaAt set but neither link → incremental /messages call
//     with `$filter=receivedDateTime gt {last_seen}` to pull anything
//     new since the last successful sync.
//  4. Nothing set → quick-start: /messages?$top=50&$orderby=receivedDateTime desc
//     to populate the newest 50 envelopes by RECEIVED time. (Graph's
//     delta endpoint does NOT support $orderby in v1.0; using delta
//     with $top=N returns whatever Graph wants, which is generally
//     order-by-lastModifiedDateTime — that's what the user saw in
//     v0.2.3 as "not the most recent emails".)
//
// Trade-off vs proper delta sync: in modes (3) and (4) we don't
// receive @removed tombstones, so server-side deletions and moves
// won't propagate to the local cache. Acceptable for the v0.2.x read
// path. A future iter can add a background "drain delta to seed the
// cursor" pass.
func (e *engine) syncFolder(ctx context.Context, folderID string) error {
	tok, err := e.st.GetDeltaToken(ctx, e.opts.AccountID, folderID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("get delta token: %w", err)
	}

	switch {
	case tok != nil && tok.NextLink != "":
		return e.followDeltaPage(ctx, folderID, tok.NextLink, true)
	case tok != nil && tok.DeltaLink != "":
		return e.followDeltaPage(ctx, folderID, tok.DeltaLink, false)
	case tok != nil && !tok.LastDeltaAt.IsZero():
		return e.pullSince(ctx, folderID, tok.LastDeltaAt)
	default:
		return e.quickStart(ctx, folderID)
	}
}

// quickStart pulls the newest QuickStartPageSize messages by received
// date via the non-delta /messages endpoint. Persists the row with
// LastDeltaAt = now so the next tick takes the pullSince path.
func (e *engine) quickStart(ctx context.Context, folderID string) error {
	page, err := e.gc.ListMessagesInFolder(ctx, folderID, graph.ListMessagesOpts{
		Top:     QuickStartPageSize,
		OrderBy: "receivedDateTime desc",
	})
	if err != nil {
		return fmt.Errorf("quick-start /messages: %w", err)
	}
	added, updated, err := e.applyPage(ctx, folderID, page.Value)
	if err != nil {
		return err
	}
	if err := e.st.PutDeltaToken(ctx, store.DeltaToken{
		AccountID:   e.opts.AccountID,
		FolderID:    folderID,
		LastDeltaAt: time.Now(),
	}); err != nil {
		return err
	}
	e.emit(FolderSyncedEvent{
		FolderID: folderID,
		Added:    added,
		Updated:  updated,
		At:       time.Now(),
	})
	return nil
}

// pullSince fetches messages received after `since` via /messages
// `$filter=receivedDateTime gt {since}`. Stand-in for delta until a
// future iter adds proper cursor seeding.
func (e *engine) pullSince(ctx context.Context, folderID string, since time.Time) error {
	filter := fmt.Sprintf("receivedDateTime gt %s", since.UTC().Format(time.RFC3339))
	page, err := e.gc.ListMessagesInFolder(ctx, folderID, graph.ListMessagesOpts{
		Top:     QuickStartPageSize,
		OrderBy: "receivedDateTime desc",
		Filter:  filter,
	})
	if err != nil {
		return fmt.Errorf("pull-since /messages: %w", err)
	}
	added, updated, err := e.applyPage(ctx, folderID, page.Value)
	if err != nil {
		return err
	}
	if err := e.st.PutDeltaToken(ctx, store.DeltaToken{
		AccountID:   e.opts.AccountID,
		FolderID:    folderID,
		LastDeltaAt: time.Now(),
	}); err != nil {
		return err
	}
	e.emit(FolderSyncedEvent{
		FolderID: folderID,
		Added:    added,
		Updated:  updated,
		At:       time.Now(),
	})
	return nil
}

// followDeltaPage fetches one page from a previously-persisted delta
// or next link. Used for mid-pagination resume (drainOnePageOnly=true)
// and for standard incremental delta calls (drainOnePageOnly=false).
//
// This path remains for backwards compatibility with delta_token rows
// that already have a deltaLink stored (e.g., a future iter that
// seeds delta cursors). New folders no longer take this path on first
// launch — they go through quickStart.
func (e *engine) followDeltaPage(ctx context.Context, folderID, url string, drainOnePageOnly bool) error {
	var added, updated, deleted int
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := e.gc.GetDelta(ctx, url, graph.DeltaOpts{
			Select:      graph.EnvelopeSelectFields,
			MaxPageSize: e.opts.DeltaPageSize,
		})
		if err != nil {
			if graph.IsSyncStateNotFound(err) {
				e.logger.Info("sync: delta token aged out, re-initialising",
					slog.String("folder_id", folderID))
				if err := e.st.ClearDeltaToken(ctx, e.opts.AccountID, folderID); err != nil {
					return err
				}
				return e.syncFolder(ctx, folderID)
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
			if drainOnePageOnly {
				if err := e.st.PutDeltaToken(ctx, store.DeltaToken{
					AccountID:   e.opts.AccountID,
					FolderID:    folderID,
					NextLink:    resp.NextLink,
					LastDeltaAt: time.Now(),
				}); err != nil {
					return err
				}
				break
			}
			url = resp.NextLink
			continue
		}
		if resp.DeltaLink != "" {
			if err := e.st.PutDeltaToken(ctx, store.DeltaToken{
				AccountID:   e.opts.AccountID,
				FolderID:    folderID,
				DeltaLink:   resp.DeltaLink,
				NextLink:    "",
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

// applyPage upserts a list of Graph messages into the local store and
// returns (added, updated, error). Used by quickStart and pullSince
// where there are no @removed tombstones.
func (e *engine) applyPage(ctx context.Context, folderID string, msgs []graph.Message) (int, int, error) {
	added := 0
	updated := 0
	for _, m := range msgs {
		isNew, err := e.applyMessage(ctx, folderID, m)
		if err != nil {
			return added, updated, err
		}
		if isNew {
			added++
		} else {
			updated++
		}
	}
	return added, updated, nil
}

// applyMessage upserts a single Graph message into the local store.
// Returns true when the row did not exist locally.
func (e *engine) applyMessage(ctx context.Context, folderID string, m graph.Message) (bool, error) {
	existing, _ := e.st.GetMessage(ctx, m.ID)
	isNew := existing == nil
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
		sm.FromAddress = strings.ToLower(m.From.EmailAddress.Address)
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
