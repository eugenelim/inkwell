# Spec 35 — Body regex & local body indexing

**Status:** Draft.
**Tracking note:** `docs/plans/spec-35.md`.
**Depends on:** Spec 02 (store schema, `bodies` table + LRU at
`internal/store/bodies.go:31-119`, `messages_fts` at
`internal/store/migrations/001_initial.sql:142-167`, migration runner +
`SchemaVersion` constant at `internal/store/store.go:21-22`), Spec 03
(sync engine — body fetch path; this spec hooks the post-fetch decode
step), Spec 05 (renderer — `htmlToText` at `internal/render/html.go:25`
is the same decoder used for indexing), Spec 06 (hybrid search Stream
+ `/` UI; spec 06 §11 explicitly rules body indexing out of v1 and is
superseded by this spec — §15.1 doc-sweep entry), Spec 08 (pattern
language — `FieldBody` / `FieldSubjectOrBody` predicates,
`eval_local.go:125-128`, `eval_search.go:95-100`, `eval_filter.go:126`
`ErrUnsupported` site; strategy selector at
`internal/pattern/compile.go:276-396`).
Reuses but does not change: Spec 17 (privacy / threat model — the new
on-disk surface is documented in `docs/THREAT_MODEL.md` "Threats and
mitigations" and `docs/PRIVACY.md` "Where data is stored" per §8;
PR description carries the spec-17 impact line), Spec 11 (saved
searches — body patterns become locally executable when this spec is
enabled; `pattern` field on the row is unchanged but compilation can
return `ErrRegexRequiresLocalIndex` if the user later disables the
index — surfaced in the picker per §9.5).
**Blocks:** None. A future Clips spec (roadmap §1.12) and any local
"AI tier 1" features (roadmap bucket 7) that need decoded body text on
disk would build on the `body_text` table this spec introduces.
**Estimated effort:** 3–4 days (one schema migration, two FTS5
virtual tables, ingestion wired into the existing body-cache write
path via a render-supplied decoded-text callback, three new pattern
execution paths, one CLI verb family, one config section, full test
pyramid + perf budgets).

---

### 0.1 Spec inventory

Body regex / local body indexing is item 1 of **Bucket 5 — Search &
knowledge** in `docs/ROADMAP.md` (Bucket 5 §0 table; backlog entry
§1.13 "Body regex search locally — P2"). The roadmap is explicit
about the constraint:

> Today, body content isn't locally indexed (FTS5 covers subject +
> bodyPreview only). Server `$search` is token-based, not regex.
> Adding local body indexing would change the cache from
> envelope-first to body-first, which has memory and sync-time
> implications. Worth doing if users complain about search precision.

This spec ships the **opt-in** version of that work: an envelope-first
cache is preserved; the body index is a parallel store gated behind
`[body_index].enabled` (default `false`), eviction-bounded
independently of the body LRU, and never required by any other
feature. With it on, three new capabilities land:

1. `~b` (body) and `~B` (subject or body) become **locally
   executable** for indexed messages — no server round-trip, `/`
   results draw the body match into the live stream instead of
   waiting on `$search`. `~h` (raw header) **stays server-only**:
   the raw RFC 5322 header block is not in the store schema and
   indexing it is explicitly out of scope (§17).
2. **Regex** body / subject search via a new `/.../` operand on
   `~b`, `~B`, `~s`, and the `/`-mode `regex:` prefix. Implemented
   by trigram FTS5 narrowing + Go-side `regexp` post-filter — no
   CGO, no custom tokenizers, no `sqlite-regex` extension.
3. CLI surface (`inkwell index status | rebuild | evict | disable`)
   plus matching `:index <subverb>` cmd-bar verbs and four command
   palette rows for users to inspect and manage the index.

---

## 1. Goal

Add a parallel, opt-in local index over decoded body plaintext so the
existing pattern language and hybrid search can match body content
**without** Graph round-trips, and so the user can express **regular
expressions** against bodies and subjects. Keep the default off; keep
the envelope-first cache model intact; keep the pure-Go stack (no
CGO, no SQLite extensions).

The user gain: a partner searching for `error code 0x800CCC0F` over a
recent project folder gets a hit in <100ms over what they've already
opened, instead of the current 1–3s server round-trip — and a hit on
an unusual phrase even where Graph `$search`'s token stemming would
miss. Crucially, `/regex:auth.*token=[a-f0-9]{32}` becomes a
two-keystroke command rather than a "fetch and grep in iTerm"
workflow.

---

## 2. Non-goals

- **Indexing every body ever fetched on a `Mail.ReadWrite`-scoped
  mailbox.** The default is opt-in. Even when on, the index is
  bounded by size and folder allow-list (§7.2). A 500 k-message
  mailbox does not silently become a 5 GB index.
- **Replacing the body LRU.** The `bodies` table (spec 02 §3.5)
  continues to hold raw HTML/text for the renderer. The new
  `body_text` table holds decoded plaintext for indexing. The
  former is a render cache; the latter is a search corpus. They
  share a `message_id` PK but evict on different policies (§6.4).
- **Indexing message bodies for the deep archive that have never
  been fetched.** The spec is "index what we have decoded"; backfill
  is bounded by what is currently in `bodies` (§7.3). The `~b`
  predicate against the deep archive continues to route through
  Graph `$search` exactly as today.
- **Indexing the raw RFC 5322 header block.** `~h` continues to
  route through Graph `$search`. The store does not persist raw
  headers.
- **Custom FTS5 tokenizers, `sqlite-regex` loadable extensions, or
  any CGO dependency.** The pure-Go invariant (CLAUDE.md §1) holds.
  We use `modernc.org/sqlite v1.50.0`, which ships SQLite 3.53.0 and
  built-in `unicode61`, `porter`, `ascii`, **and `trigram`**
  tokenizers — verified by the probe in §3.6 (the trigram tokenizer
  was added in SQLite 3.34).
- **Server-side regex.** Microsoft Graph `$search` is token-based;
  regex always runs locally.
- **Encrypted at rest.** The body index inherits the existing CASA
  boundary (FileVault + macOS user account isolation; SQLite at
  `~/Library/Application Support/inkwell/mail.db` with mode 0600).
  SQLCipher would require CGO and would defeat the modernc choice
  (spec 02 §12).
- **Multi-account.** Single-account v1 per PRD §6. The schema
  carries `account_id` on `body_text` for forward-compatibility,
  same shape as `messages.account_id`.

---

## 3. Background — research summary

A condensed survey of how leading clients handle body search drove
the design. Sources cited in §3.7.

### 3.1 What other clients do

| Client | Body index? | Regex? | Storage of decoded text |
| --- | --- | --- | --- |
| notmuch (Xapian) | yes, full | yes via per-field `/regex/`; matches against per-term posting lists (slow on bodies, fast on headers) | decoded plaintext (GMime pipeline strips HTML) |
| mu / mu4e (Xapian) | yes, full | yes, same mechanism as notmuch — term-list enumeration | decoded plaintext |
| mutt (`~b`) | no | yes; linear scan of MIME bodies via `mailcap` text conversion | none persisted |
| NeoMutt | no native; first-class notmuch integration | delegates to notmuch | via notmuch |
| aerc / himalaya | no native; aerc supports notmuch backend | delegates | via notmuch |
| Apple Mail | yes via Spotlight `kMDItemTextContent`; quality has degraded post-Big-Sur | no | OS-managed, opaque |
| Outlook for Mac | delegates to Spotlight | no | OS-managed |
| Thunderbird (Gloda) | yes; SQLite + FTS3/5 over decoded bodies | no (LIKE/MATCH only) | decoded plaintext; `global-messages-db.sqlite` runs 100 MB–5 GB |
| Gmail | server-only, token search; phrase only | no | n/a |
| Mailpile | yes; custom posting list, hashed keys | no | decoded plaintext |

### 3.2 Lessons that shape this spec

1. **Every client that supports offline body search keeps decoded
   plaintext on disk.** The honest position is "opt-in, with an
   explicit retention policy" — mirroring Thunderbird's "messages
   stored offline are indexed" stance rather than notmuch's
   "everything, forever."
2. **HTML is decoded once, then indexed.** Thunderbird, notmuch,
   Mailpile all index the stripped text, not the raw HTML.
   inkwell already runs HTML through `jaytaylor/html2text` in
   `internal/render/html.go:25` — we feed that output to the index.
3. **Xapian-style "regex over term-list" is a workable mental
   model**, but FTS5 has no equivalent. The standard FTS5 pattern
   for substring / regex is **trigram tokenizer + LIKE +
   client-side regex post-filter** (FTS5 docs §4.4.2). That's the
   path here.
4. **`unicode61 remove_diacritics 2`** is already the tokenizer for
   `messages_fts` (`internal/store/migrations/001_initial.sql:149`);
   we keep the same shape for the keyword body index. `porter`
   stemming is reserved for a v2 follow-up (§7); flipping the knob
   without a follow-up migration is a no-op + warning in v1.
5. **No custom tokenizer registration from pure Go.** Custom
   tokenizers target the C-level `sqlite3_tokenizer_module` ABI
   and are not reachable from `modernc.org/sqlite`. The built-in
   set is the closed surface.

### 3.3 Trigram-tokenizer narrowing pattern

FTS5's `trigram` tokenizer breaks input into overlapping 3-grams and
is the **only** built-in tokenizer that makes `column LIKE '%foo%'`
index-accelerated, at the cost of a larger index and a 3-character
minimum for the literal. SQLite docs §4.4.2:

> The trigram tokenizer extends FTS5 to support substring matching
> in general, instead of the usual token matching.

The standard regex narrowing recipe (cited as the design for
Datasette and Anytype FTS regex):

