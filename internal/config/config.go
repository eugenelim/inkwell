// Package config loads and validates the application configuration.
//
// Layering at runtime (ARCH §11):
//  1. Compiled defaults from [Defaults].
//  2. User TOML at ~/.config/inkwell/config.toml overriding defaults.
//  3. Environment variables (selected keys).
//  4. CLI flags (per-invocation).
//
// The loader returns a fully-populated, validated [*Config] or an error.
// The app refuses to start on invalid config rather than silently falling
// back to defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the fully-populated runtime configuration.
type Config struct {
	Account   AccountConfig   `toml:"account"`
	Cache     CacheConfig     `toml:"cache"`
	Sync      SyncConfig      `toml:"sync"`
	UI        UIConfig        `toml:"ui"`
	Bindings  BindingsConfig  `toml:"bindings"`
	Rendering RenderingConfig `toml:"rendering"`
	Logging   LoggingConfig   `toml:"logging"`
}

// AccountConfig owns the [account] section (spec 01).
type AccountConfig struct {
	TenantID             string `toml:"tenant_id"`
	ClientID             string `toml:"client_id"`
	UPN                  string `toml:"upn"`
	SignInMode           string `toml:"signin_mode"` // auto | interactive | device_code
	RequestOfflineAccess bool   `toml:"request_offline_access"`
}

// CacheConfig owns the [cache] section (spec 02).
type CacheConfig struct {
	BodyCacheMaxCount    int           `toml:"body_cache_max_count"`
	BodyCacheMaxBytes    int64         `toml:"body_cache_max_bytes"`
	VacuumInterval       time.Duration `toml:"vacuum_interval"`
	DoneActionsRetention time.Duration `toml:"done_actions_retention"`
	MmapSizeBytes        int64         `toml:"mmap_size_bytes"`
	CacheSizeKB          int           `toml:"cache_size_kb"`
}

// SyncConfig owns the [sync] section (spec 03).
type SyncConfig struct {
	MaxConcurrent      int           `toml:"max_concurrent"`
	ForegroundInterval time.Duration `toml:"foreground_interval"`
	BackgroundInterval time.Duration `toml:"background_interval"`
	BackfillDays       int           `toml:"backfill_days"`
	MaxRetries         int           `toml:"max_retries"`
}

// UIConfig owns the [ui] section (spec 04).
type UIConfig struct {
	FoldersWidth        int           `toml:"folders_width"`
	ListWidth           int           `toml:"list_width"`
	RelativeDatesWithin time.Duration `toml:"relative_dates_within"`
	Timezone            string        `toml:"timezone"`
	// Theme is the named color scheme. One of: "default", "dark",
	// "light", "solarized-dark", "solarized-light", "high-contrast".
	// Unknown values fall back to "default" with a logged warning.
	Theme string `toml:"theme"`
}

// BindingsConfig owns the [bindings] section (spec 04).
type BindingsConfig struct {
	Quit            string `toml:"quit"`
	Help            string `toml:"help"`
	Cmd             string `toml:"cmd"`
	Search          string `toml:"search"`
	Refresh         string `toml:"refresh"`
	FocusFolders    string `toml:"focus_folders"`
	FocusList       string `toml:"focus_list"`
	FocusViewer     string `toml:"focus_viewer"`
	NextPane        string `toml:"next_pane"`
	PrevPane        string `toml:"prev_pane"`
	Up              string `toml:"up"`
	Down            string `toml:"down"`
	Left            string `toml:"left"`
	Right           string `toml:"right"`
	PageUp          string `toml:"page_up"`
	PageDown        string `toml:"page_down"`
	Home            string `toml:"home"`
	End             string `toml:"end"`
	Open            string `toml:"open"`
	MarkRead        string `toml:"mark_read"`
	MarkUnread      string `toml:"mark_unread"`
	ToggleFlag      string `toml:"toggle_flag"`
	Delete          string `toml:"delete"`
	PermanentDelete string `toml:"permanent_delete"`
	Archive         string `toml:"archive"`
	Move            string `toml:"move"`
	AddCategory     string `toml:"add_category"`
	RemoveCategory  string `toml:"remove_category"`
	Undo            string `toml:"undo"`
	UndoStack       string `toml:"undo_stack"`
	Filter          string `toml:"filter"`
	ClearFilter     string `toml:"clear_filter"`
	ApplyToFiltered string `toml:"apply_to_filtered"`
}

// RenderingConfig owns the [rendering] section (spec 05).
type RenderingConfig struct {
	ShowFullHeaders bool   `toml:"show_full_headers"`
	OpenBrowserCmd  string `toml:"open_browser_cmd"`
	HTMLMaxBytes    int    `toml:"html_max_bytes"`
}

// LoggingConfig owns the [logging] section.
type LoggingConfig struct {
	Level   string `toml:"level"`
	Path    string `toml:"path"`
	MaxSize int    `toml:"max_size_mb"`
}

// Load reads the user config file (if present), applies defaults for any
// missing keys, runs validation, and returns the merged [*Config].
func Load(path string) (*Config, error) {
	cfg := Defaults()
	if path == "" {
		return cfg, cfg.Validate()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, cfg.Validate()
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config %s: %w", path, err)
	}
	return cfg, nil
}

// DefaultPath returns the canonical user config path.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "inkwell", "config.toml")
}
