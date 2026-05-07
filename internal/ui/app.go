package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
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
	// Done returns a channel closed when the engine stops. Used by
	// consumeSyncEventsCmd to avoid a goroutine leak on app shutdown.
	Done() <-chan struct{}
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
	// Move dispatches a user-folder move (spec 07 §6.5). destAlias is
	// optional — supply the well-known name for transactional folders
	// (the dispatch path prefers it because Graph accepts aliases
	// without tenant-specific IDs); pass empty for arbitrary user
	// folders.
	Move(ctx context.Context, accountID int64, messageID, destFolderID, destAlias string) error
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
	// Returns the typed ErrUndoEmpty when the stack is empty.
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

// ErrUndoEmpty is the sentinel error returned when the undo stack
// is empty. (Pre-rename: `UndoEmpty` — staticcheck ST1012.)
var ErrUndoEmpty = errors.New("undo: stack empty")

// titleCase upper-cases the first ASCII letter of s and lowers
// the rest of the leading word. Replaces `strings.Title` (deprecated
// since Go 1.18 — staticcheck SA1019). Inkwell's call sites pass
// short verb words like "delete", "archive" — pure-ASCII single-
// word inputs — so the simpler implementation is correct without
// reaching for `golang.org/x/text/cases`. If a future caller needs
// proper Unicode word boundaries, lift to that package.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	first := s[0]
	if first >= 'a' && first <= 'z' {
		first -= 'a' - 'A'
	}
	rest := s[1:]
	// Lowercase the trailing characters so "DELETE" → "Delete"
	// rather than the surprising "DELETE" pass-through. Pure
	// ASCII per the doc above.
	low := make([]byte, len(rest))
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		low[i] = c
	}
	return string(first) + string(low)
}

// BulkResult mirrors action.BatchResult — defined here so the UI
// doesn't import internal/action's full type surface.
type BulkResult struct {
	MessageID string
	Err       error
}

// ThreadExecutor handles conversation-level batch operations (spec 20).
// Verb must be a store.ActionType — use store.ActionMarkRead etc.
// Implementations route through action.Executor.ThreadExecute / ThreadMove.
type ThreadExecutor interface {
	ThreadExecute(ctx context.Context, accID int64, verb store.ActionType, focusedMsgID string) (int, []BulkResult, error)
	ThreadMove(ctx context.Context, accID int64, focusedMsgID, destFolderID, destAlias string) (int, []BulkResult, error)
}

// BulkExecutor handles "apply this action to N messages" operations
// (spec 09 / 10). Implementations route through Graph $batch.
type BulkExecutor interface {
	BulkSoftDelete(ctx context.Context, accountID int64, messageIDs []string) ([]BulkResult, error)
	BulkArchive(ctx context.Context, accountID int64, messageIDs []string) ([]BulkResult, error)
	BulkMarkRead(ctx context.Context, accountID int64, messageIDs []string) ([]BulkResult, error)
	BulkMarkUnread(ctx context.Context, accountID int64, messageIDs []string) ([]BulkResult, error)
	BulkFlag(ctx context.Context, accountID int64, messageIDs []string) ([]BulkResult, error)
	BulkUnflag(ctx context.Context, accountID int64, messageIDs []string) ([]BulkResult, error)
	BulkPermanentDelete(ctx context.Context, accountID int64, messageIDs []string) ([]BulkResult, error)
	BulkAddCategory(ctx context.Context, accountID int64, messageIDs []string, category string) ([]BulkResult, error)
	BulkRemoveCategory(ctx context.Context, accountID int64, messageIDs []string, category string) ([]BulkResult, error)
	BulkMove(ctx context.Context, accountID int64, messageIDs []string, destFolderID, destAlias string) ([]BulkResult, error)
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
	// Thread executes conversation-level operations (spec 20). Optional —
	// when nil, the T chord is a no-op.
	Thread ThreadExecutor
	// ThemeName is the [ui] theme key from config. Empty falls back to
	// "default". Unknown values fall back with a logged warning.
	ThemeName string
	// SavedSearches is the initial list of saved searches. Loaded from
	// the DB by cmd_run.go (spec 11); falls back to [[saved_searches]]
	// TOML config entries when the Manager is not wired.
	SavedSearches []SavedSearch
	// SavedSearchSvc enables CRUD and count refresh (spec 11). Optional —
	// when nil, `:rule` commands show a friendly "not wired" error.
	SavedSearchSvc SavedSearchService
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
	// RecentFoldersCount caps the move-picker MRU list (spec 07
	// §12.1). 0 disables the recent section. Falls back to 5 when
	// unset.
	RecentFoldersCount int
	// URLDisplayMaxWidth threads `[rendering].url_display_max_width`
	// to the renderer. Long URLs in the viewer body display-truncate
	// at N cells; the OSC 8 url portion stays full. 0 disables
	// truncation. Falls back to 60 when unset.
	URLDisplayMaxWidth int
	// WrapColumns overrides the computed viewer-pane width for body
	// soft-wrapping. 0 means use the actual pane width.
	WrapColumns int
	// Search runs spec 06 hybrid search (local FTS5 + Graph
	// $search). Optional — when nil, `/` and `:search` fall back
	// to the legacy single-shot store.Search flow.
	Search SearchService
	// Attachments downloads raw attachment bytes (spec 05 §8.1 /
	// PR 10). Optional — when nil, a-z / Shift+A-Z keybindings
	// show a friendly "not wired" message.
	Attachments AttachmentFetcher
	// AttachmentSaveDir is the expanded save path for a-z
	// keybindings. Falls back to ~/Downloads when empty.
	AttachmentSaveDir string
	// LargeAttachmentWarnMB triggers a confirm modal before
	// downloading files larger than this many MB. 0 disables.
	LargeAttachmentWarnMB int

	// UnreadIndicator / FlagIndicator / AttachmentIndicator / MuteIndicator
	// override the default theme glyphs for their respective row decorations.
	// Empty strings leave the theme default in place.
	UnreadIndicator     string
	FlagIndicator       string
	AttachmentIndicator string
	MuteIndicator       string
	// Stream indicators (spec 23 §5.4 / §5.5). Empty fields leave the
	// theme default in place; when StreamASCIIFallback is true, the
	// four stream indicators are forced to single ASCII letters
	// regardless of these strings.
	ImboxIndicator      string
	FeedIndicator       string
	PaperTrailIndicator string
	ScreenerIndicator   string
	StreamASCIIFallback bool
	// ShowRoutingIndicator controls the per-row routing glyph in
	// regular folder views (spec 23 §5.5). Default false.
	ShowRoutingIndicator bool
	// TransientStatusTTL controls how long engineActivity messages
	// persist before auto-clearing. 0 disables auto-clear.
	TransientStatusTTL time.Duration
	// MinTerminalCols / MinTerminalRows trigger the "terminal too
	// small" overlay when the window is narrower/shorter than the
	// minimums. 0 disables each check.
	MinTerminalCols int
	MinTerminalRows int
	// OOOIndicator is the glyph shown in the status bar when OOO is
	// active. From [mailbox_settings].ooo_indicator config.
	OOOIndicator string
	// MailboxRefreshInterval is how long to wait between automatic
	// mailbox-settings re-fetches. From [mailbox_settings].refresh_interval.
	MailboxRefreshInterval time.Duration
	// AttachmentMaxSizeMB is the per-file size limit for staged
	// attachments in the compose pane. 0 disables the check.
	AttachmentMaxSizeMB int
	// MaxAttachments caps the number of staged attachments per draft.
	// 0 disables the check.
	MaxAttachments int

	// DraftWebLinkTTL controls how long the "press s to open in Outlook"
	// status-bar hint lives after a successful draft save. 0 disables
	// auto-clear. From [compose].web_link_ttl (default 30s).
	DraftWebLinkTTL time.Duration
	// CalendarTZ is the effective timezone for calendar time display.
	// Resolved from [calendar].time_zone + mailbox settings. Nil falls
	// back to time.Local.
	CalendarTZ *time.Location
	// CalendarSidebarDays controls how many days the sidebar calendar
	// section shows (spec 12). From [calendar].sidebar_show_days.
	// 0 or negative falls back to 1.
	CalendarSidebarDays int
	// SavedSearchBgRefresh is the interval for the background count refresh
	// timer (spec 11 §6.2). Zero disables the timer.
	SavedSearchBgRefresh time.Duration
	// SavedSearchSuggestAfterN is how many times a pattern must be used as
	// a :filter before a "save this search" hint appears. 0 disables.
	SavedSearchSuggestAfterN int
}

// SearchSnapshot is one progressive emission from a streaming
// SearchService.Search call. Spec 06 §3 — the slice is the FULL
// current merged result set, not an incremental delta, so the UI
// can replace its view directly.
type SearchSnapshot struct {
	// Messages is the merged result list, sorted per spec 06
	// §4.3 (received_at DESC; SourceBoth ahead of single-source
	// ties).
	Messages []store.Message
	// Status is the user-facing hint surfaced on the search line:
	// "[searching local]" / "[📡 searching server…]" /
	// "[merged: N local, M server]" / "[local only — offline]".
	Status string
}

// SearchService is the streaming search surface the UI consumes
// (spec 06). Implementations route through internal/search; the
// UI's own type stays narrow so test stubs are trivial. Search
// returns a channel of progressive SearchSnapshots; the channel
// closes when both branches finish or when cancel() is called.
// Cancel is idempotent.
//
// folderID scopes the search to a single folder. Empty means
// cross-folder (spec 06 §5.3 `--all` mode).
type SearchService interface {
	Search(ctx context.Context, query, folderID string, sortByRelevance bool) (<-chan SearchSnapshot, func())
}

// AttachmentFetcher downloads raw attachment bytes on demand (spec 05
// §8.1 / PR 10). Optional — when nil, a-z keybindings surface a
// friendly "not wired" error. Implementations call Graph's
// GET /me/messages/{id}/attachments/{id}?$select=contentBytes.
type AttachmentFetcher interface {
	GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error)
}

// SavedSearch is a named pattern that surfaces in the sidebar. Defined
// at the consumer site so the UI doesn't import internal/savedsearch.
type SavedSearch struct {
	ID      int64
	Name    string
	Pattern string
	Pinned  bool
	Count   int // -1 = not yet evaluated; ≥0 = match count from last refresh
}

// SavedSearchService is the CRUD + count-refresh surface the UI consumes.
// Defined here at the consumer site (CLAUDE.md §2 layering).
type SavedSearchService interface {
	// Save creates or updates a saved search by name. Pattern is validated.
	Save(ctx context.Context, name, pattern string, pinned bool) error
	// DeleteByName removes by name. Returns an error if not found.
	DeleteByName(ctx context.Context, name string) error
	// Reload returns the full list with current metadata (no evaluation).
	Reload(ctx context.Context) ([]SavedSearch, error)
	// RefreshCounts evaluates all pinned searches and returns the full
	// list with updated Count fields. Errors per-search are silently skipped.
	RefreshCounts(ctx context.Context) ([]SavedSearch, error)
	// Edit updates an existing saved search atomically. If newName ≠ originalName,
	// the old entry is deleted and a new one with the new name is created.
	Edit(ctx context.Context, originalName, newName, pattern string, pinned bool) error
	// EvaluatePattern compiles and runs patternSrc against the local store,
	// returning the match count. Used by the edit modal's ctrl+t test key.
	EvaluatePattern(ctx context.Context, patternSrc string) (int, error)
}

// CalendarFetcher is the read-only calendar surface the UI consumes
// (spec 12). Defined here so the UI doesn't import internal/graph's
// full surface.
type CalendarFetcher interface {
	ListEventsToday(ctx context.Context) ([]CalendarEvent, error)
	// ListEventsBetween returns events in the half-open [start, end)
	// window. Used by day navigation (]/[/{/} keys) to fetch the
	// right day's events without a full re-fetch. Spec 12 §6.2.
	ListEventsBetween(ctx context.Context, start, end time.Time) ([]CalendarEvent, error)
	// GetEvent fetches the full detail for an event id (spec 12 §4.3
	// / §7) — used by the detail modal opened from `Enter` on the
	// calendar list. Returns attendees and the body preview that the
	// list view doesn't carry.
	GetEvent(ctx context.Context, id string) (CalendarEventDetail, error)
}

// MailboxSettings is the subset of mailbox settings the UI renders
// for the :ooo flow (spec 13). Defined at the consumer site so the
// UI doesn't import internal/graph.
type MailboxSettings struct {
	AutoReplyStatus      string // "disabled" | "alwaysEnabled" | "scheduled"
	InternalReplyMessage string
	ExternalReplyMessage string
	ExternalAudience     string
	ScheduledStart       *time.Time
	ScheduledEnd         *time.Time
	TimeZone             string
	Language             string
	WorkingHoursDisplay  string
	DateFormat           string
	TimeFormat           string
}

// MailboxClient handles GET + PATCH against /me/mailboxSettings for
// the out-of-office flow (spec 13).
type MailboxClient interface {
	Get(ctx context.Context) (*MailboxSettings, error)
	SetAutoReply(ctx context.Context, s MailboxSettings) error
}

// DraftAttachmentRef is the UI-layer view of an attachment staged for
// upload. Mirrors action.AttachmentRef; defined here so the UI
// doesn't import internal/action (CLAUDE.md §2 layering).
type DraftAttachmentRef struct {
	LocalPath string
	Name      string
	SizeBytes int64
}

