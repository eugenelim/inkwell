# Spec 33 — Rich-text / Markdown Drafts

**Status:** Shipped.
**Shipped:** v0.62.0 — goldmark v1.8.2 wired; `[compose] body_format`
config key landed (default `"plain"`, opt-in `"markdown"`);
`compose.DraftBody` flows through `DraftCreator` → `Executor` →
`graph.Client`; `action.Params` persists `content_type` for resume
path; `Ctrl+E` writes `.md` tempfile in Markdown mode; `[md]`
footer indicator; `ComposeSnapshot` round-trips `MarkdownMode`
through crash recovery.
**Depends on:** Spec 15 (compose / reply — `DraftCreator` interface,
`ComposeModel`, `saveComposeCmd`, `Ctrl+E` editor drop-out,
`compose.WriteTempfile`), Spec 14 (CLI mode — `inkwell messages
compose` is *not* in scope; CLI compose is post-v1 per spec 15
§§ 10–11), Spec 01 (auth — no new scopes; `Mail.ReadWrite` already
requested). No spec depends on this one yet; it is the third item
of Bucket 4 (Mailbox parity).
**Blocks:** None directly. A future "per-snippet format" or
"inline preview" spec could depend on `compose.RenderMarkdown`
and `compose.DraftBody` introduced here.
**Estimated effort:** 2–3 days (new pure-Go library + shallow
wiring change; no schema migration, no new Graph scopes, no new
subcommands).

---

### 0.1 Spec inventory

Rich-text / Markdown drafts is item 3 of Bucket 4 in
`docs/product/roadmap.md §0` and corresponds to backlog item §1.18. The
roadmap description:

> v1 drafts are plain text. The platform accepts HTML drafts. We
> could let users compose in Markdown and convert to HTML on save.
> Not critical; plain text covers 90% of use cases.

This spec fills in the concrete design for that conversion, picks
the library, defines the config surface, and specifies exact
integration points in the existing compose pipeline.

---

## 1. Goal

Let users who write in Markdown compose richer emails from inkwell
without learning HTML. When the user sets
`[compose] body_format = "markdown"` in config, inkwell converts
the body to HTML before saving the draft on Microsoft Graph. The
final draft is an HTML message that Outlook, mobile clients, and
recipients' clients render with proper formatting (bold, lists,
code blocks, tables, blockquote reply chains).

The user writes Markdown in the in-modal textarea or in
`$EDITOR` via `Ctrl+E`. Save behaviour is unchanged: `Ctrl+S` /
`Esc` saves the draft; the usual `✓ draft saved · press s to open
in Outlook` status message appears. The only visible indicator that
Markdown mode is active is a `[md]` tag in the compose-pane footer.

The default is `body_format = "plain"` — existing users see no
behaviour change.

---

## 2. Non-goals

- **WYSIWYG / inline preview.** The textarea shows raw Markdown
  source. A split preview pane is post-v1.
- **Per-message format toggle** during composition. v1 is global
  config only.
- **Multipart-alternative.** Microsoft Graph's `body` object
  accepts a single content type (`"text"` or `"html"`); no
  multipart concept exists at the message body level.
- **Markdown in CLI mode** (`inkwell messages compose`). Post-v1.
- **HTML sanitizer.** The user composes their own draft, which
  goes only to their own Graph mailbox — no injection attack
  surface in this flow.
- **Custom Markdown extensions** beyond the GFM set.
- **Signature management.** The Outlook signature applies when the
  user opens the draft to send. Out of scope per spec 15 §2.
- **Editing an existing draft from the Drafts folder.** Spec 15
  ships compose → save → hand off to Outlook; reopening a saved
  draft from the Drafts folder for further editing in inkwell is
  not in v1's compose surface. Spec 33 inherits this scope: the
  user composes once in inkwell, then either saves to Drafts or
  discards. Mode-switching mid-flight (saving a plain-text draft,
  toggling `body_format = "markdown"` in config, restarting,
  reopening the saved draft) is undefined — the body stored on
  Graph carries its own `contentType`, but inkwell's compose pane
  is not re-entered for an existing draft.

---

## 3. Scopes used

`Mail.ReadWrite` — already requested at sign-in (PRD §3.1).
No new scopes.

---

## 4. Library choice

### 4.1 Rationale

`github.com/yuin/goldmark` (pure Go, no CGO). Pin to v1.8.x or
later at implementation time; the exact version is recorded in
`go.mod`. Dependabot policy: automatic minor / patch bumps are
fine; a major bump requires a spec-revision PR.

