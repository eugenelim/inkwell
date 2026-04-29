package graph

import (
	"context"
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

// GetMessageBody fetches body + attachments for a message id.
func (c *Client) GetMessageBody(ctx context.Context, id string) (*Message, error) {
	url := "/me/messages/" + id + "?$select=body,hasAttachments"
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
