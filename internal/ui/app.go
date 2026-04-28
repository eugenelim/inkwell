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

// Deps wires the UI to its lower-layer collaborators.
type Deps struct {
	Auth     Authenticator
	Store    store.Store
	Engine   Engine
	Renderer render.Renderer
	Logger   *slog.Logger
	Account  *store.Account
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

// DefaultPaneWidths is the spec default (folders 25, list 40, viewer auto).
func DefaultPaneWidths() PaneWidths { return PaneWidths{Folders: 25, List: 40} }

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
	return Model{
		deps:       deps,
		paneWidths: DefaultPaneWidths(),
		focused:    ListPane,
		mode:       NormalMode,
		keymap:     DefaultKeyMap(),
		theme:      DefaultTheme(),
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
		// Default to Inbox when no folder is selected. This matches
		// user expectation: alphabetical order would land on Archive.
		if m.list.FolderID == "" && len(msg.Folders) > 0 {
			pick := msg.Folders[0]
			for _, f := range msg.Folders {
				if f.WellKnownName == "inbox" {
					pick = f
					break
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
	case key.Matches(msg, m.keymap.Open), key.Matches(msg, m.keymap.Right):
		f, ok := m.folders.Selected()
		if ok {
			m.list.FolderID = f.ID
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
	}
	return m, nil
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

// relayout recomputes pane widths after a WindowSizeMsg. Defaults
// preserve config-supplied folder/list widths; viewer takes the rest.
func (m Model) relayout() Model {
	if m.width < m.paneWidths.Folders+m.paneWidths.List+10 {
		// Cramped: shrink folders to half and list to 40% of remaining.
		m.paneWidths.Folders = m.width / 4
		m.paneWidths.List = (m.width - m.paneWidths.Folders) / 2
	}
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

	bodyHeight := m.height - 2 // status + command lines
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

	body := lipgloss.JoinHorizontal(lipgloss.Top, folders, list, viewer)
	return lipgloss.JoinVertical(lipgloss.Left, statusBar, body, cmdBar)
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
