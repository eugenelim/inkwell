# Audit drain plan — retiring `docs/audits/spec-1-15-impl-gaps.md`

## Why this file exists

The spec 1–15 impl/design audit dropped a punch list of 113 gaps
across 15 specs (plus a top-10 leverage list). The user direction:
progressively address findings in PR-sized chunks ordered by
leverage, and as each gap is verified, fold it into the relevant
**spec** or **plan** file so the audit doc shrinks. When every row
has either been resolved or explicitly deferred in a spec plan
file, the audit file (and the `docs/audits/` directory) get
deleted.

This file is the tracker for that drain. Update on every audit-
draining PR.

## Movement rules — what "drained" means per row

For each audit row, one of three things happens:

1. **Implemented and verified.** The finding is resolved by code +
   tests in the PR. The audit row is **deleted**. The spec's plan
   file (`docs/plans/spec-NN.md`) gains an iteration-log entry
   noting the closed gap.

2. **Explicitly deferred.** The finding is moved (verbatim or
   refined) into the relevant `docs/plans/spec-NN.md` under a
   "Deferred" section, with a one-line WHY. The audit row is
   **deleted**.

3. **Reclassified.** The finding is wrong, out-of-scope, or
   superseded by a later spec decision. Replaced in the plan file
   with the rationale; audit row deleted.

The audit doc is **append-only-deletes-only** during the drain —
no edits to existing rows. This keeps `git log -p
docs/audits/spec-1-15-impl-gaps.md` an honest record of what got
addressed and when.

## Phase 1 — Original 12-PR sequence (historical)

See the Phase 1 status tracker below for ship status on each of
the original 12 PRs. ~70% of the audit is now drained. What
remains is reorganised in Phase 2.

## Phase 2 — Completion plan (spec-by-spec)

Remaining work is grouped into eight impact categories. The rule:
**complete one spec fully before opening the next**. Config
sections for each spec land in the same PR (the original standalone
PR 12 dissolves into individual spec PRs). Dependencies are called
out explicitly — no PR starts before its blockers ship.

---

### Category A — Triage completeness (specs 07 → 09 → 10)

The core triage loop has three interdependent gaps. Ship in order:
single-message correctness first (07), then batch engine (09),
then bulk UX (10) which needs 09's composite undo.

**PR A-1 — Spec 07 finish** `feat(spec-07): replay + InFlight + move-id + triage config`
- Replay-on-startup: scan Pending/InFlight rows on launch and re-dispatch (spec §10 `ReplayPending`)
- InFlight state transition: Executor sets `InFlight` before each Graph call, not Pending→Done in one hop
- Move-id stale fix: after a successful Move, replace the local message row's primary key with the Graph-returned new ID; delete the old row so subsequent ops don't 404
- `[triage]` config section: `archive_folder`, `confirm_threshold`, `confirm_permanent_delete`, `undo_stack_size`, `optimistic_ui`, `recent_folders_count`
- Closes: spec 07 — InFlight skipped; move-id stale; replay absent; `[triage]` config entirely absent

**PR A-2 — Spec 09 finish** `feat(spec-09): batch retry + concurrency + bulk undo + config`
- Depends on: A-1 (executor InFlight correctness)
- Per-sub-request 429 retry with Retry-After honouring (`executeChunkWithRetry`)
- Concurrent chunk fan-out (`[batch].batch_concurrency`, default 3)
- `add_category` / `remove_category` in `actionToSubRequest`
- Permanent-delete sub-request shape (`POST /messages/{id}/permanentDelete` in $batch)
- `OnProgress(done, total)` callback wired to the UI
- 5,000-message soft cap enforcement
- Composite undo entry per bulk operation (one `UndoEntry` for the whole bulk, not per message)
- `[batch]` config section: `max_per_batch`, `batch_concurrency`, `batch_request_timeout`, `dry_run_default`, `max_retries_per_subrequest`, `bulk_size_warn_threshold`, `bulk_size_hard_max`
- Closes: spec 09 — retry absent; serial-only dispatch; no bulk undo; no OnProgress; no [batch] config

