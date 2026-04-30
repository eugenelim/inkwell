package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/action"
	"github.com/eugenelim/inkwell/internal/auth"
	"github.com/eugenelim/inkwell/internal/config"
	"github.com/eugenelim/inkwell/internal/graph"
	ilog "github.com/eugenelim/inkwell/internal/log"
	"github.com/eugenelim/inkwell/internal/render"
	"github.com/eugenelim/inkwell/internal/store"
	isync "github.com/eugenelim/inkwell/internal/sync"
	"github.com/eugenelim/inkwell/internal/ui"
	"github.com/eugenelim/inkwell/internal/unsub"
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

	// Graph client (logger required for redaction). The OnThrottle
	// hook is a closure that captures `engine` by reference — once
	// the engine is constructed (a few lines below), the closure
	// forwards 429 retries as ThrottledEvent. Spec 03 §3.
	var engine isync.Engine
	gc, err := graph.NewClient(a, graph.Options{
		Logger:        logger,
		MaxConcurrent: cfg.Sync.MaxConcurrent,
		MaxRetries:    cfg.Sync.MaxRetries,
		OnThrottle: func(d time.Duration) {
			if engine != nil {
				engine.OnThrottle(d)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("graph client: %w", err)
	}

	// Action executor (spec 07): handles single-message triage. Implements
	// sync.ActionDrainer so the engine retries pending actions every
	// cycle (handles transient throttle / network failure).
	exec := action.New(st, gc, logger)

	// Sync engine
	engine, err = isync.New(gc, st, exec, isync.Options{
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
	saved := make([]ui.SavedSearch, 0, len(cfg.SavedSearches))
	for _, s := range cfg.SavedSearches {
		saved = append(saved, ui.SavedSearch{Name: s.Name, Pattern: s.Pattern})
	}
	model, err := ui.New(ui.Deps{
		Auth:          a,
		Store:         st,
		Engine:        engine,
		Renderer:      renderer,
		Logger:        logger,
		Account:       acc,
		Triage:        triageAdapter{exec: exec},
		Bulk:          bulkAdapter{exec: exec},
		Calendar:      calendarAdapter{gc: gc, st: st, accountID: acc.ID},
		Mailbox:       mailboxAdapter{gc: gc},
		Drafts:        draftAdapter{exec: exec},
		Unsubscribe:   newUnsubAdapter(st, gc, version),
		ThemeName:     cfg.UI.Theme,
		SavedSearches: saved,
		Bindings:      bindingsToOverrides(cfg.Bindings),
	})
	if err != nil {
		return fmt.Errorf("tui init: %w", err)
	}
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
	// #nosec G304 — path is ~/Library/Logs/inkwell/inkwell.log composed from os.UserHomeDir(); not user-controlled at runtime.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, noopCloser{}, fmt.Errorf("open log file: %w", err)
	}
	logger := ilog.New(f, ilog.Options{Level: level, AllowOwnUPN: ownUPN})
	return logger, f, nil
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

// triageAdapter bridges *action.Executor → ui.TriageExecutor. The
// non-undo methods are direct passthroughs; Undo translates
// store.UndoEntry → ui.UndoneAction and store.ErrNotFound →
// ui.UndoEmpty so the UI doesn't import internal/store types
// beyond what's already exposed.
type triageAdapter struct{ exec *action.Executor }

func (t triageAdapter) MarkRead(ctx context.Context, accountID int64, messageID string) error {
	return t.exec.MarkRead(ctx, accountID, messageID)
}

func (t triageAdapter) MarkUnread(ctx context.Context, accountID int64, messageID string) error {
	return t.exec.MarkUnread(ctx, accountID, messageID)
}

func (t triageAdapter) ToggleFlag(ctx context.Context, accountID int64, messageID string, currentlyFlagged bool) error {
	return t.exec.ToggleFlag(ctx, accountID, messageID, currentlyFlagged)
}

func (t triageAdapter) SoftDelete(ctx context.Context, accountID int64, messageID string) error {
	return t.exec.SoftDelete(ctx, accountID, messageID)
}

func (t triageAdapter) Archive(ctx context.Context, accountID int64, messageID string) error {
	return t.exec.Archive(ctx, accountID, messageID)
}

func (t triageAdapter) PermanentDelete(ctx context.Context, accountID int64, messageID string) error {
	return t.exec.PermanentDelete(ctx, accountID, messageID)
}

func (t triageAdapter) Undo(ctx context.Context, accountID int64) (ui.UndoneAction, error) {
	entry, err := t.exec.Undo(ctx, accountID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ui.UndoneAction{}, ui.UndoEmpty
		}
		return ui.UndoneAction{}, err
	}
	return ui.UndoneAction{Label: entry.Label, MessageIDs: entry.MessageIDs}, nil
}

// bulkAdapter bridges action.BatchResult slices into ui.BulkResult
// slices. The two structs are intentionally identical in shape; this
// adapter exists so the ui package doesn't have to import internal/action.
type bulkAdapter struct{ exec *action.Executor }

func (b bulkAdapter) BulkSoftDelete(ctx context.Context, accountID int64, ids []string) ([]ui.BulkResult, error) {
	got, err := b.exec.BulkSoftDelete(ctx, accountID, ids)
	return convertBatchResults(got), err
}

func (b bulkAdapter) BulkArchive(ctx context.Context, accountID int64, ids []string) ([]ui.BulkResult, error) {
	got, err := b.exec.BulkArchive(ctx, accountID, ids)
	return convertBatchResults(got), err
}

func (b bulkAdapter) BulkMarkRead(ctx context.Context, accountID int64, ids []string) ([]ui.BulkResult, error) {
	got, err := b.exec.BulkMarkRead(ctx, accountID, ids)
	return convertBatchResults(got), err
}

// draftAdapter bridges action.Executor.CreateDraftReply →
// ui.DraftCreator. Same shape; the adapter exists so the UI doesn't
// import internal/action.
type draftAdapter struct{ exec *action.Executor }

func (d draftAdapter) CreateDraftReply(ctx context.Context, sourceID, body string, to, cc, bcc []string, subject string) (*ui.DraftRef, error) {
	res, err := d.exec.CreateDraftReply(ctx, sourceID, body, to, cc, bcc, subject)
	if res == nil {
		return nil, err
	}
	return &ui.DraftRef{ID: res.ID, WebLink: res.WebLink}, err
}

// mailboxAdapter bridges graph mailbox-settings calls → ui.MailboxClient.
type mailboxAdapter struct{ gc *graph.Client }

func (m mailboxAdapter) Get(ctx context.Context) (*ui.MailboxSettings, error) {
	s, err := m.gc.GetMailboxSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &ui.MailboxSettings{
		AutoReplyEnabled:     s.AutoReplies.Status != graph.AutoReplyDisabled,
		InternalReplyMessage: s.AutoReplies.InternalReplyMessage,
		ExternalReplyMessage: s.AutoReplies.ExternalReplyMessage,
	}, nil
}

func (m mailboxAdapter) SetAutoReply(ctx context.Context, enabled bool, internalMsg, externalMsg string) error {
	status := graph.AutoReplyDisabled
	if enabled {
		status = graph.AutoReplyAlwaysEnabled
	}
	return m.gc.UpdateAutoReplies(ctx, graph.AutoRepliesSetting{
		Status:               status,
		InternalReplyMessage: internalMsg,
		ExternalReplyMessage: externalMsg,
		ExternalAudience:     "all",
	})
}

// calendarAdapter bridges graph + store → ui.CalendarFetcher. Spec
// 12 §5: read from cache first so the modal renders offline; if
// the cache is empty for today's window, fetch from Graph and
// persist for next time. Stale cache (>15min) also re-fetches.
//
// Persisting on fetch closes the spec-12 audit gap "calendar
// persisted nowhere" without yet wiring the engine's third sync
// state — that's PR 6b's scope.
type calendarAdapter struct {
	gc        *graph.Client
	st        store.Store
	accountID int64
}

const calendarCacheTTL = 15 * time.Minute

func (c calendarAdapter) ListEventsToday(ctx context.Context) ([]ui.CalendarEvent, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()
	endOfDay := startOfDay.Add(24 * time.Hour)

	cached, _ := c.st.ListEvents(ctx, store.EventQuery{
		AccountID: c.accountID,
		Start:     startOfDay,
		End:       endOfDay,
	})

	// Cache hit AND fresh? Serve from local. Stale or empty → fetch.
	fresh := len(cached) > 0
	for _, e := range cached {
		if now.Sub(e.CachedAt) > calendarCacheTTL {
			fresh = false
			break
		}
	}
	if fresh {
		return convertStoreEvents(cached), nil
	}

	// Cache miss / stale: fetch from Graph and persist.
	es, err := c.gc.ListEventsToday(ctx)
	if err != nil {
		// Graph failed; if we have any cached rows, surface those
		// so the user sees the last-known state rather than an
		// empty modal. Stale-data fallback per spec 12 §5.
		if len(cached) > 0 {
			return convertStoreEvents(cached), nil
		}
		return nil, err
	}
	storeEvents := convertGraphEvents(c.accountID, es)
	if err := c.st.PutEvents(ctx, storeEvents); err != nil {
		// Persist failure isn't fatal — the user gets the data
		// for this session; next launch refetches.
		c.gc.Logger().Warn("calendar: persist failed", "err", err.Error())
	}
	return convertStoreEventsFromGraph(es), nil
}

func convertStoreEvents(events []store.Event) []ui.CalendarEvent {
	out := make([]ui.CalendarEvent, len(events))
	for i, e := range events {
		out[i] = ui.CalendarEvent{
			Subject:          e.Subject,
			OrganizerName:    e.OrganizerName,
			OrganizerAddress: e.OrganizerAddress,
			Start:            e.Start,
			End:              e.End,
			IsAllDay:         e.IsAllDay,
			Location:         e.Location,
			OnlineMeetingURL: e.OnlineMeetingURL,
		}
	}
	return out
}

func convertStoreEventsFromGraph(events []graph.Event) []ui.CalendarEvent {
	out := make([]ui.CalendarEvent, len(events))
	for i, e := range events {
		out[i] = ui.CalendarEvent{
			Subject:          e.Subject,
			OrganizerName:    e.OrganizerName,
			OrganizerAddress: e.OrganizerAddress,
			Start:            e.Start,
			End:              e.End,
			IsAllDay:         e.IsAllDay,
			Location:         e.Location,
			OnlineMeetingURL: e.OnlineMeetingURL,
		}
	}
	return out
}

func convertGraphEvents(accountID int64, events []graph.Event) []store.Event {
	out := make([]store.Event, len(events))
	now := time.Now()
	for i, e := range events {
		out[i] = store.Event{
			ID:               e.ID,
			AccountID:        accountID,
			Subject:          e.Subject,
			OrganizerName:    e.OrganizerName,
			OrganizerAddress: e.OrganizerAddress,
			Start:            e.Start,
			End:              e.End,
			IsAllDay:         e.IsAllDay,
			Location:         e.Location,
			OnlineMeetingURL: e.OnlineMeetingURL,
			ShowAs:           e.ShowAs,
			WebLink:          e.WebLink,
			CachedAt:         now,
		}
	}
	return out
}

// bindingsToOverrides translates config.BindingsConfig (TOML-typed)
// into the UI's consumer-side BindingOverrides shape. The two
// structs are deliberately the same shape; this adapter exists so
// the UI doesn't import internal/config (CLAUDE.md §2).
func bindingsToOverrides(b config.BindingsConfig) ui.BindingOverrides {
	return ui.BindingOverrides{
		Quit:            b.Quit,
		Help:            b.Help,
		Cmd:             b.Cmd,
		Search:          b.Search,
		Refresh:         b.Refresh,
		FocusFolders:    b.FocusFolders,
		FocusList:       b.FocusList,
		FocusViewer:     b.FocusViewer,
		NextPane:        b.NextPane,
		PrevPane:        b.PrevPane,
		Up:              b.Up,
		Down:            b.Down,
		Left:            b.Left,
		Right:           b.Right,
		PageUp:          b.PageUp,
		PageDown:        b.PageDown,
		Home:            b.Home,
		End:             b.End,
		Open:            b.Open,
		MarkRead:        b.MarkRead,
		MarkUnread:      b.MarkUnread,
		ToggleFlag:      b.ToggleFlag,
		Delete:          b.Delete,
		PermanentDelete: b.PermanentDelete,
		Archive:         b.Archive,
		Move:            b.Move,
		AddCategory:     b.AddCategory,
		RemoveCategory:  b.RemoveCategory,
		Undo:            b.Undo,
		Filter:          b.Filter,
		ClearFilter:     b.ClearFilter,
		ApplyToFiltered: b.ApplyToFiltered,
		Unsubscribe:     b.Unsubscribe,
	}
}

func convertBatchResults(in []action.BatchResult) []ui.BulkResult {
	out := make([]ui.BulkResult, len(in))
	for i, r := range in {
		out[i] = ui.BulkResult{MessageID: r.MessageID, Err: r.Err}
	}
	return out
}

// unsubAdapter wires the spec 16 U flow. The Resolve method tries the
// store cache first; on miss it fetches headers via Graph, parses,
// persists the parsed action, and returns the resolved kind. The
// OneClickPOST method delegates to the unsub.Executor.
type unsubAdapter struct {
	st  store.Store
	gc  *graph.Client
	exe *unsub.Executor
}

func newUnsubAdapter(st store.Store, gc *graph.Client, ver string) *unsubAdapter {
	return &unsubAdapter{st: st, gc: gc, exe: unsub.NewExecutor(ver)}
}

func (u *unsubAdapter) Resolve(ctx context.Context, messageID string) (ui.UnsubscribeAction, error) {
	// Cache hit path: row already has the parsed action persisted.
	row, err := u.st.GetMessage(ctx, messageID)
	if err == nil && row != nil && row.UnsubscribeURL != "" {
		return mapCachedUnsub(row.UnsubscribeURL, row.UnsubscribeOneClick), nil
	}

	headers, err := u.gc.GetMessageHeaders(ctx, messageID)
	if err != nil {
		return ui.UnsubscribeAction{}, fmt.Errorf("fetch headers: %w", err)
	}
	listUnsub := graph.HeaderValue(headers, "List-Unsubscribe")
	listUnsubPost := graph.HeaderValue(headers, "List-Unsubscribe-Post")
	res, err := unsub.Parse(listUnsub, listUnsubPost)
	if err != nil {
		// Persist a sentinel so we don't re-fetch on every U press.
		// Empty unsubscribe_url + empty header => "we tried, nothing to do".
		_ = u.st.SetUnsubscribe(ctx, messageID, "", false)
		return ui.UnsubscribeAction{Kind: ui.UnsubscribeNone}, err
	}
	cacheURL := unsub.IndicatorURL(res)
	oneClick := res.Action == unsub.ActionOneClickPOST
	if err := u.st.SetUnsubscribe(ctx, messageID, cacheURL, oneClick); err != nil {
		// Persistence failure isn't fatal; surface the action anyway.
		// Next press will refetch.
		_ = err
	}
	return resultToAction(res), nil
}

func (u *unsubAdapter) OneClickPOST(ctx context.Context, url string) error {
	return u.exe.OneClickPOST(ctx, url)
}

// mapCachedUnsub turns a (url, oneClick) tuple back into a
// UnsubscribeAction. URL prefixed with "mailto:" is the mailto
// path; everything else is HTTPS (one-click vs browser keyed by
// the persisted boolean).
func mapCachedUnsub(url string, oneClick bool) ui.UnsubscribeAction {
	if strings.HasPrefix(url, "mailto:") {
		return ui.UnsubscribeAction{Kind: ui.UnsubscribeMailto, Mailto: url[len("mailto:"):]}
	}
	if oneClick {
		return ui.UnsubscribeAction{Kind: ui.UnsubscribeOneClickPOST, URL: url}
	}
	return ui.UnsubscribeAction{Kind: ui.UnsubscribeBrowserGET, URL: url}
}

// resultToAction translates the unsub.Result enum to the UI's enum.
func resultToAction(r *unsub.Result) ui.UnsubscribeAction {
	switch r.Action {
	case unsub.ActionOneClickPOST:
		return ui.UnsubscribeAction{Kind: ui.UnsubscribeOneClickPOST, URL: r.URL}
	case unsub.ActionBrowserGET:
		return ui.UnsubscribeAction{Kind: ui.UnsubscribeBrowserGET, URL: r.URL}
	case unsub.ActionMailto:
		return ui.UnsubscribeAction{Kind: ui.UnsubscribeMailto, Mailto: r.MailtoAddr}
	}
	return ui.UnsubscribeAction{Kind: ui.UnsubscribeNone}
}
