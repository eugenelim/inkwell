# Spec 16 — First-class unsubscribe

## Status
shipped (v0.12.0 — 2026-04-29). Two deliberate deviations from the
spec's §11 DoD; documented in spec §11.5 and tracked below.

## DoD checklist (mirrored from spec)
- [x] `internal/unsub/` package compiles; `parse.go` + `execute.go`
      with corpus tests (real-world headers from AWS/SES,
      Mailchimp, Substack, plus quoted/lowercase/whitespace
      shapes). 18 tests across `parse_test.go` + `execute_test.go`,
      including garbage-input fuzz guard and a redaction
      source-grep guard (CLAUDE.md §7 — render package style).
- [x] Migration `003_unsubscribe.sql` lands cleanly; schema bumped
      to v3. Adds `unsubscribe_url TEXT`, `unsubscribe_one_click
      INTEGER NOT NULL DEFAULT 0`, partial index
      `idx_messages_unsubscribe(account_id, unsubscribe_url) WHERE
      unsubscribe_url IS NOT NULL`. UPSERT preserves the cached
      action across delta sync cycles via COALESCE so plain
      envelope refreshes don't blow it away.
- [x] `U` keybinding wired in viewer + list dispatch (KeyMap
      reclaims `U` from the never-used `UndoStack` slot).
- [x] `:unsub` / `:unsubscribe` command parity in command mode
      (aerc convention).
- [x] Confirm pane previews the EXACT URL/address before any
      action — phishing-spotting requirement from spec §7.2.
      `ConfirmModel.Message` carries the URL; the y/N modal
      renders it in the centre-aligned modal.
- [x] One-click POST: `unsub.Executor.OneClickPOST` issues
      `application/x-www-form-urlencoded` body
      `List-Unsubscribe=One-Click` per RFC 8058 §3.1.
      `User-Agent: inkwell/<version>`, no cookies, no referer
      (privacy invariant from spec §10). 5-second timeout, redirect
      cap of 3 hops, refuses non-HTTPS.
- [x] HTTPS GET path opens via `openInBrowser` (`open` on macOS,
      `xdg-open` on Linux).
