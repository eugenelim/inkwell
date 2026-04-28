# Spec 02 — Local Cache Schema

**Status:** Ready for implementation.
**Depends on:** ARCH §6 (body caching), §7 (schema overview), §8 (action model), §9 (undo).
**Blocks:** Specs 03 (sync), 06 (search), 07 (triage), 09 (batch executor), 11 (saved searches).
**Estimated effort:** 2 days.

---

## 1. Goal

Define the SQLite schema, indexes, and Go data access layer for the local mail cache. The store is the **single owner** of `mail.db` (ARCH §3): no other module opens the DB, no other module writes mail data to disk.

This spec defines:

1. The schema (tables, columns, indexes, FTS5 virtual table).
2. Migration mechanism.
3. Public Go API (`internal/store/`).
4. Concurrency rules.
5. Performance budgets and required indexes.

## 2. Database file

- Path: `~/Library/Application Support/inkwell/mail.db`
- Pragmas applied at open:
  - `journal_mode = WAL` — concurrent reads during sync writes.
  - `synchronous = NORMAL` — durability vs throughput trade; acceptable for a cache.
  - `foreign_keys = ON`
  - `temp_store = MEMORY`
  - `mmap_size = 268435456` (256MB)
  - `cache_size = -64000` (64MB page cache)
  - `busy_timeout = 5000`
- File mode: `0600` (user-only).
- Driver: `modernc.org/sqlite` (pure Go).

## 3. Schema

Schema is versioned. Initial version is `1`. All tables, indexes, and triggers below are in version `1` unless otherwise noted.

### 3.1 `schema_meta`

```sql
CREATE TABLE schema_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
-- Seeded with: INSERT INTO schema_meta (key, value) VALUES ('version', '1');
```

### 3.2 `accounts`

Single-row in v1; structure ready for multi-account.

```sql
CREATE TABLE accounts (
    id            INTEGER PRIMARY KEY,        -- local autoincrement
    tenant_id     TEXT NOT NULL,
    client_id     TEXT NOT NULL,
    upn           TEXT NOT NULL,              -- userPrincipalName
    display_name  TEXT,
    object_id     TEXT,                       -- Graph user.id
    last_signin   INTEGER,                    -- unix epoch seconds
    UNIQUE(tenant_id, upn)
);
```

### 3.3 `folders`

Mail folders. The `well_known_name` field maps to Graph well-known names (`inbox`, `sentitems`, `deleteditems`, `drafts`, `archive`, `junkemail`) and is `NULL` for user folders.

```sql
CREATE TABLE folders (
    id                TEXT PRIMARY KEY,         -- Graph folder id
    account_id        INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    parent_folder_id  TEXT REFERENCES folders(id) ON DELETE CASCADE,
    display_name      TEXT NOT NULL,
    well_known_name   TEXT,                     -- 'inbox' | 'sentitems' | ... | NULL
    total_count       INTEGER NOT NULL DEFAULT 0,
    unread_count      INTEGER NOT NULL DEFAULT 0,
    is_hidden         INTEGER NOT NULL DEFAULT 0,
    last_synced_at    INTEGER                   -- unix epoch seconds
);

CREATE INDEX idx_folders_parent ON folders(parent_folder_id);
CREATE INDEX idx_folders_account ON folders(account_id);
CREATE UNIQUE INDEX idx_folders_well_known ON folders(account_id, well_known_name) WHERE well_known_name IS NOT NULL;
```

### 3.4 `messages`

Tier-1 envelope cache (always present for cached messages; bodies in `bodies` table).

