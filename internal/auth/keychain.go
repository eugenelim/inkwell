package auth

import (
	"context"
	"errors"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/zalando/go-keyring"
)

// keychainCache adapts MSAL's [cache.ExportReplace] to the macOS Keychain
// via go-keyring. A Generic Password item is stored under
// service=Service, account=tenant:client. First-run access prompts the
// user via macOS Keychain ACLs, which is expected.
type keychainCache struct {
	account string
}

// Replace satisfies [cache.ExportReplace].
func (k *keychainCache) Replace(_ context.Context, c cache.Unmarshaler, _ cache.ReplaceHints) error {
	blob, err := keyring.Get(Service, k.account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	return c.Unmarshal([]byte(blob))
}

// Export satisfies [cache.ExportReplace].
func (k *keychainCache) Export(_ context.Context, c cache.Marshaler, _ cache.ExportHints) error {
	blob, err := c.Marshal()
	if err != nil {
		return err
	}
	return keyring.Set(Service, k.account, string(blob))
}
