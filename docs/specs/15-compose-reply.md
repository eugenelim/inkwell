# Spec 15 — Compose / Reply (drafts only)

**Status:** Ready for implementation.
**Depends on:** Spec 02 (messages + actions tables), spec 03 (action draining), spec 04 (TUI shell + keymap), spec 05 (rendered body for the quote chain), spec 07 (action queue + executor pattern).
**Blocks:** None — independent feature surface. CLI-mode compose (spec 14 enhancement) is post-v1.
**Estimated effort:** 2 days.

---

## 1. Goal

Let the user compose a new message, reply, reply-all, or forward — entirely from the TUI — without sending it. The result is a **draft** in the user's Drafts folder on the server. To finalise and send, the user opens the draft in native Outlook (one keystroke, `s`).

This spec is deliberately bounded by PRD §3.2: **`Mail.Send` is denied**. We never ship code that posts a message. We never ask the user to grant the scope. Anything that looks like sending is a UX bug.

The user gain: 95% of replies — body composition, recipient curation, attachment selection — happens in the terminal. Outlook is reduced to "click Send", which is one click and zero context-switching back into the inbox.

---

## 2. Non-goals

- Sending mail. Drafts only.
- Rich HTML composition. Drafts are plain-text in v1; Outlook auto-converts to HTML on send. Listed as deferred in PRD §6.
- Saving local-only drafts that never round-trip to Graph. Every draft created in the TUI lands in the server's Drafts folder.
- S/MIME / PGP signing or encryption.
- Inline image attachments. Plain attachments (file picker → Graph attach API) are in scope.
- Templates / signatures stored in inkwell config. The user's existing Outlook signature applies once they open the draft to send.

---

## 3. Scopes used

`Mail.ReadWrite` (already requested at sign-in per PRD §3.1). No new scopes.

---

## 4. Module layout

```
internal/action/
├── action.go              # action.Type extended: TypeCreateDraft, TypeCreateReply, …
├── apply_local.go         # local insert into messages with is_draft=1
├── types.go               # Graph translation for the four draft action types
└── replay.go              # treats draft creation as resumable on restart

internal/compose/
├── compose.go             # public surface: Compose(ctx, kind, src) → DraftRef
├── editor.go              # tea.ExecProcess wrapper; $EDITOR / $INKWELL_EDITOR / nano fallback
├── template.go            # reply / reply-all / forward body skeleton + quote chain
├── recipients.go          # parse To/Cc lines, normalise addresses
└── tempfile.go            # ~/Library/Caches/inkwell/drafts/{uuid}.eml lifecycle

internal/ui/compose_pane.go # confirmation pane after editor exits ("send via Outlook? discard? edit again?")
```

The new `internal/compose` package sits at the middle layer (alongside `action`, `render`). It depends on `action` (to enqueue the draft creation) and `render` (to format the quote chain). It does NOT depend on `graph` directly — compose builds the local artefact, the action executor talks to Graph.

---

## 5. Action types (extends spec 07's set)

```go
const (
    TypeCreateDraft        Type = "create_draft"            // brand-new message, no reply context
    TypeCreateReply        Type = "create_draft_reply"
    TypeCreateReplyAll     Type = "create_draft_reply_all"
    TypeCreateForward      Type = "create_draft_forward"
    TypeDiscardDraft       Type = "discard_draft"           // local + server delete; not in undo stack
)
```

Action `Params`:

```go
type DraftParams struct {
    // For Reply / ReplyAll / Forward: the source message id we're responding to.
    SourceMessageID string `json:"source_message_id,omitempty"`

    // Recipients. Compose populates these; the user can edit before saving.
    To  []string `json:"to,omitempty"`
    Cc  []string `json:"cc,omitempty"`
    Bcc []string `json:"bcc,omitempty"`

    Subject string   `json:"subject,omitempty"`
    Body    string   `json:"body"`              // plain text only in v1
    Attachments []AttachmentRef `json:"attachments,omitempty"`
}

type AttachmentRef struct {
    LocalPath string `json:"local_path"`        // absolute path; staged only
    Name      string `json:"name"`
    SizeBytes int64  `json:"size_bytes"`
}
```

---

## 6. The compose flow

