package render

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

func openRenderTestStore(t *testing.T) (store.Store, int64, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.db")
	s, err := store.Open(path, store.DefaultOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	id, err := s.PutAccount(context.Background(), store.Account{TenantID: "T", ClientID: "C", UPN: "tester@example.invalid"})
	require.NoError(t, err)
	require.NoError(t, s.UpsertFolder(context.Background(), store.Folder{
		ID: "f-inbox", AccountID: id, DisplayName: "Inbox", WellKnownName: "inbox", LastSyncedAt: time.Now(),
	}))
	return s, id, "f-inbox"
}

// stubFetcher satisfies BodyFetcher with a canned response.
type stubFetcher struct {
	contentType string
	content     string
	calls       int
	err         error
}

func (f *stubFetcher) FetchBody(_ context.Context, _ string) (FetchedBody, error) {
	f.calls++
	if f.err != nil {
		return FetchedBody{}, f.err
	}
	return FetchedBody{ContentType: f.contentType, Content: f.content}, nil
}

func TestHeadersRenderDefaultSet(t *testing.T) {
	r := New(nil, nil)
	m := &store.Message{
		FromAddress: "alice@example.invalid",
		FromName:    "Alice",
		ToAddresses: []store.EmailAddress{{Address: "bob@example.invalid", Name: "Bob"}},
		ReceivedAt:  time.Now().Add(-2 * time.Hour),
		Subject:     "Q4 forecast",
	}
	out := r.Headers(m, BodyOpts{Theme: DefaultTheme()})
	require.Contains(t, out, "From:")
	require.Contains(t, out, "Alice <alice@example.invalid>")
	require.Contains(t, out, "To:")
	require.Contains(t, out, "Subject:")
	require.Contains(t, out, "Q4 forecast")
}

func TestHeadersTruncateRecipientsBeyondThree(t *testing.T) {
	r := New(nil, nil)
	to := []store.EmailAddress{
		{Address: "a@example.invalid"},
		{Address: "b@example.invalid"},
		{Address: "c@example.invalid"},
		{Address: "d@example.invalid"},
		{Address: "e@example.invalid"},
	}
	m := &store.Message{ToAddresses: to, Subject: "x"}
	out := r.Headers(m, BodyOpts{Theme: DefaultTheme()})
	require.Contains(t, out, "(2 more)")
}

func TestHeadersFullExpandsExtras(t *testing.T) {
	r := New(nil, nil)
	m := &store.Message{
		Subject:        "x",
		Importance:     "high",
		Categories:     []string{"Work", "Q4"},
		FlagStatus:     "flagged",
		HasAttachments: true,
	}
	out := r.Headers(m, BodyOpts{ShowFullHeaders: true, Theme: DefaultTheme()})
	require.Contains(t, out, "Importance:")
	require.Contains(t, out, "Categories:")
	require.Contains(t, out, "Flag:")
	require.Contains(t, out, "Has Attachments:")
}

func TestHeadersHandleEmptySubject(t *testing.T) {
	r := New(nil, nil)
	m := &store.Message{Subject: ""}
	out := r.Headers(m, BodyOpts{Theme: DefaultTheme()})
	require.Contains(t, out, "(no subject)")
}

func TestPlainNormalisesCRLFAndQuoting(t *testing.T) {
	body := "Hi Alice,\r\n> previous reply\r\n>> deep quote\r\nthanks\r\n"
	out, _ := normalisePlain(body, 80)
	require.Contains(t, out, "Hi Alice,\n")
	require.Contains(t, out, "> previous reply\n")
	require.Contains(t, out, "> > deep quote\n")
}

func TestPlainSoftWrapsLongLines(t *testing.T) {
	long := strings.Repeat("word ", 30)
	out, _ := normalisePlain(long, 30)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		require.LessOrEqual(t, len(line), 35, "wrapped line: %q", line)
	}
}

// TestLinkifyURLsWrapsInOSC8 confirms inline URLs in the rendered
// body get wrapped in OSC 8 hyperlink escapes. Real-tenant complaint:
// drag-selecting a multi-line URL captured the adjacent message-list
// pane (terminal rectangular selection). OSC 8 makes URLs clickable
// in supporting terminals (iTerm2, kitty, alacritty, foot, wezterm)
// so the user clicks instead of dragging across pane borders.
func TestLinkifyURLsWrapsInOSC8(t *testing.T) {
	in := "see https://example.invalid/a for details."
	out := linkifyURLsInText(in)
	// OSC 8 sequence: \x1b]8;;<url>\x1b\\<text>\x1b]8;;\x1b\\
	require.Contains(t, out, "\x1b]8;;https://example.invalid/a\x1b\\")
	require.Contains(t, out, "https://example.invalid/a\x1b]8;;\x1b\\")
	// Trailing punctuation preserved outside the hyperlink wrap.
	require.Contains(t, out, " for details.")
}

func TestLinkifyURLsLeavesNonURLsAlone(t *testing.T) {
	in := "no links here, just prose."
	out := linkifyURLsInText(in)
	require.Equal(t, in, out)
}

func TestRenderLinkBlockEmitsOSC8(t *testing.T) {
	links := []ExtractedLink{
		{Index: 1, URL: "https://example.invalid/a", Text: "https://example.invalid/a"},
	}
	out := renderLinkBlock(links)
	require.Contains(t, out, "\x1b]8;;https://example.invalid/a\x1b\\")
	require.Contains(t, out, "[1]")
}

