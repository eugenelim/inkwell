---
name: work-loop
description: Use this skill whenever you're implementing a non-trivial change — a feature, a multi-file bug fix, a refactor, or anything spec-driven. It is the procedural runbook for the ralph loop documented in `docs/CONVENTIONS.md` §12 — plan → execute → gates → review → fix — extended with parallel reviewer dispatch and supervisor mode when the plan permits. Default to this skill for any task larger than a one-line edit.
---

# Skill: work-loop (inkwell)

This is the project's inner loop for non-trivial work. It exists because
LLM self-assessment is unreliable: agents declare victory when they
*feel* done, not when objective gates pass. The skill replaces "feel"
with verifiable termination criteria.

The canonical contract lives in [`docs/CONVENTIONS.md`
§12](../../../docs/CONVENTIONS.md) (the ralph loop, the seven phases,
exit criteria, doc sweep). This skill is the *procedural* runbook —
how to walk the loop, when to fan out, when to stop. Where the two
disagree, CONVENTIONS wins; fix the skill in the same PR.

> **Vocabulary.** "Surface" throughout this skill means: stop the
> current loop, emit a short description of the situation in your
> final message (what happened, what you tried, what state things
> are in), and wait for human direction. Do not retry, do not
> redispatch, do not silently reset. Reviewers also "surface"
> findings in the descriptive sense ("raised") when they return
> their report; context disambiguates.

## When this skill applies

- Implementing a spec from `docs/specs/NN-<title>/`.
- Bug fixes that touch more than one file (also see the `bug-fix`
  skill for the root-cause discipline; this skill runs around it).
- Refactors with no spec.
- Any task where you'd otherwise be tempted to "just go".

For genuine one-line edits (typo, config tweak), skip the loop — the
overhead isn't worth it.

## The loop

These five procedural phases wrap the seven numbered phases in
`docs/CONVENTIONS.md` §12.2: PLAN ⊇ §12.2 step 1–2; EXECUTE ⊇
step 3; GATES ⊇ step 4; REVIEW ⊇ step 5 + the doc/DoD update
half of step 6; DECIDE ⊇ step 7. Where the two disagree,
CONVENTIONS wins.

```
   ┌─────────────────────────────────────────────────────────┐
   │                                                         │
   ▼                                                         │
PLAN  ──►  EXECUTE  ──►  GATES  ──►  REVIEW  ──►  DECIDE    │
                          │           │            │         │
                          │           │            └── findings? ──┐
                          │           │                            │
                          └─ failed? ─┴── findings? ────── fix ────┘
                                                              │
                                                              └── back to GATES
```

### 1. PLAN — think before acting

- If the task has a spec, read `spec.md` and `plan.md` first
  (`CONVENTIONS.md` §13). The plan's task list is your work-breakdown
  — don't invent your own.
- If the task has no spec and is more than a one-file change, **stop
  and use the `new-spec` skill first**. Implementation without a
  contract drifts (the v0.12.0 missing-plan-file class of bug).
  Contract tests are part of the spec — write them *during*
  `new-spec`, not later.
- **Pick the verifier for each plan task** before writing code
  (`CONVENTIONS.md` §13 — the `Verifier:` line). The verifier is the
  single concrete signal that flips red → green when the slice is
  done. If you can't name one, the slice is too vague: split it.
  Modes:
  - **TDD** — pure logic, state machines, anything with a
    compressible invariant. Contract tests in `spec.md`, construction
    tests in `plan.md`, red-green-refactor.
  - **Goal-based** — build config, scaffolding, smoke entry points.
    The verifier is a one-liner (`make build`, `grep`, `go vet`),
    not a test file. Don't write a test that just asserts what the
    compiler proves.
  - **Visual / manual QA** — TUI rendering, end-to-end UX. The
    visible-delta rule (`CONVENTIONS.md` §5) applies: assert what
    the user sees, not internal state. `make ai-fuzz` covers the
    open-ended-invariants flavor.
- For architecturally significant work, enter Plan Mode (Shift+Tab
  twice in Claude Code) and add "think hard" or "ultrathink" to the
  prompt for adaptive thinking depth.
