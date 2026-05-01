package search

import (
	"strings"
)

// BuildGraphSearchQuery translates a ParsedQuery into the Graph
// $search dialect. Spec 06 §4.2.
//
// Differences from the FTS5 form:
//   - Field prefixes use Graph-native names (`from`, `subject`,
//     `body`) rather than the SQLite column names.
//   - Auto-quoting still applies to phrases (Graph uses the same
//     KQL-ish quoting convention).
//   - AND is the default join (matches Graph semantics).
//
// Multiple `from:` values become an OR group inside parens so the
// server matches either one without requiring both — same intent
// as the FTS5 builder.
func BuildGraphSearchQuery(p ParsedQuery) string {
	var parts []string

	addField := func(name string, values []string) {
		switch len(values) {
		case 0:
			return
		case 1:
			parts = append(parts, name+":"+graphTerm(values[0]))
			return
		}
		var or []string
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			or = append(or, name+":"+graphTerm(v))
		}
		switch len(or) {
		case 0:
			return
		case 1:
			parts = append(parts, or[0])
		default:
			parts = append(parts, "("+strings.Join(or, " OR ")+")")
		}
	}

	addField("from", p.From)
	addField("subject", p.Subject)
	addField("body", p.Body)

	for _, t := range p.PlainTerms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		parts = append(parts, graphTerm(t))
	}
	return strings.Join(parts, " AND ")
}

// graphTerm returns the Graph-side form of a single term.
// Phrases stay quoted; bare words pass through. Email-shaped
// strings (`bob@vendor.invalid`) are auto-quoted because Graph's
// $search tokeniser otherwise breaks on the `@` and `.`.
func graphTerm(t string) string {
	switch t {
	case "AND", "OR", "NOT":
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
