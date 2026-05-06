package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newMuteCmd(rc *rootContext) *cobra.Command {
	var messageID string
	cmd := &cobra.Command{
		Use:   "mute <conversation-id>",
		Short: "Mute a conversation thread (local only)",
		Long: `Mute a conversation so it no longer appears in normal folder views.
Mute state is stored locally — no Graph API call is made.

Pass a conversation ID directly, or use --message to resolve via a message ID.

Examples:
  inkwell mute AAQkADIwN...
  inkwell mute --message AAMkADIwN...`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			convID, err := resolveConversationID(ctx, app, args, messageID)
			if err != nil {
				return err
			}
			if err := app.store.MuteConversation(ctx, app.account.ID, convID); err != nil {
				return fmt.Errorf("mute: %w", err)
			}
			return printMuteResult(c, rc, convID, true)
		},
	}
	cmd.Flags().StringVar(&messageID, "message", "", "resolve conversation ID from this message ID")
	return cmd
}

func newUnmuteCmd(rc *rootContext) *cobra.Command {
	var messageID string
	cmd := &cobra.Command{
		Use:   "unmute <conversation-id>",
		Short: "Unmute a previously muted conversation thread",
		Long: `Remove a conversation from the muted list so it appears again in
normal folder views.

Examples:
  inkwell unmute AAQkADIwN...
  inkwell unmute --message AAMkADIwN...`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			convID, err := resolveConversationID(ctx, app, args, messageID)
			if err != nil {
				return err
			}
			if err := app.store.UnmuteConversation(ctx, app.account.ID, convID); err != nil {
				return fmt.Errorf("unmute: %w", err)
			}
			return printMuteResult(c, rc, convID, false)
		},
	}
	cmd.Flags().StringVar(&messageID, "message", "", "resolve conversation ID from this message ID")
	return cmd
}

// resolveConversationID returns the conversation ID either from the
// positional arg or by looking up the message in the local store.
func resolveConversationID(ctx context.Context, app *headlessApp, args []string, messageID string) (string, error) {
	if messageID != "" {
		msg, err := app.store.GetMessage(ctx, messageID)
		if err != nil {
			return "", fmt.Errorf("message %q not found in local cache", messageID)
		}
		if msg.ConversationID == "" {
			return "", fmt.Errorf("message %q has no conversation ID", messageID)
		}
		return msg.ConversationID, nil
	}
	if len(args) == 0 {
		return "", fmt.Errorf("provide a conversation ID or --message <message-id>")
	}
	return args[0], nil
}

func printMuteResult(c *cobra.Command, rc *rootContext, convID string, muted bool) error {
	verb := "muted"
	if !muted {
		verb = "unmuted"
	}
	if effectiveOutput(rc, rc.cfg) == "json" {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Muted          bool   `json:"muted"`
			ConversationID string `json:"conversation_id"`
		}{muted, convID})
	}
	fmt.Fprintf(c.OutOrStdout(), "✓ %s conversation %s\n", verb, convID)
	return nil
}
