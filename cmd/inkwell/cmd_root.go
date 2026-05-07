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
	output     string
	color      string
	quiet      bool
	noSync     bool
	yes        bool
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
	cmd.PersistentFlags().StringVarP(&rc.output, "output", "o", "", "output format: text|json (overrides [cli].default_output)")
	cmd.PersistentFlags().StringVar(&rc.color, "color", "", "color output: auto|always|never")
	cmd.PersistentFlags().BoolVarP(&rc.quiet, "quiet", "q", false, "suppress progress and INFO logs")
	cmd.PersistentFlags().BoolVarP(&rc.yes, "yes", "y", false, "skip confirmation prompts")
	cmd.PersistentFlags().BoolVar(&rc.noSync, "no-sync", false, "use cached data only, skip sync")
	cmd.AddCommand(newSigninCmd(rc))
	cmd.AddCommand(newSignoutCmd(rc))
	cmd.AddCommand(newWhoamiCmd(rc))
	// Spec 14 — non-interactive subcommands. Each opens the local DB
	// + a Graph client (no TUI).
	cmd.AddCommand(newFoldersCmd(rc))
	cmd.AddCommand(newFolderCmd(rc))
	cmd.AddCommand(newMessagesCmd(rc))
	cmd.AddCommand(newSyncCmd(rc))
	cmd.AddCommand(newFilterCmd(rc))
	cmd.AddCommand(newSearchCmd(rc))
	cmd.AddCommand(newOOOCmd(rc))
	cmd.AddCommand(newSettingsCmd(rc))
	cmd.AddCommand(newRuleCmd(rc))
	cmd.AddCommand(newCalendarCmd(rc))
	cmd.AddCommand(newDaemonCmd(rc))
	cmd.AddCommand(newExportCmd(rc))
	cmd.AddCommand(newBackfillCmd(rc))
	cmd.AddCommand(newMuteCmd(rc))
	cmd.AddCommand(newUnmuteCmd(rc))
	cmd.AddCommand(newThreadCmd(rc))
	cmd.AddCommand(newRouteCmd(rc))
	cmd.AddCommand(newTabCmd(rc))
	// Default action when no subcommand is given: launch the TUI.
	cmd.RunE = func(c *cobra.Command, _ []string) error { return runRoot(c, rc) }
	return cmd
}

// effectiveOutput returns the output format to use for a command: the
// --output flag if set, otherwise [cli].default_output, otherwise "text".
func effectiveOutput(rc *rootContext, cfg *config.Config) string {
	if rc.output != "" {
		return rc.output
	}
	if cfg != nil && cfg.CLI.DefaultOutput != "" {
		return cfg.CLI.DefaultOutput
	}
	return "text"
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
