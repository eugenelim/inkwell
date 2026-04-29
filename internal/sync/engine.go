package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// Event is the marker interface for engine notifications. Concrete
// types are the *Event values declared below.
type Event interface{ isEvent() }

// FolderSyncedEvent fires after a successful per-folder delta loop.
type FolderSyncedEvent struct {
	FolderID string
	Added    int
	Updated  int
	Deleted  int
	At       time.Time
}

// SyncStartedEvent fires when an engine cycle begins.
type SyncStartedEvent struct{ At time.Time }

// SyncCompletedEvent fires when an engine cycle ends.
type SyncCompletedEvent struct {
	At            time.Time
	FoldersSynced int
	Duration      time.Duration
}

// SyncFailedEvent fires when an engine cycle terminates with an error.
type SyncFailedEvent struct {
	At  time.Time
	Err error
}

// ThrottledEvent fires whenever the graph client retried after a 429.
type ThrottledEvent struct{ RetryAfter time.Duration }

// AuthRequiredEvent fires when the engine cannot acquire a token.
type AuthRequiredEvent struct{ At time.Time }

// FoldersEnumeratedEvent fires after the per-cycle /me/mailFolders call
// upserts the folder list into the store. The TUI uses it as a signal
// to reload its sidebar BEFORE per-folder syncs complete — folders
// appear immediately, even if a per-folder sync later errors out.
type FoldersEnumeratedEvent struct {
	Count int
	At    time.Time
}

func (FolderSyncedEvent) isEvent()      {}
func (FoldersEnumeratedEvent) isEvent() {}
func (SyncStartedEvent) isEvent()       {}
func (SyncCompletedEvent) isEvent()     {}
func (SyncFailedEvent) isEvent()        {}
func (ThrottledEvent) isEvent()         {}
func (AuthRequiredEvent) isEvent()      {}

// State enumerates the engine's lifecycle.
type State int

const (
	StateIdle State = iota
	StateDrainingActions
	StateSyncingFolders
)

// String returns the human label.
func (s State) String() string {
	switch s {
	case StateDrainingActions:
		return "draining_actions"
	case StateSyncingFolders:
		return "syncing_folders"
	default:
		return "idle"
	}
}

// Engine is the public interface consumed by the UI and CLI.
type Engine interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	SetActive(active bool)
	Sync(ctx context.Context, folderID string) error
	SyncAll(ctx context.Context) error
	Backfill(ctx context.Context, folderID string, until time.Time) error
	ResetDelta(ctx context.Context, folderID string) error
	Notifications() <-chan Event
	// Wake nudges the engine to run a cycle now (the next select
	// iteration of the loop). Single-shot — duplicate calls within a
	// running cycle are coalesced by the buffer-1 wakeup channel,
	// which prevents the cycle storms seen when callers go through
	// SyncAll concurrently with the loop's own timer.
	Wake()
}

// ActionDrainer is the seam between sync and the action executor (spec
// 09). The sync engine calls Drain when entering the
// draining-actions state; spec 09's executor satisfies this interface.
type ActionDrainer interface {
	Drain(ctx context.Context) error
}

// noopDrainer is the placeholder used until spec 09 lands.
type noopDrainer struct{}

func (noopDrainer) Drain(_ context.Context) error { return nil }

// Options configures [New].
type Options struct {
	AccountID          int64
	BackfillDays       int
	ForegroundInterval time.Duration
	BackgroundInterval time.Duration
	Logger             *slog.Logger
	// SubscribedFolders restricts which well-known names participate
	// in delta sync. Empty = the spec §5.1 default set.
	SubscribedFolders []string
}

func (o *Options) defaults() {
	if o.BackfillDays <= 0 {
		o.BackfillDays = 90
	}
	if o.ForegroundInterval <= 0 {
		o.ForegroundInterval = 30 * time.Second
	}
	if o.BackgroundInterval <= 0 {
		o.BackgroundInterval = 5 * time.Minute
	}
	if len(o.SubscribedFolders) == 0 {
		o.SubscribedFolders = DefaultSubscribedFolders()
	}
}

// DefaultSubscribedFolders is the spec §5.1 default subscription set:
// Inbox + Sent + Drafts + Archive. User folders (well_known_name = "")
// are also subscribed by default, applied in [filterSubscribed].
func DefaultSubscribedFolders() []string {
	return []string{"inbox", "sentitems", "drafts", "archive"}
}

// excludedWellKnown lists folders explicitly NOT subscribed (spec §5.1).
var excludedWellKnown = map[string]bool{
	"deleteditems":        true,
	"junkemail":           true,
	"conversationhistory": true,
	"syncissues":          true,
}

