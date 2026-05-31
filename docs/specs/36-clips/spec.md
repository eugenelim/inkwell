# Spec 36 — Clips (knowledge fragments)

**Status:** Draft.
**Owner:** eugenelim
**Tracking note:** `docs/specs/36-clips/plan.md`.
**Depends on:** Spec 02 (store schema — migration runner + `SchemaVersion`
constant at `internal/store/store.go:21-22`; the `messages_fts` FTS5 pattern
at `internal/store/migrations/001_initial.sql:142-167` is the canonical
shape this spec mirrors), Spec 04 (TUI shell — `ViewerModel` at
`internal/ui/panes.go:1632`, the list/overlay components, `dispatchViewer`
at `internal/ui/app.go`), Spec 05 (message rendering — the viewer body
string the selection runs over), Spec 06 (hybrid search — the
`messages_fts` MATCH conventions `:clips` search follows; note `:clips`
does **not** call `internal/search`'s query builder — that would be an
upward import, §6.2), Spec 22 (command
palette — `PaletteRow` at `internal/ui/palette.go:41`, `collectPaletteRows`
at `internal/ui/palette_commands.go:21`).
Reuses but does not change: Spec 17 (privacy / threat model — the new
on-disk surface and clipboard-egress path are documented in
`docs/THREAT_MODEL.md` + `docs/PRIVACY.md` per §8; PR carries the spec-17
impact line), Spec 35 (reuses `redact.HashMessageID` at
`internal/log/redact.go:69`; follows the `body_text` precedent for a new
content-bearing table but evicts on a different — non-LRU — policy, §7).
Reuses the existing clipboard path: the OSC 52 + `pbcopy` `yanker` at
`internal/ui/clipboard.go:46`, already bound to viewer `y` for URL-yank.
**Blocks:** None.
**Estimated effort:** 3–4 days (one schema migration, one store API
family, one net-new viewer interaction mode — visual line selection — one
overlay, one CLI verb family, one config section, full test pyramid + perf
budgets + privacy docs).

---

### 0.1 Spec inventory

Clips is item 2 of **Bucket 5 — Search & knowledge** in
`docs/product/roadmap.md` (backlog entry §1.12 "Clips — P2"). The roadmap
sketch:

> Highlight text within a message and save it as a knowledge fragment
> outside the email. Useful for door codes, addresses, SKUs, and other
> key data buried inside marketing fluff. A keybinding (`y` for yank,
> vim-style) copies the selected line range to a `clips` table … A
> `:clips` command opens an FTS5-searchable view of all clips. `Enter`
> on a clip jumps back to the source message.

This spec ships that sketch with two design decisions the roadmap left
open, both forced by the existing code:

1. **The keybinding.** Bare `y` in the viewer is **already** URL-yank
   (`internal/ui/keys.go:251` → `yankURL` at `internal/ui/app.go:6807`).
   Rebinding it would silently break a shipped gesture. Instead clips
   introduce a vim-faithful **visual line-selection mode** entered with
   `v`; `y` *inside* that mode saves the selection as a clip. Bare `y`
   in normal viewer mode is unchanged. The two are mode-disambiguated,
   never ambiguous (§5).

2. **Clips outlive their source.** The whole point is "a knowledge
   fragment *outside* the email." `clips.source_message_id` is therefore
   `ON DELETE SET NULL`, not `CASCADE`: permanently deleting (or evicting)
   the source message leaves the clip intact, with denormalised
   provenance (subject / sender / received-at) so the clip stays
   self-describing and the `:clips` overlay never shows a blank row
   (§4, §6.4).

---

## 1. Goal

Let a user reading a message lift a few lines out of the body — a door
code, a shipping address, a tracking number, an SKU buried in marketing
fluff — into a durable, searchable local knowledge cache, without leaving
the keyboard and without the fragment being tied to the lifetime of the
mail it came from.

The user gain: a partner who gets a building access code in a calendar
confirmation email selects the two relevant lines (`v`, `j`, `y`), and
three weeks later — after the email itself has aged out of the local
cache or been archived — types `:clips door` and reads the code in under
a second, with a one-key jump back to the original message if it still
exists.

---

## 2. Non-goals

- **Syncing clips to the server or across devices.** Clips are a
  local-only knowledge cache. inkwell holds no `Mail.ReadWrite`-style
  write scope for arbitrary server state and adds none (§8.3). The
  user's own backup of `mail.db` and `inkwell clips export` (§9) are
  the portability story.
- **Automatic extraction.** inkwell does not scan bodies and guess at
  "this looks like a door code." Every clip is an explicit user
  gesture. (A future AI-tier feature — roadmap bucket 7 — could build
  on the `clips` table, but auto-extraction is out of scope here.)
