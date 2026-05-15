package render

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/eugenelim/inkwell/internal/store"
)

// Options tunes the renderer at construction time. All fields are optional.
type Options struct {
	// WrapColumns overrides the computed pane width for body soft-wrapping.
	// 0 means use the width supplied in each BodyOpts call.
	WrapColumns int
	// QuoteCollapseThreshold collapses runs of quoted lines at depth ≥ this
	// value to a single "[… N quoted lines]" summary. 0 disables collapsing.
	QuoteCollapseThreshold int
	// StripPatterns are pre-compiled regexps whose matching lines are removed
	// from the plain-text body before rendering. When nil, built-in
	// Outlook-noise defaults apply.
	StripPatterns []*regexp.Regexp
	// HTMLConverter selects the HTML→text backend: "internal" (default) or
	// "external". When "external" and HTMLConverterCmd is non-empty, the
	// command is used; on failure it falls back to the internal path.
	HTMLConverter string
	// HTMLConverterCmd is the command run when HTMLConverter == "external".
	HTMLConverterCmd string
	// ExternalConverterTimeout caps the external subprocess. 0 → 5s.
	ExternalConverterTimeout time.Duration
	// PrettyTables toggles the spec 05 §6.1.1 data-vs-layout
	// classifier. When true (default), real data tables render as
	// ASCII grids and layout tables flatten to flowing text. When
	// false, every <table> flattens (the v0.17.x behavior).
	PrettyTables bool
	// PrettyTableMaxRows is the row-count ceiling above which a data
	// table is downgraded to a placeholder line. 0 → 50.
	PrettyTableMaxRows int
	// Logger receives fallback / error log messages from the renderer.
	// nil disables logging.
	Logger *slog.Logger
	// OnBodyDecoded fires after a successful body decode (cache fill
	// via FetchBodyAsync or warm hit re-decoded after eviction). The
	// callback receives the **pre-wrap** decoded plaintext from
	// [DecodeForIndex] so the body index does not silently break
	// regex matches on tokens the viewer happened to wrap (spec 35
	// §6.3). nil disables the hook — the renderer must stay
	// independent of sync / store-write paths beyond what the
	// renderer already does today.
	OnBodyDecoded BodyDecodedCallback
}

// BodyDecodedCallback fires after a body is successfully decoded.
// Caller (sync.Engine) decides whether to index. Spec 35 §6.3.
type BodyDecodedCallback func(ctx context.Context, m *store.Message, indexableText string)

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

// FetchedHeader is one RFC 822 header returned alongside the body.
// Mirrors graph.MessageHeader without the cross-package dependency.
type FetchedHeader struct {
	Name  string
	Value string
}

// BodyView is what the viewer pane displays.
type BodyView struct {
	Text         string
	TextExpanded string // fully un-collapsed body (quotes not folded)
	Links        []ExtractedLink
	State        BodyState
	Headers      []FetchedHeader
	// InviteCard, when non-empty, is the spec-34 invite metadata
	// card that paints above the body in the viewer. Empty for
	// non-invite messages and for invites where the event fetch
	// soft-failed. Body() does NOT populate this — the UI assigns
	// it out-of-band after the parallel GetEventMessage fetch
	// returns (spec 34 §6.4).
	InviteCard string
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
	// URLDisplayMaxWidth caps the visible text inside an OSC 8
	// hyperlink at N cells; the URL portion of the OSC 8 sequence
	// stays intact so Cmd-click + the URL picker still open the
	// full URL. End-truncation with `…` keeps the security-
	// relevant domain prefix visible (phishing detection). 0
	// disables truncation. Real-tenant complaint: long URLs
	// blocked vertical scrolling in the viewer.
	URLDisplayMaxWidth int
	// TZ is the user's resolved IANA timezone used by spec-34's
	// invite-card date formatting. nil → time.UTC. Kept on BodyOpts
	// so other date-formatting helpers can adopt the same source
	// later. Body() itself does not use this field today.
	TZ *time.Location
}

