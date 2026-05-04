package render

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// attributionLine matches "On <date>, <person> wrote:" (RFC 2822 reply attribution).
var attributionLine = regexp.MustCompile(`(?i)^On .{1,80}, .{1,80} wrote:$`)

// normalisePlain folds CRLF, optionally unwraps format=flowed content,
// normalises quoting, dims attribution lines, soft-wraps to width,
// and extracts numbered links. The link extractor uses the same regex
// as the HTML path so output is consistent. urlMaxDisplay caps the
// visible OSC 8 text width (0 = no truncation); the URL portion stays
// full regardless. See [BodyOpts.URLDisplayMaxWidth]. quoteThreshold
// collapses runs of quoted lines at depth ≥ threshold (0 = disabled).
func normalisePlain(content string, width, urlMaxDisplay, quoteThreshold int) (string, []ExtractedLink) {
	if width < 20 {
		width = 80
	}
	c := strings.ReplaceAll(content, "\r\n", "\n")
	c = strings.ReplaceAll(c, "\r", "\n")
	c = strings.TrimRight(c, " \n\t")

	if isFormatFlowed(c) {
		c = unwrapFormatFlowed(c)
	}

	// Stitch URL fragments hard-wrapped across newlines by the
	// sender's MUA back together so extractLinks captures the full
	// URL. See unwrapBrokenURLs in links.go.
	c = unwrapBrokenURLs(c)

	var out strings.Builder
	for _, line := range strings.Split(c, "\n") {
		quoted, depth := stripQuoteMarkers(line)
		// Apply attribution-line dimming before soft-wrapping.
		if depth == 0 && attributionLine.MatchString(strings.TrimSpace(quoted)) {
			// dim ANSI: wrap the attribution line in dim markers.
			out.WriteString("\x1b[2m")
			out.WriteString(quoted)
			out.WriteString("\x1b[0m")
			out.WriteByte('\n')
			continue
		}
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
	if quoteThreshold > 0 {
		body = collapseQuotes(body, quoteThreshold)
	}
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

// isFormatFlowed detects whether content looks like format=flowed
// (RFC 2646): at least 20% of non-blank lines end with a trailing space.
func isFormatFlowed(content string) bool {
	lines := strings.Split(content, "\n")
	nonBlank := 0
	trailingSpace := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		nonBlank++
		if strings.HasSuffix(l, " ") {
			trailingSpace++
		}
	}
	if nonBlank == 0 {
		return false
	}
	return float64(trailingSpace)/float64(nonBlank) >= 0.2
}

// unwrapFormatFlowed joins soft-wrapped lines (those ending with a
// trailing space) per RFC 2646 §4.2. A line ending with a space is a
// soft-wrap continuation: the trailing space is the word separator
// before the content of the following line.
func unwrapFormatFlowed(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	cur := ""
	for _, l := range lines {
		if cur != "" {
			// cur ends with a space (soft-wrap marker); append next line.
			cur = cur + l
		} else {
			cur = l
		}
		if strings.HasSuffix(cur, " ") {
			// Still soft-wrapped; keep accumulating.
			continue
		}
		result = append(result, cur)
		cur = ""
	}
	if cur != "" {
		result = append(result, cur)
	}
	return strings.Join(result, "\n")
}

// collapseQuotes replaces runs of consecutive lines at quote depth ≥
// threshold with a single "[… N quoted lines]" summary. Lines at
// depth < threshold pass through unchanged.
func collapseQuotes(text string, threshold int) string {
	if threshold <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	var out []string
	runLen := 0
	for _, l := range lines {
		_, depth := stripQuoteMarkers(l)
		if depth >= threshold {
			runLen++
		} else {
			if runLen > 0 {
				out = append(out, fmt.Sprintf("[… %d quoted lines]", runLen))
				runLen = 0
			}
			out = append(out, l)
		}
	}
	if runLen > 0 {
		out = append(out, fmt.Sprintf("[… %d quoted lines]", runLen))
	}
	return strings.Join(out, "\n")
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
