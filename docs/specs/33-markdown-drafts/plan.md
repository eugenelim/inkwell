# Spec 33 — Rich-text / Markdown Drafts

## Status
done

## DoD checklist
- [x] `internal/compose/markdown.go`: `DraftBody` type + `RenderMarkdown` (goldmark v1.8.2, GFM extensions)
- [x] `internal/compose/markdown_test.go`: full suite incl. blockquote attribution test + GFM table/strikethrough/task list/autolink coverage + render.BodyView roundtrip
- [x] `internal/compose/editor.go`: `WriteTempfileExt` + `WriteTempfile` delegates
- [x] `internal/compose/editor_test.go`: UUID-prefix naming + mode 0600 + delegation tests
- [x] `internal/config/config.go`: `BodyFormat string` in `ComposeConfig`
- [x] `internal/config/defaults.go`: `BodyFormat: "plain"`
- [x] `internal/config/validate.go`: `body_format ∈ {"plain","markdown"}`
- [x] `internal/config/config_test.go`: `TestConfigDecodeComposeBodyFormat` + `TestConfigValidateBodyFormatRejectsBadValue`
- [x] `go.mod / go.sum`: goldmark v1.8.2 added
- [x] `internal/graph/drafts.go`: `PatchMessageBody` + `CreateNewDraft` take `content, contentType string`
- [x] `internal/graph/drafts_test.go`: new HTML and text contentType assertion tests
- [x] `internal/action/draft.go`: `compose.DraftBody` through all Create* methods; unwrap for graph; `content_type` persisted in `action.Params`
- [x] `internal/action/executor_test.go`: existing tests updated to wrap as `compose.DraftBody`
- [x] `internal/ui/app.go`: `DraftCreator` uses `compose.DraftBody`; `ComposeBodyFormat` field on `Deps`; `Ctrl+E` `.md` ext
- [x] `internal/ui/compose.go`: `saveComposeCmd` converts via `RenderMarkdown` when `snap.MarkdownMode`
- [x] `internal/ui/compose_model.go`: `MarkdownMode bool` on model + snapshot; `newComposeWithFormat` helper; `[md]` footer
- [x] `internal/ui/compose_model_test.go`: footer indicator present/absent + snapshot round-trip + newComposeWithFormat tests
- [x] `internal/ui/dispatch_test.go`: stubs updated for `compose.DraftBody`; new `TestComposeMarkdownModePlainTextStillSendsHTML` + `TestComposePlainModeSendsText` end-to-end dispatch tests
- [x] `cmd/inkwell/cmd_run.go`: draftAdapter passes `compose.DraftBody`; `ComposeBodyFormat: cfg.Compose.BodyFormat` wired into Deps
- [x] `cmd/inkwell/cmd_messages.go`: CLI reply/reply-all/forward wrap body as `compose.DraftBody{Content: body, ContentType: "text"}`
- [x] `docs/ARCH.md` §1: `internal/compose` added to module tree
- [x] `docs/CONFIG.md`: `[compose] body_format` row
- [x] `docs/user/reference.md`: compose section updated with Markdown-drafts subsection + Outlook caveats
- [x] `docs/user/how-to.md`: "Compose with Markdown formatting (spec 33)" recipe
- [x] `docs/specs/33-markdown-drafts/plan.md`: status=done
- [x] `docs/specs/33-markdown-drafts/spec.md`: `**Shipped:** v0.62.0`
- [x] `docs/PRD.md` §10: spec 33 row shipped
- [x] `docs/product/roadmap.md`: Bucket-4 row + §1.18 backlog heading updated
- [x] `README.md`: status table row + download version bumped
- [x] `make regress` green (all five gates: gofmt, vet, build, race, e2e, integration, benchmarks)

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `RenderMarkdown` 10 KB body | <2ms p95 | 0.245ms (~8× headroom) | `BenchmarkRenderMarkdown10KB` | green |
| `RenderMarkdown` 100 KB body | <20ms p95 | 3.82ms (~5× headroom) | `BenchmarkRenderMarkdown100KB` | green |

Apple M5, single core; `go test -bench=. -benchmem -run=^$
./internal/compose/...` from a clean checkout.

## Iteration log

### Iter 1 — 2026-05-13 (spec + plan)
- Slice: spec + plan (research, adversarial review ×2, both review rounds fixed)
- Verifier: docs review
- Commands run: none (spec-only iteration)
- Result: spec written, two adversarial reviews completed, 10+4 findings addressed
- Critique: layering violation (DraftBody in wrong layer) found and fixed; performance math fixed; `NewCompose()` call sites audited; `go.mod` DoD bullet added; stub tests made explicit
- Next: third Opus-grade review pass before implementation

### Iter 2 — 2026-05-13 (Opus adversarial review)
- Slice: three parallel review agents over the spec
- Verifier: docs review
- Result: 24 findings (5 CRITICAL, 6 MAJOR, 8 MINOR) — all addressed
- Critique: caught the `cmd_run.go::draftAdapter` and `cmd_messages.go` CLI consumer surfaces that prior rounds missed; resolved `ComposeSnapshot.MarkdownMode` need + `action.Params["content_type"]` persistence
- Next: implementation

### Iter 3 — 2026-05-13 (implementation + ship)
- Slice: full implementation per spec
- Verifier: `make regress` (all gates) + new tests (`TestComposeMarkdownModeFooterIndicator`, `TestComposeMarkdownModePlainTextStillSendsHTML`, `TestComposeSnapshotPreservesMarkdownMode`, `TestRenderMarkdownRoundTripsThroughBodyView`, `BenchmarkRenderMarkdown10KB/100KB`, `TestPatchMessageBody_HTMLContentType`, `TestCreateNewDraft_HTMLContentType`, `TestConfigDecodeComposeBodyFormat`, `TestConfigValidateBodyFormatRejectsBadValue`)
- Commands run: `go build ./...`, `go test -race ./internal/...`, `make regress`
- Result: green across all five gates; benchmark numbers logged above
- Critique: implementation followed the spec line-for-line; the per-spec `Verifier:` line discipline (`docs/CONVENTIONS.md` §13 light GDE) made each slice concrete
- Next: tag v0.62.0
