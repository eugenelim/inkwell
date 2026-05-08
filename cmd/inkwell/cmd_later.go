package main

import "github.com/spf13/cobra"

// newLaterCmd builds the `inkwell later` parent command. Spec 25 §6.
func newLaterCmd(rc *rootContext) *cobra.Command {
	return stackParentCmd(rc, stackReplyLater,
		"Manage the Reply Later stack (Inkwell/ReplyLater category)")
}
