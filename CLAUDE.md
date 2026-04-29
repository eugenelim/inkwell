# CLAUDE.md — Inkwell contributor & AI-assistant guide

This file is loaded into every Claude Code session for this repo. It encodes the
non-negotiable conventions, the test architecture, the privacy/performance
rules, and the **ralph loop** the AI assistant uses to drive each spec to a
clean, benchmarked, reviewed Definition-of-Done.

Always read these in this order before touching code:

1. `docs/PRD.md` — what we're building, granted vs denied Graph scopes.
2. `docs/ARCH.md` — module layout, layering, data flow, invariants.
3. `docs/CONFIG.md` — config keys (skim, reference on demand).
4. `docs/specs/NN-*.md` — the spec you're implementing.

If a spec contradicts ARCH/PRD, the spec is wrong. Fix the spec first.

---

## 1. Stack invariants (do not negotiate)

- **Go 1.23+**, single module rooted at `github.com/<owner>/inkwell` (placeholder
  name; rename pre-public). `go.mod` lives at repo root.
- **TUI:** `github.com/charmbracelet/bubbletea` + `bubbles` + `lipgloss`.
- **HTTP:** `net/http` only. No Microsoft Graph SDK. We control batching and
  throttling ourselves (ARCH §1, §5).
- **Auth:** `github.com/AzureAD/microsoft-authentication-library-for-go` (MSAL Go).
- **Storage:** `modernc.org/sqlite` (pure Go, no CGO) with FTS5. WAL mode.
- **Keychain:** `github.com/zalando/go-keyring`.
- **HTML→text:** `github.com/jaytaylor/html2text`.
- **Logging:** `log/slog` (stdlib) with JSON handler + redaction.
- **Config:** TOML via `github.com/BurntSushi/toml`.
- **Test:** stdlib `testing`, `github.com/stretchr/testify`, and
  `github.com/charmbracelet/x/exp/teatest` for TUI.

Pure-Go stack is mandatory. CGO breaks macOS notarization and cross-compilation.
If you find yourself reaching for a CGO dependency, stop and ask.

**API surface:** Microsoft Graph **v1.0 only** (`https://graph.microsoft.com/v1.0`).
No Outlook REST v2.0, no `/beta`, no EWS, no IMAP/SMTP. ARCH §0 explains why; do
not relitigate.

## 2. Layering rules

Dependencies flow downward. No cycles. No skip-layering.

```
ui, cli                              ← top
sync, action, savedsearch, search,
render, settings, pattern            ← middle
graph, store                         ← lower
auth, config, log                    ← bottom
```

Concrete enforcement:

- `ui` never imports `graph` directly. It goes through `sync` / `action` /
  `render`.
- `store` is the **single owner** of `mail.db`. Nothing else opens the file.
- `graph` is the only package that talks to `https://graph.microsoft.com`.
- `auth` is the only package that talks to AAD / MSAL / Keychain.
- The action queue is the only path for writes. Direct PATCH/POST against
  Graph from anywhere else is a bug.

Add `internal/` to all package paths so external imports are impossible.

## 3. The three data-model invariants

1. **SQLite is the only persistent state.** Nothing in memory survives restart.
2. **The UI never blocks on I/O.** Every Graph call and DB query is a
   `tea.Cmd`. The Update function is pure: `(Model, Msg) → (Model, Cmd)`.
3. **Optimistic writes:** apply locally → enqueue action → reconcile on Graph
   response. Rollback on failure surfaces a status-line toast. Every mutation
   is idempotent (404-on-delete is success).

If a change violates any of these, the change is wrong.

## 4. Bubble Tea conventions

- Sub-models are **value types**, not pointers. Bubble Tea returns a fresh
  model each Update; pointer aliasing across cycles has caused real bugs in
  real apps. Compose by value.
- One root `Update`. It dispatches by `Mode` (Normal / Command / Search /
  SignIn / Confirm). Mode-dispatch keeps modal state strict.
