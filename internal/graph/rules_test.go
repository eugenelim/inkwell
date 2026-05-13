package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGraphListMessageRulesHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/me/mailFolders/inbox/messageRules", r.URL.Path)
		_, _ = io.WriteString(w, `{
			"value": [
				{
					"id": "AQAAAJ5dZqA=",
					"displayName": "Newsletters",
					"sequence": 10,
					"isEnabled": true,
					"isReadOnly": false,
					"hasError": false,
					"conditions": { "senderContains": ["newsletter@"] },
					"actions": { "moveToFolder": "folder-feed", "markAsRead": true }
				},
				{
					"id": "BQBB=",
					"displayName": "VM rule",
					"sequence": 20,
					"isEnabled": false,
					"conditions": { "isVoicemail": true }
				}
			]
		}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	out, err := c.ListMessageRules(context.Background())
	require.NoError(t, err)
	require.Len(t, out, 2)

	require.Equal(t, "AQAAAJ5dZqA=", out[0].Rule.ID)
	require.Equal(t, "Newsletters", out[0].Rule.DisplayName)
	require.Equal(t, 10, out[0].Rule.Sequence)
	require.True(t, out[0].Rule.IsEnabled)
	require.NotNil(t, out[0].Rule.Conditions)
	require.Equal(t, []string{"newsletter@"}, out[0].Rule.Conditions.SenderContains)
	require.NotNil(t, out[0].Rule.Actions)
	require.Equal(t, "folder-feed", out[0].Rule.Actions.MoveToFolder)

	// Non-v1 field (isVoicemail) survives in raw conditions.
	require.Contains(t, string(out[1].RawConditions), "isVoicemail")
	require.False(t, out[1].Rule.IsEnabled)
}

func TestGraphCreateMessageRule201(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/me/mailFolders/inbox/messageRules", r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		// Server assigns the ID; even if the client sends one we ignore it.
		require.NotContains(t, string(b), `"id":`)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{
			"id": "server-assigned-id",
			"displayName": "X",
			"sequence": 5,
			"isEnabled": true,
			"conditions": { "subjectContains": ["urgent"] },
			"actions": { "markAsRead": true }
		}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	rule := MessageRule{
		ID:          "client-tried-to-set-this", // should be stripped
		DisplayName: "X",
		Sequence:    5,
		IsEnabled:   true,
		Conditions:  &MessageRulePredicates{SubjectContains: []string{"urgent"}},
		Actions:     &MessageRuleActions{MarkAsRead: ptrBool(true)},
	}
	got, err := c.CreateMessageRule(context.Background(), rule)
	require.NoError(t, err)
	require.Equal(t, "server-assigned-id", got.Rule.ID)
}

func TestGraphUpdateMessageRule404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"ErrorItemNotFound","message":"not found"}}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	_, err = c.UpdateMessageRule(context.Background(), "missing", json.RawMessage(`{"displayName":"X"}`))
	require.Error(t, err)
	var ge *GraphError
	require.ErrorAs(t, err, &ge)
	require.Equal(t, http.StatusNotFound, ge.StatusCode)
}

func TestGraphDeleteMessageRule404IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	require.NoError(t, c.DeleteMessageRule(context.Background(), "any"))
}

func TestGraphRulesRetryAfter429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"value":[]}`)
	}))
	defer srv.Close()

	logger, _ := newCapturedLogger()
	c, err := NewClient(&fakeAuth{}, Options{BaseURL: srv.URL, Logger: logger})
	require.NoError(t, err)

	out, err := c.ListMessageRules(context.Background())
	require.NoError(t, err)
	require.Empty(t, out)
	require.GreaterOrEqual(t, atomic.LoadInt32(&calls), int32(2))
}

