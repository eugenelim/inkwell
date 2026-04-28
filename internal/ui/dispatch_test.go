package ui

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	ilog "github.com/eugenelim/inkwell/internal/log"
	"github.com/eugenelim/inkwell/internal/render"
	"github.com/eugenelim/inkwell/internal/store"
	isync "github.com/eugenelim/inkwell/internal/sync"
)

// dispatchTestStubBody returns canned content; this file does not have
// the e2e build tag so we re-declare instead of sharing fixtures with
// app_e2e_test.go.
type dispatchTestStubBody struct{}

func (dispatchTestStubBody) FetchBody(_ context.Context, _ string) (render.FetchedBody, error) {
	return render.FetchedBody{ContentType: "text", Content: "hello"}, nil
}

type dispatchTestAuth struct{}

func (dispatchTestAuth) Account() (string, string, bool) { return "tester@example.invalid", "T", true }

type dispatchTestEngine struct{ events chan isync.Event }

func newDispatchTestEngine() *dispatchTestEngine {
	return &dispatchTestEngine{events: make(chan isync.Event, 8)}
}
func (e *dispatchTestEngine) Start(_ context.Context) error     { return nil }
func (e *dispatchTestEngine) SetActive(_ bool)                  {}
func (e *dispatchTestEngine) SyncAll(_ context.Context) error   { return nil }
func (e *dispatchTestEngine) Notifications() <-chan isync.Event { return e.events }

func newDispatchTestModel(t *testing.T) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	id, err := st.PutAccount(context.Background(), store.Account{TenantID: "T", ClientID: "C", UPN: "tester@example.invalid"})
	require.NoError(t, err)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-archive", AccountID: id, DisplayName: "Archive", WellKnownName: "archive", LastSyncedAt: time.Now(),
	}))
	for i, subj := range []string{"Q4 forecast", "Newsletter weekly", "Standup notes"} {
		require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
			ID:          "m-" + string(rune('1'+i)),
			AccountID:   id,
			FolderID:    "f-inbox",
			Subject:     subj,
			FromAddress: "alice@example.invalid",
			FromName:    "Alice",
			ReceivedAt:  time.Now().Add(-time.Duration(i+1) * time.Hour),
		}))
	}
	acc, err := st.GetAccount(context.Background())
	require.NoError(t, err)
	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	m := New(Deps{
		Auth:     dispatchTestAuth{},
		Store:    st,
		Engine:   newDispatchTestEngine(),
		Renderer: render.New(st, dispatchTestStubBody{}),
		Logger:   logger,
		Account:  acc,
	})
	// Establish dimensions so rendering is well-defined.
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)
	// Drive the initial folder load by running the Init Cmd inline.
	cmd := m.Init()
	if cmd != nil {
		// Run only the loadFoldersCmd half of the batch by polling a
		// few message types we recognise; the channel-consumer Cmd
		// blocks forever.
		for i := 0; i < 4; i++ {
			loadCmd := m.loadFoldersCmd()
			msg := loadCmd()
			m2, _ := m.Update(msg)
			m = m2.(Model)
			if len(m.folders.folders) > 0 {
				break
			}
		}
	}
	// Drive the messages load now that the inbox is selected.
	if m.list.FolderID != "" {
		loadMsgs := m.loadMessagesCmd(m.list.FolderID)
		msg := loadMsgs()
		m2, _ = m.Update(msg)
		m = m2.(Model)
	}
	return m
}

// TestDispatchListJMovesCursor confirms that pressing 'j' in the list
// pane increments m.list.cursor. This is the dispatch-layer truth: if
// this fails, no rendering trick can fix the bug.
func TestDispatchListJMovesCursor(t *testing.T) {
	m := newDispatchTestModel(t)
	require.GreaterOrEqual(t, len(m.list.messages), 2, "seed must have ≥2 messages")
	require.Equal(t, ListPane, m.focused, "default focus should be list")
	require.Equal(t, 0, m.list.cursor, "cursor starts at top")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)

	require.Equal(t, 1, m.list.cursor, "j must advance the list cursor")
}

