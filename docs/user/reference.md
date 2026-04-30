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

Saved searches (configured in `[[saved_searches]]`) show under a
"Saved Searches" section with a `☆` glyph. Enter on one runs its
pattern via the filter machinery.

## Messages pane (when focused)

| Key       | Action                                                        |
| --------- | ------------------------------------------------------------- |
| `j` / `↓` | Cursor down (auto-paginates near the bottom)                  |
| `k` / `↑` | Cursor up                                                     |
| `Enter`   | Open message in viewer (focus → viewer)                       |
| `r`       | Mark read                                                     |
| `R`       | Mark unread                                                   |
| `f`       | Toggle flag                                                   |
| `d`       | Soft-delete (move to Deleted Items)                           |
| `a`       | Archive (move to Archive folder)                              |
| `;`       | Begin bulk chord (only when a filter is active)               |
| `;d`      | Bulk delete the filtered set (with confirm)                   |
| `;a`      | Bulk archive the filtered set (with confirm)                  |
| `U`       | Unsubscribe (RFC 8058 / mailto / browser; with confirm)       |
| `u`       | Undo the most recent triage action (mark, flag, delete, archive) |
| `/`       | Enter search mode                                             |

**Smart-scroll**: when you reach the last 20 messages of the loaded
slice, the next page (200 rows) loads from the local store
automatically. When the cache is exhausted, inkwell kicks a sync to
pull more from Graph.

**Calendar-invite indicator**: messages whose subject starts with
`Accepted:`, `Declined:`, `Tentative:`, `Updated:`, `Canceled:`,
`Meeting:`, or `Invitation:` show a leading `📅` glyph.

## Viewer pane (when focused)

| Key       | Action                                                        |
| --------- | ------------------------------------------------------------- |
| `j` / `↓` | Scroll body down                                              |
| `k` / `↑` | Scroll body up                                                |
| `h` / `←` | Back to messages pane                                         |
| `H`       | Toggle compact / full headers (To/Cc/Bcc expansion)           |
| `r`       | Reply (opens `$INKWELL_EDITOR` / `$EDITOR` / nano with a draft skeleton) |
| `s`       | Open the most-recently-saved draft in Outlook (after `r` saves) |
| `f`       | Toggle flag (focus stays — flag, keep reading)                |
| `d`       | Soft-delete (focus pops back to list)                         |
| `a`       | Archive (focus pops back to list)                             |
| `U`       | Unsubscribe (RFC 8058 / mailto / browser; with confirm)       |
| `u`       | Undo the most recent triage action                            |

**Compact headers** (default): only From / Date / Subject + first 3
recipients with "+ N more". On a 50-attendee thread, the body
gets the room. Press `H` to expand To / Cc / Bcc on their own
lines.

**Clickable URLs**: every URL in a rendered message body — inline
and in the trailing `Links:` block — is wrapped in OSC 8 hyperlink
escapes. Modern terminals (iTerm2, kitty, alacritty, foot, wezterm,
recent gnome-terminal / Konsole) make these directly clickable
(Cmd-click on macOS, Ctrl-click on Linux). Older terminals (Apple
Terminal.app) fall back to plain text — use the numbered `1`–`9`
keys to open links instead.

If your terminal doesn't support OSC 8 and you need to copy a long
URL, hold **Option** (macOS) or **Shift** (Linux) while drag-
selecting to limit the selection to the viewer pane region only.
Without the modifier, terminal selection is rectangular and spans
across panes.

**Reply flow** (`r`): inkwell writes a tempfile pre-populated with
To / Cc / Subject + a quoted-body skeleton, then opens it in your
editor. When you save and exit, inkwell parses the file, calls
Microsoft Graph `createReply` + `PATCH /me/messages/{id}` to update
body and headers, and stores a draft in your Drafts folder. The
status bar shows `✓ draft saved · press s to open in Outlook`. Press
`s` to launch the draft in your browser / Outlook desktop, where
you finalise send. inkwell never sends mail — see the
[explanation](explanation.md#why-no-send) for why.

`r` / `R` are reserved in the viewer for spec 15 (reply / reply-all)
and don't currently mark-read. Use the list pane for that.

## Command mode (`:`)

| Command                       | Effect                                                         |
| ----------------------------- | -------------------------------------------------------------- |
| `:quit` / `:q`                | Exit                                                            |
| `:sync`                       | Trigger a sync cycle now                                        |
| `:signin`                     | Re-auth (opens system browser)                                  |
| `:signout`                    | Confirm modal → clears tokens + local cache                     |
| `:filter <pattern>`           | Narrow message list to pattern matches                          |
| `:unfilter`                   | Clear active filter, restore prior folder                       |
| `:cal` / `:calendar`          | Open today's calendar in a modal                                |
| `:ooo` / `:outofoffice`       | Open the out-of-office modal (view + toggle on/off)             |
| `:unsub` / `:unsubscribe`     | Unsubscribe from the focused message (same flow as `U` keybinding) |

Plain-text patterns without a `~` operator are treated as a CONTAINS
search across subject and body (`~B *<text>*`). `:filter [External]`
matches any message whose subject or body contains `[External]`.

## Search mode (`/`)

| Key            | Action                                                         |
| -------------- | -------------------------------------------------------------- |
| `<text> Enter` | Run FTS query, replace list pane with hits                     |
| `Esc`          | Cancel; if a search is active, clear it and restore the folder |
| `Backspace`    | Delete the last character of the buffer                        |

Search is local-only (FTS5 against the SQLite cache) in v0.8.
Server-side `$search` merge is post-v0.8.

## Calendar mode (`:cal`)

| Key            | Action                                                         |
| -------------- | -------------------------------------------------------------- |
| `Esc` / `q`    | Close the modal, return to Normal mode                         |

Read-only. To act on an event, finish in Outlook.

## Out-of-office mode (`:ooo`)

| Key            | Action                                                         |
| -------------- | -------------------------------------------------------------- |
| `t`            | Toggle automatic-replies enable / disable                      |
| `Esc` / `q`    | Close the modal, return to Normal mode                         |

The modal shows current state and the existing internal-reply message
(read-only in v0.9.0). Toggling preserves the message; to edit the
message body, use Outlook for now.

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
| SignIn      | auth flow            | `Esc`                                            |
| Confirm     | destructive prompts  | `y` (confirm) or `n` / `Esc` (cancel)            |
| Calendar    | `:cal` / `:calendar` | `Esc` or `q`                                     |
| OOO         | `:ooo` / `:oof` / `:outofoffice` | `Esc` or `q` (`t` toggles)            |

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
| `inkwell message show <id>`                      | Print headers + body for one message.                     |
| `inkwell message show <id> --headers`            | Include full To / Cc / Bcc.                               |
| `inkwell filter '<pattern>'`                     | Print matched envelopes (dry-run).                        |
| `inkwell filter '<pattern>' --action delete --apply`   | Bulk soft-delete via Graph $batch.                  |
| `inkwell filter '<pattern>' --action archive --apply`  | Bulk archive.                                       |
| `inkwell filter '<pattern>' --action mark-read --apply`| Bulk mark-read.                                      |

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

[account]
upn = "you@example.invalid"  # optional safety check

[[saved_searches]]
name    = "Newsletters"
pattern = "~f newsletter@* | ~f noreply@*"
```

Restart inkwell after editing.

---

_Last reviewed against v0.8.0._
