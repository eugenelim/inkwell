# inkwell — user documentation

User-facing docs, separate from the contributor docs in `docs/PRD.md`,
`docs/ARCH.md`, `docs/specs/`, `docs/plans/`, etc.

Four sections, each answering a different kind of question:

| File                               | Question it answers           | Read when                                |
| ---------------------------------- | ----------------------------- | ---------------------------------------- |
| [`tutorial.md`](tutorial.md)       | "How do I get started?"       | First time. Sequential walkthrough.      |
| [`how-to.md`](how-to.md)           | "How do I do X?"              | You have a specific task in mind.        |
| [`reference.md`](reference.md)     | "What does this key do?"      | Quick lookup. Exhaustive tables.         |
| [`explanation.md`](explanation.md) | "Why does it work like this?" | Curious about design, privacy, scope.    |

If you're contributing to inkwell, read the repo `README.md` and
`CLAUDE.md` first.

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
