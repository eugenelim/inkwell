package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMuteConversationIdempotent(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	// First mute — should succeed.
	require.NoError(t, s.MuteConversation(ctx, acc, "conv-abc"))

	// Second mute on same ID — must not fail (idempotent).
	require.NoError(t, s.MuteConversation(ctx, acc, "conv-abc"))

	// Confirm it's muted.
	muted, err := s.IsConversationMuted(ctx, acc, "conv-abc")
	require.NoError(t, err)
	require.True(t, muted)
}

func TestUnmuteConversationNoop(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	// Unmute of a never-muted conversation must not error.
	require.NoError(t, s.UnmuteConversation(ctx, acc, "conv-never-muted"))

	// Mute then unmute — should no longer be muted.
	require.NoError(t, s.MuteConversation(ctx, acc, "conv-xyz"))
	require.NoError(t, s.UnmuteConversation(ctx, acc, "conv-xyz"))
	muted, err := s.IsConversationMuted(ctx, acc, "conv-xyz")
	require.NoError(t, err)
	require.False(t, muted)
}

func TestListMessagesExcludesMuted(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()

	// Insert two messages: one muted, one not.
	msg1 := SyntheticMessage(acc, folder.ID, 0, time.Now())
	msg1.ConversationID = "conv-muted"
	msg2 := SyntheticMessage(acc, folder.ID, 1, time.Now())
	msg2.ConversationID = "conv-visible"
	require.NoError(t, s.UpsertMessagesBatch(ctx, []Message{msg1, msg2}))

	require.NoError(t, s.MuteConversation(ctx, acc, "conv-muted"))

	got, err := s.ListMessages(ctx, MessageQuery{
		AccountID:    acc,
		FolderID:     folder.ID,
		ExcludeMuted: true,
	})
	require.NoError(t, err)
	// Only the non-muted message should appear.
	require.Len(t, got, 1)
	require.Equal(t, msg2.ID, got[0].ID)
}

func TestListMessagesNullConvIDNotFiltered(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()

	// Messages with empty conversation_id must not be suppressed by
	// ExcludeMuted — they can never be in muted_conversations.
	msg := SyntheticMessage(acc, folder.ID, 0, time.Now())
	msg.ConversationID = "" // no conversation ID
	require.NoError(t, s.UpsertMessage(ctx, msg))

	got, err := s.ListMessages(ctx, MessageQuery{
		AccountID:    acc,
		FolderID:     folder.ID,
		ExcludeMuted: true,
	})
	require.NoError(t, err)
	require.Len(t, got, 1, "message with empty conversation_id must not be filtered out")
}

func TestListMutedMessages(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()

	msg1 := SyntheticMessage(acc, folder.ID, 0, time.Now())
	msg1.ConversationID = "conv-a"
	msg2 := SyntheticMessage(acc, folder.ID, 1, time.Now())
	msg2.ConversationID = "conv-b"
	msg3 := SyntheticMessage(acc, folder.ID, 2, time.Now())
	msg3.ConversationID = "conv-not-muted"
	require.NoError(t, s.UpsertMessagesBatch(ctx, []Message{msg1, msg2, msg3}))

	require.NoError(t, s.MuteConversation(ctx, acc, "conv-a"))
	require.NoError(t, s.MuteConversation(ctx, acc, "conv-b"))

	got, err := s.ListMutedMessages(ctx, acc, 100)
	require.NoError(t, err)
	require.Len(t, got, 2, "only muted conversations should appear")
	ids := map[string]bool{got[0].ID: true, got[1].ID: true}
	require.True(t, ids[msg1.ID])
	require.True(t, ids[msg2.ID])
}

func TestCountMutedConversations(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	n, err := s.CountMutedConversations(ctx, acc)
	require.NoError(t, err)
	require.Equal(t, 0, n)

	require.NoError(t, s.MuteConversation(ctx, acc, "conv-1"))
	require.NoError(t, s.MuteConversation(ctx, acc, "conv-2"))

	n, err = s.CountMutedConversations(ctx, acc)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func BenchmarkListMessagesExcludeMuted(b *testing.B) {
	s := OpenTestStore(b)
	acc := SeedAccount(b, s)
	folder := SeedFolder(b, s, acc)
	ctx := context.Background()

	// Seed 100k messages with 500 distinct muted conversations.
	const totalMsgs = 100_000
	const mutedConvs = 500
	SeedMessages(ctx, b, s, acc, folder.ID, totalMsgs)
	for i := 0; i < mutedConvs; i++ {
		convID := SyntheticMessage(acc, folder.ID, i*8, time.Now()).ConversationID
		if err := s.MuteConversation(ctx, acc, convID); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msgs, err := s.ListMessages(ctx, MessageQuery{
			AccountID:    acc,
			FolderID:     folder.ID,
			Limit:        100,
			ExcludeMuted: true,
		})
		if err != nil {
			b.Fatal(err)
		}
		_ = msgs
	}

	elapsed := b.Elapsed()
	p95ms := float64(elapsed.Milliseconds()) / float64(b.N) * 1.05 // rough p95 approximation
	const budgetMs = 10
	if p95ms > budgetMs {
		b.Errorf("BenchmarkListMessagesExcludeMuted: avg %.2fms exceeds %dms budget", p95ms, budgetMs)
	}
}

func BenchmarkMuteUnmute(b *testing.B) {
	s := OpenTestStore(b)
	acc := SeedAccount(b, s)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.MuteConversation(ctx, acc, "bench-conv"); err != nil {
			b.Fatal(err)
		}
		if err := s.UnmuteConversation(ctx, acc, "bench-conv"); err != nil {
			b.Fatal(err)
		}
	}

	elapsed := b.Elapsed()
	avgMs := float64(elapsed.Milliseconds()) / float64(b.N)
	const budgetMs = 1
	if avgMs > budgetMs {
		b.Errorf("BenchmarkMuteUnmute: avg %.3fms exceeds %dms budget", avgMs, budgetMs)
	}
}
