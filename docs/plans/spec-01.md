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

### Iter 3 — 2026-04-28 (interactive flow as default; device-code is opt-in fallback)
- Trigger: real-tenant smoke caught Conditional Access rejecting device-code on a managed Mac with the AADSTS error "your admin requires the device requesting access to be managed by your tenant". Device-code flow cannot carry the device-compliance signal — the user types a code in *some* browser that has no link to the originating device. The only viable interactive path on managed-device tenants is the system browser with the OS enterprise SSO plug-in.
- Slice: add `SignInMode` enum (Auto / Interactive / DeviceCode) + `Config.Mode`. Extend `TokenSource` with `AcquireTokenInteractive`. New `acquireFallback()` routes by mode. `ModeAuto` tries interactive first and falls back to device code only on browser-launch errors (never on AAD errors — those bubble straight up). `ParseSignInMode` accepts auto/interactive/browser/device_code/device-code/devicecode.
- CLI: `inkwell signin` gains `--device-code` and `--interactive` flags (mutually exclusive). Default is auto (interactive-first).
- Config: `[account].signin_mode` (auto|interactive|device_code), default `auto`. Validated.
- Doc updates: spec 01 retitled and reframed; new §5.0 explains mode selection; §2 rewritten ("Why interactive system browser (default) + device code (fallback)"); §11 adds rows for the device-compliance CA policy and for browser-launch failures; §13 documents `signin_mode`. PRD §4 updated to declare interactive-first with device-code as the headless fallback.
- Tests added: ModeAutoUsesInteractiveWhenNoAccount, ModeAutoFallsBackToDeviceCodeOnLaunchError, ModeAutoDoesNotFallBackOnAADError, ModeInteractiveDoesNotFallBack, ModeDeviceCodeSkipsInteractive, TokenFallsBackInteractiveWhenSilentFails, ParseSignInMode, IsBrowserLaunchErrorClassification. Existing tests that drove `deviceResult` switched to `interactiveResult` since the default mode changed.
- Race + e2e + budget gates all green.

### Iter 4 — 2026-04-28 (offline_access decline retry)
- Trigger: real-tenant smoke caught `signin: interactive auth: token response failed because declined scopes are present: offline_access`. The browser flow worked; the user signed in; the tenant just declines long-lived refresh tokens for the Microsoft Graph CLI Tools client. MSAL Go raises a hard error in that case even though sign-in otherwise succeeded.
- Slice: refactor `acquireFallback` into a wrapper that retries `acquireWithScopes` once, with `offline_access` stripped, when the only declined scope was `offline_access`. New helpers `isOfflineAccessDeclined` (parses the MSAL error message; only retries when offline_access is the *sole* declined scope so other scope problems still surface) and `scopesWithout` (case-insensitive remove). Trade-off: no silent refresh — the user re-auths when the access token expires (~60 minutes).
- Tests added: RetriesWithoutOfflineAccessOnDecline, DoesNotRetryWhenOtherScopeDeclined, DoesNotRetryWhenMultipleScopesDeclinedIncludingOfflineAccess (the critical guard), RetrySurfacesSecondError, IsOfflineAccessDeclinedClassification, ScopesWithoutDropsCaseInsensitive. fakeSource gained `*ErrFirstCallOnly` flags + `observedScopes` so retry-flow assertions are precise.
- Spec 01 §11 gains a new failure-mode row documenting the retry + the trade-off.
- Race + e2e green.

