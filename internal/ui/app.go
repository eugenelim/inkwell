package ui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eugenelim/inkwell/internal/render"
	"github.com/eugenelim/inkwell/internal/store"
	isync "github.com/eugenelim/inkwell/internal/sync"
)

// Authenticator is the auth surface the UI consumes. Defined here so
// the UI does not import internal/auth's full surface (CLAUDE.md §2).
type Authenticator interface {
	Account() (upn, tenantID string, signedIn bool)
}

// Engine is the sync engine surface the UI consumes.
type Engine interface {
	Start(ctx context.Context) error
	SetActive(active bool)
	SyncAll(ctx context.Context) error
	Notifications() <-chan isync.Event
}

// TriageExecutor is the action surface the UI consumes for single-
// message triage operations. Defined here at the consumer site so the
// UI does not import internal/action's full surface (CLAUDE.md §2).
type TriageExecutor interface {
	MarkRead(ctx context.Context, accountID int64, messageID string) error
	MarkUnread(ctx context.Context, accountID int64, messageID string) error
	ToggleFlag(ctx context.Context, accountID int64, messageID string, currentlyFlagged bool) error
	SoftDelete(ctx context.Context, accountID int64, messageID string) error
	Archive(ctx context.Context, accountID int64, messageID string) error
}

// Deps wires the UI to its lower-layer collaborators.
type Deps struct {
	Auth     Authenticator
	Store    store.Store
	Engine   Engine
	Renderer render.Renderer
	Logger   *slog.Logger
	Account  *store.Account
	// Triage executes single-message triage actions (mark read, flag,
	// archive, etc.). Optional — when nil, the corresponding key
	// bindings are no-ops.
	Triage TriageExecutor
	// ThemeName is the [ui] theme key from config. Empty falls back to
	// "default". Unknown values fall back with a logged warning.
	ThemeName string
}

// bodyAsyncFetcher narrows render.Renderer to its fetch entry point.
// Defined at the consumer site so we can use a *renderer's
// FetchBodyAsync without exposing it on the public Renderer interface.
type bodyAsyncFetcher interface {
	FetchBodyAsync(ctx context.Context, m *store.Message, opts render.BodyOpts) (render.BodyView, error)
}

// PaneWidths is the configured layout (spec 04 §2).
type PaneWidths struct {
	Folders int
	List    int
}

// DefaultPaneWidths is the spec default. Real widths are recomputed in
// relayout from the terminal size — these are seed values used until
// the first WindowSizeMsg lands.
func DefaultPaneWidths() PaneWidths { return PaneWidths{Folders: 22, List: 56} }

// Model is the root Bubble Tea model. Sub-models are value types
// (CLAUDE.md §4); the entire tree round-trips through Update.
type Model struct {
	deps Deps

	width      int
	height     int
	paneWidths PaneWidths

	folders FoldersModel
	list    ListModel
	viewer  ViewerModel
	cmd     CommandModel
	status  StatusModel
	signin  SignInModel
	confirm ConfirmModel

	focused Pane
	mode    Mode

	keymap KeyMap
	theme  Theme

	// transient state shown by the status bar
	throttledFor   time.Duration
	lastSyncAt     time.Time
	lastError      error
	engineActivity string // "syncing folders…" / "syncing…" / ""
}

// New constructs the root model. After construction, callers run
// `tea.NewProgram(model).Run()`.
func New(deps Deps) Model {
	if deps.Logger == nil {
		// Required for redaction discipline; fail loudly rather than
		// silently using slog.Default.
		panic("ui.New: Logger is required (CLAUDE.md §7)")
	}
	upn := ""
	tenant := ""
	if deps.Account != nil {
		upn, tenant = deps.Account.UPN, deps.Account.TenantID
	}
	theme := DefaultTheme()
	if deps.ThemeName != "" {
		t, ok := ThemeByName(deps.ThemeName)
		if !ok {
			deps.Logger.Warn("ui: unknown theme; falling back to default", "name", deps.ThemeName)
		}
		theme = t
	}
	return Model{
		deps:       deps,
		paneWidths: DefaultPaneWidths(),
		focused:    ListPane,
		mode:       NormalMode,
		keymap:     DefaultKeyMap(),
		theme:      theme,
		folders:    NewFolders(),
		list:       NewList(),
		viewer:     NewViewer(),
		cmd:        NewCommand(),
		status:     NewStatus(upn, tenant),
		signin:     NewSignIn(),
		confirm:    NewConfirm(),
	}
}

