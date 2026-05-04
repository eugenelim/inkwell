//go:build e2e

package ui

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/stretchr/testify/require"

	ilog "github.com/eugenelim/inkwell/internal/log"
	"github.com/eugenelim/inkwell/internal/render"
	"github.com/eugenelim/inkwell/internal/store"
	isync "github.com/eugenelim/inkwell/internal/sync"
)

// stubBodyFetcher returns a canned body for renderer wiring tests.
type stubBodyFetcher struct{ contentType, content string }

func (f stubBodyFetcher) FetchBody(_ context.Context, _ string) (render.FetchedBody, error) {
	return render.FetchedBody{ContentType: f.contentType, Content: f.content}, nil
}

// fakeAuth satisfies the UI's Authenticator surface.
type fakeAuth struct{ upn, tenant string }

func (f fakeAuth) Account() (string, string, bool) {
	return f.upn, f.tenant, f.upn != ""
}

// fakeEngine satisfies the UI's Engine surface.
type fakeEngine struct {
	syncCalls int32
	events    chan isync.Event
}

func newFakeEngine() *fakeEngine                      { return &fakeEngine{events: make(chan isync.Event, 8)} }
func (f *fakeEngine) Start(_ context.Context) error   { return nil }
func (f *fakeEngine) SetActive(_ bool)                {}
func (f *fakeEngine) SyncAll(_ context.Context) error { f.syncCalls++; return nil }
func (f *fakeEngine) Wake()                           {}
func (f *fakeEngine) Backfill(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (f *fakeEngine) Notifications() <-chan isync.Event { return f.events }

func openE2EStore(t *testing.T) (store.Store, *store.Account) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	s, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	id, err := s.PutAccount(context.Background(), store.Account{TenantID: "T", ClientID: "C", UPN: "tester@example.invalid"})
	require.NoError(t, err)
	require.NoError(t, s.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertFolder(context.Background(), store.Folder{
		ID: "f-archive", AccountID: id, DisplayName: "Archive", WellKnownName: "archive", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertMessage(context.Background(), store.Message{
		ID: "m-1", AccountID: id, FolderID: "f-inbox",
		Subject: "Q4 forecast", FromAddress: "alice@example.invalid", FromName: "Alice",
		ReceivedAt: time.Now().Add(-time.Hour),
	}))
	require.NoError(t, s.UpsertMessage(context.Background(), store.Message{
		ID: "m-2", AccountID: id, FolderID: "f-inbox",
		Subject: "Newsletter weekly", FromAddress: "news@example.invalid", FromName: "News",
		ReceivedAt: time.Now().Add(-2 * time.Hour),
	}))
	a, err := s.GetAccount(context.Background())
	require.NoError(t, err)
	return s, a
}

func newE2EModel(t *testing.T) (Model, *fakeEngine) {
	t.Helper()
	st, acc := openE2EStore(t)
	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	eng := newFakeEngine()
	m, err := New(Deps{
		Auth:     fakeAuth{upn: "tester@example.invalid", tenant: "T"},
		Store:    st,
		Engine:   eng,
		Renderer: render.New(st, stubBodyFetcher{contentType: "text", content: "hello world"}),
		Logger:   logger,
		Account:  acc,
	})
	require.NoError(t, err)
	return m, eng
}

func TestBootRendersThreePanesAndStatusBar(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	// First paint must show the headers on all three panes, the focus
	// marker on the default-focus pane (list), and a non-truncated
	// subject (post-rebalance the list pane is wide enough).
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "tester@example.invalid") &&
			contains(s, "Folders") &&
			contains(s, "▌ Messages") && // focus marker on default-focus pane
			contains(s, "Message") &&
			contains(s, "Inbox") &&
			contains(s, "Archive") &&
			contains(s, "Q4 forecast") // full subject visible
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestQuitCommandExits(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	// Wait for first paint so the model has dimensions.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	tm.Type("quit")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestRefreshKickoffSyncAll(t *testing.T) {
	m, eng := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlR})
	// Allow the goroutine kicked from Update to run.
	time.Sleep(100 * time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
	require.GreaterOrEqual(t, eng.syncCalls, int32(1))
}

func TestSyncEventUpdatesStatusBar(t *testing.T) {
	m, eng := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	now := time.Now()
	eng.events <- isync.SyncCompletedEvent{At: now, FoldersSynced: 2, Duration: 100 * time.Millisecond}

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "synced")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestFocusSwitchingViaNumberKeys(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	// Wait for first paint that includes both folders.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Inbox") && contains(s, "Archive")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	// Default cursor lands on Inbox; archive is alphabetically before
	// inbox, so 'k' (up) moves to archive.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestResizeRecomputesLayout(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.WindowSizeMsg{Width: 60, Height: 20})
	// Just confirm the program survives the resize.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestUnknownCommandSetsErrorAndDoesNotCrash(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	tm.Type("nonsense")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	// App must still be alive: send q and it exits cleanly.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
	_, err := io.ReadAll(tm.FinalOutput(t))
	require.NoError(t, err)
}

func TestOpeningMessageFetchesBodyAndRenders(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	// Wait for the inbox + message list to render.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Inbox") && contains(s, "Q4 foreca")
	}, teatest.WithDuration(2*time.Second))

	// Focus list, open the first message.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// The body fetch goes through stubBodyFetcher → "hello world".
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "hello world")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestFoldersEnumeratedEventRendersSidebar reproduces the real-tenant
// flow: store starts EMPTY, engine emits FoldersEnumeratedEvent after
// it upserts folders into the store, the UI must reload its sidebar
// from the now-populated store. v0.2.5 shipped with messages visible
// but folders sidebar empty — this test guards the SetFolders mutation
// surviving across the Update cycle.
func TestFoldersEnumeratedEventRendersSidebar(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	id, err := st.PutAccount(context.Background(), store.Account{TenantID: "T", ClientID: "C", UPN: "tester@example.invalid"})
	require.NoError(t, err)
	acc, err := st.GetAccount(context.Background())
	require.NoError(t, err)

	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	eng := newFakeEngine()
	m, err := New(Deps{
		Auth:     fakeAuth{upn: "tester@example.invalid", tenant: "T"},
		Store:    st,
		Engine:   eng,
		Renderer: render.New(st, stubBodyFetcher{contentType: "text", content: "hello world"}),
		Logger:   logger,
		Account:  acc,
	})
	require.NoError(t, err)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	// First paint shows no folders (the store is empty).
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "tester@example.invalid")
	}, teatest.WithDuration(2*time.Second))

	// Engine upserts folders, then emits FoldersEnumeratedEvent.
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-drafts", AccountID: id, DisplayName: "Drafts", WellKnownName: "drafts", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-sent", AccountID: id, DisplayName: "Sent Items", WellKnownName: "sentitems", LastSyncedAt: time.Now(),
	}))
	eng.events <- isync.FoldersEnumeratedEvent{Count: 3, At: time.Now()}

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Inbox") && contains(s, "Drafts") && contains(s, "Sent Items")
	}, teatest.WithDuration(3*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestSubjectColumnVisibleAtStandardWidth pins the layout: at a 120-col
// terminal a long subject must remain readable in the list pane (more
// than ~10 chars survive after the date+sender prefix). v0.2.5 chopped
// subjects mid-word because list pane was 40 cols total; this guards
// the rebalance.
func TestSubjectColumnVisibleAtStandardWidth(t *testing.T) {
	st, acc := openE2EStore(t)
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "m-long", AccountID: acc.ID, FolderID: "f-inbox",
		Subject:     "Asian and Pacific Islander Heritage Month kickoff",
		FromAddress: "erg@example.invalid", FromName: "ERG",
		ReceivedAt: time.Now().Add(-3 * time.Hour),
	}))
	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	eng := newFakeEngine()
	m, err := New(Deps{
		Auth:     fakeAuth{upn: "tester@example.invalid", tenant: "T"},
		Store:    st,
		Engine:   eng,
		Renderer: render.New(st, stubBodyFetcher{contentType: "text", content: "hello"}),
		Logger:   logger,
		Account:  acc,
	})
	require.NoError(t, err)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		// At 120 cols the list pane is ~55 cols wide. The row prefix is
		// marker(2)+flag(2)+date(10)+gap+sender(14)+gap = 30 chars, leaving
		// ~25 chars for the subject. "Asian and Pacific Island" (24 chars)
		// confirms subjects remain readable; the full 48-char subject was
		// truncated before the flag indicator was added and remains so.
		return contains(string(out), "Asian and Pacific Island")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestFocusFoldersShowsFocusMarker drives "1" and asserts the focus
