# inkwell — user guide

A terminal-based Microsoft 365 mail client. This guide takes you from
"never run it" to "comfortable processing 200 messages before lunch".

For a one-glance reference of every key, see
[`cheatsheet.md`](cheatsheet.md).

---

## 1. Install

inkwell ships as a single signed (eventually) Mac binary. Until macOS
notarization lands, you'll see a Gatekeeper warning on first launch.

```sh
# Download the latest release.
gh release download <vX.Y.Z> -p '*macos_arm64*' -D /tmp
tar -xzf /tmp/inkwell_<vX.Y.Z>_macos_arm64.tar.gz -C /tmp

# Allow the unsigned binary to run.
xattr -d com.apple.quarantine /tmp/inkwell

# Optionally install to PATH.
sudo mv /tmp/inkwell /usr/local/bin/
```

Linux builds (amd64 / arm64) are also published on each release.

---

## 2. First sign-in

```sh
inkwell signin
```

The system browser opens to Microsoft's sign-in page. Enter your work
account; the browser closes itself when sign-in succeeds and inkwell
launches the TUI. On subsequent launches, just `inkwell run` — your
token cache lives in macOS Keychain (encrypted) and refreshes
silently for ~90 days.

If your tenant blocks the public-client flow ("interactive auth
failed" with `AADSTS50105` or similar), see
[Troubleshooting](#troubleshooting).

---

## 3. The screen

```
☰ inkwell · you@example.invalid                       ✓ synced 14:32
┌────────────┬──────────────────────────────┬────────────────────────┐
│ ▌ Folders  │   Messages                   │   Message              │
│ ▾ Inbox    │ ▶ Tue 14:30  Alice  Quote    │ From:    Bob …        │
│   Sent     │   Tue 13:55  Bob    Re: deck │ Subject: Re: deck     │
│   Drafts   │   Tue 11:02  News   Weekly   │                        │
│   Archive  │                              │ Hey team, …           │
│ ☆ Saved…   │                              │                        │
│   ☆ News   │                              │                        │
└────────────┴──────────────────────────────┴────────────────────────┘
:                                                                     
j/k nav · ⏎ open · / search · :filter narrow · f/d/a triage · q quit  
```

Three panes (folders / messages / message body), a top status bar, a
command bar (just above the help bar), and a help bar at the bottom
that changes with whichever pane has focus.

The **focus marker** is a `▌` glyph next to the focused pane's title.
The **cursor glyph** is `▶` for "the row you'd act on".

---

## 4. Navigating

| You want                          | Press                                      |
| --------------------------------- | ------------------------------------------ |
| Move between panes                | `1` folders, `2` messages, `3` viewer      |
|                                   | `Tab` / `Shift+Tab` to cycle               |
| Move within a list                | `j` / `k` (or `↓` / `↑`)                   |
| Open a folder / message           | `Enter`                                    |
| Expand / collapse a folder        | `o` (or space) on the focused folder       |
| Scroll a long message body        | Focus the viewer (`3`), then `j` / `k`     |
| Go back from viewer to list       | `h`                                        |

When you press Enter on a folder, focus jumps automatically to the
message list — saves a key. Press `1` to go back to the folder tree.

The folder tree shows your full hierarchy, with `▾` / `▸` disclosure
glyphs on parents that have children. Inbox auto-expands on first
launch.

---

## 5. Reading mail

From the message list, press `Enter` on a row. The viewer pane fills
with `From: To: Date: Subject:` plus the body (HTML is converted to
plain text). Long bodies scroll with `j` / `k` while the viewer is
focused.

Press `h` (or `2`) to return to the message list.

---

## 6. Searching

Press `/`. The cmd-bar prompt becomes `/`. Type your query and press
Enter — the message list fills with FTS5 matches from your local
cache (instant, even offline). Press `Esc` to clear and return to the
folder.

```
/ budget review
```

Server-side `$search` merge is post-v0.6 — for now, search hits
whatever's locally cached. Sync runs in the background and the cache
fills as you use the app.

---

## 7. Filtering and bulk actions

Filtering is the killer feature. It uses the `~`-operator pattern
language ([cheatsheet](cheatsheet.md#pattern-operators)) inspired by
mutt.

```
:filter ~f newsletter@* & ~d <30d
```

The list pane narrows to matches. The cmd-bar reminds you what's
filtered:

```
filter: ~f newsletter@* & ~d <30d · matched 247 · ;d delete · ;a archive · :unfilter
```

Now apply an action to **all** the matches:

| Press | Action                                |
| ----- | ------------------------------------- |
| `;d`  | Soft-delete (move to Deleted Items)   |
| `;a`  | Archive (move to Archive folder)      |

A confirm modal asks `Delete 247 messages? [y/N]` — destructive
default is `N`. Press `y` to dispatch through Microsoft Graph
`$batch` (chunked at 20-per-call). The status bar shows
`✓ soft_delete 247` when it completes; the filter clears
automatically and you're back on the original folder.

Plain text without a `~` operator works too — it's treated as
"subject or body contains":

```
:filter quarterly review
```

Type `:unfilter` to clear without performing an action.

### Saved searches

Add named patterns to your config (`~/.config/inkwell/config.toml`):

```toml
[[saved_searches]]
name    = "Newsletters"
pattern = "~f newsletter@* | ~f noreply@*"

[[saved_searches]]
name    = "Needs Reply"
pattern = "~r me@example.invalid & ~U & ~d <14d"
```

Restart inkwell. Saved searches appear in the folders pane under a
"Saved Searches" section, each row prefixed with `☆`. Press Enter on
one to run it; the result behaves exactly like a `:filter` (so `;d`,
`;a` work).

---

## 8. Triage on a single message

Highlight a message in the list pane (or open it in the viewer):

| Press | Action                                |
| ----- | ------------------------------------- |
| `r`   | Mark read                             |
| `R`   | Mark unread                           |
| `f`   | Toggle flag                           |
| `d`   | Soft-delete (Deleted Items)           |
| `a`   | Archive                               |

These work in the **list pane** for any keybinding, and in the
**viewer pane** for `f` / `d` / `a` (so you can read, decide, delete
without going back). `r` / `R` are reserved in the viewer pane for
the future reply / reply-all bindings (spec 15).

After delete or archive, the viewer pane closes itself and focus
pops back to the list — you immediately see what's next. Mark-read
and flag stay in place.

If a triage call fails (auth, throttle, network), the local change
rolls back and the top-right shows `ERR: <reason>`. The next sync
cycle retries pending actions automatically.

---

## 9. Customising

```toml
# ~/.config/inkwell/config.toml

[ui]
theme = "solarized-dark"
# Options: default, dark, light, solarized-dark, solarized-light, high-contrast.
# Unknown values fall back to default with a logged warning.

[[saved_searches]]
name    = "Newsletters"
pattern = "~f newsletter@*"
```

The full key reference is [`docs/CONFIG.md`](../CONFIG.md) (in the
repo). Restart inkwell after editing.

---

## 10. Where things live

| Path                                                         | Purpose                                  |
| ------------------------------------------------------------ | ---------------------------------------- |
| `~/Library/Application Support/inkwell/inkwell.db`            | Local SQLite cache (your mail, encrypted at rest by macOS FileVault if enabled) |
| `~/Library/Logs/inkwell/inkwell.log`                          | Logs (no message bodies, no tokens — bearer tokens are scrubbed) |
| macOS Keychain entry `inkwell` (account = your UPN)           | OAuth token cache (encrypted)            |
| `~/.config/inkwell/config.toml`                               | User config                              |

Wiping the cache (e.g. to force a fresh sync):

```sh
rm -rf ~/Library/Application\ Support/inkwell/inkwell.db*
```

Tokens stay in Keychain — re-auth isn't required.

---

## 11. Troubleshooting

| Symptom                                                       | Fix                                                                        |
| ------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `signin` opens browser but token-fetch fails with `AADSTS50105` | Your tenant blocks the public Microsoft Graph CLI Tools client. Talk to IT. |
| TUI launches but no folders / messages appear                 | Wait 30s for first sync; check `~/Library/Logs/inkwell/inkwell.log` for errors. |
| `ERR: …` in the top-right                                     | Click into the log file at the printed path; the full error is there.       |
| Help bar / sidebars disappear when scrolling a long message    | Should not happen post-v0.2.9. Update to the latest release.                |
| `;d` says "requires an active filter"                          | Run `:filter <pattern>` first.                                             |
| Search returns fewer results than Outlook                     | Search is local-only in v0.6. Server `$search` merge is post-v0.6 — sync first. |

---

## 12. What's NOT in this version (yet)

- **Sending email.** Drafts only — finish in Outlook. (`Mail.Send` is denied.)
- **Reply / reply-all / forward.** Spec 15.
- **Permanent delete (`D`)** with a strong confirm — spec 07's deferred work.
- **Move with a folder picker (`m`)** — spec 07's deferred work.
- **Composite undo for bulk ops (`u`)** — spec 11/undo overlay.
- **Linux + Windows binaries** — Linux ships now, untested. Windows post-v1.

See [`docs/ROADMAP.md`](../ROADMAP.md) for the longer list.

---

_Last reviewed against v0.7.0. If you're on a newer version, the
[cheatsheet](cheatsheet.md) is always source-of-truth for the bindings._