- **Verify every concrete claim** before the spec is reviewed
  (`CONVENTIONS.md` §12.0). Files, types, call-site counts, perf
  numbers — all backed by a tool call. This is the single most
  common source of adversarial-review findings.
- **Spec-mode adversarial review before EXECUTE.** If PLAN produced
  or modified `spec.md` / `plan.md`, invoke `adversarial-reviewer`
  in spec mode and iterate to clean before code starts. Catching a
  vague behavior or a missing `Depends on:` here costs a sentence,
  not a re-plan.

The output of PLAN is a written plan (with verifier per task) you can
return to. Don't keep it in your head — your context will turn over
and you'll lose it.

### 2. EXECUTE — make the change

Match the discipline to the verifier you picked:

- **TDD task** — red-green-refactor:
  1. Write the failing test first (red). Commit if non-trivial.
  2. Write the minimum code to make it pass (green). Commit.
  3. Refactor with the test as safety net. Commit.
- **Goal-based task** — write the code, run the one-liner from
  `Verifier:`. No production test file.
- **Visual / manual QA task** — implement, then run the manual check
  recorded in the task. Record the result.

For each task, implement the smallest coherent unit of work toward
the goal. Resist the urge to fix unrelated things you notice along
the way; note them in `plan.md` for later (`docs/CONVENTIONS.md`
§12.4 anti-patterns).

#### Parallel dispatch discipline

When this skill fans out — multiple implementers in supervisor mode,
or multiple specialist reviewers in REVIEW — the rules are the same
and they live here, single-sourced. Both call sites below reference
this discipline rather than restating it.

- **One tool-call message, one Agent use per target.** Issue all
  subagent invocations in a single message. Do not call them
  sequentially. The participants are independent, the lenses are
  independent, and sequencing tempts you to react to the first
  return before the rest land — which gives each subagent a
  different state.
- **Barrier-wait.** Don't issue follow-on Agent calls until every
  subagent in the round has returned.
- **Harness-level non-returns are failures.** A timeout, a tool
  error, or a missing report counts as `failed` for that target.
  Treat it the same as a substantive `failed` status; do not retry
  silently.
- **Merge results in your own context.** The subagents return
  markdown. You read N reports, group findings or status by your
  own bookkeeping (the spec's `plan.md` for implementers; severity
  buckets for reviewers), then decide.

#### Supervisor mode (parallel implementers)

If the plan has **two or more tasks declaring `Depends on: none`**,
EXECUTE branches into supervisor mode. You become the supervisor;
each independent task gets an `implementer` subagent (see
[`.claude/agents/implementer.md`](../../agents/implementer.md)) in
its own worktree. The full rationale and merge discipline live in
[`CONVENTIONS.md` §12.7](../../../docs/CONVENTIONS.md). Throughout
this procedure, **"task-id order" means numeric where IDs look like
`T1`, `T2`, …; lexicographic otherwise.**

The procedure:

0. **Pre-flight: check for stale worktrees.** Run
   `git worktree list` and `git worktree prune`. If
   `.worktrees/<task-id>/` exists or branch `<base>-<task-id>`
   already exists for any task you're about to dispatch, a prior
   session left scratch behind. **Surface to a human; do not
   silently reuse or destroy** — the scratch may carry in-flight
   work the previous run was about to commit.
1. **Set up worktrees.** For each independent task `<task-id>`:
   ```bash
   git worktree add .worktrees/<task-id> \
     -b "$(git branch --show-current)-<task-id>"
   ```
2. **Dispatch implementers in parallel** per the parallel-dispatch
   discipline above. Each brief includes: the task ID, the
   plan-task body, the absolute worktree path, and absolute paths
   to `spec.md` and `plan.md`.
