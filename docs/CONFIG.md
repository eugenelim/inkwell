# Configuration Reference

**Document status:** Canonical reference. Every config key in the application is documented here.
**Last updated:** 2026-04-27

This is the single source of truth for configuration. Feature specs that introduce config keys add them here; nothing is configurable that isn't listed in this document.

---

## File locations

| File | Purpose |
| ---- | ------- |
| `~/.config/inkwell/config.toml` | Main user configuration (this document) |
| `~/.config/inkwell/saved_searches.toml` | Named saved searches (spec 11) |
| `~/Library/Application Support/inkwell/mail.db` | SQLite cache (not user-edited) |
| `~/Library/Logs/inkwell/inkwell.log` | Structured logs (not user-edited) |

## Layering and precedence

Lowest to highest precedence:

1. Compiled defaults (everything has a default; the empty config file is valid and runs the app).
2. `config.toml`.
3. Environment variables (selected keys; noted below as `ENV: VAR_NAME`).
4. Command-line flags (selected keys; noted below as `FLAG: --flag-name`).

## Validation behavior

The app refuses to start on:
- Unknown top-level sections or keys.
- Type mismatches (`max_concurrent = "four"`).
- Out-of-range values (e.g., `max_concurrent = 0`).
- Malformed durations (Go `time.ParseDuration` is the parser; `30s`, `5m`, `2h` all valid).
- Missing required keys (only `[account]` keys are required; everything else has defaults).

Errors include the file path and line number.

---

## `[account]` (entirely optional)

Inkwell uses the well-known Microsoft Graph Command Line Tools first-party public client against the multi-tenant `/common` authority by default (PRD §4). The whole `[account]` section is optional; a clean install with no config file can run `inkwell signin` directly. The resolved tenant ID and UPN are persisted to the local SQLite `accounts` row after first sign-in.

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `tenant_id` | string (UUID or `"common"`) | `"common"` | no | Override the authority. Default `common` lets any Entra tenant sign in; the user's actual home tenant is inferred from the `AuthResult`. |
| `client_id` | string (UUID) | `"14d82eec-204b-4c2f-b7e8-296a70dab67e"` | no | Override the OAuth client. Default is the Microsoft-owned Graph CLI Tools app — see PRD §4. Overriding is possible but unsupported. |
| `upn` | string | — | no | If set, the auth layer asserts the signed-in account matches this UPN and refuses otherwise. Useful as a multi-account guardrail. After sign-in the resolved UPN is also written to the local store. |

**Owner spec:** 01.

---

## `[sync]`

Controls the sync engine's polling cadence, backfill, and concurrency.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `foreground_interval` | duration | `"30s"` | 5s–10m | Polling interval when terminal is active. |
| `background_interval` | duration | `"5m"` | 1m–1h | Polling interval when terminal is inactive. |
| `backfill_days` | int | `90` | 7–730 | Days of history to fetch on first launch. |
| `max_concurrent` | int | `4` | 1–10 | Max in-flight Graph requests. **Do not exceed 4** without good reason; 4 is the historic Outlook-resource soft limit. |
| `max_retries` | int | `5` | 1–10 | Per-request retry budget on 429/5xx. |
| `retry_max_backoff` | duration | `"30s"` | 5s–5m | Cap for exponential-backoff retries when no `Retry-After` header is present. |
| `delta_page_size` | int | `100` | 10–1000 | `Prefer: odata.maxpagesize` header value for delta queries. |
| `subscribed_well_known` | list of strings | `["inbox","sentitems","drafts","archive"]` | any well-known names | Well-known folders to sync. User folders are always synced unless excluded by name. |
| `excluded_folders` | list of strings | `["Deleted Items","Junk Email","Conversation History","Sync Issues"]` | any display names | Folders to skip during sync, by display name (case-insensitive). |
| `prioritize_body_fetches` | bool | `true` | — | Whether on-demand body fetches jump the concurrency queue. Disable only if debugging fairness issues. |

**Owner spec:** 03.

---

