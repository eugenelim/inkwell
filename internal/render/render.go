package render

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/eugenelim/inkwell/internal/store"
)

// BodyState enumerates the visible state of a body fetch.
type BodyState int

const (
	// BodyReady means [BodyView.Text] is the actual rendered body.
	BodyReady BodyState = iota
	// BodyFetching means a placeholder is shown; an async fetch is in flight.
	BodyFetching
	// BodyError means [BodyView.Text] contains a user-facing error string.
	BodyError
)

// BodyView is what the viewer pane displays.
type BodyView struct {
	Text  string
	Links []ExtractedLink
	State BodyState
}

// ExtractedLink is one numbered hyperlink.
type ExtractedLink struct {
	Index int
	URL   string
	Text  string
}

// BodyOpts narrows [Renderer.Body].
type BodyOpts struct {
	Width           int
	ShowFullHeaders bool
	Theme           Theme
}

// BodyFetcher is the seam to [internal/graph.Client.GetMessageBody].
// Defined at the consumer site so render does not import graph
// (CLAUDE.md §2: small consumer-side interfaces).
type BodyFetcher interface {
	FetchBody(ctx context.Context, messageID string) (FetchedBody, error)
}

// FetchedBody is the typed result of [BodyFetcher.FetchBody].
type FetchedBody struct {
	ContentType string // "text" | "html"
	Content     string
}

// Renderer is the viewer-side rendering API. Stateless beyond its
// dependencies; callers may share one instance across goroutines.
type Renderer interface {
	Headers(m *store.Message, opts BodyOpts) string
	Body(ctx context.Context, m *store.Message, opts BodyOpts) (BodyView, error)
	Attachments(atts []store.Attachment, theme Theme) string
}

// New constructs a Renderer.
func New(st store.Store, fetcher BodyFetcher) Renderer {
	return &renderer{store: st, fetcher: fetcher}
}

type renderer struct {
	store   store.Store
	fetcher BodyFetcher
}

// Body implements [Renderer]. On a cache hit it renders inline; on a
// miss it returns [BodyFetching] and the caller is expected to dispatch
// an async fetch via [FetchBodyAsync] (which the viewer wires as a
// tea.Cmd).
func (r *renderer) Body(ctx context.Context, m *store.Message, opts BodyOpts) (BodyView, error) {
	if m == nil {
		return BodyView{State: BodyError, Text: "no message"}, errors.New("render: nil message")
	}
	got, err := r.store.GetBody(ctx, m.ID)
	if err == nil {
		_ = r.store.TouchBody(ctx, m.ID)
		return r.renderBody(*got, opts), nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return BodyView{State: BodyError, Text: "store error: " + err.Error()}, err
	}
	return BodyView{State: BodyFetching, Text: "Loading message…"}, nil
}

// FetchBodyAsync performs the on-demand fetch and persists the result.
// Returns the rendered BodyView ready for the viewer to display.
func (r *renderer) FetchBodyAsync(ctx context.Context, m *store.Message, opts BodyOpts) (BodyView, error) {
	if r.fetcher == nil {
		return BodyView{State: BodyError, Text: "no fetcher configured"}, errors.New("render: nil fetcher")
	}
	fb, err := r.fetcher.FetchBody(ctx, m.ID)
	if err != nil {
		return BodyView{State: BodyError, Text: "fetch failed"}, err
	}
	body := store.Body{
		MessageID:      m.ID,
		ContentType:    fb.ContentType,
		Content:        fb.Content,
		ContentSize:    int64(len(fb.Content)),
		FetchedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}
	if err := r.store.PutBody(ctx, body); err != nil {
		return BodyView{State: BodyError, Text: "cache write failed"}, err
	}
	return r.renderBody(body, opts), nil
}

func (r *renderer) renderBody(b store.Body, opts BodyOpts) BodyView {
	width := opts.Width
	if width <= 0 {
		width = 80
	}
	switch strings.ToLower(b.ContentType) {
	case "html":
		text, links, err := htmlToText(b.Content, width)
		if err != nil {
			return BodyView{State: BodyError, Text: "html conversion failed"}
		}
		return BodyView{State: BodyReady, Text: text, Links: links}
	default:
		text, links := normalisePlain(b.Content, width)
		return BodyView{State: BodyReady, Text: text, Links: links}
	}
}
