# Spec 19 — Mute thread

**Status:** Ready for implementation.
**Depends on:** Spec 02 (store + new `muted_conversations` table),
Spec 04 (TUI), Spec 07 (action queue plumbing — undo stack only;
mute is local-only and does NOT route through the action queue).
**Blocks:** Spec 20 (conversation ops) — that spec's `T m` (move
thread) must not collide with `M` (mute); the key assignments below
are designed to be compatible.
**Estimated effort:** 1 day.

---

## 1. Goal

Silence a noisy thread without leaving it. Future replies still
arrive in the local cache and stay accessible; they just don't
surface as new in the list view. One keypress toggles; the same
keypress un-mutes. No confirmation — it is reversible in one
keystroke.

## 2. Prior art

### 2.1 Terminal clients

- **mutt / neomutt** — no built-in mute; users approximate it with
  `kill-thread` (marks all messages deleted) or pattern macros
  (`~v <conversation>` in saved searches). `kill-thread` is
  destructive (flags as deleted); mutt's pattern-based approach is
  the closest non-destructive equivalent but requires manual pattern
  bookmarking.
- **aerc** — `:mute` command available against notmuch backends;
  toggles a `muted` tag. Not a first-class IMAP concept. The muted
  filter is applied globally at the query level. Simple toggle UX.
- **alot (notmuch)** — fundamentally tag-based: `tag +muted` is the
  convention; the primary view query always appends `not tag:muted`.
  The thread is the first-class unit, making muting natural.
- **neomutt** — `<kill-thread>` (Ctrl-D by default) soft-deletes
  all messages in the thread. Destructive. Wrong model.

### 2.2 Web / desktop clients

- **Gmail** — the canonical model this spec follows. `m` mutes; the
  conversation is archived from Inbox and future replies auto-archive
  too (via a server-side label rule). Re-surfaces if a reply
  addresses you directly in To/Cc. Accessible via All Mail and
  search. Status text "Muted" shown in the conversation list.
- **Fastmail** — "Mute conversation": new replies go to archive
  rather than inbox; still accessible via search and archive view.
  Un-mute restores the thread to inbox on next reply.
- **Superhuman** — `Shift+M` mutes. Removes from Inbox. Future
  messages auto-archive. Re-surfaces on direct address.
- **HEY** — "Set Aside" (snooze-like, not permanent muting).
  "Screener" for new senders — different concept.
- **Outlook desktop / web** — "Ignore Conversation": moves all
  current and future messages to Deleted Items permanently via a
  server-side rule. **We do NOT copy this model** — destructive
  default is wrong for power users who may need the thread later.
- **Thunderbird** — "Ignore Thread" (`Del` key): marks as read and
  moves thread to Trash. Also destructive.
- **Apple Mail** — notifications silenced per thread; messages stay
  visible but dimmed. Does not hide from list view.

### 2.3 Design decision

inkwell follows Gmail / Fastmail / aerc:
- **Non-destructive**: muted threads stay in the cache in their
  original folder; they are simply excluded from the default list
  view.
- **Toggle, not modal**: `M` (capital) toggles mute. Same key
  un-mutes. No confirmation needed.
- **Local-only**: mute state is a local DB record. Graph has no
  muting API. No Graph call is made on mute/unmute.
- **Mute is absolute for v1**: unlike Gmail, muted threads in inkwell
  do NOT re-surface when a new reply addresses you directly in To/Cc.
  This simplifies the implementation significantly. The rationale:
  inkwell stores `toRecipients` in the messages table but there is no
  per-sync "did the signed-in user appear in this reply's To/Cc?" hook
  in the sync engine. Re-surfacing can be added in a follow-up spec
  once the sync notification model is established.
- **Search includes muted**: intentional search (`/` and `inkwell search`)
  always includes muted threads — if you searched for it, you want
  to see it.

## 3. Schema

Migration **`009_mute.sql`** (migrations 001–008 are already applied):

```sql
CREATE TABLE muted_conversations (
    conversation_id TEXT    NOT NULL,
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    muted_at        INTEGER NOT NULL,   -- unix epoch seconds
    PRIMARY KEY (conversation_id, account_id)
);
CREATE INDEX idx_muted_conv_account ON muted_conversations(account_id);
```

