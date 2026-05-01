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

// TestGetAttachmentDecodesBase64 exercises the happy path for
// GetAttachment: Graph returns base64 contentBytes in JSON; we
// decode and return raw bytes. Spec 05 §8.1 / PR 10.
func TestGetAttachmentDecodesBase64(t *testing.T) {
	wantData := []byte("hello attachment bytes")
	encoded := base64.StdEncoding.EncodeToString(wantData)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.URL.Path, "/me/messages/m-1/attachments/a-1")
		require.Contains(t, r.URL.RawQuery, "contentBytes")
		w.Header().Set("Content-Type", "application/json")
		payload := map[string]string{"contentBytes": encoded}
		require.NoError(t, json.NewEncoder(w).Encode(payload))
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	got, err := c.GetAttachment(context.Background(), "m-1", "a-1")
	require.NoError(t, err)
	require.Equal(t, wantData, got)
}

// TestGetAttachmentSurfaces404 verifies that a non-200 Graph response
// is returned as a *GraphError (spec 05 §8.1 / PR 10).
func TestGetAttachmentSurfaces404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"ErrorItemNotFound","message":"not found"}}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	_, err = c.GetAttachment(context.Background(), "m-1", "a-missing")
	require.Error(t, err)
	require.True(t, IsNotFound(err), "expected IsNotFound error, got %T: %v", err, err)
}

// TestGetAttachmentRejectsInvalidBase64 ensures a malformed contentBytes
// field propagates a decode error rather than returning nil bytes.
func TestGetAttachmentRejectsInvalidBase64(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"contentBytes":"not-valid-base64!!!"}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	_, err = c.GetAttachment(context.Background(), "m-1", "a-bad")
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode attachment bytes")
}
