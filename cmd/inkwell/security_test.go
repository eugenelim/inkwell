package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// SECURITY-MAP: V12.1.1
// TestAttachmentSavePathRejectsTraversal verifies that safeAttachmentDest
// rejects attachment names that would escape the destination directory.
// The critical case is ".." — filepath.Base("..") returns ".." so the old
// filepath.Join(toDir, filepath.Base(name)) code would resolve to the parent
// directory. safeAttachmentDest must reject any name whose Base is "." or "..".
func TestAttachmentSavePathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		raw  string
	}{
		{"double-dot", ".."},
		{"single-dot", "."},
	}
	for _, tc := range cases {
		_, err := safeAttachmentDest(dir, tc.raw)
		require.Error(t, err, "expected error for %s (%q)", tc.name, tc.raw)
	}
}

// SECURITY-MAP: V12.1.1
// TestAttachmentSavePathStripsDirectoryComponents verifies that names
// containing path separators (e.g. "../../etc/passwd") are safely handled:
// filepath.Base strips the directory prefix, so the file lands inside the
// target directory with the leaf name only.
func TestAttachmentSavePathStripsDirectoryComponents(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		raw      string
		wantBase string
	}{
		{"../../etc/passwd", "passwd"},
		{"../../../tmp/evil", "evil"},
		{"subdir/../../escape", "escape"},
		{"../sibling", "sibling"},
		{"/absolute/path/outside", "outside"},
	}
	for _, tc := range cases {
		got, err := safeAttachmentDest(dir, tc.raw)
		require.NoError(t, err, "expected no error for %q", tc.raw)
		require.Equal(t, filepath.Join(dir, tc.wantBase), got)
	}
}

// SECURITY-MAP: V12.1.1
// TestAttachmentSavePathAcceptsSafeNames verifies that safeAttachmentDest
// accepts ordinary filenames and returns a path inside the target directory.
func TestAttachmentSavePathAcceptsSafeNames(t *testing.T) {
	dir := t.TempDir()
	safe := []string{
		"document.pdf",
		"report_2026.xlsx",
		"image.png",
		"no-extension",
	}
	for _, name := range safe {
		got, err := safeAttachmentDest(dir, name)
		require.NoError(t, err, "expected no error for %q", name)
		require.Equal(t, filepath.Join(dir, name), got)
	}
}

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
