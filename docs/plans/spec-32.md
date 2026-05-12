# Spec 32 — Server-side rules (Inbox messageRules)

## Status
not-started

## DoD checklist

### Migration
- [ ] `internal/store/migrations/014_message_rules.sql` (re-confirm
      slot at merge per spec §3.1 handshake) creates the
      `message_rules` table — composite PK `(account_id,
      rule_id)`, CHECK on bool columns, CHECK on
      `length(rule_id) > 0`, `idx_message_rules_sequence` on
      `(account_id, sequence_num)`, and
      `UPDATE schema_meta SET value = '14'`.
- [ ] `TestMigration014AppliesCleanly` — opens a v13 DB, runs
      014, asserts schema_meta + table + index.

### Store
- [ ] `internal/store/rules.go` (new file) — `ListMessageRules`,
      `GetMessageRule`, `UpsertMessageRule`,
      `UpsertMessageRulesBatch`, `DeleteMessageRule`,
      `LastRulesPull` per spec §4.1. Errors: `ErrInvalidRuleID`
      added to `internal/store/errors.go`.
- [ ] `internal/store/rules_types.go` (new file) —
      `MessageRule`, `MessagePredicates`, `MessageActions`,
      `SizeKB`, `Recipient`, `EmailAddress` per §4.3.
      Pointer types for tri-state booleans; `omitempty` JSON
      tags throughout.
- [ ] SQL in `rules.go` follows spec §4.2: ORDER BY
      `sequence_num ASC, rule_id ASC`; UPSERT via ON CONFLICT;
      `UpsertMessageRulesBatch` is DELETE-all + multi-row INSERT
      under one tx.
- [ ] Store-level tests: `TestUpsertAndListMessageRules`,
      `TestUpsertMessageRulesBatchReplacesAll`,
      `TestDeleteMessageRule` (incl. 404-idempotent),
      `TestMessageRulesFKCascadeOnAccountDelete`,
      `TestLastRulesPullReturnsMaxTimestamp`,
      `TestListMessageRulesOrdering`.

### Graph
- [ ] `internal/graph/rules.go` (new file) —
      `ListMessageRules`, `GetMessageRule`, `CreateMessageRule`,
      `UpdateMessageRule`, `DeleteMessageRule` per §5.2. JSON
      marshal preserves omitempty for tri-state fields; unmarshal
      preserves raw `conditions` / `actions` / `exceptions` sub-
      objects in `RawConditions` / `RawActions` /
      `RawExceptions`.
- [ ] `internal/graph/rules_merge.go` (new file) — `jsonMerge`
      per spec §5.3 PATCH merge semantics. Top-level merge with
      edit-wins.
- [ ] `internal/graph/canonical_json.go` (new file) —
      `canonicalJSON` helper for the §5.4 content-hash
      stability. Sorted-key recursive walk for `map[string]any`
      values.
- [ ] Graph-level tests: `TestGraphListMessageRules_HappyPath`,
      `TestGraphCreateMessageRule_201`,
      `TestGraphUpdateMessageRule_404`,
      `TestGraphDeleteMessageRule_404IsSuccess`,
      `TestGraphRules_RetryAfter429`. Merge-side tests:
      `TestJsonMergePreservesNonV1Keys`,
      `TestJsonMergeReplacesArrays`, `TestJsonMergeEmptyEdit`,
      `TestJsonMergeEmptyPrior`,
      `TestJsonMergeRoundTripsThroughMapAny`. Canonical-JSON:
      `TestCanonicalJSONStableAcrossUnmarshalCycle`.

### Loader + apply pipeline
- [ ] `internal/rules/` (new package). Files: `loader.go`,
      `apply.go`, `pull.go`, `edit.go`, `types.go`. The new
      package is mid-tier (above store + graph; consumed by ui
      + cli).
- [ ] `LoadCatalogue` per §6.4 — validates field names,
      rejects deferred predicates / actions, enforces
      `delete = true` ⇒ `confirm = "always"` (spec 27 §3.4
      parity), rejects duplicate names among ID-less rules,
      compiles error messages with `file:line`.
- [ ] Apply pipeline per §6.5 — pull → load → resolve folders
      via new `GetFolderByPath` → diff (create / update / noop /
      delete; skip `isReadOnly = true`) → confirmation gates
      (per-rule `confirm` plus the global
      `[rules].confirm_destructive` belt-and-suspenders) →
      execute (delete first, then create, then update) →
      atomic TOML rewrite via `.tmp` + `os.Rename`.
- [ ] Pull pipeline (`pull.go`) — fetches via
      `graph.ListMessageRules`, applies the `<unnamed rule N>`
      placeholder for empty `display_name`, replaces the
      mirror via `UpsertMessageRulesBatch`, atomic TOML rewrite.
- [ ] Loader tests: `TestLoadCatalogueValidExample`,
      `TestLoadCatalogueRejectsDeferredPredicate`,
      `TestLoadCatalogueRejectsForwardAction`,
      `TestLoadCatalogueRejectsDeleteWithoutConfirm`,
      `TestLoadCatalogueRejectsConfirmNeverOnDestructive`,
      `TestLoadCatalogueRejectsDuplicateNameNoID`,
      `TestLoadCatalogueAcceptsShorthandFromString`,
      `TestLoadCatalogueMissingFileIsEmpty`,
      `TestLoadCatalogueRejectsUnknownField`.
