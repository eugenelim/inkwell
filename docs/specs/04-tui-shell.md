# Spec 04 — TUI Shell

**Status:** Ready for implementation.
**Depends on:** Specs 01 (auth), 02 (store), 03 (sync). ARCH §10 (UI architecture).
**Blocks:** Specs 05 (rendering), 07 (triage), 10 (bulk ops UX), 11 (saved searches sidebar), 12 (calendar pane), 13 (settings UI).
**Estimated effort:** 3 days.

---

## 1. Goal

Build the Bubble Tea application skeleton: root model, panes, keymap, command bar, status line, and the message-passing pipeline that connects the UI to the sync engine and store. This spec delivers a *navigable* mail client — the user can sign in, see folders, see messages, scroll through them. It does NOT implement message viewing (spec 05), triage actions (spec 07), or search (spec 06); those plug into the framework this spec creates.

## 2. Layout

```
┌────────────────────────────────────────────────────────────────────────────┐
│ ☰ inkwell · ueg@example.invalid · ✓ synced 14:32                            │ ← status (1 row)
├──────────────┬─────────────────────────────────┬───────────────────────────┤
│ ▸ Inbox  47 │ Sun 14:32  Bob Acme              │ From: Bob Acme            │
│   Drafts    │   Q4 forecast                    │ To: ueg@example.invalid     │
│   Sent      │   Hey, attached the deck for...  │ Subject: Q4 forecast      │
│   Archive   │ Sun 11:15  newsletter@vendor     │ Date: Sun 14:32           │
│ ▾ Clients   │   This week in tech              │                           │
│   ▸ TIAA   │ Sat 09:02  Alice Smith           │ Hey,                      │
│   ▸ ACME   │   Re: deployment plan            │                           │
│   Newsletters│ Fri 17:48  CI Bot               │ Attached the deck for     │
│              │   Build #4521 succeeded          │ tomorrow's review.        │
│              │ ...                              │ ...                       │
├──────────────┴─────────────────────────────────┴───────────────────────────┤
│ :                                                                          │ ← command (1 row)
└────────────────────────────────────────────────────────────────────────────┘
   ↑              ↑                                ↑
   folders pane   list pane                        viewer pane
   (default 25w) (default 40w)                     (remaining width)
```

Three panes plus a status line and command line. Pane widths are configurable in `[ui]` config.

## 3. State model

```go
package ui

type Model struct {
    // Subsystems (injected)
    store store.Store
    sync  sync.Engine
    auth  auth.Authenticator

    // Layout
    width  int
    height int
    paneWidths PaneWidths

    // Sub-models
    folders FoldersModel
    list    ListModel
    viewer  ViewerModel
    cmd     CommandModel
    status  StatusModel

    // Global state
    activeAccount  *store.Account
    focused        Pane         // FoldersPane | ListPane | ViewerPane | CommandLine
    mode           Mode         // Normal | Command | Search | SignIn | Confirm
    confirm        *ConfirmModel  // populated when mode == Confirm
    signin         *SigninModel   // populated when mode == SignIn

    // Cross-cutting
    keymap KeyMap
    theme  Theme
    log    *slog.Logger
}

type Pane int
const (
    FoldersPane Pane = iota
    ListPane
    ViewerPane
    CommandLine
)

type Mode int
const (
    NormalMode Mode = iota
    CommandMode    // : prompt active
    SearchMode     // / prompt active (forward incremental search within list)
    SignInMode     // device code modal
    ConfirmMode    // y/N prompt
)
```

The root `Model` is the single source of truth for app state. Sub-model fields are *value types*, not pointers, because Bubble Tea's update cycle returns a new model — pointer aliasing across updates has caused real bugs in real Bubble Tea apps. Compose via value semantics.

