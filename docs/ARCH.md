# Architecture

**Document status:** Foundational. Read after `PRD.md`, before any feature spec.
**Last updated:** 2026-04-27

This document describes the system architecture, module boundaries, key data flows, and cross-cutting concerns. It is the contract that feature specs rely on. If a feature spec needs to change something here, the change goes here first.

---

## 0. API surface (locked)

The application targets **Microsoft Graph v1.0 only**, at `https://graph.microsoft.com/v1.0`.

We do **not** target:

- **Outlook REST API v2.0** (`https://outlook.office.com/api/v2.0/...`). This API was officially deprecated in November 2020 and decommissioned March 2024. As of this writing some tenants still receive 200 responses from v2.0 endpoints, but this is residual back-compat behavior that Microsoft has publicly committed to ending. Building against v2.0 means building against an API with a passed retirement date and no SLA. Do not be tempted by camelCase-vs-PascalCase preferences or apparent simpler shapes — the v2.0 surface is dead.
- **Microsoft Graph beta** (`https://graph.microsoft.com/beta`). The beta surface has appealing capabilities (`mailboxItem` resource, finer-grained delta APIs) but is explicitly unstable and Microsoft's own SDK documentation warns against beta in production.
- **Exchange Web Services (EWS)**. Deprecated; retiring October 2026.
- **IMAP / SMTP**. Basic Auth was retired October 2022; modern Microsoft 365 IMAP/SMTP requires OAuth and adds nothing for our use case over Graph.

If a future contributor finds an Outlook REST v2.0 endpoint that "works" or a beta endpoint that's "more elegant," the answer is no. Stay on Graph v1.0.

---

## 1. Tech stack (locked)

- **Language:** Go 1.23+
- **TUI framework:** Bubble Tea + Bubbles + Lip Gloss (`github.com/charmbracelet/{bubbletea,bubbles,lipgloss}`)
- **HTTP client:** `net/http` with custom transport for retry / 429 handling. No Microsoft Graph SDK dependency — we want tight control over batching, throttling, and request shaping. Direct REST against `https://graph.microsoft.com/v1.0`.
- **Auth:** `github.com/AzureAD/microsoft-authentication-library-for-go` (MSAL Go) for device code flow and token management.
- **Storage:** `modernc.org/sqlite` (pure Go, no CGO) with FTS5 extension. WAL mode.
- **Keychain:** `github.com/zalando/go-keyring` for macOS Keychain access.
- **HTML rendering:** `github.com/jaytaylor/html2text` for HTML → text. Browser fallback via `open(1)`.
- **Logging:** `log/slog` (stdlib) with JSON handler.
- **Config:** TOML via `github.com/BurntSushi/toml`.
- **Testing:** stdlib `testing` + `github.com/stretchr/testify` for assertions.

Rationale highlights:
- Pure-Go SQLite avoids CGO, dramatically simplifying macOS notarization and cross-arch builds.
- No Graph SDK because the SDK's batching is generic and we have specific Outlook-resource concurrency tuning to do.
- MSAL Go is the only sane device-code-flow path; rolling our own OAuth is not worth it.

## 2. Module layout