- [ ] Apply tests: `TestApplyDiffClassifiesCreatesUpdatesDeletes`,
      `TestApplyDryRunNoWrites`, `TestApplySkipsReadOnlyRules`,
      `TestApplyResolvesFolderPaths`,
      `TestApplyFailsOnUnresolvedFolder`,
      `TestApplyPartialSuccess`, `TestApplyConflictDetection`,
      `TestApplyConfirmDestructiveRule`,
      `TestApplyRoundTripPreservesNonV1Fields`,
      `TestApplyDryRunOutputDeterministic`,
      `TestApplyResolvesUnicodeFolderPath`,
      `TestPullAssignsPlaceholderForEmptyDisplayName`,
      `TestPullAtomicRewriteSurvivesInterruption`,
      `TestPullAtomicRewriteCleansUpTmpOnFailure`.

### Store folder-path helper
- [ ] `internal/store/folders.go` — new
      `GetFolderByPath(ctx, accountID, slashPath) (Folder,
      error)` walking the cached folders tree by
      `display_name` per level, NFC-normalised, returning
      `ErrFolderNotFound` on miss.

### Config
- [ ] `internal/config/config.go` — new `RulesConfig` struct
      with `File string` (TOML `file`),
      `PullStaleThreshold time.Duration` (TOML
      `pull_stale_threshold`), `ASCIIFallback bool` (TOML
      `ascii_fallback`), `ConfirmDestructive bool` (TOML
      `confirm_destructive`), `EditorOpenAtRule bool` (TOML
      `editor_open_at_rule`). Embedded as `Rules RulesConfig`
      on the top-level config.
- [ ] `internal/config/defaults.go` — defaults per spec §11.
- [ ] `internal/config/validate.go` — rejects unknown `[rules]`
      keys.
- [ ] `TestConfigDecodeRulesSection` — decode-with-defaults,
      decode-with-overrides, unknown-key rejection.

### TUI
- [ ] `internal/ui/messages.go` — new `MessageRulesMode` mode
      constant (avoids collision with existing `RuleEditMode`).
- [ ] `internal/ui/rules_manager.go` (new file) —
      `RulesModel` value-typed sub-model with selection,
      filter input, last-pull timestamp, in-flight apply
      token. Embedded into root `Model`.
- [ ] KeyMap additions: new `KeyMap.Rules` group with `Next`,
      `Prev`, `Open`, `New`, `Edit`, `Delete`, `Toggle`,
      `ReorderUp`, `ReorderDown`, `Pull`, `Filter`,
      `DryRunApply`, `Apply`, `Close`. Defaults per §7.2.1.
      Mode-scoped; not exposed as global `[bindings]` keys.
- [ ] Root `Update` dispatch: check `MessageRulesMode` in the
      modal-overlay branch alongside `PaletteMode` /
      `SettingsMode`; after `SignInMode` / `ConfirmMode`,
      before per-pane dispatch.
- [ ] Read-only rule rendering per §7.3: 🔒 / `[ext]` / ⚠
      glyphs with ASCII fallbacks (`RO` / `ERR`; `[ext]` is
      already ASCII) gated by `[rules].ascii_fallback`.
- [ ] Responsive modal sizing per §7.2:
      `min(80, terminal_width − 4)` × `min(20,
      terminal_height − 6)`; collapsed two-column table below
      60 columns. Honours `WindowSizeMsg`.
- [ ] `$EDITOR` integration in `internal/rules/edit.go`
      (re-using the spec-15 compose suspend/resume pattern).
      Honours `[rules].editor_open_at_rule` for the `+<line>`
      argument.
- [ ] UI dispatch / e2e tests per §13: modal open/close,
      navigation, view, toggle PATCH, delete confirm,
      reorder J/K (asserts two PATCHes; transient duplicate
      sequence is acceptable), read-only / external / error
      refuses E, dry-run pane, apply pane, pull refresh,
      palette rows static, ASCII fallback.

### CLI
- [ ] `cmd/inkwell/cmd_rules.go` (new file) implements every
      subcommand per §8.1: `list`, `get`, `pull`, `apply`
      (`--dry-run`, `--yes`), `edit`, `new` (`--name`),
      `delete` (`--yes`), `enable`, `disable`, `move`
      (`--sequence`). Registered in `cmd_root.go`. Exit codes
      per §8.2 (0/1/2/3).
- [ ] Cmd-bar parity (§8.3): `:rules <subverb>` dispatches via
      the same handlers as the CLI. `:rules` alone opens the
      modal.
- [ ] CLI tests: `TestCLIRulesListEmpty`,
      `TestCLIRulesListPopulated`, `TestCLIRulesGetByID`,
      `TestCLIRulesPullRewritesFile`, `TestCLIRulesApplyDryRun`,
      `TestCLIRulesApplyYes`, `TestCLIRulesToggle`,
      `TestCLIRulesMove`, `TestCLIRulesEditInteractiveRejectsJSON`.

