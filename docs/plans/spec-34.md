# Spec 34 — Calendar invites in mail (read + hand-off)

## Status
done — **Shipped v0.63.0** (2026-05-14)

## DoD checklist
- [x] `internal/graph/event_message.go`: `EventMessage`, `EventMessageEvent`, `Client.GetEventMessage` with the §6.2 fetch shape + recurrence-summary reduction (six pattern types + missing-`daysOfWeek` fallthrough)
- [x] `internal/graph/event_message_test.go`: meetingRequest happy path, meetingCancelled with no online meeting, response-type non-panic (all three values), 404 → typed `*GraphError`, recurrence summary table coverage, ordinal helper coverage
- [x] `internal/graph/event_message.go`: `EventMessage` + `EventMessageEvent` decode types added; **no field added to existing `graph.Message`**
- [x] `internal/ui/app.go::CalendarFetcher`: gains `GetEventMessage(ctx, msgID) (*render.Invite, error)`. **Deviation from spec text:** returns `*render.Invite` (render-package mirror) not `*graph.EventMessage` to keep ui → graph layering clean (CLAUDE.md §2). Documented in spec §11 "Layering note".
- [x] `cmd/inkwell/cmd_run.go::calendarAdapter.GetEventMessage`: wired
- [x] `internal/render/invite.go`: `Invite` / `InviteEvent` / `InviteAttendee` mirror types + `InviteFromGraph` + `HasExpandableEvent` helper + `RenderInviteCard(em *Invite, sentAt time.Time, tz *time.Location, width int) string`
- [x] `internal/render/invite_test.go`: empty-location omits line, online-only renders `💻 join`, all-day renders `· all day`, mixed required+optional breakdown, hand-off hint omitted for response cards, width<40 collapse, ⚪ pip for `notResponded` (not 🟡), typo `meetingTenativelyAccepted` matched
- [x] `internal/render/render.go`: `BodyView.InviteCard string` added; `BodyOpts.TZ *time.Location` added; `Body()` UNCHANGED (UI assigns InviteCard out-of-band)
- [x] Open-message UI Cmd: fetch `GetEventMessage` in parallel with `Body()` via `errgroup` when `render.HasExpandableEvent(MeetingMessageType)`; response-type messages synthesise an `Invite{MeetingMessageType: ...}` without a Graph round-trip; reducer calls `render.RenderInviteCard` and sets `ViewerModel.inviteCard` + `inviteRouting` via a single `SetInvite` call
- [x] `internal/ui/messages.go`: `InviteSnapshot` type + `Invite *InviteSnapshot` on `BodyRenderedMsg`
- [x] `internal/ui/panes.go::ViewerModel`: `inviteCard` + `inviteRouting` fields; `SetInvite` / `InviteCard()` / `InviteRouting()`; `SetMessage` clears both
- [x] `internal/ui/app.go`: `o` keystroke handler routes to `viewer.InviteRouting().EventWebLink` when present + type is `meetingRequest`/`meetingCancelled`; falls through to `message.webLink`; engineActivity hint switches to "opening invite in browser (RSVP there)…" on the invite path
- [x] `internal/ui/panes.go`: viewer-pane View() renders `inviteCard` between attachments and body
- [x] `docs/ARCH.md` §1: `internal/graph/event_message.go` + `internal/render/invite.go` added to module tree
- [x] `internal/log/redact_test.go::TestRedactorScrubsInviteFetchError`: covers the new WARN log site `"invite: event fetch failed"`
- [x] `go test -race ./internal/graph/... ./internal/render/... ./internal/ui/... ./internal/log/...` green
- [x] `go test -tags=e2e ./internal/ui/...`: `TestViewerInviteCardRendersAboveBody` green
- [x] Dispatch tests: `TestViewerOOpensEventWebLinkOnInvite`, `TestViewerOOpensEventWebLinkOnMeetingCancelled`, `TestViewerOFallsThroughOnNonInvite`, `TestViewerOFallsThroughOnResponseTypeInvite`, `TestViewerOFallsThroughOnEmptyEventWebLink`, `TestViewerSetMessageClearsInviteRouting`, `TestSetMessageClearsInviteCard`, `TestIsResponseTypeInvite`
- [x] `docs/user/reference.md`: viewer-pane invite card section + `o` routing + 2-click hand-off honesty
- [x] `docs/user/how-to.md`: "Read a meeting invite from inkwell" recipe
- [x] `docs/PRD.md` §10: spec 34 row shipped
- [x] `docs/ROADMAP.md`: Bucket-4 row + §1.17 backlog heading updated
- [x] `README.md`: status table row + download version bumped to v0.63.0
- [x] `make regress` green across all gates

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `GetEventMessage` Graph RTT | <300ms p95 | not measured | (covered by Graph latency budget) | unmeasured — exercises live Graph; covered by spec-03 latency suite |
| `RenderInviteCard` 50-attendee event | <500µs | **33µs** | `BenchmarkRenderInviteCard` | ✅ 15× under budget (Apple M1 Max) |
| Viewer-open with invite (errgroup parallel) | <500ms p95 | not measured | (spec 05 budget) | preserved via parallel errgroup execution (the slower of body / event RTT dominates, not the sum) |

