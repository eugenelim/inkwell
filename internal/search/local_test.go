package search

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseQueryFieldPrefixes covers the spec 06 §4.1 field-
// prefix syntax: `from:`, `subject:`, `body:` carve out per-field
// terms; everything else is plain text.
func TestParseQueryFieldPrefixes(t *testing.T) {
	cases := []struct {
		in   string
		want ParsedQuery
	}{
		{
			in:   "budget review",
			want: ParsedQuery{PlainTerms: []string{"budget", "review"}},
		},
		{
			in:   "from:bob q4",
			want: ParsedQuery{From: []string{"bob"}, PlainTerms: []string{"q4"}},
		},
		{
			in:   "subject:Q4 from:alice@example.invalid",
			want: ParsedQuery{From: []string{"alice@example.invalid"}, Subject: []string{"Q4"}},
		},
		{
			in:   `"q4 review" from:bob`,
			want: ParsedQuery{From: []string{"bob"}, PlainTerms: []string{`"q4 review"`}},
		},
		{
			in:   "body:deck",
			want: ParsedQuery{Body: []string{"deck"}},
		},
		{
			// Unknown prefix passes through as plain text.
			in:   "category:work",
			want: ParsedQuery{PlainTerms: []string{"category:work"}},
		},
		{
			// URL-shaped tokens shouldn't be split on `:`.
			in:   "https://example.invalid",
			want: ParsedQuery{PlainTerms: []string{"https://example.invalid"}},
		},
		{
			// Empty value falls through.
			in:   "from:",
			want: ParsedQuery{PlainTerms: []string{"from:"}},
		},
	}
	for _, c := range cases {
		got := ParseQuery(c.in)
		require.Equal(t, c.want, got, "input=%q", c.in)
	}
}

// TestBuildFTSQueryShapes pins the spec 06 §4.1 mapping table:
// plain terms → AND'd; quoted phrases preserved; field prefixes
// become column-scoped FTS5 expressions; multiple from: values OR.
func TestBuildFTSQueryShapes(t *testing.T) {
	cases := []struct {
		name string
		in   ParsedQuery
		want string
	}{
		{
			name: "plain ANDed",
			in:   ParsedQuery{PlainTerms: []string{"budget", "review"}},
			want: "budget AND review",
		},
		{
			name: "quoted phrase preserved",
			in:   ParsedQuery{PlainTerms: []string{`"q4 review"`}},
			want: `"q4 review"`,
		},
		{
			name: "OR operator preserved verbatim",
			in:   ParsedQuery{PlainTerms: []string{"bob", "OR", "alice"}},
			want: "bob AND OR AND alice",
		},
		{
			name: "from: produces OR-grouped column scopes",
			in:   ParsedQuery{From: []string{"bob"}, PlainTerms: []string{"q4"}},
			want: "(from_address:bob OR from_name:bob) AND q4",
		},
		{
			name: "subject: scopes to subject column",
			in:   ParsedQuery{Subject: []string{"Q4"}, PlainTerms: []string{"deck"}},
			want: "subject:Q4 AND deck",
		},
		{
			name: "body: scopes to body_preview column",
			in:   ParsedQuery{Body: []string{"deck"}},
			want: "body_preview:deck",
		},
		{
			name: "email-shaped term auto-quoted",
			in:   ParsedQuery{PlainTerms: []string{"alice@example.invalid"}},
			want: `"alice@example.invalid"`,
		},
		{
			name: "prefix * preserved",
			in:   ParsedQuery{PlainTerms: []string{"q4*"}},
			want: "q4*",
		},
		{
			name: "empty parsed yields empty",
			in:   ParsedQuery{},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, BuildFTSQuery(c.in))
		})
	}
}

// TestBuildGraphSearchQueryShapes pins the spec 06 §4.2 mapping
// to Graph's $search dialect.
func TestBuildGraphSearchQueryShapes(t *testing.T) {
	cases := []struct {
		name string
		in   ParsedQuery
		want string
	}{
		{
			name: "plain ANDed",
			in:   ParsedQuery{PlainTerms: []string{"budget", "review"}},
			want: "budget AND review",
		},
		{
			name: "from: uses graph field name",
			in:   ParsedQuery{From: []string{"bob"}, PlainTerms: []string{"q4"}},
			want: "from:bob AND q4",
		},
		{
			name: "subject: maps to subject",
			in:   ParsedQuery{Subject: []string{"Q4"}},
			want: "subject:Q4",
		},
		{
			name: "body: maps to body",
			in:   ParsedQuery{Body: []string{"deck"}},
			want: "body:deck",
		},
		{
			name: "multiple from: values OR-grouped",
			in:   ParsedQuery{From: []string{"bob", "alice"}},
			want: "(from:bob OR from:alice)",
		},
		{
			name: "email-shaped value auto-quoted",
			in:   ParsedQuery{From: []string{"bob@vendor.invalid"}},
			want: `from:"bob@vendor.invalid"`,
		},
		{
			name: "quoted phrase preserved",
			in:   ParsedQuery{PlainTerms: []string{`"q4 review"`}},
			want: `"q4 review"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, BuildGraphSearchQuery(c.in))
		})
	}
}

// TestHighlightSnippetCentersOnMatch covers the snippet builder:
// match-anchored window + ellipsis padding + asterisk emphasis
// around the matched term.
func TestHighlightSnippetCentersOnMatch(t *testing.T) {
	body := "Hi team, attaching the Q4 budget review deck for tomorrow's planning meeting. Please skim before."
	out := highlightSnippet(body, []string{"budget", "review"})
	require.Contains(t, out, "*budget*",
		"first match wraps with markdown emphasis")
	require.NotContains(t, out, "*review*",
		"only the FIRST match is emphasised — keeps the snippet readable")
}

// TestHighlightSnippetTruncatesWhenNoMatch confirms the no-match
// path still returns a usable preview rather than empty string.
func TestHighlightSnippetTruncatesWhenNoMatch(t *testing.T) {
	body := "Hi team, the Q4 deck is attached for review."
	out := highlightSnippet(body, []string{"nothing-matches"})
	require.NotEmpty(t, out)
	require.NotContains(t, out, "*")
}

// TestHighlightSnippetCollapsesWhitespace folds tabs/newlines so
// list-pane snippets render as one tight line.
func TestHighlightSnippetCollapsesWhitespace(t *testing.T) {
	body := "Hi\tteam,\n\nthe\tQ4 deck."
	out := highlightSnippet(body, []string{"deck"})
	require.NotContains(t, out, "\t")
	require.NotContains(t, out, "\n")
	require.Contains(t, out, "*deck*")
}