**Design rationale:**

- **Composite PK `(conversation_id, account_id)`**: Graph
  `conversationId` values are tenant-local but not globally unique
  across accounts. Every other table keys on `(account_id, ...)`;
  this table follows the same convention. A single-column
  `conversation_id` PK would silently suppress display of
  identically-named threads in a future second account.
- **No `message_id` column**: mute is a per-conversation state, not
  per-message. New replies inherit the mute automatically because
  they carry the same `conversation_id`.
- **`muted_at`**: retained for the "Muted Threads" view (sorted by
  mute date, newest first).

## 4. Store API

### 4.1 New store methods

```go
// MuteConversation inserts a muted_conversations row. Idempotent.
MuteConversation(ctx context.Context, accountID int64, conversationID string) error

// UnmuteConversation removes the muted_conversations row.
// No-op if not muted (idempotent per CLAUDE.md §3 invariant).
UnmuteConversation(ctx context.Context, accountID int64, conversationID string) error

// IsConversationMuted returns true if the conversation is muted
// for the given account.
IsConversationMuted(ctx context.Context, accountID int64, conversationID string) (bool, error)

// ListMutedMessages returns all messages whose conversation_id
// is in muted_conversations for the given account, ordered by
// muted_at DESC, then received_at DESC within each conversation.
// Used by the "Muted Threads" virtual folder view.
ListMutedMessages(ctx context.Context, accountID int64, limit int) ([]Message, error)
```

### 4.2 Change to `MessageQuery` / `ListMessages`

Add one field to `store.MessageQuery`:

```go
type MessageQuery struct {
    // ... existing fields unchanged ...

    // ExcludeMuted, when true, suppresses messages whose
    // conversation_id appears in muted_conversations for the
    // query's AccountID. Default false preserves existing behaviour.
    ExcludeMuted bool
}
```

The SQL clause added when `ExcludeMuted = true`:

```sql
-- Anti-join via NOT EXISTS to handle NULL conversation_id safely.
-- Messages with a NULL or empty conversation_id are NEVER suppressed
-- (they cannot be in muted_conversations, which requires a non-empty
-- conversation_id).
AND (
    m.conversation_id IS NULL
    OR m.conversation_id = ''
    OR NOT EXISTS (
        SELECT 1
        FROM muted_conversations mc
        WHERE mc.conversation_id = m.conversation_id
          AND mc.account_id = :account_id
    )
)
```

**Why NOT EXISTS, not NOT IN:** `NOT IN` with a subquery returns
UNKNOWN (hence excluded) for any row where the outer value is NULL.
Messages with `conversation_id IS NULL` would silently vanish from
the list view. `NOT EXISTS` evaluates per-row without the NULL hazard.

**Bind-parameter note:** the subquery uses the named parameter
`:account_id` (SQLite named form) rather than a positional `?` to
avoid an off-by-one error when `buildListSQL` assembles its args
slice. The outer query already binds `account_id` positionally; a
second positional `?` in the subquery would require knowing its exact
ordinal in the final args slice, which is fragile as other WHERE
clauses are added. Named parameters (`?` → `:account_id`) are
supported by `modernc.org/sqlite` and decouple the subquery from
position. Alternatively: pass `accountID` as an extra trailing
argument appended after the existing args, and use `?` in the
subquery — either approach must be explicit in the implementation.

### 4.3 FTS5 search path

`store.Search` (the `SearchQuery` / FTS5 path) does **not** add an
`ExcludeMuted` field. Rationale: the hybrid searcher (spec 06) is
always intentional — the user explicitly typed a query. Muted threads
appearing in search results is the correct behaviour. No change to
`SearchQuery` or `Search`.

### 4.4 Pattern filter (`:filter`) path

`store.SearchByPredicate` is used by the pattern engine (spec 08/09).
`:filter` results in the list pane: by default the outer `ListMessages`
call that loads the list already has `ExcludeMuted: true` for normal
folder views. The pattern filter narrows the already-filtered list in
the TUI, so muted messages are excluded automatically without touching
`SearchByPredicate`. The CLI `inkwell filter` command should pass
`ExcludeMuted: false` (same reasoning as search: explicit filter is
intentional).

