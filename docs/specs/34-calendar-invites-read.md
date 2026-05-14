# Spec 34 — Calendar invites in mail (read + hand-off)

**Status:** Shipped.
**Shipped:** v0.63.0 (2026-05-14)
**Depends on:** Spec 05 (renderer — `render.Body(ctx, m *store.Message,
opts BodyOpts) (BodyView, error)` at `internal/render/render.go:206`,
the `BodyView` struct at `internal/render/render.go:68`), Spec 12
(calendar — `internal/graph/calendar.go` Event / EventDetail / GetEvent
patterns, `settings.Manager.ResolvedTimeZone()` for the user's IANA
TZ), Spec 02 (store — `meeting_message_type` column already exists
via migration 002), Spec 03 (graph client — `Client.Do(...)` infra,
throttle / retry).
Reuses but does not change: Spec 01 (auth — no new scopes), Spec 15
(compose — the `webLink` deep-link pattern this spec mirrors for the
invite hand-off; the existing `o` keystroke that opens
`message.webLink` in OWA, `internal/ui/app.go` viewer-pane Update
path).
**Blocks:** None. A future spec that wires `Calendars.ReadWrite`
could promote the read-only banner to inline `[A]ccept` /
`[T]entative` / `[D]ecline` keybindings without re-doing the
detection / rendering work introduced here.
**Estimated effort:** 2–3 days (one Graph fetch shape, one
viewer card, one viewer keybinding routing tweak, two fixture
sets; no schema migration, no new scopes, no new package).

---

### 0.1 Spec inventory

Calendar invitation actions in mail is item 4 of Bucket 4 in
`docs/ROADMAP.md §0` (line 83) and corresponds to backlog item §1.17
(line 281). The roadmap text is explicit about the scope gate:

> When a meeting invite arrives as mail, render the response
> options inline (`[A]ccept / [T]entative / [D]ecline`). Out of
> scope today because we lack `Calendars.ReadWrite` — but if a
> future tenant grants it, this is a small addition.

This spec ships the **read-only + hand-off** subset that the
denied-scope constraint allows: a viewer-pane invite card with
meeting metadata + a one-keystroke deep-link into Outlook on the
web, where the user clicks the RSVP button. The full inline
A/T/D version is a follow-up that requires a PRD §3.2 policy
change (relax the denial of `Calendars.ReadWrite`); it is
intentionally not scoped here.

---

## 1. Goal

When the focused message is a meeting invite, the viewer pane
shows a compact card above the body with: subject + when + where
+ organizer + required/optional counts + current response status.
A single keystroke (`o`) opens the corresponding event in Outlook
on the web — the user clicks Accept / Tentative / Decline there.

The user gain: 90 % of "what is this invite about?" is answered
without leaving the terminal. The 10 % that requires RSVP is two
clicks total (`o` then the OWA button). inkwell stays in its
read-and-triage lane without re-litigating the scope policy.

---

## 2. Non-goals

- **Inline `[A]ccept` / `[T]entative` / `[D]ecline`.** Requires
  `Calendars.ReadWrite` (denied by PRD §3.2). Out of scope. The
  hand-off-to-Outlook path is the v1 user surface.
- **Propose new time** (METHOD=COUNTER). Same scope constraint.
- **Local calendar write** (CalDAV or `~/Library/Calendars/`).
  inkwell's data model is Graph-only.
- **Parsing the raw `text/calendar` MIME part.** Microsoft Graph
  exposes the underlying `event` as a navigation property on
  `eventMessage`; `$expand=microsoft.graph.eventMessage/event`
  returns the parsed fields directly. No iCalendar bytes parsed
  ourselves — no new Go dep, no iMIP/RRULE edge cases (§6.2).
- **Recurrence expansion.** Display Graph's `recurrence.pattern`
  reduced to a one-line summary; do not expand instances.
- **Updating the response status optimistically.** Without write
  access the field is read-only; we display whatever Graph last
  reported. The next delta sync brings new state.
- **CLI mode** (`inkwell messages invite-info <id>`). Post-v1.
- **Multi-account.** Single-account v1 (PRD §6).

---

## 3. Scopes used

`Mail.ReadWrite` — already granted. Microsoft Learn's
`eventmessage-get` documents this as the only delegated scope
required to read an event message with its `event` navigation
property expanded (Mail.Read suffices, but we already hold the
broader Mail.ReadWrite for spec 15). `Calendars.Read` is **not**
required: the event reaches the client via the `messages`
endpoint with the eventMessage cast, not via `/me/events/{id}`.

