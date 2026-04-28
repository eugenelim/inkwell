# Spec 13 — Mailbox Settings

**Status:** Ready for implementation.
**Depends on:** Specs 03 (graph client), 04 (TUI shell, modals).
**Blocks:** Nothing.
**Estimated effort:** 1 day.

---

## 1. Goal

Surface `MailboxSettings.Read` and `MailboxSettings.ReadWrite` capabilities in the UI: view and toggle out-of-office (automatic replies), display working hours and time zone, and show the user's locale. Small but high-value: a senior professional often forgets to set OOO before flying out; one keystroke from the terminal beats finding the right Outlook submenu.

## 2. Scope

What we expose:

- **Automatic replies** — toggle on/off, set internal/external messages, schedule with start/end. (`MailboxSettings.ReadWrite`)
- **Working hours** — read-only display in the status bar context. (`MailboxSettings.Read`)
- **Time zone, language, date format** — read-only; used by other specs (calendar display, date formatting).

What we don't:

- Reading rules / forwarding rules. Available via `MailboxSettings.Read` but rules are conceptually a different feature (server-side rules vs. our client-side saved searches); not in v1.
- Resource scheduling permissions, delegate access. Not relevant to a single-user mail client.
- Inferenceclassification override. Whether a sender is "Focused" or "Other" is server-determined; we display but don't change.

## 3. Module layout

```
internal/graph/
└── mailbox.go         # /me/mailboxSettings REST helpers

internal/store/
└── settings.go        # cached settings; refreshed periodically

internal/ui/
├── settings_view.go   # display modal
└── ooo_modal.go       # OOO edit modal
```

## 4. Graph endpoints

```
GET  /me/mailboxSettings
PATCH /me/mailboxSettings
```

The mailbox settings object includes:

- `automaticRepliesSetting` — status (`disabled`/`alwaysEnabled`/`scheduled`), schedule, internal/external messages, audience.
- `workingHours` — days, time range, time zone.
- `timeZone` — IANA tz name.
- `language` — locale.
- `dateFormat`, `timeFormat` — display preferences.
- `delegateMeetingMessageDeliveryOptions` — n/a for our use.

We fetch the full object on app start, cache it in memory (no DB persistence — small, refreshable, tightly coupled to live state). Refresh on a 5-minute timer; force refresh after any PATCH.

## 5. UI surface

### 5.1 Status line indicator

When automatic replies are enabled, the status bar gets an OOO indicator:

```
☰ inkwell · ueg@example.invalid · 🌴 OOO · ✓ synced 14:32
```

The `🌴` glyph (configurable via `[mailbox_settings].ooo_indicator`) is muted/colored to draw the eye. Hovering — well, terminals don't have hover, but pressing `:status` shows full settings.

### 5.2 Settings view

`:settings` opens a read-only summary modal:

```
   ╭─────────────────────────────────────────────────────────────╮
   │  Mailbox Settings                                            │
   │                                                              │
   │  Automatic Replies:    Disabled         [press o to edit]   │
   │  Time Zone:            America/New_York                      │
   │  Locale:               en-US                                 │
   │  Date Format:          M/d/yyyy                              │
   │  Time Format:          h:mm tt                               │
   │  Working Hours:        Mon–Fri 09:00–17:00 EST               │
   │                                                              │
   │  [o] Edit OOO    [Esc] Close                                │
   ╰─────────────────────────────────────────────────────────────╯
```

### 5.3 OOO edit modal

`o` from settings, or directly via `:ooo`:

```
   ╭─────────────────────────────────────────────────────────────╮
   │  Automatic Replies                                           │
   │                                                              │
   │  Status:    ( ) Off                                          │
   │             ( ) On (no schedule)                             │
   │             (•) On with schedule                             │
   │                                                              │
   │  Start:     2026-04-28  09:00  (Mon)                         │
   │  End:       2026-05-03  17:00  (Sat)                         │
   │                                                              │
   │  Audience:  (•) Internal contacts and external senders       │
   │             ( ) Internal contacts only                       │
   │             ( ) Internal and known external (contact list)   │
   │                                                              │
   │  Internal message:                                           │
   │  ┌─────────────────────────────────────────────────────────┐ │
   │  │ I'm out of the office this week. Please reach out to    │ │
   │  │ Alice Smith (alice@example.invalid) for urgent matters.   │ │
   │  │ I'll respond when I'm back on May 4.                    │ │
   │  └─────────────────────────────────────────────────────────┘ │
   │                                                              │
   │  External message:                                           │
   │  ┌─────────────────────────────────────────────────────────┐ │
   │  │ Thanks for your message. I'm currently out of the office │ │
   │  │ and will respond when I return on May 4.                 │ │
   │  └─────────────────────────────────────────────────────────┘ │
   │                                                              │
   │  [Tab] Next field   [Enter] Save   [e] Open in $EDITOR       │
   │  [Esc] Cancel                                                │
   ╰─────────────────────────────────────────────────────────────╯
```

Three status modes mapped to Graph's `automaticRepliesSetting.status`:
- `disabled` — Off
- `alwaysEnabled` — On (no schedule)
- `scheduled` — On with schedule (requires start/end)

Audience maps to `externalAudience`:
- `all` — Internal and external (any sender)
- `none` — Internal only
- `contactsOnly` — Internal and known external

For multi-line message fields, pressing `e` suspends the TUI to `$EDITOR` (same pattern as draft composition in spec 07 §9.1). The user edits in vim/nano/whatever, saves, returns to the modal with the updated text.

### 5.4 Save flow

`Enter` saves:

1. Validate: if `scheduled`, both start and end required, end must be after start.
2. PATCH `/me/mailboxSettings`:
   ```json
   {
     "automaticRepliesSetting": {
       "status": "scheduled",
       "scheduledStartDateTime": { "dateTime": "2026-04-28T09:00:00", "timeZone": "America/New_York" },
       "scheduledEndDateTime":   { "dateTime": "2026-05-03T17:00:00", "timeZone": "America/New_York" },
       "externalAudience": "all",
       "internalReplyMessage": "...",
       "externalReplyMessage": "..."
     }
   }
   ```
3. On success: refresh cached settings, close modal, status bar updates, toast: "✓ Out-of-office set."
4. On failure: keep modal open, show error inline.

### 5.5 Quick toggle

`:ooo on` and `:ooo off` are quick commands that flip status without the modal:

- `:ooo on` — set status to `alwaysEnabled`. Uses last-saved messages if any, else minimal default ("I'm currently out of the office.").
- `:ooo off` — set status to `disabled`.

This is the "I just realized I forgot, the cab is here" use case. One typed command from any pane and you're done.

`:ooo schedule` opens the full modal with `scheduled` selected.

## 6. Confirmation

If `[mailbox_settings].confirm_ooo_change = true` (default), saves via the modal show a one-line confirm prompt at the bottom:

```
Save these changes? [y/N]
```

Quick toggles via `:ooo on/off` confirm with a y/N prompt unless `--yes` flag provided:

```
:ooo on
> Enable out-of-office? Internal: "I'm currently out of the office." [y/N]
```

`:ooo on --yes` skips the prompt.

## 7. Time zone and locale handling

### 7.1 The TZ relationship

Multiple specs use time zone:
- Spec 06 (search) for date predicates.
- Spec 12 (calendar) for event display.
- Spec 04 (UI) for "received 3h ago" relative dates.

Source of truth precedence:
1. `[calendar].time_zone` if explicitly set.
2. `mailboxSettings.timeZone` from Graph.
3. System local time zone.

The settings module exposes:

```go
package settings

func (m *Manager) ResolvedTimeZone() *time.Location
```

All other specs call this rather than reading config directly. This single resolution point ensures consistency.

### 7.2 Locale

Used for:
- Formatting dates in the message list (uses `dateFormat` and `timeFormat` from mailboxSettings if available; falls back to ISO 8601).
- Currency / number formatting in the (rare) places we render those.

## 8. Configuration

This spec owns the `[mailbox_settings]` section. New keys for CONFIG.md:

| Key | Default | Used in § |
| --- | --- | --- |
| `mailbox_settings.confirm_ooo_change` | `true` | §6 |
| `mailbox_settings.default_ooo_audience` | `"all"` | §5.3 (default for new OOO) |
| `mailbox_settings.ooo_indicator` | `"🌴"` | §5.1 |
| `mailbox_settings.refresh_interval` | `"5m"` | §4 |
| `mailbox_settings.default_internal_message` | `"I'm currently out of the office."` | §5.5 |
| `mailbox_settings.default_external_message` | `""` (empty = same as internal) | §5.5 |

## 9. CLI mode

```bash
inkwell ooo                      # show current state
inkwell ooo on                   # enable, no schedule
inkwell ooo on --until 2026-05-03  # enable, scheduled
inkwell ooo off                  # disable
inkwell ooo set --internal "I'm out" --external "I'm out, sorry"

inkwell settings                 # show all settings
inkwell settings --output json   # for scripting
```

CLI never prompts; destructive changes require explicit flags (`--yes` to skip confirmation noise, but the model already assumes scripted use).

## 10. Failure modes

| Scenario | Behavior |
| --- | --- |
| User edits OOO offline | Save fails with network error; modal stays open with retry hint. (We don't queue OOO changes through the action queue — they're rare and benefit from explicit feedback.) |
| User saves OOO with malformed time | Validation catches before PATCH; inline error. |
| Time zone field can't be parsed | Use system TZ as fallback; status line shows `⚠ TZ from server unparseable`. |
| OOO set with end-date in the past | Server accepts; we warn before save: "End date is in the past. Set anyway?" |
| External message empty but internal set, audience=`all` | Default external to internal text; warn user. |
| User has tenant policy restricting OOO content | Save fails with 403; surface tenant's error message verbatim. |
| Mailbox settings refresh fails | Cached values continue serving; log warning. |

## 11. Test plan

### Unit tests

- Modal field validation (start before end, required fields).
- Status mapping (UI radio choice ↔ Graph status enum).
- Audience mapping.
- Time zone resolution precedence.

### Integration tests

- Mock Graph PATCH; assert correct payload for various modal inputs.
- Refresh: initial GET populates cache; subsequent calls cached until interval.

### Manual smoke tests

1. `:settings` shows current state matches Outlook web settings.
2. `:ooo on` enables; verify in OWA.
3. `:ooo off` disables.
4. `:ooo` with full modal: schedule for next week; save; verify in OWA.
5. `e` in a message field opens $EDITOR; save; modal repopulates.
6. Try invalid date range; validation catches.
7. Refresh interval: edit OOO in OWA; wait <5 min; observe status bar update.

## 12. Definition of done

- [ ] `:settings` modal renders all read fields.
- [ ] `:ooo` modal supports all three status modes, both audience options, both message types.
- [ ] Editor integration for message bodies works with $EDITOR.
- [ ] `:ooo on`, `:ooo off`, `:ooo schedule` quick commands.
- [ ] Status bar indicator appears when OOO active.
- [ ] CLI commands work end-to-end.
- [ ] Time zone resolution centralized; calendar and search both use it.
- [ ] All failure modes handled.

## 13. Out of scope

- Mail rules / forwarding rules (would be a separate spec; v1 uses saved searches as the client-side analog).
- Delegate access management.
- Resource scheduling.
- Inferenceclassification override.
- Multiple OOO templates ("template: vacation", "template: training").
