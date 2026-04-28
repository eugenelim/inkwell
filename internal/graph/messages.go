package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ListMessagesOpts narrows /me/mailFolders/{id}/messages results.
type ListMessagesOpts struct {
	Select         string
	Filter         string
	Top            int
	OrderBy        string
	IncludeBodies  bool // when true, omit $select restriction
	ReceivedSince  time.Time
}

// ListMessagesResponse is the paginated wrapper.
type ListMessagesResponse struct {
	Value    []Message `json:"value"`
	NextLink string    `json:"@odata.nextLink,omitempty"`
}

// ListMessagesInFolder paginates through a folder's messages. The
// initial backfill (spec §5) consumes the result.
func (c *Client) ListMessagesInFolder(ctx context.Context, folderID string, opts ListMessagesOpts) (*ListMessagesResponse, error) {
	url := "/me/mailFolders/" + folderID + "/messages"
	q := ""
	add := func(k, v string) {
		sep := "&"
		if q == "" {
			sep = "?"
		}
		q += sep + k + "=" + v
	}
	if opts.Select == "" && !opts.IncludeBodies {
		opts.Select = EnvelopeSelectFields
	}
	if opts.Select != "" {
		add("$select", opts.Select)
	}
	if opts.Filter != "" {
		add("$filter", opts.Filter)
	}
	if opts.Top > 0 {
		add("$top", strconv.Itoa(opts.Top))
	}
	if opts.OrderBy != "" {
		add("$orderby", opts.OrderBy)
	}
	resp, err := c.Do(ctx, http.MethodGet, url+q, nil, nil)
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
