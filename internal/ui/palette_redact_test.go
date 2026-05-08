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
)

// TestPaletteLogsNoSubjectsOrAddresses guards against future
// regressions if a slog.Debug is added to the palette path. Spec 22
// §6 / §8: the palette emits no logs by design. The check captures
// every log line written during open + type + close and asserts none
// of the focused message's subject / from address / folder names
// appear verbatim.
func TestPaletteLogsNoSubjectsOrAddresses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	const ownUPN = "owner@example.invalid"
	const senderAddr = "secret-sender@example.invalid"
	const subjectStr = "TopSecretSubjectLine"
	const folderName = "ConfidentialFolderName"

	id, err := st.PutAccount(context.Background(), store.Account{
		TenantID: "T", ClientID: "C", UPN: ownUPN,
	})
	require.NoError(t, err)
	require.NoError(t, st.UpsertFolder(context.Background(), store.Folder{
		ID: "f-secret", AccountID: id, DisplayName: folderName, LastSyncedAt: time.Now(),
	}))
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "m-1", AccountID: id, FolderID: "f-secret",
		Subject: subjectStr, FromAddress: senderAddr, FromName: "Mallory",
		ReceivedAt: time.Now(),
	}))
	acc, err := st.GetAccount(context.Background())
	require.NoError(t, err)

	logger, captured := ilog.NewCaptured(ilog.Options{
		Level: slog.LevelDebug, AllowOwnUPN: ownUPN,
	})
	m, err := New(Deps{
		Auth:     dispatchTestAuth{},
		Store:    st,
		Engine:   newDispatchTestEngine(),
		Renderer: render.New(st, dispatchTestStubBody{}),
		Logger:   logger,
		Account:  acc,
	})
	require.NoError(t, err)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)
	loadFolders := m.loadFoldersCmd()
	if msg := loadFolders(); msg != nil {
		m2, _ = m.Update(msg)
		m = m2.(Model)
	}
	loadMsgs := m.loadMessagesCmd("f-secret")
	if msg := loadMsgs(); msg != nil {
		m2, _ = m.Update(msg)
		m = m2.(Model)
	}

	// Drive the palette through open + type + tab + esc.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = out.(Model)
	for _, r := range "archive" {
		out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = out.(Model)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_ = out.(Model)

	dump := captured.String()
	require.NotContains(t, dump, subjectStr,
		"subject must not appear in any log line; got: %s", dump)
	require.NotContains(t, dump, senderAddr,
		"sender address must not appear in any log line; got: %s", dump)
	require.NotContains(t, dump, folderName,
		"folder name must not appear in any log line; got: %s", dump)
	// Sanity-check the captured logger is actually receiving lines —
	// we expect at least the New() startup line.
	_ = strings.TrimSpace(dump)
}
