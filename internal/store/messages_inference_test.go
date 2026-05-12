package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seedInferenceMessages writes a deterministic mix into folderID:
// - 5 focused, 3 unread / 2 read
// - 4 other,   2 unread / 2 read
// - 2 empty inference_class (untagged)
// Senders are distinct per row so per-sender routing can target them.
func seedInferenceMessages(t testing.TB, ctx context.Context, s Store, accountID int64, folderID string) {
	t.Helper()
	base := time.Now()
	msgs := []Message{}
	add := func(idx int, cls string, isRead bool) {
		m := SyntheticMessage(accountID, folderID, idx, base)
		m.InferenceClass = cls
		m.IsRead = isRead
		msgs = append(msgs, m)
	}
	// focused: 5 rows, 3 unread, 2 read
	add(1, InferenceClassFocused, false)
	add(2, InferenceClassFocused, false)
	add(3, InferenceClassFocused, false)
	add(4, InferenceClassFocused, true)
	add(5, InferenceClassFocused, true)
	// other: 4 rows, 2 unread, 2 read
	add(6, InferenceClassOther, false)
	add(7, InferenceClassOther, false)
	add(8, InferenceClassOther, true)
	add(9, InferenceClassOther, true)
	// untagged
	add(10, "", false)
	add(11, "", true)
	require.NoError(t, s.UpsertMessagesBatch(ctx, msgs))
}

func TestListMessagesByInferenceClassFocused(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedInferenceMessages(t, ctx, s, acc, f.ID)

	out, err := s.ListMessagesByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, 0, true, false)
	require.NoError(t, err)
	require.Len(t, out, 5)
	for _, m := range out {
		require.Equal(t, InferenceClassFocused, m.InferenceClass)
	}
	// Sorted received_at DESC.
	for i := 1; i < len(out); i++ {
		require.False(t, out[i-1].ReceivedAt.Before(out[i].ReceivedAt))
	}
}

func TestListMessagesByInferenceClassOther(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedInferenceMessages(t, ctx, s, acc, f.ID)

	out, err := s.ListMessagesByInferenceClass(ctx, acc, f.ID, InferenceClassOther, 0, true, false)
	require.NoError(t, err)
	require.Len(t, out, 4)
	for _, m := range out {
		require.Equal(t, InferenceClassOther, m.InferenceClass)
	}
}

func TestListMessagesByInferenceClassEmptyClassExcluded(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedInferenceMessages(t, ctx, s, acc, f.ID)

	// Untagged rows must appear in neither segment.
	foc, err := s.ListMessagesByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, 0, true, false)
	require.NoError(t, err)
	oth, err := s.ListMessagesByInferenceClass(ctx, acc, f.ID, InferenceClassOther, 0, true, false)
	require.NoError(t, err)
	require.Equal(t, 9, len(foc)+len(oth))
}

func TestListMessagesByInferenceClassExcludeMuted(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedInferenceMessages(t, ctx, s, acc, f.ID)

	// Mute the conversation of the first focused message (idx 1 → conv-0).
	require.NoError(t, s.MuteConversation(ctx, acc, "conv-0"))

	with, err := s.ListMessagesByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, 0, false, false)
	require.NoError(t, err)
	without, err := s.ListMessagesByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, 0, true, false)
	require.NoError(t, err)
	require.Greater(t, len(with), len(without), "exclude_muted must drop at least one row")
}

func TestListMessagesByInferenceClassExcludeScreenedOut(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedInferenceMessages(t, ctx, s, acc, f.ID)

	// Route sender of idx 1 (focused, unread) to screener.
	_, err := s.SetSenderRouting(ctx, acc, "sender1@example.invalid", RoutingScreener)
	require.NoError(t, err)

	include, err := s.ListMessagesByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, 0, true, false)
	require.NoError(t, err)
	exclude, err := s.ListMessagesByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, 0, true, true)
	require.NoError(t, err)
	require.Equal(t, len(include)-1, len(exclude), "screened-out sender dropped exactly one row")
	for _, m := range exclude {
		require.NotEqual(t, "sender1@example.invalid", m.FromAddress)
	}
}

func TestListMessagesByInferenceClassRejectsInvalidCls(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	ctx := context.Background()

	for _, cls := range []string{"", "both", "none", "Focused"} {
		_, err := s.ListMessagesByInferenceClass(ctx, acc, f.ID, cls, 0, true, false)
		require.Error(t, err, "cls=%q", cls)
		require.True(t, errors.Is(err, ErrInvalidInferenceClass), "cls=%q", cls)
	}
	_, err := s.CountUnreadByInferenceClass(ctx, acc, f.ID, "junk", true, false)
	require.True(t, errors.Is(err, ErrInvalidInferenceClass))
}

func TestCountUnreadByInferenceClassFocused(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedInferenceMessages(t, ctx, s, acc, f.ID)

	n, err := s.CountUnreadByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, true, false)
	require.NoError(t, err)
	require.Equal(t, 3, n)
}

func TestCountUnreadByInferenceClassOther(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedInferenceMessages(t, ctx, s, acc, f.ID)

	n, err := s.CountUnreadByInferenceClass(ctx, acc, f.ID, InferenceClassOther, true, false)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestCountUnreadByInferenceClassRespectsMute(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedInferenceMessages(t, ctx, s, acc, f.ID)

	require.NoError(t, s.MuteConversation(ctx, acc, "conv-0"))

	with, err := s.CountUnreadByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, false, false)
	require.NoError(t, err)
	without, err := s.CountUnreadByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, true, false)
	require.NoError(t, err)
	require.Greater(t, with, without)
}

func TestCountUnreadByInferenceClassRespectsScreener(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	ctx := context.Background()
	seedInferenceMessages(t, ctx, s, acc, f.ID)

	_, err := s.SetSenderRouting(ctx, acc, "sender1@example.invalid", RoutingScreener)
	require.NoError(t, err)

	include, err := s.CountUnreadByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, true, false)
	require.NoError(t, err)
	exclude, err := s.CountUnreadByInferenceClass(ctx, acc, f.ID, InferenceClassFocused, true, true)
	require.NoError(t, err)
	require.Equal(t, include-1, exclude)
}
