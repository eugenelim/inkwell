# ADR 0003: Microsoft Graph v1.0 only — no /beta, Outlook REST, EWS, or IMAP

- **Status:** Accepted (2026-05-13)
- **Deciders:** eugenelim
- **Supersedes:** —
- **Related:** ARCH §0, PRD §3.1, ADR-0004

## Context

Microsoft exposes mailbox data through several APIs at different
states of support:

| Surface | URL | Status (as of 2026-05) |
| --- | --- | --- |
| Graph v1.0 | `https://graph.microsoft.com/v1.0` | Stable, SLA, recommended |
| Graph beta | `https://graph.microsoft.com/beta` | Unstable, may change without notice |
| Outlook REST v2.0 | `https://outlook.office.com/api/v2.0` | Deprecated Nov 2020; decommissioned Mar 2024 |
| Exchange Web Services | varies | Deprecated; retiring Oct 2026 |
| IMAP / SMTP | mailbox host | OAuth-only since Oct 2022; Basic Auth retired |

Each is a temptation. Outlook REST v2.0 has a simpler shape for some
resources. Beta has the `mailboxItem` resource and finer-grained
deltas. EWS is what every legacy email client knows. IMAP is the
universal protocol.

We need exactly one surface to build against, and it has to outlive
the product.

## Decision

Inkwell targets **Microsoft Graph v1.0 only**, at
`https://graph.microsoft.com/v1.0`. Code paths that would call beta,
Outlook REST, EWS, or IMAP are forbidden. The `internal/graph` package
is the only caller of Graph; it pins the base URL to v1.0 and treats
any other host as a programming error.

A CI lint (in `.github/workflows/`) refuses commits that reference the
forbidden URLs in `.go` files. Comments that *mention* the forbidden
surfaces (e.g. ARCH §0) are fine — the guard is for code paths.

## Consequences

### Positive
- One API to learn. Documentation, throttling guidance, and SDK
  examples all converge on a single source.
- Microsoft's stability commitment for v1.0 outlives the product's
  expected lifetime.
- No risk of an endpoint disappearing mid-release.
- Per-tenant configurability is uniform — Graph permissions are the
  only auth contract.

### Negative
- Lose access to beta-only features (e.g. some finer triage
  signals, certain delta optimizations). When a beta feature is
  load-bearing for a planned capability, the work waits for GA.
- Some operations are slightly more chatty on v1.0 than on the
  retired v2.0 (e.g. property selection nuances). Acceptable.

### Neutral
- We can never offer parity with apps that opt into beta.

## Alternatives considered

**Graph beta opt-in for select endpoints.** Considered for delta query
performance. Rejected — once a single beta call lands, the discipline
erodes; the next contributor sees the precedent and reaches for more.
The stability cost compounds.

**Outlook REST v2.0.** Rejected: officially decommissioned March 2024.
Tenants that still return 200s are doing so on residual back-compat
that has no SLA.

**EWS.** Rejected: deprecated, retiring October 2026 — inside the
product's expected life.

**IMAP / SMTP.** Rejected: requires OAuth tokens anyway, has no
mailbox-management primitives (folders, categories, flags) that Graph
offers, and would force a parallel auth path for read-only mail
access while leaving everything else on Graph.

## References

- ARCH.md §0 (API surface, locked).
- PRD.md §3.1 (granted Graph scopes).
- ADR-0004 — no Graph SDK, direct HTTP.
- [Microsoft: Outlook REST v2.0 deprecation](https://learn.microsoft.com/en-us/outlook/rest/compare-graph) (2020-11).
