package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMessageIDsInConversationExcludesDraftTrash(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	inbox := SeedFolder(t, s, acc)
	for _, wk := range []struct{ id, wkn string }{
		{"f-drafts", "drafts"},
		{"f-deleted", "deleteditems"},
		{"f-junk", "junkemail"},
	} {
		require.NoError(t, s.UpsertFolder(ctx, Folder{
			ID: wk.id, AccountID: acc, DisplayName: wk.id, WellKnownName: wk.wkn,
		}))
	}

	convID := "conv-test"
	msgs := []Message{
		{ID: "m-inbox", AccountID: acc, FolderID: inbox.ID, ConversationID: convID, ReceivedAt: time.Now()},
		{ID: "m-drafts", AccountID: acc, FolderID: "f-drafts", ConversationID: convID, ReceivedAt: time.Now()},
		{ID: "m-deleted", AccountID: acc, FolderID: "f-deleted", ConversationID: convID, ReceivedAt: time.Now()},
		{ID: "m-junk", AccountID: acc, FolderID: "f-junk", ConversationID: convID, ReceivedAt: time.Now()},
	}
	require.NoError(t, s.UpsertMessagesBatch(ctx, msgs))

	ids, err := s.MessageIDsInConversation(ctx, acc, convID, false)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	require.Equal(t, "m-inbox", ids[0])
}

func TestMessageIDsInConversationIncludeAllFolders(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	inbox := SeedFolder(t, s, acc)
	require.NoError(t, s.UpsertFolder(ctx, Folder{
		ID: "f-drafts2", AccountID: acc, DisplayName: "Drafts", WellKnownName: "drafts",
	}))

	convID := "conv-all"
	msgs := []Message{
		{ID: "m-inbox2", AccountID: acc, FolderID: inbox.ID, ConversationID: convID, ReceivedAt: time.Now()},
		{ID: "m-drafts2", AccountID: acc, FolderID: "f-drafts2", ConversationID: convID, ReceivedAt: time.Now()},
	}
	require.NoError(t, s.UpsertMessagesBatch(ctx, msgs))

	ids, err := s.MessageIDsInConversation(ctx, acc, convID, true)
	require.NoError(t, err)
	require.Len(t, ids, 2)
}

func TestMessageIDsInConversationEmptyConvID(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	ids, err := s.MessageIDsInConversation(ctx, acc, "", false)
	require.NoError(t, err)
	require.Nil(t, ids)
}

func BenchmarkMessageIDsInConversation(b *testing.B) {
	s := OpenTestStore(b)
	acc := SeedAccount(b, s)
	folder := SeedFolder(b, s, acc)
	ctx := context.Background()

	const totalMsgs = 100_000
	SeedMessages(ctx, b, s, acc, folder.ID, totalMsgs)

	// Use the conversation ID from the first synthetic message.
	msg := SyntheticMessage(acc, folder.ID, 0, time.Now())
	targetConv := msg.ConversationID

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ids, err := s.MessageIDsInConversation(ctx, acc, targetConv, false)
		if err != nil {
			b.Fatal(err)
		}
		_ = ids
	}

	elapsed := b.Elapsed()
	if b.N > 0 {
		avgMs := float64(elapsed.Milliseconds()) / float64(b.N)
		p95ms := avgMs * 1.05
		const budgetMs = 5
		if p95ms > budgetMs {
			b.Errorf("BenchmarkMessageIDsInConversation: avg %.2fms exceeds %dms budget", avgMs, budgetMs)
		}
	}
}
