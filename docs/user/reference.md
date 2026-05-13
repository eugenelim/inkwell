# Reference

Every keybinding, every command, every pattern operator. Quick
lookup. For narrative, see [`tutorial.md`](tutorial.md). For task
recipes, see [`how-to.md`](how-to.md).

---

## Pane focus & navigation (any pane)

| Key            | Action                                |
| -------------- | ------------------------------------- |
| `1`            | Focus folders pane                    |
| `2`            | Focus messages pane                   |
| `3`            | Focus viewer pane                     |
| `Tab`          | Cycle to next pane                    |
| `Shift+Tab`    | Cycle to previous pane                |
| `q`            | Quit                                  |
| `Ctrl+C`       | Quit                                  |
| `Ctrl+R`       | Force a sync cycle now                |
| `:`            | Open command bar                      |
| `/`            | Search (FTS over local cache)         |
| `?`            | Open the help overlay (every binding) |
| `Ctrl+K`       | Open the command palette (fuzzy-search every action, folder, saved search) |
| `Esc`          | Cancel mode / clear search            |

## Folders pane (when focused)

| Key       | Action                                     |
| --------- | ------------------------------------------ |
| `j` / `↓` | Cursor down (skips section headers)        |
| `k` / `↑` | Cursor up                                  |
| `o`       | Toggle expand/collapse on the focused folder |
| `Space`   | Same as `o`                                |
| `Enter`   | Open folder (loads messages, focus → list) |
| `l` / `→` | Same as `Enter`                            |
| `N`       | Create a new folder under the focused one (empty selection → top-level) |
| `R`       | Rename the focused folder (well-known folders refused server-side) |
| `X`       | Delete the focused folder (with confirm; cascades to Deleted Items server-side) |

The full folder hierarchy syncs from Microsoft Graph — Inbox
sub-folders, sub-sub-folders, any depth. The tree renders with
two-space indentation per level; `▾` / `▸` glyphs mark expanded /
collapsed parents. Inbox is auto-expanded on first launch.

Saved searches (configured in `[[saved_searches]]` or via `:rule save`) show
under a "Saved Searches" section with a `☆` glyph. Enter on one runs its
pattern via the filter machinery. `e` opens the edit modal for the focused
saved search.

**Streams section** (spec 23): four routing destinations appear below Saved Searches:

| Entry           | Glyph | Shows messages routed to…              |
| --------------- | ----- | -------------------------------------- |
| Imbox           | 📥    | your primary stream                    |
| Feed            | 📰    | newsletters, digests, subscriptions    |
| Paper Trail     | 🧾    | receipts, confirmations, transactional |
| Screener        | 🚪    | unrecognised senders                   |

Enter on a stream entry loads messages from that routing destination.

**Stacks section** (spec 25): Reply Later and Set Aside entries appear below Streams, showing their current message count. Enter on one loads the stack.

| Entry        | Glyph | Meaning                                      |
| ------------ | ----- | -------------------------------------------- |
| Reply Later  | ↩     | Messages you have flagged to reply to later  |
| Set Aside    | 📌    | Messages pinned for reference                |

| Key       | Action (Saved Searches section)                     |
| --------- | --------------------------------------------------- |
| `Enter` / `l` | Run the saved search (loads matches, focus → list) |
| `e`       | Open edit modal (rename, change pattern or pinned) |

## Messages pane (when focused)

| Key       | Action                                                        |
| --------- | ------------------------------------------------------------- |
| `j` / `↓` | Cursor down (auto-paginates near the bottom)                  |
| `k` / `↑` | Cursor up                                                     |
| `PgDn` / `Ctrl+D` | Jump cursor 20 rows down (kicks Backfill at the wall) |
| `PgUp` / `Ctrl+U` | Jump cursor 20 rows up                                |
| `Home` / `g`      | Jump to first message                                  |
| `End` / `G`       | Jump to last loaded message (kicks Backfill at the wall) |
| `Enter`   | Open message in viewer (focus → viewer)                       |
| `r`       | Mark read                                                     |
| `R`       | Mark unread                                                   |
| `f`       | Toggle flag                                                   |
| `d`       | Soft-delete (move to Deleted Items)                           |
| `D`       | Permanent delete (with confirm; bypasses Deleted Items; **NOT undoable**) |
| `a`       | Archive (move to Archive folder; also `e` — spec 30)          |
| `e`       | Archive (alias of `a`; mirrors Gmail/Inbox convention)        |
| `m`       | Move to a folder (opens picker; type to filter; Enter selects) |
| `c`       | Add category (prompts for the name)                           |
| `C`       | Remove category (prompts for the name)                        |
| `;`       | Begin bulk chord (only when a filter is active)               |
| `;d`      | Bulk delete the filtered set (with confirm)                   |
| `;a`      | Bulk archive the filtered set (with confirm; also `;e`)       |
| `;e`      | Bulk archive the filtered set (alias of `;a`)                 |
| `;m`      | Bulk move the filtered set to a folder (opens folder picker)  |
| `U`       | Unsubscribe (RFC 8058 / mailto / browser; with confirm)       |
| `M`       | Toggle mute on the focused message's conversation thread      |
| `T`       | Begin thread chord — acts on the whole conversation (see below) |
| `T r`     | Mark whole thread read                                        |
| `T R`     | Mark whole thread unread                                      |
| `T f`     | Flag every message in the thread                              |
| `T F`     | Unflag every message in the thread                            |
| `T d`     | Soft-delete the entire thread (confirm, default N)            |
| `T D`     | Permanently delete the entire thread (confirm, irreversible)  |
| `T a`     | Archive the entire thread (also `T e`)                        |
| `T e`     | Archive the entire thread (alias of `T a`)                    |
| `T m`     | Move whole thread (opens folder picker)                       |
| `S`       | Begin stream chord — route the focused message's sender (see below) |
| `S i`     | Route sender to Imbox                                         |
| `S f`     | Route sender to Feed                                          |
| `S p`     | Route sender to Paper Trail                                   |
| `S s`     | Route sender to Screener                                      |
| `S c`     | Clear routing for sender (back to unclassified)               |
| `Y`       | Spec 28 §5.4 — admit the focused sender to Imbox. Pane-scoped: only fires inside the Screener virtual folder while `[screener].enabled` is true. Equivalent to `S i`. |
| `N`       | Spec 28 §5.4 — screen out the focused sender. Pane-scoped: only fires inside the Screener virtual folder while `[screener].enabled` is true. Equivalent to `S k`. |
| `L`       | Toggle Reply Later — add/remove focused message from the Reply Later stack |
| `P`       | Toggle Set Aside — add/remove focused message from the Set Aside stack |
| `B`       | Toggle bundle designation on the focused sender (spec 26). On a bundle header, `B` un-designates the sender and dissolves the bundle in place. |
| `Space`   | Expand/collapse the focused bundle header. On a bundle member, collapses the parent and lands the cursor on the header. No-op on flat rows. |
| `[`       | Previous tab (when tabs are configured). Also cycles the Inbox sub-strip when no spec-24 tabs are configured AND `[inbox].split = "focused_other"` AND the Inbox folder is selected (spec 31 §5.5). |
| `]`       | Next tab (when tabs are configured). Same fallback to the Inbox sub-strip as `[`.                                                                                                          |
| `;l`      | Bulk add filtered set to Reply Later                          |
| `;L`      | Bulk remove filtered set from Reply Later                     |
| `;s`      | Bulk add filtered set to Set Aside                            |
| `;S`      | Bulk remove filtered set from Set Aside                       |
| `u`       | Undo the most recent triage action (mark, flag, delete, archive) |
| `/`       | Enter search mode                                             |

