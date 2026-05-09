package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seedRoutings inserts approve / screener routings for the listed
// addresses and returns the (now-routed) addresses for assertions.
func seedRouting(t *testing.T, s Store, accountID int64, addr, dest string) {
	t.Helper()
	_, err := s.SetSenderRouting(context.Background(), accountID, addr, dest)
	require.NoError(t, err)
}

// seedMsg inserts a single message with the supplied address and
// received-at offset (older offset = older message).
func seedMsg(t *testing.T, s Store, accountID int64, folderID string, id, addr, subject string, receivedOffsetMin int) {
	t.Helper()
	require.NoError(t, s.UpsertMessage(context.Background(), Message{
		ID:                id,
		AccountID:         accountID,
		FolderID:          folderID,
		InternetMessageID: "<" + id + "@example.invalid>",
		ConversationID:    "conv-" + id,
		Subject:           subject,
		FromAddress:       addr,
		FromName:          "Sender",
		ToAddresses:       []EmailAddress{{Address: "me@example.invalid"}},
		ReceivedAt:        time.Now().Add(-time.Duration(receivedOffsetMin) * time.Minute),
		SentAt:            time.Now().Add(-time.Duration(receivedOffsetMin) * time.Minute),
		Importance:        "normal",
	}))
}

// TestApplyScreenerFilterApprovedOnly verifies only Approved
// senders' mail returns when the filter is true.
func TestApplyScreenerFilterApprovedOnly(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	seedRouting(t, s, acc, "imbox@example.invalid", RoutingImbox)
	seedRouting(t, s, acc, "feed@example.invalid", RoutingFeed)
	seedRouting(t, s, acc, "trail@example.invalid", RoutingPaperTrail)
	seedRouting(t, s, acc, "out@example.invalid", RoutingScreener)
	seedMsg(t, s, acc, f.ID, "m-imbox", "imbox@example.invalid", "imbox", 5)
	seedMsg(t, s, acc, f.ID, "m-feed", "feed@example.invalid", "feed", 4)
	seedMsg(t, s, acc, f.ID, "m-trail", "trail@example.invalid", "trail", 3)
	seedMsg(t, s, acc, f.ID, "m-out", "out@example.invalid", "screened-out", 2)
	seedMsg(t, s, acc, f.ID, "m-pending", "pending@example.invalid", "pending", 1)

	got, err := s.ListMessages(context.Background(), MessageQuery{
		AccountID:           acc,
		FolderID:            f.ID,
		ApplyScreenerFilter: true,
	})
	require.NoError(t, err)
	ids := messageIDs(got)
	require.ElementsMatch(t, []string{"m-imbox", "m-feed", "m-trail"}, ids)
}

// TestApplyScreenerFilterDefaultFalse verifies behaviour matches
// spec 23 v1 when the flag is left at its zero value.
func TestApplyScreenerFilterDefaultFalse(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	seedRouting(t, s, acc, "imbox@example.invalid", RoutingImbox)
	seedRouting(t, s, acc, "out@example.invalid", RoutingScreener)
	seedMsg(t, s, acc, f.ID, "m-imbox", "imbox@example.invalid", "imbox", 3)
	seedMsg(t, s, acc, f.ID, "m-out", "out@example.invalid", "out", 2)
	seedMsg(t, s, acc, f.ID, "m-pending", "pending@example.invalid", "pending", 1)

	got, err := s.ListMessages(context.Background(), MessageQuery{
		AccountID: acc,
		FolderID:  f.ID,
	})
	require.NoError(t, err)
	require.Len(t, got, 3, "filter false → all three rows")
}

// TestApplyScreenerFilterNullFromAddress verifies NULL / empty
// from_address is NEVER suppressed (defensive: drafts and synthesised
// list-server messages predate any routing decision).
func TestApplyScreenerFilterNullFromAddress(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	seedRouting(t, s, acc, "imbox@example.invalid", RoutingImbox)
	// Draft-shaped row with empty from_address.
	require.NoError(t, s.UpsertMessage(context.Background(), Message{
		ID: "m-draft", AccountID: acc, FolderID: f.ID,
		Subject: "draft", ReceivedAt: time.Now(),
		Importance: "normal",
	}))
	seedMsg(t, s, acc, f.ID, "m-imbox", "imbox@example.invalid", "imbox", 1)

	got, err := s.ListMessages(context.Background(), MessageQuery{
		AccountID:           acc,
		FolderID:            f.ID,
		ApplyScreenerFilter: true,
	})
	require.NoError(t, err)
	ids := messageIDs(got)
	require.Contains(t, ids, "m-draft", "empty from_address must NEVER be suppressed")
	require.Contains(t, ids, "m-imbox")
}

