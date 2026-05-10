package main

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/eugenelim/inkwell/internal/cli"
	"github.com/eugenelim/inkwell/internal/savedsearch"
	"github.com/eugenelim/inkwell/internal/store"
	isync "github.com/eugenelim/inkwell/internal/sync"
)

// watchOpts is the resolved option set runWatch consumes. Built from
// the cobra flag values in cmd_messages.go.
type watchOpts struct {
	folder         string
	filter         string
	rule           string
	all            bool
	output         string
	interval       time.Duration
	initial        int
	includeUpdated bool
	count          int
	forDuration    time.Duration
}

// minWatchInterval clamps --interval to the same lower bound the
// engine itself enforces (internal/sync/engine.go).
const minWatchInterval = 5 * time.Second

// authWindowDuration is the consecutive-AuthRequiredEvent wall-clock
// budget per spec 29 §5.4 — wide enough for an interactive
// device-code sign-in.
const authWindowDuration = 10 * time.Minute

// authRateLimitWindow is the per-message rate limit on the stderr
// "auth required" line; one line per minute (spec 29 §5.4).
const authRateLimitWindow = 60 * time.Second

// runWatchFromFlags is the cobra-side entry. It validates the
// mutually-exclusive flag matrix that cobra cannot express (the
// "--watch requires --filter OR --rule" rule), constructs the
// headless app, and dispatches into runWatch.
func runWatchFromFlags(ctx context.Context, rc *rootContext, opts watchOpts) error {
	if opts.filter == "" && opts.rule == "" {
		return usageErr(fmt.Errorf("--watch requires --filter <pattern> or --rule <name>"))
	}
	if opts.initial < 0 {
		return usageErr(fmt.Errorf("--initial must be >= 0"))
	}
	if opts.count < 0 {
		return usageErr(fmt.Errorf("--count must be >= 0"))
	}
	if opts.forDuration < 0 {
		return usageErr(fmt.Errorf("--for must be >= 0"))
	}

	cfg, err := rc.loadConfig()
	if err != nil {
		return err
	}
	if opts.interval == 0 {
		opts.interval = cfg.Sync.ForegroundInterval
	}
	if opts.interval > 0 && opts.interval < minWatchInterval {
		fmt.Fprintf(os.Stderr, "watch: --interval %s is below the engine minimum %s; using %s\n", opts.interval, minWatchInterval, minWatchInterval)
		opts.interval = minWatchInterval
	}
	if opts.interval == 0 {
		opts.interval = 30 * time.Second
	}

	app, err := buildHeadlessApp(ctx, rc)
	if err != nil {
		return err
	}
	defer app.Close()

	pattern := opts.filter
	if opts.rule != "" {
		mgr := savedsearch.New(app.store, app.account.ID, cfg.SavedSearch)
		ss, err := mgr.Get(ctx, opts.rule)
		if err != nil {
			return fmt.Errorf("rule lookup: %w", err)
		}
		if ss == nil {
			return cliExitf(cli.ExitNotFound, "not found: rule %q", opts.rule)
		}
		pattern = ss.Pattern
	}

	maxSeen := cfg.CLI.WatchMaxSeen
	if maxSeen <= 0 {
		maxSeen = 5000
	}

	deps := watchDeps{
		app:        app,
		pattern:    pattern,
		opts:       opts,
		maxSeen:    maxSeen,
		jsonOutput: effectiveOutput(rc, cfg) == "json" || opts.output == "json",
		quiet:      rc.quiet,
		noSync:     rc.noSync,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		now:        time.Now,
		startEngine: func(ctx context.Context) (isync.Engine, error) {
			return startWatchEngine(ctx, app, cfg.Sync.ForegroundInterval, cfg.Sync.BackgroundInterval)
		},
	}
	return runWatch(ctx, deps)
}

// watchDeps groups the runWatch inputs so unit tests can substitute
// a fake engine, a captured stdout, and a mock clock without taking
// the production buildHeadlessApp / isync.New path.
type watchDeps struct {
	app         *headlessApp
	pattern     string
	opts        watchOpts
	maxSeen     int
	jsonOutput  bool
	quiet       bool
	noSync      bool
	stdout      io.Writer
	stderr      io.Writer
	now         func() time.Time
	startEngine func(context.Context) (isync.Engine, error)
	signals     chan os.Signal // nil → install real handler
}

