# ADR 0001: Record cross-cutting decisions as ADRs

- **Status:** Accepted (2026-05-13)
- **Deciders:** eugenelim
- **Supersedes:** —
- **Related:** CLAUDE.md, docs/PRD.md, docs/ARCH.md

## Context

Inkwell's living docs (`PRD.md`, `ARCH.md`, `CONFIG.md`) describe what
the system *is*, and the per-feature specs in `docs/specs/` describe
what each capability *does*. Neither layer is well-suited to recording
why a particular cross-cutting choice was made when there were
plausible alternatives.

Past examples that drifted into prose footnotes or section
parentheticals — and were hard to find later — include the pure-Go
stack constraint, the Graph v1.0-only API surface, the decision not to
take a dependency on the Microsoft Graph SDK, and the use of MSAL's
"Microsoft Graph CLI Tools" public client_id. A future contributor (or
future-me) hitting one of these and reading the current code can see
*what* is true, but not *why* it's true, and is likely to relitigate.

`docs/CONVENTIONS.md` §12.0 already requires that every concrete claim in a spec
be backed by a tool call — but it doesn't give cross-cutting decisions
a home of their own.

## Decision

Add `docs/adr/` as a new top-level documentation layer. Each ADR is a
single Markdown file in MADR-lite format (Status, Context, Decision,
Consequences, Alternatives, References). ADRs are numbered
monotonically and named `NNNN-kebab-case-title.md`. Once `Accepted`,
the body is immutable; to change a decision, supersede it with a new
ADR.

`docs/adr/README.md` is the index. `docs/adr/_template.md` is the
skeleton. CLAUDE.md and ARCH.md may link to ADRs but are not required
to mirror them.

## Consequences

### Positive
- Cross-cutting decisions have a discoverable, search-by-number home.
- Living docs (PRD, ARCH) can shrink as historical justifications move
  to ADRs and the living docs describe only current state.
- Reviewers can challenge a *decision* by pointing at the ADR's
  "Alternatives" list instead of reverse-engineering it from prose.
- Bootstraps a discipline of writing the reasoning down when the
  decision is fresh, not months later when the context is lost.

### Negative
- One more place to update when a decision changes (mitigated:
  superseding ADRs is cheap).
- Risk of ADR-creep — opening an ADR for every minor choice. The
  README's "what belongs here" list is the guardrail.

### Neutral
- New contributors have a new directory to learn. Cost is small;
  README explains it.

## Alternatives considered

**Keep decisions in `PRD.md` / `ARCH.md`.** This is the current state.
Rejected because living docs accumulate "why" prose over time and
become hard to read for the "what is true now" use case; the two
audiences are different.

**Capture decisions in spec headers.** Specs are per-feature; cross-
cutting decisions span specs. Putting them in one spec's header makes
them invisible to anyone reading a different spec.

**Use GitHub issues / discussions.** Doesn't travel with the repo;
external dependency; not greppable from a clone.

**Adopt full MADR (with deciders, dates per status transition, etc.).**
The full ceremony is overkill for a solo-maintained repo. MADR-lite
captures the essential fields without the bureaucracy.

## References

- `docs/CONVENTIONS.md` §11 (Definition of done) — ADR work doesn't change DoD;
  ADRs are written *with* the change that introduces the decision.
- docs/PRD.md, docs/ARCH.md — the living docs that ADRs complement.
- [MADR](https://adr.github.io/madr/) — the format this draws from.
