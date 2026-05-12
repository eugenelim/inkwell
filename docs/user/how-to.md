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

---

## Discover bindings and actions with the command palette

Press `Ctrl+K` from any pane to open the command palette. Type any
fragment of a command name, folder, or saved search — the palette
fuzzy-matches across all of them and shows the keybinding on the right.

- Type `>` to narrow to commands only.
- Type `#` to narrow to folders only.
- Type `@` to narrow to saved searches only.
- `↑` / `↓` (or `Ctrl+P` / `Ctrl+N`) move the cursor; `Enter` runs;
  `Esc` dismisses without action.

Recently used commands float to the top automatically, so the palette
doubles as a cheatsheet that learns your workflow.

---

## Route a sender to Imbox / Feed / Paper Trail / Screener

Routing assigns a sender to a stream once; all future mail from them
lands there automatically.

**From the TUI** (list or viewer pane, focused on the sender's message):

1. Press `S` to start the stream chord.
2. Press the destination: `i` Imbox · `f` Feed · `p` Paper Trail ·
   `s` Screener · `c` clear routing.

A status toast confirms. The sender's messages appear under the
matching stream entry in the sidebar.

**From the CLI:**

```sh
inkwell route assign alice@example.com feed
inkwell route list
inkwell route show alice@example.com
inkwell route clear alice@example.com
```

**Pattern filter — find all Feed-routed messages:**

```sh
inkwell filter '~o feed'
inkwell filter '~o feed' --output json | jq '.[] | .subject'
```

---

## Turn on the Screener (HEY-style first-contact gate)

The Screener (spec 28) hides mail from senders you haven't decided
about. Off by default — flipping it on without doing a routing pass
first will hide most of your Inbox until you start admitting senders.

**Recommended sequence:**

1. **Do a routing pass first.** Walk your Inbox, press `S i` /
   `S f` / `S p` / `S k` to route the senders you recognise. The
   `inkwell route list` CLI gives a quick summary of what you've
   covered so far.
2. **Pre-approve in bulk if you have a contacts dump.** `inkwell
   screener pre-approve --from-file ~/contacts.txt` reads one
   address per line (`#` comments and blank lines OK) and admits
   each to Imbox. Use `--to feed` or `--to paper_trail` if those
   contacts are noisier than Inbox-worthy.
3. **Edit `~/Library/Application Support/inkwell/config.toml`** and
   add:
   ```toml
   [screener]
   enabled = true
   ```
4. **Relaunch inkwell.** On the next launch, if there are any
   pending senders, a confirmation modal renders before the first
   list-pane render: `Enable Screener? This will hide N messages
   from M senders…` Press `Y` to proceed; `N` keeps the gate off
   for this session and re-prompts next launch.
5. **Decide from the queue.** Navigate to the Screener virtual
   folder (sidebar — between Paper Trail and Screened Out), focus
   a sender, press `Y` to admit them to Imbox or `N` to screen them
   out. The row vanishes from the queue; the cursor falls to the
   next address-different row. The chord shortcuts (`S i` / `S f` /
   `S p`) still work for finer-grained admission.

**Once admitted**, the sender's past and future mail flows where
you said. **Once screened out**, their mail stays cached and
searchable but disappears from default folder views. The
`__screened_out__` sentinel folder is the recovery surface — see
the next recipe.

---

## Pre-approve senders from a contacts dump

Easiest way to bootstrap the Screener if you have a CSV / TXT
file of email addresses (e.g. exported from your address book).

```sh
# One address per line. # comments and blank lines are skipped.
# Display-name forms ("Bob" <bob@x.com>) are rejected per-line.
inkwell screener pre-approve --from-file ~/contacts.txt

# Alternatively, pipe stdin:
cat contacts.txt | inkwell screener pre-approve --from-stdin

# Default destination is imbox. Override per batch:
inkwell screener pre-approve --from-file ~/newsletters.txt --to feed
```

Partial-success is exit 0; all-fail is exit 2. The stderr summary
prints `pre-approved N admitted, M skipped (errors above)` so you
can pipe stderr to a log and inspect it for malformed lines.

---

## Recover from a wrong Screener decision

The Screener's `Y` / `N` decisions are not in the `u` (undo) stack
— routing assignments are synchronous direct writes that bypass
the action queue (spec 23 §6). The recovery path mirrors HEY's
Screener History affordance:

1. Navigate to the **Screened Out** virtual folder in the sidebar
   (visible only when `[screener].enabled = true`). All mail from
   senders you screened out lives here.
2. Focus the offending sender's mail.
3. Press `S c` to clear their routing decision (or `S i` / `S f` /
   `S p` to re-route somewhere else).
4. The sender returns to whichever state matches the new decision
   — Pending (no row), Imbox/Feed/Paper Trail (admitted), or
   Screened Out (different from before).

If you can't remember which sender you screened out, run `inkwell
screener history` from the shell — it lists every screener-routed
sender with the date of the decision.

---

## Set up split inbox tabs

Tabs divide the list pane into named focus areas, each backed by a
saved search pattern. Useful for processing newsletters, VIP mail, or
project-specific mail in separate passes.

**Add tabs from the command bar:**

```
:tab add Newsletters ~f newsletter@*
:tab add VIP ~r alice@example.com | ~r bob@example.com
:tab add Receipts ~o paper_trail
```

`]` / `[` cycle tabs when the list pane is focused. The tab strip
appears above the list automatically once any tabs exist.

**Manage tabs from the CLI:**

```sh
inkwell tab list
inkwell tab add "Receipts" "~o paper_trail"
inkwell tab move "Receipts" 1          # make it the first tab
inkwell tab remove "Newsletters"
```

**Tips:**
- `:filter` while a tab is active AND's with the tab's pattern —
  you search only within that focus area.
- `[tabs] show_zero_count = true` keeps empty tabs visible.
- `[tabs] enabled = false` hides the strip entirely.

---

## Use Reply Later and Set Aside stacks

**Reply Later** (`L`) queues a message to reply to later. **Set
Aside** (`P`) pins a message for reference. Both use Graph categories
stored server-side, so they persist across devices.

**Toggle from the TUI** (list or viewer pane):

- `L` — add/remove focused message from Reply Later (↩)
- `P` — add/remove focused message from Set Aside (📌)
- The sidebar shows Reply Later and Set Aside entries with their
  counts when non-zero; `Enter` on either loads the stack.

**Apply to a whole thread** (T-chord):

- `T l` / `T L` — add / remove entire thread from Reply Later
- `T s` / `T S` — add / remove entire thread from Set Aside

**Bulk** (requires an active filter, `;` chord):

- `;l` / `;L` — bulk add / remove the filtered set to Reply Later
- `;s` / `;S` — bulk add / remove the filtered set to Set Aside

**Focus mode** — work through Reply Later one message at a time:

Press `:focus` (or `Enter` on the Reply Later sidebar entry then
`:focus`). The first Reply Later message opens. Reply (or archive /
delete), and when the compose pane closes the next message loads
automatically. The status bar shows `[focus N/M]`. Press `Esc` to
exit at any time.

**From the CLI:**

```sh
inkwell later list
inkwell later add <message-id>
inkwell later remove <message-id>
inkwell later clear                    # empties the stack (with confirm)

inkwell aside list
inkwell aside add <message-id>
inkwell aside clear
```

---

## Bundle a noisy newsletter sender

When a single sender (newsletter, recruiter spam, build bot) sends
many emails, a bundle collapses runs of consecutive same-sender
messages into one row in the list pane. Bundles are per-sender
opt-in: nothing is bundled until you designate it. Spec 26.

**Designate a sender:**

1. Focus the list pane (`2`).
2. Move the cursor onto any message from the sender you want to bundle.
3. Press `B`.

The status bar shows `▸ bundled <addr> — collapses N messages`. The
runs of N≥2 consecutive same-sender messages collapse to a single
header row showing `(N) — <latest subject>`.

If the address only appears once in the current view, the toast
reads `no consecutive run in current view; will collapse on next
match` — the designation is saved and applies as soon as a run
appears.

**Expand / collapse a bundle:**

- `Space` on the bundle header toggles expand/collapse. The cursor
  stays on the header.
- `Enter` on a *collapsed* header expands the bundle and leaves the
  cursor on the header. Press `Enter` again to open the
  representative (newest member) in the viewer.
- `Space` on a bundle *member* collapses the parent and lands the
  cursor on the now-collapsed header.

**Un-designate a sender:**

Press `B` again on the bundle header (or any flat row from the same
sender). The bundle dissolves into flat rows in place; the toast
reads `flat <addr> (was bundled)`.

**Act on the whole bundle as one unit (the canonical workflow):**

Single-message verbs (`d`, `a`, `f`, `T r`) on a bundle row target
the *representative* (newest member), not the whole bundle. To act
on every member at once, filter to the sender, then bulk-apply:

```
:filter ~f news@acme.com
;d        # delete every message in the filter result (12 messages,
          # not 1 row — the modal shows the true count)
```

The `;d` confirm modal text reflects the filter's true message
count, even if the filter result is currently rendered as a single
bundle row.

**From the CLI:**

```sh
inkwell bundle add news@acme.com
inkwell bundle remove news@acme.com
inkwell bundle list
inkwell bundle list --output json
```

CLI changes apply on the next `Ctrl+R` refresh inside a running TUI.

---

## Author a custom action

Custom actions chain primitive ops into a single named verb (spec 27). Recipes live in `~/.config/inkwell/actions.toml`; the file is loaded once at startup. Edits require a binary restart (the `:actions reload` shortcut is intentionally not in v1.1; iterate via `inkwell action validate`).

**Three example recipes** to drop into your `actions.toml`:

```toml
# 1) Newsletter triage: mark read, route the sender to Feed, archive,
#    advance to the next row.
[[custom_action]]
name = "newsletter_done"
key = "n"
description = "Newsletter triage: mark read, route to Feed, archive."
sequence = [
  { op = "mark_read" },
  { op = "set_sender_routing", destination = "feed" },
  { op = "archive" },
  { op = "advance_cursor" },
]

# 2) Move every message from this sender into a folder you name now.
[[custom_action]]
name = "sender_to_folder"
key = "T"
description = "Move all-from-sender to a folder I'll name."
confirm = "always"
sequence = [
  { op = "prompt_value", prompt = "Move all from {{.From}} to folder:" },
  { op = "move_filtered", pattern = "~f {{.From}}", destination = "{{.UserInput}}" },
]

# 3) Add the focused thread to Reply Later.
[[custom_action]]
name = "reply_later_thread"
key = "L"
description = "Add the entire thread to Reply Later."
sequence = [
  { op = "thread_add_category", category = "Inkwell/ReplyLater" },
]
```

**Workflow:**

1. Edit `~/.config/inkwell/actions.toml`.
2. Run `inkwell action validate` — surfaces every parse / validation error with file:line.
3. Restart inkwell.
4. Press the action's `key`, or run `:actions run <name>`, or pick it from the palette (`Ctrl+K` → "Custom actions" section).

**Confirm policy:** the default `auto` prompts before any sequence containing a destructive op (`permanent_delete*`) or a `*_filtered` step. `confirm = "always"` always prompts; `confirm = "never"` runs immediately (rejected at load when paired with a destructive op).

**Templating safety:** templates that reference `{{.From}}`, `{{.SenderDomain}}`, etc. inside a `move` destination require `allow_folder_template = true` on the action. Templates inside an `open_url` URL require `allow_url_template = true` (PII exfil guard).

**Undo:** `u` reverses queue-routed steps one at a time, in dispatch order. `set_sender_routing` and `set_thread_muted` are synchronous direct writes and are NOT reversible by `u`; the result toast flags them with `[non-undoable]` so you know what stays applied. To "undo" routing, re-route via `S` or `:route clear`.

**Run against many messages from the shell.** `inkwell action run <name> --filter '<pattern>'` runs the named action against every message that matches the pattern, capped by `[bulk].size_hard_max` (default 5000) so an over-broad pattern does not enqueue tens of thousands of operations. Actions whose templates reference per-message variables (`{{.From}}`, `{{.Subject}}`, etc.) are rejected — `--filter` mode has no single focused message to pull values from. Use `--filter` for actions whose first step is `filter` or whose only steps are `*_filtered` ops. Example:

```sh
# Archive every newsletter older than 30 days via the named recipe.
inkwell action run cleanup_newsletters --filter '~f *@newsletter.* & ~d <30d'
```

---

## Tail your inbox like `tail -f`

`inkwell messages --filter X --watch` (spec 29) streams new matches
to stdout as they arrive, then keeps running until you stop it.
Composes with shell pipelines the same way every other inkwell CLI
command does — `| jq`, `| head`, `| tee`, `| xargs`.

**Tail VIP unread to the terminal:**

```sh
inkwell messages --filter '~U & ~f vip@example.com' --watch
# (Ctrl-C exits 0 with a one-line summary on stderr.)
```

**Pipe IDs to a downstream consumer:**

```sh
inkwell messages --filter '~U & ~f vip@*' --watch --output json \
  | jq -r '.ID' | while read id; do echo "ping for $id"; done
```

**Use a saved search:**

```sh
inkwell messages --rule VIPs --watch
```

**Print the last 10 matches then keep watching:**

```sh
inkwell messages --filter '~U' --watch --initial=10
```

**Wait for the next 3 messages from Bob, then exit:**

```sh
inkwell messages --filter '~f bob@*' --watch --count 3
```

**Cron-friendly: every cron run, watch for 60s, then exit:**

```cron
* * * * * /usr/local/bin/inkwell messages --filter '~U' --watch --for 55s --quiet >> ~/inkwell-vip.log 2>&1
```

If you redirect stdout to a file (as above), set `umask 077`
**before launching** the watch — the redirected file inherits
the user's umask (typically `022` → world-readable). Inkwell's
own log file at `~/Library/Logs/inkwell/` is created with mode
`0600` regardless; only the user-supplied stdout redirection is
governed by the launching shell's umask.

```sh
( umask 077 && inkwell messages --filter '~U' --watch >> ~/inkwell-vip.log )
```

**Read-only watcher when a daemon (or the TUI) is running:**

```sh
inkwell daemon &
inkwell messages --filter '~U' --watch --no-sync   # only the daemon syncs
```

**JSONL output format.** `--output json` emits *one JSON object
per line* (no array wrapper) — well-suited for piping into `jq -r`
or any line-oriented stream consumer. The one-shot
`inkwell messages --output json` (without `--watch`) still emits
a single JSON array; the divergence is intentional and pinned by
tests.

**Exit codes** (spec 14 `internal/cli/exitcodes.go`): `0` clean
exit; `2` usage error (mutually-exclusive flags, no
`--filter`/`--rule`); `3` auth error after 10 minutes of
consecutive `AuthRequiredEvent`s with zero intervening sync
success; `5` folder or rule not found.

**Latency.** A new message lands in stdout after at most one
foreground sync interval (default 30 s) plus the per-cycle delta
fetch (~50–200 ms). For lower latency, run `inkwell sync` in a
sibling shell or keep `inkwell daemon` running — the watch
re-evaluates the local cache on every `SyncCompletedEvent`.

---

## Archive vs "done" — pick your vocabulary

`inkwell` ships with two default keys for the archive verb: `a`
(continuity with prior versions and Outlook conventions) and `e`
(matches Gmail / Inbox keyboard muscle memory). Both keys do the
same thing — move the focused message to your well-known
**Archive** folder and dispatch the optimistic local apply through
the action queue. Press `u` to undo (regardless of which key you
used).

If you prefer the HEY / Inbox **"done"** framing, flip a single
config switch and every user-visible Archive label in the app
rebrands:

```toml
[ui]
archive_label = "done"
```

After restart:

- The status-bar toast reads `✓ done · u to undo` (was
  `✓ archive · u to undo`).
- The palette title reads **Mark done** / **Mark thread done**
  (was **Archive message** / **Archive thread**); the binding
  column still shows `a, e`.
- The fullscreen body hint, filter status bar, bulk pending hint,
  list/viewer key hints, and help overlay all rebrand uniformly.
- Cmd-bar verbs work both ways regardless of the label —
  `:archive` and `:done` are aliases of the same dispatch.
- The CLI alias `inkwell thread done <conv-id>` works regardless
  of the label.

The underlying action, destination folder, undo path, and Graph
round-trip are **unchanged**. The choice is vocabulary, not
behaviour. CLI flag values (`--action archive`) and config keys
(`[triage].archive_folder`) keep the canonical `archive` spelling
because those are stable interface contracts, not user-facing
labels.

If you want only one of the two default keys, override
`[bindings].archive`:

```toml
[bindings]
archive = "a"   # only a archives; e is freed
# or:
archive = "e"   # only e archives; a is freed
```

## Show Focused / Other in the Inbox

Microsoft Graph classifies each Inbox message as **Focused** or
**Other** via the `inferenceClassification` property; that signal is
what Outlook desktop / web renders as the Focused tabs. inkwell
mirrors the split as a read-only sub-strip above the Inbox list — no
new schema, no new Graph scope, no per-message override.

Off by default. Flip one key:

```toml
[inbox]
split = "focused_other"
# optional:
# split_show_zero_count = true     # render `[Focused 0]` not `[Focused]`
# split_default_segment = "focused" # which segment ] / [ activate from cold start
```

Restart inkwell. On the Inbox folder you'll see a one-row strip:

```
 [Focused 12] [Other 47]
```

Cycle with `]` / `[`, or invoke the cmd-bar verbs `:focused` /
`:other` (always available — the verbs navigate to Inbox if needed).
Cursor and scroll position are preserved per segment across cycles.

The strip only paints when:

- `[inbox].split = "focused_other"`.
- The Inbox folder is selected (not Sent, Archive, a saved-search
  row, or a routing virtual folder).
- No spec-24 user-defined tab is active. If you also have
  user-defined tabs configured, `]` / `[` cycle THOSE; use
  `:focused` / `:other` to switch sub-tabs.
- No `:search` query is active.
- No `:filter --all` cross-folder filter is active. Plain
  `:filter <pattern>` (folder-scoped) is compatible — your pattern
  is AND'd with the sub-tab class predicate and the strip stays
  visible.

**Tenant with Focused Inbox disabled.** Some tenants turn Focused
Inbox off; Graph then returns an empty `inferenceClassification`
field and both segments show zero unread. inkwell renders a one-time
hint the first time the strip appears with both badges at zero and
the Inbox total unread non-zero:

> focused/other looks empty — your tenant may have Focused Inbox off
> (see [inbox].split docs)

Press Esc to dismiss; the hint never repeats in the same session.

**Screener interaction.** When `[screener].enabled = true`, the
sub-strip's default view hides screener-routed senders (mirroring
the unsplit Inbox folder). Running `:filter <pattern>` over an
active sub-tab does NOT apply the screener filter (per spec 28's
`:filter` contract) — the filter path can therefore "reveal" more
rows than the bare sub-tab view. Intentional.

**CLI sugar.** Outside the TUI:

```sh
inkwell messages --view focused                  # ~y focused over Inbox
inkwell messages --view other --filter '~d <7d'  # AND'd
```

`--view <unknown>` exits 2. `--view focused --folder Sent` also
exits 2 — the flag enforces the Inbox folder scope.

---

_Last reviewed against v0.60.0._