3. **Persist each report.** For each returning subagent, write the
   report verbatim to
   `docs/specs/<feature>/notes/implementer-<task-id>-<iteration>.md`
   (path is gitignored — session scratch). `<iteration>` is the
   current `iteration_count` from `state.json` (0-indexed, see
   `docs/_templates/state.json`'s `$indexing` note) — so the first
   attempt at T1 lands as `implementer-T1-0.md`. The counter is
   bumped after REVIEW completes, before the next PLAN; that
   ordering keeps reports from clobbering one another across
   re-plan attempts. Match the report's opening `## Task <task-id>`
   heading against the plan; if it doesn't match a task you
   dispatched, surface it as a failed task for an unknown name —
   never silently rename.
4. **Handle non-ready tasks first.** If any implementer reports
   `blocked` or `failed`, do not merge. Surface the failed-task
   list with report-path pointers, **bump your iteration counter**,
   then return to PLAN and revise the offending task. Do not
   redispatch the same implementer on the same task — the
   assumption that produced the failure is what needs revising.
5. **Merge ready tasks sequentially.** From the primary worktree,
   in task-id order:
   ```bash
   git merge --no-ff "$(git branch --show-current)-<task-id>"
   ```
   A conflict means the tasks weren't actually independent. Abort
   (`git merge --abort`), return to PLAN, fix the `Depends on:`
   declarations, and **bump your iteration counter** (same
   rationale as step 4 — the iteration ran, it just terminated
   early for a recoverable reason).
6. **Clean up worktrees.** After all merges succeed:
   ```bash
   git worktree remove .worktrees/<task-id>
   ```
   If that fails (uncommitted files, locked index, build
   artifacts), retry once with `--force`. On persistent failure,
   leave the directory in place, note the path in your end-of-loop
   summary, and proceed to gates — don't block on cleanup.
7. **Run gates yourself** (next phase). The implementers' gate
   results were advisory; the gates of record run in the primary
   against the merged state.

In single-agent mode (no independent tasks), skip the supervisor
branch entirely and execute as the sole agent — that's the default
flow above. The trigger is **structural** (the plan's shape), not a
user choice.

### 3. GATES — mechanical verification

Run, in order, only what's relevant to the slice
(`docs/CONVENTIONS.md` §5.6 / §5.7):

```sh
gofmt -s -d <changed files>
go vet ./...
staticcheck ./...
go test -race ./internal/<pkg>/...                       # unit + dispatch
go test -tags=integration ./internal/<pkg>/...           # if relevant
go test -tags=e2e ./internal/<pkg>/...                   # if TUI touched
go test -bench=. -benchmem -run=^$ ./internal/<pkg>/...  # if budget changed
```

Before tagging: `make regress` (the full suite, §5.7). For
TUI-touching specs the DoD requires `make ai-fuzz` smoke and a
Claude Code oracle pass of the latest
`.context/ai-fuzz/run-*/REVIEW.md` (§11).

If a gate fails, go to FIX. Don't move past a failing gate by
editing the gate.

### 4. REVIEW — adversarial self-review

After gates pass, run adversarial review against the spec.

```
Use the adversarial-reviewer subagent to review my changes against
docs/specs/<feature>/spec.md
```

Findings come back grouped by severity (Blockers / Concerns / Nits),
each with a one-sentence `Fix:`. Iterate until the agent returns
`Clean — ready to commit.`

**Specialist reviewers — after adversarial-reviewer is clean.** Pick
the ones the diff warrants; don't run all three by default.

- `security-reviewer` — diffs that cross a security boundary
  (auth, Graph HTTP, SQL composition, file I/O, subprocess, new
  on-disk surface, log redaction). Attacks along the five `§7`
  hard invariants + spec 17 threat-model table + STRIDE.
- `quality-engineer` — testability, observability, reliability,
  perf-budget honesty, maintainability. Different lens from
  adversarial-reviewer — don't skip it because the spec already
  shipped.

**Dispatch reviewers in parallel when you invoke more than one**, per
the [Parallel dispatch discipline](#parallel-dispatch-discipline)
under EXECUTE — the same rules cover both fan-out sites.

#### Fingerprint stasis — when the same findings come back twice

After each reviewer pass, before the next iteration:

1. Compute a fingerprint per finding: `sha1("<file>|<line>|<title>")`
   where `<line>` is the first integer after the first colon in the
   citation (`foo.go:88` → `88`; `foo.go:88-92` → `88`) and
   `<title>` is the reviewer's bolded heading verbatim.
2. Compare against the previous iteration's fingerprint set. If
   the two sets are **identical** (nothing resolved, nothing new),
   **stop and surface** to a human. A strict subset means one or
   more findings were fixed — that's progress, keep going. New
   findings the previous pass didn't see are a different problem,
   also not stasis. See `docs/CONVENTIONS.md` §12.8 for the
   formula and the carve-out for findings without a `file:line`
   citation.

The state-file machinery (`docs/_templates/state.json` +
`tools/check-done.py`) is optional bookkeeping for this check; in
practice, eyeballing the previous review's fingerprints against
this iteration's is usually enough.

### 5. DECIDE — fix or finish

- **Blockers from review** → FIX, then re-run GATES and REVIEW.
- **Concerns** → fix what you can in this PR; capture the rest in
  `plan.md` as follow-up. Don't let "concerns" rot in chat.
- **Gates green and review clean** → ready to ship. Walk the §11
  Definition-of-done checklist; refuse to declare done until every
  applicable bullet is true. Note especially the multi-loop bullet
  under "Tests + benchmarks" — the final loop of a multi-loop spec
  runs `quality-engineer` against the whole spec, not just the
  last diff.

## FIX phase

Fixing is the same loop, scoped to a single finding:

1. Read the finding carefully. Don't fix the symptom — fix what the
   reviewer actually flagged.
2. Make the smallest change that addresses it.
3. Re-run GATES.
4. Re-run REVIEW only if the fix touched logic the reviewer hadn't
   already approved.

## Termination — when to stop iterating

The loop must terminate (`docs/CONVENTIONS.md` §12.1 — 8-iteration
cap). Stop when **any** of these is true:

1. **Gates green AND review clean** — the normal exit. Ship.
2. **8 consecutive iterations with no green-test progress** — the
   cap from §12.1. Stop and ask the user.
3. **Fingerprint stasis** — the same findings landed two
   iterations in a row (see REVIEW above). Stop and re-plan.
4. **Diff is shrinking but findings aren't** — you're spot-fixing
   without addressing root cause. Judgment call. Back to PLAN.
5. **`tools/check-done.py` exits non-zero** — if you wired the
   state-file machinery. Read the exit message; it tells you which
   gate fired.

If you hit any of these and the work isn't done, the task is bigger
than you thought. Stop, write down what you learned, and re-plan.
Never silently expand scope to make a finding go away.

## Capture what was learned

Before the PR is opened, ask: *what would have made this loop go
faster?* Inkwell already has homes for the answer:

- **A pattern, gotcha, or antipattern worth not repeating** →
  `docs/CONVENTIONS.md` §16 (common review findings ledger).
- **"I had to grep for `<thing>` repeatedly"** → a pointer in
  `docs/ARCH.md` or the relevant `internal/<pkg>/AGENTS.md`.
- **"The test command for this package is unusual"** → that
  package's `AGENTS.md`.
- **"I made the same wrong assumption twice"** → §16 if it's a
  reviewer-finding shape, otherwise the package AGENTS.md.
- **"This workflow is now the third time I've done it"** → propose
  it as a new skill in `.claude/skills/`.

This is the part of the loop that makes the *project* smarter, not
just the current PR. Skipping it means the next agent (or you, next
month) will re-derive the same insight.

## Anti-patterns to refuse

(Most of these are also in `docs/CONVENTIONS.md` §12.4; listed here
for in-skill reach.)

- **Skipping PLAN because "the task is small."** If it's truly
  small, the plan is one sentence — write it anyway.
- **Writing code before deciding how it'll be verified.** Every
  task picks its verifier during PLAN; for TDD tasks, the test
  exists before the production code does.
- **Editing the test until it passes.** Makes the gate green by
  lying. If a test is wrong, fix it in a separate commit with a
  justification.
- **Deferring a test because the code fails it.** The inverse —
  same lie. Fix the code, or split into two commits with the
  reason recorded.
- **Declaring victory because gates pass.** Gates are necessary,
  not sufficient. Review catches what gates can't.
- **Declaring spec-complete from per-task gates.** Per §11 DoD,
  the final loop of a multi-loop spec runs `quality-engineer`
  against the whole spec, not just the last diff.
- **Mock-shape assertions.** Test the observable contract (rendered
  frame, store row), not `stubGraph.Calls == 2`. The visible-delta
  rule is hard.
- **Looping without capturing learnings.** Every loop that ends
  without updating *some* doc, skill, or §16 entry is a loop
  whose lessons are lost.
