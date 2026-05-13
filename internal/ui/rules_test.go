package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRulesCmdBarSurfaceHintForKnownSubverbs(t *testing.T) {
	cases := []string{"", "list", "pull", "apply", "edit", "new", "delete", "enable", "disable", "move", "get"}
	for _, sub := range cases {
		m := Model{}
		args := []string{}
		if sub != "" {
			args = append(args, sub)
		}
		gm, _ := m.dispatchRulesCmdBar(args)
		m = gm.(Model)
		require.NotNil(t, m.lastError, "subverb %q should set a hint via lastError", sub)
		require.Contains(t, m.lastError.Error(), "inkwell rules")
	}
}

func TestRulesCmdBarRejectsUnknownSubverb(t *testing.T) {
	m := Model{}
	gm, _ := m.dispatchRulesCmdBar([]string{"explode"})
	m = gm.(Model)
	require.NotNil(t, m.lastError)
	require.Contains(t, m.lastError.Error(), "unknown subverb")
	require.Contains(t, m.lastError.Error(), "explode")
}

func TestRulesPaletteRowsStaticPresent(t *testing.T) {
	m := &Model{}
	rows := buildMessageRulesPaletteRows(m)
	require.Len(t, rows, 5)
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	require.ElementsMatch(t,
		[]string{"rules_open", "rules_pull", "rules_apply", "rules_dry_run", "rules_new"},
		ids,
	)
	// Every row's binding starts with `:rules `.
	for _, r := range rows {
		require.Contains(t, r.Binding, ":rules ")
		require.True(t, r.Available.OK)
	}
}
