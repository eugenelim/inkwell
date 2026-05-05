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

// TestDeleteEvent verifies that DeleteEvent removes the row and a
// subsequent ListEvents returns nothing for that ID.
func TestDeleteEvent(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, s.PutEvent(context.Background(), Event{
		ID: "ev-del", AccountID: acc, Start: now, End: now.Add(time.Hour),
	}))

	got, err := s.ListEvents(context.Background(), EventQuery{AccountID: acc})
	require.NoError(t, err)
	require.Len(t, got, 1)

	require.NoError(t, s.DeleteEvent(context.Background(), "ev-del"))

	got2, err := s.ListEvents(context.Background(), EventQuery{AccountID: acc})
	require.NoError(t, err)
	require.Empty(t, got2, "DeleteEvent must remove the event from the cache")
}

// TestDeleteEventIsIdempotent verifies deleting a non-existent event
// does not return an error.
func TestDeleteEventIsIdempotent(t *testing.T) {
	s := OpenTestStore(t)
	require.NoError(t, s.DeleteEvent(context.Background(), "no-such-id"))
}

// TestPutEventAttendeesRoundTrip verifies that attendees stored by
// PutEventAttendees come back via ListEventAttendees in address order.
func TestPutEventAttendeesRoundTrip(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, s.PutEvent(context.Background(), Event{
		ID: "ev-att", AccountID: acc, Start: now, End: now.Add(time.Hour),
	}))

	attendees := []EventAttendee{
		{EventID: "ev-att", Address: "bob@example.invalid", Name: "Bob", Type: "required", Status: "accepted"},
		{EventID: "ev-att", Address: "alice@example.invalid", Name: "Alice", Type: "optional", Status: "tentativelyAccepted"},
	}
	require.NoError(t, s.PutEventAttendees(context.Background(), "ev-att", attendees))

	got, err := s.ListEventAttendees(context.Background(), "ev-att")
	require.NoError(t, err)
	require.Len(t, got, 2)
	// Ordered by address: alice < bob.
	require.Equal(t, "alice@example.invalid", got[0].Address)
	require.Equal(t, "Alice", got[0].Name)
	require.Equal(t, "optional", got[0].Type)
	require.Equal(t, "tentativelyAccepted", got[0].Status)
	require.Equal(t, "bob@example.invalid", got[1].Address)
	require.Equal(t, "accepted", got[1].Status)
}

// TestPutEventAttendeesReplacesExisting confirms the atomic replace
// semantics: a second call replaces the entire set, not appends.
func TestPutEventAttendeesReplacesExisting(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, s.PutEvent(context.Background(), Event{
		ID: "ev-rep", AccountID: acc, Start: now, End: now.Add(time.Hour),
	}))

	first := []EventAttendee{
		{Address: "a@example.invalid", Name: "A"},
		{Address: "b@example.invalid", Name: "B"},
	}
	require.NoError(t, s.PutEventAttendees(context.Background(), "ev-rep", first))

	second := []EventAttendee{{Address: "c@example.invalid", Name: "C"}}
	require.NoError(t, s.PutEventAttendees(context.Background(), "ev-rep", second))

	got, err := s.ListEventAttendees(context.Background(), "ev-rep")
	require.NoError(t, err)
	require.Len(t, got, 1, "second PutEventAttendees must replace, not append")
	require.Equal(t, "c@example.invalid", got[0].Address)
}

// TestListEventAttendeesEmptyReturnsNil verifies that ListEventAttendees
// returns nil (not an error and not an empty slice) when no rows exist.
func TestListEventAttendeesEmptyReturnsNil(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, s.PutEvent(context.Background(), Event{
		ID: "ev-noatt", AccountID: acc, Start: now, End: now.Add(time.Hour),
	}))

	got, err := s.ListEventAttendees(context.Background(), "ev-noatt")
	require.NoError(t, err)
	require.Nil(t, got, "ListEventAttendees must return nil when no attendees are cached")
}

// TestEventAttendeeCascadeOnEventDelete confirms that deleting an event
// also removes its attendee rows (via ON DELETE CASCADE).
func TestEventAttendeeCascadeOnEventDelete(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, s.PutEvent(context.Background(), Event{
		ID: "ev-cas", AccountID: acc, Start: now, End: now.Add(time.Hour),
	}))
	require.NoError(t, s.PutEventAttendees(context.Background(), "ev-cas", []EventAttendee{
		{Address: "a@example.invalid"},
	}))

	require.NoError(t, s.DeleteEvent(context.Background(), "ev-cas"))

	got, err := s.ListEventAttendees(context.Background(), "ev-cas")
	require.NoError(t, err)
	require.Nil(t, got, "attendee rows must cascade-delete with their event")
}

// TestEventResponseStatusRoundTrip verifies that response_status survives a
// PutEvents → ListEvents round-trip (migration 008 / spec 12 §3).
func TestEventResponseStatusRoundTrip(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, s.PutEvents(context.Background(), []Event{
		{ID: "ev-rs", AccountID: acc, Start: now, End: now.Add(time.Hour), ResponseStatus: "accepted"},
	}))

	got, err := s.ListEvents(context.Background(), EventQuery{AccountID: acc})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "accepted", got[0].ResponseStatus,
		"response_status must survive PutEvents → ListEvents")
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
