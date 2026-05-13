package compose

import (
	"bytes"
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// DraftBody carries the compose body through the dispatch pipeline.
// ContentType is "text" for plain or "html" for Markdown-rendered
// content. Defined here (internal/compose) so both internal/ui and
// internal/action can import it without creating an upward
// dependency.
type DraftBody struct {
	Content     string
	ContentType string
}

// RenderMarkdown converts CommonMark Markdown to an HTML fragment
// using goldmark with GFM table, strikethrough, task-list, and
// autolink extensions. Returns a self-contained HTML fragment (no
// <html>/<body> wrapper) suitable for Graph's body.content. The
// error is very unlikely with stock extensions and a bytes.Buffer
// writer (no I/O failure path), but the guard is retained for
// forward-compatibility with future parser extensions.
func RenderMarkdown(src string) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			extension.Strikethrough,
			extension.TaskList,
			extension.Linkify,
		),
	)
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "", fmt.Errorf("markdown render: %w", err)
	}
	return buf.String(), nil
}