// watchSummary is the spec 29 §5.2 final summary printed on clean
// exit. Surfaced as a struct so tests can assert the counters
// without parsing the toast string.
type watchSummary struct {
	uptime       time.Duration
	emitted      int
	syncFailures int
	throttles    int
}

// runWatch is the spec 29 §5.3 loop — consume engine events,
// re-evaluate the filter, emit unseen matches. Returns a typed
// cliExitError on a clean exit so the main wrapper exits with the
// matching code.
func runWatch(ctx context.Context, d watchDeps) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	folderID := ""
	if !d.opts.all {
		fid, err := resolveFolder(ctx, d.app, d.opts.folder)
		if err != nil {
			return cliExitf(cli.ExitNotFound, "%v", err)
		}
		folderID = fid
	}

	seen := newSeenSet(d.maxSeen)
	startTime := d.now()
	deadline := time.Time{}
	if d.opts.forDuration > 0 {
		deadline = startTime.Add(d.opts.forDuration)
	}

	emitter := &watchEmitter{
		stdout:       d.stdout,
		stderr:       d.stderr,
		jsonOutput:   d.jsonOutput,
		quiet:        d.quiet,
		stderrIsTTY:  stderrIsTTY(),
		startTime:    startTime,
		emittedCount: new(atomic.Int64),
	}
	defer emitter.flushStatusLine()

	emitter.logStart(d.app.logger, d.pattern, d.opts.rule, folderID)

	// Pre-seed the seen-set with the entire matching set as of
	// startup. Without this the first safety-net tick emits every
	// existing match (the seen-set is empty), breaking the spec
	// 29 §5.3 contract that `--initial=0` "starts strictly from
	// 'now' — no backlog dump." The cap is the seen-set's
	// configured maxSeen, so a mailbox with more matches than the
	// cap will see eviction immediately — matches that fall off
	// the LRU may re-emit later, which the spec §5.5 trade-off
	// already documents.
	startupRows, err := runFilterListing(ctx, d.app, d.pattern, folderID, d.maxSeen)
	if err != nil {
		return cliExitf(cli.ExitUserError, "watch: %v", err)
	}
	for _, r := range startupRows {
		seen.add(r.ID, r.LastModifiedAt)
	}

	// Initial backlog dump (spec 29 §5.1 / §5.3). The startup rows
	// are already in seen, so the dump is informational only —
	// pick the top N from the same listing.
	if d.opts.initial > 0 {
		n := d.opts.initial
		if n > len(startupRows) {
			n = len(startupRows)
		}
		// startupRows is received_at DESC; emit oldest-first within
		// the top-N slice so the on-screen order matches `tail -f`
		// (newest at the bottom).
		for i := n - 1; i >= 0; i-- {
			r := startupRows[i]
			if err := emitter.emit(r); err != nil {
				return err
			}
		}
	}

	// Engine setup (spec 29 §5.6).
	var (
		eng     isync.Engine
		notifs  <-chan isync.Event
		engDone <-chan struct{}
	)
	if !d.noSync && d.startEngine != nil {
		e, err := d.startEngine(ctx)
		if err != nil {
			return fmt.Errorf("watch: engine start: %w", err)
		}
		eng = e
		notifs = eng.Notifications()
		engDone = eng.Done()
		eng.SetActive(true)
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer stopCancel()
			_ = eng.Stop(stopCtx)
		}()
	}

	// Signal plumbing (spec 29 §5.7).
	sigCh := d.signals
	if sigCh == nil {
		sigCh = make(chan os.Signal, 4)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		defer signal.Stop(sigCh)
	}
	var (
		summary      watchSummary
		firstSigAt   time.Time // wall-clock of the first signal — drives the 2s second-Ctrl-C grace
		authFirst    time.Time // start of the current consecutive auth-required window
		authLastWarn time.Time // last stderr warning timestamp (60s rate limit)
		safetyTimer  = time.NewTimer(d.opts.interval)
	)
	defer safetyTimer.Stop()
	emitter.statusUpdate(d.now(), seen.len(), 0, d.now(), "")

	for {
		select {
		case <-ctx.Done():
			summary.uptime = d.now().Sub(startTime)
			emitter.printSummary(summary)
			return nil
		case <-engDone:
			summary.uptime = d.now().Sub(startTime)
			emitter.printSummary(summary)
			return nil
		case sig := <-sigCh:
			now := d.now()
			// Spec 29 §5.7 — second SIGINT within 2s of the first
			// exits immediately with 130. Outside the 2s grace, a
			// second SIGINT is treated as the user changing their
			// mind during a slow shutdown — we still exit cleanly
			// with the summary.
			if sig == syscall.SIGINT && !firstSigAt.IsZero() && now.Sub(firstSigAt) <= 2*time.Second {
				return cliExitf(130, "")
			}
			if firstSigAt.IsZero() {
				firstSigAt = now
			}
			// Drain any concurrent second signal before the clean
			// exit path so a fast double Ctrl-C is honoured even
			// when both signals were already buffered in sigCh
			// when the loop reached the select.
			select {
			case sig2 := <-sigCh:
				if sig2 == syscall.SIGINT {
					return cliExitf(130, "")
				}
			default:
			}
			summary.uptime = d.now().Sub(startTime)
			emitter.printSummary(summary)
			return nil
		case <-safetyTimer.C:
			rows, err := runFilterListing(ctx, d.app, d.pattern, folderID, batchLimitForWatch())
			if err != nil {
				warn(d.stderr, fmt.Sprintf("! evaluate failed: %v", err))
				safetyTimer.Reset(d.opts.interval)
				continue
			}
			if n, eerr := emitNew(emitter, seen, rows, d.opts.includeUpdated); eerr != nil {
				return eerr
			} else {
				summary.emitted += n
			}
			safetyTimer.Reset(d.opts.interval)
		case ev, ok := <-notifs:
			if !ok {
				summary.uptime = d.now().Sub(startTime)
				emitter.printSummary(summary)
				return nil
			}
			switch e := ev.(type) {
			case isync.SyncStartedEvent:
				emitter.statusUpdate(d.now(), seen.len(), summary.emitted, d.now(), "in flight")
			case isync.SyncCompletedEvent:
				rows, err := runFilterListing(ctx, d.app, d.pattern, folderID, batchLimitForWatch())
				if err != nil {
					warn(d.stderr, fmt.Sprintf("! evaluate failed: %v", err))
					continue
				}
				if n, eerr := emitNew(emitter, seen, rows, d.opts.includeUpdated); eerr != nil {
					return eerr
				} else {
					summary.emitted += n
				}
				authFirst = time.Time{} // SyncCompleted resets the auth window
				safetyTimer.Reset(d.opts.interval)
				emitter.statusUpdate(d.now(), seen.len(), summary.emitted, d.now(), "synced")
			case isync.FolderSyncedEvent:
				if folderID != "" && e.FolderID != folderID {
					continue
				}
				rows, err := runFilterListing(ctx, d.app, d.pattern, folderID, batchLimitForWatch())
				if err != nil {
					warn(d.stderr, fmt.Sprintf("! evaluate failed: %v", err))
					continue
				}
				if n, eerr := emitNew(emitter, seen, rows, d.opts.includeUpdated); eerr != nil {
					return eerr
				} else {
					summary.emitted += n
				}
			case isync.SyncFailedEvent:
				summary.syncFailures++
				warn(d.stderr, fmt.Sprintf("! sync failed: %v", e.Err))
			case isync.AuthRequiredEvent:
				now := d.now()
				if authFirst.IsZero() {
					authFirst = now
				}
				if now.Sub(authFirst) >= authWindowDuration {
					return cliExitf(cli.ExitAuthError, "watch: auth required for ≥10m; run `inkwell signin` then re-launch")
				}
				if authLastWarn.IsZero() || now.Sub(authLastWarn) >= authRateLimitWindow {
					warn(d.stderr, "! auth required — run `inkwell signin` (then re-launch this watch)")
					authLastWarn = now
				}
			case isync.ThrottledEvent:
				summary.throttles++
				warn(d.stderr, fmt.Sprintf("! throttled — backing off for %s", e.RetryAfter))
			}
		}
		if d.opts.count > 0 && summary.emitted >= d.opts.count {
			summary.uptime = d.now().Sub(startTime)
			emitter.printSummary(summary)
			return nil
		}
		if !deadline.IsZero() && !d.now().Before(deadline) {
			summary.uptime = d.now().Sub(startTime)
			emitter.printSummary(summary)
			return nil
		}
	}
}

