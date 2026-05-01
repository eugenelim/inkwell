package search

import (
	"strings"
	"unicode/utf8"
)

// highlightSnippet returns up to ~120 characters of context
// around the first match of any of the supplied terms in body.
// Match offsets are case-insensitive; the returned string keeps
// the body's original casing. Markdown-style `*term*` emphasis
// is added around the matched span.
//
// Empty body or empty terms returns the body's first 120 chars
// untouched (no asterisks).
//
// This is the spec 06 §2 highlight stage. We don't re-tokenise to
// match FTS5's `bm25` snippet — the body_preview is already short
// (the envelope $select cap), so a simple substring scan delivers
// the user-visible "where in the message did it match" cue.
func highlightSnippet(body string, terms []string) string {
	const maxLen = 120
	body = strings.ReplaceAll(body, "\n", " ")
	body = strings.TrimSpace(collapseSpaces(body))
	if body == "" {
		return ""
	}
	if len(terms) == 0 {
		return truncate(body, maxLen)
	}

	low := strings.ToLower(body)
	bestIdx := -1
	bestTerm := ""
	for _, t := range terms {
		needle := strings.Trim(strings.ToLower(t), `"`)
		if needle == "" {
			continue
		}
		if i := strings.Index(low, needle); i >= 0 && (bestIdx < 0 || i < bestIdx) {
			bestIdx = i
			bestTerm = needle
		}
	}
	if bestIdx < 0 {
		return truncate(body, maxLen)
	}

	// Anchor a window around the match: ~40 chars before and ~80
	// after so the matched term is visible early in the snippet
	// regardless of where it lands in the source.
	const before = 40
	start := bestIdx - before
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(body) {
		end = len(body)
	}
	snippet := body[start:end]
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(body) {
		snippet += "…"
	}
	// Add markdown-style asterisks around the matched term so the
	// UI layer can style it (or just render plain — the asterisks
	// are themselves a useful visual cue).
	low = strings.ToLower(snippet)
	if i := strings.Index(low, bestTerm); i >= 0 {
		snippet = snippet[:i] + "*" + snippet[i:i+len(bestTerm)] + "*" + snippet[i+len(bestTerm):]
	}
	return snippet
}

// collapseSpaces folds consecutive whitespace into single spaces
// so a body_preview with stray newlines / tabs renders as a
// readable single-line snippet.
func collapseSpaces(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if prevSpace {
				continue
			}
			b.WriteRune(' ')
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}

// truncate cuts s to at most maxLen runes, appending an ellipsis
// when a cut occurred. Rune-aware so multi-byte chars don't
// produce mojibake.
func truncate(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if maxLen <= 1 {
		return "…"
	}
	return string(runes[:maxLen-1]) + "…"
}
