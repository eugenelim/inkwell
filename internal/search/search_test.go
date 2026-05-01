package search

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// stubLocal returns canned local results.
type stubLocal struct {
	mu      sync.Mutex
	results []store.MessageMatch
	delay   time.Duration
	calls   int
	lastQ   store.SearchQuery
}

func (s *stubLocal) Search(ctx context.Context, q store.SearchQuery) ([]store.MessageMatch, error) {
	s.mu.Lock()
	s.calls++
	s.lastQ = q
	s.mu.Unlock()
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.results, nil
}

// stubServer returns canned server results.
type stubServer struct {
	mu      sync.Mutex
	results []store.Message
	delay   time.Duration
	err     error
	calls   int
	lastQ   ServerQuery
}

func (s *stubServer) SearchMessages(ctx context.Context, q ServerQuery) ([]store.Message, error) {
	s.mu.Lock()
	s.calls++
	s.lastQ = q
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.results, nil
}

func mkMsg(id, subj string, recv time.Time) store.Message {
	return store.Message{
		ID:         id,
		Subject:    subj,
		ReceivedAt: recv,
	}
}

// TestSearcherEmitsLocalThenMergesServer is the spec 06 §4 streaming
// invariant: the local branch produces a snapshot first, then the
// server branch's results merge in. The Stream emits at least one
// snapshot; the final snapshot contains both branches' results.
func TestSearcherEmitsLocalThenMergesServer(t *testing.T) {
	now := time.Now()
	local := &stubLocal{results: []store.MessageMatch{
		{Message: mkMsg("m-local", "Local hit", now.Add(-1*time.Hour)), Rank: 1.0},
	}}
	server := &stubServer{
		delay: 30 * time.Millisecond,
		results: []store.Message{
			mkMsg("m-server", "Server hit", now),
		},
	}
	s := New(local, server, Options{
		EmitThrottle:  10 * time.Millisecond,
		ServerTimeout: time.Second,
	})
	stream := s.Search(context.Background(), Query{Text: "anything"})

	var snapshots [][]Result
	timeout := time.After(2 * time.Second)
loop:
	for {
		select {
		case snap, ok := <-stream.Updates():
			if !ok {
				break loop
			}
			snapshots = append(snapshots, snap)
		case <-stream.Done():
			break loop
		case <-timeout:
			t.Fatal("Stream didn't finish within 2s")
		}
	}
	require.NotEmpty(t, snapshots, "at least one snapshot emitted")
	final := snapshots[len(snapshots)-1]
	require.Len(t, final, 2, "final snapshot has both branches")
	ids := []string{final[0].Message.ID, final[1].Message.ID}
	require.ElementsMatch(t, []string{"m-local", "m-server"}, ids)
	require.NoError(t, stream.Err())
}

// TestSearcherDedupesOverlappingBranches verifies the merger marks
// SourceBoth when the same message id arrives from local AND
// server, and the resulting snapshot has only one entry.
func TestSearcherDedupesOverlappingBranches(t *testing.T) {
	now := time.Now()
	overlap := mkMsg("m-overlap", "Overlap", now)
	local := &stubLocal{results: []store.MessageMatch{
		{Message: overlap, Rank: 0.5},
	}}
	server := &stubServer{results: []store.Message{overlap}}
	s := New(local, server, Options{
		EmitThrottle:  10 * time.Millisecond,
		ServerTimeout: time.Second,
	})
	stream := s.Search(context.Background(), Query{Text: "overlap"})

	final := drainFinal(t, stream)
	require.Len(t, final, 1, "duplicate id collapses to one row")
	require.Equal(t, SourceBoth, final[0].Source,
		"overlapping match transitions to SourceBoth")
}

// TestSearcherSortsReceivedDescThenSourcePriority pins the spec
// 06 §4.3 sort policy: SourceBoth before single-source ties; then
// received_at DESC.
func TestSearcherSortsReceivedDescThenSourcePriority(t *testing.T) {
	now := time.Now()
	older := mkMsg("m-old", "Old", now.Add(-3*time.Hour))
	newer := mkMsg("m-new", "New", now)

	// `m-old` is in BOTH branches; `m-new` only on server.
	local := &stubLocal{results: []store.MessageMatch{{Message: older, Rank: 1.0}}}
	server := &stubServer{results: []store.Message{older, newer}}
	s := New(local, server, Options{
		EmitThrottle:  10 * time.Millisecond,
		ServerTimeout: time.Second,
	})
	stream := s.Search(context.Background(), Query{Text: "old"})

	final := drainFinal(t, stream)
	require.Len(t, final, 2)
	// SourceBoth ranks ahead even though m-new has a more recent
	// received_at — high-confidence overlapping matches surface
	// first per spec §4.3.
	require.Equal(t, "m-old", final[0].Message.ID)
	require.Equal(t, SourceBoth, final[0].Source)
	require.Equal(t, "m-new", final[1].Message.ID)
	require.Equal(t, SourceServer, final[1].Source)
}