**Denied scope acknowledged.** `Calendars.ReadWrite` (PRD §3.2)
is the gate on inline A/T/D. CI lint guard from CLAUDE.md §7
invariant 4 enforces. This spec ships read + hand-off; nothing
in it attempts to acquire or work around the denied scope.

---

## 4. Detection

A message is an invite when its `meetingMessageType` is one of
the values Microsoft Graph documents on the `eventMessage`
derived type (the **only** v1.0 values — verified against
`learn.microsoft.com/en-us/graph/api/resources/eventmessage`):

| `meetingMessageType` | Banner shape | `o` action |
| --- | --- | --- |
| `meetingRequest` | "📅 Meeting invite" — full card with all fields | Open `event.webLink` in OWA (RSVP there) |
| `meetingCancelled` | "🚫 Meeting cancelled" — full card, strike-through header line | Open `event.webLink` in OWA (cancellation view) |
| `meetingAccepted` | "✅ Response: accepted" — header-only block ("RSVP sent on <date>") | Falls through to `message.webLink` (existing spec-05 behaviour) |
| `meetingTenativelyAccepted` | "🟡 Response: tentative" — header-only block | Falls through to `message.webLink` |
| `meetingDeclined` | "❌ Response: declined" — header-only block | Falls through to `message.webLink` |
| `none` / empty / NULL | No banner. Treat as a normal message. | Falls through to `message.webLink` |

Microsoft's typo `meetingTenativelyAccepted` is the actual
documented enum value — preserved verbatim in code; not
"correcting" the typo because doing so would mis-match Graph's
wire format.

**Response-message rows** (`meetingAccepted` /
`meetingTenativelyAccepted` / `meetingDeclined`) are messages the
user **sent** while responding from another client, viewed from
Sent Items. The card shape is a single header line: "✅ Response:
accepted on Wed 2026-05-20" — no event-expand round-trip needed,
because the responder doesn't own the event. The card body is
empty; the user's body (often "looking forward to it") renders
unchanged.

**Why the polymorphic `$select` cast is not used.** Microsoft
Graph documents `microsoft.graph.eventMessage/event` as the
`$expand` form (verified against `eventmessage-get` Example 2).
The scalar `meetingMessageType` is selectable on `messages`
directly without a cast — Graph treats it as inherited. The
v0.11-era bug fixed in `EnvelopeSelectFields` (see
`internal/graph/types.go:122–134`) was about the envelope
endpoint's stricter polymorphic handling on early Graph
versions; the viewer-fetch path uses a fresh `GET /me/messages/{id}`
which accepts the bare property. The envelope-sync exclusion
stays as-is; the list pane's 📅 indicator continues to use the
existing subject-prefix heuristic (`isLikelyMeeting` at
`internal/ui/panes.go:2281`).

---

## 5. Module layout

### 5.1 New files

```
internal/graph/event_message.go         # GetEventMessage: $expand fetch
internal/graph/event_message_test.go    # JSON payload assertions
internal/render/invite.go               # InviteCard renderer (pure function)
internal/render/invite_test.go          # exact-output tests per banner shape
```

### 5.2 Changed files

```
internal/graph/types.go                 # EventMessage + EventMessageEvent decode types (parallel to Event/EventDetail; not added to graph.Message)
internal/render/render.go               # BodyView gains InviteCard string; BodyOpts gains TZ *time.Location (UI assigns InviteCard out-of-band — see §6.4)
internal/ui/app.go                      # CalendarFetcher gains GetEventMessage(ctx, msgID); o-keystroke routing reads m.viewerInvite; status-bar hint switches
internal/ui/panes.go                    # viewer-pane View() renders BodyView.InviteCard above body
internal/ui/messages.go                 # inviteFetchedMsg
internal/ui/list_open.go (or app.go open-msg path) # fetch eventMessage in parallel with body when meetingMessageType ≠ none
cmd/inkwell/cmd_run.go                  # calendarAdapter gains GetEventMessage wiring
docs/ARCH.md                            # render/invite.go listed in module tree §1
docs/CONFIG.md                          # no new keys
docs/user/reference.md                  # viewer pane: invite card + `o` routing
docs/user/how-to.md                     # new recipe: "Read a meeting invite from inkwell"
```

