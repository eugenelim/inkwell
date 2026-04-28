# Spec 06 — Hybrid Search

**Status:** Ready for implementation.
**Depends on:** Specs 02 (FTS5 index, store), 03 (graph client). Spec 04 (TUI for search-mode UI).
**Blocks:** Spec 11 (saved searches reuse the search infrastructure).
**Estimated effort:** 2 days.

---

## 1. Goal

Implement search across the user's mailbox with two execution paths running in parallel:

1. **Local FTS5** for instant results over what's cached.
2. **Graph `$search`** for the full server-side index (covers everything, including non-cached messages).

The user gets local results in <100ms; server results merge in as they arrive (~1–3 seconds typical). For a heavy user with a deep archive, this is a meaningfully better experience than Outlook for Mac, which only does server search.

This spec covers the **plain text query** flow (`/the deck`, `:search budget review`). The structured pattern language (`~f`, `~s`, etc.) is spec 08.

## 2. Module layout

```
internal/search/
├── search.go        # public Searcher API
├── local.go         # FTS5 query construction
├── server.go        # Graph $search query construction
├── merge.go         # streaming merge of local + server results
└── highlight.go     # match-highlighting in result snippets
```

## 3. Public API

```go
package search

type Searcher interface {
    // Search runs a hybrid search. Returns a Stream that emits batches of
    // results as they become available (local first, then server merging in).
    Search(ctx context.Context, q Query) (*Stream, error)
}

type Query struct {
    Text      string         // free-text query
    FolderID  string         // optional scope; empty = all subscribed folders
    Limit     int            // max merged results to return; default from config
    LocalOnly bool           // skip server search (offline mode, manual override)
    ServerOnly bool          // skip local; rare, for testing
}

type Stream struct {
    // Updates emits each time the result set changes. The slice is the *complete*
    // current result set, not an incremental delta — UI can simply replace its view.
    Updates() <-chan []Result

    // Done closes when both branches finish. Errors emitted via Err.
    Done() <-chan struct{}
    Err() error

    // Cancel aborts both branches.
    Cancel()
}

type Result struct {
    Message  store.Message
    Snippet  string         // highlighted text fragment around the match
    Source   ResultSource   // Local | Server | Both
    Score    float64        // bm25 (FTS5) or normalized
}

type ResultSource int
const (
    Local ResultSource = iota
    Server
    Both
)

func New(store store.Store, graph graph.Client, cfg *config.Config) Searcher
```

The `Stream` design is deliberate: results flow as they arrive, the UI rerenders progressively, and there's no all-or-nothing wait. A user typing in the search input expects to see local results "immediately" (<100ms feel), even if the server query is still in flight.

## 4. Search flow

```
User types "budget review"
    │
    ▼
Searcher.Search(q)
    │
    ├─► (goroutine A) Local FTS5  ──► emit Updates after ~50ms ─┐
    │                                                            │
    └─► (goroutine B) Graph $search  ──► emit Updates as ──┐    │
                                          server returns   │    │
                                                            ▼    ▼
                                             Merge stage (combines, dedups, sorts)
                                                            │
                                                            ▼
                                                    Stream.Updates
```

Both branches run concurrently. The merge stage holds the canonical result set, updates it as either branch produces, and emits the new combined view to the channel.

### 4.1 Local branch

```go
func (s *searcher) runLocal(ctx context.Context, q Query, out chan<- []Result) {
    fts := buildFTSQuery(q.Text)
    matches, err := s.store.Search(ctx, store.SearchQuery{
        AccountID: s.accountID,
        FolderID:  q.FolderID,
        Query:     fts,
        Limit:     q.Limit,
    })
    if err != nil { /* log, send empty */ return }

    var results []Result
    for _, m := range matches {
        results = append(results, Result{
            Message: m.Message,
            Snippet: highlight(m.Match, q.Text),
            Source:  Local,
            Score:   m.BM25Score,
        })
    }
    out <- results
}
```

#### FTS5 query construction

The user's text query is passed through to FTS5 with light sanitization:

- Normalize whitespace.
- Quoted phrases preserved (`"q4 review"` → exact phrase match).
- Bare words become AND'd terms (`budget review` → `budget AND review`).
- Special FTS5 operators (`NEAR`, `OR`, `+`, `-`) preserved when typed explicitly.
- Punctuation stripped except when inside quotes.
- Operator characters (`*`, `(`, `)`) escaped if they appear in plain text contexts.

Examples:

| User input | FTS5 query |
| --- | --- |
| `budget review` | `budget AND review` |
| `"q4 review"` | `"q4 review"` |
| `bob OR alice` | `bob OR alice` |
| `meeting -declined` | `meeting NOT declined` |
| `q4*` | `q4*` (prefix search) |
| `email@domain.com` | `"email@domain.com"` (auto-quoted) |

Implementation in `internal/search/local.go`. Tested with property-based tests.

#### FTS5 columns searched

Default: all four indexed columns (`subject`, `body_preview`, `from_name`, `from_address`). User can scope:

- `from:bob` → only `from_name` and `from_address` columns.
- `subject:Q4` → only `subject` column.
- `body:deck` → only `body_preview`.