// TestSearcherLocalOnlySkipsServer covers the offline-mode
// signal: LocalOnly=true means the server branch never gets
// called, even when Searcher has a non-nil server.
func TestSearcherLocalOnlySkipsServer(t *testing.T) {
	local := &stubLocal{results: []store.MessageMatch{
		{Message: mkMsg("m-1", "x", time.Now()), Rank: 1.0},
	}}
	server := &stubServer{results: []store.Message{
		mkMsg("m-2", "y", time.Now()),
	}}
	s := New(local, server, Options{EmitThrottle: 10 * time.Millisecond})
	stream := s.Search(context.Background(), Query{
		Text: "x", LocalOnly: true,
	})
	final := drainFinal(t, stream)
	require.Len(t, final, 1)
	require.Equal(t, "m-1", final[0].Message.ID)
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Equal(t, 0, server.calls, "server branch must not run on LocalOnly")
}

// TestSearcherEmptyQueryClosesImmediately covers the empty-text
// short-circuit: the Stream's Done channel closes without
// dispatching either branch.
func TestSearcherEmptyQueryClosesImmediately(t *testing.T) {
	local := &stubLocal{}
	s := New(local, nil, Options{})
	stream := s.Search(context.Background(), Query{Text: ""})
	select {
	case <-stream.Done():
	case <-time.After(time.Second):
		t.Fatal("empty-query Stream didn't close")
	}
	local.mu.Lock()
	defer local.mu.Unlock()
	require.Equal(t, 0, local.calls)
}

// TestSearcherServerErrorDoesNotBreakLocal covers spec 06 §8: a
// server failure (timeout, 5xx, network) means the local results
// still surface; no Stream-level error.
func TestSearcherServerErrorDoesNotBreakLocal(t *testing.T) {
	local := &stubLocal{results: []store.MessageMatch{
		{Message: mkMsg("m-local", "x", time.Now()), Rank: 1.0},
	}}
	server := &stubServer{err: errors.New("simulated 503")}
	s := New(local, server, Options{
		EmitThrottle:  10 * time.Millisecond,
		ServerTimeout: time.Second,
	})
	stream := s.Search(context.Background(), Query{Text: "x"})
	final := drainFinal(t, stream)
	require.Len(t, final, 1)
	require.Equal(t, "m-local", final[0].Message.ID)
	// The server error IS surfaced via Err so callers that care
	// can paint a status hint.
	require.Error(t, stream.Err())
}

// TestSearcherCancelStopsBranches covers the Stream.Cancel
// contract: callers that abandon a search before completion get
// no further updates and Done closes cleanly.
func TestSearcherCancelStopsBranches(t *testing.T) {
	local := &stubLocal{
		results: []store.MessageMatch{{Message: mkMsg("m-1", "x", time.Now()), Rank: 1.0}},
		delay:   500 * time.Millisecond,
	}
	server := &stubServer{
		results: []store.Message{mkMsg("m-2", "y", time.Now())},
		delay:   500 * time.Millisecond,
	}
	s := New(local, server, Options{
		EmitThrottle:  10 * time.Millisecond,
		ServerTimeout: time.Second,
	})
	stream := s.Search(context.Background(), Query{Text: "x"})
	stream.Cancel()
	select {
	case <-stream.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelled Stream didn't close Done within 1s")
	}
}

// TestSearcherFirstLocalResultLatencyUnder100ms is the spec 06 §6
// budget: a small local result set (≤200 hits) emits the first
// snapshot in under 100ms. We exercise the full Searcher path
// (parse + local FTS stub + merge throttle) and pin the
// observable wall clock.
func TestSearcherFirstLocalResultLatencyUnder100ms(t *testing.T) {
	now := time.Now()
	results := make([]store.MessageMatch, 50)
	for i := range results {
		results[i] = store.MessageMatch{
			Message: mkMsg("m-"+string(rune('a'+i%26)), "x", now),
			Rank:    float64(i),
		}
	}
	local := &stubLocal{results: results}
	s := New(local, nil, Options{EmitThrottle: 10 * time.Millisecond})
	start := time.Now()
	stream := s.Search(context.Background(), Query{Text: "x"})
	select {
	case <-stream.Updates():
		elapsed := time.Since(start)
		require.Less(t, elapsed.Milliseconds(), int64(100),
			"first local snapshot in <100ms")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no first snapshot within 200ms")
	}
}

// drainFinal reads every Update from a Stream and returns the
// last snapshot received. Test helper: blocks up to 2s.
func drainFinal(t *testing.T, stream *Stream) []Result {
	t.Helper()
	var last []Result
	timeout := time.After(2 * time.Second)
	for {
		select {
		case snap, ok := <-stream.Updates():
			if !ok {
				return last
			}
			last = snap
		case <-stream.Done():
			// Drain any queued snapshots that arrived just before
			// the channel closed.
			for {
				select {
				case snap, ok := <-stream.Updates():
					if !ok {
						return last
					}
					last = snap
				default:
					return last
				}
			}
		case <-timeout:
			t.Fatal("drainFinal timed out")
		}
	}
}
