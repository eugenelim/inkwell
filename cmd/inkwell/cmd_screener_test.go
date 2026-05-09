package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestScreenerPreApproveStreamSkipsBlanksAndComments parses a
// stdin-shaped buffer through preApproveStream and asserts the
// CRLF / BOM / blank-line / # comment rules from spec 28 §7.
func TestScreenerPreApproveStreamSkipsBlanksAndComments(t *testing.T) {
	app := newCLITestApp(t)
	src := strings.NewReader("\ufeff# header\n\nalice@example.invalid\r\n# another\nbob@example.invalid\n\n")
	admitted, errs := preApproveStream(context.Background(), app, src, "imbox")
	require.Equal(t, 2, admitted)
	require.Empty(t, errs)
	dest, err := app.store.GetSenderRouting(context.Background(), app.account.ID, "alice@example.invalid")
	require.NoError(t, err)
	require.Equal(t, "imbox", dest)
}

// TestScreenerPreApproveStreamPartialSuccess admits the good lines
// and collects per-line errors for the bad ones (spec 28 §7).
func TestScreenerPreApproveStreamPartialSuccess(t *testing.T) {
	app := newCLITestApp(t)
	src := strings.NewReader("good@example.invalid\n\"Bob\" <bob@example.invalid>\nalso-good@example.invalid\n")
	admitted, errs := preApproveStream(context.Background(), app, src, "imbox")
	require.Equal(t, 2, admitted)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0].Error(), "must be bare")
}

// TestScreenerPreApproveStreamRespectsDestination verifies all
// successful lines go to the supplied destination.
func TestScreenerPreApproveStreamRespectsDestination(t *testing.T) {
	app := newCLITestApp(t)
	src := strings.NewReader("a@example.invalid\nb@example.invalid\n")
	admitted, errs := preApproveStream(context.Background(), app, src, "feed")
	require.Equal(t, 2, admitted)
	require.Empty(t, errs)
	dest, err := app.store.GetSenderRouting(context.Background(), app.account.ID, "a@example.invalid")
	require.NoError(t, err)
	require.Equal(t, "feed", dest)
}

// TestScreenerPreApproveAllFail confirms zero successes still
// returns an error list (spec 28 §7 — caller exits 2 on all-fail).
func TestScreenerPreApproveAllFail(t *testing.T) {
	app := newCLITestApp(t)
	src := strings.NewReader("\"Bob\" <bob@example.invalid>\nnot-an-email\n")
	admitted, errs := preApproveStream(context.Background(), app, src, "imbox")
	require.Equal(t, 0, admitted)
	require.Len(t, errs, 2)
}

// TestScreenerStdinIsTTY guard: when stdin is not a terminal (pipe,
// redirect, file), stdinIsTTY returns false. Hard to mock a TTY
// from go test; verify the negative branch.
func TestScreenerStdinIsTTY(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.txt")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old })
	require.False(t, stdinIsTTY(), "regular file is not a TTY")
}

// TestResolveScreenerCmdRegistered verifies the parent command is
// in the root tree and carries the expected subcommands.
func TestResolveScreenerCmdRegistered(t *testing.T) {
	root := newRootCmd()
	var screener *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "screener" {
			screener = c
			break
		}
	}
	require.NotNil(t, screener)
	verbs := []string{"list", "accept", "reject", "history", "pre-approve", "status"}
	for _, v := range verbs {
		hit := false
		for _, c := range screener.Commands() {
			if c.Name() == v {
				hit = true
				break
			}
		}
		require.True(t, hit, "screener subcommand %q missing", v)
	}
}
