# Spec 14 — CLI Mode

**Status:** In progress (CI scope, v0.10.x → v0.15.x). Implemented subcommands: `signin`, `signout`, `whoami`, `folders` (list), `messages`, `sync`, `filter`, `folder new/rename/delete` (spec 18 v0.15.x). Residual: `folder subscribe/unsubscribe/show/tree`, full `message` verb set (show / read / unread / flag / unflag / move / delete / permanent-delete / attachments / save-attachment / reply / reply-all / forward), `rule` (list / show / save / edit / delete / eval / apply — depends on spec 11), `calendar` (today / week / agenda / show — depends on spec 12 PR 6b), `ooo` (on / off / set), `settings`, `export`, `daemon`, `backfill`. ~60% of the spec's CLI surface remains; exit-code map + line-delimited JSON streams + `--config` / `--output` / `--color` global flags also pending.
**Depends on:** All prior specs (CLI exposes their underlying capabilities).
**Blocks:** Nothing.
**Estimated effort:** 1–2 days.

---

## 1. Goal

Expose the application's capabilities as a non-interactive command-line tool, runnable from shell scripts, cron, fzf pipelines, and other automation contexts. The CLI is **not a different program** — it's the same binary in non-interactive mode, sharing the auth, store, sync, and action infrastructure with the TUI.

The CLI is what makes this app composable in the Unix sense: piping output to `jq`, scheduling cleanup with cron, integrating with fzf, etc. Himalaya is the canonical reference here (spec discussion in earlier conversation).

## 2. Module layout

```
cmd/inkwell/
├── main.go                     # entrypoint; routes flags to TUI or CLI
├── cmd_root.go                 # root cobra command
├── cmd_signin.go               # auth subcommands
├── cmd_signout.go
├── cmd_whoami.go
├── cmd_sync.go                 # sync / backfill
├── cmd_folder.go               # folder list/manage
├── cmd_messages.go             # list / read / triage
├── cmd_filter.go               # pattern-based selection
├── cmd_rule.go                 # saved searches
├── cmd_calendar.go             # calendar read
├── cmd_ooo.go                  # OOO toggle
├── cmd_settings.go             # show settings
└── cmd_export.go               # export utilities

internal/cli/
├── output.go                   # text + JSON formatting
├── progress.go                 # CLI progress bars (mpb)
└── prompt.go                   # interactive y/N when applicable
```

We use `github.com/spf13/cobra` for command tree and `github.com/vbauerster/mpb` for progress bars in interactive terminals (auto-disabled when stdout is not a TTY).

## 3. Mode routing

`main.go`:

```go
func main() {
    if len(os.Args) == 1 {
        // No subcommand → launch TUI
        runTUI()
        return
    }
    // Subcommand → CLI
    rootCmd.Execute()
}
```

The first positional arg picks the mode. `inkwell` alone or `inkwell` with only flags (e.g. `inkwell --verbose`) launches the TUI; any subcommand stays in CLI mode.

## 4. Global flags

Available on all subcommands:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--config <path>` | `~/.config/inkwell/config.toml` | Override config file path. |
| `--output <fmt>` | from `[cli].default_output` (default `text`) | `text` or `json`. |
| `--color <when>` | from `[cli].color` (default `auto`) | `auto`, `always`, `never`. |
| `--log-level <level>` | from `[logging].level` (default `info`) | `debug`, `info`, `warn`, `error`. |
| `--verbose`, `-v` | false | Shorthand for `--log-level debug`. |
| `--quiet`, `-q` | false | Suppress progress output and INFO logs to stderr. |
| `--no-sync` | false | Don't trigger sync before reading. Use cached data only. |
| `--yes`, `-y` | false | Skip interactive confirmations. |
| `--help`, `-h` | — | Subcommand-specific help. |

## 5. Output formats

### 5.1 Text output (default)

Human-readable, color-coded if a TTY:

```
$ inkwell folders
ID                          Name              Unread  Total
AAMkADAxMz...                Inbox                 47    532
AAMkADAxMz...                Sent                   0    288
AAMkADAxMz...                Drafts                 3      3
AAMkADAxMz...                Archive                0   1247
AAMkADAxMz...                Clients/TIAA           4     94
```

Tables align based on terminal width. If output is to a pipe (not a TTY), tabular spaces become tab-separated for `cut`/`awk` friendliness.

### 5.2 JSON output

`--output json` produces structured JSON:

```bash
$ inkwell folders --output json
[
  { "id": "AAMkADAxMz...", "name": "Inbox", "unread": 47, "total": 532, "wellKnownName": "inbox" },
  { "id": "AAMkADAxMz...", "name": "Sent", "unread": 0, "total": 288, "wellKnownName": "sentitems" },
  ...
]
```

JSON output:
- Uses camelCase keys consistently.
- Includes ALL fields, including ones omitted from text view.
- Outputs pretty-printed with 2-space indent unless `--compact` flag set.
- Streams when possible (line-delimited JSON for long lists; one object per line, no enclosing array).

```bash
$ inkwell messages --folder Inbox --output json --compact
{"id":"AAMk...","subject":"Q4 forecast","from":{...},...}
{"id":"AAMk...","subject":"...","from":{...},...}
...
```

This makes piping to `jq` natural.

### 5.3 Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | General error |
| 2 | User error (bad flags, malformed pattern, etc.) |
| 3 | Auth error (sign in needed) |
| 4 | Network error (Graph unreachable) |
| 5 | Not found (folder, message, saved search) |
| 6 | Confirmation required but `--yes` not passed (for destructive commands) |
| 7 | Throttled (operation refused due to rate limits) |
| 8 | Permission denied (Graph 403) |

Subcommands document which codes they may return.

## 6. Subcommand reference

### 6.1 Auth

```bash
inkwell signin                  # device code flow
inkwell signout                 # clear keychain entry
inkwell whoami                  # print UPN, exit 3 if not signed in
```

(Already specified in spec 01 §8.)

### 6.2 Sync

```bash
inkwell sync                    # one-shot sync of all subscribed folders
inkwell sync --folder Inbox     # specific folder
inkwell sync --status           # show last sync time per folder, exit 0
inkwell backfill --folder Inbox --until 2025-01-01  # extend cache backward
```

`sync` blocks until done, prints a one-line summary:

```
✓ Synced 12 folders · 47 new · 12 updated · 3 deleted · 1.4s
```

`--output json` produces:

```json
{ "foldersSynced": 12, "added": 47, "updated": 12, "deleted": 3, "durationMs": 1402 }
```

### 6.3 Folders

```bash
inkwell folders                  # list all
inkwell folders --tree           # hierarchical
inkwell folder show Inbox        # details on one
inkwell folder subscribe Newsletters    # mark for sync
inkwell folder unsubscribe Junk
```

### 6.4 Messages

```bash
# List
inkwell messages --folder Inbox --limit 50
inkwell messages --folder Inbox --unread
inkwell messages --filter '~f bob' --limit 20

# Read one
inkwell message show <id>                # full content with body
inkwell message show <id> --headers      # include all headers
inkwell message show <id> --raw          # raw JSON from cache

# Triage (single message)
inkwell message read <id>                # mark read
inkwell message unread <id>
inkwell message flag <id>
inkwell message unflag <id>
inkwell message move <id> --to Archive
inkwell message delete <id>              # soft delete (move to Deleted Items)
inkwell message permanent-delete <id>    # confirms unless --yes

# Attachments
inkwell message attachments <id>         # list
inkwell message save-attachment <id> <attachmentName> --to ./

# Drafts
inkwell message reply <id> --body-from-file ./response.txt
inkwell message reply-all <id> --editor   # opens $EDITOR
inkwell message forward <id> --to alice@example.com --body "FYI"
```

### 6.5 Filter (bulk operations)

This is where the CLI shines. Pattern-based bulk operations are scriptable:

```bash
# Dry-run by default in CLI mode
inkwell filter '~f newsletter@* & ~d <30d'
# → prints matched messages, count, exits

# Apply (requires explicit --action and --apply)
inkwell filter '~f newsletter@* & ~d <30d' --action delete --apply
# → deletes; prints summary

# Common options
inkwell filter '~G Newsletters' --action archive --apply
inkwell filter '~A & ~d >180d' --action move --to "Archive/2025" --apply

# JSON output for piping
inkwell filter '~f newsletter@*' --output json | jq '.messages[] | .id'

# fzf integration
inkwell messages --filter '~U' --output json --compact | \
  fzf --preview 'inkwell message show {1}' | \
  awk '{print $1}' | \
  xargs -I {} inkwell message read {}
```

The `--apply` flag is mandatory for destructive bulk operations. Without it, the command is dry-run regardless of `[batch].dry_run_default` (CLI's contract: explicit destructive intent always required).

`--yes` skips any interactive confirmations. The CLI is non-interactive by default, so `--yes` mainly matters when `[cli].confirm_destructive_in_cli = true` is set.

### 6.6 Saved searches (rules)

```bash
inkwell rule list
inkwell rule show Newsletters
inkwell rule save Newsletters --pattern '~f newsletter@*' --pin
inkwell rule edit Newsletters --pattern '~f newsletter@* | ~f noreply@*'
inkwell rule delete Newsletters
inkwell rule eval Newsletters                         # returns matching IDs
inkwell rule apply Newsletters --action delete --apply  # rule-driven bulk
```

`rule apply` is shorthand for `filter` using a saved pattern.

### 6.7 Calendar

```bash
inkwell calendar today                    # today's events
inkwell calendar week                     # this week
inkwell calendar agenda --days 7          # agenda for next N days
inkwell calendar show <event-id>          # details

# JSON for tooling
inkwell calendar today --output json | jq '.events[] | select(.responseStatus == "tentative")'
```

### 6.8 OOO

(Spec 13 §9 already documents these.)

```bash
inkwell ooo
inkwell ooo on
inkwell ooo on --until 2026-05-03
inkwell ooo off
inkwell ooo set --internal "..." --external "..."
```

### 6.9 Settings

```bash
inkwell settings                 # human-readable
inkwell settings --output json
```

### 6.10 Export

```bash
inkwell export messages --folder Inbox --format mbox --to ./inbox.mbox
inkwell export messages --filter '~G Archive' --format json --to ./archive.json
inkwell export calendar --since 2026-01-01 --format ics --to ./cal.ics
```

Formats supported:
- `mbox` for messages (broadly compatible with mail tools).
- `eml` for individual messages (one file per message in target dir).
- `json` for messages or events (line-delimited).
- `ics` for calendar (iCalendar standard).

Export is read-only and works against cached data (`--no-sync` is the default behavior for export to avoid surprising the user with new mail mid-export). Pass `--sync-first` to force a sync before exporting.

## 7. Daemon mode (lightweight)

```bash
inkwell daemon
```

Runs the sync engine in the background without a UI. Useful for:
- Pre-warming the cache before opening the TUI.
- Cron-driven periodic refresh.

The daemon exits cleanly on SIGINT/SIGTERM. It writes a PID file at `~/Library/Application Support/inkwell/daemon.pid` so a second `inkwell daemon` invocation refuses to start (or `--replace` kills the existing one).

Not a long-running service in v1 — just a foreground process that does sync. Real daemonization (launchd plist) is a deferred enhancement.

## 8. fzf and pipeline patterns

The CLI is designed for these compositions:

```bash
# Open the message I had in mind
inkwell messages --filter '~d <7d' --output json --compact | \
  jq -r '"\(.id)\t\(.from.name) — \(.subject)"' | \
  fzf | \
  cut -f1 | \
  xargs inkwell message show

# Quick-archive workflow
for id in $(inkwell filter '~G Newsletters' --output json | jq -r '.messages[].id'); do
  inkwell message move "$id" --to "Archive/Newsletters" --yes
done
# (Better: just inkwell filter '~G Newsletters' --action move --to "Archive/Newsletters" --apply)

# Count unread per folder
for f in $(inkwell folders --output json | jq -r '.[].name'); do
  c=$(inkwell messages --folder "$f" --unread --output json --compact | wc -l)
  echo "$f: $c"
done

# Auto-OOO when on PTO (cron)
0 17 * * 5 /usr/local/bin/inkwell ooo on \
  --internal "Out for the weekend" \
  --until "$(date -v+Mon +%Y-%m-%d)"
```

## 9. Configuration

This spec owns the `[cli]` section. New keys for CONFIG.md:

| Key | Default | Used in § |
| --- | --- | --- |
| `cli.default_output` | `"text"` | §4 |
| `cli.color` | `"auto"` | §4 |
| `cli.confirm_destructive_in_cli` | `false` | §5 |
| `cli.progress_bars` | `"auto"` | §10 (auto-disabled when not TTY) |
| `cli.json_compact` | `false` | §5.2 |
| `cli.export_default_dir` | `"."` | §6.10 |

## 10. Progress and feedback

Long-running operations show progress when stdout is a TTY:

```
$ inkwell sync
Syncing folders... [3/12] ████████░░░░░░░░░░░░  25%
```

When stdout is not a TTY, progress lines go to stderr (so they don't pollute pipelines):

```bash
$ inkwell sync 2>/dev/null   # suppress progress
$ inkwell sync 2>progress.log  # capture progress separately
```

`--quiet` suppresses progress entirely.

## 11. Failure modes

| Scenario | Behavior |
| --- | --- |
| Not signed in | Exit 3 with message: `Not signed in. Run: inkwell signin` |
| Sign-in attempted in non-TTY | Exit 1 with message; device code flow needs interactive terminal. |
| Unknown subcommand | Exit 2 with cobra's standard help message. |
| Pattern parse error | Exit 2 with the parser's error message. |
| Bulk requires --apply but not provided | Exit 6 with: `This is a destructive operation. Re-run with --apply.` |
| Network unavailable | Exit 4. Read commands fall back to cache automatically (with a warning); write commands fail. |
| Throttled | Exit 7 if all retries exhausted. Single retries happen silently. |
| Output piped to broken pipe (e.g., `... \| head -1`) | Exit 0 (handle SIGPIPE cleanly). |

## 12. Test plan

### Unit tests

- Command tree compiles (cobra command structure validates).
- Output formatters: same data → same JSON, regardless of platform/locale.
- Exit code mapping: each error type maps to documented code.

### Integration tests

- Each subcommand against mocked Graph; assert correct calls and outputs.
- JSON output schema validates against a JSONSchema fixture per command.
- Exit code tests: bad pattern → 2; not signed in → 3; etc.

### Manual smoke tests

1. Pipeline: `inkwell messages --filter '~U' --output json | jq | wc -l` works.
2. fzf pattern from §8 works end-to-end.
3. Cron-style: `crontab -l` example runs without TTY.
4. SIGPIPE test: `inkwell messages | head -3` exits cleanly.
5. `inkwell --help` shows subcommand list; each subcommand `--help` is informative.

## 13. Definition of done

- [ ] All subcommands from §6 implemented and tested.
- [ ] Text and JSON output for every command.
- [ ] Exit codes match §5.3.
- [ ] Pipeline-friendly output (line-delimited JSON, no enclosing array for streams).
- [ ] Progress bars on TTY; quiet on pipes.
- [ ] `--help` is comprehensive at root and per subcommand.
- [ ] `daemon` mode runs and exits cleanly.
- [ ] At least three documented pipeline examples in the README work as written.

## 14. Out of scope

- Shell completion scripts (cobra has built-in support; we'll ship them as a stretch goal).
- A "REPL" mode — interactive shell with line editing. Adds complexity for marginal value.
- Background launchd integration (post-v1).
- Plugin system. CLI subcommands are compiled in.
- Watch mode (`inkwell messages --filter X --watch` continuously updating). Possible v1.1.
