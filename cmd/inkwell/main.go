// Command inkwell is the terminal mail and calendar client.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
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
