# internal/auth — AGENTS.md

Package-specific contract. Read the root `AGENTS.md` first for repo-wide
conventions; this file only spells out what's different about `auth`.

## What this package is

The only package permitted to call MSAL Go, Microsoft Entra ID
(`login.microsoftonline.com`), or the macOS Keychain. Implements the
OAuth 2.0 device-code flow and silent token refresh.

## Hard invariants (specific to this package)

1. **Public client via Microsoft Graph CLI Tools (ADR-0005).** The
   `client_id` (`14d82eec-204b-4c2f-b7e8-296a70dab67e`) is a compile-
   time constant — never configurable. Authority is `/common`; tenant
   is inferred from the user's UPN at sign-in.
2. **Tokens in Keychain only.** Never on disk. Never in environment
   variables (except in mock-keyring tests where the real Keychain
   isn't available). Never in logs. Never in UI error messages.
3. **Scopes are not configurable.** The list in `scopes.go` is a
   contract with the tenant admin (PRD §3.1). Adding or changing a
   scope is a code-review event, not a config flag.
4. **Forbidden scopes are guard-railed.** `Mail.Send`,
   `Calendars.ReadWrite`, anything `*.Shared`, and Teams scopes are
   refused by `permissions-check` in CI and by spec 17 tests in this
   package's `security_test`.
5. **Redaction is non-negotiable.** Bearer tokens, refresh tokens,
   MSAL response blobs, and PII fields like `userPrincipalName` are
   redacted by `internal/log/redact.go` before any log site sees
   them. Every code path in this package that could log a secret
   has a redaction test.

## API shape

- `Authenticator` is the consumer-site interface: `Token(ctx)
  (string, error)`. Callers don't see MSAL types.
- `Source` returns a refreshing token source for the `*http.Client`
  in `internal/graph`.
- Sign-out clears Keychain entries AND invalidates the in-memory
  cache; partial sign-out is a known failure mode (root §7).

## Testing

- Real Keychain access is mocked via the `keyring` interface — see
  `keychain_test.go`. The CI runs on Linux where there is no
  Keychain, so the mock is the only test path on CI.
- Device-code flow is exercised against a fake MSAL endpoint
  (`httptest.Server` in `auth_test.go`).
- Privacy guard: `privacy_test.go` enforces that no real domain or
  tenant ID appears in test fixtures (root §7 invariant 1).
- Redaction tests cover every log site in this package — adding a
  log call without a matching redaction test fails CI.

## Common pitfalls

- Reading `os.Getenv("AZURE_TENANT_ID")` or similar — the tenant is
  inferred at sign-in, not configured. Env var reads here would
  defeat ADR-0005's "zero setup" property.
- Logging the full MSAL `AuthResult` — it contains the access token
  and ID token claims. Redact at the field level.
- Calling `context.Background()` inside `Token()` — propagate the
  caller's context; a hung MSAL call must be cancellable.

## References

- spec 01 (auth — device code), spec 17 (security testing).
- ARCH §1 (tech stack), §11 (configuration).
- ADR-0005 (MSAL public client via Microsoft Graph CLI Tools).