1. Parse the user regex with `regexp/syntax`; walk the AST to
   extract one or more **mandatory literal substrings of length ≥
   3** (e.g. `/auth.*token=[a-f0-9]{32}/` yields `auth` and
   `token=`). A literal is "mandatory" when it appears on every
   path through the regex's top-level concatenation; alternation
   branches yield a literal only if every branch contributes one.
2. AND those literals into a SQL `LIKE '%lit%'` (per-literal) on
   the trigram body column to get a small candidate rowid set.
3. Read the decoded body for each candidate via `body_text`; run
   Go `regexp.MatchString` over each body.
4. If the regex has **no** ≥3-char mandatory literal (e.g.
   `/^.$/`, `/[a-z]+/`, `/.*x.*y.*/`), refuse with
   `ErrRegexUnboundedScan` — the user must add a literal anchor or
   scope the pattern to a folder. (§9.3.)

This bounds worst-case CPU to "candidate set size × body length",
which is small in practice (a typical mandatory literal hits 10–100
bodies, not 100 k).

### 3.4 Storage cost

- A typical business email plain-text body is 1–5 KB; the HTML
  alternative 10–50 KB. We index the stripped text via existing
  `htmlToText`. SQLite forum data on an 80 k-message corpus:
  `content='', detail=none, porter unicode61` adds ~40 % index
  overhead over the stored text; `detail=full` adds 2–3×. Trigram
  on email text runs 3–5× the source.
- Concretely, a 50 k-message indexed corpus with average 4 KB
  stripped text per message: ~200 MB raw text, ~280 MB unicode61
  index, ~600 MB–1 GB trigram index. Total ceiling ~1.5 GB.
- The default cap (§7) is **500 MB combined plaintext** with
  trigram and unicode61 overhead on top; users who push past 50 k
  indexed bodies raise the cap.

### 3.5 Privacy

Every desktop client that indexes bodies keeps the decoded text on
disk. Opt-in + bounded size + explicit `inkwell index disable`
(purges everything) puts inkwell ahead of Thunderbird's "configured
elsewhere" model and on par with notmuch's "you ran this command;
you understand the consequence." Logging: the body indexer must
never log indexed text or message IDs at INFO+; the only level
allowed for per-message diagnostics is DEBUG, and even there a new
`redact.HashMessageID` helper (introduced by this spec — §8.5) hashes
the ID. Redaction tests cover both the indexer and CLI sites per
CLAUDE.md §11.

### 3.6 modernc.org/sqlite probe

Verified locally by `internal/store/fts_probe_test.go` (created by
this spec; CI gate):

```
trigram                : OK
porter unicode61       : OK
ascii                  : OK
external-content fts5  : OK
trigram + detail=none  : OK
sqlite_version()       : 3.53.0
```

If a future modernc release regresses any of these, the probe fails
in unit tests and release blocks. The probe is the spec's canary.

### 3.7 Sources

