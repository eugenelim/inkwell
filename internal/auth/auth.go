package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/zalando/go-keyring"
)

// Service is the macOS Keychain service name. All MSAL cache blobs are
// stored under this service with a per-(tenant,client) account key.
const Service = "inkwell"

// proactiveRefreshWindow is the minimum lifetime a cached token must
// have before [Authenticator.Token] returns it without refreshing.
const proactiveRefreshWindow = 5 * time.Minute

// ErrNotSignedIn is returned by methods that require an active account.
var ErrNotSignedIn = errors.New("not signed in")

// SignInMode selects the fallback flow when the silent token path
// fails. See spec 01 §5.0.
type SignInMode int

const (
	// ModeAuto attempts the interactive system-browser flow first and
	// falls back to device code only when the browser fails to launch
	// (no `open` command, no display, headless SSH).
	ModeAuto SignInMode = iota
	// ModeInteractive forces the system-browser flow. Recommended for
	// managed-device tenants where Conditional Access requires the
	// device-compliance signal that only the OS enterprise SSO plug-in
	// can provide.
	ModeInteractive
	// ModeDeviceCode forces device-code flow. Useful for headless or
	// SSH usage on tenants that do not enforce a device-compliance
	// Conditional Access policy.
	ModeDeviceCode
)

// String returns the canonical lowercase name.
func (m SignInMode) String() string {
	switch m {
	case ModeInteractive:
		return "interactive"
	case ModeDeviceCode:
		return "device_code"
	default:
		return "auto"
	}
}

// ParseSignInMode converts a TOML / CLI string to a [SignInMode]. Empty
// string is ModeAuto.
func ParseSignInMode(s string) (SignInMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return ModeAuto, nil
	case "interactive", "browser":
		return ModeInteractive, nil
	case "device_code", "device-code", "devicecode":
		return ModeDeviceCode, nil
	}
	return ModeAuto, fmt.Errorf("auth: unknown signin_mode %q (auto|interactive|device_code)", s)
}

// Config is the auth-layer configuration. All fields are optional; the
// zero value is the supported production wiring (PRD §4):
// /common authority + Microsoft Graph CLI Tools client + the locked
// scope list from [DefaultScopes] + ModeAuto.
type Config struct {
	// TenantID overrides the authority. Empty or "common" → the
	// multi-tenant /common authority. Setting a specific tenant GUID
	// pins sign-in to that tenant.
	TenantID string
	// ClientID overrides the OAuth client. Empty → [PublicClientID].
	// Overriding is supported for tests; production should leave empty.
	ClientID string
	// Scopes is the list of OAuth scopes to request. Empty → [DefaultScopes].
	// Adding scopes is a code change reviewed against PRD §3.
	Scopes []string
	// Mode picks the sign-in flow when the silent path fails. The zero
	// value is [ModeAuto].
	Mode SignInMode
	// ExpectedUPN is an optional guardrail. When non-empty, the auth
	// layer refuses any sign-in whose resolved UPN does not case-
	// insensitively match.
	ExpectedUPN string
}

// resolved fills in defaults so the rest of the auth package can rely
// on non-empty values without re-checking.
func (c Config) resolved() Config {
	if c.ClientID == "" {
		c.ClientID = PublicClientID
	}
	if len(c.Scopes) == 0 {
		c.Scopes = DefaultScopes()
	}
	return c
}

// authority returns the MSAL authority URL for the configured tenant.
func (c Config) authority() string {
	t := strings.ToLower(strings.TrimSpace(c.TenantID))
	if t == "" || t == "common" {
		return CommonAuthority
	}
	return "https://login.microsoftonline.com/" + t
}

// PromptFn renders a device-code prompt to the user. The TUI implements
// this as a modal overlay; the CLI prints to stderr.
//
// PromptFn returns when the prompt has been shown; it does not poll for
// completion — MSAL handles polling internally.
type PromptFn func(ctx context.Context, p DeviceCodePrompt) error

// DeviceCodePrompt is the data displayed to the user during device code
// authentication.
type DeviceCodePrompt struct {
	UserCode        string
	VerificationURL string
	ExpiresAt       time.Time
	Message         string
}

