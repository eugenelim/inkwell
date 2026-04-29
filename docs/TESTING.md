# inkwell testing standards

The single source of truth for how tests are written, named, organised,
and run in this repo. CLAUDE.md §5 references this document; if the
two ever conflict, this one wins (the spirit of §5 stays — the
mechanics live here).

---

## 1. Stack

| Concern                         | Tool                                                       |
| ------------------------------- | ---------------------------------------------------------- |
| Test runner                     | stdlib `testing`                                           |
| Assertions                      | `github.com/stretchr/testify/require` + `assert`           |
| Bubble Tea TUI                  | `github.com/charmbracelet/x/exp/teatest`                   |
| HTTP mocking (Graph)            | stdlib `net/http/httptest`                                 |
| Goroutine leak detection        | `go.uber.org/goleak` in `TestMain` (per-package)           |
| Fuzzing                         | stdlib `go test -fuzz=` (parsers, lexers, decoders only)   |
| Coverage                        | `go test -coverpkg=./internal/... -coverprofile=cover.out` |
| Pure-Go SQLite (no CGO)         | `modernc.org/sqlite` — same in tests as production         |
| Race detector                   | always on for unit tests (`go test -race ./...`)           |

`require` for fail-fast assertions (most of the file). `assert` only
when you want the test to keep running after a failure to gather more
signal. Mixing the two in one test is fine.

---

## 2. The four test layers

| Layer                | File pattern              | Build tag       | Purpose                                                                                       |
| -------------------- | ------------------------- | --------------- | --------------------------------------------------------------------------------------------- |
| Unit                 | `*_test.go`               | none            | Pure functions, table-driven cases, dispatch logic, parsers, evaluators, store queries.       |
| Integration          | `*_integration_test.go`   | `integration`   | Multi-package flows over real SQLite + recorded Graph fixtures.                               |
| TUI end-to-end       | `*_e2e_test.go`           | `e2e`           | Bubble Tea program driven via teatest. Visible-delta assertions only.                         |
| Benchmark            | `*_test.go` (`Benchmark`) | none, `-run=^$` | Per-budget gates from PRD §7 / per-spec §"Performance budgets".                               |

Run them with `make regress`. Adding a new layer is a CLAUDE.md change.

---

## 3. Naming

```
TestUnitOfWork_Scenario_ExpectedResult
TestExecutorMarkRead_PATCHesIsRead_AndFlipsLocalState
```

Lowercase package name implicit. Underscore as the soft separator.
Subtest names go in `t.Run`; **they should also describe the case**:

```go
t.Run("prefix wildcard", func(t *testing.T) { ... })
t.Run("suffix wildcard", func(t *testing.T) { ... })
```

For tests that pin a regression caught in production, lead with the
symptom:

```go
TestFolderSwitchClearsActiveSearch        // user reported "search lingers"
TestExecutorSoftDeleteWhenDestinationIDDiffersFromAlias  // FK constraint bug
```

Future-you reads the test list and sees what bugs we've already paid
for.

---

## 4. Conventions

### 4.1 Mandatory helpers

- **`t.Helper()`** at the top of every test helper. Failures point at
  the caller, not the helper.
- **`t.TempDir()`** for any filesystem fixture. Auto-cleaned.
- **`t.Cleanup(...)`** for resources you opened (DB handles, http
  servers, fake processes). Runs in LIFO order even on `t.FailNow`.
- **`t.Setenv(k, v)`** — never use `os.Setenv` in tests; the helper
  restores on cleanup.

### 4.2 Parallelism

- Add `t.Parallel()` at the top of any test that doesn't share global
  state. The race detector likes this; the suite finishes faster.
- Tests that touch `nowFn`, package-level globals, or environment
  variables must NOT call `t.Parallel()`.
- Never `t.Parallel()` inside a subtest if the parent doesn't allow it.

### 4.3 Tables

```go
cases := []struct {
    name string
    in   string
    want T
}{
    {"empty input", "", T{}},
    {"single token", "foo", T{Foo: "foo"}},
}
for _, c := range cases {
    c := c // pin loop var (still a habit even on 1.22+)
    t.Run(c.name, func(t *testing.T) {
        t.Parallel()
        got := UnitUnderTest(c.in)
        require.Equal(t, c.want, got)
    })
}
```

### 4.4 Time-as-dependency

Production code that reads the clock takes a `nowFn func() time.Time`
or accepts a `time.Now`-shaped field on a struct. Tests inject a fixed
clock. Never call `time.Now()` inline in the production path of a
non-trivial function.

`internal/pattern/dates.go` is the example: `nowFn = time.Now` at
package-level, tests swap it via `fixedNow(t, "2026-04-28T12:00:00Z")`.

### 4.5 Goroutine leak detection

Every package that spawns goroutines (`internal/sync`,
`internal/action`, `internal/ui`) runs goleak in `TestMain`:

```go
package sync

import (
    "testing"
    "go.uber.org/goleak"
)

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m,
        goleak.IgnoreTopFunction("modernc.org/sqlite/lib.gomemFreeAll"),
    )
}
```

