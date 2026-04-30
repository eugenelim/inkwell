package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eugenelim/inkwell/internal/compose"
	"github.com/eugenelim/inkwell/internal/pattern"
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
	// Wake kicks the engine's loop to run a cycle on the next select
	// iteration. Single-shot, debounced via the engine's buffer-1
	// wakeup channel. Use this for UI-driven "sync now" signals
	// instead of SyncAll — Wake doesn't overlap with the loop's
	// timer-driven cycle.
	Wake()
	// Backfill pulls older messages from Graph for the supplied
	// folder. The engine returns when the backfill has caught up
	// (or after a sensible cap). Used by the cache-wall flow when
	// the user has scrolled past everything cached locally.
	Backfill(ctx context.Context, folderID string, until time.Time) error
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
	// PermanentDelete removes the message from the tenant entirely
	// (spec 07 §6.7). Irreversible — caller MUST guard with the
	// confirm modal; pressing `D` without confirmation is the
	// primary footgun this method exists to handle.
	PermanentDelete(ctx context.Context, accountID int64, messageID string) error
	// AddCategory / RemoveCategory tag and untag a message with a
	// category name (spec 07 §6.9 / §6.10). Case-insensitive dedup
	// per Outlook semantics.
	AddCategory(ctx context.Context, accountID int64, messageID, category string) error
	RemoveCategory(ctx context.Context, accountID int64, messageID, category string) error

	// CreateFolder / RenameFolder / DeleteFolder are spec 18 folder
	// management. CreateFolder accepts an empty parentID for top-
	// level folders; returns the canonical Graph-assigned id +
	// displayName so the UI can refocus on the new row.
	CreateFolder(ctx context.Context, accountID int64, parentID, displayName string) (CreatedFolder, error)
	RenameFolder(ctx context.Context, folderID, displayName string) error
	DeleteFolder(ctx context.Context, folderID string) error
	// Undo pops the most recent UndoEntry and applies the inverse
	// action. Returns a UndoneAction describing what just got rolled
	// back so the UI can paint a status message ("undid: deleted").
	// Returns the typed UndoEmpty when the stack is empty.
	Undo(ctx context.Context, accountID int64) (UndoneAction, error)
}

// CreatedFolder is the spec 18 result of a successful create.
type CreatedFolder struct {
	ID             string
	DisplayName    string
	ParentFolderID string
}

// UndoneAction is the UI-facing summary of a successful Undo. Mirrors
// store.UndoEntry without leaking the type into the UI consumer site.
type UndoneAction struct {
	Label      string // human-readable description ("deleted", "marked read")
	MessageIDs []string
}

// UndoEmpty is the sentinel error returned when the undo stack is
// empty.
var UndoEmpty = errors.New("undo: stack empty")

// BulkResult mirrors action.BatchResult — defined here so the UI
// doesn't import internal/action's full type surface.
type BulkResult struct {
	MessageID string
	Err       error
}

// BulkExecutor handles "apply this action to N messages" operations
// (spec 09 / 10). Implementations route through Graph $batch.
type BulkExecutor interface {
	BulkSoftDelete(ctx context.Context, accountID int64, messageIDs []string) ([]BulkResult, error)
	BulkArchive(ctx context.Context, accountID int64, messageIDs []string) ([]BulkResult, error)
	BulkMarkRead(ctx context.Context, accountID int64, messageIDs []string) ([]BulkResult, error)
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
	// Bulk executes batch triage (spec 09/10). Optional like Triage.
	Bulk BulkExecutor
	// ThemeName is the [ui] theme key from config. Empty falls back to
	// "default". Unknown values fall back with a logged warning.
	ThemeName string
	// SavedSearches is the list of [[saved_searches]] entries from
	// config. They render in the folders pane as virtual folders;
	// selecting one runs its pattern via the same machinery as
	// `:filter` (spec 10).
	SavedSearches []SavedSearch
	// Bindings carries the user's [bindings] overrides decoded from
	// config. Empty fields leave the default in place. Spec 04 §17:
	// unknown keys cause a startup error in config.Load (the TOML
	// undecoded-keys gate); duplicate bindings cause a startup
	// error from ui.New (caller should fail-fast).
	Bindings BindingOverrides
	// Calendar fetches today's events for the `:cal` modal (spec 12).
	// Optional — when nil, `:cal` shows a friendly "calendar not wired"
	// message instead of crashing.
	Calendar CalendarFetcher
	// Mailbox is the surface for the :ooo flow (spec 13). Optional.
	Mailbox MailboxClient
	// Drafts handles compose / reply (spec 15). Optional — when nil,
	// the `r` keybinding in the viewer pane shows a friendly error.
	Drafts DraftCreator
	// Unsubscribe wires the U key (spec 16). Optional — when nil, U
	// shows a friendly "unsubscribe not wired" message.
	Unsubscribe UnsubscribeService
}

// SavedSearch is a named pattern that surfaces in the sidebar. Defined
// at the consumer site so the UI doesn't import internal/config.
type SavedSearch struct {
	Name    string
	Pattern string
}

// CalendarFetcher is the read-only calendar surface the UI consumes
// (spec 12). Defined here so the UI doesn't import internal/graph's
// full surface.
type CalendarFetcher interface {
	ListEventsToday(ctx context.Context) ([]CalendarEvent, error)
}

// MailboxSettings is the subset of mailbox settings the UI renders
// for the :ooo flow (spec 13). Defined at the consumer site so the
// UI doesn't import internal/graph.
type MailboxSettings struct {
	AutoReplyEnabled     bool
	InternalReplyMessage string
	ExternalReplyMessage string
}

// MailboxClient handles GET + PATCH against /me/mailboxSettings for
// the out-of-office flow. v0.9.0 only toggles enable/disable; richer
// editing (custom message, schedule, audience) lands later.
type MailboxClient interface {
	Get(ctx context.Context) (*MailboxSettings, error)
	SetAutoReply(ctx context.Context, enabled bool, internalMsg, externalMsg string) error
}

// DraftCreator is the surface the UI consumes for spec 15
// (compose / reply). Defined here so the UI doesn't import
// internal/action's full type set.
type DraftCreator interface {
	CreateDraftReply(ctx context.Context, sourceMessageID, body string, to, cc, bcc []string, subject string) (*DraftRef, error)
}

// UnsubscribeKind enumerates the action the UI should drive after
// resolving a message's List-Unsubscribe header (spec 16).
type UnsubscribeKind int

const (
	UnsubscribeNone         UnsubscribeKind = iota // no actionable header
	UnsubscribeOneClickPOST                        // RFC 8058 one-click HTTPS POST
	UnsubscribeBrowserGET                          // HTTPS only — open in browser
	UnsubscribeMailto                              // mailto: URI — compose flow (degraded for v1)
)

// UnsubscribeAction is what UnsubscribeService.Resolve returns. The
// UI uses Kind to route the confirm modal + execution; URL/Mailto
// fields populate the modal preview so the user spots a phishing
// attempt before pressing y.
type UnsubscribeAction struct {
	Kind   UnsubscribeKind
	URL    string // populated for OneClickPOST + BrowserGET
	Mailto string // populated for Mailto (the bare address, no scheme)
}

// UnsubscribeService is the surface the UI consumes for spec 16. The
// wiring layer (cmd/inkwell) composes this from the store + graph
// client + unsub.Executor; the UI never imports graph directly
// (CLAUDE.md §2).
type UnsubscribeService interface {
	// Resolve returns the cached or freshly-fetched unsubscribe action
	// for a message. The first call may make a Graph round-trip; the
	// result is persisted on the row so subsequent calls are local.
	Resolve(ctx context.Context, messageID string) (UnsubscribeAction, error)
	// OneClickPOST issues the RFC 8058 POST. Nil on success; typed
	// error otherwise. The UI surfaces the error verbatim so the
	// status code falls back to the browser path.
	OneClickPOST(ctx context.Context, url string) error
}

// DraftRef mirrors action.DraftResult. WebLink is the Outlook URL
// surfaced on the status bar after a save — pressing 's' opens it
// in the browser / Outlook desktop.
type DraftRef struct {
	ID      string
	WebLink string
}

// CalendarEvent mirrors the fields the calendar modal renders. All
// times are UTC.
type CalendarEvent struct {
	Subject          string
	OrganizerName    string
	OrganizerAddress string
	Start            time.Time
	End              time.Time
	IsAllDay         bool
	Location         string
	OnlineMeetingURL string
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

	folders   FoldersModel
	list      ListModel
	viewer    ViewerModel
	cmd       CommandModel
	status    StatusModel
	signin    SignInModel
	confirm   ConfirmModel
	calendar  CalendarModel
	oof       OOFModel
	help      HelpModel
	urlPicker URLPickerModel
	yanker    *yanker

	focused Pane
	mode    Mode

	keymap KeyMap
	theme  Theme

	// transient state shown by the status bar
	throttledFor   time.Duration
	lastSyncAt     time.Time
	lastError      error
	engineActivity string // "syncing folders…" / "syncing…" / ""

	// Search-mode buffer + last-committed query. The list pane renders
	// search results in place of folder messages when searchActive.
	searchBuf     string
	searchActive  bool
	searchQuery   string // committed query (the one that produced m.list contents)
	priorFolderID string // folder to restore when search is cleared

	// Filter mode (spec 10): :filter <pattern> compiles via spec 08
	// and narrows the list pane to matches. ; prefix + a triage key
	// applies that action to all matched messages via BulkExecutor.
	filterActive  bool
	filterPattern string
	filterIDs     []string // matched message IDs (for bulk apply)
	bulkPending   bool     // true after `;` is pressed; next d/a fires bulk
	pendingBulk   string   // "soft_delete" / "archive" while in ConfirmMode

	// Compose / reply (spec 15). Tracks the most-recently-saved draft
	// so the viewer-pane `s` shortcut can open it in Outlook. Cleared
	// when the user starts another compose flow or moves on.
	lastDraftWebLink string
	composeTempfile  string // path of the in-flight tempfile, if any
	composeSourceID  string // source message id for the in-flight reply

	// Unsubscribe (spec 16). pendingUnsub holds the resolved action
	// while the confirm modal is open; the y/n result fires the
	// matching execution branch. Cleared after the action completes
	// or the user cancels.
	pendingUnsub          *UnsubscribeAction
	pendingUnsubMessageID string

	// Permanent delete (spec 07 §6.7). pendingPermanentDelete holds
	// the focused message while the confirm modal is open; y fires
	// PermanentDelete (irreversible — Inverse returns ok=false so
	// the action doesn't push to the undo stack).
	pendingPermanentDelete *store.Message

	// Category input (spec 07 §6.9 / §6.10). When CategoryInputMode
	// is active, pendingCategoryAction holds "add" or "remove" and
	// pendingCategoryMsg holds the focused message; the user types
	// the category name and Enter dispatches the action.
	pendingCategoryAction string // "add" | "remove"
	pendingCategoryMsg    *store.Message
	categoryBuf           string

	// Folder name input (spec 18). Reused for both `N` (create) and
	// `R` (rename). pendingFolderAction is "new" | "rename"; for
	// rename, pendingFolderID identifies the target. The buffer is
	// pre-seeded with the current name on rename.
	pendingFolderAction string // "new" | "rename"
	pendingFolderID     string
	pendingFolderParent string // parent folder ID for `new` (empty = top-level)
	folderNameBuf       string

	// Folder delete (spec 18). pendingFolderDelete holds the target
	// while the confirm modal is open. Set to nil after y/n.
	pendingFolderDelete *store.Folder
}