**PR A-3 — Spec 10 finish** `feat(spec-10): bulk verbs + progress + preview + dry-run`
- Depends on: A-2 (composite undo)
- Six missing `;` verbs: `;D` permanent-delete, `;m` move, `;r`/`;R` mark-read/unread, `;f`/`;F` flag/unflag, `;c`/`;C` category
- `F` keybind: opens command bar pre-filled with `:filter `
- Confirm modal: shows filter expression + first 3 affected subjects + total count (not just "Delete N?")
- Preview screen with toggleable checkboxes up to 5,000 (spec §6 `[p] Preview all`)
- Progress modal updating via `OnProgress`; `Esc` cancels in-flight bulk
- Result modal: success / partial (X/Y) / pending breakdown; `[l] see failed` shortcut
- Composite undo for bulk restores only the successful subset
- Dry-run mode: `!` suffix to action letter (`;d!`) previews without applying; `[batch].dry_run_default`
- `[bulk]` config section: `preview_sample_size`, `progress_threshold`, `progress_update_hz`, `suggest_save_after_n_uses`
- Closes: spec 10 — 6 bulk verbs absent; no F handler; primitive confirm; no preview screen; no progress modal; no result breakdown; no dry-run; no [bulk] config

---

### Category B — Knowledge management (spec 11 → spec 04 finish)

Spec 11 is the highest cross-cutting leverage remaining: it unblocks
`:save`/`:rule` in spec 04, `inkwell rule` in spec 14, and live
sidebar counts. Spec 04 has two items that only become trivial once
spec 11 ships.

**PR B-1 — Spec 11** `feat(spec-11): Manager API + sidebar counts + :rule CRUD + seed defaults`
- `internal/savedsearch/manager.go`: `Manager` interface — `Save`, `Get`, `List`, `Delete`, `Evaluate`, `Pinned`; implementation over the `saved_searches` DB table (replaces TOML-config-only runtime path)
- Sidebar live counts: background goroutine at `[saved_search].background_refresh_interval` runs `Evaluate` per pinned search and updates the count badge
- `:rule save <name>` / `:rule list` / `:rule show <name>` / `:rule delete <name>` / `:rule edit <name>` in-modal CRUD; `e` on saved-search row opens editor
- Auto-suggest after `[saved_search].suggest_save_after_n_uses` matching runs of the same pattern (once per session per pattern)
- Seed defaults on first launch: "Unread", "Flagged", "From me" (spec §7.3)
- TOML mirror: writes `~/.config/inkwell/saved_searches.toml` on every mutation; divergence prompt on launch if DB and file differ
- CLI stubs for `inkwell rule` (full verb surface wired in spec 14 PR G-1)
- `[saved_search]` config section: `cache_ttl`, `background_refresh_interval`, `seed_defaults`, `toml_mirror_path`
- Closes: spec 11 — Manager API absent; DB source of truth unwired; no live counts; no CRUD commands; no auto-suggest; no seed defaults; no TOML mirror; no [saved_search] config

**PR B-2 — Spec 04 finish** `feat(spec-04): lifecycle + transient_ttl + min_terminal + :save/:rule`
- Depends on: B-1 (`:save` / `:rule` use Manager)
- Lifecycle teardown: `tea.Quit` path calls `engine.Stop()` + `store.Close()` before returning (spec §14)
- `[ui].transient_status_ttl` (default 5s): auto-clear status-bar messages via a time-boxed `tea.Cmd`
- Min-terminal check: `relayout` renders "terminal too small (need 80×24)" overlay below spec §13 minimum; normal UI unblocked on resize
- `confirm_destructive_default` wired from `[ui]` config (defaults No; makes it overridable)
- `ui.unread_indicator` / `ui.flag_indicator` / `ui.attachment_indicator` config keys wired to pane rendering
- `:save <name>` command uses Manager.Save; `:rule` opens Manager edit modal
- Closes: spec 04 — lifecycle teardown broken; transient_status_ttl absent; min_terminal check absent; :save/:rule unimplemented; indicator config absent

---

### Category C — Message rendering completeness (spec 05 finish)

After PR 10 shipped viewer keys and attachments, 12 rendering items
remain — reading-experience quality that a daily user hits constantly.

**PR C-1 — Spec 05 finish** `feat(spec-05): quote-collapse + format-flowed + HTML config + body $select`
- Quote collapse: fold runs of `> `-prefixed lines to `[… N quoted lines]` at `[rendering].quote_collapse_threshold`; `e` key expands one quote block; `Q` toggles all (spec §6.3)
- Attribution-line detection: regex-match `On <date>, <name> wrote:` and apply muted style (spec §6.4)
- Outlook noise stripping: `strip_patterns` config (list of regexes; defaults include "External email" banner and `Outlook-AltVw` blocks) (spec §6.5)
- Format=flowed RFC 3676: detect `Content-Type: text/plain; format=flowed` and unwrap soft-wrapped lines before rendering (spec §7)
- HTML converter fallback: `[rendering].html_converter` config (`html2text` default; `pandoc`/`lynx` as alternatives); `external_converter_timeout`
- Full-headers $select: add `internetMessageHeaders` to `GetMessageBody` $select so `H` key renders actual SMTP headers, not just cached envelope fields (spec §5.2)
- `BodyOpts.Width` from actual viewer pane width instead of hardcoded 80; `[rendering].wrap_columns` override
- Single-flight per message ID: prevent duplicate Graph body fetches on concurrent `Enter` presses (spec §5.1)
- `[rendering]` remaining keys: `quote_collapse_threshold`, `large_attachment_warn_mb`, `strip_patterns`, `external_converter_timeout`, `html_converter`, `html_converter_cmd`, `attachment_save_dir`, `wrap_columns`
- Closes: spec 05 — quote collapse absent; format=flowed absent; strip_patterns absent; html_converter config absent; internetMessageHeaders missing from $select; hardcoded width; no single-flight; [rendering] keys incomplete

---

### Category D — Mailbox context (spec 13)

Spec 13 provides the canonical timezone source of truth and unblocks
spec 14's `inkwell ooo` / `inkwell settings` CLI. Spec 12's
calendar TZ resolution also depends on it.

**PR D-1 — Spec 13 finish** `feat(spec-13): OOF editing + :settings + timezone Manager`
- OOF full editing: scheduled mode with date-picker modal for start/end, audience picker (all / contactsOnly / none), `$EDITOR` drop-out for internal/external message bodies (spec §5)
- `:ooo on` / `:ooo off` / `:ooo schedule <start> <end>` quick commands (spec §11)
- `:settings` modal: read aggregates displayName, timezone, locale, working hours, auto-reply status (spec §6)
- Status bar OOO indicator (`🌴 OOO`) when `status != disabled` (spec §8)
- `settings.Manager` in `internal/settings/`: `ResolvedTimeZone()` reads `mailboxSettings.timeZone`, overridden by `[calendar].time_zone`, falls back to system TZ — used by calendar adapter and search
- 5-minute background refresh timer + force-refresh after any PATCH (spec §4)
- PATCH payload extended with `scheduledStartDateTime` / `scheduledEndDateTime` / `externalAudience` (spec §5.4)
- CLI stubs for `inkwell ooo` / `inkwell settings` (full verb surface in PR G-1)
- `[mailbox_settings]` config section: `confirm_ooo_change`, `default_ooo_audience`, `ooo_indicator`, `refresh_interval`, `default_internal_message`, `default_external_message`
- Closes: spec 13 — OOF read-only beyond enable/disable; no schedule/audience editing; no :settings; no OOO indicator; no timezone source of truth; no refresh timer; PATCH misses schedule fields; no [mailbox_settings] config

---

### Category E — Sync and auth reliability (specs 03, 01)

The goroutine leak in spec 03 is the most dangerous correctness gap
still open — it causes the UI goroutine to block forever on engine
Stop. Ship before spec 14's daemon mode.

