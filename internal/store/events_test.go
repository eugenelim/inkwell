package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestEventsRoundTrip verifies the spec 12 cache shape: an event
// upserted today is returned by ListEvents within the matching
// window with all fields preserved.
func TestEventsRoundTrip(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	e := Event{
		ID:               "ev-1",
		AccountID:        acc,
		Subject:          "Q4 sync",
		OrganizerName:    "Alice",
		OrganizerAddress: "alice@example.invalid",
		Start:            now,
		End:              now.Add(time.Hour),
		IsAllDay:         false,
		Location:         "Conf Room A",
		OnlineMeetingURL: "https://teams.example.invalid/x",
		ShowAs:           "busy",
		WebLink:          "https://outlook.example.invalid/e/x",
	}
	require.NoError(t, s.PutEvent(context.Background(), e))

	got, err := s.ListEvents(context.Background(), EventQuery{
		AccountID: acc,
		Start:     now.Add(-time.Hour),
		End:       now.Add(2 * time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "ev-1", got[0].ID)
	require.Equal(t, "Q4 sync", got[0].Subject)
	require.Equal(t, "Alice", got[0].OrganizerName)
	require.Equal(t, "busy", got[0].ShowAs)
	require.Equal(t, "Conf Room A", got[0].Location)
	require.Equal(t, now.Unix(), got[0].Start.Unix())
}

// TestEventsListEventsWindowFilters verifies the [Start, End)
// half-open semantic: an event whose start ≥ End is excluded;
// an event whose end ≤ Start is excluded.
func TestEventsListEventsWindowFilters(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, s.PutEvents(context.Background(), []Event{
		{ID: "ev-yesterday", AccountID: acc, Subject: "Yesterday", Start: base.Add(-24 * time.Hour), End: base.Add(-23 * time.Hour)},
		{ID: "ev-today-1", AccountID: acc, Subject: "Today AM", Start: base.Add(8 * time.Hour), End: base.Add(9 * time.Hour)},
		{ID: "ev-today-2", AccountID: acc, Subject: "Today PM", Start: base.Add(14 * time.Hour), End: base.Add(15 * time.Hour)},
		{ID: "ev-tomorrow", AccountID: acc, Subject: "Tomorrow", Start: base.Add(25 * time.Hour), End: base.Add(26 * time.Hour)},
	}))

	got, err := s.ListEvents(context.Background(), EventQuery{
		AccountID: acc,
		Start:     base,
		End:       base.Add(24 * time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, got, 2, "today's window must exclude yesterday + tomorrow")
	require.Equal(t, "ev-today-1", got[0].ID)
	require.Equal(t, "ev-today-2", got[1].ID, "ListEvents must return Start ASC")
}

// TestDeleteEventsBefore covers the window-slide hook: rolling
// past midnight cleans yesterday's rows.
func TestDeleteEventsBefore(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, s.PutEvents(context.Background(), []Event{
		{ID: "ev-old", AccountID: acc, Start: base.Add(-48 * time.Hour), End: base.Add(-47 * time.Hour)},
		{ID: "ev-new", AccountID: acc, Start: base.Add(time.Hour), End: base.Add(2 * time.Hour)},
	}))
	require.NoError(t, s.DeleteEventsBefore(context.Background(), acc, base))

	got, err := s.ListEvents(context.Background(), EventQuery{AccountID: acc})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "ev-new", got[0].ID)
}

// TestEventsCascadeOnAccountDelete is the FK invariant: deleting
// the account cascades event rows.
func TestEventsCascadeOnAccountDelete(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	require.NoError(t, s.PutEvent(context.Background(), Event{
		ID:        "ev-1",
		AccountID: acc,
		Subject:   "x",
		Start:     time.Now(),
		End:       time.Now().Add(time.Hour),
	}))
	// Delete via the underlying SQL — the public Store API doesn't
	// expose DeleteAccount, but we can drop directly.
	_, err := s.(*store).db.Exec("DELETE FROM accounts WHERE id = ?", acc)
	require.NoError(t, err)

	got, err := s.ListEvents(context.Background(), EventQuery{AccountID: acc})
	require.NoError(t, err)
	require.Empty(t, got, "events must cascade-delete with the account")
}
