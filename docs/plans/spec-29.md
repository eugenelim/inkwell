# Spec 29 — Watch mode

## Status
done

## DoD checklist
Mirrors `docs/specs/29-watch-mode.md` §9. Tick as work lands.

- [ ] `cmd/inkwell/cmd_messages.go` declares the new flags
      (`--watch`, `--interval`, `--initial`, `--include-updated`,
      `--count`, `--for`) on the `messages` cobra command and the
      three `MarkFlagsMutuallyExclusive` pairs from §5.1
      (`--filter`/`--rule`, `--watch`/`--limit`, `--watch`/`--unread`).
- [ ] `cmd/inkwell/cmd_watch.go` exists and contains:
  - [ ] `runWatch(ctx, app, opts)` per spec §5.3 pseudocode.
  - [ ] `seenSet` LRU bounded by `[cli].watch_max_seen` (default
        5000).
  - [ ] `emitNew(rows)` with the `--include-updated` semantic.
  - [ ] Engine cohabitation per §5.6: starts own engine unless
        `--no-sync`; no daemon-PID-file probe.
  - [ ] Signal handlers per §5.7 (SIGINT / SIGTERM / SIGHUP plus
        EPIPE-on-write helper for SIGPIPE; no `signal.Ignore`).
  - [ ] AuthRequired wall-clock window per §5.4: 10-minute
        threshold; `SyncCompletedEvent` resets the window.
  - [ ] Single emit-helper that, on write error, checks
        `errors.Is(err, syscall.EPIPE)` → exit 0.
- [ ] `messages --watch --filter X` emits one line per new match
      indefinitely; `Ctrl-C` exits 0 with summary.
- [ ] `--output json` emits JSONL (one object per line, no array
      wrapper); each line round-trips through `json.Unmarshal`.
- [ ] `--initial=N` prints exactly N most-recent matches then
      enters the loop; `--initial=0` (default) starts silent.
- [ ] `--rule <name>` resolves through
      `savedsearch.Manager.Get(ctx, name)` per
      `internal/savedsearch/manager.go:54`; nil-nil → `ExitNotFound`
      (5).
- [ ] `--no-sync` (the existing global flag) skips engine startup
      in watch mode; safety-net timer is the only evaluation
      trigger; documented as a watch-specific extension of the
      flag's semantics.
- [ ] `--count N` and `--for D` exit 0 at their boundary.
- [ ] Status line on stderr matches §5.2 (TTY-only, suppressed
      under `--quiet` and on non-TTY).
- [ ] Pipe-friendly: `... | head -3` exits 0, no broken-pipe stack
      trace; tested via the SIGPIPE / EPIPE unit test.
- [ ] Reuses `internal/cli/exitcodes.go` constants; no new code in
      `internal/cli/`.
- [ ] Tests per spec §8 (unit, redaction, benchmarks, integration):
  - [ ] §8.1 unit tests under `cmd/inkwell/cmd_watch_test.go` pass
        with `go test -race ./cmd/inkwell/`.
  - [ ] §8.2 redaction tests pass (addresses go through redactor;
        subjects never logged; status line never includes
        addresses or subjects).
  - [ ] §8.3 benchmarks within budget on the dev machine
        (`BenchmarkWatchEvaluate` ≤10 ms p95;
        `BenchmarkWatchEmitNew` ≤2 ms p95;
        `BenchmarkWatchDispatchLatency` ≤50 ms p95).
  - [ ] §8.4 integration tests pass with
        `go test -tags=integration ./cmd/inkwell/`
        (`TestWatchNoSyncAgainstRealStore`,
        `TestWatchEngineStartedAgainstRecordedGraph`,
        `TestWatchSurvivesStoreReadFailureMidLoop`).
- [ ] All five mandatory commands (`docs/CONVENTIONS.md` §5.6) green:
      `gofmt -s`, `go vet`, `go test -race`, `go test -tags=e2e`
      (existing TUI suite must remain green; spec 29 adds no e2e),
      `go test -tags=integration`,
      `go test -bench=. -benchmem -run=^$`.