- Pane-scoped keybindings: a single `KeyMap` binding name (e.g. `MarkRead`)
  resolves to different actions depending on focused pane. The global handler
  tries pane-specific override first, then falls back to global default.
- Long operations return a `tea.Cmd`. The Cmd does the I/O on its own
  goroutine and emits a typed `tea.Msg` on completion. The next Update cycle
  reconciles state.
- Lip Gloss styles live in `internal/ui/theme.go` and `internal/render/theme.go`.
  No inline ANSI escapes anywhere else.
- Any feature that paints to the screen must respect `WindowSizeMsg` and
  re-layout. Hard-coded widths only as defaults from `[ui]` config.

## 5. Test architecture

The test pyramid for this repo is non-negotiable. Every spec lands with all
four layers green.

> **Reference:** `docs/TESTING.md` is the canonical guide for how tests
> are written, named, and run in this repo. CLAUDE.md §5 is the
> high-level policy; TESTING.md has the mechanics (helpers, naming,
> goleak, fuzz, teatest patterns, anti-patterns). When in doubt, read
> TESTING.md first.

### 5.1 Unit tests (`*_test.go` next to source)

- Pure functions, parsers, encoders, schema migrations, FTS triggers,
  pattern AST evaluators, body-cache LRU math.
- `testify/require` for fail-fast assertions; `testify/assert` for aggregating.
- Table-driven where it helps; one test per logical case otherwise.
- Race detector mandatory: `go test -race ./...`.
- Goal: **≥80% coverage** on `internal/store`, `internal/graph`,
  `internal/pattern`, `internal/auth`, `internal/sync`. UI/CLI coverage is
  measured but not gated.

### 5.2 Benchmarks (`*_test.go`, `Benchmark*`)

- Every spec with a §"Performance budgets" section ships a benchmark per
  budget that **fails the test** if the p95 budget is missed by >50%.
- Use `testing.B` with `b.ReportAllocs()`. Fixtures live under
  `internal/<pkg>/testdata/`.
- Synthesised fixtures (e.g., 100k-message store) are generated by helpers in
  `internal/<pkg>/testfixtures.go`, not committed as binary blobs.
- Run via `go test -bench=. -benchmem -run=^$ ./...`.

### 5.3 Integration tests (`integration_test.go`, build-tag `integration`)

- Talk to a real local SQLite file (tmpdir) and a recorded Graph API HTTP
  fixture set (`httptest.Server` replaying canned JSON from
  `internal/graph/testdata/`).
- Cover: open → migrate → insert → close → reopen, delta replay, batch retry,
  $batch chunking, action drain → reconcile.
- Build tag keeps them out of the default `go test` run; CI runs both.

### 5.4 TUI end-to-end (`*_e2e_test.go`, build-tag `e2e`)

- Drive the Bubble Tea program with `teatest.NewTestModel`.
- Script keystrokes; assert on rendered final frame and on emitted Cmds.
- Wire a fake `sync.Engine` and in-memory `store.Store` so tests are
  deterministic.

**Per-control coverage is mandatory.** Every key binding in
`internal/ui/keys.go` and every visible state transition (focus change,
cursor move, mode change, pane content swap) must have an e2e test.
The test's pass condition must be the **visible delta a real user would
notice**, not just "some string appears in the buffer". Concretely:

- Cursor moves: capture frame before and after, assert the cursor glyph
  (`▶` or whatever the theme uses) is on a different row.
- Focus changes: assert the focus marker (`▌ <Pane>`) moves OFF the
  previously-focused pane AND onto the newly-focused one.
- Open / activate: assert the destination pane's content visibly
  changes (e.g. viewer was "(no message selected)", is now
  "From: …\nSubject: …").
- Mode changes: assert the mode-specific UI element renders (command
  bar shows `:`, sign-in modal renders centered, etc.).

A binding without a test is a binding that doesn't work in production.
We learned this the hard way after v0.2.6 shipped with passing tests
that asserted strings in the buffer, while real-tenant users couldn't
see the cursor move or focus change because the assertions never
verified visible feedback.

