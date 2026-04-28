package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// fakeSource is a recording, programmable [TokenSource] for tests.
type fakeSource struct {
	mu sync.Mutex

	accounts []Account

	silentResult AuthResult
	silentErr    error
	silentCalls  atomic.Int32

	deviceResult AuthResult
	deviceErr    error
	deviceCalls  atomic.Int32

	removeCalls atomic.Int32
}

func (f *fakeSource) Accounts(_ context.Context) ([]Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Account, len(f.accounts))
	copy(out, f.accounts)
	return out, nil
}

func (f *fakeSource) AcquireTokenSilent(_ context.Context, _ []string, _ Account) (AuthResult, error) {
	f.silentCalls.Add(1)
	if f.silentErr != nil {
		return AuthResult{}, f.silentErr
	}
	return f.silentResult, nil
}

func (f *fakeSource) AcquireTokenByDeviceCode(ctx context.Context, _ []string, prompt PromptFn) (AuthResult, error) {
	f.deviceCalls.Add(1)
	if err := prompt(ctx, DeviceCodePrompt{
		UserCode:        "FAKECODE",
		VerificationURL: "https://example.invalid/devicelogin",
		ExpiresAt:       time.Now().Add(15 * time.Minute),
		Message:         "go to verification url and enter the code",
	}); err != nil {
		return AuthResult{}, err
	}
	if f.deviceErr != nil {
		return AuthResult{}, f.deviceErr
	}
	f.mu.Lock()
	f.accounts = append(f.accounts, f.deviceResult.Account)
	f.mu.Unlock()
	return f.deviceResult, nil
}

func (f *fakeSource) RemoveAccount(_ context.Context, acct Account) error {
	f.removeCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.accounts[:0]
	for _, a := range f.accounts {
		if a.UPN != acct.UPN {
			out = append(out, a)
		}
	}
	f.accounts = out
	return nil
}

func newTestAuth(t *testing.T, src *fakeSource, prompt PromptFn) Authenticator {
	t.Helper()
	return NewWithSource(Config{TenantID: "T", ClientID: "C", Scopes: []string{"scope-a"}}, prompt, src)
}

func TestTokenReturnsCachedTokenWhenFar(t *testing.T) {
	src := &fakeSource{
		accounts:     []Account{{UPN: "u@example.invalid", TenantID: "T"}},
		silentResult: AuthResult{AccessToken: "tok-1", ExpiresOn: time.Now().Add(time.Hour), Account: Account{UPN: "u@example.invalid", TenantID: "T"}},
	}
	a := newTestAuth(t, src, nil)

	got, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-1", got)

	// Second call within window — should not re-call MSAL.
	got2, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-1", got2)
	require.Equal(t, int32(1), src.silentCalls.Load(), "second Token() should be served from cache")
}

func TestTokenRefreshesWhenWithinProactiveWindow(t *testing.T) {
	src := &fakeSource{
		accounts:     []Account{{UPN: "u@example.invalid"}},
		silentResult: AuthResult{AccessToken: "tok-near-expiry", ExpiresOn: time.Now().Add(2 * time.Minute)},
	}
	a := newTestAuth(t, src, nil)

	tok, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-near-expiry", tok)

	// Cached token is within 5-min window: next call must refresh.
	src.silentResult = AuthResult{AccessToken: "tok-fresh", ExpiresOn: time.Now().Add(time.Hour)}
	tok2, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-fresh", tok2)
	require.Equal(t, int32(2), src.silentCalls.Load())
}

func TestTokenFallsBackToDeviceCodeWhenNoAccount(t *testing.T) {
	var promptCalls atomic.Int32
	prompt := func(_ context.Context, p DeviceCodePrompt) error {
		promptCalls.Add(1)
		require.Equal(t, "FAKECODE", p.UserCode)
		return nil
	}
	src := &fakeSource{
		accounts:     nil,
		deviceResult: AuthResult{AccessToken: "tok-device", ExpiresOn: time.Now().Add(time.Hour), Account: Account{UPN: "u@example.invalid", TenantID: "T"}},
	}
	a := newTestAuth(t, src, prompt)

	tok, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-device", tok)
	require.Equal(t, int32(1), promptCalls.Load())
	upn, tenant, signed := a.Account()
	require.True(t, signed)
	require.Equal(t, "u@example.invalid", upn)
	require.Equal(t, "T", tenant)
}

func TestTokenFallsBackToDeviceWhenSilentFails(t *testing.T) {
	prompted := false
	src := &fakeSource{
		accounts:     []Account{{UPN: "u@example.invalid"}},
		silentErr:    errors.New("refresh expired"),
		deviceResult: AuthResult{AccessToken: "tok-d", ExpiresOn: time.Now().Add(time.Hour)},
	}
	a := newTestAuth(t, src, func(_ context.Context, _ DeviceCodePrompt) error {
		prompted = true
		return nil
	})
	tok, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-d", tok)
	require.True(t, prompted)
}

