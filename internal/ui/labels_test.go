package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchiveVerbLowerArchive(t *testing.T) {
	require.Equal(t, "archive", archiveVerbLower(ArchiveLabelArchive))
}

func TestArchiveVerbLowerDone(t *testing.T) {
	require.Equal(t, "done", archiveVerbLower(ArchiveLabelDone))
}

func TestArchiveVerbTitleArchive(t *testing.T) {
	require.Equal(t, "Archive", archiveVerbTitle(ArchiveLabelArchive))
}

func TestArchiveVerbTitleDone(t *testing.T) {
	require.Equal(t, "Done", archiveVerbTitle(ArchiveLabelDone))
}

// TestArchiveVerbForNameOnlyTouchesArchive — the helper branches
// on the action name; non-archive names pass through unchanged.
func TestArchiveVerbForNameOnlyTouchesArchive(t *testing.T) {
	cases := []struct {
		name, label, want string
	}{
		{"archive", "archive", "archive"},
		{"archive", "done", "done"},
		{"soft_delete", "done", "soft_delete"},
		{"mark_read", "archive", "mark_read"},
		{"unsubscribe", "done", "unsubscribe"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, archiveVerbForName(tc.name, ArchiveLabel(tc.label)),
			"name=%q label=%q", tc.name, tc.label)
	}
}

// TestArchiveLabelZeroValueDefaultsToArchive — defensive: a
// zero-value label (empty string) renders the default verb so an
// uninitialised Model in a unit test doesn't silently produce an
// empty token.
func TestArchiveLabelZeroValueDefaultsToArchive(t *testing.T) {
	var zero ArchiveLabel
	require.Equal(t, "archive", archiveVerbLower(zero))
	require.Equal(t, "Archive", archiveVerbTitle(zero))
}

// TestArchivePaletteRowTitleSwitchesOnLabel — palette title moves
// between "Archive message" and "Mark done" per spec 30 §5.5.
func TestArchivePaletteRowTitleSwitchesOnLabel(t *testing.T) {
	require.Equal(t, "Archive message", archivePaletteRowTitle(ArchiveLabelArchive))
	require.Equal(t, "Mark done", archivePaletteRowTitle(ArchiveLabelDone))
}

// TestArchivePaletteThreadRowTitleSwitchesOnLabel — same for the
// thread row.
func TestArchivePaletteThreadRowTitleSwitchesOnLabel(t *testing.T) {
	require.Equal(t, "Archive thread", archivePaletteThreadRowTitle(ArchiveLabelArchive))
	require.Equal(t, "Mark thread done", archivePaletteThreadRowTitle(ArchiveLabelDone))
}

// TestDefaultArchiveBindsAandE pins the spec 30 §3.2 default.
func TestDefaultArchiveBindsAandE(t *testing.T) {
	km := DefaultKeyMap()
	keys := km.Archive.Keys()
	require.Equal(t, []string{"a", "e"}, keys, "Archive default must bind both a and e")
}

// TestArchiveOverrideAOnlyDropsE — `[bindings].archive = "a"`
// drops `e` from the binding.
func TestArchiveOverrideAOnlyDropsE(t *testing.T) {
	km, err := ApplyBindingOverrides(DefaultKeyMap(), BindingOverrides{Archive: "a"})
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, km.Archive.Keys())
}

// TestArchiveOverrideEOnlyDropsA — inverse.
func TestArchiveOverrideEOnlyDropsA(t *testing.T) {
	km, err := ApplyBindingOverrides(DefaultKeyMap(), BindingOverrides{Archive: "e"})
	require.NoError(t, err)
	require.Equal(t, []string{"e"}, km.Archive.Keys())
}

// TestFindDuplicateBindingDetectsArchiveCollision — overriding
// another action to one of the archive defaults is rejected.
func TestFindDuplicateBindingDetectsArchiveCollision(t *testing.T) {
	_, err := ApplyBindingOverrides(DefaultKeyMap(), BindingOverrides{Delete: "e"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `"e"`)
}

// TestHelpOverlayArchiveRowReadsArchiveByDefault — the Triage
// section's Archive row reads "archive" under the default label.
func TestHelpOverlayArchiveRowReadsArchiveByDefault(t *testing.T) {
	sections := buildHelpSections(DefaultKeyMap(), ArchiveLabelArchive)
	require.True(t, helpRowExists(sections, "archive"), "help overlay must render the archive row")
}

// TestHelpOverlayArchiveRowReadsDoneWhenLabelDone — same row's
// description is "done" under the configured label.
func TestHelpOverlayArchiveRowReadsDoneWhenLabelDone(t *testing.T) {
	sections := buildHelpSections(DefaultKeyMap(), ArchiveLabelDone)
	require.True(t, helpRowExists(sections, "done"), "help overlay must render the done row")
	require.False(t, helpRowExists(sections, "archive"), "no leftover archive row when label is done")
}

func helpRowExists(sections []helpSection, desc string) bool {
	for _, s := range sections {
		for _, r := range s.rows {
			if r.desc == desc {
				return true
			}
		}
	}
	return false
}
