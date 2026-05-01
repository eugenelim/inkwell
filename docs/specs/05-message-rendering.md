# Spec 05 — Message Rendering

**Status:** Shipped (CI scope, v0.2.x → v0.17.x). Headers + body fetch + html2text + plain-text normalisation + URL extraction + OSC 8 hyperlink wrapping all wired. v0.17.x adds the attachment visibility block between headers and body (PR 8 sibling — spec §8 amended). URL extraction hardened against parens-in-query + hard-wrapped URLs in the same release. Residual: viewer keybindings (`o`, `O`, `e`, `Q`, `1-9`, `a-z`, `Shift+A-Z`, `[`, `]`) + GetAttachment helper + save / open path + thread map + format=flowed unwrapping + attribution-line detection — all tracked under PR 10.
**Depends on:** Specs 02 (store, body cache), 03 (sync engine, on-demand body fetch), 04 (TUI viewer pane).
**Blocks:** Spec 07 (triage actions; reply/forward needs rendered body context).
**Estimated effort:** 2 days.

---

## 1. Goal

Implement the viewer pane so users can read messages: render headers, fetch bodies on demand, convert HTML to readable terminal text, list and save attachments, and follow links. This is the "read the email" feature.

## 2. Module layout

```
internal/render/
├── render.go        # public API, viewer entry point
├── headers.go       # header formatting
├── html.go          # HTML → text via go-html-to-text and external converters
├── plain.go         # plain text body normalization (line breaks, quoting)
├── attachments.go   # attachment list rendering and download
├── links.go         # link extraction and numbering
└── theme.go         # rendering-specific styles (quote color, link color, etc.)
```

The viewer pane lives in `internal/ui/viewer.go` (introduced in spec 04 as a stub) and consumes the `render` package.

## 3. Public render API

```go
package render

type Renderer interface {
    // Headers returns formatted, styled header lines.
    Headers(m *store.Message) string

    // Body renders the message body. If the body is not in the local cache,
    // returns a placeholder and triggers an async fetch via fetchFn; the
    // caller observes completion via the engine event channel.
    Body(ctx context.Context, m *store.Message, opts BodyOpts) (BodyView, error)

    // Attachments renders the inline attachment list.
    Attachments(atts []store.Attachment) string
}

type BodyOpts struct {
    Width      int      // available columns
    ShowFullHeaders bool
    Theme      Theme
}

type BodyView struct {
    Text       string         // rendered text, ready to display
    Links      []ExtractedLink // [N] referenced links, in order
    State      BodyState
}

type BodyState int
const (
    BodyReady BodyState = iota   // text is the actual body
    BodyFetching                 // text is a "Loading..." placeholder; fetch in progress
    BodyError                    // text contains an error explanation
)

type ExtractedLink struct {
    Index int
    URL   string
    Text  string
}

func New(store store.Store, graph graph.Client, cfg *config.Config) Renderer
```

The renderer is stateless beyond the references it holds. It is safe to call from any goroutine; concurrency-safety lives in `store` and `graph`.

## 4. Header rendering

Default header set (always shown):

```
From:    Bob Acme <bob.acme@vendor.com>
To:      eu.gene@example.invalid
Cc:      jane.doe@example.invalid (3 more)
Date:    Sun 2026-04-26 14:32 EDT (3h ago)
Subject: Q4 forecast
```

Rules:

- Sender shown as `Display Name <address>` if name present, else just address.
- `To` and `Cc` truncated to 3 visible recipients with `(N more)` suffix.
- `Date` shown as configured time zone, with relative-time hint in parentheses if within `[ui].relative_dates_within`.
- `Subject` rendered prominently (bold, theme-dependent).
- The `From` field is theme-styled to indicate `inferenceClassification` (focused = normal, other = muted) when in the focused inbox.

Full headers (when `[rendering].show_full_headers = true` or `:headers` toggle):

Adds:

```
Reply-To: ...
Message-ID: <...>
Importance: High
Categories: Work, Q4-Reviews
Flag: Flagged (due 2026-04-28)
Has Attachments: 2
```

Plus all `internetMessageHeaders` if Graph returned them with a `?$select=internetMessageHeaders` request. We do **not** automatically fetch full headers; full-header view requires an explicit refetch with the expanded `$select`.

## 5. Body fetch flow

The store's `bodies` table is the cache. On message-open:

1. Renderer calls `store.GetBody(ctx, messageID)`.
2. If hit: render and return. Update `last_accessed_at` (touch) async.
3. If miss: return `BodyView{State: BodyFetching, Text: "Loading message…"}` immediately. Dispatch a fetch via `graph.GetMessageBody(ctx, messageID)` (which calls `GET /me/messages/{id}?$select=body,attachments`).
4. Fetch completion writes to `bodies` and emits a `BodyFetchedMsg` to the UI.
5. The viewer re-renders.

The fetch is single-flight per message ID: if two viewer opens fire concurrently, only one Graph call goes out. Implementation: a `singleflight.Group` keyed by message ID inside the renderer (or its caller).

### 5.1 Fetch latency budget

- Cached body: render in <50ms.
- Uncached body: under 500ms p95 on a typical broadband connection. The placeholder is shown immediately; the user does not wait on a blank screen.
- Loading indicator: a subtle progress hint in the status line (`fetching…`) plus the in-pane placeholder.

### 5.2 Body $select and attachments

Body fetch uses:

```
GET /me/messages/{id}?$select=body,attachments,internetMessageHeaders&$expand=attachments
```

Why `$expand=attachments` instead of separate calls: a typical message has 0–3 attachments. One round-trip is faster than a list call followed by per-attachment fetches. The `attachments` array contains metadata only when the attachment type is `fileAttachment`; for `itemAttachment` (embedded mail items) we get the nested item summary as well, but we do not recurse into it in v1.

### 5.3 Body content type handling

Graph returns `body.contentType` as `"text"` or `"html"`. Switch on this:

- `"text"` → plain.go pipeline (§7).
- `"html"` → html.go pipeline (§6).
- Anything else → log warning, fall back to displaying raw bytes with a "Unknown content type" header.

## 6. HTML → terminal text

This is the engine that determines readability. Bad HTML rendering is the single most common complaint about TUI mail clients (see aerc, which acknowledges this openly). We invest here.

### 6.1 Default converter: `html2text`

Use `github.com/jaytaylor/html2text` for the in-process default. It handles the bulk of well-formed HTML acceptably:

- `<p>` → blank-line separated paragraphs.
- `<strong>`, `<em>` → preserved with terminal styling (bold, italic).
- `<a href="…">text</a>` → `text [N]` with the URL collected for the link list (§9).
- `<ul>`/`<ol>` → bulleted/numbered lists with proper indentation.
- `<table>` → simple text-grid rendering when small; "[Table omitted, see :open]" when large.
- `<blockquote>` → `>` quoting with theme-styled color.
- `<img>` → `[image: alt-text]` placeholder; URL collected for `:open`.
- `<br>` → newline.

This covers ~80% of corporate email well. Marketing emails with complex CSS layouts will look terrible; that's expected and is what the `:open` fallback is for.

### 6.2 External converter option

Power users can configure an external converter (`[rendering].html_converter = "external"`):

```
[rendering]
html_converter = "external"
html_converter_cmd = "pandoc -f html -t plain"
```

The renderer pipes HTML to the command's stdin, reads stdout for the rendered text. Timeout: 5 seconds. On timeout or non-zero exit: fall back to in-process html2text and log a warning.

This is also how users configure `lynx`, `w3m`, or `links` if they prefer those tools. Examples in CONFIG.md and in the user guide:

```
html_converter_cmd = "lynx -dump -stdin -force_html"
html_converter_cmd = "w3m -dump -T text/html"
html_converter_cmd = "pandoc -f html -t plain --columns=80"
```

### 6.3 Quoted reply chains

Email threads accumulate quoted history. Strategy:

- First-level quotes (`> `) rendered in muted color.
- Deeper nesting (`>> `, `>>> `) progressively more muted.
- A configurable threshold (`[rendering].quote_collapse_threshold`, default `3`) at which deeper quotes collapse to `[… 47 quoted lines …]`. The user can press `Q` in the viewer to toggle expansion.

Rationale: in long threads, the bottom of the message is always 90% repeated history. Collapsing it makes the actual *new* content the primary thing on screen.

### 6.4 Attribution lines

Patterns like:

```
On Mon, Apr 26, 2026 at 2:32 PM Bob Acme <bob@acme.com> wrote:
```

