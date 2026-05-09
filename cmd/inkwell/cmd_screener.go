package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/store"
)

// stdinIsTTY returns true when stdin is connected to a terminal
// (no pipe / redirect). Pure stdlib via os.Stdin.Stat() — keeps the
// no-CGO discipline and avoids adding golang.org/x/term as a new
// dependency.
func stdinIsTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// newScreenerCmd registers `inkwell screener {list,accept,reject,history,pre-approve,status}`
// per spec 28 §7. Subcommands share the spec 23 bare-address
// validation; pre-approve adds CRLF / BOM / # comment / blank-line
// handling for stdin / file input.
func newScreenerCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "screener",
		Short: "Inspect and act on the Screener queue (spec 28)",
		Long: `The Screener is the first-contact gate. Senders without a routing
decision are Pending; senders routed to 'screener' are Screened
Out. The list / accept / reject / history / pre-approve / status
verbs mirror the TUI verbs and are local-only (no Graph calls).

Examples:
  inkwell screener list
  inkwell screener list --grouping message
  inkwell screener accept news@example.com
  inkwell screener accept news@example.com --to feed
  inkwell screener reject news@example.com
  inkwell screener history
  inkwell screener pre-approve --from-stdin < contacts.txt
  inkwell screener pre-approve --from-file ~/contacts.txt
  inkwell screener status`,
	}
	cmd.AddCommand(newScreenerListCmd(rc))
	cmd.AddCommand(newScreenerAcceptCmd(rc))
	cmd.AddCommand(newScreenerRejectCmd(rc))
	cmd.AddCommand(newScreenerHistoryCmd(rc))
	cmd.AddCommand(newScreenerPreApproveCmd(rc))
	cmd.AddCommand(newScreenerStatusCmd(rc))
	return cmd
}

func newScreenerListCmd(rc *rootContext) *cobra.Command {
	var grouping string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Pending senders (or messages with --grouping=message)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if grouping != "sender" && grouping != "message" {
				return usageErr(fmt.Errorf(`screener list: --grouping must be "sender" or "message"; got %q`, grouping))
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			cap := rc.cfg.Screener.MaxCountPerSender
			if cap <= 0 {
				cap = 999
			}
			excludeMuted := rc.cfg.Screener.ExcludeMuted
			if grouping == "message" {
				msgs, err := app.store.ListPendingMessages(ctx, app.account.ID, limit, excludeMuted)
				if err != nil {
					return fmt.Errorf("screener list: %w", err)
				}
				if effectiveOutput(rc, rc.cfg) == "json" {
					return json.NewEncoder(c.OutOrStdout()).Encode(msgs)
				}
				w := tabwriter.NewWriter(c.OutOrStdout(), 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "ADDRESS\tDISPLAY\tSUBJECT\tRECEIVED")
				for _, mg := range msgs {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
						mg.FromAddress, truncCLI(mg.FromName, 24), truncCLI(mg.Subject, 60), mg.ReceivedAt.Format(time.RFC3339))
				}
				return w.Flush()
			}
			rows, err := app.store.ListPendingSenders(ctx, app.account.ID, limit, cap, excludeMuted)
			if err != nil {
				return fmt.Errorf("screener list: %w", err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(rows)
			}
			w := tabwriter.NewWriter(c.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ADDRESS\tDISPLAY\tCOUNT\tLATEST\tSUBJECT")
			for _, ps := range rows {
				count := fmt.Sprintf("%d", ps.MessageCount)
				if ps.MessageCount > cap {
					count = fmt.Sprintf("%d+", cap)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					ps.EmailAddress, truncCLI(ps.DisplayName, 24), count,
					ps.LatestReceived.Format(time.RFC3339), truncCLI(ps.LatestSubject, 60))
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&grouping, "grouping", "sender", "list grouping: sender (default) or message")
	cmd.Flags().IntVar(&limit, "limit", 200, "max rows returned")
	return cmd
}

func newScreenerAcceptCmd(rc *rootContext) *cobra.Command {
	var dest string
	cmd := &cobra.Command{
		Use:   "accept <address>",
		Short: "Admit a sender (alias for `route assign <address> imbox`)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := validateBareAddress(args[0]); err != nil {
				return usageErr(fmt.Errorf("screener accept: %w", err))
			}
			switch dest {
			case "imbox", "feed", "paper_trail":
			case "screener":
				return usageErr(fmt.Errorf("screener accept: --to=screener rejected; use `inkwell screener reject` for screening-out"))
			default:
				return usageErr(fmt.Errorf("screener accept: unknown destination %q (imbox|feed|paper_trail)", dest))
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			prior, err := app.store.SetSenderRouting(ctx, app.account.ID, args[0], dest)
			if err != nil {
				if errors.Is(err, store.ErrInvalidAddress) {
					return usageErr(err)
				}
				return fmt.Errorf("screener accept: %w", err)
			}
			addr := store.NormalizeEmail(args[0])
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(struct {
					Address     string `json:"address"`
					Destination string `json:"destination"`
					Prior       string `json:"prior,omitempty"`
				}{addr, dest, prior})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ admitted %s → %s\n", addr, dest)
			return nil
		},
	}
	cmd.Flags().StringVar(&dest, "to", "imbox", "destination: imbox | feed | paper_trail")
	return cmd
}

func newScreenerRejectCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reject <address>",
		Short: "Screen out a sender (alias for `route assign <address> screener`)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := validateBareAddress(args[0]); err != nil {
				return usageErr(fmt.Errorf("screener reject: %w", err))
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			prior, err := app.store.SetSenderRouting(ctx, app.account.ID, args[0], "screener")
			if err != nil {
				if errors.Is(err, store.ErrInvalidAddress) {
					return usageErr(err)
				}
				return fmt.Errorf("screener reject: %w", err)
			}
			addr := store.NormalizeEmail(args[0])
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(struct {
					Address     string `json:"address"`
					Destination string `json:"destination"`
					Prior       string `json:"prior,omitempty"`
				}{addr, "screener", prior})
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ screened out %s\n", addr)
			return nil
		},
	}
	return cmd
}

func newScreenerHistoryCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "List Screened-Out senders",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			rows, err := app.store.ListSenderRoutings(ctx, app.account.ID, "screener")
			if err != nil {
				return fmt.Errorf("screener history: %w", err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(rows)
			}
			w := tabwriter.NewWriter(c.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ADDRESS\tADDED-AT")
			for _, sr := range rows {
				fmt.Fprintf(w, "%s\t%s\n", sr.EmailAddress, sr.AddedAt.Format(time.RFC3339))
			}
			return w.Flush()
		},
	}
	return cmd
}

func newScreenerPreApproveCmd(rc *rootContext) *cobra.Command {
	var fromStdin bool
	var fromFile string
	var dest string
	cmd := &cobra.Command{
		Use:   "pre-approve",
		Short: "Bulk-admit senders from stdin or a file (one address per line)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if fromStdin && fromFile != "" {
				return usageErr(fmt.Errorf("pre-approve: --from-stdin and --from-file are mutually exclusive"))
			}
			if !fromStdin && fromFile == "" {
				return usageErr(fmt.Errorf("pre-approve: one of --from-stdin or --from-file is required"))
			}
			switch dest {
			case "imbox", "feed", "paper_trail":
			case "screener":
				return usageErr(fmt.Errorf("pre-approve: --to: invalid destination \"screener\"; use 'inkwell screener reject' for screening-out"))
			default:
				return usageErr(fmt.Errorf("pre-approve: unknown destination %q (imbox|feed|paper_trail)", dest))
			}
			var src io.Reader
			if fromStdin {
				if stdinIsTTY() {
					return usageErr(fmt.Errorf("pre-approve: --from-stdin requires a non-tty stdin (use a pipe or file redirect)"))
				}
				src = os.Stdin
			} else {
				f, err := os.Open(fromFile)
				if err != nil {
					return usageErr(fmt.Errorf("pre-approve: open %s: %w", fromFile, err))
				}
				defer f.Close()
				src = f
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			admitted, errs := preApproveStream(ctx, app, src, dest)
			if effectiveOutput(rc, rc.cfg) == "json" {
				out := struct {
					Approved int      `json:"approved"`
					Skipped  int      `json:"skipped"`
					Errors   []string `json:"errors,omitempty"`
				}{admitted, len(errs), errsToStrings(errs)}
				if err := json.NewEncoder(c.OutOrStdout()).Encode(out); err != nil {
					return err
				}
			} else {
				for _, e := range errs {
					fmt.Fprintln(c.ErrOrStderr(), e.Error())
				}
				if admitted > 0 {
					fmt.Fprintf(c.OutOrStdout(), "✓ pre-approved %d senders to %s\n", admitted, dest)
				}
			}
			if admitted == 0 && len(errs) > 0 {
				os.Exit(2)
			}
			if admitted == 0 {
				fmt.Fprintln(c.ErrOrStderr(), "pre-approve: 0 admitted (no addresses in input)")
			} else if len(errs) > 0 {
				fmt.Fprintf(c.ErrOrStderr(), "pre-approve: %d admitted, %d skipped (errors above)\n", admitted, len(errs))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "from-stdin", false, "read addresses from stdin (one per line)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "read addresses from a file (one per line)")
	cmd.Flags().StringVar(&dest, "to", "imbox", "destination: imbox | feed | paper_trail")
	return cmd
}

func newScreenerStatusCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print the screener configuration state",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			pending, _ := app.store.CountPendingSenders(ctx, app.account.ID, rc.cfg.Screener.ExcludeMuted)
			screened, _ := app.store.CountScreenedOutMessages(ctx, app.account.ID, rc.cfg.Screener.ExcludeMuted)
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(struct {
					Enabled       bool   `json:"enabled"`
					Grouping      string `json:"grouping"`
					ExcludeMuted  bool   `json:"exclude_muted"`
					PendingCount  int    `json:"pending_count"`
					ScreenedCount int    `json:"screened_count"`
				}{rc.cfg.Screener.Enabled, rc.cfg.Screener.Grouping, rc.cfg.Screener.ExcludeMuted, pending, screened})
			}
			fmt.Fprintf(c.OutOrStdout(), "screener: enabled=%v grouping=%s exclude_muted=%v pending=%d screened=%d\n",
				rc.cfg.Screener.Enabled, rc.cfg.Screener.Grouping, rc.cfg.Screener.ExcludeMuted, pending, screened)
			return nil
		},
	}
	return cmd
}

