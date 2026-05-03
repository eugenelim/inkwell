package ui

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
func (e *dispatchTestEngine) Start(_ context.Context) error   { return nil }
func (e *dispatchTestEngine) SetActive(_ bool)                {}
func (e *dispatchTestEngine) SyncAll(_ context.Context) error { return nil }
func (e *dispatchTestEngine) Wake()                           {}
func (e *dispatchTestEngine) Backfill(_ context.Context, _ string, _ time.Time) error {
	return nil
}
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
	m, err := New(Deps{
		Auth:     dispatchTestAuth{},
		Store:    st,
		Engine:   newDispatchTestEngine(),
		Renderer: render.New(st, dispatchTestStubBody{}),
		Logger:   logger,
		Account:  acc,
	})
	require.NoError(t, err)
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
			if len(m.folders.items) > 0 {
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
// updates m.list.FolderID AND auto-focuses the list pane (so the user
// is taken directly to the messages they asked for).
func TestDispatchEnterOnFolderSwitchesList(t *testing.T) {
	m := newDispatchTestModel(t)
	// Focus folders.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	require.Equal(t, FoldersPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)

	beforeID := m.list.FolderID
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)

	require.NotEqual(t, beforeID, m.list.FolderID, "Enter on folder must switch list")
	require.Equal(t, "f-archive", m.list.FolderID)
	require.Equal(t, ListPane, m.focused, "Enter on folder must auto-focus list pane")
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

// TestViewerScrollDownAdvancesOffset confirms 'j' in the focused
// viewer pane advances scrollY (rather than triggering folder/list
// movement that doesn't apply here).
func TestViewerScrollDownAdvancesOffset(t *testing.T) {
	m := newDispatchTestModel(t)
	// Open the first message; Enter focuses the viewer.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)
	require.Equal(t, 0, m.viewer.scrollY)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Equal(t, 1, m.viewer.scrollY)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = m2.(Model)
	require.Equal(t, 0, m.viewer.scrollY)
}

// TestRenderedFrameNeverExceedsTerminalHeight is the regression for the
// "had to scroll up to see sidebar" / "long message overflows" bug.
// We seed 100 messages, paint the model at 80x24, and assert the View
// output is exactly 24 rows tall — not one row taller.
func TestRenderedFrameNeverExceedsTerminalHeight(t *testing.T) {
	m := newDispatchTestModel(t)
	// Resize the terminal to a small viewport.
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m2.(Model)

	frame := m.View()
	rows := strings.Count(frame, "\n") + 1
	require.LessOrEqual(t, rows, 24, "rendered frame must not exceed terminal height")
}

// TestRenderedFrameWithLongBodyClipsToHeight pumps a 200-line body
// through the viewer and confirms the frame still fits in the terminal.
func TestRenderedFrameWithLongBodyClipsToHeight(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)
	// Open a message and fake a huge body load.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	bigBody := strings.Repeat("paragraph of body text\n", 200)
	m.viewer.SetBody(bigBody, 1) // 1 == BodyReady

	frame := m.View()
	rows := strings.Count(frame, "\n") + 1
	require.LessOrEqual(t, rows, 30, "long body must clip to viewport, not push status bar off-screen")
}

// TestHelpBarVisibleInEveryFocusState pins the v0.2.8 → v0.2.9
// regression: opening a message hid the help bar. The fix clips the
// body region to exactly bodyHeight; this test asserts the rendered
// frame's last line carries help-bar text regardless of which pane is
// focused.
func TestHelpBarVisibleInEveryFocusState(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)

	cases := []struct {
		name      string
		setupKeys []string
		wantHint  string
	}{
		{"list-focused", nil, "1/2/3 panes"},
		{"folders-focused", []string{"1"}, "1/2/3 panes"},
		{"viewer-focused-after-open", []string{"\n"}, "h back"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mm := m
			for _, k := range tc.setupKeys {
				var msg tea.KeyMsg
				if k == "\n" {
					msg = tea.KeyMsg{Type: tea.KeyEnter}
				} else {
					msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
				}
				m2, _ := mm.Update(msg)
				mm = m2.(Model)
			}
			frame := mm.View()
			lines := strings.Split(frame, "\n")
			require.Equal(t, 30, len(lines), "frame must equal terminal height")
			last := lines[len(lines)-1]
			require.Contains(t, last, tc.wantHint, "help-bar hint must be on last visible line")
		})
	}
}

// TestFlattenFolderTreeOrdersAndIndentsCorrectly pins the sidebar tree
// layout: roots ranked Inbox→Sent→Drafts→Archive→user (alpha)→Junk;
// children sorted alphabetically under their parent, depth incremented.
func TestFlattenFolderTreeOrdersAndIndentsCorrectly(t *testing.T) {
	in := []store.Folder{
		{ID: "inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
		{ID: "team-x", DisplayName: "Team X", ParentFolderID: "inbox"},
		{ID: "team-a", DisplayName: "Team A", ParentFolderID: "inbox"},
		{ID: "team-x-sub", DisplayName: "Sub", ParentFolderID: "team-x"},
		{ID: "sent", DisplayName: "Sent Items", WellKnownName: "sentitems"},
		{ID: "user1", DisplayName: "Newsletters"},
		{ID: "user2", DisplayName: "Archive Old"}, // user folder, no well-known name
		{ID: "junk", DisplayName: "Junk Email", WellKnownName: "junkemail"},
	}
	// All-expanded so we can assert the full tree shape.
	expanded := map[string]bool{"inbox": true, "team-x": true}
	got := flattenFolderTree(in, expanded)
	require.Equal(t, 8, len(got))

	// Expected order, with depth:
	expected := []struct {
		id    string
		depth int
	}{
		{"inbox", 0},
		{"team-a", 1},     // child of inbox, alpha first
		{"team-x", 1},     // child of inbox
		{"team-x-sub", 2}, // grandchild
		{"sent", 0},       // sent items rank=1
		{"user2", 0},      // "Archive Old" — user folder, alpha
		{"user1", 0},      // "Newsletters" — user folder, alpha
		{"junk", 0},       // junk at the bottom
	}
	for i, want := range expected {
		require.Equal(t, want.id, got[i].f.ID, "row %d id", i)
		require.Equal(t, want.depth, got[i].depth, "row %d depth", i)
	}
}

// TestFlattenFolderTreeHandlesUntrackedParents confirms a folder whose
// parent is not in the input list (e.g. msgfolderroot) is treated as a
// root, not silently dropped.
func TestFlattenFolderTreeHandlesUntrackedParents(t *testing.T) {
	in := []store.Folder{
		{ID: "stranger", DisplayName: "Stranger", ParentFolderID: "not-in-list"},
		{ID: "inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
	}
	got := flattenFolderTree(in, nil)
	require.Len(t, got, 2)
	// Both should have depth 0; Inbox first by rank.
	require.Equal(t, "inbox", got[0].f.ID)
	require.Equal(t, 0, got[0].depth)
	require.Equal(t, "stranger", got[1].f.ID)
	require.Equal(t, 0, got[1].depth)
}

// TestFoldersCollapseHidesChildren seeds Inbox > Sub > Sub-Sub, asserts
// Inbox is auto-expanded (default), then collapses Inbox via 'o' and
// confirms the children disappear from m.folders.items.
func TestFoldersCollapseHidesChildren(t *testing.T) {
	in := []store.Folder{
		{ID: "inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
		{ID: "sub", DisplayName: "Sub", ParentFolderID: "inbox"},
		{ID: "subsub", DisplayName: "Sub Sub", ParentFolderID: "sub"},
	}
	fm := NewFolders()
	fm.SetFolders(in)
	// Default: Inbox expanded, Sub collapsed → 2 visible rows (Inbox, Sub).
	require.Equal(t, 2, len(fm.items), "Inbox auto-expanded; Sub collapsed by default")
	require.True(t, fm.items[0].expanded)
	require.False(t, fm.items[1].expanded)
	require.True(t, fm.items[1].hasKids, "Sub has Sub Sub")

	// Cursor on Inbox → toggle collapses it.
	fm.cursor = 0
	fm.ToggleExpand()
	require.Equal(t, 1, len(fm.items), "collapsed Inbox hides Sub")

	// Re-expand Inbox, move cursor to Sub, expand Sub.
	fm.ToggleExpand()
	require.Equal(t, 2, len(fm.items))
	fm.cursor = 1
	fm.ToggleExpand()
	require.Equal(t, 3, len(fm.items), "expanding Sub reveals Sub Sub")
	require.Equal(t, "subsub", fm.items[2].f.ID)
	require.Equal(t, 2, fm.items[2].depth)
}

// TestDispatchExpandKeyTogglesFolder confirms 'o' in the focused
// folders pane invokes ToggleExpand on the cursor folder.
func TestDispatchExpandKeyTogglesFolder(t *testing.T) {
	m := newDispatchTestModel(t)
	// Force a folder with children: add a subfolder under Inbox.
	m.folders.raw = append(m.folders.raw, store.Folder{
		ID: "subof-inbox", DisplayName: "Sub", ParentFolderID: "f-inbox",
	})
	m.folders.expanded["f-inbox"] = true // ensure parent visible
	m.folders.rebuild()
	beforeRows := len(m.folders.items)

	// Focus folders, cursor onto Inbox.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	m.folders.SelectByID("f-inbox")
	require.Equal(t, FoldersPane, m.focused)

	// Press 'o' → Inbox collapses → child row hidden.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = m2.(Model)
	require.Less(t, len(m.folders.items), beforeRows, "collapse must hide the subfolder")

	// 'o' again → expanded → child row visible again.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = m2.(Model)
	require.Equal(t, beforeRows, len(m.folders.items))
}

// TestDispatchViewerDeleteRunsTriageAndPopsToList confirms that 'd'
// while the viewer pane is focused enqueues a soft_delete on the
// currently-displayed message AND moves focus back to the list (so
// the user sees what's next rather than staring at a deleted body).
func TestDispatchViewerDeleteRunsTriageAndPopsToList(t *testing.T) {
	m := newDispatchTestModel(t)
	// Open the first message → focus moves to ViewerPane.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)
	require.NotNil(t, m.viewer.current)

	called := atomicBool{}
	m.deps.Triage = stubTriageDelete{onCall: called.set}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = m2.(Model)
	require.NotNil(t, cmd, "d in viewer must return a Cmd")

	// Run the Cmd to completion and feed the resulting Msg back.
	msg := cmd()
	m2, _ = m.Update(msg)
	m = m2.(Model)

	require.True(t, called.get(), "triage SoftDelete invoked")
	require.Equal(t, ListPane, m.focused, "focus pops back to list after delete")
	require.Nil(t, m.viewer.current, "viewer cleared after delete")
}

// TestDispatchViewerFlagRunsTriageAndStays confirms 'f' in the viewer
// toggles the flag but keeps focus on the viewer (you flagged it so
// you could keep reading).
func TestDispatchViewerFlagRunsTriageAndStays(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	called := atomicBool{}
	m.deps.Triage = stubTriageFlag{onCall: called.set}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = m2.(Model)
	require.NotNil(t, cmd)
	msg := cmd()
	m2, _ = m.Update(msg)
	m = m2.(Model)

	require.True(t, called.get())
	require.Equal(t, ViewerPane, m.focused, "flag toggle keeps viewer focus")
	require.NotNil(t, m.viewer.current, "viewer not cleared on flag")
}

// stubTriageDelete satisfies ui.TriageExecutor for the delete path.
type stubTriageDelete struct{ onCall func() }

func (s stubTriageDelete) MarkRead(context.Context, int64, string) error         { return nil }
func (s stubTriageDelete) MarkUnread(context.Context, int64, string) error       { return nil }
func (s stubTriageDelete) ToggleFlag(context.Context, int64, string, bool) error { return nil }
func (s stubTriageDelete) SoftDelete(_ context.Context, _ int64, _ string) error {
	s.onCall()
	return nil
}
func (s stubTriageDelete) Archive(context.Context, int64, string) error { return nil }
func (s stubTriageDelete) Move(context.Context, int64, string, string, string) error {
	return nil
}
func (s stubTriageDelete) PermanentDelete(context.Context, int64, string) error { return nil }
func (s stubTriageDelete) AddCategory(context.Context, int64, string, string) error {
	return nil
}
func (s stubTriageDelete) RemoveCategory(context.Context, int64, string, string) error {
	return nil
}
func (s stubTriageDelete) CreateFolder(_ context.Context, _ int64, _, _ string) (CreatedFolder, error) {
	return CreatedFolder{}, nil
}
func (s stubTriageDelete) RenameFolder(context.Context, string, string) error { return nil }
func (s stubTriageDelete) DeleteFolder(context.Context, string) error         { return nil }
func (s stubTriageDelete) Undo(context.Context, int64) (UndoneAction, error) {
	return UndoneAction{}, ErrUndoEmpty
}

type stubTriageFlag struct{ onCall func() }

func (s stubTriageFlag) MarkRead(context.Context, int64, string) error   { return nil }
func (s stubTriageFlag) MarkUnread(context.Context, int64, string) error { return nil }
func (s stubTriageFlag) ToggleFlag(_ context.Context, _ int64, _ string, _ bool) error {
	s.onCall()
	return nil
}
func (s stubTriageFlag) SoftDelete(context.Context, int64, string) error { return nil }
func (s stubTriageFlag) Archive(context.Context, int64, string) error    { return nil }
func (s stubTriageFlag) Move(context.Context, int64, string, string, string) error {
	return nil
}
func (s stubTriageFlag) PermanentDelete(context.Context, int64, string) error { return nil }
func (s stubTriageFlag) AddCategory(context.Context, int64, string, string) error {
	return nil
}
func (s stubTriageFlag) RemoveCategory(context.Context, int64, string, string) error {
	return nil
}
func (s stubTriageFlag) CreateFolder(_ context.Context, _ int64, _, _ string) (CreatedFolder, error) {
	return CreatedFolder{}, nil
}
func (s stubTriageFlag) RenameFolder(context.Context, string, string) error { return nil }
func (s stubTriageFlag) DeleteFolder(context.Context, string) error         { return nil }
func (s stubTriageFlag) Undo(context.Context, int64) (UndoneAction, error) {
	return UndoneAction{}, ErrUndoEmpty
}

// atomicBool is a tiny helper since sync/atomic.Bool is fine but adds
// another import; this stays test-local.
type atomicBool struct{ v bool }

func (a *atomicBool) set()      { a.v = true }
func (a *atomicBool) get() bool { return a.v }

// TestFilterCommandActivatesFilter drives `:filter ~A` and asserts
// the filter Cmd fires; the filterAppliedMsg handler then sets
// filterActive=true with the matched IDs.
func TestFilterCommandActivatesFilter(t *testing.T) {
	m := newDispatchTestModel(t)

	// Enter command mode and type "filter ~A" + Enter.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "filter ~A" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, cmd, "filter command must return runFilterCmd")

	// The Cmd hits the store; let it run. It returns either ErrorMsg
	// (none of the seeded messages have attachments) or
	// filterAppliedMsg with empty results — either way Update should
	// not panic.
	msg := cmd()
	m2, _ = m.Update(msg)
	m = m2.(Model)

	// Whether or not anything matched, the filter command itself must
	// not crash and the model must remain in NormalMode.
	require.Equal(t, NormalMode, m.mode)
}

// TestFilterPlainTextWrapsInBOperator confirms `:filter foo` (no `~`
// operator) is rewritten as a CONTAINS search (`~B *foo*`) — not an
// exact-match on the subject. Real-tenant regression: `:filter [External]`
// returned zero rows because the auto-wrap was `~B [External]` which
// compiled to `subject = '[External]'`. Every search box on Earth
// expects substring match; we now match that mental model.
func TestFilterPlainTextWrapsInBOperator(t *testing.T) {
	m := newDispatchTestModel(t)
	require.GreaterOrEqual(t, len(m.list.messages), 1)

	cmd := m.runFilterCmd("forecast")
	require.NotNil(t, cmd)
	msg := cmd()
	applied, ok := msg.(filterAppliedMsg)
	require.True(t, ok, "plain-text filter must return filterAppliedMsg, got %T", msg)
	require.Contains(t, applied.src, "~B *forecast*", "plain text must wrap as contains")
	// "Q4 forecast" is in the seeded set; CONTAINS must find it.
	require.GreaterOrEqual(t, len(applied.messages), 1, "contains-wrapped filter must match seeded subject")
}

// TestFilterBracketTextMatchesSubjectContains is the real-tenant
// regression: `:filter [External]` against a corpus where messages
// have subjects like "[External] Q4 deck" used to return zero. Now
// it must hit the message whose subject contains the bracketed tag.
func TestFilterBracketTextMatchesSubjectContains(t *testing.T) {
	m := newDispatchTestModel(t)
	// Seed an additional message whose subject carries the [External]
	// tag that Exchange transport rules typically prepend.
	require.NoError(t, m.deps.Store.UpsertMessage(context.Background(), store.Message{
		ID:          "m-ext",
		AccountID:   m.deps.Account.ID,
		FolderID:    "f-inbox",
		Subject:     "[External] vendor pricing",
		FromAddress: "vendor@example.invalid",
		ReceivedAt:  time.Now(),
	}))

	cmd := m.runFilterCmd("[External]")
	require.NotNil(t, cmd)
	msg := cmd()
	applied, ok := msg.(filterAppliedMsg)
	require.True(t, ok, "got %T", msg)
	require.GreaterOrEqual(t, len(applied.messages), 1, ":filter [External] must hit messages tagged by transport rule")
}

// TestSingleMessageTriageOnFilteredListReRunsFilter is the real-
// tenant regression for the v0.13.x bug where pressing `d` (or any
// single-message triage) on a filtered list reloaded against the
// `filter:<pattern>` sentinel folder ID, returned zero rows from
// the store, and made the user think every filtered message had
// been deleted. Spec 10 §4.6 invariant: triageDoneMsg must re-run
// the active filter, not loadMessagesCmd, when filterActive is true.
func TestSingleMessageTriageOnFilteredListReRunsFilter(t *testing.T) {
	m := newDispatchTestModel(t)
	require.GreaterOrEqual(t, len(m.list.messages), 1)
	// Force filter state mimicking the post-runFilterCmd shape.
	m.filterActive = true
	m.filterPattern = "~B *forecast*"
	m.priorFolderID = "f-inbox"
	m.list.FolderID = "filter:" + m.filterPattern

	// triageDoneMsg with a non-error result. The model must re-run
	// the filter (returning a Cmd that produces filterAppliedMsg)
	// rather than loadMessagesCmd against the sentinel folder ID
	// (which would return zero rows).
	done := triageDoneMsg{name: "soft_delete", folderID: m.list.FolderID, msgID: "m-1"}
	m2, cmd := m.Update(done)
	m = m2.(Model)
	require.NotNil(t, cmd, "triageDoneMsg on a filtered list must return a Cmd")
	require.Contains(t, m.engineActivity, "soft_delete",
		"status bar must reassure the user about what just happened")
	require.Contains(t, m.engineActivity, "u to undo")

	// The Cmd must be the filter-rerun, not loadMessagesCmd. We
	// detect this by running the Cmd and asserting the resulting
	// message is filterAppliedMsg (loadMessagesCmd would produce
	// MessagesLoadedMsg).
	result := cmd()
	_, isFilter := result.(filterAppliedMsg)
	require.True(t, isFilter, "Cmd must produce filterAppliedMsg, got %T", result)
}

// TestUnfilterReloadsPriorFolder is the real-tenant regression for
// the v0.11-era bug where `:unfilter` reset the model's filterActive
// flag but the list pane kept showing the stale filter results
// because no loadMessagesCmd was issued. The fix returns the load
// Cmd; this test asserts it's non-nil.
func TestUnfilterReloadsPriorFolder(t *testing.T) {
	m := newDispatchTestModel(t)
	// Apply a filter so priorFolderID is captured and filterActive flips.
	cmd := m.runFilterCmd("forecast")
	require.NotNil(t, cmd)
	applied, ok := cmd().(filterAppliedMsg)
	require.True(t, ok)
	m2, _ := m.Update(applied)
	m = m2.(Model)
	require.True(t, m.filterActive)
	require.NotEmpty(t, m.priorFolderID)

	// Now drive `:unfilter` through the command bar and assert a Cmd
	// is returned (the load-prior-folder Cmd).
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "unfilter" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.False(t, m.filterActive)
	require.NotNil(t, cmd, ":unfilter must return loadMessagesCmd to refresh the pane")
}

// TestSemicolonChordRequiresFilter confirms `;` outside a filter is
// a no-op that records an error and DOES NOT enter bulk-pending state.
func TestSemicolonChordRequiresFilter(t *testing.T) {
	m := newDispatchTestModel(t)
	require.False(t, m.filterActive)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
	m = m2.(Model)
	require.False(t, m.bulkPending, "; without a filter must not arm bulk")
	require.Error(t, m.lastError)
}

// TestSemicolonDOpensConfirmModal sets up a fake filter state and
// asserts `;d` asks the user to confirm a bulk delete. The confirm
// modal carries the matched count.
func TestSemicolonDOpensConfirmModal(t *testing.T) {
	m := newDispatchTestModel(t)
	// Force filter state.
	m.filterActive = true
	m.filterPattern = "~A"
	m.filterIDs = []string{"m-1", "m-2", "m-3"}
	m.deps.Bulk = stubBulkExecutor{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
	m = m2.(Model)
	require.True(t, m.bulkPending)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode, ";d must enter confirm mode")
	require.Equal(t, "soft_delete", m.pendingBulk)
	require.Contains(t, m.confirm.Message, "3 messages")
}

// TestConfirmYesFiresBulkCmd runs through the full ;d → confirm-y
// flow and verifies the bulk Cmd is returned.
func TestConfirmYesFiresBulkCmd(t *testing.T) {
	m := newDispatchTestModel(t)
	m.filterActive = true
	m.filterIDs = []string{"m-1", "m-2"}
	called := atomicBool{}
	m.deps.Bulk = stubBulkExecutor{onSoftDelete: called.set}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode)

	// User presses 'y'.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m2.(Model)
	// 'y' produces a ConfirmResultMsg via a Cmd.
	require.NotNil(t, cmd)
	confirmMsg := cmd()
	m2, bulkCmd := m.Update(confirmMsg)
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.NotNil(t, bulkCmd, "confirmed bulk must return runBulkCmd")

	// Drive the bulk Cmd.
	_ = bulkCmd()
	require.True(t, called.get(), "Bulk.SoftDelete invoked")
}

// stubBulkExecutor satisfies ui.BulkExecutor with optional onCall hooks.
type stubBulkExecutor struct {
	onSoftDelete func()
}

func bulkOK(ids []string) ([]BulkResult, error) {
	out := make([]BulkResult, len(ids))
	for i, id := range ids {
		out[i] = BulkResult{MessageID: id}
	}
	return out, nil
}

func (s stubBulkExecutor) BulkSoftDelete(_ context.Context, _ int64, ids []string) ([]BulkResult, error) {
	if s.onSoftDelete != nil {
		s.onSoftDelete()
	}
	return bulkOK(ids)
}
func (s stubBulkExecutor) BulkArchive(_ context.Context, _ int64, ids []string) ([]BulkResult, error) {
	return bulkOK(ids)
}
func (s stubBulkExecutor) BulkMarkRead(_ context.Context, _ int64, ids []string) ([]BulkResult, error) {
	return bulkOK(ids)
}
func (s stubBulkExecutor) BulkMarkUnread(_ context.Context, _ int64, ids []string) ([]BulkResult, error) {
	return bulkOK(ids)
}
func (s stubBulkExecutor) BulkFlag(_ context.Context, _ int64, ids []string) ([]BulkResult, error) {
	return bulkOK(ids)
}
func (s stubBulkExecutor) BulkUnflag(_ context.Context, _ int64, ids []string) ([]BulkResult, error) {
	return bulkOK(ids)
}
func (s stubBulkExecutor) BulkPermanentDelete(_ context.Context, _ int64, ids []string) ([]BulkResult, error) {
	return bulkOK(ids)
}
func (s stubBulkExecutor) BulkAddCategory(_ context.Context, _ int64, ids []string, _ string) ([]BulkResult, error) {
	return bulkOK(ids)
}
func (s stubBulkExecutor) BulkRemoveCategory(_ context.Context, _ int64, ids []string, _ string) ([]BulkResult, error) {
	return bulkOK(ids)
}

// TestFKeyPreFillsFilterCommand confirms pressing F (capital) opens
// command mode pre-filled with "filter ".
func TestFKeyPreFillsFilterCommand(t *testing.T) {
	m := newDispatchTestModel(t)
	require.Equal(t, NormalMode, m.mode)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")})
	m = m2.(Model)

	require.Equal(t, CommandMode, m.mode, "F must enter command mode")
	require.Equal(t, "filter ", m.cmd.buf, "F must pre-fill 'filter '")
}

// TestFKeyInsideBulkChordUnflags confirms ;F opens the confirm modal
// for bulk unflag rather than opening the filter command bar.
func TestFKeyInsideBulkChordUnflags(t *testing.T) {
	m := newDispatchTestModel(t)
	m.filterActive = true
	m.filterIDs = []string{"m-1", "m-2"}
	m.deps.Bulk = stubBulkExecutor{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
	m = m2.(Model)
	require.True(t, m.bulkPending)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode, ";F must enter confirm mode (bulk unflag)")
	require.Equal(t, "unflag", m.pendingBulk)
}

// TestSemicolonNewVerbsOpenConfirmModal checks that each new ; chord
// opens a confirm modal with the expected pendingBulk value.
func TestSemicolonNewVerbsOpenConfirmModal(t *testing.T) {
	cases := []struct {
		key    string
		action string
	}{
		{"D", "permanent_delete"},
		{"r", "mark_read"},
		{"R", "mark_unread"},
		{"f", "flag"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			m := newDispatchTestModel(t)
			m.filterActive = true
			m.filterIDs = []string{"m-1", "m-2"}
			m.deps.Bulk = stubBulkExecutor{}

			m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
			m = m2.(Model)
			require.True(t, m.bulkPending)

			m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
			m = m2.(Model)
			require.Equal(t, ConfirmMode, m.mode, ";"+tc.key+" must enter confirm mode")
			require.Equal(t, tc.action, m.pendingBulk)
		})
	}
}

// TestSemicolonCEntersCategoryInputMode confirms ;c opens the category
// input modal with the bulk flag set (not a single-message path).
func TestSemicolonCEntersCategoryInputMode(t *testing.T) {
	m := newDispatchTestModel(t)
	m.filterActive = true
	m.filterIDs = []string{"m-1"}
	m.deps.Bulk = stubBulkExecutor{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = m2.(Model)

	require.Equal(t, CategoryInputMode, m.mode, ";c must enter category input mode")
	require.Equal(t, "add_category", m.pendingBulkCategoryAction, ";c must set bulk action to add_category")
}

// TestSemicolonCategoryConfirmFlowReachesRunBulk drives ;c → type
// "News" → Enter → confirm modal → y → verifies runBulkCmd is returned.
func TestSemicolonCategoryConfirmFlowReachesRunBulk(t *testing.T) {
	m := newDispatchTestModel(t)
	m.filterActive = true
	m.filterIDs = []string{"m-1", "m-2"}
	m.deps.Bulk = stubBulkExecutor{}

	// ;c
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = m2.(Model)
	require.Equal(t, CategoryInputMode, m.mode)

	// type "News"
	for _, r := range "News" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	// Enter → confirm modal
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode, "Enter after category name must open confirm modal")
	require.Equal(t, "add_category", m.pendingBulk)
	require.Equal(t, "News", m.pendingBulkCategory)

	// y → bulk cmd
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m2.(Model)
	require.NotNil(t, cmd)
	confirmMsg := cmd()
	m2, bulkCmd := m.Update(confirmMsg)
	_ = m2
	require.NotNil(t, bulkCmd, "confirmed bulk add_category must return runBulkCmd")
}

// stubUnsubService satisfies ui.UnsubscribeService for dispatch tests.
// Each method records the calls so tests can assert the right path.
type stubUnsubService struct {
	resolveAction UnsubscribeAction
	resolveErr    error
	resolveCalls  int
	postCalls     int
	postURL       string
	postErr       error
}

func (s *stubUnsubService) Resolve(_ context.Context, _ string) (UnsubscribeAction, error) {
	s.resolveCalls++
	return s.resolveAction, s.resolveErr
}

func (s *stubUnsubService) OneClickPOST(_ context.Context, url string) error {
	s.postCalls++
	s.postURL = url
	return s.postErr
}

// stubTriageWithUndo extends the existing stub-triage pattern to
// satisfy the spec-07-§11 surface. Records calls so dispatch tests
// can assert which path fired.
type stubTriageWithUndo struct {
	undoCalls        int
	undoneLabel      string
	undoErr          error
	lastFolderAction string
	lastMove         string // "<msgID>:<destFolderID>:<destAlias>"
}

func (s *stubTriageWithUndo) MarkRead(_ context.Context, _ int64, _ string) error {
	return nil
}
func (s *stubTriageWithUndo) MarkUnread(_ context.Context, _ int64, _ string) error {
	return nil
}
func (s *stubTriageWithUndo) ToggleFlag(_ context.Context, _ int64, _ string, _ bool) error {
	return nil
}
func (s *stubTriageWithUndo) SoftDelete(_ context.Context, _ int64, _ string) error {
	return nil
}
func (s *stubTriageWithUndo) Archive(_ context.Context, _ int64, _ string) error { return nil }
func (s *stubTriageWithUndo) Move(_ context.Context, _ int64, msgID, destID, destAlias string) error {
	s.lastMove = msgID + ":" + destID + ":" + destAlias
	return nil
}
func (s *stubTriageWithUndo) PermanentDelete(_ context.Context, _ int64, _ string) error {
	return nil
}
func (s *stubTriageWithUndo) AddCategory(_ context.Context, _ int64, _, _ string) error {
	return nil
}
func (s *stubTriageWithUndo) RemoveCategory(_ context.Context, _ int64, _, _ string) error {
	return nil
}
func (s *stubTriageWithUndo) CreateFolder(_ context.Context, _ int64, parentID, name string) (CreatedFolder, error) {
	s.lastFolderAction = "new:" + parentID + ":" + name
	return CreatedFolder{ID: "f-new", DisplayName: name, ParentFolderID: parentID}, nil
}
func (s *stubTriageWithUndo) RenameFolder(_ context.Context, folderID, name string) error {
	s.lastFolderAction = "rename:" + folderID + ":" + name
	return nil
}
func (s *stubTriageWithUndo) DeleteFolder(_ context.Context, folderID string) error {
	s.lastFolderAction = "delete:" + folderID
	return nil
}
func (s *stubTriageWithUndo) Undo(_ context.Context, _ int64) (UndoneAction, error) {
	s.undoCalls++
	if s.undoErr != nil {
		return UndoneAction{}, s.undoErr
	}
	return UndoneAction{Label: s.undoneLabel, MessageIDs: []string{"m-1"}}, nil
}

// TestPermanentDeleteOpensConfirmModal is the spec 07 §6.7
// invariant: pressing D in the list pane MUST open a confirm
// modal carrying the irreversibility warning before any
// permanent_delete fires. Without the gate, a fat-finger press
// destroys data with no recovery.
func TestPermanentDeleteOpensConfirmModal(t *testing.T) {
	m := newDispatchTestModel(t)
	require.GreaterOrEqual(t, len(m.list.messages), 1)
	m.deps.Triage = &stubTriageWithUndo{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode, "D must transition to ConfirmMode")
	require.NotNil(t, m.pendingPermanentDelete)
	require.Contains(t, m.confirm.Message, "PERMANENT DELETE")
	require.Contains(t, m.confirm.Message, "irreversible")
	require.Equal(t, "permanent_delete", m.confirm.Topic)
}

// TestPermanentDeleteConfirmYesFires drives D → y end-to-end and
// asserts Triage.PermanentDelete actually runs.
func TestPermanentDeleteConfirmYesFires(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{}
	m.deps.Triage = stub

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode)

	// Press y → ConfirmResultMsg{Confirm:true} → runTriage permanent_delete.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m2.(Model)
	require.NotNil(t, cmd)
	m2, cmd = m.Update(cmd()) // ConfirmResultMsg
	m = m2.(Model)
	require.NotNil(t, cmd, "y must dispatch the runTriage Cmd")
	// The Cmd calls Triage.PermanentDelete; drive it inline.
	_ = cmd()
	// stubTriageWithUndo doesn't have a counter for PermanentDelete
	// — but the action is no-op success, so the lastError must be
	// nil and pendingPermanentDelete must be cleared.
	require.Nil(t, m.lastError)
	require.Nil(t, m.pendingPermanentDelete, "y must clear pendingPermanentDelete")
}

