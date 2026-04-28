package graph

import (
	"context"
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

	ilog "github.com/eu-gene-lim/inkwell/internal/log"
)

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