| Property | goldmark | blackfriday/v2 |
| --- | --- | --- |
| CommonMark-compliant | ✅ v0.30 | ❌ (predates CommonMark) |
| GFM extensions built-in | ✅ `extension/` | partial |
| Pure Go / no CGO | ✅ | ✅ |
| Active maintenance | ✅ (Hugo dependency) | ⚠ maintenance mode |

goldmark is the de-facto CommonMark implementation in the Go
ecosystem (Hugo, Gitea, Sourcegraph). `blackfriday/v2` is already
in `go.sum` as an indirect dependency of `html2text`; we do not use
it for new code because it is in maintenance-only mode and is not
CommonMark compliant.

### 4.2 Extensions enabled

- `extension.Table` — GFM-style pipe tables.
- `extension.Strikethrough` — `~~del~~`.
- `extension.TaskList` — `- [x]` / `- [ ]` rendered as checkboxes.
- `extension.Linkify` — bare URLs auto-linked.

(These are package-level variables in `github.com/yuin/goldmark/extension`,
not factory functions. The bundle alias `extension.GFM` enables
exactly the same four; the spec lists them individually for
explicitness. Either form is equivalent.)

**Not enabled:** `extension.Footnote`, `extension.TypographyExtension`
(smart quotes alter user-typed punctuation unexpectedly in email).

### 4.3 Renderer options

Hard-wrap mode (`html.WithHardWraps()`) is **not** passed. goldmark's
default behaviour (soft-wrap) is correct: a single newline in the
source is not turned into a `<br>`; only blank lines create paragraph
breaks. This matches how Markdown authors expect prose to flow.

`html.WithUnsafe()` is **not** passed. goldmark's default sanitizer
strips raw HTML blocks in the Markdown source; since email bodies
do not need pass-through HTML, this is the correct default.

---

## 5. Module layout

### 5.1 New files

```
internal/compose/
├── markdown.go         # RenderMarkdown + DraftBody type
└── markdown_test.go    # unit tests
```

`DraftBody` lives in `internal/compose` so both `internal/ui` and
`internal/action` can import it without creating an upward
dependency (`action` already imports lower-tier packages; `compose`
sits in the middle tier below `ui`, see §6.1).

### 5.2 Changed files

```
go.mod / go.sum                     # +goldmark
docs/ARCH.md                        # internal/compose added to module tree §1
internal/config/config.go           # BodyFormat string in ComposeConfig
internal/config/defaults.go         # BodyFormat: "plain"
internal/config/validate.go         # BodyFormat ∈ {"plain","markdown"}
internal/compose/editor.go          # WriteTempfileExt added; WriteTempfile delegates to it
internal/graph/drafts.go            # content, contentType string params to PatchMessageBody + CreateNewDraft
internal/action/draft.go            # import compose; compose.DraftBody through all Create* methods;
                                    # action.Params persists content_type alongside body
internal/ui/app.go                  # DraftCreator interface: body compose.DraftBody
                                    # saveComposeCmd: RenderMarkdown when markdown mode
                                    # Ctrl+E: .md ext in markdown mode
internal/ui/compose.go              # saveComposeCmd conversion + DraftBody construction
internal/ui/compose_model.go        # MarkdownMode bool; [md] in footer
cmd/inkwell/cmd_run.go              # draftAdapter (lines 498–520): all 4 Create* methods
                                    # wrap body string in compose.DraftBody before forwarding
cmd/inkwell/cmd_messages.go         # CLI reply/reply-all/forward (lines 645/679/714):
                                    # wrap body as compose.DraftBody{Content: body, ContentType: "text"}
                                    # — Markdown in CLI is post-v1 but the wrapping is mandatory for compile
docs/CONFIG.md                      # new body_format row in [compose]
docs/user/reference.md              # compose section updates
docs/user/how-to.md                 # new recipe
```

**Note: three layers consume `DraftCreator` / `Executor.Create*`** —
the in-modal save path (`internal/ui/compose.go`), the CLI
subcommands (`cmd/inkwell/cmd_messages.go`), and the wiring adapter
(`cmd/inkwell/cmd_run.go::draftAdapter`). All three must update at
the same signature break; missing any one fails the build.

---

## 6. Design

### 6.1 Layer placement of `internal/compose`

`internal/compose` is an **existing** middle-tier package (spec 15).
It currently imports only stdlib and is depended on by `ui`. After
this spec it gains a new import (`goldmark`) and introduces
`DraftBody` — a type shared upward by `ui` and sideways-downward
by `action`.