// New constructs the root model. Returns a typed error if the
// supplied [bindings] overrides produce a duplicate binding (spec
// 04 §17 invariant); the caller fails-fast at startup so the user
// gets a clear "your config is wrong" message rather than a
// silently broken keymap.
//
// After successful construction, callers run
// `tea.NewProgram(model).Run()`.
func New(deps Deps) (Model, error) {
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
	folders := NewFolders()
	if len(deps.SavedSearches) > 0 {
		folders.SetSavedSearches(deps.SavedSearches)
	}
	keymap, err := ApplyBindingOverrides(DefaultKeyMap(), deps.Bindings)
	if err != nil {
		return Model{}, fmt.Errorf("ui: %w", err)
	}
	return Model{
		deps:       deps,
		paneWidths: DefaultPaneWidths(),
		focused:    ListPane,
		mode:       NormalMode,
		keymap:     keymap,
		theme:      theme,
		folders:    folders,
		list:       NewList(),
		viewer:     NewViewer(),
		cmd:        NewCommand(),
		status:     NewStatus(upn, tenant),
		signin:     NewSignIn(),
		confirm:    NewConfirm(),
		calendar:   NewCalendar(),
		oof:        NewOOF(),
		help:       NewHelp(),
		urlPicker:  NewURLPicker(),
		yanker:     newYanker(stdoutOSC52Writer),
	}, nil
}