- **Editing clip *text* after save.** A clip is an immutable capture;
  only its `label` is mutable (§6.3). To revise the text, re-yank.
  This keeps provenance honest — a clip's text always equals what the
  body said at capture time.
- **Character-level or column selection.** v1 selection is **line
  range** only, matching the roadmap's "selected line range." No
  visual-block, no mid-line anchors.
- **Clipping from non-viewer panes.** v1 clips originate only from the
  message viewer body. No clipping from the list pane, calendar pane,
  or rendered headers.
- **Rich-media or attachment clips.** Text only. An attachment is saved
  via the existing attachment-save path, not as a clip.
- **Auto-eviction of clips.** Unlike the body LRU (spec 02 §3.5) and
  the body index (spec 35 §6.4), clips are deliberately-saved user
  knowledge. They are **never** silently evicted. A `[clips].max_clips`
  ceiling *refuses* new saves with a clear message rather than dropping
  old clips (§6.5).

---

## 3. Module layout

New code lands in `internal/store` (the table + API), `internal/ui`
(the selection mode + overlay), and `cmd/inkwell` (the CLI family). No
new top-level package; no new directory.

```
internal/store/
├── migrations/
│   └── 016_clips.sql                # new
├── clips.go                         # new — SaveClip, GetClip, ListClips, SearchClips,
│                                    #       DeleteClip, UpdateClipLabel, CountClips, ClipStats
├── clips_test.go                    # new — unit (CRUD, FTS, ON DELETE SET NULL, cap)
├── clips_bench_test.go              # new — perf budgets §11
├── clips_redact_test.go             # new — redaction (§8.5)
├── clips_integration_test.go        # new — build-tag integration
├── store.go                         # SchemaVersion 15 → 16; Store interface gains 8 methods
├── tabs_test.go                     # assertion at :40 updated "15" → "16"
├── rules_test.go                    # assertion at :21 updated "15" → "16"
└── sender_routing_test.go           # OR-list at :320 extended to include "16"

internal/ui/
├── viewer_select.go                 # new — visual line-selection state, extend ops, highlight render
├── viewer_select_test.go            # new — selection state transitions
├── clips_overlay.go                 # new — :clips overlay model (mirrors urlpicker.go / ListModel)
├── clips_overlay_test.go            # new — TUI e2e (filter, jump-back, delete, edit-label)
├── panes.go                         # ViewerModel gains selection fields (anchor, cursor, active)
├── keys.go                          # new binding VisualSelect ('v'); 'y' reused inside select mode
├── app.go                           # dispatchViewer: 'v' enters select; 'y' in select-mode saves clip;
│                                    # dispatchCommand: ':clips' case
└── palette_commands.go              # clips palette rows (open :clips; clip-from-selection availability)

cmd/inkwell/
├── cmd_clips.go                     # new — inkwell clips {list,search,show,delete,export}
├── cmd_clips_test.go                # new — CLI integration
├── cmd_clips_redact_test.go         # new — CLI redaction (§8.5)
└── cmd_root.go                      # AddCommand(newClipsCmd(rc))

internal/config/
├── config.go                        # new [clips] section struct
└── validate.go                      # clips validation rules (§7.1)

docs/
├── CONFIG.md                        # new [clips] section (§7)
├── architecture/overview.md         # schema-table row + module-tree row
├── THREAT_MODEL.md                  # on-disk surface row + clipboard-egress note (§8.1)
├── PRIVACY.md                       # "Where data is stored" row (§8.2)
├── user/reference.md                # v (visual select) + :clips + inkwell clips
├── user/how-to.md                   # recipe: "Save a knowledge clip"
├── PRD.md                           # §10 inventory — updated at ship
├── product/roadmap.md               # Bucket 5 row + §1.12 — updated at ship
└── specs/36-clips/plan.md           # tracking note (`docs/CONVENTIONS.md` §13)
```

Per-package `AGENTS.md` exist for `auth`, `graph`, `store`, `ui`. No
`AGENTS.md` invariant needs amending: `SaveClip` / `DeleteClip` are
**user-initiated** writes, not background cache management, and clips
hold no mail state and never touch Graph or the action queue. (Contrast
spec 35 §6.2, which had to carve out background eviction.)

---

## 4. Schema — migration 016

`internal/store/migrations/016_clips.sql`. Bumps `SchemaVersion` in
`internal/store/store.go:22` from `15` to `16`.