// BodyFetcher is the seam to [internal/graph.Client.GetMessageBody].
// Defined at the consumer site so render does not import graph
// (`docs/CONVENTIONS.md` §2: small consumer-side interfaces).
type BodyFetcher interface {
	FetchBody(ctx context.Context, messageID string) (FetchedBody, error)
}

// FetchedBody is the typed result of [BodyFetcher.FetchBody].
type FetchedBody struct {
	ContentType string // "text" | "html"
	Content     string
	// Attachments is the metadata-only attachment list returned by
	// Graph alongside the body (spec 05 §5.2 — `$expand=attachments`).
	// Empty when the message has no attachments. The renderer's
	// FetchBodyAsync persists these to the local store so the viewer
	// can list them without an extra round-trip on subsequent opens.
	Attachments []FetchedAttachment
	// Headers carries the RFC 822 headers returned alongside the body
	// (spec 05 C-1: internetMessageHeaders in $select). Not persisted
	// to the store; passed transiently through BodyView for the H-key
	// full-headers display.
	Headers []FetchedHeader
}

// FetchedAttachment is the metadata subset persisted on body fetch.
// Mirrors store.Attachment without the cross-package dependency on
// graph; the renderer translates between graph.Attachment and
// store.Attachment so callers (UI) only see this neutral shape.
type FetchedAttachment struct {
	ID          string
	Name        string
	ContentType string
	Size        int64
	IsInline    bool
	ContentID   string
}

// Renderer is the viewer-side rendering API. Stateless beyond its
// dependencies; callers may share one instance across goroutines.
type Renderer interface {
	Headers(m *store.Message, opts BodyOpts) string
	Body(ctx context.Context, m *store.Message, opts BodyOpts) (BodyView, error)
	Attachments(atts []store.Attachment, theme Theme) string
}

// defaultStripPatterns are the Outlook-noise regexps applied when
// Options.StripPatterns is nil.
var defaultStripPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)caution:?\s+this\s+(is\s+an?\s+)?external\s+email`),
	regexp.MustCompile(`(?i)if\s+you\s+('re|are)\s+having\s+trouble\s+viewing`),
	regexp.MustCompile(`(?s)\[cid:[^\]]+\]`),
}

// New constructs a Renderer with default options.
func New(st store.Store, fetcher BodyFetcher) Renderer {
	return NewWithOptions(st, fetcher, Options{})
}

// NewWithOptions constructs a Renderer with the supplied Options.
func NewWithOptions(st store.Store, fetcher BodyFetcher, opts Options) Renderer {
	patterns := opts.StripPatterns
	if patterns == nil {
		patterns = defaultStripPatterns
	}
	extTimeout := opts.ExternalConverterTimeout
	if extTimeout == 0 {
		extTimeout = 5 * time.Second
	}
	maxRows := opts.PrettyTableMaxRows
	if maxRows <= 0 {
		maxRows = 50
	}
	return &renderer{
		store:                    st,
		fetcher:                  fetcher,
		wrapColumns:              opts.WrapColumns,
		quoteCollapseThreshold:   opts.QuoteCollapseThreshold,
		stripPatterns:            patterns,
		htmlConverter:            opts.HTMLConverter,
		htmlConverterCmd:         opts.HTMLConverterCmd,
		externalConverterTimeout: extTimeout,
		prettyTables:             opts.PrettyTables,
		prettyTableMaxRows:       maxRows,
		logger:                   opts.Logger,
		onBodyDecoded:            opts.OnBodyDecoded,
	}
}

type renderer struct {
	store                    store.Store
	fetcher                  BodyFetcher
	wrapColumns              int
	quoteCollapseThreshold   int
	stripPatterns            []*regexp.Regexp
	htmlConverter            string
	htmlConverterCmd         string
	externalConverterTimeout time.Duration
	prettyTables             bool
	prettyTableMaxRows       int
	logger                   *slog.Logger
	onBodyDecoded            BodyDecodedCallback
	// inflight guards against concurrent duplicate fetches for the same
	// message ID. Stores messageID → struct{}{}.
	inflight sync.Map
}

