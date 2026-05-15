package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ilog "github.com/eugenelim/inkwell/internal/log"
)

// TestEnvelopeSelectFieldsExcludesMeetingMessageType is the
// inverse-regression for the v0.15.1 hotfix: Graph rejects
// `meetingMessageType` in `$select` on
// `/me/mailFolders/{id}/messages` because the property exists only
// on the `microsoft.graph.eventMessage` derived type. Including it
// returned 400 RequestBroker--ParseUri on real tenants and broke
// every Backfill call. The 📅 indicator falls back to the
// subject-prefix heuristic until a future release uses the cast
// form `microsoft.graph.eventMessage/meetingMessageType`.
func TestEnvelopeSelectFieldsExcludesMeetingMessageType(t *testing.T) {
	require.NotContains(t, EnvelopeSelectFields, "meetingMessageType",
		"$select must NOT include meetingMessageType — Graph rejects it on the polymorphic Message endpoint")
}

// TestMessageDeserializesMeetingMessageType ensures the JSON tag is
// correct and the field actually populates from a Graph response.
func TestMessageDeserializesMeetingMessageType(t *testing.T) {
	body := `{"id":"m-1","subject":"Q4 sync","meetingMessageType":"meetingRequest"}`
	var got Message
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	require.Equal(t, "meetingRequest", got.MeetingMessageType)
}

