package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// captureStdout temporarily redirects os.Stdout to a buffer for the
// duration of fn. Returns the captured bytes.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	defer func() {
		os.Stdout = orig
	}()
	fn()
	_ = w.Close()
	<-done
	return buf.String()
}

// TestPrintIndexStatus_DoesNotPrintFolderNamesWhenEmpty is the
// spec 35 §13.4 redaction check for the CLI status surface. When
// the user has no folder allow-list configured, the status output
// must show "(empty — all subscribed folders)" and NOT enumerate
// real folder names — those are PII-adjacent.
func TestPrintIndexStatus_DoesNotPrintFolderNamesWhenEmpty(t *testing.T) {
	report := indexStatusReport{
		Enabled:         true,
		Rows:            42,
		Bytes:           4096,
		MaxCount:        5000,
		MaxBytes:        500 * 1024 * 1024,
		MaxBodyBytes:    1024 * 1024,
		FolderAllowlist: nil, // critical: empty list
	}
	out := captureStdout(t, func() { printIndexStatus(report) })
	require.Contains(t, out, "all subscribed folders",
		"empty allow-list must surface the documented placeholder string")
	require.NotContains(t, out, "Inbox")
	require.NotContains(t, out, "Sent")
	require.NotContains(t, out, "Clients/")
}

// TestPrintIndexStatus_EchoesConfiguredAllowlistOnly asserts that
// when the user has set a folder allow-list, the printout echoes
// the user's own configured strings — never resolved Graph names
// or other folders that happen to be in the store.
func TestPrintIndexStatus_EchoesConfiguredAllowlistOnly(t *testing.T) {
	report := indexStatusReport{
		Enabled:         true,
		MaxCount:        5000,
		MaxBytes:        500 * 1024 * 1024,
		MaxBodyBytes:    1024 * 1024,
		FolderAllowlist: []string{"Clients/Alpha", "Clients/Bravo"},
	}
	out := captureStdout(t, func() { printIndexStatus(report) })
	require.Contains(t, out, "Clients/Alpha")
	require.Contains(t, out, "Clients/Bravo")
	require.NotContains(t, out, "all subscribed folders")
	// Bad allow-list entries (not in the user's config) should not appear.
	require.NotContains(t, out, "Inbox")
}

// TestPrintIndexStatus_NoSnippetsEcho is the §8.5 invariant
// "indexed text never reaches the CLI". The report shape doesn't
// carry snippets in v1, but the test pins the contract so a
// future iteration that adds a "preview" field has to flip this
// test to red first.
func TestPrintIndexStatus_NoSnippetsEcho(t *testing.T) {
	report := indexStatusReport{
		Enabled:      true,
		MaxCount:     5000,
		MaxBytes:     500 * 1024 * 1024,
		MaxBodyBytes: 1024 * 1024,
	}
	out := captureStdout(t, func() { printIndexStatus(report) })
	// Should be plumbing only: caps, paths, counts. Not a single
	// human-readable sentence from any indexed body should appear.
	suspicious := []string{
		"Dear ", "Hi ", "Hello ", "Please ", "Subject:", "From:",
	}
	for _, s := range suspicious {
		require.NotContains(t, strings.ToLower(out), strings.ToLower(s),
			"status output must not echo content-shaped strings (%q)", s)
	}
}