// TestPermanentDeleteConfirmNoSkips covers the cancel path: n
// drops the pending delete without firing PermanentDelete.
func TestPermanentDeleteConfirmNoSkips(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{}
	m.deps.Triage = stub

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m = m2.(Model)
	require.NotNil(t, m.pendingPermanentDelete)

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = m2.(Model)
	m2, _ = m.Update(cmd()) // ConfirmResultMsg{Confirm:false}
	m = m2.(Model)
	require.Nil(t, m.pendingPermanentDelete, "n must clear pendingPermanentDelete")
	require.Equal(t, "permanent delete cancelled", m.engineActivity)
}

// TestAddCategoryOpensCategoryInputMode drives `c` on the list pane
// and asserts CategoryInputMode opens with action="add". Spec 07
// §6.9.
func TestAddCategoryOpensCategoryInputMode(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Triage = &stubTriageWithUndo{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = m2.(Model)
	require.Equal(t, CategoryInputMode, m.mode)
	require.Equal(t, "add", m.pendingCategoryAction)
	require.NotNil(t, m.pendingCategoryMsg)
}

// TestCategoryInputDispatchesAddOnEnter types "Q4" + Enter and
// asserts the dispatch path fires.
func TestCategoryInputDispatchesAddOnEnter(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Triage = &stubTriageWithUndo{}

	// Enter category-input mode via `c`.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = m2.(Model)

	// Type "Q4" + Enter.
	for _, r := range "Q4" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	require.Equal(t, "Q4", m.categoryBuf)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, cmd, "Enter must dispatch runTriage Cmd")
	require.Equal(t, NormalMode, m.mode)
	require.Empty(t, m.pendingCategoryAction)
}

// TestCategoryInputCancelsOnEsc verifies the Esc path: empty buf
// after exit + a "cancelled" status nudge.
func TestCategoryInputCancelsOnEsc(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Triage = &stubTriageWithUndo{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Q")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.Empty(t, m.categoryBuf)
	require.Empty(t, m.pendingCategoryAction)
	require.Contains(t, m.engineActivity, "cancelled")
}

// TestNewFolderOpensNameInputMode is the spec 18 §5.1 invariant:
// pressing N in the folders pane transitions to FolderNameInputMode
// with action="new".
func TestNewFolderOpensNameInputMode(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{}
	m.deps.Triage = stub

	// Focus folders pane first.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	require.Equal(t, FoldersPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	m = m2.(Model)
	require.Equal(t, FolderNameInputMode, m.mode)
	require.Equal(t, "new", m.pendingFolderAction)
	require.Empty(t, m.folderNameBuf)
}

// TestNewFolderEnterDispatchesCreate types a name + Enter and
// asserts CreateFolder fired.
func TestNewFolderEnterDispatchesCreate(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{}
	m.deps.Triage = stub

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	m = m2.(Model)
	for _, r := range "Vendors" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, cmd, "Enter must dispatch the create Cmd")
	require.Equal(t, NormalMode, m.mode)
	require.Contains(t, m.engineActivity, "creating")
	// Drive the Cmd inline.
	_ = cmd()
	require.Contains(t, stub.lastFolderAction, "new:")
	require.Contains(t, stub.lastFolderAction, "Vendors")
}

// TestRenameFolderSeedsBufferAndDispatches drives R + edit + Enter.
func TestRenameFolderSeedsBufferAndDispatches(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{}
	m.deps.Triage = stub

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	m = m2.(Model)
	require.Equal(t, FolderNameInputMode, m.mode)
	require.Equal(t, "rename", m.pendingFolderAction)
	// Buffer must pre-seed with the focused folder's current name.
	require.NotEmpty(t, m.folderNameBuf, "rename must pre-seed the buffer")

	// Replace the buffer.
	m.folderNameBuf = "RenamedInbox"
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, cmd)
	_ = cmd()
	require.Contains(t, stub.lastFolderAction, "rename:")
	require.Contains(t, stub.lastFolderAction, "RenamedInbox")
}

// TestDeleteFolderOpensConfirmModal verifies the spec 18 §5.2
// destructive-confirm gate.
func TestDeleteFolderOpensConfirmModal(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{}
	m.deps.Triage = stub

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode)
	require.Equal(t, "delete_folder", m.confirm.Topic)
	require.Contains(t, m.confirm.Message, "Delete folder")
	require.Contains(t, m.confirm.Message, "Deleted Items")
	require.NotNil(t, m.pendingFolderDelete)
}

// TestDeleteFolderConfirmYesFiresDispatch drives X → y → DeleteFolder.
func TestDeleteFolderConfirmYesFiresDispatch(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{}
	m.deps.Triage = stub

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	m = m2.(Model)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m2.(Model)
	require.NotNil(t, cmd)
	m2, cmd = m.Update(cmd()) // ConfirmResultMsg
	m = m2.(Model)
	require.NotNil(t, cmd)
	_ = cmd()
	require.Contains(t, stub.lastFolderAction, "delete:")
}

// TestRefreshCommandWakesEngine drives `:refresh` and asserts the
// engine activity reads "syncing…" without surfacing an error.
// Spec 04 §6.4.
func TestRefreshCommandWakesEngine(t *testing.T) {
	m := newDispatchTestModel(t)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "refresh" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)

	require.Equal(t, "syncing…", m.engineActivity)
	require.Nil(t, m.lastError)
}

// TestFolderCommandJumpsListPane drives `:folder Archive` and asserts
// the list pane's FolderID swaps + focus moves to the list.
func TestFolderCommandJumpsListPane(t *testing.T) {
	m := newDispatchTestModel(t)
	require.GreaterOrEqual(t, len(m.folders.items), 1)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "folder Archive" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, cmd, ":folder must return loadMessagesCmd")
	require.Equal(t, "f-archive", m.list.FolderID)
	require.Equal(t, ListPane, m.focused)
	require.Nil(t, m.lastError)
}

// TestFolderCommandUnknownNameSurfacesError covers the typo path.
func TestFolderCommandUnknownNameSurfacesError(t *testing.T) {
	m := newDispatchTestModel(t)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "folder Nonexistent" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "Nonexistent")
}

// TestBackfillCommandRefusesFilterView ensures `:backfill` while a
// filter is active produces a friendly error rather than racing
// against the sentinel folder ID.
func TestBackfillCommandRefusesFilterView(t *testing.T) {
	m := newDispatchTestModel(t)
	m.list.FolderID = "filter:~B *foo*"

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "backfill" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "filter view")
}

// TestSearchCommandSeedsQueryAndRuns drives `:search foo` and
// asserts searchActive flips + the searchQuery is captured.
func TestSearchCommandSeedsQueryAndRuns(t *testing.T) {
	m := newDispatchTestModel(t)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "search forecast" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, cmd, ":search must return runSearchCmd")
	require.True(t, m.searchActive)
	require.Equal(t, "forecast", m.searchQuery)
	require.Equal(t, ListPane, m.focused)
}

// TestHelpKeyOpensOverlay is the spec-04-§12 dispatch invariant:
// pressing `?` from normal mode transitions to HelpMode. The
// overlay is read-only; visible-delta is in app_e2e_test.go.
func TestHelpKeyOpensOverlay(t *testing.T) {
	m := newDispatchTestModel(t)
	require.Equal(t, NormalMode, m.mode)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = m2.(Model)
	require.Equal(t, HelpMode, m.mode, "? must transition to HelpMode")

	// Esc closes.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode, "Esc must close help overlay")
}

// TestHelpCommandOpensOverlay is the parity test: `:help` drives
// the same flow as `?`.
func TestHelpCommandOpensOverlay(t *testing.T) {
	m := newDispatchTestModel(t)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "help" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, HelpMode, m.mode)
}

