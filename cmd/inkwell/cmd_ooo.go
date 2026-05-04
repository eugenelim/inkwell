package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/auth"
	"github.com/eugenelim/inkwell/internal/graph"
)

func newOOOCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ooo",
		Short: "Show or set out-of-office automatic replies",
		RunE: func(c *cobra.Command, _ []string) error {
			output, _ := c.Flags().GetString("output")
			return runOOOShow(c.Context(), rc, output)
		},
	}
	cmd.Flags().String("output", "text", "output format: text|json")

	on := newOOOOnCmd(rc)
	off := &cobra.Command{
		Use:   "off",
		Short: "Disable automatic replies",
		RunE:  func(c *cobra.Command, _ []string) error { return runOOOEnable(c.Context(), rc, false, "") },
	}
	set := newOOOSetCmd(rc)
	cmd.AddCommand(on, off, set)
	return cmd
}

func newOOOOnCmd(rc *rootContext) *cobra.Command {
	var until string
	cmd := &cobra.Command{
		Use:   "on",
		Short: "Enable automatic replies (alwaysEnabled, or scheduled with --until)",
		RunE: func(c *cobra.Command, _ []string) error {
			return runOOOEnable(c.Context(), rc, true, until)
		},
	}
	cmd.Flags().StringVar(&until, "until", "", "schedule end date/time (YYYY-MM-DD or RFC3339); enables scheduled mode")
	return cmd
}

func newOOOSetCmd(rc *rootContext) *cobra.Command {
	var internal, external, audience string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Update OOO message text without changing status",
		RunE: func(c *cobra.Command, _ []string) error {
			gc, err := buildGraphClient(c.Context(), rc)
			if err != nil {
				return err
			}
			reqCtx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
			defer cancel()
			cur, err := gc.GetMailboxSettings(reqCtx)
			if err != nil {
				return fmt.Errorf("ooo set: fetch current: %w", err)
			}
			if internal != "" {
				cur.AutoReplies.InternalReplyMessage = internal
			}
			if external != "" {
				cur.AutoReplies.ExternalReplyMessage = external
			}
			if audience != "" {
				cur.AutoReplies.ExternalAudience = audience
			}
			return gc.UpdateAutoReplies(reqCtx, cur.AutoReplies)
		},
	}
	cmd.Flags().StringVar(&internal, "internal", "", "internal reply message")
	cmd.Flags().StringVar(&external, "external", "", "external reply message")
	cmd.Flags().StringVar(&audience, "audience", "", "external audience: all|contactsOnly|none")
	return cmd
}

func runOOOShow(ctx context.Context, rc *rootContext, output string) error {
	gc, err := buildGraphClient(ctx, rc)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, err := gc.GetMailboxSettings(reqCtx)
	if err != nil {
		return fmt.Errorf("ooo: %w", err)
	}
	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(s)
	}
	// Text output.
	fmt.Fprintf(os.Stdout, "Status:    %s\n", s.AutoReplies.Status)
	if s.AutoReplies.ScheduledStart != nil {
		fmt.Fprintf(os.Stdout, "Start:     %s (%s)\n",
			s.AutoReplies.ScheduledStart.DateTime, s.AutoReplies.ScheduledStart.TimeZone)
	}
	if s.AutoReplies.ScheduledEnd != nil {
		fmt.Fprintf(os.Stdout, "End:       %s (%s)\n",
			s.AutoReplies.ScheduledEnd.DateTime, s.AutoReplies.ScheduledEnd.TimeZone)
	}
	fmt.Fprintf(os.Stdout, "Audience:  %s\n", s.AutoReplies.ExternalAudience)
	if s.AutoReplies.InternalReplyMessage != "" {
		fmt.Fprintf(os.Stdout, "Internal:  %s\n", s.AutoReplies.InternalReplyMessage)
	}
	if s.AutoReplies.ExternalReplyMessage != "" {
		fmt.Fprintf(os.Stdout, "External:  %s\n", s.AutoReplies.ExternalReplyMessage)
	}
	return nil
}

func runOOOEnable(ctx context.Context, rc *rootContext, enable bool, until string) error {
	gc, err := buildGraphClient(ctx, rc)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cur, err := gc.GetMailboxSettings(reqCtx)
	if err != nil {
		return fmt.Errorf("ooo: fetch current: %w", err)
	}

	setting := graph.AutoRepliesSetting{
		InternalReplyMessage: cur.AutoReplies.InternalReplyMessage,
		ExternalReplyMessage: cur.AutoReplies.ExternalReplyMessage,
		ExternalAudience:     cur.AutoReplies.ExternalAudience,
	}

	if !enable {
		setting.Status = graph.AutoReplyDisabled
		return gc.UpdateAutoReplies(reqCtx, setting)
	}

	if until != "" {
		// Parse the --until value (YYYY-MM-DD or RFC3339).
		end, err := parseUntilDate(until)
		if err != nil {
			return fmt.Errorf("ooo: --until: %w", err)
		}
		setting.Status = graph.AutoReplyScheduled
		now := time.Now()
		tz := now.Location().String()
		setting.ScheduledStart = &graph.DateTimeTimeZone{
			DateTime: now.Format("2006-01-02T15:04:05"),
			TimeZone: tz,
		}
		setting.ScheduledEnd = &graph.DateTimeTimeZone{
			DateTime: end.Format("2006-01-02T15:04:05"),
			TimeZone: tz,
		}
	} else {
		setting.Status = graph.AutoReplyAlwaysEnabled
	}

	return gc.UpdateAutoReplies(reqCtx, setting)
}

// parseUntilDate accepts YYYY-MM-DD or RFC3339 and returns a time.Time.
func parseUntilDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		// End of that day in local time.
		y, m, d := t.Date()
		return time.Date(y, m, d, 23, 59, 59, 0, time.Local), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("cannot parse %q (use YYYY-MM-DD or RFC3339)", s)
}

// buildGraphClient constructs a lightweight graph.Client for CLI subcommands.
func buildGraphClient(ctx context.Context, rc *rootContext) (*graph.Client, error) {
	cfg, err := rc.loadConfig()
	if err != nil {
		return nil, err
	}
	mode, err := auth.ParseSignInMode(cfg.Account.SignInMode)
	if err != nil {
		return nil, err
	}
	a, err := auth.New(auth.Config{
		TenantID:             cfg.Account.TenantID,
		ClientID:             cfg.Account.ClientID,
		ExpectedUPN:          cfg.Account.UPN,
		Mode:                 mode,
		RequestOfflineAccess: cfg.Account.RequestOfflineAccess,
	}, promptDeviceCode(os.Stderr))
	if err != nil {
		return nil, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !a.IsSignedIn(probeCtx) {
		return nil, errors.New("not signed in — run `inkwell signin` first")
	}
	return graph.NewClient(a, graph.Options{MaxConcurrent: 4, MaxRetries: 3})
}
