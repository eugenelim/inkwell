package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultsValidateWithRequiredAccount(t *testing.T) {
	c := Defaults()
	err := c.Validate()
	require.Error(t, err, "defaults should not validate without account fields")
	require.Contains(t, err.Error(), "tenant_id")
}

func TestValidateForAccountless(t *testing.T) {
	c := Defaults()
	require.NoError(t, c.ValidateForAccountless())
}

func TestLoadMissingFileFallsBackToDefaultsAndFailsOnAccount(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	require.Error(t, err)
}

func TestLoadParsesUserOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, writeFile(path, `
[account]
tenant_id = "TENANT"
client_id = "CLIENT"
upn = "user@example.invalid"

[sync]
max_concurrent = 6
`))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "TENANT", cfg.Account.TenantID)
	require.Equal(t, 6, cfg.Sync.MaxConcurrent)
	require.Equal(t, 500, cfg.Cache.BodyCacheMaxCount, "default preserved when key absent")
}

func TestValidateRejectsMaxConcurrentOutOfRange(t *testing.T) {
	c := Defaults()
	c.Account = AccountConfig{TenantID: "T", ClientID: "C", UPN: "u@x.invalid"}
	c.Sync.MaxConcurrent = 99
	require.Error(t, c.Validate())
	c.Sync.MaxConcurrent = 0
	require.Error(t, c.Validate())
}

func TestValidateRejectsBadLogLevel(t *testing.T) {
	c := Defaults()
	c.Account = AccountConfig{TenantID: "T", ClientID: "C", UPN: "u@x.invalid"}
	c.Logging.Level = "spam"
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "logging.level")
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
