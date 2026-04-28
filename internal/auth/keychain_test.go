package auth

import (
	"context"
	"testing"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// fakeBlob implements both cache.Marshaler and cache.Unmarshaler so the
// keychain adapter can be tested in isolation.
type fakeBlob struct{ data []byte }

func (f *fakeBlob) Marshal() ([]byte, error)    { return f.data, nil }
func (f *fakeBlob) Unmarshal(b []byte) error    { f.data = append(f.data[:0], b...); return nil }

var _ cache.Marshaler = (*fakeBlob)(nil)
var _ cache.Unmarshaler = (*fakeBlob)(nil)

func TestKeychainCacheRoundTripsThroughMockedKeyring(t *testing.T) {
	keyring.MockInit()
	k := &keychainCache{account: keychainAccount("T", "C")}

	src := &fakeBlob{data: []byte(`{"opaque":"msal-cache-blob"}`)}
	require.NoError(t, k.Export(context.Background(), src, cache.ExportHints{}))

	dst := &fakeBlob{}
	require.NoError(t, k.Replace(context.Background(), dst, cache.ReplaceHints{}))
	require.Equal(t, string(src.data), string(dst.data))
}

func TestKeychainCacheReplaceWithoutEntryIsNoOp(t *testing.T) {
	keyring.MockInit()
	k := &keychainCache{account: keychainAccount("T", "C")}

	dst := &fakeBlob{data: []byte("preexisting-state")}
	require.NoError(t, k.Replace(context.Background(), dst, cache.ReplaceHints{}))
	require.Equal(t, "preexisting-state", string(dst.data), "Replace with no entry leaves caller's buffer untouched")
}
