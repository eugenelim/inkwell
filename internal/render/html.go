package render

import (
	"regexp"

	"github.com/jaytaylor/html2text"
)

// trackingPixel matches 1x1 pixel <img> tags commonly used as trackers.
var trackingPixel = regexp.MustCompile(`(?i)<img[^>]*\s(?:width|height)\s*=\s*["']?1["']?[^>]*>`)

// htmlToText converts HTML to readable text via jaytaylor/html2text,
// with tracking-pixel <img> tags stripped before conversion. Returns
// the converted text plus the numbered link block. urlMaxDisplay is
// passed through to the OSC 8 hyperlink wrapping (0 = no
// truncation; see [BodyOpts.URLDisplayMaxWidth]).
func htmlToText(html string, width, urlMaxDisplay int) (string, []ExtractedLink, error) {
	cleaned := trackingPixel.ReplaceAllString(html, "")
	text, err := html2text.FromString(cleaned, html2text.Options{
		PrettyTables: false,
		OmitLinks:    false,
	})
	if err != nil {
		return "", nil, err
	}
	out, links := normalisePlain(text, width, urlMaxDisplay)
	return out, links, nil
}