```
inkwell/
├── cmd/
│   └── inkwell/
│       ├── main.go              # entrypoint; routes interactive vs CLI mode
│       ├── cmd_root.go          # cobra root
│       ├── cmd_signin.go        # auth subcommands (spec 01, 14)
│       ├── cmd_sync.go
│       ├── cmd_messages.go
│       ├── cmd_filter.go        # bulk operations CLI (spec 14)
│       ├── cmd_rule.go          # saved searches CLI (spec 11, 14)
│       ├── cmd_calendar.go      # spec 12
│       ├── cmd_ooo.go           # spec 13
│       ├── cmd_settings.go      # spec 13
│       └── cmd_export.go        # spec 14
├── internal/
│   ├── auth/                    # MSAL wrapper, Keychain integration (spec 01)
│   ├── config/                  # Config loader, validation, defaults
│   │   ├── config.go
│   │   ├── defaults.go
│   │   └── validate.go
│   ├── graph/                   # Graph REST client, batching, retry, throttling
│   │   ├── client.go            # base HTTP client + transport stack
│   │   ├── batch.go             # $batch request builder (spec 09)
│   │   ├── batch_chunk.go       # chunking helpers (spec 09)
│   │   ├── batch_retry.go       # per-sub-request retry (spec 09)
│   │   ├── messages.go          # /me/messages, /me/mailFolders/.../messages
│   │   ├── folders.go           # /me/mailFolders
│   │   ├── delta.go             # delta query helpers
│   │   ├── calendar.go          # /me/calendar, /me/events (spec 12)
│   │   ├── mailbox.go           # /me/mailboxSettings (spec 13)
│   │   ├── scheduler.go         # priority-aware concurrency (spec 03 §11)
│   │   └── errors.go            # Graph error parsing
│   ├── store/                   # SQLite layer; sole owner of mail.db
│   │   ├── schema.go            # migrations runner (spec 02)
│   │   ├── messages.go          # message CRUD
│   │   ├── folders.go           # folder CRUD
│   │   ├── bodies.go            # tier-2 body cache, LRU eviction
│   │   ├── attachments.go       # attachment metadata
│   │   ├── events.go            # calendar events table (spec 12)
│   │   ├── search.go            # FTS5 queries
│   │   ├── delta.go             # delta token persistence
│   │   ├── actions.go           # action queue (spec 02, 07)
│   │   ├── undo.go              # undo stack (spec 02, 07)
│   │   ├── saved_searches.go    # CRUD over saved_searches table
│   │   └── settings.go          # mailbox-settings cache (spec 13)
│   ├── sync/                    # sync engine: orchestrates graph ↔ store
│   │   ├── engine.go            # main loop, ticker, foreground/background
│   │   ├── backfill.go          # initial 90-day pull
│   │   ├── delta.go             # per-folder delta loop (spec 03 §6)
│   │   ├── folders.go           # folder enumeration sync
│   │   ├── calendar_sync.go     # calendar delta loop (spec 12)
│   │   └── reconcile.go         # delta application + conflict resolution
│   ├── pattern/                 # pattern language parser + evaluator (spec 08)
│   │   ├── lexer.go
│   │   ├── parser.go
│   │   ├── ast.go
│   │   ├── dates.go             # date expression parser
│   │   ├── compile.go           # strategy selection
│   │   ├── eval_local.go        # AST → SQL WHERE
│   │   ├── eval_filter.go       # AST → Graph $filter
│   │   ├── eval_search.go       # AST → Graph $search
│   │   └── execute.go           # public Execute()
│   ├── render/                  # message rendering (spec 05)
│   │   ├── render.go
│   │   ├── headers.go
│   │   ├── html.go
│   │   ├── plain.go
│   │   ├── attachments.go
│   │   ├── links.go
│   │   └── theme.go
│   ├── compose/                 # draft compose helpers (spec 15) + Markdown→HTML (spec 33)
│   │   ├── editor.go            # WriteTempfile, WriteTempfileExt, $EDITOR resolution
│   │   ├── parse.go             # legacy tempfile parse (v1 compose flow)
│   │   ├── template.go          # reply / forward skeleton + quote chain
│   │   ├── markdown.go          # DraftBody type + goldmark CommonMark/GFM renderer (spec 33)
│   │   └── security_test.go     # path-traversal regression coverage (spec 17 §4.4)
│   ├── search/                  # hybrid search (spec 06)
│   │   ├── search.go
│   │   ├── local.go
│   │   ├── server.go
│   │   ├── merge.go
│   │   └── highlight.go
│   ├── action/                  # write operations against Graph; queue + undo
│   │   ├── action.go            # action types (Move, Delete, Flag, etc.)
│   │   ├── executor.go          # single + batch dispatchers (spec 07, 09)
│   │   ├── apply_local.go       # optimistic local mutations
│   │   ├── inverse.go           # compute inverse for undo
│   │   ├── replay.go            # crash-recovery replay
│   │   └── types.go             # per-action-type Graph translation
│   ├── savedsearch/             # virtual folders (spec 11)
│   │   ├── savedsearch.go
│   │   ├── store.go             # CRUD wrapping store + TOML mirror
│   │   ├── evaluator.go
│   │   └── refresh.go
│   ├── settings/                # mailbox settings + TZ resolution (spec 13)
│   │   └── manager.go
│   ├── ui/                      # Bubble Tea models, views, commands
│   │   ├── app.go               # root model
│   │   ├── folders.go           # folder pane
│   │   ├── list.go              # message list pane
│   │   ├── viewer.go            # message viewer pane
│   │   ├── command.go           # :command input
│   │   ├── status.go            # status bar
│   │   ├── filter.go            # filter mode (spec 10)
│   │   ├── bulk.go              # ; prefix dispatch (spec 10)
│   │   ├── preview.go           # bulk preview screen (spec 10)
│   │   ├── progress.go          # bulk progress modal (spec 10)
│   │   ├── triage.go            # triage keybinding wiring (spec 07)
│   │   ├── calendar_pane.go     # spec 12
│   │   ├── calendar_modal.go    # spec 12
│   │   ├── settings_view.go     # spec 13
│   │   ├── ooo_modal.go         # spec 13
│   │   ├── undo_overlay.go      # spec 07, 10
│   │   ├── keys.go              # keymap
│   │   └── theme.go             # lipgloss styles
│   ├── cli/                     # CLI output helpers (spec 14)
│   │   ├── output.go            # text + JSON formatting
│   │   ├── progress.go          # CLI progress bars
│   │   └── prompt.go            # interactive y/N when applicable
│   └── log/                     # logging setup, redaction
└── docs/
    ├── PRD.md
    ├── ARCH.md
    ├── CONFIG.md
    └── specs/
        └── ...
```

