package config

import (
	"errors"
	"fmt"
	"strings"
)

// Validate reports the first invalid or missing field in c.
func (c *Config) Validate() error {
	var errs []string
	if c.Account.TenantID == "" {
		errs = append(errs, "account.tenant_id is required")
	}
	if c.Account.ClientID == "" {
		errs = append(errs, "account.client_id is required")
	}
	if c.Account.UPN == "" {
		errs = append(errs, "account.upn is required")
	}
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

// ValidateForAccountless skips required-account fields. Used by CLI
// subcommands that do not need credentials (e.g., `inkwell --version`).
func (c *Config) ValidateForAccountless() error {
	stash := c.Account
	c.Account = AccountConfig{TenantID: "stub", ClientID: "stub", UPN: "stub@stub"}
	err := c.Validate()
	c.Account = stash
	return err
}
