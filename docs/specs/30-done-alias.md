# Spec 30 — "Done" alias for archive

**Status:** Ready for implementation.
**Depends on:** Spec 04 (KeyMap, `BindingOverrides`,
`ApplyBindingOverrides`, `findDuplicateBinding`), Spec 07 (existing
`Triage.Archive` / `BulkArchive` / `ThreadMove(...,"archive")` paths
— this spec calls them, does not extend them), Spec 10 (`;a`
apply-to-filtered chord — this spec adds `;e` as a sibling), Spec 20
(`T a` thread chord — this spec adds `T e` as a sibling and edits the
chord-pending hint string), Spec 22 (command palette — this spec
re-titles the existing `archive` palette row dynamically), Spec 14
(CLI mode — this spec adds `inkwell thread done` as a sibling of
`inkwell thread archive`).
**Blocks:** None. Bucket 3 successor work (custom actions framework,
screener, watch mode) does not depend on this verb cosmetic.
**Estimated effort:** Half a day.

### 0.1 Spec inventory

"Done" alias is item 4 of Bucket 3 in `docs/ROADMAP.md` §0 and
backlog item §1.23. Slots 27 (custom-actions framework, row 1), 28
(screener, row 2), and 29 (watch mode, row 3) are all authored and
ready for implementation. Spec 30 takes the next sequential slot for
the row 4 verb. The four Bucket-3 specs are independent — spec 30
is half a day of binding/branding work and is unblocked by the
larger custom-actions design. The PRD §10 spec inventory adds a
single row for spec 30.

---

## 1. Goal

Two adjacent surface aliases on the existing archive verb:

- **A second key, `e`,** that does what `a` already does — moves the
  focused message to the well-known Archive folder. Both keys remain
  bound by default; users can collapse to one or the other via
  `[bindings].archive`.
- **A configurable label,** `[ui].archive_label = "archive" | "done"`,
  that flips every user-visible string for the archive verb between
  "Archive" and "Mark done". The underlying action, undo path,
  destination folder, and Graph round-trip are identical regardless.

The **action does not change**. There is no new action type, no new
executor method, no new SQL, no new Graph endpoint, no new sync path,
no new perf budget, no new privacy surface, no new redaction site.
Spec 30 is **bindings + branding**. Roadmap §1.23 explicitly scopes it
that way ("mostly a binding/branding question").

### 1.1 What does NOT change

- The action queue (spec 07): archive still routes through
  `store.ActionMove` with `destination_folder_alias = "archive"`. The
  inverse-restore-to-source-folder undo path (spec 07 §11) is
  untouched. `u` reverses an archive whether triggered by `a`, `e`,
  `:done`, `:archive`, palette, `T a`, `T e`, `;a`, `;e`, or any CLI
  surface.
- The `[triage].archive_folder` config key. Renaming the *destination*
  is out of scope; only the *verb's display string* is configurable.
- Pattern language (spec 08). No new predicate. `:filter ... --action
  archive` keeps its name (CLI is action-typed, not verb-branded).
- Spec 23 routing. The "Imbox / Feed / Paper Trail / Screener"
  destinations are independent of this verb cosmetic.
- The reply / forward bindings (`r` / `f`) in the viewer pane.
  `e` is unbound today in both list and viewer; adding it as an
  alternate key for `Archive` does not collide with any pane-scoped
  meaning (see §3.1 audit).
- Confirmation gates. Single-message archive does not confirm; bulk
  archive (`;a` / `;e`) and thread archive (`T a` / `T e`) inherit
  the existing confirm-threshold logic (spec 07 §10, spec 20 §3).

## 2. Prior art

### 2.1 Terminal clients

- **mutt / neomutt** — no built-in archive verb. Users bind `s` (save
  to folder) to a macro that saves to their Archive folder. No "done"
  framing in the canonical config.
- **aerc** — `A` archives (configurable; `:archive flat|year|month`
  shapes the destination tree). No "done" alias by default.
- **alot / astroid (notmuch)** — tag-based; the documented GTD-with-
  notmuch idiom uses `+done` as a tag (or `-inbox` as the
  archive-equivalent). The `+done` convention is the closest direct
  precedent for a keyboard-driven "Done" verb in a power-user mail
  client.

### 2.2 Web / desktop clients

- **Gmail (web)** — `e` archives, since the original 2004 launch.
  This is the dominant precedent: `e` is the keyboard convention for
  "archive" across the Google-influenced ecosystem.
- **Google Inbox (2014–2019)** — popularised the **"Done"** label.
  The check-mark gesture / `e` keybinding mapped to archive but was
  framed as task completion. Inbox's shutdown spread the "Done"
  framing to HEY and Superhuman's reminders.
- **HEY (37signals)** — does not expose a Gmail-style archive verb.
  Triaged mail is dismissed from the Imbox; "Done" is implicit in the
  flow (the user is finished with a thread, not custodially filing
  it). Spec 30 borrows the *philosophy* without copying the absent
  archive verb.
- **Superhuman** — `E` archives; branded "Archive" in the palette.
  Gmail-mode users get lowercase `e`.
- **Apple Mail** — `⌃⌘A` archives when the account supports it. No
  "Done" framing in the UI.
- **Outlook (web / desktop)** — `Backspace` archives on web; desktop
  is configurable. "Sweep" and "Categorise" are Microsoft's triage
  primitives. No "Done" verb in the official UI.
- **Fastmail** — `e` archives, matches the Gmail convention. No
  "Done" framing.

### 2.3 Design decision

Inkwell takes the lowest-cost path that satisfies both audiences:

- **Default keybinding stays `a`** for archive (continuity with
  every prior inkwell version + `[triage].archive_folder = "archive"`
  default + spec 07 / spec 22 / spec 25 documented bindings). `e` is
  added as a **second** default key, not a replacement.
- **The verb label is configurable**, not the binding. Users who
  prefer the "Done" framing flip a single switch
  (`[ui].archive_label = "done"`) and every status-bar toast,
  palette title, hint string, help-bar label, and CLI subcommand
  description rebrands. Users who prefer "Archive" change nothing.
- **Both labels coexist in the palette synonyms** (`"archive"` and
  `"done"`) regardless of the setting, so palette fuzzy-match works
  for either vocabulary.
- **No new keymap field.** Adding `Done key.Binding` would duplicate
  every dispatch site in `app.go` (single list, single viewer, bulk
  chord, thread chord) for zero semantic gain — the action is the
  same. We add `e` as an alternate key string on the existing
  `Archive` binding and lean on `key.Matches` to dispatch unchanged.

## 3. Keybinding — `e` as alternate for `Archive`

### 3.1 Availability audit

Today's `DefaultKeyMap` (`internal/ui/keys.go:179-244`) does not name
`e` in any binding field's `Keys()`. The `findDuplicateBinding` scan
(`internal/ui/keys.go:342`) covers every distinct binding field; `e`
appears in no field's `Keys()`.

However, two pane-scoped dispatchers handle `e` directly via
`string(msg.Runes) == "e"` switch arms, **outside** the keymap. Both
must be reckoned with:

