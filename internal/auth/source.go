package auth

import (
	"context"
	"time"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
)

// TokenSource is the seam between [authenticator] and MSAL Go. Production
// code uses [msalSource]; tests substitute a fake.
//
// All methods must be safe to call from a single goroutine (the
// [authenticator] serialises calls via its mutex).
type TokenSource interface {
	// Accounts lists cached accounts, or zero accounts on first run.
	Accounts(ctx context.Context) ([]Account, error)
	// AcquireTokenSilent returns a token using the cached refresh token.
	// Returns an error when no refresh path is available; callers fall
	// back to AcquireTokenByDeviceCode.
	AcquireTokenSilent(ctx context.Context, scopes []string, acct Account) (AuthResult, error)
	// AcquireTokenByDeviceCode runs the interactive device-code flow.
	// The implementation must invoke prompt with the user-visible code
	// before blocking on completion.
	AcquireTokenByDeviceCode(ctx context.Context, scopes []string, prompt PromptFn) (AuthResult, error)
	// RemoveAccount evicts acct from the MSAL cache.
	RemoveAccount(ctx context.Context, acct Account) error
}

// Account describes a signed-in user. UPN is the userPrincipalName;
// TenantID is the home tenant. msalKey is opaque and used by [msalSource]
// to find the underlying MSAL account on subsequent calls.
type Account struct {
	UPN      string
	TenantID string
	// msalKey is the MSAL HomeAccountID. Tests leave it empty.
	msalKey string
}

// AuthResult is the cross-implementation token result.
type AuthResult struct {
	AccessToken string
	ExpiresOn   time.Time
	Account     Account
}

// msalSource is the production TokenSource. It wraps a [public.Client].
type msalSource struct {
	client *public.Client
}

func (m *msalSource) Accounts(ctx context.Context) ([]Account, error) {
	accts, err := m.client.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Account, len(accts))
	for i, a := range accts {
		out[i] = Account{
			UPN:      a.PreferredUsername,
			TenantID: a.Realm,
			msalKey:  a.HomeAccountID,
		}
	}
	return out, nil
}

func (m *msalSource) AcquireTokenSilent(ctx context.Context, scopes []string, acct Account) (AuthResult, error) {
	msalAccts, err := m.client.Accounts(ctx)
	if err != nil {
		return AuthResult{}, err
	}
	var match public.Account
	for _, a := range msalAccts {
		if a.HomeAccountID == acct.msalKey {
			match = a
			break
		}
	}
	res, err := m.client.AcquireTokenSilent(ctx, scopes, public.WithSilentAccount(match))
	if err != nil {
		return AuthResult{}, err
	}
	return toAuthResult(res), nil
}

func (m *msalSource) AcquireTokenByDeviceCode(ctx context.Context, scopes []string, prompt PromptFn) (AuthResult, error) {
	dc, err := m.client.AcquireTokenByDeviceCode(ctx, scopes)
	if err != nil {
		return AuthResult{}, err
	}
	if err := prompt(ctx, DeviceCodePrompt{
		UserCode:        dc.Result.UserCode,
		VerificationURL: dc.Result.VerificationURL,
		ExpiresAt:       dc.Result.ExpiresOn,
		Message:         dc.Result.Message,
	}); err != nil {
		return AuthResult{}, err
	}
	res, err := dc.AuthenticationResult(ctx)
	if err != nil {
		return AuthResult{}, err
	}
	return toAuthResult(res), nil
}

func (m *msalSource) RemoveAccount(ctx context.Context, acct Account) error {
	msalAccts, err := m.client.Accounts(ctx)
	if err != nil {
		return err
	}
	for _, a := range msalAccts {
		if a.HomeAccountID == acct.msalKey {
			return m.client.RemoveAccount(ctx, a)
		}
	}
	return nil
}

func toAuthResult(r public.AuthResult) AuthResult {
	return AuthResult{
		AccessToken: r.AccessToken,
		ExpiresOn:   r.ExpiresOn,
		Account: Account{
			UPN:      r.Account.PreferredUsername,
			TenantID: r.Account.Realm,
			msalKey:  r.Account.HomeAccountID,
		},
	}
}
