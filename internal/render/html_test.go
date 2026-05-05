package render

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExternalConverterFallsBackOnError(t *testing.T) {
	// When the external converter command fails (non-existent binary),
	// htmlToTextWithConfig must fall back to the internal path and
	// return a non-empty result rather than an error.
	r := &renderer{
		htmlConverter:            "external",
		htmlConverterCmd:         "this-binary-does-not-exist-inkwell-test",
		externalConverterTimeout: 0, // use default 5s
	}
	html := `<p>Hello <b>world</b></p>`
	text, links, err := r.htmlToTextWithConfig(html, 80, 0, Theme{})
	require.NoError(t, err, "fallback to internal must succeed even when external command fails")
	require.Contains(t, text, "Hello", "internal fallback must produce readable text")
	_ = links
}

func TestExternalConverterUsesStdout(t *testing.T) {
	// When the external converter command is "echo plain text", the output
	// should come from the command's stdout. We use a command that echoes
	// a known string so the test is deterministic without a real converter.
	r := &renderer{
		htmlConverter:            "external",
		htmlConverterCmd:         "echo plain text from converter",
		externalConverterTimeout: 0,
	}
	html := `<p>ignored html content</p>`
	text, _, err := r.htmlToTextWithConfig(html, 80, 0, Theme{})
	require.NoError(t, err)
	require.Contains(t, text, "plain text from converter", "output must be the command's stdout")
}

func TestInternalConverterUsedWhenConfigured(t *testing.T) {
	// When htmlConverter is "internal" (or empty), use the built-in html2text.
	r := &renderer{
		htmlConverter: "internal",
	}
	html := `<p>Hello <a href="https://example.invalid/x">link</a></p>`
	text, links, err := r.htmlToTextWithConfig(html, 80, 0, Theme{})
	require.NoError(t, err)
	require.Contains(t, text, "Hello", "internal converter must produce readable text")
	require.True(t, anyLinkContains(links, "example.invalid/x"), "internal converter must extract links")
}