// DraftCreator is the surface the UI consumes for spec 15
// (compose / reply). Defined here so the UI doesn't import
// internal/action's full type set.
//
// Reply / ReplyAll / Forward share the (ctx, accountID,
// sourceMessageID, body, to, cc, bcc, subject, attachments) signature
// so the in-modal compose pane can route by Kind without per-method
// argument plumbing. NewDraft drops sourceMessageID — POST
// /me/messages doesn't reference a source.
type DraftCreator interface {
	CreateDraftReply(ctx context.Context, accountID int64, sourceMessageID, body string, to, cc, bcc []string, subject string, attachments []DraftAttachmentRef) (*DraftRef, error)
	CreateDraftReplyAll(ctx context.Context, accountID int64, sourceMessageID, body string, to, cc, bcc []string, subject string, attachments []DraftAttachmentRef) (*DraftRef, error)
	CreateDraftForward(ctx context.Context, accountID int64, sourceMessageID, body string, to, cc, bcc []string, subject string, attachments []DraftAttachmentRef) (*DraftRef, error)
	CreateNewDraft(ctx context.Context, accountID int64, body string, to, cc, bcc []string, subject string, attachments []DraftAttachmentRef) (*DraftRef, error)
	// DiscardDraft deletes a server-side draft (spec 15 §6.3 / F-1).
	// Called when the user presses 'D' after the draft was saved.
	// Idempotent: 404 is treated as success.
	DiscardDraft(ctx context.Context, accountID int64, draftID string) error
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
// times are UTC. ID + WebLink are required by the detail-modal
// dispatch (Enter routes through GetEvent(id); `o` opens WebLink).
type CalendarEvent struct {
	ID               string
	Subject          string
	OrganizerName    string
	OrganizerAddress string
	Start            time.Time
	End              time.Time
	IsAllDay         bool
	Location         string
	OnlineMeetingURL string
	// ResponseStatus is the user's own response: "accepted" |
	// "tentativelyAccepted" | "declined" | "notResponded" | "none" | "organizer".
	// Used by the adapter to filter declined events per calendar.show_declined.
	ResponseStatus string
	WebLink        string
}

// CalendarEventDetail is the spec 12 §7 detail-modal payload —
// CalendarEvent plus the attendee list and body preview that the
// list view doesn't carry.
type CalendarEventDetail struct {
	CalendarEvent
	BodyPreview string
	Attendees   []CalendarAttendee
}

// CalendarAttendee mirrors graph.EventAttendee at the consumer site
// so the UI doesn't import internal/graph (CLAUDE.md §2).
type CalendarAttendee struct {
	Name    string
	Address string
	Type    string // "required" | "optional" | "resource"
	Status  string // "accepted" | "declined" | "tentativelyAccepted" | "notResponded" | "organizer" | "none"
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

	folders         FoldersModel
	foldersByID     map[string]store.Folder // populated in FoldersLoadedMsg; used for cross-folder filter display
	list            ListModel
	viewer          ViewerModel
	cmd             CommandModel
	status          StatusModel
	signin          SignInModel
	confirm         ConfirmModel
	calendar        CalendarModel
	oof             OOFModel
	oofReturnMode   Mode // mode to restore when OOF modal saves or cancels
	settingsView    SettingsModel
	help            HelpModel
	urlPicker       URLPickerModel
	folderPicker    FolderPickerModel
	palette         PaletteModel
	calendarDetail  CalendarDetailModel
	compose         ComposeModel
	attachPickInput textinput.Model
	yanker          *yanker

	// mailboxSettings is the cached copy used for OOO status bar indicator.
	mailboxSettings *MailboxSettings

	focused Pane
	mode    Mode
	// calDetailPriorMode is the mode active before entering
	// CalendarDetailMode; Esc returns here instead of always going to
	// CalendarMode (which would be blank when the user opened the detail
	// directly from the folders pane rather than through :cal).
	calDetailPriorMode Mode

	keymap KeyMap
	theme  Theme

	// transient state shown by the status bar
	throttledFor   time.Duration
	lastSyncAt     time.Time
	lastError      error
	engineActivity string // "syncing folders…" / "syncing…" / ""

	// Search-mode buffer + last-committed query. The list pane renders
	// search results in place of folder messages when searchActive.
	searchBuf         string
	searchActive      bool
	searchQuery       string // committed query (the one that produced m.list contents)
	searchFolderID    string // folder scope: empty = all-folders (--all), non-empty = scoped
	searchSortByRelev bool   // true when --sort=relevance prefix was present
	priorFolderID     string // folder to restore when search is cleared
	// searchStatus mirrors the spec 06 §5.1 streaming hint —
	// "[searching local]", "[📡 searching server…]", "[merged: N
	// local, M server]", or "[local only — offline]". Empty
	// outside SearchMode / searchActive.
	searchStatus string
	// searchCancel is the cancel hook for the most-recent
	// streaming Searcher run; calling it terminates both branches
	// when the user dispatches a new query or exits search mode.
	searchCancel func()
	// searchUpdates is the live channel of progressive
	// SearchSnapshots from the in-flight streaming search. The
	// SearchUpdateMsg handler re-arms consumeSearchUpdatesCmd
	// against this channel after each snapshot until Done lands
	// and the channel closes.
	searchUpdates <-chan SearchSnapshot

	// Filter mode (spec 10): :filter <pattern> compiles via spec 08
	// and narrows the list pane to matches. ; prefix + a triage key
	// applies that action to all matched messages via BulkExecutor.
	filterActive      bool
	filterPattern     string
	filterIDs         []string // matched message IDs (for bulk apply)
	filterAllFolders  bool     // set when --all / -a prefix used in :filter
	filterFolderCount int      // distinct folders in current filter result (when filterAllFolders)
	filterFolderName  string   // display name when filterFolderCount == 1
	bulkPending       bool     // true after `;` is pressed; next d/a fires bulk
	pendingBulkMove   bool     // true while FolderPickerMode is active for ;m bulk-move
	pendingBulk       string   // action key while in ConfirmMode (bulk ops)
	// pendingBulkCategory holds the category name for ;c / ;C bulk ops
	// while ConfirmMode or CategoryInputMode is active. Cleared after dispatch.
	pendingBulkCategory       string
	pendingBulkCategoryAction string // "add_category" | "remove_category" while CategoryInputMode is bulk

	// savedSearches is the live sidebar list (spec 11). Updated after
	// every CRUD operation and on background count refresh.
	savedSearches []SavedSearch
	// pendingRuleDelete holds the name of the saved search being deleted
	// while the confirm modal is open.
	pendingRuleDelete string

	// ruleEdit* holds state for the spec 11 B-2 edit modal.
	ruleEditOrigName string // original name before edits
	ruleEditName     string // current name buffer
	ruleEditPattern  string // current pattern buffer
	ruleEditPinned   bool   // current pinned toggle
	ruleEditField    int    // 0=name 1=pattern 2=pinned
	ruleEditTestMsg  string // last ctrl+t test result
	ruleEditTesting  bool   // true while test cmd in flight

	// filterSuggestCounts tracks how many times each pattern has been run
	// as :filter in this session (spec 11 §5.4 auto-suggest).
	filterSuggestCounts map[string]int
	// filterSuggestedFor is the set of patterns we have already hinted
	// about in this session (so we suggest only once per pattern).
	filterSuggestedFor map[string]bool

	// Compose / reply (spec 15). Tracks the most-recently-saved draft
	// so the viewer-pane `s` shortcut can open it in Outlook and `D`
	// can discard it. Both fields are cleared together by the TTL timer
	// (draftWebLinkExpiredMsg) and by draftDiscardDoneMsg.
	lastDraftWebLink string
	lastDraftID      string
	// pendingDiscardDraftID holds the draft ID while the confirm modal
	// is open for the "discard draft" flow. Cleared after confirm/cancel.
	pendingDiscardDraftID string

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

	// Attachment save/open (spec 05 §8 / §12 / PR 10).
	// pendingAttachmentSave/Open hold the targeted attachment while the
	// large-file confirm modal is open; y fires the download cmd.
	pendingAttachmentSave *store.Attachment
	pendingAttachmentOpen *store.Attachment
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

	// Move-with-folder-picker (spec 07 §6.5 / §12.1). pendingMoveMsg
	// holds the focused message while FolderPickerMode is active;
	// recentFolderIDs is the session-scoped MRU list (most-recently-
	// moved-to first), capped at deps.RecentFoldersCount.
	pendingMoveMsg  *store.Message
	recentFolderIDs []string

	// Compose-session resume (spec 15 §7 / PR 7-ii). Set when the
	// launch-time scan finds an unconfirmed compose session in the
	// store; cleared after the user answers the resume modal
	// (Confirm=true → restore + open ComposeMode; Confirm=false →
	// confirm the session so it never resurfaces).
	pendingComposeResume *store.ComposeSession

	// Thread chord (spec 20). threadChordPending is true while waiting
	// for the second keypress of a T<verb> chord. threadChordToken is
	// incremented on each new chord to detect stale timeout messages.
	threadChordPending bool
	threadChordToken   uint64
	// Stream chord (spec 23). Symmetric with thread chord — pending
	// while waiting for the second keypress of an S<dest> chord.
	streamChordPending bool
	streamChordToken   uint64
	// pendingThreadMove is true while FolderPickerMode is active for T m.
	pendingThreadMove bool
	// pendingThreadIDs stores pre-fetched message IDs for T d / T D
	// confirm flow, avoiding a double-fetch race.
	pendingThreadIDs []string
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
	if deps.UnreadIndicator != "" {
		theme.UnreadIndicator = deps.UnreadIndicator
	}
	if deps.FlagIndicator != "" {
		theme.FlagIndicator = deps.FlagIndicator
	}
	if deps.AttachmentIndicator != "" {
		theme.AttachmentIndicator = deps.AttachmentIndicator
	}
	if deps.MuteIndicator != "" {
		theme.MuteIndicator = deps.MuteIndicator
	}
	if deps.StreamASCIIFallback {
		theme.ImboxIndicator = "i"
		theme.FeedIndicator = "f"
		theme.PaperTrailIndicator = "p"
		theme.ScreenerIndicator = "k"
	} else {
		if deps.ImboxIndicator != "" {
			theme.ImboxIndicator = deps.ImboxIndicator
		}
		if deps.FeedIndicator != "" {
			theme.FeedIndicator = deps.FeedIndicator
		}
		if deps.PaperTrailIndicator != "" {
			theme.PaperTrailIndicator = deps.PaperTrailIndicator
		}
		if deps.ScreenerIndicator != "" {
			theme.ScreenerIndicator = deps.ScreenerIndicator
		}
	}
	folders := NewFolders()
	savedSearches := append([]SavedSearch(nil), deps.SavedSearches...)
	if len(savedSearches) > 0 {
		folders.SetSavedSearches(savedSearches)
	}
	keymap, err := ApplyBindingOverrides(DefaultKeyMap(), deps.Bindings)
	if err != nil {
		return Model{}, fmt.Errorf("ui: %w", err)
	}
	return Model{
		deps:                deps,
		paneWidths:          DefaultPaneWidths(),
		focused:             ListPane,
		mode:                NormalMode,
		keymap:              keymap,
		theme:               theme,
		folders:             folders,
		savedSearches:       savedSearches,
		filterSuggestCounts: make(map[string]int),
		filterSuggestedFor:  make(map[string]bool),
		list:                NewList(),
		viewer:              NewViewer(),
		cmd:                 NewCommand(),
		status:              NewStatus(upn, tenant),
		signin:              NewSignIn(),
		confirm:             NewConfirm(),
		calendar:            NewCalendar(),
		oof:                 NewOOF(),
		help:                NewHelp(),
		urlPicker:           NewURLPicker(),
		folderPicker:        NewFolderPicker(),
		palette:             NewPalette(),
		calendarDetail:      NewCalendarDetail(),
		compose:             NewCompose(),
		attachPickInput:     newAttachPickInput(),
		yanker:              newYanker(stdoutOSC52Writer),
	}, nil
}

// newAttachPickInput builds the textinput used by the attachment picker
// overlay. Initialised once in New(); reset on each Ctrl+A activation.
func newAttachPickInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "~/Downloads/report.pdf"
	return ti
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
//
// Spec 15 §7 / PR 7-ii: also runs the compose-session resume scan
// (with a GC pass for confirmed sessions older than 24h folded
// into the same Cmd). When the scan finds an unconfirmed row, the
// Update handler surfaces a confirm modal asking the user whether
// to resume editing or discard.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.loadFoldersCmd(),
		m.consumeSyncEventsCmd(),
		m.scanComposeSessionsCmd(),
	}
	if refreshCmd := m.refreshSavedSearchCountsCmd(); refreshCmd != nil {
		cmds = append(cmds, refreshCmd)
	}
	if m.deps.SavedSearchBgRefresh > 0 && m.deps.SavedSearchSvc != nil {
		cmds = append(cmds, m.backgroundRefreshTimerCmd())
	}
	if m.deps.Mailbox != nil {
		cmds = append(cmds, m.doMailboxRefreshCmd())
	}
	if sidebarCmd := m.calendarSidebarCmd(); sidebarCmd != nil {
		cmds = append(cmds, sidebarCmd)
	}
	return tea.Batch(cmds...)
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

	case composeResumeMsg:
		// Spec 15 §7 / PR 7-ii: launch-time scan found an unconfirmed
		// compose session. Surface a confirm modal that asks whether
		// to resume editing or discard. Errors are logged and skipped
		// — a corrupt scan shouldn't block startup.
		if msg.Err != nil {
			if m.deps.Logger != nil {
				m.deps.Logger.Warn("compose: resume scan failed", "err", msg.Err.Error())
			}
			return m, nil
		}
		sess := msg.Session
		m.pendingComposeResume = &sess
		prompt := composeResumePrompt(sess)
		m.confirm = m.confirm.Ask(prompt, "compose_resume")
		m.mode = ConfirmMode
		return m, nil

	case composeResumeNoneMsg:
		// Nothing to resume; this exists so tests can assert the
		// scan-Cmd ran end-to-end on Init.
		return m, nil

	case composeEditorDoneMsg:
		if msg.err != nil {
			m.lastError = msg.err
			return m, nil
		}
		content, err := os.ReadFile(msg.tempPath) // #nosec G304 — path is an inkwell-generated tempfile from compose.WriteTempfile
		compose.CleanupTempfile(msg.tempPath)
		if err != nil {
			m.lastError = fmt.Errorf("compose: read tempfile: %w", err)
			return m, nil
		}
		m.compose.SetBody(string(content))
		return m, nil

	case clearTransientMsg:
		m.engineActivity = ""
		return m, nil

	case FoldersLoadedMsg:
		m.folders.SetFolders(msg.Folders)
		m.foldersByID = make(map[string]store.Folder, len(msg.Folders))
		for _, f := range msg.Folders {
			m.foldersByID[f.ID] = f
		}
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
			return m, tea.Batch(m.loadMessagesCmd(pick.ID), m.refreshMutedCountCmd(), m.refreshStreamCountsCmd())
		}
		return m, tea.Batch(m.refreshMutedCountCmd(), m.refreshStreamCountsCmd())

	case MessagesLoadedMsg:
		if msg.FolderID == m.list.FolderID {
			m.list.SetMessages(msg.Messages)
		}
		return m, nil

	case searchStreamMsg:
		// First snapshot from a streaming search. Stash cancel +
		// channel on the Model so Esc / new query can stop the
		// in-flight branches AND so SearchUpdateMsg can re-arm
		// the channel drain. Apply the snapshot and dispatch the
		// consumer Cmd.
		if !m.searchActive || m.searchQuery != msg.query {
			// User moved on before the first snapshot landed —
			// drop the in-flight stream cleanly.
			msg.cancel()
			return m, nil
		}
		m.searchCancel = msg.cancel
		m.searchUpdates = msg.ch
		m.list.SetMessages(msg.snapshot.Messages)
		m.searchStatus = msg.snapshot.Status
		return m, m.consumeSearchUpdatesCmd(msg.query, msg.ch)

	case SearchUpdateMsg:
		// Continuation snapshot OR Done sentinel.
		if !m.searchActive || m.searchQuery != msg.Query {
			return m, nil
		}
		if msg.Done {
			m.searchCancel = nil
			m.searchUpdates = nil
			if msg.Status != "" {
				m.searchStatus = msg.Status
			}
			return m, nil
		}
		m.list.SetMessages(msg.Results)
		m.searchStatus = msg.Status
		// Re-arm the channel drain so the next snapshot lands.
		if m.searchUpdates != nil {
			return m, m.consumeSearchUpdatesCmd(msg.Query, m.searchUpdates)
		}
		return m, nil

	case BodyRenderedMsg:
		if m.viewer.CurrentMessageID() == msg.MessageID {
			m.viewer.SetBody(msg.Text, msg.TextExpanded, msg.State)
			m.viewer.SetLinks(msg.Links)
			m.viewer.SetAttachments(msg.Attachments)
			m.viewer.SetConversationThread(msg.Conversation, msg.MessageID)
			m.viewer.SetRawHeaders(msg.RawHeaders)
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

	case SaveAttachmentDoneMsg:
		if msg.Err != nil {
			m.lastError = fmt.Errorf("save %s: %w", msg.Name, msg.Err)
		} else {
			m.engineActivity = fmt.Sprintf("saved → %s", msg.Path)
		}
		return m, nil

	case OpenAttachmentDoneMsg:
		if msg.Err != nil {
			m.lastError = fmt.Errorf("open %s: %w", msg.Name, msg.Err)
		} else {
			m.engineActivity = fmt.Sprintf("opened %s", msg.Name)
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

	case calendarSidebarLoadedMsg:
		tz := m.deps.CalendarTZ
		if tz == nil {
			tz = time.Local
		}
		days := m.deps.CalendarSidebarDays
		if days < 1 {
			days = 1
		}
		m.folders.SetCalendarEvents(msg.events, days, tz)
		return m, nil

	case calendarFetchedMsg:
		if msg.Err != nil {
			m.calendar.SetError(msg.Err)
		} else {
			m.calendar.SetEvents(msg.Events)
		}
		return m, nil

	case eventFetchedMsg:
		if msg.Err != nil {
			m.calendarDetail.SetError(msg.Err)
		} else {
			m.calendarDetail.SetDetail(msg.Detail)
		}
		return m, nil

	case oofLoadedMsg:
		if msg.Err != nil {
			m.oof.SetError(msg.Err)
			m.settingsView.SetError(msg.Err)
		} else {
			m.oof.SetSettings(msg.Settings)
			m.settingsView.SetSettings(msg.Settings)
			m.mailboxSettings = msg.Settings
		}
		return m, nil

	case oofToggledMsg:
		if msg.Err != nil {
			m.oof.SetError(msg.Err)
		} else {
			m.oof.SetSettings(msg.Settings)
			m.settingsView.SetSettings(msg.Settings)
			m.mailboxSettings = msg.Settings
			m.mode = m.oofReturnMode
			m.oofReturnMode = NormalMode
			m.oof.Reset()
			m.engineActivity = "✓ Out-of-office updated"
			return m, m.clearTransientCmd()
		}
		return m, nil

	case mailboxRefreshedMsg:
		if msg.Err == nil && msg.Settings != nil {
			m.mailboxSettings = msg.Settings
		}
		var cmd tea.Cmd
		if m.deps.Mailbox != nil {
			cmd = m.mailboxAutoRefreshCmd(m.deps.MailboxRefreshInterval)
		}
		return m, cmd

	case mailboxTickMsg:
		if m.deps.Mailbox == nil {
			return m, nil
		}
		return m, m.doMailboxRefreshCmd()

	case draftSavedMsg:
		m.engineActivity = ""
		if msg.err != nil {
			// Spec 15 v2: the in-modal flow validates inputs before
			// dispatch (recipient recovery, etc.), so the only path
			// that reaches here with err set is a Graph round-trip
			// failure. Form state lives in m.compose; the user can
			// retry without losing their work.
			m.lastError = fmt.Errorf("draft: %w", msg.err)
			m.lastDraftWebLink = msg.webLink // may be set on partial-failure (createReply ok, body PATCH failed)
			m.lastDraftID = msg.draftID
			return m, nil
		}
		m.lastError = nil
		m.lastDraftWebLink = msg.webLink
		m.lastDraftID = msg.draftID
		m.engineActivity = "✓ draft saved · s open · D discard"
		return m, clearDraftWebLinkCmd(m.deps.DraftWebLinkTTL)

	case draftWebLinkExpiredMsg:
		m.lastDraftWebLink = ""
		m.lastDraftID = ""
		if m.engineActivity == "✓ draft saved · s open · D discard" {
			m.engineActivity = ""
		}
		return m, nil

	case draftDiscardDoneMsg:
		m.lastDraftWebLink = ""
		m.lastDraftID = ""
		if msg.err != nil {
			m.lastError = fmt.Errorf("discard draft: %w", msg.err)
		} else {
			m.engineActivity = "draft discarded"
		}
		return m, nil

	case ConfirmResultMsg:
		m.mode = NormalMode
		// Bulk-action confirmation: pendingBulk carries the action key
		// set by confirmBulk(). Only fire on Confirm=true; on No, just
		// drop it.
		if m.pendingBulk != "" {
			action := m.pendingBulk
			m.pendingBulk = ""
			m.pendingBulkCategory = "" // always clear after confirm or cancel
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
		// Draft discard confirmation (spec 15 F-1): pendingDiscardDraftID
		// carries the server-side draft ID. y fires DiscardDraft (DELETE);
		// n just clears the pending state.
		if m.pendingDiscardDraftID != "" && msg.Topic == "discard_draft" {
			draftID := m.pendingDiscardDraftID
			m.pendingDiscardDraftID = ""
			if msg.Confirm {
				m.engineActivity = "discarding draft…"
				return m, m.discardSavedDraftCmd(draftID)
			}
			m.engineActivity = "discard cancelled"
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
		// Compose-session resume (spec 15 §7 / PR 7-ii): pendingComposeResume
		// carries the session row. y → restore the form into ComposeMode;
		// n → confirm the session so the resume scan stops offering it.
		if m.pendingComposeResume != nil && msg.Topic == "compose_resume" {
			sess := *m.pendingComposeResume
			m.pendingComposeResume = nil
			if msg.Confirm {
				return m.resumeCompose(sess)
			}
			// Decline: confirm-then-forget. Done inline because there's
			// no other goroutine to hang the write off and the write is
			// sub-ms on local SQLite.
			m.confirmComposeSessionInline(sess.SessionID)
			m.engineActivity = "draft discarded"
			return m, nil
		}
		// Rule-delete confirmation (spec 11): pendingRuleDelete carries the name.
		if m.pendingRuleDelete != "" && msg.Topic == "rule_delete" {
			name := m.pendingRuleDelete
			m.pendingRuleDelete = ""
			if msg.Confirm {
				return m, m.ruleDeleteCmd(name)
			}
			m.engineActivity = "rule delete cancelled"
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
		// Large-attachment save (spec 05 §8.2 / PR 10): pendingAttachmentSave
		// carries the target; y fires the download-to-disk cmd.
		if m.pendingAttachmentSave != nil && msg.Topic == "large_attachment_save" {
			att := *m.pendingAttachmentSave
			m.pendingAttachmentSave = nil
			if msg.Confirm {
				m.engineActivity = fmt.Sprintf("saving %s…", att.Name)
				return m, m.saveAttachmentCmd(att)
			}
			m.engineActivity = "save cancelled"
			return m, nil
		}
		// Large-attachment open (spec 05 §8.2 / PR 10): pendingAttachmentOpen
		// carries the target; y fires the download-to-temp+open cmd.
		if m.pendingAttachmentOpen != nil && msg.Topic == "large_attachment_open" {
			att := *m.pendingAttachmentOpen
			m.pendingAttachmentOpen = nil
			if msg.Confirm {
				m.engineActivity = fmt.Sprintf("opening %s…", att.Name)
				return m, m.openAttachmentCmd(att)
			}
			m.engineActivity = "open cancelled"
			return m, nil
		}
		// Thread soft-delete confirmation (spec 20): pendingThreadIDs holds
		// the pre-fetched message IDs; y fires BulkSoftDelete on them.
		if len(m.pendingThreadIDs) > 0 && msg.Topic == "thread:soft_delete" {
			ids := m.pendingThreadIDs
			m.pendingThreadIDs = nil
			if !msg.Confirm {
				m.engineActivity = "thread delete cancelled"
				return m, nil
			}
			return m, m.runBulkWithIDsCmd("soft_delete", ids)
		}
		// Thread permanent-delete confirmation (spec 20): irreversible.
		if len(m.pendingThreadIDs) > 0 && msg.Topic == "thread:permanent_delete" {
			ids := m.pendingThreadIDs
			m.pendingThreadIDs = nil
			if !msg.Confirm {
				m.engineActivity = "thread permanent delete cancelled"
				return m, nil
			}
			return m, m.runBulkWithIDsCmd("permanent_delete", ids)
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
		// Compute cross-folder metadata when --all was used.
		if m.filterAllFolders {
			seen := make(map[string]struct{}, len(msg.messages))
			for _, mm := range msg.messages {
				seen[mm.FolderID] = struct{}{}
			}
			m.filterFolderCount = len(seen)
			if m.filterFolderCount == 1 {
				for id := range seen {
					if f, ok := m.foldersByID[id]; ok {
						m.filterFolderName = f.DisplayName
					} else {
						m.filterFolderName = "???"
					}
				}
			} else {
				m.filterFolderName = ""
			}
			if m.filterFolderCount > 1 {
				nameMap := make(map[string]string, len(seen))
				for id := range seen {
					if f, ok := m.foldersByID[id]; ok {
						nameMap[id] = f.DisplayName
					} else {
						nameMap[id] = "???"
					}
				}
				m.list.folderNameByID = nameMap
			} else {
				m.list.folderNameByID = nil
			}
		} else {
			m.filterFolderCount = 0
			m.filterFolderName = ""
			m.list.folderNameByID = nil
		}
		// Render the filter results in the list pane via the existing
		// SetMessages path; sentinel folder ID keeps load-more / triage
		// keyed off the current filter, not the underlying folder.
		if !m.searchActive && m.priorFolderID == "" {
			m.priorFolderID = m.list.FolderID
		}
		m.list.FolderID = "filter:" + msg.src
		m.list.SetMessages(msg.messages)
		m.focused = ListPane
		// Auto-suggest: hint after N uses of the same pattern (spec 11 §5.4).
		if m.deps.SavedSearchSuggestAfterN > 0 && msg.src != "" {
			alreadySaved := false
			for _, ss := range m.savedSearches {
				if ss.Pattern == msg.src {
					alreadySaved = true
					break
				}
			}
			if !alreadySaved && !m.filterSuggestedFor[msg.src] {
				m.filterSuggestCounts[msg.src]++
				if m.filterSuggestCounts[msg.src] >= m.deps.SavedSearchSuggestAfterN {
					m.engineActivity = "tip: :rule save <name> to keep this filter"
					m.filterSuggestedFor[msg.src] = true
				}
			}
		}
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

	case savedSearchesUpdatedMsg:
		m.savedSearches = msg.searches
		m.folders.SetSavedSearches(msg.searches)
		return m, nil

	case savedSearchBgRefreshMsg:
		return m, tea.Batch(m.refreshSavedSearchCountsCmd(), m.refreshStreamCountsCmd(), m.backgroundRefreshTimerCmd())

	case ruleEditDoneMsg:
		m.mode = NormalMode
		if msg.err != nil {
			m.lastError = fmt.Errorf("rule edit: %w", msg.err)
			return m, nil
		}
		m.engineActivity = fmt.Sprintf("✓ rule saved %q", msg.newName)
		if len(msg.searches) > 0 {
			m.savedSearches = msg.searches
			m.folders.SetSavedSearches(msg.searches)
		}
		return m, m.clearTransientCmd()

	case ruleEditTestDoneMsg:
		m.ruleEditTesting = false
		if msg.err != nil {
			m.ruleEditTestMsg = "⚠ " + msg.err.Error()
		} else {
			m.ruleEditTestMsg = fmt.Sprintf("✓ matches %d messages", msg.count)
		}
		return m, nil

	case savedSearchSavedMsg:
		if msg.err != nil {
			m.lastError = fmt.Errorf("rule %s: %w", msg.action, msg.err)
			return m, nil
		}
		m.lastError = nil
		m.engineActivity = fmt.Sprintf("✓ rule %s %q", msg.action, msg.name)
		if len(msg.searches) > 0 {
			m.savedSearches = msg.searches
			m.folders.SetSavedSearches(msg.searches)
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
			if errors.Is(msg.err, ErrUndoEmpty) {
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

	case mutedToastMsg:
		if msg.err != nil {
			m.lastError = fmt.Errorf("mute failed: %w", msg.err)
			return m, nil
		}
		m.lastError = nil
		if msg.nowMuted {
			m.engineActivity = fmt.Sprintf("🔕 muted thread (subject: %s)", msg.subject)
		} else {
			m.engineActivity = fmt.Sprintf("🔔 unmuted thread (subject: %s)", msg.subject)
		}
		// Reload current folder to hide/show the newly muted/unmuted thread
		// and refresh the sidebar muted count badge.
		var reloadCmd tea.Cmd
		if m.list.FolderID == mutedSentinelID {
			reloadCmd = m.loadMutedMessagesCmd()
		} else {
			reloadCmd = m.loadMessagesCmd(m.list.FolderID)
		}
		return m, tea.Batch(reloadCmd, m.refreshMutedCountCmd(), m.refreshStreamCountsCmd(), m.clearTransientCmd())

	case mutedCountUpdatedMsg:
		m.folders.SetMutedCount(msg.count)
		return m, nil

	case threadChordTimeoutMsg:
		if msg.token == m.threadChordToken {
			m.threadChordPending = false
			m.engineActivity = ""
		}
		return m, nil

	case streamChordTimeoutMsg:
		if msg.token == m.streamChordToken {
			m.streamChordPending = false
			m.engineActivity = ""
		}
		return m, nil

	case routedMsg:
		m.engineActivity = formatRoutedToast(m.theme, msg.address, msg.dest, msg.priorDest)
		// Reload list when inside a routing virtual folder (the row
		// likely vanished from the current view) or in any folder
		// (counts changed). Always refresh sidebar bucket counts.
		var reload tea.Cmd
		switch {
		case m.list.FolderID == mutedSentinelID:
			reload = m.loadMutedMessagesCmd()
		case IsStreamSentinelID(m.list.FolderID):
			reload = m.loadByRoutingCmd(streamDestinationFromID(m.list.FolderID))
		case m.filterActive:
			reload = m.runFilterCmd(m.filterPattern)
		default:
			if m.list.FolderID != "" {
				reload = m.loadMessagesCmd(m.list.FolderID)
			}
		}
		return m, tea.Batch(reload, m.refreshStreamCountsCmd(), m.clearTransientCmd())

	case routeNoopMsg:
		m.engineActivity = formatRouteNoopToast(msg.address, msg.kind, msg.dest)
		// No list reload — spec 23 §5.7 explicitly skips the reload
		// to avoid visible flicker on a no-op.
		return m, m.clearTransientCmd()

	case routeErrMsg:
		m.lastError = fmt.Errorf("route failed: database error (see logs)")
		if m.deps.Logger != nil {
			m.deps.Logger.Error("route failed", "err", msg.err)
		}
		return m, nil

	case streamCountsUpdatedMsg:
		m.folders.SetStreamCounts(msg.counts)
		return m, nil

	case routeShowToastMsg:
		if msg.dest == "" {
			m.engineActivity = "route: " + msg.address + " is not routed"
		} else {
			m.engineActivity = "route: " + msg.address + " → " + streamDisplayLabelForDestination(msg.dest)
		}
		return m, m.clearTransientCmd()

	case routeListSummaryMsg:
		var parts []string
		for _, dest := range []string{"imbox", "feed", "paper_trail", "screener"} {
			parts = append(parts, fmt.Sprintf("%s=%d", streamDisplayLabelForDestination(dest), msg.counts[dest]))
		}
		m.engineActivity = "routings: " + strings.Join(parts, "  ")
		return m, m.clearTransientCmd()

	case threadPreFetchDoneMsg:
		if msg.err != nil {
			m.lastError = fmt.Errorf("thread: %w", msg.err)
			m.pendingThreadIDs = nil
			return m, nil
		}
		if len(msg.ids) == 0 {
			m.engineActivity = "thread: 0 messages to act on"
			return m, nil
		}
		m.pendingThreadIDs = msg.ids
		n := len(msg.ids)
		var prompt string
		if msg.action == "permanent_delete" {
			prompt = fmt.Sprintf("PERMANENT DELETE — irreversible.\n\nPermanently delete thread (%d messages)?\n\nThis cannot be undone. [y/N]", n)
			m.confirm = m.confirm.Ask(prompt, "thread:permanent_delete")
		} else {
			prompt = fmt.Sprintf("Soft-delete thread (%d messages)? [y/N]", n)
			m.confirm = m.confirm.Ask(prompt, "thread:soft_delete")
		}
		m.mode = ConfirmMode
		return m, nil

	case threadOpDoneMsg:
		m.lastError = nil
		if msg.total == 0 {
			m.engineActivity = "thread: 0 messages to act on"
			return m, m.clearTransientCmd()
		}
		if msg.failed == 0 {
			m.engineActivity = fmt.Sprintf("✓ %s thread (%d messages)", msg.verb, msg.total)
		} else {
			m.engineActivity = fmt.Sprintf("⚠ %s thread: %d/%d succeeded — %d failed", msg.verb, msg.succeeded, msg.total, msg.failed)
		}
		if msg.firstErr != nil {
			m.lastError = fmt.Errorf("%s thread: %w", msg.verb, msg.firstErr)
		}
		return m, tea.Batch(m.loadMessagesCmd(m.list.FolderID), m.clearTransientCmd())

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
		// Same shape for search: m.list.FolderID is the sentinel
		// "search:<query>" which has no real folder backing. Without
		// this branch the v0.13 filter bug rerun for search mode —
		// loadMessagesCmd("search:ABC") returned zero, every result
		// vanished after `d`, and the deleted message reappeared in
		// the next /<query> because the FTS path didn't exclude
		// trash. This bug had two halves; the FTS exclusion in
		// store.Search is the other.
		if m.searchActive {
			m.engineActivity = fmt.Sprintf("✓ %s · u to undo", msg.name)
			return m, m.runSearchCmd(m.searchQuery)
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
	case SettingsMode:
		return m.updateSettings(msg)
	case ComposeMode:
		return m.updateCompose(msg)
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
	case FolderPickerMode:
		return m.updateFolderPicker(msg)
	case CalendarDetailMode:
		return m.updateCalendarDetail(msg)
	case RuleEditMode:
		return m.updateRuleEdit(msg)
	case AttachPickMode:
		return m.updateAttachPick(msg)
	case PaletteMode:
		return m.updatePalette(msg)
	default:
		return m.updateNormal(msg)
	}
}

// updateFullscreenBody handles input while the body is in fullscreen
// mode. j/k scroll; r/R/f compose; d/D/a triage; Esc/q/z return.
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

	// Triage and compose actions mirror the viewer pane in NormalMode
	// so the user does not need to exit fullscreen to act on a message.
	// Mode is reset to NormalMode first so triageDoneMsg lands cleanly.
	case key.Matches(keyMsg, m.keymap.MarkRead):
		// r → reply (viewer-pane pane-scoped meaning, same as NormalMode)
		if cur := m.viewer.current; cur != nil && m.deps.Drafts != nil {
			m.mode = NormalMode
			return m.startCompose(*cur)
		}
	case key.Matches(keyMsg, m.keymap.MarkUnread):
		// R → reply-all
		if cur := m.viewer.current; cur != nil && m.deps.Drafts != nil {
			m.mode = NormalMode
			return m.startComposeReplyAll(*cur)
		}
	case key.Matches(keyMsg, m.keymap.ToggleFlag):
		// f → forward (viewer-pane meaning)
		if cur := m.viewer.current; cur != nil && m.deps.Drafts != nil {
			m.mode = NormalMode
			return m.startComposeForward(*cur)
		}
	case key.Matches(keyMsg, m.keymap.Delete):
		if cur := m.viewer.current; cur != nil {
			m.mode = NormalMode
			return m.runTriage("soft_delete", *cur, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
				return m.deps.Triage.SoftDelete(ctx, accID, src.ID)
			})
		}
	case key.Matches(keyMsg, m.keymap.PermanentDelete):
		if m.lastDraftID != "" {
			m.pendingDiscardDraftID = m.lastDraftID
			m.confirm = m.confirm.Ask("Discard saved draft?", "discard_draft")
			m.mode = ConfirmMode
			return m, nil
		}
		if cur := m.viewer.current; cur != nil {
			m.mode = NormalMode
			return m.startPermanentDelete(*cur)
		}
	case key.Matches(keyMsg, m.keymap.Archive):
		if cur := m.viewer.current; cur != nil {
			m.mode = NormalMode
			return m.runTriage("archive", *cur, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
				return m.deps.Triage.Archive(ctx, accID, src.ID)
			})
		}
	}
	switch keyMsg.String() {
	case "esc", "q", "z":
		m.mode = NormalMode
	}
	return m, nil
}

// updateURLPicker handles input while the URL picker overlay is
// open. j/k cursor; Enter / O open in browser; y yank; Esc / q
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

// startCompose enters the spec 15 v2 in-modal compose pane,
// pre-filled with a reply skeleton for the supplied source
// message. Replaces the legacy startReplyCmd which dispatched
// $EDITOR via tea.ExecProcess; the in-modal flow keeps inkwell's
// UI on screen so save / discard live in the persistent footer
// (resolves the user-reported "select Exit command first"
// friction).
//
// Spec 15 §7 / PR 7-ii: the compose session is persisted into
// `compose_sessions` on entry so a crash mid-edit can be resumed
// on the next launch. Subsequent focus changes (Tab / Shift+Tab)
// re-persist; save / discard mark the row confirmed.
func (m Model) startCompose(src store.Message) (tea.Model, tea.Cmd) {
	return m.startComposeOfKind(ComposeKindReply, &src)
}

// startComposeReplyAll opens the in-modal compose pane pre-filled
// for a reply-all (To = src.From + remaining To recipients; Cc =
// src.Cc; both deduped against the user's own UPN). Spec 15 §9 /
// PR 7-iii.
func (m Model) startComposeReplyAll(src store.Message) (tea.Model, tea.Cmd) {
	return m.startComposeOfKind(ComposeKindReplyAll, &src)
}

// startComposeForward opens the compose pane for a forward of src
// (To/Cc empty, Subject prefixed "Fwd:", body opens with the
// canonical "Forwarded message" header block). Spec 15 §9 /
// PR 7-iii.
func (m Model) startComposeForward(src store.Message) (tea.Model, tea.Cmd) {
	return m.startComposeOfKind(ComposeKindForward, &src)
}

// startComposeNew opens the compose pane for a brand-new draft
// (no source). Focus drops into the To field because recipients
// are the user's first task. Spec 15 §9 / PR 7-iii.
func (m Model) startComposeNew() (tea.Model, tea.Cmd) {
	return m.startComposeOfKind(ComposeKindNew, nil)
}

// startComposeOfKind is the shared body of the per-kind starters.
// src is non-nil for Reply / ReplyAll / Forward and nil for New.
// The kind selects the apply skeleton; everything else (Drafts
// guard, NewCompose, SessionID assignment, persist) is identical.
func (m Model) startComposeOfKind(kind ComposeKind, src *store.Message) (tea.Model, tea.Cmd) {
	if m.deps.Drafts == nil {
		m.lastError = fmt.Errorf("compose: not wired (drafts component missing)")
		return m, nil
	}
	m.compose = NewCompose()
	m.compose.SessionID = newComposeSessionID()
	userUPN := ""
	if m.deps.Account != nil {
		userUPN = m.deps.Account.UPN
	}
	switch kind {
	case ComposeKindReply:
		if src == nil {
			m.lastError = fmt.Errorf("reply: no source message")
			return m, nil
		}
		m.compose.ApplyReplySkeleton(*src, src.BodyPreview)
	case ComposeKindReplyAll:
		if src == nil {
			m.lastError = fmt.Errorf("reply-all: no source message")
			return m, nil
		}
		m.compose.ApplyReplyAllSkeleton(*src, src.BodyPreview, userUPN)
	case ComposeKindForward:
		if src == nil {
			m.lastError = fmt.Errorf("forward: no source message")
			return m, nil
		}
		m.compose.ApplyForwardSkeleton(*src, src.BodyPreview)
	case ComposeKindNew:
		m.compose.ApplyNewSkeleton()
	}
	m.mode = ComposeMode
	return m, m.persistComposeSnapshotCmd()
}

// updateCompose handles input while the compose pane is open:
//
//	Tab          → next field   (Body → To → Cc → Subject → Body)
//	Shift+Tab    → previous field
//	Ctrl+S / Esc → save (dispatches saveComposeCmd)
//	Ctrl+D       → discard (no Graph round-trip; modal closes)
//	other keys   → forwarded to the focused field's component
//
// Esc-as-save matches the user's mental model from the prior
// post-edit modal where Enter aliased to save; the in-modal flow
// keeps that "I'm done" gesture so the redesign doesn't break
// muscle memory.
func (m Model) updateCompose(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)
	if !isKey {
		var cmd tea.Cmd
		m.compose, cmd = m.compose.UpdateField(msg)
		return m, cmd
	}
	switch keyMsg.Type {
	case tea.KeyTab:
		m.compose.NextField()
		// Re-persist on focus change so the resume scan finds the
		// most-recent state of the field the user just left.
		return m, m.persistComposeSnapshotCmd()
	case tea.KeyShiftTab:
		m.compose.PrevField()
		return m, m.persistComposeSnapshotCmd()
	case tea.KeyCtrlS, tea.KeyEsc:
		snap := m.compose.Snapshot()
		sessionID := m.compose.SessionID
		m.mode = NormalMode
		m.compose = NewCompose()
		m.engineActivity = "saving draft…"
		// saveComposeCmd folds the ConfirmComposeSession write into
		// its goroutine so the resume scan stops offering this row
		// regardless of whether the Graph round-trip succeeded —
		// the user explicitly pressed save.
		return m, m.saveComposeCmd(snap, sessionID)
	case tea.KeyCtrlD:
		// Discard is a local-only action; do the confirm inline
		// (sub-ms SQLite WAL write) so the test contract "Ctrl+D
		// returns no Cmd" stays clean and the user's next keystroke
		// isn't ahead of the persistence layer. Persistence
		// failure is benign — the row stays unconfirmed and the
		// resume scan offers it again on next launch (worst case
		// the user discards twice).
		m.confirmComposeSessionInline(m.compose.SessionID)
		m.mode = NormalMode
		m.compose = NewCompose()
		m.engineActivity = "draft discarded"
		return m, nil
	case tea.KeyCtrlE:
		body := m.compose.Body()
		path, err := compose.WriteTempfile(body)
		if err != nil {
			m.lastError = err
			return m, nil
		}
		editorCmd, err := compose.EditorCmd(path)
		if err != nil {
			compose.CleanupTempfile(path)
			m.lastError = err
			return m, nil
		}
		return m, tea.ExecProcess(editorCmd, func(err error) tea.Msg {
			return composeEditorDoneMsg{tempPath: path, err: err}
		})
	case tea.KeyCtrlA:
		m.attachPickInput = newAttachPickInput()
		m.attachPickInput.Focus()
		m.mode = AttachPickMode
		return m, textinput.Blink
	}
	// Any other key: forward to the focused field. textinput /
	// textarea handle character insert / cursor movement / etc.
	var cmd tea.Cmd
	m.compose, cmd = m.compose.UpdateField(msg)
	return m, cmd
}

// updateOOF handles input while the out-of-office modal is open.
func (m Model) updateOOF(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch {
	case keyMsg.Type == tea.KeyEsc, string(keyMsg.Runes) == "q":
		m.mode = m.oofReturnMode
		m.oofReturnMode = NormalMode
		m.oof.Reset()
		return m, nil
	case keyMsg.Type == tea.KeyTab:
		m.oof.NextField()
		return m, nil
	case keyMsg.Type == tea.KeyShiftTab:
		m.oof.PrevField()
		return m, nil
	case string(keyMsg.Runes) == " ":
		if m.oof.cursor == 5 {
			m.oof.ToggleAudience()
		} else {
			m.oof.ToggleStatus()
		}
		return m, nil
	case keyMsg.Type == tea.KeyEnter:
		if m.oof.loading || m.oof.saving {
			return m, nil
		}
		if ve := m.oof.Validate(); ve != "" {
			m.oof.validErr = ve
			return m, nil
		}
		m.oof.validErr = ""
		s := m.oof.ToMailboxSettings()
		m.oof.SetSaving()
		return m, m.toggleOOFCmd(s)
	}
	return m, nil
}

// updateAttachPick handles input for the attachment path-input overlay.
// Esc returns to ComposeMode; Enter validates and stages the file.
func (m Model) updateAttachPick(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)
	if !isKey {
		var cmd tea.Cmd
		m.attachPickInput, cmd = m.attachPickInput.Update(msg)
		return m, cmd
	}
	switch keyMsg.Type {
	case tea.KeyEsc:
		m.mode = ComposeMode
		return m, nil
	case tea.KeyEnter:
		path := strings.TrimSpace(m.attachPickInput.Value())
		if strings.HasPrefix(path, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				path = home + path[1:]
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			m.lastError = fmt.Errorf("attach: %w", err)
			m.mode = ComposeMode
			return m, nil
		}
		if m.deps.AttachmentMaxSizeMB > 0 {
			limitBytes := int64(m.deps.AttachmentMaxSizeMB) * 1024 * 1024
			if info.Size() > limitBytes {
				m.lastError = fmt.Errorf("attach: file exceeds %d MB limit", m.deps.AttachmentMaxSizeMB)
				m.mode = ComposeMode
				return m, nil
			}
		}
		if m.deps.MaxAttachments > 0 && len(m.compose.Attachments()) >= m.deps.MaxAttachments {
			m.lastError = fmt.Errorf("attach: maximum %d attachments per draft", m.deps.MaxAttachments)
			m.mode = ComposeMode
			return m, nil
		}
		m.compose.AddAttachment(AttachmentSnapshotRef{
			LocalPath: path,
			Name:      filepath.Base(path),
			SizeBytes: info.Size(),
		})
		m.mode = ComposeMode
		return m, nil
	default:
		var cmd tea.Cmd
		m.attachPickInput, cmd = m.attachPickInput.Update(msg)
		return m, cmd
	}
}

// updateSettings handles input while the mailbox-settings overview modal
// is open. Spec 13 §5.2: `o` opens the OOF modal; Esc/q closes.
func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch {
	case keyMsg.Type == tea.KeyEsc, string(keyMsg.Runes) == "q":
		m.settingsView.Reset()
		m.mode = NormalMode
		return m, nil
	case string(keyMsg.Runes) == "o":
		if m.deps.Mailbox == nil {
			return m, nil
		}
		m.oofReturnMode = SettingsMode
		m.mode = OOFMode
		m.oof.SetLoading()
		if m.settingsView.settings != nil {
			// Pre-fill from already-loaded settings to avoid a second fetch.
			m.oof.SetSettings(m.settingsView.settings)
		}
		return m, m.fetchOOFCmd()
	}
	return m, nil
}

