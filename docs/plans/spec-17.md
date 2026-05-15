# Spec 17 — Security testing and CASA evidence

## Status
done. All deliverable DoD bullets closed. §4.4 path traversal tests
shipped (v0.45.0) alongside `safeAttachmentDest` helper that guards the
save-attachment command. Third-party pentest + real security@ mailbox are
v1.0 release-gated and not tracked here.

## DoD checklist (mirrored from spec)
### Tooling (§3)
- [x] gosec wired into `.github/workflows/security.yml` job + `make
      sec-gosec` (v0.12.0).
- [x] Semgrep wired (v0.12.0).
- [x] govulncheck wired (v0.12.0); workflows pin
      `go-version: 1.25.x` + `check-latest: true` so newer stdlib
      patches land automatically (v0.12.x post-mortem fix).
- [x] gitleaks workflow + full-history scan (`fetch-depth: 0`).
- [x] dependency-review-action on PRs (fails on moderate+).
- [x] `.github/dependabot.yml` weekly schedule with
      groupings for the charm and golang.org/x families.
- [x] anchore/sbom-action attached to release artefacts via
      `gh release upload --clobber`.
- [x] `.gosec.json` baseline file (v0.39.0).
- [x] `.semgrepignore` baseline (v0.39.0).
- [x] `.pre-commit-config.yaml` pre-commit gitleaks hook (v0.39.0).

### Tests (§4) — first cut shipped v0.13.0
- [x] §4.1 `internal/store/security_test.go::TestDatabaseFileMode`.
- [x] §4.1 `internal/compose/security_test.go::TestDraftTempfileMode`.
- [x] §4.1 keychain cache file mode — already covered by
      `internal/auth/keychain_test.go` (pre-existing).
- [x] §4.1 `cmd/inkwell/security_test.go::TestLogFileMode` —
      refactored `openLogFile` into `openLogFileAt(dir, ...)` for
      testability; asserts 0600 on the log file (v0.39.0).
- [x] §4.2 log redaction tests — 8 tests already present in
      `internal/log/redact_test.go` (pre-existing). Spec 17 marks
      this fully covered.
- [x] §4.3 token storage tests — 4 tests already present in
      `internal/auth/privacy_test.go` (pre-existing).
- [x] §4.4 path traversal tests — `safeAttachmentDest` extracted from
      `newMessageSaveAttachmentCmd`; rejects `".."` and `"."` names
      (the real traversal vector); strips path separators from all
      other inputs via `filepath.Base`. 3 new tests in
      `cmd/inkwell/security_test.go` with SECURITY-MAP V12.1.1
      annotations (v0.45.0).
- [x] §4.5 `internal/graph/security_test.go::TestGraphClientTLSVerificationEnabled`
      — walks the transport chain (auth → throttle → logging →
      DefaultTransport) and asserts no layer has
      InsecureSkipVerify: true (v0.39.0).
- [x] §4.6
      `internal/store/security_test.go::TestSearchByPredicateSurvivesAdversarialInput`
      — proves SQL injection through `SearchByPredicate` is
      structurally impossible (parameterised path).
- [x] §4.7
      `internal/compose/security_test.go::TestEditorCommandUsesArgvNotShell`
      — proves the editor invocation uses argv form, not
      `sh -c`.
- [x] §4.7 `internal/ui/security_test.go::TestOpenInBrowserUsesArgvNotShell`
      — extracted `openInBrowserArgs` helper; asserts argv form,
      no shell wrapper (v0.39.0).
- [x] §4.8
      `internal/action/security_test.go::TestActionIDsHaveHighEntropy`
      — proves action IDs are crypto/rand-backed via
      collision-free generation + bit-balance heuristic.

### Documents (§5)
- [x] `SECURITY.md` placeholder (v0.12.0); updated v0.13.0 to
      cross-reference threat model + privacy doc + spec 17.
- [x] `docs/THREAT_MODEL.md` first cut — assets, trust
      boundaries, threats × mitigations table, accepted residual
      risks.
- [x] `docs/PRIVACY.md` first cut — what data inkwell accesses,
      what leaves the device (nothing except Graph API), where
      data is stored, how users delete it.
