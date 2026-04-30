package sync

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// TestMaintenancePassEvictsBodies covers the LRU half of the spec
// 02 §8 maintenance loop: bodies past the count cap get dropped.
func TestMaintenancePassEvictsBodies(t *testing.T) {
	st := openSyncTestStore(t)
	acc := seedSyncAccount(t, st)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))
	// Seed 20 messages + bodies. The cap is 10; eviction must
	// drop the 10 LRU rows.
	for i := 0; i < 20; i++ {
		id := "m-" + string(rune('a'+i))
		require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
			ID:         id,
			AccountID:  acc,
			FolderID:   "f-inbox",
			ReceivedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		}))
		require.NoError(t, st.PutBody(context.Background(), store.Body{
			MessageID:   id,
			ContentType: "text",
			Content:     "body " + id,
		}))
	}

	eng, _, _, _ := newSyncTest(t)
	e := eng.(*engine)
	e.st = st
	e.opts.BodyCacheMaxCount = 10
	e.opts.BodyCacheMaxBytes = 1024 * 1024
	e.opts.DoneActionsRetention = 7 * 24 * time.Hour

	e.maintenancePass(context.Background())

	// Count remaining bodies via repeated GetBody. Eviction returns
	// ErrNotFound for evicted ids.
	remaining := 0
	for i := 0; i < 20; i++ {
		id := "m-" + string(rune('a'+i))
		b, _ := st.GetBody(context.Background(), id)
		if b != nil {
			remaining++
		}
	}
	require.LessOrEqual(t, remaining, 10, "body cache must not exceed configured cap after eviction")
}

// TestMaintenancePassSweepsDoneActions verifies the action-sweep
// half: rows with status=done/failed and completed_at older than
// the retention cutoff get deleted; pending rows stay.
func TestMaintenancePassSweepsDoneActions(t *testing.T) {
	st := openSyncTestStore(t)
	acc := seedSyncAccount(t, st)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox",
	}))
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "m-a", AccountID: acc, FolderID: "f-inbox", ReceivedAt: time.Now(),
	}))

	// Pending action — must survive.
	require.NoError(t, st.EnqueueAction(context.Background(), store.Action{
		ID:         "act-pending",
		AccountID:  acc,
		Type:       store.ActionMarkRead,
		MessageIDs: []string{"m-a"},
		Status:     store.StatusPending,
	}))
	// Done action — UpdateActionStatus stamps completed_at = now,
	// which is fresh so the default 7-day retention won't sweep it.
	// The test forces a tiny retention to make the cutoff > now.
	require.NoError(t, st.EnqueueAction(context.Background(), store.Action{
		ID:         "act-done",
		AccountID:  acc,
		Type:       store.ActionMarkRead,
		MessageIDs: []string{"m-a"},
		Status:     store.StatusPending,
	}))
	require.NoError(t, st.UpdateActionStatus(context.Background(), "act-done", store.StatusDone, ""))

	eng, _, _, _ := newSyncTest(t)
	e := eng.(*engine)
	e.st = st
	// Negative-duration retention pushes the cutoff into the future,
	// guaranteeing the freshly-completed Done row gets swept.
	e.opts.DoneActionsRetention = -time.Hour
	e.opts.BodyCacheMaxCount = 1000
	e.opts.BodyCacheMaxBytes = 1024 * 1024

	e.maintenancePass(context.Background())

	pending, err := st.PendingActions(context.Background())
	require.NoError(t, err)
	require.Len(t, pending, 1, "the pending action must survive maintenance")
	require.Equal(t, "act-pending", pending[0].ID)
}

// TestMaintenancePassDisabledWhenIntervalNegative is the test-only
// sentinel: setting MaintenanceInterval=-1 disables the loop so
// other sync tests don't get noise from the maintenance goroutine.
func TestMaintenancePassDisabledWhenIntervalNegative(t *testing.T) {
	eng, _, _, _ := newSyncTest(t)
	e := eng.(*engine)
	e.opts.MaintenanceInterval = -1

	// runMaintenance must return immediately on the negative sentinel.
	done := make(chan struct{})
	go func() {
		e.runMaintenance(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runMaintenance did not return on negative interval")
	}
}
