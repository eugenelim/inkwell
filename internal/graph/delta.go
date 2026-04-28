package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// DeltaOpts controls the delta query.
type DeltaOpts struct {
	// Select restricts the columns returned. Defaults to
	// EnvelopeSelectFields.
	Select string
	// MaxPageSize maps to the Prefer: odata.maxpagesize header. 100
	// is a Microsoft-recommended page size for envelope sync.
	MaxPageSize int
}

// GetDelta fetches one delta page. url is either a per-folder delta
// endpoint (first call) or a nextLink/deltaLink from a previous page.
// The caller drives the loop; this function handles a single page.
func (c *Client) GetDelta(ctx context.Context, url string, opts DeltaOpts) (*DeltaResponse, error) {
	hdr := http.Header{}
	if opts.MaxPageSize > 0 {
		hdr.Set("Prefer", "odata.maxpagesize="+strconv.Itoa(opts.MaxPageSize))
	}
	// $select is a query param, only meaningful on the first call —
	// subsequent nextLinks already carry it.
	if opts.Select != "" && !hasQueryParam(url, "$select") {
		sep := "?"
		if hasQuery(url) {
			sep = "&"
		}
		url = url + sep + "$select=" + opts.Select
	}
	resp, err := c.Do(ctx, http.MethodGet, url, nil, hdr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var d DeltaResponse
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("graph: decode delta: %w", err)
	}
	return &d, nil
}

func hasQuery(url string) bool {
	for i := 0; i < len(url); i++ {
		if url[i] == '?' {
			return true
		}
	}
	return false
}

func hasQueryParam(url, key string) bool {
	if !hasQuery(url) {
		return false
	}
	// Cheap substring match. Good enough; the only callers are us.
	return contains(url, key+"=")
}

func contains(haystack, needle string) bool {
	return indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
