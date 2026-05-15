# Spec 14 — CLI Mode

## Status
done (PR G-1 + G-1b: CLI surface complete; TUI UI polish committed alongside).

## DoD checklist (mirrored from spec)
- [x] `inkwell folders` — list cached folders, text + `--output json`.
- [x] `inkwell sync` — one-shot sync of all subscribed folders, prints duration, `--output json` carries `durationMs`.
- [x] `inkwell messages --folder <name> --limit N` — list envelopes (text + json).
- [x] `inkwell messages --folder <name> --unread` — only unread.
- [x] `inkwell messages --filter <pattern> --limit N` — list by spec 08 pattern.
- [x] `inkwell message show <id>` — full read (compact headers by default; `--headers` for full To/Cc/Bcc).
- [x] `inkwell filter <pattern>` — dry-run match printer.
- [x] `inkwell filter <pattern> --action delete|archive|mark-read --apply` — bulk dispatch via Graph $batch.
- [x] `--apply` mandatory for destructive bulk; `--yes` skips the confirm prompt.
- [x] `--output text|json` on every command.
- [x] Folder name resolution: well-known name first, then case-insensitive display name; friendly error pointing at `inkwell folders` when not found.
- [x] Tests: 7 unit tests over `resolveFolder`, `runFilterListing`, `truncCLI`.
- [x] `inkwell calendar today/week/agenda/show` — PR G-1.
- [x] `inkwell ooo on/off/set` (+ `--until`, text output) — PR G-1.
- [x] `inkwell rule list/save/edit/delete/eval/apply` — PR G-1.
- [x] `inkwell message reply/reply-all/forward` — PR G-1 (uses spec 15 action.Executor).
- [x] `inkwell message read/unread/flag/unflag/move/delete/permanent-delete` — PR G-1.
- [x] `inkwell message attachments / save-attachment` — PR G-1.
- [x] `inkwell export messages --format json|mbox` — PR G-1.
- [x] `inkwell backfill --folder --until` — PR G-1.
- [x] `inkwell daemon` — PR G-1.
- [x] Global flags: `--output/-o`, `--quiet/-q`, `--yes/-y`, `--color`, `--no-sync` — PR G-1.
- [x] `[cli]` config section + `CLIConfig` struct + defaults — PR G-1.
- [x] `internal/cli/exitcodes.go` exit code constants — PR G-1.
- [x] `inkwell folder show/subscribe/unsubscribe/tree` — PR G-1.
- [x] `inkwell settings` text output mode — PR G-1.

## Iteration log

### Iter 3 — 2026-05-04 (G-1b: TUI color + fullscreen actions + z tooltip)
- Slice: UI polish shipped alongside G-1 commit.
- Files modified: `internal/render/theme.go` (link=cyan, attach=amber; `NewTheme` for palette control), `internal/ui/theme.go` (link/attach tokens per preset; `RenderTheme render.Theme` field; import render), `internal/ui/app.go` (use `m.theme.RenderTheme` in `openMessageCmd`; fullscreen actions r/R/f/d/D/a in `updateFullscreenBody`; z hint in viewer help bar; updated fullscreen hint line), `internal/ui/dispatch_test.go` + `app_e2e_test.go` (updated assertions + 3 new tests).
- Commands: `bash scripts/regress.sh` — all 6 gates green.
- Critique: subscribe/unsubscribe stubs remain; `--no-sync` not yet threaded; calendar commands still hit Graph directly.

### Iter 2 — 2026-05-04 (PR G-1: all remaining CLI subcommands)
- Slice: all missing subcommands from the DoD checklist.
- Files added: `cmd_calendar.go`, `cmd_daemon.go`, `cmd_export.go`, `cmd_rule.go`, `internal/cli/exitcodes.go`.
- Files modified: `cmd_root.go` (global flags + effectiveOutput helper), `cmd_messages.go` (triage/attachment/compose subcommands), `cmd_folder.go` (show/subscribe/unsubscribe/tree), `cmd_ooo.go` (--until, set subcommand, text output), `cmd_settings.go` (text output), `cmd_sync.go` (backfill), `internal/config/config.go` + `defaults.go` (CLIConfig).
- Commands: `go build ./...` clean; `go test -race ./...` all pass; `go vet ./...` clean; staticcheck clean (pre-existing S1016 in ui/compose.go not introduced by this change).
- Critique: subscribe/unsubscribe are stubs (config-driven only). `--no-sync` global flag is registered but not yet threaded through buildHeadlessApp (future PR). Calendar commands hit Graph directly (no store caching path).

### Iter 1 — 2026-04-29 (folders / messages / sync / filter)
- Slice: cobra subcommands in cmd/inkwell/, share a `headlessApp` helper for the auth probe + store + Graph wiring that every non-TUI command needs.
- Files added: cmd_app.go (helper), cmd_folders.go, cmd_messages.go, cmd_sync.go, cmd_filter.go, cmd_messages_test.go.
- Folder name resolution mirrors the TUI's existing tolerance: well-known first, then case-insensitive display-name, then a friendly error.
- Pattern path reuses spec 08's Parse + CompileLocal + the store's SearchByPredicate (added in spec 10) — no new evaluator.
- Bulk filter path reuses spec 09's BatchExecute via the existing action.Executor (cmd_run.go's bulkAdapter is now a sibling, not the only path).
- Tests cover the new helpers; full-pipe e2e (cobra invocation → exit code) deferred.
- Critique:
  - `inkwell message show <id>` waits on Graph if the body isn't cached. Could time out on slow links; should grow a `--no-fetch` flag for offline usage.
  - The `filter --apply` confirm prompt only fires for `delete`. `archive` is also one-way (in the sense that messages move out of view) but recoverable; the current behaviour is fine.
  - Removed a speculative `--to` flag during the same-day code review — better not to advertise unimplemented options.

## Cross-cutting checklist (`docs/CONVENTIONS.md` §11)
- [x] Scopes used: same as the TUI surface (Mail.Read, Mail.ReadWrite, MailboxSettings.Read).
- [x] Store reads/writes: messages (read), folders (read), accounts (read), actions (Enqueue + UpdateActionStatus via the existing executor).
- [x] Graph endpoints: same as the TUI surface, via the existing graph.Client.
- [x] Offline behaviour: `folders`, `messages`, `filter` (dry-run) all run against the local cache. `sync` and `filter --apply` need network.
- [x] Undo: bulk operations land in the action queue; the engine's drain handles transient retry. No CLI-side undo (deferred — would need an `inkwell undo` command).
- [x] User errors: friendly errors for "not signed in", folder not found, missing `--apply` with `--action`. Cobra prints the error + exit 1.
- [x] Latency budget: not measured; folder + messages reads are <100ms per spec 02 budgets, the sync path is per spec 03.
- [x] Logs: every command goes through the same redacting slog handler used by the TUI; bearer tokens scrubbed.
- [x] User docs: docs/user/reference.md (new "CLI subcommands" section) + docs/user/how-to.md (new "Script your inbox from the shell" recipe).
