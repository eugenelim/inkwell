package action

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/config"
	"github.com/eugenelim/inkwell/internal/store"
)

// TestBatchHardCapRejectsOversizedBulk asserts that batchExecute returns an
// error (not a partial result) when the message count exceeds HardMax.
func TestBatchHardCapRejectsOversizedBulk(t *testing.T) {
	exec, st, accID, _ := newTestExec(t)
	exec.SetBatchConfig(config.BatchConfig{
		HardMax:                 3,
		Concurrency:             1,
		MaxRetriesPerSubrequest: 0,
	})
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("m-%d", i)
		require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
			ID: id, AccountID: accID, FolderID: "f-inbox", Subject: id,
			FromAddress: "x@example.invalid",
		}))
	}
	ids := []string{"m-1", "m-2", "m-3", "m-4", "m-5"}
	_, err := exec.BatchExecute(context.Background(), accID, store.ActionMarkRead, ids)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds hard max")
}

// TestBatchPerSubrequestRetryOn429 confirms that a sub-request returning 429
// is retried up to MaxRetriesPerSubrequest times and, on success, the result
// is surfaced correctly.
func TestBatchPerSubrequestRetryOn429(t *testing.T) {
	exec, _, accID, srv := newTestExec(t)
	exec.SetBatchConfig(config.BatchConfig{
		HardMax:                 5000,
		Concurrency:             1,
		MaxRetriesPerSubrequest: 2,
	})

	var calls atomic.Int32
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		var payload struct {
			Requests []struct {
				ID string `json:"id"`
			} `json:"requests"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// First call: return 429 for the sub-request.
			responses := make([]map[string]any, len(payload.Requests))
			for i, req := range payload.Requests {
				responses[i] = map[string]any{
					"id":     req.ID,
					"status": 429,
					"headers": map[string]string{
						"Retry-After": "0",
					},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"responses": responses})
			return
		}
		// Second call: success.
		responses := make([]map[string]any, len(payload.Requests))
		for i, req := range payload.Requests {
			responses[i] = map[string]any{"id": req.ID, "status": 200}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"responses": responses})
	})

	results, err := exec.BatchExecute(context.Background(), accID, store.ActionMarkRead, []string{"m-1"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err, "retry should have succeeded on second attempt")
	require.Equal(t, int32(2), calls.Load())
}

// TestBatchPerSubrequestRetryExhausted confirms that a sub-request stuck at
// 429 for every attempt surfaces as an error in the result.
func TestBatchPerSubrequestRetryExhausted(t *testing.T) {
	exec, _, accID, srv := newTestExec(t)
	exec.SetBatchConfig(config.BatchConfig{
		HardMax:                 5000,
		Concurrency:             1,
		MaxRetriesPerSubrequest: 1,
	})

	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Requests []struct {
				ID string `json:"id"`
			} `json:"requests"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		responses := make([]map[string]any, len(payload.Requests))
		for i, req := range payload.Requests {
			responses[i] = map[string]any{
				"id":     req.ID,
				"status": 429,
				"headers": map[string]string{
					"Retry-After": "0",
				},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"responses": responses})
	})

	results, err := exec.BatchExecute(context.Background(), accID, store.ActionMarkRead, []string{"m-1"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Error(t, results[0].Err, "exhausted retries must surface as error")
}

// TestBatchPermanentDeleteSubrequest confirms the correct Graph endpoint and
// method are used for permanent_delete in a $batch call.
func TestBatchPermanentDeleteSubrequest(t *testing.T) {
	exec, _, accID, srv := newTestExec(t)

	var gotMethod, gotURL string
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Requests []struct {
				ID     string `json:"id"`
				Method string `json:"method"`
				URL    string `json:"url"`
			} `json:"requests"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Len(t, payload.Requests, 1)
		gotMethod = payload.Requests[0].Method
		gotURL = payload.Requests[0].URL
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"responses": []map[string]any{{"id": "0", "status": 200}},
		})
	})

	results, err := exec.BulkPermanentDelete(context.Background(), accID, []string{"m-1"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	require.Equal(t, "POST", gotMethod)
	require.Equal(t, "/me/messages/m-1/permanentDelete", gotURL)
}

// TestBatchAddCategorySubrequest confirms the $batch PATCH body carries the
// full post-apply categories list (not just the added category).
func TestBatchAddCategorySubrequest(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)

	// Seed m-1 with an existing category.
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID: "m-1", AccountID: accID, FolderID: "f-inbox", Subject: "x",
		FromAddress: "a@example.invalid",
		Categories:  []string{"existing"},
	}))

	var gotCategories []string
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Requests []struct {
				ID   string         `json:"id"`
				Body map[string]any `json:"body"`
			} `json:"requests"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Len(t, payload.Requests, 1)
		if cats, ok := payload.Requests[0].Body["categories"].([]any); ok {
			for _, c := range cats {
				gotCategories = append(gotCategories, c.(string))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"responses": []map[string]any{{"id": "0", "status": 200}},
		})
	})

	results, err := exec.BulkAddCategory(context.Background(), accID, []string{"m-1"}, "new-cat")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	// PATCH body must contain both the existing category and the newly added one.
	require.ElementsMatch(t, []string{"existing", "new-cat"}, gotCategories)
}