## 5. UI

### 5.1 Toggle key

| Key (list pane or viewer pane focused) | Action                            |
| --------------------------------------- | --------------------------------- |
| `M`                                     | Toggle mute on the focused message's thread. |

`M` (Shift+m) is chosen because:
- `m` (lowercase) is `Move` — taken.
- `M` is currently unused in `DefaultKeyMap`.
- Capital letters are the convention for "this affects more than the
  single message" (following `D` for permanent-delete, `N/R/X` in
  the folders pane for spec 18).
- Spec 20's `T`-chord uses `T m` for "move thread" — `T m` ≠ `M`.

**Pane scope:** `M` is active only in the list pane and viewer pane.
It is NOT active in the folder sidebar pane.

**KeyMap changes:**
- Add `MuteThread key.Binding` to `internal/ui/keys.go KeyMap` struct.
- Add `MuteThread string` to `internal/ui/keys.go BindingOverrides` struct.
- Wire through `ApplyBindingOverrides`.
- Wire through `findDuplicateBinding` scan.
- Default binding: `key.NewBinding(key.WithKeys("M"))`.

### 5.2 List-row indicator

Muted rows render a leading indicator glyph. Config:
`[ui].mute_indicator`, default `🔕`, ASCII fallback `m` (for
terminals that cannot render the emoji). The glyph occupies the
"flags" slot in the row — the same column used for the calendar
glyph `📅` (spec 12) and the flag `⚑`. A row never carries both
mute + calendar indicators simultaneously (calendar indicator takes
priority in event-invite messages).

### 5.3 Default list filter behaviour

| Context                                 | ExcludeMuted |
| --------------------------------------- | ------------ |
| Normal folder view (list pane)          | `true`       |
| "Muted Threads" virtual folder view     | `false` (query is `ListMutedMessages`) |
| FTS5 / hybrid search results            | `false` (intentional search) |
| CLI `inkwell messages` / `inkwell filter` | `false` (intentional query) |

### 5.4 "Muted Threads" virtual folder

A **hardcoded virtual folder entry** is always present in the folder
sidebar, positioned after the user's subscribed folders (below
saved-search virtual folders). It does NOT use the `saved_searches`
table. Reasons:
- The pattern language (spec 08) has no `is:muted` predicate; a
  `saved_searches` row would require adding one.
- Users could `X`-delete (spec 18) a `saved_searches` row, but the
  spec says "never removed" — the hardcoded approach enforces this.

**Sentinel folder ID:** `__muted__` (double-underscore prefix to
avoid collision with Graph folder IDs, which are base64url strings).

**Sidebar rendering:** shown in the sidebar only when the
`muted_conversations` table has ≥1 row for the signed-in account.
Hidden when no conversations are muted. Selecting it loads
`ListMutedMessages` into the list pane, sorted by `muted_at DESC`
then `received_at DESC` within each conversation group.

**Count display:** shows the count of distinct muted conversations
(not individual message count), e.g. `🔕 Muted (3)`.

### 5.5 Status-bar feedback

| Action    | Toast                                          |
| --------- | ---------------------------------------------- |
| Mute      | `🔕 muted thread (subject: Q4 forecast)`       |
| Unmute    | `🔔 unmuted thread (subject: Q4 forecast)`     |
| No convo  | `mute: message has no conversation ID`         |
| DB error  | `mute failed: <error>`                         |

The subject in the toast is the `subject` field of the focused
message. Per ARCH §12 / CLAUDE.md §7 rule 3, subject lines must
**not** appear in log output outside DEBUG level. The toast is
terminal UI only and is not logged.

### 5.6 Unread badge count behaviour

The folder sidebar unread count (`folders.unread_count`) is sourced
directly from Graph delta sync — it is Graph's authoritative count
and includes messages from muted threads. inkwell does **not** adjust
this count locally on mute/unmute. Rationale:
- A local adjustment would require a per-mute JOIN across all folders,
  adding latency to every mute operation.
- The inconsistency ("Inbox (12)" but only 8 visible rows) is
  acceptable for v1; it matches the trade-off Apple Mail makes.