The dependency graph after spec 33:

```
ui  →  compose  →  (stdlib, goldmark)
ui  →  action   →  compose  →  (stdlib, goldmark)
                   graph    →  (stdlib)
```

`action` importing `compose` is new but introduces no cycle:
`compose` does not import `action`, `ui`, or `graph`.
`graph` does not import `compose` — it receives plain strings
(§6.4 explains why the struct stops at the `action`/`graph`
boundary).

### 6.2 `DraftBody` and `RenderMarkdown`

Location: `internal/compose/markdown.go`.

```go
// DraftBody carries the compose body through the dispatch pipeline.
// ContentType is "text" for plain or "html" for Markdown-rendered
// content. DraftBody is defined here (internal/compose) so both
// internal/ui and internal/action can import it without creating an
// upward dependency (neither package imports the other).
type DraftBody struct {
    Content     string
    ContentType string // "text" | "html"
}

// RenderMarkdown converts CommonMark Markdown to an HTML fragment
// using goldmark with GFM table, strikethrough, task-list, and
// autolink extensions. Returns a self-contained HTML fragment
// (no <html>/<body> wrapper) suitable for Graph's body.content.
// The error is very unlikely with stock extensions and a
// bytes.Buffer writer (no I/O failure path), but the guard is
// retained for forward-compatibility with future parser extensions.
func RenderMarkdown(src string) (string, error) {
    md := goldmark.New(
        goldmark.WithExtensions(
            extension.Table,
            extension.Strikethrough,
            extension.TaskList,
            extension.Linkify,
        ),
    )
    var buf bytes.Buffer
    if err := md.Convert([]byte(src), &buf); err != nil {
        return "", fmt.Errorf("markdown render: %w", err)
    }
    return buf.String(), nil
}
```

Note: no `goldmark.WithRendererOptions(...)` is passed. The
default renderer uses soft-wrap (single newlines do not become
`<br>`), which is the desired behaviour per §4.3.

The goldmark instance is created fresh per call (stateless). For
the message sizes involved (5–50 KB of email body text), goldmark's
allocation profile is acceptable: Hugo's published per-page
conversion timings (which include more than `Convert` alone) are
in the low-millisecond range, so a 10 KB email body is expected to
render sub-millisecond — well within the 2ms budget (§10).
Actual numbers are pinned by the benchmark gate, not by this prose.

### 6.3 `DraftCreator` interface update

`DraftCreator` in `internal/ui/app.go` gains a `compose` import
and replaces the bare `body string` with `body compose.DraftBody`:

```go
import "github.com/eugenelim/inkwell/internal/compose"

type DraftCreator interface {
    CreateDraftReply(ctx context.Context, accountID int64,
        sourceMessageID string, body compose.DraftBody,
        to, cc, bcc []string, subject string,
        attachments []DraftAttachmentRef) (*DraftRef, error)
    CreateDraftReplyAll(ctx context.Context, accountID int64,
        sourceMessageID string, body compose.DraftBody,
        to, cc, bcc []string, subject string,
        attachments []DraftAttachmentRef) (*DraftRef, error)
    CreateDraftForward(ctx context.Context, accountID int64,
        sourceMessageID string, body compose.DraftBody,
        to, cc, bcc []string, subject string,
        attachments []DraftAttachmentRef) (*DraftRef, error)
    CreateNewDraft(ctx context.Context, accountID int64,
        body compose.DraftBody,
        to, cc, bcc []string, subject string,
        attachments []DraftAttachmentRef) (*DraftRef, error)
    DiscardDraft(ctx context.Context, accountID int64,
        draftID string) error
}
```

`action.Executor` methods mirror this shape (importing `compose`);
`graph.Client` methods take `content, contentType string` as
separate positional parameters — no struct at the graph layer
avoids a new import from `graph` to `compose` (§6.4).

### 6.4 Graph layer

`internal/graph/drafts.go` changes two signatures. The body struct
stops at the `action`/`graph` boundary; the graph client receives
plain strings to avoid any import from `graph` upward into middle-tier packages:

```go
// PatchMessageBody sets the body of an existing draft. contentType
// must be "text" or "html".
func (c *Client) PatchMessageBody(ctx context.Context, id,
    content, contentType string,
    to, cc, bcc []string, subject string) error

// CreateNewDraft creates a new draft. contentType follows the
// same "text"/"html" convention.
func (c *Client) CreateNewDraft(ctx context.Context,
    subject, content, contentType string,
    to, cc, bcc []string) (*DraftRef, error)
```

