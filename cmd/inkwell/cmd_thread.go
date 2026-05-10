package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/action"
	"github.com/eugenelim/inkwell/internal/store"
)

func newThreadCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thread",
		Short: "Conversation-level operations (spec 20)",
	}
	cmd.AddCommand(
		newThreadArchiveCmd(rc),
		newThreadDeleteCmd(rc),
		newThreadPermanentDeleteCmd(rc),
		newThreadMarkReadCmd(rc),
		newThreadMarkUnreadCmd(rc),
		newThreadFlagCmd(rc),
		newThreadUnflagCmd(rc),
		newThreadMoveCmd(rc),
	)
	return cmd
}

func newThreadArchiveCmd(rc *rootContext) *cobra.Command {
	var outputFmt string
	cmd := &cobra.Command{
		Use:     "archive <conversation-id>",
		Aliases: []string{"done"}, // spec 30 §5.6 — Cobra alias.
		Short:   "Archive an entire thread (alias: done)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildHeadlessApp(cmd.Context(), rc)
			if err != nil {
				return err
			}
			defer app.Close()
			ex := action.New(app.store, app.graph, app.logger)
			total, results, err := cliThreadMove(cmd.Context(), ex, app, args[0], "", "archive")
			if err != nil {
				return err
			}
			return printThreadResult(cmd, outputFmt, "archive", args[0], total, results)
		},
	}
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text or json")
	return cmd
}

func newThreadDeleteCmd(rc *rootContext) *cobra.Command {
	var outputFmt string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <conversation-id>",
		Short: "Soft-delete an entire thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildHeadlessApp(cmd.Context(), rc)
			if err != nil {
				return err
			}
			defer app.Close()
			convID := args[0]
			if !yes {
				ids, listErr := app.store.MessageIDsInConversation(cmd.Context(), app.account.ID, convID, true)
				if listErr != nil {
					return listErr
				}
				fmt.Fprintf(cmd.OutOrStdout(), "would delete %d messages — pass --yes to apply\n", len(ids))
				return nil
			}
			ex := action.New(app.store, app.graph, app.logger)
			total, results, err := cliThreadExecute(cmd.Context(), ex, app, convID, store.ActionSoftDelete)
			if err != nil {
				return err
			}
			return printThreadResult(cmd, outputFmt, "delete", convID, total, results)
		},
	}
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&yes, "yes", false, "Apply the delete (required for destructive operations)")
	return cmd
}

func newThreadPermanentDeleteCmd(rc *rootContext) *cobra.Command {
	var outputFmt string
	var yes bool
	cmd := &cobra.Command{
		Use:   "permanent-delete <conversation-id>",
		Short: "Permanently delete an entire thread (irreversible)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildHeadlessApp(cmd.Context(), rc)
			if err != nil {
				return err
			}
			defer app.Close()
			convID := args[0]
			if !yes {
				ids, listErr := app.store.MessageIDsInConversation(cmd.Context(), app.account.ID, convID, true)
				if listErr != nil {
					return listErr
				}
				fmt.Fprintf(cmd.OutOrStdout(), "would permanently delete %d messages — pass --yes to apply\n", len(ids))
				return nil
			}
			ex := action.New(app.store, app.graph, app.logger)
			total, results, err := cliThreadExecute(cmd.Context(), ex, app, convID, store.ActionPermanentDelete)
			if err != nil {
				return err
			}
			return printThreadResult(cmd, outputFmt, "permanent-delete", convID, total, results)
		},
	}
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&yes, "yes", false, "Apply the delete (required for destructive operations)")
	return cmd
}

func newThreadMarkReadCmd(rc *rootContext) *cobra.Command {
	var outputFmt string
	cmd := &cobra.Command{
		Use:   "mark-read <conversation-id>",
		Short: "Mark an entire thread as read",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildHeadlessApp(cmd.Context(), rc)
			if err != nil {
				return err
			}
			defer app.Close()
			ex := action.New(app.store, app.graph, app.logger)
			total, results, err := cliThreadExecute(cmd.Context(), ex, app, args[0], store.ActionMarkRead)
			if err != nil {
				return err
			}
			return printThreadResult(cmd, outputFmt, "mark-read", args[0], total, results)
		},
	}
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text or json")
	return cmd
}

func newThreadMarkUnreadCmd(rc *rootContext) *cobra.Command {
	var outputFmt string
	cmd := &cobra.Command{
		Use:   "mark-unread <conversation-id>",
		Short: "Mark an entire thread as unread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildHeadlessApp(cmd.Context(), rc)
			if err != nil {
				return err
			}
			defer app.Close()
			ex := action.New(app.store, app.graph, app.logger)
			total, results, err := cliThreadExecute(cmd.Context(), ex, app, args[0], store.ActionMarkUnread)
			if err != nil {
				return err
			}
			return printThreadResult(cmd, outputFmt, "mark-unread", args[0], total, results)
		},
	}
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text or json")
	return cmd
}

