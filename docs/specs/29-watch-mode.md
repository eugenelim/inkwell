# Spec 29 — Watch mode

**Shipped:** v0.58.0.
**Status:** Shipped.
**Depends on:** Spec 02 (local cache — read-only `ListMessages` /
`SearchByPredicate`), Spec 03 (sync engine — `Notifications()` channel,
`Wake()`, `SetActive(true)`, `Done()`), Spec 08 (pattern language —
`pattern.Parse` / `pattern.CompileLocal`), Spec 11 (saved searches —
`--rule <name>` resolves to a stored pattern), Spec 14 (CLI mode —
`messages` subcommand, exit codes, `--output` / `--quiet` /
`--no-sync` / `--config` global flags, `headlessApp` constructor).
**Enables:** Nothing blocks on this. Custom-actions framework
(ROADMAP §2) may eventually pipe `inkwell watch …` matches into a
user-defined hook, but that is a future spec.
**Estimated effort:** 0.5–1 day. The roadmap calls watch mode
"trivial to implement" (§1.19) and that is accurate: the engine
already emits the events watch mode needs, the pattern compiler and
store query already exist, and `messages --filter` already produces
the JSON shape the watch stream emits.

### 0.1 Spec inventory

Watch mode is item 3 of Bucket 3 ("Power-user automation") in
`docs/ROADMAP.md` §0 and corresponds to backlog row §1.19. Bucket 3
is unblocked once the Bucket 2 inbox-philosophy specs (22–26) are
in flight; spec 29 has zero hard dependencies on Bucket 2 (the
`--rule` integration with spec 11 is the only optional cross-link
and it is already shipped). The PRD §10 spec inventory adds a single
row for spec 29.

This spec is numbered 29 because it is the next available slot above
the shipped tail (26 — bundle senders) plus the two slots already
implicitly claimed by in-flight Bucket 2 / Bucket 3 work (custom
actions framework prep, screener). No reservation policy is being
established by this spec; if 27 / 28 are not authored before this one
ships, the gap simply remains.

---

## 1. Goal

Continuously emit new messages matching a user-supplied filter to
stdout, without exiting, until the user interrupts. The roadmap
example is the canonical use case:

```bash
inkwell messages --filter '~U & ~f vip@*' --watch
```

That command should behave like `tail -f` over the user's mailbox:
print one line per new VIP unread message as it lands, and never
print the same message twice. It should compose with shell pipelines
the same way every other `inkwell` CLI subcommand does — `| jq`,
`| head -10`, `| tee`, `| xargs inkwell message read`, all work.

Watch mode is a thin loop on top of three things that already exist:

1. The local SQLite cache, which is the source of truth for envelope
   state (spec 02). Watch never queries Graph directly.
2. The sync engine's notification channel (spec 03), which fires a
   `SyncCompletedEvent` every foreground cycle (default 30s) and a
   `FolderSyncedEvent` per folder. Watch re-evaluates the filter on
   each event.
3. The spec 08 pattern compiler + spec 11 saved-search manager,
   which already turn a string filter into a SQL clause that
   `store.SearchByPredicate` runs in <10 ms over 100k messages.

Watch mode adds: a flag, an event-loop wrapper around the existing
filter listing, an in-memory dedup set keyed by message ID, signal
handling, and a small status line on stderr. No new schema, no new
Graph endpoint, no new pattern operator, no new config section.

### 1.1 What does NOT change

- The TUI (`internal/ui/`) is unaffected. Watch mode is CLI-only.
- The sync engine's contract (spec 03) is unchanged. Watch is a
  consumer of `Notifications()`, identical in shape to the TUI's
  `consumeSyncEventsCmd` (`internal/ui/app.go:5929`).
- The action queue (spec 07/09), undo stack, and Graph $batch path
  are untouched. Watch is read-only — it never enqueues an action.
- The spec 08 pattern grammar is not extended. Whatever
  `inkwell messages --filter X` accepts today, `--watch` accepts
  unchanged.
- The spec 14 exit-code map is unchanged. Watch reuses
  `internal/cli/exitcodes.go` constants verbatim.
- The Message JSON envelope shape on `--output json` is whatever
  `inkwell messages --output json` already emits (the `store.Message`
  Go struct, encoded by `encoding/json` with default field-name
  rules). Watch must NOT introduce a divergent envelope.
- No background daemon is required. Watch is a foreground process.
  `inkwell daemon` (spec 14 §7) is orthogonal — see §5.6 for
  cohabitation.

## 2. Prior art

### 2.1 Terminal CLI mail clients

- **himalaya (`pimalaya/himalaya`)** — has `himalaya envelope watch`
  / `notify` that uses IMAP IDLE (RFC 2177) to push notifications
  from the server. Output is one line per new envelope; `--hook`
  fires a shell command; `notify` triggers a desktop notification.
  Closest direct analogue. inkwell is on Microsoft Graph not IMAP,
  so we cannot use IDLE — but the surface (`watch` flag, one-line-
  per-new-envelope, hook-friendly) is what we copy.
- **mutt / neomutt** — `$mail_check` (default 5 s) polls the
  mailbox; `$imap_idle` enables RFC 2177 push for IMAP backends;
  `new_mail_command` shells out on each new message. These are
  TUI-internal, not CLI; the equivalent here is a shell pipeline
  on watch mode's stdout.
- **aerc** — IMAP IDLE in the TUI; no separate CLI watch.
- **notmuch** — no `watch`; users compose `tail -f mail_log` on the
  notmuch hooks output, or run `notmuch search tag:unread` on a
  cron / `inotifywait` loop. Watch mode replaces that bespoke
  shell glue with one flag.
- **mu / mu4e** — same as notmuch: no first-class watch.
- **fdm / isync / offlineimap** — fetcher daemons; the "watch"
  semantics live in the daemon, not in a per-query loop. inkwell's
  sync engine is the equivalent fetcher; watch mode is the per-
  query loop on top.

### 2.2 Web / desktop clients

- **Gmail / Outlook web** — server-side push to the browser; not a
  shell-pipe-able surface. Out of scope.
- **HEY** — "The Feed" pane is a UI surface, not a CLI. The opt-in
  routing it provides is delivered by spec 23 in inkwell, not here.
- **Apple Mail / Spark / Spike / Newton** — desktop apps; no CLI;
  rely on push (APNs / proprietary). N/A.

### 2.3 Adjacent shell tools

- **`tail -f`** — the canonical "follow" UX on a file. Watch mode's
  visible behaviour matches: append-only output, no buffer rewrite,
  Ctrl-C exits 0, broken pipe on `| head` exits 0.
- **`imsg watch`** (Apple Messages CLI) — `--json` emits one JSON
  object per line, fallback poll if file events are missed,
  `--debounce` between rapid bursts. We adopt the JSONL-on-`--output
  json` shape and the missed-event safety net (every cycle re-
  evaluates against the cache, even if `Notifications()` dropped a
  message).
- **`jsonwatch`** (`dbohdan/jsonwatch`) — diffs JSON over time.
  Conceptually adjacent; not adopted (we emit new rows, not diffs).

### 2.4 Microsoft Graph push (deliberate non-goal)

Graph offers `subscriptions` / change notifications for
`/me/messages`. These require a publicly addressable HTTPS endpoint,
expire after 45 minutes – 7 days depending on the resource, and cap
at 1000 active subscriptions per mailbox. A local CLI cannot host
the webhook target without a tunnel (ngrok / Cloudflare). Watch
mode therefore polls via the existing sync engine cadence.
Implications:

- Latency: a new message lands in stdout after at most one
  foreground sync interval (default 30 s) plus the per-cycle delta
  fetch (~50–200 ms). This is consistent with PRD §7's user-facing
  latency budgets — see §6 for the formal targets.
