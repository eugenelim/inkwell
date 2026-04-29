# Spec 08 — Pattern Language

## Status
in-progress (CI scope: lexer + parser + AST + local-SQL evaluator landed in v0.5.0; Graph $filter / $search evaluators land alongside spec 09/10).

## DoD checklist (mirrored from spec)
- [x] Lexer tokenises operators (`~f`/`~t`/…), arguments (bare-word + quoted), boolean operators (`&` `|` `!`), parens. Position tracked for diagnostics.
- [x] Parser: precedence-climbing for `!` > `&` > `|`. Implicit AND between adjacent atoms. Parenthesised groups.
- [x] AST nodes: And / Or / Not / Predicate{Field, Value} with typed value union (StringValue / DateValue / EmptyValue).
- [x] Wildcard handling for string predicates: `*foo` suffix, `foo*` prefix, `*foo*` contains, exact otherwise. Multi-`*` degrades to contains.
- [x] Date parsing: `<Nd`/`>Nd` (relative), `<=YYYY-MM-DD`/`>=YYYY-MM-DD` (absolute), `today`/`yesterday`, `a..b` (range).
- [x] Local SQL evaluator: AST → WHERE clause + bound args. LIKE-escaping for literal `%`/`_` in user input.
- [x] No-arg predicates (~A, ~N, ~F, ~U) emit fixed clauses.
- [x] Tests: 20 cases in pattern_test.go covering parser surface + evaluator output + corner cases (unterminated quotes, unknown operators, multi-star, escape).
- [ ] Graph $filter evaluator — deferred to spec 09 (batch executor will route OData-friendly predicates to the server side).
- [ ] Graph $search evaluator — deferred (string predicates over body need server-side full-text).
- [ ] Compile() chooser that picks the right backend(s) — deferred. v0.5.0 callers go directly to CompileLocal.
- [ ] Bench at 100k messages — deferred until spec 10 wires this into a UI flow with a real query budget.

## Iteration log

### Iter 1 — 2026-04-28 (parser + AST + local SQL evaluator)
- Slice: every file in internal/pattern/ in one cut: ast.go, lexer.go, parser.go, dates.go, eval_local.go, plus pattern_test.go.
- Files added: 5 source + 1 test (700 LOC including tests).
- Commands: `go test -race ./internal/pattern/...` green in 1.2s.
- Surface decisions:
  - Lexer's "argument" token is greedy: it consumes everything from the operator's letter to the next whitespace or boolean punctuation, with quoted-string support for arguments containing spaces.
  - Implicit-AND lives in the parser, not the lexer. Adjacent atoms (`tkOperator` after `tkArgument`) trigger an AND insertion.
  - Wildcards desugar to LIKE patterns at evaluator time, not parse time. The AST records MatchKind so the Graph evaluators (when they land) can pick startswith / endswith / contains for OData.
  - Dates: `<30d` becomes DateWithinLast (semantically "received >= now-30d") rather than DateBefore. The evaluator then emits `received_at >= ?`. This avoids accidentally negating user intent — a user typing `<30d` expects "recent", not "everything before 30 days ago".
- Critique:
  - The duration parser approximates months at 30 days and years at 365 — Mutt does the same; the rare user who needs calendar months should use absolute dates.
  - `~m folder/path` accepts the path string but the local evaluator currently matches against `folder_id` directly (since messages don't carry the path). Bulk-ops UX (spec 10) will resolve the path → folder ID at compile time before running this evaluator.
  - `~G category` emits `categories LIKE %name%` against the JSON-serialised array. Cheap and correct for short category names; falls down for category names that share substrings (e.g. "Work" vs "Workshop"). Acceptable for v0.5.0; revisit when categories become a hot path.

## Cross-cutting checklist (CLAUDE.md §11)
- [x] Scopes used: none in this spec — pattern is pure compute.
- [x] Store reads/writes: none directly. The evaluator emits SQL fragments the caller embeds in their own query.
- [x] Graph endpoints: none in this iter. Server-side evaluators land later.
- [x] Offline behaviour: trivially offline; no network surface.
- [x] Undo: N/A.
- [x] User errors: ParseError surfaces with byte offset + reason. CompileLocal returns an error for ~h (server-only).
- [x] Latency budget: not measured yet — spec 10 will gate this when patterns sit on the hot path.
- [x] Logs: pattern package doesn't log. Caller decides what to log.
- [x] CLI mode: spec 14.
- [x] Tests: 20 unit tests covering parser, lexer error paths, evaluator output, escape semantics, date forms.
