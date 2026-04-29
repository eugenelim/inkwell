# Inkwell threat model

> **Status:** v0.13.0 first cut. Living document — every spec PR
> reviews this file per CLAUDE.md §11 cross-cutting checklist and
> updates the threats-and-mitigations table when the work changes
> what we defend against. Spec
> [`docs/specs/17-security-testing-and-casa-evidence.md`](specs/17-security-testing-and-casa-evidence.md)
> §5.2 owns the broader CASA-evidence story; this file is the
> in-tree truth.

## Scope

This document covers the threat model for the inkwell binary as
distributed via GitHub Releases (and, eventually, an official
Homebrew tap and signed/notarized macOS package). It does not
cover Microsoft Graph, the user's Microsoft 365 tenant, or any
upstream dependency in isolation — those have their own threat
models maintained by their authors.

## Assets we protect

1. **OAuth access and refresh tokens** for the user's Microsoft 365
   account.
2. **Cached message envelopes and bodies** — the local SQLite cache
   at `~/Library/Application Support/inkwell/mail.db` and any
   bodies stored within it.
3. **Attachment files** — once spec 05 §8 lands, these will be
   written to a user-configurable directory.
4. **User configuration** — tenant id, UPN, saved searches,
   binding overrides — at `~/Library/Application
   Support/inkwell/config.toml`.
5. **Working memory of the running process** — tokens and message
   contents transit RAM during sync and rendering.
6. **Drafts in progress** — at `~/Library/Caches/inkwell/drafts/`,
   mode 0600, cleaned on save.

## Trust boundaries

- **Inkwell ↔ user's macOS account.** Inkwell runs as the user;
  trusts the user.
- **Inkwell ↔ other users on the same Mac.** File mode 0600 +
  Keychain ACL. We DO NOT trust other users.
- **Inkwell ↔ Microsoft Graph / Apple Push.** TLS 1.2+ via Go's
  `crypto/tls` defaults; certificate chain rooted in the macOS
  trust store (or system trust store on Linux).
- **Inkwell ↔ user's `$EDITOR`, `open`, `pbcopy`, browsers.** We
  trust these because the user installed them. Subprocess
  invocations always use argv form (`exec.Command(bin, args...)`),
  never `sh -c`.
- **Inkwell ↔ local filesystem.** Pathnames coming from network
  sources (attachments, message links) are NOT trusted; planned
  hardening (spec 05 §8) requires `filepath.Clean` + verified
  containment in a target dir.

## Threats and mitigations