// fireOnBodyDecoded calls the optional OnBodyDecoded hook with the
// pre-wrap plaintext from [DecodeForIndex]. Errors from
// DecodeForIndex are swallowed silently — the index hook must never
// fail the viewer fetch. Spec 35 §6.3.
func (r *renderer) fireOnBodyDecoded(ctx context.Context, m *store.Message, body store.Body) {
	if r.onBodyDecoded == nil || m == nil {
		return
	}
	var indexable string
	switch body.ContentType {
	case "html":
		t, err := DecodeForIndex(body.Content)
		if err != nil {
			return
		}
		indexable = t
	case "text":
		// Plain bodies: collapse whitespace runs but skip html2text.
		t, _ := DecodeForIndex(body.Content)
		indexable = t
	default:
		return
	}
	if indexable == "" {
		return
	}
	r.onBodyDecoded(ctx, m, indexable)
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
		r.fireOnBodyDecoded(ctx, m, *got)
		return r.renderBody(*got, opts), nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return BodyView{State: BodyError, Text: "store error: " + err.Error()}, err
	}
	return BodyView{State: BodyFetching, Text: "Loading message…"}, nil
}

// FetchBodyAsync performs the on-demand fetch and persists the result.
// Returns the rendered BodyView ready for the viewer to display.
//
// Spec 05 §5.2: alongside the body content, the fetch returns the
// message's attachment metadata via `$expand=attachments`. We
// persist that into the local store via UpsertAttachments on the
// same code path so subsequent viewer opens read from cache.
// Persist failure on attachments is non-fatal — the body still
// rendered; we log via the returned error path on the next layer.
//
// Single-flight: if a fetch for m.ID is already in flight, returns
// BodyFetching immediately so the caller does not issue a duplicate
// Graph request.
func (r *renderer) FetchBodyAsync(ctx context.Context, m *store.Message, opts BodyOpts) (BodyView, error) {
	if r.fetcher == nil {
		return BodyView{State: BodyError, Text: "no fetcher configured"}, errors.New("render: nil fetcher")
	}
	if _, loaded := r.inflight.LoadOrStore(m.ID, struct{}{}); loaded {
		return BodyView{State: BodyFetching, Text: "Loading message…"}, nil
	}
	defer r.inflight.Delete(m.ID)
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
	if len(fb.Attachments) > 0 {
		atts := make([]store.Attachment, 0, len(fb.Attachments))
		for _, a := range fb.Attachments {
			atts = append(atts, store.Attachment{
				ID:          a.ID,
				MessageID:   m.ID,
				Name:        a.Name,
				ContentType: a.ContentType,
				Size:        a.Size,
				IsInline:    a.IsInline,
				ContentID:   a.ContentID,
			})
		}
		_ = r.store.UpsertAttachments(ctx, atts)
	}
	r.fireOnBodyDecoded(ctx, m, body)
	view := r.renderBody(body, opts)
	view.Headers = fb.Headers
	return view, nil
}

func (r *renderer) applyStripPatterns(content string) string {
	if len(r.stripPatterns) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		matched := false
		for _, pat := range r.stripPatterns {
			if pat.MatchString(line) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func (r *renderer) renderBody(b store.Body, opts BodyOpts) BodyView {
	width := opts.Width
	if r.wrapColumns > 0 {
		width = r.wrapColumns
	}
	if width <= 0 {
		width = 80
	}
	content := r.applyStripPatterns(b.Content)
	switch strings.ToLower(b.ContentType) {
	case "html":
		text, links, err := r.htmlToTextWithConfig(content, width, opts.URLDisplayMaxWidth, opts.Theme)
		if err != nil {
			return BodyView{State: BodyError, Text: "html conversion failed"}
		}
		expanded := text
		if r.quoteCollapseThreshold > 0 {
			text = collapseQuotes(text, r.quoteCollapseThreshold)
		}
		return BodyView{State: BodyReady, Text: text, TextExpanded: expanded, Links: links}
	default:
		text, links := normalisePlain(content, width, opts.URLDisplayMaxWidth, r.quoteCollapseThreshold, opts.Theme)
		expanded, _ := normalisePlain(content, width, opts.URLDisplayMaxWidth, 0, opts.Theme)
		return BodyView{State: BodyReady, Text: text, TextExpanded: expanded, Links: links}
	}
}
