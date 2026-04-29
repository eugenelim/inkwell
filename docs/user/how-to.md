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

`f` / `d` / `a` also work in the **viewer pane** (so you can read,
decide, delete without going back). Delete and archive pop you back
to the list automatically — you immediately see what's next. Mark-
read and flag stay in place.

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
location/online-meeting link. Esc to close.

`Calendars.Read` only — to accept, decline, or modify, use Outlook.

## Toggle out-of-office (auto-reply)

`:ooo` opens a modal showing your current auto-reply state and the
configured internal-reply message. Press `t` to flip enabled /
disabled — the existing message is preserved server-side, so you're
not editing the body, just turning it on or off. Esc closes.

To edit the actual message body, set a schedule, or differentiate
internal vs external audiences, use Outlook for now (those edits
land in a later iteration).

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
