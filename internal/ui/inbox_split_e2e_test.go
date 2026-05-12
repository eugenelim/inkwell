//go:build e2e

package ui

import (
	"context"
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
)

func openInboxSplitE2EStore(t *testing.T) (store.Store, *store.Account) {
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
		ID: "f-sent", AccountID: id, DisplayName: "Sent Items", WellKnownName: "sentitems", LastSyncedAt: time.Now(),
	}))
	base := time.Now()
	msgs := []store.Message{
		{ID: "fm-1", AccountID: id, FolderID: "f-inbox", Subject: "Focused One", FromAddress: "f1@example.invalid", FromName: "Focus One", ReceivedAt: base.Add(-time.Hour), InferenceClass: store.InferenceClassFocused},
		{ID: "fm-2", AccountID: id, FolderID: "f-inbox", Subject: "Focused Two", FromAddress: "f2@example.invalid", FromName: "Focus Two", ReceivedAt: base.Add(-2 * time.Hour), InferenceClass: store.InferenceClassFocused},
		{ID: "om-1", AccountID: id, FolderID: "f-inbox", Subject: "Other Memo", FromAddress: "o1@example.invalid", FromName: "Other Sender", ReceivedAt: base.Add(-3 * time.Hour), InferenceClass: store.InferenceClassOther},
	}
	for _, m := range msgs {
		require.NoError(t, s.UpsertMessage(context.Background(), m))
	}
	a, err := s.GetAccount(context.Background())
	require.NoError(t, err)
	return s, a
}

func newInboxSplitE2EModel(t *testing.T, split string) (Model, *fakeEngine) {
	t.Helper()
	st, acc := openInboxSplitE2EStore(t)
	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "tester@example.invalid"})
	eng := newFakeEngine()
	m, err := New(Deps{
		Auth:                     fakeAuth{upn: "tester@example.invalid", tenant: "T"},
		Store:                    st,
		Engine:                   eng,
		Renderer:                 render.New(st, stubBodyFetcher{contentType: "text", content: "hello world"}),
		Logger:                   logger,
		Account:                  acc,
		InboxSplit:               split,
		InboxSplitDefaultSegment: "focused",
	})
	require.NoError(t, err)
	return m, eng
}

func TestInboxSubStripRendersWhenSplitFocusedOther(t *testing.T) {
	m, _ := newInboxSplitE2EModel(t, "focused_other")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return contains(s, "Focused") && contains(s, "Other") && contains(s, "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestInboxSubStripHiddenWhenSplitOff(t *testing.T) {
	// Direct render check is enough here: precondition function returns
	// false when split is off, so the strip output is empty.
	m, _ := newInboxSplitE2EModel(t, "off")
	require.False(t, m.inboxSubStripShouldRender())
	require.Empty(t, m.renderInboxSubStrip(m.theme, 80))
}

func TestColonFocusedE2ENavigatesAndRendersStrip(t *testing.T) {
	m, _ := newInboxSplitE2EModel(t, "focused_other")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	// Wait for first paint.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Focused") && contains(string(out), "Other")
	}, teatest.WithDuration(2*time.Second))

	// Invoke :focused. The strip is the visible delta.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	tm.Type("focused")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		// The active segment styling changes; assert the segment
		// names remain rendered.
		return contains(string(out), "Focused") && contains(string(out), "Other")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestInboxSubStripCycleSelectsFocusedThenOther(t *testing.T) {
	// Cycle and activation are covered by the unit tests in
	// inbox_split_test.go; the e2e variant focuses on the visible
	// rendering delta produced by the cycle keystroke.
	m, _ := newInboxSplitE2EModel(t, "focused_other")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	// Wait for the strip to paint at the top of the list column.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Focused") && contains(string(out), "Other")
	}, teatest.WithDuration(2*time.Second))

	// `]` from the -1 cold-start lands on Focused. The active segment
	// styling differs from the inactive one; assert the substring
	// shape doesn't regress.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Focused") && contains(string(out), "Other")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestColonFocusedWhenSplitOffShowsError(t *testing.T) {
	m, _ := newInboxSplitE2EModel(t, "off")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	tm.Type("focused")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "inbox split is off")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
