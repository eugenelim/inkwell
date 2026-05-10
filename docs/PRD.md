# Product Requirements Document

**Project codename:** `inkwell` (placeholder — rename in repo)
**Document status:** Foundational. Read before any feature spec.
**Last updated:** 2026-04-27

---

## 1. Product summary

A terminal-based, local-first email and calendar client for macOS that talks to Microsoft 365 (Exchange Online) via Microsoft Graph. Built for senior enterprise professionals who live in the terminal and want a faster, keyboard-driven triage loop than the native Outlook for Mac client.

The product is **read-and-triage focused**. It is not a full Outlook replacement. Sending mail and modifying calendar events are deliberately out of scope due to tenant scope restrictions (see §4). The user keeps native Outlook for composition and meeting management; this client owns the read, search, organize, and bulk-cleanup loop.

## 2. Target user

A senior professional at a large enterprise (deeply-managed enterprise IT posture is the design baseline). Characteristics assumed:

- Comfortable in the terminal; uses tmux, vim, or similar daily.
- Manages 100+ emails per day across multiple client engagements and internal threads.
- Has a deep mailbox (50k–500k messages, multi-year archive).
- Cannot install software requiring custom Azure AD app registration through self-service. App registration must go through formal enterprise architecture review.
- Subject to Conditional Access policies requiring device compliance via Intune (or equivalent MDM) and/or SSO via the Microsoft Enterprise SSO plug-in for Apple devices.
- Already runs native Outlook for Mac for composition; this client is a complementary triage surface, not a replacement.

## 3. Scope of Microsoft Graph permissions we request

This product is designed against the following delegated Microsoft Graph scopes. We request them at sign-in via the well-known Microsoft Graph Command Line Tools first-party public client (see §4); the user consents the first time. **Feature specs MUST NOT assume any scope outside §3.1.** If a feature would require a denied scope, it is out of scope.

### 3.1 Requested at sign-in (in-scope)

| Scope                       | Capability                                                              |
| --------------------------- | ----------------------------------------------------------------------- |
| `Mail.Read`                 | Read mailbox, fetch messages, attachments, headers, bodies              |
| `Mail.ReadBasic`            | Subset of Mail.Read (no body/attachments) — used for fast list views    |
| `Mail.ReadWrite`            | Modify messages: categorize, flag, mark read, move, draft create/edit, delete |
| `MailboxSettings.Read`      | Read out-of-office, working hours, time zone, locale                    |
| `MailboxSettings.ReadWrite` | Set out-of-office, working hours, automatic-reply state                 |
| `Calendars.Read`            | Read calendar events                                                    |
| `Chat.Read`                 | Read user's Teams chats and chat messages (deferred — see §6)           |
| `User.Read`                 | Read signed-in user profile                                             |
| `User.ReadBasic.All`        | Read basic profile of any directory user                                |
| `Presence.Read.All`         | Read any user's Teams presence                                          |

The Microsoft Graph CLI Tools client is a first-party Microsoft-published app pre-trusted across most Microsoft 365 tenants. Pre-consent for it generally exists at the tenant level; users can consent for themselves on first sign-in unless their admin has disabled user-consent for Microsoft-published apps. See §4 and spec 01 §11 for failure-mode handling.

### 3.2 Denied (hard out-of-scope)

The following capabilities are **out of scope** even though the public client we use may technically support them. We deliberately do not request these scopes; lint guards in CI fail any code that tries.

| Denied scope                  | Capability the product CANNOT have                                |
| ----------------------------- | ----------------------------------------------------------------- |
| `Mail.Send`                   | Cannot send email programmatically. Drafts are saved; user finalizes send in native Outlook. |
| `Mail.*.Shared`               | No access to shared mailboxes                                     |
| `Calendars.ReadWrite`         | Cannot create, modify, accept, decline, or delete calendar events |
| `Contacts.Read` / `Read.Write`| Cannot access Outlook personal contacts                           |
| `People.Read`                 | Cannot use Microsoft's people-graph relevance ranking             |
| `Tasks.*`                     | No Planner / To Do integration                                    |
| `Notes.*`                     | No OneNote integration                                            |
| `Files.*`                     | No OneDrive integration                                           |
| `ChatMessage.Send`            | Cannot post to Teams                                              |
| `Channel.ReadBasic.All`       | Cannot enumerate Teams channels                                   |

