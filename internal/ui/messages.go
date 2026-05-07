package ui

import (
	"time"

	"github.com/eugenelim/inkwell/internal/store"
	isync "github.com/eugenelim/inkwell/internal/sync"
)

// Pane identifies one of the three panes plus the command line.
type Pane int

const (
	FoldersPane Pane = iota
	ListPane
	ViewerPane
	CommandLinePane
)

// Mode is the modal state of the root model.
type Mode int

const (
	NormalMode Mode = iota
	CommandMode
	SearchMode
	SignInMode
	ConfirmMode
	CalendarMode
	OOFMode
	HelpMode
	CategoryInputMode
	FolderNameInputMode
	URLPickerMode
	FullscreenBodyMode
	FolderPickerMode
	CalendarDetailMode
	// ComposeMode is the spec 15 v2 in-modal compose overlay
	// (replaces the legacy tempfile + $EDITOR + post-edit confirm
	// flow). Persistent footer with Ctrl+S / Ctrl+D / Tab; resolves
	// the user-reported "select Exit command first" friction.
	ComposeMode
	// SettingsMode shows the read-only mailbox-settings overview modal
	// (spec 13 §5.2). Press `o` to switch to OOFMode for editing.
	SettingsMode
	// RuleEditMode is the spec 11 B-2 edit modal for saved searches.
	RuleEditMode
	// AttachPickMode is the file-path prompt for staging a local
	// attachment in the compose pane (spec 15 §5 / plan item 27).
	AttachPickMode
	// PaletteMode is the spec 22 Ctrl+K command palette overlay.
	PaletteMode
)

// composeEditorDoneMsg is returned by tea.ExecProcess when the user's
// $EDITOR exits after a Ctrl+E drop-out from the compose pane.
type composeEditorDoneMsg struct {
	tempPath string
	err      error
}

// SyncEventMsg wraps a sync.Event for delivery into Bubble Tea's update
// loop. The engine's Notifications() channel is consumed by a
// long-running tea.Cmd that repeatedly emits one of these.
type SyncEventMsg struct{ Event isync.Event }

// FoldersLoadedMsg arrives when the store has produced a fresh folder list.
type FoldersLoadedMsg struct {
	Folders []store.Folder
	At      time.Time
}

// MessagesLoadedMsg arrives when the list pane's query completes.
type MessagesLoadedMsg struct {
	FolderID string
	Messages []store.Message
}

// SearchUpdateMsg carries one progressive snapshot from a
// streaming SearchService run (spec 06 §3). The Update handler
// replaces the list-pane contents wholesale per snapshot — no
// incremental merging in the UI layer; the search package owns
// dedup + sort.
type SearchUpdateMsg struct {
	Query   string
	Status  string
	Results []store.Message
	Done    bool // true on the channel-closed final emission
}

// ErrorMsg surfaces a UI-level error (used for transient banners).
type ErrorMsg struct{ Err error }

// ConfirmResultMsg is returned from a confirmation modal.
type ConfirmResultMsg struct {
	Topic   string // free-form identifier, used to route on completion
	Confirm bool
}

// authRequiredMsg is emitted when a graph call returned 401 / token
// refresh failed. It transitions the root into SignInMode.
type authRequiredMsg struct{ At time.Time }

// BodyLink is a UI-side mirror of render.ExtractedLink. Defined
// at the consumer site so messages.go doesn't import internal/render.
type BodyLink struct {
	Index int
	URL   string
	Text  string
}

// BodyRenderedMsg is delivered after a body fetch (or cache hit) has
// produced text and link table. The viewer pane consumes it.
//
// Attachments carries the attachment metadata for the message
// (spec 05 §5.2 / §8). Empty when the message has none. The viewer
// renders an "Attachments:" block between headers and body so the
// reader sees what's attached before scrolling into the message.
//
// Conversation carries the sibling messages in the same conversation,
// sorted by ReceivedAt ASC, for the thread-map section (spec 05 §11).
// Nil when ConversationID is empty or the store query fails.
//
// TextExpanded is the fully un-collapsed body (quotes not folded).
// Text carries the collapsed version when quote collapsing is active.
//
// RawHeaders carries the RFC 822 headers returned alongside the body
// (spec 05 C-1). Empty when the fetch did not include headers.
type BodyRenderedMsg struct {
	MessageID    string
	Text         string
	TextExpanded string
	Links        []BodyLink
	State        int // mirrors render.BodyState
	Attachments  []store.Attachment
	Conversation []store.Message
	RawHeaders   []RawHeader
}

// RawHeader is a single RFC 822 header name/value pair. Defined here
// so messages.go doesn't import internal/render (consumer-site interface
// per CLAUDE.md §2).
type RawHeader struct {
	Name  string
	Value string
}

// SaveAttachmentDoneMsg is emitted when an attachment download + save to
// disk completes. Path is the destination on success; Err is non-nil on
// failure.
type SaveAttachmentDoneMsg struct {
	Name string
	Path string
	Err  error
}

// OpenAttachmentDoneMsg is emitted when an attachment download + open
// (via the default OS application) completes.
type OpenAttachmentDoneMsg struct {
	Name string
	Err  error
}

// savedSearchesUpdatedMsg delivers a refreshed saved-search list (with
// updated Count fields) back into the Update loop. The FoldersModel is
// updated so the sidebar count badges reflect the new values.
type savedSearchesUpdatedMsg struct {
	searches []SavedSearch
}

// savedSearchSavedMsg signals that a `:rule save` or `:rule delete` command
// completed. The searches field carries the reloaded list for the sidebar.
type savedSearchSavedMsg struct {
	searches []SavedSearch
	err      error
	action   string // "saved" | "deleted"
	name     string
}

// clearTransientMsg is emitted by clearTransientCmd after the TTL elapses
// to auto-clear m.engineActivity.
type clearTransientMsg struct{}

// draftWebLinkExpiredMsg fires when the spec 15 F-1 webLink TTL
// elapses so the status-bar hint auto-clears. TTL is controlled by
// [compose].web_link_ttl (default 30s).
type draftWebLinkExpiredMsg struct{}

// draftDiscardDoneMsg fires after the DiscardDraft Graph call
// completes (success or failure). Spec 15 §6.3 / F-1.
type draftDiscardDoneMsg struct{ err error }

// savedSearchBgRefreshMsg fires on the background refresh timer tick
// (spec 11 §6.2). The handler in Update refreshes counts and re-arms.
type savedSearchBgRefreshMsg struct{}

// ruleEditDoneMsg is the result of saving an edited saved search.
type ruleEditDoneMsg struct {
	searches []SavedSearch
	err      error
	newName  string
}

// ruleEditTestDoneMsg carries the result of a `:rule edit` pattern test.
type ruleEditTestDoneMsg struct {
	count int
	err   error
}
