package main

import (
	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/config"
)

// version, commit, date are populated at build time via -ldflags
// (see Makefile and .goreleaser.yaml).
var (
	version = "0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

// rootContext is the shared state passed down to subcommands. It is
// constructed lazily inside PersistentPreRunE so subcommands like
// `inkwell --version` work without a config file present.
type rootContext struct {
	configPath string
	cfg        *config.Config
	verbose    bool
}

func newRootCmd() *cobra.Command {
	rc := &rootContext{}
	cmd := &cobra.Command{
		Use:           "inkwell",
		Short:         "Terminal-based mail and calendar client for Microsoft 365",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version + " (" + commit + " · " + date + ")",
	}
	cmd.PersistentFlags().StringVar(&rc.configPath, "config", config.DefaultPath(), "path to config.toml")
	cmd.PersistentFlags().BoolVar(&rc.verbose, "verbose", false, "enable debug logging")
	cmd.AddCommand(newSigninCmd(rc))
	cmd.AddCommand(newSignoutCmd(rc))
	cmd.AddCommand(newWhoamiCmd(rc))
	// Default action when no subcommand is given: launch the TUI.
	cmd.RunE = func(c *cobra.Command, _ []string) error { return runRoot(c, rc) }
	return cmd
}

// loadConfig is called by subcommands that need credentials. It is
// idempotent: a single rootContext keeps the parsed config in memory.
func (rc *rootContext) loadConfig() (*config.Config, error) {
	if rc.cfg != nil {
		return rc.cfg, nil
	}
	cfg, err := config.Load(rc.configPath)
	if err != nil {
		return nil, err
	}
	rc.cfg = cfg
	return cfg, nil
}
