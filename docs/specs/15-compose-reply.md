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

> **v2 (PR after spec 15 v1).** The original spec described a
> `tea.ExecProcess($EDITOR)` flow that suspended Bubble Tea while
> the user edited a tempfile. Real-tenant feedback flagged this as
> non-intuitive: with the TUI suspended, `[s]ave / [d]iscard`
> couldn't appear at the bottom — the user had to "select Exit
> command first" (`:wq` / Ctrl-X / etc.) before the post-edit
> confirm modal would surface. The redesign below replaces the
> editor-driven flow with an **in-modal compose pane** that keeps
> inkwell's UI on screen the entire time. A `$EDITOR` drop-out
> for power users (`Ctrl+E` while in the body field) is a
> follow-up.

```
        ┌───────────────────────────────┐
        │ user presses r / R / f / m    │
        │  in viewer pane (r/R/f) or    │
        │  anywhere (m for new)         │
        └──────────────┬────────────────┘
                       │
                       ▼
        ┌───────────────────────────────┐
        │ ComposeMode opens. Form is    │
        │ pre-filled from skeleton:     │
        │   To       = src.FromAddress  │
        │   Subject  = "Re: <subj>"     │
        │   Body     = quote chain +    │
        │              empty cursor pos │
        │ Body field has focus.         │
        └──────────────┬────────────────┘
                       │
                       ▼
        ┌───────────────────────────────┐
        │ User edits in-place. Tab      │
        │ cycles fields (Body→To→Cc→    │
        │ Subject→Body). Footer always  │
        │ visible: Ctrl+S/Esc save ·    │
        │ Ctrl+D discard · Tab cycle.   │
        └──────────────┬────────────────┘
                       │
            ┌──────────┴──────────┐
            │                     │
            ▼                     ▼
     ┌──────────────┐      ┌─────────────┐
     │ Ctrl+S / Esc │      │ Ctrl+D      │
     │  save        │      │  discard    │
     └──────┬───────┘      └──────┬──────┘
            │                     │
            ▼                     ▼
     ┌──────────────┐      ┌─────────────┐
     │ Snapshot →   │      │ Drop form   │
     │ saveCompose- │      │ state. Mode │
     │ Cmd. Recipi- │      │ → Normal.   │
     │ ent recovery │      │ Status:     │
     │ from source. │      │ "discarded" │
     └──────┬───────┘      └─────────────┘
            │
            ▼
     ┌─────────────────────────────────┐
     │ executor.CreateDraftReply       │
     │ (action queue, two-stage:       │
     │  POST /createReply,             │
     │  PATCH /me/messages/{id}).      │
     └────────────────┬────────────────┘
                      │
                      ▼
     ┌─────────────────────────────────┐
     │ draftSavedMsg → status bar:     │
     │ "✓ draft saved · press s to     │
     │  open in Outlook"               │
     └─────────────────────────────────┘
```

**Why the structural pivot.** The user-visible bottom-line fix is
that save / discard hints live in a footer that is *always*
on-screen during compose, instead of only after the user has
exited an external editor. Secondary wins:
- No tempfile lifecycle to manage (no leaked files on crash).
- Headers can be edited in dedicated single-line inputs, so an
  accidentally-cleared `To:` is harder to produce than in a free-
  form text file.
- Recipient recovery (empty `To:` falls back to source's
  `FromAddress`) still applies — the safety net survives.

**Scope of this spec revision.** Reply only (matches the v1
shipped surface). Reply-all / forward / new message land with
PR 7-iii alongside the corresponding action types. `$EDITOR`
drop-out via `Ctrl+E` is post-MVP.

### 6.1 In-modal compose pane

`internal/ui/compose_model.go::ComposeModel`. Header fields use
`bubbles/textinput`; body uses `bubbles/textarea`. Focus tracking
is at the model level: only the focused component receives
keystrokes. The pane occupies the full screen during compose
(centered modal box) so the body has vertical room for replies of
any length.

```go
type ComposeModel struct {
    Kind     ComposeKind   // Reply | ReplyAll | Forward | New (MVP: Reply)
    SourceID string
    to, cc, subject textinput.Model
    body     textarea.Model
    focused  ComposeFieldKind
}

func NewCompose() ComposeModel
func (m *ComposeModel) ApplyReplySkeleton(src store.Message, renderedBody string)
func (m *ComposeModel) NextField()       // Tab
func (m *ComposeModel) PrevField()       // Shift+Tab
func (m ComposeModel) Snapshot() ComposeSnapshot
func (m *ComposeModel) Restore(s ComposeSnapshot)
func (m ComposeModel) UpdateField(msg tea.Msg) (ComposeModel, tea.Cmd)
func (m ComposeModel) View(t Theme, w, h int) string
```

