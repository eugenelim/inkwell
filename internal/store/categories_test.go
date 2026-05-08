package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func putMsg(t *testing.T, s Store, accID int64, folderID, id string, cats []string, isRead bool) {
	t.Helper()
	require.NoError(t, s.UpsertMessage(context.Background(), Message{
		ID:          id,
		AccountID:   accID,
		FolderID:    folderID,
		Subject:     "x",
		FromAddress: "a@x.invalid",
		ReceivedAt:  time.Now(),
		IsRead:      isRead,
		Categories:  cats,
	}))
}

func TestIsInkwellCategory(t *testing.T) {
	require.True(t, IsInkwellCategory("Inkwell/ReplyLater"))
	require.True(t, IsInkwellCategory("inkwell/replylater"))
	require.True(t, IsInkwellCategory("INKWELL/SETASIDE"))
	require.False(t, IsInkwellCategory("MyReplyLater"))
	require.False(t, IsInkwellCategory(""))
}

func TestIsInCategory(t *testing.T) {
	require.True(t, IsInCategory([]string{"Foo", "Inkwell/ReplyLater"}, CategoryReplyLater))
	require.True(t, IsInCategory([]string{"inkwell/replylater"}, CategoryReplyLater))
	require.False(t, IsInCategory([]string{"foo"}, CategoryReplyLater))
	require.False(t, IsInCategory(nil, CategoryReplyLater))
}

func TestCountMessagesInCategoryExcludesDrafts(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	require.NoError(t, s.UpsertFolder(ctx, Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertFolder(ctx, Folder{
		ID: "f-drafts", AccountID: acc, DisplayName: "Drafts", WellKnownName: "drafts", LastSyncedAt: time.Now(),
	}))
	putMsg(t, s, acc, "f-inbox", "m1", []string{CategoryReplyLater}, false)
	putMsg(t, s, acc, "f-drafts", "m2", []string{CategoryReplyLater}, false)
	n, err := s.CountMessagesInCategory(ctx, acc, CategoryReplyLater)
	require.NoError(t, err)
	require.Equal(t, 1, n, "draft must be excluded")
}

func TestCountMessagesInCategoryExcludesJunkAndTrash(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()
	require.NoError(t, s.UpsertFolder(ctx, Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertFolder(ctx, Folder{
		ID: "f-junk", AccountID: acc, DisplayName: "Junk", WellKnownName: "junkemail", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertFolder(ctx, Folder{
		ID: "f-trash", AccountID: acc, DisplayName: "Deleted", WellKnownName: "deleteditems", LastSyncedAt: time.Now(),
	}))
	putMsg(t, s, acc, "f-inbox", "m1", []string{CategoryReplyLater}, false)
	putMsg(t, s, acc, "f-junk", "m2", []string{CategoryReplyLater}, false)
	putMsg(t, s, acc, "f-trash", "m3", []string{CategoryReplyLater}, false)
	n, err := s.CountMessagesInCategory(ctx, acc, CategoryReplyLater)
	require.NoError(t, err)
	require.Equal(t, 1, n, "junk + trash excluded")
}

func TestCountMessagesInCategoryIncludesMuted(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ID: "m1", AccountID: acc, FolderID: folder.ID,
		Subject: "x", FromAddress: "a@x.invalid",
		ConversationID: "conv-1",
		ReceivedAt:     time.Now(),
		Categories:     []string{CategoryReplyLater},
	}))
	require.NoError(t, s.MuteConversation(ctx, acc, "conv-1"))
	n, err := s.CountMessagesInCategory(ctx, acc, CategoryReplyLater)
	require.NoError(t, err)
	require.Equal(t, 1, n, "stack views ignore mute")
}

func TestCountMessagesInCategoryCaseInsensitive(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()
	// Lowercase tag (e.g. user typed it that way in Outlook web).
	putMsg(t, s, acc, folder.ID, "m1", []string{"inkwell/replylater"}, false)
	n, err := s.CountMessagesInCategory(ctx, acc, CategoryReplyLater)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestListMessagesInCategoryOrderedByReceivedDesc(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ID: "old", AccountID: acc, FolderID: folder.ID,
		FromAddress: "a@x.invalid", ReceivedAt: now.Add(-time.Hour),
		Categories: []string{CategoryReplyLater},
	}))
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ID: "new", AccountID: acc, FolderID: folder.ID,
		FromAddress: "a@x.invalid", ReceivedAt: now,
		Categories: []string{CategoryReplyLater},
	}))
	got, err := s.ListMessagesInCategory(ctx, acc, CategoryReplyLater, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "new", got[0].ID)
	require.Equal(t, "old", got[1].ID)
}

func TestListMessagesInCategoryHonoursLimit(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		require.NoError(t, s.UpsertMessage(ctx, Message{
			ID: "m" + string(rune('a'+i)), AccountID: acc, FolderID: folder.ID,
			FromAddress: "a@x.invalid", ReceivedAt: time.Now().Add(-time.Duration(i) * time.Minute),
			Categories: []string{CategoryReplyLater},
		}))
	}
	got, err := s.ListMessagesInCategory(ctx, acc, CategoryReplyLater, 3)
	require.NoError(t, err)
	require.Len(t, got, 3)
}

func TestJSONEachCollateNocaseRoundtrip(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	folder := SeedFolder(t, s, acc)
	ctx := context.Background()
	putMsg(t, s, acc, folder.ID, "m1", []string{"INKWELL/REPLYLATER"}, false)
	// MessageQuery.Categories predicate (widened to COLLATE NOCASE).
	got, err := s.ListMessages(ctx, MessageQuery{
		AccountID:  acc,
		FolderID:   folder.ID,
		Categories: []string{CategoryReplyLater},
	})
	require.NoError(t, err)
	require.Len(t, got, 1, "case-insensitive Categories predicate must match upper-case stored value")
}