// preApproveStream parses stdin/file lines (CRLF-tolerant, BOM-safe,
// blank/`#` skip) and applies SetSenderRouting for each parseable
// bare address. Returns (admitted, errs). The caller renders text
// or JSON; this helper is shape-only.
func preApproveStream(ctx context.Context, app *headlessApp, src io.Reader, dest string) (int, []error) {
	var (
		admitted int
		errs     []error
		bomSeen  bool
	)
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		if !bomSeen && strings.HasPrefix(raw, "\ufeff") {
			raw = strings.TrimPrefix(raw, "\ufeff")
			bomSeen = true
		}
		raw = strings.TrimRight(raw, "\r")
		trimmed := strings.TrimFunc(raw, unicode.IsSpace)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if err := validateBareAddress(trimmed); err != nil {
			errs = append(errs, fmt.Errorf("pre-approve: line %d: address must be bare; got %q (strip the display name and angle brackets, keep just the address)", lineNo, trimmed))
			continue
		}
		if _, err := app.store.SetSenderRouting(ctx, app.account.ID, trimmed, dest); err != nil {
			errs = append(errs, fmt.Errorf("pre-approve: line %d: %w", lineNo, err))
			continue
		}
		admitted++
	}
	if err := scanner.Err(); err != nil {
		errs = append(errs, fmt.Errorf("pre-approve: read input: %w", err))
	}
	return admitted, errs
}

func errsToStrings(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}