## 4. Authentication and tenant constraints

Inkwell deliberately does **not** require an Entra ID app registration in the user's tenant. The whole onboarding gate of "go through enterprise architecture review to register a public client" is precisely the friction the product is designed to avoid for a self-service triage tool. Instead:

- **Client identity:** the well-known Microsoft Graph Command Line Tools public client (`client_id = 14d82eec-204b-4c2f-b7e8-296a70dab67e`). This is a first-party Microsoft-owned app registration, available across all Entra ID tenants. The `client_id` is hard-coded as a constant in `internal/auth/scopes.go`. It is not user-configurable; changing it requires a code review.
- **Authority:** the multi-tenant common authority `https://login.microsoftonline.com/common`. The user's actual home tenant is **inferred** from the MSAL `AuthResult` after sign-in (the `Account.Realm` field is the home tenant ID; `Account.PreferredUsername` is the UPN). These values are persisted to the local `accounts` row after first sign-in.
- **Auth flow:** OAuth 2.0 **interactive system-browser flow** (auth code + PKCE) via MSAL Go (`microsoft-authentication-library-for-go`). MSAL spawns a localhost listener on a free port, opens the system default browser via `open(1)`, and waits for the auth code. Public client, no client secret. **Device code flow is a fallback** for headless scenarios (SSH sessions, no display) — see spec 01 §5.0 for mode selection.
- **Conditional Access requires a compliant device.** Authentication flows that cannot carry the device-compliance signal (notably device-code flow) will be rejected by the tenant. The interactive system browser, with the operating system's enterprise SSO integration (Microsoft Enterprise SSO plug-in for Apple Devices), is the only viable interactive path — the plug-in transparently injects device-attestation cookies into the auth session so AAD sees the device IS managed and lets the sign-in proceed. Inkwell inherits this posture; it does not implement compliance signalling itself. CA failures surface as user-readable AADSTS errors at sign-in.
- **Token storage:** macOS Keychain via `github.com/zalando/go-keyring`. Never written to disk in plaintext. The MSAL cache blob (which contains refresh + ID tokens) is the only secret; it goes to Keychain only.
- **Token refresh:** refresh tokens persist ~90 days for work accounts in this flow. The app MUST gracefully re-prompt the user for device code re-auth when refresh fails, without losing local cache state.
- **Tenants that block the public client:** a tenant admin can in principle disable user-consent for Microsoft-published apps or block the Microsoft Graph CLI Tools app under Conditional Access. In that case sign-in fails with a specific AADSTS error. Spec 01 §11 details the user-facing message and recovery steps.
- **Multi-account:** v1 supports a single Microsoft 365 account per profile. Multi-account is deferred; the data model already supports it via the `accounts` table.

The `[account]` config section is therefore **optional** — the only field the user might still want to set is `account.upn` to pin the expected UPN as a safety check. With no config file at all, `inkwell signin` works on a clean install.

## 5. Functional capabilities (in-scope for v1)

Capabilities are listed in priority order. Each maps to one or more feature specs (see §10).

### 5.1 Authentication & onboarding

- Device code flow sign-in
- Token cache in Keychain
- Re-auth prompt when refresh fails
- Sign-out / clear local state

### 5.2 Local mail cache

- SQLite-backed local store with WAL mode
- Per-folder delta token persistence
- Message envelopes always cached; bodies cached on access (LRU eviction)
- FTS5 full-text index over cached subject + bodyPreview
- Attachment metadata cached; attachment bytes lazy-loaded on demand

### 5.3 Sync engine

- Initial backfill: bounded to last 90 days at first launch; older mail backfills in background
- Incremental sync via Graph delta query (per-folder)
- Foregrounded polling cadence: 30 seconds default, configurable
- Backgrounded polling: 5 minutes
- Resume from saved delta token; full re-init only on `syncStateNotFound`
- Bounded concurrency (3–4 in-flight Graph calls)
- 429 / Retry-After honoring with exponential backoff fallback

