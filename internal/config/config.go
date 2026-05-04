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
	Account         AccountConfig         `toml:"account"`
	Cache           CacheConfig           `toml:"cache"`
	Sync            SyncConfig            `toml:"sync"`
	UI              UIConfig              `toml:"ui"`
	Bindings        BindingsConfig        `toml:"bindings"`
	Rendering       RenderingConfig       `toml:"rendering"`
	Logging         LoggingConfig         `toml:"logging"`
	Triage          TriageConfig          `toml:"triage"`
	Bulk            BulkConfig            `toml:"bulk"`
	Batch           BatchConfig           `toml:"batch"`
	Calendar        CalendarConfig        `toml:"calendar"`
	Search          SearchConfig          `toml:"search"`
	Pattern         PatternConfig         `toml:"pattern"`
	SavedSearch     SavedSearchSettings   `toml:"saved_search"`
	SavedSearches   []SavedSearchConfig   `toml:"saved_searches"`
	MailboxSettings MailboxSettingsConfig `toml:"mailbox_settings"`
	Compose         ComposeConfig         `toml:"compose"`
	CLI             CLIConfig             `toml:"cli"`
}

// CLIConfig owns the [cli] section (spec 14). Controls output format,
// colour, and interactive confirmation for non-TUI subcommands.
type CLIConfig struct {
	// DefaultOutput sets the default output format for all subcommands.
	// Valid values: "text", "json". Overridden by --output flag.
	DefaultOutput string `toml:"default_output"`
	// Color controls ANSI colour output. Values: "auto", "always", "never".
	Color string `toml:"color"`
	// ConfirmDestructiveInCLI mirrors [triage].confirm_permanent_delete
	// for the CLI path. When true, permanent-delete requires --yes or
	// an interactive prompt.
	ConfirmDestructiveInCLI bool `toml:"confirm_destructive_in_cli"`
	// ProgressBars controls progress output. Values: "auto", "always", "never".
	ProgressBars string `toml:"progress_bars"`
	// JSONCompact emits compacted (single-line) JSON instead of pretty-printed.
	JSONCompact bool `toml:"json_compact"`
	// ExportDefaultDir is the destination directory for `inkwell export`.
	// Tilde is expanded. Default ".".
	ExportDefaultDir string `toml:"export_default_dir"`
}

// MailboxSettingsConfig owns the [mailbox_settings] section (spec 13).
type MailboxSettingsConfig struct {
	// ConfirmOOOChange prompts the user before toggling out-of-office.
	ConfirmOOOChange bool `toml:"confirm_ooo_change"`
	// DefaultOOOAudience is the default external audience when enabling OOO.
	DefaultOOOAudience string `toml:"default_ooo_audience"`
	// OOOIndicator is the glyph shown in the status bar when OOO is active.
	OOOIndicator string `toml:"ooo_indicator"`
	// RefreshInterval controls how often mailbox settings are re-fetched.
	RefreshInterval time.Duration `toml:"refresh_interval"`
	// DefaultInternalMessage is the pre-populated internal reply body.
	DefaultInternalMessage string `toml:"default_internal_message"`
	// DefaultExternalMessage is the pre-populated external reply body.
	DefaultExternalMessage string `toml:"default_external_message"`
}

// ComposeConfig owns the [compose] section (spec 15 F-1). Controls
// attachment limits and the discard webLink TTL.
type ComposeConfig struct {
	// AttachmentMaxSizeMB is the per-file size limit for staged
	// attachments. Files larger than this are rejected with an error
	// before reaching the Graph API. Default 25 MB.
	AttachmentMaxSizeMB int `toml:"attachment_max_size_mb"`
	// MaxAttachments caps the number of staged attachments per draft.
	// Default 20.
	MaxAttachments int `toml:"max_attachments"`
	// WebLinkTTL controls how long the status-bar "press s to open
	// in Outlook" hint persists after a draft is saved. 0 disables
	// auto-clear. Default 30s.
	WebLinkTTL time.Duration `toml:"web_link_ttl"`
}

// PatternConfig owns the [pattern] section (spec 08 §13). Knobs
// that the Compile/Execute path reads at request time. Defaults
// match the spec table.
type PatternConfig struct {
	// LocalMatchLimit caps the local-SQL result set per spec
	// 08 §8 (LIMIT on the generated query).
	LocalMatchLimit int `toml:"local_match_limit"`
	// ServerCandidateLimit caps the TwoStage server candidate
	// fetch per spec 08 §11 — beyond this, the executor refuses
	// and tells the user to refine the pattern.
	ServerCandidateLimit int `toml:"server_candidate_limit"`
	// PreferLocalWhenOffline biases the strategy selector toward
	// LocalOnly when the network is down (spec 08 §7.2). v1 uses
	// this only when the binary is launched offline; the
	// engine's online state isn't yet plumbed into the planner.
	PreferLocalWhenOffline bool `toml:"prefer_local_when_offline"`
}

