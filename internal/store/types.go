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
	IsRead         *bool
	FlagStatus     *string
	FolderID       *string
	Categories     *[]string
	LastModifiedAt *time.Time
}
