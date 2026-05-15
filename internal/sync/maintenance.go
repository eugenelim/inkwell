package sync

import (
	"context"
	"log/slog"
	"time"

	"github.com/eugenelim/inkwell/internal/store"
)

// runMaintenance is the spec 02 §8 housekeeping pass. Runs in its
// own goroutine off the engine's main timer so the foreground
// sync loop is never delayed by a long-running VACUUM. Each pass:
//
//  1. Body LRU eviction — drop cached bodies past the configured
//     count / bytes caps.
//  2. Done-actions sweep — delete done/failed action rows past
//     the retention window.
//  3. Optional VACUUM — only if VacuumOnMaintenance is set; the
//     SQLite VACUUM rewrites the whole DB, can be slow on large
//     mailboxes, and is rarely worth the I/O. Off by default.
//
// All steps are best-effort: failures are logged but don't stop
// the others. Returns once the pass completes or ctx cancels.
func (e *engine) runMaintenance(ctx context.Context) {
	if e.opts.MaintenanceInterval < 0 {
		// Sentinel for tests / explicit disable.
		return
	}

	// First pass runs after a short delay so we don't hammer the
	// disk on startup; subsequent passes follow the configured
	// interval.
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopped:
			return
		case <-timer.C:
		}
		e.maintenancePass(ctx)
		timer.Reset(e.opts.MaintenanceInterval)
	}
}

// maintenancePass executes one cycle of housekeeping. Exported for
// tests; production code calls it via the runMaintenance loop.
func (e *engine) maintenancePass(ctx context.Context) {
	start := time.Now()
	logger := e.logger.With(slog.String("op", "maintenance"))

	evicted, err := e.st.EvictBodies(ctx, e.opts.BodyCacheMaxCount, e.opts.BodyCacheMaxBytes)
	if err != nil {
		logger.Warn("body eviction failed", slog.String("err", err.Error()))
	} else if evicted > 0 {
		logger.Info("body LRU evicted", slog.Int("count", evicted))
	}

	// Spec 35 §6.4: body index has its own caps + LRU, independent of
	// the body LRU. Cap-driven eviction only — `inkwell index evict
	// --older-than=…` is the explicit time-based path.
	if e.opts.BodyIndexEnabled {
		indexed, err := e.st.EvictBodyIndex(ctx, store.EvictBodyIndexOpts{
			MaxCount: e.opts.BodyIndexMaxCount,
			MaxBytes: e.opts.BodyIndexMaxBytes,
		})
		if err != nil {
			logger.Warn("body index eviction failed", slog.String("err", err.Error()))
		} else if indexed > 0 {
			logger.Info("body index evicted", slog.Int("count", indexed))
		}
	}

	if e.opts.DoneActionsRetention > 0 {
		cutoff := time.Now().Add(-e.opts.DoneActionsRetention)
		swept, err := e.st.SweepDoneActions(ctx, cutoff)
		if err != nil {
			logger.Warn("action sweep failed", slog.String("err", err.Error()))
		} else if swept > 0 {
			logger.Info("done actions swept", slog.Int64("count", swept))
		}
	}

	if e.opts.VacuumOnMaintenance {
		if err := e.st.Vacuum(ctx); err != nil {
			logger.Warn("vacuum failed", slog.String("err", err.Error()))
		}
	}

	logger.Debug("maintenance pass complete", slog.Duration("elapsed", time.Since(start)))
}
