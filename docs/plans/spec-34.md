# Spec 34 — Calendar invites in mail (read + hand-off)

## Status
not-started

## DoD checklist
- [ ] `internal/graph/event_message.go`: `EventMessage`, `EventMessageEvent`, `Client.GetEventMessage(ctx, msgID) (*EventMessage, error)` with the §6.2 fetch shape + recurrence-summary reduction (six pattern types + missing-`daysOfWeek` fallthrough)
- [ ] `internal/graph/event_message_test.go`: meetingRequest happy path, meetingCancelled, response-type non-panic (each of `meetingAccepted` / `meetingTenativelyAccepted` / `meetingDeclined` — Microsoft's typo preserved), 404 → typed `*GraphError`, recurrence summary table coverage
- [ ] `internal/graph/types.go`: `EventMessage` + `EventMessageEvent` decode types added; **no field added to existing `graph.Message`**
- [ ] `internal/ui/app.go::CalendarFetcher`: gains `GetEventMessage(ctx, msgID) (*graph.EventMessage, error)`; `cmd/inkwell/cmd_run.go::calendarAdapter` wires it
- [ ] `internal/render/invite.go`: `RenderInviteCard(em *graph.EventMessage, sentAt time.Time, tz *time.Location, width int) string` pure function
- [ ] `internal/render/invite_test.go`: empty-location omits line, online-only renders `💻 join`, all-day renders `· all day`, mixed required+optional breakdown, hand-off hint omitted for response cards, width<40 collapse, ⚪ pip for `notResponded` (not 🟡), typo `meetingTenativelyAccepted` matched
- [ ] `internal/render/render.go`: `BodyView.InviteCard string` added; `BodyOpts.TZ *time.Location` added; `Body()` UNCHANGED (UI assigns InviteCard out-of-band)
- [ ] Open-message UI Cmd: fetch `GetEventMessage` in parallel with `Body()` via `errgroup` when `hasExpandableEvent(MeetingMessageType)`; store `*EventMessage` as `m.viewerInvite`; reducer calls `render.RenderInviteCard` and sets `bodyView.InviteCard`
- [ ] `internal/ui/messages.go`: `inviteFetchedMsg` (or `openMsgDoneMsg.eventMessage`)
- [ ] `internal/ui/app.go`: `o` keystroke handler routes to `viewerInvite.Event.WebLink` when present + type is `meetingRequest`/`meetingCancelled`; falls through to `message.webLink`; status hint switches
- [ ] `internal/ui/panes.go`: viewer-pane View() renders `BodyView.InviteCard` above body
- [ ] `docs/ARCH.md` §1: `internal/graph/event_message.go` + `internal/render/invite.go` added to module tree
- [ ] `go test -race ./internal/graph/... ./internal/render/... ./internal/ui/...` green
- [ ] `go test -tags=e2e ./internal/ui/...`: `TestViewerInviteCardRendersAboveBody`, `TestViewerOOpensEventWebLinkOnInvite`, `TestViewerOFallsThroughOnNonInvite`
- [ ] `docs/user/reference.md`: viewer-pane invite card + `o` routing + 2-click hand-off honesty
- [ ] `docs/user/how-to.md`: "Read a meeting invite from inkwell" recipe
- [ ] `docs/PRD.md` §10: spec 34 row shipped
- [ ] `docs/ROADMAP.md`: Bucket-4 row + §1.17 backlog heading updated
- [ ] `README.md`: status table row + download version bumped
- [ ] `make regress` green across all gates

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| `GetEventMessage` Graph RTT | <300ms p95 | — | (Graph latency budget) | pending |
| `RenderInviteCard` 50-attendee event | <500µs | — | `BenchmarkRenderInviteCard` | pending |
| Viewer-open with invite (errgroup parallel) | <500ms p95 | — | (spec 05 budget) | pending |

## Iteration log

### Iter 1 — 2026-05-13 (spec + plan)
- Slice: research, draft, three-agent adversarial review, fix loop, plan
- Verifier: docs review (CLAUDE.md §12.0 spec-verification discipline)
- Commands run: codebase grep verification, web research on Graph API
- Result: spec converged after one fix-pass cycle
- Critique: three parallel review agents found 14 findings across CRITICAL/MAJOR/MINOR. CRITICAL set was largely about codebase claims I had not personally verified (EventMessage layer mismatch — render takes `*store.Message` not `*graph.Message`; `o` is already bound to message.webLink not URL picker; `Deps.CalendarTZ` doesn't exist; `BodyView` is in render.go not types.go; `meetingMessageType` enum values were wrong — `meetingResponse` / `meetingForwardNotification` don't exist; actual values are `meetingAccepted` / `meetingTenativelyAccepted` (with Microsoft's typo) / `meetingDeclined`). A second convergence pass caught a chicken-and-egg in §6.1 (errgroup fetches EventMessage but Body() would also read it from opts in same step) — resolved by keeping `Body()` unchanged and having the UI call `RenderInviteCard` directly post-errgroup.
- Next: implementation when prioritised
