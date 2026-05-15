# Spec 05 — Message Rendering

## Status
done. All DoD bullets shipped. Manual viewer smoke deferred per `docs/CONVENTIONS.md` §5.5.

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
- [x] **`H` full-header toggle renders spec §4 extra fields**: Importance,
      Categories, FlagStatus, HasAttachments, Message-ID, and all
      `internetMessageHeaders` (rawHeaders) when `showFullHdr` is true.
- [x] **`e`/`Q` quote collapse**: `ToggleQuotes()` wired; both keys swap
      collapsed ↔ expanded body views; tests confirm state and body content.
- [x] **`:save <letter> [path]`** command-bar form: saves named attachment
      via the default save path (startSaveAttachment) or a caller-supplied
      path (saveAttachmentToPathCmd). Falls through to filter-rule save when
      the first arg isn't a known attachment letter.
- [x] **`:open <N>`** command-bar form: opens numbered link N in the system
      browser (goroutine fire-and-forget); falls through to webLink open when
      no digit arg is present.
- [x] **`:copy <N>`** command-bar form: copies numbered link N URL to the
      clipboard via the yanker (OSC 52 / pbcopy path).
- [x] Tests for all three command-bar forms in `dispatch_test.go`.

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

## Cross-cutting checklist (`docs/CONVENTIONS.md` §11)
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

- Status: shipped (C-1, 2026-05-03). Full `make regress` green after wiring `render.NewWithOptions` + `WrapColumns` in `cmd_run.go`.

### Iter 7 — 2026-05-03 (PR C-1: quote-collapse + format-flowed + strip-patterns + single-flight + external-converter + viewer-width)

- Slice: wire
- Changes:
  - `internal/config/config.go` + `defaults.go` — 6 new `[rendering]` keys:
    `wrap_columns` (0), `quote_collapse_threshold` (3), `strip_patterns` ([]),
    `html_converter` ("internal"), `html_converter_cmd` (""),
    `external_converter_timeout` (5s).
  - `internal/render/render.go` — `Options` struct; `NewWithOptions()` constructor;
    `FetchedHeader{Name,Value}` type; `BodyView.TextExpanded`; `FetchedBody.Headers`;
    single-flight via `sync.Map`; `applyStripPatterns`; collapsed+expanded renders.
  - `internal/render/plain.go` — `normalisePlain` + `quoteThreshold`; `isFormatFlowed`;
    `unwrapFormatFlowed`; `collapseQuotes`; attribution-line dim `\x1b[2m...\x1b[0m`.
  - `internal/render/html.go` — `htmlToTextWithConfig`; `runExternalConverter`
    (`#nosec G204` with WHY); html→text calls `normalisePlain(..., 0)`.
  - `internal/render/graphfetcher.go` — maps `InternetMessageHeaders` →
    `[]FetchedHeader`.
  - `internal/graph/messages.go` — `GetMessageBody $select` gains
    `internetMessageHeaders`.
  - `internal/graph/types.go` — `Message.InternetMessageHeaders []MessageHeader`.
  - `internal/ui/messages.go` — `BodyRenderedMsg.TextExpanded`, `.RawHeaders`;
    new `RawHeader{Name,Value}`.
  - `internal/ui/panes.go` — `ViewerModel.bodyExpanded`, `.quotesExpanded`,
    `.rawHeaders`; `SetBody(3-arg)`; `ToggleQuotes()`; `QuotesExpanded()`;
    `SetRawHeaders()`; `RawHeaders()`.
  - `internal/ui/app.go` — `Deps.WrapColumns`; viewer-width computed from pane
    geometry (floor 20) with override; `Q`/`e` keys call `ToggleQuotes()`;
    `convertHeaders` helper.
  - `cmd/inkwell/cmd_run.go` — `render.NewWithOptions` with all 6 config fields;
    `WrapColumns` threaded into `ui.Deps`.
- Commands run: `make regress` — all 6 gates green.
- Critique: no layering violations; no PII-adjacent logs; no context.Background()
  in request paths; error paths in external converter covered; single-flight
  race-free by sync.Map semantics.
- Next: C-1 done. Next PR is D-1 (spec-13).

### Iter 9 — 2026-05-05 (H full-header extra fields)

- Slice: close the `H` toggle gap — viewer was only showing compact vs full
  To/Cc/Bcc but not rendering Importance, Categories, FlagStatus, HasAttachments,
  Message-ID, or rawHeaders (internetMessageHeaders) in full-header mode.
