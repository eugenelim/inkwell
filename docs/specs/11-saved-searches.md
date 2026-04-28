# Spec 11 — Saved Searches as Virtual Folders

**Status:** Ready for implementation.
**Depends on:** Specs 02 (saved_searches table), 04 (folders pane in sidebar), 06 (search infrastructure), 08 (pattern compile/execute), 10 (filter UX).
**Blocks:** Nothing.
**Estimated effort:** 1–2 days.

---

## 1. Goal

Let users define named patterns ("Newsletters", "Needs Reply", "From CEO") that appear as virtual folders in the sidebar. Selecting one runs the underlying pattern and renders results just like a real folder. This is a productivity multiplier: instead of building Outlook server-side rules, the user gets fast, client-side, instantly-editable views over their mailbox.

## 2. Module layout

```
internal/savedsearch/
├── savedsearch.go     # public Manager API
├── store.go           # CRUD over saved_searches table + saved_searches.toml
├── evaluator.go       # invoke pattern + render results
└── refresh.go         # background re-evaluation policy
```

The folders pane (spec 04) gains a "Saved Searches" section. The list pane (spec 04) gains a virtual-folder render mode.

## 3. Public API

```go
package savedsearch

type Manager interface {
    // List returns all saved searches for the active account, ordered by sort_order then name.
    List(ctx context.Context) ([]SavedSearch, error)

    // Get retrieves one by name.
    Get(ctx context.Context, name string) (*SavedSearch, error)

    // Save creates or updates. The pattern is parsed and validated.
    Save(ctx context.Context, s SavedSearch) error

    // Delete removes by ID.
    Delete(ctx context.Context, id int64) error

    // Evaluate runs the named saved search and returns matching message IDs.
    // Cached for cache_ttl from config; force=true bypasses cache.
    Evaluate(ctx context.Context, name string, force bool) (*EvalResult, error)

    // Pinned returns the subset shown in the sidebar (pinned == true).
    Pinned(ctx context.Context) ([]SavedSearch, error)
}

type SavedSearch struct {
    ID         int64
    Name       string
    Pattern    string         // raw pattern source
    Pinned     bool           // shown in sidebar
    SortOrder  int
    CreatedAt  time.Time
}

type EvalResult struct {
    MessageIDs []string
    Count      int
    EvaluatedAt time.Time
    Strategy   pattern.ExecutionStrategy
    FromCache  bool
}

func New(store store.Store, patternEngine pattern.Engine, cfg *config.Config) Manager
```

## 4. Storage

The `saved_searches` table from spec 02 is the source of truth. A read-only mirror is written to `~/.config/inkwell/saved_searches.toml` after every save, for version-control friendliness:

```toml
[[search]]
name = "Newsletters"
pattern = '~f newsletter@* | ~f noreply@*'
pinned = true
sort_order = 1

[[search]]
name = "Needs Reply"
pattern = '~r eu.gene@example.invalid & ~U & ~d <14d & ! ~G Auto'
pinned = true
sort_order = 2

[[search]]
name = "Old Heavy Mail"
pattern = '~A & ~d >180d'
pinned = false
sort_order = 3
```

The TOML mirror is one-way: writes to it on save, but the database is authoritative. If a user manually edits the TOML and starts the app, the loader detects mismatch and prompts: "Saved searches in config differ from database. [k]eep DB / [r]eplace from config / [m]erge?"

This avoids the common "I git-pulled my dotfiles and now my settings are wrong" problem while still being transparent and editable.

## 5. UI integration

### 5.1 Sidebar rendering

The folders pane gets a new section below regular folders:

```
▾ Mail
  Inbox          47
  Drafts         3
  Sent
  Archive
▸ Clients
  Newsletters
▾ Saved Searches
  ☆ Newsletters    247
  ☆ Needs Reply     8
  ☆ From CEO        12
```

A leading `☆` glyph distinguishes saved searches from real folders. Selected saved searches highlight identically. The count is the last-evaluated match count (refreshed per §6).

### 5.2 Selecting a saved search