**Strict layering rule:** dependencies flow downward only. `ui` and `cli` are the top layer. Below them: `sync`, `action`, `savedsearch`, `search`, `render`, `settings`, `pattern`. Below those: `graph`, `store`. At the bottom: `auth`, `config`. No cycles. No skip-layering (e.g., `ui` does not call `graph` directly — it goes through `sync`/`action`/`render`).

## 3. Three-layer data model

```
┌─────────────────────────────────────────────────────────────┐
│                      Microsoft Graph                        │
│         (source of truth; remote, eventually consistent)    │
└─────────────────┬───────────────────────────┬───────────────┘
                  │ delta query / batch       │ batch writes
                  ▼                           ▲
┌─────────────────────────────────────────────────────────────┐
│                   sync engine + graph client                │
│       (handles delta tokens, batching, throttling, retry)   │
└─────────────────┬───────────────────────────┬───────────────┘
                  │ upsert                    │ enqueue actions
                  ▼                           │
┌─────────────────────────────────────────────────────────────┐
│                   SQLite local store                        │
│        (envelopes always; bodies LRU; FTS5 index)           │
│                  Single owner: store/                       │
└─────────────────┬───────────────────────────┬───────────────┘
                  │ read                      │ optimistic apply
                  ▼                           │
┌─────────────────────────────────────────────────────────────┐
│                   UI layer (Bubble Tea)                     │
│           (renders from store; dispatches actions)          │
└─────────────────────────────────────────────────────────────┘
```

Key invariants:

- **The SQLite store is the only persistent state.** No state lives only in memory across restarts.
- **The UI never blocks on a network call.** Every Graph operation goes through `sync` or `action` and returns to the UI as a `tea.Cmd` result (Bubble Tea message).
- **Optimistic UI applies are written to the store first, dispatched to Graph second, reconciled on response.** A failure rolls back the store change and surfaces an error toast.
- **The store is the single owner of `mail.db`.** No other module opens the DB. All access via `internal/store` interfaces.

## 4. Sync engine state machine

States the engine cycles through:

```
   ┌─────────────┐
   │  idle       │◄────────────┐
   └──────┬──────┘             │
          │ tick / wake        │ all folders synced,
          ▼                    │ no actions pending
   ┌─────────────┐             │
   │  draining   │ ── flush ──►│
   │  actions    │             │
   └──────┬──────┘             │
          │ actions empty      │
          ▼                    │
   ┌─────────────┐             │
   │  delta-     │             │
   │  pulling    │             │
   └──────┬──────┘             │
          │ all folders done   │
          └────────────────────┘
```

Detailed behavior is in `specs/03-sync-engine.md`. Summary:

- **Foregrounded (terminal active):** tick every 30s. On tick: drain action queue → run delta on all subscribed folders → idle.
- **Backgrounded:** tick every 5min.
- **Initial backfill:** runs once on first auth. Pulls last 90 days from Inbox + Sent. Older mail backfills lazily on demand or via an explicit `:backfill` command.
- **On `syncStateNotFound`:** the delta token has aged out. Run a bounded re-sync (last 90 days) and surface a status-line message; older mail will need explicit backfill if needed.

## 5. Graph client design

### 5.1 HTTP transport

A custom `http.RoundTripper` chain:

1. **AuthTransport** — injects bearer token. Refreshes via MSAL on 401.
2. **ThrottleTransport** — honors `Retry-After` headers. On 429, blocks the calling goroutine for the indicated duration. Maintains a leaky-bucket counter as a safety net.
3. **LoggingTransport** — structured request/response logging with body redaction.

### 5.2 Concurrency limits

A semaphore caps concurrent in-flight requests at **4 per mailbox**. This is the historically observed soft limit for Outlook resources, well below the 10k requests / 10 minutes quota but above the unsafe zone where mysterious 429s start firing. Tunable via config.

### 5.3 $batch builder

- Up to 20 sub-requests per batch (Graph hard limit).
- Auto-chunks larger payloads into multiple batches.
- Returns per-sub-request results so callers can identify which IDs failed.
- Sub-requests within a batch are independently throttled — a 429 on one sub-request does not fail the batch. The executor identifies 429'd sub-requests, waits the longest `Retry-After`, and re-batches them.

### 5.4 Delta query helpers

- One delta token per folder, persisted in `store.delta`.
- On nextLink: keep paging until exhausted, accumulating changes.
- On deltaLink: persist new token; round complete.
- Page size: `Prefer: odata.maxpagesize=100` for envelope sync. Bodies are not part of delta sync (see §6).

## 6. Body and attachment caching strategy

Two-tier model.

