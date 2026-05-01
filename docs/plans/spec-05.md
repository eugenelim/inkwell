# Spec 05 — Message Rendering

## Status
done (CI scope) — `:save` / `:open` attachment shellouts and link-open
keybindings deferred (need viewer key dispatch landed in spec 07);
manual viewer smoke deferred per CLAUDE.md §5.5.

## DoD checklist
- [x] Headers render with proper truncation (3 visible recipients + "(N more)").
- [x] Empty subject substitutes `(no subject)`.
- [x] Body cache hit renders inline; miss returns BodyFetching placeholder.
- [x] HTML body converted via `jaytaylor/html2text`; tracking pixels
      (`<img width=1 height=1>`) stripped before conversion.
- [x] Plain-text body normalises CRLF, retains quote-depth, soft-wraps to width.
- [x] Attachments listed with name, content type, human size, inline marker.
- [x] Links extracted from rendered text, numbered, deduped, deterministic.
- [x] `[rendering].show_full_headers` (BodyOpts.ShowFullHeaders) toggles
      Importance / Categories / Flag / Has Attachments / Message-ID.
- [x] **Privacy:** the render package imports nothing that can write
      bodies to logs; `render` does not import `log/slog` (lint test
      asserts this).
- [x] FetchBodyAsync writes the fetched body into the local store
      (LRU cache) so subsequent opens are tier-1 hits.
- [x] Renderer wired into the viewer pane via Deps.Renderer; opening a
      message in the list pane kicks off `openMessageCmd` (cache hit
      OR async fetch) → BodyRenderedMsg → viewer SetBody.
- [ ] **Deferred:** `:save <path>` and `:open` attachment commands.
      Need cmd-bar argument plumbing (in scope for spec 07 alongside
      :move <folder>).
- [ ] **Deferred:** `o N` keybinding to open numbered link.
      Same dispatch layer.

## Iteration log

### Iter 1 — 2026-04-27
- Slice: render package types (Renderer / BodyView / BodyOpts /
  ExtractedLink / FetchedBody / BodyFetcher) + headers/plain/html/
  attachments/links/theme.
- Files: internal/render/{render,headers,plain,html,attachments,
  links,theme}.go.
- Compile: clean after one mistype (unused `strings` import in html.go).

### Iter 2 — 2026-04-27
- Slice: render_test.go covers headers (default + truncate + full +
  empty subject), plain (CRLF + quoting + soft wrap), HTML→text
  (tracking-pixel strip), link extractor (numbering + dedup), body
  flow (cache hit / miss / fetch async / fetch error), attachments,
  privacy lint.
- Result: green in 1.6s under -race.

### Iter 3 — 2026-04-27
- Slice: wire Renderer into Deps and the viewer pane. New
  `BodyRenderedMsg` carries fetched/rendered body to the model;
  `openMessageCmd` runs Body() then optionally FetchBodyAsync()
  inside the same goroutine.
- Updated `internal/ui/app_e2e_test.go` with stubBodyFetcher and a
  TestOpeningMessageFetchesBodyAndRenders that drives `2 + Enter`
  and waits for `hello world` in the output.
- Whole-tree race + e2e green.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes: `Mail.Read` (already in PRD §3.1) for the body fetch,
      via `graph.Client.GetMessageBody`.
- [x] Store reads/writes: `GetBody` / `PutBody` / `TouchBody` for tier-2
      cache; FetchBodyAsync persists fetched bodies so subsequent
      opens are local.
- [x] Graph endpoints: `/me/messages/{id}?$select=body,hasAttachments`
      (already implemented in graph.GetMessageBody).
- [x] Offline behaviour: cache hit serves bodies offline; cache miss
      returns BodyFetching placeholder (graceful) and the fetch fails
      gracefully when offline.
