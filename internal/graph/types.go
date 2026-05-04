package graph

import "time"

// EmailAddress mirrors Graph's emailAddress sub-resource.
type EmailAddress struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

// Recipient is a recipients[] element in a Graph message.
type Recipient struct {
	EmailAddress EmailAddress `json:"emailAddress"`
}

// Body mirrors Graph's itemBody.
type Body struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

// Flag mirrors Graph's followupFlag.
type Flag struct {
	FlagStatus        string            `json:"flagStatus,omitempty"`
	DueDateTime       *DateTimeTimeZone `json:"dueDateTime,omitempty"`
	CompletedDateTime *DateTimeTimeZone `json:"completedDateTime,omitempty"`
}

// MailFolder mirrors the subset of /me/mailFolders we read.
type MailFolder struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	ParentFolderID   string `json:"parentFolderId,omitempty"`
	WellKnownName    string `json:"wellKnownName,omitempty"`
	TotalItemCount   int    `json:"totalItemCount,omitempty"`
	UnreadItemCount  int    `json:"unreadItemCount,omitempty"`
	IsHidden         bool   `json:"isHidden,omitempty"`
	ChildFolderCount int    `json:"childFolderCount,omitempty"`
	// Removed is set by /me/mailFolders/delta when a folder has been
	// deleted server-side. Callers using the delta endpoint without
	// a persisted delta token won't see this (a fresh delta returns
	// the current state with no removals); the field is here for
	// when the delta-token incremental path lands.
	Removed *RemovedMarker `json:"@removed,omitempty"`
}

// Message is the shape we request via $select. Fields not listed in the
// spec §5.2 envelope set are omitted intentionally.
type Message struct {
	ID                      string      `json:"id"`
	InternetMessageID       string      `json:"internetMessageId,omitempty"`
	ConversationID          string      `json:"conversationId,omitempty"`
	ConversationIndex       []byte      `json:"conversationIndex,omitempty"`
	Subject                 string      `json:"subject,omitempty"`
	BodyPreview             string      `json:"bodyPreview,omitempty"`
	From                    *Recipient  `json:"from,omitempty"`
	ToRecipients            []Recipient `json:"toRecipients,omitempty"`
	CcRecipients            []Recipient `json:"ccRecipients,omitempty"`
	BccRecipients           []Recipient `json:"bccRecipients,omitempty"`
	ReceivedDateTime        time.Time   `json:"receivedDateTime,omitempty"`
	SentDateTime            time.Time   `json:"sentDateTime,omitempty"`
	IsRead                  bool        `json:"isRead,omitempty"`
	IsDraft                 bool        `json:"isDraft,omitempty"`
	Flag                    *Flag       `json:"flag,omitempty"`
	Importance              string      `json:"importance,omitempty"`
	InferenceClassification string      `json:"inferenceClassification,omitempty"`
	HasAttachments          bool        `json:"hasAttachments,omitempty"`
	Categories              []string    `json:"categories,omitempty"`
	WebLink                 string      `json:"webLink,omitempty"`
	ParentFolderID          string      `json:"parentFolderId,omitempty"`
	LastModifiedDateTime    time.Time   `json:"lastModifiedDateTime,omitempty"`
	MeetingMessageType      string      `json:"meetingMessageType,omitempty"`
	Body                    *Body       `json:"body,omitempty"`
	// Attachments is populated by GetMessageBody via $expand=attachments
	// (spec 05 §5.2). Empty otherwise. Only metadata fields are
	// modelled — the raw bytes are fetched on-demand by the save /
	// open path (PR 10).
	Attachments []Attachment `json:"attachments,omitempty"`
	// InternetMessageHeaders carries the RFC 822 headers returned when
	// GetMessageBody includes internetMessageHeaders in $select (spec 05
	// C-1). Empty when the fetch did not request them.
	InternetMessageHeaders []MessageHeader `json:"internetMessageHeaders,omitempty"`
	// Removed is set by the delta endpoint when a message has been
	// deleted from the folder. Receivers treat as a tombstone.
	Removed *RemovedMarker `json:"@removed,omitempty"`
}

// Attachment mirrors Graph's fileAttachment / itemAttachment subset
// inkwell renders (name, size, content-type, inline flag, contentId).
// We don't recurse into itemAttachment's nested item — v1 lists the
// envelope only.
type Attachment struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	ContentType  string `json:"contentType,omitempty"`
	Size         int64  `json:"size,omitempty"`
	IsInline     bool   `json:"isInline,omitempty"`
	ContentID    string `json:"contentId,omitempty"`
	ODataType    string `json:"@odata.type,omitempty"`
	LastModified string `json:"lastModifiedDateTime,omitempty"`
}

// RemovedMarker is the tombstone payload Graph emits in delta responses.
type RemovedMarker struct {
	Reason string `json:"reason"`
}

// DeltaResponse is one page of a delta query.
type DeltaResponse struct {
	Value     []Message `json:"value"`
	NextLink  string    `json:"@odata.nextLink,omitempty"`
	DeltaLink string    `json:"@odata.deltaLink,omitempty"`
}

// FolderListResponse is a paginated list of mailFolders.
type FolderListResponse struct {
	Value    []MailFolder `json:"value"`
	NextLink string       `json:"@odata.nextLink,omitempty"`
}

// EnvelopeSelectFields is the locked $select for envelope sync (spec §5.2).
//
// `meetingMessageType` was previously included here (spec 16 v0.13.0)
// to drive the canonical 📅 indicator. Microsoft Graph rejected the
// field on `/me/mailFolders/{folderID}/messages?$select=...` with
// `RequestBroker--ParseUri: Could not find a property named
// 'meetingMessageType' on type 'Microsoft.OutlookServices.Message'` —
// the property only exists on the `microsoft.graph.eventMessage`
// derived type, not on the polymorphic Message base. Calls to that
// endpoint (every Backfill / wall-sync) failed completely on real
// tenants. Removed; the list pane falls back to the subject-prefix
// heuristic in `isLikelyMeeting` for invite detection. A future
// release can re-introduce it via `$select=microsoft.graph.event
// Message/meetingMessageType` casting if desired.
const EnvelopeSelectFields = "id,internetMessageId,conversationId,conversationIndex," +
	"subject,bodyPreview,from,toRecipients,ccRecipients,bccRecipients," +
	"receivedDateTime,sentDateTime," +
	"isRead,isDraft,flag,importance,inferenceClassification," +
	"hasAttachments,categories,webLink,parentFolderId,lastModifiedDateTime"
