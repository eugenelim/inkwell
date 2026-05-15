package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidRuleID is returned when callers pass an empty rule_id to
// the message-rule store helpers. Defence in depth: the loader and
// pull pipelines should never produce empty IDs; the store rejects
// at the SQL boundary so a buggy caller can't silently create an
// invalid row (spec 32 §4.1).
var ErrInvalidRuleID = errors.New("store: invalid rule id")

// ListMessageRules returns the cached rules for an account, ordered
// by sequence_num ASC and rule_id ASC (stable when two rules share a
// sequence — Graph allows duplicates). Returns an empty slice (not
// nil) when no rules cached. The caller distinguishes
// "never pulled" from "pulled and confirmed empty" via
// LastMessageRulesPull(ctx, accountID).IsZero().
func (s *store) ListMessageRules(ctx context.Context, accountID int64) ([]MessageRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT account_id, rule_id, display_name, sequence_num,
		       is_enabled, is_read_only, has_error,
		       conditions_json, actions_json, exceptions_json,
		       last_pulled_at
		FROM message_rules
		WHERE account_id = ?
		ORDER BY sequence_num ASC, rule_id ASC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("list message rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []MessageRule{}
	for rows.Next() {
		r, err := scanMessageRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetMessageRule returns the single cached rule by ID, or ErrNotFound
// when absent.
func (s *store) GetMessageRule(ctx context.Context, accountID int64, ruleID string) (*MessageRule, error) {
	if ruleID == "" {
		return nil, ErrInvalidRuleID
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT account_id, rule_id, display_name, sequence_num,
		       is_enabled, is_read_only, has_error,
		       conditions_json, actions_json, exceptions_json,
		       last_pulled_at
		FROM message_rules
		WHERE account_id = ? AND rule_id = ?`, accountID, ruleID)
	r, err := scanMessageRule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

// UpsertMessageRule inserts or updates one row.
func (s *store) UpsertMessageRule(ctx context.Context, r MessageRule) error {
	if r.RuleID == "" {
		return ErrInvalidRuleID
	}
	conditionsJSON, actionsJSON, exceptionsJSON, err := encodeRuleJSON(r)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO message_rules
		  (account_id, rule_id, display_name, sequence_num,
		   is_enabled, is_read_only, has_error,
		   conditions_json, actions_json, exceptions_json,
		   last_pulled_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, rule_id) DO UPDATE SET
		    display_name    = excluded.display_name,
		    sequence_num    = excluded.sequence_num,
		    is_enabled      = excluded.is_enabled,
		    is_read_only    = excluded.is_read_only,
		    has_error       = excluded.has_error,
		    conditions_json = excluded.conditions_json,
		    actions_json    = excluded.actions_json,
		    exceptions_json = excluded.exceptions_json,
		    last_pulled_at  = excluded.last_pulled_at`,
		r.AccountID, r.RuleID, r.DisplayName, r.Sequence,
		boolToInt(r.IsEnabled), boolToInt(r.IsReadOnly), boolToInt(r.HasError),
		conditionsJSON, actionsJSON, exceptionsJSON,
		r.LastPulledAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("upsert message rule: %w", err)
	}
	return nil
}

// UpsertMessageRulesBatch replaces the entire mirror for an account
// in one transaction (DELETE-all + multi-row INSERT). Empty input
// clears the cache. Returns the number of rows written.
func (s *store) UpsertMessageRulesBatch(ctx context.Context, accountID int64, rules []MessageRule) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM message_rules WHERE account_id = ?`, accountID); err != nil {
		return 0, fmt.Errorf("clear message rules: %w", err)
	}

	if len(rules) == 0 {
		return 0, tx.Commit()
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO message_rules
		  (account_id, rule_id, display_name, sequence_num,
		   is_enabled, is_read_only, has_error,
		   conditions_json, actions_json, exceptions_json,
		   last_pulled_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range rules {
		if r.RuleID == "" {
			return 0, ErrInvalidRuleID
		}
		conditionsJSON, actionsJSON, exceptionsJSON, err := encodeRuleJSON(r)
		if err != nil {
			return 0, err
		}
		// account_id from the row is ignored — the batch is scoped to
		// the function arg so callers can't accidentally cross accounts.
		if _, err := stmt.ExecContext(ctx,
			accountID, r.RuleID, r.DisplayName, r.Sequence,
			boolToInt(r.IsEnabled), boolToInt(r.IsReadOnly), boolToInt(r.HasError),
			conditionsJSON, actionsJSON, exceptionsJSON,
			r.LastPulledAt.Unix(),
		); err != nil {
			return 0, fmt.Errorf("insert message rule %q: %w", r.RuleID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return len(rules), nil
}

// DeleteMessageRule removes one row by ID. 404-on-delete is success
// (idempotent — matches the `docs/CONVENTIONS.md` §3 mutation invariant).
func (s *store) DeleteMessageRule(ctx context.Context, accountID int64, ruleID string) error {
	if ruleID == "" {
		return ErrInvalidRuleID
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM message_rules WHERE account_id = ? AND rule_id = ?`,
		accountID, ruleID)
	if err != nil {
		return fmt.Errorf("delete message rule: %w", err)
	}
	return nil
}

// LastMessageRulesPull returns the most-recent last_pulled_at across
// all rules for the account, or zero time when no rule has ever been
// cached. Used by the manager status hint and by the "never pulled"
// vs "pulled and empty" discriminator (spec 32 §4.1).
func (s *store) LastMessageRulesPull(ctx context.Context, accountID int64) (time.Time, error) {
	var lastPull sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(last_pulled_at) FROM message_rules WHERE account_id = ?`,
		accountID).Scan(&lastPull)
	if err != nil {
		return time.Time{}, fmt.Errorf("last message rules pull: %w", err)
	}
	if !lastPull.Valid {
		return time.Time{}, nil
	}
	return time.Unix(lastPull.Int64, 0), nil
}

// scanMessageRule is shared by ListMessageRules / GetMessageRule.
// rowScanner / boolToInt are package-private helpers declared in
// messages.go and reused here.
func scanMessageRule(row rowScanner) (MessageRule, error) {
	var r MessageRule
	var enabled, readOnly, hasErr int
	var conditionsJSON, actionsJSON, exceptionsJSON string
	var lastPull int64
	if err := row.Scan(
		&r.AccountID, &r.RuleID, &r.DisplayName, &r.Sequence,
		&enabled, &readOnly, &hasErr,
		&conditionsJSON, &actionsJSON, &exceptionsJSON,
		&lastPull,
	); err != nil {
		return MessageRule{}, err
	}
	r.IsEnabled = enabled != 0
	r.IsReadOnly = readOnly != 0
	r.HasError = hasErr != 0
	r.LastPulledAt = time.Unix(lastPull, 0)

	// Decode raw JSON into typed predicates / actions and preserve
	// the raw bytes so non-v1 fields round-trip cleanly on update
	// (spec 32 §4.3 / §5.3).
	r.RawConditions = json.RawMessage(conditionsJSON)
	r.RawActions = json.RawMessage(actionsJSON)
	r.RawExceptions = json.RawMessage(exceptionsJSON)
	if strings.TrimSpace(conditionsJSON) != "" {
		if err := json.Unmarshal([]byte(conditionsJSON), &r.Conditions); err != nil {
			return MessageRule{}, fmt.Errorf("decode conditions for rule %q: %w", r.RuleID, err)
		}
	}
	if strings.TrimSpace(actionsJSON) != "" {
		if err := json.Unmarshal([]byte(actionsJSON), &r.Actions); err != nil {
			return MessageRule{}, fmt.Errorf("decode actions for rule %q: %w", r.RuleID, err)
		}
	}
	if strings.TrimSpace(exceptionsJSON) != "" {
		if err := json.Unmarshal([]byte(exceptionsJSON), &r.Exceptions); err != nil {
			return MessageRule{}, fmt.Errorf("decode exceptions for rule %q: %w", r.RuleID, err)
		}
	}
	return r, nil
}

// encodeRuleJSON returns the three JSON payloads to persist. When
// Raw* is non-empty the caller has supplied a verbatim Graph payload
// to round-trip (preserves non-v1 fields); otherwise we marshal the
// typed projection.
func encodeRuleJSON(r MessageRule) (conditions, actions, exceptions string, err error) {
	conditions, err = pickRuleJSON(r.RawConditions, r.Conditions)
	if err != nil {
		return "", "", "", fmt.Errorf("encode conditions: %w", err)
	}
	actions, err = pickRuleJSON(r.RawActions, r.Actions)
	if err != nil {
		return "", "", "", fmt.Errorf("encode actions: %w", err)
	}
	exceptions, err = pickRuleJSON(r.RawExceptions, r.Exceptions)
	if err != nil {
		return "", "", "", fmt.Errorf("encode exceptions: %w", err)
	}
	return conditions, actions, exceptions, nil
}

func pickRuleJSON(raw json.RawMessage, typed any) (string, error) {
	if len(raw) > 0 && strings.TrimSpace(string(raw)) != "" {
		return string(raw), nil
	}
	b, err := json.Marshal(typed)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