- A future spec MAY add a Graph subscription path if and when
  inkwell ships a long-lived daemon with HTTPS routing. Watch mode
  remains the script-friendly surface either way.

### 2.5 Design decision

inkwell follows a deliberately narrow model:

- **CLI flag, not a separate subcommand.** The roadmap example is
  `inkwell messages --filter X --watch`; we honour it. Adding a
  top-level `inkwell watch` would duplicate the messages flag set
  with no upside. The flag goes on `messages`.
- **Local cache is the source.** Watch never queries Graph directly.
  Re-evaluation on each `SyncCompletedEvent` reads only the local
  store. This keeps watch within the spec 03 envelope and means a
  user can run several watch processes concurrently with no API
  cost.
- **Dedup by message ID, not by content.** A message updated
  in-place (read flag flip, flag toggle) does NOT re-emit by
  default. `--include-updated` opts in to re-emission on
  `last_modified_at` change. This matches `tail -f` semantics:
  follow new arrivals, ignore in-place edits.
- **Append-only output.** No screen redraw, no progress bar
  overwriting prior lines, no ANSI cursor movement on stdout.
  Stderr carries an optional one-line status when stderr is a TTY;
  stdout is pipeline-clean.
- **No new pattern syntax.** `--filter` accepts whatever spec 08
  parses. `--rule <name>` resolves a saved-search pattern via spec
  11 and feeds it to the same compiler.
- **Foreground process.** Watch holds the user's terminal. Real
  daemonisation (launchd plist) is ROADMAP §1.21 — out of scope.

## 3. Schema

None. Watch mode is read-only and adds no table, no column, no
migration.

## 4. Module layout

```
cmd/inkwell/
├── cmd_messages.go         # add --watch / --interval / --initial /
│                           #     --include-updated / --count /
│                           #     --for flags; dispatch into watch
├── cmd_watch.go            # new file: runWatch() loop, signal
│                           #     handling, status line, dedup set
└── cmd_watch_test.go       # new file: unit tests with a fake
                            #     engine and synthetic store

internal/cli/
└── (no changes — exit codes already cover every watch failure
   mode. doc.go gains a one-line note that watch mode reuses the
   existing constants.)
```

Files under `internal/sync/`, `internal/store/`, `internal/pattern/`,
`internal/ui/`, and every other package are untouched.

The watch loop lives in `cmd/inkwell/cmd_watch.go` (not in
`internal/cli/`) because it is glue over the headless app, not a
reusable library function. Keeping it in the cmd binary keeps the
test surface narrow and avoids growing `internal/cli/` beyond exit
codes.

## 5. Behaviour

### 5.1 Surface

Add five flags to the existing `inkwell messages` command (top-level
`messages` only, NOT to its sub-verbs `messages show / read / …`):

| Flag | Type | Default | Purpose |
| ---- | ---- | ------- | ------- |
| `--watch` | bool | `false` | Enable watch mode. Ignored if any sub-verb is supplied. |
| `--interval <duration>` | duration | engine `[sync].foreground_interval` (typ. 30s); minimum 5s | Cadence for re-evaluating the filter against the cache when no `SyncCompletedEvent` has fired. |
| `--initial <N>` | int | `0` | Print the most-recent N matches at startup before entering the watch loop. `--initial=0` (default) starts strictly from "now" — no backlog dump. |
| `--include-updated` | bool | `false` | Re-emit a message that was previously emitted when its `last_modified_at` advances (read/flag/folder change). Default: emit each message ID at most once per process. |
| `--count <N>` | int | `0` (unbounded) | Exit 0 after N new matches have been emitted (counted across the watch loop, NOT including `--initial` rows). |
| `--for <duration>` | duration | `0` (unbounded) | Exit 0 after this wall-clock duration elapses, regardless of how many matches were emitted. |

`--watch` requires `--filter` OR `--rule`. Specifying `--watch`
without either is a usage error (`exit 2`, message: `--watch
requires --filter <pattern> or --rule <name>`).

`--rule <name>` resolves a saved search via spec 11's manager.
Concretely: construct the manager (`savedsearch.NewManager(...)`,
following the wiring used by `cmd_rule.go`), call
`mgr.Get(ctx, name)` — signature
`Get(ctx, name) (*store.SavedSearch, error)` per
`internal/savedsearch/manager.go:54` — and use `ss.Pattern` as the
pattern source. The `Get` method returns `(nil, nil)` for
not-found; watch translates that to `ExitNotFound` (5) with
message `not found: rule "<name>"`, matching the shape
`cmd_rule.go` already uses for missing rules. The `Manager` needs
the saved-search config block from `cfg.SavedSearch`; watch calls
`rc.loadConfig()` (already idempotent — `cmd_root.go:91`) before
constructing the manager.

**Mutual-exclusivity matrix** (enforced via cobra's
`MarkFlagsMutuallyExclusive`; usage error → exit 2):

| Pair | Reason | Error string |
| ---- | ------ | ------------ |
| `--filter` × `--rule` | Both supply the watch-mode pattern; only one source of truth. | `--filter and --rule are mutually exclusive` |
| `--watch` × `--limit` | `--limit` caps a one-shot listing; `--initial=N` is the watch equivalent. | `--limit and --watch are mutually exclusive — use --initial=N to print N startup rows before watching` |
| `--watch` × `--unread` | The one-shot path silently drops `--unread` when `--filter` is set (`cmd_messages.go:58–70`); rather than perpetuate that surprise in watch mode (where `--filter`/`--rule` are mandatory), require the user to express unread-only as a pattern: `--filter '~U & …'`. | `--unread is not supported with --watch — use --filter '~U & …' instead` |

`--all` (cross-folder, spec 21) is honoured by watch mode the same
way `messages --all --filter X` honours it today (text path uses the
flat `printMessageList`, no FOLDER column — see §5.2).

### 5.2 Output

**Stdout** is the event stream. Format:

- `--output text` (default): one line per match, identical to
  `printMessageList` (`cmd/inkwell/cmd_messages.go:128`):

  ```
  RECEIVED            FROM                       SUBJECT
  2026-05-08 09:14    Alice <alice@vip.example>  Q2 forecast review
  ```

  At startup the header row is printed exactly once. Subsequent
  matches print as data rows only — no repeated header. If
  `--initial=0` AND no events have arrived yet, no header is
  emitted (so a quiet pipe stays quiet).