## 4. The root Update function

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // First: handle messages that always apply regardless of mode
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        m = m.relayout()
        return m, nil
    case tea.QuitMsg:
        return m, tea.Quit
    case sync.Event:
        m = m.handleSyncEvent(msg)
        return m, nil
    case authRequiredMsg:
        m.mode = SignInMode
        m.signin = newSigninModel()
        return m, m.signin.Init()
    }

    // Then: dispatch by mode
    switch m.mode {
    case SignInMode:
        return m.updateSignIn(msg)
    case ConfirmMode:
        return m.updateConfirm(msg)
    case CommandMode:
        return m.updateCommand(msg)
    case SearchMode:
        return m.updateSearch(msg)
    case NormalMode:
        return m.updateNormal(msg)
    }
    return m, nil
}
```

Mode-dispatch keeps modal state strict. The user cannot accidentally trigger a folder pane keybind while typing in the command line.

## 5. Keymap

Defined in `internal/ui/keys.go`. Uses `bubbles/key`:

```go
type KeyMap struct {
    // Global
    Quit          key.Binding  // q (in normal mode), Ctrl+C (anywhere)
    Help          key.Binding  // ?
    Cmd           key.Binding  // :
    Search        key.Binding  // /
    Refresh       key.Binding  // Ctrl+R (force sync)

    // Pane focus
    FocusFolders  key.Binding  // 1
    FocusList     key.Binding  // 2
    FocusViewer   key.Binding  // 3
    NextPane      key.Binding  // Tab
    PrevPane      key.Binding  // Shift+Tab

    // Movement (in any pane)
    Up            key.Binding  // k, ↑
    Down          key.Binding  // j, ↓
    Left          key.Binding  // h, ← (collapse folder, prev message)
    Right         key.Binding  // l, → (expand folder, open message)
    PageUp        key.Binding  // Ctrl+U
    PageDown      key.Binding  // Ctrl+D
    Home          key.Binding  // g g
    End           key.Binding  // G

    // List actions (placeholders; full bindings + pane scoping in spec 07)
    Open          key.Binding  // Enter
    MarkRead      key.Binding  // r (list); reply (viewer) — pane-scoped per spec 07
    MarkUnread    key.Binding  // R (list); reply-all (viewer) — pane-scoped per spec 07
    ToggleFlag    key.Binding  // f (list = toggle flag); forward (viewer) — pane-scoped per spec 07
    Delete        key.Binding  // d
    PermDelete    key.Binding  // D
    Archive       key.Binding  // a
    Move          key.Binding  // m
    AddCategory   key.Binding  // c
    RemoveCategory key.Binding // C
    Undo          key.Binding  // u
    UndoStack     key.Binding  // U

    // Search/filter (full in spec 06, 10)
    Filter        key.Binding  // F (i.e., Shift+f)
    ClearFilter   key.Binding  // Esc when filter active
    ApplyToFiltered key.Binding // ; (Mutt-style tag-prefix)
}
```

**Pane-scoped bindings.** The keymap exposes a single binding name per action (e.g., `MarkRead`), but the action it triggers depends on which pane has focus. The global handler dispatches in this order: (1) pane-specific override → (2) global default. Each pane registers its overrides at registration time. This is how `r` means "mark read" in the list pane and "reply" in the viewer pane without needing two separate bindings.

**Refresh:** `Ctrl+R`, not `R`. An earlier draft had `R` triggering refresh in the folders pane and mark-unread in the list pane; user testing showed this was confusing. `Ctrl+R` is unambiguous.

User-overridable in config:

```toml
[bindings]
quit = "q"
delete = "d"
permanent_delete = "D"
# etc.
```

## 6. Sub-models

### 6.1 FoldersModel

Hierarchical tree of folders. Uses a flattened-tree rendering: each visible row knows its depth.

```go
type FoldersModel struct {
    folders []store.Folder      // all folders, ordered
    visible []folderRow         // flattened, accounting for collapsed state
    expanded map[string]bool    // by folder ID
    cursor int                  // index into visible
    width  int
    height int
}

