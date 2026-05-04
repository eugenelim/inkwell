package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func newSettingsCmd(rc *rootContext) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Show mailbox settings",
		RunE: func(c *cobra.Command, _ []string) error {
			return runSettingsShow(c.Context(), rc, output)
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func runSettingsShow(ctx context.Context, rc *rootContext, output string) error {
	gc, err := buildGraphClient(ctx, rc)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, err := gc.GetMailboxSettings(reqCtx)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(s)
	}
	// Text output: human-readable key: value table.
	fmt.Fprintf(os.Stdout, "%-22s  %s\n", "TimeZone:", s.TimeZone)
	fmt.Fprintf(os.Stdout, "%-22s  %s\n", "Language:", s.Language)
	fmt.Fprintf(os.Stdout, "%-22s  %s\n", "DateFormat:", s.DateFormat)
	fmt.Fprintf(os.Stdout, "%-22s  %s\n", "TimeFormat:", s.TimeFormat)
	if s.WorkingHoursDisplay != "" {
		fmt.Fprintf(os.Stdout, "%-22s  %s\n", "WorkingHours:", s.WorkingHoursDisplay)
	}
	fmt.Fprintf(os.Stdout, "%-22s  %s\n", "OOO Status:", s.AutoReplies.Status)
	if s.AutoReplies.ScheduledStart != nil {
		fmt.Fprintf(os.Stdout, "%-22s  %s (%s)\n", "OOO ScheduledStart:",
			s.AutoReplies.ScheduledStart.DateTime, s.AutoReplies.ScheduledStart.TimeZone)
	}
	if s.AutoReplies.ScheduledEnd != nil {
		fmt.Fprintf(os.Stdout, "%-22s  %s (%s)\n", "OOO ScheduledEnd:",
			s.AutoReplies.ScheduledEnd.DateTime, s.AutoReplies.ScheduledEnd.TimeZone)
	}
	if s.AutoReplies.ExternalAudience != "" {
		fmt.Fprintf(os.Stdout, "%-22s  %s\n", "OOO Audience:", s.AutoReplies.ExternalAudience)
	}
	return nil
}