// stdoutOSC52Writer writes the supplied OSC 52 escape sequence to
// stdout — the same FD Bubble Tea uses for rendering, so the
// terminal sees it inline with the next render frame. Tests
// substitute a buffer-backed writer.
var stdoutOSC52Writer = func(seq string) error {
	_, err := os.Stdout.WriteString(seq)
	return err
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
			m.viewer.SetLinks(msg.Links)
		}
		// Stale local id: Graph reassigns message IDs on Move
		// (soft-delete, archive, user-folder move). The local row
		// keeps the old ID until the next sync re-discovers the
		// message in its new folder. Opening it before then 404s
		// with "ErrorItemNotFound" / "Object not found". Show a
		// friendlier hint AND drop the stale local row + refresh
		// so the user is no longer staring at a row that 404s on
		// every Enter. Real-tenant regression v0.15.x — to be
		// fixed properly via Graph immutable IDs in v0.16.
		if msg.State == int(render.BodyError) && isStaleIDError(msg.Text) {
			m.lastError = fmt.Errorf("message has been moved on the server (local cache stale); refreshing")
			m.viewer.SetMessage(store.Message{})
			m.viewer.current = nil
			folderID := m.list.FolderID
			cmds := []tea.Cmd{}
			if !strings.HasPrefix(folderID, "filter:") && folderID != "" {
				cmds = append(cmds, m.deleteStaleLocalRow(msg.MessageID), m.loadMessagesCmd(folderID))
			} else if m.filterActive {
				cmds = append(cmds, m.deleteStaleLocalRow(msg.MessageID), m.runFilterCmd(m.filterPattern))
			}
			if len(cmds) > 0 {
				return m, tea.Batch(cmds...)
			}
		}
		return m, nil

	case ErrorMsg:
		m.lastError = msg.Err
		return m, nil

	case backfillDoneMsg:
		// Success: the engine emits FolderSyncedEvent; that handler
		// refreshes the list. No work here. Failure: surface the
		// error in the status bar AND clear the activity hint so
		// the user isn't stuck on "loading older messages…". Also
		// clear wallSyncRequested so a subsequent retry can fire.
		if msg.Err != nil {
			m.lastError = fmt.Errorf("backfill: %w", msg.Err)
			m.engineActivity = ""
			if msg.FolderID == m.list.FolderID {
				m.list.ClearWallSyncRequested()
			}
		}
		return m, nil

	case calendarFetchedMsg:
		if msg.Err != nil {
			m.calendar.SetError(msg.Err)
		} else {
			m.calendar.SetEvents(msg.Events)
		}
		return m, nil

	case oofLoadedMsg:
		if msg.Err != nil {
			m.oof.SetError(msg.Err)
		} else {
			m.oof.SetSettings(msg.Settings)
		}
		return m, nil

	case oofToggledMsg:
		if msg.Err != nil {
			m.oof.SetError(msg.Err)
		} else {
			m.oof.SetSettings(msg.Settings)
		}
		return m, nil

	case composeStartedMsg:
		if msg.err != nil {
			m.lastError = fmt.Errorf("compose: %w", msg.err)
			return m, nil
		}
		// Skeleton + tempfile + editor cmd ready — suspend the TUI
		// and run the editor.
		m.composeTempfile = msg.tempfile
		m.composeSourceID = msg.sourceID
		m.engineActivity = "editing draft…"
		return m, runEditorCmd(msg.tempfile, msg.sourceID, msg.editor)

	case composeEditedMsg:
		// Editor exited. Don't save yet — pop a confirm pane so the
		// user can choose Save / Re-edit / Discard. Solves two
		// real-tenant complaints: (a) no visible "Save Draft" hint
		// during the flow; (b) editor's :q!-style exits saved the
		// draft anyway, contrary to user expectation.
		if msg.err != nil {
			// exec error before the editor even started.
			m.lastError = fmt.Errorf("compose: %w", msg.err)
			m.engineActivity = ""
			return m, nil
		}
		m.composeTempfile = msg.tempfile
		m.composeSourceID = msg.sourceID
		m.engineActivity = ""
		m.mode = ComposeConfirmMode
		return m, nil

	case draftSavedMsg:
		m.composeTempfile = ""
		m.composeSourceID = ""
		m.engineActivity = ""
		if msg.err != nil {
			// Pretty-printed errors for the parse-time discard cases.
			// Anything else is a Graph round-trip failure; surface the
			// preserved tempfile path so the user can recover.
			switch {
			case errors.Is(msg.err, compose.ErrEmpty):
				m.lastError = nil
				m.engineActivity = "draft was empty — discarded"
			case errors.Is(msg.err, compose.ErrNoRecipients):
				m.lastError = fmt.Errorf("draft: %w", msg.err)
			default:
				if msg.tempfile != "" {
					m.lastError = fmt.Errorf("draft: %w (preserved at %s)", msg.err, msg.tempfile)
				} else {
					m.lastError = fmt.Errorf("draft: %w", msg.err)
				}
			}
			m.lastDraftWebLink = msg.webLink // may be set on partial-failure (createReply ok, body PATCH failed)
			return m, nil
		}
		m.lastError = nil
		m.lastDraftWebLink = msg.webLink
		m.engineActivity = "✓ draft saved · press s to open in Outlook"
		return m, nil

	case ConfirmResultMsg:
		m.mode = NormalMode
		// Bulk-action confirmation: pendingBulk carries the action key
		// set by confirmBulk(). Only fire on Confirm=true; on No, just
		// drop it.
		if m.pendingBulk != "" {
			action := m.pendingBulk
			m.pendingBulk = ""
			if msg.Confirm {
				return m, m.runBulkCmd(action)
			}
		}
		// Folder delete confirmation (spec 18): pendingFolderDelete
		// carries the target. y fires Triage.DeleteFolder.
		if m.pendingFolderDelete != nil && msg.Topic == "delete_folder" {
			f := *m.pendingFolderDelete
			m.pendingFolderDelete = nil
			if msg.Confirm {
				cmd := func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					defer cancel()
					err := m.deps.Triage.DeleteFolder(ctx, f.ID)
					return folderActionDoneMsg{action: "delete", name: f.DisplayName, folderID: f.ID, err: err}
				}
				m.engineActivity = "deleting folder…"
				return m, cmd
			}
			m.engineActivity = "delete cancelled"
			return m, nil
		}
		// Permanent delete confirmation (spec 07 §6.7): pendingPermanentDelete
		// carries the focused message; y fires Triage.PermanentDelete.
		if m.pendingPermanentDelete != nil && msg.Topic == "permanent_delete" {
			src := *m.pendingPermanentDelete
			m.pendingPermanentDelete = nil
			if msg.Confirm {
				return m.runTriage("permanent_delete", src, ListPane, func(ctx context.Context, accID int64, s store.Message) error {
					return m.deps.Triage.PermanentDelete(ctx, accID, s.ID)
				})
			}
			m.engineActivity = "permanent delete cancelled"
			return m, nil
		}
		// Unsubscribe confirmation (spec 16): pendingUnsub carries the
		// resolved action; y fires execution, n drops it.
		if m.pendingUnsub != nil && msg.Topic == "unsubscribe" {
			action := *m.pendingUnsub
			if msg.Confirm {
				m.engineActivity = fmt.Sprintf("unsubscribing (%s)…", unsubKindLabel(action.Kind))
				return m, m.executeUnsubCmd(action)
			}
			m.pendingUnsub = nil
			m.pendingUnsubMessageID = ""
			m.engineActivity = "unsubscribe cancelled"
			return m, nil
		}
		return m, nil

	case filterAppliedMsg:
		m.filterActive = true
		m.filterPattern = msg.src
		ids := make([]string, 0, len(msg.messages))
		for _, mm := range msg.messages {
			ids = append(ids, mm.ID)
		}
		m.filterIDs = ids
		// Render the filter results in the list pane via the existing
		// SetMessages path; sentinel folder ID keeps load-more / triage
		// keyed off the current filter, not the underlying folder.
		if !m.searchActive && m.priorFolderID == "" {
			m.priorFolderID = m.list.FolderID
		}
		m.list.FolderID = "filter:" + msg.src
		m.list.SetMessages(msg.messages)
		m.focused = ListPane
		return m, nil

	case bulkDoneMsg:
		if msg.firstErr != nil && msg.succeeded == 0 {
			m.lastError = fmt.Errorf("%s: %w", msg.name, msg.firstErr)
			return m, nil
		}
		// Partial successes log the error but don't surface as red.
		m.lastError = nil
		// Status bar carries the result via engineActivity for a tick.
		if msg.failed == 0 {
			m.engineActivity = fmt.Sprintf("✓ %s %d", msg.name, msg.succeeded)
		} else {
			m.engineActivity = fmt.Sprintf("⚠ %s %d/%d", msg.name, msg.succeeded, msg.succeeded+msg.failed)
		}
		// Filter set is now stale; clear it and reload the prior folder.
		m = m.clearFilter()
		if m.priorFolderID != "" {
			return m, m.loadMessagesCmd(m.priorFolderID)
		}
		return m, nil

	case folderActionDoneMsg:
		if msg.err != nil {
			m.lastError = fmt.Errorf("folder %s: %w", msg.action, msg.err)
			return m, nil
		}
		m.lastError = nil
		switch msg.action {
		case "new":
			m.engineActivity = fmt.Sprintf("✓ created folder %q", msg.name)
		case "rename":
			m.engineActivity = fmt.Sprintf("✓ renamed folder to %q", msg.name)
			// If we just renamed the currently-loaded folder, the
			// list pane keeps working — folder ID stays the same.
		case "delete":
			m.engineActivity = fmt.Sprintf("✓ deleted folder %q", msg.name)
			// If the user was viewing the deleted folder's messages,
			// pop them off — the FK cascade just removed all of
			// them locally.
			if m.list.FolderID == msg.folderID {
				m.list.SetMessages(nil)
				m.viewer.SetMessage(store.Message{})
				m.viewer.current = nil
			}
		}
		// Reload the sidebar in all three cases.
		return m, m.loadFoldersCmd()

	case undoDoneMsg:
		if msg.err != nil {
			if errors.Is(msg.err, UndoEmpty) {
				m.lastError = nil
				m.engineActivity = "nothing to undo"
				return m, nil
			}
			m.lastError = fmt.Errorf("undo: %w", msg.err)
			return m, nil
		}
		m.lastError = nil
		m.engineActivity = fmt.Sprintf("↶ undid: %s", msg.undone.Label)
		// Reload the list so the rolled-back state is visible (an
		// undo of soft-delete must repopulate the row in its
		// original folder).
		if msg.folderID != "" && msg.folderID == m.list.FolderID {
			return m, m.loadMessagesCmd(msg.folderID)
		}
		return m, nil

	case unsubResolvedMsg:
		m.engineActivity = ""
		if msg.err != nil {
			// ErrNoHeader / ErrUnactionable arrive here; surface verbatim.
			m.lastError = fmt.Errorf("unsubscribe: %w", msg.err)
			return m, nil
		}
		m.lastError = nil
		// Park the resolved action and ask the user to confirm. Confirm
		// modal copy varies by Kind so the user sees the URL/addr.
		m.pendingUnsub = &msg.action
		m.pendingUnsubMessageID = msg.messageID
		var prompt string
		switch msg.action.Kind {
		case UnsubscribeOneClickPOST:
			prompt = fmt.Sprintf("One-click unsubscribe by POSTing to %s? [y/N]", msg.action.URL)
		case UnsubscribeBrowserGET:
			prompt = fmt.Sprintf("Open unsubscribe URL in browser?\n  %s\n[y/N]", msg.action.URL)
		case UnsubscribeMailto:
			prompt = fmt.Sprintf("Send unsubscribe mail to %s via your default mail handler? [y/N]", msg.action.Mailto)
		default:
			m.lastError = fmt.Errorf("unsubscribe: unknown action")
			m.pendingUnsub = nil
			return m, nil
		}
		m.confirm = m.confirm.Ask(prompt, "unsubscribe")
		m.mode = ConfirmMode
		return m, nil

	case unsubDoneMsg:
		m.engineActivity = ""
		m.pendingUnsub = nil
		m.pendingUnsubMessageID = ""
		if msg.err != nil {
			m.lastError = fmt.Errorf("unsubscribe failed: %w", msg.err)
			return m, nil
		}
		m.lastError = nil
		switch msg.kind {
		case UnsubscribeOneClickPOST:
			m.engineActivity = "✓ unsubscribed (one-click)"
		case UnsubscribeBrowserGET:
			m.engineActivity = "opened unsubscribe URL in browser"
		case UnsubscribeMailto:
			m.engineActivity = "opened unsubscribe mail in default handler"
		}
		return m, nil

	case triageDoneMsg:
		if msg.err != nil {
			m.lastError = fmt.Errorf("%s: %w", msg.name, msg.err)
			return m, nil
		}
		m.lastError = nil
		// If the action removed the current viewer message from the
		// active folder (delete, archive, move), clear the viewer and
		// shift focus per the dispatcher's hint.
		removed := msg.name == "soft_delete" || msg.name == "archive" || msg.name == "permanent_delete"
		if removed && m.viewer.CurrentMessageID() == msg.msgID {
			m.viewer.SetMessage(store.Message{}) // clears current
			m.viewer.current = nil
		}
		if msg.postFocus != 0 {
			m.focused = msg.postFocus
		}
		// Reload the list so the optimistic mutation (or rollback) is
		// reflected in the current pane. When a filter is active the
		// list pane's FolderID is the sentinel "filter:<pattern>"
		// which doesn't exist in the store — reloading via
		// loadMessagesCmd would return zero rows and make the user
		// think every filtered message disappeared (real-tenant bug
		// reported v0.13.x). Re-run the filter instead so the new
		// state is reflected against the same pattern.
		if m.filterActive {
			m.engineActivity = fmt.Sprintf("✓ %s · u to undo", msg.name)
			return m, m.runFilterCmd(m.filterPattern)
		}
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
	case CalendarMode:
		return m.updateCalendar(msg)
	case OOFMode:
		return m.updateOOF(msg)
	case ComposeConfirmMode:
		return m.updateComposeConfirm(msg)
	case HelpMode:
		return m.updateHelp(msg)
	case CategoryInputMode:
		return m.updateCategoryInput(msg)
	case FolderNameInputMode:
		return m.updateFolderNameInput(msg)
	case URLPickerMode:
		return m.updateURLPicker(msg)
	case FullscreenBodyMode:
		return m.updateFullscreenBody(msg)
	default:
		return m.updateNormal(msg)
	}
}

// updateFullscreenBody handles input while the body is in
// fullscreen mode. j/k scroll the body; Esc / q / z return to
// the three-pane layout.
func (m Model) updateFullscreenBody(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(keyMsg, m.keymap.Down):
		m.viewer.ScrollDown()
		return m, nil
	case key.Matches(keyMsg, m.keymap.Up):
		m.viewer.ScrollUp()
		return m, nil
	case key.Matches(keyMsg, m.keymap.Yank):
		// Single-URL fast path; otherwise no-op (URL picker is
		// only reachable from normal mode by design).
		if len(m.viewer.Links()) == 1 {
			return m.yankURL(m.viewer.Links()[0].URL)
		}
		return m, nil
	}
	switch keyMsg.String() {
	case "esc", "q", "z":
		m.mode = NormalMode
	}
	return m, nil
}

// updateURLPicker handles input while the URL picker overlay is
// open. j/k cursor; Enter / o open in browser; y yank; Esc / q
// close.
func (m Model) updateURLPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	links := m.viewer.Links()
	switch {
	case key.Matches(keyMsg, m.keymap.Up):
		m.urlPicker.Up()
	case key.Matches(keyMsg, m.keymap.Down):
		m.urlPicker.Down(len(links) - 1)
	case key.Matches(keyMsg, m.keymap.Open), key.Matches(keyMsg, m.keymap.OpenURL):
		sel := m.urlPicker.Selected(links)
		if sel != nil {
			go openInBrowser(sel.URL)
			m.engineActivity = "opened URL in browser"
		}
		m.mode = NormalMode
		return m, nil
	case key.Matches(keyMsg, m.keymap.Yank):
		sel := m.urlPicker.Selected(links)
		if sel != nil {
			m.mode = NormalMode
			return m.yankURL(sel.URL)
		}
		return m, nil
	}
	switch keyMsg.String() {
	case "esc", "q":
		m.mode = NormalMode
	}
	return m, nil
}

