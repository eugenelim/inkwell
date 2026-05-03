package graph

import (
	"context"
	"errors"
	"sync"
)

// ExecuteAllOpts configures the fan-out behaviour of [Client.ExecuteAll].
type ExecuteAllOpts struct {
	// Concurrency caps how many chunks may be in-flight simultaneously.
	// Zero or negative means 1 (serial).
	Concurrency int
	// MaxRetries is passed to [executeChunkWithRetry] for each chunk.
	MaxRetries int
	// OnProgress, if non-nil, is called after each chunk finishes.
	// done is the number of sub-requests completed so far; total is
	// len(reqs). Calls are serialised; do not block inside OnProgress.
	OnProgress func(done, total int)
}

// ExecuteAll splits reqs into MaxBatchSize chunks, fans them out up to
// opts.Concurrency at a time, and returns all responses in the same
// order as reqs. Per-chunk outer errors are converted to per-response
// GraphErrors so callers always get a full result slice.
func (c *Client) ExecuteAll(ctx context.Context, reqs []SubRequest, opts ExecuteAllOpts) ([]SubResponse, error) {
	if len(reqs) == 0 {
		return nil, nil
	}

	concurrency := opts.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	// Split into chunks.
	var chunks [][]SubRequest
	for start := 0; start < len(reqs); start += MaxBatchSize {
		end := start + MaxBatchSize
		if end > len(reqs) {
			end = len(reqs)
		}
		chunks = append(chunks, reqs[start:end])
	}

	type chunkResult struct {
		index     int
		responses []SubResponse
		err       error
	}

	results := make([]chunkResult, len(chunks))
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	var progressMu sync.Mutex
	done := 0
	total := len(reqs)

	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, ch []SubRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			resps, err := c.executeChunkWithRetry(ctx, ch, opts.MaxRetries)
			results[idx] = chunkResult{index: idx, responses: resps, err: err}

			if opts.OnProgress != nil {
				progressMu.Lock()
				done += len(ch)
				d := done
				progressMu.Unlock()
				opts.OnProgress(d, total)
			}
		}(i, chunk)
	}

	wg.Wait()

	// Merge results in original order, converting chunk errors to per-response GraphErrors.
	out := make([]SubResponse, 0, len(reqs))
	for i, chunk := range chunks {
		cr := results[i]
		if cr.err != nil {
			for _, r := range chunk {
				ge := toGraphError(cr.err)
				out = append(out, SubResponse{
					ID:         r.ID,
					Status:     ge.StatusCode,
					GraphError: ge,
				})
			}
			continue
		}
		out = append(out, cr.responses...)
	}
	return out, nil
}

// toGraphError wraps an arbitrary error as a *GraphError for embedding
// into a SubResponse when an entire chunk fails.
func toGraphError(err error) *GraphError {
	var ge *GraphError
	if errors.As(err, &ge) {
		return ge
	}
	return &GraphError{
		StatusCode: 500,
		Code:       "InternalError",
		Message:    err.Error(),
	}
}
