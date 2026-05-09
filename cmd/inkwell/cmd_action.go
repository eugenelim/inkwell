package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/customaction"
	"github.com/eugenelim/inkwell/internal/pattern"
	"github.com/eugenelim/inkwell/internal/store"
)

// newActionCmd returns the `inkwell action` parent command (spec 27 §4.11).
// Subcommands: list / show / run / validate. The catalogue is loaded
// from `[custom_actions].file` (default ~/.config/inkwell/actions.toml).
func newActionCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "action",
		Short: "Run user-defined custom actions (spec 27)",
		Long: `Custom actions chain primitive mail operations into one named verb.
Recipes live in ~/.config/inkwell/actions.toml — see
docs/user/how-to.md#bundle-a-noisy-newsletter-sender for examples.

Examples:
  inkwell action list
  inkwell action show newsletter_done
  inkwell action run newsletter_done --message <id>
  inkwell action run delete_old_news --filter '~f news@x.com'
  inkwell action validate`,
	}
	cmd.AddCommand(newActionListCmd(rc))
	cmd.AddCommand(newActionShowCmd(rc))
	cmd.AddCommand(newActionRunCmd(rc))
	cmd.AddCommand(newActionValidateCmd(rc))
	return cmd
}

func newActionListCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured custom actions",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cat, err := loadCatalogueForCLI(rc)
			if err != nil {
				return err
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				type row struct {
					Name        string `json:"name"`
					Key         string `json:"key,omitempty"`
					Description string `json:"description"`
					Steps       int    `json:"steps"`
				}
				out := make([]row, 0, len(cat.Actions))
				for _, a := range cat.Actions {
					out = append(out, row{Name: a.Name, Key: a.Key, Description: a.Description, Steps: len(a.Steps)})
				}
				return json.NewEncoder(os.Stdout).Encode(out)
			}
			if len(cat.Actions) == 0 {
				return nil // empty catalogue → exit 0 with no output
			}
			names := make([]string, 0, len(cat.Actions))
			for _, a := range cat.Actions {
				names = append(names, a.Name)
			}
			sort.Strings(names)
			fmt.Fprintf(c.OutOrStdout(), "%-24s %-6s %-6s  %s\n", "NAME", "KEY", "STEPS", "DESCRIPTION")
			for _, n := range names {
				a := cat.ByName[n]
				fmt.Fprintf(c.OutOrStdout(), "%-24s %-6s %-6d  %s\n", a.Name, a.Key, len(a.Steps), a.Description)
			}
			return nil
		},
	}
}

func newActionShowCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show a custom action's resolved sequence",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cat, err := loadCatalogueForCLI(rc)
			if err != nil {
				return err
			}
			a, ok := cat.ByName[args[0]]
			if !ok {
				return fmt.Errorf("action %q not found", args[0])
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				type stepOut struct {
					Op     string         `json:"op"`
					Params map[string]any `json:"params,omitempty"`
				}
				type out struct {
					Name        string    `json:"name"`
					Key         string    `json:"key,omitempty"`
					Description string    `json:"description"`
					Confirm     string    `json:"confirm"`
					Steps       []stepOut `json:"steps"`
				}
				steps := make([]stepOut, len(a.Steps))
				for i, s := range a.Steps {
					steps[i] = stepOut{Op: string(s.Op), Params: s.Params}
				}
				return json.NewEncoder(os.Stdout).Encode(out{
					Name:        a.Name,
					Key:         a.Key,
					Description: a.Description,
					Confirm:     confirmString(a.Confirm),
					Steps:       steps,
				})
			}
			fmt.Fprintf(c.OutOrStdout(), "%s — %s\n", a.Name, a.Description)
			if a.Key != "" {
				fmt.Fprintf(c.OutOrStdout(), "  key:     %s\n", a.Key)
			}
			fmt.Fprintf(c.OutOrStdout(), "  confirm: %s\n", confirmString(a.Confirm))
			fmt.Fprintln(c.OutOrStdout(), "  sequence:")
			for i, s := range a.Steps {
				fmt.Fprintf(c.OutOrStdout(), "    %d. %s", i+1, s.Op)
				if dest, ok := s.Params["destination"].(string); ok {
					fmt.Fprintf(c.OutOrStdout(), " → %s", dest)
				}
				if cat, ok := s.Params["category"].(string); ok {
					fmt.Fprintf(c.OutOrStdout(), " [%s]", cat)
				}
				if pat, ok := s.Params["pattern"].(string); ok {
					fmt.Fprintf(c.OutOrStdout(), " %s", pat)
				}
				if pmt, ok := s.Params["prompt"].(string); ok {
					fmt.Fprintf(c.OutOrStdout(), " %q", pmt)
				}
				fmt.Fprintln(c.OutOrStdout())
			}
			return nil
		},
	}
}

