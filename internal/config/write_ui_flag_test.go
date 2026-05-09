package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriteUIFlagCreatesFreshFile verifies the missing-file branch:
// we create the file with a `[ui]` block and the new key.
func TestWriteUIFlagCreatesFreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, WriteUIFlag(path, "screener_hint_dismissed", true))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(got), "[ui]")
	require.Contains(t, string(got), "screener_hint_dismissed = true")
}

// TestWriteUIFlagAtomicAppendNewKey verifies that an existing config
// file with a [ui] section gets the new key appended *inside* the
// section, not at end-of-file.
func TestWriteUIFlagAtomicAppendNewKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `# Inkwell config

[ui]
folders_width = 28
list_width = 50

[bindings]
quit = "q"
`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0o600))
	require.NoError(t, WriteUIFlag(path, "screener_last_seen_enabled", true))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	gotS := string(got)

	// New key must land inside [ui], before the [bindings] header.
	uiIdx := strings.Index(gotS, "[ui]")
	bindingsIdx := strings.Index(gotS, "[bindings]")
	keyIdx := strings.Index(gotS, "screener_last_seen_enabled = true")
	require.Greater(t, keyIdx, uiIdx, "new key must appear after [ui]")
	require.Less(t, keyIdx, bindingsIdx, "new key must appear before [bindings]")

	// Pre-existing keys preserved.
	require.Contains(t, gotS, `folders_width = 28`)
	require.Contains(t, gotS, `list_width = 50`)
	require.Contains(t, gotS, `quit = "q"`)
}

// TestWriteUIFlagAtomicReplaceExistingKey verifies that an existing
// `<key> = …` line is rewritten in place — no duplicates, no key
// reorder.
func TestWriteUIFlagAtomicReplaceExistingKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `[ui]
folders_width = 28
screener_hint_dismissed = false
list_width = 50
`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0o600))
	require.NoError(t, WriteUIFlag(path, "screener_hint_dismissed", true))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	gotS := string(got)
	require.Equal(t, 1, strings.Count(gotS, "screener_hint_dismissed"), "no duplicate")
	require.Contains(t, gotS, "screener_hint_dismissed = true")
	require.NotContains(t, gotS, "screener_hint_dismissed = false")
	// Order preserved.
	idxFolders := strings.Index(gotS, "folders_width")
	idxHint := strings.Index(gotS, "screener_hint_dismissed")
	idxList := strings.Index(gotS, "list_width")
	require.Less(t, idxFolders, idxHint)
	require.Less(t, idxHint, idxList)
}

// TestWriteUIFlagPreservesOtherSections verifies that other sections
// and comments are not disturbed.
func TestWriteUIFlagPreservesOtherSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `# header comment
[account]
tenant_id = "T"
client_id = "C"

[ui]
folders_width = 28

[bindings]
quit = "q"
help = "?"

[bulk]
size_warn_threshold = 100
`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0o600))
	require.NoError(t, WriteUIFlag(path, "screener_last_seen_enabled", true))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	gotS := string(got)
	require.Contains(t, gotS, "# header comment")
	require.Contains(t, gotS, `tenant_id = "T"`)
	require.Contains(t, gotS, `client_id = "C"`)
	require.Contains(t, gotS, `quit = "q"`)
	require.Contains(t, gotS, `help = "?"`)
	require.Contains(t, gotS, "size_warn_threshold = 100")
	require.Contains(t, gotS, "screener_last_seen_enabled = true")
}

// TestWriteUIFlagAppendsUISectionWhenAbsent verifies that a config
// file with no [ui] section gets one appended at end-of-file.
func TestWriteUIFlagAppendsUISectionWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `[bindings]
quit = "q"
`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0o600))
	require.NoError(t, WriteUIFlag(path, "screener_hint_dismissed", true))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	gotS := string(got)
	require.Contains(t, gotS, "[ui]")
	require.Contains(t, gotS, "screener_hint_dismissed = true")
	// [bindings] still first; [ui] appended at the end.
	require.Less(t, strings.Index(gotS, "[bindings]"), strings.Index(gotS, "[ui]"))
}

// TestWriteUIFlagMode0600 verifies the file is created with 0600
// (privacy posture per CLAUDE.md §7).
func TestWriteUIFlagMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file mode")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, WriteUIFlag(path, "screener_hint_dismissed", true))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestWriteUIFlagNoOpWhenAlreadyEqual verifies that writing the
// same value as already on disk leaves the file content byte-for-byte
// unchanged (no spurious touch).
func TestWriteUIFlagNoOpWhenAlreadyEqual(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `[ui]
screener_hint_dismissed = true
`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0o600))
	require.NoError(t, WriteUIFlag(path, "screener_hint_dismissed", true))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, initial, string(got))
}

// TestWriteUIFlagInvalidKeyRejected verifies the validation guard.
func TestWriteUIFlagInvalidKeyRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	for _, bad := range []string{"", "key with space", "key=oops", "key[bracket"} {
		err := WriteUIFlag(path, bad, true)
		require.Error(t, err, "key %q must reject", bad)
	}
}
