package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GraphError is the typed error returned by the client when Graph
// reports a non-2xx response. It mirrors the shape of Graph's
// standard error envelope.
type GraphError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
	RetryAfter string
}

// Error implements [error].
func (e *GraphError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("graph: %s: %s (status %d)", e.Code, e.Message, e.StatusCode)
	}
	return fmt.Sprintf("graph: status %d", e.StatusCode)
}

// IsThrottled reports whether err is a 429 / 503 throttle response.
func IsThrottled(err error) bool {
	var ge *GraphError
	if !errors.As(err, &ge) {
		return false
	}
	return ge.StatusCode == http.StatusTooManyRequests || ge.StatusCode == http.StatusServiceUnavailable
}

// IsAuth reports whether err is a 401 (token revoked or expired).
func IsAuth(err error) bool {
	var ge *GraphError
	if !errors.As(err, &ge) {
		return false
	}
	return ge.StatusCode == http.StatusUnauthorized
}

// IsSyncStateNotFound reports whether err indicates the persisted delta
// token has aged out (HTTP 410 with code "syncStateNotFound").
func IsSyncStateNotFound(err error) bool {
	var ge *GraphError
	if !errors.As(err, &ge) {
		return false
	}
	if ge.StatusCode != http.StatusGone {
		return false
	}
	c := strings.ToLower(ge.Code)
	return c == "syncstatenotfound"
}

// IsNotFound reports whether err is a 404. Caller treats it as success
// for idempotent deletes.
func IsNotFound(err error) bool {
	var ge *GraphError
	if !errors.As(err, &ge) {
		return false
	}
	return ge.StatusCode == http.StatusNotFound
}

// parseError reads body and constructs a GraphError. body may be empty.
func parseError(resp *http.Response) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	ge := &GraphError{
		StatusCode: resp.StatusCode,
		RequestID:  resp.Header.Get("request-id"),
		RetryAfter: resp.Header.Get("Retry-After"),
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err == nil {
		ge.Code = env.Error.Code
		ge.Message = env.Error.Message
	}
	return ge
}
