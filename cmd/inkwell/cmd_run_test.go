package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/ui"
)

// TestFilterDeclinedRemovesDeclined verifies that filterDeclined strips events
// with response_status == "declined" when showDeclined is false (spec 12 §6.1).
func TestFilterDeclinedRemovesDeclined(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	events := []ui.CalendarEvent{
		{Subject: "Accepted", ResponseStatus: "accepted", Start: now},
		{Subject: "Declined", ResponseStatus: "declined", Start: now},
		{Subject: "Tentative", ResponseStatus: "tentativelyAccepted", Start: now},
		{Subject: "NotResponded", ResponseStatus: "notResponded", Start: now},
	}
	got := filterDeclined(events, false)
	require.Len(t, got, 3)
	for _, e := range got {
		require.NotEqual(t, "declined", e.ResponseStatus, "declined events must be removed")
	}
}

// TestFilterDeclinedShowDeclinedKeepsAll verifies that filterDeclined is a
// no-op when showDeclined is true.
func TestFilterDeclinedShowDeclinedKeepsAll(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	events := []ui.CalendarEvent{
		{Subject: "Accepted", ResponseStatus: "accepted", Start: now},
		{Subject: "Declined", ResponseStatus: "declined", Start: now},
	}
	got := filterDeclined(events, true)
	require.Len(t, got, 2)
}

// TestFilterDeclinedEmptySliceIsNoop verifies nil / empty input does not panic.
func TestFilterDeclinedEmptySliceIsNoop(t *testing.T) {
	require.Empty(t, filterDeclined(nil, false))
	require.Empty(t, filterDeclined([]ui.CalendarEvent{}, false))
}
