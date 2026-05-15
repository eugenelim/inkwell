# Spec 17 — Security testing and CASA evidence

**Status:** Partial — CI tooling shipped in v0.12.0; first cut of
security tests + `docs/THREAT_MODEL.md` + `docs/PRIVACY.md` +
gitleaks + dependency-review + Dependabot + SBOM shipping in
v0.13.0. Remaining work tracked in `docs/specs/17-security-testing-and-casa-evidence/plan.md`.
**Depends on:** All v1 specs (this one hardens what they produce).
**Blocks:** Any future Gmail support spec (CASA Tier 2 self-scan
needs this evidence). Recommended before public v1 distribution.
**Estimated effort:** 2–3 days for setup; ongoing maintenance baked
into normal CI.

---

## 1. Goal

Build the security testing scaffolding into the codebase so that:

1. The CI pipeline catches security issues during development, not at
   release.
2. The artifacts a CASA Tier 2 self-scan submission requires are
   produced as a byproduct of normal builds.
3. Enterprise security reviewers (separate from CASA) have a coherent
   set of documents to review.

This spec produces three categories of output:

- **Tooling** — SAST, SCA, secret scanning in CI.
- **Tests** — security-specific test cases that produce evidence of
  correct behaviour.
- **Documents** — `SECURITY.md`, `THREAT_MODEL.md`, `PRIVACY.md`.

The work is deliberately additive — no architectural changes — so it
can be retrofitted without disturbing v1 specs.

## 2. Why this exists

Three drivers, in priority order:

1. **CASA Tier 2 readiness** if we ever pursue Gmail. The OAuth
   verification + CASA process for restricted Gmail scopes requires
   evidence of security posture. Producing this as part of normal CI
   is far cheaper than scrambling pre-submission. See research on
   Google's CASA process at https://appdefensealliance.dev/casa.

2. **Enterprise IT scrutiny.** A senior professional at a regulated
   enterprise may need to justify their use of a third-party mail
   client to an internal security team. Having a published threat
   model, a clean SAST history, and a formal vulnerability disclosure
   policy materially shortens that conversation.

3. **Defense in depth for the user.** The product handles tokens that
   grant access to the user's entire mailbox. Bugs in token handling,
   log redaction, file permissions, or path traversal are not
   theoretical — they're the canonical mistakes mail clients make.
   Catching them in CI is much cheaper than catching them in
   production.

## 3. Tooling: CI pipeline additions

### 3.1 What already shipped (v0.12.0)

The following are live on `main` as of v0.12.0:

| Tool | Where | Notes |
| --- | --- | --- |
| **gosec** (Go SAST) | `.github/workflows/security.yml` job `gosec` + `make sec-gosec` | SARIF artefact uploaded; text-format gate fails the job on any finding. 9 baseline `#nosec` annotations with one-line WHY rationales (see `docs/CONVENTIONS.md` §7). |
| **Semgrep** (multi-language SAST) | `.github/workflows/security.yml` job `semgrep` + `make sec-semgrep` | Configs: `p/golang`, `p/security-audit`, `p/secrets`. SARIF artefact uploaded. |
| **govulncheck** (Go SCA) | `.github/workflows/security.yml` job `govulncheck` + `make sec-vuln` | Reports stdlib + dependency CVEs that the code actually calls. v0.12.0 fix: workflows pin `go-version: 1.25.x` with `check-latest: true` so newer stdlib patches land automatically; go.mod floor `go 1.25.3` rejects older patches. |
| **Schedule** | `cron: "0 6 * * 1"` weekly | Catches new CVEs against unchanged code. |

The `make sec` target chains all three locally; `sec-reports/` is
gitignored.

### 3.2 What's still pending

| Tool | Why we want it | Notes |
| --- | --- | --- |
| **gitleaks** | Catches accidentally-committed credentials | Add as a job + a pre-commit hook (see §3.6 below). |
| **dependency-review-action** | Flags new vulnerable / restrictively-licensed dependencies on PRs | GitHub-native; runs only on PRs. |
| **Dependabot** | Weekly automated dependency PRs | `.github/dependabot.yml` — patch versions auto-merge on green CI; minor/major manual review. |
| **anchore/sbom-action** | SBOM (SPDX-JSON) attached to release artefacts | Required for some procurement processes; useful as CASA evidence. |
| **Baseline files** | Suppress small expected gosec/semgrep findings without blanket `--exclude` | `.gosec.json`, `.semgrepignore`. Each entry carries a one-line justification. |