// updateHelp handles input while the help overlay is open. Esc / q
// close it; everything else is a no-op (the overlay is read-only).
func (m Model) updateHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch strings.ToLower(keyMsg.String()) {
	case "esc", "q", "?":
		m.mode = NormalMode
	}
	return m, nil
}

// updateComposeConfirm handles the post-edit confirm pane. After the
// user's editor exits, the reply flow lands here so they can pick:
//
//	s — save the draft (parse → POST createReply → PATCH body)
//	e — re-open the editor (back to ExecProcess on the same tempfile)
//	d — discard (delete the tempfile, no Graph round-trip)
//
// This fixes the v0.11.0 confusion where editor `:q!` saved the draft
// regardless and there was no visible hint about which key did what.
func (m Model) updateComposeConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch string(keyMsg.Runes) {
	case "s":
		// User chose to save. Run the existing pipeline.
		path := m.composeTempfile
		src := m.composeSourceID
		m.mode = NormalMode
		m.engineActivity = "saving draft…"
		return m, m.saveDraftCmd(path, src)
	case "e":
		// Re-open the editor on the same tempfile. The user's
		// previous edits are still on disk.
		path := m.composeTempfile
		src := m.composeSourceID
		ec, err := compose.EditorCmd(path)
		if err != nil {
			m.mode = NormalMode
			m.lastError = err
			return m, nil
		}
		m.mode = NormalMode
		m.engineActivity = "editing draft…"
		return m, runEditorCmd(path, src, ec)
	case "d":
		// Discard: delete the tempfile, no Graph round-trip.
		path := m.composeTempfile
		m.composeTempfile = ""
		m.composeSourceID = ""
		m.mode = NormalMode
		m.engineActivity = "draft discarded"
		go func() { compose.CleanupTempfile(path) }()
		return m, nil
	}
	if keyMsg.Type == tea.KeyEsc {
		// Treat Esc as "back to confirm" — i.e., stay here. This
		// prevents an accidental Esc from silently discarding work.
		return m, nil
	}
	return m, nil
}

// updateOOF handles input while the out-of-office modal is open.
// Esc closes; `t` toggles the auto-reply enable flag and PATCHes
// /me/mailboxSettings.
func (m Model) updateOOF(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if keyMsg.Type == tea.KeyEsc || string(keyMsg.Runes) == "q" {
		m.mode = NormalMode
		m.oof.Reset()
		return m, nil
	}
	if string(keyMsg.Runes) == "t" {
		if m.oof.settings == nil || m.oof.loading || m.oof.saving {
			return m, nil
		}
		next := !m.oof.settings.AutoReplyEnabled
		m.oof.SetSaving()
		return m, m.toggleOOFCmd(next, *m.oof.settings)
	}
	return m, nil
}

// updateCalendar handles input while the calendar modal is open.
// Esc closes; everything else is swallowed (the modal is read-only).
func (m Model) updateCalendar(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if keyMsg.Type == tea.KeyEsc || string(keyMsg.Runes) == "q" {
		m.mode = NormalMode
		m.calendar.Reset()
	}
	return m, nil
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
	case key.Matches(keyMsg, m.keymap.Help):
		// Spec 04 §12 full overlay. Esc/q/? close.
		m.mode = HelpMode
		return m, nil
	case key.Matches(keyMsg, m.keymap.Search):
		m.mode = SearchMode
		m.searchBuf = ""
		// Remember the folder we came from so Esc / clear restores it.
		if !m.searchActive {
			m.priorFolderID = m.list.FolderID
		}
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
	switch keyMsg.Type {
	case tea.KeyEsc:
		m.mode = NormalMode
		m.searchBuf = ""
		// If a search was active, clear it and restore the prior folder.
		if m.searchActive {
			m.searchActive = false
			m.searchQuery = ""
			m.list.FolderID = m.priorFolderID
			if m.priorFolderID != "" {
				return m, m.loadMessagesCmd(m.priorFolderID)
			}
		}
		return m, nil
	case tea.KeyEnter:
		q := strings.TrimSpace(m.searchBuf)
		m.mode = NormalMode
		if q == "" {
			return m, nil
		}
		m.searchActive = true
		m.searchQuery = q
		m.list.FolderID = searchFolderID(q)
		m.focused = ListPane
		return m, m.runSearchCmd(q)
	case tea.KeyBackspace:
		if len(m.searchBuf) > 0 {
			m.searchBuf = m.searchBuf[:len(m.searchBuf)-1]
		}
		return m, nil
	}
	// Append printable runes.
	if keyMsg.Type == tea.KeyRunes || keyMsg.Type == tea.KeySpace {
		m.searchBuf += keyMsg.String()
	}
	return m, nil
}

// searchFolderID is the sentinel folder-id used while search results
// are displayed. It's never persisted to the store; the list pane
// just keys off it to know "we're showing search results, don't try
// to load from a real folder".
func searchFolderID(q string) string { return "search:" + q }

// runSearchCmd runs the FTS5 search and returns a MessagesLoadedMsg
// keyed to the sentinel search-folder ID.
func (m Model) runSearchCmd(q string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		hits, err := m.deps.Store.Search(ctx, store.SearchQuery{
			Query:     q,
			AccountID: accountID,
			Limit:     200,
		})
		if err != nil {
			return ErrorMsg{Err: err}
		}
		msgs := make([]store.Message, 0, len(hits))
		for _, h := range hits {
			msgs = append(msgs, h.Message)
		}
		return MessagesLoadedMsg{FolderID: searchFolderID(q), Messages: msgs}
	}
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
	case "filter":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("filter: usage `:filter <pattern>` (spec 08 operators or plain text)")
			return m, nil
		}
		patternSrc := strings.TrimSpace(strings.TrimPrefix(line, "filter"))
		return m, m.runFilterCmd(patternSrc)
	case "unfilter":
		prior := m.priorFolderID
		m = m.clearFilter()
		if prior == "" {
			// Fallback: user ran `:filter` before any folder finished
			// loading (priorFolderID was captured as ""). Land on the
			// account's Inbox so :unfilter is never a stuck no-op.
			// Real-tenant regression v0.15.x.
			if inbox, ok := m.folders.FindByName("inbox"); ok {
				prior = inbox.ID
				m.list.FolderID = inbox.ID
			}
		}
		if prior != "" {
			return m, m.loadMessagesCmd(prior)
		}
		// Still nothing to land on (folders not synced). At minimum,
		// clear the list so the user isn't staring at stale filter
		// rows.
		m.list.SetMessages(nil)
		m.list.FolderID = ""
		return m, nil
	case "cal", "calendar":
		if m.deps.Calendar == nil {
			m.lastError = fmt.Errorf("calendar: not wired (CLI mode or unsigned)")
			return m, nil
		}
		m.mode = CalendarMode
		m.calendar.SetLoading()
		return m, m.fetchCalendarCmd()
	case "unsub", "unsubscribe":
		// Resolves the focused message (viewer or list) through the
		// same flow as the U keybinding. Mirrors aerc's `:unsubscribe`
		// convention (spec 16 §2).
		var msgID string
		if cur := m.viewer.current; cur != nil {
			msgID = cur.ID
		} else if sel, ok := m.list.Selected(); ok {
			msgID = sel.ID
		}
		if msgID == "" {
			m.lastError = fmt.Errorf("unsubscribe: no message focused")
			return m, nil
		}
		return m.startUnsubscribe(msgID)
	case "help", "?":
		// Spec 04 §6.4 / §12: full overlay listing every binding,
		// grouped by section. Stateless model — pulls keys off the
		// current keymap so user overrides surface immediately.
		m.mode = HelpMode
		return m, nil
	case "refresh":
		// Same as Ctrl+R — kick a sync cycle. Spec 04 §6.4.
		// Wake() is the right hook (single-shot, debounced) rather
		// than SyncAll() which can overlap with the loop's own
		// timer-driven cycle.
		if m.deps.Engine != nil {
			m.deps.Engine.Wake()
			m.engineActivity = "syncing…"
		}
		return m, nil
	case "folder":
		// `:folder <name>` jumps the list pane to the folder whose
		// DisplayName or WellKnownName matches. Spec 04 §6.4.
		if len(args) < 2 {
			m.lastError = fmt.Errorf("folder: usage `:folder <name>` (DisplayName or well-known like inbox/archive)")
			return m, nil
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "folder"))
		f, ok := m.folders.FindByName(name)
		if !ok {
			m.lastError = fmt.Errorf("folder: %q not found in sidebar", name)
			return m, nil
		}
		m.list.FolderID = f.ID
		m.focused = ListPane
		return m, m.loadMessagesCmd(f.ID)
	case "open":
		// `:open` opens the focused message's webLink in the system
		// browser. Spec 04 §6.4. Best-effort fire-and-forget; if
		// the user's $BROWSER fails, the URL is on the status bar
		// for them to copy.
		var link string
		if cur := m.viewer.current; cur != nil {
			link = cur.WebLink
		} else if sel, ok := m.list.Selected(); ok {
			link = sel.WebLink
		}
		if link == "" {
			m.lastError = fmt.Errorf("open: no message focused (or webLink not yet synced)")
			return m, nil
		}
		go openInBrowser(link)
		m.engineActivity = "opened in browser"
		return m, nil
	case "backfill":
		// `:backfill` triggers spec 03's Backfill(folderID, until)
		// to pull older messages past the local cache wall. Defaults
		// to the focused folder's oldest cached `received_at` as
		// the `until` bound — same shape as the smart-scroll auto-
		// trigger, just user-initiated.
		if m.deps.Engine == nil {
			m.lastError = fmt.Errorf("backfill: not wired (CLI mode or unsigned)")
			return m, nil
		}
		folderID := m.list.FolderID
		if strings.HasPrefix(folderID, "filter:") || folderID == "" {
			m.lastError = fmt.Errorf("backfill: focus a folder (not a filter view) first")
			return m, nil
		}
		until := m.list.OldestReceivedAt()
		m.engineActivity = "backfilling older messages…"
		return m, m.kickBackfillCmd(folderID, until)
	case "search":
		// `:search <query>` enters search mode pre-populated with
		// the query string and runs it. Spec 04 §6.4. Mirrors `/`
		// + typing + Enter as a single-step command; useful for
		// scripted invocation from the cmd-bar.
		if len(args) < 2 {
			m.lastError = fmt.Errorf("search: usage `:search <query>` (FTS over local cache)")
			return m, nil
		}
		query := strings.TrimSpace(strings.TrimPrefix(line, "search"))
		if !m.searchActive {
			m.priorFolderID = m.list.FolderID
		}
		m.searchActive = true
		m.searchQuery = query
		m.list.FolderID = searchFolderID(query)
		m.focused = ListPane
		return m, m.runSearchCmd(query)
	case "ooo", "outofoffice", "oof":
		if m.deps.Mailbox == nil {
			m.lastError = fmt.Errorf("ooo: not wired (CLI mode or unsigned)")
			return m, nil
		}
		m.mode = OOFMode
		m.oof.SetLoading()
		return m, m.fetchOOFCmd()
	}
	m.lastError = fmt.Errorf("unknown command: %s", line)
	return m, nil
}

