package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	isync "github.com/eugenelim/inkwell/internal/sync"
)

func newDaemonCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the sync engine in a loop (no TUI)",
		Long: `Runs the sync engine indefinitely, printing a status line on each
sync cycle. Blocks until SIGINT or SIGTERM. Useful for running inkwell
as a background service.

  inkwell daemon`,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(c.Context())
			defer cancel()

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

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGTERM, os.Interrupt)
			defer signal.Stop(sigCh)

			if err := eng.Start(ctx); err != nil {
				return fmt.Errorf("daemon: start: %w", err)
			}
			fmt.Fprintf(os.Stderr, "daemon started — press Ctrl-C to stop\n")

			notifs := eng.Notifications()
			for {
				select {
				case ev, ok := <-notifs:
					if !ok {
						return nil
					}
					if !rc.quiet {
						fmt.Fprintf(os.Stderr, "sync event: %T\n", ev)
					}
				case <-eng.Done():
					return nil
				case sig := <-sigCh:
					fmt.Fprintf(os.Stderr, "\ncaught %s — stopping daemon\n", sig)
					// ctx may already be cancelled here; use Background for the graceful stop.
					stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer stopCancel()
					_ = eng.Stop(stopCtx)
					return nil
				}
			}
		},
	}
}