// TestGetMessageHeadersExtractsListUnsubscribe is the spec 16 plumbing
// test. The Graph endpoint returns a list under
// `internetMessageHeaders`; HeaderValue must find List-Unsubscribe
// case-insensitively and return its raw value for unsub.Parse.
func TestGetMessageHeadersExtractsListUnsubscribe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.URL.RawQuery, "select=internetMessageHeaders")
		_, _ = io.WriteString(w, `{
			"internetMessageHeaders": [
				{"name": "Date", "value": "Mon, 1 Jan 2026 00:00:00 GMT"},
				{"name": "List-Unsubscribe", "value": "<https://example.invalid/u?id=abc>"},
				{"name": "List-Unsubscribe-Post", "value": "List-Unsubscribe=One-Click"}
			]
		}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	headers, err := c.GetMessageHeaders(context.Background(), "AAMk-test-id")
	require.NoError(t, err)
	require.Len(t, headers, 3)

	// HeaderValue is case-insensitive and matches the canonical RFC
	// header name regardless of how the sender capitalises it.
	require.Equal(t, "<https://example.invalid/u?id=abc>", HeaderValue(headers, "List-Unsubscribe"))
	require.Equal(t, "<https://example.invalid/u?id=abc>", HeaderValue(headers, "list-unsubscribe"))
	require.Equal(t, "List-Unsubscribe=One-Click", HeaderValue(headers, "list-unsubscribe-post"))
	require.Empty(t, HeaderValue(headers, "X-Not-Present"))
}

// fakeAuth is a counting [Authenticator] for tests.
type fakeAuth struct {
	mu          sync.Mutex
	tokens      []string // tokens to hand out in order; falls back to last
	tokenCalls  atomic.Int32
	invalidates atomic.Int32
}

func (f *fakeAuth) Token(_ context.Context) (string, error) {
	f.tokenCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tokens) == 0 {
		return "tok-default", nil
	}
	if len(f.tokens) == 1 {
		return f.tokens[0], nil
	}
	t := f.tokens[0]
	f.tokens = f.tokens[1:]
	return t, nil
}

func (f *fakeAuth) Invalidate() { f.invalidates.Add(1) }

func newCapturedLogger() (*slog.Logger, *ilog.Captured) {
	return ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "owner@example.invalid"})
}

func TestClientInjectsBearerHeader(t *testing.T) {
	logger, captured := newCapturedLogger()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer tok-default", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c, err := NewClient(&fakeAuth{}, Options{
		BaseURL: srv.URL,
		Logger:  logger,
	})
	require.NoError(t, err)
	resp, err := c.Do(context.Background(), http.MethodGet, "/me", nil, nil)
	require.NoError(t, err)
	resp.Body.Close()

	// Logging transport must redact the bearer token in any captured slog output.
	require.NoError(t, captured.AssertNoSecret("tok-default"))
}

func TestClientRetriesOn401AfterInvalidate(t *testing.T) {
	logger, _ := newCapturedLogger()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()

	auth := &fakeAuth{tokens: []string{"old", "new"}}
	c, err := NewClient(auth, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)
	resp, err := c.Do(context.Background(), http.MethodGet, "/me", nil, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
	require.Equal(t, int32(1), auth.invalidates.Load(), "401 must trigger Invalidate")
	require.Equal(t, int32(2), calls.Load(), "exactly two calls: 401 then 200")
}

func TestClientSurfaces401WhenSecondAttemptAlsoFails(t *testing.T) {
	logger, _ := newCapturedLogger()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)
	resp, err := c.Do(context.Background(), http.MethodGet, "/me", nil, nil)
	require.NoError(t, err) // status surfaces, not error
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp.Body.Close()
}

func TestClientHonoursRetryAfterOn429(t *testing.T) {
	logger, _ := newCapturedLogger()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		c := calls.Add(1)
		if c == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	var throttled int32
	c, err := NewClient(&fakeAuth{}, Options{
		BaseURL: srv.URL,
		Logger:  logger,
		OnThrottle: func(d time.Duration) {
			atomic.AddInt32(&throttled, 1)
			require.GreaterOrEqual(t, d, time.Second)
		},
	})
	require.NoError(t, err)

	start := time.Now()
	resp, err := c.Do(context.Background(), http.MethodGet, "/me", nil, nil)
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
	require.GreaterOrEqual(t, elapsed, 900*time.Millisecond, "must wait Retry-After before retry")
	require.Equal(t, int32(1), atomic.LoadInt32(&throttled))
}

func TestClientCapsConcurrency(t *testing.T) {
	logger, _ := newCapturedLogger()
	var observedMax int32
	var inFlight atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cur := inFlight.Add(1)
		for {
			old := atomic.LoadInt32(&observedMax)
			if cur <= old || atomic.CompareAndSwapInt32(&observedMax, old, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		inFlight.Add(-1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	const cap = 3
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger, MaxConcurrent: cap})
	require.NoError(t, err)

	const N = 12
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := c.Do(context.Background(), http.MethodGet, "/me", nil, nil)
			require.NoError(t, err)
			resp.Body.Close()
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, atomic.LoadInt32(&observedMax), int32(cap),
		"concurrent in-flight requests exceeded the configured cap")
}

func TestClientCancelsViaContext(t *testing.T) {
	logger, _ := newCapturedLogger()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = c.Do(ctx, http.MethodGet, "/me", nil, nil)
	require.Error(t, err)
}

func TestParseRetryAfterAcceptsSecondsAndHTTPDate(t *testing.T) {
	require.Equal(t, 2*time.Second, parseRetryAfter("2"))
	require.Zero(t, parseRetryAfter(""))
	require.Zero(t, parseRetryAfter("abc"))
	// Past HTTP-date: returns 0.
	require.Zero(t, parseRetryAfter("Mon, 02 Jan 2006 15:04:05 GMT"))
	future := time.Now().UTC().Add(3 * time.Second).Format(http.TimeFormat)
	got := parseRetryAfter(future)
	require.Greater(t, got, time.Second)
	require.Less(t, got, 5*time.Second)
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		body   string
		status int
		want   func(error) bool
	}{
		{`{"error":{"code":"syncStateNotFound","message":"x"}}`, http.StatusGone, IsSyncStateNotFound},
		{`{"error":{"code":"InvalidAuthenticationToken"}}`, http.StatusUnauthorized, IsAuth},
		{`{"error":{"code":"ApplicationThrottled"}}`, http.StatusTooManyRequests, IsThrottled},
		{`{}`, http.StatusNotFound, IsNotFound},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.body), func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tc.status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}
			err := parseError(resp)
			require.True(t, tc.want(err), "classification miss for %T %v", err, err)
			var ge *GraphError
			require.True(t, errors.As(err, &ge))
		})
	}
}

// TestCreateFolderTopLevel covers the spec 18 §4 happy path:
// POST /me/mailFolders with the displayName body.
func TestCreateFolderTopLevel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/me/mailFolders", r.URL.Path)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "Vendors", body["displayName"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"f-new","displayName":"Vendors","parentFolderId":""}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)
	got, err := c.CreateFolder(context.Background(), "", "Vendors")
	require.NoError(t, err)
	require.Equal(t, "f-new", got.ID)
	require.Equal(t, "Vendors", got.DisplayName)
}

// TestCreateFolderNested verifies the parentID branch hits the
// childFolders sub-path.
func TestCreateFolderNested(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/me/mailFolders/f-parent/childFolders", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"f-child","displayName":"2026","parentFolderId":"f-parent"}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)
	got, err := c.CreateFolder(context.Background(), "f-parent", "2026")
	require.NoError(t, err)
	require.Equal(t, "f-parent", got.ParentFolderID)
}

// TestRenameFolderPATCHesDisplayName covers the rename path.
func TestRenameFolderPATCHesDisplayName(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		require.Equal(t, "/me/mailFolders/f-1", r.URL.Path)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		seen = body["displayName"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)
	require.NoError(t, c.RenameFolder(context.Background(), "f-1", "Renamed"))
	require.Equal(t, "Renamed", seen)
}

// TestRenameFolderRejectedOnWellKnown is the spec 18 §7 invariant:
// Graph 403s rename of system folders; we surface unchanged.
func TestRenameFolderRejectedOnWellKnown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"ErrorAccessDenied","message":"cannot rename system folder"}}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)
	err = c.RenameFolder(context.Background(), "inbox", "NotAllowed")
	require.Error(t, err)
}

// TestDeleteFolderTreatsNotFoundAsSuccess covers the `docs/CONVENTIONS.md` §3
// idempotency invariant: 404 on delete is success (folder already
// gone matches the user's intent).
func TestDeleteFolderTreatsNotFoundAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)
	require.NoError(t, c.DeleteFolder(context.Background(), "f-vanished"))
}

// TestDeleteFolder204Success verifies the canonical success path.
func TestDeleteFolder204Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, "/me/mailFolders/f-1", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)
	require.NoError(t, c.DeleteFolder(context.Background(), "f-1"))
}