- A future iteration can compute an `effectiveUnreadCount` by
  subtracting muted-thread unread counts, if users find the
  discrepancy confusing.

**Spec note:** implementers should add a comment near the sidebar
unread count render that says "includes muted thread messages; see
spec 19 §5.6 for the adjustment TODO."

## 6. Mute / Unmute flow (local-only, NOT via action queue)

Muting does NOT use the spec 07 `action.Executor` or the `actions`
table queue. Rationale: the action queue's `dispatch()` switch
dispatches to Graph. Mute has no Graph call. Routing through the
queue would require a `default` no-op branch that misleads future
readers into thinking a Graph call is made.

Instead, the UI dispatches directly:

```go
// TUI path (list pane / viewer pane dispatchList / dispatchViewer):
// ctx is passed explicitly — Bubble Tea Cmd goroutines must not
// capture a context from the Update call (Update is synchronous;
// there is no ambient ctx). Use context.Background() or a
// cancellable context threaded from the Model via a cancel field.
func muteCmd(ctx context.Context, st store.Store, accountID int64, conversationID, subject string) tea.Cmd {
    return func() tea.Msg {
        muted, err := st.IsConversationMuted(ctx, accountID, conversationID)
        if err != nil {
            return errMsg{err}
        }
        if muted {
            if err := st.UnmuteConversation(ctx, accountID, conversationID); err != nil {
                return errMsg{err}
            }
            return mutedToastMsg{subject: subject, nowMuted: false}
        }
        if err := st.MuteConversation(ctx, accountID, conversationID); err != nil {
            return errMsg{err}
        }
        return mutedToastMsg{subject: subject, nowMuted: true}
    }
}
```

After `mutedToastMsg` is received in `Update`, the list pane reloads
the current folder (same as after archive / move — one `ListMessages`
call with `ExcludeMuted: true`).

**Undo model:** `M` toggles in place. The spec 07 `u`-key undo stack
is **not** involved. The toast is the only feedback; the user presses
`M` again to reverse. Rationale: undo-stack integration would require
storing a `MuteUndoEntry` and teaching the undo runner to call a
store method rather than a Graph call. Since the toggle is instant and
lossless (the message is never moved or deleted), the extra stack
plumbing adds no user-facing value.

## 7. CLI

```sh
inkwell mute <conversation-id>       # mute by conversation ID
inkwell mute --message <message-id>  # resolve to conversation_id via local store
inkwell unmute <conversation-id>
inkwell unmute --message <message-id>
```

Both commands load the local store (no Graph call), apply the mute,
and print:

| Command | Text output | JSON output (`--output json`) |
| ------- | ----------- | ----------------------------- |
| `mute` | `✓ muted conversation <id>` | `{"muted": true, "conversation_id": "..."}` |
| `unmute` | `✓ unmuted conversation <id>` | `{"muted": false, "conversation_id": "..."}` |

`--message <id>` resolves the message's `conversation_id` from the
local store and delegates. Error if the message has no
`conversation_id`.

The commands live in `cmd/inkwell/cmd_mute.go` and are registered in
`cmd_root.go`.

## 8. Performance budgets

| Surface | Budget | Benchmark |
| --- | --- | --- |
| `ListMessages(folder, limit=100, ExcludeMuted=true)` over 100k msgs + 500 muted conversations | ≤10ms p95 | `BenchmarkListMessagesExcludeMuted` in `internal/store/` |
| `MuteConversation` / `UnmuteConversation` | ≤1ms p95 | `BenchmarkMuteUnmute` |

The `NOT EXISTS` subquery must use the `idx_muted_conv_account` index
on `(account_id)` within the anti-join. Verify with `EXPLAIN QUERY PLAN`
in the benchmark setup.

## 9. Definition of done

- [ ] Migration `009_mute.sql` created; `muted_conversations` table
      with composite PK `(conversation_id, account_id)` + index.
- [ ] `store.Store` interface gains `MuteConversation`,
      `UnmuteConversation`, `IsConversationMuted`,
      `ListMutedMessages`.
- [ ] `MessageQuery.ExcludeMuted bool` added; `buildListSQL` emits the
      `NOT EXISTS` anti-join when true; normal folder views pass
      `ExcludeMuted: true`.
