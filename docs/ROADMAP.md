# Roadmap

**Document status:** Living document. Captures post-v1 ideas.
**Last reorganized:** 2026-04-29

This roadmap tracks features, improvements, and ideas that didn't make v1.

---

## How to read this document

- **§0** — **Buckets.** Items in §1 are grouped into themed buckets in
  natural dependency order. Each bucket lists items in the order they
  should be specced/built within that theme.
- **§1** — The full backlog with concept / TUI translation / take per
  item. Items are still individually labelled P1 / P2 / P3 / research
  for priority, but the build order is the bucket order in §0.
- **§2** — Custom actions framework. Big design idea; touches almost
  every other feature.
- **§3** — AI integration. Held separately because the design space is
  huge and the ground keeps moving.
- **§4** — Platform expansion (Linux, Windows, mobile).
- **§5** — Things we deliberately won't do.
- **§6** — Contributing to this roadmap.

---

## §0 Buckets — build in this order

### Bucket 1 — Triage primitives

The atomic verbs every other bucket depends on. These ship first.

| Order | Item                            | Spec | Why this slot                                        |
| ----- | ------------------------------- | ---- | ---------------------------------------------------- |
| 1     | First-class unsubscribe (1.4)   | 16   | Shipped v0.12.0. Highest ROI in the backlog.         |
| 2     | Folder management (1.1)         | 18   | Table-stakes capability; needed by routing in B2.    |
| 3     | Mute thread (1.5)               | 19   | Cheap; new `muted_conversations` table reused later. |
| 4     | Conversation-level ops (1.8)    | 20   | Promotes thread as a first-class unit (B2 needs it). |
| 5     | Cross-folder bulk (1.3)         | 21   | Small extension to the existing filter path.         |

### Bucket 1.5 — Pre-public-distribution hardening

Cuts across all v1 specs. CI tooling is live (v0.12.0); the rest
gates the first signed/notarized public binary.

| Order | Item                                          | Spec | Status                                                 |
| ----- | --------------------------------------------- | ---- | ------------------------------------------------------ |
| 1     | Security testing + CASA evidence              | 17   | CI shipped v0.12.0; tests + threat model + privacy doc pending. Required before public v1 distribution. |

### Bucket 2 — Inbox philosophy

Workflow patterns built on the primitives. Users feel these.

| Order | Item                                | Notes                                          |
| ----- | ----------------------------------- | ---------------------------------------------- |
| 1     | Command palette (1.6)               | Owner: spec 22. Discoverability for everything else. |
| 2     | Routing destinations (1.9)          | Owner: spec 23. `sender_routing` table reused by B3. |
| 3     | Split inbox tabs (1.7)              | Depends on saved searches + conversation ops. |
| 4     | Reply Later / Set Aside (1.10)      | Graph categories — independent.                |
| 5     | Bundle senders (1.11)               | Pure UI grouping.                              |

### Bucket 3 — Power-user automation

Ties verbs together once enough verbs exist.

| Order | Item                                | Notes                                          |
| ----- | ----------------------------------- | ---------------------------------------------- |
| 1     | Custom actions framework (§2)       | Needs most primitives in B1+B2.                |
| 2     | Screener (1.16)                     | Uses `sender_routing` from B2.                 |
| 3     | Watch mode (1.19)                   | Small CLI addition.                            |
| 4     | "Done" alias (1.23)                 | Binding/branding only.                         |

### Bucket 4 — Mailbox parity

Capability completeness with native clients.

| Order | Item                                  | Notes                                        |
| ----- | ------------------------------------- | -------------------------------------------- |
| 1     | Focused / Other tab (1.15)            | Cheap, exposes existing Graph signal.         |
| 2     | Server-side rules (1.14)              | New CRUD; independent.                        |
| 3     | Rich-text / Markdown drafts (1.18)    | Editor-side; minor.                           |
| 4     | Calendar invite actions in mail (1.17)| Scope-gated on `Calendars.ReadWrite`.         |
| 5     | Multi-account (1.2)                   | Significant refactor; do when stable.         |

