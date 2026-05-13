package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultsValidateWithoutAccountConfig(t *testing.T) {
	c := Defaults()
	require.NoError(t, c.Validate(), "[account] is optional per PRD §4; defaults must validate")
	require.Equal(t, "common", c.Account.TenantID)
	require.Equal(t, "14d82eec-204b-4c2f-b7e8-296a70dab67e", c.Account.ClientID)
	require.Empty(t, c.Account.UPN, "UPN is populated at sign-in, not in defaults")
}

func TestValidateForAccountlessIsEquivalentToValidate(t *testing.T) {
	// Deprecated alias still works.
	c := Defaults()
	require.NoError(t, c.ValidateForAccountless())
}

func TestLoadMissingFileFallsBackToDefaultsAndPasses(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	require.NoError(t, err, "missing config file is fine; defaults are valid")
	require.Equal(t, "common", cfg.Account.TenantID)
}

func TestLoadParsesUserOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, writeFile(path, `
[account]
upn = "user@example.invalid"

[sync]
max_concurrent = 6
`))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "user@example.invalid", cfg.Account.UPN)
	require.Equal(t, "common", cfg.Account.TenantID, "untouched key keeps default")
	require.Equal(t, 6, cfg.Sync.MaxConcurrent)
	require.Equal(t, 500, cfg.Cache.BodyCacheMaxCount, "default preserved when key absent")
}

// TestLoadRejectsUnknownKey is the spec 04 §17 invariant: a typo
// in [bindings] (or anywhere else) must surface at startup with a
// typed error naming the offending key. Without this gate, the
// user's override silently no-ops and they can't tell why their
// rebinding didn't take.
func TestLoadRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, writeFile(path, `
[bindings]
mark_red = "x"
`))
	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown key")
	require.Contains(t, err.Error(), "mark_red")
}

// TestLoadAcceptsValidBindingsOverride is the happy-path
// counterpart to TestLoadRejectsUnknownKey.
func TestLoadAcceptsValidBindingsOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, writeFile(path, `
[bindings]
delete = "x"
unsubscribe = "U"
`))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "x", cfg.Bindings.Delete)
	require.Equal(t, "U", cfg.Bindings.Unsubscribe)
	// Untouched keys keep their defaults.
	require.Equal(t, "r", cfg.Bindings.MarkRead)
}

