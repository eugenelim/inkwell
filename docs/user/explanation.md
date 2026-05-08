# Explanation — design decisions

The "why" behind inkwell's behaviour. Read this when you want
context, not when you want to do something specific (for that, see
[`how-to.md`](how-to.md)) or look something up (for that, see
[`reference.md`](reference.md)).

---

## Why no "send"?

inkwell never sends mail. The TUI authors **drafts**; you finish in
Outlook with one click. That's a deliberate hard limit, not an
oversight.

The reason is the OAuth scope: `Mail.Send` is **denied** in inkwell's
scope set. We don't request it; CI lints fail any code that tries.

Why deny it? Because sending email programmatically is the highest
trust gate in a corporate mailbox. A bug in inkwell that misroutes
a draft is a confused user and a deletable file. A bug that silently
**sends** is a career event for the user (think "Reply All to the
all-staff DL"). The cost-benefit doesn't justify shipping the
feature when Outlook is right there.

You also keep your existing Outlook signature, server-side rules,
and out-of-office state without inkwell having to re-implement them.

## Why local-first?

Every read operation hits the local SQLite cache. Every write goes
to a queue. The Microsoft Graph round-trip happens in the
background.

The user-visible win: **<100ms feel for everything**. Folder switch,
message open, search, filter — none of these wait on a network call.
Outlook for Mac waits on the server for most of these and it
shows. The architectural cost is one engine that has to keep the
local cache consistent with the server.

The privacy win: nothing leaves your home directory. The cache is
in `~/Library/Application Support/inkwell/inkwell.db` (mode 0600).
The logs are in `~/Library/Logs/inkwell/` (no message bodies, no
tokens — bearer tokens are scrubbed at the slog handler).

The offline win: read everything you've ever cached. Search works.
Triage actions queue and replay on reconnect.

## Why a triage queue and not direct Graph calls?

When you press `d` on a message, three things happen in order:

1. **Optimistic local apply.** The row's `folder_id` flips to your
   real Deleted Items folder ID. The TUI re-renders. The message
   disappears from the inbox view. (~10ms.)
2. **Action enqueued.** A row goes into `actions` with status
   `pending`. The action queue is the only path for writes (an
   architectural rule); direct PATCH/POST against Graph from
   anywhere else would be a bug.
3. **Graph dispatch.** `POST /me/messages/{id}/move` with
   `{"destinationId":"deleteditems"}`. Same endpoint Outlook web/
   desktop use.

If Graph rejects the move (auth, throttle, network), the local
change rolls back automatically — your view stays consistent with
the server. The action's status flips to `failed`; the engine drain
retries on transient failures.

This pattern is the conceptual core. Bulk operations (`;d` / `;a`)
work the same way at $batch granularity (20 sub-requests per call,
each rolled back individually on per-sub-response failure).

## Why optimistic UI matters

When you press `;d` on 247 messages, the local store mutates 247
rows in one transaction (~50ms). The list re-renders. The cmd-bar
shows `✓ soft_delete 247` even though Graph hasn't responded yet.
*Then* the $batch fires.

The user perceives this as **instant**. Outlook's "delete 247"
freezes the UI for ~2 seconds and holds you hostage to the server
round-trip. inkwell decouples the two: your hands move at TUI
speed; the server catches up asynchronously.

Cost: when Graph fails, the rollback is **visible**. Messages
"come back". This is jarring on purpose so you can react.

## Why the pattern language?

Mutt's pattern language is the closest thing the email world has to
a query language users actually type. `~f` for from, `~s` for
subject, `~d` for date, boolean composition with `& | !`. Five
minutes to learn, five years to outgrow.

Outlook's search syntax exists but is incomplete and only runs
server-side. Mutt's runs locally over IMAP.

inkwell takes the mutt syntax and lets it run against three
back-ends:

- **Local SQL** — over the SQLite cache. Fast, offline.
- **Graph $filter** — server-side OData filter (deferred to v0.9).
- **Graph $search** — server-side full-text (deferred to v0.9).

The compiler picks the right back-end for each predicate. v0.8.0
ships with local SQL only; the cross-backend chooser lands when
the bulk-ops UX (spec 10) needs it.

## Why are there saved searches AND a filter command?

`:filter <pattern>` is **transient**. You type it now to do a
specific clean-up. After you act, it clears.

Saved searches are **persistent**. You define them once in
config; they appear in the sidebar. You select them when you want
to look at the same slice over and over (e.g. "Needs Reply" — open
it, triage, close).

They share the same evaluator. The semantics are identical. The
distinction is lifetime.

## Why does `;d` ask for confirmation but `d` doesn't?

Single-message `d` is recoverable — one undo (when undo lands), or
just go to Deleted Items and move it back. The friction of a confirm
modal would dominate the friction of the action.

Bulk `;d` is closer to "rm -rf". A confirmation modal with a
default-No answer is the right safety net. CLAUDE.md §7 #9 mandates
this for any bulk or `D`-style permanent operation.

## Why these docs are split four ways

Different questions deserve different answers. "How do I get
started?" needs a sequential walkthrough. "How do I delete all
newsletters?" needs a recipe. "What does `~r` do?" needs a row in
a table. "Why drafts only?" needs a paragraph.

A single guide mixed all four. Splitting into tutorial / how-to /
reference / explanation lets each file optimise for its question
type, and lets you ignore the three you don't currently need.

## Why a status-bar reminder for active filters / searches?

Modal state is cheap to forget. If you've filtered to 47
newsletters and walked away for coffee, you come back to a list
that *looks* like an inbox but isn't — and `;d` on it would delete
those 47, not the inbox.

The cmd-bar reminder

```
filter: ~f newsletter@* · matched 47 · ;d delete · ;a archive · :unfilter
```

is constantly visible while filtered. It also tells you the bulk
chord, so the keys you'd want next are right there.

## Why the calendar is read-only

`Calendars.Read` is granted; `Calendars.ReadWrite` is denied.
Modifying a calendar — accept, decline, propose new time, create —
is the same problem as `Mail.Send`: high-trust, easy to get wrong,
recoverable only via Outlook anyway. We render the daily view as
context, not as a tool. To act on an event, finish in Outlook.

## Why two stacks (Reply Later / Set Aside)

Inkwell ships two adjacent verbs that the native Outlook clients
handle poorly: **Reply Later** ("I owe this person a reply, but
not now") and **Set Aside** ("I want to keep this handy without
committing to a reply"). They're separate by design: the *verbs*
are different. Conflating them into a generic "later" pile loses
the asymmetry between commitment-to-write and reference-shelf
(HEY's design call; we follow it).

The implementation maps each stack onto a reserved Microsoft
Graph category — `Inkwell/ReplyLater` and `Inkwell/SetAside` —
chosen for three reasons:

1. **State syncs across devices.** A message moved into Reply
   Later on your laptop appears in Reply Later on your phone the
   next time inkwell pulls a delta sync. No new schema, no new
   server-side primitive.
2. **No new write surface.** Categories already round-trip via
   the existing `add_category` / `remove_category` action queue
   path (spec 07). Stacks reuse undo (`u`), the action queue's
   optimistic-apply, and the `~G` pattern operator.
3. **Visible in Outlook web.** You'll see `Inkwell/ReplyLater`
   when you open the message in Outlook web. That's intentional
   — the category is the storage primitive, and any client that
   tags or untags it participates in the same queue. The
   slash-prefixed namespace keeps inkwell-managed categories
   visually distinct from your own.

The behavioural metadata exposure is acknowledged: anyone with
delegated mailbox access (executive assistants, compliance
reviewers) can see which messages you've Reply-Later'd. If that
matters in your environment, the `~G` pattern operator gives you
the same workflow without the namespace prefix — set up a saved
search instead.

---

_Last reviewed against v0.8.0._