**No store schema change.** `meeting_message_type` already exists
(migration 002). The event data is **never persisted** — fetched
on viewer-open, held transiently on the model for the lifetime
of the viewer focus, discarded on row change. Rationale: events
change server-side (organizer reschedules, attendees RSVP) at a
cadence the message delta doesn't track; persisting locally
would create a staleness class with no clear invalidation signal.

---

## 6. Design

### 6.1 Detection at viewer-open time

The viewer-open path (where `GetMessageBody` runs) gains a
parallel fetch for invites. When the cached row's
`MeetingMessageType` is non-empty AND has-event (i.e. is one of
`meetingRequest` / `meetingCancelled`), both Graph calls fire
concurrently via `golang.org/x/sync/errgroup`:

```go
// In the UI's open-message Cmd (internal/ui/list_open.go or app.go
// open-msg path; the existing GetMessageBody call site):
g, gctx := errgroup.WithContext(ctx)
var body *render.BodyView
var em *graph.EventMessage
g.Go(func() error {
    bv, err := m.deps.Renderer.Body(gctx, msg, opts)
    if err != nil {
        return err
    }
    body = &bv
    return nil
})
if hasExpandableEvent(msg.MeetingMessageType) {
    g.Go(func() error {
        e, err := m.deps.Calendar.GetEventMessage(gctx, msg.ID)
        if err != nil {
            // Soft fail: log and proceed without the card. The
            // body still renders. Error is the wrapped *GraphError
            // code — never the email body.
            m.deps.Logger.Warn("invite: event fetch failed",
                "msg_id", msg.ID, "err", err.Error())
            return nil
        }
        em = e
        return nil
    })
}
if err := g.Wait(); err != nil {
    return openMsgDoneMsg{err: err}
}
// em may be nil (non-invite, or soft-fail).
return openMsgDoneMsg{body: *body, eventMessage: em}
```

`hasExpandableEvent` returns true for `meetingRequest` and
`meetingCancelled` only. Response-message types use a
header-only card built from the cached row's
`MeetingMessageType` + `SentDateTime` — no Graph round-trip.

After both calls return, the model's `viewerInvite` field holds
the `*EventMessage` (or nil). **The UI then calls
`render.RenderInviteCard` directly** (it's a pure function — no
renderer state needed) and assigns the result to
`m.viewerBody.InviteCard` in the same reducer step. The renderer's
`Body()` method stays unchanged in its single-pass contract; only
the `BodyView` struct gains the `InviteCard string` field that
the UI sets out-of-band.

This means **`BodyOpts` does NOT gain an `EventMessage` field**
(earlier draft of this spec assumed it would; the chicken-and-egg
of "Body() reads opts.EventMessage but EventMessage is fetched in
the same errgroup as Body()" is avoided by keeping the card
rendering out of `Body()`). `BodyOpts` gains only `TZ` (§6.5).

**Performance.** Concurrent execution preserves spec 05's
viewer-open budget (`<500ms p95` for `GetMessageBody`). The
invite-fetch RTT runs in parallel and only adds latency when it
exceeds the body fetch — empirically rare; the event endpoint is
typically faster than the body+attachment expand. §9 gives the
budget.

### 6.2 `GetEventMessage` — Graph fetch

`internal/graph/event_message.go`:

```go
// EventMessage is the eventMessage subtype of a Graph message.
// Only the fields the InviteCard needs are decoded.
type EventMessage struct {
    MessageID          string // mirrors the parent message id (echoed by Graph)
    MeetingMessageType string
    Event              *EventMessageEvent
}

// EventMessageEvent is the navigation-expanded event resource.
// Field set is a strict subset of graph.EventDetail (spec 12)
// plus the invite-specific Recurrence summary.
type EventMessageEvent struct {
    ID               string
    Subject          string
    Start            time.Time
    End              time.Time
    IsAllDay         bool
    Location         string
    OnlineJoinURL    string
    OrganizerName    string
    OrganizerAddress string
    ResponseStatus   string // accepted | tentativelyAccepted | declined | notResponded | none | organizer
    WebLink          string
    Recurrence       string // human-readable summary; empty for non-recurring
    Required         []EventAttendee
    Optional         []EventAttendee
}

func (c *Client) GetEventMessage(ctx context.Context, messageID string) (*EventMessage, error)
```

The implementation issues a single request:

```
GET /me/messages/{id}
  ?$select=id,meetingMessageType
  &$expand=microsoft.graph.eventMessage/event($select=
      id,subject,start,end,isAllDay,location,onlineMeeting,
      organizer,attendees,responseStatus,recurrence,webLink)
```

`$select=meetingMessageType` is the bare-property form (verified
against `eventmessage-get` and `messages-get` docs); no cast
needed because the property is inherited via the eventMessage
derived type and Graph treats the read selectable on the parent
collection. The `$expand` uses the documented cast form.

Response handling: a 200 with no `event` field (response-type
messages, or rare server quirks) decodes to `Event = nil`;
callers treat as "no card body" but still surface the
`MeetingMessageType`. A 404 surfaces a typed `*GraphError` for
the soft-fail path in §6.1.

**Recurrence summary** reduces Graph's structured `recurrence`
to a single line at decode time:

| Graph `pattern.type` | `daysOfWeek` populated? | Inkwell summary |
| --- | --- | --- |
| `daily` | n/a | "Daily" |
| `weekly` | yes | "Weekly on Monday" (lowercase daysOfWeek title-cased + comma-joined) |
| `weekly` | empty / missing | "Weekly" |
| `absoluteMonthly` | n/a | "Monthly on the 15th" |
| `relativeMonthly` | yes | "Monthly on the second Tuesday" |
| `relativeMonthly` | empty / missing | "Monthly" (graceful degradation) |
| `absoluteYearly` | n/a | "Yearly on May 20" |
| `relativeYearly` | yes | "Yearly on the second Tuesday of May" |
| `relativeYearly` | empty / missing | "Yearly" |
| (missing pattern, or unrecognized type) | — | empty string → no recurrence line |

(Only six pattern types are documented in
`learn.microsoft.com/.../recurrencepattern`; the
"unrecognized" row is forward-compat.)

### 6.3 InviteCard rendering

`internal/render/invite.go::RenderInviteCard` is a pure function:

```go
func RenderInviteCard(em *graph.EventMessage, sentAt time.Time,
    tz *time.Location, width int) string
```

`em` may be nil (no card). `sentAt` is the parent message's
`SentDateTime` (used for response-type header dates). `tz` is the
user's resolved IANA timezone (§6.5). `width` is the viewer
pane's content cell-width; the card hard-wraps at
`min(width-2, 80)`.

**Full-card shape** (`meetingRequest`, `meetingCancelled`):

```
┌────────────────────────────────────────────────┐
│ 📅 Meeting invite · 🟡 not responded           │
│                                                │
│ When:      Wed 2026-05-20 · 15:00–16:00 PDT    │
│ Where:     Conference room B  ·  💻 join         │
│ Recurs:    Weekly on Mon                       │
│ Organizer: Bob <bob@example.invalid>           │
│ Required:  5 (1 accepted · 0 tentative ·       │
│              0 declined · 4 pending)           │
│ Optional:  2                                   │
│                                                │
│ Press o to open in Outlook web (RSVP there)    │
└────────────────────────────────────────────────┘
```

Specifics:

- **First line** carries the banner kind:
  - `meetingRequest` → `📅 Meeting invite`
  - `meetingCancelled` → `🚫 Meeting cancelled` (the inner
    timestamp line is suffixed `(cancelled)` for redundancy)
  - response types: header-only (below)

  Followed by ` · ` and a coloured status pip mirroring
  `responseStatus.response`:

  | response | pip | label |
  | --- | --- | --- |
  | `accepted` | 🟢 | "accepted" |
  | `tentativelyAccepted` | 🟡 | "tentative" |
  | `declined` | 🔴 | "declined" |
  | `notResponded` | ⚪ | "not responded" |
  | `organizer` | ◆ | "you are the organizer" |
  | `none` / empty | (no pip, no label) | |

  `notResponded` is ⚪ (white circle) not 🟡 — keeping the two
  semantically different statuses visually distinct, matching
  Apple Mail / OWA convention.

