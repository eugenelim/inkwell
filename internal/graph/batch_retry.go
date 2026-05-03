package graph

import (
	"context"
	"errors"
	"time"
)

// executeChunkWithRetry wraps ExecuteBatch with per-sub-request 429
// retry logic. On each attempt it:
//  1. Fires the batch.
//  2. Scans responses for 429 status codes; those sub-requests are
//     re-batched and retried after the Retry-After delay.
//  3. If the outer HTTP call itself returns a throttle error (429/503),
//     the whole chunk is retried after the Retry-After delay.
//
// Responses are returned in the same order as the input reqs.
// Sub-requests that exhaust maxRetries are returned with Status 429.
func (c *Client) executeChunkWithRetry(ctx context.Context, reqs []SubRequest, maxRetries int) ([]SubResponse, error) {
	all := make(map[string]SubResponse, len(reqs))
	remaining := make([]SubRequest, len(reqs))
	copy(remaining, reqs)

	for attempt := 0; len(remaining) > 0; attempt++ {
		builder := NewBatch()
		for _, r := range remaining {
			builder.Add(r)
		}

		responses, err := c.ExecuteBatch(ctx, builder)
		if err != nil {
			// Outer call throttled — retry the whole chunk after the
			// Retry-After delay, up to budget.
			if IsThrottled(err) && attempt < maxRetries {
				delay := retryAfterFromErr(err)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
					continue
				}
			}
			return nil, err
		}

		var retry []SubRequest
		var maxDelay time.Duration
		for i, sr := range responses {
			if i >= len(remaining) {
				break
			}
			if sr.Status == 429 && attempt < maxRetries {
				d := retryAfterFromHeaders(sr.Headers)
				if d > maxDelay {
					maxDelay = d
				}
				retry = append(retry, remaining[i])
			} else {
				all[sr.ID] = sr
			}
		}

		remaining = retry
		if len(remaining) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			remaining = nil
		case <-time.After(maxDelay):
		}
	}

	// Anything still remaining exhausted retries — mark as 429 failed.
	for _, r := range remaining {
		all[r.ID] = SubResponse{
			ID:     r.ID,
			Status: 429,
			GraphError: &GraphError{
				StatusCode: 429,
				Code:       "TooManyRequests",
				Message:    "retry budget exhausted",
			},
		}
	}

	// Return in original input order.
	out := make([]SubResponse, len(reqs))
	for i, r := range reqs {
		out[i] = all[r.ID]
	}
	return out, nil
}

// retryAfterFromHeaders extracts the Retry-After delay from a
// sub-response header map, falling back to 1s if absent or
// unparseable.
func retryAfterFromHeaders(headers map[string]string) time.Duration {
	const fallback = time.Second
	if headers == nil {
		return fallback
	}
	v := headers["Retry-After"]
	if v == "" {
		v = headers["retry-after"]
	}
	d := parseRetryAfter(v)
	if d <= 0 {
		return fallback
	}
	return d
}

// retryAfterFromErr reads the Retry-After string from a *GraphError
// and parses it as seconds. Returns 1s if absent or unparseable.
func retryAfterFromErr(err error) time.Duration {
	var ge *GraphError
	if !errors.As(err, &ge) || ge.RetryAfter == "" {
		return time.Second
	}
	d := parseRetryAfter(ge.RetryAfter)
	if d <= 0 {
		return time.Second
	}
	return d
}
