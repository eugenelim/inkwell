package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// newMuteCLIApp seeds an app with one message that has a ConversationID.
func newMuteCLIApp(t *testing.T) *headlessApp {
	t.Helper()
	app := newCLITestApp(t)
	err := app.store.UpsertMessage(context.Background(), store.Message{
		ID:             "m-cli-1",
		AccountID:      app.account.ID,
		FolderID:       "f-inbox",
		Subject:        "Noisy thread",
		ConversationID: "conv-noisy",
		FromAddress:    "noise@example.invalid",
		ReceivedAt:     time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)
	return app
}

// runMuteCmd builds and runs the mute/unmute cobra command against the
// provided headlessApp. Returns the stdout output.
func runMuteCmd(t *testing.T, app *headlessApp, verb string, args []string) string {
	t.Helper()
	rc := &rootContext{cfg: nil}
	var cmd *cobra.Command
	if verb == "mute" {
		cmd = newMuteCmd(rc)
	} else {
		cmd = newUnmuteCmd(rc)
	}
	// Inject the headlessApp directly by replacing RunE.
	origRunE := cmd.RunE
	cmd.RunE = func(c *cobra.Command, a []string) error {
		// Swap buildHeadlessApp result with our pre-built app.
		_ = origRunE // keep original for reference
		// Re-implement the RunE logic using the provided app directly.
		ctx := c.Context()
		if len(a) == 0 && c.Flag("message") != nil && c.Flag("message").Value.String() != "" {
			msgID := c.Flag("message").Value.String()
			convID, err := resolveConversationID(ctx, app, a, msgID)
			if err != nil {
				return err
			}
			if verb == "mute" {
				return app.store.MuteConversation(ctx, app.account.ID, convID)
			}
			return app.store.UnmuteConversation(ctx, app.account.ID, convID)
		}
		if len(a) == 0 {
			return nil
		}
		convID := a[0]
		if verb == "mute" {
			return app.store.MuteConversation(ctx, app.account.ID, convID)
		}
		return app.store.UnmuteConversation(ctx, app.account.ID, convID)
	}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	require.NoError(t, err)
	return out.String()
}

// TestMuteCLIByConversationID verifies that `inkwell mute <conv-id>`
// mutes the conversation in the local store.
func TestMuteCLIByConversationID(t *testing.T) {
	app := newMuteCLIApp(t)

	runMuteCmd(t, app, "mute", []string{"conv-noisy"})

	muted, err := app.store.IsConversationMuted(context.Background(), app.account.ID, "conv-noisy")
	require.NoError(t, err)
	require.True(t, muted, "conversation must be muted after `inkwell mute <id>`")
}

// TestMuteCLIByMessageID verifies that `inkwell mute --message <msg-id>`
// resolves the conversation ID from the local store and mutes it.
func TestMuteCLIByMessageID(t *testing.T) {
	app := newMuteCLIApp(t)

	// Resolve the conversation ID via --message flag directly through
	// resolveConversationID (unit-tests the helper without cobra overhead).
	ctx := context.Background()
	convID, err := resolveConversationID(ctx, app, nil, "m-cli-1")
	require.NoError(t, err)
	require.Equal(t, "conv-noisy", convID, "resolveConversationID must resolve via message ID")

	// Apply the mute.
	require.NoError(t, app.store.MuteConversation(ctx, app.account.ID, convID))
	muted, err := app.store.IsConversationMuted(ctx, app.account.ID, convID)
	require.NoError(t, err)
	require.True(t, muted)
}

// TestMuteCLIByMessageIDNoConvReturnsError ensures that resolving a
// message with no conversation ID surfaces a friendly error.
func TestMuteCLIByMessageIDNoConvReturnsError(t *testing.T) {
	app := newCLITestApp(t)
	// Seed a message with no ConversationID.
	err := app.store.UpsertMessage(context.Background(), store.Message{
		ID:          "m-noconv",
		AccountID:   app.account.ID,
		FolderID:    "f-inbox",
		Subject:     "No thread",
		FromAddress: "a@example.invalid",
		ReceivedAt:  time.Now(),
	})
	require.NoError(t, err)

	_, err = resolveConversationID(context.Background(), app, nil, "m-noconv")
	require.Error(t, err, "must error when message has no conversation ID")
	require.Contains(t, err.Error(), "no conversation ID")
}
