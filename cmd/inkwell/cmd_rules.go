package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/config"
	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/rules"
	"github.com/eugenelim/inkwell/internal/store"
)

// newRulesCmd returns the `inkwell rules` parent. Spec 32 §8.
// Subcommands: list, get, pull, apply, edit, new, delete, enable,
// disable, move.
func newRulesCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage server-side Inbox message rules (spec 32)",
		Long: `Server-side rules run on every incoming Inbox message; persist
server-side; visible across all clients. inkwell exposes a curated v1
subset of Microsoft Graph's messageRule resource (29 predicates, 11
actions — minus forward / redirect / permanentDelete).

Workflow: edit ~/.config/inkwell/rules.toml, then run
` + "`inkwell rules apply --dry-run`" + ` to preview the diff, then
` + "`inkwell rules apply`" + ` to push it. ` + "`inkwell rules pull`" + `
re-syncs the local mirror from the server.`,
	}
	cmd.AddCommand(newRulesListCmd(rc))
	cmd.AddCommand(newRulesGetCmd(rc))
	cmd.AddCommand(newRulesPullCmd(rc))
	cmd.AddCommand(newRulesApplyCmd(rc))
	cmd.AddCommand(newRulesEditCmd(rc))
	cmd.AddCommand(newRulesNewCmd(rc))
	cmd.AddCommand(newRulesDeleteCmd(rc))
	cmd.AddCommand(newRulesEnableCmd(rc))
	cmd.AddCommand(newRulesDisableCmd(rc))
	cmd.AddCommand(newRulesMoveCmd(rc))
	return cmd
}

// rulesFilePath resolves the configured [rules].file or falls back to
// the default. Honours `~/...` expansion via os.UserHomeDir.
func rulesFilePath(cfg *config.Config) (string, error) {
	p := cfg.Rules.File
	if p == "" {
		return rules.DefaultPath()
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[2:])
	}
	return p, nil
}

func newRulesListCmd(rc *rootContext) *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cached rules (uses local mirror)",
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			if refresh {
				path, err := rulesFilePath(rc.cfg)
				if err != nil {
					return err
				}
				if _, err := rules.Pull(ctx, app.graph, app.store, app.account.ID, path); err != nil {
					return fmt.Errorf("rules pull: %w", err)
				}
			}
			rs, err := app.store.ListMessageRules(ctx, app.account.ID)
			if err != nil {
				return fmt.Errorf("rules list: %w", err)
			}
			return renderRulesList(c.OutOrStdout(), rs, effectiveOutput(rc, rc.cfg))
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Pull from server before listing")
	return cmd
}

type ruleSummary struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Sequence int      `json:"sequence"`
	Enabled  bool     `json:"enabled"`
	Flags    []string `json:"flags,omitempty"`
}

func renderRulesList(w io.Writer, rs []store.MessageRule, output string) error {
	summaries := make([]ruleSummary, 0, len(rs))
	for _, r := range rs {
		s := ruleSummary{ID: r.RuleID, Name: r.DisplayName, Sequence: r.Sequence, Enabled: r.IsEnabled}
		if r.IsReadOnly {
			s.Flags = append(s.Flags, "read_only")
		}
		if r.HasError {
			s.Flags = append(s.Flags, "error")
		}
		summaries = append(summaries, s)
	}
	if output == "json" {
		return json.NewEncoder(w).Encode(summaries)
	}
	if len(summaries) == 0 {
		fmt.Fprintln(w, "(no rules cached — press `inkwell rules pull` to fetch from server)")
		return nil
	}
	fmt.Fprintln(w, "SEQ  EN  ID                       NAME                              FLAGS")
	for _, s := range summaries {
		en := "✓"
		if !s.Enabled {
			en = "⊘"
		}
		id := truncate(s.ID, 24)
		name := truncate(s.Name, 32)
		flags := strings.Join(s.Flags, ",")
		fmt.Fprintf(w, "%-4d %s   %-24s %-32s %s\n", s.Sequence, en, id, name, flags)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func newRulesGetCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a single rule by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			r, err := app.store.GetMessageRule(ctx, app.account.ID, args[0])
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return usageErr(fmt.Errorf("rule %q not found in local mirror; try `inkwell rules pull`", args[0]))
				}
				return fmt.Errorf("rules get: %w", err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(r)
			}
			renderRuleVerbose(c.OutOrStdout(), *r)
			return nil
		},
	}
	return cmd
}

