# Tutorial — your first 30 minutes with inkwell

This is a sequential walkthrough. Follow it once, in order. By the
end you'll have signed in, navigated your inbox, opened a message,
and tried search and bulk-delete with the safety net on. Plan for
about 30 minutes.

For task-by-task recipes after this, see [`how-to.md`](how-to.md).
For lookups, see [`reference.md`](reference.md).

---

## Step 1 — Install

```sh
gh release download <vX.Y.Z> -p '*macos_arm64*' -D /tmp
tar -xzf /tmp/inkwell_<vX.Y.Z>_macos_arm64.tar.gz -C /tmp
xattr -d com.apple.quarantine /tmp/inkwell        # macOS Gatekeeper
sudo mv /tmp/inkwell /usr/local/bin/              # optional
```

Linux builds (amd64 / arm64) are also published on each release; the
`xattr` step is macOS-only.

## Step 2 — Sign in

```sh
inkwell signin
```

The system browser opens. Sign in with your work account; the browser
closes itself, and inkwell launches the TUI. On every subsequent
launch, just `inkwell run` — your token cache lives encrypted in
macOS Keychain and refreshes silently for ~90 days.

If sign-in fails with `AADSTS50105` or similar, your tenant blocks the
public-client flow. See [`how-to.md` → "When sign-in fails"](how-to.md#when-sign-in-fails).

## Step 3 — Look around

The screen has three panes (folders / messages / message body), a
status bar at the top, and a help bar at the bottom that changes
with whichever pane has focus.

```
☰ inkwell · you@example.invalid                       ✓ synced 14:32
┌────────────┬──────────────────────────────┬────────────────────────┐
│ ▌ Folders  │   Messages                   │   Message              │
│ ▾ Inbox    │ ▶ Tue 14:30  Alice  Quote    │ From:    Bob …         │
│   Sent     │   Tue 13:55  Bob    Re: deck │ Subject: Re: deck      │
│   Drafts   │ 📅 Tue 11:02  Bob    Accepted │                        │
│   Archive  │                              │ Hey team, …            │
└────────────┴──────────────────────────────┴────────────────────────┘
:                                                                     
j/k nav · ⏎ open · / search · :filter narrow · f flag · d delete …    
```

Try these in order:

1. Press `1` — focus moves to folders. The `▌` glyph marks the
   focused pane. The bottom help bar updates to folder-pane keys.
2. Press `j` and `k` to walk through your folders. `o` (or space)
   expands a folder with children. The `▾` and `▸` glyphs mark
   parents. Inbox auto-expands on first launch.
3. Press `Enter` on a folder. Focus jumps automatically to the
   message list — saving a key — and the messages load.
4. Press `j`/`k` to walk through messages. The `▶` cursor follows.
5. Press `Enter` to open a message in the viewer pane.
6. From the viewer, `j`/`k` scroll the body. `h` returns to the list.

Press `q` to quit any time.

## Step 4 — Search

Press `/`. The cmd-bar at the bottom-left becomes a search prompt.
Type a few words and press Enter:

```
/ budget review
```

The message list narrows to FTS hits from your local cache — instant,
even offline. Press `Esc` to clear and return to the folder.

> Search is local-only in v0.7. Server-side `$search` merge is post-v0.8.
> If a message you expect isn't there, sync it first by waiting a
> moment or pressing `Ctrl+R`.

## Step 5 — Filter and triage

Filtering is the killer feature. It uses a `~`-operator pattern
language inspired by mutt.

Press `:`. The cmd-bar becomes a command prompt. Type:

```
:filter ~f newsletter@*
```

Press Enter. The list narrows to messages from any `newsletter@…`
sender. The cmd-bar reminds you what's filtered:

```
filter: ~f newsletter@* · matched 47 · ;d delete · ;a archive · :unfilter
```

Glance at the matches to sanity-check. Then press `;d`.

A confirmation pops up: `Delete 47 messages? [y/N]`. The default is
`N` (safety net for destructive bulk ops). Press `y`.

inkwell dispatches a Microsoft Graph `$batch` (chunked at 20
messages per call). The status bar shows `✓ soft_delete 47` when
done. The filter clears automatically and you're back on the inbox.

The deleted messages appear in **Deleted Items** server-side — same
folder Outlook moves them to. Recover from there if you change your
mind.

## Step 6 — Tame your inbox with saved searches

Edit `~/.config/inkwell/config.toml`:

```toml
[[saved_searches]]
name    = "Newsletters"
pattern = "~f newsletter@* | ~f noreply@*"

[[saved_searches]]
name    = "Needs Reply"
pattern = "~r me@example.invalid & ~U & ~d <14d"
```

Restart inkwell. The folders pane now shows a "Saved Searches"
section with each entry prefixed by `☆`. Press Enter on one — it
runs the pattern just like `:filter`, and `;d` / `;a` work on the
matches.

## Step 7 — Glance at your calendar

Press `:`, type `cal`, Enter. A modal pops up showing today's
events with start/end times, organizer, and meeting URL if there is
one. Press Esc to close.

inkwell can't accept, decline, or modify events — that's `Calendars.Read`
only. For those, finish in Outlook.

## What's next

You've covered the daily flows. From here:

- For a recipe ("how do I delete all newsletters older than 90 days?"),
  jump to [`how-to.md`](how-to.md).
- To memorise the keys, skim [`reference.md`](reference.md).
- For the design philosophy ("why drafts only? why local-first?"),
  read [`explanation.md`](explanation.md).

---

_Last reviewed against v0.8.0._
