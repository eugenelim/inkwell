package store

import "time"

// Account is a single mailbox identity. v1 has exactly one row.
type Account struct {
	ID          int64
	TenantID    string
	ClientID    string
	UPN         string
	DisplayName string
	ObjectID    string
	LastSignin  time.Time
}

// Folder is a Microsoft Graph mail folder cached locally.
type Folder struct {
	ID             string
	AccountID      int64
	ParentFolderID string
	DisplayName    string
	WellKnownName  string
	TotalCount     int
	UnreadCount    int
	IsHidden       bool
	LastSyncedAt   time.Time
}

// Message is the tier-1 envelope row stored in the messages table.
// Bodies live in [Body]. Field shape mirrors §3.4 of spec 02.
type Message struct {
	ID                string
	AccountID         int64
	FolderID          string
	InternetMessageID string
	ConversationID    string
	ConversationIndex []byte
	Subject           string
	BodyPreview       string
	FromAddress       string
	FromName          string
	ToAddresses       []EmailAddress
	CcAddresses       []EmailAddress
	BccAddresses      []EmailAddress
	ReceivedAt        time.Time
	SentAt            time.Time
	IsRead            bool
	IsDraft           bool
	FlagStatus        string
	FlagDueAt         time.Time
	FlagCompletedAt   time.Time
	Importance        string
	InferenceClass    string
	HasAttachments    bool
	Categories        []string
	WebLink           string
	LastModifiedAt    time.Time
	CachedAt          time.Time
	EnvelopeETag      string
	// MeetingMessageType mirrors Graph's enum: empty / "none" for plain
	// mail, "meetingRequest", "meetingResponse", "meetingCancellation",
	// "meetingForwardNotification". The list pane uses non-empty
	// (and != "none") to render the 📅 invite indicator.
	MeetingMessageType string
	// UnsubscribeURL caches the parsed List-Unsubscribe action (spec 16):
	//   - "https://…" for an RFC 8058 one-click POST or browser GET
	//   - "mailto:<addr>" for a mailto: action
	//   - "" when the headers haven't been fetched OR carry no actionable URI
	// Populated lazily on first U-key press; persisted so subsequent
	// presses are a local lookup.
	UnsubscribeURL string
	// UnsubscribeOneClick is true iff Parse classified this as
	// ActionOneClickPOST (List-Unsubscribe-Post present + HTTPS URI).
	UnsubscribeOneClick bool
}

// EmailAddress is one recipient entry.
type EmailAddress struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

// Body is a tier-2 cached body. ContentType is "text" or "html".
type Body struct {
	MessageID      string
	ContentType    string
	Content        string
	ContentSize    int64
	FetchedAt      time.Time
	LastAccessedAt time.Time
}

// Attachment is the metadata-only attachment row. Bytes are not cached.
type Attachment struct {
	ID          string
	MessageID   string
	Name        string
	ContentType string
	Size        int64
	IsInline    bool
	ContentID   string
}

// DeltaToken is a per-(account, folder) sync resume cursor.
type DeltaToken struct {
	AccountID    int64
	FolderID     string
	DeltaLink    string
	NextLink     string
	LastFullSync time.Time
	LastDeltaAt  time.Time
}

// ActionType enumerates the supported queued action kinds.
type ActionType string

const (
	ActionMove            ActionType = "move"
	ActionSoftDelete      ActionType = "soft_delete"
	ActionPermanentDelete ActionType = "permanent_delete"
	ActionMarkRead        ActionType = "mark_read"
	ActionMarkUnread      ActionType = "mark_unread"
	ActionFlag            ActionType = "flag"
	ActionUnflag          ActionType = "unflag"
	ActionAddCategory     ActionType = "add_category"
	ActionRemoveCategory  ActionType = "remove_category"
	// ActionCreateDraftReply enqueues a "reply to message X with body Y"
	// draft creation. Two-stage dispatch (createReply → PATCH body):
	// the first stage records draft_id + web_link in Params so a
	// crashed second stage can resume idempotently. Spec 15 §5 / §8.
	ActionCreateDraftReply ActionType = "create_draft_reply"
	// ActionCreateDraftReplyAll mirrors ActionCreateDraftReply but
	// uses Graph's `/me/messages/{id}/createReplyAll` so the draft
	// pre-populates with the source's full audience (To = original
	// From + remaining To recipients; Cc = original Cc; deduped
	// against the user's own UPN). Two-stage dispatch identical in
	// shape to ActionCreateDraftReply. Spec 15 §5 / PR 7-iii.
	ActionCreateDraftReplyAll ActionType = "create_draft_reply_all"
	// ActionCreateDraftForward enqueues a forward of the source.
	// Stage 1 calls `/me/messages/{id}/createForward` (Graph
	// generates the "Forwarded message" header block + quote
	// chain); stage 2 PATCHes the user-supplied To/Cc/Bcc/Subject/
	// Body. Two-stage dispatch identical in shape to
	// ActionCreateDraftReply. Spec 15 §5 / PR 7-iii.
	ActionCreateDraftForward ActionType = "create_draft_forward"
	// ActionCreateDraft enqueues a brand-new (no source) draft.
	// Single-stage: POST /me/messages with the full body+headers
	// payload returns the persisted draft directly, so we don't
	// need the two-stage createX/PATCH dance. Drain still skips
	// this type because the POST is non-idempotent (a retry
	// produces a duplicate draft). Spec 15 §5 / PR 7-iii.
	ActionCreateDraft ActionType = "create_draft"
	// ActionDiscardDraft deletes the server-side draft created by a
	// previous CreateDraft* action. Idempotent: 404 from Graph is
	// treated as success (spec 15 §6.3 / F-1). SkipUndo is always
	// true — draft discard is not reversible via the undo stack.
	ActionDiscardDraft ActionType = "discard_draft"
)

