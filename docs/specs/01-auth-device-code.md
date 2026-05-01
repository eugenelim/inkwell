# Spec 01 — Authentication (interactive browser by default; device code as fallback)

**Status:** Shipped (CI scope, v0.2.x). Sign-in / sign-out / whoami flows + Keychain token storage all wired against the first-party Microsoft Graph CLI Tools client (PRD §4 / memory). Manual real-tenant smoke deferred per CLAUDE.md §5.5; AADSTS code classification + clock-skew detection + a CLI-mode device-code PromptFn remain on the audit-drain queue.
**Depends on:** PRD §4, ARCH §1, §2, §5.1.
**Blocks:** All other feature specs.
**Estimated effort:** 1–2 days.

---

## 1. Goal

Implement OAuth 2.0 device code flow against Microsoft Entra ID (Azure AD), backed by MSAL Go, with token persistence in the macOS Keychain. Provide a clean Go API to the rest of the codebase that hides MSAL details and exposes only:

- "Get me an access token for Graph, refreshing or re-authenticating as needed."
- "Sign out and clear local state."

## 2. Why interactive system browser (default) + device code (fallback)

The default is the **interactive system-browser flow** (auth code + PKCE with a localhost listener). On a managed Mac with the Microsoft Enterprise SSO plug-in for Apple Devices installed, this is the only flow that satisfies Conditional Access policies that require a compliant / managed device — the OS-level SSO plug-in injects device-attestation cookies into the browser auth session transparently, and AAD lets the sign-in succeed.

**Device code flow** is retained as an opt-in fallback for headless scenarios (SSH sessions, remote terminals without a display, automated CI). Device code flow CANNOT carry the device-compliance signal — the user types the code in *some* browser, possibly on a different machine entirely, so AAD has no way to prove the originating device is managed. Tenants with a compliant-device CA policy will reject device-code sign-ins regardless of the user's identity.

We don't use an embedded webview: shipping our own webview adds a heavy dependency, doesn't pick up the OS SSO plug-in, and is exactly the kind of thing locked-down corporate security tools flag as suspicious. The system browser is the right primitive — Safari is already trusted by both the user and the tenant.

Earlier drafts of this spec defaulted to device-code flow on the assumption that a TUI couldn't comfortably trigger a browser. In practice MSAL Go's `AcquireTokenInteractive` handles the localhost listener internally and `open(1)` launches Safari without disrupting the terminal session.

## 3. Tenant prerequisites

**Inkwell deliberately does not require an Entra ID app registration in the user's tenant** (PRD §4). Inkwell uses the well-known Microsoft Graph Command Line Tools first-party public client against the multi-tenant `/common` authority. The user's home tenant is **inferred** at sign-in from the MSAL `AuthResult`, not pre-configured.

Practical consequences:

- **No tenant-side onboarding step.** A user can install the binary, run `inkwell signin`, complete the device-code flow, and start using it. There is no `client_id` or `tenant_id` to obtain or paste into config.
- **Conditional Access still applies.** If the user's tenant requires device compliance (Intune), MFA, or the Microsoft Enterprise SSO plug-in for Apple devices, those policies still gate the sign-in. Inkwell inherits the posture; failures surface as user-readable AADSTS errors (§11).
- **Tenants that block the public client.** Some tenants explicitly disable user-consent for Microsoft-published apps or block the Microsoft Graph CLI Tools client under Conditional Access. Sign-in then fails with a specific AADSTS error. The user-facing message names the policy class; recovery is the tenant admin's call. We do not add a workaround.

Locked constants (defined in `internal/auth/scopes.go`):

```go
const (
    PublicClientID  = "14d82eec-204b-4c2f-b7e8-296a70dab67e" // Microsoft Graph Command Line Tools
    CommonAuthority = "https://login.microsoftonline.com/common"
)
```

Changing either of these constants is a code change, not user config.

## 4. Public Go API

Define in `internal/auth/auth.go`:

```go
package auth

type Authenticator interface {
    // Token returns a valid Graph access token, refreshing or prompting for
    // device-code re-auth as needed. Safe to call concurrently; serializes
    // refresh internally.
    Token(ctx context.Context) (string, error)

    // SignOut clears the cached account and tokens from Keychain.
    SignOut(ctx context.Context) error

    // Account returns the signed-in user's UPN and tenant ID, or zero values
    // if not signed in.
    Account() (upn string, tenantID string, signedIn bool)
}

// New constructs an Authenticator. PromptFn is called when interactive
// device-code auth is needed; the caller is responsible for displaying the
// code and verification URL to the user (this lets the TUI render it nicely
// vs the CLI mode printing it to stderr).
//
// Empty cfg.ClientID and cfg.TenantID fall back to the locked constants
// PublicClientID and CommonAuthority — see §3. Production code passes
// the zero Config value.
func New(cfg Config, prompt PromptFn) (Authenticator, error)

type Config struct {
    // TenantID and ClientID are optional. Empty falls back to the
    // /common authority and the Microsoft Graph CLI Tools public client.
    // Tests may override these to pin behaviour.
    TenantID string
    ClientID string
    Scopes   []string // e.g., ["Mail.ReadWrite", "Calendars.Read", ...]

    // Mode selects the sign-in flow when the silent-token path fails.
    // Default (zero value) is ModeAuto. See §5.0.
    Mode SignInMode

    // ExpectedUPN, when non-empty, is asserted against the resolved
    // signed-in account. A mismatch is a hard error.
    ExpectedUPN string
}

type SignInMode int

const (
    ModeAuto        SignInMode = iota // interactive first, fall back to device code if browser launch fails
    ModeInteractive                   // browser only; never fall back
    ModeDeviceCode                    // device code only; never use browser
)

type PromptFn func(ctx context.Context, p DeviceCodePrompt) error

type DeviceCodePrompt struct {
    UserCode        string    // e.g., "ABC123XYZ"
    VerificationURL string    // e.g., "https://microsoft.com/devicelogin"
    ExpiresAt       time.Time // when the user_code expires
    Message         string    // human-readable instruction from MSAL
}
```

## 5. Implementation

### 5.0 Sign-in flow selection

**Conditional Access on managed-device tenants (such as managed-Mac fleets at large enterprises) requires the auth flow to carry the device-compliance signal.** Device code flow CANNOT carry this signal — the user types a code in some browser that has no link to the originating machine, so AAD cannot prove the device is enrolled. Tenants with Conditional Access policies that require a managed device will reject device-code sign-ins with `AADSTS530003` / similar even though the user is who they say they are.

The interactive (browser) flow CAN carry the signal: when MSAL opens the system default browser on macOS, the operating system's enterprise SSO integration (Microsoft Enterprise SSO plug-in for Apple Devices) injects device-attestation cookies into the auth flow transparently. AAD sees the device IS managed and lets the sign-in succeed.

Therefore:

- **Default (`Mode = Auto`)**: try the interactive system-browser flow first. Fall back to device code only when the interactive flow can't launch (no `open` command, no `$DISPLAY`, headless SSH session).
- **`Mode = Interactive`**: only the browser flow; refuse to fall back. Use this when CA policy is known to require the device-compliance signal.
- **`Mode = DeviceCode`**: force device-code flow. Useful for headless / SSH usage on tenants without the device-compliance gate.

The user can override the mode at sign-in via `inkwell signin --device-code` or in `~/.config/inkwell/config.toml` via `[account].signin_mode`.

### 5.1 MSAL Go integration

Use `github.com/AzureAD/microsoft-authentication-library-for-go/apps/public`. The relevant types are `public.Client`, `public.AcquireTokenSilent`, `public.AcquireTokenInteractive` (system-browser flow with PKCE + localhost listener), and `public.AcquireTokenByDeviceCode`.

Construction:

```go
clientID := cfg.ClientID
if clientID == "" {
    clientID = PublicClientID // 14d82eec-204b-4c2f-b7e8-296a70dab67e
}
authority := CommonAuthority // https://login.microsoftonline.com/common
if cfg.TenantID != "" && cfg.TenantID != "common" {
    authority = "https://login.microsoftonline.com/" + cfg.TenantID
}
client, err := public.New(
    clientID,
    public.WithAuthority(authority),
    public.WithCache(keychainCache),  // see §5.2
)
```

The `/common` authority lets a user from any Entra tenant sign in. After the first successful sign-in MSAL knows the user's home tenant; subsequent silent calls happily resolve.

Token acquisition flow:

1. List accounts via `client.Accounts(ctx)`. If exactly one, attempt `AcquireTokenSilent`.
2. If `AcquireTokenSilent` returns success, return the token.
3. If it fails (no account, expired refresh token, etc.), fall back to the configured sign-in mode (§5.0):
   - **Interactive** (default): call `client.AcquireTokenInteractive(ctx, scopes)`. MSAL spawns a local HTTP listener on a free port, opens the system default browser at the AAD authorize endpoint with `redirect_uri=http://localhost:PORT`, and waits for the auth code to arrive. On a managed Mac the Microsoft Enterprise SSO plug-in for Apple Devices injects the device-attestation cookies, so Conditional Access policies that require a compliant device are satisfied transparently.
   - **Device code** (fallback): call `client.AcquireTokenByDeviceCode(ctx, scopes)`. This returns a `DeviceCodeResult` with `Result.Message`, `Result.UserCode`, `Result.VerificationURL`, `Result.ExpiresOn`.
   - Invoke `PromptFn` with these values so the caller can display them.
   - Then call `result.AuthenticationResult(ctx)` which blocks polling until the user completes auth (or times out / errors).
4. Return the resulting access token.

### 5.2 Keychain-backed encrypted-on-disk cache

MSAL Go's cache interface is `cache.ExportReplace`. The natural approach — write the entire serialised MSAL cache blob into a Keychain Generic Password item — falls over on Microsoft 365 tenants that issue large tokens (group-claim-heavy tokens easily exceed the ~3-4KB practical limit `zalando/go-keyring` enforces on its `security` shellout, surfacing as `data passed to Set was too big`).

Adopt the same pattern Microsoft's own `msal-extensions` / Authenticator libraries use on macOS: store a small **encryption key** in Keychain and write the **encrypted cache blob** to disk.

