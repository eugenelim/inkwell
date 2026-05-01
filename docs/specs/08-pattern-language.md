# Spec 08 — Pattern Language

**Status:** In progress (CI scope, v0.5.x). Lexer + parser + AST + local-SQL evaluator (`internal/pattern/eval_local.go`) all shipped covering 14 of 18 operators; the `~h` header operator is server-only and explicitly rejected today. Residual: spec §6 Compile/Execute API, server-side `$filter` / `$search` evaluators, two-stage execution, `--explain` output, strategy selection table — all tracked under audit-drain PR 9.
**Depends on:** Specs 02 (store), 03 (graph). Independent of UI.
**Blocks:** Specs 10 (bulk operations consume Compiled patterns), 11 (saved searches store pattern strings).
**Estimated effort:** 2–3 days.

---

## 1. Goal

Define and implement the Mutt-inspired pattern language used to select messages for bulk operations and saved searches. Patterns compile to three execution targets:

1. **Graph `$filter`** — server-side OData filter expression.
2. **Graph `$search`** — server-side full-text search.
3. **Local SQL predicate** — over the SQLite cache, including FTS5.

The compiler picks the right target(s) automatically based on what the pattern asks for and what each backend supports.

This is the **selection** layer; spec 10 handles the **action** layer (filter mode, the `;` prefix, dry-run, confirmation).

## 2. Module layout

```
internal/pattern/
├── lexer.go         # tokenization
├── parser.go        # token stream → AST
├── ast.go           # AST node types
├── eval_local.go    # AST → SQL WHERE clause + args
├── eval_filter.go   # AST → Graph $filter string (or unsupported error)
├── eval_search.go   # AST → Graph $search string (or unsupported error)
├── compile.go       # public Compile() that picks execution strategy
└── execute.go       # public Execute() that runs the strategy and returns IDs
```

## 3. Language design

### 3.1 Operators

Drawn from Mutt with light extensions. Each operator takes a single argument:

| Operator | Field | Argument | Example |
| --- | --- | --- | --- |
| `~f` | from | email or wildcard | `~f bob@acme.com` `~f newsletter@*` |
| `~t` | to (recipients) | email or wildcard | `~t eu.gene@example.invalid` |
| `~c` | cc | email or wildcard | `~c ceo@*` |
| `~r` | recipient (to OR cc) | email or wildcard | `~r alice@` |
| `~s` | subject | text or "quoted phrase" | `~s budget` `~s "Q4 review"` |
| `~b` | body | text (server-side only) | `~b "action required"` |
| `~B` | subject OR body | text | `~B forecast` |
| `~d` | received date | date expression | `~d <30d` `~d >=2026-01-01` |
| `~D` | sent date | date expression | `~D last-week` |
| `~A` | has attachments | (no arg) | `~A` |
| `~N` | unread (new) | (no arg) | `~N` |
| `~F` | flagged | (no arg) | `~F` |
| `~U` | read | (no arg) | `~U` (negation of `~N`) |
| `~G` | category | category name | `~G Work` |
| `~i` | importance | low/normal/high | `~i high` |
| `~y` | inference class | focused/other | `~y focused` |
| `~v` | conversation | conversation id | `~v <id>` (rare; for thread targeting) |
| `~m` | mailbox/folder | folder name or path | `~m Inbox` `~m Clients/TIAA` |
| `~h` | header (raw) | header-name:value | `~h list-id:newsletter` (server-only) |

### 3.2 Boolean composition

Standard precedence: `!` > `&` > `|`. Parens override.

| Syntax | Meaning |
| --- | --- |
| `~f bob ~s budget` | implicit AND between consecutive operators |
| `~f bob & ~s budget` | explicit AND (same as above) |
| `~f bob \| ~f alice` | OR |
| `! ~N` | negation (NOT new) |
| `(~f bob \| ~f alice) ~A` | grouped: from-bob-or-alice AND has-attachments |

