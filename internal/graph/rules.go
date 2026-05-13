package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// MessageRule mirrors Microsoft Graph's messageRule resource for the
// v1 catalogue subset spec 32 surfaces. Raw* fields preserve the
// unparsed Graph payload so non-v1 predicates / actions survive
// round-trips through inkwell (spec 32 §4.3 / §5.3).
type MessageRule struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName"`
	Sequence    int    `json:"sequence"`
	IsEnabled   bool   `json:"isEnabled"`
	IsReadOnly  bool   `json:"isReadOnly,omitempty"`
	HasError    bool   `json:"hasError,omitempty"`

	Conditions *MessageRulePredicates `json:"conditions,omitempty"`
	Actions    *MessageRuleActions    `json:"actions,omitempty"`
	Exceptions *MessageRulePredicates `json:"exceptions,omitempty"`
}

// MessageRulePredicates is the v1 catalogue subset of Graph's
// messageRulePredicates. See spec 32 §6.3 for the closed set of
// supported fields.
type MessageRulePredicates struct {
	BodyContains          []string           `json:"bodyContains,omitempty"`
	BodyOrSubjectContains []string           `json:"bodyOrSubjectContains,omitempty"`
	SubjectContains       []string           `json:"subjectContains,omitempty"`
	HeaderContains        []string           `json:"headerContains,omitempty"`
	FromAddresses         []Recipient        `json:"fromAddresses,omitempty"`
	SenderContains        []string           `json:"senderContains,omitempty"`
	SentToAddresses       []Recipient        `json:"sentToAddresses,omitempty"`
	RecipientContains     []string           `json:"recipientContains,omitempty"`
	SentToMe              *bool              `json:"sentToMe,omitempty"`
	SentCcMe              *bool              `json:"sentCcMe,omitempty"`
	SentOnlyToMe          *bool              `json:"sentOnlyToMe,omitempty"`
	SentToOrCcMe          *bool              `json:"sentToOrCcMe,omitempty"`
	NotSentToMe           *bool              `json:"notSentToMe,omitempty"`
	HasAttachments        *bool              `json:"hasAttachments,omitempty"`
	Importance            string             `json:"importance,omitempty"`
	Sensitivity           string             `json:"sensitivity,omitempty"`
	WithinSizeRange       *MessageRuleSizeKB `json:"withinSizeRange,omitempty"`
	Categories            []string           `json:"categories,omitempty"`
	IsAutomaticReply      *bool              `json:"isAutomaticReply,omitempty"`
	IsAutomaticForward    *bool              `json:"isAutomaticForward,omitempty"`
	MessageActionFlag     string             `json:"messageActionFlag,omitempty"`
}

// MessageRuleActions is the v1 catalogue subset of Graph's
// messageRuleActions. forwardTo / forwardAsAttachmentTo / redirectTo
// / permanentDelete are intentionally omitted (spec 32 §2.7).
type MessageRuleActions struct {
	MarkAsRead          *bool    `json:"markAsRead,omitempty"`
	MarkImportance      string   `json:"markImportance,omitempty"`
	MoveToFolder        string   `json:"moveToFolder,omitempty"`
	CopyToFolder        string   `json:"copyToFolder,omitempty"`
	AssignCategories    []string `json:"assignCategories,omitempty"`
	Delete              *bool    `json:"delete,omitempty"`
	StopProcessingRules *bool    `json:"stopProcessingRules,omitempty"`
}

// MessageRuleSizeKB matches Graph's sizeRange (kilobytes).
type MessageRuleSizeKB struct {
	MinimumSize int `json:"minimumSize"`
	MaximumSize int `json:"maximumSize"`
}

// MessageRuleRaw carries a MessageRule alongside the raw conditions /
// actions / exceptions JSON sub-objects as Graph returned them. The
// raw bytes are the source of truth for round-trip preservation of
// non-v1 fields; the typed projection is for callers that only need
// the v1 catalogue surface.
type MessageRuleRaw struct {
	Rule          MessageRule
	RawConditions json.RawMessage
	RawActions    json.RawMessage
	RawExceptions json.RawMessage
}

const messageRulesPath = "/me/mailFolders/inbox/messageRules"

