package config

import (
	"errors"
	"fmt"
	"strings"
)

// Validate reports the first invalid or missing field in c.
//
// Note: [account] is entirely optional (PRD §4). TenantID / ClientID
// have safe defaults; UPN is populated by the auth layer at first
// sign-in. Validate does NOT require any of these.
func (c *Config) Validate() error {
	var errs []string
	if c.Cache.BodyCacheMaxCount <= 0 {
		errs = append(errs, "cache.body_cache_max_count must be > 0")
	}
	if c.Cache.BodyCacheMaxBytes <= 0 {
		errs = append(errs, "cache.body_cache_max_bytes must be > 0")
	}
	if c.Sync.MaxConcurrent < 1 || c.Sync.MaxConcurrent > 16 {
		errs = append(errs, "sync.max_concurrent must be between 1 and 16")
	}
	if c.Sync.ForegroundInterval <= 0 {
		errs = append(errs, "sync.foreground_interval must be > 0")
	}
	if c.Sync.BackgroundInterval <= 0 {
		errs = append(errs, "sync.background_interval must be > 0")
	}
	if c.Sync.BackfillDays <= 0 {
		errs = append(errs, "sync.backfill_days must be > 0")
	}
	if c.UI.FoldersWidth < 5 {
		errs = append(errs, "ui.folders_width must be ≥ 5")
	}
	if c.UI.ListWidth < 10 {
		errs = append(errs, "ui.list_width must be ≥ 10")
	}
	switch strings.ToLower(c.Logging.Level) {
	case "", "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Sprintf("logging.level %q invalid (debug|info|warn|error)", c.Logging.Level))
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ValidateForAccountless is retained for back-compatibility but is now
// equivalent to Validate; the [account] section is entirely optional.
//
// Deprecated: call Validate directly.
func (c *Config) ValidateForAccountless() error { return c.Validate() }