## Iteration log

### Iter 1 — 2026-05-13 (spec + plan)
- Slice: research, draft, three-agent adversarial review, fix loop, plan
- Verifier: docs review (CLAUDE.md §12.0 spec-verification discipline)
- Commands run: codebase grep verification, web research on Graph API
- Result: spec converged after one fix-pass cycle
- Critique: three parallel review agents found 14 findings across CRITICAL/MAJOR/MINOR. CRITICAL set was largely about codebase claims I had not personally verified (EventMessage layer mismatch — render takes `*store.Message` not `*graph.Message`; `o` is already bound to message.webLink not URL picker; `Deps.CalendarTZ` doesn't exist; `BodyView` is in render.go not types.go; `meetingMessageType` enum values were wrong — `meetingResponse` / `meetingForwardNotification` don't exist; actual values are `meetingAccepted` / `meetingTenativelyAccepted` (with Microsoft's typo) / `meetingDeclined`). A second convergence pass caught a chicken-and-egg in §6.1 (errgroup fetches EventMessage but Body() would also read it from opts in same step) — resolved by keeping `Body()` unchanged and having the UI call `RenderInviteCard` directly post-errgroup.
- Next: implementation when prioritised

### Iter 2 — 2026-05-14 (implementation)
- Slice: full implementation across graph / render / ui / cmd layers
- Verifier: `go test -race ./...` + `go test -tags=e2e ./internal/ui/...` + `BenchmarkRenderInviteCard`
- Commands run: `go build ./...`, race tests, e2e tests, bench, doc-sweep
- Result: graph + render + ui + cmd shipped; all tests green; bench 33µs (vs 500µs budget)
- Critique: adversarial-reviewer agent (`.claude/agents/adversarial-reviewer.md`) caught 1 Blocker + 6 Concerns + 5 Nits on the initial implementation. Blocker: `viewerInvite` was a Model field; SetMessage cleared the viewer's `inviteCard` but not the Model-side routing snapshot, so `o` on a non-invite after viewing an invite could route to the previous meeting's webLink. **Fix:** moved routing snapshot into `ViewerModel.inviteRouting` co-located with `inviteCard`; both clear via a single `SetMessage` call. Concern: response-type cards (meetingAccepted / Tenatively / Declined) never rendered because `openMessageCmd` only built a snapshot when `inv != nil`. **Fix:** synthesise `Invite{MeetingMessageType: ...}` for response types so `renderResponseCard` lights up. Concern: no redaction test for the new `"invite: event fetch failed"` log site. **Fix:** added `TestRedactorScrubsInviteFetchError`. Spec drift on the §6.5 / §11 `BodyOpts.EventMessage` reference — fixed. Spec §6.2 "Weekly on Mon" → "Weekly on Monday" — fixed.
- Next: doc-sweep then tag.

### Iter 3 — 2026-05-14 (doc-sweep + ship)
- Slice: ARCH §1, PRD §10, ROADMAP (bucket-4 + §1.17), README status, reference.md, how-to.md, spec/plan status flips
- Verifier: `make doc-sweep` + `make regress`
- Result: shipped v0.63.0
