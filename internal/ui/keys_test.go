package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestApplyBindingOverridesEmptyKeepsDefaults is the spec 04 §17
// invariant: a user TOML with no [bindings] section gets the
// default keymap unchanged.
func TestApplyBindingOverridesEmptyKeepsDefaults(t *testing.T) {
	km, err := ApplyBindingOverrides(DefaultKeyMap(), BindingOverrides{})
	require.NoError(t, err)
	require.Equal(t, []string{"q", "ctrl+c"}, km.Quit.Keys())
	require.Equal(t, []string{"j", "down"}, km.Down.Keys())
	require.Equal(t, []string{"u"}, km.Undo.Keys())
}

// TestApplyBindingOverridesReplacesField is the rebinding happy
// path: a non-empty override replaces the default.
func TestApplyBindingOverridesReplacesField(t *testing.T) {
	km, err := ApplyBindingOverrides(DefaultKeyMap(), BindingOverrides{
		Delete: "x",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"x"}, km.Delete.Keys(),
		"override must replace, not append, the default key")
}

// TestApplyBindingOverridesRejectsDuplicates is the spec 04 §17
// duplicate-binding gate: two distinct actions sharing a key
// surface a typed error so the user sees the conflict at startup
// instead of the second binding silently winning.
func TestApplyBindingOverridesRejectsDuplicates(t *testing.T) {
	_, err := ApplyBindingOverrides(DefaultKeyMap(), BindingOverrides{
		Delete:  "x",
		Archive: "x",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), `"x"`)
}

// TestApplyBindingOverridesAllowsPaneScopedDuplicates is the
// CLAUDE.md §4 invariant: pane-scoped meanings legitimately share
// a key (e.g. `r` is reply in viewer + mark-read in list — same
// MarkRead binding, different runtime dispatch). The duplicate-
// detector must NOT flag these.
func TestApplyBindingOverridesAllowsPaneScopedDuplicates(t *testing.T) {
	// MarkRead defaults to "r"; ToggleFlag defaults to "f". Confirm
	// the default keymap loads cleanly even though "r" appears as a
	// pane-scoped key (mark-read in list, reply in viewer — same
	// binding field, different dispatch).
	_, err := ApplyBindingOverrides(DefaultKeyMap(), BindingOverrides{})
	require.NoError(t, err)
}