### Bucket 5 — Search & knowledge

| Order | Item                                  | Notes                                        |
| ----- | ------------------------------------- | -------------------------------------------- |
| 1     | Body regex / local body indexing (1.13) | Schema change.                              |
| 2     | Clips (1.12)                          | New `clips` table; FTS-searchable.           |
| 3     | Alternative query syntax (1.24)       | Parser flag, cheap.                          |

### Bucket 6 — Platform & polish

| Order | Item                                  | Notes                                        |
| ----- | ------------------------------------- | -------------------------------------------- |
| 1     | Linux build (§4.1)                    | Most portable; KDE/GNOME keychain.            |
| 2     | Shell completion (1.20)               | Cobra ships it; just publish.                |
| 3     | Launchd integration (1.21)            | Background pre-warm.                         |
| 4     | Snippets (1.22)                       | `[snippets]` config + `:snippet`.            |
| 5     | Windows build (§4.2)                  | Bigger lift; Credential Manager + console.   |
| 6     | Mass-archive doc (1.25)               | Already supported; just write the recipe.    |
| 7     | Encrypted/signed mail (1.26)          | Research; defer until requested.             |

### Bucket 7 — AI (last; per direction)

Tier 1 first (local-only, no data leaves the box). Tier 2 only with
explicit opt-in. Tier 3 (agentic) we don't do.

| Order | Item                                            | Tier  |
| ----- | ----------------------------------------------- | ----- |
| 1     | Local thread summarisation (§3.3 tier 1)        | local |
| 2     | Local triage classification (§3.3 tier 1)       | local |
| 3     | Heuristic auto-categorisation (1.27)            | local |
| 4     | Remote draft generation (§3.3 tier 2)           | opt-in remote |
| 5     | Search by intent (§3.3 tier 2)                  | opt-in remote |
| 6     | Agentic suggestions (§3.3 tier 3)               | research / probably never |

Tier 1 features ship behind a config flag, default off, with a clear
"this never leaves the machine" status indicator. Tier 2 is loud and
opt-in per spec 19+. Tier 3 stays in the backlog as research only.

---

## 1. Backlog

### 1.1 Folder management — P1

Currently you can't create, rename, or delete mail folders from the TUI. Microsoft Graph supports it; we just didn't ship it. Half-day spec. `:folder new`, `:folder rename`, `:folder delete`, plus a contextual action in the sidebar. Owner: new spec 15.

### 1.2 Multi-account — P1

The schema and architecture support multiple accounts; the auth and UI layers assume one. Adding multi-account requires: account picker in the sidebar, per-account sync engines, isolation in the SQLite cache (already present via `account_id` foreign keys), and routing rules ("messages from this domain to this account"). Estimated 3–4 days. Common request from consultants who manage personal + work + multiple client tenants.

### 1.3 Cross-folder bulk operations — P1

v1 scopes filters to the current folder. A filter like `~f newsletter@*` ought to match across all subscribed folders. Requires a schema-level decision about per-folder vs. mailbox-wide pattern execution and a new `:filter --all-folders` flag. Estimated 1–2 days.

### 1.4 First-class unsubscribe — P1

**The concept.** Treat `unsubscribe` as a single-key command that handles List-Unsubscribe headers automatically. Press a binding, the client extracts the header, and either drafts the unsubscribe mail or opens the URL.

**TUI translation.** Add `U` (or `:unsub`) on the focused message:

1. Read the `List-Unsubscribe` header.
2. If `mailto:`, create a draft to that address with the standard unsubscribe body.
3. If HTTP(S), open in default browser.
4. Optionally: bulk-delete all past messages from that sender (configurable).

**Take.** Massive value-per-effort. Probably the single highest-ROI item in this entire roadmap. Half-day spec.

### 1.5 Mute thread — P1

**The concept.** Silence a thread without leaving it. Future replies still arrive in the cache; they just don't surface as new in the list view.

**TUI translation.** Add a `muted_conversations` table:

