package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/search"
	"github.com/eugenelim/inkwell/internal/store"
)

// TestSourceNameAllCases verifies the sourceName helper covers all three
// ResultSource values (spec 06 CLI output shape).
func TestSourceNameAllCases(t *testing.T) {
	require.Equal(t, "local", sourceName(search.SourceLocal))
	require.Equal(t, "server", sourceName(search.SourceServer))
	require.Equal(t, "both", sourceName(search.SourceBoth))
}

// TestSearchSourceSummaryCountsByBucket verifies that searchSourceSummary
// tallies local/server/both counts correctly.
func TestSearchSourceSummaryCountsByBucket(t *testing.T) {
	rs := []search.Result{
		{Source: search.SourceLocal},
		{Source: search.SourceLocal},
		{Source: search.SourceServer},
		{Source: search.SourceBoth},
	}
	got := searchSourceSummary(rs)
	require.Contains(t, got, "local 2")
	require.Contains(t, got, "server 1")
	require.Contains(t, got, "both 1")
}

// TestSearchCLILocalOnlyReturnsResults verifies that the hybrid Searcher
// configured with local-only (nil server) returns FTS5 matches from the store.
func TestSearchCLILocalOnlyReturnsResults(t *testing.T) {
	app := newCLITestApp(t)
	now := time.Now()
	require.NoError(t, app.store.UpsertMessage(context.Background(), store.Message{
		ID:          "m-search-1",
		AccountID:   app.account.ID,
		FolderID:    "f-inbox",
		Subject:     "Q4 budget planning",
		FromAddress: "alice@example.invalid",
		ReceivedAt:  now,
	}))
	require.NoError(t, app.store.UpsertMessage(context.Background(), store.Message{
		ID:          "m-search-2",
		AccountID:   app.account.ID,
		FolderID:    "f-inbox",
		Subject:     "Holiday schedule",
		FromAddress: "bob@example.invalid",
		ReceivedAt:  now.Add(-time.Hour),
	}))

	s := search.New(app.store, nil, search.Options{
		AccountID:    app.account.ID,
		DefaultLimit: 200,
	})
	stream := s.Search(context.Background(), search.Query{
		Text:      "budget",
		LocalOnly: true,
	})
	defer stream.Cancel()

	var last []search.Result
	for snap := range stream.Updates() {
		last = snap
	}

	require.Len(t, last, 1, "only the budget message should match")
	require.Equal(t, "m-search-1", last[0].Message.ID)
	require.Equal(t, search.SourceLocal, last[0].Source)
}

// TestSearchCLIFolderScopeFilters verifies that passing a non-empty FolderID
// to the Searcher limits results to messages in that folder.
func TestSearchCLIFolderScopeFilters(t *testing.T) {
	app := newCLITestApp(t)
	now := time.Now()
	require.NoError(t, app.store.UpsertMessage(context.Background(), store.Message{
		ID: "m-inbox", AccountID: app.account.ID, FolderID: "f-inbox",
		Subject: "budget meeting", FromAddress: "a@example.invalid", ReceivedAt: now,
	}))
	require.NoError(t, app.store.UpsertMessage(context.Background(), store.Message{
		ID: "m-projects", AccountID: app.account.ID, FolderID: "f-projects",
		Subject: "budget review", FromAddress: "b@example.invalid", ReceivedAt: now,
	}))

	s := search.New(app.store, nil, search.Options{
		AccountID:    app.account.ID,
		DefaultLimit: 200,
	})
	stream := s.Search(context.Background(), search.Query{
		Text:      "budget",
		FolderID:  "f-inbox",
		LocalOnly: true,
	})
	defer stream.Cancel()

	var last []search.Result
	for snap := range stream.Updates() {
		last = snap
	}
	require.Len(t, last, 1, "folder scope must exclude messages in f-projects")
	require.Equal(t, "m-inbox", last[0].Message.ID)
}

// TestSearchCLIAllOverridesFolder verifies that when --all is set the folder
// scope is cleared: both folders' messages are returned.
func TestSearchCLIAllOverridesFolder(t *testing.T) {
	app := newCLITestApp(t)
	now := time.Now()
	require.NoError(t, app.store.UpsertMessage(context.Background(), store.Message{
		ID: "m-inbox", AccountID: app.account.ID, FolderID: "f-inbox",
		Subject: "budget sync", FromAddress: "a@example.invalid", ReceivedAt: now,
	}))
	require.NoError(t, app.store.UpsertMessage(context.Background(), store.Message{
		ID: "m-projects", AccountID: app.account.ID, FolderID: "f-projects",
		Subject: "budget plan", FromAddress: "b@example.invalid", ReceivedAt: now,
	}))

	s := search.New(app.store, nil, search.Options{
		AccountID:    app.account.ID,
		DefaultLimit: 200,
	})
	// --all → folderID="" (no scope)
	stream := s.Search(context.Background(), search.Query{
		Text:      "budget",
		FolderID:  "", // --all clears folder scope
		LocalOnly: true,
	})
	defer stream.Cancel()

	var last []search.Result
	for snap := range stream.Updates() {
		last = snap
	}
	require.Len(t, last, 2, "--all must return matches across all folders")
}