**PR E-1 — Spec 03 finish** `feat(spec-03): goroutine fix + tombstone delta + priority queue + config keys`
- Goroutine leak: `consumeSyncEventsCmd` selects on both the events channel and `ctx.Done()`; the engine closes the channel on `Stop()` so the UI goroutine drains cleanly (spec §3 "no goroutine leaks")
- Tombstone-aware delta: `@removed` markers from `/me/mailFolders/delta` propagate as delete ops during initial backfill, not just during steady-state delta (spec §6.2)
- Body-fetch priority queue: on-demand fetches (user opens a message) jump ahead of background backfill traffic in the concurrency semaphore (spec §11)
- `[sync]` config keys: `subscribed_well_known`, `excluded_folders`, `delta_page_size`, `retry_max_backoff`, `prioritize_body_fetches`
- Closes: spec 03 — UI goroutine leak on Stop; tombstone delta absent during backfill; no priority queue; sync config keys absent

**PR E-2 — Spec 01 finish** `feat(spec-01): AADSTS classification + clock-skew + CLI PromptFn`
- AADSTS code classification: parse MSAL error strings for `AADSTS530003` (device compliance), `AADSTS65001` (consent required), `AADSTS70011` (invalid scope); surface the spec §11 friendly messages
- Clock-skew detection: identify MSAL clock-skew error text and surface "System clock is off by more than 5 minutes; please sync your clock" hint
- CLI `PromptFn`: non-TUI device-code flow prints message + URL to stderr per spec §5.4
- Closes: spec 01 — AADSTS classification absent; clock-skew hint absent; CLI PromptFn absent

---

### Category F — Compose completeness (spec 15 finish)

Three correctness gaps and one missing feature remain after the
action-queue + crash-recovery + skeleton work shipped in PRs 7-i/ii/iii.

**PR F-1 — Spec 15 finish** `feat(spec-15): discard DELETE + webLink TTL + Mail.Send guard + attachments`
- Graph DELETE on discard: `updateComposeConfirm` case `"d"` calls `DELETE /me/messages/{draftID}` (spec §6.3)
- WebLink TTL: clear `lastDraftWebLink` 30 s after it is set via a `time.AfterFunc` Cmd; currently set indefinitely (spec §9)
- `Mail.Send` CI lint guard: `scripts/check-no-mail-send.sh` greps for the literal string `Mail.Send` outside `docs/PRD.md` and `internal/auth/scopes.go`; fails CI (spec §11)
- Compose attachments: `DraftParams.Attachments []AttachmentRef`; attach via `POST /me/messages/{id}/attachments`; spec 17 §4.4 path-traversal guard; `[compose]` config section
- Closes: spec 15 — discard doesn't DELETE server draft; webLink never auto-clears; no Mail.Send lint guard; attachments absent

---

### Category G — CLI surface (spec 14)

Spec 14 depends on specs 11, 13, and 15 being shipped first for
`rule`, `ooo`/`settings`, and compose paths. Calendar CLI is
unblocked since spec 12 PR 6b-ii already shipped.

**PR G-1 — Spec 14 build-out** `feat(spec-14): message + rule + calendar + ooo + settings + daemon + exit-codes`
- Depends on: B-1 (rule Manager), D-1 (OOF/timezone Managers), F-1 (compose Graph paths)
- `inkwell message` subcommand: `show` / `read` / `unread` / `flag` / `unflag` / `move` / `delete` / `permanent-delete` / `attachments` / `save-attachment` / `reply` / `reply-all` / `forward`
- `inkwell folder subscribe/unsubscribe/show/tree`
- `inkwell rule list/show/save/edit/delete/eval/apply` (uses spec 11 Manager)
- `inkwell calendar today/week/agenda/show`
- `inkwell ooo on/off/set` (uses spec 13 Manager)
- `inkwell settings` (uses spec 13 Manager)
- `inkwell export`, `inkwell daemon`, `inkwell backfill`
- Exit code mapping per spec §5.3 (currently 0 or 1 only)
- Line-delimited JSON output (replace current enclosing `{"messages": [...]}` array)
- Progress bars on TTY; quiet on pipes
- Global flags: `--output`, `--color`, `--log-level`, `--quiet`, `--no-sync`, `--yes`
- `[cli]` config section: `default_output`, `color`, `confirm_destructive_in_cli`, `progress_bars`, `json_compact`, `export_default_dir`
- Closes: spec 14 — ~60% CLI surface absent; exit codes wrong; array not line-delimited; no progress bars; global flags missing; no [cli] config