```
muted_conversations:
  conversation_id (PK)
  muted_at
```

The list view filters out new messages from muted conversations. `M` (capital) on a focused thread mutes it; reversible via the same key.

**Take.** Cheap to implement. Genuinely useful. Should ship in v1.1.

### 1.6 Command palette — P1

**Owner: spec 22.**

**The concept.** A fuzzy-find palette listing every action with its keybinding, opened by a chord (commonly Ctrl+K). Solves the "I forgot the shortcut" problem that vim-style TUIs have.

**TUI translation.** We already have `:` command mode. `Ctrl+K` opens a modal that lists all commands, filterable by typing, with the current keybinding shown right-aligned next to each row (passive cheatsheet). Sigils scope the search: `#` for folders, `@` for saved searches, `>` for commands-only. Custom actions (§2) and routing destinations (§1.9) will register into the same row index without re-architecting.

**Take.** Easy to implement, big UX win. Ships in v1.1.

### 1.7 Split inbox tabs — P1

**The concept.** Divide the inbox into multiple focus areas (e.g., "VIP", "Notifications", "Calendar", "Team") defined by user-supplied queries. Each becomes a tab so the user can process one focus area at a time, reducing context-switching.

**TUI translation.** We already have saved searches as virtual folders. The upgrade:

- **Tabs at the top of the list pane.** Currently the user picks one folder OR one saved search. Splits let them flip between several saved searches as tabs (`Tab`/`Shift+Tab` cycles). Each tab maintains its own scroll position and selection.
- **Auto-archive coupling.** When a message arrives in a split, it's already implicitly "filed." When the user marks it done (`E`), it disappears from all splits. Effectively a rebrand of "archive" as the default state.

**Take.** Genuinely the most productive workflow pattern we know of for a busy mailbox. Spec 17 candidate.

### 1.8 Conversation-level operations — P2

v1 operates on individual messages. "Archive entire thread," "delete entire conversation history" all require thread-aware actions. The data model has `conversation_id`; we just don't act on it as a unit yet.

### 1.9 Routing destinations (Imbox / Feed / Paper Trail) — P2

**Owner: spec 23.**

**The concept.** Divide incoming mail into intent-based streams rather than urgency-based: important mail in one stream, newsletters and digests in a "feed" stream, receipts and transactional in a "paper trail" stream. The user designates per-sender where their mail lands. Once a sender is assigned, all their future mail flows to the right place. Read mail in the primary stream drops off naturally — no archive ritual.

**TUI translation.** Maps onto the existing saved-search machinery. Three pre-seeded saved searches:

- `Imbox` — `~r me & ~y focused & ! ~G Newsletter & ! ~G Receipt`
- `Feed` — `~G Newsletter` (or `~f newsletter@*`)
- `Paper Trail` — `~G Receipt` (or sender-list match)

The categorisation layer needs improvement: instead of relying on Microsoft's `inferenceClassification` (Focused / Other), we add a per-sender preference table (`sender_routing`):

```
sender_routing:
  email_address (PK)
  destination ('imbox' | 'feed' | 'paper_trail' | 'screen')
  added_at
```

When the user marks a sender as "this goes to Feed," all future mail from that sender is auto-tagged. Existing mail can optionally be re-tagged retroactively.

**TUI keybinding suggestion:**
- `1` — assign sender to Imbox
- `2` — assign sender to Feed
- `3` — assign sender to Paper Trail
- `0` — assign sender to screener (§1.16)

**Take.** Genuinely useful pattern. The "no archive" mindset is liberating. Ships as new spec 16 or as part of an "Inbox Philosophy" pack post-v1.

### 1.10 Reply Later / Set Aside stacks — P2

**The concept.** Two adjacent ideas: messages I'll reply to later, and messages I want to keep handy without replying. Each is a stack you can fan out at will.

**TUI translation.** Add two flag-like fields backed by Graph categories:

- A built-in `_reply_later` category — used as a stack. Capital `R` on the focused message adds it; `:replies` opens a modal with the queue.
- A built-in `_set_aside` category — same pattern, key `S`.