// batchLimitForWatch caps SearchByPredicate calls inside the watch
// loop. Large enough to cover a steady inflow over the foreground
// sync interval (default 30 s); the seen-set diff suppresses
// already-emitted rows so re-fetching a fixed batch is cheap.
func batchLimitForWatch() int { return 1000 }

// emitNew diffs rows against the seen-set and emits the new entries.
// Returns the number of emissions.
func emitNew(em *watchEmitter, seen *seenSet, rows []store.Message, includeUpdated bool) (int, error) {
	emitted := 0
	// Iterate oldest-first so the printed order matches arrival
	// order — the upstream listing is received_at DESC.
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		prev, ok := seen.get(r.ID)
		shouldEmit := !ok
		if !shouldEmit && includeUpdated && r.LastModifiedAt.After(prev) {
			shouldEmit = true
		}
		if !shouldEmit {
			continue
		}
		if err := em.emit(r); err != nil {
			return emitted, err
		}
		seen.add(r.ID, r.LastModifiedAt)
		emitted++
	}
	return emitted, nil
}

// watchEmitter centralises the stdout/stderr writes so the SIGPIPE
// recovery path is one place. JSON mode emits one object per line
// (JSONL); text mode prints a header on the first emission only and
// data rows after.
type watchEmitter struct {
	stdout       io.Writer
	stderr       io.Writer
	jsonOutput   bool
	quiet        bool
	stderrIsTTY  bool
	startTime    time.Time
	emittedCount *atomic.Int64
	headerDone   bool
	statusLast   string
}

