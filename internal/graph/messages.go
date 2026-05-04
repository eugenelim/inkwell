package graph

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ListMessagesOpts narrows /me/mailFolders/{id}/messages results.
type ListMessagesOpts struct {
	Select        string
	Filter        string
	Top           int
	OrderBy       string
	IncludeBodies bool // when true, omit $select restriction
	ReceivedSince time.Time
}

// ListMessagesResponse is the paginated wrapper.
type ListMessagesResponse struct {
	Value    []Message `json:"value"`
	NextLink string    `json:"@odata.nextLink,omitempty"`
}

// ListMessagesInFolder paginates through a folder's messages. The
// initial backfill (spec §5) consumes the result.
func (c *Client) ListMessagesInFolder(ctx context.Context, folderID string, opts ListMessagesOpts) (*ListMessagesResponse, error) {
	path := "/me/mailFolders/" + folderID + "/messages"
	if opts.Select == "" && !opts.IncludeBodies {
		opts.Select = EnvelopeSelectFields
	}
	q := url.Values{}
	if opts.Select != "" {
		q.Set("$select", opts.Select)
	}
	if opts.Filter != "" {
		q.Set("$filter", opts.Filter)
	}
	if opts.Top > 0 {
		q.Set("$top", strconv.Itoa(opts.Top))
	}
	if opts.OrderBy != "" {
		q.Set("$orderby", opts.OrderBy)
	}
	full := path
	if encoded := q.Encode(); encoded != "" {
		full = path + "?" + encoded
	}
	resp, err := c.Do(ctx, http.MethodGet, full, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var page ListMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("graph: decode messages: %w", err)
	}
	return &page, nil
}

// FollowNext is the same shape as ListMessagesInFolder but fetches a
// pre-built nextLink URL.
func (c *Client) FollowNext(ctx context.Context, nextLink string) (*ListMessagesResponse, error) {
	resp, err := c.Do(ctx, http.MethodGet, nextLink, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var page ListMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("graph: decode messages: %w", err)
	}
	return &page, nil
}

// GetMessageBody fetches body + attachment metadata for a message id.
// Spec 05 §5.2 — `$expand=attachments` returns metadata for every
// attached fileAttachment / itemAttachment in one round-trip; the
// alternative (separate /attachments call per message open) doubled
// the latency on attachment-heavy threads. The attachment payload's
// `contentBytes` field is intentionally NOT in `$select` — body fetch
// is on the hot path, and attaching N MB of base64 to every viewer
// open would blow the latency budget. Bytes are pulled on demand by
// the save / open path (PR 10).
func (c *Client) GetMessageBody(ctx context.Context, id string) (*Message, error) {
	url := "/me/messages/" + id +
		"?$select=body,hasAttachments,internetMessageHeaders" +
		"&$expand=attachments($select=id,name,contentType,size,isInline)"
	resp, err := c.Do(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var m Message
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("graph: decode message body: %w", err)
	}
	return &m, nil
}

// GetAttachment fetches raw bytes for one fileAttachment (spec 05 §8.1 / PR 10).
// Graph encodes the bytes as base64 in a JSON wrapper; we decode and return
// them so the caller can write to disk. Bytes are NOT cached in the local store
// — they land directly in the user's save directory or a temp dir for open.
// The confirm-modal threshold (>25 MB by default) is enforced in the UI layer
// before this function is called, so we do not need a size guard here.
func (c *Client) GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error) {
	path := "/me/messages/" + messageID + "/attachments/" + attachmentID + "?$select=contentBytes"
	resp, err := c.Do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var payload struct {
		ContentBytes string `json:"contentBytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("graph: decode attachment: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(payload.ContentBytes)
	if err != nil {
		return nil, fmt.Errorf("graph: decode attachment bytes: %w", err)
	}
	return data, nil
}

// MessageHeader is one entry in internetMessageHeaders. Graph returns
// duplicate Name entries for multi-value headers — caller must
// concatenate or pick the first depending on header semantics.
type MessageHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// GetMessageHeaders fetches the raw RFC 822 headers for a message id.
// Spec 16 calls this lazily on the first U-key press; the result is
// parsed for List-Unsubscribe and persisted on the row so subsequent
// presses are local lookups.
func (c *Client) GetMessageHeaders(ctx context.Context, id string) ([]MessageHeader, error) {
	url := "/me/messages/" + id + "?$select=internetMessageHeaders"
	resp, err := c.Do(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var payload struct {
		Headers []MessageHeader `json:"internetMessageHeaders"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("graph: decode headers: %w", err)
	}
	return payload.Headers, nil
}

// HeaderValue returns the first value for the named header
// (case-insensitive), or "" if not present.
func HeaderValue(headers []MessageHeader, name string) string {
	for _, h := range headers {
		if equalFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// equalFold is a one-line ASCII case-insensitive compare; avoids
// pulling strings.EqualFold into the hot path of header iteration
// across thousands of messages.
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