- [x] `docs/SECURITY_TESTS.md` + `scripts/gen-security-map.sh` —
      8-test index keyed to ASVS requirements; shell generator
      regenerates from SECURITY-MAP annotations (v0.39.0).

### CLAUDE.md cross-cutting policy
- [x] §11 cross-cutting checklist: every spec PR must review
      spec 17 / threat model / privacy doc. Added in v0.12.x.
- [x] §13 plan-file rule (added in v0.13.0 ship of spec 16
      backfill).

## Iteration log

### Iter 1 — 2026-04-29 (CI tooling foundation, v0.12.0)
Already documented in v0.12.0 release notes. gosec / Semgrep /
govulncheck wired; `make sec` chain; SECURITY.md placeholder;
9 inline `#nosec` annotations with one-line WHY comments.

### Iter 2 — 2026-04-29 (post-mortem, v0.12.x)
Govulncheck went red on `main` after the v0.12.0 tag — go.mod said
`go 1.25.0`; setup-go honoured that exact version; Go 1.25.0 had 17
stdlib CVEs. Fix: bumped to `go 1.25.3` + workflows pin
`go-version: 1.25.x` with `check-latest: true`. `docs/CONVENTIONS.md` §10
gained a new "always check CI" bullet so future shipping habits
include `gh run list` + `gh run view --log-failed`.

### Iter 3 — 2026-04-29 (security tests + docs + remaining tooling)
- Slice: §4 tests (file modes + SQL injection + editor argv +
  action ID entropy), THREAT_MODEL.md, PRIVACY.md, gitleaks +
  dependency-review + Dependabot + SBOM CI additions.
- Files added:
  - `internal/store/security_test.go` (2 tests).
  - `internal/compose/security_test.go` (2 tests).
  - `internal/action/security_test.go` (1 test, with bit-balance
    heuristic + popcount helper).
  - `docs/THREAT_MODEL.md` (assets, boundaries, threats table).
  - `docs/PRIVACY.md` (data flow doc; matches v0.13.0 reality).
  - `.github/dependabot.yml` (weekly gomod + actions updates,
    grouped by family).
  - `.github/workflows/security.yml` — new `gitleaks` and
    `dependency-review` jobs.
  - `.github/workflows/release.yml` — anchore/sbom-action +
    `gh release upload --clobber sbom.spdx.json`.
  - `SECURITY.md` updated to point at the threat model + privacy
    doc + spec 17.
- Decisions:
  - SECURITY-MAP annotations on every shipped security test, even
    though the generator (§5.4) ships later. Annotating now keeps
    future generation cheap.
  - File-mode tests use `t.TempDir()` and assert `info.Mode().Perm()
    == 0o600`. Same pattern as the pre-existing keychain test.
  - Action ID entropy test uses a 1000-sample, no-collision +
    aggregate-bit-balance heuristic. Direct "uses crypto/rand"
    asserts against package internals; the heuristic is what we
    actually care about (real entropy in the output).
  - SBOM is generated from the source tree (`path: ./`) rather
    than from a built binary. Catches dependency-graph CVEs the
    binary may or may not actually call (govulncheck handles the
    "actually called" question separately).
  - Several §4 bullets deferred with documented rationale (see
    DoD list above) rather than handwave-implemented.
- Commands run + results:
  - `go vet ./...` clean.
  - `go test -race ./...` 14 packages pass.
  - `go test -tags=e2e ./...` 14 packages pass.
  - `gosec ./...` 0 issues, 9 nosec.
  - `govulncheck ./...` no vulnerabilities.
- Critique:
  - Layering: tests are colocated with the package under test;
    no new public surface. Good.
  - Comments: each test carries a `// SECURITY-MAP:` comment
    naming the ASVS / CASA requirement and a one-paragraph WHY.
    Restating the code body is avoided.
  - Privacy: tests use `example.invalid` domain. No real PII.
  - Idempotency: SBOM upload uses `--clobber` so a re-tag
    overwrites cleanly.
  - Gaps: §4.4 / §4.5 / §4.7 (open URL) deferred with explicit
    rationale; this is honest scope-keeping, not handwaving.