func renderRuleVerbose(w io.Writer, r store.MessageRule) {
	en := "yes"
	if !r.IsEnabled {
		en = "no"
	}
	fmt.Fprintf(w, "ID:        %s\n", r.RuleID)
	fmt.Fprintf(w, "Name:      %s\n", r.DisplayName)
	fmt.Fprintf(w, "Sequence:  %d\n", r.Sequence)
	fmt.Fprintf(w, "Enabled:   %s\n", en)
	if r.IsReadOnly {
		fmt.Fprintln(w, "Flags:     read-only (admin-managed)")
	}
	if r.HasError {
		fmt.Fprintln(w, "Flags:     has-error (edit in Outlook web)")
	}
	fmt.Fprintf(w, "Last pull: %s\n", r.LastPulledAt.Format(time.RFC3339))
	if len(r.RawConditions) > 0 {
		fmt.Fprintf(w, "Conditions: %s\n", string(r.RawConditions))
	}
	if len(r.RawActions) > 0 {
		fmt.Fprintf(w, "Actions:    %s\n", string(r.RawActions))
	}
	if len(r.RawExceptions) > 0 && string(r.RawExceptions) != "{}" {
		fmt.Fprintf(w, "Exceptions: %s\n", string(r.RawExceptions))
	}
}

func newRulesPullCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Pull rules from server and rewrite rules.toml",
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			path, err := rulesFilePath(rc.cfg)
			if err != nil {
				return err
			}
			res, err := rules.Pull(ctx, app.graph, app.store, app.account.ID, path)
			if err != nil {
				return fmt.Errorf("rules pull: %w", err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(struct {
					Pulled int    `json:"pulled"`
					Path   string `json:"path"`
				}{res.Pulled, res.Path})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ pulled %d rules; rewrote %s\n", res.Pulled, res.Path)
			return nil
		},
	}
}

func newRulesApplyCmd(rc *rootContext) *cobra.Command {
	var dryRun bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply rules.toml to the server (pull → diff → execute)",
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			path, err := rulesFilePath(rc.cfg)
			if err != nil {
				return err
			}
			cat, err := rules.LoadCatalogue(path)
			if err != nil {
				return usageErr(err)
			}
			confirmer := func(d rules.DiffEntry) bool {
				if yes {
					return true
				}
				fmt.Fprintf(c.OutOrStdout(), "Apply %s rule %q (destructive)? [y/N] ", diffOpLabel(d.Op), d.Rule.Name)
				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				line = strings.TrimSpace(strings.ToLower(line))
				return line == "y" || line == "yes"
			}
			res, err := rules.Apply(ctx, app.graph, app.store, app.account.ID, cat, rules.ApplyOptions{
				DryRun:             dryRun,
				Yes:                yes,
				ConfirmDestructive: rc.cfg.Rules.ConfirmDestructive,
				Confirmer:          confirmer,
			})
			if err != nil {
				return fmt.Errorf("rules apply: %w", err)
			}

			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(struct {
					DryRun  bool                `json:"dry_run"`
					Created int                 `json:"created"`
					Updated int                 `json:"updated"`
					Deleted int                 `json:"deleted"`
					Skipped int                 `json:"skipped"`
					Failed  int                 `json:"failed"`
					Diff    []rulesDiffSummary  `json:"diff,omitempty"`
					Errors  []rulesErrorSummary `json:"errors,omitempty"`
				}{
					DryRun:  dryRun,
					Created: res.Created,
					Updated: res.Updated,
					Deleted: res.Deleted,
					Skipped: res.Skipped,
					Failed:  res.Failed,
					Diff:    summariseDiff(res.Diff),
					Errors:  summariseErrors(res.Errors),
				})
			}
			renderApplyDiff(c.OutOrStdout(), res, dryRun)
			if res.Failed > 0 {
				return fmt.Errorf("rules apply: %d rule(s) failed; see output above", res.Failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Compute the diff without writing to Graph")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip interactive confirmation prompts")
	return cmd
}

type rulesDiffSummary struct {
	Op      string `json:"op"`
	Name    string `json:"name"`
	ID      string `json:"id,omitempty"`
	Warning string `json:"warning,omitempty"`
}

type rulesErrorSummary struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
	Err  string `json:"error"`
}

func summariseDiff(d []rules.DiffEntry) []rulesDiffSummary {
	out := make([]rulesDiffSummary, 0, len(d))
	for _, e := range d {
		out = append(out, rulesDiffSummary{
			Op:      diffOpLabel(e.Op),
			Name:    e.Rule.Name,
			ID:      e.Rule.ID,
			Warning: e.Warning,
		})
	}
	return out
}