func TestMergeRuleSubObjectPreservesNonV1Keys(t *testing.T) {
	prior := json.RawMessage(`{"isVoicemail":true,"senderContains":["old@"]}`)
	edit := json.RawMessage(`{"senderContains":["new@"],"subjectContains":["urgent"]}`)
	merged, err := MergeRuleSubObject(prior, edit)
	require.NoError(t, err)
	require.Contains(t, string(merged), `"isVoicemail":true`)
	require.Contains(t, string(merged), `"senderContains":["new@"]`)
	require.Contains(t, string(merged), `"subjectContains":["urgent"]`)
	require.NotContains(t, string(merged), `"old@"`)
}

func TestMergeRuleSubObjectReplacesArrays(t *testing.T) {
	prior := json.RawMessage(`{"bodyContains":["a","b"]}`)
	edit := json.RawMessage(`{"bodyContains":["c"]}`)
	merged, err := MergeRuleSubObject(prior, edit)
	require.NoError(t, err)
	require.Contains(t, string(merged), `["c"]`)
	require.NotContains(t, string(merged), `"a"`)
	require.NotContains(t, string(merged), `"b"`)
}

func TestMergeRuleSubObjectEmptyEdit(t *testing.T) {
	prior := json.RawMessage(`{"isVoicemail":true}`)
	merged, err := MergeRuleSubObject(prior, nil)
	require.NoError(t, err)
	require.Contains(t, string(merged), `"isVoicemail":true`)
}

func TestMergeRuleSubObjectEmptyPrior(t *testing.T) {
	edit := json.RawMessage(`{"subjectContains":["x"]}`)
	merged, err := MergeRuleSubObject(nil, edit)
	require.NoError(t, err)
	require.Contains(t, string(merged), `"subjectContains":["x"]`)
}

func TestMergeRuleSubObjectBothEmpty(t *testing.T) {
	merged, err := MergeRuleSubObject(nil, nil)
	require.NoError(t, err)
	require.Equal(t, "{}", string(merged))
}

func TestMergeRuleSubObjectRoundTripsThroughMapAny(t *testing.T) {
	prior := json.RawMessage(`{"isVoicemail":true}`)
	edit := json.RawMessage(`{"senderContains":["x"]}`)
	merged, err := MergeRuleSubObject(prior, edit)
	require.NoError(t, err)

	// Now stuff it into a map[string]any and re-marshal — verifies the
	// bytes are emitted as a JSON object (not a JSON-string).
	body := map[string]any{
		"conditions":  merged,
		"displayName": "Y",
	}
	out, err := json.Marshal(body)
	require.NoError(t, err)
	require.Contains(t, string(out), `"conditions":{`)
	require.NotContains(t, string(out), `"conditions":"{`)
}

func TestCanonicalJSONStableAcrossUnmarshalCycle(t *testing.T) {
	original := []byte(`{"b":2,"a":1,"nested":{"y":[1,2],"x":"v"}}`)
	var v any
	require.NoError(t, json.Unmarshal(original, &v))

	first, err := CanonicalJSON(v)
	require.NoError(t, err)

	// Round-trip: re-decode the canonical form and re-encode.
	var second any
	require.NoError(t, json.Unmarshal(first, &second))
	again, err := CanonicalJSON(second)
	require.NoError(t, err)

	require.Equal(t, string(first), string(again))
	// Confirms keys are sorted.
	require.True(t, strings.Index(string(first), `"a"`) < strings.Index(string(first), `"b"`))
	require.True(t, strings.Index(string(first), `"nested"`) > strings.Index(string(first), `"b"`))
	require.True(t, strings.Index(string(first), `"x"`) < strings.Index(string(first), `"y"`))
}

func TestContentHashIsStable(t *testing.T) {
	h1, err := ContentHash(map[string]any{"b": 2, "a": 1})
	require.NoError(t, err)
	h2, err := ContentHash(map[string]any{"a": 1, "b": 2})
	require.NoError(t, err)
	require.Equal(t, h1, h2)
}

func ptrBool(b bool) *bool { return &b }
