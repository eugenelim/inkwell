package render

import (
	"strings"
	"unicode/utf8"
)

// normalisePlain folds CRLF, normalises quoting, soft-wraps to width,
// and extracts numbered links. The link extractor uses the same regex
// as the HTML path so output is consistent. urlMaxDisplay caps the
// visible OSC 8 text width (0 = no truncation); the URL portion stays
// full regardless. See [BodyOpts.URLDisplayMaxWidth].
func normalisePlain(content string, width, urlMaxDisplay int) (string, []ExtractedLink) {
	if width < 20 {
		width = 80
	}
	c := strings.ReplaceAll(content, "\r\n", "\n")
	c = strings.ReplaceAll(c, "\r", "\n")
	c = strings.TrimRight(c, " \n\t")

	var out strings.Builder
	for _, line := range strings.Split(c, "\n") {
		quoted, depth := stripQuoteMarkers(line)
		wrapped := softWrap(quoted, width-depth*2)
		for _, w := range wrapped {
			if depth > 0 {
				out.WriteString(strings.Repeat("> ", depth))
			}
			out.WriteString(w)
			out.WriteByte('\n')
		}
	}
	body := out.String()
	links := extractLinks(body)
	// Wrap inline URLs with OSC 8 hyperlink escapes. Supporting
	// terminals make them clickable so users don't drag-select
	// across pane borders. Done BEFORE the link block is appended
	// so the [N] references in the body itself become clickable.
	body = linkifyURLsInText(body, urlMaxDisplay)
	if len(links) > 0 {
		body += renderLinkBlock(links)
	}
	return body, links
}

// stripQuoteMarkers counts leading "> " markers and returns the
// stripped line plus the quote depth.
func stripQuoteMarkers(line string) (string, int) {
	depth := 0
	for {
		if strings.HasPrefix(line, "> ") {
			line = line[2:]
			depth++
			continue
		}
		if strings.HasPrefix(line, ">") {
			line = line[1:]
			depth++
			continue
		}
		break
	}
	return line, depth
}

// softWrap breaks long lines on whitespace at <= width.
func softWrap(line string, width int) []string {
	if width < 10 {
		width = 80
	}
	if utf8.RuneCountInString(line) <= width {
		return []string{line}
	}
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	cur := words[0]
	for _, w := range words[1:] {
		if utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(w) > width {
			out = append(out, cur)
			cur = w
			continue
		}
		cur += " " + w
	}
	out = append(out, cur)
	return out
}