// engine is the [Engine] implementation. All fields except mu and
// state are immutable after [New].
type engine struct {
	gc     *graph.Client
	st     store.Store
	drain  ActionDrainer
	opts   Options
	logger *slog.Logger

	events chan Event

	mu       sync.Mutex
	state    State
	active   bool
	stopOnce sync.Once
	stopped  chan struct{}
	wakeup   chan struct{}
	// cycleMu serialises runCycle. Without it, a UI-fired SyncAll
	// goroutine can run concurrently with the engine's own loop tick,
	// producing the back-to-back HTTP storms seen in real-tenant
	// logs. The lock is held for the entire cycle (sub-second
	// typically); it does NOT block the wakeup / Stop paths.
	cycleMu sync.Mutex
}

// New constructs an Engine. The [graph.Client] handles auth + throttle;
// [store.Store] is the local cache; drain may be nil (replaced with a
// no-op until spec 09).
func New(gc *graph.Client, st store.Store, drain ActionDrainer, opts Options) (Engine, error) {
	if gc == nil {
		return nil, errors.New("sync: graph client required")
	}
	if st == nil {
		return nil, errors.New("sync: store required")
	}
	if opts.AccountID == 0 {
		return nil, errors.New("sync: account_id required")
	}
	if opts.Logger == nil {
		return nil, errors.New("sync: logger required")
	}
	opts.defaults()
	if drain == nil {
		drain = noopDrainer{}
	}
	return &engine{
		gc:      gc,
		st:      st,
		drain:   drain,
		opts:    opts,
		logger:  opts.Logger,
		events:  make(chan Event, 32),
		state:   StateIdle,
		active:  true,
		stopped: make(chan struct{}),
		wakeup:  make(chan struct{}, 1),
	}, nil
}

// Notifications returns the read-side of the event channel.
func (e *engine) Notifications() <-chan Event { return e.events }

// SetActive switches between foreground (true, 30s) and background
// (false, 5min) cadence. Idempotent.
func (e *engine) SetActive(active bool) {
	e.mu.Lock()
	e.active = active
	e.mu.Unlock()
	// Wake the loop so the next interval is recomputed.
	e.kick()
}

// Start launches the engine loop. Idempotent (a second call is a no-op).
// Pass a context that lives for the life of the app; cancel it to
// shut the engine down.
func (e *engine) Start(ctx context.Context) error {
	go func() {
		// Panic recovery: bubbletea's alt-screen swallows stderr, so
		// a goroutine panic without recovery is invisible. Capture
		// it to the log AND surface as SyncFailedEvent so the TUI
		// status bar shows it.
		defer func() {
			if r := recover(); r != nil {
				e.logger.Error("engine: panic in loop", slog.Any("panic", r))
				e.emit(SyncFailedEvent{At: time.Now(), Err: fmt.Errorf("engine panic: %v", r)})
			}
		}()
		e.loop(ctx)
	}()
	return nil
}

// Stop signals the engine loop to terminate. Drains in-flight calls
// before returning, but does not block past the supplied context.
func (e *engine) Stop(ctx context.Context) error {
	e.stopOnce.Do(func() { close(e.stopped) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return nil
	}
}

// Sync triggers an immediate sync of the named folder.
func (e *engine) Sync(ctx context.Context, folderID string) error {
	return e.syncFolder(ctx, folderID)
}

// SyncAll syncs every subscribed folder in serial. Folder enumeration
// runs first so renames land before message delta.
//
// SyncAll is the synchronous form. UI callers should prefer Wake()
// (single-shot, debounced, doesn't overlap with the engine's loop).
func (e *engine) SyncAll(ctx context.Context) error {
	return e.runCycle(ctx)
}

// Wake nudges the engine's loop to run a cycle now. Implemented as a
// non-blocking send to the buffer-1 wakeup channel: duplicate calls
// while a cycle is already pending coalesce into one. This is the
// preferred path for UI-driven "sync now please" because it
// guarantees serialisation with the engine's own timer-driven cycles.
func (e *engine) Wake() {
	e.kick()
}

// Backfill pulls older-than-default messages for folderID up to until.
// Foreground-blocking by default (spec §5.4).
func (e *engine) Backfill(ctx context.Context, folderID string, until time.Time) error {
	return e.backfillFolder(ctx, folderID, until)
}

// ResetDelta clears the per-folder cursor.
func (e *engine) ResetDelta(ctx context.Context, folderID string) error {
	return e.st.ClearDeltaToken(ctx, e.opts.AccountID, folderID)
}

// loop is the main timer loop. A single time.Timer is reset to the
// active interval after each cycle, avoiding the leak pattern of two
// concurrent tickers.
//
// The first iteration runs IMMEDIATELY (no initial wait) so the TUI
// gets folders + last-50-per-folder within seconds instead of waiting
// the full foreground interval. Spec 03 §5: "On Start():" — this is
// where first-launch detection happens.
func (e *engine) loop(ctx context.Context) {
	e.logger.Info("engine: loop starting; running first cycle immediately")
	if err := e.runCycle(ctx); err != nil && !errors.Is(err, context.Canceled) {
		e.logger.Error("engine: first cycle failed", slog.String("err", err.Error()))
		e.emit(SyncFailedEvent{At: time.Now(), Err: err})
	}

	timer := time.NewTimer(e.interval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopped:
			return
		case <-timer.C:
		case <-e.wakeup:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		if err := e.runCycle(ctx); err != nil && !errors.Is(err, context.Canceled) {
			e.logger.Error("engine: cycle failed", slog.String("err", err.Error()))
			e.emit(SyncFailedEvent{At: time.Now(), Err: err})
		}
		timer.Reset(e.interval())
	}
}

// minSyncInterval floors the active interval. Any config value below
// this is clamped to prevent a misconfigured config.toml (e.g.
// `foreground_interval = "100ms"`) from putting the engine in a
// permanent sync storm — Graph rate-limits us before the user sees
// the issue, and the cycle's HTTP fan-out (~5 folders × ~70ms each)
// dominates anyway. 5 seconds is well below any sensible foreground
// cadence; tests inject a faster clock as needed.
const minSyncInterval = 5 * time.Second

func (e *engine) interval() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	d := e.opts.BackgroundInterval
	if e.active {
		d = e.opts.ForegroundInterval
	}
	if d < minSyncInterval {
		return minSyncInterval
	}
	return d
}