// TestListPendingSendersOrderingAndDedupe verifies one row per
// sender, newest representative, ordered by received_at DESC.
func TestListPendingSendersOrderingAndDedupe(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	// Two pending senders, three messages: alice (2 msgs), bob (1 msg).
	seedMsg(t, s, acc, f.ID, "m-a-old", "alice@example.invalid", "old", 30)
	seedMsg(t, s, acc, f.ID, "m-a-new", "alice@example.invalid", "new", 10)
	seedMsg(t, s, acc, f.ID, "m-b", "bob@example.invalid", "hi", 20)

	got, err := s.ListPendingSenders(context.Background(), acc, 50, 999, true)
	require.NoError(t, err)
	require.Len(t, got, 2, "one row per sender")
	// Alice newest first (10min ago) before Bob (20min ago).
	require.Equal(t, "alice@example.invalid", got[0].EmailAddress)
	require.Equal(t, "m-a-new", got[0].LatestMessageID, "newest representative")
	require.Equal(t, "new", got[0].LatestSubject)
	require.Equal(t, 2, got[0].MessageCount)
	require.Equal(t, "bob@example.invalid", got[1].EmailAddress)
	require.Equal(t, 1, got[1].MessageCount)
}

// TestListPendingSendersExcludesApproved verifies senders with any
// sender_routing row (incl. destination='screener') are excluded.
func TestListPendingSendersExcludesApproved(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	seedRouting(t, s, acc, "approved@example.invalid", RoutingImbox)
	seedRouting(t, s, acc, "screened@example.invalid", RoutingScreener)
	seedMsg(t, s, acc, f.ID, "m-approved", "approved@example.invalid", "x", 30)
	seedMsg(t, s, acc, f.ID, "m-screened", "screened@example.invalid", "y", 20)
	seedMsg(t, s, acc, f.ID, "m-pending", "pending@example.invalid", "z", 10)

	got, err := s.ListPendingSenders(context.Background(), acc, 50, 999, true)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "pending@example.invalid", got[0].EmailAddress)
}

// TestListPendingSendersMessageCountCap verifies the SQL-side LIMIT
// caps the per-sender count at capPerSender + 1 so the UI can render
// "999+" without the count subquery dominating.
func TestListPendingSendersMessageCountCap(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	for i := 0; i < 25; i++ {
		seedMsg(t, s, acc, f.ID, "m-noisy-"+strings.Repeat("0", 3-len(itoa(i)))+itoa(i), "noisy@example.invalid", "n", i)
	}
	got, err := s.ListPendingSenders(context.Background(), acc, 50, 10, true)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 11, got[0].MessageCount, "cap+1 = 10+1 = 11 (saturation marker)")
}

// TestListPendingSendersExcludesMuted verifies that messages whose
// conversation is muted do not contribute to the count or row
// representative when excludeMuted=true.
func TestListPendingSendersExcludesMuted(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	// alice has one normal + one muted message.
	seedMsg(t, s, acc, f.ID, "m-a-normal", "alice@example.invalid", "normal", 20)
	seedMsg(t, s, acc, f.ID, "m-a-muted", "alice@example.invalid", "muted", 10)
	require.NoError(t, s.MuteConversation(context.Background(), acc, "conv-m-a-muted"))

	got, err := s.ListPendingSenders(context.Background(), acc, 50, 999, true)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "alice@example.invalid", got[0].EmailAddress)
	require.Equal(t, 1, got[0].MessageCount, "muted message must not count")
	require.Equal(t, "m-a-normal", got[0].LatestMessageID, "muted must not become representative")
}