// runFilterCmd compiles the supplied pattern and runs it against the
// local store. The matched messages replace the list pane contents.
// Plain text (no `~` operator) is treated as a CONTAINS search across
// subject and body — `:filter foo` becomes `~B *foo*`. Wrapping with
// `*…*` is what every search box does (Gmail, Outlook, Spotlight); a
// bare `~B foo` would compile to MatchExact and surprise the user
// when their substring doesn't equal the entire subject.
func (m Model) runFilterCmd(src string) tea.Cmd {
	src = strings.TrimSpace(src)
	if !strings.Contains(src, "~") {
		src = "~B *" + src + "*"
	}
	return func() tea.Msg {
		root, err := pattern.Parse(src)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("filter: %w", err)}
		}
		clause, err := pattern.CompileLocal(root)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("filter: %w", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		msgs, err := m.deps.Store.SearchByPredicate(ctx, accountID, clause.Where, clause.Args, 1000)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("filter: %w", err)}
		}
		return filterAppliedMsg{src: src, messages: msgs}
	}
}

func (m Model) clearFilter() Model {
	m.filterActive = false
	m.filterPattern = ""
	m.filterIDs = nil
	m.bulkPending = false
	if m.priorFolderID != "" {
		m.list.FolderID = m.priorFolderID
	}
	return m
}

type filterAppliedMsg struct {
	src      string
	messages []store.Message
}

type bulkDoneMsg struct {
	name      string
	succeeded int
	failed    int
	firstErr  error
}

// oofLoadedMsg is the result of the :ooo fetch (spec 13). Either
// Settings is populated or Err is set.
type oofLoadedMsg struct {
	Settings *MailboxSettings
	Err      error
}

// oofToggledMsg is the result of the t-key PATCH (spec 13). Settings
// is the post-PATCH state on success; Err carries the failure.
type oofToggledMsg struct {
	Settings *MailboxSettings
	Err      error
}

// fetchOOFCmd hits the MailboxClient and returns oofLoadedMsg.
func (m Model) fetchOOFCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := m.deps.Mailbox.Get(ctx)
		return oofLoadedMsg{Settings: s, Err: err}
	}
}

// toggleOOFCmd PATCHes /me/mailboxSettings to flip the auto-reply
// status. Preserves the existing internal/external messages.
func (m Model) toggleOOFCmd(enabled bool, prev MailboxSettings) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := m.deps.Mailbox.SetAutoReply(ctx, enabled, prev.InternalReplyMessage, prev.ExternalReplyMessage)
		if err != nil {
			return oofToggledMsg{Err: err}
		}
		// Update the locally-held settings; no need to re-fetch.
		next := prev
		next.AutoReplyEnabled = enabled
		return oofToggledMsg{Settings: &next}
	}
}

// calendarFetchedMsg is the result of the :cal Cmd. Either Events is
// populated or Err is set.
type calendarFetchedMsg struct {
	Events []CalendarEvent
	Err    error
}

// fetchCalendarCmd hits the CalendarFetcher in a goroutine and returns
// a calendarFetchedMsg.
func (m Model) fetchCalendarCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		es, err := m.deps.Calendar.ListEventsToday(ctx)
		return calendarFetchedMsg{Events: es, Err: err}
	}
}

// confirmBulk pops up the confirm modal for a destructive bulk
// operation. Stores the action name in pendingBulk so the
// ConfirmResult handler knows what to dispatch on `y`.
func (m Model) confirmBulk(action string, count int) (tea.Model, tea.Cmd) {
	if m.deps.Bulk == nil {
		m.lastError = fmt.Errorf("bulk: not wired")
		return m, nil
	}
	verb := action
	if action == "soft_delete" {
		verb = "delete"
	}
	m.confirm = m.confirm.Ask(fmt.Sprintf("%s %d messages?", strings.Title(verb), count), "bulk:"+action)
	m.pendingBulk = action
	m.mode = ConfirmMode
	return m, nil
}

// runBulkCmd fires the BulkExecutor for the named action against the
// current filter. The caller has already confirmed.
func (m Model) runBulkCmd(action string) tea.Cmd {
	if m.deps.Bulk == nil {
		return nil
	}
	ids := append([]string(nil), m.filterIDs...) // copy to avoid races with model mutation
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var (
			results []BulkResult
			err     error
		)
		switch action {
		case "soft_delete":
			results, err = m.deps.Bulk.BulkSoftDelete(ctx, accountID, ids)
		case "archive":
			results, err = m.deps.Bulk.BulkArchive(ctx, accountID, ids)
		case "mark_read":
			results, err = m.deps.Bulk.BulkMarkRead(ctx, accountID, ids)
		default:
			return ErrorMsg{Err: fmt.Errorf("runBulkCmd: unknown action %q", action)}
		}
		var (
			ok, fail int
			firstErr error
		)
		for _, r := range results {
			if r.Err != nil {
				fail++
				if firstErr == nil {
					firstErr = r.Err
				}
			} else {
				ok++
			}
		}
		// Outer error trumps the per-item error tally.
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return bulkDoneMsg{name: action, succeeded: ok, failed: fail, firstErr: firstErr}
	}
}

func (m Model) dispatchFolders(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keymap.Up):
		m.folders.Up()
	case key.Matches(msg, m.keymap.Down):
		m.folders.Down()
	case key.Matches(msg, m.keymap.PageUp):
		m.folders.PageUp()
	case key.Matches(msg, m.keymap.PageDown):
		m.folders.PageDown()
	case key.Matches(msg, m.keymap.Home):
		m.folders.JumpTop()
	case key.Matches(msg, m.keymap.End):
		m.folders.JumpBottom()
	case key.Matches(msg, m.keymap.Expand):
		if !m.folders.ToggleExpand() {
			// Leaf folder or saved-search row — paint a hint so the
			// keypress isn't visually silent. The "no children
			// synced locally" wording is intentional: the folder
			// MAY have children on the server (Graph
			// /me/mailFolders is non-recursive in v0.x) — don't
			// claim the user's mailbox is structured incorrectly.
			m.engineActivity = "no subfolders to expand here"
		}
	case key.Matches(msg, m.keymap.NewFolder):
		f, _ := m.folders.Selected()
		return m.startFolderNameInput("new", "", f.ID)
	case key.Matches(msg, m.keymap.RenameFolder):
		f, ok := m.folders.Selected()
		if !ok {
			return m, nil
		}
		return m.startFolderNameInput("rename", f.ID, "")
	case key.Matches(msg, m.keymap.DeleteFolder):
		f, ok := m.folders.Selected()
		if !ok {
			return m, nil
		}
		return m.startFolderDelete(f)
	case key.Matches(msg, m.keymap.Open), key.Matches(msg, m.keymap.Right):
		// Saved-search row: run its pattern via the existing filter
		// machinery. Selection auto-focuses the list pane (parity
		// with regular folder navigation).
		if ss, ok := m.folders.SelectedSavedSearch(); ok {
			m.focused = ListPane
			if !m.searchActive && m.priorFolderID == "" {
				m.priorFolderID = m.list.FolderID
			}
			m.searchActive = false
			m.searchQuery = ""
			return m, m.runFilterCmd(ss.Pattern)
		}
		f, ok := m.folders.Selected()
		if ok {
			m.list.FolderID = f.ID
			m.list.ResetLimit() // new folder → fresh first page
			m.focused = ListPane
			// Switching folders implicitly cancels any active search;
			// otherwise the "search: foo (esc to clear)" reminder
			// persists over messages that aren't search results.
			m.searchActive = false
			m.searchQuery = ""
			return m, m.loadMessagesCmd(f.ID)
		}
	}
	return m, nil
}