Pressing Enter on a saved search:

1. Triggers `Manager.Evaluate(name, force=false)`.
2. The list pane displays the matched messages as if they were a folder's content.
3. The list pane top shows `[saved: Newsletters]` instead of a folder name.
4. Sort order: `received_at DESC` by default.

The user can navigate, open, triage, and bulk-act normally. Triage actions affect the underlying messages (which remain in their actual folders); they're not "moving" them out of the saved search per se — but if the action makes them no longer match the pattern, they vanish from the saved search view (which is correct).

### 5.3 Editing a saved search

Triggered by `e` keybinding on the focused saved search, or via `:rule edit <name>`:

```
   ╭─────────────────────────────────────────────────────────────╮
   │  Edit saved search: Newsletters                              │
   │                                                              │
   │  Name:    Newsletters_                                       │
   │  Pattern: ~f newsletter@* | ~f noreply@*_                    │
   │  Pinned:  [✓] show in sidebar                                │
   │                                                              │
   │  [Esc] cancel   [Enter] save   [t] test pattern              │
   ╰─────────────────────────────────────────────────────────────╯
```

Pressing `t` runs the pattern in dry-run mode, showing a count and sample without saving. This lets the user iteratively refine before committing.

### 5.4 Creating a new saved search

Three entry points:

1. **From an existing filter**: with a `:filter <pattern>` active, run `:rule save <name>`. The current pattern becomes the saved search.
2. **From scratch**: `:rule new <name>` opens the edit modal with an empty pattern.
3. **Auto-suggested**: when the user runs the same `:filter <pattern>` 4+ times in a session (configurable via `bulk.suggest_save_after_n_uses`), a status hint appears: "`:rule save <name>` to keep this filter."

### 5.5 Removing a saved search

`:rule delete <name>` — confirms first. Or `dd` (vim-style) on the focused saved search in the sidebar.

## 6. Evaluation and refresh

Saved searches are evaluated lazily, on-demand, with caching.

### 6.1 Cache policy

`Manager.Evaluate` caches results in memory keyed by `(name, lastModifiedAt of saved_search row)`. Cache lifetime: `[saved_search].cache_ttl` (default 60 seconds).

When the user navigates away and returns within 60s, the count and result set come from cache (instant). Beyond 60s, re-evaluation runs.

### 6.2 Background refresh of pinned counts

For pinned searches in the sidebar, the count next to the name should feel live. A background goroutine refreshes the count for each pinned search:

- On any sync engine `FolderSyncedEvent` for any folder the search might match.
- On a periodic timer: `[saved_search].background_refresh_interval` (default 2 minutes).
- Counts only — full result sets are not pre-fetched.

The background refresh runs `Evaluate` with `count_only=true` (a future optimization in spec 08; for v1, full evaluate but discard message data — the SQL `COUNT(*)` form is generated when the caller asks for count-only).

### 6.3 Invalidation triggers

Evaluation cache is invalidated:

- Per-search: when the user edits the pattern.
- Globally: on app start (cold cache).
- For specific searches: when sync engine reports changes affecting messages that matched the previous evaluation. (Heuristic: any FolderSyncedEvent invalidates everything; we don't track per-search dependency graphs in v1.)

## 7. Integration with patterns and search

### 7.1 Patterns are reused verbatim

A saved search's `pattern` field is exactly a spec 08 pattern source. The Manager calls `pattern.Compile(s.Pattern, opts)` and then `pattern.Execute(ctx, c, store, graph)`. Same execution engine as `:filter`.

### 7.2 Folder scope handling

If a saved-search pattern doesn't include `~m`, it scopes to "all subscribed folders" (not just the currently focused one). This differs from `:filter` which defaults to current folder.

Rationale: a saved search is a persistent view across the mailbox. Limiting it to the currently focused folder would make the displayed count meaningless.

To explicitly scope a saved search to a folder, the pattern includes `~m`:

```
~f newsletter@* & ~m Inbox        # only newsletters in Inbox
```

### 7.3 Predefined defaults

On first launch, the app seeds three saved searches as examples (and useful starters):

| Name | Pattern | Pinned |
| --- | --- | --- |
| `Unread` | `~N` | yes |
| `Flagged` | `~F` | yes |
| `From me` | `~f <upn>` | no |

The user can rename, edit, or delete these like any other.

`<upn>` in the seed is filled in from the active account at first run.

## 8. CLI mode

```bash
# List
inkwell rule list
inkwell rule list --output json

# Show one
inkwell rule show Newsletters

# Create
inkwell rule save Newsletters --pattern '~f newsletter@*' --pin

# Update
inkwell rule edit Newsletters --pattern '~f newsletter@* | ~f noreply@*'

# Delete
inkwell rule delete Newsletters

# Evaluate (returns IDs)
inkwell rule eval Newsletters --output json
```

## 9. Configuration

This spec owns the `[saved_search]` section. New section in `CONFIG.md`.

| Key | Default | Used in § |
| --- | --- | --- |
| `saved_search.cache_ttl` | `"60s"` | §6.1 |
| `saved_search.background_refresh_interval` | `"2m"` | §6.2 |
| `saved_search.seed_defaults` | `true` | §7.3 |
| `saved_search.toml_mirror_path` | `"~/.config/inkwell/saved_searches.toml"` | §4 |

## 10. Failure modes

| Scenario | Behavior |
| --- | --- |
| Pattern in saved search has parse error | Show in sidebar with a `⚠` glyph; selecting it shows the error in main pane with edit button. Should not crash app. |
| Saved search references a folder by `~m` that no longer exists | Pattern compile fails at evaluate time; show error; user edits or deletes. |
| TOML mirror file is malformed | App starts normally using DB; logs warning; rewrites TOML on next save. |
| TOML mirror file diverged from DB (user manually edited) | Prompt at startup as in §4. |
| Background refresh fails (network) | Sidebar count shows stale value with a `~` prefix indicator (e.g., `~247` instead of `247`). |
| Saved search count exceeds Graph paging limit | Show `999+` instead of exact count; full evaluate on selection paginates as needed. |
| User has 100+ saved searches | Sidebar scrolls within the section; no UX changes needed. |

## 11. Test plan

### Unit tests

- CRUD round-trip via Manager.
- TOML mirror write produces parseable output that round-trips.
- TOML mirror divergence detection.
- Cache TTL: evaluate twice within TTL → second is cached; after TTL → re-evaluates.
- Invalidation on sync event.

### Integration tests

- Seed defaults populate on first run.
- Edit + save updates DB and TOML.
- Background refresh updates pinned counts within interval.
- Selecting a saved search triggers Evaluate and renders results.

### Manual smoke tests

1. Create "Newsletters" saved search; verify in sidebar.
2. Edit pattern; verify count updates.
3. Pin/unpin; verify sidebar visibility.
4. Bulk-delete from a saved search view; verify undo restores; verify saved search count updates.
5. Restart app; saved search persists; pinned states preserved.
6. Manually edit TOML; restart; observe divergence prompt.

## 12. Definition of done

- [ ] CRUD complete via Manager API.
- [ ] Sidebar renders pinned searches with live counts.
- [ ] Edit modal works; pattern test (`t`) functions.
- [ ] Auto-suggest after N uses fires once per session per pattern.
- [ ] Seed defaults populate on first launch.
- [ ] TOML mirror writes correctly; divergence prompt works.
- [ ] CLI `inkwell rule` subcommands all work.
- [ ] Cache TTL and background refresh verified.
- [ ] All failure modes handled.

## 13. Out of scope

- Server-side rule creation (would require additional Graph permissions and is fundamentally different — runs on every incoming mail, not on demand).
- Saved search sharing across machines (the TOML mirror plus user's own version control is the answer).
- Saved search action triggers ("when this pattern matches, automatically delete" — this is what server-side rules do; we don't replicate them client-side because they'd only fire when the app is running).
- Hierarchical organization of saved searches (folders within saved searches). Flat list in v1.
- Color-coding or icon assignment per saved search.