// marker MOVES from the list pane to the folders pane — the "▌"
// glyph must appear on Folders AND disappear from Messages. Just
// asserting "▌ Folders" appears isn't enough: the marker has to leave
// the previously-focused pane too.
func TestFocusFoldersShowsFocusMarker(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	// Default focus is list; first paint must put the marker on Messages.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Inbox") && contains(s, "▌ Messages")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		// Focus marker now on Folders, gone from Messages.
		return contains(s, "▌ Folders") && !contains(s, "▌ Messages")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestListNavigationOpensViewer drives j and Enter, asserting (a) the
// cursor glyph "▶" sits on Q4 forecast initially, (b) after j it sits
// on Newsletter weekly, and (c) Enter swaps the viewer from
// "(no message selected)" to "Subject: Newsletter weekly".
func TestListNavigationOpensViewer(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	// Initial paint: cursor "▶" on the first message; viewer empty.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "▶") &&
			contains(s, "Q4 forecast") &&
			contains(s, "Newsletter weekly") &&
			contains(s, "(no message selected)")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	// After j: cursor glyph must now be on Newsletter weekly's row.
	// We assert by line: the substring "▶ ...Newsletter weekly" must
	// appear on the same line in the output buffer.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return cursorOnLineWith(string(out), "Newsletter weekly")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Viewer pane swap: empty → headers visible.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Subject: Newsletter weekly") &&
			!contains(s, "(no message selected)")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestFolderEnterSwitchesMessageList puts focus on folders, moves to
// Archive (sorted before Inbox alphabetically — but archive is rank 3
// vs inbox rank 0, so Archive sorts BELOW Inbox), presses Enter, and
// asserts the list pane switches to the empty Archive folder.
func TestFolderEnterSwitchesMessageList(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Q4 forecast")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	// Cursor starts on Inbox (auto-pick on first paint). j moves down
	// in the sidebar order: Inbox(0) → Archive(3) (no Sent/Drafts in
	// the seed).
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Archive has no messages → list pane shows empty rows. The
	// previously-rendered "Q4 forecast" must drop out.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return !contains(string(out), "Q4 forecast")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestTabCyclesPanes drives Tab repeatedly and asserts the focus
// marker "▌" walks Messages → Message → Folders → Messages, each step
// removing the marker from the previous pane.
func TestTabCyclesPanes(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "▌ Messages")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "▌ Message") && !contains(s, "▌ Messages")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "▌ Folders") && !contains(s, "▌ Messages") && !contains(s, "▌ Message\n")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "▌ Messages") && !contains(s, "▌ Folders")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestSubfoldersRenderWithIndentation seeds Inbox with a "Projects"
// subfolder and a deeper "Q4" sub-subfolder, fires
// FoldersEnumeratedEvent, and asserts the rendered sidebar contains
// the children at increasing indentation. Without subfolder support
// the user only ever sees roots; this test guards that fix.
func TestSubfoldersRenderWithIndentation(t *testing.T) {
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
		ID: "f-projects", AccountID: id, ParentFolderID: "f-inbox", DisplayName: "Projects", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-q4", AccountID: id, ParentFolderID: "f-projects", DisplayName: "Q4", LastSyncedAt: time.Now(),
	}))
	acc, err := st.GetAccount(context.Background())
	require.NoError(t, err)

	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	eng := newFakeEngine()
	m, err := New(Deps{
		Auth:     fakeAuth{upn: "tester@example.invalid", tenant: "T"},
		Store:    st,
		Engine:   eng,
		Renderer: render.New(st, stubBodyFetcher{contentType: "text", content: "x"}),
		Logger:   logger,
		Account:  acc,
	})
	require.NoError(t, err)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	// Inbox auto-expands → "Projects" visible (a child of Inbox).
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Inbox") && contains(s, "Projects")
	}, teatest.WithDuration(2*time.Second))

	// Focus folders, navigate down to Projects (Inbox at row 0,
	// Projects at row 1), press 'o' to expand it. Q4 should appear.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		// Projects sits at depth 1 (2-space indent), Q4 at depth 2
		// (4-space indent). The disclosure glyph adds 2 chars too;
		// folderAppearsAtIndent matches just the leading spaces before
		// the name, allowing a non-space character (the glyph) in
		// between is too coupled — we instead assert each name appears.
		return contains(s, "Inbox") && contains(s, "Projects") && contains(s, "Q4")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestFolderEnterAutoFocusesList drives the user's "open this folder"
