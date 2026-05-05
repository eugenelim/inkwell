package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SECURITY-MAP: V12.1.1 V12.3.1
// TestOpenInBrowserUsesArgvNotShell verifies that openInBrowser
// invokes the OS handler via argv (exec.Command(binary, url)) rather
// than through a shell (exec.Command("sh", "-c", ...)).
// A malicious URL containing shell metacharacters cannot escape if
// the OS handler is invoked directly.
func TestOpenInBrowserUsesArgvNotShell(t *testing.T) {
	args, ok := openInBrowserArgs("https://example.invalid/meeting")
	if !ok {
		t.Skip("unsupported OS")
	}
	require.Len(t, args, 2, "must be exactly [binary, url] — no shell wrapper")
	require.NotEqual(t, "sh", args[0])
	require.NotEqual(t, "bash", args[0])
	require.Equal(t, "https://example.invalid/meeting", args[len(args)-1],
		"URL must be the last arg passed directly, not shell-interpolated")
}