| Pane    | File:Line               | Handler                                         | Spec 30 disposition                                                |
| ------- | ----------------------- | ----------------------------------------------- | ------------------------------------------------------------------ |
| viewer  | `app.go:5563-5566`      | `m.viewer.ToggleQuotes()` (alternative to `Q`) | **REMOVED.** Spec 30 deletes the `e` arm; `Q` remains canonical.   |
| folders | `app.go:3927-3930`      | `m.startRuleEdit(ss)` on the focused saved-search | **KEPT, pane-scoped.** Folders pane resolves `e` first, before keymap dispatch reaches the Archive binding. |

Pane-scoping rule introduced by spec 30: **`e` is `Archive` in the
list and viewer panes; `e` is "edit saved-search rule" in the
folders pane.** This follows the existing pane-scoping precedent
(`r` is `MarkRead` in list / `Reply` in viewer; `f` is `ToggleFlag`
in list / `Forward` in viewer). Spec 04 §5 calls out pane-scoped
bindings; spec 30 adds one more pane-scoped key without expanding
the framework.

The viewer's `e` quote-toggle was an undocumented alternative to `Q`
(spec 05; landed at v0.15.x) — `Q` continues to work, the existing
`TestQuoteToggleViaEKey` test (`dispatch_test.go:5028-5037`) is
**deleted** in the same commit, and `TestQuoteToggleViaQKey`
(adjacent, lines 5018-5027) confirms the canonical binding is
preserved. The viewer help text renders no `e` reference today, so
no doc churn.

The folders-pane `e` (rule-edit, spec 11) is preserved because the
folders dispatcher's switch consumes `e` before falling through to
the keymap-match cascade. No code change is required to preserve it;
spec 30's only obligation is to **document the rule** in spec
04-style language and add a regression test
(`TestFoldersPaneEStillEditsSavedSearchRule`).

Modes that consume rune input directly — Compose (spec 15), Search
prompt (spec 06), Command bar (`:`), Filter prompt (spec 10) — all
take precedence over the keymap, so `e` typed inside any of those
prompts is text input, not a triage trigger. Same shape as `a`.

### 3.2 Default change

```go
// internal/ui/keys.go:209
Archive: key.NewBinding(key.WithKeys("a", "e")),
```

The default goes from `key.WithKeys("a")` to `key.WithKeys("a",
"e")`. There is no existing `key.WithHelp` to replace; the help text
is produced separately (see §5.7).

```go
// internal/config/defaults.go:90
Archive: "a,e",
```

The defaults-config string MUST be updated in lockstep. Without this
change, `ApplyBindingOverrides` will treat the default config's
`Archive: "a"` as a non-empty override and silently overwrite the new
`["a","e"]` keymap default with `["a"]` — exactly the regression
that the comma-separated alternates pattern at lines 76-79 (`Up:
"k,up"`, etc.) exists to prevent. This is finding §3 of the
adversarial review and is the only "missed default in two places"
landmine the spec must explicitly address.

### 3.3 Override behaviour

The existing `[bindings].archive` config key continues to control the
binding. Override semantics are spec 04 §17's comma-separated alternates:

- `archive = "a,e"` (default, equivalent to no override)
- `archive = "a"` (only `a` archives; `e` is freed)
- `archive = "e"` (only `e` archives; `a` is freed)
- `archive = "x"` (only `x` archives — for users who want a different key entirely)

`findDuplicateBinding` already detects when an override collides with
another bound action (e.g. `archive = "d"` while `delete = "d"`).
This spec does not change that scan.

### 3.4 No new field

`BindingOverrides` does not gain a `Done` field. The user expectation
"`e` is my archive key" is satisfied by leaving `[bindings].archive`
at its default. A `[bindings].done = "e"` would suggest a separate
action and is rejected: there is no separate action.

## 4. Verb label — `[ui].archive_label`

### 4.1 Config key

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `archive_label` | string | `"archive"` | `"archive"`, `"done"` | Verb used for the archive action in user-visible strings (status-bar toast, palette title, hint strings, help label, fullscreen body hint, filter status bar, bulk pending hint). Underlying action and destination folder are unchanged. Spec 30 §4. |

Lives in `[ui]`. Owner spec 30. Validation in
`internal/config/validate.go`: reject any value other than the two
literals; emit `config <path>: ui.archive_label must be one of
"archive" or "done"` on load. App refuses to start (CLAUDE.md §9).

### 4.2 Branding helper

```go
// internal/ui/labels.go (new file).

// archiveVerbLower returns the imperative-lowercase verb for the
// archive action ("archive" or "done") for use in hint strings.
// archiveVerbTitle returns the title-cased form ("Archive" or "Done")
// for use in palette titles, toasts, and confirm modals. Both honour
// the [ui].archive_label config; the default is "archive" / "Archive".
//
// Single source of truth: every user-visible archive-verb string
// passes through one of these two helpers. Tests assert the helper
// is the only producer (grep audit in TestNoBareArchiveStringsRemain).

type ArchiveLabel string

const (
    ArchiveLabelArchive ArchiveLabel = "archive"
    ArchiveLabelDone    ArchiveLabel = "done"
)

func archiveVerbLower(label ArchiveLabel) string  // "archive" | "done"
func archiveVerbTitle(label ArchiveLabel) string  // "Archive" | "Done"
```

`ArchiveLabel` is a typed string defined in `internal/ui/labels.go`
(co-located with the helpers; the UI is the only consumer of the
verb strings). The config struct exposes the value as a plain string
in `UIConfig` (`internal/config/config.go` next to other `[ui]`
fields — there is no separate `internal/config/ui.go` today and
spec 30 does not introduce one); validation in
`internal/config/validate.go` rejects values other than `"archive"`
or `"done"` and emits the friendly error per §4.1. The UI converts
at boundary: `Model.archiveLabel = ArchiveLabel(cfg.UI.ArchiveLabel)`.
Helpers take the typed value (not the `Model`) so they can be called
from pure render-pass functions (palette command rows, status-bar
formatters) without coupling.

### 4.3 Sites that call the helper

Every user-visible archive-verb string is updated to pass through
`archiveVerbLower` or `archiveVerbTitle`. The exhaustive site list,
verified by grep against the codebase as of 2026-05-08:

| Site | File:Line | Helper |
| ---- | --------- | ------ |
| `triageDoneMsg` success branch (`"✓ archive · u to undo"` → `"✓ done · u to undo"`) | `app.go:1974` (filter rerun) AND `app.go:1986` (search rerun). The no-filter branch at `app.go:1989-1991` only reloads the list and does **not** write `engineActivity` — only two sites need the helper. | `archiveVerbForName(msg.name, m.archiveLabel)` |
| `triageDoneMsg` failure branch (`"<name>: <err>"`) | `app.go:1950` | `archiveVerbLower` when `msg.name == "archive"` |
| `removed` predicate that clears the viewer when archive succeeded | `app.go:1957` | unchanged — internal `msg.name == "archive"` check, not user-visible |
| Bulk dispatch arm | `app.go:3804` (`case "archive":`) | unchanged — internal action-type string |
| `confirmBulk("archive", …)` formatter (modal text + cross-folder suffix per spec 21) | `app.go:3713-3776` | `archiveVerbTitle` for the verb segment |
| `;a` apply-to-filtered chord arm | `app.go:4035-4036` (`case "a":`) | dispatch is unchanged (action stays "archive"); a sibling `case "e":` arm is added in §9.6 — no helper call |
| Spec 20 chord-pending hint (list pane chord entry) | `app.go:4129` (string literal) | unchanged — chord hint uses key glyphs, not verb labels. The string is rewritten in §5.4 to insert `/e` after `/a`. |
| Spec 20 chord dispatch arm (list) | `app.go:4152-4153` (`case "a":`) | dispatch is unchanged; a sibling `case "e":` arm is added in §9.5 |
| Palette dispatch (`runTriage("archive", …)` from palette row) | `palette_commands.go:117` | toast routes through `triageDoneMsg`; no extra helper call needed at the palette site |
| Palette → bulk confirmation status hint ×2 | `palette_commands.go:436` AND `palette_commands.go:441` (both literal `"bulk: press d (delete) or a (archive) — esc to cancel"`) | `archiveVerbLower` |
| Palette single-message row title | `palette_commands.go:109` (`Title: "Archive message"`) | dynamic per §5.5 |
| Palette thread row title | `palette_commands.go:269` (`Title: "Archive thread"`) | dynamic per §5.5 |
| Palette synonyms (single-message row) | `palette_commands.go:111` (`Synonyms: []string{"done", "file"}`) | widened to `["done","file","archive"]` per §5.5 |
| Palette synonyms (thread row) | `palette_commands.go:269` neighbourhood (no `Synonyms` field today) | added as `Synonyms: []string{"done","file","archive"}` per §5.5 |
| Single-message archive — viewer-current path A | `app.go:2098-2103` (calls `runTriage("archive", …)` at `:2101`) | dispatch is unchanged; the toast format work happens centrally in `triageDoneMsg` per row 1 |
| Single-message archive — viewer pane handler | `app.go:5449-5454` (calls `runTriage("archive", …)` at `:5452`) | dispatch is unchanged; toast format central |
| Single-message archive — list pane / confirm-resolution path | `app.go:4303-4310` (`runTriage("archive", sel, ListPane, …)` at `:4308`) | dispatch is unchanged; toast format central. Spec 30 does NOT extend the action call sites — only the central formatter learns the label. |
| Filter status bar | `app.go:6224` (`"filter: %s · matched %d%s · ;d delete · ;a archive · :unfilter"`) | `archiveVerbLower` for the `;a <verb>` segment |
| Bulk pending hint | `app.go:6226` (`"bulk: press d (delete) or a (archive) — esc to cancel"`) | `archiveVerbLower` |
| List pane key hint (status bar) | `app.go:6286` (`{{"a", "archive"}}`) | `archiveVerbLower` |
| Viewer pane key hint (status bar) | `app.go:6288` (`{{"a", "archive"}}`) | `archiveVerbLower` |
| Fullscreen body hint | `app.go:6112` (`"… d  delete  ·  a  archive  ·  y  yank URL"`) | `archiveVerbLower` |
| Help overlay row | `help.go:84` (`{keysOf(km.Archive), "archive"}` — hardcoded literal) | `archiveVerbLower` plumbed in via §5.7 (the help-overlay build function gains an `archiveLabel ArchiveLabel` parameter) |
| Spec 20 chord dispatch (viewer) | `app.go:5618-5619` (`case "a":`) | dispatch unchanged; sibling `case "e":` arm added in §9.5 |
| Spec 20 chord-pending hint (viewer) | `app.go:5595` (string literal) | rewritten in §5.4 to insert `/e` after `/a` |

The grep that produced this table:
`grep -nE 'runTriage\("archive"|"a (archive)"|"archive"|"a \(archive\)"' internal/ui/`. The list above is the
authoritative spec-30 site enumeration; an implementer should re-run
this grep before making changes and reconcile any drift.

### 4.4 What does NOT change

- **The CLI `--action` flag value** (`inkwell filter ... --action archive`)
  stays `"archive"`. CLI flag values are an *interface contract*, not
  user-visible verb-branding. Renaming would break shell aliases and
  scripts in the wild.
- **Action type names** (`store.ActionMove`,
  `destination_folder_alias = "archive"`). These are internal
  identifiers.
- **`[triage].archive_folder`** key name. Same reason: config-key
  contract.
- **Pattern AST keywords**. Spec 08 has no `archive` keyword today;
  if it ever adds one, the keyword is parser-level, not branded.
- **Log messages**. The `add_category` / `archive` action-type log
  fields stay the canonical names. ARCH §12 redaction discipline is
  unchanged. (Branding is for the *user*, not for log readers.)

## 5. Surface changes

### 5.1 KeyMap construction (no runtime help-text rewrite)

`ui.New(...)` constructs the `KeyMap`. Today it calls
`DefaultKeyMap()` then `ApplyBindingOverrides(...)`. Spec 30 does
**not** mutate `km.Archive`'s `Help()` text at runtime — there is
no existing `key.WithHelp` on the Archive default to begin with, and
the only consumer that reads binding help text is the help overlay
(`help.go:84`), which today bypasses `b.Help()` entirely and uses a
hardcoded literal description. Plumbing the label through that
specific consumer (§5.7) is simpler than threading a runtime help
mutation through every binding.

`Model.archiveLabel` is set once at `ui.New` from
`cfg.UI.ArchiveLabel`; it is a value, not a pointer, and never
mutates over a session (CLAUDE.md §9: no hot reload). All sites in
§4.3 read `m.archiveLabel` and call `archiveVerbLower(...)` /
`archiveVerbTitle(...)` at format time.

### 5.2 Status-bar toast formatting

The toast formatter for single-message triage actions is the
`triageDoneMsg` handler at `app.go:1948-1992`. Today it emits:

| Path        | Format string                                  | Resulting text             |
| ----------- | ---------------------------------------------- | -------------------------- |
| Success     | `fmt.Sprintf("✓ %s · u to undo", msg.name)`    | `"✓ archive · u to undo"`  |
| Failure     | `fmt.Errorf("%s: %w", msg.name, msg.err)`      | `"archive: <err message>"` |

Three lines emit the success format (lines 1974, 1986, plus the
post-1988 no-filter branch — actually the no-filter branch only
reloads the list and does not write `engineActivity` because the
list reload re-renders the toast slot from the action queue's
optimistic-apply state; see comment at `app.go:1989-1991`).

Spec 30 introduces a label branch keyed on `msg.name == "archive"`:

```go
// app.go:1974 / 1986 — success format helper:
verb := msg.name
if msg.name == "archive" {
    verb = archiveVerbLower(m.archiveLabel) // "archive" or "done"
}
m.engineActivity = fmt.Sprintf("✓ %s · u to undo", verb)

// app.go:1950 — failure format:
verb := msg.name
if msg.name == "archive" {
    verb = archiveVerbLower(m.archiveLabel)
}
m.lastError = fmt.Errorf("%s: %w", verb, msg.err)
```