func newActionRunCmd(rc *rootContext) *cobra.Command {
	var msgID, filterPat string
	cmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Run a custom action (against --message <id> or --filter <pattern>)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			cat, err := loadCatalogueForCLIWithApp(ctx, rc, app)
			if err != nil {
				return err
			}
			a, ok := cat.ByName[args[0]]
			if !ok {
				return fmt.Errorf("action %q not found", args[0])
			}
			if msgID == "" && filterPat == "" {
				return usageErr(fmt.Errorf("action run %q: this action needs --message <id> or --filter <pattern>", a.Name))
			}
			if filterPat != "" && a.RequiresMessageContext {
				return usageErr(fmt.Errorf("action run %q: --filter rejected (action's templates reference per-message variables)", a.Name))
			}
			cctx := customaction.Context{AccountID: app.account.ID, SelectionKind: "single"}
			if msgID != "" {
				m, err := app.store.GetMessage(ctx, msgID)
				if err != nil {
					return fmt.Errorf("message %q not found in local cache", msgID)
				}
				populateContextFromMessage(&cctx, m)
			}
			if filterPat != "" {
				cctx.SelectionKind = "filtered"
				ids, err := resolveFilterIDs(ctx, app, filterPat, rc.cfg.Bulk.SizeHardMax)
				if err != nil {
					return fmt.Errorf("--filter %q: %w", filterPat, err)
				}
				cctx.SelectionIDs = ids
			}
			deps := buildCustomActionDeps(app, app.logger, rc.cfg)
			res, err := customaction.Run(ctx, a, cctx, deps)
			if err != nil {
				fmt.Fprintln(c.ErrOrStderr(), err.Error())
				os.Exit(1)
			}
			if res.Continuation != nil {
				fmt.Fprintln(c.ErrOrStderr(), "action run from CLI cannot complete prompt_value sequences (no TTY input)")
				os.Exit(1)
			}
			ok2, failed, _ := tally(res)
			if effectiveOutput(rc, rc.cfg) == "json" {
				type out struct {
					Name   string `json:"name"`
					OK     int    `json:"ok"`
					Failed int    `json:"failed"`
				}
				_ = json.NewEncoder(os.Stdout).Encode(out{Name: a.Name, OK: ok2, Failed: failed})
			} else {
				fmt.Fprintf(c.OutOrStdout(), "%s: %d ok, %d failed\n", a.Name, ok2, failed)
			}
			if failed > 0 {
				os.Exit(2)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&msgID, "message", "", "message ID to run against")
	cmd.Flags().StringVar(&filterPat, "filter", "", "spec-08 filter pattern (for *_filtered actions)")
	return cmd
}

func newActionValidateCmd(rc *rootContext) *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate actions.toml without running anything",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg := rc.cfg
			if cfg == nil {
				loaded, err := rc.loadConfig()
				if err != nil {
					return err
				}
				cfg = loaded
			}
			actionsPath := path
			if actionsPath == "" {
				actionsPath = resolveActionsPath(cfg)
			}
			cat, err := customaction.LoadCatalogue(c.Context(), actionsPath, customaction.Deps{
				PatternCompile: stubPatternCompileForValidate,
			})
			if err != nil {
				fmt.Fprintln(c.ErrOrStderr(), err.Error())
				os.Exit(1)
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ %s: %d action(s) loaded\n", actionsPath, len(cat.Actions))
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "file", "", "actions.toml path (default ~/.config/inkwell/actions.toml)")
	return cmd
}