```sql
CREATE TABLE messages (
    id                       TEXT PRIMARY KEY,    -- Graph message id
    account_id               INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder_id                TEXT NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    internet_message_id      TEXT,
    conversation_id          TEXT,
    conversation_index       BLOB,                -- base64-decoded
    subject                  TEXT,
    body_preview             TEXT,                -- Graph's truncated preview (~255 chars)
    from_address             TEXT,                -- email
    from_name                TEXT,
    to_addresses             TEXT,                -- JSON array of {name, address}
    cc_addresses             TEXT,                -- JSON array
    bcc_addresses            TEXT,                -- JSON array
    received_at              INTEGER,             -- unix epoch seconds
    sent_at                  INTEGER,
    is_read                  INTEGER NOT NULL DEFAULT 0,  -- 0/1
    is_draft                 INTEGER NOT NULL DEFAULT 0,
    flag_status              TEXT,                -- 'notFlagged' | 'flagged' | 'complete'
    flag_due_at              INTEGER,
    flag_completed_at        INTEGER,
    importance               TEXT,                -- 'low' | 'normal' | 'high'
    inference_class          TEXT,                -- 'focused' | 'other'
    has_attachments          INTEGER NOT NULL DEFAULT 0,
    categories               TEXT,                -- JSON array of strings
    web_link                 TEXT,
    last_modified_at         INTEGER,             -- Graph lastModifiedDateTime
    -- Local bookkeeping
    cached_at                INTEGER NOT NULL,    -- when we first stored it
    envelope_etag            TEXT                 -- Graph @odata.etag at last fetch
);

CREATE INDEX idx_messages_folder_received  ON messages(folder_id, received_at DESC);
CREATE INDEX idx_messages_conversation     ON messages(conversation_id);
CREATE INDEX idx_messages_from             ON messages(from_address);
CREATE INDEX idx_messages_received         ON messages(received_at DESC);
CREATE INDEX idx_messages_flag             ON messages(flag_status) WHERE flag_status = 'flagged';
CREATE INDEX idx_messages_unread           ON messages(folder_id, is_read) WHERE is_read = 0;
```

The `idx_messages_folder_received` covers the most common UI query ("show me the inbox"). The partial indexes on `flag_status` and `is_read` keep them tiny while still accelerating "all flagged" and "unread in folder" filters.

JSON columns (`to_addresses`, `cc_addresses`, `bcc_addresses`, `categories`) are stored as JSON strings rather than normalized into separate tables. Rationale: they're rarely queried independently, and SQLite's JSON1 functions (`json_each`, `json_extract`) are sufficient for the few cases we do (e.g., "messages where I'm in `to`"). Normalization would 5x the row count and make the cache writer materially slower without comparable read benefit.

### 3.5 `bodies`

Tier-2 body cache. LRU eviction governed by `last_accessed_at`.

```sql
CREATE TABLE bodies (
    message_id        TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    content_type      TEXT NOT NULL,             -- 'text' | 'html'
    content           TEXT NOT NULL,             -- the body, as-is
    content_size      INTEGER NOT NULL,          -- length(content) in bytes
    fetched_at        INTEGER NOT NULL,
    last_accessed_at  INTEGER NOT NULL
);

CREATE INDEX idx_bodies_lru ON bodies(last_accessed_at);
```

Eviction rules:
- Cap: 500 bodies OR 200MB total `content_size`, whichever is reached first.
- Eviction triggered after each insert that pushes us over the cap.
- Evict oldest by `last_accessed_at` until back under the cap.
- Eviction is a background job; never blocks a UI fetch.

### 3.6 `attachments`

Metadata only. Bytes are never persisted to the cache; they go to user-specified paths or temp files.

```sql
CREATE TABLE attachments (
    id           TEXT PRIMARY KEY,
    message_id   TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    content_type TEXT,
    size         INTEGER NOT NULL,
    is_inline    INTEGER NOT NULL DEFAULT 0,
    content_id   TEXT
);

CREATE INDEX idx_attachments_message ON attachments(message_id);
```

### 3.7 `delta_tokens`

One row per (account, folder) being synced.

```sql
CREATE TABLE delta_tokens (
    account_id     INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder_id      TEXT NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    delta_link     TEXT,                     -- the @odata.deltaLink URL (full URL with token)
    next_link      TEXT,                     -- mid-paginate skipToken URL; usually NULL
    last_full_sync INTEGER,                  -- unix epoch seconds
    last_delta_at  INTEGER,                  -- unix epoch seconds
    PRIMARY KEY (account_id, folder_id)
);
```

We persist the entire `@odata.deltaLink` URL, not just the token, because Graph encodes query parameters (`$select`, `$top`) into the link. Persisting the full link is the documented recommended pattern.

### 3.8 `actions`