// updateCalendar handles input while the calendar list modal is
// open. Spec 12 §6.2: j/k navigate; Enter opens the detail modal;
// ]/[ day nav; }/{  week nav; t today; Esc/q closes.
func (m Model) updateCalendar(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch {
	case keyMsg.Type == tea.KeyEsc, string(keyMsg.Runes) == "q":
		m.mode = NormalMode
		m.calendar.Reset()
		return m, nil
	case key.Matches(keyMsg, m.keymap.Down):
		m.calendar.Down()
		return m, nil
	case key.Matches(keyMsg, m.keymap.Up):
		m.calendar.Up()
		return m, nil
	case keyMsg.Type == tea.KeyEnter:
		sel := m.calendar.Selected()
		if sel == nil || sel.ID == "" || m.deps.Calendar == nil {
			return m, nil
		}
		m.calendarDetail.SetLoading()
		m.calDetailPriorMode = m.mode
		m.mode = CalendarDetailMode
		return m, m.fetchEventCmd(sel.ID)
	case string(keyMsg.Runes) == "]":
		m.calendar.NavNextDay()
		m.calendar.SetLoading()
		return m, m.fetchCalendarForDateCmd(m.calendar.ViewDate())
	case string(keyMsg.Runes) == "[":
		m.calendar.NavPrevDay()
		m.calendar.SetLoading()
		return m, m.fetchCalendarForDateCmd(m.calendar.ViewDate())
	case string(keyMsg.Runes) == "}":
		m.calendar.NavNextWeek()
		m.calendar.SetLoading()
		return m, m.fetchCalendarForDateCmd(m.calendar.ViewDate())
	case string(keyMsg.Runes) == "{":
		m.calendar.NavPrevWeek()
		m.calendar.SetLoading()
		return m, m.fetchCalendarForDateCmd(m.calendar.ViewDate())
	case string(keyMsg.Runes) == "t":
		m.calendar.GotoToday()
		m.calendar.SetLoading()
		return m, m.fetchCalendarCmd()
	case string(keyMsg.Runes) == "w":
		m.calendar.ToggleWeekMode()
		m.calendar.SetLoading()
		if m.calendar.IsWeekMode() {
			return m, m.fetchCalendarForWeekCmd()
		}
		return m, m.fetchCalendarForDateCmd(m.calendar.ViewDate())
	}
	return m, nil
}