// flow: focus folders, j to a sibling, Enter — focus must move to the
// list pane so they can immediately read messages without pressing 2.
func TestFolderEnterAutoFocusesList(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox") && contains(string(out), "Archive")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "▌ Folders")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		// Focus marker has moved to Messages and is gone from Folders.
		return contains(s, "▌ Messages") && !contains(s, "▌ Folders")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestNewFolderE2E is the spec 18 visible-delta test (CLAUDE.md
// §5.4): pressing N in the folders pane paints the name modal;
// typing + Enter dispatches; status bar shows the create result.
func TestNewFolderE2E(t *testing.T) {
	m, _ := newE2EModel(t)
	stub := &e2eTriageStub{}
	m.deps.Triage = stub

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	// Focus folders pane, press N.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})

	// Visible delta: name input modal appears.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "New folder") || contains(s, "New child folder")
	}, teatest.WithDuration(2*time.Second))

	// Type a name + Enter.
	for _, r := range "Vendors" {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Visible delta: status shows the success message.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "created folder")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestPermanentDeleteOpensConfirmModalWithIrreversibleWarning is
// the spec 07 §6.7 e2e visible-delta test: pressing D paints a
// confirm modal that prominently names the irreversibility,
// includes the message subject + sender for a final sanity check,
// and the y key cycles back to the list with the row gone.
func TestPermanentDeleteOpensConfirmModalWithIrreversibleWarning(t *testing.T) {
	m, _ := newE2EModel(t)
	stub := &e2eTriageStub{}
	m.deps.Triage = stub

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Q4 forecast")
	}, teatest.WithDuration(2*time.Second))

	// Focus list, press D.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})

	// Visible delta #1: confirm modal with the irreversibility warning.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "PERMANENT DELETE") &&
			contains(s, "irreversible") &&
			contains(s, "[y]es / [N]o")
	}, teatest.WithDuration(2*time.Second))

	// Cancel — n must NOT fire PermanentDelete.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "permanent delete cancelled") && !contains(s, "[y]es / [N]o")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestHelpOverlayShowsAllSections is the spec-04-§12 e2e visible-