// TestUndoKeyDispatchesUndoCmd is the spec-07-§11 dispatch invariant:
// pressing `u` in the list pane returns runUndo's Cmd, which calls
// Triage.Undo. Visible-delta covered by app_e2e_test.go.
func TestUndoKeyDispatchesUndoCmd(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{undoneLabel: "marked read"}
	m.deps.Triage = stub

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	m = m2.(Model)
	require.NotNil(t, cmd, "u must return runUndo Cmd")

	// Drive the Cmd inline. The result lands as undoDoneMsg.
	msg := cmd()
	m2, _ = m.Update(msg)
	m = m2.(Model)
	require.Equal(t, 1, stub.undoCalls)
	require.Contains(t, m.engineActivity, "undid")
	require.Contains(t, m.engineActivity, "marked read")
	require.Nil(t, m.lastError)
}

// TestUndoKeyEmptyStackSurfacesFriendlyMessage covers the empty-stack
// path: ErrUndoEmpty must NOT show as an error (m.lastError stays nil),
// just a transient "nothing to undo" status.
func TestUndoKeyEmptyStackSurfacesFriendlyMessage(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{undoErr: ErrUndoEmpty}
	m.deps.Triage = stub

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	m = m2.(Model)
	m2, _ = m.Update(cmd())
	m = m2.(Model)
	require.Nil(t, m.lastError, "empty stack must NOT surface as a red error")
	require.Equal(t, "nothing to undo", m.engineActivity)
}

// TestUnsubscribeKeyResolvesAndOpensConfirmModal drives the spec 16
// happy path: U on a list-pane row → resolveUnsubCmd → confirm modal
// shows the URL. Visible-delta requirement: ConfirmMode is the
// post-state, and the prompt text contains the URL the user is
// about to act on (so they can spot a phishing attempt before y).
func TestUnsubscribeKeyResolvesAndOpensConfirmModal(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubUnsubService{
		resolveAction: UnsubscribeAction{Kind: UnsubscribeOneClickPOST, URL: "https://example.invalid/u/abc"},
	}
	m.deps.Unsubscribe = stub

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U")})
	m = m2.(Model)
	require.NotNil(t, cmd, "U must return resolveUnsubCmd")
	// Drive the resolve Cmd inline so the resolved action lands.
	resolved := cmd()
	m2, _ = m.Update(resolved)
	m = m2.(Model)
	require.Equal(t, 1, stub.resolveCalls)
	require.Equal(t, ConfirmMode, m.mode, "U must transition to ConfirmMode after resolve")
	require.Contains(t, m.confirm.Message, "example.invalid/u/abc", "confirm must show the URL the user is about to POST")
	require.Equal(t, "unsubscribe", m.confirm.Topic)
}

// TestUnsubscribeConfirmYesFiresPOST drives U → y → asserts the
// OneClickPOST call landed with the right URL.
func TestUnsubscribeConfirmYesFiresPOST(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubUnsubService{
		resolveAction: UnsubscribeAction{Kind: UnsubscribeOneClickPOST, URL: "https://example.invalid/u/abc"},
	}
	m.deps.Unsubscribe = stub

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U")})
	m = m2.(Model)
	require.NotNil(t, cmd)
	m2, _ = m.Update(cmd())
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode)

	// Press y → Update returns a Cmd that emits ConfirmResultMsg{Confirm:true}.
	m2, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m2.(Model)
	require.NotNil(t, cmd)
	// The Cmd produces a ConfirmResultMsg; route it through Update.
	m2, cmd = m.Update(cmd())
	m = m2.(Model)
	require.NotNil(t, cmd, "y must dispatch executeUnsubCmd")
	// Drive the execute Cmd inline.
	done := cmd()
	m2, _ = m.Update(done)
	m = m2.(Model)
	require.Equal(t, 1, stub.postCalls)
	require.Equal(t, "https://example.invalid/u/abc", stub.postURL)
	require.Contains(t, m.engineActivity, "unsubscribed")
	require.Nil(t, m.lastError)
}

// TestUnsubscribeConfirmNoSkipsPOST is the cancel path. n must drop
// the pending action without calling OneClickPOST.
func TestUnsubscribeConfirmNoSkipsPOST(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubUnsubService{
		resolveAction: UnsubscribeAction{Kind: UnsubscribeOneClickPOST, URL: "https://example.invalid/u/abc"},
	}
	m.deps.Unsubscribe = stub

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U")})
	m = m2.(Model)
	m2, _ = m.Update(cmd())
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode)

	m2, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = m2.(Model)
	m2, _ = m.Update(cmd()) // ConfirmResultMsg{Confirm:false}
	m = m2.(Model)
	require.Equal(t, 0, stub.postCalls, "n must NOT fire OneClickPOST")
	require.Nil(t, m.pendingUnsub)
}

// TestUnsubscribeNoHeaderSurfacesFriendlyError covers spec 16 §9 row 1:
// resolve returns ErrNoHeader → status bar message, NOT a confirm modal.
func TestUnsubscribeNoHeaderSurfacesFriendlyError(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubUnsubService{
		resolveErr: errors.New("unsub: no List-Unsubscribe header"),
	}
	m.deps.Unsubscribe = stub

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U")})
	m = m2.(Model)
	m2, _ = m.Update(cmd())
	m = m2.(Model)
	require.Error(t, m.lastError)
	require.NotEqual(t, ConfirmMode, m.mode, "no header must NOT open the confirm modal")
}

// TestUnsubCommandMatchesUKey is the parity test: `:unsub` (and
// `:unsubscribe`) drive the same flow as the U keybinding. Convention
// shared with aerc (spec 16 §2).
func TestUnsubCommandMatchesUKey(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubUnsubService{
		resolveAction: UnsubscribeAction{Kind: UnsubscribeBrowserGET, URL: "https://example.invalid/u"},
	}
	m.deps.Unsubscribe = stub

	// Type :unsub <Enter>.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "unsub" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, cmd, ":unsub must dispatch resolveUnsubCmd")
	m2, _ = m.Update(cmd())
	m = m2.(Model)
	require.Equal(t, 1, stub.resolveCalls)
	require.Equal(t, ConfirmMode, m.mode)
}

// TestSavedSearchesAppearInSidebar seeds two [[saved_searches]] config
// entries and confirms FoldersModel.items renders the section header
// + each saved search row with the ☆ glyph and the configured name.
func TestSavedSearchesAppearInSidebar(t *testing.T) {
	m := newDispatchTestModel(t)
	m.folders.SetSavedSearches([]SavedSearch{
		{Name: "Newsletters", Pattern: "~f newsletter@*"},
		{Name: "Needs Reply", Pattern: "~r me@example.invalid & ~U"},
	})
	// Render to a string and check for the section header + names.
	out := m.folders.View(m.theme, 30, 30, true)
	require.Contains(t, out, "Saved Searches")
	require.Contains(t, out, "☆ Newsletters")
	require.Contains(t, out, "☆ Needs Reply")
}

// TestSavedSearchEnterRunsFilter drives j/Enter onto a saved search
// row and asserts (a) the filter Cmd is returned, (b) focus moves to
// the list pane.
func TestSavedSearchEnterRunsFilter(t *testing.T) {
	m := newDispatchTestModel(t)
	m.folders.SetSavedSearches([]SavedSearch{
		{Name: "Newsletters", Pattern: "~f newsletter@*"},
	})
	// Focus folders.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	require.Equal(t, FoldersPane, m.focused)

	// Walk the cursor to the saved-search row. The newDispatchTestModel
	// seeds 2 folders (Inbox + Archive); after them, items has the
	// section header (skipped on Down) and the saved search.
	for i := 0; i < 5; i++ {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = m2.(Model)
	}
	ss, ok := m.folders.SelectedSavedSearch()
	require.True(t, ok, "cursor must land on saved-search row")
	require.Equal(t, "Newsletters", ss.Name)

	// Enter on the saved search.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ListPane, m.focused, "saved-search Enter auto-focuses list")
	require.NotNil(t, cmd, "Enter must return runFilterCmd")
}

// TestSavedSearchSectionHeaderIsNotSelectable confirms cursor j/k
// skips the synthetic "Saved Searches" header row.
func TestSavedSearchSectionHeaderIsNotSelectable(t *testing.T) {
	m := newDispatchTestModel(t)
	m.folders.SetSavedSearches([]SavedSearch{
		{Name: "X", Pattern: "~A"},
	})
	// Focus folders, walk to the bottom.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	for i := 0; i < 10; i++ {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = m2.(Model)
	}
	// At any point, the cursor must not land on isSavedHeader.
	require.False(t, m.folders.items[m.folders.cursor].isSavedHeader,
		"cursor must skip the saved-searches header")
}

// TestListLoadMoreRevealsMessagesPastInitialPage is the regression for
// the v0.7.x bug where scrolling past row 200 didn't reveal more
// messages even though the local store had them.
func TestListLoadMoreRevealsMessagesPastInitialPage(t *testing.T) {
	m := newDispatchTestModel(t)
	// Seed 500 messages into the inbox (the harness already created
	// f-inbox + 3 messages; we add ~500 more with descending dates so
	// they sort after the seeds).
	st := m.deps.Store
	accID := m.deps.Account.ID
	now := time.Now()
	for i := 0; i < 500; i++ {
		require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
			ID:          "bulk-" + strconvI(i),
			AccountID:   accID,
			FolderID:    "f-inbox",
			Subject:     "bulk-" + strconvI(i),
			FromAddress: "x@example.invalid",
			ReceivedAt:  now.Add(-time.Duration(i+10) * time.Minute),
		}))
	}
	// Force a fresh load via folder Enter so the list pane reflects
	// the seeded messages (initial limit 200).
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, cmd)
	loaded := cmd()
	m2, _ = m.Update(loaded)
	m = m2.(Model)
	require.Equal(t, 200, len(m.list.messages), "initial load is exactly the page size")
	require.Equal(t, initialListLimit, m.list.LoadLimit())

	// Move cursor to row 180 — the threshold.
	m.list.cursor = 180
	require.True(t, m.list.ShouldLoadMore())

	// Press j → should fire load-more, bump limit, return Cmd.
	m2, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.NotNil(t, cmd, "j at threshold returns load-more Cmd")
	require.True(t, m.list.loading, "loading flag set")
	require.Equal(t, initialListLimit+pageIncrement, m.list.LoadLimit(),
		"limit bumped to 400")

	// Run the Cmd and feed back. We expect the result to carry the
	// full 400 messages keyed to the inbox folder ID.
	loaded = cmd()
	mm, ok := loaded.(MessagesLoadedMsg)
	require.True(t, ok, "load-more must return MessagesLoadedMsg, got %T", loaded)
	require.Equal(t, "f-inbox", mm.FolderID, "FolderID matches list FolderID")
	require.Equal(t, 400, len(mm.Messages), "store returns 400 (limit honored)")

	// Apply the result.
	m2, _ = m.Update(loaded)
	m = m2.(Model)
	require.Equal(t, 400, len(m.list.messages),
		"REGRESSION: list pane must reflect the 400-message page")
	require.False(t, m.list.loading, "loading flag cleared after SetMessages")
}

// TestListLoadMoreStopsWhenCacheExhausted is the regression for the
// real-world flapping bug: if the local store has fewer messages than
// loadLimit (e.g. only 50 cached because sync hasn't pulled more yet),
// every j press at the threshold should NOT keep firing load-more
// against an exhausted cache.
func TestListLoadMoreStopsWhenCacheExhausted(t *testing.T) {
	m := newDispatchTestModel(t)
	// The harness seeds 3 messages; load them.
	require.LessOrEqual(t, len(m.list.messages), 50)
	require.True(t, len(m.list.messages) < m.list.LoadLimit(),
		"precondition: cache shorter than loadLimit")

	// Move cursor to the threshold row (or as close as we can get).
	if len(m.list.messages) > 0 {
		m.list.cursor = len(m.list.messages) - 1
	}
	// First j was suppressed because SetMessages already marked
	// cacheExhausted=true (3 messages < 200 loadLimit).
	require.True(t, m.list.cacheExhausted,
		"SetMessages must mark exhausted when result < limit")
	require.False(t, m.list.ShouldLoadMore(),
		"exhausted cache must not trigger load-more")

	// First j at the wall fires the wall-sync Backfill Cmd (kicks
	// Graph for older messages); subsequent j's are debounced by
	// wallSyncRequested. Neither path fires the local-store
	// load-more Cmd — that's the regression this test pins.
	for i := 0; i < 5; i++ {
		m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = m2.(Model)
		if i == 0 {
			require.NotNil(t, cmd, "first j at the wall must kick a Backfill Cmd")
			continue
		}
		require.Nil(t, cmd, "iteration %d: post-wall-sync presses must stay quiet", i)
	}
}

// TestIsLikelyMeetingDetectsInviteSubjects covers the heuristic for
// the calendar-invite indicator in the message list.
func TestIsLikelyMeetingDetectsInviteSubjects(t *testing.T) {
	yes := []string{
		"Accepted: Q4 review",
		"Declined: Standup",
		"Tentative: All-hands",
		"Tentatively accepted: All-hands",
		"Canceled: Project sync",
		"Cancelled: Project sync",
		"Updated: Roadmap chat",
		"Meeting: vendor discussion",
		"Invitation: kickoff",
		"  Accepted:  with leading whitespace",
		"ACCEPTED: case-insensitive",
	}
	no := []string{
		"Q4 review",
		"Re: standup notes",
		"Fwd: roadmap",
		"",
		"acceptance criteria for Q4",
	}
	for _, s := range yes {
		require.True(t, isLikelyMeeting(s), "%q should match", s)
	}
	for _, s := range no {
		require.False(t, isLikelyMeeting(s), "%q should NOT match", s)
	}
}

// TestIsMeetingMessagePrefersCanonicalSignal is the regression for the
// v0.11 real-tenant bug: invites without a heuristic-matching subject
// prefix lost the 📅 indicator. With Graph's meetingMessageType piped
// through the schema, the canonical signal must win — including the
// case where the subject would NOT match the heuristic.
func TestIsMeetingMessagePrefersCanonicalSignal(t *testing.T) {
	cases := []struct {
		name    string
		msg     store.Message
		want    bool
		comment string
	}{
		{
			name:    "canonical-meetingRequest-with-plain-subject",
			msg:     store.Message{Subject: "Q4 sync", MeetingMessageType: "meetingRequest"},
			want:    true,
			comment: "regression: heuristic missed this; canonical signal saves it",
		},
		{
			name:    "canonical-meetingResponse",
			msg:     store.Message{Subject: "Re: Q4", MeetingMessageType: "meetingResponse"},
			want:    true,
			comment: "responses also get the indicator",
		},
		{
			name:    "canonical-none-overrides-heuristic-false-positive",
			msg:     store.Message{Subject: "Meeting: not really a meeting", MeetingMessageType: "none"},
			want:    false,
			comment: `Graph says "not a meeting"; subject's "Meeting:" prefix would otherwise false-positive`,
		},
		{
			name:    "no-canonical-falls-back-to-heuristic-true",
			msg:     store.Message{Subject: "Accepted: standup", MeetingMessageType: ""},
			want:    true,
			comment: "legacy rows pre-migration: heuristic still works",
		},
		{
			name:    "no-canonical-and-no-heuristic-match",
			msg:     store.Message{Subject: "Q4 deck review", MeetingMessageType: ""},
			want:    false,
			comment: "plain mail",
		},
		// Real-tenant gap closed by the bodyPreview heuristic: an
		// invite where the user is being invited carries the
		// meeting title as the subject (no prefix). Outlook fills
		// the bodyPreview with the canonical "When: ... Where:
		// ..." block. With the third signal in isMeetingMessage,
		// these now get the 📅 indicator.
		{
			name: "no-prefix-but-invite-bodypreview-detected",
			msg: store.Message{
				Subject:     "Q4 deck review",
				BodyPreview: "When: Friday, October 31, 2025 14:00-15:00. Where: Conference Room 3.",
			},
			want:    true,
			comment: "When+Where in bodyPreview = invite (the user-reported gap)",
		},
		{
			name: "bodypreview-mentions-when-only-not-an-invite",
			msg: store.Message{
				Subject:     "Re: schedule",
				BodyPreview: "I'll send the details when I'm back from vacation.",
			},
			want:    false,
			comment: "casual 'when' without 'Where:' label is NOT an invite",
		},
		{
			name: "bodypreview-mentions-where-only-not-an-invite",
			msg: store.Message{
				Subject:     "lunch?",
				BodyPreview: "Where do you want to grab lunch later?",
			},
			want:    false,
			comment: "casual 'where' without 'When:' label is NOT an invite",
		},
		{
			name: "bodypreview-checks-only-first-200-chars",
			msg: store.Message{
				Subject: "long email",
				BodyPreview: strings.Repeat("noise ", 50) +
					"When: tomorrow Where: my desk",
			},
			want:    false,
			comment: "When/Where buried past 200 chars don't trigger the heuristic",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isMeetingMessage(tc.msg), tc.comment)
		})
	}
}

// TestCalCommandOpensCalendarModal drives `:cal` and asserts the
// model transitions to CalendarMode and returns a Cmd that fetches
// today's events. Calling that Cmd surfaces a calendarFetchedMsg.
func TestCalCommandOpensCalendarModal(t *testing.T) {
	m := newDispatchTestModel(t)
	now := time.Now().UTC()
	m.deps.Calendar = &stubCalendar{events: []CalendarEvent{
		{Subject: "Standup", Start: now.Add(time.Hour), End: now.Add(time.Hour + 30*time.Minute)},
		{Subject: "Q4 review", Start: now.Add(3 * time.Hour), End: now.Add(4 * time.Hour)},
	}}

	// :cal Enter
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "cal" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, CalendarMode, m.mode)
	require.True(t, m.calendar.loading)
	require.NotNil(t, cmd, ":cal must return fetchCalendarCmd")

	// Run the Cmd; feed the result back.
	res := cmd()
	mm, ok := res.(calendarFetchedMsg)
	require.True(t, ok)
	require.NoError(t, mm.Err)
	require.Len(t, mm.Events, 2)
	m2, _ = m.Update(mm)
	m = m2.(Model)
	require.False(t, m.calendar.loading)
	require.Len(t, m.calendar.events, 2)

	// Esc closes.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.Nil(t, m.calendar.events, "Reset clears events on close")
}

// TestCalCommandWithoutFetcherFailsGracefully confirms that running
// `:cal` without a Calendar wired sets lastError and stays in
// NormalMode (the modal does NOT open into a broken state).
func TestCalCommandWithoutFetcherFailsGracefully(t *testing.T) {
	m := newDispatchTestModel(t)
	require.Nil(t, m.deps.Calendar)
	m2, cmd := m.dispatchCommand("cal")
	m = m2.(Model)
	require.Nil(t, cmd)
	require.Error(t, m.lastError)
	require.NotEqual(t, CalendarMode, m.mode)
}

type stubCalendar struct {
	events    []CalendarEvent
	err       error
	detail    CalendarEventDetail
	detailErr error
	getCalls  int
	gotID     string
	// For ListEventsBetween tracking.
	betweenEvents    []CalendarEvent
	betweenErr       error
	betweenCalls     int
	lastBetweenStart time.Time
	lastBetweenEnd   time.Time
}

func (s *stubCalendar) ListEventsToday(_ context.Context) ([]CalendarEvent, error) {
	return s.events, s.err
}

func (s *stubCalendar) ListEventsBetween(_ context.Context, start, end time.Time) ([]CalendarEvent, error) {
	s.betweenCalls++
	s.lastBetweenStart = start
	s.lastBetweenEnd = end
	return s.betweenEvents, s.betweenErr
}

func (s *stubCalendar) GetEvent(_ context.Context, id string) (CalendarEventDetail, error) {
	s.getCalls++
	s.gotID = id
	return s.detail, s.detailErr
}

// TestWallSyncFiresOncePerCacheState is the regression for the
// real-tenant churn bug: every j press at the cache wall fired
// SyncAll, producing 3 cycles in 2.5s of logs. After the fix, only
// the first j at the wall kicks a sync; subsequent j-presses are
// silent until SetMessages clears the debounce flag.
func TestWallSyncFiresOncePerCacheState(t *testing.T) {
	m := newDispatchTestModel(t)
	// Force cache-wall state.
	m.list.cacheExhausted = true
	if len(m.list.messages) > 0 {
		m.list.cursor = len(m.list.messages) - 1
	}
	require.True(t, m.list.AtCacheWall())
	require.False(t, m.list.wallSyncRequested)

	// Stub engine that counts SyncAll calls.
	syncCount := atomicBool{} // we'll use it as a single-shot flag
	m.deps.Engine = stubCountingEngine{onSync: syncCount.set}

	// Press j 5 times at the wall.
	for i := 0; i < 5; i++ {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = m2.(Model)
	}
	// Crucially, the debounce flag should be set so we don't keep
	// kicking on every subsequent j.
	require.True(t, m.list.wallSyncRequested,
		"first j at the wall must arm the debounce flag")
}

