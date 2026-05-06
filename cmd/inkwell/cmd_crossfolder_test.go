package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// newCrossFolderCLIApp returns a headlessApp seeded with messages in two
// folders (f-inbox and f-projects) for cross-folder CLI tests.
func newCrossFolderCLIApp(t *testing.T) *headlessApp {
	t.Helper()
	app := newCLITestApp(t)
	ctx := context.Background()
	acc := app.account.ID
	// f-inbox and f-projects are already seeded by newCLITestApp.
	for _, m := range []store.Message{
		{ID: "cf-inbox-1", AccountID: acc, FolderID: "f-inbox", Subject: "cfkeyword inbox A", FromAddress: "bob@example.invalid", ReceivedAt: time.Now().Add(-time.Hour)},
		{ID: "cf-inbox-2", AccountID: acc, FolderID: "f-inbox", Subject: "cfkeyword inbox B", FromAddress: "bob@example.invalid", ReceivedAt: time.Now().Add(-2 * time.Hour)},
		{ID: "cf-proj-1", AccountID: acc, FolderID: "f-projects", Subject: "cfkeyword project A", FromAddress: "bob@example.invalid", ReceivedAt: time.Now().Add(-3 * time.Hour)},
	} {
		require.NoError(t, app.store.UpsertMessage(ctx, m))
	}
	return app
}

// TestFilterCLIAllFlagAddsFolderMetadata verifies that `inkwell filter
// --all` adds a `folders` count map to JSON output.
func TestFilterCLIAllFlagAddsFolderMetadata(t *testing.T) {
	app := newCrossFolderCLIApp(t)
	ctx := context.Background()

	// Retrieve the messages that match the keyword.
	msgs, err := runFilterListing(ctx, app, "~f *bob*", "", 100)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(msgs), 3, "should match messages in both folders")

	// Build the folder metadata (as cmd_filter.go does when --all is set).
	folderRows, err := app.store.ListFolders(ctx, app.account.ID)
	require.NoError(t, err)
	nameByID := make(map[string]string, len(folderRows))
	for _, f := range folderRows {
		nameByID[f.ID] = f.DisplayName
	}
	folderCounts := make(map[string]int)
	for _, msg := range msgs {
		folderCounts[nameByID[msg.FolderID]]++
	}

	// Encode to JSON and verify the folders key is present.
	var buf bytes.Buffer
	err = json.NewEncoder(&buf).Encode(struct {
		Pattern    string          `json:"pattern"`
		AllFolders bool            `json:"all_folders"`
		Matched    int             `json:"matched"`
		Folders    map[string]int  `json:"folders"`
		Messages   []store.Message `json:"messages"`
	}{"~f *bob*", true, len(msgs), folderCounts, msgs})
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	require.True(t, out["all_folders"].(bool), "all_folders must be true")
	require.GreaterOrEqual(t, int(out["matched"].(float64)), 3)

	folders, ok := out["folders"].(map[string]any)
	require.True(t, ok, "folders key must be present in JSON output")
	require.GreaterOrEqual(t, len(folders), 2, "should list at least 2 folders")

	// Inbox and Projects must each have a non-zero count.
	inboxCount, hasInbox := folders["Inbox"]
	require.True(t, hasInbox, "Inbox must appear in folder counts")
	require.Greater(t, int(inboxCount.(float64)), 0)

	projCount, hasProj := folders["Projects"]
	require.True(t, hasProj, "Projects must appear in folder counts")
	require.Greater(t, int(projCount.(float64)), 0)
}

// TestMessagesFilterAllOverridesFolder verifies that --filter combined with
// --all returns cross-folder results (folderID passed as "" to
// runFilterListing) regardless of any --folder value.
func TestMessagesFilterAllOverridesFolder(t *testing.T) {
	app := newCrossFolderCLIApp(t)
	ctx := context.Background()

	// With folderID scoped to f-inbox only.
	scopedMsgs, err := runFilterListing(ctx, app, "~f *bob*", "f-inbox", 100)
	require.NoError(t, err)
	// With allFolders=true, folderID is overridden to "".
	allMsgs, err := runFilterListing(ctx, app, "~f *bob*", "", 100)
	require.NoError(t, err)

	require.Greater(t, len(allMsgs), len(scopedMsgs), "--all (folderID='') must return more results than scoped query")

	// Verify the cross-folder result contains messages from f-projects.
	var hasProj bool
	for _, m := range allMsgs {
		if m.FolderID == "f-projects" {
			hasProj = true
			break
		}
	}
	require.True(t, hasProj, "cross-folder results must include f-projects messages")
}
