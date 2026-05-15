package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
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
	if c.Tabs.MaxNameWidth < 4 {
		errs = append(errs, "tabs.max_name_width must be ≥ 4")
	}
	if c.UI.FocusQueueLimit < 1 || c.UI.FocusQueueLimit > 1000 {
		errs = append(errs, "ui.focus_queue_limit must be between 1 and 1000")
	}
	if c.UI.BundleMinCount < 0 || c.UI.BundleMinCount > 9999 {
		errs = append(errs, "ui.bundle_min_count must be between 0 and 9999")
	}
	if w := runewidth.StringWidth(c.UI.BundleIndicatorCollapsed); w > 2 {
		errs = append(errs, fmt.Sprintf("ui.bundle_indicator_collapsed %q is %d display cells; must be ≤ 2", c.UI.BundleIndicatorCollapsed, w))
	}
	if w := runewidth.StringWidth(c.UI.BundleIndicatorExpanded); w > 2 {
		errs = append(errs, fmt.Sprintf("ui.bundle_indicator_expanded %q is %d display cells; must be ≤ 2", c.UI.BundleIndicatorExpanded, w))
	}
	switch c.UI.ArchiveLabel {
	case "archive", "done":
	default:
		// Reject empty + everything else. Spec 30 §4.1 strict literals.
		errs = append(errs, fmt.Sprintf("ui.archive_label %q must be one of \"archive\" or \"done\"", c.UI.ArchiveLabel))
	}
	switch c.Inbox.Split {
	case "off", "focused_other":
	default:
		errs = append(errs, fmt.Sprintf("inbox.split %q must be one of \"off\" or \"focused_other\"", c.Inbox.Split))
	}
	switch c.Inbox.SplitDefaultSegment {
	case "focused", "other", "none":
	default:
		errs = append(errs, fmt.Sprintf("inbox.split_default_segment %q must be one of \"focused\", \"other\", \"none\"", c.Inbox.SplitDefaultSegment))
	}
	if c.Rules.PullStaleThreshold < 0 {
		errs = append(errs, fmt.Sprintf("rules.pull_stale_threshold %s must be non-negative", c.Rules.PullStaleThreshold))
	}
	switch c.Compose.BodyFormat {
	case "", "plain", "markdown":
	default:
		errs = append(errs, fmt.Sprintf("compose.body_format %q must be \"plain\" or \"markdown\"", c.Compose.BodyFormat))
	}
	if strings.Contains(c.Rules.File, "..") {
		// Spec 17 path-traversal guard (mirrors attachment_save_dir).
		errs = append(errs, fmt.Sprintf("rules.file %q must not contain '..'", c.Rules.File))
	}
	switch strings.ToLower(c.Logging.Level) {
	case "", "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Sprintf("logging.level %q invalid (debug|info|warn|error)", c.Logging.Level))
	}
	switch strings.ToLower(strings.TrimSpace(c.Account.SignInMode)) {
	case "", "auto", "interactive", "browser", "device_code", "device-code", "devicecode":
	default:
		errs = append(errs, fmt.Sprintf("account.signin_mode %q invalid (auto|interactive|device_code)", c.Account.SignInMode))
	}
	// Spec 35 §7.1 body-index validation. Only enforced when enabled —
	// users with the feature off do not need to size the caps.
	if c.BodyIndex.Enabled {
		if c.BodyIndex.MaxCount <= 0 {
			errs = append(errs, "body_index.max_count must be > 0 when enabled")
		}
		if c.BodyIndex.MaxBytes <= 0 {
			errs = append(errs, "body_index.max_bytes must be > 0 when enabled")
		}
		if c.BodyIndex.MaxBodyBytes <= 0 {
			errs = append(errs, "body_index.max_body_bytes must be > 0 when enabled")
		} else if c.BodyIndex.MaxBytes > 0 && c.BodyIndex.MaxBodyBytes > c.BodyIndex.MaxBytes/8 {
			errs = append(errs, fmt.Sprintf("body_index.max_body_bytes (%d) must be ≤ max_bytes/8 (%d)",
				c.BodyIndex.MaxBodyBytes, c.BodyIndex.MaxBytes/8))
		}
		if c.BodyIndex.MaxRegexCandidates <= 0 {
			errs = append(errs, "body_index.max_regex_candidates must be > 0 when enabled")
		} else if c.BodyIndex.MaxCount > 0 && c.BodyIndex.MaxRegexCandidates > c.BodyIndex.MaxCount*2 {
			errs = append(errs, fmt.Sprintf("body_index.max_regex_candidates (%d) must be ≤ max_count×2 (%d)",
				c.BodyIndex.MaxRegexCandidates, c.BodyIndex.MaxCount*2))
		}
		if c.BodyIndex.RegexPostFilterTimeout <= 0 {
			errs = append(errs, "body_index.regex_post_filter_timeout must be > 0 when enabled")
		}
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
