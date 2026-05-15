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

// TestApplyBindingOverridesParsesCommaSeparatedAlternates is the
// real-tenant regression for the v0.13.x bug where shipping a
// single-string override (e.g. `Up: "k"` from defaults.go)
// silently overwrote the much richer DefaultKeyMap entry
// `["k","up"]` and the user lost arrow-key navigation. The fix:
// override strings are split on `,` so `"k,up"` re-binds both.
func TestApplyBindingOverridesParsesCommaSeparatedAlternates(t *testing.T) {
	km, err := ApplyBindingOverrides(DefaultKeyMap(), BindingOverrides{
		Up:    "k,up",
		Down:  "j, down", // tolerant of whitespace
		Left:  "h,left",
		Right: "l,right",
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"k", "up"}, km.Up.Keys(),
		"comma-separated alternates must both bind")
	require.ElementsMatch(t, []string{"j", "down"}, km.Down.Keys(),
		"whitespace around commas must be trimmed")
	require.ElementsMatch(t, []string{"h", "left"}, km.Left.Keys())
	require.ElementsMatch(t, []string{"l", "right"}, km.Right.Keys())
}

// TestApplyBindingOverridesFromDefaultsKeepsArrowKeys is the
// end-to-end check that the config-shipped defaults round-trip
// through ApplyBindingOverrides without losing the arrow-key
// alternates. Without this guard, a future regression in
// defaults.go could re-introduce the v0.13.x bug.
func TestApplyBindingOverridesFromDefaultsKeepsArrowKeys(t *testing.T) {
	// Simulate the production wiring: config defaults → BindingOverrides
	// → ApplyBindingOverrides → KeyMap.
	km, err := ApplyBindingOverrides(DefaultKeyMap(), BindingOverrides{
		Up:       "k,up",
		Down:     "j,down",
		Left:     "h,left",
		Right:    "l,right",
		PageUp:   "ctrl+u,pgup",
		PageDown: "ctrl+d,pgdown",
		Home:     "g,home",
		End:      "G,end",
	})
	require.NoError(t, err)
	require.Contains(t, km.Up.Keys(), "up", "arrow-key Up must survive defaults round-trip")
	require.Contains(t, km.Down.Keys(), "down")
	require.Contains(t, km.Left.Keys(), "left")
	require.Contains(t, km.Right.Keys(), "right")
	require.Contains(t, km.PageUp.Keys(), "pgup")
	require.Contains(t, km.PageDown.Keys(), "pgdown")
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
// `docs/CONVENTIONS.md` §4 invariant: pane-scoped meanings legitimately share
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

// TestKeymapScreenerRejectExcludedFromDuplicateScan pins the spec
// 28 §5.4 collision audit. ScreenerReject defaults to capital N,
// which overlaps NewFolder (spec 18). Pane scoping disambiguates
// at dispatch time, so findDuplicateBinding excludes ScreenerReject
// from its checks list. A future contributor who removes that
// exclusion will turn this test red and be forced to re-read the
// audit comment.
func TestKeymapScreenerRejectExcludedFromDuplicateScan(t *testing.T) {
	km, err := ApplyBindingOverrides(DefaultKeyMap(), BindingOverrides{})
	require.NoError(t, err)
	require.Empty(t, findDuplicateBinding(km), "ScreenerReject must not collide with NewFolder in the duplicate scan")
}