- **When** uses the user's IANA timezone resolved from
  `settings.Manager.ResolvedTimeZone()` (spec 12; method returns
  `*time.Location`). Format: `<day-name> <YYYY-MM-DD> · HH:MM–HH:MM <TZ abbrev>`.
  All-day events render `<day-name> <YYYY-MM-DD> · all day` (no
  time range, no TZ abbrev — all-day is timezone-independent at
  display time per Graph's contract). Spec 12 §6.4's all-day
  handling is inherited verbatim.

- **Where** prefers `location.displayName`; if empty and an
  `onlineMeeting.joinUrl` is present, shows `💻 join`. Both
  present → `<displayName>  ·  💻 join`. Both empty → line omitted.

- **Recurs** line omitted when `Recurrence` is empty (see §6.2
  table).

- **Required / Optional** counts come from
  `Attendees[].type == "required" / "optional"`. The breakdown
  appears only for required; the line wraps to a second line at
  the `·` boundary if the cell-width is under 60 (small terminals).
  At cell-widths under 40 the breakdown collapses to bare counts
  ("Required: 5") with no parenthetical.

- **Hand-off hint** is the last line for `meetingRequest` and
  `meetingCancelled` only.

**Header-only-card shape** (`meetingAccepted`,
`meetingTenativelyAccepted`, `meetingDeclined`):

```
┌────────────────────────────────────────────────┐
│ ✅ Response: accepted · sent Wed 2026-05-20    │
└────────────────────────────────────────────────┘
```

(✅ for accepted, 🟡 for tentatively accepted, ❌ for declined.)
Single line; no event-expand round-trip needed; renders
deterministically from the cached `MeetingMessageType` +
`SentDateTime`. The user's typed reply body renders below the
card unchanged.

The card uses `lipgloss.Border()` for the box characters. Apple
Terminal.app degrades the border gracefully to ASCII per the
existing render conventions.

### 6.4 `BodyView` and `BodyOpts` extensions

`internal/render/render.go::BodyView` (currently at
`render.go:68`, **not** `types.go` — verified) gains:

```go
type BodyView struct {
    // ... existing fields
    // InviteCard, when non-empty, is the spec-34 invite metadata
    // card that paints above the body in the viewer. Empty for
    // non-invite messages and for invites where the event fetch
    // soft-failed.
    InviteCard string
}
```

`render.go::BodyOpts` gains:

```go
type BodyOpts struct {
    // ... existing fields
    // TZ is the user's resolved IANA timezone used to format
    // event times in the invite card. nil → time.UTC. Kept on
    // BodyOpts so the renderer's date-formatting helpers (which
    // may also surface times in headers in the future) have a
    // single source of truth for the zone.
    TZ *time.Location
}
```

`render.go::Body()` is **unchanged** — it does NOT touch
`InviteCard`. The UI assigns `BodyView.InviteCard` out-of-band
after the errgroup in §6.1 completes:

```go
// In the UI's openMsgDone reducer:
bodyView.InviteCard = render.RenderInviteCard(
    eventMessage, msg.SentDateTime, tz, viewerContentWidth)
m.viewerBody = bodyView
```

`RenderInviteCard` is a pure function exported from
`internal/render/invite.go`; calling it doesn't require the
`Renderer` instance. Existing call sites that construct
`BodyOpts` need no update — the new `TZ` field defaults to nil
and `BodyOpts.TZ == nil` is treated as `time.UTC`.

**Layering note.** `internal/render` already imports
`internal/graph` (it references `graph.MessageHeader` from
`InternetMessageHeaders` in `BodyOpts`). The new
`RenderInviteCard` function takes `*graph.EventMessage` as a
parameter; no struct field on `BodyOpts` references the type, so
the dependency stays on the existing edge.

### 6.5 Timezone resolution

Spec 12 wires `settings.Manager.ResolvedTimeZone() *time.Location`
(verified at `internal/settings/manager.go`). The viewer-open
path passes the resolved TZ via `BodyOpts.TZ`:

```go
tz := time.UTC
if m.deps.Settings != nil {
    if loc := m.deps.Settings.ResolvedTimeZone(); loc != nil {
        tz = loc
    }
}
opts := render.BodyOpts{..., TZ: tz}
```

`Deps.Calendar.GetEventMessage` returns the raw event times
(UTC); formatting into the local zone happens in the renderer.
No cache invalidation needed.

### 6.6 `o` keystroke routing

The viewer pane already binds `o` to "open this message's
webLink in OWA" (spec 05 — verified at
`internal/ui/app.go` viewer-pane Update). Spec 34 **augments the
target URL**, not the binding: when the focused message is a
`meetingRequest` or `meetingCancelled` AND the in-flight
`viewerInvite.Event.WebLink` is non-empty, `o` opens the
**event** webLink (lands the user on the OWA calendar event view,
where Accept/Tentative/Decline buttons are visible). Otherwise
`o` opens the **message** webLink (existing behaviour).