These differ from regular flags because they have a dedicated overlay rather than just being an indicator.

**Take.** Genuinely improves workflow for senior pros with a backlog. The "Reply Later" pattern in particular hits something the native client handles poorly. Ships as part of the Inbox Philosophy pack.

### 1.11 Bundle senders — P2

**The concept.** For senders who send a deluge (newsletters, recruiter spam), collapse consecutive messages from the same sender in the list view into a single row. Expanding shows the bundle.

**TUI translation.** Group consecutive same-sender messages in the list model into a collapsible row. `Tab` on a bundle expands it; the bundle row shows `▸ Bob Acme (12 messages, latest 14:32) — most recent subject`. Pure UI feature; no schema changes.

**Take.** Particularly helpful for newsletter dumps and recruiter spam. Trivial once the list model gains a bundle data structure.

### 1.12 Clips — P2

**The concept.** Highlight text within a message and save it as a knowledge fragment outside the email. Useful for door codes, addresses, SKUs, and other key data buried inside marketing fluff.

**TUI translation.** A keybinding (`y` for yank, vim-style) copies the selected line range to a `clips` table:

```
clips:
  id (PK)
  source_message_id (FK)
  text
  label (optional, user-set on save)
  saved_at
```

A `:clips` command opens an FTS5-searchable view of all clips. `Enter` on a clip jumps back to the source message.

**Take.** Doesn't fit existing email-client conventions but the TUI demographic will appreciate something like this. Could ship as a "knowledge cache" mini-feature.

### 1.13 Body regex search locally — P2

Today, body content isn't locally indexed (FTS5 covers subject + bodyPreview only). Server `$search` is token-based, not regex. Adding local body indexing would change the cache from envelope-first to body-first, which has memory and sync-time implications. Worth doing if users complain about search precision.

### 1.14 Server-side rules — P2

Server-side rules run on every incoming message; persist server-side; visible across all clients. Different from saved searches, which are client-side and on-demand. Microsoft Graph supports them via `/me/mailFolders/inbox/messageRules`. A v1 user can manage rules from the web client; we just don't expose them in the TUI. Estimated 2 days.

### 1.15 Focused / Other tab — P2

Microsoft Graph already provides `inferenceClassification` (Focused / Other). Surface it as a tab in the list pane and filter accordingly. Cheap, immediate value. Richer auto-categorisation (Promotions, Updates, Forums) is research-grade — see §3 and §1.21.

### 1.16 Screener for new senders — P3

**The concept.** New senders aren't admitted to the main flow until you say so. Their mail sits in a separate Screener queue until accepted, then routes per §1.9.

**TUI translation.** Build it as a saved search: `~r me & ! IN (sender_routing)`. Senders not in our routing table appear here.

**Take.** The original pattern relies on disabling all-mail notifications and replacing them with selective notifications. Native macOS doesn't expose this for the platform's mail client, and our TUI has no notification subsystem at all. The concept survives as "stuff from people I haven't categorised yet" but loses the notification-suppression. Ships as part of the Inbox Philosophy pack with a note that the experience is partial.

### 1.17 Calendar invitation actions in mail viewer — P3 (scope-gated)

When a meeting invite arrives as mail, render the response options inline (`[A]ccept / [T]entative / [D]ecline`). Out of scope today because we lack `Calendars.ReadWrite` — but if a future tenant grants it, this is a small addition.

### 1.18 Rich-text / Markdown drafts — P3

v1 drafts are plain text. The platform accepts HTML drafts. We could let users compose in Markdown and convert to HTML on save. Not critical; plain text covers 90% of use cases.

### 1.19 Watch mode — P3

`inkwell messages --filter X --watch` continuously updating. Useful for "show me unread from VIPs" as a sidebar tail. Trivial to implement.

### 1.20 Shell completion — P3

Cobra supports it; just need to ship the generated scripts.

### 1.21 Launchd integration — P3

A real macOS daemon via launchd plist for background sync that pre-warms the cache. Today's `inkwell daemon` is foreground only.