The implicit-AND form is preferred for readability. Mutt-trained users will type it that way naturally.

### 3.3 Wildcards in addresses

`~f newsletter@*` matches any address starting with `newsletter@`. `~f *@vendor.com` matches any address from that domain. `~f *@*acme*` is a contains-match.

Wildcards desugar to:
- `*` at the start → suffix match (translates to `endswith` in OData, `LIKE '%foo'` in SQL).
- `*` at the end → prefix match (`startswith`, `LIKE 'foo%'`).
- `*` in the middle or both ends → contains match (`contains`, `LIKE '%foo%'`).
- No `*` → exact match (`eq`).

Multiple `*` in unusual positions (`a*b*c`) are not supported in v1; raise a parse error.

### 3.4 Date expressions

Date arguments to `~d` and `~D` accept several forms:

| Form | Meaning |
| --- | --- |
| `<30d` | within the last 30 days (received >= now - 30d) |
| `>30d` | older than 30 days |
| `<=24h` | within last 24 hours |
| `>2026-01-01` | after Jan 1 2026 (00:00 UTC) |
| `>=2026-01-01` | on or after |
| `<2026-04-01` | before |
| `2026-03..2026-04` | range, both ends inclusive on day boundaries |
| `today` | today (local timezone) |
| `yesterday` | yesterday |
| `this-week`, `last-week` | named ranges |
| `this-month`, `last-month` | named ranges |

Date parsing in `internal/pattern/dates.go`. Timezone for "today" / "yesterday" comes from mailbox settings (or `[calendar].time_zone` override).

Relative durations support `s`, `m`, `h`, `d`, `w`, `mo`, `y` (where `m` is minutes, `mo` is months). Most common: `d` and `h`.

### 3.5 Examples

| Pattern | Meaning |
| --- | --- |
| `~f newsletter@*` | All messages from any newsletter@ sender |
| `~f *@spam.com & ~d <90d` | From that domain in the last 90 days |
| `~s "Q4 review" \| ~s "Q4 forecast"` | Subject contains either phrase |
| `~U & ~A & ~d >180d` | Read messages with attachments older than 180 days |
| `~G Newsletters & ~U` | Read items in the Newsletters category |
| `~m "Clients/TIAA" & ~F` | Flagged messages in TIAA folder |
| `! ~r eu.gene@example.invalid` | Messages where I'm not a recipient (BCC, mailing list) |
| `~y other & ~d <7d` | This week's "Other" inference messages |

## 4. Lexer

Token types:

```go
type tokenKind int
const (
    tokOperator   tokenKind = iota  // ~f, ~s, etc.
    tokIdentifier                    // bareword: bob, archive
    tokString                        // "quoted phrase"
    tokDateExpr                      // raw date arg, parsed by date subparser
    tokAnd                           // & or implicit
    tokOr                            // |
    tokNot                           // !
    tokLParen
    tokRParen
    tokEOF
)
```

The lexer is small (~150 LOC). Handles:

- Whitespace as separators.
- `~X` as an operator atom (single letter; case-sensitive: `~F` ≠ `~f`).
- Quoted strings with `\"` escape.
- Bare identifiers including dots, hyphens, `@`, and `*` (so `bob.smith@vendor.com` is a single token).
- The boolean operators `&`, `|`, `!`.

Date arguments are not lexed specially; they're picked up as identifiers and parsed by the date subparser when the operator is `~d` or `~D`.

## 5. Parser

Standard recursive-descent precedence parser:

```
expr        = or_expr
or_expr     = and_expr ('|' and_expr)*
and_expr    = not_expr (('&' | implicit) not_expr)*
not_expr    = '!' not_expr | atom
atom        = '(' expr ')' | predicate
predicate   = OPERATOR (STRING | IDENTIFIER)?   -- some operators take no arg
```

