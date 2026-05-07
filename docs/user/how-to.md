# How-to guides

Task-focused recipes. Each entry assumes you've completed the
[tutorial](tutorial.md). For exhaustive lookups see
[`reference.md`](reference.md); for "why does it work this way?" see
[`explanation.md`](explanation.md).

---

## Triage one message

Highlight a message in the list pane. Then press:

| Press | Effect                                |
| ----- | ------------------------------------- |
| `r`   | Mark read                             |
| `R`   | Mark unread                           |
| `f`   | Toggle flag                           |
| `d`   | Soft-delete (move to Deleted Items)   |
| `a`   | Archive                               |
| `m`   | Move to a folder (picker; type filter; Enter selects) |

`f` / `d` / `a` / `m` also work in the **viewer pane** (so you can
read, decide, delete without going back). Delete, archive, and move
pop you back to the list automatically — you immediately see what's
next. Mark-read and flag stay in place.

`r` / `R` are reserved in the viewer pane for the future reply /
reply-all bindings (spec 15) and don't currently mark-read there.
Use the list pane for that.

## Delete all newsletters older than 30 days

```
:filter ~f newsletter@* & ~d <30d
```

Press Enter. The list narrows. Glance at the matches (sanity check).
Then `;d` → confirm with `y`.

`~d <30d` means "received within the last 30 days" — the most
common interpretation of "<30 days". For "older than 30 days" use
`~d >30d`.

## Undo a triage action

Pressed `d` on the wrong message? Marked something read by accident?
Press `u` to roll the last triage back. Pairs:

- mark-read ↔ mark-unread
- flag ↔ unflag
- soft-delete → restored to the original folder
- archive → restored to the original folder
- add-category ↔ remove-category

The stack is session-scoped (cleared on app restart) and currently
unbounded — every triage action you do in a session is undoable in
reverse order. Pressing `u` on an empty stack paints "nothing to
undo" in the status bar; no error.

`U` (capital) is unsubscribe, not undo — see below. Permanent
delete (`D`, when shipped) is intentionally NOT undoable; the
confirm modal warns you.

## Reorganise your mailbox

Manage folders without leaving inkwell. Focus the folders pane
(`1`), then:

- **`o`** (or space) — expand / collapse the focused folder.
  Inbox is auto-expanded on first launch; everything else starts
  collapsed. The full folder tree (sub-folders, sub-sub-folders,
  any depth) syncs from Microsoft Graph and renders with
  indentation per level.
- **`N`** — create a new folder under the focused one. With no
  selection or focus on a top-level folder, creates a top-level.
  Type the name + Enter.
- **`R`** — rename the focused folder. The buffer pre-seeds with
  the current name so you can edit in place.
- **`X`** — delete the focused folder (with confirm). Children +
  messages cascade to Deleted Items server-side. Recoverable
  from Outlook's Deleted Items folder within the tenant
  retention window.

Well-known folders (Inbox, Drafts, Sent Items, Archive, Deleted
Items, Junk Email) reject rename and delete server-side; the
status bar shows the error.

CLI parity:

```sh
inkwell folder new "Vendor Quotes"
inkwell folder new "Vendor Quotes/2026"     # nested via slash
inkwell folder rename "Vendor Quotes" "Vendor"
inkwell folder delete "Vendor Quotes" --yes
```

## Get off a mailing list

Open any newsletter, then press `U` (or run `:unsub`). inkwell reads
the standard `List-Unsubscribe` headers (RFC 2369 + RFC 8058) and
picks the cheapest legitimate path:

- **One-click HTTPS POST** — when the sender supports the modern
  one-click contract, inkwell shows the URL it's about to POST to,
  asks for `y` to confirm, and unsubscribes you in one network call.
- **`mailto:`** — the unsub address opens in your default mail
  handler (Outlook / Apple Mail) with the prefilled subject/body.
- **HTTPS only** — opens the unsubscribe page in your browser; you
  finish there.

The confirm modal always shows the exact URL/address, so you can
spot a phishing attempt before pressing `y`. Plain `http://`
unsubscribe links are intentionally NOT auto-opened — inkwell
surfaces a friendly "open manually if you trust the sender" status
message and you decide.

After a successful unsubscribe, follow up with
`:filter ~f news@example.invalid` then `;a` → `y` to bulk-archive
past mail from the same sender.