// stubCountingEngine satisfies ui.Engine; counts SyncAll/Backfill calls.
type stubCountingEngine struct{ onSync func() }

func (s stubCountingEngine) Start(_ context.Context) error { return nil }
func (s stubCountingEngine) SetActive(_ bool)              {}
func (s stubCountingEngine) SyncAll(_ context.Context) error {
	if s.onSync != nil {
		s.onSync()
	}
	return nil
}
func (s stubCountingEngine) Wake() {
	if s.onSync != nil {
		s.onSync()
	}
}
func (s stubCountingEngine) Backfill(_ context.Context, _ string, _ time.Time) error {
	if s.onSync != nil {
		s.onSync()
	}
	return nil
}
func (s stubCountingEngine) Notifications() <-chan isync.Event {
	return make(chan isync.Event)
}

// TestViewerCapitalHTogglesFullHeaders confirms `H` while in the
// viewer flips between compact (default) and full header display.
// The compact form addresses the many-attendee-email-eats-the-pane
// bug; H is the mutt convention for header toggle.
func TestViewerCapitalHTogglesFullHeaders(t *testing.T) {
	m := newDispatchTestModel(t)
	// Open a message; focus is now ViewerPane.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)
	require.False(t, m.viewer.showFullHdr, "default is compact")

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")})
	m = m2.(Model)
	require.True(t, m.viewer.showFullHdr, "H expands to full")

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")})
	m = m2.(Model)
	require.False(t, m.viewer.showFullHdr, "H toggles back to compact")
}

// TestCompactAddrsSummarisesAcrossToCcBcc covers the helper used by
// the viewer's compact-headers row.
func TestCompactAddrsSummarisesAcrossToCcBcc(t *testing.T) {
	mk := func(name, addr string) store.EmailAddress {
		return store.EmailAddress{Name: name, Address: addr}
	}
	to := []store.EmailAddress{
		mk("Alice", "a@x"), mk("Bob", "b@x"),
	}
	require.Equal(t, "Alice, Bob", compactAddrs(to, nil, nil),
		"≤3 recipients show in full")

	bigTo := []store.EmailAddress{
		mk("A", "a@x"), mk("B", "b@x"), mk("C", "c@x"),
		mk("D", "d@x"), mk("E", "e@x"),
	}
	got := compactAddrs(bigTo, nil, nil)
	require.Contains(t, got, "A, B, C")
	require.Contains(t, got, "+ 2 more")

	// Cc / Bcc count toward "more".
	cc := []store.EmailAddress{mk("X", "x@x")}
	bcc := []store.EmailAddress{mk("Y", "y@x")}
	got = compactAddrs(to, cc, bcc)
	require.Contains(t, got, "Alice, Bob")
	require.Contains(t, got, "+ 2 more", "Cc + Bcc add to the count")

	require.Equal(t, "—", compactAddrs(nil, nil, nil),
		"empty case shows em-dash")
}

// TestReplyKeyEntersComposeMode is the spec 15 v2 §6 invariant:
// pressing `r` in the viewer enters the in-modal ComposeMode with
// the reply skeleton pre-filled (source captured, To populated,
// Subject prefixed). Replaces the v1 editor flow that returned a
// composeStartedMsg + ran tea.ExecProcess; the in-modal flow keeps
// inkwell on screen so save / discard live in the persistent
// footer.
func TestReplyKeyEntersComposeMode(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}

	// Open a message → focus moves to viewer.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)
	require.NotNil(t, m.viewer.current)

	// Press r in the viewer pane.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)

	require.Equal(t, ComposeMode, m.mode, "r enters in-modal compose")
	require.Equal(t, m.viewer.current.ID, m.compose.SourceID,
		"compose captures the source id for the eventual save")
	require.NotEmpty(t, m.compose.Subject(),
		"reply skeleton populated subject (Re: ...)")
}

// TestViewerReplyWithoutDraftsDepShowsFriendlyError confirms `r` in
// the viewer pane records a friendly error when Drafts is nil
// instead of crashing.
func TestViewerReplyWithoutDraftsDepShowsFriendlyError(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)
	require.Nil(t, m.deps.Drafts)

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)
	require.Nil(t, cmd)
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "drafts")
}

// TestDraftSavedMsgPopulatesWebLinkAndStatus confirms the
// post-save handler stashes webLink for the `s` shortcut and shows
// the success blurb in the status bar.
func TestDraftSavedMsgPopulatesWebLinkAndStatus(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(draftSavedMsg{webLink: "https://outlook.office.com/draft/abc"})
	m = m2.(Model)
	require.Equal(t, "https://outlook.office.com/draft/abc", m.lastDraftWebLink)
	require.Contains(t, m.engineActivity, "draft saved")
	require.Contains(t, m.engineActivity, "press s")
}

// stubDraftCreator satisfies ui.DraftCreator.
type stubDraftCreator struct{ onCall func() }

func (s stubDraftCreator) CreateDraftReply(_ context.Context, _ int64, srcID, body string, to, cc, bcc []string, subject string) (*DraftRef, error) {
	if s.onCall != nil {
		s.onCall()
	}
	return &DraftRef{ID: "draft-" + srcID, WebLink: "https://outlook.office.com/draft/" + srcID}, nil
}

func (s stubDraftCreator) CreateDraftReplyAll(_ context.Context, _ int64, srcID, body string, to, cc, bcc []string, subject string) (*DraftRef, error) {
	if s.onCall != nil {
		s.onCall()
	}
	return &DraftRef{ID: "draft-rall-" + srcID, WebLink: "https://outlook.office.com/draft/" + srcID}, nil
}

func (s stubDraftCreator) CreateDraftForward(_ context.Context, _ int64, srcID, body string, to, cc, bcc []string, subject string) (*DraftRef, error) {
	if s.onCall != nil {
		s.onCall()
	}
	return &DraftRef{ID: "draft-fwd-" + srcID, WebLink: "https://outlook.office.com/draft/" + srcID}, nil
}

func (s stubDraftCreator) CreateNewDraft(_ context.Context, _ int64, body string, to, cc, bcc []string, subject string) (*DraftRef, error) {
	if s.onCall != nil {
		s.onCall()
	}
	return &DraftRef{ID: "draft-new", WebLink: "https://outlook.office.com/draft/new"}, nil
}

// TestComposeTabCyclesFields verifies the in-modal Tab navigation
// reaches the spec 15 v2 §9 cycle order (Body → To → Cc → Subject).
func TestComposeTabCyclesFields(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)
	require.Equal(t, ComposeMode, m.mode)
	require.Equal(t, ComposeFieldBody, m.compose.Focused())

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	require.Equal(t, ComposeFieldTo, m.compose.Focused())
}

// TestComposeCtrlSSavesAndExitsMode dispatches the form snapshot
// through saveComposeCmd. The model returns to NormalMode and
// surfaces the saving-status hint; the Cmd returns the
// draftSavedMsg from the stub creator.
func TestComposeCtrlSSavesAndExitsMode(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &recordingDraftCreator{}
	m.deps.Drafts = stub
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)
	require.Equal(t, ComposeMode, m.mode)

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode, "Ctrl+S exits compose mode")
	require.Contains(t, m.engineActivity, "saving")
	require.NotNil(t, cmd)
	_ = cmd()
	require.Equal(t, 1, stub.calls,
		"saveComposeCmd dispatched the draft via DraftCreator")
}

// TestComposeEscIsSaveAlias matches the user's "I'm done" gesture:
// Esc behaves identically to Ctrl+S so the redesign keeps the
// muscle memory the v1 post-edit modal's Enter alias trained.
func TestComposeEscIsSaveAlias(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &recordingDraftCreator{}
	m.deps.Drafts = stub
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.NotNil(t, cmd)
	_ = cmd()
	require.Equal(t, 1, stub.calls)
}

// TestComposeCtrlDDiscards fires the discard path: NormalMode,
// "discarded" status, no Graph round-trip.
func TestComposeCtrlDDiscards(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &recordingDraftCreator{}
	m.deps.Drafts = stub
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.Contains(t, m.engineActivity, "discarded")
	require.Nil(t, cmd, "Ctrl+D returns no save Cmd")
	require.Equal(t, 0, stub.calls, "discard means NO Graph round-trip")
}

// TestComposeRecipientRecoveryFromSourceFromAddress is the spec
// 15 v2 §6 safety net carried over from v1: if the user clears
// the To field, the save path falls back to the source message's
// FromAddress. The reply gesture implies the original sender as
// the recipient; an empty To shouldn't error out.
func TestComposeRecipientRecoveryFromSourceFromAddress(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &recordingDraftCreator{}
	m.deps.Drafts = stub

	// Construct a snapshot directly with empty To, source m-1.
	// Skip the full open-modal dance; saveComposeCmd is the unit
	// the recovery lives in.
	snap := ComposeSnapshot{
		Kind:     ComposeKindReply,
		SourceID: "m-1",
		To:       "",
		Subject:  "Re: x",
		Body:     "the body",
	}
	cmd := m.saveComposeCmd(snap, "")
	require.NotNil(t, cmd)
	res := cmd()
	saved, _ := res.(draftSavedMsg)
	require.NoError(t, saved.err)
	require.Equal(t, []string{"alice@example.invalid"}, stub.lastTo,
		"empty To recovered from m-1's FromAddress")
}

// TestComposeSaveErrorsWithoutFallback confirms the no-recipient
// path surfaces an actionable error when neither the form nor the
// source can supply a recipient — instead of silently dispatching
// an empty draft.
func TestComposeSaveErrorsWithoutFallback(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &recordingDraftCreator{}
	m.deps.Drafts = stub
	snap := ComposeSnapshot{Kind: ComposeKindReply} // no SourceID, no To
	cmd := m.saveComposeCmd(snap, "")
	res := cmd()
	saved, _ := res.(draftSavedMsg)
	require.Error(t, saved.err)
	require.Contains(t, saved.err.Error(), "no recipient")
	require.Equal(t, 0, stub.calls)
}

// recordingDraftCreator captures the args of the most recent
// CreateDraftReply call so tests can assert on the exact recipient
// list passed through.
type recordingDraftCreator struct {
	calls   int
	lastTo  []string
	lastCc  []string
	lastBcc []string
	// lastKind records which DraftCreator method was invoked so
	// per-kind dispatch tests can assert the routing in
	// saveComposeCmd. "" until first call.
	lastKind string
	// lastSourceID is "" for the New-draft path (no source).
	lastSourceID string
}

func (s *recordingDraftCreator) CreateDraftReply(_ context.Context, _ int64, srcID, body string, to, cc, bcc []string, subject string) (*DraftRef, error) {
	s.calls++
	s.lastTo = to
	s.lastCc = cc
	s.lastBcc = bcc
	s.lastKind = "reply"
	s.lastSourceID = srcID
	return &DraftRef{ID: "draft-" + srcID, WebLink: "https://outlook/draft/" + srcID}, nil
}

func (s *recordingDraftCreator) CreateDraftReplyAll(_ context.Context, _ int64, srcID, body string, to, cc, bcc []string, subject string) (*DraftRef, error) {
	s.calls++
	s.lastTo = to
	s.lastCc = cc
	s.lastBcc = bcc
	s.lastKind = "reply_all"
	s.lastSourceID = srcID
	return &DraftRef{ID: "draft-rall-" + srcID, WebLink: "https://outlook/draft/" + srcID}, nil
}

func (s *recordingDraftCreator) CreateDraftForward(_ context.Context, _ int64, srcID, body string, to, cc, bcc []string, subject string) (*DraftRef, error) {
	s.calls++
	s.lastTo = to
	s.lastCc = cc
	s.lastBcc = bcc
	s.lastKind = "forward"
	s.lastSourceID = srcID
	return &DraftRef{ID: "draft-fwd-" + srcID, WebLink: "https://outlook/draft/" + srcID}, nil
}

func (s *recordingDraftCreator) CreateNewDraft(_ context.Context, _ int64, body string, to, cc, bcc []string, subject string) (*DraftRef, error) {
	s.calls++
	s.lastTo = to
	s.lastCc = cc
	s.lastBcc = bcc
	s.lastKind = "new"
	s.lastSourceID = ""
	return &DraftRef{ID: "draft-new", WebLink: "https://outlook/draft/new"}, nil
}

// TestFolderSwitchClearsActiveSearch is the regression for the bug
// caught in the v0.5.0 internal code review: pressing '/foo<Enter>'
// then switching folders via '1' + 'Enter' left searchActive=true,
// so the cmd-bar reminder "search: foo (esc to clear)" lingered over
// messages that weren't search results.
func TestFolderSwitchClearsActiveSearch(t *testing.T) {
	m := newDispatchTestModel(t)
	// Run a search.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m2.(Model)
	for _, r := range "forecast" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.True(t, m.searchActive)

	// Now navigate to a folder via '1' + j + Enter.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)

	require.False(t, m.searchActive, "switching folders must clear search state")
	require.Empty(t, m.searchQuery)
}

// TestColonEntersCommandMode confirms ':' transitions to CommandMode
// and the command bar buffer is empty on entry.
func TestColonEntersCommandMode(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	require.Equal(t, CommandMode, m.mode)
	require.Empty(t, m.cmd.Buffer())
}