// ActionStatus enumerates the lifecycle states.
type ActionStatus string

const (
	StatusPending  ActionStatus = "pending"
	StatusInFlight ActionStatus = "in_flight"
	StatusDone     ActionStatus = "done"
	StatusFailed   ActionStatus = "failed"
)

// Action is a queued write against Graph.
type Action struct {
	ID            string
	AccountID     int64
	Type          ActionType
	MessageIDs    []string
	Params        map[string]any
	Status        ActionStatus
	FailureReason string
	CreatedAt     time.Time
	StartedAt     time.Time
	CompletedAt   time.Time
	// SkipUndo marks an action that came from the undo stack itself
	// (Executor.Undo). Without it, applying an undo would push the
	// inverse-of-the-inverse and `u` would toggle infinitely instead
	// of stepping back through history. Spec 07 §11.2.
	// Not persisted — runtime-only.
	SkipUndo bool `json:"-"`
}

// UndoEntry is a session-scoped reversible action descriptor.
type UndoEntry struct {
	ID         int64
	ActionType ActionType
	MessageIDs []string
	Params     map[string]any
	Label      string
	CreatedAt  time.Time
}

// Event is a cached calendar event (spec 12). Times are UTC; the UI
// converts to the mailbox time zone at render. Mirrors the subset
// of /me/calendarView fields the read-only calendar surfaces.
type Event struct {
	ID               string
	AccountID        int64
	Subject          string
	OrganizerName    string
	OrganizerAddress string
	Start            time.Time
	End              time.Time
	IsAllDay         bool
	Location         string
	OnlineMeetingURL string
	ShowAs           string // "free" | "busy" | "tentative" | "oof" | "workingElsewhere"
	ResponseStatus   string // "accepted" | "tentativelyAccepted" | "declined" | "notResponded" | "none" | "organizer"
	WebLink          string
	CachedAt         time.Time
}

// EventQuery narrows ListEvents results.
type EventQuery struct {
	AccountID int64
	// Start / End define a half-open [Start, End) window in UTC.
	// Zero values mean unbounded on that side.
	Start time.Time
	End   time.Time
	Limit int
}

// EventAttendee is one persisted attendee row (spec 12 §3 /
// migration 006). Keyed (event_id, address); populated by
// PutEventAttendees when the detail modal loads.
type EventAttendee struct {
	EventID string
	Address string
	Name    string
	Type    string // required | optional | resource
	Status  string // accepted | tentativelyAccepted | declined | notResponded | none | organizer
}

// ComposeSession is one in-flight compose form persisted for crash
// recovery (spec 15 §7). The Snapshot field carries the JSON-
// encoded `ComposeSnapshot` produced by `internal/ui/ComposeModel`
// — store.Store keeps it opaque (the UI is the only consumer that
// needs the structured shape) so the package boundary stays clean.
//
// CreatedAt records when the user first entered ComposeMode;
// UpdatedAt advances on each snapshot rewrite (focus changes,
// save). ConfirmedAt is non-zero once the session has been saved
// or discarded — until then the launch-time resume scan offers it
// to the user. Confirmed sessions older than 24h get garbage-
// collected.
type ComposeSession struct {
	SessionID   string
	Kind        string // "reply" | "reply_all" | "forward" | "new"
	SourceID    string // empty for KindNew
	Snapshot    string // JSON blob, opaque to store
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ConfirmedAt time.Time // zero while in flight
}

// SavedSearch persists a named pattern for the sidebar virtual folder.
type SavedSearch struct {
	ID        int64
	AccountID int64
	Name      string
	Pattern   string
	Pinned    bool
	SortOrder int
	CreatedAt time.Time
}

// MessageQuery describes a filtered list query for [Store.ListMessages].
type MessageQuery struct {
	AccountID      int64
	FolderID       string
	ConversationID string
	From           string
	UnreadOnly     bool
	FlaggedOnly    bool
	HasAttachments *bool
	ReceivedAfter  *time.Time
	ReceivedBefore *time.Time
	Categories     []string
	OrderBy        OrderField
	Limit          int
	Offset         int
}

// OrderField names the sort key of [MessageQuery].
type OrderField int

const (
	OrderReceivedDesc OrderField = iota
	OrderReceivedAsc
	OrderSubjectAsc
	OrderFromAsc
)

// SearchQuery describes an FTS5 query.
type SearchQuery struct {
	AccountID int64
	FolderID  string
	Query     string
	Limit     int
}

// MessageMatch is a scored search hit.
type MessageMatch struct {
	Message Message
	Rank    float64
}

// MessageFields names a partial update payload for [Store.UpdateMessageFields].
type MessageFields struct {
	IsRead          *bool
	FlagStatus      *string
	FlagDueAt       *time.Time
	FlagCompletedAt *time.Time
	FolderID        *string
	Categories      *[]string
	LastModifiedAt  *time.Time
}