// Init kicks off folder loading and sync-event consumption.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadFoldersCmd(),
		m.consumeSyncEventsCmd(),
	)
}

// Update implements the Bubble Tea contract. The function is
// mode-dispatched (spec 04 §4): SignIn / Confirm / Command / Search /
// Normal.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.relayout()
		return m, nil

	case tea.QuitMsg:
		return m, tea.Quit

	case SyncEventMsg:
		var follow tea.Cmd
		m, follow = m.handleSyncEvent(msg.Event)
		return m, tea.Batch(follow, m.consumeSyncEventsCmd())

	case authRequiredMsg:
		m.mode = SignInMode
		m.signin = NewSignIn()
		return m, nil

	case FoldersLoadedMsg:
		m.folders.SetFolders(msg.Folders)
		// Default to Inbox when no folder is selected. Three-step
		// fallback: wellKnownName=inbox → display_name=Inbox (case-
		// insensitive, in case the tenant doesn't return well-known
		// names) → first folder.
		if m.list.FolderID == "" && len(msg.Folders) > 0 {
			pick := msg.Folders[0]
			for _, f := range msg.Folders {
				if f.WellKnownName == "inbox" {
					pick = f
					break
				}
			}
			if pick.WellKnownName != "inbox" {
				for _, f := range msg.Folders {
					if strings.EqualFold(f.DisplayName, "Inbox") {
						pick = f
						break
					}
				}
			}
			m.list.FolderID = pick.ID
			m.folders.SelectByID(pick.ID)
			return m, m.loadMessagesCmd(pick.ID)
		}
		return m, nil

	case MessagesLoadedMsg:
		if msg.FolderID == m.list.FolderID {
			m.list.SetMessages(msg.Messages)
		}
		return m, nil

	case BodyRenderedMsg:
		if m.viewer.CurrentMessageID() == msg.MessageID {
			m.viewer.SetBody(msg.Text, msg.State)
		}
		return m, nil

	case ErrorMsg:
		m.lastError = msg.Err
		return m, nil

	case ConfirmResultMsg:
		// ConfirmMode owns dispatch; return to Normal and let Update
		// hand the result to the registered callback in a future spec.
		m.mode = NormalMode
		return m, nil

	case triageDoneMsg:
		if msg.err != nil {
			m.lastError = fmt.Errorf("%s: %w", msg.name, msg.err)
			return m, nil
		}
		m.lastError = nil
		// Reload the message list so the optimistic mutation (or its
		// rollback) is reflected in the current pane.
		if msg.folderID != "" && msg.folderID == m.list.FolderID {
			return m, m.loadMessagesCmd(msg.folderID)
		}
		return m, nil
	}

	switch m.mode {
	case SignInMode:
		return m.updateSignIn(msg)
	case ConfirmMode:
		return m.updateConfirm(msg)
	case CommandMode:
		return m.updateCommand(msg)
	case SearchMode:
		return m.updateSearch(msg)
	default:
		return m.updateNormal(msg)
	}
}