```
        ┌───────────────────────────────┐
        │ user presses r / R / f / m    │
        │  in list pane (selected msg)  │
        │  or m anywhere (new msg)      │
        └──────────────┬────────────────┘
                       │
                       ▼
        ┌───────────────────────────────┐
        │ compose.Compose(ctx, kind,    │
        │   sourceMessage, theme)       │
        │ — assembles skeleton in       │
        │   ~/Library/Caches/inkwell/   │
        │   drafts/{uuid}.eml           │
        └──────────────┬────────────────┘
                       │
                       ▼
        ┌───────────────────────────────┐
        │ tea.ExecProcess($EDITOR file) │
        │ Bubble Tea suspends.          │
        │ User edits.                   │
        │ Editor exits.                 │
        └──────────────┬────────────────┘
                       │
                       ▼
        ┌───────────────────────────────┐
        │ compose.Parse(file) splits    │
        │ headers / body. Validates To  │
        │ is non-empty (unless Forward  │
        │ to self).                     │
        └──────────────┬────────────────┘
                       │
                       ▼
        ┌───────────────────────────────┐
        │ ConfirmPane:                  │
        │  [s] save draft & open in     │
        │      Outlook                  │
        │  [e] re-edit                  │
        │  [d] discard                  │
        └──────┬──────┬─────────┬───────┘
               │      │         │
               │      │         └──> compose.Discard()
               │      │              tempfile removed.
               │      │              UI back to list.
               │      │
               │      └──> tea.ExecProcess again
               │
               ▼
        ┌───────────────────────────────┐
        │ executor.Execute(             │
        │   Action{Type: CreateReply,…})│
        └──────────────┬────────────────┘
                       │
                       ▼
        ┌───────────────────────────────┐
        │ Graph: POST /createReply etc. │
        │ then PATCH /me/messages/{id}  │
        │ to set body + attachments.    │
        └──────────────┬────────────────┘
                       │
                       ▼
        ┌───────────────────────────────┐
        │ ActionConfirmedEvent →        │
        │ status bar: "Draft saved →    │
        │  press 's' to open in Outlook"│
        │ tempfile removed.             │
        └───────────────────────────────┘
```

### 6.1 Editor integration

`internal/compose/editor.go`:

```go
// Open suspends Bubble Tea and opens the user's editor on path.
// Returns a tea.Cmd whose Msg is composeEditedMsg{path, err}.
func Open(path string) tea.Cmd {
    editor := os.Getenv("INKWELL_EDITOR")
    if editor == "" { editor = os.Getenv("EDITOR") }
    if editor == "" { editor = "nano" }
    cmd := exec.Command(editor, path)
    return tea.ExecProcess(cmd, func(err error) tea.Msg {
        return composeEditedMsg{path: path, err: err}
    })
}
```

Reliability: vim, neovim, nano, emacs, micro, helix all play nicely with `tea.ExecProcess`. VS Code (`code --wait`) works but is slow to suspend; documented in `docs/qa-checklist.md`.

### 6.2 Body templating

`internal/compose/template.go`:

```go
type Kind int
const (
    KindNew Kind = iota
    KindReply
    KindReplyAll
    KindForward
)

// Skeleton returns the pre-populated draft body for the given kind.
// Source is nil for KindNew.
func Skeleton(kind Kind, src *store.Message, rendered render.BodyView) string
```

Reply skeleton:

```
To: bob.acme@vendor.invalid
Cc:
Subject: Re: Q4 forecast

<cursor>

On Sun 2026-04-26 14:32, Bob Acme wrote:
> Hey, attached the deck for the Q4 review…
> (quoted body, line-prefixed with "> ")
```

