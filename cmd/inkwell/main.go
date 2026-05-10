// Command inkwell is the terminal mail and calendar client.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// Spec 29 watch path returns a typed cliExitError that may
		// carry a non-1 exit code (e.g. ExitNotFound = 5,
		// ExitAuthError = 3, code 130 for double-Ctrl-C). Honour the
		// code; print the message only when non-empty.
		var ce *cliExitError
		if errors.As(err, &ce) {
			if ce.msg != "" {
				fmt.Fprintln(os.Stderr, "error:", ce.msg)
			}
			os.Exit(ce.code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		// Per spec 14 §"exit codes": 0 success, 2 user / usage error,
		// 1 runtime failure. Subcommands tag bad-input errors with
		// usageError so the CLI can exit 2 here.
		var ue *usageError
		if errors.As(err, &ue) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
