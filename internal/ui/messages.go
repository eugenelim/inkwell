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
)

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
type BodyRenderedMsg struct {
	MessageID string
	Text      string
	Links     []BodyLink
	State     int // mirrors render.BodyState
}
