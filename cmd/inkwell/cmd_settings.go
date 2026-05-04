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
	return &cobra.Command{
		Use:   "settings",
		Short: "Show mailbox settings as JSON",
		RunE:  func(c *cobra.Command, _ []string) error { return runSettingsShow(c.Context(), rc) },
	}
}

func runSettingsShow(ctx context.Context, rc *rootContext) error {
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
	return json.NewEncoder(os.Stdout).Encode(s)
}
