// Package search implements the spec 06 hybrid search: local FTS5
// + Graph $search running concurrently, with a streaming merger
// that emits progressive result snapshots to the UI.
//
// The package's public surface is deliberately small: callers run
// `Searcher.Search(ctx, q) → *Stream` and consume `Stream.Updates()`
// + `Stream.Done()`. Cancellation flows through the supplied
// context AND `Stream.Cancel()` for the UI's "Esc out of search"
// path.
package search

import (
	"context"
	"sync"
	"time"

	"github.com/eugenelim/inkwell/internal/store"
)

// ResultSource records which branch produced a Result. After the
// merger sees the same message in both branches, the Result
// transitions from Local or Server to Both — the canonical
// "everyone agrees" state that signals a high-confidence match.
type ResultSource int

const (
	// SourceLocal — emitted from the local FTS5 path only.
	SourceLocal ResultSource = iota
	// SourceServer — emitted from the Graph $search path only.
	SourceServer
	// SourceBoth — same message id arrived from both branches.
	SourceBoth
)

// Result is one merged search hit. Snippet carries match-
// highlighted context drawn from the local body preview when
// available; the server branch doesn't return snippets so its
// Result.Snippet is empty until Both takes over and copies the
// local one in.
type Result struct {
	Message store.Message
	Snippet string
	Source  ResultSource
	// Score is BM25 from the FTS5 path (lower = better) or
	// receivedDateTime.Unix() from the server path (higher =
	// newer). The merger only uses Score to break ties within
	// same-source results — the canonical sort is received-date
	// descending per spec 06 §4.3.
	Score float64
}

// Query parameterises a single Searcher.Search call.
type Query struct {
	// Text is the user-typed expression including any field
	// prefixes ("from:bob q4 review"). Empty Text returns no
	// results without running either branch.
	Text string
	// FolderID scopes both branches to a single folder. Empty
	// searches all subscribed folders. Spec 06 §5.3.
	FolderID string
	// Limit caps the merged result count. 0 falls back to
	// search.default_result_limit (200 per spec 06 §7).
	Limit int
	// LocalOnly skips the server branch entirely. Used when
	// offline or when the user explicitly opts out via a CLI flag.
	LocalOnly bool
	// ServerOnly skips the local branch. Used in tests; users
	// should not normally need this — local-first is the spec's
	// invariant.
	ServerOnly bool
}

// Stream is the streaming Searcher result handle. Updates emits
// each time the merged result set changes; Done closes when both
// branches finish (or after Cancel). A single Stream lifetime
// covers exactly one Search call.
type Stream struct {
	updates chan []Result
	done    chan struct{}
	cancel  context.CancelFunc

	mu  sync.Mutex
	err error
}

// Updates returns the channel of progressive merged result
// snapshots. The slice handed to a receiver is the FULL current
// result set (not an incremental delta) so the UI can replace its
// view directly.
func (s *Stream) Updates() <-chan []Result { return s.updates }

// Done returns a channel closed once both branches have finished.
// Receivers can Done-select alongside ctx for clean shutdown.
func (s *Stream) Done() <-chan struct{} { return s.done }

// Err returns the first error observed by either branch, or nil.
// Safe to call after Done closes.
func (s *Stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Cancel terminates both branches and closes Done. Safe to call
// concurrently and idempotent.
func (s *Stream) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Stream) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

// LocalSearcher narrows store.Store to its FTS5 search method so
// the search package's tests don't need a full store instance.
type LocalSearcher interface {
	Search(ctx context.Context, q store.SearchQuery) ([]store.MessageMatch, error)
}

// ServerSearcher is the consumer-side seam for the Graph $search
// endpoint. graph.Client satisfies this via a tiny adapter; tests
// fake it directly.
type ServerSearcher interface {
	SearchMessages(ctx context.Context, q ServerQuery) ([]store.Message, error)
}

// ServerQuery describes one Graph search call.
type ServerQuery struct {
	Query    string
	FolderID string
	Top      int
}

// Options configures a Searcher. EmitThrottle gates how often the
// merger flushes Updates so a fast-emitting branch doesn't thrash
// the UI; ServerTimeout gates the server branch ctx; AccountID is
// threaded into local FTS scoping.
type Options struct {
	EmitThrottle  time.Duration
	ServerTimeout time.Duration
	DefaultLimit  int
	AccountID     int64
}

// Searcher is the public hybrid-search entry point. Constructed
// via [New]; callers run Search per query.
type Searcher struct {
	local  LocalSearcher
	server ServerSearcher
	opts   Options
	clock  func() time.Time
}

