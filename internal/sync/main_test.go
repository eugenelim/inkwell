package sync

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs goleak after the test suite. The engine spawns
// goroutines (the cycle loop, event consumers); a leak here would
// indicate a shutdown bug that production hits eventually.
//
// Ignored top-functions are runtime-internal or stdlib pools that
// goleak flags as false positives in this codebase.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("modernc.org/libc.handleSignals"),
		// httptest.Server keep-alive connections.
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"),
		goleak.IgnoreAnyFunction("internal/poll.runtime_pollWait"),
	)
}
