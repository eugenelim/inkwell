# QA checklist

Manual smoke tests run against the live tenant before each
release. CI exercises every code path against fakes and recorded
fixtures (`docs/CONVENTIONS.md` §5); this checklist is the human-in-the-loop
guard for surfaces that require a real Microsoft Graph response.

> Run from a signed-in shell (`inkwell signin` complete) on the
> release artifact (not a development build). Each row is a
> single command; pass = the bullet's expected behaviour holds.

## Release smoke

| Surface | Command | Expected |
| ------- | ------- | -------- |
| Sign-in & cache open | `inkwell folders` | Returns ≥1 folder; no auth prompt. |
| One-shot listing | `inkwell messages --folder Inbox --limit 10` | Returns recent envelopes. |
| Pattern filter | `inkwell filter '~U' --limit 10` | Returns unread envelopes if any. |
| Watch mode (spec 29) | `inkwell messages --filter '~U' --watch --for 60s` | Streams ≥1 expected match (if a new unread arrives during the window) and exits 0 with the summary line on stderr. |
| TUI launch | `inkwell` | Three-pane TUI loads; folders sidebar populates within 5s; no crash on first keystrokes. |
| Sign out | `inkwell signout` (interactive) | Confirm prompt; clears tokens + cache; subsequent commands fail with "not signed in". |

A row that fails blocks the release. File the failure with the
recorded fixtures and CI run link before retrying the tag.
