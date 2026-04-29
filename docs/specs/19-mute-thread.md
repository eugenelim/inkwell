# Spec 19 — Mute thread

**Status:** Ready for implementation.
**Depends on:** Specs 02 (store + new `muted_conversations` table),
04 (TUI), 07 (action queue).
**Blocks:** Conversation-level operations (spec 20) — the
muting model assumes the thread-as-unit semantics formalised
there. Custom actions framework (§2) — the `set_thread_muted` op
primitive is exposed here.
**Estimated effort:** ½ day.

---

## 1. Goal

Silence a noisy thread without leaving it. Future replies still
arrive in the local cache and stay accessible; they just don't
surface as new in the list view. Reversible with the same key.

This is the "mute conversation" feature every modern mail client
ships (Gmail, Outlook web, Apple Mail). Mutt and aerc don't ship
it — but they don't have a strong notification model either, and
inkwell's "stuff that's actively unread" surface is more
analogous to Gmail's primary inbox than mutt's index.

## 2. Prior art

- **mutt / neomutt** — no built-in mute. Users wire patterns: a
  `~v <conversation>` in saved searches that kills a thread.
  Workable, awkward.
- **aerc** — has `:mute` against a notmuch backend; not a
  first-class IMAP feature.
- **alot (notmuch)** — `tag +muted` is the convention; the
  user's main view filters out muted threads.
- **Gmail** — `m` mutes, `Z` un-mutes (or it lifts on a
  reply mentioning you).
- **Outlook web** — "Ignore conversation" → moves to Deleted
  Items + auto-deletes future messages. We do NOT copy this —
  destructive default is wrong for our audience.

We follow Gmail's model: `M` (capital) toggles mute on the focused
conversation. Muted threads are still in the cache and accessible;
they just don't show in the default list view.

## 3. Schema

Migration `005_mute.sql`:

```sql
CREATE TABLE muted_conversations (
    conversation_id TEXT PRIMARY KEY,
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    muted_at        INTEGER NOT NULL  -- unix epoch
);
CREATE INDEX idx_muted_conversations_account ON muted_conversations(account_id);
```

Rationale for keying on `conversation_id` (not message-id): Graph's
`conversationId` is the thread-key the user is muting. New replies
arrive with the same ID and inherit the mute.

## 4. UI

### 4.1 Toggle key

| Key (any pane on a focused message) | Action                                         |
| ----------------------------------- | ---------------------------------------------- |
| `M`                                 | Toggle mute on the focused message's thread.   |

`M` is shifted-m (capital) so it doesn't collide with the
move-with-picker `m` (deferred from spec 07).

### 4.2 Indicator

Muted rows render with a leading `🔕` glyph (config:
`[ui].mute_indicator`, default `🔕`, ASCII fallback `m`). The
glyph sits in the same column as the calendar-invite indicator
(📅 from spec 04 iter 4); a row never carries both.

### 4.3 List filter

By default the list pane EXCLUDES muted-thread messages from
folders that aren't explicitly the muted view. Pressing `M` on
an already-muted thread un-mutes (the row reappears in the
default view).

Two ways to see muted threads:

1. **Per-thread**: when the user opens a message that happens to
   be muted (via `:filter` for example), the row is shown with
   the `🔕` indicator. Mute is silencing-without-hiding-from-
   search.
2. **Dedicated view**: a built-in saved-search "Muted Threads"
   pinned at the bottom of the sidebar (added automatically on
   first mute, never removed). Selecting it lists every muted
   thread for review / un-muting.

### 4.4 Status bar feedback

`M` produces an immediate status-bar toast: `🔕 muted thread "Q4
forecast"` or `🔔 unmuted thread "Q4 forecast"`. No confirmation
modal — toggling is reversible.

## 5. List query change

`store.ListMessages` gets a new filter: `excludeMuted bool`.
Default true for normal folder views. The filter joins against
`muted_conversations`:

```sql
WHERE conversation_id NOT IN (SELECT conversation_id FROM muted_conversations WHERE account_id = ?)
```

Search and `:filter` paths take a similar parameter; default to
include muted (search is intentional — if you searched for it,
you want it). The dedicated "Muted Threads" saved search inverts
the predicate.

## 6. Action queue integration

Two new action types per spec 07:

```go
const (
    ActionMuteThread   = "mute_thread"
    ActionUnmuteThread = "unmute_thread"
)
```

Both are local-only — Graph has no concept of muting. The action
queue still tracks them so:

- A failed local apply (rare; e.g. DB lock) gets retried.
- Custom actions (§2) can reference `mute_thread` as an op
  primitive that participates in undo.

Apply: insert / delete the `muted_conversations` row.
Inverse: the opposite action.

## 7. Definition of done

- [ ] Migration 005 lands; `muted_conversations` table created.
- [ ] action.Executor extends with `mute_thread` / `unmute_thread`.
- [ ] `store.ListMessages` honours the `excludeMuted` flag.
- [ ] `M` keybinding wired in list + viewer dispatch.
- [ ] Status-bar toast on toggle.
- [ ] `🔕` indicator in list pane (only on muted rows).
- [ ] Built-in "Muted Threads" saved search appears in sidebar
      after first mute.
- [ ] Tests: action apply + inverse round-trip; ListMessages
      excludes muted by default; `M` dispatches the right action.
- [ ] User docs: reference (`M` row), how-to ("Mute a thread you
      can't escape").

## 8. Cross-cutting checklist

- [ ] Scopes: none new.
- [ ] Store reads/writes: muted_conversations (CRUD).
- [ ] Graph endpoints: none.
- [ ] Offline: works fully offline.
- [ ] Undo: `M` toggles; previous `M` undoes itself.
- [ ] User errors: none expected.
- [ ] Tests: §7.