// TestDispatchListKMovesCursorBack confirms 'k' decrements cursor.
func TestDispatchListKMovesCursorBack(t *testing.T) {
	m := newDispatchTestModel(t)
	// Move down twice, then up once.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Equal(t, 2, m.list.cursor)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = m2.(Model)
	require.Equal(t, 1, m.list.cursor)
}

// TestDispatchOneFocusesFolders confirms '1' moves m.focused to FoldersPane.
func TestDispatchOneFocusesFolders(t *testing.T) {
	m := newDispatchTestModel(t)
	require.Equal(t, ListPane, m.focused)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)

	require.Equal(t, FoldersPane, m.focused, "'1' must focus the folders pane")
}

// TestDispatchTwoFocusesList confirms '2' moves focus to ListPane.
func TestDispatchTwoFocusesList(t *testing.T) {
	m := newDispatchTestModel(t)
	// Move to folders first.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	require.Equal(t, FoldersPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = m2.(Model)
	require.Equal(t, ListPane, m.focused)
}

// TestDispatchThreeFocusesViewer confirms '3' moves focus to ViewerPane.
func TestDispatchThreeFocusesViewer(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)
}

// TestDispatchEnterOpensMessageInViewer confirms Enter on a list message
// (a) sets m.viewer.current to the highlighted message and (b) flips
// focus to ViewerPane.
func TestDispatchEnterOpensMessageInViewer(t *testing.T) {
	m := newDispatchTestModel(t)
	require.GreaterOrEqual(t, len(m.list.messages), 1)
	expectedID := m.list.messages[0].ID

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)

	require.Equal(t, ViewerPane, m.focused, "Enter on list must focus viewer")
	require.NotNil(t, m.viewer.current, "viewer must hold the opened message")
	require.Equal(t, expectedID, m.viewer.current.ID)
	require.NotNil(t, cmd, "Enter must return openMessageCmd to fetch body")
}

// TestDispatchEnterOnFolderSwitchesList confirms folder-pane Enter
// updates m.list.FolderID to the highlighted folder.
func TestDispatchEnterOnFolderSwitchesList(t *testing.T) {
	m := newDispatchTestModel(t)
	// Focus folders.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	require.Equal(t, FoldersPane, m.focused)

	// Cursor starts on Inbox (auto-selected on first load). j moves to
	// Archive (rank 3 in sortFoldersForSidebar).
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)

	beforeID := m.list.FolderID
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)

	require.NotEqual(t, beforeID, m.list.FolderID, "Enter on folder must switch list")
	require.Equal(t, "f-archive", m.list.FolderID)
	require.NotNil(t, cmd, "Enter on folder must return loadMessagesCmd")
}

// TestDispatchTabCyclesFocus confirms Tab moves through panes in order.
func TestDispatchTabCyclesFocus(t *testing.T) {
	m := newDispatchTestModel(t)
	require.Equal(t, ListPane, m.focused)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	require.Equal(t, FoldersPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	require.Equal(t, ListPane, m.focused)
}

// TestDispatchFoldersJMovesFolderCursor confirms 'j' in the folders
// pane moves m.folders.cursor (not m.list.cursor).
func TestDispatchFoldersJMovesFolderCursor(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	require.Equal(t, FoldersPane, m.focused)

	beforeFolderCursor := m.folders.cursor
	beforeListCursor := m.list.cursor

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)

	require.Equal(t, beforeFolderCursor+1, m.folders.cursor, "j in folders must move folder cursor")
	require.Equal(t, beforeListCursor, m.list.cursor, "j in folders must NOT move list cursor")
}

// TestDispatchQuitReturnsTeaQuit confirms 'q' returns a tea.Cmd that
// emits tea.QuitMsg. The runtime then exits cleanly. Without this,
// the user has no way out.
func TestDispatchQuitReturnsTeaQuit(t *testing.T) {
	m := newDispatchTestModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	require.NotNil(t, cmd, "q must return a Cmd")
	msg := cmd()
	_, ok := msg.(tea.QuitMsg)
	require.True(t, ok, "q must produce tea.QuitMsg")
}