`UpdateField` forwards to whichever component holds focus. Tab /
Shift+Tab / Ctrl+S / Esc / Ctrl+D are handled at the
`internal/ui/app.go::updateCompose` layer above (see §9).

### 6.1.1 `$EDITOR` drop-out (post-MVP)

Power users keep their muscle memory via `Ctrl+E` while in the
body field: opens the body slice in `$INKWELL_EDITOR` /
`$EDITOR` / nano. On editor exit, the body returns to the
textarea; headers stay in their form fields the whole time. The
existing `internal/compose/{editor,parse}.go` helpers retarget for
this drop-out path. Lands in a follow-up PR; not in MVP.

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

Reply skeleton populates the form's individual fields and the
textarea's body block (the headers no longer share the body's
text region; v1's "To: …\nCc: …\n…\n\n<body>" tempfile shape is
v2's separate-fields model):

```
[ form fields ]
   To:      bob.acme@vendor.invalid
   Cc:
   Subject: Re: Q4 forecast

[ body textarea ]
   <cursor>

   On Sun 2026-04-26 14:32, Bob Acme wrote:
   > Hey, attached the deck for the Q4 review…
   > (quoted body, line-prefixed with "> ")
```

The legacy `compose.ReplySkeleton(src, body)` is reused — it
emits the canonical header-block + quote-chain string. The
in-modal `ApplyReplySkeleton` strips the leading
`To:.../Cc:.../Subject:.../\n` block and lands the remaining body
into the textarea, while populating the field inputs from `src`
directly. Keeping one skeleton formatter means the post-MVP
`Ctrl+E` drop-out can hand the same string to `$EDITOR`.

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

### 6.3 Form snapshot + dispatch

