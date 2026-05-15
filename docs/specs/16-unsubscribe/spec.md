# Spec 16 — First-class unsubscribe

**Status:** v1 shipped (v0.12.0). One deliberate deviation — see §11.5.
**Depends on:** Specs 02 (store), 03 (sync engine — adds the
`List-Unsubscribe` header to the `$select` list), 04 (TUI shell), 07
(action queue / keybindings), 15 (compose, for the `mailto:`
unsubscribe path).
**Blocks:** Custom actions framework (§2 of ROADMAP) — the
`unsubscribe` operation primitive is exposed here.
**Estimated effort:** 1 day.

---

## 1. Goal

A single keystroke that handles "get me off this list." The user
focuses any newsletter-style message and presses `U`; inkwell reads
the standard headers, picks the cheapest legitimate path, and
either drafts a one-line `unsubscribe` mail or opens the URL in
the system browser. Optional follow-up: bulk-archive everything past
from the same sender so the inbox is actually clean.

The mechanism is RFC 2369 + RFC 8058 — protocols every mailing-list
implementation that wants to be deliverable already speaks. We're
not "fighting spam"; we're using the headers senders already set.

## 2. Prior art (terminal mail clients)

- **mutt / neomutt** — no built-in. Users wire macros against
  `~h "List-Unsubscribe:"`. The 90th-percentile mutt config has a
  hand-rolled key for this.
- **aerc** — `:unsubscribe` is the canonical convention. Reads
  `List-Unsubscribe`; honours both the `mailto:` and HTTPS forms;
  shows a confirm prompt before sending. Most inkwell-aligned
  precedent.
- **alot (notmuch)** — no first-class command; relies on notmuch
  searches + tagging.
- **claws-mail (GUI)** — `Tools → Unsubscribe`. Same pattern as
  aerc: parse the header, confirm, act.
- **Thunderbird / Gmail / Outlook web** — UI button surfaced on
  detect. Same underlying RFC.

We follow aerc's pattern — `:unsubscribe` (and a `U` keybinding) +
a confirm pane that previews what we're about to do. Inkwell's
twist: an optional follow-up bulk-archive of past mail from the
same sender, behind a separate opt-in.

## 3. RFC mechanics in 30 lines

`List-Unsubscribe` (RFC 2369) is a header with one or more URI
forms separated by commas. Examples:

```
List-Unsubscribe: <mailto:unsub@list.example.com?subject=remove>
List-Unsubscribe: <https://example.com/u?id=abc>
List-Unsubscribe: <mailto:u@example.com>, <https://example.com/u/abc>
```

`List-Unsubscribe-Post` (RFC 8058) adds the one-click form:

```
List-Unsubscribe: <https://example.com/unsub?id=abc>
List-Unsubscribe-Post: List-Unsubscribe=One-Click
```

When `List-Unsubscribe-Post` is present, the right action is a POST
to the HTTPS URI with body `List-Unsubscribe=One-Click` — no user
interaction required. This is what every modern marketing platform
sets, and Gmail / iCloud / Outlook auto-honour it.

Selection priority (matches aerc + Thunderbird):

1. RFC 8058 one-click HTTPS POST — fully automatic.
2. `mailto:` — open compose flow (spec 15) with the URI's body and
   subject parameters as the draft.
3. HTTPS GET — open in system browser; user finishes there.

If the message has no header, surface a friendly error.

## 4. Module layout

```
internal/unsub/
├── parse.go        # parse List-Unsubscribe + List-Unsubscribe-Post
├── execute.go      # one-click POST / mailto draft / browser open
└── parse_test.go   # the corpus of real-world headers we've seen

internal/graph/
└── messages.go     # extend $select to include
                    # `internetMessageHeaders`; one new GetHeaders helper

internal/store/
└── messages.go     # cache the header on insert; add an
                    # `unsubscribe_url` derived column for fast lookup
```

The header lives on the message row but it's noisy. v0.x persists
the parsed unsubscribe URL alone (or empty) in a new
`unsubscribe_url TEXT` column populated by sync. The full
`internetMessageHeaders` blob isn't kept locally — the renderer can
fetch it on demand for the rare case the user wants the raw view.

## 5. Graph endpoints

- `GET /me/messages/{id}` already returns `internetMessageHeaders`
  via `$select=internetMessageHeaders` — already accessible, just
  not in our delta select. Add it.
- One-click POST: external HTTPS to whatever the header named.
  Goes through `net/http` directly — NOT the Graph client, since
  it's not a Graph URL. Caller wraps with the standard transport
  + 5-second timeout + redirect cap.
- `mailto:` path goes through spec 15 (compose) → drafts on the
  server.

