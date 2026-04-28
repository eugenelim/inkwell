package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/auth"
)

// promptDeviceCode writes the user-facing instructions to stderr in a
// grep-friendly format. CLI subcommands use this; the TUI registers its
// own modal-style prompt.
func promptDeviceCode(w io.Writer) auth.PromptFn {
	return func(_ context.Context, p auth.DeviceCodePrompt) error {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Sign in to Microsoft 365:")
		fmt.Fprintf(w, "  user_code:        %s\n", p.UserCode)
		fmt.Fprintf(w, "  verification_url: %s\n", p.VerificationURL)
		fmt.Fprintf(w, "  expires_at:       %s\n", p.ExpiresAt.Format(time.RFC3339))
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Open the URL in a browser, paste the user_code, then return here.")
		return nil
	}
}

func newAuthenticator(rc *rootContext) (auth.Authenticator, error) {
	cfg, err := rc.loadConfig()
	if err != nil {
		return nil, err
	}
	return auth.New(auth.Config{
		TenantID: cfg.Account.TenantID,
		ClientID: cfg.Account.ClientID,
	}, promptDeviceCode(os.Stderr))
}

func runSignin(cmd *cobra.Command, rc *rootContext) error {
	a, err := newAuthenticator(rc)
	if err != nil {
		return err
	}
	a.Invalidate()
	if _, err := a.Token(cmd.Context()); err != nil {
		return fmt.Errorf("signin: %w", err)
	}
	upn, tenant, _ := a.Account()
	fmt.Fprintf(cmd.OutOrStdout(), "signed in as %s (tenant %s)\n", upn, tenant)
	return nil
}

func runSignout(cmd *cobra.Command, rc *rootContext) error {
	a, err := newAuthenticator(rc)
	if err != nil {
		return err
	}
	if !confirm(cmd, "clear cached credentials?") {
		return nil
	}
	if err := a.SignOut(cmd.Context()); err != nil {
		return fmt.Errorf("signout: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "signed out")
	return nil
}

func runWhoami(cmd *cobra.Command, rc *rootContext) error {
	cfg, err := rc.loadConfig()
	if err != nil {
		return err
	}
	// whoami must never prompt for device code. We install a refusing
	// prompt so any fallthrough produces an immediate error.
	refuse := auth.PromptFn(func(_ context.Context, _ auth.DeviceCodePrompt) error {
		return errors.New("not signed in")
	})
	a, err := auth.New(auth.Config{TenantID: cfg.Account.TenantID, ClientID: cfg.Account.ClientID}, refuse)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Second)
	defer cancel()
	if _, err := a.Token(ctx); err != nil {
		return errors.New("not signed in")
	}
	upn, tenant, signedIn := a.Account()
	if !signedIn {
		return errors.New("not signed in")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s (tenant %s)\n", upn, tenant)
	return nil
}

func confirm(cmd *cobra.Command, prompt string) bool {
	in := cmd.InOrStdin()
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s [y/N]: ", prompt)
	r := bufio.NewReader(in)
	line, _ := r.ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(line), "y")
}

// silenceWarnings suppresses the unused-import warnings if a build
// removes references; left here intentionally as a no-op.
var _ = slog.LevelInfo
