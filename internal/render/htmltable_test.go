package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// fixturePath joins the testdata/tables/ relative path the corpus
// lives at.
func fixturePath(name string) string {
	return filepath.Join("testdata", "tables", name)
}

// classifyHTML is a test convenience: parse the html, classify, and
// return the classifications keyed by ordinal (depth-first encounter
// of <table>).
func classifyHTML(t *testing.T, raw string) []tableClass {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(raw))
	require.NoError(t, err)
	classes := map[*html.Node]tableClass{}
	classifyAll(doc, classes)
	var out []tableClass
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Table {
			out = append(out, classes[n])
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// TestClassifyMinimalFixtures asserts each hand-crafted boundary
// fixture lands on the expected classifier branch (spec 05 §6.1.1).
func TestClassifyMinimalFixtures(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		// expected is the depth-first sequence of table classifications.
		// `min_*` fixtures have one table each.
		expected []tableClass
	}{
		{"data with thead", "min_data_with_thead.eml", []tableClass{classData}},
		{"data without thead", "min_data_no_thead.eml", []tableClass{classData}},
		{"layout marketing", "min_layout_marketing.eml", []tableClass{classLayout, classLayout, classLayout}},
		{"single-cell wrapper", "min_single_cell_wrapper.eml", []tableClass{classLayout}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyHTML(t, loadEmlHTML(t, fixturePath(tc.fixture)))
			require.Equal(t, tc.expected, got)
		})
	}
}

// TestClassifyNestedDataInLayout verifies the canonical case: an
// outer layout-table-of-layout-tables wrapping an inner data table.
// Outer tables hit rule 3 (nested table descendant); inner data
// table hits rule 2 (<th> own descendant).
func TestClassifyNestedDataInLayout(t *testing.T) {
	got := classifyHTML(t, loadEmlHTML(t, fixturePath("min_nested_data_in_layout.eml")))
	// Two outer layout tables (root wrapper + 600-px container) and
	// one inner data table.
	require.Equal(t, []tableClass{classLayout, classLayout, classData}, got)
}

// TestClassifyRolePresentationOverridesTh confirms rule 1 wins over
// rule 2: a presentation-role table containing a <th> still lands as
// layout.
func TestClassifyRolePresentationOverridesTh(t *testing.T) {
	raw := `<table role="presentation"><tr><th>x</th></tr><tr><td>y</td></tr></table>`
	got := classifyHTML(t, raw)
	require.Equal(t, []tableClass{classLayout}, got)
}

// TestClassifyInconsistentRowsLayout exercises rule 5: rows with
// different cell counts and no <th> classify as layout.
func TestClassifyInconsistentRowsLayout(t *testing.T) {
	raw := `<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>`
	got := classifyHTML(t, raw)
	require.Equal(t, []tableClass{classLayout}, got)
}

// TestClassifyLongFirstRowFallsToLayout exercises rule 6 negation:
// rectangular rows but a long first-row cell isn't header-shaped.
func TestClassifyLongFirstRowFallsToLayout(t *testing.T) {
	raw := `<table><tr><td>` + strings.Repeat("verylong", 10) + `</td><td>b</td></tr><tr><td>x</td><td>y</td></tr></table>`
	got := classifyHTML(t, raw)
	require.Equal(t, []tableClass{classLayout}, got)
}

// TestRewriteUnwrapsLayoutKeepsData verifies the rewrite output: a
// layout outer table is renamed to <div>, its inner data table
// survives unchanged.
func TestRewriteUnwrapsLayoutKeepsData(t *testing.T) {
	raw := loadEmlHTML(t, fixturePath("min_nested_data_in_layout.eml"))
	got := classifyTables(raw, 80, 50)
	// Inner data table is preserved.
	require.Contains(t, got, "<table")
	// Outer layout structural tags must not survive — they were renamed.
	// Count remaining tables: should be exactly one (the inner data).
	require.Equal(t, 1, strings.Count(got, "<table"), "only the inner data table should remain")
}

