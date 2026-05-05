package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// SECURITY-MAP: V8.1.1 V8.2.1
// TestLogFileMode is the spec 17 §4.1 file-permission invariant for
// the inkwell log file. Log lines may contain email subjects at
// DEBUG level; 0600 ensures other users on the same machine cannot
// read them.
func TestLogFileMode(t *testing.T) {
	dir := t.TempDir()
	_, closer, err := openLogFileAt(dir, slog.LevelInfo, "test@example.invalid")
	require.NoError(t, err)
	defer closer.Close()

	info, err := os.Stat(filepath.Join(dir, "inkwell.log"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"log file must be 0600 — CLAUDE.md §7 rule 1: log content may include subject lines at DEBUG level")
}
