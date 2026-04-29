package action

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs goleak after the action package's test suite. The
// executor's Drain may dispatch concurrently; a leak here means a
// goroutine outlives the test, which means it would also outlive
// production usage.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("modernc.org/libc.handleSignals"),
		// httptest.Server leaves persistent client connections in
		// readLoop / writeLoop after Close(). Not an inkwell leak.
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"),
		goleak.IgnoreAnyFunction("internal/poll.runtime_pollWait"),
	)
}
