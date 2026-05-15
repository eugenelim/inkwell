# internal/graph — AGENTS.md

Package-specific contract. Read the root `AGENTS.md` (entry point) and `docs/CONVENTIONS.md` (long-form rules, §-numbered) first for repo-wide
conventions; this file only spells out what's different about `graph`.

## What this package is

The only package in the repo permitted to make HTTP calls to
`https://graph.microsoft.com`. Exposes a typed REST client with
batching, throttling, retry, and redaction-aware logging.

## Hard invariants (specific to this package)

1. **Microsoft Graph v1.0 only.** Base URL is pinned to
   `https://graph.microsoft.com/v1.0`. No `/beta`, no Outlook REST,
   no EWS, no IMAP. CI lints code for forbidden hosts (ADR-0003).
2. **No Microsoft Graph SDK.** Requests built with `net/http`
   directly. We own the transport because we own throttling, batching
   shape, `$select` discipline, and redaction (ADR-0004).
3. **`$select` everything.** Every list/get call must `$select` the
   exact fields persisted by `internal/store`. Returning the whole
   resource wastes wire bytes and creates schema-drift risk.
4. **Per-mailbox write serialization.** Outlook throttles concurrent
   writes per mailbox; the transport (`client.go`) holds a per-mailbox
   write semaphore. Reads run in parallel up to a global ceiling.
5. **`$batch` ≤ 20 sub-requests, dependency-aware.** The batch builder
   in `batch.go` enforces the cap. Use `dependsOn` for ordered actions
   (move-then-mark-read patterns).
6. **Redaction at the transport.** Outbound `Authorization` headers and
   inbound body content pass through `internal/log/redact.go` before
   any log site sees them. Every new endpoint adds a redaction test
   covering its body shape (spec 17).

## Error handling

- `*GraphError` carries the Graph error code, HTTP status, and
  `request-id`. Callers branch on the code (`ErrorItemNotFound` is
  success-for-delete, `ErrorThrottled` triggers backoff, etc.).
- 429 / 503 with `Retry-After` is handled in the transport. Callers
  see a synthetic success after retry, or a `*GraphError` after the
  max-attempt budget.
- Never `context.Background()` inside the request path — propagate the
  caller's context for cancellation.

## Testing

- Real `httptest.Server` replaying canned JSON from `testdata/`. Tests
  must not require network; CI runs offline.
- Use the `integration` build tag for tests that mimic Graph's
  throttling/retry shape end-to-end.
- Spec-17 security tests in `security_test.go` cover scope violations,
  redaction completeness, and the Mail.Send guard.

## Common pitfalls

- New endpoint added without `$select` — caught by the unit test that
  asserts every list call carries `$select=…`.
- Logging a response body without redaction — caught by the redact
  test suite, which fails if a non-redacted field name appears in log
  output for a request that touched that field.
- Importing `internal/store` or `internal/action` from this package —
  these are upper-layer consumers; `graph` returns typed structs and
  lets consumers persist them.

## References

- spec 03 (sync engine), spec 14 (CLI), spec 17 (security testing).
- ARCH §0 (API surface), §1 (tech stack), §5 (graph client design).
- ADR-0003 (Graph v1.0 only), ADR-0004 (no Graph SDK).
