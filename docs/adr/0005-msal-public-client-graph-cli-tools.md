# ADR 0005: Authenticate via MSAL public client "Microsoft Graph CLI Tools"

- **Status:** Accepted (2026-05-13)
- **Deciders:** eugenelim
- **Supersedes:** —
- **Related:** spec 01 (auth, device code), ARCH §1, PRD §3.1

## Context

Inkwell ships as an OSS macOS application that any Microsoft 365 user
should be able to install and sign into. There is no organizational
sponsor with an Entra ID tenant where we can register a "first-party"
Azure AD application on the user's behalf.

The three realistic paths are:

1. **Each user registers their own Azure AD app**, copies the
   client_id into inkwell's config, and grants it the scopes inkwell
   needs. Workable for power users; impossible friction for the
   typical user inkwell targets.
2. **Maintain a single Azure AD app in a maintainer-owned tenant**,
   bake its client_id into the binary, and route every user's auth
   through that tenant. Works, but binds the user base to the
   maintainer's tenant for consent + delegated permissions. If the
   maintainer's tenant gets disabled, every install breaks. Also
   creates compliance ambiguity ("whose app is calling Graph on my
   behalf?") that doesn't pass a corporate IT review.
3. **Use Microsoft's own public client ID** — the same one used by
   the Microsoft Graph CLI Tools and `m365` CLI. Microsoft maintains
   this app registration; it's trusted by every Entra tenant by
   default; users see a familiar "Microsoft Graph CLI Tools" name on
   the consent screen.

Option 3 is exactly the pattern Microsoft documents for non-tenant-
specific developer tooling.

## Decision

Inkwell uses MSAL Go's public-client device-code flow against:

- **Authority:** `https://login.microsoftonline.com/common` (tenant
  inferred at sign-in from the user's UPN).
- **client_id:** `14d82eec-204b-4c2f-b7e8-296a70dab67e` — Microsoft's
  "Microsoft Graph CLI Tools" public client.
- **Scopes:** as listed in PRD §3.1 (delegated, user-consentable).

The client_id is a compile-time constant in `internal/auth` and not
configurable. Tokens are stored in macOS Keychain only (never on
disk, never in env). Refresh is handled by MSAL Go's silent-acquire.

## Consequences

### Positive
- **Zero setup.** A new user runs `inkwell signin`, sees Microsoft's
  device-code page, approves the same "Microsoft Graph CLI Tools"
  consent screen they may already have approved for other tools, and
  is in. No app registration paperwork; no tenant-admin escalation
  for the maintainer.
- **No maintainer-tenant lock-in.** Inkwell's continued operation
  doesn't depend on a private Azure AD tenant the maintainer pays for
  and keeps healthy.
- **Familiar consent screen.** Tenant admins reviewing consent
  audit-logs see a known Microsoft-owned app, not an unknown
  third-party.
- **Compliance posture.** "What app called Graph on my behalf?" has a
  Microsoft answer, not an indie-developer answer.

### Negative
- **Inkwell can't grant itself any scope.** We're constrained to
  scopes the Microsoft Graph CLI Tools app is configured to permit
  delegated consent for. If Microsoft tightens that list, inkwell
  would have to react. (Today the scopes in PRD §3.1 are all in the
  permitted set; we monitor this on every release.)
- **Tenant admins may policy-block the Microsoft Graph CLI Tools
  app**, in which case inkwell can't sign in. Workaround: the admin
  whitelists the app; we don't offer a fallback path.
- **Some auditors may consider it inelegant** that inkwell
  identifies as "Microsoft Graph CLI Tools" in tenant audit logs.
  Acceptable; it's accurate.

### Neutral
- Branding cost: the consent screen says Microsoft's name, not
  inkwell's. Fine — the privacy posture (no telemetry, all data
  local) means we don't *need* a recognizable consent identity.

## Alternatives considered

**Maintainer-owned multi-tenant Azure AD app.** Rejected for the
lock-in, audit, and tenant-policy-friction reasons in Context.

**Per-user Entra app registration.** Rejected as installation
friction. Inkwell is supposed to be a TUI you launch and use, not a
tenant-admin onboarding exercise.

**Native macOS Outlook OAuth flow.** Doesn't exist as a public API
outside Microsoft's own apps.

**Browser-based auth-code flow with localhost callback.** Considered.
Rejected because the device-code flow is simpler in a TUI (no
browser handoff, no port conflict, no callback server in our
process), and works on machines without a graphical browser.

## References

- spec 01 — auth-device-code.
- PRD.md §3.1 — granted Graph scopes.
- [Microsoft 365 CLI: app registration](https://pnp.github.io/cli-microsoft365/user-guide/using-own-azure-ad-identity) — describes the public-client pattern.
