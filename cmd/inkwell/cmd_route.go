package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/store"
)

// newRouteCmd returns the `inkwell route` parent command. Spec 23 §7.
// Subcommands: assign / clear / list / show. Bare-address validation
// rejects display-name forms (`"Bob" <bob@…>`) — strict input is
// preferable to silent extraction.
func newRouteCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "route",
		Short: "Manage per-sender routing destinations (Imbox / Feed / Paper Trail / Screener)",
		Long: `Routing assigns a sender's mail to one of four streams. Past and
future mail from a routed sender appears in the matching virtual
folder; the assignment is local-only and does not call Graph.

Examples:
  inkwell route assign news@example.com feed
  inkwell route assign aws-billing@amazon.com paper_trail
  inkwell route clear news@example.com
  inkwell route list
  inkwell route list --destination feed
  inkwell route show news@example.com`,
	}
	cmd.AddCommand(newRouteAssignCmd(rc))
	cmd.AddCommand(newRouteClearCmd(rc))
	cmd.AddCommand(newRouteListCmd(rc))
	cmd.AddCommand(newRouteShowCmd(rc))
	return cmd
}

func newRouteAssignCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign <address> <destination>",
		Short: "Assign a sender to a routing destination",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			addr := args[0]
			dest := args[1]
			if err := validateBareAddress(addr); err != nil {
				return usageErr(err)
			}
			if !isValidDestination(dest) {
				return usageErr(fmt.Errorf(`route: unknown destination %q; expected one of imbox, feed, paper_trail, screener`, dest))
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			prior, err := app.store.SetSenderRouting(ctx, app.account.ID, addr, dest)
			if err != nil {
				if errors.Is(err, store.ErrInvalidAddress) {
					return usageErr(err)
				}
				if errors.Is(err, store.ErrInvalidDestination) {
					return usageErr(err)
				}
				return fmt.Errorf("route assign: %w", err)
			}
			normalised := store.NormalizeEmail(addr)
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Address     string `json:"address"`
					Destination string `json:"destination"`
					Prior       string `json:"prior,omitempty"`
				}{normalised, dest, prior})
			}
			out := fmt.Sprintf("✓ routed %s → %s", normalised, dest)
			if prior != "" && prior != dest {
				out += " (was " + prior + ")"
			}
			fmt.Fprintln(c.OutOrStdout(), out)
			return nil
		},
	}
	return cmd
}

func newRouteClearCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear <address>",
		Short: "Clear routing for a sender (returns them to the unrouted state)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			addr := args[0]
			if err := validateBareAddress(addr); err != nil {
				return usageErr(err)
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			prior, err := app.store.ClearSenderRouting(ctx, app.account.ID, addr)
			if err != nil {
				if errors.Is(err, store.ErrInvalidAddress) {
					return usageErr(err)
				}
				return fmt.Errorf("route clear: %w", err)
			}
			normalised := store.NormalizeEmail(addr)
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Address string `json:"address"`
					Cleared bool   `json:"cleared"`
					Prior   string `json:"prior,omitempty"`
				}{normalised, prior != "", prior})
			}
			if prior == "" {
				fmt.Fprintf(c.OutOrStdout(), "%s is not routed\n", normalised)
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ cleared routing for %s\n", normalised)
			return nil
		},
	}
	return cmd
}

func newRouteListCmd(rc *rootContext) *cobra.Command {
	var destination string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all routings, optionally filtered by destination",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if destination != "" && !isValidDestination(destination) {
				return usageErr(fmt.Errorf(`route: unknown destination %q; expected one of imbox, feed, paper_trail, screener`, destination))
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			rows, err := app.store.ListSenderRoutings(ctx, app.account.ID, destination)
			if err != nil {
				return fmt.Errorf("route list: %w", err)
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				type row struct {
					Address     string    `json:"address"`
					Destination string    `json:"destination"`
					AddedAt     time.Time `json:"added_at"`
				}
				out := make([]row, 0, len(rows))
				for _, r := range rows {
					out = append(out, row{r.EmailAddress, r.Destination, r.AddedAt})
				}
				return json.NewEncoder(os.Stdout).Encode(out)
			}
			if len(rows) == 0 {
				fmt.Fprintln(c.OutOrStdout(), "(no routings)")
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "%-12s  %-40s  %s\n", "DESTINATION", "ADDRESS", "ADDED")
			for _, r := range rows {
				fmt.Fprintf(c.OutOrStdout(), "%-12s  %-40s  %s\n", r.Destination, r.EmailAddress, r.AddedAt.Format("2006-01-02 15:04"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&destination, "destination", "", "filter by destination (imbox|feed|paper_trail|screener)")
	return cmd
}

func newRouteShowCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <address>",
		Short: "Show the routing for one sender",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			addr := args[0]
			if err := validateBareAddress(addr); err != nil {
				return usageErr(err)
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			dest, err := app.store.GetSenderRouting(ctx, app.account.ID, addr)
			if err != nil {
				return fmt.Errorf("route show: %w", err)
			}
			normalised := store.NormalizeEmail(addr)
			if effectiveOutput(rc, rc.cfg) == "json" {
				type out struct {
					Address     string  `json:"address"`
					Destination *string `json:"destination"`
				}
				var d *string
				if dest != "" {
					d = &dest
				}
				return json.NewEncoder(os.Stdout).Encode(out{normalised, d})
			}
			if dest == "" {
				fmt.Fprintf(c.OutOrStdout(), "%s is not routed\n", normalised)
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "%s → %s\n", normalised, dest)
			return nil
		},
	}
	return cmd
}

// isValidDestination reports whether dest is one of the four allowed
// strings. Mirrors store.validRoutingDestination but lives here so
// the CLI can validate before opening the database.
func isValidDestination(dest string) bool {
	switch dest {
	case "imbox", "feed", "paper_trail", "screener":
		return true
	}
	return false
}

// validateBareAddress rejects display-name forms ("Bob" <bob@…>) and
// empty / whitespace-only strings. Strict input is preferable to
// silent extraction (spec 23 §7).
func validateBareAddress(addr string) error {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return fmt.Errorf("route: address is empty")
	}
	if strings.ContainsAny(trimmed, "<>\"") {
		return fmt.Errorf("route: address must be bare; got %q", addr)
	}
	if !strings.Contains(trimmed, "@") {
		return fmt.Errorf("route: address must contain '@'; got %q", addr)
	}
	return nil
}

// usageErr is a sentinel cobra wraps to exit with code 2 (per spec 14
// "exit codes": 0 success, 2 usage error). cobra's RunE does not have
// a built-in usage-vs-runtime distinction, so we tag the returned
// error and the caller's main inspects it via errors.Is.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func usageErr(err error) error { return &usageError{err: err} }
