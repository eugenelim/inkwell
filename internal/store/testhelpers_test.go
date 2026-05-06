package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// OpenTestStore opens a fresh DB in t.TempDir() and returns it.
func OpenTestStore(t testing.TB) Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	s, err := Open(path, DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// SeedAccount inserts a minimal account and returns its id.
func SeedAccount(t testing.TB, s Store) int64 {
	t.Helper()
	id, err := s.PutAccount(context.Background(), Account{
		TenantID: "tenant-1",
		ClientID: "client-1",
		UPN:      "tester@example.invalid",
	})
	require.NoError(t, err)
	return id
}

// SeedFolder inserts an inbox folder for accountID.
func SeedFolder(t testing.TB, s Store, accountID int64) Folder {
	t.Helper()
	f := Folder{
		ID:            "folder-inbox",
		AccountID:     accountID,
		DisplayName:   "Inbox",
		WellKnownName: "inbox",
		LastSyncedAt:  time.Now(),
	}
	require.NoError(t, s.UpsertFolder(context.Background(), f))
	return f
}

// SyntheticMessage produces a deterministic message at the given offset.
func SyntheticMessage(accountID int64, folderID string, offset int, base time.Time) Message {
	id := "msg-" + strconv.Itoa(offset)
	return Message{
		ID:                id,
		AccountID:         accountID,
		FolderID:          folderID,
		InternetMessageID: "<" + id + "@example.invalid>",
		ConversationID:    fmt.Sprintf("conv-%d", offset/8),
		Subject:           fmt.Sprintf("Synthetic subject %d about meeting", offset),
		BodyPreview:       fmt.Sprintf("preview body %d about review and budget", offset),
		FromAddress:       fmt.Sprintf("sender%d@example.invalid", offset%100),
		FromName:          fmt.Sprintf("Sender %d", offset%100),
		ToAddresses:       []EmailAddress{{Address: "tester@example.invalid"}},
		ReceivedAt:        base.Add(-time.Duration(offset) * time.Minute),
		SentAt:            base.Add(-time.Duration(offset) * time.Minute).Add(-time.Second),
		IsRead:            offset%4 == 0,
		Importance:        "normal",
		HasAttachments:    offset%9 == 0,
		Categories:        []string{"work"},
	}
}

// SeedMessages writes n synthetic messages in batches of 1000.
func SeedMessages(ctx context.Context, t testing.TB, s Store, accountID int64, folderID string, n int) {
	t.Helper()
	base := time.Now()
	const batch = 1000
	buf := make([]Message, 0, batch)
	for i := 0; i < n; i++ {
		buf = append(buf, SyntheticMessage(accountID, folderID, i, base))
		if len(buf) == batch {
			require.NoError(t, s.UpsertMessagesBatch(ctx, buf))
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		require.NoError(t, s.UpsertMessagesBatch(ctx, buf))
	}
}