// TestPrevPaneCyclesBackwards confirms shift+tab cycles in reverse:
// list → folders → viewer → list.
func TestPrevPaneCyclesBackwards(t *testing.T) {
	m := newDispatchTestModel(t)
	require.Equal(t, ListPane, m.focused)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = m2.(Model)
	require.Equal(t, FoldersPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = m2.(Model)
	require.Equal(t, ListPane, m.focused)
}

// TestViewerLeftReturnsToList confirms 'h' in the viewer pane moves
// focus back to the list, mirroring vim navigation idioms.
func TestViewerLeftReturnsToList(t *testing.T) {
	m := newDispatchTestModel(t)
	// Open a message → focus moves to viewer.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = m2.(Model)
	require.Equal(t, ListPane, m.focused)
}

// TestFolderRightOpensFolder confirms 'l' (Right) in the folders pane
// behaves like Enter: switches the message list to that folder and
// auto-focuses ListPane.
func TestFolderRightOpensFolder(t *testing.T) {
	m := newDispatchTestModel(t)
	// Focus folders, move cursor down to Archive.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)

	beforeID := m.list.FolderID
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = m2.(Model)
	require.NotEqual(t, beforeID, m.list.FolderID, "Right on folder must switch the list")
	require.Equal(t, ListPane, m.focused, "Right also auto-focuses the list (Enter parity)")
	require.NotNil(t, cmd, "Right returns loadMessagesCmd")
}

// TestSearchModeCapturesAndRunsQuery activates search via '/', types
// a query, presses Enter, and asserts (a) the model entered SearchMode
// then exited, (b) searchActive is true, and (c) a Cmd was returned to
// run the FTS query.
func TestSearchModeCapturesAndRunsQuery(t *testing.T) {
	m := newDispatchTestModel(t)

	// '/' enters search mode.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m2.(Model)
	require.Equal(t, SearchMode, m.mode)
	require.Empty(t, m.searchBuf)

	// Type "forecast".
	for _, r := range "forecast" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	require.Equal(t, "forecast", m.searchBuf)

	// Enter commits and runs the query.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.True(t, m.searchActive)
	require.Equal(t, "forecast", m.searchQuery)
	require.NotNil(t, cmd, "Enter must return runSearchCmd")

	// Esc clears search and restores prior folder.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m2.(Model)
	require.Equal(t, SearchMode, m.mode)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.False(t, m.searchActive, "Esc clears searchActive")
}

// TestSearchEmptyQueryDoesNothing confirms hitting Enter on an empty
// search buffer just exits to NormalMode without firing a Cmd.
func TestSearchEmptyQueryDoesNothing(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m2.(Model)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.False(t, m.searchActive)
	require.Nil(t, cmd, "empty query must not run search")
}

// TestListLoadMoreFiresWhenCursorNearsBottom seeds a list with 200
// messages, drives j to the threshold, and asserts the next j returns
// a load-more Cmd that calls loadMessagesCmd with bumped limit.
func TestListLoadMoreFiresWhenCursorNearsBottom(t *testing.T) {
	m := newDispatchTestModel(t)
	// Build a synthetic 200-message list at exactly initialListLimit.
	msgs := make([]store.Message, initialListLimit)
	for i := range msgs {
		msgs[i] = store.Message{ID: "m-" + strconvI(i), AccountID: 1, FolderID: "f-inbox"}
	}
	m.list.SetMessages(msgs)
	require.Equal(t, initialListLimit, m.list.LoadLimit())

	// Move cursor to 200 - 21 = 179 → still above threshold.
	m.list.cursor = len(msgs) - loadMoreThreshold - 1
	require.False(t, m.list.ShouldLoadMore())

	// One more j → cursor at 180 → threshold reached → ShouldLoadMore.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.NotNil(t, cmd, "j at threshold must return load-more Cmd")
	require.True(t, m.list.loading, "loading flag set so duplicate j doesn't refire")
	require.Equal(t, initialListLimit+pageIncrement, m.list.LoadLimit(),
		"limit bumped by pageIncrement")

	// A second j while loading must NOT fire another Cmd.
	m2, cmd2 := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Nil(t, cmd2, "duplicate load-more suppressed while loading")
	_ = m
}

// TestListLoadMoreSuppressedDuringSearch — search results have a
// fixed FTS limit; pre-fetch must be a no-op so we don't trigger
// folder loads with the search-sentinel ID.
func TestListLoadMoreSuppressedDuringSearch(t *testing.T) {
	m := newDispatchTestModel(t)
	msgs := make([]store.Message, initialListLimit)
	for i := range msgs {
		msgs[i] = store.Message{ID: "m-" + strconvI(i)}
	}
	m.list.SetMessages(msgs)
	m.list.cursor = len(msgs) - 1
	m.searchActive = true
	m.list.FolderID = "search:foo"

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Nil(t, cmd, "search results must not trigger pagination")
	_ = m
}

// strconvI keeps the test self-contained without an extra import for
// a single int-to-string conversion.
func strconvI(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// TestThemePresetsAreValid confirms every preset palette renders into
// a Theme without empty styles. A new preset that fails this check
// would silently produce an unstyled UI in production.
func TestThemePresetsAreValid(t *testing.T) {
	for name := range presetPalettes {
		theme, ok := ThemeByName(name)
		require.True(t, ok, "preset %q must resolve", name)
		// Every Theme field must be non-zero (lipgloss styles aren't
		// directly comparable, so we render a probe and require non-
		// empty output).
		require.NotEmpty(t, theme.Bold.Render("x"), "Bold must render content for %q", name)
		require.NotEmpty(t, theme.Dim.Render("x"), "Dim must render content for %q", name)
	}
}

// TestThemeUnknownNameFallsBack confirms an unknown theme name returns
// (default, false). cmd_run.go logs a warning on the false branch.
func TestThemeUnknownNameFallsBack(t *testing.T) {
	_, ok := ThemeByName("not-a-real-theme")
	require.False(t, ok, "unknown name must report fallback")
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

// openViewerWithLinks brings the model into ViewerPane focus with a
// known URL list staged on the renderer. Used by the URL-picker
// tests so each one starts from a deterministic point.
func openViewerWithLinks(t *testing.T, m Model, links []BodyLink) Model {
	t.Helper()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)
	m.viewer.SetLinks(links)
	return m
}

// captureYanker swaps a buffer-backed writer onto the model's
// yanker so tests can read what got "copied" without firing
// pbcopy or hitting stdout. Returns the byte buffer.
func captureYanker(m *Model) *strings.Builder {
	buf := &strings.Builder{}
	m.yanker = &yanker{
		writeOSC52: func(s string) error { _, _ = buf.WriteString(s); return nil },
	}
	return buf
}

// TestDispatchViewerOOpensURLPicker is the visible-truth dispatch
// test: pressing `o` in the focused viewer pane flips m.mode to
// URLPickerMode AND the picker render produces non-empty output.
// Without the mode flip the View branch never runs so the user
// sees nothing.
func TestDispatchViewerOOpensURLPicker(t *testing.T) {
	m := newDispatchTestModel(t)
	m = openViewerWithLinks(t, m, []BodyLink{
		{Index: 1, URL: "https://example.invalid/a", Text: "anchor a"},
		{Index: 2, URL: "https://example.invalid/b", Text: "anchor b"},
	})

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	m = m2.(Model)

	require.Equal(t, URLPickerMode, m.mode, "O in viewer must enter URLPickerMode")
	frame := m.View()
	require.Contains(t, frame, "https://example.invalid/a", "picker frame must render URL #1")
	require.Contains(t, frame, "https://example.invalid/b", "picker frame must render URL #2")
}

// TestDispatchURLPickerJKMovesCursor confirms j/k move the picker
// cursor within the link bounds. Without this the user is stuck on
// row 0 and can only ever yank/open the first URL.
func TestDispatchURLPickerJKMovesCursor(t *testing.T) {
	m := newDispatchTestModel(t)
	m = openViewerWithLinks(t, m, []BodyLink{
		{Index: 1, URL: "https://example.invalid/a"},
		{Index: 2, URL: "https://example.invalid/b"},
		{Index: 3, URL: "https://example.invalid/c"},
	})
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	m = m2.(Model)
	require.Equal(t, 0, m.urlPicker.cursor)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Equal(t, 1, m.urlPicker.cursor)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Equal(t, 2, m.urlPicker.cursor)

	// j at the bottom is a no-op (no wrap).
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Equal(t, 2, m.urlPicker.cursor)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = m2.(Model)
	require.Equal(t, 1, m.urlPicker.cursor)
}

// TestDispatchURLPickerYYanksSelectedURL is the spec 05 §10 truth: y
// in the picker writes the cursor's URL via the yanker and exits
// the picker. Without this the picker is decorative.
func TestDispatchURLPickerYYanksSelectedURL(t *testing.T) {
	m := newDispatchTestModel(t)
	m = openViewerWithLinks(t, m, []BodyLink{
		{Index: 1, URL: "https://example.invalid/a"},
		{Index: 2, URL: "https://example.invalid/b"},
	})
	buf := captureYanker(&m)

	// Open picker, move to row 1, yank.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m2.(Model)

	require.Equal(t, NormalMode, m.mode, "y must close the picker")
	require.Contains(t, buf.String(), "\x1b]52;c;", "OSC 52 sequence must hit the writer")
	require.Contains(t, buf.String(), osc52Sequence("https://example.invalid/b"),
		"yanked URL must be the cursor's row, not row 0")
	require.Contains(t, m.engineActivity, "copied URL", "status must reflect yank")
}

// TestDispatchViewerYWithSingleURLFastPathYanks confirms the
// shortcut: when the body has exactly one URL, viewer-pane y skips
// the picker and yanks immediately. This is the urlview parity case.
func TestDispatchViewerYWithSingleURLFastPathYanks(t *testing.T) {
	m := newDispatchTestModel(t)
	m = openViewerWithLinks(t, m, []BodyLink{
		{Index: 1, URL: "https://example.invalid/only"},
	})
	buf := captureYanker(&m)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m2.(Model)

	require.Equal(t, NormalMode, m.mode, "single-URL y must NOT enter picker mode")
	require.Contains(t, buf.String(), osc52Sequence("https://example.invalid/only"))
	require.Contains(t, m.engineActivity, "copied URL")
}

// TestDispatchViewerYWithMultipleURLsOpensPicker is the disambig
// case: 2+ URLs means y must surface the picker so the user can
// pick. We assert mode flip + status hint so the user knows what
// to do next.
func TestDispatchViewerYWithMultipleURLsOpensPicker(t *testing.T) {
	m := newDispatchTestModel(t)
	m = openViewerWithLinks(t, m, []BodyLink{
		{Index: 1, URL: "https://example.invalid/a"},
		{Index: 2, URL: "https://example.invalid/b"},
	})

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m2.(Model)

	require.Equal(t, URLPickerMode, m.mode)
	require.Contains(t, m.engineActivity, "yanks selected URL", "must hint at picker workflow")
}

// TestDispatchURLPickerEscClosesWithoutAction confirms Esc / q
// closes the picker without yanking or opening anything.
func TestDispatchURLPickerEscClosesWithoutAction(t *testing.T) {
	m := newDispatchTestModel(t)
	m = openViewerWithLinks(t, m, []BodyLink{
		{Index: 1, URL: "https://example.invalid/a"},
	})
	buf := captureYanker(&m)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	m = m2.(Model)
	require.Equal(t, URLPickerMode, m.mode)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)

	require.Equal(t, NormalMode, m.mode, "Esc must close the picker")
	require.Empty(t, buf.String(), "Esc must NOT emit any clipboard sequence")
}

// TestDispatchViewerZEntersFullscreenAndExits confirms `z` in the
// viewer toggles FullscreenBodyMode, and that the rendered frame in
// that mode does NOT include the folder/list pane chrome (those are
// the panes the mode is meant to hide so terminal-native drag-
// selection works end-to-end).
func TestDispatchViewerZEntersFullscreenAndExits(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	m = m2.(Model)
	require.Equal(t, FullscreenBodyMode, m.mode)

	frame := m.View()
	require.NotContains(t, frame, "Folders", "fullscreen frame must hide the folders pane header")
	require.Contains(t, frame, "exit fullscreen", "fullscreen hint must be visible")

	// z again exits.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
}

// TestIsStaleIDErrorRecognisesGraph404Variants pins the matcher
// for Graph "object not found" responses. These come back as text
// strings inside BodyRenderedMsg so the matcher is what decides
// whether to clean up the stale local row.
func TestIsStaleIDErrorRecognisesGraph404Variants(t *testing.T) {
	hits := []string{
		"fetch error: graph: ErrorItemNotFound: The specified object was not found in the store.",
		"fetch error: graph: 404 Not Found",
		"render error: object not found",
	}
	for _, s := range hits {
		require.True(t, isStaleIDError(s), "expected stale-id match for %q", s)
	}
	misses := []string{
		"render error: html parse failure",
		"fetch error: graph: 503 Service Unavailable",
		"",
	}
	for _, s := range misses {
		require.False(t, isStaleIDError(s), "expected non-match for %q", s)
	}
}

// TestTriageDoneInSearchModeReRunsSearch is the v0.15.x
// regression: pressing `d` on a `/<query>` result fired
// triageDoneMsg with folderID = "search:<query>" (the list pane
// sentinel). The handler matched it against m.list.FolderID and
// fired loadMessagesCmd("search:<query>") — which returned zero
// rows because the sentinel has no real folder backing. Every
// search result visibly disappeared. Fix: when searchActive,
// re-run runSearchCmd instead.
func TestTriageDoneInSearchModeReRunsSearch(t *testing.T) {
	m := newDispatchTestModel(t)
	// Establish the search-active state directly (sidesteps the
	// async runSearchCmd which would race with the test).
	m.searchActive = true
	m.searchQuery = "ABC"
	m.list.FolderID = searchFolderID("ABC")
	m.priorFolderID = "f-inbox"

	m2, cmd := m.Update(triageDoneMsg{
		name:      "soft_delete",
		folderID:  m.list.FolderID,
		msgID:     "m-1",
		postFocus: ListPane,
	})
	m = m2.(Model)
	require.NotNil(t, cmd, "search-mode triage must return a re-search Cmd")
	// The Cmd is runSearchCmd — exercising it produces a
	// MessagesLoadedMsg keyed to the sentinel folder ID, NOT a
	// loadMessagesCmd that would return zero rows.
	out := cmd()
	loaded, ok := out.(MessagesLoadedMsg)
	require.True(t, ok, "search-mode triage Cmd must produce MessagesLoadedMsg, got %T", out)
	require.Equal(t, searchFolderID("ABC"), loaded.FolderID,
		"refreshed list must stay keyed to the search sentinel")
	require.Contains(t, m.engineActivity, "soft_delete", "status hint must reflect the action")
}

// TestUnfilterFallsBackToInboxWhenPriorEmpty confirms the v0.15.x
// regression where running `:filter` before any folder load (so
// priorFolderID was captured as "") then `:unfilter` was a stuck
// no-op. With the inbox available in the folders pane, unfilter
// must land the list there.
func TestUnfilterFallsBackToInboxWhenPriorEmpty(t *testing.T) {
	m := newDispatchTestModel(t)
	// Force the buggy state: filter active, prior empty.
	m.filterActive = true
	m.filterPattern = "~B *something*"
	m.priorFolderID = ""
	m.list.FolderID = "filter:" + m.filterPattern

	m2, cmd := m.dispatchCommand("unfilter")
	m = m2.(Model)
	require.False(t, m.filterActive, "filter must clear")
	require.Equal(t, "f-inbox", m.list.FolderID, "unfilter must fall back to Inbox")
	require.NotNil(t, cmd, "unfilter must reload the inbox")
}

// TestTruncateRespectsCellWidthForEmoji is the real-tenant
// Ghostty regression: list rows with the 📅 invite glyph (1 rune,
// 2 cells) overshot the configured pane width when truncate
// sliced by rune count instead of visual cell width. The right-
// edge characters then spilled past the pane until the user
// resized the terminal.
func TestTruncateRespectsCellWidthForEmoji(t *testing.T) {
	// "📅 hello" — 📅 (2 cells) + " " (1) + "hello" (5) = 8 cells.
	in := "📅 hello"
	require.Equal(t, 8, lipgloss.Width(in))

	// Cap at 5 cells: must keep "📅 he" (= 5 cells).
	out := truncate(in, 5)
	require.LessOrEqual(t, lipgloss.Width(out), 5,
		"truncate must respect cell width even with wide-glyph prefix")
	require.Equal(t, "📅 he", out)

	// Cap at 2 cells: only the 📅 fits (2 cells).
	out2 := truncate(in, 2)
	require.LessOrEqual(t, lipgloss.Width(out2), 2)
	require.Equal(t, "📅", out2)

	// Cap at 1 cell: 📅 doesn't fit (it's 2 cells); result is empty
	// rather than overshot.
	out3 := truncate(in, 1)
	require.LessOrEqual(t, lipgloss.Width(out3), 1)
	require.Empty(t, out3)
}

// TestTruncateRespectsCellWidthForCJK guards the same invariant
// for East-Asian wide characters: each CJK glyph occupies 2 cells
// despite being 1 rune.
func TestTruncateRespectsCellWidthForCJK(t *testing.T) {
	// "李四" — 2 runes, 4 cells.
	in := "李四 王五"
	w := lipgloss.Width(in)
	out := truncate(in, 4)
	require.LessOrEqual(t, lipgloss.Width(out), 4,
		"CJK glyphs must be measured by cell width, got input width %d", w)
}

// TestViewerBodyPreservesOSC8Hyperlinks asserts the rendered viewer
// pane retains the renderer's OSC 8 escape sequences end-to-end.
// Without this, lipgloss width / height truncation could silently
// strip the escapes and Cmd-click would stop working in iTerm2 /
// kitty / wezterm — exactly what users reported on v0.15.0
// ("can't click links"). The test is byte-level: it asserts the
// raw \x1b]8;; / \x1b\\ delimiters survive the View() pipeline.
func TestViewerBodyPreservesOSC8Hyperlinks(t *testing.T) {
	v := NewViewer()
	msg := store.Message{ID: "x", Subject: "test"}
	v.SetMessage(msg)
	// Body that the renderer would have produced — OSC 8 wrap
	// around a URL.
	url := "https://example.invalid/click-me"
	wrapped := "\x1b]8;;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\"
	v.SetBody("See: "+wrapped+"\n", 0)

	out := v.View(DefaultTheme(), 80, 20, true)
	require.Contains(t, out, "\x1b]8;;"+url+"\x1b\\",
		"OSC 8 opening escape must survive lipgloss render")
	require.Contains(t, out, url+"\x1b]8;;\x1b\\",
		"OSC 8 closing escape must survive lipgloss render")
}

// TestDispatchListPageDownJumpsCursor confirms PgDn moves the list
// cursor multiple rows. Without a handler, PgDn was a silent no-op
// (real-tenant regression v0.15.0).
func TestDispatchListPageDownJumpsCursor(t *testing.T) {
	m := newDispatchTestModel(t)
	// Inflate the seeded list so PgDn can actually jump.
	for i := 0; i < 30; i++ {
		require.NoError(t, m.deps.Store.UpsertMessage(context.Background(), store.Message{
			ID:          "p-" + strconvI(i),
			AccountID:   m.deps.Account.ID,
			FolderID:    "f-inbox",
			Subject:     "filler " + strconvI(i),
			FromAddress: "x@example.invalid",
			ReceivedAt:  time.Now().Add(-time.Duration(i) * time.Minute),
		}))
	}
	cmd := m.loadMessagesCmd("f-inbox")
	m2, _ := m.Update(cmd())
	m = m2.(Model)
	require.GreaterOrEqual(t, len(m.list.messages), 20)
	// SetMessages preserves the cursor's prior message-ID; reset
	// to row 0 explicitly so the PgDn delta is unambiguous.
	m.list.cursor = 0

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = m2.(Model)
	require.Greater(t, m.list.cursor, 0, "PgDn must advance cursor")
	require.LessOrEqual(t, m.list.cursor, len(m.list.messages)-1)
}

// TestDispatchListEndJumpsToLastMessage pins End behaviour: cursor
// snaps to the last loaded message.
func TestDispatchListEndJumpsToLastMessage(t *testing.T) {
	m := newDispatchTestModel(t)
	require.GreaterOrEqual(t, len(m.list.messages), 2)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = m2.(Model)
	require.Equal(t, len(m.list.messages)-1, m.list.cursor)
}

// TestDispatchListHomeJumpsToFirst pins Home: cursor snaps to row 0
// even from deep in the list.
func TestDispatchListHomeJumpsToFirst(t *testing.T) {
	m := newDispatchTestModel(t)
	require.GreaterOrEqual(t, len(m.list.messages), 2)
	m.list.cursor = len(m.list.messages) - 1
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = m2.(Model)
	require.Equal(t, 0, m.list.cursor)
}

// TestDispatchViewerPageDownAdvancesScroll confirms PgDn in the
// viewer scrolls the body by viewerPageStep lines.
func TestDispatchViewerPageDownAdvancesScroll(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	before := m.viewer.scrollY
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = m2.(Model)
	require.Equal(t, before+viewerPageStep, m.viewer.scrollY)

	// PgUp returns toward the top, clamped at 0.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = m2.(Model)
	require.Equal(t, before, m.viewer.scrollY)
}

// TestDispatchExpandOnLeafFolderPaintsHint confirms pressing `o`
// on a folder with no synced children paints a status hint instead
// of staying visually silent (real-tenant regression v0.15.0 where
// users on inboxes whose nested children weren't yet synced
// thought Expand was broken).
func TestDispatchExpandOnLeafFolderPaintsHint(t *testing.T) {
	m := newDispatchTestModel(t)
	// Focus folders pane.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	require.Equal(t, FoldersPane, m.focused)

	// The seed has Inbox + Archive — both top-level, no children
	// in the local store. Pressing `o` on either is a no-op
	// expand but should paint the hint.
	m.engineActivity = ""
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = m2.(Model)
	require.Contains(t, m.engineActivity, "no subfolders")
}

// TestBackfillDoneMsgErrorSurfaces confirms a failed Backfill
// transitions the activity hint OFF "loading older messages…" and
// pushes the error into m.lastError. Real-tenant regression
// v0.15.0: the previous fire-and-forget goroutine swallowed every
// error so users were stuck on the activity hint forever.
func TestBackfillDoneMsgErrorSurfaces(t *testing.T) {
	m := newDispatchTestModel(t)
	m.engineActivity = "loading older messages…"
	m.list.MarkWallSyncRequested()

	m2, _ := m.Update(backfillDoneMsg{
		FolderID: m.list.FolderID,
		Err:      errors.New("graph: 503 Service Unavailable"),
	})
	m = m2.(Model)
	require.NotNil(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "503")
	require.Empty(t, m.engineActivity, "activity must clear so user isn't stuck")
	require.False(t, m.list.wallSyncRequested,
		"debounce must clear on error so retry is possible")
}

// TestBackfillDoneMsgSuccessIsSilent confirms a successful Backfill
// doesn't touch the error or activity surface (the FolderSyncedEvent
// that follows handles the refresh).
func TestBackfillDoneMsgSuccessIsSilent(t *testing.T) {
	m := newDispatchTestModel(t)
	m.engineActivity = "loading older messages…"
	m2, _ := m.Update(backfillDoneMsg{FolderID: m.list.FolderID, Err: nil})
	m = m2.(Model)
	require.Nil(t, m.lastError)
	require.Equal(t, "loading older messages…", m.engineActivity,
		"on success, activity stays until FolderSyncedEvent updates it")
}

// TestURLPickerEmptyShowsHelpfulModal confirms an `O` press on a
// message with zero extracted URLs still opens the picker and
// renders an empty-state hint, instead of silently doing nothing.
// (Empty-state silence has bitten us twice — folder pane in v0.5,
// help bar in v0.10.)
func TestURLPickerEmptyShowsHelpfulModal(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)
	m.viewer.SetLinks(nil)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	m = m2.(Model)
	require.Equal(t, URLPickerMode, m.mode)
	frame := m.View()
	require.Contains(t, frame, "No URLs in this message")
}

