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

// syncFolder runs the per-folder delta loop (spec §6).
//
// Cursor selection:
//  1. non-empty next_link → mid-pagination resume; follow ONE page only
//  2. else non-empty delta_link → standard incremental delta
//  3. else first-launch → quick-start with $top=50, follow ONE page only
//
// "Drain one page only" means we yield after a single page so other
// folders can advance on the same tick. Subsequent ticks resume by
// reading next_link again.
func (e *engine) syncFolder(ctx context.Context, folderID string) error {
	tok, err := e.st.GetDeltaToken(ctx, e.opts.AccountID, folderID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("get delta token: %w", err)
	}

	url, drainOnePageOnly := pickURL(tok, folderID)
	var added, updated, deleted int

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := e.gc.GetDelta(ctx, url, graph.DeltaOpts{
			Select:      graph.EnvelopeSelectFields,
			MaxPageSize: pageSizeForCursor(drainOnePageOnly),
		})
		if err != nil {
			if graph.IsSyncStateNotFound(err) {
				e.logger.Info("sync: delta token aged out, re-initialising",
					slog.String("folder_id", folderID))
				if err := e.st.ClearDeltaToken(ctx, e.opts.AccountID, folderID); err != nil {
					return err
				}
				return e.syncFolder(ctx, folderID) // retry once with fresh cursor selection
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
				// Quick-start or mid-pagination resume: persist the
				// nextLink and yield. The next sync tick continues
				// the drain.
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
			// Pagination drained. Persist deltaLink and clear any
			// lingering next_link.
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

// pickURL implements the cursor-selection rules of spec §5.3.
// Returns the URL to call and whether the caller should yield after a
// single page (true for quick-start and mid-pagination resume).
func pickURL(tok *store.DeltaToken, folderID string) (url string, drainOnePageOnly bool) {
	switch {
	case tok != nil && tok.NextLink != "":
		return tok.NextLink, true
	case tok != nil && tok.DeltaLink != "":
		return tok.DeltaLink, false
	default:
		return "/me/mailFolders/" + folderID + "/messages/delta?$top=" + intToA(QuickStartPageSize), true
	}
}

// pageSizeForCursor decides the Prefer odata.maxpagesize hint. On
// quick-start / mid-pagination resume we ask for QuickStartPageSize so
// the user's first paint is bounded; on incremental delta we use the
// standard 100.
func pageSizeForCursor(drainOnePageOnly bool) int {
	if drainOnePageOnly {
		return QuickStartPageSize
	}
	return 100
}

// intToA is a stdlib-only int→ascii helper to avoid importing strconv
// just for one call site.
func intToA(n int) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
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

