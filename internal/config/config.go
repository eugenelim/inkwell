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
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the fully-populated runtime configuration.
type Config struct {
	Account       AccountConfig       `toml:"account"`
	Cache         CacheConfig         `toml:"cache"`
	Sync          SyncConfig          `toml:"sync"`
	UI            UIConfig            `toml:"ui"`
	Bindings      BindingsConfig      `toml:"bindings"`
	Rendering     RenderingConfig     `toml:"rendering"`
	Logging       LoggingConfig       `toml:"logging"`
	Triage        TriageConfig        `toml:"triage"`
	Bulk          BulkConfig          `toml:"bulk"`
	Calendar      CalendarConfig      `toml:"calendar"`
	SavedSearches []SavedSearchConfig `toml:"saved_searches"`
}

// TriageConfig owns the [triage] section (spec 07). Knobs that are
// already wired through the engine (BodyCacheMax* / DoneActionsRetention)
// stay under [cache] for back-compat; this section adds the
// triage-flow controls.
type TriageConfig struct {
	// ConfirmThreshold: a single-message triage acts immediately;
	// no threshold here, kept for forward compat with bulk.
	// ConfirmPermanentDelete: always true today (the modal is hard-
	// coded). Surface the knob so a power user can opt out at their
	// own risk.
	ConfirmPermanentDelete bool `toml:"confirm_permanent_delete"`
	// UndoStackSize caps the per-session undo stack. 0 = unlimited
	// (the current default). Spec 07 §11.
	UndoStackSize int `toml:"undo_stack_size"`
	// RecentFoldersCount is the cap on the move-picker MRU list
	// (spec 07 §12.1). The folder picker surfaces the N most-
	// recently-used move destinations above the alphabetical list
	// so frequent destinations stay one keystroke away. 0 disables
	// the recent section entirely. Default 5.
	RecentFoldersCount int `toml:"recent_folders_count"`
}

// BulkConfig owns the [bulk] section (spec 09 / 10).
type BulkConfig struct {
	// ProgressThreshold: a bulk operation with N≥this messages
	// shows the progress modal (spec 10 §7).
	ProgressThreshold int `toml:"progress_threshold"`
	// PreviewSampleSize: when the user hits `[p] Preview` on the
	// confirm modal, show this many messages (spec 10 §5.4).
	PreviewSampleSize int `toml:"preview_sample_size"`
	// SizeWarnThreshold / SizeHardMax: spec 09 §11.
	SizeWarnThreshold int `toml:"size_warn_threshold"`
	SizeHardMax       int `toml:"size_hard_max"`
	// DryRunDefault: when true, `:filter --apply` requires `!`
	// suffix to actually mutate (spec 10 §6).
	DryRunDefault bool `toml:"dry_run_default"`
}

// CalendarConfig owns the [calendar] section (spec 12). Matches the
// existing calendarAdapter TTL constant + the spec §6 layout knobs.
type CalendarConfig struct {
	// LookaheadDays / LookbackDays bound the cached window. Spec 12
	// §5 default is 1 day each side — modal shows today only.
	LookaheadDays int `toml:"lookahead_days"`
	LookbackDays  int `toml:"lookback_days"`
	// ShowDeclined: include events the user has declined. Default
	// false (spec 12 §6).
	ShowDeclined bool `toml:"show_declined"`
	// CacheTTL: how long the modal trusts cached events before
	// re-fetching from Graph. Matches calendarAdapter's constant.
	CacheTTL time.Duration `toml:"cache_ttl"`
}

// SavedSearchConfig is one [[saved_searches]] table entry. The pattern
// is the spec 08 source; it's parsed at UI init.
type SavedSearchConfig struct {
	Name    string `toml:"name"`
	Pattern string `toml:"pattern"`
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
	Filter          string `toml:"filter"`
	ClearFilter     string `toml:"clear_filter"`
	ApplyToFiltered string `toml:"apply_to_filtered"`
	Unsubscribe     string `toml:"unsubscribe"`
}

// RenderingConfig owns the [rendering] section (spec 05).
type RenderingConfig struct {
	ShowFullHeaders bool   `toml:"show_full_headers"`
	OpenBrowserCmd  string `toml:"open_browser_cmd"`
	HTMLMaxBytes    int    `toml:"html_max_bytes"`
	// URLDisplayMaxWidth caps the visible OSC 8 hyperlink text in
	// the viewer body at N cells with end-truncation
	// (`https://example.com/auth/…`). The OSC 8 url-portion stays
	// full so Cmd-click + the URL picker still open the full URL,
	// and the trailing `Links:` block always shows untruncated
	// URLs. 0 disables truncation. Default 60 (set in
	// internal/config/defaults.go).
	URLDisplayMaxWidth int `toml:"url_display_max_width"`
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
	// #nosec G304 — path is the user's TOML config (~/Library/Application Support/inkwell/config.toml or --config flag). Single-user desktop tool; the user owns the path.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, cfg.Validate()
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	md, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	// Spec 04 §17: unknown keys in [bindings] (or anywhere) are a
	// startup error. Without this gate a typo like `mark_red = "r"`
	// would silently no-op — the binding stays default and the user
	// can't tell why their override didn't take. Surface as a typed
	// error with the offending key path so the user can fix the
	// TOML in seconds.
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		names := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			names = append(names, k.String())
		}
		return nil, fmt.Errorf("config %s: unknown key(s): %s", path, strings.Join(names, ", "))
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
