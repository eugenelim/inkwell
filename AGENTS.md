# AGENTS.md — Inkwell

> Canonical agent / contributor entry point. `CLAUDE.md` is a symlink to
> this file; Cursor, Codex, Copilot, and Gemini CLI also read it.
>
> Keep this file short. The long-form repo conventions — stack invariants,
> layering, test architecture, performance budgets, privacy invariants, the
> ralph loop, definition-of-done — live in
> [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md) with stable `§N` anchors.
> Code, ADRs, and specs cite those by number; don't restate them here.

## What this repo is

inkwell — a terminal-based, local-first email + calendar client for macOS
that talks to Microsoft 365 via Microsoft Graph. Read-and-triage focused;
not a full Outlook replacement (composition stays in native Outlook).

- **What we're building:** [`docs/PRD.md`](docs/PRD.md).
- **How the code is organized:** [`docs/ARCH.md`](docs/ARCH.md).
- **How we work (DoD, ralph loop, style, review-findings ledger):**
  [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md).

## Read these in this order before touching code

1. [`docs/PRD.md`](docs/PRD.md) — what we're building, granted vs denied Graph scopes.
2. [`docs/ARCH.md`](docs/ARCH.md) — module layout, layering, data flow, invariants.
3. [`docs/CONFIG.md`](docs/CONFIG.md) — config keys (skim, reference on demand).
4. [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md) — repo conventions; §-numbered surfaces are stable contracts.
5. [`docs/specs/NN-*.md`](docs/specs/) — the spec you're implementing.
6. [`internal/<pkg>/AGENTS.md`](internal/) — package-specific invariants (`store`, `graph`, `ui`, `auth`).
7. [`docs/adr/`](docs/adr/) — cross-cutting decisions, when you're about to relitigate one.

If a spec contradicts ARCH/PRD, the spec is wrong. Fix the spec first.

## Source of truth

| Question | Where it lives |
| --- | --- |
| What is this project, in/out of scope? | `docs/PRD.md` |
| Why did we choose X over Y? | `docs/adr/` |
| How is the code organized today? | `docs/ARCH.md` |
| What does this spec/feature do? | `docs/specs/NN-*.md` |
| How will it be built, step by step? | `docs/plans/spec-NN.md` |
| How do we work (DoD, ralph loop, style)? | `docs/CONVENTIONS.md` |
| Config keys + defaults? | `docs/CONFIG.md` |
| Threat model? | `docs/THREAT_MODEL.md` |
| Privacy data flows? | `docs/PRIVACY.md` |
| Testing layers + commands? | `docs/TESTING.md` |
| User-facing docs (Diátaxis)? | `docs/user/` |
| Agent skills (repeating workflows)? | `.claude/skills/<name>/SKILL.md` |
| Specialist reviewer subagents? | `.claude/agents/*.md` |

If you can't find the answer in one of these places, **the answer doesn't
exist yet** — ask. Don't guess.

## Workflow

For anything beyond a one-line edit, follow the **plan → execute → verify
→ review → iterate** loop. Mechanics live in
[`docs/CONVENTIONS.md §12`](docs/CONVENTIONS.md) (the ralph loop). One-line
summary:

1. **Plan.** Re-read the spec. Name the next slice.
2. **Execute.** Smallest coherent unit; tests in the same change, not after.
3. **Gates.** `make regress` (or the relevant subset — see
   [`docs/CONVENTIONS.md §5.6` / `§5.7`](docs/CONVENTIONS.md)).
4. **Review.** Run the `adversarial-reviewer` subagent until it returns
   `Clean — ready to commit.`
5. **Iterate** on findings; hard cap five rounds — re-plan past that.
6. **Capture learnings** in the right AGENTS.md, `docs/CONVENTIONS.md`,
   skill, or doc.
7. **Conventional commit.** `<type>(spec-NN): <subject>`. No
   `Co-Authored-By` trailers (see
   [`docs/CONVENTIONS.md §10`](docs/CONVENTIONS.md)).

## Quick reference — common commands

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

# Full regression suite (mandatory before every tag)
make regress

# Build
go build ./...
go build -o bin/inkwell ./cmd/inkwell

# Module hygiene (separate commit)
go mod tidy
go mod verify
```

A spec is done when these pass and the spec's DoD is fully ticked. Not
before. (See [`docs/CONVENTIONS.md §11`](docs/CONVENTIONS.md) for the DoD
checklist.)

## Specialist subagents

- [`adversarial-reviewer`](.claude/agents/adversarial-reviewer.md) — spec /
  plan / implementation drift; missing edge cases; scope creep. Default
  reviewer; runs after gates pass. Re-run iteratively until it returns
  `Clean — ready to commit.`

## Skills available to you

`.claude/skills/` contains workflows that have been used enough to deserve
a name:

- `new-spec` — scaffold `docs/specs/NN-<title>.md` +
  `docs/plans/spec-NN.md` together (the v0.12.0 missing-plan-file
  regression made this rule).

## Things you should not do without asking

- **Don't run destructive commands** (`rm -rf`, `git push --force`,
  dropping DB tables) without explicit confirmation in the same turn.
- **Don't skip hooks** (`--no-verify`, `--no-gpg-sign`) unless the user
  has explicitly asked for it.
- **Don't add `Co-Authored-By` trailers** to commits.
- **Don't request scopes outside PRD §3.1.** `Mail.Send` and the rest
  of the denied list are out of scope by construction; CI lint enforces.
- **Don't fabricate APIs.** If you're unsure a function exists, grep
  first. See [`docs/CONVENTIONS.md §12.0`](docs/CONVENTIONS.md) for the
  spec-verification discipline.
- **Don't create new top-level directories.** The structure is
  intentional; check `docs/ARCH.md §2` and
  [`docs/CONVENTIONS.md §14`](docs/CONVENTIONS.md) first.

## When this file (or CONVENTIONS.md) is wrong

Flag drift in your PR — don't silently work around it. AGENTS.md /
CONVENTIONS.md vs reality drift is the single biggest cause of agent
quality decay. Trivial fixes (typos, broken links) are normal PRs;
substantive changes carry a one-line "convention change:" note in the
PR body.

---

*Detailed conventions — including the ralph loop, definition-of-done
checklist, performance budgets, privacy invariants, and the common-review-
findings ledger — live in [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md).
The §-numbered surfaces there are stable contracts; code comments and
specs cite them by number.*