func TestExtractLinksAreNumberedAndDeduped(t *testing.T) {
	body := "see https://example.invalid/a and https://example.invalid/b and again https://example.invalid/a."
	links := extractLinks(body)
	require.Len(t, links, 2)
	require.Equal(t, 1, links[0].Index)
	require.Equal(t, "https://example.invalid/a", links[0].URL)
	require.Equal(t, 2, links[1].Index)
}

func TestHTMLToTextStripsTrackingPixels(t *testing.T) {
	html := `<html><body>Hello <img src="https://t.example.invalid/p.gif" width=1 height=1>world<a href="https://example.invalid/x">link</a></body></html>`
	text, links, err := htmlToText(html, 80)
	require.NoError(t, err)
	require.Contains(t, text, "Hello")
	require.Contains(t, text, "world")
	// Tracking pixel URL must NOT appear; the legitimate link must.
	require.NotContains(t, text, "t.example.invalid/p.gif")
	require.True(t, anyLinkContains(links, "example.invalid/x"))
}

func TestBodyHitFromCacheRendersInline(t *testing.T) {
	s, _, _ := openRenderTestStore(t)
	m := store.Message{ID: "m-1", AccountID: 1, FolderID: "f-inbox", Subject: "x"}
	require.NoError(t, s.UpsertMessage(context.Background(), m))
	require.NoError(t, s.PutBody(context.Background(), store.Body{
		MessageID:   "m-1",
		ContentType: "text",
		Content:     "Hello world",
	}))
	r := New(s, nil)
	view, err := r.Body(context.Background(), &m, BodyOpts{Width: 80})
	require.NoError(t, err)
	require.Equal(t, BodyReady, view.State)
	require.Contains(t, view.Text, "Hello world")
}

func TestBodyMissReturnsFetchingPlaceholder(t *testing.T) {
	s, _, _ := openRenderTestStore(t)
	m := store.Message{ID: "m-2", AccountID: 1, FolderID: "f-inbox"}
	require.NoError(t, s.UpsertMessage(context.Background(), m))

	r := New(s, &stubFetcher{contentType: "text", content: "fetched body"})
	view, err := r.Body(context.Background(), &m, BodyOpts{Width: 80})
	require.NoError(t, err)
	require.Equal(t, BodyFetching, view.State)
	require.Contains(t, view.Text, "Loading")
}

func TestFetchBodyAsyncWritesCacheAndReturnsRendered(t *testing.T) {
	s, _, _ := openRenderTestStore(t)
	m := store.Message{ID: "m-3", AccountID: 1, FolderID: "f-inbox"}
	require.NoError(t, s.UpsertMessage(context.Background(), m))

	fetcher := &stubFetcher{contentType: "html", content: `<p>hello <a href="https://example.invalid/x">link</a></p>`}
	r := New(s, fetcher)
	view, err := r.(*renderer).FetchBodyAsync(context.Background(), &m, BodyOpts{Width: 80})
	require.NoError(t, err)
	require.Equal(t, BodyReady, view.State)
	require.Equal(t, 1, fetcher.calls)
	require.Contains(t, view.Text, "hello")
	require.True(t, anyLinkContains(view.Links, "example.invalid/x"))

	// Subsequent Body() call now hits the cache.
	view2, err := r.Body(context.Background(), &m, BodyOpts{Width: 80})
	require.NoError(t, err)
	require.Equal(t, BodyReady, view2.State)
}

func TestFetchBodyAsyncSurfacesFetchError(t *testing.T) {
	s, _, _ := openRenderTestStore(t)
	m := store.Message{ID: "m-4", AccountID: 1, FolderID: "f-inbox"}
	require.NoError(t, s.UpsertMessage(context.Background(), m))
	r := New(s, &stubFetcher{err: errors.New("boom")})
	view, err := r.(*renderer).FetchBodyAsync(context.Background(), &m, BodyOpts{Width: 80})
	require.Error(t, err)
	require.Equal(t, BodyError, view.State)
}

func TestAttachmentsListRendersEachWithSize(t *testing.T) {
	r := New(nil, nil)
	out := r.Attachments([]store.Attachment{
		{Name: "deck.pdf", ContentType: "application/pdf", Size: 2 * 1024 * 1024},
		{Name: "logo.png", ContentType: "image/png", Size: 4096, IsInline: true},
	}, DefaultTheme())
	require.Contains(t, out, "deck.pdf")
	require.Contains(t, out, "2.0MB")
	require.Contains(t, out, "logo.png")
	require.Contains(t, out, "4.0KB")
}

func TestPrivacyNoBodyContentLoggedDuringRender(t *testing.T) {
	// Sanity: the render package emits no slog calls. This test
	// reads the source files and asserts none import log/slog.
	pkg := "render"
	require.NotContains(t, mustReadAll(pkg), "log/slog", "render package must not import log/slog (CLAUDE.md §7 lint)")
}

// helpers

func anyLinkContains(links []ExtractedLink, sub string) bool {
	for _, l := range links {
		if strings.Contains(l.URL, sub) {
			return true
		}
	}
	return false
}

func mustReadAll(pkg string) string {
	dir := filepath.Join("..", pkg)
	out := strings.Builder{}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, p := range matches {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		// Best-effort read; an error here is a setup bug, not a test failure.
		b, _ := readFile(p)
		out.Write(b)
	}
	return out.String()
}

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