---

### Category H — Calendar completion and polish (specs 12, 02, 06, 08)

These are the remaining deferred items after all primary specs ship.

**PR H-1 — Spec 12 finish** `feat(spec-12): sidebar pane + week/agenda + timezone`
- Depends on: D-1 (ResolvedTimeZone from settings.Manager)
- Sidebar calendar pane below "Saved Searches" in folder-pane layout; shows today + next N days (configurable via `[calendar].sidebar_show_days`)
- Week view and agenda toggle (`w` key in calendar list or sidebar)
- `c` key from calendar list opens full-screen week/agenda view
- `mailboxSettings.timeZone` resolution via `settings.Manager.ResolvedTimeZone()` (replaces `time.Now().Date()` in local TZ)
- Closes: spec 12 deferred — sidebar pane; week/agenda view; timezone resolution

**PR H-2 — Spec 02 finish** `fix(spec-02): flag_due_at persistence + saved-search delete-by-name`
- Depends on: B-1 (delete-by-name consumed by Manager.Delete)
- `flag_due_at` / `flag_completed_at` wired through `MessageFields` so the flag action's `due_date` param persists to the DB columns (columns exist in migration 001; just never written)
- `DeleteSavedSearchByName(ctx, accountID, name)` store helper for Manager.Delete
- Closes: spec 02 — flag_due_at not persisted; saved-search delete-by-name absent

**PR H-3 — Spec 06 `--all` flag** `feat(spec-06): cross-folder --all search flag`
- Depends on: G-1 (global flag infrastructure in spec 14)
- `--all` flag for `inkwell filter` and the TUI `/` search: scopes query across all subscribed folders instead of the active one (spec §5.3)

**PR H-4 — Spec 08 CI bench** `bench(spec-08): 100k-message bench + 10k-AST fuzz in CI`
- Enable 100k-message bench gate in CI (deferred during PR 9 ship)
- `go test -fuzz FuzzParse -fuzztime=30s` as a CI fuzz step

## Execution rules

1. **One PR per slice above.** Don't bundle. Bundling makes the
   audit-drain bookkeeping unmanageable.
2. **Each PR's commit message lists the audit rows it closes** by
   the same format used here (`spec NN §X.Y "<row text>"`).
3. **Each PR deletes the closed rows from `docs/audits/spec-1-15-
   impl-gaps.md`** in the same commit. The diff makes the
   bookkeeping mechanical.
4. **Each PR appends an iteration-log entry to
   `docs/plans/spec-NN.md`** for whichever spec it touches. If a
   PR spans multiple specs (e.g. PR 5's `:rule` touches specs 04
   and 11), update both plan files.
5. **Deferred rows go into the spec plan file's "Deferred"
   section** with a one-line WHY, and the audit row gets deleted.
6. **When the audit doc has no rows left** (only headers /
   summary), delete `docs/audits/spec-1-15-impl-gaps.md`. Then
   delete the `docs/audits/` directory if no other audits live
   there. Update `CLAUDE.md` §14 "Where things live" to remove
   the `audits/` line if it's listed (it isn't currently).

## Phase 1 — Status tracker (historical)

