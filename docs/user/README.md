# inkwell — user documentation

User-facing docs, separate from the contributor docs in `docs/PRD.md`,
`docs/architecture/overview.md`, `docs/specs/` (per-feature `spec.md` + `plan.md`), etc.

Four kinds of docs, each answering a different question (long-form
how-tos additionally get one page each under `howto/`):

| File                               | Question it answers           | Read when                                |
| ---------------------------------- | ----------------------------- | ---------------------------------------- |
| [`tutorial.md`](tutorial.md)       | "How do I get started?"       | First time. Sequential walkthrough.      |
| [`how-to.md`](how-to.md)           | "How do I do X?"              | You have a specific task in mind.        |
| [`howto/`](howto/)                 | "How do I do X?" (long form)  | Bigger tasks get one page each — first up: [`agent-skills.md`](howto/agent-skills.md). |
| [`reference.md`](reference.md)     | "What does this key do?"      | Quick lookup. Exhaustive tables.         |
| [`explanation.md`](explanation.md) | "Why does it work like this?" | Curious about design, privacy, scope.    |

If you're contributing to inkwell, read the repo `README.md`,
`AGENTS.md` (agent/contributor entry point), and
`docs/CONVENTIONS.md` (long-form rules) first.

---

## Versioning

Each doc carries a `_Last reviewed against vX.Y.Z_` line at the bottom.
The version you have should match the version they describe. If they
disagree, the binary wins — open an issue.

---

## Reporting issues

`https://github.com/eugenelim/inkwell/issues`. Include:

- inkwell version (release tag you downloaded).
- macOS version (`sw_vers`).
- A minimal reproduction (the keystrokes you typed, what you expected,
  what happened instead).
- A redacted snippet from `~/Library/Logs/inkwell/inkwell.log` if the
  issue is a sync / auth / Graph error.
