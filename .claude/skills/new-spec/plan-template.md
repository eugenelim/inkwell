# Spec <NN> — <TITLE>

## Status
not-started

<!--
Status lifecycle:
  not-started → in-progress → (blocked | done)

Set "done — **Shipped vX.Y.Z** (YYYY-MM-DD)" when the spec ships
(`docs/CONVENTIONS.md` §12.6). The shipped-consistency check in
scripts/doc-sweep.sh enforces that this line starts with `done`
once the spec carries a `**Shipped:**` marker.
-->

## DoD checklist

Mirrors `docs/specs/NN-<title>/spec.md` §9. Tick boxes as they land.

- [ ] ...
- [ ] ...
- [ ] ...

## Perf budgets

One row per perf-budget line in the spec. Measured numbers go in as
each iteration produces them.

| Surface | Budget | Measured | Bench | Status |
| --- | --- | --- | --- | --- |
| ... | ... | — | — | pending |

## Iteration log

The ralph-loop journal (`docs/CONVENTIONS.md` §12.2). One entry per iteration.

### Iter 1 — YYYY-MM-DD
- **Slice:** <one-line description of what this iteration did>
- **Verifier:** <test name, bench name, or "docs review" that flips red→green>
- **Commands run:** `<the relevant subset of `docs/CONVENTIONS.md` §5.6>`
- **Result:** <pass/fail, key counts>
- **Critique:** <self-critique from §12.2 step 5>
- **Next:** <the slice for iter 2, or "exit — DoD complete">

<!--
Multi-task iteration (supervisor mode — only when an iteration has
two or more independent slices that can land in parallel). Label
slices T1, T2, … and add a `- Depends on:` line per slice. Two or
more `Depends on: none` siblings is the structural trigger for
supervisor mode (`docs/CONVENTIONS.md` §12.7). Most iterations are
single-slice and use the shape above; reach for this shape only
when fan-out actually pays.

### Iter N — YYYY-MM-DD  (supervisor-mode example)
- **T1:** <slice one-liner>
  - Depends on: none
  - Verifier: <test name>
- **T2:** <slice one-liner>
  - Depends on: none
  - Verifier: <bench name>
- **Commands run (post-merge):** ...
- **Result:** ...
- **Critique:** ...
- **Next:** ...

Add subsequent iterations here. Keep them tight; the log is
read by the next iteration's planning, so verbosity hurts.
-->
