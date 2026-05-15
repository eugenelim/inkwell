package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/eugenelim/inkwell/internal/auth"
	"github.com/eugenelim/inkwell/internal/config"
	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// headlessApp bundles the dependencies a non-TUI subcommand needs:
// store + graph client + signed-in account + logger. Centralises the
// auth → store → graph wiring that every CLI subcommand would
// otherwise duplicate from cmd_run.go.
type headlessApp struct {
	store     store.Store
	graph     *graph.Client
	account   *store.Account
	cfg       *config.Config
	logger    *slog.Logger
	logCloser io.Closer
}

// Close releases the log file and the SQLite handle. Always defer
// this in subcommand RunE.
func (a *headlessApp) Close() {
	if a.store != nil {
		_ = a.store.Close()
	}
	if a.logCloser != nil {
		_ = a.logCloser.Close()
	}
}

// buildHeadlessApp constructs the dependencies for a non-TUI flow.
// Verifies the user is signed in (no browser pop, no device-code
// fallthrough) before doing any I/O. Returns a friendly error if
// they're not signed in or the local DB is missing.
func buildHeadlessApp(ctx context.Context, rc *rootContext) (*headlessApp, error) {
	cfg, err := rc.loadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	level := slog.LevelInfo
	if rc.verbose {
		level = slog.LevelDebug
	}
	logger, logCloser, err := openLogFile(cfg.Account.UPN, level)
	if err != nil {
		return nil, err
	}

	mode, err := auth.ParseSignInMode(cfg.Account.SignInMode)
	if err != nil {
		_ = logCloser.Close()
		return nil, err
	}
	authCfg := auth.Config{
		TenantID:             cfg.Account.TenantID,
		ClientID:             cfg.Account.ClientID,
		ExpectedUPN:          cfg.Account.UPN,
		Mode:                 mode,
		RequestOfflineAccess: cfg.Account.RequestOfflineAccess,
	}
	a, err := auth.New(authCfg, nil)
	if err != nil {
		_ = logCloser.Close()
		return nil, err
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Second)
	if !a.IsSignedIn(probeCtx) {
		probeCancel()
		_ = logCloser.Close()
		return nil, errors.New("not signed in — run `inkwell signin` first")
	}
	probeCancel()

	st, err := store.Open(storeDBPath(), store.DefaultOptions())
	if err != nil {
		_ = logCloser.Close()
		return nil, fmt.Errorf("open store: %w", err)
	}
	acc, err := st.GetAccount(ctx)
	if err != nil {
		_ = st.Close()
		_ = logCloser.Close()
		return nil, fmt.Errorf("load account: %w (run `inkwell signin`)", err)
	}
	gc, err := graph.NewClient(a, graph.Options{
		Logger:        logger,
		MaxConcurrent: cfg.Sync.MaxConcurrent,
		MaxRetries:    cfg.Sync.MaxRetries,
	})
	if err != nil {
		_ = st.Close()
		_ = logCloser.Close()
		return nil, fmt.Errorf("graph client: %w", err)
	}
	return &headlessApp{
		store:     st,
		graph:     gc,
		account:   acc,
		cfg:       cfg,
		logger:    logger,
		logCloser: logCloser,
	}, nil
}
