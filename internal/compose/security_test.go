package compose

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// SECURITY-MAP: V8.1.1 V8.2.1
// TestDraftTempfileMode is the spec 17 §4.1 file-permission invariant
// for compose tempfiles. Drafts contain unsent mail (potentially
// sensitive); they must be 0600 so other users on the same system
// cannot read them.
func TestDraftTempfileMode(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	path, err := WriteTempfile("To: a@example.invalid\n\nbody\n")
	require.NoError(t, err)
	defer func() { _ = os.Remove(path) }()

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"draft tempfile must be 0600 — `docs/CONVENTIONS.md` §7 rule 1 + spec 15 §6 invariant")
}

// SECURITY-MAP: V12.1.1 V12.3.1
// TestEditorCommandUsesArgvNotShell is the spec 17 §4.7 subprocess-
// injection invariant: the editor binary is invoked via
// exec.Command(bin[0], args...) — argv form. NOT exec.Command("sh",
// "-c", ...). This means a malicious $EDITOR value can't smuggle
// shell metacharacters through.
//
// We assert by inspecting the *exec.Cmd's Args and Path fields. If a
// future refactor accidentally wraps with `sh -c`, the test breaks.
func TestEditorCommandUsesArgvNotShell(t *testing.T) {
	t.Setenv("INKWELL_EDITOR", "echo")
	cmd, err := EditorCmd("/tmp/draft.eml")
	require.NoError(t, err)
	require.Equal(t, "echo", cmd.Args[0],
		"first argv element must be the editor binary itself, not `sh`")
	require.NotEqual(t, "-c", cmd.Args[1],
		"second argv element must be the path / editor args, not `-c`")
	// And the resolved Path must NOT be a shell binary.
	require.False(t, strings.HasSuffix(cmd.Path, "/sh"),
		"resolved Path must not be a shell")
	require.False(t, strings.HasSuffix(cmd.Path, "/bash"),
		"resolved Path must not be a shell")
}