func (m Model) updateNormal(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(keyMsg, m.keymap.Quit):
		return m, tea.Quit
	case key.Matches(keyMsg, m.keymap.Refresh):
		go func() { _ = m.deps.Engine.SyncAll(context.Background()) }()
		return m, nil
	case key.Matches(keyMsg, m.keymap.Cmd):
		m.mode = CommandMode
		m.cmd.Activate()
		return m, nil
	case key.Matches(keyMsg, m.keymap.Search):
		m.mode = SearchMode
		return m, nil
	case key.Matches(keyMsg, m.keymap.FocusFolders):
		m.focused = FoldersPane
		return m, nil
	case key.Matches(keyMsg, m.keymap.FocusList):
		m.focused = ListPane
		return m, nil
	case key.Matches(keyMsg, m.keymap.FocusViewer):
		m.focused = ViewerPane
		return m, nil
	case key.Matches(keyMsg, m.keymap.NextPane):
		m.focused = nextPane(m.focused)
		return m, nil
	case key.Matches(keyMsg, m.keymap.PrevPane):
		m.focused = prevPane(m.focused)
		return m, nil
	}
	// Pane-scoped dispatch (spec 04 §5). The list pane handles list
	// movement, the folders pane handles tree movement, etc.
	switch m.focused {
	case FoldersPane:
		return m.dispatchFolders(keyMsg)
	case ListPane:
		return m.dispatchList(keyMsg)
	case ViewerPane:
		return m.dispatchViewer(keyMsg)
	}
	return m, nil
}

func (m Model) updateCommand(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if keyMsg.Type == tea.KeyEsc {
		m.mode = NormalMode
		m.cmd.Reset()
		return m, nil
	}
	if keyMsg.Type == tea.KeyEnter {
		entered := strings.TrimSpace(m.cmd.Buffer())
		m.cmd.Reset()
		m.mode = NormalMode
		return m.dispatchCommand(entered)
	}
	m.cmd.HandleKey(keyMsg)
	return m, nil
}

func (m Model) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if keyMsg.Type == tea.KeyEsc || keyMsg.Type == tea.KeyEnter {
		m.mode = NormalMode
		return m, nil
	}
	return m, nil
}

func (m Model) updateSignIn(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if keyMsg.Type == tea.KeyEsc {
		// Cancel sign-in; return to normal even though we're not auth'd.
		m.mode = NormalMode
		return m, nil
	}
	return m, nil
}

func (m Model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch strings.ToLower(keyMsg.String()) {
	case "y":
		m.mode = NormalMode
		return m, func() tea.Msg { return ConfirmResultMsg{Topic: m.confirm.Topic, Confirm: true} }
	case "n", "esc":
		m.mode = NormalMode
		return m, func() tea.Msg { return ConfirmResultMsg{Topic: m.confirm.Topic, Confirm: false} }
	}
	return m, nil
}

// dispatchCommand handles `:command` invocations from the command bar.
// Spec 04 ships :sync, :signin, :signout, :quit; the rest land in
// later specs.
func (m Model) dispatchCommand(line string) (tea.Model, tea.Cmd) {
	if line == "" {
		return m, nil
	}
	args := strings.Fields(line)
	switch args[0] {
	case "quit", "q":
		return m, tea.Quit
	case "sync":
		go func() { _ = m.deps.Engine.SyncAll(context.Background()) }()
		return m, nil
	case "signin":
		m.mode = SignInMode
		m.signin = NewSignIn()
		return m, nil
	case "signout":
		// Spec 01 implements the underlying flow; UI just transitions
		// to confirm-then-action. Wire-through deferred to spec 13.
		m.confirm = m.confirm.Ask("Sign out and clear cached credentials?", "signout")
		m.mode = ConfirmMode
		return m, nil
	}
	m.lastError = fmt.Errorf("unknown command: %s", line)
	return m, nil
}

func (m Model) dispatchFolders(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keymap.Up):
		m.folders.Up()
	case key.Matches(msg, m.keymap.Down):
		m.folders.Down()
	case key.Matches(msg, m.keymap.Expand):
		m.folders.ToggleExpand()
	case key.Matches(msg, m.keymap.Open), key.Matches(msg, m.keymap.Right):
		f, ok := m.folders.Selected()
		if ok {
			m.list.FolderID = f.ID
			m.focused = ListPane
			return m, m.loadMessagesCmd(f.ID)
		}
	}
	return m, nil
}

