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
| `a`       | Archive (move to Archive folder)                              |
| `m`       | Move to a folder (opens picker; type to filter; Enter selects) |
| `c`       | Add category (prompts for the name)                           |
| `C`       | Remove category (prompts for the name)                        |
| `;`       | Begin bulk chord (only when a filter is active)               |
| `;d`      | Bulk delete the filtered set (with confirm)                   |
| `;a`      | Bulk archive the filtered set (with confirm)                  |
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
| `T a`     | Archive the entire thread                                     |
| `T m`     | Move whole thread (opens folder picker)                       |
| `S`       | Begin stream chord — route the focused message's sender (see below) |
| `S i`     | Route sender to **Imbox**                                     |
| `S f`     | Route sender to **Feed**                                      |
| `S p`     | Route sender to **Paper Trail**                               |
| `S k`     | Route sender to **Screener** (mnemonic: s**k**reener)         |
| `S c`     | **Clear** routing for the focused sender                      |
| `]`       | Cycle to the **next** spec 24 tab (list pane only)            |
| `[`       | Cycle to the **previous** spec 24 tab (list pane only)        |
| `L`       | Spec 25. Toggle Reply Later on the focused message            |
| `P`       | Spec 25. Toggle Set Aside (Pin) on the focused message        |
| `T l`     | Add the entire thread to Reply Later                          |
| `T L`     | Remove the entire thread from Reply Later                     |
| `T s`     | Add the entire thread to Set Aside                            |
| `T S`     | Remove the entire thread from Set Aside                       |
| `;l`      | Bulk: add to Reply Later (after `:filter`)                    |
| `;L`      | Bulk: remove from Reply Later                                 |
| `;s`      | Bulk: add to Set Aside                                        |
| `;S`      | Bulk: remove from Set Aside                                   |
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
| `S`       | Begin stream chord — route the focused sender (see Messages pane table above) |
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
| `:route assign <addr> <dest>` | Spec 23. Route a sender to imbox / feed / paper_trail / screener. Reassigns retroactively (past mail follows). |
| `:route clear <addr>`         | Clear routing for a sender (returns them to unrouted). |
| `:route show <addr>`          | Print the current routing for a sender in the status bar. |
| `:route list`                 | Print a summary count of all four routing destinations in the status bar. |
| `:tab list`                   | Spec 24. List configured tabs in the status bar. |
| `:tab add <name>`             | Promote saved search `<name>` to the tab strip (appends at end). |
| `:tab remove <name>`          | Demote a saved search from the tab strip. |
| `:tab move <name> <pos>`      | Reorder. `<pos>` is 0-based. |
| `:tab close`                  | Demote the active tab. |
| `:tab <name>`                 | Jump to the tab named `<name>`. |
| `:later`                      | Spec 25. Switch to the Reply Later virtual folder. |
| `:aside`                      | Switch to the Set Aside virtual folder. |
| `:focus [N]`                  | Walk the Reply Later queue, opening compose-reply for each message. `N` is an optional 1-indexed start position. |
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

Search is local-only (FTS5 against the SQLite cache) in v0.8.
Server-side `$search` merge is post-v0.8.

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
| `~o <dest>`      | routing destination (spec 23) | `~o feed`, `~o paper_trail`, `~o none` for unrouted senders |

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

| Mode        | How you enter        | How you exit                                     |
| ----------- | -------------------- | ------------------------------------------------ |
| Normal      | (default)            | —                                                |
| Command     | `:`                  | `Enter` (run) or `Esc`                           |
| Search      | `/`                  | `Enter` (run) or `Esc`                           |
| Palette     | `Ctrl+K`             | `Esc` (cancel) or `Enter` (run highlighted) — see "Command palette" below |
| SignIn      | auth flow            | `Esc`                                            |
| Confirm     | destructive prompts  | `y` (confirm) or `n` / `Esc` (cancel)            |
| Calendar    | `:cal` / `:calendar` / `c` (Folders pane) | `Esc` or `q` (`j`/`k` nav, `w` week-grid, `Enter` opens detail) |
| CalendarDetail | `Enter` on a calendar event | `Esc` or `q` (`o` Outlook, `l` meeting URL) |
| Settings    | `:settings`                      | `Esc` or `q` (`o` to edit OOF)        |
| OOO         | `:ooo` / `:oof` / `:outofoffice` | `Esc` or `q` (`Space` cycles status, `Enter` saves) |
| FolderPicker | `m` (list / viewer)              | `Esc` (cancel) or `Enter` (move)      |