This `field:value` syntax overlays the FTS5 query and is resolved before passing to the store.

### 4.2 Server branch

```go
func (s *searcher) runServer(ctx context.Context, q Query, out chan<- []Result) {
    if q.LocalOnly { return }
    if !s.network.online() {
        return // silently skip; local handles offline case
    }

    timeout := s.cfg.Search.ServerSearchTimeout
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    graphQuery := buildGraphSearchQuery(q.Text)
    var url string
    if q.FolderID != "" {
        url = fmt.Sprintf("/me/mailFolders/%s/messages", q.FolderID)
    } else {
        url = "/me/messages"
    }
    url += fmt.Sprintf("?$search=%s&$top=%d&$select=%s",
        urlQuoteSearch(graphQuery), q.Limit, envelopeSelect)

    var results []Result
    for url != "" {
        resp, err := s.graph.Get(ctx, url)
        if err != nil {
            if errors.Is(err, context.DeadlineExceeded) {
                // Server too slow; emit what we have and stop
                return
            }
            // Other errors: log and stop server branch
            return
        }
        // Parse response, append to results
        for _, m := range resp.Messages {
            results = append(results, Result{
                Message: convertGraphMessage(m),
                Source:  Server,
                // Graph doesn't return scores; use receivedDateTime as a proxy
                Score:   float64(m.ReceivedDateTime.Unix()),
            })
        }
        url = resp.NextLink
        out <- results // emit progressively
    }
}
```

#### Graph $search constraints

Important Graph quirks to respect:

- `$search` and `$filter` are **not combinable** on `/me/messages`. If the user wants both ("from bob about budget"), the search must run via `$search="from:bob AND subject:budget"` — not split into `$search` + `$filter`.
- `$search` does not support `$orderby`. Results return in relevance order, server-decided.
- `$search` is approximate; freshly-arrived messages may not appear for a few minutes (Graph indexes asynchronously).
- `ConsistencyLevel: eventual` header is **not** required for `/me/messages` $search (it's required for directory queries). Don't add it.

The Graph search query syntax is similar to but not identical to KQL. We use these forms:

- `budget review` → matches messages containing both terms.
- `"q4 review"` → exact phrase.
- `from:bob@acme.com` → from-field match.
- `subject:Q4` → subject match.
- `received>=2026-01-01` → date range.

User-facing query syntax (FTS5 + `field:value`) is mapped to Graph's syntax in `buildGraphSearchQuery`. A query like `from:bob "q4 review"` becomes `from:bob AND "q4 review"` for both branches; the field syntax is shared.

#### URL encoding

`$search` value MUST be URL-encoded and the entire value wrapped in double quotes when it contains spaces. The encoded form: `$search="budget review"` becomes `$search=%22budget%20review%22`. A common bug — failing to quote — gives wrong results silently. Verify with a test fixture.

### 4.3 Merge stage

```go
type merger struct {
    mu      sync.Mutex
    results map[string]*Result // keyed by message ID
    order   []string           // current sort order
    out     chan []Result
    cfg     *config.Config
}

func (m *merger) add(rs []Result) {
    m.mu.Lock()
    defer m.mu.Unlock()
    for _, r := range rs {
        if existing, ok := m.results[r.Message.ID]; ok {
            // Already had it; mark Both
            existing.Source = Both
            // Prefer local snippet if available (already has highlight)
            if r.Source == Local && existing.Snippet == "" {
                existing.Snippet = r.Snippet
            }
        } else {
            cp := r
            m.results[r.Message.ID] = &cp
        }
    }
    m.resort()
    m.emit()
}

func (m *merger) resort() {
    // Sort: Both > Local > Server (presence boost),
    // then by receivedDateTime DESC.
    // Local BM25 score used as a tiebreaker within Local sources.
    ...
}
```

The merge is deduplicating: if a message is in both branches, it gets `Source: Both` and only appears once.

#### Sort policy

Default sort: **received date descending**. The most recent matching message at the top.

Why not relevance? For email triage, recency dominates. A message from 3 weeks ago is rarely what the user typed for; they wanted the recent one. Outlook for Mac sorts the same way.

Optional `--sort=relevance` flag for cases where the user really wants BM25 ordering — useful when searching a deep archive for "that thing from a year ago about X."

### 4.4 Emit cadence

The merger emits on every `add` call, but throttled at 100ms minimum between emits to avoid UI thrash. If three `add`s arrive in quick succession, the user sees one update with the union, not three flickers.

Implementation: a debouncer goroutine inside the merger that holds the latest result set and emits at most every 100ms.

## 5. UI integration

### 5.1 Entering search mode

In `NormalMode`, `/` enters `SearchMode`. The list pane shows a search input at its top:

```
/budget review_                                   [📡 searching server…]
─────────────────────────────────────────────────────────────────
Sun 14:32  ●  Bob Acme               Q4 budget review — Hey...
Fri 11:08     newsletter@vendor      Budget tools — This week's...
Mon 09:15     Alice Smith            Re: budget review — Updated...
```

As the user types, the search runs after a 200ms debounce. The status indicator on the right reflects:

- `[searching local]` while local FTS is running (rare, very brief).
- `[📡 searching server…]` while the server branch is in flight.
- `[merged: 12 local, 47 server]` when both done.
- `[local only — offline]` when offline.

`Esc` exits search mode without applying. `Enter` keeps the result set displayed and returns focus to the list (now showing the matched messages). User can then triage as normal.

### 5.2 :search command

`:search budget review` does the same thing as `/budget review`, but is more discoverable for new users and is scriptable in saved searches and rules.

### 5.3 Search across folders

By default, search scopes to the currently focused folder. To search across all subscribed folders, prefix with `:` then `search`: `:search --all budget review` or in `/` mode, append `--all`:

```
/--all budget review
```

The `--all` flag isn't visible in the typical flow; it's an advanced option documented in `:help search`.

### 5.4 Result selection

After search results are displayed, the user navigates with `j`/`k`, opens with `Enter`. Selected message renders in the viewer pane as normal. Search context is preserved — pressing `Esc` after closing the viewer returns to the search results, not the original folder view.

## 6. Performance characteristics

### Latency budgets

| Operation | Target |
| --- | --- |
| First local result emission after query | <100ms |
| First server result emission | <2s typical, <5s p95 |
| Full local result set (100 matches over 100k messages) | <200ms |
| Server timeout (config) | 5s default |
| Total search wall-clock for typical query | <3s |

### Throughput

A single search consumes roughly:
- 1 SQLite query
- 1 Graph call (or several if paginating; usually 1 page suffices for `$top=200`)
- O(N) memory where N = result count

This is not a heavy operation. Throttling concerns negligible.

## 7. Configuration

This spec owns the `[search]` section. Full reference in `CONFIG.md`.

| Key | Default | Used in § |
| --- | --- | --- |
| `search.local_first` | `true` | §4 (parallel branches) |
| `search.server_search_timeout` | `"5s"` | §4.2 |
| `search.default_result_limit` | `200` | §3 (Query.Limit fallback) |

New keys this spec adds:

| Key | Default | Used in § |
| --- | --- | --- |
| `search.debounce_typing` | `"200ms"` | §5.1 |
| `search.merge_emit_throttle` | `"100ms"` | §4.4 |
| `search.default_sort` | `"received_desc"` | §4.3 (sort policy) |

## 8. Failure modes

| Scenario | Behavior |
| --- | --- |
| Local FTS5 returns no matches | Show only server results; no error. |
| Server returns no matches | Show only local results; status shows `[local matches only]`. |
| Both return no matches | Empty result list with `No matches.` message in pane. |
| Server times out (>5s) | Show whatever the server emitted before timeout; status shows `[server slow; partial results]`. |
| Server returns 429 (throttled) | Throttle transport waits and retries; if still failing, show local-only with `[server throttled]`. |
| Network down | Skip server branch silently; status shows `[offline; local only]`. |
| FTS query is malformed (e.g., unclosed quote) | Show error in status: `Search syntax error: <reason>`. Do not run search. |
| User types a single character | Don't search until 2+ characters typed; reduces wasted load. |
| Result set exceeds Limit | Truncate; show `[showing first 200 of many results — refine query]`. |

## 9. Test plan

### Unit tests

- `buildFTSQuery`: table-driven tests over query → expected FTS5 string.
- `buildGraphSearchQuery`: same for Graph syntax.
- URL encoding of `$search` values: assert quoting and percent-encoding.
- Merge dedup correctness: feed local and server result sets with overlaps; assert single Result per message ID with Source=Both.
- Sort policy: results in expected order under various score/date combinations.

### Integration tests

- `httptest.Server` mock returning canned `$search` responses; assert merger produces expected stream.
- Local-only mode: assert server branch never called.
- Timeout simulation: server delays beyond timeout; assert partial results emitted.
- Offline simulation: assert server branch skipped without erroring.

### Manual smoke tests

1. Search for a recent term; results appear quickly.
2. Search for an old term not in cache; server fills in results.
3. Search for a term in only the cache; local-only results.
4. Search across all folders with `--all`; results from multiple folders.
5. Type quickly into search box; debounce works (no thrash).
6. Disconnect network; search; only local results, clear status.

## 10. Definition of done

- [ ] `internal/search/` package compiles, passes unit tests.
- [ ] `/` and `:search` commands work end-to-end against real tenant.
- [ ] Hybrid streaming verified: local results appear before server results consistently.
- [ ] Throttling and timeouts honored; partial results emitted.
- [ ] Result merging correctness verified with overlap test.
- [ ] FTS5 search latency budget met on a 100k-message synthetic corpus.
- [ ] All failure modes in §8 produce sensible UX.

## 11. Out of scope for this spec

- Structured pattern queries (`~f bob`, `~d <30d`, etc.) — spec 08.
- Saved searches as virtual folders — spec 11.
- Searching message bodies (not just previews) locally — would require local body indexing, which we don't do (bodies LRU-evict; index would be unstable). Server-side $search covers full body.
- Search history / recall. Not in v1.
- Boolean expression UI (a query builder). Power users use the pattern language (spec 08); casual users use plain text.
