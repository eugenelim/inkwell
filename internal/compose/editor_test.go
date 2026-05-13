package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriteTempfilePreservesEMLExtension asserts the legacy
// WriteTempfile path still produces a .eml file (backwards compat
// invariant from spec 33 §6.6).
func TestWriteTempfilePreservesEMLExtension(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := WriteTempfile("body content")
	require.NoError(t, err)
	defer os.Remove(path)
	require.True(t, strings.HasSuffix(path, ".eml"), "got %q", path)
	require.Equal(t, filepath.Base(filepath.Dir(path)), "drafts")
	data, err := os.ReadFile(path) // #nosec G304 — path is the WriteTempfile output under t.TempDir().
	require.NoError(t, err)
	require.Equal(t, "body content", string(data))
}

// TestWriteTempfileExtMD asserts the new helper produces .md when
// requested, with the same UUID-prefix naming scheme.
func TestWriteTempfileExtMD(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := WriteTempfileExt("# heading\n\nbody", ".md")
	require.NoError(t, err)
	defer os.Remove(path)
	require.True(t, strings.HasSuffix(path, ".md"), "got %q", path)
	// UUID prefix: filename without extension is hex (16 chars from
	// 8-byte newID()).
	base := filepath.Base(path)
	require.Equal(t, len(base), len(".md")+16, "got %q", base)
	data, err := os.ReadFile(path) // #nosec G304 — path is the WriteTempfileExt output under t.TempDir().
	require.NoError(t, err)
	require.Equal(t, "# heading\n\nbody", string(data))
}

// TestWriteTempfileExtMode0600 asserts privacy invariant from
// CLAUDE.md §7: drafts live at mode 0600.
func TestWriteTempfileExtMode0600(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := WriteTempfileExt("secret body", ".md")
	require.NoError(t, err)
	defer os.Remove(path)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestWriteTempfileDelegatesToExt asserts WriteTempfile is a literal
// delegation — same dir, same UUID-length naming, .eml suffix.
func TestWriteTempfileDelegatesToExt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p1, err := WriteTempfile("content")
	require.NoError(t, err)
	defer os.Remove(p1)
	p2, err := WriteTempfileExt("content", ".eml")
	require.NoError(t, err)
	defer os.Remove(p2)
	// Same dir, same suffix, same filename length (UUID is fixed).
	require.Equal(t, filepath.Dir(p1), filepath.Dir(p2))
	require.Equal(t, filepath.Ext(p1), filepath.Ext(p2))
	require.Equal(t, len(filepath.Base(p1)), len(filepath.Base(p2)))
}
