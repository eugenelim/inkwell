package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ilog "github.com/eugenelim/inkwell/internal/log"
)

// newBatchClient builds a Client pointed at the given httptest.Server with
// MaxRetries=0 on the outer transport so the retry-after logic is driven by
// executeChunkWithRetry / ExecuteAll, not the HTTP transport layer.
func newBatchClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	logger, _ := ilog.NewCaptured(ilog.Options{})
	c, err := NewClient(&fakeAuth{}, Options{
		BaseURL:    srv.URL,
		Logger:     logger,
		MaxRetries: 0,
	})
	require.NoError(t, err)
	return c
}

// TestParseRetryAfterHeaders checks both case variants and fallback.
func TestParseRetryAfterHeaders(t *testing.T) {
	require.Equal(t, 5*time.Second, retryAfterFromHeaders(map[string]string{"Retry-After": "5"}))
	require.Equal(t, 10*time.Second, retryAfterFromHeaders(map[string]string{"retry-after": "10"}))
	require.Equal(t, time.Second, retryAfterFromHeaders(nil))
	require.Equal(t, time.Second, retryAfterFromHeaders(map[string]string{}))
	require.Equal(t, time.Second, retryAfterFromHeaders(map[string]string{"Retry-After": "bad"}))
}

// TestExecuteChunkWithRetryOuter429 confirms that when ExecuteBatch returns
// an outer 429 (IsThrottled), the whole chunk is retried.
func TestExecuteChunkWithRetryOuter429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var payload struct {
			Requests []struct{ ID string `json:"id"` } `json:"requests"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		responses := make([]map[string]any, len(payload.Requests))
		for i, req := range payload.Requests {
			responses[i] = map[string]any{"id": req.ID, "status": 200}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"responses": responses})
	}))
	defer srv.Close()

	c := newBatchClient(t, srv)
	reqs := []SubRequest{{ID: "r1", Method: "PATCH", URL: "/me/messages/m-1"}}
	responses, err := c.executeChunkWithRetry(context.Background(), reqs, 2)
	require.NoError(t, err)
	require.Len(t, responses, 1)
	require.Equal(t, 200, responses[0].Status)
	require.Equal(t, int32(2), calls.Load(), "outer 429 should cause one retry")
}

// TestExecuteChunkWithRetrySubrequest429 confirms that a 429 sub-response
// causes just that sub-request to be retried.
func TestExecuteChunkWithRetrySubrequest429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		var payload struct {
			Requests []struct{ ID string `json:"id"` } `json:"requests"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			responses := make([]map[string]any, len(payload.Requests))
			for i, req := range payload.Requests {
				responses[i] = map[string]any{
					"id":      req.ID,
					"status":  429,
					"headers": map[string]string{"Retry-After": "0"},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"responses": responses})
			return
		}
		responses := make([]map[string]any, len(payload.Requests))
		for i, req := range payload.Requests {
			responses[i] = map[string]any{"id": req.ID, "status": 200}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"responses": responses})
	}))
	defer srv.Close()

	c := newBatchClient(t, srv)
	reqs := []SubRequest{{ID: "r1", Method: "PATCH", URL: "/me/messages/m-1"}}
	responses, err := c.executeChunkWithRetry(context.Background(), reqs, 2)
	require.NoError(t, err)
	require.Len(t, responses, 1)
	require.Equal(t, 200, responses[0].Status)
	require.Equal(t, int32(2), calls.Load())
}

// TestExecuteAllInputOrderPreserved confirms that ExecuteAll returns responses
// in the same order as the input reqs, regardless of chunk completion order.
func TestExecuteAllInputOrderPreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Requests []struct{ ID string `json:"id"` } `json:"requests"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		responses := make([]map[string]any, len(payload.Requests))
		for i, req := range payload.Requests {
			responses[i] = map[string]any{"id": req.ID, "status": 200}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"responses": responses})
	}))
	defer srv.Close()

	c := newBatchClient(t, srv)
	reqs := make([]SubRequest, 25)
	for i := range reqs {
		reqs[i] = SubRequest{ID: "r" + itoa(i), Method: "PATCH", URL: "/me/messages/m-x"}
	}
	responses, err := c.ExecuteAll(context.Background(), reqs, ExecuteAllOpts{Concurrency: 3, MaxRetries: 0})
	require.NoError(t, err)
	require.Len(t, responses, 25)
	for i, r := range responses {
		require.Equal(t, reqs[i].ID, r.ID, "response[%d] must match input req[%d]", i, i)
	}
}

// TestExecuteAllOnProgressCallback confirms the OnProgress callback is
// invoked once per chunk with an increasing done count.
func TestExecuteAllOnProgressCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Requests []struct{ ID string `json:"id"` } `json:"requests"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		responses := make([]map[string]any, len(payload.Requests))
		for i, req := range payload.Requests {
			responses[i] = map[string]any{"id": req.ID, "status": 200}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"responses": responses})
	}))
	defer srv.Close()

	c := newBatchClient(t, srv)
	reqs := make([]SubRequest, 25)
	for i := range reqs {
		reqs[i] = SubRequest{ID: "r" + itoa(i), Method: "PATCH", URL: "/me/messages/m-x"}
	}

	var callCount atomic.Int32
	responses, err := c.ExecuteAll(context.Background(), reqs, ExecuteAllOpts{
		Concurrency: 1,
		MaxRetries:  0,
		OnProgress: func(done, total int) {
			callCount.Add(1)
			require.Equal(t, 25, total)
			require.Greater(t, done, 0)
			require.LessOrEqual(t, done, total)
		},
	})
	require.NoError(t, err)
	require.Len(t, responses, 25)
	require.Equal(t, int32(2), callCount.Load(), "25 reqs → 2 chunks → 2 progress calls")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	buf := make([]byte, 0, 4)
	for i > 0 {
		buf = append(buf, byte('0'+i%10))
		i /= 10
	}
	// Reverse.
	for lo, hi := 0, len(buf)-1; lo < hi; lo, hi = lo+1, hi-1 {
		buf[lo], buf[hi] = buf[hi], buf[lo]
	}
	return string(buf)
}