## `[cache]`

Controls the local SQLite cache.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `body_cache_max_count` | int | `500` | 50–10000 | Max cached message bodies. |
| `body_cache_max_bytes` | int | `209715200` (200MB) | 10MB–10GB | Max total body bytes cached. Eviction triggers on whichever cap hits first. |
| `vacuum_interval` | duration | `"168h"` (7d) | 24h–168h | How often to consider running `VACUUM`. |
| `done_actions_retention` | duration | `"168h"` (7d) | 24h–720h | How long to keep completed action records before cleanup. |
| `mmap_size_bytes` | int | `268435456` (256MB) | 64MB–4GB | SQLite `mmap_size` pragma. |
| `cache_size_kb` | int | `65536` (64MB) | 4MB–1GB | SQLite page cache size. Negative-int convention is hidden in code. |

**Owner spec:** 02.

---

## `[ui]`

Controls the terminal UI.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `theme` | string | `"default"` | `default`, `dark`, `light`, `solarized-dark`, `solarized-light`, `high-contrast` | Color theme preset. Unknown values fall back to `default` with a logged warning. |
| `folder_pane_width` | int | `25` | 15–60 | Width of the folders sidebar in columns. |
| `list_pane_width` | int | `40` | 25–80 | Width of the message list pane in columns. |
| `relative_dates_within` | duration | `"168h"` (7d) | 1h–720h | Show relative dates (e.g., "Sun 14:32") for messages received within this window; absolute dates beyond it. |
| `unread_indicator` | string | `"●"` | any string ≤ 2 chars | Glyph for unread messages. Use `"*"` if your terminal lacks the dot. |
| `flag_indicator` | string | `"⚑"` | any string ≤ 2 chars | Glyph for flagged messages. |
| `attachment_indicator` | string | `"📎"` | any string ≤ 2 chars | Suffix for messages with attachments. Use `"@"` for ASCII-only terminals. |
| `mute_indicator` | string | `"🔕"` | any string ≤ 2 chars | Glyph for messages in muted threads. Use `"m"` for ASCII-only terminals. |
| `show_routing_indicator` | bool | `false` | `true` / `false` | Spec 23 §5.5. Toggles the per-row routing glyph in regular folder views. Always on inside routing virtual folders regardless of this setting. |
| `stream_indicators.imbox` | string | `"📥"` | any string ≤ 2 chars | Spec 23 §5.4. Per-destination glyph for the Imbox stream. Empty falls back to the theme default. |
| `stream_indicators.feed` | string | `"📰"` | any string ≤ 2 chars | Per-destination glyph for the Feed stream. |
| `stream_indicators.paper_trail` | string | `"🧾"` | any string ≤ 2 chars | Per-destination glyph for the Paper Trail stream. |
| `stream_indicators.screener` | string | `"🚪"` | any string ≤ 2 chars | Per-destination glyph for the Screener stream. |
| `stream_ascii_fallback` | bool | `false` | `true` / `false` | When `true`, the four stream indicators are forced to single ASCII letters (`i` / `f` / `p` / `k`) regardless of `stream_indicators.*`. For terminals without emoji support. |
| `reply_later_indicator` | string | `"↩"` | any string ≤ 2 chars | Spec 25 §5.2. Glyph for the Reply Later stack indicator on list rows and in the viewer header. Use `"R"` for ASCII-only terminals. |
| `set_aside_indicator` | string | `"📌"` | any string ≤ 2 chars | Glyph for the Set Aside stack indicator. Use `"P"` for ASCII-only terminals. |
| `focus_queue_limit` | int | `200` | 1–1000 | Spec 25 §5.7.1. Cap on the Reply Later queue pre-fetched by `:focus`. The queue is frozen for the session; rare to exceed 200. |
| `bundle_min_count` | int | `2` | 0–9999 | Spec 26 §5.3. Minimum size of a consecutive same-sender run before it collapses into a bundle row. `0` disables bundling entirely while preserving designations. |
| `bundle_indicator_collapsed` | string | `"▸"` | any string ≤ 2 cells | Spec 26 §5.2. Disclosure glyph on a collapsed bundle row. Use `">"` for ASCII-only terminals. |
| `bundle_indicator_expanded` | string | `"▾"` | any string ≤ 2 cells | Spec 26 §5.2. Disclosure glyph on an expanded bundle row. Use `"v"` for ASCII-only terminals. |
| `screener_hint_dismissed` | bool | `false` | `true` / `false` | Spec 28 §5.3.2. One-shot flag set the first time the user dismisses the post-enable Screener hint with `Esc`. Auto-written by `config.WriteUIFlag`; manual edits are honoured. |
| `screener_last_seen_enabled` | bool | `false` | `true` / `false` | Spec 28 §5.3.1. Marker the first-launch detection compares against `[screener].enabled` to decide whether to render the gate-flip confirmation modal. Auto-written when the user confirms the gate. |
| `transient_status_ttl` | duration | `"5s"` | 1s–60s | How long transient status messages remain visible. |
| `confirm_destructive_default` | string | `"no"` | `yes`, `no` | Default selection on confirmation prompts. `no` is safer; `yes` saves a keystroke for users who want it. |
| `min_terminal_cols` | int | `80` | 60–200 | Below this width, render a "terminal too small" message. |
| `min_terminal_rows` | int | `24` | 20–100 | Below this height, render a "terminal too small" message. |

