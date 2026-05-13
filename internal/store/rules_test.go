package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMigration014AppliesCleanly(t *testing.T) {
	s := OpenTestStore(t)
	st := s.(*store)
	ctx := context.Background()

	var version string
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT value FROM schema_meta WHERE key = 'version'`).Scan(&version))
	require.Equal(t, "14", strings.TrimSpace(version))

	// Table exists.
	var name string
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='message_rules'`).Scan(&name))
	require.Equal(t, "message_rules", name)

	// Index exists.
	var idx string
	require.NoError(t, st.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_message_rules_sequence'`).Scan(&idx))
	require.Equal(t, "idx_message_rules_sequence", idx)
}

func TestUpsertAndListMessageRules(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	enabled := true
	r1 := MessageRule{
		AccountID:   acc,
		RuleID:      "rule-1",
		DisplayName: "Newsletters → Feed",
		Sequence:    10,
		IsEnabled:   true,
		Conditions: MessagePredicates{
			SenderContains: []string{"newsletter@"},
		},
		Actions: MessageActions{
			MarkAsRead:          &enabled,
			MoveToFolder:        "folder-feed",
			StopProcessingRules: &enabled,
		},
		LastPulledAt: time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, s.UpsertMessageRule(ctx, r1))

	rules, err := s.ListMessageRules(ctx, acc)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, "rule-1", rules[0].RuleID)
	require.Equal(t, "Newsletters → Feed", rules[0].DisplayName)
	require.Equal(t, 10, rules[0].Sequence)
	require.True(t, rules[0].IsEnabled)
	require.Equal(t, []string{"newsletter@"}, rules[0].Conditions.SenderContains)
	require.NotNil(t, rules[0].Actions.MarkAsRead)
	require.True(t, *rules[0].Actions.MarkAsRead)
	require.Equal(t, "folder-feed", rules[0].Actions.MoveToFolder)

	// Round-trip preserves raw JSON.
	require.NotEmpty(t, rules[0].RawConditions)
	require.NotEmpty(t, rules[0].RawActions)
}

func TestUpsertMessageRulesBatchReplacesAll(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	rules := []MessageRule{
		{AccountID: acc, RuleID: "a", DisplayName: "A", Sequence: 10, IsEnabled: true, LastPulledAt: now},
		{AccountID: acc, RuleID: "b", DisplayName: "B", Sequence: 20, IsEnabled: true, LastPulledAt: now},
		{AccountID: acc, RuleID: "c", DisplayName: "C", Sequence: 30, IsEnabled: true, LastPulledAt: now},
	}
	n, err := s.UpsertMessageRulesBatch(ctx, acc, rules)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	// Replace with smaller batch — older rows must disappear.
	smaller := []MessageRule{
		{AccountID: acc, RuleID: "a", DisplayName: "A2", Sequence: 5, IsEnabled: false, LastPulledAt: now},
	}
	n, err = s.UpsertMessageRulesBatch(ctx, acc, smaller)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	got, err := s.ListMessageRules(ctx, acc)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "a", got[0].RuleID)
	require.Equal(t, "A2", got[0].DisplayName)
	require.False(t, got[0].IsEnabled)

	// Empty batch clears the mirror.
	n, err = s.UpsertMessageRulesBatch(ctx, acc, nil)
	require.NoError(t, err)
	require.Equal(t, 0, n)
	got, err = s.ListMessageRules(ctx, acc)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestDeleteMessageRule(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	r := MessageRule{
		AccountID: acc, RuleID: "x", DisplayName: "X",
		Sequence: 1, IsEnabled: true, LastPulledAt: time.Now(),
	}
	require.NoError(t, s.UpsertMessageRule(ctx, r))

	require.NoError(t, s.DeleteMessageRule(ctx, acc, "x"))

	_, err := s.GetMessageRule(ctx, acc, "x")
	require.ErrorIs(t, err, ErrNotFound)

	// 404-on-delete is idempotent success.
	require.NoError(t, s.DeleteMessageRule(ctx, acc, "x"))

	// Empty rule_id is a caller bug.
	require.ErrorIs(t, s.DeleteMessageRule(ctx, acc, ""), ErrInvalidRuleID)
}

func TestUpsertMessageRuleRejectsEmptyRuleID(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	r := MessageRule{
		AccountID: acc, RuleID: "", DisplayName: "X",
		Sequence: 1, LastPulledAt: time.Now(),
	}
	require.ErrorIs(t, s.UpsertMessageRule(ctx, r), ErrInvalidRuleID)
}

func TestMessageRulesFKCascadeOnAccountDelete(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	r := MessageRule{
		AccountID: acc, RuleID: "x", DisplayName: "X",
		Sequence: 1, IsEnabled: true, LastPulledAt: time.Now(),
	}
	require.NoError(t, s.UpsertMessageRule(ctx, r))

	// Direct DELETE on accounts row to simulate sign-out + purge.
	st := s.(*store)
	_, err := st.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, acc)
	require.NoError(t, err)

	rules, err := s.ListMessageRules(ctx, acc)
	require.NoError(t, err)
	require.Empty(t, rules)
}

