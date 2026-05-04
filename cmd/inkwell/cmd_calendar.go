package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/graph"
)

func newCalendarCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "Show calendar events",
		Long: `Query calendar events from Microsoft Graph.

Examples:
  inkwell calendar today
  inkwell calendar week
  inkwell calendar agenda --days 14
  inkwell calendar show <event-id>`,
	}
	cmd.AddCommand(newCalendarTodayCmd(rc))
	cmd.AddCommand(newCalendarWeekCmd(rc))
	cmd.AddCommand(newCalendarAgendaCmd(rc))
	cmd.AddCommand(newCalendarShowCmd(rc))
	return cmd
}

func newCalendarTodayCmd(rc *rootContext) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "today",
		Short: "Show events for today",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			now := time.Now()
			y, m, d := now.Date()
			start := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
			end := start.Add(24 * time.Hour)
			reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			events, err := app.graph.ListEventsBetween(reqCtx, start, end)
			if err != nil {
				return fmt.Errorf("calendar today: %w", err)
			}
			return printEvents(events, output)
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func newCalendarWeekCmd(rc *rootContext) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "week",
		Short: "Show events for the current week (Mon–Sun)",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			start, end := currentWeekBounds(time.Now())
			reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			events, err := app.graph.ListEventsBetween(reqCtx, start, end)
			if err != nil {
				return fmt.Errorf("calendar week: %w", err)
			}
			return printEvents(events, output)
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func newCalendarAgendaCmd(rc *rootContext) *cobra.Command {
	var output string
	var days int
	cmd := &cobra.Command{
		Use:   "agenda",
		Short: "Show agenda for the next N days",
		RunE: func(c *cobra.Command, _ []string) error {
			if days <= 0 {
				days = 7
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			now := time.Now()
			end := now.Add(time.Duration(days) * 24 * time.Hour)
			reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			events, err := app.graph.ListEventsBetween(reqCtx, now, end)
			if err != nil {
				return fmt.Errorf("calendar agenda: %w", err)
			}
			return printEvents(events, output)
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	cmd.Flags().IntVar(&days, "days", 7, "number of days ahead to show")
	return cmd
}

func newCalendarShowCmd(rc *rootContext) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "show <event-id>",
		Short: "Show details for a single event including attendees",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			det, err := app.graph.GetEvent(reqCtx, args[0])
			if err != nil {
				return fmt.Errorf("calendar show: %w", err)
			}
			if output == "json" {
				return json.NewEncoder(os.Stdout).Encode(det)
			}
			printEventDetail(det)
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func printEvents(events []graph.Event, output string) error {
	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(events)
	}
	if len(events) == 0 {
		fmt.Fprintln(os.Stdout, "(no events)")
		return nil
	}
	fmt.Fprintf(os.Stdout, "%-20s %-20s  %-40s  %s\n", "START", "END", "SUBJECT", "LOCATION")
	for _, e := range events {
		startStr := formatEventTime(e.Start, e.IsAllDay)
		endStr := formatEventTime(e.End, e.IsAllDay)
		fmt.Fprintf(os.Stdout, "%-20s %-20s  %-40s  %s\n",
			startStr, endStr, truncCLI(e.Subject, 40), e.Location)
	}
	return nil
}

func printEventDetail(det graph.EventDetail) {
	fmt.Fprintf(os.Stdout, "Subject:    %s\n", det.Subject)
	fmt.Fprintf(os.Stdout, "Organizer:  %s <%s>\n", det.OrganizerName, det.OrganizerAddress)
	if det.IsAllDay {
		fmt.Fprintf(os.Stdout, "Start:      %s (all day)\n", det.Start.Format("Mon 2006-01-02"))
	} else {
		fmt.Fprintf(os.Stdout, "Start:      %s\n", det.Start.Format("Mon 2006-01-02 15:04"))
		fmt.Fprintf(os.Stdout, "End:        %s\n", det.End.Format("Mon 2006-01-02 15:04"))
	}
	if det.Location != "" {
		fmt.Fprintf(os.Stdout, "Location:   %s\n", det.Location)
	}
	if det.OnlineMeetingURL != "" {
		fmt.Fprintf(os.Stdout, "Join URL:   %s\n", det.OnlineMeetingURL)
	}
	if det.BodyPreview != "" {
		fmt.Fprintf(os.Stdout, "Preview:    %s\n", det.BodyPreview)
	}
	if len(det.Attendees) > 0 {
		fmt.Fprintf(os.Stdout, "Attendees:\n")
		for _, a := range det.Attendees {
			fmt.Fprintf(os.Stdout, "  %s <%s>  type=%s  status=%s\n",
				a.Name, a.Address, a.Type, a.Status)
		}
	}
}

func formatEventTime(t time.Time, allDay bool) string {
	if t.IsZero() {
		return ""
	}
	if allDay {
		return t.Local().Format("2006-01-02")
	}
	return t.Local().Format("01-02 15:04")
}

// currentWeekBounds returns [Monday 00:00, next Monday 00:00) for the
// week that contains t.
func currentWeekBounds(t time.Time) (start, end time.Time) {
	y, m, d := t.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	weekday := int(today.Weekday())
	// Sunday = 0; shift so Monday = 0.
	offset := (weekday + 6) % 7
	start = today.AddDate(0, 0, -offset)
	end = start.AddDate(0, 0, 7)
	return start, end
}