The `internal/ui/app_e2e_test.go` file is the source of truth: any
new keymap entry, any new pane, any new mode must land alongside a
test that exercises it the way a user would.

### 5.5 What we never test

- Live tenant calls in CI. Manual smoke is documented in
  `docs/qa-checklist.md` and run before each release.
- The Microsoft Graph SDK. We don't depend on it.
- macOS Keychain end-to-end in CI. Mock the `keyring` interface.

### 5.6 Mandatory commands

```sh
go vet ./...
go test -race ./...                         # unit + race
go test -tags=integration ./...             # integration
go test -tags=e2e ./...                     # TUI e2e
go test -bench=. -benchmem -run=^$ ./...    # benchmarks
go build ./...                              # everything compiles
```

A spec is not done until **all five** pass on a clean checkout.

### 5.7 The full regression suite (`make regress`)

`scripts/regress.sh` (also wired as `make regress`) runs every gate
from §5.6 in one command:

1. `gofmt -s` — fails if any file is unformatted.
2. `go vet ./...`
3. `go build ./...`
4. `go test -race ./...` (unit + dispatch).
5. `go test -tags=e2e ./...` (TUI visible-delta).
6. `go test -tags=integration ./...` (only if any test file declares the tag).
7. `go test -bench=. -benchmem -run=^$ ./...` (perf budgets).

**Mandatory**:

- After every substantial change (anything beyond a single-file edit
  whose blast radius is obvious from the diff).
- **Always** before tagging a release. No exceptions. If `make regress`
  is red, the tag does not happen.

The suite is the institutional memory for the bugs that have already
shipped and were caught after the fact. v0.2.6 → v0.2.7 happened
because dispatch tests passed but the e2e visual feedback was broken.
v0.2.8 → v0.2.9 happened because a height off-by-one trimmed the help
bar. v0.3.0 → v0.3.1 happened because the soft_delete FK was caught
by a regression test we hadn't written until the user hit it. Each of
those is now in the suite. Adding to the suite is how we keep them
from coming back.

When you fix a bug a user reported, write the regression test BEFORE
the fix lands in the same commit, and add it to the relevant package's
test file. The next user who hits a similar issue gets a green test
that proves the surface is intact, or a red one that points at the new
regression.

### 5.8 Linting

- `gofmt -s` (simplify).
- `go vet`.
- `staticcheck ./...` (install via `go install honnef.co/go/tools/cmd/staticcheck@latest`).
- `golangci-lint run` with the config at `.golangci.yml` once the project
  ships one.

---

## 6. Performance budgets (do not regress)

These come from PRD §7 and per-spec §"Performance budgets". Every change must
verify the relevant budget via benchmark. A regression >50% blocks merge.

| Surface | Budget |
| --- | --- |
| Cold start to interactive TUI | <500ms |
| Folder switch / message open from cache / local search | <100ms |
| RSS at steady state | <200MB |
| `GetMessage(id)` cached | <1ms p95 |
| `ListMessages(folder, limit=100)` over 100k msgs | <10ms p95 |
| `UpsertMessagesBatch(100)` | <50ms p95 |
| `Search(q, limit=50)` over 100k msgs | <100ms p95 |
| Bulk pattern delete of N matches | <30s end-to-end (PRD §9) |

Optimisations that violate the layering or simplicity rules are not allowed.
First make it correct, then make it fast — but the "fast" step is mandatory,
not optional.

---

## 7. Privacy and security (non-negotiable)

> **Cross-reference:** spec 17
> (`docs/specs/17-security-testing-and-casa-evidence.md`) is the
> canonical source for security CI gates, security-specific tests,
> and the threat-model / privacy-policy documents. The rules below
> are the day-to-day implementation contract; spec 17 is the
> hardening + evidence layer that proves the rules hold. Future
> specs MUST review spec 17 (per §11 cross-cutting checklist) and
> surface threat-model deltas in their PR.

These rules apply to every piece of code, every log line, every test fixture.

