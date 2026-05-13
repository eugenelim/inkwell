# Spec <NN> — <TITLE>

## Status
not-started

<!--
Status lifecycle:
  not-started → in-progress → (blocked | done)

Set "done — **Shipped vX.Y.Z** (YYYY-MM-DD)" when the spec ships
(CLAUDE.md §12.6). The shipped-consistency check in
scripts/doc-sweep.sh enforces that this line starts with `done`
once the spec carries a `**Shipped:**` marker.
-->

## DoD checklist

Mirrors `docs/specs/<NN>-*.md` §9. Tick boxes as they land.

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

The ralph-loop journal (CLAUDE.md §12.2). One entry per iteration.

### Iter 1 — YYYY-MM-DD
- **Slice:** <one-line description of what this iteration did>
- **Verifier:** <test name, bench name, or "docs review" that flips red→green>
- **Commands run:** `<the relevant subset of CLAUDE.md §5.6>`
- **Result:** <pass/fail, key counts>
- **Critique:** <self-critique from §12.2 step 5>
- **Next:** <the slice for iter 2, or "exit — DoD complete">

<!--
Add subsequent iterations here. Keep them tight; the log is
read by the next iteration's planning, so verbosity hurts.
-->