Reply-all skeleton: same as reply, but `To` includes the original `From` and any other `To`/`Cc` recipients (deduped against the user's own UPN).

Forward skeleton:

```
To:
Cc:
Subject: Fwd: Q4 forecast

<cursor>

---------- Forwarded message ----------
From:    Bob Acme <bob.acme@vendor.invalid>
Date:    Sun 2026-04-26 14:32
Subject: Q4 forecast
To:      …

(forwarded body)
```

The quoted body comes from `render.BodyView.Text` (already converted from HTML by spec 05's renderer). Line-prefixing with `> ` happens in `template.go`, not in render.

### 6.3 Header parsing

After the editor exits, `compose.Parse(path)` splits the file:

```go
type ParsedDraft struct {
    To, Cc, Bcc []string
    Subject     string
    Body        string
}

// Parse returns ErrInvalidDraft when the headers block is malformed
// (missing colon, unrecognised header), ErrNoRecipients when To is
// empty (forwards may bypass this; the user finalises in Outlook).
func Parse(path string) (ParsedDraft, error)
```

Format: RFC-2822-style headers up to the first blank line, then the body. We don't support folded headers in v1 (they break round-trip with `text/plain` editors). `Cc` and `Bcc` are optional. `Subject` is mandatory.

### 6.4 Outlook hand-off

After the action executor confirms the Graph round-trip, the response carries `webLink` — a URL that opens the draft in Outlook web (and via deep-link, Outlook desktop on macOS if installed).

The status bar shows: `Draft saved · 's' to open in Outlook · 'D' to discard`.

`s` runs `open <webLink>` (the macOS native handler). `D` enqueues a `discard_draft` action.

We don't poll the draft for changes after the user opens it in Outlook — the next delta sync of the Drafts folder pulls any edits the user made.

---

## 7. Local cache schema

The `messages` table already has an `is_draft INTEGER NOT NULL DEFAULT 0` column (spec 02). Drafts created in inkwell get `is_draft = 1` in the optimistic local insert, then the Graph round-trip reconciles the canonical message ID and replaces the temporary local UUID.

New table — temp-file lifecycle for incomplete drafts:

```sql
CREATE TABLE compose_sessions (
    session_id   TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,             -- 'new'|'reply'|'reply_all'|'forward'
    source_id    TEXT,                       -- message ID for reply/forward, NULL otherwise
    tempfile     TEXT NOT NULL,              -- absolute path
    created_at   INTEGER NOT NULL,
    confirmed_at INTEGER,                    -- set when user picked 's' or 'd'
    FOREIGN KEY (source_id) REFERENCES messages(id) ON DELETE SET NULL
);
CREATE INDEX idx_compose_sessions_unconfirmed
  ON compose_sessions(created_at) WHERE confirmed_at IS NULL;
```

Why: if the app crashes while the user is in the editor, the next launch finds an unconfirmed compose_session, prompts "resume draft?" and re-opens the editor on the saved tempfile. This is cheap and avoids the "lost draft" footgun.

Garbage collection: confirmed sessions older than 24h are deleted along with their tempfiles on next launch.

---

## 8. Optimistic UI rules

Same as spec 07 §7. Compose is unusual because the local row gets a temp ID that's replaced after the Graph response:

1. `Execute` inserts a new `messages` row with `id = "local:<uuid>"`, `folder_id = drafts_folder_id`, `is_draft = 1`, `subject`, `body_text`, etc.
2. UI re-renders: the user sees the draft in the Drafts folder immediately.
3. Graph confirms: executor `UPDATE`s the row, replacing `id` with the server-assigned message ID. Action's `done` row records the mapping.
4. Rollback (Graph error): executor `DELETE`s the local row. Status bar shows the error.

Idempotency: replay-on-startup of a draft creation that's already confirmed is detected by the action's `done` status; we don't double-create.

---

## 9. Keybindings (extends spec 04 keymap)

| Pane / mode      | Key       | Action                                    |
| ---------------- | --------- | ----------------------------------------- |
| List or viewer   | `r`       | Reply to focused message                  |
| List or viewer   | `R`       | Reply-all                                 |
| List or viewer   | `f`       | Forward                                   |
| Anywhere normal  | `m`       | New message (compose blank)               |
| Confirm pane     | `s`       | Save draft to server, hand off to Outlook |
| Confirm pane     | `e`       | Re-edit                                   |
| Confirm pane     | `d` / `D` | Discard the draft                         |
| Status-bar hint  | `s`       | Open most-recently-saved draft in Outlook |

`r` already has a meaning in spec 07 (`mark_read`). The collision is resolved by pane scope: in the list pane, `r` = mark-read on the focused message; in the **viewer pane** `r` = reply. This mirrors mutt and is the precedent users will recognise.

For `m`: the existing keymap binds `m` to `move`. We resolve by pane: `m` from the list pane is move; `m` from anywhere else (folders pane, viewer pane) is "new message". The command `:compose` also works from any mode and is the discoverable form.

---

## 10. Failure modes

| Failure                                 | User-visible behaviour                                                                                                |
| --------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `$EDITOR` not set, `nano` not in PATH   | Compose returns an error. Status: "no editor available — set INKWELL_EDITOR in config or install nano".               |
| Editor exits with non-zero status       | Treat as discard. Tempfile preserved on disk so the user can recover. Log path in status.                             |
| Headers malformed (parse error)         | Re-open the editor on the same tempfile, status shows the parse error inline as a `# error: ...` comment.             |
| `To` empty (Reply / ReplyAll / New)     | Confirm pane refuses `s`; status: "no recipients". User can `e` to fix, `d` to discard.                               |
| Graph 401 (token expired)               | Action sits in `Pending`; engine triggers re-auth flow per spec 03. Tempfile preserved. Replay fires on next session. |
| Graph 5xx                               | Action retries per spec 07's exponential backoff. Local row stays in Drafts. User sees "draft pending sync" badge.    |
| App crash while editor is open          | On next launch, "resume draft?" prompt; tempfile and source_id are intact in `compose_sessions`.                      |
| Draft deleted in Outlook before send    | Next delta sync removes the local row. No special handling needed.                                                    |

---

## 11. Definition of done

- [ ] `internal/compose/` package compiles, exports `Compose(ctx, kind, source)` that returns the path to a tempfile populated with the skeleton.
- [ ] `internal/compose/editor.go` opens `$INKWELL_EDITOR` / `$EDITOR` / `nano` via `tea.ExecProcess` and re-enters the TUI cleanly on exit.
- [ ] `compose.Parse` round-trips a known fixture set (reply / forward / new) and rejects malformed input with typed errors.
- [ ] `compose_sessions` table created by migration N+1 (latest schema version bumped accordingly).
- [ ] Action executor (extending spec 07) handles the four new draft types with idempotent local apply + Graph dispatch + replay.
- [ ] On `s`, the action's `webLink` is captured; the status bar exposes "open in Outlook" for 30s after.
- [ ] Discard flow deletes both the local draft row AND the server-side draft (Graph `DELETE /me/messages/{id}`).
- [ ] Crash-recovery: kill -9 the app while in the editor, restart, the resume-prompt fires and the tempfile is intact.
- [ ] `r`/`R`/`f`/`m` keybindings wired with the pane-scoped resolution rule from §9.
- [ ] e2e teatest: drives `r`, mocks the editor as a no-op that writes a fixed body, asserts confirm pane appears, drives `s`, asserts an action of type `create_draft_reply` enters the queue.
- [ ] Unit tests for `template.Skeleton` covering reply / reply-all / forward dedup logic.
- [ ] Privacy: tempfiles are mode 0600 in `~/Library/Caches/inkwell/drafts/`; cleaned on confirm or after 24h.
- [ ] No code path imports `internal/auth` (Mail.Send temptation is structurally impossible — compose only talks to the action queue).
- [ ] Lint guard (CI script) fails any source line that contains the literal string `Mail.Send` outside `docs/PRD.md` and `internal/auth/scopes.go` (where it's listed in the denied-set).

## 12. Performance budgets

| Surface                                                | Budget         |
| ------------------------------------------------------ | -------------- |
| Skeleton generation (reply with 50KB quoted body)      | <50ms          |
| Editor suspend → resume (Bubble Tea screen restore)    | <200ms (modulo editor startup) |
| `Parse` of a 100KB tempfile                            | <20ms          |
| Local optimistic insert                                | <10ms          |
| Graph round-trip (createReply → PATCH body)            | <2s p95        |

Benchmarks: `BenchmarkSkeletonReplyLargeQuote`, `BenchmarkParseLargeDraft`. Both live in `internal/compose/` and are gated per CLAUDE.md §5.2 (fail at >50% over budget).

---

## 13. Cross-cutting checklist (CLAUDE.md §11)

- [x] Scopes used: `Mail.ReadWrite` only. No `Mail.Send`. Lint guard added.
- [x] Store reads/writes: `messages` (insert local draft), `actions` (enqueue create_draft_*), `compose_sessions` (new table).
- [x] Graph endpoints: `POST /me/messages/{id}/createReply` (et al.), `POST /me/messages` (new), `PATCH /me/messages/{id}` (body + attachments), `DELETE /me/messages/{id}` (discard).
- [x] Offline behaviour: editor flow runs offline. Action sits in `Pending` until the next sync cycle reaches the server.
- [x] Undo: `discard_draft` is the inverse of `create_draft_*`. Pushed onto the undo stack like other actions.
- [x] User errors: `ErrNoEditor`, `ErrNoRecipients`, `ErrParseDraft` surfaced to the status bar with actionable messages.
- [x] Latency budget: §12 above; benchmarks gated per §5.2.
- [x] Logs: tempfile paths logged at INFO; body content NEVER logged. Redaction test covers a draft body fixture.
- [x] CLI mode: deferred to post-v1. CLAUDE.md note: `inkwell reply <id> --to=…` is a future extension; not in spec 14 v1.
- [x] Tests: unit (template + parse + dedup), integration (action queue round-trip with httptest Graph), e2e (teatest with a stub editor that writes fixed body).

---

## 14. Open questions

- Attachments: in v1, scope to "stage a local file as an attachment". Outlook's compose UI takes over for inline images and signatures.
- HTML drafts: deferred. The user's Outlook signature is HTML; pinning to plain-text means signatures won't appear until the user opens the draft in Outlook. Acceptable for v1; revisit if users push back.
- Per-account default editor: `INKWELL_EDITOR` is single-account. Multi-account is a post-v1 concern (PRD §6).

---

## 15. Notes for follow-up specs

- A future spec could add `CLI compose` (`inkwell reply <message-id> --to=...`) as an extension to spec 14. v1 is interactive-only.
- "Templates" — pre-canned reply skeletons selected by hotkey — are post-v1 (roadmap §1.x).
- Threading: replies pull `conversationId` from the source message; spec 11 (saved searches) and a future "thread view" can rely on that.