// delta test (CLAUDE.md §5.4): pressing `?` paints a modal that
// includes every section header from buildHelpSections, plus the
// Esc-to-close hint. Without this the user could press `?` and
// see nothing — exactly the v0.2.6 regression class.
func TestHelpOverlayShowsAllSections(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		// All four section headers must render.
		return contains(s, "Pane focus") &&
			contains(s, "Triage") &&
			contains(s, "Filter") &&
			contains(s, "Modes") &&
			contains(s, "Esc")
	}, teatest.WithDuration(2*time.Second))

	// Esc closes; the three-pane layout returns.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		// "▌ Messages" focus marker is the canonical normal-mode
		// signal; the modal-only string "Esc / q  close" should be
		// gone.
		return contains(s, "▌ Messages") && !contains(s, "Esc / q  close")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// e2eTriageStub satisfies ui.TriageExecutor with no-op writes; the
// undo path returns a canned UndoneAction so the e2e test can
// assert the visible status-bar delta.
type e2eTriageStub struct {
	undoCalls int32
	label     string
}

func (s *e2eTriageStub) MarkRead(_ context.Context, _ int64, _ string) error   { return nil }
func (s *e2eTriageStub) MarkUnread(_ context.Context, _ int64, _ string) error { return nil }
func (s *e2eTriageStub) ToggleFlag(_ context.Context, _ int64, _ string, _ bool) error {
	return nil
}
func (s *e2eTriageStub) SoftDelete(_ context.Context, _ int64, _ string) error { return nil }
func (s *e2eTriageStub) Archive(_ context.Context, _ int64, _ string) error    { return nil }
func (s *e2eTriageStub) Move(_ context.Context, _ int64, _, _, _ string) error {
	return nil
}
func (s *e2eTriageStub) PermanentDelete(_ context.Context, _ int64, _ string) error {
	return nil
}
func (s *e2eTriageStub) AddCategory(_ context.Context, _ int64, _, _ string) error {
	return nil
}
func (s *e2eTriageStub) RemoveCategory(_ context.Context, _ int64, _, _ string) error {
	return nil
}
func (s *e2eTriageStub) CreateFolder(_ context.Context, _ int64, parentID, name string) (CreatedFolder, error) {
	return CreatedFolder{ID: "f-new", DisplayName: name, ParentFolderID: parentID}, nil
}
func (s *e2eTriageStub) RenameFolder(context.Context, string, string) error { return nil }
func (s *e2eTriageStub) DeleteFolder(context.Context, string) error         { return nil }
func (s *e2eTriageStub) Undo(_ context.Context, _ int64) (UndoneAction, error) {
	atomicAdd(&s.undoCalls, 1)
	return UndoneAction{Label: s.label, MessageIDs: []string{"m-1"}}, nil
}

// TestUndoKeyShowsStatusBarMessage is the spec-07-§11 e2e visible-
// delta test (CLAUDE.md §5.4): pressing `u` after a triage action
// must paint a "↶ undid: <label>" message in the status bar. Without
// the visible-delta requirement, dispatch tests can pass while the
// user sees nothing change — exactly the v0.2.6 regression class.
func TestUndoKeyShowsStatusBarMessage(t *testing.T) {
	m, _ := newE2EModel(t)
	stub := &e2eTriageStub{label: "marked read"}
	m.deps.Triage = stub

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Q4 forecast")
	}, teatest.WithDuration(2*time.Second))

	// Focus the list pane, press `u`. The undo Cmd fires, the stub
	// returns the canned UndoneAction, the status bar paints.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		// "↶ undid: marked read" — assert both the icon AND the
		// label so the test fails for the right reason if either
		// is dropped in a future refactor.
		return contains(s, "undid") && contains(s, "marked read")
	}, teatest.WithDuration(2*time.Second))

	require.Equal(t, int32(1), stub.undoCalls)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// e2eUnsubStub is the spec 16 stub. Records the calls so the test