### 1.22 Snippets — P3

**The concept.** Reusable text fragments insertable into drafts (signatures, boilerplate replies, attachment-with-cover-note templates).

**TUI translation.** A `~/.config/inkwell/snippets/` directory of text files. In the draft editor (vim/etc.), the user runs the editor's own read command (`:r`) to pull a snippet. We don't need a custom system; we lean on `$EDITOR`'s native abilities.

For users who want first-class support: a `[snippets]` config section mapping names to file paths, plus a `:snippet <name>` command that pipes the snippet to the open draft.

**Take.** Most TUI users will solve this via their editor's existing template / abbreviation systems. Optional convenience.

### 1.23 "Done" alias for archive — P3

A one-key `e` that aliases archive, branded as "mark done" rather than "archive." Mostly a binding/branding question. Configuration option to alias `archive=done` if the user wants it.

### 1.24 Alternative query syntax — P3

Spec 08 implements a Mutt-style operator language. Some users prefer a `from:` / `to:` / `subject:` / `has:attachment:` / `older_than:` syntax. We could add a `:q` mode that accepts that style and translates to our pattern AST. Mostly a parser flag; cheap to add.

### 1.25 Mass-archive backlog — documentation only

Already supported via `:filter ~d >180d & ~U --apply --action archive`. Just needs documentation as a recommended onboarding step ("declare bankruptcy on mail older than 6 months"). No feature work required.

### 1.26 Encrypted/signed mail — research

S/MIME and PGP support. Both require keychain-backed certificate handling and aren't lightweight. Demand is low at the target user demographic; defer until requested.

### 1.27 Richer auto-categorisation — research

Beyond the binary Focused / Other split, classify into Promotions, Updates, Forums, Receipts, etc. Microsoft Graph doesn't provide these signals, so we'd need our own classifier. Three approaches:

- **Heuristic.** Detect `unsubscribe` headers, `noreply@`, `list-id:`, etc. and tag accordingly. Ships small, predictable.
- **Local model.** A bundled small classifier. Adds binary size; latency probably acceptable.
- **User-trained.** The per-sender routing in §1.9 is essentially this, deferred to user input.

The user-trained approach is probably the right answer for this audience. Heuristics as a fast-path baseline. ML-only is overkill.

---

## 2. Custom actions framework

### 2.1 The idea

The most powerful pattern across mature mail workflows is **user-defined verbs**: a named action that combines a series of operations on a message and is invoked with a single keybinding or command.

In v1, our action types are atomic: move, flag, mark read, etc. There's no way to define "do all of these things at once." Custom actions close that gap and turn the client into an inbox-automation tool.

### 2.2 Design

Add a `custom_actions` config section:

```toml
[[custom_action]]
name = "newsletter_done"
key = "n"
description = "Newsletter triage: archive, set Feed routing, mark read"
when = "list, viewer"
sequence = [
  { op = "mark_read" },
  { op = "set_sender_routing", destination = "feed" },
  { op = "archive" },
]

[[custom_action]]
name = "to_client_tiaa"
key = "t"
description = "Move to TIAA client folder and tag"
sequence = [
  { op = "move", destination = "Clients/TIAA" },
  { op = "add_category", category = "TIAA" },
  { op = "mark_read" },
]

[[custom_action]]
name = "sender_punisher"
key = "X"
description = "Aggressive sender cleanup"
prompt_confirm = true
sequence = [
  { op = "filter", pattern = "~f {sender}" },
  { op = "permanent_delete_filtered" },
  { op = "block_sender" },
]
```

Each action is a sequence of operations. Operations can reference fields of the focused message via `{sender}`, `{conversation_id}`, etc. The framework templates these in.

### 2.3 Operation primitives