| PR | Spec(s) | Status | Branch | Audit rows closed | Plan file updated |
|----|---------|--------|--------|-------------------|-------------------|
| 1  | 07      | shipped (v0.13.x) | main | spec 07 §11 undo + §7.1 inverse | docs/plans/spec-07.md iter 2 |
| 2  | 04      | shipped (v0.13.x) | main | spec 04 §17 [bindings] + §12 help overlay + §6.4 :help | docs/plans/spec-04.md iter 9 |
| 3  | 03      | shipped (v0.13.x) | main | spec 03 §3 ThrottledEvent + AuthRequiredEvent | docs/plans/spec-03.md iter 8 |
| 4a | 07      | shipped (v0.13.x) | main | spec 07 §6.7 permanent_delete | docs/plans/spec-07.md iter 3 |
| 4b | 07      | shipped (v0.13.x) — categories closed; move-with-picker carved as PR 4c | main | spec 07 §6.9 / §6.10 add_category / remove_category | docs/plans/spec-07.md iter 4 |
| 4c | 07      | shipped (v0.13.x) | main | spec 07 §6.5 / §12.1 move-with-folder-picker | docs/plans/spec-07.md iter 5 |
| 5  | 04      | shipped (v0.13.x) | main | spec 04 §6.4 :refresh / :folder / :open / :backfill / :search | docs/plans/spec-04.md iter 10 |
| 5b | 04 (+11)| superseded by Phase 2 PRs B-1 (spec 11 Manager) + B-2 (spec 04 :save/:rule) | — | — | — |
| 6a | 12      | shipped (v0.13.x) | main | spec 12 §3 events schema + persistence | docs/plans/spec-12.md iter 2 |
| 6b-i | 12    | shipped (v0.13.x) | main | spec 12 §6.2 j/k/Enter + §4.3 GetEvent + §7 detail modal | docs/plans/spec-12.md iter 3 |
| 6b-ii | 12   | shipped (v0.21.0) | main | spec 12 §4.2 delta sync + §5 engine 3rd state + §5.1 midnight slide + §6.2 day nav (]/[/{/}/t) + §3 event_attendees + attendees persistence | docs/plans/spec-12.md iter 4 |
| 7-i | 15    | shipped (v0.13.x) | main | spec 15 §5 / §8 "drafts bypass action queue" | docs/plans/spec-15.md iter 2 |
| 7-ii | 15   | shipped (v0.13.x) | (this branch) | spec 15 §7 compose_sessions migration + crash-recovery resume + 24h GC | docs/plans/spec-15.md iter 4 |
| 7-iii | 15  | shipped (v0.13.x) | (this branch) | spec 15 §5 ReplyAll/Forward/NewMessage action types + skeletons + R/f/m bindings | docs/plans/spec-15.md iter 5 |
| 8  | 06      | shipped (v0.17.x) | (this branch) | spec 06 streaming Searcher + graph $search + merger + field prefixes + UI streaming integration | docs/plans/spec-06.md iter 2 |
| 9  | 08      | shipped (v0.18.x) | (this branch) | spec 08 §6 Compile/Execute API + §9 $filter + §10 $search + §11 TwoStage + [pattern] config | docs/plans/spec-08.md iter 2 |
| 10 | 05 (+17)| shipped (v0.20.0) | main | spec 05 §8 GetAttachment + save/open; §11 thread map; §12 viewer keybindings (`o`/`O`/`1-9`/`[`/`]`/`a-z`/`A-Z`); spec 17 §4.4 path-traversal guard | docs/plans/spec-05.md iter 6 |
| 11 | 02      | shipped (v0.13.x) | main | spec 02 §8 maintenance loop | docs/plans/spec-02.md iter 3 |
| 12 | config  | partial (v0.13.x) — runtime-consumed [triage]/[bulk]/[calendar] sections shipped; remaining sections dissolve into Phase 2 spec PRs | main | spec 02 §17 / spec 04 §17 / spec 12 §config | docs/plans/spec-04.md notes |

## Phase 2 — Status tracker

