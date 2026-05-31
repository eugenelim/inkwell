---
description: Lint AGENTS.md / CLAUDE.md and the projected agent artifacts (.claude/skills, .claude/agents, .claude/commands) against inkwell's conventions
---

Run inkwell's mechanical doc gate, then check the agent artifacts by
inspection. inkwell ships no dedicated `tools/lint-*.py` for these — the
mechanical doc checks live in `make doc-sweep` (`docs/CONVENTIONS.md`
§12.6); the artifact-frontmatter checks below are done by reading the
files. Don't auto-fix anything — report findings and let the user decide.

## 1. Mechanical doc gate

```bash
make doc-sweep
```

`docs/CONVENTIONS.md` §12.6: plan-file existence per spec, shipped-vs-spec
consistency, and the other mechanical checks. If it exits non-zero, report
the failing check verbatim.

## 2. AGENTS.md / CLAUDE.md hygiene (by inspection)

1. Root `AGENTS.md` stays short — flag if it has grown past ~250 lines
   (it delegates long-form material to `docs/CONVENTIONS.md`).
2. `CLAUDE.md` is a symlink to `AGENTS.md` (inkwell's canonical setup),
   or — on a checkout that can't materialise symlinks — a byte-identical
   copy. A diverged regular `CLAUDE.md` is a finding.
3. Internal links in `AGENTS.md` resolve (the `docs/*.md` targets exist).
4. `docs/CHARTER.md`, `docs/PRD.md`, `docs/architecture/overview.md`, and
   `docs/CONVENTIONS.md` exist (the "read these in order" set).
5. Any subdirectory `AGENTS.md` (e.g. `internal/<pkg>/AGENTS.md`) stays
   focused — flag if one has grown past ~150 lines.

## 3. Agent-artifact hygiene (by inspection)

For every file under `.claude/skills/*/SKILL.md`, `.claude/agents/*.md`,
and `.claude/commands/*.md`:

1. Well-formed YAML frontmatter delimited by `---`, with the required
   keys: skills/agents need `name` + `description`; commands need
   `description`.
2. Skill directory names match the frontmatter `name`; subagent
   filenames match their frontmatter `name`; kebab-case throughout.
3. No unknown top-level frontmatter keys (project-specific fields live
   under `metadata:`).
4. Each skill dir contains exactly one `SKILL.md` and no stray `.md`
   siblings at its root (templates belong under `assets/`).
5. Internal markdown links inside each artifact resolve.
6. No `<adapt:…>` markers left unresolved (the adapt-to-project skill
   should have substituted them).

If `make doc-sweep` is unavailable, fall back to inspecting the files
directly and report the same checks manually.
