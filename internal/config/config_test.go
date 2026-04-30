package config

import (
	"os"
	"path/filepath"
	"testing"

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

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