**Owner spec:** 04.

---

## `[rendering]`

Controls how message bodies are displayed.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `html_converter` | string | `"html2text"` | `html2text`, `lynx`, `w3m`, `external` | HTML → text engine. `external` uses `html_converter_cmd`. |
| `html_converter_cmd` | string | `""` | shell command | Used when `html_converter = "external"`. Receives HTML on stdin, must emit plain text on stdout. |
| `open_browser_cmd` | string | `"open"` | shell command | Command for `:open` (open in browser fallback). On macOS, `open` is correct. |
| `wrap_columns` | int | `0` | 0, 60–200 | Hard-wrap rendered text to this column width. `0` = use viewer pane width. |
| `show_full_headers` | bool | `false` | — | Show every email header (Received, Authentication-Results, etc.) by default. Power users only. |
| `attachment_save_dir` | string | `"~/Downloads"` | path | Default destination for `:save`. Tilde-expanded. |
| `quote_collapse_threshold` | int | `3` | 0–10 | Quote nesting depth at which deeper levels are collapsed in the viewer. `0` disables collapsing. |
| `large_attachment_warn_mb` | int | `25` | 1–1000 | Show a confirmation prompt before downloading attachments larger than this. |
| `strip_patterns` | list of regex strings | `[]` (defaults shipped in code) | regex | Regex patterns to strip from rendered email bodies. Defaults cover common Outlook noise (external email banners, "trouble viewing" preludes). |
| `external_converter_timeout` | duration | `"5s"` | 1s–60s | Timeout when `html_converter = "external"`. On timeout, falls back to in-process renderer. |
| `url_display_max_width` | int | `60` | `0`, 30–300 | Cap the visible OSC 8 hyperlink text in the viewer body at N cells with end-truncation (`https://example.com/auth/…`). The OSC 8 url-portion stays full so Cmd-click + the URL picker (`o`) + the trailing `Links:` block still resolve to the full URL. `0` disables truncation (URLs render full inline). |
| `pretty_tables` | bool | `true` | — | Spec 05 §6.1.1 data-vs-layout classifier: real data tables (with `<th>` or rectangular header-shaped first row) render as ASCII grids; layout tables (MJML newsletters, single-cell wrappers, etc.) flatten to flowing text. `false` flattens every `<table>` (the v0.17.x behavior). |
| `pretty_table_max_rows` | int | `50` | 1–10000 | Row-count ceiling above which a data table is downgraded to `[Wide table — N×M, omitted; press O to view in browser]`. Above the ceiling the grid exceeds a screenful and the user is better served by the browser fallback. |