// updateCalendarDetail handles input inside the detail modal.
// Spec 12 §7: Esc returns to the list; o opens the webLink; l
// opens the online meeting URL. Both shellouts are best-effort —
// the modal stays open after dispatching the open call.
func (m Model) updateCalendarDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch {
	case keyMsg.Type == tea.KeyEsc, string(keyMsg.Runes) == "q":
		m.calendarDetail.Reset()
		m.mode = m.calDetailPriorMode
		return m, nil
	case key.Matches(keyMsg, m.keymap.Down):
		m.calendarDetail.ScrollDown()
		return m, nil
	case key.Matches(keyMsg, m.keymap.Up):
		m.calendarDetail.ScrollUp()
		return m, nil
	case key.Matches(keyMsg, m.keymap.PageDown):
		m.calendarDetail.PageDown()
		return m, nil
	case key.Matches(keyMsg, m.keymap.PageUp):
		m.calendarDetail.PageUp()
		return m, nil
	case string(keyMsg.Runes) == "o":
		if d := m.calendarDetail.Detail(); d != nil && d.WebLink != "" {
			go openInBrowser(d.WebLink)
			m.engineActivity = "opened event in Outlook"
		}
		return m, nil
	case string(keyMsg.Runes) == "l":
		if d := m.calendarDetail.Detail(); d != nil && d.OnlineMeetingURL != "" {
			go openInBrowser(d.OnlineMeetingURL)
			m.engineActivity = "opened meeting URL"
		}
		return m, nil
	}
	return m, nil
}

// eventFetchedMsg lands when CalendarFetcher.GetEvent completes.
type eventFetchedMsg struct {
	Detail CalendarEventDetail
	Err    error
}

// fetchEventCmd hits CalendarFetcher.GetEvent in a goroutine and
// returns an eventFetchedMsg.
func (m Model) fetchEventCmd(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d, err := m.deps.Calendar.GetEvent(ctx, id)
		return eventFetchedMsg{Detail: d, Err: err}
	}
}

func (m Model) updateNormal(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	// Spec 05 §12 / PR 10: digits 1-9 open the corresponding numbered
	// link directly from the viewer pane. These are intercepted BEFORE
	// the global FocusFolders/FocusList/FocusViewer bindings (which
	// consume 1/2/3) because the spec assigns 1-9 to link-open in the
	// viewer context. When the viewer has no link [N], the digit is NOT
	// consumed — pane-focus bindings still fire for 1/2/3.
	if m.focused == ViewerPane && keyMsg.Type == tea.KeyRunes && len(keyMsg.Runes) == 1 {
		r := keyMsg.Runes[0]
		if r >= '1' && r <= '9' {
			n := int(r - '0')
			for _, l := range m.viewer.Links() {
				if l.Index == n {
					go openInBrowser(l.URL)
					m.engineActivity = fmt.Sprintf("opening link %d…", n)
					return m, nil
				}
			}
			// No link at index n — fall through to global key switch.
		}
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
	case key.Matches(keyMsg, m.keymap.Palette):
		m.palette.Open(&m)
		m.mode = PaletteMode
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
	case key.Matches(keyMsg, m.keymap.Filter):
		// When bulkPending is true the user typed `;F` (unflag chord);
		// let dispatchList handle it rather than opening the command bar.
		if !m.bulkPending {
			m.mode = CommandMode
			m.cmd.Activate()
			m.cmd.buf = "filter "
			return m, nil
		}
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
			if m.searchCancel != nil {
				m.searchCancel()
			}
			m.searchCancel = nil
			m.searchUpdates = nil
			m.searchStatus = ""
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
		// Detect spec 06 §5.3 `--all` prefix: cross-folder search.
		allFolders := false
		if strings.HasPrefix(q, "--all") {
			q = strings.TrimSpace(q[5:])
			allFolders = true
		}
		// Detect spec 06 §4.3 `--sort=relevance` prefix: BM25 sort.
		sortByRelev := false
		if strings.HasPrefix(q, "--sort=relevance") {
			q = strings.TrimSpace(q[16:])
			sortByRelev = true
		}
		if q == "" {
			return m, nil
		}
		// Cancel any in-flight search before kicking the new one
		// so the prior stream's stale snapshots can't overwrite
		// the new query's results.
		if m.searchCancel != nil {
			m.searchCancel()
		}
		m.searchCancel = nil
		m.searchUpdates = nil
		m.searchActive = true
		m.searchQuery = q
		m.searchSortByRelev = sortByRelev
		if allFolders {
			m.searchFolderID = ""
		} else {
			m.searchFolderID = m.priorFolderID
		}
		m.searchStatus = "[searching…]"
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

// runSearchCmd kicks off a hybrid search and returns the first
// SearchUpdateMsg (or a fallback MessagesLoadedMsg when no
// SearchService is wired). Streaming continuation lives in
// consumeSearchUpdatesCmd, which the SearchUpdateMsg handler
// re-arms until Done flips.
//
// Spec 06 §3 / §4: when deps.Search is non-nil, we hand the query
// to the streaming searcher and return a Cmd that waits for the
// first snapshot. The Update handler then schedules
// consumeSearchUpdatesCmd to drain the rest of the channel. When
// deps.Search is nil (test setup, degraded mode), we fall back to
// the legacy single-shot store.Search path.
func (m Model) runSearchCmd(q string) tea.Cmd {
	if m.deps.Search != nil {
		return m.startStreamingSearchCmd(q)
	}
	folderID := m.searchFolderID
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
			FolderID:  folderID,
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

// startStreamingSearchCmd opens a streaming SearchService.Search
// channel and returns the first snapshot wrapped in a
// SearchUpdateMsg. The Update handler then re-arms
// consumeSearchUpdatesCmd to drain subsequent snapshots until the
// channel closes.
//
// The cancel function is stashed on the Model (via the dispatched
// SearchUpdateMsg's reflective handler) so a subsequent Esc or
// new search can call it cleanly. We rely on the search service
// itself to be idempotent on Cancel.
func (m Model) startStreamingSearchCmd(q string) tea.Cmd {
	svc := m.deps.Search
	folderID := m.searchFolderID
	sortByRelev := m.searchSortByRelev
	return func() tea.Msg {
		ctx := context.Background()
		ch, cancel := svc.Search(ctx, q, folderID, sortByRelev)
		// Block for the first snapshot so the user sees results
		// before the next render frame. The streaming continuation
		// is handed off via SearchUpdateMsg.Done routing in the
		// Update handler.
		snap, ok := <-ch
		if !ok {
			cancel()
			return SearchUpdateMsg{Query: q, Done: true, Status: "[no matches]"}
		}
		// Stash the cancel + remaining channel via a typed
		// envelope the Update handler routes — see the
		// `searchStreamMsg` channel-handoff path below.
		return searchStreamMsg{
			query:    q,
			snapshot: snap,
			ch:       ch,
			cancel:   cancel,
		}
	}
}

// searchStreamMsg carries the live channel + cancel from a
// streaming Searcher run into the Update loop. The handler stows
// the cancel on the Model and dispatches a follow-up Cmd to drain
// the channel into SearchUpdateMsgs. Defined here (not in
// messages.go) because it carries non-public references the test
// stubs don't need to see.
type searchStreamMsg struct {
	query    string
	snapshot SearchSnapshot
	ch       <-chan SearchSnapshot
	cancel   func()
}

// consumeSearchUpdatesCmd reads one snapshot off ch and returns
// it as a SearchUpdateMsg. When ch closes, returns Done=true so
// the Update handler can clean up. Mirrors the
// consumeSyncEventsCmd pattern (long-running channel-drain Cmd
// re-armed on each emission).
func (m Model) consumeSearchUpdatesCmd(query string, ch <-chan SearchSnapshot) tea.Cmd {
	return func() tea.Msg {
		snap, ok := <-ch
		if !ok {
			return SearchUpdateMsg{Query: query, Done: true}
		}
		return SearchUpdateMsg{
			Query:   query,
			Status:  snap.Status,
			Results: snap.Messages,
		}
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
			m.lastError = fmt.Errorf("filter: usage `:filter [--all] <pattern>` (spec 08 operators or plain text)")
			return m, nil
		}
		patternSrc := strings.TrimSpace(strings.TrimPrefix(line, "filter"))
		allFolders := false
		if strings.HasPrefix(patternSrc, "--all") {
			patternSrc = strings.TrimSpace(patternSrc[5:])
			allFolders = true
		} else if patternSrc == "-a" || strings.HasPrefix(patternSrc, "-a ") {
			patternSrc = strings.TrimSpace(patternSrc[2:])
			allFolders = true
		}
		if patternSrc == "" {
			m.lastError = fmt.Errorf("filter: usage :filter [--all] <pattern>")
			return m, nil
		}
		m.filterAllFolders = allFolders
		return m, m.runFilterCmd(patternSrc)
	case "save":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("save: usage `:save <name>` or `:save <letter> [path]` in viewer")
			return m, nil
		}
		// Viewer-context attachment save: `:save <letter> [custom-path]` (spec 05 §8).
		// Routed here only when the first arg is a single a-z letter that maps to
		// an existing attachment in the viewer. Otherwise falls through to rule save.
		if m.focused == ViewerPane && len(args[1]) == 1 {
			r := rune(args[1][0])
			if r >= 'a' && r <= 'z' {
				atts := m.viewer.Attachments()
				if idx := int(r - 'a'); idx < len(atts) {
					att := atts[idx]
					if len(args) >= 3 {
						customPath := expandHomePath(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "save")), args[1])))
						return m, m.saveAttachmentToPathCmd(att, customPath)
					}
					return m.startSaveAttachment(att)
				}
			}
		}
		if !m.filterActive || m.filterPattern == "" {
			m.lastError = fmt.Errorf("save: no active filter — run :filter <pattern> first")
			return m, nil
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "save"))
		return m, m.ruleSaveCmd(name, m.filterPattern)
	case "rule":
		return m.dispatchRule(args[1:], strings.TrimSpace(strings.TrimPrefix(line, "rule")))
	case "route":
		return m.dispatchRoute(args[1:])
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
		// `:open <N>` opens numbered link N (spec 05 §9); `:open` alone
		// opens the focused message's webLink (spec 04 §6.4).
		if len(args) >= 2 && len(args[1]) == 1 && args[1][0] >= '1' && args[1][0] <= '9' {
			n := int(args[1][0] - '0')
			for _, l := range m.viewer.Links() {
				if l.Index == n {
					go openInBrowser(l.URL)
					m.engineActivity = fmt.Sprintf("opening link %d…", n)
					return m, nil
				}
			}
			m.lastError = fmt.Errorf("open: no link [%d] in current message", n)
			return m, nil
		}
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
	case "copy":
		// `:copy <N>` copies numbered link N to the clipboard (spec 05 §9).
		if len(args) < 2 || len(args[1]) != 1 || args[1][0] < '1' || args[1][0] > '9' {
			m.lastError = fmt.Errorf("copy: usage `:copy <N>` where N is 1–9")
			return m, nil
		}
		n := int(args[1][0] - '0')
		for _, l := range m.viewer.Links() {
			if l.Index == n {
				return m.yankURL(l.URL)
			}
		}
		m.lastError = fmt.Errorf("copy: no link [%d] in current message", n)
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
		// Sub-commands: on, off, schedule; plain :ooo opens modal.
		sub := ""
		if len(args) >= 2 {
			sub = args[1]
		}
		switch sub {
		case "on":
			s := MailboxSettings{AutoReplyStatus: "alwaysEnabled"}
			if m.mailboxSettings != nil {
				s = *m.mailboxSettings
				s.AutoReplyStatus = "alwaysEnabled"
			}
			return m, m.toggleOOFCmd(s)
		case "off":
			s := MailboxSettings{AutoReplyStatus: "disabled"}
			if m.mailboxSettings != nil {
				s = *m.mailboxSettings
				s.AutoReplyStatus = "disabled"
			}
			return m, m.toggleOOFCmd(s)
		case "schedule":
			m.oofReturnMode = NormalMode
			m.mode = OOFMode
			m.oof.Reset()
			m.oof.SetLoading()
			if m.mailboxSettings != nil {
				m.oof.SetSettings(m.mailboxSettings)
			}
			m.oof.editStatus = "scheduled"
			return m, m.fetchOOFCmd()
		default:
			m.oofReturnMode = NormalMode
			m.mode = OOFMode
			m.oof.SetLoading()
			return m, m.fetchOOFCmd()
		}
	case "settings":
		if m.deps.Mailbox == nil {
			m.lastError = fmt.Errorf("settings: not wired (CLI mode or unsigned)")
			return m, nil
		}
		m.mode = SettingsMode
		m.settingsView.SetLoading()
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
//
// PR 9 routed this through pattern.Compile + pattern.Execute so the
// strategy selector + Plan.Notes + future server-routed bulk
// operations all share the same path. LocalOnly is forced for now —
// :filter has always been "narrow the cached folder" UX; flipping
// to default-server would silently dispatch Graph queries on every
// keystroke. A `--server` flag can lift the gate later.
func (m Model) runFilterCmd(src string) tea.Cmd {
	src = strings.TrimSpace(src)
	if !strings.Contains(src, "~") {
		src = "~B *" + src + "*"
	}
	return func() tea.Msg {
		compiled, err := pattern.Compile(src, pattern.CompileOptions{LocalOnly: true})
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("filter: %w", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		ids, err := pattern.Execute(ctx, compiled, m.deps.Store, nil, pattern.ExecuteOptions{
			AccountID:       accountID,
			LocalMatchLimit: 1000,
		})
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("filter: %w", err)}
		}
		// Execute returns IDs; the list pane wants Messages.
		// Bulk-fetch the envelope rows from the local cache.
		msgs := make([]store.Message, 0, len(ids))
		for _, id := range ids {
			if mm, err := m.deps.Store.GetMessage(ctx, id); err == nil && mm != nil {
				msgs = append(msgs, *mm)
			}
		}
		return filterAppliedMsg{src: src, messages: msgs}
	}
}