Implicit AND: when two `not_expr`s are juxtaposed without an explicit operator between them, treat as AND.

### 5.1 AST

```go
type Node interface{ isNode() }

type AndNode  struct{ Left, Right Node }
type OrNode   struct{ Left, Right Node }
type NotNode  struct{ Inner Node }

type FromPred       struct{ AddressMatch }
type ToPred         struct{ AddressMatch }
type CcPred         struct{ AddressMatch }
type RecipientPred  struct{ AddressMatch }       // To or Cc
type SubjectPred    struct{ TextMatch }
type BodyPred       struct{ TextMatch }
type SubjectBodyPred struct{ TextMatch }
type DateReceivedPred struct{ DateMatch }
type DateSentPred     struct{ DateMatch }
type HasAttachmentsPred struct{}
type UnreadPred  struct{}
type ReadPred    struct{}
type FlaggedPred struct{}
type CategoryPred struct{ Name string }
type ImportancePred struct{ Level string }       // low/normal/high
type InferencePred  struct{ Class string }       // focused/other
type FolderPred     struct{ NameMatch }          // exact or prefix
type ConversationPred struct{ ID string }
type HeaderPred       struct{ Name, Value string }
```

`AddressMatch`, `TextMatch`, `NameMatch` carry the value plus a wildcard kind:

```go
type matchKind int
const (
    matchExact matchKind = iota
    matchPrefix
    matchSuffix
    matchContains
)

type AddressMatch struct {
    Value string
    Kind  matchKind
}
```

`DateMatch`:

```go
type DateMatch struct {
    Op    dateOp        // lt, le, gt, ge, eq, range
    Value time.Time     // for single-bound
    From  time.Time     // for range
    To    time.Time     // for range
}
```

### 5.2 Parser errors

Errors include the column position. Examples:

```
~f bob @ acme    →   col 7: unexpected '@' after value 'bob'; did you mean ~f bob@acme?
~s "unclosed     →   col 4: unterminated quoted string
~d not-a-date    →   col 4: unrecognized date expression 'not-a-date'
(~f bob          →   col 8: missing closing ')'
~z something     →   col 1: unknown operator '~z'
```

Errors are surfaced to the user verbatim in the command line (`:filter <expr>` displays the error in the status line; the filter does not apply).

## 6. The Compile and Execute API

```go
package pattern

type Compiled struct {
    AST      Node
    Strategy ExecutionStrategy
    Plan     CompilationPlan
}

type ExecutionStrategy int
const (
    StrategyLocalOnly ExecutionStrategy = iota   // pure local SQL
    StrategyServerFilter                         // pure Graph $filter
    StrategyServerSearch                         // pure Graph $search
    StrategyServerHybrid                         // server $filter + $search combined
    StrategyTwoStage                             // server query for candidate set, local filter for refinement
)

type CompilationPlan struct {
    LocalSQL    string             // empty if not local-stage
    LocalArgs   []any
    GraphFilter string             // empty if not used
    GraphSearch string             // empty if not used
    GraphFolderID string           // optional folder scope
    Notes       []string           // human-readable strategy explanation, for --explain
}

// Compile parses and analyzes a pattern, choosing the best execution strategy
// given what each backend supports.
func Compile(src string, opts CompileOptions) (*Compiled, error)

type CompileOptions struct {
    DefaultFolderID string       // default scope if pattern has no ~m
    IncludeArchive  bool         // search archived folders too
    LocalOnly       bool         // force local-only (offline)
    PreferLocal     bool         // bias toward local when ambiguous
}

// Execute runs the compiled pattern and returns matching message IDs.
func Execute(ctx context.Context, c *Compiled, store store.Store, graph graph.Client) ([]string, error)
```

## 7. Strategy selection

The compiler analyzes the AST and picks a strategy based on which predicates appear and which backends can satisfy them.

### 7.1 What each backend supports