**Owner spec:** 05.

---

## `[search]`

Controls search behavior.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `local_first` | bool | `true` | — | Run local FTS first and render immediately, then merge server results. |
| `server_search_timeout` | duration | `"5s"` | 1s–60s | Cap on Graph `$search` round-trips. After timeout, show local-only results with a warning. |
| `default_result_limit` | int | `200` | 10–1000 | Max results to display per search. |
| `debounce_typing` | duration | `"200ms"` | 50ms–2s | Delay before running a search as the user types in `/` mode. |
| `merge_emit_throttle` | duration | `"100ms"` | 50ms–1s | Minimum time between UI updates as local + server result streams merge. |
| `default_sort` | string | `"received_desc"` | `received_desc`, `received_asc`, `relevance` | Default sort order for search results. |

**Owner spec:** 06.

---

## `[triage]`

Controls single-message and bulk triage actions.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `archive_folder` | string | `"archive"` | well-known name OR folder display name | Destination folder for `a` (archive). `"archive"` is the well-known Archive folder. |
| `confirm_threshold` | int | `10` | 0–10000 | Bulk operations affecting more than this many messages require confirmation. `0` = always confirm. |
| `confirm_permanent_delete` | bool | `true` | — | Always confirm permanent delete regardless of count. **Strongly recommend leaving on.** |
| `undo_stack_size` | int | `50` | 0–500 | Max session undo entries. `0` = disable undo. |
| `optimistic_ui` | bool | `true` | — | Apply changes locally before Graph confirms. Disable only for debugging. |
| `editor` | string | `""` (uses `$EDITOR`, falls back to `nano`) | shell command | Editor for composing draft replies and OOO messages. |
| `draft_temp_dir` | string | `"~/Library/Caches/inkwell/drafts"` | path | Temporary location for in-progress drafts before Graph confirms creation. |
| `recent_folders_count` | int | `5` | 0–20 | Number of recently-used folders surfaced at the top of the folder picker. |

**Owner spec:** 07.

---

## `[batch]`

Controls $batch execution for bulk operations.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `max_per_batch` | int | `20` | 1–20 | Sub-requests per `$batch` call. **Do not exceed 20** — Graph hard limit. |
| `batch_concurrency` | int | `3` | 1–4 | Number of $batch calls in flight simultaneously. Multiplied by `max_per_batch`, this is the bulk operation throughput. Stays within `[sync].max_concurrent`. |
| `batch_request_timeout` | duration | `"60s"` | 10s–300s | Timeout for a single $batch HTTP call. |
| `dry_run_default` | bool | `false` | — | If true, all destructive bulk operations dry-run by default; user must add `!` to actually execute. |
| `max_retries_per_subrequest` | int | `5` | 1–10 | Maximum 429-retry attempts per individual sub-request within a batch. |
| `bulk_size_warn_threshold` | int | `1000` | 10–10000 | Warn user with time estimate when bulk exceeds this size. |
| `bulk_size_hard_max` | int | `5000` | 100–50000 | Refuse bulk operations that would exceed this size; user must refine pattern. |

**Owner spec:** 09.

---

## `[calendar]`

Controls the read-only calendar pane.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `default_view` | string | `"today"` | `today`, `week`, `agenda` | Initial calendar view. |
| `lookback_days` | int | `7` | 0–365 | Past days fetched. |
| `lookahead_days` | int | `30` | 1–365 | Future days fetched. |
| `time_zone` | string | `""` (use mailbox setting) | IANA tz name | Override the mailbox time zone. Empty = use Graph mailboxSettings. |
| `show_declined` | bool | `false` | — | Show events the user has declined. |
| `sidebar_show_days` | int | `2` | 1–14 | Number of days visible in the sidebar calendar pane (today + N more). |
| `show_tentative` | bool | `true` | — | Show events the user has tentatively accepted. |
| `online_meeting_indicator` | string | `"🔗"` | any string ≤ 2 chars | Glyph for events with online meeting URLs. |
| `now_indicator` | string | `"▶"` | any string ≤ 2 chars | Glyph marking the currently-active event. |
| `cache_ttl` | duration | `"15m"` | 1m–60m | How long the modal trusts locally-cached events before re-fetching from Graph. |