### 5.4 TUI shell

- Bubble Tea-based main loop
- Three primary panes: folder sidebar, message list, message viewer
- Vim-style keybindings throughout
- Tabs for parallel folder/search views
- Command-line input (`:command` style)
- Status line showing sync state, account, counts

### 5.5 Reading

- Render plain-text bodies natively
- HTML bodies: convert via `html2text`-equivalent; fall back to `:open` in default browser
- Inline attachment list with `:save` and `:open`
- Conversation grouping via `conversationId`

### 5.6 Search

- Local FTS5 search for cached content (sub-100ms)
- Server-side `$search` for non-cached content
- Hybrid mode: local fires immediately, server runs in parallel, results merge as they arrive
- Saved searches surfaced as virtual folders

### 5.7 Triage actions (single-message)

- Mark read / unread (`r` / `R`)
- Flag / unflag (`f` / `F`)
- Move to folder (`m <folder>`)
- Save (alias of move) (`s <folder>`)
- Soft delete to Deleted Items (`d`)
- Permanent delete (`D`, gated behind confirmation)
- Archive (`a`, well-known folder)
- Add / remove category (`c <name>` / `C <name>`)

(Draft creation — reply / reply-all / forward / new — moves to its own capability §5.13 because the editor and templating surface is substantial.)

### 5.8 Bulk pattern operations

- Mutt-style pattern language: `~f ~t ~s ~b ~d ~A ~N ~F ~c` with boolean composition
- `:filter <pattern>` narrows visible list to matches
- `:search <pattern>` highlights matches without narrowing
- `;<action>` (semicolon prefix) applies action to all filtered messages
- Mandatory dry-run preview showing match count and sample
- `$batch` engine: 20-per-batch chunking, bounded parallelism, 429 retry
- Single-level undo stack for moves and soft deletes

### 5.9 Saved searches as virtual folders

- Define a saved search by name + pattern in config
- Surface in folder sidebar
- Re-evaluate on each delta sync

### 5.10 Calendar (read-only sidebar)

- Today's events in a dismissable sidebar pane
- Week-view command (`:cal`)
- Click event → render details inline (no actions available)

### 5.11 Mailbox settings

- View and toggle automatic replies (out-of-office) from TUI
- Display configured working hours / time zone in status line

### 5.12 CLI mode (non-interactive)

- All operations available as `inkwell <subcommand>` for scripting
- JSON output mode for piping to `jq`
- Dry-run flags on every destructive operation

### 5.13 Compose / Reply (drafts only)

- Reply (`r`), reply-all (`R`), forward (`f`) on the focused message
- New blank message (`m` from outside the list pane, or `:compose`)
- Body authored in the user's editor (`$INKWELL_EDITOR` → `$EDITOR` → `nano`) via `tea.ExecProcess`
- Reply / forward skeletons pre-populated: To/Cc/Subject + standard quote chain (line-prefixed `> `) for replies, "Forwarded message" header block for forwards
- Confirm pane after the editor exits: `s` save and open in Outlook, `e` re-edit, `d` discard
- Draft round-trips to the server's Drafts folder via `POST /me/messages/{id}/createReply` (et al.) + `PATCH /me/messages/{id}` for body. Plain-text body in v1; rich HTML deferred (§6).
- After save, `webLink` is exposed in the status bar; `s` runs `open <webLink>` and the user finalises send in native Outlook. **`Mail.Send` remains denied (PRD §3.2)**.
- Crash-recovery: tempfile and source-message ref persisted in `compose_sessions`; on next launch the user is offered "resume draft?".
- Discard: deletes both the local row and the server-side draft.
- Plain attachments (file picker → Graph attach API) are in scope; inline images, signatures, and rich HTML happen in Outlook on send.

## 6. Explicitly deferred to post-v1

