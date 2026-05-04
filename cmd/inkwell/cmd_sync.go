package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	isync "github.com/eugenelim/inkwell/internal/sync"
)

func newSyncCmd(rc *rootContext) *cobra.Command {
	var (
		output string
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Run one sync cycle now and exit",
		Long: `Forces a single sync cycle: enumerate folders, drain the action
queue, then run a per-folder pull for every subscribed folder.
Blocks until done. Subsequent ` + "`inkwell run`" + ` (or any other
command that reads the cache) will see the updated state.

Examples:
  inkwell sync
  inkwell sync --output json`,
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
			eng, err := isync.New(app.graph, app.store, nil, isync.Options{
				AccountID:          app.account.ID,
				Logger:             app.logger,
				ForegroundInterval: cfg.Sync.ForegroundInterval,
				BackgroundInterval: cfg.Sync.BackgroundInterval,
			})
			if err != nil {
				return fmt.Errorf("sync engine: %w", err)
			}
			start := time.Now()
			if err := eng.SyncAll(ctx); err != nil {
				return fmt.Errorf("sync: %w", err)
			}
			dur := time.Since(start)
			if output == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					DurationMs int64 `json:"durationMs"`
				}{dur.Milliseconds()})
			}
			fmt.Fprintf(os.Stdout, "✓ synced in %s\n", dur.Round(time.Millisecond))
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func newBackfillCmd(rc *rootContext) *cobra.Command {
	var folder, until string
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Backfill message history for a folder to a specified date",
		Long: `Extend the local cache backwards in time for a given folder.

Examples:
  inkwell backfill --folder Inbox --until 2025-01-01`,
		RunE: func(c *cobra.Command, _ []string) error {
			if folder == "" {
				return fmt.Errorf("--folder is required")
			}
			if until == "" {
				return fmt.Errorf("--until is required (YYYY-MM-DD)")
			}
			untilTime, err := time.Parse("2006-01-02", until)
			if err != nil {
				return fmt.Errorf("--until: cannot parse %q (use YYYY-MM-DD)", until)
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			folderID, err := resolveFolder(ctx, app, folder)
			if err != nil {
				return err
			}
			cfg, err := rc.loadConfig()
			if err != nil {
				return err
			}
			eng, err := isync.New(app.graph, app.store, nil, isync.Options{
				AccountID:          app.account.ID,
				Logger:             app.logger,
				ForegroundInterval: cfg.Sync.ForegroundInterval,
				BackgroundInterval: cfg.Sync.BackgroundInterval,
			})
			if err != nil {
				return fmt.Errorf("sync engine: %w", err)
			}
			fmt.Fprintf(os.Stderr, "backfilling %q to %s…\n", folder, until)
			start := time.Now()
			if err := eng.Backfill(ctx, folderID, untilTime); err != nil {
				return fmt.Errorf("backfill: %w", err)
			}
			fmt.Fprintf(os.Stdout, "✓ backfilled %q in %s\n", folder, time.Since(start).Round(time.Millisecond))
			return nil
		},
	}
	cmd.Flags().StringVar(&folder, "folder", "", "folder name (required)")
	cmd.Flags().StringVar(&until, "until", "", "backfill to this date, YYYY-MM-DD (required)")
	return cmd
}