func newThreadFlagCmd(rc *rootContext) *cobra.Command {
	var outputFmt string
	cmd := &cobra.Command{
		Use:   "flag <conversation-id>",
		Short: "Flag every message in a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildHeadlessApp(cmd.Context(), rc)
			if err != nil {
				return err
			}
			defer app.Close()
			ex := action.New(app.store, app.graph, app.logger)
			total, results, err := cliThreadExecute(cmd.Context(), ex, app, args[0], store.ActionFlag)
			if err != nil {
				return err
			}
			return printThreadResult(cmd, outputFmt, "flag", args[0], total, results)
		},
	}
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text or json")
	return cmd
}

func newThreadUnflagCmd(rc *rootContext) *cobra.Command {
	var outputFmt string
	cmd := &cobra.Command{
		Use:   "unflag <conversation-id>",
		Short: "Unflag every message in a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildHeadlessApp(cmd.Context(), rc)
			if err != nil {
				return err
			}
			defer app.Close()
			ex := action.New(app.store, app.graph, app.logger)
			total, results, err := cliThreadExecute(cmd.Context(), ex, app, args[0], store.ActionUnflag)
			if err != nil {
				return err
			}
			return printThreadResult(cmd, outputFmt, "unflag", args[0], total, results)
		},
	}
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text or json")
	return cmd
}

func newThreadMoveCmd(rc *rootContext) *cobra.Command {
	var outputFmt string
	var folderName string
	cmd := &cobra.Command{
		Use:   "move <conversation-id>",
		Short: "Move an entire thread to a folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if folderName == "" {
				return fmt.Errorf("thread move: --folder required")
			}
			app, err := buildHeadlessApp(cmd.Context(), rc)
			if err != nil {
				return err
			}
			defer app.Close()
			convID := args[0]
			folderID, _, _, resolveErr := resolveFolderByNameCtx(cmd.Context(), app, folderName)
			if resolveErr != nil {
				return fmt.Errorf("thread move: %w", resolveErr)
			}
			ex := action.New(app.store, app.graph, app.logger)
			total, results, err := cliThreadMove(cmd.Context(), ex, app, convID, folderID, "")
			if err != nil {
				return err
			}
			return printThreadResult(cmd, outputFmt, "move", convID, total, results)
		},
	}
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text or json")
	cmd.Flags().StringVar(&folderName, "folder", "", "Destination folder name or well-known name")
	return cmd
}

// cliThreadExecute fetches all message IDs for the conversation (all
// folders, since the CLI user is explicit) and applies the action.
func cliThreadExecute(ctx context.Context, ex *action.Executor, app *headlessApp, convID string, verb store.ActionType) (int, []action.BatchResult, error) {
	ids, err := app.store.MessageIDsInConversation(ctx, app.account.ID, convID, true)
	if err != nil {
		return 0, nil, err
	}
	if len(ids) == 0 {
		return 0, nil, nil
	}
	results, err := ex.BatchExecute(ctx, app.account.ID, verb, ids)
	return len(ids), results, err
}

// cliThreadMove fetches all message IDs for the conversation and
// bulk-moves them to the destination folder.
func cliThreadMove(ctx context.Context, ex *action.Executor, app *headlessApp, convID, destFolderID, destAlias string) (int, []action.BatchResult, error) {
	ids, err := app.store.MessageIDsInConversation(ctx, app.account.ID, convID, true)
	if err != nil {
		return 0, nil, err
	}
	if len(ids) == 0 {
		return 0, nil, nil
	}
	results, err := ex.BulkMove(ctx, app.account.ID, ids, destFolderID, destAlias)
	return len(ids), results, err
}

type threadResultJSON struct {
	Action         string `json:"action"`
	ConversationID string `json:"conversation_id"`
	Succeeded      int    `json:"succeeded"`
	Failed         int    `json:"failed"`
}

func printThreadResult(cmd *cobra.Command, format, action, convID string, total int, results []action.BatchResult) error {
	var succeeded, failed int
	for _, r := range results {
		if r.Err != nil {
			failed++
		} else {
			succeeded++
		}
	}
	if format == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(threadResultJSON{
			Action:         action,
			ConversationID: convID,
			Succeeded:      succeeded,
			Failed:         failed,
		})
	}
	if total == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "thread: 0 messages to act on\n")
		return nil
	}
	if failed == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s thread (%d messages)\n", action, total)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "⚠ %s thread: %d/%d succeeded — %d failed\n", action, succeeded, total, failed)
	}
	return nil
}