func summariseErrors(errs []rules.ApplyError) []rulesErrorSummary {
	out := make([]rulesErrorSummary, 0, len(errs))
	for _, e := range errs {
		out = append(out, rulesErrorSummary{Name: e.RuleName, ID: e.RuleID, Err: e.Err.Error()})
	}
	return out
}

func renderApplyDiff(w io.Writer, res *rules.ApplyResult, dryRun bool) {
	if dryRun {
		fmt.Fprintln(w, "Dry run — no writes were made to Graph.")
	}
	for _, d := range res.Diff {
		fmt.Fprintf(w, "  %-6s %s", diffOpLabel(d.Op), d.Rule.Name)
		if d.Warning != "" {
			fmt.Fprintf(w, "   ⚠ %s", d.Warning)
		}
		fmt.Fprintln(w)
	}
	if dryRun {
		return
	}
	fmt.Fprintf(w, "\n✓ rules applied: %d created, %d updated, %d deleted, %d skipped, %d failed\n",
		res.Created, res.Updated, res.Deleted, res.Skipped, res.Failed)
	for _, e := range res.Errors {
		fmt.Fprintf(w, "  ✗ %s\n", e.Error())
	}
}

func diffOpLabel(op rules.DiffOp) string {
	switch op {
	case rules.DiffCreate:
		return "create"
	case rules.DiffUpdate:
		return "update"
	case rules.DiffDelete:
		return "delete"
	case rules.DiffSkip:
		return "skip"
	default:
		return "noop"
	}
}

func newRulesEditCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open rules.toml in $EDITOR",
		RunE: func(c *cobra.Command, args []string) error {
			if effectiveOutput(rc, rc.cfg) == "json" {
				return usageErr(errors.New("rules edit: interactive subcommand does not support --output json"))
			}
			path, err := rulesFilePath(rc.cfg)
			if err != nil {
				return err
			}
			if err := ensureRulesFileExists(path); err != nil {
				return err
			}
			return openInEditor(path, rc.cfg.Rules.EditorOpenAtRule, 0)
		},
	}
}

func newRulesNewCmd(rc *rootContext) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Append a stub rule to rules.toml and open it in $EDITOR",
		RunE: func(c *cobra.Command, args []string) error {
			if effectiveOutput(rc, rc.cfg) == "json" {
				return usageErr(errors.New("rules new: interactive subcommand does not support --output json"))
			}
			path, err := rulesFilePath(rc.cfg)
			if err != nil {
				return err
			}
			if err := ensureRulesFileExists(path); err != nil {
				return err
			}
			body, err := os.ReadFile(path) // #nosec G304 — path is the user's rules.toml (DefaultPath or --file flag). Single-user desktop tool; the user owns the path. Path-traversal checked in config.Validate.
			if err != nil {
				return err
			}
			// Append a stub rule.
			if name == "" {
				name = "New rule"
			}
			seq := nextSequenceHeuristic(body)
			stub := fmt.Sprintf(`

[[rule]]
name      = %q
sequence  = %d
enabled   = true

  [rule.when]
  # add predicates here, e.g. sender_contains = ["x@"]

  [rule.then]
  mark_read = true
`, name, seq)
			body = append(body, []byte(stub)...)
			if err := rules.AtomicWriteFile(path, body, 0o600); err != nil {
				return err
			}
			return openInEditor(path, rc.cfg.Rules.EditorOpenAtRule, 0)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Display name for the new rule")
	return cmd
}

func nextSequenceHeuristic(body []byte) int {
	// Conservative: scan for `sequence = N` lines and return max+10
	// (matches Outlook's default spacing). Default 10.
	max := 0
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "sequence") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &n); err == nil && n > max {
			max = n
		}
	}
	return max + 10
}

func ensureRulesFileExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	stub := []byte(`# rules.toml — managed by 'inkwell rules pull' and 'inkwell rules apply'.
# Run 'inkwell rules apply --dry-run' before pushing.
# See spec 32 §6.3 for the v1 catalogue.
`)
	return rules.AtomicWriteFile(path, stub, 0o600)
}

