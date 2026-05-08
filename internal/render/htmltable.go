package render

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// classifyTables walks rawHTML, classifies every <table> per spec 05
// §6.1.1, and rewrites layout tables as generic <div>/<span> blocks so
// the downstream html2text pass sees flowing text instead of nested
// boxes. Oversize data tables are replaced with a placeholder
// paragraph. Surviving data tables are passed through unchanged so
// html2text (with PrettyTables: true) renders them as ASCII grids.
//
// paneWidth feeds the 2× sizing guard. maxRows is the per-table row
// ceiling; data tables exceeding it are downgraded.
//
// On parse failure the input is returned unchanged.
func classifyTables(rawHTML string, paneWidth, maxRows int) string {
	if paneWidth <= 0 {
		paneWidth = 80
	}
	if maxRows <= 0 {
		maxRows = 50
	}
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return rawHTML
	}
	// Per HTML5 spec, URL attributes (href, src, action) must have control
	// characters including CR, LF, and TAB stripped. Some HTML email generators
	// split long href values across lines; those embedded newlines survive
	// html.Parse unchanged and cause the URL to appear split in html2text's
	// output, truncating whatever the regex captures at the first newline.
	sanitizeURLAttrs(doc)
	classes := map[*html.Node]tableClass{}
	classifyAll(doc, classes)
	rewriteAll(doc, classes, paneWidth, maxRows, false)
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return rawHTML
	}
	return buf.String()
}

// sanitizeURLAttrs strips CR, LF, and TAB from URL-type attributes (href,
// src, action) on all elements in the subtree rooted at n. This implements
// the HTML5 spec requirement that URL attribute values be stripped of
// ASCII control characters before use.
func sanitizeURLAttrs(n *html.Node) {
	if n.Type == html.ElementNode {
		for i, attr := range n.Attr {
			switch strings.ToLower(attr.Key) {
			case "href", "src", "action":
				stripped := strings.Map(func(r rune) rune {
					if r == '\r' || r == '\n' || r == '\t' {
						return -1
					}
					return r
				}, attr.Val)
				if stripped != attr.Val {
					n.Attr[i].Val = stripped
				}
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sanitizeURLAttrs(c)
	}
}

type tableClass int

const (
	classLayout tableClass = iota
	classData
)

func classifyAll(n *html.Node, m map[*html.Node]tableClass) {
	if n.Type == html.ElementNode && n.DataAtom == atom.Table {
		m[n] = classify(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		classifyAll(c, m)
	}
}

// classify implements the 7-rule heuristic from spec 05 §6.1.1.
// Descendants reached via a nested <table> belong to that nested
// table, not this one — see hasOwnDescendant / ownRows / textContentOwn.
func classify(table *html.Node) tableClass {
	for _, attr := range table.Attr {
		if strings.EqualFold(attr.Key, "role") && strings.EqualFold(attr.Val, "presentation") {
			return classLayout
		}
	}
	if hasOwnDescendant(table, atom.Th) {
		return classData
	}
	if hasNestedTable(table) {
		return classLayout
	}
	rows := ownRows(table)
	if len(rows) == 1 && len(ownCells(rows[0])) == 1 {
		return classLayout
	}
	if !consistentCellCount(rows) {
		return classLayout
	}
	if len(rows) >= 2 {
		cells := ownCells(rows[0])
		if n := len(cells); n >= 2 && n <= 8 && allShort(cells, 30) {
			return classData
		}
	}
	return classLayout
}

// hasOwnDescendant reports whether n contains a descendant element of
// the given atom that is not inside a nested <table>.
func hasOwnDescendant(n *html.Node, target atom.Atom) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.DataAtom == atom.Table {
			continue
		}
		if c.DataAtom == target {
			return true
		}
		if hasOwnDescendant(c, target) {
			return true
		}
	}
	return false
}

// hasNestedTable reports whether n contains any <table> descendant at
// any depth (in contrast to hasOwnDescendant which stops at table
// boundaries).
func hasNestedTable(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.DataAtom == atom.Table {
			return true
		}
		if hasNestedTable(c) {
			return true
		}
	}
	return false
}

// ownRows returns every <tr> belonging to table (skipping rows that
// live inside a nested <table>). Walks through optional <thead> /
// <tbody> / <tfoot> wrappers.
func ownRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.DataAtom == atom.Table {
				continue
			}
			if c.DataAtom == atom.Tr {
				rows = append(rows, c)
				continue
			}
			walk(c)
		}
	}
	walk(table)
	return rows
}

