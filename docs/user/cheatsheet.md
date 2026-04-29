# inkwell cheat sheet

Every keybinding and command in the current release. For narrative
context, see [`guide.md`](guide.md).

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

---

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

---

## Messages pane (when focused)

| Key       | Action                                                        |
| --------- | ------------------------------------------------------------- |
| `j` / `↓` | Cursor down (auto-paginates near the bottom — see below)      |
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
| `/`       | Enter search mode                                             |

**Smart-scroll:** when you reach the last 20 messages of the loaded
slice, the next page (200 rows) loads from the local store
automatically.

---

## Viewer pane (when focused)

| Key       | Action                                                        |
| --------- | ------------------------------------------------------------- |
| `j` / `↓` | Scroll body down                                              |
| `k` / `↑` | Scroll body up                                                |
| `h` / `←` | Back to messages pane                                         |
| `f`       | Toggle flag (focus stays — flag, keep reading)                |
| `d`       | Soft-delete (focus pops back to list)                         |
| `a`       | Archive (focus pops back to list)                             |

`r` / `R` are reserved in the viewer for spec 15 (reply / reply-all)
and don't currently mark-read. Use the list pane for that.

---

## Command mode (`:`)

| Command                       | Effect                                                         |
| ----------------------------- | -------------------------------------------------------------- |
| `:quit` / `:q`                | Exit                                                            |
| `:sync`                       | Trigger a sync cycle now                                        |
| `:signin`                     | Re-auth (opens system browser)                                  |
| `:signout`                    | Confirm modal → clears tokens + local cache                     |
| `:filter <pattern>`           | Narrow message list to pattern matches (see below)              |
| `:unfilter`                   | Clear active filter, restore prior folder                       |

Plain-text patterns without a `~` operator are treated as `~B <text>`
(subject or body contains).

---

## Search mode (`/`)

| Key            | Action                                                         |
| -------------- | -------------------------------------------------------------- |
| `<text> Enter` | Run FTS query, replace list pane with hits                     |
| `Esc`          | Cancel; if a search is active, clear it and restore the folder |
| `Backspace`    | Delete the last character of the buffer                        |

Search is local-only (FTS5 against the SQLite cache) in v0.6 / v0.7.
Server-side `$search` merge is post-v0.7.

---

## Pattern operators

Argument-bearing (the most common):

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

---

## Indicators

| Glyph              | Meaning                                                         |
| ------------------ | --------------------------------------------------------------- |
| `▌ <Title>`        | Pane is focused                                                 |
| `▶`                | Cursor on this row, focused pane                                |
| `· `               | Cursor on this row, unfocused pane                              |
| `▾` / `▸`          | Folder expanded / collapsed                                     |
| `☆`                | Saved search                                                    |
| `✓ synced HH:MM`   | Last sync time (top-right)                                      |
| `syncing folders…` | Engine is working                                               |
| `⏳ throttled Ns`  | Graph is rate-limiting; engine backing off                      |
| `ERR: …`           | Last error; full text in the log file                           |
| `✓ <action> N`     | Bulk op succeeded for N messages                                |
| `⚠ <action> X/Y`   | Bulk op partial — X succeeded, X+Y attempted                    |

---

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

_Cheatsheet last reviewed against v0.7.0._