## Command palette (`Ctrl+K`)

A fuzzy-find modal that exposes every action — keybinding, `:`
command verb, saved search, sidebar folder — in one overlay. The
right-hand column shows the live binding for each row, so the
palette doubles as a passive cheatsheet: every time you open it,
you can glance at the shortcut next to the action you just used and
learn the muscle-memory key.

| Key                | What it does                                          |
| ------------------ | ----------------------------------------------------- |
| `Ctrl+K`           | Open the palette (from Normal mode)                   |
| `↑` / `↓`          | Move cursor                                            |
| `Ctrl+P` / `Ctrl+N` | Same as `↑` / `↓` (readline / fzf parity)             |
| `Enter`            | Run the highlighted row                                |
| `Tab`              | For rows that need an argument (Move, Filter, Add category, Jump to folder, Saved search edit/delete), open the existing argument flow (folder picker, command-bar pre-fill, etc.) |
| `Esc`              | Close without acting                                   |
| `Backspace`        | Delete one rune from the buffer (no-op at empty)       |

Sigils scope the result list:

| Sigil | Scope                                                              |
| ----- | ------------------------------------------------------------------ |
| (none) | Mixed — commands + folders + saved searches                        |
| `#`    | Folders only                                                       |
| `@`    | Saved searches only                                                |
| `>`    | Commands only (rules out accidental folder/saved-search matches)   |

`/` is **not** a sigil — typing `/` after `Ctrl+K` inserts a literal
slash. The `/` global search key stays bound to spec 06's full-text
search, reachable from Normal mode.

When the buffer is empty, the palette surfaces recently-used
commands first (in-process MRU, capped at 8). Recents reset every
time you restart inkwell.

To disable the palette, set `palette = ""` in `[bindings]`. The
`:` cmd-bar and `?` help overlay are independent and stay available.

## Indicators

| Glyph              | Meaning                                                         |
| ------------------ | --------------------------------------------------------------- |
| `▌ <Title>`        | Pane is focused                                                 |
| `▶`                | Cursor on this row, focused pane                                |
| `· `               | Cursor on this row, unfocused pane                              |
| `▾` / `▸`          | Folder expanded / collapsed                                     |
| `☆`                | Saved search                                                    |
| `📅`               | Calendar-invite message (heuristic by subject prefix)           |
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
| `inkwell filter '<pattern>' --action mark-read --apply`| Bulk mark-read.                                      |
| `inkwell route assign <addr> <dest>`             | Spec 23. Route a sender to imbox/feed/paper_trail/screener. |
| `inkwell route clear <addr>`                     | Clear routing for a sender.                              |
| `inkwell route list`                             | List all routings.                                       |
| `inkwell route list --destination feed`          | Filter by destination.                                   |
| `inkwell route show <addr>`                      | Print the current routing for one sender.                |
| `inkwell tab list`                               | Spec 24. List the configured split-inbox tabs (with matched + unread counts). |
| `inkwell tab add <name>`                         | Promote a saved search to the tab strip.                |
| `inkwell tab remove <name>`                      | Demote a saved search from the tab strip.               |
| `inkwell tab move <name> <pos>`                  | Reorder (0-based).                                       |
| `inkwell later add <message-id>`                 | Spec 25. Tag a message into Reply Later.                |
| `inkwell later remove <message-id>`              | Untag.                                                   |
| `inkwell later list [--limit N]`                 | List messages in Reply Later (`--output json`).          |
| `inkwell later count`                            | Print the message count in Reply Later.                  |
| `inkwell aside add\|remove\|list\|count`         | Same shape for Set Aside.                                |

`--output json` works on every command above. Pipe into `jq` for
ad-hoc analysis:

```sh
inkwell messages --folder Inbox --unread --output json | jq '.[] | .subject'
inkwell filter '~A & ~d <30d' --output json | jq '.matched'
```

`--apply` is **mandatory** for destructive bulk operations. Without
it, `inkwell filter` is dry-run regardless of any config setting.
`--yes` skips the confirmation prompt for `delete`.

Deferred to v0.10+: `inkwell calendar`, `inkwell ooo`, `inkwell rule`
(saved-search CRUD), `inkwell message reply` / `forward` (drafts),
`inkwell message save-attachment`.

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

_Last reviewed against v0.8.0._
