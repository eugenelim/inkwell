# ADR 0006: Bubble Tea sub-models stored and passed by value

- **Status:** Accepted (2026-05-13)
- **Deciders:** eugenelim
- **Supersedes:** —
- **Related:** ARCH §10, `docs/CONVENTIONS.md` §4

## Context

Bubble Tea's update loop is pure functional in shape:
`(Model, Msg) → (Model, Cmd)`. Each cycle returns a *new* model;
the framework swaps it in. Sub-models (a folder pane, a viewer pane,
a confirm modal) compose into the root model.

The framework itself doesn't dictate whether sub-models are stored
by value or by pointer. Either compiles, either passes the type
checker, and the framework will dispatch updates either way.

The Charm community has accumulated guidance from real apps that
both choices have correctness implications:

- **Pointers** allow accidental aliasing: a `tea.Cmd` started in
  iteration N can capture a pointer to a sub-model whose contents
  change in iteration N+1 before the Cmd's emitted `tea.Msg` lands.
  Subtle bugs result — usually visible as a UI element that
  "remembers" stale data after a refresh.
- **Values** force the assignment-discipline: `m.sub, cmd =
  m.sub.Update(msg)` rebinds the field. Cmds that closed over the
  old `m.sub` see a snapshot, not a live reference. The cost is one
  struct copy per cycle (negligible for inkwell's sub-models, which
  are all <1KB).

ARCH §10 calls out "Sub-models are value types, not pointers" as a
convention. This ADR records the *why*.

## Decision

All Bubble Tea sub-models in `internal/ui` are stored on `Model` as
value types, not pointers. Sub-model `Update` methods take a value
receiver, return a fresh value. The root `Update` rebinds:

```go
var cmd tea.Cmd
m.list, cmd = m.list.Update(msg)
```

Cmds capture identifiers (folder IDs, message IDs) by value — never
references to sub-models. Sub-models hold no pointers to their
siblings. Communication between sub-models is via typed `tea.Msg`,
not direct method calls.

A test in `internal/ui/dispatch_test.go` asserts that the root
`Update` returns a model that's a distinct value from its input
(i.e. the framework's contract is upheld even when nothing visible
changed).

## Consequences

### Positive
- Eliminates the class of bugs where a long-lived `tea.Cmd` reads
  stale sub-model state on completion.
- The Update function reads as a pure transformation, matching
  Bubble Tea's documented mental model.
- No need to reason about which goroutine owns which sub-model — by
  value, there's no shared mutable state to fight over.
- Concurrency-safety review for spec 17 (security) is simpler:
  Cmds operate on snapshots.

### Negative
- One struct copy per sub-model per Update cycle. Negligible at
  inkwell's scale; benchmarked at <100ns per cycle for the entire
  root Model on a 2024 M2.
- Some Bubble Tea community examples use pointer sub-models; a
  contributor copy-pasting may produce non-idiomatic code. Mitigated
  by the convention being called out in `docs/CONVENTIONS.md` §4 and ARCH §10
  and tested.

### Neutral
- Slightly more verbose Update signature (`m.sub, cmd = …` vs
  `m.sub.Update(…)`). Once internalised, indistinguishable.

## Alternatives considered

**Pointer sub-models.** Rejected — the aliasing risk above is real
and has been seen in other Bubble Tea apps. The performance edge
(no copy) is not material.

**Single flat Model with no sub-model composition.** Considered for
the smallest UIs. Rejected because inkwell's UI has six distinct
panes/modes and a flat Model becomes a ~80-field struct that's
impossible to test in isolation.

**Interface-typed sub-model field with implementations swapped per
mode.** Considered. Rejected — type-erasing the field defeats Go's
exhaustive-switch checks and complicates testing.

## References

- ARCH.md §10 (UI architecture, Bubble Tea).
- `docs/CONVENTIONS.md` §4 (Bubble Tea conventions).
- [Charm Bubble Tea — Composing models](https://github.com/charmbracelet/bubbletea) — framework conventions.