**Owner spec:** 12.

---

## `[mailbox_settings]`

Controls how mailbox-settings UI behaves.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `confirm_ooo_change` | bool | `true` | — | Prompt before toggling out-of-office. |
| `default_ooo_audience` | string | `"all"` | `all`, `internal` | Default audience for new OOO messages. |
| `ooo_indicator` | string | `"🌴"` | any string ≤ 2 chars | Glyph shown in status bar when out-of-office is active. |
| `refresh_interval` | duration | `"5m"` | 1m–1h | How often to re-fetch mailbox settings from Graph. |
| `default_internal_message` | string | `"I'm currently out of the office."` | any | Default message body when toggling OOO via `:ooo on` without explicit text. |
| `default_external_message` | string | `""` (empty = same as internal) | any | Default external message; empty falls back to internal. |

**Owner spec:** 13.

---

## `[cli]`

Controls non-interactive CLI mode.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `default_output` | string | `"text"` | `text`, `json` | Default output format. CLI flag `--output json` overrides. |
| `color` | string | `"auto"` | `auto`, `always`, `never` | Whether to colorize CLI output. |
| `confirm_destructive_in_cli` | bool | `true` | — | Whether destructive CLI commands prompt by default. Set to `false` for scripting; use `--yes` per-invocation. |
| `progress_bars` | string | `"auto"` | `auto`, `always`, `never` | Show progress bars during long operations. `auto` enables on TTY, disables on pipes. |
| `json_compact` | bool | `false` | — | Emit compact (single-line) JSON instead of pretty-printed. |
| `export_default_dir` | string | `"."` | path | Default output directory for `inkwell export`. |

**Owner spec:** 14.

---

## `[pattern]`

Controls the pattern language compiler and executor.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `local_match_limit` | int | `5000` | 100–100000 | Max matches returned by a local-strategy pattern execution. Bulk operations cap separately. |
| `server_candidate_limit` | int | `1000` | 100–10000 | Cap on the candidate set returned by server queries in TwoStage execution. Beyond this, refuse and tell user to refine pattern. |
| `prefer_local_when_offline` | bool | `true` | — | When offline, automatically fall back to local-only strategy with cache-only results. |

**Owner spec:** 08.

---

## `[[saved_searches]]`

A repeatable TOML table for named patterns that surface as virtual
folders in the sidebar (spec 11). v0.7.0 implements the config-driven
form; the DB-backed CRUD path (`Manager` API + TOML mirror + cache TTL
+ background refresh) is post-v0.7.

```toml
[[saved_searches]]
name = "Newsletters"
pattern = "~f newsletter@* | ~f noreply@*"

[[saved_searches]]
name = "Needs Reply"
pattern = "~r me@example.invalid & ~U & ~d <14d"

[[saved_searches]]
name = "Old Heavy Mail"
pattern = "~A & ~d >180d"
```

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `name` | string | (required) | Display name in the sidebar; must be unique. |
| `pattern` | string | (required) | Spec 08 pattern source. Plain text without `~` desugars to `~B <text>`. |

**Owner spec:** 11.

---

## `[saved_search]`

Runtime knobs for the saved-search Manager (spec 11 §9).

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `cache_ttl` | duration | `"60s"` | How long `Evaluate` results are reused before re-querying. |
| `background_refresh_interval` | duration | `"2m"` | How often pinned-search counts refresh in the sidebar, independent of sync events. `"0s"` disables the timer. |
| `seed_defaults` | bool | `true` | Seed `Unread`, `Flagged`, `From me` on first launch when the table is empty. |
| `toml_mirror_path` | string | `"~/.config/inkwell/saved_searches.toml"` | Path for the human-readable TOML snapshot written after every save/delete. Empty string disables. |
| `suggest_save_after_n_uses` | int | `4` | After running the same `:filter` pattern this many times in a session, show a hint to save it as a named rule. `0` disables. |

