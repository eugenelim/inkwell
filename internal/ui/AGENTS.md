# internal/ui — AGENTS.md

Package-specific contract. Read the root `AGENTS.md` (entry point) and `docs/CONVENTIONS.md` (long-form rules, §-numbered) first for repo-wide
conventions; this file only spells out what's different about `ui`.

## What this package is

The Bubble Tea TUI: root `Model`, sub-models, `Update` dispatch, key
bindings, panes, modes, theme, command palette, viewer renderer.

## Hard invariants (specific to this package)

1. **Sub-models stored by value, not by pointer (ADR-0006).** Update
   takes a value receiver and returns a fresh value. The root rebinds:
   `m.list, cmd = m.list.Update(msg)`. A `tea.Cmd` that captures
   `*subModel` would race with the next Update cycle — don't.
2. **One root `Update`.** Dispatches by `Mode` (Normal / Command /
   Search / SignIn / Confirm / …). New mode = new constant in
   `messages.go` + new branch in `Update`. Pane-scoped meanings (`r` =
   reply in viewer, `r` = mark-read in list) resolve at the focused
   pane, not via separate bindings.
3. **Never block.** Every Graph/DB call is dispatched as a `tea.Cmd`
   that returns a typed `tea.Msg`. The Update function is pure; I/O
   happens on the Cmd's goroutine.
4. **Visible-delta tests are mandatory.** Every new key binding, focus
   change, cursor move, mode swap, pane swap needs a `*_e2e_test.go`
   that captures frames before/after and asserts the **user-visible
   glyph** moved (cursor `▶` row, focus marker `▌ <Pane>`, viewer
   content swap). String-in-buffer assertions are not enough — `docs/CONVENTIONS.md` §5 explains why (v0.2.6 lesson).
5. **No layering up.** `ui` does not import `internal/graph` or
   `internal/auth` directly. Graph access goes through
   `internal/sync` and `internal/action`; auth state is read through
   the `Deps` injected at construction.
6. **No inline ANSI escapes.** All Lip Gloss styles live in
   `internal/ui/theme.go`. The `render` package owns viewer-content
   styling; `ui` owns chrome / status / cursor styling.

## Conventions

- `BindingOverrides` (`keys.go`) is the consumer-site shape of
  `config.BindingsConfig` — defined here so `ui` doesn't need to
  import `internal/config` (`docs/CONVENTIONS.md` §2 layering).
- Window resize: every paint must respect `WindowSizeMsg`. Hard-coded
  widths only as defaults sourced from `[ui]` config.
- New key binding? Update `internal/ui/keys.go` `DefaultKeyMap`,
  `BindingOverrides`, the help text, the visible-delta e2e test, and
  `docs/user/reference.md` (`docs/CONVENTIONS.md` §12.6 doc sweep).
- New palette command? Add to `palette_commands.go` with an
  `ID:` and `Title:` field; the doc-sweep advisory check (run with
  `--all`) verifies the ID appears in `docs/user/reference.md`.

## Testing

- Unit tests for dispatch logic (`dispatch_test.go`, `keys_test.go`).
- E2E tests via `teatest` for visible-delta. Build tag `e2e`.
- New mode → corresponding test in `app_e2e_test.go`.
- **AI exploratory fuzz** (`internal/ui/aifuzz_test.go`, build tag
  `e2e && aifuzz`). Drives the real `Model` with a 38-action fuzz
  alphabet and writes frames + unified diffs to
  `.context/ai-fuzz/run-<ts>/` for Claude Code to oracle. Required
  closing ritual for **any spec or major change that touches this
  package** (`docs/CONVENTIONS.md` §11 DoD): `make ai-fuzz`, read
  `REVIEW.md`, fix or file every flagged anomaly. Replay a finding
  with `INKWELL_FUZZ_SEED=N scripts/ai-fuzz.sh <steps>`.

## Common pitfalls

- Using a pointer sub-model "just for the test setup" — leaks into
  the production path, breaks ADR-0006.
- Asserting on the contents of `tea.Model.View()` directly instead of
  via teatest's frame-comparison — string-in-buffer false-positives.
- Forgetting that `key.WithHelp` strings are visible to the user;
  changes there are user-visible changes and need a doc-sweep entry.

## References

- spec 04 (TUI shell), spec 22 (command palette), spec 24 (split
  inbox tabs), spec 28 (screener), spec 30 (done alias).
- ARCH §10 (UI architecture).
- ADR-0006 (Bubble Tea sub-models by value).
