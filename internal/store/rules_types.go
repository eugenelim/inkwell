package store

import (
	"encoding/json"
	"time"
)

// MessageRule mirrors Microsoft Graph's messageRule resource. The
// raw *JSON fields preserve the unparsed Graph payload so non-v1
// predicates / actions survive round-trips through inkwell (spec 32
// §4.3). The typed Conditions / Actions / Exceptions are the v1
// catalogue subset surfaced to the TUI / CLI / loader; everything
// else round-trips through the Raw* JSON columns.
type MessageRule struct {
	AccountID   int64
	RuleID      string
	DisplayName string
	Sequence    int
	IsEnabled   bool
	IsReadOnly  bool
	HasError    bool

	Conditions MessagePredicates
	Actions    MessageActions
	Exceptions MessagePredicates

	RawConditions json.RawMessage
	RawActions    json.RawMessage
	RawExceptions json.RawMessage

	LastPulledAt time.Time
}

// MessagePredicates models the v1 catalogue subset of
// messageRulePredicates. Fields outside the catalogue (spec 32 §6.3)
// are preserved in MessageRule.RawConditions but not surfaced here.
//
// Pointer types distinguish "field unset" (nil) from "field set to
// zero" (e.g. *bool false). Graph itself treats absent fields as
// "no constraint"; we preserve that.
type MessagePredicates struct {
	BodyContains          []string        `json:"bodyContains,omitempty"`
	BodyOrSubjectContains []string        `json:"bodyOrSubjectContains,omitempty"`
	SubjectContains       []string        `json:"subjectContains,omitempty"`
	HeaderContains        []string        `json:"headerContains,omitempty"`
	FromAddresses         []RuleRecipient `json:"fromAddresses,omitempty"`
	SenderContains        []string        `json:"senderContains,omitempty"`
	SentToAddresses       []RuleRecipient `json:"sentToAddresses,omitempty"`
	RecipientContains     []string        `json:"recipientContains,omitempty"`
	SentToMe              *bool           `json:"sentToMe,omitempty"`
	SentCcMe              *bool           `json:"sentCcMe,omitempty"`
	SentOnlyToMe          *bool           `json:"sentOnlyToMe,omitempty"`
	SentToOrCcMe          *bool           `json:"sentToOrCcMe,omitempty"`
	NotSentToMe           *bool           `json:"notSentToMe,omitempty"`
	HasAttachments        *bool           `json:"hasAttachments,omitempty"`
	Importance            string          `json:"importance,omitempty"`
	Sensitivity           string          `json:"sensitivity,omitempty"`
	WithinSizeRange       *RuleSizeKB     `json:"withinSizeRange,omitempty"`
	Categories            []string        `json:"categories,omitempty"`
	IsAutomaticReply      *bool           `json:"isAutomaticReply,omitempty"`
	IsAutomaticForward    *bool           `json:"isAutomaticForward,omitempty"`
	MessageActionFlag     string          `json:"messageActionFlag,omitempty"`
}

// MessageActions models the v1 catalogue subset of messageRuleActions.
// Forward / redirect / permanentDelete are intentionally absent — see
// spec 32 §2.7 and §6.3.
type MessageActions struct {
	MarkAsRead          *bool    `json:"markAsRead,omitempty"`
	MarkImportance      string   `json:"markImportance,omitempty"`
	MoveToFolder        string   `json:"moveToFolder,omitempty"`
	CopyToFolder        string   `json:"copyToFolder,omitempty"`
	AssignCategories    []string `json:"assignCategories,omitempty"`
	Delete              *bool    `json:"delete,omitempty"`
	StopProcessingRules *bool    `json:"stopProcessingRules,omitempty"`
}

// RuleSizeKB matches Graph's sizeRange in kilobytes.
type RuleSizeKB struct {
	MinimumSize int `json:"minimumSize"`
	MaximumSize int `json:"maximumSize"`
}

// RuleRecipient mirrors graph.Recipient. Duplicated minimally here
// because per `docs/CONVENTIONS.md` §2 layering `store` and `graph` are sibling
// lower-tier packages and cannot import each other; the typed values
// flow up to `internal/rules` (a middle-tier consumer) which converts
// between the two.
type RuleRecipient struct {
	EmailAddress RuleEmailAddress `json:"emailAddress"`
}

// RuleEmailAddress mirrors graph.EmailAddress (see RuleRecipient
// comment).
type RuleEmailAddress struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}
