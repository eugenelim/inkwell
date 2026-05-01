package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// SearchMessagesOpts narrows the /me/messages?$search call. Spec 06
// §4.2: $search and $filter are not combinable on this endpoint, so
// any structured constraints have to flow through the search query
// itself ("from:bob AND subject:budget"). $orderby is rejected by
// the server when combined with $search — relevance order is
// returned regardless.
type SearchMessagesOpts struct {
	// Query is the user-facing search expression. Caller is
	// responsible for translating field-prefix syntax (`from:bob`,
	// `subject:Q4`) into the Graph dialect; this helper applies
	// only the URL-encoding and double-quoting required by the
	// $search parameter.
	Query string
	// FolderID scopes the search to a single mailbox folder. Empty
	// means /me/messages (all folders).
	FolderID string
	// Top is the page size. Graph rejects values > 250 on $search.
	Top int
	// Select is the $select projection. Empty falls back to
	// EnvelopeSelectFields (the same set used by sync).
	Select string
}

// SearchMessages issues GET /me/messages?$search=... (or the
// per-folder variant) and returns one page of results plus the
// continuation cursor.
//
// URL encoding: Graph requires the $search value to be wrapped in
// double quotes (RFC 3986 unreserved characters notwithstanding —
// quoting is what makes the server treat the value as a phrase
// rather than tokenising). The naive form `$search=q4 review` runs
// a wrong query silently; `$search="q4 review"` is the correct
// shape. We always quote.
func (c *Client) SearchMessages(ctx context.Context, opts SearchMessagesOpts) (*ListMessagesResponse, error) {
	if opts.Query == "" {
		return nil, fmt.Errorf("graph: SearchMessages requires a non-empty query")
	}
	path := "/me/messages"
	if opts.FolderID != "" {
		path = "/me/mailFolders/" + opts.FolderID + "/messages"
	}
	sel := opts.Select
	if sel == "" {
		sel = EnvelopeSelectFields
	}
	q := url.Values{}
	q.Set("$select", sel)
	if opts.Top > 0 {
		q.Set("$top", strconv.Itoa(opts.Top))
	}

	// Build the query string manually so the $search value can
	// retain the literal double-quote wrapping that net/url's
	// values.Encode would percent-encode but in the wrong order
	// (Graph wants `%22query%22`, which url.QueryEscape does
	// produce — but mixing in the other params via Values.Encode
	// reorders the keys non-deterministically and the test fixture
	// relies on a stable ordering for the URL match).
	full := path + "?$search=" + url.QueryEscape(`"`+opts.Query+`"`) + "&" + q.Encode()
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
		return nil, fmt.Errorf("graph: decode search results: %w", err)
	}
	return &page, nil
}
