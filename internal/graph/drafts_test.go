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

// TestPatchMessageBody_HTMLContentType verifies that spec 33's
// new contentType parameter flows into the JSON payload's body
// object, replacing the pre-spec-33 hard-coded "text".
func TestPatchMessageBody_HTMLContentType(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	require.NoError(t, c.PatchMessageBody(context.Background(),
		"draft-1", "<p>hi</p>\n", "html",
		[]string{"alice@example.invalid"}, nil, nil, "Re: subj"))

	require.Equal(t, http.MethodPatch, gotMethod)
	require.Equal(t, "/me/messages/draft-1", gotPath)
	bodyObj := gotBody["body"].(map[string]any)
	require.Equal(t, "html", bodyObj["contentType"])
	require.Equal(t, "<p>hi</p>\n", bodyObj["content"])
	require.Equal(t, "Re: subj", gotBody["subject"])
}

// TestPatchMessageBody_TextContentType confirms the pre-spec-33
// "text" path still works (default plain mode).
func TestPatchMessageBody_TextContentType(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	require.NoError(t, c.PatchMessageBody(context.Background(),
		"draft-2", "plain body", "text", nil, nil, nil, ""))

	bodyObj := gotBody["body"].(map[string]any)
	require.Equal(t, "text", bodyObj["contentType"])
	require.Equal(t, "plain body", bodyObj["content"])
}

// TestCreateNewDraft_HTMLContentType verifies spec 33's new
// contentType parameter flows through CreateNewDraft.
func TestCreateNewDraft_HTMLContentType(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"new-1","webLink":"https://outlook.invalid/new-1"}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	ref, err := c.CreateNewDraft(context.Background(),
		"Subj", "<p>hi</p>\n", "html",
		[]string{"a@example.invalid"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "new-1", ref.ID)

	bodyObj := gotBody["body"].(map[string]any)
	require.Equal(t, "html", bodyObj["contentType"])
	require.Equal(t, "<p>hi</p>\n", bodyObj["content"])
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
