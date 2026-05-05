package graph

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	ilog "github.com/eugenelim/inkwell/internal/log"
)

// SECURITY-MAP: V9.1.3
// TestGraphClientTLSVerificationEnabled asserts that the Graph HTTP
// client never sets InsecureSkipVerify: true. Go's http.DefaultTransport
// defaults to TLS 1.2 minimum + system trust store. This test guards
// against accidental introduction of a permissive TLS config.
func TestGraphClientTLSVerificationEnabled(t *testing.T) {
	logger, _ := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "owner@example.invalid"})
	c, err := NewClient(&fakeAuth{}, Options{Logger: logger})
	require.NoError(t, err)

	// Walk the transport chain looking for any *tls.Config with
	// InsecureSkipVerify set. The chain is authT → throttle → logging
	// → http.DefaultTransport; none of our layers customise TLS.
	require.False(t, tlsSkipVerifyInChain(c.hc.Transport),
		"Graph HTTP client must not have InsecureSkipVerify: true anywhere in the transport chain")
}

// tlsSkipVerifyInChain walks the transport chain and returns true if
// any layer has a *tls.Config with InsecureSkipVerify set.
func tlsSkipVerifyInChain(rt http.RoundTripper) bool {
	switch t := rt.(type) {
	case *http.Transport:
		return t.TLSClientConfig != nil && t.TLSClientConfig.InsecureSkipVerify
	case *authTransport:
		return tlsSkipVerifyInChain(t.base)
	case *throttleTransport:
		return tlsSkipVerifyInChain(t.base)
	case *loggingTransport:
		return tlsSkipVerifyInChain(t.base)
	default:
		return false
	}
}
