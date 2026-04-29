// Package unsub parses RFC 2369 List-Unsubscribe and RFC 8058
// List-Unsubscribe-Post headers and exposes the right action for the
// caller to take (one-click POST, mailto: draft, or browser open).
//
// Spec 16 §3 picks the action by priority:
//
//  1. RFC 8058 one-click HTTPS POST — fully automatic.
//  2. mailto: URI — open compose flow (spec 15).
//  3. HTTPS GET — open in system browser.
//
// Insecure http: URIs are intentionally NOT actionable; spec 16 §9
// mandates the user open them manually if they trust the sender.
package unsub

import (
	"errors"
	"net/url"
	"strings"
)

// ErrNoHeader is returned by Parse when no List-Unsubscribe is set.
var ErrNoHeader = errors.New("unsub: no List-Unsubscribe header")

// ErrUnactionable is returned when a header is present but every URI
// failed validation (malformed, http: only, unsupported scheme).
var ErrUnactionable = errors.New("unsub: header present but no actionable URI")

// Action enumerates what the caller should do.
type Action int

const (
	ActionUnknown Action = iota
	// ActionOneClickPOST: POST `List-Unsubscribe=One-Click` to the
	// HTTPS URI. Fully automated per RFC 8058.
	ActionOneClickPOST
	// ActionMailto: open compose flow (spec 15) with the mailto: URI's
	// address / subject / body.
	ActionMailto
	// ActionBrowserGET: open the HTTPS URI in the system browser; user
	// finishes there.
	ActionBrowserGET
)

// String renders Action as a human label for status messages.
func (a Action) String() string {
	switch a {
	case ActionOneClickPOST:
		return "one-click"
	case ActionMailto:
		return "mailto"
	case ActionBrowserGET:
		return "browser"
	}
	return "unknown"
}

// Result is the parsed unsubscribe instruction.
type Result struct {
	// Action is what the caller should execute.
	Action Action
	// URL is the HTTPS URI for ActionOneClickPOST / ActionBrowserGET.
	// Empty for ActionMailto.
	URL string
	// MailtoAddr is the address parsed from a mailto: URI (e.g.
	// "unsub@list.example.com"). Empty for the HTTPS actions.
	MailtoAddr string
	// MailtoSubject and MailtoBody come from mailto: query params.
	MailtoSubject string
	MailtoBody    string
}

// Parse reads the headers off a message and returns the action the
// caller should take. listUnsub is the raw `List-Unsubscribe` value;
// listUnsubPost is the raw `List-Unsubscribe-Post` value (empty when
// absent).
//
// Returns ErrNoHeader when listUnsub is empty / whitespace.
// Returns ErrUnactionable when no URI in the header is actionable
// under our policy (insecure http:, malformed, unknown scheme).
func Parse(listUnsub, listUnsubPost string) (*Result, error) {
	if strings.TrimSpace(listUnsub) == "" {
		return nil, ErrNoHeader
	}
	uris := splitURIs(listUnsub)
	if len(uris) == 0 {
		return nil, ErrUnactionable
	}

	oneClickEligible := isOneClickPostHeader(listUnsubPost)

	// Priority pass 1: one-click HTTPS, but only if List-Unsubscribe-Post
	// declares the contract. Without that header, an HTTPS URI is
	// treated as ActionBrowserGET — never auto-POST without the
	// sender's explicit RFC 8058 opt-in.
	if oneClickEligible {
		for _, raw := range uris {
			if u := parseHTTPS(raw); u != nil {
				return &Result{Action: ActionOneClickPOST, URL: u.String()}, nil
			}
		}
	}
	// Priority pass 2: mailto:.
	for _, raw := range uris {
		if r := parseMailto(raw); r != nil {
			return r, nil
		}
	}
	// Priority pass 3: HTTPS GET (browser).
	for _, raw := range uris {
		if u := parseHTTPS(raw); u != nil {
			return &Result{Action: ActionBrowserGET, URL: u.String()}, nil
		}
	}
	return nil, ErrUnactionable
}

// splitURIs parses one List-Unsubscribe value into the angle-bracketed
// URIs. Real-world headers are messy:
//
//	<https://a.example/u>, <mailto:b@c.example>
//	< https://a.example/u >,< mailto:b@c.example >
//	<https://a.example/u>
//
// We tolerate whitespace inside and outside the brackets. Anything not
// inside angle brackets is ignored — RFC 2369 §3.6 mandates the
// brackets, and lenient parsers that try to recover bare URIs trip on
// commas inside query strings.
func splitURIs(header string) []string {
	var out []string
	for {
		i := strings.IndexByte(header, '<')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(header[i+1:], '>')
		if j < 0 {
			return out
		}
		uri := strings.TrimSpace(header[i+1 : i+1+j])
		if uri != "" {
			out = append(out, uri)
		}
		header = header[i+1+j+1:]
	}
}

// isOneClickPostHeader checks whether List-Unsubscribe-Post declares
// `List-Unsubscribe=One-Click`. Some senders include extra whitespace
// or varying capitalisation; match case-insensitively after trim.
func isOneClickPostHeader(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(v, "list-unsubscribe=one-click")
}

// parseHTTPS returns the URL when raw is a valid HTTPS URI, nil
// otherwise. http: is rejected (spec 16 §9): plain HTTP is a known
// downgrade vector and we surface a friendly "open manually if you
// trust the sender" error instead of auto-acting.
func parseHTTPS(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	if u.Scheme != "https" {
		return nil
	}
	if u.Host == "" {
		return nil
	}
	return u
}

// parseMailto extracts address + optional ?subject= + ?body= from a
// mailto: URI. Returns nil for non-mailto / malformed.
func parseMailto(raw string) *Result {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "mailto" {
		return nil
	}
	addr := strings.TrimSpace(u.Opaque)
	if addr == "" {
		// Some clients emit `mailto://addr` (non-standard). url.Parse
		// puts the address in Host instead of Opaque.
		addr = u.Host
	}
	if addr == "" {
		return nil
	}
	q := u.Query()
	return &Result{
		Action:        ActionMailto,
		MailtoAddr:    addr,
		MailtoSubject: q.Get("subject"),
		MailtoBody:    q.Get("body"),
	}
}

// IndicatorURL is the simple, store-shaped "do we have an actionable
// unsubscribe?" signal. Returns ("", false) for ErrNoHeader,
// (url, true) for the URI we'd POST/GET, and (mailto-addr, true) for
// mailto:. Populated on demand by the UI / sync engine and persisted
// on the message row so the next render is a local lookup.
func IndicatorURL(r *Result) string {
	if r == nil {
		return ""
	}
	switch r.Action {
	case ActionOneClickPOST, ActionBrowserGET:
		return r.URL
	case ActionMailto:
		return "mailto:" + r.MailtoAddr
	}
	return ""
}