// New returns a Searcher. The server arg may be nil (e.g., when
// the binary is launched offline) — Search auto-falls-back to the
// local-only path. Defaults: 100ms EmitThrottle, 5s ServerTimeout,
// 200 DefaultLimit if zero.
func New(local LocalSearcher, server ServerSearcher, opts Options) *Searcher {
	if opts.EmitThrottle <= 0 {
		opts.EmitThrottle = 100 * time.Millisecond
	}
	if opts.ServerTimeout <= 0 {
		opts.ServerTimeout = 5 * time.Second
	}
	if opts.DefaultLimit <= 0 {
		opts.DefaultLimit = 200
	}
	return &Searcher{
		local:  local,
		server: server,
		opts:   opts,
		clock:  time.Now,
	}
}

// Search kicks off a hybrid query and returns a *Stream. The two
// branches run in their own goroutines; the merger goroutine
// dedups, sorts, and emits to Stream.Updates via a throttled
// debouncer. All goroutines exit when ctx cancels OR when both
// branches finish naturally.
func (s *Searcher) Search(ctx context.Context, q Query) *Stream {
	if q.Limit <= 0 {
		q.Limit = s.opts.DefaultLimit
	}
	cctx, cancel := context.WithCancel(ctx)
	st := &Stream{
		updates: make(chan []Result, 4),
		done:    make(chan struct{}),
		cancel:  cancel,
	}

	if q.Text == "" {
		// Empty query → no work; close immediately so callers
		// can rely on Done firing without inspecting Text first.
		close(st.updates)
		close(st.done)
		return st
	}

	parsed := ParseQuery(q.Text)
	mrg := newMerger(s.opts.EmitThrottle, q.Limit)
	mrg.start(cctx, st.updates)

	var wg sync.WaitGroup
	if !q.ServerOnly {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.runLocal(cctx, q, parsed, mrg); err != nil {
				st.setErr(err)
			}
		}()
	}
	if !q.LocalOnly && s.server != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tctx, tcancel := context.WithTimeout(cctx, s.opts.ServerTimeout)
			defer tcancel()
			if err := s.runServer(tctx, q, parsed, mrg); err != nil {
				st.setErr(err)
			}
		}()
	}

	go func() {
		wg.Wait()
		mrg.close()
		<-mrg.flushed
		close(st.updates)
		close(st.done)
		cancel()
	}()
	return st
}

// runLocal executes the FTS5 branch and pushes its results into
// the merger. Errors flow up via the Stream's err.
func (s *Searcher) runLocal(ctx context.Context, q Query, parsed ParsedQuery, mrg *merger) error {
	if s.local == nil {
		return nil
	}
	fts := BuildFTSQuery(parsed)
	if fts == "" {
		return nil
	}
	hits, err := s.local.Search(ctx, store.SearchQuery{
		AccountID: s.opts.AccountID,
		FolderID:  q.FolderID,
		Query:     fts,
		Limit:     q.Limit,
	})
	if err != nil {
		return err
	}
	results := make([]Result, 0, len(hits))
	for _, h := range hits {
		results = append(results, Result{
			Message: h.Message,
			Snippet: highlightSnippet(h.Message.BodyPreview, parsed.PlainTerms),
			Source:  SourceLocal,
			Score:   h.Rank,
		})
	}
	mrg.add(results)
	return nil
}

// runServer executes the Graph $search branch and pushes its
// results into the merger. Timeouts emit whatever the server
// returned before the deadline; other errors propagate via the
// Stream's err.
func (s *Searcher) runServer(ctx context.Context, q Query, parsed ParsedQuery, mrg *merger) error {
	if s.server == nil {
		return nil
	}
	graphQuery := BuildGraphSearchQuery(parsed)
	if graphQuery == "" {
		return nil
	}
	msgs, err := s.server.SearchMessages(ctx, ServerQuery{
		Query:    graphQuery,
		FolderID: q.FolderID,
		Top:      q.Limit,
	})
	if err != nil {
		// Server timeout / 429 / network failure: spec 06 §8 says
		// the user gets local-only results with a clear status.
		// Don't surface as a Stream err — the local branch's
		// success is the dominant signal. Log via setErr so callers
		// who care can inspect.
		if ctx.Err() == context.DeadlineExceeded {
			return nil
		}
		return err
	}
	results := make([]Result, 0, len(msgs))
	for _, m := range msgs {
		results = append(results, Result{
			Message: m,
			Source:  SourceServer,
			// Score is unix-seconds — newer wins ties within the
			// server bucket. Merge sort uses received_at as the
			// canonical key so this is defence-in-depth.
			Score: float64(m.ReceivedAt.Unix()),
		})
	}
	mrg.add(results)
	return nil
}
