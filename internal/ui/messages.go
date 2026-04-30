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
	ComposeConfirmMode
	HelpMode
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

// BodyRenderedMsg is delivered after a body fetch (or cache hit) has
// produced text and link table. The viewer pane consumes it.
type BodyRenderedMsg struct {
	MessageID string
	Text      string
	State     int // mirrors render.BodyState
}