| Predicate | Local SQL | Graph $filter | Graph $search |
| --- | --- | --- | --- |
| `~f` exact | ✓ | ✓ | ✓ |
| `~f` prefix/suffix/contains | ✓ | ✓ (`startswith`/`endswith`/`contains`) | ✓ (limited) |
| `~t` / `~c` exact | ✓ | ✓ via `any()` collection filter | ✓ |
| `~t` wildcard | ✓ | partial (collection filter limitations) | ✓ |
| `~s` exact text | ✓ FTS5 | ✓ contains | ✓ |
| `~s` quoted phrase | ✓ FTS5 | partial | ✓ |
| `~b` body match | ✗ (only previews indexed) | ✗ | ✓ |
| `~B` subject OR body | ✗ (body not local) | partial | ✓ |
| `~d` / `~D` | ✓ | ✓ | partial (`received>=` syntax) |
| `~A` | ✓ | ✓ (`hasAttachments eq true`) | partial |
| `~N` / `~U` / `~F` | ✓ | ✓ | ✗ |
| `~G` (category) | ✓ via JSON | ✓ via collection filter | ✓ (`category:`) |
| `~i` / `~y` | ✓ | ✓ | ✗ |
| `~m` (folder scope) | ✓ | folder scope is in the URL, not $filter | folder scope is in URL |
| `~h` (header) | ✗ | ✗ (Graph `internetMessageHeaders` not in $filter) | ✓ |

### 7.2 Decision tree

```
1. If pattern uses ~b, ~B, or ~h:
       → MUST use server. Specifically Graph $search (only path that searches body / headers).
       → If pattern ALSO uses predicates not supported by $search (~N, ~U, ~F, ~i, ~y, complex wildcards):
           → StrategyTwoStage: $search returns candidate IDs; local SQL filters them.
       → Else:
           → StrategyServerSearch.

2. Else if LocalOnly is forced (offline) OR PreferLocal AND all predicates have a local execution path:
       → StrategyLocalOnly.

3. Else if pattern fits cleanly in $filter:
       → StrategyServerFilter.
       → Why prefer server: it sees the full mailbox, not just cached.

4. Else (pattern uses some $filter predicates and some ~s text matching):
       → StrategyServerHybrid: combine $filter for the structural part and $search for the text part. Note: Graph forbids $filter+$search combination on /me/messages, so we route $search-eligible parts through /me/messages?$search and $filter-eligible parts through a separate call, then INTERSECT in memory. (See §7.3.)

5. Fallback: StrategyLocalOnly with a warning that results are limited to cached content.
```

The chosen strategy is recorded in `Compiled.Plan.Notes` and surfaced via `:filter --explain <expr>`:

```
$ :filter --explain ~f newsletter@* & ~U
Strategy: StrategyServerFilter
Graph URL: GET /me/messages?$filter=startswith(from/emailAddress/address,'newsletter@') and isRead eq true
Reason: All predicates satisfiable by Graph $filter. Server query covers full mailbox.
```

### 7.3 Server hybrid: avoiding the $filter+$search ban

Graph blocks combining `$filter` and `$search` on `/me/messages`. For `~f bob & ~b "action required"`:

- We can't do `$filter=from eq 'bob'&$search="action required"` (rejected).
- Workaround: run `$search="from:bob action required"` — Graph's $search syntax accepts the field-prefix form. This is what we do for the simple case.
- If the structural part is not expressible in $search syntax (e.g., `~A` or `~i high`), we run two queries: one with `$filter`, one with `$search`, intersect the IDs in memory.

Two-query intersection cost: 2 round-trips. Acceptable for an interactive bulk operation.

## 8. Local SQL generation

`eval_local.go` walks the AST and produces a parameterized WHERE clause. Examples:

| Pattern | SQL fragment | Args |
| --- | --- | --- |
| `~f bob@acme.com` | `from_address = ?` | `["bob@acme.com"]` |
| `~f newsletter@*` | `from_address LIKE ?` | `["newsletter@%"]` |
| `~s budget` | `rowid IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)` | `["budget"]` |
| `~d <30d` | `received_at >= ?` | `[<unix_timestamp_30d_ago>]` |
| `~A` | `has_attachments = 1` | — |
| `~N` | `is_read = 0` | — |
| `~F` | `flag_status = 'flagged'` | — |
| `~G Work` | `EXISTS (SELECT 1 FROM json_each(messages.categories) WHERE value = ?)` | `["Work"]` |
| `~m Inbox` | `folder_id IN (SELECT id FROM folders WHERE display_name = ? OR well_known_name = ?)` | `["Inbox","inbox"]` |
| `~r eu.gene@example.invalid` | `(EXISTS (SELECT 1 FROM json_each(messages.to_addresses) ja WHERE json_extract(ja.value, '$.address') = ?) OR EXISTS (SELECT 1 FROM json_each(messages.cc_addresses) ja WHERE json_extract(ja.value, '$.address') = ?))` | `["eu.gene@example.invalid","eu.gene@example.invalid"]` |

Boolean composition wraps fragments with `AND`, `OR`, `NOT (...)`.

The full query template:

```sql
SELECT id FROM messages
WHERE account_id = ?
  AND <generated_predicate>
ORDER BY received_at DESC
LIMIT ?
```

`LIMIT` defaults to 5000 to bound memory. Bulk operations that genuinely need to operate on more rows can override; spec 10 documents this.

### 8.1 FTS interaction

When a pattern includes a text predicate (`~s`, `~B`, `~G` with multi-term value), the SQL uses `messages_fts MATCH` for the text part and joins to `messages` for structural predicates:

```sql
SELECT m.id FROM messages m
JOIN messages_fts fts ON fts.rowid = m.rowid
WHERE m.account_id = ?
  AND fts.subject MATCH ?      -- text predicate
  AND m.is_read = 0             -- structural
  AND m.received_at >= ?
```

The FTS5 query must be a single clause; multiple text predicates compose via FTS5's own operators (`AND`, `OR`, `NOT`) within the MATCH expression. Boolean structure across both kinds of predicates is the SQL layer's job.

## 9. Graph $filter generation

`eval_filter.go` produces an OData $filter expression. Examples:

| Pattern | $filter |
| --- | --- |
| `~f bob@acme.com` | `from/emailAddress/address eq 'bob@acme.com'` |
| `~f newsletter@*` | `startswith(from/emailAddress/address,'newsletter@')` |
| `~f *@vendor.com` | `endswith(from/emailAddress/address,'@vendor.com')` |
| `~s budget` | `contains(subject,'budget')` |
| `~d <30d` | `receivedDateTime ge 2026-03-28T00:00:00Z` |
| `~A` | `hasAttachments eq true` |
| `~N` | `isRead eq false` |
| `~F` | `flag/flagStatus eq 'flagged'` |
| `~G Work` | `categories/any(c:c eq 'Work')` |
| `~r eu.gene@example.invalid` | `toRecipients/any(r:r/emailAddress/address eq 'eu.gene@example.invalid') or ccRecipients/any(r:r/emailAddress/address eq 'eu.gene@example.invalid')` |

Bool composition: `and`, `or`, `not`. Parentheses for grouping.

String literals must single-quote-escape (`'` → `''`). The generator handles this.

### 9.1 Unsupported by $filter

If the AST includes any predicate that $filter cannot express, `eval_filter` returns `ErrUnsupported`. The compiler then tries `eval_search` or falls back to TwoStage.

Unsupported in $filter, in v1:
- `~b` (Graph does not allow $filter on body content).
- `~B` (subject OR body — same reason).
- `~h` (raw header match).
- Some recipient-collection wildcards Graph rejects with cryptic errors. We test these and route to local instead.

## 10. Graph $search generation