// TestListPendingSendersIncludesMutedWhenFlagFalse covers the inverse.
func TestListPendingSendersIncludesMutedWhenFlagFalse(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	seedMsg(t, s, acc, f.ID, "m-a", "alice@example.invalid", "x", 5)
	require.NoError(t, s.MuteConversation(context.Background(), acc, "conv-m-a"))

	got, err := s.ListPendingSenders(context.Background(), acc, 50, 999, false)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 1, got[0].MessageCount)
}

// TestListPendingMessagesParity ensures per-message mode returns the
// raw rows ordered by received_at DESC.
func TestListPendingMessagesParity(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	seedMsg(t, s, acc, f.ID, "m-old", "alice@example.invalid", "x", 30)
	seedMsg(t, s, acc, f.ID, "m-new", "alice@example.invalid", "y", 10)
	seedMsg(t, s, acc, f.ID, "m-bob", "bob@example.invalid", "z", 20)
	seedRouting(t, s, acc, "approved@example.invalid", RoutingImbox)
	seedMsg(t, s, acc, f.ID, "m-approved", "approved@example.invalid", "ok", 15)

	got, err := s.ListPendingMessages(context.Background(), acc, 50, false)
	require.NoError(t, err)
	ids := messageIDs(got)
	require.ElementsMatch(t, []string{"m-old", "m-new", "m-bob"}, ids, "approved sender excluded")
	require.Equal(t, "m-new", got[0].ID, "ordered DESC by received_at")
}

// TestListScreenedOutMessages verifies only destination='screener'
// senders' mail returns.
func TestListScreenedOutMessages(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	seedRouting(t, s, acc, "out@example.invalid", RoutingScreener)
	seedRouting(t, s, acc, "in@example.invalid", RoutingImbox)
	seedMsg(t, s, acc, f.ID, "m-out", "out@example.invalid", "x", 10)
	seedMsg(t, s, acc, f.ID, "m-in", "in@example.invalid", "y", 5)
	seedMsg(t, s, acc, f.ID, "m-pending", "pending@example.invalid", "z", 3)

	got, err := s.ListScreenedOutMessages(context.Background(), acc, 50, true)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "m-out", got[0].ID)
}

// TestCountPendingSendersDistinct counts unique addresses, not
// messages.
func TestCountPendingSendersDistinct(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	for i := 0; i < 5; i++ {
		seedMsg(t, s, acc, f.ID, "m-"+itoa(i), "alice@example.invalid", "x", i)
	}
	seedMsg(t, s, acc, f.ID, "m-bob", "bob@example.invalid", "y", 10)
	seedRouting(t, s, acc, "approved@example.invalid", RoutingImbox)
	seedMsg(t, s, acc, f.ID, "m-approved", "approved@example.invalid", "z", 7)

	n, err := s.CountPendingSenders(context.Background(), acc, true)
	require.NoError(t, err)
	require.Equal(t, 2, n, "two distinct pending senders")
}

// TestCountScreenedOutMessages mirrors len(ListScreenedOutMessages).
func TestCountScreenedOutMessages(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	seedRouting(t, s, acc, "out@example.invalid", RoutingScreener)
	for i := 0; i < 3; i++ {
		seedMsg(t, s, acc, f.ID, "m-out-"+itoa(i), "out@example.invalid", "x", i)
	}
	n, err := s.CountScreenedOutMessages(context.Background(), acc, true)
	require.NoError(t, err)
	require.Equal(t, 3, n)
}

// TestCountMessagesFromPendingSenders covers the modal copy path.
func TestCountMessagesFromPendingSenders(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	for i := 0; i < 4; i++ {
		seedMsg(t, s, acc, f.ID, "m-a-"+itoa(i), "alice@example.invalid", "x", i)
	}
	seedMsg(t, s, acc, f.ID, "m-bob", "bob@example.invalid", "y", 10)
	seedRouting(t, s, acc, "approved@example.invalid", RoutingImbox)
	seedMsg(t, s, acc, f.ID, "m-approved", "approved@example.invalid", "z", 5)

	n, err := s.CountMessagesFromPendingSenders(context.Background(), acc, true)
	require.NoError(t, err)
	require.Equal(t, 5, n, "4 alice + 1 bob = 5; approved excluded")
}

// messageIDs is a tiny helper for ID-only assertions across tests.
func messageIDs(ms []Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}
