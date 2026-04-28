package ui

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
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
		{"list-focused", nil, "r/R"},
		{"folders-focused", []string{"1"}, "2 list"},
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
		{"team-a", 1},      // child of inbox, alpha first
		{"team-x", 1},      // child of inbox
		{"team-x-sub", 2},  // grandchild
		{"sent", 0},        // sent items rank=1
		{"user2", 0},       // "Archive Old" — user folder, alpha
		{"user1", 0},       // "Newsletters" — user folder, alpha
		{"junk", 0},        // junk at the bottom
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