Offline action queue. See ARCH §8.

```sql
CREATE TABLE actions (
    id            TEXT PRIMARY KEY,            -- local UUIDv4
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    type          TEXT NOT NULL,               -- 'move' | 'soft_delete' | 'permanent_delete' | 'mark_read' | 'mark_unread' | 'flag' | 'unflag' | 'add_category' | 'remove_category'
    message_ids   TEXT NOT NULL,               -- JSON array of message IDs
    params        TEXT,                        -- JSON object of type-specific params (e.g., destination_folder_id)
    status        TEXT NOT NULL,               -- 'pending' | 'in_flight' | 'done' | 'failed'
    failure_reason TEXT,
    created_at    INTEGER NOT NULL,
    started_at    INTEGER,
    completed_at  INTEGER
);

CREATE INDEX idx_actions_status ON actions(status) WHERE status IN ('pending', 'in_flight');
CREATE INDEX idx_actions_created ON actions(created_at);
```

### 3.9 `undo`

Session-scoped undo stack. Cleared on app start (`DELETE FROM undo`).

```sql
CREATE TABLE undo (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    action_type  TEXT NOT NULL,         -- the inverse action type
    message_ids  TEXT NOT NULL,         -- JSON
    params       TEXT,                  -- JSON
    label        TEXT NOT NULL,         -- human-readable, e.g., "Move 12 messages to Inbox"
    created_at   INTEGER NOT NULL
);
```

`AUTOINCREMENT` because we want strict monotonic IDs for stack semantics (undo pops the highest id).

### 3.10 `saved_searches`

```sql
CREATE TABLE saved_searches (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,           -- display name, e.g., "Newsletters"
    pattern       TEXT NOT NULL,           -- the pattern language source, e.g., "~f newsletter@*"
    pinned        INTEGER NOT NULL DEFAULT 0, -- show in sidebar?
    sort_order    INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    UNIQUE (account_id, name)
);
```

### 3.11 `messages_fts` (FTS5 virtual table)

Full-text index over what we have cached. The trigger pattern keeps it in sync with `messages`.

```sql
CREATE VIRTUAL TABLE messages_fts USING fts5(
    subject,
    body_preview,
    from_name,
    from_address,
    content='messages',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);

-- Triggers
CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, subject, body_preview, from_name, from_address)
    VALUES (new.rowid, new.subject, new.body_preview, new.from_name, new.from_address);
END;

CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, body_preview, from_name, from_address)
    VALUES('delete', old.rowid, old.subject, old.body_preview, old.from_name, old.from_address);
END;

CREATE TRIGGER messages_fts_au AFTER UPDATE OF subject, body_preview, from_name, from_address ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, body_preview, from_name, from_address)
    VALUES('delete', old.rowid, old.subject, old.body_preview, old.from_name, old.from_address);
    INSERT INTO messages_fts(rowid, subject, body_preview, from_name, from_address)
    VALUES (new.rowid, new.subject, new.body_preview, new.from_name, new.from_address);
END;
```

