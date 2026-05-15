package render

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDecodeForIndex_PreservesTokensAcrossWidth verifies the
// load-bearing claim of spec 35 §6.3: the indexer input is NOT
// width-wrapped, so a regex `token=[a-f0-9]+` survives even when
// the viewer renderer would have inserted a newline mid-token.
func TestDecodeForIndex_PreservesTokensAcrossWidth(t *testing.T) {
	html := `<html><body><p>Please reset your password by clicking ` +
		`<a href="https://example.invalid/auth?token=abcdef0123456789">here</a>. ` +
		`The auth-token=abcdef0123456789 is single-use.</p></body></html>`
	text, err := DecodeForIndex(html)
	require.NoError(t, err)

	// The token literal must survive — no whitespace inside it.
	require.Contains(t, text, "auth-token=abcdef0123456789")
	// And no newlines mid-line (paragraph break is OK).
	require.NotContains(t, text, "token=\n")
	require.NotContains(t, text, "auth-\n")
}

// TestDecodeForIndex_StripsTrackingPixels keeps `Privacy` redaction
// invariants honest — tracking-pixel <img> URLs must not land in the
// indexed text.
func TestDecodeForIndex_StripsTrackingPixels(t *testing.T) {
	html := `Hi there.<img width="1" height="1" src="https://tracker.example/abc"/> bye.`
	text, err := DecodeForIndex(html)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(text), "tracker.example")
}

// TestDecodeForIndex_CollapsesWhitespace asserts the FTS-friendly
// whitespace handling: runs of spaces / tabs / nbsps collapse to a
// single space; newlines stay as paragraph separators.
func TestDecodeForIndex_CollapsesWhitespace(t *testing.T) {
	html := "<p>multi    space   word</p><p>line two</p>"
	text, err := DecodeForIndex(html)
	require.NoError(t, err)
	require.Contains(t, text, "multi space word")
	require.NotContains(t, text, "  ")
}

// TestDecodeForIndex_PlainTextInput exercises the text/plain path
// (no html tags). html2text on plain text is near-no-op; whitespace
// collapse still applies.
func TestDecodeForIndex_PlainTextInput(t *testing.T) {
	plain := "subject line  with\tnbsp"
	text, err := DecodeForIndex(plain)
	require.NoError(t, err)
	require.Equal(t, "subject line with nbsp", text)
}
