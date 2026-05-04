package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/action"
	"github.com/eugenelim/inkwell/internal/savedsearch"
	"github.com/eugenelim/inkwell/internal/store"
)

func newRuleCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rule",
		Short: "Manage saved search rules",
		Long: `Create, edit, evaluate, and apply saved search rules (spec 11).

Examples:
  inkwell rule list
  inkwell rule save newsletters --pattern '~f newsletter@*' --pin
  inkwell rule eval newsletters
  inkwell rule apply newsletters --action delete --apply`,
	}
	cmd.AddCommand(newRuleListCmd(rc))
	cmd.AddCommand(newRuleShowCmd(rc))
	cmd.AddCommand(newRuleSaveCmd(rc))
	cmd.AddCommand(newRuleEditCmd(rc))
	cmd.AddCommand(newRuleDeleteCmd(rc))
	cmd.AddCommand(newRuleEvalCmd(rc))
	cmd.AddCommand(newRuleApplyCmd(rc))
	return cmd
}

func newRuleListCmd(rc *rootContext) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all saved search rules",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			cfg, err := rc.loadConfig()
			if err != nil {
				return err
			}
			mgr := savedsearch.New(app.store, app.account.ID, cfg.SavedSearch)
			searches, err := mgr.List(ctx)
			if err != nil {
				return err
			}
			if output == "json" {
				return json.NewEncoder(os.Stdout).Encode(searches)
			}
			if len(searches) == 0 {
				fmt.Fprintln(os.Stdout, "(no rules defined)")
				return nil
			}
			fmt.Fprintf(os.Stdout, "%-20s %-6s %-5s %s\n", "NAME", "PINNED", "ORDER", "PATTERN")
			for _, s := range searches {
				pinned := "no"
				if s.Pinned {
					pinned = "yes"
				}
				fmt.Fprintf(os.Stdout, "%-20s %-6s %-5d %s\n",
					truncCLI(s.Name, 20), pinned, s.SortOrder, s.Pattern)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func newRuleShowCmd(rc *rootContext) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show details of a saved search rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			cfg, err := rc.loadConfig()
			if err != nil {
				return err
			}
			mgr := savedsearch.New(app.store, app.account.ID, cfg.SavedSearch)
			ss, err := mgr.Get(ctx, args[0])
			if err != nil {
				return err
			}
			if ss == nil {
				return fmt.Errorf("rule %q not found", args[0])
			}
			if output == "json" {
				return json.NewEncoder(os.Stdout).Encode(ss)
			}
			fmt.Fprintf(os.Stdout, "Name:       %s\n", ss.Name)
			fmt.Fprintf(os.Stdout, "Pattern:    %s\n", ss.Pattern)
			fmt.Fprintf(os.Stdout, "Pinned:     %v\n", ss.Pinned)
			fmt.Fprintf(os.Stdout, "SortOrder:  %d\n", ss.SortOrder)
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func newRuleSaveCmd(rc *rootContext) *cobra.Command {
	var pattern string
	var pin bool
	var sortOrder int
	cmd := &cobra.Command{
		Use:   "save <name>",
		Short: "Create or update a saved search rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if pattern == "" {
				return fmt.Errorf("--pattern is required")
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			cfg, err := rc.loadConfig()
			if err != nil {
				return err
			}
			mgr := savedsearch.New(app.store, app.account.ID, cfg.SavedSearch)
			ss := store.SavedSearch{
				Name:      args[0],
				Pattern:   pattern,
				Pinned:    pin,
				SortOrder: sortOrder,
				CreatedAt: time.Now(),
			}
			if err := mgr.Save(ctx, ss); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "saved rule %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&pattern, "pattern", "", "spec 08 pattern (required)")
	cmd.Flags().BoolVar(&pin, "pin", false, "pin to sidebar")
	cmd.Flags().IntVar(&sortOrder, "sort-order", 0, "sort order in sidebar")
	return cmd
}

func newRuleEditCmd(rc *rootContext) *cobra.Command {
	var pattern string
	var pin *bool
	var sortOrder int
	var pinFlag bool
	var noPinFlag bool
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit an existing saved search rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			cfg, err := rc.loadConfig()
			if err != nil {
				return err
			}
			mgr := savedsearch.New(app.store, app.account.ID, cfg.SavedSearch)
			existing, err := mgr.Get(ctx, args[0])
			if err != nil {
				return err
			}
			if existing == nil {
				return fmt.Errorf("rule %q not found", args[0])
			}
			if pattern != "" {
				existing.Pattern = pattern
			}
			if pinFlag {
				v := true
				pin = &v
			} else if noPinFlag {
				v := false
				pin = &v
			}
			if pin != nil {
				existing.Pinned = *pin
			}
			if c.Flags().Changed("sort-order") {
				existing.SortOrder = sortOrder
			}
			if err := mgr.Save(ctx, *existing); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "updated rule %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&pattern, "pattern", "", "new pattern")
	cmd.Flags().BoolVar(&pinFlag, "pin", false, "pin to sidebar")
	cmd.Flags().BoolVar(&noPinFlag, "no-pin", false, "remove pin from sidebar")
	cmd.Flags().IntVar(&sortOrder, "sort-order", 0, "new sort order")
	// Suppress the unused pin pointer lint — it's set conditionally above.
	_ = pin
	return cmd
}

func newRuleDeleteCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a saved search rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			if !rc.yes {
				if !confirm(c, fmt.Sprintf("Delete rule %q?", args[0])) {
					return nil
				}
			}
			cfg, err := rc.loadConfig()
			if err != nil {
				return err
			}
			mgr := savedsearch.New(app.store, app.account.ID, cfg.SavedSearch)
			if err := mgr.DeleteByName(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "deleted rule %q\n", args[0])
			return nil
		},
	}
	return cmd
}

