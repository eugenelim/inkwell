package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/auth"
	"github.com/eugenelim/inkwell/internal/graph"
	ilog "github.com/eugenelim/inkwell/internal/log"
	"github.com/eugenelim/inkwell/internal/render"
	"github.com/eugenelim/inkwell/internal/store"
	isync "github.com/eugenelim/inkwell/internal/sync"
	"github.com/eugenelim/inkwell/internal/ui"
)

// runRoot is the default action when `inkwell` is invoked without a
// subcommand: build the full dependency graph (auth → store →
// graph.Client → sync.Engine → render.Renderer → ui.Model) and run
// the Bubble Tea program. Spec 04 §1 / iter 3.
func runRoot(cmd *cobra.Command, rc *rootContext) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	cfg, err := rc.loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	level := slog.LevelInfo
	if rc.verbose {
		level = slog.LevelDebug
	}
	logger, logCloser, err := openLogFile(cfg.Account.UPN, level)
	if err != nil {
		return err
	}
	defer logCloser.Close()
	logger.Info("inkwell: starting",
		slog.String("version", version),
		slog.String("commit", commit),
		slog.Bool("verbose", rc.verbose),
	)

	// Auth
	mode, err := auth.ParseSignInMode(cfg.Account.SignInMode)
	if err != nil {
		return err
	}
	authCfg := auth.Config{
		TenantID:             cfg.Account.TenantID,
		ClientID:             cfg.Account.ClientID,
		ExpectedUPN:          cfg.Account.UPN,
		Mode:                 mode,
		RequestOfflineAccess: cfg.Account.RequestOfflineAccess,
	}
	a, err := auth.New(authCfg, promptDeviceCode(os.Stderr))
	if err != nil {
		return err
	}

	// Verify the user is signed in BEFORE we open the TUI; otherwise
	// the TUI flashes empty and exits when the engine fails on its
	// first Graph call. IsSignedIn is silent-only — never opens a
	// browser, never hits device-code (that's the bug v0.2.0
	// shipped: the previous probe used Token() with Mode=Auto, which
	// would silently fall through to interactive on the second run
	// and open the browser AGAIN even though the user just signed in).
	probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Second)
	if !a.IsSignedIn(probeCtx) {
		probeCancel()
		return errors.New("not signed in — run `inkwell signin` first")
	}
	probeCancel()

	// Store
	dbPath := storeDBPath()
	st, err := store.Open(dbPath, store.DefaultOptions())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	acc, err := st.GetAccount(ctx)
	if err != nil {
		return fmt.Errorf("load account: %w (run `inkwell signin`)", err)
	}

	// Graph client (logger required for redaction).
	gc, err := graph.NewClient(a, graph.Options{
		Logger:        logger,
		MaxConcurrent: cfg.Sync.MaxConcurrent,
		MaxRetries:    cfg.Sync.MaxRetries,
	})
	if err != nil {
		return fmt.Errorf("graph client: %w", err)
	}

	// Sync engine
	engine, err := isync.New(gc, st, nil, isync.Options{
		AccountID:          acc.ID,
		Logger:             logger,
		ForegroundInterval: cfg.Sync.ForegroundInterval,
		BackgroundInterval: cfg.Sync.BackgroundInterval,
	})
	if err != nil {
		return fmt.Errorf("sync engine: %w", err)
	}

	// Renderer with the production graph-backed body fetcher.
	renderer := render.New(st, render.NewGraphBodyFetcher(gc))

	// Kick off the engine. Its loop runs an immediate first cycle
	// (spec 03 §5: "On Start():") which enumerates folders and pulls
	// the last-50-per-folder via the lazy progressive backfill. The
	// goroutines from v0.2.0 that called SyncAll/QuickStartBackfill
	// from the cmd layer are gone — they duplicated work and
	// swallowed errors.
	logger.Info("engine: starting", slog.Int64("account_id", acc.ID))
	if err := engine.Start(ctx); err != nil {
		return fmt.Errorf("start engine: %w", err)
	}

	// Tell the user where logs go before alt-screen takes over the
	// terminal. With --verbose, this is even more useful.
	if home, err := os.UserHomeDir(); err == nil {
		fmt.Fprintf(os.Stderr, "logs: %s\n",
			filepath.Join(home, "Library", "Logs", "inkwell", "inkwell.log"))
	}

	// UI
	model := ui.New(ui.Deps{
		Auth:     a,
		Store:    st,
		Engine:   engine,
		Renderer: renderer,
		Logger:   logger,
		Account:  acc,
	})
	prog := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

// storeDBPath returns the SQLite path. Mirrors spec 02 §2.
func storeDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "inkwell", "mail.db")
}

// openLogFile opens (or creates) the log file under
// ~/Library/Logs/inkwell/. Returns a redacting slog.Logger pointed at
// it. The caller closes the io.Closer at shutdown.
func openLogFile(ownUPN string, level slog.Level) (*slog.Logger, io.Closer, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Logs", "inkwell")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, noopCloser{}, fmt.Errorf("mkdir log dir: %w", err)
	}
	path := filepath.Join(dir, "inkwell.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, noopCloser{}, fmt.Errorf("open log file: %w", err)
	}
	logger := ilog.New(f, ilog.Options{Level: level, AllowOwnUPN: ownUPN})
	return logger, f, nil
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }
