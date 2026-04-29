# Spec 14 — CLI Mode

## Status
in-progress (CI scope: folders / messages / message show / sync / filter shipped in v0.10.0; calendar / OOO / saved-search / drafts / attachments / daemon mode deferred).

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
- [ ] `inkwell calendar today/week/agenda` — deferred. Calendar is read-only and the modal works in the TUI.
- [ ] `inkwell ooo on/off/set` — deferred. Modal works in the TUI; CLI form is a follow-up.
- [ ] `inkwell rule list/save/edit/delete/eval/apply` — deferred. Saved-searches CRUD lands when DB-backed Manager arrives (spec 11 v2).
- [ ] `inkwell message reply/forward` — depends on spec 15 (compose), not yet implemented.
- [ ] `inkwell message attachments / save-attachment` — deferred.
- [ ] `inkwell export --since` — deferred. JSONL/mbox roller for archival; nice-to-have.
- [ ] Daemon mode (`inkwell daemon`) — deferred. Long-running background sync without the TUI.

## Iteration log

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

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: same as the TUI surface (Mail.Read, Mail.ReadWrite, MailboxSettings.Read).
- [x] Store reads/writes: messages (read), folders (read), accounts (read), actions (Enqueue + UpdateActionStatus via the existing executor).
- [x] Graph endpoints: same as the TUI surface, via the existing graph.Client.
- [x] Offline behaviour: `folders`, `messages`, `filter` (dry-run) all run against the local cache. `sync` and `filter --apply` need network.
- [x] Undo: bulk operations land in the action queue; the engine's drain handles transient retry. No CLI-side undo (deferred — would need an `inkwell undo` command).
- [x] User errors: friendly errors for "not signed in", folder not found, missing `--apply` with `--action`. Cobra prints the error + exit 1.
- [x] Latency budget: not measured; folder + messages reads are <100ms per spec 02 budgets, the sync path is per spec 03.
- [x] Logs: every command goes through the same redacting slog handler used by the TUI; bearer tokens scrubbed.
- [x] User docs: docs/user/reference.md (new "CLI subcommands" section) + docs/user/how-to.md (new "Script your inbox from the shell" recipe).
