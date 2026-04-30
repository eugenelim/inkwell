package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenCreatesDBFileWithMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.db")
	s, err := Open(path, DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestMigrationsRunOnceAndAreIdempotentOnReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.db")
	s1, err := Open(path, DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	s2, err := Open(path, DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	v, err := readSchemaVersion(context.Background(), s2.(*store).db)
	require.NoError(t, err)
	require.Equal(t, SchemaVersion, v)
}

func TestPutAndGetAccount(t *testing.T) {
	s := OpenTestStore(t)
	id := SeedAccount(t, s)
	require.NotZero(t, id)

	a, err := s.GetAccount(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tester@example.invalid", a.UPN)
	require.Equal(t, "tenant-1", a.TenantID)
}

func TestPutAccountUpsertsByTenantUPN(t *testing.T) {
	s := OpenTestStore(t)
	id1, err := s.PutAccount(context.Background(), Account{TenantID: "T", ClientID: "C", UPN: "u@x.invalid", DisplayName: "Name 1"})
	require.NoError(t, err)
	id2, err := s.PutAccount(context.Background(), Account{TenantID: "T", ClientID: "C", UPN: "u@x.invalid", DisplayName: "Name 2"})
	require.NoError(t, err)
	require.Equal(t, id1, id2)

	a, err := s.GetAccount(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Name 2", a.DisplayName)
}

func TestFolderRoundTripAndDeleteCascades(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)

	got, err := s.GetFolderByWellKnown(context.Background(), acc, "inbox")
	require.NoError(t, err)
	require.Equal(t, f.DisplayName, got.DisplayName)

	// Insert a message under the folder.
	m := SyntheticMessage(acc, f.ID, 0, time.Now())
	require.NoError(t, s.UpsertMessage(context.Background(), m))

	// Delete folder; cascade should drop the message.
	require.NoError(t, s.DeleteFolder(context.Background(), f.ID))
	_, err = s.GetMessage(context.Background(), m.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

// TestAdjustFolderCountsClampsAtZero is the safety invariant for
// the optimistic count adjustment: if the server-side count was
// stale and an optimistic decrement would drive the column
// negative, the SQL clamps at 0. The next sync overwrites with
// Graph's authoritative value anyway, so 0 (vs the actual server
// value) is at worst a sub-cycle visual quirk.
func TestAdjustFolderCountsClampsAtZero(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	require.NoError(t, s.UpsertFolder(context.Background(), Folder{
		ID: "f-stale", AccountID: acc, DisplayName: "Stale",
		TotalCount: 1, UnreadCount: 0, LastSyncedAt: time.Now(),
	}))

	// Decrement past zero — clamp must hold.
	require.NoError(t, s.AdjustFolderCounts(context.Background(), "f-stale", -5, -3))

	folders, err := s.ListFolders(context.Background(), acc)
	require.NoError(t, err)
	require.Len(t, folders, 1)
	require.Equal(t, 0, folders[0].TotalCount, "total clamped at 0 after over-decrement")
	require.Equal(t, 0, folders[0].UnreadCount, "unread clamped at 0 after over-decrement")
}

// TestAdjustFolderCountsNoOpForUnknownFolder confirms the helper
// silently no-ops when the folder ID isn't present locally — the
// optimistic apply path can fire it for both source and
// destination without checking which exists, and a destination
// the user just synced (or hasn't) won't break the call.
func TestAdjustFolderCountsNoOpForUnknownFolder(t *testing.T) {
	s := OpenTestStore(t)
	_ = SeedAccount(t, s)
	// folder "f-ghost" doesn't exist; UPDATE matches 0 rows.
	require.NoError(t, s.AdjustFolderCounts(context.Background(), "f-ghost", -1, -1))
}

func TestUpsertAndQueryMessages(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)

	for i := 0; i < 50; i++ {
		require.NoError(t, s.UpsertMessage(context.Background(), SyntheticMessage(acc, f.ID, i, time.Now())))
	}

	all, err := s.ListMessages(context.Background(), MessageQuery{AccountID: acc, FolderID: f.ID, Limit: 100})
	require.NoError(t, err)
	require.Len(t, all, 50)
	// Default order is received DESC: offset 0 is newest.
	require.Equal(t, "msg-0", all[0].ID)

	unread, err := s.ListMessages(context.Background(), MessageQuery{AccountID: acc, FolderID: f.ID, UnreadOnly: true, Limit: 100})
	require.NoError(t, err)
	for _, m := range unread {
		require.False(t, m.IsRead)
	}
}

// TestSearchByPredicateExcludesDeletedAndJunk is the v0.15.x
// regression for the filter UX bug where pressing `d` (soft-
// delete) on a `:filter [External]` view didn't visually
// remove the row: the message moved to Deleted Items but kept
// matching the predicate, so the re-filter post-triage still
// returned it. SearchByPredicate must default-exclude messages
// in the well-known deleteditems / junkemail folders so the
// re-filter result reflects "what's still in active folders".
func TestSearchByPredicateExcludesDeletedAndJunk(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)

	// Inbox + Deleted Items + Junk Email folders.
	require.NoError(t, s.UpsertFolder(context.Background(), Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertFolder(context.Background(), Folder{
		ID: "f-trash", AccountID: acc, DisplayName: "Deleted Items", WellKnownName: "deleteditems", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertFolder(context.Background(), Folder{
		ID: "f-junk", AccountID: acc, DisplayName: "Junk Email", WellKnownName: "junkemail", LastSyncedAt: time.Now(),
	}))

	// Same subject in three folders.
	for i, fid := range []string{"f-inbox", "f-trash", "f-junk"} {
		require.NoError(t, s.UpsertMessage(context.Background(), Message{
			ID:          "m-" + strconv.Itoa(i),
			AccountID:   acc,
			FolderID:    fid,
			Subject:     "[External] vendor pricing",
			FromAddress: "vendor@example.invalid",
			ReceivedAt:  time.Now(),
		}))
	}

	out, err := s.SearchByPredicate(context.Background(), acc,
		"subject LIKE ?", []any{"%[External]%"}, 100)
	require.NoError(t, err)
	require.Len(t, out, 1, "filter must exclude Deleted Items + Junk Email by default")
	require.Equal(t, "f-inbox", out[0].FolderID)
}

// TestSearchExcludesDeletedAndJunk pins the v0.15.x regression
// where pressing `d` on a `/<query>` result moved the message to
// Deleted Items but FTS still returned it, so re-running the
// search resurrected the row and the user thought delete didn't
// work. Search default-excludes the well-known deleteditems +
// junkemail folders. Caller can opt back in by passing the
// folder ID explicitly.
func TestSearchExcludesDeletedAndJunk(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	require.NoError(t, s.UpsertFolder(context.Background(), Folder{
		ID: "f-inbox", AccountID: acc, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertFolder(context.Background(), Folder{
		ID: "f-trash", AccountID: acc, DisplayName: "Deleted Items", WellKnownName: "deleteditems", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertFolder(context.Background(), Folder{
		ID: "f-junk", AccountID: acc, DisplayName: "Junk Email", WellKnownName: "junkemail", LastSyncedAt: time.Now(),
	}))
	for i, fid := range []string{"f-inbox", "f-trash", "f-junk"} {
		require.NoError(t, s.UpsertMessage(context.Background(), Message{
			ID:          "m-fts-" + strconv.Itoa(i),
			AccountID:   acc,
			FolderID:    fid,
			Subject:     "ABC quarterly review",
			BodyPreview: "ABC body preview text",
			ReceivedAt:  time.Now(),
		}))
	}

	hits, err := s.Search(context.Background(), SearchQuery{
		Query:     "ABC",
		AccountID: acc,
		Limit:     50,
	})
	require.NoError(t, err)
	require.Len(t, hits, 1, "Search must default-exclude trash/junk")
	require.Equal(t, "f-inbox", hits[0].Message.FolderID)

	// Explicit folder scope opts back in — useful for "search inside
	// my Deleted Items" workflows.
	trashHits, err := s.Search(context.Background(), SearchQuery{
		Query:     "ABC",
		AccountID: acc,
		FolderID:  "f-trash",
		Limit:     50,
	})
	require.NoError(t, err)
	require.Len(t, trashHits, 1)
	require.Equal(t, "f-trash", trashHits[0].Message.FolderID)
}

// TestMeetingMessageTypeRoundTrip is the regression for the v0.11-era
// real-tenant bug where the calendar indicator (📅) silently dropped
// off invites whose subject didn't begin with one of the heuristic
// prefixes (Accepted: / Meeting: / etc.). Spec 02 v2 added a
// meeting_message_type column populated from Graph's $select. This
// test asserts the column round-trips through upsert→scan.
func TestMeetingMessageTypeRoundTrip(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	m := Message{
		ID:                 "m-invite-1",
		AccountID:          acc,
		FolderID:           f.ID,
		Subject:            "Q4 sync", // no meeting prefix
		MeetingMessageType: "meetingRequest",
		ReceivedAt:         time.Now(),
	}
	require.NoError(t, s.UpsertMessage(context.Background(), m))
	got, err := s.GetMessage(context.Background(), "m-invite-1")
	require.NoError(t, err)
	require.Equal(t, "meetingRequest", got.MeetingMessageType)
}

func TestUpdateMessageFieldsPartial(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	m := SyntheticMessage(acc, f.ID, 0, time.Now())
	m.IsRead = false
	require.NoError(t, s.UpsertMessage(context.Background(), m))

	read := true
	flag := "flagged"
	require.NoError(t, s.UpdateMessageFields(context.Background(), m.ID, MessageFields{IsRead: &read, FlagStatus: &flag}))

	got, err := s.GetMessage(context.Background(), m.ID)
	require.NoError(t, err)
	require.True(t, got.IsRead)
	require.Equal(t, "flagged", got.FlagStatus)
}

func TestFTSTriggersReflectInsertUpdateDelete(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	m := SyntheticMessage(acc, f.ID, 0, time.Now())
	m.Subject = "Quarterly review meeting"
	m.BodyPreview = "agenda budget revenue"
	require.NoError(t, s.UpsertMessage(context.Background(), m))

	hits, err := s.Search(context.Background(), SearchQuery{Query: "review", Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.Equal(t, m.ID, hits[0].Message.ID)

	// Update subject; FTS should reflect the new term.
	m.Subject = "Cancelled meeting"
	require.NoError(t, s.UpsertMessage(context.Background(), m))
	hits2, err := s.Search(context.Background(), SearchQuery{Query: "review", Limit: 10})
	require.NoError(t, err)
	require.Empty(t, hits2)

	// Delete; FTS should not match.
	require.NoError(t, s.DeleteMessage(context.Background(), m.ID))
	hits3, err := s.Search(context.Background(), SearchQuery{Query: "cancelled", Limit: 10})
	require.NoError(t, err)
	require.Empty(t, hits3)
}

func TestBodyPutAndGetTouch(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	m := SyntheticMessage(acc, f.ID, 0, time.Now())
	require.NoError(t, s.UpsertMessage(context.Background(), m))

	require.NoError(t, s.PutBody(context.Background(), Body{
		MessageID:   m.ID,
		ContentType: "text",
		Content:     "hello world",
	}))
	got, err := s.GetBody(context.Background(), m.ID)
	require.NoError(t, err)
	require.Equal(t, "hello world", got.Content)
	require.Equal(t, int64(11), got.ContentSize)

	prevAccess := got.LastAccessedAt
	time.Sleep(1100 * time.Millisecond)
	require.NoError(t, s.TouchBody(context.Background(), m.ID))
	got2, err := s.GetBody(context.Background(), m.ID)
	require.NoError(t, err)
	require.True(t, got2.LastAccessedAt.After(prevAccess))
}

func TestEvictBodiesRespectsCountAndByteCaps(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)

	for i := 0; i < 10; i++ {
		m := SyntheticMessage(acc, f.ID, i, time.Now())
		require.NoError(t, s.UpsertMessage(context.Background(), m))
		require.NoError(t, s.PutBody(context.Background(), Body{
			MessageID:      m.ID,
			ContentType:    "text",
			Content:        string(make([]byte, 1024)),
			LastAccessedAt: time.Now().Add(time.Duration(i) * time.Second),
		}))
	}

	// Cap at 6 rows: must evict 4 oldest.
	evicted, err := s.EvictBodies(context.Background(), 6, 0)
	require.NoError(t, err)
	require.Equal(t, 4, evicted)
	for i := 0; i < 4; i++ {
		_, err := s.GetBody(context.Background(), "msg-"+itoa(i))
		require.True(t, errors.Is(err, ErrNotFound), "msg-%d should be evicted", i)
	}

	// Now add until ~10KB, then cap at ~3KB.
	for i := 10; i < 14; i++ {
		m := SyntheticMessage(acc, f.ID, i, time.Now())
		require.NoError(t, s.UpsertMessage(context.Background(), m))
		require.NoError(t, s.PutBody(context.Background(), Body{
			MessageID:   m.ID,
			ContentType: "text",
			Content:     string(make([]byte, 1024)),
		}))
	}
	evicted, err = s.EvictBodies(context.Background(), 0, 3*1024)
	require.NoError(t, err)
	require.GreaterOrEqual(t, evicted, 5)
}

func TestActionLifecycle(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	require.NoError(t, s.EnqueueAction(context.Background(), Action{
		ID:         "act-1",
		AccountID:  acc,
		Type:       ActionMove,
		MessageIDs: []string{"m-1", "m-2"},
		Params:     map[string]any{"destination_folder_id": "f-archive"},
	}))

	pending, err := s.PendingActions(context.Background())
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, []string{"m-1", "m-2"}, pending[0].MessageIDs)

	require.NoError(t, s.UpdateActionStatus(context.Background(), "act-1", StatusInFlight, ""))
	require.NoError(t, s.UpdateActionStatus(context.Background(), "act-1", StatusDone, ""))

	pending2, err := s.PendingActions(context.Background())
	require.NoError(t, err)
	require.Empty(t, pending2)
}

func TestUndoStackPushPopMonotonic(t *testing.T) {
	s := OpenTestStore(t)
	for i := 0; i < 3; i++ {
		require.NoError(t, s.PushUndo(context.Background(), UndoEntry{
			ActionType: ActionMove,
			MessageIDs: []string{"m-" + itoa(i)},
			Label:      "Move " + itoa(i),
		}))
	}
	for i := 2; i >= 0; i-- {
		e, err := s.PopUndo(context.Background())
		require.NoError(t, err)
		require.Equal(t, "Move "+itoa(i), e.Label, "stack pops most recent")
	}
	_, err := s.PopUndo(context.Background())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestUndoClearedOnReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.db")
	s1, err := Open(path, DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, s1.PushUndo(context.Background(), UndoEntry{ActionType: ActionMove, Label: "x"}))
	require.NoError(t, s1.Close())

	s2, err := Open(path, DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	_, err = s2.PeekUndo(context.Background())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSavedSearchCRUD(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	require.NoError(t, s.PutSavedSearch(context.Background(), SavedSearch{AccountID: acc, Name: "Newsletters", Pattern: "~f newsletter@*"}))
	list, err := s.ListSavedSearches(context.Background(), acc)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "Newsletters", list[0].Name)
	require.NoError(t, s.DeleteSavedSearch(context.Background(), list[0].ID))

	list2, err := s.ListSavedSearches(context.Background(), acc)
	require.NoError(t, err)
	require.Empty(t, list2)
}

func TestDeltaTokenRoundTrip(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	require.NoError(t, s.PutDeltaToken(context.Background(), DeltaToken{
		AccountID:   acc,
		FolderID:    f.ID,
		DeltaLink:   "https://graph.microsoft.com/v1.0/me/mailFolders/" + f.ID + "/messages/delta?$skiptoken=abc",
		LastDeltaAt: time.Now(),
	}))
	got, err := s.GetDeltaToken(context.Background(), acc, f.ID)
	require.NoError(t, err)
	require.Contains(t, got.DeltaLink, "skiptoken")

	require.NoError(t, s.ClearDeltaToken(context.Background(), acc, f.ID))
	_, err = s.GetDeltaToken(context.Background(), acc, f.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestConcurrentReadsAndWritesNoErrors(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	SeedMessages(context.Background(), t, s, acc, f.ID, 200)

	const writers = 4
	const readers = 4
	const duration = 2 * time.Second
	var wg sync.WaitGroup

	// done is closed once when the deadline elapses; ALL goroutines see
	// the close. (time.After only delivers to one receiver.)
	done := make(chan struct{})
	time.AfterFunc(duration, func() { close(done) })

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(off int) {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-done:
					return
				default:
				}
				m := SyntheticMessage(acc, f.ID, off*1_000_000+i, time.Now())
				if err := s.UpsertMessage(context.Background(), m); err != nil {
					t.Errorf("writer %d: %v", off, err)
					return
				}
				i++
			}
		}(i)
	}
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if _, err := s.ListMessages(context.Background(), MessageQuery{AccountID: acc, FolderID: f.ID, Limit: 50}); err != nil {
					t.Errorf("reader: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// itoa avoids strconv for the test-only helpers.
func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%10]
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