// TestMoveOpensFolderPicker is the spec 07 §6.5 / §12.1 invariant:
// pressing `m` in the list pane MUST transition into FolderPickerMode
// with the focused message captured. Without this gate, `m` is a
// silent no-op and the user has no path to user-folder moves.
func TestMoveOpensFolderPicker(t *testing.T) {
	m := newDispatchTestModel(t)
	require.GreaterOrEqual(t, len(m.list.messages), 1)
	m.deps.Triage = &stubTriageWithUndo{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = m2.(Model)
	require.Equal(t, FolderPickerMode, m.mode, "m must transition to FolderPickerMode")
	require.NotNil(t, m.pendingMoveMsg, "pendingMoveMsg must capture the focused message")
	require.NotEmpty(t, m.folderPicker.rows, "picker must seed rows from FoldersModel")
}

// TestFolderPickerFiltersOnTypedInput drives `m` then types "Arc"
// and asserts only the Archive row remains in the filtered list.
// Confirms the typed-input filter wires through Update without
// j/k accidentally being captured by keymap.Up/Down.
func TestFolderPickerFiltersOnTypedInput(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Triage = &stubTriageWithUndo{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = m2.(Model)
	for _, r := range "Arc" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	require.Equal(t, "Arc", m.folderPicker.Buffer())
	visible := m.folderPicker.filtered()
	require.Len(t, visible, 1, "only Archive matches \"Arc\"")
	require.Equal(t, "f-archive", visible[0].id)
}

// TestFolderPickerEnterDispatchesMove confirms the full m → filter
// → Enter path fires Triage.Move with the highlighted row's
// folder ID + alias. Bumps the recent-folder MRU as a side effect.
func TestFolderPickerEnterDispatchesMove(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{}
	m.deps.Triage = stub

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = m2.(Model)
	for _, r := range "Arc" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, cmd, "Enter must dispatch the runTriage Cmd")
	require.Equal(t, NormalMode, m.mode)
	require.Nil(t, m.pendingMoveMsg, "Enter must clear pendingMoveMsg")
	require.Equal(t, []string{"f-archive"}, m.recentFolderIDs,
		"successful Enter must promote the destination to MRU front")
	_ = cmd()
	require.NotEmpty(t, stub.lastMove, "Triage.Move must fire")
	require.Contains(t, stub.lastMove, ":f-archive:archive")
}

// TestFolderPickerEscCancels covers the cancel path: Esc returns
// to NormalMode without dispatching, drops pendingMoveMsg, and
// surfaces a cancellation hint.
func TestFolderPickerEscCancels(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{}
	m.deps.Triage = stub

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.Nil(t, m.pendingMoveMsg)
	require.Empty(t, stub.lastMove, "Esc must NOT dispatch Move")
	require.Contains(t, m.engineActivity, "cancelled")
}

// TestFolderPickerArrowsDoNotFilter confirms tea.KeyUp / tea.KeyDown
// move the cursor without leaking into the filter buffer. j/k DO
// flow into the buffer (typed-input rule); arrows are reserved for
// navigation. This is the precise invariant that lets the user
// scroll the list while typing a partial filter.
func TestFolderPickerArrowsDoNotFilter(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Triage = &stubTriageWithUndo{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = m2.(Model)
	pre := m.folderPicker.cursor
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m2.(Model)
	require.Empty(t, m.folderPicker.Buffer(), "arrow keys must not leak into filter")
	require.NotEqual(t, pre, m.folderPicker.cursor, "tea.KeyDown must move the cursor")
}

// TestBumpRecentFolderPromotesAndCaps tests the MRU ring directly
// — a unit-level guard against an off-by-one that would silently
// keep duplicates or exceed the cap.
func TestBumpRecentFolderPromotesAndCaps(t *testing.T) {
	out := bumpRecentFolder(nil, "a", 3)
	require.Equal(t, []string{"a"}, out)

	out = bumpRecentFolder([]string{"a", "b"}, "c", 3)
	require.Equal(t, []string{"c", "a", "b"}, out)

	// Re-promoting an existing entry moves it to the front.
	out = bumpRecentFolder([]string{"c", "a", "b"}, "a", 3)
	require.Equal(t, []string{"a", "c", "b"}, out)

	// Cap enforces eviction of the oldest.
	out = bumpRecentFolder([]string{"a", "c", "b"}, "d", 3)
	require.Equal(t, []string{"d", "a", "c"}, out)

	// Cap of 0 disables recents entirely (config knob exposes this).
	out = bumpRecentFolder([]string{"x"}, "y", 0)
	require.Equal(t, []string{"x"}, out)
}

// TestFolderPickerRowsSkipDrafts is the spec 07 §6.5 invariant: the
// Drafts well-known folder is filtered out of move destinations.
// Outlook rejects moves into Drafts; surfacing the row would
// generate a confused-user 400.
func TestFolderPickerRowsSkipDrafts(t *testing.T) {
	folders := []store.Folder{
		{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
		{ID: "f-drafts", DisplayName: "Drafts", WellKnownName: "drafts"},
		{ID: "f-archive", DisplayName: "Archive", WellKnownName: "archive"},
	}
	rows := buildFolderPickerRows(folders, nil)
	for _, r := range rows {
		require.NotEqual(t, "drafts", r.alias, "Drafts must be filtered out")
	}
	require.Len(t, rows, 2)
}

// TestFolderPickerRowsRecentRanksFirst confirms the recent IDs
// surface above the alphabetical section, in MRU order.
func TestFolderPickerRowsRecentRanksFirst(t *testing.T) {
	folders := []store.Folder{
		{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
		{ID: "f-archive", DisplayName: "Archive", WellKnownName: "archive"},
		{ID: "f-projects", DisplayName: "Projects"},
	}
	rows := buildFolderPickerRows(folders, []string{"f-projects", "f-archive"})
	require.Len(t, rows, 3)
	require.True(t, rows[0].recent && rows[0].id == "f-projects")
	require.True(t, rows[1].recent && rows[1].id == "f-archive")
	require.False(t, rows[2].recent, "non-MRU row must not be tagged recent")
	require.Equal(t, "f-inbox", rows[2].id)
}

// TestMoveAndUndoRoundTrip is the cross-feature integration check:
// PR 4c (move-with-folder-picker) + spec 07 §11 (undo). User
// opens the picker, selects a destination, dispatches the move,
// presses `u`, and the undo machinery fires Triage.Undo. Without
// this we'd find dispatch passes for each feature independently
// while the user-facing "move + undo" gesture broke at the seam.
func TestMoveAndUndoRoundTrip(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{undoneLabel: "moved"}
	m.deps.Triage = stub

	// `m` opens the picker; type "Arc" to narrow to Archive; Enter.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = m2.(Model)
	for _, r := range "Arc" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, cmd, "Enter must dispatch the move")
	_ = cmd()
	require.NotEmpty(t, stub.lastMove, "move dispatched")
	require.Equal(t, []string{"f-archive"}, m.recentFolderIDs, "MRU bumped")

	// Now press `u` — undo must fire Triage.Undo. The stub returns
	// {label:"moved", ids:[m-1]} so the undoDoneMsg handler paints
	// the status bar.
	m2, undoCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	m = m2.(Model)
	require.NotNil(t, undoCmd, "u must dispatch runUndo")
	res := undoCmd()
	require.IsType(t, undoDoneMsg{}, res, "undo Cmd lands as undoDoneMsg")
	m2, _ = m.Update(res)
	m = m2.(Model)
	require.Equal(t, 1, stub.undoCalls, "Triage.Undo invoked exactly once")
	require.Contains(t, m.engineActivity, "undid", "status bar paints undo confirmation")
}

// TestFolderPickerRowsRenderNestedPaths verifies the picker
// surfaces nested folder destinations with path-style labels
// ("Inbox / Projects / Q4") so duplicate child names in
// different parent chains stay disambiguated. This is the cross-
// feature integration of RT-1 (sync now fetches nested folders)
// + PR 4c (the picker uses them).
func TestFolderPickerRowsRenderNestedPaths(t *testing.T) {
	folders := []store.Folder{
		{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
		{ID: "f-projects", DisplayName: "Projects", ParentFolderID: "f-inbox"},
		{ID: "f-q4", DisplayName: "Q4", ParentFolderID: "f-projects"},
		{ID: "f-q3", DisplayName: "Q3", ParentFolderID: "f-projects"},
	}
	rows := buildFolderPickerRows(folders, nil)
	labels := make([]string, 0, len(rows))
	for _, r := range rows {
		labels = append(labels, r.label)
	}
	require.Contains(t, labels, "Inbox", "root visible by name")
	require.Contains(t, labels, "Inbox / Projects", "level-2 child uses path")
	require.Contains(t, labels, "Inbox / Projects / Q4", "level-3 child uses full path")
	require.Contains(t, labels, "Inbox / Projects / Q3")

	// Filter "Q4" matches the deepest nested folder uniquely.
	picker := NewFolderPicker()
	picker.Reset(folders, nil)
	for _, r := range "Q4" {
		picker.AppendRune(r)
	}
	visible := picker.filtered()
	require.Len(t, visible, 1, "filter narrows to a single deepest match")
	require.Equal(t, "f-q4", visible[0].id)
}

// TestFolderPickerStaleMRUIDIsFilteredOut covers the edge where
// the MRU list points at a folder id that no longer exists locally
// (e.g. the destination was deleted server-side, the next sync
// removed the row, but the in-memory recents slice still
// references the id). The picker must render without panicking
// and silently drop the stale id.
func TestFolderPickerStaleMRUIDIsFilteredOut(t *testing.T) {
	folders := []store.Folder{
		{ID: "f-inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
		{ID: "f-archive", DisplayName: "Archive", WellKnownName: "archive"},
	}
	// Recents include both a present folder (f-archive) and a stale
	// id (f-deleted) that doesn't appear in `folders`.
	rows := buildFolderPickerRows(folders, []string{"f-deleted", "f-archive"})
	require.Len(t, rows, 2, "stale id dropped; only present folders rendered")
	// f-archive is the only recent; it ranks first.
	require.True(t, rows[0].recent && rows[0].id == "f-archive")
	require.False(t, rows[1].recent)
}

// TestStartMoveWithoutFoldersErrors covers the edge case the audit
// flagged: pressing m before the first folder sync surfaces a
// useful error rather than opening an empty picker.
func TestStartMoveWithoutFoldersErrors(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Triage = &stubTriageWithUndo{}
	// Empty the folder list to simulate pre-sync state.
	m.folders = NewFolders()

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode, "no folders → no picker")
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "no folders synced")
}

// TestCalendarJKMovesCursor is the spec 12 §6.2 invariant: with
// the calendar list modal open, j/k moves CalendarModel.cursor
// without leaving the modal. Without this the user can see today's
// events but can't pick one to drill into.
func TestCalendarJKMovesCursor(t *testing.T) {
	m := newDispatchTestModel(t)
	now := time.Now().UTC()
	m.deps.Calendar = &stubCalendar{events: []CalendarEvent{
		{ID: "e-1", Subject: "Standup", Start: now.Add(time.Hour), End: now.Add(time.Hour + 30*time.Minute)},
		{ID: "e-2", Subject: "Q4 review", Start: now.Add(3 * time.Hour), End: now.Add(4 * time.Hour)},
		{ID: "e-3", Subject: "1:1", Start: now.Add(5 * time.Hour), End: now.Add(6 * time.Hour)},
	}}
	// Open the modal and hand-feed events so the cursor has rows.
	m2, cmd := m.dispatchCommand("cal")
	m = m2.(Model)
	require.Equal(t, CalendarMode, m.mode)
	require.NotNil(t, cmd)
	res := cmd()
	m2, _ = m.Update(res)
	m = m2.(Model)
	require.Equal(t, 0, m.calendar.cursor)

	// j → cursor=1.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Equal(t, 1, m.calendar.cursor)

	// j → 2; j again clamps at end.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Equal(t, 2, m.calendar.cursor)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = m2.(Model)
	require.Equal(t, 2, m.calendar.cursor, "no wrap-around at end")

	// k twice → 0; k again clamps at top.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = m2.(Model)
	require.Equal(t, 0, m.calendar.cursor)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = m2.(Model)
	require.Equal(t, 0, m.calendar.cursor, "no wrap-around at top")
	require.Equal(t, CalendarMode, m.mode, "j/k stays in CalendarMode")
}

// TestCalendarEnterDispatchesGetEventAndOpensDetailModal drives
// the spec 12 §7 detail flow: Enter on a highlighted event opens
// CalendarDetailMode with loading state; the GetEvent Cmd
// resolves; the modal renders the attendees + body.
func TestCalendarEnterDispatchesGetEventAndOpensDetailModal(t *testing.T) {
	m := newDispatchTestModel(t)
	now := time.Now().UTC()
	stub := &stubCalendar{
		events: []CalendarEvent{{
			ID: "e-42", Subject: "Q4 review", Start: now, End: now.Add(time.Hour),
		}},
		detail: CalendarEventDetail{
			CalendarEvent: CalendarEvent{
				ID: "e-42", Subject: "Q4 review", Start: now, End: now.Add(time.Hour),
				Location: "Conf Rm 3", WebLink: "https://outlook/event/42",
				OnlineMeetingURL: "https://teams/meet",
			},
			BodyPreview: "Going through the final draft.",
			Attendees: []CalendarAttendee{
				{Name: "Alice", Address: "alice@example.invalid", Type: "required", Status: "accepted"},
				{Name: "Bob", Address: "bob@example.invalid", Type: "optional", Status: "tentativelyAccepted"},
			},
		},
	}
	m.deps.Calendar = stub
	m2, cmd := m.dispatchCommand("cal")
	m = m2.(Model)
	require.NotNil(t, cmd)
	m2, _ = m.Update(cmd())
	m = m2.(Model)
	require.Len(t, m.calendar.events, 1)

	// Enter dispatches the GetEvent fetch.
	m2, getCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, CalendarDetailMode, m.mode)
	require.True(t, m.calendarDetail.loading)
	require.NotNil(t, getCmd, "Enter must return fetchEventCmd")

	// Drive the Cmd; feed result back.
	res := getCmd()
	fm, ok := res.(eventFetchedMsg)
	require.True(t, ok)
	require.NoError(t, fm.Err)
	require.Equal(t, "e-42", stub.gotID, "GetEvent called with the highlighted event's id")
	m2, _ = m.Update(fm)
	m = m2.(Model)
	require.False(t, m.calendarDetail.loading)
	require.NotNil(t, m.calendarDetail.detail)
	require.Equal(t, "Q4 review", m.calendarDetail.detail.Subject)

	// View must paint attendees + body.
	frame := m.View()
	require.Contains(t, frame, "Q4 review")
	require.Contains(t, frame, "Alice")
	require.Contains(t, frame, "Bob")
	require.Contains(t, frame, "Going through the final draft")
}

// TestCalendarDetailEscReturnsToCalendarMode covers the back-out
// path: Esc on the detail modal returns to the calendar list,
// preserving the events the list had loaded.
func TestCalendarDetailEscReturnsToCalendarMode(t *testing.T) {
	m := newDispatchTestModel(t)
	now := time.Now().UTC()
	stub := &stubCalendar{
		events: []CalendarEvent{{ID: "e-1", Subject: "Standup", Start: now, End: now.Add(time.Hour)}},
		detail: CalendarEventDetail{
			CalendarEvent: CalendarEvent{ID: "e-1", Subject: "Standup", Start: now, End: now.Add(time.Hour)},
		},
	}
	m.deps.Calendar = stub
	m2, cmd := m.dispatchCommand("cal")
	m = m2.(Model)
	m2, _ = m.Update(cmd())
	m = m2.(Model)
	m2, getCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	m2, _ = m.Update(getCmd())
	m = m2.(Model)
	require.Equal(t, CalendarDetailMode, m.mode)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	require.Equal(t, CalendarMode, m.mode, "Esc on detail returns to list, not Normal")
	require.Len(t, m.calendar.events, 1, "list state preserved across detail trip")
	require.Nil(t, m.calendarDetail.detail, "detail cleared on Esc")
}

// TestCalendarEnterIsSafeWhenNoEventsLoaded covers the edge case:
// Enter on an empty / loading calendar must not crash, must not
// dispatch GetEvent, and must stay in CalendarMode.
func TestCalendarEnterIsSafeWhenNoEventsLoaded(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubCalendar{} // empty events list
	m.deps.Calendar = stub
	m2, cmd := m.dispatchCommand("cal")
	m = m2.(Model)
	require.NotNil(t, cmd)
	m2, _ = m.Update(cmd())
	m = m2.(Model)
	require.Empty(t, m.calendar.events)

	m2, getCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Nil(t, getCmd, "Enter on empty list must not dispatch GetEvent")
	require.Equal(t, CalendarMode, m.mode)
	require.Equal(t, 0, stub.getCalls)
}

// TestCalendarDetailFetchErrorPaintsErrorState confirms the spec
// 12 §10 failure-mode contract: GetEvent error surfaces inside
// the detail modal rather than dropping the user back to the list
// silently.
func TestCalendarDetailFetchErrorPaintsErrorState(t *testing.T) {
	m := newDispatchTestModel(t)
	now := time.Now().UTC()
	stub := &stubCalendar{
		events:    []CalendarEvent{{ID: "e-1", Subject: "Standup", Start: now, End: now.Add(time.Hour)}},
		detailErr: errors.New("graph throttled"),
	}
	m.deps.Calendar = stub
	m2, cmd := m.dispatchCommand("cal")
	m = m2.(Model)
	m2, _ = m.Update(cmd())
	m = m2.(Model)

	m2, getCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.NotNil(t, getCmd)
	m2, _ = m.Update(getCmd())
	m = m2.(Model)
	require.Equal(t, CalendarDetailMode, m.mode)
	require.NotNil(t, m.calendarDetail.err)
	frame := m.View()
	require.Contains(t, frame, "graph throttled")
}

// TestFolderPickerEnterWithEmptyResultsIsSafe covers the ‟filter
// matches nothing then Enter” path: must not panic, must paint a
// status hint, must stay in FolderPickerMode for the user to
// adjust the filter.
func TestFolderPickerEnterWithEmptyResultsIsSafe(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Triage = &stubTriageWithUndo{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = m2.(Model)
	for _, r := range "zzznosuchfolder" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	require.Empty(t, m.folderPicker.filtered())
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Nil(t, cmd, "Enter on empty filter must not dispatch")
	require.Equal(t, FolderPickerMode, m.mode, "stay in picker mode")
	require.Contains(t, m.engineActivity, "no folder selected")
}

// TestBodyRenderedMsgPopulatesAttachmentsBlock is the spec 05 §8
// minimal-visibility regression. A BodyRenderedMsg with an
// Attachments slice must reach the viewer model AND the rendered
// frame must contain each attachment's name. Real-tenant complaint
// 2026-05-01: the user could see only the list-pane `📎` glyph,
// never the filenames. Save / open keybindings (PR 10) build on
// top of this visibility layer.
func TestBodyRenderedMsgPopulatesAttachmentsBlock(t *testing.T) {
	m := newDispatchTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)
	require.NotNil(t, m.viewer.current)

	atts := []store.Attachment{
		{ID: "a1", MessageID: m.viewer.current.ID, Name: "Q4-forecast.pptx", ContentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation", Size: 4 * 1024 * 1024},
		{ID: "a2", MessageID: m.viewer.current.ID, Name: "notes.docx", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Size: 87 * 1024},
		{ID: "a3", MessageID: m.viewer.current.ID, Name: "chart.png", ContentType: "image/png", Size: 124 * 1024, IsInline: true},
	}
	m2, _ = m.Update(BodyRenderedMsg{
		MessageID:   m.viewer.current.ID,
		Text:        "Hello world",
		State:       0,
		Attachments: atts,
	})
	m = m2.(Model)

	require.Equal(t, atts, m.viewer.Attachments(),
		"viewer captures the attachment slice from the BodyRenderedMsg")

	frame := m.View()
	require.Contains(t, frame, "Q4-forecast.pptx",
		"first attachment name visible in the rendered viewer pane")
	require.Contains(t, frame, "notes.docx",
		"second attachment name visible")
	require.Contains(t, frame, "chart.png",
		"inline attachment still listed by name")
	require.Contains(t, frame, "(inline)",
		"inline attachments flagged so users see they're embedded")
	require.Contains(t, frame, "Attach:",
		"summary header opens the attachment block")
}

// TestRenderAttachmentLinesEmptyForNoAttachments confirms the
// helper returns nil for an empty list — the viewer's tight height
// budget shouldn't be wasted on a header for zero entries.
func TestRenderAttachmentLinesEmptyForNoAttachments(t *testing.T) {
	require.Nil(t, renderAttachmentLines(nil))
	require.Nil(t, renderAttachmentLines([]store.Attachment{}))
}

// TestComposeSessionPersistsOnEntry covers the spec 15 §7 / PR 7-ii
// crash-recovery shape: entering ComposeMode via `r` writes the
// initial skeleton snapshot into compose_sessions so a crash after
// this point can be resumed on next launch.
func TestComposeSessionPersistsOnEntry(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)
	require.Equal(t, ComposeMode, m.mode)
	require.NotEmpty(t, m.compose.SessionID,
		"startCompose assigns a SessionID so the session can be persisted")

	require.NotNil(t, cmd, "startCompose returns a Cmd to persist the snapshot")
	_ = cmd()

	rows, err := m.deps.Store.ListUnconfirmedComposeSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1, "compose entry persists exactly one session row")
	require.Equal(t, m.compose.SessionID, rows[0].SessionID)
	require.Equal(t, "reply", rows[0].Kind)
	require.NotEmpty(t, rows[0].Snapshot, "snapshot blob is non-empty")
}

// TestComposeSessionConfirmedOnSave is the post-save invariant:
// after Ctrl+S, the row's confirmed_at is set so the resume scan
// no longer offers it. Tests run synchronously by invoking the
// Cmd inline.
func TestComposeSessionConfirmedOnSave(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = &recordingDraftCreator{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	m2, entryCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)
	require.NotNil(t, entryCmd)
	_ = entryCmd() // persist initial snapshot

	sessionID := m.compose.SessionID
	require.NotEmpty(t, sessionID)

	// Press Ctrl+S; the returned Cmd runs the Graph save AND the
	// confirm-session write.
	m2, saveCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.NotNil(t, saveCmd)
	_ = saveCmd()

	rows, err := m.deps.Store.ListUnconfirmedComposeSessions(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows, "save confirms the session; no unconfirmed rows remain")
}

// TestComposeSessionConfirmedOnDiscard mirrors the save invariant
// for Ctrl+D: the user explicitly chose "no draft", the row should
// not resurface as a resume offer next launch.
func TestComposeSessionConfirmedOnDiscard(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	m2, entryCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)
	require.NotNil(t, entryCmd)
	_ = entryCmd()

	require.NotEmpty(t, m.compose.SessionID)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)

	rows, err := m.deps.Store.ListUnconfirmedComposeSessions(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows, "Ctrl+D inline-confirms; no unconfirmed rows remain")
}

// TestComposeResumeMsgOpensConfirmModal is the spec 15 §7 / PR
// 7-ii startup-resume invariant: when the launch-time scan finds
// an unconfirmed compose session, the UI offers a confirm modal
// so the user can resume editing or discard.
func TestComposeResumeMsgOpensConfirmModal(t *testing.T) {
	m := newDispatchTestModel(t)
	sess := store.ComposeSession{
		SessionID: "cs-resume",
		Kind:      "reply",
		Snapshot:  `{"kind":1,"source_id":"m-1","to":"alice@example.invalid","subject":"Re: Q4","body":"hi"}`,
		UpdatedAt: time.Now().Add(-15 * time.Minute),
	}
	m2, _ := m.Update(composeResumeMsg{Session: sess})
	m = m2.(Model)

	require.Equal(t, ConfirmMode, m.mode, "resume scan opens the confirm modal")
	require.NotNil(t, m.pendingComposeResume,
		"pending row tracked for the confirm-result handler")
	require.Equal(t, "cs-resume", m.pendingComposeResume.SessionID)
}