The `action.Executor` unwraps the `DraftBody` before calling:

```go
e.gc.PatchMessageBody(ctx, ref.ID, body.Content, body.ContentType, to, cc, bcc, subject)
```

The PATCH payload only touches `body`, `subject`, `toRecipients`,
`ccRecipients`, `bccRecipients` (existing spec-15 behaviour, not
changed here). It never sets `internetMessageHeaders`, which would
break the `In-Reply-To` / `References` headers Graph populated via
`createReply`.

### 6.5 `saveComposeCmd` — where conversion happens

In `internal/ui/compose.go`, `saveComposeCmd` reads the config and
converts before calling the executor:

```go
// snap.MarkdownMode is captured at Snapshot() time from the live
// ComposeModel — single source of truth per the §6.7 invariant.
bodyText := snap.Body
var draftBody compose.DraftBody
if snap.MarkdownMode {
    html, err := compose.RenderMarkdown(bodyText)
    if err != nil {
        return func() tea.Msg {
            return draftSavedMsg{err: fmt.Errorf("markdown: %w", err)}
        }
    }
    draftBody = compose.DraftBody{Content: html, ContentType: "html"}
} else {
    draftBody = compose.DraftBody{Content: bodyText, ContentType: "text"}
}
// pass draftBody instead of snap.Body to the DraftCreator methods
```

`ComposeSnapshot` gains a `MarkdownMode bool` field captured from
`ComposeModel.MarkdownMode` at snapshot time; the JSON-persisted
session row in `compose_sessions` carries this flag so a resumed
draft after restart converts on the same format the user originally
selected.

The `ComposeSnapshot.Body` field stores the raw user text (Markdown
source or plain text). The action's `Params` blob stores **both**
the rendered HTML body and its content type, so the resume path
can re-PATCH Graph with the correct contentType:

- **crash-recovery restore** reads from `compose_sessions.snapshot`
  (raw Markdown source → restores into textarea so the user gets
  their editable source back)
- **action replay** reads from `actions.params["body"]` AND
  `actions.params["content_type"]` (already-rendered HTML +
  `"html"` → re-PATCH Graph with the same payload). Without
  persisting `content_type`, a resumed Markdown draft would
  re-patch as `contentType="text"` and Outlook would render the
  HTML source literally — a silent bug.

When both a `compose_sessions` unconfirmed row and a `Pending`
action row exist for the same draft (the crash happened between
stage 1 and stage 2), the resume-compose path (spec 15 PR 7-ii)
uses the `compose_sessions` row. The action executor's drain skips
`CreateDraftReply*` / `CreateNewDraft` (non-idempotent; draft may
already exist on server). The user re-saves from the Markdown
source in the restored form, triggering a fresh Graph round-trip.

**`ComposeSnapshot.Body` invariant:** the field is *raw user text
in the user's chosen format*. No consumer may treat it as
renderable plain text — it may contain `**`, `_`, `> `, `|`
delimiters that mean Markdown structure, not literal characters.
The resume confirm modal today only reads `snap.Subject` (safe).
Any future consumer that previews `snap.Body` must either render
it through `compose.RenderMarkdown` (if `MarkdownMode`) or treat
it as opaque source.

### 6.6 `Ctrl+E` editor drop-out — `.md` extension

New helper in `internal/compose/editor.go`:

```go
// WriteTempfileExt creates a tempfile like WriteTempfile but with
// an explicit extension. The UUID prefix and cache-directory path
// are identical to WriteTempfile; only the suffix differs.
// Example: <draftsDir>/<uuid>.md
func WriteTempfileExt(content, ext string) (string, error)

// WriteTempfile is unchanged in behaviour; it delegates to
// WriteTempfileExt(content, ".eml").
func WriteTempfile(content string) (string, error) {
    return WriteTempfileExt(content, ".eml")
}
```

The UUID prefix is preserved verbatim (the naming invariant for
the compose_sessions `tempPath` column is not changed).

In `app.go`'s `tea.KeyCtrlE` handler (reads from `m.compose.MarkdownMode`,
not config — see §6.7 invariant):

```go
ext := ".eml"
if m.compose.MarkdownMode {
    ext = ".md"
}
path, err := compose.WriteTempfileExt(body, ext)
```