// Authenticator is the only auth surface exposed to other packages.
type Authenticator interface {
	// Token returns a Graph access token. It refreshes silently when
	// possible and falls back to the device-code prompt when needed.
	// Safe to call concurrently; refresh attempts are serialised.
	Token(ctx context.Context) (string, error)

	// Invalidate drops the in-memory cached token, forcing the next
	// [Token] call to consult MSAL. Invoked by the auth transport on a
	// 401 from Graph (spec 03 §10.2).
	Invalidate()

	// SignOut removes the cached account and clears the Keychain entry.
	// Idempotent.
	SignOut(ctx context.Context) error

	// Account returns the signed-in account's UPN and tenant ID. The
	// signedIn return is false when no account is cached.
	Account() (upn, tenantID string, signedIn bool)
}

// New constructs an Authenticator backed by MSAL Go and the macOS
// Keychain. The zero Config value is supported and recommended (PRD §4).
func New(cfg Config, prompt PromptFn) (Authenticator, error) {
	cfg = cfg.resolved()
	src, err := newMSALSource(cfg)
	if err != nil {
		return nil, err
	}
	return NewWithSource(cfg, prompt, src), nil
}

// NewWithSource is the test-friendly constructor: it accepts a custom
// [TokenSource] implementation so tests can substitute a fake MSAL
// client. Production code should call [New].
func NewWithSource(cfg Config, prompt PromptFn, src TokenSource) Authenticator {
	cfg = cfg.resolved()
	if prompt == nil {
		prompt = noopPrompt
	}
	return &authenticator{
		cfg:    cfg,
		prompt: prompt,
		src:    src,
		now:    time.Now,
	}
}

type authenticator struct {
	cfg    Config
	prompt PromptFn
	src    TokenSource
	now    func() time.Time

	mu          sync.Mutex
	cachedToken string
	cachedExp   time.Time
	cachedAcct  Account
}

// Token implements [Authenticator].
func (a *authenticator) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cachedToken != "" && a.now().Add(proactiveRefreshWindow).Before(a.cachedExp) {
		return a.cachedToken, nil
	}

	accts, err := a.src.Accounts(ctx)
	if err != nil {
		return "", fmt.Errorf("list accounts: %w", err)
	}

	if len(accts) > 0 {
		res, err := a.src.AcquireTokenSilent(ctx, a.cfg.Scopes, accts[0])
		if err == nil {
			if err := a.checkAccount(res.Account); err != nil {
				return "", err
			}
			a.applyResult(res)
			return res.AccessToken, nil
		}
		// Silent failed (refresh-token expired, scope changed, etc.) —
		// fall through to device code. We deliberately do not log err
		// detail; the redactor handles MSAL strings, but we also avoid
		// even producing them.
	}

	res, err := a.acquireFallback(ctx)
	if err != nil {
		return "", err
	}
	if err := a.checkAccount(res.Account); err != nil {
		return "", err
	}
	a.applyResult(res)
	return res.AccessToken, nil
}

// acquireFallback runs the configured sign-in flow when silent token
// acquisition has failed. ModeAuto tries interactive first and falls
// back to device code only when the browser cannot be launched
// (spec 01 §5.0).
func (a *authenticator) acquireFallback(ctx context.Context) (AuthResult, error) {
	switch a.cfg.Mode {
	case ModeDeviceCode:
		res, err := a.src.AcquireTokenByDeviceCode(ctx, a.cfg.Scopes, a.prompt)
		if err != nil {
			return AuthResult{}, fmt.Errorf("device code auth: %w", err)
		}
		return res, nil
	case ModeInteractive:
		res, err := a.src.AcquireTokenInteractive(ctx, a.cfg.Scopes)
		if err != nil {
			return AuthResult{}, fmt.Errorf("interactive auth: %w", err)
		}
		return res, nil
	default:
		// ModeAuto: interactive first, fall back to device code only
		// on launch errors so the SSH / no-display case still works.
		res, ierr := a.src.AcquireTokenInteractive(ctx, a.cfg.Scopes)
		if ierr == nil {
			return res, nil
		}
		if !isBrowserLaunchError(ierr) {
			return AuthResult{}, fmt.Errorf("interactive auth: %w", ierr)
		}
		dres, derr := a.src.AcquireTokenByDeviceCode(ctx, a.cfg.Scopes, a.prompt)
		if derr != nil {
			return AuthResult{}, fmt.Errorf("device code fallback (after browser launch failed: %v): %w", ierr, derr)
		}
		return dres, nil
	}
}