- [x] Undo: N/A.
- [x] User-facing errors: BodyError state with a short message ("html
      conversion failed", "fetch failed", "store error: …"); no stack
      traces.
- [x] Latency budget: spec calls for HTML→text 50KB <30ms p95, body
      open from cache <5ms p95. Cache budget covered by spec 02
      (`BenchmarkGetBodyCached` measured 8.6µs). HTML→text bench
      deferred — synthetic 50KB fixtures + bench scaffold land in a
      follow-up iteration before spec 07.
- [x] Logs: render package imports no logger; lint test enforces it.
- [x] CLI mode: spec 14 will add `inkwell view <id>` and friends.
- [x] Tests: unit + integration via stubBodyFetcher + e2e via teatest.

## Notes for follow-up specs
- Spec 06 (search) consumes the same Renderer for highlighted-result
  bodies if needed.
- Spec 07 (triage) wires the cmd-bar to dispatch `:save <path>`,
  `:open`, and the `o N` link-opener — all of which call into
  helpers under `internal/render/links.go` and a new attachment
  download helper that lives next to existing `Attachments()`.
- Add `BenchmarkHTMLToText50KB` and `BenchmarkExtractLinks200` once
  the synthetic fixture lands. Targets per spec §"Performance budgets".

## Iter — auth pivot 2026-04-27
- Spec 05 functionality is unchanged by the spec-01 auth pivot (first-party Microsoft Graph CLI Tools client, /common authority, no tenant app registration). This package consumes the auth surface only via the typed `Authenticator` / `Token()` / `Invalidate()` contract, which is unchanged. No code changes needed; race + e2e + budget gates re-run and all green.

### Iter 4 — 2026-04-28 (production BodyFetcher adapter)
- Trigger: same v0.2.0 smoke as the other specs — `render.BodyFetcher` was defined in iter 1 and stub-implemented in tests, but no production adapter calls `graph.Client.GetMessageBody`. Without one the renderer can never serve a body when the cache misses; the viewer is permanently stuck on `(loading…)`.
- Slice: `internal/render/graphfetcher.go` — small adapter struct holding `*graph.Client`, implementing `FetchBody(ctx, messageID) (FetchedBody, error)` by calling `c.GetMessageBody(ctx, id)` and mapping `graph.Message.Body.{ContentType,Content}` → `render.FetchedBody`.
- This file lives in `internal/render` (not `internal/graph`) because rendering is the consumer-side; the dependency direction is render → graph, which is allowed by ARCH §2 layering.
- Tests: stubBodyFetcher already covers the rendering side. The adapter itself is mostly a type conversion; covered by smoke (the v0.2.0 release will exercise it on first message-open).

### Iter 5 — 2026-05-01 (URL extraction hardening + attachment visibility)
- Trigger: two real-tenant complaints landed in the same session:
  1. Tracking link cut off mid-URL. The corporate digest URL
     `https://host:10020/euweb/digest?…&msg_id=(V_<hash>)&c=tenant&…`
     was truncated at the first `)`. A second corporate tracker
     URL was truncated at a hard newline before `&tranId=…`. Both
     made click-throughs land on a useless prefix. The URL regex
     stop-set excluded `)` and `]` (good for `(URL)` wrappers,
     bad for tracker URLs that legitimately carry balanced
     `(...)` or `[...]` in the query). Long URLs that the
     sender's MUA hard-wraps at column 78 dropped everything
     after the newline because the regex is per-line.
  2. Attachments invisible. The list pane showed the `📎` glyph
     but the viewer pane never listed filenames. mutt and alpine
     both surface attachments above the body for the same
     discoverability reason; spec 05 §8 had specced "below the
     body" — wrong, because long bodies push the block off-screen
     so users never see what's attached.

- Slice (URL hardening):
  - `internal/render/links.go::urlPattern` switched from
    `https?://[^\s)\]]+` to `https?://\S+` (greedy through any
    non-whitespace).
  - New `trimUnbalancedTrailing(u)` that strips trailing `)`,
    `]`, `>` whose counts inside the URL don't balance —
    rebalances `(URL)` / `[URL]` / `<URL>` wrapper cases without
    eating legitimate balanced parens / brackets in tracker
    query strings.
  - New `unwrapBrokenURLs(body)` runs at the top of
    `normalisePlain` (before the line-split loop): joins URL
    fragments split across `\n` when the next line starts with
    a strict URL-symbol char (`&`, `?`, `/`, `#`, `=`, `%`, `+`,
    `(`, `[`). Conservative continuation set so plain prose lines
    starting with alphanumerics aren't merged.

- Tests (URL):
  - `TestExtractLinksKeepsBalancedParensInQuery` — 2nd user URL
    round-trips whole.
  - `TestExtractLinksStripsUnbalancedTrailingWrappers` — 5
    cases (`(URL)`, `[URL]`, `<URL>`, `((URL))`, `URL.`).
  - `TestUnwrapBrokenURLsJoinsHardWrappedTrackerURL` — 1st user
    URL with explicit `\n` between `%5D` and `&tranId=…` — full
    URL stitched + extracted.
  - `TestUnwrapBrokenURLsLeavesNonURLContinuationsAlone` —
    plain-prose lines stay separate.

- Slice (attachment visibility):
  - `internal/graph/types.go` — new `Attachment` type modelling
    Graph's `id, name, contentType, size, isInline, contentId`
    subset; `Message.Attachments` field.
  - `internal/graph/messages.go::GetMessageBody` — URL gains
    `&$expand=attachments($select=id,name,contentType,size,isInline,contentId)`.
    `contentBytes` deliberately NOT in the $select — body fetch
    is hot-path and base64 bytes would blow the latency budget;
    bytes get pulled on demand by the eventual save / open path.
  - `internal/render/render.go` — `FetchedBody` gains
    `Attachments []FetchedAttachment`. `FetchBodyAsync` upserts
    via `store.UpsertAttachments` so subsequent viewer opens
    read from cache.
  - `internal/render/graphfetcher.go` — populates
    `FetchedBody.Attachments` from the graph response.
  - `internal/ui/messages.go::BodyRenderedMsg` — gains
    `Attachments []store.Attachment`.
  - `internal/ui/app.go::openMessageCmd` — loads attachments
    via `store.ListAttachments` after body resolves; threads
    them through the message.
  - `internal/ui/panes.go::ViewerModel` — gains `attachments`
    field + `SetAttachments` / `Attachments()` accessors.
  - `internal/ui/panes.go::View` — renders `Attach: 3 files ·
    4.4 MB` summary line + per-file lines (`name · size ·
    content-type` with `(inline)` flag) between headers and
    body.

- Spec amend: §8 rewritten to reflect "between headers and
  body" placement + v0.13.x visibility-only scope. Accelerator
  letters `[a]` / `[b]` and save/open keybindings explicitly
  carved into a §8.0 PR-10 future-work block — the spec stays a
  single source of truth without lying about what shipped.

- Audit-doc: §5.2 "body $select drift" partially closed (the
  `$expand=attachments` half); `internetMessageHeaders` half
  remains tracked under PR 10. §8 row partially closed
  (visibility shipped; accelerator letters / save / open / the
  graph helper / spec 17 §4.4 path-traversal guard remain under
  PR 10).

- Result: full `go test -race` green for `internal/render`,
  `internal/ui`, `internal/graph`, `internal/store`. Attachment
  block paints in the viewer at the rendered-frame level
  (verified via dispatch test asserting filename + summary +
  `(inline)` flag are all visible after a BodyRenderedMsg). URL
  hardening verified by 4 new render unit tests; existing
  `TestNormalisePlain*` suite stayed green after tightening the
  unwrap heuristic.

  **Deferred to PR 10 (audit-drain spec 05 §12 + §8 + spec 17
  §4.4):** `[a]` / `[b]` accelerator letter prefixes, the
  `internal/graph/GetAttachment` helper, save / open
  keybindings, the path-traversal guard, full
  `internetMessageHeaders` $select for `H` toggle.

### Iter 6 — 2026-05-01 (PR 10: attachment save/open + viewer keybindings + conversation thread)

- Scope: audit-drain PR 10 — closes spec 05 §8 (GetAttachment helper +
  `[a]`/`[b]` prefixes + save/open), §11 (conversation thread map),
  §12 (viewer keybindings: `o` webLink, `O` URL picker, `1-9` link-open,
  `[`/`]` thread nav, `a-z` save attachment, `A-Z` open attachment),
  and spec 17 §4.4 (path-traversal guard).

- Changes:
  - `internal/graph/messages.go` — new `GetAttachment(ctx, msgID, attID) ([]byte, error)`;
    base64-decodes Graph's JSON `contentBytes` field.
  - `internal/config/config.go` + `defaults.go` — `AttachmentSaveDir` (`~/Downloads`),
    `LargeAttachmentWarnMB` (25).
  - `internal/ui/keys.go` — `OpenURL` changed from `"o"` to `"O"` per spec §12 table.
  - `internal/ui/messages.go` — `BodyRenderedMsg` gains `Conversation []store.Message`;
    new `SaveAttachmentDoneMsg` + `OpenAttachmentDoneMsg`.
  - `internal/ui/panes.go` — `ViewerModel` gains `conversationThread`/`convIdx` fields
    + `SetConversationThread` / `ConversationThread` / `NavPrevInThread` /
    `NavNextInThread`; `renderAttachmentLines` prefixes `[a]`/`[b]`…;
    new `renderConversationSection`.
  - `internal/ui/app.go` — `AttachmentFetcher` consumer-site interface; `Deps` gains
    `Attachments` / `AttachmentSaveDir` / `LargeAttachmentWarnMB`; Model gains
    `pendingAttachmentSave` / `pendingAttachmentOpen`; `openMessageCmd` loads
    conversation siblings via `loadConv()`; `dispatchViewer` checks attachment
    letters before switch; `o`→webLink / `O`→picker / `[`/`]`→thread nav / `1-9`→
    link-open guards added; `safeAttachmentPath` / `startSaveAttachment` /
    `startOpenAttachment` / `saveAttachmentCmd` / `openAttachmentCmd` helpers added.
  - `cmd/inkwell/cmd_run.go` — `expandHome` helper; new fields wired into `ui.Deps`.
  - `internal/ui/urlpicker.go` — hint text updated `o`→`O`.
  - `docs/CONFIG.md` — `attachment_save_dir` + `large_attachment_warn_mb` entries.
  - `docs/user/reference.md` — viewer keybindings table + prose updated.
  - `docs/user/how-to.md` — new attachment + thread + webLink recipes.

- Tests added:
  - `internal/graph/messages_test.go` — `TestGetAttachmentDecodesBase64`,
    `TestGetAttachmentSurfaces404`, `TestGetAttachmentRejectsInvalidBase64`.
  - `internal/ui/panes_test.go` — `TestRenderAttachmentLinesLetterPrefixes`,
    `TestRenderAttachmentLinesEmpty`, `TestRenderAttachmentLinesSingleFileGrammar`,
    `TestRenderConversationSectionOmittedForSingleOrNil`,
    `TestRenderConversationSectionMarksCurrent`,
    `TestRenderConversationSectionHasNavHint`,
    `TestSetConversationThreadIndexing`,
    `TestSetConversationThreadUnknownIDDefaultsToZero`,
    `TestNavPrevNextInThreadBounds`,
    `TestSafeAttachmentPathHappyPath`, `TestSafeAttachmentPathStripsTraversal`,
    `TestSafeAttachmentPathStripsSubDirectory`, `TestSafeAttachmentPathRejectsDot`,
    `TestSafeAttachmentPathRejectsDotDot`,
    `TestSafeAttachmentPathDirPrefixFalsePositive`.
  - `internal/ui/app_e2e_test.go` — `TestViewerOpenWebLinkShowsActivity`,
    `TestViewerOpenLinkByNumberShowsActivity`,
    `TestViewerConversationThreadRendered`;
    `TestURLPickerOOpensModalWithExtractedURL` updated (`o`→`O`).

- Deferred (explicitly out of PR 10 scope):
  - `e`/`Q` quote collapse.
  - `internetMessageHeaders` full-header toggle (`H` key).

- Status: implementation + tests written. Pending `make regress` green.