type folderRow struct {
    folder store.Folder
    depth  int
    hasChildren bool
}
```

Rendering:
- Folder name, with `▸`/`▾` glyphs for expandable.
- Unread count appended in muted color: `Inbox  47`.
- Selected folder highlighted (bg color) when this pane has focus.

Init: load all folders from store. Refresh on `FolderSyncedEvent` from sync engine. Emit `folderSelectedMsg` upward when user presses Enter or Right.

### 6.2 ListModel

The message list for the currently selected folder.

```go
type ListModel struct {
    messages []store.Message
    cursor   int            // index into messages
    viewport viewport.Model // bubbles/viewport for windowed scrolling
    folderID string

    // Filter state (extended by spec 10)
    filter      *pattern.Compiled  // nil = no filter
    filterCount int

    // Multi-select (spec 10)
    selected map[string]bool      // by message ID

    width  int
    height int
}
```

Each row is one line:

```
Sun 14:32  ●  Bob Acme              Q4 forecast — Hey, attached the deck for...
```

Columns:
- Date (relative for recent, absolute for older). 11 chars.
- Read indicator (`●` unread, ` ` read). 2 chars.
- Sender name (or address if no name). 20 chars truncated.
- Subject + bodyPreview joined with em-dash. Remainder of width.

Selected message highlighted. Flagged messages prefixed with `⚑`. Messages with attachments suffixed with `📎` (or `@` if no Unicode).

Pagination: load 200 messages from store at a time. Scroll past the bottom triggers a load of the next 200. Total folder count shown in status line.

### 6.3 ViewerModel

Placeholder in this spec — full implementation in spec 05 (rendering). For now, render a basic header + body preview:

```
From: Bob Acme <bob@acme.com>
To:   ueg@example.invalid
Date: Sun 2026-04-26 14:32
Subj: Q4 forecast

Hey, attached the deck for tomorrow's review. Let me know if you spot...
```

Plus an "[E]xpand body" hint that, in spec 05, will trigger the full body fetch.

### 6.4 CommandModel

A `bubbles/textinput` for `:` commands. Single line, no history navigation in v1 (history is post-v1). Renders at the bottom of the screen; visible only in `CommandMode`.

Commands routed through a dispatcher table:

```go
var commandTable = map[string]CommandHandler{
    "quit":      cmdQuit,
    "q":         cmdQuit,
    "signin":    cmdSignin,
    "signout":   cmdSignout,
    "refresh":   cmdRefresh,
    "sync":      cmdSync,
    "folder":    cmdFolder,
    "filter":    cmdFilter,
    "search":    cmdSearch,
    "open":      cmdOpen,
    "save":      cmdSave,
    "rule":      cmdRule,
    "backfill":  cmdBackfill,
    "ooo":       cmdOutOfOffice,
    "cal":       cmdCalendar,
    "help":      cmdHelp,
}
```

Each handler returns a `tea.Cmd`. Unknown commands show an error in the status line.

### 6.5 StatusModel

Two zones:

- **Left:** account info, sync state, current folder + counts.
- **Right:** transient messages (last sync time, last error, current mode hint).

```go
type StatusModel struct {
    account     string
    syncState   SyncState  // Idle | Syncing | Error
    syncMsg     string     // last sync summary
    folder      string
    folderTotal int
    folderUnread int
    transient   string     // ephemeral; clears after 5s
}
```

Transient messages set via `SetTransient(msg, ttl)`. The model emits a `clearTransientMsg` after `ttl` to itself.

## 7. Sign-in flow

When `Init()` runs, the model calls `auth.Token(ctx)` with a 1-second timeout. If a valid token comes back, normal app start. Otherwise:

```go
type SigninModel struct {
    state SigninState  // PromptDisplayed | Polling | Failed
    code  string
    url   string
    expires time.Time
    err   error
}
```

Renders centered modal (using lipgloss `Place`):

```
    ╭──────────────────────────────────────────╮
    │                                          │
    │  Sign in to Microsoft 365                │
    │                                          │
    │  Visit:  https://microsoft.com/devicelogin
    │  Code:   ABC123XYZ                       │
    │                                          │
    │  Code expires in 14:32                   │
    │                                          │
    │  Press Esc to cancel                     │
    ╰──────────────────────────────────────────╯