func (m Model) dispatchList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keymap.Up):
		m.list.Up()
	case key.Matches(msg, m.keymap.Down):
		m.list.Down()
	case key.Matches(msg, m.keymap.Open):
		sel, ok := m.list.Selected()
		if ok {
			m.viewer.SetMessage(sel)
			m.focused = ViewerPane
			return m, m.openMessageCmd(sel)
		}
	case key.Matches(msg, m.keymap.MarkRead):
		return m.runTriage("mark_read", func(ctx context.Context, accID int64, sel store.Message) error {
			return m.deps.Triage.MarkRead(ctx, accID, sel.ID)
		})
	case key.Matches(msg, m.keymap.MarkUnread):
		return m.runTriage("mark_unread", func(ctx context.Context, accID int64, sel store.Message) error {
			return m.deps.Triage.MarkUnread(ctx, accID, sel.ID)
		})
	case key.Matches(msg, m.keymap.ToggleFlag):
		return m.runTriage("toggle_flag", func(ctx context.Context, accID int64, sel store.Message) error {
			return m.deps.Triage.ToggleFlag(ctx, accID, sel.ID, sel.FlagStatus == "flagged")
		})
	case key.Matches(msg, m.keymap.Delete):
		return m.runTriage("soft_delete", func(ctx context.Context, accID int64, sel store.Message) error {
			return m.deps.Triage.SoftDelete(ctx, accID, sel.ID)
		})
	case key.Matches(msg, m.keymap.Archive):
		return m.runTriage("archive", func(ctx context.Context, accID int64, sel store.Message) error {
			return m.deps.Triage.Archive(ctx, accID, sel.ID)
		})
	}
	return m, nil
}

// runTriage is the shared dispatch boilerplate for r/R/f/d/a. Captures
// the focused message, fires the triage callback in a goroutine (so
// Update returns promptly), and reloads the list when done. Errors
// land in m.lastError via the post-completion message.
func (m Model) runTriage(name string, fn func(context.Context, int64, store.Message) error) (tea.Model, tea.Cmd) {
	if m.deps.Triage == nil {
		m.lastError = fmt.Errorf("triage: not wired (run from cmd_run.go path)")
		return m, nil
	}
	sel, ok := m.list.Selected()
	if !ok {
		return m, nil
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	folderID := m.list.FolderID
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := fn(ctx, accountID, sel)
		return triageDoneMsg{name: name, folderID: folderID, err: err}
	}
	return m, cmd
}

type triageDoneMsg struct {
	name     string
	folderID string
	err      error
}

// openMessageCmd renders headers immediately, then either reads the
// cached body or kicks off an async fetch. The result lands as a
// BodyRenderedMsg so the viewer pane can refresh.
func (m Model) openMessageCmd(msg store.Message) tea.Cmd {
	if m.deps.Renderer == nil {
		return nil
	}
	r := m.deps.Renderer
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		opts := render.BodyOpts{Width: 80, Theme: render.DefaultTheme()}
		view, err := r.Body(ctx, &msg, opts)
		if err != nil {
			return BodyRenderedMsg{MessageID: msg.ID, Text: "render error: " + err.Error(), State: int(render.BodyError)}
		}
		if view.State == render.BodyReady {
			return BodyRenderedMsg{MessageID: msg.ID, Text: view.Text, State: int(view.State)}
		}
		// BodyFetching: dispatch the fetch synchronously inside this
		// goroutine and return the final rendered view.
		if f, ok := r.(bodyAsyncFetcher); ok {
			final, err := f.FetchBodyAsync(ctx, &msg, opts)
			if err != nil {
				return BodyRenderedMsg{MessageID: msg.ID, Text: "fetch error: " + err.Error(), State: int(render.BodyError)}
			}
			return BodyRenderedMsg{MessageID: msg.ID, Text: final.Text, State: int(final.State)}
		}
		return BodyRenderedMsg{MessageID: msg.ID, Text: view.Text, State: int(view.State)}
	}
}