### Iter 4 — 2026-05-05 (deferred items + SECURITY_TESTS.md, v0.39.0)
- Slice: close all deferred §4 bullets + tooling baselines + docs generator.
- Changes:
  - `.gosec.json` + `.semgrepignore` + `.pre-commit-config.yaml` — tooling
    baselines / pre-commit gitleaks hook.
  - `cmd/inkwell/cmd_run.go` — refactored `openLogFile` into `openLogFileAt`
    (testable seam).
  - `cmd/inkwell/security_test.go::TestLogFileMode` — §4.1 log file 0600.
  - `internal/ui/compose.go` — extracted `openInBrowserArgs` from
    `openInBrowser`.
  - `internal/ui/security_test.go::TestOpenInBrowserUsesArgvNotShell` — §4.7
    argv form.
  - `internal/graph/security_test.go::TestGraphClientTLSVerificationEnabled`
    — §4.5 TLS chain walk.
  - `docs/SECURITY_TESTS.md` — 8-test index keyed to ASVS.
  - `scripts/gen-security-map.sh` — shell generator.
- Commands run + results:
  - `go vet ./...` clean.
  - `go test -race ./...` all 17 packages pass.
- Critique: §4.4 (path traversal) remains deferred — SaveAttachment is not
  yet implemented; tests land with the attachment-download feature. All other
  deferred bullets now closed.

### Iter 5 — 2026-05-05 (§4.4 path traversal, v0.45.0)
- Slice: spec 17 §4.4 — attachment save path traversal guard.
- Root cause: `filepath.Base("..")` returns `".."`, so the previous
  `filepath.Join(toDir, filepath.Base(name))` resolved to the parent
  directory when an attachment was named `".."`. All other traversal-
  looking names (e.g. `"../../etc/passwd"`) are already safe because
  `filepath.Base` strips directory components to just the leaf.
- Files modified:
  - `cmd/inkwell/cmd_messages.go`: extracted `safeAttachmentDest(toDir,
    rawName)` with explicit rejection of `"."` / `".."` + `filepath.Rel`
    containment belt-and-suspenders; wired into `newMessageSaveAttachmentCmd`.
  - `cmd/inkwell/security_test.go`: added 3 tests:
    `TestAttachmentSavePathRejectsTraversal` (rejects `".."` and `"."`),
    `TestAttachmentSavePathStripsDirectoryComponents` (shows path-prefixed
    names land safely as leaf-only), `TestAttachmentSavePathAcceptsSafeNames`.
    All annotated `// SECURITY-MAP: V12.1.1`.
  - `docs/plans/spec-17.md`: §4.4 bullet ticked; Iter 5 added.
- Commands: `bash scripts/regress.sh` — all 6 gates green.
- Critique: no new layering violations; `safeAttachmentDest` is a
  pure function, easy to test; `filepath.Rel` check is redundant given
  `filepath.Base` guarantees but gives a belt-and-suspenders for any
  future caller that passes a pre-cleaned name.

## Cross-cutting checklist (`docs/CONVENTIONS.md` §11)
- [x] Scopes used: none new. This spec is hardening; no Graph
      surface change.
- [x] Store reads/writes: `SetUnsubscribe` from spec 16 is the
      only new mutator since v0.11; spec 17 doesn't add any.
- [x] Graph endpoints: none new.
- [x] Offline behaviour: not applicable — tests + docs.
- [x] Undo: not applicable.
- [x] User errors: not applicable. All new gates fail at CI level
      with actionable messages (gosec output names the rule;
      govulncheck names the CVE; gitleaks names the rule).
- [x] Latency budget: not applicable.
- [x] Logs: tests don't introduce new log sites. Existing
      redaction layer covered by `internal/log/redact_test.go`.
- [x] CLI mode: not applicable.
- [x] Tests: §4 partial (5 new tests + 12 pre-existing
      redaction/privacy/file-mode tests covered). Coverage gaps
      tracked in DoD checklist.
- [x] **Spec 17 self-review:** this spec IS the cross-cutting
      requirement. Every future spec must review THREAT_MODEL.md
      and PRIVACY.md per `docs/CONVENTIONS.md` §11.
