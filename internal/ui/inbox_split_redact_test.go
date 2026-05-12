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
)

// TestInboxSubTabLogsContainNoSubjectOrSender covers spec 31 §11: the
// sub-strip dispatcher emits log lines but none carry subject / from /
// body. Captures logs over a `:focused` invocation and the subsequent
// cycle and asserts the seeded subject / sender strings do not appear.
func TestInboxSubTabLogsContainNoSubjectOrSender(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	const ownUPN = "owner@example.invalid"
	const senderAddr = "secret-focused-sender@example.invalid"
	const subjectStr = "TopSecretFocusedSubjectLine"

	id, err := st.PutAccount(context.Background(), store.Account{
		TenantID: "T", ClientID: "C", UPN: ownUPN,
	})
	require.NoError(t, err)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "m-1", AccountID: id, FolderID: "f-inbox",
		Subject: subjectStr, FromAddress: senderAddr, FromName: "Mallory",
		ReceivedAt:     time.Now(),
		InferenceClass: store.InferenceClassFocused,
	}))
	acc, err := st.GetAccount(context.Background())
	require.NoError(t, err)

	logger, captured := ilog.NewCaptured(ilog.Options{
		Level: slog.LevelDebug, AllowOwnUPN: ownUPN,
	})
	m, err := New(Deps{
		Auth:                     dispatchTestAuth{},
		Store:                    st,
		Engine:                   newDispatchTestEngine(),
		Renderer:                 render.New(st, dispatchTestStubBody{}),
		Logger:                   logger,
		Account:                  acc,
		InboxSplit:               "focused_other",
		InboxSplitDefaultSegment: "focused",
	})
	require.NoError(t, err)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)
	loadFolders := m.loadFoldersCmd()
	if msg := loadFolders(); msg != nil {
		m2, _ = m.Update(msg)
		m = m2.(Model)
	}

	// Drive a `:focused` cmd-bar invocation and a cycle.
	mm, cmd := m.dispatchCommand("focused")
	m = mm.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			m2, _ = m.Update(msg)
			m = m2.(Model)
		}
	}
	mmModel, cmd := m.cycleInboxSubTab(+1)
	m = mmModel
	if cmd != nil {
		if msg := cmd(); msg != nil {
			m2, _ = m.Update(msg)
			m = m2.(Model)
		}
	}

	dump := captured.String()
	require.NotContains(t, dump, subjectStr,
		"subject must not appear in any log line; got: %s", dump)
	require.NotContains(t, dump, senderAddr,
		"sender address must not appear in any log line; got: %s", dump)
}
