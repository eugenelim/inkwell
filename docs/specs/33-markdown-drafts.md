# Spec 33 — Rich-text / Markdown Drafts

**Status:** Draft.
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
`docs/ROADMAP.md §0` and corresponds to backlog item §1.18. The
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

---

## 3. Scopes used

`Mail.ReadWrite` — already requested at sign-in (PRD §3.1).
No new scopes.

---

## 4. Library choice

### 4.1 Rationale

`github.com/yuin/goldmark` v1.7.x (pure Go, no CGO).

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
internal/action/draft.go            # import compose; compose.DraftBody through all Create* methods
internal/ui/app.go                  # DraftCreator interface: body compose.DraftBody
                                    # saveComposeCmd: RenderMarkdown when markdown mode
                                    # Ctrl+E: .md ext in markdown mode
internal/ui/compose_model.go        # MarkdownMode bool; [md] in footer
docs/CONFIG.md                      # new body_format row in [compose]
docs/user/reference.md              # compose section updates
docs/user/how-to.md                 # new recipe
```

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
// Writing to a bytes.Buffer never returns an error; the returned
// error is structurally unreachable in this usage, but callers
// must check it for forward-compatibility.
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

Note: `html.WithHardWraps()` is intentionally **absent** from
`goldmark.WithRendererOptions(...)` — see §4.3.

The goldmark instance is created fresh per call (stateless). For
the message sizes involved (5–50 KB of email body text), goldmark's
allocation profile is acceptable: benchmark results from goldmark's
own test suite show ~20–30 MB/s throughput at typical email body
sizes, placing a 10 KB body at ≈0.3–0.5ms — well within the 2ms
budget (§10).

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

### 6.5 `saveComposeCmd` — where conversion happens

In `internal/ui/compose.go`, `saveComposeCmd` reads the config and
converts before calling the executor:

```go
bodyText := snap.Body
var draftBody compose.DraftBody
if m.deps.Config.Compose.BodyFormat == "markdown" {
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

The `ComposeSnapshot.Body` field stores the raw user text (Markdown
source or plain text). The action's `Params` blob stores the
rendered HTML body for replay. This is intentional:

- **crash-recovery restore** reads from `compose_sessions.snapshot`
  (raw Markdown source → restores into textarea so the user gets
  their editable source back)
- **action replay** reads from `actions.params["body"]` (already
  rendered HTML → correct to re-PATCH Graph with the same payload)

When both a `compose_sessions` unconfirmed row and a `Pending`
action row exist for the same draft (the crash happened between
stage 1 and stage 2), the resume-compose path (spec 15 PR 7-ii)
uses the `compose_sessions` row. The action executor's drain skips
`CreateDraftReply*` / `CreateNewDraft` (non-idempotent; draft may
already exist on server). The user re-saves from the Markdown
source in the restored form, triggering a fresh Graph round-trip.

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

In `app.go`'s `tea.KeyCtrlE` handler:

```go
ext := ".eml"
if m.deps.Config.Compose.BodyFormat == "markdown" {
    ext = ".md"
}
path, err := compose.WriteTempfileExt(body, ext)
```

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
line-numbered error (CLAUDE.md §9).

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

Tables render as `<table>` / `<tr>` / `<th>` / `<td>`. Outlook's
HTML renderer supports standard table elements without special
`border` attributes.

---

## 8. Failure modes

| Failure | User-visible behaviour |
| ------- | ---------------------- |
| goldmark returns an error (structurally unreachable with `bytes.Buffer` as destination, but handled) | Status: `"markdown: …"`. Compose form remains open with the original source intact. No draft is saved. |
| User types `**bold**` expecting literal asterisks | Draft renders with `<strong>bold</strong>`. Mitigation: `[md]` footer indicator is always visible. User disables Markdown mode via `body_format = "plain"`. |
| Graph rejects HTML body (Exchange has accepted HTML drafts since Exchange 2010; rejection is not expected) | Standard draft-save error path from spec 15. Status bar shows the error; webLink shown if stage 1 succeeded. |

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
- [ ] `internal/graph/drafts.go` adds `content, contentType string` parameters to `PatchMessageBody` and `CreateNewDraft` in place of the hard-coded `"text"` string. No new imports. Existing `graph` tests (`TestPatchMessageBody`, `TestCreateNewDraft*`) updated to pass `"hello"`, `"text"` at the new positions.
- [ ] `internal/action/draft.go` imports `internal/compose`; all four `Create*` methods accept `body compose.DraftBody` and unwrap it when calling `e.gc.PatchMessageBody` / `e.gc.CreateNewDraft`. Existing executor tests (`TestCreateNewDraftSinglePost`, `TestCreateDraftReply*`, etc. in `internal/action/executor_test.go`) updated to pass `compose.DraftBody{Content: "…", ContentType: "text"}`.
- [ ] `internal/ui/app.go` imports `internal/compose`; `DraftCreator` interface uses `compose.DraftBody`; `saveComposeCmd` calls `compose.RenderMarkdown` when `BodyFormat == "markdown"`; `Ctrl+E` uses `.md` extension when `BodyFormat == "markdown"`.
- [ ] `internal/ui/dispatch_test.go` `stubDraftCreator` and `recordingDraftCreator` methods updated from `body string` to `body compose.DraftBody`.
- [ ] `internal/ui/compose_model.go` adds `MarkdownMode bool`; footer appends ` · [md]` when true.
- [ ] Both `NewCompose()` call sites that feed a rendered view (`New()` and `startComposeOfKind`) set `m.compose.MarkdownMode` from config.
- [ ] `docs/ARCH.md` §1 module-tree listing updated to include `internal/compose`.
- [ ] `go test -race ./internal/compose/...` green.
- [ ] `go test -race ./internal/config/...` green.
- [ ] `go test -race ./internal/graph/...` green (existing draft tests updated for new signature).
- [ ] `go test -race ./internal/action/...` green (existing draft tests updated).
- [ ] `go test -race ./internal/ui/...` green (stubs updated for `compose.DraftBody`).
- [ ] `go test -tags=e2e ./internal/ui/...` green: new test `TestComposeMarkdownModeFooterIndicator` asserts `[md]` appears in the compose footer frame when `MarkdownMode = true`; and a second e2e test asserts `[md]` is **absent** when `MarkdownMode = false`.
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
A regression of >50% over the budget (CLAUDE.md §5.2) fails the
benchmark test.

---

## 11. Cross-cutting checklist (CLAUDE.md §11)

- [ ] Which Graph scope(s)? `Mail.ReadWrite` only — already requested. No new scopes.
- [ ] State in store? None — no schema change.
- [ ] Graph endpoints? `PATCH /me/messages/{id}` and `POST /me/messages` (existing). The `contentType` field in the JSON payload changes; no new endpoints.
- [ ] Offline behaviour? `saveComposeCmd` runs when connected; no change to the offline path.
- [ ] Undo behaviour? No change; `DiscardDraft` is still the inverse.
- [ ] Error states? goldmark render failure surfaces a status-bar error; compose form stays open with source intact. Graph errors unchanged.
- [ ] Latency budget? §10 above; benchmarks gated per CLAUDE.md §5.2.
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