### Iter 5 — 2026-04-28 (encrypted-on-disk MSAL cache)
- Trigger: real-tenant smoke after v0.1.2 caught `signin: interactive auth: data passed to Set was too big`. zalando/go-keyring on Darwin shells out to `security` and caps the command at 4096 bytes. Token-heavy tenants (group claims, long ID tokens) blow past it.
- Slice: redesign `keychainCache` to store a 32-byte AES-256 key in Keychain and the encrypted MSAL cache blob on disk under `~/Library/Application Support/inkwell/msal_cache.bin`. AES-GCM (nonce + sealed; tag in sealed). Atomic write via temp + rename. On Replace, decryption failure is treated as empty cache (so a rotated key doesn't brick the app). On Export, the key is generated lazily on first call. SignOut clears both Keychain entry and on-disk file. Pure Go, no CGO. Tests cover round-trip, large blobs (16KB), file mode 0600, plaintext-leak guard, missing-key, missing-file, rotated-key, GCM tamper-resistance, atomic-write temp-file cleanup.
- Spec 01 §5.2 and §11 updated to document the design + the failure modes it deliberately prevents.
- Race + e2e all green.

### Iter 6 — 2026-04-28 (persist account on signin)
- Trigger: real-tenant smoke after v0.1.3 surfaced the question "after signin works, what does it take to actually see my email?". The TUI's data-access path (`store.ListFolders(accountID)`, `store.ListMessages(accountID, …)`) needs an `accounts` row to scope every query against. Spec 02 §5 already exposes `PutAccount(ctx, a) (id, err)`; nothing was calling it.
- Slice: at the end of `runSignin` after `auth.Token()` succeeds, open the local store, call `PutAccount(Account{TenantID: resolvedTenant, ClientID: cfg.ClientID, UPN: resolvedUPN, LastSignin: now})`, close. `whoami` still works without writes — it's a read-only path. The TUI default-action flow (spec 04 iter 3) reads the row before constructing `Deps.Account`.
- Layering: this stays in `cmd/inkwell` rather than `internal/auth`; auth must not import store (CLAUDE.md §2). The cmd layer is the natural place for the auth → store handoff.
- Tests: cmd-layer wiring is hard to unit-test because it builds a real store + real auth; covered by smoke instead.

### Iter 7 — 2026-04-28 (silent-only probe + offline_access opt-in)
- Trigger: real-tenant smoke after v0.2.0 surfaced two regressions:
  1. `./inkwell` (no subcommand) opened the browser even though the user had just signed in. Cause: my `runRoot` probe used `Token()` with `Mode=Auto` and a 2-second timeout. When silent didn't return inside 2s the auto-fallback fired interactive flow — which our refusing PromptFn doesn't catch (PromptFn only fires for device-code). Browser opened, SSO redirected to localhost, but the 2s timeout had already torn down the listener; user saw the localhost-redirect URL flash and then "not signed in".
  2. `./inkwell signin` opened the browser **twice** on managed-device tenants. Cause: the offline_access decline retry path from iter 4 — first attempt with offline_access fails (declined), second without succeeds. Two browsers, two localhost listeners. SSO plug-in makes both transparent (no typing) but it's still ugly UX.
- Slice for (1): new `Authenticator.IsSignedIn(ctx) bool` method that ONLY does the silent path: list accounts → AcquireTokenSilent → checkAccount. Never invokes interactive or device-code. `runRoot` and `whoami` switched from the broken `Token()`-with-refusing-prompt pattern to `IsSignedIn`. Bumped probe timeout to 5s now that it's truly silent (worst case is one network round-trip to AAD's token endpoint to refresh).
- Slice for (2): drop `offline_access` from `DefaultScopes()`. New `ScopesWithOfflineAccess(bool)` constructor and `Config.RequestOfflineAccess` field. Default off → single browser open per signin, hourly re-auth (same end-user behaviour as before on managed-device tenants where it was declined anyway). `[account].request_offline_access` config opts back in for tenants that grant it. The retry-on-decline safety net stays in place.
- Slice for UX: `inkwell signin` flows into `runRoot` on success unless `--no-tui` is passed. CI / scripting path remains via `--no-tui`.
- Tests added: TestDefaultScopesOmitsOfflineAccessByDefault, TestScopesWithOfflineAccessIncludesItWhenAsked, TestConfigResolvedHonoursRequestOfflineAccess, TestIsSignedInReturnsTrueOnSilentHit, TestIsSignedInReturnsTrueFromInMemoryCache, TestIsSignedInReturnsFalseWhenNoAccounts, TestIsSignedInReturnsFalseWhenSilentFails, TestIsSignedInRefusesConsumerAccount. Critical: each IsSignedIn test asserts `interactiveCalls == 0` and `deviceCalls == 0` so a future regression that adds a fallback to IsSignedIn fails loudly.
- Spec 01 §6 fully rewritten with new §6.1 explaining the offline_access opt-in trade-off. §13 documents `request_offline_access`.
- Race + e2e green.

### Iter 8 — 2026-04-28 (--verbose actually wired)
- `--verbose` was a parsed flag with no effect — log level was hardcoded to Info. Now wires through to the redacting slog handler. Useful for the diagnostic phase that landed alongside this iter (see spec-03 iter 4 below).

### Iter E-2 — 2026-05-04 (AADSTS classification + clock-skew + CLI PromptFn)
- Slice: `internal/auth/errors.go` — `ClassifyAuthError`, `IsClockSkewError`, `classifyAAD`, `isClockSkewMsg`, `containsAny`. Wire into `acquireWithScopes` error paths (device code, interactive, auto-fallback). Update `promptDeviceCode` in `cmd/inkwell/cmd_auth_runners.go` to print `p.Message` when non-empty.
- Tests: `internal/auth/errors_test.go` — 13 test cases covering nil, no-match, pass-through-unchanged, errors.Is chain, per-code device-compliance/consent/scope/clock-skew, hint-line format.
- Commands: `gofmt -s -l` clean; `go vet ./...` clean; `go test -race ./...` green (17/17); `go test -tags=e2e ./...` green; `go test -tags=integration ./...` green; `go test -bench=. -benchmem ./...` green.
- Critique: no layering violations; no comments restating code; no public symbols beyond spec; no PII in new code paths; no perf budget impact; `ClassifyAuthError` preserves `%w` chain so `errors.Is/As` callers unaffected.
- Closed: spec 01 §11 AADSTS classification absent; clock-skew hint absent; §5.4 CLI PromptFn absent.

## Notes for spec 03
- Auth transport (spec 03 §10.2) needs `Authenticator.Invalidate()` — already shipped.
- `TokenSource` seam is package-private; spec 03's auth transport will consume the public `Authenticator` interface only.
- The graph client is unaffected by the auth pivot; it consumes the same `Token()` / `Invalidate()` contract regardless of which client_id MSAL is using.