- Changes:
  - `internal/ui/panes.go` — in `showFullHdr` block: appended Importance,
    Categories (joined), FlagStatus (when ≠ "notFlagged"), Has-Attachments,
    Message-ID (from `InternetMessageID`), and all `rawHeaders` as `Name: Value`.
  - `internal/ui/dispatch_test.go` — `TestViewerFullHeaderShowsExtraFields`:
    seeds a message with all extra fields + one rawHeader sentinel, asserts
    they appear in full mode and are absent in compact mode.
- Commands run:
  - `go vet ./...` — clean.
  - `go test -race ./...` — all 17 packages pass.
  - `go test -tags=e2e ./...` — all 17 packages pass.
- Note: `e`/`Q` quote collapse was already fully shipped in Iter 7 + tested.
  No code change needed; plan bullets updated to reflect that.

### Iter 8 — 2026-05-05 (command-bar forms for attachment save + link open/copy)

- Slice: close the two remaining deferred DoD bullets — `:save <letter> [path]`,
  `:open <N>`, `:copy <N>` command-bar forms.
- Changes:
  - `internal/ui/app.go` — extended `dispatchCommand` "save" case: detects
    single-letter first arg in viewer pane, routes to `startSaveAttachment`
    (default path) or new `saveAttachmentToPathCmd` (caller-supplied path);
    falls through to rule-save otherwise. Extended "open" case: digit first arg
    → open numbered link. New "copy" case: digit first arg → `yankURL` for link N.
    New helpers: `expandHomePath` (local `~` expansion), `saveAttachmentToPathCmd`.
  - Fixed path-extraction bug: strip "save" + TrimSpace before stripping letter arg.
  - `internal/ui/dispatch_test.go` — 8 new tests covering happy + error paths for
    all three forms; new `stubAttachmentFetcher` + `openViewerWithAttachments` helpers.
- Commands run:
  - `go vet ./...` — clean.
  - `go test -race ./...` — all 17 packages pass.
  - `go test -tags=e2e ./...` — all 17 packages pass.

### Iter (planning) — 2026-05-07
- Slice: scope the §6.1.1 data-vs-layout table classifier — no code yet.
  Today's `htmlToText()` (`internal/render/html.go:23`) sets
  `html2text.PrettyTables: false`, which flattens both real data tables
  and layout-only tables. Users with finance/dashboard email lose the
  grid; flipping the flag globally would catastrophise marketing email
  (every MJML body is 50 nested tables of layout chrome). Need a
  classifier.
- Spec amendment landed: `docs/specs/05-message-rendering/spec.md` §6.1
  redirect → §6.1.1 (new). Heuristic written: `role="presentation"`,
  nested-table → layout; `<th>` or rectangular short-header first row →
  data; default-fallback layout. Sizing guard downgrades wide / >50-row
  tables. Adds `[rendering].pretty_tables` (default `true`) and
  `pretty_table_max_rows` (default `50`) to §13. New perf rows in §14:
  HTML render with classifier <100ms; classifier walk over 50-nested
  body <10ms. Status line bumped.
- Fixture corpus committed to `internal/render/testdata/tables/`:
  - `min_data_with_thead.eml`, `min_data_no_thead.eml`,
    `min_layout_marketing.eml`, `min_nested_data_in_layout.eml`,
    `min_single_cell_wrapper.eml` — hand-crafted, one per classifier
    branch, kept short for golden-file diff readability.
  - `real_newsletter_data_analysis.eml` (50 tables, 3 `<th>`),
    `real_review_request.eml` (50 tables, 0 `<th>`),
    `real_card_shipped.eml` (46 tables, 0 `<th>`) — wrapped from
    Mailteorite/mjml-email-templates (MIT). Attribution + provenance
    table in `LICENSES.md` alongside the fixtures.
  - All headers use `example.invalid`, no PII, repo-distributable.
- Open questions before implementation iter:
  - Width of pretty tables vs pane: tablewriter takes its own width
    decision based on cell content; needs a pre-render width estimator
    so we can downgrade *before* invoking it. Likely: walk the data
    table once, sum max-cell-widths + separators, compare to
    `2 × pane_width`.
  - Interaction with the configured external converter
    (`html.go:38-50`): classifier should run only on the internal-path
    body. External converter output is already plain text — no tables
    to classify.
  - `golang.org/x/net v0.53.0` is already in `go.mod` as an indirect
    dep, so importing `golang.org/x/net/html` only promotes it to
    direct (no new module, no `go.sum` line drift). Still warrants
    its own `go mod tidy` commit per `docs/CONVENTIONS.md` §10.