- [ ] `KeyMap` gains `MuteThread key.Binding`; `BindingOverrides` gains
      `MuteThread string`; `ApplyBindingOverrides` wires it;
      `findDuplicateBinding` includes it; default `M`.
- [ ] `M` wired in `dispatchList` and `dispatchViewer`; dispatches
      `muteCmd` Cmd; on `mutedToastMsg` reloads list + shows status
      toast.
- [ ] `🔕` indicator in list-pane row for muted messages.
- [ ] "Muted Threads" virtual sidebar entry (sentinel ID `__muted__`);
      visible only when ≥1 muted conversation exists; selecting it
      calls `ListMutedMessages`; count shows distinct muted-conversation
      count.
- [ ] `[ui].mute_indicator` config key documented in `docs/CONFIG.md`
      (default `🔕`, ASCII fallback `m`).
- [ ] CLI: `cmd/inkwell/cmd_mute.go` implementing `inkwell mute` and
      `inkwell unmute` with `--message` resolver; registered in
      `cmd_root.go`.
- [ ] Tests:
      - store: `TestMuteConversationIdempotent`,
        `TestUnmuteConversationNoop`, `TestListMessagesExcludesMuted`,
        `TestListMessagesNullConvIDNotFiltered` (NULL safety regression),
        `TestListMutedMessages`.
      - UI dispatch: `TestMuteKeyMutesThread` (M on muted=false → muted),
        `TestMuteKeyUnmutesThread` (M on muted=true → unmuted),
        `TestMuteKeyNoConvIDShowsError` (message without conversation_id).
      - CLI: `TestMuteCLIByConversationID`, `TestMuteCLIByMessageID`.
      - Benchmarks: `BenchmarkListMessagesExcludeMuted` (100k msgs +
        500 muted conversations ≤10ms p95); `BenchmarkMuteUnmute`
        (single mute/unmute cycle ≤1ms p95).
- [ ] User docs: `docs/user/reference.md` adds `M` row to list-pane
      keybindings table; `docs/user/how-to.md` adds "Mute a noisy
      thread" recipe.

## 10. Cross-cutting checklist

- [ ] Scopes: none new (`Mail.ReadWrite` already in PRD §3.1; mute is
      local-only and makes no Graph calls).
- [ ] Store reads/writes: `muted_conversations` (INSERT + DELETE +
      SELECT); `messages` read-only (ListMessages, ListMutedMessages).
      FK cascade on account delete handles cleanup automatically.
- [ ] Graph endpoints: none.
- [ ] Offline: works fully offline. The muted state is local; sync
      does not overwrite it.
- [ ] Undo: toggle (M again) is the undo. Does NOT push to spec 07
      undo stack. `u` does not un-mute.
- [ ] User errors: message with no `conversation_id` (spec 19 §5.5);
      DB write failure surfaces as a status-bar error toast.
- [ ] Latency budget: ≤10ms p95 for `ListMessages` with mute filter
      over 100k msgs (§8 benchmark). ≤1ms for mute/unmute store writes.
- [ ] Logs: `MuteConversation` / `UnmuteConversation` log at DEBUG
      level with `conversation_id` only — never log subject or any
      message content. Toast subject display is UI-only, not logged.
      Redaction constraint: existing `internal/log/redact.go` does not
      strip `conversation_id` (it's not PII), but MUST NOT log subject.
- [ ] CLI mode: `inkwell mute` / `inkwell unmute` per §7.
- [ ] Tests: §9 test list.
- [ ] **Spec 17 review:** mute adds a new store path (local CRUD) and
      a new log site (conversation_id at DEBUG). No new external HTTP,
      no token handling, no subprocess, no cryptographic primitive.
      Threat-model row: local mute data persists across sign-out;
      signing out and signing back in with a different account could
      expose muted-conversation metadata to the new session if the DB
      path is the same. Mitigation: account FK cascade deletes rows on
      account delete (sign-out + purge covers this). Privacy doc impact:
      `muted_conversations` table added to the "what data inkwell stores
      locally" section.
- [ ] **Docs consistency sweep:** CONFIG.md updated for
      `[ui].mute_indicator`; reference.md updated for `M` keybinding;
      how-to.md updated for mute recipe.