A small helper `archiveVerbForName(name string, label ArchiveLabel) string`
in `internal/ui/labels.go` factors the `if name == "archive"` branch.
Resulting text:

```
[archive_label = "archive"]   ✓ archive · u to undo       archive: <err>
[archive_label = "done"]      ✓ done · u to undo          done: <err>
```

This matches the existing `<verb>: <err>` pattern used for
delete / move / mark-read / etc. Spec 30 does **not** invent a
"failed to archive: <err>" wording; the existing convention is
preserved and the only change is the verb token.

Bulk and thread variants flow through `bulkResultMsg` /
`threadResultMsg` handlers (spec 09 §6, spec 20 §6) which use the
same `<verb>` token for their `"✓ <verb> N messages"` /
`"⚠ <verb> X/N succeeded — Y failed"` shapes. The spec extends the
same `archiveVerbForName` branch to those handlers; the "thread" /
"messages" / "across N folders" wrappers are unchanged.

### 5.3 Cmd-bar verbs (`:archive`, `:done`)

Today there is **no** `:archive` cmd-bar verb. Spec 30 adds two:

| Command   | Behaviour                                                                |
| --------- | ------------------------------------------------------------------------ |
| `:archive`| Archive the focused message (same as pressing `a` / `e`).                |
| `:done`   | Identical to `:archive` — provided as a vocabulary alias.                |

Both verbs dispatch through `runTriage("archive", *cur, ListPane,
…)` exactly like the keybinding handlers. Both are available
regardless of `archive_label`. Reasoning: a user who flips the label
to "done" still benefits from `:archive` working (muscle memory from
prior inkwell versions, or copy-pasted instructions on the web); a
user who keeps the label as "archive" still benefits from `:done`
working (vocabulary discoverability for HEY / Inbox emigrés).

Both verbs require a focused message; on the empty list they print
`<verb>: no message focused` to the status bar, mirroring the spec 22
empty-state pattern. The verb in the error string is the literal
typed verb (`:archive` → `archive: no message focused`; `:done` →
`done: no message focused`), not the configured label, because the
error refers to the command the user just typed.

Implementation: two `case` arms in `dispatchCommand`
(`internal/ui/app.go` cmd-bar dispatcher) calling a shared
`m.runArchiveOnFocused()` helper.

### 5.4 Thread chord (`T e` as sibling of `T a`)

Spec 20's chord dispatcher uses a `switch string(msg.Runes)` over
the rune typed after `T`. Today the relevant arm is:

```go
// app.go:4152-4153 (list pane) and app.go:5618-5619 (viewer pane):
case "a":
    return m, m.runThreadMoveCmd("archive", sel.ID, "", "archive")
```

Spec 30 changes it to a fall-through:

```go
case "a", "e":
    return m, m.runThreadMoveCmd("archive", sel.ID, "", "archive")
```

This is the simplest correct change. Honouring user overrides of
`[bindings].archive` here (e.g. `[bindings].archive = "x"` so `T x`
also archives) would require restructuring the chord dispatcher
from a `string(msg.Runes)` switch to a `key.Matches` cascade — a
broader refactor. **Spec 30 explicitly does not do that.** The chord
matches the literal default keys `a` / `e`; users who override
`[bindings].archive = "x"` get the override for the single-message
binding but not for the thread chord. Edge case noted in §6.

The chord-pending status hint string at TWO sites:

```
"thread: r/R/f/F/d/D/a/m/l/L/s/S  esc cancel"
```

becomes:

```
"thread: r/R/f/F/d/D/a/e/m/l/L/s/S  esc cancel"
```

(Insert `/e` after `/a`; preserve every other glyph.) The hint sits
at `app.go:4129` (list-pane chord entry) and `app.go:5595` (viewer-
pane chord entry); the dispatch arms are at `app.go:4152` and
`app.go:5618`. **Both pairs are edited independently** — the hint
strings at 4129/5595 (literal change), and the dispatch arms at
4152/5618 (`case "a":` → `case "a", "e":`). All four sites are
called out as separate DoD bullets in §9.5. Spec 25's "edit both
sites" precedent applies for the hints; spec 30 doubles it for the
dispatch arms.

### 5.5 Palette command rows

The existing archive palette rows (`internal/ui/palette_commands.go`):

```go
{
    ID: "archive", Title: "Archive message",
    Binding: keysOf(km.Archive),
    Synonyms: []string{"done", "file"},
    …
}
{
    ID: "thread_archive", Title: "Archive thread",
    Binding: "T " + keysOf(km.Archive),
    …
}
```

Spec 30 changes the title to be label-aware:

| `archive_label` | Single-row title  | Thread-row title       |
| --------------- | ----------------- | ---------------------- |
| `"archive"`     | `Archive message` | `Archive thread`       |
| `"done"`        | `Mark done`       | `Mark thread done`     |

Synonyms become `[]string{"done", "file", "archive"}` for both rows
**regardless of the label** — palette fuzzy match works for either
vocabulary. The current state asymmetry must be preserved in the
DoD: the single-message row has `Synonyms: []string{"done", "file"}`
today; the thread row has no `Synonyms` field today. Both rows land
on `["done", "file", "archive"]` after this change. The `ID` field
stays `"archive"` / `"thread_archive"`; IDs are stable identifiers
(used by tests and any future palette-binding indirection).

`keysOf(km.Archive)` already renders comma-separated keys, so users
see `a, e` in the binding column when both keys are bound, or `a`
alone after a `[bindings].archive = "a"` override.

The palette row's `Available.Why` text (rendered when the row is
unavailable, e.g. no message focused) also routes through
`archiveVerbLower(m.archiveLabel)` so a user with `archive_label =
"done"` sees `done: no message focused` rather than a stray
`archive:` token.

### 5.6 CLI

Today: `inkwell thread archive <conversation-id>` is the sole
thread-archive subcommand. Spec 30 adds `inkwell thread done` as a
**Cobra alias** of the same command:

```go
// cmd/inkwell/cmd_thread.go — newThreadArchiveCmd():
cmd := &cobra.Command{
    Use:     "archive <conversation-id>",
    Aliases: []string{"done"},
    Short:   "Archive an entire thread (alias: done)",
    …
}
```

Cobra aliases are first-class: `inkwell thread done abc123` invokes
the same RunE. Help text for `inkwell thread --help` lists both
forms. The flag set (`--output text|json`) is identical because
Cobra aliases share flags.

The `--action archive` flag on `inkwell filter` is **not aliased**.
CLI flag values are a stable contract; renaming would break shell
scripts. Users get vocabulary parity in the TUI; the CLI keeps
`archive` as the canonical action-type identifier.

There is no top-level `inkwell archive <message-id>` CLI subcommand
today (only `inkwell thread archive` exists). Spec 30 does **not**
add one. Single-message archive from the headless harness has no
existing surface; that is a separate feature, out of scope for the
"Done alias" spec.

