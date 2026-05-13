package compose

import (
	"strings"
	"testing"

	"github.com/jaytaylor/html2text"
	"github.com/stretchr/testify/require"
)

// TestRenderMarkdownEmpty asserts that an empty source renders to an
// empty HTML fragment (no spurious wrapper elements).
func TestRenderMarkdownEmpty(t *testing.T) {
	out, err := RenderMarkdown("")
	require.NoError(t, err)
	require.Equal(t, "", out)
}

// TestRenderMarkdownPlainProse confirms that text with no Markdown
// syntax still becomes a <p> element (CommonMark default).
func TestRenderMarkdownPlainProse(t *testing.T) {
	out, err := RenderMarkdown("just plain text")
	require.NoError(t, err)
	require.Equal(t, "<p>just plain text</p>\n", out)
}

// TestRenderMarkdownBoldItalic exercises the canonical inline
// formatting.
func TestRenderMarkdownBoldItalic(t *testing.T) {
	out, err := RenderMarkdown("Send the **report** in *italic*.")
	require.NoError(t, err)
	require.Equal(t, "<p>Send the <strong>report</strong> in <em>italic</em>.</p>\n", out)
}

// TestRenderMarkdownUnorderedList renders an unordered list.
func TestRenderMarkdownUnorderedList(t *testing.T) {
	out, err := RenderMarkdown("- one\n- two\n- three")
	require.NoError(t, err)
	require.Contains(t, out, "<ul>")
	require.Contains(t, out, "<li>one</li>")
	require.Contains(t, out, "<li>two</li>")
	require.Contains(t, out, "<li>three</li>")
	require.Contains(t, out, "</ul>")
}

// TestRenderMarkdownOrderedList renders an ordered list.
func TestRenderMarkdownOrderedList(t *testing.T) {
	out, err := RenderMarkdown("1. first\n2. second\n3. third")
	require.NoError(t, err)
	require.Contains(t, out, "<ol>")
	require.Contains(t, out, "<li>first</li>")
	require.Contains(t, out, "</ol>")
}

// TestRenderMarkdownGFMTable confirms GFM table extension is enabled
// and renders as a <table>.
func TestRenderMarkdownGFMTable(t *testing.T) {
	src := "| col1 | col2 |\n| --- | --- |\n| a | b |\n| c | d |"
	out, err := RenderMarkdown(src)
	require.NoError(t, err)
	require.Contains(t, out, "<table>")
	require.Contains(t, out, "<th>col1</th>")
	require.Contains(t, out, "<td>a</td>")
	require.Contains(t, out, "<td>d</td>")
	require.Contains(t, out, "</table>")
}

// TestRenderMarkdownBlockquoteQuoteChain matches the spec §7.1
// expected output: the attribution line sits outside the blockquote,
// quoted text becomes a proper <blockquote><p>...</p></blockquote>.
func TestRenderMarkdownBlockquoteQuoteChain(t *testing.T) {
	src := "On Mon 2026-05-13 at 14:32, Alice wrote:\n> Hey, can you review the spec?\n> Let me know."
	out, err := RenderMarkdown(src)
	require.NoError(t, err)
	require.Contains(t, out, "<p>On Mon 2026-05-13 at 14:32, Alice wrote:</p>")
	require.Contains(t, out, "<blockquote>")
	require.Contains(t, out, "Hey, can you review the spec?")
	require.Contains(t, out, "Let me know.")
	require.Contains(t, out, "</blockquote>")
}

// TestRenderMarkdownStrikethrough exercises the GFM strikethrough
// extension.
func TestRenderMarkdownStrikethrough(t *testing.T) {
	out, err := RenderMarkdown("This is ~~deleted~~ text.")
	require.NoError(t, err)
	require.Equal(t, "<p>This is <del>deleted</del> text.</p>\n", out)
}

// TestRenderMarkdownAutolink exercises the GFM linkify extension —
// bare URLs become anchor tags without explicit Markdown link syntax.
func TestRenderMarkdownAutolink(t *testing.T) {
	out, err := RenderMarkdown("See https://example.invalid/ for details.")
	require.NoError(t, err)
	require.Contains(t, out, `<a href="https://example.invalid/">`)
	require.Contains(t, out, "https://example.invalid/")
}

