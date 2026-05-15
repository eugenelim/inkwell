# inkwell

**Triage your Microsoft 365 inbox at vim speed.** A terminal-native
mail client that reads, searches, filters, and bulk-acts on your work
mail without touching Outlook for the daily flows. Local-first.
Pure Go. Sign in with one command — no IT ticket, no Entra ID app
registration in your tenant.

```
☰ inkwell · you@example.com                       ✓ synced 14:32
┌────────────┬──────────────────────────────┬────────────────────────┐
│ ▌ Folders  │   Messages                   │   Message              │
│ ▾ Inbox    │ ▶ Tue 14:30  Alice  Quote    │ From:    Bob …         │
│   Sent     │   Tue 13:55  Bob    Re: deck │ Subject: Re: deck      │
│   Drafts   │ 📅 Tue 11:02  Bob  Accepted: │                        │
│   Archive  │   Tue 10:14  News   Weekly   │ Hey team, …            │
│ ☆ Saved…   │                              │                        │
│   ☆ News   │                              │                        │
└────────────┴──────────────────────────────┴────────────────────────┘
filter: ~f newsletter@* · matched 47 · ;d delete · ;a archive
j/k nav · ⏎ open · / search · :filter narrow · f flag · d delete · …
```

---

## Why inkwell

- **At keyboard speed.** Folder switch, message open, search, filter
  — all under 100ms against the local cache. No spinner, no network
  round-trip in the hot path.
- **Bulk cleanup that actually finishes.** Pattern-based filters
  (`:filter ~f newsletter@* & ~d <30d`) with one-keystroke bulk
  delete (`;d`). 247 newsletters in three seconds, not three minutes.
- **No IT ticket.** Signs in via Microsoft's first-party Graph CLI
  Tools client against `/common` — your tenant doesn't need to
  register an Entra ID app for you. `inkwell signin` opens the
  system browser, and you're in.
- **Local-first.** SQLite cache, FTS5 search, every read offline.
  Writes queue and replay on reconnect.
- **Privacy-respecting.** Tokens in Keychain only, never disk.
  Bodies and PII scrubbed from logs. Zero telemetry.
- **Drafts, not sends.** inkwell composes; Outlook sends. Hard scope
  boundary that keeps the auto-Reply-All disasters out of v1.

For the design decisions behind these, see
[`docs/user/explanation.md`](docs/user/explanation.md).

---

## Get started — 3 minutes

```sh
# Download (replace vX.Y.Z with the latest release).
gh release download v0.64.0 -p '*macos_arm64*' -D /tmp
tar -xzf /tmp/inkwell_0.60.0_macos_arm64.tar.gz -C /tmp
xattr -d com.apple.quarantine /tmp/inkwell        # macOS Gatekeeper
sudo mv /tmp/inkwell /usr/local/bin/              # optional

# Sign in (opens system browser).
inkwell signin

# Launch the TUI.
inkwell run
```

**On first launch**: type `1` to focus folders, `Enter` to open one,
`j`/`k` to walk messages, `Enter` to read, `q` to quit. Help bar at
the bottom of the screen always shows the keys for the focused pane.

Linux builds (amd64 / arm64) are also published on each release.

---

## Terminal compatibility

inkwell uses three terminal escape conventions. Beyond these,
inkwell makes no demands — pick whichever terminal you prefer that
meets the bar.

- **OSC 8** — clickable hyperlinks (Cmd-click on URLs). Only
  feature where the UX visibly degrades when missing. Fallback:
  the in-app URL picker (`o`) and yank (`y`) work without it.
- **OSC 52** — clipboard yank from `y` over SSH / tmux. macOS has
  a `pbcopy` fallback baked in; Linux without OSC 52 loses yank.
- **24-bit true color** — theme rendering. Without it, themes
  degrade to 256-color.

| Terminal                 | OSC 8 | OSC 52 | True color | Platforms                |
| ------------------------ | ----- | ------ | ---------- | ------------------------ |
| Ghostty                  | ✅    | ✅     | ✅         | macOS, Linux (beta)      |
| iTerm2                   | ✅    | ✅     | ✅         | macOS                    |
| WezTerm                  | ✅    | ✅     | ✅         | macOS, Linux, Windows    |
| Kitty                    | ✅    | ✅     | ✅         | macOS, Linux             |
| Alacritty                | ✅    | ✅     | ✅         | macOS, Linux, Windows    |
| Foot                     | ✅    | ✅     | ✅         | Linux (Wayland)          |
| GNOME Terminal           | ✅    | ✅     | ✅         | Linux (vte ≥ 0.50)       |
| Konsole                  | ✅    | ✅     | ✅         | Linux (KDE)              |
| Tilix                    | ✅    | ✅     | ✅         | Linux (vte ≥ 0.50)       |
| VS Code terminal         | ✅    | ✅     | ✅         | macOS, Linux, Windows    |
| **Apple Terminal.app**   | ❌    | ❌     | ❌         | macOS                    |