### 5.7 Help overlay (`?`)

The help overlay (`internal/ui/help.go`, function
`buildHelpSections`) renders binding rows from a hardcoded slice.
The Archive row at `help.go:84` is:

```go
{keysOf(km.Archive), "archive"},
```

Spec 30 plumbs the configured label through. `buildHelpSections`
gains a second parameter:

```go
func buildHelpSections(km KeyMap, archiveLabel ArchiveLabel) []helpSection {
    …
    {keysOf(km.Archive), archiveVerbLower(archiveLabel)},
    …
}
```

Every existing call site (`internal/ui/help.go` itself, plus any
test) is updated to pass the label. The two-arg signature is the
smallest change that makes the help overlay reflect the label
without regressing other rows.

Result:

```
[archive_label = "archive"]    a, e   archive
[archive_label = "done"]       a, e   done
```

### 5.8 List-row indicators

Unchanged. There is no archived-state indicator on list rows today
(spec 25 §5.2 audit confirmed). Archived messages are simply not in
the inbox folder anymore. Spec 30 adds none.

## 6. Edge cases

| Case | Behaviour |
|------|-----------|
| User sets `[bindings].archive = ""` (empty) | Spec 04 already treats empty as "leave default"; both `a` and `e` remain bound. To unbind entirely, the user sets `archive = "ctrl+disabled"` or similar bogus key — but the duplicate-binding scan does not enforce non-emptiness, so this is the existing contract. |
| User sets `[bindings].archive = "e"` (drops `a`) | Only `e` archives. The chord-pending hint still shows `a/e` (§5.4). The fullscreen body hint and list/viewer pane hints render `e archive` (or `e done`). The palette binding column renders `e`. Spec-30 hint generators read `m.keymap.Archive.Keys()` and join with `/`. |
| User sets `[bindings].archive = "x"` and `[bindings].next_pane = "x"` | `findDuplicateBinding` rejects with `bindings: key "x" bound to multiple actions`; app refuses to start. Same gate as today. |
| User sets `[ui].archive_label = "DONE"` (uppercase) | Validation rejects: `archive_label must be one of "archive" or "done"`. Strict literal match. |
| User sets `[ui].archive_label = "complete"` | Same — validation rejects. We deliberately limit to two values; adding more values is a roadmap question, not an open extension point. |
| User pressed `e` in inkwell v0.53.0 (before this spec ships) | No-op (`e` was unbound). Post-upgrade, `e` archives. Documented in the release notes. |
| User has a chord buffer mid-`T` and presses `e` | Resolves the chord as `T e` → archive thread. Same as `T a` today. |
| Focused message is a draft (in Drafts folder) and user presses `e` / `:done` / `T e` | Identical to `a` / `:archive` / `T a` today: the move-to-Archive PATCH succeeds against Graph; the draft moves out of Drafts and into Archive. Inkwell's compose flow opens drafts only from the Drafts folder, so an "archived draft" is reachable via the Archive folder list view but cannot be re-edited via spec 15's compose entry path. Spec 30 inherits this existing behaviour without modification. |
| User overrides `[bindings].archive = "x"` and presses `T x` | The chord arm matches the literal default keys `a` / `e` (§5.4). `T x` falls through to the chord's "unknown verb" path (silent cancel + clear chord state). Documented as a known v1 quirk; honouring overrides in chord arms is a broader spec 20 refactor that spec 30 does not pursue. The single-message `x` (set via override) still archives via `key.Matches`. |
| `:done` on a focused message inside a multi-message thread | `:done` is **single-message scoped**. Only the focused message moves to Archive; the rest of the thread stays where it is. Users wanting whole-thread archive use `T e` / `T a`. The cmd-bar verb does not implicitly look up thread context. The HEY framing ("the thread is done with you") is not adopted at the cmd-bar level — that would require a separate `:thread done` verb, which is out of scope. |
| User runs `:done` while the cursor is on an empty folder | `done: no message focused`. Status-bar transient (5s default per `[ui].transient_status_ttl`). |
| User runs `:done` while focused message is already in Archive folder | The action queue still dispatches a `Move` with destination = archive. Graph treats move-to-current-folder as a no-op (HTTP 200, message unchanged). Spec 07's idempotent-move precedent. The toast still emits "✓ Done" — user perception of the action's success is unchanged. |
| User flips `[ui].archive_label` between sessions | Takes effect on next `inkwell` start (CLAUDE.md §9: no hot reload). All toasts, hints, palette titles, and help text rebrand on next launch. |
| User has a saved `:filter ...` and presses `;e` | Bulk archive on the filter set, identical to `;a`. Confirm modal text branded per `archive_label`. |
| Cross-folder bulk archive (`;e` after `:filter --all`) | Spec 21 suffix preserved: `Archive 247 messages across 3 folders?` (or `Mark 247 messages across 3 folders done?`). |
| User invokes `inkwell thread done` against an unknown conversation-id | Inherits `inkwell thread archive`'s existing error path: prints the same error to stderr, non-zero exit. The alias does not change error semantics. |
| Palette search for "done" with `archive_label = "archive"` | Matches the row titled "Archive message" via the synonym `"done"`. Title still reads "Archive message"; binding column shows `a, e`. |
| Palette search for "archive" with `archive_label = "done"` | Matches the row titled "Mark done" via the synonym `"archive"`. Title reads "Mark done"; binding column shows `a, e`. |
| User pressed `a` in compose mode while typing | Compose owns input mode; `a` is text. Same precedent as every other lowercase rune. |

## 7. Performance budgets

**No new perf budget.** Spec 30 does not introduce a new SQL path,
new Graph call, new in-memory data structure, or new render-loop
allocation. The branding helper `archiveVerbLower(label)` is a
two-arm switch on a typed string, called from existing format
functions; it allocates no new heap.

The single-message archive path's existing budget (spec 07 §10:
"local apply ≤ 1ms, batched dispatch ≤ 50ms p95 per 100-message
batch") is unchanged and uncovered by this spec's tests.

## 8. Logging

- Action queue log sites (spec 07): unchanged. `archive` action
  logs the action type as `move` and the destination as `archive`
  (well-known name). The branding helper is UI-only; logs do not
  consult `archive_label`.
- Toast text is UI-only (ARCH §12 / CLAUDE.md §7 rule 3): not logged.
- `:done` / `:archive` cmd-bar verbs do not introduce a new log site.
  The cmd-bar dispatcher already logs `cmd: <verb>` at DEBUG.
- No subject-line, body, or PII exposure introduced.
- No new redaction tests required.

## 9. Definition of done

**No schema migration.** **No new action type.** **No new perf bench.**
**No new threat-model surface.**

### 9.1 Keymap and pane-scoping

- [ ] `internal/ui/keys.go:209` — `Archive` default updated to
      `key.NewBinding(key.WithKeys("a", "e"))`. There is no
      pre-existing `key.WithHelp` to remove.
