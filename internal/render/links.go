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
func renderLinkBlock(links []ExtractedLink) string {
	if len(links) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nLinks:\n")
	for _, l := range links {
		fmt.Fprintf(&b, "  [%d] %s\n", l.Index, l.URL)
	}
	return b.String()
}