Today there is no orphan-tempfile garbage collector that filters
by extension; `<draftsDir>` is read by tempPath alone (full path
in `compose_sessions.tempPath`). If a future GC sweeps the dir
by glob, it must accept both `.eml` and `.md` suffixes.

On `composeEditorDoneMsg` return: `m.compose.SetBody(string(content))`
is called as today (the body is placed back into the textarea as-is).
Conversion from Markdown to HTML is deferred to `saveComposeCmd`, not
performed eagerly on editor return:
- The textarea shows the raw Markdown after editor exit ✓
- The user can edit further in the textarea before pressing `Ctrl+S` ✓
- The single conversion path in `saveComposeCmd` is the source of
  truth for both the textarea-only flow and the `Ctrl+E` flow ✓

### 6.7 `ComposeModel.MarkdownMode` — footer indicator

```go
type ComposeModel struct {
    // ...existing fields...
    // MarkdownMode mirrors [compose] body_format = "markdown".
    // When true, the footer shows " · [md]" and Ctrl+E writes .md.
    MarkdownMode bool
}
```

The footer in `View(t Theme, width, height int) string`:

```
Ctrl+S / Esc save  ·  Ctrl+D discard  ·  Tab cycle  ·  Ctrl+E editor  ·  Ctrl+A attach  ·  [md]
```

(` · [md]` appended only when `MarkdownMode` is true.)

**`NewCompose()` entry points that need `MarkdownMode` set.**
There are four call sites for `NewCompose()` in `app.go`. Two of
them create a compose model that is **immediately discarded** (the
Ctrl+S reset-on-save at line 2731 and the Ctrl+D discard at
line 2748 — both reset the field and switch mode back to Normal
before any View call, so `MarkdownMode` on those models is never
rendered). The two that need `MarkdownMode` set:

1. **`New()` constructor** (line 1005) — the model is stored on `m`
   and will be displayed if crash-recovery restores a compose
   session on startup.
2. **`startComposeOfKind()`** (line 2667) — the main entry point
   for `r` / `R` / `f` / `m` compose flows.

Both sites add:

```go
m.compose = NewCompose()
m.compose.MarkdownMode = m.deps.Config.Compose.BodyFormat == "markdown"
```

**`MarkdownMode` is the single source of truth for the session.**
`saveComposeCmd` (§6.5) and the `Ctrl+E` handler (§6.6) MUST read
`m.compose.MarkdownMode`, never `m.deps.Config.Compose.BodyFormat`
directly. The invariant: format is fixed at compose-entry time
and cannot drift mid-session. This lets a future per-message
`:format` toggle flip `MarkdownMode` cleanly without re-reading
config; it also prevents a config reload mid-compose from changing
how a partially-written body gets converted.

### 6.8 Config

New key in `[compose]` section (`internal/config/config.go`):

```go
// BodyFormat controls how the draft body is submitted to Graph.
// "plain" (default) sends contentType="text" with the raw body.
// "markdown" converts via goldmark (GFM + CommonMark) to HTML and
// sends contentType="html". Spec 33 §6.5.
BodyFormat string `toml:"body_format"`
```

Default (`internal/config/defaults.go`): `BodyFormat: "plain"`.

Validation (`internal/config/validate.go`): if `BodyFormat` is
neither `"plain"` nor `"markdown"`, the app hard-fails with a
line-numbered error (`docs/CONVENTIONS.md` §9).

`docs/CONFIG.md` entry:

```
| `[compose] body_format` | `"plain"` | `"plain"` or `"markdown"`. Markdown mode converts the draft body via goldmark (CommonMark + GFM) to HTML before saving on Graph. |
```

---

## 7. Render contract for Markdown body

### 7.1 Quote chains

After `ApplyReplySkeleton`, the compose textarea contains:

```markdown
<cursor>

On Mon 2026-05-13 at 14:32, Alice wrote:
> Hey, can you review the attached spec before Thursday?
> Let me know if you have questions.
```

goldmark (CommonMark) processes this as:
- Attribution line (`On Mon...`): a standalone `<p>` element.
- `> ` prefix lines: a `<blockquote>` block sibling to the `<p>`.

The expected HTML fragment:

```html
<p>On Mon 2026-05-13 at 14:32, Alice wrote:</p>
<blockquote>
<p>Hey, can you review the attached spec before Thursday?
Let me know if you have questions.</p>
</blockquote>
```

This is the **desired** output: the attribution sits outside the
blockquote (as Outlook and email conventions expect), and the quoted
text is a proper `<blockquote>`. No special treatment of the quote
region is needed.