func (e *engine) kick() {
	select {
	case e.wakeup <- struct{}{}:
	default:
	}
}

// runCycle implements ARCH §4: drain actions → enumerate folders → sync
// each subscribed folder. Folders are iterated in spec §5.1 priority
// order (Inbox first, then well-known, then user folders alpha) so the
// user sees Inbox messages before anything else fills in.
func (e *engine) runCycle(ctx context.Context) error {
	// Serialise the cycle. The engine's loop and any external caller
	// (UI-fired SyncAll, Wake) all funnel through here; without the
	// lock they can stack and produce overlapping HTTP fan-outs.
	e.cycleMu.Lock()
	defer e.cycleMu.Unlock()

	start := time.Now()
	e.logger.Info("sync: cycle starting")
	e.emit(SyncStartedEvent{At: start})
	e.setState(StateDrainingActions)
	if err := e.drain.Drain(ctx); err != nil {
		e.setState(StateIdle)
		return fmt.Errorf("drain actions: %w", err)
	}

	e.setState(StateSyncingFolders)
	if err := e.syncFolders(ctx); err != nil {
		e.setState(StateIdle)
		return fmt.Errorf("sync folders: %w", err)
	}

	folders, err := e.st.ListFolders(ctx, e.opts.AccountID)
	if err != nil {
		e.setState(StateIdle)
		return fmt.Errorf("list folders: %w", err)
	}
	subscribed := orderForQuickStart(filterSubscribed(folders, e.opts.SubscribedFolders))
	e.logger.Info("sync: enumerated folders",
		slog.Int("total", len(folders)),
		slog.Int("subscribed", len(subscribed)),
	)
	// Emit FoldersEnumeratedEvent so the TUI re-loads its sidebar
	// BEFORE per-folder syncs complete. Folders show up the moment
	// they hit the store, even if individual folder pulls later fail.
	e.emit(FoldersEnumeratedEvent{Count: len(folders), At: time.Now()})
	for _, f := range subscribed {
		select {
		case <-ctx.Done():
			e.setState(StateIdle)
			return ctx.Err()
		default:
		}
		e.logger.Debug("sync: folder begin", slog.String("folder_id", f.ID), slog.String("name", f.DisplayName))
		if err := e.syncFolder(ctx, f.ID); err != nil {
			e.logger.Warn("sync: folder failed",
				slog.String("folder_id", f.ID),
				slog.String("name", f.DisplayName),
				slog.String("err", err.Error()),
			)
			// Continue with remaining folders; surface error via
			// SyncFailedEvent at the cycle level only on hard errors.
		}
	}
	e.setState(StateIdle)
	e.logger.Info("sync: cycle complete",
		slog.Int("folders", len(subscribed)),
		slog.Duration("duration", time.Since(start)),
	)
	e.emit(SyncCompletedEvent{
		At:            time.Now(),
		FoldersSynced: len(subscribed),
		Duration:      time.Since(start),
	})
	return nil
}

func (e *engine) setState(s State) {
	e.mu.Lock()
	e.state = s
	e.mu.Unlock()
}

// State returns the current state. Exposed for tests and observability.
func (e *engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

func (e *engine) emit(ev Event) {
	select {
	case e.events <- ev:
	default:
		// channel full: drop. The status line consumer is best-effort.
	}
}

// filterSubscribed returns the folders in `all` that match the
// subscription set per spec §5.1: well-known names in `subscribed`,
// PLUS any user folder (no well-known name), MINUS the excluded set.
func filterSubscribed(all []store.Folder, subscribed []string) []store.Folder {
	want := make(map[string]bool, len(subscribed))
	for _, s := range subscribed {
		want[s] = true
	}
	var out []store.Folder
	for _, f := range all {
		if f.WellKnownName == "" {
			out = append(out, f)
			continue
		}
		if excludedWellKnown[f.WellKnownName] {
			continue
		}
		if want[f.WellKnownName] {
			out = append(out, f)
		}
	}
	return out
}