```sql
-- User-yanked knowledge fragments. A clip is an immutable capture of a
-- body line range; only `label` is mutable post-save. Clips OUTLIVE their
-- source message (ON DELETE SET NULL, not CASCADE) — the point is a
-- knowledge fragment that survives the email. Denormalised provenance
-- keeps the clip self-describing when the source is gone.
CREATE TABLE clips (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id         INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    source_message_id  TEXT REFERENCES messages(id) ON DELETE SET NULL,  -- nullable
    text               TEXT NOT NULL,                  -- the yanked lines, joined by '\n'
    label              TEXT NOT NULL DEFAULT '',       -- optional, user-set; mutable
    source_subject     TEXT NOT NULL DEFAULT '',       -- provenance snapshot at save time
    source_sender      TEXT NOT NULL DEFAULT '',       -- provenance snapshot (display name or address)
    source_received_at INTEGER,                        -- unix seconds; nullable
    line_start         INTEGER,                        -- 1-based body line at save time; nullable
    line_end           INTEGER,                        -- 1-based, inclusive; nullable
    saved_at           INTEGER NOT NULL                -- unix seconds
);

CREATE INDEX idx_clips_account ON clips(account_id);
CREATE INDEX idx_clips_source  ON clips(source_message_id);
CREATE INDEX idx_clips_saved   ON clips(saved_at);

-- FTS5 over clip text + label + provenance. Mirrors messages_fts
-- (001_initial.sql:142). content_rowid is the clips INTEGER PK.
CREATE VIRTUAL TABLE clips_fts USING fts5(
    text,
    label,
    source_subject,
    source_sender,
    content='clips',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER clips_ai AFTER INSERT ON clips BEGIN
    INSERT INTO clips_fts(rowid, text, label, source_subject, source_sender)
    VALUES (new.id, new.text, new.label, new.source_subject, new.source_sender);
END;

CREATE TRIGGER clips_ad AFTER DELETE ON clips BEGIN
    INSERT INTO clips_fts(clips_fts, rowid, text, label, source_subject, source_sender)
    VALUES('delete', old.id, old.text, old.label, old.source_subject, old.source_sender);
END;

-- Only `label` is mutable, so the AU trigger fires on label updates.
CREATE TRIGGER clips_au AFTER UPDATE OF label ON clips BEGIN
    INSERT INTO clips_fts(clips_fts, rowid, text, label, source_subject, source_sender)
    VALUES('delete', old.id, old.text, old.label, old.source_subject, old.source_sender);
    INSERT INTO clips_fts(rowid, text, label, source_subject, source_sender)
    VALUES (new.id, new.text, new.label, new.source_subject, new.source_sender);
END;

-- Final statement: bump the recorded schema version. The runner reads
-- this back and refuses to open if it still reads '15' (every prior
-- migration ends with this — e.g. 015_body_index.sql). Omitting it is a
-- start-up failure, not a no-op.
UPDATE schema_meta SET value = '16' WHERE key = 'version';
```

The migration runner version-gates: it skips any migration whose version
is ≤ the recorded `schema_meta` version (`internal/store/migrations.go`,
spec 02 §4), so the file is never re-executed against an already-migrated
DB and no `IF NOT EXISTS` guards are needed. (The individual `CREATE`
statements are *not* themselves re-runnable; safety comes from the
version gate, not from the SQL.)

### 4.1 Schema-version regression tests

The migration's final `UPDATE schema_meta SET value = '16'` is what the
runner reads back; without it the store fails to open with "missing
migrations: at version 15, target 16." The T1 verification confirms a
fresh apply leaves the recorded version at `16` *and* that the constant
matches.