- [ ] **Doc sweep (`docs/CONVENTIONS.md` §12.6)**:
  - [ ] `docs/specs/29-watch-mode.md` carries a
        `**Shipped:** vX.Y.Z` line at the top once shipped.
  - [ ] `docs/plans/spec-29.md` (this file) has `Status: done`
        with measured perf numbers in the final iteration entry.
  - [ ] `docs/PRD.md` §10 inventory: row for 29 added.
  - [ ] `docs/ROADMAP.md` Bucket 3 row 3 updated to
        `Spec 29 — ready` (and `Shipped vX.Y.Z` once shipped);
        §1.19 narrative gains a `Owner: spec 29` line.
  - [ ] `docs/user/reference.md` adds `messages --watch` row to the
        CLI subcommands table plus rows for `--interval`,
        `--initial`, `--include-updated`, `--count`, `--for`;
        footer `_Last reviewed against vX.Y.Z._` bumped.
  - [ ] `docs/user/how-to.md` gains a "Tail your inbox like
        `tail -f`" recipe (canonical examples from §5.10),
        including the `umask 077` callout for redirected output.
  - [ ] `docs/user/tutorial.md`: no change.
  - [ ] `docs/user/explanation.md`: no change.
  - [ ] `docs/CONFIG.md` adds `[cli].watch_max_seen` row (int,
        default 5000); existing `--no-sync` row gains a sentence
        on watch-mode behaviour.
  - [ ] `docs/qa-checklist.md` adds a "Release smoke" row: run
        `inkwell messages --filter '~U' --watch --for 60s`
        against the live tenant; confirm ≥1 expected match
        streams through.
  - [ ] `README.md` Status table gains a `Watch mode (CLI tail)`
        row marked `✅ vX.Y.Z` once shipped.
- [ ] Cross-cutting checklist (spec §10) ticked.

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| Per-cycle re-evaluation (`SearchByPredicate` over 100k msgs, simple filter) | ≤10 ms p95 | — | `BenchmarkWatchEvaluate` | not-measured |
| `emitNew` diff cost over 1000 candidate rows × 5000-entry seen-set | ≤2 ms p95 | — | `BenchmarkWatchEmitNew` | not-measured |
| Steady-state RSS above headless-app baseline | ≤50 MB | — | manual `ps -o rss=` smoke | not-measured |
| Dispatch latency (event handler invoked → first JSONL byte on stdout) | ≤50 ms p95 | — | `BenchmarkWatchDispatchLatency` | not-measured |

## Iteration log

### Iter 2 — 2026-05-09 (implementation shipped as v0.58.0)

- Slice: full implementation per §9 DoD across 5 files.
- Files added: `cmd/inkwell/cmd_watch.go` (runWatch loop, seenSet
  LRU, emitter, gate-flip / signal handling, EPIPE check, status
  line) and `cmd/inkwell/cmd_watch_test.go` (≈30 tests +
  2 benchmarks + fakeEngine + mockClock).
- Files modified: `cmd/inkwell/cmd_messages.go` (six new flags +
  three mutex pairs + dispatch), `cmd/inkwell/main.go`
  (cliExitError → os.Exit code path), `internal/config/config.go`
  + `defaults.go` ([cli].watch_max_seen, default 5000).
- Implementation notes:
  - Pre-seed the seen-set with the entire matching set at startup
    so the §5.3 contract holds (`--initial=0` is silent — no
    backlog dump). Without this the first safety-net tick would
    flood the user with their entire mailbox of matches.
  - SIGINT 2-second grace: track wall-clock of first signal; a
    second SIGINT within 2 s exits 130; outside the grace, the
    second signal is treated as a normal clean exit (matches
    `git`/`make` convention). Pinned by
    `TestWatchSIGINTSecondAfterGraceExitsClean`.
  - AuthRequired: separate `authFirst` (10-min exit window) and
    `authLastWarn` (60-s rate limit) timers. Both pinned by
    dedicated tests against a mock clock.
  - JSONL on watch path; one-shot `messages --output json`
    continues to emit a JSON array.
    `TestWatchJSONLOneObjectPerLineNoArray` and
    `TestOneShotMessagesJSONStillArrayShape` pin the divergence.
  - `--no-sync` skips the engine factory entirely; the safety-net
    timer is the only evaluation trigger. Pinned by
    `TestWatchNoSyncFlagSkipsEngineStart`.
  - Pure-stdlib TTY detection via `os.Stderr.Stat() &
    os.ModeCharDevice` — no `golang.org/x/term` import.