- [ ] **`internal/config/defaults.go:90`** — `Archive: "a"` updated
      to `Archive: "a,e"`. **This is the change without which the
      keymap default is silently overwritten** (the bootstrap path
      decodes `Bindings.Archive = "a"` into `ApplyBindingOverrides`,
      which treats non-empty strings as override and replaces
      `["a","e"]` with `["a"]`). Mirrors the comma-separated pattern
      at `defaults.go:76-79` (`Up: "k,up"`, etc.).
- [ ] **Viewer-pane `e` quote-toggle alternative removed.** Delete
      `app.go:5563-5566` (`case msg.Type == tea.KeyRunes &&
      string(msg.Runes) == "e": m.viewer.ToggleQuotes()`). `Q` at
      `app.go:5559-5562` remains the canonical viewer quote-toggle.
- [ ] **Existing test removed.** `internal/ui/dispatch_test.go`
      `TestEKeyTogglesQuoteExpansion` (around line 5022) is deleted
      in the same commit. The adjacent `Q`-key test for canonical
      quote-toggle remains.
- [ ] **Folders-pane `e` rule-edit preserved.** `app.go:3927-3930`
      is unchanged; `e` continues to dispatch
      `m.startRuleEdit(ss)` for the focused saved-search. New
      regression test `TestFoldersPaneEStillEditsSavedSearchRule`
      (per §3.1 / §9.9) asserts pane-scoping.
- [ ] `BindingOverrides.Archive` semantics unchanged; no new field.
- [ ] `findDuplicateBinding` checks slice unchanged (the `archive`
      entry already exists). The scan walks every key in `b.Keys()`
      for every binding in its **explicit allowlist**
      (`internal/ui/keys.go:353-379`). The allowlist already
      includes `archive`; an `e`-vs-`archive` collision against
      any other allowlisted binding (e.g. `delete`, `move`,
      `permanent_delete`, `unsubscribe`) is correctly detected.
      Movement keys (`up`/`down`/`left`/`right`) and the pane-scoped
      bindings deliberately excluded by the allowlist comments
      (`keys.go:363-366`) are still NOT scanned — spec 30 does not
      expand the allowlist.
- [ ] **Audit:** ship-time grep
      `grep -nE '"e"' internal/ui/keys.go internal/ui/app.go` to
      verify `e` is bound only in (a) `Archive` default keys, (b)
      folders-pane rule-edit at `app.go:3927`. The deleted viewer
      quote-toggle should produce no remaining match.

### 9.2 Branding helper

- [ ] `internal/ui/labels.go` (new file): `ArchiveLabel` typed
      string with constants `ArchiveLabelArchive = "archive"`,
      `ArchiveLabelDone = "done"`; `archiveVerbLower(label)`,
      `archiveVerbTitle(label)`, and
      `archiveVerbForName(name string, label ArchiveLabel) string`
      (returns `name` unchanged for non-archive cases; lowercase
      verb for archive). A title-form helper
      `archiveVerbTitleForName(name, label)` is provided for the
      bulk/thread paths that use title-cased verbs.
- [ ] `internal/config/config.go` `UIConfig` struct gains
      `ArchiveLabel string` field with TOML tag `archive_label`.
      No new file is added under `internal/config/`.
- [ ] `internal/config/defaults.go` `Defaults()` factory sets
      `UI.ArchiveLabel = "archive"`.
- [ ] `internal/config/validate.go`: reject any value other than
      `"archive"` or `"done"` (and reject empty string explicitly)
      with the friendly error per §4.1.
- [ ] `Model.archiveLabel` field of type `ArchiveLabel`; threaded
      from `ui.New(deps, cfg)` once at construction. The label is
      a value, not a pointer; never mutates over a session.
- [ ] No `WithArchiveLabel` mutator on `KeyMap`. The label flows
      through `m.archiveLabel` to format-time helpers; no rewrite
      of `key.Binding.Help()` occurs.

### 9.3 Surface updates (every site)

For each site in §4.3, the implementer replaces the literal verb
with a call to `archiveVerbLower(m.archiveLabel)` /
`archiveVerbTitle(...)` (or, for the central toast formatter, the
`archiveVerbForName(name, label)` helper):

- [ ] **`triageDoneMsg` success format** (`app.go:1974` AND
      `app.go:1986`): the existing `fmt.Sprintf("✓ %s · u to undo",
      msg.name)` becomes `fmt.Sprintf("✓ %s · u to undo",
      archiveVerbForName(msg.name, m.archiveLabel))`. For `msg.name
      != "archive"` the helper returns `name` unchanged; only the
      archive case branches on the label.
- [ ] **`triageDoneMsg` failure format** (`app.go:1950`): the
      existing `fmt.Errorf("%s: %w", msg.name, msg.err)` adopts the
      same helper.
- [ ] **`bulkResultMsg` toast** (single / partial / zero) — verb
      segment replaced with `archiveVerbForName` for the archive
      action type. Same for `threadResultMsg`. Other action types
      unaffected.
- [ ] **`confirmBulk` modal text** (`app.go:3713-3776`) — the verb
      segment passes through `archiveVerbTitle` when the verb is
      `"archive"`. Cross-folder suffix (spec 21) and pluralisation
      logic preserved.
- [ ] **Filter status bar** (`app.go:6224`) — replace literal
      `;a archive` segment with `;a <archiveVerbLower(label)>`.
- [ ] **Bulk pending hint** (`app.go:6226`) — replace literal
      `a (archive)` with `a (<archiveVerbLower(label)>)`. **Two
      additional bulk-pending sites in `palette_commands.go:436`
      and `palette_commands.go:441` carry the same literal and must
      be updated in lockstep.**
- [ ] **List pane key hint** (`app.go:6286`) — replace literal
      `{"a", "archive"}` with `{"a", archiveVerbLower(label)}`.
      Note: the slice already shows `a` as the binding glyph; the
      hint stays that way regardless of `e` being a default
      alternate, because `a` is the canonical label-bearing key.
- [ ] **Viewer pane key hint** (`app.go:6288`) — same change.
- [ ] **Fullscreen body hint** (`app.go:6112`) — replace literal
      `a  archive` with `a  <archiveVerbLower(label)>`.
- [ ] **Palette row title** (`palette_commands.go:109`) —
      `Title:` becomes dynamic via §5.5 mapping
      (`Archive message` ↔ `Mark done`).
- [ ] **Palette thread row title** (`palette_commands.go:269`) —
      `Title: "Archive thread"` becomes dynamic via §5.5 mapping
      (`Archive thread` ↔ `Mark thread done`).
- [ ] **Palette single-message synonyms** (`palette_commands.go:111`)
      — expand from `["done", "file"]` to `["done", "file",
      "archive"]`.
- [ ] **Palette thread synonyms** (`palette_commands.go:269`
      neighbourhood — no `Synonyms` field today) — add
      `Synonyms: []string{"done", "file", "archive"}`.
- [ ] **Existing palette fixture** (`internal/ui/palette_test.go:73`
      ish — fixture row with `synonyms: []string{"archive"}` for
      the archive row) is updated to the new default
      `["done","file","archive"]` so the fixture-vs-production
      drift the table-driven palette tests assert on stays green.