## Bulk-archive everything from a single sender

```
:filter ~f bob@vendor.invalid
```

Then `;a` → `y`. Archived messages still exist on the server in your
Archive folder; nothing is permanently destroyed.

## Cross-folder cleanup

`:filter` already searches all folders by default, but the result
normally hides _which_ folders were touched. To surface that
information, prefix the pattern with `--all` (or `-a`):

```
:filter --all ~f newsletter@*
```

The status bar now shows `matched 247 (5 folders)`. The list pane
renders a FOLDER column so you can see at a glance where each match
lives. When you press `;d`, the confirm modal reads:

```
Delete 247 messages across 5 folders?
```

All other bulk verbs (`;a`, `;r`, `;R`, `;f`, `;F`) work the same way.

**Without `--all`:** the filter runs cross-folder silently (as it
always has), but the folder count is not shown and the FOLDER column
is hidden. This preserves the existing UX for users with single-folder
filter-heavy workflows.

**Muted threads:** cross-folder filter includes muted messages
(consistent with the search path). Muted rows carry the `🔕` indicator.

CLI equivalent:

```sh
# Dry-run: see which folders are affected
inkwell filter '~f newsletter@*' --all --output json | jq '.folders'

# Bulk-delete (same underlying query, --all adds folder metadata to output)
inkwell filter '~f newsletter@*' --action delete --apply --yes

# All-folders listing ignoring any --folder scope
inkwell messages --filter '~f bob@vendor.invalid' --all
```

## Find all unread messages from the last week with attachments

```
:filter ~U & ~A & ~d <7d
```

Plain reading: unread, has attachments, received within the last 7
days. Boolean operators: `&` AND, `|` OR, `!` NOT, parens for grouping.

## Set up saved searches

Edit `~/.config/inkwell/config.toml`. Add as many `[[saved_searches]]`
blocks as you like:

```toml
[[saved_searches]]
name    = "Newsletters"
pattern = "~f newsletter@* | ~f noreply@*"

[[saved_searches]]
name    = "Needs Reply"
pattern = "~r me@example.invalid & ~U & ~d <14d & ! ~G Auto"

[[saved_searches]]
name    = "Old Heavy Mail"
pattern = "~A & ~d >180d"
```

Restart inkwell. Saved searches appear in the folders pane under a
"Saved Searches" section. Press Enter on one to run it; `;d` / `;a`
work on the matches.

The DB-backed `:savedsearch new/edit/delete` commands ship in v0.7.x;
for now, edit the config file and restart.

## Find a specific email by content

Press `/`. Type a few words from the subject or body. Enter. The list
narrows to FTS hits from your local cache.

For more structured queries (from a specific sender, in a date range,
with attachments) use `:filter` instead.

## Open the calendar

`:cal` → modal shows today's events: time range, subject, organizer,
location/online-meeting link. `j`/`k` walk the events; `Enter` on
a focused event opens a detail modal showing attendees (with
accepted/tentative/declined glyphs), the body preview, and the
meeting URL. From the detail modal, `o` opens the event in Outlook,
`l` joins the online meeting, `Esc` returns to the list.

`Calendars.Read` only — to accept, decline, or modify, use Outlook.

## Toggle out-of-office (auto-reply)

`:ooo on` enables automatic replies immediately. `:ooo off` disables.
Both are single-step, no modal — useful for quick toggles.

`:ooo` (plain) opens the full editing modal where you can set Status,
Audience (All / Contacts only / None), and preview the internal and
external reply messages. `Tab` / `Shift+Tab` move between fields;
`Space` cycles the Status radio; `Enter` saves; `Esc` cancels.

## Set out-of-office with a schedule

1. Run `:ooo schedule` to open the editing modal with "On with schedule"
   pre-selected.
2. `Tab` to the Start date field; type the date in `YYYY-MM-DD` form.
3. `Tab` to Start time and type `HH:MM`.
4. Repeat for End date and End time.
5. `Tab` to Audience and `Space` to pick who receives the auto-reply.
6. Press `Enter` to save.

Graph will activate auto-replies at the start time and deactivate at
the end time. The status bar shows `🌴 OOO` whenever replies are active
(configurable via `[mailbox_settings].ooo_indicator`).

## Reply to a message

Open a message in the viewer (Enter). Press `r`.

The compose pane opens, pre-filled:

```
  To:       bob@vendor.com
  Cc:
  Subject:  Re: Q4 forecast

▶ Body:
  <cursor>

  On Mon 2026-04-29 14:32, Bob <bob@vendor.com> wrote:
  > Hey team, see attached.
  > …

  Ctrl+S / Esc save  ·  Ctrl+D discard  ·  Tab cycle field
```

Type your reply. The `▶` marks the focused field; `Tab` cycles
between Body / To / Cc / Subject if you need to fix any header.
When you're done, press `Ctrl+S` (or `Esc` — they do the same
thing). inkwell creates the draft on the server (`Mail.ReadWrite`)
and shows `✓ draft saved · press s to open in Outlook` in the
status bar.

Press `s` (in Normal mode) to open the draft in your browser /
Outlook desktop, where you finalise send. inkwell does not send
mail — see [explanation](explanation.md#why-no-send).

**Discard a draft**: press `Ctrl+D` while in compose. No Graph
round-trip; form state is dropped.

**Cleared the `To:` line by accident?** Save still works —
inkwell falls back to the original sender's address (you pressed
Reply, the original sender is the implicit recipient). For new
messages or forwards where there's no source to fall back on, an
empty `To:` produces an actionable error and your draft stays in
the form so you can correct and retry.

Reply-all and forward (`R` / `f` in the viewer) and new message
(`m`) land in a follow-up. `Ctrl+E` to drop the body into
`$EDITOR` for vim/emacs power users is also a follow-up.

## Copy a URL from a message

Open the message in the viewer pane (Enter from the list). Then:

- **Single URL** — press `y`. The URL is on your clipboard.
- **Multiple URLs** — press `O` to open the URL picker. Use `j` /
  `k` to move the cursor; `y` copies the highlighted URL; `Enter`
  or `O` opens it in your default browser; `Esc` / `q` close.

Inkwell delivers the copy via OSC 52 — the standard terminal
clipboard protocol — so it works over SSH on iTerm2, WezTerm,
Kitty, Ghostty, foot, Alacritty, Windows Terminal, and recent
GNOME Terminal / Konsole. On macOS, inkwell additionally pipes
through `pbcopy` so Apple Terminal users (which doesn't support
OSC 52) still get the local clipboard.

If you're inside tmux, enable OSC 52 passthrough once:

```sh
echo 'set -g set-clipboard on' >> ~/.tmux.conf
```

## Select multiple lines of body text to copy

Terminals do their own click-drag selection in a rectangular
shape. The three-pane layout means a normal drag spills across
pane borders — selection captures the folder list and message
list as well as the body. To select cleanly across multiple body
rows:

1. Open the message (Enter from the list).
2. In the viewer pane, press `z` — folders + list panes
   disappear, the body uses the full terminal width.
3. Click-drag the text you want. (On macOS, hold `Option` while
   dragging if you want to bypass tmux/screen mouse interception
   too.)
4. Cmd-C / Ctrl-Shift-C copies the selected text via the
   terminal, the same as anywhere else.
5. Press `z` (or `Esc`) to return to the three-pane view.

This is the same workflow neomutt's `pager.full` mode and aerc's
fullscreen view ship for the same reason.

## Read a thread with many attendees

Open a message in the viewer pane. By default the headers row shows
From / Date / Subject + the first 3 recipients with "+ N more". On a
20-attendee thread the body gets the screen real estate.

Press `H` (capital) to expand the full To / Cc / Bcc lines. Press
`H` again to collapse. Mutt convention.

## Switch themes

```toml
[ui]
theme = "solarized-dark"
```

Choices: `default`, `dark`, `light`, `solarized-dark`,
`solarized-light`, `high-contrast`. Unknown names fall back to
`default` with a warning logged.

Restart inkwell after editing.

## Force a sync now

`Ctrl+R`, or `:sync`.

The engine syncs every 30 seconds in the foreground anyway, but
`Ctrl+R` is useful when you've just sent something from another
client and want to see it immediately.

## Script your inbox from the shell

inkwell ships a non-interactive subcommand surface alongside the
TUI. Useful when you want to chain it through `jq`, `fzf`, or a
periodic cron job.