A goroutine leaked from a test means your code-under-test has a
shutdown bug that production will eventually hit.

### 4.6 HTTP mocking

Use `httptest.NewServer` + a `*http.ServeMux`. Wire `graph.NewClient`
with `BaseURL: srv.URL`. Capture request payloads via
`atomic.Pointer[T]` if multiple goroutines may write.

Pattern:

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    require.Equal(t, http.MethodPatch, r.Method)
    var body map[string]any
    require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
    capturedBody.Store(&body)
    w.WriteHeader(http.StatusOK)
}))
t.Cleanup(srv.Close)
```

Never use real network calls in unit tests. CI cannot reach Graph.

### 4.7 Fuzz targets

Anything that parses untrusted input gets a fuzz target:

```go
func FuzzParse(f *testing.F) {
    seeds := []string{
        "~f bob",
        "~A & ~N",
        "(~f a | ~f b) ~A",
        "~d <30d",
    }
    for _, s := range seeds {
        f.Add(s)
    }
    f.Fuzz(func(t *testing.T, src string) {
        _, _ = Parse(src) // must not panic on any input
    })
}
```

Run with `go test -fuzz=Fuzz -fuzztime=30s ./internal/pattern/...`
locally before any change to the parser. The corpus lives in
`testdata/fuzz/<TestName>/` and accumulates over time as crashes are
found; commit those files.

### 4.8 Bubble Tea (TUI) tests

Two tiers:

1. **Dispatch tests** (`internal/ui/dispatch_test.go`, no build tag):
   call `Update(msg)` directly, inspect model fields. Cheap, fast,
   test the state machine. Use this for "does pressing X mutate Y".

2. **Visible-delta tests** (`internal/ui/app_e2e_test.go`, e2e tag):
   teatest with `WithInitialTermSize(120, 30)`, drive keystrokes,
   assert on the rendered framebuffer. Use this for "does pressing X
   make Y visible".

Per CLAUDE.md §5.4: every keymap binding gets at least one dispatch
test; every visible state transition gets at least one visible-delta
test.

### 4.9 Benchmarks

```go
func BenchmarkListMessagesInbox100kLimit100(b *testing.B) {
    st := openBenchStore(b)        // helper, t.Helper at top
    seedMessages(b, st, 100_000)
    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := st.ListMessages(ctx, q)
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

Always `b.ReportAllocs()`. Always `b.ResetTimer()` after the seed
phase. Run with `make test-bench` (no race; race inflates per-op
time and gives misleading numbers).

### 4.10 Coverage

```sh
go test -race -coverpkg=./internal/... -coverprofile=cover.out ./internal/...
go tool cover -func=cover.out
```

CLAUDE.md §5.1 sets the floor at 80% for `store`, `graph`, `pattern`,
`auth`, `sync`. UI/CLI coverage is measured but not gated — the e2e
suite covers UI behaviour in a way line coverage doesn't capture.

### 4.11 Skipping slow paths under `-short`

```go
func TestSomethingThatTakes30s(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping under -short")
    }
    ...
}
```

`make test-short` skips these; `make regress` runs everything.

---

## 5. Anti-patterns we won't accept

| Anti-pattern                                                         | Why                                                  |
| -------------------------------------------------------------------- | ---------------------------------------------------- |
| `time.Sleep` to "wait for goroutine to start"                        | Race-detector trap. Use channels or sync primitives. |
| Real network calls in unit tests                                     | Flaky CI, vendor lock-in, security exposure.         |
| Tests that pass without asserting anything                           | Theatrical only. Every test ends in `require.X` or `assert.X`. |
| Tests that read `time.Now()` in production code paths                | Non-deterministic; flaky on slow machines.           |
| Copy-pasted test setup blocks                                        | Extract to a `newXXXTest(t)` helper with `t.Helper`. |
| `t.Errorf` instead of `require.X`                                    | testify's APIs are clearer and stop on first error.  |
| Tests in `package foo_test` for internal access                      | Use the same package as the code; `go test` sees both. |
| Build-tag misuse (e.g. `e2e` on a unit test)                         | Tags are for cost-tier selection, not skipping.       |

---

## 6. The regression-test discipline (CLAUDE.md §5.7)

Every user-reported bug ships with its regression test in the same
commit, written **before** the fix. If you can't write the test, you
don't yet understand the bug; write it first, watch it fail, then
write the fix.

This is how the bug stays fixed.

---

## 7. Running the suite

| Command                | What it does                                                                |
| ---------------------- | --------------------------------------------------------------------------- |
| `make test`            | `go test -race ./...` — fast, the inner loop.                               |
| `make test-e2e`        | `go test -tags=e2e ./...` — TUI visible-delta.                              |
| `make test-bench`      | Benchmarks without race.                                                    |
| `make test-short`      | Race tests with `-short` (skips long ones).                                 |
| `make regress`         | Every gate from CLAUDE.md §5.6 in order. **Mandatory before any tag.**      |

`make regress` is the contract. If it's red, nothing ships.
