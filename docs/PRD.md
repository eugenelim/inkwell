# Product Requirements Document

**Project codename:** `inkwell` (placeholder — rename in repo)
**Document status:** Foundational. Read before any feature spec.
**Last updated:** 2026-04-27

---

## 1. Product summary

A terminal-based, local-first email and calendar client for macOS that talks to Microsoft 365 (Exchange Online) via Microsoft Graph. Built for senior enterprise professionals who live in the terminal and want a faster, keyboard-driven triage loop than the native Outlook for Mac client.

The product is **read-and-triage focused**. It is not a full Outlook replacement. Sending mail and modifying calendar events are deliberately out of scope due to tenant scope restrictions (see §4). The user keeps native Outlook for composition and meeting management; this client owns the read, search, organize, and bulk-cleanup loop.

## 2. Target user

A senior professional at a large enterprise (Accenture-class IT posture is the design baseline). Characteristics assumed:

- Comfortable in the terminal; uses tmux, vim, or similar daily.
- Manages 100+ emails per day across multiple client engagements and internal threads.
- Has a deep mailbox (50k–500k messages, multi-year archive).
- Cannot install software requiring custom Azure AD app registration through self-service. App registration must go through formal enterprise architecture review.
- Subject to Conditional Access policies requiring device compliance via Intune (or equivalent MDM) and/or SSO via the Microsoft Enterprise SSO plug-in for Apple devices.
- Already runs native Outlook for Mac for composition; this client is a complementary triage surface, not a replacement.

## 3. Scope of available Microsoft Graph permissions

This product is designed against the following confirmed delegated scopes. **Feature specs MUST NOT assume any scope outside this set.** If a feature would require a denied scope, it is out of scope.

### 3.1 Granted (in-scope)

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

### 3.2 Denied (hard out-of-scope)

The following capabilities are **structurally impossible** under the current scope grant. Do not design features that depend on them. If a future scope grant changes this, that requires a PRD revision, not a feature workaround.

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

- **Auth flow:** OAuth 2.0 device code flow via MSAL Go (`microsoft-authentication-library-for-go`). Browser-based interactive flows are not viable for a TUI; device code flow is the only realistic path.
- **App registration:** Single sanctioned tenant app registration. Public client (no client secret). Redirect URI: `https://login.microsoftonline.com/common/oauth2/nativeclient` (device code flow uses this implicitly). The registration must be obtained through enterprise architecture review; the client app does NOT self-register or use a Microsoft-published `client_id` belonging to another product.
- **Conditional Access:** The macOS device must already be compliant via Intune (or equivalent) and/or the user must be signed in through the Microsoft Enterprise SSO plug-in for Apple devices. The client app inherits this posture; it does not implement device-compliance signaling itself.
- **Token storage:** macOS Keychain via the `keyring` Go library or `security` CLI shellout. Never written to filesystem in plaintext.
- **Token refresh:** Refresh tokens persist ~90 days for work accounts in this flow. App MUST gracefully prompt for re-auth via device code when refresh fails, without losing local cache state.
- **Multi-account:** v1 supports a single Microsoft 365 account per profile. Multi-account is deferred; the data model should not preclude it.

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
- Create draft reply / reply-all / forward (opens `$EDITOR`)

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

## 6. Explicitly deferred to post-v1

- Teams chat reading (`Chat.Read` is granted but scope creep for v1)
- Multi-account
- Cross-folder bulk operations
- Thread-level (conversation) bulk operations
- Body regex search (Graph `$search` is token-based, not regex; local regex is feasible only over cached content)
- Encrypted/signed mail (S/MIME, PGP)
- Rich HTML composition (drafts created with plain-text only in v1)
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
2. Read and triage their inbox for a full workday without opening Outlook for Mac, except for sending replies.
3. Run a pattern-based bulk cleanup (e.g., delete all newsletters older than 30 days) in under 30 seconds end-to-end.
4. Search across both cached and non-cached mail with results visible in under 2 seconds.
5. View calendar at a glance without leaving the terminal.

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

Specs 01–04 are foundational and must land before the rest. Specs 08–10 are tightly coupled and best landed together. The remainder are independently parallelizable.

## 11. Open questions for stakeholders

- Final product name (replace `inkwell` placeholder).
- Whether to require an enterprise distribution channel (e.g., internal Munki / Jamf catalog) in addition to public Homebrew tap.
- Telemetry stance: zero by default, or opt-in anonymous error reporting?
- License: MIT, Apache-2.0, or proprietary?