func TestLastMessageRulesPullReturnsMaxTimestamp(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	// Empty cache → zero time.
	last, err := s.LastMessageRulesPull(ctx, acc)
	require.NoError(t, err)
	require.True(t, last.IsZero())

	now := time.Now().UTC().Truncate(time.Second)
	older := now.Add(-1 * time.Hour)
	rules := []MessageRule{
		{AccountID: acc, RuleID: "a", DisplayName: "A", Sequence: 10, LastPulledAt: older},
		{AccountID: acc, RuleID: "b", DisplayName: "B", Sequence: 20, LastPulledAt: now},
	}
	_, err = s.UpsertMessageRulesBatch(ctx, acc, rules)
	require.NoError(t, err)

	last, err = s.LastMessageRulesPull(ctx, acc)
	require.NoError(t, err)
	require.Equal(t, now.Unix(), last.Unix())
}

func TestListMessageRulesOrdering(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	now := time.Now()
	// Two rules share sequence 10; the secondary key is rule_id ASC.
	rules := []MessageRule{
		{AccountID: acc, RuleID: "c", DisplayName: "C", Sequence: 20, LastPulledAt: now},
		{AccountID: acc, RuleID: "b", DisplayName: "B", Sequence: 10, LastPulledAt: now},
		{AccountID: acc, RuleID: "a", DisplayName: "A", Sequence: 10, LastPulledAt: now},
	}
	_, err := s.UpsertMessageRulesBatch(ctx, acc, rules)
	require.NoError(t, err)

	got, err := s.ListMessageRules(ctx, acc)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, []string{"a", "b", "c"}, []string{got[0].RuleID, got[1].RuleID, got[2].RuleID})
}

func TestUpsertMessageRulePreservesRawJSON(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	// Server payload contains a deferred field (isVoicemail) not modelled
	// in our typed predicates. Round-trip must preserve it.
	raw := json.RawMessage(`{"senderContains":["x@"],"isVoicemail":true}`)
	r := MessageRule{
		AccountID:     acc,
		RuleID:        "vm",
		DisplayName:   "VM rule",
		Sequence:      1,
		IsEnabled:     true,
		Conditions:    MessagePredicates{SenderContains: []string{"x@"}},
		RawConditions: raw,
		LastPulledAt:  time.Now(),
	}
	require.NoError(t, s.UpsertMessageRule(ctx, r))

	got, err := s.GetMessageRule(ctx, acc, "vm")
	require.NoError(t, err)
	require.Contains(t, string(got.RawConditions), "isVoicemail")
	require.Contains(t, string(got.RawConditions), "true")
}

func TestGetFolderByPathHappy(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	root := Folder{ID: "f-root", AccountID: acc, DisplayName: "Folders", LastSyncedAt: time.Now()}
	mid := Folder{ID: "f-mid", AccountID: acc, ParentFolderID: "f-root", DisplayName: "Newsletters", LastSyncedAt: time.Now()}
	leaf := Folder{ID: "f-leaf", AccountID: acc, ParentFolderID: "f-mid", DisplayName: "Vendor", LastSyncedAt: time.Now()}
	require.NoError(t, s.UpsertFolder(ctx, root))
	require.NoError(t, s.UpsertFolder(ctx, mid))
	require.NoError(t, s.UpsertFolder(ctx, leaf))

	got, err := s.GetFolderByPath(ctx, acc, "Folders/Newsletters/Vendor")
	require.NoError(t, err)
	require.Equal(t, "f-leaf", got.ID)

	// Top-level resolves.
	got, err = s.GetFolderByPath(ctx, acc, "Folders")
	require.NoError(t, err)
	require.Equal(t, "f-root", got.ID)

	// Trailing / repeated slashes are tolerated.
	got, err = s.GetFolderByPath(ctx, acc, "/Folders/Newsletters/")
	require.NoError(t, err)
	require.Equal(t, "f-mid", got.ID)
}

func TestGetFolderByPathMissingSegment(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	require.NoError(t, s.UpsertFolder(ctx, Folder{
		ID: "f-1", AccountID: acc, DisplayName: "A", LastSyncedAt: time.Now(),
	}))

	_, err := s.GetFolderByPath(ctx, acc, "A/B")
	require.ErrorIs(t, err, ErrFolderNotFound)
	_, err = s.GetFolderByPath(ctx, acc, "")
	require.ErrorIs(t, err, ErrFolderNotFound)
}

func TestGetFolderByPathNFCNormalises(t *testing.T) {
	s := OpenTestStore(t)
	acc := SeedAccount(t, s)
	ctx := context.Background()

	// Graph returns NFC. Simulate by storing NFC; query with NFD (macOS
	// filesystem convention). Both forms of "é":
	//   NFC: U+00E9
	//   NFD: U+0065 + U+0301
	nfcName := "Café"
	nfdQuery := "Café"
	require.NoError(t, s.UpsertFolder(ctx, Folder{
		ID: "f-cafe", AccountID: acc, DisplayName: nfcName, LastSyncedAt: time.Now(),
	}))

	got, err := s.GetFolderByPath(ctx, acc, nfdQuery)
	require.NoError(t, err)
	require.Equal(t, "f-cafe", got.ID)
}