1. **No mail content leaves `~/`.** SQLite cache lives at
   `~/Library/Application Support/inkwell/mail.db` with mode `0600`. Logs at
   `~/Library/Logs/inkwell/`. Nothing else.
2. **Tokens live only in Keychain.** Never on disk. Never in env vars (except
   transient tests with a mock keyring). Never in logs. Never in error
   messages returned to the UI.
3. **Mandatory log redaction** (ARCH §12). The slog handler at
   `internal/log/redact.go` scrubs:
   - Bearer tokens (regex `Bearer [A-Za-z0-9._-]+`)
   - Refresh tokens, MSAL cache blobs
   - Message bodies, **always**
   - Email addresses → `<email-N>` keyed per-session
   - Subject lines outside DEBUG level
4. **Test fixtures are scrubbed.** Recorded Graph responses under
   `internal/graph/testdata/` use the synthetic domain `example.invalid` and
   redacted message bodies. Real customer data never enters the repo.
5. **No telemetry by default.** Zero outbound calls except to Graph and AAD.
   Crash dumps stay local, also redacted.
6. **No third-party analytics, no auto-updater, no usage pings.**
7. **Permissions discipline.** If a feature would need a Graph scope outside
   PRD §3.1, the feature is out of scope. Do not work around denied scopes.
8. **Drafts only, never `Mail.Send`.** The user finalises send in native
   Outlook. This is a hard scope boundary.
9. **Confirmation gates** for destructive actions: `D` (permanent delete),
   bulk operations, sign-out + cache purge. Default to "No".
10. **`go.sum` is committed.** Dependency drift is a security event;
    `go mod tidy` in a separate commit, reviewed.

If a code path could log a token, body, or PII, add a redaction test for it.

---

## 8. Code style

- Default to writing **no comments**. Only add a comment when the **why** is
  non-obvious — a hidden constraint, a workaround, a surprising invariant.
  Don't restate what the code does.
- Doc comments on every exported identifier. One sentence, declarative,
  starts with the identifier name (`// Engine is …`).
- Errors flow up; wrap with `fmt.Errorf("doing X: %w", err)`. Sentinels via
  `errors.Is`. Custom error types only when callers need to branch on them
  (e.g., `*graph.GraphError`, `*store.MigrationError`).
- `context.Context` is the first parameter on every I/O-bound function.
  Honour cancellation everywhere; never `context.Background()` inside a
  request path.
- Prefer small interfaces defined at the **consumer** site (Go convention).
  Don't pre-declare giant interfaces upfront unless the spec mandates one
  (e.g. `auth.Authenticator`, `store.Store`, `sync.Engine` — these are
  contract surfaces and live with the implementation).
- No `init()` functions for anything beyond registering test fixtures.
- No global mutable state. Constructors return values; main wires them up.
- Keep functions <60 lines unless there's a strong reason. Split on logical
  boundaries, not arbitrary line counts.
- File names are lowercase, words separated by `_` only when needed for
  readability (e.g. `keychain_cache.go`).

## 9. Configuration discipline

- Every new config key lands in `docs/CONFIG.md` **in the same change** that
  introduces it.
- Defaults are struct literals in `internal/config/defaults.go`, not constants
  scattered across packages.
- Validation errors produce line numbers; the app refuses to start on invalid
  config (ARCH §11).
- No hot reload. Config changes require restart.
- The scopes list (spec 01 §6) is **not** configurable. It's a contract with
  the tenant admin and changes via code review.

## 10. Git / commit conventions

- Conventional commit prefixes: `feat(spec-NN): ...`, `fix(pkg): ...`,
  `test(pkg): ...`, `bench(pkg): ...`, `docs: ...`, `chore: ...`.
- One spec per branch where practical: `feat/spec-01-auth`, etc.
- PR description mirrors the spec's Definition-of-Done checklist with
  ticked boxes and links to the green CI run.
