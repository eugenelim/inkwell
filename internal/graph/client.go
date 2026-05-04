package graph

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultBaseURL is Microsoft Graph v1.0. Spec ARCH §0 locks v1.0.
const DefaultBaseURL = "https://graph.microsoft.com/v1.0"

// Authenticator is the subset of [github.com/eugenelim/inkwell/internal/auth.Authenticator]
// the graph package depends on. Declared here at the consumer site so
// graph does not import auth's full surface.
type Authenticator interface {
	Token(ctx context.Context) (string, error)
	Invalidate()
}

// Options configures [NewClient].
type Options struct {
	BaseURL string
	// MaxConcurrent caps in-flight Graph requests. Spec ARCH §5.2 sets
	// this to 4. Values >16 are rejected as unsafe.
	MaxConcurrent int
	// MaxRetries bounds the throttle-retry loop before surfacing the
	// last 429. Default 5.
	MaxRetries int
	// MaxBackoff caps the exponential-backoff delay when no Retry-After
	// header is present. Zero uses the default (30s).
	MaxBackoff time.Duration
	// Logger is the redacting slog logger. Required.
	Logger *slog.Logger
	// Transport is the underlying RoundTripper (defaults to
	// http.DefaultTransport). Tests substitute httptest's transport.
	Transport http.RoundTripper
	// OnThrottle is called whenever a request had to wait on a 429.
	// The sync engine consumes this to surface a status-line note.
	OnThrottle func(retryAfter time.Duration)
}

// Client is the typed Microsoft Graph REST client. It is goroutine-safe.
type Client struct {
	baseURL string
	hc      *http.Client
	logger  *slog.Logger
}

// NewClient builds a Graph client. auth is required; the auth transport
// in the stack injects Bearer tokens and refreshes on 401.
func NewClient(auth Authenticator, opts Options) (*Client, error) {
	if auth == nil {
		return nil, errors.New("graph: authenticator required")
	}
	if opts.Logger == nil {
		return nil, errors.New("graph: logger required (redaction is mandatory)")
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 4
	}
	if opts.MaxConcurrent > 16 {
		return nil, fmt.Errorf("graph: max_concurrent %d > 16 (unsafe)", opts.MaxConcurrent)
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 5
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 30 * time.Second
	}
	base := opts.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	// Inside-out: logging → throttle → auth, applied by wrapping each
	// transport around the next. The outer-most transport (auth) runs
	// first on RoundTrip so token refresh on 401 can re-issue.
	logging := &loggingTransport{base: base, logger: opts.Logger}
	throttle := &throttleTransport{
		base:       logging,
		sem:        make(chan struct{}, opts.MaxConcurrent),
		maxRetries: opts.MaxRetries,
		maxBackoff: opts.MaxBackoff,
		onThrottle: opts.OnThrottle,
		logger:     opts.Logger,
	}
	authT := &authTransport{base: throttle, auth: auth, logger: opts.Logger}

	url := opts.BaseURL
	if url == "" {
		url = DefaultBaseURL
	}
	url = strings.TrimRight(url, "/")
	return &Client{
		baseURL: url,
		hc:      &http.Client{Transport: authT, Timeout: 60 * time.Second},
		logger:  opts.Logger,
	}, nil
}

// BaseURL returns the configured base URL (no trailing slash).
func (c *Client) BaseURL() string { return c.baseURL }

// Logger exposes the redacting logger to typed wrappers.
func (c *Client) Logger() *slog.Logger { return c.logger }

// Do issues a request relative to the base URL. path may begin with "/"
// or be a fully-qualified URL (delta nextLink follow case).
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader, hdr http.Header) (*http.Response, error) {
	url := path
	if strings.HasPrefix(path, "/") {
		url = c.baseURL + path
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	return c.hc.Do(req)
}

// authTransport injects Bearer tokens and retries once on 401.
type authTransport struct {
	base   http.RoundTripper
	auth   Authenticator
	logger *slog.Logger
}

func (a *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := a.attach(req); err != nil {
		return nil, err
	}
	resp, err := a.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	_ = resp.Body.Close()
	a.auth.Invalidate()
	if err := a.attach(req); err != nil {
		return nil, err
	}
	return a.base.RoundTrip(req)
}

func (a *authTransport) attach(req *http.Request) error {
	tok, err := a.auth.Token(req.Context())
	if err != nil {
		return fmt.Errorf("graph: auth: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return nil
}

// throttleTransport applies the per-mailbox concurrency cap and honours
// Retry-After on 429 / 503. Up to maxRetries retries with exponential
// fallback when Retry-After is missing.
type throttleTransport struct {
	base       http.RoundTripper
	sem        chan struct{}
	maxRetries int
	maxBackoff time.Duration
	onThrottle func(time.Duration)
	logger     *slog.Logger

	inFlight atomic.Int32
}

func (t *throttleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case t.sem <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	defer func() { <-t.sem }()
	t.inFlight.Add(1)
	defer t.inFlight.Add(-1)

	for attempt := 0; ; attempt++ {
		resp, err := t.base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
			return resp, nil
		}
		if attempt >= t.maxRetries {
			return resp, nil
		}
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		if retryAfter == 0 {
			retryAfter = time.Duration(1<<uint(attempt)) * time.Second
			if retryAfter > t.maxBackoff {
				retryAfter = t.maxBackoff
			}
		}
		if t.onThrottle != nil {
			t.onThrottle(retryAfter)
		}
		t.logger.Warn("graph: throttled, retrying",
			slog.Int("attempt", attempt),
			slog.Duration("retry_after", retryAfter),
			slog.Int("status", resp.StatusCode),
		)
		_ = resp.Body.Close()
		select {
		case <-time.After(retryAfter):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
}

// InFlight reports how many requests are currently in the throttle
// window. Used by tests and observability.
func (t *throttleTransport) InFlight() int32 { return t.inFlight.Load() }

func parseRetryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

// loggingTransport emits a single redacted line per request. It is the
// innermost transport so the logged headers reflect what actually went
// on the wire.
type loggingTransport struct {
	base   http.RoundTripper
	logger *slog.Logger
}

func (l *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := l.base.RoundTrip(req)
	attrs := []any{
		slog.String("method", req.Method),
		slog.String("url", req.URL.String()),
		slog.Duration("duration", time.Since(start)),
	}
	if err != nil {
		attrs = append(attrs, slog.String("err", err.Error()))
		l.logger.Warn("graph: request failed", attrs...)
		return nil, err
	}
	attrs = append(attrs, slog.Int("status", resp.StatusCode))
	if reqID := resp.Header.Get("request-id"); reqID != "" {
		attrs = append(attrs, slog.String("request_id", reqID))
	}
	if l.logger.Enabled(req.Context(), slog.LevelDebug) {
		// At DEBUG we dump the request without body. The redactor
		// scrubs the bearer token from any string output.
		if dump, derr := httputil.DumpRequestOut(req, false); derr == nil {
			attrs = append(attrs, slog.String("request_dump", string(dump)))
		}
	}
	l.logger.Info("graph: request", attrs...)
	return resp, nil
}
