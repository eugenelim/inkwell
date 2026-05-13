package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// GraphClient is the subset of *graph.Client this package needs. A
// narrow interface keeps the pull / apply pipelines unit-testable
// without standing up a real httptest server (the e2e CLI tests
// exercise the real client).
type GraphClient interface {
	ListMessageRules(ctx context.Context) ([]graph.MessageRuleRaw, error)
	GetMessageRule(ctx context.Context, ruleID string) (graph.MessageRuleRaw, error)
	CreateMessageRule(ctx context.Context, r graph.MessageRule) (graph.MessageRuleRaw, error)
	UpdateMessageRule(ctx context.Context, ruleID string, body json.RawMessage) (graph.MessageRuleRaw, error)
	DeleteMessageRule(ctx context.Context, ruleID string) error
}

// PullResult is what Pull returns to its caller.
type PullResult struct {
	Pulled int
	Path   string // path of the rewritten rules.toml
}

// Pull fetches every Inbox rule from Graph, refreshes the local
// mirror via store.UpsertMessageRulesBatch, and atomically rewrites
// rules.toml. Empty display names are populated with the placeholder
// `<unnamed rule N>` (where N is the sequence) so downstream apply
// passes can match by name.
func Pull(ctx context.Context, gc GraphClient, s store.Store, accountID int64, rulesPath string) (PullResult, error) {
	raws, err := gc.ListMessageRules(ctx)
	if err != nil {
		return PullResult{}, fmt.Errorf("graph list message rules: %w", err)
	}

	now := time.Now().UTC()
	storeRules := make([]store.MessageRule, 0, len(raws))
	tomlRules := make([]Rule, 0, len(raws))
	for _, raw := range raws {
		sr := storeRuleFromGraph(accountID, raw, now)
		storeRules = append(storeRules, sr)
		tr := tomlRuleFromGraph(raw)
		tomlRules = append(tomlRules, tr)
	}

	if _, err := s.UpsertMessageRulesBatch(ctx, accountID, storeRules); err != nil {
		return PullResult{}, fmt.Errorf("upsert mirror: %w", err)
	}

	body, err := EncodeCatalogue(tomlRules)
	if err != nil {
		return PullResult{}, fmt.Errorf("encode catalogue: %w", err)
	}
	if err := AtomicWriteFile(rulesPath, body, 0o600); err != nil {
		return PullResult{}, fmt.Errorf("write rules.toml: %w", err)
	}
	return PullResult{Pulled: len(raws), Path: rulesPath}, nil
}

func storeRuleFromGraph(accountID int64, raw graph.MessageRuleRaw, now time.Time) store.MessageRule {
	r := raw.Rule
	displayName := r.DisplayName
	if strings.TrimSpace(displayName) == "" {
		displayName = unnamedPlaceholder(r.Sequence)
	}
	conditions := storePredicatesFromGraph(r.Conditions)
	exceptions := storePredicatesFromGraph(r.Exceptions)
	actions := storeActionsFromGraph(r.Actions)
	return store.MessageRule{
		AccountID:     accountID,
		RuleID:        r.ID,
		DisplayName:   displayName,
		Sequence:      r.Sequence,
		IsEnabled:     r.IsEnabled,
		IsReadOnly:    r.IsReadOnly,
		HasError:      r.HasError,
		Conditions:    conditions,
		Actions:       actions,
		Exceptions:    exceptions,
		RawConditions: raw.RawConditions,
		RawActions:    raw.RawActions,
		RawExceptions: raw.RawExceptions,
		LastPulledAt:  now,
	}
}

func tomlRuleFromGraph(raw graph.MessageRuleRaw) Rule {
	r := raw.Rule
	displayName := r.DisplayName
	if strings.TrimSpace(displayName) == "" {
		displayName = unnamedPlaceholder(r.Sequence)
	}
	return Rule{
		ID:            r.ID,
		Name:          displayName,
		Sequence:      r.Sequence,
		Enabled:       r.IsEnabled,
		IsReadOnly:    r.IsReadOnly,
		HasError:      r.HasError,
		When:          storePredicatesFromGraph(r.Conditions),
		Except:        storePredicatesFromGraph(r.Exceptions),
		Then:          storeActionsFromGraph(r.Actions),
		RawConditions: raw.RawConditions,
		RawActions:    raw.RawActions,
		RawExceptions: raw.RawExceptions,
	}
}

func unnamedPlaceholder(seq int) string {
	return fmt.Sprintf("<unnamed rule %d>", seq)
}