We index `body_preview`, not the full body, to keep the FTS index small (~1-2KB per message) and to avoid the cache-fill problem (we'd have to fetch every body to index it). Full-body search is handled by Graph's server-side `$search`. This is intentional: local FTS is for "what I've already seen recently"; server search is for the deep archive.

When a body is fetched (tier-2), we do **not** add it to FTS5. The body cache turns over via LRU; FTS5 is for the stable envelope set.

`unicode61 remove_diacritics 2` is the modern tokenizer and handles non-ASCII subject lines (German colleagues, French project names, etc.) correctly.

## 4. Migrations

Migration framework: simple sequential SQL files embedded via `embed.FS`.

```
internal/store/migrations/
├── 001_initial.sql
├── 002_<future>.sql
└── ...
```

On open:

1. Read `schema_meta.version`. If table doesn't exist, treat as version 0.
2. For each migration file with version `> current`, execute in a transaction. Update `schema_meta.version` in the same transaction.
3. If any migration fails, roll back; surface error; refuse to start.

**Migrations are forward-only.** No down-migrations; if we need to undo, we ship a forward migration that does the inverse.

**Migrations are idempotent where reasonable** — use `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`. But the source of truth is the version number, not idempotency.

## 5. Public Go API

Defined in `internal/store/store.go`:

```go
package store

type Store interface {
    // Account
    GetAccount(ctx context.Context) (*Account, error)
    PutAccount(ctx context.Context, a Account) error

    // Folders
    ListFolders(ctx context.Context, accountID int64) ([]Folder, error)
    GetFolderByWellKnown(ctx context.Context, accountID int64, name string) (*Folder, error)
    UpsertFolder(ctx context.Context, f Folder) error
    DeleteFolder(ctx context.Context, id string) error

    // Messages
    GetMessage(ctx context.Context, id string) (*Message, error)
    ListMessages(ctx context.Context, q MessageQuery) ([]Message, error)
    UpsertMessage(ctx context.Context, m Message) error
    UpsertMessagesBatch(ctx context.Context, ms []Message) error  // single tx
    DeleteMessage(ctx context.Context, id string) error
    DeleteMessages(ctx context.Context, ids []string) error
    UpdateMessageFields(ctx context.Context, id string, fields MessageFields) error

    // Bodies
    GetBody(ctx context.Context, messageID string) (*Body, error)
    PutBody(ctx context.Context, b Body) error
    TouchBody(ctx context.Context, messageID string) error  // updates last_accessed_at
    EvictBodies(ctx context.Context) error                  // run by background goroutine

    // Attachments
    ListAttachments(ctx context.Context, messageID string) ([]Attachment, error)
    UpsertAttachments(ctx context.Context, atts []Attachment) error

    // Delta tokens
    GetDeltaToken(ctx context.Context, accountID int64, folderID string) (*DeltaToken, error)
    PutDeltaToken(ctx context.Context, t DeltaToken) error
    ClearDeltaToken(ctx context.Context, accountID int64, folderID string) error

    // Actions
    EnqueueAction(ctx context.Context, a Action) error
    PendingActions(ctx context.Context) ([]Action, error)
    UpdateActionStatus(ctx context.Context, id string, status ActionStatus, reason string) error

    // Undo
    PushUndo(ctx context.Context, e UndoEntry) error
    PopUndo(ctx context.Context) (*UndoEntry, error)
    PeekUndo(ctx context.Context) (*UndoEntry, error)
    ClearUndo(ctx context.Context) error

    // Saved searches
    ListSavedSearches(ctx context.Context, accountID int64) ([]SavedSearch, error)
    PutSavedSearch(ctx context.Context, s SavedSearch) error
    DeleteSavedSearch(ctx context.Context, id int64) error

    // FTS search
    Search(ctx context.Context, q SearchQuery) ([]MessageMatch, error)

    // Lifecycle
    Close() error
    Vacuum(ctx context.Context) error  // periodic maintenance
}

func Open(path string) (Store, error)
```

`MessageQuery` is the workhorse for list views:

```go
type MessageQuery struct {
    AccountID    int64
    FolderID     string         // optional
    ConversationID string       // optional
    From         string         // exact email match, optional
    UnreadOnly   bool
    FlaggedOnly  bool
    HasAttachments *bool
    ReceivedAfter  *time.Time
    ReceivedBefore *time.Time
    Categories     []string     // ANY-of match
    OrderBy        OrderField   // received_desc | received_asc | subject_asc | from_asc
    Limit          int
    Offset         int
}
```

`SearchQuery` is FTS-specific:

```go
type SearchQuery struct {
    AccountID int64
    FolderID  string  // optional, scope to folder
    Query     string  // FTS5 syntax (passed through)
    Limit     int
}
```

## 6. Concurrency

- The DB connection pool is sized to `runtime.NumCPU()`, capped at 8.
- WAL mode permits one writer + many readers.
- All write operations use `BEGIN IMMEDIATE` to avoid SQLite's "deferred-then-upgrade-fails" deadlock pattern.
- Bulk operations (`UpsertMessagesBatch`, `DeleteMessages`) wrap a single transaction.
- The store has a single internal `sync.Mutex` only for the migration step at open; runtime concurrency is fully delegated to SQLite's WAL.

The store is goroutine-safe. Callers do not need external locking.

## 7. Performance budgets

These are non-negotiable and must be verified in benchmarks (see §9).

| Operation                                                   | Target latency (p95) |
| ----------------------------------------------------------- | -------------------- |
| `GetMessage(id)` for a cached message                       | <1ms                 |
| `ListMessages(folder=Inbox, limit=100)` from a 100k-message Inbox | <10ms          |
| `UpsertMessage(single)`                                     | <5ms                 |
| `UpsertMessagesBatch(100)`                                  | <50ms                |
| `Search(q="meeting", limit=50)` over 100k messages           | <100ms               |
| App-start migration check on existing DB                     | <50ms                |
| Body fetch from `bodies` table                              | <5ms                 |

## 8. Maintenance

A nightly maintenance job (run on app start if last-run > 24h):

1. `EvictBodies` until under cap.
2. `DELETE FROM actions WHERE status = 'done' AND completed_at < now - 7d`.
3. `PRAGMA optimize;`
4. (Weekly only): `VACUUM` if `freelist_count` > 1000.

## 9. Test plan

### Unit tests

- Schema migration from 0 → current succeeds and is idempotent (running twice is a no-op).
- Each public API method round-trips correctly (insert → read → delete → confirm absent).
- FTS5 triggers fire correctly: insert/update/delete on `messages` reflect in `messages_fts`.
- Body LRU eviction respects both row-count and byte-size caps.
- Concurrent writes from N goroutines do not corrupt; final state is consistent.

### Benchmarks

For each performance budget in §7, write a benchmark that asserts the budget is met on a 100k-message synthesized dataset. Benchmark fixtures live in `internal/store/testdata/`. Use `go test -bench` and fail CI if a budget regresses by >50%.

### Integration tests

- Open a fresh DB; run migration; insert 1k messages; close; reopen; confirm all messages present.
- Simulate a crash mid-transaction (kill the process during a `BeginImmediate` batch insert); reopen; confirm partial writes did not persist.
- Run `Vacuum` on a 100MB DB and confirm size reduction.

## 10. Definition of done

- [ ] All tables, indexes, and FTS triggers from §3 created by migration `001_initial.sql`.
- [ ] Public API in §5 implemented and tested.
- [ ] All performance budgets in §7 verified by benchmarks.
- [ ] Test coverage ≥ 80% on `internal/store`.
- [ ] Concurrent-access stress test passes (N=8 goroutines, mixed reads/writes, 60 seconds, no errors).
- [ ] DB file mode is 0600 on creation (verified by integration test on macOS).

## 11. Configuration

This spec owns the `[cache]` section. Full reference in `CONFIG.md`.

| Key | Default | Used in §  |
| --- | --- | --- |
| `cache.body_cache_max_count` | `500` | §3.5 (eviction trigger) |
| `cache.body_cache_max_bytes` | `209715200` (200MB) | §3.5 (eviction trigger) |
| `cache.vacuum_interval` | `"168h"` (7d) | §8 |
| `cache.done_actions_retention` | `"168h"` (7d) | §8 |
| `cache.mmap_size_bytes` | `268435456` (256MB) | §2 (PRAGMA) |
| `cache.cache_size_kb` | `65536` (64MB) | §2 (PRAGMA) |

**Hard-coded, intentionally non-configurable:**
- DB file path (`mail.db` under `~/Library/Application Support/inkwell/`). Changing this would require multi-account support to land first.
- File mode `0600`. Security baseline.
- WAL mode and other PRAGMAs not listed above. Changing these without testing risks correctness, not just performance.
- FTS5 tokenizer (`unicode61 remove_diacritics 2`). Changing it requires a forward migration.

The store accepts a `*config.Config` at `Open()`. Configuration values are read once at open; runtime changes require restart.

## 12. Out of scope

- Cross-account queries (deferred with multi-account support).
- Encrypted SQLite (SQLCipher). The cache is in the user's home directory under their account; FileVault and macOS account isolation are the security boundary. SQLCipher would require CGO and would defeat one of the reasons for choosing modernc.
- Replication / sync between machines. The cache is per-machine.
- Folder eviction. We keep all folders' messages indefinitely; only bodies LRU-evict.