| Operation | Source | Notes |
| --- | --- | --- |
| `mark_read` | Spec 07 | |
| `mark_unread` | Spec 07 | |
| `flag` | Spec 07 | |
| `unflag` | Spec 07 | |
| `move` | Spec 07 | Param: `destination` |
| `archive` | Spec 07 | |
| `soft_delete` | Spec 07 | |
| `permanent_delete` | Spec 07 | Always confirms unless action declares `safe_to_skip_confirm = true` |
| `add_category` | Spec 07 | Param: `category` |
| `remove_category` | Spec 07 | Param: `category` |
| `set_sender_routing` | New (§1.9) | Param: `destination` |
| `set_thread_muted` | New (§1.5) | |
| `filter` | Spec 10 | Param: `pattern` (with template substitution) |
| `permanent_delete_filtered` | Spec 10 | Acts on the active filter set |
| `move_filtered` | Spec 10 | Param: `destination` |
| `block_sender` | New | Server-side block via mailbox rule |
| `unsubscribe` | New (§1.4) | |
| `open_url` | New | Param: `url` (with template) |
| `shell` | New (research) | Param: `command`. Runs a shell command with message context as env vars |
| `prompt_value` | New | Asks user for a value, binds to `{user_input}` for following ops |

### 2.4 Templating

| Variable | Value |
| --- | --- |
| `{sender}` | from address |
| `{sender_name}` | from display name |
| `{sender_domain}` | domain part of from address |
| `{subject}` | subject |
| `{conversation_id}` | conversation ID |
| `{message_id}` | message ID |
| `{user_input}` | the response to the most recent `prompt_value` op |
| `{date}` | received date in ISO format |
| `{folder}` | parent folder name |

This makes "move all from this sender to a folder I'll name" a 2-op action:

```toml
[[custom_action]]
name = "sender_to_folder"
key = "T"
sequence = [
  { op = "prompt_value", prompt = "Move all from {sender} to folder:" },
  { op = "move_filtered", pattern = "~f {sender}", destination = "{user_input}" },
]
```

### 2.5 Discoverability

Custom actions appear in:

- The command palette (§1.6) under "Custom actions."
- A `:actions` command listing all configured actions.
- Help screen (`?`) under a "Custom actions" section.

### 2.6 Take

**P1.** This is the most differentiated feature we could build. It positions the product as an inbox-automation tool for power users rather than just "a fast mail reader." Ships as a substantial spec (call it spec 18, ~3 days) post-v1.

The design should land in v1.1 or v1.2 — early enough that the user community starts building and sharing custom actions. A `~/.config/inkwell/actions/` directory of community-shared actions would be a good outcome.

---

## 3. AI integration (research)

### 3.1 What AI features for mail typically do

- **Draft replies in the user's voice** based on past sent mail.
- **Summarise long threads** ("Read 47 messages in 3 sentences").
- **Extract action items** from threads.
- **Triage classification** ("This is a sales email," "This is a calendar question").
- **Search by intent** ("emails from Bob about the budget" instead of `from:bob budget`).
- **Suggest split-inbox rules** based on observed user behaviour.

### 3.2 What's hard for our context

- **Tenant policy.** Many enterprise tenants prohibit sending mail content to third-party LLM APIs. This is THE blocker for most enterprise users.
- **Local models.** The window for high-quality mail-task models running on a MacBook is genuinely opening. Latency is borderline acceptable for short tasks; quality varies.
- **Cost.** At scale, even cheap APIs cost a meaningful amount per active user.

### 3.3 Possible integrations

**Tier 1 — local-only — P2:**

- **Summarisation** of cached threads using an on-device model. No data leaves the machine.
- **Classification** ("is this a meeting request?" "is this transactional?") to feed routing into Imbox / Feed / Paper Trail (§1.9).

**Tier 2 — opt-in remote — P3:**

- **Draft generation** with explicit consent and a clear "this calls a remote service" indicator. Configurable per-deployment via `[ai].provider = "local|remote|none"`.
- **Search by intent** — natural-language query → pattern AST.

**Tier 3 — agentic — research:**

- **Inbox automation suggestions** — "every Friday, archive newsletter emails older than 7 days" is already covered by saved searches + cron. The AI version is "infer my routing from my actions and propose new rules."
- **Reply autopilot** — too risky for enterprise mail. Don't.