// loadCatalogueForCLI is the read-only path: list / show don't need
// the headless app. Reads config + actions.toml, validates without
// the executor adapters.
func loadCatalogueForCLI(rc *rootContext) (*customaction.Catalogue, error) {
	cfg := rc.cfg
	if cfg == nil {
		loaded, err := rc.loadConfig()
		if err != nil {
			return nil, err
		}
		cfg = loaded
	}
	path := resolveActionsPath(cfg)
	return customaction.LoadCatalogue(context.Background(), path, customaction.Deps{
		PatternCompile: stubPatternCompileForValidate,
	})
}

// loadCatalogueForCLIWithApp loads against the live signed-in app
// (action run path). The headless app is needed for store calls.
func loadCatalogueForCLIWithApp(ctx context.Context, rc *rootContext, _ *headlessApp) (*customaction.Catalogue, error) {
	return loadCatalogueForCLI(rc)
}

// confirmString stringifies the customaction.ConfirmPolicy.
func confirmString(p customaction.ConfirmPolicy) string {
	switch p {
	case customaction.ConfirmAlways:
		return "always"
	case customaction.ConfirmNever:
		return "never"
	default:
		return "auto"
	}
}

func tally(res customaction.Result) (ok, failed, skipped int) {
	for _, r := range res.Steps {
		switch r.Status {
		case customaction.StepOK:
			ok++
		case customaction.StepFailed:
			failed++
		case customaction.StepSkipped:
			skipped++
		}
	}
	return
}

// populateContextFromMessage snapshots a *store.Message into the
// customaction.Context shape. Mirrors the UI-side helper.
func populateContextFromMessage(c *customaction.Context, m *store.Message) {
	c.From = strings.ToLower(strings.TrimSpace(m.FromAddress))
	c.FromName = m.FromName
	if at := strings.LastIndex(c.From, "@"); at >= 0 {
		c.SenderDomain = c.From[at+1:]
	}
	c.Subject = m.Subject
	c.ConversationID = m.ConversationID
	c.MessageID = m.ID
	c.IsRead = m.IsRead
	c.FlagStatus = m.FlagStatus
	c.Date = m.ReceivedAt
	if len(m.ToAddresses) > 0 {
		c.To = m.ToAddresses[0].Address
	}
}

// resolveFilterIDs runs a spec-08 pattern against the local store and
// returns the matched message IDs. Used by `inkwell action run
// --filter <pat>`. Reuses the runFilterListing path that `inkwell
// filter` uses, capped by [bulk].size_hard_max so an over-broad
// pattern does not enqueue tens of thousands of operations.
func resolveFilterIDs(ctx context.Context, app *headlessApp, pat string, sizeHardMax int) ([]string, error) {
	if app == nil || app.store == nil {
		return nil, fmt.Errorf("filter: store not wired")
	}
	if sizeHardMax <= 0 {
		sizeHardMax = 5000
	}
	// Pull cap+1 so we can detect "too many matches" without scanning
	// the entire result set on the cap boundary.
	msgs, err := runFilterListing(ctx, app, pat, "", sizeHardMax+1)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("no messages match")
	}
	if len(msgs) > sizeHardMax {
		return nil, fmt.Errorf("filter matched more than %d messages (bulk size_hard_max); refine the pattern", sizeHardMax)
	}
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	return ids, nil
}

// stubPatternCompileForValidate is the loader-side pattern compiler
// used by `validate` and read-only CLI paths. It matches
// pattern.Compile's signature; production wires the same.
var stubPatternCompileForValidate = func(s string, opts pattern.CompileOptions) (*pattern.Compiled, error) {
	return pattern.Compile(s, opts)
}
