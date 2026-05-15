# CHARTER.md — Inkwell

The one-page mission, scope, and principles. Stable for years; rarely
changed. Detailed product surface lives in [`docs/PRD.md`](PRD.md);
detailed conventions in [`docs/CONVENTIONS.md`](CONVENTIONS.md).

## Mission

Build a terminal-based, local-first email + calendar client for macOS
that lets a senior enterprise professional triage Microsoft 365 mail
**faster than native Outlook for Mac**, without requiring custom
tenant-side app registration or any compromise on privacy.

## In scope

- **Read.** Mailbox / calendar / mailbox-settings via the granted Microsoft
  Graph scopes (PRD §3.1).
- **Triage.** Mark read, flag, move, archive, delete, categorise, mute, set
  aside, reply-later, screener — keyboard-driven and bulkable.
- **Search.** Hybrid local + server search; pattern language with optional
  body regex against a local index.
- **Compose drafts.** Save drafts on Graph; user finalises send in native
  Outlook.
- **Local-only.** No telemetry, no analytics, no auto-updater. Everything
  the app needs lives under `~/Library/Application Support/inkwell/` or
  the macOS Keychain.

## Out of scope (deliberate, not deferred)

- **Sending mail.** `Mail.Send` is a denied scope (PRD §3.2). Drafts only.
- **Modifying calendar.** `Calendars.ReadWrite` is denied. Inline invite
  accept/decline routes through Outlook on the web.
- **Shared mailboxes, contacts, Teams write, OneDrive, OneNote, Planner /
  To Do.** All denied scopes (PRD §3.2).
- **Custom Entra ID app registration.** Inkwell uses the well-known
  Microsoft Graph CLI Tools first-party public client; no admin gate.
- **Replacing native Outlook.** The user keeps Outlook for composition and
  meeting management; inkwell owns the read / search / organise / bulk-
  cleanup loop.
- **Tier-3 agentic AI.** Roadmap §"AI" ships tier-1 (local-only) and
  opt-in tier-2 (data leaves the box); fully agentic is not on the path.

## Principles

When two reasonable approaches conflict, these resolve ties:

1. **Local first.** Cache to SQLite at mode `0600`; tokens to Keychain;
   no body / token / PII leaves `~`. Logging is structured + redacted by
   default. Five hard invariants in [`docs/CONVENTIONS.md`](CONVENTIONS.md) §7.
2. **Keyboard first.** Every triage action is a keystroke or a
   `:command`. Mouse is optional. CLI parity for triage-shaped verbs
   (PRD §5.12).
3. **Read-and-triage focused.** Not a full mail client. Out-of-scope
   requests stay out-of-scope; if a feature needs a denied scope, the
   feature is out-of-scope.
4. **Spec → plan → test → ship.** Every non-trivial change starts at
   `docs/specs/NN-<title>/spec.md` with a co-located `plan.md`. The
   four-layer test pyramid (unit + integration + TUI e2e visible-delta
   + benchmark) is mandatory (`docs/CONVENTIONS.md` §5).
5. **Pure-Go stack.** No CGO; pure-Go SQLite + pure-Go MSAL; pure-Go
   HTML→text. CGO breaks macOS notarization and cross-compilation
   (`docs/CONVENTIONS.md` §1).
6. **Privacy by construction, not by policy.** Defaults err toward
   *not* persisting / *not* logging / *not* phoning home. Opt-in for
   anything that widens the surface (spec 35 body index is the
   precedent: default off, capped, purgeable).
7. **Institutional memory in code.** Every bug we ship gets a
   same-commit regression test (`docs/CONVENTIONS.md` §5.7). Every
   review pattern that recurs goes into the §16 ledger. The next
   contributor (or the assistant in a future session) inherits the
   lessons, not just the diff.

## Where to read next

- **What's in scope vs out, in detail:** [`docs/PRD.md`](PRD.md).
- **How the code is organised:** [`docs/ARCH.md`](ARCH.md).
- **How we work day to day:** [`docs/CONVENTIONS.md`](CONVENTIONS.md).
- **What's shipped vs planned:** [`docs/product/roadmap.md`](product/roadmap.md).
- **Why we made the cross-cutting decisions we did:** [`docs/adr/`](adr/).