```go
case "o":
    var target string
    if em := m.viewerInvite; em != nil && em.Event != nil && em.Event.WebLink != "" {
        switch em.MeetingMessageType {
        case "meetingRequest", "meetingCancelled":
            target = em.Event.WebLink
        }
    }
    if target == "" {
        target = m.focusedMessage.WebLink
    }
    if target == "" {
        return m, nil // nothing to open
    }
    return m, openURLCmd(target)
```

The status-bar hint reflects the active routing:

- invite focused, event.WebLink present: `o: open invite in Outlook (RSVP there)`
- non-invite OR no event.WebLink: `o: open in Outlook` (unchanged)

The uppercase `O` URL-picker binding is untouched.

**UX honest about hand-off.** `event.webLink` opens the OWA
calendar event surface, not an inline RSVP modal — the user
clicks Accept / Tentative / Decline on that surface. This is a
two-step interaction; the spec-12 `:cal` modal's `o` keystroke
already uses the same hand-off shape and the recipe in
`docs/user/how-to.md` will document this clearly.

### 6.7 Status pip in the list pane (no change)

The list-pane 📅 indicator stays as-is (subject-prefix heuristic
in `isLikelyMeeting`). Replacing it with the canonical
`meetingMessageType` signal at envelope-sync time is the
v0.11-era regression the existing `EnvelopeSelectFields`
exclusion guards against; spec 34 does NOT re-open that
decision. The canonical signal is consumed at viewer-fetch time
where it is the same per-row cost as the body fetch.

---

## 7. Failure modes

| Failure | User-visible behaviour |
| --- | --- |
| `GetEventMessage` returns 404 (Graph false-positive on `MeetingMessageType` — rare but documented) | Body renders without the card. WARN log with the wrapped `*GraphError` code (no response body, no email body). 📅 list-pane indicator may still appear via subject-prefix heuristic. |
| `GetEventMessage` returns 403 (cross-mailbox edge case on shared inboxes) | Same soft-fail. The cached row still opens for body display. |
| `event` is null in the response (response-type messages, or server quirks) | Renderer emits the header-only card or no card; never crashes on nil. |
| `Event.WebLink` is empty | `o` falls through to message.webLink. |
| Network unavailable when opening the invite | Body renders if cached; invite card empty. Status bar: `invite: offline`. |
| Recurrence pattern is a future-added Graph value not in §6.2 table | Empty recurrence summary (no line). No regression. |
| `daysOfWeek` empty for `relativeMonthly` / `relativeYearly` | Falls through to bare frequency word ("Monthly" / "Yearly") with no day list — see §6.2 table. |
| Viewer pane width <40 cells | Card's required-breakdown line collapses to bare count; other lines hard-wrap. |
| All-day event with timezone disagreement between organizer and user | Spec 12 §6.4 all-day handling: midnight-to-midnight block in the user's resolved zone, `· all day` shown instead of times. Inherited verbatim. |

---

## 8. Definition of done

