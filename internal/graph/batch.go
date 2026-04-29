package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// MaxBatchSize is the Graph $batch hard limit. Callers must chunk
// before calling [Client.ExecuteBatch].
const MaxBatchSize = 20

// SubRequest is a single inner request inside a $batch payload. ID is
// the caller-supplied correlation key; it must be unique within the
// batch but can be reused across batches.
type SubRequest struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    any               `json:"body,omitempty"`
}

// SubResponse is the parsed inner response. Body is the raw JSON
// (callers decode based on Method/URL). GraphError is populated when
// Status >= 400.
type SubResponse struct {
	ID         string
	Status     int
	Headers    map[string]string
	Body       json.RawMessage
	GraphError *GraphError
}

// BatchBuilder assembles a $batch payload. Reuse via NewBatch() per
// chunk; do NOT share across chunks (the slice mutates).
type BatchBuilder struct {
	requests []SubRequest
}

// NewBatch returns an empty builder.
func NewBatch() *BatchBuilder { return &BatchBuilder{} }

// Add appends a sub-request. Returns the builder for chaining.
func (b *BatchBuilder) Add(req SubRequest) *BatchBuilder {
	b.requests = append(b.requests, req)
	return b
}

// Len returns the number of pending sub-requests.
func (b *BatchBuilder) Len() int { return len(b.requests) }

// Build returns the JSON payload for POST /$batch.
func (b *BatchBuilder) Build() ([]byte, error) {
	if len(b.requests) == 0 {
		return nil, fmt.Errorf("graph: empty batch")
	}
	if len(b.requests) > MaxBatchSize {
		return nil, fmt.Errorf("graph: batch size %d exceeds limit %d", len(b.requests), MaxBatchSize)
	}
	// Default Content-Type for POST/PATCH/PUT bodies — Graph rejects
	// these without it.
	for i := range b.requests {
		if b.requests[i].Body != nil && b.requests[i].Headers == nil {
			b.requests[i].Headers = map[string]string{"Content-Type": "application/json"}
		}
	}
	payload := struct {
		Requests []SubRequest `json:"requests"`
	}{Requests: b.requests}
	return json.Marshal(payload)
}

// ExecuteBatch posts the payload built by [BatchBuilder.Build] to
// Graph's /$batch endpoint and returns parsed sub-responses in the
// same order as the request list (correlated via SubRequest.ID).
//
// A non-2xx HTTP response on the OUTER batch call returns an error.
// Per-sub-request failures are surfaced via SubResponse.GraphError;
// callers decide how to react (retry, rollback, etc.).
func (c *Client) ExecuteBatch(ctx context.Context, builder *BatchBuilder) ([]SubResponse, error) {
	payload, err := builder.Build()
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(ctx, http.MethodPost, "/$batch", bytes.NewReader(payload), http.Header{
		"Content-Type": []string{"application/json"},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseError(resp)
	}
	return parseBatchResponse(resp.Body, builder.requests)
}

// batchResponseEnvelope mirrors the Graph $batch response shape.
type batchResponseEnvelope struct {
	Responses []rawSubResponse `json:"responses"`
}

type rawSubResponse struct {
	ID      string            `json:"id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// parseBatchResponse decodes the envelope and returns sub-responses
// in the order of the original requests (Graph may return them out of
// order).
func parseBatchResponse(body interface{ Read(p []byte) (int, error) }, reqs []SubRequest) ([]SubResponse, error) {
	var env batchResponseEnvelope
	if err := json.NewDecoder(body).Decode(&env); err != nil {
		return nil, fmt.Errorf("graph: decode batch response: %w", err)
	}
	byID := make(map[string]rawSubResponse, len(env.Responses))
	for _, r := range env.Responses {
		byID[r.ID] = r
	}
	out := make([]SubResponse, 0, len(reqs))
	for _, req := range reqs {
		raw, ok := byID[req.ID]
		if !ok {
			out = append(out, SubResponse{
				ID:     req.ID,
				Status: 0,
				GraphError: &GraphError{
					StatusCode: 0,
					Code:       "missing",
					Message:    "no response for request id " + strconv.Quote(req.ID),
				},
			})
			continue
		}
		sr := SubResponse{
			ID:      raw.ID,
			Status:  raw.Status,
			Headers: raw.Headers,
			Body:    raw.Body,
		}
		if raw.Status >= 400 {
			sr.GraphError = parseSubError(raw)
		}
		out = append(out, sr)
	}
	return out, nil
}

// parseSubError extracts a GraphError from an embedded sub-response
// body. The Graph error envelope sits inside Body as {"error":{...}}.
func parseSubError(raw rawSubResponse) *GraphError {
	ge := &GraphError{StatusCode: raw.Status}
	if len(raw.Body) > 0 {
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw.Body, &payload); err == nil {
			ge.Code = payload.Error.Code
			ge.Message = payload.Error.Message
		}
	}
	return ge
}
