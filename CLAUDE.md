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

> **Canonical guide:** `docs/TESTING.md`. CLAUDE.md §5 is policy;
> TESTING.md has the mechanics (helpers, naming, goleak, fuzz,
> teatest patterns, anti-patterns). When in doubt, read TESTING.md
> first.

Every spec lands with all four layers green:

- **Unit** (`*_test.go`): pure functions, parsers, encoders,
  migrations, evaluators. ≥80% coverage on `internal/store`,
  `graph`, `pattern`, `auth`, `sync`. Race detector mandatory.
- **Benchmarks** (`Benchmark*`): one per perf-budget row in the
  spec. Fails the test if the budget is missed by >50%. Fixtures
  in `internal/<pkg>/testdata/`; synthesised fixtures in
  `internal/<pkg>/testfixtures.go`, not committed as binary blobs.
- **Integration** (`integration_test.go`, build-tag `integration`):
  real SQLite in tmpdir, `httptest.Server` replaying canned Graph
  JSON from `internal/graph/testdata/`.
- **TUI e2e** (`*_e2e_test.go`, build-tag `e2e`): drive
  `teatest.NewTestModel`; assert on the **visible delta a real
  user would notice**, not just "some string appears in the
  buffer". Every key binding, focus change, cursor move, mode
  change, pane swap must have a test that captures frames
  before/after and asserts the user-visible glyph moved (cursor
  `▶` row, focus marker `▌ <Pane>`, viewer content swap, modal
  centering).

The "visible delta" rule exists because v0.2.6 shipped with passing
dispatch tests but broken e2e visual feedback — real-tenant users
couldn't see the cursor move because the assertions never verified
visible feedback. `internal/ui/app_e2e_test.go` is the source of
truth: new keymap entries, new panes, new modes land alongside
their test.

We never test live tenant calls in CI (manual smoke in
`docs/qa-checklist.md` before each release), the Microsoft Graph
SDK (we don't depend on it), or macOS Keychain end-to-end (mock
the `keyring` interface).

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

> **Canonical sources:** spec 17
> (`docs/specs/17-security-testing-and-casa-evidence.md`) for CI
> gates, security tests, and CASA evidence;
> `docs/THREAT_MODEL.md` for threats + mitigations;
> `docs/PRIVACY.md` for data-flow claims. Future specs MUST
> review spec 17 per §11 and surface threat-model deltas in the PR.

The five hard invariants — apply to every line of code, every log
line, every test fixture:

1. **No mail content leaves `~/`.** SQLite cache at
   `~/Library/Application Support/inkwell/mail.db` (mode 0600);
   logs at `~/Library/Logs/inkwell/`. Nothing else.
2. **Tokens in Keychain only.** Never on disk, never in env vars
   (except transient mock-keyring tests), never in logs, never in
   UI error messages.
3. **Log redaction is mandatory.** `internal/log/redact.go` scrubs
   Bearer tokens, refresh tokens, MSAL blobs, message bodies
   (always), email addresses (per-session keyed), and subject
   lines outside DEBUG level. If you add a log site that could see
   a secret, add a redaction test for it.
4. **No `Mail.Send`, no scopes outside PRD §3.1.** Drafts only;
   user finalises send in native Outlook. CI lint enforces. If a
   feature would need a denied scope, it's out of scope — do not
   work around.
5. **No telemetry, no analytics, no auto-updater.** Zero outbound
   calls except Graph and AAD. Crash dumps stay local, also
   redacted.

Destructive actions (`D` permanent delete, bulk ops, sign-out +
cache purge) carry a confirmation gate defaulting to "No". Test
fixtures use the synthetic domain `example.invalid` with redacted
bodies — real customer data never enters the repo. `go.sum` is
committed; `go mod tidy` runs in a separate reviewed commit
(dependency drift is a security event).

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
- **Never add `Co-Authored-By` trailers** to commit messages. Commits are
  solely attributed to the human committer.
