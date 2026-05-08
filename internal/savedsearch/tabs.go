package savedsearch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/eugenelim/inkwell/internal/store"
)

// tabRefreshConcurrency caps the parallel goroutines used by
// CountTabs to refresh per-tab unread counts (spec 24 §4 / §8).
// 5 is a balance: enough to overlap the four-or-five-tab common
// case without overwhelming SQLite's WAL reader limit.
const tabRefreshConcurrency = 5

// Tabs returns the saved searches promoted to the spec 24 tab strip
// for this account, in display order. Wraps store.ListTabs.
func (m *Manager) Tabs(ctx context.Context) ([]store.SavedSearch, error) {
	return m.st.ListTabs(ctx, m.accountID)
}

// Promote attaches a saved search (by name) to the tab strip,
// appending at the end. Idempotent: if already a tab, returns the
// current tab order unchanged. Errors if name does not resolve.
// Logs only saved-search ID and assigned order — name is PII-adjacent
// so it is logged at DEBUG only (spec 24 §11).
func (m *Manager) Promote(ctx context.Context, name string) (int, error) {
	ss, err := m.Get(ctx, name)
	if err != nil {
		return 0, err
	}
	if ss == nil {
		return 0, fmt.Errorf("tab: no saved search named %q; run :rule save %s first", name, name)
	}
	tabs, err := m.Tabs(ctx)
	if err != nil {
		return 0, err
	}
	for _, t := range tabs {
		if t.ID == ss.ID {
			if t.TabOrder != nil {
				slog.Debug("tab.promote.idempotent", "name", t.Name)
				slog.Info("tab.promote.idempotent", "id", t.ID, "order", *t.TabOrder)
				return *t.TabOrder, nil
			}
		}
	}
	ids := make([]int64, 0, len(tabs)+1)
	for _, t := range tabs {
		ids = append(ids, t.ID)
	}
	ids = append(ids, ss.ID)
	if err := m.st.ApplyTabOrder(ctx, m.accountID, ids); err != nil {
		return 0, fmt.Errorf("tab promote: %w", err)
	}
	order := len(ids) - 1
	slog.Debug("tab.promote", "name", ss.Name)
	slog.Info("tab.promote", "id", ss.ID, "order", order)
	_ = m.writeTOMLMirror(ctx)
	return order, nil
}

// Demote removes a saved search from the tab strip. Idempotent for
// non-tabs. Reindex remaining tabs to keep dense ordering.
func (m *Manager) Demote(ctx context.Context, name string) error {
	ss, err := m.Get(ctx, name)
	if err != nil {
		return err
	}
	if ss == nil {
		return fmt.Errorf("tab: no saved search named %q", name)
	}
	tabs, err := m.Tabs(ctx)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(tabs))
	found := false
	for _, t := range tabs {
		if t.ID == ss.ID {
			found = true
			continue
		}
		ids = append(ids, t.ID)
	}
	if !found {
		return nil
	}
	if err := m.st.ApplyTabOrder(ctx, m.accountID, ids); err != nil {
		return fmt.Errorf("tab demote: %w", err)
	}
	slog.Debug("tab.demote", "name", ss.Name)
	slog.Info("tab.demote", "id", ss.ID)
	_ = m.writeTOMLMirror(ctx)
	return nil
}

// Reorder moves the tab at position `from` to position `to`. Both
// indices are 0-based against the current tab list. Returns an error
// if either is out of bounds.
func (m *Manager) Reorder(ctx context.Context, from, to int) error {
	tabs, err := m.Tabs(ctx)
	if err != nil {
		return err
	}
	n := len(tabs)
	if from < 0 || from >= n {
		return fmt.Errorf("tab: position %d out of range (have %d)", from, n)
	}
	if to < 0 || to >= n {
		return fmt.Errorf("tab: position %d out of range (have %d)", to, n)
	}
	if from == to {
		return nil
	}
	ids := make([]int64, 0, n)
	moving := tabs[from].ID
	for i, t := range tabs {
		if i == from {
			continue
		}
		ids = append(ids, t.ID)
	}
	// Insert `moving` at position `to`.
	out := make([]int64, 0, n)
	out = append(out, ids[:to]...)
	out = append(out, moving)
	out = append(out, ids[to:]...)
	if err := m.st.ApplyTabOrder(ctx, m.accountID, out); err != nil {
		return fmt.Errorf("tab reorder: %w", err)
	}
	slog.Info("tab.reorder", "from", from, "to", to)
	_ = m.writeTOMLMirror(ctx)
	return nil
}

// CountTabs returns the unread match count for each tab keyed by
// saved-search ID. Per-tab errors are swallowed — an unevaluable
// pattern surfaces as a missing key in the map (the UI renders `⚠`
// per spec 24 §5.5). Infrastructure errors propagate.
//
// Implementation: per-tab evaluation runs in parallel through a
// bounded errgroup (concurrency 5). Each goroutine reads only the
// Manager cache and the store; the store is safe under concurrent
// reads (spec 02 §3 WAL invariant).
func (m *Manager) CountTabs(ctx context.Context) (map[int64]int, error) {
	tabs, err := m.Tabs(ctx)
	if err != nil {
		return nil, err
	}
	if len(tabs) == 0 {
		return map[int64]int{}, nil
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(tabRefreshConcurrency)

	var mu sync.Mutex
	out := make(map[int64]int, len(tabs))
	for _, t := range tabs {
		t := t
		g.Go(func() error {
			r, err := m.Evaluate(gctx, t.Name, false)
			if err != nil {
				slog.Debug("tab.count.evaluate.error", "id", t.ID, "err", err)
				return nil
			}
			n, err := m.st.CountUnreadByIDs(gctx, m.accountID, r.MessageIDs)
			if err != nil {
				return err
			}
			mu.Lock()
			out[t.ID] = n
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}