**Tier 1 — envelope (always cached, kept indefinitely):**
- `id`, `internetMessageId`, `conversationId`, `conversationIndex`
- `subject`, `bodyPreview` (Graph's truncated preview, up to 255 chars)
- `from`, `toRecipients`, `ccRecipients`, `bccRecipients`
- `receivedDateTime`, `sentDateTime`
- `isRead`, `flag`, `categories`, `hasAttachments`, `importance`, `inferenceClassification`
- `parentFolderId`
- Attachment metadata (id, name, size, contentType) — but not bytes

**Tier 2 — body (LRU cached on access):**
- `body.contentType`, `body.content` (full HTML or text)
- Inline attachment bytes
- Cached up to 500 bodies or 200MB on disk, whichever first. LRU eviction.
- On open: check tier 2; if absent, fetch via `GET /me/messages/{id}` with `$select=body`, store, render.

**Attachment bytes (separate, on-demand only):**
- Never prefetched.
- `:save <path>` → fetch, write to disk, do not cache.
- `:open` → fetch to temp file, hand to `open(1)`.

Rationale: envelope sync is fast (<1KB per message). Body fetch is heavy (often 50KB+ per HTML email). Eager body sync bloats the mailbox by 100x and dominates initial-backfill time. Lazy body fetch costs ~200ms on first open of a message; subsequent opens are local.

## 7. Local store schema (high level)

Detailed schema in `specs/02-local-cache-schema.md`. Tables:

| Table          | Purpose                                                        |
| -------------- | -------------------------------------------------------------- |
| `accounts`     | Single row in v1. Tenant ID, user principal name, display name |
| `folders`      | Mail folders (Inbox, Sent, custom, etc.)                       |
| `messages`     | Tier-1 envelopes                                               |
| `bodies`       | Tier-2 bodies with LRU `last_accessed_at`                      |
| `attachments`  | Attachment metadata (no bytes)                                 |
| `delta_tokens` | Per-folder delta token persistence                             |
| `actions`      | Offline action queue (pending writes)                          |
| `undo`         | Session undo stack (cleared on app close)                      |
| `saved_searches` | Named saved searches                                         |
| `events`       | Cached calendar events (spec 12)                               |
| `compose_sessions` | In-flight compose-form snapshots for crash recovery (spec 15 §7) |
| `messages_fts` | FTS5 virtual table over subject + bodyPreview                  |

## 8. Action execution model

Actions are typed records:

```go
type Action struct {
    ID         string    // local UUID
    Type       ActionType // Move, Delete, PermanentDelete, Flag, MarkRead, Categorize, ...
    MessageIDs []string   // Graph IDs to operate on (≥1)
    Params     ActionParams // type-specific (e.g., destination folder)
    CreatedAt  time.Time
    Status     ActionStatus // Pending, InFlight, Done, Failed
}
```

Lifecycle:

1. UI dispatches action (e.g., user presses `;d` after filter).
2. `action.Executor` writes the action to `actions` table with status `Pending`.
3. Executor optimistically applies to local store (e.g., move messages out of current folder view).
4. Executor pushes onto undo stack (for reversible actions).
5. UI re-renders immediately from updated store.
6. Executor batches the action via `graph.Batch` (chunked into 20s).
7. On success: action status → `Done`, removed from queue.
8. On per-sub-request failure: those message IDs stay in `Pending`, will retry on next tick. Local store is reconciled from a fresh delta on the affected folder.
9. On hard error (auth, network down): all message IDs stay `Pending`, executor backs off, UI shows status-line warning.

**Crash safety:** on app start, the executor scans `actions` for `Pending` or `InFlight` rows and resumes. `InFlight` is treated as `Pending` with a "may have already executed" flag — the next delta pull will reveal whether the server already processed it.

**Local-only mutation surfaces.** Two features deliberately bypass the
action queue because they have no Graph round-trip:

- **Mute** (spec 19, table `muted_conversations`) — first explicit
  local-only mutation surface added after the queue existed. The UI
  dispatches `MuteConversation` / `UnmuteConversation` directly on
  the store; the action queue's `dispatch()` switch is reserved for
  Graph-bound verbs.
- **Sender routing** (spec 23, table `sender_routing`) — second such
  surface. `SetSenderRouting` / `ClearSenderRouting` are read-then-
  write internally so a no-op (same destination) skips the SQL write
  entirely. Microsoft's `inferenceClassificationOverride` is
  intentionally unused (spec 23 §2.2): it's prospective-only and
  binary, mismatched against routing's retroactive four-bucket model.
- **Screener** (spec 28) does not add a third surface — it reuses
  spec 23's `routeCmd` for both the pane-scoped `Y`/`N` shortcuts
  and the `:screener accept|reject` cmd-bar verbs. The gate is a
  read-only filter layer over `sender_routing` plus the
  `__screened_out__` sentinel; concurrent `Y` keypresses serialise
  via the SQLite write lock and the `(account_id, email_address)`
  PK conflict-target.

Saved searches (spec 11) are also local-only state but predate the
queue concept and are managed via `savedsearch.Manager`. Categorise
mute + routing as the canonical local-only mutation surfaces; saved
searches as the broader category of local-only state alongside undo
bookkeeping.

## 9. Undo

Session-scoped undo stack:

- Every reversible action (Move, Soft Delete, Flag, MarkRead, Categorize) pushes an inverse onto the stack at execution time.
- `u` keybinding pops and executes the inverse.
- Stack is bounded (50 entries) and cleared on app exit.
- **Permanent Delete is never reversible.** Pressing `D` requires an explicit y/N confirmation showing the count.
- Bulk operations push a single composite undo entry.

## 10. UI architecture (Bubble Tea)

The Bubble Tea Elm-architecture pattern:

- **Model** = root struct holding sub-models (folder pane, list pane, viewer pane, command line, status bar) + global state (selected folder, current filter, mode).
- **Msg** = events. Sources: keyboard input, sync engine notifications, action results, ticker.
- **Update** = pure function from `(Model, Msg) → (Model, Cmd)`. Does not perform I/O.
- **Cmd** = side effect. All Graph calls and DB queries are Cmds. They run on goroutines and emit Msgs back to Update.
- **View** = pure function from `Model → string`. Lip Gloss for styling.

Key pattern: **the UI never awaits.** Any operation that might block (DB read, Graph call) is wrapped as a `tea.Cmd` that returns a `Msg` when complete. The Update function dispatches the Cmd and continues; the result arrives later as a fresh Update cycle.

## 11. Configuration

The canonical configuration reference is `docs/CONFIG.md`. It documents every config key, its default, its valid range, and which spec owns it. **When a feature spec introduces a new config key, it MUST be added to `CONFIG.md` in the same change.**

Summary of the configuration layering used at runtime:

1. **Compiled defaults** — the source of truth for "what the app does out of the box." Defined in code as struct literals in `internal/config/defaults.go`. Every key has a default.
2. **User config file** at `~/.config/inkwell/config.toml`. Overrides any default. Only keys the user wants to change need appear; missing keys fall through to defaults.
3. **Environment variables** (where applicable, e.g., `INKWELL_LOG_LEVEL`). Overrides config file. Useful for ops/debugging.
4. **Command-line flags** (where applicable). Overrides everything else. Per-invocation only.

Saved searches live in a separate file (`saved_searches.toml`) so they can be version-controlled and shared without exposing tenant info.

The config loader is `internal/config/config.go`. It exposes a single `Load() (*Config, error)` function that returns a fully-populated, validated `Config` struct. Validation failures (unknown keys, out-of-range values, malformed durations) return errors with line numbers; the app refuses to start on invalid config rather than silently falling back to defaults.

**Hot reload:** not in v1. Config changes require app restart.

## 12. Logging and observability

- Structured JSON logs via `log/slog`.
- Log levels: `DEBUG`, `INFO`, `WARN`, `ERROR`.
- Default level: `INFO`. `--verbose` flag bumps to `DEBUG`.
- Output: `~/Library/Logs/inkwell/inkwell.log` rotated at 10MB, keeping 5 archives.
- **Redaction is mandatory.** A custom slog handler scrubs:
  - Bearer tokens (anything matching `Bearer .*`)
  - Message bodies (always)
  - Email addresses (replaced with `<email-N>` keyed per-session for log correlation)
  - Subject lines in non-DEBUG logs
- Crash dumps to `~/Library/Logs/inkwell/crash-{timestamp}.log` with the same redaction.

## 13. Error handling philosophy

- **User-facing errors** appear in the status line. Brief, actionable. No stack traces.
- **Recoverable errors** (transient network, 429, single-sub-request batch failures) are retried silently with exponential backoff up to a bound, then surfaced.
- **Auth errors** trigger a re-auth prompt; do not crash.
- **Data corruption** in the local cache: log error, surface "cache rebuild required" prompt, offer to re-init from delta queries.
- **Unrecoverable errors** trigger a clean shutdown with a crash log; never partial state writes.

## 14. Testing strategy

- **Unit tests** for `pattern/`, `store/`, isolated functions in `graph/`.
- **Integration tests** with a recorded Graph API fixture set (using `httptest` + canned responses). No live tenant in CI.
- **End-to-end tests** for the TUI via `teatest` (Bubble Tea's test harness): driven by scripted keystrokes, asserts on rendered output.
- **Manual smoke testing** against a real tenant before each release. Documented checklist in `docs/qa-checklist.md`.

## 15. Build, signing, distribution

- `go build` produces a single binary.
- macOS-specific: code-signed and notarized via Apple Developer ID. Build script at `scripts/release.sh`.
- Distribution: Homebrew tap `<sponsor>/inkwell` (or whatever sponsor namespace).
- Versioning: SemVer. Pre-1.0 uses `0.x.y`.
- Update mechanism: `brew upgrade`. No in-app auto-updater in v1.

## 16. Cross-cutting concerns checklist for every feature spec

When writing or implementing a feature spec, verify it addresses:

- [ ] Which scope(s) does it require? Are they all in PRD §3.1?
- [ ] What state does it read from / write to in the store?
- [ ] What Graph endpoints does it call?
- [ ] How does it behave offline?
- [ ] What is its undo behavior, if any?
- [ ] What error states surface to the user, and how?
- [ ] What is the expected latency budget?
- [ ] What logs does it emit (and what is redacted)?
- [ ] Is there a CLI-mode equivalent (PRD §5.12)?
- [ ] Are tests at unit / integration / e2e levels specified?

## 17. Open architectural questions

- Should the action queue support a "scheduled" status (e.g., "delete this in 24h") for soft-undo windows? — *Deferred; not v1.*
- Should saved searches be evaluated server-side (translating to `$filter` once and caching the message ID set) or always client-side over the cache? — *Hybrid, see spec 11.*
- Folder cache size cap: do we evict folders the user hasn't opened in N days? — *Open question; default to no eviction in v1.*