- **Always check CI after a push or tag.** Local green is necessary,
  not sufficient. After every `git push` or `git push --tags`, run
  `gh run list --limit 5` and inspect any failure with
  `gh run view <id> --log-failed`. CI runs on a different toolchain
  version + Linux kernel than the dev machine; v0.12.0 shipped a
  govulncheck failure on `main` because Go 1.25.0 (CI) had stdlib
  CVEs that Go 1.26.x (dev) did not. The fix is part of the same
  push, not a follow-up. Treat a red CI on main as a stop-the-line
  signal.

## 11. Definition-of-done checklist

This is the **single canonical checklist** for every spec PR. §12.3
(loop exit criteria) and §12.6 (ship-time doc sweep) both reference
back here instead of restating. Copy into the PR description with
ticked boxes; link the green CI run.

**Spec content**
- [ ] Which Graph scope(s)? Are they in PRD §3.1?
- [ ] What state does it read from / write to in `store`?
- [ ] What Graph endpoints does it call?
- [ ] How does it behave offline?
- [ ] What is its undo behaviour?
- [ ] What error states surface to the user, and how?
- [ ] Is there a CLI-mode equivalent (PRD §5.12)?

**Tests + benchmarks** (all must be green on a clean checkout)
- [ ] `go vet ./...`
- [ ] `go test -race ./...` (unit + dispatch)
- [ ] `go test -tags=integration ./...`
- [ ] `go test -tags=e2e ./...` (if the spec touches the TUI)
- [ ] Every perf budget in the spec has a benchmark; passes within
      budget on the dev machine (>50% over budget fails — §5.2)
- [ ] Redaction tests cover every new log site that could see
      secrets

**Security (spec 17)**
- [ ] If this PR introduces or changes token handling, file I/O
      paths, subprocess invocation, external HTTP, third-party data
      flow, cryptographic primitive, SQL composition, or local
      persisted state — `docs/specs/17-*.md` §4, `docs/THREAT_MODEL.md`,
      and/or `docs/PRIVACY.md` updated in the same PR. The PR
      description carries a one-line "spec 17 impact:" note.
- [ ] CI gates green: gosec, Semgrep, govulncheck. Any new
      `// #nosec` annotation carries a one-line WHY comment (no
      blanket suppression). Local `make sec` clean.

**Docs**
- [ ] §12.6 doc-sweep table run in full. Every applicable file
      updated in the same PR (or the immediately-following commit
      if a tag went out first).

No CHANGELOG-style or planning markdown added unless the user
explicitly asked.

---

## 12. The ralph loop — how the AI assistant drives a spec

A **ralph loop** is a self-driven, finite, test-anchored development loop
named after the "ralph" pattern: think → act → critique → adjust → repeat
until exit criteria fire. It exists so the assistant cannot declare a spec
"done" prematurely. Every iteration is grounded in a runnable command whose
output the assistant must read and react to.

### 12.0 Spec writing: verify every concrete claim

Specs make falsifiable claims about the codebase — file paths,
function names, exact call-site counts, type placements, package
imports, performance numbers, expected outputs. **Every concrete
claim must be backed by a tool call** (Read / Grep / Glob /
arithmetic) before the spec is reviewed or implemented. This is
the single most common source of adversarial-review findings.

Audit each spec before submitting:

- **Files in §"Module layout":** every "new file" / "changed file"
  actually exists or will exist at the named path. Grep for the
  package to confirm placement.
- **Type placement:** new types live where their consumers can
  reach them without skip-layering. If `ui` and `action` both
  consume the type, it lives in a package below both (e.g.
  `internal/compose`, `internal/store`, or a new neutral package).
  Layering is in §2 — verify the proposed package against it.
- **Call-site counts:** "all 3 places that call `NewX`" → grep
  `NewX(` and confirm the count. Be explicit about which sites
  need the change vs. which don't (a constructor call that's
  immediately reset doesn't).
- **Tests / stubs to update:** name them. `stubDraftCreator` in
  `dispatch_test.go`, `TestCreateNewDraftSinglePost` in
  `executor_test.go`. "Existing tests updated" is not enough.