**Smart-scroll**: when you reach the last 20 messages of the loaded
slice, the next page (200 rows) loads from the local store
automatically. When the cache is exhausted, inkwell kicks a sync to
pull more from Graph.

**Calendar-invite indicator**: messages get a leading `📅` glyph
when ANY of these signal a meeting:

- Subject prefix: `Accepted:`, `Declined:`, `Tentative:`,
  `Updated:`, `Canceled:`, `Meeting:`, `Invitation:`,
  `Forwarded Invitation:`, `New Time Proposed:`. These catch
  *responses* and *cancellations*.
- Body preview shape: Outlook auto-generates a `When: <date>`
  / `Where: <location>` header block on the body of every
  invite it sends. Detecting both labels in the first ~200
  chars catches *meeting requests* whose subject is just the
  meeting title with no prefix.

Limitation: heuristics are English-only. Non-English Outlook
deployments emit localised `When:` / `Where:` labels and won't
match. A future release will use the Graph type-cast `$select`
to make detection locale-independent — that path is currently
disabled because of a real-tenant 400 regression on the bare
`$select=meetingMessageType` form.

## Viewer pane (when focused)

| Key       | Action                                                        |
| --------- | ------------------------------------------------------------- |
| `j` / `↓` | Scroll body down                                              |
| `k` / `↑` | Scroll body up                                                |
| `PgDn` / `Ctrl+D` | Scroll body down ~10 lines                            |
| `PgUp` / `Ctrl+U` | Scroll body up ~10 lines                              |
| `Home` / `g`      | Jump to top of body                                    |
| `End` / `G`       | Jump to bottom of body                                 |
| `h` / `←` | Back to messages pane                                         |
| `H`       | Toggle compact / full headers (To/Cc/Bcc expansion)           |
| `r`       | Reply — opens the in-modal compose pane with the reply skeleton |
| `R`       | Reply All — opens compose pre-filled with src.From + remaining recipients (deduped against your UPN) |
| `f`       | Forward — opens compose with the canonical "Forwarded message" header block |
| `m`       | New message — opens compose with a blank form (focuses To)    |
| `s`       | Open the most-recently-saved draft in Outlook (after `r`/`R`/`f`/`m` saves) |
| `D`       | **If a draft was just saved:** discard that draft (DELETE server-side, with confirm). Otherwise: permanent delete of the focused message (**NOT undoable**) |
| `d`       | Soft-delete (focus pops back to list)                         |
| `a`       | Archive (focus pops back to list)                             |
| `c`       | Add category (prompts for the name)                           |
| `C`       | Remove category (prompts for the name)                        |
| `U`       | Unsubscribe (RFC 8058 / mailto / browser; with confirm)       |
| `M`       | Toggle mute on the focused message's conversation thread      |
| `T`       | Begin thread chord — acts on the whole conversation (see Messages pane table above) |
| `S`       | Begin stream chord — route the focused message's sender (same sub-verbs as Messages pane) |
| `L`       | Toggle Reply Later for the focused message                    |
| `P`       | Toggle Set Aside for the focused message                      |
| `u`       | Undo the most recent triage action                            |
| `o`       | Open message in system browser (OWA deep-link / webLink)      |
| `O`       | Open the URL picker (lists every URL the renderer extracted)  |
| `1`–`9`   | Open extracted link N directly (skips the picker for quick access) |
| `a`–`z`   | Save attachment with that accelerator letter to `~Downloads`  |
| `A`–`Z`   | Open attachment with that accelerator letter (reserved: H/D/C/R/U) |
| `[`       | Navigate to the previous message in the conversation thread   |
| `]`       | Navigate to the next message in the conversation thread       |
| `y`       | Yank a URL to the clipboard (single URL → fast path; multi → picker) |
| `z`       | Toggle fullscreen body (hide folders + list panes for drag-select) |

**Fullscreen body** (`z`): hides the folder and list panes so the
viewer occupies the full terminal width. Inside fullscreen, all the
usual triage and compose actions are available — `r` reply, `R`
reply-all, `f` forward, `d` delete, `a` archive — so you rarely
need to exit before acting. Press `z`, `Esc`, or `q` to return to
the three-pane layout.

**Compact headers** (default): only From / Date / Subject + first 3
recipients with "+ N more". On a 50-attendee thread, the body
gets the room. Press `H` to expand To / Cc / Bcc on their own
lines.

**Stacks header**: when a message is in the Reply Later or Set Aside
stack (or both), a `Stacks: ↩ Reply Later · 📌 Set Aside` line
appears between Date and Subject in the compact header block.

**Clickable URLs**: every URL in a rendered message body — inline
and in the trailing `Links:` block — is wrapped in OSC 8 hyperlink
escapes WITH a stable `id=` parameter so terminals can group all
fragments of the same URL as one logical link. The practical effect:
when a long URL wraps to multiple visual rows, hovering any row
highlights the entire URL together. Modern terminals (iTerm2, kitty,
alacritty, foot, wezterm, ghostty, recent gnome-terminal / Konsole)
make these directly clickable (Cmd-click on macOS, Ctrl-click on
Linux). Older terminals (Apple Terminal.app) fall back to plain
text — use the URL picker (`O`) instead.

