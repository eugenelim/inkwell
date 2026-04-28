package main

import (
	"github.com/spf13/cobra"
)

// These runner stubs are filled in by spec 01. Keeping them in a separate
// file lets cmd_signin.go declare the cobra.Command shapes once and lets
// spec 01 add real bodies without touching command wiring.

func runSignin(cmd *cobra.Command, _ *rootContext) error {
	cmd.Println("signin: not yet implemented (spec 01)")
	return nil
}

func runSignout(cmd *cobra.Command, _ *rootContext) error {
	cmd.Println("signout: not yet implemented (spec 01)")
	return nil
}

func runWhoami(cmd *cobra.Command, _ *rootContext) error {
	cmd.Println("whoami: not yet implemented (spec 01)")
	return nil
}
