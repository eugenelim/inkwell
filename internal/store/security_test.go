package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// SECURITY-MAP: V8.1.1 V8.2.1
// TestDatabaseFileMode is the spec 17 §4.1 file-permission invariant:
// mail.db must be created with mode 0600 so other users on the same
// system cannot read mail content. CLAUDE.md §7 rule 1 promises this;
// this test verifies it. Real-tenant relevance: shared workstations
// (lab machines, dev VMs) where the user's home directory is
// readable by other accounts.
func TestDatabaseFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.db")
	s, err := Open(path, DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, s.Close())

	info, err := os.Stat(path)
	require.NoError(t, err)
	mode := info.Mode().Perm()
	require.Equal(t, os.FileMode(0o600), mode,
		"mail.db must be created with mode 0600 to prevent other users on the same system from reading mail content (CLAUDE.md §7 rule 1)")
}

// SECURITY-MAP: V5.3.4
// TestSearchByPredicateSurvivesAdversarialInput is the spec 17 §4.6
// SQL injection invariant. The pattern compiler is parameterised and
// hands the WHERE fragment to the store with `?` placeholders for
// values; user-supplied text never gets concatenated into SQL. This
// test pushes a payload that LOOKS like SQL injection through the
// bound-parameter path and verifies the messages table survives.
func TestSearchByPredicateSurvivesAdversarialInput(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	f := SeedFolder(t, s, acc)
	require.NoError(t, s.UpsertMessage(context.Background(),
		SyntheticMessage(acc, f.ID, 0, time.Now())))

	adversarial := "bob'; DROP TABLE messages; --"
	_, err := s.SearchByPredicate(context.Background(), acc,
		`from_address LIKE ? ESCAPE '\'`, []any{"%" + adversarial + "%"}, 100)
	require.NoError(t, err)

	// Table must still exist; new inserts must still work.
	require.NoError(t, s.UpsertMessage(context.Background(),
		SyntheticMessage(acc, f.ID, 1, time.Now())))
	got, err := s.GetMessage(context.Background(), "msg-1")
	require.NoError(t, err)
	require.NotNil(t, got, "messages table must survive adversarial-input search")
}
