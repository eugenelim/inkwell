# inkwell

> Pre-release. Foundational specs 01–05 land; the TUI boots, lists folders
> and messages from the local cache, and renders bodies. Authentication
> uses the well-known Microsoft Graph CLI Tools first-party public client
> against `/common`, so **no Entra ID app registration is required** in
> your tenant — just `inkwell signin`. Conditional Access still applies.

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

Inkwell uses the well-known Microsoft Graph Command Line Tools first-party
public client (`14d82eec-204b-4c2f-b7e8-296a70dab67e`) against the
multi-tenant `/common` authority. **No tenant admin onboarding is
required.** Just sign in:

```sh
inkwell signin       # device code flow — open the URL, paste the code
inkwell whoami       # confirm UPN + resolved home tenant
inkwell              # launch the TUI
```

A config file is **optional**. If you want a multi-account guardrail you
can pin the expected UPN in `~/.config/inkwell/config.toml`:

```toml
[account]
upn = "you@example.com"
```

If your tenant blocks the Microsoft Graph CLI Tools app under
Conditional Access — or disables user-consent for Microsoft-published
apps — sign-in fails with an `AADSTS` error and the relevant policy
class. Recovery is your IT admin's call; see
`docs/specs/01-auth-device-code.md` §11.

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
