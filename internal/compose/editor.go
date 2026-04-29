package compose

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// editorBinary picks the editor command. Order:
//
//  1. INKWELL_EDITOR env var (per-app override; doesn't clobber the
//     user's general $EDITOR for other tools)
//  2. EDITOR env var (user's normal terminal editor)
//  3. nano as a sane fallback that ships on every macOS / Linux box
//
// Returned as `[]string` so tests can stub. If nano isn't present
// either, the caller surfaces the error to the user.
func editorBinary() []string {
	if v := os.Getenv("INKWELL_EDITOR"); v != "" {
		return []string{v}
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return []string{v}
	}
	return []string{"nano"}
}

// EditorCmd returns the *exec.Cmd that opens path in the user's
// editor. Bubble Tea's tea.ExecProcess takes this and handles the
// terminal suspend/resume. Caller adds env or other knobs as needed.
func EditorCmd(path string) (*exec.Cmd, error) {
	bin := editorBinary()
	if _, err := exec.LookPath(bin[0]); err != nil {
		return nil, fmt.Errorf("compose: editor %q not in PATH (set INKWELL_EDITOR or EDITOR)", bin[0])
	}
	args := append([]string{}, bin[1:]...)
	args = append(args, path)
	// #nosec G204 — bin[0] is the user's chosen $EDITOR / INKWELL_EDITOR resolved through exec.LookPath; path is an inkwell-generated tempfile in ~/Library/Caches/inkwell/drafts. Spec 15 §6 is the explicit design: we hand control to the user's editor.
	return exec.Command(bin[0], args...), nil
}

// WriteTempfile creates a fresh tempfile under
// ~/Library/Caches/inkwell/drafts/{uuid}.eml (or os.TempDir on
// non-mac systems) and writes content to it. The file mode is 0600
// — privacy invariant from CLAUDE.md §7.
func WriteTempfile(content string) (string, error) {
	dir, err := draftsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("compose: mkdir drafts: %w", err)
	}
	id, err := newID()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, id+".eml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("compose: write tempfile: %w", err)
	}
	return path, nil
}

// CleanupTempfile removes a tempfile created by WriteTempfile.
// Best-effort; errors are silently ignored (the cache directory is
// scrubbed periodically anyway).
func CleanupTempfile(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

func draftsDir() (string, error) {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "Library", "Caches", "inkwell", "drafts"), nil
	}
	return filepath.Join(os.TempDir(), "inkwell-drafts"), nil
}

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
