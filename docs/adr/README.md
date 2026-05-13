# Architecture Decision Records

This directory holds ADRs — short, immutable records of cross-cutting
design decisions. An ADR captures the *why* behind a choice when the
*what* alone (visible in ARCH.md, PRD.md, or a spec) doesn't preserve
enough context for a future contributor (or future-you) to second-guess
intelligently.

## What belongs here

Use an ADR when a decision:

- Spans more than one spec (e.g. "pure-Go stack" affects every package).
- Has plausible alternatives that someone might re-litigate later.
- Sets a constraint that's hard to reverse (API surface, licence
  posture, language runtime).
- Was made for a reason that lives outside the code (org policy,
  distribution constraint, learnt-from-a-past-bug).

Don't open an ADR for:

- Feature-level work — that's a spec under `docs/specs/`.
- Routine refactors or code-style choices.
- Anything already captured precisely by ARCH.md or PRD.md.

## Format

Markdown ADR (MADR-lite), one file per decision. Filename:
`NNNN-kebab-case-title.md`. Numbering is monotonic — once an ADR is
written, the number is permanent even if the ADR is later deprecated
or superseded.

Use `_template.md` as the skeleton. Keep an ADR to roughly one screen.
If you need more than that, the decision probably needs a spec instead.

## Status lifecycle

`Proposed → Accepted → (Deprecated | Superseded by ADR-NNNN)`

Once `Accepted`, the body is immutable. To change a decision, write a
new ADR that supersedes the old one — never edit the old body.

## Cross-references

ADRs link out to ARCH.md, PRD.md, and specs as needed. The reverse
isn't required — those docs can ignore ADRs and stay readable.
Searching the repo for `ADR-NNNN` finds anywhere the decision is
relied on.

## Index

| #    | Title                                                       | Status   |
| ---- | ----------------------------------------------------------- | -------- |
| 0001 | [Record decisions as ADRs](0001-record-decisions-as-adrs.md) | Accepted |
| 0002 | [Pure-Go stack, no CGO](0002-pure-go-stack-no-cgo.md)        | Accepted |
| 0003 | [Microsoft Graph v1.0 only](0003-microsoft-graph-v1-only.md) | Accepted |
| 0004 | [No Microsoft Graph SDK — `net/http` directly](0004-no-graph-sdk-direct-http.md) | Accepted |
| 0005 | [MSAL public client via Microsoft Graph CLI Tools](0005-msal-public-client-graph-cli-tools.md) | Accepted |
| 0006 | [Bubble Tea sub-models stored by value](0006-bubble-tea-sub-models-by-value.md) | Accepted |
