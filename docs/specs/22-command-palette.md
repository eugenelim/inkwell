# Spec 22 — Command palette

**Status:** Shipped.
**Shipped:** v0.50.0 (2026-05-07)
**Depends on:** Specs 04 (TUI shell — `Mode` enum, command-bar,
`KeyMap`, help overlay), 07 (triage verbs surfaced as actions), 10
(`:filter` cmd-bar entry surfaced), 11 (saved searches surfaced),
18 (folder management verbs surfaced), 19 (mute-thread verb), 20
(conversation chord verbs).
**Blocks:** Custom actions framework (ROADMAP §2 — actions register
into the same palette index), routing destinations / split-inbox
(ROADMAP §1.7, §1.9 — both gain a discoverability surface here).
**Estimated effort:** 1.5 days.

---

## 1. Goal

A fuzzy-find modal that exposes **every action** the user can take —
keybinding, `:` command verb, saved search, sidebar folder — in one
overlay opened by a single chord. Solves the discoverability problem
that vim-style TUIs have ("I know I can do X, what was the key?")
without inflating the keymap or splitting it across panes. It also
becomes the **passive cheatsheet**: every time the palette opens, the
right-hand column shows the live keybinding for the action, so users
who *do* memorise the binding learn it from the palette itself.

The roadmap calls this out explicitly (§1.6, P1, "Easy to implement,
big UX win"). This spec lands the palette and uses it as the seed for
later buckets — custom actions (§2) and routing destinations register
into the same row index without re-architecting.

### 1.1 What does NOT change

- The `:` cmd-bar (spec 04 §6.4) is **kept**. The palette is a *parallel*
  entry point optimised for browsing/discovering; the cmd-bar stays
  for muscle-memory power-use ("I know exactly what I'm typing"). The
  two share the same `dispatchCommand` so verb behaviour stays
  authoritative in one place.
- No keybinding for any existing action changes. The palette **only
  adds** `Ctrl+K` as a new global binding.
- The help overlay (`?`) is **kept** — it remains the categorised
  cheatsheet view. The palette is action-oriented; help is
  reference-oriented. Both pull from the same `KeyMap` so they cannot
  drift.
- No new Graph scopes, no new store tables, no new SQL. The palette is
  pure-UI: it indexes verbs the binary already knows about.
- No CLI subcommand. The palette is a TUI affordance and has no
  meaningful headless equivalent.

## 2. Prior art

### 2.1 Mail clients

- **Superhuman (Cmd+K)** — the reference design for mail. One flat
  fuzzy-ranked list; the keybinding for each command renders right-
  aligned on the row so users learn shortcuts passively. Recently-
  used commands surface near the top.
- **Hey** — no fuzzy palette; `?` overlay only and a hierarchical
  app menu on `Meta+J`. Cited as a counter-example: cheatsheets
  alone leave power features undiscovered.
- **Gmail** — `?` opens a static categorised cheatsheet. No palette.
- **aerc** — `:` command line with prefix-Tab completion in a popup.
  No fuzzy ranking, no recency, no binding hints. Strong inspiration
  for "share the dispatcher with the cmd-bar"; weak inspiration for
  the palette UX itself.
- **mutt / neomutt / Mailspring / Spike** — none ships a true Cmd+K
  palette; mutt's `?` shows context-scoped bindings only.

### 2.2 Adjacent tools (the design's actual ancestors)

- **VS Code (Cmd+Shift+P)** — subsequence fuzzy match; alphabetical
  display order with a **top-50 MRU bucket** that floats recents.
  Right-aligned binding column. Sigil sub-modes inside one input
  (`>` commands, `:` line, `@` symbol).
- **Sublime Text** — subsequence character matching; matched chars
  bolded inline; score = run-length × position weight.
- **Slack (Cmd+K)** — fuzzy + sectioned (Channels / People / Files);
  sigils (`#` channels, `@` people) scope sections. Recency-weighted.
- **Linear / Raycast (`cmd+k`)** — sectioned palette; when the user
  types, sections collapse to a flat ranked list. Raycast scores by
  **frecency** (frequency × recency, Mozilla algorithm).
- **telescope.nvim (`telescope-fzf-native`)** — TUI reference: fzf
  scoring (subsequence with bonuses for word-boundary, camelCase,
  path-separator, query-prefix, consecutive matches). Pluggable
  sorters; frecency available via `telescope-frecency`.
- **lazygit / k9s** — neither has a true palette today (open issue
  jesseduffield/lazygit#4846 explicitly requests one citing VS Code
  as the model). Cited as evidence the niche is under-served in TUIs.

### 2.3 Design decision

Borrow the high-leverage parts and reject the costly ones:

- **Right-aligned binding column** (Superhuman) — the single biggest
  UX win.
- **fzf-style scoring** with `(start-of-word, consecutive-run,
  full-prefix)` bonuses, **plus a frecency boost**. No alphabetical
  fallback — for a list this small (≤80 commands at v1) score-only
  is unambiguous and surfaces relevant rows first without an MRU
  carve-out.
- **Sigil-scoped sections in one input** (Slack/VS Code model). Empty
  buffer → recent commands. Plain typing → flat ranked actions. A
  leading `#` scopes to folders, `@` to saved searches, `>` is an
  explicit-commands sigil (no behavioural change vs no-sigil; kept
  for VS Code muscle-memory parity). `/` is **not** a sigil — typing
  `/` inserts the literal character into the buffer like any other
  rune. Spec 06's full-text search lives behind the `/` global key
  and stays distinct from the palette to avoid mode-teleporting from
  inside one modal into another.
- **No preview pane.** Palette is for *acting*, not *browsing*. Single
  column keeps the modal under 50 cols wide so it fits any terminal
  and stays well within the <100ms local-action budget.
- **Frecency persists in-process only** for v1. Cross-session frecency
  pulls in a new schema row + migration + decay tuning that is not
  worth the complexity for the discoverability case. In-session
  recency is sufficient (Slack-style "I just used this five minutes
  ago, surface it"). A `[palette]` config key can lift it to a JSON
  cache file later.

## 3. UI

### 3.1 Trigger

| Binding   | Action                                 |
|-----------|----------------------------------------|
| `Ctrl+K`  | Open palette (any pane, any mode where input is normal — Normal, but blocked while a modal is already open) |
| `Esc`     | Close palette without acting           |
| `Enter`   | Run the highlighted row                |
| `↑` / `↓` | Move cursor                            |
| `Ctrl+P` / `Ctrl+N` | Same as `↑` / `↓` (readline / fzf parity) |
| `Tab`     | Accept the highlighted row's verb into the input as a prefix and re-query against argument index (see §3.5) |

`Ctrl+K` is the global de-facto palette chord (VS Code, Superhuman,
Slack, Linear, Raycast). It does not collide with any existing
inkwell binding (`Ctrl+R` refresh, `Ctrl+U`/`Ctrl+D` page up/down,
`Ctrl+C` quit, `Ctrl+E` editor drop-out, `Ctrl+S` / `Ctrl+T` are
spec-15 compose-only).

The palette opens from `NormalMode` only. Pressing `Ctrl+K` from
another modal mode (Confirm, Compose, FolderPicker, OOF, …) is a
no-op — the modal owns the keystroke. Rationale: opening a second
modal on top of a confirm modal would orphan the confirm topic and
produce dispatch ambiguity ("which modal does Esc close?"). The user
can `Esc` out and re-open.

### 3.2 Modal layout

Centered overlay. Width = `min(max(60, terminal_width/2), 80)`,
height = `min(20, terminal_height-4)`. Reuses `t.Modal` for the box
style (same as folder picker).

```
┌─ Command palette ─────────────────────────── (mixed) ─┐
│ > arc▎                                                │
│                                                       │
│ ▶ Archive message                              a      │
│   Archive thread                               T a    │
│   Add category                                 c      │
│   [Folders]   Archive                                 │
│   [Saved]     Archive candidates                      │
│                                                       │
│   42 of 78 rows  ·  ↑/↓ navigate  ·  ⏎ run  ·  ⎋ close│
└───────────────────────────────────────────────────────┘
```

The right-side header glyph reflects the active sigil:
`(mixed)` (no sigil), `(folders)` (`#`), `(saved searches)` (`@`),
`(commands)` (`>`). Section badges (`[Folders]`, `[Saved]`,
`[Cmd]`) render as a dimmed prefix on the row title in mixed scope
to disambiguate cross-section ties; in scoped sigils the badge is
omitted because the scope is unambiguous.

Lines from top:

1. Header: `t.HelpKey.Render("Command palette")` left, hint
   right-justified showing the active scope (`(commands)`,
   `(folders)`, `(saved searches)`).
2. Input: `> ` prompt + buffer + cursor glyph (`▎`).
3. Blank.
4–N. Result rows. Each row: `marker + label`, then
   right-justified binding glyph. `marker` is `▶ ` for the cursor row,
   `  ` otherwise. Label is dimmed for rows whose action is currently
   unavailable (see §3.4).
N+1. Blank.
N+2. Status: result count + key hints (Dim style).

### 3.3 Row anatomy

A row carries:

| Field        | Source                                                   |
|--------------|----------------------------------------------------------|
| `Title`      | Human-readable verb name ("Archive message")             |
| `Subtitle`   | Optional second-line clarifier (dimmed) — used sparingly |
| `Binding`    | Right-aligned keybinding glyph (e.g. `a`, `T a`, `:filter`) |
| `Section`    | One of `Commands`, `Folders`, `Saved searches`, `Bulk` |
| `Available`  | `bool` — when `false`, row renders dimmed and Enter shows a "(why unavailable)" toast instead of dispatching |
| `RunFn`      | A `func(Model) (tea.Model, tea.Cmd)` closure the palette invokes on Enter |
| `Synonyms`   | Optional extra strings the matcher considers (e.g. "trash" matches Delete, "rm" matches Permanent delete) |

`Subtitle` exists for disambiguation only — for `Move message` the
subtitle is the resolved destination flow ("opens folder picker"); for
`Filter pattern` the subtitle is "type pattern after — opens cmd-bar
pre-filled". Most rows have no subtitle.

### 3.4 Availability

A row's `Available` bit gates whether Enter actually dispatches. It
mirrors the same gates the dispatcher already enforces today (the
palette must not let the user run a command that the cmd-bar would
reject — the failure mode would be palette → cmd-bar error toast
"calendar: not wired", which is a worse UX than the palette pre-
filtering). Each row computes its `Available` against the current
`Model` at palette-open time; the palette does not re-evaluate on
each keystroke (cheap rebuild, but locks the snapshot for the open
session — re-open to refresh).

| Row                     | Available when                                     |
|-------------------------|----------------------------------------------------|
| Archive / delete / move | A message is focused (list or viewer)              |
| Reply / forward         | Viewer has a current message AND `deps.Drafts != nil` |
| Mute / unmute thread    | Focused message has a non-empty `ConversationID`   |
| Thread chord verbs      | `deps.Thread != nil` AND focused message exists    |
| Filter / unfilter       | Always (filter open is gated by the dispatcher's "no pattern" check) |
| Apply to filtered (`;` chord) | `filterActive == true` AND `len(filterIDs) > 0` |
| Open / copy link        | Viewer has ≥1 link                                 |
| Calendar                | `deps.Calendar != nil`                             |
| OOO / settings          | `deps.Mailbox != nil`                              |
| Backfill                | `m.list.FolderID` is a real folder (not a search/filter view) |
| Saved searches          | `len(m.savedSearches) > 0` (rows present even when 0; section header just hides) |

Pressing Enter on a dimmed row emits a status-bar toast naming the
missing dependency ("calendar: not wired (CLI mode or unsigned)") —
the same string the dispatcher emits today, so the user sees a
consistent message whichever entry path they took.

### 3.5 Argument continuation (Tab)

Some palette rows correspond to commands that take an argument:

- `Move message` — needs a destination folder.
- `Apply category` — needs a category name.
- `Filter` — needs a pattern.
- `Folder: jump to…` — needs a folder name.
- `Folder: new` — needs a name.

Pressing `Tab` on such a row **does not run the command**. Instead it
delegates argument selection to the command's existing flow:

| Verb              | What `Tab` does                                          |
|-------------------|----------------------------------------------------------|
| `Move message`    | Closes palette → opens `FolderPickerMode` (the existing `m` flow)        |
| `Apply category`  | Closes palette → opens `CategoryInputMode`                               |
| `Filter`          | Closes palette → enters `CommandMode` with cmd-bar buffer pre-filled `filter ` (cursor at end). User types pattern + Enter, dispatcher runs as today. |
| `Filter (all folders)` | Same as `Filter`, but pre-filled `filter --all `. Spec 21's `dispatchCommand` `case "filter":` strips `--all` and sets `filterAllFolders=true` — composes correctly. |
| `Jump to folder`  | Reloads palette in folder-scope (`#` sigil) with the buffer cleared      |
| `New folder`      | Closes palette → opens `FolderNameInputMode` with the same parent context the `N` keypress uses (current folders-pane focus, or top-level when none) |
| `Apply to filtered (`;` chord)` | Closes palette → sets `m.bulkPending = true` and returns, mirroring the `;` keypress. Status bar shows the chord-pending hint per spec 10. |

Pressing `Enter` on the same row does the same thing as `Tab` for
verbs that have no zero-arg form. For verbs with both forms (e.g.
`Filter` could be argless if there is already a pattern in the
cmd-bar — there isn't), Enter dispatches the zero-arg form and Tab
defers to argument entry.

This avoids reimplementing argument editors inside the palette.
Existing modals stay authoritative; the palette routes to them.

### 3.6 Sigil scopes

| Sigil  | Scope                | Index                                                    |
|--------|----------------------|----------------------------------------------------------|
| (none) | All (mixed)          | Static commands + folders + saved searches in one ranked list (rows carry their `Section` for the section badge) |
| `#`    | Folders only         | All entries from `m.folders.raw` (display name + path); selecting one is `:folder <name>` |
| `@`    | Saved searches only  | All entries from `m.savedSearches` (name + pattern); selecting one runs `runFilterCmd(pattern)` |
| `>`    | Commands only        | Static commands only (no folders, no saved searches). Provided for VS Code muscle-memory parity. The reason `>` is a real scope (not just a no-op pass-through) is so users can rule out "I typed something that fuzzy-matches a folder by accident" — `>archive` always matches the archive command, never the Archive folder. |

Sigil is consumed from the buffer's leading char. Backspacing past
the sigil returns to no-scope (mixed). Switching sigils mid-query
resets the cursor to row 0. Typing `/` is **not** a sigil — `/` is
a literal character in the buffer and matches verbs/folders that
include `/` in their path label (e.g. `Inbox / Project / Q4`).

### 3.7 Empty-buffer behaviour

When the buffer is empty (no sigil, no typed chars), the palette
shows the **recent-commands** view:

```
> ▎

Recent
   Archive message                              a
   Filter ~f newsletter@*                       :filter
   Move to Receipts                             m

   78 commands available  ·  type to search  ·  ⎋ close
```

Recent rows are MRU-ordered; up to 8 rows. The recent list is in-
process state on the palette model (`recents []string`, where each
entry is the row's `ID`). Repeating an action moves it to the front;
the list never grows past 8.

If `len(recents) == 0`, the empty-buffer view shows a curated
"start here" set, in this order: `Help`, `Filter`, `Search`,
`Calendar`, `Move`, `Archive`, `Mark read`, `Sync now`. Each row
renders with its **resolved Title at Open time** — so a row whose
label flips by state (`mute_thread` resolves to either "Mute thread"
or "Unmute thread", §8 spec-19 row) carries its current label. The
default start-here set deliberately omits `mute_thread` to avoid
the empty-buffer view depending on conversation state; mute appears
in the recents bucket once the user has dispatched it once.

## 4. Implementation

### 4.1 Static command index

A new file `internal/ui/palette_commands.go` declares the static row
table. Each row is a struct literal:

```go
// PaletteRow is one entry the palette can dispatch. Pure data — the
// RunFn / ArgFn close over the snapshot Model passed in at row-
// collection time (so the resolved focused message + folder + deps
// stay consistent with what the palette was rendered against, even
// if Bubble Tea schedules a state-changing message before Enter
// fires). Both functions receive the *current* Model from
// updatePalette and merge what they need from it before returning.
type PaletteRow struct {
    ID        string   // stable identifier for recency/MRU tracking
    Title     string
    Subtitle  string   // optional
    Binding   string   // human-rendered ("a", "T a", ":filter", "")
    Section   string   // "Commands" | "Folders" | "Saved searches"
    Synonyms  []string
    NeedsArg  bool     // when true, Enter behaves like Tab (defer to arg flow)
    Available Availability
    RunFn     func(m Model) (tea.Model, tea.Cmd)
    ArgFn     func(m Model) (tea.Model, tea.Cmd) // §3.5 Tab handler; nil for argless verbs
}

// Availability is resolved once per palette Open against the live
// Model snapshot and stored on the row. Renderer + dispatcher both
// read from it without re-evaluating per keystroke. A row with
// Available.OK == false renders dimmed and Enter emits Why as a
// status-bar toast instead of running RunFn.
type Availability struct {
    OK  bool
    Why string
}
```

`RunFn` and `ArgFn` are **values, not pointers**. The palette stores
its rows in `[]PaletteRow`; each row's closures capture small
identifiers (folder ID, saved-search name, key string) — never the
whole Model. At Enter time the caller hands the *current* Model into
`RunFn`, the closure mutates the value-copy as needed, and Bubble
Tea picks up the returned `tea.Model`. This matches the value-type
sub-model convention from CLAUDE.md §4.

`buildStaticPaletteRows(km KeyMap) []PaletteRow` returns the canonical
list. The keymap is passed in so `Binding` is rendered from
`keysOf(km.Archive)` etc. — same convention as `help.go`. The list:

| ID                       | Title                                  | Binding (default)        |
|--------------------------|----------------------------------------|--------------------------|
| `archive`                | Archive message                        | `a`                      |
| `delete`                 | Delete (move to Deleted Items)         | `d`                      |
| `permanent_delete`       | Permanent delete (skip Deleted Items)  | `D`                      |
| `mark_read`              | Mark read                              | `r` (list-pane meaning)  |
| `mark_unread`            | Mark unread                            | `R`                      |
| `toggle_flag`            | Toggle flag                            | `f`                      |
| `move`                   | Move to folder…                        | `m`                      |
| `add_category`           | Add category…                          | `c`                      |
| `remove_category`        | Remove category…                       | `C`                      |
| `undo`                   | Undo last action                       | `u`                      |
| `unsubscribe`            | Unsubscribe (RFC 8058)                 | `U`                      |
| `mute_thread`            | Mute / unmute thread                   | `M`                      |
| `thread_archive`         | Archive thread                         | `T a`                    |
| `thread_delete`          | Delete thread                          | `T d`                    |
| `thread_permanent_delete`| Permanent delete thread                | `T D`                    |
| `thread_mark_read`       | Mark thread read                       | `T r`                    |
| `thread_mark_unread`     | Mark thread unread                     | `T R`                    |
| `thread_flag`            | Flag thread                            | `T f`                    |
| `thread_unflag`          | Unflag thread                          | `T F`                    |
| `thread_move`            | Move thread to folder…                 | `T m`                    |
| `reply`                  | Reply                                  | `r` (viewer-pane)        |
| `reply_all`              | Reply-all                              | `R` (viewer-pane)        |
| `forward`                | Forward                                | `f` (viewer-pane)        |
| `compose`                | New message                            | (`:compose` — none today)|
| `filter`                 | Filter…                                | `F` / `:filter`          |
| `filter_all`             | Filter (all folders)…                  | `:filter --all`          |
| `unfilter`               | Clear filter                           | `Esc` / `:unfilter`      |
| `apply_to_filtered`      | Apply action to filtered…              | `;`                      |
| `open_url`               | Open URL…                              | `O`                      |
| `yank_url`               | Yank URL                               | `y`                      |
| `fullscreen_body`        | Fullscreen body                        | `z`                      |
| `folder_jump`            | Jump to folder…                        | `:folder`                |
| `folder_new`             | New folder…                            | `N` (folders pane) / `:folder new` |
| `folder_rename`          | Rename folder…                         | `R` (folders pane)       |
| `folder_delete`          | Delete folder…                         | `X` (folders pane)       |
| `search`                 | Search messages…                       | `/`                      |
| `backfill`               | Backfill older messages                | `:backfill`              |
| `sync`                   | Sync now                               | `Ctrl+R`                 |
| `calendar`               | Open calendar                          | `:cal`                   |
| `ooo_on`                 | Out-of-office: turn on                 | `:ooo on`                |
| `ooo_off`                | Out-of-office: turn off                | `:ooo off`               |
| `ooo_schedule`           | Out-of-office: schedule…               | `:ooo schedule`          |
| `settings`               | Mailbox settings                       | `:settings`              |
| `rule_save`              | Saved search: save current filter…     | `:save`                  |
| `rule_list`              | Saved search: list                     | `:rule list`             |
| `rule_edit`              | Saved search: edit…                    | `:rule edit`             |
| `rule_delete`            | Saved search: delete…                  | `:rule delete`           |
| `signin`                 | Sign in                                | `:signin`                |
| `signout`                | Sign out and clear credentials         | `:signout`               |
| `help`                   | Help (every binding)                   | `?`                      |
| `quit`                   | Quit                                   | `q` / `Ctrl+C`           |

Synonyms (illustrative — full list in code):

- `delete`: `["trash", "remove"]`
- `permanent_delete`: `["purge", "rm", "destroy"]`
- `archive`: `["done", "file"]`
- `mark_read`: `["read"]`
- `unsubscribe`: `["unsub", "list-unsub"]`
- `compose`: `["new", "write"]`
- `filter`: `["search local", "narrow"]`
- `apply_to_filtered`: `["bulk"]`
- `fullscreen_body`: `["zoom"]`
- `mute_thread`: `["silence"]`
- `sync`: `["refresh", "fetch"]`

Synonyms are matched at lower priority than the title (see §4.4
scoring) — a hit on the title outranks a hit on a synonym. This
keeps the obvious matches obvious.

### 4.2 Dynamic indexes

Folder index (`#` sigil) is built from `m.folders.raw`. Each folder
becomes a row:

```go
PaletteRow{
    ID:      "folder:" + f.ID,
    Title:   pathByID[f.ID],          // "Inbox / Project / 2025"
    Binding: "",                      // jump has no shortcut
    Section: "Folders",
    RunFn: func(m Model) (tea.Model, tea.Cmd) {
        m.list.FolderID = f.ID
        m.focused = ListPane
        return m, m.loadMessagesCmd(f.ID)
    },
}
```

Saved-search index (`@` sigil) is built from `m.savedSearches`:

```go
PaletteRow{
    ID:       "rule:" + ss.Name,
    Title:    ss.Name,
    Subtitle: ss.Pattern,
    Binding:  "",
    Section:  "Saved searches",
    RunFn:    func(m Model) (tea.Model, tea.Cmd) { return m, m.runFilterCmd(ss.Pattern) },
}
```

Both rebuild on palette-open from the live model snapshot. No long-
lived caches — saved searches change rarely; folders change only via
spec 18 actions.

### 4.3 Palette model

`internal/ui/palette.go`:

```go
// PaletteModel is the Ctrl+K modal. Stateless beyond cursor + buffer
// + recents; the row index is rebuilt on each Open().
type PaletteModel struct {
    cursor   int
    buf      string         // includes the leading sigil if any
    rows     []PaletteRow   // unfiltered, in static + dynamic order
    filtered []scoredRow    // result of the most-recent matchAndScore()
    recents  []string       // MRU IDs, capped at 8
}

type scoredRow struct {
    row   PaletteRow
    score int
    hits  []int  // rune indices in the title that matched (for inline highlighting)
}

func NewPalette() PaletteModel { return PaletteModel{} }

// Open seeds the row list from a Model snapshot. Called whenever the
// palette is entered so live state (focused message, deps) is fresh.
// The snapshot is **frozen for the open session**: a background
// FoldersLoadedMsg or savedSearchesUpdatedMsg arriving while the
// palette is open does not re-collect rows. Re-open to refresh.
// Per §6 edge cases this is benign — folder RunFns dispatch by ID
// and a stale ID surfaces the existing 404 path.
func (p *PaletteModel) Open(m *Model) {
    p.cursor = 0
    p.buf = ""
    p.rows = collectPaletteRows(m)
    p.refilter()
}
```

`collectPaletteRows(m *Model)` returns `static + folders + saved`,
each row's `Availability` resolved against `m` and cached on the row.
Unavailable rows are **included** (so the user sees they exist) but
render dimmed; Enter on a dimmed row emits the cached `Why` string
as a status-bar toast (handled in `updatePalette`, §4.6).

### 4.4 Fuzzy matching

`internal/ui/palette_match.go` implements an fzf-style subsequence
matcher. Per-row caches (computed once at `collectPaletteRows` and
stored on a sibling `paletteRowCache` keyed by row index — *not*
on `PaletteRow` itself so the dispatchable type stays a plain
data record):

- `runes []rune` — lowercased rune slice of `Title + " ¦ " +
  strings.Join(Synonyms, " ")`. The `¦` (U+00A6) is a sentinel rune
  unlikely to appear in any title or query; it never matches a
  user-typed query character (the matcher excludes the sentinel
  explicitly) and acts as the title/synonym boundary.
- `titleEnd int` — index into `runes` immediately after the last
  rune of `Title` (the index of the leading space before the
  sentinel). Used by the matcher to award the in-title bonus.

Algorithm:

1. Lowercase the buffer (`q`).
2. Walk `q` left-to-right. For each rune, find the next matching
   rune in `runes` strictly after the previous match index, skipping
   the sentinel. If any rune of `q` has no match → row is excluded.
3. Score = sum of per-rune contributions, with bonuses:

   | Condition                                | Delta  |
   |------------------------------------------|--------|
   | Match at index 0 (full prefix on title)  | `+30`  |
   | Match at start-of-word (preceded by space, `_`, `:`, `/`) | `+12`  |
   | Match is uppercase in the original (case-preserving) Title | `+8`   |
   | Match is consecutive with previous       | `+6`   |
   | Match index < `titleEnd` (i.e. inside the title proper, not the synonym tail) | `+10`  |
   | Otherwise                                | `+1`   |
   | Penalty per skipped rune (between matches) | `-1`   |

   The title-bonus is computed by index, so synonyms genuinely rank
   below titles for the same query — verified by
   `TestMatchTitleOutranksSynonym` (§7).

4. Frecency boost: `recencyBoost = max(0, 60 - 8*idx)` where `idx` is
   the row's position in `recents` (0 = most recent). Rows not in
   `recents` get `0`.

5. Final = scoring sum + recencyBoost. Rows with
   `Availability.OK == false` get a `-50` penalty so they sink below
   comparable available rows without disappearing.

Sort descending by final score, then ascending by Title for ties.
The full scan is O(rows × |q|); with rows ≤ ~200 (commands + folders
+ saved searches in a typical mailbox) and |q| < 16 this stays well
under the per-keystroke budget (§6).

When `q` is empty, skip scoring entirely and return rows in this
order: recents first (in MRU order), then the curated start-here
set, then everything else in `Section, Title` order.

### 4.5 Sigil routing

`refilter()` reads the leading sigil:

```go
switch first := firstByte(p.buf); first {
case '#': // folders only
    p.scope = scopeFolders
    p.match(p.buf[1:], scopeFolders)
case '@': // saved searches only
    p.scope = scopeSavedSearches
    p.match(p.buf[1:], scopeSavedSearches)
case '>':
    p.scope = scopeCommands
    p.match(p.buf[1:], scopeCommands)
default:
    p.scope = scopeAll
    p.match(p.buf, scopeAll)
}
```

`scopeAll` includes commands + folders + saved searches in one mixed
list (rows still carry `Section` for the right-side hint, and the
section appears as a dimmed prefix on the row title when score-tied
rows belong to different sections).

### 4.6 Wiring into the app

1. **New mode** in `messages.go`:

   ```go
   const (
       // ... existing modes ...
       PaletteMode
   )
   ```

2. **New keymap field** in `keys.go`:

   ```go
   type KeyMap struct {
       // ... existing fields ...
       Palette key.Binding
   }
   // DefaultKeyMap:
   Palette: key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "command palette")),
   ```

   Plus a `Palette string` field on `BindingOverrides` and an `apply`
   call in `ApplyBindingOverrides`. Add `{"palette", km.Palette}` to
   the duplicate-detection set in `findDuplicateBinding`.

3. **Trigger** in `updateNormal` (the global key dispatcher in
   `app.go`, alongside the existing `:`, `/`, `?` matchers — locate
   the block that matches `keymap.Cmd` / `keymap.Search` /
   `keymap.Help`; do not trust line numbers, they shift with every
   commit):

   ```go
   case key.Matches(keyMsg, m.keymap.Palette):
       m.palette.Open(&m)
       m.mode = PaletteMode
       return m, nil
   ```

4. **Mode dispatch** in the root Update mode switch (the
   `switch m.mode { case SignInMode: …  case ConfirmMode: …  case
   CommandMode: … }` block in `app.go`'s `Update`):

   ```go
   case PaletteMode:
       return m.updatePalette(msg)
   ```

5. **`updatePalette`** new function in `app.go`:

   ```go
   func (m Model) updatePalette(msg tea.Msg) (tea.Model, tea.Cmd) {
       keyMsg, ok := msg.(tea.KeyMsg)
       if !ok {
           return m, nil
       }
       switch {
       case keyMsg.Type == tea.KeyEsc:
           m.mode = NormalMode
           return m, nil

       case keyMsg.Type == tea.KeyEnter:
           sel := m.palette.Selected()
           m.mode = NormalMode
           if sel == nil {
               return m, nil
           }
           m.palette.recordRecent(sel.ID)
           if sel.NeedsArg && sel.ArgFn != nil {
               return sel.ArgFn(m)
           }
           if !sel.Available.OK {
               m.lastError = fmt.Errorf("%s", sel.Available.Why)
               return m, nil
           }
           return sel.RunFn(m)

       case keyMsg.Type == tea.KeyTab:
           sel := m.palette.Selected()
           m.mode = NormalMode
           if sel == nil || sel.ArgFn == nil {
               return m, nil
           }
           m.palette.recordRecent(sel.ID)
           return sel.ArgFn(m)

       case keyMsg.Type == tea.KeyUp, keyMsg.String() == "ctrl+p":
           m.palette.Up()
           return m, nil

       case keyMsg.Type == tea.KeyDown, keyMsg.String() == "ctrl+n":
           m.palette.Down()
           return m, nil

       case keyMsg.Type == tea.KeyBackspace:
           m.palette.Backspace()
           return m, nil

       case keyMsg.Type == tea.KeyRunes:
           m.palette.AppendRunes(keyMsg.Runes)
           return m, nil
       }
       return m, nil
   }
   ```

   `Available` is resolved at row-collection time (§4.3) and stored
   on the row, so Enter reads `sel.Available.OK` without re-running
   the gate. `recordRecent` is called *before* the dispatch so even
   a verb that changes mode (Filter pre-fills the cmd-bar →
   `CommandMode`) still records its row in the palette MRU.

6. **Render** in `View()` (alongside the existing
   `if m.mode == FolderPickerMode { … }` branch in the modal-render
   chain at the top of `View`):

   ```go
   if m.mode == PaletteMode {
       return m.palette.View(m.theme, m.keymap, m.width, m.height)
   }
   ```

7. **Field** on `Model`:

   ```go
   palette PaletteModel
   ```

   Initialised by value (`palette: NewPalette()` in `NewModel` /
   wherever the model is constructed).

8. **Help overlay** (`internal/ui/help.go` `buildHelpSections`) gains a
   row in `Modes & meta`:

   ```go
   {keysOf(km.Palette), "command palette"},
   ```

### 4.7 Theme

A new style token on `Theme`:

```go
PaletteHeader   lipgloss.Style  // the "Command palette" title
PaletteBinding  lipgloss.Style  // the right-aligned binding glyph (HelpKey-equivalent, dim)
PaletteSection  lipgloss.Style  // the "Folders" / "Commands" badge on rows
```

Default palette derivations: `PaletteHeader` = `HelpKey`,
`PaletteBinding` = `Dim`, `PaletteSection` = `Dim` italic. Each
preset palette in `theme.go` inherits the defaults; explicit overrides
can be added later if the palette needs differentiation from help.

## 5. Edge cases

| Case | Behaviour |
|------|-----------|
| `Ctrl+K` from `CommandMode` (cmd-bar open) | No-op. Cmd-bar consumes the keystroke first; user must `Esc` then `Ctrl+K`. Documented in user reference. |
| `Ctrl+K` from `SearchMode` | No-op (same reasoning). |
| `Ctrl+K` from another modal (Compose, FolderPicker, OOF, Confirm, etc.) | No-op. The modal's `update*` handler does not match on `m.keymap.Palette`. |
| Enter with empty filtered list | No-op (cursor is past end → `Selected()` returns nil → return early with no error). |
| Enter on a dimmed (unavailable) row | Status-bar toast with the `why` string from `Available`; mode returns to NormalMode. |
| Tab on a row whose `ArgFn == nil` | Same as Enter (no separate arg flow exists). |
| Backspace at empty buffer | No-op (do not close palette — Esc is the close key). |
| Palette open + folder list refreshes via `FoldersLoadedMsg` while the palette is open | The palette's snapshot is **stale** for this open session. Re-open to refresh. Documented; not a bug — the palette is a short-lived modal. The folder row's `RunFn` captures `f.ID` (a string) by closure value, never a pointer to a `store.Folder`. Selecting a row whose folder was deleted server-side dispatches `loadMessagesCmd` against an ID that yields zero local rows; if a Graph-side mutation referenced the missing ID, the existing 404 path emits a status-bar error. **No nil deref** because closures carry IDs. |
| A folder is renamed or moved server-side mid-session | Folder `f.ID` is stable across rename and move on Microsoft Graph, so the captured ID still resolves. The cached `Title` (path label) is stale until palette re-open; benign. |
| User overrides `Ctrl+K` to another key in `[bindings]` | `findDuplicateBinding` rejects the override at config-load time if it collides; otherwise `Ctrl+K` no longer triggers and the override key does. |
| User overrides `Ctrl+K` to empty / unbinds | `key.NewBinding(WithKeys())` matches nothing. Palette becomes inaccessible — same UX as unbinding `?`. Friendly hint in the user reference: "to disable the palette, set `palette = ''` in `[bindings]`". |
| Frecency entry for a now-deleted saved search | Recent entry survives across that session but matches no row (filtered out at row-collection time); harmlessly skipped. The MRU is bounded at 8 so it self-prunes. |
| Two saved searches with identical names | Both render. Selecting one runs whichever's `RunFn` matches first by score; ties broken by alphabetical (matches §4.4). Saved-search names are unique in the store (spec 11) so this case is theoretical. |
| Palette opened with terminal width < 60 | Width clamps to `terminal_width - 2`. If the result is < 30, palette renders without the binding column (the right-aligned glyph is dropped) and shows a one-line warning at the top: "(palette: terminal too narrow for binding hints)". The palette never refuses to render. |
| Long folder paths exceed modal width | Truncate with ellipsis at the end, preserving the rightmost component (the leaf name) so `Inbox / Project / Q4 forecast 2026` becomes `…/Q4 forecast 2026`. |
| Filter scope `#` + buffer matches no folders | Empty list with hint "no folders match — Esc to close, Backspace for commands". Same shape for `@`. |

## 6. Performance

The palette runs entirely in-process against the in-memory row list.
No I/O on any keystroke. Budgets:

| Surface                                     | Budget        |
|---------------------------------------------|---------------|
| Open palette (rebuild rows from snapshot)   | <5ms p95      |
| Per-keystroke filter+rerank (typical mailbox: ~200 rows) | <2ms p95 |
| Per-keystroke filter+rerank (extreme: 5000 rows — large tenant with many folders) | <15ms p95 |
| Render frame (modal layout)                 | <16ms (per the cold-start / interactive budget in PRD §7 — folder picker is the closest existing analogue and renders within this) |

Bench `BenchmarkPaletteFilter` over a synthesised 5000-row index
(generated by a `testfixtures.go` helper that creates 80 commands +
4900 folders + 20 saved searches). Fail the test if any keystroke
exceeds 22ms (the 50% headroom rule from CLAUDE.md §6).

The matcher is allocation-light: pre-allocated `scored` slice,
re-used between rebuilds; no map lookups in the inner loop. `bytes`
+ rune-by-rune walk over an `[]rune` snapshot of each row's
search string (cached on the row, not recomputed per keystroke).

## 7. Definition of done

- [ ] **Mode**: `PaletteMode` constant added to `internal/ui/messages.go`.
- [ ] **Keymap**: `KeyMap.Palette` field, `BindingOverrides.Palette`
      field, default binding `ctrl+k`, override plumbing in
      `ApplyBindingOverrides`, duplicate detection includes palette.
- [ ] **Trigger**: `updateNormal` in `app.go` matches `m.keymap.Palette`
      and transitions to `PaletteMode`. Other modes' update handlers
      do not match the palette key (no-op).
- [ ] **Files**: `internal/ui/palette.go` (model + view +
      Open/Up/Down/Backspace/AppendRunes/Selected/recordRecent),
      `internal/ui/palette_commands.go`
      (`buildStaticPaletteRows(km KeyMap)` and the row literals),
      `internal/ui/palette_match.go` (scoring + sort).
- [ ] **Sigils**: `#` folders, `@` saved searches, `>` commands-only,
      no-sigil = mixed. `/` is a **literal** rune in the buffer (not
      a sigil) — typing `/` after `Ctrl+K` does not exit the palette.
- [ ] **Tab vs Enter**: Tab on `NeedsArg` rows defers to `ArgFn`
      (folder picker / category input / cmd-bar pre-fill /
      folder-name input). Enter on `NeedsArg` rows behaves the same
      since these have no zero-arg form.
- [ ] **Availability**: dimmed rows render but Enter shows a toast
      with the `why` string instead of dispatching.
- [ ] **Recents**: in-process MRU, cap 8, surfaced first when
      buffer is empty. Selection records into recents before the
      RunFn fires.
- [ ] **Render**: centered modal via `lipgloss.Place(... t.Modal ...)`.
      Width = `min(max(60, w/2), 80)`. Right-aligned binding column.
      Section badges. Status footer with result count + key hints.
- [ ] **Help overlay**: `buildHelpSections` in `help.go` gains a
      `command palette` row in `Modes & meta`.
- [ ] **Tests** (§9 spec layer):
  - [ ] Unit: scoring (subsequence, prefix bonus, word-boundary
        bonus, consecutive bonus, frecency boost, exclusion on
        non-match).
  - [ ] Unit: sigil routing (`#`, `@`, `>`, none); switching sigils
        resets cursor.
  - [ ] Unit: `recordRecent` MRU semantics (move-to-front, dedup,
        cap 8).
  - [ ] Unit: `collectPaletteRows` includes commands + folders +
        saved searches and resolves `Available` against the supplied
        Model.
  - [ ] Unit: width clamp <30 hides binding column.
  - [ ] Unit: `TestMatchTitleOutranksSynonym` — a query matching
        both a Title rune and a Synonym rune scores higher when the
        match falls in the title (verifies the `titleEnd` boundary
        contributes the +10 in-title bonus).
  - [ ] e2e (CLAUDE.md §5.4 — visible-delta required for every binding):
        - `TestPaletteOpensFromNormalMode` — `Ctrl+K` from NormalMode
          shows the palette modal frame (header "Command palette"
          appears in the rendered final frame; was absent before).
        - `TestPaletteEscClosesPalette` — `Ctrl+K` then `Esc`: the
          palette modal frame disappears from the rendered output
          and the prior pane chrome is back.
        - `TestPaletteCursorDownMovesMarker` — capture frame before/
          after `↓`, assert the `▶` glyph is on a different row.
        - `TestPaletteCursorUpMovesMarker` — same for `↑`.
        - `TestPaletteCtrlNMovesCursor` and `TestPaletteCtrlPMovesCursor`
          — fzf/readline parity bindings; visible-delta on `▶` row.
        - `TestPaletteEnterArchiveDispatchesSoftDelete` — Enter on
          the `archive` row runs the same path as the single-key
          `a` (compare emitted Cmd type / final visible model state).
        - `TestPaletteTypeNarrowsRows` — typing narrows the visible
          rows; first matching row carries the `▶` glyph.
        - `TestPaletteSigilHashShowsFolders` — typing `#` swaps to
          folder-only scope; visible delta is the section hint in
          the header changing to "(folders)".
        - `TestPaletteSigilAtShowsSavedSearches` — typing `@` swaps
          to saved-search-only scope; header glyph "(saved searches)".
        - `TestPaletteSigilGreaterScopesToCommands` — typing `>` shows
          static commands only; header glyph "(commands)"; folder
          rows that match by name are absent from the result list.
        - `TestPaletteSigilSwitchResetsCursor` — switching from `#`
          to `@` mid-query moves the `▶` marker back to row 0.
        - `TestPaletteBackspacePastSigilReturnsToMixed` — type `#`,
          then Backspace; header glyph changes from `(folders)` to
          `(mixed)` and the row list re-expands to include commands
          and saved searches.
        - `TestPaletteBackspaceAtEmptyBufferIsNoop` — Backspace at
          empty buffer leaves mode = `PaletteMode`, buffer empty,
          row list unchanged (visible-delta: nothing changes).
        - `TestPaletteSlashIsLiteral` — typing `/` after `Ctrl+K`
          inserts the rune into the buffer; mode stays `PaletteMode`.
        - `TestPaletteTabMoveOpensFolderPicker` — `Ctrl+K` then type
          `move` then `Tab` transitions to `FolderPickerMode`.
        - `TestPaletteTabFilterPrefillsCmdBar` — Tab on Filter
          row leaves mode = `CommandMode` and cmd-bar buffer =
          `"filter "`.
        - `TestPaletteOpenFromCommandModeIsNoop` — pressing `Ctrl+K`
          while the cmd-bar is open does not change mode and does
          not clear the cmd-bar buffer.
        - `TestPaletteOpenFromConfirmModeIsNoop` — same shape for a
          live confirm modal.
        - `TestPaletteDimmedRowEnterEmitsToast` — Enter on a row
          with `Available.OK == false` leaves mode = `NormalMode`
          and renders the `Why` string in the status bar.
        - `TestPaletteEmptyBufferShowsRecents` — after a prior
          dispatch, re-opening the palette shows the row in the
          Recent section header.
        - `TestPaletteMuteRowLabelFlips` — for a focused message
          whose conversation is muted, the `mute_thread` row's
          Title renders "Unmute thread" (resolved at Open).
        - `TestPaletteNarrowTerminalDropsBindingColumn` — at width
          < 30 the right-aligned binding glyph is omitted and the
          one-line warning renders.
  - [ ] Redaction: `TestPaletteLogsNoSubjectsOrAddresses` — open
        palette with a focused message; assert no log line at any
        level contains the subject or any email address from the
        focused message or from any folder name. The palette emits
        no logs by design (§7); this test guards against future
        regressions if a `slog.Debug` is added later.
  - [ ] Bench: `BenchmarkPaletteFilter` over 5000 rows under 22ms
        p95 per keystroke. Fixture generated by
        `internal/ui/testfixtures.go` (synthesised; no committed
        binary blobs per CLAUDE.md §5.2).
- [ ] **Config**: `[bindings] palette = "..."` documented in
      `docs/CONFIG.md` (new row alongside the existing bindings).
- [ ] **User docs**:
  - [ ] `docs/user/reference.md` adds a "Command palette" section
        listing `Ctrl+K`, `Esc`, `↑/↓`, `Ctrl+P/N`, `Tab`, `Enter`,
        sigils `#` `@` `>`.
  - [ ] `docs/user/how-to.md` gains a recipe "Discover and learn
        keybindings using the palette" — open `Ctrl+K`, type a
        verb, glance at the right-hand binding column, use it next
        time.
  - [ ] `docs/user/explanation.md` updated only if the palette
        changes a design invariant; it does not (cmd-bar stays, help
        stays, no scope change). Skip.
- [ ] **Project docs** (must land in the same commit that ships the
      feature, per CLAUDE.md §13):
  - [ ] `docs/PRD.md` §10 inventory gains row
        `22 | 22-command-palette.md | post-v1, ROADMAP §0 bucket 2`.
  - [ ] `docs/ROADMAP.md` §0 Bucket 2 row 1 ("Command palette (1.6)")
        updates the `Why this slot` cell to reference Spec 22 and the
        §1.6 narrative gains a "Owner: spec 22" line.
  - [ ] `docs/plans/spec-22.md` exists and is updated each iteration
        (CLAUDE.md §13 mandatory tracking note).
- [ ] All five mandatory commands (CLAUDE.md §5.6) green:
      `gofmt -s`, `go vet`, `go test -race`, `go test -tags=e2e`,
      `go test -bench=. -benchmem -run=^$`.

## 8. Cross-cutting checklist

- [ ] **Scopes:** none. Palette dispatches verbs that already exist;
      it adds no Graph endpoint and no new permission.
- [ ] **Store reads/writes:** none directly. Folder + saved-search
      indexes read from in-memory `Model` snapshots already populated
      from store via existing `FoldersLoadedMsg` /
      `savedSearchesUpdatedMsg` handlers.
- [ ] **Graph endpoints:** none new.
- [ ] **Offline:** works fully offline. Every dispatched verb falls
      back to its existing offline behaviour (action queue, FTS,
      cached body) — palette adds no online assumption.
- [ ] **Undo:** palette dispatches verbs that already manage their
      own undo entries (spec 07). The palette itself is a discovery
      surface and emits no undo records.
- [ ] **Error states:** unavailable rows surface the same `why`
      strings the cmd-bar emits. Empty filtered list is a quiet
      no-op + count hint. No new error surfaces.
- [ ] **Latency:** §6 budget. Bench gates. Open + per-keystroke both
      well within the spec 04 frame budget.
- [ ] **Logs:** the palette emits no logs by default. If a row's
      RunFn logs (e.g. `:filter` does at DEBUG), that path is unchanged.
      No PII enters the palette buffer that wasn't already in the
      cmd-bar buffer (saved-search names, folder names — same surface
      already touched). No redaction work needed.
- [ ] **CLI:** none. Palette is TUI-only.
- [ ] **Tests:** §7 list.
- [ ] **Spec 17 review:** no token handling, no file I/O, no subprocess,
      no external HTTP, no third-party data flow, no cryptographic
      primitive, no new SQL composition, no local persisted state. The
      only new surface is in-memory string matching against
      strings already in `Model`. Nothing for the threat model or
      privacy doc to gain. No `// #nosec` annotations expected.
- [ ] **Spec 04 consistency:** mode dispatch via the root switch;
      pane-scoped meaning preserved (palette opens from NormalMode,
      not from inside other modes); `KeyMap` is the contract surface,
      bindings overridable via `[bindings]`.
- [ ] **Spec 11 consistency:** saved searches surface via the `@`
      sigil and via section in mixed view. Selecting one runs
      `runFilterCmd(pattern)` — exactly the path `:rule` Save uses
      today. No new saved-search semantics.
- [ ] **Spec 18 consistency:** folder management verbs surface
      (`Folder: new / rename / delete`). Tab on `New folder` opens
      `FolderNameInputMode`; Enter without Tab is the same (NeedsArg).
      Rename/Delete are folder-pane-scoped today (need a focused
      folder); the palette's `Available` reflects that — disabled
      when the folders pane has no focus and no folder is selected.
- [ ] **Spec 19 consistency:** `mute_thread` row's `Available`
      requires a focused message with `ConversationID != ""` (same
      gate as the `M` key). Mute and unmute share one row whose label
      flips ("Mute thread" / "Unmute thread") based on
      `IsMuted(ConversationID)` resolved at palette-open.
- [ ] **Spec 20 consistency:** thread-chord verbs (`T r`, `T R`, `T f`,
      `T F`, `T a`, `T d`, `T D`, `T m`) each get a palette row with
      `Binding: "T <verb>"`. **No refactor of spec 20's dispatch
      code is in scope** (CLAUDE.md §12.4 — "Let me refactor while
      I'm here. No. Scope discipline."). Each palette RunFn copies
      the 1–3 line dispatch from the chord handler (each verb is one
      method call —
      `m.runThreadExecuteCmd(verb, store.ActionXxx, focusedID)` for
      most; `T m` is the only longer one, ~12 lines for the move
      flow). The copy carries a `// keep in sync with the T<verb>
      chord handler` comment pointing at the chord block so the two
      stay aligned. A spec 20 follow-up may extract a shared helper
      later; not here. Synthesising a `tea.KeyMsg` to replay the
      chord is **rejected** as brittle — the chord handler gates on
      `threadChordPending` across two Update cycles, which is not
      reliably reproducible from inside a single Update.
- [ ] **Spec 21 consistency:** `filter --all` is a separate row
      (`filter_all`). Selecting it pre-fills the cmd-bar with
      `filter --all ` so the user types the pattern in the same
      flow as `filter`.
- [ ] **Docs consistency sweep:** reference + how-to updated per §7;
      `CONFIG.md` row for `[bindings] palette` added; ROADMAP
      §0 bucket 2 row 1 marks command palette as Spec 22; PRD §10
      spec inventory gains row `22 | 22-command-palette.md | post-v1,
      ROADMAP §0 bucket 2`.