func (m Model) dispatchList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// `;` prefix begins a bulk-apply chord. Only meaningful while a
	// filter is active.
	if msg.Type == tea.KeyRunes && string(msg.Runes) == ";" {
		if !m.filterActive {
			m.lastError = fmt.Errorf(";: requires an active filter (run :filter <pattern>)")
			return m, nil
		}
		m.bulkPending = true
		return m, nil
	}
	if m.bulkPending {
		m.bulkPending = false
		switch string(msg.Runes) {
		case "d":
			return m.confirmBulk("soft_delete", len(m.filterIDs))
		case "a":
			return m.confirmBulk("archive", len(m.filterIDs))
		}
		// Unknown chord follow-up: clear pending, fall through.
	}
	switch {
	case key.Matches(msg, m.keymap.Up):
		m.list.Up()
	case key.Matches(msg, m.keymap.PageUp):
		m.list.PageUp()
	case key.Matches(msg, m.keymap.PageDown):
		m.list.PageDown()
		// Re-run the smart pre-fetch / wall-sync flow so a PgDn that
		// lands the cursor near the bottom triggers the same
		// pagination + Backfill kick a sequence of j-presses would.
		if !m.searchActive && m.list.ShouldLoadMore() {
			m.list.MarkLoading()
			return m, m.loadMessagesCmd(m.list.FolderID)
		}
		if !m.searchActive && m.list.ShouldKickWallSync() && m.deps.Engine != nil {
			folderID := m.list.FolderID
			until := m.list.OldestReceivedAt()
			m.list.MarkWallSyncRequested()
			m.engineActivity = "loading older messages…"
			return m, m.kickBackfillCmd(folderID, until)
		}
	case key.Matches(msg, m.keymap.Home):
		m.list.JumpTop()
	case key.Matches(msg, m.keymap.End):
		m.list.JumpBottom()
		if !m.searchActive && m.list.ShouldKickWallSync() && m.deps.Engine != nil {
			folderID := m.list.FolderID
			until := m.list.OldestReceivedAt()
			m.list.MarkWallSyncRequested()
			m.engineActivity = "loading older messages…"
			return m, m.kickBackfillCmd(folderID, until)
		}
	case key.Matches(msg, m.keymap.Down):
		m.list.Down()
		// Smart pre-fetch: when the cursor approaches the end of the
		// currently-loaded slice, load the next page from the local
		// store. The engine's progressive backfill keeps the local
		// store filling from Graph; we just paginate through what's
		// cached. Searches don't paginate (the FTS limit is fixed).
		if !m.searchActive && m.list.ShouldLoadMore() {
			m.list.MarkLoading()
			return m, m.loadMessagesCmd(m.list.FolderID)
		}
		// Cache wall: kick a Backfill ONCE per cache-exhausted state.
		// Earlier (v0.8.0) we fired SyncAll() on every j press, which
		// (a) overlapped with the engine's own loop and (b) used
		// pullSince — a NEWER-than filter that never returns OLDER
		// messages. Backfill is the right call: it pulls messages
		// older than the oldest currently-cached message in this
		// folder. The wallSyncRequested flag debounces: only the
		// first j at the wall kicks; the rest are silent until
		// SetMessages clears the flag.
		if !m.searchActive && m.list.ShouldKickWallSync() && m.deps.Engine != nil {
			folderID := m.list.FolderID
			until := m.list.OldestReceivedAt()
			m.list.MarkWallSyncRequested()
			m.engineActivity = "loading older messages…"
			return m, m.kickBackfillCmd(folderID, until)
		}
	case key.Matches(msg, m.keymap.Open):
		sel, ok := m.list.Selected()
		if ok {
			m.viewer.SetMessage(sel)
			m.focused = ViewerPane
			return m, m.openMessageCmd(sel)
		}
	case key.Matches(msg, m.keymap.MarkRead):
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		return m.runTriage("mark_read", sel, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
			return m.deps.Triage.MarkRead(ctx, accID, src.ID)
		})
	case key.Matches(msg, m.keymap.MarkUnread):
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		return m.runTriage("mark_unread", sel, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
			return m.deps.Triage.MarkUnread(ctx, accID, src.ID)
		})
	case key.Matches(msg, m.keymap.ToggleFlag):
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		return m.runTriage("toggle_flag", sel, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
			return m.deps.Triage.ToggleFlag(ctx, accID, src.ID, src.FlagStatus == "flagged")
		})
	case key.Matches(msg, m.keymap.Delete):
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		return m.runTriage("soft_delete", sel, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
			return m.deps.Triage.SoftDelete(ctx, accID, src.ID)
		})
	case key.Matches(msg, m.keymap.PermanentDelete):
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		return m.startPermanentDelete(sel)
	case key.Matches(msg, m.keymap.Archive):
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		return m.runTriage("archive", sel, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
			return m.deps.Triage.Archive(ctx, accID, src.ID)
		})
	case key.Matches(msg, m.keymap.Unsubscribe):
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		return m.startUnsubscribe(sel.ID)
	case key.Matches(msg, m.keymap.AddCategory):
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		return m.startCategoryInput("add", sel)
	case key.Matches(msg, m.keymap.RemoveCategory):
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		return m.startCategoryInput("remove", sel)
	case key.Matches(msg, m.keymap.Undo):
		return m.runUndo()
	}
	return m, nil
}

// runUndo dispatches the spec 07 §11 undo flow. Returns a Cmd that
// pops + applies the most recent UndoEntry; result lands as
// undoDoneMsg. Errors (empty stack, dispatch failure) surface to
// the status bar.
func (m Model) runUndo() (tea.Model, tea.Cmd) {
	if m.deps.Triage == nil {
		m.lastError = fmt.Errorf("undo: not wired (run from cmd_run.go path)")
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
		undone, err := m.deps.Triage.Undo(ctx, accountID)
		return undoDoneMsg{undone: undone, folderID: folderID, err: err}
	}
	return m, cmd
}

// startFolderNameInput opens the spec 18 name modal for `N`
// (create) or `R` (rename). For rename, folderID identifies the
// target and the buffer pre-seeds with the current name. For new,
// parentID is the parent folder (empty = top-level) and the
// buffer is empty.
func (m Model) startFolderNameInput(action, folderID, parentID string) (tea.Model, tea.Cmd) {
	if m.deps.Triage == nil {
		m.lastError = fmt.Errorf("folder: not wired (run from cmd_run.go path)")
		return m, nil
	}
	m.pendingFolderAction = action
	m.pendingFolderID = folderID
	m.pendingFolderParent = parentID
	switch action {
	case "rename":
		// Pre-seed buffer with current name so the user edits in place.
		if f, ok := m.folders.Selected(); ok && f.ID == folderID {
			m.folderNameBuf = f.DisplayName
		}
	default:
		m.folderNameBuf = ""
	}
	m.mode = FolderNameInputMode
	return m, nil
}

// updateFolderNameInput handles the spec 18 name modal — typing,
// Enter, Esc.
func (m Model) updateFolderNameInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.Type {
	case tea.KeyEsc:
		m.mode = NormalMode
		m.pendingFolderAction = ""
		m.pendingFolderID = ""
		m.pendingFolderParent = ""
		m.folderNameBuf = ""
		m.engineActivity = "folder action cancelled"
		return m, nil
	case tea.KeyEnter:
		name := strings.TrimSpace(m.folderNameBuf)
		if name == "" {
			m.mode = NormalMode
			m.pendingFolderAction = ""
			m.pendingFolderID = ""
			m.pendingFolderParent = ""
			m.engineActivity = "folder name required"
			return m, nil
		}
		action := m.pendingFolderAction
		fid := m.pendingFolderID
		parent := m.pendingFolderParent
		m.mode = NormalMode
		m.pendingFolderAction = ""
		m.pendingFolderID = ""
		m.pendingFolderParent = ""
		m.folderNameBuf = ""
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		switch action {
		case "new":
			cmd := func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				res, err := m.deps.Triage.CreateFolder(ctx, accountID, parent, name)
				return folderActionDoneMsg{action: "new", name: name, created: res, err: err}
			}
			m.engineActivity = "creating folder…"
			return m, cmd
		case "rename":
			cmd := func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				err := m.deps.Triage.RenameFolder(ctx, fid, name)
				return folderActionDoneMsg{action: "rename", name: name, folderID: fid, err: err}
			}
			m.engineActivity = "renaming folder…"
			return m, cmd
		}
		return m, nil
	case tea.KeyBackspace:
		if len(m.folderNameBuf) > 0 {
			m.folderNameBuf = m.folderNameBuf[:len(m.folderNameBuf)-1]
		}
		return m, nil
	}
	if keyMsg.Type == tea.KeyRunes {
		m.folderNameBuf += string(keyMsg.Runes)
	}
	return m, nil
}

// startFolderDelete opens the spec 18 delete-confirm modal.
func (m Model) startFolderDelete(f store.Folder) (tea.Model, tea.Cmd) {
	if m.deps.Triage == nil {
		m.lastError = fmt.Errorf("folder: not wired (run from cmd_run.go path)")
		return m, nil
	}
	prompt := fmt.Sprintf("Delete folder %q? Children + messages cascade to Deleted Items server-side.\n\nUse Outlook's Deleted Items to recover.\n\n[y]es / [N]o", f.DisplayName)
	m.pendingFolderDelete = &f
	m.confirm = m.confirm.Ask(prompt, "delete_folder")
	m.mode = ConfirmMode
	return m, nil
}

// folderActionDoneMsg is the result of a folder create / rename /
// delete dispatch. The handler in Update reloads the sidebar on
// success, surfaces the error otherwise.
type folderActionDoneMsg struct {
	action   string
	name     string
	folderID string
	created  CreatedFolder
	err      error
}

// startCategoryInput opens the spec 07 §6.9 / §6.10 prompt. action
// is "add" or "remove"; src is the focused message. The user types
// the category name; Enter dispatches; Esc cancels.
func (m Model) startCategoryInput(action string, src store.Message) (tea.Model, tea.Cmd) {
	if m.deps.Triage == nil {
		m.lastError = fmt.Errorf("category: not wired (run from cmd_run.go path)")
		return m, nil
	}
	m.pendingCategoryAction = action
	m.pendingCategoryMsg = &src
	m.categoryBuf = ""
	m.mode = CategoryInputMode
	return m, nil
}

