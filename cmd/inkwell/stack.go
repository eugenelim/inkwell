package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/action"
	"github.com/eugenelim/inkwell/internal/store"
)

// stackKind identifies which inkwell stack a `inkwell later` /
// `inkwell aside` subcommand operates on. Spec 25 §6.
type stackKind struct {
	subcmd   string // "later" / "aside"
	category string
	jsonName string // "reply_later" / "set_aside"
}

var (
	stackReplyLater = stackKind{subcmd: "later", category: store.CategoryReplyLater, jsonName: "reply_later"}
	stackSetAside   = stackKind{subcmd: "aside", category: store.CategorySetAside, jsonName: "set_aside"}
)

// stackParentCmd builds the `inkwell later` / `inkwell aside`
// parent command with shared subcommands.
func stackParentCmd(rc *rootContext, kind stackKind, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   kind.subcmd,
		Short: short,
		Long: fmt.Sprintf(`Manage the %s stack — saved as the %q category on each
message and surfaced in the TUI sidebar (when count > 0). Spec 25.

Examples:
  inkwell %s add <message-id>
  inkwell %s remove <message-id>
  inkwell %s list
  inkwell %s count`, kind.jsonName, kind.category, kind.subcmd, kind.subcmd, kind.subcmd, kind.subcmd),
	}
	cmd.AddCommand(stackAddCmd(rc, kind))
	cmd.AddCommand(stackRemoveCmd(rc, kind))
	cmd.AddCommand(stackListCmd(rc, kind))
	cmd.AddCommand(stackCountCmd(rc, kind))
	return cmd
}

func stackAddCmd(rc *rootContext, kind stackKind) *cobra.Command {
	return &cobra.Command{
		Use:   "add <message-id>",
		Short: "Tag a message into this stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return usageErr(fmt.Errorf("%s add: message id is empty", kind.subcmd))
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			exec := action.New(app.store, app.graph, app.logger)
			if err := exec.AddCategory(ctx, app.account.ID, id, kind.category); err != nil {
				return fmt.Errorf("%s add: %w", kind.subcmd, err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Stack    string `json:"stack"`
					Message  string `json:"message_id"`
					Tagged   bool   `json:"tagged"`
					Category string `json:"category"`
				}{kind.jsonName, id, true, kind.category})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ tagged %s with %s\n", id, kind.category)
			return nil
		},
	}
}

func stackRemoveCmd(rc *rootContext, kind stackKind) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <message-id>",
		Short: "Untag a message from this stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return usageErr(fmt.Errorf("%s remove: message id is empty", kind.subcmd))
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			exec := action.New(app.store, app.graph, app.logger)
			if err := exec.RemoveCategory(ctx, app.account.ID, id, kind.category); err != nil {
				return fmt.Errorf("%s remove: %w", kind.subcmd, err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Stack    string `json:"stack"`
					Message  string `json:"message_id"`
					Removed  bool   `json:"removed"`
					Category string `json:"category"`
				}{kind.jsonName, id, true, kind.category})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ removed %s from %s\n", id, kind.category)
			return nil
		},
	}
}

func stackListCmd(rc *rootContext, kind stackKind) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Print messages in this stack (newest first)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			if limit <= 0 {
				limit = 200
			}
			msgs, err := app.store.ListMessagesInCategory(ctx, app.account.ID, kind.category, limit)
			if err != nil {
				return fmt.Errorf("%s list: %w", kind.subcmd, err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				type row struct {
					ID         string `json:"id"`
					Subject    string `json:"subject"`
					From       string `json:"from"`
					ReceivedAt string `json:"received_at"`
					Folder     string `json:"folder"`
				}
				out := struct {
					Stack    string `json:"stack"`
					Count    int    `json:"count"`
					Messages []row  `json:"messages"`
				}{Stack: kind.jsonName, Count: len(msgs), Messages: make([]row, 0, len(msgs))}
				for _, m := range msgs {
					out.Messages = append(out.Messages, row{
						ID:         m.ID,
						Subject:    m.Subject,
						From:       m.FromAddress,
						ReceivedAt: m.ReceivedAt.UTC().Format(time.RFC3339),
						Folder:     m.FolderID,
					})
				}
				return json.NewEncoder(os.Stdout).Encode(out)
			}
			if len(msgs) == 0 {
				fmt.Fprintln(c.OutOrStdout(), "(empty)")
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "%-20s  %-30s  %s\n", "RECEIVED", "FROM", "SUBJECT")
			for _, m := range msgs {
				when := m.ReceivedAt.UTC().Format("2006-01-02 15:04 UTC")
				fmt.Fprintf(c.OutOrStdout(), "%-20s  %-30s  %s\n", when, truncateField(m.FromAddress, 30), m.Subject)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 200, "max messages to list")
	return cmd
}

func stackCountCmd(rc *rootContext, kind stackKind) *cobra.Command {
	return &cobra.Command{
		Use:   "count",
		Short: "Print the number of messages in this stack",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			n, err := app.store.CountMessagesInCategory(ctx, app.account.ID, kind.category)
			if err != nil {
				return fmt.Errorf("%s count: %w", kind.subcmd, err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Stack string `json:"stack"`
					Count int    `json:"count"`
				}{kind.jsonName, n})
			}
			fmt.Fprintln(c.OutOrStdout(), n)
			return nil
		},
	}
}

func truncateField(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	rs := []rune(s)
	return string(rs[:max-1]) + "…"
}

// silence unused if stackParentCmd isn't called yet (it is, but Go
// is not great at detecting interface-only-method sets here).
var _ = context.Background