**Attachments block**: messages with attachments paint a compact
list between the headers and the body — `Attach: 3 files · 4.4 MB`
summary line followed by one line per attachment with an accelerator
letter prefix (`[a]`, `[b]`, …), name, human size, and content-type.
Press the letter to save to `~/Downloads`; press Shift+letter (e.g.
`A`) to download and open with your default app. Files larger than
25 MB (configurable: `[rendering].large_attachment_warn_mb`) show a
confirmation prompt first.  An `(inline)` flag marks `cid:`-referenced
images embedded in the body.

**Long-URL truncation**: URLs longer than 60 cells render with end-
truncation in the body — `https://example.com/auth/…` — so they
don't dominate vertical space when you scroll. The OSC 8 escape
sequence keeps the FULL URL in its `url` portion, so:

- Cmd-click on a truncated URL still opens the full URL.
- The URL picker (`O`) shows full URLs.
- The `Links:` block at the bottom of every body keeps full URLs
  untruncated — that's the canonical place to read or copy a full
  link.

The domain prefix is always preserved (security: spot phishing at
a glance).

**URL picker (`O`)**: lists every URL the renderer pulled out of
the body. `j` / `k` move the cursor; `Enter` or `O` opens the
selected URL in your default browser; `y` copies it to the
clipboard; `Esc` / `q` close. This is the workflow that handles
URLs that wrap across rows (terminal click can't pick those up) and
disambiguates short anchor texts that share the same hostname.
For fast access to a specific link, press its digit (`1`–`9`) directly
from the viewer — no picker needed.

**Yank URL (`y`)**: when the message has exactly one URL, `y` in
the viewer copies it directly. With more than one, `y` opens the
picker first so you can choose. Copy is delivered via OSC 52
(works over SSH on iTerm2 / WezTerm / Kitty / Ghostty / foot /
Alacritty / Windows Terminal / recent VTE) and, on macOS,
additionally via `pbcopy` so Apple Terminal users still get the
local clipboard. tmux users need `set -g set-clipboard on` for
OSC 52 passthrough.

**Folder picker (`m`)**: opens a centered modal listing every
synced folder, with the ones you've most recently moved messages
to ranked at the top under `[recent]`. Type any substring to
narrow (case-insensitive on the path AND the well-known alias);
the `↑` / `↓` arrow keys move the cursor (so `j` and `k` flow
into the filter buffer, not navigation); `Enter` dispatches the
move and bumps that folder to the top of the recents; `Esc`
cancels. The MRU list is session-scoped; the cap is the
`[triage].recent_folders_count` config key (default 5). The
Drafts well-known folder is filtered out of destinations because
Graph rejects moves into Drafts for non-draft items.

**Fullscreen body (`z`)**: hides the folders + list panes so the
viewer body uses the full terminal width. Use this when you need
terminal-native multi-line drag-select to copy a paragraph — the
side-by-side three-pane layout normally breaks rectangular
selection across pane borders. Press `z` again (or `Esc` / `q`) to
return.

**Compose flow** (`r` / `R` / `f` / `m`): each binding opens the
in-modal compose pane with a different skeleton:

- `r` Reply: To = source's From, Subject = `Re: <source>`, body
  starts with the quote chain.
- `R` Reply All: To = source's From + remaining To recipients
  (deduped against your UPN); Cc = source's Cc; Subject = `Re:
  <source>`.
- `f` Forward: To/Cc empty; Subject = `Fwd: <source>`; body opens
  with a `---------- Forwarded message ----------` header block
  carrying the source's From / Date / Subject / To, followed by
  the source body verbatim.
- `m` New message: blank form; focus drops into To since recipients
  are your first task (no source-sender to pre-fill).

While in compose:

| Key            | Action                                              |
| -------------- | --------------------------------------------------- |
| `Tab`          | Cycle field forward (Body → To → Cc → Subject → Body) |
| `Shift+Tab`    | Cycle backward                                      |
| `Ctrl+S`       | Save the draft, close the pane                      |
| `Esc`          | Save (alias for Ctrl+S — the "I'm done" gesture)    |
| `Ctrl+D`       | Discard the draft (no Graph round-trip)             |
| `Ctrl+E`       | Open the body in `$INKWELL_EDITOR` / `$EDITOR` / nano; apply on exit |
| `Ctrl+A`       | Attach a local file — opens a path-input prompt     |

Save dispatches via the action queue to Microsoft Graph: Reply /
Reply All / Forward use a two-stage createReply* + PATCH; New
uses a single-stage POST /me/messages with the full payload. The
draft lands in your Drafts folder. The status bar shows `✓ draft
saved · s open · D discard`. Press `s` (in Normal mode) to launch
the draft in your browser / Outlook desktop, where you finalise
send. Press `D` (in Normal mode) to DELETE the draft from the
server (with confirm). inkwell never sends mail — see the
[explanation](explanation.md#why-no-send) for why.

The status-bar hint auto-clears after `[compose].web_link_ttl`
(default 30 s). Once it clears, `D` reverts to permanent-delete
of the focused message.

**Recipient recovery**: if you clear the `To:` field by accident
on a Reply / Reply All / Forward, Save falls back to the original
sender's address (the implicit recipient). If neither the form
nor the source has a recipient, you get an actionable error and
the form state stays on screen so you can correct and retry. New
messages don't have a source-sender to fall back to — Save errors
out asking you to fill `To:` explicitly.

**Crash-recovery**: the form state (kind / source / To / Cc /
Subject / Body) snapshots into a local table on entry and on
each Tab. If inkwell crashes mid-compose, on next launch you get
a confirm modal offering to resume where you left off — `y`
restores the form into ComposeMode; `n` discards it. Confirmed
sessions older than 24h get garbage-collected on launch.

**Markdown drafts (spec 33).** Set `[compose] body_format =
"markdown"` in config to compose in CommonMark. inkwell converts
the body to HTML via goldmark (GFM extensions: tables,
`~~strikethrough~~`, `- [x]` task lists, autolinked URLs) before
saving the draft. The compose footer shows ` · [md]` when this
mode is on. `Ctrl+E` writes a `.md` tempfile so `$EDITOR` activates
Markdown filetype detection. The reply quote chain (`> ` prefixed
lines from the source) renders as a `<blockquote>` automatically.
Default is `body_format = "plain"` — your draft body goes through
unchanged.

The footer indicator `[md]` is the only persistent visual signal
that Markdown mode is on. If you type `**bold**` expecting literal
asterisks but see them rendered as `<strong>bold</strong>` in
Outlook, check this indicator (or your config).

The format is captured at compose-entry time and cannot drift
mid-session — a config change requires a fresh compose to take
effect.

**Outlook caveat.** Outlook desktop re-renders HTML drafts through
its own editor when you open them for send, normalising whitespace
and applying its default theme. goldmark's output (`<p>`,
`<strong>`, `<ul>`, `<table>`, `<blockquote>` — no `<style>` or
inline CSS) survives this cleanly; recipients see Outlook's
default styling applied on top. Outlook desktop's default table CSS
is sparse, so GFM tables render borderless there; OWA, Gmail, and
Apple Mail render bordered tables out of the box.

**Editing existing drafts.** Spec 33 covers the compose-once-save
flow only. Reopening a saved draft from the Drafts folder for
further editing in inkwell is not in scope — the user finalises
edits in Outlook (which respects the draft's stored `contentType`).

**Pane-scoped bindings**:
- `r`: viewer = reply; messages pane = mark-read.
- `R`: viewer = reply-all; messages pane = mark-unread; folders pane = rename-folder.
- `f`: viewer = forward (when drafts wired) else toggle-flag; messages pane = toggle-flag.
- `m`: viewer + folders pane = new message (when drafts wired) else move; messages pane = move-with-folder-picker.

## Command mode (`:`)

| Command                       | Effect                                                         |
| ----------------------------- | -------------------------------------------------------------- |
| `:quit` / `:q`                | Exit                                                            |
| `:sync`                       | Trigger a sync cycle now                                        |
| `:signin`                     | Re-auth (opens system browser)                                  |
| `:signout`                    | Confirm modal → clears tokens + local cache                     |
| `:filter <pattern>`           | Narrow message list to pattern matches (cross-folder by default, no folder metadata shown) |
| `:filter --all <pattern>`     | Same query, but shows folder count in status bar, FOLDER column in list pane, and folder context in confirm modal |
| `:filter -a <pattern>`        | Short form of `--all`                                           |
| `:unfilter`                   | Clear active filter, restore prior folder                       |
| `:archive`                    | Spec 30. Archive the focused message (same as pressing `a` / `e`). |
| `:done`                       | Spec 30. Alias of `:archive`; same dispatch path.               |
| `:refresh`                    | Force a sync cycle now (same as `Ctrl+R`)                       |
| `:folder <name>`              | Jump the list pane to a folder (DisplayName or well-known like `inbox`) |
| `:search <query>`             | Run an FTS search and show hits (same as `/<query>`)            |
| `:open`                       | Open the focused message's webLink in the system browser        |
| `:open <N>`                   | Open numbered link N (1–9) from the viewer in the system browser |
| `:copy <N>`                   | Copy numbered link N URL to the clipboard (OSC 52 / pbcopy)    |
| `:save <letter>`              | Save viewer attachment by letter (`a`=first, `b`=second, …) to `attachment_save_dir` |
| `:save <letter> <path>`       | Save viewer attachment to a specific path (e.g. `:save a ~/Desktop/invoice.pdf`) |
| `:backfill`                   | Pull older messages past the cache wall for the focused folder |
| `:cal` / `:calendar`          | Open today's calendar in a modal                                |
| `:settings`                   | Open the read-only mailbox-settings overview modal              |
| `:ooo` / `:outofoffice`       | Open the out-of-office editing modal                            |
| `:ooo on`                     | Enable automatic replies immediately (alwaysEnabled)            |
| `:ooo off`                    | Disable automatic replies immediately                           |
| `:ooo schedule`               | Open the OOF modal with "scheduled" pre-selected                |
| `:unsub` / `:unsubscribe`     | Unsubscribe from the focused message (same flow as `U` keybinding) |
| `:rule save <name>`           | Save the active `:filter` pattern as a named saved search       |
| `:rule list`                  | Show all saved search names in the status bar                   |
| `:rule show <name>`           | Show a saved search's pattern in the status bar                 |
| `:rule edit <name>`           | Open the edit modal (rename, change pattern/pinned)             |
| `:rule delete <name>`         | Delete a saved search (with confirm)                            |
| `:route <email> <dest>`       | Route sender to a destination: `imbox`, `feed`, `paper_trail`, `screener`, or `clear` |
| `:screener accept <email>`    | Spec 28 §7.1. Admit a sender. `--to imbox\|feed\|paper_trail` overrides the default Imbox destination. |
| `:screener reject <email>`    | Spec 28 §7.1. Screen out a sender (alias for `:route <email> screener`).        |
| `:screener list`              | Navigate to the Screener virtual folder (`__screener__`).        |
| `:screener history`           | Navigate to the Screened-Out virtual folder (`__screened_out__`). Requires `[screener].enabled = true`. |
| `:screener status`            | Toast the current screener configuration (gate state, grouping, mute exclusion). |
| `:tab list`                   | Show all configured tabs in the status bar                      |
| `:tab add <name> <pattern>`   | Add a saved search as a list-pane tab                           |
| `:tab remove <name>`          | Remove a tab (does not delete the saved search)                 |
| `:tab move <name> <position>` | Reorder a tab (1-based)                                         |
| `:focused`                    | Spec 31. Switch to the Focused sub-tab over the Inbox folder. Navigates to Inbox first if needed. Requires `[inbox].split = "focused_other"`. |
| `:other`                      | Spec 31. Switch to the Other sub-tab over the Inbox folder. Same preconditions as `:focused`.                          |
| `:later`                      | Toggle Reply Later for the focused message                      |
| `:aside`                      | Toggle Set Aside for the focused message                        |
| `:focus`                      | Enter focus mode — fan through Reply Later stack one by one     |
| `:actions` / `:actions list`  | List configured custom actions (spec 27)                        |
| `:actions show <name>`        | Show a custom action's resolved sequence                        |
| `:actions run <name>`         | Run a custom action against the focused message                 |
| `:help` / `:?`                | Open the help overlay (same as `?`)                             |

Plain-text patterns without a `~` operator are treated as a CONTAINS
search across subject and body (`~B *<text>*`). `:filter [External]`
matches any message whose subject or body contains `[External]`.

## Search mode (`/`)

| Key            | Action                                                         |
| -------------- | -------------------------------------------------------------- |
| `<text> Enter` | Run FTS query scoped to the current folder, replace list pane with hits |
| `--all <text> Enter` | Run FTS query across **all** subscribed folders (spec 06 §5.3) |
| `--sort=relevance <text> Enter` | Sort results by BM25 relevance score instead of received-date (spec 06 §4.3) |
| `Esc`          | Cancel; if a search is active, clear it and restore the folder |
| `Backspace`    | Delete the last character of the buffer                        |

By default, `/` search is scoped to the folder visible in the list pane.
Prefix the query with `--all` to search across all subscribed folders:

```
/--all budget review
```

Use `--sort=relevance` when BM25 ranking matters more than recency:

```
/--sort=relevance quarterly earnings report
```

Flags can be combined: `--all --sort=relevance <query>`.

Search uses a hybrid strategy: FTS5 against the local SQLite cache
runs first (sub-100ms), followed by a Graph `$search` round-trip
that merges any server-side hits not yet in the cache. Use
`--local-only` (CLI) or the `/--local-only` prefix (TUI) to skip
the server leg entirely (offline-safe).

## Calendar mode (`:cal` or `c` from Folders pane)

The calendar modal can be opened from any pane via `:cal` (command bar) or
by pressing `c` while the Folders pane is focused.

| Key            | Action                                                         |
| -------------- | -------------------------------------------------------------- |
| `j` / `↓`      | Move cursor to next event                                      |
| `k` / `↑`      | Move cursor to previous event                                  |
| `Enter`        | Open the focused event's detail modal (attendees, body, links) |
| `]`            | Navigate to the next day                                       |
| `[`            | Navigate to the previous day                                   |
| `}`            | Navigate forward one week                                      |
| `{`            | Navigate back one week                                         |
| `t`            | Jump to today                                                  |
| `w`            | Toggle week-grid view (shows all events for Mon–Sun of the current week) |
| `a`            | Return to agenda view (when week-grid view is active)          |
| `Esc` / `q`    | Close the modal, return to Normal mode                         |

Read-only. To act on an event, finish in Outlook.

### Sidebar calendar section

When `[calendar].sidebar_show_days` is set (default 2), inkwell adds a
calendar section at the bottom of the Folders sidebar showing today's and
upcoming events. Pressing `Enter` on a sidebar calendar event opens the
full detail modal directly (same as pressing `Enter` in the `:cal` agenda
view). The section refreshes automatically after each background sync.

## Calendar detail mode (Enter on `:cal`)

| Key            | Action                                                         |
| -------------- | -------------------------------------------------------------- |
| `o`            | Open the event in Outlook (web link)                           |
| `l`            | Open the online meeting URL (Teams / Zoom / etc.)              |
| `Esc` / `q`    | Close the detail modal, return to the calendar list            |

Attendee status glyphs: `✓` accepted, `~` tentatively accepted,
`✗` declined, `?` not responded.

## Settings mode (`:settings`)

| Key            | Action                                                         |
| -------------- | -------------------------------------------------------------- |
| `o`            | Switch to the OOF editing modal                                |
| `Esc` / `q`    | Close the modal, return to Normal mode                         |

The settings modal is read-only and shows: Automatic Replies status,
Time Zone, Locale, Date Format (if set), Time Format (if set), and
Working Hours (if configured on the mailbox).

The OOO status-bar indicator (`🌴 OOO` by default, configurable via
`[mailbox_settings].ooo_indicator`) appears next to the account UPN
whenever automatic replies are active.

## Out-of-office mode (`:ooo`)

| Key            | Action                                                         |
| -------------- | -------------------------------------------------------------- |
| `Space`        | Cycle status: Off → On → On with schedule → Off                |
| `Tab`          | Move focus to next field                                       |
| `Shift+Tab`    | Move focus to previous field                                   |
| `Enter`        | Save (PATCH /me/mailboxSettings)                               |
| `Esc` / `q`    | Close the modal, return to Normal mode (no save)               |

Fields: Status radio (Off / On / On with schedule), Start/End date+time
(only when scheduled), Audience radio (All / Contacts only / None),
Internal message preview, External message preview.

---

## Pattern operators

Argument-bearing:

| Operator         | Field            | Example                              |
| ---------------- | ---------------- | ------------------------------------ |
| `~f <addr>`      | from             | `~f bob@acme.com`, `~f newsletter@*` |
| `~t <addr>`      | to               | `~t me@example.invalid`              |
| `~c <addr>`      | cc               | `~c ceo@*`                           |
| `~r <addr>`      | recipient (to+cc)| `~r alice@`                          |
| `~s <text>`      | subject          | `~s budget`, `~s "Q4 review"`        |
| `~b <text>`      | body             | `~b "action required"`               |
| `~B <text>`      | subject OR body  | `~B forecast`                        |
| `~d <date-expr>` | received date    | `~d <30d`, `~d >=2026-01-01`         |
| `~D <date-expr>` | sent date        | `~D yesterday`                       |
| `~G <category>`  | category         | `~G Newsletters`                     |
| `~i <level>`     | importance       | `~i high`                            |
| `~y <class>`     | inference class  | `~y focused`                         |
| `~v <conv-id>`   | conversation     | `~v <id>`                            |
| `~m <folder>`    | folder           | `~m Inbox`                           |
| `~o <dest>`      | routing dest     | `~o imbox`, `~o feed`, `~o paper_trail`, `~o screener`, `~o none` (no row), `~o pending` (alias for `none`, spec 28 §4.5) |

Argument-less:

| Operator | Field             |
| -------- | ----------------- |
| `~A`     | has attachments   |
| `~N`     | unread (new)      |
| `~U`     | read              |
| `~F`     | flagged           |

Boolean composition:

| Syntax              | Meaning                                |
| ------------------- | -------------------------------------- |
| `~f a ~s b`         | Implicit AND between adjacent atoms    |
| `~f a & ~s b`       | Explicit AND                           |
| `~f a \| ~f b`      | OR                                     |
| `! ~N`              | NOT                                    |
| `(~f a \| ~f b) ~A` | Grouped (parens override precedence)   |

Wildcards (string operators only):

| Form         | Match            |
| ------------ | ---------------- |
| `foo`        | exact            |
| `foo*`       | starts-with      |
| `*foo`       | ends-with        |
| `*foo*`      | contains         |

Date expressions:

| Form                  | Meaning                                              |
| --------------------- | ---------------------------------------------------- |
| `<30d`                | Within the last 30 days                              |
| `>30d`                | Older than 30 days                                   |
| `<=24h`               | Within last 24 hours                                 |
| `>=2026-01-01`        | On or after a date (UTC)                             |
| `<2026-04-01`         | Before                                               |
| `2026-03..2026-04`    | Range, inclusive on day boundaries                   |
| `today`               | Today (local time-of-day)                            |
| `yesterday`           | Yesterday                                            |

Duration units: `s`, `m` (minutes), `h`, `d`, `w`, `mo` (≈30 days),
`y` (≈365 days).

---

## Modes

| Mode           | How you enter                             | How you exit                                     |
| -------------- | ----------------------------------------- | ------------------------------------------------ |
| Normal         | (default)                                 | —                                                |
| Command        | `:`                                       | `Enter` (run) or `Esc`                           |
| Search         | `/`                                       | `Enter` (run) or `Esc`                           |
| Palette        | `Ctrl+K`                                  | `Enter` (run) or `Esc`                           |
| SignIn         | auth flow                                 | `Esc`                                            |
| Confirm        | destructive prompts                       | `y` (confirm) or `n` / `Esc` (cancel)            |
| ThreadChord    | `T` (list / viewer)                       | sub-verb key or `Esc` / timeout                  |
| StreamChord    | `S` (list / viewer)                       | sub-verb key (`i`/`f`/`p`/`s`/`c`) or `Esc` / timeout |
| Calendar       | `:cal` / `:calendar` / `c` (Folders pane) | `Esc` or `q`                                     |
| CalendarDetail | `Enter` on a calendar event               | `Esc` or `q` (`o` Outlook, `l` meeting URL)      |
| Settings       | `:settings`                               | `Esc` or `q` (`o` to edit OOF)                   |
| OOO            | `:ooo` / `:oof` / `:outofoffice`          | `Esc` or `q` (`Space` cycles status, `Enter` saves) |
| FolderPicker   | `m` (list / viewer)                       | `Esc` (cancel) or `Enter` (move)                 |

## Indicators

| Glyph              | Meaning                                                         |
| ------------------ | --------------------------------------------------------------- |
| `▌ <Title>`        | Pane is focused                                                 |
| `▶`                | Cursor on this row, focused pane                                |
| `· `               | Cursor on this row, unfocused pane                              |
| `▾` / `▸`          | Folder expanded / collapsed                                     |
| `☆`                | Saved search                                                    |
| `📅`               | Calendar-invite message (heuristic by subject prefix)           |
| `📥`               | Sender routed to Imbox                                          |
| `📰`               | Sender routed to Feed                                           |
| `🧾`               | Sender routed to Paper Trail                                    |
| `🚪`               | Sender routed to Screener                                       |
| `↩`                | Message is in the Reply Later stack                             |
| `📌`               | Message is in the Set Aside stack                               |
| `▸` (list pane)    | Bundle header, collapsed (spec 26). The disclosure glyph replaces the flag/calendar slot on bundle rows. |
| `▾` (list pane)    | Bundle header, expanded — member rows follow below.             |
| `✓ synced HH:MM`   | Last sync time (top-right)                                      |
| `syncing folders…` | Engine is working                                               |
| `syncing more…`    | Engine kicked because list pane hit the cache wall              |
| `⏳ throttled Ns`  | Graph is rate-limiting; engine backing off                      |
| `ERR: …`           | Last error; full text in the log file                           |
| `✓ <action> N`     | Bulk op succeeded for N messages                                |
| `⚠ <action> X/Y`   | Bulk op partial — X succeeded, X+Y attempted                    |

---

## CLI subcommands (non-interactive)

inkwell ships a scriptable surface alongside the TUI. Run any of
these without launching the interface; output is text by default and
JSON via `--output json`.

| Command                                          | What it does                                              |
| ------------------------------------------------ | --------------------------------------------------------- |
| `inkwell signin` / `signout` / `whoami`          | Auth — same flow as the TUI's `:signin` / `:signout`.     |
| `inkwell sync`                                   | Run one sync cycle now and exit.                          |
| `inkwell folders`                                | List cached folders.                                      |
| `inkwell messages --folder Inbox --limit 50`     | List envelopes from a folder.                             |
| `inkwell messages --folder Inbox --unread`       | Only unread.                                              |
| `inkwell messages --filter '~f bob' --limit 20`  | List by spec-08 pattern.                                  |
| `inkwell messages --filter '~f bob' --all`       | Same, but ignores any `--folder` scope and returns all-folder results. |
| `inkwell messages --view focused`                | Spec 31. Sugar for `--filter '~y focused' --folder Inbox`. Exits 2 on an unknown value or when combined with a non-Inbox `--folder`. Composes with `--filter` (AND'd). |
| `inkwell messages --view other`                  | Spec 31. Same as `--view focused` for the Other segment.                |
| `inkwell messages --filter '~U' --watch`         | Spec 29. Stream new matches like `tail -f`; Ctrl-C exits 0. |
| `inkwell messages --rule VIPs --watch`           | Same, with a saved-search name (spec 11) instead of a literal pattern. |
| `inkwell messages --filter X --watch --initial=N` | Print the most-recent N matches at startup before entering the loop. |
| `inkwell messages --filter X --watch --include-updated` | Re-emit a previously-seen message when its `last_modified_at` advances. |
| `inkwell messages --filter X --watch --count N`  | Exit 0 after N new matches. |
| `inkwell messages --filter X --watch --for D`    | Exit 0 after wall-clock duration D. |
| `inkwell messages --filter X --watch --interval D` | Re-evaluation cadence (default = engine foreground interval; min 5s). |
| `inkwell messages --filter X --watch --no-sync`  | Skip starting an embedded sync engine; tail the cache only (use when a TUI / `inkwell daemon` is already syncing). |
| `inkwell message show <id>`                      | Print headers + body for one message.                     |
| `inkwell message show <id> --headers`            | Include full To / Cc / Bcc.                               |
| `inkwell search "q4 budget"`                     | Hybrid search (local FTS5 + Graph $search), all folders.  |
| `inkwell search --folder Inbox "from:alice"`     | Scope search to a single folder.                          |
| `inkwell search --local-only "draft notes"`      | FTS5 only; skip Graph $search (offline-safe).             |
| `inkwell search --sort-relevance "annual review"`| BM25 relevance order instead of received-date DESC.       |
| `inkwell filter '<pattern>'`                     | Print matched envelopes (dry-run).                        |
| `inkwell filter '<pattern>' --all`               | Same, with a `folders` count map added to output (shows which folders were touched). |
| `inkwell filter '<pattern>' --action delete --apply`   | Bulk soft-delete via Graph $batch.                  |
| `inkwell filter '<pattern>' --action archive --apply`  | Bulk archive.                                       |
| `inkwell filter '<pattern>' --action mark-read --apply`| Bulk mark-read.                                     |
| `inkwell thread archive <conv-id>`                     | Spec 30. Archive every message in a conversation. Cobra alias: `inkwell thread done <conv-id>`. |
| `inkwell thread done <conv-id>`                        | Alias of `inkwell thread archive`. Same RunE.       |
| `inkwell route assign <email> <dest>`                  | Set sender routing: `imbox`, `feed`, `paper_trail`, `screener`. |
| `inkwell route clear <email>`                          | Remove routing for a sender.                        |
| `inkwell route list`                                   | List all routing entries.                           |
| `inkwell route show <email>`                           | Show routing for one sender.                        |
| `inkwell screener list`                                | Spec 28 §7. List Pending senders. `--grouping=message` lists messages instead. |
| `inkwell screener accept <email>`                      | Admit a sender. `--to imbox\|feed\|paper_trail` overrides the default Imbox. |
| `inkwell screener reject <email>`                      | Screen out a sender (alias for `route assign <email> screener`). |
| `inkwell screener history`                             | List Screened-Out senders.                          |
| `inkwell screener pre-approve --from-stdin < file`     | Bulk-admit senders from stdin (one address per line; `#` comments and blank lines skipped). |
| `inkwell screener pre-approve --from-file <path>`      | Bulk-admit senders from a file. Mutually exclusive with `--from-stdin`. |
| `inkwell screener status`                              | Print the gate / grouping / counts.                 |
| `inkwell tab list`                                     | List configured list-pane tabs.                     |
| `inkwell tab add <name> <pattern>`                     | Add a saved search as a tab.                        |
| `inkwell tab remove <name>`                            | Remove a tab (keeps the saved search).              |
| `inkwell tab move <name> <position>`                   | Reorder a tab (1-based position).                   |
| `inkwell later list`                                   | List messages in the Reply Later stack.             |
| `inkwell later add <id>`                               | Add a message to Reply Later.                       |
| `inkwell later remove <id>`                            | Remove a message from Reply Later.                  |
| `inkwell later clear`                                  | Clear the entire Reply Later stack (with confirm).  |
| `inkwell aside list`                                   | List messages in the Set Aside stack.               |
| `inkwell aside add <id>`                               | Add a message to Set Aside.                         |
| `inkwell aside remove <id>`                            | Remove a message from Set Aside.                    |
| `inkwell aside clear`                                  | Clear the entire Set Aside stack (with confirm).    |
| `inkwell bundle add <email>`                           | Designate a sender for bundling (spec 26).          |
| `inkwell bundle remove <email>`                        | Remove a bundled-sender designation.                |
| `inkwell bundle list`                                  | List currently bundled senders.                     |
| `inkwell action list`                                  | List custom actions configured in `actions.toml` (spec 27). |
| `inkwell action show <name>`                           | Show a custom action's resolved sequence.           |
| `inkwell action run <name> --message <id>`             | Execute a custom action against a specific message. |
| `inkwell action run <name> --filter '<pattern>'`       | Execute a custom action against the matched message set (capped by `[bulk].size_hard_max`). Rejected when the action's templates reference per-message variables. |
| `inkwell action validate`                              | Load + validate `actions.toml` without running anything. |

`--output json` works on every command above. Pipe into `jq` for
ad-hoc analysis:

```sh
inkwell messages --folder Inbox --unread --output json | jq '.[] | .subject'
inkwell filter '~A & ~d <30d' --output json | jq '.matched'
inkwell route list --output json | jq '.[] | select(.destination == "feed")'
```

`--apply` is **mandatory** for destructive bulk operations. Without
it, `inkwell filter` is dry-run regardless of any config setting.
`--yes` skips the confirmation prompt for `delete`.

## Configuration

`~/.config/inkwell/config.toml`. Full key reference:
[`docs/CONFIG.md`](../CONFIG.md). Most-used keys:

```toml
[ui]
theme = "default"  # default | dark | light | solarized-dark | solarized-light | high-contrast
# Each theme assigns distinct semantic colors:
#   links    → cyan family (visually distinct, universally "hyperlink")
#   attachments → amber family (visually distinct from links and body text)

[account]
upn = "you@example.invalid"  # optional safety check

[[saved_searches]]
name    = "Newsletters"
pattern = "~f newsletter@* | ~f noreply@*"
```

Restart inkwell after editing.

---

## Custom actions (spec 27)

User-authored recipes loaded from `~/.config/inkwell/actions.toml` (path overridable via `[custom_actions].file`). Each `[[custom_action]]` declares a name, optional single-key binding, optional `confirm = "auto" | "always" | "never"`, and a `sequence = [...]` of step tables. The catalogue is read once at startup; edits require a restart (no hot reload). Use `inkwell action validate` to dry-run a recipe file.

**Op catalogue (22 ops, v1.1):**

| Group | Ops |
| --- | --- |
| Triage | `mark_read`, `mark_unread`, `flag`, `unflag`, `archive`, `soft_delete`, `permanent_delete` (destructive — forces confirm), `move`, `add_category`, `remove_category` |
| Per-sender / per-thread | `set_sender_routing` (literal `imbox`/`feed`/`paper_trail`/`screener`; **not undoable by `u`**), `set_thread_muted` (default `value = true`; **not undoable**), `thread_add_category`, `thread_remove_category`, `thread_archive`, `unsubscribe` |
| Filter / bulk | `filter`, `move_filtered`, `permanent_delete_filtered` (destructive) |
| Control flow | `prompt_value` (modal mid-sequence; binds the user's reply into `{{.UserInput}}`), `advance_cursor`, `open_url` |

Deferred to a future spec (rejected at load): `block_sender`, `shell`, `forward`.

**Template variables** (Go `text/template` syntax — `{{.Name}}` not `{name}`):

`{{.From}}`, `{{.FromName}}`, `{{.SenderDomain}}`, `{{.To}}`, `{{.Subject}}`, `{{.ConversationID}}`, `{{.MessageID}}`, `{{.Date}}`, `{{.Folder}}`, `{{.UserInput}}`. The roadmap's draft single-brace alias syntax (`{sender}`, `{subject}`, …) is rewritten with a deprecation warning at load.

**Safety opt-ins:**

- `allow_folder_template = true` — required when a `move` / `move_filtered` destination references message data (e.g. `"Clients/{{.SenderDomain}}"`).
- `allow_url_template = true` — required when an `open_url` URL templates message data (PII exfil guard, T-CA3 in the threat model).

**Single-key bindings only.** Chord-style strings (`"<C-x> n"`, whitespace) are rejected. The catalogue's `key` participates in the global duplicate-detector — collisions with `[bindings]` defaults abort startup.

**Reversibility.** Most ops route through the spec 07 action queue and reverse via `u` like any other triage. `set_sender_routing` and `set_thread_muted` are synchronous direct writes and are NOT undoable by `u`; the result toast flags non-undoable rows with `[non-undoable]`.

## Server-side rules (spec 32)

Server-side Inbox rules are managed via the `inkwell rules` CLI; the authoring format is `~/.config/inkwell/rules.toml`. Rules run server-side on every incoming message — unlike saved searches (spec 11) which are client-side and on-demand. Required Graph scope `MailboxSettings.ReadWrite` is already in PRD §3.1.

**Workflow.** Edit `rules.toml`, then `inkwell rules apply --dry-run` to preview, then `inkwell rules apply` to push. `inkwell rules pull` syncs from server (overwriting the local file).

### CLI

| Command                                  | Description                                                                                  |
| ---------------------------------------- | -------------------------------------------------------------------------------------------- |
| `inkwell rules list [--refresh]`         | List cached rules (mirror). `--refresh` pulls first. `--output json` returns a flat array.    |
| `inkwell rules get <id>`                 | Verbose dump of one rule from the local mirror. `--output json` returns the full payload.    |
| `inkwell rules pull`                     | Fetch from Graph, rewrite `rules.toml` atomically.                                            |
| `inkwell rules apply [--dry-run] [--yes]`| Diff TOML against mirror; execute. Dry-run prints the plan; `--yes` skips prompts.            |
| `inkwell rules edit`                     | Open `rules.toml` in `$EDITOR`.                                                               |
| `inkwell rules new [--name X]`           | Append a stub rule block and open `$EDITOR`.                                                  |
| `inkwell rules delete <id> [--yes]`      | Synchronous PATCH; prompts unless `--yes`.                                                    |
| `inkwell rules enable <id>`              | Synchronous toggle.                                                                           |
| `inkwell rules disable <id>`             | Synchronous toggle.                                                                           |
| `inkwell rules move <id> --sequence N`   | Set sequence; synchronous PATCH.                                                              |

### TUI

The cmd-bar accepts `:rules <subverb>` for parity; v1 surfaces a one-line hint pointing at the CLI (the in-TUI manager modal is a follow-up). Command palette (Ctrl+K) ships five static rows:

| Palette row                            | Cmd-bar form               |
| -------------------------------------- | -------------------------- |
| Manage server rules…                   | `:rules list`              |
| Rules: pull from server                | `:rules pull`              |
| Rules: apply changes (push to server)  | `:rules apply`             |
| Rules: preview changes (dry-run)       | `:rules apply --dry-run`   |
| Rules: new rule from template…         | `:rules new`               |

### `rules.toml` catalogue (v1 subset)

Predicates (under `[rule.when]` or `[rule.except]`; items in a list are **OR'd**, separate predicates are **AND'd**):

`body_contains`, `body_or_subject_contains`, `subject_contains`, `header_contains`, `from` (recipient list or shorthand strings), `sender_contains`, `sent_to`, `recipient_contains`, `sent_to_me`, `sent_cc_me`, `sent_only_to_me`, `sent_to_or_cc_me`, `not_sent_to_me`, `has_attachments`, `importance` (`"low"|"normal"|"high"`), `sensitivity` (`"normal"|"personal"|"private"|"confidential"`), `size_min_kb`, `size_max_kb`, `categories`, `is_automatic_reply`, `is_automatic_forward`, `flag` (one of `any`, `call`, `doNotForward`, `followUp`, `fyi`, `forward`, `noResponseNecessary`, `read`, `reply`, `replyToAll`, `review`).

Actions (under `[rule.then]`):

`mark_read`, `mark_importance` (`"low"|"normal"|"high"`), `move` (folder slash-path, resolved to ID at apply time), `copy` (same), `add_categories`, `delete` (soft → Deleted Items; requires `confirm = "always"` at the rule level), `stop` (= `stopProcessingRules`).

Deferred in v1 (rejected by the loader; preserved on round-trip for read-only display):
- Predicates: `is_voicemail`, `is_meeting_request`, `is_meeting_response`, `is_approval_request`, `is_non_delivery_report`, `is_permission_controlled`, `is_read_receipt`, `is_signed`, `is_encrypted`.
- Actions: `forward_to`, `forward_as_attachment_to`, `redirect_to` (Mail.Send-adjacent; out of scope per PRD §3.2), `permanent_delete` (irreversible; requires per-message intent per CLAUDE.md §7 rule 9).

### Safety gates

- Any rule with `delete = true` MUST set `confirm = "always"` at the `[[rule]]` level. `confirm = "never"` is rejected for any destructive rule.
- `inkwell rules apply` always pulls from Graph first to narrow the conflict window.
- `inkwell rules apply` prompts Y/N for every destructive rule unless `--yes` is passed; the global `[rules].confirm_destructive` toggle (default `true`) is an additional belt-and-suspenders override.
- `rules.toml` is atomically rewritten via `.tmp` + `fsync` + `os.Rename`; orphans on write failure are cleaned by a defer.

_Last reviewed against v0.62.0._