are detected (regex matching common patterns from Outlook, Apple Mail, Gmail, Thunderbird) and rendered in a slightly distinct style so the user's eye finds the boundaries between thread contributions.

### 6.5 Microsoft Outlook–specific noise

Outlook adds artifacts that we strip:

- "External email" disclaimer banners injected by the tenant's mail flow rules.
- "If you're having trouble viewing this email…" preludes.
- Long horizontal rule sequences ("──────") used as visual separators in HTML — we collapse these to a single line.
- Outlook's `Outlook-attachment` / `Outlook-AltVw` `<div>` wrappers stripped.

These transforms are configurable via `[rendering].strip_patterns`, which takes a list of regex patterns. Defaults ship with the common ones; users can add tenant-specific banners.

## 7. Plain text body normalization

For `body.contentType == "text"`:

- Normalize line endings to `\n`.
- Detect format=flowed (`Content-Type: text/plain; format=flowed`) and unwrap soft line breaks. This is critical for readable plaintext from clients that wrap at 78 columns.
- Apply quote-level styling (same as §6.3).
- Preserve trailing signature blocks but render them in muted color (signatures are detected via the `-- ` (dash dash space) sentinel, RFC 3676).

## 8. Attachment rendering

**Layout (v0.13.x): between the headers and body**, not below.
Real-tenant feedback (2026-05-01) flagged that an attachment block
appended after the body is invisible until the user scrolls to the
end — and most marketing / corporate emails have long bodies, so
"the user never sees the filenames" became a common surprise. mutt,
neomutt, and alpine all surface attachments above (or alongside)
the body for the same reason: discoverability beats reading-flow
preservation. inkwell follows that convention.

```
From:    Bob Acme <bob@vendor.invalid>
Date:    Fri, 01 May 2026 10:32:00 GMT
Subject: Q4 deck

Attach:  3 files · 4.4 MB
  Q4-forecast.pptx · 4.2MB · application/vnd.…presentation
  meeting-notes.docx · 87.0KB · application/vnd.…wordprocessing
  chart.png · 124.0KB · image/png (inline)

Hi team, attaching the deck for the Q4 review…
```

The summary line (`3 files · 4.4 MB`) gives the count + total weight
at a glance even when the per-file lines scroll off-screen on a
narrow viewer. Each per-file line carries name, human-readable
size, and content-type so the user can spot a `.exe` at the level
of the fold rather than after clicking through.

**v0.13.x scope: visibility only.** The list renders names, sizes,
content-types, and the `(inline)` flag for `cid:`-referenced
images. Save / open / accelerator-letter keybindings ride on the
broader spec 05 §12 viewer-keys work (audit-drain PR 10) — those
land alongside the `internal/graph/GetAttachment` helper and the
path-traversal guard called out in spec 17 §4.4. Until then, the
user reads what's attached, then opens the message in Outlook
(`o`) to pull the file.

### 8.0 Future: accelerator keys (PR 10)

Once PR 10 ships, each per-file line is prefixed with an
accelerator letter:

```
  [a] Q4-forecast.pptx · 4.2MB · application/vnd.…presentation
  [b] meeting-notes.docx · 87.0KB · application/vnd.…wordprocessing
  [c] chart.png · 124.0KB · image/png (inline)
```

Letters `[a]`, `[b]`, etc. become single-key shortcuts in viewer
focus:

- `a`, `b`, … → `:save` the attachment to `[rendering].attachment_save_dir`.
- `Shift+A`, `Shift+B`, … → `:open` the attachment (downloads to temp file, hands off to `open(1)`).
- `:save <letter> <path>` → save to a custom path.

Inline attachments (referenced via `cid:` in HTML) are listed but flagged `(inline)` so users understand they're images embedded in the body.

### 8.1 Download flow

`GET /me/messages/{id}/attachments/{attachmentId}/$value` returns the raw bytes. Stream to the destination file; do not buffer the whole thing in memory (some attachments are large).

For `:open`:
1. Stream to `~/Library/Caches/inkwell/attachments/{message-id}/{attachment-name}`.
2. Set permissions 0600.
3. Hand off via `exec.Command(cfg.OpenBrowserCmd, path).Start()`.
4. Schedule cleanup of that directory on app exit. Files persist long enough for the external app to read them.

### 8.2 Size warnings

Files >25MB show a confirmation prompt before download begins. The threshold is `[rendering].large_attachment_warn_mb` (default 25).

