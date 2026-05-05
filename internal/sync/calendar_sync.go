package sync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// calendarDeltaKey is the store key used for the calendar delta token.
// Stored in the same per-account delta_tokens table as folder tokens.
const calendarDeltaKey = "__calendar__"

// syncCalendar performs one calendar sync pass for the engine's
// account. It fetches the window [now - lookbackDays, now + lookaheadDays]
// using /me/calendarView/delta so successive calls are incremental.
// On first call (no stored token) a full-window fetch populates the cache;
// subsequent calls only transfer changes.
//
// Removed events (Graph @removed marker) are deleted from the local cache.
// New / updated events are upserted. Events outside the new window are
// pruned by DeleteEventsBefore(now - lookbackDays).
func (e *engine) syncCalendar(ctx context.Context) error {
	now := time.Now()
	start := truncateToDay(now.UTC()).Add(-time.Duration(e.opts.CalendarLookbackDays) * 24 * time.Hour)
	end := truncateToDay(now.UTC()).Add(time.Duration(e.opts.CalendarLookaheadDays+1) * 24 * time.Hour)

	tok, _ := e.st.GetDeltaToken(ctx, e.opts.AccountID, calendarDeltaKey)
	var deltaLink string
	if tok != nil {
		deltaLink = tok.DeltaLink
	}

	result, err := e.gc.ListCalendarDelta(ctx, start, end, deltaLink)
	if err != nil {
		// 410 SyncStateNotFound: token aged out; reset and full re-fetch.
		if graph.IsSyncStateNotFound(err) {
			e.logger.Info("calendar sync: delta token expired; full re-fetch")
			_ = e.st.ClearDeltaToken(ctx, e.opts.AccountID, calendarDeltaKey)
			result, err = e.gc.ListCalendarDelta(ctx, start, end, "")
			if err != nil {
				return fmt.Errorf("calendar sync: full re-fetch: %w", err)
			}
		} else {
			return fmt.Errorf("calendar sync: delta: %w", err)
		}
	}

	// Upsert new / updated events.
	if len(result.Events) > 0 {
		storeEvents := make([]store.Event, len(result.Events))
		for i, ev := range result.Events {
			storeEvents[i] = store.Event{
				ID:               ev.ID,
				AccountID:        e.opts.AccountID,
				Subject:          ev.Subject,
				OrganizerName:    ev.OrganizerName,
				OrganizerAddress: ev.OrganizerAddress,
				Start:            ev.Start,
				End:              ev.End,
				IsAllDay:         ev.IsAllDay,
				Location:         ev.Location,
				OnlineMeetingURL: ev.OnlineMeetingURL,
				ShowAs:           ev.ShowAs,
				ResponseStatus:   ev.ResponseStatus,
				WebLink:          ev.WebLink,
				CachedAt:         time.Now(),
			}
		}
		if err := e.st.PutEvents(ctx, storeEvents); err != nil {
			return fmt.Errorf("calendar sync: upsert events: %w", err)
		}
	}

	// Delete removed events.
	for _, id := range result.Removed {
		if err := e.st.DeleteEvent(ctx, id); err != nil {
			e.logger.Warn("calendar sync: delete removed event", slog.String("id", id), slog.String("err", err.Error()))
		}
	}

	// Prune out-of-window events.
	if err := e.st.DeleteEventsBefore(ctx, e.opts.AccountID, start); err != nil {
		e.logger.Warn("calendar sync: prune old events", slog.String("err", err.Error()))
	}

	// Persist the delta link for the next pass.
	if result.DeltaLink != "" {
		if err := e.st.PutDeltaToken(ctx, store.DeltaToken{
			AccountID:   e.opts.AccountID,
			FolderID:    calendarDeltaKey,
			DeltaLink:   result.DeltaLink,
			LastDeltaAt: time.Now(),
		}); err != nil {
			e.logger.Warn("calendar sync: persist delta token", slog.String("err", err.Error()))
		}
	}

	e.logger.Info("calendar sync: complete",
		slog.Int("upserted", len(result.Events)),
		slog.Int("removed", len(result.Removed)),
	)
	return nil
}

// truncateToDay returns the start of the day in UTC.
func truncateToDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// midnightWatcher runs once per day just after local midnight and
// resets the calendar delta token so the next sync cycle does a full
// re-fetch of the new window. This ensures the window always covers
// [now-lookback, now+lookahead] rather than drifting as days pass.
//
// The goroutine exits when ctx is cancelled or e.stopped closes.
func (e *engine) midnightWatcher(ctx context.Context) {
	for {
		now := time.Now()
		// Next midnight in local time.
		y, m, d := now.Date()
		nextMidnight := time.Date(y, m, d+1, 0, 0, 1, 0, now.Location())
		timer := time.NewTimer(time.Until(nextMidnight))

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-e.stopped:
			timer.Stop()
			return
		case <-timer.C:
		}

		e.logger.Info("calendar sync: midnight window slide")
		_ = e.st.ClearDeltaToken(ctx, e.opts.AccountID, calendarDeltaKey)
		e.kick()
	}
}
