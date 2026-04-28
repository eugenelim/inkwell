package main

import (
	"github.com/spf13/cobra"
)

func newSigninCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "signin",
		Short: "Sign in via OAuth device code flow",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runSignin(cmd, rc) },
	}
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