**Owner spec:** 11.

---

## `[tabs]`

Controls the spec 24 split-inbox tab strip. Tabs are saved searches
promoted to a one-row strip above the list pane; cycle with `]` /
`[` when the list pane is focused.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | `true` | true / false | When `false`, the strip is hidden even if tabs are configured (escape hatch — the tabs themselves persist). |
| `show_zero_count` | bool | `false` | true / false | When `true`, render `[Name 0]` for tabs with no unread; default hides the zero. |
| `max_name_width` | int | `16` | ≥ 4 | Per-tab name truncation width (with `…`). |
| `cycle_wraps` | bool | `true` | true / false | When `false`, `]` at the last tab and `[` at the first tab no-op instead of wrapping. |

Promote a saved search via `:tab add <name>` (cmd-bar) or
`inkwell tab add <name>` (CLI). The `[bindings].next_tab` /
`[bindings].prev_tab` keys default to `]` / `[`; both are
list-pane-scoped (the viewer pane keeps `]` / `[` for thread
navigation, the calendar pane keeps them for day navigation).

**Owner spec:** 24.

---

## `[bulk]`

Controls bulk-operation UX.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `preview_sample_size` | int | `5` | 0–50 | Number of sample messages shown in the bulk-confirm modal. |
| `progress_threshold` | int | `50` | 1–10000 | Show a progress modal for bulks larger than this. Smaller bulks finish too fast for the modal to be useful. |
| `progress_update_hz` | int | `10` | 1–60 | Maximum progress UI update frequency, in Hz. |
**Owner spec:** 10.

---

## `[logging]`

Controls log output and redaction.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `level` | string | `"info"` | `debug`, `info`, `warn`, `error` | Minimum level. ENV: `INKWELL_LOG_LEVEL`. FLAG: `--log-level`. |
| `path` | string | `"~/Library/Logs/inkwell/inkwell.log"` | path | Log file location. |
| `max_size_mb` | int | `10` | 1–500 | Rotate at this size. |
| `max_backups` | int | `5` | 0–50 | Number of rotated archives to keep. |
| `redact_email_addresses` | bool | `true` | — | Replace email addresses with `<email-N>` placeholders in logs. **Strongly recommend leaving on.** |
| `redact_subjects` | bool | `true` | — | Suppress subject lines except at DEBUG level. |
| `console_output` | bool | `false` | — | Also write logs to stderr (useful in development). |

**Owner spec:** ARCH §12 (cross-cutting).

---

## `[bindings]`

Override default keybindings. Each value is a single key or comma-separated alternatives.

Keys are drawn from the `key.Binding` description in `internal/ui/keys.go`. Anything overridable in the keymap appears here. **Defaults documented in spec 04.** A subset:

| Key | Default | Notes |
| --- | --- | --- |
| `quit` | `"q"` | Active in normal mode, list pane. |
| `force_quit` | `"ctrl+c"` | Always active. |
| `cmd` | `":"` | Enter command mode. |
| `search` | `"/"` | Enter search mode (`/`). |
| `refresh` | `"ctrl+r"` | Force a sync. |
| `up` | `"k,up"` | |
| `down` | `"j,down"` | |
| `left` | `"h,left"` | |
| `right` | `"l,right"` | |
| `page_up` | `"ctrl+u"` | |
| `page_down` | `"ctrl+d"` | |
| `home` | `"g g"` | Compound binding. |
| `end` | `"G"` | |
| `next_pane` | `"tab"` | |
| `prev_pane` | `"shift+tab"` | |
| `mark_read` | `"r"` | List pane only; in viewer = reply. Pane-scoped. |
| `mark_unread` | `"R"` | List pane only; in viewer = reply-all. Pane-scoped. |
| `toggle_flag` | `"f"` | List pane: toggle flag. Viewer pane: forward. Pane-scoped. |
| `delete` | `"d"` | Soft delete. |
| `permanent_delete` | `"D"` | Hard delete. Always confirms. |
| `archive` | `"a"` | |
| `move` | `"m"` | |
| `add_category` | `"c"` | |
| `remove_category` | `"C"` | |
| `undo` | `"u"` | Pop the last triage from the undo stack. |
| `filter` | `"F"` | Bulk filter mode. |
| `clear_filter` | `"esc"` | Clear active filter. |
| `apply_to_filtered` | `";"` | Mutt-style tag-prefix. |
| `unsubscribe` | `"U"` | RFC 8058 one-click / mailto / browser flow. Spec 16. |
| `mute_thread` | `"M"` | Toggle mute on the focused message's conversation. Spec 19. |
| `thread_chord` | `"T"` | Begin thread chord (T+r/R/f/F/d/D/a/m). Spec 20. |
| `stream_chord` | `"S"` | Begin stream chord (S+i/f/p/k/c) — route the focused sender to Imbox / Feed / Paper Trail / Screener, or clear routing. Spec 23. |
| `next_tab` | `"]"` | Cycle to the next spec 24 tab when the list pane is focused. Pane-scoped — viewer pane keeps `]` for thread navigation, calendar pane keeps it for day navigation. |
| `prev_tab` | `"["` | Cycle to the previous spec 24 tab when the list pane is focused. Same pane-scoping as `next_tab`. |
| `reply_later_toggle` | `"L"` | Spec 25. Toggle the focused message in the Reply Later stack (Inkwell/ReplyLater category). |
| `set_aside_toggle` | `"P"` | Spec 25. Toggle the focused message in the Set Aside stack (Inkwell/SetAside category). Mnemonic: Pin (matches the 📌 indicator). Spec text suggested `S`; deviated because spec 23's stream chord already claimed `S`. |
| `bundle_toggle` | `"B"` | Spec 26 §5.1. List-pane only. Toggle per-sender bundle designation on the focused message's address. |
| `bundle_expand` | `" "` | Spec 26 §5.1. List-pane only, when the focused row is a bundle header. Toggles expand/collapse. Shares Space with `expand` (folders pane) by intent — pane dispatch resolves which fires. |
| `screener_accept` | `"Y"` | Spec 28 §5.4. Pane-scoped to the Screener virtual folder while `[screener].enabled` is true. Equivalent to the `S i` chord — admit the focused sender to Imbox. |
| `screener_reject` | `"N"` | Spec 28 §5.4. Pane-scoped to the Screener virtual folder while `[screener].enabled` is true. Equivalent to the `S k` chord — screen out the focused sender. Overlaps `new_folder` (spec 18) on capital N; pane scoping disambiguates at dispatch time. |
| `palette` | `"ctrl+k"` | Open the spec 22 command palette (fuzzy-find every action; right-aligned binding column doubles as a passive cheatsheet). Set to `""` to disable. |
| `help` | `"?"` | Open the help overlay (every binding). |

**Pane-scoped bindings** (e.g., `r`, `R`, `f`) have different actions in list vs. viewer panes by design — see spec 04 §5 and spec 07 §12. Overriding them via this section changes the binding in BOTH panes; per-pane override is not supported in v1.

**Unknown keys are a startup error.** A typo like `mark_red = "r"` (instead of `mark_read`) produces `config <path>: unknown key(s): bindings.mark_red` at startup. Spec 04 §17 invariant — without this gate, the typo would silently no-op and the user couldn't tell why their override didn't take.

**Duplicate bindings are also a startup error.** If two distinct actions resolve to the same key (e.g. `delete = "x"` and `archive = "x"`), `ui.New` returns `bindings: key "x" bound to multiple actions` and the binary refuses to start. Pane-scoped sharing (e.g. `r` for mark-read in list and reply in viewer) is still allowed because both resolve to the same `mark_read` field.

I changed `refresh` from `"R"` (which conflicted with mark_unread) to `"ctrl+r"`. The original "Conflicts with `mark_unread` resolved by pane" was clever but causes user confusion; `ctrl+r` is unambiguous.