Three existing sites assert the schema version (per `docs/CONVENTIONS.md`
§16 "new table added but the schema-version test still asserts the old
number"). All three must move 15 → 16:

- `internal/store/tabs_test.go:40` — `require.Equal(t, "15", …)` → `"16"`.
- `internal/store/rules_test.go:21` — `require.Equal(t, "15", …)` → `"16"`.
- `internal/store/sender_routing_test.go:320` — extend the OR-list to
  include `v == "16"`.

Failing to update any of the three is a known §16 review finding. The
implementing task re-greps these sites at execution time in case line
numbers have drifted.

### 4.2 Why `ON DELETE SET NULL` (not `CASCADE`)

A clip is knowledge the user lifted *out* of the mailbox. Cascading the
delete would mean "archiving the email throws away the door code," which
defeats the feature. `SET NULL` keeps the text; the denormalised
provenance columns (subject / sender / received-at) keep the row
meaningful; the `:clips` overlay renders such a clip with a "source
unavailable" affordance instead of a dead link (§6.4). `account_id`
*does* cascade — removing an account is a deliberate full teardown.

---

## 5. Viewer selection — the `v` / `y` interaction

This is the one net-new TUI surface. The viewer (`ViewerModel`,
`internal/ui/panes.go:1632`) today scrolls the body by line offset
(`scrollY`) but has **no per-line cursor and no selection**. This spec
adds a line-addressable visual mode.

### 5.1 State

`ViewerModel` gains:

```go
selectActive bool // visual line-selection mode is on
selectAnchor int  // 0-based body line where 'v' was pressed
selectCursor int  // 0-based body line the selection head is on
```

When `selectActive` is false the viewer behaves exactly as today; all new
behaviour is gated behind the flag, so no existing gesture changes.

### 5.2 Gestures

| Key | Normal viewer mode | Visual-select mode (`selectActive`) |
| --- | --- | --- |
| `v` | enter select mode; anchor = cursor = first visible body line | (no-op / re-anchor — re-anchors to current cursor) |
| `j` / `↓` | scroll body down (unchanged) | move `selectCursor` down one line, auto-scroll to keep it visible |
| `k` / `↑` | scroll body up (unchanged) | move `selectCursor` up one line |
| `G` / `gg` | jump bottom / top (unchanged) | extend selection to bottom / top |
| `y` | **URL-yank (unchanged)** | **save selection as a clip** (§5.3), then exit select mode |
| `Esc` | (existing) | cancel select mode; no clip saved |
| other viewer keys | unchanged | swallowed while in select mode (status hint shown) |

The selected line range `[min(anchor,cursor) … max(anchor,cursor)]` is
rendered in reverse video. A status-line hint shows
`-- VISUAL -- y: save clip  Esc: cancel` while active.

The collision the roadmap glossed over is resolved purely by mode: bare
`y` is URL-yank; `y` only saves a clip when `selectActive` is true.
Acceptance criterion §10 asserts the URL-yank path is untouched.

### 5.3 What a yank saves

On `y` in select mode:

1. Join the selected body lines with `\n`. Trim leading/trailing **blank**
   lines from the range (internal blanks preserved). If the trimmed text
   is empty → status "nothing to clip", exit select mode, save nothing.
2. If `len(text) > [clips].max_clip_bytes`, truncate at the byte cap and
   set a "(clip truncated)" status suffix.
3. If `CountClips ≥ [clips].max_clips` → refuse: status "clip limit
   (N) reached — delete clips with :clips", save nothing, exit select
   mode (§6.5).
4. **If `[clips].prompt_for_label` (default false)**, open a one-line
   label input now, before the insert; Enter captures the label, Esc
   captures an empty label. Default-off skips this step entirely, keeping
   the hot path two gestures (`v…y`).
5. `SaveClip` **once** with the joined text, the label from step 4 (empty
   when the prompt is off), `source_message_id` = current message, and
   denormalised provenance from the open message (`source_subject` =
   `Subject`; `source_sender` = `FromName` if non-empty else
   `FromAddress`; `source_received_at` = `ReceivedAt`; see §14),
   `line_start` / `line_end` (1-based), `saved_at = now`. The clip is
   inserted exactly once with its final label, so the `clips_au` trigger
   never fires on a fresh save.
6. If `[clips].copy_to_clipboard` (default true), also copy the clip text
   (not the label) to the OS clipboard via the existing `yanker`
   (`internal/ui/clipboard.go:46`). The clipboard write is **best-effort**:
   if it fails, the clip is still saved and the status notes the
   clipboard miss. The DB save is the primary, must-succeed action.
7. Status confirms: `clip saved (3 lines) → :clips` (and `+ clipboard`
   when copied).

---

## 6. Store API — `internal/store/clips.go`

### 6.1 Public additions to `store.Store`

```go
type Store interface {
    // ... existing methods ...

    // SaveClip inserts a clip and returns its new id. saved_at and the FTS
    // row are set by the implementation. The caller supplies already-trimmed,
    // already-byte-clamped text (the UI/CLI enforces [clips].max_clip_bytes).
    SaveClip(ctx context.Context, c Clip) (int64, error)

    // GetClip returns one clip by id, or ErrNotFound.
    GetClip(ctx context.Context, id int64) (Clip, error)

    // ListClips returns clips for the account, newest saved_at first.
    // limit 0 means a sane default page; offset paginates. (Bare-limit
    // list precedent: store.go:158/181/196; offset precedent:
    // MessageQuery.Offset, types.go:383 → LIMIT ? OFFSET ? at
    // messages.go:486. Not a Page type.)
    ListClips(ctx context.Context, accountID int64, limit, offset int) ([]Clip, error)

    // SearchClips runs an FTS5 MATCH over clips_fts (text/label/subject/sender)
    // and returns clips ordered by BM25, newest-first on ties. The query is
    // escaped by clipsMatchQuery (§6.2) — a store-local helper — before MATCH.
    SearchClips(ctx context.Context, accountID int64, q ClipQuery) ([]Clip, error)

    // UpdateClipLabel changes only the label (the sole mutable field).
    UpdateClipLabel(ctx context.Context, id int64, label string) error

    // DeleteClip removes one clip by id (and its FTS row via the AD trigger).
    DeleteClip(ctx context.Context, id int64) error

    // CountClips returns the number of clips for the account — backs the
    // [clips].max_clips ceiling check (§6.5).
    CountClips(ctx context.Context, accountID int64) (int, error)

    // ClipStats backs `inkwell clips list` summary + tests.
    ClipStats(ctx context.Context, accountID int64) (ClipStats, error)
}

type Clip struct {
    ID               int64
    AccountID        int64
    SourceMessageID  string    // "" once the source is deleted (NULL in DB)
    Text             string
    Label            string
    SourceSubject    string
    SourceSender     string
    SourceReceivedAt time.Time // zero when unknown
    LineStart        int       // 0 when unknown
    LineEnd          int
    SavedAt          time.Time
}

type ClipQuery struct {
    Query  string // raw user query; escaped by clipsMatchQuery before MATCH (§6.2)
    Limit  int    // 0 = sane default page
    Offset int
}

type ClipStats struct {
    Count       int
    Bytes       int64 // sum(length(text))
    OldestSaved time.Time
    NewestSaved time.Time
    Orphaned    int   // count with source_message_id IS NULL
}
```

`SourceMessageID` maps NULL ↔ `""`. Reads use a `sql.NullString`
scan; the field is `""` exactly when the source has been deleted.

### 6.2 SQL composition

All writes and the FTS MATCH use parameterised `?` placeholders — the
spec 17 §4.3 baseline. The user's search string is passed as a bound `?`
argument to `clips_fts MATCH ?`; a small **store-local** helper
`clipsMatchQuery(raw string) string` escapes FTS5 syntax metacharacters
(double-quoting the terms) so a stray operator can't change the query
shape. This deliberately does **not** reuse `internal/search`'s
`BuildFTSQuery` (`internal/search/local.go`): that package imports
`internal/store`, so calling up into it would be an upward import / cycle
(`docs/CONVENTIONS.md` §2 layering), and it hard-codes `messages_fts`
columns anyway. The escaper lives beside `clips.go` in `internal/store`.
This spec introduces **zero** new `// #nosec`
annotations; an explicit injection probe in `clips_test.go` feeds
`'; DROP TABLE clips; --` and a malicious FTS operator string and asserts
the table survives and the query returns no rows rather than erroring.

### 6.3 Label edit

`UpdateClipLabel` is the only mutation of an existing clip. The `clips_au`
trigger fires only on `OF label`, so re-reading or jumping to a clip never
re-writes the FTS index.

### 6.4 Source-message lifecycle

- **Permanent delete** (`D` confirmed) removes the `messages` row;
  `ON DELETE SET NULL` nulls `clips.source_message_id`. The clip and its
  provenance survive. `ClipStats.Orphaned` counts these.
- **Cache eviction** (message envelope falls out of the synced window):
  the `messages` row may be gone even though it was never user-deleted.
  Same outcome — `source_message_id` becomes NULL (FK SET NULL fires on
  any delete of the referenced row). Jump-back shows "source unavailable."
- **Jump-back** (Enter in the `:clips` overlay): if `SourceMessageID != ""`
  and `GetMessage` resolves it, load that message into the viewer and
  scroll to `LineStart` when present. Otherwise show status "source
  message no longer available" and keep the clip selected; the clip text
  and provenance remain visible in the overlay. Never crash, never block.

### 6.5 Capacity ceiling

`SaveClip` callers check `CountClips` against `[clips].max_clips` *before*
inserting (the UI in §5.3 step 3, the CLI in §9). At the ceiling the save
is **refused** with a clear message; existing clips are never evicted.
Rationale: clips are deliberately-saved knowledge; silent LRU eviction
would be data loss. The store itself does not enforce the cap (it has no
config); the cap lives at the call sites so the error message can be
user-appropriate.

### 6.6 Undo & offline behaviour

**Undo.** Saving a clip is **not** undoable — it is itself the
undo-friendly action (a clip is additive, low-cost; the user deletes it
if unwanted). **Deleting** a clip is **not** undoable either: the
confirm-gate (§8.4, `dd` / `inkwell clips delete`) is the only guard,
matching the spec 07 destructive-action convention. Clips do **not**
enter the action queue / undo stack (`internal/action`) — that queue is
for reversible *mail-state* mutations synced to Graph, and clips are
neither mail state nor synced. This is stated so "not undoable" is a
deliberate answer, not an omission (DoD spec-content rule).

**Offline.** Clips are entirely local: save, search, list, label-edit,
delete, and `:clips` browse all run against `mail.db` with **no Graph
dependency**, so every clip operation works identically offline. The
single online-only edge is jump-back to a source whose envelope is no
longer cached — and even that degrades gracefully to "source
unavailable" rather than failing (§6.4); v1 does not attempt an online
re-fetch (§13, deferred).

---

## 7. Configuration — `[clips]`

This spec **owns the `[clips]` section** of `CONFIG.md`.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | `true` | — | Master switch. When `false`, `v` is a no-op (status: "clips disabled"), `:clips` shows an empty disabled overlay, and the CLI family reports "clips disabled". Existing clips are **not** purged (contrast spec 35's body-index purge-on-disable) — disabling clips hides the feature, it does not destroy user knowledge. |
| `copy_to_clipboard` | bool | `true` | — | On yank-save, also copy the text to the OS clipboard via the existing `yanker` (OSC 52 + `pbcopy`). Best-effort; never blocks the DB save. |
| `prompt_for_label` | bool | `false` | — | When true, yank opens a one-line label input before committing the clip. Default off keeps the hot path two gestures (§5.3). |
| `max_clips` | int | `10000` | 100–1000000 | Hard ceiling on stored clips per account. At the ceiling, new saves are **refused** (no eviction — §6.5). |
| `max_clip_bytes` | int | `65536` (64 KB) | 256 B–1 MB | Per-clip text clamp. Larger selections are truncated with a status note. |

Owner: spec 36. Defaults make clips on-by-default but cheap and bounded;
unlike the body index, nothing is auto-populated — every byte stored is a
byte the user explicitly yanked.

### 7.1 Config validation

`internal/config/validate.go` adds:

- `max_clip_bytes ≤ 1 MB` and `≥ 256 B` (a clip is a fragment, not a
  document).
- `max_clips ≥ 100` (a smaller ceiling is almost certainly a typo).

Out-of-range values fail config load with the standard validation error
(consistent with existing `[cache]` / `[body_index]` validation).

---

## 8. Privacy & security — spec 17 impact

This spec adds a new on-disk surface (`clips` + `clips_fts`) holding
user-selected **decoded body fragments**, and routes those fragments
through the OS clipboard. Both are spec-17-relevant. The PR description
carries the impact line:

> spec 17 impact: new on-disk surface (`clips` table + FTS5 companion)
> holding user-yanked body fragments; new clipboard-egress path for clip
> text; THREAT_MODEL + PRIVACY updated; redaction tests added for the
> store + UI + CLI sites; no new Graph scopes.

### 8.1 Threat model update

`docs/THREAT_MODEL.md` "Threats and mitigations" gains:

> | Threat | Asset | Mitigation |
> |---|---|---|
> | Local-state exfiltration of saved clips | User-yanked body fragments in `mail.db` (`clips` + `clips_fts`) | Only user-yanked fragments are stored (no bulk auto-index); bounded by `[clips].max_clips`; deletable per-clip via `:clips`/`inkwell clips delete`; inherits FileVault + macOS user-account isolation + mode `0600` on `mail.db`. |
> | Clip text leaking via terminal clipboard / OSC 52 over SSH | Clip text in transit to the OS / remote clipboard | `[clips].copy_to_clipboard` is a documented egress; OSC 52 is the same channel already used by URL-yank (spec 04); users on untrusted multiplexers/SSH set `copy_to_clipboard = false`. |

"Accepted residual risks" gains: clips are decoded plaintext on disk by
the user's explicit gesture; the blast radius of `mail.db` exfiltration
grows by the fragments the user chose to keep.

### 8.2 Privacy doc update

`docs/PRIVACY.md` "Where data is stored" gains a `clips` row: decoded
body fragments the user explicitly yanked, plus a provenance snapshot
(subject / sender / received-at); local only, never sent to Graph;
erased per-clip via `:clips` delete or `inkwell clips delete`.

### 8.3 No new scopes

The clip text comes from the **already-rendered** body in the viewer —
material already fetched under `Mail.Read`. Saving a clip makes **no**
Graph call. The `Mail.Send`-denial CI guard (`docs/CONVENTIONS.md` §7
invariant 4, `scripts/check-no-mail-send.sh`) stays green by construction.

### 8.4 Destructive-action gate

`inkwell clips delete <id>` and the overlay `dd` confirm before deleting
(default "No"), per the spec 07 §3 / spec 10 §5.4 confirmation pattern.
`--yes` skips the prompt for scripting (spec 14 §6). There is no bulk
"delete all clips" verb in v1 (deliberately — see §10 deferred).

### 8.5 Log redaction

Clip text, label, provenance subject/sender, and source message IDs
MUST NOT appear in logs at INFO+ anywhere — store, UI, or CLI. DEBUG
diagnostics may correlate a clip to its source only via
`redact.HashMessageID` (`internal/log/redact.go:69`, added by spec 35);
clip text and label are never logged at any level. Redaction tests live
at `internal/store/clips_redact_test.go` and
`cmd/inkwell/cmd_clips_redact_test.go` and cover both sites per
`docs/CONVENTIONS.md` §11. The UI status-line strings (§5.3) show counts
and line numbers, never the clip text.

---

## 9. CLI mode — `inkwell clips`

PRD §5.12 requires a CLI answer for every TUI feature. Saving is a TUI
selection gesture, so the CLI focuses on the *knowledge-cache* half —
browse, search, inspect, delete, export:

```bash
inkwell clips list   [--limit N] [--output table|json]      # newest first; summary line from ClipStats
inkwell clips search <query> [--output table|json]          # FTS5 MATCH
inkwell clips show   <id>     [--output text|json]          # full text + provenance
inkwell clips delete <id>     [--yes]                       # confirm unless --yes
inkwell clips export [--format md|json|txt] [--output FILE]  # dump the whole cache
```

Wired exactly like `inkwell index` (spec 35): `newClipsCmd(rc)` parent in
`cmd/inkwell/cmd_clips.go`, registered via `cmd.AddCommand(newClipsCmd(rc))`
in `cmd_root.go` (alongside `newIndexCmd` at `cmd_root.go:76`). `export`
is the "knowledge cache" payoff — `--format md` emits one `##`-headed
section per clip with provenance and the fenced text, suitable for pasting
into a notes app. The CLI never echoes clip text at INFO log level (§8.5);
`show`/`export` write to stdout/file, not the log.

A CLI-side `clips add` (save from stdin / `--message-id`) is **deferred**
(§10 deferred-anchor) — the v1 save path is the viewer gesture; a headless
add is a thin follow-up.

---

## 10. Acceptance criteria

- [ ] Migration 016 creates `clips` + `clips_fts` + the three triggers;
  `SchemaVersion == 16`; the three schema-version regression sites (§4.1)
  are updated and green.
- [ ] `v` in the viewer enters visual line-selection; `j`/`k`/`G`/`gg`
  extend the range; the range renders in reverse video; `Esc` cancels
  with no clip saved.
- [ ] `y` in select mode saves a clip with the joined (blank-trimmed)
  text, `source_message_id`, denormalised subject/sender/received-at, and
  the 1-based line range; the mode exits and the status confirms with the
  line count.
- [ ] Bare `y` in **normal** viewer mode still performs URL-yank
  (existing behaviour); a regression test asserts the URL-yank path is
  unchanged.
- [ ] `[clips].copy_to_clipboard = true` also copies the selection to the
  OS clipboard; a forced clipboard failure still leaves the clip saved
  and surfaces a non-fatal status.
- [ ] `:clips` opens an overlay listing clips newest-first; typing filters
  via `clips_fts`; `Enter` loads the source message and scrolls to
  `line_start` when present.
- [ ] `Enter` on a clip whose source is gone shows "source message no
  longer available", does not crash, and keeps the clip text + provenance
  visible.
- [ ] Permanently deleting the source message leaves the clip; its
  `source_message_id` becomes NULL and `ClipStats.Orphaned` increments.
- [ ] Overlay `dd` deletes a clip (confirm); `e` edits the label;
  edits persist and re-index FTS.
- [ ] `inkwell clips list|search|show|delete|export` work; `--output json`
  is valid JSON for list/search/show; `export --format md` round-trips.
- [ ] At `[clips].max_clips`, a new save is refused with a clear message;
  no existing clip is evicted.
- [ ] No clip text, label, provenance, or message ID is logged at INFO+;
  DEBUG uses `HashMessageID`; redaction tests cover the store + CLI sites.
- [ ] `docs/THREAT_MODEL.md` + `docs/PRIVACY.md` updated (on-disk surface +
  clipboard egress); PR carries the spec-17 impact line.
- [ ] Perf budgets met by benchmarks (§11).
- [ ] `make ai-fuzz` smoke passes and a Claude-Code-oracle pass over
  `.context/ai-fuzz/run-*/REVIEW.md` finds no clip-related regression
  (TUI surface, `docs/CONVENTIONS.md` §11).

**Deferred (shipped unmet on purpose):**

- [ ] CLI `inkwell clips add` (headless save) (deferred:
  `docs/backlog.md#clips-cli-add`).
- [ ] Bulk "delete all clips" verb (deferred:
  `docs/backlog.md#clips-bulk-delete`).

---

## 11. Perf budgets

Each row has a benchmark in `internal/store/clips_bench_test.go` (or a
UI bench where noted), per `docs/CONVENTIONS.md` §5.6.

| Operation | Budget | Benchmark |
| --- | --- | --- |
| `SaveClip` (INSERT + FTS trigger) | < 10 ms p99 | `BenchmarkSaveClip` |
| `SearchClips` over 10 000 clips | < 50 ms p99 | `BenchmarkSearchClips_10k` |
| `ListClips` first page (50) over 10 000 clips | < 20 ms p99 | `BenchmarkListClips_10k` |
| `:clips` overlay open (load + render first page) | < 100 ms | `BenchmarkClipsOverlayOpen` in `internal/ui/clips_overlay_test.go` |
| Visual-mode selection extend (keystroke → re-render) | one viewer-body re-render per keystroke; no new allocation hot path | no new benchmark — covered by the visible-delta assertion in `viewer_select_test.go` and the ai-fuzz layout-hold pass (§12) |

---

## 12. Testing strategy

- **Store CRUD, FTS MATCH, `ON DELETE SET NULL`, injection probe, cap
  check** — TDD unit (`clips_test.go`), `-race`.
- **Visual selection state transitions** (anchor/cursor math, blank
  trimming, empty-selection guard) — TDD unit (`viewer_select_test.go`).
- **`v…y` gesture, `:clips` filter, jump-back, source-gone path, label
  edit, delete** — TUI e2e (`clips_overlay_test.go`, teatest) with
  visible-delta assertions on rendered frames (not mock-shape
  assertions, per `docs/TESTING.md`).
- **CLI list/search/show/delete/export** — integration (`cmd_clips_test.go`).
- **Redaction** — unit at each log site (`*_redact_test.go`).
- **Perf** — benchmarks (§11).
- **Closing ritual** — `make ai-fuzz` + Claude-oracle over `REVIEW.md`
  (TUI surface).

---

## 13. Failure modes

| Scenario | Behaviour |
| --- | --- |
| `v` then `y` on a single all-blank line | "nothing to clip"; no save; exit select mode. |
| Selection text exceeds `max_clip_bytes` | Truncate at the byte cap; status "(clip truncated)"; save the truncated text. |
| At `max_clips` ceiling | Save refused; status "clip limit (N) reached — delete clips with :clips". |
| `copy_to_clipboard` set but clipboard write fails (headless / no TTY) | Clip still saved; status notes "(clipboard unavailable)". |
| `:clips` `Enter` on clip with NULL source | "source message no longer available"; clip text/provenance still shown; no crash. |
| `:clips` `Enter` on clip whose source id is non-NULL but evicted from cache | Same as NULL source — "source message no longer available". (Future: offer re-fetch via Graph — out of scope.) |
| FTS query with operator/quote metacharacters | Escaped by the store-local `clipsMatchQuery` helper (§6.2); returns matches or empty, never an error or injection. |
| `[clips].enabled = false` | `v` no-op with status; `:clips` overlay disabled; CLI reports disabled; existing clips untouched. |
| `inkwell clips show <id>` for missing id | Non-zero exit with "clip <id> not found". |
| Concurrent save while overlay open | Overlay reflects the new clip on next open/refresh; no live push in v1. |

---

## 14. Assumptions

- Technical: the viewer body is available as an addressable slice of
  rendered lines at selection time (the same lines `scrollY` indexes).
  (source: `internal/ui/panes.go:1632` `ViewerModel`, probe by Explore
  2026-05-31.)
- Technical: `redact.HashMessageID` exists and is one-way/stable.
  (source: `internal/log/redact.go:69`, probe 2026-05-31.)
- Technical: the OSC 52 + `pbcopy` `yanker` is reusable for arbitrary
  text, not just URLs. (source: `internal/ui/clipboard.go:46`
  `yanker.Yank(data string)`, probe 2026-05-31.)
- Technical: `content_rowid='id'` is correct because `clips.id INTEGER
  PRIMARY KEY AUTOINCREMENT` *is* the table's `rowid` alias, so the
  external-content FTS5 row id resolves to it. (Note: this is **not** the
  same as `messages_fts`, which uses `content_rowid='rowid'` because
  `messages.id` is `TEXT` and therefore not a rowid alias —
  `001_initial.sql:36`. We follow the FTS5 mechanism, not that specific
  precedent.) (source: SQLite FTS5 external-content docs; probe 2026-05-31.)
- Technical: the open message (`ViewerModel.current *store.Message`,
  `panes.go:1633`) carries the provenance fields the clip denormalises —
  `Subject`, `FromName` (display name), `FromAddress`, `ReceivedAt`
  (`internal/store/types.go:38,40,41,45`). `SourceSender` stores
  `FromName` when non-empty, else `FromAddress`. These are populated at
  viewer time because the envelope is the list/viewer's own data.
  (source: `internal/store/types.go`, probe 2026-05-31.)
- Product: clips are local-only and survive their source message; no
  server sync in v1. (source: roadmap §1.12 "knowledge fragment outside
  the email"; user direction 2026-05-31.)
- Product: clips default **on** (opt-out), unlike the body index which is
  opt-in, because nothing is auto-populated — every clip is an explicit
  gesture. (source: this spec's §0.1 rationale; confirm with owner.)
