package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// fakeBlob implements both cache.Marshaler and cache.Unmarshaler so the
// keychain adapter can be tested in isolation.
type fakeBlob struct{ data []byte }

func (f *fakeBlob) Marshal() ([]byte, error) { return f.data, nil }
func (f *fakeBlob) Unmarshal(b []byte) error { f.data = append(f.data[:0], b...); return nil }

var _ cache.Marshaler = (*fakeBlob)(nil)
var _ cache.Unmarshaler = (*fakeBlob)(nil)

func newCacheUnderTest(t *testing.T) *keychainCache {
	t.Helper()
	keyring.MockInit()
	return newKeychainCache(keychainAccount("T", "C"), filepath.Join(t.TempDir(), "msal_cache.bin"))
}

func TestKeychainCacheRoundTripEncryptedOnDisk(t *testing.T) {
	k := newCacheUnderTest(t)

	src := &fakeBlob{data: []byte(`{"opaque":"msal-cache-blob","tokens":["a","b","c"]}`)}
	require.NoError(t, k.Export(context.Background(), src, cache.ExportHints{}))

	// File exists; mode is 0600 (POSIX systems only).
	info, err := os.Stat(k.cachePath)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	// File contents are NOT plaintext — encryption discipline check.
	raw, err := os.ReadFile(k.cachePath)
	require.NoError(t, err)
	require.False(t, bytes.Contains(raw, src.data),
		"on-disk cache must be encrypted; raw bytes leaked plaintext")

	// Round trip through Replace.
	dst := &fakeBlob{}
	require.NoError(t, k.Replace(context.Background(), dst, cache.ReplaceHints{}))
	require.Equal(t, string(src.data), string(dst.data))
}

func TestKeychainCacheHandlesLargeBlobs(t *testing.T) {
	k := newCacheUnderTest(t)

	// 16 KB — well past zalando/go-keyring's 4 KB Darwin shellout cap.
	// The whole point of this design is to make this work.
	big := make([]byte, 16*1024)
	_, _ = io.ReadFull(rand.Reader, big)
	src := &fakeBlob{data: big}
	require.NoError(t, k.Export(context.Background(), src, cache.ExportHints{}))

	dst := &fakeBlob{}
	require.NoError(t, k.Replace(context.Background(), dst, cache.ReplaceHints{}))
	require.Equal(t, big, dst.data)
}

func TestKeychainCacheReplaceFirstRunNoKey(t *testing.T) {
	k := newCacheUnderTest(t)
	dst := &fakeBlob{data: []byte("preserved")}
	require.NoError(t, k.Replace(context.Background(), dst, cache.ReplaceHints{}),
		"no Keychain key on first run must be a clean no-op")
	require.Equal(t, "preserved", string(dst.data),
		"caller's buffer must be untouched when there's no cache to load")
}

func TestKeychainCacheReplaceMissingFile(t *testing.T) {
	k := newCacheUnderTest(t)
	// Pre-seed the Keychain key (as if we had Exported once and then
	// the cache file was deleted out from under us).
	_, err := k.getOrCreateKey()
	require.NoError(t, err)
	require.NoFileExists(t, k.cachePath)

	dst := &fakeBlob{data: []byte("preserved")}
	require.NoError(t, k.Replace(context.Background(), dst, cache.ReplaceHints{}))
	require.Equal(t, "preserved", string(dst.data))
}

func TestKeychainCacheReplaceWithRotatedKeyTreatsAsEmpty(t *testing.T) {
	// Export under one key, then forcibly rotate it. Replace must NOT
	// error — the user can recover by re-signing in. Erroring here
	// would brick the app on key rotation.
	k := newCacheUnderTest(t)
	require.NoError(t, k.Export(context.Background(), &fakeBlob{data: []byte("v1")}, cache.ExportHints{}))

	// Rotate: write a new random key directly.
	require.NoError(t, keyring.Set(Service, k.account, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="))

	dst := &fakeBlob{data: []byte("untouched")}
	require.NoError(t, k.Replace(context.Background(), dst, cache.ReplaceHints{}))
	require.Equal(t, "untouched", string(dst.data),
		"decryption failure must surface as an empty cache, not an error")
}

func TestKeychainCacheClearDeletesBoth(t *testing.T) {
	k := newCacheUnderTest(t)
	require.NoError(t, k.Export(context.Background(), &fakeBlob{data: []byte("x")}, cache.ExportHints{}))
	require.FileExists(t, k.cachePath)

	require.NoError(t, k.clear())

	_, err := keyring.Get(Service, k.account)
	require.ErrorIs(t, err, keyring.ErrNotFound)
	require.NoFileExists(t, k.cachePath)
}

func TestKeychainCacheClearIsIdempotent(t *testing.T) {
	k := newCacheUnderTest(t)
	require.NoError(t, k.clear()) // never exported; both targets missing
	require.NoError(t, k.clear()) // running again is still fine
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, keyBytes)
	_, _ = io.ReadFull(rand.Reader, key)
	// Empty/nil plaintexts aren't a real input for MSAL caches.
	// Test the realistic shapes.
	for _, payload := range [][]byte{
		[]byte("hello"),
		bytes.Repeat([]byte("A"), 100*1024),
	} {
		ct, err := encryptAESGCM(payload, key)
		require.NoError(t, err)
		// Output is nonce(12) + sealed(>=tag(16)).
		require.GreaterOrEqual(t, len(ct), nonceBytes+16)
		// Two encryptions of the same plaintext must use different nonces.
		ct2, err := encryptAESGCM(payload, key)
		require.NoError(t, err)
		require.NotEqual(t, ct, ct2)
		pt, err := decryptAESGCM(ct, key)
		require.NoError(t, err)
		require.Equal(t, payload, pt)
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	key := make([]byte, keyBytes)
	_, _ = io.ReadFull(rand.Reader, key)
	ct, err := encryptAESGCM([]byte("important"), key)
	require.NoError(t, err)
	ct[len(ct)-1] ^= 0xFF // flip a bit in the auth tag
	_, err = decryptAESGCM(ct, key)
	require.Error(t, err, "GCM auth tag must catch tampering")
}

func TestDecryptRejectsTooShort(t *testing.T) {
	key := make([]byte, keyBytes)
	_, err := decryptAESGCM([]byte("nope"), key)
	require.Error(t, err)
}

func TestAtomicWriteFileLeavesNoTempOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.bin")
	require.NoError(t, atomicWriteFile(path, []byte("payload"), 0o600))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "payload", string(got))

	// No leftover temp file.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), ".msal_cache.bin-"),
			"atomicWriteFile must remove its temp file on success")
	}
}
