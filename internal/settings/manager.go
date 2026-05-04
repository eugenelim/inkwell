package settings

import (
	"context"
	"fmt"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
)

// Fetcher is the graph surface the Manager consumes. Defined at the
// consumer site so the settings package doesn't import graph's full
// type surface transitively.
type Fetcher interface {
	GetMailboxSettings(ctx context.Context) (*graph.MailboxSettings, error)
}

// Manager caches mailbox settings and resolves the effective timezone.
// No background goroutine — the UI drives Refresh via tea.Cmd.
type Manager struct {
	fetcher  Fetcher
	configTZ string
	cached   *graph.MailboxSettings
}

// New returns a Manager wired to fetcher. configTZ is the IANA timezone
// name from [calendar].time_zone config; empty means defer to mailbox
// or system.
func New(fetcher Fetcher, configTZ string) *Manager {
	return &Manager{fetcher: fetcher, configTZ: configTZ}
}

// Refresh fetches fresh settings from Graph and updates the cache.
// If the fetch fails the cached value is preserved.
func (m *Manager) Refresh(ctx context.Context) error {
	s, err := m.fetcher.GetMailboxSettings(ctx)
	if err != nil {
		return fmt.Errorf("settings: refresh: %w", err)
	}
	m.cached = s
	return nil
}

// GetCached returns the cached settings, or nil if not yet loaded.
func (m *Manager) GetCached() *graph.MailboxSettings {
	return m.cached
}

// ResolvedTimeZone returns the effective *time.Location with precedence:
//  1. configTZ (if non-empty and parseable)
//  2. mailboxSettings.TimeZone (if loaded and parseable)
//  3. time.Local (final fallback)
func (m *Manager) ResolvedTimeZone() *time.Location {
	if m.configTZ != "" {
		if loc, err := time.LoadLocation(m.configTZ); err == nil {
			return loc
		}
	}
	if m.cached != nil && m.cached.TimeZone != "" {
		if loc, err := time.LoadLocation(m.cached.TimeZone); err == nil {
			return loc
		}
	}
	return time.Local
}