| PR | Spec(s) | Status | Audit rows closed | Plan file updated |
|----|---------|--------|-------------------|-------------------|
| A-1 | 07 | done | spec 07 §5 InFlight; §5.5 move-id stale; §10 replay-on-startup; [triage] config | 2026-05-02 |
| A-2 | 09 | done | spec 09 §8 retry; §7 concurrency; bulk undo; [batch] config | 2026-05-02 |
| A-3 | 10 | done | spec 10 §4 6 missing bulk verbs; §5 F keybind; §8 confirm sample | 2026-05-02 |
| B-1 | 11 | done \| 2026-05-02 | spec 11 §2 Manager API; §5 live counts; §4 CRUD commands; §7.3 seed defaults; §8 TOML mirror; [saved_search] config | 2026-05-02 |
| B-2 | 04 | done \| 2026-05-03 | spec 04 §14 lifecycle teardown; §5 transient_status_ttl; §13 min_terminal; :save/:rule; indicator config | 2026-05-03 |
| C-1 | 05 | done \| 2026-05-03 | spec 05 §6.3 quote collapse; §7 format=flowed; §6.5 strip_patterns; html_converter; §5.2 internetMessageHeaders; single-flight; [rendering] keys | 2026-05-03 |
| D-1 | 13 | done \| 2026-05-04 | spec 13 §5 OOF editing; §11 :ooo variants; §6 :settings; §8 OOO indicator; timezone Manager; §4 refresh timer; PATCH schedule fields; [mailbox_settings] config | 2026-05-04 |
| E-1 | 03 | done \| 2026-05-04 | spec 03 §3 goroutine leak; §6.2 tombstone delta backfill; §11 priority queue; [sync] config keys | — |
| E-2 | 01 | done \| 2026-05-04 | spec 01 §11 AADSTS classification; clock-skew hint; §5.4 CLI PromptFn | 2026-05-04 |
| F-1 | 15 | done \| 2026-05-04 | spec 15 §6.3 discard DELETE; §9 webLink TTL; §11 Mail.Send lint guard; §5 compose attachments | 2026-05-04 |
| G-1 | 14 | done \| 2026-05-04 | spec 14 §6 message/rule/calendar/ooo/settings/export/daemon/backfill subcommands; §5.3 exit codes; §5.2 line-delimited JSON; progress bars; global flags; [cli] config | 2026-05-04 |
| H-1 | 12 | done \| 2026-05-04 | spec 12 deferred sidebar pane; week/agenda view; timezone resolution; `c` key from folders pane | docs/plans/spec-12.md iter 5 |
| H-2 | 02 | not-started | spec 02 flag_due_at persistence; saved-search delete-by-name | — |
| H-3 | 06 | not-started | spec 06 §5.3 --all cross-folder flag | — |
| H-4 | 08 | not-started | spec 08 CI 100k bench + 10k-AST fuzz gate | — |

## Real-tenant gaps (outside the audit)

These are bugs surfaced by real-tenant smoke or user reports that
weren't in the original spec 1–15 audit. They drain alongside the
audit-drain queue with the same bookkeeping discipline (commit
message lists what's closed; spec plan file gains an iter entry).

| PR | Spec(s) | Status | Branch | Bug closed | Plan file updated |
|----|---------|--------|--------|------------|-------------------|
| RT-1 | 03 | shipped (v0.13.x) | main | nested folders never synced (`/me/mailFolders` is non-recursive); `o`/Enter on Inbox showed no children even when server-side children existed. Switched `syncFolders` to `/me/mailFolders/delta` which returns the full tree flat. Delta token persistence deferred. | docs/plans/spec-03.md iter 9 |
| RT-2 | 05 | shipped (v0.13.x) | main | OSC 8 hyperlinks that wrap across visual rows highlighted only the hovered row on Cmd-click hover. Added a stable `id=u<fnv32>` parameter so terminals (Ghostty, iTerm2, kitty, wezterm, ghostty) group every fragment of the same URL as one logical link. Same id used for repeat occurrences in the same body — hovering any one highlights all. | (no spec plan; behaviour-only fix in `internal/render/links.go`; covered by render unit tests) |
| RT-3 | 04 | shipped (v0.13.x) | main | List-pane rows containing the 📅 invite glyph (1 rune, 2 cells) or CJK names overshot the configured pane width because `truncate()` sliced by rune count instead of visual cell width. The right-edge characters spilled past the pane until the user resized the terminal (Ghostty regression). Fix: `truncate` now walks runes accumulating `runewidth.RuneWidth()` and stops at the cell budget. | (no spec plan; UI-layer fix in `internal/ui/panes.go`; covered by dispatch unit tests) |

When all rows show "shipped" and the audit doc is empty, this
plan file (`audit-drain.md`) gets a final commit deleting it
along with the audit doc.