- **Encryption key:** 32 random bytes (AES-256) generated once on first sign-in via `crypto/rand`. Stored in Keychain at `service="inkwell"`, `account=tenant:client`. 32 bytes is well under any size limit.
- **Cipher:** AES-GCM (authenticated; tampering with the on-disk file makes decryption fail). Nonce is 12 random bytes per encryption, prepended to the ciphertext.
- **On-disk file:** `~/Library/Application Support/inkwell/msal_cache.bin`, mode `0600`, written atomically (temp file + rename so a crash mid-write can't leave a half-written cache).

```go
type keychainCache struct {
    service     string  // "inkwell"
    account     string  // tenant:client
    cachePath   string  // ~/Library/Application Support/inkwell/msal_cache.bin
}

func (k *keychainCache) Replace(_ context.Context, c cache.Unmarshaler, _ cache.ReplaceHints) error {
    key, err := k.readKey()
    if errors.Is(err, keyring.ErrNotFound) { return nil }   // first run
    if err != nil                          { return err }

    ct, err := os.ReadFile(k.cachePath)
    if errors.Is(err, os.ErrNotExist)       { return nil }   // first run after key set
    if err != nil                           { return err }

    pt, err := decryptAESGCM(ct, key)
    if err != nil { return nil }                            // stale / wrong key — treat as empty
    return c.Unmarshal(pt)
}

func (k *keychainCache) Export(_ context.Context, c cache.Marshaler, _ cache.ExportHints) error {
    pt, err := c.Marshal()
    if err != nil { return err }
    key, err := k.getOrCreateKey()
    if err != nil { return err }
    ct, err := encryptAESGCM(pt, key)
    if err != nil { return err }
    return atomicWriteFile(k.cachePath, ct, 0o600)
}
```

`SignOut` deletes both the Keychain entry and the on-disk file (idempotent: `ErrNotFound` and `os.IsNotExist` are not errors).

Failure modes added by this design:
- **Keychain access denied** (user rejected the prompt) — `keyring.Get` returns the macOS-specific access error. Surfaced to the user as "Keychain access denied; sign-in cannot proceed". No fallback to plaintext.
- **Disk file deleted but Keychain key still present** — `Replace` returns nil empty cache; next sign-in re-creates the file and rotates the key.
- **Disk file present but Keychain key missing or rotated** — decryption fails; we treat it as empty cache and the user signs in again. We do **not** error here because that would brick the app on key rotation.

Use `github.com/zalando/go-keyring` for the Keychain side (storing a 32-byte key is well under its limit). The encryption is `crypto/aes` + `crypto/cipher.NewGCM` from the Go standard library — pure Go, no CGO, satisfies CLAUDE.md §1.

The earlier all-in-Keychain approach is rejected: macOS Keychain Services *can* technically hold larger items via direct `SecItemAdd` calls, but `zalando/go-keyring`'s `security` CLI shellout caps the command-line at 4096 bytes. Switching to a different keychain library (e.g. `99designs/keyring`) would pull in CGO via `keybase/go-keychain`, violating our pure-Go constraint.

### 5.3 Concurrency

`Authenticator.Token` may be called from many goroutines (every Graph request). Behavior:

- A `sync.Mutex` serializes refresh attempts. Multiple concurrent `Token()` calls during a refresh queue and all receive the new token.
- After successful refresh, results are cached in-memory in addition to Keychain to avoid hammering the Keychain on every Graph call.
- The in-memory cache holds: token, expires-at. Refresh triggered when `time.Until(expiresAt) < 5 * time.Minute` (proactive refresh).

### 5.4 Re-auth handling

When the refresh token is expired or revoked:

- `AcquireTokenSilent` returns an error.
- `Token()` falls through to device code.
- Device code flow blocks until user completes the browser step OR the user aborts via `ctx` cancellation.
- The TUI's `PromptFn` should display a modal-style overlay with the code and URL, plus a "press Esc to cancel" affordance.
- The CLI's `PromptFn` should print to stderr in a format script-friendly enough to grep (`stderr` not `stdout`, in case stdout is being piped).

### 5.5 Sign out

```go
func (a *authenticator) SignOut(ctx context.Context) error {
    accounts, _ := a.client.Accounts(ctx)
    for _, acc := range accounts {
        if err := a.client.RemoveAccount(ctx, acc); err != nil {
            return err
        }
    }
    return keyring.Delete(a.service, a.account)
}
```

The `inkwell signout` CLI command and `:signout` TUI command both call this. After sign-out, the local SQLite cache is **not** automatically deleted — the user can choose to retain it (offline read access to historical mail) or run `inkwell purge` to clear it.

## 6. Requested scopes

Pass the following resource scopes exactly to MSAL:

```go
[]string{
    "https://graph.microsoft.com/Mail.Read",
    "https://graph.microsoft.com/Mail.ReadBasic",
    "https://graph.microsoft.com/Mail.ReadWrite",
    "https://graph.microsoft.com/MailboxSettings.Read",
    "https://graph.microsoft.com/MailboxSettings.ReadWrite",
    "https://graph.microsoft.com/Calendars.Read",
    "https://graph.microsoft.com/User.Read",
    "https://graph.microsoft.com/Presence.Read.All",
}
```

The `Chat.Read` and `User.ReadBasic.All` scopes are deferred (not in v1 surface area).

### 6.1 `offline_access` is opt-in

Earlier drafts of this spec included `offline_access` in the default scope list. Real-tenant smoke against deeply-managed enterprise tenants showed that:

1. The tenant declines `offline_access` for the Microsoft Graph CLI Tools client. MSAL Go raises a hard error.
2. We retry the same flow with `offline_access` stripped (spec §11 still documents this safety net).
3. The browser opens **twice** — once per attempt. With the OS enterprise SSO plug-in active, both prompts complete transparently, but the user sees two redirect flashes and two localhost listener cycles. Bad UX.

For v0.2.1 the default flips: `offline_access` is **not** in the default scope list. The trade-off:

- **Default behaviour:** one browser open per signin, no refresh tokens, so the user re-auths whenever the access token expires (~60 minutes). On deeply-managed enterprise tenants this is the *same* behaviour as if we had requested `offline_access` (since it's declined anyway), with cleaner UX.
- **Tenants that grant `offline_access`:** users can opt in via `[account].request_offline_access = true`. The flow is unchanged from before — we request it, the tenant grants it, the user gets ~90-day refresh tokens, no double-prompt.

The safety-net retry from §11 is retained for the opt-in case: if a user opts in and the tenant subsequently changes policy to decline, sign-in still works (with the double-prompt cost).

## 7. Logging

- INFO on first sign-in, sign-out.
- INFO on silent refresh success (no token logged).
- WARN on silent refresh failure with reason.
- INFO when device code prompt is shown (no code logged).
- ERROR on any unexpected MSAL error with full error text (MSAL errors do not contain tokens).

Do not log the access token. Do not log the refresh token. Do not log the cache blob. The redaction layer (ARCH §12) is a backstop, but auth code should never produce these strings in the first place.

## 8. CLI commands

| Command           | Behavior                                                        |
| ----------------- | --------------------------------------------------------------- |
| `inkwell signin`  | Forces device code flow even if cached token exists. Useful for first-time setup or switching accounts. |
| `inkwell signout` | Calls `SignOut`. Prompts before clearing cache.                 |
| `inkwell whoami`  | Prints UPN and tenant ID if signed in; exits non-zero if not.   |

## 9. TUI integration

- On TUI startup: try `Token()` once with a 1-second context. If it returns immediately (cached token valid), proceed to main UI.
- If `Token()` would block on device code: switch UI to a "Sign in" screen with the code and URL displayed prominently. Poll completion. On completion, transition to main UI.
- If `Token()` fails non-recoverably (e.g., network down on first run): show the error and exit cleanly. The user can retry once network is up.

## 10. Test plan

### Unit tests

- `keychainCache.Replace` / `Export` round-trips a known blob through a mocked keyring.
- `Token()` serializes concurrent calls (use a fake MSAL client to count refresh invocations).
- `Token()` returns cached token when not near expiry.
- `Token()` triggers refresh when within the 5-minute window.

### Integration tests

- Cannot run device code flow in CI. Test instead: provide a fake MSAL `public.Client` that returns canned `AuthenticationResult` values and asserts the call sequences.

### Manual smoke test (documented in qa-checklist.md)

1. Fresh install on a clean macOS user account.
2. `inkwell signin` shows code + URL.
3. Complete browser auth.
4. `inkwell whoami` returns correct UPN.
5. `inkwell signout` clears Keychain (verify with `security find-generic-password -s inkwell` returning not-found).
6. Re-sign in. Confirm second sign-in does not require re-typing code (cache hit).

## 11. Failure modes to handle explicitly

| Scenario                                              | Behavior                                                       |
| ----------------------------------------------------- | -------------------------------------------------------------- |
| Network down on first launch                          | Clear error; exit cleanly; no partial state.                  |
| Keychain access denied by user                        | Fall through to in-memory-only token; warn in status line; tokens lost on exit. |
| User aborts device code flow                          | Return context.Canceled; UI returns to sign-in screen.        |
| Tenant admin revokes consent mid-session              | Next Graph call returns 401; auth layer triggers re-auth via device code; UI shows re-auth modal. |
| Tenant admin disables user-consent for Microsoft-published apps OR blocks the Microsoft Graph CLI Tools app via Conditional Access | MSAL returns AADSTS error (e.g., AADSTS65001 / AADSTS530002 / AADSTS50105). Surface a user-friendly message: "Your tenant blocks the Microsoft Graph Command Line Tools app — ask your IT admin to allow it or grant user-consent for first-party Microsoft apps." Do not retry; do not fall back to a different client_id. |
| Conditional Access requires a compliant / managed device | Device-code flow CANNOT satisfy this — the typed code has no link to the originating machine, so AAD cannot prove the device is enrolled, and sign-in is rejected (AADSTS530003 / similar). Inkwell defaults to the interactive system-browser flow (§5.0) which **can** satisfy device-compliance via the OS enterprise SSO plug-in (Microsoft Enterprise SSO plug-in for Apple Devices). If the user has explicitly forced `--device-code` on a managed-device tenant, the auth layer surfaces: "This tenant requires a compliant device. Run `inkwell signin` without --device-code so the system browser can carry the device-attestation signal." Do not auto-retry as interactive — if the user picked device code, honour it. |
| Conditional Access denies token for other reasons (MFA, location, risk) | MSAL returns specific AADSTS error; surface a user-friendly message naming the policy class and link to https://aka.ms/MFASetup as appropriate. |
| Interactive flow can't launch (no `open` command, headless SSH, no display) | In `Mode = Auto`: log a notice and fall back to device code. In `Mode = Interactive`: surface "Cannot open system browser; rerun in a desktop session or use --device-code on a tenant that allows it." |
| Tenant declines `offline_access` (no long-lived refresh tokens) | MSAL Go raises `token response failed because declined scopes are present: offline_access`. The user *did* sign in successfully — the tenant just doesn't allow this client to hold a long-lived refresh token. Retry the same flow once with `offline_access` stripped from the scope list. The retry succeeds; the user is signed in. The cost: no silent refresh — when the access token expires (~60 minutes) the user re-auths via the same flow. We do not surface this as an error; we log a Warn telling future maintainers what happened. If the retry *also* errors, surface the second error to the user (the offline_access decline was not the root cause). |
| MSAL cache blob exceeds the Keychain library's size limit | `zalando/go-keyring` shells out to `security` and caps the command at 4096 bytes. Token-heavy tenants (group claims, long ID tokens) blow past this and surface as `data passed to Set was too big`. §5.2 mitigates this by storing a 32-byte encryption key in Keychain and the encrypted MSAL blob on disk under `~/Library/Application Support/inkwell/msal_cache.bin`. There is no user-visible error path here; the design prevents it by construction. |
| User signs in with a personal (Microsoft consumer) account | The `/common` authority technically allows this, but Inkwell targets work / school accounts only. Detect via `Account.Realm == "9188040d-6c67-4c5b-b112-36a304b66dad"` (the consumer tenant guid) at sign-in completion and refuse with a clear error message. |
| Clock skew > 5 minutes                                | Token validation fails; surface as "system clock incorrect"; exit. |

## 12. Definition of done

- [ ] `internal/auth/` package compiles and passes unit tests.
- [ ] `inkwell signin`, `inkwell signout`, `inkwell whoami` work end-to-end against a real tenant.
- [ ] Tokens persist across app restarts (verified manually).
- [ ] Token refresh happens silently (verified by leaving the app open past token expiry; check logs for refresh, no UI prompt).
- [ ] Device code re-auth triggers when refresh token is invalidated (verified by manually deleting the Keychain item mid-session).
- [ ] No tokens or cache blobs appear in logs (verified by grepping `~/Library/Logs/inkwell/` after a full sign-in/use/sign-out cycle).

## 13. Configuration

This spec owns the `[account]` section. Full reference in `CONFIG.md`.

| Key | Default | Notes |
| --- | --- | --- |
| `account.tenant_id` | `common` | Optional. Defaults to the multi-tenant common authority. Set to a specific tenant GUID only to pin sign-in to a single tenant (rare; useful in CI scripting against a service principal — out of scope for v1). |
| `account.client_id` | `14d82eec-204b-4c2f-b7e8-296a70dab67e` | Optional. Defaults to the Microsoft Graph Command Line Tools first-party public client. Overriding is possible but unsupported; PRD §4 explains why. |
| `account.upn` | (optional, populated at sign-in) | Optional. If set, the auth layer asserts the signed-in account matches this UPN and refuses otherwise. Useful as a guardrail when a user has multiple accounts. After successful sign-in, the resolved UPN is also persisted to the local `accounts` row. |
| `account.signin_mode` | `auto` | Sign-in flow selection (§5.0). `auto` tries the interactive system browser first and falls back to device code only if the browser can't launch. `interactive` forces the browser path (recommended for managed-device tenants). `device_code` forces device code (headless / SSH only). |
| `account.request_offline_access` | `false` | Whether to include the `offline_access` scope (§6.1). Default off: signin opens the browser exactly once and the user re-auths when the access token expires (~60 minutes). Set to `true` only on tenants known to grant `offline_access` for first-party Microsoft apps; the user gets ~90-day refresh tokens at the cost of a possible double browser-open if the tenant ever declines. |

**The whole `[account]` section is optional.** A user with no `~/.config/inkwell/config.toml` at all can run `inkwell signin` and Inkwell will use the locked first-party defaults. After sign-in the resolved `tenant_id` and `upn` are persisted in the local SQLite store.

The auth module reads these values from `*config.Config` at construction time. It does not read environment variables or flags directly; the loader at `cmd/inkwell/main.go` is responsible for assembling the final `Config` and passing it down.

The scopes list in §6 is **not** configurable. Adding or removing scopes changes the contract with the tenant admin and must be done via a code change, not user config.

## 14. Out of scope for this spec

- Multi-account support (the `Account()` method returns one account; multi-account is a v2 redesign).
- Certificate-based or managed-identity auth (not applicable to a desktop client).
- Custom CA / proxy trust configuration. If the corporate environment requires it, document it as a `GRAPH_CA_BUNDLE` env var the user sets; do not bake CA logic into auth.
