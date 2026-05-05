package search

import (
	"context"
	"sort"
	"sync"
	"time"
)

// merger holds the canonical merged result set across both
// branches. Each `add` dedups by message ID, marks Both for
// overlapping rows, resorts by spec 06 §4.3 policy
// (received_at DESC unless sortByRelevance), truncates to limit,
// and signals the debouncer to emit on the next throttle window.
type merger struct {
	throttle        time.Duration
	limit           int
	sortByRelevance bool

	mu      sync.Mutex
	results map[string]*Result // keyed by message id

	pending chan struct{} // buffered=1; receiver fan-in for emit
	closed  chan struct{} // signalled when both branches done
	flushed chan struct{} // closed when the debouncer goroutine exits
}

func newMerger(throttle time.Duration, limit int, sortByRelevance bool) *merger {
	return &merger{
		throttle:        throttle,
		limit:           limit,
		sortByRelevance: sortByRelevance,
		results:         make(map[string]*Result),
		pending:         make(chan struct{}, 1),
		closed:          make(chan struct{}),
		flushed:         make(chan struct{}),
	}
}

// add merges a batch of results from one branch into the canonical
// set. Same-id matches transition to SourceBoth and absorb the
// local snippet (if any) so the final view always carries the
// best-available context.
func (m *merger) add(rs []Result) {
	if len(rs) == 0 {
		return
	}
	m.mu.Lock()
	for _, r := range rs {
		if r.Message.ID == "" {
			continue
		}
		existing, ok := m.results[r.Message.ID]
		if !ok {
			cp := r
			m.results[r.Message.ID] = &cp
			continue
		}
		// Dedup: mark as Both, prefer the local Snippet (it has
		// match-highlighting; the server doesn't return one).
		existing.Source = SourceBoth
		if existing.Snippet == "" && r.Snippet != "" {
			existing.Snippet = r.Snippet
		}
		// Score: keep the lower BM25 (local). Server score is a
		// unix timestamp and would otherwise dominate the merge.
		if r.Source == SourceLocal && r.Score < existing.Score {
			existing.Score = r.Score
		}
	}
	m.mu.Unlock()
	m.kick()
}

// snapshot produces the current sorted+truncated result slice.
// Default (spec 06 §4.3): received_at DESC; SourceBoth ranks ahead of
// single-source ties; BM25 score is the within-bucket tiebreaker.
// When sortByRelevance is set: BM25 score ASC (lower = better) is the
// primary key, SourceBoth still ranks ahead of single-source ties.
func (m *merger) snapshot() []Result {
	m.mu.Lock()
	out := make([]Result, 0, len(m.results))
	for _, r := range m.results {
		out = append(out, *r)
	}
	m.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool {
		// Both > Local > Server when a single message is in only
		// one bucket; SourceBoth comes first so the strongest
		// matches surface at the top.
		bi := sourcePriority(out[i].Source)
		bj := sourcePriority(out[j].Source)
		if bi != bj {
			return bi < bj
		}
		if m.sortByRelevance {
			// BM25 ascending (lower score = more relevant in FTS5).
			return out[i].Score < out[j].Score
		}
		ri := out[i].Message.ReceivedAt
		rj := out[j].Message.ReceivedAt
		if !ri.Equal(rj) {
			return ri.After(rj)
		}
		return out[i].Score < out[j].Score
	})
	if m.limit > 0 && len(out) > m.limit {
		out = out[:m.limit]
	}
	return out
}

// sourcePriority is the rank-by-source ordering used in snapshot.
// Both = 0 (highest), Local = 1, Server = 2.
func sourcePriority(s ResultSource) int {
	switch s {
	case SourceBoth:
		return 0
	case SourceLocal:
		return 1
	case SourceServer:
		return 2
	}
	return 3
}

// kick signals the debouncer there's a new batch to consider.
// Buffer-1 keeps multiple rapid adds collapsing into a single
// emit window without dropping the wakeup.
func (m *merger) kick() {
	select {
	case m.pending <- struct{}{}:
	default:
	}
}

// start launches the debouncer goroutine. Each `add` schedules an
// emit; the debouncer holds the latest snapshot for `throttle`
// before flushing, so a burst of adds collapses into one update.
// On close, the debouncer drains any pending and exits.
func (m *merger) start(ctx context.Context, out chan<- []Result) {
	go func() {
		defer close(m.flushed)
		var (
			timer *time.Timer
			fired <-chan time.Time
		)
		armTimer := func() {
			if timer == nil {
				timer = time.NewTimer(m.throttle)
				fired = timer.C
				return
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(m.throttle)
			fired = timer.C
		}
		flush := func() {
			snap := m.snapshot()
			select {
			case out <- snap:
			case <-ctx.Done():
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.pending:
				armTimer()
			case <-fired:
				fired = nil
				flush()
			case <-m.closed:
				// Drain any remaining pending kick.
				select {
				case <-m.pending:
				default:
				}
				flush()
				return
			}
		}
	}()
}

// close signals the debouncer to flush and exit. Called by the
// Searcher when both branches finish.
func (m *merger) close() {
	select {
	case <-m.closed:
		return
	default:
	}
	close(m.closed)
}