func (em *watchEmitter) emit(m store.Message) error {
	if em.jsonOutput {
		b, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		b = append(b, '\n')
		if _, err := em.stdout.Write(b); err != nil {
			if isPipeClosed(err) {
				return cliExitf(cli.ExitOK, "")
			}
			return fmt.Errorf("write stdout: %w", err)
		}
	} else {
		if !em.headerDone {
			if _, err := fmt.Fprintf(em.stdout, "%-19s %-26s %s\n", "RECEIVED", "FROM", "SUBJECT"); err != nil {
				if isPipeClosed(err) {
					return cliExitf(cli.ExitOK, "")
				}
				return fmt.Errorf("write stdout: %w", err)
			}
			em.headerDone = true
		}
		from := m.FromName
		if from == "" {
			from = m.FromAddress
		}
		if _, err := fmt.Fprintf(em.stdout, "%-19s %-26s %s\n",
			m.ReceivedAt.Format("2006-01-02 15:04"),
			truncCLI(from, 26), m.Subject); err != nil {
			if isPipeClosed(err) {
				return cliExitf(cli.ExitOK, "")
			}
			return fmt.Errorf("write stdout: %w", err)
		}
	}
	em.emittedCount.Add(1)
	return nil
}

// statusUpdate redraws the rolling stderr status line. No-op when
// stderr is not a TTY or when --quiet is set. The line is plain
// text — never includes addresses or subjects (spec 29 §9 redaction).
func (em *watchEmitter) statusUpdate(now time.Time, seenLen, emitted int, lastSync time.Time, syncState string) {
	if em.quiet || !em.stderrIsTTY {
		return
	}
	uptime := now.Sub(em.startTime).Round(time.Second)
	state := "—"
	if !lastSync.IsZero() && syncState != "" {
		state = fmt.Sprintf("%s %s ago", syncState, now.Sub(lastSync).Round(time.Second))
	}
	line := fmt.Sprintf("⏱ watching: %d seen · %d emitted · last sync %s · uptime %s", seenLen, emitted, state, uptime)
	if line == em.statusLast {
		return
	}
	fmt.Fprintf(em.stderr, "\r\033[K%s", line)
	em.statusLast = line
}

// flushStatusLine clears the rolling status line on exit so the
// summary and the user's prompt land on a clean column.
func (em *watchEmitter) flushStatusLine() {
	if em.quiet || !em.stderrIsTTY || em.statusLast == "" {
		return
	}
	fmt.Fprint(em.stderr, "\r\033[K")
	em.statusLast = ""
}