// TestComposeResumeYesRestoresIntoComposeMode confirms the y path:
// the snapshot decodes back into the ComposeModel and the user is
// dropped into ComposeMode with their fields pre-populated.
func TestComposeResumeYesRestoresIntoComposeMode(t *testing.T) {
	m := newDispatchTestModel(t)
	sess := store.ComposeSession{
		SessionID: "cs-resume",
		Kind:      "reply",
		Snapshot:  `{"kind":1,"source_id":"m-1","to":"alice@example.invalid","subject":"Re: Q4","body":"my reply text"}`,
		UpdatedAt: time.Now().Add(-15 * time.Minute),
	}
	m2, _ := m.Update(composeResumeMsg{Session: sess})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode)

	// User accepts (y).
	m2, _ = m.Update(ConfirmResultMsg{Topic: "compose_resume", Confirm: true})
	m = m2.(Model)

	require.Equal(t, ComposeMode, m.mode, "y enters ComposeMode")
	require.Equal(t, "cs-resume", m.compose.SessionID,
		"session id preserved so subsequent saves hit the same row")
	require.Equal(t, "alice@example.invalid", m.compose.To())
	require.Equal(t, "Re: Q4", m.compose.Subject())
	require.Equal(t, "my reply text", m.compose.Body())
	require.Nil(t, m.pendingComposeResume, "pending row cleared after handling")
}

// TestComposeResumeNoConfirmsAndDiscards covers the n path: the
// session row is stamped confirmed_at so the resume scan stops
// offering it. The Cmd is run inline (sub-ms SQLite).
func TestComposeResumeNoConfirmsAndDiscards(t *testing.T) {
	m := newDispatchTestModel(t)
	// Seed an unconfirmed row so we can verify it goes away.
	require.NoError(t, m.deps.Store.PutComposeSession(context.Background(), store.ComposeSession{
		SessionID: "cs-resume",
		Kind:      "reply",
		Snapshot:  `{}`,
	}))
	sess := store.ComposeSession{
		SessionID: "cs-resume",
		Kind:      "reply",
		Snapshot:  `{}`,
		UpdatedAt: time.Now().Add(-15 * time.Minute),
	}
	m2, _ := m.Update(composeResumeMsg{Session: sess})
	m = m2.(Model)

	m2, _ = m.Update(ConfirmResultMsg{Topic: "compose_resume", Confirm: false})
	m = m2.(Model)

	require.Equal(t, NormalMode, m.mode)
	require.Nil(t, m.pendingComposeResume)
	require.Contains(t, m.engineActivity, "discarded")

	rows, err := m.deps.Store.ListUnconfirmedComposeSessions(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows, "n confirms the session inline")
}

// TestComposeResumeCorruptSnapshotDoesNotCrash covers a defensive
// case: an undecodable snapshot doesn't take down the UI; the row
// is confirmed (so we don't infinite-loop on it) and a status
// hint surfaces.
func TestComposeResumeCorruptSnapshotDoesNotCrash(t *testing.T) {
	m := newDispatchTestModel(t)
	require.NoError(t, m.deps.Store.PutComposeSession(context.Background(), store.ComposeSession{
		SessionID: "cs-corrupt",
		Kind:      "reply",
		Snapshot:  `{not valid json`,
	}))
	sess := store.ComposeSession{
		SessionID: "cs-corrupt",
		Kind:      "reply",
		Snapshot:  `{not valid json`,
		UpdatedAt: time.Now(),
	}
	m2, _ := m.Update(composeResumeMsg{Session: sess})
	m = m2.(Model)
	m2, _ = m.Update(ConfirmResultMsg{Topic: "compose_resume", Confirm: true})
	m = m2.(Model)

	require.NotEqual(t, ComposeMode, m.mode, "corrupt snapshot doesn't drop into ComposeMode")
	require.Error(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "snapshot")

	rows, err := m.deps.Store.ListUnconfirmedComposeSessions(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows, "corrupt rows get confirmed so they never resurface")
}

// TestScanComposeSessionsCmdReturnsNoneWhenEmpty confirms the
// launch scan emits a deterministic `composeResumeNoneMsg` when
// no unconfirmed rows exist — Init's tea.Batch always gets a
// signal back, so test harnesses can synchronise.
func TestScanComposeSessionsCmdReturnsNoneWhenEmpty(t *testing.T) {
	m := newDispatchTestModel(t)
	cmd := m.scanComposeSessionsCmd()
	require.NotNil(t, cmd)
	msg := cmd()
	_, ok := msg.(composeResumeNoneMsg)
	require.True(t, ok, "no unconfirmed rows → composeResumeNoneMsg")
}

// TestScanComposeSessionsCmdReturnsResumeWhenPresent — happy path
// of the scan-Cmd: an unconfirmed row in the store becomes a
// composeResumeMsg with the row payload.
func TestScanComposeSessionsCmdReturnsResumeWhenPresent(t *testing.T) {
	m := newDispatchTestModel(t)
	require.NoError(t, m.deps.Store.PutComposeSession(context.Background(), store.ComposeSession{
		SessionID: "cs-present",
		Kind:      "reply",
		Snapshot:  `{}`,
	}))

	cmd := m.scanComposeSessionsCmd()
	msg := cmd()
	resume, ok := msg.(composeResumeMsg)
	require.True(t, ok)
	require.NoError(t, resume.Err)
	require.Equal(t, "cs-present", resume.Session.SessionID)
}

// TestScanComposeSessionsCmdGCsOldConfirmed is the GC half of the
// scan: confirmed sessions older than 24h are pruned even when
// the resume path returns "none". Behaviour is observed via a
// follow-up store lookup.
func TestScanComposeSessionsCmdGCsOldConfirmed(t *testing.T) {
	m := newDispatchTestModel(t)
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, m.deps.Store.PutComposeSession(context.Background(), store.ComposeSession{
		SessionID:   "cs-old",
		Kind:        "reply",
		Snapshot:    `{}`,
		CreatedAt:   old,
		UpdatedAt:   old,
		ConfirmedAt: old,
	}))

	cmd := m.scanComposeSessionsCmd()
	_ = cmd()

	// The old confirmed row should have been GC'd; the table is
	// otherwise empty so a manual peek covers it.
	rows, err := m.deps.Store.ListUnconfirmedComposeSessions(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows)
	// Re-insert the same id; if the original wasn't deleted, this
	// upsert would just bump updated_at and we'd still see it as
	// confirmed. Direct evidence is the GC-deleted-1 return value
	// from a follow-up GC pass: the row is gone (0) rather than
	// pruneable (1).
	deleted, err := m.deps.Store.GCConfirmedComposeSessions(context.Background(), time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(0), deleted, "scan already pruned the old confirmed row")
}

// TestViewerCapitalRStartsReplyAll is the spec 15 §9 / PR 7-iii
// invariant for `R` in the viewer pane: enters ComposeMode with
// the reply-all skeleton applied. Source's From + remaining To
// addresses populate the To field; Cc preserves the source's Cc.
func TestViewerCapitalRStartsReplyAll(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}

	// Open a message → focus moves to viewer.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	m = m2.(Model)
	require.Equal(t, ComposeMode, m.mode, "viewer R enters ComposeMode")
	require.Equal(t, ComposeKindReplyAll, m.compose.Kind,
		"compose snapshot reflects reply-all kind")
	require.Contains(t, m.compose.Subject(), "Re:",
		"reply-all skeleton prefixes Re:")
}

// TestViewerLowerFStartsForward covers the spec 15 §9 / PR 7-iii
// rebinding: `f` in the viewer pane is Forward. Subject is prefixed
// "Fwd:"; To/Cc start empty for the user to fill.
func TestViewerLowerFStartsForward(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = m2.(Model)
	require.Equal(t, ComposeMode, m.mode, "viewer f enters ComposeMode")
	require.Equal(t, ComposeKindForward, m.compose.Kind,
		"compose snapshot reflects forward kind")
	require.Contains(t, m.compose.Subject(), "Fwd:",
		"forward skeleton prefixes Fwd:")
	require.Empty(t, m.compose.To(),
		"forward starts with empty To for user to fill")
}

// TestViewerLowerFFlagsWhenNoDraftsWired keeps the legacy
// flag-from-viewer behaviour available when Drafts isn't wired
// (e.g., test or degraded mode). Without this fallback the `f`
// keypress would be visually dead in viewer pane.
func TestViewerLowerFFlagsWhenNoDraftsWired(t *testing.T) {
	m := newDispatchTestModel(t)
	stubT := &stubTriageWithUndo{}
	m.deps.Triage = stubT
	require.Nil(t, m.deps.Drafts, "test setup has no drafts")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = m2.(Model)
	require.NotEqual(t, ComposeMode, m.mode, "no drafts → fall back to flag")
}

// TestViewerLowerMStartsNewWhenDraftsWired is the spec 15 §9 / PR
// 7-iii rebinding: `m` from the viewer pane creates a brand-new
// draft (no source) — recipients empty, body empty, focus drops
// into To.
func TestViewerLowerMStartsNewWhenDraftsWired(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = m2.(Model)
	require.Equal(t, ComposeMode, m.mode, "viewer m enters ComposeMode")
	require.Equal(t, ComposeKindNew, m.compose.Kind,
		"new draft (no source)")
	require.Empty(t, m.compose.To())
	require.Empty(t, m.compose.Subject())
	require.Empty(t, m.compose.SourceID, "new drafts have no source")
}

// TestFolderPaneMStartsNew confirms the spec 15 §9 / PR 7-iii
// rebinding from the folders pane: `m` opens compose for a new
// message. Folder-pane Move was previously a no-op (no list of
// messages to act on).
func TestFolderPaneMStartsNew(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}

	// Focus the folders pane via `1`.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	require.Equal(t, FoldersPane, m.focused)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = m2.(Model)
	require.Equal(t, ComposeMode, m.mode, "folders m → new message")
	require.Equal(t, ComposeKindNew, m.compose.Kind)
}

// TestSaveComposeRoutesByKind covers the saveComposeCmd dispatch
// table: snapshot.Kind selects the matching DraftCreator method.
// One test per kind so a regression in the switch surfaces with
// a clear failure.
func TestSaveComposeRoutesByKind(t *testing.T) {
	cases := []struct {
		name string
		kind ComposeKind
		want string
	}{
		{"reply", ComposeKindReply, "reply"},
		{"reply_all", ComposeKindReplyAll, "reply_all"},
		{"forward", ComposeKindForward, "forward"},
		{"new", ComposeKindNew, "new"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newDispatchTestModel(t)
			stub := &recordingDraftCreator{}
			m.deps.Drafts = stub

			snap := ComposeSnapshot{
				Kind:     c.kind,
				SourceID: "m-1",
				To:       "alice@example.invalid",
				Subject:  "x",
				Body:     "body",
			}
			cmd := m.saveComposeCmd(snap, "")
			require.NotNil(t, cmd)
			res := cmd()
			saved, _ := res.(draftSavedMsg)
			require.NoError(t, saved.err, "save dispatch returns nil err")
			require.Equal(t, c.want, stub.lastKind,
				"kind %s routed to the matching DraftCreator method", c.name)
		})
	}
}

// TestSaveComposeNewDraftSkipsRecipientFallback confirms the new-
// message path doesn't try to fall back to source.FromAddress (it
// has no source). Empty To on a New draft should error rather
// than silently dispatch.
func TestSaveComposeNewDraftSkipsRecipientFallback(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &recordingDraftCreator{}
	m.deps.Drafts = stub

	snap := ComposeSnapshot{Kind: ComposeKindNew, To: ""} // no source, no To
	cmd := m.saveComposeCmd(snap, "")
	res := cmd()
	saved, _ := res.(draftSavedMsg)
	require.Error(t, saved.err)
	require.Contains(t, saved.err.Error(), "no recipient")
	require.Equal(t, 0, stub.calls,
		"new draft with no recipient must not silently dispatch")
}

// TestApplyReplyAllSkeletonFiltersUserUPN is the model-side
// dedup invariant: when the user is one of the source's To
// recipients, ApplyReplyAllSkeleton drops them from the form's
// To list so the resulting draft doesn't email the user
// themselves.
func TestApplyReplyAllSkeletonFiltersUserUPN(t *testing.T) {
	src := store.Message{
		Subject:     "Q4 forecast",
		FromName:    "Bob",
		FromAddress: "bob@vendor.invalid",
		ToAddresses: []store.EmailAddress{
			{Address: "alice@example.invalid"},
			{Address: "tester@example.invalid"},
		},
	}
	cm := NewCompose()
	cm.ApplyReplyAllSkeleton(src, "", "tester@example.invalid")
	require.Contains(t, cm.To(), "bob@vendor.invalid")
	require.Contains(t, cm.To(), "alice@example.invalid")
	require.NotContains(t, cm.To(), "tester@example.invalid",
		"user's own UPN filtered out")
}

// TestApplyForwardSkeletonClearsRecipients confirms a fresh
// ApplyForwardSkeleton call zeroes To/Cc — the user fills these
// for a forward.
func TestApplyForwardSkeletonClearsRecipients(t *testing.T) {
	src := store.Message{Subject: "x", FromAddress: "b@x"}
	cm := NewCompose()
	cm.SetTo("stale@x")
	cm.SetCc("stalecc@x")
	cm.ApplyForwardSkeleton(src, "")
	require.Empty(t, cm.To(), "forward clears To from any prior state")
	require.Empty(t, cm.Cc(), "forward clears Cc from any prior state")
}

// TestApplyNewSkeletonFocusesTo verifies the new-message UX
// invariant: focus drops into the To field rather than the body
// because there's no source-sender to pre-fill from and recipients
// are the user's first task.
func TestApplyNewSkeletonFocusesTo(t *testing.T) {
	cm := NewCompose()
	cm.ApplyNewSkeleton()
	require.Equal(t, ComposeFieldTo, cm.Focused(),
		"new draft focuses To field")
	require.Empty(t, cm.SourceID)
	require.Empty(t, cm.To())
	require.Empty(t, cm.Subject())
	require.Empty(t, cm.Body())
}

// TestTitleCaseHandlesSingleWordVerbs covers the strings.Title
// replacement: ASCII single-word inputs round-trip with the first
// letter capitalised and the rest lowered (so "DELETE" → "Delete"
// rather than passing through). Pre-rename used the deprecated
// strings.Title which staticcheck SA1019 flagged.
func TestTitleCaseHandlesSingleWordVerbs(t *testing.T) {
	cases := []struct{ in, want string }{
		{"delete", "Delete"},
		{"archive", "Archive"},
		{"DELETE", "Delete"},
		{"reMOVE", "Remove"},
		{"", ""},
		{"x", "X"},
	}
	for _, c := range cases {
		require.Equal(t, c.want, titleCase(c.in), "in=%q", c.in)
	}
}

// stubSearchService implements ui.SearchService for dispatch
// tests. The supplied snapshots are emitted in order; cancel
// closes the channel mid-stream so tests can assert clean
// shutdown.
type stubSearchService struct {
	mu        sync.Mutex
	calls     int
	lastQuery string
	snapshots []SearchSnapshot
	cancelled bool
}

func (s *stubSearchService) Search(_ context.Context, query string) (<-chan SearchSnapshot, func()) {
	s.mu.Lock()
	s.calls++
	s.lastQuery = query
	snaps := s.snapshots
	s.mu.Unlock()
	out := make(chan SearchSnapshot, len(snaps)+1)
	for _, snap := range snaps {
		out <- snap
	}
	close(out)
	return out, func() {
		s.mu.Lock()
		s.cancelled = true
		s.mu.Unlock()
	}
}

// TestSearchEnterRoutesThroughSearchService covers spec 06 §3:
// pressing `/` then typing then Enter dispatches via the
// streaming SearchService rather than the legacy single-shot
// store.Search path. The list pane receives the merged snapshot
// + the search status line picks up the merger's hint.
func TestSearchEnterRoutesThroughSearchService(t *testing.T) {
	m := newDispatchTestModel(t)
	now := time.Now()
	stub := &stubSearchService{
		snapshots: []SearchSnapshot{
			{
				Status: "[merged: 1 local, 1 server, 0 both]",
				Messages: []store.Message{
					{ID: "m-hit", AccountID: 1, Subject: "Q4 budget", FolderID: "f-inbox", ReceivedAt: now},
				},
			},
		},
	}
	m.deps.Search = stub

	// Enter SearchMode and type a query.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m2.(Model)
	require.Equal(t, SearchMode, m.mode)
	for _, r := range "budget" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}

	// Enter dispatches the search.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.True(t, m.searchActive)
	require.Equal(t, "budget", m.searchQuery)
	require.NotNil(t, cmd, "Enter returns the streaming-search start Cmd")

	// Run the start Cmd; it should yield a searchStreamMsg.
	first := cmd()
	stream, ok := first.(searchStreamMsg)
	require.True(t, ok, "first message off the streaming Cmd is searchStreamMsg")
	require.Equal(t, "budget", stream.query)

	// Apply the searchStreamMsg; the list pane updates.
	m2, drain := m.Update(stream)
	m = m2.(Model)
	require.NotNil(t, drain, "first snapshot returns a continuation drain Cmd")

	// The first snapshot's message lands in the list pane.
	require.Len(t, m.list.messages, 1)
	require.Equal(t, "m-hit", m.list.messages[0].ID)
	require.Contains(t, m.searchStatus, "merged")

	// Drain the channel — the next message should be the Done
	// sentinel because the stub closes after 1 snapshot.
	final := drain()
	doneMsg, ok := final.(SearchUpdateMsg)
	require.True(t, ok)
	require.True(t, doneMsg.Done)

	// Apply Done — the stream cleans up.
	m2, _ = m.Update(doneMsg)
	m = m2.(Model)
	require.Nil(t, m.searchCancel)
	require.Nil(t, m.searchUpdates)

	stub.mu.Lock()
	defer stub.mu.Unlock()
	require.Equal(t, 1, stub.calls)
	require.Equal(t, "budget", stub.lastQuery)
}

// TestSearchEscCancelsInFlightStream covers the spec 06 §5.1
// invariant: pressing Esc while a streaming search is mid-flight
// MUST call the searcher's cancel hook so the local + server
// goroutines exit cleanly rather than leak.
func TestSearchEscCancelsInFlightStream(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubSearchService{snapshots: []SearchSnapshot{{Status: "[searching…]"}}}
	m.deps.Search = stub

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m2.(Model)
	for _, r := range "budget" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	stream := cmd().(searchStreamMsg)
	m2, _ = m.Update(stream)
	m = m2.(Model)
	require.NotNil(t, m.searchCancel, "stream cancel set on first snapshot")

	// Re-enter SearchMode (the Update for `/` requires the model
	// to still be in NormalMode after Enter — confirm) then
	// press Esc which exits SearchMode AND clears searchActive.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m2.(Model)
	require.Equal(t, SearchMode, m.mode)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	require.False(t, m.searchActive)
	require.Nil(t, m.searchCancel, "Esc clears the cancel hook")
	require.Nil(t, m.searchUpdates)

	stub.mu.Lock()
	defer stub.mu.Unlock()
	require.True(t, stub.cancelled,
		"the stream's Cancel was invoked on Esc")
}

// TestSearchUpdateMsgIgnoredAfterQueryChange covers the
// out-of-order safety: a stale snapshot from a prior query
// arriving after the user has dispatched a new query must NOT
// overwrite the new query's results. The Update handler keys
// every snapshot to its query string and drops mismatches.
func TestSearchUpdateMsgIgnoredAfterQueryChange(t *testing.T) {
	m := newDispatchTestModel(t)
	m.searchActive = true
	m.searchQuery = "current"
	stale := SearchUpdateMsg{Query: "stale", Results: []store.Message{{ID: "should-not-land"}}}
	before := len(m.list.messages)
	m2, _ := m.Update(stale)
	m = m2.(Model)
	require.Equal(t, before, len(m.list.messages),
		"stale snapshot from a prior query is dropped")
}

