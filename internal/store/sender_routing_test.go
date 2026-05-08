package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"  Bob@Acme.IO  ", "bob@acme.io"},
		{"news@example.com", "news@example.com"},
		{"", ""},
	}
	for _, c := range cases {
		require.Equal(t, c.want, NormalizeEmail(c.in), "in=%q", c.in)
	}
}

func TestSetSenderRoutingUpsertsAndNormalizes(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	prior, err := s.SetSenderRouting(ctx, acc, "  News@Example.COM  ", RoutingFeed)
	require.NoError(t, err)
	require.Equal(t, "", prior)

	dest, err := s.GetSenderRouting(ctx, acc, "news@example.com")
	require.NoError(t, err)
	require.Equal(t, RoutingFeed, dest)
	dest2, err := s.GetSenderRouting(ctx, acc, "NEWS@example.com")
	require.NoError(t, err)
	require.Equal(t, RoutingFeed, dest2)
}

func TestSetSenderRoutingNoOpDoesNotBumpAddedAt(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	prior, err := s.SetSenderRouting(ctx, acc, "a@x.invalid", RoutingFeed)
	require.NoError(t, err)
	require.Equal(t, "", prior)

	rows, err := s.ListSenderRoutings(ctx, acc, RoutingFeed)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	first := rows[0].AddedAt

	time.Sleep(1100 * time.Millisecond) // unix-second resolution; ensure a tick gap

	prior, err = s.SetSenderRouting(ctx, acc, "a@x.invalid", RoutingFeed)
	require.NoError(t, err)
	require.Equal(t, RoutingFeed, prior, "no-op: prior == destination")

	rows, err = s.ListSenderRoutings(ctx, acc, RoutingFeed)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, first.Unix(), rows[0].AddedAt.Unix(), "added_at must not move on no-op")
}

func TestSetSenderRoutingReassignBumpsAddedAt(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	_, err := s.SetSenderRouting(ctx, acc, "a@x.invalid", RoutingImbox)
	require.NoError(t, err)
	rows, err := s.ListSenderRoutings(ctx, acc, "")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	first := rows[0].AddedAt

	time.Sleep(1100 * time.Millisecond)

	prior, err := s.SetSenderRouting(ctx, acc, "a@x.invalid", RoutingFeed)
	require.NoError(t, err)
	require.Equal(t, RoutingImbox, prior)
	rows, err = s.ListSenderRoutings(ctx, acc, "")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, RoutingFeed, rows[0].Destination)
	require.Greater(t, rows[0].AddedAt.Unix(), first.Unix(), "reassign must bump added_at")
}

func TestSetSenderRoutingRejectsInvalidDestination(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	_, err := s.SetSenderRouting(ctx, acc, "a@x.invalid", "primary")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidDestination))
}

func TestSetSenderRoutingRejectsEmptyAddress(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	_, err := s.SetSenderRouting(ctx, acc, "   ", RoutingImbox)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidAddress))
}

func TestClearSenderRoutingNoop(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	prior, err := s.ClearSenderRouting(ctx, acc, "missing@x.invalid")
	require.NoError(t, err)
	require.Equal(t, "", prior)
}

func TestClearSenderRoutingReturnsPrior(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	_, err := s.SetSenderRouting(ctx, acc, "a@x.invalid", RoutingFeed)
	require.NoError(t, err)
	prior, err := s.ClearSenderRouting(ctx, acc, "a@x.invalid")
	require.NoError(t, err)
	require.Equal(t, RoutingFeed, prior)
	dest, err := s.GetSenderRouting(ctx, acc, "a@x.invalid")
	require.NoError(t, err)
	require.Equal(t, "", dest)
}

func TestListMessagesByRoutingNormalizesCaseAndWhitespace(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()

	// Two messages from "the same" sender stored under different
	// casings/whitespace; the JOIN's lower(trim(...)) collapses both.
	msg1 := SyntheticMessage(acc, folder.ID, 0, time.Now())
	msg1.FromAddress = "Bob@Acme.IO"
	msg2 := SyntheticMessage(acc, folder.ID, 1, time.Now())
	msg2.FromAddress = "  bob@acme.io  "
	require.NoError(t, s.UpsertMessagesBatch(ctx, []Message{msg1, msg2}))

	_, err := s.SetSenderRouting(ctx, acc, "Bob@Acme.IO", RoutingFeed)
	require.NoError(t, err)

	got, err := s.ListMessagesByRouting(ctx, acc, RoutingFeed, 100, false)
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestListMessagesByRoutingExcludesMuted(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()

	msg1 := SyntheticMessage(acc, folder.ID, 0, time.Now())
	msg1.FromAddress = "news@x.invalid"
	msg1.ConversationID = "conv-muted"
	msg2 := SyntheticMessage(acc, folder.ID, 1, time.Now())
	msg2.FromAddress = "news@x.invalid"
	msg2.ConversationID = "conv-visible"
	require.NoError(t, s.UpsertMessagesBatch(ctx, []Message{msg1, msg2}))

	_, err := s.SetSenderRouting(ctx, acc, "news@x.invalid", RoutingFeed)
	require.NoError(t, err)
	require.NoError(t, s.MuteConversation(ctx, acc, "conv-muted"))

	got, err := s.ListMessagesByRouting(ctx, acc, RoutingFeed, 100, true)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, msg2.ID, got[0].ID)

	all, err := s.ListMessagesByRouting(ctx, acc, RoutingFeed, 100, false)
	require.NoError(t, err)
	require.Len(t, all, 2)
}

