---
name: new-spec
description: Use this skill when the user wants to start a new feature with a spec, or wants to write a spec for something they're about to build in inkwell. Triggers on "new spec", "write a spec for X", "let's spec this out", "scaffold spec NN", "start a feature for…". Creates both docs/specs/NN-<title>.md AND docs/plans/spec-NN.md so the two artefacts land together — the v0.12.0 missing-plan-file class of bug can't recur because the plan file is created up front.
---

# Skill: new-spec

Create a new feature spec under `docs/specs/NN-<title>.md` AND the
corresponding tracking note at `docs/plans/spec-NN.md` together. Both
files are mandatory per CLAUDE.md §13 — the plan must exist *with*
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
- **Bug fixes** — open a PR with a regression test; CLAUDE.md §5.7
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

3. **Create the spec from `spec-template.md`** in this skill's
   directory:

   ```bash
   cp .claude/skills/new-spec/spec-template.md docs/specs/NN-<title>.md
   ```

   Then replace `<NN>` and `<TITLE>` placeholders. Leave the body
   sections empty for the user to fill — but keep the headings,
   because §16 finds that skipping them is the #1 source of vague
   specs.

4. **Create the plan from `plan-template.md`** at the same time:

   ```bash
   cp .claude/skills/new-spec/plan-template.md docs/plans/spec-NN.md
   ```

   Replace `<NN>` and `<TITLE>`. The plan begins life with
   `## Status\nnot-started`. Update to `in-progress` as soon as
   work begins; `done` only at ship time.

5. **Don't update PRD.md §10 / ROADMAP.md yet.** Those are
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
   - Each perf budget row needs a benchmark (CLAUDE.md §5.6).
   - Spec 17 impact line: token handling? file I/O? subprocess?
     HTTP? SQL? cryptographic primitive? If yes, update
     `docs/THREAT_MODEL.md` / `docs/PRIVACY.md` *in the same PR*.
   - CLI-mode equivalent? (PRD §5.12 — every TUI feature should
     have a CLI-mode answer, even if it's "n/a").

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

Open the new `docs/specs/NN-<title>.md` and walk the user through
filling it in. The spec is the input to the ralph loop (CLAUDE.md
§12) — once it's coherent, you can hand it to the loop and let it
drive the implementation iteration by iteration.
