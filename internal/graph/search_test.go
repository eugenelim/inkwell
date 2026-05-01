package graph

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSearchMessagesEncodesQuotedQueryString covers the spec 06
// §4.2 encoding rule: $search values must be URL-encoded AND
// wrapped in literal double quotes (Graph treats unquoted values
// inconsistently; the test pins the encoded form `%22q4%20review%22`).
func TestSearchMessagesEncodesQuotedQueryString(t *testing.T) {
	var seen *url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL
		_, _ = io.WriteString(w, `{"value":[]}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	_, err = c.SearchMessages(context.Background(), SearchMessagesOpts{
		Query: "q4 review",
		Top:   25,
	})
	require.NoError(t, err)
	require.NotNil(t, seen)
	require.Equal(t, "/me/messages", seen.Path)

	got := seen.RawQuery
	// $search must carry the URL-encoded quoted value. %22 = "
	// and %20 = space — Graph accepts the spec form
	// ?$search=%22q4%20review%22.
	require.Contains(t, got, `$search=%22q4+review%22`,
		"raw $search param: got=%q", got)
	require.Contains(t, got, "%24top=25",
		"%24top encodes the $ in $top; got=%q", got)
}

// TestSearchMessagesScopesToFolder confirms FolderID switches the
// path from /me/messages to /me/mailFolders/{id}/messages.
func TestSearchMessagesScopesToFolder(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_, _ = io.WriteString(w, `{"value":[]}`)
	}))
	defer srv.Close()
	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)
	_, err = c.SearchMessages(context.Background(), SearchMessagesOpts{
		Query: "x", FolderID: "AAMk-folder-id",
	})
	require.NoError(t, err)
	require.Equal(t, "/me/mailFolders/AAMk-folder-id/messages", seenPath)
}

// TestSearchMessagesRejectsEmptyQuery is a defensive check —
// Graph would 400 on $search="" but it's cheaper to fail at the
// helper boundary than burn an HTTP round-trip.
func TestSearchMessagesRejectsEmptyQuery(t *testing.T) {
	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: "https://unused.invalid", Logger: logger})
	require.NoError(t, err)
	_, err = c.SearchMessages(context.Background(), SearchMessagesOpts{Query: ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-empty query")
}
