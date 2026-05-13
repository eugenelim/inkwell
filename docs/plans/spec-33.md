# Spec 33 — Rich-text / Markdown Drafts

## Status
not-started

## DoD checklist
- [ ] `internal/compose/markdown.go`: `DraftBody` type + `RenderMarkdown` (goldmark, GFM extensions)
- [ ] `internal/compose/markdown_test.go`: full suite incl. blockquote attribution test
- [ ] `internal/compose/editor.go`: `WriteTempfileExt` + `WriteTempfile` delegates
- [ ] `internal/config/config.go`: `BodyFormat string` in `ComposeConfig`
- [ ] `internal/config/defaults.go`: `BodyFormat: "plain"`
- [ ] `internal/config/validate.go`: `body_format ∈ {"plain","markdown"}`
- [ ] `internal/config/config_test.go`: `TestConfigDecodeComposeBodyFormat` + `TestConfigValidateBodyFormatRejectsBadValue`
- [ ] `go.mod / go.sum`: goldmark added
- [ ] `internal/graph/drafts.go`: `PatchMessageBody` + `CreateNewDraft` take `content, contentType string`
- [ ] `internal/action/draft.go`: `compose.DraftBody` through all four Create* methods; unwrap for graph
- [ ] `internal/ui/app.go`: `DraftCreator` uses `compose.DraftBody`; `saveComposeCmd` converts; Ctrl+E `.md` ext
- [ ] `internal/ui/dispatch_test.go`: stubs updated for `compose.DraftBody`
- [ ] `internal/ui/compose_model.go`: `MarkdownMode bool` + `[md]` footer
- [ ] `docs/ARCH.md` §1: `internal/compose` added to module tree
- [ ] All unit/integration/e2e/bench gates green (`make regress`)
- [ ] `docs/CONFIG.md`: `[compose] body_format` row
- [ ] `docs/user/reference.md`: compose section updated
- [ ] `docs/user/how-to.md`: Markdown recipe
- [ ] `docs/plans/spec-33.md`: status=done, final iter entry
- [ ] `docs/specs/33-markdown-drafts.md`: `**Shipped:** vX.Y.Z`
- [ ] `docs/PRD.md` §10: spec 33 row shipped
- [ ] `docs/ROADMAP.md`: Bucket-4 row + §1.18 heading updated
- [ ] `README.md`: status table row + download version bumped

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `RenderMarkdown` 10 KB body | <2ms p95 | — | `BenchmarkRenderMarkdown10KB` | pending |
| `RenderMarkdown` 100 KB body | <20ms p95 | — | `BenchmarkRenderMarkdown100KB` | pending |

## Iteration log

### Iter 1 — 2026-05-13
- Slice: spec + plan (research, adversarial review ×2, both review rounds fixed)
- Commands run: none (spec-only iteration)
- Result: spec written, two adversarial reviews completed, 10+4 findings addressed
- Critique: layering violation (DraftBody in wrong layer) found and fixed; performance math fixed; `NewCompose()` call sites audited; `go.mod` DoD bullet added; stub tests made explicit
- Next: implement (start with goldmark library + compose/markdown.go)
