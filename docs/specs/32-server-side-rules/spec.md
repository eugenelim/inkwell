# Spec 32 — Server-side rules (Inbox messageRules)

**Status:** Shipped.
**Shipped:** v0.61.0 (CLI + cmd-bar + palette + migration + apply/pull pipeline). The full in-TUI manager modal documented in §7.2 is **deferred** to a follow-up iteration; the CLI plus cmd-bar `:rules` parity is the complete authoring surface for v1, and the §7.6 palette rows provide TUI discoverability.
**Depends on:** Spec 01 (auth — `MailboxSettings.ReadWrite` already
in the requested scopes list, `internal/auth/scopes.go:34`), Spec 02
(store — migration runner + `accounts` FK target), Spec 03 (graph —
`Client.Do(ctx, method, path, body, hdr)` infra in
`internal/graph/client.go`, throttle / auth transports), Spec 04
(TUI shell — `:` command bar, modal dispatch, `WindowSizeMsg`
relayout), Spec 13 (mailbox settings — read-write Graph PATCH
precedent for synchronous, non-queued mutations and modal save
flow), Spec 14 (CLI mode — `cmd_root.go` subcommand registration
pattern), Spec 15 (compose — `$EDITOR` foreground-suspend pattern
this spec reuses for `E` / `N`), Spec 18 (folder management —
folders cache is the source of truth for path-to-ID resolution;
this spec adds a new `GetFolderByPath` helper to
`internal/store/folders.go`, §6.5 step 3), Spec 22 (command
palette — static palette rows for `:rules` family), Spec 27
(custom actions — TOML authoring file precedent at
`~/.config/inkwell/actions.toml`, load-time validation pipeline,
`ConfirmPolicy` tri-state enum that spec 32 reuses verbatim for
`confirm` field). No spec depends on this one; it is a standalone
surface that can ship in any order within Bucket 4 after spec 31.
**Blocks:** None directly. A future spec could compile a subset of
the inkwell pattern language (`~f`, `~B`, `~G`) to a Graph
messageRule via a `~ → Graph` translator and present it as
"promote this saved search to a server rule", but that is a
separate feature and not a v1 requirement (see §15).
**Estimated effort:** 5–7 days (matches the spec 27 / 28 budget for
features that ship a new migration + new graph package + new
internal package + CLI subverbs + TUI mode; the roadmap's
"2 days" estimate predates the curated-subset + apply-pipeline
design and the spec-17 doc obligations).

---

### 0.1 Spec inventory

Server-side rules is item 2 of Bucket 4 (Mailbox parity) in
`docs/ROADMAP.md` §0 and corresponds to backlog item §1.14. Bucket
4 item 1 (Focused / Other tab) shipped in v0.60.0 as spec 31; spec
32 takes the next slot. The PRD §10 spec inventory adds a single
row for spec 32.

The roadmap text for §1.14 is short and explicit:

> Server-side rules run on every incoming message; persist
> server-side; visible across all clients. Different from saved
> searches, which are client-side and on-demand. Microsoft Graph
> supports them via `/me/mailFolders/inbox/messageRules`. A v1
> user can manage rules from the web client; we just don't expose
> them in the TUI. Estimated 2 days.

The implementation budget is broader than the roadmap suggests
because the Graph schema is rich (29 predicate fields, 11 action
fields). v1 of this spec ships a **curated practical subset** —
roughly the same surface a Thunderbird / Outlook Web user would
see in the filter editor — and treats rules outside that subset as
**read-only** in inkwell (visible, not editable). The full Graph
shape is preserved in the local mirror so a follow-up spec can
widen the subset without a schema migration.

---

## 1. Goal

Bring Microsoft Graph **server-side message rules** into inkwell as a
first-class managed resource. The user can list, create, edit,
enable / disable, reorder, and delete rules without leaving the
terminal. Rules run server-side on every incoming Inbox message —
unlike inkwell's client-side saved searches (spec 11) or custom
actions (spec 27) which only execute when the user invokes them —
so the value proposition is **delivery-time automation that works
the same in every client** (Outlook web / desktop / mobile / inkwell)
without any inkwell process being running.

Rule authoring uses the spec-27 TOML-file pattern (`rules.toml`,
declarative, version-controllable, $EDITOR-friendly) backed by a
synchronous Graph CRUD path. A local mirror table caches the latest
pulled state for offline listing and diffing. The Graph
`/me/mailFolders/inbox/messageRules` endpoint is the **only** source
of truth — rule execution is entirely server-side; inkwell never
re-implements it locally.

### 1.1 What does NOT change

