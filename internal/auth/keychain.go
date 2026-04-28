package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/zalando/go-keyring"
)

// Keychain size limits in zalando/go-keyring's `security` shellout cap
// the cache blob at ~3-4KB on Darwin. Microsoft 365 tokens with group
// claims regularly exceed that. We therefore store only a 32-byte
// AES-256 key in Keychain and persist the encrypted MSAL cache to a
// file under the user's app-support directory. See spec 01 §5.2.

// keyBytes is the size of the AES-256 key stored in Keychain.
const keyBytes = 32

// nonceBytes is the AES-GCM nonce length.
const nonceBytes = 12

// keychainCache adapts MSAL's [cache.ExportReplace] to a hybrid
// Keychain-key + encrypted-on-disk cache.
type keychainCache struct {
	account   string // service is the package-level Service constant
	cachePath string // absolute path to the encrypted blob
}

// newKeychainCache constructs a keychainCache for the given (tenant,
// client) composite key. cachePath defaults to
// ~/Library/Application Support/inkwell/msal_cache.bin if empty.
func newKeychainCache(account, cachePath string) *keychainCache {
	if cachePath == "" {
		cachePath = defaultCachePath()
	}
	return &keychainCache{account: account, cachePath: cachePath}
}

// defaultCachePath returns the canonical encrypted-cache file path.
func defaultCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "inkwell", "msal_cache.bin")
}

// Replace satisfies [cache.ExportReplace]. Loads the encrypted blob
// from disk, decrypts with the Keychain-stored key, and hands the
// plaintext to the MSAL Unmarshaler. Any of the recoverable
// "first-run" cases (no key, no file, decryption fails) returns nil
// and a logically-empty cache — sign-in re-creates everything.
func (k *keychainCache) Replace(_ context.Context, c cache.Unmarshaler, _ cache.ReplaceHints) error {
	key, err := k.readKey()
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil // first run on this machine
		}
		return fmt.Errorf("auth: read keychain key: %w", err)
	}
	ct, err := os.ReadFile(k.cachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // key set but no blob yet
		}
		return fmt.Errorf("auth: read cache file: %w", err)
	}
	pt, err := decryptAESGCM(ct, key)
	if err != nil {
		// Wrong key (rotated) or tampered file. Treat as empty so the
		// next sign-in succeeds; do not error out and brick the app.
		return nil
	}
	return c.Unmarshal(pt)
}

// Export satisfies [cache.ExportReplace]. Encrypts MSAL's serialised
// blob and writes it atomically to disk (mode 0600). Generates the
// Keychain key on the first call.
func (k *keychainCache) Export(_ context.Context, c cache.Marshaler, _ cache.ExportHints) error {
	pt, err := c.Marshal()
	if err != nil {
		return fmt.Errorf("auth: msal marshal: %w", err)
	}
	key, err := k.getOrCreateKey()
	if err != nil {
		return fmt.Errorf("auth: keychain key: %w", err)
	}
	ct, err := encryptAESGCM(pt, key)
	if err != nil {
		return fmt.Errorf("auth: encrypt cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(k.cachePath), 0o700); err != nil {
		return fmt.Errorf("auth: mkdir cache dir: %w", err)
	}
	if err := atomicWriteFile(k.cachePath, ct, 0o600); err != nil {
		return fmt.Errorf("auth: write cache file: %w", err)
	}
	return nil
}

// clear deletes both the Keychain key and the on-disk encrypted blob.
// Idempotent. Called by SignOut.
func (k *keychainCache) clear() error {
	if err := keyring.Delete(Service, k.account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("auth: delete keychain key: %w", err)
	}
	if err := os.Remove(k.cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("auth: delete cache file: %w", err)
	}
	return nil
}

// readKey returns the 32-byte AES-256 key from Keychain.
func (k *keychainCache) readKey() ([]byte, error) {
	enc, err := keyring.Get(Service, k.account)
	if err != nil {
		return nil, err
	}
	dec, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, fmt.Errorf("decode keychain key: %w", err)
	}
	if len(dec) != keyBytes {
		return nil, fmt.Errorf("keychain key has unexpected length %d (want %d)", len(dec), keyBytes)
	}
	return dec, nil
}

// getOrCreateKey returns the existing Keychain key or generates and
// stores a fresh one.
func (k *keychainCache) getOrCreateKey() ([]byte, error) {
	if got, err := k.readKey(); err == nil {
		return got, nil
	} else if !errors.Is(err, keyring.ErrNotFound) {
		return nil, err
	}
	key := make([]byte, keyBytes)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if err := keyring.Set(Service, k.account, base64.StdEncoding.EncodeToString(key)); err != nil {
		return nil, fmt.Errorf("set keychain key: %w", err)
	}
	return key, nil
}

// encryptAESGCM seals plaintext under key using AES-256-GCM. Output is
// nonce || sealed (sealed embeds the auth tag).
func encryptAESGCM(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceBytes)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// decryptAESGCM is the inverse of encryptAESGCM. Returns an error on
// any tampering / wrong key / truncation.
func decryptAESGCM(ciphertext, key []byte) ([]byte, error) {
	if len(ciphertext) < nonceBytes {
		return nil, errors.New("ciphertext too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := ciphertext[:nonceBytes]
	sealed := ciphertext[nonceBytes:]
	return gcm.Open(nil, nonce, sealed, nil)
}

// atomicWriteFile writes data to path atomically: write to a sibling
// temp file, then rename. The rename on POSIX is atomic so a crash
// mid-write cannot leave a half-written cache.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".msal_cache.bin-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op on success
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
