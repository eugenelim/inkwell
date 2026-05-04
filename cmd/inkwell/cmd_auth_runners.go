package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/auth"
	"github.com/eugenelim/inkwell/internal/store"
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
		if p.Message != "" {
			fmt.Fprintln(w)
			fmt.Fprintln(w, p.Message)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Open the URL in a browser, paste the user_code, then return here.")
		return nil
	}
}

// authConfigFromRoot builds the auth.Config from the loaded *config.Config.
// Empty TenantID/ClientID resolve inside auth to the locked first-party
// defaults (PRD §4); ExpectedUPN, when set, is a guardrail.
func authConfigFromRoot(rc *rootContext) (auth.Config, error) {
	cfg, err := rc.loadConfig()
	if err != nil {
		return auth.Config{}, err
	}
	mode, err := auth.ParseSignInMode(cfg.Account.SignInMode)
	if err != nil {
		return auth.Config{}, err
	}
	return auth.Config{
		TenantID:             cfg.Account.TenantID,
		ClientID:             cfg.Account.ClientID,
		ExpectedUPN:          cfg.Account.UPN,
		Mode:                 mode,
		RequestOfflineAccess: cfg.Account.RequestOfflineAccess,
	}, nil
}

func newAuthenticator(rc *rootContext) (auth.Authenticator, error) {
	c, err := authConfigFromRoot(rc)
	if err != nil {
		return nil, err
	}
	return auth.New(c, promptDeviceCode(os.Stderr))
}

func runSignin(cmd *cobra.Command, rc *rootContext) error {
	authCfg, err := authConfigFromRoot(rc)
	if err != nil {
		return err
	}
	// CLI flags override config. --device-code and --interactive are
	// mutually exclusive; detect that.
	useDeviceCode, _ := cmd.Flags().GetBool("device-code")
	useInteractive, _ := cmd.Flags().GetBool("interactive")
	if useDeviceCode && useInteractive {
		return errors.New("signin: --device-code and --interactive are mutually exclusive")
	}
	switch {
	case useDeviceCode:
		authCfg.Mode = auth.ModeDeviceCode
	case useInteractive:
		authCfg.Mode = auth.ModeInteractive
	}

	a, err := auth.New(authCfg, promptDeviceCode(os.Stderr))
	if err != nil {
		return err
	}
	a.Invalidate()
	if _, err := a.Token(cmd.Context()); err != nil {
		return fmt.Errorf("signin: %w", err)
	}
	upn, tenant, signedIn := a.Account()
	if !signedIn {
		return errors.New("signin: token acquired but no account resolved")
	}

	// Persist the resolved account to the local store so the TUI's
	// data-access path (Deps.Account → ListFolders/ListMessages) has
	// a row to scope queries against. Spec 01 §13 / spec 02 §5.
	if err := persistAccountRow(cmd.Context(), authCfg.ClientID, upn, tenant); err != nil {
		return fmt.Errorf("signin: persist account: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "signed in as %s (tenant %s)\n", upn, tenant)

	// Spec 04 iter 4: signin flows into the TUI on success unless
	// the user opted out (--no-tui, useful for CI scripting).
	noTUI, _ := cmd.Flags().GetBool("no-tui")
	if noTUI {
		return nil
	}
	return runRoot(cmd, rc)
}

// persistAccountRow opens the local store and upserts the signed-in
// account. Idempotent: if the row already exists with the same
// (tenant_id, upn), only mutable fields (client_id, last_signin) are
// updated.
func persistAccountRow(ctx context.Context, clientID, upn, tenantID string) error {
	if clientID == "" {
		clientID = auth.PublicClientID
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	dbPath := filepath.Join(home, "Library", "Application Support", "inkwell", "mail.db")
	s, err := store.Open(dbPath, store.DefaultOptions())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	_, err = s.PutAccount(ctx, store.Account{
		TenantID:   tenantID,
		ClientID:   clientID,
		UPN:        upn,
		LastSignin: time.Now(),
	})
	return err
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
	c, err := authConfigFromRoot(rc)
	if err != nil {
		return err
	}
	a, err := auth.New(c, promptDeviceCode(os.Stderr))
	if err != nil {
		return err
	}
	// IsSignedIn is silent-only (spec 01 §iter-7): never falls
	// through to interactive or device-code. whoami is read-only.
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()
	if !a.IsSignedIn(ctx) {
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