func newRuleEvalCmd(rc *rootContext) *cobra.Command {
	var showMessages bool
	var output string
	cmd := &cobra.Command{
		Use:   "eval <name>",
		Short: "Evaluate a saved search rule and print matches",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			cfg, err := rc.loadConfig()
			if err != nil {
				return err
			}
			mgr := savedsearch.New(app.store, app.account.ID, cfg.SavedSearch)
			result, err := mgr.Evaluate(ctx, args[0], true)
			if err != nil {
				return err
			}
			if output == "json" {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"name":  args[0],
					"count": result.Count,
					"ids":   result.MessageIDs,
				})
			}
			fmt.Fprintf(os.Stdout, "rule %q matched %d message(s)\n", args[0], result.Count)
			if showMessages && len(result.MessageIDs) > 0 {
				msgs := make([]store.Message, 0, len(result.MessageIDs))
				for _, id := range result.MessageIDs {
					m, err := app.store.GetMessage(ctx, id)
					if err == nil && m != nil {
						msgs = append(msgs, *m)
					}
				}
				printMessageList(msgs)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showMessages, "messages", false, "print message envelopes")
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func newRuleApplyCmd(rc *rootContext) *cobra.Command {
	var actionType string
	var apply bool
	cmd := &cobra.Command{
		Use:   "apply <name>",
		Short: "Evaluate a rule and dispatch a bulk action",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if !apply {
				return fmt.Errorf("pass --apply to execute (dry-run by default)")
			}
			if actionType == "" {
				return fmt.Errorf("--action is required (delete|archive|mark-read)")
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			cfg, err := rc.loadConfig()
			if err != nil {
				return err
			}
			mgr := savedsearch.New(app.store, app.account.ID, cfg.SavedSearch)
			result, err := mgr.Evaluate(ctx, args[0], true)
			if err != nil {
				return err
			}
			if len(result.MessageIDs) == 0 {
				fmt.Fprintln(os.Stdout, "no messages matched — nothing to do")
				return nil
			}
			if !rc.yes && actionType == "delete" {
				if !confirm(c, fmt.Sprintf("Delete %d messages matched by %q?", result.Count, args[0])) {
					return nil
				}
			}
			exec := action.New(app.store, app.graph, app.logger)
			var results []action.BatchResult
			switch actionType {
			case "delete":
				results, err = exec.BulkSoftDelete(ctx, app.account.ID, result.MessageIDs)
			case "archive":
				results, err = exec.BulkArchive(ctx, app.account.ID, result.MessageIDs)
			case "mark-read":
				results, err = exec.BulkMarkRead(ctx, app.account.ID, result.MessageIDs)
			default:
				return fmt.Errorf("unknown action %q (use delete|archive|mark-read)", actionType)
			}
			if err != nil {
				return fmt.Errorf("bulk: %w", err)
			}
			ok, fail := 0, 0
			for _, r := range results {
				if r.Err != nil {
					fail++
				} else {
					ok++
				}
			}
			fmt.Fprintf(os.Stdout, "✓ %s: %d succeeded, %d failed\n", actionType, ok, fail)
			if fail > 0 {
				return fmt.Errorf("%d action(s) failed", fail)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&actionType, "action", "", "delete|archive|mark-read")
	cmd.Flags().BoolVar(&apply, "apply", false, "execute the action (without this flag the command is a no-op)")
	return cmd
}