## 9. Link rendering and following

Links extracted during HTML rendering get numbered. The body shows them inline as `[N]`, and a footer lists them:

```
Hey Eu Gene,

Please review the deck [1] and the supporting data [2] before
tomorrow's call.

Bob

Links:
  [1] https://sharepoint.example.invalid/sites/M365Copilot/.../forecast.pptx
  [2] https://sharepoint.example.invalid/sites/Magnum/.../q4-data.xlsx
```

In viewer focus:

- Number keys `1`–`9` → `:open` link N.
- `:open <N>` → same.
- `:copy <N>` → copy link N to system clipboard via `pbcopy` shellout.

Links are de-duplicated: if `[1]` and `[3]` are the same URL, they share an entry.

### 9.1 OSC 8 hyperlinks (terminal-clickable)

Every URL the renderer emits — both the inline body URLs and the
`Links:` footer — is wrapped in OSC 8 hyperlink escape sequences:

```
\x1b]8;;<url>\x1b\\<text>\x1b]8;;\x1b\\
```

Terminals that support OSC 8 (iTerm2 ≥ 3.1, kitty, alacritty, foot,
wezterm, recent gnome-terminal / Konsole) render the URLs as
clickable. Cmd-click (macOS) / Ctrl-click (Linux) opens the link in
the default browser without the user dragging across pane borders.

Terminals without OSC 8 support (Apple Terminal.app, older xterm)
silently strip the escapes; URLs render as plain text and the user
falls back to the numbered `1`–`9` keys above.

Why this matters: drag-selecting a multi-line URL across the
viewer pane previously captured content from the adjacent
message-list pane (terminal rectangular selection). OSC 8 sidesteps
the selection problem entirely — users click, never drag.

Config: `[ui].clickable_links = "auto" | "always" | "never"`
(default `auto`, treated as `always`; future iteration can detect
terminal capability via `$TERM_PROGRAM` and downgrade automatically).

## 10. Web link fallback (`:open`)

When the body is unreadable (rare CSS-heavy marketing email, broken HTML), the user can press `O` (or run `:open`) to open the message in the default browser. This uses the `webLink` field from the message (which Graph populates as a deep link to OWA).

`exec.Command("open", message.WebLink).Start()` — done.

## 11. Conversation context

Below the message, the viewer shows a small thread map:

```
Thread (4 messages):
  Apr 23 14:02  Alice Smith    Original request
  Apr 24 09:15  Bob Acme       Re: Original request
* Apr 26 14:32  Bob Acme       Q4 forecast              ← current
  Apr 26 16:48  Eu Gene        Re: Q4 forecast (draft)
```

Built from `store.ListMessages(MessageQuery{ConversationID: m.ConversationID})`. Cursor-navigable: pressing `[` and `]` moves to previous/next message in the thread without leaving the viewer.

This pulls all conversation members from the cache. It does not fetch from Graph. For deep threads where some replies are in folders not subscribed for sync, those messages won't appear; that's an accepted limitation.

## 12. Viewer keybindings (added to spec 04 keymap)

When viewer pane is focused:

| Key | Action |
| --- | --- |
| `j`, `k` | Scroll body |
| `Space`, `Shift+Space` | Page down/up |
| `gg`, `G` | Top, bottom of body |
| `o` | Open in browser (uses webLink) |
| `O` | Open the focused link (prompts for number) |
| `e` | Toggle expand/collapse quoted history |
| `H` | Toggle full headers |
| `Q` | Toggle expand of all collapsed quotes |
| `1`–`9` | Open link [N] in browser |
| `a`–`z` | Save attachment with that letter |
| `Shift+A`–`Shift+Z` | Open attachment with that letter |
| `[`, `]` | Previous, next message in conversation |
| `Esc`, `q` | Return focus to list pane |

## 13. Configuration

This spec owns the `[rendering]` section. Full reference in `CONFIG.md`.

| Key | Default | Used in § |
| --- | --- | --- |
| `rendering.html_converter` | `"html2text"` | §6 |
| `rendering.html_converter_cmd` | `""` | §6.2 |
| `rendering.open_browser_cmd` | `"open"` | §10, §8.1 |
| `rendering.wrap_columns` | `0` (auto) | §6, §7 |
| `rendering.show_full_headers` | `false` | §4 |
| `rendering.attachment_save_dir` | `"~/Downloads"` | §8.1 |

