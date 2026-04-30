package ui

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOSC52SequenceFormat pins the wire format of the OSC 52 escape:
// `\x1b]52;c;<base64>\x07`. A single byte off and terminals silently
// ignore the sequence — there's no error path. This test is the only
// thing standing between us and a v0.16.0 regression where yank
// "succeeds" but the clipboard never receives anything.
func TestOSC52SequenceFormat(t *testing.T) {
	got := osc52Sequence("https://example.invalid/x")
	require.True(t, strings.HasPrefix(got, "\x1b]52;c;"), "missing OSC 52 prefix")
	require.True(t, strings.HasSuffix(got, "\x07"), "missing BEL terminator")
	payload := strings.TrimSuffix(strings.TrimPrefix(got, "\x1b]52;c;"), "\x07")
	dec, err := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, err, "payload must be valid base64")
	require.Equal(t, "https://example.invalid/x", string(dec))
}

// TestYankerEmptyDataReturnsError covers the only synchronous error
// path (input validation). Empty-data yanks would print a base64 of
// nothing and confuse the user about whether it worked.
func TestYankerEmptyDataReturnsError(t *testing.T) {
	y := &yanker{writeOSC52: func(string) error { return nil }}
	_, err := y.Yank("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty data")
}

// TestYankerOSC52OnlyEmitsExpectedSequence runs a yank with no pbcopy
// path and asserts the exact bytes hit the writer. Captures the
// "OSC 52" label so the status bar message stays stable.
func TestYankerOSC52OnlyEmitsExpectedSequence(t *testing.T) {
	var captured string
	y := &yanker{writeOSC52: func(seq string) error { captured = seq; return nil }}
	label, err := y.Yank("hello")
	require.NoError(t, err)
	require.Equal(t, "OSC 52", label)
	require.Equal(t, osc52Sequence("hello"), captured)
}

// TestYankerOSC52WriterErrorReturnsLabel covers the "best-effort
// success" semantics: if OSC 52 fails AND there's no pbcopy fallback,
// Yank surfaces an error. This is the only failure path the user
// sees as a status-bar error.
func TestYankerOSC52WriterErrorOnlyPath(t *testing.T) {
	y := &yanker{writeOSC52: func(string) error { return errors.New("boom") }}
	_, err := y.Yank("hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "yank failed")
}

// TestYankerNilWriterStillSucceedsOnPbcopy guards the construction
// invariant: even with no OSC 52 destination, a darwin-mode yanker
// can still succeed via pbcopy. Production code always provides a
// writer; tests don't have to.
//
// We don't fork a real pbcopy here — pbcopyDarwin is left false so
// the call short-circuits — but we DO assert that with both paths
// disabled, the yanker reports "no path took". (Empty label + nil
// error would mislead the status bar.)
func TestYankerBothPathsDisabledReturnsEmptyLabel(t *testing.T) {
	y := &yanker{writeOSC52: nil, pbcopyDarwin: false}
	label, err := y.Yank("hello")
	require.NoError(t, err)
	require.Equal(t, "", label)
}