The in-modal flow doesn't parse a tempfile — fields live as typed
values in the form components. On `Ctrl+S` / `Esc` the model
emits a `ComposeSnapshot` struct (the JSON-serialisable view used
by §7's crash recovery) and `saveComposeCmd` consumes it:

```go
type ComposeSnapshot struct {
    Kind     ComposeKind `json:"kind"`
    SourceID string      `json:"source_id,omitempty"`
    To       string      `json:"to,omitempty"`       // comma-separated
    Cc       string      `json:"cc,omitempty"`
    Subject  string      `json:"subject,omitempty"`
    Body     string      `json:"body,omitempty"`
}
```

Recipient parsing splits To/Cc on `,` or `;` and trims whitespace.
Empty entries are dropped. **Recipient recovery** (carried over
from v1's saveDraftCmd): if the parsed To list is empty AND the
snapshot has a SourceID, look up the source message and use its
`FromAddress` as the implicit recipient — the user pressed Reply,
the original sender is the obvious target. Without a source
fallback, surface an actionable error (`"draft has no recipient
(set To: in the compose form)"`) and keep the form state in
m.compose so the user can correct + retry.

### 6.4 Outlook hand-off

After the action executor confirms the Graph round-trip, the response carries `webLink` — a URL that opens the draft in Outlook web (and via deep-link, Outlook desktop on macOS if installed).

The status bar shows: `Draft saved · 's' to open in Outlook · 'D' to discard`.

`s` runs `open <webLink>` (the macOS native handler). `D` enqueues a `discard_draft` action.

We don't poll the draft for changes after the user opens it in Outlook — the next delta sync of the Drafts folder pulls any edits the user made.

---

## 7. Local cache schema

The `messages` table already has an `is_draft INTEGER NOT NULL DEFAULT 0` column (spec 02). Drafts created in inkwell get `is_draft = 1` in the optimistic local insert, then the Graph round-trip reconciles the canonical message ID and replaces the temporary local UUID.

New table — JSON snapshot of in-flight compose form state, used
by the resume-on-startup path (PR 7-ii):

```sql
CREATE TABLE compose_sessions (
    session_id   TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,             -- 'new'|'reply'|'reply_all'|'forward'
    source_id    TEXT,                       -- message ID for reply/forward, NULL otherwise
    snapshot     TEXT NOT NULL,              -- JSON-encoded ComposeSnapshot
    created_at   INTEGER NOT NULL,
    confirmed_at INTEGER,                    -- set when user saves (Ctrl+S / Esc) or discards (Ctrl+D)
    FOREIGN KEY (source_id) REFERENCES messages(id) ON DELETE SET NULL
);
CREATE INDEX idx_compose_sessions_unconfirmed
  ON compose_sessions(created_at) WHERE confirmed_at IS NULL;
```

Why JSON snapshot vs the v1 tempfile path: in-modal compose
holds form state in memory (textinputs + textarea), not on disk.
The crash-recovery path serialises the form state via
`ComposeModel.Snapshot()` on every keystroke (or on a debounced
timer; PR 7-ii decides), persists into this table. On startup
the resume prompt reads the most recent unconfirmed row,
deserialises the snapshot, and `Restore`s into a fresh
`ComposeModel`.

Garbage collection: confirmed sessions older than 24h are deleted on next launch.

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

| Pane / mode    | Key             | Action                                     |
| -------------- | --------------- | ------------------------------------------ |
| Viewer         | `r`             | Reply to focused message → ComposeMode     |
| Viewer         | `R` (post-MVP)  | Reply-all → ComposeMode                    |
| Viewer         | `f` (post-MVP)  | Forward → ComposeMode                      |
| Anywhere normal | `m` (post-MVP) | New message → ComposeMode (blank)          |
| ComposeMode    | `Tab`           | Cycle field forward (Body → To → Cc → Subj) |
| ComposeMode    | `Shift+Tab`     | Cycle field backward                        |
| ComposeMode    | `Ctrl+S`        | Save draft → enqueue + dispatch + close    |
| ComposeMode    | `Esc`           | Save (alias for Ctrl+S — "I'm done" gesture) |
| ComposeMode    | `Ctrl+D`        | Discard draft (no Graph round-trip)        |
| ComposeMode    | `Ctrl+E` (post-MVP) | Drop the body slice into `$EDITOR`     |
| Status-bar hint | `s`            | Open most-recently-saved draft in Outlook  |

`r` already has a meaning in spec 07 (`mark_read`). The collision is resolved by pane scope: in the list pane, `r` = mark-read on the focused message; in the **viewer pane** `r` = reply. This mirrors mutt and is the precedent users will recognise.

For `m` (post-MVP): the existing keymap binds `m` to `move` in the list pane. We resolve by pane: `m` from the list pane is move; `m` from anywhere else (folders pane, viewer pane) is "new message".

**Why `Esc` saves and not cancels.** Standard modal convention is Esc-cancels, but in compose the user has just typed real content; silently throwing it away on Esc is the v1 mode the post-edit modal explicitly avoided. The redesign's principle: every exit from ComposeMode should be deliberate. `Ctrl+D` is the explicit discard; `Esc` matches the user's "I'm done" mental model from the v1 post-edit modal where Enter aliased to save.

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

**MVP (in-modal compose, reply only):**
- [x] `internal/ui/compose_model.go` ships `ComposeModel` backed by `bubbles/textinput` (To/Cc/Subject) + `bubbles/textarea` (body); pure-state unit tests cover construction, skeleton apply (incl. "Re:" dedup + empty-FromAddress passthrough), Tab cycle, Snapshot/Restore round-trip, and the footer-rendering invariant.
- [x] `internal/ui/messages.go` adds `ComposeMode`; `internal/ui/app.go` routes Update + View; `r` in the viewer enters compose with the reply skeleton pre-populated.
- [x] `Ctrl+S` / `Esc` dispatch `saveComposeCmd` (snapshot → `CreateDraftReply` action). `Ctrl+D` discards (no Graph round-trip). Tab cycles fields.
- [x] Recipient recovery: empty `To` falls back to source's `FromAddress`; absent that, surfaces an actionable error without dispatching.
- [x] `internal/compose/template.go::ReplySkeleton` reused for the body's quote chain (one skeleton formatter so the post-MVP `Ctrl+E` drop-out can hand the same string to `$EDITOR`).
- [x] Action executor (PR 7-i v0.13.x) handles `ActionCreateDraftReply` with two-stage idempotent dispatch (`createReply` → record draft_id → `PATCH body`). `Drain` skips this action type (createReply is non-idempotent).
- [x] On save, `webLink` is captured; status bar shows `✓ draft saved · press s to open in Outlook`.
- [x] No code path imports `internal/auth` for Mail.Send (compose only flows through the action queue).

**Post-MVP (deferred):**
- [ ] Reply-all (`R`), forward (`f`), new message (`m`) — adds `TypeCreateReplyAll` / `TypeCreateForward` / `TypeCreateDraft` action types and matching skeletons. PR 7-iii.
- [ ] `compose_sessions` migration + crash-recovery resume prompt that re-opens compose with the saved snapshot. PR 7-ii.
- [ ] `Ctrl+E` drop-out: opens `$EDITOR` with the body slice, returns to the in-modal form on exit.
- [ ] Discard flow deletes both local and server-side draft when the user cancels in compose AFTER the draft was saved (today: cancel before save = `Ctrl+D`, no server roundtrip; this DoD bullet covers the post-save discard path).
- [ ] Lint guard (CI script) fails any source line containing the literal string `Mail.Send` outside `docs/PRD.md` and `internal/auth/scopes.go`.

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
