# ADR 0002: Pure-Go stack, no CGO

- **Status:** Accepted (2026-05-13)
- **Deciders:** eugenelim
- **Supersedes:** —
- **Related:** ARCH §1, CLAUDE.md §1

## Context

Inkwell ships as a signed macOS binary that users install from a
release artefact. The same binary needs to cross-compile cleanly for
linux/amd64 and linux/arm64 (release pipeline runs on Ubuntu).

The Go ecosystem has two routes for the load-bearing libraries we
need:

- **SQLite** — the obvious choice (`mattn/go-sqlite3`) is a CGO wrapper
  around the upstream SQLite C library. Performance is excellent, but
  it forces every consumer to be CGO-enabled.
- **HTML→text, keychain, TOML, MSAL, Bubble Tea** — all have pure-Go
  implementations of acceptable quality.

CGO has two costs that matter here. First, macOS notarization. A CGO
binary links libSystem in ways that interact badly with the notary
service's checks (gatekeeper sees a mixed-language binary and flags
warnings that we can't easily suppress without setting up a developer
ID code-signing identity for every dependency). Second, cross-compile.
A CGO build needs a C toolchain for the *target* triple — building
linux/arm64 from a macOS dev machine without CGO is `GOOS=linux
GOARCH=arm64 go build`; with CGO it's "set up a cross-compiler, hope
your dependencies' configure scripts cooperate."

For a single-developer OSS project, the second cost is the binding one.

## Decision

The entire dependency tree is pure-Go. Specifically:

- `modernc.org/sqlite` for the local cache (pure-Go SQLite
  transpilation by Jan Mercl). WAL mode + FTS5 supported.
- `github.com/jaytaylor/html2text` for HTML rendering.
- `github.com/zalando/go-keyring` for macOS Keychain (pure-Go bindings
  to Security framework via syscalls).
- `github.com/AzureAD/microsoft-authentication-library-for-go` (MSAL Go).
- `github.com/charmbracelet/{bubbletea,bubbles,lipgloss}` for the TUI.
- `github.com/BurntSushi/toml` for config.
- Stdlib `net/http`, `log/slog`, `testing`.

Adding any CGO dependency requires superseding this ADR.

## Consequences

### Positive
- `GOOS=… GOARCH=… go build` always works. CI build matrix is trivial.
- macOS notarization passes without per-dependency code-signing.
- No surprise build failures on a fresh machine from missing C
  headers.
- The release binary is statically linked (no glibc-vs-musl runtime
  surprises on Linux).

### Negative
- `modernc.org/sqlite` is ~2–3× slower than CGO `mattn/go-sqlite3` for
  some workloads. For inkwell's 100k-message budgets (ARCH §1, PRD §7),
  benchmarks show this is comfortably within margin — but we forfeit
  headroom for future bulk operations on much larger mailboxes.
- A subtle bug in `modernc.org/sqlite`'s transpilation would be harder
  to debug than a bug in upstream SQLite C. Mitigated by the fact that
  modernc is actively maintained and the test suite mirrors SQLite's.

### Neutral
- Smaller pool of contributors who'd consider "switching to CGO" a
  reasonable suggestion. Acceptable for an opinionated OSS app.

## Alternatives considered

**`mattn/go-sqlite3` (CGO).** Rejected for the notarization +
cross-compile costs above. The performance edge isn't material at
inkwell's scale.

**Bundle a SQLite binary alongside the Go binary and shell out.**
Operationally bizarre; latency cost dwarfs any in-process gain.
Rejected.

**Skip SQLite entirely, use a Bolt/Badger-style KV store.** Considered
briefly. Rejected because the spec relies on FTS5 for the search
layer (spec 06) and re-implementing that on a KV is a research
project.

## References

- ARCH.md §1 (Tech stack, locked).
- CLAUDE.md §1 (Stack invariants, do not negotiate).
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) — upstream.