```sh
# One-shot sync, no UI.
inkwell sync

# List the 20 most recent unread messages in Inbox.
inkwell messages --folder Inbox --unread --limit 20

# Pattern-based dry-run (no changes).
inkwell filter '~f newsletter@* & ~d <30d'

# Same pattern, applied destructively (with confirm prompt).
inkwell filter '~f newsletter@* & ~d <30d' --action delete --apply

# Skip the prompt for cron / scripts.
inkwell filter '~f newsletter@*' --action archive --apply --yes

# JSON output piped into jq.
inkwell messages --folder Inbox --output json | jq '.[].subject'
inkwell filter '~A' --output json | jq '.matched'
```

The full subcommand reference is in
[reference.md](reference.md#cli-subcommands-non-interactive).
Drafts (`reply` / `forward`), calendar, OOO, and saved-search CRUD
are coming in v0.10+.

## Wipe the local cache (e.g. troubleshooting)

```sh
rm -rf ~/Library/Application\ Support/inkwell/inkwell.db*
```

Tokens stay in Keychain; re-auth isn't needed. The next launch
re-syncs from scratch.

## When sign-in fails

| Error                                | What it means                                                  | Fix                                                              |
| ------------------------------------ | -------------------------------------------------------------- | ---------------------------------------------------------------- |
| `AADSTS50105` / "user not assigned"  | Your tenant has Conditional Access blocking the public client. | Talk to IT — they need to grant the Microsoft Graph CLI Tools client to your account. |
| Browser opens but doesn't close      | The localhost listener got blocked.                            | Check macOS firewall; try `inkwell signin --device-code`.        |
| "data passed to Set was too big"     | Keychain rejected the MSAL cache (>4 KB token bundle).         | Update to v0.1.3+; this was fixed by switching to AES-GCM-on-disk encryption. |

For other auth errors, check `~/Library/Logs/inkwell/inkwell.log` —
the error code there is what to share with IT.

## When triage reports an error

If `r`/`d`/`a` etc. fails, the top-right shows `ERR: <reason>`. The
local change is rolled back automatically — your view stays
consistent with the server. The next sync cycle will retry pending
actions; if it's a hard failure (auth revoked, permission denied),
the action stays Failed in the queue and never re-fires.

## When the list "runs out" of messages

You scroll to the bottom; nothing new loads. Likely cause: the
local cache is exhausted at the current limit. inkwell auto-kicks a
sync when you hit the cache wall — wait a few seconds and the engine
backfills more from Graph. If that still doesn't help, `Ctrl+R`
forces a full sync cycle.

## Save or open an attachment

Open the message in the viewer (Enter from the list). If the message
has attachments, an `Attach:` block appears between the headers and
the body with one line per attachment, each prefixed by an accelerator
letter — `[a]`, `[b]`, etc.

- **Save to `~/Downloads`** — press the accelerator letter (e.g. `a`
  for the first attachment). A progress note appears in the status bar;
  `✓ saved → ~/Downloads/file.pdf` confirms success.
- **Open with your default app** — press Shift+letter (e.g. `A`).
  inkwell downloads to a temp directory, then calls `open <file>`
  (macOS) or `xdg-open <file>` (Linux) to hand off to the app
  registered for that MIME type.
- **Large files (>25 MB by default)** — a confirmation modal appears
  first. Confirm with `y`; cancel with `n`. The threshold is
  `[rendering].large_attachment_warn_mb` in `config.toml`.

The save directory defaults to `~/Downloads`. Override it with
`[rendering].attachment_save_dir = "/your/path"` in `config.toml`.

## Navigate a conversation thread

When a message belongs to a multi-message conversation (same email
chain), a `Thread (N messages)` block appears at the bottom of the
viewer body. The currently-displayed message is marked with `▶`.

- **Previous in thread** — press `[` (left bracket). The viewer
  switches to the chronologically older sibling.
- **Next in thread** — press `]` (right bracket). The viewer switches
  to the chronologically newer sibling.

The thread view loads from the local SQLite cache — it's instant and
offline-safe. If the conversation has more messages than are cached
locally, only the locally-synced subset appears. `Ctrl+R` syncs more.

## Open a message in Outlook (webLink)

Press `o` (lowercase) while the viewer pane is focused. inkwell opens
the OWA deep-link for the message in your default browser — useful
when a message has heavy CSS/images that the plain-text renderer can't
represent faithfully, or when you need to see the original formatting.

The webLink is populated when the message is synced from Graph. If the
message was just synced and the link hasn't arrived yet, the status bar
shows `open: no webLink for this message` — `Ctrl+R` and try again.

## Mute a noisy thread

When a thread is sending too many notifications but you don't want to
delete it, press `M` (Shift+m) in the list pane or viewer pane.

- The thread disappears from your normal folder view immediately.
- All future messages in that conversation still arrive and are cached
  locally — they just won't surface in the regular list.
- To un-mute, navigate to **🔕 Muted** in the sidebar, open one of
  the messages, and press `M` again. The thread returns to its folder.
- The `🔕 Muted` sidebar entry appears only when at least one
  conversation is muted; it shows the count of distinct muted threads.

**Find all muted threads:** navigate to `🔕 Muted` in the sidebar.
inkwell loads all muted messages ordered by when they were muted
(newest mute first).

**Intentional search includes muted:** pressing `/` and searching
always includes muted threads — if you explicitly searched, you want
to see the result.

**CLI:**

```sh
inkwell mute <conversation-id>
inkwell mute --message <message-id>   # resolves via local store
inkwell unmute <conversation-id>
```

---

## Triage an entire thread

When you want to act on every message in a conversation at once, use the
`T` chord in the messages pane or viewer pane. Press `T` — the status bar
shows the available second keys:

```
thread: r/R/f/F/d/D/a/m  esc cancel
```

Then press a second key within 3 seconds:

| Second key | What happens |
| ---------- | ------------ |
| `r`        | Mark all messages in the thread read |
| `R`        | Mark all messages in the thread unread |
| `f`        | Flag every message |
| `F`        | Unflag every message |
| `d`        | Soft-delete the thread (confirm required, default N) |
| `D`        | Permanently delete the thread (confirm required, **irreversible**) |
| `a`        | Archive the whole thread (no confirm) |
| `m`        | Move the whole thread — opens the folder picker |
| `Esc`      | Cancel the chord |

The chord automatically cancels after 3 seconds with no second key.

Messages in your **Drafts**, **Deleted Items**, and **Junk** folders are
excluded from thread operations — acting on a draft or a trashed message
is generally not what you intend. To include them, use the CLI with the
conversation ID directly.

**Partial failure:** if some messages fail server-side (e.g., Graph
throttle), the status bar shows `⚠ archive thread: 11/12 succeeded — 1 failed`.
The successfully acted-on messages are in their final state; the failed ones
retain their previous state.

**CLI equivalents:**

```sh
# Archive an entire thread by conversation ID
inkwell thread archive <conversation-id>

# Mark a thread read / unread
inkwell thread mark-read <conversation-id>
inkwell thread mark-unread <conversation-id>

# Flag / unflag
inkwell thread flag <conversation-id>
inkwell thread unflag <conversation-id>

# Soft-delete (dry-run without --yes)
inkwell thread delete <conversation-id>
inkwell thread delete <conversation-id> --yes

# Permanent delete (irreversible, dry-run without --yes)
inkwell thread permanent-delete <conversation-id>
inkwell thread permanent-delete <conversation-id> --yes

# Move to a folder (resolved by display name)
inkwell thread move <conversation-id> --folder "Archive"

# JSON output for scripting
inkwell thread archive <conversation-id> --output json
```

---

## Discover and learn keybindings using the palette

The TUI has a lot of bindings. The fastest way to find one is the
**command palette** (`Ctrl+K`).

```
Ctrl+K           # opens the palette anywhere in Normal mode
arch             # type to fuzzy-match — first match auto-selected
Enter            # run the highlighted action

# Sigils scope the search:
#inbox           # `#` → folders only; selecting jumps the list pane
@receipts        # `@` → saved searches only; selecting runs the filter
>archive         # `>` → commands only; rules out folder name matches
```

The right-hand column on every row shows the live keybinding for
that action — glance at it once, use the shortcut next time. The
palette itself doubles as a cheatsheet. Recently-used commands
surface first when the buffer is empty (`Ctrl+K` → `Enter`), so
your common actions are always one keystroke away.

For commands that take an argument (move to folder, set a category,
type a filter pattern, jump to a folder by name), press `Tab`
instead of `Enter` — the palette closes and hands off to the
existing argument flow (folder picker / category input /
command-bar pre-fill), so you finish the command in the same way
you would by typing it directly.

---

_Last reviewed against v0.9.0._