// TestRewritePlaceholderForOversizedRows checks the row-count guard:
// a data table with rows above max is replaced with the placeholder.
func TestRewritePlaceholderForOversizedRows(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<table><thead><tr><th>a</th><th>b</th></tr></thead><tbody>`)
	for i := 0; i < 60; i++ {
		b.WriteString(`<tr><td>x</td><td>y</td></tr>`)
	}
	b.WriteString(`</tbody></table>`)
	got := classifyTables(b.String(), 80, 50)
	require.Contains(t, got, "Wide table")
	require.Contains(t, got, "61×2") // 60 body rows + 1 thead row × 2 cols
	require.NotContains(t, got, "<table")
}

// TestRewritePlaceholderForOversizedWidth checks the width guard.
func TestRewritePlaceholderForOversizedWidth(t *testing.T) {
	wide := strings.Repeat("X", 200)
	raw := `<table><thead><tr><th>` + wide + `</th><th>` + wide + `</th></tr></thead><tr><td>a</td><td>b</td></tr></table>`
	got := classifyTables(raw, 80, 50)
	require.Contains(t, got, "Wide table")
	require.NotContains(t, got, "<table")
}

// TestRewritePreservesSmallDataTable confirms a within-budget data
// table passes through with its <table> shell intact (so html2text
// will pretty-render it downstream).
func TestRewritePreservesSmallDataTable(t *testing.T) {
	raw := loadEmlHTML(t, fixturePath("min_data_with_thead.eml"))
	got := classifyTables(raw, 80, 50)
	require.Contains(t, got, "<table")
	require.Contains(t, got, "Northwind")
}

// TestEndToEndPlaceholderSurfaces verifies the user-visible output
// contains the "[Wide table — N×M, …]" placeholder when a data table
// trips the row-count sizing guard. This is the surface the user
// actually sees — TestRewritePlaceholderForOversizedRows above only
// inspects the post-classifyTables HTML.
func TestEndToEndPlaceholderSurfaces(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<table><thead><tr><th>a</th><th>b</th></tr></thead><tbody>`)
	for i := 0; i < 60; i++ {
		b.WriteString(`<tr><td>x</td><td>y</td></tr>`)
	}
	b.WriteString(`</tbody></table>`)
	text, _, err := htmlToText(b.String(), 80, 0, Theme{}, true, 50)
	require.NoError(t, err)
	require.Contains(t, text, "Wide table")
	require.Contains(t, text, "press O")
}

// TestEndToEndDataTableRendersAsBox runs the full htmlToText pipeline
// over the data-with-thead fixture with PrettyTables on, asserting
// the output contains box-drawing characters from tablewriter.
func TestEndToEndDataTableRendersAsBox(t *testing.T) {
	raw := loadEmlHTML(t, fixturePath("min_data_with_thead.eml"))
	text, _, err := htmlToText(raw, 120, 0, Theme{}, true, 50)
	require.NoError(t, err)
	// tablewriter draws ASCII grids. At minimum we expect the column
	// header text plus pipe-or-dash separators on a row of their own.
	require.Contains(t, text, "ACCOUNT")
	require.Contains(t, text, "Northwind")
	// Look for any of tablewriter's separator glyphs. Different
	// tablewriter versions use different default border chars; we
	// allow any common one.
	hasSeparator := strings.ContainsAny(text, "|+-─│┼")
	require.True(t, hasSeparator, "expected an ASCII box-drawing separator in output:\n%s", text)
}

// TestEndToEndLayoutTableFlattens runs the full pipeline over the
// marketing layout fixture: no box-drawing should appear (every
// table is layout) and the body text should be readable.
func TestEndToEndLayoutTableFlattens(t *testing.T) {
	raw := loadEmlHTML(t, fixturePath("min_layout_marketing.eml"))
	text, _, err := htmlToText(raw, 80, 0, Theme{}, true, 50)
	require.NoError(t, err)
	require.Contains(t, text, "Spring Sale")
	require.Contains(t, text, "SPRING20")
	// No grid lines — every table is layout.
	require.False(t, strings.ContainsAny(text, "│┼├┤┬┴"),
		"layout-only fixture must not produce box drawing:\n%s", text)
}