// ListMessageRules returns every Inbox rule for the signed-in user.
// Single GET; Graph does not paginate this endpoint in practice.
// Returns the typed rules alongside the raw conditions / actions /
// exceptions JSON so callers can round-trip non-v1 fields (spec 32
// §5.2 / §5.3).
func (c *Client) ListMessageRules(ctx context.Context) ([]MessageRuleRaw, error) {
	resp, err := c.Do(ctx, http.MethodGet, messageRulesPath, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var page struct {
		Value []json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("graph: decode list-message-rules: %w", err)
	}
	out := make([]MessageRuleRaw, 0, len(page.Value))
	for _, raw := range page.Value {
		r, err := decodeMessageRuleRaw(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// GetMessageRule fetches one rule by ID. Returns *GraphError with
// StatusCode == 404 when the rule is missing.
func (c *Client) GetMessageRule(ctx context.Context, ruleID string) (MessageRuleRaw, error) {
	resp, err := c.Do(ctx, http.MethodGet, messageRulesPath+"/"+ruleID, nil, nil)
	if err != nil {
		return MessageRuleRaw{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return MessageRuleRaw{}, parseError(resp)
	}
	raw, err := decodeRawMessage(resp.Body)
	if err != nil {
		return MessageRuleRaw{}, fmt.Errorf("graph: decode get-message-rule: %w", err)
	}
	return decodeMessageRuleRaw(raw)
}

// CreateMessageRule posts a new rule. The server assigns the ID; the
// returned MessageRuleRaw has it populated. The supplied
// MessageRule.ID is ignored on the wire.
func (c *Client) CreateMessageRule(ctx context.Context, r MessageRule) (MessageRuleRaw, error) {
	r.ID = "" // server assigns
	body, err := json.Marshal(r)
	if err != nil {
		return MessageRuleRaw{}, fmt.Errorf("graph: marshal create-message-rule: %w", err)
	}
	resp, err := c.Do(ctx, http.MethodPost, messageRulesPath, bytes.NewReader(body), http.Header{
		"Content-Type": []string{"application/json"},
	})
	if err != nil {
		return MessageRuleRaw{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MessageRuleRaw{}, parseError(resp)
	}
	raw, err := decodeRawMessage(resp.Body)
	if err != nil {
		return MessageRuleRaw{}, fmt.Errorf("graph: decode create-message-rule: %w", err)
	}
	return decodeMessageRuleRaw(raw)
}

// UpdateMessageRule PATCHes a rule by ID. The body must be the full
// MessageRule payload for the v1-known fields, plus any non-v1 fields
// the caller wants to preserve (Graph PATCH on messageRule sub-objects
// is replace-only, see spec 32 §5.3). The caller is responsible for
// merging non-v1 fields via JSONMerge before passing the payload as a
// json.RawMessage.
func (c *Client) UpdateMessageRule(ctx context.Context, ruleID string, body json.RawMessage) (MessageRuleRaw, error) {
	resp, err := c.Do(ctx, http.MethodPatch, messageRulesPath+"/"+ruleID,
		bytes.NewReader(body), http.Header{
			"Content-Type": []string{"application/json"},
		})
	if err != nil {
		return MessageRuleRaw{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MessageRuleRaw{}, parseError(resp)
	}
	raw, err := decodeRawMessage(resp.Body)
	if err != nil {
		return MessageRuleRaw{}, fmt.Errorf("graph: decode update-message-rule: %w", err)
	}
	return decodeMessageRuleRaw(raw)
}

// DeleteMessageRule deletes a rule by ID. 404 is treated as success
// (idempotent per CLAUDE.md §3).
func (c *Client) DeleteMessageRule(ctx context.Context, ruleID string) error {
	resp, err := c.Do(ctx, http.MethodDelete, messageRulesPath+"/"+ruleID, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp)
	}
	return nil
}

func decodeMessageRuleRaw(raw json.RawMessage) (MessageRuleRaw, error) {
	var r MessageRule
	if err := json.Unmarshal(raw, &r); err != nil {
		return MessageRuleRaw{}, fmt.Errorf("graph: decode message rule: %w", err)
	}
	// Pull out the raw sub-object bytes so callers can round-trip
	// non-v1 fields. Missing keys → empty.
	var subs struct {
		Conditions json.RawMessage `json:"conditions"`
		Actions    json.RawMessage `json:"actions"`
		Exceptions json.RawMessage `json:"exceptions"`
	}
	_ = json.Unmarshal(raw, &subs)
	return MessageRuleRaw{
		Rule:          r,
		RawConditions: subs.Conditions,
		RawActions:    subs.Actions,
		RawExceptions: subs.Exceptions,
	}, nil
}

// decodeRawMessage reads a full JSON value from r into a RawMessage.
// Used by GetMessageRule / CreateMessageRule / UpdateMessageRule to
// preserve the bytes for decodeMessageRuleRaw.
func decodeRawMessage(body interface {
	Read(p []byte) (n int, err error)
}) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}