// updateCategoryInput handles input for the spec 07 category
// prompt. Type the name + Enter to dispatch; Esc cancels.
func (m Model) updateCategoryInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.Type {
	case tea.KeyEsc:
		m.mode = NormalMode
		m.pendingCategoryAction = ""
		m.pendingCategoryMsg = nil
		m.categoryBuf = ""
		m.engineActivity = "category input cancelled"
		return m, nil
	case tea.KeyEnter:
		cat := strings.TrimSpace(m.categoryBuf)
		if cat == "" {
			m.mode = NormalMode
			m.pendingCategoryAction = ""
			m.pendingCategoryMsg = nil
			return m, nil
		}
		action := m.pendingCategoryAction
		src := *m.pendingCategoryMsg
		m.mode = NormalMode
		m.pendingCategoryAction = ""
		m.pendingCategoryMsg = nil
		m.categoryBuf = ""
		switch action {
		case "add":
			return m.runTriage("add_category", src, ListPane, func(ctx context.Context, accID int64, s store.Message) error {
				return m.deps.Triage.AddCategory(ctx, accID, s.ID, cat)
			})
		case "remove":
			return m.runTriage("remove_category", src, ListPane, func(ctx context.Context, accID int64, s store.Message) error {
				return m.deps.Triage.RemoveCategory(ctx, accID, s.ID, cat)
			})
		}
		return m, nil
	case tea.KeyBackspace:
		if len(m.categoryBuf) > 0 {
			m.categoryBuf = m.categoryBuf[:len(m.categoryBuf)-1]
		}
		return m, nil
	}
	if keyMsg.Type == tea.KeyRunes {
		m.categoryBuf += string(keyMsg.Runes)
	}
	return m, nil
}

// startPermanentDelete opens the spec 07 §6.7 confirm modal. The
// modal copy explicitly names the irreversibility so the user
// can't claim "I didn't realise"; the y key fires
// `Triage.PermanentDelete`. n / Esc cancels.
func (m Model) startPermanentDelete(src store.Message) (tea.Model, tea.Cmd) {
	if m.deps.Triage == nil {
		m.lastError = fmt.Errorf("permanent_delete: not wired (run from cmd_run.go path)")
		return m, nil
	}
	subj := src.Subject
	if subj == "" {
		subj = "(no subject)"
	}
	prompt := fmt.Sprintf(
		"PERMANENT DELETE — irreversible.\n\nMessage: %q from %s\n\nThis bypasses Deleted Items. The message is gone from your tenant.\n\n[y]es / [N]o",
		truncateForModal(subj, 60),
		src.FromAddress,
	)
	m.pendingPermanentDelete = &src
	m.confirm = m.confirm.Ask(prompt, "permanent_delete")
	m.mode = ConfirmMode
	return m, nil
}

// truncateForModal cuts a string for inline display in a confirm
// modal. Subjects can be hundreds of characters; the modal would
// blow past the screen width without trimming.
func truncateForModal(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// startUnsubscribe begins the spec 16 U flow for a message id. If
// Unsubscribe wiring is missing (CLI path / unsigned), surfaces a
// friendly error. Otherwise kicks off resolveUnsubCmd which lands
// as unsubResolvedMsg → confirm modal.
func (m Model) startUnsubscribe(messageID string) (tea.Model, tea.Cmd) {
	if m.deps.Unsubscribe == nil {
		m.lastError = fmt.Errorf("unsubscribe: not wired (run from cmd_run.go path)")
		return m, nil
	}
	if messageID == "" {
		m.lastError = fmt.Errorf("unsubscribe: no message focused")
		return m, nil
	}
	m.engineActivity = "checking unsubscribe header…"
	return m, m.resolveUnsubCmd(messageID)
}

// runTriage is the shared dispatch boilerplate. The caller supplies
// the source message (from list selection or viewer.current) and a
// post-action focus hint (where to put focus after Graph confirms).
// Errors land in m.lastError via triageDoneMsg.
func (m Model) runTriage(name string, src store.Message, postFocus Pane,
	fn func(context.Context, int64, store.Message) error) (tea.Model, tea.Cmd) {
	if m.deps.Triage == nil {
		m.lastError = fmt.Errorf("triage: not wired (run from cmd_run.go path)")
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
		err := fn(ctx, accountID, src)
		return triageDoneMsg{name: name, folderID: folderID, postFocus: postFocus, msgID: src.ID, err: err}
	}
	return m, cmd
}

type triageDoneMsg struct {
	name      string
	folderID  string
	postFocus Pane
	msgID     string
	err       error
}

// undoDoneMsg is the result of TriageExecutor.Undo. On success
// `undone` carries the UI label ("deleted", "marked read", etc.)
// for the status bar. On error, the err field is set; an empty
// stack surfaces a friendly "nothing to undo" message rather than
// a red error.
type undoDoneMsg struct {
	undone   UndoneAction
	folderID string
	err      error
}

// unsubResolvedMsg is the result of UnsubscribeService.Resolve. The
// dispatcher routes it into the confirm modal (one-click / browser /
// mailto) or surfaces a friendly error (no header, malformed).
type unsubResolvedMsg struct {
	messageID string
	action    UnsubscribeAction
	err       error
}

// unsubDoneMsg is the result of the action execution (POST or
// browser-open). For browser/mailto the Cmd returns immediately and
// this carries the friendly status message; for one-click POST it
// carries the success/failure of the network call.
type unsubDoneMsg struct {
	kind UnsubscribeKind
	url  string
	err  error
}

// unsubKindLabel renders an UnsubscribeKind for the status bar.
func unsubKindLabel(k UnsubscribeKind) string {
	switch k {
	case UnsubscribeOneClickPOST:
		return "one-click"
	case UnsubscribeBrowserGET:
		return "browser"
	case UnsubscribeMailto:
		return "mailto"
	}
	return "unknown"
}

// resolveUnsubCmd kicks off the Resolve flow. Returns unsubResolvedMsg.
func (m Model) resolveUnsubCmd(messageID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		action, err := m.deps.Unsubscribe.Resolve(ctx, messageID)
		return unsubResolvedMsg{messageID: messageID, action: action, err: err}
	}
}

// executeUnsubCmd runs the chosen action (after y in the confirm
// modal). One-click is the only path that hits the network; the
// other two are local fire-and-forget.
func (m Model) executeUnsubCmd(action UnsubscribeAction) tea.Cmd {
	return func() tea.Msg {
		switch action.Kind {
		case UnsubscribeOneClickPOST:
			ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
			defer cancel()
			err := m.deps.Unsubscribe.OneClickPOST(ctx, action.URL)
			return unsubDoneMsg{kind: action.Kind, url: action.URL, err: err}
		case UnsubscribeBrowserGET:
			openInBrowser(action.URL)
			return unsubDoneMsg{kind: action.Kind, url: action.URL}
		case UnsubscribeMailto:
			// v1: hand off to the OS mail handler via mailto: URL. Spec
			// 15 integration (drop a draft on Outlook server) is a
			// follow-up — the OS hand-off is enough to unblock the user.
			openInBrowser("mailto:" + action.Mailto)
			return unsubDoneMsg{kind: action.Kind, url: action.Mailto}
		}
		return unsubDoneMsg{kind: action.Kind, err: fmt.Errorf("unsubscribe: unknown action kind")}
	}
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
			return BodyRenderedMsg{MessageID: msg.ID, Text: view.Text, Links: convertLinks(view.Links), State: int(view.State)}
		}
		// BodyFetching: dispatch the fetch synchronously inside this
		// goroutine and return the final rendered view.
		if f, ok := r.(bodyAsyncFetcher); ok {
			final, err := f.FetchBodyAsync(ctx, &msg, opts)
			if err != nil {
				return BodyRenderedMsg{MessageID: msg.ID, Text: "fetch error: " + err.Error(), State: int(render.BodyError)}
			}
			return BodyRenderedMsg{MessageID: msg.ID, Text: final.Text, Links: convertLinks(final.Links), State: int(final.State)}
		}
		return BodyRenderedMsg{MessageID: msg.ID, Text: view.Text, Links: convertLinks(view.Links), State: int(view.State)}
	}
}

// isStaleIDError reports whether a body-render error text smells
// like Graph's "ID not found at this location" path — typically a
// 404 with `ErrorItemNotFound` or `RequestBroker--ParseUri` style
// payload, OR the human-readable variant. Used by the
// BodyRenderedMsg handler to recover from stale local rows that
// Graph has since invalidated (soft-delete / archive / user move).
func isStaleIDError(s string) bool {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "erroritemnotfound"),
		strings.Contains(low, "object not found"),
		strings.Contains(low, "the specified object was not found"),
		strings.Contains(low, "not found"):
		return true
	}
	return false
}

// deleteStaleLocalRow returns a tea.Cmd that deletes the local
// messages row keyed by id. Used after Graph confirms (via 404)
// that the row's id no longer maps to any server-side message —
// keeping the row would re-surface the same 404 on every open
// attempt.
func (m Model) deleteStaleLocalRow(id string) tea.Cmd {
	if id == "" || m.deps.Store == nil {
		return nil
	}
	st := m.deps.Store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := st.DeleteMessage(ctx, id); err != nil {
			return ErrorMsg{Err: fmt.Errorf("clean stale row: %w", err)}
		}
		// Caller batches a loadMessagesCmd / runFilterCmd after this
		// so no follow-up message is required here.
		return nil
	}
}

// convertLinks translates render.ExtractedLink → ui.BodyLink. The
// UI defines its own link type so messages.go doesn't import
// internal/render (CLAUDE.md §2 layering).
func convertLinks(in []render.ExtractedLink) []BodyLink {
	out := make([]BodyLink, len(in))
	for i, l := range in {
		out[i] = BodyLink{Index: l.Index, URL: l.URL, Text: l.Text}
	}
	return out
}