func TestCountMessagesByRouting(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()

	for i, addr := range []string{"a@x.invalid", "a@x.invalid", "b@x.invalid"} {
		msg := SyntheticMessage(acc, folder.ID, i, time.Now())
		msg.FromAddress = addr
		require.NoError(t, s.UpsertMessage(ctx, msg))
	}
	_, err := s.SetSenderRouting(ctx, acc, "a@x.invalid", RoutingFeed)
	require.NoError(t, err)
	_, err = s.SetSenderRouting(ctx, acc, "b@x.invalid", RoutingImbox)
	require.NoError(t, err)

	n, err := s.CountMessagesByRouting(ctx, acc, RoutingFeed, true)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	n, err = s.CountMessagesByRouting(ctx, acc, RoutingImbox, true)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	n, err = s.CountMessagesByRouting(ctx, acc, RoutingPaperTrail, true)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

func TestCountMessagesByRoutingAllReturnsAllFour(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()

	for i, addr := range []string{"a@x.invalid", "b@x.invalid"} {
		msg := SyntheticMessage(acc, folder.ID, i, time.Now())
		msg.FromAddress = addr
		require.NoError(t, s.UpsertMessage(ctx, msg))
	}
	_, err := s.SetSenderRouting(ctx, acc, "a@x.invalid", RoutingFeed)
	require.NoError(t, err)
	_, err = s.SetSenderRouting(ctx, acc, "b@x.invalid", RoutingImbox)
	require.NoError(t, err)

	counts, err := s.CountMessagesByRoutingAll(ctx, acc, true)
	require.NoError(t, err)
	require.Contains(t, counts, RoutingImbox)
	require.Contains(t, counts, RoutingFeed)
	require.Contains(t, counts, RoutingPaperTrail)
	require.Contains(t, counts, RoutingScreener)
	require.Equal(t, 1, counts[RoutingImbox])
	require.Equal(t, 1, counts[RoutingFeed])
	require.Equal(t, 0, counts[RoutingPaperTrail])
	require.Equal(t, 0, counts[RoutingScreener])
}

func TestListMessagesByRoutingUsesIndex(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		msg := SyntheticMessage(acc, folder.ID, i, time.Now())
		require.NoError(t, s.UpsertMessage(ctx, msg))
	}
	_, err := s.SetSenderRouting(ctx, acc, "sender0@example.invalid", RoutingFeed)
	require.NoError(t, err)

	// EXPLAIN QUERY PLAN against the production-shape query.
	st, ok := s.(*store)
	require.True(t, ok)
	rows, err := st.db.QueryContext(ctx, `EXPLAIN QUERY PLAN
		SELECT id FROM messages
		WHERE account_id = ?
		AND lower(trim(from_address)) IN (
			SELECT email_address FROM sender_routing
			WHERE account_id = ? AND destination = ?
		)
		ORDER BY received_at DESC LIMIT 100`,
		acc, acc, RoutingFeed)
	require.NoError(t, err)
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var id, parent, notused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notused, &detail))
		plan += detail + "\n"
	}
	// The plan must reference at least one of the spec 23 indexes
	// (idx_messages_from_lower for the IN-subquery probe OR
	// idx_messages_account_received for the LIMIT short-circuit) and
	// the sender_routing destination index. A full SCAN of messages
	// without an index is the failure mode this test guards.
	require.NotContains(t, plan, "SCAN messages",
		"plan must not full-scan messages; got:\n%s", plan)
	hasMessagesIdx := strings.Contains(plan, "idx_messages_from_lower") ||
		strings.Contains(plan, "idx_messages_account_received_routed")
	require.True(t, hasMessagesIdx,
		"plan must reference one of the spec 23 messages indexes; got:\n%s", plan)
	require.Contains(t, plan, "idx_sender_routing_account_dest",
		"plan must use the sender_routing destination index; got:\n%s", plan)
}

func TestSenderRoutingFKCascadeOnAccountDelete(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	_, err := s.SetSenderRouting(ctx, acc, "a@x.invalid", RoutingFeed)
	require.NoError(t, err)

	st, ok := s.(*store)
	require.True(t, ok)
	_, err = st.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, acc)
	require.NoError(t, err)
	rows, err := s.ListSenderRoutings(ctx, acc, "")
	require.NoError(t, err)
	require.Empty(t, rows, "FK cascade should remove routing rows on account delete")
}

func TestMigration011AppliesCleanly(t *testing.T) {
	s := OpenTestStore(t)
	st, ok := s.(*store)
	require.True(t, ok)
	ctx := context.Background()

	var version string
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT value FROM schema_meta WHERE key = 'version'`).Scan(&version))
	// Migrations run cumulatively; SchemaVersion increments with each
	// new migration. This test guards that migration 011 still
	// applies cleanly (its objects are present, see below); the
	// version cap floats with the latest landed migration.
	v := strings.TrimSpace(version)
	require.True(t, v == "11" || v == "12" || v == "13",
		"schema_meta.version should be at the spec 23 level or above; got %q", v)

	// sender_routing table exists.
	var name string
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='sender_routing'`).Scan(&name))
	require.Equal(t, "sender_routing", name)

	// idx_messages_from_lower exists.
	var idx string
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_messages_from_lower'`).Scan(&idx))
	require.Equal(t, "idx_messages_from_lower", idx)
}
