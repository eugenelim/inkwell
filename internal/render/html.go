package render

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"

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
	out, links := normalisePlain(text, width, urlMaxDisplay, 0)
	return out, links, nil
}

// htmlToTextWithConfig converts HTML using the renderer's configured
// backend. Falls back to the internal html2text path on error.
func (r *renderer) htmlToTextWithConfig(html string, width, urlMaxDisplay int) (string, []ExtractedLink, error) {
	if r.htmlConverter == "external" && r.htmlConverterCmd != "" {
		text, err := runExternalConverter(r.htmlConverterCmd, html, r.externalConverterTimeout)
		if err == nil {
			out, links := normalisePlain(text, width, urlMaxDisplay, 0)
			return out, links, nil
		}
		if r.logger != nil {
			r.logger.Warn("external html converter failed, falling back to internal", slog.String("err", err.Error()))
		}
	}
	return htmlToText(html, width, urlMaxDisplay)
}

// runExternalConverter pipes html into cmd's stdin and returns stdout.
func runExternalConverter(cmd, html string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	parts := strings.Fields(cmd)
	// #nosec G204 — cmd is the user-configured HTMLConverterCmd from config.toml.
	// The user controls their own config file on their own machine; this is
	// equivalent to launching a user-specified pager or editor. Documented as such.
	c := exec.CommandContext(ctx, parts[0], parts[1:]...) //nolint:gosec
	c.Stdin = strings.NewReader(html)
	var stdout bytes.Buffer
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}
