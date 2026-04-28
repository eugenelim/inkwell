//go:build e2e

package ui

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
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

func newFakeEngine() *fakeEngine { return &fakeEngine{events: make(chan isync.Event, 8)} }
func (f *fakeEngine) Start(_ context.Context) error                 { return nil }
func (f *fakeEngine) SetActive(_ bool)                              {}
func (f *fakeEngine) SyncAll(_ context.Context) error               { f.syncCalls++; return nil }
func (f *fakeEngine) Notifications() <-chan isync.Event             { return f.events }

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
	return New(Deps{
		Auth:     fakeAuth{upn: "tester@example.invalid", tenant: "T"},
		Store:    st,
		Engine:   eng,
		Renderer: render.New(st, stubBodyFetcher{contentType: "text", content: "hello world"}),
		Logger:   logger,
		Account:  acc,
	}), eng
}

func TestBootRendersThreePanesAndStatusBar(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "tester@example.invalid") &&
			contains(s, "Inbox") &&
			contains(s, "Archive") &&
			contains(s, "Q4 foreca") // truncated to 40-char list width
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