- [ ] `internal/graph/event_message.go` ships `EventMessage`, `EventMessageEvent`, and `Client.GetEventMessage(ctx, msgID) (*EventMessage, error)` with the §6.2 fetch shape and recurrence-summary reduction.
- [ ] `internal/graph/event_message_test.go` covers: (a) `meetingRequest` happy path with required+optional attendees, recurrence, and `onlineMeeting.joinUrl`; (b) `meetingCancelled` with no online meeting; (c) `meetingAccepted` / `meetingTenativelyAccepted` / `meetingDeclined` (response types — assert `Event = nil` does not panic); (d) 404 returns typed `*GraphError`; (e) each of the six recurrence pattern types renders the expected summary; (f) `weekly` with empty `daysOfWeek` returns "Weekly"; (g) `relativeMonthly` with empty `daysOfWeek` returns "Monthly".
- [ ] `internal/graph/types.go` gains the `EventMessage` and `EventMessageEvent` decode shapes. **Does NOT add a field to the existing `Message` struct** — the eventMessage is fetched and held separately by the UI (avoids the layer mismatch where the renderer takes `*store.Message` not `*graph.Message`).
- [ ] `internal/ui/app.go::CalendarFetcher` interface gains `GetEventMessage(ctx, msgID string) (*graph.EventMessage, error)`. The fetch is conceptually a calendar-event read routed via the messages endpoint, so it belongs on the existing calendar interface; a single-method peer interface would be YAGNI. Concrete adapter in `cmd/inkwell/cmd_run.go::calendarAdapter` calls through to `Client.GetEventMessage`.
- [ ] `internal/render/invite.go::RenderInviteCard(em *graph.EventMessage, sentAt time.Time, tz *time.Location, width int) string` produces the exact-output card per §6.3.
- [ ] `internal/render/invite_test.go` exact-output tests including: empty-location omits the line, online-only meeting renders `💻 join`, all-day event renders `· all day`, mixed required+optional renders the breakdown, the hand-off hint omitted for response-type cards, the card collapses to bare counts at width <40, `notResponded` renders with ⚪ (not 🟡), Microsoft's typo `meetingTenativelyAccepted` is matched in the response-type switch.
- [ ] `internal/render/render.go::BodyView` adds `InviteCard string`. `BodyOpts` adds `TZ *time.Location` only (nil → UTC). `Body()` is **unchanged** — it does not touch `InviteCard`. The UI assigns `bodyView.InviteCard = render.RenderInviteCard(em, sentAt, tz, width)` after the errgroup in §6.1 returns, before storing on the model. Existing `BodyOpts` call sites need no update.
- [ ] The open-message UI Cmd (the existing `GetMessageBody` call site — `internal/ui/app.go` or wherever the viewer-open Cmd lives) is updated to fetch `GetEventMessage` in parallel with `Renderer.Body()` via `errgroup` when `hasExpandableEvent(msg.MeetingMessageType)` is true. The `*EventMessage` is stored on the model as `m.viewerInvite`. After the errgroup returns, the reducer calls `render.RenderInviteCard(em, msg.SentDateTime, tz, viewerContentWidth)` and assigns the result to `bodyView.InviteCard` before storing on `m.viewerBody`.
- [ ] `internal/ui/messages.go` declares `inviteFetchedMsg` (or carries `eventMessage` on the existing `openMsgDoneMsg`); the reducer sets `m.viewerInvite`.
- [ ] `internal/ui/app.go` `o` keystroke handler in the viewer-pane Update path routes to `viewerInvite.Event.WebLink` when present AND `MeetingMessageType` is `meetingRequest` / `meetingCancelled`; else falls through to the existing `message.webLink` open.
- [ ] Status-bar hint switches between `"o: open invite in Outlook (RSVP there)"` and `"o: open in Outlook"` based on the focused-message routing.
- [ ] `cmd/inkwell/cmd_run.go` `calendarAdapter` (or peer adapter) wires `GetEventMessage` from `graph.Client` to the UI's interface.
- [ ] `docs/ARCH.md` §1 module-tree adds `internal/render/invite.go` and `internal/graph/event_message.go`.
- [ ] `go test -race ./internal/graph/...` green.
- [ ] `go test -race ./internal/render/...` green.
- [ ] `go test -race ./internal/ui/...` green.
- [ ] `go test -tags=e2e ./internal/ui/...` green: new test `TestViewerInviteCardRendersAboveBody` (stub renderer returns `BodyView{InviteCard: "📅 ...", Text: "body"}`; assert frame shows card glyph above body text); `TestViewerOOpensEventWebLinkOnInvite` (focused message is `meetingRequest`, `viewerInvite.Event.WebLink` non-empty → `o` emits open-URL Cmd with event webLink); `TestViewerOFallsThroughOnNonInvite` (no `viewerInvite` → `o` emits open-URL Cmd with message.webLink — regression for spec 05 binding).
- [ ] `docs/user/reference.md` viewer-pane section documents the invite card, the status pip colours, the `o` keystroke routing, and the read-only nature (two-click hand-off honesty).
- [ ] `docs/user/how-to.md` "Read a meeting invite from inkwell" recipe.
- [ ] `docs/PRD.md` §10 inventory adds the spec 34 row.
- [ ] `docs/ROADMAP.md` Bucket 4 row + §1.17 backlog heading updated.
- [ ] `README.md` status table adds the new row.
- [ ] Spec 17 cross-cut: no new file I/O paths, no new subprocess (existing `openURLCmd` from spec 05), no new external HTTP host (Graph only), no new SQL composition, no new persisted state. No spec 17 update needed.