- **Performance numbers:** do the arithmetic. If the spec says
  "5 MB/s on 1 KB renders 10 KB in 0.3 ms", multiply: 10 KB ÷
  5 MB/s = 2 ms, not 0.3 ms. Pick one consistent number.
- **Code snippets:** mentally compile them. A snippet that calls
  `html.WithHardWraps()` with a comment "disabled" gets
  copy-pasted and breaks: the call **enables** hard-wrap.
- **Cross-doc updates:** ARCH.md, CONFIG.md, PRD.md, README.md.
  New package → ARCH.md module tree gets a row. New config key →
  CONFIG.md row. New CLI verb → reference.md.
- **Lifecycle ambiguity:** when two states can exist for the same
  entity (a `compose_sessions` row AND a `Pending` action), the
  spec must say which wins.

**Red-flag phrases** ("works naturally", "no special handling
needed", "this is fine") are unverifiable. Either show the
expected output explicitly (a code fragment, an exact HTML
fragment, a state-machine diagram) or remove the phrase. The
reader cannot verify "works naturally"; they can verify a literal
output.

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

   **When this is the final iteration (spec shipping):** run the full
   §12.6 doc sweep. Every file in that table must be updated before
   the tag is pushed, not after.

7. **Decide.** Are all DoD bullets ticked, all five mandatory commands (§5.6)
   green, all perf budgets measured and met, all redaction tests passing?
   - **Yes:** loop exits. Open a PR with the spec's DoD copy-pasted, all
     boxes ticked, attached benchmark numbers, and the redaction test
     listing. Stop.
   - **No:** record the iteration outcome in the tracking note. Schedule the
     next iteration. Go to phase 1.

### 12.3 Exit criteria for any spec loop

The loop exits when **all of §11** is ticked AND every bullet from
the spec's own §"Definition of done" is ticked. §11 is the
canonical list; do not duplicate it here.

If after **8** iterations the checklist is not fully green, the
loop **stops and asks the user**. Do not loop forever; do not
silently give up.

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

### 12.6 Doc sweep at ship time

When the spec's tag is pushed and all gates are green, update **every**
applicable file below in the same commit (or the immediately-following
one if the tag went out first). This is the authoritative checklist;
§12.3 and §11 both reference it.

| File | What to update |
| ---- | -------------- |
| `docs/plans/spec-NN.md` | Set `Status: done`. Add a final iteration entry with the tag, measured perf numbers, and any noted deviations from spec. |
| `docs/specs/NN-*.md` | Add a `**Shipped:** vX.Y.Z` line at the top of the spec (inside the opening metadata block or just below the title). |
| `docs/PRD.md` §10 | Mark the spec's inventory row as shipped with version. |
| `docs/ROADMAP.md` | In the relevant **bucket table**: change the status cell to `Shipped vX.Y.Z`. In the **§1 backlog heading**: change `— P1/P2` to `— Shipped vX.Y.Z (spec NN)`. |
| `docs/user/reference.md` | Add every new surface (see trigger list below). Update the `_Last reviewed against vX.Y.Z._` footer. |
| `docs/user/how-to.md` | Add a recipe if the spec introduces a task flow a user would look up. Skip only if no new task flow exists. |
| `docs/user/tutorial.md` | Update only if the first-30-minutes path changed (rare). |
| `docs/user/explanation.md` | Update only if a design invariant changed (rarer). |
| `docs/CONFIG.md` | Rows for every new config key (should already be done per §9 — verify). |
| `README.md` | In the **Status table**: add a row for the new capability with `✅ vX.Y.Z`. Update the get-started download example to the new version if this is the latest release. |

**Trigger list for `reference.md`** — if ANY of these are new or changed, the
reference is incomplete and the spec is not done:

- A key binding in any pane or mode (check `internal/ui/keys.go` `DefaultKeyMap`)
- A `:command` bar verb (check `internal/ui/palette_commands.go` and `app.go`)
- A CLI subcommand or flag (check `cmd/inkwell/`)
- A pattern operator (check `internal/pattern/`)
- A new mode or chord (check `internal/ui/messages.go` mode constants)
- A new sidebar section, indicator glyph, or status-bar element
- A viewer header line

The mechanical check: diff `internal/ui/keys.go`, `internal/ui/palette_commands.go`,
`cmd/inkwell/`, `internal/pattern/` against the previous spec's merge-base and
confirm every new symbol appears in `reference.md`.

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
- Verifier: <test name or check> red → green
- Commands run: ...
- Result: ...
- Critique: ...
- Next: ...

### Iter 2 ...
```

**Verifier line (lightweight goal-driven execution).** Every
iteration names a single concrete signal that flips red → green
when the slice is done — a test name (`TestFooHandlesEmptyInput`),
a benchmark, a `make sec` exit code, a doc-sweep diff. If you
can't name a verifier, the slice is too vague: split it. The
verifier is what proves the slice landed; "looks right to me" is
not a verifier. Exception: pure-design iterations (writing or
revising spec text) take `Verifier: docs review` and rely on the
§12.0 spec-verification discipline instead.

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
│   ├── adr/                   # immutable records of cross-cutting decisions
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

---

## 16. Common review findings

A running list of issues that have shown up in adversarial reviews.
Use this as a pre-review checklist for new specs and PRs. When a
new pattern of finding emerges, add a bullet here — institutional
memory beats re-learning.

**Spec / design**

- Type placed at the wrong layer (e.g. `DraftBody` in `internal/ui`
  but referenced from `internal/action` → upward import). Resolve
  by moving the type down to a package both layers depend on.
- Performance budget numbers self-contradicting (throughput vs.
  per-input math doesn't agree). Do the arithmetic in the spec.
- "Works naturally" / "no special handling needed" without showing
  the expected output. Concretise to a code fragment, exact HTML,
  or state-machine outcome.
- Crash-recovery / lifecycle ambiguity: when state X and state Y
  both exist for the same entity (a `compose_sessions` row AND a
  `Pending` action), the spec must say which wins.
- DoD lists changes by directory but skips specific files. Name
  the test stubs (`stubDraftCreator`), the specific test functions
  to update, and the `go.mod` / `go.sum` update.
- Code snippets that "look right" but contain misleading function
  calls (e.g. `html.WithHardWraps()` with a comment "disabled" —
  the call enables it). Mentally compile every snippet.
- Cross-doc updates (ARCH.md / CONFIG.md / README.md) missing from
  the §"Changed files" list. Use the §12.6 doc-sweep table.

**Implementation**

- New `// #nosec` annotation without a one-line WHY comment (§11).
- Function uses `os.ReadFile(path)` with a user-supplied path but
  doesn't gate it through the spec 17 §4.4 path-traversal check.
- New log site that could see body / token / PII without a
  redaction test (§7 invariant 3).
- Test asserts "string appears in buffer" instead of "visible
  delta a user would notice" (§5 visible-delta rule).
- New table or column added but the schema-version test
  (`tabs_test.go`, `sender_routing_test.go`) still asserts the old
  version number.
- Migration file added but `SchemaVersion` in `store.go` not bumped.
- New `tea.Cmd` does I/O with `context.Background()` instead of a
  parent context tied to the request lifecycle (§8 code style).

**Process**

- Shipped spec without `docs/plans/spec-NN.md` (§13 rule, added
  after spec 16 v0.12.0 ship).
- Tag pushed without checking CI status. After every push or tag,
  run `gh run list --limit 5` and inspect any failure (§10).
- Local `make regress` green but CI red — different toolchain
  version / kernel. Treat red CI on `main` as stop-the-line.
- Commit message includes a `Co-Authored-By` trailer (forbidden
  by §10).
- Cherry-picked fix to `main` from a feature branch without
  verifying the worktree state first (feature branch may be
  ahead of `main` in unrelated commits).