`eval_search.go` produces the value for `?$search="..."`.

| Pattern | $search value |
| --- | --- |
| `~s budget` | `subject:budget` |
| `~b "action required"` | `body:"action required"` |
| `~B forecast` | `forecast` (default-field search hits subject + body) |
| `~f bob@acme.com` | `from:bob@acme.com` |
| `~r eu.gene@example.invalid` | `to:eu.gene@example.invalid OR cc:eu.gene@example.invalid` |
| `~A` | `hasattachment:true` |
| `~G Work` | `category:Work` |
| `~d <30d` | `received>=2026-03-28` |
| `~h list-id:newsletter` | `list-id:newsletter` (raw header search) |

Bool composition: `AND`, `OR`, `NOT`. Spaces between tokens are implicit AND.

Quotation rules:
- Multi-word values must be quoted: `subject:"Q4 review"`.
- The entire $search value is then wrapped in additional outer quotes for URL transport.

### 10.1 Unsupported by $search

- `~N`, `~U`, `~F` — there's no `isread:false` in $search syntax.
- `~i`, `~y` — no field for importance or inference class.
- Negation of structural fields (`!~A`) — partial; `NOT hasattachment:true` works in some cases but is unreliable.

## 11. TwoStage execution

When neither pure-server nor pure-local works, we run server first to get a candidate set, then local filter to refine.

```go
func executeTwoStage(ctx context.Context, c *Compiled, store store.Store, graph graph.Client) ([]string, error) {
    // 1. Run server query to get candidate IDs
    serverIDs, err := executeServer(ctx, c.Plan.GraphFilter, c.Plan.GraphSearch, graph)
    if err != nil { return nil, err }

    // 2. Local filter refinement
    // We need the cached envelopes to apply the structural predicates.
    // Bulk-fetch them from store.
    msgs, err := store.GetMessagesByIDs(ctx, serverIDs)
    if err != nil { return nil, err }

    var matched []string
    for _, m := range msgs {
        if evaluateInMemory(c.AST, &m) {
            matched = append(matched, m.ID)
        }
    }

    // Some serverIDs may not be in our local cache (deep archive). Those are
    // dropped — the user can :backfill the relevant folder if they want them.
    return matched, nil
}
```

`evaluateInMemory` is a Go-level interpreter of the AST against an in-memory `Message`. It's used:
- In TwoStage refinement.
- In `~b`/`~B` cases where the body has been fetched (rare; spec 10 will not pre-fetch bodies for filtering).

### 11.1 The "deep archive" gap

If a user runs `~b "magic phrase" & ~F` (body match AND flagged):
- `~b` → goes via server $search.
- `~F` → server $search can't express flagged.
- TwoStage: server returns IDs of messages with the body phrase, local filters for flagged.
- If a flagged message with the phrase exists ONLY in the deep archive (not cached locally), we miss it.

This is documented limitation. The user can run `:backfill` first to widen the cache, or accept that some matches require the cache to know about them.

## 12. The execution interface from spec 10's perspective

Spec 10 (bulk operations) consumes `Compile` and `Execute` as a black box:

```go
c, err := pattern.Compile(userInput, opts)
if err != nil {
    showError(err); return
}
ids, err := pattern.Execute(ctx, c, store, graph)
if err != nil { ... }
// ids is now the message IDs to operate on
```

Spec 11 (saved searches) likewise:

```go
saved := loadSavedSearch("Newsletters")
c, _ := pattern.Compile(saved.Pattern, opts)
ids, _ := pattern.Execute(ctx, c, store, graph)
// virtual folder content
```

## 13. Configuration

This spec owns no `[section]` of its own. It's a library used by other specs.

But it consumes:

| Key | Owner spec | Used in § |
| --- | --- | --- |
| `[search].server_search_timeout` | 06 | §7 (server-side execution timeout) |
| `[calendar].time_zone` | 12 | §3.4 (timezone for "today" / relative dates) |