```

Sign-in is driven by the `auth` package's `PromptFn` callback (spec 01). The TUI implementation of `PromptFn`:

```go
func (m *Model) signinPrompt(ctx context.Context, p auth.DeviceCodePrompt) error {
    return m.send(showSigninMsg{prompt: p})  // routed back into Update
}
```

`auth.Token` blocks until completion; the TUI updates concurrently with a goroutine that emits the final result as a `signinDoneMsg`.

On success: transition to `NormalMode`, kick off `sync.Engine.Start()`.
On failure: surface the error in the modal with a "retry" hint.

## 8. Confirmation flow

Modal y/N prompt, used by destructive operations:

```go
type ConfirmModel struct {
    prompt  string
    detail  string  // shown beneath, e.g., "This will permanently delete 247 messages."
    onYes   tea.Cmd
    onNo    tea.Cmd
}
```

Renders centered:

```
    ╭──────────────────────────────────────────╮
    │  Permanently delete 247 messages?         │
    │                                          │
    │  This action cannot be undone.            │
    │                                          │
    │  [y] Yes    [n] No                       │
    ╰──────────────────────────────────────────╯
```

Only `y`, `Y`, `n`, `N`, and `Esc` are accepted. `Esc` → onNo.

## 9. Sync event handling

The model subscribes to `sync.Engine.Notifications()`. Each event becomes a `tea.Msg`:

```go
func subscribeSync(e sync.Engine) tea.Cmd {
    return func() tea.Msg {
        ev := <-e.Notifications()
        return syncEventMsg{ev: ev}
    }
}

func (m Model) handleSyncEvent(ev sync.Event) Model {
    switch ev := ev.(type) {
    case sync.SyncStartedEvent:
        m.status.syncState = Syncing
    case sync.SyncCompletedEvent:
        m.status.syncState = Idle
        m.status.syncMsg = fmt.Sprintf("✓ synced %d folders %s", ev.FoldersSynced, ev.At.Format("15:04"))
    case sync.FolderSyncedEvent:
        // If this is the currently displayed folder, reload its messages
        if ev.FolderID == m.list.folderID {
            m.list.refresh(m.store) // re-query store
        }
        // Always update folder unread counts in sidebar
        m.folders.refresh(m.store)
    case sync.SyncFailedEvent:
        m.status.syncState = Error
        m.status.transient = "⚠ sync failed: " + ev.Err.Error()
    case sync.AuthRequiredEvent:
        m.mode = SignInMode
        m.signin = newSigninModel()
    case sync.ThrottledEvent:
        m.status.transient = fmt.Sprintf("throttled, retrying in %s", ev.RetryAfter)
    }
    return m
}
```

After handling, the subscription must re-arm: each `Update` cycle that consumed a sync event returns a new `subscribeSync` cmd to wait for the next.

## 10. The `Init` sequence

```go
func (m Model) Init() tea.Cmd {
    return tea.Batch(
        m.tryAutoSignin(),       // attempts auth.Token with 1s timeout
        subscribeSync(m.sync),   // start listening for sync events
        m.folders.Init(m.store), // load folders from cache
    )
}
```

`tryAutoSignin` either resolves to a `signedInMsg` (success) or `signinNeededMsg` (timeout/failure), causing the appropriate mode transition.

## 11. Theme and styling

`internal/ui/theme.go` defines lipgloss styles:

```go
type Theme struct {
    Background      lipgloss.Style
    Foreground      lipgloss.Style
    SelectedRow     lipgloss.Style
    Unread          lipgloss.Style
    Muted           lipgloss.Style
    Border          lipgloss.Style
    StatusBar       lipgloss.Style
    Error           lipgloss.Style
    Success         lipgloss.Style
    PaneTitle       lipgloss.Style
    Modal           lipgloss.Style
}