**History note.** Today (post-spec-15, pre-spec-33), the
plain-text PATCH on the draft already overwrites the HTML quote
chain that `createReply` initially populated server-side. The
canonical quote chain has been the user-typed `> ` lines from
`ApplyReplySkeleton`, rendered as plain text in the recipient's
client. Spec 33 preserves that contract — the same `> ` lines now
render as `<blockquote>` instead of bare text. No regression in
quote-chain semantics.

### 7.2 New-message body

For brand-new messages the user writes Markdown as normal:

```markdown
Hi Alice,

Could you send me the **Q4 deck**?

- Latest version preferred
- PDF if possible
```

goldmark renders `<p>`, `<strong>`, `<ul>` / `<li>` elements —
a standard HTML email body that renders in all clients.

### 7.3 GFM tables

Tables render as `<table>` / `<tr>` / `<th>` / `<td>`. Modern
clients with default user-agent table CSS (OWA, Gmail, Apple Mail,
Thunderbird) render bordered tables out of the box. **Outlook
desktop's default table styling is sparse** — tables without
inline `style="border-collapse:collapse; border:1px solid …"`
render borderless. goldmark does not emit inline CSS, so Outlook
recipients see plain unstyled tables. Documented in the user
how-to recipe.

goldmark emits HTML without line wrapping (long paragraphs are
single lines). RFC 5322's 998-character line limit is an SMTP
transport concern handled by Exchange on send; Graph accepts the
unwrapped HTML as a JSON string, no client-side wrapping needed.

---

## 8. Failure modes

| Failure | User-visible behaviour |
| ------- | ---------------------- |
| goldmark returns an error (very unlikely with stock extensions and `bytes.Buffer` writer — parser failures are the only remaining source) | Status: `"markdown: …"`. Compose form remains open with the original source intact. No draft is saved. |
| User types `**bold**` expecting literal asterisks | Draft renders with `<strong>bold</strong>`. Mitigation: `[md]` footer indicator is always visible. User disables Markdown mode via `body_format = "plain"`. |
| Graph rejects HTML body (Exchange has accepted HTML drafts since Exchange 2010; rejection is not expected) | Standard draft-save error path from spec 15. Status bar shows the error; webLink shown if stage 1 succeeded. |
| Outlook desktop opens the HTML draft for send and re-renders / re-styles the HTML | Acceptable. Outlook is the send authority; inkwell's `webLink` hand-off means Outlook owns final styling. goldmark's clean fragment output (`<p>`, `<strong>`, `<ul>`, `<table>`, `<blockquote>` with no `<style>` or inline CSS) is the safe subset that survives Outlook's normalization. Recipients may see Outlook's default theme applied on top. Documented as a user-facing caveat in `docs/user/how-to.md`. |

---

## 9. Definition of done

