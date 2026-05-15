---
name: bug-fix
description: Use this skill when the user wants to fix a bug — a deviation between current behaviour and intended behaviour in code that already exists. Triggers on "fix bug", "fix this bug", "diagnose and fix", "investigate this regression", "this is broken", "post-mortem this", "why is X failing". Do NOT use for new features (use `new-spec`) or refactors that don't fix incorrect behaviour. Inkwell-specific: the regression test BEFORE the fix is mandatory and lands in the same commit (`docs/CONVENTIONS.md §5.7`).
---

# Skill: bug-fix

Fix a defect in the smallest, most root-causing way. The discipline is
universal: reproduce before fixing, write the failing test first,
identify root vs symptom, minimum diff, commit body documents why.

The inkwell-specific tightening: `docs/CONVENTIONS.md §5.7` ("the
institutional memory for the bugs that have already shipped") and
`§16` (the common-review-findings ledger) make the regression test
**mandatory and same-commit**. v0.2.6 → v0.2.7, v0.2.8 → v0.2.9,
v0.3.0 → v0.3.1 each happened because the test that would have caught
the next regression hadn't been written. Every fix adds one.

## When to invoke

The user is pointing at a defect — observed behaviour differs from
intended behaviour in code that already exists. Even a one-line fix
benefits from walking this discipline; it forces the question "is
this fixing the cause or hiding it?"

For multi-file changes that go beyond fixing one defect — refactors,
new features triggered by discovering the bug — stop and use
`new-spec` instead. This skill is for bug fixes, not opportunistic
restructuring.

## Procedure

1. **Reproduce first.** Don't write a fix until you have one of: a
   failing test, documented manual reproduction steps that fail
   reliably, or a captured error / stack trace / log signature. No
   reproduction = no fix; you might be fixing the wrong thing.

   For TUI bugs: the visible-delta rule (`docs/CONVENTIONS.md §5`)
   applies. "User sees stale row" needs a `teatest` reproduction that
   captures the frame, not a unit assertion on internal state. The
   v0.2.6 → v0.2.7 cycle proves this: dispatch tests were green; the
   user-visible glyph was wrong.

2. **Write the failing test (red).** It should pin the *observable
   contract being violated*, not the current implementation. Push
   back on these failure modes:
   - **Mock-shape assertion.** Asserting `stubGraph.Calls == 2` when
     the observable contract is the resulting `store.Message` row or
     the rendered viewer string. Test the contract, not the
     implementation. (See `quality-engineer`'s "mock-shape
     assertions" check.)
   - **Test passes for the wrong reason.** Run the test against the
     unfixed code; confirm it fails *because of* the bug, not
     because the setup is wrong. `require.Equal` on the wrong field
     can pass for the wrong reason — re-read the assertion.
   - **Test asserts in the wrong layer.** A bug surfaced in the TUI
     viewer might actually live in `internal/render`; pin the test
     at the layer where the invariant lives. Avoid e2e tests for
     unit-shaped invariants.

3. **Identify root cause before writing the fix.** Write down a
   one-line answer to each:
   - **Where is the defect actually?** In the called function, the
     caller, their shared assumption, or upstream of both? A nil
     `*store.Body` that crashes the renderer may originate in
     `sync.Engine` that should never have called render with a nil.
   - **When did it start?** `git log -p -- <path>` and `git blame`
     on the affected code. For regression-shaped bugs the commit
     that broke it often tells you why. For bugs that go back
     before recorded history, the surrounding `spec-NN.md` and any
     `docs/adr/` files surface the original intent.
   - **Could the same class of bug exist elsewhere?** Grep for
     similar patterns — same function called from other sites, same
     assumption made elsewhere. Common inkwell-shaped repeats:
     ETag-handling between sync paths, context.Background() in
     request-path goroutines, missing redaction on a new log site,
     missing FK cascade on a new table. If yes, decide whether the
     fix's scope widens or whether you file follow-ups in the
     same-PR "noted for follow-up" list.

4. **Minimum fix.** Write the smallest change that turns the failing
   test green. Refuse to fix adjacent issues in the same PR; note
   them for follow-up. Scope discipline is enforced by
   `adversarial-reviewer` — out-of-scope changes are a Blocker until
   justified or extracted.

5. **Verify root vs symptom.** Look at the diff and ask: does this
   address what step 3 identified, or does it mask the symptom?
   Inkwell-flavoured symptom-only anti-patterns:
   - **Silently dropping nil from the sync→render handoff** instead
     of fixing the engine that should never have called with nil.
   - **Defensive `if err != nil { return }`** at every call site of a
     function whose contract should not return that error.
   - **Retries around flaky Graph 429s** when the right fix is to
     respect `Retry-After` in `internal/graph/batch_retry.go` once.
   - **Disabling a feature flag (`[body_index].enabled = false`)
     instead of fixing the bug** — config knobs are for opt-in scope,
     not for hiding broken code.
   - **Suppressing the redaction test** instead of removing the
     log site that emits PII (§7 invariant 3).

   If the failing test from step 2 still passes under a symptom-only
   fix, you wrote the wrong test — go back to step 2 and sharpen it.

6. **Regression test stays.** The failing test from step 2 lands in
   the **same commit** as the fix (CONVENTIONS §5.7 is explicit). It
   joins the relevant package's test file as the institutional memory
   for the next time someone repeats this mistake. If the bug
   surfaced a deeper invariant, add a bullet to CONVENTIONS §16 so
   future PRs grep this surface before re-introducing the pattern.

7. **Run the right subset of gates.** The full `make regress` is the
   pre-tag gate; for a bug-fix PR the relevant subset is usually:
   ```sh
   gofmt -s -d <changed files>
   go vet ./...
   go test -race ./internal/<pkg>/...
   go test -tags=integration ./internal/<pkg>/...   # if the bug crossed integration boundaries
   go test -tags=e2e ./internal/ui/...              # if TUI behaviour changed
   ```
   `make regress` before pushing if the fix touches more than one
   package.

8. **Commit body documents the root cause.** Conventional commit
   subject — `fix(spec-NN): <subject>` or `fix(<pkg>): <subject>`
   (CONVENTIONS §10). The body explains:
   - What was wrong (the observable bug).
   - Why it was wrong (the root cause from step 3).
   - Why the fix takes the shape it does (and why not the obvious
     alternatives).

   The diff shows *what*; the commit body shows *why*. Future readers
   care more about the latter. No `Co-Authored-By: Claude` trailer
   (CONVENTIONS §10).

9. **Update the plan file** if this fix relates to a shipped spec.
   `docs/plans/spec-NN.md` is the journal; add an iteration entry
   noting "Bug X reported on YYYY-MM-DD; regression test
   `TestFooHandlesBar` added in commit `<sha>`."

## Anti-patterns to refuse

- **Fixing forward without a reproduction.** The obvious fix is
  wrong about a third of the time, and you can't tell which third
  until the test fails red first.
- **Fixing the bug plus adjacent cleanup in one PR.** Each cleanup
  is its own PR with its own justification. Bug-fix PRs are for
  fixing bugs.
- **Adjusting the spec or the test to match the buggy behaviour.**
  If the spec and the fix disagree, one of them is wrong — surface
  that explicitly before continuing, don't paper over it.
- **Giving up before exhausting reproduction strategies.** Different
  OS, different tenant, different account, fresh DB, RSS over time,
  network conditions. Document what was tried, on what version,
  with what data, before asking the user for more. "Couldn't
  reproduce on my machine" is a hypothesis worth testing, not a
  closing condition.
- **Cherry-picking the fix to `main` without rebasing.** The feature
  branch may be ahead of `main` in unrelated commits (CONVENTIONS
  §16 "Process" bullet). Verify the worktree state first.
- **Tagging without checking CI.** Local `make regress` green is
  necessary, not sufficient. After every push, `gh run list --limit
  5` (CONVENTIONS §10).
