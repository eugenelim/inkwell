package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/savedsearch"
)

// newTabCmd returns the `inkwell tab` parent command. Spec 24 §9.
// Subcommands: list / add / remove / move. There is no `tab eval` —
// `inkwell rule eval` already evaluates a saved search by name.
func newTabCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tab",
		Short: "Manage spec 24 split-inbox tabs (saved searches promoted to the list-pane tab strip)",
		Long: `Tabs are saved searches that surface as a strip above the list pane.
Promoting a saved search to a tab is local-only and persists across
restart. Cycle in the TUI with ] / [ when the list pane is focused.

Examples:
  inkwell tab list
  inkwell tab add Newsletters
  inkwell tab remove Newsletters
  inkwell tab move Newsletters 0`,
	}
	cmd.AddCommand(newTabListCmd(rc))
	cmd.AddCommand(newTabAddCmd(rc))
	cmd.AddCommand(newTabRemoveCmd(rc))
	cmd.AddCommand(newTabMoveCmd(rc))
	return cmd
}

// tabManager builds a savedsearch.Manager bound to the current
// account, mirroring cmd_rule's pattern.
func tabManager(rc *rootContext, app *headlessApp) (*savedsearch.Manager, error) {
	cfg, err := rc.loadConfig()
	if err != nil {
		return nil, err
	}
	return savedsearch.New(app.store, app.account.ID, cfg.SavedSearch), nil
}

func newTabListCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the configured tabs (with matched + unread counts)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			mgr, err := tabManager(rc, app)
			if err != nil {
				return err
			}
			tabs, err := mgr.Tabs(ctx)
			if err != nil {
				return fmt.Errorf("tab list: %w", err)
			}
			counts, _ := mgr.CountTabs(ctx)
			matched := make(map[int64]int, len(tabs))
			for _, t := range tabs {
				r, err := mgr.Evaluate(ctx, t.Name, false)
				if err == nil && r != nil {
					matched[t.ID] = r.Count
				}
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				type row struct {
					Name     string `json:"name"`
					TabOrder int    `json:"tab_order"`
					Matched  int    `json:"matched"`
					Unread   int    `json:"unread"`
				}
				out := struct {
					Tabs []row `json:"tabs"`
				}{Tabs: make([]row, 0, len(tabs))}
				for _, t := range tabs {
					order := 0
					if t.TabOrder != nil {
						order = *t.TabOrder
					}
					out.Tabs = append(out.Tabs, row{
						Name: t.Name, TabOrder: order,
						Matched: matched[t.ID], Unread: counts[t.ID],
					})
				}
				return json.NewEncoder(os.Stdout).Encode(out)
			}
			if len(tabs) == 0 {
				fmt.Fprintln(c.OutOrStdout(), "(no tabs)")
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "%-4s  %-30s  %-8s  %s\n", "POS", "NAME", "UNREAD", "MATCHED")
			for _, t := range tabs {
				order := 0
				if t.TabOrder != nil {
					order = *t.TabOrder
				}
				fmt.Fprintf(c.OutOrStdout(), "%-4d  %-30s  %-8d  %d\n",
					order, t.Name, counts[t.ID], matched[t.ID])
			}
			return nil
		},
	}
	return cmd
}

func newTabAddCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Promote a saved search to the tab strip (appends at the end)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			mgr, err := tabManager(rc, app)
			if err != nil {
				return err
			}
			order, err := mgr.Promote(ctx, name)
			if err != nil {
				return usageErr(err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Name     string `json:"name"`
					TabOrder int    `json:"tab_order"`
				}{name, order})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ tab %q at position %d\n", name, order)
			return nil
		},
	}
	return cmd
}

func newTabRemoveCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Demote a saved search from the tab strip",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			mgr, err := tabManager(rc, app)
			if err != nil {
				return err
			}
			if err := mgr.Demote(ctx, name); err != nil {
				return usageErr(err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Name    string `json:"name"`
					Removed bool   `json:"removed"`
				}{name, true})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ tab %q removed\n", name)
			return nil
		},
	}
	return cmd
}

func newTabMoveCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move <name> <position>",
		Short: "Reorder the tab strip (0-based position)",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			var to int
			if _, err := fmt.Sscanf(args[1], "%d", &to); err != nil {
				return usageErr(fmt.Errorf("tab move: position %q is not an integer", args[1]))
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			mgr, err := tabManager(rc, app)
			if err != nil {
				return err
			}
			tabs, err := mgr.Tabs(ctx)
			if err != nil {
				return fmt.Errorf("tab move: %w", err)
			}
			from := -1
			for i, t := range tabs {
				if t.Name == name {
					from = i
					break
				}
			}
			if from < 0 {
				return usageErr(fmt.Errorf("tab move: no tab named %q", name))
			}
			if err := mgr.Reorder(ctx, from, to); err != nil {
				return usageErr(err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Name string `json:"name"`
					From int    `json:"from"`
					To   int    `json:"to"`
				}{name, from, to})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ moved %q from %d to %d\n", name, from, to)
			return nil
		},
	}
	return cmd
}
