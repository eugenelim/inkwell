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

## Notes for spec 03
- Auth transport (spec 03 §10.2) needs `Authenticator.Invalidate()` — already shipped.
- `TokenSource` seam is package-private; spec 03's auth transport will consume the public `Authenticator` interface only.
