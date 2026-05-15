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
// with tracking-pixel <img> tags stripped before conversion. When
// prettyTables is true, layout <table>s are pre-rewritten to flat
// <div>/<span> via classifyTables (spec 05 §6.1.1) so html2text only
// pretty-renders real data tables. urlMaxDisplay is passed through
// to OSC 8 hyperlink wrapping (0 = no truncation; see
// [BodyOpts.URLDisplayMaxWidth]).
func htmlToText(rawHTML string, width, urlMaxDisplay int, theme Theme, prettyTables bool, prettyTableMaxRows int) (string, []ExtractedLink, error) {
	cleaned := trackingPixel.ReplaceAllString(rawHTML, "")
	processed := cleaned
	if prettyTables {
		processed = classifyTables(cleaned, width, prettyTableMaxRows)
	}
	text, err := html2text.FromString(processed, html2text.Options{
		PrettyTables: prettyTables,
		OmitLinks:    false,
	})
	if err != nil {
		return "", nil, err
	}
	out, links := normalisePlain(text, width, urlMaxDisplay, 0, theme)
	return out, links, nil
}

// htmlToTextWithConfig converts HTML using the renderer's configured
// backend. Falls back to the internal html2text path on error.
func (r *renderer) htmlToTextWithConfig(rawHTML string, width, urlMaxDisplay int, theme Theme) (string, []ExtractedLink, error) {
	if r.htmlConverter == "external" && r.htmlConverterCmd != "" {
		text, err := runExternalConverter(r.htmlConverterCmd, rawHTML, r.externalConverterTimeout)
		if err == nil {
			out, links := normalisePlain(text, width, urlMaxDisplay, 0, theme)
			return out, links, nil
		}
		if r.logger != nil {
			r.logger.Warn("external html converter failed, falling back to internal", slog.String("err", err.Error()))
		}
	}
	return htmlToText(rawHTML, width, urlMaxDisplay, theme, r.prettyTables, r.prettyTableMaxRows)
}

// DecodeForIndex returns the decoded plaintext of rawHTML for the
// body index (spec 35 §6.3). It runs the same trackingPixel strip +
// html2text.FromString pipeline as the viewer renderer but skips the
// width-wrap pass in [normalisePlain] — indexing wrapped text would
// silently break `~b /token=abc/` matches when the wrapper inserts a
// newline mid-token. Whitespace runs are collapsed per line so
// `token=abc` survives intact. For `text/plain` bodies, rawHTML may
// already be plain text; the html2text pass on plain text is a near
// no-op (handles stray entities) and we still collapse whitespace.
func DecodeForIndex(rawHTML string) (string, error) {
	cleaned := trackingPixel.ReplaceAllString(rawHTML, "")
	text, err := html2text.FromString(cleaned, html2text.Options{
		PrettyTables: false,
		OmitLinks:    false,
	})
	if err != nil {
		return "", err
	}
	// Collapse runs of whitespace per line; keep newlines as paragraph
	// separators. Do NOT word-wrap — the index needs un-wrapped tokens.
	var b strings.Builder
	b.Grow(len(text))
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		// strings.Fields collapses spaces, tabs, NBSPs into single spaces.
		b.WriteString(strings.Join(strings.Fields(line), " "))
	}
	return b.String(), nil
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