// ownCells returns the <td>/<th> direct-element children of row.
func ownCells(row *html.Node) []*html.Node {
	var cells []*html.Node
	for c := row.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.DataAtom == atom.Td || c.DataAtom == atom.Th {
			cells = append(cells, c)
		}
	}
	return cells
}

func consistentCellCount(rows []*html.Node) bool {
	if len(rows) == 0 {
		return false
	}
	first := len(ownCells(rows[0]))
	if first == 0 {
		return false
	}
	for _, r := range rows[1:] {
		if len(ownCells(r)) != first {
			return false
		}
	}
	return true
}

func allShort(cells []*html.Node, maxLen int) bool {
	for _, c := range cells {
		if len(strings.TrimSpace(textContentOwn(c))) > maxLen {
			return false
		}
	}
	return true
}

// textContentOwn returns the concatenated text content of n,
// excluding text inside any nested <table>.
func textContentOwn(n *html.Node) string {
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			buf.WriteString(x.Data)
			return
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.DataAtom == atom.Table {
				continue
			}
			walk(c)
		}
	}
	walk(n)
	return buf.String()
}

// rewriteAll walks the tree and applies the rewrite rules:
//   - layout <table> → <div>; structural descendants (<tr>, <td>,
//     <thead>, etc.) renamed to <div>/<span>
//   - data <table>: kept intact, unless oversized → placeholder <p>
//
// rewriteStructural says whether ancestor context is a layout table
// (i.e. a `<tr>` outside any layout context is a stray and gets
// renamed only when we have already entered one).
func rewriteAll(n *html.Node, classes map[*html.Node]tableClass, paneWidth, maxRows int, rewriteStructural bool) {
	if n.Type == html.ElementNode && n.DataAtom == atom.Table {
		cls := classes[n]
		if cls == classData {
			rows := ownRows(n)
			if len(rows) > maxRows || estimatedWidth(rows) > 2*paneWidth {
				replaceWithPlaceholder(n, len(rows), maxColumnCount(rows))
				return
			}
			for c := n.FirstChild; c != nil; {
				next := c.NextSibling
				rewriteAll(c, classes, paneWidth, maxRows, false)
				c = next
			}
			return
		}
		n.DataAtom = atom.Div
		n.Data = "div"
		n.Attr = nil
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			rewriteAll(c, classes, paneWidth, maxRows, true)
			c = next
		}
		return
	}
	if rewriteStructural && n.Type == html.ElementNode {
		switch n.DataAtom {
		case atom.Tr, atom.Thead, atom.Tbody, atom.Tfoot, atom.Caption:
			n.DataAtom = atom.Div
			n.Data = "div"
			n.Attr = nil
		case atom.Td, atom.Th:
			n.DataAtom = atom.Span
			n.Data = "span"
			n.Attr = nil
			n.AppendChild(&html.Node{Type: html.TextNode, Data: " "})
		case atom.Col, atom.Colgroup:
			parent := n.Parent
			if parent != nil {
				parent.RemoveChild(n)
			}
			return
		}
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		rewriteAll(c, classes, paneWidth, maxRows, rewriteStructural)
		c = next
	}
}

// estimatedWidth approximates how wide tablewriter would render the
// given rows: sum of max column widths + ~3 chars of separator
// overhead per column boundary. Cell length is measured as byte
// count; for non-ASCII content this slightly overestimates, which is
// the safe direction for a "too wide" guard.
func estimatedWidth(rows []*html.Node) int {
	colWidths := map[int]int{}
	for _, r := range rows {
		for i, c := range ownCells(r) {
			w := len(strings.TrimSpace(textContentOwn(c)))
			if w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}
	if len(colWidths) == 0 {
		return 0
	}
	total := 0
	for _, w := range colWidths {
		total += w
	}
	return total + 3*(len(colWidths)+1)
}

func maxColumnCount(rows []*html.Node) int {
	maxN := 0
	for _, r := range rows {
		if n := len(ownCells(r)); n > maxN {
			maxN = n
		}
	}
	return maxN
}

func replaceWithPlaceholder(table *html.Node, nRows, nCols int) {
	parent := table.Parent
	if parent == nil {
		return
	}
	p := &html.Node{Type: html.ElementNode, DataAtom: atom.P, Data: "p"}
	p.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: fmt.Sprintf("[Wide table — %d×%d, omitted; press O to view in browser]", nRows, nCols),
	})
	parent.InsertBefore(p, table)
	parent.RemoveChild(table)
}
