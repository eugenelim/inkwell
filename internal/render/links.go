package render

import (
	"fmt"
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

// osc8 wraps text in the OSC 8 hyperlink escape sequence.
//
//	\e]8;;<url>\e\\ <text> \e]8;;\e\\
//
// Supporting terminals render text as a clickable link to url. Non-
// supporting terminals strip the escapes; text shows through.
func osc8(url, text string) string {
	const (
		osc8Start = "\x1b]8;;"
		osc8End   = "\x1b\\"
	)
	return osc8Start + url + osc8End + text + osc8Start + osc8End
}

// linkifyURLsInText scans body for bare URLs and wraps them with
// OSC 8 escapes in place. Used after HTML→text conversion so the
// rare inline URL that didn't get converted to a `[N]` reference
// still becomes clickable. Caller-controlled by the
// [ui].clickable_links config (default "auto" → on).
func linkifyURLsInText(body string) string {
	return urlPattern.ReplaceAllStringFunc(body, func(u string) string {
		trimmed := trimTrailingPunct(u)
		if trimmed == "" {
			return u
		}
		// Preserve any trailing punctuation we didn't consume.
		suffix := u[len(trimmed):]
		return osc8(trimmed, trimmed) + suffix
	})
}
