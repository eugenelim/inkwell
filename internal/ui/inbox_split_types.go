package ui

// InboxSplit is the typed config value for [inbox].split. Two literal
// values are accepted; config validation (internal/config/validate.go)
// rejects anything else at load time (spec 31 §10).
type InboxSplit string

const (
	// InboxSplitOff means the inbox sub-strip never renders. Default.
	InboxSplitOff InboxSplit = "off"
	// InboxSplitFocusedOther renders the two-segment sub-strip above
	// the list pane when the Inbox folder is selected.
	InboxSplitFocusedOther InboxSplit = "focused_other"
)

// Sub-tab segment indices for the [2]listSnapshot / [2]int arrays.
// Fixed two-element arrays — there are never more than two segments.
const (
	inboxSubTabFocused = 0
	inboxSubTabOther   = 1
)
