package graph

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDeleteDraftSuccess verifies the DELETE /me/messages/{id} path
// returns nil on a 204 response.
func TestDeleteDraftSuccess(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	require.NoError(t, c.DeleteDraft(context.Background(), "draft-123"))
	require.Equal(t, http.MethodDelete, method)
	require.Equal(t, "/me/messages/draft-123", path)
}

// TestDeleteDraftNotFoundIsSuccess verifies 404 is treated as success
// (idempotent: draft already gone).
func TestDeleteDraftNotFoundIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"ErrorItemNotFound"}}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	require.NoError(t, c.DeleteDraft(context.Background(), "gone-draft"))
}

// TestDeleteDraftServerErrorSurfaces verifies a 500 returns an error.
func TestDeleteDraftServerErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"InternalServerError"}}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	require.Error(t, c.DeleteDraft(context.Background(), "draft-err"))
}

// TestAddDraftAttachmentGraphCall verifies that POST
// /me/messages/{id}/attachments sends the correct @odata.type and
// base64-encoded contentBytes.
func TestAddDraftAttachmentGraphCall(t *testing.T) {
	var gotBody map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"att-1"}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	data := []byte("hello attachment")
	require.NoError(t, c.AddDraftAttachment(context.Background(), "draft-abc", "hello.txt", data))

	require.Equal(t, "/me/messages/draft-abc/attachments", gotPath)
	require.Equal(t, "#microsoft.graph.fileAttachment", gotBody["@odata.type"])
	require.Equal(t, "hello.txt", gotBody["name"])
	require.Equal(t, base64.StdEncoding.EncodeToString(data), gotBody["contentBytes"])
}
