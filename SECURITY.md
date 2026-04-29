# Security policy

> **Status:** placeholder. Inkwell is in active pre-1.0 development
> (currently v0.12.x). The dedicated security mailbox + PGP key, the
> coordinated-disclosure SLA, and the supported-versions matrix will
> all be defined and published as part of the **v1.0 release**.
> Spec [`17-security-testing-and-casa-evidence.md`](docs/specs/17-security-testing-and-casa-evidence.md)
> tracks the work.

## Reporting a vulnerability — pre-1.0

Until the security mailbox is published, please use **GitHub's
private security advisory** mechanism to report a vulnerability:

> **Security → Advisories → Report a vulnerability** on the
> repository's GitHub page, or directly at
> <https://github.com/eugenelim/inkwell/security/advisories/new>.

This sends the report privately to the maintainer. We aim to
acknowledge within a few days during pre-1.0; the formal SLA
(48h acknowledgement, 7-day mitigation timeline) lands at v1.0.

**Do NOT open public GitHub issues for security vulnerabilities.**
Pre-1.0 issues are still public; the private-advisory flow is the
right channel.

## Scope (pre-1.0)

- The `inkwell` binary distributed via the GitHub Releases page of
  this repository.
- The build pipeline that produces those binaries
  (`.github/workflows/release.yml`, `scripts/`).
- The CI pipeline (`.github/workflows/ci.yml`,
  `.github/workflows/security.yml`) — issues that would let a
  malicious PR exfiltrate secrets, tamper with releases, or bypass
  the SAST/SCA gates.

## Out of scope

- Vulnerabilities in Microsoft Graph itself — report to Microsoft.
- Vulnerabilities in third-party dependencies — report upstream.
  Inkwell's `govulncheck` CI gate catches dependency CVEs the code
  actually exercises; if you've found one not flagged, that's worth
  reporting.
- Issues requiring local code execution as the user (we trust the
  user's machine).
- Theoretical issues without a working PoC.

## Security CI gates

On every PR and on `main`, the following run automatically (see
`.github/workflows/security.yml`):

- **gosec** — Go-specific SAST. Fails on any finding.
- **Semgrep** — multi-language SAST (`p/golang`, `p/security-audit`,
  `p/secrets`).
- **govulncheck** — official Go vulnerability scanner against the
  stdlib and module graph; pinned to the latest 1.25.x patch via
  `go-version: 1.25.x` + `check-latest: true` in the workflows.

Local equivalents via `make sec` (Makefile target). Reports land
under `sec-reports/` (gitignored).

## Recognition

Security researchers who follow the private-advisory disclosure flow
above will be credited in the release notes for the version that
ships the fix (with permission). Recognition is symbolic during
pre-1.0; a formal program lands at v1.0 if adoption warrants it.

## What this file will contain at v1.0

- A real security mailbox (`security@<domain>`) with PGP fingerprint.
- Acknowledgement / mitigation / disclosure SLAs.
- A supported-versions matrix.
- A scope expansion to cover signed/notarized distribution channels
  (Homebrew tap, signed `.pkg`, etc.) once those exist.

Until then: **GitHub private security advisories.**
