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
		RunE:  func(c *cobra.Command, _ []string) error { return runOOOShow(c.Context(), rc) },
	}
	on := &cobra.Command{
		Use:   "on",
		Short: "Enable automatic replies (alwaysEnabled)",
		RunE:  func(c *cobra.Command, _ []string) error { return runOOOEnable(c.Context(), rc, true) },
	}
	off := &cobra.Command{
		Use:   "off",
		Short: "Disable automatic replies",
		RunE:  func(c *cobra.Command, _ []string) error { return runOOOEnable(c.Context(), rc, false) },
	}
	cmd.AddCommand(on, off)
	return cmd
}

func runOOOShow(ctx context.Context, rc *rootContext) error {
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
	return json.NewEncoder(os.Stdout).Encode(s)
}

func runOOOEnable(ctx context.Context, rc *rootContext, enable bool) error {
	gc, err := buildGraphClient(ctx, rc)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Fetch current state to preserve messages.
	cur, err := gc.GetMailboxSettings(reqCtx)
	if err != nil {
		return fmt.Errorf("ooo: fetch current: %w", err)
	}
	status := graph.AutoReplyDisabled
	if enable {
		status = graph.AutoReplyAlwaysEnabled
	}
	return gc.UpdateAutoReplies(reqCtx, graph.AutoRepliesSetting{
		Status:               status,
		InternalReplyMessage: cur.AutoReplies.InternalReplyMessage,
		ExternalReplyMessage: cur.AutoReplies.ExternalReplyMessage,
		ExternalAudience:     cur.AutoReplies.ExternalAudience,
	})
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
