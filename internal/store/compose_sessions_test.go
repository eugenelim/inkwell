package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestComposeSessionRoundTrip verifies the upsert + list shape:
// inserting a session and listing it back returns the same fields,
// and `confirmed_at IS NULL` lands the row in the resume scan
// output.
func TestComposeSessionRoundTrip(t *testing.T) {
	s := OpenTestStore(t)

	sess := ComposeSession{
		SessionID: "sess-1",
		Kind:      "reply",
		SourceID:  "",
		Snapshot:  `{"kind":1,"source_id":"","to":"a@x","cc":"","subject":"Re: hi","body":"hello"}`,
	}
	require.NoError(t, s.PutComposeSession(context.Background(), sess))

	rows, err := s.ListUnconfirmedComposeSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "sess-1", rows[0].SessionID)
	require.Equal(t, "reply", rows[0].Kind)
	require.Equal(t, sess.Snapshot, rows[0].Snapshot)
	require.True(t, rows[0].ConfirmedAt.IsZero(), "in-flight session has zero confirmed_at")
	require.False(t, rows[0].CreatedAt.IsZero())
	require.False(t, rows[0].UpdatedAt.IsZero())
}

// TestComposeSessionUpsertRewritesSnapshot covers the focus-change
// path: each Tab in the in-modal compose pane re-PUTs the session
// to capture the field the user just left. The second PUT must
// overwrite the snapshot (not insert a duplicate row).
func TestComposeSessionUpsertRewritesSnapshot(t *testing.T) {
	s := OpenTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.PutComposeSession(ctx, ComposeSession{
		SessionID: "sess-1", Kind: "reply", Snapshot: `{"body":"v1"}`,
	}))
	require.NoError(t, s.PutComposeSession(ctx, ComposeSession{
		SessionID: "sess-1", Kind: "reply", Snapshot: `{"body":"v2"}`,
	}))

	rows, err := s.ListUnconfirmedComposeSessions(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "upsert keeps the row count at 1")
	require.Equal(t, `{"body":"v2"}`, rows[0].Snapshot, "snapshot reflects the latest write")
}

// TestComposeSessionConfirmHidesFromUnconfirmedScan confirms that
// once the user saves or discards (calling ConfirmComposeSession),
// the row no longer surfaces in the launch-time resume scan.
func TestComposeSessionConfirmHidesFromUnconfirmedScan(t *testing.T) {
	s := OpenTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.PutComposeSession(ctx, ComposeSession{
		SessionID: "sess-1", Kind: "reply", Snapshot: `{}`,
	}))
	require.NoError(t, s.ConfirmComposeSession(ctx, "sess-1"))

	rows, err := s.ListUnconfirmedComposeSessions(ctx)
	require.NoError(t, err)
	require.Empty(t, rows, "confirmed sessions don't surface in the resume scan")
}

// TestComposeSessionListUnconfirmedNewestFirst confirms the resume
// scan returns the most-recently-created session first — when the
// user has multiple stale rows from past crashes (rare but
// possible), they get the latest one offered to restore.
func TestComposeSessionListUnconfirmedNewestFirst(t *testing.T) {
	s := OpenTestStore(t)
	ctx := context.Background()

	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-30 * time.Minute)
	require.NoError(t, s.PutComposeSession(ctx, ComposeSession{
		SessionID: "sess-old", Kind: "reply", Snapshot: `{}`,
		CreatedAt: older, UpdatedAt: older,
	}))
	require.NoError(t, s.PutComposeSession(ctx, ComposeSession{
		SessionID: "sess-new", Kind: "reply", Snapshot: `{}`,
		CreatedAt: newer, UpdatedAt: newer,
	}))

	rows, err := s.ListUnconfirmedComposeSessions(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "sess-new", rows[0].SessionID, "newest first")
	require.Equal(t, "sess-old", rows[1].SessionID)
}

// TestComposeSessionGCRemovesOldConfirmed covers the launch-time
// garbage collection: confirmed sessions older than the cutoff
// disappear; unconfirmed sessions stay regardless of age (so a
// long-pending crashed draft from yesterday still gets resumed
// today).
func TestComposeSessionGCRemovesOldConfirmed(t *testing.T) {
	s := OpenTestStore(t)
	ctx := context.Background()

	long := time.Now().Add(-48 * time.Hour)
	require.NoError(t, s.PutComposeSession(ctx, ComposeSession{
		SessionID: "sess-old-confirmed", Kind: "reply", Snapshot: `{}`,
		CreatedAt: long, UpdatedAt: long, ConfirmedAt: long,
	}))
	require.NoError(t, s.PutComposeSession(ctx, ComposeSession{
		SessionID: "sess-recent-confirmed", Kind: "reply", Snapshot: `{}`,
		ConfirmedAt: time.Now(),
	}))
	require.NoError(t, s.PutComposeSession(ctx, ComposeSession{
		SessionID: "sess-old-unconfirmed", Kind: "reply", Snapshot: `{}`,
		CreatedAt: long, UpdatedAt: long,
	}))

	cutoff := time.Now().Add(-24 * time.Hour)
	deleted, err := s.GCConfirmedComposeSessions(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted, "only the old-confirmed row gets pruned")

	rows, err := s.ListUnconfirmedComposeSessions(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "unconfirmed survives GC regardless of age")
	require.Equal(t, "sess-old-unconfirmed", rows[0].SessionID)
}

// TestComposeSessionForeignKeySetNullOnSourceDelete covers the
// FK ON DELETE SET NULL contract: when the source message is
// deleted between crash and resume (e.g., another device deleted
// it), the session row stays but its source_id is cleared so the
// resume modal can warn the user without crashing on a missing
// source.
func TestComposeSessionForeignKeySetNullOnSourceDelete(t *testing.T) {
	s := OpenTestStore(t)
	ctx := context.Background()
	acc := SeedAccount(t, s)
	require.NoError(t, s.UpsertFolder(ctx, Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ID: "m-source", AccountID: acc, FolderID: "f-inbox", Subject: "x", ReceivedAt: time.Now(),
	}))

	require.NoError(t, s.PutComposeSession(ctx, ComposeSession{
		SessionID: "sess-1", Kind: "reply", SourceID: "m-source", Snapshot: `{}`,
	}))
	require.NoError(t, s.DeleteMessage(ctx, "m-source"))

	rows, err := s.ListUnconfirmedComposeSessions(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "session row survives source deletion")
	require.Empty(t, rows[0].SourceID,
		"source_id is cleared so the resume modal can warn / fall back gracefully")
}
