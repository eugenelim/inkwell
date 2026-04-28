package main

import (
	"github.com/spf13/cobra"
)

func newSigninCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "signin",
		Short: "Sign in via the system browser (with device code as a fallback)",
		Long: `Sign in to Microsoft 365.

By default, Inkwell opens your system browser to complete sign-in. On a
managed Mac, the Microsoft Enterprise SSO plug-in for Apple Devices
transparently injects the device-compliance signal that Conditional
Access policies require — this is the only flow that works on
deeply-managed enterprise tenants.

Use --device-code only on headless / SSH sessions where no browser can
be launched. Conditional Access policies that require a managed device
will reject device-code sign-ins.`,
		RunE: func(cmd *cobra.Command, _ []string) error { return runSignin(cmd, rc) },
	}
	cmd.Flags().Bool("device-code", false, "force device code flow (headless / SSH only; cannot satisfy device-compliance CA policies)")
	cmd.Flags().Bool("interactive", false, "force interactive (system browser) flow; refuse to fall back to device code")
	return cmd
}

func newSignoutCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "signout",
		Short: "Clear cached credentials and signed-in account",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runSignout(cmd, rc) },
	}
}

func newWhoamiCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Print the signed-in user's UPN and tenant ID",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runWhoami(cmd, rc) },
	}
}