- **No new Graph scope.** `MailboxSettings.ReadWrite` is already in
  `internal/auth/scopes.go:34` and PRD §3.1; it is the documented
  permission for `messageRules` CRUD per the Graph reference
  ([Create rule — Permissions](https://learn.microsoft.com/en-us/graph/api/mailfolder-post-messagerules?view=graph-rest-1.0#permissions)).
  v1 ships no scope change.
- **No new pattern operator.** inkwell's local pattern language
  (`~f`, `~B`, `~o`, …) is unrelated to Graph's `messageRulePredicates`
  vocabulary. Saved searches (spec 11) remain local-only and
  on-demand; server rules use Microsoft's own predicate names
  verbatim. A `~ → Graph` translator is out of scope (§15).
- **No new chord prefix.** Rule management is rare and admin-shaped;
  it does not warrant a single-key entry binding. The cmd-bar verb
  `:rules` is the only entry point; the manager modal has its own
  internal bindings (§7.2) that live in a new keymap **group**
  alongside `Help`, `Palette`, etc. — not a chord prefix.
- **No action queue integration.** Rule CRUD is synchronous (matches
  spec 13 mailbox settings, §6 in this spec). Rules apply at
  delivery time on the server; there is nothing to optimistically
  apply in the local mailbox state.
- **No client-side execution.** Inkwell does not evaluate rules
  against local messages. Outlook's "Run Rules Now" is intentionally
  not implemented in v1 (§15): re-running a server rule over the
  cached mailbox requires either (a) implementing the full
  `messageRulePredicates` evaluator locally, which doubles the
  surface area, or (b) calling a Graph `POST` that does not exist on
  v1.0 (`messageRule/applyToInbox` is beta-only and not in our API
  surface — see ARCH §0 "v1.0 only" rule).
- **No new well-known sentinel folder.** Rule destinations
  (`moveToFolder`, `copyToFolder`) resolve against existing
  user-created folders (spec 18). Rules-pane navigation is a modal
  overlay, not a sidebar entry.
- **`messages` table and sync engine are untouched.** The local
  mirror lives in a new `message_rules` table; delta sync
  (`internal/sync/delta.go`) does not need to change. Rule
  evaluation happens on Graph's side and lands as the user
  configured the rule (moved, marked read, categorised, …); the
  resulting state appears in the next normal delta cycle, no
  rule-specific code path.
- **No multi-folder rules.** Graph's `messageRules` is rooted at the
  **Inbox folder** specifically (`POST
  /me/mailFolders/inbox/messageRules`). Outlook does not expose
  Inbox-rule equivalents on other folders via v1.0. Inkwell scopes
  rule management to Inbox; child-folder filter rules are out of
  scope (§15).
- **No rule import from `.rwz` / `.sieve` files.** Importing Outlook
  legacy rule exports (`.rwz`) or Sieve scripts (RFC 5228) is a
  conversion problem orthogonal to the CRUD surface. v1 ships the
  inkwell-native TOML format only.

## 2. Prior art

### 2.1 Outlook (desktop / web) — Microsoft-native

Microsoft Outlook's rules manager is the **canonical** UI for
`messageRules`. Both desktop and web variants render rules as an
ordered list with per-rule toggle, edit, and delete; rules execute
in `sequence` order on the server (Exchange Transport Rules at
the mailbox-policy boundary plus per-user rules at delivery).

Outlook surfaces 29 predicate fields and 11 action fields
([messageRulePredicates](https://learn.microsoft.com/en-us/graph/api/resources/messagerulepredicates?view=graph-rest-1.0),
[messageRuleActions](https://learn.microsoft.com/en-us/graph/api/resources/messageruleactions?view=graph-rest-1.0)).
The desktop client's classic "Rules and Alerts" dialog is the
fullest UI; New Outlook (the rewrite) trims it. The **"Run Rules
Now"** command (classic only — flagged as missing in New Outlook
through Microsoft Q&A 2025) re-runs selected rules over the
existing Inbox, simulating delivery-time execution post hoc.
Inkwell v1 does **not** ship Run Rules Now (§15).

Outlook treats admin-pushed rules (`isReadOnly = true`, set via
Exchange Transport Rule or tenant policy) as visible but
non-editable. Inkwell mirrors this (§7.3): rules with
`isReadOnly = true` render with a 🔒 indicator and the
`E` / `X` bindings in the manager are no-ops with a friendly toast.

### 2.2 Gmail filters — query-string + apply-label

Gmail's filter UI is a single search-query string (`from:foo AND
has:attachment`) plus a fixed set of actions: skip inbox / apply
label / mark read / forward / delete / never spam / always mark
important / categorise. The Gmail Filters API mirrors this 1:1
([gmail.users.settings.filters](https://developers.google.com/workspace/gmail/api/guides/filter_settings)).

The notable lesson is **action explicitness**: Gmail's actions
include "Skip the Inbox" (= move to Archive folder) as a first-
class checkbox even though it's redundant with "Apply label
'Archive'", because users think in inbox-flow terms, not folder-
state terms. Microsoft's `moveToFolder` action covers the
equivalent on Graph; inkwell's TOML schema exposes
`move = "Archive"` as the canonical phrasing (§6.3) — the
user types a folder name, the loader resolves it to a folder ID.

### 2.3 Thunderbird Message Filters

Thunderbird is the closest GUI analogue to inkwell in editing
posture: cross-platform, keyboard-friendly, IMAP/POP-agnostic. Its
**Message Filters** dialog is the lowest-common-denominator filter
editor: ordered list, Match-all / Match-any conditions, per-rule
move / copy / tag / mark / delete / forward / reply / custom
actions, and a **Run Now** button that applies the filter
retroactively to selected folders.

Thunderbird filters live **client-side** in a per-account `msgFilterRules.dat`
file. They only run while Thunderbird is open. This is the inverse
of Graph's model and the reason inkwell adopts Graph's: rules that
"just work" no matter which client receives the message are more
useful than rules that need a running client.

The lesson inkwell takes from Thunderbird is the **declarative
file-on-disk authoring model** — `msgFilterRules.dat` is text-
based and round-trippable. Inkwell's `rules.toml` is the same idea
in TOML.

### 2.4 mutt / neomutt — no first-class rules

mutt and neomutt have no Inbox-rules feature. Users emulate via
**procmail** (`/etc/procmailrc`, runs at MDA) for local accounts,
**Sieve** (RFC 5228) via ManageSieve (RFC 5804) for IMAP servers
that support it, or **maildrop** for similar. mutt itself is "a
mail viewer, not a mail manager" (see
[Mutt Users archive](https://mutt-users.mutt.narkive.com/IzxLzyUB/procmail-and-sorting-mail-with-imap-folders)).

Microsoft Graph does not expose ManageSieve over its REST surface,
and Sieve and `messageRules` are not semantically equivalent
(Sieve is a Turing-incomplete scripting language; messageRules is
a flat list of predicate-action structs). Inkwell does not ship
Sieve import in v1 (§15).

### 2.5 aerc — `filters` for display, not delivery

aerc has `filters` in `~/.config/aerc/aerc.conf` but they govern
**display-pipeline post-processing** (e.g. "pipe text/html through
w3m"), not delivery-time mail rules. aerc users wanting delivery
rules use the same procmail / Sieve / server-side stack as mutt
users. No precedent for the inkwell rule surface.

### 2.6 himalaya — no rules

himalaya (Rust CLI mail client) ships no rule management. Users
manage rules via Outlook web or the server's native admin UI.

### 2.7 Design decision

Inkwell follows **Outlook** for the **data model** (the Graph
`messageRule` resource is the only source of truth) and
**Thunderbird** for the **authoring model** (declarative
text-on-disk file that round-trips). The synthesis:

- **TOML on disk** — `~/.config/inkwell/rules.toml` is the
  user-editable representation. Same parent path as the spec-27
  `actions.toml` and the spec-11 `saved_searches.toml`. Same
  load-time validation pipeline (§6.4) — load all errors, refuse
  to start on any error (`docs/CONVENTIONS.md` §9).
- **Graph is the truth, local file is the desire** — `inkwell rules
  pull` reads from Graph and rewrites the file; `inkwell rules
  apply` diffs the file against the local mirror (which reflects
  the last-pulled Graph state) and posts the differences. The
  workflow mirrors Terraform / `kubectl apply`: read → edit → apply,
  with `--dry-run` showing the diff first.
- **Local mirror table for offline listing** — a small
  `message_rules` cache table (migration 014, §3) lets `inkwell
  rules list` / `:rules` work offline. Mirror is rebuilt on each
  `pull`; writes go to Graph synchronously and update the mirror
  on success.
- **Curated subset for v1** — predicates and actions outside the
  v1 catalogue (§6.3) are tolerated on **pull** (preserved in the
  mirror's JSON columns and round-tripped on subsequent applies)
  but rejected on **create / edit** from TOML. This avoids the
  loader becoming the implicit Microsoft-rule-language compiler
  and keeps the v1 surface bounded. A rule that contains a
  deferred predicate is rendered with a `[external]` marker in
  the manager and is read-only from inkwell (§7.3).
- **No forward / redirect actions** — `forwardTo`,
  `forwardAsAttachmentTo`, and `redirectTo` are rejected at load
  time (§6.3). Rationale: PRD §3.2 explicitly denies `Mail.Send`,
  and a forward rule is functionally a programmatic-send surface.
  Microsoft itself documents tenant-level mitigations against
  auto-forward as a routine data-exfiltration concern (the
  "All forwarding disabled" outbound spam policy ships ON by
  default in Microsoft 365 since 2020). Inkwell does not provide
  this surface; users with a legitimate auto-forward need
  configure it in Outlook web, outside inkwell, and inkwell shows
  it as read-only.
- **No `permanentDelete` action** — same reasoning as
  `Mail.ReadWrite` triage actions in spec 07: the irreversible
  variant requires explicit user intent per-message via the
  `D` chord (spec 07 §3), not a fire-and-forget delivery-time rule.

The intersection of these choices is small, predictable, and
defensible.

## 3. Storage

### 3.1 Migration `014_message_rules.sql`

Migrations 001–013 are applied on disk under
`internal/store/migrations/` (verified at spec-32 design time:
`013_bundled_senders.sql` is the highest existing slot). Spec 32
claims slot **014**.

**Pre-merge handshake.** The implementer MUST run
`ls internal/store/migrations | sort -n | tail -1` immediately
before opening the PR. If the highest slot is no longer 013, the
implementer:

1. Picks the next free integer (`N` = highest + 1).
2. Renames the migration file to `0NN_message_rules.sql`.
3. Updates the `UPDATE schema_meta SET value = '<N>'` line
   inside the migration (the SQL uses the unpadded integer, e.g.
   `'14'` for slot 014; if renumbering to 015 it becomes `'15'`).
4. Greps `docs/specs/32-server-side-rules/spec.md` for **both** the
   padded slot literal (`014`) and the unpadded SQL literal
   (`'14'`) and replaces every occurrence (this section, §13
   DoD, tests). Use
   `grep -nE "014|'14'" docs/specs/32-server-side-rules/spec.md`
   as the checklist.
5. Greps the test file for `TestMigration014` and renames.

No other file mentions `014`; the handshake is purely textual.

```sql
-- 014_message_rules.sql
CREATE TABLE message_rules (
    account_id      INTEGER NOT NULL
                            REFERENCES accounts(id) ON DELETE CASCADE,
    rule_id         TEXT    NOT NULL CHECK(length(rule_id) > 0),
    display_name    TEXT    NOT NULL,
    sequence_num    INTEGER NOT NULL CHECK(sequence_num >= 0),
    is_enabled      INTEGER NOT NULL CHECK(is_enabled    IN (0,1)),
    is_read_only    INTEGER NOT NULL CHECK(is_read_only  IN (0,1)),
    has_error       INTEGER NOT NULL CHECK(has_error     IN (0,1)),
    conditions_json TEXT    NOT NULL DEFAULT '{}',
    actions_json    TEXT    NOT NULL DEFAULT '{}',
    exceptions_json TEXT    NOT NULL DEFAULT '{}',
    last_pulled_at  INTEGER NOT NULL,   -- unix epoch seconds
    PRIMARY KEY (account_id, rule_id)
);

CREATE INDEX idx_message_rules_sequence
    ON message_rules(account_id, sequence_num);

UPDATE schema_meta SET value = '14' WHERE key = 'version';
```

Column shape rationale:

- `rule_id` is the Graph `id` field
  ([messageRule.id is read-only](https://learn.microsoft.com/en-us/graph/api/resources/messagerule?view=graph-rest-1.0#properties)).
  TEXT because Graph returns opaque base64-ish IDs (e.g.
  `"AQAAAJ5dZqA="`); never assume integer.
- `sequence_num`, not `sequence`, to avoid the SQLite reserved-word
  collision with the `sequence` virtual table extension. The
  Graph field name is preserved on the wire (`sequence` JSON
  key); only the SQL column is renamed.
- `conditions_json` / `actions_json` / `exceptions_json` store the
  full Graph payload as returned by `GET`, verbatim. This is
  intentional: predicates and actions outside the v1-curated
  subset (§6.3) round-trip safely through the mirror without
  inkwell having to model them. The Go layer pretty-prints into
  the typed structs (§4.3) for v1-known fields; unknown JSON keys
  are preserved on update via `PATCH` with the full prior payload
  (§5.3 "PATCH merge semantics").
- `has_error` is a Graph-set read-only flag indicating the rule
  failed evaluation server-side (e.g. moveToFolder target deleted).
  We render it as a `⚠` indicator (§7.2) and surface it in
  `inkwell rules list --output json`.
- `last_pulled_at` lets the manager show "(last pull 3 min ago)"
  in the status hint (§7.5).
- FK `ON DELETE CASCADE` on `account_id` — sign-out + purge drops
  the mirror cleanly. Matches the spec-23 / spec-26 pattern.
- Composite PK `(account_id, rule_id)` makes the schema forward-
  compatible with multi-account (roadmap §1.2) without further
  migration.

### 3.2 Index audit

The mirror is small (typical user has 0–50 rules; tenants with
admin-pushed rules might see ~200). All queries are
account-scoped and either by `rule_id` (PK lookup) or ordered by
`sequence_num` (covered by `idx_message_rules_sequence`).

No FTS5 wiring; rule text is short and the manager's filter input
(`/<pattern>` over rule names) is a Go-side substring match.

No partial index on `is_enabled` — the row count is too low for
the planner to benefit, and the sidebar / manager always renders
disabled rules with a visual marker.

### 3.3 What is NOT cached

- **Rule predicates as separate columns** — we do not normalise the
  29 predicate fields into typed columns. The JSON blob is the
  source of truth in the cache, and the Go layer projects it on
  read (§4.3). Rationale: predicate values are infrequently
  queried by inkwell itself (no "show me all rules that match
  sender X" feature in v1), and a 29-column schema would force a
  migration each time Microsoft adds a predicate.
- **Server-side rule execution history** — Graph's `messageRule`
  resource does not expose an execution log (no
  `lastExecutedAt`, no per-message-id link). Inkwell does not
  fabricate one. If a user wants "did the newsletter rule fire on
  this message?", they read the message's resulting folder /
  categories — which the existing sync engine already reflects.

## 4. Store API

### 4.1 New helpers

```go
// internal/store/rules.go (new file).

// MessageRule mirrors the Graph messageRule resource for v1-known
// fields. The "raw" *Json fields preserve the unparsed Graph payload
// so non-v1 predicates / actions survive round-trips through inkwell.
type MessageRule struct {
    AccountID      int64
    RuleID         string
    DisplayName    string
    Sequence       int    // Graph "sequence" field; SQL column is sequence_num.
    IsEnabled      bool
    IsReadOnly     bool
    HasError       bool

    // Typed projections of the JSON payloads. Only v1-catalogue
    // predicate / action fields are populated; everything else is
    // accessible via the Raw* JSON.
    Conditions     MessagePredicates
    Actions        MessageActions
    Exceptions     MessagePredicates

    RawConditions  json.RawMessage  // full server payload, verbatim
    RawActions     json.RawMessage
    RawExceptions  json.RawMessage

    LastPulledAt   time.Time
}

// ListMessageRules returns the cached rules for an account, ordered
// by sequence. Returns an empty slice (not nil) when no rules cached.
// The caller distinguishes "never pulled" from "pulled and confirmed
// empty" via store.LastRulesPull(ctx, accountID).IsZero():
//   * IsZero()  → never pulled; UI shows the "press P to pull" hint.
//   * !IsZero() AND len(rules) == 0 → server confirmed empty rule set.
func (s *Store) ListMessageRules(
    ctx context.Context, accountID int64,
) ([]MessageRule, error)

// GetMessageRule returns the single cached rule by ID, or
// ErrNotFound when absent.
func (s *Store) GetMessageRule(
    ctx context.Context, accountID int64, ruleID string,
) (MessageRule, error)

// UpsertMessageRule inserts or updates one row. Caller is the pull
// path or the apply path; both paths normalise the typed projection
// before passing the Raw* JSON through. Returns ErrInvalidRuleID
// when ruleID is empty.
func (s *Store) UpsertMessageRule(
    ctx context.Context, r MessageRule,
) error

// UpsertMessageRulesBatch replaces the entire mirror for an account
// in one transaction. Used by the pull path: empty input means
// "the server has zero rules; clear the cache". Returns the count
// of rows written.
func (s *Store) UpsertMessageRulesBatch(
    ctx context.Context, accountID int64, rules []MessageRule,
) (int, error)

// DeleteMessageRule removes one row by ID. 404-on-delete is success
// (idempotent — matches `docs/CONVENTIONS.md` §3 invariant 3 for mutations).
func (s *Store) DeleteMessageRule(
    ctx context.Context, accountID int64, ruleID string,
) error

// LastRulesPull returns the most-recent pulled-at across all rules
// for the account, or zero time if no rule has ever been cached.
// Used by the "(last pull N min ago)" status hint (§7.5).
func (s *Store) LastRulesPull(
    ctx context.Context, accountID int64,
) (time.Time, error)
```

Sentinel errors in `internal/store/errors.go`:

- `ErrInvalidRuleID` — empty `rule_id`; caller bug.

(No `ErrInvalidDestination` analogue here — predicate / action
validation is the loader's job, §6.4, not the store's. The store
trusts what the loader produced; it only rejects PK violations.)

### 4.2 SQL

```sql
-- ListMessageRules
SELECT account_id, rule_id, display_name, sequence_num,
       is_enabled, is_read_only, has_error,
       conditions_json, actions_json, exceptions_json,
       last_pulled_at
  FROM message_rules
 WHERE account_id = :account_id
 ORDER BY sequence_num ASC, rule_id ASC;
```

`sequence_num ASC, rule_id ASC` — secondary order on `rule_id`
ensures stable output when two rules share a sequence number
(Graph allows it; rare but observed).

```sql
-- UpsertMessageRule (INSERT ... ON CONFLICT DO UPDATE).
INSERT INTO message_rules
  (account_id, rule_id, display_name, sequence_num,
   is_enabled, is_read_only, has_error,
   conditions_json, actions_json, exceptions_json,
   last_pulled_at)
VALUES (:account_id, :rule_id, :display_name, :sequence_num,
        :is_enabled, :is_read_only, :has_error,
        :conditions_json, :actions_json, :exceptions_json,
        :last_pulled_at)
ON CONFLICT (account_id, rule_id) DO UPDATE SET
    display_name    = excluded.display_name,
    sequence_num    = excluded.sequence_num,
    is_enabled      = excluded.is_enabled,
    is_read_only    = excluded.is_read_only,
    has_error       = excluded.has_error,
    conditions_json = excluded.conditions_json,
    actions_json    = excluded.actions_json,
    exceptions_json = excluded.exceptions_json,
    last_pulled_at  = excluded.last_pulled_at;
```

`UpsertMessageRulesBatch` does the replace-all under one
transaction: `DELETE FROM message_rules WHERE account_id = ?` then
multi-row INSERT. The DELETE-then-INSERT pattern is used (rather
than a per-row UPSERT with a sentinel sweep) so a rule that was
deleted server-side disappears locally without inkwell needing to
track tombstones — the server is the truth.

### 4.3 Typed projection of conditions and actions

```go
// internal/store/rules_types.go

// MessagePredicates models the v1 catalogue subset of
// messageRulePredicates. Fields outside the catalogue (§6.3) are
// preserved in MessageRule.RawConditions but not surfaced here.
//
// Pointer types distinguish "field unset" (nil) from "field set
// to zero" (e.g. *bool false). Graph itself treats absent fields
// as "no constraint"; we preserve that.
type MessagePredicates struct {
    BodyContains          []string  `json:"bodyContains,omitempty"`
    BodyOrSubjectContains []string  `json:"bodyOrSubjectContains,omitempty"`
    SubjectContains       []string  `json:"subjectContains,omitempty"`
    HeaderContains        []string  `json:"headerContains,omitempty"`
    FromAddresses         []Recipient `json:"fromAddresses,omitempty"`
    SenderContains        []string  `json:"senderContains,omitempty"`
    SentToAddresses       []Recipient `json:"sentToAddresses,omitempty"`
    RecipientContains     []string  `json:"recipientContains,omitempty"`
    SentToMe              *bool     `json:"sentToMe,omitempty"`
    SentCcMe              *bool     `json:"sentCcMe,omitempty"`
    SentOnlyToMe          *bool     `json:"sentOnlyToMe,omitempty"`
    SentToOrCcMe          *bool     `json:"sentToOrCcMe,omitempty"`
    NotSentToMe           *bool     `json:"notSentToMe,omitempty"`
    HasAttachments        *bool     `json:"hasAttachments,omitempty"`
    Importance            string    `json:"importance,omitempty"`   // "low"|"normal"|"high"
    Sensitivity           string    `json:"sensitivity,omitempty"`  // "normal"|"personal"|"private"|"confidential"
    WithinSizeRange       *SizeKB   `json:"withinSizeRange,omitempty"`
    Categories            []string  `json:"categories,omitempty"`
    IsAutomaticReply      *bool     `json:"isAutomaticReply,omitempty"`
    IsAutomaticForward    *bool     `json:"isAutomaticForward,omitempty"`
    MessageActionFlag     string    `json:"messageActionFlag,omitempty"`
}

// MessageActions models the v1 catalogue subset of messageRuleActions.
type MessageActions struct {
    MarkAsRead          *bool     `json:"markAsRead,omitempty"`
    MarkImportance      string    `json:"markImportance,omitempty"`   // "low"|"normal"|"high"
    MoveToFolder        string    `json:"moveToFolder,omitempty"`     // Graph folder ID
    CopyToFolder        string    `json:"copyToFolder,omitempty"`     // Graph folder ID
    AssignCategories    []string  `json:"assignCategories,omitempty"`
    Delete              *bool     `json:"delete,omitempty"`           // → Deleted Items
    StopProcessingRules *bool     `json:"stopProcessingRules,omitempty"`
}

// SizeKB matches Graph's sizeRange in kilobytes.
type SizeKB struct {
    MinimumSize int `json:"minimumSize"`
    MaximumSize int `json:"maximumSize"`
}

// Recipient mirrors graph.Recipient (existing in internal/graph).
// Duplicated minimally here because per `docs/CONVENTIONS.md` §2 layering
// `store` and `graph` are sibling lower-tier packages and cannot
// import each other; the typed values flow up to `internal/rules`
// (a middle-tier consumer), which converts between the two.
type Recipient struct {
    EmailAddress EmailAddress `json:"emailAddress"`
}
type EmailAddress struct {
    Address string `json:"address"`
    Name    string `json:"name,omitempty"`
}
```

The full Graph predicate / action set (29 + 11 fields, per
[messageRulePredicates](https://learn.microsoft.com/en-us/graph/api/resources/messagerulepredicates?view=graph-rest-1.0)
and
[messageRuleActions](https://learn.microsoft.com/en-us/graph/api/resources/messageruleactions?view=graph-rest-1.0))
remains accessible via the Raw* JSON columns. A rule with
non-v1 fields is read-only from inkwell (§7.3) and round-trips
unchanged on subsequent applies that don't touch it.

## 5. Graph API

### 5.1 Endpoints used

| Operation                          | Method + URL                                            | Body                                                 | Notes |
|------------------------------------|---------------------------------------------------------|------------------------------------------------------|-------|
| List rules                         | `GET /me/mailFolders/inbox/messageRules`                | —                                                    | Returns `{ value: [messageRule, …] }`. Single page in practice (rule limits are low; no `$top` / `$skip` needed). |
| Get one rule                       | `GET /me/mailFolders/inbox/messageRules/{id}`           | —                                                    | Used by `inkwell rules get <id>` (`--output json`). |
| Create rule                        | `POST /me/mailFolders/inbox/messageRules`               | full messageRule JSON minus read-only fields         | Returns `201 Created` + the new rule. Required scope `MailboxSettings.ReadWrite` (already requested). |
| Update rule                        | `PATCH /me/mailFolders/inbox/messageRules/{id}`         | full messageRule JSON for v1-known fields + RawConditions/RawActions/RawExceptions for non-v1 round-trip | See §5.3 "PATCH merge semantics". |
| Delete rule                        | `DELETE /me/mailFolders/inbox/messageRules/{id}`        | —                                                    | 404-on-delete treated as success. |

All requests use the existing transport stack
(`internal/graph/client.go`): `loggingTransport → throttleTransport
→ authTransport`. No new transport tier; rule traffic is low-volume
and benefits from the same 429 retry / Retry-After honour as the
rest of the API surface.

### 5.2 New graph client surface

```go
// internal/graph/rules.go (new file).

// ListMessageRules returns every Inbox rule for the signed-in user.
// Single GET; Graph does not paginate this endpoint in practice.
func (c *Client) ListMessageRules(ctx context.Context) ([]MessageRule, error)

// GetMessageRule fetches one rule by ID. Returns *GraphError with
// StatusCode == 404 when the rule is missing.
func (c *Client) GetMessageRule(ctx context.Context, ruleID string) (MessageRule, error)

// CreateMessageRule posts a new rule. The server assigns the ID;
// the returned MessageRule has it populated.
func (c *Client) CreateMessageRule(ctx context.Context, r MessageRule) (MessageRule, error)

// UpdateMessageRule patches a rule by ID. PATCH semantics are
// "merge top-level fields"; see §5.3 below. Returns the updated rule.
func (c *Client) UpdateMessageRule(ctx context.Context, ruleID string, r MessageRule) (MessageRule, error)

// DeleteMessageRule deletes a rule by ID. 404 is treated as success
// (idempotent).
func (c *Client) DeleteMessageRule(ctx context.Context, ruleID string) error
```

`MessageRule` here is `graph.MessageRule`, a value type with the
same field layout as `store.MessageRule` minus the SQL-specific
fields (`AccountID`, `LastPulledAt`). The `graph` package owns the
JSON marshal/unmarshal for the wire format; the `store` package
owns the SQLite serialization.

### 5.3 PATCH merge semantics

Microsoft Graph documents PATCH on the `messageRule` resource as
JSON-merge-patch at the **top-level property** of the resource.
The behaviour of nested complex types (`conditions`, `actions`,
`exceptions`) is **not** consistently documented as "deep merge"
— the [Update messageRule reference](https://learn.microsoft.com/en-us/graph/api/messagerule-update?view=graph-rest-1.0)
shows examples that send the full sub-object, and tenant
observations (Microsoft Q&A 2022, Graph SDK GitHub issue #1234)
confirm that sending a partial `conditions` object can wipe other
predicate fields. Inkwell treats this **conservatively as
replace-only** at the sub-object level:

- Sending `{"conditions": {...}}` replaces the entire conditions
  object. Any non-v1 predicate fields the prior version had are
  lost UNLESS the inkwell-side merge (§5.3 below) re-injected them
  before the PATCH.
- Sending `{"displayName": "..."}` alone changes only the name.

Consequence for inkwell: when applying a user's TOML edit, we MUST
merge the user's v1-catalogue fields into the prior `conditions_json`
(from the local mirror) so non-v1 fields survive. The merge is
performed by the apply pipeline (§6.5) on the JSON-string level
**before** the PATCH body is built:

```go
// internal/customaction-equivalent pseudo for rules:
prior := store.GetMessageRule(ctx, accountID, ruleID)
mergedConditions := jsonMerge(prior.RawConditions, marshal(edit.Conditions))
patchBody := map[string]any{
    "displayName": edit.DisplayName,
    "sequence":    edit.Sequence,
    "isEnabled":   edit.IsEnabled,
    "conditions":  mergedConditions,
    "actions":     jsonMerge(prior.RawActions,    marshal(edit.Actions)),
    "exceptions":  jsonMerge(prior.RawExceptions, marshal(edit.Exceptions)),
}
```

`jsonMerge` deep-merges two JSON objects at the top level, with the
"edit" side winning per-key. This preserves non-v1 keys verbatim on
update and is the **only** correct behaviour given the curated-
subset design (§2.7). The merge logic lives in
`internal/graph/rules_merge.go` with unit tests covering: empty
prior, empty edit, conflicting key, nested object replace (Graph's
PATCH does not deep-merge `conditions` itself — replacement is
top-level), array replacement (`bodyContains: [...]` always
replaces rather than appends).

### 5.4 ETag / conflict handling

Graph does **not** issue ETags on `messageRule` resources (verified
against the v1.0 schema). There is no `If-Match` header for
optimistic concurrency. Concurrent edits from two clients (e.g.
inkwell + Outlook web) result in last-write-wins on the server.

Inkwell's mitigation:

- The apply path always re-pulls the rule by ID immediately before
  the PATCH (`graph.GetMessageRule`), diffs against the user's
  intended edit, and surfaces a "rule has changed on the server
  since last pull — re-pull before retrying" error if the
  freshly-fetched content differs from the local mirror at the
  time the user opened the editor. *Updated* is not a Graph-exposed
  field; the comparison is the SHA-256 of a **canonical JSON
  encoding** of `(displayName, sequence, isEnabled, conditions,
  actions, exceptions)`. Canonical = `encoding/json.Marshal` over a
  struct with sorted keys (helper `canonicalJSON` in a new
  `internal/graph/canonical_json.go` file; identical key ordering
  for `map[string]any` values via a sorted-key recursive walk).
  A round-trip test (`TestCanonicalJSONStableAcrossUnmarshalCycle`)
  confirms that decoding and re-encoding a Graph payload twice
  produces identical bytes.
- The dry-run (`inkwell rules apply --dry-run`) shows the same
  conflict if detected.
- The race window is small (one round-trip) but not zero. v1
  accepts it; a future spec could integrate `If-Match` if Graph
  adds it.

## 6. The `rules.toml` file

### 6.1 Default path and overrides

Default: `~/.config/inkwell/rules.toml` (XDG-style; same parent as
`config.toml`, `saved_searches.toml`, and `actions.toml`). The
path is overridable via `[rules].file` in the main `config.toml`
(§11). Missing file is **not** an error; an empty catalogue
behaves identically to `rules = []`.

### 6.2 Example file

```toml
# rules.toml — managed by `inkwell rules pull` and `inkwell rules
# apply`. Hand-edits are welcome; run `inkwell rules apply --dry-run`
# before pushing.

[[rule]]
id            = "AQAAAJ5dZqA="  # set by the server on first pull
name          = "Newsletters → Feed folder"
sequence      = 10
enabled       = true

  [rule.when]
  sender_contains = ["newsletter@", "no-reply@"]
  header_contains = ["List-Unsubscribe"]

  [rule.then]
  move          = "Folders/Newsletters"
  mark_read     = true
  stop          = true

[[rule]]
name          = "Receipts → Paper Trail"
sequence      = 20
enabled       = true

  [rule.when]
  from = [
      { address = "receipts@amazon.com" },
      { address = "billing@stripe.com" },
  ]

  [rule.then]
  move          = "Folders/Paper Trail"
  add_categories = ["Receipt"]

[[rule]]
name          = "Boss → mark important"
sequence      = 30
enabled       = true

  [rule.when]
  from = [{ address = "boss@example.invalid" }]

  [rule.then]
  mark_importance = "high"
```

Notes on the example:

- `id` is omitted for new rules (the server assigns one on
  `apply`); inkwell rewrites the file in place on next `pull` to
  include the assigned ID. Editing IDs by hand is allowed (you'd
  point at a different server rule) but discouraged — easier to
  delete + re-create.
- `name` is the inkwell-facing display name and is mapped to
  Graph's `displayName`. Required.
- `sequence` is the Graph `sequence` field (int32, ordering at
  delivery). Required.
- `enabled` maps to `isEnabled`. Optional; default `true`.
- `[rule.when]` is the inkwell name for `conditions`. Fields use
  inkwell-idiomatic snake_case names that map 1:1 to Graph's
  camelCase (`sender_contains` ↔ `senderContains`; full map in
  §6.3).
- `[rule.then]` is the inkwell name for `actions`. Field mapping in
  §6.3.
- The TOML `from = [{ address = "..." }]` shape mirrors Graph's
  `fromAddresses` (array of recipient objects). Inkwell's loader
  also accepts the shorthand `from = ["addr1", "addr2"]` and
  expands each string to a `{address: ..., name: ""}` recipient
  on serialise — saves the user typing.
- `stop` is the inkwell name for `stopProcessingRules`. Boolean,
  default `false`.
- Exceptions: `[rule.except]` mirrors `[rule.when]` with the same
  predicate vocabulary. Empty by default.
- Multi-line TOML preferred for readability; inkwell's `pull`
  writes consistent formatting (BurntSushi/toml encoder defaults
  with two-space sub-table indent — same convention as the
  spec-11 `saved_searches.toml`).

### 6.3 Predicate / action catalogue (v1)

**Boolean semantics within `[rule.when]`.** Microsoft Graph
`messageRulePredicates` are evaluated per-field by the server with
this contract:

- **Items inside a single predicate's list are OR'd.** For example
  `sender_contains = ["newsletter@", "no-reply@"]` matches if
  *either* substring appears in the sender. `from = [r1, r2]`
  matches if the message is from r1 *or* r2.
- **Different predicates AND together.** `sender_contains` AND
  `header_contains` AND `has_attachments` all narrow.
- **`[rule.except]` is a top-level negation.** A message matches
  the rule iff `when` matches AND `except` does *not* match. Items
  inside `except` follow the same OR-within / AND-across rules.

The §7.2.2 viewer renders predicates with this grammar: each
predicate line shows OR commas between items; multiple lines are
joined with `AND` words. The §6.2 example file pre-comments this
once near the top of the generated file via the §6.5 pull
round-trip.

#### Predicates (`[rule.when]` / `[rule.except]`)

| TOML key                 | Graph field             | Type            | Notes |
|--------------------------|-------------------------|-----------------|-------|
| `body_contains`          | `bodyContains`          | `[]string`      | Case-insensitive on server. |
| `body_or_subject_contains` | `bodyOrSubjectContains` | `[]string`     | |
| `subject_contains`       | `subjectContains`       | `[]string`      | |
| `header_contains`        | `headerContains`        | `[]string`      | Matches raw RFC 5322 header lines. |
| `from`                   | `fromAddresses`         | `[]Recipient`   | Shorthand strings expand to `{address:s, name:""}`. |
| `sender_contains`        | `senderContains`        | `[]string`      | Substring against the From display name. |
| `sent_to`                | `sentToAddresses`       | `[]Recipient`   | |
| `recipient_contains`     | `recipientContains`     | `[]string`      | Substring against To / CC display names. |
| `sent_to_me`             | `sentToMe`              | `bool`          | |
| `sent_cc_me`             | `sentCcMe`              | `bool`          | |
| `sent_only_to_me`        | `sentOnlyToMe`          | `bool`          | |
| `sent_to_or_cc_me`       | `sentToOrCcMe`          | `bool`          | |
| `not_sent_to_me`         | `notSentToMe`           | `bool`          | |
| `has_attachments`        | `hasAttachments`        | `bool`          | |
| `importance`             | `importance`            | `"low"\|"normal"\|"high"` | |
| `sensitivity`            | `sensitivity`           | `"normal"\|"personal"\|"private"\|"confidential"` | |
| `size_min_kb`            | `withinSizeRange.minimumSize` | `int`     | KB; defaults to `0` if `size_max_kb` set alone. Range `[0, 2_097_151]` (Graph's documented int32 KB ceiling ≈ 2 GiB). |
| `size_max_kb`            | `withinSizeRange.maximumSize` | `int`     | KB; defaults to `2_097_151` if `size_min_kb` set alone. Same range. |
| `categories`             | `categories`            | `[]string`      | Already-applied categories (rare in pre-delivery rules; valid for rules running after labelling). |
| `is_automatic_reply`     | `isAutomaticReply`      | `bool`          | |
| `is_automatic_forward`   | `isAutomaticForward`    | `bool`          | |
| `flag`                   | `messageActionFlag`     | enum            | One of: `"any"`, `"call"`, `"doNotForward"`, `"followUp"`, `"fyi"`, `"forward"`, `"noResponseNecessary"`, `"read"`, `"reply"`, `"replyToAll"`, `"review"`. Loader rejects any other string with a pointed error citing this closed set. |

**Deferred predicates** (rejected on create / edit with a load-
time error; preserved on round-trip via `RawConditions`):

- `isApprovalRequest`, `isMeetingRequest`, `isMeetingResponse`,
  `isNonDeliveryReport`, `isPermissionControlled`, `isReadReceipt`,
  `isSigned`, `isVoicemail`, `isEncrypted`.

Rationale: low-frequency, opaque to most users, and outside the
"durable Outlook user expectations" baseline of Outlook desktop
classic's rule wizard. A user who needs one configures it in
Outlook web; inkwell shows the rule as read-only (§7.3).

#### Actions (`[rule.then]`)

| TOML key          | Graph field           | Type           | Notes |
|-------------------|-----------------------|----------------|-------|
| `mark_read`       | `markAsRead`          | `bool`         | |
| `mark_importance` | `markImportance`      | `"low"\|"normal"\|"high"` | |
| `move`            | `moveToFolder`        | folder-path (string) | Resolved to a Graph folder ID at apply time via `internal/graph/folders.go` (path-walk; nested folders separated by `/`). Apply fails with a friendly error if the path does not resolve. |
| `copy`            | `copyToFolder`        | folder-path (string) | Same resolution. |
| `add_categories`  | `assignCategories`    | `[]string`     | |
| `delete`          | `delete`              | `bool`         | Soft-delete to Deleted Items. Requires `confirm = "always"` in the rule block (§6.4). |
| `stop`            | `stopProcessingRules` | `bool`         | |

**Deferred actions** (rejected on create / edit with a load-time
error; preserved on round-trip):

- `forwardTo`, `forwardAsAttachmentTo`, `redirectTo` (PRD §3.2
  `Mail.Send` denial — see §2.7).
- `permanentDelete` (irreversible; `docs/CONVENTIONS.md` §7 rule 9
  "Confirmation gates for destructive actions" requires per-
  message intent, not fire-and-forget delivery-time rules).

Rules containing any deferred field on the **server side** are
displayed in inkwell with an `[external]` marker (§7.3) and are
read-only — the user retains them via the normal pull/apply cycle
but cannot edit them from inkwell. To edit, the user opens Outlook
web.

### 6.4 Loader contract

```go
// internal/rules/loader.go (new package).

// LoadCatalogue parses rules.toml from path, validates every rule,
// resolves folder paths to IDs (using a folder-lookup function the
// caller provides), and returns the catalogue. If the file does not
// exist, returns an empty catalogue with nil err. Any validation
// failure returns a multi-error with file:line for each invalid
// entry; the binary refuses to apply (`docs/CONVENTIONS.md` §9 invalid config
// philosophy: hard fail, no partial apply).
//
// Folder resolution is deferred to apply time (the folder may not
// be in the local mirror yet at start). The loader only validates
// **syntactic** correctness:
//   * Each [[rule]] has name, sequence, [rule.then] with ≥ 1 action.
//   * Field names map to v1-catalogue keys (§6.3); typos rejected.
//   * Deferred predicates / actions rejected with a pointed error.
//   * `delete = true` requires `confirm = "always"` (safety gate;
//     see below). `confirm` is the spec-27 ConfirmPolicy tri-state.
//   * Sequence numbers are non-negative; duplicates allowed
//     (Graph allows it; the manager renders them with a ⚠ on
//     the second-onwards row).
func LoadCatalogue(ctx context.Context, path string) (*Catalogue, error)

type Catalogue struct {
    Rules []RuleDraft  // pre-validated, awaiting folder resolution at apply time.
}

type RuleDraft struct {
    ID              string           // empty on new rules
    Name            string
    Sequence        int
    Enabled         bool
    Confirm         customaction.ConfirmPolicy  // reused from spec 27 §3.2:
                                                // ConfirmAuto | ConfirmAlways | ConfirmNever
                                                // Default ConfirmAuto. Required = ConfirmAlways
                                                // when Then.Delete is true (§6.4 gate).
    When            store.MessagePredicates
    Then            store.MessageActions
    Except          store.MessagePredicates

    SourcePos       int              // line in rules.toml; used for error messages
}
```

The `confirm` field at the rule level (default `"auto"`) is the
**destructive-action gate** and reuses the spec-27 `ConfirmPolicy`
tri-state (`"auto" | "always" | "never"`). Any rule with `delete =
true` in `[rule.then]` MUST set `confirm = "always"` at the top of
the `[[rule]]` block. Without it, the loader rejects:

```
rules.toml:42: rule "Spam → trash" has destructive action `delete`
without `confirm = "always"` at the [[rule]] level. Add the field
to acknowledge intent (`"never"` is rejected for any rule containing
`delete = true`; matches spec 27 §3.4 destructive-op rule).
```

The same gate applies during `inkwell rules apply` interactive
mode:

- `confirm = "always"` rules — prompt Y/N every apply, *unless*
  `--yes` is passed.
- `confirm = "auto"` (default) rules — prompt iff the rule contains
  a destructive action (matches spec 27 §3.4 auto-policy).
- `confirm = "never"` rules — apply silently. Forbidden when
  destructive (load-time error per above).

### 6.5 Apply pipeline

`inkwell rules apply` is the only path that writes to Graph. The
pipeline (linear, all-or-nothing per rule):

1. **Pull current state.** Call `graph.ListMessageRules(ctx)`;
   update the local mirror atomically. This ensures `apply` sees
   the freshest view of the server, narrowing the §5.4
   conflict window.
2. **Load TOML.** Run the §6.4 loader; abort on any load-time
   error (no partial apply).
3. **Resolve folder paths.** For every `move` / `copy` action,
   look up the Graph folder ID via the local folders cache. The
   helper `GetFolderByPath(ctx, accountID, slashPath) (Folder,
   error)` is **new** in `internal/store/folders.go` (existing
   surface has `ListFolders`, `GetFolderByWellKnown`,
   `UpsertFolder`, but no path-walker). Path syntax is
   `<parent>/<child>/<leaf>` with `/` separator, NFC-normalised
   before comparison (§9 Unicode edge case). The helper walks
   the cached `folders` tree by `display_name` at each level,
   returning `ErrFolderNotFound` on a miss. Apply aborts the
   offending rule (other rules unaffected) on unresolved paths.

   **Retargeting warning.** If the resolved folder ID differs from
   the prior `moveToFolder` / `copyToFolder` ID stored in the
   local mirror (e.g., the user renamed a folder in Outlook web
   and a different folder now lives at the old slash-path),
   apply emits a one-line warning to the diff output and the
   manager toast: `⚠ rule "X" retargets <action> from /Old to
   /New (folder rename detected). Confirm before --yes.` The
   warning fires *before* the confirmation prompt so the user
   sees it.
4. **Compute diff.** Match TOML rules to server rules by `id`
   (when set) or by `name` (when ID empty — a new rule). Classify
   each into: **create** (TOML has no ID, no name match server-
   side), **update** (TOML+server both present, content differs),
   **noop** (content identical), **delete** (server has rules that
   the TOML does not). Server-side `isReadOnly = true` rules are
   **skipped entirely** in the diff — they are pulled into the
   mirror, surfaced in `:rules`, but neither updated nor deleted
   by `apply`.
5. **Confirmation gates.** Per the §6.4 `confirm` tri-state: prompt
   Y/N for every rule where the gate fires (or skip if `--yes`).
   If `--dry-run`, print the diff and exit 0 without prompting.
6. **Execute.** Order: `delete` operations first, then `create`,
   then `update`. The order is for **idempotency and
   debuggability** (a partial-apply failure midway through is
   easier to reason about when deletions have already happened
   than when creates have raced against deletes), not for any
   server-side uniqueness constraint — Graph permits duplicate
   `displayName` values. Each Graph call uses the existing client
   (synchronous, throttle-aware). On any failure, **stop** and
   surface the partial-apply state: rules already applied stay
   applied; the failing rule is reported with the Graph error;
   subsequent rules are unprocessed. The local mirror is re-pulled
   at the end regardless of success to reconcile state.
7. **Round-trip.** On full success, re-pull the rules list and
   rewrite `rules.toml` with the canonical server state
   (preserving non-v1 fields via the Raw* round-trip). The rewrite
   is **atomic**: write the new content to `rules.toml.tmp` in
   the same directory (same filesystem — `os.Rename` fails
   across mounts), `fsync`, then `os.Rename` over the original.
   Power-loss mid-rewrite leaves the prior `rules.toml` intact
   (matches spec 23 / spec 27's TOML-rewrite contract). On any
   write / fsync error, the tmp file is `os.Remove`d before the
   function returns so the user's directory does not accumulate
   `rules.toml.tmp` orphans across failures (a
   `defer cleanupTmp(path)` registered at write start, cleared
   on successful rename). Tests:
   `TestPullAtomicRewriteSurvivesInterruption` uses a fault-
   injecting writer to confirm the prior file content survives a
   simulated crash between `fsync` and `Rename`;
   `TestPullAtomicRewriteCleansUpTmpOnFailure` confirms the
   orphan-cleanup `defer` fires on write error. This is the
   equivalent of Terraform's "state matches reality after apply"
   guarantee.

The apply pipeline lives in a new `internal/rules` package, not in
`internal/action`: rules CRUD is not part of the message-mutation
queue. The `action.Executor` is for per-message Graph calls
optimistically applied to the local mirror; rule CRUD is a
synchronous configuration surface with its own confirmation gates,
its own diff, and no optimistic local apply.

## 7. UI

### 7.1 Entry points

The user surfaces a rules manager via one of three paths:

- `:rules` (cmd-bar verb) — opens the manager modal (§7.2). No
  argument required; `:rules` is a synonym for `:rules list`.
- `:rules <subverb>` (cmd-bar) — runs the same CLI verb the
  `inkwell rules` subcommand exposes (§8.1). Output flows to the
  status bar (success toast) or the manager modal (`list`).
- Command palette (spec 22) — static palette rows
  `rules_open`, `rules_pull`, `rules_apply`, `rules_dry_run`,
  `rules_new` (§7.6).

There is no chord prefix. Rules management is rare relative to
triage; not worth a top-level binding. The cmd-bar verb and palette
are the only entry surfaces.

### 7.2 The manager modal

`:rules` opens a **modal pane** rendered as a centered overlay
sized to `min(80, terminal_width − 4)` columns × `min(20,
terminal_height − 6)` rows. The width-clamp matches the
spec-22 palette overlay's responsive sizing. Below 60 columns the
table collapses to a two-column view (`# En  Name`); the
`When → Then` column is hidden until the user presses `Enter` on a
row (the view pane then renders at full content width). Mode
constant: `MessageRulesMode` (new), added alongside `NormalMode` /
`CommandMode` / `PaletteMode` in `internal/ui/messages.go`. The
modal owns the keyboard until dismissed. A `WindowSizeMsg` during
modal display triggers an immediate re-layout per `docs/CONVENTIONS.md` §4.

> **Naming note.** `internal/ui/messages.go:46` already defines
> `RuleEditMode` for the spec 11 saved-search edit modal. The new
> mode is deliberately `MessageRulesMode` (full noun-phrase) to
> disambiguate from the saved-search `Rule*` namespace at grep
> time. Implementers MUST NOT shorten to `RulesMode`.

```
 ╭─ Rules ────────────────────────────────────────────────────────────────────╮
 │  #  En  Name                              When → Then              Flags    │
 │  10 ✓  Newsletters → Feed folder         from: newsletter@…        ✓ stop  │
 │  20 ✓  Receipts → Paper Trail            from: 2 senders                   │
 │  30 ✓  Boss → mark important             from: boss@example.invalid        │
 │  40 ⊘  Old vacation auto-archive         is_automatic_reply        🔒      │
 │  50 ✓  Block "Acme promotions"           subject: "Acme promo…"    [ext]   │
 │                                                                             │
 │  Last pull: 3 min ago · 5 rules · 1 read-only · 0 with errors              │
 │  [j/k] move  [Enter] view  [N]ew  [E]dit  [X]elete  [T]oggle  [P]ull       │
 │  [J/K] reorder  [/] filter  [a] apply (dry-run) [A] apply  [Esc] close     │
 ╰─────────────────────────────────────────────────────────────────────────────╯
```

#### 7.2.1 Modal bindings

| Key            | Action                                                              |
|----------------|---------------------------------------------------------------------|
| `j` / `k`      | Move selection.                                                     |
| `Enter`        | Open the **view** pane for the focused rule (read-only details).    |
| `N`            | Create a new rule — opens `$EDITOR` on a stub rule block in `rules.toml`. |
| `E`            | Edit the focused rule — opens `$EDITOR` on the rule's block in `rules.toml`. |
| `X`            | Delete the focused rule. Confirm modal mandatory.                   |
| `T`            | Toggle the focused rule's `enabled`. Synchronous PATCH; result toast.|
| `J` / `K` (cap)| Reorder: swap `sequence` with the next/prev rule. Two sequential PATCHes (first PATCH leaves a transient duplicate `sequence` value, which Graph permits — see §9 "Two rules share a sequence value"; second PATCH restores uniqueness). |
| `P`            | Pull from Graph (refresh the mirror + the modal contents).          |
| `/`            | Open a filter input — substring match over rule names.              |
| `a` (lower)    | `apply --dry-run` — show the diff in a side pane.                   |
| `A` (caps)     | `apply` — confirm + push to Graph.                                  |
| `Esc`          | Close the modal. Unsaved TOML edits in $EDITOR remain on disk.      |

Bindings live in a new `KeyMap` group (no chord prefix, mode-
scoped). The dispatcher checks `m.mode == MessageRulesMode` before any
other dispatch — `j` outside the modal moves the list cursor; `j`
inside the modal moves the rules selection. Same pattern as
spec 22's `PaletteMode`.

#### 7.2.2 The view pane

`Enter` on a rule swaps the manager body for a read-only details
pane:

```
 ╭─ Rule "Newsletters → Feed folder" ─────────────────────────────────────────╮
 │  ID:        AQAAAJ5dZqA=                                                   │
 │  Sequence:  10                                                             │
 │  Enabled:   yes                                                            │
 │                                                                             │
 │  When (predicates AND together; commas within a line OR):                  │
 │    sender contains: "newsletter@" or "no-reply@"                           │
 │    header contains: "List-Unsubscribe"                                     │
 │                                                                             │
 │  Then:                                                                     │
 │    move        → Folders/Newsletters                                       │
 │    mark_read   = true                                                      │
 │    stop                                                                    │
 │                                                                             │
 │  Last pulled:  3 minutes ago                                               │
 │                                                                             │
 │  [E]dit  [X]elete  [T]oggle  [Esc] back                                    │
 ╰─────────────────────────────────────────────────────────────────────────────╯
```

The view pane reads exclusively from the local mirror; no Graph
call. Round-tripping non-v1 fields are listed under
`Extra (managed in Outlook web):` and rendered greyed-out.

### 7.3 Read-only rules

A rule is **inkwell-read-only** when **any** of:

- `is_read_only = true` (admin / Exchange Transport Rule).
- Conditions or actions contain a deferred-catalogue field
  (§6.3): inkwell will not edit a rule it can't fully express in
  the TOML format.
- `has_error = true` (server flagged the rule as broken — edit it
  in Outlook web).

Read-only rules render with one of three indicators in the manager:
- `🔒` for `is_read_only` (admin),
- `[ext]` for deferred-field content,
- `⚠` for `has_error`.

(`[rules].ascii_fallback = true` substitutes the non-ASCII glyphs:
`🔒 → RO`, `⚠ → ERR`. `[ext]` is already ASCII and renders
unchanged in both modes; the fallback toggle does not affect it.
See §11.)

`E`, `X`, `T`, `J`, `K` on a read-only rule produce a toast:
- 🔒: `cannot edit: rule is admin-managed (set via Exchange policy)`.
- `[ext]`: `cannot edit: rule uses fields outside inkwell's v1
  catalogue. Edit it at outlook.office.com.`
- `⚠`: `rule has a server-side error. Edit it at outlook.office.com
  to repair.`

`P` (pull) and the read-only viewer are always available.

### 7.4 Edit flow (`E` / `N`)

`E` and `N` suspend the TUI to `$EDITOR` (same pattern as spec 15
compose, which is the shipped precedent; spec 13's OOO `$EDITOR`
path is documented but unshipped residual at the time of writing
and is not the load-bearing reference):

1. The current `rules.toml` is opened in the user's `$EDITOR`. For
   `E`, the cursor is positioned on the focused rule's `[[rule]]`
   line (`+<line>` argument to vim/nano/helix; same as spec 15's
   `$EDITOR` invocation).
2. For `N`, a stub rule block is inserted at the end of the file
   (or at line 1 if empty):
   ```toml
   [[rule]]
   name      = "New rule"
   sequence  = <maxExistingSeq + 10>
   enabled   = true

     [rule.when]
     # …

     [rule.then]
     mark_read = true
   ```
3. On editor exit, inkwell re-loads the file via the §6.4 loader.
   Load errors are shown in a side modal; the user can re-edit
   (Enter) or discard (Esc).
4. The loaded catalogue replaces the in-memory draft; the manager
   refreshes from the file.
5. Edits are **not yet applied to Graph**. The user invokes `a`
   (dry-run) or `A` (apply) explicitly.

This three-phase model (load → diff → apply) is identical to
Terraform's `terraform plan` / `apply` split. It is intentional:
rules ARE infrastructure; explicit confirmation before pushing to
Graph is the safety contract.

### 7.5 Status hints

The modal header shows:

```
Last pull: 3 min ago · 5 rules · 1 read-only · 0 with errors
```

When `Last pull` is older than the `[rules].pull_stale_threshold`
(default `1h`), the value is rendered in `theme.WarningEmphasis`
to nudge the user to `P`. Status-bar toasts outside the modal use
the standard `m.Status` field with `TransientStatusTTL` (existing
spec 04 transient hint).

On `apply`:
- Success: `✓ rules applied: 1 created, 2 updated, 1 deleted`.
- Failure: `✗ rules apply failed at "Boss → mark important": 403
  (rule limit exceeded)`.
- Partial: `⚠ rules apply partial: 1 created, 1 failed; pull state
  may be stale — press P to refresh`.

### 7.6 Command palette rows

`internal/ui/palette_commands.go` gains five static rows (spec 22
§4.2 static-row pattern; matches spec 23's five routing rows and
spec 27's `:actions` row):

| `ID`              | Title                                  | `Binding` | Section          |
|-------------------|----------------------------------------|-----------|------------------|
| `rules_open`      | "Manage server rules…"                 | `:rules`  | `sectionCommands` |
| `rules_pull`      | "Rules: pull from server"              | `:rules pull` | `sectionCommands` |
| `rules_apply`     | "Rules: apply changes (push to server)"| `:rules apply` | `sectionCommands` |
| `rules_dry_run`   | "Rules: preview changes (dry-run)"     | `:rules apply --dry-run` | `sectionCommands` |
| `rules_new`       | "Rules: new rule from template…"       | `:rules new` | `sectionCommands` |

Each row's `Available` resolves to OK unconditionally — rules
management does not depend on a focused message. The `RunFn`
delegates to the same handlers as the cmd-bar.

### 7.7 List-pane / sidebar integration — none

The list pane and sidebar do not change. Rules are a settings-
shaped surface; surfacing them in the sidebar would pollute the
folder tree and competes with the spec-23 routing folders and the
spec-19 muted sentinel for sidebar real estate. The modal-overlay
approach matches spec 13's `:settings`.

### 7.8 Folders-pane rendering — none

Folders that are targets of rule `moveToFolder` / `copyToFolder`
actions are NOT specially marked in the sidebar. Rationale: a
folder may be a target of many rules; rendering a marker would be
visually noisy and would couple folder rendering to a Graph call.
A future spec could add an indicator if user feedback warrants it.

## 8. CLI

### 8.1 Subcommands

```sh
# List cached rules (uses local mirror; --refresh forces a pull first).
inkwell rules list
inkwell rules list --output json
inkwell rules list --refresh

# Get one rule by ID.
inkwell rules get AQAAAJ5dZqA=
inkwell rules get AQAAAJ5dZqA= --output json

# Pull from Graph and rewrite ~/.config/inkwell/rules.toml.
inkwell rules pull

# Diff the TOML against the local mirror; print actions that
# `apply` would take. Read-only; no Graph writes.
inkwell rules apply --dry-run

# Apply the TOML. Interactive Y/N for destructive rules unless --yes.
inkwell rules apply
inkwell rules apply --yes

# Open the rules.toml in $EDITOR; on save, run dry-run automatically.
inkwell rules edit

# Create a stub rule and open in $EDITOR.
inkwell rules new --name "Newsletters → Feed"

# Delete a rule by ID (PATCH + cache invalidation).
inkwell rules delete AQAAAJ5dZqA=
inkwell rules delete AQAAAJ5dZqA= --yes

# Toggle enabled state.
inkwell rules enable AQAAAJ5dZqA=
inkwell rules disable AQAAAJ5dZqA=

# Reorder: set sequence on a specific rule.
inkwell rules move AQAAAJ5dZqA= --sequence 25
```

Subcommand table:

| Subcommand        | Text output                                                       | JSON output                                                |
|-------------------|-------------------------------------------------------------------|------------------------------------------------------------|
| `list`            | `SEQ  EN  ID                NAME                            FLAGS` rows | `[{"id":"…","name":"…","sequence":N,"enabled":true,"flags":["read_only","external","error"]?, …}, …]` |
| `get <id>`        | Verbose multi-line dump (same shape as the §7.2.2 viewer)         | Full `MessageRule` JSON                                    |
| `pull`            | `✓ pulled N rules; rewrote ~/.config/inkwell/rules.toml`          | `{"pulled":N,"path":"…"}`                                  |
| `apply`           | Diff summary + per-rule actions; final `✓ applied / ⚠ partial`     | `{"created":N,"updated":N,"deleted":N,"errors":[…]}`       |
| `apply --dry-run` | Diff summary; exit 0; no Graph writes                             | Diff JSON                                                  |
| `edit`            | (no text output; `$EDITOR` foreground)                            | (n/a)                                                      |
| `new`             | (no text output; `$EDITOR` foreground)                            | (n/a — interactive)                                        |
| `delete <id>`     | `✓ deleted rule "<name>"`                                         | `{"deleted":true,"id":"…","name":"…"}`                     |
| `enable <id>`     | `✓ enabled rule "<name>"`                                         | `{"enabled":true,"id":"…"}`                                |
| `disable <id>`    | `✓ disabled rule "<name>"`                                        | `{"enabled":false,"id":"…"}`                               |
| `move <id> --sequence N` | `✓ moved rule "<name>" to sequence N`                      | `{"id":"…","sequence":N}`                                  |

### 8.2 Exit codes

Following spec 14's CLI exit-code contract:

- `0` — success.
- `1` — runtime failure (Graph 5xx, network down, store error).
- `2` — user error (unknown rule ID, malformed TOML, missing
  required field, deferred predicate / action in TOML).
- `3` — confirmation declined (user said `N` at the prompt).

### 8.3 Cmd-bar parity

The TUI cmd-bar accepts the same verbs:

```
:rules list
:rules pull
:rules apply
:rules apply --dry-run
:rules new
:rules edit
:rules delete <id>
:rules enable <id>
:rules disable <id>
:rules move <id> <seq>
```

`:rules` alone opens the manager modal (§7.2); every other subverb
invokes the same handler as the CLI subcommand.

Behaviour matches the CLI exactly. Modal-opening subverbs (`list`)
render in the manager; non-modal subverbs render to the status bar.

## 9. Edge cases

| Case                                                          | Behaviour                                                                                                                         |
|---------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------|
| User runs `apply` while offline                               | Pull fails with `network unreachable`; abort before any PATCH. No partial state. Toast and exit 1.                                |
| User runs `apply` after editing TOML offline                  | Pull is the first step (§6.5); if pull fails, no apply runs. The TOML stays untouched.                                            |
| Rule `move = "Folders/Inbox/Newsletters"` but folder missing  | Apply fails for that rule with `folder "Folders/Inbox/Newsletters" not found`. Other rules continue (per-rule sequential failure). |
| User edits TOML to set `id = "wrong-id"`                      | Apply matches by ID; if ID doesn't exist server-side, treated as **create** (TOML rule's ID is dropped, server assigns a new one). Toast warns: `rule "X" had unknown id; created new`. |
| User renames an ID-less rule in TOML (changed `name = "X"` → `name = "Y"`) | Diff classifies the old name as `delete` and the new name as `create` — there is no rename detection. **The first `pull` after any `apply` writes the server-assigned ID back into the TOML for every rule, so this race only fires for ID-less rules the user is actively editing before their first `apply`.** Apply emits a one-line warning when it detects an ID-less `delete` + `create` whose predicate / action shape matches identically: `⚠ rule "Y" looks like a rename of "X" but no ID is set; will delete + recreate. Run pull first to lock IDs.` |
| User deletes a rule from TOML                                 | Apply diff classifies it as `delete`. Deletes always prompt unless `--yes` (deletion confirmation is independent of the rule's `confirm` field — the rule no longer exists in TOML to read it from). |
| Two rules in TOML share a name (no ID)                        | Loader rejects with `rules.toml:L42: duplicate rule name "X" — names must be unique among ID-less rules`. The user adds an ID (post-pull) or renames one. |
| Two rules share a `sequence` value                            | Allowed (Graph allows it). Manager renders both with a `⚠ tied` indicator; CLI list shows both. |
| Server returns 429 mid-apply                                  | The throttle transport waits per `Retry-After` (existing infra in `internal/graph/client.go`). Apply runs serially, so a 429 on rule N pauses the whole apply until the retry succeeds. If `Retry-After` ≤ 30s, apply blocks transparently (toast updates to `applying rule N/M (waiting Xs for rate limit)…`). If `Retry-After` > 30s, the toast surfaces the delay explicitly and the user may Ctrl+C to abort. If the transport's `MaxBackoff` (default 30s, configured at startup) is exceeded after retries, apply exits 1 with the Graph error and reports which rules already succeeded. Bulk applies (50+ rules) on tight tenant policies may legitimately trip 429; this is acceptable v1 behaviour. |
| Admin-pushed rule (`isReadOnly = true`)                       | Listed; `apply` diff skips it (no update / no delete). Editor refuses to write changes via `E`. |
| Rule contains a deferred predicate (e.g. `isMeetingResponse`) | Pull preserves verbatim in `RawConditions`; manager marks `[ext]`; `E` refuses to open. Re-pull/round-trip cycle does NOT lose data. |
| Rule `display_name` empty on the server                       | The **pull pipeline** (`graph.ListMessageRules` → `store.UpsertMessageRulesBatch`) assigns the placeholder `"<unnamed rule N>"` where N is the rule's `sequence`. The placeholder is written into the local mirror and the TOML rewrite so subsequent applies match by name. The loader does NOT generate placeholders — it only validates user-typed names; the pull-time placeholder is owned by `internal/rules/pull.go`. Test: `TestPullAssignsPlaceholderForEmptyDisplayName`. |
| User passes `--output json` to an interactive subcommand (`edit`, `new`) | Exit 2 with `inkwell rules edit: interactive subcommand does not support --output json`. |
| `rules.toml` has a deferred action (e.g. `forward_to`)        | Loader rejects with `rules.toml:L17: action "forward_to" is not supported in inkwell v1 (see docs §6.3). Edit this rule in Outlook web.` |
| Rule has `delete = true` but `confirm != "always"`            | Loader rejects with the gate error (§6.4). `confirm = "never"` is similarly rejected for any destructive rule.                    |
| User opens `:rules` with no signed-in account                 | Modal shows `no account signed in — run inkwell` and dismisses on any keypress.                                                   |
| Sign-out + sign-in to a different account                     | Mirror rows FK-cascaded on account delete. New account starts with `last_pulled_at = 0` everywhere; first `:rules` shows "never pulled — press P". |
| Concurrent edit in Outlook web while inkwell modal is open    | Manager's "Last pull" timestamp tells the user the data is stale; `P` refreshes. Apply pipeline always re-pulls first (§6.5).      |
| Migration applied on a populated DB                           | Table starts empty; `:rules` shows "never pulled". First `pull` populates.                                                        |
| Folder path with non-ASCII (e.g. `move = "Folders/受信箱/News"`) | macOS HFS+/APFS returns folder display names in NFD form via filesystem APIs; Graph returns folder display names in NFC. The `GetFolderByPath` helper (§6.5 step 3) NFC-normalises both sides via `golang.org/x/text/unicode/norm` before comparison. Test: `TestApplyResolvesUnicodeFolderPath`. |
| Rule `display_name` with non-ASCII                            | Round-trips verbatim. The redactor's email regex is `\w`-based (Unicode-aware in Go's RE2 flavour with `(?i)` flag); a name like `"会議メモ → boss@example.invalid"` scrubs only the email portion. |
| TOML file with mixed `\r\n` / `\n` line endings                 | BurntSushi/toml decoder accepts both. Round-trip rewrite uses platform default (LF on Unix). |
| `messageRules` endpoint returns 503 mid-list                  | Pull aborts; mirror untouched; toast surfaces the error. Retry via `P`.                                                            |

## 10. Performance budgets

| Surface                                                                                        | Budget          | Benchmark                                              |
|------------------------------------------------------------------------------------------------|-----------------|--------------------------------------------------------|
| `Store.ListMessageRules` over 50-rule mirror                                                   | ≤2ms p95        | `BenchmarkListMessageRules` in `internal/store/`        |
| `Store.UpsertMessageRulesBatch` writing 50 rules                                               | ≤20ms p95       | `BenchmarkUpsertMessageRulesBatch`                      |
| `inkwell rules pull` end-to-end (warm cache, 50-rule fixture, mocked Graph)                    | ≤2s p95         | integration test `TestRulesPullEndToEnd_50Rules`        |
| `inkwell rules apply --dry-run` over a 50-rule fixture                                          | ≤200ms p95      | integration test `TestRulesApplyDryRun_50Rules`         |
| `inkwell rules apply` diff computation (10 create, 10 update, 5 delete) — pure CPU, no I/O      | ≤50ms p95       | `BenchmarkRulesDiffComputation` in `internal/rules/`     |
| `:rules` modal open → first render (cold cache, render-only path)                              | ≤100ms p95      | TUI e2e timed within `TestRulesModalOpensInTime`        |
| `T` toggle (synchronous PATCH, mocked Graph 50ms)                                              | ≤500ms p95      | integration test `TestRulesToggleEndToEnd`              |

The pull benchmark uses a synthesised `internal/graph/testdata/`
fixture of 50 messageRule envelopes (the practical upper bound for
most user mailboxes; tenants with admin rules approach ~200, which
the spec accepts as out-of-budget acceptable for v1 — the manager
remains functional, just slower to first render). The 50-rule
fixture is generated by `internal/rules/testfixtures.go` per
`docs/CONVENTIONS.md` §5.2 ("Synthesised fixtures … are generated by helpers
in `internal/<pkg>/testfixtures.go`, not committed as binary
blobs.").

The store budgets are tight because the mirror is small and
indexed. The Graph budgets allow for one network round-trip per
sub-operation plus throttle margin — verified against a httptest
server that injects a synthetic 30ms latency per call.

## 11. Configuration

This spec adds a new `[rules]` section to `config.toml`:

| Key                              | Default                                           | Used in §       | Description |
|----------------------------------|---------------------------------------------------|-----------------|-------------|
| `rules.file`                     | `~/.config/inkwell/rules.toml`                    | §6.1            | Path to the rules authoring TOML. |
| `rules.pull_stale_threshold`     | `"1h"`                                            | §7.5            | When the last pull is older than this, the manager status hint switches to the warning style. |
| `rules.ascii_fallback`           | `false`                                           | §7.3            | When `true`, substitutes `🔒 → RO` and `⚠ → ERR` for terminals without UTF-8. `[ext]` is already ASCII and unchanged. |
| `rules.confirm_destructive`      | `true`                                            | §6.5 / §8.1     | Global belt-and-suspenders gate. When `true` (default), `apply` prompts for any rule with `delete = true` *regardless of the rule's `confirm` field*. Setting to `false` lets the per-rule `confirm` value decide alone. `--yes` overrides per-invocation. |
| `rules.editor_open_at_rule`      | `true`                                            | §7.4            | When `true`, `E` opens `$EDITOR` with the `+<line>` argument to position the cursor at the rule's TOML block. Editors that do not understand `+<line>` (rare in 2026) can disable this. |

TOML form:

```toml
[rules]
file                    = "~/.config/inkwell/rules.toml"
pull_stale_threshold    = "1h"
ascii_fallback          = false
confirm_destructive     = true
editor_open_at_rule     = true
```

No new `[bindings]` keys — the manager-modal bindings are
mode-scoped and do not pollute the global `[bindings]` table.
(Rationale: spec 22's palette bindings are also mode-scoped.)

## 12. Logging and redaction

### 12.1 Log sites

Every Graph call adds a structured log line at DEBUG level:

```
"graph.rules.list", "graph.rules.get", "graph.rules.create",
"graph.rules.update", "graph.rules.delete"
```

Fields: `account_id`, `rule_id` (when known), `display_name`
(scrubbed at INFO+ per §12.2 — the existing redactor's email-regex
`/[\w._%+-]+@[\w.-]+\.[A-Za-z]{2,}/` triggers a `<email-N>`
substitution; pure-descriptor names pass through unchanged),
`http_status`, `duration_ms`. The Graph client's existing
`loggingTransport` covers the HTTP-side request/response logging;
the new lines are at the rule-pkg layer for "what operation was
attempted, in business terms".

`inkwell rules apply` emits one INFO line summarising the apply at
exit:

```
"rules.apply", account_id=N, created=N, updated=N, deleted=N,
errors=N, dry_run=bool, duration_ms=N
```

### 12.2 Redaction additions

Rule conditions can contain user-typed email addresses (`from`,
`sent_to`, `sender_contains`). The existing redaction handler in
`internal/log/redact.go` already scrubs email addresses via the
`<email-N>` keying (`docs/CONVENTIONS.md` §7 rule 3). New log sites MUST go
through `slog` with structured fields, not through `fmt.Sprintf`
into a message string — the structured path is what redact hooks
into.

Two new fields gain redaction coverage. The policy is **codified
in tests**, not by analogy:

- `display_name` of a rule — may contain a user-typed name like
  `"Boss boss@example.invalid"`. Policy: treated equivalently to
  a subject line per `docs/CONVENTIONS.md` §7 rule 3 — logged verbatim at
  DEBUG, scrubbed at INFO and above by the existing email-regex
  scrubber (matches `/[\w._%+-]+@[\w.-]+\.[A-Za-z]{2,}/`,
  case-insensitive). Rule names that are pure descriptors (e.g.
  `"Newsletters → Feed"`) round-trip unchanged because the
  regex finds no match. Test `TestRedactScrubsRuleDisplayNameAtInfo`
  codifies the rule.
- `subject_contains`, `body_contains`, `body_or_subject_contains`,
  `header_contains` — match strings the user typed. Risk: a user
  who types `"password reset"` as a `body_contains` substring is
  preserving a literal in the log. Spec policy: log these fields
  at DEBUG only, and scrub at INFO and above. Apply-summary INFO
  lines do NOT log predicate values — only counts.

Spec 17 cross-reference: the new log fields and new SQL composition
(parameterised; no string concat) are listed in §14's spec-17
review row.

### 12.3 What is never logged

- Graph access tokens — already covered by the existing
  `Authorization` header scrubber.
- `RawConditions` / `RawActions` / `RawExceptions` JSON blobs
  verbatim — the redaction handler does not parse JSON; safer to
  not log raw payloads. The Go layer logs only the typed v1
  fields it understands.
- The contents of `rules.toml` itself. The file lives at the
  user's home directory and is not log-relevant; if a user reports
  a bug, the support recipe is "attach the file deliberately",
  not "scrape it from the logs".

## 13. Definition of done

- [ ] Migration `014_message_rules.sql` lands cleanly with:
      `message_rules` table (composite PK, CHECK on bool columns,
      CHECK on `length(rule_id) > 0`), `idx_message_rules_sequence`
      index, and `UPDATE schema_meta SET value = '14'`. Pre-merge
      re-confirmation of the slot number per `docs/CONVENTIONS.md` §13.
- [ ] `store.Store` interface gains `ListMessageRules`,
      `GetMessageRule`, `UpsertMessageRule`,
      `UpsertMessageRulesBatch`, `DeleteMessageRule`,
      `LastRulesPull`. Errors: `ErrInvalidRuleID` for empty
      rule IDs; `ErrNotFound` for missing rules; `store` does
      not validate predicate / action shape (loader's job).
- [ ] `MessagePredicates`, `MessageActions`, `SizeKB`, `Recipient`,
      `EmailAddress` types in `internal/store/rules_types.go`
      mirror the v1 catalogue per §4.3 with pointer-types for
      tri-state booleans.
- [ ] `internal/graph/rules.go` provides `ListMessageRules`,
      `GetMessageRule`, `CreateMessageRule`, `UpdateMessageRule`,
      `DeleteMessageRule`. JSON marshal preserves omitempty for
      tri-state fields; unmarshal preserves the raw `conditions`
      / `actions` / `exceptions` sub-objects in
      `RawConditions` / `RawActions` / `RawExceptions`.
- [ ] `internal/graph/rules_merge.go` implements `jsonMerge` for
      the PATCH semantics in §5.3. Unit tests cover empty-prior,
      empty-edit, conflicting key, array replacement, deeply-
      nested key preservation.
- [ ] `internal/rules` package: `LoadCatalogue`, `RuleDraft`,
      `Catalogue` per §6.4. Loader validates:
  - field names against v1 catalogue (§6.3);
  - deferred fields rejected with file:line error;
  - `delete = true` requires `confirm = "always"`; `confirm =
    "never"` rejected for any destructive rule (spec 27 §3.4
    parity);
  - `name` non-empty and unique among ID-less rules;
  - `sequence` non-negative;
  - folder paths in `move` / `copy` are well-formed strings (resolution at apply time).
- [ ] `internal/rules/apply.go` implements the §6.5 pipeline:
      pull → load → resolve → diff → confirm → execute → round-trip.
      All-or-nothing per rule; sequential, not parallel.
- [ ] `internal/rules/edit.go` opens `$EDITOR` at the appropriate
      line for `E` / `new`. Honours `[rules].editor_open_at_rule`.
- [ ] `MessageRulesMode` constant in `internal/ui/messages.go`;
      mode dispatch in the root `Update` checks `MessageRulesMode`
      alongside `PaletteMode` / `SettingsMode` in the modal-overlay
      branch (after the higher-priority `SignInMode` and
      `ConfirmMode` checks, before the per-pane dispatch). Mode is
      set on `:rules` open and cleared on Esc.
- [ ] `MessageRulesModel` (value-typed, per `docs/CONVENTIONS.md` §4): holds the
      manager state (selection, filter input, last pull
      timestamp, in-flight apply token). Embedded into the root
      `Model` alongside `PaletteModel`.
- [ ] KeyMap fields: a new `KeyMap.Rules` group with `Next`,
      `Prev`, `Open`, `New`, `Edit`, `Delete`, `Toggle`,
      `ReorderUp`, `ReorderDown`, `Pull`, `Filter`, `DryRunApply`,
      `Apply`, `Close`. Defaults per §7.2.1. Mode-scoped; not
      exposed as global `[bindings]` keys.
- [ ] Read-only rule rendering per §7.3: 🔒 / `[ext]` / `⚠`
      glyphs with ASCII fallbacks (`RO` / `ERR`; `[ext]` is
      already ASCII) gated by `[rules].ascii_fallback`. `E` /
      `X` / `T` / `J` / `K` on read-only rules produce the
      friendly toast.
- [ ] CLI `cmd/inkwell/cmd_rules.go` implements every subcommand
      in §8.1 with the table-shape outputs and JSON outputs.
      Registered in `cmd_root.go`. Exit codes per §8.2.
- [ ] Cmd-bar parity (§8.3): `:rules <subverb>` dispatches via
      the same handlers as the CLI. `:rules` alone opens the
      modal.
- [ ] Command-palette rows (spec 22): `internal/ui/palette_commands.go`
      gains the five static rows from §7.6.
- [ ] User docs: `docs/user/reference.md` adds:
  - `:rules` family verbs (table row per subverb);
  - `inkwell rules <subverb>` table row per subcommand;
  - the manager-modal bindings table (§7.2.1);
  - the `~/.config/inkwell/rules.toml` field catalogue (§6.3).
  `docs/user/how-to.md` adds a "Manage server-side rules" recipe.
- [ ] `docs/CONFIG.md`: new `[rules]` section per §11; cross-
      reference to PRD §3.1 `MailboxSettings.ReadWrite` scope.
- [ ] `internal/config`: new `RulesConfig` struct embedded in the
      top-level `Config` with `toml:"rules"` tag; defaults wired in
      `internal/config/defaults.go`; validator rejects unknown
      `[rules]` keys. Test `TestConfigDecodeRulesSection` covers
      decode-with-defaults, decode-with-overrides, and unknown-key
      rejection.
- [ ] `docs/specs/32-server-side-rules/plan.md` exists with `Status: done` at ship
      time, per `docs/CONVENTIONS.md` §13's mandatory-plan-file rule (the
      same rule whose violation in spec 16 v0.12.0 motivated the
      memory note).
- [ ] `docs/PRD.md` §10 spec inventory adds spec 32.
- [ ] `docs/ROADMAP.md`: Bucket 4 row updated to `Shipped vX.Y.Z`;
      §1.14 backlog heading updated.
- [ ] `docs/THREAT_MODEL.md` (per spec 17 §4): new row
      "server-side rules — token never logged; rules.toml
      contains user-typed predicate values that may include
      email addresses; redaction handler covers email scrub in
      logs but not in the user-owned file". Privacy doc impact:
      `message_rules` table added to "what data inkwell stores
      locally".
- [ ] Tests:
  - **migration**:
    - `TestMigration014AppliesCleanly` — opens a v13 DB, runs 014,
      asserts `schema_meta.version == '14'`, `message_rules` table
      exists with the right columns, `idx_message_rules_sequence`
      is present.
  - **store**:
    - `TestUpsertAndListMessageRules` — round-trip preserves all
      v1 fields + Raw* JSON.
    - `TestUpsertMessageRulesBatchReplacesAll` — batch insert
      followed by smaller batch produces the smaller mirror (no
      stale rows).
    - `TestDeleteMessageRule` — happy path + 404-on-delete
      idempotent.
    - `TestMessageRulesFKCascadeOnAccountDelete` — deleting an
      account drops its rules.
    - `TestLastRulesPullReturnsMaxTimestamp` — multi-rule mirror
      with varying `last_pulled_at` returns the max.
    - `TestListMessageRulesOrdering` — verifies
      `sequence_num ASC, rule_id ASC` stable sort.
  - **graph**:
    - `TestGraphListMessageRules_HappyPath` — httptest with canned
      `{value: [r1, r2]}` response.
    - `TestGraphCreateMessageRule_201` — POST returns the created
      rule including server-assigned ID.
    - `TestGraphUpdateMessageRule_404` — PATCH on missing rule
      returns `*GraphError` with `StatusCode == 404`.
    - `TestGraphDeleteMessageRule_404IsSuccess` — idempotent
      delete.
    - `TestGraphRules_RetryAfter429` — verifies the throttle
      transport retries after Retry-After.
  - **graph merge**:
    - `TestJsonMergePreservesNonV1Keys` — prior has
      `isVoicemail: true`; edit has new `subjectContains: [...]`;
      merged result has both.
    - `TestJsonMergeReplacesArrays` — `bodyContains: ["a", "b"]`
      in prior + `bodyContains: ["c"]` in edit → merged has
      `["c"]`, not `["a","b","c"]`.
    - `TestJsonMergeEmptyEdit` — empty edit returns prior verbatim.
    - `TestJsonMergeEmptyPrior` — empty prior returns edit
      verbatim.
    - `TestJsonMergeRoundTripsThroughMapAny` — the merged
      `json.RawMessage` is assigned into the `map[string]any`
      PATCH body and remarshalled; verifies `encoding/json`
      preserves the RawMessage bytes through the round-trip
      (not double-encoded as a JSON string).
  - **rules loader**:
    - `TestLoadCatalogueValidExample` — the §6.2 example loads.
    - `TestLoadCatalogueRejectsDeferredPredicate` — `is_voicemail`
      in TOML produces a load error with file:line.
    - `TestLoadCatalogueRejectsForwardAction` — `forward_to` in
      TOML produces a load error with PRD §3.2 reference.
    - `TestLoadCatalogueRejectsDeleteWithoutConfirm` — `delete =
      true` without `confirm = "always"` errors.
    - `TestLoadCatalogueRejectsConfirmNeverOnDestructive` —
      `delete = true` with `confirm = "never"` errors with spec-27
      §3.4 message text.
    - `TestLoadCatalogueRejectsDuplicateNameNoID` — two rules with
      the same name and no ID error.
    - `TestLoadCatalogueAcceptsShorthandFromString` — `from = ["x@y"]`
      expands to `[{address:"x@y", name:""}]`.
    - `TestLoadCatalogueMissingFileIsEmpty` — non-existent path
      returns empty catalogue, nil err.
    - `TestLoadCatalogueRejectsUnknownField` — typo'd field
      (`form_contains` instead of `from_contains`) produces a
      pointed error.
  - **apply pipeline**:
    - `TestApplyDiffClassifiesCreatesUpdatesDeletes` — three-bucket
      diff from a fixture.
    - `TestApplyDryRunNoWrites` — `--dry-run` makes zero PATCH /
      POST / DELETE calls (verified by counting on the mock).
    - `TestApplySkipsReadOnlyRules` — server has `isReadOnly =
      true` rule; TOML omits it → no delete is issued.
    - `TestApplyResolvesFolderPaths` — `move = "Folders/X"` is
      translated to the Graph folder ID before PATCH.
    - `TestApplyFailsOnUnresolvedFolder` — missing folder → exit
      1 with a pointed message; other rules unaffected.
    - `TestApplyPartialSuccess` — 3 creates, second fails with
      403; first applied, third unprocessed; toast text matches
      §7.5.
    - `TestApplyConflictDetection` — server rule changed between
      pull and apply → conflict toast.
    - `TestApplyConfirmDestructiveRule` — `delete = true` rule
      prompts; `--yes` skips the prompt.
    - `TestApplyRoundTripPreservesNonV1Fields` — create a rule
      via TOML, pull, observe Raw* preserved.
    - `TestApplyDryRunOutputDeterministic` — same input twice
      produces the same diff string.
  - **rules CLI**:
    - `TestCLIRulesListEmpty` — never-pulled state.
    - `TestCLIRulesListPopulated` — 5-rule fixture, text + JSON.
    - `TestCLIRulesGetByID` — happy path + unknown ID exit 2.
    - `TestCLIRulesPullRewritesFile` — verifies rules.toml on disk
      after pull.
    - `TestCLIRulesApplyDryRun` — does not write.
    - `TestCLIRulesApplyYes` — applies without prompt.
    - `TestCLIRulesToggle` — enable / disable.
    - `TestCLIRulesMove` — sequence change.
    - `TestCLIRulesEditInteractiveRejectsJSON` — exit 2 on
      `--output json`.
  - **UI dispatch (e2e)**:
    - `TestRulesModalOpensOnColon` — `:rules` enter activates the
      mode and renders the manager.
    - `TestRulesModalEscClosesAndRestoresMode` — `Esc` exits to
      Normal.
    - `TestRulesModalNavigationJK` — `j` / `k` move selection.
    - `TestRulesModalEnterOpensViewer` — `Enter` swaps to the
      view pane; second `Esc` returns to manager.
    - `TestRulesModalTToggleSyncsPATCH` — `T` triggers a PATCH and
      flips `is_enabled`.
    - `TestRulesModalXConfirmDelete` — `X` prompts; `y` deletes;
      `n` cancels.
    - `TestRulesModalReorderShiftJK` — `J` swaps sequence with
      the next rule; PATCH count = 2.
    - `TestRulesModalReadOnlyRefusesEdit` — `E` on
      `is_read_only` rule produces the toast and makes no Graph
      call.
    - `TestRulesModalExternalRefusesEdit` — `E` on a rule with a
      deferred predicate refuses with the `[ext]` toast.
    - `TestRulesModalErrorRefusesEdit` — `E` on `has_error` rule
      refuses with the `⚠` toast.
    - `TestRulesModalAOpensDryRunPane` — `a` renders the diff in
      the side pane; no Graph writes.
    - `TestRulesModalCapAApplies` — `A` confirms + writes.
    - `TestRulesModalPullRefreshes` — `P` triggers
      `graph.ListMessageRules`; mirror updated.
    - `TestRulesPaletteRowsStaticPresent` — palette rows render
      from `Ctrl+K`.
    - `TestRulesModalAsciiFallback` — `[rules].ascii_fallback =
      true` substitutes `🔒 → RO` and `⚠ → ERR`; `[ext]` is
      unchanged.
  - **bench**: per §10 — `BenchmarkListMessageRules`,
    `BenchmarkUpsertMessageRulesBatch` (both in
    `internal/store/`), `BenchmarkRulesDiffComputation` (in
    `internal/rules/`), `TestRulesPullEndToEnd_50Rules`,
    `TestRulesApplyDryRun_50Rules`, `TestRulesToggleEndToEnd`,
    `TestRulesModalOpensInTime` (the last four are integration /
    e2e timed tests, not Go `Benchmark*` functions; budget gates
    via `time.Since(start)` assertions).
  - **redaction**:
    - `TestRedactScrubsRuleDisplayNameAtInfo` — INFO-level log of
      a rule whose name is `"Boss boss@example.invalid"` produces
      `"Boss <email-N>"`; DEBUG-level log preserves the literal.
    - `TestRulesLoggingDoesNotLeakBodyContains` — INFO-level apply
      summary does NOT include predicate values (`body_contains`,
      `subject_contains`, `header_contains`, `body_or_subject_contains`).

## 14. Cross-cutting checklist (`docs/CONVENTIONS.md` §11)

- [ ] **Scopes:** `MailboxSettings.ReadWrite` (already in PRD §3.1
      and `internal/auth/scopes.go:34`). No new scope.
- [ ] **Store reads/writes:** `message_rules` (INSERT / UPDATE /
      DELETE / SELECT). `messages` table unchanged. FK cascade on
      account delete.
- [ ] **Graph endpoints:** `GET / GET-one / POST / PATCH / DELETE`
      on `/me/mailFolders/inbox/messageRules` (§5.1). No new
      transport tier.
- [ ] **Offline:** Read paths (`list`, manager open) work offline
      from the local mirror. Writes (`pull`, `apply`, `toggle`,
      `delete`, …) require connectivity; failure surfaces as an
      error toast and does NOT enqueue for replay (rules are not
      message mutations and bypass the action queue).
- [ ] **Undo:** Spec 07's `u`-key undo stack is NOT involved.
      Rationale: rules are configuration, not per-message
      mutations. Undo for rule edits is "edit the TOML again and
      re-apply", which is the natural workflow. Document in the
      explanation doc.
- [ ] **User errors:** §9 edge-case table covers offline, unknown
      ID, unresolved folder, deferred predicate, duplicate name,
      malformed TOML. The CLI exit codes (§8.2) and TUI toasts
      (§7.5) are the surfaces.
- [ ] **Latency budget:** §10 covers every new surface. Pull at
      2s is the user-visible budget; the rest are internal.
- [ ] **Logs:** §12 covers log sites and redaction. New DEBUG
      lines per Graph call; one INFO apply-summary line per
      `apply` invocation. Predicate values logged at DEBUG only.
- [ ] **CLI mode:** `inkwell rules <list|get|pull|apply|edit|new|delete|enable|disable|move>`
      per §8.1.
- [ ] **Tests:** §13 comprehensive list — migration, store, graph,
      graph merge, loader, apply, CLI, e2e, bench, redaction.
- [ ] **Spec 11 consistency:** saved searches (local, on-demand)
      and server rules (server, on-delivery) are deliberately
      different surfaces. Future work could expose "promote saved
      search to a rule" (§15); v1 keeps them separate. Both use
      a TOML file under `~/.config/inkwell/`, both round-trip
      cleanly with the same `BurntSushi/toml` encoder defaults.
- [ ] **Spec 13 consistency:** mailbox settings ships the
      synchronous-PATCH-with-modal pattern this spec mirrors. The
      rules manager modal layout follows the spec-13 `:settings`
      modal's overlay sizing (80 cols × min(20, h-6)). Rule edits
      are NOT queued — same rationale as OOO: rare, benefits from
      explicit feedback. Spec 13's `Out of scope (§13)` "Mail
      rules / forwarding rules" line is now satisfied by this
      spec.
- [ ] **Spec 17 review (security testing + CASA evidence):**
      this spec introduces:
  - New Graph endpoint surface (`/me/mailFolders/inbox/messageRules`).
  - New local persisted state (`message_rules` table).
  - New user-owned file at `~/.config/inkwell/rules.toml`.
  - No new subprocess; no new cryptographic primitive; no new
    third-party data flow.
      Spec 17 §4 (security tests) gets:
  - `TestRulesGraphHonoursMailboxSettingsScope` — verifies the
    auth context includes `MailboxSettings.ReadWrite` on rule
    calls; gosec passes on the new files.
  - `TestRulesTOMLPathTraversal` — `[rules].file` is path-
    cleaned and rejects `..` traversal (the spec 17 path-
    traversal guard applies to the same XDG helper).
  - `TestRulesNoForwardActionAccepted` — confirms the
    `forwardTo` deny path stays closed at the loader.
      THREAT_MODEL.md row: `message_rules` table persists across
      sign-out; rules.toml is user-owned and not redacted.
      PRIVACY.md row: rules.toml stores user-typed predicate
      values (may include email addresses, body fragments).
- [ ] **Spec 18 consistency:** the `move` / `copy` action's
      destination is resolved via the new `GetFolderByPath`
      helper on the folders cache. Microsoft Graph stores
      `moveToFolder` as an opaque folder *ID*, so a folder
      *rename* (in inkwell or Outlook web) does NOT break a rule
      that already has the ID stored server-side — the rule
      keeps targeting the same folder under its new name. The
      retargeting warning in §6.5 step 3 fires for the inverse
      case: when the path string in TOML still points at the
      old name, the user wrote `move = "Folders/Old"`, and a
      *different* folder now lives at that path. Folder
      *deletion* is what breaks rules and surfaces as `folder
      "X" not found` per §9. Folder management surfaces (`N` /
      `R` / `X`) are unaffected.
- [ ] **Spec 19 consistency:** muted threads are unaffected — mute
      is local-only state; server rules run before delivery and
      do not see inkwell's mute table. A rule that moves a
      message into a folder happens before mute, so a muted-
      thread message routed by a rule ends up in the rule's
      target folder but stays hidden from the muted view (mute
      filter applies post-store).
- [ ] **Spec 22 consistency:** five new static palette rows
      (§7.6) follow spec 22's `palette_commands.go` pattern. No
      `#` sigil overload. The `:rules` manager modal uses
      `MessageRulesMode`, not `PaletteMode` — distinct overlays.
- [ ] **Spec 23 consistency:** routing is local-only sender →
      destination mapping; server rules are delivery-time
      predicate → action mapping. They overlap (a `move` rule and
      a routing assignment to Feed can both surface the same
      message in similar buckets) but the data axes are
      independent. The doc sweep adds a one-line note to
      `docs/user/how-to.md` "When to use a server rule vs. a
      routing assignment" — server rule wins for delivery-time
      behaviour that must work in Outlook web too; routing wins
      for inkwell-local sender bucketing.
- [ ] **Spec 24 consistency:** split-inbox tabs (saved searches
      with `tab_order`) are unchanged. A user who wants a tab
      that mirrors a server rule's effect must create the saved
      search themselves; auto-promotion is out of scope.
- [ ] **Spec 27 consistency:** custom actions (TOML, key-bound,
      client-side per-message) and server rules (TOML, server-
      side, delivery-time) share a TOML-on-disk pattern but
      otherwise do not overlap. A custom action that calls a
      `move` op happens at user invocation; a rule's `move`
      action happens at delivery. Both files live under
      `~/.config/inkwell/`; the `[custom_actions].file` and
      `[rules].file` keys are independent.
- [ ] **Spec 28 consistency:** screener gates new senders into a
      review queue; server rules act at delivery on every
      message. A screened-out sender's mail still hits server
      rules first (Graph runs rules before any inkwell-side
      filter applies). If the rule moves the message to a
      non-Inbox folder, the spec-28 default-view filter still
      hides it because the screener anti-join is **folder-
      agnostic** (`ApplyScreenerFilter = true` matches on
      `sender_routing.destination = 'screener'`, not on
      folder ID — see `docs/specs/28-screener/spec.md` §4.4 and
      §5.5). Net behaviour: a screened-out sender whose mail is
      rule-moved to /Newsletters is in /Newsletters server-side
      *and* in the local DB but hidden from /Newsletters'
      default folder view; it surfaces only in the
      `__screened_out__` sentinel folder or via an explicit
      `:filter`. Document in `docs/user/how-to.md` under "When
      rules and the screener disagree".
- [ ] **Spec 31 consistency:** Focused / Other classification
      happens server-side, independent of `messageRules`.
      Microsoft does not publicly document the precise pipeline
      order between rule execution and `inferenceClassification`
      assignment, but empirical tenant observation (Microsoft
      Q&A 2023 thread "Do Inbox rules run before Focused
      Inbox?") indicates rules run *first*, so a rule that
      moves a message out of Inbox produces a message that
      never gets a `focused`/`other` label (and is invisible to
      the spec-31 sub-strip because it's not in Inbox). The
      cross-feature interactions table in
      `docs/user/reference.md` should document this empirically
      ("our testing shows X; verify in your tenant") rather than
      as a Microsoft-documented guarantee.
- [ ] **Docs consistency sweep:** `docs/CONFIG.md` updated for
      `[rules]` keys; `docs/user/reference.md` updated for
      `:rules` family, `inkwell rules`, manager-modal bindings,
      and `rules.toml` field catalogue; `docs/user/how-to.md`
      adds the "Manage server-side rules" recipe and the two
      cross-feature notes; `docs/PRD.md` §10 spec inventory adds
      spec 32; `docs/ROADMAP.md` bucket-4 row updated and §1.14
      heading flipped to Shipped. `docs/user/tutorial.md`
      unchanged (rules are a power-user surface; not in the
      first-30-minutes path). `docs/user/explanation.md` gains a
      paragraph on "configuration-as-code: rules.toml is your
      source of truth".

## 15. Out of scope (deferred)

This list is the explicit "we considered it, said no, and here's
why" set. Future specs may revisit.

- **Run Rules Now / retroactive apply.** Outlook desktop has it;
  inkwell does not (v1). Implementation cost is non-trivial: it
  requires either a full local evaluator for `messageRulePredicates`
  (29 fields, regex-adjacent matching, locale-sensitive importance
  / sensitivity enums) or a beta-only Graph endpoint
  (`messageRule/applyToInbox`) which violates the ARCH §0
  v1.0-only constraint. A future spec could add it once the value
  is demonstrated.
- **Forward / redirect actions.** PRD §3.2 denies `Mail.Send`; auto-
  forward is structurally a send-on-behalf surface. Inkwell remains
  drafts-only.
- **Permanent delete action.** Irreversible without server-side
  recoverability; the per-message `D` chord (spec 07) requires
  per-message intent, which a delivery-time rule cannot offer.
- **Pattern-language → Graph rule translator.** A user might want
  to type `~f vendor@ & ~B "invoice"` and "save as server rule".
  The translation problem is bounded but non-trivial (predicate
  vocabulary differs; not every inkwell operator has a
  `messageRulePredicates` equivalent — e.g., `~y focused` cannot be
  pre-rule because `inferenceClassification` is assigned post-rule).
  A separate spec could enumerate the safe subset.
- **Rules on folders other than Inbox.** Microsoft Graph v1.0
  scopes `messageRules` to the Inbox folder. Child-folder filter
  rules (e.g. "in /Newsletters, mark older than 30d as read") are
  out of scope. If Graph adds the endpoint shape, a follow-up
  spec can extend the inkwell surface.
- **Rule import / export from `.rwz` / Sieve.** Conversion problem
  orthogonal to CRUD. Out of scope.
- **Multi-account rules.** v1 is single-account (roadmap §1.2).
  The schema is forward-compatible (`account_id` PK); the apply
  pipeline currently iterates a single account.
- **Rule-execution telemetry.** Graph does not expose a
  "this rule fired N times last week" surface. A local heuristic
  (compare folder-state deltas against rule predicates) is
  inferable but out of scope.
- **In-modal rule editor (no `$EDITOR`).** A future spec could
  render an inline rule editor with field-by-field tab navigation
  (similar to spec 13's OOO modal). v1 stays with the `$EDITOR`
  pattern because it is the same as spec 13 / 15 / 27 — one
  fewer UI mode to learn.
- **`isReadOnly = true` rule edit override.** Inkwell will never
  allow editing an admin-pushed rule; the user must use the
  tenant's admin console. This is intentional, not a v1 cut.
