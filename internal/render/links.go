package render

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

// urlPattern captures bare URLs found in plain text (post html2text).
var urlPattern = regexp.MustCompile(`https?://[^\s)\]]+`)

// extractLinks returns deduplicated, numbered links found in body. The
// numbering is deterministic by first occurrence.
func extractLinks(body string) []ExtractedLink {
	matches := urlPattern.FindAllString(body, -1)
	seen := make(map[string]int)
	var out []ExtractedLink
	for _, u := range matches {
		u = trimTrailingPunct(u)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = len(out) + 1
		out = append(out, ExtractedLink{Index: len(out) + 1, URL: u, Text: u})
	}
	return out
}

// trimTrailingPunct removes characters that often follow a URL in
// running prose ('.', ',', ';', ':') but never appear at the end of a
// real URL.
func trimTrailingPunct(u string) string {
	return strings.TrimRight(u, ".,;:!?")
}

// renderLinkBlock formats the numbered link list appended to a body.
// URLs are wrapped in OSC 8 hyperlink escape sequences so terminals
// that support them (iTerm2 ≥ 3.1, kitty, alacritty, foot, wezterm,
// recent gnome-terminal / Konsole) render them as clickable. This
// is the spec-15.x fix for the "drag-selecting a long URL captures
// the adjacent message list pane" complaint — users click instead.
//
// Terminals without OSC 8 support (Apple Terminal.app, older xterm)
// silently strip the escape; the URL still renders as plain text and
// the user falls back to the numbered-link `1`-`9` keys (spec 05 §9).
func renderLinkBlock(links []ExtractedLink) string {
	if len(links) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nLinks:\n")
	for _, l := range links {
		fmt.Fprintf(&b, "  [%d] %s\n", l.Index, osc8(l.URL, l.URL))
	}
	return b.String()
}

// osc8 wraps text in the OSC 8 hyperlink escape sequence with a
// deterministic `id=` parameter derived from the URL itself.
//
//	\e]8;id=u<hash>;<url>\e\\ <text> \e]8;;\e\\
//
// The `id` parameter is **load-bearing** for hover behaviour when
// the URL spans multiple visual rows: lipgloss wraps long lines to
// the viewer pane width, and without `id` the terminal treats each
// row's segment as a separate hyperlink (only the row under the
// cursor highlights). Setting a stable `id` per URL groups every
// rendered fragment as one logical link, so hovering any row of a
// wrapped URL highlights the entire URL — and all repeat
// occurrences of the same URL in the body highlight together.
//
// Supporting terminals render text as a clickable link to url.
// Non-supporting terminals (Apple Terminal.app, older xterm) strip
// the escapes; text shows through.
func osc8(url, text string) string {
	const (
		osc8Start = "\x1b]8;"
		osc8End   = "\x1b\\"
	)
	id := osc8LinkID(url)
	return osc8Start + "id=" + id + ";" + url + osc8End + text + osc8Start + ";" + osc8End
}

// osc8LinkID returns a short stable id for a URL. fnv-32 keeps the
// string ≤8 hex chars (well under terminals' 250-byte id limit per
// the OSC 8 spec) and the same URL always produces the same id, so
// repeat occurrences in the same body behave as one hyperlink for
// hover.
func osc8LinkID(url string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(url))
	return fmt.Sprintf("u%x", h.Sum32())
}

// linkifyURLsInText scans body for bare URLs and wraps them with
// OSC 8 escapes in place. urlMaxDisplay caps the visible link text
// at N cells with end-truncation (`https://example.com/auth/…`);
// the URL portion of the OSC 8 sequence stays intact so Cmd-click
// + the URL picker still open the full URL. 0 disables truncation.
//
// End-truncation (vs middle-truncation) is the deliberate choice
// for security: the domain prefix stays visible so users can spot
// a phishing URL at a glance. The full URL is also retained in the
// trailing `Links:` block produced by [renderLinkBlock] so the
// user has one always-untruncated source of truth.
func linkifyURLsInText(body string, urlMaxDisplay int) string {
	return urlPattern.ReplaceAllStringFunc(body, func(u string) string {
		trimmed := trimTrailingPunct(u)
		if trimmed == "" {
			return u
		}
		// Preserve any trailing punctuation we didn't consume.
		suffix := u[len(trimmed):]
		display := truncateURLForDisplay(trimmed, urlMaxDisplay)
		return osc8(trimmed, display) + suffix
	})
}

// truncateURLForDisplay returns the visible text for an OSC 8
// hyperlink display. When maxDisplay > 0 and the URL exceeds it,
// the result is the URL's first maxDisplay-1 cells followed by `…`
// (total cells == maxDisplay). 0 or a non-truncating cap returns
// the URL unchanged. URLs are mostly ASCII so rune count == cell
// count; for the rare wide-character URL the rune-cap remains a
// safe upper bound.
func truncateURLForDisplay(url string, maxDisplay int) string {
	if maxDisplay <= 0 {
		return url
	}
	runes := []rune(url)
	if len(runes) <= maxDisplay {
		return url
	}
	if maxDisplay == 1 {
		return "…"
	}
	return string(runes[:maxDisplay-1]) + "…"
}