var DarkTheme = Theme{ /* ... */ }
var LightTheme = Theme{ /* ... */ }
```

Theme picked from `[ui].theme` config. Default: `dark`. Auto-detection from terminal can come post-v1 (Bubble Tea exposes `lipgloss.HasDarkBackground()`).

## 12. Help screen

`?` opens a help overlay listing keybindings, grouped by category. Implementation: a sub-model `HelpModel` that renders a scrollable list. `Esc` closes.

Help content auto-generated from the keymap descriptions (each `key.Binding` has a `Help()` method).

## 13. Window resizing

The `tea.WindowSizeMsg` triggers `m.relayout()`:

```go
func (m Model) relayout() Model {
    folderW := m.paneWidths.Folders
    listW := m.paneWidths.List
    if folderW + listW > m.width / 2 {
        // Squish proportionally for narrow terminals
        ...
    }
    viewerW := m.width - folderW - listW
    contentH := m.height - 2  // status + command rows

    m.folders.SetSize(folderW, contentH)
    m.list.SetSize(listW, contentH)
    m.viewer.SetSize(viewerW, contentH)
    m.cmd.SetWidth(m.width)
    m.status.SetWidth(m.width)
    return m
}
```

Minimum supported terminal: 80 columns × 24 rows. Below that, render a "terminal too small" message until resized.

## 14. Logging

Use a context-scoped logger attached to `Model.log`. Every Update call gets a request-scoped logger via `m.log.With("event", reflect.TypeOf(msg).String())`.

Never log message bodies. Subject lines logged only at `DEBUG`.

## 15. Test plan

### Unit tests

- Model state transitions for each mode (Normal → Command → Normal, etc.).
- Keymap conflict resolution (R → Refresh in folders pane, R → MarkUnread in list pane).
- Layout calculations for various terminal sizes.

### Integration tests with `teatest`

`github.com/charmbracelet/x/exp/teatest` (Bubble Tea's testing harness) drives the model with scripted inputs and asserts on rendered output:

```go
func TestNavigation(t *testing.T) {
    tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
    tm.Send(tea.KeyMsg{Type: tea.KeyDown})
    tm.Send(tea.KeyMsg{Type: tea.KeyDown})
    tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
    teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
        return bytes.Contains(out, []byte("Subject: Q4 forecast"))
    }, teatest.WithDuration(2*time.Second))
}
```

Test cases:
- Sign-in flow renders code and URL correctly.
- Folder navigation moves selection.
- Message list reflects sync events.
- Confirm modal blocks all other input until resolved.
- Help overlay opens and closes.
- Window resize relays out.

## 16. Definition of done

- [ ] `internal/ui/` package compiles; `inkwell` (no subcommand) launches the TUI.
- [ ] All three panes render with correct layout at 120x40 and 80x24.
- [ ] Sign-in flow works end-to-end (uses real auth module against a real tenant in manual smoke test).
- [ ] Folder navigation, list scrolling, message selection all work.
- [ ] Sync events update the UI without restart.
- [ ] Sync failures surface in the status line; do not crash.
- [ ] `:quit`, `:q`, `Ctrl+C`, `q` all exit cleanly (engine stop, store close, no goroutine leaks).
- [ ] Help screen lists all bindings.
- [ ] `teatest` test suite passes.

## 17. Configuration

This spec owns the `[ui]` section and the `[bindings]` section (mechanics; specific binding defaults are added by specs 07 and 10 as they introduce new actions). Full reference in `CONFIG.md`.

`[ui]` keys:

| Key | Default | Used in §  |
| --- | --- | --- |
| `ui.theme` | `"dark"` | §11 |
| `ui.folder_pane_width` | `25` | §13 (relayout) |
| `ui.list_pane_width` | `40` | §13 (relayout) |
| `ui.relative_dates_within` | `"168h"` (7d) | §6.2 (list rendering) |
| `ui.unread_indicator` | `"●"` | §6.2 |
| `ui.flag_indicator` | `"⚑"` | §6.2 |
| `ui.attachment_indicator` | `"📎"` | §6.2 |
| `ui.transient_status_ttl` | `"5s"` | §6.5 |
| `ui.confirm_destructive_default` | `"no"` | §8 |
| `ui.min_terminal_cols` | `80` | §13 |
| `ui.min_terminal_rows` | `24` | §13 |

`[bindings]` keys: see CONFIG.md `[bindings]` section. This spec ships the bindings shown in §5; specs 07 and 10 add more.

**Hard-coded:**
- The mode-dispatch structure in §4. Modes are a finite set defined in code.
- The pane structure (folders / list / viewer / status / command). Adding a pane is a code change, not config.
- The Bubble Tea Elm-architecture pattern.

**Binding overrides** are validated against the canonical key set: an unknown binding name in `[bindings]` is a startup error. This prevents typos from silently disabling functionality.

## 18. Out of scope for this spec

- Message body rendering with HTML conversion (spec 05).
- Triage actions (spec 07).
- Search and filter UX (specs 06, 10).
- Saved searches in sidebar (spec 11).
- Calendar pane (spec 12).
- Out-of-office settings UI (spec 13).
