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

	interactiveResult AuthResult
	interactiveErr    error
	interactiveCalls  atomic.Int32
	// interactiveErrFirstCallOnly clears interactiveErr after the
	// first call so retries see the success path.
	interactiveErrFirstCallOnly bool
	// observedScopes captures the scope list of the last fallback
	// flow call (interactive or device code).
	observedScopes []string

	deviceResult AuthResult
	deviceErr    error
	deviceCalls  atomic.Int32
	// deviceErrFirstCallOnly clears deviceErr after the first call.
	deviceErrFirstCallOnly bool

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

func (f *fakeSource) AcquireTokenInteractive(_ context.Context, scopes []string) (AuthResult, error) {
	f.interactiveCalls.Add(1)
	f.mu.Lock()
	f.observedScopes = append([]string(nil), scopes...)
	err := f.interactiveErr
	if f.interactiveErrFirstCallOnly {
		f.interactiveErr = nil
	}
	f.mu.Unlock()
	if err != nil {
		return AuthResult{}, err
	}
	f.mu.Lock()
	f.accounts = append(f.accounts, f.interactiveResult.Account)
	f.mu.Unlock()
	return f.interactiveResult, nil
}

func (f *fakeSource) AcquireTokenByDeviceCode(ctx context.Context, scopes []string, prompt PromptFn) (AuthResult, error) {
	f.deviceCalls.Add(1)
	f.mu.Lock()
	f.observedScopes = append([]string(nil), scopes...)
	err := f.deviceErr
	if f.deviceErrFirstCallOnly {
		f.deviceErr = nil
	}
	f.mu.Unlock()
	if err := prompt(ctx, DeviceCodePrompt{
		UserCode:        "FAKECODE",
		VerificationURL: "https://example.invalid/devicelogin",
		ExpiresAt:       time.Now().Add(15 * time.Minute),
		Message:         "go to verification url and enter the code",
	}); err != nil {
		return AuthResult{}, err
	}
	if err != nil {
		return AuthResult{}, err
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

func TestModeAutoUsesInteractiveWhenNoAccount(t *testing.T) {
	src := &fakeSource{
		accounts: nil,
		interactiveResult: AuthResult{
			AccessToken: "tok-browser",
			ExpiresOn:   time.Now().Add(time.Hour),
			Account:     Account{UPN: "u@example.invalid", TenantID: "T"},
		},
	}
	a := newTestAuth(t, src, nil)

	tok, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-browser", tok)
	require.Equal(t, int32(1), src.interactiveCalls.Load(), "auto mode tries interactive first")
	require.Equal(t, int32(0), src.deviceCalls.Load(), "device code must NOT be invoked when interactive succeeds")
}

func TestModeAutoFallsBackToDeviceCodeOnLaunchError(t *testing.T) {
	prompted := false
	src := &fakeSource{
		interactiveErr: errors.New(`exec: "open": executable file not found in $PATH`),
		deviceResult: AuthResult{
			AccessToken: "tok-fallback",
			ExpiresOn:   time.Now().Add(time.Hour),
			Account:     Account{UPN: "u@example.invalid", TenantID: "T"},
		},
	}
	a := newTestAuth(t, src, func(_ context.Context, _ DeviceCodePrompt) error {
		prompted = true
		return nil
	})

	tok, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-fallback", tok)
	require.Equal(t, int32(1), src.interactiveCalls.Load())
	require.Equal(t, int32(1), src.deviceCalls.Load())
	require.True(t, prompted)
}

func TestModeAutoDoesNotFallBackOnAADError(t *testing.T) {
	src := &fakeSource{
		interactiveErr: errors.New("AADSTS50105: tenant blocks user"),
		deviceResult:   AuthResult{AccessToken: "should-not-be-used", ExpiresOn: time.Now().Add(time.Hour)},
	}
	a := newTestAuth(t, src, nil)

	_, err := a.Token(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "AADSTS50105")
	require.Equal(t, int32(0), src.deviceCalls.Load(), "AAD error must surface; do not silently fall back")
}

func TestModeInteractiveDoesNotFallBack(t *testing.T) {
	src := &fakeSource{
		interactiveErr: errors.New(`exec: "open": executable file not found in $PATH`),
		deviceResult:   AuthResult{AccessToken: "should-not-be-used", ExpiresOn: time.Now().Add(time.Hour)},
	}
	a := NewWithSource(Config{Mode: ModeInteractive}, nil, src)
	_, err := a.Token(context.Background())
	require.Error(t, err)
	require.Equal(t, int32(0), src.deviceCalls.Load(), "ModeInteractive must never invoke device code")
}

func TestModeDeviceCodeSkipsInteractive(t *testing.T) {
	src := &fakeSource{
		interactiveResult: AuthResult{AccessToken: "should-not-be-used"},
		deviceResult: AuthResult{
			AccessToken: "tok-dc",
			ExpiresOn:   time.Now().Add(time.Hour),
			Account:     Account{UPN: "u@example.invalid", TenantID: "T"},
		},
	}
	a := NewWithSource(Config{Mode: ModeDeviceCode}, func(context.Context, DeviceCodePrompt) error { return nil }, src)
	tok, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-dc", tok)
	require.Equal(t, int32(0), src.interactiveCalls.Load())
}

func TestTokenFallsBackInteractiveWhenSilentFails(t *testing.T) {
	src := &fakeSource{
		accounts:  []Account{{UPN: "u@example.invalid"}},
		silentErr: errors.New("refresh expired"),
		interactiveResult: AuthResult{
			AccessToken: "tok-i",
			ExpiresOn:   time.Now().Add(time.Hour),
			Account:     Account{UPN: "u@example.invalid", TenantID: "T"},
		},
	}
	a := newTestAuth(t, src, nil)
	tok, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-i", tok)
}

func TestParseSignInMode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want SignInMode
		ok   bool
	}{
		{"", ModeAuto, true},
		{"auto", ModeAuto, true},
		{"interactive", ModeInteractive, true},
		{"browser", ModeInteractive, true},
		{"device_code", ModeDeviceCode, true},
		{"device-code", ModeDeviceCode, true},
		{"deviceCode", ModeDeviceCode, true},
		{"banana", ModeAuto, false},
	} {
		got, err := ParseSignInMode(tc.in)
		if tc.ok {
			require.NoError(t, err, tc.in)
			require.Equal(t, tc.want, got, tc.in)
		} else {
			require.Error(t, err, tc.in)
		}
	}
}