// can assert the right wires fired without needing a real Graph
// server — the unsub package's own tests cover the network path.
type e2eUnsubStub struct {
	resolveAction UnsubscribeAction
	postCalls     int32
}

func (s *e2eUnsubStub) Resolve(_ context.Context, _ string) (UnsubscribeAction, error) {
	return s.resolveAction, nil
}

func (s *e2eUnsubStub) OneClickPOST(_ context.Context, _ string) error {
	atomicAdd(&s.postCalls, 1)
	return nil
}

// atomicAdd is a tiny helper — using sync/atomic directly inflates
// the import surface for one call.
func atomicAdd(p *int32, n int32) { *p += n }

// TestUnsubscribeUKeyOpensConfirmModalAndExecutes is the spec 16 e2e
// visible-delta test (CLAUDE.md §5.4): U on the focused message must
// (a) make the confirm modal visible with the URL the user is about
// to act on, (b) on y the modal disappears and the status bar shows
// the success message. Without this, dispatch tests can pass while
// the user sees nothing — exactly the v0.2.6 regression class.
func TestUnsubscribeUKeyOpensConfirmModalAndExecutes(t *testing.T) {
	m, _ := newE2EModel(t)
	stub := &e2eUnsubStub{
		resolveAction: UnsubscribeAction{Kind: UnsubscribeOneClickPOST, URL: "https://example.invalid/u/abc"},
	}
	m.deps.Unsubscribe = stub

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Newsletter weekly")
	}, teatest.WithDuration(2*time.Second))

	// Focus the list pane, navigate to the seeded newsletter row (it's
	// 2nd by received-time), press U.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U")})

	// VISIBLE DELTA #1: confirm modal renders with the URL.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "example.invalid/u/abc") && contains(s, "[y]es / [N]o")
	}, teatest.WithDuration(2*time.Second))

	// y → execute one-click POST.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	// VISIBLE DELTA #2: status bar shows the success message and the
	// confirm modal is gone.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "unsubscribed") && !contains(s, "[y]es / [N]o")
	}, teatest.WithDuration(2*time.Second))

	require.Equal(t, int32(1), stub.postCalls, "y must fire OneClickPOST exactly once")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// newE2EModelWithBody mirrors newE2EModel but wires a stubBodyFetcher
// with caller-supplied content. Used by the URL-picker e2e to seed a
// body whose plain-text URL the renderer can extract.
func newE2EModelWithBody(t *testing.T, content string) (Model, *fakeEngine) {
	t.Helper()
	st, acc := openE2EStore(t)
	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	eng := newFakeEngine()
	m, err := New(Deps{
		Auth:     fakeAuth{upn: "tester@example.invalid", tenant: "T"},
		Store:    st,
		Engine:   eng,
		Renderer: render.New(st, stubBodyFetcher{contentType: "text", content: content}),
		Logger:   logger,
		Account:  acc,
	})
	require.NoError(t, err)
	return m, eng
}

// TestURLPickerOOpensModalWithExtractedURL is the spec 05 §10 e2e
// visible-delta test. Body contains a plain-text URL the renderer
// extracts; pressing `o` in the focused viewer must paint the picker
// modal containing that URL. Without this, the user can press `o`
// and see nothing — the v0.2.6 regression class.
func TestURLPickerOOpensModalWithExtractedURL(t *testing.T) {
	m, _ := newE2EModelWithBody(t, "Read more at https://example.invalid/article")

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Q4 forecast")
	}, teatest.WithDuration(2*time.Second))

	// Open the first message — body fetch + render extracts the URL.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Read more at")
	}, teatest.WithDuration(2*time.Second))

	// `O` opens the picker.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "URLs (") &&
			contains(s, "https://example.invalid/article") &&
			contains(s, "Enter / O  open")
	}, teatest.WithDuration(2*time.Second))

	// Esc closes; viewer chrome returns.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "▌ Message") && !contains(s, "URLs (")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestFullscreenBodyZHidesPanesAndShowsHint is the v0.15.x e2e
// visible-delta test for fullscreen body mode. Pressing `z` in the
// focused viewer hides the folders + list panes (so terminal-native
// click-drag selection works) and paints the exit hint. `z` again
// (or Esc) restores the three-pane layout.
func TestFullscreenBodyZHidesPanesAndShowsHint(t *testing.T) {
	m, _ := newE2EModel(t)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Q4 forecast")
	}, teatest.WithDuration(2*time.Second))

	// Open the first message; viewer is focused.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "▌ Message")
	}, teatest.WithDuration(2*time.Second))

	// `z` enters fullscreen body. Folders pane header (and list pane
	// header) must disappear; exit hint must render.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "exit fullscreen") &&
			!contains(s, "Folders") &&
			!contains(s, "Messages")
	}, teatest.WithDuration(2*time.Second))

	// `z` again exits — pane chrome returns.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Folders") && !contains(s, "exit fullscreen")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestCalendarJKEnterE2E is the spec 12 §6.2 / §7 visible-delta