func TestInvalidateForcesReacquire(t *testing.T) {
	src := &fakeSource{
		accounts:     []Account{{UPN: "u@example.invalid"}},
		silentResult: AuthResult{AccessToken: "tok-1", ExpiresOn: time.Now().Add(time.Hour)},
	}
	a := newTestAuth(t, src, nil)

	_, err := a.Token(context.Background())
	require.NoError(t, err)

	a.Invalidate()
	src.silentResult = AuthResult{AccessToken: "tok-2", ExpiresOn: time.Now().Add(time.Hour)}
	tok, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-2", tok)
	require.Equal(t, int32(2), src.silentCalls.Load())
}

func TestConcurrentTokenSerialisesRefresh(t *testing.T) {
	src := &fakeSource{
		accounts:     []Account{{UPN: "u@example.invalid"}},
		silentResult: AuthResult{AccessToken: "tok", ExpiresOn: time.Now().Add(time.Hour)},
	}
	a := newTestAuth(t, src, nil)

	const N = 32
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := a.Token(context.Background())
			require.NoError(t, err)
			require.Equal(t, "tok", tok)
		}()
	}
	wg.Wait()
	// First caller acquires; the rest hit the cache. Exactly one
	// silent call total.
	require.Equal(t, int32(1), src.silentCalls.Load())
}

func TestSignOutRemovesAccountAndClearsKeychain(t *testing.T) {
	keyring.MockInit()
	require.NoError(t, keyring.Set(Service, keychainAccount("T", "C"), "fake-cache-blob"))

	src := &fakeSource{
		accounts:     []Account{{UPN: "u@example.invalid", TenantID: "T"}},
		silentResult: AuthResult{AccessToken: "tok", ExpiresOn: time.Now().Add(time.Hour)},
	}
	a := newTestAuth(t, src, nil)

	require.NoError(t, a.SignOut(context.Background()))
	require.Equal(t, int32(1), src.removeCalls.Load())

	_, err := keyring.Get(Service, keychainAccount("T", "C"))
	require.ErrorIs(t, err, keyring.ErrNotFound)

	_, _, signed := a.Account()
	require.False(t, signed)
}

func TestSignOutWhenNoKeychainEntryIsIdempotent(t *testing.T) {
	keyring.MockInit()
	src := &fakeSource{}
	a := newTestAuth(t, src, nil)
	require.NoError(t, a.SignOut(context.Background()))
}

func TestKeychainAccountKeyIsLowercased(t *testing.T) {
	require.Equal(t, "tenant:client", keychainAccount("TENANT", "Client"))
	require.Equal(t, "tenant:client", keychainAccount("  Tenant  ", " CLIENT "))
}

func TestNewWithEmptyConfigUsesPublicClientDefaults(t *testing.T) {
	a, err := New(Config{}, nil)
	require.NoError(t, err, "zero Config must construct using PublicClientID + /common")
	require.NotNil(t, a)
}

func TestConfigResolvedFillsDefaults(t *testing.T) {
	r := Config{}.resolved()
	require.Equal(t, PublicClientID, r.ClientID)
	require.NotEmpty(t, r.Scopes)
}

func TestConfigAuthorityHandlesEmptyAndCommonAndPinned(t *testing.T) {
	require.Equal(t, CommonAuthority, Config{}.authority())
	require.Equal(t, CommonAuthority, Config{TenantID: "common"}.authority())
	require.Equal(t, CommonAuthority, Config{TenantID: "  Common  "}.authority())
	require.Equal(t, "https://login.microsoftonline.com/12345678-1234-1234-1234-123456789abc",
		Config{TenantID: "12345678-1234-1234-1234-123456789abc"}.authority())
}

func TestRefusesConsumerMicrosoftAccount(t *testing.T) {
	src := &fakeSource{
		deviceResult: AuthResult{
			AccessToken: "tok",
			ExpiresOn:   time.Now().Add(time.Hour),
			Account: Account{
				UPN:      "personal@outlook.com",
				TenantID: ConsumerTenantID,
			},
		},
	}
	a := newTestAuth(t, src, nil)
	_, err := a.Token(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "personal Microsoft accounts")
	_, _, signed := a.Account()
	require.False(t, signed, "rejected sign-in must not populate Account()")
}

func TestExpectedUPNGuardrailRejectsMismatch(t *testing.T) {
	src := &fakeSource{
		deviceResult: AuthResult{
			AccessToken: "tok",
			ExpiresOn:   time.Now().Add(time.Hour),
			Account:     Account{UPN: "real@example.invalid", TenantID: "T"},
		},
	}
	a := NewWithSource(Config{ExpectedUPN: "expected@example.invalid"}, nil, src)
	_, err := a.Token(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match")
}

func TestExpectedUPNGuardrailAcceptsMatchCaseInsensitive(t *testing.T) {
	src := &fakeSource{
		deviceResult: AuthResult{
			AccessToken: "tok",
			ExpiresOn:   time.Now().Add(time.Hour),
			Account:     Account{UPN: "User@Example.Invalid", TenantID: "T"},
		},
	}
	a := NewWithSource(Config{ExpectedUPN: "user@example.invalid"}, nil, src)
	tok, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok", tok)
}

func TestDefaultScopesIncludesOfflineAccess(t *testing.T) {
	scopes := DefaultScopes()
	found := false
	for _, s := range scopes {
		if s == "offline_access" {
			found = true
		}
	}
	require.True(t, found, "offline_access scope must be present (spec 01 §6)")
}