- No squash of unrelated changes; rebase to keep history readable.
- Never `--no-verify`, never force-push to `main`.
- **Always check CI after a push or tag.** Local green is necessary,
  not sufficient. After every `git push` or `git push --tags`, run
  `gh run list --limit 5` and inspect any failure with
  `gh run view <id> --log-failed`. CI runs on a different toolchain
  version + Linux kernel than the dev machine; v0.12.0 shipped a
  govulncheck failure on `main` because Go 1.25.0 (CI) had stdlib
  CVEs that Go 1.26.x (dev) did not. The fix is part of the same
  push, not a follow-up. Treat a red CI on main as a stop-the-line
  signal.

## 11. Cross-cutting checklist for every spec PR

Copy from ARCH §16 into every PR description:

- [ ] Which Graph scope(s) does it require? Are they in PRD §3.1?
- [ ] What state does it read from / write to in `store`?
- [ ] What Graph endpoints does it call?
- [ ] How does it behave offline?
- [ ] What is its undo behaviour?
- [ ] What error states surface to the user, and how?
- [ ] What is the latency budget, and is it benched?
- [ ] What logs does it emit, and what is redacted?
- [ ] Is there a CLI-mode equivalent (PRD §5.12)?
- [ ] Are unit / integration / e2e / bench tests all present and green?
- [ ] **Spec 17 review (security testing + CASA evidence)** — does
      this PR introduce or change any of: token handling, file I/O
      paths, subprocess invocation, external HTTP, new third-party
      data flow, new cryptographic primitive, new SQL composition,
      new local persisted state? If **any** of those, the PR MUST
      update `docs/specs/17-security-testing-and-casa-evidence.md`
      §4 (security tests), `docs/THREAT_MODEL.md` (threats &
      mitigations once it lands), and/or `docs/PRIVACY.md`
      (where data is stored / what leaves the device). When in
      doubt, surface it explicitly in the PR description with a
      one-line "spec 17 impact: …" note.
- [ ] **Spec 17 CI gates green** — gosec, Semgrep, govulncheck.
      New `// #nosec` annotations carry a one-line WHY comment
      (no blanket suppression). Local `make sec` clean.

---

## 12. The ralph loop — how the AI assistant drives a spec

A **ralph loop** is a self-driven, finite, test-anchored development loop
named after the "ralph" pattern: think → act → critique → adjust → repeat
until exit criteria fire. It exists so the assistant cannot declare a spec
"done" prematurely. Every iteration is grounded in a runnable command whose
output the assistant must read and react to.

### 12.1 Loop control

The assistant runs the loop in **dynamic-pacing mode** (no fixed cron). Each
iteration calls back via `ScheduleWakeup` only if the loop has more work to
do. The loop **terminates** when the spec's exit criteria are met or after
**8 consecutive iterations** with no green-test progress (the assistant must
then ask the user for input rather than spin further).

### 12.2 The seven phases of one iteration

For the spec under work, run these phases in order. Do not skip.

1. **Re-read the spec.** Open `docs/specs/NN-*.md`. Re-read PRD §3.1 (scopes)
   and any ARCH section the spec depends on. List the §"Definition of done"
   bullets and the §"Performance budgets" rows. Treat these as the loop's
   exit criteria.

2. **Plan the smallest next slice.** What single, runnable, testable change
   moves at least one DoD bullet from ✗ to ✓? Write it as a one-line task in
   `TaskCreate`. Slices are typed: `schema`, `api`, `wire`, `test`, `bench`,
   `redact`, `e2e`, `polish`.

3. **Implement the slice.** Edit code. Default to writing no comments. Stay
   within the layering rules (§2 here). New file? Add it under
   `internal/<pkg>/`. New config? Add to `docs/CONFIG.md` in the same change.

4. **Self-test.** Run, in this order, only what's relevant to the slice:
   ```sh
   gofmt -s -d <changed files>
   go vet ./...
   go test -race ./internal/<pkg>/...        # unit
   go test -tags=integration ./internal/<pkg>/...  # if relevant
   go test -bench=. -benchmem -run=^$ ./internal/<pkg>/...  # if budget changed
   ```
   Read the output. Do not assume green; copy the actual pass/fail counts
   into the iteration note.

