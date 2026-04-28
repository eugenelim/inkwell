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
- Trigger: real-tenant smoke caught Conditional Access rejecting device-code on a managed Mac with the AADSTS error "your admin requires the device requesting access to be managed by ExampleCorp". Device-code flow cannot carry the device-compliance signal — the user types a code in *some* browser that has no link to the originating device. The only viable interactive path on managed-device tenants is the system browser with the OS enterprise SSO plug-in.
- Slice: add `SignInMode` enum (Auto / Interactive / DeviceCode) + `Config.Mode`. Extend `TokenSource` with `AcquireTokenInteractive`. New `acquireFallback()` routes by mode. `ModeAuto` tries interactive first and falls back to device code only on browser-launch errors (never on AAD errors — those bubble straight up). `ParseSignInMode` accepts auto/interactive/browser/device_code/device-code/devicecode.
- CLI: `inkwell signin` gains `--device-code` and `--interactive` flags (mutually exclusive). Default is auto (interactive-first).
- Config: `[account].signin_mode` (auto|interactive|device_code), default `auto`. Validated.
- Doc updates: spec 01 retitled and reframed; new §5.0 explains mode selection; §2 rewritten ("Why interactive system browser (default) + device code (fallback)"); §11 adds rows for the device-compliance CA policy and for browser-launch failures; §13 documents `signin_mode`. PRD §4 updated to declare interactive-first with device-code as the headless fallback.
- Tests added: ModeAutoUsesInteractiveWhenNoAccount, ModeAutoFallsBackToDeviceCodeOnLaunchError, ModeAutoDoesNotFallBackOnAADError, ModeInteractiveDoesNotFallBack, ModeDeviceCodeSkipsInteractive, TokenFallsBackInteractiveWhenSilentFails, ParseSignInMode, IsBrowserLaunchErrorClassification. Existing tests that drove `deviceResult` switched to `interactiveResult` since the default mode changed.
- Race + e2e + budget gates all green.

### Iter 4 — 2026-04-28 (offline_access decline retry)
- Trigger: real-tenant smoke (ExampleCorp) caught `signin: interactive auth: token response failed because declined scopes are present: offline_access`. The browser flow worked; the user signed in; the tenant just declines long-lived refresh tokens for the Microsoft Graph CLI Tools client. MSAL Go raises a hard error in that case even though sign-in otherwise succeeded.
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

## Notes for spec 03
- Auth transport (spec 03 §10.2) needs `Authenticator.Invalidate()` — already shipped.
- `TokenSource` seam is package-private; spec 03's auth transport will consume the public `Authenticator` interface only.
- The graph client is unaffected by the auth pivot; it consumes the same `Token()` / `Invalidate()` contract regardless of which client_id MSAL is using.