New keys this spec adds:

| Key | Default | Used in § |
| --- | --- | --- |
| `pattern.local_match_limit` | `5000` | §8 (LIMIT on local SQL) |
| `pattern.server_candidate_limit` | `1000` | §11 (cap on TwoStage server candidate set; beyond that, refuse and tell user to refine) |
| `pattern.prefer_local_when_offline` | `true` | §7.2 |

## 14. Error handling

| Scenario | Behavior |
| --- | --- |
| Parse error | `Execute` not called; user sees error from `Compile`. |
| Date arg unparseable | `Compile` returns specific date error with column. |
| Pattern combines unsupported predicates everywhere | `Compile` returns `ErrPatternUnsupported` with explanation. |
| Server query fails mid-execution | `Execute` returns error; spec 10 surfaces "could not run pattern: <reason>". |
| Server returns more than `server_candidate_limit` | `Execute` returns `ErrTooManyCandidates`; user must refine pattern. |
| TwoStage finds candidates not in local cache | Silently dropped from result; debug log records count. |
| FTS5 reports syntax error | Wrap as `ErrPatternUnsupported` with original FTS error. |

## 15. Test plan

### Unit tests

- **Lexer:** every operator + composition, with positions verified.
- **Parser:** AST structure for canonical patterns + error cases (table-driven).
- **Date parser:** all forms in §3.4, including timezone edge cases.
- **eval_local:** SQL output snapshots for ~50 representative patterns.
- **eval_filter:** $filter output snapshots.
- **eval_search:** $search output snapshots.
- **Wildcard desugaring:** `*foo*`, `*foo`, `foo*`, `foo`, `*foo*bar*` (error).
- **Strategy selection:** for each pattern shape, assert chosen strategy. This is the most important test set.

### Integration tests

- Mock Graph; run patterns end-to-end; assert correct ID set.
- Run TwoStage flow; assert refinement filters correctly.
- Run with offline (no graph) and assert LocalOnly fallback.

### Property-based tests

- Random AST generation → compile → assert it produces *some* strategy without panic.
- Round-trip: generate AST → render to source → re-parse → assert AST equality.

### Hand-verified golden tests

A `testdata/patterns.txt` file with ~30 real patterns, each annotated with expected:
- Strategy
- Local SQL
- Graph $filter or $search

Maintained alongside the parser; any change requires updating expected outputs and reviewing.

## 16. Performance budgets

| Operation | Target |
| --- | --- |
| Compile (parse + plan) | <5ms |
| Execute, LocalOnly, returns 1k IDs | <50ms |
| Execute, ServerFilter, single page (1k results) | <2s |
| Execute, ServerHybrid (two queries) | <4s |
| Execute, TwoStage with 1k candidates | <2s server + <100ms local |

The compiler is dominated by the planner's predicate-by-predicate analysis. The executor is dominated by network for server strategies and SQLite for local.

## 17. Definition of done

- [ ] `internal/pattern/` package compiles, all tests pass.
- [ ] All 18 operators from §3.1 implemented.
- [ ] All date forms from §3.4 parse correctly.
- [ ] Strategy selection table-driven test passes for ≥30 patterns.
- [ ] `--explain` output is human-readable for at least 10 sample patterns.
- [ ] Property-based parser tests pass on 10k random ASTs.
- [ ] Performance budgets met on a 100k-message synthetic corpus.

## 18. Out of scope for this spec

- The `:filter` / `:search` UI (spec 10).
- The bulk-action `;` prefix (spec 10).
- Saved-search storage and re-evaluation (spec 11).
- Pattern composition operators we don't ship in v1: parent thread (`~v` is supported; deeper thread navigation isn't), regex on body (Graph $search is token-based, not regex).
- Sorting controls in pattern syntax. We always sort by `received_at DESC` for now; future syntax `~o sender` for sort-by-sender can be added.