// TestEndToEndNestedDataInLayout verifies the canonical mixed case:
// outer layout flattens, inner data table renders as a grid.
func TestEndToEndNestedDataInLayout(t *testing.T) {
	raw := loadEmlHTML(t, fixturePath("min_nested_data_in_layout.eml"))
	text, _, err := htmlToText(raw, 120, 0, Theme{}, true, 50)
	require.NoError(t, err)
	require.Contains(t, text, "Order")
	require.Contains(t, text, "Notebook A5")
	hasSeparator := strings.ContainsAny(text, "|+-─│┼")
	require.True(t, hasSeparator, "inner data table must render as a grid:\n%s", text)
}

// TestEndToEndRealNewsletter runs the pipeline over the real
// Mailteorite newsletter (50 nested tables, 3 <th> in the inner
// data table). The data table must surface as a grid; the layout
// chrome must flatten without producing nested ASCII boxes.
func TestEndToEndRealNewsletter(t *testing.T) {
	raw := loadEmlHTML(t, fixturePath("real_newsletter_data_analysis.eml"))
	text, _, err := htmlToText(raw, 120, 0, Theme{}, true, 50)
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(text))
	// The fixture contains a finance "Top Performers" section — the
	// inner data table — and box drawing should appear at least once.
	hasSeparator := strings.ContainsAny(text, "|+-─│┼")
	require.True(t, hasSeparator, "newsletter inner data table must render as a grid")
	// Sanity: the body should still be smaller than the raw HTML.
	require.Less(t, len(text), len(raw))
}

// TestEndToEndRealLayoutOnlyFlattens runs the pipeline over the
// real review-request fixture (50 nested tables, zero <th>):
// every table is layout; output must not contain box drawing.
func TestEndToEndRealLayoutOnlyFlattens(t *testing.T) {
	for _, fix := range []string{"real_review_request.eml", "real_card_shipped.eml"} {
		t.Run(fix, func(t *testing.T) {
			raw := loadEmlHTML(t, fixturePath(fix))
			text, _, err := htmlToText(raw, 120, 0, Theme{}, true, 50)
			require.NoError(t, err)
			require.NotEmpty(t, strings.TrimSpace(text))
			require.False(t, strings.ContainsAny(text, "│┼├┤┬┴"),
				"layout-only fixture %s must not produce box drawing:\n%s", fix, text)
		})
	}
}

// TestPrettyTablesOffMatchesV017Behavior confirms the kill-switch:
// with PrettyTables=false, every table flattens (the v0.17.x
// behavior) and the classifier is bypassed entirely.
func TestPrettyTablesOffMatchesV017Behavior(t *testing.T) {
	raw := loadEmlHTML(t, fixturePath("min_data_with_thead.eml"))
	text, _, err := htmlToText(raw, 120, 0, Theme{}, false, 50)
	require.NoError(t, err)
	require.Contains(t, text, "Northwind")
	require.False(t, strings.ContainsAny(text, "│┼├┤┬┴"),
		"PrettyTables=false must never produce box drawing")
}

// BenchmarkClassifyRealNewsletter measures the classifier-walk cost
// over a real ~50-nested-<table> body. Spec 05 §14 budget: <10ms.
func BenchmarkClassifyRealNewsletter(b *testing.B) {
	raw := loadEmlHTML(b, fixturePath("real_newsletter_data_analysis.eml"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifyTables(raw, 120, 50)
	}
}

// BenchmarkHTMLToTextRealNewsletter measures end-to-end render cost
// (classifier + html2text + normalisePlain). Spec 05 §14 budget:
// <100ms for HTML body render with classifier active.
func BenchmarkHTMLToTextRealNewsletter(b *testing.B) {
	raw := loadEmlHTML(b, fixturePath("real_newsletter_data_analysis.eml"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := htmlToText(raw, 120, 0, Theme{}, true, 50)
		if err != nil {
			b.Fatal(err)
		}
	}
}
