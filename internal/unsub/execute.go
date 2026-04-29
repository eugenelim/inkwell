package unsub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Executor performs a one-click unsubscribe POST against the URL
// surfaced by Parse. Construction takes the Inkwell version string
// (used in the User-Agent) so the binary can identify itself politely
// without leaking PII.
type Executor struct {
	client    *http.Client
	userAgent string
}

// NewExecutor returns a ready-to-use Executor. The HTTP client has a
// 5-second timeout (spec 16 §12) and caps redirects at 3 — open
// redirects via a chain of unsubscribe forwarders is the only way an
// attacker could exfiltrate the user-agent header, and 3 hops is
// enough for legitimate CDN fronts.
func NewExecutor(version string) *Executor {
	return &Executor{
		client: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
		userAgent: "inkwell/" + version,
	}
}

// OneClickPOST issues `POST <urlStr>` with body
// `List-Unsubscribe=One-Click` (RFC 8058 §3.1). Returns nil on a 2xx,
// a typed error otherwise that the UI surfaces verbatim.
func (e *Executor) OneClickPOST(ctx context.Context, urlStr string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("unsub: parse %q: %w", urlStr, err)
	}
	if u.Scheme != "https" {
		// Belt-and-braces — Parse already rejects http:, but if a
		// caller hand-builds a URL we refuse here too.
		return errors.New("unsub: refusing to POST to non-HTTPS URL")
	}
	body := strings.NewReader("List-Unsubscribe=One-Click")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, body)
	if err != nil {
		return fmt.Errorf("unsub: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", e.userAgent)
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("unsub: POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unsub: POST returned %d", resp.StatusCode)
	}
	return nil
}