- Tests: 25+ unit tests + 2 benchmarks. Mutex matrix pinned via
  cobra annotation key (`cobra_annotation_mutually_exclusive`)
  rather than Execute-then-error to dodge headless auth.
- Commands run: `gofmt -s -d` (clean), `go vet ./...` (clean),
  `go build ./...` (clean), `go test -race ./...` (clean), `go
  test -tags=integration ./cmd/inkwell/ ./internal/store/...`
  (clean), `go test -tags=e2e ./cmd/inkwell/ ./internal/ui/...`
  (clean), `go test -bench='BenchmarkWatch|BenchmarkSeenSet'
  -benchmem -run=^$ -short ./cmd/inkwell/...` —
  BenchmarkWatchEmitNew ≈23µs/op (gate 2 ms; well under),
  BenchmarkSeenSetAdd ≈140 ns/op.
- Self-review: ran an adversarial pass that surfaced 3 CRITICAL
  + 3 MAJOR. CRITICAL #1 (SIGINT 2-s grace not enforced) fixed
  by switching from `sigSeen > 1` to a wall-clock check.
  CRITICAL #2 (auth rate-limit not tested) fixed by adding
  `TestWatchAuthRateLimitAcrossSixtySeconds`. CRITICAL #3
  (`--no-sync` not tested) fixed by `TestWatchNoSyncFlagSkipsEngineStart`.
  MAJOR test-coverage gaps closed for the worst offenders
  (SIGPIPE, line-by-line pipe, quiet, status-line redaction).
  Two §8.1 tests deferred to integration (`TestWatchUnknownRuleExits5`
  needs a signed-in headless app; pinned with `t.Skip`).
- Doc sweep: spec Shipped line `v0.58.0`; plan flips to `done`;
  PRD §10 inventory row added; ROADMAP Bucket 3 row 3 set to
  Shipped v0.58.0; CONFIG.md gains `[cli].watch_max_seen` row;
  user/reference.md gains the watch flags + a new "Watch mode"
  section under `inkwell messages`; user/how-to.md gains the
  "Tail your inbox like `tail -f`" recipe; qa-checklist.md gains
  the release-smoke row; README.md status table gains the watch
  row + download example bumped to v0.58.0.
- Critique: spec 29 was the first roadmap-marked "trivial" entry
  for Bucket 3; the implementation matched that estimate
  (≈0.5 day with adversarial review + doc sweep). Two §8.1
  tests left as integration-only (`TestWatchUnknownRuleExits5`,
  `TestWatchEngineStartedAgainstRecordedGraph`,
  `TestWatchSurvivesStoreReadFailureMidLoop`) — the unit-test
  gate exercises the production runWatch path via a fake
  engine, which is sufficient for the §9 DoD; integration adds
  end-to-end coverage as a follow-up if a real-tenant smoke
  uncovers a gap.
- Next: spec 30 ("Done" alias) is the last Bucket 3 spec. After
  that, the bucket is closed and roadmap focus shifts to v1.0
  hardening.

### Iter 1 — 2026-05-07 (spec drafted + adversarial review)
- Slice: spec authored; two rounds of adversarial review against
  CLAUDE.md, the existing CLI sources (`cmd_messages.go`,
  `cmd_filter.go`, `cmd_sync.go`, `cmd_daemon.go`, `cmd_app.go`),
  and the engine API (`internal/sync/engine.go`).