// SearchConfig owns the [search] section (spec 06 §7). Knobs the
// hybrid searcher reads at request time; defaults match the spec
// table so first-launch behaviour matches the doc.
type SearchConfig struct {
	// LocalFirst controls whether the local FTS5 branch is given
	// the head-start emit; today both branches always run, this
	// is forward-compat for a future "kick server only after
	// local empties" optimisation.
	LocalFirst bool `toml:"local_first"`
	// ServerSearchTimeout caps the Graph $search round-trip.
	// Server slowness past the timeout is reported as
	// `[server slow; partial results]` per spec 06 §8.
	ServerSearchTimeout time.Duration `toml:"server_search_timeout"`
	// DefaultResultLimit caps the merged result set (spec 06 §7).
	DefaultResultLimit int `toml:"default_result_limit"`
	// DebounceTyping is the delay between the last keystroke and
	// the search dispatch (spec 06 §5.1).
	DebounceTyping time.Duration `toml:"debounce_typing"`
	// MergeEmitThrottle is the minimum gap between merger
	// emissions (spec 06 §4.4 — avoids UI thrash when both
	// branches emit close together).
	MergeEmitThrottle time.Duration `toml:"merge_emit_throttle"`
	// DefaultSort: "received_desc" today; future "relevance" /
	// "score_desc" lift later.
	DefaultSort string `toml:"default_sort"`
}

