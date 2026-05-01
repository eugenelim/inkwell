package search

import (
	"strings"
)

// ParsedQuery is the structured form of a user search expression
// after field-prefix extraction. PlainTerms is the leftover free-
// text payload (already-quoted phrases preserved) that runs
// against ALL FTS5 columns; field-prefixed terms run against the
// matching column only.
//
// Example: `from:bob "q4 review"` → ParsedQuery{
//
//	From: ["bob"],
//	PlainTerms: ["\"q4 review\""],
//
// }
type ParsedQuery struct {
	From       []string
	Subject    []string
	Body       []string
	PlainTerms []string
}

// ParseQuery splits a user-typed expression into field-prefixed
// terms (`from:`, `subject:`, `body:`) and free-text remainder.
// Quoted phrases (`"q4 review"`) are preserved as-is.
//
// The parser is forgiving: an unrecognised prefix like
// `category:work` is treated as plain text (no special handling
// — falls through to the FTS5 default-column match).
func ParseQuery(text string) ParsedQuery {
	var out ParsedQuery
	tokens := tokenise(text)
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		field, value, ok := splitFieldPrefix(tok)
		if !ok {
			out.PlainTerms = append(out.PlainTerms, tok)
			continue
		}
		switch field {
		case "from":
			out.From = append(out.From, value)
		case "subject":
			out.Subject = append(out.Subject, value)
		case "body":
			out.Body = append(out.Body, value)
		default:
			// Unknown prefix — fall through as plain text. This
			// keeps `cat:foo` (where `cat:` isn't ours) from
			// silently dropping; the user sees a no-match instead
			// of a phantom result.
			out.PlainTerms = append(out.PlainTerms, tok)
		}
	}
	return out
}

// tokenise walks the input and returns whitespace-separated
// tokens, preserving quoted phrases intact (including their
// surrounding quotes). The tokeniser is single-pass: no regex.
func tokenise(s string) []string {
	var (
		out      []string
		buf      strings.Builder
		inQuotes bool
	)
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, buf.String())
			buf.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			buf.WriteRune(r)
			inQuotes = !inQuotes
		case (r == ' ' || r == '\t') && !inQuotes:
			flush()
		default:
			buf.WriteRune(r)
		}
	}
	flush()
	return out
}

// splitFieldPrefix returns ("from", "bob", true) for a token
// shaped `from:bob`. Returns ok=false when no prefix is present
// OR when the value portion is empty (`from:`).
func splitFieldPrefix(tok string) (field, value string, ok bool) {
	idx := strings.Index(tok, ":")
	if idx <= 0 || idx == len(tok)-1 {
		return "", "", false
	}
	// Don't split URLs / time stamps masquerading as field
	// prefixes. A `:` is a prefix marker only when the LHS is
	// exclusively lower-ASCII letters (matches the `field:` shape
	// in the spec). Anything else (e.g., `https:`, `12:34`,
	// `~from:`) falls through as plain text.
	for i := 0; i < idx; i++ {
		c := tok[i]
		if c < 'a' || c > 'z' {
			return "", "", false
		}
	}
	return tok[:idx], tok[idx+1:], true
}

// BuildFTSQuery turns a ParsedQuery into the FTS5 expression run
// against messages_fts. Spec 06 §4.1.
//
// Conjunction rules:
//   - Free-text terms are AND'd.
//   - Field-prefixed terms become column filters
//     (`{column}:value`) AND'd with the rest.
//   - Quoted phrases stay verbatim so FTS5's phrase-match handles
//     them.
//   - An OR/NOT/NEAR operator typed by the user (uppercase) is
//     preserved.
//   - Special FTS5 operator characters that appear inside a free-
//     text term (`*`, `(`, `)`) are passed through; they're how
//     FTS5 callers do prefix / grouping search.
//
// Empty parsed → empty string. The Searcher treats empty as
// "skip the local branch".
func BuildFTSQuery(p ParsedQuery) string {
	var parts []string

	addColumn := func(col string, values []string) {
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			parts = append(parts, col+":"+ftsTerm(v))
		}
	}

	addColumn("from_address", p.From)
	addColumn("from_name", p.From)
	addColumn("subject", p.Subject)
	addColumn("body_preview", p.Body)

	for _, t := range p.PlainTerms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		parts = append(parts, ftsTerm(t))
	}

	// Collapse `from:` doubles (we emit two columns for the same
	// user-typed `from:` token, so two adjacent should OR — but
	// FTS5 treats space as AND. Group them with parens + OR.
	return joinFTSWithFromOR(parts, len(p.From))
}

// ftsTerm wraps a token in FTS5-safe quoting. Phrases already
// surrounded by double quotes pass through; bare words containing
// punctuation (`@`, `.`) are auto-quoted to dodge FTS5's tokeniser
// rejection of email-shaped strings; FTS5 operator words are
// preserved verbatim.
func ftsTerm(t string) string {
	switch t {
	case "AND", "OR", "NOT", "NEAR":
		return t
	}
	if strings.HasPrefix(t, `"`) && strings.HasSuffix(t, `"`) {
		return t
	}
	if needsAutoQuote(t) {
		return `"` + strings.ReplaceAll(t, `"`, ``) + `"`
	}
	return t
}

// needsAutoQuote reports whether a bare term contains characters
// that FTS5's tokeniser would split on (causing the search to
// silently miss `email@domain.com` style strings). The tokeniser
// is the ICU/unicode61 default — anything outside `[A-Za-z0-9*_]`
// is a separator, so we wrap.
func needsAutoQuote(t string) bool {
	if t == "" {
		return false
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		switch c {
		case '*', '_':
			continue
		}
		return true
	}
	return false
}

// joinFTSWithFromOR joins parts with ` AND ` but groups the first
// `2*fromCount` parts (from_address + from_name pairs for each
// from: term) with ` OR ` so the FTS5 query matches either column.
// Without this, `from:bob` would compile to
// `from_address:bob AND from_name:bob` and require BOTH columns,
// which is wrong (Bob is usually only in one).
func joinFTSWithFromOR(parts []string, fromCount int) string {
	if len(parts) == 0 {
		return ""
	}
	pairs := fromCount * 2
	if pairs > len(parts) {
		pairs = len(parts)
	}
	var head []string
	for i := 0; i < fromCount; i++ {
		base := i * 2
		if base+1 >= pairs {
			head = append(head, parts[base])
			continue
		}
		head = append(head, "("+parts[base]+" OR "+parts[base+1]+")")
	}
	tail := parts[pairs:]
	all := append(head, tail...)
	return strings.Join(all, " AND ")
}