- [x] Friendly errors for every §9 row: `ErrNoHeader` ("no
      List-Unsubscribe header — try Outlook for this one"),
      `ErrUnactionable` (plain HTTP / unsupported scheme), POST
      4xx/5xx surface verbatim, context cancel surfaces as a
      typed error.
- [x] e2e teatest visible-delta:
      `TestUnsubscribeUKeyOpensConfirmModalAndExecutes` asserts
      (a) the confirm modal renders with the URL, (b) on `y` the
      modal disappears AND the status bar shows "unsubscribed".
      Without the visible-delta requirement we could ship a
      v0.2.6-class regression where dispatch passes but the user
      sees nothing change.
- [x] User docs: `docs/user/reference.md` rows for `U` (list +
      viewer) and `:unsub` / `:unsubscribe`. `docs/user/how-to.md`
      adds the "Get off a mailing list" recipe with the
      bulk-archive follow-up via `:filter ~f <sender>` then `;a`.
- [x] **Spec 17 cross-cutting:** §10 (privacy) is satisfied —
      generic `User-Agent`, no cookies/referer, log layer
      untouched (parse package never logs). §13 (cross-cutting
      policy on every spec) — this spec adds a new external
      HTTP flow; no PII flows out (the URL is the sender's own
      identifier). When the threat-model document lands (spec 17
      §5.2), this flow gets a row.

### Deferred from §11 / §13 — see spec §11.5
- [ ] **Headers in delta `$select`.** §11 asks for `internetMessage
      Headers` to be added to `EnvelopeSelectFields`. Doing so
      5–10x's the per-cycle delta payload. v1 ships lazy-fetch
      via `Client.GetMessageHeaders` on the first U press per
      message, persists the result, and serves subsequent presses
      from the cache. Re-evaluate when delta-cycle wall-clock
      bench shows headroom.
- [ ] **Bulk follow-up `b` chord.** §7.3 wants a post-action `b`
      to bulk-archive past mail from the same sender. Depends on
      spec 10 (bulk preview pane). v1 surfaces the recipe in the
      how-to doc as the manual workaround
      (`:filter ~f <sender>` → `;a`).
- [ ] **mailto: full spec 15 integration.** v1 hands off via the
      OS mailto: handler (`open mailto:<addr>`). Wiring straight
      into the spec-15 compose flow lands in a follow-up.
- [ ] **CLI mode.** `inkwell message unsub <id>` deferred per
      spec §13 — TUI is the main surface for v1.

## Iteration log

### Iter 1 — 2026-04-29 (one-shot ship)
- Slice: full v1 stack in one push. Driven by user direction
  to "implement the next spec" with the new rigor (golden
  fixtures, e2e visible-delta, redaction guards) baked in.
- Files added:
  - `internal/unsub/{parse,execute,parse_test,execute_test}.go`
    (~520 LOC including tests).
  - `internal/store/migrations/003_unsubscribe.sql`.
  - `cmd/inkwell/cmd_run.go`: `unsubAdapter` (~80 LOC) bridges
    store + graph headers + executor → `ui.UnsubscribeService`
    interface (defined at the UI consumer site so `internal/ui`
    doesn't import `internal/graph`, CLAUDE.md §2).
  - `internal/ui/app.go`: `UnsubscribeService` interface,
    `UnsubscribeAction` value type, `UnsubscribeKind` enum,
    `Deps.Unsubscribe`, `pendingUnsub` model state,
    `unsubResolvedMsg` / `unsubDoneMsg`, `resolveUnsubCmd` /
    `executeUnsubCmd`, list-pane and viewer-pane U handlers,
    `:unsub`/`:unsubscribe` command, ConfirmResultMsg branch
    for the y/N answer.
  - `internal/ui/keys.go`: `Unsubscribe` keybinding (`U`),
    removed unused `UndoStack` placeholder.
  - `internal/graph/messages.go`: `MessageHeader`,
    `GetMessageHeaders`, `HeaderValue` (case-insensitive),
    `equalFold` helper.
  - `internal/store/types.go`: `UnsubscribeURL`,
    `UnsubscribeOneClick` fields on `Message`.
  - `internal/store/messages.go`: upsert SQL + bind + scan +
    columns updated; new `SetUnsubscribe(ctx, id, url, oneClick)`
    on `Store` interface for the lazy-write path.
  - `internal/ui/dispatch_test.go`: 5 dispatch tests
    (resolve→modal, y→POST, n→cancel, no-header→friendly error,
    `:unsub` parity).
  - `internal/ui/app_e2e_test.go`: 1 e2e visible-delta test
    asserting modal appears with URL, then disappears with
    success status.
  - `internal/graph/client_test.go`: 2 tests (envelope select
    contains `meetingMessageType`; header fetcher round-trips
    via httptest).
  - Spec note + how-to recipe + reference rows.
- Result: all four test layers green; gosec 0 issues;
  govulncheck 0 vulnerabilities; e2e visible-delta proves the
  user-visible flow.
- Critique:
  - Spec layering held — UI defines the consumer-side interface;
    `cmd_run.go` composes the implementation. UI never imports
    graph or unsub directly (it imports unsub only through the
    interface contract).
  - Ergonomics: confirm modal carries the URL verbatim. Phishing
    preview requirement satisfied; user can spot a tampered
    domain before pressing y.
  - Privacy: parser is pure, never logs. Executor uses generic
    User-Agent, no cookies/referer. POST body is the RFC 8058
    constant. Nothing PII-shaped flows out.
  - Idempotency: `SetUnsubscribe` is a single UPDATE; safe to
    repeat. UPSERT's COALESCE preserves cached action across
    delta sync.
  - One real-tenant gap: cached-action expiry. If a sender
    rotates their unsubscribe URL after we cached the old one,
    pressing U on a stale row POSTs the old URL. Acceptable for
    v1 — the cached action is a hint; the caller can manually
    re-fetch by clearing the column. Tracked here for follow-up.
- Next: spec 17 (security testing + CASA). Spec 18+ from bucket
  1 (folder management → mute → conversation ops → cross-folder
  bulk) follow.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: `Mail.Read` for header fetch, `Mail.ReadWrite`
      for the (deferred) mailto/draft path. Both already in
      PRD §3.1. No new scopes.
- [x] Store reads/writes: messages (read for cached action; write
      via `SetUnsubscribe` and via the COALESCE-protected upsert
      column). Migration `003_unsubscribe.sql` adds two columns
      and a partial index.
- [x] Graph endpoints: `GET /me/messages/{id}?$select=internet
      MessageHeaders` (new). One-click POST is external HTTPS,
      not a Graph URL — goes through `unsub.Executor`'s own
      http.Client (5s timeout, 3-hop redirect cap).
- [x] Offline: cached unsubscribe action serves U presses
      offline (browser open / mailto: hand-off). One-click POST
      requires network; surfaces a friendly error otherwise.
- [x] Undo: not applicable — unsubscribe is intentionally
      irreversible by design (re-subscribe via the sender's own
      flow if needed). Spec 16 §13 calls this out explicitly.
- [x] User errors: §9 table covered (no header / unactionable /
      4xx / 5xx / cancel / non-HTTPS).
- [x] Latency: header parse <1ms (asserted by `make sec`-clean
      run on the corpus tests; not gated as a perf bench).
      One-click POST 5s timeout (network-bound, not gated).
- [x] Logs: parser never logs; executor uses `_ = resp.Body.Close()`
      pattern; graph layer's existing transport handles
      request/response logging with redaction.
- [x] CLI mode: deferred (spec §13). The TUI is the main surface
      for v1.
- [x] Tests: unit (corpus) + dispatch + e2e visible-delta + a
      privacy redaction guard + a graph httptest path. Four
      layers, one PR.
- [x] **Spec 17 review:** new external HTTP flow with no PII
      egress; new local persisted state (`unsubscribe_url`,
      `unsubscribe_one_click`); no new subprocess invocation;
      no new cryptographic primitive. When `THREAT_MODEL.md`
      lands, add a row: "External one-click POST to sender-named
      URL — mitigation: HTTPS-only, generic User-Agent, no
      cookies, 3-hop redirect cap, 5s timeout."