| Threat | Mitigation | Verified by |
| --- | --- | --- |
| Token theft from disk | Keychain only; never written to filesystem in plaintext (CLAUDE.md §7 rule 2). | `internal/auth/privacy_test.go` |
| Token theft from process memory | Out of scope (full memory access = game over). | n/a |
| Token theft via swap | macOS encrypted swap (when FileVault on). | n/a — OS-level |
| Cache exfiltration by another user on the same Mac | File mode 0600 on `mail.db` + Keychain ACL. | `internal/store/security_test.go::TestDatabaseFileMode`; `internal/auth/keychain_test.go` |
| Cache exfiltration by malware running as the user | Out of scope. Defending against in-user malware is beyond a desktop client's threat model. | n/a |
| Draft exfiltration during compose | Tempfile written 0600 in `~/Library/Caches/inkwell/drafts/`; cleaned on save. | `internal/compose/security_test.go::TestDraftTempfileMode` |
| MITM on Graph traffic | TLS 1.2 minimum (Go stdlib default), full cert validation, system trust store. `InsecureSkipVerify` is never set in code. | gosec G402 rule (CI gate); planned `TestHTTPClientVerifiesCertificates` |
| Token leakage in logs | Structured slog handler with redaction layer; bearer tokens / refresh tokens / message bodies / email addresses (above DEBUG) all scrubbed. | `internal/log/redact_test.go` (8 tests); `internal/auth/privacy_test.go` |
| Path traversal via attachment filename | `filepath.Clean` + containment check before write. Spec 05 §8 not yet implemented. | TBD when spec 05 §8 lands |
| Shell injection via `$EDITOR` / external converter | `exec.Command` with argv, never `exec.Command("sh", "-c", ...)`. | `internal/compose/security_test.go::TestEditorCommandUsesArgvNotShell` |
| Shell injection via `:open` URL | argv-form `exec.Command("open", url)` / `exec.Command("xdg-open", url)`. URL is the Graph webLink the server gave us. | `#nosec G204` annotation in `internal/ui/compose.go::openInBrowser` documents the contract |
| SQL injection via pattern language | All store queries are parameterised; no string concatenation of user input. | `internal/store/security_test.go::TestSearchByPredicateSurvivesAdversarialInput`; `internal/pattern/pattern_test.go::TestCompileLocalEscapesWildcardLiterals` |
| Replay attack on action queue | Idempotent actions (spec 07 §1); server reconciles. 404-on-delete is success. | `internal/action/executor_test.go` |
| Misuse of denied-scope tokens | Scopes are hardcoded in `internal/auth/scopes.go`; tenant admin enforces consent. CI grep guard refuses any source line containing `Mail.Send` / `Calendars.ReadWrite` etc. | `.github/workflows/ci.yml::permissions-check` |
| Predictable action IDs | `crypto/rand`-backed 16-byte hex IDs (32 chars); collision-free over 1k generations; aggregate bit-balance ~50%. | `internal/action/security_test.go::TestActionIDsHaveHighEntropy` |
| Compromised dependency | govulncheck on every PR + on weekly schedule; planned Dependabot for proactive updates; planned SBOM published with each release. | `.github/workflows/security.yml` |
| Compromised stdlib (Go CVE) | govulncheck reports stdlib vulns; workflows pin `go-version: 1.25.x` + `check-latest: true` to pull patches automatically. v0.12.0 fix landed after CI surfaced 17 stdlib CVEs from a stale floor. | `.github/workflows/{ci,security,release}.yml` |
| Tampered binary distribution | Pending: code-sign + notarize via Apple Developer ID before public v1.0 distribution. PRD §7. | TBD |
| Backdoored build pipeline | Pending: pin GitHub Actions by SHA, scope tokens, dependency-review-action on PRs. | Partial — `permissions: contents: read` set on the security workflow |
| Accidental secret commits | Planned: gitleaks workflow + pre-commit hook (spec 17 §3.6). | TBD |
| One-click unsubscribe POST sent to attacker URL | URL is extracted only from the message's own `List-Unsubscribe` header — sender-asserted. Generic `User-Agent`, no cookies, no referer. 5s timeout; 3-hop redirect cap; HTTPS-only. | `internal/unsub/parse_test.go`; `internal/unsub/execute_test.go` |
| Cached unsubscribe action stale after sender rotates URL | Accepted residual risk (cache hint only). User can clear the column to re-fetch. | Documented in `docs/plans/spec-16.md` |

## Things we do NOT defend against

- **Compromised macOS / FileVault disabled.** OS-level security is
  the foundation; we trust it.
- **Compromised Microsoft tenant or compromised user account.** If
  the upstream account is compromised, inkwell's local cache is
  the least of the user's worries.
- **Compromised Apple Developer account** (post-signing). Out of
  scope for the binary; relies on Apple's revocation flow.
- **Physical access to an unlocked machine.** No defence; the
  whole filesystem is readable as the user.
- **Compromised user (phishing, social engineering).** No
  technical defence beyond log redaction (so logs the user
  shares with support don't leak what they shouldn't).

## Accepted residual risks

- Cached unsubscribe actions can become stale (sender rotates URL).
  Cost of refreshing on every press > probability × severity of
  acting on a stale URL. Tracked in spec 16's plan note.
- Action queue grows unbounded if the user batch-deletes many
  thousands of messages while offline. Soft cap at 1000 from
  spec 07 §15; not yet enforced in code.
- The `/me/messages/{id}?$select=internetMessageHeaders` lazy
  fetch on the first U press is a one-bit signal to the server
  that the user opened the message. Acceptable — clicking
  unsubscribe is a stronger signal.

## Change log

- 2026-04-29 — first cut as part of v0.13.0 spec 17 partial ship
  (CI tooling already live in v0.12.0; security tests + this
  document landing now).
