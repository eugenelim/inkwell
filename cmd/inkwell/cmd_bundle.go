package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// newBundleCmd returns the `inkwell bundle` parent command (spec 26 §7).
// Subcommands: add / remove / list. Bundling is local-only — no Graph
// API call is made; mutations apply on the next `Ctrl+R` refresh
// inside a running TUI (spec 26 §6.1).
func newBundleCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Manage per-sender bundle designations (local only)",
		Long: `Bundling collapses consecutive same-sender messages in the list pane
into a single header row. Designations are local-only; no Graph API
call is made. Address arguments are lowercased before storage.

Examples:
  inkwell bundle add news@example.com
  inkwell bundle remove news@example.com
  inkwell bundle list
  inkwell bundle list --output json`,
	}
	cmd.AddCommand(newBundleAddCmd(rc))
	cmd.AddCommand(newBundleRemoveCmd(rc))
	cmd.AddCommand(newBundleListCmd(rc))
	return cmd
}

func newBundleAddCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "add <address>",
		Short: "Designate a sender for bundling",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			addr := strings.ToLower(strings.TrimSpace(args[0]))
			if err := validateBareAddress(addr); err != nil {
				return usageErr(err)
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			if err := app.store.AddBundledSender(ctx, app.account.ID, addr); err != nil {
				return fmt.Errorf("bundle add: %w", err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Bundled bool   `json:"bundled"`
					Address string `json:"address"`
				}{true, addr})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ bundled %s\n", addr)
			return nil
		},
	}
}

func newBundleRemoveCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <address>",
		Short: "Remove a sender's bundle designation",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			addr := strings.ToLower(strings.TrimSpace(args[0]))
			if err := validateBareAddress(addr); err != nil {
				return usageErr(err)
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			if err := app.store.RemoveBundledSender(ctx, app.account.ID, addr); err != nil {
				return fmt.Errorf("bundle remove: %w", err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Bundled bool   `json:"bundled"`
					Address string `json:"address"`
				}{false, addr})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ unbundled %s\n", addr)
			return nil
		},
	}
}

func newBundleListCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List currently bundled senders",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			rows, err := app.store.ListBundledSenders(ctx, app.account.ID)
			if err != nil {
				return fmt.Errorf("bundle list: %w", err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				type row struct {
					Address string    `json:"address"`
					AddedAt time.Time `json:"added_at"`
				}
				out := make([]row, 0, len(rows))
				for _, r := range rows {
					out = append(out, row{r.Address, r.AddedAt})
				}
				return json.NewEncoder(os.Stdout).Encode(out)
			}
			if len(rows) == 0 {
				fmt.Fprintln(c.OutOrStdout(), "(no bundled senders)")
				return nil
			}
			fmt.Fprintln(c.OutOrStdout(), "ADDRESS\tADDED")
			for _, r := range rows {
				fmt.Fprintf(c.OutOrStdout(), "%s\t%s\n", r.Address, r.AddedAt.Local().Format(time.RFC3339))
			}
			return nil
		},
	}
}