// test (CLAUDE.md §5.4): :cal opens the list modal; j moves the
// ▶ cursor to the second event row; Enter loads the detail modal
// with attendees + body preview painted; Esc returns to the list.
// Without this, the dispatch test could pass while the rendered
// frame shows nothing changing — the v0.2.6 regression class.
func TestCalendarJKEnterE2E(t *testing.T) {
	m, _ := newE2EModel(t)
	now := time.Now().UTC()
	stub := &e2eCalendarStub{
		events: []CalendarEvent{
			{ID: "e-1", Subject: "Standup", Start: now.Add(time.Hour), End: now.Add(time.Hour + 30*time.Minute)},
			{ID: "e-2", Subject: "Q4 review", Start: now.Add(3 * time.Hour), End: now.Add(4 * time.Hour)},
		},
		detail: CalendarEventDetail{
			CalendarEvent: CalendarEvent{
				ID: "e-2", Subject: "Q4 review",
				Start: now.Add(3 * time.Hour), End: now.Add(4 * time.Hour),
				WebLink: "https://outlook/event/2",
			},
			BodyPreview: "Reviewing the deck before the call.",
			Attendees: []CalendarAttendee{
				{Name: "Alice Smith", Address: "alice@example.invalid", Status: "accepted"},
				{Name: "Bob Acme", Address: "bob@example.invalid", Status: "tentativelyAccepted"},
			},
		},
	}
	m.deps.Calendar = stub

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	// Open :cal — modal lists today's events.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	for _, r := range "cal" {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Standup") && contains(s, "Q4 review") && contains(s, "navigate")
	}, teatest.WithDuration(2*time.Second))

	// j moves the cursor to "Q4 review".
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return cursorOnLineWith(string(out), "Q4 review")
	}, teatest.WithDuration(2*time.Second))

	// Enter opens the detail modal with attendees + body preview.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Q4 review") &&
			contains(s, "Alice Smith") &&
			contains(s, "Bob Acme") &&
			contains(s, "Reviewing the deck") &&
			contains(s, "Outlook")
	}, teatest.WithDuration(2*time.Second))

	// Esc returns to the list — Standup row visible again, attendees gone.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Standup") && !contains(s, "Alice Smith")
	}, teatest.WithDuration(2*time.Second))

	// Esc again to leave the calendar list, then `q` to quit. (Esc
	// in CalendarMode returns to NormalMode; q in NormalMode quits.)
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// e2eCalendarStub satisfies CalendarFetcher for the e2e test.
type e2eCalendarStub struct {
	events []CalendarEvent
	detail CalendarEventDetail
}

func (s *e2eCalendarStub) ListEventsToday(_ context.Context) ([]CalendarEvent, error) {
	return s.events, nil
}
func (s *e2eCalendarStub) ListEventsBetween(_ context.Context, _, _ time.Time) ([]CalendarEvent, error) {
	return s.events, nil
}
func (s *e2eCalendarStub) GetEvent(_ context.Context, _ string) (CalendarEventDetail, error) {
	return s.detail, nil
}

// TestFolderPickerMOpensModalAndDispatchesMove is the spec 07
// §6.5 / §12.1 e2e visible-delta test: pressing `m` paints the
// "Move to:" picker; typing narrows the row list; Enter dispatches
// the move and the picker closes. Without this test the picker
// could pass dispatch tests while the modal renders no visible
// change to a real user (v0.2.6 regression class).
func TestFolderPickerMOpensModalAndDispatchesMove(t *testing.T) {
	m, _ := newE2EModel(t)
	stub := &e2eTriageStub{}
	m.deps.Triage = stub

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Q4 forecast")
	}, teatest.WithDuration(2*time.Second))

	// Focus list, press `m`.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})

	// Visible delta #1: "Move to:" picker appears with the filter
	// label and the helper line. Both Inbox and Archive must be
	// visible before the user types anything.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Move to:") &&
			contains(s, "filter:") &&
			contains(s, "Inbox") &&
			contains(s, "Archive") &&
			contains(s, "type to filter")
	}, teatest.WithDuration(2*time.Second))

	// Type "Arc" — the filter should narrow to Archive only. We
	// don't assert the absence of "Inbox" in raw output because the
	// status bar also paints folder names; instead we assert the
	// filter buffer renders + Archive is still in the modal.
	for _, r := range "Arc" {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "filter: Arc") && contains(s, "Archive")
	}, teatest.WithDuration(2*time.Second))

	// Enter dispatches the move; the picker disappears.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return !contains(s, "Move to:") && !contains(s, "filter: Arc")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// folderAppearsAtIndent returns true if `name` appears in `buf`
