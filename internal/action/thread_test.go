package action

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// seedThreadMessage adds a message with a ConversationID to the store.
func seedThreadMessage(t *testing.T, st store.Store, accID int64, msgID, convID string) {
	t.Helper()
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ID:             msgID,
		AccountID:      accID,
		FolderID:       "f-inbox",
		Subject:        "Thread subject",
		ConversationID: convID,
		IsRead:         false,
		FlagStatus:     "notFlagged",
		ReceivedAt:     time.Now(),
	}))
}

func TestThreadExecuteMarkRead(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	// Seed two messages in the same conversation.
	seedThreadMessage(t, st, accID, "t-1", "conv-thread")
	seedThreadMessage(t, st, accID, "t-2", "conv-thread")
	// Second message needs its own ID — update m-1 to be our thread message.
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/t-1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/t-2", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Also need to wire the batch endpoint for the $batch call.
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return success for all sub-requests.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"responses":[{"id":"0","status":200},{"id":"1","status":200}]}`))
	})

	total, results, err := exec.ThreadExecute(context.Background(), accID, store.ActionMarkRead, "t-1")
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, results, 2)
}

func TestThreadExecuteRejectsMove(t *testing.T) {
	exec, _, accID, _ := newTestExec(t)
	total, results, err := exec.ThreadExecute(context.Background(), accID, store.ActionMove, "m-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "use ThreadMove")
	require.Zero(t, total)
	require.Nil(t, results)
}

func TestThreadMoveCallsBulkMove(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	seedThreadMessage(t, st, accID, "tm-1", "conv-move")

	srv.Config.Handler.(*http.ServeMux).HandleFunc("/$batch", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"responses":[{"id":"0","status":200}]}`))
	})

	total, results, err := exec.ThreadMove(context.Background(), accID, "tm-1", "", "archive")
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, results, 1)
}

func TestThreadExecuteNoConvID(t *testing.T) {
	exec, _, accID, _ := newTestExec(t)
	// m-1 was seeded by newTestExec with an empty ConversationID.
	total, results, err := exec.ThreadExecute(context.Background(), accID, store.ActionMarkRead, "m-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no conversation id")
	require.Zero(t, total)
	require.Nil(t, results)
}
