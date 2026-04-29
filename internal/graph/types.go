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
	FlagStatus string `json:"flagStatus,omitempty"`
}

// MailFolder mirrors the subset of /me/mailFolders we read.
type MailFolder struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName"`
	ParentFolderID  string `json:"parentFolderId,omitempty"`
	WellKnownName   string `json:"wellKnownName,omitempty"`
	TotalItemCount  int    `json:"totalItemCount,omitempty"`
	UnreadItemCount int    `json:"unreadItemCount,omitempty"`
	IsHidden        bool   `json:"isHidden,omitempty"`
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
	// Removed is set by the delta endpoint when a message has been
	// deleted from the folder. Receivers treat as a tombstone.
	Removed *RemovedMarker `json:"@removed,omitempty"`
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
const EnvelopeSelectFields = "id,internetMessageId,conversationId,conversationIndex," +
	"subject,bodyPreview,from,toRecipients,ccRecipients,bccRecipients," +
	"receivedDateTime,sentDateTime," +
	"isRead,isDraft,flag,importance,inferenceClassification," +
	"hasAttachments,categories,webLink,parentFolderId,lastModifiedDateTime," +
	"meetingMessageType"