func (m Model) clearFilter() Model {
	m.filterActive = false
	m.filterPattern = ""
	m.filterIDs = nil
	m.filterAllFolders = false
	m.filterFolderCount = 0
	m.filterFolderName = ""
	m.list.folderNameByID = nil
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

// dispatchRule handles the :rule sub-command surface (spec 11 §5.4 / §5.5).
// line is the full argument string after "rule " (for multi-word names).
func (m Model) dispatchRule(args []string, line string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.lastError = fmt.Errorf("rule: usage :rule save <name> | list | show <name> | edit <name> | delete <name>")
		return m, nil
	}
	switch args[0] {
	case "save":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("rule save: usage :rule save <name>")
			return m, nil
		}
		if !m.filterActive || m.filterPattern == "" {
			m.lastError = fmt.Errorf("rule save: no active filter — run :filter <pattern> first")
			return m, nil
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "save"))
		return m, m.ruleSaveCmd(name, m.filterPattern)
	case "list":
		if len(m.savedSearches) == 0 {
			m.engineActivity = "no saved searches (use :rule save <name>)"
			return m, nil
		}
		var parts []string
		for _, ss := range m.savedSearches {
			parts = append(parts, ss.Name)
		}
		m.engineActivity = "saved: " + strings.Join(parts, ", ")
		return m, nil
	case "show":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("rule show: usage :rule show <name>")
			return m, nil
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "show"))
		for _, ss := range m.savedSearches {
			if ss.Name == name {
				m.engineActivity = fmt.Sprintf("%s: %s", ss.Name, ss.Pattern)
				return m, nil
			}
		}
		m.lastError = fmt.Errorf("rule show: %q not found", name)
		return m, nil
	case "edit":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("rule edit: usage :rule edit <name>")
			return m, nil
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "edit"))
		for _, ss := range m.savedSearches {
			if ss.Name == name {
				return m.startRuleEdit(ss)
			}
		}
		m.lastError = fmt.Errorf("rule edit: %q not found", name)
		return m, nil
	case "delete":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("rule delete: usage :rule delete <name>")
			return m, nil
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "delete"))
		m.pendingRuleDelete = name
		m.confirm = m.confirm.Ask(fmt.Sprintf("Delete saved search %q?", name), "rule_delete")
		m.mode = ConfirmMode
		return m, nil
	}
	m.lastError = fmt.Errorf("rule: unknown sub-command %q (save / list / show / edit / delete)", args[0])
	return m, nil
}

// refreshSavedSearchCountsCmd evaluates all pinned saved searches and
// emits a savedSearchesUpdatedMsg with fresh count badges for the sidebar.
func (m Model) refreshSavedSearchCountsCmd() tea.Cmd {
	if m.deps.SavedSearchSvc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		searches, err := m.deps.SavedSearchSvc.RefreshCounts(ctx)
		if err != nil {
			return nil // non-fatal: sidebar keeps stale counts
		}
		return savedSearchesUpdatedMsg{searches: searches}
	}
}

// ruleSaveCmd persists the current filter as a named saved search.
// clearTransientCmd returns a Cmd that sleeps TransientStatusTTL then
// emits clearTransientMsg to auto-clear m.engineActivity. Returns nil
// when TTL is zero (auto-clear disabled).
func (m Model) clearTransientCmd() tea.Cmd {
	if m.deps.TransientStatusTTL <= 0 {
		return nil
	}
	ttl := m.deps.TransientStatusTTL
	return func() tea.Msg {
		time.Sleep(ttl)
		return clearTransientMsg{}
	}
}

func (m Model) ruleSaveCmd(name, patternSrc string) tea.Cmd {
	if m.deps.SavedSearchSvc == nil {
		return func() tea.Msg {
			return savedSearchSavedMsg{action: "saved", name: name, err: fmt.Errorf("saved search: not wired")}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.deps.SavedSearchSvc.Save(ctx, name, patternSrc, true); err != nil {
			return savedSearchSavedMsg{action: "saved", name: name, err: err}
		}
		searches, _ := m.deps.SavedSearchSvc.Reload(ctx)
		return savedSearchSavedMsg{action: "saved", name: name, searches: searches}
	}
}

// ruleDeleteCmd removes a named saved search after the user confirmed.
func (m Model) ruleDeleteCmd(name string) tea.Cmd {
	if m.deps.SavedSearchSvc == nil {
		return func() tea.Msg {
			return savedSearchSavedMsg{action: "deleted", name: name, err: fmt.Errorf("saved search: not wired")}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.deps.SavedSearchSvc.DeleteByName(ctx, name); err != nil {
			return savedSearchSavedMsg{action: "deleted", name: name, err: err}
		}
		searches, _ := m.deps.SavedSearchSvc.Reload(ctx)
		return savedSearchSavedMsg{action: "deleted", name: name, searches: searches}
	}
}

// startRuleEdit opens the spec 11 B-2 edit modal for the given saved search.
func (m Model) startRuleEdit(ss SavedSearch) (tea.Model, tea.Cmd) {
	if m.deps.SavedSearchSvc == nil {
		m.lastError = fmt.Errorf("rule edit: not wired")
		return m, nil
	}
	m.ruleEditOrigName = ss.Name
	m.ruleEditName = ss.Name
	m.ruleEditPattern = ss.Pattern
	m.ruleEditPinned = ss.Pinned
	m.ruleEditField = 0
	m.ruleEditTestMsg = ""
	m.ruleEditTesting = false
	m.mode = RuleEditMode
	return m, nil
}

// updateRuleEdit handles keyboard input while the rule-edit modal is open.
// Fields: 0=name, 1=pattern, 2=pinned (boolean toggle).
// ctrl+t tests the pattern; Enter saves; Esc cancels; Tab/Shift+Tab cycle fields.
func (m Model) updateRuleEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.Type {
	case tea.KeyEsc:
		m.mode = NormalMode
		m.ruleEditTestMsg = ""
		m.engineActivity = "rule edit cancelled"
		return m, nil
	case tea.KeyEnter:
		name := strings.TrimSpace(m.ruleEditName)
		pat := strings.TrimSpace(m.ruleEditPattern)
		if name == "" {
			m.ruleEditTestMsg = "⚠ name is required"
			return m, nil
		}
		if pat == "" {
			m.ruleEditTestMsg = "⚠ pattern is required"
			return m, nil
		}
		origName := m.ruleEditOrigName
		pinned := m.ruleEditPinned
		svc := m.deps.SavedSearchSvc
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			if err := svc.Edit(ctx, origName, name, pat, pinned); err != nil {
				return ruleEditDoneMsg{err: err, newName: name}
			}
			searches, err := svc.Reload(ctx)
			if err != nil {
				searches = nil
			}
			return ruleEditDoneMsg{searches: searches, newName: name}
		}
	case tea.KeyTab, tea.KeyDown:
		m.ruleEditField = (m.ruleEditField + 1) % 3
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		m.ruleEditField = (m.ruleEditField + 2) % 3
		return m, nil
	case tea.KeyBackspace:
		switch m.ruleEditField {
		case 0:
			if len(m.ruleEditName) > 0 {
				m.ruleEditName = m.ruleEditName[:len(m.ruleEditName)-1]
			}
		case 1:
			if len(m.ruleEditPattern) > 0 {
				m.ruleEditPattern = m.ruleEditPattern[:len(m.ruleEditPattern)-1]
			}
		}
		return m, nil
	case tea.KeyCtrlT:
		// ctrl+t tests the pattern from any field without consuming
		// regular 't' runes from name/pattern text inputs.
		if !m.ruleEditTesting {
			pat := strings.TrimSpace(m.ruleEditPattern)
			if pat == "" {
				m.ruleEditTestMsg = "⚠ enter a pattern first"
				return m, nil
			}
			m.ruleEditTesting = true
			m.ruleEditTestMsg = "testing…"
			svc := m.deps.SavedSearchSvc
			return m, func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				count, err := svc.EvaluatePattern(ctx, pat)
				return ruleEditTestDoneMsg{count: count, err: err}
			}
		}
		return m, nil
	case tea.KeyRunes:
		switch m.ruleEditField {
		case 0:
			m.ruleEditName += string(keyMsg.Runes)
		case 1:
			m.ruleEditPattern += string(keyMsg.Runes)
		case 2:
			// Space toggles pinned in the boolean field.
			if string(keyMsg.Runes) == " " {
				m.ruleEditPinned = !m.ruleEditPinned
			}
		}
	case tea.KeySpace:
		switch m.ruleEditField {
		case 0:
			m.ruleEditName += " "
		case 1:
			m.ruleEditPattern += " "
		case 2:
			m.ruleEditPinned = !m.ruleEditPinned
		}
	}
	return m, nil
}

// backgroundRefreshTimerCmd sleeps for the configured background refresh
// interval and emits savedSearchBgRefreshMsg to trigger a count refresh.
func (m Model) backgroundRefreshTimerCmd() tea.Cmd {
	interval := m.deps.SavedSearchBgRefresh
	return func() tea.Msg {
		time.Sleep(interval)
		return savedSearchBgRefreshMsg{}
	}
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

// oofToggledMsg is the result of the save PATCH (spec 13). Settings
// is the post-PATCH state on success; Err carries the failure.
type oofToggledMsg struct {
	Settings *MailboxSettings
	Err      error
}

// mailboxRefreshedMsg carries background-refresh results. Unlike
// oofLoadedMsg it does not open or modify the OOF modal.
type mailboxRefreshedMsg struct {
	Settings *MailboxSettings
	Err      error
}

// mailboxTickMsg fires when the auto-refresh timer elapses.
type mailboxTickMsg struct{}

// fetchOOFCmd hits the MailboxClient and returns oofLoadedMsg.
func (m Model) fetchOOFCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := m.deps.Mailbox.Get(ctx)
		return oofLoadedMsg{Settings: s, Err: err}
	}
}

// mailboxAutoRefreshCmd schedules a background mailbox fetch after interval.
func (m Model) mailboxAutoRefreshCmd(interval time.Duration) tea.Cmd {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return tea.Tick(interval, func(_ time.Time) tea.Msg { return mailboxTickMsg{} })
}

// doMailboxRefreshCmd fetches mailbox settings and returns mailboxRefreshedMsg.
func (m Model) doMailboxRefreshCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := m.deps.Mailbox.Get(ctx)
		return mailboxRefreshedMsg{Settings: s, Err: err}
	}
}

// toggleOOFCmd PATCHes /me/mailboxSettings with the full MailboxSettings.
func (m Model) toggleOOFCmd(s MailboxSettings) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := m.deps.Mailbox.SetAutoReply(ctx, s)
		if err != nil {
			return oofToggledMsg{Err: err}
		}
		return oofToggledMsg{Settings: &s}
	}
}

// calendarFetchedMsg is the result of the :cal Cmd. Either Events is
// populated or Err is set.
type calendarFetchedMsg struct {
	Events []CalendarEvent
	Err    error
}

// calendarSidebarLoadedMsg is the result of the sidebar background
// calendar fetch. Events populates the sidebar section in FoldersModel.
type calendarSidebarLoadedMsg struct{ events []CalendarEvent }

// fetchCalendarCmd fetches today's events via the CalendarFetcher.
func (m Model) fetchCalendarCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		es, err := m.deps.Calendar.ListEventsToday(ctx)
		return calendarFetchedMsg{Events: es, Err: err}
	}
}