- [ ] `internal/compose/markdown.go` defines `DraftBody{Content, ContentType string}` and `RenderMarkdown(src string) (string, error)` using goldmark with the extensions listed in §4.2.
- [ ] `internal/compose/markdown_test.go` covers: empty string, plain prose (no Markdown syntax → unchanged output), bold/italic, unordered list, ordered list, GFM table, blockquote quote chain (attribution line outside `<blockquote>` as specified in §7.1), `~~strikethrough~~`, bare-URL autolink, fenced code block, task list. Each test asserts the exact HTML fragment produced.
- [ ] `internal/compose/editor.go` adds `WriteTempfileExt(content, ext string) (string, error)` that preserves the UUID-prefix naming scheme; `WriteTempfile` delegates to `WriteTempfileExt(content, ".eml")`. `WriteTempfileExt` test added in `editor_test.go` (or new `editor_ext_test.go`).
- [ ] `internal/config/config.go` adds `BodyFormat string` to `ComposeConfig`.
- [ ] `internal/config/defaults.go` sets `BodyFormat: "plain"`.
- [ ] `internal/config/validate.go` rejects values other than `"plain"` / `"markdown"`.
- [ ] `internal/config/config_test.go` adds `TestConfigDecodeComposeBodyFormat` and `TestConfigValidateBodyFormatRejectsBadValue`.
- [ ] `go get github.com/yuin/goldmark` run; `go.mod` and `go.sum` committed in the same change.
- [ ] `internal/graph/drafts.go` adds `content, contentType string` parameters to `PatchMessageBody` and `CreateNewDraft` in place of the hard-coded `"text"` string. No new imports. `internal/graph/drafts_test.go` today only covers `DeleteDraft` and `AddDraftAttachment`; add new tests `TestPatchMessageBody_HTMLContentType` and `TestCreateNewDraft_HTMLContentType` that assert the outbound JSON payload's `body.contentType` field is `"html"` when so requested.
- [ ] `internal/action/draft.go` imports `internal/compose`; all four `Create*` methods accept `body compose.DraftBody` and unwrap it when calling `e.gc.PatchMessageBody` / `e.gc.CreateNewDraft`. Existing executor tests (`TestCreateNewDraftSinglePost` at `internal/action/executor_test.go:965`, plus the `TestCreateDraftReply*` family at `:728+` — `TestCreateDraftReplyEnqueuesActionAndPersistsDraftID`, `TestCreateDraftReplyKeepsDraftIDOnPATCHFailure`, `TestCreateDraftReplyMarksFailedOnCreateReplyFailure`, `TestCreateDraftReplyRecipientsRoundTripThroughJSON`) updated to pass `compose.DraftBody{Content: "…", ContentType: "text"}`.
- [ ] `internal/action/draft.go::createDraftFromSource` and `::CreateNewDraft` write `a.Params["body"] = body.Content` AND `a.Params["content_type"] = body.ContentType` (two separate entries). Spec-15 PR 7-ii's resume path reads both and passes them to `PatchMessageBody`. Without persisting `content_type`, a resumed HTML draft re-PATCHes as `contentType="text"` and Outlook renders the HTML source literally — a silent regression.
- [ ] `internal/ui/app.go` imports `internal/compose`; `DraftCreator` interface uses `compose.DraftBody`; `Ctrl+E` reads `m.compose.MarkdownMode` (not config) to decide `.md` extension.
- [ ] `internal/ui/compose.go::saveComposeCmd` reads `m.compose.MarkdownMode` (not config); calls `compose.RenderMarkdown` when true; constructs `compose.DraftBody`.
- [ ] `internal/ui/dispatch_test.go` `stubDraftCreator` (line 2263) and `recordingDraftCreator` (line 2424) methods updated from `body string` to `body compose.DraftBody`.
- [ ] `internal/ui/compose_model.go` adds `MarkdownMode bool` to `ComposeModel`; footer appends ` · [md]` when true.
- [ ] `internal/ui/compose_model.go::ComposeSnapshot` adds `MarkdownMode bool` with JSON tag `json:"markdown_mode,omitempty"`. `Snapshot()` copies the field from the model; `Restore()` sets it back. The new field is JSON-additive — old `compose_sessions.snapshot` rows decode with `MarkdownMode = false` (plain mode), preserving the safe default for any pre-spec-33 unconfirmed session.
- [ ] Both `NewCompose()` call sites that feed a rendered view (`New()` at `app.go:1005` and `startComposeOfKind` at `app.go:2667`) set `m.compose.MarkdownMode` from config. The two reset-discard sites (lines 2731, 2748) do not need this — the model is immediately discarded.
- [ ] `cmd/inkwell/cmd_run.go` `draftAdapter` (lines 498–520): the 4 `Create*` methods accept `body compose.DraftBody` (matching the `ui.DraftCreator` and `action.Executor` signatures — same struct flows straight through, no unwrap-rewrap). `DiscardDraft` is unchanged (no body parameter). Existing `internal/compose` import via this file is already present.
- [ ] `cmd/inkwell/cmd_messages.go` adds `"github.com/eugenelim/inkwell/internal/compose"` to its imports (not present today). CLI subcommands `messages reply` (line 645), `reply-all` (line 679), `forward` (line 714): each wraps `body` as `compose.DraftBody{Content: body, ContentType: "text"}` at the `exec.Create*` call. Markdown rendering from the CLI is post-v1; the wrap is a mandatory compile fix.
- [ ] `docs/ARCH.md` §1 module-tree listing updated to include `internal/compose`.
- [ ] `go test -race ./internal/compose/...` green.
- [ ] `go test -race ./internal/config/...` green.
- [ ] `go test -race ./internal/graph/...` green (existing draft tests updated for new signature).
- [ ] `go test -race ./internal/action/...` green (existing draft tests updated).
- [ ] `go test -race ./internal/ui/...` green (stubs updated for `compose.DraftBody`).
- [ ] `go test -tags=e2e ./internal/ui/...` green:
  - `TestComposeMarkdownModeFooterIndicator` — `[md]` appears in the compose footer frame when `MarkdownMode = true`.
  - `TestComposeMarkdownModeAbsentInPlain` — `[md]` is **absent** when `MarkdownMode = false`.
  - `TestComposeMarkdownModePlainTextStillSendsHTML` — with `MarkdownMode = true` and body `"hi"`, the captured `DraftCreator.CreateNewDraft` call receives `compose.DraftBody{Content: "<p>hi</p>\n", ContentType: "html"}`. Catches the "did MarkdownMode actually wire through end-to-end" regression class.
  - `TestComposeSnapshotPreservesMarkdownMode` — `ComposeSnapshot` round-trips `MarkdownMode` through JSON serialization so a resumed session converts on the original format.