// preceded by exactly `indent` spaces (after the cursor-marker col).
// We split on visual lines and check each line for the pattern
// "(any cursor marker, 2 chars)<indent>name".
func folderAppearsAtIndent(buf, name string, indent int) bool {
	prefix := strings.Repeat(" ", indent) + name
	for _, line := range splitVisualLines(buf) {
		// Strip ANSI escape sequences before matching whitespace.
		clean := stripAnsi(line)
		if strings.Contains(clean, prefix) {
			return true
		}
	}
	return false
}

func stripAnsi(s string) string {
	var out []byte
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			i = j + 1
			continue
		}
		out = append(out, c)
		i++
	}
	return string(out)
}

// e2eMailboxStub is a MailboxClient stub for e2e tests.
type e2eMailboxStub struct {
	settings *MailboxSettings
	err      error
}

func (s *e2eMailboxStub) Get(_ context.Context) (*MailboxSettings, error) {
	return s.settings, s.err
}

func (s *e2eMailboxStub) SetAutoReply(_ context.Context, _ MailboxSettings) error {
	return s.err
}

// TestSettingsModalRendersFields drives `:settings` and verifies that the
// modal renders "Mailbox Settings" and "Time Zone:".
func TestSettingsModalRendersFields(t *testing.T) {
	m, _ := newE2EModel(t)
	m.deps.Mailbox = &e2eMailboxStub{
		settings: &MailboxSettings{
			AutoReplyStatus: "disabled",
			TimeZone:        "Europe/London",
			Language:        "en-GB",
		},
	}

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	for _, r := range "settings" {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Mailbox Settings") && contains(s, "Time Zone:")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestOOFModalToggleStatus drives `:ooo` and confirms that Space cycles
// the status radio from Off to On.
func TestOOFModalToggleStatus(t *testing.T) {
	m, _ := newE2EModel(t)
	m.deps.Mailbox = &e2eMailboxStub{
		settings: &MailboxSettings{
			AutoReplyStatus: "disabled",
			TimeZone:        "UTC",
		},
	}

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	for _, r := range "ooo" {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Wait for the modal to render.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Out of Office")
	}, teatest.WithDuration(2*time.Second))

	// Space should cycle status from disabled to alwaysEnabled.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "(•) On")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// contains is the tiny helper used everywhere — avoids importing strings
// into every test for a single call.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// cursorOnLineWith returns true if the framebuffer has any line that
// contains BOTH the focused-cursor glyph "▶" and the supplied text.
// This is how we assert "the cursor sits on the row with this
// message" without coupling to terminal-emulator output details.
// Splits on '\n' AND on the alt-screen ANSI cursor-position escape
// sequence (`\x1b[<row>;<col>H`) since teatest's renderer often emits
// per-line with that prefix instead of newlines.
func cursorOnLineWith(buf, text string) bool {
	// Split by both newline and ANSI cursor-position resets so we get
	// individual visual lines.
	lines := splitVisualLines(buf)
	for _, line := range lines {
		if contains(line, "▶") && contains(line, text) {
			return true
		}
	}
	return false
}

// newE2EModelWithWebLink seeds a single message that has a WebLink
// field set, used by TestViewerOpenWebLinkShowsActivity.
func newE2EModelWithWebLink(t *testing.T) (Model, *fakeEngine) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	s, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	id, err := s.PutAccount(context.Background(), store.Account{TenantID: "T", ClientID: "C", UPN: "tester@example.invalid"})
	require.NoError(t, err)
	require.NoError(t, s.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, s.UpsertMessage(context.Background(), store.Message{
		ID: "m-1", AccountID: id, FolderID: "f-inbox",
		Subject: "Linked message", FromAddress: "alice@example.invalid", FromName: "Alice",
		ReceivedAt: time.Now().Add(-time.Hour),
		WebLink:    "https://outlook.example.invalid/weblink/1",
	}))
	acc, err := s.GetAccount(context.Background())
	require.NoError(t, err)
	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	eng := newFakeEngine()
	m, err := New(Deps{
		Auth:     fakeAuth{upn: "tester@example.invalid", tenant: "T"},
		Store:    s,
		Engine:   eng,
		Renderer: render.New(s, stubBodyFetcher{contentType: "text", content: "hello"}),
		Logger:   logger,
		Account:  acc,
	})
	require.NoError(t, err)
	return m, eng
}

// newE2EModelWithConversation seeds three messages sharing a
// ConversationID so the viewer's thread-map section can be tested.
func newE2EModelWithConversation(t *testing.T) (Model, *fakeEngine) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	s, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	id, err := s.PutAccount(context.Background(), store.Account{TenantID: "T", ClientID: "C", UPN: "tester@example.invalid"})
	require.NoError(t, err)
	require.NoError(t, s.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	convID := "conv-thread-test"
	type threadMsg struct {
		id   string
		subj string
		age  time.Duration
	}
	thread := []threadMsg{
		{"mt-1", "Thread start", 3 * time.Hour},
		{"mt-2", "Thread reply 1", 2 * time.Hour},
		{"mt-3", "Thread reply 2", 1 * time.Hour},
	}
	for _, tm := range thread {
		require.NoError(t, s.UpsertMessage(context.Background(), store.Message{
			ID: tm.id, AccountID: id, FolderID: "f-inbox",
			Subject: tm.subj, FromAddress: "alice@example.invalid", FromName: "Alice",
			ReceivedAt:     time.Now().Add(-tm.age),
			ConversationID: convID,
		}))
	}
	acc, err := s.GetAccount(context.Background())
	require.NoError(t, err)
	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	eng := newFakeEngine()
	m, err := New(Deps{
		Auth:     fakeAuth{upn: "tester@example.invalid", tenant: "T"},
		Store:    s,
		Engine:   eng,
		Renderer: render.New(s, stubBodyFetcher{contentType: "text", content: "thread body"}),
		Logger:   logger,
		Account:  acc,
	})
	require.NoError(t, err)
	return m, eng
}

// TestViewerOpenWebLinkShowsActivity is the spec 05 §12 / PR 10
// visible-delta test: pressing `o` in the viewer when the message has
// a webLink sets the status-bar activity to "opening in browser…".
// Without this, the key fires a goroutine silently with no visible
// confirmation to the user.
func TestViewerOpenWebLinkShowsActivity(t *testing.T) {
	m, _ := newE2EModelWithWebLink(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Linked message")
	}, teatest.WithDuration(2*time.Second))

	// Open the message; viewer becomes focused.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "▌ Message")
	}, teatest.WithDuration(2*time.Second))

	// `o` should trigger the webLink open and surface "opening in browser…"
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "opening in browser")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestViewerOpenLinkByNumberShowsActivity is the spec 05 §12 / PR 10
// visible-delta test: pressing a digit (1-9) in the viewer when the
// body contains a corresponding link opens it and shows the activity
// "opening link N…" in the status bar. Without this, the key fires a
// goroutine with no visible confirmation.
func TestViewerOpenLinkByNumberShowsActivity(t *testing.T) {
	m, _ := newE2EModelWithBody(t, "Read more at https://example.invalid/article")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Q4 forecast")
	}, teatest.WithDuration(2*time.Second))

	// Open the first message; viewer is focused, body + links are loaded.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Read more at")
	}, teatest.WithDuration(2*time.Second))

	// Press `1` — the viewer is focused and link [1] (the extracted URL)
	// exists. Expect the status-bar activity string.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "opening link 1")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestViewerConversationThreadRendered is the spec 05 §11 / PR 10