- [ ] **Palette `Available.Why`** for both archive rows
      (`palette_commands.go:109` single-message and `:269` thread)
      passes through `archiveVerbLower(m.archiveLabel)` so the
      "no message focused" line uses the configured verb.
- [ ] **Help overlay** (`internal/ui/help.go:84`) —
      `buildHelpSections(km KeyMap, archiveLabel ArchiveLabel)`
      gains the second parameter; the Archive row's description
      becomes `archiveVerbLower(archiveLabel)`. Every existing
      caller of `buildHelpSections` is updated in the same commit.

### 9.4 Cmd-bar verbs

- [ ] `:archive` and `:done` cases in `dispatchCommand`. Both call
      a shared `m.runArchiveOnFocused()` helper that calls
      `runTriage("archive", *cur, ListPane, …)`.
- [ ] Empty-list error path: `<verb>: no message focused`.

### 9.5 Thread chord

- [ ] Spec 20 chord dispatch arms become `case "a", "e":` at
      **TWO** locations: `app.go:4152-4153` (list pane) and
      `app.go:5618-5619` (viewer pane). Dispatch payload
      (`runThreadMoveCmd("archive", sel.ID, "", "archive")`) is
      unchanged.
- [ ] Chord-pending hint string at **TWO** locations:
      `app.go:4129` (list-pane chord entry) and `app.go:5595`
      (viewer-pane chord entry). Each becomes
      `"thread: r/R/f/F/d/D/a/e/m/l/L/s/S  esc cancel"` —
      `/e` inserted after `/a`. Hardcoded edits (spec 25
      precedent); no string-template refactor.
- [ ] §6 edge-case row documents that user overrides of
      `[bindings].archive` do not propagate to the chord arm.

### 9.6 Apply-to-filtered chord

- [ ] `;e` arm added in the filter-mode `;` chord:
      `app.go:4035-4036` `case "a":` becomes `case "a", "e":`,
      same fall-through pattern as §9.5. The dispatch
      (`m.confirmBulk("archive", len(m.filterIDs))`) is unchanged.
- [ ] No new approach for `;<key>` chord — the spec keeps the
      existing `switch string(msg.Runes)` and limits its change to
      the archive arm. (Honouring user overrides of
      `[bindings].archive` here is a broader spec 10 question.)
- [ ] Confirm modal verb (the `"Archive 247 messages?"` text)
      passes through `archiveVerbTitle`.

### 9.7 CLI

- [ ] `cmd/inkwell/cmd_thread.go:newThreadArchiveCmd` gains
      `Aliases: []string{"done"}` and `Short` updated to mention
      the alias. RunE unchanged.
- [ ] `inkwell thread done <conversation-id>` works identically to
      `inkwell thread archive <conversation-id>`.
- [ ] No new `inkwell archive` top-level subcommand. (Out of scope.)

### 9.8 Configuration

- [ ] `docs/CONFIG.md` `[ui]` table row for `archive_label`
      (default `"archive"`, range `"archive" | "done"`, description
      per §4.1).
- [ ] `docs/CONFIG.md` `[bindings].archive` row updated to mention
      the new default (`"a,e"`) and the label-config interaction
      ("see `[ui].archive_label` for verb branding").
- [ ] `internal/config/config.go` `UIConfig` struct gains an
      `ArchiveLabel string` field (TOML tag `archive_label`).
      Default is set in `internal/config/defaults.go`'s
      `Defaults()` factory: `ArchiveLabel: "archive"`.
- [ ] `internal/config/validate.go` rejects any `[ui].archive_label`
      value other than `"archive"` or `"done"` with the message
      `config <path>: ui.archive_label must be one of "archive" or
      "done"`. App refuses to start (CLAUDE.md §9 invariant).

### 9.9 Tests