// fetchCalendarForDateCmd fetches events for the given day [day, day+1)
// via CalendarFetcher.ListEventsBetween.
func (m Model) fetchCalendarForDateCmd(day time.Time) tea.Cmd {
	return func() tea.Msg {
		if m.deps.Calendar == nil {
			return calendarFetchedMsg{Err: fmt.Errorf("calendar not wired")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
		end := start.Add(24 * time.Hour)
		es, err := m.deps.Calendar.ListEventsBetween(ctx, start, end)
		return calendarFetchedMsg{Events: es, Err: err}
	}
}

// calendarSidebarCmd fetches the multi-day window for the sidebar
// calendar section (spec 12). Returns nil when Calendar is not wired.
func (m Model) calendarSidebarCmd() tea.Cmd {
	if m.deps.Calendar == nil {
		return nil
	}
	days := m.deps.CalendarSidebarDays
	if days < 1 {
		days = 1
	}
	tz := m.deps.CalendarTZ
	if tz == nil {
		tz = time.Local
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		now := time.Now().In(tz)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)
		end := today.AddDate(0, 0, days)
		events, _ := m.deps.Calendar.ListEventsBetween(ctx, today.UTC(), end.UTC())
		return calendarSidebarLoadedMsg{events: events}
	}
}

// fetchCalendarForWeekCmd fetches events for the Mon–Sun window
// containing the current viewDate. Used by the `w` week-mode toggle.
func (m Model) fetchCalendarForWeekCmd() tea.Cmd {
	return func() tea.Msg {
		if m.deps.Calendar == nil {
			return calendarFetchedMsg{Err: fmt.Errorf("calendar not wired")}
		}
		tz := m.deps.CalendarTZ
		if tz == nil {
			tz = time.Local
		}
		vd := m.calendar.ViewDate()
		t := vd.In(tz)
		weekday := int(t.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday → 7 so Monday is day 1
		}
		monday := t.AddDate(0, 0, -(weekday - 1))
		monday = time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, tz)
		sunday := monday.AddDate(0, 0, 7)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		events, err := m.deps.Calendar.ListEventsBetween(ctx, monday.UTC(), sunday.UTC())
		return calendarFetchedMsg{Events: events, Err: err}
	}
}

