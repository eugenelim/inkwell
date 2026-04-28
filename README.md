# inkwell

> Pre-release. Foundational specs 01–05 land; the TUI boots, lists folders
> and messages from the local cache, and renders bodies. Authentication and
> sync against a real Microsoft 365 tenant require operator-side setup — see
> `docs/specs/01-auth-device-code.md` §3.

A terminal-native mail and calendar client for Microsoft 365 on macOS.
Read, search, and triage your inbox at the speed of thought. Vim-style
keybindings. Pattern-based bulk cleanup. Local-first. Pure Go.

## What works in this release (v0.1.0)

- TUI shell with three panes (folders / list / viewer), modal command bar,
  status line, sign-in modal, confirm modal.
- OAuth 2.0 device code authentication via MSAL Go; tokens stored in macOS
  Keychain only, never on disk.
- Local SQLite cache with WAL mode, FTS5 over subjects + previews,
  body LRU eviction.
- Microsoft Graph v1.0 client with auth-refresh-on-401, throttle / Retry-After,
  semaphore-capped concurrency.
- Per-folder delta sync with cursor persistence and `syncStateNotFound`
  recovery.
- HTML→text body conversion with tracking-pixel removal, numbered link
  extraction, attachment listing.

See `docs/plans/spec-{01..05}.md` for what's done vs deferred per spec.

## What's missing (next iterations)

- Triage actions wired to keybindings (spec 07).
- Search UX (spec 06).
- Pattern language + bulk operations (specs 08–10).
- macOS code-signing + notarization (PRD §7) — current binaries are unsigned.
- Roadmap items in `docs/ROADMAP.md`.

## Install

### macOS

Pre-built binaries are unsigned in v0.1.x. After download, run:

```sh
xattr -d com.apple.quarantine ./inkwell
chmod +x ./inkwell
./inkwell --version
```

Alternatively, right-click the binary in Finder → Open → Open Anyway.

### From source

Requires Go 1.23+:

```sh
git clone https://github.com/eugenelim/inkwell.git
cd inkwell
make build
./bin/inkwell --version
```

## First-time setup

Inkwell talks to Microsoft Graph v1.0 via an Entra ID app registration. The
tenant admin must create the registration before you can sign in. See
`docs/specs/01-auth-device-code.md` §3 for the full prerequisites; the short
version:

1. Public-client app registration with "Allow public client flows" enabled.
2. Delegated Microsoft Graph permissions per `docs/PRD.md` §3.1.
3. Admin consent granted for the tenant.
4. Write `client_id`, `tenant_id`, and your UPN into
   `~/.config/inkwell/config.toml`:

```toml
[account]
tenant_id = "<your-tenant-guid>"
client_id = "<your-app-client-id>"
upn       = "you@example.com"
```

Then:

```sh
inkwell signin       # device code flow
inkwell whoami       # confirm
inkwell              # launch the TUI
```

## Documentation

- `docs/PRD.md` — product scope, granted vs denied Graph permissions.
- `docs/ARCH.md` — system architecture, layering, data flow.
- `docs/CONFIG.md` — every config key.
- `docs/specs/` — feature specs in implementation order.
- `docs/ROADMAP.md` — post-v1 backlog, ranked by impact.
- `CLAUDE.md` — contributor guide, test architecture, performance &
  privacy rules.

## Privacy stance

- No telemetry. Zero outbound calls except Microsoft Graph and Entra ID.
- Tokens live in Keychain only.
- Mail content never leaves `~/Library/Application Support/inkwell/`.
- Logs scrub bearer tokens, JWTs, message bodies, and tokenise email
  addresses (your own UPN excepted) before they touch disk.

## License

MIT — see `LICENSE`.

## Project name

`inkwell` is a working name. It is set in `go.mod` and the binary `cmd/inkwell`
package; rename before any public stable release.