- [ ] **unit (config)**: `TestArchiveLabelDefaultIsArchive`;
      `TestArchiveLabelAcceptsDone`;
      `TestArchiveLabelRejectsUnknownValue` (e.g. `"DONE"`,
      `"complete"`); `TestArchiveLabelEmptyStringRejected`
      (empty becomes a hard error to avoid a future "empty means
      default" silent path).
- [ ] **unit (ui/labels)**: `TestArchiveVerbLowerArchive` →
      `"archive"`; `TestArchiveVerbLowerDone` → `"done"`;
      `TestArchiveVerbTitleArchive` → `"Archive"`;
      `TestArchiveVerbTitleDone` → `"Done"`;
      `TestArchiveVerbForNameOnlyTouchesArchive`
      (other action names like `"soft_delete"` pass through
      unchanged).
- [ ] **unit (keys)**: `TestDefaultArchiveBindsAandE`
      (`DefaultKeyMap().Archive.Keys() == ["a","e"]`);
      `TestDefaultsBootstrapPreservesAandE`
      (`Defaults()` + `ApplyBindingOverrides` → `["a","e"]`); this
      is the regression test for the §3.2 / §9.1 defaults-decode
      landmine;
      `TestArchiveOverrideAOnlyDropsE`
      (`[bindings].archive = "a"` → `Keys() == ["a"]`);
      `TestArchiveOverrideEOnlyDropsA`;
      `TestFindDuplicateBindingDetectsArchiveCollision`
      (`[bindings].next_pane = "e"` → startup error mentioning
      `"e"`).
- [ ] **unit (help)**:
      `TestHelpOverlayArchiveRowReadsArchiveByDefault`;
      `TestHelpOverlayArchiveRowReadsDoneWhenLabelDone`.
- [ ] **dispatch**: `TestKeyEArchivesFromList` (`e` on focused list
      message dispatches `runTriage("archive", …)`);
      `TestKeyEArchivesFromViewer`;
      `TestKeyEDoesNothingInComposeMode` (compose owns input);
      `TestFoldersPaneEStillEditsSavedSearchRule` (regression for
      §3.1 pane-scoping);
      `TestViewerEDoesNotToggleQuotes` (regression confirming the
      removal of the `e` quote-toggle alternative; `Q` still
      toggles via `TestViewerQTogglesQuotes`);
      `TestThreadChordTeArchivesThread`;
      `TestSemicolonEArchivesFiltered`;
      `TestColonDoneArchivesFocused`;
      `TestColonArchiveSamePathAsColonDone`;
      `TestColonDoneOnEmptyListShowsError`.
- [ ] **dispatch (branding)**:
      `TestArchiveToastReadsArchiveWhenLabelArchive` (the success
      string `"✓ archive · u to undo"` for the default label);
      `TestArchiveToastReadsDoneWhenLabelDone`
      (`"✓ done · u to undo"`);
      `TestArchiveFailureToastReadsDoneWhenLabelDone`
      (`"done: <err>"`);
      `TestBulkConfirmModalUsesConfiguredVerb`;
      `TestThreadConfirmReadsMarkThreadDoneWhenLabelDone`;
      `TestPaletteArchiveRowTitleSwitchesOnLabel`;
      `TestPaletteArchiveSynonymMatchesArchiveAndDoneRegardlessOfLabel`
      (palette match for both vocabularies under both labels).
- [ ] **e2e (TUI)**:
      `TestPressingEArchivesFocusedMessage` (script `e` on a
      message → assert message disappears from list, toast
      `"✓ archive · u to undo"` rendered);
      `TestColonDoneArchivesFocusedMessage`;
      `TestArchiveToastBrandedDoneWithDoneLabel` (set
      `archive_label = "done"` in cfg, script `e`, assert toast
      reads `"✓ done · u to undo"`);
      `TestThreadChordTEArchivesThread`;
      `TestChordPendingHintShowsAEGlyphs`
      (`T` pressed → status string includes `r/R/f/F/d/D/a/e/m/l/L/s/S`);
      `TestPaletteShowsBindingAandE` (palette opened on default
      cfg → archive row binding column reads `a, e`);
      `TestPaletteThreadArchiveSynonymsIncludeArchive` (palette
      filter `archive` matches the thread row even when
      `archive_label = "done"` titles it `Mark thread done`);
      `TestPaletteBulkPendingHintBranded` (palette-launched bulk
      flow's `"bulk: press d (delete) or a (done) — esc to cancel"`
      hint reflects label);
      `TestHelpOverlayShowsDoneLabelWhenConfigured`.
- [ ] **CLI**: `TestThreadDoneAliasInvokesArchive`
      (`inkwell thread done <id>` produces same JSON output as
      `inkwell thread archive <id>` against the same harness);
      `TestThreadHelpListsDoneAlias` (`inkwell thread --help`
      output mentions `done`).

### 9.10 User docs

- [ ] `docs/user/reference.md`: row for `e` (archive / done — same
      action as `a`); row for `:archive`; row for `:done`; row for
      `T e` chord; row for `;e`; row for `inkwell thread done` CLI
      alias. The existing `a` / `T a` / `;a` / `inkwell thread
      archive` rows gain a "(also `e` / `T e` / `;e` / `done`)" note.
- [ ] `docs/user/how-to.md`: short paragraph in the triage section
      noting the two-key alias and the `[ui].archive_label`
      switch ("If you prefer the HEY/Inbox 'done' framing, set
      `[ui].archive_label = \"done\"` and every Archive label in the
      app rebrands.").
- [ ] `docs/user/explanation.md`: one-paragraph note on the
      "archive vs done" framing — both verbs do the same thing
      (move to the well-known Archive folder); the choice is
      vocabulary, not behaviour.
- [ ] `docs/CONFIG.md`: per §9.8.
- [ ] `README.md`: status table row for spec 30 once shipped (per
      CLAUDE.md §12.6). PR sets it during the ship-time doc sweep.
- [ ] **`docs/plans/spec-30.md`** exists with `Status: done` and
      a final iteration entry per CLAUDE.md §13. The plan file is
      a mandatory ship-time artefact; missing it is a CLAUDE.md
      compliance failure (the inventory check in PRD §10 vs.
      `git ls-files docs/plans/` must return non-empty for every
      shipped spec).
- [ ] **`docs/PRD.md` §10** spec inventory adds a row for spec 30.
- [ ] **`docs/ROADMAP.md`** §0 Bucket 3 row 4 status updated when
      shipped; §1.23 backlog heading updated likewise.
- [ ] PR checklist (CLAUDE.md §11) fully ticked.

## 10. Cross-cutting checklist

- [ ] **Scopes:** none new. The action runs through
      `Mail.ReadWrite` (PRD §3.1), via the existing archive path.
- [ ] **Store reads/writes:** none new. The action queue table is
      written by the existing `add_action` path; no new SQL.
- [ ] **Graph endpoints:** none new. `POST /me/messages/{id}/move`
      (existing) and `/$batch` for thread/bulk variants (existing).
- [ ] **Offline:** `e` / `:done` enqueue locally and drain on
      reconnect, identical to `a` / `:archive` (spec 07 invariant 3).
- [ ] **Undo:** `u` reverses an archive triggered by any surface
      via the existing inverse path (`internal/action/inverse.go:60`).
      No spec-30 change.
- [ ] **User errors:** §6 edge-case table. `:done` / `:archive` on
      an empty list prints `<verb>: no message focused`.
      Validation of `archive_label` is a startup error.
- [ ] **Latency budget:** none new. Existing single-message archive
      path's spec 07 budget is unaffected.
- [ ] **Logs:** existing `archive` action log site; no new emission.
      Subject-line not logged in toasts. The branding helper is
      UI-only.
- [ ] **CLI:** `inkwell thread done` Cobra alias added. The
      `--action archive` flag value is unchanged (CLI-flag contract).
- [ ] **Tests:** §9.9 list.
- [ ] **Spec 17 review:** No new external HTTP surface, no SQL
      composition change, no token handling, no subprocess
      invocation, no new third-party data flow, no new cryptographic
      primitive, no new local persisted state. The `[ui].archive_label`
      typed-string config has two literal allowed values; validation
      rejects everything else at load time (fail-closed allow-list).
      Values reach only render-pass formatters; logs do not consult
      `archive_label`. Reserved verb strings (`"archive"`, `"done"`,
      `"Archive"`, `"Done"`, `"Mark done"`, `"Mark thread done"`)
      are compile-time literals. **No spec 17 §4 update required**;
      threat model unchanged; PRIVACY.md unchanged.
- [ ] **Spec 04 (KeyMap) consistency:** `Archive` default keys
      change from `["a"]` to `["a", "e"]`; `BindingOverrides.Archive`
      override semantics (comma-separated alternates) unchanged.
      Help string is constructed at `ui.New` time after
      `ApplyBindingOverrides` so user overrides flow through.
- [ ] **Spec 07 (action queue) consistency:** the `archive` action
      type, the `Move` underlying action, the
      `destination_folder_alias = "archive"` resolution, and the
      undo inverse path are all unchanged. Spec 30 is a
      surface alias.
- [ ] **Spec 20 (thread chord) consistency:** chord dispatch
      reads `m.keymap.Archive.Keys()` instead of the literal
      `"a"`. The pending-status hint string is updated at TWO sites
      (`app.go:4129`, `app.go:5595`); spec 25 precedent for the
      "edit both" pattern is followed.
- [ ] **Spec 22 (palette) consistency:** palette row IDs
      (`archive`, `thread_archive`) unchanged; titles become
      label-aware; synonyms widened to `["done", "file",
      "archive"]` for both rows.
- [ ] **Spec 14 (CLI) consistency:** `inkwell thread done` is a
      Cobra alias of the existing subcommand. CLI flag values
      (`--action archive`) are unchanged — CLI flag contracts are
      stable identifiers.
- [ ] **Docs consistency sweep:** CONFIG.md (1 new key + 1 amended
      row), reference.md (4 keybinding/command rows + amendments),
      how-to.md (one paragraph), explanation.md (one paragraph).
      No CHANGELOG-style file added. Full §12.6 ship-time table
      enumerated in §9.10 (plan file, PRD §10, ROADMAP §0/§1.23,
      README status table, spec `**Shipped:**` line).