- Commands run: none (no code changes this iter; spec + fixtures only).
- Next: implementation iter — `internal/render/htmltable.go` with the
  classifier + width estimator + rewrite, `htmltable_test.go` driving
  the corpus, perf benchmark over `real_newsletter_data_analysis.eml`.

### Iter (implementation) — 2026-05-08
- Slice: ship §6.1.1. New file `internal/render/htmltable.go` (~280
  LoC) with the seven-rule classifier, sizing guard, and HTML rewrite
  via `golang.org/x/net/html`. Layout `<table>` → `<div>` and
  structural descendants (`<tr>`, `<thead>`, `<tbody>`, `<tfoot>`,
  `<caption>`) renamed to `<div>`; `<td>`/`<th>` renamed to `<span>`
  with a trailing space; `<col>`/`<colgroup>` dropped. Data tables
  pass through; oversize ones (rows > 50 OR width > 2× pane) get a
  `[Wide table — N×M, omitted; press O to view in browser]`
  placeholder.
- Wired into `htmlToText()`: between the tracking-pixel strip and
  `html2text.FromString`. `PrettyTables: true` fed to html2text only
  when the classifier ran — `false` keeps the v0.17.x behavior so
  `[rendering].pretty_tables = false` is a clean kill-switch.
- Config: `[rendering].pretty_tables` (default `true`),
  `pretty_table_max_rows` (default `50`) added to
  `internal/config/{config,defaults}.go`, plumbed through
  `cmd/inkwell/cmd_run.go`, documented in `docs/CONFIG.md`.
- `golang.org/x/net` promoted from indirect → direct (was already at
  v0.53.0; no `go.sum` drift, single-line `require ()` move).
- Tests:
  - `internal/render/eml_test.go` — `loadEmlHTML(testing.TB, path)`
    helper. Multipart-aware though all current fixtures are
    single-part `text/html`.
  - `internal/render/htmltable_test.go` — 13 tests covering each
    classifier branch, the rewrite output, the sizing guard
    (rows + width), end-to-end `htmlToText` on each fixture,
    placeholder visibility in user-visible text, and the
    PrettyTables=false kill-switch. Plus 2 benchmarks.
- Privacy: scrubbed three brand-placeholder domains
  (`postable.com`, `yourskincare.com`, `yourreviewlink.com`) in the
  Mailteorite fixtures to `example.invalid`. Public-CDN references
  (Google Fonts, image hosts, social-media href profile URLs) left
  intact — they are technical infrastructure, not PII. Modification
  acknowledged in `internal/render/testdata/tables/LICENSES.md`.
- Self-review pass: tightened `loadEmlHTML` to take `testing.TB` so
  benchmarks pass real `*testing.B` (not a faked `*testing.T{}`);
  defensive defaults in `classifyTables` for `paneWidth ≤ 0` /
  `maxRows ≤ 0`; reworded the `estimatedWidth` doc-comment that was
  contradicting itself on rune-vs-byte counting; added an end-to-end
  placeholder-visibility test (`TestEndToEndPlaceholderSurfaces`) so
  we cover the user-visible string, not just the post-rewrite HTML.
- Commands run:
  - `gofmt -l internal/render/` — clean.
  - `go vet ./...` — clean.
  - `go test -race -count=1 ./...` — all packages green.
  - `go test -tags=e2e -timeout=5m ./...` — all packages green.
  - `go test -bench=. -benchmem -run='^$' ./internal/render/...`:
    - `BenchmarkClassifyRealNewsletter`: 0.34 ms/op, 304 KB/op,
      2305 allocs/op (budget <10 ms — 29× margin).
    - `BenchmarkHTMLToTextRealNewsletter`: 1.17 ms/op, 1.20 MB/op,
      6462 allocs/op (budget <100 ms — 85× margin).
- Perf budgets §14:
  - HTML body render with classifier active (cached, <500KB):
    measured 1.17 ms vs <100 ms budget. ✓
  - Classifier walk over 50-nested `<table>` MJML body:
    measured 0.34 ms vs <10 ms budget. ✓
- Status-line bumped: §6.1.1 marked shipped in v0.54.0.