### Palette
- [ ] `internal/ui/palette_commands.go` gains the five static
      palette rows per §7.6 (`rules_open`, `rules_pull`,
      `rules_apply`, `rules_dry_run`, `rules_new`). Each row
      delegates to the same handlers as the cmd-bar.

### Logging + redaction
- [ ] New DEBUG-level log lines `graph.rules.list`,
      `graph.rules.get`, `graph.rules.create`,
      `graph.rules.update`, `graph.rules.delete` per §12.1.
- [ ] One INFO-level `rules.apply` summary line per
      `apply` invocation with counts only (no predicate
      values).
- [ ] `display_name` and predicate substring values go through
      slog structured fields so the existing redactor's email
      regex applies at INFO+.
- [ ] Redaction tests: `TestRedactScrubsRuleDisplayNameAtInfo`,
      `TestRulesLoggingDoesNotLeakBodyContains`.

### Docs
- [ ] `docs/CONFIG.md` — new `[rules]` section per §11 (five
      keys); cross-reference to PRD §3.1
      `MailboxSettings.ReadWrite`.
- [ ] `docs/user/reference.md` — `:rules` family verbs table;
      `inkwell rules` subcommand table; manager-modal bindings
      table; `~/.config/inkwell/rules.toml` field catalogue.
      Footer `_Last reviewed against vX.Y.Z._` updated.
- [ ] `docs/user/how-to.md` — "Manage server-side rules"
      recipe; "When to use a server rule vs. a routing
      assignment" + "When rules and the screener disagree"
      cross-feature notes.
- [ ] `docs/user/explanation.md` — paragraph on
      "configuration-as-code: rules.toml is your source of
      truth".
- [ ] `docs/PRD.md` §10 — add spec 32 inventory row.
- [ ] `docs/ROADMAP.md` — Bucket 4 row "Server-side rules"
      flipped to `Shipped vX.Y.Z (spec 32)`; §1.14 backlog
      heading updated.
- [ ] `docs/THREAT_MODEL.md` — new row per §14 spec-17 review.
- [ ] `docs/PRIVACY.md` — row for `message_rules` table +
      `rules.toml` user-typed predicate values.
- [ ] `README.md` Status table — row for the new capability;
      download example version bumped if this is the latest
      release.
- [ ] `docs/plans/spec-32.md` — set `Status: done`; final
      iteration entry with version + measured perf numbers.

## Perf budgets

| Surface                                                                                         | Budget          | Measured | Bench / Test                                              | Status      |
|-------------------------------------------------------------------------------------------------|-----------------|----------|-----------------------------------------------------------|-------------|
| `Store.ListMessageRules` over 50-rule mirror                                                    | ≤2ms p95        | —        | `BenchmarkListMessageRules`                               | not measured |
| `Store.UpsertMessageRulesBatch` writing 50 rules                                                | ≤20ms p95       | —        | `BenchmarkUpsertMessageRulesBatch`                        | not measured |
| `inkwell rules pull` end-to-end (50-rule fixture, mocked Graph)                                  | ≤2s p95         | —        | `TestRulesPullEndToEnd_50Rules`                            | not measured |
| `inkwell rules apply --dry-run` (50-rule fixture)                                                | ≤200ms p95      | —        | `TestRulesApplyDryRun_50Rules`                             | not measured |
| `inkwell rules apply` diff computation (10c/10u/5d)                                             | ≤50ms p95       | —        | `BenchmarkRulesDiffComputation` in `internal/rules/`       | not measured |
| `:rules` modal cold open → first render                                                          | ≤100ms p95      | —        | `TestRulesModalOpensInTime`                                | not measured |
| `T` toggle synchronous PATCH (mocked Graph 50ms)                                                 | ≤500ms p95      | —        | `TestRulesToggleEndToEnd`                                  | not measured |

## Iteration log

### Iter 0 — 2026-05-12 (drafting)

- Slice: spec authoring + adversarial review.
- Output: `docs/specs/32-server-side-rules.md` (~1900 lines),
  this plan file, PRD/ROADMAP updates, README status row.
- Research: Microsoft Graph `messageRule` resource +
  `messageRulePredicates` (29 fields) +
  `messageRuleActions` (11 fields); permissions
  (`MailboxSettings.ReadWrite`, already granted); Outlook /
  Gmail / Thunderbird / mutt+Sieve / aerc / himalaya prior
  art.
- Adversarial review pass 1: 35 findings (6 blockers, 21
  significant, 8 nits). All addressed.
- Adversarial review pass 2: 9 new findings (clarifying
  predicate AND/OR rendering in viewer; missing flag enum
  enumeration; size_max_kb sentinel value;
  migration-handshake regex coverage; tmp-file orphan cleanup;
  jsonMerge map[string]any round-trip test; reorder transient
  duplicate sequence; screener interaction wording). All 9
  addressed.
- Result: spec ready for implementation; no further blockers.
- Next: implementation slice when scheduled.