- [notmuch-search-terms(7)](https://notmuchmail.org/manpages/notmuch-search-terms-7/)
- [mu-query(7)](https://manpages.ubuntu.com/manpages/focal/man7/mu-query.7.html)
- [Mutt manual ch. 4](https://www.sendmail.org/~ca/email/mutt/manual-4.html)
- [NeoMutt notmuch feature](https://neomutt.org/feature/notmuch)
- [aerc-notmuch(5)](https://man.archlinux.org/man/aerc-notmuch.5.en)
- [Outlook for Mac search uses Spotlight](https://support.microsoft.com/en-us/office/troubleshoot-search-issues-in-outlook-for-mac-8bbd21ab-4d87-48e8-82b3-57631f17d7bf)
- [Mozilla MDN: Gloda](http://www.devdoc.net/web/developer.mozilla.org/en-US/docs/Mozilla/Thunderbird/gloda)
- [Mailpile: Indexing Mail](https://github.com/mailpile/Mailpile/wiki/Indexing-Mail)
- [SQLite FTS5 docs](https://www.sqlite.org/fts5.html)
- [SQLite forum: FTS5 index size on email corpus](https://sqlite.org/forum/info/3baccecae55769ff)

---

## 4. Module layout

New code lands in two places. No new top-level package; the index is
a store concern with a thin render-supplied callback and pattern
executor extensions.

```
internal/store/
├── migrations/
│   └── 015_body_index.sql            # new
├── body_index.go                     # new — IndexBody, UnindexBody, Stats, Search*, Evict*, Purge*
├── body_index_test.go                # new — unit + injection probe
├── body_index_bench_test.go          # new — perf budgets §14
├── body_index_redact_test.go         # new — redaction (§8.5, §13.4)
├── body_index_integration_test.go    # new — build-tag integration
├── fts_probe_test.go                 # new — modernc tokenizer CI gate
├── store.go                          # SchemaVersion 14 → 15; Store interface gains 7 methods
├── tabs_test.go                      # assertion at :40 updated to "15"
├── sender_routing_test.go            # OR-list at :320 extended to include "15"
├── AGENTS.md                         # invariant 1 amended: cache-management write carve-out (§6.2)
└── bodies.go                         # unchanged

internal/pattern/
├── ast.go                            # new RegexValue PredicateValue; StringValue unchanged
├── lexer.go                          # /.../ delimiter recogniser
├── eval_local.go                     # ~b / ~B / ~s join body_text when BodyIndexEnabled is in CompileOptions
├── eval_local_regex.go               # new — literal extraction + trigram-narrow + post-filter
├── eval_local_regex_test.go          # new
├── eval_local_regex_bench_test.go    # new
├── eval_local_regex_prop_test.go     # new — property-based literal extraction
├── eval_memory.go                    # EvalEnv gains BodyTextFor func; regex over candidate set
├── eval_search.go                    # untouched (server $search shape unchanged)
└── compile.go                        # CompileOptions gains BodyIndexEnabled; selectStrategy at :276-396 extended

internal/sync/
├── body_index_hook.go                # new — render-callback wiring, gated on config
└── maintenance.go                    # EvictBodies pass extended to also run EvictBodyIndex (§6.4)

internal/render/
├── render.go                         # callback hook fires post-decode with (msgID, decoded text)
└── html.go                           # unchanged; htmlToText reused

internal/log/
├── redact.go                         # adds HashMessageID(id) → opaque hash (§8.5)
└── redact_test.go                    # adds TestRedact_HashMessageID_OneWayAndStable (§13.4)

cmd/inkwell/
├── cmd_index.go                      # new — `inkwell index {status,rebuild,evict,disable}`
└── cmd_index_redact_test.go          # new — CLI redaction (§13.4)

docs/
├── CONFIG.md                         # new [body_index] section (§7)
├── ARCH.md                           # §6 + §7 schema-table row + module-tree row
├── PRD.md                            # §10 inventory updated at ship
├── ROADMAP.md                        # Bucket 5 row + §1.13 — updated at ship
├── THREAT_MODEL.md                   # "Threats and mitigations" row + "Accepted residual risks" entry (§8.1)
├── PRIVACY.md                        # "Where data is stored" row + "What data inkwell accesses" entry (§8.2)
├── specs/06-search-hybrid.md         # §11 bullet struck through; back-ref to spec 35
├── user/reference.md                 # new pattern operands + /regex: prefix + :index verbs
├── user/how-to.md                    # new recipe: "Search inside bodies with regex"
└── plans/spec-35.md                  # tracking note (CLAUDE.md §13)
```

Per-package `AGENTS.md` files exist for `auth`, `graph`, `store`,
`ui`. The only one this spec amends is `store` (the cache-management
write carve-out, §6.2). The other packages have no AGENTS.md and
need none.

---

## 5. Schema — migration 015

`internal/store/migrations/015_body_index.sql`. Bumps `SchemaVersion`
in `internal/store/store.go:22` from `14` to `15`.

```sql
-- Decoded plaintext per message. Populated by body_index.go from the
-- existing htmlToText pipeline (internal/render/html.go:25). Eviction
-- is governed by [body_index] caps in CONFIG.md, NOT by the body LRU.
CREATE TABLE body_text (
    message_id        TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    account_id        INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder_id         TEXT NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    content           TEXT NOT NULL,            -- decoded plaintext
    content_size      INTEGER NOT NULL,         -- length(content) in bytes
    indexed_at        INTEGER NOT NULL,         -- unix seconds
    last_accessed_at  INTEGER NOT NULL,         -- driven by viewer opens + index hits
    truncated         INTEGER NOT NULL DEFAULT 0 -- 1 if body exceeded [body_index].max_body_bytes
);

CREATE INDEX idx_body_text_lru     ON body_text(last_accessed_at);
CREATE INDEX idx_body_text_folder  ON body_text(folder_id);
CREATE INDEX idx_body_text_account ON body_text(account_id);

-- Token index for keyword body search. External content over body_text;
-- mirrors messages_fts (internal/store/migrations/001_initial.sql:142).
-- Tokenizer matches messages_fts (`unicode61 remove_diacritics 2`).
-- porter stemming is reserved for a v2 follow-up; flipping
-- [body_index].stemming in v1 is a no-op + config warning (§7).
CREATE VIRTUAL TABLE body_fts USING fts5(
    content,
    content='body_text',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);

-- Trigram index for substring / regex narrowing. detail=none is the
-- cheapest detail level; we don't need offset/column info because the
-- regex post-filter re-runs against the source text.
CREATE VIRTUAL TABLE body_trigram USING fts5(
    content,
    content='body_text',
    content_rowid='rowid',
    tokenize='trigram',
    detail=none
);

-- Triggers keep both FTS tables in sync with body_text.
CREATE TRIGGER body_text_ai AFTER INSERT ON body_text BEGIN
    INSERT INTO body_fts(rowid, content) VALUES (new.rowid, new.content);
    INSERT INTO body_trigram(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TRIGGER body_text_ad AFTER DELETE ON body_text BEGIN
    INSERT INTO body_fts(body_fts, rowid, content) VALUES('delete', old.rowid, old.content);
    INSERT INTO body_trigram(body_trigram, rowid, content) VALUES('delete', old.rowid, old.content);
END;

CREATE TRIGGER body_text_au AFTER UPDATE OF content ON body_text BEGIN
    INSERT INTO body_fts(body_fts, rowid, content) VALUES('delete', old.rowid, old.content);
    INSERT INTO body_trigram(body_trigram, rowid, content) VALUES('delete', old.rowid, old.content);
    INSERT INTO body_fts(rowid, content) VALUES (new.rowid, new.content);
    INSERT INTO body_trigram(rowid, content) VALUES (new.rowid, new.content);
END;

-- v1 migration is a no-op for existing data: the index is opt-in and
-- starts empty. Users who enable [body_index] later run `inkwell index
-- rebuild` to backfill from the bodies the LRU has cached (§7.3).
```

The migration is **idempotent**: the existing migration runner
(`internal/store/migrations.go`) is the source of truth for version
progression (spec 02 §4). No `IF NOT EXISTS` guards are needed; the
runner refuses to re-execute a completed migration.

### 5.1 Why a separate `body_text` table and not extending `bodies`

- `bodies` stores raw HTML/text exactly as Graph returns it (for
  rendering fidelity in the viewer). The index needs **decoded
  plaintext** post-`htmlToText`. Storing both in one table would
  conflate two different cache populations with different eviction
  policies.
- `bodies` LRU-evicts aggressively (caller-supplied caps from
  `internal/store/bodies.go:60-119`, defaults wired through
  `[cache]` in `docs/CONFIG.md:76-89`). Index membership should
  persist longer than render-cache membership; re-decoding on
  every viewer open is acceptable, re-indexing every body the user
  opens is not.
- `body_text` shape (decoded text + size + folder + indexed_at) is
  cheap; the column overlap with `bodies` is small. Keeping them
  separate keeps each table's schema purpose legible.

### 5.2 Schema-version regression tests

Two existing sites assert the schema version (per CLAUDE.md §16
"new table or column added but the schema-version test still
asserts the old version number"):

- `internal/store/tabs_test.go:40` — `require.Equal(t, "14",
  strings.TrimSpace(ver))` → update to `"15"`.
- `internal/store/sender_routing_test.go:320` —
  `require.True(t, v == "11" || v == "12" || v == "13" || v ==
  "14", ...)` → extend the OR-list to include `v == "15"`.

Failing to update either site is a known §16 review finding.

---

## 6. Indexer — `internal/store/body_index.go`

### 6.1 Public API additions to `store.Store`

```go
type Store interface {
    // ... existing methods ...

    // IndexBody stores decoded plaintext for messageID. ContentSize and
    // indexed_at are set by the implementation; truncated reflects
    // max_body_bytes clamping done by the caller (the renderer
    // truncates before calling).
    IndexBody(ctx context.Context, e BodyIndexEntry) error

    // UnindexBody removes a single message from the body index. Called
    // by `inkwell index evict --message-id=X` and by the rebuild path
    // when --force replaces stale rows. Permanent-delete of the
    // `messages` row cascades via FK (§6.4), so UnindexBody is not on
    // the delete-message hot path.
    UnindexBody(ctx context.Context, messageID string) error

    // BodyIndexStats returns the size and shape of the index for
    // `inkwell index status` and the maintenance loop.
    BodyIndexStats(ctx context.Context) (BodyIndexStats, error)

    // EvictBodyIndex enforces [body_index].max_count / .max_bytes the
    // same way EvictBodies does for the body LRU
    // (internal/store/bodies.go:63). When olderThan is non-zero, rows
    // with last_accessed_at < olderThan are evicted in addition to
    // (not instead of) the cap-driven eviction; this backs the
    // CLI's --older-than flag.
    EvictBodyIndex(ctx context.Context, opts EvictBodyIndexOpts) (int, error)

    // PurgeBodyIndex drops every row in body_text (cascading to
    // body_fts + body_trigram via the AD trigger). Used by
    // `inkwell index disable` and by the startup detector when
    // [body_index].enabled has flipped to false (§12).
    PurgeBodyIndex(ctx context.Context) error

    // SearchBodyText runs an FTS5 query against body_fts and returns
    // matching message IDs ordered by BM25 score. Used by spec 06's
    // hybrid Searcher local branch when [body_index].enabled is true.
    SearchBodyText(ctx context.Context, q BodyTextQuery) ([]BodyTextHit, error)

    // SearchBodyTrigramCandidates returns message IDs whose decoded
    // body content matches LIKE '%lit1%' AND '%lit2%' ... — i.e. the
    // candidate set for a regex post-filter (§3.3 step 2). Optional
    // structural SQL filter is interpolated as an AND clause so a
    // single query handles the combined pattern (§9.4).
    SearchBodyTrigramCandidates(ctx context.Context, q BodyTrigramQuery) ([]BodyCandidate, error)
}

type EvictBodyIndexOpts struct {
    MaxCount  int       // 0 disables count-cap eviction
    MaxBytes  int64     // 0 disables byte-cap eviction
    OlderThan time.Time // zero disables age-based eviction
    FolderID  string    // optional folder scope (used by `inkwell index evict --folder=X`)
    MessageID string    // optional single-message eviction (used by `inkwell index evict --message-id=X`)
}

type BodyIndexEntry struct {
    MessageID  string
    AccountID  int64
    FolderID   string
    Content    string // decoded plaintext (already truncated to <= max_body_bytes)
    // ContentSize is computed from len(Content) by the implementation.
    Truncated  bool   // true if Content was clipped at max_body_bytes
}

type BodyIndexStats struct {
    Rows            int64
    Bytes           int64
    Truncated       int64 // count of rows with truncated=1
    OldestIndexedAt time.Time
    NewestIndexedAt time.Time
}

type BodyTextQuery struct {
    AccountID int64
    FolderID  string // optional folder scope
    Query     string // FTS5 MATCH syntax (sanitised by spec 06's local FTS query builder)
    Limit     int
}

type BodyTextHit struct {
    MessageID string
    Score     float64 // BM25
    Snippet   string  // FTS5 snippet() output around the match
}

type BodyTrigramQuery struct {
    AccountID         int64
    FolderID          string
    Literals          []string // each ≥ 3 characters; ANDed together
    StructuralWhere   string   // optional extra WHERE fragment composed in eval_local.go (e.g. "m.flag_status = 'flagged'")
    StructuralArgs    []any
    Limit             int
}

type BodyCandidate struct {
    MessageID string
    Content   string // full decoded body — used by the regex post-filter
}
```

### 6.2 Cache-management write carve-out

`internal/store/AGENTS.md` invariant 1 forbids writes outside the
action queue, but `EvictBodies` is already a write path called from
`internal/sync/maintenance.go:23-77` that is not covered by the
literal reading (the invariant targets *mail-state* writes). This
spec makes the carve-out explicit for `EvictBodies` and the new
sibling methods (`IndexBody`, `EvictBodyIndex`, `PurgeBodyIndex`,
`UnindexBody`): they are **cache-management** writes that never
mutate user-visible mail state. This spec amends
`internal/store/AGENTS.md` invariant 1 to read:

> **No mail-state write outside the action queue.** Mail
> mutations (move, mark-read, delete) flow through
> `internal/action` → `store.ApplyAction`. Cache-management
> writes (`EvictBodies`, `EvictBodyIndex`, `IndexBody`,
> `UnindexBody`, `PurgeBodyIndex`) are exempt: they are owned by
> the store, do not touch Graph, are idempotent, and have no undo.

The PR description carries the amendment so the review surfaces it.

### 6.3 Ingestion path

The single ingestion site is the existing body-render flow in
`internal/render/render.go`. The renderer already decodes HTML via
`htmlToText` for display; we add one callback hook **after** decode
succeeds. The render package does **not** import sync — that would
be an upward layer crossing. Instead, the engine registers a
callback at renderer construction time.

**Important — indexable text vs display text.** `htmlToText`
(`internal/render/html.go:25`) ends with a call to `normalisePlain`
that **inserts newlines at the viewer width**. Indexing the
wrapped output would silently break `~b /token=abc/` against a body
where the wrapper happened to insert `tok\nen=abc`. The indexer
needs the **pre-wrap** decoded text. The renderer therefore exposes
a sibling function `render.DecodeForIndex(rawHTML string) (string,
error)` that runs the same `trackingPixel` strip + `html2text.FromString`
+ `classifyTables` pipeline but skips `normalisePlain` (no wrap, no
URL truncation, no theme tagging — just decoded plaintext with
collapsed runs of whitespace via a single `strings.Join(strings.Fields(...), " ")`
pass per line so that `token=abc` survives intact). The viewer
callback hands the **`DecodeForIndex`** output to the indexer, not
the wrapped viewer string:

```go
// internal/render/render.go (existing constructor gains one option)
type Options struct {
    // ... existing options ...
    OnBodyDecoded func(ctx context.Context, msgID string, accountID int64, folderID, indexableText string)
}

// internal/sync/engine.go (registration site)
r := render.New(render.Options{
    // ...
    OnBodyDecoded: e.maybeIndexBody,
})
```

The viewer pipeline is unchanged: it continues to call the
existing `htmlToText` for its own width-wrapped display string.
`DecodeForIndex` runs once after a successful decode and feeds the
callback. For `text/plain` bodies, `DecodeForIndex` returns the
raw text with the same whitespace-collapse pass; no
`html2text.FromString` step.

```go
// internal/sync/body_index_hook.go (new)
// Receiver is *engine (the concrete impl at internal/sync/engine.go:232),
// not the Engine interface (line 93).
func (e *engine) maybeIndexBody(ctx context.Context, msgID string, accountID int64, folderID, decoded string) {
    if !e.cfg.BodyIndex.Enabled {
        return
    }
    if !e.bodyIndexAllowed(folderID) { // folder allow-list check (§7.2)
        return
    }
    text := decoded
    truncated := false
    if max := int(e.cfg.BodyIndex.MaxBodyBytes); max > 0 && len(text) > max {
        text = text[:max]
        truncated = true
    }
    if err := e.store.IndexBody(ctx, store.BodyIndexEntry{
        MessageID: msgID,
        AccountID: accountID,
        FolderID:  folderID,
        Content:   text,
        Truncated: truncated,
    }); err != nil {
        // Per §8.5: no message ID, no folder ID, no content.
        e.logger.Warn("body index write failed", slog.String("err", err.Error()))
    }
}
```

The hook is called from two sites:
1. The **render path** every time a body is successfully decoded
   for display (cache fill + warm hit re-decoded after eviction).
2. The optional **backfill path** in §7.3 (`inkwell index
   rebuild`), which walks `bodies`, runs `htmlToText`, and calls
   `IndexBody` directly without going through the renderer
   callback.

`IndexBody` is idempotent: on conflict, content + size + indexed_at
are replaced; `last_accessed_at` is bumped. The `AFTER UPDATE OF
content` trigger only fires when content actually changes, so
repeated opens of the same message do not re-fire the FTS triggers
(SQLite trigger fires only when an `OF` column is written to a new
value).

### 6.4 Eviction and the body-LRU relationship

`body_text` and `bodies` share a `message_id` PK but evict
independently:

| Surface | Cap (default) | Order | Trigger |
| --- | --- | --- | --- |
| `bodies` (spec 02 §3.5) | `[cache].body_cache_max_count` × `[cache].body_cache_max_bytes` (`docs/CONFIG.md:76-89`; defaults 500 rows / 200 MB) | `last_accessed_at ASC` | maintenance loop (`internal/sync/maintenance.go:23-77`) |
| `body_text` (this spec) | `[body_index].max_count` × `[body_index].max_bytes` (default 5000 rows / 500 MB; §7) | `last_accessed_at ASC` on `body_text` | maintenance loop (extends `maintenancePass`) |

The body LRU is a render cache (next-open latency). The body index
cache is a search corpus (recall). Indexes outlive renders.
`maintenance.go`'s `maintenancePass` runs `EvictBodyIndex`
immediately after `EvictBodies`.

`body_text` rows have a `last_accessed_at` that is bumped on two
events:
1. The renderer fires `OnBodyDecoded` for a message that's already
   indexed (signal: "user looked at it again").
2. The indexer surfaces the message in a `~b` / `/regex:` / FTS body
   match. `Search*` methods bump the matched rows' timestamps as a
   side effect (single UPDATE per query).

When the user permanently deletes a message (`D` confirmed), the
`messages` row is removed; `ON DELETE CASCADE` on `body_text`
(REFERENCES messages(id)) drops the row, which fires the AD trigger,
which removes the FTS rows.

### 6.5 Staleness

The store has no per-body ETag. The render path is the source of
truth: every time the renderer decodes a body and fires
`OnBodyDecoded`, the indexer calls `IndexBody`, which fires the
`AFTER UPDATE OF content` trigger if and only if the decoded text
changed. If the body has changed on the server, sync's existing
delta path (spec 03 §6 — per-folder delta tokens) detects a new
`lastModifiedDateTime` / replaces the row; the next viewer open
re-fetches and re-decodes, and the trigger re-indexes. No separate
"is this index stale" job is needed.

The single corner: a body that has changed on the server but has
**not been re-opened locally** holds a stale `body_text` row. Search
against it returns the stale match. This is a documented limitation
(reference.md gains a one-line note). Workaround: `inkwell index
rebuild --force` re-decodes the LRU.

---

## 7. Configuration — `[body_index]`

This spec **owns the `[body_index]` section** of `CONFIG.md`.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | `false` | — | Master switch. When `false`, every code path in this spec is a no-op; the index is empty. Toggling `false → true` is non-destructive (rebuild required); `true → false` triggers a one-shot `PurgeBodyIndex` on next startup (with an INFO log line) — see §12. |
| `max_count` | int | `5000` | 100–100000 | Max indexed messages. LRU eviction (§6.4). |
| `max_bytes` | int | `524288000` (500 MB) | 50 MB–10 GB | Max total stored decoded text (sum of `content_size`). Eviction triggers on whichever cap hits first. Index overhead (FTS5 token + trigram) is observed empirically at ~3–4× this; users should size accordingly. |
| `max_body_bytes` | int | `1048576` (1 MB) | 64 KB–10 MB | Per-message clamp. Bodies decoded above this size are truncated and `truncated=1` is set on the row. Keeps a single huge mail (e.g. a 1000-reply attached thread) from blowing the index budget. |
| `folder_allowlist` | list of strings | `[]` | — | Optional. When non-empty, only indexes messages whose folder `display_name` or `well_known_name` is in this list. Empty means "all subscribed folders." Match is exact; folder paths use forward-slash (`Clients/TIAA`). |
| `stemming` | bool | `false` | — | Reserved for v2. v1 ships only the default tokenizer (matches `messages_fts` per §3.4). Setting `stemming = true` in v1 logs a config-validation warning ("stemming requires a follow-up migration; deferred") and behaves as `false`. |
| `max_regex_candidates` | int | `2000` | 100–50000 | Cap on the trigram-narrowed candidate set fed to Go regex (§3.3 step 2). Beyond this, regex search returns `ErrTooManyCandidates`. |
| `backfill_on_enable` | bool | `false` | — | When the user toggles `enabled false → true`, automatically backfill the folders in `folder_allowlist` (or all subscribed folders if empty) from cached bodies. Default `false` to keep the toggle cheap. Backfill is also runnable on demand via `inkwell index rebuild`. |
| `regex_post_filter_timeout` | duration | `"5s"` | 500ms–60s | Wall-clock cap on the Go regex post-filter for a single search call. On timeout, return whatever matched and a "search cut short" status. |

Owner: spec 35. All defaults err on the side of "the user has to
explicitly opt in to size and behaviour they did not ask for."

### 7.1 Config validation

`internal/config/validate.go` adds three rules:

- `max_body_bytes ≤ max_bytes / 8`. A per-message cap that exceeds
  the global cap is incoherent.
- `max_regex_candidates ≤ max_count × 2`. Beyond this, the trigram
  index can't return enough candidates anyway.
- `folder_allowlist` items must be valid folder identifiers; the
  store resolves them at startup and logs unknown entries as
  warnings (does not refuse to start — folder names can change).

### 7.2 Folder allow-list semantics

When `folder_allowlist` is non-empty:
- `maybeIndexBody` consults a per-engine resolved set at config-load
  time.
- A folder rename keeps the index for messages indexed under the
  old name (`folder_id` is stable in Graph). The next startup
  resolves the new `display_name` against the allow-list; if it
  no longer matches, the folder's existing rows are **kept** but
  no new rows are added. `inkwell index evict --folder=NAME` is
  the explicit cleanup path.
- `~m Folder & ~b text` works against any folder that ever had
  rows, regardless of current allow-list membership. Closing the
  allow-list is a forward-only knob.
- Message moves across folders are reflected by updating
  `body_text.folder_id` in the same transaction that updates
  `messages.folder_id`. Without this patch, an indexed message
  moved out of an allow-listed folder would still appear in
  `~m Inbox & ~b foo` after the move. The
  `UpdateMessageFields` site in
  `internal/store/messages.go` gains a one-line UPDATE on
  `body_text` (regression test in `body_index_test.go`).

### 7.3 Backfill mode

`inkwell index rebuild [--folder=NAME] [--limit=N] [--force]`:
- Walks the **`bodies` table** (or a folder-scoped subset), pulls
  each raw body through `htmlToText`, and writes to `body_text`.
  **No Graph round-trips** — only bodies the user already has in
  the LRU cache. Because the body LRU is small by default
  (`[cache].body_cache_max_count = 500`,
  `[cache].body_cache_max_bytes = 200 MB`), a long-running user's
  rebuild indexes ~500 bodies in v1, not 50 000. This is a
  feature, not a bug: indexing what hasn't been opened would
  require Graph round-trips, and we defer that.
- Bounded by `[body_index].max_count`, `.max_bytes`, and the
  optional `--limit`. Reports progress on stdout (TTY) or a
  newline-delimited JSON stream (non-TTY).
- Idempotent and resumable: re-running `rebuild` after a Ctrl-C
  picks up where it left off (rows already in `body_text` are
  detected by PK and skipped unless `--force`, which re-decodes
  via the AFTER UPDATE OF content trigger).
- `inkwell index status` emits a row-count vs body-LRU-count line
  so the user can see at a glance how much of their mailbox is
  actually indexed:

  ```
  Indexed: 412 messages (cap 5,000; current body-LRU size 487)
  ```

Backfilling **unread / archived** bodies that have never been
fetched is out of scope (§17). A 50 k-message backfill is roughly
50 k × ~200 ms = 2.8 hours and 5–10 GB of network. The user can
open the messages they care about, run `:filter ~F & ~d <90d` to
warm the LRU first, or accept the limitation.

---

## 8. Privacy & security — spec 17 impact

This spec adds a new on-disk surface (`body_text` + two FTS5 virtual
tables) holding **decoded plaintext message bodies**. That is the
single most sensitive bulk data inkwell persists. The PR description
carries the spec-17 impact line:

> spec 17 impact: new opt-in on-disk plaintext index; THREAT_MODEL
> "Threats and mitigations" + PRIVACY "Where data is stored"
> updated; redaction tests added for the indexer and CLI; no new
> scopes.

### 8.1 Threat model update

`docs/THREAT_MODEL.md` "Threats and mitigations" table (the existing
flat section beginning at line 55) gains a row covering the new
on-disk surface:

> | Threat | Asset | Mitigation |
> |---|---|---|
> | Local-state exfiltration with `[body_index].enabled = true` | Decoded plaintext bodies in `mail.db` (the `body_text` table + FTS5 companions) | Opt-in default; configurable cap (default 500 MB plaintext); `inkwell index disable` purges everything; inherits FileVault + macOS user-account isolation + mode `0600` on `mail.db`. |

The "Accepted residual risks" section gains a one-line entry:

> When `[body_index].enabled = true`, the on-disk blast radius of
> `mail.db` exfiltration grows from "envelopes + recent bodies
> (LRU cap)" to "envelopes + indexed plaintext bodies
> (`[body_index].max_bytes`)". This is the user's explicit choice.

### 8.2 Privacy doc update

`docs/PRIVACY.md` "Where data is stored" (existing section starting
at line 59) gains a row:

> `body_text` (and its FTS5 companions): decoded plaintext per
> indexed message, with HTML stripped via `jaytaylor/html2text`.
> Opt-in via `[body_index].enabled` (default `false`). Inherits the
> same on-disk protections as the rest of `mail.db`. Erased by
> `inkwell index disable` or by flipping `[body_index].enabled` to
> `false` (one-shot purge on next startup).

"What data inkwell accesses" gains a sentence under the existing
body-cache description:

> When `[body_index].enabled` is set, inkwell additionally retains
> a decoded plaintext copy of each body it indexes; the source
> material is the same Graph body fetch we already make for
> rendering, never a separate Graph call.

### 8.3 No new scopes

The body indexer **only reads `bodies`**; it never calls Graph.
`Mail.Read` (granted) covered fetching the body initially; this spec
re-uses what's already on disk. CI lint guard from CLAUDE.md §7
invariant 4 (`Mail.Send` denial) remains green by construction.

### 8.4 Destructive-action gate

`inkwell index disable` is a destructive operation — it permanently
drops every row in the body index (which is not re-derivable
without re-decoding every cached body). Gate it with the
confirmation prompt pattern from spec 10 §5.4 + spec 07 §3
(default "No"):

```
$ inkwell index disable
This will delete the local body index (~427 MB across 3,812 messages)
and disable indexing until you re-enable [body_index].enabled = true.
You can rebuild from cached bodies later with `inkwell index rebuild`.
Proceed? [y/N]: _
```

`--yes` skips the prompt (scripting) per spec 14 §6 convention.

### 8.5 Log redaction

`internal/log/redact.go` (spec 17 §4.2) gains:

- A new helper `HashMessageID(id string) string` that returns a
  short hex digest (truncated SHA-256). Used by indexer / CLI
  DEBUG-level diagnostics so a developer can correlate two lines
  without exposing the underlying Graph ID.
- A test in the same file's test suite asserting the helper is
  one-way and stable across process restarts.

The **indexer site** in `internal/sync/body_index_hook.go` and
`internal/store/body_index.go` MUST NOT log message IDs, folder
IDs, body content, candidate counts, or snippet outputs at INFO+
levels. DEBUG-level logging is allowed only with
`redact.HashMessageID(msgID)`.

The **CLI surface** in `cmd/inkwell/cmd_index.go` displays
aggregated counts and sizes; when `folder_allowlist` is empty, it
does **not** enumerate folder names (shows "all subscribed
folders"); when non-empty it echoes the user's own configured
list, not the resolved Graph names. Snippet output is never echoed
by the CLI.

Redaction tests live at
`internal/store/body_index_redact_test.go` and
`cmd/inkwell/cmd_index_redact_test.go` and cover both sites per
CLAUDE.md §11 ("redaction tests cover every new log site that
could see secrets").

### 8.6 `// #nosec` budget

This spec introduces **zero** new `// #nosec` annotations. The
trigram + LIKE query path uses parameterised SQL (`?` placeholders
for every literal — see `SearchBodyTrigramCandidates` in §6.3 and
the explicit injection probe in §13.1), which is the spec 17 §4.3
baseline. CI's gosec, semgrep, and govulncheck stay green.

---

## 9. Pattern language integration

### 9.1 New AST shape for regex

`internal/pattern/ast.go` gains a new `RegexValue` concrete type
implementing the existing `PredicateValue` interface
(`internal/pattern/ast.go:91`), alongside the existing
`StringValue` (`internal/pattern/ast.go:96-99`):

```go
// RegexValue is the argument for a regex-form predicate. The raw
// source is the user-typed delimiter form (without the slashes); the
// compiled Regexp is produced at parse time so syntax errors surface
// with column positions exactly like date errors (spec 08 §5.2).
type RegexValue struct {
    Raw      string         // the regex source between the // delimiters
    Compiled *regexp.Regexp // compiled at parse time; never nil for a successfully parsed RegexValue
}

func (RegexValue) isValue() {}
```

`StringValue` is **unchanged**. `Predicate.Value` (interface) carries
whichever shape the lexer produced. Existing call sites that
pattern-match on `StringValue` in `eval_search.go`, `eval_local.go`,
`eval_filter.go`, `eval_memory.go`, `lexer.go` continue to compile;
new switch arms for `RegexValue` are added explicitly (§9.2 / §9.4
/ §9.5).

User-facing syntax:

| Pattern | Meaning | Previous behaviour |
| --- | --- | --- |
| `~b "action required"` | exact phrase substring in body | server-only via $search (§9.2 changes the local routing) |
| `~b /auth.*token=[a-f0-9]{32}/` | regex in body | n/a (new) |
| `~B /(?i)urgent\|asap/` | regex in subject or body | n/a (new) |
| `~s /^\[release\]/` | regex in subject | n/a (new; subject lives on `messages`, no `body_text` needed) |
| `~h /regex/` | (unsupported) | `ErrRegexNotSupportedOnHeader` at compile time |

The `/.../` recogniser lives in `internal/pattern/lexer.go`: a
`/`-delimited token, with `\/` escaping for embedded slashes, runs
through `regexp.Compile` at lex time. Perl-style flags after the
closing delimiter (`/.../i`) are **not** supported; users use Go
regexp syntax inline (`(?i)pattern`). Documented in
`docs/user/reference.md`.

### 9.2 Strategy selection — extending the spec 08 §7.2 tree

`internal/pattern/compile.go` adds a `BodyIndexEnabled bool` field
to `CompileOptions` (line 81-98). The engine sets it from
`cfg.BodyIndex.Enabled`. The strategy selector at
`internal/pattern/compile.go:276-396` is amended:

```
Step 0 (NEW, runs before existing steps): if the AST contains any
RegexValue predicate:
    If any RegexValue is on FieldHeader → return
        ErrRegexNotSupportedOnHeader. (~h is server-only and Graph
        $search has no regex; we refuse rather than silently lose
        semantics.)
    For each RegexValue, extract mandatory ≥3-char literals.
        If any RegexValue has no mandatory literal → return
        ErrRegexUnboundedScan.
    If opts.BodyIndexEnabled is false AND any RegexValue is on
    FieldBody / FieldSubjectOrBody → return ErrRegexRequiresLocalIndex.
    (Regex on FieldSubject alone is permitted regardless of
    BodyIndexEnabled — subjects live on `messages`, no body_text
    needed; see §9.4 subject-only emission.)
    → StrategyLocalRegex (new — §9.4).

Step 1 (existing, at compile.go:302): if hasBody or hasHeader:
    If opts.BodyIndexEnabled AND !hasHeader AND every Body/SubjectOrBody
    predicate is a StringValue (no regex):
        → StrategyLocalOnly via CompileLocal, but eval_local.go
          emits the body_text-joined SQL shape (§9.4) instead of
          the body_preview LIKE.
    Else (the existing branch is preserved exactly):
        → StrategyServerSearch / StrategyTwoStage as today.

Steps 2-5 (existing): unchanged.
```

`eval_local.go:125-128` (`emitStringPredicate`) is amended for
`FieldBody` and `FieldSubjectOrBody` to consult the
`BodyIndexEnabled` flag. The threading change: `CompileLocal`
(`internal/pattern/eval_local.go:21`), `emitLocal`
(`eval_local.go:32`), `emitPredicate` (`eval_local.go:66`), and
`emitStringPredicate` (`eval_local.go:111`) all gain a trailing
`opts CompileOptions` parameter. Existing call sites in
`compile.go` (the `CompileLocal(root)` invocations) pass the same
`opts` they were given. `eval_memory.go`'s in-memory evaluator is
unaffected by this thread — it makes its routing decisions from
`EvalEnv`, not from `CompileOptions`.

```go
// internal/pattern/eval_local.go (extended emitStringPredicate)
case FieldBody:
    if opts.BodyIndexEnabled {
        return bodyTextLike(v) // JOIN body_text; LIKE on content
    }
    return likeOne("body_preview", v) // existing behaviour
case FieldSubjectOrBody:
    if opts.BodyIndexEnabled {
        return subjectOrBodyText(v) // (subject LIKE ?) OR (body_text JOIN + LIKE)
    }
    return likeAny([]string{"subject", "body_preview"}, v)
```

The previous behaviour ("LIKE on body_preview when run locally") is
preserved exactly when the index is disabled. This avoids a
silent-mode-change for users who had relied on local `~b` against
the 255-character preview.

### 9.3 Error shapes

| Sentinel | Surfaces as | Cause |
| --- | --- | --- |
| `ErrRegexUnboundedScan` | `"regex requires at least one 3-character literal substring; add a literal anchor or scope to a folder"` | `/^.$/`, `/[a-z]/`, anchored-only patterns |
| `ErrRegexRequiresLocalIndex` | `"regex body / subject-or-body search needs [body_index].enabled = true; run 'inkwell index rebuild' first"` | `[body_index]` off and regex hits body |
| `ErrRegexNotSupportedOnHeader` | `"~h does not support regex; Graph $search is token-based. Use a literal value or run a folder-scoped search."` | `~h /regex/` |
| `ErrTooManyCandidates` | `"too many candidates ({N}); narrow with a folder or a more specific literal"` | candidate cap exceeded |
| `ErrRegexTimeout` | `"regex post-filter exceeded {duration}; returned {K} partial matches"` | `regex_post_filter_timeout` hit |

All four are returned from `pattern.Compile` (the first two) or
`pattern.Execute` (the latter two). UI surfaces (`:filter`,
`/regex:...`) display them verbatim in the status line.

### 9.4 SQL for combined structural + regex (`StrategyLocalRegex`)

When `~b /token=/ & ~F & ~m Inbox` lands on `StrategyLocalRegex`,
`eval_local_regex.go` emits a single query that combines structural
predicates with the trigram narrowing:

```sql
SELECT m.id, bt.content
FROM messages m
JOIN folders f ON f.id = m.folder_id
JOIN body_text bt ON bt.message_id = m.id
JOIN body_trigram tr ON tr.rowid = bt.rowid
WHERE m.account_id = ?
  AND m.flag_status = 'flagged'
  AND (f.display_name = ? OR f.well_known_name = ?)
  AND bt.content LIKE ?       -- '%token=%'
ORDER BY m.received_at DESC
LIMIT ?
```

For each extracted literal, an extra `AND bt.content LIKE ?` is
appended. `?` is parameterised with `'%lit%'` (literal escape for
SQL LIKE metacharacters `\`, `%`, `_` per existing `likeArgs` in
`eval_local.go`). The Go post-filter (`regexp.MatchString`) then
runs over each returned `bt.content`. The post-filter respects
`regex_post_filter_timeout` (§7) via a `context.WithTimeout`
cancellation that fires between iterations.

For `~b /token=/` alone (no structural part), the query collapses
to:

```sql
SELECT m.id, bt.content
FROM body_text bt
JOIN messages m ON m.id = bt.message_id
JOIN body_trigram tr ON tr.rowid = bt.rowid
WHERE m.account_id = ?
  AND bt.content LIKE ?
ORDER BY m.received_at DESC
LIMIT ?
```

`SearchBodyTrigramCandidates` is the public wrapper that returns
`[]BodyCandidate{MessageID, Content}` so the caller post-filters
without a second round-trip to fetch the body.

#### Subject-only regex

`~s /regex/` does **not** join `body_text` and works regardless of
`[body_index].enabled`. The emitted shape:

```sql
SELECT m.id, m.subject
FROM messages m
WHERE m.account_id = ?
  AND m.subject LIKE ?    -- '%lit1%' (per mandatory literal)
  -- additional AND m.subject LIKE ? per literal
ORDER BY m.received_at DESC
LIMIT ?
```

The `LIKE` is **not** index-accelerated (`messages.subject` uses
the existing `messages_fts` with `unicode61`, not trigram). That
is acceptable: the row set is the envelope set, not the body set,
so a full-scan `LIKE` over 100 k subjects is well under the §14
budget for `Search(q, limit=50) <100 ms`. The Go regex post-filter
runs over the returned `m.subject` strings — short by definition.

### 9.5 In-memory evaluator update

`internal/pattern/eval_memory.go` `EvalEnv`
(`internal/pattern/eval_memory.go:16-18`) gains a callback for
lazy-loading body text:

```go
type EvalEnv struct {
    Routing     map[string]string                                    // existing
    BodyTextFor func(ctx context.Context, msgID string) (string, error) // new; nil when body index off
}
```

When `BodyTextFor != nil`, the in-memory evaluator handles
`RegexValue` on `FieldBody` / `FieldSubjectOrBody` by calling
`BodyTextFor(ctx, m.ID)`. If the body is not in the index,
`BodyTextFor` returns `ErrNotFound`; the evaluator drops the
message from the result. Drops are counted and surfaced as
`"refined N → M (K dropped: not in body index)"` in the status line
when `K > 0` — same shape as spec 08 §11.1's "deep-archive gap".

When `BodyTextFor == nil` (index disabled), the evaluator returns
`ErrRegexRequiresLocalIndex` if it encounters a `RegexValue` on a
body field — caller surfaces in the status bar.

### 9.6 Saved-search behaviour (spec 11 interaction)

A saved search with a body / regex pattern stored when
`[body_index].enabled = true` will fail to compile after the user
flips the index off. Spec 11's `Manager`
(`internal/savedsearch/manager.go`) invokes `pattern.Compile` at
four sites:

- `Save` (line 72) — write-time validation.
- `Evaluate` (line 143) — runtime expansion to a message ID list.
- `Edit` (line 218) — write-time re-validation.
- `EvaluatePattern` (line 271; Compile call at line 272) — same as
  Evaluate, by raw pattern source.

All four sites today pass `pattern.CompileOptions{LocalOnly: true}`
without a `BodyIndexEnabled` bit. The constructor at
`internal/savedsearch/manager.go:39` takes
`config.SavedSearchSettings` (not `*config.Config`). This spec
extends `config.SavedSearchSettings` with a `BodyIndexEnabled bool`
field populated from `cfg.BodyIndex.Enabled` at the `savedsearch.New(...)`
invocation in `cmd/inkwell/cmd_run.go` (the single construction
site). `Manager` threads the bit into every `pattern.Compile` call
via `pattern.CompileOptions{LocalOnly: true, BodyIndexEnabled:
m.cfg.BodyIndexEnabled}` so saving `~b /foo/` while the index is
on succeeds.

The preserve-and-tag behaviour lives at the **read** site:

- `Manager.List` (line 49) returns `[]store.SavedSearch`. This
  spec adds a **transient** `LastCompileError string` field to
  `store.SavedSearch` (`internal/store/types.go:278`) — no schema
  column, no migration; the field is populated by `Manager.List`
  after `rows.Scan` by attempting `pattern.Compile` on each row
  and capturing `ErrRegexRequiresLocalIndex` /
  `ErrRegexUnboundedScan` there rather than failing the whole
  list call.
- The parallel UI-local `SavedSearch` type at
  `internal/ui/app.go:413` gains a matching `LastCompileError`
  field. The adapter sites in `cmd/inkwell/cmd_run.go` that
  construct `ui.SavedSearch{...}` literals from
  `store.SavedSearch` (the `SavedSearchService` interface
  implementations — `Reload`, `RefreshCounts`) carry the field
  across.
- The sidebar render path renders rows with a non-empty
  `LastCompileError` greyed out and replaces the count badge with
  a `!` indicator. Focus reveals the error verbatim in the status
  bar. `Enter` on a greyed row shows a toast with the fix hint
  ("enable `[body_index]` and rebuild") and does **not** open the
  saved search.

Tests:
- `internal/savedsearch/manager_test.go` gains
  `TestManager_ListPreservesRowOnRegexCompileError` (table-driven
  over the two sentinel errors).
- `internal/savedsearch/manager_test.go` gains
  `TestManager_SaveAcceptsRegexWhenBodyIndexEnabled` and
  `TestManager_SaveRejectsRegexWhenBodyIndexDisabled`.
- `internal/ui/app_e2e_test.go` (TUI visible-delta per CLAUDE.md
  §5) gains `TestSidebar_GreysOutRegexSavedSearchWhenIndexDisabled`
  asserting the row's render frame contains the `!` indicator and
  the help-line text.

---

## 10. UI integration

### 10.1 `/`-mode regex prefix

`/`-mode keeps its plain-text flow. A regex prefix unlocks the
regex path:

```
/regex:auth.*token=[a-f0-9]{32}_                    [regex local-only]
─────────────────────────────────────────────────────────────────
Sun 14:32  ●  notifications@vendor      Token refresh required — …auth?token=ab12…
Fri 11:08     ci@build.example.invalid  Build deploy keys — Updated auth bearer t…
```

The `regex:` prefix is recognised by the existing search-mode
dispatcher in `internal/ui/app.go` (search-mode lives inside
`app.go`, not a separate `search.go`) and mapped to a `Regex bool`
field on spec 06's `search.Query`. The Searcher's local branch
invokes `pattern.Compile` with a synthesised `~B /regex/` AST when
`Regex == true`; the server branch is **skipped** for regex queries
because Graph `$search` is token-based.

Status indicator (spec 06 §5 "UI integration" section, search status
in `internal/ui/app.go`) gains two new states:

- `[regex needs index]` when `[body_index].enabled = false`.
- `[regex local-only]` when regex is in play (server branch
  skipped).

### 10.2 `:filter` mode

`:filter ~b /auth.*token=/` is the structured form. Spec 10's
filter flow (the `;` prefix and confirmation gate) carries the
regex path through unchanged — it's just another pattern.

`:filter --explain` shows the strategy and the extracted literals.
Example for `~b /auth.*token=[a-f0-9]{32}/ & ~F`, against an Inbox
of 50 k indexed bodies:

```
$ :filter --explain ~b /auth.*token=[a-f0-9]{32}/ & ~F
Strategy: StrategyLocalRegex
Extracted literals: ["auth", "token="]
SQL: SELECT m.id, bt.content FROM messages m
     JOIN body_text bt ON bt.message_id = m.id
     JOIN body_trigram tr ON tr.rowid = bt.rowid
     WHERE m.account_id = ?
       AND m.flag_status = 'flagged'
       AND bt.content LIKE ?  -- '%auth%'
       AND bt.content LIKE ?  -- '%token=%'
     ORDER BY m.received_at DESC
     LIMIT 2000
Reason: regex predicate; trigram narrow + Go regexp post-filter.
```

(The SQL is shown literally; the test for `--explain` asserts on
exactly this string, so it doesn't drift.)

### 10.3 Command palette + `:index` cmd-bar

The command palette (spec 22) gains four rows:

- `Index — Status` (`:index status`)
- `Index — Rebuild` (`:index rebuild`)
- `Index — Evict older than…` (`:index evict --older-than=NNd`)
- `Index — Disable` (`:index disable`)

Cmd-bar verbs mirror the CLI in §11. `:index` with no subverb
prints status. CLAUDE.md §12.6 mechanical reference-doc check
requires `docs/user/reference.md` to gain rows for these.

### 10.4 No new pane and no new mode

The viewer pane is unchanged. No new sidebar entry, no new modal,
no new pane width key, no new key binding. The body index is
plumbing surfaced only via command verbs and search status.

---

## 11. CLI surface — `inkwell index`

`cmd/inkwell/cmd_index.go` adds one cobra parent with four
subcommands. Mirrors spec 14 patterns for stdout shape.

```
$ inkwell index status
Status: enabled  (config: ~/.config/inkwell/config.toml)
Indexed: 3,812 messages (cap 5,000), 427.3 MB plaintext (cap 500.0 MB)
Body LRU: 487 messages, 184.2 MB  (rebuild can only re-decode what the LRU currently caches)
Oldest:  2024-08-12 14:22
Newest:  2026-05-14 09:51
Truncated bodies: 27 (cap: 1.0 MB per message)
Folder allow-list: (empty — all subscribed folders)
```

```
$ inkwell index rebuild --folder='Clients/TIAA' --limit=2000
Walking cached bodies in folder 'Clients/TIAA' (1,847 candidates)...
Indexed 1,847 / 1,847  (avg 4.1 KB, 7.7 MB total)
Done in 12.3s.
```

```
$ inkwell index evict --older-than=90d
Evicting body_text rows with last_accessed_at < 2026-02-13.
Removed 412 rows (114.7 MB) from body_text + body_fts + body_trigram.
New total: 3,400 messages, 312.6 MB.

$ inkwell index evict --folder='Newsletters'
Evicting body_text rows scoped to folder 'Newsletters'.
Removed 1,204 rows (108.3 MB).
New total: 2,608 messages, 319.0 MB.

$ inkwell index evict --message-id=AAMkAGI2...
Removed 1 row (4.8 KB).
```

The `evict` subcommand accepts `--older-than=DURATION`,
`--folder=NAME`, and `--message-id=ID`; they compose (all-of) when
combined. All three are surfaced via the same `EvictBodyIndex(ctx,
EvictBodyIndexOpts{...})` store call (§6.1).

```
$ inkwell index disable
This will delete the local body index (~427 MB across 3,812 messages)
and disable indexing until you re-enable [body_index].enabled = true.
You can rebuild from cached bodies later with `inkwell index rebuild`.
Proceed? [y/N]: y
Index purged. [body_index].enabled remains true in your config; flip
it to false to stop future indexing on body fetches.
```

`--json` on any subcommand emits the same data as a single JSON
object on stdout, for scripting (spec 14 convention).

All four verbs are exposed at the cmd-bar (`:index <verb>`) and the
command palette (§10.3) per CLAUDE.md §12.6 mechanical-reference
trigger list.

---

## 12. Failure modes

| Scenario | Behaviour |
| --- | --- |
| `[body_index].enabled = false` and user runs `/regex:foo` | Status: `regex needs index — run 'inkwell index rebuild' first`. No server fallback (Graph can't do regex). |
| Cache for indexed message has been evicted from `bodies` | `body_text` row still present (independent eviction); search returns the hit and snippet from `body_text`. Viewer opens the body, re-fetching via the existing tier-2 path. |
| Body is truncated at `max_body_bytes` | Search hits within the first N bytes; status row tagged `[truncated]`. `inkwell index status` reports the count. |
| User regex is malformed | `pattern.Compile` returns the `regexp.Compile` error verbatim; cmd-bar / `:filter` shows column-position error per spec 08 §5.2. |
| User regex has no mandatory literal | `ErrRegexUnboundedScan` (§9.3). |
| Trigram candidates exceed `max_regex_candidates` | `ErrTooManyCandidates` (§9.3); user refines. |
| `regex_post_filter_timeout` hit | `ErrRegexTimeout`; partial results returned and a status warning is shown. |
| Rebuild interrupted (Ctrl-C, OS kill) | `body_text` rows persist in a half-finished state; next `rebuild` resumes (PK-conflict-skip). |
| `enabled true → false` toggled between runs | Next startup detects the flip (compares previous on-disk marker in `schema_meta` against current `[body_index].enabled`), calls `PurgeBodyIndex`, emits an INFO log line: `body index disabled — purged N rows (M bytes)`. No interactive prompt; this path is for the daemon. The CLI's `inkwell index disable` is the interactive surface (§8.4). |
| Folder rename (Graph) | `body_text.folder_id` is stable (it's the Graph folder ID, not the name). Allow-list re-resolves on next startup (§7.2). |
| Message moved across folders | `UpdateMessageFields` site in `internal/store/messages.go` updates `body_text.folder_id` in the same transaction. Without this patch, an indexed message moved out of an allow-listed folder would still appear in `~m Inbox & ~b foo` after the move. Regression test in `body_index_test.go`. |
| modernc.org/sqlite update breaks trigram | Probe test `fts_probe_test.go` fails CI; release blocks. The probe is the spec's canary (§3.6). |
| Server body changed but never re-opened locally | Index returns the stale match. Documented limitation (§6.5); `inkwell index rebuild --force` is the explicit refresh. |
| Saved search uses regex; user disables index later | Spec 11 picker greys out the row with `ErrRegexRequiresLocalIndex` as the help line; `Enter` shows a toast (§9.6). |

---

## 13. Test plan

All four layers of the CLAUDE.md §5 test pyramid must land before
this spec is done. Test file paths are absolute relative to repo
root.

### 13.1 Unit — `internal/store/body_index_test.go`

- `TestIndexBody_RoundTrip`: insert → read → verify FTS rows
  present in both `body_fts` and `body_trigram` (via direct table
  queries).
- `TestIndexBody_Idempotent`: re-IndexBody with same content does
  not change rowid; the `AFTER UPDATE OF content` trigger does not
  re-fire (assert by capturing FTS row counts before/after a
  no-op re-index).
- `TestEvictBodyIndex_DualCap`: row count + byte cap both enforced,
  oldest-by-`last_accessed_at` first — mirrors `EvictBodies`
  shape.
- `TestPurgeBodyIndex_ClearsAllThreeTables`: `body_text`,
  `body_fts`, `body_trigram` are empty after purge.
- `TestSearchBodyText_BM25Ordering`: order matches FTS5 BM25.
- `TestSearchBodyTrigramCandidates_LiteralsAreParameterised`:
  injection probe — the input literal `'%'; DROP TABLE body_text; --`
  survives as a literal that matches nothing and the table is
  intact afterwards.
- `TestUnindexBody_CascadesFTS`: delete by message_id; assert no
  rows remain in `body_fts` or `body_trigram` for that rowid.
- `TestMessageMove_UpdatesBodyTextFolderID`: regression for the
  §7.2 cross-folder move case.
- `TestPermanentDeleteCascades`: delete the `messages` row;
  assert the AD trigger drops the FTS rows.
- `TestSchemaVersion15`: round-trip migration; assert
  `schema_meta.version == "15"`.

### 13.2 Unit — `internal/pattern/eval_local_regex_test.go`

- `TestExtractMandatoryLiterals_PositiveCases`: table-driven set
  of regexes → expected literals.
- `TestExtractMandatoryLiterals_RefusesWithoutLiteral`:
  `/^.$/`, `/[a-z]+/`, `/.*x.*y.*/` (where x and y are <3 chars)
  → `ErrRegexUnboundedScan`.
- `TestStrategySelector_RoutesRegexToLocal`: with
  `BodyIndexEnabled = true`, regex predicates yield
  `StrategyLocalRegex`; off, yields `ErrRegexRequiresLocalIndex`
  (for body); off but `~s /regex/`, yields a working subject-only
  plan.
- `TestRegexPostFilter_Timeout`: a synthetic corpus + a
  catastrophic-backtracking regex; assert `ErrRegexTimeout` fires
  and partial result count > 0.
- `TestEmitLocal_BodyPredicateRoutingFlipsOnFlag`: with
  `BodyIndexEnabled = false`, `~b foo` emits `body_preview LIKE
  ?`; with `BodyIndexEnabled = true`, it emits the body_text JOIN
  shape — exact SQL strings asserted.

### 13.3 Unit — `internal/store/fts_probe_test.go`

The modernc probe (§3.6). Must pass at every `go test ./...` so
any upstream regression in the sqlite driver fails CI immediately.

### 13.4 Redaction —
`internal/store/body_index_redact_test.go` +
`cmd/inkwell/cmd_index_redact_test.go`

- `TestIndexer_DoesNotLogContent_AtInfo`: capture via
  `redact.NewCaptured`; assert the records carry no `content`,
  no plain message ID, no folder ID at level >= INFO.
- `TestIndexer_HashesMessageIDAtDebug`: at DEBUG, the
  `message_id` attribute is `redact.HashMessageID(id)` form, not
  the plain ID. Hash is stable across calls.
- `TestCLI_IndexStatusDoesNotPrintFolderNames`: when
  `folder_allowlist` is empty, the output contains "(empty — all
  subscribed folders)" and no folder names.
- `TestRedact_HashMessageID_OneWayAndStable`: the helper is
  deterministic and irreversible (no plaintext substring of the
  Graph ID appears in the output).

### 13.5 Integration — build-tag `integration`,
`internal/store/body_index_integration_test.go`

- Real SQLite in tmpdir.
- Insert 5 000 synthetic messages with bodies (fixtures synthesised
  in `internal/store/testfixtures.go`; `SyntheticBodies(N)` is the
  new helper).
- Run a mixed workload (10 % regex, 30 % plain `~b`, 60 %
  envelope-only filters) for 30 s; assert no `database is locked`,
  no leaked goroutines (goleak), no orphan FTS rows
  (`SELECT COUNT(*) FROM body_fts` matches `body_text`).
- Toggle `enabled true → false` mid-run; assert the next startup
  purges and the search path returns `ErrRegexRequiresLocalIndex`.

### 13.6 TUI e2e — build-tag `e2e`,
`internal/ui/regex_search_e2e_test.go`

Per CLAUDE.md §5 visible-delta rule:

- `/regex:auth.*` types into search-mode; assert the status bar
  visibly transitions to `[regex local-only]` and the first row
  in the list pane swaps to the highest-BM25 candidate.
- `:filter ~b /token=/ & ~F` opens the filter status bar and
  shows the count `Matched: 17`.
- `:index status` opens the cmd-bar, types, and the resulting
  toast contains the indexed row count.

### 13.7 Benchmarks —
`internal/store/body_index_bench_test.go` +
`internal/pattern/eval_local_regex_bench_test.go`

One benchmark per row in §14. Fixtures synthesised at test setup
(no committed binary blobs) — body fixtures are randomised English
prose at controlled lengths plus a sprinkle of HTML-like markup so
`htmlToText` exercises the strip path.

### 13.8 Property-based —
`internal/pattern/eval_local_regex_prop_test.go`

- Generate random regexes; assert literal extraction terminates
  and either yields ≥3-char literals or returns
  `ErrRegexUnboundedScan` (no panics, no infinite loops).
- For regexes with at least one literal, generate a body
  containing the literal; assert the trigram candidate query
  returns the body's rowid.

---

## 14. Performance budgets

Non-negotiable; verified by benchmark. >50 % over budget fails CI
per CLAUDE.md §5.2. All numbers are warm-buffer-cache p95 on the
dev machine baseline (M2 macOS, modernc.org/sqlite v1.50.0, Go
1.23+). Cold-start regex search latency is out of scope (mmap
warmup dominates; not a steady-state concern).

| Operation | Budget |
| --- | --- |
| `IndexBody(1 KB body)` | <3ms p95 |
| `IndexBody(10 KB body)` | <8ms p95 |
| `IndexBody(1 MB body)` | <60ms p95 |
| `SearchBodyText(q, limit=50)` over 50 k indexed bodies | <80ms p95 |
| `SearchBodyTrigramCandidates(["auth","token="], limit=2000)` over 50 k indexed bodies | <100ms p95 |
| Regex post-filter: 200 candidate × 5 KB body × moderate regex | <120ms p95 |
| Full regex search end-to-end (literal extract + candidate + post-filter), 50 k bodies | <300ms p95 |
| `EvictBodyIndex` reducing 5 000 → 4 500 rows | <500ms p95 |
| `PurgeBodyIndex` from 5 000 rows | <1 s p95 |
| `inkwell index rebuild` rebuilding 500 cached bodies from `bodies` | <5 s end-to-end (matches the default `[cache].body_cache_max_count`) |
| Cold-start overhead of `[body_index].enabled = true` | <50 ms added to TUI cold start (CLAUDE.md §6: 500 ms total) |

Sanity check: on the **default config**
(`max_count = 5 000`, `max_bytes = 500 MB`), 5 000 bodies × 4 KB
= 20 MB raw text; trigram overhead ~3× → ~60 MB index. The cap is
miles ahead of the count, intentionally — the count is the
limiting factor at default. Users who raise `max_count` toward
50 000 should also raise `max_bytes`: 50 k × 4 KB = 200 MB raw
text, ~600 MB trigram index — the byte cap becomes binding around
that range.

---

## 15. Documentation updates

### 15.1 Updates this spec lands in the same PR

| File | What to update |
| --- | --- |
| `docs/CONFIG.md` | New `[body_index]` section per §7. **Owner spec: 35**. |
| `docs/ARCH.md` | §6 — add Tier 3 ("body index, opt-in"). §7 schema table — add `body_text` row. §2 module-tree row updated if relevant. |
| `docs/PRD.md` | §10 inventory — add spec 35 row (status set at ship). |
| `docs/THREAT_MODEL.md` | "Threats and mitigations" table — new row per §8.1. "Accepted residual risks" entry. |
| `docs/PRIVACY.md` | "Where data is stored" — new row. "What data inkwell accesses" — new sentence per §8.2. |
| `docs/ROADMAP.md` | Bucket 5 row + §1.13 — set status at ship. |
| `docs/specs/06-search-hybrid.md` | §11 "Out of scope" bullet that rules out local body indexing — strike through and back-reference spec 35. (`docs/specs/06-search-hybrid.md:425` is the line.) |
| `docs/user/reference.md` | New pattern syntax (regex form on `~b`, `~B`, `~s`), `/regex:` prefix, `:index` cmd-bar verbs, CLI verbs. Update `_Last reviewed against vX.Y.Z._` footer at ship. |
| `docs/user/how-to.md` | New recipe: "Search inside bodies (and with regex)." |
| `docs/user/tutorial.md` | **Skip** — first-30-minutes path unchanged. |
| `docs/user/explanation.md` | **Skip** — design invariant unchanged. |
| `docs/plans/spec-35.md` | Tracking note (CLAUDE.md §13) created at draft time; finalised at ship. |
| `README.md` | Status table — new row for body index at ship. |
| `internal/store/AGENTS.md` | Invariant 1 amended per §6.2. |

### 15.2 Mechanical reference-check (CLAUDE.md §12.6)

This spec introduces:
- A new pattern AST shape (`/.../` regex form) — `reference.md`
  mandatory.
- A new `/`-mode prefix (`regex:`) — `reference.md` mandatory.
- A new cmd-bar verb (`:index <subverb>`) — `reference.md`
  mandatory.
- A new CLI parent + 4 subcommands — `reference.md` CLI section
  mandatory.
- Four new palette rows (§10.3) — `reference.md` mandatory.

No new key bindings, no new pane, no new mode. Existing key
bindings are unaffected.

---

## 16. Definition of done

**Spec content (CLAUDE.md §11)**
- [ ] Which Graph scope(s)? `Mail.Read` only (existing). In PRD §3.1.
- [ ] What state does it read from / write to in `store`? Reads
      `bodies` for backfill; writes `body_text` + FTS5 companions.
- [ ] What Graph endpoints does it call? **None.** Indexer is
      offline.
- [ ] How does it behave offline? Fully functional offline once
      bodies are decoded.
- [ ] What is its undo behaviour? Indexing is non-mutating w.r.t.
      mail state — there is no user-visible action to undo. CLI
      destructive ops carry the confirmation gate (§8.4).
- [ ] What error states surface to the user, and how?
      `ErrRegexUnboundedScan`, `ErrRegexRequiresLocalIndex`,
      `ErrTooManyCandidates`, `ErrRegexTimeout` — all in status
      bar / cmd-bar / `:filter` (§9.3).
- [ ] Is there a CLI-mode equivalent (PRD §5.12)? Yes — `inkwell
      index {status,rebuild,evict,disable}` and regex via
      `inkwell messages --filter '~b /.../'`.

**Tests + benchmarks**
- [ ] `go vet ./...`
- [ ] `go test -race ./...`
- [ ] `go test -tags=integration ./...`
- [ ] `go test -tags=e2e ./...`
- [ ] Every perf budget in §14 has a benchmark; passes within
      budget on dev machine (>50 % over fails per CLAUDE.md §5.2).
- [ ] Redaction tests cover the indexer and CLI sites (§13.4),
      plus `HashMessageID` (§8.5).
- [ ] Schema-version regression test sites updated
      (`tabs_test.go:40`, `sender_routing_test.go:320`).

**Security (spec 17)**
- [ ] `docs/THREAT_MODEL.md` "Threats and mitigations" row added,
      "Accepted residual risks" extended; `docs/PRIVACY.md`
      "Where data is stored" + "What data inkwell accesses"
      updated. PR description carries the spec-17 impact note (§8).
- [ ] gosec, semgrep, govulncheck green. Zero new `// #nosec`
      annotations.

**Docs**
- [ ] CLAUDE.md §12.6 doc-sweep table run in full. Every file in
      §15.1 updated in the same PR (or the immediately-following
      commit if the tag went out first).
- [ ] `internal/store/AGENTS.md` invariant 1 amended (§6.2).

---

## 17. Out of scope

- Custom FTS5 tokenizers, `sqlite-regex` loadable extension, any
  CGO dependency. Pure-Go invariant (CLAUDE.md §1) holds.
- Indexing the raw RFC 5322 header block locally. `~h` continues
  to route through Graph `$search`.
- Backfilling bodies that have never been fetched (would require
  Graph round-trips, ~hours and ~GBs of network). User opens or
  `:filter`s first; LRU then carries them into rebuild's reach.
- Per-folder retention policies beyond the allow-list and global
  caps. A v2 could add `[body_index.folders."Clients/TIAA"]`
  sub-tables; not now.
- Encrypted FTS / encrypted body_text. The CASA boundary stays
  at the FileVault + 0600 layer.
- Multi-account (PRD §6). Schema is forward-compatible
  (`account_id` on `body_text`); the executor and CLI assume one
  account in v1.
- Porter stemming for the body keyword index. Reserved for a v2
  follow-up; flipping `[body_index].stemming` in v1 is a no-op +
  warning.
- A "search history" / "recent regex queries" feature. Spec 06
  §11 already deferred this; this spec does not revisit.
- A GUI for managing the index. The CLI + cmd-bar surface is the
  authoring surface in v1, mirroring spec 32's choice for rules.
- AI / embedding-based body search (roadmap bucket 7). This spec
  builds the on-disk surface those features will consume but does
  not itself invoke any model.