// TestBatchCompositeUndo confirms that a successful bulk mark_read pushes a
// single UndoEntry covering all succeeded message IDs.
func TestBatchCompositeUndo(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	for _, id := range []string{"m-2", "m-3"} {
		require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
			ID: id, AccountID: accID, FolderID: "f-inbox", Subject: id,
			FromAddress: "x@example.invalid", IsRead: false,
		}))
	}

	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
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
	})

	results, err := exec.BulkMarkRead(context.Background(), accID, []string{"m-1", "m-2", "m-3"})
	require.NoError(t, err)
	require.Len(t, results, 3)

	entry, err := st.PeekUndo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, entry, "a composite undo entry must be pushed")
	require.Equal(t, store.ActionMarkUnread, entry.ActionType)
	require.ElementsMatch(t, []string{"m-1", "m-2", "m-3"}, entry.MessageIDs)
}

// TestBulkUndoRoundTrip confirms that a bulk mark_read can be undone: after
// BulkMarkRead, all messages are read=true; after Undo, all are read=false.
func TestBulkUndoRoundTrip(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	for _, id := range []string{"m-2", "m-3"} {
		require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
			ID: id, AccountID: accID, FolderID: "f-inbox", Subject: id,
			FromAddress: "x@example.invalid", IsRead: false,
		}))
	}

	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
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
	})

	// Apply.
	_, err := exec.BulkMarkRead(context.Background(), accID, []string{"m-1", "m-2", "m-3"})
	require.NoError(t, err)
	for _, id := range []string{"m-1", "m-2", "m-3"} {
		m, _ := st.GetMessage(context.Background(), id)
		require.True(t, m.IsRead, "%s should be read after BulkMarkRead", id)
	}

	// Undo.
	undoEntry, err := exec.Undo(context.Background(), accID)
	require.NoError(t, err)
	require.Equal(t, store.ActionMarkUnread, undoEntry.ActionType)
	require.Len(t, undoEntry.MessageIDs, 3)
	for _, id := range []string{"m-1", "m-2", "m-3"} {
		m, _ := st.GetMessage(context.Background(), id)
		require.False(t, m.IsRead, "%s should be unread after Undo", id)
	}
}

// TestBatchConcurrentChunks verifies that ExecuteAll runs multiple chunks in
// parallel by confirming two simultaneous $batch requests when concurrency ≥ 2.
func TestBatchConcurrentChunks(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	exec.SetBatchConfig(config.BatchConfig{
		HardMax:                 5000,
		Concurrency:             2,
		MaxRetriesPerSubrequest: 0,
	})
	// Seed 25 messages (2 chunks).
	for i := 2; i <= 25; i++ {
		id := fmt.Sprintf("m-%d", i)
		require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
			ID: id, AccountID: accID, FolderID: "f-inbox", Subject: id,
			FromAddress: "x@example.invalid",
		}))
	}

	var inflight atomic.Int32
	var maxInflight atomic.Int32
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, r *http.Request) {
		cur := inflight.Add(1)
		defer inflight.Add(-1)
		// Record peak concurrency.
		for {
			old := maxInflight.Load()
			if cur <= old || maxInflight.CompareAndSwap(old, cur) {
				break
			}
		}
		// Small delay so both goroutines can be observed in flight.
		time.Sleep(10 * time.Millisecond)
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
	})

	ids := make([]string, 25)
	for i := range ids {
		ids[i] = fmt.Sprintf("m-%d", i+1)
	}
	results, err := exec.BatchExecute(context.Background(), accID, store.ActionMarkRead, ids)
	require.NoError(t, err)
	require.Len(t, results, 25)
	require.GreaterOrEqual(t, maxInflight.Load(), int32(2), "should have observed at least 2 concurrent $batch calls")
}