// TestLoadParsesNewSections verifies the [triage] / [bulk] /
// [calendar] sections added in PR 12 round-trip TOML → Config.
func TestConfigDecodeRulesSection(t *testing.T) {
	// Defaults.
	def := Defaults()
	require.Equal(t, "", def.Rules.File)
	require.Equal(t, 1*time.Hour, def.Rules.PullStaleThreshold)
	require.False(t, def.Rules.ASCIIFallback)
	require.True(t, def.Rules.ConfirmDestructive)
	require.True(t, def.Rules.EditorOpenAtRule)

	// Overrides.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, writeFile(path, `
[rules]
file = "/tmp/my-rules.toml"
pull_stale_threshold = "30m"
ascii_fallback = true
confirm_destructive = false
editor_open_at_rule = false
`))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "/tmp/my-rules.toml", cfg.Rules.File)
	require.Equal(t, 30*time.Minute, cfg.Rules.PullStaleThreshold)
	require.True(t, cfg.Rules.ASCIIFallback)
	require.False(t, cfg.Rules.ConfirmDestructive)
	require.False(t, cfg.Rules.EditorOpenAtRule)

	// Unknown key rejection.
	require.NoError(t, writeFile(path, `
[rules]
nope = "bad"
`))
	_, err = Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nope")
}

// TestConfigDecodeComposeBodyFormat asserts that the spec-33
// [compose] body_format key round-trips through TOML decoding for
// both valid values, and that the default is "plain".
func TestConfigDecodeComposeBodyFormat(t *testing.T) {
	// Default.
	def := Defaults()
	require.Equal(t, "plain", def.Compose.BodyFormat)

	// Override to markdown.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, writeFile(path, `
[compose]
body_format = "markdown"
`))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "markdown", cfg.Compose.BodyFormat)

	// Override to plain (explicit).
	require.NoError(t, writeFile(path, `
[compose]
body_format = "plain"
`))
	cfg, err = Load(path)
	require.NoError(t, err)
	require.Equal(t, "plain", cfg.Compose.BodyFormat)
}

// TestConfigValidateBodyFormatRejectsBadValue asserts that any
// value other than "plain" / "markdown" fails Validate with a
// useful message.
func TestConfigValidateBodyFormatRejectsBadValue(t *testing.T) {
	cfg := Defaults()
	cfg.Compose.BodyFormat = "rich-text"
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "compose.body_format")
	require.Contains(t, err.Error(), "rich-text")
}

func TestConfigValidateRulesRejectsPathTraversal(t *testing.T) {
	cfg := Defaults()
	cfg.Rules.File = "../../etc/passwd"
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "rules.file")
	require.Contains(t, err.Error(), "..")
}

func TestLoadParsesNewSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, writeFile(path, `
[triage]
confirm_permanent_delete = false
undo_stack_size = 100

[bulk]
progress_threshold = 25
size_hard_max = 10000

[calendar]
lookahead_days = 7
cache_ttl = "30m"
`))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.False(t, cfg.Triage.ConfirmPermanentDelete)
	require.Equal(t, 100, cfg.Triage.UndoStackSize)
	require.Equal(t, 25, cfg.Bulk.ProgressThreshold)
	require.Equal(t, 10000, cfg.Bulk.SizeHardMax)
	require.Equal(t, 7, cfg.Calendar.LookaheadDays)
	require.Equal(t, "30m0s", cfg.Calendar.CacheTTL.String())
}

// TestRenderingURLDisplayMaxWidthRoundTrip verifies the
// `[rendering].url_display_max_width` key parses through TOML and
// preserves the explicit-zero (truncation disabled) override —
// distinguishing user-set 0 from "unset → fall back to default".
// Without explicit values the default (60) ships from
// internal/config/defaults.go.
func TestRenderingURLDisplayMaxWidthRoundTrip(t *testing.T) {
	def := Defaults()
	require.Equal(t, 60, def.Rendering.URLDisplayMaxWidth, "default cap = 60 cells")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, writeFile(path, `
[rendering]
url_display_max_width = 0
`))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, 0, cfg.Rendering.URLDisplayMaxWidth,
		"explicit 0 must round-trip — disables truncation entirely")

	require.NoError(t, writeFile(path, `
[rendering]
url_display_max_width = 80
`))
	cfg, err = Load(path)
	require.NoError(t, err)
	require.Equal(t, 80, cfg.Rendering.URLDisplayMaxWidth)
}

func TestValidateRejectsMaxConcurrentOutOfRange(t *testing.T) {
	c := Defaults()
	c.Sync.MaxConcurrent = 99
	require.Error(t, c.Validate())
	c.Sync.MaxConcurrent = 0
	require.Error(t, c.Validate())
}

func TestValidateRejectsBadLogLevel(t *testing.T) {
	c := Defaults()
	c.Logging.Level = "spam"
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "logging.level")
}

func TestValidateBundleMinCountRange(t *testing.T) {
	c := Defaults()
	c.UI.BundleMinCount = -1
	require.Error(t, c.Validate())
	c.UI.BundleMinCount = 10000
	require.Error(t, c.Validate())
	c.UI.BundleMinCount = 0
	require.NoError(t, c.Validate(), "bundle_min_count=0 (off-switch) is valid")
	c.UI.BundleMinCount = 2
	require.NoError(t, c.Validate())
}

func TestValidateBundleIndicatorWidthClamp(t *testing.T) {
	c := Defaults()
	// CJK glyph (2 cells in one rune) is accepted.
	c.UI.BundleIndicatorCollapsed = "中"
	require.NoError(t, c.Validate())
	// Three-cell override is rejected.
	c.UI.BundleIndicatorCollapsed = "▶▶▶"
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle_indicator_collapsed")
}

func TestArchiveLabelDefaultIsArchive(t *testing.T) {
	c := Defaults()
	require.Equal(t, "archive", c.UI.ArchiveLabel)
	require.NoError(t, c.Validate())
}

func TestArchiveLabelAcceptsDone(t *testing.T) {
	c := Defaults()
	c.UI.ArchiveLabel = "done"
	require.NoError(t, c.Validate())
}

func TestArchiveLabelRejectsUnknownValue(t *testing.T) {
	for _, bad := range []string{"DONE", "Archive", "complete", "file"} {
		c := Defaults()
		c.UI.ArchiveLabel = bad
		err := c.Validate()
		require.Error(t, err, "label %q must be rejected", bad)
		require.Contains(t, err.Error(), "ui.archive_label")
	}
}

func TestArchiveLabelEmptyStringRejected(t *testing.T) {
	c := Defaults()
	c.UI.ArchiveLabel = ""
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "ui.archive_label")
}

func TestInboxSplitDefaultIsOff(t *testing.T) {
	c := Defaults()
	require.Equal(t, "off", c.Inbox.Split)
	require.NoError(t, c.Validate())
}

func TestInboxSplitAcceptsFocusedOther(t *testing.T) {
	c := Defaults()
	c.Inbox.Split = "focused_other"
	require.NoError(t, c.Validate())
}

func TestInboxSplitRejectsUnknownValue(t *testing.T) {
	for _, bad := range []string{"FOCUSED_OTHER", "complete", "split", "all"} {
		c := Defaults()
		c.Inbox.Split = bad
		err := c.Validate()
		require.Error(t, err, "value %q must be rejected", bad)
		require.Contains(t, err.Error(), "inbox.split")
	}
}

func TestInboxSplitRejectsEmptyString(t *testing.T) {
	c := Defaults()
	c.Inbox.Split = ""
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "inbox.split")
}

func TestInboxSplitDefaultSegmentDefault(t *testing.T) {
	c := Defaults()
	require.Equal(t, "focused", c.Inbox.SplitDefaultSegment)
	require.NoError(t, c.Validate())
}

func TestInboxSplitDefaultSegmentRejectsUnknown(t *testing.T) {
	for _, bad := range []string{"", "Focused", "auto", "either"} {
		c := Defaults()
		c.Inbox.SplitDefaultSegment = bad
		err := c.Validate()
		require.Error(t, err, "value %q must be rejected", bad)
		require.Contains(t, err.Error(), "inbox.split_default_segment")
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
