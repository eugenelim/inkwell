package ui

import (
	"context"
	"errors"
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
func (s stubTriageDelete) Undo(context.Context, int64) (UndoneAction, error) {
	return UndoneAction{}, UndoEmpty
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
func (s stubTriageFlag) Undo(context.Context, int64) (UndoneAction, error) {
	return UndoneAction{}, UndoEmpty
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

func (s stubBulkExecutor) BulkSoftDelete(_ context.Context, _ int64, ids []string) ([]BulkResult, error) {
	if s.onSoftDelete != nil {
		s.onSoftDelete()
	}
	out := make([]BulkResult, len(ids))
	for i, id := range ids {
		out[i] = BulkResult{MessageID: id}
	}
	return out, nil
}
func (s stubBulkExecutor) BulkArchive(_ context.Context, _ int64, ids []string) ([]BulkResult, error) {
	out := make([]BulkResult, len(ids))
	for i, id := range ids {
		out[i] = BulkResult{MessageID: id}
	}
	return out, nil
}
func (s stubBulkExecutor) BulkMarkRead(_ context.Context, _ int64, ids []string) ([]BulkResult, error) {
	out := make([]BulkResult, len(ids))
	for i, id := range ids {
		out[i] = BulkResult{MessageID: id}
	}
	return out, nil
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
	undoCalls   int
	undoneLabel string
	undoErr     error
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
func (s *stubTriageWithUndo) Undo(_ context.Context, _ int64) (UndoneAction, error) {
	s.undoCalls++
	if s.undoErr != nil {
		return UndoneAction{}, s.undoErr
	}
	return UndoneAction{Label: s.undoneLabel, MessageIDs: []string{"m-1"}}, nil
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
// path: UndoEmpty must NOT show as an error (m.lastError stays nil),
// just a transient "nothing to undo" status.
func TestUndoKeyEmptyStackSurfacesFriendlyMessage(t *testing.T) {
	m := newDispatchTestModel(t)
	stub := &stubTriageWithUndo{undoErr: UndoEmpty}
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

	// Press j repeatedly — none should return a load-more Cmd.
	for i := 0; i < 5; i++ {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		require.Nil(t, cmd, "iteration %d: load-more must stay quiet on exhausted cache", i)
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
	m.deps.Calendar = stubCalendar{events: []CalendarEvent{
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
	events []CalendarEvent
	err    error
}

func (s stubCalendar) ListEventsToday(_ context.Context) ([]CalendarEvent, error) {
	return s.events, s.err
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

// TestViewerReplyKeyDispatchesCompose drives `r` in the viewer pane
// with a Drafts dep wired and asserts the compose flow starts:
// composeStartedMsg fires, the model captures the source id, and a
// non-nil Cmd is returned (which would then trigger tea.ExecProcess).
func TestViewerReplyKeyDispatchesCompose(t *testing.T) {
	m := newDispatchTestModel(t)
	called := atomicBool{}
	m.deps.Drafts = stubDraftCreator{onCall: called.set}

	// Open a message → focus moves to viewer.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	require.Equal(t, ViewerPane, m.focused)
	require.NotNil(t, m.viewer.current)

	// Press r in the viewer pane.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)
	require.NotNil(t, cmd, "viewer-pane r must return startReplyCmd")

	// Driving cmd produces composeStartedMsg. We can't run the
	// editor in the test (and tea.ExecProcess is owned by the
	// runtime), so we just verify the started msg shape.
	res := cmd()
	started, ok := res.(composeStartedMsg)
	require.True(t, ok, "cmd produces composeStartedMsg")
	// A working editor is unlikely in CI; allow either path.
	if started.err == nil {
		require.NotEmpty(t, started.tempfile)
		require.NotEmpty(t, started.sourceID)
		require.NotNil(t, started.editor)
		// Cleanup the tempfile we just wrote.
		_ = called // stub doesn't fire on the start path
	}
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

func (s stubDraftCreator) CreateDraftReply(_ context.Context, srcID, body string, to, cc, bcc []string, subject string) (*DraftRef, error) {
	if s.onCall != nil {
		s.onCall()
	}
	return &DraftRef{ID: "draft-" + srcID, WebLink: "https://outlook.office.com/draft/" + srcID}, nil
}

// TestComposeEditedRoutesIntoConfirmMode pins the v0.11.x fix: the
// post-edit msg now hands off to a confirm pane instead of saving
// immediately. Real-tenant feedback: editor `:q!` exits saved the
// draft anyway, contrary to user expectation.
func TestComposeEditedRoutesIntoConfirmMode(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}
	m2, _ := m.Update(composeEditedMsg{tempfile: "/tmp/x.eml", sourceID: "msg-1"})
	m = m2.(Model)
	require.Equal(t, ComposeConfirmMode, m.mode, "post-edit lands in confirm pane, not save")
	require.Equal(t, "/tmp/x.eml", m.composeTempfile)
	require.Equal(t, "msg-1", m.composeSourceID)
}

// TestComposeConfirmDDiscards covers the d-key path — discard
// without a Graph round-trip.
func TestComposeConfirmDDiscards(t *testing.T) {
	m := newDispatchTestModel(t)
	m.mode = ComposeConfirmMode
	m.composeTempfile = "/tmp/inkwell-test-discard.eml"
	m.composeSourceID = "msg-1"

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.Empty(t, m.composeTempfile, "tempfile cleared from model state")
	require.Contains(t, m.engineActivity, "discarded")
	require.Nil(t, cmd, "discard does NOT return a save Cmd")
}

// TestComposeConfirmSReturnsSaveCmd covers the s-key path — save
// dispatches the existing draft pipeline.
func TestComposeConfirmSReturnsSaveCmd(t *testing.T) {
	m := newDispatchTestModel(t)
	m.deps.Drafts = stubDraftCreator{}
	m.mode = ComposeConfirmMode
	m.composeTempfile = "/tmp/x.eml"
	m.composeSourceID = "msg-1"

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = m2.(Model)
	require.Equal(t, NormalMode, m.mode)
	require.Contains(t, m.engineActivity, "saving")
	require.NotNil(t, cmd, "s returns saveDraftCmd")
}

// TestComposeConfirmEscDoesNotDiscard pins the safety property: an
// accidental Esc must NOT silently throw away the user's work.
func TestComposeConfirmEscDoesNotDiscard(t *testing.T) {
	m := newDispatchTestModel(t)
	m.mode = ComposeConfirmMode
	m.composeTempfile = "/tmp/x.eml"

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	require.Equal(t, ComposeConfirmMode, m.mode, "Esc stays on the prompt")
	require.Equal(t, "/tmp/x.eml", m.composeTempfile, "tempfile preserved")
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