// TestRenderMarkdownFencedCodeBlock renders a fenced code block.
func TestRenderMarkdownFencedCodeBlock(t *testing.T) {
	src := "```\nfmt.Println(\"hi\")\n```"
	out, err := RenderMarkdown(src)
	require.NoError(t, err)
	require.Contains(t, out, "<pre><code>")
	require.Contains(t, out, "fmt.Println")
	require.Contains(t, out, "</code></pre>")
}

// TestRenderMarkdownTaskList exercises the GFM task-list extension —
// "- [x]" and "- [ ]" render as checkbox <input> elements.
func TestRenderMarkdownTaskList(t *testing.T) {
	src := "- [x] done\n- [ ] todo"
	out, err := RenderMarkdown(src)
	require.NoError(t, err)
	require.Contains(t, out, `type="checkbox"`)
	require.Contains(t, out, "checked")
	require.Contains(t, out, "done")
	require.Contains(t, out, "todo")
}

// TestRenderMarkdownMixedProseAndQuote simulates the realistic reply
// scenario from spec §7.1 — user's Markdown above the divider, quote
// chain below.
func TestRenderMarkdownMixedProseAndQuote(t *testing.T) {
	src := "Sounds good — sending the **report** tomorrow.\n\nOn Mon 2026-05-13 at 14:32, Alice wrote:\n> Can you send the Q4 deck?"
	out, err := RenderMarkdown(src)
	require.NoError(t, err)
	// User's prose: bold rendered.
	require.Contains(t, out, "<strong>report</strong>")
	// Attribution outside blockquote.
	require.Contains(t, out, "<p>On Mon 2026-05-13 at 14:32, Alice wrote:</p>")
	// Quote in blockquote.
	require.Contains(t, out, "<blockquote>")
	require.Contains(t, out, "Q4 deck")
	require.Contains(t, out, "</blockquote>")
}

// TestRenderMarkdownNoHTMLEscape — goldmark default sanitizer strips
// raw HTML blocks. The spec deliberately omits html.WithUnsafe().
func TestRenderMarkdownNoHTMLEscape(t *testing.T) {
	out, err := RenderMarkdown("<script>alert(1)</script>\n\nhello")
	require.NoError(t, err)
	// goldmark replaces unsafe raw HTML with a comment marker.
	require.NotContains(t, out, "<script>")
	require.Contains(t, out, "hello")
}

// TestRenderMarkdownRoundTripsThroughBodyView — spec 33 DoD: drafts
// saved as HTML show up in the Drafts folder; clicking one to view
// uses render.BodyView (HTML → text). This regression target asserts
// goldmark's output is readable when run back through html2text (the
// same lib render.BodyView uses internally). No raw <p> tags leak;
// list items become readable lines.
func TestRenderMarkdownRoundTripsThroughBodyView(t *testing.T) {
	src := "Hi Alice,\n\nSending the **report** below.\n\n- One\n- Two\n\n> Original message"
	html, err := RenderMarkdown(src)
	require.NoError(t, err)
	text, err := html2text.FromString(html, html2text.Options{})
	require.NoError(t, err)
	require.NotContains(t, text, "<p>", "raw <p> must not leak through HTML→text")
	require.NotContains(t, text, "<strong>", "raw inline tags must not leak")
	require.Contains(t, text, "Hi Alice")
	require.Contains(t, text, "report")
	require.Contains(t, text, "One")
	require.Contains(t, text, "Two")
	require.Contains(t, text, "Original message")
}

// BenchmarkRenderMarkdown10KB — perf budget §10: <2ms p95.
func BenchmarkRenderMarkdown10KB(b *testing.B) {
	src := strings.Repeat("This is a paragraph of **bold** prose with a [link](https://example.invalid/).\n\n", 100)
	// ~10 KB
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := RenderMarkdown(src)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderMarkdown100KB — perf budget §10: <20ms p95.
func BenchmarkRenderMarkdown100KB(b *testing.B) {
	src := strings.Repeat("Paragraph with **bold** and *italic* and a [link](https://example.invalid/) and `code`.\n\n", 1000)
	// ~100 KB
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := RenderMarkdown(src)
		if err != nil {
			b.Fatal(err)
		}
	}
}