func TestIsBrowserLaunchErrorClassification(t *testing.T) {
	require.True(t, isBrowserLaunchError(errors.New(`exec: "open": executable file not found in $PATH`)))
	require.True(t, isBrowserLaunchError(errors.New("DISPLAY environment variable not set; no display")))
	require.False(t, isBrowserLaunchError(nil))
	require.False(t, isBrowserLaunchError(errors.New("AADSTS50105: tenant blocks user")))
	require.False(t, isBrowserLaunchError(errors.New("user cancelled the auth flow")))
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
		interactiveResult: AuthResult{
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
		interactiveResult: AuthResult{
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
		interactiveResult: AuthResult{
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

func TestRetriesWithoutOfflineAccessOnDecline(t *testing.T) {
	src := &fakeSource{
		interactiveErr:              errors.New("token response failed because declined scopes are present: offline_access"),
		interactiveErrFirstCallOnly: true,
		interactiveResult: AuthResult{
			AccessToken: "tok-no-refresh",
			ExpiresOn:   time.Now().Add(time.Hour),
			Account:     Account{UPN: "u@example.invalid", TenantID: "T"},
		},
	}
	a := newTestAuth(t, src, nil)
	tok, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-no-refresh", tok)
	require.Equal(t, int32(2), src.interactiveCalls.Load(), "first call declined; second retried without offline_access")
	require.NotContains(t, src.observedScopes, "offline_access", "retry must strip offline_access")
}

func TestDoesNotRetryWhenOtherScopeDeclined(t *testing.T) {
	src := &fakeSource{
		interactiveErr:              errors.New("token response failed because declined scopes are present: Mail.ReadWrite"),
		interactiveErrFirstCallOnly: true,
	}
	a := newTestAuth(t, src, nil)
	_, err := a.Token(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Mail.ReadWrite")
	require.Equal(t, int32(1), src.interactiveCalls.Load(), "no retry: declined scope was not offline_access")
}

func TestDoesNotRetryWhenMultipleScopesDeclinedIncludingOfflineAccess(t *testing.T) {
	// If the tenant declined offline_access AND something else, the
	// "something else" is the real problem; do not silently swallow it.
	src := &fakeSource{
		interactiveErr:              errors.New("token response failed because declined scopes are present: offline_access, Mail.ReadWrite"),
		interactiveErrFirstCallOnly: true,
	}
	a := newTestAuth(t, src, nil)
	_, err := a.Token(context.Background())
	require.Error(t, err)
	require.Equal(t, int32(1), src.interactiveCalls.Load())
}

func TestRetrySurfacesSecondError(t *testing.T) {
	src := &fakeSource{
		interactiveErr: errors.New("token response failed because declined scopes are present: offline_access"),
		// interactiveErrFirstCallOnly is false → both calls fail.
		// The first error is the offline_access decline, the second
		// (a fresh, unrelated failure) must reach the user.
	}
	src.interactiveErr = errors.New("token response failed because declined scopes are present: offline_access")
	a := newTestAuth(t, src, nil)
	_, err := a.Token(context.Background())
	require.Error(t, err)
	require.Equal(t, int32(2), src.interactiveCalls.Load())
}

func TestIsOfflineAccessDeclinedClassification(t *testing.T) {
	require.True(t, isOfflineAccessDeclined(errors.New("token response failed because declined scopes are present: offline_access")))
	require.True(t, isOfflineAccessDeclined(errors.New("Token Response Failed Because Declined Scopes Are Present: offline_access")))
	require.True(t, isOfflineAccessDeclined(errors.New("declined scopes are present:  offline_access  ")))
	require.False(t, isOfflineAccessDeclined(nil))
	require.False(t, isOfflineAccessDeclined(errors.New("token response failed because declined scopes are present: Mail.Read")))
	require.False(t, isOfflineAccessDeclined(errors.New("token response failed because declined scopes are present: offline_access, Mail.Read")))
	require.False(t, isOfflineAccessDeclined(errors.New("AADSTS50105: tenant blocks user")))
	require.False(t, isOfflineAccessDeclined(errors.New("declined scopes are present:")))
}

func TestScopesWithoutDropsCaseInsensitive(t *testing.T) {
	in := []string{"Mail.Read", " offline_access ", "Calendars.Read", "OFFLINE_ACCESS"}
	got := scopesWithout(in, "offline_access")
	require.Equal(t, []string{"Mail.Read", "Calendars.Read"}, got)
	// Original slice unchanged.
	require.Equal(t, 4, len(in))
}
