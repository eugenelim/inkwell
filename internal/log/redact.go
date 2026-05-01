// Package log provides the application's structured logger and a
// redaction handler that scrubs secrets, message bodies, and PII before
// any log line is emitted.
//
// Redaction is mandatory (CLAUDE.md §7, ARCH §12). Construct loggers via
// [New]; do not configure slog directly elsewhere in the codebase.
package log

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// SensitiveKeys are slog attribute keys whose values are always redacted.
// Add to this list when introducing a new attribute that may carry secrets.
var SensitiveKeys = map[string]bool{
	"token":         true,
	"access_token":  true,
	"refresh_token": true,
	"id_token":      true,
	"bearer":        true,
	"authorization": true,
	"cache_blob":    true,
	"body":          true,
	"content":       true,
	// snapshot is the JSON-encoded ComposeSnapshot blob persisted in
	// compose_sessions (spec 15 §7). It carries body + subject + To/
	// Cc — same content sensitivity as the raw body. Defense-in-
	// depth so a future log site that emits the blob doesn't leak.
	"snapshot":      true,
	"password":      true,
	"secret":        true,
	"client_secret": true,
}

// SubjectIsSensitiveAtLevel returns true when subject lines must be
// redacted at the given level. Subjects are visible only at DEBUG.
func SubjectIsSensitiveAtLevel(level slog.Level) bool {
	return level > slog.LevelDebug
}

var (
	bearerPattern = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-~+/=]+`)
	jwtPattern    = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`)
	emailPattern  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

// EmailMap maps real email addresses to opaque per-session tokens
// (<email-N>) so log output preserves correlation without leaking PII.
type EmailMap struct {
	mu    sync.Mutex
	next  int
	known map[string]string
}

// NewEmailMap returns a fresh per-session map.
func NewEmailMap() *EmailMap { return &EmailMap{known: make(map[string]string)} }

// Token returns the opaque token for addr, allocating one if needed.
func (m *EmailMap) Token(addr string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	addr = strings.ToLower(strings.TrimSpace(addr))
	if t, ok := m.known[addr]; ok {
		return t
	}
	t := "<email-" + strconv.Itoa(m.next) + ">"
	m.next++
	m.known[addr] = t
	return t
}

// Redact replaces every email occurrence in s with its opaque token.
func (m *EmailMap) Redact(s string) string {
	return emailPattern.ReplaceAllStringFunc(s, m.Token)
}

// Options configures [New].
type Options struct {
	// Level is the minimum slog level emitted (default: Info).
	Level slog.Level
	// AllowOwnUPN is the user's own UPN, which is preserved verbatim
	// in log output. All other email addresses are tokenised.
	AllowOwnUPN string
}

// New returns a slog.Logger that writes to w, scrubbing secrets, bodies,
// and PII per CLAUDE.md §7.
func New(w io.Writer, opts Options) *slog.Logger {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: opts.Level})
	return slog.New(&redactor{
		base:     base,
		emails:   NewEmailMap(),
		ownUPN:   strings.ToLower(strings.TrimSpace(opts.AllowOwnUPN)),
		minLevel: opts.Level,
	})
}

type redactor struct {
	base     slog.Handler
	emails   *EmailMap
	ownUPN   string
	minLevel slog.Level
}

func (r *redactor) Enabled(ctx context.Context, l slog.Level) bool {
	return r.base.Enabled(ctx, l)
}

func (r *redactor) Handle(ctx context.Context, rec slog.Record) error {
	clean := slog.NewRecord(rec.Time, rec.Level, r.scrubString(rec.Message, rec.Level), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(r.scrubAttr(a, rec.Level))
		return true
	})
	return r.base.Handle(ctx, clean)
}

func (r *redactor) WithAttrs(attrs []slog.Attr) slog.Handler {
	scrubbed := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		scrubbed[i] = r.scrubAttr(a, slog.LevelInfo)
	}
	return &redactor{
		base:     r.base.WithAttrs(scrubbed),
		emails:   r.emails,
		ownUPN:   r.ownUPN,
		minLevel: r.minLevel,
	}
}

func (r *redactor) WithGroup(name string) slog.Handler {
	return &redactor{
		base:     r.base.WithGroup(name),
		emails:   r.emails,
		ownUPN:   r.ownUPN,
		minLevel: r.minLevel,
	}
}

func (r *redactor) scrubAttr(a slog.Attr, lvl slog.Level) slog.Attr {
	key := strings.ToLower(a.Key)
	if SensitiveKeys[key] {
		return slog.String(a.Key, "<redacted>")
	}
	if key == "subject" && SubjectIsSensitiveAtLevel(lvl) {
		return slog.String(a.Key, "<redacted>")
	}
	return slog.Any(a.Key, r.scrubValue(a.Value, lvl))
}

func (r *redactor) scrubValue(v slog.Value, lvl slog.Level) any {
	switch v.Kind() {
	case slog.KindString:
		return r.scrubString(v.String(), lvl)
	case slog.KindGroup:
		attrs := v.Group()
		scrubbed := make([]slog.Attr, len(attrs))
		for i, a := range attrs {
			scrubbed[i] = r.scrubAttr(a, lvl)
		}
		return slog.GroupValue(scrubbed...)
	default:
		return v.Any()
	}
}

// scrubString applies all string-level redactions in order.
func (r *redactor) scrubString(s string, _ slog.Level) string {
	if s == "" {
		return s
	}
	s = bearerPattern.ReplaceAllString(s, "Bearer <redacted>")
	s = jwtPattern.ReplaceAllString(s, "<redacted-jwt>")
	if r.ownUPN == "" {
		return r.emails.Redact(s)
	}
	return emailPattern.ReplaceAllStringFunc(s, func(addr string) string {
		if strings.EqualFold(addr, r.ownUPN) {
			return addr
		}
		return r.emails.Token(addr)
	})
}

// Captured is a helper for tests that need to read every line emitted by a
// redacting logger. Use [NewCaptured] in tests; it is goroutine-safe.
type Captured struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// NewCaptured returns a logger plus a buffer of its emitted bytes.
func NewCaptured(opts Options) (*slog.Logger, *Captured) {
	c := &Captured{}
	return New(c, opts), c
}

// Write satisfies io.Writer.
func (c *Captured) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

// String returns everything captured so far.
func (c *Captured) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Contains reports whether the captured output contains any of needles.
func (c *Captured) Contains(needles ...string) bool {
	s := c.String()
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// AssertNoSecret is a convenience used by tests in many packages: it fails
// if any of the synthetic secret strings appears in captured output.
// Returns a non-nil error describing the leaked needle.
func (c *Captured) AssertNoSecret(needles ...string) error {
	s := c.String()
	for _, n := range needles {
		if strings.Contains(s, n) {
			return fmt.Errorf("captured log leaked secret %q in: %s", n, s)
		}
	}
	return nil
}
