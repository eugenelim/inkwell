package savedsearch

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/config"
	"github.com/eugenelim/inkwell/internal/store"
)

// newBodyIndexManager opens a fresh store + returns a Manager
// constructed against the supplied [body_index].enabled value.
// Mirrors testManager() but exposes the toggle.
func newBodyIndexManager(t *testing.T, bodyIndexEnabled bool) (*Manager, store.Store, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	st, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	accID, err := st.PutAccount(ctx, store.Account{TenantID: "T", ClientID: "C", UPN: "user@example.invalid"})
	require.NoError(t, err)
	require.NoError(t, st.UpsertFolder(ctx, store.Folder{
		ID: "f-inbox", AccountID: accID, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	cfg := config.SavedSearchSettings{
		CacheTTL:         30 * time.Second,
		BodyIndexEnabled: bodyIndexEnabled,
	}
	return New(st, accID, cfg), st, accID
}

// TestManager_ListPreservesRowOnRegexCompileError is spec 35 §9.6's
// canonical assertion: a saved search whose pattern fails to compile
// under the current [body_index].enabled flag MUST stay in the list
// with LastCompileError populated, so the sidebar can grey it out
// instead of silently dropping it.
func TestManager_ListPreservesRowOnRegexCompileError(t *testing.T) {
	// Save the regex saved search while the index is on, then re-open
	// a fresh Manager with the index off and list.
	mOn, st, accID := newBodyIndexManager(t, true)
	ctx := context.Background()
	require.NoError(t, mOn.Save(ctx, store.SavedSearch{
		AccountID: accID, Name: "AuthTokens", Pattern: `~b /auth.*token=[a-f0-9]+/`,
	}))

	mOff := New(st, accID, config.SavedSearchSettings{CacheTTL: time.Minute, BodyIndexEnabled: false})
	rows, err := mOff.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "AuthTokens", rows[0].Name)
	require.NotEmpty(t, rows[0].LastCompileError, "regex saved search must carry LastCompileError when index is off")
}

// TestManager_SaveAcceptsRegexWhenBodyIndexEnabled covers the
// happy path: with the index on, saving a regex pattern succeeds.
func TestManager_SaveAcceptsRegexWhenBodyIndexEnabled(t *testing.T) {
	m, _, accID := newBodyIndexManager(t, true)
	require.NoError(t, m.Save(context.Background(), store.SavedSearch{
		AccountID: accID, Name: "AuthRegex", Pattern: `~b /auth.*token/`,
	}))
}

// TestManager_SaveRejectsRegexWhenBodyIndexDisabled covers the
// failure path: with the index off, saving a body-regex pattern
// fails with the spec 35 §9.3 sentinel.
func TestManager_SaveRejectsRegexWhenBodyIndexDisabled(t *testing.T) {
	m, _, accID := newBodyIndexManager(t, false)
	err := m.Save(context.Background(), store.SavedSearch{
		AccountID: accID, Name: "Bad", Pattern: `~b /auth/`,
	})
	require.Error(t, err)
	require.True(t,
		strings.Contains(err.Error(), "body_index") ||
			strings.Contains(err.Error(), "regex"),
		"expected body-index gating error, got %q", err.Error())
}

// TestManager_ListNoErrorForPlainSavedSearches asserts the grey-out
// path doesn't false-positive on patterns that compile fine
// regardless of the body-index flag.
func TestManager_ListNoErrorForPlainSavedSearches(t *testing.T) {
	m, _, accID := newBodyIndexManager(t, false)
	ctx := context.Background()
	require.NoError(t, m.Save(ctx, store.SavedSearch{AccountID: accID, Name: "Unread", Pattern: "~N"}))
	require.NoError(t, m.Save(ctx, store.SavedSearch{AccountID: accID, Name: "FromBob", Pattern: "~f bob@acme.invalid"}))
	rows, err := m.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		require.Empty(t, r.LastCompileError, "plain saved search %q should not carry a compile error", r.Name)
	}
}
