---
name: new-spec
description: Use this skill when the user wants to start a new feature with a spec, or wants to write a spec for something they're about to build in inkwell. Triggers on "new spec", "write a spec for X", "let's spec this out", "scaffold spec NN", "start a feature for…". Creates both docs/specs/NN-<title>/spec.md AND docs/specs/NN-<title>/plan.md so the two artefacts land together — the v0.12.0 missing-plan-file class of bug can't recur because the plan file is created up front.
---

# Skill: new-spec

Create a new feature spec under `docs/specs/NN-<title>/spec.md` AND the
corresponding tracking note at `docs/specs/NN-<title>/plan.md` together. Both
files are mandatory per `docs/CONVENTIONS.md` §13 — the plan must exist *with*
the spec, not added later.

## When to invoke

The user is about to build a non-trivial feature. The spec is the
contract (Definition of done, perf budgets, Graph scopes, behaviour
on offline, undo behaviour); the plan is the working journal (DoD
checklist with tickable boxes, perf-budget measurements, iteration
log). They land together.

Don't use for:

- **Cross-cutting design decisions** with alternatives — use ADRs:
  add a file under `docs/adr/` based on `docs/adr/_template.md`.
- **Documentation-only updates** — those don't need a spec.
- **Bug fixes** — open a PR with a regression test; `docs/CONVENTIONS.md` §5.7
  says "write the regression test BEFORE the fix lands in the same
  commit."

## Procedure

1. **Pick a kebab-case feature title** from the user's description.
   Short and noun-y: `folder-management`, `mute-thread`,
   `command-palette`. Not `improve-the-folder-experience`.

2. **Compute the next spec number** by inspecting the highest NN in
   `docs/specs/`:

   ```bash
   ls docs/specs/ | grep -oE '^[0-9]+' | sort -n | tail -1
   ```

   The new number is that + 1, zero-padded to two digits (`02`,
   `34`, etc.). If the result would be three digits (`100+`), keep
   three.

3. **Create the spec directory + spec from the bundled `assets/spec.md`
   template** in this skill's directory:

   ```bash
   mkdir -p docs/specs/NN-<title>
   cp .claude/skills/new-spec/assets/spec.md docs/specs/NN-<title>/spec.md
   ```

   Then fill the `<feature name>` title and `<github-handle>` owner;
   leave `Status: Draft`. Leave the body sections empty for the user
   to fill — but keep the headings, because §16 finds that skipping
   them is the #1 source of vague specs. The `## Inkwell specifics`
   block carries the inkwell-required fields (Non-goals, perf budgets,
   Graph scopes, Spec-17 impact, CLI-mode) — fill or mark "n/a", don't
   delete.

4. **Create the plan from the bundled `assets/plan.md` template** in
   the same directory:

   ```bash
   cp .claude/skills/new-spec/assets/plan.md docs/specs/NN-<title>/plan.md
   ```

   Fill the `<feature name>` title. The plan begins life with
   `Status: Drafting`. Update to `Executing` as soon as work begins;
   `Done` only at ship time.

   Fill the plan's task breakdown with the same rigour as the spec.
   Push back on these plan-stage failure modes:

   - **Task too big.** "Implement the feature" is not a task; "add the
     validation function for X" is. Each task should fit a single PR
     and a single context window. Split coarse tasks until they do.
   - **`Depends on:` omitted.** Every task must state `Depends on:`
     explicitly — prior task IDs or `none`. Don't let authors lean on
     task order to imply dependency; that hides serial-by-default
     thinking and blocks supervisor-mode parallel dispatch
     (`docs/CONVENTIONS.md` §12.7).
   - **Verification mode unstated.** Every task declares its gate — a
     unit/integration/e2e test, a benchmark, or visual QA via
     `make ai-fuzz`. Silent defaults produce untested invariants.
   - **Tasks without spec mapping.** Each task references which
     behaviour from the spec's Definition-of-done it implements.
     Orphan tasks are scope creep in disguise; behaviours with no
     implementing task are gaps.
   - **Specificity miss.** Reference exact file paths and function or
     symbol names where known. "Update the parser" is too coarse to
     verify; "add a null-check in `internal/store/sync.go:applyDelta`"
     is the right level.

5. **Don't update PRD.md §10 / docs/product/roadmap.md yet.** Those are
   ship-time edits — they mark the spec as inventory once it lands.
   At spec-creation time, the spec itself is the only authoritative
   surface.

6. **Verify `make doc-sweep` passes.** The plan-file existence check
   will fail if step 4 was skipped — that's the foot-gun this skill
   eliminates.

7. **Remind the user of the spec contract:**

   - Every behaviour bullet must be testable. "Should be fast" is
     not a behaviour; "Returns within 200ms at p99 for payloads
     under 1KB" is.
   - Non-goals are mandatory — at least two. Specs without
     explicit non-goals get scope-crept.
   - Each perf budget row needs a benchmark (`docs/CONVENTIONS.md` §5.6).
   - Spec 17 impact line: token handling? file I/O? subprocess?
     HTTP? SQL? cryptographic primitive? If yes, update
     `docs/THREAT_MODEL.md` / `docs/PRIVACY.md` *in the same PR*.
   - CLI-mode equivalent? (PRD §5.12 — every TUI feature should
     have a CLI-mode answer, even if it's "n/a").
   - **TUI surface?** Closing the spec requires `make ai-fuzz` and
     a Claude-Code-oracle pass over the resulting
     `.context/ai-fuzz/run-*/REVIEW.md`. Bake this into the plan's
     Definition-of-done checklist now so it doesn't get skipped at
     ship time (`docs/CONVENTIONS.md` §11).

8. **Spec-mode adversarial review.** Once the spec and plan read
   coherently, dispatch the `adversarial-reviewer` subagent in spec
   mode over the freshly drafted `spec.md` + `plan.md`. Iterate on
   findings until it returns `Clean — ready to commit.` Spec-mode
   reviews should converge in 1–2 passes; if you can't reach clean in
   3, the spec has a structural problem — surface it to a human rather
   than grinding.

## Anti-patterns to refuse

- **Drafting a spec for something already half-built** without
  reading the existing code first. Either align the spec with
  current behaviour (and note the divergences as "Existing
  surfaces touched") or write a new spec for what *should* change.
- **Writing a spec that reads like a design doc** (full of "the
  function `foo` calls `bar`…"). Specs are contracts; design
  belongs in the plan or in source comments.
- **Skipping non-goals** to "keep it short." Non-goals are exactly
  the section that prevents scope creep months later.
- **Naming the spec after the implementation approach** rather than
  the user-visible capability. "rate-limited-fetcher" is a design;
  "watch-mode" is a capability. Use the latter.

## After the skill runs

Open the new `docs/specs/NN-<title>/spec.md` and walk the user through
filling it in. The spec is the input to the ralph loop (`docs/CONVENTIONS.md`
§12) — once it's coherent, you can hand it to the loop and let it
drive the implementation iteration by iteration.