New keys this spec adds (also in CONFIG.md):

| Key | Default | Used in § |
| --- | --- | --- |
| `rendering.quote_collapse_threshold` | `3` | §6.3 |
| `rendering.large_attachment_warn_mb` | `25` | §8.2 |
| `rendering.strip_patterns` | `[]` of regexes (defaults shipped in code) | §6.5 |
| `rendering.external_converter_timeout` | `"5s"` | §6.2 |

## 14. Performance budgets

| Operation | Target latency (p95) |
| --- | --- |
| Render headers from cached message | <5ms |
| Render body (cached, plain text, <100KB) | <20ms |
| Render body (cached, HTML, <500KB) | <100ms |
| Body fetch from Graph (uncached) | <500ms |
| Attachment metadata listing (cached) | <5ms |
| Conversation thread map render | <20ms |

The HTML budget is the looser one because real-world corporate HTML is gnarly. If we exceed it on a real sample, we either invest in faster HTML parsing or surface a "rendering took 200ms…" debug indicator and accept it.

## 15. Failure modes

| Scenario | Behavior |
| --- | --- |
| Body fetch fails (network) | Show `Failed to load body: <reason>. Press R to retry.` Retry uses standard sync path. |
| Body fetch returns 404 (message moved/deleted between sync and open) | Show `Message no longer exists.` Trigger a folder re-sync. |
| External HTML converter times out | Fall back to in-process `html2text`; log warning; show subtle indicator. |
| External HTML converter exits non-zero | Same as timeout. |
| Attachment download fails partway | Delete partial file; show error with retry option. |
| Attachment file already exists at save path | Prompt: overwrite, rename, cancel. |
| Inline image cid: not found | Render as `[broken inline image]` placeholder. |
| Body is suspiciously huge (>10MB after fetch) | Render only first 100KB with `[truncated; press F to see full]` indicator. |

## 16. Test plan

### Unit tests

- HTML rendering: golden-file tests over a corpus of real (anonymized) emails covering Outlook, Gmail, Apple Mail, marketing platforms (Mailchimp, Constant Contact), GitHub notifications, Microsoft system emails. Each input HTML pairs with expected text output. Regression-friendly.
- Quoted-chain detection: synthesized inputs at depths 1, 2, 3, 5, 10.
- Attribution-line patterns: positive/negative cases for each major mail client's format.
- Format=flowed unwrapping: verified against RFC 3676 examples.
- Link extraction and numbering: dedup verified.

### Integration tests

- Mocked Graph returning a body fetch; assert renderer produces expected output.
- Mocked Graph returning 404 on body fetch; assert error path.
- Real attachment download against a fixture file; assert size and content match.

### Manual smoke tests

1. Open a plain-text message; readable.
2. Open a corporate HTML message; readable; quotes collapsed.
3. Toggle quote expansion; works.
4. Open a marketing email; readable enough OR `:open` fallback launches browser.
5. Save an attachment via accelerator key; appears in `~/Downloads`.
6. Open an attachment via Shift-letter; opens in correct app.
7. Click through a conversation with `[` and `]`; renders each message.

## 17. Definition of done

- [ ] `internal/render/` package compiles, passes unit tests.
- [ ] Body fetch flow integrated with `internal/store` and `internal/graph`.
- [ ] HTML golden-file tests cover at least 20 real email samples (anonymized, committed to `internal/render/testdata/`).
- [ ] All viewer keybindings from §12 work.
- [ ] External converter fallback verified manually with `pandoc` and `lynx`.
- [ ] Attachment download tested with files at 10KB, 1MB, 25MB sizes.
- [ ] Performance budgets in §14 verified against real corpus.
- [ ] All failure modes in §15 handled (no crashes; clear error messaging).

## 18. Out of scope for this spec

- Reply / forward composition. Drafts are introduced in spec 07 (triage); this spec only renders.
- Encrypted (S/MIME) bodies. Decryption requires keychain access patterns and certificate chain validation that warrants its own spec post-v1.
- Calendar invitation rendering with accept/decline buttons (calendar is read-only in v1; spec 12 covers what we do show).
- Image rendering in-terminal (kitty/iTerm2 graphics protocols). Possible post-v1 stretch goal.
- Markdown rendering for messages where the sender used `text/markdown`. Treat as `text/plain` in v1.