// confirmBulk pops up the confirm modal for a bulk operation. Stores
// the action name in pendingBulk so the ConfirmResult handler knows
// what to dispatch on `y`. Shows the filter pattern and a 5-message
// sample so the user can sanity-check before committing.
func (m Model) confirmBulk(action string, count int) (tea.Model, tea.Cmd) {
	if m.deps.Bulk == nil {
		m.lastError = fmt.Errorf("bulk: not wired")
		return m, nil
	}
	verb := action
	switch action {
	case "soft_delete":
		verb = "delete"
	case "permanent_delete":
		verb = "permanently delete"
	case "mark_read":
		verb = "mark read"
	case "mark_unread":
		verb = "mark unread"
	case "add_category":
		if m.pendingBulkCategory != "" {
			verb = "add category " + m.pendingBulkCategory + " to"
		} else {
			verb = "add category to"
		}
	case "remove_category":
		if m.pendingBulkCategory != "" {
			verb = "remove category " + m.pendingBulkCategory + " from"
		} else {
			verb = "remove category from"
		}
	}
	var sb strings.Builder
	firstLine := fmt.Sprintf("%s %d messages", titleCase(verb), count)
	if m.filterAllFolders && m.filterFolderCount > 1 {
		firstLine += fmt.Sprintf(" across %d folders", m.filterFolderCount)
	}
	sb.WriteString(firstLine + "?")
	if m.filterPattern != "" {
		sb.WriteString("\n\nFilter: " + m.filterPattern)
	}
	n := len(m.list.messages)
	if n > 5 {
		n = 5
	}
	if n > 0 {
		sb.WriteString("\n\nSample:")
		for i := 0; i < n; i++ {
			msg := m.list.messages[i]
			subj := msg.Subject
			if len(subj) > 44 {
				subj = subj[:41] + "..."
			}
			sb.WriteString("\n  " + subj)
		}
		if count > n {
			sb.WriteString(fmt.Sprintf("\n  … and %d more", count-n))
		}
	}
	m.confirm = m.confirm.Ask(sb.String(), "bulk:"+action)
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
	category := m.pendingBulkCategory            // captured before any model mutation
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
		case "mark_unread":
			results, err = m.deps.Bulk.BulkMarkUnread(ctx, accountID, ids)
		case "flag":
			results, err = m.deps.Bulk.BulkFlag(ctx, accountID, ids)
		case "unflag":
			results, err = m.deps.Bulk.BulkUnflag(ctx, accountID, ids)
		case "permanent_delete":
			results, err = m.deps.Bulk.BulkPermanentDelete(ctx, accountID, ids)
		case "add_category":
			results, err = m.deps.Bulk.BulkAddCategory(ctx, accountID, ids, category)
		case "remove_category":
			results, err = m.deps.Bulk.BulkRemoveCategory(ctx, accountID, ids, category)
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

// runBulkMoveCmd fires BulkExecutor.BulkMove for the current filter IDs
// to the user-selected destination folder.
func (m Model) runBulkMoveCmd(destFolderID, destAlias, destLabel string) tea.Cmd {
	if m.deps.Bulk == nil {
		return nil
	}
	ids := append([]string(nil), m.filterIDs...)
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		results, err := m.deps.Bulk.BulkMove(ctx, accountID, ids, destFolderID, destAlias)
		var ok, fail int
		var firstErr error
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
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return bulkDoneMsg{name: "move to " + destLabel, succeeded: ok, failed: fail, firstErr: firstErr}
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
			// keypress isn't visually silent.
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
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "c":
		if m.deps.Calendar == nil {
			m.lastError = fmt.Errorf("calendar not wired")
			return m, nil
		}
		m.calendar.Reset()
		m.calendar.SetLoading()
		m.mode = CalendarMode
		return m, m.fetchCalendarCmd()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "e":
		if ss, ok := m.folders.SelectedSavedSearch(); ok {
			return m.startRuleEdit(ss)
		}
	case key.Matches(msg, m.keymap.Open), key.Matches(msg, m.keymap.Right):
		// Calendar event row: open detail modal.
		if ev, ok := m.folders.SelectedCalendarEvent(); ok {
			if ev.ID != "" && m.deps.Calendar != nil {
				m.calendarDetail.SetLoading()
				m.calDetailPriorMode = m.mode
				m.mode = CalendarDetailMode
				return m, m.fetchEventCmd(ev.ID)
			}
			return m, nil
		}
		// Muted Threads virtual folder (spec 19 §5.4).
		if m.folders.SelectedMuted() {
			m.list.FolderID = mutedSentinelID
			m.list.ResetLimit()
			m.focused = ListPane
			m.searchActive = false
			m.searchQuery = ""
			return m, m.loadMutedMessagesCmd()
		}
		// Routing virtual folders — Imbox / Feed / Paper Trail /
		// Screener (spec 23 §5.4). Same shape as muted: select the
		// sentinel ID, reset limit, focus the list pane, dispatch
		// the routing-scoped load.
		if dest := m.folders.SelectedStream(); dest != "" {
			m.list.FolderID = streamSentinelIDForDestination(dest)
			m.list.ResetLimit()
			m.focused = ListPane
			m.searchActive = false
			m.searchQuery = ""
			return m, m.loadByRoutingCmd(dest)
		}
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
	case key.Matches(msg, m.keymap.Move):
		// Spec 15 §9 / PR 7-iii: `m` from the folders pane is "new
		// message". The list pane keeps `m` for the move-with-folder-
		// picker (existing behaviour). Pane scope resolves the
		// collision the same way `r` is reply in the viewer / mark-
		// read in the list.
		if m.deps.Drafts != nil {
			return m.startComposeNew()
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
		case "D":
			return m.confirmBulk("permanent_delete", len(m.filterIDs))
		case "a":
			return m.confirmBulk("archive", len(m.filterIDs))
		case "r":
			return m.confirmBulk("mark_read", len(m.filterIDs))
		case "R":
			return m.confirmBulk("mark_unread", len(m.filterIDs))
		case "f":
			return m.confirmBulk("flag", len(m.filterIDs))
		case "F":
			return m.confirmBulk("unflag", len(m.filterIDs))
		case "c":
			m.pendingBulkCategoryAction = "add_category"
			m.pendingCategoryMsg = nil
			m.categoryBuf = ""
			m.mode = CategoryInputMode
			return m, nil
		case "C":
			m.pendingBulkCategoryAction = "remove_category"
			m.pendingCategoryMsg = nil
			m.categoryBuf = ""
			m.mode = CategoryInputMode
			return m, nil
		case "m":
			// Bulk move: open folder picker; on Enter dispatch BulkMove.
			if m.deps.Bulk == nil {
				m.lastError = fmt.Errorf(";m: bulk not wired")
				return m, nil
			}
			folders := m.folders.raw
			if len(folders) == 0 {
				m.lastError = fmt.Errorf(";m: no folders synced yet")
				return m, nil
			}
			m.pendingBulkMove = true
			m.folderPicker.Reset(folders, m.recentFolderIDs)
			m.mode = FolderPickerMode
			return m, nil
		}
		// Unknown chord follow-up: clear pending, fall through.
	}
	// S chord: routing destination assignment (spec 23 §5.1). Active
	// in the list pane only here; viewer pane has its own dispatch.
	// Cross-chord cancel: an S press while threadChordPending cancels
	// the thread chord without starting the stream chord (§5.1).
	if key.Matches(msg, m.keymap.StreamChord) && !m.streamChordPending {
		if m.threadChordPending {
			m.threadChordPending = false
			m.engineActivity = "thread chord cancelled"
			return m, nil
		}
		if _, ok := m.list.Selected(); !ok {
			m.lastError = fmt.Errorf("stream: no message selected")
			return m, nil
		}
		mm, cmd := m.startStreamChord()
		return mm, cmd
	}
	if m.streamChordPending {
		sel, ok := m.list.Selected()
		var focused *store.Message
		if ok {
			s := sel
			focused = &s
		}
		mm, cmd := m.dispatchStreamChord(msg, focused)
		return mm, cmd
	}
	// T chord: thread-level operations (spec 20).
	if msg.Type == tea.KeyRunes && string(msg.Runes) == "T" && !m.threadChordPending {
		// Cross-chord cancel: T while stream-pending cancels stream
		// chord without entering thread chord (handled above by
		// streamChordPending dispatching to dispatchStreamChord
		// which catches 'T' as a cancel — this branch only fires
		// when streamChordPending is false, so it's safe).
		if _, ok := m.list.Selected(); !ok {
			m.lastError = fmt.Errorf("thread: no message selected")
			return m, nil
		}
		m.threadChordToken++
		m.threadChordPending = true
		m.engineActivity = "thread: r/R/f/F/d/D/a/m  esc cancel"
		return m, threadChordTimeout(m.threadChordToken)
	}
	if m.threadChordPending {
		m.threadChordPending = false
		m.engineActivity = ""
		if msg.Type == tea.KeyEsc {
			m.engineActivity = "thread chord cancelled"
			return m, nil
		}
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		switch string(msg.Runes) {
		case "r":
			return m, m.runThreadExecuteCmd("mark read", store.ActionMarkRead, sel.ID)
		case "R":
			return m, m.runThreadExecuteCmd("mark unread", store.ActionMarkUnread, sel.ID)
		case "f":
			return m, m.runThreadExecuteCmd("flag", store.ActionFlag, sel.ID)
		case "F":
			return m, m.runThreadExecuteCmd("unflag", store.ActionUnflag, sel.ID)
		case "a":
			return m, m.runThreadMoveCmd("archive", sel.ID, "", "archive")
		case "d":
			return m, m.threadPreFetchCmd("soft_delete", sel.ID)
		case "D":
			return m, m.threadPreFetchCmd("permanent_delete", sel.ID)
		case "m":
			if m.deps.Thread == nil {
				m.lastError = fmt.Errorf("thread move: not wired")
				return m, nil
			}
			folders := m.folders.raw
			if len(folders) == 0 {
				m.lastError = fmt.Errorf("thread move: no folders synced yet")
				return m, nil
			}
			m.pendingThreadMove = true
			m.pendingMoveMsg = &sel
			m.folderPicker.Reset(folders, m.recentFolderIDs)
			m.mode = FolderPickerMode
			return m, nil
		}
		// Unrecognised second key — cancel silently.
		return m, nil
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
	case key.Matches(msg, m.keymap.MuteThread):
		return m.startMute()
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
	case key.Matches(msg, m.keymap.Move):
		sel, ok := m.list.Selected()
		if !ok {
			return m, nil
		}
		return m.startMove(sel)
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
		m.pendingBulkCategoryAction = ""
		m.categoryBuf = ""
		m.engineActivity = "category input cancelled"
		return m, nil
	case tea.KeyEnter:
		cat := strings.TrimSpace(m.categoryBuf)
		if cat == "" {
			m.mode = NormalMode
			m.pendingCategoryAction = ""
			m.pendingCategoryMsg = nil
			m.pendingBulkCategoryAction = ""
			return m, nil
		}
		// Bulk path: ;c / ;C entered a category name — confirm before applying.
		if m.pendingBulkCategoryAction != "" {
			bulkAction := m.pendingBulkCategoryAction
			m.pendingBulkCategoryAction = ""
			m.categoryBuf = ""
			m.mode = NormalMode
			m.pendingBulkCategory = cat
			return m.confirmBulk(bulkAction, len(m.filterIDs))
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

// startMove opens the spec 07 §6.5 / §12.1 folder picker for a
// move action. The picker rebuilds its row list from the current
// FoldersModel raw list + the session MRU each time it opens so
// freshly-synced folders surface without a refresh, and recent
// destinations always appear above the alphabetical section.
func (m Model) startMove(src store.Message) (tea.Model, tea.Cmd) {
	if m.deps.Triage == nil {
		m.lastError = fmt.Errorf("move: not wired (run from cmd_run.go path)")
		return m, nil
	}
	folders := m.folders.raw
	if len(folders) == 0 {
		m.lastError = fmt.Errorf("move: no folders synced yet")
		return m, nil
	}
	m.pendingMoveMsg = &src
	m.folderPicker.Reset(folders, m.recentFolderIDs)
	m.mode = FolderPickerMode
	return m, nil
}

// updatePalette handles input while the spec 22 Ctrl+K command
// palette overlay is open. Esc closes; Enter dispatches the
// highlighted row's RunFn (or ArgFn for NeedsArg rows); Tab always
// defers to ArgFn; ↑/↓/Ctrl+P/Ctrl+N navigate; Backspace narrows
// (no-op at empty buffer per spec); typed runes append to the
// buffer and refilter.
func (m Model) updatePalette(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch {
	case keyMsg.Type == tea.KeyEsc:
		m.mode = NormalMode
		return m, nil

	case keyMsg.Type == tea.KeyEnter:
		sel := m.palette.Selected()
		m.mode = NormalMode
		if sel == nil {
			return m, nil
		}
		m.palette.recordRecent(sel.ID)
		if !sel.Available.OK {
			m.lastError = fmt.Errorf("%s", sel.Available.Why)
			return m, nil
		}
		if sel.NeedsArg && sel.ArgFn != nil {
			return sel.ArgFn(m)
		}
		if sel.RunFn == nil {
			return m, nil
		}
		return sel.RunFn(m)

	case keyMsg.Type == tea.KeyTab:
		sel := m.palette.Selected()
		m.mode = NormalMode
		if sel == nil {
			return m, nil
		}
		m.palette.recordRecent(sel.ID)
		if sel.ArgFn != nil {
			return sel.ArgFn(m)
		}
		if !sel.Available.OK {
			m.lastError = fmt.Errorf("%s", sel.Available.Why)
			return m, nil
		}
		if sel.RunFn == nil {
			return m, nil
		}
		return sel.RunFn(m)

	case keyMsg.Type == tea.KeyUp, keyMsg.String() == "ctrl+p":
		m.palette.Up()
		return m, nil

	case keyMsg.Type == tea.KeyDown, keyMsg.String() == "ctrl+n":
		m.palette.Down()
		return m, nil

	case keyMsg.Type == tea.KeyBackspace:
		m.palette.Backspace()
		return m, nil

	case keyMsg.Type == tea.KeyRunes:
		m.palette.AppendRunes(keyMsg.Runes)
		return m, nil

	case keyMsg.Type == tea.KeySpace:
		m.palette.AppendRunes([]rune{' '})
		return m, nil
	}
	return m, nil
}

// updateFolderPicker handles input while the move-picker overlay
// is open. Up/Down navigate (arrows only — letters flow into the
// filter buffer); Enter dispatches the move; Esc cancels.
func (m Model) updateFolderPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.Type {
	case tea.KeyEsc:
		m.mode = NormalMode
		m.pendingMoveMsg = nil
		m.pendingBulkMove = false
		m.pendingThreadMove = false
		m.engineActivity = "move cancelled"
		return m, nil
	case tea.KeyUp:
		m.folderPicker.Up()
		return m, nil
	case tea.KeyDown:
		m.folderPicker.Down()
		return m, nil
	case tea.KeyEnter:
		row := m.folderPicker.Selected()
		if row == nil {
			m.engineActivity = "move: no folder selected"
			return m, nil
		}
		destID := row.id
		destAlias := row.alias
		destLabel := row.label
		m.mode = NormalMode
		m.recentFolderIDs = bumpRecentFolder(m.recentFolderIDs, destID, m.recentFoldersCap())
		// Bulk move (;m): move all filter matches to the chosen folder.
		if m.pendingBulkMove {
			m.pendingBulkMove = false
			return m, m.runBulkMoveCmd(destID, destAlias, destLabel)
		}
		// Thread move (T m): move entire conversation to the chosen folder.
		if m.pendingThreadMove {
			m.pendingThreadMove = false
			if m.pendingMoveMsg == nil {
				m.engineActivity = "thread move: no message"
				return m, nil
			}
			focusedID := m.pendingMoveMsg.ID
			m.pendingMoveMsg = nil
			return m, m.runThreadMoveCmd("move", focusedID, destID, destAlias)
		}
		// Single-message move (m key).
		if m.pendingMoveMsg == nil {
			return m, nil
		}
		src := *m.pendingMoveMsg
		m.pendingMoveMsg = nil
		return m.runTriage("move", src, ListPane, func(ctx context.Context, accID int64, s store.Message) error {
			if err := m.deps.Triage.Move(ctx, accID, s.ID, destID, destAlias); err != nil {
				return fmt.Errorf("move to %s: %w", destLabel, err)
			}
			return nil
		})
	case tea.KeyBackspace:
		m.folderPicker.Backspace()
		return m, nil
	}
	if keyMsg.Type == tea.KeyRunes {
		for _, r := range keyMsg.Runes {
			m.folderPicker.AppendRune(r)
		}
		return m, nil
	}
	if keyMsg.Type == tea.KeySpace {
		m.folderPicker.AppendRune(' ')
		return m, nil
	}
	return m, nil
}

// recentFoldersCap returns the configured MRU cap for the move
// picker. Falls back to 5 when unset (matches CONFIG.md default).
func (m Model) recentFoldersCap() int {
	if m.deps.RecentFoldersCount > 0 {
		return m.deps.RecentFoldersCount
	}
	return 5
}

// bumpRecentFolder promotes id to the front of recents (or inserts
// it if absent), capped at max. A cap of 0 disables recents entirely.
func bumpRecentFolder(recents []string, id string, max int) []string {
	if max <= 0 || id == "" {
		return recents
	}
	out := make([]string, 0, max)
	out = append(out, id)
	for _, r := range recents {
		if r == id {
			continue
		}
		if len(out) >= max {
			break
		}
		out = append(out, r)
	}
	return out
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

// startMute dispatches muteCmd for the focused message. Works from
// both the list pane and the viewer pane (spec 19 §5.1). Surfaces a
// status-bar error when no message is focused or the message has no
// conversation_id.
func (m Model) startMute() (tea.Model, tea.Cmd) {
	var msg *store.Message
	if cur := m.viewer.current; cur != nil && m.focused == ViewerPane {
		msg = cur
	} else if sel, ok := m.list.Selected(); ok {
		msg = &sel
	}
	if msg == nil {
		m.lastError = fmt.Errorf("mute: no message focused")
		return m, nil
	}
	if msg.ConversationID == "" {
		m.lastError = fmt.Errorf("mute: message has no conversation ID")
		return m, nil
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return m, muteCmd(m.deps.Store, accountID, msg.ConversationID, msg.Subject)
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

// mutedToastMsg is the result of muteCmd. nowMuted=true on mute, false on
// unmute. The subject is used for the status-bar toast (not logged).
type mutedToastMsg struct {
	subject  string
	nowMuted bool
	err      error
}

// muteCmd toggles mute on a conversation. It checks the current mute
// state and applies the inverse, then returns a mutedToastMsg.
// ctx must be the background context threaded via model's cancel field —
// Bubble Tea Cmd goroutines must not capture a context from Update.
func muteCmd(st store.Store, accountID int64, conversationID, subject string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		muted, err := st.IsConversationMuted(ctx, accountID, conversationID)
		if err != nil {
			return mutedToastMsg{err: err}
		}
		if muted {
			if err := st.UnmuteConversation(ctx, accountID, conversationID); err != nil {
				return mutedToastMsg{err: err}
			}
			return mutedToastMsg{subject: subject, nowMuted: false}
		}
		if err := st.MuteConversation(ctx, accountID, conversationID); err != nil {
			return mutedToastMsg{err: err}
		}
		return mutedToastMsg{subject: subject, nowMuted: true}
	}
}

// loadMutedMessagesCmd loads messages for the "Muted Threads" virtual
// folder (spec 19 §5.4). Returns a MessagesLoadedMsg with FolderID
// set to mutedSentinelID so the list pane can identify the view.
func (m Model) loadMutedMessagesCmd() tea.Cmd {
	limit := m.list.LoadLimit()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		msgs, err := m.deps.Store.ListMutedMessages(ctx, accountID, limit)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MessagesLoadedMsg{FolderID: mutedSentinelID, Messages: msgs}
	}
}

// refreshMutedCountCmd queries the muted-conversation count and returns
// a mutedCountUpdatedMsg so the sidebar badge stays accurate.
func (m Model) refreshMutedCountCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		n, err := m.deps.Store.CountMutedConversations(ctx, accountID)
		if err != nil {
			return nil // non-fatal; badge just won't refresh
		}
		return mutedCountUpdatedMsg{count: n}
	}
}

// mutedCountUpdatedMsg carries the fresh muted-conversation count for
// the sidebar badge (spec 19 §5.4).
type mutedCountUpdatedMsg struct{ count int }

// threadChordTimeoutMsg cancels an in-progress T<verb> chord when
// no second key arrives within the timeout window. The token field
// allows stale timeout messages to be discarded.
type threadChordTimeoutMsg struct{ token uint64 }

// threadChordTimeout returns a Cmd that fires threadChordTimeoutMsg
// after 3 seconds if no second key is pressed.
func threadChordTimeout(token uint64) tea.Cmd {
	return func() tea.Msg {
		<-time.After(3 * time.Second)
		return threadChordTimeoutMsg{token: token}
	}
}

// threadOpDoneMsg is the result of a thread-level batch operation.
type threadOpDoneMsg struct {
	verb      string
	total     int
	succeeded int
	failed    int
	firstErr  error
}

// threadPreFetchDoneMsg carries the pre-fetched thread IDs for T d / T D.
type threadPreFetchDoneMsg struct {
	action string // "soft_delete" or "permanent_delete"
	ids    []string
	err    error
}

// runThreadExecuteCmd dispatches a verb over all messages in the
// focused message's conversation via ThreadExecutor.ThreadExecute.
func (m Model) runThreadExecuteCmd(verb string, actionType store.ActionType, focusedMsgID string) tea.Cmd {
	if m.deps.Thread == nil {
		return nil
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		total, results, err := m.deps.Thread.ThreadExecute(ctx, accountID, actionType, focusedMsgID)
		var ok, fail int
		var firstErr error
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
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return threadOpDoneMsg{verb: verb, total: total, succeeded: ok, failed: fail, firstErr: firstErr}
	}
}

// runThreadMoveCmd dispatches a bulk-move over all messages in the
// focused message's conversation via ThreadExecutor.ThreadMove.
func (m Model) runThreadMoveCmd(verb, focusedMsgID, destFolderID, destAlias string) tea.Cmd {
	if m.deps.Thread == nil {
		return nil
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		total, results, err := m.deps.Thread.ThreadMove(ctx, accountID, focusedMsgID, destFolderID, destAlias)
		var ok, fail int
		var firstErr error
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
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return threadOpDoneMsg{verb: verb, total: total, succeeded: ok, failed: fail, firstErr: firstErr}
	}
}

// threadPreFetchCmd fetches message IDs for a conversation so the
// T d / T D confirm flow has the count before showing the modal.
func (m Model) threadPreFetchCmd(action, focusedMsgID string) tea.Cmd {
	if m.deps.Store == nil {
		return nil
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		msg, err := m.deps.Store.GetMessage(ctx, focusedMsgID)
		if err != nil {
			return threadPreFetchDoneMsg{action: action, err: err}
		}
		if msg.ConversationID == "" {
			return threadPreFetchDoneMsg{action: action, err: fmt.Errorf("thread: no conversation id")}
		}
		ids, err := m.deps.Store.MessageIDsInConversation(ctx, accountID, msg.ConversationID, false)
		return threadPreFetchDoneMsg{action: action, ids: ids, err: err}
	}
}

// focusedMessageID returns the ID of the currently focused message —
// viewer.current when the viewer pane is active, list selection otherwise.
func (m Model) focusedMessageID() string {
	if cur := m.viewer.current; cur != nil && m.focused == ViewerPane {
		return cur.ID
	}
	if sel, ok := m.list.Selected(); ok {
		return sel.ID
	}
	return ""
}

// runBulkWithIDsCmd executes BulkSoftDelete or BulkPermanentDelete on a
// pre-fetched slice of message IDs. Used by the T d / T D confirm path.
func (m Model) runBulkWithIDsCmd(action string, ids []string) tea.Cmd {
	if m.deps.Bulk == nil {
		return nil
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var results []BulkResult
		var err error
		switch action {
		case "soft_delete":
			results, err = m.deps.Bulk.BulkSoftDelete(ctx, accountID, ids)
		case "permanent_delete":
			results, err = m.deps.Bulk.BulkPermanentDelete(ctx, accountID, ids)
		default:
			return threadOpDoneMsg{verb: action, firstErr: fmt.Errorf("unknown action %q", action)}
		}
		var ok, fail int
		var firstErr error
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
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return threadOpDoneMsg{verb: action, total: len(ids), succeeded: ok, failed: fail, firstErr: firstErr}
	}
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
// BodyRenderedMsg so the viewer pane can refresh. Attachments are
// loaded from the local cache after the body resolves (the renderer's
// FetchBodyAsync persisted them on the same call) so the viewer's
// "Attachments:" block paints alongside the body without an extra
// Graph round-trip.
func (m Model) openMessageCmd(msg store.Message) tea.Cmd {
	if m.deps.Renderer == nil {
		return nil
	}
	r := m.deps.Renderer
	st := m.deps.Store
	accID := int64(0)
	if m.deps.Account != nil {
		accID = m.deps.Account.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// URLDisplayMaxWidth truncates long URLs in the body so they
		// don't dominate vertical space. Cmd-click + `O` (URL picker)
		// + the trailing `Links:` block all retain the full URL.
		// Threaded from `[rendering].url_display_max_width` via Deps;
		// 0 disables truncation explicitly. The production wire-up
		// in cmd_run.go forwards the config value (default 60).
		viewerW := m.width - m.paneWidths.Folders - m.paneWidths.List
		if viewerW < 20 {
			viewerW = 20
		}
		if m.deps.WrapColumns > 0 {
			viewerW = m.deps.WrapColumns
		}
		opts := render.BodyOpts{
			Width:              viewerW,
			Theme:              m.theme.RenderTheme,
			URLDisplayMaxWidth: m.deps.URLDisplayMaxWidth,
		}
		loadAtts := func() []store.Attachment {
			if !msg.HasAttachments || st == nil {
				return nil
			}
			atts, err := st.ListAttachments(ctx, msg.ID)
			if err != nil {
				return nil
			}
			return atts
		}
		// Spec 05 §11: load conversation siblings from local store.
		// No Graph call — offline-safe. Cap at 50 so deep threads
		// don't flood the thread-map section.
		loadConv := func() []store.Message {
			if msg.ConversationID == "" || st == nil {
				return nil
			}
			conv, err := st.ListMessages(ctx, store.MessageQuery{
				AccountID:      accID,
				ConversationID: msg.ConversationID,
				OrderBy:        store.OrderReceivedAsc,
				Limit:          50,
			})
			if err != nil {
				return nil
			}
			return conv
		}
		view, err := r.Body(ctx, &msg, opts)
		if err != nil {
			return BodyRenderedMsg{MessageID: msg.ID, Text: "render error: " + err.Error(), State: int(render.BodyError)}
		}
		if view.State == render.BodyReady {
			return BodyRenderedMsg{MessageID: msg.ID, Text: view.Text, TextExpanded: view.TextExpanded, Links: convertLinks(view.Links), State: int(view.State), Attachments: loadAtts(), Conversation: loadConv(), RawHeaders: convertHeaders(view.Headers)}
		}
		// BodyFetching: dispatch the fetch synchronously inside this
		// goroutine and return the final rendered view.
		if f, ok := r.(bodyAsyncFetcher); ok {
			final, err := f.FetchBodyAsync(ctx, &msg, opts)
			if err != nil {
				return BodyRenderedMsg{MessageID: msg.ID, Text: "fetch error: " + err.Error(), State: int(render.BodyError)}
			}
			return BodyRenderedMsg{MessageID: msg.ID, Text: final.Text, TextExpanded: final.TextExpanded, Links: convertLinks(final.Links), State: int(final.State), Attachments: loadAtts(), Conversation: loadConv(), RawHeaders: convertHeaders(final.Headers)}
		}
		return BodyRenderedMsg{MessageID: msg.ID, Text: view.Text, TextExpanded: view.TextExpanded, Links: convertLinks(view.Links), State: int(view.State), Attachments: loadAtts(), Conversation: loadConv(), RawHeaders: convertHeaders(view.Headers)}
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

func convertHeaders(in []render.FetchedHeader) []RawHeader {
	if len(in) == 0 {
		return nil
	}
	out := make([]RawHeader, len(in))
	for i, h := range in {
		out[i] = RawHeader{Name: h.Name, Value: h.Value}
	}
	return out
}

func (m Model) dispatchViewer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Attachment accelerator letters (spec 05 §12 / PR 10).
	// lowercase a-z → save attachment [letter] if one exists at that
	// index; otherwise falls through to the regular keybinding switch.
	// Uppercase A-Z → open attachment [letter] via default OS app,
	// skipping letters reserved by other viewer bindings (H=headers,
	// D=permanent-delete, C=remove-category, R=reply-all, U=unsubscribe).
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		r := msg.Runes[0]
		atts := m.viewer.Attachments()
		switch {
		case r >= 'a' && r <= 'z':
			if idx := int(r - 'a'); idx < len(atts) {
				return m.startSaveAttachment(atts[idx])
			}
		case r >= 'A' && r <= 'Z':
			// H/D/C/R/U are reserved — fall through to the regular switch.
			reserved := r == 'H' || r == 'D' || r == 'C' || r == 'R' || r == 'U'
			if idx := int(r - 'A'); !reserved && idx < len(atts) {
				return m.startOpenAttachment(atts[idx])
			}
		}
	}

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
		// Spec 15 §9 / PR 7-iii: `f` from the viewer pane is
		// Forward (mutt convention). The list pane keeps `f` for
		// flag-toggle; users still flag from the list. When Drafts
		// isn't wired (e.g., test setup without a draft creator),
		// fall back to flag so the binding isn't visually dead.
		if cur := m.viewer.current; cur != nil {
			if m.deps.Drafts != nil {
				return m.startComposeForward(*cur)
			}
			return m.runTriage("toggle_flag", *cur, ViewerPane, func(ctx context.Context, accID int64, src store.Message) error {
				return m.deps.Triage.ToggleFlag(ctx, accID, src.ID, src.FlagStatus == "flagged")
			})
		}
	case key.Matches(msg, m.keymap.MarkUnread):
		// Spec 15 §9 / PR 7-iii: `R` from the viewer pane is
		// Reply All. The list pane keeps `R` for mark-unread; the
		// folders pane keeps `R` for rename-folder (separate
		// pane-scoped meaning).
		if cur := m.viewer.current; cur != nil && m.deps.Drafts != nil {
			return m.startComposeReplyAll(*cur)
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
		// Spec 15 F-1: when a draft was just saved, 'D' discards it
		// (DELETE /me/messages/{id}) rather than permanently deleting
		// the focused message. This avoids destroying the source message
		// when the user means "I changed my mind about this draft".
		if m.lastDraftID != "" {
			m.pendingDiscardDraftID = m.lastDraftID
			m.confirm = m.confirm.Ask("Discard saved draft?", "discard_draft")
			m.mode = ConfirmMode
			return m, nil
		}
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
		// Spec 15 v2: opens the in-modal compose pane instead of
		// the legacy tempfile + $EDITOR flow. $EDITOR drop-out for
		// power users lands as a follow-up via Ctrl+E.
		if cur := m.viewer.current; cur != nil && m.deps.Drafts != nil {
			return m.startCompose(*cur)
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
	case key.Matches(msg, m.keymap.MuteThread):
		return m.startMute()
	case key.Matches(msg, m.keymap.AddCategory):
		if cur := m.viewer.current; cur != nil {
			return m.startCategoryInput("add", *cur)
		}
	case key.Matches(msg, m.keymap.RemoveCategory):
		if cur := m.viewer.current; cur != nil {
			return m.startCategoryInput("remove", *cur)
		}
	case key.Matches(msg, m.keymap.Move):
		// Spec 15 §9 / PR 7-iii: `m` from the viewer pane is
		// "new message" (parity with folders pane). The list pane
		// keeps `m` for move-with-folder-picker. When Drafts isn't
		// wired, fall back to startMove so the binding isn't dead.
		if cur := m.viewer.current; cur != nil {
			if m.deps.Drafts != nil {
				return m.startComposeNew()
			}
			return m.startMove(*cur)
		}
	// Spec 05 §12 / PR 10: `o` (lowercase) opens the current message's
	// webLink in the system browser (OWA deep link). Fast "escape
	// hatch" for unreadable CSS-heavy emails.
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "o":
		if cur := m.viewer.current; cur != nil {
			if cur.WebLink != "" {
				go openInBrowser(cur.WebLink)
				m.engineActivity = "opening in browser…"
			} else {
				m.lastError = fmt.Errorf("open: no webLink for this message")
			}
		}

	// Spec 05 §12 / PR 10: `[` navigates to the previous message in
	// the conversation thread; `]` to the next.
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "[":
		if prev := m.viewer.NavPrevInThread(); prev != nil {
			m.viewer.SetMessage(*prev)
			return m, m.openMessageCmd(*prev)
		}

	case msg.Type == tea.KeyRunes && string(msg.Runes) == "]":
		if next := m.viewer.NavNextInThread(); next != nil {
			m.viewer.SetMessage(*next)
			return m, m.openMessageCmd(*next)
		}

	case key.Matches(msg, m.keymap.OpenURL):
		// Spec 05 §12 / PR 10: `O` (capital O) opens the URL picker
		// overlay so the user can select a numbered link. Was `o` in
		// v0.15.x; now `o` is the webLink fast-open and `O` is the
		// picker (matches spec §12 table: O = open focused link).
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
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "Q":
		// Q toggles quote expansion (show/hide collapsed quoted blocks).
		m.viewer.ToggleQuotes()
		return m, nil
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "e":
		// e also toggles quote expansion (alternative binding).
		m.viewer.ToggleQuotes()
		return m, nil
	}
	// S chord in viewer pane (spec 23 §5.1).
	if key.Matches(msg, m.keymap.StreamChord) && !m.streamChordPending {
		if m.threadChordPending {
			m.threadChordPending = false
			m.engineActivity = "thread chord cancelled"
			return m, nil
		}
		if m.viewer.current == nil {
			m.lastError = fmt.Errorf("stream: no message open")
			return m, nil
		}
		mm, cmd := m.startStreamChord()
		return mm, cmd
	}
	if m.streamChordPending {
		mm, cmd := m.dispatchStreamChord(msg, m.viewer.current)
		return mm, cmd
	}
	// T chord in viewer pane mirrors list pane (spec 20).
	if msg.Type == tea.KeyRunes && string(msg.Runes) == "T" && !m.threadChordPending {
		cur := m.viewer.current
		if cur == nil {
			m.lastError = fmt.Errorf("thread: no message open")
			return m, nil
		}
		m.threadChordToken++
		m.threadChordPending = true
		m.engineActivity = "thread: r/R/f/F/d/D/a/m  esc cancel"
		return m, threadChordTimeout(m.threadChordToken)
	}
	if m.threadChordPending {
		m.threadChordPending = false
		m.engineActivity = ""
		if msg.Type == tea.KeyEsc {
			m.engineActivity = "thread chord cancelled"
			return m, nil
		}
		cur := m.viewer.current
		if cur == nil {
			return m, nil
		}
		switch string(msg.Runes) {
		case "r":
			return m, m.runThreadExecuteCmd("mark read", store.ActionMarkRead, cur.ID)
		case "R":
			return m, m.runThreadExecuteCmd("mark unread", store.ActionMarkUnread, cur.ID)
		case "f":
			return m, m.runThreadExecuteCmd("flag", store.ActionFlag, cur.ID)
		case "F":
			return m, m.runThreadExecuteCmd("unflag", store.ActionUnflag, cur.ID)
		case "a":
			return m, m.runThreadMoveCmd("archive", cur.ID, "", "archive")
		case "d":
			return m, m.threadPreFetchCmd("soft_delete", cur.ID)
		case "D":
			return m, m.threadPreFetchCmd("permanent_delete", cur.ID)
		case "m":
			if m.deps.Thread == nil {
				m.lastError = fmt.Errorf("thread move: not wired")
				return m, nil
			}
			folders := m.folders.raw
			if len(folders) == 0 {
				m.lastError = fmt.Errorf("thread move: no folders synced yet")
				return m, nil
			}
			m.pendingThreadMove = true
			m.pendingMoveMsg = cur
			m.folderPicker.Reset(folders, m.recentFolderIDs)
			m.mode = FolderPickerMode
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

// safeAttachmentPath resolves name inside dir and rejects path-traversal
// attempts (spec 17 §4.4). filepath.Base strips any directory prefix so
// `../evil.sh` becomes `evil.sh`; then the cleaned join is verified to
// stay within dir using the trailing-separator prefix check to prevent
// false positives like dir="/foo" matching clean="/foobar/x".
// expandHomePath replaces a leading "~" with os.UserHomeDir().
func expandHomePath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

func safeAttachmentPath(dir, name string) (string, error) {
	base := filepath.Base(name)
	if base == "." || base == ".." {
		return "", fmt.Errorf("attachment: unsafe name %q", name)
	}
	clean := filepath.Clean(filepath.Join(dir, base))
	sep := string(filepath.Separator)
	if !strings.HasPrefix(clean+sep, dir+sep) {
		return "", fmt.Errorf("attachment: name %q escapes save directory", name)
	}
	return clean, nil
}

// startSaveAttachment begins the save flow for an attachment by letter
// (spec 05 §12 / PR 10). Large files (>LargeAttachmentWarnMB) get a
// confirm modal first; small files dispatch the download cmd directly.
func (m Model) startSaveAttachment(att store.Attachment) (tea.Model, tea.Cmd) {
	if m.deps.Attachments == nil {
		m.lastError = fmt.Errorf("save attachment: not wired (run from cmd_run.go path)")
		return m, nil
	}
	const bytesPerMB = 1024 * 1024
	if warnMB := m.deps.LargeAttachmentWarnMB; warnMB > 0 && att.Size > int64(warnMB)*bytesPerMB {
		prompt := fmt.Sprintf(
			"Large attachment: %s (%.1f MB)\n\nSave to %s?\n\n[y]es / [N]o",
			att.Name, float64(att.Size)/float64(bytesPerMB),
			m.deps.AttachmentSaveDir,
		)
		m.pendingAttachmentSave = &att
		m.confirm = m.confirm.Ask(prompt, "large_attachment_save")
		m.mode = ConfirmMode
		return m, nil
	}
	m.engineActivity = fmt.Sprintf("saving %s…", att.Name)
	return m, m.saveAttachmentCmd(att)
}

// startOpenAttachment begins the open flow for an attachment by letter
// (spec 05 §12 / PR 10). Large files get a confirm modal first.
func (m Model) startOpenAttachment(att store.Attachment) (tea.Model, tea.Cmd) {
	if m.deps.Attachments == nil {
		m.lastError = fmt.Errorf("open attachment: not wired (run from cmd_run.go path)")
		return m, nil
	}
	const bytesPerMB = 1024 * 1024
	if warnMB := m.deps.LargeAttachmentWarnMB; warnMB > 0 && att.Size > int64(warnMB)*bytesPerMB {
		prompt := fmt.Sprintf(
			"Large attachment: %s (%.1f MB)\n\nDownload and open?\n\n[y]es / [N]o",
			att.Name, float64(att.Size)/float64(bytesPerMB),
		)
		m.pendingAttachmentOpen = &att
		m.confirm = m.confirm.Ask(prompt, "large_attachment_open")
		m.mode = ConfirmMode
		return m, nil
	}
	m.engineActivity = fmt.Sprintf("opening %s…", att.Name)
	return m, m.openAttachmentCmd(att)
}

// saveAttachmentCmd downloads attachment bytes and writes them to
// AttachmentSaveDir / att.Name (spec 05 §8.1). The path is validated
// by safeAttachmentPath before writing (spec 17 §4.4).
func (m Model) saveAttachmentCmd(att store.Attachment) tea.Cmd {
	af := m.deps.Attachments
	if af == nil {
		return nil
	}
	msgID := m.viewer.CurrentMessageID()
	saveDir := m.deps.AttachmentSaveDir
	if saveDir == "" {
		home, _ := os.UserHomeDir()
		saveDir = filepath.Join(home, "Downloads")
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		data, err := af.GetAttachment(ctx, msgID, att.ID)
		if err != nil {
			return SaveAttachmentDoneMsg{Name: att.Name, Err: err}
		}
		savePath, err := safeAttachmentPath(saveDir, att.Name)
		if err != nil {
			return SaveAttachmentDoneMsg{Name: att.Name, Err: err}
		}
		if err := os.MkdirAll(filepath.Dir(savePath), 0o700); err != nil {
			return SaveAttachmentDoneMsg{Name: att.Name, Err: fmt.Errorf("mkdir: %w", err)}
		}
		// #nosec G306 — 0o600 is intentionally restrictive: attachment
		// data is private mail content; world-readable would be a privacy
		// leak (CLAUDE.md §7 rule 1). G304 suppressed by the safeAttachmentPath
		// call above which verifies the path stays within saveDir.
		if err := os.WriteFile(savePath, data, 0o600); err != nil {
			return SaveAttachmentDoneMsg{Name: att.Name, Err: fmt.Errorf("write: %w", err)}
		}
		return SaveAttachmentDoneMsg{Name: att.Name, Path: savePath}
	}
}

// saveAttachmentToPathCmd is the `:save <letter> <path>` variant of
// saveAttachmentCmd (spec 05 §8). The caller supplies an absolute path
// (after expandHome); no safeAttachmentPath check is needed because the
// user explicitly chose the destination via the command bar.
func (m Model) saveAttachmentToPathCmd(att store.Attachment, destPath string) tea.Cmd {
	af := m.deps.Attachments
	if af == nil {
		return nil
	}
	msgID := m.viewer.CurrentMessageID()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		data, err := af.GetAttachment(ctx, msgID, att.ID)
		if err != nil {
			return SaveAttachmentDoneMsg{Name: att.Name, Err: err}
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
			return SaveAttachmentDoneMsg{Name: att.Name, Err: fmt.Errorf("mkdir: %w", err)}
		}
		// #nosec G306 — 0o600: attachment data is private mail content.
		if err := os.WriteFile(destPath, data, 0o600); err != nil {
			return SaveAttachmentDoneMsg{Name: att.Name, Err: fmt.Errorf("write: %w", err)}
		}
		return SaveAttachmentDoneMsg{Name: att.Name, Path: destPath}
	}
}

// openAttachmentCmd downloads attachment bytes to a per-message temp
// directory under ~/Library/Caches/inkwell/attachments/{msgID}/ and
// opens the file with the default OS application (spec 05 §8.1).
func (m Model) openAttachmentCmd(att store.Attachment) tea.Cmd {
	af := m.deps.Attachments
	if af == nil {
		return nil
	}
	msgID := m.viewer.CurrentMessageID()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		data, err := af.GetAttachment(ctx, msgID, att.ID)
		if err != nil {
			return OpenAttachmentDoneMsg{Name: att.Name, Err: err}
		}
		home, _ := os.UserHomeDir()
		tmpDir := filepath.Join(home, "Library", "Caches", "inkwell", "attachments", msgID)
		if err := os.MkdirAll(tmpDir, 0o700); err != nil {
			return OpenAttachmentDoneMsg{Name: att.Name, Err: fmt.Errorf("mkdir: %w", err)}
		}
		tmpPath, err := safeAttachmentPath(tmpDir, att.Name)
		if err != nil {
			return OpenAttachmentDoneMsg{Name: att.Name, Err: err}
		}
		// #nosec G306 — 0o600 for the same reason as saveAttachmentCmd;
		// G304 suppressed by safeAttachmentPath validation above.
		if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
			return OpenAttachmentDoneMsg{Name: att.Name, Err: fmt.Errorf("write: %w", err)}
		}
		openInBrowser(tmpPath) // `open <path>` on macOS opens with default app
		return OpenAttachmentDoneMsg{Name: att.Name}
	}
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
			AccountID:    accountID,
			FolderID:     folderID,
			Limit:        limit,
			ExcludeMuted: true, // spec 19 §5.3: normal folder views hide muted threads
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
//
// Selects on both Notifications() and Engine.Done() to avoid a
// goroutine leak when the engine stops before the channel is drained.
// Spec 03 §3.
func (m Model) consumeSyncEventsCmd() tea.Cmd {
	ch := m.deps.Engine.Notifications()
	done := m.deps.Engine.Done()
	return func() tea.Msg {
		select {
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			return SyncEventMsg{Event: ev}
		case <-done:
			return nil
		}
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
		if sidebarCmd := m.calendarSidebarCmd(); sidebarCmd != nil {
			return m, sidebarCmd
		}
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
		// Invalidate saved-search counts so sidebar badges stay fresh
		// after a sync delivers new messages. The counts Cmd is cheap
		// because the Manager's cache absorbs repeat calls within TTL.
		if refreshCmd := m.refreshSavedSearchCountsCmd(); refreshCmd != nil {
			cmds = append(cmds, refreshCmd)
		}
		// Refresh routing-bucket counts — a sync that delivered new
		// messages from already-routed senders shifts the badges
		// (spec 23 §5.4).
		if refreshCmd := m.refreshStreamCountsCmd(); refreshCmd != nil {
			cmds = append(cmds, refreshCmd)
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
	if m.deps.MinTerminalCols > 0 && m.width < m.deps.MinTerminalCols ||
		m.deps.MinTerminalRows > 0 && m.height < m.deps.MinTerminalRows {
		msg := fmt.Sprintf("terminal too small\nneed %d×%d, have %d×%d",
			m.deps.MinTerminalCols, m.deps.MinTerminalRows, m.width, m.height)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}
	if m.mode == SignInMode {
		return m.signin.View(m.theme, m.width, m.height)
	}
	if m.mode == ConfirmMode {
		return m.confirm.View(m.theme, m.width, m.height)
	}
	if m.mode == CalendarMode {
		tz := m.deps.CalendarTZ
		if tz == nil {
			tz = time.Local
		}
		return m.calendar.View(m.theme, tz, m.width, m.height)
	}
	if m.mode == CalendarDetailMode {
		return m.calendarDetail.View(m.theme, m.width, m.height)
	}
	if m.mode == OOFMode {
		return m.oof.View(m.theme, m.width, m.height)
	}
	if m.mode == SettingsMode {
		return m.settingsView.View(m.theme, m.width, m.height)
	}
	if m.mode == ComposeMode {
		return m.compose.View(m.theme, m.width, m.height)
	}
	if m.mode == AttachPickMode {
		base := m.compose.View(m.theme, m.width, m.height)
		prompt := m.theme.Modal.Render(
			"Attach file:\n\n" + m.attachPickInput.View() + "\n\n" +
				m.theme.Dim.Render("Enter to add  ·  Esc cancel"),
		)
		return base + "\n" + lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, prompt)
	}
	if m.mode == HelpMode {
		return m.help.View(m.theme, m.keymap, m.width, m.height)
	}
	if m.mode == URLPickerMode {
		return m.urlPicker.View(m.theme, m.viewer.Links(), m.width, m.height)
	}
	if m.mode == FolderPickerMode {
		return m.folderPicker.View(m.theme, m.width, m.height)
	}
	if m.mode == PaletteMode {
		return m.palette.View(m.theme, m.keymap, m.width, m.height)
	}
	if m.mode == FullscreenBodyMode {
		// Render the viewer at full terminal width with no
		// surrounding pane chrome so terminal selection drag works
		// end-to-end. Reserves the bottom row for a hint line.
		body := m.viewer.View(m.theme, m.width, m.height-1, true)
		hint := m.theme.Dim.Render("z/Esc/q  exit  ·  r  reply  ·  R  reply-all  ·  f  forward  ·  d  delete  ·  a  archive  ·  y  yank URL")
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
		var title, verb string
		if m.pendingBulkCategoryAction != "" {
			switch m.pendingBulkCategoryAction {
			case "add_category":
				verb = "add"
			case "remove_category":
				verb = "remove"
			default:
				verb = "set"
			}
			title = titleCase(verb) + " category (bulk · " + fmt.Sprintf("%d messages", len(m.filterIDs)) + ")"
		} else {
			verb = m.pendingCategoryAction
			if verb == "" {
				verb = "set"
			}
			title = titleCase(verb) + " category"
		}
		body := title + "\n\n" + m.theme.HelpKey.Render(verb+":") + " " + m.categoryBuf + "▎\n\n" +
			m.theme.Dim.Render("Enter to apply  ·  Esc to cancel")
		box := m.theme.Modal.Render(body)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	if m.mode == RuleEditMode {
		cursor := func(field int) string {
			if m.ruleEditField == field {
				return "▎"
			}
			return " "
		}
		nameLine := m.theme.HelpKey.Render("name:   ") + " " + m.ruleEditName + cursor(0)
		patLine := m.theme.HelpKey.Render("pattern:") + " " + m.ruleEditPattern + cursor(1)
		pinnedStr := "[ ] show in sidebar"
		if m.ruleEditPinned {
			pinnedStr = "[✓] show in sidebar"
		}
		pinnedLine := m.theme.HelpKey.Render("pinned: ") + " " + pinnedStr + cursor(2)
		hints := m.theme.Dim.Render("Tab next field  ·  ctrl+t test  ·  Enter save  ·  Esc cancel")
		body := "Edit saved search: " + m.ruleEditOrigName + "\n\n" +
			nameLine + "\n" +
			patLine + "\n" +
			pinnedLine + "\n\n" +
			hints
		if m.ruleEditTestMsg != "" {
			body += "\n\n" + m.ruleEditTestMsg
		}
		box := m.theme.Modal.Render(body)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	oooActive := m.mailboxSettings != nil && m.mailboxSettings.AutoReplyStatus != "" && m.mailboxSettings.AutoReplyStatus != "disabled"
	statusBar := m.status.View(m.theme, m.width, StatusInputs{
		LastSync:     m.lastSyncAt,
		Throttled:    m.throttledFor,
		Activity:     m.engineActivity,
		LastErr:      m.lastError,
		OOOActive:    oooActive,
		OOOIndicator: m.deps.OOOIndicator,
	})
	cmdBar := m.cmd.View(m.theme, m.width, m.mode == CommandMode)
	if m.mode == SearchMode {
		cmdBar = m.theme.CommandBar.Render("/" + m.searchBuf)
	} else if m.searchActive {
		// Append the streaming-search status hint when the
		// search package has emitted one (spec 06 §5.1):
		// "[searching local]" / "[📡 searching server…]" /
		// "[merged: N local, M server]" / "[local only — offline]".
		hint := "search: " + m.searchQuery + "  (esc to clear)"
		if m.searchStatus != "" {
			hint += "  " + m.searchStatus
		}
		cmdBar = m.theme.Dim.Render(hint)
	} else if m.filterActive {
		folderHint := ""
		if m.filterAllFolders {
			if m.filterFolderCount > 1 {
				folderHint = fmt.Sprintf(" (%d folders)", m.filterFolderCount)
			} else if m.filterFolderName != "" {
				folderHint = " (" + m.filterFolderName + ")"
			}
		}
		hint := fmt.Sprintf("filter: %s · matched %d%s · ;d delete · ;a archive · :unfilter", m.filterPattern, len(m.filterIDs), folderHint)
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
		hints = [][2]string{{"1/2/3", "panes"}, {"h", "back"}, {"j/k", "scroll"}, {"H", "headers"}, {"r", "reply"}, {"f", "fwd"}, {"a", "archive"}, {"d", "delete"}, {"z", "fullscreen"}, {"q", "quit"}}
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
