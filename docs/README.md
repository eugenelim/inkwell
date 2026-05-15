# Inkwell — Documentation

This is the documentation bundle for **inkwell**, a terminal-based mail and calendar client for Microsoft 365 on macOS.

## Reading order

For new readers (humans or LLMs), read in this order:

1. **`PRD.md`** — what we're building, why, and the hard scope boundaries (granted vs denied Graph permissions).
2. **`ARCH.md`** — system architecture, module layout, data flow, cross-cutting concerns.
3. **`CONFIG.md`** — canonical reference for every config key. Skim on first read; reference on demand later.
4. **`specs/`** — one file per feature, numbered in implementation order.
5. **`ROADMAP.md`** — post-v1 ideas, ranked by impact. Read after v1 is shipping or shipped; not required for v1 implementation.

The PRD, ARCH, and CONFIG documents are foundational; specs assume the reader has read them.

## Spec index

| #  | File                                  | Topic                                    | Effort |
| -- | ------------------------------------- | ---------------------------------------- | ------ |
| 01 | `specs/01-auth-device-code/spec.md`        | OAuth device code flow + Keychain        | 1–2d   |
| 02 | `specs/02-local-cache-schema/spec.md`      | SQLite schema, FTS5, migrations          | 2d     |
| 03 | `specs/03-sync-engine/spec.md`             | Sync engine, delta query, throttling     | 3–4d   |
| 04 | `specs/04-tui-shell/spec.md`               | Bubble Tea panes, keymap, modes          | 3d     |
| 05 | `specs/05-message-rendering/spec.md`       | HTML→text, headers, attachments, links   | 2d     |
| 06 | `specs/06-search-hybrid/spec.md`           | Local FTS + Graph $search, streaming     | 2d     |
| 07 | `specs/07-triage-actions-single/spec.md`   | Single-message actions, optimistic UI    | 2d     |
| 08 | `specs/08-pattern-language/spec.md`        | Mutt-style pattern parser + compiler     | 2–3d   |
| 09 | `specs/09-batch-engine/spec.md`            | Graph $batch executor, retry, undo       | 2d     |
| 10 | `specs/10-bulk-operations/spec.md`         | Filter UX, ; prefix, preview, progress   | 2–3d   |
| 11 | `specs/11-saved-searches/spec.md`          | Virtual folders in sidebar               | 1–2d   |
| 12 | `specs/12-calendar-readonly/spec.md`       | Calendar pane (read-only)                | 1–2d   |
| 13 | `specs/13-mailbox-settings/spec.md`        | Out-of-office toggle, settings view      | 1d     |
| 14 | `specs/14-cli-mode/spec.md`                | Non-interactive subcommands              | 1–2d   |

**Implementation order:** specs 01–04 are foundational and must land first. Specs 08–10 are tightly coupled (the bulk-triage trio) and should land together. The rest are largely parallelizable.

## Spec template

Each spec follows the same structure:

- **Status / Depends on / Blocks / Estimated effort** header
- **§1 Goal** — one paragraph
- **§2 Module layout** — files and directories
- **§3+ Design** — types, flows, algorithms
- **§n Configuration** — config keys this spec owns (referencing CONFIG.md)
- **§n Performance budgets** — testable latency and throughput targets
- **§n Failure modes** — table of scenario → behavior
- **§n Test plan** — unit, integration, manual
- **§n Definition of done** — checklist
- **§n Out of scope** — what we deliberately don't do

## Conventions

- **Go 1.23+, Bubble Tea, modernc/sqlite, MSAL Go.** Stack locked in ARCH §1.
- **Microsoft Graph v1.0 only.** No Outlook REST v2.0, no beta, no EWS, no IMAP/SMTP. ARCH §0 explains why.
- **Pure-Go dependencies preferred.** No CGO unless absolutely necessary (notarization friction on macOS).
- **Optimistic UI everywhere.** Every write applies locally first, dispatches to Graph asynchronously, reconciles on response. ARCH §3.
- **Idempotent actions.** All mutations are designed to be safely re-runnable; 404 on delete = success. Spec 07 §6.
- **Strict layering.** ARCH §2 lists the dependency rules; specs reference them.

## Adding a new feature

1. Read PRD §3.1 to confirm the Graph permissions exist.
2. Write a new spec following the template.
3. Add config keys (if any) to `CONFIG.md` in the SAME change.
4. Update `PRD.md`'s spec inventory in §10.
5. Update `ARCH.md` module layout if introducing a new package.

## Adding a new config key

See `CONFIG.md` § "Adding a new config key".

## Project name

`inkwell` is a placeholder. Find-and-replace before public release.
