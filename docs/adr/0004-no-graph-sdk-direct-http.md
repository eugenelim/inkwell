# ADR 0004: No Microsoft Graph SDK — `net/http` directly

- **Status:** Accepted (2026-05-13)
- **Deciders:** eugenelim
- **Supersedes:** —
- **Related:** ARCH §1, ARCH §5, ADR-0003

## Context

Microsoft publishes `msgraph-sdk-go`, an officially supported Graph
SDK for Go. It handles auth, retries, paging, batching, and exposes
typed accessors for every Graph resource. On the face of it, it's the
expected dependency.

Inkwell, however, depends on tight control over the HTTP layer for
three reasons:

1. **Throttling.** Outlook resources have per-mailbox concurrency
   limits that the generic SDK throttler doesn't tune for. We need to
   serialize writes per-mailbox while parallelising reads (ARCH §5.2).
2. **`$batch` shaping.** Graph's `$batch` endpoint accepts up to 20
   sub-requests per call. The SDK's batch helper hides the ordering
   semantics (dependency chains via `dependsOn`) that we exploit for
   action-queue execution (ARCH §5.3).
3. **`$select` discipline.** Every Graph call we make is `$select`-ed
   to the exact fields we persist, both to minimise wire bytes and to
   keep our local cache schema in sync with what we ask for. The SDK
   defaults to returning the full resource and `$select`-overriding
   per-call is verbose.

We also have spec-17 obligations to redact log lines, which means our
HTTP transport needs to be the redaction site. Layering a redactor
under the SDK's transport works, but the SDK's logging hooks change
shape across major versions.

## Decision

The `internal/graph` package builds requests with `net/http` directly.
It owns:

- A single `*http.Client` with a custom `Transport` that handles
  retries with respect for `Retry-After`, 429s, and exponential
  backoff on 5xx.
- A `$batch` request builder typed to inkwell's action queue shape.
- Helpers for delta-tokenised list endpoints.
- The log-redaction site for outbound headers and inbound bodies.

No package outside `internal/graph` makes outbound calls to
`graph.microsoft.com`.

## Consequences

### Positive
- We control retry behaviour, batching shape, `$select` discipline,
  and redaction. None of these are extension points in a third-party
  client; owning them outright is simpler than working around them.
- Dependency tree stays small. `msgraph-sdk-go` pulls in `kiota` and
  several transitive dependencies; skipping the SDK keeps the binary
  smaller and the supply-chain surface narrower (spec 17).
- No SDK-version churn. Microsoft's SDK has had API breaks across
  major versions; our hand-written client doesn't move unless we move
  it.

### Negative
- Every new Graph endpoint is hand-coded — typed request/response
  structs, $select fields, error shape. The SDK gets new endpoints
  "for free" when Microsoft updates it.
- We must keep our client honest about Graph's documented behaviour.
  If Microsoft changes a response shape, our typed structs don't
  auto-adapt.
- No autocompletion-grade discoverability for Graph resources.

### Neutral
- Code reviewers must read Graph's REST docs alongside our client
  code. Acceptable — they have to anyway for `$select` discipline.

## Alternatives considered

**`msgraph-sdk-go` (the official SDK).** Rejected for the three
control reasons in Context. The SDK is correct for apps that need
broad Graph coverage and don't care about wire-level shaping;
inkwell needs the opposite.

**Generate a typed client from Microsoft's OpenAPI.** Considered. The
spec is enormous and we use ~20 endpoints; the generated surface is
dead weight. Rejected.

**Use a thin community wrapper (e.g. `microsoft/kiota-http-go`).**
Same downsides as the SDK in slightly different shape. Rejected.

## References

- ARCH.md §1 (Tech stack), §5 (Graph client design).
- ADR-0003 — Graph v1.0 only.
- spec 17 — security/redaction expectations the transport must meet.
