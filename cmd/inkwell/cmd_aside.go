package main

import "github.com/spf13/cobra"

// newAsideCmd builds the `inkwell aside` parent command. Spec 25 §6.
func newAsideCmd(rc *rootContext) *cobra.Command {
	return stackParentCmd(rc, stackSetAside,
		"Manage the Set Aside stack (Inkwell/SetAside category)")
}