- Teams chat reading (`Chat.Read` is granted but scope creep for v1)
- Multi-account
- Cross-folder bulk operations
- Thread-level (conversation) bulk operations
- Body regex search (Graph `$search` is token-based, not regex; local regex is feasible only over cached content)
- Encrypted/signed mail (S/MIME, PGP)
- Rich HTML composition (drafts created with plain-text only in v1; user's Outlook signature is applied when they open the draft to send)
- CLI-mode compose (`inkwell reply <id> --to=...`); compose is interactive-only in v1
- Inline image attachments in drafts (plain attachments only in v1)
- Graph webhook subscriptions (require public HTTPS endpoint — not feasible for desktop TUI)

## 7. Non-functional requirements

- **Cold start time:** TUI fully interactive within 500ms of launch (using cached data; sync runs in background).
- **Cached operation latency:** Folder switch, message open from cache, local search — all <100ms.
- **Memory footprint:** <200MB resident under normal use. SQLite cache on disk may be multi-GB for heavy users; that is acceptable.
- **Network failure tolerance:** All read operations work offline against the cache. Write operations queue and replay on reconnect.
- **Security:** No mail content ever written outside the user's home directory. SQLite cache file at `~/Library/Application Support/inkwell/mail.db` with mode 0600. Tokens in Keychain only.
- **Distribution:** Single signed and notarized macOS binary. Apple Developer ID required. Homebrew tap as the install path.
- **Logging:** Structured logs to `~/Library/Logs/inkwell/`. Never log message bodies, tokens, or PII. Log rotation at 10MB.

## 8. Out of scope (non-goals)

- Windows or Linux support in v1 (architecture should not preclude, but no testing matrix)
- Outlook.com personal accounts (work/school only)
- IMAP / SMTP fallback
- Migration from Apple Mail / Thunderbird
- Calendar invitations workflow
- Server-side rule management (use Outlook for that; we have client-side saved searches instead)

## 9. Success criteria for v1

A v1 ships when a target user can, on their primary work machine:

1. Sign in via device code flow without IT intervention beyond the one-time tenant app registration.
2. Read and triage their inbox for a full workday without opening Outlook for Mac, except for the final send-button click on outgoing replies.
3. Compose a reply / reply-all / forward in their editor of choice and have it land as a draft in the server's Drafts folder, ready for Outlook to finish.
4. Run a pattern-based bulk cleanup (e.g., delete all newsletters older than 30 days) in under 30 seconds end-to-end.
5. Search across both cached and non-cached mail with results visible in under 2 seconds.
6. View calendar at a glance without leaving the terminal.

## 10. Feature spec inventory

The following feature specs implement this PRD. Each lives in `docs/specs/`. Specs assume the reader has already read this PRD, `ARCH.md`, and `CONFIG.md`.

| #   | Spec file                          | Capabilities (§5 ref)        |
| --- | ---------------------------------- | ---------------------------- |
| 01  | `01-auth-device-code.md`           | 5.1                          |
| 02  | `02-local-cache-schema.md`         | 5.2                          |
| 03  | `03-sync-engine.md`                | 5.3                          |
| 04  | `04-tui-shell.md`                  | 5.4                          |
| 05  | `05-message-rendering.md`          | 5.5                          |
| 06  | `06-search-hybrid.md`              | 5.6                          |
| 07  | `07-triage-actions-single.md`      | 5.7                          |
| 08  | `08-pattern-language.md`           | 5.8 (parser only)            |
| 09  | `09-batch-engine.md`               | 5.8 (execution)              |
| 10  | `10-bulk-operations.md`            | 5.8 (UX integration)         |
| 11  | `11-saved-searches.md`             | 5.9                          |
| 12  | `12-calendar-readonly.md`          | 5.10                         |
| 13  | `13-mailbox-settings.md`           | 5.11                         |
| 14  | `14-cli-mode.md`                   | 5.12                         |
| 15  | `15-compose-reply.md`              | 5.13                         |
| 16  | `16-unsubscribe.md`                                | post-v1, ROADMAP §0 bucket 1 — **shipped v0.12.0** |
| 17  | `17-security-testing-and-casa-evidence.md`         | hardening — **shipped v0.39.0 / v0.45.0** (CI gates, threat model, privacy doc, path traversal guard) |
| 18  | `18-folder-management.md`                          | post-v1, ROADMAP §0 bucket 1 — **shipped v0.46.0** |
| 19  | `19-mute-thread.md`                                | post-v1, ROADMAP §0 bucket 1 — **shipped v0.47.0** |
| 20  | `20-conversation-ops.md`                           | post-v1, ROADMAP §0 bucket 1 — **shipped v0.48.0** |
| 21  | `21-cross-folder-bulk.md`                          | post-v1, ROADMAP §0 bucket 1 — **shipped v0.49.0** |
| 22  | `22-command-palette.md`                            | post-v1, ROADMAP §0 bucket 2 — **shipped v0.50.0** |
| 23  | `23-routing-destinations.md`                       | post-v1, ROADMAP §0 bucket 2 — **shipped v0.51.0** |
| 24  | `24-split-inbox-tabs.md`                           | post-v1, ROADMAP §0 bucket 2 — **shipped v0.52.0** |
| 25  | `25-reply-later-set-aside.md`                      | post-v1, ROADMAP §0 bucket 2 — **shipped v0.53.0** |
| 26  | `26-bundle-senders.md`                             | post-v1, ROADMAP §0 bucket 2 — **shipped v0.55.0** |
| 27  | `27-custom-actions.md`                             | post-v1, ROADMAP §0 bucket 3 — **shipped v0.56.0** |
| 28  | `28-screener.md`                                   | post-v1, ROADMAP §0 bucket 3 — ready               |
| 29  | `29-watch-mode.md`                                 | post-v1, ROADMAP §0 bucket 3 — ready               |
| 30  | `30-done-alias.md`                                 | post-v1, ROADMAP §0 bucket 3 — ready (binding/branding only) |

Spec 17 (security testing + CASA evidence) is a hardening pass over
the v1 specs — additive, no architectural change. Fully shipped across
v0.39.0 and v0.45.0: CI gates (gosec, Semgrep, govulncheck), security
tests, SECURITY_TESTS.md, THREAT_MODEL.md, PRIVACY.md, and path
traversal guard for attachment save. CLAUDE.md §11 makes "did this PR
change anything spec 17 cares about?" a required cross-cutting
checklist item so future specs surface threat-model deltas as they land.

Specs 16, 18–21 cover the "triage primitives" bucket from
`docs/ROADMAP.md` §0 — all shipped v0.12.0 through v0.49.0.

Specs 22–26 cover the "inbox philosophy" bucket — all shipped
v0.50.0 through v0.55.0.

Spec 27 (custom actions framework, ROADMAP §0 Bucket 3 row 1 / §2)
is the first Bucket-3 entry, **shipped v0.56.0** with the
`--filter` CLI wiring closing in v0.56.1. Spec 28 (screener,
§1.16) is the second, **shipped v0.57.0**. Spec 29 (watch mode,
§1.19) is the third, **shipped v0.58.0**. Spec 30 ("Done" alias,
§1.23) is authored and ready; no dependencies on Bucket 2 work.

**Recommended landing order** (CI scope, foundational → leaves):

1. **01 → 02 → 03 → 04** (foundational; auth, store, sync, TUI shell). These are sequential.
2. **05** (rendering) gates the viewer pane and provides the quote-chain text spec 15 needs.
3. **07** (triage actions) lands the action queue + executor + replay + undo stack — the contract that everything that mutates server state runs through.
4. **15** (compose / reply) lands next, in parallel with **06** (search). 15 reuses 07's executor; 06 is independent of 07.
5. **08 → 09 → 10** (pattern + batch + bulk ops) are tightly coupled and best landed together. They depend on 07's action plumbing.
6. **11, 12, 13** (saved searches, calendar, mailbox settings) are independently parallelizable.
7. **14** (CLI mode) lands last; it surfaces every prior capability as a non-interactive subcommand.

Specs 01–04 are blocking for everything else. Once 04, 05, 07 are green, 15 (compose) can ship without waiting for 06/08–10/11–14.

## 11. Open questions for stakeholders

- Final product name (replace `inkwell` placeholder).
- Whether to require an enterprise distribution channel (e.g., internal Munki / Jamf catalog) in addition to public Homebrew tap.
- Telemetry stance: zero by default, or opt-in anonymous error reporting?
- License: MIT, Apache-2.0, or proprietary?