- `--output json` (or any other JSON-flavoured value resolved by
  `effectiveOutput`): **JSONL — one JSON object per line, no
  enclosing array, no separator beyond `\n`**. The object is the
  same `store.Message` value the encoder produces today
  (`store.Message` carries no `json:"…"` tags, so field names are
  the Go field names verbatim — `ID`, `Subject`, `FromAddress`,
  …). Per-line emission is via a per-row `json.Marshal` followed by
  a write of `bytes + "\n"` directly to `os.Stdout`; the kernel
  pipe buffer takes over from there. A downstream `jq` / `head`
  sees rows as soon as the kernel flushes the pipe write.

  ```jsonl
  {"ID":"AAMk…","Subject":"Q2 forecast review","FromAddress":"alice@vip.example",…}
  {"ID":"AAMk…","Subject":"Re: deploy","FromAddress":"bob@vip.example",…}
  ```

  **Shape divergence from one-shot `messages --output json`.**
  Today's one-shot path encodes the entire result slice in one
  call (`json.NewEncoder(os.Stdout).Encode(msgs)` —
  `cmd/inkwell/cmd_messages.go:76`), which produces a **single
  JSON array**, not JSONL. That contradicts spec 14 §5.2's
  aspirational design ("line-delimited JSON for long lists; one
  object per line, no enclosing array"). Watch mode is the first
  caller to fulfil the §5.2 contract. Spec 29 deliberately does
  NOT migrate the one-shot path: changing it is a behavioural
  change for existing pipelines that consume the array shape,
  and that migration belongs in a follow-up that updates spec 14
  in the same commit. Until then:

  - `inkwell messages --output json` continues to emit a JSON array.
  - `inkwell messages --filter X --watch --output json` emits JSONL.
  - A unit test (`TestWatchJSONLOneObjectPerLineNoArray`, §8.1)
    pins the watch shape; a parallel test
    (`TestOneShotMessagesJSONStillArrayShape`, §8.1) pins the
    one-shot shape so a future migration trips an explicit
    failure rather than silently breaking pipelines.

- **`--all` cross-folder mode**: watch under `--all` emits the same
  flat text layout the one-shot `inkwell messages --all --filter X`
  emits today — no FOLDER column (the one-shot uses
  `printMessageList`, not `printMessageListWithFolder`; the latter
  is currently exclusive to `inkwell filter --all`). Users who need
  the folder name should use `--output json` (folder ID is already
  in the envelope; map to display name client-side). Adding a
  FOLDER column to `messages --all` is a separate UX change that
  is out of scope here and would need to update both the one-shot
  and watch paths in the same commit.

**Stderr** carries optional status:

- When stderr IS a TTY AND `--quiet` is NOT set, watch prints a
  single rolling status line, redrawn in place using `\r` + ANSI
  erase-to-EOL (no scrollback pollution):

  ```
  ⏱ watching: 18 seen · last sync 12s ago · uptime 03:14
  ```

  The line redraws on each event (start of sync, end of sync,
  match emitted) and on a 1-second wall-clock timer. The leading
  `⏱` glyph is omitted under `--color=never`.

- When stderr is NOT a TTY (e.g. `2>watch.log`) OR `--quiet` is
  set, status is suppressed entirely. Auth and throttle events
  (§5.4) still print, but as one-shot stderr lines (no `\r`).

- On exit (signal or `--count` / `--for` boundary), watch prints
  one final summary line to stderr (suppressed under `--quiet`):

  ```
  ✓ watched for 47m12s — 18 new matches, 1 sync failures, 0 throttle events
  ```

The newline-flush discipline is mandatory: every match is written
to `os.Stdout` directly (no in-process `bufio.Writer` wrapper —
existing cobra subcommands in this repo also write directly to
`os.Stdout`, e.g. `cmd_messages.go:75–77`). For text mode the helper
is one `fmt.Fprintf(os.Stdout, …)` per row plus the once-per-process
header. For JSON mode the helper is one `json.Marshal` plus a single
`os.Stdout.Write(append(b, '\n'))`. A consumer running
`inkwell messages --filter X --watch | head -3` must see each line
as soon as it is emitted, bounded only by the kernel pipe buffer
(4 KiB on Linux, 16 KiB on macOS) — confirmed by
`TestWatchEmitsLineByLineUnderPipe` (§8.1).

### 5.3 Loop semantics

Pseudocode (the actual implementation lives in `runWatch` in
`cmd/inkwell/cmd_watch.go`):

```
runWatch(ctx, app, opts):
    pattern  = compile(opts.filter)            # spec 08
    folderID = resolveFolder(opts.folder)      # may be ""
    seen     = newSeenSet(opts.maxSeen)        # bounded LRU of msg IDs
    deadline = time.Now().Add(opts.forDuration) // zero = no deadline

    if opts.initial > 0:
        rows = evaluate(pattern, folderID, limit=opts.initial)
        for r in rows: emit(r); seen.add(r.ID, r.LastModifiedAt)
        statusUpdate("initial dump: %d", len(rows))

    # `evaluate` is `runFilterListing` (cmd_filter.go:156): it
    # parses + compiles the pattern via spec 08 and queries
    # `store.SearchByPredicate` with the compiled WHERE/args plus
    # an optional folder_id constraint. SearchByPredicate does NOT
    # consult muted_conversations (the ExcludeMuted knob is on
    # MessageQuery/ListMessages, not on SearchByPredicate), so
    # muted-thread rows surface in watch output if they match the
    # filter — same as one-shot `inkwell messages --filter X`.

    eng = startEngineIfNotRunning(app)         # see §5.6
    eng.SetActive(true)                        // foreground 30s cadence

    timer = time.NewTimer(opts.interval)
    emitted = 0

    for:
        select:
        case <-ctx.Done():       return 0
        case <-eng.Done():       return 0     // engine stopped → exit cleanly
        case ev := <-eng.Notifications():
            if isAuthRequired(ev): warnOnce(stderr, "auth required — run `inkwell signin`"); continue
            if isThrottled(ev):    warnOnce(stderr, "throttled — backing off"); continue
            if isSyncFailed(ev):   warn(stderr, ev.err); continue
            if isSyncCompleted(ev) || isFolderSynced(ev) && (folderID == "" || ev.FolderID == folderID):
                rows = evaluate(pattern, folderID, limit=opts.maxBatch)
                emitNew(rows)                  // diff against `seen`
        case <-timer.C:
            // safety-net poll: re-evaluate even if no event arrived
            // (e.g. delta returned no changes for this folder)
            rows = evaluate(pattern, folderID, limit=opts.maxBatch)
            emitNew(rows)
            timer.Reset(opts.interval)

        if opts.count > 0 && emitted >= opts.count:    return 0
        if opts.forDuration > 0 && time.Now().After(deadline): return 0
```

Where `emitNew(rows)`:

- For each row, key on `r.ID`. If the ID is new to `seen`, emit.
  If `--include-updated` is set AND the seen-set's stored
  `last_modified_at` predates `r.LastModifiedAt`, emit and update
  the stored timestamp. Otherwise skip.
- Increment `emitted` per emission.
- Update the seen-set in time order (LRU eviction at
  `opts.maxSeen`, default 5000 — see §5.5).

### 5.4 Engine event handling

Watch consumes the same channel the TUI does
(`internal/ui/app.go:5929`). Per-event behaviour:

| Event | Watch reaction |
| ----- | -------------- |
| `SyncStartedEvent` | Update status: `last sync: in flight`. |
| `SyncCompletedEvent` | Re-evaluate filter; emit new matches; reset safety-net timer. |
| `FolderSyncedEvent` | Same as above but only when `folderID == ""` OR `ev.FolderID == folderID`. |
| `FoldersEnumeratedEvent` | Ignored. Watch operates on existing folder ID; if the user-supplied folder name no longer resolves, that surfaces at startup, not mid-loop. |
| `SyncFailedEvent` | Print one line on stderr (`! sync failed: <err>`), do NOT exit, do NOT exit non-zero. The next cycle will retry. |
| `AuthRequiredEvent` | Print one line on stderr; suppress repeat copies within a 60s wall-clock window (anchored to the last printed line) so a refresh storm produces at most one line per minute. Watch does NOT exit on first auth failure — interactive sign-in takes longer than one cycle and a transient token-refresh hiccup should not kill a long-running tail. **Auth recovery is NOT automatic in this watch process**: the engine holds an in-memory MSAL token cache, and a successful `inkwell signin` in a sibling shell updates the keychain blob but not the in-process cache. Users must restart the watch after re-signing in; stderr surfaces a hint to that effect. If `AuthRequiredEvent`s persist for ≥10 minutes wall-clock with zero intervening `SyncCompletedEvent`, watch exits with `ExitAuthError` (3) so cron / launchd / supervisord notice. The 10-minute threshold is wide enough for a human to complete an interactive device-code sign-in (typically ≤2 minutes) and notice the watch is dead before it gives up. |
| `ThrottledEvent` | Print one line on stderr, continue. The engine handles backoff internally. |

Watch never re-emits to the engine (no `Wake()` calls on each tick;
the engine's own timer drives cadence). The exception: if the user
passes `--no-sync` watch does NOT start an engine at all (§5.6).

### 5.5 Dedup set

The seen-set is an in-memory `map[string]time.Time`
(`messageID → last_modified_at`) bounded by an LRU. Constraints:

- Default capacity: 5000 IDs. Configurable via `[cli].watch_max_seen`
  (new key — see §9 doc-sweep, §10 cross-cutting).
- Eviction policy: when the map is full, evict the oldest-emitted
  entry. The cost: a message that was emitted once, then evicted,
  then re-evaluated would re-emit. In practice Graph immutable IDs
  do not "arrive again" within the same folder, so eviction is
  purely a memory bound. Memory math at default capacity:
  5000 entries × (≈160-byte Graph ID string + 24-byte
  `time.Time` + ≈48-byte map overhead) ≈ 1.1 MB. Raising to
  100 000 costs ≈22 MB, still well below the §6 RSS budget.
- **Folder moves mint a new Graph ID.** A message moved between
  folders (e.g. by a routing rule per spec 23, by the user, or by
  an external client) gets a fresh `id` from Graph — the old ID is
  invalidated. Under `--all`, watch will emit the destination row
  as a new event because the dedup set has never seen the new ID.
  This is correct behaviour for "tail every folder", but users who
  want to suppress re-emission on move can scope with `--folder`
  or filter the destination out (`~G !"Archive/X"`).
- The set persists for the lifetime of the watch process. There is
  NO disk persistence: a restarted watch starts fresh with
  `--initial=0` as the default behaviour. Users who want
  resumable-watch state can pipe through `tee >> watch.log` and
  filter with `awk` / `jq` themselves. Persisting state to disk
  would mean a new file under `~/Library/Application Support/inkwell/`
  (privacy / spec 17 surface) for almost no payoff.
- The dedup key is `r.ID` (Graph message ID, immutable while the
  message stays in its folder). The `--include-updated` path
  additionally compares `r.LastModifiedAt`; if the key is present
  but the stored timestamp predates the row's, the row re-emits.
  Equal timestamps do not re-emit. Strictly-newer timestamps only.
- **Shutdown semantics.** On clean shutdown (SIGINT / SIGTERM /
  SIGHUP / `--count` reached / `--for` elapsed) watch returns from
  the loop without running a final safety-net poll. Matches that
  arrived between the last cycle and the shutdown signal will
  surface on the next watch invocation (after the engine commits
  them to the cache); they are not lost.

### 5.6 Cohabitation with the TUI / daemon

Watch starts its own sync engine via `isync.New(...)` and
`Start(ctx)`, mirroring `cmd_sync.go:41–46` and `cmd_daemon.go:40–48`.
SQLite WAL mode allows multiple readers and a single writer; the
delta-token rows are upsert-on-conflict, so concurrent writers
converge.

Cohabitation matrix:

| Other process | Watch behaviour |
| ------------- | --------------- |
| None | Watch starts its own engine on `Start(ctx)`; `SetActive(true)` keeps it on the foreground (30s) cadence; engine writes delta tokens and message rows. |
| Another `inkwell watch …` | Both engines run concurrently. Each updates delta tokens; the second engine's writes are no-ops if the first already advanced the cursor. Cost: 2× HTTP traffic. Suboptimal but correct. We do NOT detect the other watch. |
| `inkwell daemon` | The daemon also runs an engine. Both engines pull deltas independently and write to the cache; SQLite WAL tolerates this. Cost: 2× HTTP traffic. The recommended pattern is to pass `--no-sync` to watch processes when a daemon is running — only the daemon syncs, the watches just tail the cache via the safety-net timer (see below). Watch does NOT auto-detect the daemon: today's `inkwell daemon` (`cmd/inkwell/cmd_daemon.go`) does NOT write a PID file, so there is nothing to probe. The §11 (out of scope) line item flags this: a future spec MAY add a daemon PID file and switch watch to auto-detect-and-fall-back-to-cache-poll. |
| The TUI (`inkwell` with no subcommand) | The TUI also runs an engine. Same shape as the daemon case: 2× HTTP, correctness preserved by WAL + upsert-on-conflict. Pass `--no-sync` to watch when the TUI is running on the same machine for the same account. |
| `--no-sync` flag (the spec 14 global) | Watch interprets `--no-sync` as "do not start an engine in this watch process". The safety-net timer (§5.3) is the only re-evaluation trigger; the watch consumes whatever the cache contains, including rows written by another inkwell process (TUI, daemon, sibling watch). This is a watch-mode-specific extension of the global flag's existing meaning ("use cached data only, skip sync"); see §10 spec-14 consistency for the explicit declaration. |

The "two engines fight" cases are correct-but-wasteful; we accept
the cost rather than build a process-coordination protocol that
this small a feature does not warrant. The `--no-sync` escape hatch
covers every workflow where the user wants exactly one syncer and N
tailers.

### 5.7 Signal handling

- **SIGINT** (Ctrl-C) / **SIGTERM**: cancel the watch context, wait
  up to 2 s for the engine to stop (`eng.Stop(ctx)` semantics —
  spec 03 §6), flush stdout, print the exit summary, exit 0. A
  second SIGINT within the 2 s grace inside the same process exits
  immediately with code 130 (128 + SIGINT) without flushing —
  matches `git`, `make`, and standard Unix convention.
- **SIGPIPE** (`| head -3` closes the read side): on POSIX, the Go
  runtime already special-cases SIGPIPE for stdout and stderr —
  writes to a closed stdout return `*os.PathError` wrapping
  `syscall.EPIPE`, and the program is NOT killed by the kernel
  signal. Watch therefore does not call `signal.Ignore` (which
  would change semantics for downstream goroutines that legitimately
  use SIGPIPE on non-stdout fds). Instead, every emit goes through
  one helper that, on write error, checks `errors.Is(err, syscall.EPIPE)`
  and exits 0 cleanly — never panic, never print `Error: write
  stdout: broken pipe`. Windows is not in scope (CLAUDE.md §1
  pure-Go stack invariant; Linux build is roadmapped at §4.1, macOS
  is the first release target — both POSIX). When a Windows port
  lands, this section becomes a spec-29.x scope; until then watch
  builds only on POSIX targets.
- **SIGHUP**: same as SIGTERM (clean exit 0). No config-reload
  semantics — watch reads its config once at startup and never
  re-reads. A user who edits config and wants new behaviour
  restarts the watch.
- **SIGQUIT**: default Go behaviour (dump goroutines, exit 131).
  Useful for debugging hangs.

### 5.8 Exit codes

Watch reuses `internal/cli/exitcodes.go` constants:

| Code | When watch returns it |
| ---- | --------------------- |
| 0 | Normal exit: SIGINT/SIGTERM/SIGHUP/SIGPIPE, `--count` reached, `--for` elapsed, engine `Done()` channel closed. |
| 1 | Generic engine startup failure (e.g. store open failed for a reason other than "not signed in"). |
| 2 | Usage error: `--watch` without `--filter`/`--rule`; mutually-exclusive flags supplied; bad pattern (parser error); `--initial < 0`; `--interval < 5s` (clamped at the same `minSyncInterval` the engine uses, `internal/sync/engine.go:484`). |
| 3 | Auth error: ≥10 minutes wall-clock of `AuthRequiredEvent`s with zero intervening `SyncCompletedEvent` (§5.4). |
| 5 | Folder not found (when `--folder` resolves to no row in the local cache). Identical to the one-shot `messages` command's behaviour. |

`ExitNetError` (4), `ExitNeedConfirm` (6), `ExitThrottled` (7), and
`ExitForbidden` (8) are NOT exit codes watch ever returns. Watch
treats network errors and throttling as transient (see §5.4); it is
read-only so confirm/forbid never apply.

### 5.9 Privacy & redaction

- **Stdout output** is the same envelope already produced by
  `messages --output json` — subject and `bodyPreview` included.
  This is by design (the user is asking to see the matches). The
  user controls where stdout goes; nothing is logged to disk by
  watch beyond what the engine itself logs.
- **slog logging** routes through the existing redactor
  (`internal/log/redact.go`). Watch logs at INFO when the loop
  starts and stops; at DEBUG on each cycle (event type, matches
  emitted). All addresses pass through the redactor (`<email-N>`),
  subjects are NOT logged, and message IDs are logged verbatim
  (Graph IDs are random tokens, not PII).
- **Status line on stderr** never includes addresses or subjects
  — only counters and durations.
- **`--rule <name>`**: the saved-search name surfaces in the
  status line at most as `rule=<name>` for debugging. Saved-search
  names are user-typed, not derived from message content; they are
  treated as non-sensitive (matching spec 11's existing logging
  posture).
- **Stdout redirected to disk.** `inkwell … --watch >> watch.log`
  persists subjects (and `bodyPreview` in JSONL mode) in plaintext
  on the filesystem. Inkwell's own log file at
  `~/Library/Logs/inkwell/` is created with mode `0600` per
  CLAUDE.md §7 rule 1, but a user-supplied redirection is governed
  by the user's umask (typically `022` → `0644`). The
  `docs/user/how-to.md` recipe added by §9 calls this out and
  recommends `umask 077` before launching long-running redirected
  watches. This is documentation-only; the spec does not introduce
  permission policing on user-controlled stdout destinations.

### 5.10 Worked examples

```bash
# Tail VIP unread to the terminal:
inkwell messages --filter '~U & ~f vip@example.com' --watch

# Same, but pipe IDs to a downstream consumer:
inkwell messages --filter '~U & ~f vip@*' --watch --output json \
  | jq -r '.ID' | while read id; do echo "ping for $id"; done

# Use a saved search:
inkwell messages --rule VIPs --watch

# Auto-archive newsletter mail as it arrives (composes with
# `inkwell message move`):
inkwell messages --filter '~G Newsletters' --watch --output json \
  | jq -r '.ID' \
  | xargs -I {} inkwell message move {} --to "Archive/Newsletters" --yes

# Print the last 10 matches then keep watching:
inkwell messages --filter '~U' --watch --initial=10

# Wait for the next 3 messages from Bob, then exit:
inkwell messages --filter '~f bob@*' --watch --count 3

# Watch for 8 hours during a deployment window:
inkwell messages --filter '~f alerts@*' --watch --for 8h
# Caveat: long --for durations require an unlocked keychain
# throughout. On macOS, screen-lock locks the login keychain by
# default; watch will see AuthRequired events when it tries to
# refresh the token. Either keep the session unlocked, or run inside
# a session that doesn't lock (e.g. a tmux on a CI host).

# Cron-friendly: every cron run, watch for 60s, then exit (the
# engine inside watch runs sync, picking up missed mail too):
* * * * * /usr/local/bin/inkwell messages --filter '~U' --watch --for 55s --quiet >> ~/inkwell-vip.log 2>&1

# Read-only watcher when a daemon is running:
inkwell daemon &
inkwell messages --filter '~U' --watch                # auto-detects daemon, read-only
inkwell messages --filter '~U' --watch --no-sync      # explicit read-only
```

## 6. Performance budgets

| Surface | Budget | How measured |
| ------- | ------ | ------------ |
| Per-cycle re-evaluation (`SearchByPredicate` over 100 k messages, simple filter `~U & ~f addr`) | ≤10 ms p95 | `BenchmarkWatchEvaluate` in `cmd/inkwell/cmd_watch_test.go`. Aligned with PRD §7's `Search(q, limit=50) over 100k msgs <100ms p95` envelope; watch uses the existing `SearchByPredicate` index path so the budget is well within. |
| `emitNew` diff cost over 1000 candidate rows against a 5000-entry seen-set | ≤2 ms p95 | `BenchmarkWatchEmitNew`. |
| Steady-state RSS above the headless-app baseline | ≤50 MB | Manual `ps -o rss=` smoke; the seen-set at default 5000 IDs is ≈1 MB; the rest is the engine and store. |
| **Dispatch latency** (sync event handler invoked → first JSONL byte hits stdout) — in-process measurement only | ≤50 ms p95 | `BenchmarkWatchDispatchLatency` driven by a fake engine that emits `SyncCompletedEvent` and a synthetic store with one matching row. **End-to-end "new mail to stdout" latency is dominated by the engine's foreground interval (default 30 s); this budget measures only the loop overhead from event-arrived to byte-on-stdout.** |
| Worst-case end-to-end lag from message arriving on Graph → emitted on stdout | foreground sync interval (default 30 s) + delta fetch (≈50–200 ms) | Not benched (depends on Graph latency). Documented in §1 / §2.4 so users understand the delay envelope. |

A regression of more than 50 % on any benched budget blocks merge,
per CLAUDE.md §6.

## 7. CLI surface — full grammar

Modified subcommand:

```
inkwell messages [flags]

  --folder <name>           folder display-name; default "Inbox"
  --all                     ignore --folder, evaluate cross-folder
  --filter <pattern>        spec 08 pattern (mutually exclusive with --rule)
  --rule <name>             saved search to use (mutually exclusive with --filter)
  --limit <N>               cap one-shot output (mutually exclusive with --watch)
  --unread                  one-shot: only unread (mutually exclusive with --watch)
  --output <text|json>      output format

  --watch                   enter watch mode (requires --filter or --rule)
  --interval <duration>     re-eval cadence (default = engine foreground interval; min 5s)
  --initial <N>             startup backlog rows (default 0)
  --include-updated         re-emit on last_modified_at advance
  --count <N>               exit 0 after N matches
  --for <duration>          exit 0 after this wall-clock duration
```

`--watch` flags are silently ignored if `--watch` itself is not set
(matches cobra's standard behaviour for orthogonal flags). The help
text groups the watch flags under a `Watch mode (--watch)` header
so `inkwell messages --help` does not inflate to a wall of text in
the default view.

`inkwell messages --help` gains a `WATCH MODE` section. Example:

```
WATCH MODE
  Use --watch to continuously stream new matches to stdout, like `tail -f`.
  Watch mode requires --filter or --rule.

  Examples:
    inkwell messages --filter '~U & ~f vip@*' --watch
    inkwell messages --rule VIPs --watch --output json | jq '.Subject'

  Stop with Ctrl-C; watch exits 0. Use --count or --for to bound the watch.
```

## 8. Test plan

Tests live in `cmd/inkwell/cmd_watch_test.go` unless noted.
`internal/savedsearch/` and `internal/sync/` already provide the
fakes/seams watch tests need; we do not invent new test packages.

### 8.1 Unit tests (no Graph, fake engine)

- `TestWatchRequiresFilterOrRule` — `inkwell messages --watch` with
  no filter exits 2 with the documented message.
- `TestWatchFilterAndRuleExclusive` — both supplied → exit 2.
- `TestWatchLimitAndWatchExclusive` — `--watch --limit 50` → exit 2.
- `TestWatchUnreadFlagExits2` — `--watch --unread` → exit 2 with
  message `--unread is not supported with --watch — use --filter
  '~U & …' instead`.
- `TestWatchBadPatternExits2` — `--filter '~~~'` exits 2 with the
  parser's error string (no panic).
- `TestWatchIntervalClampedToMinimum` — `--interval 100ms` is
  clamped to `minSyncInterval` (5s) and a one-line stderr warning
  is printed (matches `internal/sync/engine.go:484` policy).
- `TestWatchInitialZeroEmitsNoBacklog` — startup with 50 cached
  matches and `--initial=0` emits zero stdout lines until the first
  event arrives.
- `TestWatchInitialNPrintsLastNInOrder` — `--initial=3` emits the
  3 most-recent matches, in `received_at DESC` order, BEFORE the
  loop arms.
- `TestWatchEmitsOnlyNewMessages` — fake engine emits two
  `SyncCompletedEvent`s; the store grows by 2 rows between them;
  watch emits exactly the 2 new rows.
- `TestWatchDoesNotReEmitOnReadFlagFlip` — a previously emitted
  message has its `is_read` flipped between cycles; without
  `--include-updated`, watch does NOT re-emit.
- `TestWatchIncludeUpdatedReEmitsOnLastModifiedAdvance` — same
  fixture, with `--include-updated` set, watch DOES re-emit.
- `TestWatchSeenSetEvictsOldestAtCapacity` — set
  `[cli].watch_max_seen=4`, push 5 distinct matches across 5
  cycles; the oldest entry is evicted; if the same ID appears
  again the row re-emits (regression: documents the eviction
  trade-off).
- `TestWatchCountTerminates` — `--count 2` exits 0 exactly after
  the 2nd emission and prints the summary line on stderr.
- `TestWatchForTerminates` — `--for 100ms` exits 0 within the
  duration ±50 ms, regardless of how many matches arrived.
- `TestWatchSafetyNetTimerEvaluatesWithoutEvent` — engine emits
  zero events; new rows still surface within `--interval`.
- `TestWatchSyncFailedKeepsRunning` — `SyncFailedEvent` arrives;
  watch prints to stderr and continues; the next
  `SyncCompletedEvent` causes a normal emission.
- `TestWatchAuthRequiredOnceWarnsAndKeepsRunning` — single
  `AuthRequiredEvent`, no exit, one stderr line.
- `TestWatchAuthRateLimitAcrossSixtySeconds` — drive ten
  `AuthRequiredEvent`s within a fake-clock 30 s window; assert
  exactly one stderr line (rate-limit window pinned).
- `TestWatchAuthTenMinuteWindowExits3` — drive consecutive
  `AuthRequiredEvent`s spanning ≥10 minutes of fake-clock with
  zero `SyncCompletedEvent` in between → exit 3.
- `TestWatchSyncCompletedResetsAuthWindow` — after 9 minutes of
  consecutive auth failures, a single `SyncCompletedEvent` resets
  the window; subsequent auth failures must accumulate another
  10 minutes to exit 3.
- `TestWatchThrottledEventWarnsContinues` — single
  `ThrottledEvent`, no exit, one stderr line.
- `TestWatchSIGINTExitsZero` — send SIGINT to the watch loop;
  exits 0; final summary is printed.
- `TestWatchSIGINTTwiceExitsImmediately` — second SIGINT within
  2 s exits 130 without flushing the summary.
- `TestWatchSIGPIPECloseExitsZero` — pipe stdout to a reader that
  closes immediately; watch exits 0 (NO stack trace, NO `broken
  pipe` on stderr).
- `TestWatchJSONLOneObjectPerLineNoArray` — JSON mode emits raw
  objects separated by newlines; the bytes do NOT begin with `[`
  and do NOT end with `]`; each line round-trips through
  `json.Unmarshal` independently.
- `TestOneShotMessagesJSONStillArrayShape` — guard test in
  `cmd_messages_test.go` (NOT cmd_watch_test.go) that the existing
  `inkwell messages --output json` continues to emit a single JSON
  array. Pins the shape divergence documented in §5.2 so a future
  migration trips an explicit failure rather than silently
  breaking pipelines.
- `TestWatchEmitsLineByLineUnderPipe` — connect `os.Stdout` to a
  `os.Pipe()`; emit two matches across two cycles; the reader sees
  the first line bytes BEFORE the second match is emitted (no
  buffering held the first row hostage).
- `TestWatchTextHeaderPrintedOnceAcrossCycles` — header emitted
  once at first match, never repeated even after multiple
  re-evaluations.
- `TestWatchAllNoFolderColumn` — `--all` watch mode uses the flat
  `printMessageList` printer; the rendered text does NOT contain a
  FOLDER column header (matches one-shot `messages --all`
  behaviour today; pinned to surface a regression if a follow-up
  spec adds the FOLDER column).
- `TestWatchAllFolderMoveReEmitsRow` — under `--all`, a message
  moved between folders mid-watch (its Graph ID changes) re-emits
  as a new row. Documents the §5.5 "folder moves mint a new ID"
  contract.
- `TestWatchRuleResolvesPatternFromManager` — `--rule VIPs`
  resolves through a fake `savedsearch.Manager` to its pattern;
  watch then behaves identically to `--filter` with that pattern.
- `TestWatchUnknownRuleExits5` — `--rule DoesNotExist` exits 5
  with `not found: rule "DoesNotExist"`.
- `TestWatchUnknownFolderExits5` — `--folder DoesNotExist` exits
  5 with the existing one-shot `messages` error.
- `TestWatchNoSyncFlagSkipsEngineStart` — `--no-sync` set; watch
  never invokes `eng.Start`; the safety-net timer is the only
  evaluation trigger; an injected fake row in the store surfaces
  on the next timer tick. Verifies the §5.6 cohabitation hatch.
- `TestWatchStatusLineSuppressedWhenStderrNotTTY` — fake
  `term.IsTerminal(stderr) == false`; assert no `\r` writes hit
  stderr.
- `TestWatchQuietSuppressesStatusAndSummary` — `--quiet`; no
  status line, no exit summary; warnings (auth/throttle/fail) are
  still printed.

### 8.2 Logging / redaction

- `TestWatchLogsRedactAddresses` — match a row from
  `bob@example.com`; capture `slog` output via the existing test
  helper; assert log lines contain `<email-` and NOT
  `bob@example.com`.
- `TestWatchLogsDoNotIncludeSubjects` — same fixture; assert no
  log line at any level contains the subject string.
- `TestWatchStatusLineNeverIncludesAddressOrSubject` — capture
  stderr; assert `\r`-prefixed lines never contain `@` or
  subject substrings.

### 8.3 Benchmarks (`cmd/inkwell/cmd_watch_test.go`)

- `BenchmarkWatchEvaluate` — seeded fixture of 100 k messages,
  filter `~U & ~f vip@*`, ≤10 ms p95.
- `BenchmarkWatchEmitNew` — 1000 candidate rows × 5000-entry
  seen-set, ≤2 ms p95.
- `BenchmarkWatchDispatchLatency` — fake engine fires
  `SyncCompletedEvent`; measure wall-clock from event emit → first
  byte on the captured stdout. ≤50 ms p95. Naming aligned with
  §6's "Dispatch latency" budget row.
- `b.ReportAllocs()` on every benchmark per CLAUDE.md §5.2.

### 8.4 Integration (build-tag `integration`)

- `TestWatchNoSyncAgainstRealStore` — open a real SQLite tmpdir
  store, seed 50 messages, invoke watch with `--no-sync` (no
  engine started; no Graph dial-out), `--for 1s`, `--initial=0`,
  and a goroutine that inserts 3 new matching rows directly via
  the store API mid-watch; assert exactly 3 stdout lines after
  the deadline. Uses no Graph fixtures — the integration boundary
  is store ↔ watch loop, not Graph.
- `TestWatchEngineStartedAgainstRecordedGraph` — same shape, but
  with the engine started and the graph client wired to a
  `httptest.Server` replaying the existing
  `internal/graph/testdata/` fixtures (consistent with CLAUDE.md
  §5.3). Asserts at least one `SyncCompletedEvent` arrives and
  emits the expected matches.
- `TestWatchSurvivesStoreReadFailureMidLoop` — close the store
  mid-watch; assert watch logs the error, the safety-net timer
  re-tries on the next tick, and a SIGINT still produces a clean
  exit 0.

### 8.5 What we do NOT test

- TUI e2e: watch is CLI-only; no `*_e2e_test.go`.
- Live tenant: per CLAUDE.md §5.5, manual smoke only. Add a row
  to `docs/qa-checklist.md` under "Release smoke" — see §9.

## 9. Definition of done

- [ ] `cmd/inkwell/cmd_messages.go` declares the new flags
      (`--watch`, `--interval`, `--initial`, `--include-updated`,
      `--count`, `--for`) on the `messages` cobra command and the
      three `MarkFlagsMutuallyExclusive` pairs from §5.1's matrix
      (`--filter`/`--rule`, `--watch`/`--limit`, `--watch`/`--unread`).
- [ ] `cmd/inkwell/cmd_watch.go` exists and contains:
  - [ ] `runWatch(ctx, app, opts)` per §5.3 pseudocode.
  - [ ] `seenSet` LRU bounded by `[cli].watch_max_seen`.
  - [ ] `emitNew(rows)` with the `--include-updated` semantic.
  - [ ] Engine cohabitation per §5.6: watch starts its own engine
        unless `--no-sync` is set; no daemon-PID-file probe (the
        daemon does not write one today).
  - [ ] Signal handlers per §5.7 (SIGINT, SIGTERM, SIGHUP, EPIPE-
        on-write helper for SIGPIPE; no `signal.Ignore` call).
  - [ ] AuthRequired wall-clock window per §5.4 (10-minute
        threshold; `SyncCompletedEvent` resets the window).
  - [ ] Single emit-helper that handles `errors.Is(err,
        syscall.EPIPE)` → exit 0.
- [ ] `messages --watch --filter X` emits one line per new match
      indefinitely; `Ctrl-C` exits 0 with summary.
- [ ] `--output json` emits JSONL (one object per line, no array
      wrapper); each line round-trips through `json.Unmarshal`.
- [ ] `--initial=N` prints exactly N most-recent matches then
      enters the loop; `--initial=0` (default) starts silent.
- [ ] `--rule <name>` resolves a saved search via spec 11
      `Manager.Get`; unknown rule exits 5.
- [ ] `--no-sync` (the existing global flag) skips engine startup
      in watch mode; safety-net timer is the only evaluation
      trigger; documented as a watch-specific extension of the
      flag's semantics in §10 spec-14 consistency.
- [ ] `--count N` and `--for D` exit 0 at their boundary.
- [ ] Status line on stderr matches §5.2 (TTY-only, suppressed
      under `--quiet` and on non-TTY).
- [ ] Pipe-friendly: `... | head -3` exits 0, no broken-pipe
      stack trace; tested via SIGPIPE unit test.
- [ ] Reuses `internal/cli/exitcodes.go` constants; no new code
      added.
- [ ] All §8.1 unit tests pass with `go test -race ./cmd/inkwell/`.
- [ ] All §8.2 redaction tests pass.
- [ ] All §8.3 benchmarks pass within budget on the dev machine.
- [ ] §8.4 integration tests pass with
      `go test -tags=integration ./cmd/inkwell/`.
- [ ] `gofmt -s`, `go vet`, `go test -race`, `go test -tags=e2e`
      (no new e2e but the existing TUI e2e suite must remain
      green), `go test -tags=integration`,
      `go test -bench=. -benchmem -run=^$` (CLAUDE.md §5.6) — all
      green.
- [ ] **Doc sweep (CLAUDE.md §12.6)**:
  - [ ] `docs/specs/29-watch-mode.md` carries the
        `**Shipped:** vX.Y.Z` line at the top of the metadata
        block once shipped.
  - [ ] `docs/plans/spec-29.md` exists and is updated each
        iteration; final entry is `Status: done` with measured
        perf numbers.
  - [ ] `docs/PRD.md` §10 inventory has a row for spec 29.
  - [ ] `docs/ROADMAP.md` Bucket 3 row 3 ("Watch mode (1.19)")
        cell updated to `Spec 29 — ready` (and `Shipped vX.Y.Z`
        once shipped); §1.19 narrative gains a "Owner: spec 29"
        line.
  - [ ] `docs/user/reference.md` adds a `messages --watch` row
        to the CLI subcommands table, plus rows for each new
        flag (`--interval`, `--initial`, `--include-updated`,
        `--count`, `--for`). Footer `_Last reviewed against
        vX.Y.Z._` bumped.
  - [ ] `docs/user/how-to.md` gains a "Tail your inbox like
        `tail -f`" recipe based on §5.10.
  - [ ] `docs/user/tutorial.md`: no change (watch is not in the
        first-30-minutes path).
  - [ ] `docs/user/explanation.md`: no change (no design
        invariant moved).
  - [ ] `docs/CONFIG.md` adds the `[cli].watch_max_seen` row
        (type: int, default 5000, used by §5.5 dedup-set
        capacity bound). Existing `--no-sync` row gains a
        sentence: "in watch mode (spec 29) this flag also skips
        starting the embedded sync engine, so the watch process
        polls only the cache".
  - [ ] `docs/qa-checklist.md` adds one row under "Release
        smoke": run `inkwell messages --filter '~U' --watch
        --for 60s` against the live tenant and confirm at least
        one expected match streams through.
  - [ ] `README.md` Status table gains a `Watch mode (CLI tail)`
        row marked `✅ vX.Y.Z` once shipped.
- [ ] Cross-cutting checklist (§10) ticked.

## 10. Cross-cutting checklist

- [ ] **Scopes:** none new. Watch reads the local cache and
      consumes engine events; both are inside the existing
      `Mail.Read` envelope.
- [ ] **Store reads/writes:** read-only on `messages` and
      `folders`; the engine started by watch DOES write
      `delta_tokens` and `messages` (same writes as one-shot
      `inkwell sync`). Watch itself never touches the action
      queue.
- [ ] **Graph endpoints:** none new. Watch's engine uses the same
      delta endpoints spec 03 already calls.
- [ ] **Offline:** with `--no-sync`, watch is fully offline (cache
      reads only); without it, watch behaves like every other
      `inkwell` subcommand — degraded gracefully when Graph is
      unreachable (engine emits `SyncFailedEvent`, watch prints a
      stderr line, continues).
- [ ] **Undo:** N/A. Watch is read-only and emits no actions.
- [ ] **Error states:** §5.4 / §5.8 cover sync failures, auth-
      required cycles, throttling, broken pipes, missing folders,
      missing rules, bad patterns, and clamped intervals.
- [ ] **Latency budget:** §6 table; `BenchmarkWatchEvaluate`,
      `BenchmarkWatchEmitNew`, `BenchmarkWatchDispatchLatency`.
- [ ] **Logs:** start/stop at INFO; per-cycle at DEBUG; addresses
      redacted via existing `internal/log/redact.go`; subjects
      never logged. `TestWatchLogsRedactAddresses` /
      `TestWatchLogsDoNotIncludeSubjects` enforce this.
- [ ] **CLI mode:** this spec IS the CLI. The TUI (`internal/ui/`)
      sees no change.
- [ ] **Tests:** §8.1–8.4 list.
- [ ] **Spec 17 review** (security testing + CASA evidence): watch
      introduces a new long-running CLI subcommand and a new
      signal handler, both of which fall under "process-level
      surfaces" rather than the categories spec 17 §4 enumerates
      (token handling, file I/O paths, subprocess invocation,
      external HTTP, third-party data flow, cryptographic
      primitives, SQL composition, persisted local state). Watch
      adds NONE of those: tokens are handled by the existing
      auth+graph layers; file I/O is limited to existing SQLite
      cache reads through `internal/store/` (no new path opened,
      no path-traversal surface added — watch reads no PID file
      and writes no per-process state); no subprocess; no new
      external HTTP (the embedded sync engine reuses the
      existing `internal/graph/` client); no new data flow; no
      crypto; no new SQL composition (`pattern.CompileLocal` →
      `SearchByPredicate` is the existing parameterised path); no
      persisted local state. Therefore: NO update to
      `docs/specs/17-security-testing-and-casa-evidence.md` §4,
      NO update to `docs/THREAT_MODEL.md`, NO update to
      `docs/PRIVACY.md` (watch's stdout is user-controlled like
      every other CLI command). NO new `// #nosec` annotations
      expected. The PR description carries `spec 17 impact: none
      (no new sensitive surface)` per CLAUDE.md §11.
- [ ] **Spec 17 CI gates green:** gosec, Semgrep, govulncheck —
      no anticipated findings; the JSON marshaller and the cobra
      flag plumbing are existing patterns.
- [ ] **Spec 03 consistency:** watch consumes `Notifications()`
      with the exact `select` pattern documented in spec 03 §3
      (`Notifications` + `Done()` to avoid the goroutine-leak
      bug fixed in iter 10 of `docs/plans/spec-03.md`). Watch
      calls `eng.SetActive(true)` so the engine ticks at the
      foreground (30 s) cadence regardless of TUI state.
- [ ] **Spec 08 consistency:** watch's filter compiler call is
      `pattern.Parse` then `pattern.CompileLocal`, identical to
      `runFilterListing` (`cmd/inkwell/cmd_filter.go:156`). No
      new operators, no parser fork.
- [ ] **Spec 11 consistency:** `--rule <name>` resolves through
      `Manager.Get(ctx, name)` per
      `internal/savedsearch/manager.go:54` (signature
      `Get(ctx, name) (*store.SavedSearch, error)`; returns
      `(nil, nil)` for not-found, which watch maps to
      `ExitNotFound` (5)). The pattern source is `ss.Pattern`,
      compiled by the same `pattern.Parse` →
      `pattern.CompileLocal` pair `runFilterListing` already uses.
      Manager construction follows `cmd_rule.go`'s wiring
      (manager needs `cfg.SavedSearch`; watch invokes
      `rc.loadConfig()` to populate it). The rule's pattern is
      read once at watch startup; if the rule is renamed or
      deleted mid-watch, the watch continues with the original
      pattern — restart to pick up changes.
- [ ] **Spec 14 consistency:** `--output`, `--quiet`, `--config`
      global flags work; exit codes per `internal/cli/exitcodes.go`;
      `--help` groups watch flags under a `WATCH MODE` section.
      Two **explicit deltas** to spec 14 declared by this spec:
      (a) **`--no-sync` semantics extended for watch only.** Today
      the global `--no-sync` flag is declared in `cmd_root.go:46`
      with help text "use cached data only, skip sync" and is not
      consulted by any subcommand. Spec 29 reads `rc.noSync` in
      the watch loop and skips `eng.Start` when set. The flag's
      meaning for non-watch paths is unchanged. The CONFIG.md /
      reference.md doc sweep notes the watch-specific behaviour
      under the existing `--no-sync` row.
      (b) **JSONL on `messages --watch --output json`** fulfils
      spec 14 §5.2's "line-delimited JSON for long lists; one
      object per line, no enclosing array" contract for the watch
      path only. The one-shot `messages --output json` continues
      to emit a JSON array; migrating it is out of scope here and
      pinned by `TestOneShotMessagesJSONStillArrayShape`.
- [ ] **Spec 19 consistency** (mute thread): `SearchByPredicate`
      is the raw-WHERE entry point; it does NOT consult
      `muted_conversations` (the `ExcludeMuted` knob lives on
      `MessageQuery`/`ListMessages`, not on `SearchByPredicate`).
      Muted-conversation rows therefore appear in watch output if
      they match the filter, exactly as they do for one-shot
      `inkwell messages --filter X` and `inkwell filter X` today.
      This is the desired behaviour for a "tail my filter" UX
      (the user explicitly asked for this filter; surfacing
      muted matches lets them act). Watch does NOT add a new
      `--exclude-muted` flag.
- [ ] **Spec 21 consistency** (cross-folder filter): `--all`
      flag works in watch mode and uses the flat
      `printMessageList` (no FOLDER column), matching the
      one-shot `inkwell messages --all --filter X` behaviour
      today. JSONL output is unchanged (folder ID is already in
      the envelope; map to display name client-side via `jq`).
      Adding a FOLDER column to `messages --all` text output is
      a separate UX change that would update both the one-shot
      and watch paths in the same commit and is out of scope
      here. `TestWatchAllNoFolderColumn` pins the current
      behaviour so a follow-up that adds the column trips an
      explicit failure rather than silently changing the shape.
- [ ] **Spec 23 consistency** (routing destinations): routing
      and watch are orthogonal. A routed sender's mail still
      lands in whatever folder the routing rule designated; if
      that folder matches the watch's `--folder` (or `--all` is
      set), the row surfaces as expected. No interaction
      contract beyond "watch reads from the cache the routing
      writes to".
- [ ] **Spec 24 consistency** (split inbox tabs): tabs are a
      TUI-only surface (synthetic-folder IDs in the UI model);
      watch consumes real folders only via `--folder` / `--all`.
      A future spec could add `--tab <name>` resolution; not in
      this scope.
- [ ] **Spec 25 consistency** (reply-later / set-aside): the
      Reply Later / Set Aside categories are messages with
      well-known category names. A watch with
      `--filter '~y "Reply Later"'` (or whichever spec 25
      operator landed) works unchanged; watch makes no special
      case for category-keyed filters.
- [ ] **Spec 26 consistency** (bundle senders): bundling is a
      TUI render pass (spec 26 §1.1). Watch emits flat rows —
      one line per message — independent of any user's bundle
      designation. Two watch processes running against the same
      mailbox with the same filter emit identical streams.
- [ ] **Docs consistency sweep:** `docs/CONFIG.md` row for
      `[cli].watch_max_seen`; `docs/user/reference.md` rows for
      every new flag; `docs/user/how-to.md` recipe; `docs/PRD.md`
      §10 spec inventory; `docs/ROADMAP.md` Bucket 3 row 3 +
      §1.19 owner line; `docs/qa-checklist.md` smoke row;
      `README.md` Status table row.

## 11. Out of scope

- Microsoft Graph push subscriptions (§2.4 — no public HTTPS
  endpoint; deferred indefinitely).
- IMAP IDLE — inkwell does not speak IMAP (stack invariant
  CLAUDE.md §1).
- Acting on watched matches in-process (e.g. `--auto-archive`).
  Compose with `xargs inkwell message ...` instead — example in
  §5.10.
- Desktop / system notification (`notify-send`, `terminal-notifier`).
  Compose externally if desired; example:
  `inkwell messages --filter X --watch --output json |
   jq -r '.Subject' | xargs -I {} terminal-notifier -message {}`.
- Bell / audible alert. Same compose-externally rule.
- Persisted seen-set (re-emit suppression across watch restarts).
  §5.5 explains the trade-off.
- `--rule` autocomplete on the command line. Cobra completion is a
  separate ROADMAP item (§1.20).
- `inkwell watch <…>` as a top-level subcommand. The roadmap
  example uses `messages --watch`; we honour it.
- Streaming bodies, attachments, or rendered text. Watch emits
  envelopes only — same shape `messages` already emits.
- `tail`-style line-numbering / context (`-n`, `--follow=name`,
  etc.). The unix toolkit composes for those needs.

---

_End of spec 29._
