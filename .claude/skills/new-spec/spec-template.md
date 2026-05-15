# Spec <NN> — <TITLE>

**Status:** Draft.
**Depends on:** <spec NN, spec MM, …>
**Blocks:** <follow-on specs, if any>
**Estimated effort:** <hours / days>

---

## 1. Goal

One paragraph in plain English: what does this feature do, and why
is it worth building now? Include the link to the ROADMAP bucket
this lands under, if applicable.

### 1.1 What does NOT change

The non-goals. List at least two. These are what prevents scope
creep both at review time and later when someone re-opens the spec.

- ...
- ...

## 2. Prior art (optional)

Cite mail clients / TUIs that handle the same problem, and what
inkwell takes / rejects from each. Skip this section if there's no
useful prior art.

## 3. Behaviour

Numbered, testable bullets. Each bullet states an observable
post-condition or invariant. No "should be fast" — quantify, or
move to §6 perf budgets.

1. ...
2. ...
3. ...

### 3.1 Offline behaviour

What happens when the network is down or the action queue is
draining? Idempotency story.

### 3.2 Undo behaviour

Either "undoable via `u` (action queue rollback)" with the rollback
mechanics, or "not undoable" with a justification (typically:
destructive permanent-delete with confirmation gate).

### 3.3 Error surfaces

What does the user see when the operation fails? Toast text,
status-line, modal — be specific.

## 4. Graph scopes

Which scope(s) does this feature require? Confirm each is in PRD
§3.1 (granted scopes). If a desired scope is in §3.2 (denied), the
feature is out of scope until that changes.

| Scope | Required for | In PRD §3.1? |
| --- | --- | --- |

## 5. CLI-mode equivalent

Per PRD §5.12, every TUI feature should have a CLI-mode answer.
Either the verb / flag it exposes via `cmd/inkwell/`, or "n/a — TUI
affordance only" with a reason.

## 6. Performance budgets

Every row here gets a `Benchmark*` in the corresponding package.
Budgets are absolute (not "no worse than current"). Misses >50% over
budget fail CI (`docs/CONVENTIONS.md` §5.2).

| Surface | Budget | Notes |
| --- | --- | --- |
| ... | ... | ... |

## 7. Spec 17 impact

One line, copied into the PR description's "spec 17 impact:" field:

- **Token handling:** none | <what & how>
- **File I/O:** none | <paths touched + traversal guard>
- **Subprocess:** none | <command + redaction>
- **External HTTP:** none | Graph only | <other>
- **SQL composition:** none | parameterised only | <other>
- **Persisted state:** none | <table / column + redaction story>

If any row is non-none, this PR must also update
`docs/THREAT_MODEL.md` and/or `docs/PRIVACY.md`.

## 8. Module layout / changed files

List the new and changed files by path. Be specific — name the test
stubs to update, the schema-version line to bump, the doc rows to
add. `docs/CONVENTIONS.md` §12.0 calls out that vague file lists are the #1
source of adversarial findings.

**New:**
- `internal/<pkg>/<file>.go`
- ...

**Changed:**
- `internal/<pkg>/<file>.go` — <what changes>
- ...

## 9. Definition of done

Mirrors `docs/CONVENTIONS.md` §11. Carried forward into the plan file's DoD
checklist; ticked there.

**Spec content**
- [ ] Behaviour bullets are all testable
- [ ] Non-goals listed (at least two)
- [ ] Graph scopes verified against PRD §3.1
- [ ] Perf-budget rows have benchmarks
- [ ] Spec 17 impact line filled
- [ ] CLI-mode answer present

**Tests + benchmarks** (in PR)
- [ ] `make regress` green
- [ ] Redaction tests for every new log site
- [ ] Visible-delta e2e for every new UI surface

**Docs**
- [ ] `docs/CONFIG.md` rows for every new config key
- [ ] `docs/user/reference.md` lists every new binding / verb / config
- [ ] `docs/user/how-to.md` recipe for any new task flow
- [ ] `docs/specs/NN-<title>/plan.md` maintained per `docs/CONVENTIONS.md` §13

## 10. Open questions

Things you don't yet know that block writing the plan. Each one is
a one-liner with a who-decides annotation.

- ...

---

_When the spec ships, add `**Shipped:** vX.Y.Z (YYYY-MM-DD)` near the top
of this file per `docs/CONVENTIONS.md` §12.6._