func (m Model) dispatchViewer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keymap.Left):
		m.focused = ListPane
	case key.Matches(msg, m.keymap.Down):
		m.viewer.ScrollDown()
	case key.Matches(msg, m.keymap.Up):
		m.viewer.ScrollUp()
	}
	return m, nil
}

// Commands

func (m Model) loadFoldersCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		fs, err := m.deps.Store.ListFolders(ctx, accountID)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return FoldersLoadedMsg{Folders: fs, At: time.Now()}
	}
}

func (m Model) loadMessagesCmd(folderID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		msgs, err := m.deps.Store.ListMessages(ctx, store.MessageQuery{
			AccountID: accountID,
			FolderID:  folderID,
			Limit:     200,
		})
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MessagesLoadedMsg{FolderID: folderID, Messages: msgs}
	}
}

// consumeSyncEventsCmd reads a single event from the engine's
// notification channel and re-arms itself in Update. This pattern keeps
// the channel-read off the Update goroutine (Bubble Tea contract:
// Update never blocks on I/O).
func (m Model) consumeSyncEventsCmd() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.deps.Engine.Notifications()
		if !ok {
			return nil
		}
		return SyncEventMsg{Event: ev}
	}
}

func (m Model) handleSyncEvent(ev isync.Event) (Model, tea.Cmd) {
	switch e := ev.(type) {
	case isync.SyncStartedEvent:
		m.engineActivity = "syncing…"
		m.lastError = nil
	case isync.SyncCompletedEvent:
		m.engineActivity = ""
		m.lastSyncAt = e.At
	case isync.SyncFailedEvent:
		m.engineActivity = ""
		m.lastError = e.Err
	case isync.ThrottledEvent:
		m.throttledFor = e.RetryAfter
	case isync.AuthRequiredEvent:
		m.mode = SignInMode
		m.signin = NewSignIn()
	case isync.FoldersEnumeratedEvent:
		// Folder list just landed in the store. Reload the sidebar
		// IMMEDIATELY so the user sees folders even before per-folder
		// sync completes (or even if it later errors out).
		m.engineActivity = "syncing folders…"
		return m, m.loadFoldersCmd()
	case isync.FolderSyncedEvent:
		m.lastSyncAt = e.At
		m.engineActivity = "syncing…"
		// Refresh the folder list (counts may have changed) and, if
		// the user is on the synced folder, refresh the message list
		// too. Spec 04 §10: the UI never blocks; both reloads are Cmds.
		cmds := []tea.Cmd{m.loadFoldersCmd()}
		if e.FolderID == m.list.FolderID {
			cmds = append(cmds, m.loadMessagesCmd(e.FolderID))
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// relayout recomputes pane widths from terminal size. The list pane
// gets the dominant share of the remaining cols (after the fixed
// folders sidebar) so subject lines stay readable. Viewer takes the
// rest. At <90 cols everything compresses proportionally.
func (m Model) relayout() Model {
	if m.width < 1 {
		return m
	}
	folders := 22
	if m.width < 90 {
		folders = m.width / 4
		if folders < 14 {
			folders = 14
		}
	}
	remaining := m.width - folders
	if remaining < 30 {
		remaining = 30
	}
	// 60% of the remaining width to list, 40% to viewer. At 120 cols
	// that's folders=22, list=58, viewer=40 — subjects get ~28 chars
	// after the date/sender prefix, viewer keeps a usable reading column.
	list := remaining * 6 / 10
	if list < 40 {
		list = 40
	}
	if list > remaining-25 {
		list = remaining - 25
	}
	m.paneWidths.Folders = folders
	m.paneWidths.List = list
	return m
}

// View renders the full screen.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}
	if m.mode == SignInMode {
		return m.signin.View(m.theme, m.width, m.height)
	}
	if m.mode == ConfirmMode {
		return m.confirm.View(m.theme, m.width, m.height)
	}

	statusBar := m.status.View(m.theme, m.width, StatusInputs{
		LastSync:  m.lastSyncAt,
		Throttled: m.throttledFor,
		Activity:  m.engineActivity,
		LastErr:   m.lastError,
	})
	cmdBar := m.cmd.View(m.theme, m.width, m.mode == CommandMode)
	helpBar := renderHelpBar(m.theme, m.width, m.focused)

	// 3 chrome lines: status (top) + command (just above help) + help (bottom).
	bodyHeight := m.height - 3
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	viewerWidth := m.width - m.paneWidths.Folders - m.paneWidths.List
	if viewerWidth < 10 {
		viewerWidth = 10
	}

	folders := m.folders.View(m.theme, m.paneWidths.Folders, bodyHeight, m.focused == FoldersPane)
	list := m.list.View(m.theme, m.paneWidths.List, bodyHeight, m.focused == ListPane)
	viewer := m.viewer.View(m.theme, viewerWidth, bodyHeight, m.focused == ViewerPane)

	// Clip the body region to EXACTLY bodyHeight rows. Each pane's
	// lipgloss.Height(bodyHeight) pads with a trailing newline; left
	// alone, JoinVertical inflates the frame past m.height and the
	// help bar slides off the bottom (regression in v0.2.8). Trimming
	// here guarantees: 1 + bodyHeight + 1 + 1 == m.height.
	body := lipgloss.JoinHorizontal(lipgloss.Top, folders, list, viewer)
	body = strings.TrimRight(body, "\n")
	bodyLines := strings.Split(body, "\n")
	if len(bodyLines) > bodyHeight {
		bodyLines = bodyLines[:bodyHeight]
	}
	body = strings.Join(bodyLines, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, statusBar, body, cmdBar, helpBar)
}

// renderHelpBar emits a one-line key-binding hint at the bottom of the
// TUI. Hints are pane-specific so the user always sees the most
// relevant keys for what's focused. Each hint is "key description";
// the key glyph is bold-coloured (HelpKey), the description is
// regular (Help), separated by a dim middot (HelpSep).
func renderHelpBar(t Theme, width int, focused Pane) string {
	var hints [][2]string
	switch focused {
	case FoldersPane:
		hints = [][2]string{{"j/k", "nav"}, {"o", "expand"}, {"⏎", "open"}, {"2", "list"}, {"q", "quit"}}
	case ListPane:
		hints = [][2]string{{"j/k", "nav"}, {"⏎", "open"}, {"r/R", "read"}, {"f", "flag"}, {"a", "archive"}, {"d", "delete"}, {"q", "quit"}}
	case ViewerPane:
		hints = [][2]string{{"h", "back"}, {"j/k", "scroll"}, {"2", "list"}, {"q", "quit"}}
	default:
		hints = [][2]string{{"1/2/3", "panes"}, {":", "command"}, {"q", "quit"}}
	}
	sep := t.HelpSep.Render(" · ")
	var b strings.Builder
	for i, h := range hints {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(t.HelpKey.Render(h[0]))
		b.WriteByte(' ')
		b.WriteString(t.Help.Render(h[1]))
	}
	rendered := b.String()
	pad := width - lipgloss.Width(rendered)
	if pad < 0 {
		pad = 0
	}
	return rendered + strings.Repeat(" ", pad)
}

func nextPane(p Pane) Pane {
	switch p {
	case FoldersPane:
		return ListPane
	case ListPane:
		return ViewerPane
	default:
		return FoldersPane
	}
}

func prevPane(p Pane) Pane {
	switch p {
	case FoldersPane:
		return ViewerPane
	case ListPane:
		return FoldersPane
	default:
		return ListPane
	}
}
