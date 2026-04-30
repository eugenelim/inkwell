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

`:ooo` opens a modal showing your current auto-reply state and the
configured internal-reply message. Press `t` to flip enabled /
disabled — the existing message is preserved server-side, so you're
not editing the body, just turning it on or off. Esc closes.

To edit the actual message body, set a schedule, or differentiate
internal vs external audiences, use Outlook for now (those edits
land in a later iteration).

## Reply to a message

Open a message in the viewer (Enter). Press `r`.

inkwell suspends the TUI and opens your editor on a tempfile
pre-populated with:

```
To: bob@vendor.com
Cc:
Subject: Re: Q4 forecast


On Mon 2026-04-29 14:32, Bob <bob@vendor.com> wrote:
> Hey team, see attached.
> …
```

Edit the body. Save and exit. inkwell parses the file, creates the
draft on the server (`Mail.ReadWrite`), and shows `✓ draft saved ·
press s to open in Outlook` in the status bar.

Press `s` to open the draft in your browser / Outlook desktop, where
you finalise send. inkwell does not send mail — see
[explanation](explanation.md#why-no-send).

**Editor selection** order:
1. `INKWELL_EDITOR` env var (per-app override; e.g. `INKWELL_EDITOR=vim`)
2. `EDITOR` env var
3. `nano` as a portable fallback

**Discard a draft**: blank out the body in the editor and save (or
exit without saving). The empty file produces `ErrEmpty` and the
draft is dropped — no Graph round-trip.

**No recipients**: if you delete the `To:` line, the parse returns
`no recipients` and inkwell skips the round-trip with a friendly
error in the status bar.

Reply-all and forward (`R` / `f` in the viewer) are coming in v0.12.

## Copy a URL from a message

Open the message in the viewer pane (Enter from the list). Then:

- **Single URL** — press `y`. The URL is on your clipboard.
- **Multiple URLs** — press `o` to open the URL picker. Use `j` /
  `k` to move the cursor; `y` copies the highlighted URL; `Enter`
  or `o` opens it in your default browser; `Esc` / `q` close.

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

---

_Last reviewed against v0.8.0._
