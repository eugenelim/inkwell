<!--
Mirrors CLAUDE.md §11 (Definition-of-done) and §12.6 (doc sweep).
Tick boxes as you complete each item. Link the green CI run at the bottom.
Drop any section that genuinely doesn't apply (e.g. "Spec content" for a
pure refactor) — but say so in the Summary rather than silently deleting.
-->

## Summary

<!-- 1–3 sentences. What changed and why. Link the spec: docs/specs/NN-*.md -->

**Spec:** <!-- docs/specs/NN-*.md, or "n/a — refactor/chore" -->
**Spec 17 impact:** <!-- one line: "none" | "new log site at X — redaction test added" | "new file I/O at Y — §4.4 path check added" | etc. -->

## Spec content (CLAUDE.md §11)

- [ ] Which Graph scope(s)? Are they in PRD §3.1?
- [ ] What state does it read from / write to in `store`?
- [ ] What Graph endpoints does it call?
- [ ] How does it behave offline?
- [ ] What is its undo behaviour?
- [ ] What error states surface to the user, and how?
- [ ] Is there a CLI-mode equivalent (PRD §5.12)?

## Tests + benchmarks

All five must be green on a clean checkout (CLAUDE.md §5.6). Paste the
relevant counts or attach the `make regress` tail.

- [ ] `go vet ./...`
- [ ] `go test -race ./...` (unit + dispatch)
- [ ] `go test -tags=integration ./...`
- [ ] `go test -tags=e2e ./...` (if the spec touches the TUI)
- [ ] `go test -bench=. -benchmem -run=^$ ./...` — every perf budget in the spec has a benchmark; passes within budget on the dev machine (>50% over budget fails — §5.2)
- [ ] Redaction tests cover every new log site that could see secrets (§7 invariant 3)

## Security (spec 17)

- [ ] If this PR introduces or changes token handling, file I/O paths, subprocess invocation, external HTTP, third-party data flow, cryptographic primitive, SQL composition, or local persisted state — `docs/specs/17-*.md` §4, `docs/THREAT_MODEL.md`, and/or `docs/PRIVACY.md` updated in the same PR.
- [ ] CI gates green: gosec, Semgrep, govulncheck. Any new `// #nosec` annotation carries a one-line WHY comment (no blanket suppression).
- [ ] `make sec` clean locally.

## Doc sweep (CLAUDE.md §12.6)

Tick only those that apply. If you're shipping a tag, every applicable
row must be in this PR or the immediately-following commit.

- [ ] `docs/plans/spec-NN.md` updated (status, iteration log, perf numbers)
- [ ] `docs/specs/NN-*.md` carries `**Shipped:** vX.Y.Z` (if tagging)
- [ ] `docs/PRD.md` §10 inventory row updated
- [ ] `docs/ROADMAP.md` bucket table + §1 backlog heading updated
- [ ] `docs/user/reference.md` lists every new key binding, `:command`, CLI verb, pattern operator, mode/chord, sidebar section, indicator glyph, or viewer header line
- [ ] `docs/user/how-to.md` recipe added if the spec introduces a meaningful new task flow
- [ ] `docs/user/tutorial.md` / `docs/user/explanation.md` updated only if the first-launch path or a design invariant moved
- [ ] `docs/CONFIG.md` rows for every new config key (§9)
- [ ] `README.md` Status table row added if a user-visible capability is new

## Conventions (CLAUDE.md §10)

- [ ] Conventional Commit prefix used (`feat(spec-NN): …`, `fix(pkg): …`, etc.)
- [ ] No `Co-Authored-By` trailer in any commit
- [ ] No `--no-verify`, no force-push to `main`

## CI

<!-- Paste the green run URL: gh run list --limit 1 -->