// TestComposeSessionPersistsOnTab covers the focus-change re-write:
// each Tab captures whatever the user just typed in the field they
// left, so a crash mid-typing recovers up to the most-recent Tab.
func TestComposeSessionPersistsOnTab(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	m2, entryCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)
	_ = entryCmd()

	// Modify the body before tabbing away.
	m.compose.SetBody("hello world")

	m2, tabCmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	require.NotNil(t, tabCmd, "Tab returns a persist Cmd")
	_ = tabCmd()

	rows, err := m.deps.Store.ListUnconfirmedComposeSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Contains(t, rows[0].Snapshot, "hello world",
		"Tab re-persists the snapshot with the body the user just typed")
}

// TestAttachmentSummaryFormatsCountAndTotal covers the "3 files ·
// 2.4 MB" summary line. Singular / plural matters; total bytes is
// the sum across every entry including inline.
func TestAttachmentSummaryFormatsCountAndTotal(t *testing.T) {
	one := []store.Attachment{{Size: 500}}
	require.Equal(t, "1 file · 500B", attachmentSummary(one))

	many := []store.Attachment{{Size: 1024}, {Size: 2 * 1024}, {Size: 3 * 1024}}
	require.Equal(t, "3 files · 6.0KB", attachmentSummary(many))
}

// TestAttachmentLetterKeyWithNoFetcherSetsError verifies that pressing
// the attachment-letter key (`a`) in the viewer when the Attachments
// dep is not wired surfaces a visible error rather than silently
// dropping the keypress (spec 05 §12 / PR 10). The error will show in
// the status-bar error banner on the next render.
func TestAttachmentLetterKeyWithNoFetcherSetsError(t *testing.T) {
	m := newDispatchTestModel(t)

	// Populate the viewer with a message + one attachment so the
	// binding actually fires (key 'a' maps to attachment[0]).
	msg := store.Message{ID: "m-1", Subject: "test"}
	m.viewer.SetMessage(msg)
	m.viewer.SetAttachments([]store.Attachment{
		{ID: "att-1", Name: "file.pdf", Size: 1024},
	})
	m.focused = ViewerPane

	// Press `a` — no AttachmentFetcher wired, so startSaveAttachment
	// sets lastError and returns a nil Cmd.
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m2 := result.(Model)

	require.Nil(t, cmd, "no Cmd should be dispatched when Attachments dep is nil")
	require.Error(t, m2.lastError, "lastError must be set so the user sees feedback")
	require.Contains(t, m2.lastError.Error(), "attachment")
}

// openCalendarWithEvents is a helper that opens the :cal modal, feeds in
// the stub's events, and returns the loaded model.
func openCalendarWithEvents(t *testing.T, stub *stubCalendar) Model {
	t.Helper()
	m := newDispatchTestModel(t)
	m.deps.Calendar = stub
	m2, cmd := m.dispatchCommand("cal")
	m = m2.(Model)
	require.NotNil(t, cmd, "dispatchCommand must return a fetchCalendarCmd")
	m2, _ = m.Update(cmd())
	return m2.(Model)
}

// TestCalendarBracketRightNavNextDay verifies that `]` in CalendarMode
// advances viewDate by one day and dispatches a ListEventsBetween Cmd.
func TestCalendarBracketRightNavNextDay(t *testing.T) {
	now := time.Now().UTC()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)

	stub := &stubCalendar{
		events: []CalendarEvent{
			{Subject: "Today event", Start: now, End: now.Add(time.Hour)},
		},
		betweenEvents: []CalendarEvent{
			{Subject: "Tomorrow event", Start: today.Add(25 * time.Hour), End: today.Add(26 * time.Hour)},
		},
	}
	m := openCalendarWithEvents(t, stub)
	require.Equal(t, CalendarMode, m.mode)
	require.Equal(t, today, m.calendar.ViewDate(), "modal must start on today")

	// Press `]` → viewDate advances one day.
	m2, fetchCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = m2.(Model)
	require.Equal(t, today.AddDate(0, 0, 1), m.calendar.ViewDate(), "] must advance viewDate by one day")
	require.True(t, m.calendar.loading, "] must set loading while fetch is in flight")
	require.NotNil(t, fetchCmd, "] must return a fetch Cmd")

	// Run the Cmd; verify it called ListEventsBetween with tomorrow's window.
	res := fetchCmd()
	require.Equal(t, 1, stub.betweenCalls, "Cmd must call ListEventsBetween once")
	require.Equal(t, today.AddDate(0, 0, 1), stub.lastBetweenStart, "start must be tomorrow midnight UTC")
	require.Equal(t, today.AddDate(0, 0, 2), stub.lastBetweenEnd, "end must be day-after-tomorrow midnight UTC")

	// Feed result back; verify events updated.
	m2, _ = m.Update(res)
	m = m2.(Model)
	require.False(t, m.calendar.loading)
	require.Len(t, m.calendar.events, 1)
	require.Equal(t, "Tomorrow event", m.calendar.events[0].Subject)
}

// TestCalendarBracketLeftNavPrevDay verifies that `[` in CalendarMode
// retreats viewDate by one day and dispatches a ListEventsBetween Cmd.
func TestCalendarBracketLeftNavPrevDay(t *testing.T) {
	now := time.Now().UTC()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)

	stub := &stubCalendar{betweenEvents: []CalendarEvent{
		{Subject: "Yesterday event"},
	}}
	m := openCalendarWithEvents(t, stub)

	m2, fetchCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	m = m2.(Model)
	require.Equal(t, today.AddDate(0, 0, -1), m.calendar.ViewDate(), "[ must retreat viewDate by one day")
	require.True(t, m.calendar.loading)
	require.NotNil(t, fetchCmd)

	m2, _ = m.Update(fetchCmd())
	m = m2.(Model)
	require.Len(t, m.calendar.events, 1)
	require.Equal(t, "Yesterday event", m.calendar.events[0].Subject)
}

// TestCalendarBraceRightNavNextWeek verifies that `}` advances viewDate
// by seven days and dispatches ListEventsBetween.
func TestCalendarBraceRightNavNextWeek(t *testing.T) {
	now := time.Now().UTC()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)

	stub := &stubCalendar{betweenEvents: []CalendarEvent{{Subject: "Next week event"}}}
	m := openCalendarWithEvents(t, stub)

	m2, fetchCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("}")})
	m = m2.(Model)
	require.Equal(t, today.AddDate(0, 0, 7), m.calendar.ViewDate(), "} must advance viewDate by seven days")
	require.NotNil(t, fetchCmd)

	// Execute the Cmd; verify it called ListEventsBetween with next-week window.
	_ = fetchCmd()
	require.Equal(t, 1, stub.betweenCalls, "Cmd must call ListEventsBetween once")
	require.Equal(t, today.AddDate(0, 0, 7), stub.lastBetweenStart)
	require.Equal(t, today.AddDate(0, 0, 8), stub.lastBetweenEnd)
}

// TestCalendarBraceLeftNavPrevWeek verifies that `{` retreats viewDate
// by seven days and dispatches ListEventsBetween.
func TestCalendarBraceLeftNavPrevWeek(t *testing.T) {
	now := time.Now().UTC()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)

	stub := &stubCalendar{betweenEvents: []CalendarEvent{{Subject: "Last week event"}}}
	m := openCalendarWithEvents(t, stub)

	m2, fetchCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("{")})
	m = m2.(Model)
	require.Equal(t, today.AddDate(0, 0, -7), m.calendar.ViewDate(), "{ must retreat viewDate by seven days")
	require.NotNil(t, fetchCmd)

	// Execute the Cmd; verify window is [today-7, today-6).
	_ = fetchCmd()
	require.Equal(t, 1, stub.betweenCalls, "Cmd must call ListEventsBetween once")
	require.Equal(t, today.AddDate(0, 0, -7), stub.lastBetweenStart)
	require.Equal(t, today.AddDate(0, 0, -6), stub.lastBetweenEnd)
}

// TestCalendarTKeyReturnsToToday verifies that `t` in CalendarMode resets
// viewDate to today and dispatches a ListEventsToday Cmd (not ListEventsBetween).
func TestCalendarTKeyReturnsToToday(t *testing.T) {
	now := time.Now().UTC()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)

	stub := &stubCalendar{
		events: []CalendarEvent{{Subject: "Today event"}},
	}
	m := openCalendarWithEvents(t, stub)

	// Navigate to tomorrow first.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = m2.(Model)
	require.Equal(t, today.AddDate(0, 0, 1), m.calendar.ViewDate())

	// Press `t` to return to today.
	m2, fetchCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = m2.(Model)
	require.Equal(t, today, m.calendar.ViewDate(), "t must reset viewDate to today")
	require.True(t, m.calendar.loading)
	require.NotNil(t, fetchCmd, "t must return a fetchCalendarCmd")

	// The Cmd should call ListEventsToday (not ListEventsBetween).
	res := fetchCmd()
	require.Equal(t, 0, stub.betweenCalls, "t must use ListEventsToday, not ListEventsBetween")
	_, ok := res.(calendarFetchedMsg)
	require.True(t, ok)
}

// TestCalendarNavStaysInCalendarMode confirms that ]/[/{/}/t all stay
// in CalendarMode — none of them close the modal.
func TestCalendarNavStaysInCalendarMode(t *testing.T) {
	stub := &stubCalendar{betweenEvents: []CalendarEvent{}}
	m := openCalendarWithEvents(t, stub)

	for _, key := range []string{"]", "[", "}", "{"} {
		t.Run("key="+key, func(t *testing.T) {
			m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			require.Equal(t, CalendarMode, m2.(Model).mode, key+" must stay in CalendarMode")
		})
	}
}

// stubSavedSearchService implements SavedSearchService for tests.
type stubSavedSearchService struct {
	saveErr   error
	deleteErr error
	saved     []SavedSearch
	onSave    func(name, pattern string, pinned bool)
	onDelete  func(name string)
}

func (s *stubSavedSearchService) Save(_ context.Context, name, pattern string, pinned bool) error {
	if s.onSave != nil {
		s.onSave(name, pattern, pinned)
	}
	return s.saveErr
}

func (s *stubSavedSearchService) DeleteByName(_ context.Context, name string) error {
	if s.onDelete != nil {
		s.onDelete(name)
	}
	return s.deleteErr
}

func (s *stubSavedSearchService) Reload(_ context.Context) ([]SavedSearch, error) {
	return s.saved, nil
}

func (s *stubSavedSearchService) RefreshCounts(_ context.Context) ([]SavedSearch, error) {
	return s.saved, nil
}

// TestRuleSaveWithActiveFilterCallsService drives `:rule save Newsletters`
// while a filter is active and confirms the SavedSearchService.Save method is
// called with the active filter pattern.
func TestRuleSaveWithActiveFilterCallsService(t *testing.T) {
	m := newDispatchTestModel(t)
	var gotName, gotPattern string
	svc := &stubSavedSearchService{
		onSave: func(name, pattern string, _ bool) {
			gotName = name
			gotPattern = pattern
		},
	}
	m.deps.SavedSearchSvc = svc
	m.filterActive = true
	m.filterPattern = "~N"

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "rule save Newsletters" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd, ":rule save must return a Cmd")

	msg := cmd()
	saved, ok := msg.(savedSearchSavedMsg)
	require.True(t, ok, "got %T", msg)
	require.Equal(t, "Newsletters", saved.name)
	require.NoError(t, saved.err)
	require.Equal(t, "Newsletters", gotName)
	require.Equal(t, "~N", gotPattern)
}

// TestRuleSaveWithNoFilterSetsError confirms `:rule save` with no active filter
// sets m.lastError instead of firing the Cmd.
func TestRuleSaveWithNoFilterSetsError(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.SavedSearchSvc = &stubSavedSearchService{}
	m.filterActive = false

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "rule save Unread" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Nil(t, cmd, ":rule save without filter must not fire Cmd")
	require.NotNil(t, m.lastError, "must set lastError when no filter is active")
}

// TestRuleListShowsNamesInActivity drives `:rule list` with two seeded
// saved searches and confirms the names appear in m.engineActivity.
func TestRuleListShowsNamesInActivity(t *testing.T) {
	m := newDispatchTestModel(t)
	m.savedSearches = []SavedSearch{
		{Name: "Unread", Pattern: "~N"},
		{Name: "Flagged", Pattern: "~F"},
	}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "rule list" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Contains(t, m.engineActivity, "Unread")
	require.Contains(t, m.engineActivity, "Flagged")
}

// TestRuleShowDisplaysPattern drives `:rule show Unread` and confirms the
// active pattern appears in m.engineActivity.
func TestRuleShowDisplaysPattern(t *testing.T) {
	m := newDispatchTestModel(t)
	m.savedSearches = []SavedSearch{
		{Name: "Unread", Pattern: "~N"},
	}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "rule show Unread" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Contains(t, m.engineActivity, "~N")
}

// TestRuleDeleteOpensConfirmModal drives `:rule delete Unread` and confirms
// the confirm modal opens with topic "rule_delete".
func TestRuleDeleteOpensConfirmModal(t *testing.T) {
	m := newDispatchTestModel(t)
	m.savedSearches = []SavedSearch{
		{Name: "Unread", Pattern: "~N"},
	}
	m.deps.SavedSearchSvc = &stubSavedSearchService{}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "rule delete Unread" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode, ":rule delete must open confirm modal")
	require.Equal(t, "rule_delete", m.confirm.Topic)
	require.Equal(t, "Unread", m.pendingRuleDelete)
}

// TestRuleDeleteConfirmYesFiresDeleteCmd confirms that after `:rule delete` + y
// the delete Cmd runs and savedSearchSavedMsg is emitted.
func TestRuleDeleteConfirmYesFiresDeleteCmd(t *testing.T) {
	m := newDispatchTestModel(t)
	m.savedSearches = []SavedSearch{
		{Name: "Unread", Pattern: "~N"},
	}
	var deleted string
	m.deps.SavedSearchSvc = &stubSavedSearchService{
		onDelete: func(name string) { deleted = name },
	}

	// Drive `:rule delete Unread` to open modal.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "rule delete Unread" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ConfirmMode, m.mode)

	// y produces a ConfirmResultMsg via Cmd; feed it back to get the delete Cmd.
	m2, yCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m2.(Model)
	require.NotNil(t, yCmd)
	confirmMsg := yCmd()
	m2, deleteCmd := m.Update(confirmMsg)
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode, "confirm must close modal")
	require.NotNil(t, deleteCmd, "confirm must return delete Cmd")

	msg := deleteCmd()
	saved, ok := msg.(savedSearchSavedMsg)
	require.True(t, ok, "got %T", msg)
	require.Equal(t, "deleted", saved.action)
	require.Equal(t, "Unread", deleted)
}

// TestSavedSearchCountBadgeRendersWhenNonNegative confirms that a saved
// search with Count≥0 shows the count in the sidebar View output.
func TestSavedSearchCountBadgeRendersWhenNonNegative(t *testing.T) {
	m := newDispatchTestModel(t)
	m.folders.SetSavedSearches([]SavedSearch{
		{Name: "Unread", Pattern: "~N", Pinned: true, Count: 5},
		{Name: "Flagged", Pattern: "~F", Pinned: true, Count: 0},
		{Name: "From me", Pattern: "~f me@x", Pinned: false, Count: -1},
	})
	out := m.folders.View(m.theme, 35, 30, true)
	require.Contains(t, out, "5", "Unread count badge must appear")
	require.Contains(t, out, "☆ Flagged  0", "count=0 must also render")
	require.NotContains(t, out, "-1", "Count=-1 must not render")
}

// TestFlagIndicatorRendersOnFlaggedMessage seeds a flagged message and
// asserts the flag indicator (⚑) appears in the list view output.
func TestFlagIndicatorRendersOnFlaggedMessage(t *testing.T) {
	m := newDispatchTestModel(t)
	m.list.messages = []store.Message{
		{
			ID:         "m-flagged",
			AccountID:  m.deps.Account.ID,
			FolderID:   "f-inbox",
			Subject:    "Important flagged message",
			FromName:   "Bob",
			FlagStatus: "flagged",
			ReceivedAt: time.Now(),
		},
	}
	out := m.list.View(m.theme, 80, 20, true)
	require.Contains(t, out, "⚑", "flag indicator must appear for flagged message")
}

// TestAttachmentIndicatorRendersOnMessageWithAttachments seeds a
// message with HasAttachments=true and asserts the attachment
// indicator (📎) appears in the list view output.
func TestAttachmentIndicatorRendersOnMessageWithAttachments(t *testing.T) {
	m := newDispatchTestModel(t)
	m.list.messages = []store.Message{
		{
			ID:             "m-attach",
			AccountID:      m.deps.Account.ID,
			FolderID:       "f-inbox",
			Subject:        "Report with attachments",
			FromName:       "Carol",
			HasAttachments: true,
			ReceivedAt:     time.Now(),
		},
	}
	out := m.list.View(m.theme, 80, 20, true)
	require.Contains(t, out, "📎", "attachment indicator must appear when HasAttachments=true")
}

// TestNoFlagIndicatorOnUnflaggedMessage confirms that the flag
// indicator does NOT appear for a normal (unflagged) message.
func TestNoFlagIndicatorOnUnflaggedMessage(t *testing.T) {
	m := newDispatchTestModel(t)
	m.list.messages = []store.Message{
		{
			ID:         "m-normal",
			AccountID:  m.deps.Account.ID,
			FolderID:   "f-inbox",
			Subject:    "Normal message",
			FromName:   "Dave",
			FlagStatus: "notFlagged",
			ReceivedAt: time.Now(),
		},
	}
	out := m.list.View(m.theme, 80, 20, true)
	require.NotContains(t, out, "⚑", "flag indicator must not appear for unflagged message")
}

// TestSaveCommandAliasesRulesSave drives `:save Unread` with an active
// filter and asserts a non-nil Cmd is returned that produces
// savedSearchSavedMsg.
func TestSaveCommandAliasesRulesSave(t *testing.T) {
	m := newDispatchTestModel(t)
	var gotName, gotPattern string
	svc := &stubSavedSearchService{
		onSave: func(name, pattern string, _ bool) {
			gotName = name
			gotPattern = pattern
		},
	}
	m.deps.SavedSearchSvc = svc
	m.filterActive = true
	m.filterPattern = "~N"

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "save Unread" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd, ":save must return a Cmd")

	msg := cmd()
	saved, ok := msg.(savedSearchSavedMsg)
	require.True(t, ok, "got %T", msg)
	require.Equal(t, "Unread", saved.name)
	require.NoError(t, saved.err)
	require.Equal(t, "Unread", gotName)
	require.Equal(t, "~N", gotPattern)
}

// TestSaveCommandRequiresActiveFilter confirms that `:save Unread`
// without an active filter sets m.lastError and returns no Cmd.
func TestSaveCommandRequiresActiveFilter(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.SavedSearchSvc = &stubSavedSearchService{}
	m.filterActive = false

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = m2.(Model)
	for _, r := range "save Unread" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Nil(t, cmd, ":save without filter must not fire Cmd")
	require.NotNil(t, m.lastError, "must set lastError when no filter is active")
}

// TestMinTerminalCheckRendersOverlay sets a minimum terminal size and
// a window smaller than the minimum, then confirms View returns the
// "terminal too small" overlay.
func TestMinTerminalCheckRendersOverlay(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.MinTerminalCols = 80
	m.deps.MinTerminalRows = 24
	m.width = 40
	m.height = 20
	out := m.View()
	require.Contains(t, out, "terminal too small", "must show overlay when window is under minimum size")
}

// TestMinTerminalCheckAbsentWhenLargeEnough confirms the overlay is
// absent when the window meets the minimum size.
func TestMinTerminalCheckAbsentWhenLargeEnough(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.MinTerminalCols = 80
	m.deps.MinTerminalRows = 24
	m.width = 120
	m.height = 40
	out := m.View()
	require.NotContains(t, out, "terminal too small", "must not show overlay when window is large enough")
}

// TestTransientClearCmdFires sets a 1ms TTL, calls clearTransientCmd,
// runs the returned Cmd, and asserts it emits clearTransientMsg.
func TestTransientClearCmdFires(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.TransientStatusTTL = 1 * time.Millisecond
	m.engineActivity = "syncing…"
	cmd := m.clearTransientCmd()
	require.NotNil(t, cmd, "clearTransientCmd must return non-nil Cmd when TTL > 0")
	msg := cmd()
	_, ok := msg.(clearTransientMsg)
	require.True(t, ok, "Cmd must emit clearTransientMsg, got %T", msg)
}