// TriageConfig owns the [triage] section (spec 07). Knobs that are
// already wired through the engine (BodyCacheMax* / DoneActionsRetention)
// stay under [cache] for back-compat; this section adds the
// triage-flow controls.
type TriageConfig struct {
	// ArchiveFolder is the destination for the `a` keybinding.
	// Default "archive" (Graph well-known name). Set to a folder
	// display name to route to a custom archive folder.
	ArchiveFolder string `toml:"archive_folder"`
	// ConfirmThreshold: bulk operations affecting more than this
	// many messages require an explicit confirm step. 0 = always
	// confirm. Single-message triage always acts immediately.
	// Reserved for spec 09/10 bulk paths.
	ConfirmThreshold int `toml:"confirm_threshold"`
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
	// OptimisticUI applies changes locally before Graph confirms.
	// Default true. Disable only for debugging.
	OptimisticUI bool `toml:"optimistic_ui"`
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

// BatchConfig owns the [batch] section (spec 09). Engine-level knobs
// for $batch fan-out, retry, and bulk size limits.
type BatchConfig struct {
	// MaxPerBatch caps sub-requests per $batch call. Graph hard limit is 20.
	MaxPerBatch int `toml:"max_per_batch"`
	// Concurrency caps the number of $batch calls in flight simultaneously.
	Concurrency int `toml:"batch_concurrency"`
	// RequestTimeout is the timeout for a single $batch HTTP call.
	RequestTimeout time.Duration `toml:"batch_request_timeout"`
	// MaxRetriesPerSubrequest is the maximum 429-retry attempts per
	// individual sub-request.
	MaxRetriesPerSubrequest int `toml:"max_retries_per_subrequest"`
	// WarnThreshold: bulk operations with N≥this show a time estimate.
	WarnThreshold int `toml:"bulk_size_warn_threshold"`
	// HardMax: bulk operations exceeding this are refused.
	HardMax int `toml:"bulk_size_hard_max"`
}

// CalendarConfig owns the [calendar] section (spec 12). Matches the
// existing calendarAdapter TTL constant + the spec §6/§9 layout knobs.
type CalendarConfig struct {
	// LookaheadDays / LookbackDays bound the sync window. Spec 12
	// §5 defaults: lookahead 30 days, lookback 7 days.
	LookaheadDays int `toml:"lookahead_days"`
	LookbackDays  int `toml:"lookback_days"`
	// ShowDeclined: include events the user has declined. Default
	// false (spec 12 §6).
	ShowDeclined bool `toml:"show_declined"`
	// ShowTentative: include tentatively-accepted events. Default true.
	ShowTentative bool `toml:"show_tentative"`
	// CacheTTL: how long the modal trusts cached events before
	// re-fetching from Graph.
	CacheTTL time.Duration `toml:"cache_ttl"`
	// TimeZone is the IANA timezone name for event display. Empty
	// string means use the system local timezone.
	TimeZone string `toml:"time_zone"`
	// OnlineMeetingIndicator is the glyph shown next to events with
	// a join URL (spec 12 §6.1).
	OnlineMeetingIndicator string `toml:"online_meeting_indicator"`
	// NowIndicator is the glyph marking the currently-active event.
	NowIndicator string `toml:"now_indicator"`
	// SidebarShowDays controls how many days the sidebar calendar
	// section renders (today + N−1 more days).
	SidebarShowDays int `toml:"sidebar_show_days"`
}

// SavedSearchConfig is one [[saved_searches]] table entry. The pattern
// is the spec 08 source; it's parsed at UI init.
type SavedSearchConfig struct {
	Name    string `toml:"name"`
	Pattern string `toml:"pattern"`
}

// SavedSearchSettings owns the [saved_search] section (spec 11 §9).
// Operational knobs for the Manager: cache lifetime, refresh cadence,
// first-launch seeding, and the TOML mirror path.
type SavedSearchSettings struct {
	// CacheTTL is how long Evaluate results are reused before re-querying.
	CacheTTL time.Duration `toml:"cache_ttl"`
	// BackgroundRefreshInterval controls how often pinned-search counts
	// are refreshed in the sidebar even without user navigation.
	BackgroundRefreshInterval time.Duration `toml:"background_refresh_interval"`
	// SeedDefaults: if true, seed "Unread", "Flagged", "From me" on
	// first launch (when the saved_searches table is empty).
	SeedDefaults bool `toml:"seed_defaults"`
	// TOMLMirrorPath is the path written after every save/delete as a
	// human-readable snapshot (version-control friendly). Tilde is
	// expanded. Empty string disables the mirror.
	TOMLMirrorPath string `toml:"toml_mirror_path"`
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
	MaxConcurrent         int           `toml:"max_concurrent"`
	ForegroundInterval    time.Duration `toml:"foreground_interval"`
	BackgroundInterval    time.Duration `toml:"background_interval"`
	BackfillDays          int           `toml:"backfill_days"`
	MaxRetries            int           `toml:"max_retries"`
	SubscribedWellKnown   []string      `toml:"subscribed_well_known"`
	ExcludedFolders       []string      `toml:"excluded_folders"`
	DeltaPageSize         int           `toml:"delta_page_size"`
	RetryMaxBackoff       time.Duration `toml:"retry_max_backoff"`
	PrioritizeBodyFetches bool          `toml:"prioritize_body_fetches"`
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
	Theme               string        `toml:"theme"`
	UnreadIndicator     string        `toml:"unread_indicator"`
	FlagIndicator       string        `toml:"flag_indicator"`
	AttachmentIndicator string        `toml:"attachment_indicator"`
	TransientStatusTTL  time.Duration `toml:"transient_status_ttl"`
	MinTerminalCols     int           `toml:"min_terminal_cols"`
	MinTerminalRows     int           `toml:"min_terminal_rows"`
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
	// AttachmentSaveDir is the default destination for the a-z
	// attachment-save keybindings. Tilde is expanded to the user's
	// home directory. Default "~/Downloads".
	AttachmentSaveDir string `toml:"attachment_save_dir"`
	// LargeAttachmentWarnMB triggers a confirm modal before
	// downloading attachments larger than this many megabytes.
	// 0 disables the warning. Default 25.
	LargeAttachmentWarnMB int `toml:"large_attachment_warn_mb"`
	// WrapColumns is the target column for soft-wrapping in the
	// viewer body. 0 means use the computed pane width. Default 0.
	WrapColumns int `toml:"wrap_columns"`
	// QuoteCollapseThreshold: runs of quoted lines at depth ≥ this
	// value are collapsed to a single "[… N quoted lines]" summary.
	// 0 disables collapsing. Default 3.
	QuoteCollapseThreshold int `toml:"quote_collapse_threshold"`
	// StripPatterns is a list of regular expressions; any line
	// matching one is removed from the plain-text body before
	// rendering. When empty, built-in Outlook-noise patterns apply.
	StripPatterns []string `toml:"strip_patterns"`
	// HTMLConverter selects the HTML→text backend. "internal" (default)
	// uses jaytaylor/html2text; "external" spawns HTMLConverterCmd.
	HTMLConverter string `toml:"html_converter"`
	// HTMLConverterCmd is the command used when HTMLConverter == "external".
	// HTML is piped to stdin; plain text is read from stdout.
	HTMLConverterCmd string `toml:"html_converter_cmd"`
	// ExternalConverterTimeout caps the external-converter subprocess.
	// Default 5s. On timeout the renderer falls back to the internal path.
	ExternalConverterTimeout time.Duration `toml:"external_converter_timeout"`
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