func openInEditor(path string, openAtRule bool, line int) error {
	_ = openAtRule
	_ = line
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	args := []string{path}
	cmd := exec.Command(editor, args...) // #nosec G204 G702 — editor is $EDITOR/$VISUAL or "vi" (user-controlled by the operator); path is the validated rules.toml on disk. Intentional shell-out identical to how git/mutt invoke $EDITOR.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func newRulesDeleteCmd(rc *rootContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a rule by ID (synchronous PATCH to server)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			id := args[0]
			prior, err := app.store.GetMessageRule(ctx, app.account.ID, id)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("rules delete: %w", err)
			}
			if !yes && !rc.yes {
				name := id
				if prior != nil {
					name = prior.DisplayName
				}
				fmt.Fprintf(c.OutOrStdout(), "Delete rule %q? [y/N] ", name)
				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				line = strings.TrimSpace(strings.ToLower(line))
				if line != "y" && line != "yes" {
					return errors.New("rules delete: declined by user")
				}
			}
			if err := app.graph.DeleteMessageRule(ctx, id); err != nil {
				return fmt.Errorf("rules delete: %w", err)
			}
			if err := app.store.DeleteMessageRule(ctx, app.account.ID, id); err != nil {
				return fmt.Errorf("rules delete: %w", err)
			}
			name := id
			if prior != nil {
				name = prior.DisplayName
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(struct {
					Deleted bool   `json:"deleted"`
					ID      string `json:"id"`
					Name    string `json:"name"`
				}{true, id, name})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ deleted rule %q\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	return cmd
}

func newRulesEnableCmd(rc *rootContext) *cobra.Command {
	return ruleEnableCmd(rc, true)
}

func newRulesDisableCmd(rc *rootContext) *cobra.Command {
	return ruleEnableCmd(rc, false)
}

func ruleEnableCmd(rc *rootContext, enable bool) *cobra.Command {
	use := "enable <id>"
	short := "Enable a rule (synchronous PATCH to server)"
	if !enable {
		use = "disable <id>"
		short = "Disable a rule (synchronous PATCH to server)"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			id := args[0]
			body, err := json.Marshal(map[string]any{"isEnabled": enable})
			if err != nil {
				return err
			}
			if _, err := app.graph.UpdateMessageRule(ctx, id, body); err != nil {
				return fmt.Errorf("rules toggle: %w", err)
			}
			path, err := rulesFilePath(rc.cfg)
			if err != nil {
				return err
			}
			if _, err := rules.Pull(ctx, app.graph, app.store, app.account.ID, path); err != nil {
				return fmt.Errorf("rules toggle: refresh mirror: %w", err)
			}
			r, err := app.store.GetMessageRule(ctx, app.account.ID, id)
			if err != nil {
				return fmt.Errorf("rules toggle: %w", err)
			}
			label := "enabled"
			if !enable {
				label = "disabled"
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(struct {
					Enabled bool   `json:"enabled"`
					ID      string `json:"id"`
				}{enable, id})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ %s rule %q\n", label, r.DisplayName)
			return nil
		},
	}
}

func newRulesMoveCmd(rc *rootContext) *cobra.Command {
	var seq int
	cmd := &cobra.Command{
		Use:   "move <id> --sequence N",
		Short: "Set the sequence on a rule (synchronous PATCH)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if seq < 0 {
				return usageErr(fmt.Errorf("rules move: --sequence must be >= 0"))
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			id := args[0]
			body, err := json.Marshal(map[string]any{"sequence": seq})
			if err != nil {
				return err
			}
			if _, err := app.graph.UpdateMessageRule(ctx, id, body); err != nil {
				return fmt.Errorf("rules move: %w", err)
			}
			path, err := rulesFilePath(rc.cfg)
			if err != nil {
				return err
			}
			if _, err := rules.Pull(ctx, app.graph, app.store, app.account.ID, path); err != nil {
				return fmt.Errorf("rules move: refresh mirror: %w", err)
			}
			r, err := app.store.GetMessageRule(ctx, app.account.ID, id)
			if err != nil {
				return fmt.Errorf("rules move: %w", err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(struct {
					ID       string `json:"id"`
					Sequence int    `json:"sequence"`
				}{id, seq})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ moved rule %q to sequence %d\n", r.DisplayName, seq)
			return nil
		},
	}
	cmd.Flags().IntVar(&seq, "sequence", -1, "New sequence value (>= 0)")
	_ = cmd.MarkFlagRequired("sequence")
	return cmd
}

// Compile-time interface satisfaction proofs so a refactor of graph
// signatures or rules.GraphClient breaks loudly at build time.
var _ rules.GraphClient = (*graph.Client)(nil)
var _ context.Context = context.Background()