**Owner spec:** 04 (binding mechanics), 07 (action bindings), 10 (bulk bindings).

---

## `[compose]`

Controls the in-modal compose pane (spec 15 F-1). Drafts are created server-side via Graph; inkwell never calls `Mail.Send`.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `attachment_max_size_mb` | int | `25` | 1–100 | Maximum attachment file size in MB. Files larger than this limit are rejected before the Graph upload attempt (spec 15 §5 / spec 17 §4.4). |
| `max_attachments` | int | `20` | 1–100 | Maximum number of attachments per draft. Graph enforces its own limit server-side; this client-side gate provides an early error. |
| `web_link_ttl` | duration | `"30s"` | 0s–300s | How long the "press s to open in Outlook / D to discard" status-bar hint persists after a successful draft save. `0` disables auto-clear (hint stays until the next compose action). |

**Owner spec:** 15.

---

## `[custom_actions]`

Spec 27. Points the loader at the user's `actions.toml` recipe file.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `file` | string | `""` | filesystem path | Override path to the custom actions TOML. Empty falls back to `~/.config/inkwell/actions.toml`. Missing file is not an error — the framework loads zero recipes. Validation failures abort startup with the offending file:line. |

The recipe schema (op catalogue, template variables, confirm policies) lives in `docs/user/reference.md` and `docs/user/how-to.md` because it is a separate file format from `config.toml`.

**Owner spec:** 27.

---

## `[screener]`

Spec 28. The first-contact gate: when enabled, mail from senders not in `sender_routing` OR routed to `'screener'` is hidden from default folder views and surfaces in the Screener / Screened-Out virtual folders. Off by default — flipping the flag without a routing pass first is the most common surprise; the gate-flip confirmation modal (§5.3.1) renders at the next launch when there are pending messages to hide.

| Key | Type | Default | Range | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | `false` | `true` / `false` | Master gate flag. When true, default folder views call `ListMessages` with `ApplyScreenerFilter=true` and the `__screener__` sentinel content shifts from "screener-routed senders" to "pending senders." |
| `grouping` | string | `"sender"` | `"sender"` / `"message"` | Screener queue rendering. `"sender"` shows one row per pending sender (with a count badge); `"message"` shows one row per pending message — useful for triaging individual messages before committing to a per-sender routing. |
| `exclude_muted` | bool | `true` | `true` / `false` | Excludes muted-thread messages from the Screener queue. Mute is a stronger signal than "no decision yet"; treating it as such avoids muted threads popping back demanding a decision. |
| `max_count_per_sender` | int | `999` | 1–9999 | Cap on the per-sender message-count display in the Screener queue. Counts above this render as `999+`. Performance safeguard — the SQL count subquery short-circuits at `cap+1`. |

**Owner spec:** 28.

---

## Example complete config

A user's `~/.config/inkwell/config.toml` overriding defaults:

```toml
# [account] is optional. Inkwell uses the Microsoft Graph CLI Tools
# first-party public client against /common by default. Set upn here
# only if you want a guardrail against signing in as the wrong account.
[account]
upn = "eu.gene@example.invalid"

[sync]
foreground_interval = "20s"
backfill_days = 180

[ui]
theme = "auto"
folder_pane_width = 30
attachment_indicator = "@"

[rendering]
html_converter = "external"
html_converter_cmd = "pandoc -f html -t plain"

[triage]
confirm_threshold = 25

[bindings]
delete = "x"            # vim-style cut
permanent_delete = "X"
```

Everything not listed uses defaults from this document.

---

## Adding a new config key

When a feature spec needs a new key:

1. Choose the right section, or define a new one in this document with rationale.
2. Add a row to that section's table: name, type, default, range, description.
3. Add the `[section].key` to `internal/config/defaults.go` with the same default.
4. Add validation logic in `internal/config/validate.go`.
5. Reference the key by `[section].key` in the feature spec text.
6. The PR for the feature spec MUST include the CONFIG.md change in the same commit.