// isBrowserLaunchError returns true when err looks like the OS could
// not open a browser (no `open` binary, no $DISPLAY, headless session).
// AAD / consent / network errors are deliberately NOT classified as
// launch errors — those bubble straight up so the user sees the real
// reason instead of a confusing "fell back to device code" path.
func isBrowserLaunchError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "exec:") && strings.Contains(msg, "executable file not found"):
		return true
	case strings.Contains(msg, "open command not found"):
		return true
	case strings.Contains(msg, "no $display"), strings.Contains(msg, "no display"):
		return true
	case strings.Contains(msg, "exec: \"open\""):
		return true
	}
	return false
}

// checkAccount enforces spec 01 §11 guardrails: refuse personal MSA
// accounts and refuse a UPN mismatch when ExpectedUPN was supplied.
func (a *authenticator) checkAccount(acct Account) error {
	if strings.EqualFold(acct.TenantID, ConsumerTenantID) {
		return errors.New("auth: personal Microsoft accounts are not supported; sign in with a work or school account")
	}
	if a.cfg.ExpectedUPN != "" && !strings.EqualFold(strings.TrimSpace(a.cfg.ExpectedUPN), strings.TrimSpace(acct.UPN)) {
		return fmt.Errorf("auth: signed-in account does not match configured account.upn")
	}
	return nil
}

// Invalidate implements [Authenticator].
func (a *authenticator) Invalidate() {
	a.mu.Lock()
	a.cachedToken = ""
	a.cachedExp = time.Time{}
	a.mu.Unlock()
}

// SignOut implements [Authenticator].
func (a *authenticator) SignOut(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	accts, err := a.src.Accounts(ctx)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	for _, acct := range accts {
		if err := a.src.RemoveAccount(ctx, acct); err != nil {
			return fmt.Errorf("remove account: %w", err)
		}
	}
	if err := keyring.Delete(Service, keychainAccount(a.cfg.TenantID, a.cfg.ClientID)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("clear keychain: %w", err)
	}
	a.cachedToken = ""
	a.cachedExp = time.Time{}
	a.cachedAcct = Account{}
	return nil
}

// Account implements [Authenticator].
func (a *authenticator) Account() (string, string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cachedAcct.UPN == "" {
		return "", "", false
	}
	return a.cachedAcct.UPN, a.cachedAcct.TenantID, true
}

func (a *authenticator) applyResult(res AuthResult) {
	a.cachedToken = res.AccessToken
	a.cachedExp = res.ExpiresOn
	a.cachedAcct = res.Account
}

// noopPrompt is used when the caller passes nil. The CLI mode is the
// only path where a meaningful prompt is currently registered; the TUI
// installs its own modal at startup.
func noopPrompt(_ context.Context, _ DeviceCodePrompt) error { return nil }

// keychainAccount is the per-(tenant,client) Keychain key. We use a
// composite so multi-account profiles (post-v1) collide cleanly with
// single-account today. Tenant defaults to "common" before key
// composition so the common-authority single-account install always
// hits the same Keychain entry across runs.
func keychainAccount(tenantID, clientID string) string {
	t := strings.ToLower(strings.TrimSpace(tenantID))
	if t == "" {
		t = "common"
	}
	c := strings.ToLower(strings.TrimSpace(clientID))
	return t + ":" + c
}

// newMSALSource builds the production TokenSource backed by MSAL Go.
// cfg is expected to be the resolved value (defaults applied).
func newMSALSource(cfg Config) (TokenSource, error) {
	cacheAdapter := &keychainCache{
		account: keychainAccount(cfg.TenantID, cfg.ClientID),
	}
	client, err := public.New(
		cfg.ClientID,
		public.WithAuthority(cfg.authority()),
		public.WithCache(cache.ExportReplace(cacheAdapter)),
	)
	if err != nil {
		return nil, fmt.Errorf("msal new: %w", err)
	}
	return &msalSource{client: &client}, nil
}
