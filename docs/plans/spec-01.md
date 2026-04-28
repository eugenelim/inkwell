# Spec 01 — Authentication via Device Code Flow

## Status
done (CI scope) — manual-tenant smoke deferred per CLAUDE.md §5.5

## DoD checklist
- [x] `internal/auth/` compiles, unit tests pass under `-race`.
- [x] `inkwell signin/signout/whoami` cobra subcommands wired.
- [x] Concurrent `Token()` serialises refresh (verified).
- [x] Cached token returned within window; refresh triggered within 5-min proactive window (verified).
- [x] `Invalidate()` forces reacquire (verified).
- [x] `keychainCache` round-trips a blob through mocked keyring (verified).
- [x] Privacy backstop: redactor scrubs token-shaped strings even if a future change adds logging.
- [x] `whoami` refuses to prompt for device code; returns "not signed in" instead.
- [ ] **Deferred (manual smoke):** real-tenant signin / signout / whoami; refresh-token persistence across restarts; re-auth on revoked refresh; grep `~/Library/Logs/inkwell/` for token-shaped strings post-session.

## Iteration log

### Iter 1 — 2026-04-27
- Slice: lay out Authenticator interface, TokenSource seam, MSAL wrapper, keychain adapter, scopes, runners, tests.
- Files added: internal/auth/{auth,source,scopes,keychain}.go, internal/auth/{auth,keychain,privacy}_test.go, cmd/inkwell/cmd_auth_runners.go updated.
- Commands: `go vet ./...`, `go test -race ./...` — green.
- Critique fixed in-iteration: `whoami` originally fell through to device-code prompt; replaced with a refusing prompt + 2s timeout.
- Critique outstanding: none session-blocking.

### Iter 2 — 2026-04-27 (auth pivot to first-party public client)
- Trigger: user instruction to drop the per-tenant app-registration requirement and use the well-known Microsoft Graph CLI Tools client.
- Slice: lock `PublicClientID = 14d82eec-204b-4c2f-b7e8-296a70dab67e` and `CommonAuthority = https://login.microsoftonline.com/common` in scopes.go. Add `Config.{TenantID,ClientID}.resolved()` so the zero Config works in production. Add `Config.ExpectedUPN` guardrail. Add `ConsumerTenantID` constant + `checkAccount()` that refuses MSA personal accounts and UPN mismatches. Drop the required-account validation from internal/config; defaults now ship with the locked tenant=`common` + Graph-CLI-Tools client. cmd/inkwell wires `cfg.Account.UPN` → `auth.Config.ExpectedUPN`.
- Doc updates: PRD §3 reframed (we *request*, the user *consents*), PRD §4 fully rewritten, spec 01 §3/§6/§11/§13 updated, CONFIG.md `[account]` flagged optional, README first-time-setup is now `inkwell signin`.
- New tests: empty-Config construction, resolved() defaults, authority handling for "" / "common" / pinned tenant GUID, consumer-account refusal, ExpectedUPN guardrail (mismatch + case-insensitive match).
- Race + e2e + budget gates all green.
- Critique outstanding: none. Manual-tenant smoke now genuinely first-run: `inkwell signin` with no config file.

## Notes for spec 03
- Auth transport (spec 03 §10.2) needs `Authenticator.Invalidate()` — already shipped.
- `TokenSource` seam is package-private; spec 03's auth transport will consume the public `Authenticator` interface only.
- The graph client is unaffected by the auth pivot; it consumes the same `Token()` / `Invalidate()` contract regardless of which client_id MSAL is using.