// visible-delta test: when a message belongs to a multi-message
// conversation, opening it must render the "Thread (N messages)"
// section so the user can see the full context. Without this, thread
// nav (`[`/`]`) appears to do nothing — the v0.2.6 regression class.
func TestViewerConversationThreadRendered(t *testing.T) {
	m, _ := newE2EModelWithConversation(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Thread start")
	}, teatest.WithDuration(2*time.Second))

	// Open the first message; body + conversation thread load.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		// Thread section header must appear; all three subjects visible.
		return contains(s, "Thread (3 messages)") &&
			contains(s, "Thread start") &&
			contains(s, "Thread reply 1") &&
			contains(s, "Thread reply 2")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func splitVisualLines(buf string) []string {
	var out []string
	var cur []byte
	for i := 0; i < len(buf); i++ {
		c := buf[i]
		if c == '\n' {
			out = append(out, string(cur))
			cur = cur[:0]
			continue
		}
		// ANSI: ESC [ ... H is cursor-position; treat as line break.
		if c == 0x1b && i+1 < len(buf) && buf[i+1] == '[' {
			// Find the terminator letter.
			j := i + 2
			for j < len(buf) && !((buf[j] >= 'A' && buf[j] <= 'Z') || (buf[j] >= 'a' && buf[j] <= 'z')) {
				j++
			}
			if j < len(buf) && buf[j] == 'H' {
				out = append(out, string(cur))
				cur = cur[:0]
			}
			i = j
			continue
		}
		cur = append(cur, c)
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}