5. **Self-critique.** Ask each question and write an explicit Yes/No answer:
   - Did I introduce a layering violation? (ui→graph, store-bypass, etc.)
   - Did I add a comment that restates the code instead of explaining a
     constraint? Delete it.
   - Did I add a public symbol the spec didn't ask for? Inline it or remove.
   - Could this code log a token, body, or PII? Add a redaction test or fix.
   - Did I weaken a perf budget without justification?
   - Is the change idempotent and crash-safe (action queue, migrations)?
   - Did I rely on `context.Background()` inside a request path? Replace.
   - Are error paths covered, or only the happy path?

   If any answer is "No / oops", that becomes the next iteration's slice.

6. **Update the DoD checklist** in the spec's tracking note (kept under
   `docs/plans/spec-NN.md`, see §13). Mark bullets ticked or note remaining
   work. Update perf-budget table with measured numbers.

   **At the same time, update the user docs.** `docs/user/` follows
   the [Diátaxis](https://diataxis.fr) four-quadrant structure
   (tutorial / how-to / reference / explanation). If this iteration
   added or changed a user-visible surface, `docs/user/reference.md`
   gets the new row (mandatory) and `docs/user/how-to.md` gets a
   recipe if the spec introduces a meaningful new task flow.
   `tutorial.md` and `explanation.md` change rarely — only when the
   first-launch path or a design invariant moves. Internal-only
   refactors (test infra, private helpers, bench tweaks) are exempt.
   The check: would a user reading the reference be surprised that
   this thing exists? If yes, the docs change is mandatory.

7. **Decide.** Are all DoD bullets ticked, all five mandatory commands (§5.6)
   green, all perf budgets measured and met, all redaction tests passing?
   - **Yes:** loop exits. Open a PR with the spec's DoD copy-pasted, all
     boxes ticked, attached benchmark numbers, and the redaction test
     listing. Stop.
   - **No:** record the iteration outcome in the tracking note. Schedule the
     next iteration. Go to phase 1.

### 12.3 Exit criteria for any spec loop

The loop exits **only when all** are true:

- [ ] Every §"Definition of done" bullet in the spec is ticked.
- [ ] `go vet ./...` clean.
- [ ] `go test -race ./...` green.
- [ ] `go test -tags=integration ./...` green.
- [ ] `go test -tags=e2e ./...` green if the spec touches the TUI.
- [ ] Every perf budget in the spec has a benchmark, and the benchmark
      passes within budget on the dev machine.
- [ ] Redaction tests cover every new log site that could see secrets.
- [ ] `docs/CONFIG.md` updated for every new key.
- [ ] **`docs/user/reference.md` updated** for every new keybinding,
      command, mode, or pane glyph the user touches.
- [ ] **`docs/user/how-to.md` updated** when the spec adds a new task
      flow worth a recipe (e.g. "delete all newsletters older than N
      days"). Skip if the spec is purely a primitive used by other
      flows already documented.
- [ ] **`docs/user/tutorial.md` updated** if the spec changes the
      first-30-minutes path (rare). Otherwise skip.
- [ ] **`docs/user/explanation.md` updated** if the spec changes a
      design invariant the explanation file currently asserts (rarer).
- [ ] No CHANGELOG-style or planning markdown added unless the user asked.
- [ ] PR checklist (§11) fully ticked.

If after **8** iterations exit criteria are still not met, the loop must
**stop and ask the user**. Do not loop forever; do not silently give up.

### 12.4 Anti-patterns the loop must reject

- "It compiles, ship it." Compiling is necessary, not sufficient.
- "Tests pass locally on the happy path." Insufficient; cover errors,
  cancellation, retries.
- "Coverage will come later." It won't. Land tests in the same change.
- "I'll add the benchmark next week." Same answer.
- "The spec doesn't strictly require this perf number." If the spec lists a
  budget, the loop measures it. No exceptions.
- "Let me refactor while I'm here." No. Scope discipline. Open a separate
  task.
- "Mock everything." Mock at boundaries (Graph HTTP, Keychain, clock). Real
  SQLite in tmpdir for store tests; real `tea.Program` for TUI e2e.

### 12.5 When the loop should pause and ask

- Spec ambiguity that materially affects the design (not a wording nit).
- A required Graph endpoint behaves differently from the spec's assumption.
- A perf budget is unattainable on the chosen approach; the user must
  decide whether to relax the budget or change the approach.
- Anything that would require a new Graph scope (PRD §3.1).

When pausing, write the question concretely with the smallest reproducible
example, then `ScheduleWakeup` is **not** called. The user resumes the loop.

---

## 13. Per-spec tracking notes

For each spec under active loop, the assistant maintains a running note at
`docs/plans/spec-NN.md`. This file is **the** state of the loop. It contains:

**Mandatory at ship time.** When a spec is shipped (commit + tag +
release), the corresponding `docs/plans/spec-NN.md` MUST exist and
MUST be updated in the same commit that ships the feature. A spec
that ships without a plan file is a missing artefact: the next
contributor (or the assistant in a future session) loses the trail
of decisions, deferred bullets, and known gaps.

The check is mechanical — the spec inventory in `docs/PRD.md` §10
lists every spec; for each row marked "shipped" or with a real
status, `git ls-files docs/plans/spec-NN.md` must return non-empty.
The plan file is the journal; the spec is the contract; both must
land together. Forgetting it once (spec 16 v0.12.0 ship) is the
reason this rule is now in CLAUDE.md.

```
# Spec NN — <title>

## Status
<one of: not-started | in-progress | blocked | done>

## DoD checklist
- [ ] bullet from spec §"Definition of done"
- [ ] ...

## Perf budgets
| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| ...     | ...    | ...      | ...   | ...    |

## Iteration log
### Iter 1 — YYYY-MM-DD
- Slice: <one-liner>
- Commands run: ...
- Result: ...
- Critique: ...
- Next: ...

### Iter 2 ...
```

The note is updated **at every iteration**. It is the only mutable status
artefact. Do not invent other planning files.

When the loop exits successfully, the file is left in place as the artefact
of how the spec landed.

---

## 14. Where things live

```
inkwell/
├── CLAUDE.md                  # this file
├── docs/
│   ├── PRD.md
│   ├── ARCH.md
│   ├── CONFIG.md
│   ├── specs/                 # the source of truth for each feature
│   ├── plans/                 # ralph-loop tracking notes (per-spec)
│   ├── ROADMAP.md
│   └── qa-checklist.md        # manual smoke before release
├── cmd/inkwell/               # main, cobra subcommands
├── internal/                  # everything else (no external imports allowed)
│   ├── auth/
│   ├── config/
│   ├── graph/
│   ├── store/
│   ├── sync/
│   ├── action/
│   ├── pattern/
│   ├── render/
│   ├── search/
│   ├── savedsearch/
│   ├── settings/
│   ├── ui/
│   ├── cli/
│   └── log/
├── scripts/                   # release.sh, dev helpers
├── go.mod
└── go.sum
```

`internal/` everywhere is intentional: nothing the user installs (cmd/inkwell)
imports anything they could re-import. The contract is the binary, not a Go
API.

---

## 15. Quick reference — common commands

```sh
# Lint + vet
gofmt -s -w .
go vet ./...
staticcheck ./...

# Tests by layer
go test -race ./...
go test -tags=integration ./...
go test -tags=e2e ./...
go test -bench=. -benchmem -run=^$ ./...

# Coverage report (per package)
go test -race -coverprofile=cover.out ./internal/store/...
go tool cover -func=cover.out

# Build
go build ./...
go build -o bin/inkwell ./cmd/inkwell

# Module hygiene (separate commit)
go mod tidy
go mod verify
```

A spec is done when these all pass and the spec's DoD is fully ticked. Not
before.