- [ ] `internal/render/render_test.go` or `internal/compose/markdown_test.go` adds `TestRenderMarkdownRoundTripsThroughBodyView` — feeds `RenderMarkdown(sample)` HTML into `render.BodyView` (HTML → text) and asserts the resulting plain text is readable (no raw `<p>` tags leak through, lists become `- ` lines, etc.). Self-rendering own drafts in the Drafts viewer is the regression target.
- [ ] `go test -bench=. -benchmem -run=^$ ./internal/compose/...` green: `BenchmarkRenderMarkdown10KB` and `BenchmarkRenderMarkdown100KB` pass within their §10 budgets.
- [ ] `docs/CONFIG.md` row for `[compose] body_format`.
- [ ] `docs/user/reference.md` "Compose" section updated with `body_format`, `[md]` indicator, `.md` tempfile extension.
- [ ] `docs/user/how-to.md` recipe: "Compose with Markdown formatting".
- [ ] Spec 17 cross-cut: no new file I/O paths, subprocess invocations, external HTTP, crypto, SQL, or persisted state. No spec 17 update needed.

---

## 10. Performance budgets

| Surface | Budget | Benchmark |
| --- | --- | --- |
| `RenderMarkdown` on a 10 KB body | <2ms p95 | `BenchmarkRenderMarkdown10KB` |
| `RenderMarkdown` on a 100 KB body | <20ms p95 | `BenchmarkRenderMarkdown100KB` |

goldmark's throughput at typical email body sizes (5–50 KB) is
approximately 20–30 MB/s, placing a 10 KB body at ≈0.3–0.5ms.
The 2ms budget is ≈4–6× headroom against measured performance.
A regression of >50% over the budget (`docs/CONVENTIONS.md` §5.2) fails the
benchmark test.

---

## 11. Cross-cutting checklist (`docs/CONVENTIONS.md` §11)

- [ ] Which Graph scope(s)? `Mail.ReadWrite` only — already requested. No new scopes.
- [ ] State in store? None — no schema change.
- [ ] Graph endpoints? `PATCH /me/messages/{id}` and `POST /me/messages` (existing). The `contentType` field in the JSON payload changes; no new endpoints.
- [ ] Offline behaviour? `saveComposeCmd` runs when connected; no change to the offline path.
- [ ] Undo behaviour? No change; `DiscardDraft` is still the inverse.
- [ ] Error states? goldmark render failure surfaces a status-bar error; compose form stays open with source intact. Graph errors unchanged.
- [ ] Latency budget? §10 above; benchmarks gated per `docs/CONVENTIONS.md` §5.2.
- [ ] Logs? No new log sites. Body content never logged (spec 15 redaction invariant unchanged).
- [ ] CLI-mode equivalent? None — CLI compose is post-v1.
- [ ] Tests? Unit (markdown converter + WriteTempfileExt), config tests, updated graph/action/ui unit tests, new e2e indicator tests, benchmarks.
- [ ] Spec 17? No new file I/O, subprocess, external HTTP, crypto, SQL, or persisted state. No spec 17 update needed.
- [ ] Spec 17 CI gates? No `// #nosec` annotations needed.

---

## 12. Open questions

None. The design is fully determined by the existing spec 15
pipeline and goldmark's API.

---

## 13. Notes for follow-up specs

- A `[compose] markdown_preview = true` option could add a split
  preview pane in ComposeMode using the existing HTML→text renderer
  (`render.BodyView`) to display the rendered HTML. This is post-v1.
- `RenderMarkdown` and `DraftBody` are reusable for a future
  "snippets" feature (roadmap §1.22) if snippets support Markdown.
- Per-message format toggle (`:format markdown` / `:format plain`
  while composing) is post-v1. `ComposeModel.MarkdownMode` is
  already in position for a per-compose override; a `:format`
  command-bar verb would flip it.
