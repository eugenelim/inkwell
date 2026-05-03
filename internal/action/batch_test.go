package action

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// TestBatchExecuteMarkReadHappyPath fires mark_read on 3 messages,
// asserts (a) the $batch payload was a single POST to /$batch with
// 3 PATCH sub-requests, (b) all three local rows flipped to is_read=1,
// (c) all 3 results carry no error.
func TestBatchExecuteMarkReadHappyPath(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	// Seed two extra messages on top of the executor harness's m-1.
	for _, id := range []string{"m-2", "m-3"} {
		require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
			ID: id, AccountID: accID, FolderID: "f-inbox", Subject: id,
			FromAddress: "x@example.invalid", IsRead: false,
		}))
	}

	var batchCalls atomic.Int32
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
		batchCalls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		var payload struct {
			Requests []struct {
				ID     string         `json:"id"`
				Method string         `json:"method"`
				URL    string         `json:"url"`
				Body   map[string]any `json:"body"`
			} `json:"requests"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Len(t, payload.Requests, 3)

		// Echo a 200 for each sub-request.
		out := struct {
			Responses []map[string]any `json:"responses"`
		}{}
		for _, req := range payload.Requests {
			require.Equal(t, "PATCH", req.Method)
			require.Equal(t, true, req.Body["isRead"])
			out.Responses = append(out.Responses, map[string]any{
				"id":     req.ID,
				"status": 200,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	results, err := exec.BatchExecute(context.Background(), accID,
		store.ActionMarkRead, []string{"m-1", "m-2", "m-3"})
	require.NoError(t, err)
	require.Len(t, results, 3)
	for _, r := range results {
		require.NoError(t, r.Err, "message %s should succeed", r.MessageID)
	}
	require.Equal(t, int32(1), batchCalls.Load(), "single $batch call for ≤20 messages")

	for _, id := range []string{"m-1", "m-2", "m-3"} {
		got, err := st.GetMessage(context.Background(), id)
		require.NoError(t, err)
		require.True(t, got.IsRead, "%s should be marked read locally", id)
	}
}

// TestBatchExecutePartialFailureRollsBackFailedOnly fires batch with
// 3 messages where m-2's sub-response is 403; m-1 and m-3 succeed.
// Asserts m-1 and m-3 flipped, m-2 reverted, results carry the error
// only for m-2.
func TestBatchExecutePartialFailureRollsBackFailedOnly(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	for _, id := range []string{"m-2", "m-3"} {
		require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
			ID: id, AccountID: accID, FolderID: "f-inbox", Subject: id,
			FromAddress: "x@example.invalid", IsRead: false,
		}))
	}

	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Requests []struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			} `json:"requests"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))

		out := struct {
			Responses []map[string]any `json:"responses"`
		}{}
		for _, req := range payload.Requests {
			status := 200
			var body any
			if req.URL == "/me/messages/m-2" {
				status = 403
				body = map[string]any{
					"error": map[string]string{"code": "forbidden", "message": "nope"},
				}
			}
			r := map[string]any{"id": req.ID, "status": status}
			if body != nil {
				r["body"] = body
			}
			out.Responses = append(out.Responses, r)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	results, err := exec.BatchExecute(context.Background(), accID,
		store.ActionMarkRead, []string{"m-1", "m-2", "m-3"})
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Per-message results.
	byID := map[string]error{}
	for _, r := range results {
		byID[r.MessageID] = r.Err
	}
	require.NoError(t, byID["m-1"])
	require.Error(t, byID["m-2"], "m-2 surfaced a 403")
	require.NoError(t, byID["m-3"])

	// Local state: m-1 + m-3 flipped, m-2 rolled back.
	m1, _ := st.GetMessage(context.Background(), "m-1")
	require.True(t, m1.IsRead)
	m2, _ := st.GetMessage(context.Background(), "m-2")
	require.False(t, m2.IsRead, "m-2 must be rolled back")
	m3, _ := st.GetMessage(context.Background(), "m-3")
	require.True(t, m3.IsRead)
}

// TestBatchExecuteChunksAt20 confirms 25 messages produce two $batch
// calls (20 + 5).
func TestBatchExecuteChunksAt20(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	ids := []string{"m-1"}
	for i := 2; i <= 25; i++ {
		id := "m-" + itoa(i)
		ids = append(ids, id)
		require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
			ID: id, AccountID: accID, FolderID: "f-inbox", Subject: id,
			FromAddress: "x@example.invalid", IsRead: false,
		}))
	}

	var calls atomic.Int32
	var sizesMu sync.Mutex
	var sizes []int
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			Requests []struct {
				ID string `json:"id"`
			} `json:"requests"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		sizesMu.Lock()
		sizes = append(sizes, len(payload.Requests))
		sizesMu.Unlock()
		out := struct {
			Responses []map[string]any `json:"responses"`
		}{}
		for _, req := range payload.Requests {
			out.Responses = append(out.Responses, map[string]any{"id": req.ID, "status": 200})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	results, err := exec.BatchExecute(context.Background(), accID, store.ActionMarkRead, ids)
	require.NoError(t, err)
	require.Len(t, results, 25)
	require.Equal(t, int32(2), calls.Load(), "25 actions → 2 $batch calls")
	// Chunks run concurrently — sort before comparing.
	sort.Sort(sort.Reverse(sort.IntSlice(sizes)))
	require.Equal(t, []int{20, 5}, sizes)
}

// TestBatchExecuteSoftDeleteUsesAlias confirms the dispatched URL is
// /me/messages/{id}/move and the body uses the "deleteditems" alias
// (not the resolved real folder ID).
func TestBatchExecuteSoftDeleteUsesAlias(t *testing.T) {
	exec, _, accID, srv := newTestExec(t)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Requests []struct {
				ID     string            `json:"id"`
				Method string            `json:"method"`
				URL    string            `json:"url"`
				Body   map[string]string `json:"body"`
			} `json:"requests"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Len(t, payload.Requests, 1)
		require.Equal(t, "POST", payload.Requests[0].Method)
		require.Equal(t, "/me/messages/m-1/move", payload.Requests[0].URL)
		require.Equal(t, "deleteditems", payload.Requests[0].Body["destinationId"])

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"responses": []map[string]any{{"id": "0", "status": 200}},
		})
	})

	results, err := exec.BatchExecute(context.Background(), accID, store.ActionSoftDelete, []string{"m-1"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
}

// itoa keeps the test self-contained without importing strconv for a
// single conversion.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
