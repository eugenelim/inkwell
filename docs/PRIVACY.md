# Privacy policy — inkwell

> **Status:** v0.13.0 first cut. The dedicated privacy mailbox lands
> with the v1.0 release (currently pre-1.0); see SECURITY.md.

## What data inkwell accesses

With the user's authorisation (OAuth device-code flow against
their organisation's Microsoft 365 tenant), inkwell reads:

- **Mail** — message envelopes (headers, sender, recipients,
  subject), bodies, and attachments from the user's Microsoft 365
  mailbox via the Microsoft Graph API.
- **Calendar** — events from the same mailbox (read-only in v1).
- **Mailbox settings** — out-of-office state, time zone (read +
  toggle on/off in v1).

The exact OAuth scopes requested are listed in
[`internal/auth/scopes.go`](../internal/auth/scopes.go) and bounded
by [`docs/PRD.md`](PRD.md) §3.1. Scopes outside that list are
explicitly out of scope (`Mail.Send`, `Calendars.ReadWrite`,
shared-mailbox scopes, etc.).

## What data leaves the user's device

Nothing, except API calls to Microsoft Graph itself. inkwell does
NOT:

- Send any data to inkwell-operated servers (we don't operate any
  servers).
- Send telemetry or analytics anywhere.
- Phone home for updates (Homebrew / OS package manager handle
  updates externally).
- Connect to any third-party AI service (no LLM features in v1).
- Connect to any tracking, analytics, or crash-reporting service.

The only outbound HTTP traffic, beyond Microsoft Graph and AAD
(Microsoft's identity platform), is a single one-click HTTPS POST
to the URL in a message's `List-Unsubscribe` header — and only
when the user explicitly presses `U` and confirms in the modal.
That POST carries the body `List-Unsubscribe=One-Click`
(RFC 8058 §3.1), a generic `User-Agent: inkwell/<version>`, and
nothing else. No cookies, no referer.

**Reply Later / Set Aside (spec 25) sync via Graph categories.**
The two stacks store their state as Microsoft Graph categories on
each message: `Inkwell/ReplyLater` and `Inkwell/SetAside`. Adding
a message to either stack PATCHes the message resource on the
user's mailbox so the state syncs across devices and reappears in
inkwell on the same account on a different machine. The category
strings are visible to **anyone with delegated access to the
mailbox** (executive assistants, compliance reviewers, eDiscovery
exports). The behavioural metadata exposure is intentional and
acknowledged: the trade-off is cross-device sync without operating
inkwell-side servers. Users who don't want this exposure can
substitute the same workflow with the `~G` pattern operator over
non-`Inkwell/`-prefixed categories or saved searches.

## Where data is stored

| Data | Location | Mode | Notes |
| --- | --- | --- | --- |
| OAuth access + refresh tokens | macOS Keychain (encrypted at rest by the OS) | Keychain ACL | Never written to filesystem in plaintext. CLAUDE.md §7 rule 2. |
| Cached mail (envelopes, bodies, attachments) | `~/Library/Application Support/inkwell/mail.db` | 0600 | SQLite DB. Verified by `internal/store/security_test.go::TestDatabaseFileMode`. |
| Local-only metadata tables in `mail.db` | same file as above | 0600 | `muted_conversations` (spec 19), `sender_routing` (spec 23), `saved_searches.tab_order` (spec 24), `bundled_senders` (spec 26). All per-account, FK-cascade on account delete. None of this is sent to Graph. |
| Custom action recipes | `~/.config/inkwell/actions.toml` (override via `[custom_actions].file`) | user-set | TOML file authored by the user (spec 27). Read at startup; never written to. Templates render against the focused-message context but the user's typed `prompt_value` response is never logged at any level. |
| Screener gate (spec 28) | reads `sender_routing` (already covered above); no new state | — | The screener is a local-only filter layer over the spec 23 table. The decision "this sender is screened out" is private — never sent to the sender, never sent to a third party, never reported to Graph. The `[ui].screener_hint_dismissed` and `[ui].screener_last_seen_enabled` markers are bool flags in the user's `config.toml`, written via `config.WriteUIFlag` (atomic temp-file + rename, mode 0600). |
| Configuration | `~/Library/Application Support/inkwell/config.toml` | user-set | TOML; user-readable, no secrets. |
| Logs | `~/Library/Logs/inkwell/inkwell.log` | 0600 | Bodies, tokens, and PII are scrubbed by the log redaction layer (`internal/log/redact.go`). 8 redaction tests verify the contract. |
| Drafts in progress | `~/Library/Caches/inkwell/drafts/<uuid>.eml` | 0600 | Cleaned up after Graph confirms draft creation. Verified by `internal/compose/security_test.go::TestDraftTempfileMode`. |

On Linux, paths follow the XDG Base Directory spec
(`~/.config/inkwell/`, `~/.cache/inkwell/`, `~/.local/share/inkwell/`).

## How users delete their data

| Goal | Command |
| --- | --- |
| Remove tokens | `inkwell signout` (clears Keychain entries) |
| Remove the local cache | `inkwell purge` (planned — until then, see below) |
| Remove everything | `rm -rf ~/Library/Application\ Support/inkwell ~/Library/Caches/inkwell ~/Library/Logs/inkwell` (macOS) |

Revoking the OAuth grant from the Microsoft side (so future
inkwell installs cannot re-auth) is done in the user's Microsoft
account portal, not in inkwell.

## Third parties

The only third party that receives data from inkwell is **Microsoft
Corporation** (via the Microsoft Graph API), and only in the form
of OAuth-authorised API calls the user has consented to. Microsoft
Graph traffic is governed by Microsoft's own privacy policy
(<https://privacy.microsoft.com/>).

The unsubscribe POST flow (above) sends one HTTPS POST to the URL
the sender themselves placed in the `List-Unsubscribe` header. That
URL is sender-controlled; it's the same identifier the sender
already has for the user (the subscriber id baked into the URL).
The flow does not reveal anything new.

## Telemetry, opt-in or otherwise

There is no telemetry. There is no opt-in for telemetry. The
binary is offline-capable; if you firewall outbound HTTPS to
everything except `*.microsoft.com` / `*.microsoftonline.com`,
inkwell still works for read flows (any draft / unsubscribe flow
that needs the network surfaces a friendly error).

## Logs

Logs are written to `~/Library/Logs/inkwell/inkwell.log` (mode
0600). The log redaction layer scrubs:

- Bearer tokens (regex `Bearer [A-Za-z0-9._-]+`).
- Refresh tokens, MSAL cache blobs, sensitive header values.
- Message bodies, **always**.
- Email addresses → `<email-N>` placeholder, keyed per session.
- Subject lines outside DEBUG level.

Verified by `internal/log/redact_test.go` (8 tests).

If you share a log file with support / an issue reporter, it
should already be scrubbed. If it isn't, that's a privacy bug —
please report it via SECURITY.md.

## Contact

Pre-1.0: use [GitHub private security
advisories](https://github.com/eugenelim/inkwell/security/advisories/new)
for any privacy concern. v1.0 will publish a dedicated mailbox.

## Change log

- 2026-04-29 — first cut as part of v0.13.0 spec 17 partial ship.