func (m Model) dispatchViewer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keymap.Left):
		m.focused = ListPane
	case key.Matches(msg, m.keymap.Down):
		m.viewer.ScrollDown()
	case key.Matches(msg, m.keymap.Up):
		m.viewer.ScrollUp()
	case key.Matches(msg, m.keymap.PageDown):
		m.viewer.PageDown()
	case key.Matches(msg, m.keymap.PageUp):
		m.viewer.PageUp()
	case key.Matches(msg, m.keymap.Home):
		m.viewer.JumpTop()
	case key.Matches(msg, m.keymap.End):
		m.viewer.JumpBottom()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "H":
		// Capital H toggles compact ↔ full headers (mutt convention).
		// Compact is the default; full expands To/Cc/Bcc.
		m.viewer.ToggleHeaders()
	case key.Matches(msg, m.keymap.ToggleFlag):
		if cur := m.viewer.current; cur != nil {
			return m.runTriage("toggle_flag", *cur, ViewerPane, func(ctx context.Context, accID int64, src store.Message) error {
				return m.deps.Triage.ToggleFlag(ctx, accID, src.ID, src.FlagStatus == "flagged")
			})
		}
	case key.Matches(msg, m.keymap.Delete):
		if cur := m.viewer.current; cur != nil {
			// After delete, the message is gone — pop back to the list
			// so the user sees what's next. The triageDoneMsg handler
			// applies the postFocus hint.
			return m.runTriage("soft_delete", *cur, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
				return m.deps.Triage.SoftDelete(ctx, accID, src.ID)
			})
		}
	case key.Matches(msg, m.keymap.PermanentDelete):
		if cur := m.viewer.current; cur != nil {
			return m.startPermanentDelete(*cur)
		}
	case key.Matches(msg, m.keymap.Archive):
		if cur := m.viewer.current; cur != nil {
			return m.runTriage("archive", *cur, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
				return m.deps.Triage.Archive(ctx, accID, src.ID)
			})
		}
	case key.Matches(msg, m.keymap.MarkRead):
		// Per spec 15 §9 the viewer-pane `r` binding is REPLY (mutt
		// convention). The list-pane `r` stays mark-read.
		if cur := m.viewer.current; cur != nil && m.deps.Drafts != nil {
			return m, m.startReplyCmd(*cur)
		}
		if m.deps.Drafts == nil {
			m.lastError = fmt.Errorf("reply: not wired (drafts component missing)")
		}
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "s":
		// Open the most-recently-saved draft in Outlook. The webLink
		// is set on draftSavedMsg and lives on the Model until the
		// next compose action or Esc clears it.
		if m.lastDraftWebLink != "" {
			go openInBrowser(m.lastDraftWebLink)
		}
	case key.Matches(msg, m.keymap.Unsubscribe):
		if cur := m.viewer.current; cur != nil {
			return m.startUnsubscribe(cur.ID)
		}
	case key.Matches(msg, m.keymap.AddCategory):
		if cur := m.viewer.current; cur != nil {
			return m.startCategoryInput("add", *cur)
		}
	case key.Matches(msg, m.keymap.RemoveCategory):
		if cur := m.viewer.current; cur != nil {
			return m.startCategoryInput("remove", *cur)
		}
	case key.Matches(msg, m.keymap.OpenURL):
		// Spec 05 §10 / v0.15.x — open the URL picker overlay.
		// Acts on the renderer's extracted URL table for the
		// current message. Empty list still opens the modal so
		// the user gets feedback ("No URLs in this message").
		m.urlPicker.Reset()
		m.mode = URLPickerMode
		return m, nil
	case key.Matches(msg, m.keymap.Yank):
		// Spec 05 §10 / v0.15.x — quick yank from the viewer:
		// if the body has a single URL, copy it; otherwise prompt
		// to disambiguate by opening the picker.
		if len(m.viewer.Links()) == 1 {
			return m.yankURL(m.viewer.Links()[0].URL)
		}
		m.urlPicker.Reset()
		m.mode = URLPickerMode
		m.engineActivity = "y in picker yanks selected URL"
		return m, nil
	case key.Matches(msg, m.keymap.FullscreenBody):
		// Hide folders + list panes so the viewer body uses the
		// full terminal width — terminal-native click-drag for
		// multi-line text selection only works when the cursor's
		// drag region isn't crossed by pane borders.
		m.mode = FullscreenBodyMode
		return m, nil
	case key.Matches(msg, m.keymap.Undo):
		return m.runUndo()
	}
	return m, nil
}

// yankURL copies a URL to the clipboard and paints a status hint.
// Used by the viewer-pane y shortcut when there's only one URL,
// and by the URL picker's y key.
func (m Model) yankURL(url string) (tea.Model, tea.Cmd) {
	label, err := m.yanker.Yank(url)
	if err != nil {
		m.lastError = fmt.Errorf("yank: %w", err)
		return m, nil
	}
	m.lastError = nil
	m.engineActivity = fmt.Sprintf("✓ copied URL (%s)", label)
	return m, nil
}

// Commands

// backfillDoneMsg arrives when a wall-sync Backfill returns. nil
// Err is success (the FolderSyncedEvent that follows refreshes the
// list). Non-nil Err surfaces in the status bar so the user is no
// longer staring at "loading older messages…" indefinitely. Real-
// tenant regression v0.15.0: the previous fire-and-forget goroutine
// silently swallowed every error path.
type backfillDoneMsg struct {
	FolderID string
	Err      error
}

// kickBackfillCmd fires Engine.Backfill on its own goroutine via
// tea.Cmd and routes the outcome through Update so a network /
// auth / throttle failure becomes a visible error instead of a
// stuck activity message.
func (m Model) kickBackfillCmd(folderID string, until time.Time) tea.Cmd {
	if m.deps.Engine == nil {
		return nil
	}
	engine := m.deps.Engine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := engine.Backfill(ctx, folderID, until)
		return backfillDoneMsg{FolderID: folderID, Err: err}
	}
}

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
	limit := m.list.LoadLimit()
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
			Limit:     limit,
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
		// If the engine just confirmed there's no more older mail
		// in the user's current folder, mark the list as graph-
		// exhausted so further j-presses at the cache wall stop
		// re-firing no-op Backfills. Real-tenant regression
		// v0.14.x: without this, the list silently froze at the
		// last cached row.
		if e.FolderID == m.list.FolderID && e.Added == 0 && m.list.AtCacheWall() {
			m.list.MarkGraphExhausted()
			m.engineActivity = "no older messages on the server"
		}
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
	if m.mode == CalendarMode {
		return m.calendar.View(m.theme, m.width, m.height)
	}
	if m.mode == OOFMode {
		return m.oof.View(m.theme, m.width, m.height)
	}
	if m.mode == ComposeConfirmMode {
		return m.renderComposeConfirm()
	}
	if m.mode == HelpMode {
		return m.help.View(m.theme, m.keymap, m.width, m.height)
	}
	if m.mode == URLPickerMode {
		return m.urlPicker.View(m.theme, m.viewer.Links(), m.width, m.height)
	}
	if m.mode == FullscreenBodyMode {
		// Render the viewer at full terminal width with no
		// surrounding pane chrome so terminal selection drag works
		// end-to-end. Reserves the bottom row for a hint line.
		body := m.viewer.View(m.theme, m.width, m.height-1, true)
		hint := m.theme.Dim.Render("z / Esc / q  exit fullscreen  ·  drag to select  ·  y  yank URL")
		return body + "\n" + hint
	}
	if m.mode == FolderNameInputMode {
		var title string
		switch m.pendingFolderAction {
		case "new":
			if m.pendingFolderParent == "" {
				title = "New folder (top-level)"
			} else {
				title = "New child folder"
			}
		case "rename":
			title = "Rename folder"
		default:
			title = "Folder name"
		}
		body := title + "\n\n" + m.theme.HelpKey.Render("name:") + " " + m.folderNameBuf + "▎\n\n" +
			m.theme.Dim.Render("Enter to apply  ·  Esc to cancel")
		box := m.theme.Modal.Render(body)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	if m.mode == CategoryInputMode {
		verb := m.pendingCategoryAction
		if verb == "" {
			verb = "set"
		}
		title := strings.Title(verb) + " category"
		body := title + "\n\n" + m.theme.HelpKey.Render(verb+":") + " " + m.categoryBuf + "▎\n\n" +
			m.theme.Dim.Render("Enter to apply  ·  Esc to cancel")
		box := m.theme.Modal.Render(body)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	statusBar := m.status.View(m.theme, m.width, StatusInputs{
		LastSync:  m.lastSyncAt,
		Throttled: m.throttledFor,
		Activity:  m.engineActivity,
		LastErr:   m.lastError,
	})
	cmdBar := m.cmd.View(m.theme, m.width, m.mode == CommandMode)
	if m.mode == SearchMode {
		cmdBar = m.theme.CommandBar.Render("/" + m.searchBuf)
	} else if m.searchActive {
		cmdBar = m.theme.Dim.Render("search: " + m.searchQuery + "  (esc to clear)")
	} else if m.filterActive {
		hint := fmt.Sprintf("filter: %s · matched %d · ;d delete · ;a archive · :unfilter", m.filterPattern, len(m.filterIDs))
		if m.bulkPending {
			hint = "bulk: press d (delete) or a (archive) — esc to cancel"
		}
		cmdBar = m.theme.CommandBar.Render(hint)
	}
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
	// Pane-switch hints come first so the user always knows how to
	// reach the OTHER panes from wherever they are. Then pane-local
	// actions, then global meta (search, filter), then quit.
	switch focused {
	case FoldersPane:
		hints = [][2]string{{"1/2/3", "panes"}, {"j/k", "nav"}, {"o", "expand"}, {"⏎", "open"}, {"/", "search"}, {"q", "quit"}}
	case ListPane:
		hints = [][2]string{{"1/2/3", "panes"}, {"j/k", "nav"}, {"⏎", "open"}, {"/", "search"}, {":filter", "narrow"}, {"f", "flag"}, {"d", "delete"}, {"a", "archive"}, {"q", "quit"}}
	case ViewerPane:
		hints = [][2]string{{"1/2/3", "panes"}, {"h", "back"}, {"j/k", "scroll"}, {"H", "headers"}, {"r", "reply"}, {"f", "flag"}, {"a", "archive"}, {"d", "delete"}, {"q", "quit"}}
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
