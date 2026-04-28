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