---

## 9. Performance budgets

| Surface | Budget | Benchmark |
| --- | --- | --- |
| `GetEventMessage` Graph round-trip | <300ms p95 | covered by Graph latency budget |
| `RenderInviteCard` pure function on a 50-attendee event | <500µs | `BenchmarkRenderInviteCard` |
| Viewer-open with invite card (parallel `GetMessageBody` + `GetEventMessage` via errgroup) | <500ms p95 | budget unchanged from spec 05; parallel execution means the slower of the two RTTs dominates, not the sum |

The added round-trip occurs only on viewer-open of `meetingRequest`
/ `meetingCancelled` messages — a small minority of opens. The
card render is pure formatting. The errgroup parallel-fetch keeps
the spec-05 viewer-open budget intact.

---

## 10. Cross-cutting checklist (CLAUDE.md §11)

- [ ] Graph scope(s)? `Mail.ReadWrite` only — already requested. `Calendars.Read` not required (the event reaches the client via the message endpoint). No new scopes. `Calendars.ReadWrite` denial acknowledged in §3.
- [ ] Store reads / writes? No schema change. `meeting_message_type` already on rows from migration 002. The fetched `EventMessage` is never persisted.
- [ ] Graph endpoints? `GET /me/messages/{id}` with the eventMessage `$expand` (one new request shape). No new endpoint host or method.
- [ ] Offline behaviour? Soft-fail: body renders without the card; status hint `invite: offline`. Cached body opens.
- [ ] Undo? None — read surface.
- [ ] Error states? Soft-fail on Graph error; `o` status hint switches when the routing target changes.
- [ ] Latency budget? §9 above.
- [ ] Logs? One new WARN log site (`"invite: event fetch failed"`). Error string is the wrapped `*GraphError` code, not the response body. Body content NEVER logged. Redaction test covers the new log site.
- [ ] CLI-mode equivalent? Post-v1. Viewer surface only.
- [ ] Tests? Unit (event decode, recurrence summary, render card), integration (httptest Graph replaying the expand response), e2e (visible-delta on card glyph + `o` hand-off Cmd routing).
- [ ] Spec 17 impact? No new file I/O, no new subprocess host, no new external HTTP host, no crypto / SQL / persisted-state change. No spec 17 update needed.
- [ ] Spec 17 CI gates? No `// #nosec` annotations needed.

---

## 11. Open questions

None. The scope policy is explicit (`Calendars.ReadWrite` denied
→ hand-off only), the Graph shape is verified against
`learn.microsoft.com`, and the existing viewer / render
infrastructure takes the new `BodyView.InviteCard` field +
`BodyOpts.TZ` thread without re-architecture.

**Layering note (shipped divergence from §6.x prose):** CLAUDE.md §2
forbids `internal/ui` from importing `internal/graph`. The
implementation therefore defines a render-package mirror —
`render.Invite` / `render.InviteEvent` / `render.InviteAttendee` +
`render.InviteFromGraph` — so `CalendarFetcher.GetEventMessage`
returns `*render.Invite` and `RenderInviteCard(em *render.Invite, …)`
takes the same. The decode types `graph.EventMessage` /
`graph.EventMessageEvent` still live in `internal/graph` per §6.2.
Behaviour is identical to the prose; only the type-path moved.

---

## 12. Notes for follow-up specs

- **Inline A/T/D.** A future spec revisits the PRD §3.2 denial of
  `Calendars.ReadWrite` and, if accepted, layers
  `[A]ccept` / `[T]entative` / `[D]ecline` keybindings on top of
  this spec's card. Detection / rendering / card data flow stays
  identical; only the keystroke handlers and an optimistic-write
  path through the action queue need to be added.
- **Envelope-sync canonical signal.** A future spec could add a
  per-account capability probe at sign-in that tries
  `microsoft.graph.eventMessage/meetingMessageType` in envelope
  `$select` against the tenant; if it succeeds, switch envelope
  sync to use the canonical signal and drop the
  `isLikelyMeeting` heuristic. Skipped here to keep blast radius
  bounded.
- **Counter-propose / forward.** Same scope gate as inline A/T/D.
- **Inline event preview from `:cal` modal.** The reverse
  direction — when looking at an event in the spec-12 calendar
  modal, show "from invite: <mail subject>" with a jump-to-mail
  shortcut. Independent feature.