func (em *watchEmitter) printSummary(s watchSummary) {
	if em.quiet {
		return
	}
	em.flushStatusLine()
	fmt.Fprintf(em.stderr, "✓ watched for %s — %d new matches, %d sync failures, %d throttle events\n",
		s.uptime.Round(time.Second), s.emitted, s.syncFailures, s.throttles)
}

func (em *watchEmitter) logStart(logger *slog.Logger, pattern, rule, folderID string) {
	if logger == nil {
		return
	}
	if rule != "" {
		logger.Info("watch: started", "rule", rule, "folder_id_set", folderID != "")
	} else {
		logger.Info("watch: started", "folder_id_set", folderID != "")
	}
	_ = pattern // pattern may include sender address fragments; never logged.
}

// warn prints a one-shot stderr line. Compatible with both TTY and
// pipe destinations (no `\r` rewrite).
func warn(w io.Writer, line string) {
	fmt.Fprintln(w, line)
}

// stderrIsTTY returns true when stderr is a terminal. Pure stdlib
// via os.Stderr.Stat() & os.ModeCharDevice (matching the screener
// CLI's stdinIsTTY helper — no x/term dependency).
func stderrIsTTY() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// isPipeClosed reports whether err is a stdout-broken-pipe — the
// reader on the other side of `| head -3` closing early. Watch
// treats this as a clean exit (spec 29 §5.7).
func isPipeClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPIPE) {
		return true
	}
	// Fallback: some Go versions wrap EPIPE inside os.PathError /
	// fmt errors that strip the syscall sentinel.
	return strings.Contains(err.Error(), "broken pipe")
}

// startWatchEngine is the production engine factory. Pulled out so
// tests can short-circuit without dialing Graph.
func startWatchEngine(ctx context.Context, app *headlessApp, foreground, background time.Duration) (isync.Engine, error) {
	eng, err := isync.New(app.graph, app.store, nil, isync.Options{
		AccountID:          app.account.ID,
		Logger:             app.logger,
		ForegroundInterval: foreground,
		BackgroundInterval: background,
	})
	if err != nil {
		return nil, err
	}
	if err := eng.Start(ctx); err != nil {
		return nil, err
	}
	return eng, nil
}

// seenSet is the bounded LRU of message IDs the watch loop has
// already emitted. Eviction policy: oldest-emitted entry first.
type seenSet struct {
	cap   int
	order *list.List // front = newest
	idx   map[string]*list.Element
}

type seenEntry struct {
	id    string
	stamp time.Time
}

func newSeenSet(cap int) *seenSet {
	if cap <= 0 {
		cap = 5000
	}
	return &seenSet{cap: cap, order: list.New(), idx: make(map[string]*list.Element, cap)}
}

func (s *seenSet) add(id string, stamp time.Time) {
	if elem, ok := s.idx[id]; ok {
		entry := elem.Value.(*seenEntry)
		if stamp.After(entry.stamp) {
			entry.stamp = stamp
		}
		s.order.MoveToFront(elem)
		return
	}
	elem := s.order.PushFront(&seenEntry{id: id, stamp: stamp})
	s.idx[id] = elem
	for s.order.Len() > s.cap {
		oldest := s.order.Back()
		if oldest == nil {
			break
		}
		entry := oldest.Value.(*seenEntry)
		delete(s.idx, entry.id)
		s.order.Remove(oldest)
	}
}

func (s *seenSet) get(id string) (time.Time, bool) {
	if elem, ok := s.idx[id]; ok {
		entry := elem.Value.(*seenEntry)
		return entry.stamp, true
	}
	return time.Time{}, false
}

func (s *seenSet) len() int { return s.order.Len() }

// cliExitError is the typed exit signal runWatch returns so main can
// translate it to the matching exit code without parsing strings.
type cliExitError struct {
	code int
	msg  string
}

func (e *cliExitError) Error() string { return e.msg }
func cliExitf(code int, format string, args ...any) error {
	return &cliExitError{code: code, msg: fmt.Sprintf(format, args...)}
}

// As lets errors.As find the typed value — main uses it to read the
// exit code from a wrapped runWatch error.
func (e *cliExitError) Code() int { return e.code }