### 3.3 SAST: gosec (already shipped)

The current workflow runs gosec with `-exclude-dir=docs
-exclude-generated`. SARIF format publishes to GitHub Security tab;
text format fails the job on any finding. False positives use
inline `// #nosec G<rule> — <reason>` comments, never blanket
suppression. New findings beyond the in-tree annotations fail the
build.

### 3.4 SAST: Semgrep (already shipped)

Configs: `p/golang`, `p/security-audit`, `p/secrets`. Same
baseline-and-fail-on-new pattern. Excludes `docs/` and
`internal/graph/testdata/` (recorded HTTP responses use
`example.invalid` and aren't real secrets).

### 3.5 SCA: govulncheck (already shipped)

Detects use of vulnerable functions in dependencies (more precise
than just flagging vulnerable modules). Run on every PR; fail on
any finding. v0.12.0 lesson learned: setup-go's
`go-version-file: go.mod` pins the EXACT version in the `go`
directive, so an older floor traps CI on stdlib CVEs already fixed
upstream. Workflows now use `go-version: 1.25.x` +
`check-latest: true` to always pull the latest 1.25 patch.

### 3.6 Pending: gitleaks

```yaml
- name: gitleaks
  uses: gitleaks/gitleaks-action@v2
```

Pre-commit hook companion:

```yaml
# .pre-commit-config.yaml
- repo: https://github.com/gitleaks/gitleaks
  rev: v8.18.0
  hooks:
    - id: gitleaks
```

### 3.7 Pending: dependency-review

```yaml
- name: dependency-review
  uses: actions/dependency-review-action@v4
  with:
    fail-on-severity: moderate
```

### 3.8 Pending: Dependabot

`.github/dependabot.yml` — weekly module updates, grouped by minor
version bumps. Auto-merge for patch versions in green CI; manual
review for minor and major.

**Grouping invariant (lesson from v0.13.x).** Dependabot opens one
PR per dependency by default. When multiple deps live in the same
file in adjacent lines (e.g. every CI job has both
`actions/checkout` and `actions/setup-go` in the same step block),
merging the first PR creates a 3-way conflict for every subsequent
PR — a recurring rebase chore. The fix: group ALL bumps in each
ecosystem into a single weekly PR via `groups: <name>: patterns:
["*"]`. Single PR, single rebase, single merge. The cost of
reviewing a fatter PR is much smaller than the cost of conflict-
resolving N-1 PRs each week.

### 3.9 Pending: SBOM

```yaml
- name: generate-sbom
  uses: anchore/sbom-action@v0
  with:
    format: spdx-json
    output-file: sbom.spdx.json
```

Generates SBOM in SPDX format on every release build. Attach to
GitHub releases.

## 4. Security-specific tests

These live in `internal/<package>/security_test.go` files alongside
normal unit tests. They produce evidence that hardening worked, not
just that it was designed.

### 4.1 File permission tests

```go
// internal/store/security_test.go
func TestDatabaseFileMode(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "mail.db")
    s, err := store.Open(path)
    require.NoError(t, err)
    s.Close()

    info, err := os.Stat(path)
    require.NoError(t, err)
    mode := info.Mode().Perm()
    assert.Equal(t, os.FileMode(0600), mode,
        "mail.db must be created with mode 0600 to prevent other users on the same system from reading mail content")
}

func TestLogFileMode(t *testing.T) {
    // Same pattern for log files in ~/Library/Logs/inkwell/
}

func TestAttachmentTempFileMode(t *testing.T) {
    // Verify attachment temp files are 0600 not 0644
}

func TestDraftTempFileMode(t *testing.T) {
    // Verify ~/Library/Caches/inkwell/drafts/ files are 0600
}
```

### 4.2 Log redaction tests

```go
// internal/log/security_test.go
func TestLogRedactsTokens(t *testing.T) {
    var buf bytes.Buffer
    logger := log.NewWithRedaction(&buf)

    logger.Info("auth headers",
        slog.String("Authorization", "Bearer eyJ0eXAiOiJKV1QiLCJh..."))
    logger.Info("response", slog.String("body", "access_token=abc123def456"))

    output := buf.String()
    assert.NotContains(t, output, "eyJ0eXAi")
    assert.NotContains(t, output, "abc123def456")
    assert.Contains(t, output, "[REDACTED]")
}

func TestLogRedactsEmailAddresses(t *testing.T) {
    // Verify configured redaction replaces emails with <email-N> placeholders
}

func TestLogRedactsSubjectsAtInfoLevel(t *testing.T) {
    // Verify subjects appear only at DEBUG, not INFO
}

func TestLogRedactsMessageBodies(t *testing.T) {
    // Verify message bodies never appear in logs at any level
}
```

These tests are critical because the redaction layer is implemented
in code, not just promised in `ARCH.md` §12. The test is the
verification.

### 4.3 Token storage tests

```go
// internal/auth/security_test.go
func TestTokensWrittenOnlyToKeychain(t *testing.T) {
    // Mock the keyring; verify the Authenticator only calls keyring.Set/Get
    // and never writes to filesystem.
}

func TestRefreshTokenNotInExportedConfig(t *testing.T) {
    // Verify config.toml writers don't accidentally include token data
}
```

### 4.4 Path traversal tests

```go
// internal/render/security_test.go
func TestAttachmentSavePathRejectsTraversal(t *testing.T) {
    cases := []string{
        "../../etc/passwd",
        "../../../tmp/evil",
        "/absolute/path/outside",
        "subdir/../../escape",
    }
    for _, name := range cases {
        t.Run(name, func(t *testing.T) {
            err := SaveAttachment(targetDir, name, []byte("data"))
            assert.Error(t, err)
            // Verify the file was NOT created outside targetDir
        })
    }
}

func TestFolderPickerRejectsTraversal(t *testing.T) {
    // Verify the move-to-folder picker can't be used to write to arbitrary
    // server-side folders via crafted folder names
}
```

### 4.5 TLS verification tests

```go
// internal/graph/security_test.go
func TestHTTPClientVerifiesCertificates(t *testing.T) {
    client := graph.NewClient(...)
    transport := client.HTTPClient().Transport.(*http.Transport)
    assert.False(t, transport.TLSClientConfig.InsecureSkipVerify,
        "TLS verification must never be disabled")
    assert.Equal(t, uint16(tls.VersionTLS12), transport.TLSClientConfig.MinVersion,
        "TLS 1.2 minimum")
}
```

### 4.6 SQL injection tests

```go
// internal/store/security_test.go
func TestPatternQueryParameterized(t *testing.T) {
    p, _ := pattern.Compile(`~f "bob'; DROP TABLE messages; --"`, opts)
    ids, err := pattern.Execute(ctx, p, store, graph)
    require.NoError(t, err)
    _, err = store.GetMessage(ctx, "any-id")
    require.NoError(t, err)
}
```

### 4.7 Subprocess injection tests

```go
// internal/render/security_test.go
func TestExternalConverterRejectsShellInjection(t *testing.T) {
    // Configure html_converter_cmd; verify HTML inputs containing shell
    // metachars are not interpreted by the shell
}

func TestEditorCommandUsesArgvNotShell(t *testing.T) {
    // Verify $EDITOR launching uses exec.Command, not exec.Command("sh", "-c", ...)
}

func TestOpenURLUsesArgvNotShell(t *testing.T) {
    // Verify the :open command for webLinks uses exec, not shell interpolation
}
```

### 4.8 Cryptographic randomness tests

```go
// internal/action/security_test.go
func TestActionIDsUseCryptoRand(t *testing.T) {
    // Generate 1000 action IDs; verify high entropy
    // (catches accidental use of math/rand instead of crypto/rand)
}
```

## 5. Documents to produce

### 5.1 `SECURITY.md` (repo root)

**Status:** Placeholder shipped in v0.12.0. The dedicated security
mailbox + PGP key are deliberately deferred to the v1.0 release
(currently v0.12.x is pre-release). The placeholder points
reporters to GitHub's private security-advisory feature and warns
against opening public issues.

Final v1.0 form (roughly):

```markdown
# Security Policy

## Supported versions
The latest minor release is supported. Older versions receive security
fixes for critical issues only.

## Reporting a vulnerability
Email security@inkwell.dev (PGP key: <fingerprint>). We aim to acknowledge
within 48 hours and provide a fix or mitigation timeline within 7 days.
Public disclosure happens after a fix is available, coordinated with the reporter.

Do NOT open public GitHub issues for security vulnerabilities.

## Scope
- The inkwell binary distributed via official Homebrew tap or GitHub Releases.
- The build pipeline that produces those binaries.

## Out of scope
- Vulnerabilities in Microsoft Graph itself (report to Microsoft).
- Vulnerabilities in third-party tooling (gosec, MSAL, etc.).
- Issues requiring local code execution (we assume the user's machine is trusted).
- Theoretical issues without a working PoC.

## Recognition
Security researchers who follow responsible disclosure are credited in
release notes (with permission).
```

### 5.2 `docs/THREAT_MODEL.md`

A formal threat model. Structure:

**Assets we protect:**
- OAuth access and refresh tokens.
- Cached message envelopes and bodies.
- Attachment files.
- User configuration (which tenant, which UPN, saved searches).
- Working memory of the running process (tokens may live there
  transiently).

**Trust boundaries:**
- Between inkwell and the user's macOS account: inkwell runs as the
  user; trusts the user.
- Between inkwell and other users on the same Mac: file mode 0600 +
  Keychain ACL; we DO NOT trust other users.
- Between inkwell and Microsoft Graph / Apple Push: TLS; we trust the
  certificate chain rooted in macOS's trust store.
- Between inkwell and the user's `$EDITOR`, `open`, `pbcopy`, etc.: we
  trust these because the user installed them.
- Between inkwell and the local filesystem: we don't trust pathnames
  coming from network sources (attachments, message links).

**Threats and mitigations** (table form):

| Threat | Mitigation |
| --- | --- |
| Token theft from disk | Keychain only; never written to filesystem in plaintext. |
| Token theft from process memory | Out of scope (full memory access = game over). |
| Token theft via swap | macOS encrypted swap. |
| Cache exfiltration by another user on the same Mac | File mode 0600. |
| Cache exfiltration by malware running as the user | Out of scope (malware as user = game over). |
| MITM on Graph traffic | TLS 1.2 minimum, full cert validation, system trust store. |
| Token leakage in logs | Structured slog handler with redaction layer; tested. |
| Path traversal via attachment names | `filepath.Clean` + verified containment in target dir. |
| Shell injection via $EDITOR / external converter | `exec.Command` with argv, never `exec.Command("sh", "-c", ...)`. |
| SQL injection via pattern language | All store queries are parameterized; no string concat. |
| Replay attack on action queue | Idempotent actions; server reconciles. |
| Misuse of denied-scope tokens | Scopes are hardcoded in source; tenant admin enforces consent. |
| Compromised dependency | govulncheck + Dependabot + SBOM published. |
| Tampered binary distribution | Code-signed and notarized via Apple Developer ID. |
| Backdoored build pipeline | GitHub Actions hardening: pinned actions by SHA, scoped tokens. |

**Things we don't defend against:**
- Compromised macOS / FileVault disabled.
- Compromised Microsoft tenant / Apple Developer account.
- Physical access to an unlocked machine.
- Compromised user (phishing, social engineering).

### 5.3 `docs/PRIVACY.md`

Required for the OAuth consent screen (Google explicitly requires
this; Microsoft's tenant admin may want it too).

```markdown
# Privacy Policy — inkwell

## What data we access
With the user's authorization, inkwell reads:
- Mail messages (envelopes, bodies, attachments) from their Microsoft 365
  mailbox via Microsoft Graph API.
- Calendar events from the same mailbox.
- Mailbox settings (out-of-office, time zone, etc.).

## What data leaves the user's device
Nothing, except calls to Microsoft Graph itself. inkwell does not:
- Send any data to inkwell-operated servers (we don't operate any).
- Send telemetry or analytics to anyone.
- Phone home for updates (Homebrew handles updates externally).
- Connect to any third-party AI service (no LLM features in v1).
- Connect to any tracking service.

## Where data is stored
- OAuth tokens: macOS Keychain.
- Cached mail (envelopes, bodies, attachments) and config:
  ~/Library/Application Support/inkwell/ (mode 0600).
- Logs: ~/Library/Logs/inkwell/ (with mail bodies and tokens redacted).
- Drafts in progress: ~/Library/Caches/inkwell/drafts/ (mode 0600;
  cleaned up after Graph confirms draft creation).

## How users can delete their data
- inkwell signout — clears tokens from Keychain.
- inkwell purge — clears the local cache.
- Or simply: rm -rf ~/Library/Application\ Support/inkwell ~/Library/Caches/inkwell ~/Library/Logs/inkwell.

## Third parties
Microsoft Graph (Microsoft Corporation) is the only third party that
receives data from inkwell, and only in the form of API calls authorized
by the user via OAuth.

## Contact
privacy@inkwell.dev
```

### 5.4 `docs/SECURITY_TESTS.md`

A document that maps inkwell's security tests to ASVS / CASA
requirements. Used as evidence in CASA self-attestation.

Format: a table where each row lists a CASA requirement, the ASVS
section it derives from, the test(s) that verify it, and pass/fail
status from the most recent CI run. Generated automatically from
test annotations:

```go
// SECURITY-MAP: V8.1.1 V8.2.1
// Verifies that mail.db is created with mode 0600.
func TestDatabaseFileMode(t *testing.T) { ... }
```

A small generator (`scripts/security-map.go`) walks the test files,
extracts these annotations, and produces `SECURITY_TESTS.md`. Run
as part of CI; commit the generated file.

## 6. Pre-CASA-submission checklist

When the time comes to submit for CASA Tier 2:

- [ ] All CI checks (gosec, semgrep, govulncheck, gitleaks,
      dependency-review) green on `main` for at least 30 days.
- [ ] No high-severity SAST findings; all medium-severity findings
      either fixed or documented in `.gosec.json` baseline with
      justification.
- [ ] Zero govulncheck findings.
- [ ] All security tests in §4 passing.
- [ ] `SECURITY.md`, `THREAT_MODEL.md`, `PRIVACY.md`,
      `SECURITY_TESTS.md` published and current.
- [ ] SBOM generated and attached to the most recent release.
- [ ] Domain registered, homepage published, privacy policy hosted at
      first-party domain (NOT GitHub Pages on `*.github.io`).
- [ ] Demo video recorded showing OAuth flow end-to-end, in English
      (Google CASA requirement).
- [ ] OAuth consent screen configured in Google Cloud Console with all
      required fields.
- [ ] Test users added (yourself + a small group) before requesting
      verification.

The CASA self-attestation portal will then ask Yes/No/N-A questions
for the ~20 requirements not covered by the SAST scan. Each answer
is one paragraph pointing at the relevant code, document, or test
result.

## 7. Configuration

This spec adds no new runtime config keys.

It does add CI configuration:
- `.github/workflows/security.yml` (already shipped)
- `.gosec.json` (baseline) — pending
- `.semgrepignore` (baseline) — pending
- `.github/dependabot.yml` — pending
- `.gitleaks.toml` — pending
- `.pre-commit-config.yaml` — pending

## 8. Failure modes

| Scenario | Behavior |
| --- | --- |
| New gosec finding above threshold | CI fails; PR cannot merge until fixed or explicitly baselined with justification. |
| New govulncheck finding | CI fails; must be addressed by updating the dependency (or, for stdlib CVEs, bumping the Go floor / `check-latest: true` toolchain pin). |
| New gitleaks finding | CI fails; the secret must be rotated and the commit history cleaned. |
| Dependency review flags new dependency | PR review highlights it; reviewer decides whether to merge. |
| Security test fails | Treated as a blocker bug; release halted until fixed. |
| SAST tool itself becomes vulnerable / abandoned | Documented in `THREAT_MODEL.md` as accepted risk; mitigated by using multiple tools (gosec + semgrep). |

## 9. Test plan

This spec IS partly a test plan. The §4 tests verify the security
properties; the §3 tools detect regressions in shipped code; the §5
documents are the manual-review evidence.

In addition, run the security suite quarterly with intent:
- Manually review SARIF outputs for newly introduced false negatives.
- Manually attempt the threats listed in `THREAT_MODEL.md` against the
  running app.
- Spot-check log files from real usage to confirm redaction works on
  real data, not just test fixtures.

## 10. Definition of done

- [x] gosec + Semgrep + govulncheck wired into
      `.github/workflows/security.yml` (v0.12.0).
- [x] Local `make sec` chain produces reports under `sec-reports/`
      (gitignored) (v0.12.0).
- [x] `SECURITY.md` placeholder shipped at repo root, pointing to
      GitHub private advisories (v0.12.0).
- [x] CI Go floor bumped to 1.25.3 + workflows pin `1.25.x` with
      `check-latest: true` (v0.12.x post-mortem).
- [x] gitleaks workflow (full-history scan via `fetch-depth: 0`)
      (v0.13.0). Pre-commit hook still pending.
- [x] dependency-review-action on PRs (v0.13.0).
- [x] `.github/dependabot.yml` weekly schedule (v0.13.0).
- [x] SBOM (SPDX-JSON) generated on release builds and attached to
      GitHub releases via `gh release upload --clobber` (v0.13.0).
- [x] First cut of §4 security tests (file modes for db + draft
      tempfile, SQL-injection survival via `SearchByPredicate`,
      editor argv form, action ID entropy). Remaining §4 bullets
      tracked in plan file with explicit deferral rationale.
- [x] `docs/THREAT_MODEL.md` published (v0.13.0).
- [x] `docs/PRIVACY.md` published (v0.13.0).
- [ ] `.gosec.json` baseline file with documented justifications.
      Deferred — current state is 0 findings + 9 inline `#nosec`
      annotations; switch to a baseline file once the inline list
      crosses double digits.
- [ ] `SECURITY.md` filled in with real mailbox + PGP key (deferred
      to v1.0).
- [ ] `docs/SECURITY_TESTS.md` generator + checked-in output.
      `// SECURITY-MAP:` annotations are added in v0.13.0; the
      AST-walker generator lands as a follow-up so we don't ship
      an unmaintained tool.
- [ ] Pre-commit hook for gitleaks. CI gate covers the same
      ground; lands when contributors hit it as friction.
- [ ] At least one full quarterly review cycle completed with
      documented findings.

## 11. Out of scope for this spec

- Penetration testing by third parties (CASA Tier 3 territory; not
  pursuing).
- Bug bounty program (post-launch, if adoption warrants).
- Formal threat modeling tools (Microsoft Threat Modeling Tool,
  IriusRisk). The Markdown-based threat model is sufficient for our
  scale.
- Runtime application self-protection (RASP) — not applicable to a
  desktop client.
- Hardware security module (HSM) integration for token storage —
  Keychain is sufficient.
- Reproducible builds. Nice-to-have but expensive; deferred unless
  adoption justifies it.

## 12. Notes for Claude Code

When implementing the rest of this spec, do these in order:

1. **Wrap up the remaining tooling** (§3.6–§3.9). gitleaks first
   (cheapest, highest hit-rate against accidental secret commits),
   then Dependabot, then SBOM, then `.gosec.json` baseline.

2. **Implement security tests** (§4) one section at a time. Each
   section maps to a single internal package, so they can land as
   separate PRs.

3. **Generate documents** (§5). They depend on what was actually
   implemented, so writing them last avoids the doc-and-code drift
   problem.

4. **The `SECURITY_TESTS.md` generator** (§5.4) is the cleanest
   piece to write — Go AST parsing of test files with
   `// SECURITY-MAP:` annotations. Probably ~150 LOC.

5. **The threat model is a living document.** Don't try to make it
   complete on first pass. Start with the obvious threats from §5.2's
   table and add more as you encounter scenarios during implementation.

6. **CASA submission is its own project.** This spec produces the
   artifacts; the actual CASA portal interaction is a separate ~1
   week of calendar time, mostly waiting for the lab review.

## 13. Cross-cutting policy: every future spec

Per CLAUDE.md, every spec PR must check this spec's threat model
and privacy policy for required updates:

- **Threats added or removed?** A new feature that touches tokens,
  files, subprocesses, or external HTTP must update
  `docs/THREAT_MODEL.md`'s threats-and-mitigations table.
- **New data flow?** A feature that introduces a new data flow (new
  third party, new local file, new server-side write) must update
  `docs/PRIVACY.md`.
- **New CI gate needed?** A feature that introduces a new class of
  failure mode (e.g. binary signing, new cryptographic primitive) may
  require a new CI gate; surface it in the spec PR description.

The spec template's cross-cutting checklist enforces this; reviewers
block on it.