No new Graph scopes; `Mail.Read` covers the header read,
`Mail.ReadWrite` covers the draft (already requested).

## 6. Schema additions

Migration `004_unsubscribe.sql` (next migration number after
calendar/oof land):

```sql
ALTER TABLE messages ADD COLUMN unsubscribe_url TEXT;
ALTER TABLE messages ADD COLUMN unsubscribe_one_click INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_messages_unsubscribe ON messages(account_id, unsubscribe_url) WHERE unsubscribe_url IS NOT NULL;
```

The index covers the bulk-flow ("show me every message that has an
unsubscribe link") and the screener case ("does this sender's mail
typically carry an unsubscribe header?").

## 7. UI

### 7.1 Indicator

The list pane gets an optional `🚪` glyph (configurable; ASCII fallback
`u`) on rows whose `unsubscribe_url` is non-empty. Shown after the
date column, before the sender. Off by default — surface the
indicator behind `[ui].show_unsubscribe_indicator = true` because
many users won't care once they've cleaned up.

### 7.2 The flow

- `U` (capital, viewer or list pane) on the focused message:
  - **No header** → status bar: `no List-Unsubscribe header — try
    Outlook for this one`. No-op.
  - **One-click** → confirm pane: `Unsubscribe from <sender domain>
    via one-click?` `y` posts; `n` cancels.
  - **mailto:** → compose flow (spec 15) opens with the To: /
    Subject: / body pre-populated from the URI. User saves the
    draft, presses `s` to open in Outlook to send (spec 15
    semantics).
  - **HTTPS only** → status: `opened
    https://example.com/u?... in browser`. The user finishes there.
- The confirm pane shows the EXACT URL/address we'll act on, so the
  user can spot a phishing attempt before pressing y.

### 7.3 Bulk follow-up (separate decision)

After a successful unsubscribe (any mode), the status bar shows a
follow-up hint:

```
Unsubscribed from news@example.com · b to bulk-archive past mail · esc to dismiss
```

`b` runs `:filter ~f news@example.com --action archive --apply`
(spec 10). This is the cleanup half of "I never want to see this
again." Separate from the unsubscribe itself so the user can
selectively keep archive history.

## 8. Cmd / dispatch flow

```
U pressed
   │
   ▼
unsub.Parse(message.unsubscribe_url, one_click)
   │
   ├─ none → ErrorMsg
   ├─ one-click HTTPS → confirmModal("one-click?") → yes → POST
   │                                                  → result → status bar
   ├─ mailto → compose.OpenMailto(uri) → spec 15 draft flow
   └─ HTTPS GET → openInBrowser(uri)
```

The one-click POST is fire-and-forget; success means a 2xx response.
Failure (timeout, 4xx, 5xx) surfaces in the status bar with the
URL preserved so the user can copy and try in a browser.

## 9. Failure modes

| Failure                                    | Behaviour                                                           |
| ------------------------------------------ | ------------------------------------------------------------------- |
| No `List-Unsubscribe` header               | Friendly status; suggest "Outlook for this one".                    |
| One-click POST returns 4xx                 | Status: `unsubscribe failed (status 4xx); falling back to browser`. Open the same URL via browser as a GET.|
| One-click POST times out (>5s)             | Status with the URL; user can copy.                                  |
| `mailto:` URI malformed                    | Status: `unsubscribe address malformed; full header in message details (Tab to expand)`. |
| HTTPS URL is `http:` (not TLS)             | Status: `unsubscribe link is plain HTTP; open manually if you trust the sender`. We don't auto-open insecure URLs. |
| Sender has no past mail to bulk-archive    | `b` shows `no past mail to archive`.                                |

## 10. Privacy

- `List-Unsubscribe-Post: List-Unsubscribe=One-Click` is the
  modern unsubscribe contract — it explicitly authorises us to POST
  without user interaction. We honour it without flinching.
- The HTTP client used for the POST uses a generic User-Agent
  (`inkwell/<version>`), no cookies, no referer. There's nothing
  to leak; the URL the user clicked is already the sender's
  identifier.
- We never POST to anything we didn't extract from the source
  message's header. Open redirect attacks via the `b` follow-up
  flow: not applicable — `b` runs a local pattern, no network
  involvement.

## 11. Definition of done

- [ ] `internal/unsub/` package compiles; parse + execute + 8+
      header-corpus tests (the wild headers from AWS, Mailchimp,
      LinkedIn, Slack, Substack, Stripe, GitHub, Gmail-list).
- [ ] Sync engine's $select on the delta path includes
      `internetMessageHeaders`; the parsed `unsubscribe_url` +
      `unsubscribe_one_click` populate the store.
- [ ] Migration 004 lands cleanly.
- [ ] `U` keybinding wired in viewer + list dispatch.
- [ ] `:unsub` / `:unsubscribe` command equivalent in command mode.
- [ ] Confirm pane shows the URL/address before any action.
- [ ] One-click POST with 5s timeout + 2xx success / 4xx fallback
      logic.
- [ ] `mailto:` integrates with spec 15 (compose) — pre-populates
      the draft and uses the same save → Outlook hand-off.
- [ ] HTTPS GET opens via `openInBrowser` (macOS `open`, Linux
      `xdg-open`).
- [ ] Bulk follow-up: post-action `b` triggers
      `:filter ~f <sender> --action archive --apply`.
- [ ] Friendly errors for every failure case in §9.
- [ ] e2e teatest: unsub on a one-click message → confirm modal →
      mock POST handler observes the body + replies 200 → status
      bar shows success.
- [ ] User docs: `docs/user/reference.md` (new `U` row in viewer
      + list, new `:unsub` row); `docs/user/how-to.md` (new "Get
      off a mailing list" recipe).

## 11.5. v1 deviations

The shipped v0.12.0 implementation diverges from §11 in two places.
Both are deliberate; both are tracked for follow-up.

1. **Headers are NOT in the delta `$select`.** §11 asks the sync
   engine to add `internetMessageHeaders` to `EnvelopeSelectFields`.
   Doing so 5–10x's the per-cycle delta payload (every message gets
   a 30-row header array even when the user never presses U on it).
   Instead v1 lazy-fetches headers on the first U press for a given
   message, parses, and persists the result. Subsequent presses are
   local lookups. Cost: one extra Graph round-trip on the first
   press per message; benefit: zero overhead on the steady-state
   sync path. Schema migration `003_unsubscribe.sql` still ships so
   a future iter can populate the column from delta sync.

2. **Bulk follow-up `b` chord NOT wired in v1.** §7.3's
   "press b after a successful unsub to bulk-archive past mail
   from the same sender" depends on spec 10 (bulk preview pane),
   which has more pre-work. v1 surfaces the `:filter ~f <sender>`
   recipe in the docs/user/how-to.md "Get off a mailing list"
   section as the manual workaround.

The DoD bullets corresponding to these (the §"Definition of done"
"sync engine's $select…" line and the "Bulk follow-up `b`"
line) are intentionally not ticked. They get re-checked on the
v0.13.x follow-up.

## 12. Performance budgets

- Header parse: <1ms per message.
- Delta-sync overhead from the bigger `$select`: <10% increase in
  per-cycle wall clock against a 100k mailbox. Budget gated by
  bench in spec 03.
- One-click POST: 5s timeout. Not gated; network-bound.

## 13. Cross-cutting checklist (`docs/CONVENTIONS.md` §11)

- [ ] Scopes used: `Mail.Read` (header read), `Mail.ReadWrite` (mailto
      draft via spec 15).
- [ ] Store reads/writes: messages (read for url; sync writes
      `unsubscribe_url` + `unsubscribe_one_click`).
- [ ] Graph endpoints: existing message endpoints with extended
      `$select`. External HTTPS for one-click POST is logged
      through the same redacting slog handler — request URL
      domain logged, body NOT logged (avoid leaking the
      subscriber id).
- [ ] Offline behaviour: parsing the cached URL is offline; the
      POST and the GET are both online-only with friendly errors.
- [ ] Undo: bulk-archive follow-up uses spec 10's existing undo.
      The unsubscribe POST itself is irreversible by design — but
      the user's mailing-list subscription rarely is, so re-
      subscribing via the sender's own mechanism is the recovery.
- [ ] User errors: §9 table covers every branch.
- [ ] Latency: §12.
- [ ] Logs: header parse logs only the URL domain at INFO; full
      URL and email address scrubbed.
- [ ] CLI mode: `inkwell message unsub <id>` mirror via spec 14
      pattern. Deferred to v0.13+; the TUI is the main surface.
- [ ] Tests: header-corpus unit tests, dispatch tests for the U key,
      e2e for the one-click flow.

## 14. Open questions

- Whether to honour `List-Unsubscribe` from senders the user has
  marked as VIP / explicitly trusted. Current take: yes —
  the header is the sender's public statement that they support
  unsubscription. VIP status is about routing, not about
  preventing legitimate opt-out.
- Should the bulk follow-up default to `archive` or `delete`?
  Default `archive` — recoverable, the safer default. User config
  can flip it via `[unsub].bulk_action = "archive" | "delete"`.

## 15. Notes for follow-up specs

- Custom actions framework (§2) gets `unsubscribe` as an op
  primitive after this lands.
- Screener (1.16) can use the presence of an unsubscribe URL as
  a heuristic for "this is mailing-list traffic."