> **Apple Terminal.app warning.** macOS's bundled Terminal doesn't
> support any of the three. inkwell still runs — `o` opens the
> URL picker and `y` uses the `pbcopy` fallback — but Cmd-click on
> URLs does nothing, themes render flat, and yanking over SSH
> won't reach your clipboard. Switching terminals takes 30 seconds
> and your shell config carries over unchanged.

> **Linux note.** Linux builds aren't officially baked in yet
> (binaries publish on each release; the macOS code paths are the
> only ones smoke-tested), but the GNOME Terminal / Konsole / Tilix
> rows above are accurate for terminal-feature support when Linux
> support lands. The vte-based terminals (GNOME Terminal, Tilix)
> require vte ≥ 0.50 for OSC 8 — every distro shipping in the last
> ~5 years already has it. urxvt, st (unpatched), and pre-379 xterm
> are the Linux equivalents of Apple Terminal.app: same workflow
> degradation, same `o` / `y` fallbacks.

---

## Documentation

Start here, in this order:

1. **[Tutorial](docs/user/tutorial.md)** — your first 30 minutes.
   Sequential walkthrough: install → sign in → navigate → search →
   filter → bulk delete → saved searches → calendar.
2. **[How-to](docs/user/how-to.md)** — task recipes ("delete all
   newsletters older than 30 days", "set up saved searches", "force
   a sync now").
3. **[Reference](docs/user/reference.md)** — every keybinding, every
   `:command`, every pattern operator. Bookmark this.
4. **[Explanation](docs/user/explanation.md)** — design decisions,
   privacy stance, why-it-works-this-way.

For contributing or hacking on the codebase, jump to
[Contributor docs](#contributor-docs) at the bottom.

---

## Status

**Pre-1.0.** Tagged releases ship continuously as specs land. The
current major surfaces:

| Capability                                                                  | Status     |
| --------------------------------------------------------------------------- | ---------- |
| Sign-in (Microsoft Graph CLI Tools client, multi-tenant)                    | ✅ v0.1+  |
| Local SQLite cache, FTS5, body LRU eviction                                 | ✅ v0.2+  |
| Sync engine (folders + per-folder delta + lazy backfill)                    | ✅ v0.2+  |
| Three-pane TUI with cursor / focus markers, theming                         | ✅ v0.2+  |
| HTML → text rendering, scrollable viewer                                    | ✅ v0.2+  |
| Triage: read/unread, flag, soft-delete, archive, unsubscribe (`U`)          | ✅ v0.3+  |
| Local FTS search (`/`)                                                      | ✅ v0.4+  |
| Pattern language (`~f`, `~d <30d`, etc.) + bulk filter (`;d`)               | ✅ v0.6+  |
| Saved searches as virtual folders                                           | ✅ v0.7+  |
| Calendar (read-only, `:cal`)                                                | ✅ v0.8+  |
| Mailbox settings (out-of-office, `:ooo`)                                    | ✅ v0.27+ |
| Compose / reply (drafts only)                                               | ✅ v0.30+ |
| CLI mode (non-interactive: `inkwell messages`, `inkwell search`, etc.)      | ✅ v0.31+ |
| Security hardening (CASA evidence, gosec / Semgrep / govulncheck CI gates)  | ✅ v0.39+ |
| Folder management (TUI sidebar: `N` new · `R` rename · `X` delete)         | ✅ v0.46+ |
| Mute thread (`M`), thread chord (`T`-chord), cross-folder bulk              | ✅ v0.47+ |
| Command palette (`Ctrl+K`)                                                  | ✅ v0.50+ |
| Routing destinations — Imbox / Feed / Paper Trail / Screener (`S`-chord)   | ✅ v0.51+ |
| Split inbox tabs (`[` / `]`)                                                | ✅ v0.52+ |
| Reply Later (`L`) / Set Aside (`P`) stacks                                 | ✅ v0.53+ |
| Bundle senders (collapse same-sender runs in list — `B` toggle)             | ✅ v0.54+ |
| Custom actions framework (chain ops via `actions.toml` + `inkwell action`)  | ✅ v0.56+ |
| Screener for new senders (HEY-style first-contact gate, opt-in)             | ✅ v0.57+ |
| Watch mode (CLI tail — `inkwell messages --filter X --watch`)               | ✅ v0.58+ |
| "Done" alias for archive (`e`, `:done`, `[ui].archive_label = "done"`)      | ✅ v0.59+ |
| Focused / Other tab (read-only Inbox sub-strip, `:focused` / `:other`)      | ✅ v0.60+ |
| Server-side rules (`inkwell rules pull/apply`, `~/.config/inkwell/rules.toml`) | ✅ v0.61+ |
| Markdown drafts (`[compose] body_format = "markdown"`, goldmark + GFM)        | ✅ v0.62+ |
| Calendar invites in mail (card in viewer, `o` opens event in OWA)             | ✅ v0.63+ |
| Opt-in local body index + regex search (`~b /regex/`, `inkwell index`)        | ✅ v0.64+ |
| Code-signing + notarization                                                 | 🚧 v1.0   |

Reading the binary's full feature list at any version: see
[`docs/user/reference.md`](docs/user/reference.md). Roadmap beyond
v1 is in [`docs/product/roadmap.md`](docs/product/roadmap.md).

---

## Privacy stance

- **No telemetry.** Zero outbound calls except Microsoft Graph and
  Entra ID for sign-in.
- **Tokens stay in Keychain.** Never on disk in plaintext, never in
  logs, never in env vars.
- **Mail content never leaves `~/`.** SQLite cache lives at
  `~/Library/Application Support/inkwell/inkwell.db` (mode 0600).
- **Logs scrub aggressively.** Bearer tokens, refresh tokens, message
  bodies, email addresses (yours excepted) — all stripped before
  the slog handler writes anything.
- **No `Mail.Send`.** inkwell can never send email. Drafts only;
  finalise in Outlook. This is a CI-enforced scope boundary.

---

## Install

### macOS — pre-built binary (recommended)

```sh
gh release download <vX.Y.Z> -p '*macos_arm64*' -D /tmp   # or *macos_amd64*
tar -xzf /tmp/inkwell_<vX.Y.Z>_macos_arm64.tar.gz -C /tmp
xattr -d com.apple.quarantine /tmp/inkwell
sudo mv /tmp/inkwell /usr/local/bin/
```

Code-signing + notarization land in v1.0; until then the
`xattr` step is required on macOS.

### Linux

Same pattern, swap the asset:
`*linux_amd64*` or `*linux_arm64*`. No quarantine step needed.

### From source

Go 1.23+ required.

```sh
git clone https://github.com/eugenelim/inkwell.git
cd inkwell
make build
./bin/inkwell run
```

---

## First-time sign-in

```sh
inkwell signin
```

Opens your system browser. Sign in with your work account; the
browser closes itself when sign-in succeeds. Token cache lives
encrypted in macOS Keychain and refreshes silently for ~90 days.

**Behind the scenes**: inkwell uses the multi-tenant Microsoft
Graph Command Line Tools first-party public client
(`14d82eec-204b-4c2f-b7e8-296a70dab67e`) against the `/common`
authority. Your tenant does not need to register an Entra ID app
for you. On a managed Mac, the Microsoft Enterprise SSO plug-in
satisfies Conditional Access policies that require a compliant
device — this is the only flow that works on deeply-managed
enterprise tenants.

If sign-in fails with `AADSTS50105` or similar, your tenant has
specifically blocked the public-client flow. See
[How-to → "When sign-in fails"](docs/user/how-to.md#when-sign-in-fails).

For headless / SSH sessions: `inkwell signin --device-code`.
Tenants that require a managed device will reject device-code,
so this is only useful where Conditional Access is permissive.

---

## Contributor docs

Jumping into the codebase? Read these in order:

- [`AGENTS.md`](AGENTS.md) — agent / contributor entry point; source-
  of-truth table; workflow summary; quick-reference commands.
  (`CLAUDE.md` is a symlink to this file.)
- [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md) — long-form repo
  conventions with stable `§N` anchors: stack invariants, layering,
  test architecture, perf budgets, privacy, the ralph loop,
  definition-of-done, common review findings.
- [`docs/PRD.md`](docs/PRD.md) — product scope, granted vs denied
  Graph permissions, success criteria.
- [`docs/ARCH.md`](docs/ARCH.md) — system architecture, layering,
  data flow.
- [`docs/CONFIG.md`](docs/CONFIG.md) — every config key.
- [`docs/TESTING.md`](docs/TESTING.md) — test conventions, fuzz,
  goleak, teatest patterns, the regression suite.
- [`docs/specs/`](docs/specs/) — per-feature directories in
  implementation order. Each `NN-<title>/` contains a `spec.md`
  (contract — what "done" means) and a `plan.md` (tracking note —
  DoD checklist + iteration log).
- [`docs/product/roadmap.md`](docs/product/roadmap.md) — post-v1 backlog, ranked by
  impact.

`make regress` is the gate before any release tag.

---

## License

MIT — see [`LICENSE`](LICENSE).

## Project name

`inkwell` is a working name. It is set in `go.mod` and the binary
`cmd/inkwell` package; rename before any public stable release.