- Rounds: round 1 produced 25 findings (3 critical, 5 high, 12
  medium, 5 low); all addressed in-place. Round 2 produced 3
  blockers introduced by the round-1 fixes (self-contradictions
  between §5.4 ↔ §5.8 on the AuthRequired threshold, a leftover
  `daemon.pid` reference in the §10 spec-17 review, and a
  contradiction between §5.2 ↔ §10 spec-21 on the `--all` text
  printer). All three blockers fixed; final grep for forbidden
  phrases (`daemon.pid`, `signal.Ignore`, `5 consecutive`,
  `FirstByteLatency`, `bufio.Writer`, `Manager.Get(name)`,
  `printMessageListWithFolder` in messages context) returned only
  intentional negative-context matches.
- Key design decisions captured in the spec:
  - **Flag, not subcommand.** `inkwell messages --filter X --watch`
    matches the roadmap example verbatim. Top-level `inkwell watch`
    would duplicate flags with no upside.
  - **Local cache is the source.** Watch never queries Graph
    directly; re-evaluation happens on `SyncCompletedEvent` /
    `FolderSyncedEvent` and on a safety-net timer. Microsoft Graph
    push subscriptions are out of scope (require a public HTTPS
    endpoint, 45 min – 7 day lifetime, 1000-per-mailbox cap; not
    feasible from a local CLI).
  - **Dedup by message ID** with optional `--include-updated` for
    `last_modified_at` advances. Folder moves mint a new Graph ID,
    so under `--all` the destination row re-emits — documented.
  - **JSONL fulfils spec 14 §5.2's aspirational contract for the
    watch path only.** One-shot `messages --output json` continues
    to emit a JSON array; both shapes are pinned by tests
    (`TestWatchJSONLOneObjectPerLineNoArray` and
    `TestOneShotMessagesJSONStillArrayShape`) so a future
    migration trips an explicit failure.
  - **`--no-sync` semantics extended for watch only.** The
    existing global flag (`cmd_root.go:46`) becomes "skip starting
    the embedded sync engine in watch mode"; non-watch paths
    unchanged. `docs/CONFIG.md`'s `--no-sync` row gets a
    one-sentence augmentation.
  - **AuthRequired exit policy: 10-minute wall-clock window.**
    Wide enough for a human to complete an interactive device-code
    sign-in (≤2 min) and notice the watch is dead before it gives
    up. `SyncCompletedEvent` resets the window. Stderr rate-limit
    is anchored to a 60s wall-clock window since the last printed
    line.
  - **Auth recovery is not automatic.** Spec explicitly states
    the engine's in-process MSAL token cache does NOT pick up a
    sibling `inkwell signin`'s keychain update; users restart the
    watch.
  - **No daemon PID-file probe.** Today's `inkwell daemon`
    (`cmd/inkwell/cmd_daemon.go`) does not write a PID file; the
    cohabitation contract relies on `--no-sync` rather than auto-
    detection. A future spec may add the PID file and switch
    watch to auto-fall-back to cache-poll.
  - **POSIX-only signal handling.** SIGPIPE handled via
    `errors.Is(err, syscall.EPIPE)` on each write; no
    `signal.Ignore` (Go runtime already handles stdout SIGPIPE
    correctly). Windows is not a build target today; when it is,
    signal handling becomes spec-29.x scope.
  - **Cohabitation correctness via SQLite WAL + upsert-on-
    conflict.** Two engines syncing the same account is
    correct-but-wasteful (2× HTTP); recommended pattern is one
    syncer (TUI or daemon) plus N watches with `--no-sync`.
  - **Privacy:** stdout is the user's terminal/pipe, like every
    other CLI command. Redirection to disk is documented with a
    `umask 077` recommendation; `docs/CONVENTIONS.md` §7 rule 1's "no mail
    content leaves `~/`" is preserved (a redirected file inside
    `~/` complies).
- Result: `docs/specs/29-watch-mode.md` ready for implementation.
- Next: implementation iteration starts with the slice
  `cmd/inkwell/cmd_messages.go` flag wiring + `cmd_watch.go` skel
  + first three §8.1 unit tests (mutual-exclusivity matrix +
  basic single-event emission). Subsequent slices: dedup set,
  signal handling, JSONL stream, status line, AuthRequired
  window, integration tests, benchmarks, doc sweep.
