# internal/store — AGENTS.md

Package-specific contract. Read the root `AGENTS.md` (entry point) and `docs/CONVENTIONS.md` (long-form rules, §-numbered) first for repo-wide
conventions; this file only spells out what's different about `store`.

## What this package is

The sole owner of the local SQLite cache (`mail.db`). Nothing else in the
repo opens that file. All other packages consume Store's typed API.

- DB path: `~/Library/Application Support/inkwell/mail.db`, mode `0600`.
- Engine: `modernc.org/sqlite` (pure-Go, no CGO — see ADR-0002).
- WAL journal mode; FTS5 enabled for search.
- All connections via one `*sql.DB`; statements via prepared `*sql.Stmt`.

## Hard invariants (specific to this package)

1. **No mail-state write outside the action queue.** Mail mutations
   (move, mark-read, soft-delete, flag, categorise, draft) flow
   through `internal/action` → `store.EnqueueAction` → `Executor.Drain`.
   Direct `UPDATE` / `INSERT` against `messages` / `folders` /
   `actions` from outside the package is a layering violation.
   **Cache-management writes** are the explicit exception:
   `EvictBodies` (spec 02 §3.5), `IndexBody` / `UnindexBody` /
   `EvictBodyIndex` / `PurgeBodyIndex` (spec 35 §6.2) are owned by
   the store, do not touch Graph, are idempotent, and have no undo.
   They are called from sync's maintenance loop or render's
   post-decode hook, not from the action queue.
2. **Migrations are forward-only.** Add a numbered file under
   `migrations/`, bump `SchemaVersion` in `store.go`, and add a
   regression test that asserts the new version. Down-migrations do not
   exist (cost > benefit at this scale).
3. **Every mutation is idempotent.** Apply twice = same result. 404-on-
   delete is success. The action queue replays on crash; non-idempotent
   writes corrupt state.
4. **No business logic.** Store stores; it does not interpret. Sender-
   routing rules, screener inferences, conversation grouping — that
   logic lives in `internal/sync` / `internal/pattern` / etc. Store
   exposes the typed primitives those packages compose.

## Testing

- Unit + benchmarks against a **real** SQLite in `t.TempDir()`. No
  in-memory `:memory:` — WAL semantics differ, and we've shipped bugs
  before that mocked DB tests missed.
- Race detector mandatory (per `docs/CONVENTIONS.md` §5.6).
- Budget gate: `TestBudgetsHonoured` runs without `-race` (race inflates
  per-op time). The CI step at `.github/workflows/ci.yml` runs it
  separately.
- Spec-17: every new column / table that could carry user content gets
  a redaction test in the consuming package.

## Common pitfalls

- Adding a column without bumping `SchemaVersion` — caught by the
  schema-version regression test in `migrations_test.go`. Add the
  migration first, then the column-using code.
- Forgetting that `NULL` and empty-string round-trip differently in
  SQLite's typed columns — use `sql.NullString` for optional fields.
- Holding the WAL writer across long-running operations — every batch
  write should commit promptly or yield to readers.

## References

- spec 02 (local cache schema), spec 06 (search), spec 11 (saved
  searches), spec 18 (folder management).
- ARCH §3 (data model), §7 (schema), §8 (action execution).
- ADR-0002 (pure-Go stack).