### 3.4 Take

**P2 for tier 1 (local), P3 for tier 2 (remote opt-in), avoid tier 3.** AI features should be additive and explicit, never silent or pervasive. The TUI demographic is generally suspicious of magic; surface what the AI did and why, with a kill switch in config.

A specific high-value first feature: **thread summarisation using a local model.** Runs in 1–3 seconds on cached data, never leaves the machine, and visibly helps when opening a 47-message thread. This alone justifies an `ai` package and could ship as spec 19.

---

## 4. Platform expansion

### 4.1 Linux — P1

The current architecture is mostly portable. Specifics to address:

- **Keychain.** macOS Keychain → Linux Secret Service API (works with GNOME Keyring, KWallet). The `go-keyring` library we use already supports both.
- **File paths.** `~/Library/Application Support/inkwell/` on macOS → `$XDG_DATA_HOME/inkwell/` (typically `~/.local/share/inkwell/`) on Linux. Trivial.
- **Code signing.** N/A on Linux.
- **`open` command.** Linux uses `xdg-open`. Configurable via `[rendering].open_browser_cmd`.
- **Terminal differences.** Most modern Linux terminals (GNOME Terminal, kitty, alacritty, foot) handle Unicode and 256-color fine. Test on a few.

A Linux build is the natural second platform. ~1 week of porting + testing.

### 4.2 Windows — P2

Harder than Linux:

- **Keychain.** Windows Credential Manager. Different API.
- **TUI rendering.** Windows Terminal handles ANSI well; older console hosts do not. Document a minimum.
- **Conditional Access.** The Apple-side SSO plug-in doesn't apply on Windows; the equivalent is the Web Account Manager. Different integration.
- **File paths.** `%APPDATA%`. Different.
- **Many enterprises mandate Windows.** This is the platform with the largest potential audience.

Real demand exists; cost is moderate. Probably 2 weeks of work + ongoing test matrix maintenance.

### 4.3 Mobile (iOS / iPadOS / Android) — won't do

Bubble Tea doesn't run on mobile in any reasonable way. A mobile client would be a different codebase sharing only the spec layer (API conventions, sync semantics). Out of scope for this project's premise. If v1 succeeds, mobile is a different project.

---

## 5. Things we deliberately won't do

Documenting these prevents recurring "what if we…" requests.

### 5.1 Send mail

PRD §3.2 hard out-of-scope. The tenant doesn't grant `Mail.Send`. Even if a future tenant did, the design choice to defer composition to the platform's native client has aged well: it forces a friction point that prevents accidental sends from a TUI's terse keystrokes.

### 5.2 Replace the platform's native client entirely

The platform handles composition, calendar editing, meeting responses, contact management, and a hundred enterprise integrations we'll never reach. Inkwell is a complement, not a replacement.

### 5.3 IMAP/SMTP fallback

Microsoft 365 IMAP/SMTP requires OAuth and adds nothing for our use case over Graph. Connecting to non-Microsoft mail backends would mean a parallel sync engine and storage layer; out of scope.

### 5.4 Plugin runtime (Lua / JS / etc.)

The custom actions framework (§2) gives users 90% of what plugin systems usually deliver. A real scripting runtime adds complexity (sandboxing, API surface stability, security review) that isn't worth it for the marginal user.

### 5.5 Web UI

The whole point is the terminal. A web port defeats the design premise.

### 5.6 Replace native macOS notifications

The platform's native client handles notifications. We don't. The user keeps it running for notifications; we provide the focused triage surface.

### 5.7 SaaS-hosted variant

The product is local-first. There's no server we operate.

---

## 6. Contributing to this roadmap

Anyone proposing a new feature post-v1:

1. Draft a paragraph in §1 (or open a new section if it doesn't fit).
2. Include: concept, TUI translation, take (priority + reasoning).
3. If priority becomes P1 or P2, follow up with a full spec following the template in `docs/specs/`.

The roadmap is intentionally opinionated. Disagreement is welcome and should be raised as discussion before changes land.
