# inkwell — user documentation

Welcome. This is the docs you read **as a user of the binary** — separate from
the contributor / internal docs in the repo root (`PRD.md`, `ARCH.md`,
`specs/`, `plans/`, `CONFIG.md`, `TESTING.md`).

If you're contributing to inkwell, read the repo `README.md` and `CLAUDE.md`
first.

---

## What's here

| Document                | Audience      | Read when                                                        |
| ----------------------- | ------------- | ---------------------------------------------------------------- |
| [`guide.md`](guide.md)        | New users     | First-time install, sign-in, "how do I do X?" walk-through.       |
| [`cheatsheet.md`](cheatsheet.md) | Every user    | Quick lookup of a keybinding or `:command`.                       |

The cheatsheet is **exhaustive** — every binding, every command, every mode
that's wired in the current release. The guide is **selective** — it covers
the daily flows in narrative form. Together they should be enough for the
2 a.m. "what was that key again?" question and the "okay, what does this
thing actually do?" question.

---

## Versioning

These docs ship with the binary. The version you have should match the version
they describe. If they disagree, the binary wins — open an issue.

Cheatsheet last reviewed: see the bottom of [`cheatsheet.md`](cheatsheet.md).
Guide last reviewed: see the bottom of [`guide.md`](guide.md).

---

## Reporting issues

`https://github.com/eugenelim/inkwell/issues`. Include:

- inkwell version (`inkwell --version` or check the release tag)
- macOS version (`sw_vers`)
- A minimal reproduction (the keystrokes you typed, what you expected,
  what happened instead)
- A redacted snippet from `~/Library/Logs/inkwell/inkwell.log` if the issue
  is a sync / auth / Graph error
